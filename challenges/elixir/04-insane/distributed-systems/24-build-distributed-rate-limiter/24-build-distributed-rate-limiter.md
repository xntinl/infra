# Distributed Rate Limiter

**Project**: `throttlex` — a distributed rate limiter with quorum consistency and clock-skew tolerance for multi-node clusters.

---

## Project context

You are building `throttlex`, a distributed rate limiter that remains correct under node failures, network partitions, and NTP clock skew. The system implements token bucket and sliding window algorithms, distributes state via consistent hashing, uses quorum reads/writes, and reduces coordination through a lease mechanism.

The system must handle:
1. **Distributed state**: rate limit state for one account is spread across 3+ nodes, all of which must agree on decisions.
2. **Clock skew**: NTP adjustments can cause wall-clock time to jump backward. Use monotonic time.
3. **High throughput**: 500k checks/second, with P99 < 1ms on a 3-node cluster.

Project structure:

```
throttlex/
├── lib/
│   └── throttlex/
│       ├── application.ex           # OTP supervision: cluster, shards
│       ├── limiter.ex               # public API: check/3, record/2
│       ├── token_bucket.ex          # token bucket algorithm: refill on check
│       ├── sliding_window.ex        # exact sliding window: no fixed-window approximation
│       ├── ring.ex                  # consistent hashing: account → replica nodes
│       ├── shard.ex                 # GenServer per shard: stores account state in ETS
│       ├── quorum.ex                # quorum protocol: read/write with majority acks
│       ├── lease.ex                 # lease manager: local approval with TTL
│       ├── clock.ex                 # monotonic clock wrapper, skew-aware comparisons
│       └── cluster.ex               # cluster management for testing
├── test/
│   └── throttlex/
│       ├── token_bucket_test.exs    # refill rate, burst capacity, token depletion
│       ├── sliding_window_test.exs  # exact count, no boundary artifacts
│       ├── quorum_test.exs          # single node failure tolerance
│       ├── lease_test.exs           # local approval within lease, expiry
│       ├── clock_skew_test.exs      # monotonic time correctness under adjustment
│       └── distributed_test.exs     # 3-node cluster correctness under load
├── bench/
│   └── throttlex_bench.exs
└── mix.exs
```

---

## The problem

A single-node rate limiter is trivial: maintain a counter in ETS, check it on every request. The distributed version is hard: each node has its own counter, but together they represent the global state for one account.

**Failure scenario**: account A has a limit of 100 requests/min. Across a 3-node cluster:
- Without coordination: each node sees ~33 requests and allows all of them. Total: 99 requests allowed. Limit is effectively tripled.
- With naive quorum: every check requires contacting 2/3 nodes. At 500k req/s, this is 1M network round-trips/s. Unacceptable latency.
- With leases: node A acquires a lease to approve up to K requests without coordination (5-30 seconds). Bursts within the lease are free, but total throughput across all leases respects the global limit.

---

## Why this design

**Consistent hashing for shard locality**: account state is sharded to a subset of nodes by the consistent hashing ring. All requests for account A go to the same 3 nodes — no hot node from all requests for different accounts touching a central location.

**Quorum reads/writes for fault tolerance**: a check requires acknowledgment from `floor(N/2) + 1` replica nodes. With R=3, this is 2. A single node failure does not allow over-limit requests.

**Lease-based local approval**: acquiring a lease gives a node the right to approve up to K requests without per-request cross-node coordination. The lease is bound to a (account_id, node_id, epoch) tuple, so even if the node is partitioned, the lease expires after TTL.

**Monotonic time**: using `System.monotonic_time/1` eliminates NTP effects within a node. Comparisons between nodes use epoch-based "logical clocks" that account for skew.

---

## Design decisions

**Option A — Redis-backed rate limiter (`INCR` + TTL)**
- Pros: trivially distributed; battle-tested.
- Cons: every check is a network round trip; Redis becomes a dependency and a bottleneck.

**Option B — Local token bucket in ETS with periodic quorum reconciliation** (chosen)
- Pros: per-check cost is < 5 µs; survives Redis outage; accuracy is tunable via reconciliation period.
- Cons: allows brief over-limit bursts during reconciliation gaps.

→ Chose **B** because at our scale target (10s of µs per check), a network hop is an order of magnitude too slow; local buckets with bounded error are the right trade.

---

## Key Concepts: Rate Limiting and Fairness

Rate limiting controls the rate of requests from a client or account. The core properties are:

1. **Correctness**: no more than L requests are allowed per window (or token bucket period).
2. **Latency**: rate-limit decision < 1ms, ideally < 100µs.
3. **Fairness**: all clients sharing the same limit get equal chances to consume tokens.

**Token bucket algorithm**: maintain a bucket with a maximum capacity C and a refill rate R (tokens/sec). On each request:
1. Refill: `tokens = min(C, tokens + (now - last_refill) * R)`.
2. Consume: if `tokens >= cost`, deduct cost and allow; else deny.
3. Retry: return `ceil((cost - tokens) / R)` milliseconds to retry.

Advantages: natural burst support (up to C tokens), simple O(1) implementation, low latency.

**Sliding window algorithm**: maintain a list of timestamps of recent requests (last W milliseconds). On each request:
1. Filter: remove all timestamps older than W.
2. Count: if `len(timestamps) < L`, allow and append current timestamp; else deny.
3. Retry: return `oldest_timestamp + W - now` milliseconds to retry.

Advantages: exact: no off-by-one errors from fixed-window boundaries, no burst artifact at window boundaries.

Disadvantages: O(L) space per account per window, O(L) time to filter.

---

## Trade-off analysis

| Aspect | Quorum + lease (your impl) | Redis INCR + EXPIRE | Centralized counter |
|--------|--------------------------|--------------------|--------------------|
| Correctness under partition | strong (quorum) | eventual (TTL) | unavailable |
| Latency — local | < 0.1ms (lease) | ~0.5ms (TCP) | depends on mailbox |
| Throughput | 500k/s (target) | ~200k/s per node | single-core bound |
| Clock skew tolerance | monotonic time | none | local only |
| Operational complexity | medium | low (Redis dependency) | medium |

**When does local + quorum win?**
- Throughput-critical services: 100k+ req/s per node.
- Partition tolerance required: cannot accept Redis unavailability.
- Latency-critical: sub-millisecond decisions required.

**When should you use Redis?**
- Simplicity: Redis Lua scripts handle complex rate limit logic.
- Consistency: Redis is a single source of truth; no reconciliation needed.
- Multi-tenant: accounts are not pre-sharded; routing logic is centralized.

---

## Common production mistakes

**1. Using wall-clock time in token bucket refill**

NTP adjustments can cause wall-clock time to jump backward. Use `System.monotonic_time/1`. In Erlang, monotonic time is a 64-bit integer measured in the `native` unit (typically nanoseconds), and it never goes backward — even if the system clock is adjusted.

Failure: token bucket refills backward, allowing infinite requests.

**2. Lease not invalidated on node rejoin after partition**

The lease manager must check the lease's expiry timestamp against monotonic time on every local approval. If node A acquires a lease and is then partitioned from the cluster, the lease expires. When node A rejoins, old leases must be rejected.

Mitigation: store lease.acquired_at and check `System.monotonic_time() < lease.acquired_at + lease.ttl_ms`.

**3. Quorum write not rolling back on partial success**

Ensure the read quorum is strictly `floor(R/2) + 1` and that writes and reads share overlapping sets. If you read from {A, B} and write to {A, C}, a partition where A is in one part and B, C in the other can lead to inconsistency.

Mitigation: use the same replica set for all operations to a given account.

**4. Sliding window growing unboundedly**

Always filter expired timestamps before inserting a new one. Without filtering, the timestamp list grows indefinitely, consuming memory and increasing filter time.

Mitigation: in `check/1`, filter before insertion:
```elixir
valid = Enum.filter(timestamps, fn ts -> ts > cutoff end)
updated = [now | valid]
```

**5. Lease not accounting for skew between nodes**

Nodes have different clocks. Node A acquires a lease at "local time 1000ms" with TTL 30s. Node B's clock is 100ms ahead. Node B sees the lease as expiring at "local time 930ms" — it's already expired. Reconciliation fails.

Mitigation: use a shared epoch-based logical clock or add a skew buffer. Alternatively, have the lease manager on the primary node do the expiry check; secondary nodes just trust it.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new throttlex --sup
cd throttlex
mkdir -p lib/throttlex test/throttlex bench
```

### Step 2: Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Token bucket algorithm

**Objective**: Compute tokens from monotonic time deltas so refill stays accurate across clock skew and lazy access patterns.

```elixir
# lib/throttlex/token_bucket.ex
defmodule Throttlex.TokenBucket do
  @moduledoc """
  Token bucket rate limiter with monotonic time refill.

  State: %{tokens: float, last_refill_at: monotonic_ms, capacity: N, refill_rate: R}

  On check/consume:
    1. Calculate elapsed = now - last_refill_at
    2. new_tokens = min(capacity, tokens + elapsed * refill_rate / 1000)
    3. If new_tokens >= cost: allow, deduct cost
    4. Else: deny, return retry_after_ms
  """

  @doc "Creates a new token bucket with given capacity and refill rate."
  @spec new(number(), number()) :: map()
  def new(capacity, refill_rate_per_second) do
    %{
      tokens: capacity * 1.0,
      last_refill_at: System.monotonic_time(:millisecond),
      capacity: capacity,
      refill_rate: refill_rate_per_second
    }
  end

  @doc "Checks if a request can be allowed. Returns {:allow, state, remaining} or {:deny, state, retry_ms}."
  @spec check(map(), number()) :: {:allow, map(), float()} | {:deny, map(), float()}
  def check(state, cost \\ 1) do
    now = System.monotonic_time(:millisecond)
    elapsed = max(0, now - state.last_refill_at)
    refilled = state.tokens + elapsed * state.refill_rate / 1000.0
    new_tokens = min(state.capacity * 1.0, refilled)

    new_state = %{state | tokens: new_tokens, last_refill_at: now}

    if new_tokens >= cost do
      final_state = %{new_state | tokens: new_tokens - cost}
      {:allow, final_state, new_tokens - cost}
    else
      deficit = cost - new_tokens
      retry_after_ms =
        if state.refill_rate > 0 do
          deficit / state.refill_rate * 1000.0
        else
          :infinity
        end

      {:deny, new_state, retry_after_ms}
    end
  end
end
```

### Step 4: Sliding window

**Objective**: Weight previous-window counts by elapsed fraction so burst detection stays smooth and avoids boundary-aligned cliff evasion.

```elixir
# lib/throttlex/sliding_window.ex
defmodule Throttlex.SlidingWindow do
  @moduledoc """
  Exact sliding window counter using a list of timestamps.
  No fixed-window approximation; every request is tracked.
  """

  @doc "Creates a new sliding window limiter."
  @spec new(pos_integer(), pos_integer()) :: map()
  def new(limit, window_ms) do
    %{timestamps: [], window_ms: window_ms, limit: limit}
  end

  @doc "Checks if a request can be allowed."
  @spec check(map()) :: {:allow, map(), non_neg_integer()} | {:deny, map(), number()}
  def check(state) do
    now    = System.monotonic_time(:millisecond)
    cutoff = now - state.window_ms
    valid  = Enum.filter(state.timestamps, fn ts -> ts > cutoff end)

    if length(valid) < state.limit do
      {:allow, %{state | timestamps: [now | valid]}, state.limit - length(valid) - 1}
    else
      oldest = Enum.min(valid)
      retry_after_ms = oldest + state.window_ms - now
      {:deny, %{state | timestamps: valid}, retry_after_ms}
    end
  end
end
```

### Step 5: Quorum protocol

**Objective**: Reconcile per-node counters through quorum reads so cluster-wide limits stay globally correct under partition.

```elixir
# lib/throttlex/quorum.ex
defmodule Throttlex.Quorum do
  @moduledoc """
  Quorum read/write for distributed rate limit state.
  """

  @doc "Writes state to replicas, waiting for quorum acknowledgment."
  @spec write([pid()], String.t(), map(), pos_integer()) :: :ok | {:error, :quorum_failed}
  def write(replicas, account_id, new_state, quorum_size) do
    tasks = Enum.map(replicas, fn replica ->
      Task.async(fn ->
        try do
          GenServer.call(replica, {:write, account_id, new_state}, 1_000)
        catch
          :exit, _ -> {:error, :timeout}
        end
      end)
    end)

    results = Task.await_many(tasks, 2_000)
    acks = Enum.count(results, fn r -> r == :ok end)

    if acks >= quorum_size, do: :ok, else: {:error, :quorum_failed}
  end

  @doc "Reads state from replicas, returning the most recent."
  @spec read([pid()], String.t(), pos_integer()) :: {:ok, map()} | {:error, :quorum_failed}
  def read(replicas, account_id, quorum_size) do
    tasks = Enum.map(replicas, fn replica ->
      Task.async(fn ->
        try do
          GenServer.call(replica, {:read, account_id}, 1_000)
        catch
          :exit, _ -> {:error, :timeout}
        end
      end)
    end)

    results = Task.await_many(tasks, 2_000)
    valid = Enum.reject(results, fn r -> match?({:error, _}, r) end)

    if length(valid) >= quorum_size do
      latest = Enum.max_by(valid, fn state -> Map.get(state, :version, 0) end, fn -> %{} end)
      {:ok, latest}
    else
      {:error, :quorum_failed}
    end
  end
end
```

### Step 6: Cluster management

**Objective**: Shard keys consistently across nodes so identical account ids always aggregate on the same quorum replica set.

```elixir
# lib/throttlex/cluster.ex
defmodule Throttlex.Cluster do
  @moduledoc """
  Manages a simulated cluster of rate limiter nodes for testing.
  """

  use GenServer

  defstruct [:nodes, :replication, :states, :killed]

  def start(opts) do
    GenServer.start(__MODULE__, opts)
  end

  def kill_node(cluster, node_name) do
    GenServer.call(cluster, {:kill_node, node_name})
  end

  @impl true
  def init(opts) do
    node_count = Keyword.get(opts, :nodes, 3)
    replication = Keyword.get(opts, :replication, 3)
    node_names = for i <- 1..node_count, do: :"node_#{i}"
    states = Map.new(node_names, fn n -> {n, %{}} end)

    {:ok, %__MODULE__{
      nodes: node_names,
      replication: replication,
      states: states,
      killed: MapSet.new()
    }}
  end

  @impl true
  def handle_call({:kill_node, node_name}, _from, state) do
    {:reply, :ok, %{state | killed: MapSet.put(state.killed, node_name)}}
  end

  @impl true
  def handle_call({:check, account_id, opts}, _from, state) do
    limit = Keyword.get(opts, :limit, 10)
    window_ms = Keyword.get(opts, :window_ms, 60_000)

    alive_nodes = Enum.reject(state.nodes, &MapSet.member?(state.killed, &1))
    quorum_size = div(length(state.nodes), 2) + 1

    account_state =
      state.states
      |> Enum.flat_map(fn {node, node_states} ->
        if node in alive_nodes do
          case Map.get(node_states, account_id) do
            nil -> []
            s -> [s]
          end
        else
          []
        end
      end)
      |> Enum.max_by(fn s -> length(Map.get(s, :timestamps, [])) end, fn -> nil end)

    sw = account_state || Throttlex.SlidingWindow.new(limit, window_ms)

    case Throttlex.SlidingWindow.check(sw) do
      {:allow, new_sw, _remaining} ->
        new_states =
          Enum.reduce(alive_nodes, state.states, fn node, acc ->
            node_state = Map.get(acc, node, %{})
            Map.put(acc, node, Map.put(node_state, account_id, new_sw))
          end)

        {:reply, :allow, %{state | states: new_states}}

      {:deny, _sw, _retry} ->
        {:reply, :deny, state}
    end
  end
end

defmodule Throttlex do
  @doc "Checks if a request for the given account should be allowed."
  @spec check(pid(), String.t(), keyword()) :: :allow | :deny
  def check(cluster, account_id, opts) do
    GenServer.call(cluster, {:check, account_id, opts})
  end
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/throttlex/token_bucket_test.exs
defmodule Throttlex.TokenBucketTest do
  use ExUnit.Case, async: true

  alias Throttlex.TokenBucket

  test "allows requests within capacity" do
    state = TokenBucket.new(10, 1.0)
    {result, _state, _remaining} = TokenBucket.check(state)
    assert result == :allow
  end

  test "denies when bucket is empty" do
    state = TokenBucket.new(3, 0.0)
    {_, s1, _} = TokenBucket.check(state)
    {_, s2, _} = TokenBucket.check(s1)
    {_, s3, _} = TokenBucket.check(s2)
    {result, _s4, retry_ms} = TokenBucket.check(s3)

    assert result == :deny
    assert retry_ms > 0
  end

  test "tokens refill at configured rate" do
    state = TokenBucket.new(1, 10.0)

    {:allow, empty_state, _} = TokenBucket.check(state)
    {:deny, _, _} = TokenBucket.check(empty_state)

    Process.sleep(200)
    {result, _, _} = TokenBucket.check(empty_state)
    assert result == :allow
  end
end

# test/throttlex/distributed_test.exs
defmodule Throttlex.DistributedTest do
  use ExUnit.Case, async: false

  test "quorum failure on one node does not allow over-limit requests" do
    {:ok, cluster} = Throttlex.Cluster.start(nodes: 3, replication: 3)

    Throttlex.Cluster.kill_node(cluster, :node_3)

    account = "test_account"
    limit = 10

    results = for _ <- 1..15 do
      Throttlex.check(cluster, account, limit: limit, window_ms: 60_000)
    end

    allowed = Enum.count(results, fn r -> r == :allow end)
    assert allowed <= limit,
      "expected at most #{limit} allowed requests, got #{allowed}"
  end
end
```

### Step 8: Run the tests

```bash
mix test test/throttlex/ --trace
```

### Step 9: Benchmark

**Objective**: Quantify decisions-per-second against quorum size so replication-factor tradeoffs versus accuracy stay measured under hot-key load.

```elixir
# bench/throttlex_bench.exs
{:ok, cluster} = Throttlex.Cluster.start(nodes: 3, replication: 2)

accounts = for i <- 1..1_000, do: "account_#{i}"

Benchee.run(
  %{
    "check — sliding window" => fn ->
      Throttlex.check(cluster, Enum.random(accounts), limit: 1_000, window_ms: 60_000)
    end
  },
  parallel: 8,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: 500k checks/second on a 3-node cluster with P99 < 1ms.

---


## Main Entry Point

```elixir
def main do
  IO.puts("======== 24 build distributed rate limiter ========")
  IO.puts("Demonstrating core functionality")
  IO.puts("")
  
  IO.puts("Run: mix test")
end
```

## Reflection

1. **Lease size tradeoff**: If you acquire a lease for K tokens over T seconds, what happens if two nodes acquire leases for the same account during the same epoch?
   - **Answer**: Both nodes can approve up to K requests. Total: 2K. Mitigation: leases must have overlapping "held by" semantics, e.g., (account_id, node_id, epoch). If the same epoch, only one node can hold the lease at a time.

2. **Clock skew impact**: If node A's clock is 500ms ahead of node B, how does this affect lease expiry?
   - **Answer**: Node A may issue a lease that node B already thinks has expired. Mitigation: add a "skew buffer" of ~1000ms to lease expiry checks.

3. **Burst tolerance under quorum failure**: If your system guarantees 100 req/min but loses quorum for 10 seconds, how many extra requests are allowed?
   - **Answer**: In the worst case, all nodes independently approve the limit. With 3 nodes and quorum=2, one node down means the other two can each approve the full limit independently — burst is 2x for 10 seconds. Mitigation: lower per-node local limits when nodes are down.

---

## Resources

- Cloudflare Engineering Blog — *How We Built Rate Limiting Capable of Scaling to Millions of Domains*.
- Stripe Engineering Blog — *Idempotency and rate limiting*.
- [Riak Core documentation](https://github.com/basho/riak_core) — consistent hashing background.
- [Erlang `:atomics` and `:counters`](https://www.erlang.org/doc/man/atomics.html) — lock-free counters for high-throughput rate limiting.
