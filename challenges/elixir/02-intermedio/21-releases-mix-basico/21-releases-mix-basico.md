# Mix Releases

## Goal

Build a `task_queue` project as a Mix release -- a self-contained binary that runs without Elixir or Erlang installed on the target server. Learn how to configure releases in `mix.exs`, build them, operate them (start/stop/remote shell), and understand why `runtime.exs` is what makes releases configurable.

---

## What a release contains

```
_build/prod/rel/task_queue/
├── bin/
│   ├── task_queue          <- control script (start, stop, remote, eval, ...)
│   └── task_queue.bat      <- Windows equivalent
├── erts-15.x/              <- Erlang runtime -- no Erlang needed on target server
├── lib/
│   ├── task_queue-0.1.0/   <- your compiled code
│   ├── jason-1.4.4/        <- all dependencies included
│   └── ...
└── releases/
    └── 0.1.0/
        └── task_queue.rel  <- OTP release descriptor
```

The `erts/` inclusion means the same binary runs on any machine with the same OS and CPU architecture. No Elixir, no `mix`, nothing else required.

---

## Why `runtime.exs` is what makes releases configurable

Without `runtime.exs`, configuration is frozen in the binary at compile time. Every environment change requires a recompile and redeploy. With `runtime.exs`, the same binary reads env vars fresh on each startup -- one build, multiple environments.

```
Build time: compile source + bake in config.exs/prod.exs values
Start time: evaluate runtime.exs, read env vars, start the app
```

---

## Implementation

### Step 1: `mix.exs` -- add release configuration

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      releases: releases()
    ]
  end

  defp releases do
    [
      task_queue: [
        include_executables_for: [:unix],
        include_erts: true
      ]
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
      {:jason, "~> 1.4"},
      {:dialyxir, "~> 1.0", only: :dev, runtime: false},
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

The `releases/0` function configures the release: `include_executables_for: [:unix]` generates the shell control scripts, and `include_erts: true` bundles the Erlang runtime so the target machine does not need Erlang installed.

### Step 2: Configuration files

```elixir
# config/config.exs
import Config

config :task_queue,
  max_retries: 3,
  worker_pool_size: 5,
  webhook_url: nil

config :logger, level: :info

import_config "#{config_env()}.exs"
```

```elixir
# config/dev.exs
import Config

config :task_queue,
  worker_pool_size: 2,
  webhook_url: "http://localhost:4000/webhook"

config :logger, level: :debug
```

```elixir
# config/test.exs
import Config

config :task_queue,
  worker_pool_size: 1,
  max_retries: 1,
  webhook_url: nil

config :logger, level: :warning
```

```elixir
# config/prod.exs
import Config

config :logger, level: :info
```

```elixir
# config/runtime.exs
import Config

if config_env() == :prod do
  webhook_url =
    System.get_env("WEBHOOK_URL") ||
      raise """
      environment variable WEBHOOK_URL is missing.
      Set it to the URL that should receive job completion notifications.
      """

  worker_pool_size =
    System.get_env("WORKER_POOL_SIZE", "5")
    |> String.to_integer()

  max_retries =
    System.get_env("MAX_RETRIES", "3")
    |> String.to_integer()

  config :task_queue,
    webhook_url: webhook_url,
    worker_pool_size: worker_pool_size,
    max_retries: max_retries
end
```

### Step 3: `lib/task_queue/application.ex` -- startup instrumentation

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    pool_size = Application.get_env(:task_queue, :worker_pool_size, 5)
    :logger.info("TaskQueue starting: pool_size=#{pool_size}")

    children = []
    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 4: `lib/task_queue/scheduler.ex` -- config reader

```elixir
defmodule TaskQueue.Scheduler do
  @moduledoc """
  Dispatches jobs from the queue to available workers.
  Reads configuration from the application environment at runtime.
  """

  @spec worker_pool_size() :: pos_integer()
  def worker_pool_size do
    Application.get_env(:task_queue, :worker_pool_size, 5)
  end

  @spec max_retries() :: non_neg_integer()
  def max_retries do
    Application.get_env(:task_queue, :max_retries, 3)
  end

  @spec webhook_url() :: String.t() | nil
  def webhook_url do
    Application.get_env(:task_queue, :webhook_url, nil)
  end

  @spec config() :: map()
  def config do
    %{
      worker_pool_size: worker_pool_size(),
      max_retries: max_retries(),
      webhook_url: webhook_url(),
      log_level: Application.get_env(:logger, :level, :info)
    }
  end
end
```

### Step 5: Build and inspect the release

```bash
MIX_ENV=prod mix deps.get
MIX_ENV=prod mix release

ls _build/prod/rel/task_queue/
ls _build/prod/rel/task_queue/bin/
du -sh _build/prod/rel/task_queue/
```

### Step 6: Operate the release

```bash
RELEASE="./_build/prod/rel/task_queue/bin/task_queue"

# Start in foreground -- logs to stdout, Ctrl+C to stop
WEBHOOK_URL="https://hooks.example.com/notify" $RELEASE start

# Start as background daemon
WEBHOOK_URL="https://hooks.example.com/notify" $RELEASE daemon

# Verify it's running
$RELEASE pid

# Evaluate an expression in the running node -- no interruption
$RELEASE eval "IO.puts(TaskQueue.Scheduler.worker_pool_size())"

# Stop the daemon
$RELEASE stop
```

### Step 7: Remote shell -- live production debugging

```bash
# While the daemon is running:
$RELEASE remote

# Inside the remote shell -- you are connected to the live node
Supervisor.which_children(TaskQueue.Supervisor)
TaskQueue.Scheduler.config()

# IMPORTANT: to exit WITHOUT killing the node:
# Press Ctrl+C twice (disconnects the shell, node keeps running)
# Do NOT call System.halt() -- that kills the running application
```

### Step 8: Tests

```elixir
# test/task_queue/release_config_test.exs
defmodule TaskQueue.ReleaseConfigTest do
  use ExUnit.Case, async: true

  alias TaskQueue.Scheduler

  test "application starts and exposes config" do
    config = Scheduler.config()
    assert is_integer(config.worker_pool_size)
    assert is_integer(config.max_retries)
    assert config.worker_pool_size >= 1
  end

  test "worker_pool_size is 1 in test environment (from test.exs)" do
    assert Scheduler.worker_pool_size() == 1
  end

  test "webhook_url is nil in test environment" do
    assert Scheduler.webhook_url() == nil
  end
end
```

```bash
mix test test/task_queue/release_config_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Source deployment | Mix release |
|--------|-------------------|-------------|
| Requires Elixir on server | yes | no |
| Build artifact size | source files only | ~25-40 MB with ERTS |
| Configuration | `mix run` reads config files | `runtime.exs` reads env vars |
| Remote debugging | `iex -S mix` | `bin/app remote` |
| Hot code upgrades | manual | `bin/app upgrade` (advanced) |
| CI reproducibility | depends on Elixir version on server | self-contained |

`start_permanent: Mix.env() == :prod` causes the Erlang VM to exit if the top-level supervisor crashes. In production, a crashed top-level supervisor means the application is broken beyond recovery -- exiting lets the orchestrator (systemd, Docker, Kubernetes) restart from a clean state. In development, you want the VM to stay alive after a crash so you can inspect the error in IEx.

---

## Common production mistakes

**1. Building the release without `MIX_ENV=prod`**
Dev dependencies are included, dev config is baked in, and `start_permanent` is `false`.

**2. `System.halt()` in the remote shell**
This kills the Erlang VM and terminates your production application. To disconnect, press Ctrl+C twice.

**3. Assuming code changes take effect without a rebuild**
A release is a compiled binary. Editing `lib/` has no effect until you run `mix release` again.

**4. Not setting `WEBHOOK_URL` before starting in prod**
With `runtime.exs` configured to raise on missing vars, the application will refuse to start. This is intentional.

**5. Not testing `runtime.exs` validation locally**
Run `MIX_ENV=prod mix run --no-halt` without the env vars. You should see the `raise` message immediately.

---

## Resources

- [Mix.Tasks.Release -- official docs](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
- [Releases guide -- Elixir official](https://elixir-lang.org/getting-started/mix-otp/config-and-releases.html)
- [Deploying with Releases -- Phoenix guide](https://hexdocs.pm/phoenix/releases.html)
- [Runtime Configuration -- Config module](https://hexdocs.pm/elixir/Config.html)
