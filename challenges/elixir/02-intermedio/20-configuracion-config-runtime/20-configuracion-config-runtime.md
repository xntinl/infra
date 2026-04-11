# Configuration: Config Files and Runtime

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` processes background jobs and notifies webhooks on completion. Different environments need different behavior: verbose logging in dev, fast timeouts in test, mandatory env vars in prod. The wrong approach bakes secrets into source code or reads env vars at compile time, breaking releases.

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
│   ├── config.exs        # ← base configuration — all environments
│   ├── dev.exs           # ← development overrides
│   ├── test.exs          # ← test overrides
│   ├── prod.exs          # ← production compile-time config
│   └── runtime.exs       # ← you implement this — reads env vars at startup
├── test/
│   └── task_queue/
│       └── config_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The ops team needs to deploy `task_queue` to multiple environments with zero code changes. They inject configuration via environment variables at startup. Specifically:

1. `WEBHOOK_URL` — required in production, must fail loudly if missing
2. `WORKER_POOL_SIZE` — optional, defaults to 5
3. `LOG_LEVEL` — optional, defaults to `info`
4. `MAX_RETRIES` — optional, defaults to 3

The dev team wants verbose logs and long timeouts locally. Tests must be deterministic and silent.

---

## Why compile-time vs runtime matters

`config/config.exs`, `dev.exs`, `prod.exs` are evaluated at compile time — the values are baked into BEAM files. This is fine for constants that never change between deployments.

`config/runtime.exs` is evaluated when the application starts, including inside a release. It can read `System.get_env/2` reliably because the OS environment is available at startup.

The critical mistake: calling `System.get_env("DATABASE_URL")` in `config/config.exs`. During CI compilation, that env var may not exist. The value is frozen to `nil` and cannot be changed without recompiling.

```
compile-time (config.exs):  values frozen in BEAM binary
runtime (runtime.exs):      values read fresh on every startup
```

---

## Implementation

### Step 1: `config/config.exs` — base values for all environments

```elixir
# config/config.exs
import Config

# Shared defaults — can be overridden per environment
config :task_queue,
  max_retries: 3,
  worker_pool_size: 5,
  log_level: :info,
  webhook_url: nil  # must be set in runtime.exs for prod

config :logger, level: :info

# Import the env-specific file last so it can override these defaults
import_config "#{config_env()}.exs"
```

### Step 2: `config/dev.exs` — verbose development settings

```elixir
# config/dev.exs
import Config

# TODO: override :log_level with :debug
# TODO: override :worker_pool_size with 2 (small pool, easier to observe)
# TODO: set :webhook_url to a local mock server, e.g. "http://localhost:4000/webhook"
# TODO: configure :logger level to :debug
```

### Step 3: `config/test.exs` — fast, deterministic test settings

```elixir
# config/test.exs
import Config

# TODO: override :log_level with :warning (silence noise in test output)
# TODO: override :worker_pool_size with 1 (single worker for deterministic tests)
# TODO: override :max_retries with 1 (fail fast in tests)
# TODO: set :webhook_url to nil (webhook calls must be mocked in tests)
# TODO: configure :logger level to :warning
```

### Step 4: `config/runtime.exs` — read env vars at startup

```elixir
# config/runtime.exs
import Config

# LOG_LEVEL is configurable in all environments
log_level =
  System.get_env("LOG_LEVEL", "info")
  |> String.to_existing_atom()

config :logger, level: log_level

# In production, WEBHOOK_URL is mandatory — fail loud and early
if config_env() == :prod do
  webhook_url =
    System.get_env("WEBHOOK_URL") ||
      raise """
      environment variable WEBHOOK_URL is missing.
      Set it to the URL that should receive job completion notifications.
      Example: https://hooks.example.com/task-queue
      """

  worker_pool_size =
    System.get_env("WORKER_POOL_SIZE", "5")
    |> String.to_integer()

  max_retries =
    System.get_env("MAX_RETRIES", "3")
    |> String.to_integer()

  # TODO: configure :task_queue with webhook_url, worker_pool_size, max_retries
end
```

### Step 5: `lib/task_queue/scheduler.ex` — read config at runtime

```elixir
defmodule TaskQueue.Scheduler do
  @moduledoc """
  Dispatches jobs from the queue to available workers.

  Configuration is read from the application environment at runtime,
  not hardcoded — allowing ops to tune without recompiling.
  """

  def worker_pool_size do
    # TODO: read :worker_pool_size from Application.get_env with default 5
    # HINT: Application.get_env(:task_queue, :worker_pool_size, 5)
  end

  def max_retries do
    # TODO: read :max_retries with default 3
  end

  def webhook_url do
    # TODO: read :webhook_url with default nil
  end

  @doc """
  Returns a map of all current configuration values.
  Useful for health checks and debugging.
  """
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

### Step 6: Given tests — must pass without modification

```elixir
# test/task_queue/config_test.exs
defmodule TaskQueue.ConfigTest do
  use ExUnit.Case, async: false
  # async: false because Application.put_env modifies global state

  alias TaskQueue.Scheduler

  describe "Scheduler reads application config" do
    test "worker_pool_size defaults to 1 in test environment" do
      # test.exs sets this to 1 for determinism
      assert Scheduler.worker_pool_size() == 1
    end

    test "max_retries defaults to 1 in test environment" do
      assert Scheduler.max_retries() == 1
    end

    test "webhook_url is nil in test environment" do
      assert Scheduler.webhook_url() == nil
    end

    test "config/0 returns a map with all keys" do
      config = Scheduler.config()
      assert Map.has_key?(config, :worker_pool_size)
      assert Map.has_key?(config, :max_retries)
      assert Map.has_key?(config, :webhook_url)
      assert Map.has_key?(config, :log_level)
    end
  end

  describe "Application.put_env for test isolation" do
    setup do
      original_retries = Application.get_env(:task_queue, :max_retries)

      on_exit(fn ->
        Application.put_env(:task_queue, :max_retries, original_retries)
      end)

      :ok
    end

    test "put_env changes visible value for this test" do
      Application.put_env(:task_queue, :max_retries, 10)
      assert Scheduler.max_retries() == 10
    end

    test "original value is restored after the previous test" do
      # This passes because setup/on_exit restored the value
      assert Scheduler.max_retries() == 1
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/task_queue/config_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `config.exs` (compile-time) | `runtime.exs` | `Application.put_env` |
|--------|----------------------------|---------------|----------------------|
| When evaluated | at compile time | at application start | at runtime, in-memory only |
| Can read env vars reliably | no (not in releases) | yes | yes |
| Survives a release | values frozen in binary | re-evaluated on each start | lost on restart |
| Use case | constants, library config | secrets, deployment vars | test isolation, feature flags |

Reflection question: what happens if you call `String.to_existing_atom/1` with a value that was never compiled into any atom? Why is this safer than `String.to_atom/1` for reading env vars?

---

## Common production mistakes

**1. `System.get_env` in `config.exs`**
The value is read at compile time and frozen into the release binary. The env var may not exist during compilation or may differ between environments. Always use `runtime.exs`.

**2. Not restoring `Application.put_env` in tests**
`put_env` changes global process-level state. Without `on_exit` cleanup, a test that sets a value contaminates subsequent tests. This is a common source of intermittent test failures.

**3. Hardcoding secrets in `prod.exs`**
`prod.exs` is committed to version control. Any secret in it is permanently in your git history. Secrets belong exclusively in `runtime.exs`, read from env vars.

**4. `String.to_atom/1` for env var values**
Atoms are not garbage collected. If the value comes from user input or a dynamic source, use `String.to_existing_atom/1` — it raises if the atom was never compiled, preventing atom table exhaustion.

**5. Missing `import_config "#{config_env()}.exs"` in `config.exs`**
Without this line, the env-specific files are never loaded. A common mistake when setting up a project from scratch.

---

## Resources

- [Config module — official docs](https://hexdocs.pm/elixir/Config.html)
- [Application module — official docs](https://hexdocs.pm/elixir/Application.html)
- [Mix.Tasks.Release — runtime config](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
- [Releases and Runtime Configuration — Phoenix guide](https://hexdocs.pm/phoenix/releases.html#runtime-configuration)
