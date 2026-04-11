# Configuration: Config Files and Runtime

## Goal

Build a `task_queue` project with environment-specific configuration using compile-time config files (`config.exs`, `dev.exs`, `test.exs`, `prod.exs`) and runtime configuration (`runtime.exs`). Learn when each is appropriate and how to read config safely in application code.

---

## Why compile-time vs runtime matters

`config/config.exs`, `dev.exs`, `prod.exs` are evaluated at compile time -- the values are baked into BEAM files. This is fine for constants that never change between deployments.

`config/runtime.exs` is evaluated when the application starts, including inside a release. It can read `System.get_env/2` reliably because the OS environment is available at startup.

The critical mistake: calling `System.get_env("DATABASE_URL")` in `config/config.exs`. During CI compilation, that env var may not exist. The value is frozen to `nil` and cannot be changed without recompiling.

```
compile-time (config.exs):  values frozen in BEAM binary
runtime (runtime.exs):      values read fresh on every startup
```

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
    []
  end
end
```

### Step 2: `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = []
    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: `config/config.exs` -- base values for all environments

```elixir
# config/config.exs
import Config

config :task_queue,
  max_retries: 3,
  worker_pool_size: 5,
  log_level: :info,
  webhook_url: nil

config :logger, level: :info

import_config "#{config_env()}.exs"
```

The `import_config` at the end loads the environment-specific file, which can override any of these defaults. The order matters: the last `config` call for a given key wins.

### Step 4: `config/dev.exs` -- verbose development settings

```elixir
# config/dev.exs
import Config

config :task_queue,
  log_level: :debug,
  worker_pool_size: 2,
  webhook_url: "http://localhost:4000/webhook"

config :logger, level: :debug
```

### Step 5: `config/test.exs` -- fast, deterministic test settings

```elixir
# config/test.exs
import Config

config :task_queue,
  log_level: :warning,
  worker_pool_size: 1,
  max_retries: 1,
  webhook_url: nil

config :logger, level: :warning
```

Test config uses `worker_pool_size: 1` for determinism (only one worker, no race conditions) and `max_retries: 1` so failure tests complete quickly.

### Step 6: `config/runtime.exs` -- read env vars at startup

```elixir
# config/runtime.exs
import Config

log_level =
  System.get_env("LOG_LEVEL", "info")
  |> String.to_existing_atom()

config :logger, level: log_level

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

  config :task_queue,
    webhook_url: webhook_url,
    worker_pool_size: worker_pool_size,
    max_retries: max_retries
end
```

`String.to_existing_atom/1` is used instead of `String.to_atom/1` because atoms are never garbage-collected. An attacker (or a misconfigured env var) could create arbitrary atoms that exhaust the atom table (default limit: 1,048,576). For log levels, the atoms `:debug`, `:info`, `:warning`, `:error` are already compiled into the BEAM, so `to_existing_atom` succeeds for valid values and fails loudly for typos.

### Step 7: `lib/task_queue/scheduler.ex` -- read config at runtime

The Scheduler reads configuration from the application environment at runtime using `Application.get_env/3`. This means ops can change values via `runtime.exs` env vars or via `Application.put_env/3` without recompiling.

```elixir
defmodule TaskQueue.Scheduler do
  @moduledoc """
  Dispatches jobs from the queue to available workers.

  Configuration is read from the application environment at runtime,
  not hardcoded -- allowing ops to tune without recompiling.
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

  @doc """
  Returns a map of all current configuration values.
  Useful for health checks and debugging.
  """
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

### Step 8: Tests

```elixir
# test/task_queue/config_test.exs
defmodule TaskQueue.ConfigTest do
  use ExUnit.Case, async: false
  # async: false because Application.put_env modifies global state

  alias TaskQueue.Scheduler

  describe "Scheduler reads application config" do
    test "worker_pool_size defaults to 1 in test environment" do
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
      assert Scheduler.max_retries() == 1
    end
  end
end
```

The tests demonstrate a key pattern: `Application.put_env/3` changes global state. Without the `on_exit` cleanup in `setup`, a test that sets a value contaminates subsequent tests. This is a common source of intermittent test failures.

### Step 9: Run

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

---

## Common production mistakes

**1. `System.get_env` in `config.exs`**
The value is read at compile time and frozen into the release binary. The env var may not exist during compilation or may differ between environments. Always use `runtime.exs`.

**2. Not restoring `Application.put_env` in tests**
`put_env` changes global process-level state. Without `on_exit` cleanup, a test that sets a value contaminates subsequent tests.

**3. Hardcoding secrets in `prod.exs`**
`prod.exs` is committed to version control. Any secret in it is permanently in your git history. Secrets belong exclusively in `runtime.exs`, read from env vars.

**4. `String.to_atom/1` for env var values**
Atoms are not garbage collected. If the value comes from user input or a dynamic source, use `String.to_existing_atom/1`.

**5. Missing `import_config "#{config_env()}.exs"` in `config.exs`**
Without this line, the env-specific files are never loaded.

---

## Resources

- [Config module -- official docs](https://hexdocs.pm/elixir/Config.html)
- [Application module -- official docs](https://hexdocs.pm/elixir/Application.html)
- [Mix.Tasks.Release -- runtime config](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
- [Releases and Runtime Configuration -- Phoenix guide](https://hexdocs.pm/phoenix/releases.html#runtime-configuration)
