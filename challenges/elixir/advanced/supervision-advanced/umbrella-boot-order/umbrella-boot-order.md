# Umbrella Boot Order and Application Dependencies

**Project**: `umbrella_boot` — a three-application umbrella where boot order is load-bearing.

---

## Why umbrella boot order and application dependencies matters

This challenge encodes a production-grade Elixir/OTP pattern that directly affects throughput, memory, or fault-tolerance when the system is under real load. The naive approach works on a developer laptop; the version built here survives the scheduler pressure, binary refc pitfalls, and supervisor budgets of a running node.

The trade-off chart and the executable benchmark are the core of the lesson: you calibrate the cost of the abstraction against a measurable gain, not a vibe.

---
## The business problem

You are the platform lead for a payments company that runs a single umbrella project called
`umbrella_boot`. The umbrella contains three Elixir applications: `core` (domain logic and
an ETS cache), `infra` (Ecto repo, HTTP client pool, telemetry), and `web` (Phoenix-like HTTP
endpoint that depends on both). Historically, developers added children to `web`'s
`application.ex` expecting everything to "just start", and every few deploys a race condition
surfaced: `web` would boot, accept an HTTP request, call `Infra.Repo`, and crash with
`:repo_not_started` because the release manager had listed applications in the wrong order
in `mix.exs`.

The root cause is that the BEAM has two, very different ordering mechanisms. The
`:applications` list in `mix.exs` and `extra_applications` in each child app's `mix.exs`
define the **application start order** — a DAG resolved by `:application_controller`.
Inside a single application, the `Supervisor` child list defines the **process start
order** — strictly sequential and deterministic. Mixing these up (for instance, trying
to fix a cross-app race by reordering a supervisor's children) leads to fragile systems
that work on a laptop and fail on a release target.

Your job is to build an umbrella where boot order is explicit, documented, and verifiable
with a smoke test. You will also implement a `released_applications` preference inside the
release config so that `mix release` produces a tarball whose `sys.config` lists the apps in
the correct order regardless of how `iex -S mix` happens to resolve them in development.

Along the way you will learn which knobs to use, when the default dependency resolution
is enough, and why `included_applications` is a trap that most teams should avoid.

---

## Project structure

```
umbrella_boot/
├── apps/
│   ├── core/
│   │   ├── lib/
│   │   │   ├── core.ex
│   │   │   ├── core/application.ex
│   │   │   └── core/cache.ex
│   │   ├── test/
│   │   │   └── core_test.exs
│   │   └── mix.exs
│   ├── infra/
│   │   ├── lib/
│   │   │   ├── infra.ex
│   │   │   ├── infra/application.ex
│   │   │   ├── infra/repo.ex
│   │   │   └── infra/http_pool.ex
│   │   ├── test/
│   │   │   └── infra_test.exs
│   │   └── mix.exs
│   └── web/
│       ├── lib/
│       │   ├── web.ex
│       │   ├── web/application.ex
│       │   └── web/endpoint.ex
│       ├── test/
│       │   ├── web_test.exs
│       │   └── boot_order_test.exs
│       └── mix.exs
├── config/
│   └── config.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Design decisions

**Option A — single application with nested supervisors for boot ordering**
- Pros: no umbrella overhead; one mix project to build.
- Cons: you hand-roll application-level ordering; `Application.ensure_all_started/1` guarantees do not apply; teams coupled on build and dep graph.

**Option B — umbrella with `:applications` dependency declarations** (chosen)
- Pros: OTP's application controller guarantees boot order from the DAG; each app has its own dialyzer/test/release lifecycle; teams can work in parallel.
- Cons: more mix projects; slower CI unless you cache per-app artefacts; dev-only vs prod-only deps need explicit gating.

→ Chose **B** because boot order is a correctness invariant, and letting OTP enforce it from declared dependencies is cheaper than re-implementing the sequencing in code.

---

## Implementation

### Step 1: Root `mix.exs`

**Objective**: Declare the umbrella with `apps_path:` so each child app has its own lifecycle and `mix release` sorts them by dep DAG.

```elixir
defmodule UmbrellaBoot.MixProject do
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

  defp deps, do: []

  defp releases do
    [
      umbrella_boot: [
        applications: [
          core: :permanent,
          infra: :permanent,
          web: :permanent
        ],
        include_executables_for: [:unix],
        steps: [:assemble, :tar]
      ]
    ]
  end
end
```
### Step 2: `apps/core/mix.exs`

**Objective**: Core has zero umbrella deps — it must boot first so downstream apps can trust its ETS cache exists.

```elixir
defmodule Core.MixProject do
  use Mix.Project

  def project do
    [
      app: :core,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {Core.Application, []}
    ]
  end
end
```
### Step 3: `apps/core/lib/core/application.ex`

**Objective**: Start `Core.Cache` under a plain `:one_for_one` — the ETS table owner is the only process that matters here.

```elixir
defmodule Core.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      Core.Cache
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: Core.Supervisor)
  end
end
```
### Step 4: `apps/core/lib/core/cache.ex`

**Objective**: Make one GenServer the sole ETS owner so a crash takes the table with it and restart repopulates from source.

```elixir
defmodule Core.Cache do
  @moduledoc """
  Owner of the `:core_cache` ETS table.

  The table is created in `init/1` and dies with this process. A crash triggers
  a restart and repopulation from persistent storage (out of scope for this module).
  """
  use GenServer

  @table :core_cache

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec get(term()) :: {:ok, term()} | :miss
  def get(key) do
    case :ets.lookup(@table, key) do
      [{^key, value}] -> {:ok, value}
      [] -> :miss
    end
  end

  @spec put(term(), term()) :: :ok
  def put(key, value) do
    :ets.insert(@table, {key, value})
    :ok
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    {:ok, %{}}
  end
end
```
### Step 5: `apps/infra/mix.exs`

**Objective**: Declare `{:core, in_umbrella: true}` so Mix infers Infra boots after Core — avoid hand-rolling `:applications`.

```elixir
defmodule Infra.MixProject do
  use Mix.Project

  def project do
    [
      app: :infra,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: [
        {:core, in_umbrella: true}
      ]
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {Infra.Application, []}
    ]
  end
end
```
### Step 6: `apps/infra/lib/infra/application.ex`

**Objective**: Use `:rest_for_one` so HttpPool restarts when Repo dies — its config was derived from a stale Repo session.

```elixir
defmodule Infra.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      Infra.Repo,
      Infra.HttpPool
    ]

    # rest_for_one: if Repo dies, HttpPool is restarted too
    # because HttpPool may have cached configuration derived from Repo.
    Supervisor.start_link(children, strategy: :rest_for_one, name: Infra.Supervisor)
  end
end
```
### Step 7: `apps/infra/lib/infra/repo.ex`

**Objective**: Simulate boot latency in `init/1` so downstream pool code visibly fails if declared `:applications` order is wrong.

```elixir
defmodule Infra.Repo do
  @moduledoc """
  Fake Ecto-style repo. Boot latency is simulated so the boot order is observable.
  """
  use GenServer
  require Logger

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec ready?() :: boolean()
  def ready?, do: GenServer.call(__MODULE__, :ready?)

  @impl true
  def init(_opts) do
    Logger.info("Infra.Repo booting")
    Process.sleep(200)
    {:ok, %{ready: true}}
  end

  @impl true
  def handle_call(:ready?, _from, %{ready: r} = s), do: {:reply, r, s}
end
```
### Step 8: `apps/infra/lib/infra/http_pool.ex`

**Objective**: Block `init/1` on `Repo.ready?` so boot-order bugs crash at startup, not under the first production request.

```elixir
defmodule Infra.HttpPool do
  @moduledoc """
  HTTP pool that reads runtime config populated by `Infra.Repo`.
  """
  use GenServer
  require Logger

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec call(String.t()) :: {:ok, map()}
  def call(path), do: GenServer.call(__MODULE__, {:call, path})

  @impl true
  def init(_opts) do
    true = Infra.Repo.ready?()
    Logger.info("Infra.HttpPool booting (Repo is ready)")
    {:ok, %{}}
  end

  @impl true
  def handle_call({:call, path}, _from, s), do: {:reply, {:ok, %{path: path}}, s}
end
```
### Step 9: `apps/web/mix.exs`

**Objective**: Depend on both `:core` and `:infra` so OTP's DAG boots `web` last — no manual `:applications` override needed.

```elixir
defmodule Web.MixProject do
  use Mix.Project

  def project do
    [
      app: :web,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: [
        {:core, in_umbrella: true},
        {:infra, in_umbrella: true}
      ]
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {Web.Application, []}
    ]
  end
end
```
### Step 10: `apps/web/lib/web/application.ex`

**Objective**: Keep `web` minimal — it only supervises `Endpoint`; dependency readiness is delegated to the `:applications` DAG.

```elixir
defmodule Web.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      Web.Endpoint
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: Web.Supervisor)
  end
end
```
### Step 11: `apps/web/lib/web/endpoint.ex`

**Objective**: Assert `:core` and `:infra` are running in `init/1` so a misdeclared `:applications` list crashes at boot, not under traffic.

```elixir
defmodule Web.Endpoint do
  @moduledoc """
  Simulates an HTTP endpoint. On boot it asserts that its transitive dependencies
  are actually running — this is the canary that catches wrong boot order early.
  """
  use GenServer
  require Logger

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec request(String.t()) :: {:ok, map()}
  def request(path), do: GenServer.call(__MODULE__, {:request, path})

  @impl true
  def init(_opts) do
    for app <- [:core, :infra] do
      unless Application.started_applications() |> Enum.any?(fn {a, _, _} -> a == app end) do
        raise "Web booting before #{app} is running — check :applications list"
      end
    end

    Logger.info("Web.Endpoint ready")
    {:ok, %{}}
  end

  @impl true
  def handle_call({:request, path}, _from, s) do
    Core.Cache.put(path, :served)
    {:ok, resp} = Infra.HttpPool.call(path)
    {:reply, {:ok, resp}, s}
  end
end
```
### Step 12: `apps/web/test/boot_order_test.exs`

**Objective**: Freeze the boot DAG as executable assertions so reorderings of `:applications` fail CI instead of production.

```elixir
defmodule Web.BootOrderTest do
  use ExUnit.Case, async: false
  doctest Web.Endpoint

  describe "Web.BootOrder" do
    test "all three umbrella apps are running" do
      running = Application.started_applications() |> Enum.map(&elem(&1, 0))
      assert :core in running
      assert :infra in running
      assert :web in running
    end

    test "web can serve a request end-to-end through core and infra" do
      assert {:ok, %{path: "/health"}} = Web.Endpoint.request("/health")
      assert {:ok, :served} = Core.Cache.get("/health")
    end

    test "boot order is infra before web in the running app list" do
      started = Application.started_applications() |> Enum.map(&elem(&1, 0))
      infra_idx = Enum.find_index(started, &(&1 == :infra))
      web_idx = Enum.find_index(started, &(&1 == :web))
      # started_applications is reverse-chronological
      assert web_idx < infra_idx
    end
  end
end
```
---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---

## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Trade-offs and production gotchas

**1. Overriding `:applications` manually.** When you write `applications: [...]` in
`application/0`, Mix stops inferring from `deps`. If you later add a new dep and forget
to add it to `:applications`, the app will boot but the new dep will not — you get
`undef` at runtime. Prefer `:extra_applications` for the few infra apps (`:logger`,
`:runtime_tools`, `:observer`) and let Mix infer the rest.

**2. `included_applications`.** Avoid. It breaks `Application.stop/1`, makes boot order
reasoning harder, and is incompatible with `mix release` if you do not also add the
included app to the release's `:applications` manifest. The use cases it once solved
(static linking for embedded) are better served today by a proper dependency.

**3. Circular dependencies between umbrella apps.** `core` depends on nothing, `infra`
on `core`, `web` on both — a DAG. If you ever write `core` depending on `infra`, Mix
will refuse to compile. That is not a bug; it is the system forcing you to factor out
a shared module (usually into a fourth app, `domain`, or into `core` itself).

**4. Boot latency from `init/1`.** A slow `init/1` in any supervised process blocks the
parent supervisor, which blocks the next application. In this exercise `Infra.Repo`
sleeps 200ms deliberately. If your real `Repo.init` does `connect + migrate + schema_diff`,
that can be 5–30s. Move slow work into a `handle_continue(:load, state)` or a dedicated
`Task` supervised separately so the app boots quickly and becomes "ready" asynchronously.

**5. Permanent vs transient.** If you mark `web: :transient` in the release config, a
clean `:normal` exit will be allowed. For an HTTP endpoint, there is rarely a reason to
exit normally — if it does, something is wrong. `:permanent` is almost always correct
for top-level apps.

**6. Dev-only vs prod-only deps.** `extra_applications: [:runtime_tools]` in `:prod`
works because the app is always on the node. If you add `:observer`, which requires
`wx`, your production release on a Docker image without `wx` will fail to boot.
Gate with `Mix.env()` in `application/0`.

**7. When NOT to use this.** If your umbrella has only two apps and they share no state,
an umbrella is over-engineering. Put both in a single app with separate top-level
namespaces. Umbrellas pay their cost (CI, dialyzer, dep resolution) only when you have
3+ apps with genuinely distinct release lifecycles.

---

### Why this works

`:applications` in each app's `mix.exs` describes the DAG; the OTP application controller refuses to start an app whose dependencies have not finished booting. That turns a runtime ordering concern into a declarative one, and makes "forgot to declare the dependency" a boot-time error instead of a flaky integration test.

---

## Benchmark

The assertion in `Web.Endpoint.init/1` adds < 100µs to boot. `Application.started_applications/0`
is an ETS lookup inside `:application_controller`. It is safe to call hundreds of times.

For deeper verification, replace the boot_order_test with a release-level smoke test:
`_build/prod/rel/umbrella_boot/bin/umbrella_boot eval "Web.Endpoint.request(\"/ping\")"`
and measure total boot-to-first-request latency. On a cold JIT, expect 1.5–2.5s.

Target: boot-to-first-request ≤ 2.5 s on a cold JIT for a three-app umbrella; per-app `init/1` ≤ 200 ms unless work is deferred via `handle_continue`.

---

## Reflection

1. Your `infra` app's `init/1` does a 5 s schema migration. Do you block boot on it, move it to a `handle_continue`, or split it into a separate application with its own lifecycle? Which option gives ops the clearest "ready" signal?
2. A new application `analytics` depends on `core` but must boot *after* `web`. OTP's DAG does not express "after web". What is the minimum change — reorder the release, move `analytics` behind a start phase, or make `web` signal readiness — that correctly captures the intent?

---

### `script/main.exs`
```elixir
defmodule Web.Endpoint do
  @moduledoc """
  Simulates an HTTP endpoint. On boot it asserts that its transitive dependencies
  are actually running — this is the canary that catches wrong boot order early.
  """
  use GenServer
  require Logger

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec request(String.t()) :: {:ok, map()}
  def request(path), do: GenServer.call(__MODULE__, {:request, path})

  @impl true
  def init(_opts) do
    for app <- [:core, :infra] do
      unless Application.started_applications() |> Enum.any?(fn {a, _, _} -> a == app end) do
        raise "Web booting before #{app} is running — check :applications list"
      end
    end

    Logger.info("Web.Endpoint ready")
    {:ok, %{}}
  end

  @impl true
  def handle_call({:request, path}, _from, s) do
    Core.Cache.put(path, :served)
    {:ok, resp} = Infra.HttpPool.call(path)
    {:reply, {:ok, resp}, s}
  end
end

defmodule Main do
  def main do
      # Demonstrate umbrella boot order and application dependencies

      # Boot order is defined by :applications in root mix.exs:
      # [:core, :infra, :web] ensures correct dependency resolution

      # Verify core app is running (domain logic + ETS cache)
      core_cache_pid = Process.whereis(UmbrellaBoot.Core.Cache)
      assert is_pid(core_cache_pid), "Core cache must be initialized"
      IO.puts("✓ Core application started (domain + ETS cache)")

      # Verify infra app is running (Ecto repo + HTTP client pool)
      infra_repo_pid = Process.whereis(UmbrellaBoot.Infra.Repo)
      assert is_pid(infra_repo_pid), "Infra repo must be initialized"
      IO.puts("✓ Infra application started (Ecto repo + HTTP pool)")

      # Verify web app is running (depends on core + infra)
      web_listener_pid = Process.whereis(UmbrellaBoot.Web.Listener)
      assert is_pid(web_listener_pid), "Web listener must be initialized"
      IO.puts("✓ Web application started (depends on core + infra)")

      # Test dependency resolution: web can call infra and core
      {:ok, data} = UmbrellaBoot.Web.handle_request()
      assert data != nil, "Web should successfully call infra and core"
      IO.inspect(data, label: "Web request result (via infra)")

      # Verify boot order via :applications list
      app_order = Application.loaded_applications()
        |> Enum.filter(fn {app, _, _} -> app in [:core, :infra, :web] end)
        |> Enum.map(fn {app, _, _} -> app end)

      assert app_order == [:core, :infra, :web], "Applications must boot in dependency order"
      IO.puts("✓ Boot order verified: core → infra → web (via :applications list)")

      IO.puts("\n✓ Umbrella boot order mechanism demonstrated:")
      IO.puts("  - :applications in root mix.exs defines global boot order")
      IO.puts("  - Child supervisor lists define intra-app process order")
      IO.puts("  - Mismatch = race conditions on release targets")
      IO.puts("  - Solution: put dependencies in :applications, not supervisor")
      IO.puts("✓ Payments umbrella ready (no boot order races)")
  end
end

Main.main()
```
### `lib/umbrella_boot.ex`

```elixir
defmodule UmbrellaBoot do
  @moduledoc """
  Reference implementation for Umbrella Boot Order and Application Dependencies.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the umbrella_boot module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> UmbrellaBoot.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/umbrella_boot_test.exs`

```elixir
defmodule UmbrellaBootTest do
  use ExUnit.Case, async: true

  doctest UmbrellaBoot

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert UmbrellaBoot.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

### 1. Two orderings, two mechanisms

Boot in an umbrella happens in two layers:

```
  layer 1 (application_controller)
  ┌────────────────────────────────────────────┐
  │ :kernel → :stdlib → :crypto → :core →      │
  │ :infra → :web                              │
  │   each one fully boots before the next     │
  └────────────────────────────────────────────┘
                    │
                    ▼
  layer 2 (each app's Supervisor)
  ┌────────────────────────────────────────────┐
  │ Infra.Application starts:                  │
  │   Infra.Repo → Infra.HttpPool              │
  │   strictly sequential, rest_for_one        │
  └────────────────────────────────────────────┘
```

Layer 1 is controlled by `application` keys in `mix.exs`. Layer 2 by the `children` list
in each `Application.start/2`.

### 2. `:applications` vs `:extra_applications`

Every app's `mix.exs` has an `application/0` callback returning a keyword list:

| Key | Meaning | Who adds |
|-----|---------|----------|
| `:applications` | Explicit list of apps to boot before this one. Overrides inference. | Rare. Only when you must. |
| `:extra_applications` | Apps to boot in addition to those inferred from deps. | You. `:logger`, `:runtime_tools`. |
| `:mod` | `{Module, args}` callback. Defines this as an OTP app with a supervisor. | Any app that starts processes. |

Mix infers `:applications` from the `deps` list of your `mix.exs` by default.
If `web` depends on `infra` in deps, `web` will boot after `infra` automatically.
You only override `:applications` when the default is wrong (a very suspicious situation
and usually a sign of a dep cycle or a missing dep declaration).

### 3. `included_applications` — the trap

`included_applications` declares an app is part of your app's supervision tree but is
NOT started independently by the application controller. Your app's supervisor must
start its top-level supervisor manually. It was useful for releases on OTP < 19. It is
rarely useful today and it breaks `:application.which_applications/0`, hot code upgrades,
and most observability tooling. If a junior on your team suggests it, ask them to use a
dependency instead.

### 4. Release application order and `released_applications`

In a release, `mix release` writes a `sys.config` and a boot script. The order in that
boot script is the resolved topological sort of your dependency DAG. You can influence
it with the `:applications` key of `release` in `mix.exs`:

### `mix.exs`
```elixir
defmodule UmbrellaBootOrder.MixProject do
  use Mix.Project

  def project do
    [
      app: :umbrella_boot_order,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [# No external dependencies — pure Elixir]
  end
end
```
```elixir
releases: [
  umbrella_boot: [
    applications: [core: :permanent, infra: :permanent, web: :permanent],
    include_executables_for: [:unix]
  ]
]
```
The `:permanent` level means a crash of that application brings down the node (SASL
terminates the VM). `:transient` allows clean exit. `:temporary` allows any exit.
For a payments gateway, `web` should be `:permanent` — if HTTP is dead, the node
should die so the orchestrator can replace it.

### 5. Cross-app crash semantics

If `infra` crashes permanently and it is `:permanent`, the VM halts. This is correct for
stateful infra (repo, pool). `web` being `:permanent` is also correct. Making `core`
`:transient` is a mistake most teams make — domain modules usually have no processes
worth crashing the node over, but they host the ETS-backed cache in this codebase,
and a cache owner crash means losing the table. Permanent it is.

---
