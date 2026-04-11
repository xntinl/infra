# Circuit Breaker Patterns

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` already has a raw `:gen_statem` circuit breaker for the payments service
(exercise 33). Now the operations team wants two additions: a second breaker for the
fraud-scoring service backed by the battle-tested `Fuse` library (less code, telemetry
built-in), and a bulkhead to cap concurrent in-flight calls to any downstream service
so a slow dependency cannot exhaust the gateway's connection pool.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       └── circuit_breaker/
│           ├── breaker.ex          # ← from exercise 33 (gen_statem, keep as-is)
│           ├── fuse_breaker.ex     # ← you implement this (Exercise 1)
│           ├── bulkhead.ex         # ← you implement this (Exercise 2)
│           └── supervisor.ex       # already exists
├── test/
│   └── api_gateway/
│       └── circuit_breaker/
│           ├── fuse_breaker_test.exs    # given tests
│           └── bulkhead_test.exs        # given tests
└── mix.exs
```

---

## The business problem

Two downstream services are now protected:

1. **Payments service** — already guarded by the raw `:gen_statem` breaker from
   exercise 33. Production deployments need better observability without adding code.
   The `Fuse` library ships with telemetry events out of the box.

2. **Fraud-scoring service** — moderate failure rate, needs the same three-state
   protection. The team does not want to maintain a second hand-rolled state machine.

3. **Connection pool exhaustion** — both payment and fraud services can be slow under
   load. Even with circuit breakers, a burst of concurrent calls can fill the gateway's
   connection pool before the breaker opens. The bulkhead limits how many calls can be
   in-flight at the same time.

---

## Why Fuse over a custom `:gen_statem` breaker

Both approaches implement the same state machine. The trade-off:

| Aspect | Raw `:gen_statem` (exercise 33) | Fuse library |
|--------|--------------------------------|--------------|
| Code to maintain | ~80 lines | ~0 (library) |
| Telemetry events | Manual | Built-in (`[:fuse, :circuit_breaker, :open]` etc.) |
| Threshold config | Custom struct | `{:standard, N, window_ms}` |
| Test reset | Not built-in | `:fuse.reset/1` |
| Dependency | None (OTP) | `{:fuse, "~> 2.4"}` |
| When to choose | Custom logic, no deps preferred | Production use, telemetry needed |

---

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    {:fuse, "~> 2.4"},
    # existing deps...
  ]
end
```

### Step 2: `lib/api_gateway/circuit_breaker/fuse_breaker.ex`

The FuseBreaker delegates all state management to the Fuse library. `install/1` registers
a named fuse with threshold and reset configuration. `call/2` checks whether the circuit
is open before executing the function — if open, it returns immediately without calling
the function. On failure, `:fuse.melt/1` records the failure, and Fuse internally tracks
whether the threshold has been reached.

```elixir
defmodule ApiGateway.CircuitBreaker.FuseBreaker do
  @moduledoc """
  Circuit breaker for the fraud-scoring service, backed by the Fuse library.

  Fuse manages the :closed / :open / :half_open state machine internally.
  This module provides the gateway's integration layer:
    - install/1    — register a named fuse with threshold + reset config
    - call/2       — execute a function under the breaker; record failures
    - state/1      — query current circuit state

  Fuse configuration format:
    {{:standard, max_failures, window_ms}, {:reset, reset_ms}}

  max_failures failures in window_ms -> circuit opens.
  After reset_ms -> Fuse allows one probe (:half_open).
  """

  @type fuse_name :: atom()

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc "Install a named fuse. Safe to call multiple times (idempotent)."
  @spec install(fuse_name(), keyword()) :: :ok
  def install(name, opts \\ []) do
    threshold  = Keyword.get(opts, :threshold, 5)
    window_ms  = Keyword.get(opts, :window_ms, 10_000)
    reset_ms   = Keyword.get(opts, :reset_ms, 30_000)

    case :fuse.install(name, {{:standard, threshold, window_ms}, {:reset, reset_ms}}) do
      :ok -> :ok
      {:error, :already_installed} -> :ok
    end
  end

  @doc """
  Execute `fun` under the named circuit breaker.

  Returns:
    - `{:ok, result}` when the circuit is closed and the call succeeds
    - `{:error, reason}` when the call fails (also records the failure in Fuse)
    - `{:error, :circuit_open}` when the circuit is open (fun is NOT called)
  """
  @spec call(fuse_name(), (-> term())) :: {:ok, term()} | {:error, term()}
  def call(name, fun) when is_function(fun, 0) do
    case :fuse.ask(name, :sync) do
      :ok ->
        case safe_call(fun) do
          {:ok, _} = result ->
            result

          {:error, _} = error ->
            :fuse.melt(name)
            error
        end

      :blown ->
        {:error, :circuit_open}
    end
  end

  @doc "Query the current circuit state."
  @spec state(fuse_name()) :: :ok | :blown | {:error, term()}
  def state(name) do
    case :fuse.circuit_state(name) do
      :ok -> :ok
      :blown -> :blown
      {:error, _} = error -> error
    end
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp safe_call(fun) do
    try do
      case fun.() do
        {:ok, _} = ok   -> ok
        {:error, _} = e -> e
        other           -> {:ok, other}
      end
    rescue
      e -> {:error, Exception.message(e)}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end
end
```

### Step 3: `lib/api_gateway/circuit_breaker/bulkhead.ex`

The bulkhead pattern limits concurrent in-flight requests to a downstream service.
The GenServer serializes acquire/release operations, maintaining an exact count of
in-flight calls. When the count reaches `max_concurrent`, new requests are rejected
immediately (fail fast) rather than queued.

The `run/2` function uses `try/after` to guarantee that the slot is released even if the
user function raises an exception. The `release` operation is a cast (fire-and-forget)
because the caller does not need to wait for confirmation.

```elixir
defmodule ApiGateway.CircuitBreaker.Bulkhead do
  @moduledoc """
  Concurrency limiter (bulkhead pattern) for downstream service calls.

  Keeps a count of in-flight requests per service. If the count reaches
  max_concurrent, new requests are rejected immediately (fail fast) rather
  than queued — queuing under backpressure creates latency accumulation.

  Usage:
    Bulkhead.run(:fraud_scorer, fn -> FraudScorer.score(payload) end)
    # => {:ok, result} | {:error, :at_capacity} | {:error, reason}

  The GenServer serializes acquire/release, so the counter is exact.
  For very high throughput, replace with :atomics-based semaphore.
  """
  use GenServer

  defstruct [:name, :max_concurrent, :current]

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    max  = Keyword.get(opts, :max_concurrent, 10)
    GenServer.start_link(__MODULE__, {name, max}, name: name)
  end

  @doc "Run `fun` inside the bulkhead. Returns {:error, :at_capacity} if full."
  @spec run(atom(), (-> term())) :: {:ok, term()} | {:error, term()}
  def run(name, fun) when is_function(fun, 0) do
    case acquire(name) do
      :ok ->
        try do
          case fun.() do
            {:ok, _} = ok   -> ok
            {:error, _} = e -> e
            other           -> {:ok, other}
          end
        after
          release(name)
        end

      {:error, :at_capacity} = err ->
        err
    end
  end

  @spec stats(atom()) :: map()
  def stats(name), do: GenServer.call(name, :stats)

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init({name, max_concurrent}) do
    {:ok, %__MODULE__{name: name, max_concurrent: max_concurrent, current: 0}}
  end

  @impl true
  def handle_call(:acquire, _from, %{current: current, max_concurrent: max} = state)
      when current < max do
    {:reply, :ok, %{state | current: current + 1}}
  end

  @impl true
  def handle_call(:acquire, _from, state) do
    {:reply, {:error, :at_capacity}, state}
  end

  @impl true
  def handle_call(:stats, _from, state) do
    stats = %{
      name:           state.name,
      max_concurrent: state.max_concurrent,
      in_flight:      state.current,
      available:      state.max_concurrent - state.current
    }
    {:reply, stats, state}
  end

  @impl true
  def handle_cast(:release, %{current: current} = state) do
    {:noreply, %{state | current: max(0, current - 1)}}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp acquire(name), do: GenServer.call(name, :acquire)
  defp release(name), do: GenServer.cast(name, :release)
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/circuit_breaker/fuse_breaker_test.exs
defmodule ApiGateway.CircuitBreaker.FuseBreakerTest do
  use ExUnit.Case, async: true

  alias ApiGateway.CircuitBreaker.FuseBreaker

  defp fuse_name do
    :"fuse_#{System.unique_integer([:positive])}"
  end

  test "install is idempotent" do
    name = fuse_name()
    assert :ok = FuseBreaker.install(name)
    assert :ok = FuseBreaker.install(name)
  end

  test "closed circuit executes the function" do
    name = fuse_name()
    FuseBreaker.install(name, threshold: 5, window_ms: 10_000, reset_ms: 60_000)

    assert {:ok, :result} = FuseBreaker.call(name, fn -> {:ok, :result} end)
  end

  test "failure is recorded and circuit opens at threshold" do
    name = fuse_name()
    FuseBreaker.install(name, threshold: 3, window_ms: 10_000, reset_ms: 60_000)

    for _ <- 1..3 do
      FuseBreaker.call(name, fn -> {:error, :down} end)
    end

    assert {:error, :circuit_open} =
      FuseBreaker.call(name, fn -> {:ok, :never_called} end)
  end

  test "open circuit does not call the function" do
    name = fuse_name()
    FuseBreaker.install(name, threshold: 1, window_ms: 10_000, reset_ms: 60_000)
    FuseBreaker.call(name, fn -> {:error, :down} end)

    called = :counters.new(1, [])
    FuseBreaker.call(name, fn ->
      :counters.add(called, 1, 1)
      {:ok, :never}
    end)

    assert :counters.get(called, 1) == 0
  end

  test "state/1 returns :ok when circuit is healthy" do
    name = fuse_name()
    FuseBreaker.install(name, threshold: 5)
    assert FuseBreaker.state(name) == :ok
  end

  test "exceptions in the function are caught and recorded as failures" do
    name = fuse_name()
    FuseBreaker.install(name, threshold: 1)

    result = FuseBreaker.call(name, fn -> raise "boom" end)
    assert {:error, _reason} = result

    # Circuit should now be open (threshold 1)
    assert {:error, :circuit_open} =
      FuseBreaker.call(name, fn -> {:ok, :should_not_run} end)
  end
end
```

```elixir
# test/api_gateway/circuit_breaker/bulkhead_test.exs
defmodule ApiGateway.CircuitBreaker.BulkheadTest do
  use ExUnit.Case, async: true

  alias ApiGateway.CircuitBreaker.Bulkhead

  defp start_bulkhead(max) do
    name = :"bh_#{System.unique_integer([:positive])}"
    start_supervised!({Bulkhead, [name: name, max_concurrent: max]})
    name
  end

  test "allows requests up to max_concurrent" do
    name = start_bulkhead(3)
    parent = self()

    # Start 3 tasks that hold a slot for a moment
    tasks = Enum.map(1..3, fn _ ->
      Task.async(fn ->
        Bulkhead.run(name, fn ->
          send(parent, :slot_acquired)
          Process.sleep(50)
          {:ok, :done}
        end)
      end)
    end)

    # All 3 should acquire slots
    for _ <- 1..3, do: assert_receive(:slot_acquired, 500)

    Task.await_many(tasks)
  end

  test "rejects when at capacity" do
    name = start_bulkhead(1)
    parent = self()
    gate = self()

    # Hold one slot
    holder = Task.async(fn ->
      Bulkhead.run(name, fn ->
        send(parent, :holding)
        receive do: (:release -> {:ok, :done})
      end)
    end)

    assert_receive(:holding, 500)

    # Second request should be rejected
    assert {:error, :at_capacity} = Bulkhead.run(name, fn -> {:ok, :never} end)

    send(gate, :release)
    Task.await(holder)
  end

  test "slot is released even when function raises" do
    name = start_bulkhead(1)

    catch_error(Bulkhead.run(name, fn -> raise "oops" end))

    # Slot must be freed — next call should succeed
    stats = Bulkhead.stats(name)
    assert stats.in_flight == 0
  end

  test "stats reflect current in-flight count" do
    name = start_bulkhead(5)
    stats = Bulkhead.stats(name)
    assert stats.max_concurrent == 5
    assert stats.in_flight == 0
    assert stats.available == 5
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/circuit_breaker/fuse_breaker_test.exs --trace
mix test test/api_gateway/circuit_breaker/bulkhead_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Fuse library | Raw `:gen_statem` (exercise 33) |
|--------|-------------|--------------------------------|
| Lines of circuit-breaker code | ~15 | ~80 |
| Telemetry events | Built-in | Manual |
| Threshold algorithm | Standard + monotone | Custom |
| Test reset | `:fuse.reset/1` | Not built-in |
| External dependency | Yes | No (OTP only) |
| Custom state logic | Not possible | Full control |

| Aspect | Bulkhead (concurrency limit) | Circuit breaker (error rate) |
|--------|-----------------------------|-----------------------------|
| Protects against | Slow services exhausting pool | Failing services cascading |
| Trigger | In-flight count | Failure count in window |
| Fast-fail | Always when full | Only when open |
| Recovery | Automatic (slots freed) | Timeout then probe |
| Combined | Use both together | Use both together |

Reflection: the bulkhead limits concurrency; the circuit breaker limits failure rate.
When would you need one but not the other?

---

## Common production mistakes

**1. Calling `:fuse.melt/1` on success**
`melt` records a failure. Calling it unconditionally (before checking the result)
opens the circuit on successful calls. Only melt on `{:error, _}`.

**2. Bulkhead queuing instead of fail-fast**
If you queue callers when the bulkhead is full, latency accumulates: 100 callers
waiting 500ms each means the last caller waits 50 seconds. Fail fast and let the
caller decide to retry, shed load, or return a degraded response.

**3. Threshold too low in production**
A threshold of 1 opens the circuit on the first transient error. Calibrate threshold
against your service's baseline error rate. A rule of thumb: `threshold > baseline_errors_per_window`.

**4. Not combining circuit breaker with bulkhead**
A circuit breaker protects against cascading failures from error rates. A bulkhead
protects against slow-but-not-failing services that fill the connection pool. You need
both for complete downstream service protection.

**5. Not resetting Fuse in tests**
Fuse state persists across tests in the same BEAM session. Use `:fuse.reset/1` in
`on_exit` or use unique fuse names per test (preferred — avoids global state).

---

## Resources

- [Fuse — GitHub](https://github.com/jlouis/fuse)
- [Fuse — HexDocs](https://hexdocs.pm/fuse/fuse.html)
- [Circuit Breaker pattern — Martin Fowler](https://martinfowler.com/bliki/CircuitBreaker.html)
- [Bulkhead pattern — Microsoft](https://docs.microsoft.com/en-us/azure/architecture/patterns/bulkhead)
- [Release It! — Michael Nygard](https://pragprog.com/titles/mnee2/release-it-second-edition/)
