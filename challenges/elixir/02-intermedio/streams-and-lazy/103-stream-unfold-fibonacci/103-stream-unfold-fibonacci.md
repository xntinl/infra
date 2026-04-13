# `Stream.unfold/2` — infinite sequences from a seed

**Project**: `unfold_fib` — generate Fibonacci, Collatz, and other recurrences
as lazy infinite streams using `Stream.unfold/2`.

---

## Project context

Lots of interesting sequences are defined recursively: Fibonacci, Collatz,
geometric progressions, random walks, iterative numerical methods. In
strict languages you either bound them up front ("first 100 Fibonacci
numbers") or you build a custom iterator. Elixir gives you a cleaner tool:
`Stream.unfold/2` turns *any* "state → {value, new_state}" function into
a lazy, potentially infinite stream.

The key insight: the stream is not a recursive data structure; it's a
*recipe*. A 100-billionth Fibonacci call doesn't blow the stack because
nothing is computed until you ask — and you ask for only as much as
`Enum.take/2` needs.

Project structure:

```
unfold_fib/
├── lib/
│   └── unfold_fib.ex
├── test/
│   └── unfold_fib_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Stream.unfold/2` shape

```elixir
Stream.unfold(initial_state, fn state ->
  {value_to_emit, next_state}  # to continue
  # or
  nil                          # to halt
end)
```

Each call produces *exactly one* element and the next state. Returning `nil`
ends the stream. This mirrors `Enum.reduce/3` in reverse — instead of folding
values into an accumulator, you unfold an accumulator into values.

### 2. Fibonacci as a two-number state

```elixir
Stream.unfold({0, 1}, fn {a, b} -> {a, {b, a + b}} end)
```

State is `{current, next}`. We emit `current`, then advance to `{next, current+next}`.
No recursion on the call stack — advancement happens lazily, one step per pull.

### 3. Collatz: termination is data, not control

Collatz "halts" at 1 (under the conjecture). We can model that inside `unfold`
by returning `nil` when the state is 1 — after emitting it once:

```elixir
Stream.unfold(n, fn
  1 -> nil
  n when rem(n, 2) == 0 -> {n, div(n, 2)}
  n -> {n, 3 * n + 1}
end)
```

We emit `n` before halting, so the final sequence includes `1`.

### 4. `Stream.iterate/2` vs `Stream.unfold/2`

`Stream.iterate(x, f)` = `x, f(x), f(f(x))...` — one-argument step, identity
output. `Stream.unfold/2` decouples state from output, so the *emitted* value
can differ from the next state. Use `iterate` when state == output; `unfold`
when they diverge (Fibonacci: we track `{a, b}`, emit only `a`).

---

## Design decisions

**Option A — Recursive function with explicit list accumulator**
- Pros: simple to write; one function; no abstraction.
- Cons: must bound up front; no laziness; stack-consuming unless tail-recursive; can't share infinite-sequence logic cleanly.

**Option B — `Stream.unfold/2` returning a lazy stream** (chosen)
- Pros: decouples "how to step" from "how many to take"; infinite streams are cheap; composes with other `Stream.*` and terminates only at the consumer.
- Cons: per-element function call overhead; no cleanup hook — use `Stream.resource/3` if you need one.

→ Chose **B** because the point is to internalize the unfold primitive and see that infinite sequences are just a recipe, not a materialized list.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
    {halt},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new unfold_fib
cd unfold_fib
```

### Step 2: `lib/unfold_fib.ex`

**Objective**: Implement `unfold_fib.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.


```elixir
defmodule UnfoldFib do
  @moduledoc """
  Infinite and terminating sequences built with `Stream.unfold/2`.
  All functions return lazy streams — take as many elements as you need.
  """

  @doc """
  Infinite Fibonacci stream starting `0, 1, 1, 2, 3, 5, 8, ...`.

  State `{a, b}` means "next to emit is `a`, and after emitting, the pair
  advances to `{b, a + b}`". Because it's lazy, requesting the first 10
  elements does exactly 10 additions.
  """
  @spec fibonacci() :: Enumerable.t()
  def fibonacci do
    Stream.unfold({0, 1}, fn {a, b} -> {a, {b, a + b}} end)
  end

  @doc """
  Collatz sequence starting at `n`. Terminates at 1 (assuming the Collatz
  conjecture, which has been empirically verified for every integer ever
  tested — no proof exists yet).

  Returning `nil` from the step function ends the stream.
  """
  @spec collatz(pos_integer()) :: Enumerable.t()
  def collatz(n) when is_integer(n) and n >= 1 do
    Stream.unfold(n, fn
      # Emit the final 1, then halt on the next pull.
      1 -> {1, :done}
      :done -> nil
      k when rem(k, 2) == 0 -> {k, div(k, 2)}
      k -> {k, 3 * k + 1}
    end)
  end

  @doc """
  Geometric progression `a, a*r, a*r^2, ...` — infinite.

  A nice illustration that when output == state, `Stream.iterate/2` is
  equally valid; we use `unfold` here for symmetry with the rest.
  """
  @spec geometric(number(), number()) :: Enumerable.t()
  def geometric(a, r) do
    Stream.unfold(a, fn x -> {x, x * r} end)
  end
end
```

### Step 3: `test/unfold_fib_test.exs`

**Objective**: Write `unfold_fib_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule UnfoldFibTest do
  use ExUnit.Case, async: true

  describe "fibonacci/0" do
    test "first 10 Fibonacci numbers" do
      assert UnfoldFib.fibonacci() |> Enum.take(10) ==
               [0, 1, 1, 2, 3, 5, 8, 13, 21, 34]
    end

    test "the stream is infinite — take never exhausts it" do
      # Take a big chunk; if fibonacci/0 were strict this would run forever.
      huge = UnfoldFib.fibonacci() |> Enum.take(1_000)
      assert length(huge) == 1_000
    end
  end

  describe "collatz/1" do
    test "classic 6 → 3 → 10 → 5 → 16 → 8 → 4 → 2 → 1 sequence" do
      assert Enum.to_list(UnfoldFib.collatz(6)) == [6, 3, 10, 5, 16, 8, 4, 2, 1]
    end

    test "starting at 1 emits only 1" do
      assert Enum.to_list(UnfoldFib.collatz(1)) == [1]
    end

    test "27 produces the famously long (112-step) sequence" do
      # Pick a known-count: Collatz(27) is 112 terms including the starting 27 and final 1.
      assert length(Enum.to_list(UnfoldFib.collatz(27))) == 112
    end
  end

  describe "geometric/2" do
    test "powers of 2" do
      assert UnfoldFib.geometric(1, 2) |> Enum.take(5) == [1, 2, 4, 8, 16]
    end

    test "shrinking geometric" do
      assert UnfoldFib.geometric(1.0, 0.5) |> Enum.take(4) == [1.0, 0.5, 0.25, 0.125]
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

`Stream.unfold/2` captures the step function in a `%Stream{}` and defers all computation. Each pull invokes the step function once with the current state, yielding `{value, next_state}` or `nil`. Because nothing is allocated beyond the current state, a stream of 10 billion Fibonacci numbers costs the same memory as a stream of ten — only the indices you consume pay compute.

---

## Benchmark

```elixir
{t, _} = :timer.tc(fn -> UnfoldFib.fibonacci() |> Enum.take(10_000) |> List.last() end)
IO.puts("fib(10_000) in #{div(t, 1000)}ms")
```

Target esperado: 10_000 Fibonacci elements in <50 ms on modern hardware; most of the cost is the bigint arithmetic on the last few thousand (the 10_000th has ~2090 digits).

---

## Trade-offs and production gotchas

**1. The state must be small enough to afford per-step allocation**
`Stream.unfold/2` calls your function once per element. If your "state"
is a large map that you rebuild each step, you're paying that cost per
element. For big stateful transforms over a source, `Stream.transform/3`
is usually better — it gives you an outer source plus
an accumulator, not two conflated things.

**2. Infinite streams require an upstream bound**
`UnfoldFib.fibonacci() |> Enum.to_list()` never terminates. Always compose
with `Stream.take/2`, `Stream.take_while/2`, or an outer `Enum.take/2`
*before* any materializing terminal.

**3. BigInt arithmetic is free in Elixir, but not free in cost**
Fibonacci(1000) has 209 digits. Elixir handles arbitrary-precision
integers transparently, but each addition past the machine word grows
in cost. A stream of 10k Fibonacci numbers is not O(10k) — it's closer
to O(k² log k) in bits. Don't be surprised if large indices are slow.

**4. `Stream.unfold` can return functions as state — be careful**
Technically you can store a closure as state. This is almost always a
smell — it hides what the iteration does. Prefer plain data (tuples,
structs) so the recurrence is visible at the call site.

**5. Halt semantics: `nil` vs `{:halt, _}`**
Unlike `Stream.resource/3` (which uses `{:halt, acc}`), `Stream.unfold/2`
halts on `nil` from the step function. The acc at halt time is simply
discarded — no cleanup hook. If you need cleanup, use `Stream.resource/3`
instead.

**6. When NOT to use `Stream.unfold`**
- When you already have a concrete collection — `Stream.unfold` is for
  *generating* values, not transforming them.
- When the next step needs information from *outside* the state (DB,
  another stream) — use `Stream.transform/3` or `Stream.zip/2` instead.
- For simple arithmetic progressions — `Stream.iterate/2` is shorter.

---

## Reflection

- You need Newton-Raphson iterations for `sqrt(x)` stopping when `|x_{n+1} - x_n| < eps`. Can you express that with `Stream.unfold/2` alone, or do you need `Stream.take_while/2` layered on top? Why?
- Storing a closure as unfold state is possible but discouraged. Invent a concrete case where doing so would actually be clearer than using plain data, then argue against yourself.

---

## Resources

- [`Stream.unfold/2` — hexdocs](https://hexdocs.pm/elixir/Stream.html#unfold/2)
- [`Stream.iterate/2` — hexdocs](https://hexdocs.pm/elixir/Stream.html#iterate/2) — the simpler sibling
- [Wikipedia — Collatz conjecture](https://en.wikipedia.org/wiki/Collatz_conjecture)
- [José Valim — original Stream announcement](https://elixir-lang.org/blog/2013/08/21/elixir-streams/) — introduces unfold as the "generator primitive"
- Dave Thomas — *Programming Elixir*, chapter on lazy enumerables


## Deep Dive

Streams are lazy, composable data pipelines that process one element at a time without materializing intermediate collections. This is fundamentally different from Enum, which materializes the entire dataset before the next operation.

**Lazy evaluation semantics:**
Stream operations return a `%Stream{}` struct containing a function. The actual computation is deferred until consumed by a terminal operation (`.run()`, `Enum.to_list()`, etc.). This allows streams to:
- Chain indefinite sequences (e.g., `Stream.iterate(0, &(&1 + 1))`)
- Transform without memory bloat (e.g., processing multi-gigabyte files)
- Compose reusable pipelines as first-class values

**Resource lifecycle in streams:**
Streams wrapping resources (`Stream.resource/3`) must define cleanup functions. A stream created from a file remains "open" (in terms of the lambda) until the consumer finishes or errors. If the consumer crashes or stops early, the cleanup function still runs — critical for proper file/socket/port management.

**Backpressure and demand:**
Unlike streams in other languages, Elixir's synchronous streams don't inherently implement backpressure. Backpressure is demand-based: the consumer pulls data at its own pace. `GenStage` and `Flow` add explicit backpressure — the producer waits for the consumer to request more elements. This is why benchmarking matters: a naive stream consumer can overwhelm memory if the pipeline produces faster than it consumes.
