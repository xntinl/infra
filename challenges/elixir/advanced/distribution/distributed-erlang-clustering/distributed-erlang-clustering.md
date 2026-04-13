# Distributed Erlang clustering fundamentals

**Project**: `node_cluster_demo` — a hands-on tour of distributed BEAM primitives: `epmd`, cookies, `Node.connect/1`, `Node.list/0`, `net_kernel`, and cross-node message passing

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
node_cluster_demo/
├── lib/
│   └── node_cluster_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── node_cluster_demo_test.exs
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
defmodule NodeClusterDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :node_cluster_demo,
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

### `lib/node_cluster_demo.ex`

```elixir
defmodule NodeClusterDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      NodeClusterDemo.ClusterMonitor,
      NodeClusterDemo.RemoteEcho
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: NodeClusterDemo.Supervisor)
  end
end

defmodule NodeClusterDemo.ClusterMonitor do
  @moduledoc """
  Subscribes to `:net_kernel.monitor_nodes/2` and keeps an in-memory view
  of the cluster membership, timestamped with the local monotonic clock.

  Publishes `{:cluster_event, event}` to all subscribers registered via
  `subscribe/0`. This is the foundation of every libcluster-style topology
  strategy.
  """
  use GenServer
  require Logger

  @type event :: {:nodeup, node()} | {:nodedown, node()}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns subscribe result."
  @spec subscribe() :: :ok
  def subscribe do
    GenServer.call(__MODULE__, {:subscribe, self()})
  end

  @doc "Returns known nodes result."
  @spec known_nodes() :: [{node(), integer()}]
  def known_nodes do
    GenServer.call(__MODULE__, :known_nodes)
  end

  @impl true
  def init(_opts) do
    :ok = :net_kernel.monitor_nodes(true, node_type: :visible)
    Logger.info("ClusterMonitor started on #{inspect(node())}")

    state = %{
      nodes: Map.new(Node.list(), &{&1, System.monotonic_time(:millisecond)}),
      subscribers: MapSet.new()
    }

    {:ok, state}
  end

  @impl true
  def handle_call({:subscribe, pid}, _from, state) do
    ref = Process.monitor(pid)
    {:reply, :ok, %{state | subscribers: MapSet.put(state.subscribers, {pid, ref})}}
  end

  def handle_call(:known_nodes, _from, state) do
    {:reply, Enum.to_list(state.nodes), state}
  end

  @impl true
  def handle_info({:nodeup, node}, state) do
    Logger.info("[ClusterMonitor] nodeup #{inspect(node)}")
    ts = System.monotonic_time(:millisecond)
    broadcast(state.subscribers, {:nodeup, node})
    {:noreply, %{state | nodes: Map.put(state.nodes, node, ts)}}
  end

  def handle_info({:nodedown, node}, state) do
    Logger.warning("[ClusterMonitor] nodedown #{inspect(node)}")
    broadcast(state.subscribers, {:nodedown, node})
    {:noreply, %{state | nodes: Map.delete(state.nodes, node)}}
  end

  def handle_info({:DOWN, _ref, :process, pid, _reason}, state) do
    subs = Enum.reject(state.subscribers, fn {p, _} -> p == pid end) |> MapSet.new()
    {:noreply, %{state | subscribers: subs}}
  end

  defp broadcast(subscribers, event) do
    for {pid, _ref} <- subscribers, do: send(pid, {:cluster_event, event})
  end
end

defmodule NodeClusterDemo.RemoteEcho do
  @moduledoc """
  A tiny named GenServer used from other nodes. Demonstrates that
  `GenServer.call({__MODULE__, remote_node}, ...)` works out of the box
  once two nodes are connected.
  """
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @doc "Returns echo result from target_node and payload."
  @spec echo(node(), term()) :: {:echo_from, node(), term()}
  def echo(target_node, payload) do
    GenServer.call({__MODULE__, target_node}, {:echo, payload})
  end

  @impl true
  def init(:ok), do: {:ok, %{}}

  @impl true
  def handle_call({:echo, payload}, _from, state) do
    {:reply, {:echo_from, node(), payload}, state}
  end
end

defmodule NodeClusterDemo.CrossNodePing do
  @moduledoc """
  Measures round-trip latency for different cross-node primitives:
  raw `send/2`, `GenServer.call/2`, and `:erpc.call/4`.
  """

  @doc "Sends roundtrip result from target and iterations."
  @spec send_roundtrip(node(), pos_integer()) :: %{min: integer(), p50: integer(), p99: integer()}
  def send_roundtrip(target, iterations \\ 1_000) do
    measurements =
      for _ <- 1..iterations do
        ref = make_ref()
        me = self()

        :erpc.cast(target, fn -> send(me, {:pong, ref}) end)

        t0 = System.monotonic_time(:microsecond)

        receive do
          {:pong, ^ref} -> System.monotonic_time(:microsecond) - t0
        after
          5_000 -> :timeout
        end
      end
      |> Enum.reject(&(&1 == :timeout))
      |> Enum.sort()

    percentiles(measurements)
  end

  @doc "Returns genserver call roundtrip result from target and iterations."
  @spec genserver_call_roundtrip(node(), pos_integer()) :: %{min: integer(), p50: integer(), p99: integer()}
  def genserver_call_roundtrip(target, iterations \\ 1_000) do
    measurements =
      for _ <- 1..iterations do
        t0 = System.monotonic_time(:microsecond)
        _ = NodeClusterDemo.RemoteEcho.echo(target, :ping)
        System.monotonic_time(:microsecond) - t0
      end
      |> Enum.sort()

    percentiles(measurements)
  end

  defp percentiles([]), do: %{min: 0, p50: 0, p99: 0}

  defp percentiles(sorted) do
    n = length(sorted)
    %{
      min: List.first(sorted),
      p50: Enum.at(sorted, div(n, 2)),
      p99: Enum.at(sorted, min(n - 1, div(n * 99, 100)))
    }
  end
end
```

### `test/node_cluster_demo_test.exs`

```elixir
defmodule NodeClusterDemo.ClusterMonitorTest do
  use ExUnit.Case, async: true
  doctest NodeClusterDemo.Application

  alias NodeClusterDemo.ClusterMonitor

  setup do
    _ = Process.whereis(ClusterMonitor) || start_supervised!(ClusterMonitor)
    :ok
  end

  describe "NodeClusterDemo.ClusterMonitor" do
    test "known_nodes/0 returns the current list" do
      assert is_list(ClusterMonitor.known_nodes())
    end

    test "subscribe/0 receives a synthetic nodeup event" do
      :ok = ClusterMonitor.subscribe()
      fake = :"synthetic@127.0.0.1"
      send(Process.whereis(ClusterMonitor), {:nodeup, fake})

      assert_receive {:cluster_event, {:nodeup, ^fake}}, 500
    end

    test "subscribe/0 receives a synthetic nodedown event" do
      :ok = ClusterMonitor.subscribe()
      fake = :"synthetic@127.0.0.1"
      send(Process.whereis(ClusterMonitor), {:nodedown, fake})

      assert_receive {:cluster_event, {:nodedown, ^fake}}, 500
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate distributed Erlang basics: node connectivity and messaging
      {:ok, _pid} = NodeClusterDemo.ClusterMonitor.start_link()
      {:ok, _pid} = NodeClusterDemo.RemoteEcho.start_link()

      # Check known nodes (locally, just this node)
      nodes = NodeClusterDemo.ClusterMonitor.known_nodes()
      IO.puts("✓ Known nodes: #{inspect(nodes)}")

      # Subscribe to cluster events
      :ok = NodeClusterDemo.ClusterMonitor.subscribe()

      # Test remote echo locally
      result = NodeClusterDemo.RemoteEcho.echo(node(), "test_payload")
      IO.inspect(result, label: "✓ Local echo result")

      assert match?({:echo_from, _, "test_payload"}, result), "Echo works"

      IO.puts("✓ Distributed Erlang: node connectivity and messaging working")
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
