# Sliding-window rate counter with `handle_info` self-scheduling

**Project**: `rate_counter_gs` — a GenServer that counts events per second using a 1-second sliding window and a self-scheduled tick via `Process.send_after/3`.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Your service needs a lightweight, in-process "events per second" gauge: how
many times a specific thing happened in the last second. Metrics libraries
like `:telemetry` + Prometheus are the production answer, but sometimes you
need an internal signal for fast feedback loops (circuit breakers, adaptive
sampling, rate-based alerting) without a round-trip to an external system.

The interesting part isn't the counter — it's the **time** mechanics.
You'll learn to use `handle_info` to receive timer messages, and the
self-scheduling idiom (`Process.send_after(self(), :tick, 1_000)` inside
the tick handler) to drive a periodic action without spawning a separate
timer process.

This is the same pattern used by libraries like `Hackney` pools, `Finch`,
and many rate limiters in the wild. Understanding it well — including the
drift and cancellation subtleties — is table stakes for OTP-level work.

Project structure:

```
rate_counter_gs/
├── lib/
│   └── rate_counter_gs.ex
├── test/
│   └── rate_counter_gs_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `handle_info/2` — the catch-all message handler

`handle_call` handles `GenServer.call`, `handle_cast` handles `GenServer.cast`,
and `handle_info` handles **everything else**: raw `send/2` messages, timer
messages, monitor `:DOWN`, port data, node events... If you receive a
non-OTP message (a timer firing, for example), it lands in `handle_info`.

Forgetting to implement `handle_info` means unexpected messages log a
warning every time they arrive. Always implement it, even if it's just a
`{:noreply, state}` catch-all.

### 2. `Process.send_after/3` vs `:timer.send_interval/2`

```
Process.send_after(self(), :tick, 1_000)   # one-shot; you reschedule in the handler
:timer.send_interval(1_000, :tick)          # fires forever; harder to stop
```

`send_after` returns a ref you can cancel with `Process.cancel_timer/1`.
It also avoids the `:timer` server bottleneck (`:timer` is a single gen_server
in Erlang's stdlib). For per-process periodic work, **always prefer
`send_after` + self-reschedule**. See exercise 40 for a deeper dive.

### 3. Sliding window via bucket rotation

The simplest accurate "events in last N seconds" counter is `N` one-second
buckets. On each tick, drop the oldest bucket and start a new one. Current
rate = sum of all buckets. For a 1-second window, `N = 1` plus the current
"in-flight" bucket, so we keep exactly two counters and rotate.

```
 before tick:   [prev = 17][curr = 4]  events added to curr
 after tick:    [prev = 4 ][curr = 0]  previous window dropped
```

`rate/1` returns `prev + curr` — the safest approximation of "events in the
last ~1s" without subsecond bookkeeping.

### 4. Self-scheduled ticks in `init/1`

Schedule the first tick in `init/1` so the loop is running the moment the
server is alive. Each `handle_info(:tick, state)` schedules the next tick
before returning — this way the cadence is driven by the process itself,
not by an external timer server.

---

## Implementation

### Step 1: Create the project

```bash
mix new rate_counter_gs
cd rate_counter_gs
```

### Step 2: `lib/rate_counter_gs.ex`

```elixir
defmodule RateCounterGs do
  @moduledoc """
  A sliding-window events-per-second counter. Producers call `hit/1` to
  record an event; `rate/1` returns the approximate count over the last
  ~1 second using two rotating buckets.

  Internally the server self-schedules a `:tick` every second via
  `Process.send_after/3`, which demonstrates the canonical periodic-work
  pattern for a GenServer.
  """

  use GenServer

  @tick_interval_ms 1_000

  defmodule State do
    @moduledoc false
    defstruct prev: 0, curr: 0, tick_ref: nil, interval: 1_000

    @type t :: %__MODULE__{
            prev: non_neg_integer(),
            curr: non_neg_integer(),
            tick_ref: reference() | nil,
            interval: pos_integer()
          }
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @doc """
  Starts the counter. Options:

    * `:name` — optional registered name.
    * `:interval` — tick interval in milliseconds (default 1_000). Exposed
      for tests; in production you almost always want 1_000.
  """
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    {interval, opts} = Keyword.pop(opts, :interval, @tick_interval_ms)
    GenServer.start_link(__MODULE__, interval, opts)
  end

  @doc """
  Records one event. Uses `cast` because callers on the hot path don't
  need acknowledgement — and at high event rates, `call` would serialize
  producers through a round-trip.
  """
  @spec hit(GenServer.server()) :: :ok
  def hit(server), do: GenServer.cast(server, :hit)

  @doc "Returns the approximate number of events in the last ~1 second."
  @spec rate(GenServer.server()) :: non_neg_integer()
  def rate(server), do: GenServer.call(server, :rate)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(interval) do
    state = %State{interval: interval}
    {:ok, schedule_tick(state)}
  end

  @impl true
  def handle_cast(:hit, %State{curr: curr} = state) do
    {:noreply, %{state | curr: curr + 1}}
  end

  @impl true
  def handle_call(:rate, _from, %State{prev: prev, curr: curr} = state) do
    {:reply, prev + curr, state}
  end

  @impl true
  def handle_info(:tick, %State{curr: curr} = state) do
    # Rotate buckets: what was "current" becomes "previous"; start a fresh
    # current bucket. Then reschedule — this is what keeps the loop alive.
    new_state = %{state | prev: curr, curr: 0}
    {:noreply, schedule_tick(new_state)}
  end

  def handle_info(_unexpected, state) do
    # Swallow unexpected messages silently instead of crashing or flooding logs.
    {:noreply, state}
  end

  # ── Helpers ─────────────────────────────────────────────────────────────

  defp schedule_tick(%State{interval: interval} = state) do
    ref = Process.send_after(self(), :tick, interval)
    %{state | tick_ref: ref}
  end
end
```

### Step 3: `test/rate_counter_gs_test.exs`

```elixir
defmodule RateCounterGsTest do
  use ExUnit.Case, async: true

  describe "hit/1 and rate/1" do
    test "counts hits within the current window" do
      # Use a long interval so the tick doesn't fire during the test.
      {:ok, counter} = RateCounterGs.start_link(interval: 60_000)

      for _ <- 1..5, do: RateCounterGs.hit(counter)

      # Flush the cast mailbox with a call, then assert.
      assert RateCounterGs.rate(counter) == 5
    end

    test "starts at zero" do
      {:ok, counter} = RateCounterGs.start_link(interval: 60_000)
      assert RateCounterGs.rate(counter) == 0
    end
  end

  describe "tick rotation" do
    test "after a tick, previous-window hits still count, new hits accumulate" do
      # Use a short interval so the tick fires within the test.
      {:ok, counter} = RateCounterGs.start_link(interval: 50)

      for _ <- 1..3, do: RateCounterGs.hit(counter)
      # Wait long enough for exactly one tick to rotate the buckets.
      Process.sleep(80)

      # After rotation: prev = 3, curr = 0. rate is prev + curr = 3.
      assert RateCounterGs.rate(counter) == 3

      # New hits land in the fresh current bucket.
      for _ <- 1..2, do: RateCounterGs.hit(counter)
      assert RateCounterGs.rate(counter) == 5
    end

    test "two ticks drop the oldest bucket entirely" do
      {:ok, counter} = RateCounterGs.start_link(interval: 30)

      for _ <- 1..10, do: RateCounterGs.hit(counter)
      # Two full ticks: first rotates 10 into prev; second drops it.
      Process.sleep(90)

      assert RateCounterGs.rate(counter) == 0
    end
  end

  describe "unexpected messages" do
    test "does not crash on stray messages" do
      {:ok, counter} = RateCounterGs.start_link(interval: 60_000)
      send(counter, :garbage)
      # Still alive and functional.
      assert RateCounterGs.rate(counter) == 0
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Timer drift is real — `send_after` does not compensate**
`send_after(self(), :tick, 1_000)` rescheduled inside the handler means the
actual period is `1_000 + handler_time + scheduler_latency`. Over hours
this drifts. For drift-free cadence, use `:erlang.monotonic_time/0` and
compute the next deadline absolutely, not relatively.

**2. The rate is approximate — not point-in-time**
With two buckets, the reported rate can span anywhere from 1 second (right
after a tick) to 2 seconds (right before a tick). For accurate windows,
use more buckets (e.g. 10 × 100 ms) at the cost of more work per tick.

**3. `hit/1` uses `cast`; rate spikes can grow the mailbox**
If hits arrive faster than the GenServer drains them, the mailbox grows.
At extreme rates (tens of thousands/sec), back the counter with `:counters`
or `:atomics` (lock-free, per-scheduler) and use the GenServer only to
rotate buckets.

**4. Don't forget `Process.cancel_timer/1` on shutdown**
If your GenServer sometimes restarts and your tick ref is stale, a stale
`:tick` can arrive after the new init already scheduled one — you'll tick
twice per interval briefly. On `terminate/2` or bucket changes, cancel the
old ref with `Process.cancel_timer/1`.

**5. `handle_info` catch-all is mandatory**
Any process that isn't pure request/reply will eventually get stray
messages (late replies, monitor DOWNs from dead refs, node events). Always
have a catch-all `handle_info(_, state)` to avoid log spam.

**6. When NOT to use a GenServer rate counter**
For service-level metrics, use `:telemetry` + a real TSDB (Prometheus,
StatsD). A GenServer is fine for *internal* feedback loops (e.g. a
circuit breaker reading its own recent error rate), not for observability.

---

## Resources

- [`Process.send_after/3` — Elixir stdlib](https://hexdocs.pm/elixir/Process.html#send_after/4)
- [`GenServer` callbacks — `handle_info/2`](https://hexdocs.pm/elixir/GenServer.html#c:handle_info/2)
- [`:telemetry` — the production observability story](https://hexdocs.pm/telemetry/)
- ["Timer Module" — Erlang docs](https://www.erlang.org/doc/man/timer.html) — explains why `:timer` is a bottleneck
- [`ex_rated` — a battle-tested rate limiter library](https://hexdocs.pm/ex_rated/) — worth reading for production patterns
