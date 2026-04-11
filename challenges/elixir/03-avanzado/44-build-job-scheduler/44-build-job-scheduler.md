# Job Scheduler with Cron, Retry, and Backoff

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`, an internal HTTP gateway. The cache layer is in place
(previous exercise). The infrastructure team now needs scheduled maintenance tasks:
purge stale rate-limiter entries, refresh upstream service configs, and send hourly
health digests. You need a job scheduler — without Oban or Quantum, using only the
BEAM runtime.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex              # already exists — supervises Scheduler
│       └── scheduler/
│           ├── server.ex               # scheduler GenServer
│           ├── job.ex                  # job and execution structs
│           ├── cron_parser.ex          # cron expression parser
│           └── backoff.ex              # exponential backoff with jitter
├── test/
│   └── api_gateway/
│       └── scheduler/
│           ├── server_test.exs         # given tests — must pass without modification
│           └── cron_parser_test.exs    # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The infra team needs three recurring tasks:

1. Every 60 seconds: purge expired rate-limiter entries from ETS
2. Every 5 minutes: fetch the current list of upstream service URLs from Consul
3. `0 * * * *` (every hour on the hour): emit a health digest to the logging pipeline

Each task must retry on failure with exponential backoff, never run more than N instances
concurrently, and expose a history of recent executions for debugging.

---

## Why build this instead of using Quantum

Quantum is production-grade but pulls in 6 transitive dependencies and runs a Postgres
or ETS-backed persistence layer. For an embedded scheduler that runs 3-5 tasks, the overhead
is unjustified. Building it yourself also means you understand exactly what happens when a
job crashes, times out, or runs long.

The patterns here — `Process.send_after` for self-scheduling, `Task.Supervisor` for isolated
execution, exponential backoff with jitter — are the same ones Quantum uses internally.

---

## Why exponential backoff with jitter

A failing job that retries immediately creates load storms: if the downstream is down, 20
concurrent jobs all retry at second intervals and hammer the recovering service. Exponential
backoff spreads retries out geometrically. Jitter (randomized delay) prevents multiple
independent job instances from synchronizing their retry times — the thundering herd problem.

```
Attempt 1: fails -> wait 1s + jitter(250ms) = ~1.2s
Attempt 2: fails -> wait 2s + jitter(500ms) = ~2.4s
Attempt 3: fails -> wait 4s + jitter(1000ms) = ~4.7s
Attempt 4: fails -> dead letter queue
```

---

## Implementation

### Step 1: `lib/api_gateway/scheduler/job.ex`

```elixir
defmodule ApiGateway.Scheduler.Job do
  @enforce_keys [:id, :name, :fun, :schedule]
  defstruct [
    :id,           # String.t() — unique identifier
    :name,         # String.t() — human-readable description
    :fun,          # (-> any()) — zero-arity function to execute
    :schedule,     # {:every, ms} | {:cron, String.t()}
    max_retries: 3,
    timeout_ms: 30_000,
    enabled: true
  ]
end

defmodule ApiGateway.Scheduler.Execution do
  defstruct [
    :job_id,
    :started_at,
    :finished_at,
    :duration_ms,
    :result,    # :ok | {:error, reason}
    :attempt    # 1..max_retries
  ]
end
```

### Step 2: `lib/api_gateway/scheduler/backoff.ex`

Exponential backoff caps delay at a maximum to prevent unbounded wait times.
Jitter adds randomized noise to prevent synchronized retry storms.

```elixir
defmodule ApiGateway.Scheduler.Backoff do
  @base_ms 1_000
  @max_ms  300_000

  @doc """
  Delay in ms for a given retry attempt. Grows exponentially, capped at @max_ms.

  ## Examples
      delay_for(1) #=> 1_000
      delay_for(2) #=> 2_000
      delay_for(3) #=> 4_000
  """
  @spec delay_for(pos_integer()) :: pos_integer()
  def delay_for(attempt) do
    delay = round(@base_ms * :math.pow(2, attempt - 1))
    min(delay, @max_ms)
  end

  @doc """
  Adds uniform random jitter of up to 25% of the base delay.
  Prevents synchronized retries from multiple job instances.
  """
  @spec with_jitter(pos_integer()) :: pos_integer()
  def with_jitter(delay_ms) do
    jitter = :rand.uniform(max(1, div(delay_ms, 4)))
    delay_ms + jitter
  end
end
```

### Step 3: `lib/api_gateway/scheduler/cron_parser.ex`

Parses standard 5-field cron expressions and calculates the next firing time.
The parser supports `*`, `*/n`, exact values, ranges (`a-b`), and lists (`a,b,c`).

```elixir
defmodule ApiGateway.Scheduler.CronParser do
  @moduledoc """
  Parses cron expressions and calculates milliseconds until the next execution.

  Supported syntax per field:
    *       — any value
    */n     — every n units
    n       — exact value
    a-b     — inclusive range
    a,b,c   — list of values

  Fields: minute hour day-of-month month day-of-week
  """

  defstruct [:minute, :hour, :day, :month, :weekday]

  @type field :: :any | {:every, pos_integer()} | {:range, integer(), integer()} |
                 {:list, [integer()]} | integer()

  @doc """
  Parses a 5-field cron expression string into a CronParser struct.
  Raises ArgumentError on invalid input.

  ## Examples
      parse("*/5 * * * *")   #=> %CronParser{minute: {:every, 5}, ...}
      parse("0 9 * * 1")     #=> %CronParser{minute: 0, hour: 9, weekday: 1, ...}
  """
  @spec parse(String.t()) :: t()
  def parse(expr) do
    case String.split(expr, " ") do
      [min, hr, day, mon, wday] ->
        %__MODULE__{
          minute:  parse_field(min),
          hour:    parse_field(hr),
          day:     parse_field(day),
          month:   parse_field(mon),
          weekday: parse_field(wday)
        }
      _ ->
        raise ArgumentError, "invalid cron expression: #{inspect(expr)}"
    end
  end

  @doc """
  Returns milliseconds from now until the next time this cron fires.
  Always returns a positive value — even if the cron should have fired moments ago.
  """
  @spec next_run_in_ms(t()) :: pos_integer()
  def next_run_in_ms(%__MODULE__{} = parsed) do
    now  = DateTime.utc_now()
    # Start searching from the next full minute
    next = find_next(parsed, DateTime.add(now, 60, :second) |> truncate_to_minute())
    DateTime.diff(next, now, :millisecond)
  end

  defp parse_field("*"), do: :any
  defp parse_field("*/" <> n), do: {:every, String.to_integer(n)}

  defp parse_field(f) do
    cond do
      String.contains?(f, "-") ->
        [a, b] = String.split(f, "-")
        {:range, String.to_integer(a), String.to_integer(b)}
      String.contains?(f, ",") ->
        {:list, f |> String.split(",") |> Enum.map(&String.to_integer/1)}
      true ->
        String.to_integer(f)
    end
  end

  # Advance minute-by-minute from `from` until a DateTime matches all fields.
  # Cap search at 1 year (525_600 minutes) to avoid infinite loops on impossible expressions.
  defp find_next(parsed, from, iterations \\ 0)

  defp find_next(_parsed, from, iterations) when iterations > 525_600 do
    # Safety valve — if no match in a year, return the current candidate
    from
  end

  defp find_next(parsed, from, iterations) do
    if matches_all?(parsed, from) do
      from
    else
      find_next(parsed, DateTime.add(from, 60, :second), iterations + 1)
    end
  end

  defp matches_all?(parsed, dt) do
    # Date.day_of_week/1 returns 1 (Monday) through 7 (Sunday).
    # Cron convention: 0 = Sunday, 1 = Monday, ..., 6 = Saturday, 7 = Sunday.
    dow = Date.day_of_week(dt) |> rem(7)

    field_matches?(parsed.minute, dt.minute) and
      field_matches?(parsed.hour, dt.hour) and
      field_matches?(parsed.day, dt.day) and
      field_matches?(parsed.month, dt.month) and
      field_matches?(parsed.weekday, dow)
  end

  defp field_matches?(:any, _value), do: true
  defp field_matches?({:every, n}, value), do: rem(value, n) == 0
  defp field_matches?({:range, a, b}, value), do: value >= a and value <= b
  defp field_matches?({:list, vals}, value), do: value in vals
  defp field_matches?(exact, value), do: exact == value

  defp truncate_to_minute(dt) do
    %{dt | second: 0, microsecond: {0, 0}}
  end
end
```

### Step 4: `lib/api_gateway/scheduler/server.ex`

The scheduler GenServer manages job registration, timer-based scheduling, and delegates
execution to a `Task.Supervisor` for isolation. Failed jobs retry with exponential backoff.

```elixir
defmodule ApiGateway.Scheduler.Server do
  use GenServer

  alias ApiGateway.Scheduler.{Job, Execution, Backoff, CronParser}

  @history_limit 20

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Registers a job and schedules its first execution.

  ## Options
    - `every: ms`       — fixed interval in milliseconds
    - `cron: expr`      — cron expression string
    - `name: string`    — human-readable label
    - `max_retries: n`  — default 3
    - `timeout_ms: ms`  — default 30_000
  """
  @spec schedule((-> any()), keyword()) :: String.t()
  def schedule(fun, opts) do
    GenServer.call(__MODULE__, {:schedule, fun, opts})
  end

  @spec cancel(String.t()) :: :ok | {:error, :not_found}
  def cancel(job_id) do
    GenServer.call(__MODULE__, {:cancel, job_id})
  end

  @spec run_now(String.t()) :: :ok | {:error, :not_found}
  def run_now(job_id) do
    GenServer.call(__MODULE__, {:run_now, job_id})
  end

  @spec pause(String.t()) :: :ok
  def pause(job_id), do: GenServer.call(__MODULE__, {:set_enabled, job_id, false})

  @spec resume(String.t()) :: :ok
  def resume(job_id), do: GenServer.call(__MODULE__, {:set_enabled, job_id, true})

  @spec list_jobs() :: [map()]
  def list_jobs, do: GenServer.call(__MODULE__, :list_jobs)

  @spec job_history(String.t()) :: [Execution.t()]
  def job_history(job_id), do: GenServer.call(__MODULE__, {:history, job_id})

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    max_concurrent = Keyword.get(opts, :max_concurrent, 10)
    {:ok, task_sup} = Task.Supervisor.start_link()

    state = %{
      jobs:           %{},    # job_id => %Job{}
      timers:         %{},    # job_id => timer_ref
      history:        %{},    # job_id => [%Execution{}, ...]
      running:        MapSet.new(),
      task_refs:      %{},    # ref => {job_id, attempt}
      max_concurrent: max_concurrent,
      task_supervisor: task_sup
    }

    {:ok, state}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call({:schedule, fun, opts}, _from, state) do
    job_id = generate_id()
    schedule_type = cond do
      Keyword.has_key?(opts, :every) -> {:every, Keyword.fetch!(opts, :every)}
      Keyword.has_key?(opts, :cron)  -> {:cron, Keyword.fetch!(opts, :cron)}
      true -> raise ArgumentError, "must provide :every or :cron"
    end

    job = %Job{
      id:          job_id,
      name:        Keyword.get(opts, :name, job_id),
      fun:         fun,
      schedule:    schedule_type,
      max_retries: Keyword.get(opts, :max_retries, 3),
      timeout_ms:  Keyword.get(opts, :timeout_ms, 30_000)
    }

    # Calculate the first interval and schedule
    interval_ms = case schedule_type do
      {:every, ms}  -> ms
      {:cron, expr} -> CronParser.parse(expr) |> CronParser.next_run_in_ms()
    end

    timer_ref = Process.send_after(self(), {:run_job, job_id}, interval_ms)

    state = state
    |> put_in([:jobs, job_id], job)
    |> put_in([:timers, job_id], timer_ref)
    |> put_in([:history, job_id], [])

    {:reply, job_id, state}
  end

  @impl true
  def handle_call({:cancel, job_id}, _from, state) do
    case state.jobs[job_id] do
      nil ->
        {:reply, {:error, :not_found}, state}

      _job ->
        # Cancel the pending timer
        if timer = state.timers[job_id] do
          Process.cancel_timer(timer)
        end

        state = state
        |> update_in([:jobs], &Map.delete(&1, job_id))
        |> update_in([:timers], &Map.delete(&1, job_id))

        {:reply, :ok, state}
    end
  end

  @impl true
  def handle_call({:run_now, job_id}, _from, state) do
    case state.jobs[job_id] do
      nil ->
        {:reply, {:error, :not_found}, state}

      _job ->
        # Cancel the existing timer to avoid double-execution
        if timer = state.timers[job_id] do
          Process.cancel_timer(timer)
        end

        # Send the run message immediately
        send(self(), {:run_job, job_id})
        {:reply, :ok, state}
    end
  end

  @impl true
  def handle_call({:set_enabled, job_id, enabled}, _from, state) do
    case state.jobs[job_id] do
      nil ->
        {:reply, :ok, state}

      job ->
        updated_job = %{job | enabled: enabled}
        state = put_in(state.jobs[job_id], updated_job)

        # If resuming, reschedule the next run
        state =
          if enabled do
            schedule_next(updated_job, state)
          else
            state
          end

        {:reply, :ok, state}
    end
  end

  @impl true
  def handle_call(:list_jobs, _from, state) do
    jobs = Enum.map(state.jobs, fn {_id, job} ->
      %{id: job.id, name: job.name, enabled: job.enabled, schedule: job.schedule}
    end)
    {:reply, jobs, state}
  end

  @impl true
  def handle_call({:history, job_id}, _from, state) do
    {:reply, Map.get(state.history, job_id, []), state}
  end

  @impl true
  def handle_info({:run_job, job_id}, state) do
    job = state.jobs[job_id]

    if job && job.enabled do
      if MapSet.size(state.running) >= state.max_concurrent do
        require Logger
        Logger.warning("[Scheduler] Skipping #{job.name} -- max concurrent reached")
        state = schedule_next(job, state)
        {:noreply, state}
      else
        # Execute the job in an isolated task
        started_at = DateTime.utc_now()

        task = Task.Supervisor.async_nolink(state.task_supervisor, fn ->
          job.fun.()
        end)

        # Track the task reference to the job
        state = state
        |> update_in([:running], &MapSet.put(&1, job_id))
        |> put_in([:task_refs, task.ref], {job_id, 1, started_at})

        # Reschedule the next run
        state = schedule_next(job, state)

        {:noreply, state}
      end
    else
      {:noreply, state}
    end
  end

  @impl true
  def handle_info({ref, result}, state) when is_reference(ref) do
    # Task completed successfully — result is the return value of fun.()
    Process.demonitor(ref, [:flush])

    case Map.pop(state.task_refs, ref) do
      {{job_id, attempt, started_at}, task_refs} ->
        finished_at = DateTime.utc_now()
        duration_ms = DateTime.diff(finished_at, started_at, :millisecond)

        execution = %Execution{
          job_id: job_id,
          started_at: started_at,
          finished_at: finished_at,
          duration_ms: duration_ms,
          result: :ok,
          attempt: attempt
        }

        state = state
        |> Map.put(:task_refs, task_refs)
        |> record_execution(job_id, execution)
        |> update_in([:running], &MapSet.delete(&1, job_id))

        {:noreply, state}

      {nil, _} ->
        {:noreply, state}
    end
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, reason}, state) when reason != :normal do
    # Task crashed — implement retry with backoff
    Process.demonitor(ref, [:flush])

    case Map.pop(state.task_refs, ref) do
      {{job_id, attempt, started_at}, task_refs} ->
        state = Map.put(state, :task_refs, task_refs)
        job = state.jobs[job_id]

        if job && attempt < job.max_retries do
          # Schedule a retry with exponential backoff + jitter
          delay = Backoff.delay_for(attempt) |> Backoff.with_jitter()

          retry_task_fn = fn ->
            new_started_at = DateTime.utc_now()

            task = Task.Supervisor.async_nolink(state.task_supervisor, fn ->
              job.fun.()
            end)

            # We need to send ourselves a message to update state since we can't
            # modify state from inside Process.send_after callback
            {task, new_started_at}
          end

          # Schedule retry: send a message to ourselves after the backoff delay
          Process.send_after(self(), {:retry_job, job_id, attempt + 1}, delay)

          state = update_in(state.running, &MapSet.delete(&1, job_id))
          {:noreply, state}
        else
          # Max retries exhausted — record as failed
          finished_at = DateTime.utc_now()
          duration_ms = DateTime.diff(finished_at, started_at, :millisecond)

          execution = %Execution{
            job_id: job_id,
            started_at: started_at,
            finished_at: finished_at,
            duration_ms: duration_ms,
            result: {:error, reason},
            attempt: attempt
          }

          state = state
          |> record_execution(job_id, execution)
          |> update_in([:running], &MapSet.delete(&1, job_id))

          {:noreply, state}
        end

      {nil, _} ->
        {:noreply, state}
    end
  end

  # Handle :DOWN for normal exits (task finished normally but we already handled via {ref, result})
  def handle_info({:DOWN, _ref, :process, _pid, :normal}, state) do
    {:noreply, state}
  end

  @impl true
  def handle_info({:retry_job, job_id, attempt}, state) do
    job = state.jobs[job_id]

    if job && job.enabled do
      started_at = DateTime.utc_now()

      task = Task.Supervisor.async_nolink(state.task_supervisor, fn ->
        job.fun.()
      end)

      state = state
      |> update_in([:running], &MapSet.put(&1, job_id))
      |> put_in([:task_refs, task.ref], {job_id, attempt, started_at})

      {:noreply, state}
    else
      {:noreply, state}
    end
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp schedule_next(job, state) do
    interval_ms = case job.schedule do
      {:every, ms}  -> ms
      {:cron, expr} -> CronParser.parse(expr) |> CronParser.next_run_in_ms()
    end

    timer_ref = Process.send_after(self(), {:run_job, job.id}, interval_ms)
    put_in(state.timers[job.id], timer_ref)
  end

  defp record_execution(state, job_id, execution) do
    history = Map.get(state.history, job_id, [])
    new_history = Enum.take([execution | history], @history_limit)
    put_in(state.history[job_id], new_history)
  end

  defp generate_id do
    :crypto.strong_rand_bytes(8) |> Base.url_encode64(padding: false)
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/api_gateway/scheduler/server_test.exs
defmodule ApiGateway.Scheduler.ServerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Scheduler.Server

  setup do
    # Restart with a clean state
    if Process.whereis(Server), do: GenServer.stop(Server)
    {:ok, _} = Server.start_link(max_concurrent: 5)
    :ok
  end

  test "schedules and executes a job" do
    parent = self()
    Server.schedule(fn -> send(parent, :executed) end, every: 50, name: "test-job")
    assert_receive :executed, 500
  end

  test "cancel stops future executions" do
    parent = self()
    job_id = Server.schedule(fn -> send(parent, :should_not_run) end, every: 50, name: "cancel-test")
    Server.cancel(job_id)
    refute_receive :should_not_run, 200
  end

  test "run_now triggers immediate execution" do
    parent = self()
    job_id = Server.schedule(fn -> send(parent, :ran) end, every: 60_000, name: "immediate")
    Server.run_now(job_id)
    assert_receive :ran, 500
  end

  test "pause and resume" do
    parent = self()
    job_id = Server.schedule(fn -> send(parent, :tick) end, every: 50, name: "pausable")
    assert_receive :tick, 300
    Server.pause(job_id)
    flush_mailbox()
    refute_receive :tick, 200
    Server.resume(job_id)
    assert_receive :tick, 300
  end

  test "history records executions" do
    job_id = Server.schedule(fn -> :ok end, every: 50, name: "history-job")
    Process.sleep(200)
    history = Server.job_history(job_id)
    assert length(history) >= 2
    assert Enum.all?(history, &(&1.result == :ok))
  end

  defp flush_mailbox do
    receive do
      _ -> flush_mailbox()
    after
      0 -> :ok
    end
  end
end
```

```elixir
# test/api_gateway/scheduler/cron_parser_test.exs
defmodule ApiGateway.Scheduler.CronParserTest do
  use ExUnit.Case

  alias ApiGateway.Scheduler.CronParser

  describe "parse/1" do
    test "parses wildcard" do
      assert %CronParser{minute: :any} = CronParser.parse("* * * * *")
    end

    test "parses every-n" do
      assert %CronParser{minute: {:every, 5}} = CronParser.parse("*/5 * * * *")
    end

    test "parses exact value" do
      assert %CronParser{hour: 9} = CronParser.parse("0 9 * * *")
    end

    test "parses range" do
      assert %CronParser{weekday: {:range, 1, 5}} = CronParser.parse("0 9 * * 1-5")
    end

    test "parses list" do
      assert %CronParser{minute: {:list, [0, 15, 30, 45]}} = CronParser.parse("0,15,30,45 * * * *")
    end
  end

  describe "next_run_in_ms/1" do
    test "returns a positive number of milliseconds" do
      ms = CronParser.parse("*/5 * * * *") |> CronParser.next_run_in_ms()
      assert is_integer(ms) and ms > 0
    end

    test "hourly cron fires within the hour" do
      ms = CronParser.parse("0 * * * *") |> CronParser.next_run_in_ms()
      assert ms <= 3_600_000
    end
  end
end
```

### Step 6: Run the tests

```bash
mix test test/api_gateway/scheduler/ --trace
```

---

## Trade-off analysis

Fill in this table based on your implementation.

| Aspect | This scheduler | Oban | Quantum |
|--------|---------------|------|---------|
| Persistence after crash | none (in-memory) | PostgreSQL | configurable |
| Distributed (multi-node) | no | yes (DB-backed) | yes (global/distributed) |
| Cron syntax | subset (5-field) | n/a | full |
| Retry semantics | exponential backoff | at-least-once via DB | configurable |
| Observability | in-memory history | full DB queryable | dashboard |
| Dependencies | none | ~6 | ~4 |

Reflection: when would you reach for Oban over this scheduler? (Hint: what happens to
scheduled jobs when the node restarts?)

---

## Common production mistakes

**1. Not re-enqueuing in `handle_info({:run_job, ...})`**
If `schedule_next/2` is only called in `handle_call({:schedule, ...})`, the job fires once
and never again. Always re-schedule inside the execution handler.

**2. Accumulating timers on `run_now`**
`run_now/1` should cancel the existing timer before sending the immediate message, otherwise
the job runs twice in quick succession (once immediately and once when the timer fires).

**3. No jitter on retry**
Without jitter, all retries for all failing jobs fire at exact multiples of the base delay.
If 10 jobs fail simultaneously, they all retry at t+1s, t+2s, t+4s — correlated spikes.

**4. Blocking the GenServer during job execution**
Running `job.fun.()` inside `handle_info` blocks the entire scheduler for the duration of
the job. Always delegate to `Task.Supervisor`.

**5. Unlimited history**
Without `@history_limit`, long-running gateways accumulate unlimited execution records per
job. After a week, the history map becomes the dominant memory consumer.

---

## Resources

- [`Process.send_after/3`](https://hexdocs.pm/elixir/Process.html#send_after/3) — scheduling without external deps
- [`Task.Supervisor`](https://hexdocs.pm/elixir/Task.Supervisor.html) — isolated async execution
- [Oban source — scheduler](https://github.com/sorentwo/oban) — how production-grade scheduling works
- [Exponential backoff and jitter — AWS blog](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/) — the jitter argument
