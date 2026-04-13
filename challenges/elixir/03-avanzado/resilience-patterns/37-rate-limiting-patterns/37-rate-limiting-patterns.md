# Rate Limiting Patterns — Token Bucket vs Leaky Bucket vs Sliding Window

**Project**: `rate_limiting_patterns` — three interchangeable rate limiter backends behind a common behaviour, with a comparative benchmark.

---

## Project context

You run a public API with three classes of consumer: partners paying for a
burst-tolerant SLA, free-tier users on a strict budget, and internal services
that must never be throttled. One rate-limiter implementation cannot serve
all three well — token bucket is great for bursts, leaky bucket is great for
protecting a slow downstream, sliding window is great for precise billing.

Your job is to build all three behind a single `RateLimiter` behaviour, then
pick the right one per route. You will benchmark them side-by-side under
contention to ground the decision in numbers rather than tribal knowledge.

```
rate_limiting_patterns/
├── lib/
│   └── rate_limiting_patterns/
│       ├── application.ex
│       ├── rate_limiter.ex              # behaviour
│       ├── token_bucket.ex
│       ├── leaky_bucket.ex
│       └── sliding_window.ex
├── test/
│   └── rate_limiting_patterns/
│       ├── token_bucket_test.exs
│       ├── leaky_bucket_test.exs
│       └── sliding_window_test.exs
├── bench/
│   └── compare_bench.exs
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
### 1. What each algorithm optimizes for

```
Token bucket            ┌──────────┐  consume(1)
  refill r tokens/sec   │ tokens=8 │  ────────▶ tokens=7 → allow
  capacity C (burst)    │ cap=10   │            tokens=0 → deny
                        └──────────┘

Leaky bucket            queue=[req,req,req]      drains 1 req per tick
  fixed drain rate      │                 │   ───────────▶ downstream
  capacity Q            └─────────────────┘      overflow → deny

Sliding window          [ts1,ts2,ts3,ts4,ts5]    count in last W ms
  precise accounting    └───────────────────┘    count ≥ limit → deny
```

- **Token bucket** allows bursts up to `capacity`. Between bursts, tokens
  replenish at `rate/sec`. Used by AWS API Gateway, Stripe, GitHub.
- **Leaky bucket** smooths output. Calls enqueue, a fixed drain services them.
  Excess overflows. Used where downstream cannot absorb bursts (legacy DB).
- **Sliding window** counts timestamps in a rolling window — no burst, no
  queueing, high fidelity. Used where exact budget accounting matters (billing).

### 2. Burst tolerance — why it matters

A customer with `100 req/min` budget sending 100 requests in the first second
is fine for an API with auto-scaling (token bucket) but catastrophic for a
legacy downstream that can only handle 2 req/s (leaky bucket must smooth).

| Algorithm | Allows burst? | Smooths output? | Memory | CPU per check |
|-----------|---------------|-----------------|--------|----------------|
| Token bucket | yes (up to cap) | no | O(1) per key | O(1) |
| Leaky bucket | no | yes | O(1) per key | O(1) |
| Sliding window | no | no | O(N) per key | O(N) |

### 3. State location — GenServer vs ETS vs process dict

For all three algorithms, the state is small (`{tokens, last_refill}` or
`{window_entries}`) and updates are frequent. Putting every mutation through
a GenServer serializes them — at 100k req/s that's a clear bottleneck.

ETS with `write_concurrency: true` and `:ets.update_counter/4` (atomic) is
the production pattern. The GenServer owns the table for lifecycle, but
`check/2` calls hit ETS directly without a message round-trip.

### 4. Monotonic time, not wall-clock

NTP, leap seconds, and VM migrations can jump `System.os_time/0` backwards.
Rate limiters that compute elapsed as `now - last` must use
`System.monotonic_time/1` — it is guaranteed non-decreasing per BEAM node.

### 5. Per-key isolation

One rogue client should not starve others. All three implementations key on
`client_id` (or `{client_id, route}` for finer control). The ETS schema uses
this compound key so a single client's entries never touch another's.

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

### Step 1: The behaviour

**Objective**: Define RateLimiter behaviour contract so token/leaky/sliding implementations are swappable and clients don't depend on specific algorithm.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule RateLimitingPatterns.RateLimiter do
  @moduledoc "Contract every limiter implementation must satisfy."

  @type key :: term()
  @type decision ::
          {:allow, remaining :: non_neg_integer()}
          | {:deny, retry_after_ms :: non_neg_integer()}

  @callback init(opts :: keyword()) :: :ok
  @callback check(key(), opts :: keyword()) :: decision()
end
```

### Step 2: Token bucket — `lib/rate_limiting_patterns/token_bucket.ex`

**Objective**: Implement token bucket with lazy refill so tokens replenish on check/2 without background timers; allows bursts up to capacity_ms via fractional arithmetic.

```elixir
defmodule RateLimitingPatterns.TokenBucket do
  @moduledoc """
  Token bucket rate limiter backed by ETS.

  Refill is computed lazily on each `check/2` — no background process. State per
  key is a 3-tuple `{key, tokens_millis, last_refill_ms}`. `tokens_millis` is
  tokens * 1000 so fractional refill survives in integer arithmetic.
  """
  @behaviour RateLimitingPatterns.RateLimiter

  @table :token_bucket_state

  @impl true
  def init(_opts) do
    case :ets.whereis(@table) do
      :undefined ->
        :ets.new(@table, [
          :named_table,
          :public,
          :set,
          write_concurrency: true,
          read_concurrency: true
        ])

        :ok

      _ ->
        :ok
    end
  end

  @impl true
  def check(key, opts) do
    capacity = Keyword.fetch!(opts, :capacity)
    refill_per_sec = Keyword.fetch!(opts, :refill_per_sec)
    now = System.monotonic_time(:millisecond)

    {tokens_millis, last} = fetch_or_init(key, capacity, now)
    elapsed = max(now - last, 0)
    replenished = elapsed * refill_per_sec
    new_millis = min(tokens_millis + replenished, capacity * 1000)

    if new_millis >= 1000 do
      :ets.insert(@table, {key, new_millis - 1000, now})
      {:allow, div(new_millis - 1000, 1000)}
    else
      missing = 1000 - new_millis
      retry_ms = div(missing * 1000, refill_per_sec) + 1
      :ets.insert(@table, {key, new_millis, now})
      {:deny, retry_ms}
    end
  end

  defp fetch_or_init(key, capacity, now) do
    case :ets.lookup(@table, key) do
      [{^key, tokens_millis, last}] -> {tokens_millis, last}
      [] -> {capacity * 1000, now}
    end
  end
end
```

### Step 3: Leaky bucket — `lib/rate_limiting_patterns/leaky_bucket.ex`

**Objective**: Implement leaky bucket with fixed drain rate so requests queue logically then drain at constant throughput; smooths bursts to protect slow downstreams.

```elixir
defmodule RateLimitingPatterns.LeakyBucket do
  @moduledoc """
  Leaky bucket (virtual queue): track `water_level`, drain at fixed rate, deny
  when level >= capacity. We compute drain lazily — no background drainer.
  """
  @behaviour RateLimitingPatterns.RateLimiter

  @table :leaky_bucket_state

  @impl true
  def init(_opts) do
    case :ets.whereis(@table) do
      :undefined ->
        :ets.new(@table, [:named_table, :public, :set, write_concurrency: true])
        :ok

      _ ->
        :ok
    end
  end

  @impl true
  def check(key, opts) do
    capacity = Keyword.fetch!(opts, :capacity)
    drain_per_sec = Keyword.fetch!(opts, :drain_per_sec)
    now = System.monotonic_time(:millisecond)

    {level_millis, last} =
      case :ets.lookup(@table, key) do
        [{^key, l, t}] -> {l, t}
        [] -> {0, now}
      end

    elapsed = max(now - last, 0)
    drained = elapsed * drain_per_sec
    level_millis = max(level_millis - drained, 0)

    if level_millis + 1000 <= capacity * 1000 do
      :ets.insert(@table, {key, level_millis + 1000, now})
      {:allow, div(capacity * 1000 - (level_millis + 1000), 1000)}
    else
      overflow = level_millis + 1000 - capacity * 1000
      retry_ms = div(overflow * 1000, drain_per_sec) + 1
      :ets.insert(@table, {key, level_millis, now})
      {:deny, retry_ms}
    end
  end
end
```

### Step 4: Sliding window — `lib/rate_limiting_patterns/sliding_window.ex`

**Objective**: Store request timestamps in :bag ETS table so window pruning counts only recent requests; provides exact billing accuracy without burst tolerance.

```elixir
defmodule RateLimitingPatterns.SlidingWindow do
  @moduledoc """
  Exact sliding window: store one timestamp per request in an ETS `:bag`.
  Best when you need precise accounting and the limit is small (< 10_000 per window).
  """
  @behaviour RateLimitingPatterns.RateLimiter

  @table :sliding_window_state

  @impl true
  def init(_opts) do
    case :ets.whereis(@table) do
      :undefined ->
        :ets.new(@table, [:named_table, :public, :bag, write_concurrency: true])
        :ok

      _ ->
        :ok
    end
  end

  @impl true
  def check(key, opts) do
    limit = Keyword.fetch!(opts, :limit)
    window_ms = Keyword.fetch!(opts, :window_ms)
    now = System.monotonic_time(:millisecond)
    cutoff = now - window_ms

    valid = for {^key, ts} <- :ets.lookup(@table, key), ts >= cutoff, do: ts

    if length(valid) < limit do
      :ets.insert(@table, {key, now})
      {:allow, limit - length(valid) - 1}
    else
      oldest = Enum.min(valid)
      {:deny, oldest + window_ms - now + 1}
    end
  end
end
```

### Step 5: Tests (token bucket shown; leaky / sliding analogous)

**Objective**: Write tests for (token bucket shown; leaky / sliding analogous).

```elixir
defmodule RateLimitingPatterns.TokenBucketTest do
  use ExUnit.Case, async: false
  alias RateLimitingPatterns.TokenBucket

  setup do
    TokenBucket.init([])
    :ets.delete_all_objects(:token_bucket_state)
    :ok
  end

  describe "RateLimitingPatterns.TokenBucket" do
    test "allows up to capacity in a burst" do
      for _ <- 1..10 do
        assert {:allow, _} = TokenBucket.check("c1", capacity: 10, refill_per_sec: 1)
      end

      assert {:deny, _} = TokenBucket.check("c1", capacity: 10, refill_per_sec: 1)
    end

    test "refills over time" do
      for _ <- 1..10, do: TokenBucket.check("c2", capacity: 10, refill_per_sec: 100)
      Process.sleep(30)
      assert {:allow, _} = TokenBucket.check("c2", capacity: 10, refill_per_sec: 100)
    end

    test "isolates keys" do
      for _ <- 1..10, do: TokenBucket.check("c3", capacity: 10, refill_per_sec: 1)
      assert {:allow, _} = TokenBucket.check("c4", capacity: 10, refill_per_sec: 1)
    end
  end
end
```

### Step 6: Benchmark — `bench/compare_bench.exs`

**Objective**: Benchmark bench/compare_bench.exs to compare approaches.

```elixir
alias RateLimitingPatterns.{TokenBucket, LeakyBucket, SlidingWindow}

for mod <- [TokenBucket, LeakyBucket, SlidingWindow], do: mod.init([])

# Preload 500 entries per key to exercise the hot path under realistic state.
now = System.monotonic_time(:millisecond)
for _ <- 1..500, do: :ets.insert(:sliding_window_state, {"sw_hot", now})

Benchee.run(
  %{
    "token_bucket" => fn ->
      TokenBucket.check("tb_hot", capacity: 1_000, refill_per_sec: 1_000)
    end,
    "leaky_bucket" => fn ->
      LeakyBucket.check("lb_hot", capacity: 1_000, drain_per_sec: 1_000)
    end,
    "sliding_window (500 entries)" => fn ->
      SlidingWindow.check("sw_hot", limit: 1_000, window_ms: 60_000)
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2
)
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

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Lazy vs eager refill.** Both token and leaky bucket compute refill on read.
Eager (a background GenServer ticking every 10ms) guarantees wall-clock
accuracy but wastes work when no one asks. Lazy is the default unless you have
sub-millisecond SLAs.

**2. Integer arithmetic for sub-tick refill.** Storing `tokens * 1000`
(millitokens) lets you refill less than one token per millisecond without
floating-point drift. `System.monotonic_time(:microsecond)` plus
millitokens/microsecond arithmetic pushes precision further when needed.

**3. Sliding window memory blows up.** At 1000 req/s per client and 60s window
you carry 60k timestamps per client in memory. For > 10k req/s, use a
sliding-log approximation (counters per sub-window) or bite the bullet and
move to Redis with sorted sets.

**4. Clock skew across nodes.** If you cluster and share a limiter via
`:pg` or CRDT-based state, each node's `monotonic_time` is independent.
Either pin state to one node (single point of failure) or use a logical
clock / coordinating backend (Redis).

**5. `update_counter` is atomic; read-then-write is not.** For the token
bucket, you could use `:ets.update_counter/4` with a decrement-but-floor-at-0
operation — see the `fourth element` form of update_counter. It removes the
need for explicit read + insert and halves contention under heavy concurrency.

**6. `write_concurrency: true` enables per-row locking.** Without it, ETS
serializes writes globally. This option is near-free for uniformly-distributed
keys (our case) and essential at > 10k req/s.

**7. Cleanup. Who deletes idle clients?** After a client goes silent for an
hour, their bucket state still occupies a row. Run a periodic
`:ets.select_delete/2` with `last_refill < cutoff`; alternatively accept
unbounded growth for short-lived processes.

**8. When NOT to use any of these.** For global fairness across a fleet of
nodes, in-process limiters diverge. Use a central Redis (Lua script for
atomic check-and-decrement) or a token-budget distribution protocol
(envoy's global rate limit service). For cost-control rate limits
(e.g., OpenAI tokens per minute), use a backend that your billing system
already trusts.

---

## Expected benchmark shape

On a 2023 M2 Pro, 8 parallel workers:

```
token_bucket                    ~   500 ns/op   (single update_counter path: ~200 ns)
leaky_bucket                    ~   550 ns/op
sliding_window (500 entries)    ~  15 µs/op    (30× slower — O(n) iteration)
```

Take-away: sliding window is the "precise but slow" option. Reach for it when
precision beats throughput.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
defmodule RateLimitingPatterns.TokenBucket do
  @moduledoc """
  Token bucket rate limiter backed by ETS.

  Refill is computed lazily on each `check/2` — no background process. State per
  key is a 3-tuple `{key, tokens_millis, last_refill_ms}`. `tokens_millis` is
  tokens * 1000 so fractional refill survives in integer arithmetic.
  """
  @behaviour RateLimitingPatterns.RateLimiter

  @table :token_bucket_state

  @impl true
  def init(_opts) do
    case :ets.whereis(@table) do
      :undefined ->
        :ets.new(@table, [
          :named_table,
          :public,
          :set,
          write_concurrency: true,
          read_concurrency: true
        ])

        :ok

      _ ->
        :ok
    end
  end

  @impl true
  def check(key, opts) do
    capacity = Keyword.fetch!(opts, :capacity)
    refill_per_sec = Keyword.fetch!(opts, :refill_per_sec)
    now = System.monotonic_time(:millisecond)

    {tokens_millis, last} = fetch_or_init(key, capacity, now)
    elapsed = max(now - last, 0)
    replenished = elapsed * refill_per_sec
    new_millis = min(tokens_millis + replenished, capacity * 1000)

    if new_millis >= 1000 do
      :ets.insert(@table, {key, new_millis - 1000, now})
      {:allow, div(new_millis - 1000, 1000)}
    else
      missing = 1000 - new_millis
      retry_ms = div(missing * 1000, refill_per_sec) + 1
      :ets.insert(@table, {key, new_millis, now})
      {:deny, retry_ms}
    end
  end

  defp fetch_or_init(key, capacity, now) do
    case :ets.lookup(@table, key) do
      [{^key, tokens_millis, last}] -> {tokens_millis, last}
      [] -> {capacity * 1000, now}
    end
  end
end

defmodule Main do
  def main do
    IO.puts("✓ Rate Limiting Patterns — Token Bucket vs Leaky Bucket vs Sliding Window")
  - Rate limiting patterns
    - Token bucket implementation
  end
end

Main.main()
```
