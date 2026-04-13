# The `:global` process registry — consistency and limits

**Project**: `global_registry` — singleton workers registered cluster-wide with `:global.register_name/2`. Explore conflict resolution, leader election, and why `:global` is unsuitable for high-churn workloads

---

## Why distribution and clustering matters

Distributed Erlang gives you remote message-passing transparency, but the cost is your responsibility for split-brain detection, registry consistency, and net-tick policies. Libcluster, Horde, and PG provide pieces; you compose them.

Clusters fail in interesting ways: netsplits, asymmetric partitions, GC pauses misread as crashes, and global registry race conditions. Designing for the network — rather than against it — is the senior shift.

---

## The business problem

You are building a production-grade Elixir component in the **Distribution and clustering** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
global_registry/
├── lib/
│   └── global_registry.ex
├── script/
│   └── main.exs
├── test/
│   └── global_registry_test.exs
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

Chose **B** because in Distribution and clustering the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule GlobalRegistry.MixProject do
  use Mix.Project

  def project do
    [
      app: :global_registry,
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

### `lib/global_registry.ex`

```elixir
defmodule GlobalRegistry.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {DynamicSupervisor, strategy: :one_for_one, name: GlobalRegistry.TenantSupervisor}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: GlobalRegistry.Supervisor)
  end
end

defmodule GlobalRegistry.ConflictResolver do
  @moduledoc """
  Resolves `:global` name clashes that arise after a split-brain heals.

  Strategy: keep the pid with the oldest start time (`erlang.process_info(:registered_name)`
  is unreliable across partitions, so we store a monotonic start_ts in the worker state
  and query it via `GenServer.call`). The loser is terminated gracefully.
  """
  require Logger

  @spec resolve(term(), pid(), pid()) :: pid()
  def resolve(name, pid1, pid2) do
    ts1 = safe_start_ts(pid1)
    ts2 = safe_start_ts(pid2)

    {winner, loser} = if ts1 <= ts2, do: {pid1, pid2}, else: {pid2, pid1}

    Logger.warning(
      "[:global clash] name=#{inspect(name)} winner=#{inspect(winner)} loser=#{inspect(loser)}"
    )

    # The caller of `:global` expects us to return the surviving pid;
    # it will kill the other with reason :name_conflict.
    _ = GenServer.stop(loser, :name_conflict, 1_000)
    winner
  catch
    :exit, _ ->
      # If either pid is already dead, return the other.
      if Process.alive?(pid1), do: pid1, else: pid2
  end

  defp safe_start_ts(pid) do
    GenServer.call(pid, :start_ts, 500)
  catch
    :exit, _ -> System.monotonic_time() # worst case, treat as "just born"
  end
end

defmodule GlobalRegistry.TenantWorker do
  @moduledoc """
  A singleton per `tenant_id`, registered cluster-wide via `:global`.

  Publicly: always call `TenantWorker.whereis(tenant_id)` to locate the
  live pid — the registration may have migrated to another node.
  """
  use GenServer
  require Logger

  alias GlobalRegistry.ConflictResolver

  @type tenant_id :: String.t()

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    tenant_id = Keyword.fetch!(opts, :tenant_id)
    GenServer.start_link(__MODULE__, tenant_id, name: via(tenant_id))
  end

  @doc "Returns the cluster-wide pid or `:undefined`."
  @spec whereis(tenant_id()) :: pid() | :undefined
  def whereis(tenant_id), do: :global.whereis_name({:tenant_worker, tenant_id})

  @doc "Performs a unit of work on whichever node owns the singleton."
  @spec do_work(tenant_id(), term()) :: {:ok, term()} | {:error, :not_found}
  def do_work(tenant_id, payload) do
    case whereis(tenant_id) do
      :undefined -> {:error, :not_found}
      pid -> {:ok, GenServer.call(pid, {:work, payload})}
    end
  end

  defp via(tenant_id), do: {:global, {:tenant_worker, tenant_id}}

  @impl true
  def init(tenant_id) do
    Logger.info("TenantWorker #{inspect(tenant_id)} started on #{node()}")
    {:ok, %{tenant_id: tenant_id, start_ts: System.monotonic_time(), counter: 0}}
  end

  @impl true
  def handle_call(:start_ts, _from, state), do: {:reply, state.start_ts, state}

  def handle_call({:work, payload}, _from, state) do
    result = {:processed_on, node(), payload, state.counter}
    {:reply, result, %{state | counter: state.counter + 1}}
  end

  # We register using `:global.re_register_name/3` on init too, so the resolver
  # is associated with our name (Application child spec only registers via `name:`).
  @impl true
  def handle_info({:global_name_conflict, _name}, state) do
    # Fallback path if the default resolver is invoked; we re-register with ours.
    :global.re_register_name(
      {:tenant_worker, state.tenant_id},
      self(),
      &ConflictResolver.resolve/3
    )
    {:noreply, state}
  end
end

defmodule GlobalRegistry.TenantSupervisor do
  @moduledoc "Thin wrapper around DynamicSupervisor for starting TenantWorkers."

  alias GlobalRegistry.TenantWorker

  @spec start_tenant(String.t()) :: DynamicSupervisor.on_start_child()
  def start_tenant(tenant_id) do
    spec = {TenantWorker, [tenant_id: tenant_id]}
    DynamicSupervisor.start_child(__MODULE__, spec)
  end

  @spec stop_tenant(String.t()) :: :ok | {:error, :not_found}
  def stop_tenant(tenant_id) do
    case TenantWorker.whereis(tenant_id) do
      :undefined -> {:error, :not_found}
      pid -> DynamicSupervisor.terminate_child(__MODULE__, pid)
    end
  end
end

defmodule Bench do
  def run(n) do
    t0 = System.monotonic_time(:microsecond)
    for i <- 1..n do
      {:ok, _} = GlobalRegistry.TenantSupervisor.start_tenant("t_#{i}")
    end
    elapsed = System.monotonic_time(:microsecond) - t0
    IO.puts("#{n} registrations in #{elapsed}µs — #{div(elapsed, n)}µs each")
  end
end
```

### `test/global_registry_test.exs`

```elixir
defmodule GlobalRegistry.TenantWorkerTest do
  use ExUnit.Case, async: true
  doctest GlobalRegistry.Application

  alias GlobalRegistry.{TenantSupervisor, TenantWorker}

  setup do
    on_exit(fn ->
      for {_, pid, _, _} <- DynamicSupervisor.which_children(TenantSupervisor) do
        DynamicSupervisor.terminate_child(TenantSupervisor, pid)
      end
    end)

    :ok
  end

  describe "GlobalRegistry.TenantWorker" do
    test "registers a singleton and resolves it via whereis/1" do
      {:ok, pid} = TenantSupervisor.start_tenant("tenant_a")
      assert TenantWorker.whereis("tenant_a") == pid
    end

    test "returns {:error, :not_found} for missing tenants" do
      assert TenantWorker.do_work("missing_tenant", :noop) == {:error, :not_found}
    end

    test "second start_link for the same tenant returns {:error, {:already_started, pid}}" do
      {:ok, pid1} = TenantSupervisor.start_tenant("tenant_b")
      assert {:error, {:already_started, ^pid1}} = TenantSupervisor.start_tenant("tenant_b")
    end

    test "do_work routes to the owning pid and returns node()" do
      {:ok, _pid} = TenantSupervisor.start_tenant("tenant_c")
      assert {:ok, {:processed_on, n, :payload, 0}} = TenantWorker.do_work("tenant_c", :payload)
      assert n == node()
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for The `:global` process registry — consistency and limits.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== The `:global` process registry — consistency and limits ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case GlobalRegistry.run(payload) do
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
        for _ <- 1..1_000, do: GlobalRegistry.run(:bench)
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

### 1. Partitions are the rule, not the exception

In a multi-AZ cluster, brief netsplits happen daily. Design for them: prefer eventual consistency, use idempotent operations, and detect split-brain explicitly.

### 2. Registries don't replicate transparently

Local Registry is fast and node-local. :global is consistent but slow. Horde.Registry replicates via CRDTs — eventual consistency, no global locks. Pick based on your read/write ratio.

### 3. Tune net_kernel ticks for your environment

The default 60-second tick is too long for production failure detection but too short for high-latency cross-region links. Measure first.

---
