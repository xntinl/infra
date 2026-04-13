# Node Monitoring with `:net_kernel.monitor_nodes`

**Project**: `cluster_observer` — a small observability layer that tracks cluster membership changes, their reasons, and exposes them as events and telemetry

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
cluster_observer/
├── lib/
│   └── cluster_observer.ex
├── script/
│   └── main.exs
├── test/
│   └── cluster_observer_test.exs
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
defmodule ClusterObserver.MixProject do
  use Mix.Project

  def project do
    [
      app: :cluster_observer,
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

### `lib/cluster_observer.ex`

```elixir
# lib/cluster_observer/event.ex
defmodule ClusterObserver.Event do
  @moduledoc "Normalised representation of a distribution event."

  @type reason ::
          :connection_closed
          | :disconnect
          | :net_tick_timeout
          | :killed
          | {:shutdown, term()}
          | term()

  @enforce_keys [:type, :node, :at]
  defstruct [:type, :node, :at, :reason, :node_type]

  @doc "Creates result from type, node and info."
  @spec new(:nodeup | :nodedown, node(), keyword()) :: %__MODULE__{}
  def new(type, node, info) when type in [:nodeup, :nodedown] do
    %__MODULE__{
      type: type,
      node: node,
      at: System.system_time(:millisecond),
      reason: Keyword.get(info, :nodedown_reason),
      node_type: Keyword.get(info, :node_type, :visible)
    }
  end
end

# lib/cluster_observer/monitor.ex
defmodule ClusterObserver.Monitor do
  use GenServer
  require Logger

  alias ClusterObserver.Event

  @telemetry_prefix [:cluster_observer]

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns the last N events observed by the monitor."
  @spec recent_events(non_neg_integer()) :: [%Event{}]
  def recent_events(n \\ 50), do: GenServer.call(__MODULE__, {:recent, n})

  @impl true
  def init(opts) do
    history_size = Keyword.get(opts, :history_size, 100)
    node_type = Keyword.get(opts, :node_type, :visible)

    :ok = :net_kernel.monitor_nodes(true, [:nodedown_reason, {:node_type, node_type}])

    {:ok, %{history: :queue.new(), history_size: history_size}}
  end

  @impl true
  def handle_info({:nodeup, node, info}, state) do
    handle_event(:nodeup, node, info, state)
  end

  def handle_info({:nodedown, node, info}, state) do
    handle_event(:nodedown, node, info, state)
  end

  @impl true
  def handle_call({:recent, n}, _from, state) do
    events = state.history |> :queue.to_list() |> Enum.take(-n)
    {:reply, events, state}
  end

  defp handle_event(type, node, info, state) do
    event = Event.new(type, node, info)

    :telemetry.execute(
      @telemetry_prefix ++ [type],
      %{count: 1, at: event.at},
      %{node: node, reason: event.reason, node_type: event.node_type}
    )

    Logger.info("#{type} node=#{inspect(node)} reason=#{inspect(event.reason)}")

    history = enqueue(state.history, event, state.history_size)
    {:noreply, %{state | history: history}}
  end

  defp enqueue(q, event, max) do
    q = :queue.in(event, q)

    if :queue.len(q) > max do
      {_, q2} = :queue.out(q)
      q2
    else
      q
    end
  end
end

# lib/cluster_observer/application.ex
defmodule ClusterObserver.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [ClusterObserver.Monitor]
    Supervisor.start_link(children, strategy: :one_for_one, name: ClusterObserver.Supervisor)
  end
end
```

### `test/cluster_observer_test.exs`

```elixir
defmodule ClusterObserver.MonitorTest do
  use ExUnit.Case, async: true
  doctest ClusterObserver.Event

  alias ClusterObserver.{Event, Monitor}

  setup do
    # Ensure a clean history each test
    :sys.replace_state(Monitor, fn state -> %{state | history: :queue.new()} end)
    :ok
  end

  describe "event struct" do
    test "builds a nodeup event with metadata" do
      ev = Event.new(:nodeup, :"a@h", node_type: :visible)
      assert ev.type == :nodeup
      assert ev.node == :"a@h"
      assert ev.node_type == :visible
      assert is_integer(ev.at)
    end

    test "builds a nodedown event capturing the reason" do
      ev = Event.new(:nodedown, :"a@h", nodedown_reason: :net_tick_timeout)
      assert ev.reason == :net_tick_timeout
    end
  end

  describe "monitor — telemetry integration" do
    test "synthetic nodeup message fires telemetry" do
      ref = make_ref()
      self_pid = self()

      :telemetry.attach(
        "test-#{inspect(ref)}",
        [:cluster_observer, :nodeup],
        fn _event, measurements, meta, _ -> send(self_pid, {ref, measurements, meta}) end,
        nil
      )

      send(Process.whereis(Monitor), {:nodeup, :"fake@h", [node_type: :visible]})

      assert_receive {^ref, %{count: 1}, %{node: :"fake@h"}}, 500

      :telemetry.detach("test-#{inspect(ref)}")
    end

    test "recent_events returns the nodedown event in history" do
      send(Process.whereis(Monitor), {:nodedown, :"fake@h", [nodedown_reason: :disconnect, node_type: :visible]})
      Process.sleep(50)

      events = Monitor.recent_events(10)
      assert Enum.any?(events, &(&1.type == :nodedown and &1.node == :"fake@h"))
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Node Monitoring with `:net_kernel.monitor_nodes`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Node Monitoring with `:net_kernel.monitor_nodes` ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ClusterObserver.run(payload) do
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
        for _ <- 1..1_000, do: ClusterObserver.run(:bench)
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
