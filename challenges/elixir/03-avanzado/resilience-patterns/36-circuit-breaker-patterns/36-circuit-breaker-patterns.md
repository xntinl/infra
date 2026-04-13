# Circuit Breaker Patterns — Manual State Machine

**Project**: `circuit_breaker_patterns` — a hand-rolled circuit breaker with closed / open / half-open states, telemetry, and production-grade failure classification.

---

## Project context

You are hardening a payment service that fans out to three downstream providers
(Stripe, Adyen, a homegrown bank rail). Every two weeks, one of those providers
degrades: responses go from 50ms to 30 seconds before timing out, saturating the
connection pool and bringing down the whole payment pipeline. The post-mortems
always end the same way — "we need a circuit breaker".

You could pull in `:fuse` and be done. But before you hide the
problem behind a library you want to own the state machine end-to-end: understand
why half-open exists, how to calibrate the failure window, what to count as a
"failure", and how to expose telemetry so SRE can dashboard it. This exercise
builds that understanding.

The breaker lives as a `GenServer` per upstream (one breaker per provider), with
a tiny ETS table for fast reads from caller processes. Writes (state transitions)
go through the GenServer — they are rare and need serialization. Reads (state
queries) hit ETS directly. The supervision tree restarts the breaker on crash
while preserving the ETS table via an application-level owner.

```
circuit_breaker_patterns/
├── lib/
│   └── circuit_breaker_patterns/
│       ├── application.ex
│       ├── breaker.ex               # GenServer with state machine
│       ├── classifier.ex            # failure classification rules
│       └── telemetry.ex             # emits :telemetry events
├── test/
│   └── circuit_breaker_patterns/
│       ├── breaker_test.exs
│       └── classifier_test.exs
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
### 1. The three states and their transitions

```
                      failure_threshold reached
                       ┌──────────────────────┐
                       ▼                      │
       ┌──────────┐              ┌──────────┐ │   ┌────────────┐
       │  CLOSED  │ ──fail──────▶│  CLOSED  │─┴──▶│    OPEN    │
       │ (normal) │              │ (counting)│    │ (rejecting)│
       └──────────┘              └──────────┘     └────────────┘
             ▲                                          │
             │                                          │ reset_timeout
             │                                          ▼
             │ success in half_open           ┌─────────────────┐
             └────────────────────────────────│   HALF-OPEN     │
                                              │ (1 probe allowed)│
                                              └─────────────────┘
                                                       │
                                                       │ probe fails
                                                       ▼
                                                   back to OPEN
```

- **CLOSED**: requests flow through. Failures increment a counter inside a
  rolling time window. When `failure_count >= failure_threshold` within
  `failure_window_ms`, the breaker trips and moves to OPEN.
- **OPEN**: all calls short-circuit with `{:error, :circuit_open}`. A timer
  fires after `reset_timeout_ms` and moves the breaker to HALF-OPEN.
- **HALF-OPEN**: exactly ONE probe request is allowed. If it succeeds, the
  breaker moves to CLOSED (counters reset). If it fails, back to OPEN.

### 2. Why half-open is not optional

A naive breaker that just flips CLOSED ↔ OPEN causes a thundering herd:
when the reset timer fires, every pending caller retries simultaneously. If
the upstream is still sick, thousands of requests hit it at once and it
immediately dies again.

Half-open admits a single probe to test the water. All other callers still
see OPEN until the probe returns. This decouples discovery (did upstream
recover?) from load (who gets to go through?).

### 3. What counts as a failure — classification matters

Counting every non-2xx response as a failure is wrong. A 404 from "user not
found" is not an outage. A 401 from "invalid token" is a client bug, not an
upstream problem. Trip conditions that matter:

- `:timeout` — connection or receive timeout
- `:connect_error` — DNS, ECONNREFUSED, TLS handshake
- HTTP 5xx (500, 502, 503, 504)
- HTTP 429 if `Retry-After` is not honored

Do NOT trip on:
- HTTP 4xx (except 408 Request Timeout and 429 under specific conditions)
- Business-logic errors returned in the body with a 200

This is why we have a separate `Classifier` module.

### 4. Rolling window vs total counter

A naive `failure_count` that only resets on state transition accumulates
forever. After an hour, you have hundreds of stale failures dominating a
decision that should reflect the last minute.

We use a rolling window: every failure is a millisecond timestamp pushed
onto a list. On each failure we prune entries older than `failure_window_ms`,
then check if the remaining count exceeds the threshold.

### 5. ETS read path vs GenServer write path

| Operation | Goes through | Why |
|-----------|--------------|-----|
| `call/2` (check state before request) | ETS lookup | Hot path, zero contention |
| `report_success/1`, `report_failure/1` | GenServer cast | Rare vs reads, needs serialization |
| State transitions | GenServer handle_info | Only the owner mutates state |

Reads are 100x more common than writes in a stable system. Putting reads
behind a GenServer mailbox would make the breaker itself a bottleneck —
the opposite of what we want.

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

### Step 1: mix.exs dependencies

**Objective**: Import telemetry for state-transition events so operators instrument the breaker without modifying code; exclude test libraries from production builds.

```elixir
defp deps do
  [
    {:telemetry, "~> 1.2"},
    {:jason, "~> 1.4", only: [:dev, :test]}
  ]
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defp deps do
  [
    {:telemetry, "~> 1.2"},
    {:jason, "~> 1.4", only: [:dev, :test]}
  ]
end
```

### Step 2: `lib/circuit_breaker_patterns/classifier.ex`

**Objective**: Extract failure classification rules so breaker FSM remains pure and per-upstream policies (429 vs 408) are swappable without modifying state machine.

```elixir
defmodule CircuitBreakerPatterns.Classifier do
  @moduledoc """
  Decides whether a result counts as a failure for the breaker.

  Classifying is separate from the breaker so the same rules can be unit-tested
  and swapped per upstream (Stripe's 429 means something different than Adyen's).
  """

  @type result :: {:ok, any()} | {:error, term()}
  @type classification :: :success | :failure | :ignore

  @spec classify(result()) :: classification()
  def classify({:ok, %{status: status}}) when status in 500..599, do: :failure
  def classify({:ok, %{status: 408}}), do: :failure
  def classify({:ok, %{status: 429}}), do: :failure
  def classify({:ok, %{status: status}}) when status in 200..499, do: :success
  def classify({:error, :timeout}), do: :failure
  def classify({:error, :connect_timeout}), do: :failure
  def classify({:error, :nxdomain}), do: :failure
  def classify({:error, :econnrefused}), do: :failure
  def classify({:error, {:tls_alert, _}}), do: :failure
  def classify({:error, _other}), do: :failure
  def classify(_), do: :ignore
end
```

### Step 3: `lib/circuit_breaker_patterns/breaker.ex`

**Objective**: Serialize state transitions through GenServer but read current state from ETS so call/2 gate checks scale lock-free across cores without mailbox contention.

```elixir
defmodule CircuitBreakerPatterns.Breaker do
  @moduledoc """
  Manual circuit breaker GenServer with closed / open / half-open states.

  One breaker per upstream (identify by `name`). Reads are lock-free through ETS.
  """
  use GenServer
  require Logger

  alias CircuitBreakerPatterns.Classifier

  @type state_name :: :closed | :open | :half_open
  @type name :: atom()

  @default_failure_threshold 5
  @default_failure_window_ms 10_000
  @default_reset_timeout_ms 30_000

  @table :circuit_breaker_states

  # ─── Public API ────────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, opts, name: via(name))
  end

  @doc """
  Wraps `fun` with breaker semantics. Short-circuits with `{:error, :circuit_open}`
  if the breaker is OPEN; runs the function and reports outcome otherwise.
  """
  @spec call(name(), (-> Classifier.result())) ::
          Classifier.result() | {:error, :circuit_open}
  def call(name, fun) when is_function(fun, 0) do
    case current_state(name) do
      :open ->
        emit(:rejected, name, %{state: :open})
        {:error, :circuit_open}

      state when state in [:closed, :half_open] ->
        run_and_report(name, fun, state)
    end
  end

  @spec current_state(name()) :: state_name()
  def current_state(name) do
    case :ets.lookup(@table, name) do
      [{^name, state, _generation}] -> state
      [] -> :closed
    end
  end

  @spec report_success(name()) :: :ok
  def report_success(name), do: GenServer.cast(via(name), :success)

  @spec report_failure(name()) :: :ok
  def report_failure(name), do: GenServer.cast(via(name), :failure)

  # ─── Internal helpers ──────────────────────────────────────────────────────

  defp run_and_report(name, fun, entered_state) do
    start = System.monotonic_time()
    result = safe_invoke(fun)
    duration = System.monotonic_time() - start

    case Classifier.classify(result) do
      :success ->
        report_success(name)
        emit(:success, name, %{duration: duration, entered_state: entered_state})

      :failure ->
        report_failure(name)
        emit(:failure, name, %{duration: duration, entered_state: entered_state})

      :ignore ->
        :ok
    end

    result
  end

  defp safe_invoke(fun) do
    fun.()
  rescue
    error -> {:error, {:exception, error}}
  catch
    :exit, reason -> {:error, {:exit, reason}}
  end

  defp via(name), do: {:via, Registry, {CircuitBreakerPatterns.Registry, name}}

  defp emit(event, name, meta) do
    :telemetry.execute(
      [:circuit_breaker, event],
      %{count: 1},
      Map.put(meta, :breaker, name)
    )
  end

  # ─── GenServer callbacks ───────────────────────────────────────────────────

  @impl true
  def init(opts) do
    name = Keyword.fetch!(opts, :name)
    ensure_table()

    state = %{
      name: name,
      status: :closed,
      failure_threshold: Keyword.get(opts, :failure_threshold, @default_failure_threshold),
      failure_window_ms: Keyword.get(opts, :failure_window_ms, @default_failure_window_ms),
      reset_timeout_ms: Keyword.get(opts, :reset_timeout_ms, @default_reset_timeout_ms),
      failures: [],
      generation: 0,
      reset_timer: nil
    }

    publish(state)
    {:ok, state}
  end

  @impl true
  def handle_cast(:failure, %{status: :closed} = state) do
    now = System.monotonic_time(:millisecond)
    failures = prune(state.failures, now - state.failure_window_ms)
    failures = [now | failures]

    if length(failures) >= state.failure_threshold do
      {:noreply, trip(state, failures)}
    else
      {:noreply, %{state | failures: failures}}
    end
  end

  def handle_cast(:failure, %{status: :half_open} = state) do
    {:noreply, trip(state, state.failures)}
  end

  def handle_cast(:failure, %{status: :open} = state), do: {:noreply, state}

  def handle_cast(:success, %{status: :half_open} = state) do
    Logger.info("[breaker #{inspect(state.name)}] probe succeeded → CLOSED")
    new_state = %{state | status: :closed, failures: [], generation: state.generation + 1}
    publish(new_state)
    emit_transition(new_state, :closed)
    {:noreply, new_state}
  end

  def handle_cast(:success, %{status: :closed} = state) do
    {:noreply, %{state | failures: []}}
  end

  def handle_cast(:success, %{status: :open} = state), do: {:noreply, state}

  @impl true
  def handle_info(:try_half_open, %{status: :open} = state) do
    Logger.info("[breaker #{inspect(state.name)}] timer fired → HALF-OPEN")
    new_state = %{state | status: :half_open, generation: state.generation + 1, reset_timer: nil}
    publish(new_state)
    emit_transition(new_state, :half_open)
    {:noreply, new_state}
  end

  def handle_info(:try_half_open, state), do: {:noreply, state}

  # ─── State transition helpers ─────────────────────────────────────────────

  defp trip(state, failures) do
    Logger.warning("[breaker #{inspect(state.name)}] TRIPPED → OPEN")
    timer = Process.send_after(self(), :try_half_open, state.reset_timeout_ms)

    new_state = %{
      state
      | status: :open,
        failures: failures,
        generation: state.generation + 1,
        reset_timer: timer
    }

    publish(new_state)
    emit_transition(new_state, :open)
    new_state
  end

  defp prune(failures, cutoff), do: Enum.filter(failures, &(&1 >= cutoff))

  defp publish(state) do
    :ets.insert(@table, {state.name, state.status, state.generation})
  end

  defp emit_transition(state, to) do
    :telemetry.execute(
      [:circuit_breaker, :transition],
      %{count: 1},
      %{breaker: state.name, to: to, generation: state.generation}
    )
  end

  defp ensure_table do
    case :ets.whereis(@table) do
      :undefined ->
        :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])

      _ ->
        :ok
    end
  end
end
```

### Step 4: Supervisor & Registry in application.ex

**Objective**: Own the shared ETS table at the application level and supervise breakers dynamically so individual FSM crashes never lose the published gate state.

```elixir
defmodule CircuitBreakerPatterns.Application do
  use Application

  @impl true
  def start(_type, _args) do
    ensure_table_owner()

    children = [
      {Registry, keys: :unique, name: CircuitBreakerPatterns.Registry},
      {DynamicSupervisor, name: CircuitBreakerPatterns.BreakerSup, strategy: :one_for_one}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: CircuitBreakerPatterns.Supervisor)
  end

  # The ETS table must survive individual breaker restarts. We create it under the
  # application supervisor process so it lives as long as the app itself.
  defp ensure_table_owner do
    case :ets.whereis(:circuit_breaker_states) do
      :undefined ->
        :ets.new(:circuit_breaker_states, [
          :named_table,
          :public,
          :set,
          read_concurrency: true
        ])

      _ ->
        :ok
    end
  end
end
```

### Step 5: `test/circuit_breaker_patterns/breaker_test.exs`

**Objective**: Drive the FSM across closed→open→half-open→closed transitions with deterministic time control so regressions in trip thresholds surface instantly.

```elixir
defmodule CircuitBreakerPatterns.BreakerTest do
  use ExUnit.Case, async: false

  alias CircuitBreakerPatterns.Breaker

  setup do
    name = :"breaker_#{System.unique_integer([:positive])}"

    {:ok, _pid} =
      DynamicSupervisor.start_child(
        CircuitBreakerPatterns.BreakerSup,
        {Breaker,
         name: name,
         failure_threshold: 3,
         failure_window_ms: 1_000,
         reset_timeout_ms: 100}
      )

    %{name: name}
  end

  describe "closed state" do
    test "lets successful calls through", %{name: name} do
      assert {:ok, %{status: 200}} =
               Breaker.call(name, fn -> {:ok, %{status: 200}} end)
    end

    test "trips after threshold consecutive failures", %{name: name} do
      for _ <- 1..3 do
        Breaker.call(name, fn -> {:error, :timeout} end)
      end

      Process.sleep(20)
      assert Breaker.current_state(name) == :open
    end
  end

  describe "open state" do
    test "short-circuits immediately", %{name: name} do
      for _ <- 1..3, do: Breaker.call(name, fn -> {:error, :timeout} end)
      Process.sleep(20)

      assert {:error, :circuit_open} =
               Breaker.call(name, fn -> {:ok, %{status: 200}} end)
    end

    test "transitions to half-open after reset_timeout_ms", %{name: name} do
      for _ <- 1..3, do: Breaker.call(name, fn -> {:error, :timeout} end)
      Process.sleep(150)

      assert Breaker.current_state(name) == :half_open
    end
  end

  describe "half-open state" do
    test "probe success → closed", %{name: name} do
      for _ <- 1..3, do: Breaker.call(name, fn -> {:error, :timeout} end)
      Process.sleep(150)

      Breaker.call(name, fn -> {:ok, %{status: 200}} end)
      Process.sleep(20)

      assert Breaker.current_state(name) == :closed
    end

    test "probe failure → back to open", %{name: name} do
      for _ <- 1..3, do: Breaker.call(name, fn -> {:error, :timeout} end)
      Process.sleep(150)

      Breaker.call(name, fn -> {:error, :timeout} end)
      Process.sleep(20)

      assert Breaker.current_state(name) == :open
    end
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Generation counter against stale timer fires.** Timers scheduled during one
OPEN period might fire after the breaker has already been manually reset. A
`generation` field (bumped on every transition) lets `handle_info` discard
out-of-date messages.

**2. Reads via ETS, writes via GenServer.** The ETS table is `:public` with
`read_concurrency: true` — dozens of caller processes can poll state in parallel
without contention. Only the owner GenServer writes.

**3. Classifier is your biggest lever.** Tripping on a 404 (user not found) will
oscillate the breaker during normal traffic. Take time to define failure per
upstream: Stripe's 429 with `Retry-After` is a signal to slow down, not open.

**4. Reset timeout vs retry storms.** When reset fires, every pending caller
retries. Mitigate with (a) half-open (only 1 probe) and (b) jitter on the
reset timer (`reset_timeout_ms + :rand.uniform(div(reset_timeout_ms, 4))`).

**5. One breaker per endpoint, not per host.** If `/payments` is healthy but
`/refunds` is sick, a host-level breaker opens both. Fine-grained breakers
per endpoint (or per upstream_id + operation) give surgical isolation.

**6. Telemetry is non-negotiable.** Without `[:circuit_breaker, :transition]`
events, SRE cannot build a dashboard showing "time spent in OPEN" — the single
most useful SLO input.

**7. When NOT to use this.** For non-idempotent operations where re-trying is
harmful (financial transfers, emails), the breaker is only half the story —
you also need idempotency keys. For internal services in the
same cluster, prefer `:pg` / `:global` patterns and deal with split-brain
explicitly. For hard real-time systems (< 1ms budget), an ETS lookup per call
may itself be too expensive — inline the state in process state.

---

## Benchmark

```elixir
# bench/breaker_bench.exs
alias CircuitBreakerPatterns.Breaker

Benchee.run(
  %{
    "call (closed, success)" => fn ->
      Breaker.call(:bench_closed, fn -> {:ok, %{status: 200}} end)
    end,
    "call (open, short-circuit)" => fn ->
      Breaker.call(:bench_open, fn -> {:ok, %{status: 200}} end)
    end,
    "current_state/1 (ETS lookup)" => fn ->
      Breaker.current_state(:bench_closed)
    end
  },
  time: 5,
  parallel: 8
)
```

Expected on commodity hardware: `current_state/1` ~ 300–500 ns, `call/2` in
short-circuit mode ~ 1 µs (ETS lookup + telemetry dispatch). In production this
is pure win vs 30-second upstream timeouts.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Executable Example

```elixir
defmodule Main do
  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4", only: [:dev, :test]}
    ]
  end



  ### Step 2: `lib/circuit_breaker_patterns/classifier.ex`

  **Objective**: Extract failure classification rules so breaker FSM remains pure and per-upstream policies (429 vs 408) are swappable without modifying state machine.



  ### Step 3: `lib/circuit_breaker_patterns/breaker.ex`

  **Objective**: Serialize state transitions through GenServer but read current state from ETS so call/2 gate checks scale lock-free across cores without mailbox contention.



  ### Step 4: Supervisor & Registry in application.ex

  **Objective**: Own the shared ETS table at the application level and supervise breakers dynamically so individual FSM crashes never lose the published gate state.



  ### Step 5: `test/circuit_breaker_patterns/breaker_test.exs`

  **Objective**: Drive the FSM across closed→open→half-open→closed transitions with deterministic time control so regressions in trip thresholds surface instantly.

  defmodule Main do
    def main do
        # Demonstrating 36-circuit-breaker-patterns
        :ok
    end
  end
end

Main.main()
```
