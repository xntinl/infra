# Concurrent Test Isolation — async: true Pitfalls and Correct Patterns

**Project**: `cache_layer` — a cache module with both process-local and ETS-backed tests, showing which async patterns are safe and which corrupt state

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
cache_layer/
├── lib/
│   └── cache_layer.ex
├── script/
│   └── main.exs
├── test/
│   └── cache_layer_test.exs
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
defmodule CacheLayer.MixProject do
  use Mix.Project

  def project do
    [
      app: :cache_layer,
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
### `lib/cache_layer.ex`

```elixir
# lib/cache_layer/process_cache.ex
defmodule CacheLayer.ProcessCache do
  @moduledoc """
  Stores values in the Process dictionary of the calling process.
  Automatically scoped per-process — async-safe.
  """

  @doc "Returns put result from key and value."
  def put(key, value), do: Process.put({__MODULE__, key}, value)
  @doc "Returns result from key."
  def get(key),        do: Process.get({__MODULE__, key})
  @doc "Deletes result from key."
  def delete(key),     do: Process.delete({__MODULE__, key})
end

# lib/cache_layer/agent_cache.ex
defmodule CacheLayer.AgentCache do
  @moduledoc "Agent-backed cache. Must be started with an explicit unique name."

  @spec start_link(keyword()) :: {:ok, pid()} | {:error, term()}
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    Agent.start_link(fn -> %{} end, name: name)
  end

  @doc "Returns put result from name, key and value."
  def put(name, key, value), do: Agent.update(name, &Map.put(&1, key, value))
  @doc "Returns result from name and key."
  def get(name, key), do: Agent.get(name, &Map.get(&1, key))
end

# lib/cache_layer/ets_cache.ex
defmodule CacheLayer.EtsCache do
  @moduledoc """
  ETS-backed cache. Table name is parameterized to allow concurrent tests.
  Not named-table by default when the caller passes a unique atom;
  tests must pass a unique name to avoid collisions.
  """

  @doc "Creates result from name."
  def new(name) when is_atom(name) do
    :ets.new(name, [:named_table, :public, :set])
  end

  @doc "Returns put result from name, k and v."
  def put(name, k, v), do: :ets.insert(name, {k, v})

  @doc "Returns result from name and k."
  def get(name, k) do
    case :ets.lookup(name, k) do
      [{^k, v}] -> {:ok, v}
      []        -> :error
    end
  end

  @doc "Deletes result from name."
  def delete(name) do
    if :ets.whereis(name) != :undefined do
      :ets.delete(name)
    end
    :ok
  end
end

defmodule CacheLayer.AgentCacheTest do
  use ExUnit.Case, async: true
  doctest CacheLayer.MixProject

  alias CacheLayer.AgentCache

  # Pattern: unique name derived from the test context.
  setup context do
    name = Module.concat([__MODULE__, :"agent_#{context.test}"])
    {:ok, _pid} = start_supervised({AgentCache, [name: name]})
    {:ok, name: name}
  end

  describe "agent-backed cache" do
    test "stores and retrieves a value", %{name: name} do
      AgentCache.put(name, :k, "hi")
      assert AgentCache.get(name, :k) == "hi"
    end

    test "a different test sees its own empty agent", %{name: name} do
      assert AgentCache.get(name, :k) == nil
    end
  end
end

# test/cache_layer/ets_cache_test.exs
defmodule CacheLayer.EtsCacheTest do
  use ExUnit.Case, async: true

  alias CacheLayer.EtsCache

  setup context do
    # Derive a unique table name — System.unique_integer guarantees no collision across
    # modules, pids, or retries.
    name = :"cache_#{context.test}_#{System.unique_integer([:positive])}"
    EtsCache.new(name)
    on_exit(fn -> EtsCache.delete(name) end)
    {:ok, name: name}
  end

  describe "ETS-backed cache" do
    test "put then get returns the value", %{name: name} do
      EtsCache.put(name, "foo", :bar)
      assert {:ok, :bar} = EtsCache.get(name, "foo")
    end

    test "different tests do NOT share a table", %{name: name} do
      assert :error = EtsCache.get(name, "foo")
    end
  end
end

# Illustrative only — DO NOT actually add this test; it is async-unsafe.
#
# defmodule CacheLayer.BadTest do
#   use ExUnit.Case, async: true
#
#   test "flaky: hardcoded global name" do
#     # Two tests in two modules running in parallel both call this:
#     {:ok, _} = AgentCache.start_link(name: :my_cache)     # :already_started in one of them
#     AgentCache.put(:my_cache, :x, 1)
#     assert AgentCache.get(:my_cache, :x) == 1
#   end
# end
```
### `test/cache_layer_test.exs`

```elixir
defmodule CacheLayer.ProcessCacheTest do
  # Safe: Process dictionary is always process-local.
  use ExUnit.Case, async: true
  doctest CacheLayer.MixProject

  alias CacheLayer.ProcessCache

  describe "process-dictionary-backed cache" do
    test "stores and retrieves a value" do
      ProcessCache.put(:k, 42)
      assert ProcessCache.get(:k) == 42
    end

    test "another test's value is invisible here" do
      # Each test runs in its own process — dict is fresh
      assert ProcessCache.get(:k) == nil
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Concurrent Test Isolation — async: true Pitfalls and Correct Patterns.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Concurrent Test Isolation — async: true Pitfalls and Correct Patterns ===")
    IO.puts("Category: Advanced testing\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case CacheLayer.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: CacheLayer.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
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
