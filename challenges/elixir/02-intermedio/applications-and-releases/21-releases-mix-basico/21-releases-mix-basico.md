# `mix release` — a first self-contained release

**Project**: `mini_release` — a tiny app packaged as a Mix release so you
can inspect the resulting directory, run the `bin/mini_release` launcher,
and understand what actually ships to production.


---

## Project context

Your team has been deploying with `mix phx.server` on the target machine.
It works until it doesn't: you're dragging `mix`, Hex, the full source,
and the entire build toolchain into production. Releases fix this by
bundling your compiled code, the Erlang runtime (ERTS), and every
dependency into a single tarball you can copy and run — with zero build
tools on the host.

This exercise produces a minimal release and walks its directory so you
can see the moving parts: the launcher, ERTS, the release payload, and
the runtime config file.

Project structure (source):

```
mini_release/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   ├── mini_release.ex
│   └── mini_release/
│       └── application.ex
├── test/
│   └── mini_release_test.exs
└── mix.exs
```

Release output layout (after `mix release`):

```
_build/prod/rel/mini_release/
├── bin/
│   └── mini_release            # launcher script — start/stop/remote/eval/rpc
├── erts-<ver>/                 # the Erlang runtime; no system Erlang required
├── lib/                        # your app + deps, compiled
│   └── mini_release-0.1.0/
├── releases/
│   └── 0.1.0/
│       ├── env.sh              # shell env wiring
│       ├── runtime.exs         # runtime config, read on every boot
│       ├── sys.config          # compile-time config, baked in
│       └── vm.args             # BEAM flags (node name, cookie, limits)
└── ...
```

---


## Why X and not Y

- **Why not `mix run` in prod?** Ships Mix, ships your dev deps, requires Elixir installed, no bootscripts, no RPC — the opposite of what you want in prod.

## Core concepts

### 1. `mix release` vs `mix run`

`mix run` assumes Mix, deps as source, and the build toolchain exist on
the host. `mix release` produces a **single directory** with ERTS, your
compiled beams, your deps' compiled beams, and a launcher — no Mix, no
source, no Hex. Copy the tree, run `bin/<app> start`, done.

### 2. `releases/<ver>/` — the three config files

| File           | Evaluated when     | Source                        |
|----------------|--------------------|-------------------------------|
| `sys.config`   | Compile / assembly | `config/config.exs` + env     |
| `runtime.exs` | Every boot         | `config/runtime.exs`           |
| `vm.args`      | Every boot         | release assembly defaults      |

`sys.config` is a flat Erlang term file; `runtime.exs` is live Elixir
code run before apps start. Together they define the boot environment.

### 3. The launcher commands

```
bin/mini_release start     # boot in foreground
bin/mini_release daemon    # boot detached
bin/mini_release stop      # graceful stop
bin/mini_release remote    # attach a remote IEx shell
bin/mini_release eval "..." # run code in a short-lived node
bin/mini_release rpc  "..." # run code in the live node
```

`remote`, `eval`, and `rpc` are operations gold — covered in exercise 70.

### 4. `:releases` in `mix.exs`

A single `releases:` stanza declares the release name, applications
included, and options like `include_executables_for:` (cross-platform
launcher scripts).

---

## Design decisions

**Option A — Mix in production**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `mix release` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because releases include ERTS and exclude Mix, giving reproducible, self-contained deploys.


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
mix new mini_release --sup
cd mini_release
mkdir -p config
```

### Step 2: `mix.exs`

```elixir
defmodule MiniRelease.MixProject do
  use Mix.Project

  def project do
    [
      app: :mini_release,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: [],
      releases: releases()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {MiniRelease.Application, []}
    ]
  end

  # Release definition. `include_executables_for: [:unix]` skips Windows
  # scripts; add `:windows` if you ship there. `applications:` is inferred
  # automatically — listed here only when you want to override :permanent.
  defp releases do
    [
      mini_release: [
        include_executables_for: [:unix],
        applications: [mini_release: :permanent]
      ]
    ]
  end
end
```

### Step 3: `config/config.exs` and `config/runtime.exs`

```elixir
# config/config.exs
import Config

config :mini_release, greeting: "hello from sys.config"
```

```elixir
# config/runtime.exs
import Config

# Read at boot on the target machine. Defaults keep `mix` workflows happy.
config :mini_release,
  greeting: System.get_env("GREETING", "hello from runtime.exs")
```

### Step 4: `lib/mini_release/application.ex` and `lib/mini_release.ex`

```elixir
defmodule MiniRelease.Application do
  @moduledoc false
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    Logger.info("mini_release booting; greeting=#{MiniRelease.greeting()}")
    Supervisor.start_link([], strategy: :one_for_one, name: MiniRelease.Supervisor)
  end
end
```

```elixir
defmodule MiniRelease do
  @moduledoc """
  Tiny public surface so we can exercise the release via `bin/mini_release eval`.
  """

  @spec greeting() :: String.t()
  def greeting, do: Application.fetch_env!(:mini_release, :greeting)

  @spec shout() :: String.t()
  def shout, do: String.upcase(greeting())
end
```

### Step 5: `test/mini_release_test.exs`

```elixir
defmodule MiniReleaseTest do
  use ExUnit.Case, async: false

  test "greeting has a runtime default" do
    assert MiniRelease.greeting() =~ "hello"
  end

  test "shout/0 uppercases the greeting" do
    assert MiniRelease.shout() == String.upcase(MiniRelease.greeting())
  end
end
```

### Step 6: Build and run the release

```bash
MIX_ENV=prod mix release

# Inspect the layout
ls _build/prod/rel/mini_release

# Run it in the foreground (Ctrl+C twice to stop)
GREETING="hello production" _build/prod/rel/mini_release/bin/mini_release start

# Or run a one-off expression against the compiled release
_build/prod/rel/mini_release/bin/mini_release eval "IO.puts MiniRelease.shout()"
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

```elixir
{us, _} = :timer.tc(fn -> hot_path() end)
IO.puts("#{us} µs")
```

Target esperado: medición relevante según el hot path del módulo.

## Trade-offs and production gotchas

**1. ERTS is platform-specific**
A release built on macOS won't run on Linux. Always build on (or for) the
deployment target — typically a Linux container matching your base image.
For cross-OS dev, see `include_erts: false` to reuse the host Erlang.

**2. `MIX_ENV=prod` matters**
Releases default to `prod`. If you build in `dev`, you'll ship unoptimized
beams and pull in dev-only deps. Always set `MIX_ENV=prod` explicitly in CI.

**3. Secrets do not belong in `sys.config`**
Anything in `config.exs` ends up in plaintext inside the release tarball.
Secrets belong in env vars read by `runtime.exs` — or in a config provider.

**4. `start_permanent: true` means the node halts if your app stops**
In prod, that's what you want — a crashed top supervisor should bring the
node down so the orchestrator restarts it. In dev/test you'd rather stay
alive, which is why the flag is gated on `Mix.env()`.

**5. When NOT to use `mix release`**
Pure libraries never ship as releases — they ship to Hex. Scripts you run
on your dev machine are fine with `mix run`. Releases are for deployed
services.

---


## Reflection

- Tu equipo quiere deployar con `mix run` en producción. Dá 3 razones concretas para migrar a releases.

## Resources

- [`mix release` task](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
- [`Mix.Release` module](https://hexdocs.pm/mix/Mix.Release.html)
- [Config and releases guide](https://hexdocs.pm/elixir/config-and-releases.html)
- [Why releases? — Dashbit blog](https://dashbit.co/blog/mix-releases-is-here)
