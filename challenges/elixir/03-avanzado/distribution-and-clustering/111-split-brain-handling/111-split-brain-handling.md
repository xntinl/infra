# Split-Brain Handling Strategies

**Project**: `split_brain_demo` — comparing LWW, quorum, and CRDT-merge

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

You run an inventory service across 5 BEAM nodes. Each node keeps a local count of stock
for ~1000 SKUs and gossips changes via `:pg`. Last month a network partition split the
cluster 3-2 for 4 minutes. Both sides kept serving writes. When the partition healed,
the two sides had divergent counts for 217 SKUs. The ops team picked "whichever number
looked bigger" by hand — a thirty-person-hour cleanup and an overselling incident.

Split-brain is not a bug you can patch; it is a consistency/availability trade-off that
must be designed explicitly. Three families of strategies cover most real systems:

1. **Last-Write-Wins (LWW)** — each replica timestamps every write; on merge, the newer
   timestamp wins. Simple, AP, loses concurrent updates.
2. **Quorum writes** — writes require confirmation from majority. Consistent but unavailable
   on the minority side. Works for small fleets with odd node counts.
3. **CRDT merge** — structure the data so that merge is commutative, associative, idempotent.
   No conflict resolution needed. Works for counters, sets, maps — not for arbitrary scalars.

This exercise implements all three on the same domain object (SKU counter), injects a
partition, and compares convergence, data loss, and complexity.

```
split_brain_demo/
├── lib/
│   └── split_brain_demo/
│       ├── application.ex
│       ├── lww_register.ex         # strategy 1
│       ├── quorum_counter.ex       # strategy 2
│       ├── gcounter.ex             # strategy 3 — grow-only CRDT
│       ├── pncounter.ex            # strategy 3 — positive-negative CRDT
│       └── partition.ex            # test harness injects splits
├── test/
│   └── split_brain_demo/
│       ├── lww_register_test.exs
│       ├── quorum_counter_test.exs
│       ├── gcounter_test.exs
│       └── partition_heal_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The CAP tradeoff, concretely

During a partition you must choose:

- **CP (consistency)**: reject writes on the minority side. Serve stale reads or nothing.
- **AP (availability)**: accept writes on both sides. Reconcile on heal.

Neither is universally right. Payments: usually CP. Shopping carts: usually AP. The
strategies below implement both modes.

### 2. Hybrid logical clocks for LWW

Physical wall-clock timestamps break LWW during clock skew — a node whose clock is 2s
ahead wins every merge. Use a **hybrid logical clock**: 64-bit tuple `(physical, logical)`
where `physical` is wall time and `logical` is monotonic counter that advances on ties or
when seen timestamps are in the future.

```
on_local_event(prev_clock):
  now = system_time_ms()
  if prev_clock.physical == now:
    return (now, prev_clock.logical + 1)
  if prev_clock.physical > now:
    return (prev_clock.physical, prev_clock.logical + 1)
  return (now, 0)

on_receive(prev_clock, remote_clock):
  now = system_time_ms()
  max_physical = max(prev_clock.physical, remote_clock.physical, now)
  logical =
    cond:
      max_physical == prev_clock.physical == remote_clock.physical ->
        max(prev_clock.logical, remote_clock.logical) + 1
      max_physical == prev_clock.physical -> prev_clock.logical + 1
      max_physical == remote_clock.physical -> remote_clock.logical + 1
      true -> 0
  return (max_physical, logical)
```

Break ties on equal clocks by node name lexicographically. Deterministic.

### 3. Quorum writes — why odd node counts matter

A write needs majority ack. For N nodes, majority = `floor(N/2) + 1`:

| N | majority |
|---|----------|
| 3 | 2 |
| 5 | 3 |
| 4 | 3 (same as 5 — so 4 nodes = worst of both worlds) |

With N=5 and a 3-2 partition: the side with 3 nodes can still write, the side with 2
cannot. On heal, both sides agree (because writes required majority → minority side saw
no new writes).

Reality check: quorum only works if you also have **fencing** — when the minority side
heals, it must reject any writes it accepted (there shouldn't be any, but clock skew +
optimistic local writes can sneak in). Fencing typically uses epoch numbers incremented
on majority changes.

### 4. State-based CRDTs — grow-only counter (G-Counter)

A G-Counter stores `{node_name => count}`. Each node only increments its own entry. The
value is `sum(all entries)`. Merge is element-wise max:

```
merge(A, B) = %{
  node => max(A[node], B[node])
  for node in keys(A) ∪ keys(B)
}
```

Properties:
- **Commutative**: merge(A, B) = merge(B, A)
- **Associative**: merge(merge(A, B), C) = merge(A, merge(B, C))
- **Idempotent**: merge(A, A) = A

These three properties guarantee convergence regardless of message ordering or duplication.
No consensus needed. This is the foundation of Riak, DynamoDB (kind of), and Horde's
internal state.

### 5. PN-Counter for increments and decrements

G-Counter only grows. For +/- operations, use two G-Counters:

```
PN = %{p: G-Counter-positives, n: G-Counter-negatives}
value(PN) = sum(PN.p) - sum(PN.n)
merge(A, B) = %{p: merge(A.p, B.p), n: merge(A.n, B.n)}
```

Still CRDT. Still convergent.

Limitation: you cannot "undo" beyond what was decremented. The counter can go negative, which
may or may not be semantically valid. For stock inventory, you may need to reject decrements
that would make the local view negative — but other replicas may have different views.
That's where CRDTs end and domain logic begins.

### 6. Strategy comparison

| | LWW | Quorum | G-Counter | PN-Counter |
|---|---|---|---|---|
| Availability | Always | Majority only | Always | Always |
| Concurrent updates | **Last wins, data loss** | Serialized via consensus | Preserved, merged | Preserved, merged |
| Ops supported | set, get | all | increment only | inc, dec |
| Memory per key | 1 value + clock | 1 value + epoch | N values (one per node) | 2N values |
| Merge cost | O(1) | O(log N) via consensus | O(N) | O(N) |
| Good for | cache, config, session | accounts, ledgers | views, likes | inventory, votes |

---

## Implementation

### Step 1: Mix setup

```elixir
defmodule SplitBrainDemo.MixProject do
  use Mix.Project

  def project do
    [app: :split_brain_demo, version: "0.1.0", elixir: "~> 1.15", deps: []]
  end

  def application, do: [extra_applications: [:logger]]
end
```

### Step 2: LWW register with hybrid logical clock

```elixir
defmodule SplitBrainDemo.LwwRegister do
  @moduledoc """
  Last-Write-Wins register using a Hybrid Logical Clock.

  The register holds a single value. Concurrent writes are resolved by HLC
  comparison; ties are broken by lexicographic node name. Data loss: if two
  nodes set different values at the same HLC tick, only one survives.
  """

  @type hlc :: {physical :: non_neg_integer(), logical :: non_neg_integer(), node()}
  @type t :: %__MODULE__{value: term(), clock: hlc()}

  defstruct value: nil, clock: {0, 0, :nonode@nohost}

  @spec new(term()) :: t()
  def new(initial \\ nil) do
    %__MODULE__{value: initial, clock: {System.system_time(:millisecond), 0, node()}}
  end

  @spec set(t(), term()) :: t()
  def set(%__MODULE__{clock: clock} = reg, value) do
    %{reg | value: value, clock: tick_local(clock)}
  end

  @spec merge(t(), t()) :: t()
  def merge(%__MODULE__{} = a, %__MODULE__{} = b) do
    if compare(a.clock, b.clock) == :gt do
      a
    else
      b
    end
  end

  @doc "Used when receiving a remote update — advances the local clock."
  @spec update_from_remote(t(), t()) :: t()
  def update_from_remote(%__MODULE__{} = local, %__MODULE__{} = remote) do
    merged = merge(local, remote)
    %{merged | clock: tick_receive(local.clock, remote.clock)}
  end

  @spec value(t()) :: term()
  def value(%__MODULE__{value: v}), do: v

  # --- HLC internals ---

  defp tick_local({phys, log, _node}) do
    now = System.system_time(:millisecond)

    cond do
      now > phys -> {now, 0, node()}
      now == phys -> {phys, log + 1, node()}
      true -> {phys, log + 1, node()}
    end
  end

  defp tick_receive({lp, ll, _}, {rp, rl, _}) do
    now = System.system_time(:millisecond)
    max_p = Enum.max([now, lp, rp])

    log =
      cond do
        max_p == lp and max_p == rp -> max(ll, rl) + 1
        max_p == lp -> ll + 1
        max_p == rp -> rl + 1
        true -> 0
      end

    {max_p, log, node()}
  end

  defp compare({p1, l1, n1}, {p2, l2, n2}) do
    cond do
      p1 > p2 -> :gt
      p1 < p2 -> :lt
      l1 > l2 -> :gt
      l1 < l2 -> :lt
      n1 > n2 -> :gt
      n1 < n2 -> :lt
      true -> :eq
    end
  end
end
```

### Step 3: G-Counter

```elixir
defmodule SplitBrainDemo.GCounter do
  @moduledoc """
  Grow-only counter. Each node increments its own slot. Merge is pointwise max.
  """

  @type t :: %__MODULE__{entries: %{node() => non_neg_integer()}}

  defstruct entries: %{}

  @spec new() :: t()
  def new, do: %__MODULE__{}

  @spec inc(t(), pos_integer()) :: t()
  def inc(%__MODULE__{entries: e} = c, n \\ 1) when n > 0 do
    %{c | entries: Map.update(e, node(), n, &(&1 + n))}
  end

  @spec value(t()) :: non_neg_integer()
  def value(%__MODULE__{entries: e}), do: e |> Map.values() |> Enum.sum()

  @spec merge(t(), t()) :: t()
  def merge(%__MODULE__{entries: a}, %__MODULE__{entries: b}) do
    merged =
      Map.merge(a, b, fn _k, v1, v2 -> max(v1, v2) end)

    %__MODULE__{entries: merged}
  end
end
```

### Step 4: PN-Counter

```elixir
defmodule SplitBrainDemo.PNCounter do
  @moduledoc """
  Positive-Negative counter. Supports inc/dec; merge is pointwise on both halves.
  Value can go negative — domain logic decides validity.
  """

  alias SplitBrainDemo.GCounter

  @type t :: %__MODULE__{p: GCounter.t(), n: GCounter.t()}

  defstruct p: %GCounter{}, n: %GCounter{}

  @spec new() :: t()
  def new, do: %__MODULE__{p: GCounter.new(), n: GCounter.new()}

  @spec inc(t(), pos_integer()) :: t()
  def inc(%__MODULE__{} = c, amount \\ 1), do: %{c | p: GCounter.inc(c.p, amount)}

  @spec dec(t(), pos_integer()) :: t()
  def dec(%__MODULE__{} = c, amount \\ 1), do: %{c | n: GCounter.inc(c.n, amount)}

  @spec value(t()) :: integer()
  def value(%__MODULE__{p: p, n: n}), do: GCounter.value(p) - GCounter.value(n)

  @spec merge(t(), t()) :: t()
  def merge(%__MODULE__{} = a, %__MODULE__{} = b) do
    %__MODULE__{p: GCounter.merge(a.p, b.p), n: GCounter.merge(a.n, b.n)}
  end
end
```

### Step 5: Quorum counter

```elixir
defmodule SplitBrainDemo.QuorumCounter do
  @moduledoc """
  Write requires majority ack. Under partition, the minority side returns
  {:error, :no_quorum} for writes. Reads are always served locally (best-effort).
  """
  use GenServer

  @type t :: %{
          value: integer(),
          epoch: non_neg_integer(),
          nodes: [node()]
        }

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec inc(integer()) :: {:ok, integer()} | {:error, term()}
  def inc(amount \\ 1), do: GenServer.call(__MODULE__, {:inc, amount})

  @spec get() :: integer()
  def get, do: GenServer.call(__MODULE__, :get)

  @impl true
  def init(opts) do
    nodes = Keyword.get(opts, :cluster_nodes, [node() | Node.list()])
    {:ok, %{value: 0, epoch: 0, nodes: nodes}}
  end

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  def handle_call({:inc, amount}, _from, state) do
    reachable = reachable_nodes(state.nodes)
    quorum = div(length(state.nodes), 2) + 1

    if length(reachable) >= quorum do
      new_state = %{state | value: state.value + amount, epoch: state.epoch + 1}
      replicate(reachable -- [node()], new_state)
      {:reply, {:ok, new_state.value}, new_state}
    else
      {:reply, {:error, :no_quorum}, state}
    end
  end

  @impl true
  def handle_cast({:replicate, from_state}, state) do
    # Replica accepts updates with higher epoch
    if from_state.epoch > state.epoch do
      {:noreply, Map.merge(state, %{value: from_state.value, epoch: from_state.epoch})}
    else
      {:noreply, state}
    end
  end

  defp reachable_nodes(nodes) do
    Enum.filter(nodes, fn
      n when n == node() -> true
      n -> Node.ping(n) == :pong
    end)
  end

  defp replicate([], _state), do: :ok

  defp replicate(nodes, state) do
    Enum.each(nodes, fn n ->
      # cast, best-effort; majority already confirmed by Node.ping above
      GenServer.cast({__MODULE__, n}, {:replicate, state})
    end)
  end
end
```

### Step 6: Tests

```elixir
defmodule SplitBrainDemo.GCounterTest do
  use ExUnit.Case, async: true

  alias SplitBrainDemo.GCounter

  test "merge is commutative" do
    a = %GCounter{entries: %{:a@x => 3, :b@x => 5}}
    b = %GCounter{entries: %{:a@x => 1, :b@x => 7, :c@x => 2}}

    assert GCounter.merge(a, b) == GCounter.merge(b, a)
  end

  test "merge is associative" do
    a = %GCounter{entries: %{:a@x => 3}}
    b = %GCounter{entries: %{:a@x => 5, :b@x => 2}}
    c = %GCounter{entries: %{:b@x => 1, :c@x => 4}}

    left = GCounter.merge(GCounter.merge(a, b), c)
    right = GCounter.merge(a, GCounter.merge(b, c))
    assert left == right
  end

  test "merge is idempotent" do
    a = %GCounter{entries: %{:a@x => 3, :b@x => 5}}
    assert GCounter.merge(a, a) == a
  end

  test "concurrent increments both preserved after merge" do
    # node a increments locally 3 times
    a = GCounter.new() |> Map.put(:entries, %{:a@x => 3})
    # node b increments locally 5 times
    b = GCounter.new() |> Map.put(:entries, %{:b@x => 5})

    merged = GCounter.merge(a, b)
    assert GCounter.value(merged) == 8
  end
end
```

```elixir
defmodule SplitBrainDemo.LwwRegisterTest do
  use ExUnit.Case, async: true

  alias SplitBrainDemo.LwwRegister

  test "later clock wins on merge" do
    a = LwwRegister.new(:a) |> LwwRegister.set("v1")
    Process.sleep(5)
    b = a |> LwwRegister.set("v2")

    merged = LwwRegister.merge(a, b)
    assert LwwRegister.value(merged) == "v2"
  end

  test "merge is commutative" do
    a = LwwRegister.new() |> LwwRegister.set("a")
    Process.sleep(2)
    b = LwwRegister.new() |> LwwRegister.set("b")

    assert LwwRegister.merge(a, b).value == LwwRegister.merge(b, a).value
  end

  test "ties are broken by node name, not randomly" do
    now = System.system_time(:millisecond)

    a = %LwwRegister{value: "one", clock: {now, 0, :"aaa@x"}}
    b = %LwwRegister{value: "two", clock: {now, 0, :"bbb@x"}}

    # :bbb@x > :aaa@x in term order
    assert LwwRegister.merge(a, b).value == "two"
    assert LwwRegister.merge(b, a).value == "two"
  end
end
```

```elixir
defmodule SplitBrainDemo.PartitionHealTest do
  use ExUnit.Case, async: false

  alias SplitBrainDemo.{GCounter, LwwRegister, PNCounter}

  test "PNCounter — concurrent updates on both sides converge" do
    # Simulate: before split, counter is at 10
    initial = PNCounter.new() |> PNCounter.inc(10)

    # Node A's copy receives 3 more increments during partition
    side_a =
      Enum.reduce(1..3, initial, fn _, acc ->
        # Simulate node(), since we're on a single VM, we can't truly differentiate
        # but the math still demonstrates convergence
        PNCounter.inc(acc)
      end)

    # Node B's copy receives 2 decrements during partition
    side_b =
      Enum.reduce(1..2, initial, fn _, acc -> PNCounter.dec(acc) end)

    # Heal: merge both sides — order doesn't matter
    merged1 = PNCounter.merge(side_a, side_b)
    merged2 = PNCounter.merge(side_b, side_a)
    assert PNCounter.value(merged1) == PNCounter.value(merged2)
  end

  test "LWW — data loss on concurrent writes, documented behavior" do
    a = LwwRegister.new("initial")
    Process.sleep(2)

    # simulate two sides of a partition writing concurrently
    side_a = LwwRegister.set(a, "A-value")
    side_b = LwwRegister.set(a, "B-value")

    merged = LwwRegister.merge(side_a, side_b)
    # one of them wins; the other's value is lost — this is LWW, not a bug
    assert merged.value in ["A-value", "B-value"]
  end
end
```

---

## Trade-offs and production gotchas

**1. LWW silently loses writes**
If two sides of a partition set different values "at the same time", only one survives. For
user preferences this is usually fine. For financial transactions, this is catastrophic. Never
use LWW for values where every write matters.

**2. Quorum doesn't help with single-region failures**
If all your quorum nodes are in one AZ and that AZ fails, you have zero availability. Spread
quorum members across failure domains. And remember: 4-node clusters need 3 acks — they're
strictly worse than 3-node clusters for availability.

**3. CRDTs leak metadata**
A G-Counter for 1000 nodes carries 1000 entries forever — even for nodes that left the
cluster. Garbage collection of departed nodes is a separate problem (delta CRDTs, tombstones,
configurable TTL).

**4. State-based CRDTs are bandwidth-heavy**
Every gossip sync ships the whole state. For a G-Counter with 10k entries, that's 160KB per
sync. Use **delta-CRDTs** (ship only deltas since last sync) or **op-based CRDTs** (ship
individual ops + require causal delivery).

**5. CRDT merge preserves but doesn't validate**
A PN-Counter can go negative. If stock = -5, you already oversold. CRDTs converge, but
domain constraints (non-negative, unique keys, etc.) still need enforcement — often by
rejecting operations that would violate invariants, before they enter the CRDT.

**6. HLC depends on semi-sane clocks**
If NTP is broken and a node's clock is 2 years ahead, its HLC dominates forever until
the "future" catches up. Cap the physical component's deviation from local clock; reject
(or clamp) remote updates that look absurdly ahead.

**7. Partition detection lag**
`Node.ping/1` is not instant on a real partition — TCP takes seconds to fail. A write that
"got quorum" at t=0 may find out at t=5 that only 2 of 5 nodes actually received it. Either
use `:erpc` with short timeouts for synchronous ack, or accept write-after-nodedown
reconciliation.

**8. When NOT to pick any of these**
If your data is truly transactional (double-entry bookkeeping, inventory with strong
constraints, seat reservations), you need **consensus**: Raft (via `:ra`), Paxos, or a
CP external database (Postgres with Patroni, FoundationDB, CockroachDB). CRDTs and LWW
are not substitutes for consensus when you need linearizability.

---

## Benchmark

```elixir
Benchee.run(
  %{
    "GCounter.merge (100 entries)" => fn ->
      a = Enum.into(1..100, %{}, fn i -> {:"n#{i}@x", i} end)
      b = Enum.into(1..100, %{}, fn i -> {:"n#{i}@x", i + 1} end)
      SplitBrainDemo.GCounter.merge(
        %SplitBrainDemo.GCounter{entries: a},
        %SplitBrainDemo.GCounter{entries: b}
      )
    end,
    "LwwRegister.merge" => fn ->
      a = SplitBrainDemo.LwwRegister.new() |> SplitBrainDemo.LwwRegister.set(:x)
      b = SplitBrainDemo.LwwRegister.new() |> SplitBrainDemo.LwwRegister.set(:y)
      SplitBrainDemo.LwwRegister.merge(a, b)
    end
  },
  time: 3,
  warmup: 1
)
```

Expected: G-Counter merge at 100 entries is ~10-25 µs (pure map operations); LWW merge is
~1-3 µs (constant-time comparison). Neither is the bottleneck — network sync is.

---

## Resources

- [Marc Shapiro et al. — "A comprehensive study of CRDTs"](https://hal.inria.fr/inria-00555588/document) — the canonical paper
- [Martin Kleppmann — Designing Data-Intensive Applications, ch. 5 & 9](https://dataintensive.net/) — replication and consensus
- [Jepsen — Riak CRDT analysis](https://aphyr.com/posts/285-jepsen-riak) — Kyle Kingsbury's partition tests
- [Delta-CRDT paper (Almeida et al.)](https://arxiv.org/abs/1603.01529) — bandwidth optimizations
- [`:delta_crdt` hex package](https://hex.pm/packages/delta_crdt) — production-ready delta-CRDT in Elixir
- [Horde's CRDT usage](https://github.com/derekkraan/horde) — real-world case study
- [Chris Keathley — "Distributed Elixir"](https://keathley.io/) — patterns blog
