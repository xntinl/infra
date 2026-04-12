# Benchmarking lookups — ETS vs `Map` vs `Keyword`

**Project**: `ets_benchmark` — measure point-lookup latency across three
in-memory data structures with `:timer.tc/1` and Benchee, then interpret
the results honestly.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

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

Project structure:

```
ets_benchmark/
├── lib/
│   └── ets_benchmark.ex
├── test/
│   └── ets_benchmark_test.exs
├── bench/
│   └── lookup_bench.exs
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

## Implementation

### Step 1: Create the project

```bash
mix new ets_benchmark
cd ets_benchmark
```

Add to `mix.exs`:

```elixir
defp deps do
  [{:benchee, "~> 1.3", only: :dev}]
end
```

Then `mix deps.get`.

### Step 2: `lib/ets_benchmark.ex`

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

```elixir
defmodule EtsBenchmarkTest do
  use ExUnit.Case, async: true

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

```bash
mix test
mix run bench/lookup_bench.exs
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

## Resources

- [Benchee — official docs](https://hexdocs.pm/benchee/readme.html)
- [`:timer.tc/1`](https://www.erlang.org/doc/man/timer.html#tc-1)
- [`:ets.lookup/2` and `:ets.lookup_element/3`](https://www.erlang.org/doc/man/ets.html#lookup-2)
- [Fred Hébert — "Erlang in Anger"](https://www.erlang-in-anger.com/) — chapter on ETS performance and operational characteristics
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets)
- [José Valim — "Discussions on Elixir performance"](https://elixirforum.com/) — recurring forum threads with concrete numbers
