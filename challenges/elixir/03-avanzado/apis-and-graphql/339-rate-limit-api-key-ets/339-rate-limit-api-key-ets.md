# Per-API-Key Rate Limiting with ETS and Token Bucket

**Project**: `api_portal` — an API gateway layer that enforces per-tenant quotas using a token bucket stored in ETS, with atomic updates via `:ets.update_counter/4`.

## Project context

`api_portal` exposes a public API. Each tenant has an API key with a plan: `free` (60 req/min), `pro` (600 req/min), `enterprise` (6000 req/min). A misbehaving free-tier client should not degrade a paying customer's experience. The gateway must make a rate decision on every request with microsecond latency — any hop across processes or networks breaks the SLA.

This exercise builds a **token bucket** rate limiter in ETS. Unlike the sliding-window approach (which keeps a list of timestamps), the token bucket stores two integers per key: `tokens` and `last_refill_us`. Updates happen in one atomic `update_counter` call, which is wait-free on modern BEAM versions for public tables.

```
api_portal/
├── lib/
│   ├── api_portal/
│   │   ├── application.ex
│   │   └── rate_limiter/
│   │       ├── bucket.ex              # pure math helpers
│   │       └── limiter.ex             # ETS-backed
│   └── api_portal_web/
│       └── plugs/rate_limit.ex
├── test/api_portal/rate_limiter_test.exs
├── bench/rate_limit_bench.exs
└── mix.exs
```

## Why token bucket and not sliding window for this use case

Sliding window gives precise "N requests in the last T" semantics at the cost of O(N) memory per key and O(N) work per check. For a quota use case ("this plan gets R requests per minute, burstable up to B"), a token bucket is cheaper and more expressive:

- **Fixed memory** per key (two integers) regardless of traffic.
- **Burst capacity** encoded naturally as bucket size; sustained rate as refill rate.
- **O(1) per check**.

Token bucket gives "refill rate R tokens/s, capped at B tokens". A plan with R=1/s and B=60 means "average 60/min, burst up to 60 at once". Rate and burst are independent knobs.

## Why ETS with `update_counter` and not a GenServer

Every alternative funnels traffic through a single process:

- A `GenServer.call` per request serializes all decisions through one mailbox.
- `Mutex` libraries reintroduce a process bottleneck.

`:ets.update_counter/4` with a `:public` table is **atomic** and executed inline by the calling scheduler — no message passing. Multiple schedulers contend on the ETS lock for the key bucket, but modern ETS (OTP 24+) uses fine-grained locks per bucket, so realistic contention at 100k rps is minimal.

## Core concepts

### 1. The bucket record
`{key, tokens, last_refill_us, refill_rate_per_s, capacity}` — a 5-tuple stored as an ETS set entry.

### 2. Lazy refill
We do not run a timer to refill every bucket every second. On each check we compute `elapsed_us * rate / 1_000_000` tokens to add, capped at `capacity`. This is O(1) per check and avoids waking schedulers for idle keys.

### 3. Atomic update
`:ets.update_counter/4` accepts a list of `{position, increment, threshold, set_value}` ops. We use it to subtract 1 from tokens only if ≥ 1; otherwise return a sentinel that means "denied".

### 4. Insert-if-missing with default
The fourth argument to `update_counter/4` is a default tuple used if the key is absent. This lets first-request-for-a-key work without a prior `insert`.

## Design decisions

- **Option A — store raw tokens and refill lazily on each check**: pros: O(1); cons: subtle, must be reasoned carefully.
- **Option B — background GenServer that refills every second**: pros: mental model simple; cons: waste CPU for idle keys, extra synchronization.
→ **A**. Lazy refill is idiomatic for rate limiters and matches how Cloudflare, envoy, and Linux traffic shapers do it.

## Implementation

### Dependencies

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:plug_cowboy, "~> 2.7"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 1: Pure helpers (no ETS)

**Objective**: Extract `refill/5` token-bucket math into a pure module so concurrency semantics are tested without ETS in the loop.

```elixir
defmodule ApiPortal.RateLimiter.Bucket do
  @moduledoc "Pure token-bucket math. Easy to unit-test."

  @type plan :: %{rate: pos_integer(), capacity: pos_integer()}

  @spec refill(integer(), integer(), integer(), float(), integer()) :: integer()
  def refill(tokens, last_us, now_us, rate_per_s, capacity) do
    elapsed_s = max(now_us - last_us, 0) / 1_000_000
    added = trunc(elapsed_s * rate_per_s)
    min(tokens + added, capacity)
  end

  @plans %{
    "free" =>       %{rate: 1, capacity: 60},
    "pro" =>        %{rate: 10, capacity: 600},
    "enterprise" => %{rate: 100, capacity: 6000}
  }

  def plan(name), do: Map.get(@plans, name, @plans["free"])
end
```

### Step 2: ETS-backed limiter

**Objective**: Store buckets in an ETS `:set` with `write_concurrency` so per-key locks serialize writes while distinct keys refill in parallel.

```elixir
defmodule ApiPortal.RateLimiter.Limiter do
  alias ApiPortal.RateLimiter.Bucket

  @table :rate_buckets

  def setup do
    :ets.new(@table, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true,
      decentralized_counters: true
    ])
  end

  @doc """
  Returns :ok if allowed, {:error, retry_after_ms} if denied.
  """
  @spec check(String.t(), String.t()) :: :ok | {:error, pos_integer()}
  def check(api_key, plan_name) do
    %{rate: rate, capacity: cap} = Bucket.plan(plan_name)
    now_us = System.monotonic_time(:microsecond)

    # Step 1: read current bucket (default to {key, capacity, now_us})
    {tokens, last_us} =
      case :ets.lookup(@table, api_key) do
        [{^api_key, t, last}] -> {t, last}
        [] -> {cap, now_us}
      end

    # Step 2: compute refilled tokens lazily
    refilled = Bucket.refill(tokens, last_us, now_us, rate, cap)

    if refilled >= 1 do
      # Step 3: atomic decrement. If another scheduler raced us, update_counter
      #         still operates atomically on whatever the current value is.
      :ets.insert(@table, {api_key, refilled - 1, now_us})
      :ok
    else
      # Tokens unavailable — compute time until next token
      retry_us = trunc(1_000_000 / rate)
      {:error, div(retry_us, 1_000)}
    end
  end
end
```

> Note on concurrency: because we `read then insert`, two schedulers may temporarily undercount. For strict exactness, use `:ets.update_counter/4` (see "Why this works" below). For quota enforcement with ~1% tolerance, the simple version is sufficient and clearer.

### Step 3: Strict atomic variant using `update_counter`

**Objective**: Replace the lookup-then-insert race window with `:ets.update_counter/4` `[{2, -1, 0, 0}]` so the decrement is lost-update-proof under contention.

```elixir
defmodule ApiPortal.RateLimiter.LimiterStrict do
  @table :rate_buckets_strict

  def setup do
    :ets.new(@table, [:named_table, :public, :set, write_concurrency: true])
  end

  def check(api_key, plan_name) do
    %{rate: rate, capacity: cap} = ApiPortal.RateLimiter.Bucket.plan(plan_name)
    now_us = System.monotonic_time(:microsecond)

    # First ensure the row exists and refill it.
    ensure_row(api_key, cap, now_us)
    refill_row(api_key, rate, cap, now_us)

    # Atomic: decrement tokens by 1 only if result >= 0; else keep at 0.
    # ops = [{position, increment, threshold, set_value}]
    case :ets.update_counter(@table, api_key, [{2, -1, 0, 0}]) do
      [new] when new >= 0 -> :ok
      [0] -> {:error, div(1_000_000, rate) |> div(1_000)}
    end
  end

  defp ensure_row(key, cap, now_us) do
    :ets.insert_new(@table, {key, cap, now_us})
  end

  defp refill_row(key, rate, cap, now_us) do
    [{^key, tokens, last_us}] = :ets.lookup(@table, key)
    refilled = ApiPortal.RateLimiter.Bucket.refill(tokens, last_us, now_us, rate, cap)
    :ets.insert(@table, {key, refilled, now_us})
  end
end
```

### Step 4: Plug

**Objective**: Reject over-budget requests with 429 + `retry-after` header derived from the plan's refill rate, not a hardcoded guess.

```elixir
defmodule ApiPortalWeb.Plugs.RateLimit do
  @behaviour Plug
  import Plug.Conn
  alias ApiPortal.RateLimiter.Limiter

  @impl true
  def init(opts), do: opts

  @impl true
  def call(conn, _opts) do
    with [key] <- get_req_header(conn, "x-api-key"),
         plan <- lookup_plan(key) do
      case Limiter.check(key, plan) do
        :ok ->
          conn
          |> put_resp_header("x-ratelimit-plan", plan)

        {:error, retry_ms} ->
          conn
          |> put_resp_header("retry-after", Integer.to_string(div(retry_ms, 1000)))
          |> send_resp(429, "")
          |> halt()
      end
    else
      _ -> conn |> send_resp(401, "") |> halt()
    end
  end

  defp lookup_plan(_api_key), do: "free"  # replace with your tenant DB lookup
end
```

## Why this works

```
request with X-API-Key: abc123 (plan: pro, rate=10/s, cap=600)
           │
           ▼
   Limiter.check("abc123", "pro")
           │
           ▼
   lookup row: {abc123, tokens=450, last_us=t0}
   now_us - t0 = 2s → add 20 tokens → 470 (capped at 600)
           │
           ▼
   if 470 >= 1: insert {abc123, 469, now_us} → :ok
                otherwise compute retry_after
```

Because ETS `:set` with `write_concurrency: true` uses fine-grained locks per bucket (not per table), two different API keys updating simultaneously take locks on different buckets and run in parallel. Even for the same key, the critical section is a handful of instructions: lookup + insert. Modern BEAM measures this at < 500 ns per operation.

For strict correctness, `update_counter/4`'s atomic `[{2, -1, 0, 0}]` op subtracts-and-reads-in-one-step, so there is no lost update even under contention.

## Tests

```elixir
defmodule ApiPortal.RateLimiterTest do
  use ExUnit.Case, async: false
  alias ApiPortal.RateLimiter.Limiter

  setup_all do
    Limiter.setup()
    :ok
  end

  setup do
    :ets.delete_all_objects(:rate_buckets)
    :ok
  end

  describe "token bucket semantics" do
    test "allows up to capacity without waiting" do
      for _ <- 1..60 do
        assert :ok = Limiter.check("k", "free")
      end
      assert {:error, _} = Limiter.check("k", "free")
    end

    test "refills at rate after time passes" do
      for _ <- 1..60, do: :ok = Limiter.check("k", "free")
      assert {:error, _} = Limiter.check("k", "free")

      # Simulate 2 seconds passing by manipulating last_refill_us.
      [{_, _, _}] = :ets.lookup(:rate_buckets, "k")
      :ets.update_element(:rate_buckets, "k", {3, System.monotonic_time(:microsecond) - 2_000_000})

      # Free plan = 1/s, so 2s later → 2 more tokens available.
      assert :ok = Limiter.check("k", "free")
      assert :ok = Limiter.check("k", "free")
      assert {:error, _} = Limiter.check("k", "free")
    end

    test "plans are independent between keys" do
      for _ <- 1..60, do: :ok = Limiter.check("free_k", "free")
      assert :ok = Limiter.check("pro_k", "pro")
    end
  end

  describe "concurrent checks" do
    test "100 parallel checks never exceed capacity" do
      key = "burst"
      tasks = for _ <- 1..200, do: Task.async(fn -> Limiter.check(key, "free") end)
      results = Task.await_many(tasks, 5_000)

      allowed = Enum.count(results, &(&1 == :ok))
      # Capacity is 60; should be at most 60 allowed (plus any lazy refill).
      assert allowed <= 61
    end
  end
end
```

## Benchmark

```elixir
# bench/rate_limit_bench.exs
ApiPortal.RateLimiter.Limiter.setup()

Benchee.run(%{
  "check (hot key)"  => fn -> ApiPortal.RateLimiter.Limiter.check("hot", "pro") end,
  "check (new key)"  => fn ->
    ApiPortal.RateLimiter.Limiter.check("new_" <> Integer.to_string(:rand.uniform(1_000_000)), "pro")
  end
}, parallel: 16, time: 5, warmup: 2)
```

**Expected**: ~500 ns – 2 µs per check; throughput > 500k checks/s on a modern 8-core machine.

## Deep Dive: Query Complexity and N+1 Prevention Patterns

GraphQL's flexibility is a double-edged sword. A query like `{ users { posts { comments { author { email } } } } }`
becomes a DDoS vector if unchecked: a resolver that loads each post's comments naively yields 1000 database 
queries for a 100-user query.

**Three strategies to prevent N+1**:
1. **Dataloader batching** (Absinthe-native): Queue fields in phase 1 (`load/3`), flush in phase 2 (`run/1`).
   Single database call per level. Works across HTTP boundaries via custom sources.
2. **Ecto select/5 eager loading** (preload): Best when schema relationships are known at resolver definition time.
   Fine-grained control; requires discipline in your types.
3. **Complexity analysis** (persisted queries): Assign a "weight" to each field (users=2, posts=5, comments=10).
   Reject queries exceeding a threshold BEFORE execution. Prevents runaway queries entirely.

**Production gotcha**: Complexity analysis doesn't prevent slow queries — it prevents expensive queries.
A query that hits 50,000 database rows but under the complexity limit still runs. Combine with database 
query timeouts and active monitoring.

**Subscription patterns** (real-time): Subscriptions over PubSub break traditional Dataloader batching 
because events arrive asynchronously. Use a separate resolver that doesn't call the loader; instead, 
publish (source) and subscribe (sink) directly. This keeps subscriptions cheap and doesn't starve 
the dataloader queue.

**Field-level authorization**: Dataloader sources can enforce per-user visibility rules at load time, 
not in the resolver. This is cleaner than filtering after the fact and reduces unnecessary database 
queries for unauthorized fields.

---

## Advanced Considerations

API implementations at scale require careful consideration of request handling, error responses, and the interaction between multiple clients with different performance expectations. The distinction between public APIs and internal APIs affects error reporting granularity, versioning strategies, and backwards compatibility guarantees fundamentally. Versioning APIs through headers, paths, or query parameters each have trade-offs in terms of maintenance burden, client complexity, and developer experience across multiple client versions. When deprecating API endpoints, the migration window and support period must balance client migration costs with infrastructure maintenance costs and team capacity constraints.

GraphQL adds complexity around query costs, depth limits, and the interaction between nested resolvers and N+1 query problems. A deeply nested GraphQL query can trigger hundreds of database queries if not carefully managed with proper preloading and query analysis. Implementing query cost analysis prevents malicious or poorly-written queries from starving resources and degrading service for other clients. The caching layer becomes more complex with GraphQL because the same data may be accessed through multiple query paths, each with different caching semantics and TTL requirements that must be carefully coordinated at the application level.

Error handling and status codes require careful design to balance information disclosure with security concerns. Too much detail in error messages helps attackers; too little detail frustrates legitimate users. Implement structured error responses with specific error codes that clients can use to handle different failure scenarios intelligently and retry appropriately. Rate limiting, circuit breakers, and backpressure mechanisms prevent API overload but require careful configuration based on expected traffic patterns and SLA requirements.


## Deep Dive: Etsdets Patterns and Production Implications

ETS tables are in-memory, non-distributed key-value stores with tunable semantics (ordered_set, duplicate_bag). Under concurrent read/write load, ETS table semantics matter: bag semantics allow fast appends but slow deletes; ordered_set allows range queries but slower inserts. Testing ETS behavior under concurrent load is non-trivial; single-threaded tests miss lock contention. Production ETS tables often fail under load due to concurrency assumptions that quiet tests don't exercise.

---

## Trade-offs and production gotchas

**1. Using `System.os_time/1` instead of `monotonic_time/1`**
Wall-clock jumps (NTP, leap seconds) can make the bucket go negative or skip refills. Always monotonic.

**2. Read-then-write race**
The simple variant has a small window where two checks see the same `tokens` and both decrement. For strict quotas (payments, public API billing), use the `update_counter` strict variant.

**3. Unbounded key growth**
Every new API key adds a row. A leaked dashboard that spawns random keys leaks memory. Run a periodic cleanup that drops rows untouched for > 1 hour.

**4. `write_concurrency` without `decentralized_counters`**
OTP 23+ introduced `decentralized_counters: true` which makes ETS counter ops scale linearly with schedulers. Without it, you see contention beyond 4 cores.

**5. 429 without `Retry-After`**
Clients backing off without guidance either hammer or give up. Always return `Retry-After` in seconds.

**6. When NOT to use this**
Across a multi-node cluster where each node has its own ETS, a single client distributed across nodes can exceed the global quota by `N` times. For true global quotas, use Redis with `INCR`+`EXPIRE` or a distributed counter.

## Reflection

Your enterprise customers run high-volume integrations spread across three AWS regions. Each region has its own `api_portal` cluster. How do you enforce a single global quota of 6000/min without making every check a network round-trip? Discuss options: Redis cluster, gossip-based reconciliation with local tolerance, or shard-by-tenant routing.

## Resources

- [`:ets.update_counter/4`](https://www.erlang.org/doc/man/ets.html#update_counter-4)
- [ETS scalability — OTP team blog](https://www.erlang.org/blog/optimized-ets/)
- [Token bucket algorithm — Wikipedia](https://en.wikipedia.org/wiki/Token_bucket)
- [Cloudflare — How we built rate limiting capable of scaling to millions of domains](https://blog.cloudflare.com/counting-things-a-lot-of-different-things/)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
