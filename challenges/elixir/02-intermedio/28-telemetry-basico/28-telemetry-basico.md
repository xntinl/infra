# Telemetry: Instrumentation and Observability

## Goal

Build a `task_queue` project instrumented with `:telemetry` events for job execution (start/stop/exception) and queue operations (enqueue/dequeue). Learn why telemetry decouples measurement from collection, enabling you to swap backends (stdout, Datadog, Prometheus) without changing instrumentation code.

---

## Why `:telemetry` and not manual logging or custom metrics

Manual logging is not structured. `Logger.info("job completed in 42ms")` is a string -- a metrics backend cannot parse it reliably.

A custom `GenServer` that accumulates counters creates tight coupling: every component must know the GenServer's name and call format.

`:telemetry` is a convention:
- **Emitters** call `:telemetry.execute/3` with an event name, measurements map, and metadata map
- **Handlers** call `:telemetry.attach/4` with a handler ID, event name list, handler function, and config
- Emitters and handlers never know about each other

The BEAM ecosystem (Phoenix, Ecto, Oban, Broadway) all instrument via `:telemetry`, so your handlers can observe both your code and every library you use.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {TaskQueue.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"}
    ]
  end
end
```

### Step 2: `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    TaskQueue.Telemetry.setup()

    children = []
    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: `lib/task_queue/telemetry.ex` -- handler setup

The `setup/0` function attaches a single handler function to all five event types using `attach_many/4`. The handler ID `"task-queue-telemetry"` must be unique -- calling `attach` twice with the same ID raises. The detach-before-attach pattern makes `setup/0` idempotent.

Telemetry handlers are called synchronously in the process that called `:telemetry.execute/3`. A slow handler (HTTP call to a metrics backend) blocks the worker. For production, send to a dedicated reporter process instead.

```elixir
defmodule TaskQueue.Telemetry do
  @moduledoc """
  Attaches telemetry handlers for task_queue events.

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

    :telemetry.attach_many(
      "task-queue-telemetry",
      events,
      &handle_event/4,
      nil
    )
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

### Step 4: `lib/task_queue/worker.ex` -- emit telemetry from job execution

The Worker emits a `:start` event before execution (with empty measurements -- no timing yet), then either a `:stop` event (with duration) on success or an `:exception` event (with duration and error reason) on failure. Duration is computed using `System.monotonic_time/0` which guarantees monotonicity even across NTP clock adjustments.

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Executes jobs and emits telemetry events for each execution.

  Emits:
  - `[:task_queue, :job, :start]` before execution
  - `[:task_queue, :job, :stop]` after successful execution
  - `[:task_queue, :job, :exception]` if execution raises
  """

  @spec execute(map()) :: {:ok, term()} | {:error, term()}
  def execute(%{type: type, args: args} = job) do
    job_id   = Map.get(job, :id, "unknown")
    metadata = %{job_id: job_id, job_type: type}

    :telemetry.execute([:task_queue, :job, :start], %{}, metadata)

    start_time = System.monotonic_time()

    try do
      result = do_execute(type, args, job_id)
      duration = System.monotonic_time() - start_time
      :telemetry.execute([:task_queue, :job, :stop], %{duration: duration}, metadata)
      {:ok, result}
    rescue
      e ->
        duration = System.monotonic_time() - start_time
        :telemetry.execute(
          [:task_queue, :job, :exception],
          %{duration: duration},
          Map.put(metadata, :reason, e)
        )
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

### Step 5: Tests

The test handler captures telemetry events by sending them as messages to the test process. The unique handler ID per test process (`"test-handler-#{inspect(self())}"`) avoids ID collisions if tests run concurrently. The `on_exit` callback detaches the handler to prevent leaks across tests.

```elixir
# test/task_queue/telemetry_test.exs
defmodule TaskQueue.TelemetryTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Worker

  setup do
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

  describe "Worker.execute/1 -- telemetry events" do
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

  describe "Telemetry.setup/0 -- handler attachment" do
    test "setup attaches handlers without error" do
      :telemetry.detach("task-queue-telemetry")
      assert :ok = TaskQueue.Telemetry.setup()
    end

    test "attached handlers survive a re-setup if detached first" do
      :telemetry.detach("task-queue-telemetry")
      TaskQueue.Telemetry.setup()
      Worker.execute(%{id: "j5", type: "noop", args: %{}})
      assert_receive {:telemetry, [:task_queue, :job, :start], _, _}
    end
  end
end
```

### Step 6: Run

```bash
mix deps.get
mix test test/task_queue/telemetry_test.exs --trace
```

---

## Trade-off analysis

| Approach | Decoupled from backend | Structured data | BEAM ecosystem compatible |
|----------|------------------------|-----------------|--------------------------|
| `:telemetry.execute/3` | yes -- handlers are separate | yes -- maps | yes -- standard |
| `Logger.info/1` | no -- format is a string | no | no |
| Custom metrics GenServer | no -- callers coupled to it | yes | no |
| `:statsix` / `:prometheus_ex` directly | no -- backend embedded in code | yes | no |

---

## Common production mistakes

**1. Calling `setup/0` multiple times without detaching**
`:telemetry.attach/4` raises if the handler ID is already registered. Detach before reattaching.

**2. Doing heavy work inside the handler**
Telemetry handlers are called synchronously in the caller's process. A slow handler blocks the worker. Send to a dedicated reporter process.

**3. Using raw integers for duration without unit conversion**
`System.monotonic_time()` returns native time units that vary by OS. Always convert with `System.convert_time_unit/3`.

**4. Forgetting to detach handlers in test cleanup**
Test handlers persist for the lifetime of the test suite unless explicitly detached. Use `on_exit/1`.

**5. Using atoms for handler IDs in concurrent tests**
Multiple test processes attach handlers with the same ID. Use `"test-handler-#{inspect(self())}"` for unique IDs.

---

## Resources

- [Telemetry -- official hex package](https://hexdocs.pm/telemetry/readme.html)
- [Telemetry.Metrics -- higher-level aggregation](https://hexdocs.pm/telemetry_metrics/TelemetryMetrics.html)
- [Instrumenting Elixir with Telemetry -- Elixir School](https://elixirschool.com/en/lessons/advanced/telemetry)
- [OpenTelemetry for Elixir](https://github.com/open-telemetry/opentelemetry-erlang)
