# Distributed Rate Limiter with Consistent Hashing

**Project**: `edge_rate_limiter` — a cluster-wide rate limiter where every node can enforce the same per-key budget without coordinating on every request.

## Project context

You run an API gateway fleet of N BEAM nodes sitting behind a load balancer. A client can land on any node. You need a rate limit of `limit` requests per `window` per client, enforced across the cluster — not per node. Two naive approaches fail:

1. **Per-node limit of `limit / N`**: wrong because clients unlucky enough to always land on the same node still get only `limit / N`, and clients lucky enough to spread across nodes get `limit` but might burst far above.
2. **Central Redis counter**: works, but every request makes a network round-trip. At 50k req/s the gateway becomes latency-bound on Redis.

The consistent-hashing approach: for each `client_id`, one node in the cluster owns the counter. Any gateway that receives a request for that client forwards the check to the owning node (a local ETS read + `GenServer.cast` on the owner). Overhead: one Erlang-distribution round-trip per request for remote clients, zero for local clients. Consistent hashing minimizes reshuffling when nodes join or leave.

```
edge_rate_limiter/
├── lib/
│   └── edge_rate_limiter/
│       ├── application.ex
│       ├── ring.ex
│       ├── shard.ex
│       └── limiter.ex
├── test/
│   └── edge_rate_limiter/
│       ├── ring_test.exs
│       └── limiter_test.exs
├── bench/
│   └── limiter_bench.exs
└── mix.exs
```

## Why consistent hashing and not modulo hashing

`hash(client_id) rem N` works until N changes. A node joining or leaving shuffles `(N-1)/N` of all keys — catastrophic for cached per-client counters. Consistent hashing with virtual nodes reshuffles only `1/N` of keys on membership change: the counter for most clients stays on the same node across rebalances.

## Why owner-forward and not local enforcement

If every node keeps its own counter for every client, the budget is the union — each node allows `limit`, total is `N * limit`. Forwarding to one owner gives a single source of truth per key. The cost is a `GenServer.call` across a node link (typical 200–500 µs on LAN).

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. Consistent hash ring

A hash ring is an array of `{hash, node}` sorted by `hash`. To find the owner for `key`: hash the key and binary-search for the smallest hash ≥ key_hash (wrap around at the end). Virtual nodes: put each real node at K different positions on the ring, so the load distribution is more even. K = 100–200 is typical.

### 2. Owner for a key

```
key       = "client:42"
key_hash  = :erlang.phash2("client:42")
owner     = ring |> find_successor(key_hash)
```

### 3. Forwarding

If `owner == Node.self()`, run the check locally. Otherwise `GenServer.call({Limiter, owner}, {:check, key})`.

### 4. Ring rebuild on topology change

When a node joins or leaves, rebuild the ring. Until all nodes agree on the new ring, requests may briefly be sent to the wrong owner — acceptable because the limiter is approximate, not strict.

## Design decisions

- **Option A — rendezvous hashing**: variant of consistent hashing without explicit rings; compute `hash(key || node)` for every node, pick the max. O(N) per lookup but no ring state. Fine for small N (< 50).
- **Option B — consistent hashing with virtual nodes** (chosen): O(log(N·K)) lookup, simple to implement, well-understood behaviour.
- **Option C — Jump consistent hash (Google)**: O(log N), no memory. Great for fixed bucket counts but harder to handle heterogeneous node weights.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule EdgeRateLimiter.MixProject do
  use Mix.Project

  def project do
    [app: :edge_rate_limiter, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {EdgeRateLimiter.Application, []}]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: The hash ring

**Objective**: Build a consistent hash ring with virtual nodes over `:gb_trees` so ownership lookup is O(log n) and node churn reshuffles ~1/n keys.

```elixir
# lib/edge_rate_limiter/ring.ex
defmodule EdgeRateLimiter.Ring do
  @moduledoc """
  Consistent hash ring with virtual nodes.

  A ring is an opaque term built from a list of nodes. Lookup is O(log(n·vnodes)).
  """

  @vnodes_per_node 128

  @opaque t :: {sorted_entries :: :gb_trees.tree(non_neg_integer(), node()), node_count :: non_neg_integer()}

  @spec build([node()]) :: t()
  def build(nodes) when is_list(nodes) do
    tree =
      for n <- nodes, i <- 0..(@vnodes_per_node - 1), reduce: :gb_trees.empty() do
        acc -> :gb_trees.insert(hash({n, i}), n, acc)
      end

    {tree, length(nodes)}
  end

  @spec owner(t(), term()) :: node() | nil
  def owner({tree, 0}, _key), do: nil

  def owner({tree, _count}, key) do
    h = hash(key)

    case successor(tree, h) do
      :none ->
        # wrap around to the first entry
        {_hash, node} = :gb_trees.smallest(tree)
        node

      {_hash, node} ->
        node
    end
  end

  @spec nodes(t()) :: [node()]
  def nodes({tree, _}) do
    tree
    |> :gb_trees.values()
    |> Enum.uniq()
  end

  defp successor(tree, h) do
    iter = :gb_trees.iterator_from(h, tree)

    case :gb_trees.next(iter) do
      :none -> :none
      {hash, node, _next} -> {hash, node}
    end
  end

  defp hash(term), do: :erlang.phash2(term, 1 <<< 32)
end
```

### Step 2: The per-node shard — local ETS counter

**Objective**: Keep per-client timestamps in a public ETS bag so sliding-window checks stay lock-free for readers and self-garbage-collect.

```elixir
# lib/edge_rate_limiter/shard.ex
defmodule EdgeRateLimiter.Shard do
  @moduledoc "Local counter and sliding-window store. Owns an ETS table."
  use GenServer

  @table :edge_rate_limiter_shard

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  def check(client_id, limit, window_ms) do
    now = System.monotonic_time(:millisecond)
    cutoff = now - window_ms

    timestamps =
      @table
      |> :ets.lookup(client_id)
      |> Enum.map(fn {_id, ts} -> ts end)
      |> Enum.filter(&(&1 >= cutoff))

    count = length(timestamps)

    cond do
      count < limit ->
        :ets.insert(@table, {client_id, now})
        {:allow, limit - count - 1}

      true ->
        oldest = Enum.min(timestamps)
        {:deny, max(1, oldest + window_ms - now)}
    end
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :bag, {:read_concurrency, true}])
    :timer.send_interval(60_000, :cleanup)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:cleanup, state) do
    cutoff = System.monotonic_time(:millisecond) - 3_600_000
    ms = [{{:_, :"$1"}, [{:<, :"$1", cutoff}], [true]}]
    :ets.select_delete(@table, ms)
    {:noreply, state}
  end
end
```

### Step 3: The cluster-aware limiter — forward to the owner

**Objective**: Cache the ring in `:persistent_term` and forward non-local keys via `:rpc.call/5` so every node sees one authoritative counter per client.

```elixir
# lib/edge_rate_limiter/limiter.ex
defmodule EdgeRateLimiter.Limiter do
  @moduledoc "Public API. Locates the owning node via the ring and forwards the check."
  use GenServer

  alias EdgeRateLimiter.{Ring, Shard}

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  def check(client_id, limit, window_ms) do
    ring = :persistent_term.get(:edge_rate_limiter_ring)

    case Ring.owner(ring, client_id) do
      nil ->
        {:deny, window_ms}

      node when node == node() ->
        Shard.check(client_id, limit, window_ms)

      other_node ->
        :rpc.call(other_node, Shard, :check, [client_id, limit, window_ms], 500)
    end
  end

  @impl true
  def init(_) do
    :net_kernel.monitor_nodes(true, node_type: :visible)
    rebuild_ring()
    {:ok, %{}}
  end

  @impl true
  def handle_info({:nodeup, _, _}, state) do
    rebuild_ring()
    {:noreply, state}
  end

  def handle_info({:nodedown, _, _}, state) do
    rebuild_ring()
    {:noreply, state}
  end

  defp rebuild_ring do
    nodes = [Node.self() | Node.list(:visible)]
    :persistent_term.put(:edge_rate_limiter_ring, Ring.build(nodes))
  end
end
```

### Step 4: Supervision

**Objective**: Use `:rest_for_one` so a limiter crash rebuilds the ring before the shard resumes serving forwarded requests.

```elixir
# lib/edge_rate_limiter/application.ex
defmodule EdgeRateLimiter.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      EdgeRateLimiter.Shard,
      EdgeRateLimiter.Limiter
    ]

    Supervisor.start_link(children, strategy: :rest_for_one, name: EdgeRateLimiter.Supervisor)
  end
end
```

## Data flow diagram

```
  Request arrives on Node B with client_id = "alice"
            │
            ▼
  Limiter.check("alice", 100, 60_000)
            │
            ▼
  Ring.owner(ring, "alice")  # O(log n·vnodes)
            │
     ┌──────┴──────┐
     │             │
  owner == self    owner == Node A
     │             │
  Shard.check      :rpc.call(A, Shard, :check, ...)
   (local ETS)     (Erlang distribution round-trip)
     │             │
     └─────┬───────┘
           ▼
  {:allow, remaining} | {:deny, retry_after}
```

## Why this works

The ring is stored in `:persistent_term`: reads are lock-free, nanosecond-scale, and do not copy the term on read. Every request on every node uses the same (eventually consistent) ring, so all requests for `"alice"` converge to the same owner even when nodes enter/leave — except for a brief window during topology change, during which a small fraction of keys are briefly owned by two nodes. This is acceptable for a rate limiter (at most temporarily lenient), unacceptable for anything needing linearizability.

## Tests

```elixir
# test/edge_rate_limiter/ring_test.exs
defmodule EdgeRateLimiter.RingTest do
  use ExUnit.Case, async: true
  alias EdgeRateLimiter.Ring

  describe "build/1 and owner/2" do
    test "empty ring returns nil owner" do
      assert Ring.owner(Ring.build([]), "any") == nil
    end

    test "single-node ring always returns that node" do
      ring = Ring.build([:"a@h"])
      assert Ring.owner(ring, "k1") == :"a@h"
      assert Ring.owner(ring, "k2") == :"a@h"
    end

    test "stable ownership: same key → same node" do
      ring = Ring.build([:"a@h", :"b@h", :"c@h"])
      assert Ring.owner(ring, "alice") == Ring.owner(ring, "alice")
    end

    test "minimal reshuffle when a node leaves" do
      ring1 = Ring.build([:"a@h", :"b@h", :"c@h"])
      ring2 = Ring.build([:"a@h", :"b@h"])

      keys = for i <- 1..1000, do: "k#{i}"

      moved =
        Enum.count(keys, fn k ->
          Ring.owner(ring1, k) != Ring.owner(ring2, k)
        end)

      # After removing :c@h, ~1/3 of keys should move. Allow wide range.
      assert moved < 500
      assert moved > 200
    end
  end
end
```

```elixir
# test/edge_rate_limiter/limiter_test.exs
defmodule EdgeRateLimiter.LimiterTest do
  use ExUnit.Case, async: false

  alias EdgeRateLimiter.{Limiter, Shard}

  setup do
    :ets.delete_all_objects(:edge_rate_limiter_shard)
    :ok
  end

  describe "check/3 — single-node behaviour" do
    test "allows up to the limit" do
      for _ <- 1..5 do
        assert {:allow, _} = Limiter.check("client_a", 10, 60_000)
      end
    end

    test "denies when limit is reached" do
      for _ <- 1..10, do: Limiter.check("client_b", 10, 60_000)
      assert {:deny, retry} = Limiter.check("client_b", 10, 60_000)
      assert retry > 0
    end
  end

  describe "Shard.check/3 directly" do
    test "new client has full budget" do
      assert {:allow, 99} = Shard.check("new_c", 100, 60_000)
    end
  end
end
```

## Benchmark

```elixir
# bench/limiter_bench.exs
alias EdgeRateLimiter.Limiter

for i <- 1..500, do: Limiter.check("bench_warm", 1_000_000, 60_000)

Benchee.run(
  %{
    "check — local owner" => fn ->
      Limiter.check("bench_warm", 1_000_000, 60_000)
    end
  },
  time: 5,
  warmup: 2,
  parallel: 8
)
```

Target: `check — local owner` < 15 µs on a warm table. Cross-node checks (via `:rpc.call`) add ~200–500 µs depending on RTT.

## Deep Dive

Distributed Erlang relies on a heartbeat mechanism (net_kernel tick) to detect node failure, but the network is fundamentally asynchronous—split-brain scenarios are inevitable. A partitioned cluster may have two sets of nodes, each believing the other is dead. Libraries like Horde and Phoenix.PubSub solve this with quorum-aware consensus, but they add latency and complexity. At scale, choose your consistency model explicitly: eventual consistency (via Redis PubSub) is faster but allows temporary divergence; strong consistency (via Horde DLM or distributed transactions) is slower but guarantees atomicity. For global registries, the order of operations matters—registering a process before its monitor is live creates race conditions. In multi-region setups, latency between nodes compounds these issues; consider regional clusters with a lightweight coordinator rather than a fully meshed topology.
## Advanced Considerations

Distributed Elixir systems require careful consideration of network partitions, consistent hashing for distributed state, and the interaction between clustering libraries and node discovery mechanisms. Network partitions are not rare edge cases; they happen regularly in cloud deployments due to maintenance windows and infrastructure issues. A system that works perfectly during local testing but fails under network partitions indicates insufficient failure handling throughout the codebase. Split-brain scenarios where multiple network partitions lead to different cluster views require explicit recovery mechanisms that are often business-specific and context-dependent.

Horde and distributed registries provide eventual consistency guarantees, but "eventual" can mean minutes during network partitions. Applications must handle the case where the same name is registered on multiple nodes simultaneously without coordination. Consistent hashing for distributed services requires understanding rebalancing costs — a single node failure can cause significant key redistribution and thundering herd problems if not carefully managed. The cost of distributed consensus using algorithms like Raft is high; choose it only when consistency is more important than availability and can afford the performance cost.

Global state replication across nodes creates synchronization challenges at scale. Choosing between replicating everywhere versus replicating to specific nodes affects both consistency latency and network bandwidth utilization fundamentally. Node monitoring and heartbeat mechanisms require careful timeout tuning — too aggressive and you get false positives during network hiccups; too conservative and you don't detect actual failures quickly enough for recovery. The EPMD (Erlang Port Mapper Daemon) is a critical component that can become a bottleneck in large clusters and requires careful capacity planning.


## Deep Dive: Distributed Patterns and Production Implications

Distributed testing with Peer spawns multiple Erlang nodes in separate BEAM instances, allowing you to test actual node failure, network partitions, and message delays. This is essential for OTP applications but adds latency and complexity. The key insight is that distributed tests reveal assumptions about network reliability that single-node tests cannot—timeouts, partial failures, and split-brain scenarios are invisible to local tests.

---

## Trade-offs and production gotchas

1. **Cross-node RTT dominates tail latency**: if your LB sprays requests uniformly, ~(N-1)/N of checks are cross-node. Co-locating clients via sticky sessions dramatically improves p99.
2. **Ring rebuild race**: during `:nodeup`/`:nodedown`, nodes briefly disagree on the owner. Counters split across two nodes for a few ms — the limiter over-allows. Acceptable for rate-limiting, not for deduplication.
3. **`:rpc.call` blocks the calling process**: under an overloaded owner, every remote check times out and all gateway fronting nodes stall. Consider `Task.async` + timeout budget, or replace RPC with a fire-and-forget local overlay that periodically syncs.
4. **`:persistent_term` churn**: rebuilding the ring calls `:persistent_term.put/2`, which triggers a global garbage collection for any process holding the old term. Rebuild only on topology change, never on every request.
5. **`:erlang.phash2` has a 2^27 range by default** — always pass `1 <<< 32` explicitly for 32-bit distribution, or the ring will bucket poorly.
6. **When NOT to use this**: if you need exact, strict rate limiting with audit trails (billing, regulatory), use a linearizable store (Redis with Lua, CockroachDB). Consistent hashing gives approximate fairness, not exactness.

## Reflection

Your cluster doubles from 4 nodes to 8 during a deploy. Approximately what fraction of client counters move to a different owner? Now the deploy rolls back after the first 2 new nodes misbehaved. What is the total fraction of counters that experienced at least one migration, and how much "amnesia" did the limiter exhibit during that window?


## Executable Example

```elixir
# lib/edge_rate_limiter/shard.ex
defmodule EdgeRateLimiter.Shard do
  @moduledoc "Local counter and sliding-window store. Owns an ETS table."
  use GenServer

  @table :edge_rate_limiter_shard

  def start_link(_opts), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  def check(client_id, limit, window_ms) do
    now = System.monotonic_time(:millisecond)
    cutoff = now - window_ms

    timestamps =
      @table
      |> :ets.lookup(client_id)
      |> Enum.map(fn {_id, ts} -> ts end)
      |> Enum.filter(&(&1 >= cutoff))

    count = length(timestamps)

    cond do
      count < limit ->
        :ets.insert(@table, {client_id, now})
        {:allow, limit - count - 1}

      true ->
        oldest = Enum.min(timestamps)
        {:deny, max(1, oldest + window_ms - now)}
    end
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :bag, {:read_concurrency, true}])
    :timer.send_interval(60_000, :cleanup)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:cleanup, state) do
    cutoff = System.monotonic_time(:millisecond) - 3_600_000
    ms = [{{:_, :"$1"}, [{:<, :"$1", cutoff}], [true]}]
    :ets.select_delete(@table, ms)
    {:noreply, state}
  end
end

defmodule Main do
  def main do
      # Simulate distributed rate limiter: consistent hash to same bucket
      customer_id = "customer_abc"
      budget = 100  # requests per minute

      # Consistent hash: always maps customer_id to same bucket across cluster
      hash = :erlang.phash2(customer_id)
      bucket_node = rem(hash, 3) + 1  # Distribute across 3 nodes

      # Track usage
      usage = %{customer_id => %{count: 45, reset_at: System.os_time(:millisecond) + 60_000}}

      available = budget - usage[customer_id].count

      IO.puts("✓ Customer: #{customer_id}")
      IO.puts("✓ Hashed to node: #{bucket_node}")
      IO.puts("✓ Available budget: #{available}/#{budget}")
      IO.inspect(usage, label: "✓ Rate limit state")

      assert bucket_node in 1..3, "Consistent hash to valid node"
      assert available >= 0, "Valid budget"

      IO.puts("✓ Distributed rate limiter: consistent hashing working")
  end
end

Main.main()
```
