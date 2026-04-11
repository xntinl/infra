# Telemetry: Instrumentation and Observability

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` is running in production and the ops team has no visibility into what is happening. How many jobs are being processed per minute? What is the average execution time? Which job types are failing? Without instrumentation, the answers are "we don't know" and the first sign of a problem is a customer complaint.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── telemetry.ex            # ← you implement this
│       ├── worker.ex               # ← you instrument this
│       ├── queue_server.ex         # ← you instrument this
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── telemetry_test.exs      # given tests — must pass without modification
└── mix.exs
```

Add to `mix.exs`:

```elixir
{:telemetry, "~> 1.2"}
```

---

## The business problem

The SRE team needs three dashboards:

1. **Throughput** — jobs processed per minute, broken down by type
2. **Latency** — p50/p95/p99 job execution time
3. **Error rate** — job failures per minute, with error reason

These metrics must be emitted in a way that is decoupled from the collection mechanism. Today they want to log to stdout. Next month they may want to ship to Datadog. The instrumentation code must not change when the backend changes.

That is exactly what `:telemetry` provides: a publish-subscribe system for measurements. The code that generates the measurement (`execute/3`) is decoupled from the code that handles it (`attach/4`).

---

## Why `:telemetry` and not manual logging or custom `GenServer` metrics

Manual logging is not structured. `Logger.info("job completed in 42ms")` is a string — a metrics backend cannot parse it reliably.

A custom `GenServer` that accumulates counters creates tight coupling: every component that wants to emit a metric must know the GenServer's name and call format. If you want to add a new handler (Prometheus, StatsD, a test assertion), you modify the GenServer.

`:telemetry` is a convention:
- **Emitters** call `:telemetry.execute/3` with an event name, measurements map, and metadata map
- **Handlers** call `:telemetry.attach/4` with a handler ID, event name list, handler function, and config
- Emitters and handlers never know about each other

The BEAM ecosystem (Phoenix, Ecto, Oban, Broadway) all instrument via `:telemetry`, so your handlers can observe both your code and every library you use.

---

## Event naming convention

Telemetry event names are lists of atoms, following the pattern `[:app, :component, :action]`:

```elixir
[:task_queue, :job, :start]    # when a job begins execution
[:task_queue, :job, :stop]     # when a job completes successfully
[:task_queue, :job, :exception] # when a job raises
[:task_queue, :queue, :enqueue] # when a job is added to the queue
[:task_queue, :queue, :dequeue] # when a job is removed from the queue
```

The `:start`/`:stop`/`:exception` trio is a convention from `telemetry`'s `span` helper — it makes it easy to compute duration and error rates.

---

## Implementation

### Step 1: `lib/task_queue/telemetry.ex` — handler setup

```elixir
defmodule TaskQueue.Telemetry do
  @moduledoc """
  Attaches telemetry handlers for `task_queue` events.

  Call `TaskQueue.Telemetry.setup/0` during application startup.
  All handlers log structured events to stdout. Replace or extend
  the handler functions to ship metrics to Datadog, Prometheus, etc.
  """

  @doc """
  Attaches all telemetry handlers. Called once at application start.
  """
  def setup do
    events = [
      [:task_queue, :job, :start],
      [:task_queue, :job, :stop],
      [:task_queue, :job, :exception],
      [:task_queue, :queue, :enqueue],
      [:task_queue, :queue, :dequeue]
    ]

    # TODO: use :telemetry.attach_many/4 to attach handle_event/4 to all events
    # The handler_id must be unique — use "task-queue-telemetry"
    # HINT:
    # :telemetry.attach_many(
    #   "task-queue-telemetry",
    #   events,
    #   &handle_event/4,
    #   nil
    # )
  end

  @doc false
  def handle_event([:task_queue, :job, :start], _measurements, metadata, _config) do
    :logger.info("[job:start] job_id=#{metadata.job_id} type=#{metadata.job_type}")
  end

  def handle_event([:task_queue, :job, :stop], measurements, metadata, _config) do
    duration_ms = System.convert_time_unit(measurements.duration, :native, :millisecond)
    :logger.info("[job:stop] job_id=#{metadata.job_id} type=#{metadata.job_type} duration_ms=#{duration_ms}")
  end

  def handle_event([:task_queue, :job, :exception], measurements, metadata, _config) do
    duration_ms = System.convert_time_unit(measurements.duration, :native, :millisecond)
    :logger.warning("[job:exception] job_id=#{metadata.job_id} reason=#{inspect(metadata.reason)} duration_ms=#{duration_ms}")
  end

  def handle_event([:task_queue, :queue, :enqueue], _measurements, metadata, _config) do
    :logger.debug("[queue:enqueue] queue_size=#{metadata.queue_size}")
  end

  def handle_event([:task_queue, :queue, :dequeue], _measurements, metadata, _config) do
    :logger.debug("[queue:dequeue] queue_size=#{metadata.queue_size}")
  end
end
```

### Step 2: `lib/task_queue/worker.ex` — emit telemetry from job execution

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Executes jobs and emits telemetry events for each execution.

  Emits:
  - `[:task_queue, :job, :start]` before execution
  - `[:task_queue, :job, :stop]` after successful execution
  - `[:task_queue, :job, :exception]` if execution raises
  """

  @doc """
  Executes a job and emits telemetry events around execution.

  ## Examples

      iex> TaskQueue.Worker.execute(%{id: "j1", type: "noop", args: %{}})
      {:ok, :noop}

  """
  @spec execute(map()) :: {:ok, term()} | {:error, term()}
  def execute(%{type: type, args: args} = job) do
    job_id   = Map.get(job, :id, "unknown")
    metadata = %{job_id: job_id, job_type: type}

    # TODO: emit [:task_queue, :job, :start] with empty measurements and metadata
    # HINT: :telemetry.execute([:task_queue, :job, :start], %{}, metadata)

    start_time = System.monotonic_time()

    try do
      result = do_execute(type, args, job_id)

      # TODO: emit [:task_queue, :job, :stop] with duration measurement
      # duration = System.monotonic_time() - start_time
      # HINT: :telemetry.execute([:task_queue, :job, :stop], %{duration: duration}, metadata)

      {:ok, result}
    rescue
      e ->
        # TODO: emit [:task_queue, :job, :exception] with duration and reason
        # HINT: :telemetry.execute([:task_queue, :job, :exception],
        #         %{duration: System.monotonic_time() - start_time},
        #         Map.put(metadata, :reason, e))
        {:error, {:unexpected, Exception.message(e)}}
    end
  end

  def execute(_), do: {:error, :missing_required_fields}

  defp do_execute("noop", _args, _job_id), do: :noop
  defp do_execute("echo", args, _job_id), do: args
  defp do_execute("fail", %{reason: reason}, job_id) do
    raise RuntimeError, "Job #{job_id} failed: #{inspect(reason)}"
  end
  defp do_execute(type, _args, job_id) do
    raise RuntimeError, "Job #{job_id}: unknown type #{type}"
  end
end
```

### Step 3: `lib/task_queue/queue_server.ex` — emit telemetry from queue operations

```elixir
# In QueueServer, add telemetry to enqueue and dequeue:

# In handle_call({:enqueue, job}, ...):
# After successfully enqueuing:
# :telemetry.execute(
#   [:task_queue, :queue, :enqueue],
#   %{},
#   %{queue_size: :queue.len(new_q)}
# )

# In handle_call(:dequeue, ...):
# After successfully dequeuing:
# :telemetry.execute(
#   [:task_queue, :queue, :dequeue],
#   %{},
#   %{queue_size: :queue.len(new_q)}
# )
```

### Step 4: `lib/task_queue/application.ex` — call `Telemetry.setup/0`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    # TODO: call TaskQueue.Telemetry.setup() before starting children
    # This attaches the handlers before any events are emitted
    TaskQueue.Telemetry.setup()

    children = [
      TaskQueue.QueueServer,
      TaskQueue.Scheduler,
      TaskQueue.Registry
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/task_queue/telemetry_test.exs
defmodule TaskQueue.TelemetryTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Worker

  setup do
    # Attach a test handler that captures events into the test process
    test_pid = self()

    :telemetry.attach_many(
      "test-handler-#{inspect(self())}",
      [
        [:task_queue, :job, :start],
        [:task_queue, :job, :stop],
        [:task_queue, :job, :exception],
        [:task_queue, :queue, :enqueue],
        [:task_queue, :queue, :dequeue]
      ],
      fn event, measurements, metadata, _config ->
        send(test_pid, {:telemetry, event, measurements, metadata})
      end,
      nil
    )

    on_exit(fn ->
      :telemetry.detach("test-handler-#{inspect(test_pid)}")
    end)

    :ok
  end

  describe "Worker.execute/1 — telemetry events" do
    test "emits :start event before execution" do
      Worker.execute(%{id: "j1", type: "noop", args: %{}})
      assert_receive {:telemetry, [:task_queue, :job, :start], _measurements, metadata}
      assert metadata.job_id == "j1"
      assert metadata.job_type == "noop"
    end

    test "emits :stop event with duration after success" do
      Worker.execute(%{id: "j2", type: "echo", args: %{msg: "hi"}})
      assert_receive {:telemetry, [:task_queue, :job, :stop], measurements, metadata}
      assert metadata.job_id == "j2"
      assert is_integer(measurements.duration)
      assert measurements.duration >= 0
    end

    test "emits :exception event when job fails" do
      Worker.execute(%{id: "j3", type: "fail", args: %{reason: :test}})
      assert_receive {:telemetry, [:task_queue, :job, :exception], measurements, metadata}
      assert metadata.job_id == "j3"
      assert is_integer(measurements.duration)
      assert metadata.reason != nil
    end

    test "start event is always emitted before stop" do
      Worker.execute(%{id: "j4", type: "noop", args: %{}})

      assert_receive {:telemetry, [:task_queue, :job, :start], _, _}
      assert_receive {:telemetry, [:task_queue, :job, :stop], _, _}
    end
  end

  describe "Telemetry.setup/0 — handler attachment" do
    test "setup attaches handlers without error" do
      # Detach first to avoid duplicate ID error
      :telemetry.detach("task-queue-telemetry")
      assert :ok = TaskQueue.Telemetry.setup()
    end

    test "attached handlers survive a re-setup if detached first" do
      :telemetry.detach("task-queue-telemetry")
      TaskQueue.Telemetry.setup()
      # If handlers were attached, execute will not raise
      Worker.execute(%{id: "j5", type: "noop", args: %{}})
      assert_receive {:telemetry, [:task_queue, :job, :start], _, _}
    end
  end
end
```

### Step 6: Run the tests

```bash
mix deps.get
mix test test/task_queue/telemetry_test.exs --trace
```

---

## Trade-off analysis

| Approach | Decoupled from backend | Structured data | BEAM ecosystem compatible |
|----------|------------------------|-----------------|--------------------------|
| `:telemetry.execute/3` | yes — handlers are separate | yes — maps | yes — standard |
| `Logger.info/1` | no — format is a string | no | no |
| Custom metrics GenServer | no — callers coupled to it | yes | no |
| `:statsix` / `:prometheus_ex` directly | no — backend embedded in code | yes | no |

Reflection question: `:telemetry.attach/4` takes a handler ID. What happens if you call `setup/0` twice without detaching first? How would you make `setup/0` idempotent?

---

## Common production mistakes

**1. Calling `setup/0` multiple times without detaching**

`:telemetry.attach/4` raises if the handler ID is already registered. Always detach before reattaching, or check with `:telemetry.list_handlers/1`:

```elixir
def setup do
  :telemetry.detach("task-queue-telemetry")
  :telemetry.attach_many("task-queue-telemetry", events, &handle_event/4, nil)
end
```

**2. Doing heavy work inside the handler**

Telemetry handlers are called synchronously in the process that called `:telemetry.execute/3`. A slow handler (HTTP call to a metrics backend) blocks the worker:

```elixir
# Wrong — HTTP call inside the handler blocks the worker
def handle_event(event, measurements, metadata, _config) do
  HTTPClient.post("https://metrics.example.com", body: Jason.encode!(%{...}))
end

# Right — send to a dedicated reporter process; worker continues immediately
def handle_event(event, measurements, metadata, _config) do
  send(TaskQueue.MetricsReporter, {:telemetry_event, event, measurements, metadata})
end
```

**3. Using raw integers for duration without unit conversion**

`System.monotonic_time()` returns native time units that vary by OS. Always convert:

```elixir
# Wrong — unit is platform-dependent
measurements.duration  # could be nanoseconds, microseconds, or native ticks

# Right
System.convert_time_unit(measurements.duration, :native, :millisecond)
```

**4. Forgetting to detach handlers in test cleanup**

Test handlers attached with `attach/4` persist for the lifetime of the test suite unless explicitly detached. Use `on_exit/1` in the test `setup` block to detach after each test.

**5. Using atoms for handler IDs in tests**

If tests run concurrently (`async: true`), multiple test processes attach handlers with the same ID:

```elixir
# Wrong — ID collision in async tests
:telemetry.attach(:test_handler, events, handler, nil)

# Right — use a unique ID per test process
:telemetry.attach("test-handler-#{inspect(self())}", events, handler, nil)
```

---

## Resources

- [Telemetry — official hex package](https://hexdocs.pm/telemetry/readme.html)
- [Telemetry.Metrics — higher-level aggregation](https://hexdocs.pm/telemetry_metrics/TelemetryMetrics.html)
- [Instrumenting Elixir with Telemetry — Elixir School](https://elixirschool.com/en/lessons/advanced/telemetry)
- [OpenTelemetry for Elixir — open-telemetry](https://github.com/open-telemetry/opentelemetry-erlang)
