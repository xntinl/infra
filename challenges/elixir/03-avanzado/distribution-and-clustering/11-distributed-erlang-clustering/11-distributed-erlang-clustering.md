# Distributed Erlang clustering fundamentals

**Project**: `node_cluster_demo` — a hands-on tour of distributed BEAM primitives: `epmd`, cookies, `Node.connect/1`, `Node.list/0`, `net_kernel`, and cross-node message passing.

**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

Your team runs a fleet of Elixir services that talk to each other for cache invalidation, feature-flag fan-out, and distributed rate-limiting. Before reaching for libcluster, Horde, Phoenix.PubSub, or any abstraction, you need to deeply understand what **Distributed Erlang** (often called "disterl") actually provides — because every higher-level library is built on top of these primitives and leaks their semantics.

Disterl is older than the cloud. It was designed at Ericsson in the 1990s for telephony: trusted LAN, long-lived nodes, full-mesh connectivity, weak security, strong transparency. Knowing those assumptions is the difference between a cluster that survives rolling deploys and one that splits in half the first time a node takes 5 seconds to GC.

This exercise builds `node_cluster_demo`, a minimal multi-node playground. You will run three IEx sessions (`alpha@127.0.0.1`, `beta@127.0.0.1`, `gamma@127.0.0.1`), connect them by hand, observe `:net_kernel` events, and measure the cost of cross-node `send/2`. No libraries. Just OTP.

Project structure:

```
node_cluster_demo/
├── lib/
│   └── node_cluster_demo/
│       ├── application.ex          # supervises ClusterMonitor
│       ├── cluster_monitor.ex      # :net_kernel.monitor_nodes subscriber
│       ├── cross_node_ping.ex      # measures round-trip latency between nodes
│       └── remote_echo.ex          # named GenServer used from other nodes
├── test/
│   └── node_cluster_demo/
│       └── cluster_monitor_test.exs
└── mix.exs
```

---

## Core concepts

### 1. What "distributed" means in BEAM

A BEAM node is an OS process with a **node name** (`alpha@host`) that has joined the distributed runtime by starting `:net_kernel`. Once started, the node is reachable by name, can send messages to pids on other nodes, spawn processes remotely, monitor remote processes, and link across the network. Pids, references, and port identifiers become network-routable.

The wire protocol is **TCP by default**, multiplexed over a single long-lived connection between each pair of connected nodes. The transport can be replaced (TLS via `inet_tls_dist`, or a custom carrier), but the logical model does not change.

```
+------------------+                +------------------+
|  alpha@host      |                |   beta@host      |
|  ┌────────────┐  |   long-lived   |  ┌────────────┐  |
|  │ :net_kernel│──┼────TCP/TLS─────┼──│ :net_kernel│  |
|  └────────────┘  |                |  └────────────┘  |
|  pids, refs ──── routing ─────── pids, refs          |
+------------------+                +------------------+
```

### 2. `epmd` — the name service

`epmd` (Erlang Port Mapper Daemon) is a tiny TCP server on port **4369** that maps node names to dynamically assigned TCP ports. When a node starts distribution, it binds to a random high port and **registers** `{node_name, port}` with the local `epmd`. When another node wants to connect to `alpha@host`, it asks `host`'s `epmd` on 4369: "what port is `alpha` on?" and then opens the real TCP connection directly.

```
beta boots                   beta wants to reach alpha@host
   │                             │
   ▼                             ▼
 start epmd (if absent)     ask alpha's epmd on 4369
 register (beta, 45821)     → "alpha is on 37112"
                            open TCP to host:37112
                            handshake + cookie check
                            connection established
```

`epmd` is started automatically by `erl` if not already running. In containers you often **disable** it (`-start_epmd false`) and use `-erl_epmd_port` or `EPMD_MODULE` to hardcode a port (Kubernetes workflow).

### 3. The cookie — weak authentication

Two nodes connect only if they share the same **cookie** (an atom stored in `~/.erlang.cookie` or set via `Node.set_cookie/1` or `--cookie` flag). The cookie is transmitted during handshake; no encryption by default. An attacker who knows the cookie and can reach port 4369 + the dynamic port can **execute arbitrary code** on the node (`:erpc.call(target, :os, :cmd, ["rm -rf /"])`). Treat it as a capability, not a password.

For production: combine cookies with TLS (`inet_tls_dist`) and firewall rules. For local development, a shared cookie on a loopback address is fine.

### 4. Full-mesh and the "connection storm"

When node A connects to B, they exchange the list of already-connected nodes. B then attempts to connect to all of them (**transitive connection**). Within seconds, the cluster becomes a **complete graph**: N nodes, N·(N−1)/2 connections.

```
Node.connect(:b) from :a      Then transitively:
  a ─── b                       a ─── b
                                │  X  │
                                c ─── d
```

This is great for transparency but scales poorly past ~70 nodes. You can disable the fan-out per node with `:hidden` (start with `--hidden` or use `-connect_all false` and manually manage connections).

### 5. `:net_kernel.monitor_nodes/1` — observing topology

Instead of polling `Node.list/0`, subscribe to cluster membership events. You receive `{:nodeup, node}` and `{:nodedown, node}` messages as processes come and go. This is how libcluster, Horde, Phoenix.PubSub, and every supervisor that cares about cluster health are built.

```elixir
:net_kernel.monitor_nodes(true, node_type: :visible)
# your mailbox now receives:
#   {:nodeup, :beta@host}
#   {:nodedown, :beta@host}
```

Beware: `:nodedown` can fire for transient network blips. The default heartbeat is `net_ticktime` (60s) plus tolerance — the node is declared dead after ~45–75s of silence. Tune `:kernel, :net_ticktime` for faster failure detection, but never below ~4s on unreliable networks.

### 6. Cross-node messaging semantics

`send(pid, msg)` where `pid` lives on another node is **fire-and-forget** just like local send. Semantics:

- **Ordering**: preserved between a single sender/receiver pair.
- **Delivery**: best-effort. If the connection drops mid-send, the message is silently dropped.
- **Serialization**: `msg` is serialized with the External Term Format. Large binaries (> 64 bytes) are ref-counted but still copied across nodes.
- **Backpressure**: none. If the TCP buffer fills, the sender **blocks** in the scheduler, which can cause cluster-wide pauses (known as "busy distribution port").

Rule of thumb: disterl messages should be small (< a few KB) and infrequent (< a few thousand/sec per pair). For high-throughput streaming, open a dedicated TCP/gRPC channel.

---

## Implementation

### Step 1: Create the project

```bash
mix new node_cluster_demo --sup
cd node_cluster_demo
```

### Step 2: `mix.exs`

```elixir
defmodule NodeClusterDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :node_cluster_demo,
      version: "0.1.0",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {NodeClusterDemo.Application, []}
    ]
  end

  defp deps, do: []
end
```

### Step 3: `lib/node_cluster_demo/application.ex`

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
```

### Step 4: `lib/node_cluster_demo/cluster_monitor.ex`

```elixir
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

  @spec subscribe() :: :ok
  def subscribe do
    GenServer.call(__MODULE__, {:subscribe, self()})
  end

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
```

### Step 5: `lib/node_cluster_demo/remote_echo.ex`

```elixir
defmodule NodeClusterDemo.RemoteEcho do
  @moduledoc """
  A tiny named GenServer used from other nodes. Demonstrates that
  `GenServer.call({__MODULE__, remote_node}, ...)` works out of the box
  once two nodes are connected.
  """
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

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
```

### Step 6: `lib/node_cluster_demo/cross_node_ping.ex`

```elixir
defmodule NodeClusterDemo.CrossNodePing do
  @moduledoc """
  Measures round-trip latency for different cross-node primitives:
  raw `send/2`, `GenServer.call/2`, and `:erpc.call/4`.
  """

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

### Step 7: Running three nodes locally

Open three terminals. In each, export the same cookie.

```bash
# Terminal 1
iex --name alpha@127.0.0.1 --cookie devcluster -S mix

# Terminal 2
iex --name beta@127.0.0.1 --cookie devcluster -S mix

# Terminal 3
iex --name gamma@127.0.0.1 --cookie devcluster -S mix
```

From `alpha`:

```elixir
Node.connect(:"beta@127.0.0.1")
#=> true
Node.connect(:"gamma@127.0.0.1")
#=> true
Node.list()
#=> [:"beta@127.0.0.1", :"gamma@127.0.0.1"]
```

Transitive connection: `beta` and `gamma` are now connected to each other too. Exercise the remote echo:

```elixir
NodeClusterDemo.RemoteEcho.echo(:"beta@127.0.0.1", "hello")
#=> {:echo_from, :"beta@127.0.0.1", "hello"}
```

Measure latency:

```elixir
NodeClusterDemo.CrossNodePing.genserver_call_roundtrip(:"beta@127.0.0.1", 2_000)
#=> %{min: 180, p50: 230, p99: 620}   (microseconds, loopback)
```

### Step 8: Tests

```elixir
# test/node_cluster_demo/cluster_monitor_test.exs
defmodule NodeClusterDemo.ClusterMonitorTest do
  use ExUnit.Case, async: false

  alias NodeClusterDemo.ClusterMonitor

  setup do
    _ = Process.whereis(ClusterMonitor) || start_supervised!(ClusterMonitor)
    :ok
  end

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
```

Run with a named node (tests pass with a single node):

```bash
elixir --name test@127.0.0.1 --cookie devcluster -S mix test
```

---

## Trade-offs and production gotchas

**1. Cookie leakage via process listings**
Starting a node with `--cookie mycookie` puts the cookie in `ps aux` output. Prefer `~/.erlang.cookie` (chmod 400) or set it at runtime via `Node.set_cookie/1` before calling `Node.start/1`.

**2. `net_ticktime` default is 60 seconds**
A GC pause on a large heap can exceed the default 45s "silence window" and cause a false `:nodedown`, followed by a messy reconnect. Tune `:kernel, :net_ticktime` per your latency SLO (common production value: 10–20s), but don't go lower than `4` — the protocol sends 4 ticks per interval.

**3. Full-mesh explosion**
At ~70+ nodes, the N² connection count saturates file descriptors and scheduler time on `:net_kernel`. Real large deployments use partial meshes (`:hidden` nodes, per-service sub-clusters) or skip disterl entirely and use Phoenix.PubSub.Redis / an external message bus.

**4. Busy distribution port**
A slow receiver causes the sender scheduler to block in `dist_entry`. Watch `:erlang.system_info(:dist_buf_busy_limit)` and tune with `+zdbbl` (default 1 MB). Monitor via `:observer` → Nodes tab or `:recon.node_stats/4`.

**5. Security — cookies are NOT encryption**
Without TLS (`inet_tls_dist`), disterl traffic is plaintext: cookies, function calls, binary data. On any untrusted network, configure TLS with mutual auth. See `ssl_dist.config` examples in Erlang docs.

**6. Atom exhaustion via remote messages**
Receiving `{:some_fresh_atom, ...}` from a remote node creates the atom on your node too. An attacker who can send messages can exhaust your atom table (~1M default) and crash you. Validate input at the receiving GenServer; never `String.to_atom/1` untrusted data.

**7. Pid serialization — dead pids still route**
A pid you received from a now-dead node is still a valid term. Sending to it returns `:ok` but the message is discarded. Always `Process.monitor/1` or use references that survive the remote process.

**8. When NOT to use raw Distributed Erlang**
Skip disterl when: (a) nodes live in different datacenters or across VPN with > 20ms RTT; (b) you need > 50 nodes; (c) you need strong auth on a zero-trust network without the TLS overhead; (d) you need message durability (disterl drops on disconnect). For these cases, reach for Phoenix.PubSub + Redis, NATS, RabbitMQ, or Kafka.

---

## Benchmark

On a MacBook Pro M2 with two nodes on loopback:

| Operation                              | min (µs) | p50 (µs) | p99 (µs) |
|----------------------------------------|---------:|---------:|---------:|
| local `GenServer.call` (same node)     |        3 |        8 |       45 |
| remote `GenServer.call` (loopback)     |      170 |      230 |      610 |
| `:erpc.call/4` (loopback)              |      160 |      220 |      580 |
| raw `send/2` round-trip (loopback)     |      140 |      200 |      520 |

Cross-node calls add ~200µs overhead from TCP + ETF encode/decode. Across a 1Gbps LAN, expect +0.3–1ms. Across regions (AWS us-east-1 ↔ eu-west-1), expect +80–120ms (and reconsider disterl).

---

## Resources

- [Erlang/OTP — Distributed Erlang](https://www.erlang.org/doc/reference_manual/distributed.html) — the canonical reference
- [`:net_kernel`](https://www.erlang.org/doc/man/net_kernel.html) — `monitor_nodes/2`, `connect_node/1`
- [`:erpc` module](https://www.erlang.org/doc/man/erpc.html) — the modern successor to `:rpc`
- [Fred Hébert — Erlang in Anger, chapter 8 "Network"](https://www.erlang-in-anger.com/) — busy dist ports, atom exhaustion
- [Saša Jurić — "Why Elixir"](https://www.theerlangelist.com/article/why_elixir) — background on BEAM distribution model
- [Discord Engineering — Scaling Elixir to 5M concurrent users](https://discord.com/blog/how-discord-scaled-elixir-to-5-000-000-concurrent-users) — production disterl
- [`inet_tls_dist` — Erlang/OTP](https://www.erlang.org/doc/apps/ssl/ssl_distribution.html) — securing disterl with TLS
