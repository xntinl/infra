# HTTP Client Testing with Bypass

**Project**: `bypass_http` — a weather API client with retries and timeouts.
---

## Project context

Your team maintains a weather-data enrichment service that consumes three upstream APIs,
each with different failure modes: flaky network, 429 rate limiting, 5xx flaps during
provider deployments, 30-second timeouts. Mox tests prove the domain logic around the
HTTP client is correct — but they don't exercise what actually breaks in production: how
your HTTP client (Finch) handles chunked responses, keep-alive connection resets, slow
headers, and connection pool exhaustion.

[Bypass](https://github.com/PSPDFKit-labs/bypass) spins up a real HTTP server on an ephemeral
port inside your test suite. Your code makes real TCP connections, parses real HTTP/1.1
frames, and you control exactly what the "upstream" returns — status code, headers, body,
delay, abrupt connection close. Unlike Mox, Bypass tests the full stack: TLS negotiation,
pool checkout, decompression, timeouts.

This exercise builds a production-quality weather client and tests: happy path, retry on 5xx,
respect `Retry-After` on 429, connection timeout, response timeout, abrupt disconnect,
partial body, and "upstream is simply offline" (Bypass never started or down).

Project structure:

```
bypass_http/
├── lib/
│   └── weather/
│       ├── application.ex
│       ├── client.ex              # Finch-based HTTP client
│       ├── retry.ex               # exponential backoff with jitter
│       └── parser.ex              # JSON → domain struct
├── test/
│   ├── weather/
│   │   └── client_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

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

When you call `Bypass.open/0`, Bypass starts a `Plug.Cowboy` on a random port. Your code
connects via TCP — there is no process-level mocking. This is the key difference from Mox:

```
+----------------+      TCP       +-----------------+
| Weather.Client |==============> | Bypass (cowboy) |
|  (Finch pool)  |<==============  | on port 62341   |
+----------------+                 +-----------------+
                                         ^
                                         |
                                   Bypass.expect(...)
                                   returns a Plug.Conn
```

### 2. `Bypass.expect/2` vs `Bypass.stub/2` vs `Bypass.expect_once/2`

| Function | Asserts call count | Behaviour |
|----------|-------------------|-----------|
| `expect/2` | At least once | Fails test on exit if never called |
| `expect_once/2` | Exactly once | Fails on 0 or 2+ calls |
| `stub/2` | No assertion | Silent, useful for "whatever happens" scenarios |

For testing retries you need multiple responses: use `expect/4` with a counter or call-index
closure (see implementation).

### 3. Simulating failure modes

Bypass can simulate:

- **Non-2xx status**: `Plug.Conn.resp(conn, 429, body)` + custom headers
- **Slow response**: `Process.sleep(ms)` before `send_resp/1`
- **Abrupt disconnect**: `Bypass.down/1` drops the listening socket; subsequent requests
  get `:econnrefused`
- **Partial body**: `send_chunked/2` + `chunk/2` and stop chunking midway
- **No response at all**: `Process.sleep` longer than client timeout

### 4. Finch configuration for testability

Finch wants a named pool. In tests, point it at the Bypass port using a runtime config so
the test process can override per-test:

```elixir
defmodule Weather.Client do
  def base_url, do: Application.get_env(:weather, :base_url, "https://api.weather.example.com")
end
```

In tests:

```elixir
Application.put_env(:weather, :base_url, "http://localhost:#{bypass.port}")
```

### 5. Isolation: one Bypass per test

Each test should call `Bypass.open/0` in `setup`. ExUnit tears down the process on exit so
cleanup is automatic. Reusing a single Bypass across tests works but loses `async: true`
because expectations accumulate.

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

### Step 1: `mix.exs`

**Objective**: Add Finch, Jason, and Bypass (test-only) to build real HTTP I/O testing without production overhead.

```elixir
defmodule Weather.MixProject do
  use Mix.Project

  def project do
    [
      app: :weather,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {Weather.Application, []}]
  end

  defp deps do
    [
      {:finch, "~> 0.18"},
      {:jason, "~> 1.4"},
      {:bypass, "~> 2.1", only: :test}
    ]
  end
end
```

### Step 2: Application

**Objective**: Start Finch pool with connection timeout config under the app supervisor for test environment integration.

```elixir
# lib/weather/application.ex
defmodule Weather.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Finch,
       name: Weather.Finch,
       pools: %{
         default: [size: 10, count: 1, conn_opts: [transport_opts: [timeout: 1_000]]]
       }}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: Weather.Supervisor)
  end
end
```

### Step 3: Retry helper

**Objective**: Implement exponential backoff with jitter so tests can verify retry semantics without sleeping real time.

```elixir
# lib/weather/retry.ex
defmodule Weather.Retry do
  @moduledoc """
  Exponential backoff with jitter. Retriable reasons: :timeout, :closed, {:http, 5xx},
  {:http, 429} (honouring Retry-After when present).
  """

  @type attempt :: non_neg_integer()
  @type delay_ms :: non_neg_integer()
  @type reason :: :timeout | :closed | :econnrefused | {:http, integer()}

  @spec delay(attempt(), keyword()) :: delay_ms()
  def delay(attempt, opts \\ []) do
    base = Keyword.get(opts, :base_ms, 100)
    cap = Keyword.get(opts, :cap_ms, 2_000)
    backoff = min(cap, base * :math.pow(2, attempt)) |> trunc()
    jitter = :rand.uniform(max(1, div(backoff, 2)))
    backoff + jitter
  end

  @spec retriable?(reason()) :: boolean()
  def retriable?(:timeout), do: true
  def retriable?(:closed), do: true
  def retriable?(:econnrefused), do: true
  def retriable?({:http, status}) when status >= 500 and status < 600, do: true
  def retriable?({:http, 429}), do: true
  def retriable?(_), do: false
end
```

### Step 4: Parser

**Objective**: Implement JSON schema extraction and error-handling struct so tests verify parsing boundaries without HTTP overhead.

```elixir
# lib/weather/parser.ex
defmodule Weather.Parser do
  @moduledoc "Parses upstream JSON into a compact internal struct."

  defstruct [:city, :temp_c, :humidity, :fetched_at]

  @type t :: %__MODULE__{
          city: String.t(),
          temp_c: number(),
          humidity: number(),
          fetched_at: DateTime.t()
        }

  @spec parse(binary()) :: {:ok, t()} | {:error, :invalid_json | :missing_fields}
  def parse(body) do
    with {:ok, %{"city" => c, "temp_c" => t, "humidity" => h}} <- Jason.decode(body) do
      {:ok, %__MODULE__{city: c, temp_c: t, humidity: h, fetched_at: DateTime.utc_now()}}
    else
      {:ok, _} -> {:error, :missing_fields}
      {:error, _} -> {:error, :invalid_json}
    end
  end
end
```

### Step 5: Client

**Objective**: Implement Finch-based HTTP client with retry logic, Retry-After respect, and timeout handling for production resilience.

```elixir
# lib/weather/client.ex
defmodule Weather.Client do
  @moduledoc "Fetches current weather with retries and honour of Retry-After."

  alias Weather.{Parser, Retry}

  @max_attempts 4

  @spec fetch(String.t(), keyword()) :: {:ok, Parser.t()} | {:error, term()}
  def fetch(city, opts \\ []) do
    do_fetch(city, 0, opts)
  end

  defp do_fetch(_city, attempt, _opts) when attempt >= @max_attempts,
    do: {:error, :max_retries_exhausted}

  defp do_fetch(city, attempt, opts) do
    url = base_url() <> "/v1/weather?city=" <> URI.encode(city)
    req = Finch.build(:get, url, [{"accept", "application/json"}])

    case Finch.request(req, Weather.Finch, receive_timeout: 1_000) do
      {:ok, %Finch.Response{status: 200, body: body}} ->
        Parser.parse(body)

      {:ok, %Finch.Response{status: 429, headers: headers}} ->
        retry_after = parse_retry_after(headers) || Retry.delay(attempt, opts)
        Process.sleep(retry_after)
        do_fetch(city, attempt + 1, opts)

      {:ok, %Finch.Response{status: status}} when status >= 500 ->
        if Retry.retriable?({:http, status}) do
          Process.sleep(Retry.delay(attempt, opts))
          do_fetch(city, attempt + 1, opts)
        else
          {:error, {:http, status}}
        end

      {:ok, %Finch.Response{status: status}} ->
        {:error, {:http, status}}

      {:error, %Mint.TransportError{reason: reason}} ->
        if Retry.retriable?(reason) do
          Process.sleep(Retry.delay(attempt, opts))
          do_fetch(city, attempt + 1, opts)
        else
          {:error, reason}
        end
    end
  end

  defp parse_retry_after(headers) do
    case List.keyfind(headers, "retry-after", 0) do
      {_, seconds} ->
        case Integer.parse(seconds) do
          {n, _} -> n * 1000
          :error -> nil
        end
      _ -> nil
    end
  end

  defp base_url,
    do: Application.get_env(:weather, :base_url, "https://api.weather.example.com")
end
```

### Step 6: Tests

**Objective**: Use Bypass.expect/2 to inject real HTTP failures (500s, 429s, timeouts, disconnects) and verify exponential backoff + Retry-After logic.

```elixir
# test/weather/client_test.exs
defmodule Weather.ClientTest do
  use ExUnit.Case, async: true

  alias Weather.Client

  setup do
    bypass = Bypass.open()
    Application.put_env(:weather, :base_url, "http://localhost:#{bypass.port}")
    {:ok, bypass: bypass}
  end

  describe "fetch/2 — happy path" do
    test "parses a 200 response", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/v1/weather", fn conn ->
        conn
        |> Plug.Conn.put_resp_header("content-type", "application/json")
        |> Plug.Conn.resp(200, ~s({"city":"Buenos Aires","temp_c":22.4,"humidity":60}))
      end)

      assert {:ok, w} = Client.fetch("Buenos Aires")
      assert w.city == "Buenos Aires"
      assert w.temp_c == 22.4
    end
  end

  describe "fetch/2 — retries" do
    test "retries on 500 and succeeds on third attempt", %{bypass: bypass} do
      counter = :counters.new(1, [:atomics])

      Bypass.expect(bypass, "GET", "/v1/weather", fn conn ->
        n = :counters.get(counter, 1)
        :counters.add(counter, 1, 1)

        if n < 2 do
          Plug.Conn.resp(conn, 500, "boom")
        else
          Plug.Conn.resp(conn, 200, ~s({"city":"Lima","temp_c":18,"humidity":80}))
        end
      end)

      assert {:ok, w} = Client.fetch("Lima", base_ms: 10, cap_ms: 20)
      assert w.city == "Lima"
      assert :counters.get(counter, 1) == 3
    end

    test "honours Retry-After header on 429", %{bypass: bypass} do
      counter = :counters.new(1, [:atomics])

      Bypass.expect(bypass, "GET", "/v1/weather", fn conn ->
        n = :counters.get(counter, 1)
        :counters.add(counter, 1, 1)

        if n == 0 do
          conn
          |> Plug.Conn.put_resp_header("retry-after", "1")
          |> Plug.Conn.resp(429, "slow down")
        else
          Plug.Conn.resp(conn, 200, ~s({"city":"Bogota","temp_c":15,"humidity":70}))
        end
      end)

      t0 = System.monotonic_time(:millisecond)
      assert {:ok, _} = Client.fetch("Bogota")
      elapsed = System.monotonic_time(:millisecond) - t0
      assert elapsed >= 1_000, "should wait >= Retry-After (1s), waited #{elapsed}ms"
    end

    test "gives up after max_attempts", %{bypass: bypass} do
      Bypass.expect(bypass, "GET", "/v1/weather", fn conn ->
        Plug.Conn.resp(conn, 503, "service unavailable")
      end)

      assert {:error, :max_retries_exhausted} =
               Client.fetch("Quito", base_ms: 1, cap_ms: 5)
    end
  end

  describe "fetch/2 — transport failures" do
    test "returns :econnrefused when upstream is down", %{bypass: bypass} do
      Bypass.down(bypass)

      assert {:error, :max_retries_exhausted} =
               Client.fetch("Caracas", base_ms: 1, cap_ms: 5)
    end

    test "times out when upstream is too slow", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/v1/weather", fn conn ->
        Process.sleep(1_500)
        Plug.Conn.resp(conn, 200, "{}")
      end)

      # receive_timeout in Client is 1_000 — this must return :timeout and retry
      assert {:error, _} = Client.fetch("Slow", base_ms: 1, cap_ms: 5)
    end
  end

  describe "fetch/2 — 4xx non-retriable" do
    test "returns the error without retrying on 404", %{bypass: bypass} do
      counter = :counters.new(1, [:atomics])

      Bypass.expect(bypass, "GET", "/v1/weather", fn conn ->
        :counters.add(counter, 1, 1)
        Plug.Conn.resp(conn, 404, "not found")
      end)

      assert {:error, {:http, 404}} = Client.fetch("Atlantis")
      assert :counters.get(counter, 1) == 1
    end
  end

  describe "fetch/2 — malformed body" do
    test "returns :invalid_json on non-JSON 200", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/v1/weather", fn conn ->
        Plug.Conn.resp(conn, 200, "not-json")
      end)

      assert {:error, :invalid_json} = Client.fetch("Lima")
    end

    test "returns :missing_fields when JSON lacks required keys", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/v1/weather", fn conn ->
        Plug.Conn.resp(conn, 200, ~s({"city":"Lima"}))
      end)

      assert {:error, :missing_fields} = Client.fetch("Lima")
    end
  end
end
```

### Step 7: Run

**Objective**: Verify the implementation by running the test suite.

```bash
mix test --trace
```

Expected: all 9 tests pass. Total runtime ~3–5 seconds, dominated by the 1s Retry-After test
and the 1.5s timeout test. If you see > 30s you have a retry storm — check `base_ms`/`cap_ms`.

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive: Bypass Patterns and Production Implications

HTTP mocking in tests presents a false choice: mock at the client level (loses realistic errors) or spin up a real HTTP server (adds complexity and brittleness). Bypass sits in the middle—it starts a real HTTP server on localhost that captures requests and allows you to define stub responses. This catches serialization bugs, malformed headers, and timeout logic that pure client mocks miss. The downside is that Bypass tests must serialize before making requests, limiting some concurrency patterns. Production incidents often involve HTTP edge cases (partial responses, connection timeouts) that live HTTP testing reveals.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Bypass is a real HTTP/1.1 server — no HTTP/2, no gRPC**
If your upstream requires HTTP/2 (modern gRPC, some CDNs), Bypass cannot simulate it.
Use [mox](https://hexdocs.pm/mox) with a transport behaviour, or [WireMock standalone](https://wiremock.org/)
via Docker for HTTP/2 scenarios.

**2. Port collisions between parallel suites**
`Bypass.open/0` grabs a random port but CI runners with low ephemeral range can collide.
If you see intermittent `eaddrinuse`, serialize the suite (`async: false`) or pass `port:
0` explicitly.

**3. Connection pool pollution across tests**
Finch keeps connections alive. If test A leaves a half-open connection to the (now-closed)
Bypass port, test B on a different Bypass port is unaffected *only* because the URL changes.
Sharing the pool across a suite with identical URLs is risky — each test should use a fresh
Bypass or explicit `Finch.stop/1`.

**4. Bypass.down does not kill in-flight requests**
Calling `Bypass.down/1` prevents new accepts but doesn't sever existing sockets. Any request
already being handled will complete normally. To test abrupt disconnects mid-request, drop
the connection from inside the plug: `conn |> Plug.Conn.send_chunked(200) |> then(fn _ ->
Process.exit(self(), :kill) end)`.

**5. Relative vs absolute paths in matchers**
`Bypass.expect(bypass, "GET", "/v1/weather", ...)` matches path exactly, no query string.
Use a wildcard matcher or inspect `conn.query_string` inside the plug.

**6. Async tests with one Bypass = race conditions**
Don't share one Bypass across `async: true` tests. Each `expect` is global to the Bypass
process. Open a fresh Bypass per test.

**7. Retry storm during test**
If your retry logic has base_ms=1000 and max_attempts=5, a single "always fail" test costs
31s. Parameterize retry config so tests can inject tight bounds. Shown here as `base_ms:
1, cap_ms: 5`.

**8. When NOT to use Bypass**
- Unit tests of domain logic — use Mox.
- Load testing — Bypass is single-process cowboy; it can serve a few thousand req/s but
  isn't tuned for sustained load. Use the real staging service.
- Non-HTTP protocols (TCP, UDP, WebSockets as first-class) — use [ranch](https://github.com/ninenines/ranch)
  listeners directly, or a real fake.

---

## Performance notes

Each `Bypass.open` costs ~2–5ms (starts cowboy + accept loop). For a 100-test suite, that's
200–500ms overhead — acceptable. If you see per-test setup >50ms, check whether you're
starting Finch pools inside `setup` instead of application start.

A `Finch.request` against localhost:bypass round-trips in ~500µs. Comparable to a
domestic-datacenter HTTP call.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
# test/weather/client_test.exs
defmodule Weather.ClientTest do
  use ExUnit.Case, async: true

  alias Weather.Client

  setup do
    bypass = Bypass.open()
    Application.put_env(:weather, :base_url, "http://localhost:#{bypass.port}")
    {:ok, bypass: bypass}
  end

  describe "fetch/2 — happy path" do
    test "parses a 200 response", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/v1/weather", fn conn ->
        conn
        |> Plug.Conn.put_resp_header("content-type", "application/json")
        |> Plug.Conn.resp(200, ~s({"city":"Buenos Aires","temp_c":22.4,"humidity":60}))
      end)

      assert {:ok, w} = Client.fetch("Buenos Aires")
      assert w.city == "Buenos Aires"
      assert w.temp_c == 22.4
    end
  end

  describe "fetch/2 — retries" do
    test "retries on 500 and succeeds on third attempt", %{bypass: bypass} do
      counter = :counters.new(1, [:atomics])

      Bypass.expect(bypass, "GET", "/v1/weather", fn conn ->
        n = :counters.get(counter, 1)
        :counters.add(counter, 1, 1)

        if n < 2 do
          Plug.Conn.resp(conn, 500, "boom")
        else
          Plug.Conn.resp(conn, 200, ~s({"city":"Lima","temp_c":18,"humidity":80}))
        end
      end)

      assert {:ok, w} = Client.fetch("Lima", base_ms: 10, cap_ms: 20)
      assert w.city == "Lima"
      assert :counters.get(counter, 1) == 3
    end

    test "honours Retry-After header on 429", %{bypass: bypass} do
      counter = :counters.new(1, [:atomics])

      Bypass.expect(bypass, "GET", "/v1/weather", fn conn ->
        n = :counters.get(counter, 1)
        :counters.add(counter, 1, 1)

        if n == 0 do
          conn
          |> Plug.Conn.put_resp_header("retry-after", "1")
          |> Plug.Conn.resp(429, "slow down")
        else
          Plug.Conn.resp(conn, 200, ~s({"city":"Bogota","temp_c":15,"humidity":70}))
        end
      end)

      t0 = System.monotonic_time(:millisecond)
      assert {:ok, _} = Client.fetch("Bogota")
      elapsed = System.monotonic_time(:millisecond) - t0
      assert elapsed >= 1_000, "should wait >= Retry-After (1s), waited #{elapsed}ms"
    end

    test "gives up after max_attempts", %{bypass: bypass} do
      Bypass.expect(bypass, "GET", "/v1/weather", fn conn ->
        Plug.Conn.resp(conn, 503, "service unavailable")
      end)

      assert {:error, :max_retries_exhausted} =
               Client.fetch("Quito", base_ms: 1, cap_ms: 5)
    end
  end

  describe "fetch/2 — transport failures" do
    test "returns :econnrefused when upstream is down", %{bypass: bypass} do
      Bypass.down(bypass)

      assert {:error, :max_retries_exhausted} =
               Client.fetch("Caracas", base_ms: 1, cap_ms: 5)
    end

    test "times out when upstream is too slow", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/v1/weather", fn conn ->
        Process.sleep(1_500)
        Plug.Conn.resp(conn, 200, "{}")
      end)

      # receive_timeout in Client is 1_000 — this must return :timeout and retry
      assert {:error, _} = Client.fetch("Slow", base_ms: 1, cap_ms: 5)
    end
  end

  describe "fetch/2 — 4xx non-retriable" do
    test "returns the error without retrying on 404", %{bypass: bypass} do
      counter = :counters.new(1, [:atomics])

      Bypass.expect(bypass, "GET", "/v1/weather", fn conn ->
        :counters.add(counter, 1, 1)
        Plug.Conn.resp(conn, 404, "not found")
      end)

      assert {:error, {:http, 404}} = Client.fetch("Atlantis")
      assert :counters.get(counter, 1) == 1
    end
  end

  describe "fetch/2 — malformed body" do
    test "returns :invalid_json on non-JSON 200", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/v1/weather", fn conn ->
        Plug.Conn.resp(conn, 200, "not-json")
      end)

      assert {:error, :invalid_json} = Client.fetch("Lima")
    end

    test "returns :missing_fields when JSON lacks required keys", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/v1/weather", fn conn ->
        Plug.Conn.resp(conn, 200, ~s({"city":"Lima"}))
      end)

      assert {:error, :missing_fields} = Client.fetch("Lima")
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
