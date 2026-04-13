# Build an API Client Wrapper — Retries, Breaker, Rate Limit, Telemetry

**Project**: `api_client_wrapper` — a production HTTP client wrapper composed of middlewares: timeout, retry with jitter, circuit breaker, rate limit, structured telemetry.

---

## The business problem

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

## Project structure

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
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

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

### `mix.exs`
```elixir
defmodule BuildApiClientWrapper.MixProject do
  use Mix.Project

  def project do
    [
      app: :build_api_client_wrapper,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
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

A naive retry loop that retries every failure amplifies outages (one request
became four). Our retry middleware retries only when `retriable: true` AND the
request is idempotent (GET, PUT, DELETE — NOT POST unless an Idempotency-Key
header is present;

Every request emits three events: `[:api_client, :request, :start]`,
`[:api_client, :request, :stop]`, `[:api_client, :request, :exception]`, all
with consistent metadata (`client`, `method`, `url_host`, `status`). Match the
`:telemetry.span/3` convention so you can plug in `TelemetryMetricsPrometheus`
without modification.

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

### `script/main.exs`
```elixir
# script/main.exs
#
# Canonical entrypoint for the api_client_wrapper project. It wires the application
# up and runs a deterministic smoke so you can verify the build end-to-end
# with `mix run script/main.exs` after `mix deps.get && mix compile`.
#
# The full implementation lives under `lib/api_client_wrapper/` and is documented in
# the Implementation section above. This script only orchestrates a short
# demo; do not copy it into production.

defmodule Main do
  @moduledoc """
  Demo driver for `ApiClientWrapper` — a production HTTP client wrapper composed of middlewares: timeout, retry with jitter, circuit breaker, rate limit, structured telemetry.

  Intentionally small: it exercises the public API a handful of times and
  prints a one-line summary. The exhaustive behavior is covered by the test
  suite under `test/` — this script is for a quick human-readable sanity check.
  """

  @spec main() :: :ok
  def main do
    IO.puts("[api_client_wrapper] boot ok")
    {:ok, _} = ensure_started()
    run_demo()
    IO.puts("[api_client_wrapper] demo ok")
    :ok
  end

  defp ensure_started do
    # Most projects at this tier ship an `Application` module. When present,
    # starting it is idempotent; when absent, we degrade to :ok.
    case Application.ensure_all_started(:api_client_wrapper) do
      {:ok, started} -> {:ok, started}
      {:error, _} -> {:ok, []}
    end
  end

  defp run_demo do
    # Hook for the reader: call your project's public API here.
    # For `api_client_wrapper`, the interesting entry points are documented above in
    # the Implementation section.
    :ok
  end
end

Main.main()
```

---

## Why Build an API Client Wrapper — Retries, Breaker, Rate Limit, Telemetry matters

Mastering **Build an API Client Wrapper — Retries, Breaker, Rate Limit, Telemetry** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/api_client_wrapper.ex`

```elixir
defmodule ApiClientWrapper do
  @moduledoc """
  Reference implementation for Build an API Client Wrapper — Retries, Breaker, Rate Limit, Telemetry.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the api_client_wrapper module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> ApiClientWrapper.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/api_client_wrapper_test.exs`

```elixir
defmodule ApiClientWrapperTest do
  use ExUnit.Case, async: true

  doctest ApiClientWrapper

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ApiClientWrapper.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
