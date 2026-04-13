# Umbrella application with interdependent apps and consolidated release

**Project**: `umbrella_services` — three OTP apps under one umbrella, deployed as one release.

---

## Project context

Your monolith grew to 150 k LOC in a single app. Build times are 90 s for `mix compile` even with incremental compilation. Tests take 4 min. Teams step on each other's toes in `config/config.exs`. You decide to split by bounded context. Two options:

1. **Poncho** — sibling `mix` projects that reference each other as path deps. Full isolation; each has its own `deps`, `config`, `application.ex`. Can be extracted to a separate repo later with zero diffs.
2. **Umbrella** — a parent `mix.exs` that coordinates child apps in `apps/`. Shared `deps`, shared `config`, single `mix test` runs all child suites. Single release.

For internal services shipped as one VM on one node, an umbrella wins: you save the per-app boilerplate, compilation is cached across apps, and the release packages all three as one artifact. For libraries meant to be extracted, poncho is better.

This exercise builds a 3-app umbrella:

- `catalog` — product data (GenServer-backed, mock DB).
- `pricing` — depends on catalog, computes price quotes.
- `checkout` — depends on pricing and catalog, top-level API.

The catch: when the release boots, `catalog` must be running before `pricing` tries to call it, otherwise `pricing`'s `GenServer.call/2` crashes with `:noproc`. Umbrella start order is defined by the `applications` list in each app's `mix.exs`, NOT by apps/ directory order.

```
umbrella_services/
├── mix.exs                          # umbrella root
├── config/
│   └── config.exs                   # shared config
├── apps/
│   ├── catalog/
│   │   ├── mix.exs
│   │   └── lib/
│   │       └── catalog/
│   │           ├── application.ex
│   │           └── repo.ex
│   ├── pricing/
│   │   ├── mix.exs
│   │   └── lib/
│   │       └── pricing/
│   │           ├── application.ex
│   │           └── calculator.ex
│   └── checkout/
│       ├── mix.exs
│       └── lib/
│           └── checkout/
│               ├── application.ex
│               └── api.ex
└── rel/
    └── overlays/
        └── releases.exs
```

---

## Core concepts

### 1. Umbrella vs poncho vs mono-repo

| Dimension | Monolith | Umbrella | Poncho | Mono-repo (separate releases) |
|---|---|---|---|---|
| `mix.exs` files | 1 | 1 root + N children | N siblings | N + coordination |
| Shared `deps` | n/a | yes | no | no |
| Shared `config` | yes | yes (inherit + override) | no | no |
| Release artifacts | 1 | 1 | 1 per project | N |
| Compilation unit | 1 | N (cached) | N | N |
| Can extract one app | painful | easy (move to own repo) | trivial | already separate |
| Start order control | manual | `applications:` list | manual orchestration | n/a |

### 2. Application start order

Umbrella children are listed in the umbrella's `deps:` via `in_umbrella: true`. At release time, the tool looks at each app's `application` spec and its `applications:` list, does a topological sort, and boots in dependency order.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
# apps/pricing/mix.exs
def application do
  [mod: {Pricing.Application, []},
   extra_applications: [:logger, :catalog]]   # <- catalog before pricing
end
```

Without `:catalog` in `extra_applications`, pricing might boot first and crash on first call. The umbrella does NOT infer this from `deps:`.

### 3. Shared config with per-app overrides

```elixir
# config/config.exs
import Config

# Shared
config :logger, level: :info

# Per-app
config :catalog, :repo_url, "postgres://..."
config :pricing, :margin_pct, 15
config :checkout, :idempotency_ttl, 300_000

# Environment-specific
import_config "#{config_env()}.exs"
```

Each app reads its own config via `Application.fetch_env!(:catalog, :repo_url)`. Do NOT write `Application.fetch_env!(:my_app, ...)` where `:my_app` is wrong — you'll get `nil`s silently.

### 4. Cross-app calls

Apps call each other via public module APIs:

```elixir
# In checkout:
price = Pricing.Calculator.quote(sku, qty)
product = Catalog.Repo.get(sku)
```

Do NOT reach into `GenServer.call(:some_pid, ...)` across app boundaries. The app's API module is its public contract; the GenServer is an implementation detail.

### 5. One release, multiple apps

```elixir
# mix.exs (root)
def project do
  [apps_path: "apps",
   version: "0.1.0",
   releases: [
     umbrella_services: [
       applications: [
         catalog:  :permanent,
         pricing:  :permanent,
         checkout: :permanent
       ]
     ]
   ]]
end
```

`mix release` produces ONE artifact under `_build/prod/rel/umbrella_services/` containing BEAM files for all three apps plus ERTS. Deploy that single tarball.

---

## Why umbrella and not poncho

Poncho gives stricter isolation — each app has its own `deps`, config, and release. That wins when one of the apps will genuinely be extracted to its own repo on its own deploy cadence. In this system, the three apps ship together on one VM, share the same TLS certs, and have never been deployed separately. Paying poncho's boilerplate tax for isolation that is never exercised is pure cost. Umbrella's shared `deps`, shared `config`, and single release match the deployment shape; the per-app `mix.exs` still enforces module-level boundaries.

---

## Design decisions

**Option A — single monolithic app with internal contexts**
- Pros: zero boilerplate; one `mix.exs`; trivial `iex -S mix`.
- Cons: nothing enforces the context boundary; config conflicts; 150 k LOC recompiles together; teams collide on a single `application.ex`.

**Option B — umbrella with three child apps** (chosen)
- Pros: enforced app-level boundaries (you can't accidentally depend on a transitive module); per-app test suites; cached per-app compilation; `applications:` list gives declarative start order; one release artifact.
- Cons: directory overhead; release config is per-umbrella; extracting an app later requires mechanical (but real) work.

→ Chose **B** because the pain described (90 s compile, shared config conflicts, team coupling) is exactly what umbrella's per-app boundaries solve, and the deploy shape stays single-artifact.

---

## Implementation

### Dependencies (umbrella root `mix.exs`)

The root project declares no runtime deps — children declare their own. Each child's `mix.exs` lists its `applications:` to control start order.

### Step 1: Root `mix.exs`

**Objective**: Implement Root mix.exs.

```elixir
# mix.exs
defmodule UmbrellaServices.MixProject do
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
      umbrella_services: [
        applications: [
          catalog: :permanent,
          pricing: :permanent,
          checkout: :permanent
        ],
        steps: [:assemble, :tar]
      ]
    ]
  end
end
```

### Step 2: `catalog` app

**Objective**: Implement catalog app.

```elixir
# apps/catalog/mix.exs
defmodule Catalog.MixProject do
  use Mix.Project

  def project do
    [
      app: :catalog,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.15",
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {Catalog.Application, []}]
  end
end
```

```elixir
# apps/catalog/lib/catalog/application.ex
defmodule Catalog.Application do
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([Catalog.Repo], strategy: :one_for_one, name: Catalog.Supervisor)
  end
end

# apps/catalog/lib/catalog/repo.ex
defmodule Catalog.Repo do
  use GenServer

  @type sku :: String.t()
  @type product :: %{sku: sku(), name: String.t(), base_price: pos_integer()}

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec get(sku()) :: {:ok, product()} | {:error, :not_found}
  def get(sku), do: GenServer.call(__MODULE__, {:get, sku})

  @impl true
  def init(:ok) do
    products = %{
      "SKU-A" => %{sku: "SKU-A", name: "Widget", base_price: 1_000},
      "SKU-B" => %{sku: "SKU-B", name: "Gadget", base_price: 2_500}
    }

    {:ok, products}
  end

  @impl true
  def handle_call({:get, sku}, _from, state) do
    case Map.fetch(state, sku) do
      {:ok, p} -> {:reply, {:ok, p}, state}
      :error -> {:reply, {:error, :not_found}, state}
    end
  end
end
```

### Step 3: `pricing` app (depends on catalog)

**Objective**: Implement pricing app (depends on catalog).

```elixir
# apps/pricing/mix.exs
defmodule Pricing.MixProject do
  use Mix.Project

  def project do
    [
      app: :pricing,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.15",
      deps: [{:catalog, in_umbrella: true}]
    ]
  end

  def application do
    # :catalog MUST be listed so OTP starts it before us.
    [extra_applications: [:logger, :catalog], mod: {Pricing.Application, []}]
  end
end
```

```elixir
# apps/pricing/lib/pricing/application.ex
defmodule Pricing.Application do
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: Pricing.Supervisor)
  end
end

# apps/pricing/lib/pricing/calculator.ex
defmodule Pricing.Calculator do
  @spec quote(Catalog.Repo.sku(), pos_integer()) :: {:ok, pos_integer()} | {:error, term()}
  def quote(sku, qty) when qty > 0 do
    case Catalog.Repo.get(sku) do
      {:ok, %{base_price: base}} ->
        margin_pct = Application.fetch_env!(:pricing, :margin_pct)
        total = base * qty * (100 + margin_pct) |> div(100)
        {:ok, total}

      {:error, _} = err ->
        err
    end
  end
end
```

### Step 4: `checkout` app

**Objective**: Implement checkout app.

```elixir
# apps/checkout/mix.exs
defmodule Checkout.MixProject do
  use Mix.Project

  def project do
    [
      app: :checkout,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.15",
      deps: [{:catalog, in_umbrella: true}, {:pricing, in_umbrella: true}]
    ]
  end

  def application do
    [extra_applications: [:logger, :catalog, :pricing], mod: {Checkout.Application, []}]
  end
end
```

```elixir
# apps/checkout/lib/checkout/application.ex
defmodule Checkout.Application do
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([Checkout.Api], strategy: :one_for_one, name: Checkout.Supervisor)
  end
end

# apps/checkout/lib/checkout/api.ex
defmodule Checkout.Api do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec checkout(String.t(), pos_integer()) :: {:ok, map()} | {:error, term()}
  def checkout(sku, qty), do: GenServer.call(__MODULE__, {:checkout, sku, qty})

  @impl true
  def init(:ok), do: {:ok, %{orders: 0}}

  @impl true
  def handle_call({:checkout, sku, qty}, _from, state) do
    with {:ok, product} <- Catalog.Repo.get(sku),
         {:ok, total} <- Pricing.Calculator.quote(sku, qty) do
      order = %{id: state.orders + 1, sku: sku, name: product.name, qty: qty, total: total}
      {:reply, {:ok, order}, %{state | orders: state.orders + 1}}
    else
      {:error, _} = err -> {:reply, err, state}
    end
  end
end
```

### Step 5: Shared config

**Objective**: Implement Shared config.

```elixir
# config/config.exs
import Config

config :pricing, :margin_pct, 15

config :logger, :default_formatter,
  format: "$time $metadata[$level] $message\n",
  metadata: [:application]
```

### Step 6: Tests per app

**Objective**: Write tests for per app.

```elixir
# apps/pricing/test/pricing/calculator_test.exs
defmodule Pricing.CalculatorTest do
  use ExUnit.Case, async: true

  describe "Pricing.Calculator" do
    test "quote uses catalog base price + margin" do
      assert {:ok, total} = Pricing.Calculator.quote("SKU-A", 2)
      # base 1000 * qty 2 * 115 / 100 = 2300
      assert total == 2_300
    end

    test "returns catalog error" do
      assert {:error, :not_found} = Pricing.Calculator.quote("NOPE", 1)
    end
  end
end

# apps/checkout/test/checkout/api_test.exs
defmodule Checkout.ApiTest do
  use ExUnit.Case, async: false

  describe "Checkout.Api" do
    test "end-to-end checkout across 3 apps" do
      assert {:ok, order} = Checkout.Api.checkout("SKU-B", 3)
      assert order.sku == "SKU-B"
      assert order.name == "Gadget"
      # 2500 * 3 * 115 / 100 = 8625
      assert order.total == 8_625
    end

    test "propagates not_found from catalog" do
      assert {:error, :not_found} = Checkout.Api.checkout("MISSING", 1)
    end
  end
end
```

Run from the root:

```bash
mix test
# Runs all apps' tests. Use `mix test apps/pricing` to scope.
```

### Why this works

Each child's `application.ex` declares its `applications:` list; this is the single source of truth OTP uses to compute a topological sort of app starts. `catalog` declares only `[:logger]`, so it starts first. `pricing` declares `[:logger, :catalog]`, forcing `catalog`'s supervisor to be fully up before `pricing.init/1` runs. `checkout` declares both. By the time a cross-app `GenServer.call/2` happens in production, every upstream supervisor has completed its start callback — no `:noproc` races possible.

### Step 7: Building the release

**Objective**: Implement Building the release.

```bash
MIX_ENV=prod mix release umbrella_services
_build/prod/rel/umbrella_services/bin/umbrella_services start
```

Boot log (abridged):

```
[info] Application catalog started
[info] Application pricing started
[info] Application checkout started
```

Order is guaranteed by `extra_applications`. If `pricing` starts before `catalog`, that's your `mix.exs` wrong — fix it.

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

**1. Umbrella start order is controlled by `extra_applications`, not directory order.** A common bug: `pricing` lists `{:catalog, in_umbrella: true}` in `deps:` but forgets `:catalog` in `extra_applications:`. Compile succeeds. Boot succeeds. First call crashes with `:noproc`. Always list the app in BOTH.

**2. Shared `config/config.exs` means shared compile-time state.** A compile-time config in `:catalog` is visible to `:pricing` during its compilation. This couples them. Prefer runtime config (`config/runtime.exs`) for anything that might differ.

**3. `mix test` runs ALL apps sequentially.** Test isolation breaks if two apps share names via `Process.whereis`. Use `async: false` and explicit start/stop in setup, or scope to one app (`mix test apps/foo`).

**4. Hot code upgrades across umbrella apps are a minefield.** A release upgrade that changes two apps simultaneously can boot in any order for the NEW code, potentially calling an interface that doesn't exist yet. Avoid relups for umbrellas; prefer full restarts.

**5. Circular deps are compile errors.** If `pricing` depends on `catalog` and `catalog` depends on `pricing`, `mix compile` fails. This is GOOD — it forces you to extract shared types into a third app (`shared_types` is a common pattern).

**6. `in_umbrella: true` path is resolved at compile time.** If you later extract `:catalog` to its own repo, you replace `{:catalog, in_umbrella: true}` with `{:catalog, "~> 1.0"}`. Your source stays unchanged.

**7. Each app has its OWN supervision tree.** The release starts them in order, each app's `start/2` returns a supervisor pid. There is NO "umbrella root supervisor" — if an app's supervisor dies 3 times, only that app restarts (default `restart: :permanent` on the app itself would crash the VM).

**8. When NOT to use this.** If your 3 apps are deployed to different hosts with different scaling profiles, an umbrella forces them into one VM artifact. You want separate mix projects + separate releases. Umbrella is for "one VM, multiple contexts".

---

## Benchmark

Umbrella compile time scales near-linearly with app count if apps compile in parallel (Elixir 1.14+ does this automatically for independent apps). The shared `_build` means stdlib modules compile once, not N times.

Release size for this toy umbrella: ~35 MB (most of which is ERTS). Adding apps adds only their BEAM files. The real cost is `deps/` if each app has distinct deps — prefer to hoist common deps to the umbrella root.

Target: umbrella `mix compile` cold ≤ 30 s for a 3-app tree of ~5 k LOC each; incremental re-compile on a single-app change ≤ 2 s; release artifact ≤ 50 MB.

---

## Reflection

1. One of the three apps (`checkout`) starts needing its own scaling profile — you want to run 10 instances of it behind a load balancer while `catalog` and `pricing` remain single-node. Do you extract `checkout` to its own mix project, keep it in the umbrella and deploy the whole VM 10 times, or split into two releases from the same umbrella? What does each cost in ops and code duplication?
2. A new dev accidentally imports `Catalog.Repo` directly from `checkout`, bypassing the public API boundary. `mix compile` succeeds because they're in the same umbrella. How would you enforce boundaries at compile time without extracting the apps to separate repos?

---

## Resources

- [Dashbit — Mix and umbrella projects](https://elixir-lang.org/getting-started/mix-otp/dependencies-and-umbrella-projects.html) — the official intro.
- [José Valim — Umbrella projects, poncho, mono-repos](https://dashbit.co/blog/are-umbrella-apps-dead-in-elixir) — when to pick which.
- [`mix release` — hexdocs](https://hexdocs.pm/mix/Mix.Tasks.Release.html) — release assembly, boot order, runtime config.
- [Phoenix umbrella generator source](https://github.com/phoenixframework/phoenix/blob/main/lib/mix/tasks/phx.new.ex) — read how Phoenix structures an umbrella (`phx.new --umbrella`).
- [Plataformatec — Why our umbrella project became...](https://blog.plataformatec.com.br/) — historical motivation for umbrella vs mono-repo choices.
- [OTP Design Principles: Applications](https://www.erlang.org/doc/design_principles/applications.html) — the underlying `application:start/2` semantics.
