# Saga Pattern with Compensating Actions

**Project**: `booking_saga` — a travel-booking saga that coordinates flight + hotel + car reservations across three independent services, with compensating actions that roll back partial state on failure.

## Project context

A user books a trip: flight, hotel, car. Each is a separate service with its own database. Two-phase commit across three services is impractical (latency, coupling, availability). The alternative is the Saga pattern: execute steps forward, and if a later step fails, run explicit *compensating actions* for the already-completed steps.

If hotel booking fails after flight succeeds, we must *cancel* the flight — not undo via rollback (flight's DB doesn't know about our transaction). Cancellation is a new business transaction that produces the same net effect.

```
booking_saga/
├── lib/
│   └── booking_saga/
│       ├── saga.ex                 # pure saga executor
│       ├── step.ex                 # step record
│       ├── flight.ex               # simulated service
│       ├── hotel.ex
│       └── car.ex
├── test/
│   └── booking_saga/
│       └── saga_test.exs
└── mix.exs
```

## Why sagas and not distributed transactions

XA / 2PC requires a coordinator, prepared state, and all participants to hold locks until commit. Across three HTTP services with variable latency, prepared locks can last seconds, starving concurrent transactions. Sagas eliminate the coordinator and locks: each step commits independently. The trade-off is eventual consistency — there's a window where the system is partially-committed.

## Why explicit compensations and not generic "undo"

"Undo" assumes you can reverse state. In most business systems you can't: money charged is not "uncharged", it is "refunded" — a separate event with different side effects (ledger entries, audit log, notifications). The compensation is *business logic*, not a technical rollback.

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
### 1. Forward path
```
[flight] → [hotel] → [car] → success
```
Run each step sequentially. If all succeed, saga completes.

### 2. Compensation path
```
[flight] → [hotel] → [car fails]
                         ↓
           cancel(hotel) ← cancel(flight)
```
When step N fails, run compensations for steps 1..N-1 in *reverse order*. Flight is compensated last because it was completed first (LIFO).

### 3. Step definition
Each step has:
- `run/1` — forward action returning `{:ok, state_update}` or `{:error, reason}`
- `compensate/1` — takes the state produced by run, undoes its effect

### 4. Compensation must be best-effort but logged
If compensation itself fails, you have a *real* inconsistency. Log it, alert, and let an operator resolve. Sagas cannot promise atomicity; only guaranteed-best-effort reversal.

## Design decisions

- **Option A — Orchestration (single coordinator calls services)**: simpler, centralized, easier to test. Coordinator is a single point of failure.
- **Option B — Choreography (each service listens for events, publishes its own)**: decoupled, resilient, harder to trace.
→ Chose **A** (orchestration) for clarity. Choreography is a natural extension.

- **Option A — Steps as modules**: polymorphic, Elixir-idiomatic.
- **Option B — Steps as data (maps with `:run`, `:compensate` functions)**: easier to build dynamically.
→ Chose **B**. Lets you assemble a saga from a list.

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
defmodule BookingSaga.MixProject do
  use Mix.Project
  def project, do: [app: :booking_saga, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [extra_applications: [:logger]]
end
```

### Step 1: Step struct (`lib/booking_saga/step.ex`)

**Objective**: Encode forward action and its compensator in one struct so business rollback logic lives alongside forward logic without separate reverse tables.

```elixir
defmodule BookingSaga.Step do
  @type state :: map()
  @type run_result :: {:ok, state()} | {:error, term()}

  @type t :: %__MODULE__{
          name: atom(),
          run: (state() -> run_result()),
          compensate: (state() -> :ok | {:error, term()})
        }

  defstruct [:name, :run, :compensate]
end
```

### Step 2: Saga executor (`lib/booking_saga/saga.ex`)

**Objective**: Accumulate completed steps in LIFO stack so compensation runs in exact reverse order and failures are logged without distributed transaction overhead.

```elixir
defmodule BookingSaga.Saga do
  alias BookingSaga.Step
  require Logger

  @type result :: {:ok, map()} | {:error, term(), map(), [atom()]}

  @spec run([Step.t()], map()) :: result()
  def run(steps, initial_state \\ %{}) do
    do_run(steps, initial_state, [])
  end

  defp do_run([], state, _completed), do: {:ok, state}

  defp do_run([%Step{name: name, run: run_fun} | rest], state, completed) do
    case run_fun.(state) do
      {:ok, new_state} ->
        do_run(rest, new_state, [name | completed])

      {:error, reason} ->
        failed_step = name
        compensation_log = compensate_all(completed, state)
        {:error, {failed_step, reason}, state, compensation_log}
    end
  end

  defp compensate_all(completed_names, state) do
    Enum.map(completed_names, fn name ->
      step = find_step(state[:__steps__] || [], name)

      result =
        try do
          step.compensate.(state)
        rescue
          e ->
            Logger.error("compensation raised for #{name}: #{inspect(e)}")
            {:error, {:raised, e}}
        end

      {name, result}
    end)
  end

  defp find_step(_list, _name), do: %Step{compensate: fn _ -> :ok end}
end
```

The version above assumes steps pass themselves through state. A cleaner alternative threads `steps` explicitly:

```elixir
defmodule BookingSaga.Saga do
  alias BookingSaga.Step
  require Logger

  @type log :: [{atom(), :ok | {:error, term()}}]
  @type result :: {:ok, map()} | {:error, term(), map(), log()}

  @spec run([Step.t()], map()) :: result()
  def run(steps, initial_state \\ %{}) do
    do_run(steps, initial_state, [])
  end

  defp do_run([], state, _completed), do: {:ok, state}

  defp do_run([%Step{name: name, run: run_fun} = step | rest], state, completed) do
    case run_fun.(state) do
      {:ok, new_state} ->
        do_run(rest, new_state, [step | completed])

      {:error, reason} ->
        log = compensate_all(completed, state)
        {:error, {name, reason}, state, log}
    end
  end

  defp compensate_all(completed, state) do
    Enum.map(completed, fn %Step{name: name, compensate: comp} ->
      result =
        try do
          comp.(state)
        rescue
          e ->
            Logger.error("compensation raised for #{name}: #{inspect(e)}")
            {:error, {:raised, e}}
        end

      {name, result}
    end)
  end
end
```

Use the second version — it is the real one we'll test.

### Step 3: Simulated services

**Objective**: Inject success/failure outcomes via Process.put/get so test scenarios control booking results without external service mocks or network calls.

```elixir
defmodule BookingSaga.Flight do
  def book(_state) do
    case Process.get(:flight_result, :ok) do
      :ok -> {:ok, %{flight_ref: "FL-#{:rand.uniform(1000)}"}}
      {:error, r} -> {:error, r}
    end
  end

  def cancel(state) do
    Process.put(:flight_cancelled, state.flight_ref)
    :ok
  end
end

defmodule BookingSaga.Hotel do
  def book(_state) do
    case Process.get(:hotel_result, :ok) do
      :ok -> {:ok, %{hotel_ref: "HT-#{:rand.uniform(1000)}"}}
      {:error, r} -> {:error, r}
    end
  end

  def cancel(state) do
    Process.put(:hotel_cancelled, state.hotel_ref)
    :ok
  end
end

defmodule BookingSaga.Car do
  def book(_state) do
    case Process.get(:car_result, :ok) do
      :ok -> {:ok, %{car_ref: "CA-#{:rand.uniform(1000)}"}}
      {:error, r} -> {:error, r}
    end
  end

  def cancel(state) do
    Process.put(:car_cancelled, state.car_ref)
    :ok
  end
end
```

### Step 4: Example saga construction

**Objective**: Assemble booking saga as data structure so executor is reusable and saga logic is transparent without inheritance or mutable state.

```elixir
defmodule BookingSaga.TripSaga do
  alias BookingSaga.{Saga, Step, Flight, Hotel, Car}

  def book_trip(user_id) do
    steps = [
      %Step{
        name: :flight,
        run: fn state -> with {:ok, r} <- Flight.book(state), do: {:ok, Map.merge(state, r)} end,
        compensate: &Flight.cancel/1
      },
      %Step{
        name: :hotel,
        run: fn state -> with {:ok, r} <- Hotel.book(state), do: {:ok, Map.merge(state, r)} end,
        compensate: &Hotel.cancel/1
      },
      %Step{
        name: :car,
        run: fn state -> with {:ok, r} <- Car.book(state), do: {:ok, Map.merge(state, r)} end,
        compensate: &Car.cancel/1
      }
    ]

    Saga.run(steps, %{user_id: user_id})
  end
end
```

## Why this works

- **LIFO compensation order** — completed steps are prepended to a list; compensating in list order reverses them naturally (newest completed is cancelled first, matching the order of the reversal).
- **Compensations run even if one fails** — we iterate all completed steps; if hotel compensation fails we still attempt flight compensation. The returned `log` records each one so an operator can inspect what was undone and what wasn't.
- **Rescue prevents leak** — a raising compensation doesn't stop subsequent compensations. We log the raise, record `{:error, {:raised, e}}`, and continue.
- **State is threaded immutably** — each `run` returns a new state; we never mutate. Compensations receive the full state, including refs for every service they need to cancel.

## Tests

```elixir
defmodule BookingSaga.SagaTest do
  use ExUnit.Case, async: false
  alias BookingSaga.TripSaga

  setup do
    Process.put(:flight_result, :ok)
    Process.put(:hotel_result, :ok)
    Process.put(:car_result, :ok)
    Process.delete(:flight_cancelled)
    Process.delete(:hotel_cancelled)
    Process.delete(:car_cancelled)
    :ok
  end

  describe "happy path" do
    test "all three steps succeed" do
      assert {:ok, state} = TripSaga.book_trip("u1")
      assert Map.has_key?(state, :flight_ref)
      assert Map.has_key?(state, :hotel_ref)
      assert Map.has_key?(state, :car_ref)

      refute Process.get(:flight_cancelled)
      refute Process.get(:hotel_cancelled)
      refute Process.get(:car_cancelled)
    end
  end

  describe "compensation on failure" do
    test "hotel fails → flight is cancelled, car is not attempted" do
      Process.put(:hotel_result, {:error, :no_rooms})

      assert {:error, {:hotel, :no_rooms}, state, log} = TripSaga.book_trip("u1")

      assert Map.has_key?(state, :flight_ref)
      refute Map.has_key?(state, :hotel_ref)
      refute Map.has_key?(state, :car_ref)

      assert {:flight, :ok} in log
      assert Process.get(:flight_cancelled) == state.flight_ref
    end

    test "car fails → hotel and flight are cancelled in that order" do
      Process.put(:car_result, {:error, :unavailable})

      assert {:error, {:car, :unavailable}, state, log} = TripSaga.book_trip("u1")

      # hotel compensated first, then flight
      assert [{:hotel, :ok}, {:flight, :ok}] = log
      assert Process.get(:hotel_cancelled) == state.hotel_ref
      assert Process.get(:flight_cancelled) == state.flight_ref
    end
  end

  describe "first-step failure" do
    test "no compensations to run" do
      Process.put(:flight_result, {:error, :sold_out})

      assert {:error, {:flight, :sold_out}, _state, []} = TripSaga.book_trip("u1")
    end
  end
end
```

## Benchmark

```elixir
# Saga overhead for a 3-step happy path — should be sub-millisecond in memory
{t, _} = :timer.tc(fn ->
  for _ <- 1..10_000 do
    Process.put(:flight_result, :ok)
    Process.put(:hotel_result, :ok)
    Process.put(:car_result, :ok)
    BookingSaga.TripSaga.book_trip("u")
  end
end)
IO.puts("avg: #{t / 10_000} µs")
```

Expected: < 50µs per saga in-memory. Real-world sagas are dominated by network I/O, not orchestration cost.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. Eventual consistency is visible** — between "flight booked" and "hotel failed, flight cancelled", users can observe a booked flight they never actually get to use. Don't emit user notifications inside saga steps; wait for completion.

**2. Compensations must be idempotent** — if compensation itself is retried, it must not double-refund or double-cancel. Same idempotency-key pattern as the forward path.

**3. Semantic compensation only** — you cannot compensate "sent email" — the email is already in the recipient's inbox. Sagas work only when every step has a meaningful inverse transaction.

**4. Persistent saga state for durability** — this exercise is in-process. Production sagas persist each step's result (Oban, DB table) so a crashed saga can be resumed.

**5. Compensation ordering matters** — LIFO is the safe default. Some domains need a different order; make it explicit.

**6. When NOT to use this** — within a single database, use a transaction. Sagas are for cross-service workflows where a single transaction is impossible.

## Reflection

Hotel booking succeeded but the saga crashed before hotel_ref was written to the saga's state. When an operator tries to recover, how do they know the hotel is booked? What mechanism guarantees exactly-once compensation?


## Executable Example

```elixir
defmodule BookingSaga.SagaTest do
  use ExUnit.Case, async: false
  alias BookingSaga.TripSaga

  setup do
    Process.put(:flight_result, :ok)
    Process.put(:hotel_result, :ok)
    Process.put(:car_result, :ok)
    Process.delete(:flight_cancelled)
    Process.delete(:hotel_cancelled)
    Process.delete(:car_cancelled)
    :ok
  end

  describe "happy path" do
    test "all three steps succeed" do
      assert {:ok, state} = TripSaga.book_trip("u1")
      assert Map.has_key?(state, :flight_ref)
      assert Map.has_key?(state, :hotel_ref)
      assert Map.has_key?(state, :car_ref)

      refute Process.get(:flight_cancelled)
      refute Process.get(:hotel_cancelled)
      refute Process.get(:car_cancelled)
    end
  end

  describe "compensation on failure" do
    test "hotel fails → flight is cancelled, car is not attempted" do
      Process.put(:hotel_result, {:error, :no_rooms})

      assert {:error, {:hotel, :no_rooms}, state, log} = TripSaga.book_trip("u1")

      assert Map.has_key?(state, :flight_ref)
      refute Map.has_key?(state, :hotel_ref)
      refute Map.has_key?(state, :car_ref)

      assert {:flight, :ok} in log
      assert Process.get(:flight_cancelled) == state.flight_ref
    end

    test "car fails → hotel and flight are cancelled in that order" do
      Process.put(:car_result, {:error, :unavailable})

      assert {:error, {:car, :unavailable}, state, log} = TripSaga.book_trip("u1")

      # hotel compensated first, then flight
      assert [{:hotel, :ok}, {:flight, :ok}] = log
      assert Process.get(:hotel_cancelled) == state.hotel_ref
      assert Process.get(:flight_cancelled) == state.flight_ref
    end
  end

  describe "first-step failure" do
    test "no compensations to run" do
      Process.put(:flight_result, {:error, :sold_out})

      assert {:error, {:flight, :sold_out}, _state, []} = TripSaga.book_trip("u1")
    end
  end
end

defmodule Main do
  def main do
    IO.puts("✓ Saga Pattern with Compensating Actions")
  - Saga pattern with compensating actions
    - Distributed transaction handling
  end
end

Main.main()
```
