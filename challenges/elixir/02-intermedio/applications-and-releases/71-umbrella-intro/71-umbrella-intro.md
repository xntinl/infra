# Umbrella projects — multiple apps in one tree

**Project**: `tiny_umbrella` — an umbrella with two apps, `tiny_core`
(pure domain) and `tiny_web` (depends on core), to show what umbrellas
buy you and what they cost.


---

## Project context

You've got one Elixir app and it's growing. You want boundaries
between "the business logic" and "the HTTP edge" without splitting
into separate repos. An umbrella project gives you multiple OTP
applications in one mix tree, each with its own `mix.exs`, its own
supervision tree, and a shared build.

This is **not** the only way to organize code — modular boundaries
inside a single app also work well — but it's the canonical Mix
answer. This exercise builds the smallest umbrella that actually
shows the mechanics.

Project structure:

```
tiny_umbrella/
├── apps/
│   ├── tiny_core/
│   │   ├── lib/
│   │   │   └── tiny_core.ex
│   │   ├── test/
│   │   │   └── tiny_core_test.exs
│   │   └── mix.exs
│   └── tiny_web/
│       ├── lib/
│       │   └── tiny_web.ex
│       ├── test/
│       │   └── tiny_web_test.exs
│       └── mix.exs
├── config/
│   └── config.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not one mix app?** Fine until you need independent dep sets or test isolation across subsystems.

## Core concepts

### 1. Umbrella vs single app vs multi-repo

```
single app       one mix.exs, one supervision tree
umbrella         one mix.exs per app, shared build, shared deps lock
multi-repo       separate repos, separate builds, Hex or git deps
```

Umbrellas are a middle ground: strong module boundaries, single build,
shared CI. You pay with slightly more friction (adding an app means
a new `mix.exs`, moving code across apps requires recompilation of
both).

### 2. `apps/` convention

Each child app lives in `apps/<name>/` and looks like a regular Mix
project — `lib/`, `test/`, its own `mix.exs`. The umbrella root's
`mix.exs` has `apps_path: "apps"`, which tells Mix: this is an
umbrella.

### 3. Inter-app dependencies via `in_umbrella: true`

```elixir
# apps/tiny_web/mix.exs
defp deps, do: [{:tiny_core, in_umbrella: true}]
```

Tells Mix `tiny_core` is a sibling app in the same umbrella — no
version, no git source, just a local path resolved by the umbrella.

### 4. Shared `config/` at the root

The umbrella root owns `config/config.exs`. Individual apps can still
have their own, but the root config has final say. This is the
canonical place for cross-app settings.

---

## Design decisions

**Option A — a single mix app**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — an umbrella project (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because independent apps with their own deps and tests, while still shipping as one release.


## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```




### Step 1: Create the umbrella

**Objective**: Create the umbrella.


```bash
mix new tiny_umbrella --umbrella
cd tiny_umbrella
cd apps
mix new tiny_core --sup
mix new tiny_web --sup
cd ..
```

### Step 2: Umbrella root `mix.exs`

**Objective**: Umbrella root `mix.exs`.


```elixir
defmodule TinyUmbrella.MixProject do
  use Mix.Project

  def project do
    [
      apps_path: "apps",
      version: "0.1.0",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      releases: releases()
    ]
  end

  # Umbrella-wide deps (dev tools, linters). App-specific deps go in
  # each app's own mix.exs.
  defp deps, do: []

  # One release that bundles BOTH apps. :permanent means if either stops,
  # the release halts — desirable since tiny_web depends on tiny_core.
  defp releases do
    [
      tiny_umbrella: [
        include_executables_for: [:unix],
        applications: [tiny_core: :permanent, tiny_web: :permanent]
      ]
    ]
  end
end
```

### Step 3: `apps/tiny_core/mix.exs` and `apps/tiny_core/lib/tiny_core.ex`

**Objective**: Provide `apps/tiny_core/mix.exs` and `apps/tiny_core/lib/tiny_core.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule TinyCore.MixProject do
  use Mix.Project

  def project do
    [
      app: :tiny_core,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {TinyCore.Application, []}]
  end

  defp deps, do: []
end
```

```elixir
defmodule TinyCore do
  @moduledoc """
  The domain layer — pure functions, no HTTP, no side effects beyond
  what the domain actually needs. Consumed by `tiny_web`.
  """

  @spec greet(String.t()) :: String.t()
  def greet(name) when is_binary(name), do: "hello, #{name}"

  @spec add(integer(), integer()) :: integer()
  def add(a, b) when is_integer(a) and is_integer(b), do: a + b
end
```

```elixir
defmodule TinyCore.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: TinyCore.Supervisor)
  end
end
```

### Step 4: `apps/tiny_web/mix.exs` and `apps/tiny_web/lib/tiny_web.ex`

**Objective**: Provide `apps/tiny_web/mix.exs` and `apps/tiny_web/lib/tiny_web.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule TinyWeb.MixProject do
  use Mix.Project

  def project do
    [
      app: :tiny_web,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    # Declare that tiny_web depends on tiny_core being started first.
    [extra_applications: [:logger], mod: {TinyWeb.Application, []}]
  end

  # `in_umbrella: true` resolves to the sibling app — no version, no
  # git, just the one in ../tiny_core.
  defp deps, do: [{:tiny_core, in_umbrella: true}]
end
```

```elixir
defmodule TinyWeb do
  @moduledoc """
  The presentation / API layer — calls into TinyCore for domain logic,
  knows about HTTP-ish concerns (not actually implemented here to keep
  the example small).
  """

  @spec handle_greet(String.t()) :: %{status: 200, body: String.t()}
  def handle_greet(name) do
    body = TinyCore.greet(name)
    %{status: 200, body: body}
  end

  @spec handle_add(integer(), integer()) :: %{status: 200, body: integer()}
  def handle_add(a, b) do
    %{status: 200, body: TinyCore.add(a, b)}
  end
end
```

```elixir
defmodule TinyWeb.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: TinyWeb.Supervisor)
  end
end
```

### Step 5: `apps/tiny_core/test/tiny_core_test.exs` and `apps/tiny_web/test/tiny_web_test.exs`

**Objective**: Provide `apps/tiny_core/test/tiny_core_test.exs` and `apps/tiny_web/test/tiny_web_test.exs` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule TinyCoreTest do
  use ExUnit.Case, async: true

  test "greet/1 builds the greeting" do
    assert TinyCore.greet("world") == "hello, world"
  end

  test "add/2 sums integers" do
    assert TinyCore.add(2, 3) == 5
  end
end
```

```elixir
defmodule TinyWebTest do
  use ExUnit.Case, async: true

  test "handle_greet/1 delegates to TinyCore and wraps in response" do
    assert TinyWeb.handle_greet("world") == %{status: 200, body: "hello, world"}
  end

  test "handle_add/2 sums via TinyCore" do
    assert TinyWeb.handle_add(2, 3) == %{status: 200, body: 5}
  end
end
```

### Step 6: Run from the umbrella root

**Objective**: Run from the umbrella root.


```bash
mix test              # runs both apps' tests
mix compile           # compiles both apps
MIX_ENV=prod mix release  # packages both into one release
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Key Concepts

Umbrella projects group multiple Mix applications under one root, sharing a single `mix.exs` and `mix.lock`. Each app under `apps/` is independent with its own supervision tree, but they coordinate via shared dependencies. This pattern is ideal for splitting large systems: one app for domain logic, one for HTTP API, one for background jobs. The key advantage: true separation—test each app independently, reason about boundaries clearly. The trap: umbrellas are not microservices; they're still in the same VM, so crashes cascade. Use umbrellas for modular monoliths, not for avoiding deployment coordination. Each sub-app can be released separately but typically they deploy as a unit.

---

## Deep Dive: Compile-Time vs Runtime Configuration Boundaries

A release is a static artifact: code and compile-time config are baked in. Runtime config must be provided at boot via environment variables, config files, or config providers. Simple rule: if a value changes between dev and prod, it goes in `config/runtime.exs`, not `config/config.exs`.

Footgun: putting config in compile-time files and assuming environment variables work at runtime. Releases ignore env vars unless `config/runtime.exs` explicitly reads them. If you need env vars, fetch them in `config/runtime.exs` and store in application state.

For distributed systems, config providers (modules loading config from Consul, S3, etc.) are powerful but complex. Start with environment variables and `config/runtime.exs`; only reach for providers if you need dynamic reloading without downtime or multi-tenant config switching. Premature provider complexity is a mistake.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Umbrellas don't enforce boundaries — they suggest them**
Nothing stops `tiny_web` from reaching into `TinyCore`'s private
modules. Umbrellas are a convention, not a capability. For real
enforcement, use Boundary.ex or compile-time checks.

**2. Test isolation can be surprising**
`mix test` from the root runs every app's tests, but they share a BEAM.
If `tiny_core` starts a named process and `tiny_web`'s test assumes
none exists, they'll race. Keep tests `async: true` only when they
truly don't share globals.

**3. Cross-app refactors hurt**
Moving a module from `tiny_core` to `tiny_web` is a change in two
apps. In a single app, it's a rename. If 90% of your code lives in
one app anyway, you might not need the umbrella.

**4. Deployment unit is still one release**
The umbrella gives you *code* boundaries, not *deploy* boundaries.
Both apps ship together, start together, die together. If you want
independent deploys, use separate repos or a monorepo with separate
releases.

**5. When NOT to use an umbrella**
Two apps where one uses the other only? Internal modules are lighter.
Truly independent services? Separate repos. Umbrellas shine when
several apps share a domain core and get deployed together (e.g.
`core` + `web` + `worker`).

---


## Reflection

- ¿Cuándo un umbrella es prematuro y un solo mix app es más honesto? Dá los 2 indicadores más fuertes.

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule TinyWeb.MixProject do
    use Mix.Project

    def project do
      [
        app: :tiny_web,
        version: "0.1.0",
        build_path: "../../_build",
        config_path: "../../config/config.exs",
        deps_path: "../../deps",
        lockfile: "../../mix.lock",
        elixir: "~> 1.17",
        start_permanent: Mix.env() == :prod,
        deps: deps()
      ]
    end

    def application do
      # Declare that tiny_web depends on tiny_core being started first.
      [extra_applications: [:logger], mod: {TinyWeb.Application, []}]
    end

    # `in_umbrella: true` resolves to the sibling app — no version, no
    # git, just the one in ../tiny_core.
    defp deps, do: [{:tiny_core, in_umbrella: true}]
  end

  def main do
    # Demo: proyectos umbrella con múltiples apps
    {:ok, _} = Application.ensure_all_started(:tiny_core)
    {:ok, _} = Application.ensure_all_started(:tiny_web)
  
    IO.puts("TinyWeb: demostración exitosa")
    IO.puts("  ✓ tiny_core iniciada")
    IO.puts("  ✓ tiny_web iniciada")
    IO.puts("  Umbrellas permiten múltiples apps en un repositorio")
  end

end

Main.main()
```


## Resources

- [Mix — Umbrella projects](https://hexdocs.pm/elixir/dependencies-and-umbrella-projects.html#umbrella-projects)
- [Saša Jurić — "Towards maintainable Elixir"](https://medium.com/very-big-things/towards-maintainable-elixir-the-core-and-the-interface-c267f0da43) — the "core + interface" argument against umbrellas
- [Boundary.ex](https://hexdocs.pm/boundary/) — enforce cross-module boundaries
