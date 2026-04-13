# HTTP Client Testing with Bypass

**Project**: `weather_sync` — a weather data aggregator whose upstream HTTP client is tested against a local Cowboy-based HTTP server

---

## Why advanced testing matters

Production Elixir test suites must run in parallel, isolate side-effects, and exercise concurrent code paths without races. Tooling like Mox, ExUnit async mode, Bypass, ExMachina and StreamData turns testing from a chore into a deliberate design artifact.

When tests double as living specifications, the cost of refactoring drops. When they don't, every change becomes a coin flip. Senior teams treat the test suite as a first-class product — measuring runtime, flake rate, and coverage of failure modes alongside production metrics.

---

## The business problem

You are building a production-grade Elixir component in the **Advanced testing** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
weather_sync/
├── lib/
│   └── weather_sync.ex
├── script/
│   └── main.exs
├── test/
│   └── weather_sync_test.exs
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

Chose **B** because in Advanced testing the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule WeatherSync.MixProject do
  use Mix.Project

  def project do
    [
      app: :weather_sync,
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

### `lib/weather_sync.ex`

```elixir
# lib/weather_sync/openweather/client.ex
defmodule WeatherSync.Openweather.Client do
  @moduledoc """
  Client for the OpenWeather API. Returns normalized maps or typed errors.
  """

  @type forecast :: %{city: String.t(), temp_c: float(), observed_at: DateTime.t()}
  @type error ::
          :rate_limited
          | :service_unavailable
          | :invalid_response
          | {:http_error, pos_integer()}
          | {:transport_error, term()}

  @doc "Fetches result from city."
  @spec fetch(String.t()) :: {:ok, forecast()} | {:error, error()}
  def fetch(city) when is_binary(city) do
    url = "#{base_url()}/weather?q=#{URI.encode(city)}"

    case Req.get(url, receive_timeout: 2_000, retry: false) do
      {:ok, %{status: 200, body: body}} -> parse(body)
      {:ok, %{status: 429}} -> {:error, :rate_limited}
      {:ok, %{status: 503}} -> {:error, :service_unavailable}
      {:ok, %{status: status}} -> {:error, {:http_error, status}}
      {:error, reason} -> {:error, {:transport_error, reason}}
    end
  end

  defp parse(%{"name" => name, "main" => %{"temp" => kelvin}, "dt" => epoch}) do
    {:ok,
     %{
       city: name,
       temp_c: Float.round(kelvin - 273.15, 2),
       observed_at: DateTime.from_unix!(epoch)
     }}
  end

  defp parse(_), do: {:error, :invalid_response}

  defp base_url, do: Application.fetch_env!(:weather_sync, :openweather_base_url)
end
```

### `test/weather_sync_test.exs`

```elixir
defmodule WeatherSync.Openweather.ClientTest do
  use ExUnit.Case, async: true
  doctest WeatherSync.Openweather.Client

  alias WeatherSync.Openweather.Client

  setup do
    bypass = Bypass.open()
    Application.put_env(:weather_sync, :openweather_base_url, "http://localhost:#{bypass.port}")
    {:ok, bypass: bypass}
  end

  describe "fetch/1 — successful responses" do
    test "returns normalized forecast on 200 OK", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/weather", fn conn ->
        assert %{"q" => "Buenos Aires"} = URI.decode_query(conn.query_string)

        Plug.Conn.resp(conn, 200, Jason.encode!(%{
          "name" => "Buenos Aires",
          "main" => %{"temp" => 295.15},
          "dt"   => 1_730_000_000
        }))
      end)

      assert {:ok, forecast} = Client.fetch("Buenos Aires")
      assert forecast.city == "Buenos Aires"
      assert forecast.temp_c == 22.0
      assert forecast.observed_at.year == 2024
    end

    test "parses body regardless of key order", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/weather", fn conn ->
        # Keys deliberately out of order
        Plug.Conn.resp(conn, 200, Jason.encode!(%{
          "dt" => 1_700_000_000,
          "main" => %{"temp" => 300.0},
          "name" => "Paris"
        }))
      end)

      assert {:ok, %{city: "Paris"}} = Client.fetch("Paris")
    end
  end

  describe "fetch/1 — server-side errors" do
    test "maps 429 to :rate_limited", %{bypass: bypass} do
      Bypass.expect_once(bypass, fn conn -> Plug.Conn.resp(conn, 429, "") end)
      assert {:error, :rate_limited} = Client.fetch("X")
    end

    test "maps 503 to :service_unavailable", %{bypass: bypass} do
      Bypass.expect_once(bypass, fn conn -> Plug.Conn.resp(conn, 503, "") end)
      assert {:error, :service_unavailable} = Client.fetch("X")
    end

    test "wraps other 4xx/5xx in {:http_error, status}", %{bypass: bypass} do
      Bypass.expect_once(bypass, fn conn -> Plug.Conn.resp(conn, 418, "") end)
      assert {:error, {:http_error, 418}} = Client.fetch("X")
    end
  end

  describe "fetch/1 — malformed payloads" do
    test "missing fields return :invalid_response", %{bypass: bypass} do
      Bypass.expect_once(bypass, fn conn ->
        Plug.Conn.resp(conn, 200, Jason.encode!(%{"name" => "Only Name"}))
      end)

      assert {:error, :invalid_response} = Client.fetch("X")
    end
  end

  describe "fetch/1 — transport errors" do
    test "connection refused returns transport_error", %{bypass: bypass} do
      # Shut the server down before the request arrives
      Bypass.down(bypass)

      assert {:error, {:transport_error, _reason}} = Client.fetch("X")
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for HTTP Client Testing with Bypass.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== HTTP Client Testing with Bypass ===")
    IO.puts("Category: Advanced testing\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case WeatherSync.run(payload) do
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
        for _ <- 1..1_000, do: WeatherSync.run(:bench)
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

### 1. Async tests are the default, not the exception

ExUnit defaults to sequential execution. Set `async: true` and structure tests so they don't share global state — Application env, ETS tables, the database. The reward is 5–10× faster suites in CI.

### 2. Mock the boundary, not the dependency

A behaviour-backed mock (Mox.defmock for: SomeBehaviour) is a contract. A bare function stub is a wish. Defining the boundary as a behaviour costs one file and pays back every time the implementation changes.

### 3. Test the failure mode, always

An assertion that succeeds when everything goes right teaches nothing. Tests that prove the system handles `{:error, :timeout}`, `{:error, :network}`, and partial failures are the ones that prevent regressions.

---
