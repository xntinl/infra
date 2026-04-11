# Application: The OTP Entry Point

## Why Application matters

`Application` is the topmost layer of an OTP release. It is the contract between your code
and the BEAM runtime: OTP calls `start/2` when the VM boots, and your code returns the root
Supervisor PID. Everything else in the system hangs from that PID.

Without a registered Application module, your Supervisor is never started. You would have
to manually call your supervisor's `start_link/0` every time. More importantly:

- OTP guarantees dependency ordering for your application's dependencies.
- `Application.get_env/3` is the standard runtime configuration API.
- `stop/1` gives you a hook to flush logs and release resources before the VM exits.

---

## The business problem

Build a `TaskQueue.Application` that:

1. Reads configuration: max queue size, job TTL, worker count, and log level.
2. Starts the root Supervisor with TaskRegistry, QueueServer, and Worker children.
3. Logs the startup configuration for observability.
4. In `stop/1`, logs the shutdown event.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── task_registry.ex
│       ├── queue_server.ex
│       └── worker.ex
├── config/
│   ├── config.exs
│   └── dev.exs
├── test/
│   └── task_queue/
│       └── application_test.exs
└── mix.exs
```

---

## Implementation

### `config/config.exs`

```elixir
import Config

config :task_queue,
  max_queue_size: 1_000,
  job_ttl_ms: 300_000,
  worker_count: 4,
  log_level: :info

import_config "#{config_env()}.exs"
```

### `config/dev.exs`

```elixir
import Config

config :task_queue,
  max_queue_size: 100,
  job_ttl_ms: 60_000,
  log_level: :debug
```

### `mix.exs` — register the Application module

```elixir
def application do
  [
    mod: {TaskQueue.Application, []},
    extra_applications: [:logger, :crypto]
  ]
end
```

Without the `mod:` key, OTP never calls `start/2`. The application compiles fine,
but in production `mix run --no-halt` starts nothing.

### `lib/task_queue/task_registry.ex`

```elixir
defmodule TaskQueue.TaskRegistry do
  use Agent

  def start_link(initial \\ %{}) do
    Agent.start_link(fn -> initial end, name: __MODULE__)
  end

  def register(task_id) do
    entry = %{status: :pending, updated_at: System.monotonic_time(:millisecond)}
    Agent.update(__MODULE__, fn state -> Map.put(state, task_id, entry) end)
  end

  def get(task_id) do
    Agent.get(__MODULE__, fn state -> Map.get(state, task_id) end)
  end

  def stats do
    Agent.get(__MODULE__, fn state ->
      Enum.reduce(state, %{pending: 0, running: 0, done: 0, failed: 0}, fn {_id, entry}, acc ->
        Map.update(acc, entry.status, 1, &(&1 + 1))
      end)
    end)
  end
end
```

### `lib/task_queue/queue_server.ex`

```elixir
defmodule TaskQueue.QueueServer do
  use GenServer
  require Logger

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def push(payload) do
    job = %{
      id: :crypto.strong_rand_bytes(8) |> Base.url_encode64(padding: false),
      payload: payload,
      queued_at: System.monotonic_time(:millisecond)
    }
    GenServer.cast(__MODULE__, {:push, job})
  end

  def pop, do: GenServer.call(__MODULE__, :pop)
  def size, do: GenServer.call(__MODULE__, :size)

  @impl GenServer
  def init(_opts), do: {:ok, []}

  @impl GenServer
  def handle_cast({:push, job}, state), do: {:noreply, state ++ [job]}

  @impl GenServer
  def handle_call(:pop, _from, []), do: {:reply, {:error, :empty}, []}
  def handle_call(:pop, _from, [job | rest]), do: {:reply, {:ok, job}, rest}

  @impl GenServer
  def handle_call(:size, _from, state), do: {:reply, length(state), state}

  @impl GenServer
  def handle_info(_, state), do: {:noreply, state}
end
```

### `lib/task_queue/worker.ex`

```elixir
defmodule TaskQueue.Worker do
  use GenServer
  require Logger

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl GenServer
  def init(_opts) do
    Logger.info("Worker started")
    {:ok, %{jobs_processed: 0}}
  end

  @impl GenServer
  def handle_info(_, state), do: {:noreply, state}
end
```

### `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application
  require Logger

  @impl Application
  def start(_type, _args) do
    max_queue_size = Application.get_env(:task_queue, :max_queue_size, 1_000)
    job_ttl_ms     = Application.get_env(:task_queue, :job_ttl_ms, 300_000)
    worker_count   = Application.get_env(:task_queue, :worker_count, 4)
    log_level      = Application.get_env(:task_queue, :log_level, :info)

    Logger.configure(level: log_level)

    Logger.info("""
    TaskQueue starting
      max_queue_size: #{max_queue_size}
      job_ttl_ms:     #{job_ttl_ms}
      worker_count:   #{worker_count}
      log_level:      #{log_level}
    """)

    children = [
      TaskQueue.TaskRegistry,
      TaskQueue.QueueServer,
      TaskQueue.Worker
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.RootSupervisor]
    Supervisor.start_link(children, opts)
  end

  @impl Application
  def stop(_state) do
    Logger.info("TaskQueue stopped")
    :ok
  end
end
```

Configuration is read at runtime with `Application.get_env/3`, not at compile time
with module attributes. This means config values can be changed between compilations
(e.g., via `config/runtime.exs` in production) without recompiling.

### Tests

```elixir
# test/task_queue/application_test.exs
defmodule TaskQueue.ApplicationTest do
  use ExUnit.Case, async: false

  test "Application is listed in started_applications after mix start" do
    started = Application.started_applications() |> Enum.map(&elem(&1, 0))
    assert :task_queue in started
  end

  test "RootSupervisor is running" do
    assert pid = Process.whereis(TaskQueue.RootSupervisor)
    assert is_pid(pid)
    assert Process.alive?(pid)
  end

  test "all supervised children are running" do
    assert Process.whereis(TaskQueue.TaskRegistry) != nil
    assert Process.whereis(TaskQueue.QueueServer) != nil
    assert Process.whereis(TaskQueue.Worker) != nil
  end

  test "Application.get_env reads task_queue config" do
    assert is_integer(Application.get_env(:task_queue, :max_queue_size))
    assert is_integer(Application.get_env(:task_queue, :job_ttl_ms))
  end

  test "Application.spec returns metadata for task_queue" do
    spec = Application.spec(:task_queue)
    assert spec != nil
    assert Keyword.get(spec, :description) != nil
  end

  test "end-to-end: push a job and process it through the running system" do
    TaskQueue.TaskRegistry.register("e2e_job")
    TaskQueue.QueueServer.push("e2e_payload")
    Process.sleep(20)

    assert 1 = TaskQueue.QueueServer.size()
    TaskQueue.QueueServer.pop()
    assert 0 = TaskQueue.QueueServer.size()
  end
end
```

### Run the tests

```bash
mix test test/task_queue/application_test.exs --trace
```

---

## Common production mistakes

**1. Forgetting `mod:` in mix.exs**
Without `mod: {TaskQueue.Application, []}`, OTP never calls `start/2`.

**2. Not returning `{:ok, pid}` from `start/2`**
`start/2` must return `{:ok, pid}` — the value from `Supervisor.start_link`.

**3. Reading `Application.get_env` at compile time**
```elixir
# WRONG — resolved at compile time
@max_size Application.get_env(:task_queue, :max_queue_size, 1_000)

# CORRECT — resolved at runtime
def max_size, do: Application.get_env(:task_queue, :max_queue_size, 1_000)
```

**4. Doing slow initialization in `start/2`**
OTP has a startup timeout. Slow initialization should happen in a dedicated GenServer's
`init/1` — not in `start/2`.

---

## Resources

- [Application — HexDocs](https://hexdocs.pm/elixir/Application.html)
- [Config — HexDocs](https://hexdocs.pm/elixir/Config.html)
- [Mix and OTP: Application](https://elixir-lang.org/getting-started/mix-otp/supervisor-and-application.html)
