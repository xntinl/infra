# Rate Limiter with ETS and Sliding Window

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`, an internal HTTP gateway that routes traffic to microservices.
Routing is already working (previous exercises). The next step is to protect downstream
services from abusive clients.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex          # already exists — supervises RateLimiter
│       ├── router.ex               # already exists — calls RateLimiter.Server.check/3
│       └── rate_limiter/
│           ├── server.ex           # ← you implement this
│           └── window.ex           # ← and this
├── test/
│   └── api_gateway/
│       └── rate_limiter_test.exs   # given tests — must pass without modification
├── bench/
│   └── rate_limiter_bench.exs      # benchmark — run at the end
└── mix.exs
```

---

## The business problem

The infra team reported that a misconfigured client is sending 10,000 req/min to the
payments service, degrading response times for all other clients. You need a rate limiter that:

1. Operates per `client_id` (from the `X-Client-ID` request header)
2. Uses **sliding window** semantics — not fixed window
3. Can be consulted on every request **without becoming a bottleneck**
4. Automatically cleans up expired entries — the system runs 24/7

---

## Why sliding window and not fixed window

A **fixed window** rate limiter resets the counter at the start of each interval.
If the limit is 100 req/min, a malicious client can make 100 requests at 00:59 and
100 more at 01:00 — 200 requests in under 2 seconds. Both windows were within limits.

**Sliding window** keeps the timestamp of every individual request. On check, it counts
how many timestamps fall within the last `window_ms`. There are no window edges to exploit.

The cost: more memory (you store N timestamps instead of 1 counter) and more CPU per
check (O(n) lookup instead of O(1)). In practice, for 60s windows with reasonable limits
(< 1000 req/min), this overhead is negligible.

---

## Why ETS and not GenServer state

Using a `%{client_id => [timestamps]}` map in GenServer state has a fundamental problem:

```
request A ──GenServer.call──▶ GenServer (serialized)
request B ──GenServer.call──▶ (waiting in mailbox)
request C ──GenServer.call──▶ (waiting in mailbox)
```

Under high load, the mailbox grows. The GenServer processes one message at a time.
The latency of `check/3` rises proportionally to the backlog.

ETS with a `:public` table allows **concurrent reads without going through any process**:

```
request A ──ets:lookup──▶ ETS table  (concurrent, no serialization)
request B ──ets:lookup──▶ ETS table
request C ──ets:lookup──▶ ETS table
request D ──GenServer.cast──▶ GenServer ──ets:insert──▶ ETS table
```

Only writes (`record/1`) go through the GenServer to ensure the table exists while
the process is alive. Reads (`check/3`) go directly to ETS.

This is the **read-heavy ETS owner** pattern: the GenServer owns the table (if the
process dies, the table is destroyed) but is not the bottleneck for reads.

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

### Step 1: Create the project

```bash
mix new api_gateway --sup
cd api_gateway
mkdir -p lib/api_gateway/rate_limiter
mkdir -p test/api_gateway
mkdir -p bench
```

### Step 2: `mix.exs` — add benchee as a dev dependency

```elixir
# mix.exs
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/api_gateway/rate_limiter/server.ex`

`# TODO` marks what you need to implement. `# HINT` gives direction without spoiling
the solution. Do not change the public function signatures — the tests depend on them.

```elixir
defmodule ApiGateway.RateLimiter.Server do
  use GenServer

  @table :rate_limiter_windows
  @cleanup_interval_ms 60_000

  # ---------------------------------------------------------------------------
  # Public API — entry points used by the router and tests
  # ---------------------------------------------------------------------------

  @doc """
  Checks whether `client_id` is allowed to make a request given the limit and window.

  Returns `{:allow, remaining}` or `{:deny, retry_after_ms}`.

  This function reads directly from ETS — it does NOT go through the GenServer.
  """
  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  def check(client_id, limit, window_ms) do
    # HINT: use :ets.lookup/2 to get all timestamps for client_id
    # HINT: filter for timestamps that fall within the window (now - window_ms)
    # HINT: if count < limit → {:allow, limit - count}
    # HINT: if count >= limit → {:deny, time_until_oldest_entry_expires}
    # TODO: implement
  end

  @doc """
  Records a new request for `client_id` with the current timestamp.

  Only call this if check/3 returned :allow. This is a cast — fire and forget.
  """
  @spec record(String.t()) :: :ok
  def record(client_id) do
    ts = System.monotonic_time(:millisecond)
    GenServer.cast(__MODULE__, {:record, client_id, ts})
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    # TODO: create the ETS table with the correct options
    #
    # Options to consider:
    #   :named_table   → access by name instead of pid, needed for reads in check/3
    #   :public        → any process can read/write (needed for check/3 without GenServer)
    #   :bag           → allows multiple values per key (needed for timestamps)
    #
    # Design question: why :bag and not :set here?
    # With :set, {client_id} can only have ONE value. With :bag, it can have N.
    # We need to store one timestamp per request — we need :bag.

    table = :ets.new(@table, [:named_table, :public, :bag])
    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:ok, %{table: table}}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_cast({:record, client_id, timestamp}, state) do
    # TODO: insert {client_id, timestamp} into the ETS table
    # HINT: :ets.insert/2 takes the table name and a tuple {key, value}
    {:noreply, state}
  end

  @impl true
  def handle_info(:cleanup, state) do
    # Periodic cleanup: delete entries older than 1 hour.
    # ETS has no native TTL — cleanup is the owner's responsibility.
    #
    # Option A (simple, to start):
    #   :ets.tab2list/1 + Enum.filter + :ets.delete_object/2
    #   Pros: easy to read. Cons: O(n) memory (copies the entire table).
    #
    # Option B (efficient for production):
    #   :ets.select_delete/2 with a match spec
    #   Pros: operates directly in ETS, no copy. Cons: match spec syntax.
    #
    # Start with Option A. If benchmarks show cleanup is a bottleneck,
    # migrate to Option B.

    cutoff = System.monotonic_time(:millisecond) - 3_600_000

    # TODO: delete all entries with timestamp < cutoff
    # HINT (Option A): :ets.tab2list(@table) returns [{client_id, ts}, ...]
    # HINT (Option A): to delete a specific entry: :ets.delete_object(@table, {client_id, ts})

    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:noreply, state}
  end
end
```

### Step 4: Given tests — must pass without modification

Copy this file exactly. Your implementation must make all 4 tests pass.

```elixir
# test/api_gateway/rate_limiter_test.exs
defmodule ApiGateway.RateLimiterTest do
  use ExUnit.Case, async: false
  # async: false because tests share the global ETS table :rate_limiter_windows

  alias ApiGateway.RateLimiter.Server

  setup do
    :ets.delete_all_objects(:rate_limiter_windows)
    :ok
  end

  describe "check/3 — sliding window semantics" do
    test "allows requests within the limit" do
      for _ <- 1..5, do: Server.record("client_allow")
      # Give the GenServer time to process the casts
      Process.sleep(10)

      assert {:allow, remaining} = Server.check("client_allow", 10, 60_000)
      assert remaining == 5
    end

    test "denies when limit is exceeded" do
      for _ <- 1..10, do: Server.record("client_deny")
      Process.sleep(10)

      assert {:deny, retry_after_ms} = Server.check("client_deny", 10, 60_000)
      assert retry_after_ms > 0 and retry_after_ms <= 60_000
    end

    test "expired requests do not count in the window" do
      # Insert artificial timestamps that have already expired (90s ago)
      old_ts = System.monotonic_time(:millisecond) - 90_000

      for _ <- 1..10 do
        :ets.insert(:rate_limiter_windows, {"client_expired", old_ts})
      end

      # With a 60s window, those timestamps have expired — should allow
      assert {:allow, _remaining} = Server.check("client_expired", 10, 60_000)
    end

    test "new client has the full limit available" do
      assert {:allow, 100} = Server.check("client_new", 100, 60_000)
    end
  end

  describe "check/3 — concurrent reads" do
    test "100 concurrent readers without race conditions" do
      # Populate with some requests
      for _ <- 1..50, do: Server.record("client_concurrent")
      Process.sleep(20)

      tasks =
        for _ <- 1..100 do
          Task.async(fn -> Server.check("client_concurrent", 100, 60_000) end)
        end

      results = Task.await_many(tasks, 5_000)

      # All must return a valid response — no crashes
      assert Enum.all?(results, fn
               {:allow, n} when is_integer(n) -> true
               {:deny, ms} when is_integer(ms) -> true
               _ -> false
             end)
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/rate_limiter_test.exs --trace
```

All 4 tests fail initially — that's expected. Your job is to implement `Server`
until all of them pass.

### Step 6: Concurrency benchmark

Once tests pass, measure real throughput:

```elixir
# bench/rate_limiter_bench.exs
Benchee.run(
  %{
    "check — empty table" => fn ->
      ApiGateway.RateLimiter.Server.check("bench_new", 1_000, 60_000)
    end,
    "check — 500 entries in table" => fn ->
      ApiGateway.RateLimiter.Server.check("bench_heavy", 1_000, 60_000)
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

Seed data before the benchmark:

```elixir
# In iex -S mix, before running the bench:
ts = System.monotonic_time(:millisecond)
for _ <- 1..500, do: :ets.insert(:rate_limiter_windows, {"bench_heavy", ts})
```

```bash
mix run bench/rate_limiter_bench.exs
```

**Expected result on modern hardware**: `check` < 10µs at p99 for a table with 500 entries.
If you see latencies > 100µs, verify that `check/3` is NOT making a `GenServer.call`.

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Trade-off analysis

Fill in this table based on your implementation and benchmark results.

| Aspect | ETS `:bag` (your impl) | State map in GenServer | External Redis |
|--------|----------------------|----------------------|----------------|
| Concurrent reads | no serialization | serialized by mailbox | network round-trip |
| Consistency | eventual (async casts) | strong (sync calls) | configurable |
| p50 latency | < 5µs (measure) | proportional to backlog | > 500µs |
| Memory for 10k active clients | estimate | estimate | n/a (off-heap) |
| Survives node crash | no | no | yes |
| Cleanup complexity | manual (your cleanup) | manual | native TTL |

Reflection question: in what scenarios would you prefer the `GenServer.call` alternative
over direct ETS reads? (Hint: transactional consistency.)

---

## Common production mistakes

**1. `handle_call` for ETS reads**
If `check/3` uses `GenServer.call`, the GenServer serializes all reads.
The ETS table exists to avoid exactly that. Read directly with `:ets.lookup/2`.

**2. Not cleaning up expired entries**
The table grows indefinitely. In production with 10k active clients and 60s windows,
you can accumulate millions of entries within hours. Periodic cleanup is not optional.

**3. `:set` instead of `:bag`**
With `:set`, you can only store ONE value per key. If you insert `{"client", ts2}` after
`{"client", ts1}`, the second replaces the first. You lose the timestamp history that
sliding window needs. You need `:bag`.

**4. `System.os_time` instead of `System.monotonic_time`**
`os_time` can go backwards (NTP adjustment, leap seconds). For time windows where you
compare "now - window_ms", you need `monotonic_time` which guarantees monotonic advance.

**5. `record/1` as `call` instead of `cast`**
Recording a timestamp needs no confirmation. Using `cast` releases the caller immediately.
Using `call` makes every request wait for a write acknowledgment.

---

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [`:ets` documentation — Erlang/OTP](https://www.erlang.org/doc/man/ets.html) — read the section on `type` and `access`
- [Erlang in Anger — Fred Hebert](https://www.erlang-in-anger.com/) — chapter on ETS in production (free PDF)
- [Plug.Session.ETS source](https://github.com/elixir-plug/plug/blob/main/lib/plug/session/ets.ex) — how Plug uses ETS as session store (similar pattern)
- [Benchee](https://github.com/bencheeorg/benchee) — idiomatic benchmarking in Elixir
