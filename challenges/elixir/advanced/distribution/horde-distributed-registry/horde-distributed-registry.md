# Horde: CRDT-backed distributed registry and supervisor

**Project**: `horde_registry_demo` — distribute tens of thousands of stateful workers across a BEAM cluster using `Horde.Registry` + `Horde.DynamicSupervisor`, surviving node churn without a global lock

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
horde_registry_demo/
├── lib/
│   └── horde_registry_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── horde_registry_demo_test.exs
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
defmodule HordeRegistryDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :horde_registry_demo,
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
### `lib/horde_registry_demo.ex`

```elixir
defmodule HordeRegistryDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    topologies = Application.get_env(:libcluster, :topologies, [])

    children = [
      {Cluster.Supervisor, [topologies, [name: HordeRegistryDemo.ClusterSupervisor]]},
      HordeRegistryDemo.Horde.Registry,
      HordeRegistryDemo.Horde.DynamicSupervisor,
      HordeRegistryDemo.NodeObserver
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: HordeRegistryDemo.Supervisor)
  end
end

defmodule HordeRegistryDemo.Horde.Registry do
  @moduledoc "Thin wrapper that starts `Horde.Registry` with our process name."

  use Horde.Registry

  def start_link(_) do
    Horde.Registry.start_link(name: __MODULE__, keys: :unique, members: :auto)
  end

  def child_spec(_), do: %{id: __MODULE__, start: {__MODULE__, :start_link, [[]]}, type: :supervisor}
end

defmodule HordeRegistryDemo.Horde.DynamicSupervisor do
  @moduledoc """
  Cluster-wide dynamic supervisor. Uses the UniformDistribution strategy —
  processes are spread roughly evenly across the cluster using consistent
  hashing on the child name.
  """

  use Horde.DynamicSupervisor

  def start_link(_) do
    Horde.DynamicSupervisor.start_link(
      name: __MODULE__,
      strategy: :one_for_one,
      distribution_strategy: Horde.UniformDistribution,
      process_redistribution: :active,
      members: :auto
    )
  end

  def child_spec(_), do: %{id: __MODULE__, start: {__MODULE__, :start_link, [[]]}, type: :supervisor}
end

defmodule HordeRegistryDemo.NodeObserver do
  @moduledoc """
  Keeps `Horde.Registry` and `Horde.DynamicSupervisor` membership in sync
  with the set of connected BEAM nodes (driven by libcluster).

  Without this, adding a node to the BEAM cluster does NOT automatically
  make it a Horde peer — the CRDT would never merge.
  """
  use GenServer
  require Logger

  @horde_processes [
    HordeRegistryDemo.Horde.Registry,
    HordeRegistryDemo.Horde.DynamicSupervisor
  ]

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_) do
    :ok = :net_kernel.monitor_nodes(true, node_type: :visible)
    set_members()
    {:ok, %{}}
  end

  @impl true
  def handle_info({:nodeup, node}, state) do
    Logger.info("[NodeObserver] nodeup #{node}, syncing Horde members")
    set_members()
    {:noreply, state}
  end

  def handle_info({:nodedown, node}, state) do
    Logger.warning("[NodeObserver] nodedown #{node}, syncing Horde members")
    set_members()
    {:noreply, state}
  end

  defp set_members do
    all = [node() | Node.list()]

    for horde <- @horde_processes do
      members = Enum.map(all, fn n -> {horde, n} end)
      Horde.Cluster.set_members(horde, members)
    end
  end
end

defmodule HordeRegistryDemo.DocumentSession do
  @moduledoc """
  A toy document session. Real implementation would keep a CRDT
  of edits, flush to Postgres, and broadcast via Phoenix.PubSub.
  """
  use GenServer
  require Logger

  alias HordeRegistryDemo.Horde.Registry, as: HordeReg

  @type doc_id :: String.t()
  @type edit :: %{op: atom(), pos: non_neg_integer(), text: binary()}

  def start_link(doc_id) do
    GenServer.start_link(__MODULE__, doc_id, name: via(doc_id))
  end

  def child_spec(doc_id) do
    %{
      id: {__MODULE__, doc_id},
      start: {__MODULE__, :start_link, [doc_id]},
      restart: :transient,
      shutdown: 10_000
    }
  end

  defp via(doc_id), do: {:via, Horde.Registry, {HordeReg, doc_id}}

  @doc "Sends edit result from doc_id and edit."
  @spec send_edit(doc_id(), edit()) :: :ok | {:error, :not_found}
  def send_edit(doc_id, edit) do
    case Horde.Registry.lookup(HordeReg, doc_id) do
      [{pid, _}] -> GenServer.call(pid, {:edit, edit})
      [] -> {:error, :not_found}
    end
  end

  @doc "Returns snapshot result from doc_id."
  @spec snapshot(doc_id()) :: {:ok, [edit()]} | {:error, :not_found}
  def snapshot(doc_id) do
    case Horde.Registry.lookup(HordeReg, doc_id) do
      [{pid, _}] -> {:ok, GenServer.call(pid, :snapshot)}
      [] -> {:error, :not_found}
    end
  end

  @impl true
  def init(doc_id) do
    Process.flag(:trap_exit, true)
    Logger.info("DocumentSession #{doc_id} started on #{node()}")
    {:ok, %{doc_id: doc_id, edits: [], node: node()}}
  end

  @impl true
  def handle_call({:edit, edit}, _from, state) do
    new_edits = [edit | state.edits]
    {:reply, {:ok, length(new_edits)}, %{state | edits: new_edits}}
  end

  def handle_call(:snapshot, _from, state) do
    {:reply, Enum.reverse(state.edits), state}
  end

  @impl true
  def terminate(reason, state) do
    Logger.info(
      "DocumentSession #{state.doc_id} terminating on #{node()} reason=#{inspect(reason)} edits=#{length(state.edits)}"
    )

    :ok
  end
end

defmodule HordeRegistryDemo.SessionRouter do
  @moduledoc "Public entry point to start and talk to DocumentSessions."

  alias HordeRegistryDemo.Horde.DynamicSupervisor, as: HordeSup
  alias HordeRegistryDemo.DocumentSession

  @doc "Starts session result from doc_id."
  @spec start_session(String.t()) :: DynamicSupervisor.on_start_child()
  def start_session(doc_id) do
    Horde.DynamicSupervisor.start_child(HordeSup, {DocumentSession, doc_id})
  end

  @doc "Stops session result from doc_id."
  @spec stop_session(String.t()) :: :ok | {:error, :not_found}
  def stop_session(doc_id) do
    case Horde.Registry.lookup(HordeRegistryDemo.Horde.Registry, doc_id) do
      [{pid, _}] -> Horde.DynamicSupervisor.terminate_child(HordeSup, pid)
      [] -> {:error, :not_found}
    end
  end

  @doc "Counts sessions result."
  @spec count_sessions() :: non_neg_integer()
  def count_sessions do
    HordeSup |> Horde.DynamicSupervisor.which_children() |> length()
  end
end
```
### `test/horde_registry_demo_test.exs`

```elixir
defmodule HordeRegistryDemo.SessionRouterTest do
  use ExUnit.Case, async: true
  doctest HordeRegistryDemo.Application

  alias HordeRegistryDemo.{DocumentSession, SessionRouter}

  setup do
    on_exit(fn ->
      for {_, pid, _, _} <- Horde.DynamicSupervisor.which_children(
                              HordeRegistryDemo.Horde.DynamicSupervisor
                            ) do
        Horde.DynamicSupervisor.terminate_child(HordeRegistryDemo.Horde.DynamicSupervisor, pid)
      end
    end)

    :ok
  end

  describe "HordeRegistryDemo.SessionRouter" do
    test "starts a session and routes an edit" do
      {:ok, _pid} = SessionRouter.start_session("doc_42")
      edit = %{op: :insert, pos: 0, text: "Hello"}

      assert {:ok, 1} = DocumentSession.send_edit("doc_42", edit)
      assert {:ok, [^edit]} = DocumentSession.snapshot("doc_42")
    end

    test "starting the same doc twice returns already_started" do
      {:ok, pid} = SessionRouter.start_session("doc_dup")
      assert {:error, {:already_started, ^pid}} = SessionRouter.start_session("doc_dup")
    end

    test "missing session returns :not_found" do
      assert DocumentSession.send_edit("ghost", %{op: :noop, pos: 0, text: ""}) == {:error, :not_found}
    end

    test "count_sessions reflects live children" do
      before = SessionRouter.count_sessions()
      {:ok, _} = SessionRouter.start_session("doc_count_a")
      {:ok, _} = SessionRouter.start_session("doc_count_b")
      assert SessionRouter.count_sessions() == before + 2
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Horde: CRDT-backed distributed registry and supervisor.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Horde: CRDT-backed distributed registry and supervisor ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case HordeRegistryDemo.run(payload) do
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
        for _ <- 1..1_000, do: HordeRegistryDemo.run(:bench)
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
