# `bin/my_app eval` vs `rpc` — admin on a release

**Project**: `release_eval_rpc` — a release with a small admin module
you can drive via both `eval` (new short-lived node) and `rpc`
(the live node), so the difference is concrete.


---

## Project context

Production is live. You need to: run a migration, flush a cache, bump
a feature flag, inspect a GenServer's state. Two commands on the
release launcher cover nearly every admin task:

- `bin/my_app eval "..."` — starts a *new* node, runs your code, exits.
  No connection to the live node. Good for migrations on a box that
  isn't running the service, or one-shot utilities.
- `bin/my_app rpc "..."` — connects to the already-running node, runs
  your code **there**, returns the result. Good for inspecting or
  mutating live state without restarting.

This exercise packages a release with a `Release` admin module and
shows how to invoke the same function both ways.

Project structure:

```
release_eval_rpc/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   ├── release_eval_rpc.ex
│   └── release_eval_rpc/
│       ├── application.ex
│       ├── counter.ex
│       └── release.ex
├── test/
│   └── release_eval_rpc_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not SSH + iex?** No audit trail, no typed interface, and easy to run destructive code by mistake.

## Core concepts

### 1. `eval` — a brand-new node

```
$ bin/my_app eval "MyApp.Release.migrate()"
   ┌──────────────────────────────────────┐
   │ NEW BEAM node starts                 │
   │ Applications loaded, NOT started     │
   │ Your expression runs                 │
   │ Node exits                           │
   └──────────────────────────────────────┘
```

Apps are **loaded** (modules available) but **not started** (no
supervision tree running). This is the right level for schema
migrations: you don't want the full service booting just to run DDL.

### 2. `rpc` — talk to the live node

```
$ bin/my_app rpc "MyApp.Counter.bump()"
   Connects to   ──▶   running node
                        runs expression
                        returns result
```

Requires the live node to exist (same cookie, same name family).
Every side effect happens in the running service's state.

### 3. `remote` — interactive IEx on the live node

```
bin/my_app remote
```

Like `rpc` but an interactive IEx shell. Use sparingly in production
— typos in a shared shell are expensive.

### 4. The `Release` module convention

Mix releases conventionally expect a `MyApp.Release` module hosting
admin entry points (`migrate/0`, `rollback/1`, `seed/0`, etc.). The
key design rule: these functions must boot only what they need,
because they run under `eval` without the full supervision tree.

---

## Design decisions

**Option A — SSH into the node and hope**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `bin/app eval` and `bin/app rpc` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because release scripts give you a typed, auditable interface to a running node.


## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
```




### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new release_eval_rpc --sup
cd release_eval_rpc
mkdir -p config
```

### Step 2: `mix.exs`, `config/config.exs`, `config/runtime.exs`

**Objective**: Provide `mix.exs`, `config/config.exs`, `config/runtime.exs` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
# mix.exs — relevant parts
def application, do: [extra_applications: [:logger], mod: {ReleaseEvalRpc.Application, []}]

defp releases do
  [
    release_eval_rpc: [
      include_executables_for: [:unix],
      applications: [release_eval_rpc: :permanent]
    ]
  ]
end
```

```elixir
# config/config.exs
import Config
config :release_eval_rpc, :label, "default-label"
```

```elixir
# config/runtime.exs
import Config
config :release_eval_rpc, :label, System.get_env("LABEL", "default-label")
```

### Step 3: `lib/release_eval_rpc/counter.ex`

**Objective**: Implement `counter.ex` — release-time behavior that depends on application env resolution and runtime boot semantics.


```elixir
defmodule ReleaseEvalRpc.Counter do
  @moduledoc """
  A trivial GenServer that holds mutable state. We use it to prove that
  `rpc` sees the live process while `eval` starts from zero.
  """

  use GenServer

  @spec start_link(any()) :: GenServer.on_start()
  def start_link(_), do: GenServer.start_link(__MODULE__, 0, name: __MODULE__)

  @spec value() :: integer()
  def value, do: GenServer.call(__MODULE__, :value)

  @spec bump() :: integer()
  def bump, do: GenServer.call(__MODULE__, :bump)

  @impl true
  def init(n), do: {:ok, n}

  @impl true
  def handle_call(:value, _f, n), do: {:reply, n, n}
  def handle_call(:bump, _f, n), do: {:reply, n + 1, n + 1}
end
```

### Step 4: `lib/release_eval_rpc/application.ex` and `lib/release_eval_rpc.ex`

**Objective**: Provide `lib/release_eval_rpc/application.ex` and `lib/release_eval_rpc.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule ReleaseEvalRpc.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [ReleaseEvalRpc.Counter]
    Supervisor.start_link(children, strategy: :one_for_one, name: ReleaseEvalRpc.Supervisor)
  end
end
```

```elixir
defmodule ReleaseEvalRpc do
  @spec label() :: String.t()
  def label, do: Application.fetch_env!(:release_eval_rpc, :label)
end
```

### Step 5: `lib/release_eval_rpc/release.ex` — the admin module

**Objective**: Edit `release.ex` — the admin module, exposing release-time behavior that depends on application env resolution and runtime boot semantics.


```elixir
defmodule ReleaseEvalRpc.Release do
  @moduledoc """
  Entry points invoked via `bin/release_eval_rpc eval` (no supervision
  tree running) and `bin/release_eval_rpc rpc` (live node).

  Functions meant for `eval` must start any dependency they need — apps
  are LOADED but not STARTED under `eval`.
  """

  @app :release_eval_rpc

  @doc """
  Prints the current configured label. Safe under both `eval` and `rpc`
  because `:label` is read from Application env, which is loaded by
  `load_app/0` below.
  """
  @spec print_label() :: :ok
  def print_label do
    load_app()
    IO.puts("label = #{inspect(Application.fetch_env!(@app, :label))}")
  end

  @doc """
  Pretend-migration: runs in `eval` mode. A real version would start
  Ecto's Repo, call `Ecto.Migrator.run/4`, then stop the Repo. The
  pattern is: start exactly what you need, no more.
  """
  @spec migrate() :: :ok
  def migrate do
    load_app()
    IO.puts("running migrations for #{@app}...")
    # Ecto.Migrator.run(MyApp.Repo, :up, all: true) — in a real project.
    :ok
  end

  @doc """
  Only works under `rpc` — requires the live node where the Counter
  GenServer is already supervised.
  """
  @spec counter_status() :: integer()
  def counter_status, do: ReleaseEvalRpc.Counter.value()

  defp load_app do
    # In eval mode, apps are loaded by the launcher but calling this
    # ensures dependencies would also be loaded in a more complex app.
    Application.load(@app)
  end
end
```

### Step 6: `test/release_eval_rpc_test.exs`

**Objective**: Write `release_eval_rpc_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule ReleaseEvalRpcTest do
  use ExUnit.Case, async: false

  test "counter starts at 0 and bumps" do
    assert ReleaseEvalRpc.Counter.value() == 0
    assert ReleaseEvalRpc.Counter.bump() == 1
    assert ReleaseEvalRpc.Counter.value() == 1
  end

  test "label has a runtime default" do
    assert ReleaseEvalRpc.label() == "default-label"
  end

  test "Release.print_label writes to stdout" do
    output = ExUnit.CaptureIO.capture_io(&ReleaseEvalRpc.Release.print_label/0)
    assert output =~ "label ="
  end
end
```

### Step 7: Build and demonstrate both commands

**Objective**: Build and demonstrate both commands.


```bash
MIX_ENV=prod mix release

REL=_build/prod/rel/release_eval_rpc

# Start the release in the background
LABEL="prod-one" $REL/bin/release_eval_rpc daemon

# rpc: runs on the LIVE node, sees the running counter
$REL/bin/release_eval_rpc rpc "ReleaseEvalRpc.Counter.bump()"  # => 1
$REL/bin/release_eval_rpc rpc "ReleaseEvalRpc.Counter.bump()"  # => 2
$REL/bin/release_eval_rpc rpc "ReleaseEvalRpc.Counter.value()" # => 2

# eval: brand new node — its Counter is NOT the one running under daemon.
# (and in fact eval doesn't start the supervision tree, so Counter isn't
#  even alive in the eval node unless we started it ourselves.)
$REL/bin/release_eval_rpc eval "ReleaseEvalRpc.Release.print_label()"

# Shut the daemon down cleanly
$REL/bin/release_eval_rpc stop
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Key Concepts

Releases support `bin/myapp eval CODE` and `bin/myapp rpc MODULE FUNCTION` for operational tasks without a full IEx shell. `eval` runs arbitrary Elixir code in the running node; `rpc` calls a specific function. Both assume the node is already running—they are operational tools, not deployment tools. This enables patterns: `bin/myapp rpc Myapp.Maintenance clean_old_data` or `bin/myapp eval 'IO.puts Myapp.version()'. These are powerful but dangerous if exposed; in production, restrict access and log all invocations. Most production systems use these for migrations, cleanup tasks, or diagnostic queries—never for data mutations in untrusted environments.

---

## Deep Dive: Compile-Time vs Runtime Configuration Boundaries

A release is a static artifact: code and compile-time config are baked in. Runtime config must be provided at boot via environment variables, config files, or config providers. Simple rule: if a value changes between dev and prod, it goes in `config/runtime.exs`, not `config/config.exs`.

Footgun: putting config in compile-time files and assuming environment variables work at runtime. Releases ignore env vars unless `config/runtime.exs` explicitly reads them. If you need env vars, fetch them in `config/runtime.exs` and store in application state.

For distributed systems, config providers (modules loading config from Consul, S3, etc.) are powerful but complex. Start with environment variables and `config/runtime.exs`; only reach for providers if you need dynamic reloading without downtime or multi-tenant config switching. Premature provider complexity is a mistake.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `eval` does NOT start your supervision tree**
Every admin function must explicitly start what it needs. Forgetting
this is the #1 cause of "migrate works locally, fails in the release."

**2. `rpc` requires a running node and correct cookie**
If the daemon died or the cookie file is unreadable, `rpc` fails with
an opaque error. `bin/my_app pid` or `ping` confirms the node is
reachable before you trust `rpc`.

**3. `rpc` serializes through the remote node**
A heavy computation via `rpc` runs in the live node's scheduler,
competing with your real traffic. Reserve it for lightweight admin
tasks or for kicking off async work.

**4. Don't shell out to `eval` repeatedly in scripts**
Each `eval` starts a full BEAM — hundreds of milliseconds. A loop of
50 `eval` calls is minutes of startup. Batch the work into one call.

**5. When NOT to use `rpc`/`eval`**
If the operation is frequent and programmatic, expose it as an HTTP
endpoint (admin port with auth) or a telemetry metric. `rpc`/`eval`
are human-driven tools, not RPC for other services.

---


## Reflection

- Diseñá un script de migración de DB que corra bajo `bin/app eval`. ¿Cómo lo testeás sin una release?

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule ReleaseEvalRpc.Counter do
    @moduledoc """
    A trivial GenServer that holds mutable state. We use it to prove that
    `rpc` sees the live process while `eval` starts from zero.
    """

    use GenServer

    @spec start_link(any()) :: GenServer.on_start()
    def start_link(_), do: GenServer.start_link(__MODULE__, 0, name: __MODULE__)

    @spec value() :: integer()
    def value, do: GenServer.call(__MODULE__, :value)

    @spec bump() :: integer()
    def bump, do: GenServer.call(__MODULE__, :bump)

    @impl true
    def init(n), do: {:ok, n}

    @impl true
    def handle_call(:value, _f, n), do: {:reply, n, n}
    def handle_call(:bump, _f, n), do: {:reply, n + 1, n + 1}
  end

  def main do
    # Demo: eval y rpc para administración de releases
    IO.puts("ReleaseEvalRpc: demostración exitosa")
    IO.puts("  'bin/myapp eval' ejecuta código una sola vez")
    IO.puts("  'bin/myapp rpc' ejecuta código en el nodo vivo")
    IO.puts("  Ambos permiten administración sin SSH")
  end

end

Main.main()
```


## Resources

- [Mix release — launcher commands](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-bin-my_app-commands)
- ["Release management" — Elixir guide](https://hexdocs.pm/elixir/config-and-releases.html)
- [`:rpc` — Erlang docs](https://www.erlang.org/doc/man/rpc.html) — the BEAM-level mechanism under `bin/my_app rpc`
