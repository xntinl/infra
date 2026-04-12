# Build a Cron-like Job Scheduler

**Project**: `job_scheduler_built` — in-memory scheduler on top of `DynamicSupervisor` with cron expressions, retries, and exponential backoff.
**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

You work on the billing platform for a SaaS company. A new feature requires
scheduled maintenance work inside the BEAM node: refresh cached exchange rates
every 5 minutes, reconcile unpaid invoices every hour, send dunning emails at 03:00
UTC. Before reaching for Oban (which needs a Postgres database) you want a
**lightweight in-memory scheduler** for tasks that do not require durability —
cache warmers, telemetry flushes, short-lived retries.

The scheduler must:

1. Accept cron-like expressions (`*/5 * * * *` — every 5 minutes).
2. Run each job under its own short-lived supervised process so a crash is contained.
3. Offer retry with exponential backoff + jitter on failure.
4. Skip overlapping runs — if run #N is still going when run #N+1 fires, the
   second one is dropped (configurable to enqueue instead).
5. Emit telemetry for start/stop/error to be consumed by Prometheus.

This is the same design `Quantum` uses internally (the dominant pre-Oban cron
library for Elixir). You will build a simplified version that hits the real
trade-offs: drift, overlap, crash handling, time-zone correctness.

Project structure:

```
job_scheduler_built/
├── lib/
│   └── job_scheduler/
│       ├── application.ex
│       ├── scheduler.ex        # tick loop + cron evaluation
│       ├── cron.ex             # cron expression parser
│       ├── job.ex              # job struct + registry
│       ├── runner.ex           # supervised worker
│       └── backoff.ex          # exponential backoff + jitter
├── test/
│   └── job_scheduler/
│       ├── cron_test.exs
│       ├── scheduler_test.exs
│       └── backoff_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Tick loop drift and how to avoid it

A naive tick loop does:

```elixir
def loop do
  do_work()
  Process.sleep(60_000)
  loop()
end
```

This drifts: if `do_work` takes 200 ms, the next tick happens 60,200 ms after the
previous start. Over a day you accumulate 288 seconds of drift. The fix is to
**anchor the next tick to the wall clock**:

```elixir
next_second = System.os_time(:second) + 1
sleep_ms = max(0, (next_second * 1_000) - System.os_time(:millisecond))
Process.send_after(self(), :tick, sleep_ms)
```

Every tick targets the next full second regardless of how long the previous tick
took. This is what Quantum and most production schedulers do.

### 2. Cron expressions

Classic 5-field cron: `minute hour day-of-month month day-of-week`.

```
*/5 * * * *      → every 5 minutes
0 3 * * *        → 03:00 every day
0 */2 * * *      → every 2 hours
0 0 1 * *        → 00:00 on the 1st of each month
15,45 * * * *    → at :15 and :45 past every hour
```

We'll support: `*`, numbers, lists (`1,3,5`), ranges (`1-5`), and steps (`*/5`).

### 3. Overlap policy

When a scheduled run's previous instance is still running, you have options:

| Policy | Behavior |
|--------|----------|
| `:skip` (default) | Drop the new tick if a runner for this job is alive |
| `:queue` | Wait for the current one to finish, then run immediately |
| `:parallel` | Start a new process regardless |

Skipping is almost always the right default. "Queue" is useful for work that
**must** run exactly N times per day (reconciliation). "Parallel" only makes sense
for stateless jobs (health pings).

### 4. Retry with exponential backoff + jitter

On failure, the runner doesn't immediately reschedule. It computes

```
delay_ms = base * 2^attempt + rand(0..jitter_ms)
```

Jitter prevents the **thundering herd**: if 1,000 jobs all fail because the
downstream was unavailable, they must not all retry at the same instant and
re-overload it. AWS's famous "Exponential Backoff And Jitter" paper proved this.

### 5. Why `DynamicSupervisor` for runners

Each job invocation is a **transient** process: it runs once, either succeeds or
fails, and dies. `DynamicSupervisor` with `restart: :temporary` means the
supervisor does not auto-restart runners — retries are the scheduler's job, not
OTP's. This gives you full control over retry semantics while keeping crash
isolation.

---

## Implementation

### Step 1: Create the project

```bash
mix new job_scheduler_built --sup
cd job_scheduler_built
```

### Step 2: `lib/job_scheduler/cron.ex`

```elixir
defmodule JobScheduler.Cron do
  @moduledoc """
  Minimal cron expression parser. Supports 5-field classic cron:
  `minute hour day-of-month month day-of-week`.
  """

  defstruct [:minute, :hour, :day, :month, :dow]

  @type t :: %__MODULE__{
          minute: MapSet.t(0..59),
          hour: MapSet.t(0..23),
          day: MapSet.t(1..31),
          month: MapSet.t(1..12),
          dow: MapSet.t(0..6)
        }

  @ranges %{minute: 0..59, hour: 0..23, day: 1..31, month: 1..12, dow: 0..6}

  @spec parse(String.t()) :: {:ok, t()} | {:error, term()}
  def parse(expression) when is_binary(expression) do
    case String.split(expression, " ", trim: true) do
      [m, h, d, mo, dow] ->
        with {:ok, mm} <- expand(m, @ranges.minute),
             {:ok, hh} <- expand(h, @ranges.hour),
             {:ok, dd} <- expand(d, @ranges.day),
             {:ok, mmo} <- expand(mo, @ranges.month),
             {:ok, dwo} <- expand(dow, @ranges.dow) do
          {:ok,
           %__MODULE__{minute: mm, hour: hh, day: dd, month: mmo, dow: dwo}}
        end

      _ ->
        {:error, :invalid_expression}
    end
  end

  @spec matches?(t(), DateTime.t()) :: boolean()
  def matches?(%__MODULE__{} = cron, %DateTime{} = dt) do
    MapSet.member?(cron.minute, dt.minute) and
      MapSet.member?(cron.hour, dt.hour) and
      MapSet.member?(cron.day, dt.day) and
      MapSet.member?(cron.month, dt.month) and
      MapSet.member?(cron.dow, Date.day_of_week(dt) |> rem(7))
  end

  # ---------------------------------------------------------------- internals

  defp expand("*", range), do: {:ok, MapSet.new(range)}

  defp expand("*/" <> step, range) do
    with {:ok, step_int} <- parse_int(step) do
      {:ok,
       range
       |> Enum.filter(&(rem(&1 - Enum.min(range), step_int) == 0))
       |> MapSet.new()}
    end
  end

  defp expand(expr, range) do
    expr
    |> String.split(",", trim: true)
    |> Enum.reduce_while({:ok, MapSet.new()}, fn token, {:ok, acc} ->
      case expand_token(token, range) do
        {:ok, set} -> {:cont, {:ok, MapSet.union(acc, set)}}
        error -> {:halt, error}
      end
    end)
  end

  defp expand_token(token, range) do
    cond do
      String.contains?(token, "-") ->
        [a, b] = String.split(token, "-")

        with {:ok, a_int} <- parse_int(a),
             {:ok, b_int} <- parse_int(b) do
          {:ok, a_int..b_int |> Enum.filter(&Enum.member?(range, &1)) |> MapSet.new()}
        end

      true ->
        with {:ok, v} <- parse_int(token) do
          if Enum.member?(range, v),
            do: {:ok, MapSet.new([v])},
            else: {:error, {:out_of_range, v}}
        end
    end
  end

  defp parse_int(s) do
    case Integer.parse(s) do
      {v, ""} -> {:ok, v}
      _ -> {:error, {:not_integer, s}}
    end
  end
end
```

### Step 3: `lib/job_scheduler/backoff.ex`

```elixir
defmodule JobScheduler.Backoff do
  @moduledoc """
  Exponential backoff with full jitter, as recommended by the AWS Architecture
  Blog ("Exponential Backoff And Jitter"). Capped to avoid unbounded delays.
  """

  @base_ms 500
  @cap_ms 60_000

  @spec next_delay(non_neg_integer()) :: pos_integer()
  def next_delay(attempt) when is_integer(attempt) and attempt >= 0 do
    exp = min(@cap_ms, @base_ms * :math.pow(2, attempt) |> trunc())
    :rand.uniform(exp)
  end
end
```

### Step 4: `lib/job_scheduler/job.ex`

```elixir
defmodule JobScheduler.Job do
  @moduledoc "A scheduled job specification."

  @enforce_keys [:name, :schedule, :mfa]
  defstruct [
    :name,
    :schedule,
    :mfa,
    overlap: :skip,
    max_attempts: 3
  ]

  @type t :: %__MODULE__{
          name: atom(),
          schedule: JobScheduler.Cron.t(),
          mfa: {module(), atom(), list()},
          overlap: :skip | :queue | :parallel,
          max_attempts: pos_integer()
        }
end
```

### Step 5: `lib/job_scheduler/runner.ex`

```elixir
defmodule JobScheduler.Runner do
  @moduledoc "Runs a single job invocation with retries. Temporary, crash-contained."

  use Task, restart: :temporary

  alias JobScheduler.Backoff

  require Logger

  def start_link({job, attempt}), do: Task.start_link(__MODULE__, :run, [job, attempt])

  def run(job, attempt \\ 1) do
    start = System.monotonic_time()

    :telemetry.execute([:job_scheduler, :job, :start], %{system_time: System.system_time()},
      %{name: job.name, attempt: attempt}
    )

    {mod, fun, args} = job.mfa

    try do
      _ = apply(mod, fun, args)

      :telemetry.execute(
        [:job_scheduler, :job, :stop],
        %{duration: System.monotonic_time() - start},
        %{name: job.name, attempt: attempt, status: :ok}
      )
    rescue
      exception ->
        :telemetry.execute(
          [:job_scheduler, :job, :exception],
          %{duration: System.monotonic_time() - start},
          %{name: job.name, attempt: attempt, reason: Exception.message(exception)}
        )

        if attempt < job.max_attempts do
          delay = Backoff.next_delay(attempt)

          Logger.warning(
            "job #{job.name} failed (attempt #{attempt}): retrying in #{delay} ms"
          )

          Process.sleep(delay)
          run(job, attempt + 1)
        else
          Logger.error(
            "job #{job.name} exhausted retries (#{job.max_attempts}): #{Exception.message(exception)}"
          )

          reraise exception, __STACKTRACE__
        end
    end
  end
end
```

### Step 6: `lib/job_scheduler/scheduler.ex`

```elixir
defmodule JobScheduler.Scheduler do
  @moduledoc """
  Tick loop. Every second (anchored to the wall clock), evaluates all registered
  jobs and spawns runners for those whose cron expression matches the current
  UTC time.
  """

  use GenServer

  alias JobScheduler.{Cron, Job, Runner}

  require Logger

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec register(Job.t()) :: :ok
  def register(%Job{} = job), do: GenServer.call(__MODULE__, {:register, job})

  @spec unregister(atom()) :: :ok
  def unregister(name), do: GenServer.call(__MODULE__, {:unregister, name})

  @spec jobs() :: [Job.t()]
  def jobs, do: GenServer.call(__MODULE__, :jobs)

  @impl true
  def init(_opts) do
    schedule_next_tick()
    {:ok, %{jobs: %{}, running: %{}}}
  end

  @impl true
  def handle_call({:register, job}, _from, state),
    do: {:reply, :ok, put_in(state.jobs[job.name], job)}

  def handle_call({:unregister, name}, _from, state),
    do: {:reply, :ok, update_in(state.jobs, &Map.delete(&1, name))}

  def handle_call(:jobs, _from, state),
    do: {:reply, Map.values(state.jobs), state}

  @impl true
  def handle_info(:tick, state) do
    now = DateTime.utc_now() |> DateTime.truncate(:second)

    new_running =
      Enum.reduce(state.jobs, state.running, fn {_name, job}, running ->
        if Cron.matches?(job.schedule, now) and allow_start?(job, running) do
          {:ok, pid} =
            DynamicSupervisor.start_child(
              JobScheduler.RunnerSupervisor,
              {Runner, {job, 1}}
            )

          Process.monitor(pid)
          Map.put(running, pid, job.name)
        else
          running
        end
      end)

    schedule_next_tick()
    {:noreply, %{state | running: new_running}}
  end

  def handle_info({:DOWN, _ref, :process, pid, _reason}, state),
    do: {:noreply, %{state | running: Map.delete(state.running, pid)}}

  # ---------------------------------------------------------------- internals

  defp allow_start?(%Job{overlap: :parallel}, _), do: true

  defp allow_start?(%Job{name: name, overlap: :skip}, running) do
    not Enum.any?(running, fn {_pid, n} -> n == name end)
  end

  defp allow_start?(%Job{overlap: :queue}, _) do
    # Simplification: always start; real queue semantics would stash.
    true
  end

  defp schedule_next_tick do
    now_ms = System.os_time(:millisecond)
    next_second_ms = (div(now_ms, 1_000) + 1) * 1_000
    Process.send_after(self(), :tick, next_second_ms - now_ms)
  end
end
```

### Step 7: `lib/job_scheduler/application.ex`

```elixir
defmodule JobScheduler.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {DynamicSupervisor, name: JobScheduler.RunnerSupervisor, strategy: :one_for_one},
      JobScheduler.Scheduler
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: JobScheduler.Supervisor)
  end
end
```

### Step 8: Tests

```elixir
# test/job_scheduler/cron_test.exs
defmodule JobScheduler.CronTest do
  use ExUnit.Case, async: true

  alias JobScheduler.Cron

  describe "parse/1" do
    test "accepts wildcards" do
      assert {:ok, cron} = Cron.parse("* * * * *")
      assert MapSet.size(cron.minute) == 60
    end

    test "accepts */n steps" do
      assert {:ok, cron} = Cron.parse("*/15 * * * *")
      assert MapSet.to_list(cron.minute) |> Enum.sort() == [0, 15, 30, 45]
    end

    test "accepts lists" do
      assert {:ok, cron} = Cron.parse("0,30 * * * *")
      assert MapSet.to_list(cron.minute) |> Enum.sort() == [0, 30]
    end

    test "accepts ranges" do
      assert {:ok, cron} = Cron.parse("1-5 * * * *")
      assert MapSet.to_list(cron.minute) |> Enum.sort() == [1, 2, 3, 4, 5]
    end

    test "rejects malformed expressions" do
      assert {:error, _} = Cron.parse("not a cron")
    end
  end

  describe "matches?/2" do
    test "matches when all fields match" do
      {:ok, cron} = Cron.parse("30 10 * * *")
      dt = ~U[2026-04-12 10:30:00Z]
      assert Cron.matches?(cron, dt)
    end

    test "does not match when minute differs" do
      {:ok, cron} = Cron.parse("30 10 * * *")
      dt = ~U[2026-04-12 10:31:00Z]
      refute Cron.matches?(cron, dt)
    end
  end
end
```

```elixir
# test/job_scheduler/backoff_test.exs
defmodule JobScheduler.BackoffTest do
  use ExUnit.Case, async: true

  alias JobScheduler.Backoff

  test "grows exponentially within cap" do
    a = Backoff.next_delay(0)
    b = Backoff.next_delay(10)
    assert a <= 500
    assert b <= 60_000
    assert b > a
  end
end
```

```elixir
# test/job_scheduler/scheduler_test.exs
defmodule JobScheduler.SchedulerTest do
  use ExUnit.Case, async: false

  alias JobScheduler.{Cron, Job, Scheduler}

  defmodule Signal do
    def ping(pid), do: send(pid, :pong)
    def fail(_), do: raise("boom")
  end

  setup do
    Enum.each(Scheduler.jobs(), &Scheduler.unregister(&1.name))
    :ok
  end

  test "registering a '* * * * *' job triggers ping within ~1 second" do
    {:ok, cron} = Cron.parse("* * * * *")

    job = %Job{name: :ping_job, schedule: cron, mfa: {Signal, :ping, [self()]}}
    :ok = Scheduler.register(job)

    assert_receive :pong, 2_500
  end
end
```

### Step 9: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. In-memory only.** Node restart → all jobs forgotten. If persistence matters
(finance, billing, email sequences) use Oban.

**2. Single-node scope.** If you run 3 nodes, every node schedules every job ⇒
3× executions. Either elect a leader (via `:global` or `libcluster`) or use Oban
which handles this through the DB.

**3. Clock drift between nodes.** In a cluster, tick loops running on different
nodes will match cron at slightly different wall times. NTP sync is mandatory;
even then, cron boundaries can fire twice on leap seconds.

**4. Overlap=`:skip` assumes idempotency is not required across skips.** If a
cache warmer is skipped because the previous run is still going, that's fine. For
anything that MUST run once per schedule, use `:queue` and serialize.

**5. `Process.sleep/1` inside retries blocks the runner process.** Acceptable for
short backoffs (<60 s). For longer delays, schedule a new `send_after` tick
instead.

**6. Time zones.** `DateTime.utc_now/0` is UTC; your "03:00 UTC" is 20:00 local
time for a Buenos Aires user. Either standardize on UTC cron specs or extend
`Cron.matches?/2` to accept a timezone parameter and use `DateTime.shift_zone!/2`.

**7. Telemetry firehose.** Every job start/stop emits telemetry. On high-frequency
jobs (every second) you can saturate the handler. Sample in the handler, not at
the scheduler.

**8. When NOT to use this.** If you need durability across restarts, distributed
uniqueness, retries across deploys, prioritized queues, or unique constraints —
use Oban. This scheduler is for ephemeral in-VM tasks only.

---

## Performance notes

The tick loop cost is O(N) in registered jobs per second. For 10,000 registered
jobs with `MapSet.member?/2` checks (5 per job), expect ~2 ms per tick on modern
hardware — well within the 1-second budget.

Job runners execute in their own process, so scheduler tick latency is decoupled
from job duration. Measure with:

```elixir
:telemetry.attach("sched-latency",
  [:job_scheduler, :job, :start], fn _e, meas, meta, _ ->
    IO.inspect({meas, meta})
  end, nil)
```

---

## Resources

- [Quantum source on GitHub](https://github.com/quantum-elixir/quantum-core) — the mature version of this design
- [AWS Architecture Blog — Exponential Backoff And Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)
- [Oban documentation — hexdocs.pm](https://hexdocs.pm/oban/Oban.html)
- [`DateTime` module — hexdocs.pm](https://hexdocs.pm/elixir/DateTime.html)
- [Fred Hébert — Stuff Goes Bad: Erlang in Anger](https://www.erlang-in-anger.com/) — chapters on long-running tasks and retries
- [Chris Keathley — "Good and Bad Elixir"](https://keathley.io/blog/good-and-bad-elixir.html) — process design principles
