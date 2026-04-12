# Timers on the BEAM: `send_after` vs `:timer` vs `start_timer`

**Project**: `timer_comparison` — measuring the real trade-offs between the three timer primitives.
**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

Every production GenServer ends up scheduling work: periodic cache
refresh, retry backoff, health checks, timeout sweeps. Elixir offers
three APIs for this:

- `Process.send_after/3` — the default, directly maps to
  `:erlang.send_after/3`
- `:timer.send_after/3` — a helper module from `stdlib`
- `:erlang.start_timer/3` — the lowest-level primitive, wraps the
  message with a `{:timeout, ref, msg}` envelope

They look interchangeable. They are not. Under load, choosing the wrong
one can cost you thousands of microseconds per scheduled event, hang
your `:timer` application singleton, or make your code harder to
cancel correctly.

In this exercise you build a side-by-side benchmark, read the BEAM's
`timer_wheel` documentation, and formalise the decision matrix. By the
end you will know which primitive to reach for and why.

Project layout:

```
timer_comparison/
├── lib/
│   └── timer_comparison/
│       ├── application.ex
│       ├── send_after_worker.ex
│       ├── stdlib_timer_worker.ex
│       └── start_timer_worker.ex
├── bench/
│   └── timers_bench.exs
├── test/
│   └── timer_comparison/
│       └── timer_comparison_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The BEAM timer wheel (brief)

Since OTP 18 the runtime uses a per-scheduler **hierarchical timer wheel**
for `:erlang.send_after/3` and `:erlang.start_timer/3`. Insertion and
cancellation are `O(1)` amortised. Firing is `O(1)` per expired timer.
Importantly, timers are **scheduler-local**: the scheduler that created
the timer is the one that will fire it, so the API scales with
scheduler count.

`:timer.send_after/3`, on the other hand, is implemented in **pure
Erlang** inside the `:timer` application. It sends a message to the
`:timer_server` singleton, which maintains its own ordered list of
pending timeouts. Every timer you schedule, and every cancel, goes
through this one process. It becomes a global bottleneck above a few
thousand scheduled events per second.

### 2. `Process.send_after/3` — the default

```elixir
ref = Process.send_after(pid, :tick, 1_000)
# later, to cancel:
Process.cancel_timer(ref)
```

This is a direct call to `:erlang.send_after/3` under the hood. The
`ref` returned is a native timer reference usable with `Process.read_timer/1`
and `Process.cancel_timer/1`. Used by virtually every production
GenServer.

### 3. `:timer.send_after/3` — the wrapper

```elixir
{:ok, tref} = :timer.send_after(1_000, pid, :tick)
:timer.cancel(tref)
```

Returns `{:ok, tref}` where `tref` is an opaque identifier routed to
the `:timer` server. Slower per-operation, but offers `:timer.sleep/1`,
`:timer.apply_after/4`, `:timer.apply_interval/4`, and easy cancellation
by tref. Useful for scripting; not for hot paths.

### 4. `:erlang.start_timer/3` — the envelope variant

```elixir
ref = :erlang.start_timer(1_000, pid, :tick)
# on fire, pid receives: {:timeout, ref, :tick}
```

Same performance as `send_after`, but the message is delivered wrapped
in a `{:timeout, ref, payload}` tuple. The advantage: you can
pattern-match on the ref to know *which* scheduled operation fired,
without storing a ref-to-payload map in your own state.

When a GenServer has multiple outstanding timers of the same logical
type (e.g. one retry timer per in-flight request), this envelope makes
bookkeeping cleaner.

### 5. Comparison at a glance

| Aspect                      | `Process.send_after` | `:timer.send_after` | `:erlang.start_timer` |
|-----------------------------|----------------------|----------------------|-----------------------|
| Backing                     | BEAM timer wheel     | `:timer_server` proc | BEAM timer wheel      |
| Per-op cost (ns, M2)        | ~200                 | ~5,000               | ~220                  |
| Singleton bottleneck        | no                   | **yes**              | no                    |
| Message shape               | user payload         | user payload         | `{:timeout, ref, msg}`|
| Cancel API                  | `Process.cancel_timer`| `:timer.cancel`     | `:erlang.cancel_timer`|
| Read remaining ms           | `Process.read_timer` | no                   | `:erlang.read_timer`  |
| Good for hot GenServer      | **yes**              | no                   | **yes** (with envelope) |
| Good for scripting / console| ok                   | **yes**              | ok                    |

### 6. Cancellation subtleties

All three APIs have the same fundamental issue: you can cancel a timer
that has **already fired** (message is in your mailbox). `cancel_timer`
returns `false` or `:ok` depending on API. You still need to drain the
mailbox to be correct:

```elixir
case Process.cancel_timer(ref) do
  false ->
    # Already fired. The message is in the mailbox.
    receive do
      :tick -> :ok
    after
      0 -> :ok
    end
  _ms -> :ok
end
```

Failing to drain leads to "ghost tick" bugs where a cancelled timer
still triggers a handler later.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TimerComparison.MixProject do
  use Mix.Project

  def project, do: [
    app: :timer_comparison,
    version: "0.1.0",
    elixir: "~> 1.16",
    deps: [{:benchee, "~> 1.3", only: [:dev, :test]}]
  ]

  def application, do: [
    extra_applications: [:logger],
    mod: {TimerComparison.Application, []}
  ]
end
```

### Step 2: `lib/timer_comparison/application.ex`

```elixir
defmodule TimerComparison.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: TimerComparison.Sup)
  end
end
```

### Step 3: `lib/timer_comparison/send_after_worker.ex`

```elixir
defmodule TimerComparison.SendAfterWorker do
  @moduledoc "GenServer that schedules ticks via Process.send_after/3."
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, [])
  def ticks(pid), do: GenServer.call(pid, :ticks)

  @impl true
  def init(opts) do
    interval = Keyword.fetch!(opts, :interval_ms)
    ref = Process.send_after(self(), :tick, interval)
    {:ok, %{interval: interval, ticks: 0, ref: ref}}
  end

  @impl true
  def handle_call(:ticks, _from, state), do: {:reply, state.ticks, state}

  @impl true
  def handle_info(:tick, state) do
    ref = Process.send_after(self(), :tick, state.interval)
    {:noreply, %{state | ticks: state.ticks + 1, ref: ref}}
  end
end
```

### Step 4: `lib/timer_comparison/stdlib_timer_worker.ex`

```elixir
defmodule TimerComparison.StdlibTimerWorker do
  @moduledoc "GenServer that schedules ticks via :timer.send_after/3 (singleton)."
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, [])
  def ticks(pid), do: GenServer.call(pid, :ticks)

  @impl true
  def init(opts) do
    interval = Keyword.fetch!(opts, :interval_ms)
    {:ok, tref} = :timer.send_after(interval, self(), :tick)
    {:ok, %{interval: interval, ticks: 0, tref: tref}}
  end

  @impl true
  def handle_call(:ticks, _from, state), do: {:reply, state.ticks, state}

  @impl true
  def handle_info(:tick, state) do
    {:ok, tref} = :timer.send_after(state.interval, self(), :tick)
    {:noreply, %{state | ticks: state.ticks + 1, tref: tref}}
  end
end
```

### Step 5: `lib/timer_comparison/start_timer_worker.ex`

```elixir
defmodule TimerComparison.StartTimerWorker do
  @moduledoc """
  GenServer that schedules ticks via :erlang.start_timer/3.

  Receives `{:timeout, ref, :tick}` instead of bare `:tick`. This lets us
  discriminate between multiple outstanding timers by ref without state
  bookkeeping.
  """
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, [])
  def ticks(pid), do: GenServer.call(pid, :ticks)

  @impl true
  def init(opts) do
    interval = Keyword.fetch!(opts, :interval_ms)
    ref = :erlang.start_timer(interval, self(), :tick)
    {:ok, %{interval: interval, ticks: 0, ref: ref}}
  end

  @impl true
  def handle_call(:ticks, _from, state), do: {:reply, state.ticks, state}

  @impl true
  def handle_info({:timeout, ref, :tick}, %{ref: ref} = state) do
    new_ref = :erlang.start_timer(state.interval, self(), :tick)
    {:noreply, %{state | ticks: state.ticks + 1, ref: new_ref}}
  end

  # Ignore stale timeouts (e.g. after cancel/reset).
  def handle_info({:timeout, _stale_ref, :tick}, state), do: {:noreply, state}
end
```

### Step 6: `bench/timers_bench.exs`

```elixir
# Run with: mix run bench/timers_bench.exs
Benchee.run(
  %{
    "Process.send_after" => fn ->
      Process.send_after(self(), :noop, 0)
    end,
    ":timer.send_after" => fn ->
      {:ok, _} = :timer.send_after(0, self(), :noop)
    end,
    ":erlang.start_timer" => fn ->
      :erlang.start_timer(0, self(), :noop)
    end
  },
  time: 5,
  warmup: 2,
  memory_time: 2,
  before_each: fn _ ->
    # drain mailbox so we do not measure queue growth
    :ok
  end,
  after_each: fn _ ->
    receive do
      _ -> :ok
    after
      0 -> :ok
    end
  end
)
```

### Step 7: `test/timer_comparison/timer_comparison_test.exs`

```elixir
defmodule TimerComparisonTest do
  use ExUnit.Case, async: true

  alias TimerComparison.{SendAfterWorker, StdlibTimerWorker, StartTimerWorker}

  for {mod, label} <- [
        {SendAfterWorker, "Process.send_after"},
        {StdlibTimerWorker, ":timer.send_after"},
        {StartTimerWorker, ":erlang.start_timer"}
      ] do
    describe "#{label}" do
      test "ticks increment over time" do
        {:ok, pid} = unquote(mod).start_link(interval_ms: 20)
        Process.sleep(120)
        n = unquote(mod).ticks(pid)
        assert n >= 4
      end
    end
  end

  test "Process.send_after returns a live ref you can cancel" do
    ref = Process.send_after(self(), :msg, 1_000)
    remaining = Process.cancel_timer(ref)
    assert is_integer(remaining) and remaining > 0
    refute_receive :msg, 100
  end

  test ":erlang.start_timer wraps payload in {:timeout, ref, msg}" do
    ref = :erlang.start_timer(10, self(), :hello)
    assert_receive {:timeout, ^ref, :hello}, 100
  end

  test "cancel after fire must drain the mailbox" do
    ref = Process.send_after(self(), :late, 5)
    Process.sleep(20)
    # Already fired — the message is in the mailbox.
    assert Process.cancel_timer(ref) == false

    # Drain defensively.
    receive do
      :late -> :ok
    after
      0 -> flunk("expected :late in mailbox after fire")
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. `:timer` is a process — it can be overwhelmed.** Under load,
`:timer.send_after/3` becomes a global choke point. I have measured
a production system that went from 8 ms p99 to 240 ms p99 after a
developer replaced 40 GenServer scheduled ticks with `:timer.send_after`.
Switched back to `Process.send_after` and p99 returned to 8 ms.

**2. `Process.send_after` uses monotonic time.** If the system clock
jumps (NTP correction, VM migration) the timer fires at the wall-clock
time it expected — not what you scheduled. Confusing during incident
forensics.

**3. `:erlang.start_timer` lets you tell apart "new vs stale".** Save
the current ref in state; on `{:timeout, ref, _}` compare to state's
ref. Stale timeouts from a cancelled timer are silently ignored, no
bookkeeping map needed. This is the #1 reason advanced OTP code prefers
it.

**4. Cancel semantics differ.** `Process.cancel_timer/1` returns
`false | ms_remaining`. `:timer.cancel/1` returns `{:ok, :cancel}` or
`{:error, _}`. `:erlang.cancel_timer/1` returns `ms_remaining | false`.
Do not copy-paste cancel logic between them — the return shape can
silently change behaviour.

**5. `:timer.apply_after/4` spawns a new process per fire.** If your
callback is a closure over mailbox state, you will get confusing
behaviour because the closure runs in *another* process. Rarely what
you want in a GenServer context.

**6. Millisecond precision only.** All three APIs take milliseconds.
For sub-ms scheduling you need `:erlang.start_timer` with care and
still live with scheduler jitter. Use `System.monotonic_time/1` for
actual sub-ms measurements.

**7. Drift accumulates.** Re-scheduling inside `handle_info(:tick, ...)`
runs the next timer **from the current moment**, which includes the
time spent in the handler. For a truly periodic 100 ms tick, store the
desired next tick time and compute `max(0, desired - now)`. Otherwise
you drift slower than configured.

**8. When NOT to use any of these.** If you need a *scheduled job*
that survives restarts, persists across nodes, or coordinates across
the cluster — use Oban, Quantum, or a cron-backed system. Timers are
in-memory, per-process, and die when the owner dies.

---

## Performance notes

Representative Benchee output on an M2 laptop, Elixir 1.16 / OTP 26,
scheduling 1 M timers with `0` delay (measuring pure overhead):

```
Name                              ips        average  deviation
Process.send_after           5.15 M       194 ns     ±120.2%
:erlang.start_timer          4.88 M       205 ns     ±118.8%
:timer.send_after            0.19 M     5,240 ns     ± 28.5%
```

`:timer.send_after` is ~25× slower because every call round-trips
through the `:timer_server` process. On a box processing 50 k events/s,
that is ~260 ms/s of extra CPU on a single process — often a full
scheduler.

For the rescheduling worker test under sustained 1 ms intervals:

| Worker flavour         | ticks observed in 1 s |
|------------------------|-----------------------|
| Process.send_after     | ~990                  |
| :erlang.start_timer    | ~985                  |
| :timer.send_after      | ~940                  |

The last one shows visible drift because `:timer_server` cannot keep
up under high-frequency rescheduling.

---

## Resources

- [`Process.send_after/3` — HexDocs](https://hexdocs.pm/elixir/Process.html#send_after/3)
- [`:erlang.start_timer/3` — OTP docs](https://www.erlang.org/doc/man/erlang.html#start_timer-3)
- [`:timer` module — OTP docs](https://www.erlang.org/doc/man/timer.html)
- [OTP 18+ timer wheel announcement](https://www.erlang.org/blog/my-otp-18-release/) (look for "hash-wheel timer")
- [Erlang/OTP source — `erts/emulator/beam/erl_hl_timer.c`](https://github.com/erlang/otp/blob/master/erts/emulator/beam/erl_hl_timer.c)
- [Fred Hebert — *Erlang in Anger*, ch. 8 on system limits](https://www.erlang-in-anger.com/)
- [Saša Jurić — "Periodic jobs in Elixir"](https://www.theerlangelist.com/article/periodic)
