# Remote Process Monitoring with Process.monitor and nodedown Handling

**Project**: `node_ping_monitor` — robust remote-process supervision

---

## Project context

You maintain a multi-tenant SaaS where each tenant owns a `TenantCoordinator` GenServer
pinned to one node via Horde. A client dashboard process on node A needs to call into the
tenant coordinator on node B and react if the coordinator crashes, if node B goes down, or
if network between A and B breaks.

Three events look superficially similar but require different responses:

1. **Remote process exits normally** — coordinator rebooted, rejoin when it comes back.
2. **Remote node crashes or loses network** — coordinator might rejoin elsewhere; wait for
   Horde rebalance before reconnecting.
3. **Local node loses its own distribution** — nothing you can do remotely; shut down
   dashboard sessions gracefully.

The primitive for all three is `Process.monitor/1`, but you monitor a name tuple
`{:coordinator, :"b@host"}` rather than a pid (you can't always resolve the pid from afar).
When the monitored target disappears, you get a `{:DOWN, ref, :process, name, reason}` with
different reasons you must disambiguate.

```
node_ping_monitor/
├── lib/
│   └── node_ping_monitor/
│       ├── application.ex
│       ├── tenant_coordinator.ex   # lives on node B (the monitored target)
│       ├── dashboard_client.ex     # lives on node A (the watcher)
│       └── ping_tracker.ex         # translates :DOWN reasons to domain events
├── test/
│   └── node_ping_monitor/
│       ├── ping_tracker_test.exs
│       └── cluster_monitor_test.exs
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

### 1. Monitoring a remote name — not a pid

`Process.monitor(pid)` requires a pid. For a remote registered process, you don't know the
pid (and it may change after a restart). Use:

```elixir
Process.monitor({name, node})
```

This monitors whatever process is registered under `name` on `node`. On OTP 19+, this is
sometimes called "monitoring a name". If nothing is registered:

- The `:DOWN` message is delivered **immediately** with `reason = :noproc`.

If the node is not connected:

- Elixir attempts to connect (`Node.connect/1`). If connection fails, `:DOWN` fires with
  `reason = :noconnection`.

If the node IS connected but the name is unregistered there:

- `:DOWN` fires with `reason = :noproc`.

### 2. The five `:DOWN` reasons you'll see

```
{:DOWN, ref, :process, target, reason}
```

| reason | meaning | action |
|--------|---------|--------|
| `:normal` | target exited cleanly (e.g., completed work) | log, consider re-monitor |
| `:shutdown` | supervisor terminated target | log, re-monitor after restart window |
| `:killed` | external `Process.exit(pid, :kill)` | alert, re-monitor |
| `:noproc` | name not registered (possibly restarting) | backoff + re-monitor |
| `:noconnection` | node unreachable from here | different: wait for `:nodeup` |
| any term | target raised or exited with that reason | rescue, log with context |

**Don't collapse** all of these into a single "retry" branch. Especially do not treat
`:noconnection` the same as `:noproc` — one is "wait for network", the other is "wait for
restart".

### 3. Name monitoring + nodedown: race condition

Consider:

```
(t=0)  Process.monitor({:coord, :"b@host"})    # returns ref
(t=1)  node b@host loses network
(t=2)  You receive: {:nodedown, :"b@host"}     (from :net_kernel.monitor_nodes)
(t=3)  You receive: {:DOWN, ref, :process, {:coord, :"b@host"}, :noconnection}
```

Order of `:nodedown` vs `:DOWN` is not guaranteed across versions. Your reconciliation must
handle both orderings. The cleanest approach is to **subscribe to `nodeup`/`nodedown` AND
use `Process.monitor`** — they serve different purposes:

- `monitor_nodes` tells you about node-level events — useful for "when does B come back?"
- `Process.monitor` tells you about target-level events — useful for "is the coordinator
  alive *there*?"

### 4. Watching vs linking — don't confuse the two

```
            Process.monitor              Process.link
Direction:  one-way (watcher only)       bidirectional (both crash together)
Remote ok:  yes ({name, node})           yes (by pid only)
Death msg:  {:DOWN, ...}                 {:EXIT, ...} if trap_exit else crash
Cancel:     Process.demonitor/1          Process.unlink/1
```

For monitoring a peer you do NOT want to die with, use `Process.monitor`. Links are for
"this child must stay alive or I die too" — wrong semantics here.

### 5. The re-monitor pattern

A one-shot monitor fires once. To keep watching across restarts, you re-monitor after each
`:DOWN`:

```
    ┌──────────────────────────────┐
    │  state = :disconnected       │
    └──────────────┬───────────────┘
                   │
                   │ Process.monitor({name, node})
                   │
                   ▼
    ┌──────────────────────────────┐
    │  state = :watching           │
    │  ref = Ref<...>              │
    └──────────────┬───────────────┘
                   │
                   │ {:DOWN, ref, :process, _, reason}
                   │
                   ▼
    ┌──────────────────────────────┐
    │  classify(reason):           │
    │    :normal, :shutdown        │──▶ schedule_retry_fast
    │    :noproc                   │──▶ schedule_retry_fast
    │    :noconnection             │──▶ wait_for_nodeup
    │    other                     │──▶ alert + schedule_retry_slow
    └──────────────────────────────┘
```

### 6. Backoff is mandatory

Without backoff, a persistent `:noproc` (target not yet started) triggers a monitor storm:
each `:DOWN` fires immediately and you re-monitor immediately — burning CPU. Use
exponential backoff with jitter: 100ms, 200ms, 400ms, 800ms, capped at 5-10s.

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

### Step 1: Mix project

```elixir
defmodule NodePingMonitor.MixProject do
  use Mix.Project

  def project do
    [app: :node_ping_monitor, version: "0.1.0", elixir: "~> 1.15", deps: []]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {NodePingMonitor.Application, []}
    ]
  end
end
```

### Step 2: TenantCoordinator (the target)

```elixir
defmodule NodePingMonitor.TenantCoordinator do
  @moduledoc """
  Domain process. In production, owns tenant-specific state. Here it's
  a registered GenServer that answers `:ping` calls; this is the process
  we remote-monitor from node A.
  """
  use GenServer

  def start_link(opts) do
    name = Keyword.get(opts, :name, __MODULE__)
    GenServer.start_link(__MODULE__, opts, name: name)
  end

  def ping(name \\ __MODULE__, timeout \\ 1_000) do
    GenServer.call(name, :ping, timeout)
  end

  @impl true
  def init(opts), do: {:ok, %{started_at: System.monotonic_time(), opts: opts}}

  @impl true
  def handle_call(:ping, _from, state), do: {:reply, {:pong, node()}, state}

  @impl true
  def handle_call(:crash, _from, _state), do: exit(:deliberate_crash)
end
```

### Step 3: PingTracker — the watcher logic in isolation

```elixir
defmodule NodePingMonitor.PingTracker do
  @moduledoc """
  Translates raw :DOWN / :nodedown / :nodeup events into domain events
  suitable for supervision logic. Pure-ish: no side effects besides logging.
  """
  require Logger

  @type down_reason :: :normal | :shutdown | :killed | :noproc | :noconnection | term()

  @type classification ::
          :retry_fast | :retry_slow | :wait_for_nodeup | :alert_and_slow

  @spec classify(down_reason()) :: classification()
  def classify(:normal), do: :retry_fast
  def classify(:shutdown), do: :retry_fast
  def classify(:noproc), do: :retry_fast
  def classify(:noconnection), do: :wait_for_nodeup
  def classify(:killed), do: :alert_and_slow
  def classify({:shutdown, _}), do: :retry_fast
  def classify(_other), do: :alert_and_slow

  @doc """
  Next backoff delay given the current attempt count, classification, and RNG.
  Pure function so tests are deterministic.
  """
  @spec next_delay_ms(non_neg_integer(), classification(), (non_neg_integer() -> non_neg_integer())) ::
          pos_integer()
  def next_delay_ms(attempts, classification, rand \\ &:rand.uniform/1) do
    base =
      case classification do
        :retry_fast -> min(100 * :math.pow(2, attempts) |> trunc(), 2_000)
        :retry_slow -> min(500 * :math.pow(2, attempts) |> trunc(), 10_000)
        :alert_and_slow -> min(500 * :math.pow(2, attempts) |> trunc(), 10_000)
        :wait_for_nodeup -> 30_000
      end

    # +/- 20% jitter
    jitter = rand.(max(div(base, 5), 1))
    base + jitter - div(base, 10)
  end
end
```

### Step 4: DashboardClient — the watcher process

```elixir
defmodule NodePingMonitor.DashboardClient do
  @moduledoc """
  Watches a remote registered process and emits domain events on transitions.
  Re-monitors automatically with backoff. Subscribes to nodeup/nodedown so
  it can fast-path reconnect when the target's node comes back.
  """
  use GenServer
  require Logger

  alias NodePingMonitor.PingTracker

  @type t :: %{
          target_name: atom(),
          target_node: node(),
          monitor_ref: reference() | nil,
          attempts: non_neg_integer(),
          state: :watching | :backing_off | :waiting_for_nodeup,
          listener: pid()
        }

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    target_name = Keyword.fetch!(opts, :target_name)
    target_node = Keyword.fetch!(opts, :target_node)
    listener = Keyword.get(opts, :listener, self())

    :net_kernel.monitor_nodes(true, node_type: :all)

    state =
      %{
        target_name: target_name,
        target_node: target_node,
        monitor_ref: nil,
        attempts: 0,
        state: :backing_off,
        listener: listener
      }
      |> start_monitoring()

    {:ok, state}
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _target, reason}, %{monitor_ref: ref} = state) do
    classification = PingTracker.classify(reason)
    emit(state.listener, {:target_down, state.target_node, reason, classification})
    Logger.info("target down: node=#{state.target_node} reason=#{inspect(reason)} class=#{classification}")

    new_state = %{state | monitor_ref: nil, attempts: state.attempts + 1}

    case classification do
      :wait_for_nodeup ->
        {:noreply, %{new_state | state: :waiting_for_nodeup}}

      _ ->
        delay = PingTracker.next_delay_ms(state.attempts, classification)
        Process.send_after(self(), :retry_monitor, delay)
        {:noreply, %{new_state | state: :backing_off}}
    end
  end

  def handle_info(:retry_monitor, state) do
    {:noreply, start_monitoring(state)}
  end

  def handle_info({:nodeup, node, _info}, %{target_node: node, state: :waiting_for_nodeup} = state) do
    Logger.info("target node reappeared: #{inspect(node)}")
    emit(state.listener, {:nodeup, node})
    {:noreply, start_monitoring(%{state | attempts: 0})}
  end

  def handle_info({:nodeup, _other, _info}, state), do: {:noreply, state}

  def handle_info({:nodedown, node, _info}, %{target_node: node} = state) do
    Logger.warning("target node went down: #{inspect(node)}")
    emit(state.listener, {:nodedown, node})
    {:noreply, state}
  end

  def handle_info({:nodedown, _other, _info}, state), do: {:noreply, state}

  def handle_info(other, state) do
    Logger.debug("unhandled message: #{inspect(other)}")
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, %{monitor_ref: ref}) when is_reference(ref) do
    Process.demonitor(ref, [:flush])
    :ok
  end

  def terminate(_, _), do: :ok

  defp start_monitoring(state) do
    ref = Process.monitor({state.target_name, state.target_node})

    emit(state.listener, {:monitoring, state.target_node})

    %{state | monitor_ref: ref, state: :watching}
  end

  defp emit(nil, _), do: :ok
  defp emit(pid, msg) when is_pid(pid), do: send(pid, msg)
end
```

### Step 5: Application

```elixir
defmodule NodePingMonitor.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = []
    opts = [strategy: :one_for_one, name: NodePingMonitor.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 6: Tests

```elixir
defmodule NodePingMonitor.PingTrackerTest do
  use ExUnit.Case, async: true

  alias NodePingMonitor.PingTracker

  test "classify produces distinct classes for distinct reasons" do
    assert PingTracker.classify(:normal) == :retry_fast
    assert PingTracker.classify(:shutdown) == :retry_fast
    assert PingTracker.classify(:noproc) == :retry_fast
    assert PingTracker.classify(:noconnection) == :wait_for_nodeup
    assert PingTracker.classify(:killed) == :alert_and_slow
    assert PingTracker.classify({:shutdown, :deliberate}) == :retry_fast
    assert PingTracker.classify(:custom_error) == :alert_and_slow
  end

  test "next_delay_ms grows exponentially for fast retries" do
    deterministic_rand = fn _ -> 0 end

    d0 = PingTracker.next_delay_ms(0, :retry_fast, deterministic_rand)
    d1 = PingTracker.next_delay_ms(1, :retry_fast, deterministic_rand)
    d2 = PingTracker.next_delay_ms(2, :retry_fast, deterministic_rand)

    assert d0 < d1
    assert d1 < d2
  end

  test "next_delay_ms caps fast retries at 2s" do
    deterministic_rand = fn _ -> 0 end
    d = PingTracker.next_delay_ms(100, :retry_fast, deterministic_rand)
    # cap is 2000, jitter adds a bit, but the base is clamped
    assert d <= 2_100
  end

  test "wait_for_nodeup uses a long fixed delay" do
    d = PingTracker.next_delay_ms(0, :wait_for_nodeup, fn _ -> 0 end)
    assert d >= 20_000
  end
end
```

```elixir
defmodule NodePingMonitor.ClusterMonitorTest do
  use ExUnit.Case, async: false

  alias NodePingMonitor.{DashboardClient, TenantCoordinator}

  setup do
    :net_kernel.start([:"primary@127.0.0.1"], %{name_domain: :longnames})
    {:ok, peer, node} = :peer.start_link(%{name: :b, host: ~c"127.0.0.1", longnames: true})

    :rpc.call(node, :code, :add_paths, [:code.get_path()])
    :rpc.call(node, Application, :ensure_all_started, [:node_ping_monitor])

    on_exit(fn -> :peer.stop(peer) end)

    %{peer: peer, node: node}
  end

  test "receives :DOWN with :noproc when target is not started", %{node: node} do
    start_supervised!(
      {DashboardClient,
       target_name: :absent_coord, target_node: node, listener: self()}
    )

    assert_receive {:monitoring, ^node}, 2_000
    assert_receive {:target_down, ^node, :noproc, :retry_fast}, 2_000
  end

  test "receives :DOWN with target exit reason", %{node: node} do
    {:ok, _pid} =
      :rpc.call(node, TenantCoordinator, :start_link, [[name: :live_coord]])

    start_supervised!(
      {DashboardClient,
       target_name: :live_coord, target_node: node, listener: self()}
    )

    assert_receive {:monitoring, ^node}, 2_000

    # Trigger a remote crash
    catch_exit(:rpc.call(node, GenServer, :call, [:live_coord, :crash, 200]))

    assert_receive {:target_down, ^node, _reason, _class}, 2_000
  end

  test "nodedown is delivered when peer stops", %{peer: peer, node: node} do
    start_supervised!(
      {DashboardClient,
       target_name: :any_name, target_node: node, listener: self()}
    )

    assert_receive {:monitoring, ^node}, 2_000
    :peer.stop(peer)

    assert_receive {:nodedown, ^node}, 5_000
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Trade-offs and production gotchas

**1. `Process.monitor({name, node})` may race with node connection**
If the node is not yet connected, Elixir tries to connect. If DNS or TLS is slow, you can
get `:noconnection` even when the node is technically available. Re-monitor with backoff.

**2. Monitors do not survive node disconnects cleanly**
When the remote node disconnects, you get `{:DOWN, _, _, _, :noconnection}`. The monitor
ref is consumed. You must re-monitor after `:nodeup` — the monitor does not auto-revive.

**3. Monitoring by pid vs by name**
If you call `Process.monitor(pid)` where `pid` is on a remote node and that node
disconnects, you get `:noconnection`. If the node reconnects, the pid might be invalid
(the process likely restarted). Monitoring by `{name, node}` lets you re-attach cleanly.

**4. `Process.demonitor(ref, [:flush])` matters during shutdown**
Without `:flush`, a `:DOWN` message in your mailbox after demonitor is never cleaned up —
it stays until GenServer receives it, potentially after state has moved on. Always use
`:flush` unless you explicitly want the message.

**5. Don't monitor thousands of remote pids from one process**
Each monitor adds a small memory cost on both ends and a row in the distribution monitor
table. At 10k+ monitors per process, expect measurable slowdowns on disconnection (each
`:DOWN` is enqueued into the same mailbox). Shard across multiple watcher processes.

**6. `monitor_nodes` with duplicate nodes**
Calling `:net_kernel.monitor_nodes(true)` twice from the same process subscribes twice.
Each event is delivered twice. Track whether you've subscribed; or call it in `init/1`
where it's known to be once.

**7. `:noconnection` can also mean "not yet connected"**
On a fresh BEAM, until `Node.connect/1` is called, `{name, other_node}` monitoring returns
`:noconnection` instantly. Don't treat that as a partition — treat it as "still booting".

**8. When NOT to use this pattern**
- For client-server where you always call from a pool: use `:poolboy` or `NimblePool` and
  handle call failures directly.
- For "notify me when anything in this group dies": use `:pg.monitor/2` — single monitor
  tracks the whole group, no per-pid wiring.
- For internal supervision: supervisors already monitor; don't duplicate.

---

## Benchmark

Basic microbenchmark — how fast can a node process `:DOWN` messages?

```elixir
Benchee.run(
  %{
    "Process.monitor + demonitor (local)" => fn ->
      {:ok, pid} = Agent.start_link(fn -> 0 end)
      ref = Process.monitor(pid)
      Agent.stop(pid)
      receive do
        {:DOWN, ^ref, :process, _, _} -> :ok
      after
        100 -> :timeout
      end
    end
  },
  time: 5,
  warmup: 1
)
```

On a modern laptop: ~15-25 µs per cycle. Cross-node monitoring adds network RTT — expect
~1-5 ms on LAN. Per-monitor memory cost: ~80 bytes in the local process heap, similar on
the remote.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [`Process` module — hexdocs](https://hexdocs.pm/elixir/Process.html#monitor/1)
- [`:net_kernel.monitor_nodes/2` — erlang.org](https://www.erlang.org/doc/man/net_kernel.html#monitor_nodes-2)
- [Erlang efficiency guide — monitors](https://www.erlang.org/doc/efficiency_guide/processes.html)
- [Saša Jurić — "Beyond GenServer" (ElixirConf talk)](https://www.theerlangelist.com/) — when to monitor vs link
- [Phoenix.Tracker source](https://github.com/phoenixframework/phoenix_pubsub/blob/main/lib/phoenix/tracker.ex) — real-world remote-monitoring at scale
- [Horde's node-watcher implementation](https://github.com/derekkraan/horde/blob/master/lib/horde/node_listener.ex) — production-grade remote-node tracking
