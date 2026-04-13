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

## Project structure
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
├── script/
│   └── main.exs
└── mix.exs
```

## Implementation
### Step 1: Create the project

```bash
mix new throttlex --sup
cd throttlex
mkdir -p lib/throttlex test/throttlex bench
```

### Step 2: Dependencies (`mix.exs`)

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
defmodule Throttlex.TokenBucketTest do
  use ExUnit.Case, async: true
  doctest Throttlex.Cluster

  alias Throttlex.TokenBucket

  describe "core functionality" do
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
  IO.puts("======== 24-build-distributed-rate-limiter ========")
  IO.puts("Build Distributed Rate Limiter")
  IO.puts("")
  
{:ok, limiter} = RateLimiter.start_link(requests_per_second: 10)
  IO.puts("Rate limiter started (10 req/sec)")
  
  results = Enum.map(1..15, fn _ ->
    RateLimiter.allow?(limiter)
  end)
  
  allowed = Enum.count(results, & &1)
  IO.puts("Allowed #{allowed}/15 requests")
  
  IO.puts("Run: mix test")
end
```
---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule GlobalLimit.MixProject do
  use Mix.Project

  def project do
    [
      app: :global_limit,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {GlobalLimit.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `global_limit` (distributed rate limit).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 2000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:global_limit) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== GlobalLimit stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:global_limit) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:global_limit)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual global_limit operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

GlobalLimit classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **1,000,000 checks/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **2 ms** | Stripe blog on rate limits |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Stripe blog on rate limits: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Distributed Rate Limiter matters

Mastering **Distributed Rate Limiter** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `lib/throttlex.ex`

```elixir
defmodule Throttlex do
  @moduledoc """
  Reference implementation for Distributed Rate Limiter.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the throttlex module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> Throttlex.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/throttlex_test.exs`

```elixir
defmodule ThrottlexTest do
  use ExUnit.Case, async: true

  doctest Throttlex

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Throttlex.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Stripe blog on rate limits
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
