# Production Deployment with Mix Release

**Project**: `mix_release_advanced` — production-grade production deployment with mix release in Elixir

---

## Why beam internals and performance matters

Performance work on the BEAM rewards depth: schedulers, reductions, process heaps, garbage collection, binary reference counting, and the JIT compiler each have observable knobs. Tools like recon, eflame, Benchee, and :sys.statistics let you measure before tuning.

The pitfall is benchmarking without a hypothesis. Senior engineers characterize the workload first (CPU-bound? Memory-bound? Lock contention?), then choose the instrument. Premature optimization on the BEAM is particularly costly because micro-benchmarks rarely reflect real scheduler behavior under load.

---

## The business problem

You are building a production-grade Elixir component in the **BEAM internals and performance** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
mix_release_advanced/
├── lib/
│   └── mix_release_advanced.ex
├── script/
│   └── main.exs
├── test/
│   └── mix_release_advanced_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in BEAM internals and performance the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule MixReleaseAdvanced.MixProject do
  use Mix.Project

  def project do
    [
      app: :mix_release_advanced,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/mix_release_advanced.ex`

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

### `test/mix_release_advanced_test.exs`

```elixir
defmodule GatewayCore.ConfigValidatorTest do
  use ExUnit.Case, async: true
  doctest GatewayCore.ConfigValidator

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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Production Deployment with Mix Release.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Production Deployment with Mix Release ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case MixReleaseAdvanced.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: MixReleaseAdvanced.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Reductions, not time, govern preemption

The BEAM scheduler counts reductions (function calls + I/O ops). After ~4000, the process yields. Long lists processed in tight Elixir loops are not the bottleneck people think.

### 2. Binary reference counting can leak

Sub-binaries hold references to large parent binaries. A 10-byte slice of a 10MB binary keeps the 10MB alive. Use :binary.copy/1 when storing slices long-term.

### 3. Profile production with recon

recon's process_window/3 finds memory leaks; bin_leak/1 finds binary refc leaks; proc_count/2 finds runaway processes. These are non-invasive and safe in production.

---
