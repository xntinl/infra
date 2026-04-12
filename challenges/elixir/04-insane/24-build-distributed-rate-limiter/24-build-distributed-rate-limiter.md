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
│       ├── clock.ex                 # monotonic clock wrapper, skew-aware comparisons
│       └── cluster.ex               # cluster management for testing
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

---

## Why this design

**Consistent hashing for shard locality**: account state is sharded to a subset of nodes by the consistent hashing ring. All requests for account A go to the same 3 nodes.

**Quorum reads/writes for fault tolerance**: a check requires acknowledgment from `floor(N/2) + 1` replica nodes. With R=3, this is 2. A single node failure does not allow over-limit requests.

**Lease-based local approval**: acquiring a lease gives a node the right to approve up to K requests without per-request cross-node coordination.

**Clock-skew tolerance via monotonic time**: using `System.monotonic_time/1` eliminates NTP effects within a node.

---

## Design decisions

**Option A — Redis-backed rate limiter (`INCR` + TTL)**
- Pros: trivially distributed; battle-tested.
- Cons: every check is a network round trip; Redis becomes a dependency and a bottleneck.

**Option B — Local token bucket in ETS with periodic gossip-based reconciliation** (chosen)
- Pros: per-check cost is < 5 µs; survives Redis outage; accuracy is tunable via reconciliation period.
- Cons: allows brief over-limit bursts during reconciliation gaps.

→ Chose **B** because at our scale target (tens of µs per check), a network hop is an order of magnitude too slow; local buckets with bounded error are the right trade.

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

```elixir
# lib/throttlex/sliding_window.ex
defmodule Throttlex.SlidingWindow do
  @moduledoc """
  Exact sliding window counter using a list of timestamps.
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
```

```elixir
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

### Why this works

Each node maintains its own bucket in ETS and gossips consumption deltas periodically. Refill is lazy (computed on read from last-refill-time), so no timer is needed, and the bucket state is a single atomic ETS update.

---

## Benchmark

```elixir
# bench/rl_bench.exs
Benchee.run(%{"check" => fn -> RateLimiter.check(user_id()) end}, time: 10)
```

Target: < 5 µs per check on a single node; accuracy within ±5% under 1k nodes gossiping at 1 Hz.

---

## Trade-off analysis

| Aspect | Quorum + lease (your impl) | Redis INCR + EXPIRE | Centralized counter |
|--------|--------------------------|--------------------|--------------------|
| Correctness under partition | strong (quorum) | eventual | unavailable |
| Latency — local | < 0.1ms (lease) | ~0.5ms (TCP) | depends on mailbox |
| Throughput | 500k/s (target) | ~200k/s per node | single-core bound |
| Clock skew tolerance | monotonic time | none | local only |

Reflection: the lease mechanism allows up to K extra requests if two nodes hold leases simultaneously. How do you calculate the worst-case over-limit factor?

---

## Common production mistakes

**1. Using wall-clock time in token bucket refill**
NTP adjustments can cause wall-clock time to jump backward. Use `System.monotonic_time/1`.

**2. Lease not invalidated on node rejoin after partition**
The lease manager must check the lease's expiry timestamp against monotonic time on every local approval.

**3. Quorum write not rolling back on partial success**
Ensure the read quorum is strictly `floor(R/2) + 1` and that writes and reads share overlapping sets.

**4. Sliding window growing unboundedly**
Always filter expired timestamps before inserting a new one.

## Reflection

- If your system has 100k active users at any moment, would you keep ETS for the buckets, or move to Redis for central accuracy? Justify the tradeoff.
- Under a coordinated attack from 10k clients, would gossip reconciliation still bound the error? Walk through the failure mode.

---

## Resources

- Cloudflare Engineering Blog — *How We Built Rate Limiting Capable of Scaling to Millions of Domains*
- Stripe Engineering Blog — *Idempotency and rate limiting*
- [Riak Core documentation](https://github.com/basho/riak_core) — consistent hashing
- [Erlang `:atomics` and `:counters`](https://www.erlang.org/doc/man/atomics.html) — lock-free counters
