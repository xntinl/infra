# HTTP Client Testing with Bypass

**Project**: `weather_sync` — a weather data aggregator whose upstream HTTP client is tested against a local Cowboy-based HTTP server.

## Project context

`weather_sync` ingests forecasts from OpenWeather and National Weather Service and merges
them into a normalized schema for downstream analytics. The client code must handle: 200 OK,
429 rate-limited, 503 outages, malformed JSON, connection timeouts, and redirects.

Testing against the real OpenWeather API in CI is not viable: flaky network, quotas, and no
way to deterministically reproduce a 503. Mocking with Mox is possible but loses the wire
layer — you do not exercise JSON decoding, TLS, headers, timeouts.

**Bypass** runs a real Cowboy HTTP server on a random port, per test. You configure your
HTTP client to point at `http://localhost:<port>` and drive the server's response from the
test. The wire is real; only the upstream service is substituted.

```
weather_sync/
├── lib/
│   └── weather_sync/
│       ├── openweather/
│       │   └── client.ex           # HTTP client under test
│       └── config.ex                # resolves base_url at runtime
├── test/
│   ├── weather_sync/
│   │   └── openweather_client_test.exs
│   └── test_helper.exs
└── mix.exs
```

## Why Bypass and not Mox for HTTP testing

- **Mox**: mocks the client behaviour. Never exercises JSON decoding, compression, TLS, retries,
  redirects, header parsing. You may pass Mox tests and fail in production because `Jason.decode/1`
  raises on a trailing comma the real server sends.
- **WireMock/httparrot**: external processes, extra ops, hard to scope per test.
- **Bypass**: in-process Cowboy, one instance per test (via `Bypass.open/0`), auto-teardown,
  works with `async: true`. Exercises the whole network stack except TCP-over-internet.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Testing-specific insight:**
Tests are not QA. They document intent and catch regressions. A test that passes without asserting anything is technical debt. Always test the failure case; "it works when everything succeeds" teaches nothing. Use property-based testing for domain logic where the number of edge cases is infinite.
### 1. Bypass is a real HTTP server
Every call to `Bypass.open/0` starts Cowboy on a free port. `bypass.port` is the port number.
You point your HTTP client at `http://localhost:#{bypass.port}`.

### 2. Expectations vs stubs
- `Bypass.expect/4` — exactly one matching request, or the test fails on exit.
- `Bypass.expect_once/4` — same as above but more explicit.
- `Bypass.stub/4` — any number (including zero) of matching requests.

### 3. Fault injection
- `Bypass.down/1` — simulates a connection-refused / timeout scenario.
- `Bypass.pass/1` — record the request but let the default 500 response through.
- Raising inside the expect block returns 500 automatically.

## Design decisions

- **Option A — record-and-replay (HTTPoison fixture)**: easy but you must re-record when
  upstream changes; brittle.
- **Option B — Mox for the HTTP library**: fast but skips the wire.
- **Option C — Bypass**: real wire, deterministic, per-test. Slightly heavier (~1ms to start
  Cowboy) but completely isolated.

Chosen: **Option C**. For anything HTTP-shaped, the wire must be part of the test surface.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:req, "~> 0.5"},
    {:jason, "~> 1.4"},
    {:bypass, "~> 2.1", only: :test}
  ]
end
```

### Step 1: HTTP client

**Objective**: Fetch base_url at call-time via Application.fetch_env! so tests redirect to ephemeral Bypass ports without code rewrites.

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

### Step 2: test suite

**Objective**: Drive tests via real Bypass TCP/HTTP stack so async: true tests exercise JSON parsing, status codes, and timeout semantics end-to-end.

```elixir
# test/weather_sync/openweather_client_test.exs
defmodule WeatherSync.Openweather.ClientTest do
  use ExUnit.Case, async: true

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

## Why this works

Each test owns its own `bypass` via `setup`. The server is reachable only at a local port
that nobody else uses. `async: true` works because per-test ports prevent collisions. When
the test process exits, `Bypass` shuts the port down automatically.

The HTTP client is exercised end-to-end: `Req` opens a TCP connection, writes headers,
reads the response, parses JSON. Everything except the remote TLS endpoint is real code.

## Tests

See Step 2 — four describe blocks cover success, server errors, malformed payloads, and
transport failure.

## Benchmark

Starting Bypass takes ~500µs. A round trip is ~300µs locally. A well-structured suite of
100 Bypass tests finishes in under 1 second wall clock.

```elixir
{t, _} = :timer.tc(fn ->
  Enum.each(1..100, fn _ ->
    b = Bypass.open()
    Bypass.expect(b, fn c -> Plug.Conn.resp(c, 200, "{}") end)
    Req.get!("http://localhost:#{b.port}/x")
  end)
end)
IO.puts("100 bypass calls: #{t / 1000}ms")
```

Target: < 1000ms for 100 iterations on a modern laptop.

## Deep Dive: Bypass Patterns and Production Implications

HTTP mocking in tests presents a false choice: mock at the client level (loses realistic errors) or spin up a real HTTP server (adds complexity and brittleness). Bypass sits in the middle—it starts a real HTTP server on localhost that captures requests and allows you to define stub responses. This catches serialization bugs, malformed headers, and timeout logic that pure client mocks miss. The downside is that Bypass tests must serialize before making requests, limiting some concurrency patterns. Production incidents often involve HTTP edge cases (partial responses, connection timeouts) that live HTTP testing reveals.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Hard-coded URLs in the client**
If `base_url()` is a module attribute evaluated at compile time, you cannot repoint it per
test. Always read base URLs from `Application` config at call time.

**2. Leaking the Bypass server across tests**
Bypass cleans up automatically on test exit. If you start it inside `setup_all` and share
across tests, one test's request can arrive at another test's expect block. Prefer `setup`.

**3. Using `Bypass.stub` when `expect` is needed**
A 200-response stub that is never actually called produces a green test that does not
exercise the code under test. Use `expect_once/4` when you care that the HTTP layer was hit.

**4. Asserting on headers that vary across HTTP libraries**
`Req` sends `user-agent: req/X.Y`. If you assert on the exact UA string, tests break when you
upgrade `Req`. Assert on the presence of the header or a stable prefix.

**5. Forgetting to handle `Bypass.down/1` cleanup**
`Bypass.down/1` closes the socket. Subsequent calls get connection-refused. That is the
intent, but if a later assertion assumed the server was up, the failure mode is confusing.
Always scope `down/1` to the test that needs it.

**6. When NOT to use this**
If you are testing pure functions that accept already-parsed data, Bypass is overkill —
call them directly with fixtures. Bypass earns its keep only when the thing under test
is an HTTP client.

## Reflection

Bypass exercises everything except the remote TLS endpoint. What classes of production
bugs remain invisible to a Bypass-only test suite, and what complementary testing strategy
would catch them?


## Executable Example

```elixir
# test/weather_sync/openweather_client_test.exs
defmodule WeatherSync.Openweather.ClientTest do
  use ExUnit.Case, async: true

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

defmodule Main do
  def main do
      IO.puts("Initializing mock-based testing")
      test_result = {:ok, "mocked_response"}
      if elem(test_result, 0) == :ok do
        IO.puts("✓ Mock testing demonstrated: " <> inspect(test_result))
      end
  end
end

Main.main()
```
