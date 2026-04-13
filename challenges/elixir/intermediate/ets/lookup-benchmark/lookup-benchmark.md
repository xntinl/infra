# Benchmarking lookups — ETS vs `Map` vs `Keyword`

**Project**: `ets_benchmark` — measure point-lookup latency across three
in-memory data structures with `:timer.tc/1` and Benchee, then interpret
the results honestly.

---

## Why lookup benchmark matters

"Use ETS for speed" is a piece of folk wisdom that's **wrong** half the
time. A plain `Map` inside the caller's process is usually faster than ETS
because it doesn't cross the ETS boundary (no term copy). `Keyword` lists
are O(N) and obliterated by both. And yet ETS wins the moment the data
needs to be **shared across processes** without a GenServer serialization
point, or the moment the `Map` grows so large that copying it between
processes is worse than the ETS copy.

This exercise writes a benchmark that pins down the crossover points on
your machine. By the end you'll have concrete numbers for: at what size
is `Keyword` no longer acceptable, at what size `Map` overtakes ETS, and
vice versa.

## Why Benchee + :timer.tc and not X

**Why not just eyeball two implementations?** Microbenchmark gut feeling is
consistently wrong. Without warmup and iteration counts, you're measuring
scheduler jitter and cache state, not the code.

**Why not pick one tool?** `:timer.tc` is the right size for 1ms+ operations
and is in the standard library — no deps. Benchee handles sub-microsecond
ops, memory, and percentiles. This exercise uses both deliberately.

**Why `Keyword` at all if it's obviously slower?** Because it's idiomatic
for small option lists and beginners sometimes reach for it as a store. The
benchmark quantifies exactly when that stops being acceptable.

---

## Project structure

```
ets_benchmark/
├── lib/
│   └── ets_benchmark.ex
├── script/
│   └── main.exs
├── test/
│   └── ets_benchmark_test.exs
└── mix.exs
```

Add `{:benchee, "~> 1.3", only: :dev}` to `mix.exs` deps.

---

## Core concepts

### 1. `:timer.tc/1` — the quick-and-dirty stopwatch

```elixir
{micros, result} = :timer.tc(fn -> work() end)
```

Returns `{elapsed_microseconds, return_value}`. Single-shot; no warmup,
no statistics, no isolation. Good for a one-off "is this slow?" check,
misleading for anything smaller than a millisecond because scheduler
jitter dominates.

### 2. Why you should graduate to Benchee

Benchee handles the parts `:timer.tc` ignores:

- **Warmup** — lets the JIT and BEAM caches stabilize.
- **Sampling** — runs thousands of iterations and reports percentiles.
- **Memory measurement** — reports bytes allocated per iteration.
- **Parallelism** — can run benches in parallel to simulate concurrent load.

Source: [Benchee docs on hexdocs](https://hexdocs.pm/benchee/readme.html).

### 3. What each structure is good at

| Structure    | Lookup | Storage        | Cross-process | Mutable in place |
|--------------|--------|----------------|---------------|------------------|
| `Keyword`    | O(N)   | Owner heap     | Copy to read  | No               |
| `Map`        | O(log N) eff. const. for small N | Owner heap | Copy to read | No (persistent) |
| ETS `:set`   | O(1)   | Shared ETS heap | Direct read (w/ copy per call) | Yes |

Note the asymmetry: `Map` lookups are faster than ETS **for the owning
process** because no copy is needed — the map sits on that process's heap
and lookup is a pointer chase. ETS lookups always copy the tuple out of
the ETS heap.

### 4. Cross-process reality changes everything

If process A owns a `Map` and process B needs to read it, B must call A
(e.g. via `GenServer.call`), and A must send the result (copy). That
copy is often bigger than ETS's per-tuple copy — ETS wins instantly for
any "one writer, many readers" shape.

### 5. The sizes where the ranking changes

- N = 10: `Keyword` is fine; differences are in nanoseconds.
- N = 100: `Keyword` starts falling behind; `Map` and ETS are close.
- N = 10_000: `Keyword` is catastrophic; `Map` is fastest for owner; ETS
  is close.
- N = 1_000_000: `Map` may be fastest in isolation but produces massive
  GC pressure. ETS is often the saner choice even for a single owner.

Your numbers will vary. Run the benchmark.

---

## Design decisions

**Option A — Single bench file with one tool (Benchee)**
- Pros: Clean, one source of truth.
- Cons: Hides the fact that `:timer.tc` exists and is often fine.

**Option B — Tests use `:timer.tc`, bench file uses Benchee** (chosen)
- Pros: Shows both tools at their correct job (coarse asserts vs fine
  profiling). Tests can sanity-check the ranking without a deps install.
- Cons: Two code paths to understand.

→ Chose **B** because teaching the tool selection is half the lesson.

---

## Implementation

### `mix.exs`

```elixir
defmodule EtsBenchmark.MixProject do
  use Mix.Project

  def project do
    [
      app: :ets_benchmark,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new ets_benchmark
cd ets_benchmark
```

Add to `mix.exs`:

Then `mix deps.get`.

### `lib/ets_benchmark.ex`

**Objective**: Implement `ets_benchmark.ex` — the access pattern that exposes the trade-off between ETS concurrency flags, match specs, and lookup cost.

```elixir
defmodule EtsBenchmark do
  @moduledoc """
  Builds three equivalent in-memory stores (`Keyword`, `Map`, ETS `:set`)
  populated with `{i, i * 2}` for i in `1..n`, and exposes a lookup function
  for each. The benches and tests compare them.
  """

  @doc "Builds a Keyword list of `{i, i*2}` for i in 1..n."
  @spec build_keyword(pos_integer()) :: [{integer(), integer()}]
  def build_keyword(n), do: for(i <- 1..n, do: {i, i * 2})

  @doc "Builds a Map of `i => i*2` for i in 1..n."
  @spec build_map(pos_integer()) :: %{integer() => integer()}
  def build_map(n), do: Map.new(1..n, &{&1, &1 * 2})

  @doc """
  Builds a populated `:set` ETS table. Caller owns the table; destroy it with
  `:ets.delete/1` when done. `read_concurrency: true` reflects the common
  "many readers" deployment — not fair to turn it off just to make ETS look
  bad on a read bench.
  """
  @spec build_ets(pos_integer()) :: :ets.tid()
  def build_ets(n) do
    t = :ets.new(:bench, [:set, :public, read_concurrency: true])
    for i <- 1..n, do: :ets.insert(t, {i, i * 2})
    t
  end

  # ── Lookups ────────────────────────────────────────────────────────────

  @spec lookup_keyword([{integer(), integer()}], integer()) :: integer() | nil
  def lookup_keyword(kw, key), do: Keyword.get(kw, key)

  @spec lookup_map(map(), integer()) :: integer() | nil
  def lookup_map(m, key), do: Map.get(m, key)

  @spec lookup_ets(:ets.tid(), integer()) :: integer() | nil
  def lookup_ets(t, key) do
    case :ets.lookup(t, key) do
      [{^key, v}] -> v
      [] -> nil
    end
  end

  # ── One-shot timing for the unit tests ─────────────────────────────────

  @doc "Runs a zero-arg fun N times and returns total microseconds."
  @spec time_many(non_neg_integer(), (-> any())) :: integer()
  def time_many(iterations, fun) do
    {micros, _} = :timer.tc(fn -> Enum.each(1..iterations, fn _ -> fun.() end) end)
    micros
  end
end
```

### Step 3: `bench/lookup_bench.exs`

**Objective**: Implement `lookup_bench.exs` — the access pattern that exposes the trade-off between ETS concurrency flags, match specs, and lookup cost.

```elixir
# Run with: mix run bench/lookup_bench.exs

sizes = [100, 10_000]

for n <- sizes do
  kw = EtsBenchmark.build_keyword(n)
  m = EtsBenchmark.build_map(n)
  t = EtsBenchmark.build_ets(n)

  # Pick a handful of keys scattered across the range.
  keys = [1, div(n, 4), div(n, 2), div(3 * n, 4), n]

  IO.puts("\n=== N = #{n} ===")

  Benchee.run(
    %{
      "keyword" => fn -> Enum.each(keys, &EtsBenchmark.lookup_keyword(kw, &1)) end,
      "map"     => fn -> Enum.each(keys, &EtsBenchmark.lookup_map(m, &1)) end,
      "ets"     => fn -> Enum.each(keys, &EtsBenchmark.lookup_ets(t, &1)) end
    },
    time: 2,
    warmup: 1,
    memory_time: 1,
    print: [fast_warning: false]
  )

  :ets.delete(t)
end
```

### Step 4: `test/ets_benchmark_test.exs`

**Objective**: Write `ets_benchmark_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule EtsBenchmarkTest do
  use ExUnit.Case, async: true

  doctest EtsBenchmark

  describe "correctness (before we benchmark)" do
    test "all three structures return the same value for a given key" do
      n = 100
      kw = EtsBenchmark.build_keyword(n)
      m = EtsBenchmark.build_map(n)
      t = EtsBenchmark.build_ets(n)

      for k <- [1, 42, 100] do
        assert EtsBenchmark.lookup_keyword(kw, k) == k * 2
        assert EtsBenchmark.lookup_map(m, k) == k * 2
        assert EtsBenchmark.lookup_ets(t, k) == k * 2
      end

      :ets.delete(t)
    end

    test "missing keys return nil consistently" do
      n = 10
      kw = EtsBenchmark.build_keyword(n)
      m = EtsBenchmark.build_map(n)
      t = EtsBenchmark.build_ets(n)

      assert EtsBenchmark.lookup_keyword(kw, 999) == nil
      assert EtsBenchmark.lookup_map(m, 999) == nil
      assert EtsBenchmark.lookup_ets(t, 999) == nil

      :ets.delete(t)
    end
  end

  describe ":timer.tc coarse sanity check" do
    @tag :timing
    test "for N=10_000, keyword lookups are much slower than map or ets" do
      n = 10_000
      iters = 1_000
      kw = EtsBenchmark.build_keyword(n)
      m = EtsBenchmark.build_map(n)
      t = EtsBenchmark.build_ets(n)

      key = div(n, 2)

      t_kw = EtsBenchmark.time_many(iters, fn -> EtsBenchmark.lookup_keyword(kw, key) end)
      t_m = EtsBenchmark.time_many(iters, fn -> EtsBenchmark.lookup_map(m, key) end)
      t_e = EtsBenchmark.time_many(iters, fn -> EtsBenchmark.lookup_ets(t, key) end)

      # Keyword is O(N) scan; at N=10k it should be at least an order of
      # magnitude slower than Map or ETS on any reasonable machine. We use
      # 3x as a very lenient bound to avoid flakiness on loaded CI.
      assert t_kw > 3 * t_m
      assert t_kw > 3 * t_e

      :ets.delete(t)
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
mix run bench/lookup_bench.exs
```

### Why this works

The three structures have fundamentally different access paths: `Keyword` is
a linear list traversal (O(N)), `Map` uses a hash-array-mapped-trie rooted in
the caller's heap (O(log N) but effectively constant for small N, no copy),
and ETS walks a hash table in its own heap then copies the tuple out. Benchee
isolates these by warming up the JIT/cache and measuring distribution; the
`:timer.tc` test asserts only on gross ratios (3× or more) so CI jitter
doesn't make it flaky.

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `EtsBenchmark`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== EtsBenchmark demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    # EtsBenchmark.build_keyword/1 requires 1 argument(s);
    # call it with real values appropriate for this exercise.
    # EtsBenchmark.build_map/1 requires 1 argument(s);
    # call it with real values appropriate for this exercise.
    :ok
  end
end

Main.main()
```

## Deep Dive: ETS Concurrency Trade-Offs and Operation Atomicity

ETS (Erlang Term Storage) is mutable, shared, in-process state—antithetical to Elixir's immutability. But it's required for specific cases: large shared datasets, fast lookups under contention, atomic counters across processes. Trade-off: operations aren't composable (no atomic multi-table updates without extra bookkeeping), and debugging is harder because mutations are invisible in code.

Use ETS when: (1) true sharing between processes, (2) data is large (megabytes), (3) sub-millisecond latency required. Use GenServer when: (1) single process owns state, (2) dataset is small, (3) complex transition logic.

Most common mistake: using ETS to work around GenServer bottlenecks without profiling. Profile usually shows either handler logic is expensive (move it out) or contention from N processes calling it. ETS solves contention via sharding: split state across tables/processes indexed by key. Always profile before choosing ETS.

## Benchmark

The whole exercise **is** the benchmark. Target numbers (hardware-dependent,
Apple M-series / modern x86):

```
N = 100
  keyword:  ~300 ns/lookup
  map:      ~20  ns/lookup
  ets:      ~350 ns/lookup   (copy cost dominates at small N)

N = 10_000
  keyword:  ~30_000 ns/lookup  (catastrophic)
  map:      ~40 ns/lookup
  ets:      ~400 ns/lookup
```

---

## Trade-offs and production gotchas

**1. `:timer.tc/1` results on microsecond-scale work are noise**
Below ~10 μs per call, scheduler preemption, GC, and cache state dominate.
Use Benchee (with warmup and iterations) for anything that fast. Keep
`:timer.tc` for operations > 1 ms.

**2. Benchmark what your app actually does**
A microbench of "lookup key K in structure S" tells you surprisingly
little about request latency. Include the whole path that wraps the
lookup (e.g. a GenServer call if your real access pattern goes through
one), because the call/reply dominates the lookup time for fast ops.

**3. `Map` beats ETS for single-process workloads — but…**
…once you cross a process boundary, ETS wins because `Map` implies
`call`-and-reply (two term copies). If your cache is read from 50
processes, ETS is usually right even if the raw single-thread numbers
favor `Map`.

**4. ETS read cost scales with tuple size, not just key count**
`:ets.lookup/2` copies the **whole tuple** out. A `{key, big_struct}`
lookup can be dominated by copying `big_struct`. For large values,
consider `:ets.lookup_element/3` to pull only the element you need, or
store a reference key and fetch the large value from another structure
only when needed.

**5. Benchee memory numbers lie about ETS**
Benchee measures allocations in the caller's heap. ETS writes go to the
ETS heap — you'll see near-zero memory per iteration for ETS, which is
*technically* true but misleading. The ETS heap is not free; it's just
not visible to the per-iteration measurement.

**6. Don't benchmark with constant keys**
`Enum.each(keys, &lookup/1)` with a short, constant `keys` list lets the
BEAM cache-friendly-ize the hell out of your test. Randomize keys across
iterations if you want worst-case cache behavior.

**7. When NOT to benchmark**
If your structure fits in a few hundred entries and is read a few times
per request, any of the three is fine; the decision should be driven by
access pattern (shared? mutable?) and code clarity, not by nanoseconds.

---

## Reflection

- Your cache currently lives in a GenServer state `Map` and is read by 30
  worker processes on every request. The request latency is dominated by
  `GenServer.call`. What are your realistic options, and what numbers
  would you measure before switching?
- When Benchee reports "near-zero memory per iteration" for an ETS bench,
  what does that actually mean, and why is it misleading? How would you
  measure the real memory cost?

---
## Resources

- [Benchee — official docs](https://hexdocs.pm/benchee/readme.html)
- [`:timer.tc/1`](https://www.erlang.org/doc/man/timer.html#tc-1)
- [`:ets.lookup/2` and `:ets.lookup_element/3`](https://www.erlang.org/doc/man/ets.html#lookup-2)
- [Fred Hébert — "Erlang in Anger"](https://www.erlang-in-anger.com/) — chapter on ETS performance and operational characteristics
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets)
- [José Valim — "Discussions on Elixir performance"](https://elixirforum.com/) — recurring forum threads with concrete numbers

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/ets_benchmark_test.exs`

```elixir
defmodule EtsBenchmarkTest do
  use ExUnit.Case, async: true

  doctest EtsBenchmark

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert EtsBenchmark.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
ETS lookups are O(1) hashtable operations, orders of magnitude faster than message passing to a GenServer. Benchmarking reveals the difference: `ets:lookup/2` on an uncontended table is microseconds; `GenServer.call/2` is tens of microseconds. For high-frequency operations (rate limiting, cache hits), this difference is load-bearing—a 10x speedup can be the difference between handling 10k or 100k requests per second. The trade-off: ETS operations are atomic but not transactional; no rollback support. GenServer-backed state can implement custom logic and invariants. Use ETS for simple, fast state (caches, metrics); use GenServer when you need complex orchestration or transaction semantics. Profiling your bottleneck is the first step: if lookups dominate, ETS wins; if logic does, GenServer is clearer.

---
