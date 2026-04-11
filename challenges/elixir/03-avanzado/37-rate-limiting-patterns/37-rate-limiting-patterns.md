# Rate Limiting Patterns

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` already has a sliding-window rate limiter from the intermediate level
(exercise 71). The operations team now needs two additional algorithms for different
traffic shapes: a token bucket for APIs that tolerate controlled bursts, and a sliding
window counter that scales to high traffic without memory growing proportionally to
requests-per-window.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── rate_limiter/
│           ├── sliding_window.ex         # from exercise 71 (keep as-is)
│           ├── token_bucket.ex           # ← you implement this (Exercise 1)
│           └── sliding_window_counter.ex # ← you implement this (Exercise 2)
├── test/
│   └── api_gateway/
│       └── rate_limiter/
│           ├── token_bucket_test.exs      # given tests
│           └── sliding_window_counter_test.exs
└── mix.exs
```

---

## The business problem

Three rate limiting use cases:

1. **Payment API** — 100 req/min, but allows short bursts (a user submitting a
   form 3 times quickly is normal). Token bucket: fill at 100/60 tokens per second,
   capacity 10. Bursts of up to 10 requests are allowed; steady rate is 100/min.

2. **Webhook ingestion** — 1,000 events/sec per source, no bursts. Fixed-window
   approaches suffer from the boundary burst problem. Sliding window log is exact but
   uses O(events-in-window) memory per source. Sliding window counter approximates
   the same precision with O(1) memory.

3. **Why not a GenServer for each limiter?** A GenServer is a serialization point.
   At 10,000 req/s, all requests queue behind one process. ETS with `write_concurrency`
   allows concurrent writes from multiple schedulers — no single bottleneck.

---

## Algorithm comparison

| Algorithm | Memory | Accuracy | Burst control | Implementation |
|-----------|--------|----------|---------------|---------------|
| Fixed window | O(1) | Boundary burst (2x at reset) | No | Trivial |
| Token bucket | O(1) per key | Exact | Yes (capacity = burst size) | Medium |
| Sliding window log | O(events) per key | Exact | No | Medium |
| Sliding window counter | O(1) per key | ~1% error | No | Medium |

---

## Implementation

### Step 1: `lib/api_gateway/rate_limiter/token_bucket.ex`

The token bucket uses lazy refill: instead of a background process that adds tokens
periodically, we compute how many tokens should have been added since the last check.
This eliminates the need for a timer process and makes the implementation stateless from
a process perspective.

The ETS table stores `{key, tokens_remaining, last_refill_monotonic_ms}` per key.
`System.monotonic_time/1` is used instead of wall clock time because monotonic time
never jumps backward (NTP adjustments can cause `DateTime.utc_now()` to go backward).

```elixir
defmodule ApiGateway.RateLimiter.TokenBucket do
  @moduledoc """
  Token bucket rate limiter backed by ETS.

  Each key (e.g., user ID or IP) has its own bucket:
    {key, tokens_remaining, last_refill_monotonic_ms}

  Tokens refill lazily on each check — no background refill process.
  This makes the implementation stateless from a process perspective:
  no GenServer, no single bottleneck, concurrent writes via ETS.

  Trade-off: the read-compute-write cycle is not atomic. Under extreme
  concurrency, two processes may both read the same token count and both
  consume from it. For rate limiting, a small over-allowance is usually
  acceptable. If strict enforcement is required, serialize via a GenServer
  or use :ets.update_counter/3 for integer-only token counts.
  """

  @table :token_bucket

  @doc "Create the ETS table. Call once at application start."
  def init do
    :ets.new(@table, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true
    ])
    :ok
  end

  @doc """
  Check whether `key` can make a request of `cost` tokens.

  opts:
    capacity:    maximum tokens in the bucket (default 100)
    refill_rate: tokens added per second (default 10)

  Returns:
    {:ok, tokens_remaining}              — allowed; tokens consumed
    {:error, :rate_limited, retry_after_ms} — denied; ms until enough tokens
  """
  @spec check_and_consume(term(), pos_integer(), keyword()) ::
          {:ok, non_neg_integer()} | {:error, :rate_limited, non_neg_integer()}
  def check_and_consume(key, cost \\ 1, opts \\ []) do
    capacity    = Keyword.get(opts, :capacity, 100)
    refill_rate = Keyword.get(opts, :refill_rate, 10)
    now         = System.monotonic_time(:millisecond)

    case :ets.lookup(@table, key) do
      [] ->
        new_tokens = capacity - cost
        :ets.insert(@table, {key, new_tokens, now})
        {:ok, new_tokens}

      [{^key, current_tokens, last_refill}] ->
        elapsed_ms          = now - last_refill
        refilled            = trunc(elapsed_ms * refill_rate / 1_000)
        tokens_after_refill = min(capacity, current_tokens + refilled)

        if tokens_after_refill >= cost do
          new_tokens = tokens_after_refill - cost
          new_last_refill = if refilled > 0, do: now, else: last_refill
          :ets.insert(@table, {key, new_tokens, new_last_refill})
          {:ok, new_tokens}
        else
          tokens_needed = cost - tokens_after_refill
          ms_to_wait = ceil(tokens_needed / max(refill_rate, 1) * 1_000)
          {:error, :rate_limited, ms_to_wait}
        end
    end
  end

  @doc "Reset a key's bucket (useful in tests)."
  @spec reset(term()) :: :ok
  def reset(key) do
    :ets.delete(@table, key)
    :ok
  end

  @doc "Inspect a key's current bucket state."
  @spec inspect_bucket(term()) :: :not_found | map()
  def inspect_bucket(key) do
    case :ets.lookup(@table, key) do
      []                              -> :not_found
      [{^key, tokens, last_refill}]   ->
        %{tokens: tokens, last_refill_ms: last_refill}
    end
  end
end
```

### Step 2: `lib/api_gateway/rate_limiter/sliding_window_counter.ex`

The sliding window counter approximates a sliding window with O(1) memory per key.
It maintains two fixed windows (current and previous) and weights the previous window's
count by how much of it overlaps with the current sliding window.

The formula: `estimated = prev_count * ((window_ms - elapsed_in_current) / window_ms) + current_count`

When the current window expires, it becomes the previous window and a new current window starts.

```elixir
defmodule ApiGateway.RateLimiter.SlidingWindowCounter do
  @moduledoc """
  Sliding window counter rate limiter.

  Approximates a sliding window with O(1) memory using two fixed windows:
  the current window and the previous window. The estimate is:

    count ~ prev_count * ((window_ms - elapsed_in_current) / window_ms)
            + current_count

  Error is at most 1 request per window — accurate enough for most use cases
  while using O(1) memory per key (vs. O(requests-in-window) for the log).

  ETS layout per key:
    {key, current_count, current_window_start, prev_count}
  """

  @table :sliding_window_counter

  @doc "Create the ETS table. Call once at application start."
  def init do
    :ets.new(@table, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true
    ])
    :ok
  end

  @doc """
  Check whether `key` can make a request within the rate limit.

  opts:
    limit:     maximum requests allowed in the window (default 100)
    window_ms: window size in milliseconds (default 60_000)

  Returns:
    {:ok, estimated_count}               — allowed
    {:error, :rate_limited, retry_ms}    — denied; ms until a slot opens
  """
  @spec check_and_record(term(), keyword()) ::
          {:ok, non_neg_integer()} | {:error, :rate_limited, non_neg_integer()}
  def check_and_record(key, opts \\ []) do
    limit     = Keyword.get(opts, :limit, 100)
    window_ms = Keyword.get(opts, :window_ms, 60_000)
    now       = System.monotonic_time(:millisecond)

    {current_count, prev_count, window_start} =
      case :ets.lookup(@table, key) do
        [] ->
          {0, 0, now}

        [{^key, cur, win_start, prev}] ->
          if now - win_start >= window_ms do
            {0, cur, now}
          else
            {cur, prev, win_start}
          end
      end

    elapsed_in_current = now - window_start
    prev_weight        = (window_ms - elapsed_in_current) / window_ms
    estimated          = prev_count * prev_weight + current_count

    if estimated < limit do
      :ets.insert(@table, {key, current_count + 1, window_start, prev_count})
      {:ok, trunc(estimated) + 1}
    else
      retry_ms = trunc(window_ms / max(limit, 1))
      {:error, :rate_limited, retry_ms}
    end
  end

  @doc "Reset a key's counters."
  @spec reset(term()) :: :ok
  def reset(key) do
    :ets.delete(@table, key)
    :ok
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/rate_limiter/token_bucket_test.exs
defmodule ApiGateway.RateLimiter.TokenBucketTest do
  use ExUnit.Case, async: false  # shares named ETS table

  alias ApiGateway.RateLimiter.TokenBucket

  setup_all do
    TokenBucket.init()
    :ok
  end

  setup do
    key = "user_#{System.unique_integer([:positive])}"
    {:ok, key: key}
  end

  test "first request is allowed and returns remaining tokens", %{key: key} do
    assert {:ok, remaining} = TokenBucket.check_and_consume(key, 1, capacity: 10)
    assert remaining == 9
  end

  test "requests within capacity are all allowed", %{key: key} do
    for _ <- 1..10 do
      assert {:ok, _} = TokenBucket.check_and_consume(key, 1, capacity: 10, refill_rate: 0)
    end
  end

  test "request beyond capacity is denied with retry_after", %{key: key} do
    # Exhaust the bucket
    for _ <- 1..10 do
      TokenBucket.check_and_consume(key, 1, capacity: 10, refill_rate: 0)
    end

    assert {:error, :rate_limited, retry_after} =
      TokenBucket.check_and_consume(key, 1, capacity: 10, refill_rate: 1)

    assert is_integer(retry_after)
    assert retry_after > 0
  end

  test "tokens refill over time", %{key: key} do
    # Exhaust
    TokenBucket.check_and_consume(key, 10, capacity: 10, refill_rate: 100)

    # Wait for 100ms at 100 tokens/sec -> 10 tokens refilled
    Process.sleep(110)

    assert {:ok, _} =
      TokenBucket.check_and_consume(key, 5, capacity: 10, refill_rate: 100)
  end

  test "tokens never exceed capacity", %{key: key} do
    # Allow a very long time to pass (conceptually — test does not sleep)
    # By setting a tiny initial bucket we can verify the cap
    assert {:ok, remaining} =
      TokenBucket.check_and_consume(key, 1, capacity: 5, refill_rate: 0)
    assert remaining == 4
    assert remaining <= 5
  end

  test "reset clears the bucket", %{key: key} do
    TokenBucket.check_and_consume(key, 5, capacity: 10)
    TokenBucket.reset(key)
    # After reset, next call initializes a fresh bucket
    assert {:ok, 9} = TokenBucket.check_and_consume(key, 1, capacity: 10, refill_rate: 0)
  end
end
```

```elixir
# test/api_gateway/rate_limiter/sliding_window_counter_test.exs
defmodule ApiGateway.RateLimiter.SlidingWindowCounterTest do
  use ExUnit.Case, async: false

  alias ApiGateway.RateLimiter.SlidingWindowCounter

  setup_all do
    SlidingWindowCounter.init()
    :ok
  end

  setup do
    key = "src_#{System.unique_integer([:positive])}"
    {:ok, key: key}
  end

  test "requests within limit are allowed", %{key: key} do
    for _ <- 1..5 do
      assert {:ok, _} =
        SlidingWindowCounter.check_and_record(key, limit: 10, window_ms: 60_000)
    end
  end

  test "request at limit is denied with retry_after", %{key: key} do
    for _ <- 1..10 do
      SlidingWindowCounter.check_and_record(key, limit: 10, window_ms: 60_000)
    end

    assert {:error, :rate_limited, retry_ms} =
      SlidingWindowCounter.check_and_record(key, limit: 10, window_ms: 60_000)

    assert is_integer(retry_ms)
    assert retry_ms > 0
  end

  test "window rolls over and resets the count", %{key: key} do
    # Fill the window
    for _ <- 1..5 do
      SlidingWindowCounter.check_and_record(key, limit: 5, window_ms: 100)
    end

    assert {:error, :rate_limited, _} =
      SlidingWindowCounter.check_and_record(key, limit: 5, window_ms: 100)

    # Wait for the window to expire
    Process.sleep(150)

    assert {:ok, _} =
      SlidingWindowCounter.check_and_record(key, limit: 5, window_ms: 100)
  end

  test "reset clears the counter", %{key: key} do
    for _ <- 1..5 do
      SlidingWindowCounter.check_and_record(key, limit: 5, window_ms: 60_000)
    end

    SlidingWindowCounter.reset(key)

    assert {:ok, 1} =
      SlidingWindowCounter.check_and_record(key, limit: 5, window_ms: 60_000)
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/rate_limiter/token_bucket_test.exs --trace
mix test test/api_gateway/rate_limiter/sliding_window_counter_test.exs --trace
```

---

## Trade-off analysis

| Algorithm | Memory per key | Burst handling | Accuracy | Best for |
|-----------|---------------|---------------|---------|---------|
| Token bucket | O(1) | Yes (capacity = burst size) | Exact | APIs with legitimate burst patterns |
| Sliding window log | O(N) N=requests in window | No | Exact | Low-volume, strict accuracy |
| Sliding window counter | O(1) | No | ~1% error | High-volume webhook ingestion |
| Fixed window | O(1) | Boundary burst (2x at reset) | Exact per window | Legacy systems, simple quotas |

| Concurrency model | Throughput | Strict accuracy |
|------------------|-----------|----------------|
| ETS `write_concurrency` | High | No (read-compute-write race) |
| GenServer (serialized) | Lower (single bottleneck) | Yes |
| `:ets.update_counter/3` | High | Yes (for integer-only counters) |

Reflection: the sliding window counter approximates the sliding window log with O(1)
memory. When is the ~1% inaccuracy unacceptable? Consider financial compliance scenarios
where every request must be counted exactly.

---

## Common production mistakes

**1. Using GenServer for high-throughput rate limiting**
A GenServer processes one message at a time. At 10,000 req/s, all requests queue
behind the single process. Use ETS with `write_concurrency: true` and accept the small
race condition, or use `:ets.update_counter/3` for atomic integer increments.

**2. Token bucket: using wall clock instead of monotonic time**
`DateTime.utc_now()` can jump backward when the system clock is adjusted (NTP). Use
`System.monotonic_time(:millisecond)` for measuring elapsed time in rate limiters.

**3. Sliding window counter: forgetting to weight the previous window**
The approximation formula is `prev * weight + current`. Omitting the weight factor
turns it back into a fixed window, losing the sliding behavior and reintroducing the
boundary burst problem.

**4. ETS table name collisions in tests**
Named ETS tables are global. Running tests with `async: true` and a fixed table name
causes test interference. Use `async: false` for tests that share a named table, or
pass the table name as a parameter and create unique names per test.

**5. Not setting `read_concurrency: true` when reads dominate**
For rate limiters where most requests succeed (reads dominate denials), set both
`read_concurrency: true` and `write_concurrency: true`. Omitting `read_concurrency`
leaves scheduler-level read bottlenecks on multi-core BEAM systems.

---

## Resources

- [ETS — Erlang Docs](https://www.erlang.org/doc/man/ets.html)
- [Rate Limiting Algorithms — Stripe Engineering Blog](https://stripe.com/blog/rate-limiters)
- [Token Bucket vs Leaky Bucket — Cloudflare Blog](https://blog.cloudflare.com/counting-things-a-lot-of-different-things/)
- [Hammer — Elixir rate limiting library](https://github.com/ExHammer/hammer)
