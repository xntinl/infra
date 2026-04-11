# Rigorous Benchmarking with Benchee

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. Three architectural decisions need data to resolve:

1. The router currently uses a linear scan through route patterns. A radix tree
   alternative was proposed. Which is faster for the gateway's actual route set?
2. The cache uses ETS direct lookup. A two-level L1/L2 design was added. What is
   the actual hit-rate vs latency trade-off under realistic access patterns?
3. The rate limiter has three counter implementations from exercise 20. Which
   performs best under the gateway's actual concurrency level (50 workers)?

All three need rigorous benchmarks — not "I ran it once and it seemed faster."

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── router.ex
│       ├── cache/
│       │   └── store.ex
│       ├── metrics/
│       │   └── sliding_window.ex
│       └── ...
├── bench/
│   ├── router_bench.exs            # ← you implement this
│   ├── cache_bench.exs             # ← and this
│   └── counter_bench.exs           # (from exercise 20, extend here)
└── mix.exs
```

Add Benchee to `mix.exs`:
```elixir
defp deps do
  [
    # ...
    {:benchee, "~> 1.3", only: [:dev, :bench]}
  ]
end
```

---

## The business problem

Benchmarks must answer specific questions, not just produce numbers. For each
benchmark, the team needs:
- A clear hypothesis ("the radix router is faster for > 50 routes")
- Inputs that represent real production patterns (not just a single case)
- Statistical validity (warm-up, multiple iterations, standard deviation reported)
- A conclusion with the data to back it

---

## Why microbenchmarking is difficult

```
Sources of noise in benchmarks:
├── JIT warm-up: BEAM interprets bytecode before native-compiling hot paths
├── GC pauses: a GC cycle during measurement inflates latency
├── CPU throttling: thermal throttling on laptops skews results
├── OS scheduling: the BEAM scheduler thread can be preempted by the OS
├── Branch predictor: CPU learns patterns in hot loops, inflating throughput
└── Cache effects: data accessed repeatedly moves to L1 cache vs RAM
```

Benchee mitigates these with:
- **Warm-up phase**: discarded iterations to let BEAM JIT and CPU caches settle
- **Multiple samples**: statistical mean, median, std_dev, p99 across many iterations
- **`inputs:`**: run the same benchmark across multiple input sizes to observe scaling

---

## Benchee API essentials

```elixir
Benchee.run(
  %{
    "scenario name" => fn -> code_to_benchmark() end,
    # With setup that runs before each measurement:
    "with setup" => fn input -> use(input) end
  },
  inputs: %{
    "small" => generate_small_input(),
    "large" => generate_large_input()
  },
  before_scenario: fn input ->
    # Runs once before each input/scenario combination
    # Use to set up state that should not be measured
    prepare(input)
  end,
  warmup: 2,        # seconds of warm-up (discarded)
  time: 10,         # seconds of measurement
  memory_time: 2,   # seconds of memory measurement (optional)
  parallel: 4,      # number of concurrent processes
  formatters: [Benchee.Formatters.Console]
)
```

**`before_scenario` vs `before_each`**:
- `before_scenario`: runs once per `{scenario, input}` combination, before the
  timed loop. Use for setup that is expensive and should not be measured.
- `before_each`: runs before *every single iteration*. Avoid unless the setup
  is intentionally part of the measurement.

---

## Implementation

### Step 1: `bench/router_bench.exs`

```elixir
# bench/router_bench.exs
# Benchmarks linear scan vs ETS-based route matching across route set sizes.

# --- Setup -------------------------------------------------------------------

# Builds a list of {method, pattern, handler} route definitions
build_routes = fn count ->
  for i <- 1..count do
    {"GET", "/api/resource_#{i}/:id", :"Handler#{i}"}
  end
end

# Linear scan matcher: iterate routes until a match is found
linear_match = fn routes, method, path ->
  Enum.find(routes, fn {m, pattern, _handler} ->
    m == method and String.starts_with?(path, String.replace(pattern, "/:id", "/"))
  end)
end

# ETS-based matcher: store routes in an ETS table, lookup by prefix
build_ets_table = fn routes ->
  table = :ets.new(:route_table, [:set, :public, {:read_concurrency, true}])
  for {method, pattern, handler} <- routes do
    prefix = String.replace(pattern, "/:id", "")
    :ets.insert(table, {{method, prefix}, handler})
  end
  table
end

ets_match = fn table, method, path ->
  # Simplified: strip the last segment to get the prefix
  prefix = path |> String.split("/") |> Enum.drop(-1) |> Enum.join("/")
  :ets.lookup(table, {method, prefix})
end

# --- Benchmark ---------------------------------------------------------------

Benchee.run(
  %{
    "linear_scan" => fn {routes, _table} ->
      linear_match.(routes, "GET", "/api/resource_25/42")
    end,
    "ets_lookup" => fn {_routes, table} ->
      ets_match.(table, "GET", "/api/resource_25/42")
    end
  },
  inputs: %{
    "10 routes" => build_routes.(10),
    "50 routes" => build_routes.(50),
    "200 routes" => build_routes.(200)
  },
  before_scenario: fn routes ->
    # Setup runs once per input — build both the list and the ETS table
    table = build_ets_table.(routes)
    {routes, table}
  end,
  warmup: 2,
  time: 8,
  formatters: [Benchee.Formatters.Console]
)
```

### Step 2: `bench/cache_bench.exs`

```elixir
# bench/cache_bench.exs
# Benchmarks single-level ETS cache vs L1/L2 two-level cache under
# varying cache hit rates.

alias ApiGateway.Cache.Store

# Setup: populate the cache with N entries.
# Store is started by the application supervision tree.
# flush/0 clears stale entries before each scenario.
populate_cache = fn count ->
  Store.flush()

  for i <- 1..count do
    Store.put("key_#{i}", "value_#{i}")
  end

  count
end

# Simulates a realistic access pattern: 80% of requests hit 20% of keys (Pareto)
pareto_key = fn count ->
  hot_key_count = max(1, div(count, 5))
  if :rand.uniform() < 0.8 do
    "key_#{:rand.uniform(hot_key_count)}"
  else
    "key_#{:rand.uniform(count)}"
  end
end

Benchee.run(
  %{
    "cache_get (pareto access)" => fn {_n, count} ->
      Store.get(pareto_key.(count))
    end,
    "cache_get_or_put (fetch on miss)" => fn {_count, count} ->
      key = pareto_key.(count)
      case Store.get(key) do
        {:ok, _value} -> :hit
        :miss -> Store.put(key, "fetched_value")
      end
    end
  },
  inputs: %{
    "100 entries" => 100,
    "10_000 entries" => 10_000
  },
  before_scenario: fn count ->
    n = populate_cache.(count)
    {n, count}
  end,
  warmup: 2,
  time: 8,
  parallel: 10,   # simulate concurrent cache access
  formatters: [Benchee.Formatters.Console]
)
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/bench/benchee_integration_test.exs
defmodule ApiGateway.BencheeIntegrationTest do
  @moduledoc """
  Smoke tests that verify benchmark scripts are syntactically valid and
  all referenced modules and functions exist before running the full benchmarks.
  """

  use ExUnit.Case, async: true

  # Verify modules referenced in benchmarks exist and export the expected functions
  describe "router benchmark dependencies" do
    test "ETS table creation works" do
      table = :ets.new(:test_routes, [:set, :public])
      :ets.insert(table, {{"GET", "/api/users"}, MyHandler})
      result = :ets.lookup(table, {"GET", "/api/users"})
      assert [{{"GET", "/api/users"}, MyHandler}] = result
      :ets.delete(table)
    end
  end

  describe "Benchee.run/2 basic invocation" do
    test "runs a minimal benchmark without error" do
      # Verify Benchee is available and the API works
      result =
        Benchee.run(
          %{"noop" => fn -> :ok end},
          warmup: 0,
          time: 0.01,
          formatters: []
        )

      assert %Benchee.Suite{} = result
    end

    test "inputs: option works correctly" do
      result =
        Benchee.run(
          %{"identity" => fn input -> input end},
          inputs: %{"small" => 1, "large" => 1_000},
          warmup: 0,
          time: 0.01,
          formatters: []
        )

      scenario_names = Enum.map(result.scenarios, & &1.name)
      assert "identity" in scenario_names
    end

    test "before_scenario: runs before each scenario" do
      test_pid = self()

      Benchee.run(
        %{"with_setup" => fn _input -> :ok end},
        inputs: %{"x" => 1},
        before_scenario: fn input ->
          send(test_pid, {:setup_ran, input})
          input
        end,
        warmup: 0,
        time: 0.01,
        formatters: []
      )

      assert_receive {:setup_ran, 1}, 2_000
    end

    test "parallel: option accepts integer" do
      result =
        Benchee.run(
          %{"parallel_noop" => fn -> :ok end},
          parallel: 2,
          warmup: 0,
          time: 0.01,
          formatters: []
        )

      assert %Benchee.Suite{} = result
    end

    test "memory_time: option measures memory" do
      result =
        Benchee.run(
          %{"list_alloc" => fn -> Enum.to_list(1..100) end},
          warmup: 0,
          time: 0.01,
          memory_time: 0.01,
          formatters: []
        )

      scenario = hd(result.scenarios)
      # memory_usage_data may be nil if no allocations happened, but the field exists
      assert Map.has_key?(scenario, :memory_usage_data)
    end
  end

  describe "statistical output" do
    test "Benchee.Suite contains scenario statistics" do
      suite =
        Benchee.run(
          %{"with_stats" => fn -> Enum.sum(1..1000) end},
          warmup: 0,
          time: 0.1,
          formatters: []
        )

      scenario = hd(suite.scenarios)
      stats = scenario.run_time_data.statistics

      assert stats.average > 0
      assert stats.median > 0
      assert stats.std_dev >= 0
      assert stats.sample_size > 0
    end
  end
end
```

### Step 4: Run the tests and benchmarks

```bash
# Unit tests (fast — verifies Benchee API and dependencies)
mix test test/api_gateway/bench/benchee_integration_test.exs --trace

# Router benchmark (takes ~30 seconds)
mix run bench/router_bench.exs

# Cache benchmark (takes ~30 seconds)
mix run bench/cache_bench.exs
```

---

## Reading Benchee output

```
Name                    ips        average  deviation         median         99th %
ets_lookup          4.23 M      236.64 ns   ±142.94%     210.00 ns      470.00 ns
linear_scan (10)    2.15 M      465.12 ns    ±89.23%     430.00 ns      890.00 ns
linear_scan (200)  48.32 K    20701.54 ns    ±31.45%   20200.00 ns    35400.00 ns
```

- **ips** (iterations per second): higher is better. Primary throughput metric.
- **average**: mean latency. Skewed by outliers — prefer median.
- **deviation**: `±X%` of the mean. High deviation (> 20%) means noisy measurement.
  Re-run with longer `time:` or check for GC interference.
- **median**: 50th percentile. Best single-number latency representation.
- **99th %**: tail latency. If this is 10x the median, there are occasional slow
  outliers (GC, OS scheduling) — check memory_time to confirm.

**Comparing scenarios**: Benchee prints comparison ratios:
```
Comparison:
ets_lookup           4.23 M
linear_scan (10)     2.15 M — 1.97x slower
linear_scan (200)   48.32 K — 87.5x slower
```

---

## Trade-off analysis

| Benchmark type | When to use | Pitfall |
|----------------|-------------|---------|
| `parallel: 1` (sequential) | Pure CPU performance, no contention | Doesn't reflect real concurrency |
| `parallel: N` (concurrent) | Throughput under concurrent load, lock contention | Results vary by hardware core count |
| `inputs:` scaling | To find O(n) vs O(1) behavior | Must use representative input sizes |
| `before_scenario:` | Setup that should not be measured | Do NOT use for state that varies per iteration |
| `memory_time:` | When memory allocation is part of the trade-off | Not all scenarios produce measurable allocation |

---

## Common production mistakes

**1. Benchmarking with `warmup: 0`**
The first iterations of a benchmark run interpreted bytecode before BEAM's JIT
compiles the hot path. Without warm-up, the first measurement samples include
slow interpreted execution, inflating the average. Always use at least 2 seconds
of warm-up for functions that will be called frequently in production.

**2. Benchmarking in `MIX_ENV=test` or `MIX_ENV=dev`**
Mix builds in dev/test include debug assertions, extra logging, and unoptimized
compilation. Always run production benchmarks with `MIX_ENV=prod mix run bench/...`.
The difference can be 2–5x for CPU-bound code.

**3. Using `before_each:` for expensive setup**
`before_each` runs before *every single iteration* — potentially millions of times.
Use `before_scenario` for anything that takes > 1µs. `before_each` should only be
used when the setup is truly per-iteration (e.g., generating a unique random key
that must not be cached).

**4. Drawing conclusions from a single run on a laptop**
Thermal throttling, background processes, and OS scheduling make single-run results
unreliable. Run benchmarks at least 3 times and compare medians, not averages.
On CI infrastructure, pin CPU frequency (`cpupower frequency-set`) for reproducible results.

**5. Benchmarking the wrong thing**
A benchmark that measures "how fast is the router?" actually measures
"how fast is the router, the ETS table, and the process scheduler together in
this specific configuration on this machine." Make sure the benchmark reflects
the actual bottleneck, not a neighboring system component.

---

## Resources

- [Benchee documentation](https://github.com/bencheeorg/benchee) — comprehensive guide with `inputs`, `before_scenario`, formatters
- [Benchee formatters](https://github.com/bencheeorg/benchee#formatters) — HTML, CSV, and JSON output for storing historical results
- [Saša Jurić — "Benchmarking Elixir" (ElixirConf 2019)](https://www.youtube.com/watch?v=7-mE5CKXjkw) — practical benchmarking methodology
- [`:timer.tc/1` — Erlang docs](https://www.erlang.org/doc/man/timer.html#tc-1) — manual microsecond timing for one-off measurements
- [Elixir Forum: Benchee best practices](https://elixirforum.com/t/benchmarking-best-practices/45218) — community discussion on avoiding pitfalls
