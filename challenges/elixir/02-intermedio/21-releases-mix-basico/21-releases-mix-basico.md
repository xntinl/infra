# Mix Releases

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` is ready for production. The ops team needs a deployable artifact — a self-contained binary that runs without Elixir or Erlang installed on the target server. That artifact is a Mix release.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── config/
│   ├── config.exs
│   ├── dev.exs
│   ├── test.exs
│   ├── prod.exs
│   └── runtime.exs         # reads WEBHOOK_URL, WORKER_POOL_SIZE, etc.
├── test/
│   └── task_queue/
└── mix.exs                 # ← you add releases/0 here
```

---

## The business problem

The DevOps pipeline needs to:

1. Build a release on the CI server (which has Elixir installed)
2. Copy the release artifact to a production VM (which does not have Elixir)
3. Start, stop, and inspect the running application via the binary
4. Connect a remote shell for live debugging without restarting

None of this is possible with a raw source deployment.

---

## What a release contains

```
_build/prod/rel/task_queue/
├── bin/
│   ├── task_queue          ← control script (start, stop, remote, eval, ...)
│   └── task_queue.bat      ← Windows equivalent
├── erts-15.x/              ← Erlang runtime — no Erlang needed on target server
├── lib/
│   ├── task_queue-0.1.0/   ← your compiled code
│   ├── jason-1.4.4/        ← all dependencies included
│   └── ...
└── releases/
    └── 0.1.0/
        └── task_queue.rel  ← OTP release descriptor
```

The `erts/` inclusion means the same binary runs on any machine with the same OS and CPU architecture. No Elixir, no `mix`, nothing else required.

---

## Why `runtime.exs` is what makes releases configurable

Without `runtime.exs`, configuration is frozen in the binary at compile time. Every environment change requires a recompile and redeploy. With `runtime.exs`, the same binary reads `WEBHOOK_URL` fresh on each startup — one build, multiple environments.

```
Build time: compile source + bake in config.exs/prod.exs values
Start time: evaluate runtime.exs, read env vars, start the app
```

---

## Implementation

### Step 1: `mix.exs` — add release configuration

```elixir
# mix.exs
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
      {:req, "~> 0.5"},
      {:dialyxir, "~> 1.0", only: :dev, runtime: false},
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### Step 2: Build and inspect the release

```bash
# Always build releases with MIX_ENV=prod
MIX_ENV=prod mix deps.get
MIX_ENV=prod mix release

# Explore the generated structure
ls _build/prod/rel/task_queue/
ls _build/prod/rel/task_queue/bin/
du -sh _build/prod/rel/task_queue/
```

### Step 3: `lib/task_queue/application.ex` — startup instrumentation

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    pool_size = Application.get_env(:task_queue, :worker_pool_size, 5)

    # Log the resolved config so ops can verify env vars were picked up
    :logger.info("TaskQueue starting: pool_size=#{pool_size}")

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

### Step 4: Operate the release

```bash
RELEASE="./_build/prod/rel/task_queue/bin/task_queue"

# Start in foreground — logs to stdout, Ctrl+C to stop
WEBHOOK_URL="https://hooks.example.com/notify" $RELEASE start

# Start as background daemon
WEBHOOK_URL="https://hooks.example.com/notify" $RELEASE daemon

# Verify it's running
$RELEASE pid

# Evaluate an expression in the running node — no interruption
$RELEASE eval "IO.puts(TaskQueue.Scheduler.worker_pool_size())"

# Stop the daemon
$RELEASE stop
```

### Step 5: Remote shell — live production debugging

```bash
# While the daemon is running:
$RELEASE remote

# Inside the remote shell — you are connected to the live node
# Inspect the supervision tree
Supervisor.which_children(TaskQueue.Supervisor)

# Check current configuration
TaskQueue.Scheduler.config()

# Inspect the queue state
:sys.get_state(TaskQueue.QueueServer)

# IMPORTANT: to exit WITHOUT killing the node:
# Press Ctrl+C twice (disconnects the shell, node keeps running)
# Do NOT call System.halt() — that kills the running application
```

### Step 6: Given tests — must pass without modification

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
| Build artifact size | source files only | ~25–40 MB with ERTS |
| Configuration | `mix run` reads config files | `runtime.exs` reads env vars |
| Remote debugging | `iex -S mix` | `bin/app remote` |
| Hot code upgrades | manual | `bin/app upgrade` (advanced) |
| CI reproducibility | depends on Elixir version on server | self-contained |

Reflection question: `start_permanent: Mix.env() == :prod` causes the Erlang VM to exit if the top-level supervisor crashes. Why is this desirable in production but undesirable in development?

Answer: In production, a crashed top-level supervisor means the application is broken beyond recovery. Exiting the VM lets the orchestrator (systemd, Docker, Kubernetes) restart the entire node from a clean state, which is far safer than leaving a half-running application. In development, you want the VM to stay alive after a crash so you can inspect the error in IEx, fix the code, and recompile — without restarting the entire session and losing REPL state.

---

## Common production mistakes

**1. Building the release without `MIX_ENV=prod`**
The default is `:dev`. Dev dependencies (Dialyxir, ExDoc) are included, dev config is baked in, and `start_permanent` is `false` — the VM does not exit on supervisor crash.

**2. `System.halt()` in the remote shell**
This kills the Erlang VM and terminates your production application. To disconnect from the remote shell without affecting the running node, press Ctrl+C twice or call `exit(:normal)`.

**3. Assuming code changes take effect without a rebuild**
A release is a compiled binary. Editing `lib/` has no effect until you run `mix release` again. The running node executes the code that was compiled at build time.

**4. Not setting `WEBHOOK_URL` before starting in prod**
With `runtime.exs` configured to raise on missing vars, the application will refuse to start. This is intentional — a loud early failure beats a silent misconfiguration discovered mid-operation.

**5. Not testing `runtime.exs` validation locally**
Run `MIX_ENV=prod mix run --no-halt` without the env vars. You should see the `raise` message immediately. If you don't, your validation is not in `runtime.exs`.

---

## Resources

- [Mix.Tasks.Release — official docs](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
- [Releases guide — Elixir official](https://elixir-lang.org/getting-started/mix-otp/config-and-releases.html)
- [Deploying with Releases — Phoenix guide](https://hexdocs.pm/phoenix/releases.html)
- [Runtime Configuration — Config module](https://hexdocs.pm/elixir/Config.html)
