# Retry with Exponential Backoff and Three Jitter Strategies

**Project**: `resilient_http` — a retry helper that supports full, equal, and decorrelated jitter on top of exponential backoff.

## Project context

Your service pulls from a rate-limited third-party analytics API. When the API returns `429 Too Many Requests`, naive retries cause a thundering herd: every request that was in-flight at the moment of throttling retries at the same `2^n * base` delay, re-hammering the upstream at predictable intervals.

Jitter spreads retries across a randomized window. Different jitter strategies trade off smoothness of load vs. maximum delay observed. This exercise builds a retry executor that supports all three AWS-documented jitter strategies, with retriable/non-retriable classification and a budget cap.

```
resilient_http/
├── lib/
│   └── resilient_http/
│       ├── retry.ex               # main retry executor
│       └── retry/
│           └── backoff.ex         # pure backoff calculation
├── test/
│   └── resilient_http/
│       ├── retry_test.exs
│       └── backoff_test.exs
└── mix.exs
```

## Why jitter (and which kind)

Without jitter, N clients throttled at the same instant all wait exactly `1000ms`, then all retry at t=1001ms, causing another throttle. This is the thundering herd. Marc Brooker's AWS post showed three strategies:

- **Full jitter**: `delay = rand(0, exp)` — maximal spread, lowest collision, but highest variance (some retries come almost immediately).
- **Equal jitter**: `delay = exp/2 + rand(0, exp/2)` — bounded below, still spread. Good for pre-warming caches.
- **Decorrelated jitter**: `delay = min(cap, rand(base, prev * 3))` — each attempt depends on the previous delay, producing the lowest total completion time under contention (best for retry storms).

All three beat no-jitter. Full jitter is the default recommended by AWS for most workloads.

## Why not `:retry` / `ExBackoff` / `Retry.DelayStreams`

`Retry.DelayStreams` from `retry` package is excellent but opaque; reading its output stream is not obvious. Building this from scratch lets you expose `compute_delay/3` as a pure function you can test deterministically, which matters because randomness is otherwise hard to assert.

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
### 1. Exponential growth
`exp = min(cap, base * 2^(attempt - 1))`. The `cap` prevents `2^10 * base = 1024s` monsters.

### 2. Retriable classification
Not every error should retry. `429`, `503`, `:timeout`, `:closed` are retriable. `400`, `401`, `422` are not (they won't succeed on retry). Classify explicitly; never blind-retry.

### 3. Budget cap
Maximum total elapsed time (`total_budget_ms`) trumps `max_attempts`. A request with `max_attempts: 10` and `total_budget_ms: 5_000` may stop at attempt 4 if subsequent delays would exceed the budget.

## Design decisions

- **Option A — Fixed list of delays**: `[100, 200, 400, 800]`. Simple but loses the retry math.
- **Option B — Delay function**: `delay_fn(attempt, prev_delay, opts)`. Composable, testable, supports all jitter strategies via one signature.
→ Chose **B**. Deterministic unit tests via an injected RNG.

- **Option A — Sleep with `Process.sleep/1`**: blocks the caller.
- **Option B — `Process.send_after/3` + receive**: non-blocking. Requires a process context.
→ Chose **A** — retries are called inside worker processes; blocking the worker is the whole point (we want to not issue the next request).

## Implementation

### Dependencies (`mix.exs`)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule ResilientHttp.MixProject do
  use Mix.Project
  def project, do: [app: :resilient_http, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [extra_applications: [:logger]]
end
```

### Step 1: Backoff math (`lib/resilient_http/retry/backoff.ex`)

**Objective**: Decouple jitter computation from randomness source via injectable RNG so curves are deterministic in tests and independent of :rand global state.

```elixir
defmodule ResilientHttp.Retry.Backoff do
  @moduledoc """
  Pure backoff calculation. Takes an RNG function to enable deterministic tests.
  """

  @type strategy :: :none | :full | :equal | :decorrelated

  @spec compute(pos_integer(), non_neg_integer(), keyword(), (integer(), integer() -> integer())) :: non_neg_integer()
  def compute(attempt, prev_delay, opts, rng \\ &rand_range/2) do
    base = Keyword.fetch!(opts, :base_ms)
    cap = Keyword.fetch!(opts, :cap_ms)
    strategy = Keyword.get(opts, :jitter, :full)

    exp = min(cap, base * pow2(attempt - 1))

    case strategy do
      :none -> exp
      :full -> rng.(0, exp)
      :equal -> div(exp, 2) + rng.(0, div(exp, 2))
      :decorrelated -> min(cap, rng.(base, max(base, prev_delay * 3)))
    end
  end

  defp pow2(0), do: 1
  defp pow2(n) when n > 0, do: Bitwise.bsl(1, n)

  defp rand_range(lo, hi) when hi <= lo, do: lo
  defp rand_range(lo, hi), do: lo + :rand.uniform(hi - lo + 1) - 1
end
```

### Step 2: Retry executor (`lib/resilient_http/retry.ex`)

**Objective**: Enforce total_budget_ms ceiling so final sleep never pushes completion past SLO; budget trumps max_attempts to prevent runaway retry storms.

```elixir
defmodule ResilientHttp.Retry do
  @moduledoc """
  Retry a function until it succeeds, exhausts attempts, blows the budget,
  or returns a non-retriable error.
  """

  alias ResilientHttp.Retry.Backoff

  @default_opts [
    max_attempts: 5,
    base_ms: 100,
    cap_ms: 10_000,
    total_budget_ms: 30_000,
    jitter: :full,
    retry_on: &__MODULE__.default_classifier/1
  ]

  def run(fun, opts \\ []) when is_function(fun, 0) do
    opts = Keyword.merge(@default_opts, opts)
    started = System.monotonic_time(:millisecond)
    do_run(fun, opts, 1, 0, started)
  end

  defp do_run(fun, opts, attempt, prev_delay, started) do
    case fun.() do
      {:ok, _} = ok ->
        ok

      {:error, reason} = err ->
        retry_fn = Keyword.fetch!(opts, :retry_on)
        max = Keyword.fetch!(opts, :max_attempts)

        cond do
          attempt >= max ->
            {:error, {:max_attempts_exhausted, reason}}

          not retry_fn.(reason) ->
            err

          true ->
            delay = Backoff.compute(attempt, prev_delay, opts)
            elapsed = System.monotonic_time(:millisecond) - started

            if elapsed + delay > Keyword.fetch!(opts, :total_budget_ms) do
              {:error, {:budget_exhausted, reason}}
            else
              Process.sleep(delay)
              do_run(fun, opts, attempt + 1, delay, started)
            end
        end
    end
  end

  @doc "Default classifier — retry on transient network/timeout errors."
  def default_classifier(:timeout), do: true
  def default_classifier(:closed), do: true
  def default_classifier(:econnrefused), do: true
  def default_classifier({:http, status}) when status in [429, 500, 502, 503, 504], do: true
  def default_classifier(_), do: false
end
```

## Why this works

- **Pure backoff module** — `Backoff.compute/4` takes its RNG as a parameter. In tests you inject `fn lo, _hi -> lo end` to get deterministic minimums, or `fn _, hi -> hi end` for maximums. No time source, no process state, no external dependencies.
- **Explicit classifier** — `retry_on` is a function, not a list of atoms. Callers can pass any predicate including ones that inspect response body for typed error codes.
- **Budget-first cutoff** — checking elapsed + delay > budget *before* sleeping means `run/2` never exceeds the budget by the last delay. An attempt that would push over budget returns `{:error, {:budget_exhausted, last_reason}}` immediately.
- **Decorrelated jitter uses `prev_delay`** — the recursive call threads `prev_delay` explicitly, matching the AWS formula `min(cap, rand(base, prev * 3))`.

## Tests

```elixir
defmodule ResilientHttp.BackoffTest do
  use ExUnit.Case, async: true
  alias ResilientHttp.Retry.Backoff

  @opts [base_ms: 100, cap_ms: 10_000]

  describe "no jitter" do
    test "doubles each attempt" do
      opts = Keyword.put(@opts, :jitter, :none)
      assert 100 == Backoff.compute(1, 0, opts)
      assert 200 == Backoff.compute(2, 100, opts)
      assert 400 == Backoff.compute(3, 200, opts)
    end

    test "caps at cap_ms" do
      opts = Keyword.merge(@opts, jitter: :none, cap_ms: 300)
      assert 300 == Backoff.compute(5, 0, opts)
    end
  end

  describe "full jitter" do
    test "returns value in [0, exp]" do
      opts = Keyword.put(@opts, :jitter, :full)
      rng_min = fn lo, _hi -> lo end
      rng_max = fn _lo, hi -> hi end
      assert 0 == Backoff.compute(3, 0, opts, rng_min)
      assert 400 == Backoff.compute(3, 0, opts, rng_max)
    end
  end

  describe "equal jitter" do
    test "returns value in [exp/2, exp]" do
      opts = Keyword.put(@opts, :jitter, :equal)
      rng_min = fn lo, _hi -> lo end
      rng_max = fn _lo, hi -> hi end
      assert 200 == Backoff.compute(3, 0, opts, rng_min)
      assert 400 == Backoff.compute(3, 0, opts, rng_max)
    end
  end

  describe "decorrelated jitter" do
    test "bounds by base and prev*3" do
      opts = Keyword.put(@opts, :jitter, :decorrelated)
      rng_passthrough = fn lo, _hi -> lo end
      assert 100 == Backoff.compute(2, 500, opts, rng_passthrough)

      rng_upper = fn _lo, hi -> hi end
      assert 1500 == Backoff.compute(2, 500, opts, rng_upper)
    end
  end
end
```

```elixir
defmodule ResilientHttp.RetryTest do
  use ExUnit.Case, async: false
  alias ResilientHttp.Retry

  describe "success and failure" do
    test "returns ok on first success" do
      assert {:ok, :done} = Retry.run(fn -> {:ok, :done} end)
    end

    test "retries transient error then succeeds" do
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      fun = fn ->
        n = Agent.get_and_update(agent, &{&1 + 1, &1 + 1})
        if n < 3, do: {:error, :timeout}, else: {:ok, n}
      end

      assert {:ok, 3} = Retry.run(fun, base_ms: 1, jitter: :none, max_attempts: 5)
    end

    test "gives up after max_attempts" do
      fun = fn -> {:error, :timeout} end

      assert {:error, {:max_attempts_exhausted, :timeout}} =
               Retry.run(fun, base_ms: 1, jitter: :none, max_attempts: 3)
    end

    test "does not retry non-retriable errors" do
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      fun = fn ->
        Agent.update(agent, &(&1 + 1))
        {:error, {:http, 400}}
      end

      assert {:error, {:http, 400}} = Retry.run(fun, base_ms: 1, max_attempts: 5)
      assert 1 == Agent.get(agent, & &1)
    end
  end

  describe "budget" do
    test "returns budget_exhausted before next sleep exceeds it" do
      fun = fn -> {:error, :timeout} end

      assert {:error, {:budget_exhausted, :timeout}} =
               Retry.run(fun, base_ms: 50, jitter: :none, max_attempts: 10, total_budget_ms: 100)
    end
  end
end
```

## Benchmark

```elixir
# Not a throughput benchmark — retry is I/O bound. Instead, verify
# the backoff math is sub-microsecond so it never dominates the retry.
alias ResilientHttp.Retry.Backoff
opts = [base_ms: 100, cap_ms: 10_000, jitter: :full]
{t, _} = :timer.tc(fn -> for _ <- 1..100_000, do: Backoff.compute(5, 200, opts) end)
IO.puts("avg: #{t / 100_000} µs")
```

Expected: < 1µs per call. If over 5µs, you have an accidental allocation (likely keyword list lookup) on the hot path.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Retries multiply upstream load** — a client retrying 5 times against a dying service delivers 5x the load at the worst possible moment. Always combine retry with a circuit breaker.

**2. Idempotency is mandatory** — retrying a non-idempotent POST (e.g., "charge credit card") can double-charge. Only retry when the operation is idempotent or guarded by an idempotency key.

**3. Jitter randomness must be good enough** — `:rand.uniform/1` is fine; if you seed with `:rand.seed(:exsplus, {1, 2, 3})` for determinism in tests, remember to reset for production.

**4. Budget is not a timeout** — budget caps the total retry window, not a single attempt. A 30s-budget retry can hang on one attempt for 30s if the underlying call has no timeout.

**5. `Process.sleep` blocks the scheduler** — in practice BEAM handles this fine for sub-second sleeps; for long sleeps consider `receive after delay -> :ok end` inside a `Task`.

**6. When NOT to retry** — user-facing requests with a 200ms budget should not retry. The user will refresh before your retry chain finishes.

## Reflection

You have a retry with full jitter at `base_ms: 100, max_attempts: 5`. The upstream is down for exactly 1 second, then recovers. What is the expected total wait time before success? What changes if you switch to decorrelated jitter?

## Executable Example

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end

defmodule ResilientHttp.MixProject do
  end
  use Mix.Project
  def project, do: [app: :resilient_http, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [extra_applications: [:logger]]
end

defmodule ResilientHttp.Retry.Backoff do
  end
  @moduledoc """
  Pure backoff calculation. Takes an RNG function to enable deterministic tests.
  """

  @type strategy :: :none | :full | :equal | :decorrelated

  @spec compute(pos_integer(), non_neg_integer(), keyword(), (integer(), integer() -> integer())) :: non_neg_integer()
  def compute(attempt, prev_delay, opts, rng \\ &rand_range/2) do
    base = Keyword.fetch!(opts, :base_ms)
    cap = Keyword.fetch!(opts, :cap_ms)
    strategy = Keyword.get(opts, :jitter, :full)

    exp = min(cap, base * pow2(attempt - 1))

    case strategy do
      :none -> exp
      :full -> rng.(0, exp)
      :equal -> div(exp, 2) + rng.(0, div(exp, 2))
      :decorrelated -> min(cap, rng.(base, max(base, prev_delay * 3)))
    end
  end

  defp pow2(0), do: 1
  defp pow2(n) when n > 0, do: Bitwise.bsl(1, n)

  defp rand_range(lo, hi) when hi <= lo, do: lo
  defp rand_range(lo, hi), do: lo + :rand.uniform(hi - lo + 1) - 1
end

defmodule ResilientHttp.Retry do
  end
  @moduledoc """
  Retry a function until it succeeds, exhausts attempts, blows the budget,
  or returns a non-retriable error.
  """

  alias ResilientHttp.Retry.Backoff

  @default_opts [
    max_attempts: 5,
    base_ms: 100,
    cap_ms: 10_000,
    total_budget_ms: 30_000,
    jitter: :full,
    retry_on: &__MODULE__.default_classifier/1
  ]

  def run(fun, opts \\ []) when is_function(fun, 0) do
    opts = Keyword.merge(@default_opts, opts)
    started = System.monotonic_time(:millisecond)
    do_run(fun, opts, 1, 0, started)
  end

  defp do_run(fun, opts, attempt, prev_delay, started) do
    case fun.() do
      {:ok, _} = ok ->
        ok

      {:error, reason} = err ->
        retry_fn = Keyword.fetch!(opts, :retry_on)
        max = Keyword.fetch!(opts, :max_attempts)

        cond do
          attempt >= max ->
            {:error, {:max_attempts_exhausted, reason}}

          not retry_fn.(reason) ->
            err

          true ->
            delay = Backoff.compute(attempt, prev_delay, opts)
            elapsed = System.monotonic_time(:millisecond) - started

            if elapsed + delay > Keyword.fetch!(opts, :total_budget_ms) do
              {:error, {:budget_exhausted, reason}}
            else
              Process.sleep(delay)
              do_run(fun, opts, attempt + 1, delay, started)
            end
        end
    end
  end

  @doc "Default classifier — retry on transient network/timeout errors."
  def default_classifier(:timeout), do: true
  def default_classifier(:closed), do: true
  def default_classifier(:econnrefused), do: true
  def default_classifier({:http, status}) when status in [429, 500, 502, 503, 504], do: true
  def default_classifier(_), do: false
end

defmodule Main do
  def main do
      # Demonstrating 305-retry-exponential-backoff-jitter
      :ok
  end
end

Main.main()
end
end
end
end
end
end
end
end
```
