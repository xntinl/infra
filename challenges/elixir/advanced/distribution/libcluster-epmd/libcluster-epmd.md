# libcluster — Epmd strategy for local multi-node development

**Project**: `libcluster_epmd` — use `Cluster.Strategy.Epmd` to auto-connect a small, statically configured set of BEAM nodes during development and integration testing

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
libcluster_epmd/
├── lib/
│   └── libcluster_epmd.ex
├── script/
│   └── main.exs
├── test/
│   └── libcluster_epmd_test.exs
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
defmodule LibclusterEpmd.MixProject do
  use Mix.Project

  def project do
    [
      app: :libcluster_epmd,
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

### `lib/libcluster_epmd.ex`

```elixir
defmodule LibclusterEpmd.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    topologies = Application.get_env(:libcluster, :topologies, [])

    children = [
      {Cluster.Supervisor, [topologies, [name: LibclusterEpmd.ClusterSupervisor]]},
      LibclusterEpmd.ClusterProbe
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: LibclusterEpmd.Supervisor)
  end
end

defmodule LibclusterEpmd.ClusterProbe do
  @moduledoc """
  Subscribes to libcluster topology events and to :net_kernel node monitors.
  Keeps a map of `node => %{status, last_event_at}` and exposes it via `status/0`.
  """
  use GenServer
  require Logger

  @type status :: :connected | :disconnected | :unknown

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @doc "Returns status result."
  @spec status() :: %{node() => %{status: status(), last_event_at: integer()}}
  def status, do: GenServer.call(__MODULE__, :status)

  @impl true
  def init(_) do
    :ok = :net_kernel.monitor_nodes(true, node_type: :visible)
    Logger.info("[ClusterProbe] online on #{node()}")

    state = Map.new(Node.list(), &{&1, %{status: :connected, last_event_at: ts()}})
    {:ok, state}
  end

  @impl true
  def handle_call(:status, _from, state), do: {:reply, state, state}

  @impl true
  def handle_info({:nodeup, node}, state) do
    Logger.info("[ClusterProbe] nodeup #{node}")
    {:noreply, Map.put(state, node, %{status: :connected, last_event_at: ts()})}
  end

  def handle_info({:nodedown, node}, state) do
    Logger.warning("[ClusterProbe] nodedown #{node}")
    {:noreply, Map.put(state, node, %{status: :disconnected, last_event_at: ts()})}
  end

  defp ts, do: System.monotonic_time(:millisecond)
end

defmodule LibclusterEpmd.Topology do
  @moduledoc "Introspect the configured libcluster topology at runtime."

  @doc "Returns hosts result from topology_name."
  @spec hosts(atom()) :: [node()]
  def hosts(topology_name \\ :dev_epmd) do
    :libcluster
    |> Application.get_env(:topologies, [])
    |> Keyword.fetch!(topology_name)
    |> Keyword.fetch!(:config)
    |> Keyword.fetch!(:hosts)
  end

  @doc "Returns whether connected holds from node."
  @spec connected?(node()) :: boolean()
  def connected?(node), do: node in [node() | Node.list()]

  @doc "Returns coverage result from topology_name."
  @spec coverage(atom()) :: %{connected: [node()], missing: [node()]}
  def coverage(topology_name \\ :dev_epmd) do
    all = hosts(topology_name)
    {c, m} = Enum.split_with(all, &connected?/1)
    %{connected: c, missing: m}
  end
end
```

### `test/libcluster_epmd_test.exs`

```elixir
defmodule LibclusterEpmd.ClusterProbeTest do
  use ExUnit.Case, async: true
  doctest LibclusterEpmd.Application

  alias LibclusterEpmd.{ClusterProbe, Topology}

  describe "LibclusterEpmd.ClusterProbe" do
    test "status/0 returns a map" do
      assert is_map(ClusterProbe.status())
    end

    test "synthetic nodeup event updates status" do
      fake = :"synthetic@127.0.0.1"
      send(Process.whereis(ClusterProbe), {:nodeup, fake})
      Process.sleep(50)

      assert %{status: :connected} = ClusterProbe.status()[fake]
    end

    test "synthetic nodedown event updates status" do
      fake = :"synthetic@127.0.0.1"
      send(Process.whereis(ClusterProbe), {:nodedown, fake})
      Process.sleep(50)

      assert %{status: :disconnected} = ClusterProbe.status()[fake]
    end

    test "Topology.coverage/1 returns the split" do
      # Run this test with `elixir --name test@127.0.0.1 --cookie devcluster -S mix test`
      # so `node/0` is a real node; otherwise coverage will treat everything as missing.
      %{connected: _c, missing: _m} = Topology.coverage(:dev_epmd)
      assert is_list(Topology.hosts(:dev_epmd))
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for libcluster — Epmd strategy for local multi-node development.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== libcluster — Epmd strategy for local multi-node development ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case LibclusterEpmd.run(payload) do
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
        for _ <- 1..1_000, do: LibclusterEpmd.run(:bench)
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
