# Application: The OTP Entry Point

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system has a Supervisor that manages its components (exercise 05). But how
does the Supervisor start in the first place? Who reads the configuration? Who decides
whether to boot in `:dev` mode or `:prod` mode? That is the `Application` behaviour's job.

`Application` is the topmost layer of an OTP release. It is the contract between your code
and the BEAM runtime: OTP calls `start/2` when the VM boots, and your code returns the root
Supervisor PID. Everything else in the system hangs from that PID.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── supervisor.ex        # exercise 05
│       ├── queue_server.ex      # exercise 04
│       ├── task_registry.ex     # exercise 02
│       └── worker.ex            # exercise 05
├── config/
│   ├── config.exs
│   └── dev.exs
├── test/
│   └── task_queue/
│       └── application_test.exs # given tests — must pass without modification
└── mix.exs                      # ← the :mod key registers the Application
```

---

## Why Application matters

Without a registered Application module, your Supervisor is never started. You would have
to manually call `TaskQueue.Supervisor.start_link()` every time. More importantly:

- OTP guarantees dependency ordering: if `:logger` or `:crypto` is listed in your
  `mix.exs` dependencies, they are fully started before your `start/2` is called.
- `Application.get_env/3` is the standard runtime configuration API. It reads from
  `config/config.exs` (and its environment overrides) without hardcoding values.
- `stop/1` gives you a hook to flush logs, close connections, and release resources before
  the VM exits — crucial for production deployments with graceful drain.

---

## The business problem

`TaskQueue.Application` must:

1. Read configuration: max queue size, job TTL, worker count, and log level.
2. Start the root Supervisor.
3. Log the startup configuration for observability.
4. In `stop/1`, log the shutdown event.

The configuration must come from `config/config.exs`, not hardcoded in the module.

---

## Implementation

### Step 1: `config/config.exs`

```elixir
import Config

config :task_queue,
  max_queue_size: 1_000,
  job_ttl_ms: 300_000,
  worker_count: 4,
  log_level: :info

import_config "#{config_env()}.exs"
```

### Step 2: `config/dev.exs`

```elixir
import Config

config :task_queue,
  max_queue_size: 100,
  job_ttl_ms: 60_000,
  log_level: :debug
```

### Step 3: `mix.exs` — register the Application module

```elixir
def application do
  [
    mod: {TaskQueue.Application, []},
    extra_applications: [:logger, :crypto]
  ]
end
```

Without the `mod:` key, OTP never calls `start/2`. The application compiles fine,
`mix test` runs, but in production `mix run --no-halt` starts nothing. This is the
single most common OTP configuration bug.

### Step 4: `lib/task_queue/application.ex`

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

    children = build_children(worker_count)

    opts = [strategy: :one_for_one, name: TaskQueue.RootSupervisor]

    Supervisor.start_link(children, opts)
  end

  @impl Application
  def stop(_state) do
    Logger.info("TaskQueue stopped")
    :ok
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  defp build_children(_worker_count) do
    # For now, a single worker. Exercise 14 (Registry) adds dynamic workers.
    [
      TaskQueue.TaskRegistry,
      TaskQueue.QueueServer,
      TaskQueue.Worker
    ]
  end
end
```

The `start/2` function reads configuration with `Application.get_env/3`, configures the
Logger level, logs the startup parameters, and starts the root Supervisor. The return
value of `Supervisor.start_link/2` is `{:ok, pid}` — exactly what OTP requires from
`start/2`. Returning `:ok` or any other value causes OTP to mark the application as
failed to start.

The `stop/1` callback is called by OTP when the application is shutting down (either
via `Application.stop/1` or during VM termination). Here we log the event; in production
you would flush log buffers, close database connections, and drain pending work.

Configuration is read at runtime with `Application.get_env/3`, not at compile time
with module attributes. This means the config values can be changed between compilations
(e.g., via `config/runtime.exs` in production) without recompiling the module.

### Step 5: Given tests — must pass without modification

```elixir
# test/task_queue/application_test.exs
defmodule TaskQueue.ApplicationTest do
  use ExUnit.Case, async: false

  test "Application is listed in started_applications after mix start" do
    # In a mix test run, the Application starts automatically via mix.exs :mod
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
    # Verify config.exs values are accessible at runtime
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

### Step 6: Run the tests

```bash
mix test test/task_queue/application_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `Application.get_env` (runtime) | `Application.compile_env` | Hardcoded constant |
|--------|--------------------------------|--------------------------|-------------------|
| When resolved | At call time (runtime) | At compile time | At compile time |
| Survives hot code reload | Yes | No — requires recompile | No |
| Suitable for secrets | Yes (from env vars via `runtime.exs`) | No | Never |
| Suitable for feature flags | Yes | Only if stable | No |
| Type safety | None out of the box | None | N/A |

Reflection question: `start/2` logs the configuration. In a production deployment where
`config/runtime.exs` reads from environment variables, what happens if a required
environment variable is not set? Where should you validate that — in `start/2` or in
`runtime.exs` itself?

---

## Common production mistakes

**1. Forgetting `mod:` in mix.exs**
Without `mod: {TaskQueue.Application, []}`, OTP never calls `start/2`. The application
compiles fine, `mix test` runs, but in production `mix run --no-halt` starts nothing.
This is the single most common OTP configuration bug.

**2. Not returning `{:ok, pid}` from `start/2`**
`start/2` must return `{:ok, pid}` — the value from `Supervisor.start_link`. Returning
`:ok` or any other value causes OTP to mark the application as failed to start.

**3. Reading `Application.get_env` at compile time**
```elixir
# WRONG — resolved at compile time, before config.exs is loaded
@max_size Application.get_env(:task_queue, :max_queue_size, 1_000)

# CORRECT — resolved at runtime
def max_size, do: Application.get_env(:task_queue, :max_queue_size, 1_000)
# Or for truly stable compile-time values:
@max_size Application.compile_env(:task_queue, :max_queue_size, 1_000)
```

**4. Doing slow initialization in `start/2`**
OTP has a startup timeout. If `start/2` blocks for more than a few seconds, OTP considers
the start failed. Slow initialization (loading a large data file, warming a cache) should
happen in the `init/1` of a dedicated GenServer child — not in `start/2`.

---

## Resources

- [Application — HexDocs](https://hexdocs.pm/elixir/Application.html)
- [Application.get_env/3 — HexDocs](https://hexdocs.pm/elixir/Application.html#get_env/3)
- [Config — HexDocs](https://hexdocs.pm/elixir/Config.html)
- [Mix and OTP: Application](https://elixir-lang.org/getting-started/mix-otp/supervisor-and-application.html)
