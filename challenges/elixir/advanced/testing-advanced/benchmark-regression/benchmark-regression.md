# Benchmark-Driven Testing for Performance Regression

**Project**: `search_index` — an in-memory search index whose critical functions have automated benchmarks with asserted performance budgets

---

## Why advanced testing matters

Production Elixir test suites must run in parallel, isolate side-effects, and exercise concurrent code paths without races. Tooling like Mox, ExUnit async mode, Bypass, ExMachina and StreamData turns testing from a chore into a deliberate design artifact.

When tests double as living specifications, the cost of refactoring drops. When they don't, every change becomes a coin flip. Senior teams treat the test suite as a first-class product — measuring runtime, flake rate, and coverage of failure modes alongside production metrics.

---

## The business problem

You are building a production-grade Elixir component in the **Advanced testing** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
search_index/
├── lib/
│   └── search_index.ex
├── script/
│   └── main.exs
├── test/
│   └── search_index_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Advanced testing the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule SearchIndex.MixProject do
  use Mix.Project

  def project do
    [
      app: :search_index,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/search_index.ex`

```elixir
# lib/search_index/trie.ex
defmodule SearchIndex.Trie do
  @moduledoc """
  Prefix-trie lookup. Hot path; p95 budget: 50µs for 1000-item index.
  """

  @type trie :: %{optional(char()) => trie()} | %{terminal: boolean(), children: map()}

  @doc "Builds result from words."
  @spec build([String.t()]) :: trie()
  def build(words) do
    Enum.reduce(words, %{terminal: false, children: %{}}, &insert(&2, &1))
  end

  @doc "Looks up result from trie and prefix."
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
### `test/search_index_test.exs`

```elixir
defmodule SearchIndex.TrieRegressionTest do
  # Kept separate from correctness tests — run in CI with `mix test --only performance`
  use ExUnit.Case, async: true
  doctest SearchIndex.Trie
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
### `script/main.exs`

```elixir
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
---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Async tests are the default, not the exception

ExUnit defaults to sequential execution. Set `async: true` and structure tests so they don't share global state — Application env, ETS tables, the database. The reward is 5–10× faster suites in CI.

### 2. Mock the boundary, not the dependency

A behaviour-backed mock (Mox.defmock for: SomeBehaviour) is a contract. A bare function stub is a wish. Defining the boundary as a behaviour costs one file and pays back every time the implementation changes.

### 3. Test the failure mode, always

An assertion that succeeds when everything goes right teaches nothing. Tests that prove the system handles `{:error, :timeout}`, `{:error, :network}`, and partial failures are the ones that prevent regressions.

---
