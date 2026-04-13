# Build an API Client Wrapper — Retries, Breaker, Rate Limit, Telemetry

**Project**: `api_client_wrapper` — a production HTTP client wrapper composed of middlewares: timeout, retry with jitter, circuit breaker, rate limit, structured telemetry.

---

## Project context

Every team reinvents the "call this external API" wrapper: retry three times,
give up on 500s, open a breaker when the provider dies, emit a log line with
request ID. The result is six slightly-different wrappers per repo with subtly
different retry semantics — an operational nightmare.

This exercise builds a single, composable wrapper. Every concern is a
pluggable middleware following the pattern pioneered by Tesla and Finch's
pipelines. You pick the middlewares per client, configure them, and get
a single `request/2` that returns `{:ok, response}` or a structured error.

The wrapper composes cleanly with exercises 36 (breaker), 37 (rate limiter),
and 191 (retry with jitter) — each pattern is a self-contained middleware.
It mirrors what Finch + Tesla give you out of the box; building it yourself
reveals the trade-offs before you reach for a library.

```
api_client_wrapper/
├── lib/
│   └── api_client_wrapper/
│       ├── application.ex
│       ├── client.ex                      # public entry: request/2
│       ├── middleware.ex                  # behaviour + pipeline runner
│       └── middlewares/
│           ├── timeout.ex
│           ├── retry.ex
│           ├── circuit_breaker.ex
│           ├── rate_limit.ex
│           └── telemetry.ex
├── test/
│   └── api_client_wrapper/
│       ├── client_test.exs
│       └── middlewares/*_test.exs
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

### 1. The middleware contract

Inspired by Plug and Tesla, every middleware is a module implementing one
callback:

```
request ──▶ M1.call ──▶ M2.call ──▶ ... ──▶ adapter ──▶ response
                │                                           ▲
                └───────── (skip or modify) ────────────────┘
```

`call(request, next, opts)` either (a) invokes `next.(request)` to continue
down the pipeline and transforms the result, (b) short-circuits with an error,
or (c) skips recursion entirely (breaker short-circuit).

### 2. Pipeline composition as a reducer

The pipeline runs by folding a list of middlewares into a single function. The
innermost function is the transport adapter (Finch/Req). Each wrapping layer
sees the full request on the way in and the full result on the way out.

```elixir
adapter_fn = fn req -> HTTPAdapter.request(req) end

pipeline_fn =
  middlewares
  |> Enum.reverse()
  |> Enum.reduce(adapter_fn, fn {mod, opts}, inner ->
    fn req -> mod.call(req, inner, opts) end
  end)

pipeline_fn.(request)
```

Order matters: Telemetry outermost (captures everything including breaker
rejections); CircuitBreaker before RateLimit (don't waste rate-limit tokens on
a known-dead upstream); Retry inside both so retries are counted against the
breaker and the rate limiter.

### 3. Structured error taxonomy

Returning `{:error, :something}` loses causal chain. We use a struct:

```elixir
%ClientError{
  kind: :timeout | :circuit_open | :rate_limited | :http_error | :transport,
  status: integer() | nil,
  retriable: boolean(),
  meta: map(),
  original: term()
}
```

Downstream can pattern-match on `kind` and inspect `retriable` to decide
whether to bubble up or degrade gracefully.

### 4. Determinism under retry

A naive retry loop that retries every failure amplifies outages (one request
became four). Our retry middleware retries only when `retriable: true` AND the
request is idempotent (GET, PUT, DELETE — NOT POST unless an Idempotency-Key
header is present;

### 5. Telemetry events shape

Every request emits three events: `[:api_client, :request, :start]`,
`[:api_client, :request, :stop]`, `[:api_client, :request, :exception]`, all
with consistent metadata (`client`, `method`, `url_host`, `status`). Match the
`:telemetry.span/3` convention so you can plug in `TelemetryMetricsPrometheus`
without modification.

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

### Step 1: mix.exs

**Objective**: Declare Finch (pool), Telemetry (instrumentation), Jason (codec) so middleware pipeline is built on production foundations from the start.

```elixir
defp deps do
  [
    {:finch, "~> 0.18"},
    {:jason, "~> 1.4"},
    {:telemetry, "~> 1.2"}
  ]
end
```

### Dependencies (mix.exs)

```elixir
```elixir
adapter_fn = fn req -> HTTPAdapter.request(req) end

pipeline_fn =
  middlewares
  |> Enum.reverse()
  |> Enum.reduce(adapter_fn, fn {mod, opts}, inner ->
    fn req -> mod.call(req, inner, opts) end
  end)

pipeline_fn.(request)
```

Order matters: Telemetry outermost (captures everything including breaker
rejections); CircuitBreaker before RateLimit (don't waste rate-limit tokens on
a known-dead upstream); Retry inside both so retries are counted against the
breaker and the rate limiter.

### 3. Structured error taxonomy

Returning `{:error, :something}` loses causal chain. We use a struct:

```elixir
%ClientError{
  kind: :timeout | :circuit_open | :rate_limited | :http_error | :transport,
  status: integer() | nil,
  retriable: boolean(),
  meta: map(),
  original: term()
}
```

Downstream can pattern-match on `kind` and inspect `retriable` to decide
whether to bubble up or degrade gracefully.

### 4. Determinism under retry

A naive retry loop that retries every failure amplifies outages (one request
became four). Our retry middleware retries only when `retriable: true` AND the
request is idempotent (GET, PUT, DELETE — NOT POST unless an Idempotency-Key
header is present;

### 5. Telemetry events shape

Every request emits three events: `[:api_client, :request, :start]`,
`[:api_client, :request, :stop]`, `[:api_client, :request, :exception]`, all
with consistent metadata (`client`, `method`, `url_host`, `status`). Match the
`:telemetry.span/3` convention so you can plug in `TelemetryMetricsPrometheus`
without modification.

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

### Step 1: mix.exs

**Objective**: Declare Finch (pool), Telemetry (instrumentation), Jason (codec) so middleware pipeline is built on production foundations from the start.

```elixir
defp deps do
  [
    {:finch, "~> 0.18"},
    {:jason, "~> 1.4"},
    {:telemetry, "~> 1.2"}
  ]
end
```

### Step 2: `lib/api_client_wrapper/middleware.ex` and the error struct

**Objective**: Define Middleware behaviour and ClientError struct so middlewares are composable and errors carry retriable flag for smart retry logic.

```elixir
defmodule ApiClientWrapper.Middleware do
  @moduledoc "Behaviour every middleware must implement."

  @type request :: map()
  @type response :: {:ok, map()} | {:error, ApiClientWrapper.ClientError.t()}
  @type next :: (request() -> response())

  @callback call(request(), next(), keyword()) :: response()
end

defmodule ApiClientWrapper.ClientError do
  @type kind :: :timeout | :circuit_open | :rate_limited | :http_error | :transport
  defexception [:kind, :status, :retriable, :meta, :original]

  @type t :: %__MODULE__{
          kind: kind(),
          status: integer() | nil,
          retriable: boolean(),
          meta: map(),
          original: term()
        }

  @impl true
  def message(%{kind: kind, status: status, meta: meta}) do
    "#{kind} status=#{inspect(status)} meta=#{inspect(meta)}"
  end
end
```

### Step 3: Timeout middleware

**Objective**: Wrap next/1 call in Task.yield + Task.shutdown so timeouts abort downstream work cleanly without leaking stuck processes into caller's hierarchy.

```elixir
defmodule ApiClientWrapper.Middlewares.Timeout do
  @behaviour ApiClientWrapper.Middleware
  alias ApiClientWrapper.ClientError

  @impl true
  def call(req, next, opts) do
    timeout = Keyword.get(opts, :timeout, 5_000)

    task = Task.async(fn -> next.(req) end)

    case Task.yield(task, timeout) || Task.shutdown(task, :brutal_kill) do
      {:ok, result} ->
        result

      nil ->
        {:error,
         %ClientError{
           kind: :timeout,
           retriable: true,
           meta: %{timeout_ms: timeout},
           original: :timeout
         }}
    end
  end
end
```

### Step 4: Retry middleware (exponential + full jitter)

**Objective**: Retry only retriable errors with full-jitter backoff so thundering herd during recovery decorrelates into independent retry waves.

```elixir
defmodule ApiClientWrapper.Middlewares.Retry do
  @behaviour ApiClientWrapper.Middleware
  alias ApiClientWrapper.ClientError

  @impl true
  def call(req, next, opts) do
    max_attempts = Keyword.get(opts, :max_attempts, 3)
    base_ms = Keyword.get(opts, :base_ms, 100)
    cap_ms = Keyword.get(opts, :cap_ms, 5_000)

    do_attempt(req, next, 1, max_attempts, base_ms, cap_ms)
  end

  defp do_attempt(req, next, attempt, max, base, cap) do
    case next.(req) do
      {:ok, _} = ok ->
        ok

      {:error, %ClientError{retriable: true}} when attempt < max ->
        sleep_ms = full_jitter(attempt, base, cap)
        Process.sleep(sleep_ms)
        do_attempt(req, next, attempt + 1, max, base, cap)

      other ->
        other
    end
  end

  # AWS "full jitter": sleep between 0 and min(cap, base * 2^(attempt - 1))
  defp full_jitter(attempt, base, cap) do
    upper = min(cap, base * Bitwise.bsl(1, attempt - 1))
    :rand.uniform(upper)
  end
end
```

### Step 5: Circuit-breaker middleware (thin wrapper over a breaker)

**Objective**: Short-circuit calls when the ETS-published breaker state is `:open` so a tripped dependency rejects in microseconds instead of burning timeout budget.

```elixir
defmodule ApiClientWrapper.Middlewares.CircuitBreaker do
  @behaviour ApiClientWrapper.Middleware
  alias ApiClientWrapper.ClientError

  @impl true
  def call(req, next, opts) do
    breaker = Keyword.fetch!(opts, :breaker)

    case breaker_state(breaker) do
      :open ->
        {:error,
         %ClientError{kind: :circuit_open, retriable: false, meta: %{breaker: breaker}}}

      _ ->
        result = next.(req)
        report(breaker, result)
        result
    end
  end

  defp breaker_state(name) do
    case :ets.lookup(:circuit_breaker_states, name) do
      [{^name, state, _}] -> state
      [] -> :closed
    end
  end

  defp report(breaker, {:ok, %{status: s}}) when s in 200..499,
    do: CircuitBreakerPatterns.Breaker.report_success(breaker)

  defp report(breaker, {:ok, _}),
    do: CircuitBreakerPatterns.Breaker.report_failure(breaker)

  defp report(breaker, {:error, _}),
    do: CircuitBreakerPatterns.Breaker.report_failure(breaker)
end
```

### Step 6: Rate-limit middleware

**Objective**: Consult a token bucket keyed per tenant/route so local shaping absorbs bursts before the upstream's 429 does it for you.

```elixir
defmodule ApiClientWrapper.Middlewares.RateLimit do
  @behaviour ApiClientWrapper.Middleware
  alias ApiClientWrapper.ClientError
  alias RateLimitingPatterns.TokenBucket

  @impl true
  def call(req, next, opts) do
    key = Keyword.fetch!(opts, :key_fn).(req)
    bucket_opts = Keyword.fetch!(opts, :bucket)

    case TokenBucket.check(key, bucket_opts) do
      {:allow, _} ->
        next.(req)

      {:deny, retry_ms} ->
        {:error,
         %ClientError{
           kind: :rate_limited,
           retriable: true,
           meta: %{key: key, retry_after_ms: retry_ms}
         }}
    end
  end
end
```

### Step 7: Telemetry middleware

**Objective**: Wrap every call in `:telemetry.span` so latency, outcome and error-kind flow to handlers without threading loggers through the pipeline.

```elixir
defmodule ApiClientWrapper.Middlewares.Telemetry do
  @behaviour ApiClientWrapper.Middleware

  @impl true
  def call(req, next, opts) do
    meta = %{
      client: Keyword.fetch!(opts, :client),
      method: req.method,
      url: req.url
    }

    :telemetry.span([:api_client, :request], meta, fn ->
      result = next.(req)

      status_meta =
        case result do
          {:ok, %{status: s}} -> %{status: s, outcome: :ok}
          {:error, %{kind: k}} -> %{status: nil, outcome: {:error, k}}
        end

      {result, Map.merge(meta, status_meta)}
    end)
  end
end
```

### Step 8: Client pipeline runner — `lib/api_client_wrapper/client.ex`

**Objective**: Fold the middleware list right-to-left over the adapter so composition is a plain closure chain with zero runtime dispatch overhead.

```elixir
defmodule ApiClientWrapper.Client do
  @moduledoc """
  Composes a middleware pipeline and executes requests.

  Example:

      client =
        ApiClientWrapper.Client.new(
          adapter: &ApiClientWrapper.Adapters.Finch.request/1,
          pipeline: [
            {ApiClientWrapper.Middlewares.Telemetry, client: :stripe},
            {ApiClientWrapper.Middlewares.CircuitBreaker, breaker: :stripe},
            {ApiClientWrapper.Middlewares.Retry, max_attempts: 3, base_ms: 100, cap_ms: 2_000},
            {ApiClientWrapper.Middlewares.Timeout, timeout: 5_000}
          ]
        )

      ApiClientWrapper.Client.request(client, %{
        method: :get,
        url: "https://api.stripe.com/v1/charges"
      })
  """

  @type t :: %__MODULE__{pipeline: (map() -> ApiClientWrapper.Middleware.response())}
  defstruct [:pipeline]

  @spec new(keyword()) :: t()
  def new(opts) do
    adapter = Keyword.fetch!(opts, :adapter)
    mws = Keyword.fetch!(opts, :pipeline)

    pipeline =
      mws
      |> Enum.reverse()
      |> Enum.reduce(adapter, fn {mod, mw_opts}, inner ->
        fn req -> mod.call(req, inner, mw_opts) end
      end)

    %__MODULE__{pipeline: pipeline}
  end

  @spec request(t(), map()) :: ApiClientWrapper.Middleware.response()
  def request(%__MODULE__{pipeline: pipeline}, request), do: pipeline.(request)
end
```

### Step 9: Tests — pipeline composition

**Objective**: Exercise retry/non-retry branches with a scripted adapter agent so composition invariants are verified without touching the network.

```elixir
defmodule ApiClientWrapper.ClientTest do
  use ExUnit.Case, async: true
  alias ApiClientWrapper.{Client, ClientError}
  alias ApiClientWrapper.Middlewares.{Retry, Timeout}

  describe "ApiClientWrapper.Client" do
    test "retries on retriable error and eventually succeeds" do
      agent =
        start_agent([
          {:error, %ClientError{kind: :transport, retriable: true}},
          {:error, %ClientError{kind: :transport, retriable: true}},
          {:ok, %{status: 200, body: "ok"}}
        ])

      client =
        Client.new(
          adapter: fn _req -> next_response(agent) end,
          pipeline: [
            {Retry, max_attempts: 3, base_ms: 1, cap_ms: 5}
          ]
        )

      assert {:ok, %{status: 200}} =
               Client.request(client, %{method: :get, url: "http://x"})
    end

    test "non-retriable error is not retried" do
      counter = :counters.new(1, [])

      client =
        Client.new(
          adapter: fn _req ->
            :counters.add(counter, 1, 1)
            {:error, %ClientError{kind: :http_error, retriable: false, status: 400}}
          end,
          pipeline: [{Retry, max_attempts: 3, base_ms: 1, cap_ms: 5}]
        )

      assert {:error, %ClientError{}} =
               Client.request(client, %{method: :get, url: "http://x"})

      assert :counters.get(counter, 1) == 1
    end

    test "timeout wraps slow adapter" do
      client =
        Client.new(
          adapter: fn _req ->
            Process.sleep(200)
            {:ok, %{status: 200}}
          end,
          pipeline: [{Timeout, timeout: 50}]
        )

      assert {:error, %ClientError{kind: :timeout}} =
               Client.request(client, %{method: :get, url: "http://x"})
    end
  end

  defp start_agent(responses) do
    {:ok, a} = Agent.start_link(fn -> responses end)
    a
  end

  defp next_response(agent) do
    Agent.get_and_update(agent, fn [h | t] -> {h, t} end)
  end
end
```

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

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Middleware order is load-bearing.** Breaker before Retry means a retry
storm doesn't count against the breaker twice; Retry before Breaker means the
breaker sees every attempt. Pick the semantic you want and document it.

**2. Timeout via `Task.async` is expensive.** It spawns a process per request.
For high-RPS clients use the transport's built-in timeout (Finch's
`:receive_timeout`) instead — spawn only when you genuinely need to interrupt
uncooperative code.

**3. Jitter must be randomized per-client.** If all caller processes retry
with the same deterministic exponential backoff after an outage, they hit the
upstream in perfect sync. `:rand.uniform` with full jitter decorrelates them.

**4. Retries on non-idempotent methods corrupt state.** POST without
`Idempotency-Key` cannot be retried safely. The retry middleware should read
the request method and refuse to retry unsafe methods unless an idempotency
marker is present.

**5. Telemetry span vs execute.** `:telemetry.span/3` guarantees both start
and stop events even on exception. Hand-rolled `execute` calls frequently
forget the stop event in error paths — dashboards show start counts that
don't match stop counts.

**6. Sharing a breaker across clients.** Two clients pointing at the same
host but different endpoints need different breakers, or a slow `/reports`
opens `/health`. Key the breaker on `{host, path_prefix}` not just `host`.

**7. Memory per-pipeline.** Each `Client.new/1` returns a closure holding
references to every middleware's options. Cache the client — don't rebuild
on every request.

**8. When NOT to use this.** For one-off scripts and Oban workers that hit
an API a few times, use `Req` directly — all of this is already there. Roll
your own wrapper only when you need per-client circuit-breaker state,
custom error taxonomy, or a specific retry policy that Req/Tesla doesn't
expose.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [Tesla middleware docs](https://hexdocs.pm/tesla/Tesla.html#module-writing-middleware) — the original Elixir middleware pattern
- [`:telemetry.span/3`](https://hexdocs.pm/telemetry/telemetry.html#span/3) — official span convention
- [Finch](https://hexdocs.pm/finch/Finch.html) — HTTP client this exercise assumes as adapter
- [AWS Architecture Blog — Exponential Backoff and Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/) — the paper on full vs equal vs decorrelated jitter
- [Req](https://hexdocs.pm/req/Req.html) — batteries-included alternative built on Finch
- [Stripe API — Errors](https://stripe.com/docs/api/errors) — taxonomy of HTTP errors to model your `ClientError` after
- [Chris Keathley — Good and Bad Elixir](https://keathley.io/posts/good-and-bad-elixir) — why `with` chains + error structs beat tagged tuples for client layers
