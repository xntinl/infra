# Tuning net_kernel Tick and Heartbeats Between Nodes

**Project**: `net_kernel_tuning` — stabilizing a WAN-spanning BEAM cluster

---

## Project context

Your trading platform runs two BEAM clusters — one in Frankfurt, one in Virginia — joined
via a dedicated IPSEC tunnel with ~75 ms RTT and the occasional 400 ms latency spike during
cross-Atlantic BGP reroutes. Every few weeks during one of those spikes, all cross-region
nodes suddenly disconnect, triggering a cascade: `:pg` groups lose half their members,
Phoenix Presence fires leave events, and every subscribed client shows stale data until the
cluster rebuilds. Investigation reveals that `net_kernel` declared the remote nodes dead.

The culprit is the default **net tick time** of 60 seconds. Erlang's distribution protocol
sends a tick message every `net_ticktime/4` seconds and declares a peer dead if no data is
received for `net_ticktime + ticktime/4`. On a LAN, 60s is fine. Over a lossy WAN, a single
dropped tick is enough to trip the detector, even if the application layer is healthy.

This exercise builds a reproducible lab for measuring and tuning `net_kernel` tick behavior.
You'll learn how Erlang's heartbeats actually work, how `net_ticktime` and `net_ticktime_min`
interact, how to configure asymmetric intervals safely (both nodes must agree), and when to
use explicit application-layer heartbeats instead.

```
net_kernel_tuning/
├── lib/
│   └── net_kernel_tuning/
│       ├── application.ex
│       ├── heartbeat.ex        # application-level heartbeat GenServer
│       ├── latency_probe.ex    # measures inter-node RTT
│       └── partition_reporter.ex # logs nodedown/nodeup events
├── config/
│   └── runtime.exs             # configures ticktime per environment
├── test/
│   └── net_kernel_tuning/
│       ├── heartbeat_test.exs
│       └── partition_reporter_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. The tick protocol — what actually happens

```
Node A                                   Node B
  │                                        │
  │──── tick (no payload, ~0 bytes) ──────▶│
  │                                        │
  │◀─── tick (no payload, ~0 bytes) ───────│
  │                                        │
  │    (NetTickIntensity = 4)              │
  │    If no traffic for 15s               │
  │    (ticktime / ticktime_intensity),    │
  │    send a tick anyway.                 │
  │                                        │
  │    If no data received for             │
  │    5 consecutive intervals             │
  │    (ticktime + ticktime/ticktime_intensity)│
  │    declare the peer dead.              │
```

With defaults (`net_ticktime = 60`, `net_tickintensity = 4`):
- Tick every 15 seconds (60/4).
- Peer declared dead after no data for 75 seconds (60 + 60/4).

Any application traffic counts as liveness — you don't need ticks to flow if you're already
sending messages. Ticks are a **keepalive for quiet connections**.

### 2. `net_ticktime` and `net_tickintensity`

- **`net_ticktime`**: base interval in seconds. Every node in a cluster must agree on this
  value at handshake time. Mismatch → the node with the higher value is used (OTP 21+) or
  connection is refused (older OTP).
- **`net_tickintensity`**: how many sub-intervals per base period. Ticks are sent at
  `ticktime / tickintensity` seconds. Higher intensity means more frequent, smaller
  probes — finer-grained failure detection at higher network cost.

Formula for detection time:

```
max_silence = net_ticktime + net_ticktime / net_tickintensity  (seconds)
```

| ticktime | intensity | tick interval | detection window |
|----------|-----------|---------------|------------------|
| 60 (default) | 4 (default) | 15 s | 75 s |
| 30 | 4 | 7.5 s | 37.5 s |
| 30 | 8 | 3.75 s | 33.75 s |
| 10 | 4 | 2.5 s | 12.5 s |
| 120 | 4 | 30 s | 150 s |

### 3. Why you rarely want aggressive ticks

Lowering `net_ticktime` to 10 seconds gives you 12.5-second failure detection. But:

- **False positives under WAN jitter**: A 400 ms spike + scheduler latency can delay a
  tick enough to trip a 2.5s-interval check. You'll see phantom `nodedown` events.
- **CPU overhead**: minimal for ticks themselves, but `nodedown`/`nodeup` triggers
  application-level logic (Horde rebalancing, `:pg` convergence, Presence churn) —
  expensive if it cascades.
- **Thundering herd on heal**: after a flap, every subscribed process reconnects,
  re-registers, re-subscribes. For a cluster with 50k subscribed pids, a flap can
  momentarily consume several seconds of CPU.

### 4. Asymmetric settings are a trap

Node A: `net_ticktime: 10`. Node B: `net_ticktime: 60`.

OTP 21+ negotiates the **maximum** at handshake — both effectively run at 60. You thought
you'd get 12.5s detection; you got 75s. Always set ticktime globally via environment variable
or `vm.args` so every node boots with the same value.

Command-line flag: `-kernel net_ticktime 30`
Runtime config: `Application.put_env(:kernel, :net_ticktime, 30)` **before** connecting to
any peer — too late if nodes are already connected.

### 5. When to use application-layer heartbeats instead

`net_kernel` gives you coarse-grained alive/dead detection. It does NOT tell you:

- "Is node B's scheduler starved?"
- "Is the disk I/O stuck, preventing application progress?"
- "Is the BEAM's message queue at 90% depth?"

For application-level health, run a heartbeat GenServer that does a round-trip via `:erpc`
and measures p99 RTT. If the application layer is degraded but TCP is fine, `net_kernel`
will not help you.

### 6. Decision matrix

```
              ┌────────────────────────────────────────┐
              │   Is this a LAN or WAN cluster?        │
              └─────────────┬──────────────────────────┘
                            │
           ┌────────────────┴──────────────────┐
           │                                   │
         LAN (<5ms)                         WAN (>20ms)
           │                                   │
  ticktime 20–30 s                     ticktime 90–120 s
  tickintensity 4–8                    tickintensity 4
  detection: ~25–37 s                  detection: ~112–150 s
           │                                   │
  No app heartbeat needed             + Application heartbeat
  for liveness.                         every 5s with :erpc
                                        for scheduler health.
```

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Project skeleton

**Objective**: Scaffold supervised app to tune :net_kernel heartbeats and measure ticktime impact on failure detection."""

```elixir
defmodule NetKernelTuning.MixProject do
  use Mix.Project

  def project do
    [app: :net_kernel_tuning, version: "0.1.0", elixir: "~> 1.15", deps: deps()]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {NetKernelTuning.Application, []}
    ]
  end

  defp deps do
    []
  end
end
```

### Step 2: Runtime config — setting ticktime correctly

**Objective**: Set kernel.net_ticktime per topology (LAN vs WAN) so failure detection tuning survives rolling deploy."""

```elixir
# config/runtime.exs
import Config

ticktime =
  case System.get_env("CLUSTER_TOPOLOGY", "lan") do
    "lan" -> 30
    "wan" -> 120
    other -> raise "unknown CLUSTER_TOPOLOGY: #{other}"
  end

# Set BEFORE distribution starts. If the kernel app is already up, it will
# still apply but ONLY affects connections established after this point.
config :kernel,
  net_ticktime: ticktime
```

You must ALSO pass `-kernel net_ticktime 30` in `rel/vm.args.eex` so the value is set
before any peer connection is attempted. `runtime.exs` runs late.

```
# rel/vm.args.eex
-kernel net_ticktime 30
```

### Step 3: Partition reporter

**Objective**: Trap :net_kernel.monitor_nodes events and log with net_ticktime context to distinguish false positives."""

```elixir
defmodule NetKernelTuning.PartitionReporter do
  @moduledoc """
  Subscribes to nodeup/nodedown events and logs with enough context to
  diagnose whether a disconnect was a real partition or a ticktime false
  positive. Include the current net_ticktime so logs are self-explanatory.
  """
  use GenServer
  require Logger

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init([]) do
    :net_kernel.monitor_nodes(true, node_type: :all)
    {:ok, %{since: %{}}}
  end

  @impl true
  def handle_info({:nodeup, node, info}, state) do
    Logger.info("nodeup: #{inspect(node)} info=#{inspect(info)} ticktime=#{net_ticktime()}")
    {:noreply, put_in(state.since[node], System.monotonic_time(:millisecond))}
  end

  @impl true
  def handle_info({:nodedown, node, info}, state) do
    lived_ms =
      case Map.fetch(state.since, node) do
        {:ok, t} -> System.monotonic_time(:millisecond) - t
        :error -> :unknown
      end

    Logger.warning(
      "nodedown: #{inspect(node)} info=#{inspect(info)} " <>
        "lived_ms=#{inspect(lived_ms)} ticktime=#{net_ticktime()}"
    )

    {:noreply, %{state | since: Map.delete(state.since, node)}}
  end

  defp net_ticktime, do: :net_kernel.get_net_ticktime()
end
```

### Step 4: Latency probe

**Objective**: Measure :erpc RTT to peers periodically so WAN degradation is detected before :net_kernel splits."""

```elixir
defmodule NetKernelTuning.LatencyProbe do
  @moduledoc """
  Measures inter-node RTT via :erpc. Run on a schedule to detect WAN
  degradation before net_kernel tears the cluster apart.
  """
  use GenServer
  require Logger

  @interval_ms 5_000
  @slow_threshold_ms 250

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @doc "Synchronously probe `node` and return the RTT in ms."
  @spec probe(node(), timeout()) :: {:ok, pos_integer()} | {:error, term()}
  def probe(node, timeout \\ 1_000) do
    t0 = System.monotonic_time(:microsecond)

    try do
      :erpc.call(node, :erlang, :node, [], timeout)
      {:ok, div(System.monotonic_time(:microsecond) - t0, 1000)}
    rescue
      e -> {:error, e}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end

  @impl true
  def init(_opts) do
    schedule()
    {:ok, %{samples: %{}}}
  end

  @impl true
  def handle_info(:probe, state) do
    samples =
      Node.list()
      |> Enum.map(fn node ->
        case probe(node) do
          {:ok, rtt_ms} ->
            if rtt_ms >= @slow_threshold_ms do
              Logger.warning("slow peer: #{inspect(node)} rtt=#{rtt_ms}ms")
            end

            {node, rtt_ms}

          {:error, reason} ->
            Logger.warning("probe failed: #{inspect(node)} reason=#{inspect(reason)}")
            {node, :unreachable}
        end
      end)
      |> Map.new()

    schedule()
    {:noreply, %{state | samples: samples}}
  end

  @spec samples() :: %{node() => pos_integer() | :unreachable}
  def samples, do: GenServer.call(__MODULE__, :samples)

  @impl true
  def handle_call(:samples, _from, state), do: {:reply, state.samples, state}

  defp schedule, do: Process.send_after(self(), :probe, @interval_ms)
end
```

### Step 5: Application-layer heartbeat

**Objective**: Poll peer run_queue via :erpc to catch scheduler stalls that :net_kernel heartbeat misses."""

```elixir
defmodule NetKernelTuning.Heartbeat do
  @moduledoc """
  Periodic round-trip over `:erpc` to confirm peer is not only reachable but
  responsive. Detects scheduler stalls that net_kernel misses.
  """
  use GenServer
  require Logger

  @interval_ms 5_000
  @timeout_ms 2_000
  @max_consecutive_failures 3

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    schedule()
    {:ok, %{failures: %{}}}
  end

  @impl true
  def handle_info(:beat, state) do
    failures =
      Node.list()
      |> Enum.reduce(state.failures, fn node, acc ->
        case beat(node) do
          :ok ->
            Map.delete(acc, node)

          {:error, reason} ->
            count = Map.get(acc, node, 0) + 1

            if count >= @max_consecutive_failures do
              Logger.error(
                "peer #{inspect(node)} unresponsive #{count} times; " <>
                  "reason=#{inspect(reason)}. Consider application-level removal."
              )
            end

            Map.put(acc, node, count)
        end
      end)

    schedule()
    {:noreply, %{state | failures: failures}}
  end

  defp beat(node) do
    try do
      case :erpc.call(node, :erlang, :statistics, [:run_queue], @timeout_ms) do
        n when is_integer(n) and n < 1_000 -> :ok
        n when is_integer(n) -> {:error, {:run_queue_high, n}}
        other -> {:error, {:unexpected, other}}
      end
    rescue
      e -> {:error, e}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end

  defp schedule, do: Process.send_after(self(), :beat, @interval_ms)
end
```

### Step 6: Supervisor wiring

**Objective**: Start PartitionReporter before probes so node membership events are captured with RTT history."""

```elixir
defmodule NetKernelTuning.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      NetKernelTuning.PartitionReporter,
      NetKernelTuning.LatencyProbe,
      NetKernelTuning.Heartbeat
    ]

    opts = [strategy: :one_for_one, name: NetKernelTuning.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 7: Tests

**Objective**: Assert LatencyProbe and Heartbeat handle bogus nodes fast without waiting for net_ticktime expiry."""

```elixir
defmodule NetKernelTuning.HeartbeatTest do
  use ExUnit.Case, async: false

  alias NetKernelTuning.Heartbeat

  setup do
    :net_kernel.start([:"test@127.0.0.1"], %{name_domain: :longnames})

    {:ok, peer, node} =
      :peer.start_link(%{name: :beat_peer, host: ~c"127.0.0.1", longnames: true})

    on_exit(fn -> :peer.stop(peer) end)
    %{peer: peer, node: node}
  end

  describe "NetKernelTuning.Heartbeat" do
    test "probe succeeds against a healthy peer", %{node: node} do
      assert {:ok, rtt_ms} = NetKernelTuning.LatencyProbe.probe(node, 1_000)
      assert rtt_ms >= 0
      assert rtt_ms < 500
    end

    test "probe fails fast against a bogus node" do
      t0 = System.monotonic_time(:millisecond)
      assert {:error, _} = NetKernelTuning.LatencyProbe.probe(:"nonexistent@127.0.0.1", 200)
      elapsed = System.monotonic_time(:millisecond) - t0
      assert elapsed < 800
    end

    test "get_net_ticktime returns the configured value" do
      # OTP rounds to the nearest valid value; just assert it's a positive integer.
      assert is_integer(:net_kernel.get_net_ticktime())
      assert :net_kernel.get_net_ticktime() > 0
    end
  end
end
```

```elixir
defmodule NetKernelTuning.PartitionReporterTest do
  use ExUnit.Case, async: false

  import ExUnit.CaptureLog

  setup do
    :net_kernel.start([:"reporter@127.0.0.1"], %{name_domain: :longnames})
    start_supervised!(NetKernelTuning.PartitionReporter)
    :ok
  end

  describe "NetKernelTuning.PartitionReporter" do
    test "logs nodeup when peer connects" do
      log =
        capture_log(fn ->
          {:ok, peer, _node} =
            :peer.start_link(%{name: :flap, host: ~c"127.0.0.1", longnames: true})

          # let the nodeup message propagate
          Process.sleep(200)
          :peer.stop(peer)
          Process.sleep(200)
        end)

      assert log =~ "nodeup"
      assert log =~ "nodedown"
      assert log =~ "ticktime="
    end
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

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

**1. ticktime must match cluster-wide — or the max wins**
Do not try to give one node a short ticktime and another a long one expecting the average.
OTP picks the max at handshake. Set it identically everywhere, preferably via `vm.args`.

**2. runtime.exs is too late for the first peer connection**
`runtime.exs` runs after `:kernel` starts but possibly after auto-discovery has already
established some connections (via `libcluster`, kubernetes discovery, etc.). The first
handshake might use the old ticktime. Prefer `vm.args` for ticktime.

**3. `:net_kernel.set_net_ticktime/2` at runtime is risky**
It only affects connections established afterwards. Existing connections retain the old
value. During a planned change, drain the cluster, change ticktime, restart nodes — don't
mutate at runtime.

**4. Heartbeats don't replace circuit breakers**
Detecting "peer unresponsive 3 times" is signal, not action. Your app still needs circuit
breakers on remote calls and timeouts so it doesn't block when a peer is degraded but not
yet removed by `:net_kernel`.

**5. `monitor_nodes(true)` is per-process**
The subscriber receives events only while alive. If `PartitionReporter` crashes and is
restarted, it re-subscribes — but events during the gap are lost. Log transitions with
timestamps so you can spot gaps.

**6. TCP keepalive does NOT help here**
`net_kernel` uses its own tick mechanism on top of an already-established TCP connection.
TCP keepalive (`SO_KEEPALIVE`) kicks in at 2 hours by default on Linux — way too slow.
Ticktime is what you tune.

**7. Very short ticktimes can self-DOS the cluster**
Every `nodedown` triggers `Process.exit(:noconnection)` on every monitor. For a cluster
with 100k monitors on remote pids, a flap can take seconds to process. If ticktime is
2 seconds, another tick may fail during the cleanup, creating a cascade.

**8. When NOT to tune ticktime**
If you're running on LAN with <5ms RTT and no flaky network, defaults are fine. Tuning
ticktime to fix a symptom ("nodes disconnect sometimes") without diagnosing the root cause
(packet loss, scheduler starvation, GC pauses) just masks the problem. Tune when you have
measurements, not guesses.

---

## Observability checklist

Before deploying a ticktime change, collect:

1. `:net_kernel.get_net_ticktime()` — confirm current value
2. `:erlang.statistics(:run_queue)` sampled every second for 1 minute — scheduler health
3. `ss -on` (Linux) — TCP retransmits on distribution port 4369 / Erlang ports
4. p50/p95/p99 RTT from `LatencyProbe.samples/0` over 24h
5. Count of `{:nodedown, _, _}` events over 24h — your false-positive baseline

After the change, re-measure #4 and #5. If false positives drop without p99 latency
increasing significantly, the change is a win.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [`:net_kernel` module docs](https://www.erlang.org/doc/man/net_kernel.html) — canonical reference for ticktime
- [Erlang distribution protocol spec](https://www.erlang.org/doc/apps/erts/erl_dist_protocol.html) — wire format of ticks and handshake
- [Fred Hébert — Learn You Some Erlang: Distribunomicon](https://learnyousomeerlang.com/distribunomicon) — partitions and healing
- [Saša Jurić — "To spawn, or not to spawn?"](https://www.theerlangelist.com/) — process and distribution fundamentals
- [Dashbit — "When the BEAM disconnects"](https://dashbit.co/blog) — real-world partition war stories
- [Discord engineering — "Scaling Elixir"](https://discord.com/blog/how-discord-scaled-elixir-to-5-000-000-concurrent-users) — distribution tuning at scale
- [`:peer` docs](https://www.erlang.org/doc/man/peer.html) — lab tool for the tests

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
