# 47 — Umbrella Application (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 6-8 horas  
**Área**: Umbrella · Mix · Arquitectura · Phoenix · Oban · Domain Separation

---

## Contexto

Una umbrella application es la solución de Elixir para separar dominios en un monorepo. Cada
sub-aplicación tiene sus propias dependencias, tests y boundaries claros. A diferencia de los
microservicios, comparten el mismo proceso BEAM y se comunican directamente vía funciones, sin
serialización de red. El reto está en mantener las dependencias unidireccionales y evitar el
acoplamiento entre apps.

---

## Arquitectura propuesta

```
my_platform/                         ← umbrella root
├── apps/
│   ├── my_platform_core/            ← dominio puro
│   │   ├── lib/
│   │   │   ├── accounts/            ← contexto de usuarios
│   │   │   ├── billing/             ← contexto de pagos
│   │   │   └── reports/             ← lógica de reportes
│   │   └── test/
│   │
│   ├── my_platform_api/             ← Phoenix HTTP API
│   │   ├── lib/
│   │   │   ├── controllers/
│   │   │   ├── views/
│   │   │   └── router.ex
│   │   └── test/
│   │
│   └── my_platform_workers/         ← Oban background jobs
│       ├── lib/
│       │   └── workers/
│       └── test/
│
├── config/
│   ├── config.exs                   ← config compartida
│   ├── dev.exs
│   ├── test.exs
│   └── runtime.exs
└── mix.exs                          ← umbrella mix.exs
```

### Grafo de dependencias (unidireccional)

```
my_platform_api     →  my_platform_core
my_platform_workers →  my_platform_core
                        ↑
                  (NO dependency inversa)
```

---

## Ejercicio 1 — Crear y estructurar el umbrella

Genera el umbrella y las tres sub-apps con sus dependencias correctas.

### Comandos de generación

```bash
# Crear el umbrella
mix new my_platform --umbrella
cd my_platform

# Crear las sub-apps dentro de apps/
cd apps
mix new my_platform_core --sup
mix phx.new my_platform_api --no-ecto --no-assets  # Phoenix sin DB propia
mix new my_platform_workers --sup
cd ..
```

### mix.exs del umbrella raíz

```elixir
# my_platform/mix.exs
defmodule MyPlatform.MixProject do
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

  # Dependencias compartidas entre todas las apps
  defp deps do
    []
  end

  defp aliases do
    [
      setup: ["deps.get", "ecto.setup"],
      "ecto.setup": ["ecto.create", "ecto.migrate", "run priv/repo/seeds.exs"],
      test: ["ecto.create --quiet", "ecto.migrate --quiet", "test"]
    ]
  end
end
```

### mix.exs de my_platform_core

```elixir
defmodule MyPlatformCore.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_platform_core,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",   # config compartida
      deps_path: "../../deps",
      lockfile: "../../mix.lock",               # lockfile unificado
      elixir: "~> 1.16",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:postgrex, ">= 0.0.0"},
      {:bcrypt_elixir, "~> 3.0"},
      {:jason, "~> 1.4"}
    ]
  end
end
```

### mix.exs de my_platform_api

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:phoenix_live_view, "~> 0.20"},
    {:my_platform_core, in_umbrella: true},   # ← dependencia intra-umbrella
    {:corsica, "~> 2.0"}
  ]
end
```

### Requisitos

- Los tres `mix.exs` con `build_path`, `config_path`, `deps_path` y `lockfile` apuntando al root
- `my_platform_api` y `my_platform_workers` dependen de `my_platform_core` via `in_umbrella: true`
- `my_platform_core` NO tiene dependencia de las otras dos
- Tests: `mix test` desde el root corre los tests de las 3 apps
- Verificar: `mix deps.tree` no muestra ciclos

---

## Ejercicio 2 — Core domain (my_platform_core)

Implementa los tres contextos de dominio con Ecto y lógica de negocio pura.

### Contexto Accounts

```elixir
defmodule MyPlatformCore.Accounts do
  alias MyPlatformCore.Repo
  alias MyPlatformCore.Accounts.User

  def create_user(attrs) do
    %User{}
    |> User.registration_changeset(attrs)
    |> Repo.insert()
  end

  def get_user(id), do: Repo.get(User, id)
  def get_user!(id), do: Repo.get!(User, id)

  def get_user_by_email(email) do
    Repo.get_by(User, email: String.downcase(email))
  end

  def authenticate(email, password) do
    user = get_user_by_email(email)
    if user && Bcrypt.verify_pass(password, user.password_hash) do
      {:ok, user}
    else
      {:error, :invalid_credentials}
    end
  end

  # Evento de dominio — la API y Workers escuchan esto via PubSub
  defp broadcast_user_created(user) do
    Phoenix.PubSub.broadcast(MyPlatform.PubSub, "users", {:user_created, user})
  end
end
```

### User schema

```elixir
defmodule MyPlatformCore.Accounts.User do
  use Ecto.Schema
  import Ecto.Changeset

  schema "users" do
    field :email,         :string
    field :name,          :string
    field :password,      :string, virtual: true, redact: true
    field :password_hash, :string
    field :role,          Ecto.Enum, values: [:user, :admin], default: :user
    field :confirmed_at,  :utc_datetime

    timestamps()
  end

  def registration_changeset(user, attrs) do
    user
    |> cast(attrs, [:email, :name, :password])
    |> validate_required([:email, :name, :password])
    |> validate_format(:email, ~r/^[^\s]+@[^\s]+$/)
    |> validate_length(:password, min: 8)
    |> unique_constraint(:email)
    |> put_password_hash()
  end

  defp put_password_hash(%{valid?: true, changes: %{password: pw}} = cs) do
    change(cs, password_hash: Bcrypt.hash_pwd_salt(pw))
  end
  defp put_password_hash(cs), do: cs
end
```

### Requisitos

- `my_platform_core` tiene su propio `Repo` configurado en shared config
- Contextos: `Accounts` (users), `Billing` (plans, subscriptions), `Reports` (generación de reportes)
- Cada contexto expone solo funciones públicas — schemas son privados al contexto
- Migraciones en `apps/my_platform_core/priv/repo/migrations/`
- Tests de contexto con `DataCase` (ExUnit + Ecto.Adapters.SQL.Sandbox)

---

## Ejercicio 3 — API layer (my_platform_api) y Workers

Implementa la API Phoenix que usa Core y los workers que procesan en background.

### Router de la API

```elixir
defmodule MyPlatformApiWeb.Router do
  use MyPlatformApiWeb, :router

  pipeline :api do
    plug :accepts, ["json"]
    plug MyPlatformApiWeb.Auth.BearerToken  # extrae user del JWT
  end

  scope "/api", MyPlatformApiWeb do
    pipe_through :api

    post "/auth/login",    AuthController,   :login
    post "/auth/register", AuthController,   :register

    # Rutas autenticadas
    pipe_through :authenticated

    get  "/users/me",      UserController,   :show
    put  "/users/me",      UserController,   :update
    post "/reports",       ReportController, :create
    get  "/reports/:id",   ReportController, :show
  end
end
```

### Controller que delega a Core + Workers

```elixir
defmodule MyPlatformApiWeb.ReportController do
  use MyPlatformApiWeb, :controller

  alias MyPlatformCore.Reports
  alias MyPlatformWorkers.ReportWorker  # depende de workers app? NO — usar alias directo

  def create(conn, %{"type" => type, "format" => format}) do
    user = conn.assigns.current_user

    # La API no llama directamente a Oban — delega al Core
    # El Core decide si generar sincrónicamente o encolar
    case Reports.request_report(user, type: type, format: format) do
      {:ok, :enqueued, report_id} ->
        conn
        |> put_status(:accepted)
        |> json(%{report_id: report_id, status: "processing"})

      {:ok, :ready, report} ->
        render(conn, :show, report: report)

      {:error, reason} ->
        conn |> put_status(:unprocessable_entity) |> json(%{error: reason})
    end
  end
end
```

### Workers como aplicación separada

```elixir
# apps/my_platform_workers/lib/workers/report_worker.ex
defmodule MyPlatformWorkers.ReportWorker do
  use Oban.Worker, queue: :reports, max_attempts: 3

  # Importa del core — dependencia permitida
  alias MyPlatformCore.Reports
  alias MyPlatformCore.Accounts

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"user_id" => uid, "type" => type, "format" => fmt}}) do
    with {:ok, user}   <- {:ok, Accounts.get_user!(uid)},
         {:ok, data}   <- Reports.generate(type, user: user),
         {:ok, output} <- Reports.render(data, format: fmt) do
      Reports.save_completed_report(uid, output)
    end
  end
end
```

### Requisitos

- La API NO importa módulos de `my_platform_workers`
- Los Workers SÍ importan módulos de `my_platform_core`
- `my_platform_core` encola jobs vía `Oban.insert/1` (Oban es dep del core o de workers?)
  - Respuesta: Oban puede ser dependencia solo de `my_platform_workers`; core emite un evento y workers lo escucha
  - O simplificar: Oban como dep del root umbrella disponible a todas las apps
- Tests: controllers con `ConnCase`, workers con `Oban.Testing`

---

## Ejercicio 4 — Config compartida, Integration Tests y Release

Configuración unificada, tests de integración inter-app y empaquetado.

### Config compartida (umbrella root)

```elixir
# config/config.exs — configuración base para todas las apps
import Config

config :my_platform_core, MyPlatformCore.Repo,
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  database: "my_platform_#{config_env()}"

config :my_platform_core,
  ecto_repos: [MyPlatformCore.Repo]

config :my_platform_api, MyPlatformApiWeb.Endpoint,
  http: [ip: {127, 0, 0, 1}, port: 4000],
  secret_key_base: "..."

config :my_platform_api, :jwt_secret, "dev-secret-change-in-prod"

config :my_platform_workers, Oban,
  repo: MyPlatformCore.Repo,
  queues: [default: 10, reports: 2]

# config/runtime.exs — secrets y config de producción
import Config

if config_env() == :prod do
  config :my_platform_core, MyPlatformCore.Repo,
    url: System.fetch_env!("DATABASE_URL"),
    pool_size: String.to_integer(System.get_env("POOL_SIZE", "10"))

  config :my_platform_api, :jwt_secret,
    System.fetch_env!("JWT_SECRET")
end
```

### Integration tests

```elixir
# test/integration/user_registration_flow_test.exs (en el root del umbrella)
defmodule MyPlatform.UserRegistrationFlowTest do
  use ExUnit.Case
  use MyPlatformCore.DataCase   # Sandbox para DB
  use MyPlatformApiWeb.ConnCase # Para HTTP

  test "registrar usuario dispara email de bienvenida" do
    conn = build_conn()
    |> post("/api/auth/register", %{
      email: "alice@example.com",
      name: "Alice",
      password: "password123"
    })

    assert conn.status == 201
    assert %{"user" => %{"email" => "alice@example.com"}} = json_response(conn, 201)

    # Verificar que el job de email fue encolado
    assert_enqueued(
      worker: MyPlatformWorkers.EmailWorker,
      args: %{"type" => "welcome", "email" => "alice@example.com"}
    )

    # Verificar que el usuario existe en DB
    assert MyPlatformCore.Accounts.get_user_by_email("alice@example.com")
  end
end
```

### Release del umbrella

```bash
# El umbrella se empaqueta como un único release
MIX_ENV=prod mix release

# Esto genera:
# _build/prod/rel/my_platform/
#   bin/my_platform          ← script de inicio
#   lib/                     ← beam files de todas las apps
#   releases/*/              ← config y vm.args

# Comandos de release:
./bin/my_platform start            # inicio en foreground
./bin/my_platform daemon           # inicio en background
./bin/my_platform remote           # conectar IEx remoto
./bin/my_platform eval "MyPlatformCore.Release.migrate()"  # migraciones en prod
```

### Migration helper para release

```elixir
defmodule MyPlatformCore.Release do
  @app :my_platform_core

  def migrate do
    load_app()
    for repo <- repos() do
      {:ok, _, _} = Ecto.Migrator.with_repo(repo, &Ecto.Migrator.run(&1, :up, all: true))
    end
  end

  def rollback(repo, version) do
    load_app()
    {:ok, _, _} = Ecto.Migrator.with_repo(repo, &Ecto.Migrator.run(&1, :down, to: version))
  end

  defp repos, do: Application.fetch_env!(@app, :ecto_repos)
  defp load_app, do: Application.load(@app)
end
```

### Requisitos

- Integration test que abarca API → Core → Workers (sin mocks — todo real con Sandbox)
- `mix test` desde el root ejecuta tests de las 3 apps en el orden correcto
- `mix release` genera un único artefacto deployable
- Documentar cómo hacer `mix release` con las 3 apps incluidas
- `MyPlatformCore.Release.migrate/0` usable en scripts de deployment

### Estructura final

```
my_platform/
├── apps/
│   ├── my_platform_core/
│   │   ├── lib/my_platform_core/
│   │   │   ├── accounts/user.ex
│   │   │   ├── accounts.ex
│   │   │   ├── billing/plan.ex
│   │   │   ├── billing.ex
│   │   │   ├── reports.ex
│   │   │   ├── repo.ex
│   │   │   └── release.ex
│   │   ├── priv/repo/migrations/
│   │   └── test/
│   ├── my_platform_api/
│   │   ├── lib/my_platform_api_web/
│   │   │   ├── controllers/
│   │   │   ├── plugs/
│   │   │   └── router.ex
│   │   └── test/
│   └── my_platform_workers/
│       ├── lib/my_platform_workers/
│       │   └── workers/
│       └── test/
├── test/integration/
├── config/
└── mix.exs
```

---

## Criterios de aceptación

- [ ] `mix new my_platform --umbrella` + 3 sub-apps correctamente estructuradas
- [ ] Dependencias unidireccionales: api → core, workers → core (verificar con `mix deps.tree`)
- [ ] Config compartida en `config/config.exs` del root funciona para las 3 apps
- [ ] `mix test` desde el root ejecuta tests de las 3 apps
- [ ] Integration test que cubre el flujo completo de registro de usuario
- [ ] `mix release` genera artefacto que incluye las 3 apps
- [ ] `MyPlatformCore.Release.migrate/0` funciona en modo release

---

## Retos adicionales (opcional)

- 4ta app: `my_platform_admin` con LiveView — depende solo de Core
- Boundary checking con la librería `boundary` — error de compilación si se viola la arquitectura
- CI/CD: GitHub Actions que corre `mix test` y `mix release` en el umbrella
- Múltiples releases: un release solo de workers (sin Phoenix) para scaling independiente
