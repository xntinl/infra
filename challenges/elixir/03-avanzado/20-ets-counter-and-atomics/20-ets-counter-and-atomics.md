# ETS Counters and Atomics

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway has per-route ETS counters and a periodic
metrics flush. But the dashboard team reports that counter reads are lagging under
50k req/s load — the ETS update path is showing up in profiling. And a new requirement
arrives: a sliding window rate limiter that counts requests per second with sub-microsecond
overhead per request.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       ├── metrics/
│       │   ├── counter.ex       # existing
│       │   ├── event_log.ex     # existing
│       │   └── sliding_window.ex
│       ├── cache/
│       └── config/
├── test/
│   └── api_gateway/
│       └── metrics/
│           └── sliding_window_test.exs
├── bench/
│   └── counter_bench.exs
└── mix.exs
```

---

## The business problem

Two requirements:

1. **Sliding window rate limiting**: the existing rate limiter uses a fixed window.
   The security team wants a sliding window at per-second granularity: "no more than
   N requests in the last 60 seconds." The check happens on every request, so overhead
   must be under 1µs per call at p99.

2. **Counter throughput comparison**: the engineering team wants a definitive answer to
   "which counter mechanism should we standardize on?" — with actual benchmark numbers
   for the three candidates: ETS `update_counter`, `:atomics.add`, and `:counters.add`.

---

## Why `:atomics` and `:counters` exist alongside ETS

ETS `update_counter` is atomic and highly concurrent, but it carries the overhead of
the ETS hash table: key hashing, bucket lookup, and locking at the shard level.
For a rate limiter that fires on every single request, that overhead accumulates.

`:atomics` (OTP 21.2+) is a raw array of 64-bit integers with hardware-level CAS
operations. There is no hash table, no key lookup, no per-element lock. Access is
array-index based: `O(1)` with a constant that is 5–10x smaller than ETS.

`:counters` (OTP 23+) extends `:atomics` with an optional `write_concurrency` mode
that keeps one counter copy per scheduler. Writes are local — no cache coherency traffic.
The cost: reading the current total requires summing all scheduler-local copies, making
reads more expensive than writes.

When to use which:

| Mechanism | Best for |
|-----------|----------|
| ETS `update_counter` | Counter is part of a larger record keyed by a runtime string/atom |
| `:atomics` | Fixed set of pre-allocated integer slots, CAS required, reads and writes balanced |
| `:counters` with `write_concurrency` | Maximum write throughput, infrequent total reads |

---

## How sliding window counters work with `:atomics`

A sliding window of W seconds with 1-second resolution uses W buckets.
Bucket index = `rem(unix_second, W)`.

The challenge: a bucket index wraps around. Bucket 30 in the current minute may still
hold counts from the previous minute's second-30. On write, check if the bucket's
timestamp matches the current second. If not, reset the bucket first.

```
window = 60 seconds
current time: unix second 123

bucket index = rem(123, 60) = 3

arrays: counts[1..60], timestamps[1..60]
  counts[3] = 42       ← from second 63 (previous minute)
  timestamps[3] = 63   ← 63 ≠ 123 → bucket is stale → reset

reset: CAS(timestamps[3], 63, 123) → if wins: counts[3] = 1
       if loses CAS: another process reset it → just add 1
```

This design requires two `:atomics` arrays (counts + timestamps) and uses
`:atomics.compare_exchange/4` to coordinate bucket resets without locks.

---

## Implementation

### Step 1: `lib/api_gateway/metrics/sliding_window.ex`

```elixir
defmodule ApiGateway.Metrics.SlidingWindow do
  @moduledoc """
  Sliding window request counter backed by :atomics.

  Uses two arrays of 60 slots:
    - counts[1..60]: request count for each second bucket
    - timestamps[1..60]: the unix second this bucket belongs to

  On record/1: check if the bucket is current. If stale, reset via CAS then add 1.
  On count/1: sum all bucket slots whose timestamp falls within the window.

  Thread-safe: multiple processes can call record/1 concurrently without locks.

  The CAS (compare-and-swap) on the timestamp array ensures that when a bucket
  wraps around to a new cycle, exactly one process wins the reset. All others
  see that the timestamp has already been updated and simply increment the count.
  """

  @buckets 60

  @doc """
  Creates a new sliding window counter.
  Returns {counts_ref, timestamps_ref}.
  """
  @spec new() :: {reference(), reference()}
  def new do
    counts = :atomics.new(@buckets, signed: false)
    timestamps = :atomics.new(@buckets, signed: true)
    {counts, timestamps}
  end

  @doc """
  Records one event at the current second.
  If the slot belongs to a previous cycle, it is reset first via CAS.
  """
  @spec record({reference(), reference()}) :: :ok
  def record({counts, timestamps}) do
    now = System.os_time(:second)
    # idx is 1-based (atomics uses 1-based indexing)
    idx = rem(now, @buckets) + 1
    stored_ts = :atomics.get(timestamps, idx)

    if stored_ts != now do
      # Bucket belongs to a previous cycle — reset it.
      # CAS ensures only one process wins the reset; others just add.
      case :atomics.compare_exchange(timestamps, idx, stored_ts, now) do
        :ok ->
          # Won the reset — set count to 1
          :atomics.put(counts, idx, 1)
        _current ->
          # Lost the reset — another process already reset this bucket
          :atomics.add(counts, idx, 1)
      end
    else
      :atomics.add(counts, idx, 1)
    end

    :ok
  end

  @doc """
  Counts total events in the last `seconds` seconds (seconds <= 60).

  Iterates backwards from the current second, checking each bucket's timestamp
  to verify it belongs to the current window. Only buckets with a matching
  timestamp are included in the sum — stale buckets from a previous cycle
  are excluded even though their count slot may still hold old data.
  """
  @spec count({reference(), reference()}, pos_integer()) :: non_neg_integer()
  def count({counts, timestamps}, seconds) when seconds <= @buckets do
    now = System.os_time(:second)

    Enum.reduce((now - seconds + 1)..now, 0, fn second, acc ->
      idx = rem(second, @buckets) + 1

      if :atomics.get(timestamps, idx) == second do
        acc + :atomics.get(counts, idx)
      else
        acc
      end
    end)
  end

  @doc """
  Returns average events per second over the last `seconds` seconds.
  """
  @spec rate({reference(), reference()}, pos_integer()) :: float()
  def rate(ref, seconds) do
    count(ref, seconds) / max(seconds, 1)
  end
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/api_gateway/metrics/sliding_window_test.exs
defmodule ApiGateway.Metrics.SlidingWindowTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Metrics.SlidingWindow

  describe "record/1 and count/2" do
    test "counts events recorded in the current window" do
      counter = SlidingWindow.new()

      for _ <- 1..100 do
        SlidingWindow.record(counter)
      end

      total = SlidingWindow.count(counter, 60)
      # All 100 events were in the current second — should be counted
      assert total == 100
    end

    test "count/2 with seconds=1 returns current second's events" do
      counter = SlidingWindow.new()

      for _ <- 1..50 do
        SlidingWindow.record(counter)
      end

      count_1s = SlidingWindow.count(counter, 1)
      assert count_1s == 50
    end

    test "returns 0 for a fresh counter" do
      counter = SlidingWindow.new()
      assert SlidingWindow.count(counter, 60) == 0
    end
  end

  describe "concurrent writes" do
    test "100 concurrent writers produce accurate totals" do
      counter = SlidingWindow.new()

      tasks =
        for _ <- 1..100 do
          Task.async(fn ->
            for _ <- 1..100, do: SlidingWindow.record(counter)
          end)
        end

      Task.await_many(tasks, 10_000)

      total = SlidingWindow.count(counter, 60)
      # 100 tasks × 100 events = 10_000, all within the current second
      assert total == 10_000
    end
  end

  describe "rate/2" do
    test "returns a float" do
      counter = SlidingWindow.new()
      for _ <- 1..30, do: SlidingWindow.record(counter)
      rate = SlidingWindow.rate(counter, 10)
      assert is_float(rate)
      assert rate > 0
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/api_gateway/metrics/sliding_window_test.exs --trace
```

### Step 4: Counter throughput benchmark

```elixir
# bench/counter_bench.exs
# Compares ETS update_counter, :atomics.add, and :counters.add
# under 50 concurrent writers.

table = :ets.new(:bench_counters, [
  :set, :public, {:write_concurrency, true}, {:decentralized_counters, true}
])

atomics_ref = :atomics.new(1, signed: false)
counters_ref = :counters.new(1, [:write_concurrency])
sw = ApiGateway.Metrics.SlidingWindow.new()

Benchee.run(
  %{
    "ETS update_counter + decentralized" => fn ->
      :ets.update_counter(table, :requests, {2, 1}, {:requests, 0})
    end,
    ":atomics.add" => fn ->
      :atomics.add(atomics_ref, 1, 1)
    end,
    ":counters.add (write_concurrency)" => fn ->
      :counters.add(counters_ref, 1, 1)
    end,
    "SlidingWindow.record" => fn ->
      ApiGateway.Metrics.SlidingWindow.record(sw)
    end
  },
  parallel: 50,
  warmup: 2,
  time: 5,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/counter_bench.exs
```

**Expected on modern hardware** (varies by core count and OTP version):
- `:atomics.add`: ~200–400ns per op
- `:counters.add` with `write_concurrency`: ~50–150ns per op  
- ETS `update_counter` with `decentralized_counters`: ~300–600ns per op
- `SlidingWindow.record`: ~400–800ns per op (adds CAS for bucket reset)

---

## Trade-off analysis

Fill in this table based on your benchmark results:

| Mechanism | Write throughput | Read cost | CAS available | Use in gateway |
|-----------|-----------------|-----------|---------------|---------------|
| ETS `update_counter` | measure | O(1) | No | Route stats (per-key) |
| `:atomics.add` | measure | O(1) | Yes | Sliding window buckets |
| `:counters.add` (`write_concurrency`) | measure | O(schedulers) | No | High-frequency per-app metrics |
| GenServer `cast` + map | measure | O(1) via call | No | Low-frequency admin counters |

Reflection: `SlidingWindow` resets buckets via CAS inside `record/1`. What happens
if a process calls `record/1`, wins the CAS, sets `counts[idx] = 1`, then the process
is preempted before the CAS on `timestamps` wins, and another process reads that bucket?
Is this a correctness problem or an acceptable approximation?

---

## Common production mistakes

**1. Using `update_counter/4` without a default — crashes on first call**
If the key does not exist and you omit the fourth argument (the default record),
`update_counter` raises `ArgumentError`. Always provide a default:

```elixir
# WRONG — crashes if :route not in table
:ets.update_counter(table, route, {2, 1})

# CORRECT — inserts {route, 0} if key absent, then increments
:ets.update_counter(table, route, {2, 1}, {route, 0})
```

**2. 0-based index on `:atomics` and `:counters`**
Both modules use 1-based indexing (like Erlang tuples). Index 0 does not exist
and raises `ArgumentError`. Always compute `rem(x, N) + 1`.

**3. Using `:counters` with `write_concurrency` when reads are frequent**
With `write_concurrency`, `:counters.get/2` sums all scheduler-local copies.
On a 16-core machine this is 16 atomic reads per `get`. For a rate limiter that
checks the counter on every request, this is more expensive than `:atomics.get`.

**4. CAS spin-loops under high contention**
`:atomics.compare_exchange` in a tight loop causes CPU spin when many processes
compete for the same slot. If more than a few processes compete for a single
counter, the spin degrades throughput. ETS `update_counter` with `decentralized_counters`
handles high-contention counters more gracefully.

**5. Not persisting atomics/counters on shutdown**
Both `:atomics` and `:counters` are pure in-memory structures. If the process that
holds the reference terminates, the data is gone. For metrics that must survive restarts,
persist to DETS or ETS + DETS before shutdown.

---

## Resources

- [`:atomics` — Erlang documentation](https://www.erlang.org/doc/man/atomics.html)
- [`:counters` — Erlang documentation](https://www.erlang.org/doc/man/counters.html)
- [ETS `update_counter/4` — Erlang documentation](https://www.erlang.org/doc/man/ets.html#update_counter-4)
- [Lukas Larsson — "Lock-free data structures in Erlang", EUC 2019](https://www.youtube.com/watch?v=3VGYAevO9E4) — internals of `:atomics`
- [The Erlang Runtime System — Erik Stenman](https://www.oreilly.com/library/view/the-erlang-runtime/9781800560818/) — shared memory chapter
