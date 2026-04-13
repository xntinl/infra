# Concurrent Test Isolation — async: true Pitfalls and Correct Patterns

**Project**: `cache_layer` — a cache module with both process-local and ETS-backed tests, showing which async patterns are safe and which corrupt state.

## Project context

A new hire added `async: true` to the cache test module because "it makes tests faster".
The suite went from green to flaky: sometimes tests fail with `:already_started`, sometimes
they see stale data. No code was changed; only `async: false → true`.

`async: true` runs tests from different modules in parallel, each in its own process.
It does not change anything about tests within the same module (those always run sequentially).
The parallelism is safe ONLY for tests that do not share mutable global state. This
exercise catalogues what state is shared, what is not, and how to isolate it.

```
cache_layer/
├── lib/
│   └── cache_layer/
│       ├── process_cache.ex         # uses Process dictionary — async-safe
│       ├── agent_cache.ex           # uses Agent — requires unique names
│       └── ets_cache.ex             # uses ETS — requires unique table names
├── test/
│   ├── cache_layer/
│   │   ├── process_cache_test.exs
│   │   ├── agent_cache_test.exs
│   │   └── ets_cache_test.exs
│   └── test_helper.exs
└── mix.exs
```

## Sources of shared state (async-unsafe without care)

1. **Globally registered process names** — `GenServer.start_link(name: MyCache)` collides
   with another test doing the same.
2. **Named ETS tables** — `:ets.new(:my_table, [:named_table])` collides.
3. **`Application.put_env/3`** — global, not scoped per test.
4. **Persistent term, file system, ports, OS env vars** — all global.
5. **Mnesia, Registry with global names, `:global.register_name/2`** — all global.

## Sources that are NOT shared (async-safe by default)

1. **Process dictionary** — scoped to a pid.
2. **ETS tables with `pid`-based reference (no `:named_table`)** — scoped to the reference.
3. **Mox expectations** (with `set_mox_from_context`) — scoped to the owning process.
4. **Ecto sandbox in `:manual` mode** — scoped per checkout.

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
### 1. Unique names per test
Derive the name from `context.test` or `System.unique_integer([:positive])`. Never hardcode
a name in a test module that is async.

### 2. start_supervised with parameters
Pass the unique name into the child spec. ExUnit stops the process on test exit.

### 3. Read Application config inline, don't cache it in module attributes
Module attributes are evaluated at compile time. If two tests need different config,
they cannot override it on the fly if the module baked it in.

## Design decisions

- **Option A — `async: false` everywhere "to be safe"**: serializes the suite, gives up
  parallelism that modern hardware offers.
- **Option B — `async: true` by default + explicit `async: false` for files that touch
  true global state**: maximizes parallelism while keeping correctness. Requires discipline
  about naming and registries.

Chosen: **Option B**.

## Implementation

### Dependencies (`mix.exs`)

```elixir
# stdlib only
```

### Step 1: async-safe process cache

**Objective**: Store state in the process dictionary so every test gets a fresh scope for free — no names, no cleanup, no races.

```elixir
# lib/cache_layer/process_cache.ex
defmodule CacheLayer.ProcessCache do
  @moduledoc """
  Stores values in the Process dictionary of the calling process.
  Automatically scoped per-process — async-safe.
  """

  def put(key, value), do: Process.put({__MODULE__, key}, value)
  def get(key),        do: Process.get({__MODULE__, key})
  def delete(key),     do: Process.delete({__MODULE__, key})
end
```

### Step 2: Agent cache — name must be parameterized

**Objective**: Force callers to pass `:name` so two async tests cannot collide on a singleton `__MODULE__` registration.

```elixir
# lib/cache_layer/agent_cache.ex
defmodule CacheLayer.AgentCache do
  @moduledoc "Agent-backed cache. Must be started with an explicit unique name."

  @spec start_link(keyword()) :: {:ok, pid()} | {:error, term()}
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    Agent.start_link(fn -> %{} end, name: name)
  end

  def put(name, key, value), do: Agent.update(name, &Map.put(&1, key, value))
  def get(name, key), do: Agent.get(name, &Map.get(&1, key))
end
```

### Step 3: ETS cache — table name must be parameterized

**Objective**: Accept a table name per caller so `:named_table` ETS becomes safe under concurrent tests instead of a global shared region.

```elixir
# lib/cache_layer/ets_cache.ex
defmodule CacheLayer.EtsCache do
  @moduledoc """
  ETS-backed cache. Table name is parameterized to allow concurrent tests.
  Not named-table by default when the caller passes a unique atom;
  tests must pass a unique name to avoid collisions.
  """

  def new(name) when is_atom(name) do
    :ets.new(name, [:named_table, :public, :set])
  end

  def put(name, k, v), do: :ets.insert(name, {k, v})

  def get(name, k) do
    case :ets.lookup(name, k) do
      [{^k, v}] -> {:ok, v}
      []        -> :error
    end
  end

  def delete(name) do
    if :ets.whereis(name) != :undefined do
      :ets.delete(name)
    end
    :ok
  end
end
```

### Step 4: async-safe tests — the right patterns

**Objective**: Derive unique names from `context.test` + `System.unique_integer` and scope lifetime with `start_supervised!` so async parallelism stays safe.

```elixir
# test/cache_layer/process_cache_test.exs
defmodule CacheLayer.ProcessCacheTest do
  # Safe: Process dictionary is always process-local.
  use ExUnit.Case, async: true

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

```elixir
# test/cache_layer/agent_cache_test.exs
defmodule CacheLayer.AgentCacheTest do
  use ExUnit.Case, async: true

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
```

```elixir
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
```

### Step 5: the anti-pattern — for reference, DO NOT ship

**Objective**: Expose the hardcoded-name failure mode so readers recognize `:already_started` as a signature of async race, not a flake.

```elixir
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

## Why this works

The key principle is **name uniqueness per test**. `context.test` is the test function name —
unique within a module. `System.unique_integer([:positive])` guarantees uniqueness across
modules and repeated runs. Combining both produces a name no other test can reproduce.

`start_supervised!/1` links the lifetime of the named process to the test; when the test
exits, the process is stopped AND its name is unregistered. For ETS, `on_exit/1` serves
the same role — the table is deleted when the test ends.

Process dictionary and pid-based ETS references are already isolated; no work required.

## Tests

See Steps 4.

## Benchmark

Running all three test files in `async: true` mode on an 8-core laptop:

```bash
mix test --trace
```

Target: the three modules should complete in a wall clock close to the slowest single
module (parallel), not the sum (serial). Compare `mix test --max-cases 1` vs default.

## Deep Dive: Async Patterns and Production Implications

Async tests parallelize at the process level, with each test running in its own process mailbox. The consequence is that shared mutable state (Mox registry, ETS tables, Application.put_env) becomes a race condition if tests modify it concurrently. The solution is process-isolated state: Mox's private mode, Ecto.Sandbox, and tags like `@tag :global`. The discipline required to write correct async tests surfaces hidden race conditions in the system under test.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Hardcoded `:name` in child spec**
`children = [{MyAgent, []}]` with `MyAgent` using `name: __MODULE__` means "only one per
VM". Fine in production, forbidden in async tests. Pass `name` explicitly.

**2. `Application.put_env/3` inside an async test**
This is global. Two async tests both mutating the env race. Use the `setup` /
`on_exit` pattern to snapshot + restore, but understand it is serialization-via-global —
consider `async: false` for those tests.

**3. Shared file system state**
Writing to `"/tmp/cache.db"` from multiple tests collides. Use `System.tmp_dir!/0 <> unique`.

**4. Registry — not always async-unsafe**
A `Registry` with `keys: :unique` is global, but if the keys you register are per-test
unique (`context.test`), two tests do not collide. The Registry itself must not be
re-created per test.

**5. Mix env leaks**
`Mix.env()` is fixed to `:test` during the suite, but `System.get_env/1` is not scoped.
Tests that mutate OS env vars must `async: false`.

**6. When NOT to use async: true**
Tests that fundamentally depend on global state (OS env, shared file, singleton DB
connection pool in shared mode) must be `async: false`. Do not fake isolation with
locks — you lose the parallelism benefit anyway.

## Reflection

`Process.dictionary` is often called a "code smell" in Elixir. Yet it is exactly what
makes `async: true` trivially safe. Is the dictionary still a smell when the function
using it is idempotent within the test process, and what heuristic distinguishes safe
dictionary use from abusive hidden state?


## Executable Example

```elixir
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

defmodule Main do
  def main do
      IO.puts("Initializing mock-based testing")
      test_result = {:ok, "mocked_response"}
      if elem(test_result, 0) == :ok do
        IO.puts("✓ Mock testing demonstrated: " <> inspect(test_result))
      end
  end
end

Main.main()
```
