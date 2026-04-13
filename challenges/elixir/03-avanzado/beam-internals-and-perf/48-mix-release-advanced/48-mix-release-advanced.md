# Production Deployment with Mix Release

## Overview

Ship an Elixir umbrella application to production using Mix releases. This exercise covers
the complete production lifecycle: runtime configuration with secrets, startup validation,
health checks for Kubernetes probes, graceful shutdown with drain support, and Docker
multi-stage builds.

Project structure:

```
api_gateway_umbrella/
├── config/
│   ├── config.exs
│   ├── dev.exs
│   ├── test.exs
│   └── runtime.exs
├── rel/
│   ├── vm.args.eex
│   └── env.sh.eex
├── lib/gateway_core/
│   ├── release.ex
│   ├── config_validator.ex
│   └── shutdown_handler.ex
├── lib/gateway_api_web/
│   └── plugs/health_check.ex
├── Dockerfile
└── k8s/
    └── deployment.yaml
```

---

## The business problem

The ops team deploys the gateway on Kubernetes with rolling updates. Three requirements:

1. The release must fail fast at startup if required secrets are missing
2. Kubernetes must know when the pod is ready to receive traffic (readiness probe) and
   when it is still alive but not ready (liveness probe)
3. During rolling deploys, in-flight requests must complete before the pod shuts down

---

## Why `runtime.exs` and not `config.exs`

`config.exs` and `prod.exs` are evaluated at **compile time** -- when building the release.
If you read `System.get_env("DATABASE_URL")` there, it reads the developer's machine
environment, not the production server's. The value is baked into the binary.

`runtime.exs` is evaluated at **startup time** -- after the release is shipped to the
production server. Environment variables are read from the server's environment. This is
where secrets belong.

```
compile time                              runtime
----------------                          --------------------------
config.exs    --baked into binary-->     runtime.exs reads env vars
prod.exs                                  |
                                         Application.start/2 runs
```

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `config/runtime.exs`

**Objective**: Read DATABASE_URL, SECRET_KEY_BASE, and JWT_SECRET from environment at boot so secrets never bake into binary.

All production secrets are read at startup from environment variables. Missing required
variables raise immediately with a descriptive message.

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

**Objective**: Validate all required config keys present at startup so boot crashes fast with clear messages.

Validates that all required configuration is present before the application accepts
traffic. Called in `Application.start/2`.

```elixir
defmodule GatewayCore.ConfigValidator do
  @moduledoc """
  Validates required configuration is present before the application accepts traffic.
  Called in Application.start/2 -- crashes the boot if anything is missing.
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

**Objective**: Expose /health/live (always 200) and /health/ready (503 when draining) for Kubernetes probes.

Health check plug registered BEFORE the Phoenix router. Kubernetes uses `/health/live`
for liveness and `/health/ready` for readiness. During graceful shutdown, `/health/ready`
returns 503 so Kubernetes stops routing new requests.

```elixir
defmodule GatewayApiWeb.Plugs.HealthCheck do
  @moduledoc """
  Health check plug registered BEFORE the Phoenix router.

  /health/live  -- liveness: always 200 if the BEAM is running
  /health/ready -- readiness: 200 only when DB and dependencies are healthy,
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

**Objective**: Handle SIGTERM via :os.set_signal and set :persistent_term draining flag so /health/ready returns 503.

Handles SIGTERM for graceful shutdown during rolling deploys.

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

    :persistent_term.put(:app_draining, true)

    Oban.pause_queue(queue: :notifications)
    Oban.pause_queue(queue: :audit)
    Oban.pause_queue(queue: :reports)

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

**Objective**: Provide migrate/0 and rollback/1 for Ecto migrations callable via release eval command.

Migration runner for releases. Called via `eval` in deployment scripts.

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

**Objective**: Configure BEAM process limits (+P/+Q), crash dumps, and distributed node name from environment variables.

Erlang VM arguments control process limits, crash dump behavior, and distributed Erlang.

```

### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Erlang VM arguments -- evaluated at release start

-name api_gateway@<%= System.get_env("HOSTNAME", "localhost") %>
-setcookie <%= System.get_env("RELEASE_COOKIE", "dev-insecure-cookie") %>

+P 524288
+Q 65536

-env ERL_CRASH_DUMP /tmp/erl_crash.dump
-env ERL_CRASH_DUMP_SECONDS 5

-logger sasl_error_logger false
```

### Step 7: Dockerfile (multi-stage)

**Objective**: Build release in Alpine (compiler deps), then copy binary to Debian runtime so final image excludes build toolchain.

The multi-stage build separates compilation from the runtime image. The first stage
compiles the release; the second copies only the compiled release into a minimal image.

```dockerfile
# -- Stage 1: Build --
FROM hexpm/elixir:1.16.2-erlang-26.2.5-alpine-3.19.1 AS builder

RUN apk add --no-cache build-base git

WORKDIR /app

RUN mix local.hex --force && mix local.rebar --force

COPY mix.exs mix.lock ./
COPY apps/gateway_core/mix.exs      apps/gateway_core/
COPY apps/gateway_api/mix.exs       apps/gateway_api/
COPY apps/gateway_workers/mix.exs   apps/gateway_workers/

RUN MIX_ENV=prod mix deps.get --only prod
RUN MIX_ENV=prod mix deps.compile

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

ENTRYPOINT ["./bin/api_gateway_umbrella"]
CMD ["start"]
```

### Step 8: `k8s/deployment.yaml`

**Objective**: Configure rolling deployment with /health/ready and /health/live probes, graceful preStop, resource requests/limits.

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
      maxUnavailable: 0
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
              command: ["sleep", "5"]
        resources:
          requests:
            memory: "256Mi"
            cpu: "200m"
          limits:
            memory: "512Mi"
            cpu: "1000m"
```

### Step 9: Tests

**Objective**: Validate config validator raises on missing env vars and health checks respond correctly to draining state.

```elixir
# test/gateway_core/config_validator_test.exs
defmodule GatewayCore.ConfigValidatorTest do
  use ExUnit.Case

  alias GatewayCore.ConfigValidator

  describe "GatewayCore.ConfigValidator" do
    test "validate! passes with all required config present" do
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
end
```

```elixir
# test/gateway_api_web/plugs/health_check_test.exs
defmodule GatewayApiWeb.Plugs.HealthCheckTest do
  use GatewayApiWeb.ConnCase

  describe "GatewayApiWeb.Plugs.HealthCheck" do
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
end
```

### Step 10: Build and run the release

**Objective**: Implement: Build and run the release.

```bash
MIX_ENV=prod mix release

./_build/prod/rel/api_gateway_umbrella/bin/api_gateway_umbrella \
  eval "GatewayCore.Release.migrate()"

./_build/prod/rel/api_gateway_umbrella/bin/api_gateway_umbrella start

./_build/prod/rel/api_gateway_umbrella/bin/api_gateway_umbrella remote
```

---

## Deep Dive: BEAM Scheduler Tuning and Memory Profiling in Production

The BEAM scheduler is not "magic" — it's a preemptive work-stealing scheduler that divides CPU time 
into reductions (bytecode instructions). Understanding scheduler tuning is critical when you suspect 
latency spikes in production.

**Key concepts**:
- **Reductions budget**: By default, a process gets ~2000 reductions before yielding to another process.
  Heavy CPU work (binary matching, list recursion) can exhaust the budget and cause tail latency.
- **Dirty schedulers**: If a process does CPU-intensive work (crypto, compression, numerical), it blocks 
  the main scheduler. Use dirty NIFs or `spawn_opt(..., [{:fullsweep_after, 0}])` for GC tuning.
- **Heap tuning per process**: `Process.flag(:min_heap_size, ...)` reserves heap upfront, reducing GC 
  pauses. Measure; don't guess.

**Memory profiling workflow**:
1. Run `recon:memory/0` in iex; identify top 10 memory consumers by type (atoms, binaries, ets).
2. If binaries dominate, check for refc binary leaks (binary held by process that should have been freed).
3. Use `eprof` or `fprof` for function-level CPU attribution; `recon:proc_window/3` for process memory trends.

**Production pattern**: Deploy with `+K true` (async IO), `-env ERL_MAX_PORTS 65536` (port limit), 
`+T 9` (async threads). Measure GC time with `erlang:statistics(garbage_collection)` — if >5% of uptime, 
tune heap or reduce allocation pressure. Never assume defaults are optimal for YOUR workload.

---

## Advanced Considerations

Understanding BEAM internals at production scale requires deep knowledge of scheduler behavior, memory models, and garbage collection dynamics. The soft real-time guarantees of BEAM only hold under specific conditions — high system load, uneven process distribution across schedulers, or GC pressure can break predictable latency completely. Monitor `erlang:statistics(run_queue)` in production to catch scheduler saturation before it degrades latency significantly. The difference between immediate, offheap, and continuous GC garbage collection strategies can significantly impact tail latencies in systems with millions of messages per second and sustained memory pressure.

Process reductions and the reduction counter affect scheduler fairness fundamentally. A process that runs for extended periods without yielding can starve other processes, even though the scheduler treats it fairly by reduction count per scheduling interval. This is especially critical in pipelines processing large data structures or performing recursive computations where yielding points are infrequent and difficult to predict. The BEAM's preemption model is deterministic per reduction, making performance testing reproducible but sometimes hiding race conditions that only manifest under specific load patterns and GC interactions.

The interaction between ETS, Mnesia, and process message queues creates subtle bottlenecks in distributed systems. ETS reads don't block other processes, but writes require acquiring locks; understanding when your workload transitions from read-heavy to write-heavy is crucial for capacity planning. Port drivers and NIFs bypass the BEAM scheduler entirely, which can lead to unexpected priority inversions if not carefully managed. Always profile with `eprof` and `fprof` in realistic production-like environments before deployment to catch performance surprises.


## Trade-off analysis

| Aspect | Mix release | Docker + `mix run` | Kubernetes + Distillery |
|--------|------------|--------------------|-----------------------|
| Elixir needed on server | no (self-contained) | yes | no |
| Hot code upgrades | yes (appup) | no | limited |
| Config at compile time | no (runtime.exs) | no | no |
| Startup validation | explicit (ConfigValidator) | manual | manual |
| Health check integration | explicit Plug | must add | must add |

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

**4. Not running `migrate/0` before starting the app**
Rolling out a new release with DB schema changes before running migrations causes
Ecto query failures. The Dockerfile `ENTRYPOINT` should run migrations before `start`.

**5. Multi-stage Dockerfile skipping layer cache for deps**
Copying all source files before `mix deps.get` means every code change rebuilds
the dependency layer. Always copy `mix.exs` and `mix.lock` first, run `deps.get`,
then copy source.

---

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
defmodule GatewayApiWeb.Plugs.HealthCheck do
  @moduledoc """
  Health check plug registered BEFORE the Phoenix router.

  /health/live  -- liveness: always 200 if the BEAM is running
  /health/ready -- readiness: 200 only when DB and dependencies are healthy,
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

defmodule Main do
  def main do
      IO.puts("Benchmarking initialized")
      {elapsed_us, result} = :timer.tc(fn ->
        Enum.reduce(1..1000, 0, &+/2)
      end)
      if is_number(elapsed_us) do
        IO.puts("✓ Benchmark completed: sum(1..1000) = " <> inspect(result) <> " in " <> inspect(elapsed_us) <> "µs")
      end
  end
end

Main.main()
```
