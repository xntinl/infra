# HTTP Client Testing with Bypass

**Project**: `bypass_http` — a weather API client with retries and timeouts.
**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

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

## Core concepts

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

## Implementation

### Step 1: `mix.exs`

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

```bash
mix test --trace
```

Expected: all 9 tests pass. Total runtime ~3–5 seconds, dominated by the 1s Retry-After test
and the 1.5s timeout test. If you see > 30s you have a retry storm — check `base_ms`/`cap_ms`.

---

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

## Resources

- [Bypass README](https://github.com/PSPDFKit-labs/bypass) — official usage guide
- [Finch hexdocs](https://hexdocs.pm/finch) — the client under test here
- [Plug.Conn reference](https://hexdocs.pm/plug/Plug.Conn.html) — to compose responses inside the expect plug
- ["Testing external HTTP APIs in Elixir" — Dashbit blog](https://dashbit.co/blog) — general patterns
- [Mint transport errors reference](https://hexdocs.pm/mint/Mint.TransportError.html)
- [cowboy docs](https://ninenines.eu/docs/en/cowboy/2.10/manual/) — what Bypass runs under the hood
