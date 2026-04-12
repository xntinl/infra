# Periodic heartbeats — `Process.send_after/3` vs `:timer.send_interval/2`

**Project**: `timer_gs` — a GenServer that emits a heartbeat every N milliseconds, implemented in both flavors side by side so you can see why `Process.send_after/3` is the idiomatic choice.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

A huge number of OTP services need periodic work: heartbeats for liveness,
cache eviction sweeps, stats rollups, reconnection attempts, token refresh.
Elixir has two obvious tools for this, and new developers pick the wrong
one about half the time.

- `:timer.send_interval(N, msg)` — "just fires every N ms forever, easy".
- `Process.send_after(self(), msg, N)` rescheduled inside the handler —
  "more code, but you own the loop".

On the surface the first looks simpler. In production the second wins almost
every time, for three reasons: **drift isolation**, **cancellation**, and
**testability**. This exercise builds both, compares them, and makes those
reasons concrete with runnable tests.

Project structure:

```
timer_gs/
├── lib/
│   ├── timer_gs/send_after.ex
│   └── timer_gs/send_interval.ex
├── test/
│   ├── timer_gs_send_after_test.exs
│   └── timer_gs_send_interval_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Process.send_after/3` — one-shot, owned by this process

```elixir
ref = Process.send_after(self(), :tick, 1_000)
# later, in handle_info(:tick, state): do work, then reschedule.
```

- Returns a ref you can cancel with `Process.cancel_timer/1`.
- The timer lives inside the VM's per-scheduler timer wheel — no gen_server
  bottleneck.
- If your handler is slow, the *next* tick is delayed — never overlapping.
- Cancelling on `terminate/2` is trivial and deterministic.

### 2. `:timer.send_interval/2` — recurring, owned by the `:timer` server

```elixir
{:ok, tref} = :timer.send_interval(1_000, :tick)
# later, in handle_info(:tick, state): do work. DO NOT reschedule (it keeps firing).
```

- The `:timer` module is a single global gen_server (`:timer_server`). Every
  `send_interval` call routes through it. Under contention it becomes a
  bottleneck — ask anyone who scaled a monitoring system on it.
- If your handler stalls, ticks queue up in the mailbox: you come back and
  process 10 ticks at once. Not always what you want.
- Cancellation via `:timer.cancel/1` exists but is easier to forget.

### 3. Drift: self-scheduled vs fixed interval

```
send_after loop:  handler_time + interval  (drifts forward if handler is slow)
send_interval:    fires every interval regardless  (mailbox backs up)
```

Neither is "correct" in the absolute sense — which one you want depends on
whether you care about cadence (use `send_interval` or compute absolute
deadlines) or about never overlapping work (use `send_after`). Most
applications want the latter.

### 4. Testability

With `send_after`, a test can inject a short interval via a `start_link`
option. With `send_interval`, you also can — but stopping cleanly between
tests requires remembering to cancel the tref, and the `:timer` server is
shared state across the whole VM (slower `async: true` runs).

---

## Implementation

### Step 1: Create the project

```bash
mix new timer_gs
cd timer_gs
```

### Step 2: `lib/timer_gs/send_after.ex`

```elixir
defmodule TimerGs.SendAfter do
  @moduledoc """
  Heartbeat implemented with `Process.send_after/3` and self-rescheduling.
  This is the idiomatic OTP pattern for per-process periodic work.
  """

  use GenServer

  defmodule State do
    @moduledoc false
    defstruct [:interval, :notify_to, :ticks, :timer_ref]
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    {init_opts, gen_opts} = Keyword.split(opts, [:interval, :notify_to])
    GenServer.start_link(__MODULE__, init_opts, gen_opts)
  end

  @doc "Number of ticks this process has handled since start."
  @spec ticks(GenServer.server()) :: non_neg_integer()
  def ticks(server), do: GenServer.call(server, :ticks)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(opts) do
    interval = Keyword.fetch!(opts, :interval)
    notify_to = Keyword.get(opts, :notify_to)

    state = %State{
      interval: interval,
      notify_to: notify_to,
      ticks: 0
    }

    {:ok, schedule(state)}
  end

  @impl true
  def handle_info(:tick, %State{ticks: ticks, notify_to: notify_to} = state) do
    if notify_to, do: send(notify_to, {:tick, ticks + 1})
    {:noreply, schedule(%{state | ticks: ticks + 1})}
  end

  def handle_info(_other, state), do: {:noreply, state}

  @impl true
  def handle_call(:ticks, _from, %State{ticks: ticks} = state) do
    {:reply, ticks, state}
  end

  @impl true
  def terminate(_reason, %State{timer_ref: ref}) do
    # Cancelling is cheap and deterministic here.
    if is_reference(ref), do: Process.cancel_timer(ref)
    :ok
  end

  # ── Helpers ─────────────────────────────────────────────────────────────

  defp schedule(%State{interval: interval} = state) do
    ref = Process.send_after(self(), :tick, interval)
    %{state | timer_ref: ref}
  end
end
```

### Step 3: `lib/timer_gs/send_interval.ex`

```elixir
defmodule TimerGs.SendInterval do
  @moduledoc """
  Heartbeat implemented with `:timer.send_interval/2`. Included for
  comparison — production code should usually prefer `SendAfter`.
  """

  use GenServer

  defmodule State do
    @moduledoc false
    defstruct [:interval, :notify_to, :ticks, :tref]
  end

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    {init_opts, gen_opts} = Keyword.split(opts, [:interval, :notify_to])
    GenServer.start_link(__MODULE__, init_opts, gen_opts)
  end

  @spec ticks(GenServer.server()) :: non_neg_integer()
  def ticks(server), do: GenServer.call(server, :ticks)

  @impl true
  def init(opts) do
    interval = Keyword.fetch!(opts, :interval)
    notify_to = Keyword.get(opts, :notify_to)

    # `send_interval` installs a repeating timer via the global :timer server.
    {:ok, tref} = :timer.send_interval(interval, :tick)

    {:ok,
     %State{
       interval: interval,
       notify_to: notify_to,
       ticks: 0,
       tref: tref
     }}
  end

  @impl true
  def handle_info(:tick, %State{ticks: ticks, notify_to: notify_to} = state) do
    if notify_to, do: send(notify_to, {:tick, ticks + 1})
    # Do NOT reschedule — send_interval keeps firing on its own.
    {:noreply, %{state | ticks: ticks + 1}}
  end

  def handle_info(_other, state), do: {:noreply, state}

  @impl true
  def handle_call(:ticks, _from, %State{ticks: ticks} = state) do
    {:reply, ticks, state}
  end

  @impl true
  def terminate(_reason, %State{tref: tref}) do
    # Forgetting this leaks a timer in the :timer server until the VM stops.
    if tref, do: :timer.cancel(tref)
    :ok
  end
end
```

### Step 4: `test/timer_gs_send_after_test.exs`

```elixir
defmodule TimerGs.SendAfterTest do
  use ExUnit.Case, async: true

  alias TimerGs.SendAfter

  test "ticks fire at the configured interval" do
    {:ok, pid} = SendAfter.start_link(interval: 20, notify_to: self())

    assert_receive {:tick, 1}, 200
    assert_receive {:tick, 2}, 200
    assert_receive {:tick, 3}, 200

    GenServer.stop(pid)
  end

  test "tick counter is readable via call" do
    {:ok, pid} = SendAfter.start_link(interval: 10, notify_to: self())
    assert_receive {:tick, _}, 200

    # Wait a couple of ticks to be robust to scheduling jitter.
    Process.sleep(50)
    assert SendAfter.ticks(pid) >= 2

    GenServer.stop(pid)
  end

  test "terminate cancels the outstanding timer cleanly" do
    {:ok, pid} = SendAfter.start_link(interval: 1_000, notify_to: self())
    # Stop before the first tick fires.
    GenServer.stop(pid)
    refute_receive {:tick, _}, 100
  end
end
```

### Step 5: `test/timer_gs_send_interval_test.exs`

```elixir
defmodule TimerGs.SendIntervalTest do
  # NOTE: async: false — :timer is global VM state; concurrent runs can
  # interfere. This is itself a reason to prefer SendAfter.
  use ExUnit.Case, async: false

  alias TimerGs.SendInterval

  test "ticks fire at the configured interval" do
    {:ok, pid} = SendInterval.start_link(interval: 20, notify_to: self())

    assert_receive {:tick, 1}, 200
    assert_receive {:tick, 2}, 200

    GenServer.stop(pid)
  end

  test "terminate cancels the recurring timer" do
    {:ok, pid} = SendInterval.start_link(interval: 1_000, notify_to: self())
    GenServer.stop(pid)
    refute_receive {:tick, _}, 100
  end
end
```

### Step 6: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. `:timer` is a single gen_server — avoid it on hot paths**
Every `:timer.send_interval`, `:timer.send_after`, and `:timer.cancel` goes
through one process. Under thousands of timer operations per second, it
serializes everything. `Process.send_after/3` uses the scheduler's internal
timer wheel and scales with cores.

**2. Forgetting `:timer.cancel/1` leaks refs**
Processes die, but timer refs registered with `:timer` live until their
deadline unless cancelled. Over time you accumulate zombie timers firing
`:tick` into dead pids (which is a no-op, but still work). `Process.send_after/3`
timers are tied to the calling process and garbage-collected automatically
when it dies.

**3. `send_interval` backs up the mailbox on slow handlers**
If your tick handler takes 1.5 seconds with a 1-second interval, you'll
receive bursts: 0 ticks for a while, then 2+ at once. `send_after`
reschedules *after* the handler finishes, naturally spacing ticks out.

**4. Drift is unavoidable with naive self-rescheduling**
`Process.send_after(self(), :tick, 1_000)` inside the handler means real
interval ≈ `1_000 + handler_duration`. For drift-free cadence, compute
absolute deadlines with `System.monotonic_time/1` and sleep until each one:
`next = prev + interval; delay = max(0, next - now)`.

**5. Testing with short intervals can still be flaky**
CI schedulers are noisy. Use generous `assert_receive` timeouts and avoid
asserting exact counts over time windows; assert cadence instead
(successive ticks within some tolerance).

**6. When NOT to use either**
For cron-style schedules (e.g. "every day at 03:00 UTC"), use a dedicated
scheduler like `Quantum` or `Oban`. For one-off deferred work across a
cluster, use `Oban`. Raw timers are for per-process periodic work inside a
single node.

---

## Resources

- [`Process.send_after/4` — Elixir stdlib](https://hexdocs.pm/elixir/Process.html#send_after/4)
- [`:timer` — Erlang stdlib](https://www.erlang.org/doc/man/timer.html) — note the "Using the Timer Server" section
- [Erlang Efficiency Guide — process timers](https://www.erlang.org/doc/efficiency_guide/processes.html)
- [`Quantum` — cron-like scheduler for Elixir](https://hexdocs.pm/quantum/)
- [Saša Jurić — "Elixir in Action" (2nd ed), chapter on OTP timers](https://www.manning.com/books/elixir-in-action-second-edition)
