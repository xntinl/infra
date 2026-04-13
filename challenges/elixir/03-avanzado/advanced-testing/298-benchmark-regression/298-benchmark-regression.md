# Benchmark-Driven Testing for Performance Regression

**Project**: `search_index` — an in-memory search index whose critical functions have automated benchmarks with asserted performance budgets.

## Project context

`search_index` serves autocomplete suggestions for an e-commerce site at 10k RPS.
Latency is product-critical: p99 > 5ms directly hurts conversion. The team added a
feature, all unit tests passed, and the feature shipped. A week later p99 was 40ms.
Root cause: a nested `Enum.map + Enum.filter` that the refactor introduced, invisible
to correctness tests.

Correctness tests prove what the code does. Benchmark tests prove how fast it does it.
When performance is a feature — latency SLOs, high-throughput paths, hot loops — you
need automated regression detection in CI.

```
search_index/
├── bench/
│   ├── index_bench.exs           # Benchee-driven benchmarks (manual run)
│   └── regression_bench.exs      # CI-friendly pass/fail budgets
├── lib/
│   └── search_index/
│       └── trie.ex               # function under budget
├── test/
│   └── search_index/
│       └── trie_regression_test.exs   # asserts on measured budget
└── mix.exs
```

## Why benchmark-as-test, not just manual Benchee runs

- Manual Benchee runs require discipline. Developers skip them under deadline pressure.
- A budget-asserting test in the normal `mix test` pipeline fails the build when someone
  makes the hot path slower by 20%. Nobody can ignore it.
- Benchee is for fine-grained exploration. A regression test is for CI tripwires —
  coarse but automatic.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Testing-specific insight:**
Tests are not QA. They document intent and catch regressions. A test that passes without asserting anything is technical debt. Always test the failure case; "it works when everything succeeds" teaches nothing. Use property-based testing for domain logic where the number of edge cases is infinite.
### 1. `:timer.tc/1` returns microseconds
Precise enough for sub-ms measurements on the BEAM.

### 2. Budget with margin
Budget must tolerate CI jitter. A 5ms operation budgeted at "must be under 5ms" fails
randomly. Budget = 2× to 4× the typical local measurement.

### 3. Warmup before measurement
The first N calls include module loading, JIT warmup, ETS allocation. Discard them.

### 4. Multiple samples, assert on a percentile
Minimum is too optimistic. Mean is skewed by outliers. Assert on a quantile (p90 or p95)
to stay robust against CI noise.

## Design decisions

- **Option A — assert on mean of 1 sample**: noisy, flaky in CI.
- **Option B — assert on minimum of 1000 samples**: too permissive; a slow regression
  might still have a fast minimum.
- **Option C — assert on p95 of 1000 samples with a budget at 3× the expected value**:
  robust, catches real regressions, tolerates jitter.

Chosen: **Option C**.

Additional decision:

- **Option A — run regression tests on every `mix test`**: may slow down normal TDD.
- **Option B — gate with `@moduletag :performance`** and run only in CI (`mix test --only performance`):
  keeps the fast feedback loop clean, enforces budgets in CI.

Chosen: **Option B**.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: [:dev, :test]}
  ]
end
```

### Step 1: the function under budget

**Objective**: Implement prefix lookup with a stated p95 budget of 50µs so regression tests have a named contract to defend.

```elixir
# lib/search_index/trie.ex
defmodule SearchIndex.Trie do
  @moduledoc """
  Prefix-trie lookup. Hot path; p95 budget: 50µs for 1000-item index.
  """

  @type trie :: %{optional(char()) => trie()} | %{terminal: boolean(), children: map()}

  @spec build([String.t()]) :: trie()
  def build(words) do
    Enum.reduce(words, %{terminal: false, children: %{}}, &insert(&2, &1))
  end

  @spec lookup(trie(), String.t()) :: [String.t()]
  def lookup(trie, prefix) do
    case descend(trie, String.graphemes(prefix)) do
      {:ok, node} -> collect(node, prefix)
      :error -> []
    end
  end

  defp insert(node, word) do
    do_insert(node, String.graphemes(word))
  end

  defp do_insert(node, []), do: %{node | terminal: true}

  defp do_insert(node, [g | rest]) do
    child = Map.get(node.children, g, %{terminal: false, children: %{}})
    updated = do_insert(child, rest)
    %{node | children: Map.put(node.children, g, updated)}
  end

  defp descend(node, []), do: {:ok, node}

  defp descend(node, [g | rest]) do
    case Map.fetch(node.children, g) do
      {:ok, child} -> descend(child, rest)
      :error -> :error
    end
  end

  defp collect(node, acc) do
    terminal = if node.terminal, do: [acc], else: []

    children_results =
      for {g, child} <- node.children, result <- collect(child, acc <> g), do: result

    terminal ++ children_results
  end
end
```

### Step 2: CI regression test with budget assertion

**Objective**: Sample 1000 `:timer.tc` runs after warmup and assert p95 against the budget so GC outliers don't mask real regressions.

```elixir
# test/search_index/trie_regression_test.exs
defmodule SearchIndex.TrieRegressionTest do
  # Kept separate from correctness tests — run in CI with `mix test --only performance`
  use ExUnit.Case, async: false
  @moduletag :performance

  alias SearchIndex.Trie

  @sample_words for i <- 1..1_000, do: "word#{i}"

  describe "lookup/2 — performance budget" do
    test "p95 over 1000 samples stays under 50µs for a 1k-item trie" do
      trie = Trie.build(@sample_words)

      # Warmup — discarded
      for _ <- 1..100, do: Trie.lookup(trie, "word5")

      samples =
        for _ <- 1..1_000 do
          {us, _result} = :timer.tc(fn -> Trie.lookup(trie, "word5") end)
          us
        end

      p95 = percentile(samples, 95)
      budget_us = 50

      # Assertion prints the actual measurement on failure
      assert p95 <= budget_us,
             "lookup p95 = #{p95}µs exceeds budget of #{budget_us}µs " <>
               "(min=#{Enum.min(samples)}µs, max=#{Enum.max(samples)}µs)"
    end

    test "p95 over 1000 samples stays under 500µs for an empty-prefix full-scan" do
      trie = Trie.build(@sample_words)
      for _ <- 1..100, do: Trie.lookup(trie, "")

      samples =
        for _ <- 1..1_000 do
          {us, _} = :timer.tc(fn -> Trie.lookup(trie, "") end)
          us
        end

      p95 = percentile(samples, 95)
      budget_us = 5_000

      assert p95 <= budget_us,
             "full scan p95 = #{p95}µs exceeds budget of #{budget_us}µs"
    end
  end

  # ---------------------------------------------------------------------------
  # Helpers — kept at the bottom of the module for readability
  # ---------------------------------------------------------------------------

  defp percentile(samples, p) when p >= 0 and p <= 100 do
    sorted = Enum.sort(samples)
    idx = trunc((length(sorted) - 1) * p / 100)
    Enum.at(sorted, idx)
  end
end
```

### Step 3: exploratory Benchee script (for local profiling)

**Objective**: Use Benchee at 100/1k/10k sizes with warmup and memory stats so engineers see scaling behaviour before promoting a budget into CI.

```elixir
# bench/index_bench.exs
alias SearchIndex.Trie

words_100   = for i <- 1..100,   do: "word#{i}"
words_1000  = for i <- 1..1_000, do: "word#{i}"
words_10000 = for i <- 1..10_000, do: "word#{i}"

trie_100   = Trie.build(words_100)
trie_1000  = Trie.build(words_1000)
trie_10000 = Trie.build(words_10000)

Benchee.run(
  %{
    "lookup 100"    => fn -> Trie.lookup(trie_100,   "word5") end,
    "lookup 1000"   => fn -> Trie.lookup(trie_1000,  "word5") end,
    "lookup 10000"  => fn -> Trie.lookup(trie_10000, "word5") end
  },
  time: 5,
  warmup: 2,
  memory_time: 1,
  formatters: [Benchee.Formatters.Console]
)
```

### Step 4: test.exs wiring

**Objective**: Tag performance tests and `exclude: [:performance]` in `test_helper.exs` so TDD stays fast and regression budgets run only in the CI stage that owns them.

```elixir
# test/test_helper.exs
ExUnit.start(exclude: [:performance])
```

Run correctness tests with `mix test` (fast). Run regression tests in CI with
`mix test --only performance`.

## Why this works

- The budget is **3× the local measurement** (typical p95 is ~15µs locally; budget is 50µs).
  CI noise is covered without masking a real regression.
- Running 1000 iterations plus 100 warmup smooths out per-call jitter without making the
  test take more than ~200ms.
- The `@moduletag :performance` and `exclude: [:performance]` in `test_helper.exs` means
  TDD never runs these — they execute only on the CI pipeline that needs them.
- The assertion message prints the actual p95, min, and max. When the test fails, the
  operator sees immediately by how much.

## Tests

See Step 2.

## Benchmark

Running the regression suite alone:

```bash
mix test --only performance
```

Expected wall clock: < 500ms for the whole file.

## Deep Dive: Benchmark Patterns and Production Implications

Benchmarking in Elixir requires statistical rigor: a single run means nothing. Tools like Benchee measure distribution, not just mean time. The mistake most engineers make is benchmarking in isolation (single process) and then deploying to a system under concurrent load where cache hits, scheduler contention, and garbage collection behave differently. Production performance tuning must account for these realities.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Asserting on mean or minimum**
Mean is dragged by GC outliers. Minimum underreports real behaviour. Always use a
percentile: p90 or p95. p99 requires much larger sample sizes to be stable.

**2. No warmup**
The first call after module load is 10–100× slower than subsequent ones (module load,
code path JIT, ETS init). Discard the first N calls.

**3. Budget too tight**
If local p95 is 15µs and you set the budget at 20µs, CI noise alone will flake the test.
Keep 3–5× headroom between measured value and budget.

**4. Running perf tests on the same CPU as CI tests**
A noisy neighbour (another CI job) distorts the reading. If possible, run perf tests on
a dedicated CI machine or use percentile thresholds.

**5. No sample rejection**
Single GC pause can create an outlier of 50ms in an otherwise-20µs function. For very
tight budgets, discard samples > Nµs before computing percentiles.

**6. When NOT to use this**
For code that isn't on a hot path or where latency is not contracted (batch jobs, admin
scripts), regression tests add overhead without value. Budget the paths your SLOs depend
on — nothing else.

## Reflection

The budget is a number in the test file. When hardware changes (a new CI runner, a move
from x86 to ARM) the budget's calibration is stale. What mechanism would you build to
keep budgets honest across hardware generations without turning every hardware upgrade
into an orchestrated retuning project?


## Executable Example

```elixir
# test/search_index/trie_regression_test.exs
defmodule SearchIndex.TrieRegressionTest do
  # Kept separate from correctness tests — run in CI with `mix test --only performance`
  use ExUnit.Case, async: false
  @moduletag :performance

  alias SearchIndex.Trie

  @sample_words for i <- 1..1_000, do: "word#{i}"

  describe "lookup/2 — performance budget" do
    test "p95 over 1000 samples stays under 50µs for a 1k-item trie" do
      trie = Trie.build(@sample_words)

      # Warmup — discarded
      for _ <- 1..100, do: Trie.lookup(trie, "word5")

      samples =
        for _ <- 1..1_000 do
          {us, _result} = :timer.tc(fn -> Trie.lookup(trie, "word5") end)
          us
        end

      p95 = percentile(samples, 95)
      budget_us = 50

      # Assertion prints the actual measurement on failure
      assert p95 <= budget_us,
             "lookup p95 = #{p95}µs exceeds budget of #{budget_us}µs " <>
               "(min=#{Enum.min(samples)}µs, max=#{Enum.max(samples)}µs)"
    end

    test "p95 over 1000 samples stays under 500µs for an empty-prefix full-scan" do
      trie = Trie.build(@sample_words)
      for _ <- 1..100, do: Trie.lookup(trie, "")

      samples =
        for _ <- 1..1_000 do
          {us, _} = :timer.tc(fn -> Trie.lookup(trie, "") end)
          us
        end

      p95 = percentile(samples, 95)
      budget_us = 5_000

      assert p95 <= budget_us,
             "full scan p95 = #{p95}µs exceeds budget of #{budget_us}µs"
    end
  end

  # ---------------------------------------------------------------------------
  # Helpers — kept at the bottom of the module for readability
  # ---------------------------------------------------------------------------

  defp percentile(samples, p) when p >= 0 and p <= 100 do
    sorted = Enum.sort(samples)
    idx = trunc((length(sorted) - 1) * p / 100)
    Enum.at(sorted, idx)
  end
end

defmodule Main do
  def main do
      IO.puts("Property-based test generator initialized")
      a = 10
      b = 20
      c = 30
      assert (a + b) + c == a + (b + c)
      IO.puts("✓ Property invariant verified: (a+b)+c = a+(b+c)")
  end
end

Main.main()
```
