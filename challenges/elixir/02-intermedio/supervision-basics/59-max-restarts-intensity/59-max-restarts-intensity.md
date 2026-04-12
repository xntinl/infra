# `max_restarts` and `max_seconds` — crash-loop detection

**Project**: `restart_intensity_demo` — a supervisor tuned to demonstrate the restart-intensity safety valve and how it escalates failures upward.

---

## Project context

Restart is not a free do-over. If a child crashes on startup because of a
bad config, the supervisor would restart it forever, burning CPU and
filling logs, while hiding the real problem from you. OTP's answer is the
**restart intensity**: if the supervisor restarts more than `max_restarts`
times within `max_seconds`, the supervisor itself crashes — escalating the
failure to its own parent.

The defaults (`max_restarts: 3`, `max_seconds: 5`) are conservative on
purpose. Understanding how to tune them, and what escalation actually looks
like, separates "it works on my machine" from "it works in production".

Project structure:

```
restart_intensity_demo/
├── lib/
│   ├── restart_intensity_demo.ex
│   ├── restart_intensity_demo/
│   │   ├── flaky_worker.ex
│   │   └── supervisor.ex
├── test/
│   └── restart_intensity_demo_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not the defaults?** Defaults are a starting point; they don't match bursty failures or long-lived pools.

## Core concepts

### 1. Restart intensity is about the *supervisor*, not any single child

```
                 restart_count (last max_seconds window)
  Supervisor ─▶  ┌───┬───┬───┐
                 │ 1 │ 2 │ 3 │   — within limit, keep restarting
                 └───┴───┴───┘
                 ┌───┬───┬───┬───┐
                 │ 1 │ 2 │ 3 │ 4 │   — exceeded, supervisor CRASHES
                 └───┴───┴───┴───┘
```

Every child restart under this supervisor increments the same counter.
Doesn't matter which child — the tree as a whole has one budget.

### 2. Defaults: 3 restarts in 5 seconds

These are good defaults for most trees. A child that crashes four times
in five seconds is almost certainly broken, not "unlucky".

### 3. Escalation: when the supervisor dies, its parent sees it

If your supervisor is a child of the top-level `Application` supervisor
and it dies from intensity exhaustion, the Application's own restart
strategy kicks in — potentially taking the entire app down and restarting
it. That's intentional: a subtree that can't heal itself is declared
"broken" and the problem surfaces.

### 4. Tuning up: expensive, workload-dependent

Bumping `max_restarts: 100, max_seconds: 10` is legitimate when you own a
`DynamicSupervisor` spawning thousands of short-lived children some of
which will legitimately fail (network hiccups). It is NOT legitimate as
"shut the supervisor up about my crashing worker". Fix the worker.

### 5. The math

Roughly: allow `max_restarts / max_seconds` restarts per second as a
sustained rate. With defaults, that's 0.6/s — well under what you'd hit
during normal operation but above transient hiccups.

---

## Design decisions

**Option A — defaults (3 restarts / 5s)**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — tuned `max_restarts`/`max_seconds` per subtree (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because defaults are a reasonable starting point, but long-lived pools and flaky externals need different curves.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

```bash
mix new restart_intensity_demo
cd restart_intensity_demo
```

### Step 2: `lib/restart_intensity_demo/flaky_worker.ex`

```elixir
defmodule RestartIntensityDemo.FlakyWorker do
  @moduledoc """
  A GenServer that crashes on demand. Used to exercise the supervisor's
  restart-intensity budget: tight-loop crashing exceeds the budget and
  brings the supervisor down, which is what we want to observe.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    GenServer.start_link(__MODULE__, :ok, Keyword.take(opts, [:name]))
  end

  @spec crash(GenServer.server()) :: :ok
  def crash(server), do: GenServer.cast(server, :crash)

  @impl true
  def init(:ok), do: {:ok, %{}}

  @impl true
  def handle_cast(:crash, _s), do: raise("crash for intensity test")
end
```

### Step 3: `lib/restart_intensity_demo/supervisor.ex`

```elixir
defmodule RestartIntensityDemo.Supervisor do
  @moduledoc """
  Intentionally tight restart budget (2 in 5 seconds) so we can observe
  crash-loop detection in a test without long waits. Production values
  should be tuned to the expected failure rate, not to "silence" bugs.
  """

  use Supervisor

  @default_max_restarts 2
  @default_max_seconds 5

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, opts, Keyword.take(opts, [:name]))
  end

  @impl true
  def init(opts) do
    max_restarts = Keyword.get(opts, :max_restarts, @default_max_restarts)
    max_seconds = Keyword.get(opts, :max_seconds, @default_max_seconds)

    children = [
      {RestartIntensityDemo.FlakyWorker, [name: :flaky]}
    ]

    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: max_restarts,
      max_seconds: max_seconds
    )
  end
end
```

### Step 4: `test/restart_intensity_demo_test.exs`

```elixir
defmodule RestartIntensityDemoTest do
  use ExUnit.Case, async: false

  alias RestartIntensityDemo.FlakyWorker

  test "within budget: worker is restarted and supervisor stays up" do
    sup = start_supervised!({RestartIntensityDemo.Supervisor, max_restarts: 5, max_seconds: 5})

    old = Process.whereis(:flaky)
    ref = Process.monitor(old)
    FlakyWorker.crash(:flaky)
    assert_receive {:DOWN, ^ref, :process, ^old, _}, 500

    Process.sleep(50)
    # Supervisor still alive, worker restarted.
    assert Process.alive?(sup)
    new = Process.whereis(:flaky)
    assert new != nil and new != old
  end

  test "exceeding budget: supervisor itself crashes and escalates" do
    # 2 restarts in 5s budget. The 3rd crash in quick succession kills the sup.
    {:ok, sup} =
      RestartIntensityDemo.Supervisor.start_link(max_restarts: 2, max_seconds: 5)

    ref_sup = Process.monitor(sup)

    # Fire three crashes in quick succession. The first two are within budget,
    # the third blows it.
    for _ <- 1..3 do
      # Need a small gap so the restart actually happens before we crash again.
      pid = wait_for_pid(:flaky, 200)
      FlakyWorker.crash(pid)
      # Wait for the worker to go down before crashing the next one.
      ref = Process.monitor(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}, 500
    end

    # Supervisor dies with :shutdown reason :reached_max_restart_intensity
    # (exact atom is an Erlang internal; we only need to know it went down).
    assert_receive {:DOWN, ^ref_sup, :process, ^sup, reason}, 1_000
    assert match?(:shutdown, reason) or
             match?({:shutdown, _}, reason) or
             is_atom(reason) or
             is_tuple(reason)
  end

  defp wait_for_pid(name, timeout) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_wait_pid(name, deadline)
  end

  defp do_wait_pid(name, deadline) do
    case Process.whereis(name) do
      pid when is_pid(pid) -> pid
      nil ->
        if System.monotonic_time(:millisecond) > deadline do
          flunk("process #{inspect(name)} never came up")
        else
          Process.sleep(10)
          do_wait_pid(name, deadline)
        end
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Bumping `max_restarts` to hide crashes is a code smell**
If you find yourself raising the budget because "the worker keeps
crashing", you're silencing the alarm instead of fixing the fire. The
only legitimate reason to raise the budget is "this supervisor
legitimately expects N crashes per window under normal load".

**2. Intensity is per-supervisor, not per-child**
A noisy child in a tree of twenty quiet ones can still blow the shared
budget. If one child has a legitimately higher expected crash rate,
isolate it under its own supervisor with its own budget.

**3. Default (3, 5) can be too tight for DynamicSupervisor pools**
A pool spawning 1000 workers/minute will see occasional legitimate
crashes. With defaults, three unlucky crashes within 5s kill the pool.
Bump to something like `max_restarts: 10, max_seconds: 10` — but measure
your actual crash rate first.

**4. Escalation is a feature, not a bug**
When a supervisor dies from intensity exhaustion, it's telling its parent
"I can't heal this subtree." If the parent takes the app down, good — an
app that can't heal itself shouldn't silently half-work. Use structured
logs and alerts on `Logger` crash reports to catch this in production.

**5. When NOT to rely on intensity as a safety net**
For "crash budget" semantics at the business level (e.g., "retry this job
at most 5 times") do NOT use supervisor intensity. Use a proper job
library (Oban, Broadway) with explicit retry/backoff. Intensity is for
process-level stability, not domain-level retry logic.

---


## Reflection

- ¿Preferís fallar rápido con `max_restarts: 1` o ser tolerante con `max_restarts: 100`? Definí en qué subtree se aplica cada uno.

## Resources

- [`Supervisor.init/2` — `:max_restarts`/`:max_seconds`](https://hexdocs.pm/elixir/Supervisor.html#init/2)
- [Erlang `supervisor` — Maximum Restart Intensity](https://www.erlang.org/doc/man/supervisor.html#maximum-restart-intensity)
- ["Let It Crash" is not an excuse for bad code — Fred Hebert](https://ferd.ca/the-zen-of-erlang.html)
