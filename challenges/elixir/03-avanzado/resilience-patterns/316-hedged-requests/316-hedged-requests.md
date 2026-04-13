# Hedged Requests

**Project**: `search_hedger` — reduces tail latency on search queries by firing a second duplicate request after a delay; first successful response wins and the loser is cancelled.

## Project context

Your search API calls a backend that normally responds in 20ms but has a long tail: 1 in 1000 requests takes 500ms due to GC pause, cache miss, or a slow replica. At high volume, p99.9 is dominated by those laggards.

Hedged requests (Jeff Dean, "The Tail at Scale") issue a second request to a *different backend* (or replica) after a short delay. The first successful response wins; the other is cancelled. This turns a p99 of 500ms into roughly `min(p50, p50 + delay)`.

The trick: do not fire the hedge on every request (would double upstream load). Fire only after waiting `delay_ms`. For typical Gaussian tails with `delay = p95`, you hedge ~5% of requests and cut tail latency dramatically.

```
search_hedger/
├── lib/
│   └── search_hedger/
│       ├── hedger.ex
│       └── backend.ex              # simulated backend with controlled latency
├── test/
│   └── search_hedger/
│       └── hedger_test.exs
├── bench/
│   └── hedger_bench.exs
└── mix.exs
```

## Why hedging and not retry

Retry fires a second request *after* the first has failed (or timed out). Hedging fires *before* the first is known to have failed. Retry waits for bad news; hedging preempts it.

## Why not fire both in parallel always

Doubling load to halve tail latency is a bad trade at scale. Firing after `delay = p95` means only the slowest 5% generate a hedge. Upstream load increases by ~5%, tail latency drops by > 90%.

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
### 1. Delay-triggered fan-out
```
t=0     fire req_A
t=20    hedge timer fires → fire req_B
t=25    req_A responds → return, cancel req_B
```
Or:
```
t=0     fire req_A
t=20    hedge → fire req_B
t=22    req_B responds (faster) → return, cancel req_A
```

### 2. "First wins" pattern
`Task.yield_many/2` or explicit `receive` on both Tasks' refs. First `{ref, {:ok, _}}` message wins; we demonitor/shutdown the other.

### 3. Bounded by overall timeout
Even with hedging, total time is capped by `max_timeout`. If both hedges time out, return `{:error, :timeout}`.

## Design decisions

- **Option A — Fire N hedges at fixed intervals**: "after 20ms, 40ms, 60ms fire again". Cuts tail further, amplifies load more.
- **Option B — Fire one hedge**: simplest, catches most of the benefit. Google's data shows 1 hedge at p95 captures most of the tail reduction.
→ Chose **B**. One hedge is the common case.

- **Option A — Cancellation via `Task.shutdown/2`**: proper shutdown, may wait.
- **Option B — `Process.exit(pid, :kill)`**: instant, brutal.
→ Chose **A** (`:brutal_kill` via `shutdown/2` with `:brutal_kill` arg). Fast enough and well-supervised.

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
defmodule SearchHedger.MixProject do
  use Mix.Project
  def project, do: [app: :search_hedger, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
  def application, do: [extra_applications: [:logger]]
  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 1: Simulated backend

**Objective**: Provide closure API with controllable latency and failures so hedging tests are deterministic without real network variability or GC injection.

```elixir
defmodule SearchHedger.Backend do
  @doc """
  Simulates a backend with controllable per-attempt latency.
  Returns a function that, when called, sleeps then returns the given value.
  """
  def with_latency(latency_ms, value) do
    fn ->
      Process.sleep(latency_ms)
      {:ok, value}
    end
  end

  def failing(latency_ms, reason) do
    fn ->
      Process.sleep(latency_ms)
      {:error, reason}
    end
  end
end
```

### Step 2: Hedger (`lib/search_hedger/hedger.ex`)

**Objective**: Fire second Task only after hedge_after_ms delay so 5% of slow requests spawn hedge, cutting p99.9 without 2x amplification on fast-path (95% of traffic).

```elixir
defmodule SearchHedger.Hedger do
  @doc """
  Executes `fun` once, and if no response arrives within `hedge_after_ms`,
  fires a second copy. Returns the first successful result.

  `fun` must be a 0-arity function returning `{:ok, _}` or `{:error, _}`.
  """
  def run(fun, opts) when is_function(fun, 0) do
    hedge_after = Keyword.fetch!(opts, :hedge_after_ms)
    timeout = Keyword.fetch!(opts, :timeout_ms)

    task_primary = Task.async(fun)
    deadline = System.monotonic_time(:millisecond) + timeout

    case wait_primary(task_primary, hedge_after, deadline) do
      {:ok, result} ->
        result

      :hedge ->
        task_hedge = Task.async(fun)
        await_first_success([task_primary, task_hedge], deadline)
    end
  end

  defp wait_primary(task, hedge_after, deadline) do
    remaining = min(hedge_after, max(0, deadline - System.monotonic_time(:millisecond)))

    receive do
      {ref, result} when ref == task.ref ->
        Process.demonitor(ref, [:flush])
        {:ok, result}
    after
      remaining -> :hedge
    end
  end

  defp await_first_success(tasks, deadline) do
    remaining = max(0, deadline - System.monotonic_time(:millisecond))

    receive do
      {ref, {:ok, _} = ok} ->
        finish(tasks, ref)
        ok

      {ref, {:error, _} = err} ->
        remaining_tasks = Enum.reject(tasks, &(&1.ref == ref))
        Process.demonitor(ref, [:flush])

        case remaining_tasks do
          [] -> err
          [_ | _] -> await_first_success(remaining_tasks, deadline)
        end

      {:DOWN, ref, :process, _, _} ->
        remaining_tasks = Enum.reject(tasks, &(&1.ref == ref))
        case remaining_tasks do
          [] -> {:error, :both_down}
          [_ | _] -> await_first_success(remaining_tasks, deadline)
        end
    after
      remaining ->
        Enum.each(tasks, &Task.shutdown(&1, :brutal_kill))
        {:error, :timeout}
    end
  end

  defp finish(tasks, winner_ref) do
    Enum.each(tasks, fn t ->
      if t.ref != winner_ref do
        Task.shutdown(t, :brutal_kill)
      else
        Process.demonitor(t.ref, [:flush])
      end
    end)
  end
end
```

## Why this works

- **Primary completes first → no hedge fired** — the common case: `wait_primary` returns `{:ok, result}` before the `hedge_after_ms` timeout. Zero upstream load increase.
- **Primary slow → hedge fires** — `wait_primary` returns `:hedge`, a second Task is spawned. We then `receive` on both refs and return whichever completes first with `{:ok, _}`.
- **Error is not a win** — an `:error` reply from one task doesn't terminate the race; we keep waiting for the other in case it succeeds. Only if *both* error do we return an error.
- **Cancellation via `Task.shutdown(:brutal_kill)`** — sends `Process.exit(pid, :kill)` directly; the loser is killed within microseconds. No wasted upstream work after we have a winner.
- **`Process.demonitor/2` with `:flush`** — cleans the mailbox of leftover `{:DOWN, ...}` messages from the cancelled task so the caller isn't polluted.

## Tests

```elixir
defmodule SearchHedger.HedgerTest do
  use ExUnit.Case, async: true
  alias SearchHedger.{Backend, Hedger}

  describe "fast primary" do
    test "returns primary result without hedging" do
      fun = Backend.with_latency(10, :primary)

      {time, result} =
        :timer.tc(fn ->
          Hedger.run(fun, hedge_after_ms: 100, timeout_ms: 500)
        end)

      assert {:ok, :primary} = result
      assert time < 50_000
    end
  end

  describe "slow primary triggers hedge" do
    test "hedge returns first" do
      # every invocation takes 40ms; after 20ms a second one fires;
      # the hedge will finish at t=60ms, primary at t=40ms, so primary still wins
      fun = Backend.with_latency(40, :ok_value)

      {:ok, :ok_value} = Hedger.run(fun, hedge_after_ms: 20, timeout_ms: 500)
    end

    test "faster of two wins when calls have randomized latency" do
      {:ok, agent} = Agent.start_link(fn -> [100, 10] end)

      fun = fn ->
        [latency | rest] = Agent.get_and_update(agent, fn l -> {l, tl(l) ++ [hd(l)]} end) |> List.wrap()
        Process.sleep(latency)
        {:ok, latency}
      end

      assert {:ok, _} = Hedger.run(fun, hedge_after_ms: 30, timeout_ms: 500)
    end
  end

  describe "both time out" do
    test "returns :timeout" do
      fun = Backend.with_latency(1_000, :ok)

      assert {:error, :timeout} = Hedger.run(fun, hedge_after_ms: 10, timeout_ms: 50)
    end
  end

  describe "primary errors, hedge succeeds" do
    test "returns hedge result" do
      {:ok, agent} = Agent.start_link(fn -> 0 end)

      fun = fn ->
        n = Agent.get_and_update(agent, &{&1, &1 + 1})

        if n == 0 do
          Process.sleep(5)
          {:error, :boom}
        else
          Process.sleep(20)
          {:ok, :hedge_win}
        end
      end

      assert {:ok, :hedge_win} = Hedger.run(fun, hedge_after_ms: 10, timeout_ms: 500)
    end
  end
end
```

## Benchmark

```elixir
# bench/hedger_bench.exs
alias SearchHedger.{Backend, Hedger}

Benchee.run(
  %{
    "no-hedge fast primary" => fn ->
      Hedger.run(Backend.with_latency(5, :ok), hedge_after_ms: 50, timeout_ms: 200)
    end,
    "hedge fires" => fn ->
      Hedger.run(Backend.with_latency(40, :ok), hedge_after_ms: 10, timeout_ms: 200)
    end
  },
  time: 5,
  warmup: 2
)
```

Expected: no-hedge path dominated by the 5ms sleep; hedge path dominated by 40ms. Overhead added by hedger itself should be < 100µs.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Hedging amplifies upstream load** — at `hedge_after = p95` you add ~5% load. At `p50` you'd double it. Set `hedge_after_ms >= p95_ms` to keep amplification bounded.

**2. Non-idempotent operations cannot hedge** — POST /charge cannot be hedged; you risk double-charging. Hedge only reads, queries, lookups.

**3. Shared connection pool** — if both requests go through the same pool, you've doubled pool contention. Route hedges to a different pool or different replica.

**4. Winner cancellation isn't free** — `Task.shutdown(:brutal_kill)` sends an exit, but the upstream connection may still complete the request before it notices the client disconnect. Upstream expends work for nothing.

**5. Cancelled responses can arrive anyway** — after `demonitor(ref, [:flush])` the message is drained, but between arrival and flush it sat in your mailbox. Cost is memory.

**6. When NOT to hedge** — low-volume endpoints where the tail is already acceptable. Hedging adds complexity; only deploy where observed p99 latency justifies it.

## Reflection

You set `hedge_after_ms: 20` based on measured p95. Three months later, the p95 drifts to 80ms. What's your observable symptom, and what metric should alert?

## Executable Example

```elixir
defmodule Main do
  defp deps do
    [
      # No external dependencies — pure Elixir
    ]
  end

  defmodule SearchHedger.MixProject do
    use Mix.Project
    def project, do: [app: :search_hedger, version: "0.1.0", elixir: "~> 1.17", deps: deps()]
    def application, do: [extra_applications: [:logger]]
    defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
  end

  defmodule SearchHedger.Backend do
    @doc """
    Simulates a backend with controllable per-attempt latency.
    Returns a function that, when called, sleeps then returns the given value.
    """
    def with_latency(latency_ms, value) do
      fn ->
        Process.sleep(latency_ms)
        {:ok, value}
      end
    end

    def failing(latency_ms, reason) do
      fn ->
        Process.sleep(latency_ms)
        {:error, reason}
      end
    end
  end

  defmodule SearchHedger.Hedger do
    @doc """
    Executes `fun` once, and if no response arrives within `hedge_after_ms`,
    fires a second copy. Returns the first successful result.

    `fun` must be a 0-arity function returning `{:ok, _}` or `{:error, _}`.
    """
    def run(fun, opts) when is_function(fun, 0) do
      hedge_after = Keyword.fetch!(opts, :hedge_after_ms)
      timeout = Keyword.fetch!(opts, :timeout_ms)

      task_primary = Task.async(fun)
      deadline = System.monotonic_time(:millisecond) + timeout

      case wait_primary(task_primary, hedge_after, deadline) do
        {:ok, result} ->
          result

        :hedge ->
          task_hedge = Task.async(fun)
          await_first_success([task_primary, task_hedge], deadline)
      end
    end

    defp wait_primary(task, hedge_after, deadline) do
      remaining = min(hedge_after, max(0, deadline - System.monotonic_time(:millisecond)))

      receive do
        {ref, result} when ref == task.ref ->
          Process.demonitor(ref, [:flush])
          {:ok, result}
      after
        remaining -> :hedge
      end
    end

    defp await_first_success(tasks, deadline) do
      remaining = max(0, deadline - System.monotonic_time(:millisecond))

      receive do
        {ref, {:ok, _} = ok} ->
          finish(tasks, ref)
          ok

        {ref, {:error, _} = err} ->
          remaining_tasks = Enum.reject(tasks, &(&1.ref == ref))
          Process.demonitor(ref, [:flush])

          case remaining_tasks do
            [] -> err
            [_ | _] -> await_first_success(remaining_tasks, deadline)
          end

        {:DOWN, ref, :process, _, _} ->
          remaining_tasks = Enum.reject(tasks, &(&1.ref == ref))
          case remaining_tasks do
            [] -> {:error, :both_down}
            [_ | _] -> await_first_success(remaining_tasks, deadline)
          end
      after
        remaining ->
          Enum.each(tasks, &Task.shutdown(&1, :brutal_kill))
          {:error, :timeout}
      end
    end

    defp finish(tasks, winner_ref) do
      Enum.each(tasks, fn t ->
        if t.ref != winner_ref do
          Task.shutdown(t, :brutal_kill)
        else
          Process.demonitor(t.ref, [:flush])
        end
      end)
    end
  end

  defmodule Main do
    def main do
        # Demonstrating 316-hedged-requests
        :ok
    end
  end
end

Main.main()
```
