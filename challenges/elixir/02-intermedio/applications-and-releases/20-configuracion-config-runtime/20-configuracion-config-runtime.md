# `config/config.exs` vs `config/runtime.exs`

**Project**: `runtime_config_demo` — an app that reads the same key from
compile-time config and runtime config so you can see exactly when each
one is evaluated.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You're about to ship your app as a release and realized a painful fact:
`config/config.exs` is evaluated at **compile time**, baked into the
build, and won't read your production environment variables. To read
`System.get_env/1` where `DATABASE_URL` actually lives — on the machine —
you need `config/runtime.exs`, which runs after the release boots and
*before* any application is started.

This exercise wires both files and prints the values at boot so the
difference becomes visible.

Project structure:

```
runtime_config_demo/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   ├── runtime_config_demo.ex
│   └── runtime_config_demo/
│       └── application.ex
├── test/
│   └── runtime_config_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `config/config.exs` is compile-time

```
mix compile ──▶ evaluates config.exs ──▶ values embedded in .beam files
```

Anything read with `Application.compile_env/2` locks the value at build
time. Perfect for feature flags that never change per environment. Wrong
for secrets or URLs that differ per deployment.

### 2. `config/runtime.exs` is runtime

```
release boot ──▶ evaluates runtime.exs ──▶ before Application.start/2 ──▶ apps start
```

It runs on the target machine, sees the actual environment, and can call
`System.fetch_env!/1`. This is **the** place to wire secrets, endpoints,
pool sizes. It applies to both `mix` and releases — in Mix it runs at
`mix run` / `iex -S mix` boot.

### 3. `Application.get_env/2` vs `Application.compile_env/2`

```elixir
# Late-bound: reads the value every call. Changes to env at runtime take effect.
Application.get_env(:my_app, :endpoint)

# Early-bound: value is inlined at compile time. Runtime changes are ignored,
# and the compiler warns if the config is later rewritten by runtime.exs.
Application.compile_env(:my_app, :endpoint)
```

Use `compile_env` when the value must be fixed in the build (it enables
compiler optimizations and catches misconfig early). Use `get_env` for
anything that should follow the environment.

### 4. File evaluation order

```
config/config.exs
  └── import_config "#{config_env()}.exs"   (e.g. dev.exs / test.exs / prod.exs)
config/runtime.exs                           (always runs, regardless of env)
```

`runtime.exs` always wins for any key it sets — it's the last writer.

---

## Implementation

### Step 1: Create the project

```bash
mix new runtime_config_demo --sup
cd runtime_config_demo
mkdir -p config
```

### Step 2: `config/config.exs`

```elixir
import Config

# Compile-time defaults. These get baked into the build. Good for
# values that legitimately differ per build (dev vs prod behavior),
# not for secrets.
config :runtime_config_demo,
  mode: :compile_time_default,
  feature_flags: %{new_ui: false}
```

### Step 3: `config/runtime.exs`

```elixir
import Config

# Runs at release boot (and at `mix run` boot). Free to touch
# System.get_env/1 — the environment is real here.
#
# System.get_env/2 with a default keeps `mix test` happy when the var
# isn't set; in prod you'd use System.fetch_env!/1 to fail loudly.
config :runtime_config_demo,
  endpoint: System.get_env("SERVICE_ENDPOINT", "http://localhost:4000"),
  mode: :runtime_override
```

### Step 4: `lib/runtime_config_demo/application.ex`

```elixir
defmodule RuntimeConfigDemo.Application do
  @moduledoc false
  use Application
  require Logger

  # `compile_env/2` is evaluated at COMPILE TIME and embedded here.
  # Even if runtime.exs later rewrites :mode, THIS value is frozen.
  @compiled_mode Application.compile_env(:runtime_config_demo, :mode)

  @impl true
  def start(_type, _args) do
    # `get_env/2` is evaluated at CALL TIME — sees whatever runtime.exs set.
    runtime_mode = Application.get_env(:runtime_config_demo, :mode)
    endpoint = Application.get_env(:runtime_config_demo, :endpoint)

    Logger.info("compile_env :mode = #{inspect(@compiled_mode)}")
    Logger.info("get_env     :mode = #{inspect(runtime_mode)}")
    Logger.info("get_env :endpoint = #{inspect(endpoint)}")

    Supervisor.start_link([], strategy: :one_for_one, name: RuntimeConfigDemo.Supervisor)
  end
end
```

### Step 5: `lib/runtime_config_demo.ex`

```elixir
defmodule RuntimeConfigDemo do
  @moduledoc """
  Public surface for inspecting the three values side by side.
  """

  @compiled_mode Application.compile_env(:runtime_config_demo, :mode)

  @spec compiled_mode() :: atom()
  def compiled_mode, do: @compiled_mode

  @spec runtime_mode() :: atom()
  def runtime_mode, do: Application.get_env(:runtime_config_demo, :mode)

  @spec endpoint() :: String.t()
  def endpoint, do: Application.fetch_env!(:runtime_config_demo, :endpoint)
end
```

### Step 6: `test/runtime_config_demo_test.exs`

```elixir
defmodule RuntimeConfigDemoTest do
  use ExUnit.Case, async: false

  test "compiled_mode is frozen to the config.exs value" do
    # config.exs set :mode to :compile_time_default. runtime.exs overrode it
    # to :runtime_override, but compile_env captured the earlier value.
    assert RuntimeConfigDemo.compiled_mode() == :compile_time_default
  end

  test "runtime_mode reflects the runtime.exs override" do
    assert RuntimeConfigDemo.runtime_mode() == :runtime_override
  end

  test "endpoint has a default when SERVICE_ENDPOINT is unset" do
    assert RuntimeConfigDemo.endpoint() == "http://localhost:4000"
  end

  test "get_env sees dynamic changes; compile_env does not" do
    Application.put_env(:runtime_config_demo, :mode, :runtime_changed)
    assert RuntimeConfigDemo.runtime_mode() == :runtime_changed
    assert RuntimeConfigDemo.compiled_mode() == :compile_time_default
  after
    Application.put_env(:runtime_config_demo, :mode, :runtime_override)
  end
end
```

### Step 7: Run and experiment

```bash
mix test
SERVICE_ENDPOINT=https://api.prod iex -S mix
# Observe the log lines: :endpoint reflects the env var, :mode reflects runtime.exs.
```

---

## Trade-offs and production gotchas

**1. `compile_env` will warn — loudly — if you later change the value at runtime**
That warning is intentional: it means the runtime change is silently
ignored because the compiled value is frozen. Either move the read to
`get_env`, or remove the runtime override.

**2. `config/prod.exs` ≠ `config/runtime.exs`**
`config/prod.exs` runs at **compile time** when `MIX_ENV=prod`. Putting
`System.fetch_env!/1` there reads the *builder's* env, not the target's.
For releases, always put env-dependent config in `runtime.exs`.

**3. `fetch_env!/2` > `get_env/3` for required values**
`fetch_env!` crashes with a clear "config key missing" error at boot.
`get_env` with a silent default can hide misconfig until hours later
when a request reveals it.

**4. Don't read config in hot paths repeatedly**
`Application.get_env/2` is ETS-backed and cheap, but not free. If you
read the same value per request, cache it at boot in a GenServer state
or module attribute.

**5. When NOT to use `runtime.exs`**
Pure libraries should avoid any config at all — accept it as arguments
to `start_link/1`. `runtime.exs` is for the application's own settings,
not for dependencies that should be configured by their host.

---

## Resources

- [`Config` — Elixir stdlib](https://hexdocs.pm/elixir/Config.html)
- [`Application.compile_env/2`](https://hexdocs.pm/elixir/Application.html#compile_env/2)
- [Mix releases — Runtime configuration](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-runtime-configuration)
- [Configuration and releases guide](https://hexdocs.pm/elixir/config-and-releases.html)
