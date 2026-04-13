# Node Monitoring with `:net_kernel.monitor_nodes`

**Project**: `cluster_observer` — a small observability layer that tracks cluster membership changes, their reasons, and exposes them as events and telemetry.

## Project context

Your team runs a fleet of BEAM nodes and wants first-party visibility into `:nodeup` and `:nodedown` events: which node left, when, whether it came back, and whether the cause was a crash or a clean shutdown. The hosted monitoring solution (Datadog, Prometheus) can scrape counters but it does not see BEAM distribution events. You need a process inside the cluster that listens to distribution signals and publishes them on a telemetry bus, so that dashboards, alerts and custom consumers (a Horde rebalancer, a circuit breaker, a leader re-election hook) can all react.

The classic pitfall: people call `:net_kernel.monitor_nodes/1` in `init/1` and never handle the message format difference between `monitor_nodes(true)` and `monitor_nodes(true, [:nodedown_reason])`. The reason-enabled form returns three-element tuples; the default form returns two-element tuples. Mixing them silently drops messages.

```
cluster_observer/
├── lib/
│   └── cluster_observer/
│       ├── application.ex
│       ├── monitor.ex
│       └── event.ex
├── test/
│   └── cluster_observer/
│       └── monitor_test.exs
├── bench/
│   └── monitor_bench.exs
└── mix.exs
```

## Why `:net_kernel.monitor_nodes` and not `Node.list/0` polling

`Node.list/0` is a snapshot. Polling it every second means you can lose a flap: node leaves at t, rejoins at t+200ms — both changes are invisible to a 1-second poll. `:net_kernel.monitor_nodes/1` delivers a message to the calling process the moment the distribution driver notices a link change. No polling, no missed events.

## Why a dedicated `GenServer` and not `spawn/1`

A raw `spawn` has no supervision, no state, no telemetry integration. A `GenServer`:

- is supervised (restart after crash), so the stream of events is durable,
- buffers events in state if you need windowing or debouncing,
- integrates with `:telemetry` for fan-out to multiple consumers,
- lets you register as `:global` or Horde if you want a single cluster-wide observer.

## Core concepts

### 1. Monitor message formats

```elixir
:net_kernel.monitor_nodes(true)
# messages: {:nodeup, node} | {:nodedown, node}

:net_kernel.monitor_nodes(true, [:nodedown_reason])
# messages: {:nodeup, node, info_list} | {:nodedown, node, info_list}
#   info_list for :nodedown contains {:nodedown_reason, reason}
#   where reason ∈ :connection_closed | :disconnect | :net_tick_timeout | ...
```

Pick one format and stick to it. The reason-enabled form is strictly more informative; use it.

### 2. Subscribing vs registering

`monitor_nodes(true, opts)` subscribes the calling process. There can be many subscribers — each gets a copy of every event. When the subscriber dies, the kernel silently stops sending to it. There is no unsubscribe-then-resubscribe drop risk; the process dictionary tracks it by pid.

### 3. `:visible` vs hidden nodes

By default only visible nodes trigger events. Hidden nodes (connected with `-hidden`) do not. Pass `node_type: :all` if you need both.

### 4. `:telemetry` as the fan-out layer

Rather than building a bespoke subscribe API, emit `[:cluster_observer, :nodeup]` and `[:cluster_observer, :nodedown]` via `:telemetry.execute/3`. Any consumer attaches with `:telemetry.attach/4`. No coupling, no custom process group.

## Design decisions

- **Option A — use `Node.monitor/2`** for each specific remote pid of interest. Fine for peer-to-peer links, not for cluster-wide topology.
- **Option B — `:net_kernel.monitor_nodes/1` in a `GenServer` + `:telemetry` fan-out** (chosen): single place of truth, pluggable consumers.
- **Option C — `libcluster`'s `Cluster.Events`**: exists but tied to libcluster lifecycle. Our observer must work even if libcluster is not running.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ClusterObserver.MixProject do
  use Mix.Project

  def project do
    [app: :cluster_observer, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {ClusterObserver.Application, []}]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Event struct

**Objective**: Normalize `:nodeup` and `:nodedown` raw tuples into a typed struct with reason and node_type for telemetry.

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
```

### Step 2: The monitor GenServer

**Objective**: Subscribe to `:net_kernel` distribution events, emit telemetry, and buffer history queue for observability queries.

```elixir
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
```

### Step 3: Supervision

**Objective**: Start Monitor GenServer under supervision so distribution events flow continuously from boot.

```elixir
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

## Data flow diagram

```
  BEAM distribution driver
       │
       │  {:nodeup, n, [node_type: :visible]}
       │  {:nodedown, n, [nodedown_reason: :net_tick_timeout, node_type: :visible]}
       ▼
  ClusterObserver.Monitor (GenServer)
       │
       ├──▶ :telemetry.execute([:cluster_observer, :nodeup], ..., meta)
       │       │
       │       ├──▶ Prometheus exporter handler
       │       ├──▶ Horde rebalancer handler
       │       └──▶ Your custom handler
       │
       └──▶ in-memory ring buffer (last N events, for debugging)
```

## Why this works

`:net_kernel.monitor_nodes/2` is an explicit subscription: the kernel internally keeps a set of subscriber pids and iterates it on every topology change. Message delivery uses normal Erlang mailbox semantics — ordered per sender, unordered across senders. Because there is exactly one sender here (`:net_kernel`), our handler sees events strictly in the order the kernel observed them, which matches the actual wire ordering from the distribution driver.

## Tests

```elixir
# test/cluster_observer/monitor_test.exs
defmodule ClusterObserver.MonitorTest do
  use ExUnit.Case, async: false

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

## Benchmark

```elixir
# bench/monitor_bench.exs
pid = Process.whereis(ClusterObserver.Monitor)

Benchee.run(
  %{
    "synthetic nodeup message" => fn ->
      send(pid, {:nodeup, :"bench@h", [node_type: :visible]})
    end
  },
  time: 5,
  warmup: 2
)
```

Target: handling > 200k synthetic events/second, since the handler is pure in-memory work (enqueue + telemetry fan-out). Real `:nodeup`/`:nodedown` rates are orders of magnitude lower (one per node crash), so throughput is never the constraint; latency of detection is.

## Deep Dive

Distributed Erlang relies on a heartbeat mechanism (net_kernel tick) to detect node failure, but the network is fundamentally asynchronous—split-brain scenarios are inevitable. A partitioned cluster may have two sets of nodes, each believing the other is dead. Libraries like Horde and Phoenix.PubSub solve this with quorum-aware consensus, but they add latency and complexity. At scale, choose your consistency model explicitly: eventual consistency (via Redis PubSub) is faster but allows temporary divergence; strong consistency (via Horde DLM or distributed transactions) is slower but guarantees atomicity. For global registries, the order of operations matters—registering a process before its monitor is live creates race conditions. In multi-region setups, latency between nodes compounds these issues; consider regional clusters with a lightweight coordinator rather than a fully meshed topology.
## Advanced Considerations

Distributed Elixir systems require careful consideration of network partitions, consistent hashing for distributed state, and the interaction between clustering libraries and node discovery mechanisms. Network partitions are not rare edge cases; they happen regularly in cloud deployments due to maintenance windows and infrastructure issues. A system that works perfectly during local testing but fails under network partitions indicates insufficient failure handling throughout the codebase. Split-brain scenarios where multiple network partitions lead to different cluster views require explicit recovery mechanisms that are often business-specific and context-dependent.

Horde and distributed registries provide eventual consistency guarantees, but "eventual" can mean minutes during network partitions. Applications must handle the case where the same name is registered on multiple nodes simultaneously without coordination. Consistent hashing for distributed services requires understanding rebalancing costs — a single node failure can cause significant key redistribution and thundering herd problems if not carefully managed. The cost of distributed consensus using algorithms like Raft is high; choose it only when consistency is more important than availability and can afford the performance cost.

Global state replication across nodes creates synchronization challenges at scale. Choosing between replicating everywhere versus replicating to specific nodes affects both consistency latency and network bandwidth utilization fundamentally. Node monitoring and heartbeat mechanisms require careful timeout tuning — too aggressive and you get false positives during network hiccups; too conservative and you don't detect actual failures quickly enough for recovery. The EPMD (Erlang Port Mapper Daemon) is a critical component that can become a bottleneck in large clusters and requires careful capacity planning.


## Deep Dive: Cluster Patterns and Production Implications

Clustering distributes computation across nodes using Erlang's distribution protocol. Testing clusters requires simulating node failures, network partitions, and message delays—challenges that single-node tests don't expose. Production clusters fail in ways that cluster tests reveal: nodes can become isolated (stuck), messages can be reordered, and consensus is expensive.

---

## Trade-offs and production gotchas

1. **Net tick timeout is 60 s by default**: if a node goes silent but the TCP connection stays open (frozen VM, paused container), you will not see `:nodedown` for up to 60 s. Tune `net_ticktime` in `vm.args` (lower value = faster detection, more false positives). 15–20 s is common in containerized environments.
2. **Flapping nodes spam events**: a node that repeatedly reconnects generates many pairs of `:nodeup`/`:nodedown`. Consumers that trigger work on every event (leader election, Horde rebalance) can melt down. Add debouncing at the consumer side — not in the monitor itself.
3. **Order across subscribers is not guaranteed**: two processes each calling `monitor_nodes(true)` will both see the same events but potentially schedule differently. Do not rely on one handler running before another.
4. **Missing `:node_type` option**: forgetting to specify `node_type` makes you blind to hidden nodes. If you run `observer` sessions with `-hidden`, they will not appear in your event stream.
5. **Ring buffer memory**: an unbounded history queue leaks. Cap at a known size and document it.
6. **When NOT to use this**: for monitoring downstream HTTP services, use health probes. `:net_kernel.monitor_nodes` is strictly about BEAM-to-BEAM cluster membership.

## Reflection

Two BEAM nodes A and B hold an open distribution connection. You pause the container running A for 30 seconds (SIGSTOP). With `net_ticktime` at the default 60 s, does B see `:nodedown`? What about with `net_ticktime = 15`? What if you do the pause across a TCP keep-alive boundary, and how does this interact with Kubernetes liveness probes on A?

## Resources

- [`:net_kernel` docs](https://www.erlang.org/doc/man/net_kernel.html)
- [`:telemetry` hexdocs](https://hexdocs.pm/telemetry)
- [Erlang distribution tuning — Fred Hebert](https://ferd.ca/)
- [BEAM distribution deep dive](https://blog.erlang.org/erlang-21-otp-netsplit/)
- [`erts :net_ticktime`](https://www.erlang.org/doc/man/kernel_app.html#net_ticktime)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
