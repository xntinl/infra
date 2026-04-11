# Distributed Rate Limiter

**Project**: `throttlex` — a distributed rate limiter with quorum consistency and clock-skew tolerance

---

## Project context

You are building `throttlex`, a distributed rate limiter that remains correct under node failures, network partitions, and NTP clock skew. The system implements both token bucket and sliding window algorithms, distributes state via consistent hashing, uses quorum reads/writes, and reduces coordination through a lease mechanism.

Project structure:

```
throttlex/
├── lib/
│   └── throttlex/
│       ├── application.ex           # cluster supervisor
│       ├── limiter.ex               # public API: check/3, record/2
│       ├── token_bucket.ex          # token bucket algorithm: refill on check
│       ├── sliding_window.ex        # exact sliding window: no fixed-window approximation
│       ├── ring.ex                  # consistent hashing: account → replica nodes
│       ├── shard.ex                 # GenServer per shard: stores account state
│       ├── quorum.ex                # quorum read/write: majority acknowledgment
│       ├── lease.ex                 # lease manager: acquire K-token local authority for T seconds
│       └── clock.ex                 # monotonic clock wrapper, skew-aware comparisons
├── test/
│   └── throttlex/
│       ├── token_bucket_test.exs    # refill rate, burst capacity, token depletion
│       ├── sliding_window_test.exs  # exact count, no boundary artifacts
│       ├── quorum_test.exs          # single node failure tolerance, stale replica detection
│       ├── lease_test.exs           # local approval within lease, expiry behavior
│       └── distributed_test.exs    # 3-node cluster correctness under load
├── bench/
│   └── throttlex_bench.exs
└── mix.exs
```

---

## The problem

A single-node rate limiter is trivial: maintain a counter in ETS, check it on every request. The distributed version is hard: each node has its own counter, but together they represent the global state for one account. When account A sends 100 requests spread across 3 nodes, each node sees only 33 requests. Without coordination, the limit of 100 is effectively tripled.

You need the distributed view to be consistent enough to enforce the limit accurately, while remaining fast enough to meet the sub-millisecond latency target under 500k requests/second.

---

## Why this design

**Consistent hashing for shard locality**: account state is sharded to a subset of nodes by the consistent hashing ring. All requests for account A go to the same 3 nodes (with R=2 replication). This keeps coordination within a small group rather than broadcasting to all N nodes.

**Quorum reads/writes for fault tolerance**: a check requires acknowledgment from `floor(N/2) + 1` replica nodes. With R=3 replicas, this is 2. A single node failure does not allow over-limit requests — the quorum of 2 surviving replicas enforces the limit correctly.

**Lease-based local approval to reduce coordination**: acquiring a lease gives a node the right to approve up to K requests for account A within T seconds, without per-request cross-node coordination. The K tokens are "borrowed" from the global limit. When the lease expires or is exhausted, the node re-coordinates. This trades brief consistency (up to K extra requests if leases are not synchronized) for a 100x reduction in coordination overhead.

**Clock-skew tolerance via monotonic time and skew margin**: NTP can adjust clocks by up to 100ms. A token bucket that uses wall-clock time for refill calculations may refill too early or too late when clocks differ across nodes. Using `System.monotonic_time/1` eliminates NTP effects within a node; the quorum protocol's timestamps must include a skew margin when comparing across nodes.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new throttlex --sup
cd throttlex
mkdir -p lib/throttlex test/throttlex bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Token bucket algorithm

```elixir
# lib/throttlex/token_bucket.ex
defmodule Throttlex.TokenBucket do
  @moduledoc """
  Token bucket rate limiter.

  State: %{tokens: float, last_refill_at: monotonic_ms, capacity: N, refill_rate: R}

  On check/consume:
    1. Calculate elapsed = now - last_refill_at
    2. new_tokens = min(capacity, tokens + elapsed * refill_rate / 1000)
    3. If new_tokens >= cost: new_tokens = new_tokens - cost; allow
    4. Else: deny, return retry_after_ms = (cost - new_tokens) / refill_rate * 1000
  """

  def new(capacity, refill_rate_per_second) do
    %{
      tokens: capacity * 1.0,
      last_refill_at: System.monotonic_time(:millisecond),
      capacity: capacity,
      refill_rate: refill_rate_per_second
    }
  end

  @doc "Returns {:allow, new_state, remaining_tokens} or {:deny, new_state, retry_after_ms}."
  def check(state, cost \\ 1) do
    # TODO: implement refill and consume
  end
end
```

### Step 4: Sliding window without fixed-window artifacts

```elixir
# lib/throttlex/sliding_window.ex
defmodule Throttlex.SlidingWindow do
  @moduledoc """
  Exact sliding window counter using a list of timestamps.

  State: %{timestamps: [monotonic_ms], window_ms: N, limit: L}

  On check:
    1. now = monotonic_ms
    2. cutoff = now - window_ms
    3. valid = filter(timestamps, fn ts -> ts > cutoff end)
    4. if length(valid) < limit: allow, add now to valid
    5. else: deny, retry_after_ms = oldest_in_valid + window_ms - now
  """

  def new(limit, window_ms) do
    %{timestamps: [], window_ms: window_ms, limit: limit}
  end

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

```elixir
# lib/throttlex/quorum.ex
defmodule Throttlex.Quorum do
  @moduledoc """
  Quorum read/write for distributed rate limit state.

  Write quorum: majority of replicas must acknowledge a state update.
  Read quorum: majority of replicas must respond; take the most recent state.

  "Most recent" is determined by a hybrid logical clock timestamp embedded
  in each state value. A replica with a higher timestamp wins on merge.
  """

  def write(replicas, account_id, new_state, quorum_size) do
    # TODO: send update to all replicas concurrently (Task.async)
    # TODO: wait for quorum_size acks with timeout
    # TODO: return :ok or {:error, :quorum_failed}
  end

  def read(replicas, account_id, quorum_size) do
    # TODO: read from all replicas concurrently
    # TODO: wait for quorum_size responses
    # TODO: return the state with the highest HLC timestamp
  end
end
```

### Step 6: Given tests — must pass without modification

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
    state = TokenBucket.new(3, 0.0)  # no refill
    {_, s1, _} = TokenBucket.check(state)
    {_, s2, _} = TokenBucket.check(s1)
    {_, s3, _} = TokenBucket.check(s2)
    {result, _s4, retry_ms} = TokenBucket.check(s3)

    assert result == :deny
    assert retry_ms > 0
  end

  test "tokens refill at configured rate" do
    state = TokenBucket.new(1, 10.0)  # 10 tokens/second

    # Consume the 1 token
    {:allow, empty_state, _} = TokenBucket.check(state)
    {:deny, _, _} = TokenBucket.check(empty_state)

    # Wait 200ms — should have refilled ~2 tokens
    Process.sleep(200)
    {result, _, _} = TokenBucket.check(empty_state)
    assert result == :allow
  end
end
```

```elixir
# test/throttlex/distributed_test.exs
defmodule Throttlex.DistributedTest do
  use ExUnit.Case, async: false

  test "quorum failure on one node does not allow over-limit requests" do
    {:ok, cluster} = Throttlex.Cluster.start(nodes: 3, replication: 3)

    # Kill one replica node
    Throttlex.Cluster.kill_node(cluster, :node_3)

    # Should still enforce the limit correctly with 2 surviving nodes (quorum=2)
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

### Step 7: Run the tests

```bash
mix test test/throttlex/ --trace
```

### Step 8: Benchmark

```elixir
# bench/throttlex_bench.exs
{:ok, cluster} = Throttlex.Cluster.start(nodes: 3, replication: 2)

accounts = for i <- 1..1_000, do: "account_#{i}"

Benchee.run(
  %{
    "check — local (lease active)" => fn ->
      Throttlex.check(cluster, Enum.random(accounts), limit: 1_000, window_ms: 60_000)
    end,
    "check — quorum (no lease)" => fn ->
      Throttlex.check(cluster, "uncached_#{:rand.uniform(1_000_000)}",
                      limit: 1_000, window_ms: 60_000)
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

## Trade-off analysis

| Aspect | Quorum + lease (your impl) | Redis INCR + EXPIRE | Centralized counter (single GenServer) |
|--------|--------------------------|--------------------|-----------------------------------------|
| Correctness under partition | strong (quorum) | eventual (Redis cluster) | unavailable |
| Latency — local | < 0.1ms (lease) | ~0.5ms (TCP to Redis) | depends on mailbox |
| Latency — quorum | ~1ms (2 round trips) | ~1ms | depends on mailbox |
| Throughput | 500k/s (target) | ~200k/s per node | single-core bound |
| Clock skew tolerance | monotonic time + margin | none (NTP dependent) | local only |
| Failover | automatic (quorum) | Sentinel/Cluster | none |

Reflection: the lease mechanism allows up to K extra requests to be approved in a window if two nodes hold leases simultaneously and are not synchronized. How do you calculate the worst-case over-limit factor given lease size K, lease duration T, replication factor R, and refill rate?

---

## Common production mistakes

**1. Using wall-clock time in token bucket refill**
NTP adjustments can cause wall-clock time to jump backward. A refill calculation using `System.os_time(:millisecond)` would compute a negative elapsed time and subtract tokens. Use `System.monotonic_time(:millisecond)` which is guaranteed to be non-decreasing.

**2. Lease not invalidated on node rejoin after partition**
If node A holds a lease for account X granting K tokens and then disconnects and reconnects after the lease expires, it may still have the lease in memory. The lease manager must check the lease's expiry timestamp against monotonic time on every local approval.

**3. Quorum write not rolling back on partial success**
If 2 of 3 replicas acknowledge the write (quorum met) but the 3rd fails to apply it, the 3rd replica has stale state. A subsequent quorum read may include the stale replica and return incorrect state if the quorum happens to include only the stale replica. Ensure the read quorum is strictly `floor(R/2) + 1` and that writes and reads share overlapping sets.

**4. Sliding window growing unboundedly**
The sliding window list grows by one entry per request. Without cleanup of expired timestamps, it grows without bound. Always filter expired timestamps before inserting a new one (done correctly in the provided implementation above, but easy to omit in a first draft).

---

## Resources

- Cloudflare Engineering Blog — *How We Built Rate Limiting Capable of Scaling to Millions of Domains*
- Stripe Engineering Blog — *Idempotency and rate limiting*
- [Riak Core documentation](https://github.com/basho/riak_core) — consistent hashing and virtual node design
- [Erlang `:atomics` and `:counters`](https://www.erlang.org/doc/man/atomics.html) — lock-free in-process counters
