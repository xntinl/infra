# 48 — Mix Release Avanzado (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 5-7 horas  
**Área**: Mix Release · Config Providers · Docker · Deployment · OPS

---

## Contexto

Deployar una aplicación Elixir en producción va más allá de `mix release`. Necesitas configurar
secrets via variables de entorno, validar la configuración al inicio, implementar health checks,
ejecutar migraciones como parte del deployment, y garantizar que los deploys rolling no pierden
requests activos. Este capstone te convierte en experto en el ciclo completo de producción.

---

## Arquitectura del deployment

```
Build stage (CI)                    Runtime stage (prod server)
─────────────────                   ──────────────────────────
mix deps.get                        _build/prod/rel/my_app/
mix assets.deploy                   ├── bin/
MIX_ENV=prod mix release            │   └── my_app          ← entrypoint
                                    ├── lib/                ← beam files
                         ┌─────────►├── releases/
                         │          │   └── 0.1.0/
                         │          │       ├── runtime.exs ← cargado en start
                         │          │       └── sys.config
                         │
                    Docker image
                    FROM elixir:alpine (build)
                    FROM debian:slim (runtime)
```

### Flujo de inicio en producción

```
1. bin/my_app start
2. BEAM VM arranca
3. Config.Reader carga /etc/my_app/runtime.exs (si existe)
4. Sistema lee ENV vars en runtime.exs
5. Valida secrets obligatorios → crash fast si faltan
6. Application.start/2 se ejecuta
7. Startup hooks: verifica DB connection antes de aceptar tráfico
8. HTTP server escucha en puerto configurado
9. Señal SIGTERM → session draining → shutdown limpio
```

---

## Ejercicio 1 — Config providers y runtime.exs

Implementa configuración de producción robusta con validación de secrets.

### runtime.exs con validación

```elixir
# config/runtime.exs — se ejecuta en INICIO del release, no en compilación
import Config

# Helper para variables de entorno obligatorias
defp fetch_env!(name) do
  System.get_env(name) || raise """
  Environment variable #{name} is required but not set.

  For local development, add it to your .env file:
    #{name}=your-value

  In production, set it via your deployment system (Kubernetes secrets,
  AWS Parameter Store, etc.)
  """
end

if config_env() == :prod do
  database_url = fetch_env!("DATABASE_URL")
  secret_key_base = fetch_env!("SECRET_KEY_BASE")

  config :my_app, MyApp.Repo,
    url:       database_url,
    pool_size: System.get_env("DATABASE_POOL_SIZE", "10") |> String.to_integer(),
    ssl:       System.get_env("DATABASE_SSL", "true") == "true"

  config :my_app, MyAppWeb.Endpoint,
    secret_key_base: secret_key_base,
    http: [
      ip:   {0, 0, 0, 0},
      port: System.get_env("PORT", "4000") |> String.to_integer()
    ],
    url: [
      host:   fetch_env!("PHX_HOST"),
      scheme: "https",
      port:   443
    ],
    server: true

  config :my_app,
    aws_region:         System.get_env("AWS_REGION", "us-east-1"),
    stripe_secret_key:  fetch_env!("STRIPE_SECRET_KEY"),
    sendgrid_api_key:   fetch_env!("SENDGRID_API_KEY")
end
```

### Config provider desde archivo externo

```elixir
# rel/config.exs (Release config, NO application config)
import Config
import Mix.Releases.Config

release :my_app do
  set version: "0.1.0"

  set config_providers: [
    # Carga config adicional desde archivo en runtime
    # Útil para Kubernetes ConfigMaps montados como archivos
    {Config.Reader, {:system, "RELEASE_ROOT", "/etc/my_app/config.exs"}}
  ]
end
```

### Validación de configuración al inicio

```elixir
defmodule MyApp.ConfigValidator do
  @required_configs [
    {MyApp.Repo, :url},
    {MyAppWeb.Endpoint, :secret_key_base},
    {:my_app, :stripe_secret_key}
  ]

  def validate! do
    errors = Enum.flat_map(@required_configs, &check_config/1)

    unless Enum.empty?(errors) do
      raise """
      Configuration validation failed. Missing required configs:
      #{Enum.map_join(errors, "\n", &"  - #{inspect(&1)}")}
      """
    end

    :ok
  end

  defp check_config({app, key}) do
    case Application.get_env(app, key) do
      nil -> [{app, key}]
      ""  -> [{app, key}]
      _   -> []
    end
  end
end
```

### Requisitos

- `runtime.exs` funciona en dev (valores default) y prod (vars de entorno)
- `fetch_env!/1` da un mensaje de error claro con el nombre de la variable
- `ConfigValidator.validate!/0` llamado en `Application.start/2` antes de iniciar servicios
- Docs: tabla de todas las ENV vars con nombre, descripción, requerida/opcional, default
- Tests: verificar que la app crashea inmediatamente si falta una ENV var crítica

---

## Ejercicio 2 — Health checks y startup hooks

Implementa health checks y verificación de dependencias antes de aceptar tráfico.

### Plug de health check (sin Phoenix completo)

```elixir
defmodule MyApp.HealthCheck do
  @behaviour Plug

  import Plug.Conn

  def init(opts), do: opts

  def call(%Plug.Conn{request_path: "/health/live"} = conn, _opts) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(200, Jason.encode!(%{status: "ok", node: node()}))
    |> halt()
  end

  def call(%Plug.Conn{request_path: "/health/ready"} = conn, _opts) do
    checks = [
      {:database, check_database()},
      {:cache,    check_cache()},
      {:oban,     check_oban()}
    ]

    {status_code, status_text} =
      if Enum.all?(checks, fn {_, result} -> result == :ok end) do
        {200, "ready"}
      else
        {503, "not_ready"}
      end

    body = Jason.encode!(%{
      status: status_text,
      checks: Map.new(checks, fn {k, v} -> {k, v == :ok} end)
    })

    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status_code, body)
    |> halt()
  end

  def call(conn, _opts), do: conn

  defp check_database do
    case Ecto.Adapters.SQL.query(MyApp.Repo, "SELECT 1", []) do
      {:ok, _}   -> :ok
      {:error, e} -> {:error, Exception.message(e)}
    end
  rescue
    e -> {:error, Exception.message(e)}
  end

  defp check_cache do
    # Verificar conexión a Redis/ETS cache
    :ok
  end

  defp check_oban do
    case Oban.check_queue(queue: :default) do
      %{ok: true}  -> :ok
      _            -> {:error, "oban unavailable"}
    end
  end
end
```

### Startup hook — verificar DB antes de aceptar tráfico

```elixir
defmodule MyApp.Application do
  use Application

  @max_db_retries 10
  @retry_delay_ms 2_000

  def start(_type, _args) do
    # 1. Validar configuración
    MyApp.ConfigValidator.validate!()

    # 2. Verificar conectividad a DB (con reintentos)
    wait_for_database!()

    # 3. Iniciar supervisor tree
    children = [
      MyApp.Repo,
      {Oban, oban_config()},
      MyAppWeb.Endpoint
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: MyApp.Supervisor)
  end

  defp wait_for_database!(attempt \\ 1) when attempt <= @max_db_retries do
    case Ecto.Adapters.SQL.query(MyApp.Repo, "SELECT 1", []) do
      {:ok, _} ->
        :ok
      {:error, reason} ->
        require Logger
        Logger.warning("DB not ready (attempt #{attempt}/#{@max_db_retries}): #{inspect(reason)}")
        Process.sleep(@retry_delay_ms)
        wait_for_database!(attempt + 1)
    end
  rescue
    _ ->
      Process.sleep(@retry_delay_ms)
      wait_for_database!(attempt + 1)
  end

  defp wait_for_database!(_attempt) do
    raise "Cannot connect to database after #{@max_db_retries} attempts. Aborting."
  end
end
```

### Requisitos

- `/health/live`: responde 200 siempre que el proceso BEAM esté vivo (liveness probe)
- `/health/ready`: responde 200 solo cuando DB y dependencias están disponibles (readiness probe)
- Startup hook con reintentos y fail-fast si DB no disponible después de N intentos
- El health check plug se registra ANTES del endpoint Phoenix (sin framework, plug puro)
- Tests: mock de `check_database/0` que retorna error → `/health/ready` devuelve 503

---

## Ejercicio 3 — Graceful shutdown y rolling deploys

Implementa session draining para cero downtime en deploys.

### Session draining

```elixir
defmodule MyApp.ShutdownHandler do
  use GenServer
  require Logger

  @drain_timeout_ms 30_000  # 30 segundos para drenar

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def init(_) do
    # Capturar SIGTERM del OS
    :os.set_signal(:sigterm, :handle)
    {:ok, %{draining: false, start_time: nil}}
  end

  def handle_info({:signal, :sigterm}, state) do
    Logger.info("Received SIGTERM, starting graceful shutdown...")

    # 1. Marcar el health check /ready como no disponible (tráfico nuevo deja de llegar)
    :persistent_term.put(:app_draining, true)

    # 2. Pausar las queues de Oban
    Oban.pause_queue(queue: :default)
    Oban.pause_queue(queue: :critical)

    # 3. Esperar a que los jobs en curso terminen
    Process.send_after(self(), :force_shutdown, @drain_timeout_ms)

    {:noreply, %{state | draining: true, start_time: System.monotonic_time(:millisecond)}}
  end

  def handle_info(:force_shutdown, state) do
    elapsed = System.monotonic_time(:millisecond) - state.start_time
    Logger.warning("Drain timeout reached after #{elapsed}ms, forcing shutdown")
    System.stop(0)
    {:noreply, state}
  end
end
```

### Health check consciente del draining

```elixir
# En MyApp.HealthCheck.call para /health/ready:
defp check_draining do
  if :persistent_term.get(:app_draining, false) do
    {:error, "draining"}
  else
    :ok
  end
end
```

### Migración en release

```elixir
defmodule MyApp.Release do
  @app :my_app

  def migrate do
    load_app()

    for repo <- repos() do
      {:ok, _, _} = Ecto.Migrator.with_repo(repo, &Ecto.Migrator.run(&1, :up, all: true))
    end

    Logger.info("Migrations completed successfully")
  end

  def rollback(version) do
    load_app()
    repo = hd(repos())
    {:ok, _, _} = Ecto.Migrator.with_repo(repo, &Ecto.Migrator.run(&1, :down, to: version))
  end

  def create_admin(email, password) do
    load_app()
    MyApp.Accounts.create_user(%{email: email, password: password, role: :admin})
  end

  defp repos, do: Application.fetch_env!(@app, :ecto_repos)
  defp load_app do
    Application.load(@app)
    Application.ensure_all_started(:ssl)
  end
end
```

### Comando en Dockerfile/deployment

```bash
# En el entrypoint del container, ANTES de iniciar la app:
./bin/my_app eval "MyApp.Release.migrate()"

# Luego iniciar la app:
./bin/my_app start
```

### Requisitos

- SIGTERM capturado y convierte `/health/ready` a 503 (Kubernetes deja de enviar tráfico)
- Oban queues pausadas en drain para no empezar nuevos jobs
- Drain timeout de 30s: si hay jobs largos, se fuerza shutdown al expirar
- `MyApp.Release.migrate/0` ejecuta todas las migraciones pendientes
- `eval "MyApp.Release.migrate()"` funciona en el release compilado

---

## Ejercicio 4 — Dockerfile multi-stage y vm.args

Dockerfile optimizado y configuración de la VM para producción.

### Dockerfile multi-stage

```dockerfile
# Stage 1: Build
FROM hexpm/elixir:1.16.0-erlang-26.2.1-alpine-3.18.4 AS builder

RUN apk add --no-cache build-base git

WORKDIR /app

# Instalar Hex y Rebar
RUN mix local.hex --force && mix local.rebar --force

# Copiar dependencias primero para aprovechar caché de Docker
COPY mix.exs mix.lock ./
RUN MIX_ENV=prod mix deps.get --only prod
RUN MIX_ENV=prod mix deps.compile

# Compilar assets (si aplica)
COPY assets assets
RUN MIX_ENV=prod mix assets.deploy

# Compilar app
COPY . .
RUN MIX_ENV=prod mix compile

# Generar release
RUN MIX_ENV=prod mix release

# Stage 2: Runtime (imagen mínima)
FROM debian:bookworm-slim AS runner

RUN apt-get update -y && \
    apt-get install -y --no-install-recommends \
      libstdc++6 openssl libncurses5 locales ca-certificates && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# Configurar locale
RUN sed -i '/en_US.UTF-8/s/^# //g' /etc/locale.gen && locale-gen
ENV LANG en_US.UTF-8
ENV LANGUAGE en_US:en
ENV LC_ALL en_US.UTF-8

WORKDIR /app

# Usuario no-root
RUN useradd --uid 1000 --create-home appuser
RUN chown -R appuser:appuser /app
USER appuser

# Copiar el release del stage builder
COPY --from=builder --chown=appuser:appuser /app/_build/prod/rel/my_app ./

ENV HOME=/app
ENV MIX_ENV=prod
ENV RELEASE_COOKIE=my-secret-cookie  # override en producción

EXPOSE 4000

# Entrypoint con migración + inicio
ENTRYPOINT ["./bin/my_app"]
CMD ["start"]
```

### vm.args para producción

```
## rel/vm.args.eex
## Configuración de la VM Erlang para producción

# Nombre del nodo (para clustering)
-name my_app@<%= System.get_env("HOSTNAME", "localhost") %>

# Cookie para clustering (usar env var en producción)
-setcookie <%= System.get_env("RELEASE_COOKIE", "dev-cookie") %>

# Scheduler threads = CPU cores (default automático, pero hacerlo explícito)
+S <%= System.schedulers_online() %>:<%= System.schedulers_online() %>

# Aumentar el max de procesos (default 262144)
+P 1048576

# Aumentar max atoms (si se usan muchos módulos)
+t 1000000

# Max ports (conexiones de red)
+Q 65536

# Enable SMP
-smp enable

# Garbage collector: generational GC más agresivo
+hms 32768   # heap min size

# Crash dump en /tmp para no llenar disco del app
-env ERL_CRASH_DUMP /tmp/erl_crash.dump
-env ERL_CRASH_DUMP_SECONDS 5

# Deshabilitar SASL reports en stdout (usar Logger)
-logger sasl_error_logger false
```

### Kubernetes deployment

```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0      # ← cero downtime
  template:
    spec:
      containers:
      - name: my-app
        image: myapp:latest
        ports:
        - containerPort: 4000
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: my-app-secrets
              key: database-url
        - name: SECRET_KEY_BASE
          valueFrom:
            secretKeyRef:
              name: my-app-secrets
              key: secret-key-base
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 4000
          initialDelaySeconds: 10
          periodSeconds: 5
        livenessProbe:
          httpGet:
            path: /health/live
            port: 4000
          initialDelaySeconds: 30
          periodSeconds: 10
        lifecycle:
          preStop:
            exec:
              command: ["sleep", "5"]   # espera que k8s actualice endpoints
```

### Requisitos

- Dockerfile multi-stage: imagen final < 200MB (sin herramientas de build)
- Usuario no-root en el container (UID 1000)
- `vm.args.eex` con nombre de nodo dinámico via hostname
- Kubernetes readiness/liveness probes integradas con los endpoints del Ejercicio 2
- `preStop` sleep de 5s para que Kubernetes actualice el load balancer antes del drain
- Documentar el flujo completo de un rolling deploy con cero downtime

### Estructura del proyecto

```
.
├── config/
│   ├── config.exs
│   ├── dev.exs
│   ├── prod.exs
│   ├── test.exs
│   └── runtime.exs          ← secrets y config dinámica
├── rel/
│   ├── vm.args.eex          ← config de la VM
│   └── env.sh.eex           ← env vars de release
├── lib/my_app/
│   ├── application.ex       ← startup hooks + validación
│   ├── config_validator.ex  ← validación de secrets
│   ├── health_check.ex      ← plug de health
│   ├── shutdown_handler.ex  ← graceful shutdown
│   └── release.ex           ← migrate/rollback helpers
├── Dockerfile
└── k8s/
    ├── deployment.yaml
    ├── service.yaml
    └── secrets.yaml
```

---

## Criterios de aceptación

- [ ] `runtime.exs` carga secrets de ENV vars y falla claro si faltan
- [ ] `/health/live` responde 200 independientemente del estado de DB
- [ ] `/health/ready` responde 503 cuando DB está caída
- [ ] Startup hook reintenta conexión a DB 10 veces antes de fallar
- [ ] SIGTERM inicia graceful drain: `/health/ready` → 503, Oban pausa, espera jobs activos
- [ ] `./bin/my_app eval "MyApp.Release.migrate()"` ejecuta migraciones en producción
- [ ] Dockerfile multi-stage produce imagen runtime sin herramientas de build
- [ ] `vm.args.eex` configura nombre de nodo dinámico con HOSTNAME

---

## Retos adicionales (opcional)

- Clustering automático en Kubernetes: `libcluster` con `Cluster.Strategy.Kubernetes`
- Config desde AWS Parameter Store: custom Config.Provider que lee SSM en startup
- Observabilidad: OpenTelemetry con `opentelemetry_exporter` a Jaeger/Honeycomb
- Canary deployments: usar Kubernetes traffic splitting con dos Deployments
