# Production Deployment with Mix Release

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` umbrella is feature-complete (previous exercises). You need to ship it to
production. `mix run` is not a deployment strategy — it requires Elixir installed on the
target machine, doesn't handle secrets, and has no lifecycle management. Mix releases
solve all of this: they produce a self-contained artifact that requires only the OS.

This exercise covers the complete production lifecycle: configuration, health checks,
graceful shutdown, and Docker packaging.

Project structure after this exercise:

```
api_gateway_umbrella/
├── config/
│   ├── config.exs
│   ├── dev.exs
│   ├── test.exs
│   └── runtime.exs         # ← you write this
├── rel/
│   ├── vm.args.eex          # ← you write this
│   └── env.sh.eex
├── lib/gateway_core/
│   ├── release.ex           # ← you write this
│   └── config_validator.ex  # ← you write this
├── lib/gateway_api_web/
│   └── plugs/health_check.ex # ← you write this
├── Dockerfile               # ← you write this
└── k8s/
    └── deployment.yaml      # ← you write this
```

---

## The business problem

The ops team is deploying `api_gateway` on Kubernetes with rolling updates. Three
requirements drive this exercise:

1. The release must fail fast at startup if required secrets are missing — not silently
   run with empty strings
2. Kubernetes must know when the pod is ready to receive traffic (readiness probe) and
   when it is still alive but not ready (liveness probe)
3. During rolling deploys, in-flight requests must complete before the pod shuts down

---

## Why `runtime.exs` and not `config.exs`

`config.exs` and `prod.exs` are evaluated at **compile time** — when building the release.
If you read `System.get_env("DATABASE_URL")` there, it reads the developer's machine
environment, not the production server's. The value is baked into the binary.

`runtime.exs` is evaluated at **startup time** — after the release is shipped to the
production server, every time the process starts. Environment variables are read from
the server's environment. This is where secrets belong.

```
compile time                              runtime
────────────────                          ────────────────────────────
config.exs    ──baked into binary──▶     runtime.exs reads env vars
prod.exs                                  ↓
                                         Application.start/2 runs
```

---

## Implementation

### Step 1: `config/runtime.exs`

```elixir
import Config

defp fetch_env!(name) do
  System.get_env(name) ||
    raise """
    Required environment variable #{name} is not set.

    In development: add to .env and source it.
    In production: set via Kubernetes secret or deployment system.
    """
end

if config_env() == :prod do
  config :gateway_core, GatewayCore.Repo,
    url:       fetch_env!("DATABASE_URL"),
    pool_size: System.get_env("DATABASE_POOL_SIZE", "10") |> String.to_integer(),
    ssl:       System.get_env("DATABASE_SSL", "true") == "true"

  config :gateway_api, GatewayApiWeb.Endpoint,
    secret_key_base: fetch_env!("SECRET_KEY_BASE"),
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

  config :gateway_core,
    jwt_secret: fetch_env!("JWT_SECRET")
end
```

### Step 2: `lib/gateway_core/config_validator.ex`

```elixir
defmodule GatewayCore.ConfigValidator do
  @moduledoc """
  Validates required configuration is present before the application accepts traffic.
  Called in Application.start/2 — crashes the boot if anything is missing.
  """

  @required [
    {GatewayCore.Repo, :url},
    {GatewayApiWeb.Endpoint, :secret_key_base},
    {:gateway_core, :jwt_secret}
  ]

  @spec validate!() :: :ok
  def validate! do
    # TODO: for each {app_or_module, key} in @required,
    # check Application.get_env/2 is not nil or empty string.
    # Collect all missing configs and raise with a descriptive message listing them all.
    # HINT: use Enum.flat_map + Enum.empty? to collect and check in one pass
  end
end
```

### Step 3: `lib/gateway_api_web/plugs/health_check.ex`

```elixir
defmodule GatewayApiWeb.Plugs.HealthCheck do
  @moduledoc """
  Health check plug registered BEFORE the Phoenix router.

  /health/live  — liveness: always 200 if the BEAM is running
  /health/ready — readiness: 200 only when DB and dependencies are healthy,
                  503 when draining (SIGTERM received)
  """
  @behaviour Plug
  import Plug.Conn

  def init(opts), do: opts

  def call(%Plug.Conn{request_path: "/health/live"} = conn, _) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(200, Jason.encode!(%{status: "ok", node: node()}))
    |> halt()
  end

  def call(%Plug.Conn{request_path: "/health/ready"} = conn, _) do
    # TODO:
    # 1. If :persistent_term.get(:app_draining, false) is true → return 503
    # 2. Run check_database/0 — 503 if it fails
    # 3. Return 200 with JSON body listing each check result
  end

  def call(conn, _), do: conn

  defp check_database do
    # TODO: Ecto.Adapters.SQL.query(GatewayCore.Repo, "SELECT 1", [])
    # Return :ok or {:error, reason}
    # HINT: wrap in rescue to handle the case where Repo is not yet started
  end
end
```

### Step 4: `lib/gateway_core/shutdown_handler.ex`

```elixir
defmodule GatewayCore.ShutdownHandler do
  use GenServer
  require Logger

  @drain_timeout_ms 30_000

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_) do
    :os.set_signal(:sigterm, :handle)
    {:ok, %{draining: false}}
  end

  @impl true
  def handle_info({:signal, :sigterm}, state) do
    Logger.info("SIGTERM received — starting graceful drain")

    # Mark the app as draining — health check /ready will return 503
    # Kubernetes will stop routing new traffic here within ~5s
    :persistent_term.put(:app_draining, true)

    # Pause Oban queues — don't start new jobs
    # TODO: Oban.pause_queue(queue: :notifications)
    # TODO: Oban.pause_queue(queue: :audit)
    # TODO: Oban.pause_queue(queue: :reports)

    # Force shutdown after drain timeout
    Process.send_after(self(), :force_shutdown, @drain_timeout_ms)

    {:noreply, %{state | draining: true}}
  end

  @impl true
  def handle_info(:force_shutdown, state) do
    Logger.warning("Drain timeout (#{@drain_timeout_ms}ms) reached — forcing shutdown")
    System.stop(0)
    {:noreply, state}
  end
end
```

### Step 5: `lib/gateway_core/release.ex`

```elixir
defmodule GatewayCore.Release do
  @app :gateway_core

  @doc "Run pending migrations. Called via eval in deployment scripts."
  def migrate do
    load_app()
    for repo <- repos() do
      {:ok, _, _} = Ecto.Migrator.with_repo(repo, &Ecto.Migrator.run(&1, :up, all: true))
    end
    :ok
  end

  @doc "Roll back to a specific migration version."
  def rollback(version) do
    load_app()
    {:ok, _, _} = Ecto.Migrator.with_repo(hd(repos()), &Ecto.Migrator.run(&1, :down, to: version))
    :ok
  end

  defp repos, do: Application.fetch_env!(@app, :ecto_repos)

  defp load_app do
    Application.load(@app)
    Application.ensure_all_started(:ssl)
  end
end
```

### Step 6: `rel/vm.args.eex`

```
## Erlang VM arguments — evaluated at release start

# Node name for distributed Erlang (clustering)
-name api_gateway@<%= System.get_env("HOSTNAME", "localhost") %>

# Cluster cookie — override with RELEASE_COOKIE env var in production
-setcookie <%= System.get_env("RELEASE_COOKIE", "dev-insecure-cookie") %>

# Max concurrent processes (default 262144; increase for high-connection gateways)
+P 524288

# Max ports (network connections + file descriptors)
+Q 65536

# Crash dump location — keep out of the app directory
-env ERL_CRASH_DUMP /tmp/erl_crash.dump
-env ERL_CRASH_DUMP_SECONDS 5

# Suppress SASL progress reports (use Logger instead)
-logger sasl_error_logger false
```

### Step 7: Dockerfile (multi-stage)

```dockerfile
# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM hexpm/elixir:1.16.2-erlang-26.2.5-alpine-3.19.1 AS builder

RUN apk add --no-cache build-base git

WORKDIR /app

RUN mix local.hex --force && mix local.rebar --force

# Copy dependency manifests first — Docker layer cache
COPY mix.exs mix.lock ./
COPY apps/gateway_core/mix.exs      apps/gateway_core/
COPY apps/gateway_api/mix.exs       apps/gateway_api/
COPY apps/gateway_workers/mix.exs   apps/gateway_workers/

RUN MIX_ENV=prod mix deps.get --only prod
RUN MIX_ENV=prod mix deps.compile

# Copy source
COPY config config
COPY apps apps
COPY rel rel

RUN MIX_ENV=prod mix compile
RUN MIX_ENV=prod mix release

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM debian:bookworm-slim AS runner

RUN apt-get update -y && \
    apt-get install -y --no-install-recommends \
      libstdc++6 openssl libncurses6 locales ca-certificates && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

RUN sed -i '/en_US.UTF-8/s/^# //g' /etc/locale.gen && locale-gen
ENV LANG=en_US.UTF-8 LANGUAGE=en_US:en LC_ALL=en_US.UTF-8

WORKDIR /app

RUN useradd --uid 1000 --create-home appuser && chown -R appuser:appuser /app
USER appuser

COPY --from=builder --chown=appuser:appuser /app/_build/prod/rel/api_gateway_umbrella ./

ENV HOME=/app MIX_ENV=prod

EXPOSE 4000

# Migrations run first; app starts after
ENTRYPOINT ["./bin/api_gateway_umbrella"]
CMD ["start"]
```

### Step 8: `k8s/deployment.yaml`

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0          # zero-downtime rolling update
  template:
    spec:
      containers:
      - name: api-gateway
        image: api-gateway:latest
        ports:
        - containerPort: 4000
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: api-gateway-secrets
              key: database-url
        - name: SECRET_KEY_BASE
          valueFrom:
            secretKeyRef:
              name: api-gateway-secrets
              key: secret-key-base
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 4000
          initialDelaySeconds: 10
          periodSeconds: 5
          failureThreshold: 3
        livenessProbe:
          httpGet:
            path: /health/live
            port: 4000
          initialDelaySeconds: 30
          periodSeconds: 10
        lifecycle:
          preStop:
            exec:
              command: ["sleep", "5"]  # allow k8s to update endpoints before drain
        resources:
          requests:
            memory: "256Mi"
            cpu: "200m"
          limits:
            memory: "512Mi"
            cpu: "1000m"
```

### Step 9: Given tests — must pass without modification

```elixir
# test/gateway_core/config_validator_test.exs
defmodule GatewayCore.ConfigValidatorTest do
  use ExUnit.Case

  alias GatewayCore.ConfigValidator

  test "validate! passes with all required config present" do
    # All required keys are set in test.exs
    assert :ok = ConfigValidator.validate!()
  end

  test "validate! raises with descriptive message when config is missing" do
    original = Application.get_env(:gateway_core, :jwt_secret)
    Application.put_env(:gateway_core, :jwt_secret, nil)

    assert_raise RuntimeError, ~r/jwt_secret/, fn ->
      ConfigValidator.validate!()
    end

    Application.put_env(:gateway_core, :jwt_secret, original)
  end
end
```

```elixir
# test/gateway_api_web/plugs/health_check_test.exs
defmodule GatewayApiWeb.Plugs.HealthCheckTest do
  use GatewayApiWeb.ConnCase

  test "GET /health/live returns 200" do
    conn = get(build_conn(), "/health/live")
    assert conn.status == 200
    assert %{"status" => "ok"} = json_response(conn, 200)
  end

  test "GET /health/ready returns 200 when DB is up" do
    conn = get(build_conn(), "/health/ready")
    assert conn.status == 200
  end

  test "GET /health/ready returns 503 when draining" do
    :persistent_term.put(:app_draining, true)
    conn = get(build_conn(), "/health/ready")
    assert conn.status == 503
    :persistent_term.put(:app_draining, false)
  end
end
```

### Step 10: Build and run the release

```bash
# Build
MIX_ENV=prod mix release

# Run migrations before first start
./_build/prod/rel/api_gateway_umbrella/bin/api_gateway_umbrella \
  eval "GatewayCore.Release.migrate()"

# Start
./_build/prod/rel/api_gateway_umbrella/bin/api_gateway_umbrella start

# Connect an IEx remote shell
./_build/prod/rel/api_gateway_umbrella/bin/api_gateway_umbrella remote
```

---

## Trade-off analysis

| Aspect | Mix release | Docker + `mix run` | Kubernetes + Distillery |
|--------|------------|--------------------|-----------------------|
| Elixir needed on server | no (self-contained) | yes | no |
| Hot code upgrades | yes (appup) | no | limited |
| Config at compile time | no (runtime.exs) | no | no |
| Startup validation | explicit (ConfigValidator) | manual | manual |
| Health check integration | explicit Plug | must add | must add |

Reflection: the `preStop: sleep 5` in the Kubernetes manifest adds 5 seconds to every
pod shutdown. Why is this necessary? What happens without it when Kubernetes is doing a
rolling update? (Hint: endpoint controller propagation delay.)

---

## Common production mistakes

**1. Reading secrets in `config.exs` instead of `runtime.exs`**
Secrets baked into the release binary are visible to anyone with access to the artifact.
They also don't rotate without rebuilding. All secrets go in `runtime.exs`.

**2. No startup validation for required secrets**
An app that starts with `jwt_secret: nil` accepts all tokens (or crashes on first request).
`ConfigValidator.validate!()` in `Application.start/2` prevents silent misconfigurations.

**3. `/health/ready` returning 200 during drain**
Kubernetes uses the readiness probe to decide whether to send traffic to a pod. If it
returns 200 during drain, the pod continues receiving new requests while shutting down.
`persistent_term` is the right mechanism — it's readable without any process hop.

**4. Not running `migrate/0` before starting the app**
Rolling out a new release with DB schema changes before running migrations causes
Ecto query failures on the new code. The Dockerfile `ENTRYPOINT` should run
`eval "GatewayCore.Release.migrate()"` before `start`.

**5. Multi-stage Dockerfile skipping layer cache for deps**
Copying all source files before `mix deps.get` means every code change rebuilds
the dependency layer. Always copy `mix.exs` and `mix.lock` first, run `deps.get`,
then copy source.

---

## Resources

- [Mix Release docs](https://hexdocs.pm/mix/Mix.Tasks.Release.html) — comprehensive release configuration
- [Distillery → Mix Release migration guide](https://elixirforum.com/t/distillery-to-mix-releases/26904)
- [Kubernetes probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) — liveness vs readiness
- [hexpm/elixir Docker images](https://hub.docker.com/r/hexpm/elixir) — official multi-arch images
