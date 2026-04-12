# `get_env` (runtime) vs `compile_env` (boot-time)

**Project**: `env_vs_compile` — a single module that exposes both reads
side by side so the difference is unambiguous: one value moves with the
environment, the other is frozen in the BEAM files.


---

## Project context

"Why did my runtime override stop working after the release was built?"
is one of the most common deployment bugs in Elixir. The answer is
almost always that someone used `Application.compile_env/2` (or a module
attribute that captured `get_env/2` at compile time) and then expected
`runtime.exs` to change it. It can't — the value is already baked into
the compiled bytecode.

This exercise makes the distinction concrete with a minimal project,
a pair of functions, and a test that proves the asymmetry.

Project structure:

```
env_vs_compile/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   └── env_vs_compile.ex
├── test/
│   └── env_vs_compile_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not always `get_env`?** `compile_env` lets the compiler warn when the config shape breaks — catches deploy-time errors at build time.

## Core concepts

### 1. When each one is evaluated

```
                  compile-time (mix compile)     runtime (release boot / iex -S mix)
                  ───────────────────────────    ──────────────────────────────────
config.exs        evaluated ✓                     already evaluated
runtime.exs       ignored                         evaluated ✓
compile_env/2     value INLINED into .beam        never re-read
get_env/2         not evaluated yet               evaluated on every call
```

### 2. `compile_env/2` tracks changes across builds

The compiler records which `compile_env` keys you read. If a later
config change would alter the value, `mix compile` warns that the
module needs recompilation. This gives you a safety net — but only
across rebuilds, not across deploys.

### 3. `get_env/2` is a pure ETS read at runtime

Cheap, dynamic, and respects `put_env/3`. The right tool for 95% of
application settings.

### 4. Rule of thumb

```
Needs to change per deploy?      ──▶ get_env/2 (or fetch_env!/2)
Used in `case` / pattern match?  ──▶ compile_env/2 (so the compiler optimizes)
Secret / endpoint / pool size?   ──▶ get_env/2, always
Feature flag decided at build?   ──▶ compile_env/2 (and recompile to toggle)
```

---

## Design decisions

**Option A — `Application.get_env` everywhere (runtime)**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `compile_env` for static, `get_env` for runtime (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because `compile_env` lets the compiler warn when the shape of config changes; runtime values must stay runtime.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

```bash
mix new env_vs_compile
cd env_vs_compile
mkdir -p config
```

### Step 2: `config/config.exs` and `config/runtime.exs`

```elixir
# config/config.exs
import Config

config :env_vs_compile,
  build_flavor: :vanilla,
  endpoint: "http://localhost:4000"
```

```elixir
# config/runtime.exs
import Config

# runtime.exs wins for :endpoint — that's by design.
# It ALSO sets :build_flavor, but compile_env will NOT see this change.
config :env_vs_compile,
  build_flavor: :runtime_tried_to_override,
  endpoint: System.get_env("ENDPOINT", "http://localhost:4000")
```

### Step 3: `lib/env_vs_compile.ex`

```elixir
defmodule EnvVsCompile do
  @moduledoc """
  Demonstrates the compile-time vs runtime asymmetry between
  `Application.compile_env/2` and `Application.get_env/2`.
  """

  # Evaluated NOW (at compile time). Frozen into the .beam.
  # If runtime.exs later sets :build_flavor to something else, THIS constant
  # will not notice — you'd need to recompile.
  @build_flavor Application.compile_env(:env_vs_compile, :build_flavor)

  @doc """
  Returns the `:build_flavor` as seen at compile time.
  Always `:vanilla` in this project because that's what `config.exs` set
  when `mix compile` ran.
  """
  @spec compile_time_build_flavor() :: atom()
  def compile_time_build_flavor, do: @build_flavor

  @doc """
  Returns the `:build_flavor` as seen right now.
  Reflects `runtime.exs` and any subsequent `Application.put_env/3`.
  """
  @spec runtime_build_flavor() :: atom()
  def runtime_build_flavor, do: Application.get_env(:env_vs_compile, :build_flavor)

  @doc """
  Endpoint — canonical runtime read. Use this pattern for anything that
  can differ per environment.
  """
  @spec endpoint() :: String.t()
  def endpoint, do: Application.fetch_env!(:env_vs_compile, :endpoint)

  @doc """
  Demonstrates compile-time specialization: the `case` is reduced to a
  constant branch at compile time because the value is known.
  """
  @spec describe_build() :: String.t()
  def describe_build do
    case @build_flavor do
      :vanilla -> "standard build — no feature overrides"
      :enterprise -> "enterprise build — premium features enabled"
      other -> "unknown flavor: #{inspect(other)}"
    end
  end
end
```

### Step 4: `test/env_vs_compile_test.exs`

```elixir
defmodule EnvVsCompileTest do
  use ExUnit.Case, async: false

  test "compile-time flavor is frozen regardless of runtime.exs" do
    # config.exs set :vanilla. runtime.exs tried to override it.
    # compile_env captured :vanilla at `mix compile` time — nothing changes it now.
    assert EnvVsCompile.compile_time_build_flavor() == :vanilla
  end

  test "runtime flavor reflects runtime.exs override" do
    assert EnvVsCompile.runtime_build_flavor() == :runtime_tried_to_override
  end

  test "put_env affects get_env but not compile_env" do
    Application.put_env(:env_vs_compile, :build_flavor, :mutated_at_runtime)
    assert EnvVsCompile.runtime_build_flavor() == :mutated_at_runtime
    assert EnvVsCompile.compile_time_build_flavor() == :vanilla
  after
    Application.put_env(:env_vs_compile, :build_flavor, :runtime_tried_to_override)
  end

  test "describe_build/0 returns the compile-time branch" do
    assert EnvVsCompile.describe_build() =~ "standard build"
  end
end
```

### Step 5: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `compile_env` with `runtime.exs` writes the same key = compiler warning**
If your config.exs sets `:k = 1` and runtime.exs sets `:k = 2`, and you
read `:k` with `compile_env`, the compiler warns that runtime changes
will be ignored. Listen to the warning — pick one side.

**2. Module attributes that read `get_env` are a trap**
`@value Application.get_env(:app, :k)` looks dynamic but is evaluated
exactly once at compile time. If you meant "read on every call", use
a function body, not a module attribute.

**3. `compile_env` enables real optimizations**
Because the value is known at compile time, the compiler can eliminate
dead `case` branches and specialize call sites. If you don't need
runtime changes, `compile_env` is genuinely faster.

**4. Recompile discipline with `compile_env`**
After changing a `compile_env` key in config, you usually need
`mix compile --force` (or let the mix env invalidation handle it) —
touch-only changes to `config.exs` sometimes don't trigger recompile
for transitive dependents.

**5. When NOT to use `compile_env`**
Anything that varies per deploy (secrets, URLs, pool sizes, feature
flags you might toggle without a rebuild) — always `get_env` /
`fetch_env!`.

---


## Reflection

- Si `compile_env` cambia en runtime, ¿qué warning ves y cuándo? Describí el flujo exacto.

## Resources

- [`Application.compile_env/2`](https://hexdocs.pm/elixir/Application.html#compile_env/2)
- [`Application.get_env/3`](https://hexdocs.pm/elixir/Application.html#get_env/3)
- [Configuration and releases guide](https://hexdocs.pm/elixir/config-and-releases.html)
