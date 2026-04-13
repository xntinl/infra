# `config/config.exs` vs `config/runtime.exs`

**Project**: `runtime_config_demo` — an app that reads the same key from
compile-time config and runtime config so you can see exactly when each
one is evaluated.


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


## Why X and not Y

- **Why not `config.exs` for everything?** Compile-time config bakes values into artifacts — fatal for releases that ship across envs.

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

## Design decisions

**Option A — `config/config.exs` (compile-time)**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `config/runtime.exs` for env-dependent values (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because runtime config is required for releases; compile-time config bakes secrets into artifacts.


## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```




### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new runtime_config_demo --sup
cd runtime_config_demo
mkdir -p config
```

### Step 2: `config/config.exs`

**Objective**: Implement `config.exs` — release-time behavior that depends on application env resolution and runtime boot semantics.


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

**Objective**: Implement `runtime.exs` — release-time behavior that depends on application env resolution and runtime boot semantics.


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

**Objective**: Wire `application.ex` to start the OTP application callback so BEAM starts/stops the supervision tree through the proper application controller lifecycle.


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

**Objective**: Implement `runtime_config_demo.ex` — release-time behavior that depends on application env resolution and runtime boot semantics.


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

**Objective**: Write `runtime_config_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


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

**Objective**: Run and experiment.


```bash
mix test
SERVICE_ENDPOINT=https://api.prod iex -S mix
# Observe the log lines: :endpoint reflects the env var, :mode reflects runtime.exs.
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Key Concepts

OTP applications use configuration with different timing semantics. Compile-time config (in `config/` files, evaluated when `mix compile` runs) bakes values into the release beam files—immutable and fast. Runtime config (read at startup via `config/runtime.exs` or config providers) comes from environment variables, files on disk, or API calls during boot. The key distinction: compile-time values are optimal for static settings (library options, feature flags); runtime values are essential for deployment-specific values (database URLs, API keys, port numbers). Using the wrong type creates hard-to-debug production bugs. If your DB URL is compile-time, you cannot deploy to different envs without recompiling. Releases are immutable artifacts; configuration must be injected at startup.

---

## Deep Dive: Compile-Time vs Runtime Configuration Boundaries

A release is a static artifact: code and compile-time config are baked in. Runtime config must be provided at boot via environment variables, config files, or config providers. Simple rule: if a value changes between dev and prod, it goes in `config/runtime.exs`, not `config/config.exs`.

Footgun: putting config in compile-time files and assuming environment variables work at runtime. Releases ignore env vars unless `config/runtime.exs` explicitly reads them. If you need env vars, fetch them in `config/runtime.exs` and store in application state.

For distributed systems, config providers (modules loading config from Consul, S3, etc.) are powerful but complex. Start with environment variables and `config/runtime.exs`; only reach for providers if you need dynamic reloading without downtime or multi-tenant config switching. Premature provider complexity is a mistake.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

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


## Reflection

- Listá 3 valores que DEBEN estar en `runtime.exs` y 3 que DEBEN estar en `config.exs`. Justificá cada uno.

## Resources

- [`Config` — Elixir stdlib](https://hexdocs.pm/elixir/Config.html)
- [`Application.compile_env/2`](https://hexdocs.pm/elixir/Application.html#compile_env/2)
- [Mix releases — Runtime configuration](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-runtime-configuration)
- [Configuration and releases guide](https://hexdocs.pm/elixir/config-and-releases.html)
