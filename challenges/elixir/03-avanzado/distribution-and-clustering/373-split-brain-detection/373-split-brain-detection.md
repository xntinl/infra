# Split-Brain Detection and Resolution

**Project**: `split_brain_guard` — detect when a BEAM cluster has partitioned into multiple islands, pick a winning side deterministically, and gracefully stop services on the losing side.

## Project context

You run a stateful service (a queue processor, a singleton leader, a CRDT that does not tolerate conflicting writes) across five nodes. A network glitch partitions the cluster into `{a, b}` and `{c, d, e}`. Both partitions think the other side is dead. Left alone, both sides keep processing messages and, worse, both elect their own leader. When the partition heals, you have divergent state.

The two classical resolution strategies:

- **Majority wins (quorum)**: only partitions with a strict majority of the original node set continue running. In `{a,b} | {c,d,e}`, `{c,d,e}` keeps running; `{a,b}` pauses. This is what Raft, ZooKeeper and Consul do.
- **Static preference (fencing)**: a pre-configured "primary" node takes precedence; the side lacking the primary pauses. Simpler but loses availability on primary failure.

BEAM does not provide quorum out of the box (Mnesia has partial support via `:set_master_nodes` / `:dynamic`). We implement a quorum-based guard that monitors cluster size, compares it to the configured expected size, and stops a local "worker" supervisor when the local partition lacks a majority.

```
split_brain_guard/
├── lib/
│   └── split_brain_guard/
│       ├── application.ex
│       ├── quorum.ex
│       ├── guard.ex
│       └── worker.ex
├── test/
│   └── split_brain_guard/
│       ├── quorum_test.exs
│       └── guard_test.exs
├── bench/
│   └── quorum_bench.exs
└── mix.exs
```

## Why quorum and not "last write wins"

LWW loses data. If both sides accept writes during a partition, LWW picks one, discards the other. For idempotent caches it is acceptable. For orders, payments, events — unacceptable.

Quorum trades availability for consistency (CAP): the minority side cannot write, but nothing is ever lost on merge because the minority never wrote. This matches what users of stateful systems actually expect.

## Why minority-pause and not kill-the-minority

Killing the minority is the simplest pattern (used by ZooKeeper via session expiry), but on BEAM we can do better: keep the minority processes alive (so they resume quickly on heal) but stop **side-effecting** work. New inbound requests get `:service_unavailable`. When the partition heals, the guard re-enables services without a restart.

## Core concepts

### 1. Expected cluster size

You must tell the guard what the full cluster looks like. Either:

- static list: `[:"a@h", :"b@h", :"c@h", :"d@h", :"e@h"]` — works for fixed-size deployments,
- dynamic with a discovery hook (libcluster + last-known-good snapshot): more complex but necessary when you autoscale.

Dynamic quorum is hard: if you resize from 3 → 5 → 3, the guard needs to know the "current" expected size, or it can deadlock on "I see 3 but expected 5". For this exercise we use static expected size.

### 2. Majority rule

`majority = div(expected, 2) + 1`. For expected=5, majority=3. A partition of 2 is minority; a partition of 3 is majority.

### 3. Even-sized clusters

An even cluster (2, 4, 6) can split exactly in half — **both halves are minority**. Both pause. This is why Raft / etcd strongly recommend odd cluster sizes. A 4-node cluster gives worse availability than a 3-node one.

### 4. Detection via `:net_kernel.monitor_nodes`

Every `:nodeup` / `:nodedown` updates the observed set. The guard recomputes whether the local partition has majority and enables/disables the worker supervisor accordingly.

## Design decisions

- **Option A — tombstone on merge**: both sides keep running; on merge, reconcile using vector clocks or CRDT. Chosen by Riak, Cassandra. Correct but complex; requires every data structure to be a CRDT.
- **Option B — quorum-based pause** (chosen): minority side stops accepting work. Simple, robust, well-understood. Loses availability on even splits.
- **Option C — external arbiter (etcd / consul)**: a non-BEAM witness that decides for you. Solves even-split ambiguity but adds infra.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule SplitBrainGuard.MixProject do
  use Mix.Project

  def project do
    [app: :split_brain_guard, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {SplitBrainGuard.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: Quorum predicate

```elixir
# lib/split_brain_guard/quorum.ex
defmodule SplitBrainGuard.Quorum do
  @moduledoc "Pure quorum math — no side effects, trivial to test."

  @type decision :: :majority | :minority | :tied

  @spec decide([node()], non_neg_integer()) :: decision()
  def decide(visible_nodes, expected_size)
      when is_list(visible_nodes) and is_integer(expected_size) and expected_size > 0 do
    local_size = length(visible_nodes)
    majority = div(expected_size, 2) + 1

    cond do
      local_size >= majority -> :majority
      local_size * 2 == expected_size -> :tied
      true -> :minority
    end
  end
end
```

### Step 2: Worker supervisor (suspendable)

```elixir
# lib/split_brain_guard/worker.ex
defmodule SplitBrainGuard.Worker do
  @moduledoc """
  A stand-in for your stateful workload. Exposes enable/disable so the guard
  can pause it on minority partition.
  """
  use GenServer
  require Logger

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  def process(task), do: GenServer.call(__MODULE__, {:process, task})
  def enable, do: GenServer.cast(__MODULE__, :enable)
  def disable(reason), do: GenServer.cast(__MODULE__, {:disable, reason})
  def status, do: GenServer.call(__MODULE__, :status)

  @impl true
  def init(_), do: {:ok, %{enabled: true, reason: nil}}

  @impl true
  def handle_call({:process, _}, _from, %{enabled: false, reason: reason} = s) do
    {:reply, {:error, {:unavailable, reason}}, s}
  end

  def handle_call({:process, task}, _from, %{enabled: true} = s) do
    {:reply, {:ok, {:processed, task, Node.self()}}, s}
  end

  def handle_call(:status, _from, s), do: {:reply, s, s}

  @impl true
  def handle_cast(:enable, s) do
    Logger.info("worker: ENABLED")
    {:noreply, %{s | enabled: true, reason: nil}}
  end

  def handle_cast({:disable, reason}, s) do
    Logger.warning("worker: DISABLED (#{inspect(reason)})")
    {:noreply, %{s | enabled: false, reason: reason}}
  end
end
```

### Step 3: The guard

```elixir
# lib/split_brain_guard/guard.ex
defmodule SplitBrainGuard.Guard do
  use GenServer
  require Logger

  alias SplitBrainGuard.{Quorum, Worker}

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  def evaluate, do: GenServer.call(__MODULE__, :evaluate)

  @impl true
  def init(opts) do
    expected = Keyword.fetch!(opts, :expected_size)
    :net_kernel.monitor_nodes(true, node_type: :visible)
    state = %{expected: expected, last_decision: nil}
    {:ok, react(state)}
  end

  @impl true
  def handle_info({:nodeup, node, _}, state) do
    Logger.info("guard: nodeup #{node}")
    {:noreply, react(state)}
  end

  def handle_info({:nodedown, node, _}, state) do
    Logger.warning("guard: nodedown #{node}")
    {:noreply, react(state)}
  end

  @impl true
  def handle_call(:evaluate, _from, state) do
    new_state = react(state)
    {:reply, new_state.last_decision, new_state}
  end

  defp react(state) do
    visible = [Node.self() | Node.list(:visible)]
    decision = Quorum.decide(visible, state.expected)

    if decision != state.last_decision do
      apply_decision(decision)
    end

    %{state | last_decision: decision}
  end

  defp apply_decision(:majority), do: Worker.enable()
  defp apply_decision(:minority), do: Worker.disable(:minority_partition)
  defp apply_decision(:tied), do: Worker.disable(:tied_partition)
end
```

### Step 4: Application

```elixir
# lib/split_brain_guard/application.ex
defmodule SplitBrainGuard.Application do
  use Application

  @impl true
  def start(_type, _args) do
    expected_size = Application.get_env(:split_brain_guard, :expected_cluster_size, 1)

    children = [
      SplitBrainGuard.Worker,
      {SplitBrainGuard.Guard, expected_size: expected_size}
    ]

    Supervisor.start_link(children, strategy: :rest_for_one, name: SplitBrainGuard.Supervisor)
  end
end
```

## Data flow diagram

```
  Cluster expected size = 5
  Partition occurs: {a, b} | {c, d, e}

  Node A (side {a,b}):
    :net_kernel sees [a, b] → 2 nodes visible
    Quorum.decide([a, b], 5) → :minority
    Worker.disable(:minority_partition)
    Incoming Worker.process(task) → {:error, {:unavailable, :minority_partition}}

  Node C (side {c,d,e}):
    :net_kernel sees [c, d, e] → 3 nodes visible
    Quorum.decide([c, d, e], 5) → :majority
    Worker.enable()
    Worker.process(task) → {:ok, {:processed, task, c@h}}

  Partition heals:
    All nodes see 5 visible
    Guard on {a,b} transitions :minority → :majority
    Worker.enable()
```

## Why this works

The guard installs exactly one side-effect source: transitions between `last_decision` states. Repeated `:nodeup` events on the same topology never re-enable the worker twice. Because `Quorum.decide/2` is pure, we can unit-test every partition shape without spinning up real nodes. The majority predicate guarantees at most one side of any split has majority (strict inequality on odd sizes; both sides have tied on even sizes).

## Tests

```elixir
# test/split_brain_guard/quorum_test.exs
defmodule SplitBrainGuard.QuorumTest do
  use ExUnit.Case, async: true
  alias SplitBrainGuard.Quorum

  describe "decide/2 — odd cluster sizes" do
    test "5 nodes: partition of 3 is majority" do
      assert Quorum.decide([:"a@h", :"b@h", :"c@h"], 5) == :majority
    end

    test "5 nodes: partition of 2 is minority" do
      assert Quorum.decide([:"a@h", :"b@h"], 5) == :minority
    end

    test "3 nodes: partition of 2 is majority" do
      assert Quorum.decide([:"a@h", :"b@h"], 3) == :majority
    end

    test "3 nodes: single node is minority" do
      assert Quorum.decide([:"a@h"], 3) == :minority
    end
  end

  describe "decide/2 — even cluster sizes" do
    test "4 nodes: partition of exactly 2 is tied" do
      assert Quorum.decide([:"a@h", :"b@h"], 4) == :tied
    end

    test "4 nodes: partition of 3 is majority" do
      assert Quorum.decide([:"a@h", :"b@h", :"c@h"], 4) == :majority
    end

    test "2 nodes: each alone is tied" do
      assert Quorum.decide([:"a@h"], 2) == :tied
    end
  end

  describe "decide/2 — single node clusters" do
    test "1 node: always majority" do
      assert Quorum.decide([:"a@h"], 1) == :majority
    end
  end
end
```

```elixir
# test/split_brain_guard/guard_test.exs
defmodule SplitBrainGuard.GuardTest do
  use ExUnit.Case, async: false

  alias SplitBrainGuard.{Worker, Guard}

  describe "guard + worker integration on a single node" do
    test "single-node cluster with expected_size=1 → majority, worker enabled" do
      # Application is already started with expected=1 in test env
      assert %{enabled: true} = Worker.status()
      assert {:ok, _} = Worker.process("hello")
    end

    test "simulated minority: worker disabled, processing rejected" do
      :sys.replace_state(Guard, fn state -> %{state | expected: 5} end)
      Guard.evaluate()

      assert %{enabled: false, reason: :minority_partition} = Worker.status()
      assert {:error, {:unavailable, :minority_partition}} = Worker.process("nope")

      # Restore
      :sys.replace_state(Guard, fn state -> %{state | expected: 1} end)
      Guard.evaluate()
      assert %{enabled: true} = Worker.status()
    end
  end
end
```

## Benchmark

```elixir
# bench/quorum_bench.exs
alias SplitBrainGuard.Quorum

Benchee.run(
  %{
    "decide/2 — 5 nodes" => fn ->
      Quorum.decide([:"a@h", :"b@h", :"c@h"], 5)
    end,
    "decide/2 — 50 nodes" => fn ->
      nodes = for i <- 1..30, do: :"node#{i}@h"
      Quorum.decide(nodes, 50)
    end
  },
  time: 3,
  warmup: 1
)
```

Target: < 1 µs. This is pure arithmetic and `length/1`; it is the cheapest part of the system. The expensive part is the distribution-level nodedown detection (bounded by `net_ticktime`).

## Trade-offs and production gotchas

1. **Static expected size is a liability during scaling**: if you resize from 5 to 7 nodes, forgetting to update the config means your new 7-node cluster thinks it has quorum at 3 — which is wrong. Use config-management discipline or dynamic quorum with a witness.
2. **Even-sized clusters are a trap**: 2, 4, 6 nodes → any symmetric split pauses BOTH sides. Always prefer 3, 5, 7. Seriously.
3. **`net_ticktime` controls detection latency**: default 60 s is too slow for split-brain scenarios. Tune to 15 s for cluster-critical services, understanding the false-positive trade-off.
4. **Worker state during pause**: we disable request handling but keep the process alive. If the worker holds big state and memory pressure matters, consider terminating it and restarting on re-enable. Document that state loss is possible.
5. **Cascading decisions**: many services guarded the same way can create a thundering herd on heal as every worker re-enables simultaneously. Stagger re-enables with jitter for anything expensive to warm.
6. **When NOT to use this**: CRDT-based systems (Horde, `:pg`) do not need a guard — they handle partition and merge natively. Imposing a guard on them reduces availability without consistency benefit.

## Reflection

You run a 4-node cluster. The network partitions into `{a, b}` and `{c, d}`. Both sides pause per our guard. Your on-call engineer reboots node `d` thinking it will help. Now the partitions are `{a, b}` (size 2 of 4 → tied → paused) and `{c}` (size 1 of 4 → minority → paused). The service is fully unavailable. What is the fastest correct action, and what is the smallest change to the design that would have prevented this with no additional infra?

## Resources

- [CAP theorem — Gilbert & Lynch](https://users.ece.cmu.edu/~adrian/731-sp04/readings/GL-cap.pdf)
- [Mnesia `:set_master_nodes`](https://www.erlang.org/doc/man/mnesia.html#set_master_nodes-1)
- [Raft paper — Ongaro & Ousterhout](https://raft.github.io/raft.pdf)
- [Call Me Maybe — Kyle Kingsbury (Jepsen)](https://aphyr.com/tags/jepsen)
- [`:net_kernel` tuning](https://www.erlang.org/doc/man/kernel_app.html#net_ticktime)
