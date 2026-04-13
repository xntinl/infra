# Reading config at runtime with `Application.fetch_env!/2`

**Project**: `app_env_runtime` — an app that reads its configuration
lazily via `Application.fetch_env!/2` so every value is resolved fresh
from the application environment at the moment of the call.


---

## Project context

Your app needs a handful of settings — an HTTP timeout, an external
endpoint, a pool size — that may differ between local dev and a
production release. You want to **read them at runtime**, not freeze
them at compile time, so that `runtime.exs` (and `Application.put_env/3`
in tests) can override them.

This exercise is a deliberate contrast to compile-time reads: every
getter is a function that calls `Application.fetch_env!/2`, raising
loudly if misconfigured.

Project structure:

```
app_env_runtime/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   ├── app_env_runtime.ex
│   └── app_env_runtime/
│       ├── application.ex
│       └── config.ex
├── test/
│   └── app_env_runtime_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not scattered `System.get_env`?** Unreviewable, untypable, and impossible to override cleanly in tests.

## Core concepts

### 1. `fetch_env!/2` — fail loudly on missing config

```
Application.fetch_env!(:my_app, :endpoint)
# ArgumentError if missing — CLEAR message, early failure
```

Contrast with `get_env/3`, which returns a default and masks misconfig.
For values your app *requires*, `fetch_env!` turns a boot-time misconfig
into an immediate crash instead of a three-hour debug session.

### 2. Centralize reads in one module

Scattering `Application.fetch_env!/2` across the codebase creates three
problems: you can't tell what keys you actually use, you can't apply a
transform consistently, and you can't mock cleanly in tests. A small
`MyApp.Config` module is the canonical pattern.

```
callers ──▶ MyApp.Config.endpoint() ──▶ Application.fetch_env!(:my_app, :endpoint)
                                   └──▶ + validation / normalization
```

### 3. Runtime reads are cheap but not free

`Application.fetch_env!/2` is an ETS lookup — nanoseconds, but measurable
in tight loops. For per-request reads it's fine; for per-message reads
inside a hot GenServer, cache the value in state at `init/1`.

### 4. `put_env/3` in tests — scoped overrides

In tests (non-`async`), `Application.put_env/3` lets you flip a value for
a single test. Because `fetch_env!` always hits ETS, the override takes
effect immediately — no restart required.

---

## Design decisions

**Option A — `System.get_env` scattered across modules**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — centralized `runtime.exs` + `Application.get_env` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because one place to read env, typed config, easy to override in tests.


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
mix new app_env_runtime --sup
cd app_env_runtime
mkdir -p config
```

### Step 2: `config/config.exs` and `config/runtime.exs`

**Objective**: Provide `config/config.exs` and `config/runtime.exs` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
# config/config.exs
import Config

# Sensible defaults for dev/test. Override in runtime.exs or per-env files.
config :app_env_runtime,
  endpoint: "http://localhost:4000",
  http_timeout_ms: 5_000,
  pool_size: 10
```

```elixir
# config/runtime.exs
import Config

# Read whatever the environment provides at boot.
config :app_env_runtime,
  endpoint: System.get_env("APP_ENDPOINT", "http://localhost:4000"),
  http_timeout_ms: String.to_integer(System.get_env("APP_HTTP_TIMEOUT_MS", "5000")),
  pool_size: String.to_integer(System.get_env("APP_POOL_SIZE", "10"))
```

### Step 3: `lib/app_env_runtime/config.ex`

**Objective**: Implement `config.ex` — release-time behavior that depends on application env resolution and runtime boot semantics.


```elixir
defmodule AppEnvRuntime.Config do
  @moduledoc """
  The single entry point for reading configuration.

  Every function is a runtime read — no module attributes, no compile-time
  inlining. That means `runtime.exs` overrides are honored immediately,
  tests can use `Application.put_env/3` per test, and misconfig raises at
  the first call with a clear message.
  """

  @app :app_env_runtime

  @spec endpoint() :: String.t()
  def endpoint, do: Application.fetch_env!(@app, :endpoint)

  @spec http_timeout_ms() :: pos_integer()
  def http_timeout_ms do
    value = Application.fetch_env!(@app, :http_timeout_ms)
    unless is_integer(value) and value > 0 do
      raise ArgumentError, "expected :http_timeout_ms > 0, got #{inspect(value)}"
    end
    value
  end

  @spec pool_size() :: pos_integer()
  def pool_size do
    value = Application.fetch_env!(@app, :pool_size)
    unless is_integer(value) and value > 0 do
      raise ArgumentError, "expected :pool_size > 0, got #{inspect(value)}"
    end
    value
  end

  @doc "Snapshot of the current config. Handy for admin endpoints and logs."
  @spec snapshot() :: %{endpoint: String.t(), http_timeout_ms: pos_integer(), pool_size: pos_integer()}
  def snapshot do
    %{endpoint: endpoint(), http_timeout_ms: http_timeout_ms(), pool_size: pool_size()}
  end
end
```

### Step 4: `lib/app_env_runtime/application.ex`

**Objective**: Wire `application.ex` to start the OTP application callback so BEAM starts/stops the supervision tree through the proper application controller lifecycle.


```elixir
defmodule AppEnvRuntime.Application do
  @moduledoc false
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    # Touching config at boot surfaces misconfig immediately instead of
    # on the first request hours later.
    snap = AppEnvRuntime.Config.snapshot()
    Logger.info("config snapshot: #{inspect(snap)}")

    Supervisor.start_link([], strategy: :one_for_one, name: AppEnvRuntime.Supervisor)
  end
end
```

### Step 5: `test/app_env_runtime_test.exs`

**Objective**: Write `app_env_runtime_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule AppEnvRuntimeTest do
  use ExUnit.Case, async: false

  alias AppEnvRuntime.Config

  test "default values are available" do
    assert Config.endpoint() == "http://localhost:4000"
    assert Config.http_timeout_ms() == 5_000
    assert Config.pool_size() == 10
  end

  test "put_env override is observed immediately" do
    Application.put_env(:app_env_runtime, :endpoint, "http://override.local")
    assert Config.endpoint() == "http://override.local"
  after
    Application.put_env(:app_env_runtime, :endpoint, "http://localhost:4000")
  end

  test "fetch_env! raises when the key is missing" do
    Application.delete_env(:app_env_runtime, :endpoint)
    assert_raise ArgumentError, ~r/could not fetch application environment/, fn ->
      Config.endpoint()
    end
  after
    Application.put_env(:app_env_runtime, :endpoint, "http://localhost:4000")
  end

  test "invalid values fail validation" do
    Application.put_env(:app_env_runtime, :pool_size, 0)
    assert_raise ArgumentError, ~r/expected :pool_size > 0/, fn ->
      Config.pool_size()
    end
  after
    Application.put_env(:app_env_runtime, :pool_size, 10)
  end
end
```

### Step 6: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
APP_ENDPOINT=https://prod.example.com iex -S mix
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Key Concepts

Application environment is OTP's pattern for configuration state. `Application.put_env/3` and `Application.get_env/2` manage a per-application key-value store, readable from any process without passing state. Use app env for configuration values set at boot (read-only after that), never for mutable state. Mutable state belongs in GenServers, ETS, or other process structures. If two processes write conflicting values to app env, the last write wins silently—no transaction or conflict detection. Relying on app env for cache state or session data is a footgun. Using it for immutable configuration (database URL, pool size, feature flags) is idiomatic and efficient—reading app env is a hashtable lookup, not a message send.

---

## Deep Dive: Compile-Time vs Runtime Configuration Boundaries

A release is a static artifact: code and compile-time config are baked in. Runtime config must be provided at boot via environment variables, config files, or config providers. Simple rule: if a value changes between dev and prod, it goes in `config/runtime.exs`, not `config/config.exs`.

Footgun: putting config in compile-time files and assuming environment variables work at runtime. Releases ignore env vars unless `config/runtime.exs` explicitly reads them. If you need env vars, fetch them in `config/runtime.exs` and store in application state.

For distributed systems, config providers (modules loading config from Consul, S3, etc.) are powerful but complex. Start with environment variables and `config/runtime.exs`; only reach for providers if you need dynamic reloading without downtime or multi-tenant config switching. Premature provider complexity is a mistake.

## Benchmark

```elixir
{us, _} = :timer.tc(fn -> hot_path() end)
IO.puts("#{us} µs")
```

Target esperado: medición relevante según el hot path del módulo.

## Trade-offs and production gotchas

**1. `fetch_env!/2` is great for required; use `get_env/3` for optional**
Don't wrap optional feature flags in `fetch_env!` — you'll force a value
into config.exs forever. Reserve `fetch_env!` for values that MUST be set.

**2. Centralize but don't over-wrap**
Don't build a `Config.get/1` that takes keys as atoms — you'll lose the
spec, the validation, and the discoverability. One function per key is
verbose but unambiguous.

**3. Validation belongs at read-time, not init-time**
If validation only runs once at boot, a later `put_env/3` can poison the
state. Validate on every read (it's cheap), or if that's too much,
validate at a single source of truth call like `snapshot/0`.

**4. Avoid reading config in a supervisor's child list**
Supervisor child specs are built once at boot. If a value must update
without restart, the child needs to read it dynamically in its own
callback, not capture it at spec-build time.

**5. When NOT to use `Application.fetch_env!`**
Library code should accept config as arguments to `start_link/1`, not
reach into `Application.env`. Apps own their env; libraries are guests.

---


## Reflection

- ¿Cuándo `Application.get_env` se vuelve un anti-pattern y pasás a pasar config explícitamente por arg? Definí el límite.

## Resources

- [`Application.fetch_env!/2`](https://hexdocs.pm/elixir/Application.html#fetch_env!/2)
- [`Application.get_env/3`](https://hexdocs.pm/elixir/Application.html#get_env/3)
- [Library guidelines — config](https://hexdocs.pm/elixir/library-guidelines.html#avoid-application-configuration)
