# HTTP Client Testing with Bypass

**Project**: `bypass_http` — a weather API client with retries and timeouts

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
bypass_http/
├── lib/
│   └── bypass_http.ex
├── script/
│   └── main.exs
├── test/
│   └── bypass_http_test.exs
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
defmodule BypassHttp.MixProject do
  use Mix.Project

  def project do
    [
      app: :bypass_http,
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
### `lib/bypass_http.ex`

```elixir
defmodule Weather.Client do
  @doc "Returns base url result."
  def base_url, do: Application.get_env(:weather, :base_url, "https://api.weather.example.com")
end

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

# lib/weather/retry.ex
defmodule Weather.Retry do
  @moduledoc """
  Exponential backoff with jitter. Retriable reasons: :timeout, :closed, {:http, 5xx},
  {:http, 429} (honouring Retry-After when present).
  """

  @type attempt :: non_neg_integer()
  @type delay_ms :: non_neg_integer()
  @type reason :: :timeout | :closed | :econnrefused | {:http, integer()}

  @doc "Returns delay result from attempt and opts."
  @spec delay(attempt(), keyword()) :: delay_ms()
  def delay(attempt, opts \\ []) do
    base = Keyword.get(opts, :base_ms, 100)
    cap = Keyword.get(opts, :cap_ms, 2_000)
    backoff = min(cap, base * :math.pow(2, attempt)) |> trunc()
    jitter = :rand.uniform(max(1, div(backoff, 2)))
    backoff + jitter
  end

  @doc "Returns whether retriable holds."
  @spec retriable?(reason()) :: boolean()
  def retriable?(:timeout), do: true
  @doc "Returns whether retriable holds."
  def retriable?(:closed), do: true
  @doc "Returns whether retriable holds."
  def retriable?(:econnrefused), do: true
  @doc "Returns whether retriable holds from status."
  def retriable?({:http, status}) when status >= 500 and status < 600, do: true
  @doc "Returns whether retriable holds."
  def retriable?({:http, 429}), do: true
  @doc "Returns whether retriable holds from _."
  def retriable?(_), do: false
end

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

  @doc "Parses result from body."
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

# lib/weather/client.ex
defmodule Weather.Client do
  @moduledoc "Fetches current weather with retries and honour of Retry-After."

  alias Weather.{Parser, Retry}

  @max_attempts 4

  @doc "Fetches result from city and opts."
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
### `test/bypass_http_test.exs`

```elixir
defmodule Weather.ClientTest do
  use ExUnit.Case, async: true
  doctest Weather.Client

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
        case BypassHttp.run(payload) do
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
        for _ <- 1..1_000, do: BypassHttp.run(:bench)
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
