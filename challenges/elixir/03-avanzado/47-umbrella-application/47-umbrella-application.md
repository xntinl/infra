# Umbrella Application with Domain Separation

## Overview

Restructure an API gateway codebase into an umbrella application with three sub-applications
and enforced unidirectional dependencies. The core domain logic, the HTTP API layer, and the
background workers are separated into independent apps that compile with enforced boundaries.

Project structure:

```
api_gateway_umbrella/
├── apps/
│   ├── gateway_core/               # domain + Ecto + business logic
│   │   ├── lib/gateway_core/
│   │   │   ├── clients/
│   │   │   ├── repo.ex
│   │   │   └── release.ex
│   │   └── test/
│   ├── gateway_api/                # Phoenix HTTP API -- depends on core
│   │   ├── lib/gateway_api_web/
│   │   │   ├── router.ex
│   │   │   ├── controllers/
│   │   │   └── plugs/
│   │   └── test/
│   └── gateway_workers/            # Oban workers -- depends on core
│       ├── lib/gateway_workers/
│       │   └── workers/
│       └── test/
├── config/
│   ├── config.exs
│   ├── dev.exs
│   ├── test.exs
│   └── runtime.exs
└── mix.exs                         # umbrella root
```

---

## The business problem

Two teams contribute to the gateway: the platform team (routing, auth, rate limiting)
and the analytics team (audit logs, usage reports). Their code keeps colliding. The project
needs:

1. Enforced compilation boundaries -- `gateway_api` cannot import from `gateway_workers`
2. A single shared database configuration without duplication
3. One `mix test` from the root that runs all three test suites in dependency order
4. One `mix release` that produces a single deployable artifact

---

## Why umbrella and not microservices

Microservices communicate over the network -- every call serializes, deserializes, and
traverses TCP. For components that call each other hundreds of times per request, this
is unacceptable latency. Umbrella apps share the same BEAM VM: a call from `gateway_api`
to `gateway_core` is a local function call with no serialization overhead.

The boundary enforcement is architectural (dependency declarations in `mix.exs`), not
physical. If you later need independent scaling, you can split the umbrella into separate
releases or repositories with minimal changes.

---

## Dependency graph -- unidirectional

```
gateway_api     ──depends on──> gateway_core
gateway_workers ──depends on──> gateway_core
                                      ^
                  (NO reverse dependency -- core never imports from api or workers)
```

`mix deps.tree` must show no cycles. Violating this rule (core importing from api) is a
compilation error, not just a lint warning.

---

## Implementation

### Step 1: Create the umbrella

```bash
mix new api_gateway_umbrella --umbrella
cd api_gateway_umbrella

cd apps
mix new gateway_core --sup
mix phx.new gateway_api --no-ecto --no-assets --no-mailer
mix new gateway_workers --sup
cd ..
```

### Step 2: Umbrella `mix.exs`

```elixir
# api_gateway_umbrella/mix.exs
defmodule ApiGatewayUmbrella.MixProject do
  use Mix.Project

  def project do
    [
      apps_path: "apps",
      version: "0.1.0",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      aliases: aliases()
    ]
  end

  defp deps, do: []

  defp aliases do
    [
      setup:       ["deps.get", "ecto.setup"],
      "ecto.setup": ["ecto.create", "ecto.migrate"],
      test:        ["ecto.create --quiet", "ecto.migrate --quiet", "test"]
    ]
  end
end
```

### Step 3: `gateway_core/mix.exs`

```elixir
defmodule GatewayCore.MixProject do
  use Mix.Project

  def project do
    [
      app: :gateway_core,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:postgrex, ">= 0.0.0"},
      {:jason, "~> 1.4"},
      {:oban, "~> 2.17"}
    ]
  end
end
```

### Step 4: `gateway_api/mix.exs` -- depends on core

```elixir
defmodule GatewayApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :gateway_api,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:phoenix_live_view, "~> 0.20"},
      {:gateway_core, in_umbrella: true}
    ]
  end
end
```

### Step 5: `gateway_workers/mix.exs` -- depends on core

```elixir
defmodule GatewayWorkers.MixProject do
  use Mix.Project

  def project do
    [
      app: :gateway_workers,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  defp deps do
    [
      {:gateway_core, in_umbrella: true}
    ]
  end
end
```

### Step 6: Shared config

```elixir
# config/config.exs
import Config

config :gateway_core, GatewayCore.Repo,
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  database: "api_gateway_#{config_env()}"

config :gateway_core,
  ecto_repos: [GatewayCore.Repo]

config :gateway_api, GatewayApiWeb.Endpoint,
  http: [ip: {127, 0, 0, 1}, port: 4000],
  secret_key_base: "dev-secret-replace-in-prod-min-64-chars-long-abcdefgh"

config :gateway_workers, Oban,
  repo: GatewayCore.Repo,
  queues: [notifications: 10, audit: 50, reports: 2]
```

```elixir
# config/runtime.exs
import Config

if config_env() == :prod do
  config :gateway_core, GatewayCore.Repo,
    url: System.fetch_env!("DATABASE_URL"),
    pool_size: System.get_env("POOL_SIZE", "10") |> String.to_integer()

  config :gateway_api, GatewayApiWeb.Endpoint,
    secret_key_base: System.fetch_env!("SECRET_KEY_BASE"),
    server: true
end
```

### Step 7: Domain -- `gateway_core`

```elixir
# apps/gateway_core/lib/gateway_core/clients/client.ex
defmodule GatewayCore.Clients.Client do
  use Ecto.Schema
  import Ecto.Changeset

  schema "clients" do
    field :name,       :string
    field :api_key,    :string
    field :plan,       Ecto.Enum, values: [:free, :pro, :enterprise], default: :free
    field :active,     :boolean, default: true
    timestamps()
  end

  def changeset(client, attrs) do
    client
    |> cast(attrs, [:name, :api_key, :plan])
    |> validate_required([:name, :api_key])
    |> unique_constraint(:api_key)
  end
end
```

```elixir
# apps/gateway_core/lib/gateway_core/clients.ex
defmodule GatewayCore.Clients do
  alias GatewayCore.{Repo, Clients.Client}

  @spec get_by_api_key(String.t()) :: Client.t() | nil
  def get_by_api_key(key), do: Repo.get_by(Client, api_key: key, active: true)

  @spec create(map()) :: {:ok, Client.t()} | {:error, Ecto.Changeset.t()}
  def create(attrs) do
    %Client{}
    |> Client.changeset(attrs)
    |> Repo.insert()
  end
end
```

```elixir
# apps/gateway_core/lib/gateway_core/release.ex
defmodule GatewayCore.Release do
  @app :gateway_core

  @doc "Run pending migrations. Called via eval in deployment scripts."
  def migrate do
    load_app()
    for repo <- repos() do
      {:ok, _, _} = Ecto.Migrator.with_repo(repo, &Ecto.Migrator.run(&1, :up, all: true))
    end
  end

  @doc "Roll back to a specific migration version."
  def rollback(version) do
    load_app()
    {:ok, _, _} = Ecto.Migrator.with_repo(hd(repos()), &Ecto.Migrator.run(&1, :down, to: version))
  end

  defp repos, do: Application.fetch_env!(@app, :ecto_repos)
  defp load_app, do: Application.load(@app)
end
```

### Step 8: API layer -- `gateway_api`

```elixir
# apps/gateway_api/lib/gateway_api_web/plugs/authenticate.ex
defmodule GatewayApiWeb.Plugs.Authenticate do
  import Plug.Conn

  def init(opts), do: opts

  def call(conn, _opts) do
    api_key = get_req_header(conn, "x-api-key") |> List.first()

    case GatewayCore.Clients.get_by_api_key(api_key) do
      nil    -> conn |> send_resp(401, "Unauthorized") |> halt()
      client -> assign(conn, :current_client, client)
    end
  end
end
```

```elixir
# apps/gateway_api/lib/gateway_api_web/router.ex
defmodule GatewayApiWeb.Router do
  use GatewayApiWeb, :router

  pipeline :api do
    plug :accepts, ["json"]
    plug GatewayApiWeb.Plugs.Authenticate
  end

  scope "/api", GatewayApiWeb do
    pipe_through :api

    post "/proxy/:service",  ProxyController, :forward
    get  "/clients/me",      ClientController, :show
  end

  get "/health", GatewayApiWeb.HealthController, :index
end
```

### Step 9: Workers -- `gateway_workers`

```elixir
# apps/gateway_workers/lib/gateway_workers/workers/audit_worker.ex
defmodule GatewayWorkers.Workers.AuditWorker do
  use Oban.Worker, queue: :audit, max_attempts: 3

  alias GatewayCore.Audit

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"event" => event, "client_id" => client_id} = args}) do
    metadata = Map.get(args, "metadata", %{})

    case Audit.record(event, client_id, metadata) do
      :ok ->
        :ok

      {:error, :unknown_event} ->
        {:cancel, "unknown event type: #{event}"}

      {:error, reason} ->
        {:error, reason}
    end
  end
end
```

### Step 10: Integration test (umbrella root)

```elixir
# test/integration/client_request_flow_test.exs
defmodule ApiGatewayUmbrella.ClientRequestFlowTest do
  use ExUnit.Case
  use GatewayCore.DataCase
  use GatewayApiWeb.ConnCase

  test "authenticated request enqueues audit log job" do
    {:ok, client} = GatewayCore.Clients.create(%{
      name: "Test Client",
      api_key: "test-key-123"
    })

    conn = build_conn()
    |> put_req_header("x-api-key", "test-key-123")
    |> get("/api/clients/me")

    assert conn.status == 200

    use Oban.Testing, repo: GatewayCore.Repo
    assert_enqueued(worker: GatewayWorkers.Workers.AuditWorker,
                    args: %{"client_id" => client.id})
  end
end
```

### Step 11: Run everything

```bash
mix deps.get
mix ecto.setup
mix test
mix deps.tree    # verify: no cycles
```

---

## Trade-off analysis

| Aspect | Umbrella | Monolith (single app) | Separate repos (microservices) |
|--------|---------|-----------------------|-------------------------------|
| Compile boundary enforcement | mix.exs deps | none | network contract |
| Inter-component call latency | function call | function call | network round-trip |
| Independent deployment | partial (release subsets) | no | yes |
| Shared DB transactions | yes | yes | no (distributed transactions) |
| Test isolation | per-app ExUnit suites | single suite | separate CI pipelines |
| Code sharing | in_umbrella dep | implicit | versioned package |

---

## Common production mistakes

**1. Putting shared config in app-level config instead of umbrella root**
Each sub-app's `mix.exs` points to `config_path: "../../config/config.exs"`. If you put
DB config in `apps/gateway_core/config/config.exs`, it won't be visible to `gateway_api`.

**2. Forgetting `build_path`, `deps_path`, `lockfile` in sub-app `mix.exs`**
Without these, each sub-app uses its own `_build`, `deps`, and `mix.lock` -- defeating the
purpose of the umbrella. All four paths must point to the umbrella root.

**3. Circular dependencies**
`gateway_core` importing `GatewayApiWeb.Endpoint` to broadcast PubSub events is a cycle.
Use Phoenix.PubSub directly -- `gateway_api` subscribes, `gateway_core` publishes via the
PubSub server name.

**4. Running `mix test` inside a sub-app without proper DataCase**
The `GatewayCore.Repo` sandbox must be started for integration tests. Run `mix test` from
the umbrella root -- it sets up the full application before running any test suite.

---

## Resources

- [Mix Umbrella projects](https://hexdocs.pm/mix/Mix.html#module-umbrella-projects) -- official docs
- [Boundary library](https://github.com/sasa1977/boundary) -- compile-time enforcement of cross-app deps
- [Programming Phoenix -- Pragprog](https://pragprog.com/titles/phoenix14/programming-phoenix-1-4/) -- umbrella chapter
