# Resilient HTTP Client with Circuit Breaker and Telemetry

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`, an internal HTTP gateway. The scheduler is in place (previous
exercise). The gateway proxies requests to multiple upstream services. When an upstream is
degraded, the gateway must not let that failure cascade — a slow payments service should
not make the entire gateway unresponsive. You need a resilient HTTP client layer.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex              # already exists — starts Finch and ETS tables
│       └── http_client/
│           ├── client.ex               # facade combining circuit breaker + retry
│           ├── circuit_breaker.ex      # ETS-based circuit breaker
│           ├── retry.ex                # retry with exponential backoff
│           └── telemetry_handler.ex    # structured logging via telemetry
├── test/
│   └── api_gateway/
│       └── http_client/
│           ├── client_test.exs         # given tests — Bypass-based
│           ├── circuit_breaker_test.exs
│           └── retry_test.exs
└── mix.exs
```

---

## The business problem

The payments service has an SLA of 99.9% uptime, but occasionally it becomes slow (>5s
responses) rather than outright down. A slow upstream means all gateway requests pile up in
connection pools, exhausting file descriptors across the node. You need:

1. A circuit breaker that opens after N consecutive failures and stops sending requests
   to a degraded upstream
2. Retry logic that retries on transient errors but respects `Retry-After` headers
3. Telemetry events on every request so the ops team can build dashboards

---

## Why circuit breaker and not just retries

Retries alone amplify load on a struggling service: if it is responding in 5s and you retry
3 times, each incoming request becomes 15s of upstream load. A circuit breaker opens after
the first wave of failures and rejects subsequent requests immediately — allowing the upstream
to recover without continued pressure.

The three states:

```
CLOSED --(N failures)--> OPEN --(recovery_timeout)--> HALF_OPEN
  ^                                                         |
  <--------(M consecutive successes)------------------------+
```

In `HALF_OPEN`, only a limited number of probe requests are allowed through. If they succeed,
the circuit closes; if they fail, it opens again with a fresh timeout.

---

## Why ETS for circuit breaker state

A GenServer holding circuit state serializes all access — every request must acquire the
lock to read the state. For a high-traffic gateway, this becomes the bottleneck.

ETS with `:set, :public, read_concurrency: true` allows concurrent reads without any
process overhead. Only state transitions (closed->open, open->half_open) require coordinated
writes.

---

## Implementation

### Step 1: `mix.exs` — add dependencies

```elixir
defp deps do
  [
    {:finch, "~> 0.18"},
    {:jason, "~> 1.4"},
    {:bypass, "~> 2.1", only: :test},
    {:telemetry, "~> 1.2"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: `lib/api_gateway/http_client/circuit_breaker.ex`

The circuit breaker stores per-host state in ETS for lock-free reads. The three states
(closed, open, half_open) are tracked along with metadata: failure count for closed,
opened_at timestamp for open, and probe count for half_open.

```elixir
defmodule ApiGateway.HttpClient.CircuitBreaker do
  @moduledoc """
  ETS-based circuit breaker. State is stored per host, read lock-free.

  State stored per host: {host, state, metadata}
  where state is :closed | :open | :half_open
  and metadata is the opened_at timestamp (for :open) or failure count (for :closed).
  """

  @table :circuit_breaker

  @failure_threshold  5
  @recovery_timeout_ms 30_000
  @half_open_max_calls 3

  def init do
    :ets.new(@table, [:set, :public, :named_table, read_concurrency: true])
  end

  @doc """
  Executes `fun` through the circuit breaker for `host`.

  Returns the result of `fun.()` if the circuit is closed or half-open.
  Returns `{:error, :circuit_open}` if the circuit is open and not yet ready to probe.
  """
  @spec call(String.t(), (-> {:ok, term()} | {:error, term()})) ::
          {:ok, term()} | {:error, term()}
  def call(host, fun) do
    case get_state(host) do
      :closed    -> execute_closed(host, fun)
      :open      -> {:error, :circuit_open}
      :half_open -> execute_half_open(host, fun)
    end
  end

  @doc "Returns :closed, :half_open, or {:open, remaining_ms}."
  @spec status(String.t()) :: :closed | :half_open | {:open, pos_integer()}
  def status(host) do
    now = System.monotonic_time(:millisecond)

    case :ets.lookup(@table, host) do
      [{^host, :open, opened_at}] ->
        elapsed = now - opened_at
        if elapsed >= @recovery_timeout_ms do
          :half_open
        else
          {:open, @recovery_timeout_ms - elapsed}
        end

      [{^host, :half_open, _}] ->
        :half_open

      [{^host, :closed, _}] ->
        :closed

      [] ->
        :closed
    end
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  defp get_state(host) do
    now = System.monotonic_time(:millisecond)
    case :ets.lookup(@table, host) do
      [{^host, :open, opened_at}] ->
        if now - opened_at >= @recovery_timeout_ms, do: :half_open, else: :open
      [{^host, state, _}] ->
        state
      [] ->
        :closed
    end
  end

  defp execute_closed(host, fun) do
    case fun.() do
      {:ok, _} = result ->
        reset_failures(host)
        result
      {:error, _} = err ->
        failures = increment_failures(host)
        if failures >= @failure_threshold, do: open_circuit(host)
        err
    end
  end

  defp execute_half_open(host, fun) do
    # Transition to half_open state with probe count tracking.
    # Use update_counter to atomically increment probe count.
    # If this is the first probe, initialize the half_open entry.
    ensure_half_open_entry(host)
    probe_count = :ets.update_counter(@table, {:half_open_probes, host}, {2, 1}, {{:half_open_probes, host}, 0})

    case fun.() do
      {:ok, _} = result ->
        if probe_count >= @half_open_max_calls do
          # Enough successful probes — close the circuit
          close_circuit(host)
        end
        result

      {:error, _} = err ->
        # Probe failed — re-open circuit with fresh timestamp
        open_circuit(host)
        err
    end
  end

  defp open_circuit(host) do
    :ets.insert(@table, {host, :open, System.monotonic_time(:millisecond)})
    # Clean up probe counter if it exists
    :ets.delete(@table, {:half_open_probes, host})

    :telemetry.execute(
      [:api_gateway, :circuit_breaker, :state_change],
      %{},
      %{host: host, from: :closed, to: :open}
    )
  end

  defp close_circuit(host) do
    :ets.delete(@table, host)
    :ets.delete(@table, {:half_open_probes, host})

    :telemetry.execute(
      [:api_gateway, :circuit_breaker, :state_change],
      %{},
      %{host: host, from: :half_open, to: :closed}
    )
  end

  defp ensure_half_open_entry(host) do
    # Mark as half_open if not already
    case :ets.lookup(@table, host) do
      [{^host, :half_open, _}] -> :ok
      _ -> :ets.insert(@table, {host, :half_open, System.monotonic_time(:millisecond)})
    end
  end

  defp reset_failures(host) do
    :ets.delete(@table, host)
  end

  defp increment_failures(host) do
    # Atomic increment with default value. The tuple {host, :closed, 0} is the default
    # if the key doesn't exist. {3, 1} means: increment element at position 3 by 1.
    :ets.update_counter(@table, host, {3, 1}, {host, :closed, 0})
  end
end
```

### Step 3: `lib/api_gateway/http_client/retry.ex`

Retry logic wraps any function call with exponential backoff. It distinguishes
retryable server errors (5xx, 429) from non-retryable client errors (4xx) and
respects `Retry-After` headers from rate-limited responses.

```elixir
defmodule ApiGateway.HttpClient.Retry do
  @moduledoc """
  Retry wrapper with exponential backoff.

  Retryable conditions:
    - HTTP 429, 500, 502, 503, 504
    - {:error, _} (network errors)

  Non-retryable:
    - HTTP 400, 401, 403, 404, 422 (client errors)
    - HTTP 200, 201, 204 (success)
  """

  @retryable_statuses [429, 500, 502, 503, 504]

  @doc """
  Executes `fun` with up to `max_retries` retries on transient failures.
  Respects `Retry-After` response headers.
  """
  @spec with_retry((-> {:ok, map()} | {:error, term()}), keyword()) ::
          {:ok, map()} | {:error, term()}
  def with_retry(fun, opts \\ []) do
    max = Keyword.get(opts, :max_retries, 3)
    do_retry(fun, 0, max)
  end

  defp do_retry(fun, attempt, max) do
    case fun.() do
      {:ok, %{status: status}} = result when status not in @retryable_statuses ->
        result

      {:ok, %{status: 429, headers: headers}} when attempt < max ->
        delay = extract_retry_after(headers) || backoff(attempt)
        Process.sleep(delay)
        do_retry(fun, attempt + 1, max)

      {:ok, %{status: status}} when status in @retryable_statuses and attempt < max ->
        Process.sleep(backoff(attempt))
        do_retry(fun, attempt + 1, max)

      {:error, _} when attempt < max ->
        Process.sleep(backoff(attempt))
        do_retry(fun, attempt + 1, max)

      other ->
        other
    end
  end

  defp backoff(attempt) do
    # 100ms * 2^attempt, capped at 5_000ms, with jitter to prevent synchronized retries
    base_delay = min(round(100 * :math.pow(2, attempt)), 5_000)
    base_delay + :rand.uniform(50)
  end

  defp extract_retry_after(headers) do
    # Find "retry-after" header (case-insensitive), parse as seconds -> ms.
    # HTTP headers are typically lowercase but the spec allows mixed case.
    case List.keyfind(headers, "retry-after", 0) do
      {_, value} ->
        case Integer.parse(value) do
          {seconds, _} -> seconds * 1_000
          :error -> nil
        end
      nil ->
        nil
    end
  end
end
```

### Step 4: `lib/api_gateway/http_client/client.ex`

The client facade composes circuit breaker and retry into a single pipeline.
Every request: check circuit -> retry on transient errors -> emit telemetry.

```elixir
defmodule ApiGateway.HttpClient.Client do
  @moduledoc """
  Resilient HTTP client facade.

  Pipeline per request:
    1. CircuitBreaker.call(host, fn ->
         Retry.with_retry(fn ->
           Finch HTTP request
         end)
       end)
    2. Emit telemetry
  """

  alias ApiGateway.HttpClient.{CircuitBreaker, Retry}

  def get(url, opts \\ []),          do: request(:get, url, nil, opts)
  def post(url, body, opts \\ []),   do: request(:post, url, body, opts)
  def put(url, body, opts \\ []),    do: request(:put, url, body, opts)
  def delete(url, opts \\ []),       do: request(:delete, url, nil, opts)

  defp request(method, url, body, opts) do
    uri       = URI.parse(url)
    host      = uri.host
    start     = System.monotonic_time()

    result = CircuitBreaker.call(host, fn ->
      Retry.with_retry(fn ->
        do_request(method, url, body, opts)
      end, opts)
    end)

    duration = System.monotonic_time() - start
    emit_telemetry(method, host, url, result, duration)

    result
  end

  defp do_request(method, url, body, opts) do
    timeout_ms = Keyword.get(opts, :timeout_ms, 30_000)
    headers    = build_headers(body, Keyword.get(opts, :headers, []))
    encoded    = encode_body(body)

    req = Finch.build(method, url, headers, encoded)

    case Finch.request(req, ApiGateway.Finch, receive_timeout: timeout_ms) do
      {:ok, %Finch.Response{status: status, headers: resp_headers, body: resp_body}} ->
        # Decode JSON body if Content-Type is application/json
        decoded_body = maybe_decode_json(resp_headers, resp_body)
        {:ok, %{status: status, headers: resp_headers, body: decoded_body}}
      {:error, _} = err ->
        err
    end
  end

  defp maybe_decode_json(headers, body) do
    content_type = List.keyfind(headers, "content-type", 0)

    case content_type do
      {_, ct} when is_binary(ct) ->
        if String.contains?(ct, "application/json") do
          case Jason.decode(body) do
            {:ok, decoded} -> decoded
            {:error, _} -> body
          end
        else
          body
        end
      _ ->
        body
    end
  end

  defp build_headers(nil, headers), do: headers
  defp build_headers(body, headers) when is_map(body) do
    [{"content-type", "application/json"} | headers]
  end
  defp build_headers(_, headers), do: headers

  defp encode_body(nil), do: nil
  defp encode_body(body) when is_map(body), do: Jason.encode!(body)
  defp encode_body(body), do: body

  defp emit_telemetry(method, host, url, result, duration) do
    status = case result do
      {:ok, %{status: s}} -> s
      _ -> 0
    end

    :telemetry.execute(
      [:api_gateway, :http_client, :request],
      %{duration: duration},
      %{method: method, host: host, url: url, status: status}
    )
  end
end
```

### Step 5: `lib/api_gateway/http_client/telemetry_handler.ex`

Telemetry handlers convert raw events into structured log lines. Attaching
them at application startup ensures every HTTP request and circuit breaker
state change is logged without any code changes in the client.

```elixir
defmodule ApiGateway.HttpClient.TelemetryHandler do
  require Logger

  def attach do
    :telemetry.attach_many(
      "api-gateway-http-client",
      [
        [:api_gateway, :http_client, :request],
        [:api_gateway, :circuit_breaker, :state_change]
      ],
      &handle_event/4,
      nil
    )
  end

  def handle_event([:api_gateway, :http_client, :request], %{duration: dur}, meta, _) do
    ms = System.convert_time_unit(dur, :native, :millisecond)
    Logger.info("HTTP #{meta.method} #{meta.host} -> #{meta.status} (#{ms}ms)")
  end

  def handle_event([:api_gateway, :circuit_breaker, :state_change], _, meta, _) do
    Logger.warning("Circuit breaker: #{meta.host} #{meta.from} -> #{meta.to}")
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/http_client/circuit_breaker_test.exs
defmodule ApiGateway.HttpClient.CircuitBreakerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.HttpClient.CircuitBreaker

  setup do
    # Reset ETS table between tests
    :ets.delete_all_objects(:circuit_breaker)
    :ok
  end

  test "circuit starts closed and allows requests" do
    result = CircuitBreaker.call("api.example.com", fn -> {:ok, %{status: 200}} end)
    assert {:ok, _} = result
  end

  test "circuit opens after failure threshold" do
    host = "flaky.example.com"
    for _ <- 1..5 do
      CircuitBreaker.call(host, fn -> {:error, :timeout} end)
    end
    assert {:error, :circuit_open} = CircuitBreaker.call(host, fn -> {:ok, %{}} end)
  end

  test "circuit transitions to half_open after recovery timeout" do
    host = "recovering.example.com"
    # Force open state with past timestamp (recovery already elapsed)
    past = System.monotonic_time(:millisecond) - 60_000
    :ets.insert(:circuit_breaker, {host, :open, past})

    # Next call should attempt half_open probe
    result = CircuitBreaker.call(host, fn -> {:ok, %{status: 200}} end)
    assert {:ok, _} = result
  end
end
```

```elixir
# test/api_gateway/http_client/retry_test.exs
defmodule ApiGateway.HttpClient.RetryTest do
  use ExUnit.Case

  alias ApiGateway.HttpClient.Retry

  test "does not retry on 200" do
    calls = :counters.new(1, [])
    Retry.with_retry(fn ->
      :counters.add(calls, 1, 1)
      {:ok, %{status: 200, headers: [], body: ""}}
    end)
    assert :counters.get(calls, 1) == 1
  end

  test "retries on 503 up to max_retries" do
    calls = :counters.new(1, [])
    Retry.with_retry(
      fn ->
        :counters.add(calls, 1, 1)
        {:ok, %{status: 503, headers: [], body: ""}}
      end,
      max_retries: 2
    )
    assert :counters.get(calls, 1) == 3  # 1 original + 2 retries
  end

  test "does not retry on 404" do
    calls = :counters.new(1, [])
    Retry.with_retry(fn ->
      :counters.add(calls, 1, 1)
      {:ok, %{status: 404, headers: [], body: ""}}
    end)
    assert :counters.get(calls, 1) == 1
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/http_client/ --trace
```

---

## Trade-off analysis

Fill in this table based on your implementation.

| Aspect | ETS circuit breaker | GenServer circuit breaker | No circuit breaker |
|--------|--------------------|--------------------------|--------------------|
| Read path latency | ETS lookup (~1us) | GenServer call (varies) | none |
| State consistency | eventual under concurrent writes | strong | n/a |
| Recovery detection | time-based (fixed timeout) | time-based | n/a |
| Half-open probes | per-host counter in ETS | per-host counter in state | n/a |
| Observable | telemetry events | telemetry events | blind |

Reflection: the ETS-based circuit breaker has a race condition — two concurrent requests can
both read `:closed` and both increment the failure counter. Under what traffic conditions
does this matter? Is the race acceptable for this use case?

---

## Common production mistakes

**1. Retrying on 4xx errors**
HTTP 4xx errors are client errors — retrying them wastes resources and never succeeds.
Only retry 429 (rate limit, transient), 500, 502, 503, 504 (server errors, transient).

**2. Not respecting `Retry-After`**
When a service responds with 429 and a `Retry-After: 30` header, retrying before 30 seconds
will immediately hit the rate limit again. Always extract and honor the header.

**3. Circuit breaker without jitter on recovery**
If the circuit opens for 100 requests simultaneously, and the recovery timeout is 30s,
all 100 will probe at t=30s — a coordinated thundering herd. Add jitter to the recovery
timeout to spread probes.

**4. Telemetry emitted only on success**
A telemetry event with `status: 0` when there's no HTTP response (network error, timeout)
is still valuable — it tells the ops team the request never reached the service. Always
emit telemetry regardless of outcome.

**5. Finch pool per host not pre-warmed**
Finch creates connection pools lazily by default. The first request to a new host pays the
TCP handshake + TLS negotiation cost. Pre-configure known hosts in Application config.

---

## Resources

- [Finch](https://hexdocs.pm/finch/Finch.html) — connection-pooled HTTP client built on Mint
- [Bypass](https://github.com/PSPDFKit-labs/bypass) — test HTTP servers without a real service
- [`:telemetry`](https://hexdocs.pm/telemetry/telemetry.html) — instrumentation standard for the BEAM ecosystem
- [Release It! — Michael Nygard](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker pattern origin
