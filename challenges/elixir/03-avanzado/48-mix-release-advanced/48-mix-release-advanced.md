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
│   └── runtime.exs         # runtime secrets and env-specific config
├── rel/
│   ├── vm.args.eex          # Erlang VM arguments
│   └── env.sh.eex
├── lib/gateway_core/
│   ├── release.ex           # migration runner for releases
│   └── config_validator.ex  # startup validation for required config
├── lib/gateway_api_web/
│   └── plugs/health_check.ex # K8s liveness and readiness probes
├── Dockerfile               # multi-stage build
└── k8s/
    └── deployment.yaml      # Kubernetes manifest
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
----------------                          --------------------------
config.exs    --baked into binary-->     runtime.exs reads env vars
prod.exs                                  |
                                         Application.start/2 runs
```

---

## Implementation

### Step 1: `config/runtime.exs`

All production secrets are read at startup from environment variables. The `fetch_env!/1`
helper raises immediately with a descriptive message if a required variable is missing —
preventing the app from starting in a misconfigured state.

```elixir
import Config

if config_env() == :prod do
  database_url =
    System.get_env("DATABASE_URL") ||
      raise """
      Required environment variable DATABASE_URL is not set.

      In development: add to .env and source it.
      In production: set via Kubernetes secret or deployment system.
      """

  secret_key_base =
    System.get_env("SECRET_KEY_BASE") ||
      raise """
      Required environment variable SECRET_KEY_BASE is not set.

      Generate with: mix phx.gen.secret
      """

  phx_host =
    System.get_env("PHX_HOST") ||
      raise "Required environment variable PHX_HOST is not set."

  jwt_secret =
    System.get_env("JWT_SECRET") ||
      raise "Required environment variable JWT_SECRET is not set."

  config :gateway_core, GatewayCore.Repo,
    url:       database_url,
    pool_size: System.get_env("DATABASE_POOL_SIZE", "10") |> String.to_integer(),
    ssl:       System.get_env("DATABASE_SSL", "true") == "true"

  config :gateway_api, GatewayApiWeb.Endpoint,
    secret_key_base: secret_key_base,
    http: [
      ip:   {0, 0, 0, 0},
      port: System.get_env("PORT", "4000") |> String.to_integer()
    ],
    url: [
      host:   phx_host,
      scheme: "https",
      port:   443
    ],
    server: true

  config :gateway_core,
    jwt_secret: jwt_secret
end
```

### Step 2: `lib/gateway_core/config_validator.ex`

Validates that all required configuration is present before the application accepts
traffic. Called in `Application.start/2` — crashes the boot if anything is missing.
This catches misconfiguration at startup, not at the first request.

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
    missing =
      Enum.flat_map(@required, fn {app_or_module, key} ->
        value = Application.get_env(app_or_module, key) || Application.get_env(:gateway_core, key)

        if is_nil(value) or value == "" do
          [{app_or_module, key}]
        else
          []
        end
      end)

    if Enum.empty?(missing) do
      :ok
    else
      formatted =
        Enum.map_join(missing, "\n  - ", fn {app, key} ->
          "#{inspect(app)} :#{key}"
        end)

      raise """
      Missing required configuration:
        - #{formatted}

      Set the corresponding environment variables in runtime.exs or application config.
      """
    end
  end
end
```

### Step 3: `lib/gateway_api_web/plugs/health_check.ex`

Health check plug registered BEFORE the Phoenix router. Kubernetes uses `/health/live`
to determine if the process is alive, and `/health/ready` to determine if it should
receive traffic. During graceful shutdown, `/health/ready` returns 503 so Kubernetes
stops routing new requests to this pod.

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
    draining = :persistent_term.get(:app_draining, false)

    if draining do
      conn
      |> put_resp_content_type("application/json")
      |> send_resp(503, Jason.encode!(%{status: "draining"}))
      |> halt()
    else
      case check_database() do
        :ok ->
          conn
          |> put_resp_content_type("application/json")
          |> send_resp(200, Jason.encode!(%{status: "ok", checks: %{database: "ok"}}))
          |> halt()

        {:error, reason} ->
          conn
          |> put_resp_content_type("application/json")
          |> send_resp(503, Jason.encode!(%{status: "unhealthy", checks: %{database: inspect(reason)}}))
          |> halt()
      end
    end
  end

  def call(conn, _), do: conn

  defp check_database do
    case Ecto.Adapters.SQL.query(GatewayCore.Repo, "SELECT 1", []) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  rescue
    error -> {:error, error}
  end
end
```

### Step 4: `lib/gateway_core/shutdown_handler.ex`

Handles SIGTERM for graceful shutdown. When Kubernetes sends SIGTERM during a rolling
deploy, this handler marks the app as draining (health check returns 503), pauses Oban
queues, and waits for in-flight requests to complete before shutting down.

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
    Logger.info("SIGTERM received -- starting graceful drain")

    # Mark the app as draining — health check /ready will return 503.
    # Kubernetes will stop routing new traffic here within ~5s.
    :persistent_term.put(:app_draining, true)

    # Pause Oban queues — don't start new jobs, let running ones finish
    Oban.pause_queue(queue: :notifications)
    Oban.pause_queue(queue: :audit)
    Oban.pause_queue(queue: :reports)

    # Force shutdown after drain timeout
    Process.send_after(self(), :force_shutdown, @drain_timeout_ms)

    {:noreply, %{state | draining: true}}
  end

  @impl true
  def handle_info(:force_shutdown, state) do
    Logger.warning("Drain timeout (#{@drain_timeout_ms}ms) reached -- forcing shutdown")
    System.stop(0)
    {:noreply, state}
  end
end
```

### Step 5: `lib/gateway_core/release.ex`

Migration runner for releases. Called via `eval` in deployment scripts before starting
the application. This avoids requiring Mix on the production server.

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

Erlang VM arguments control process limits, crash dump behavior, and distributed
Erlang naming. These are evaluated at release start time using EEx.

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

The multi-stage build separates compilation from the runtime image. The first stage
installs Elixir and compiles the release; the second stage copies only the compiled
release into a minimal Debian image. This produces a ~100MB image instead of ~1GB.

```dockerfile
# -- Stage 1: Build --
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

# -- Stage 2: Runtime --
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
- [Distillery to Mix Release migration guide](https://elixirforum.com/t/distillery-to-mix-releases/26904)
- [Kubernetes probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) — liveness vs readiness
- [hexpm/elixir Docker images](https://hub.docker.com/r/hexpm/elixir) — official multi-arch images
