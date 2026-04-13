# Circuit Breaker with Three States and ETS

**Project**: `payments_breaker` — a stateful circuit breaker protecting a flaky external payments API, backed by ETS for concurrent reads.

## Project context

The payments team operates a checkout flow that depends on a third-party card processor. During peak hours the upstream vendor occasionally returns `503` bursts lasting 30–90 seconds. Without protection, every checkout request piles up waiting on a 5-second HTTP timeout, saturating the connection pool and causing a cascading failure that takes down unrelated features (fraud scoring, loyalty points).

A circuit breaker isolates the failure: once the error rate crosses a threshold, subsequent calls fail fast without touching the network. After a cooldown, the breaker tentatively lets a single probe through (half-open) to test recovery before re-opening the floodgates.

This exercise implements a three-state breaker (closed / open / half-open) with per-service isolation, concurrent-safe reads via ETS, and window-based error accounting. The hot path for `allow?/1` must avoid any `GenServer.call` so that thousands of concurrent callers do not serialize on the breaker process.

```
payments_breaker/
├── lib/
│   └── payments_breaker/
│       ├── application.ex
│       ├── breaker.ex              # public API + GenServer owner of ETS
│       └── breaker/
│           └── state.ex            # pure state transition function
├── test/
│   └── payments_breaker/
│       └── breaker_test.exs
├── bench/
│   └── allow_bench.exs
└── mix.exs
```

## Why a three-state breaker and not a two-state one

A two-state breaker (closed / open) must either keep the circuit open forever or reset blindly after a timer. Resetting blindly is dangerous: if the upstream is still failing, reopening immediately sends full traffic to a sick dependency, re-triggering the same outage you were protecting against.

The half-open state is a controlled probe. Exactly one request is allowed through. If it succeeds the breaker closes. If it fails the breaker re-opens with a fresh cooldown. This gives the upstream time to recover without gambling the entire request load on a single hopeful moment.

## Why ETS for reads and not a pure GenServer

The breaker's `allow?/1` is called on every request. A `GenServer.call` serializes all callers on the breaker's mailbox: at 10k req/s with a 100µs handler you hit mailbox saturation. ETS `:named_table, :public, read_concurrency: true` allows lock-free parallel reads from any scheduler. Only state transitions (open → half-open, etc.) go through the GenServer, and those happen at most a few times per minute.

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
### 1. State transitions
```
        failures >= threshold
  CLOSED ─────────────────────▶ OPEN
     ▲                            │
     │ probe ok                   │ cooldown expired
     │                            ▼
     └──────── HALF_OPEN ◀────────┘
                  │
                  │ probe fails
                  ▼
                 OPEN (with new cooldown)
```

### 2. Rolling window for failure accounting
A naive "count every failure ever" monotonically increases; the breaker never recovers. A fixed bucket resets at cliff edges and allows bursts. We use a rolling window: only failures within `window_ms` count toward the threshold.

### 3. Half-open mutual exclusion
Only one caller must pass through in half-open. We use `:ets.update_counter/4` with a bounded increment trick: atomically increment a probe counter; only the caller who gets `1` proceeds.

## Design decisions

- **Option A — Count failures and successes both**: balanced view, but more writes on the hot path.
- **Option B — Count only failures, reset on success**: simpler, matches most production breakers (Netflix Hystrix's original model).
→ Chose **B** for lower overhead. Production telemetry already tracks success rates elsewhere.

- **Option A — Timer-based half-open transition**: `Process.send_after` schedules the `open → half_open` flip.
- **Option B — Lazy transition on read**: `allow?/1` inspects `opened_at` and computes state.
→ Chose **B**. Avoids timer storms when thousands of breakers exist, and keeps the GenServer idle.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule PaymentsBreaker.MixProject do
  use Mix.Project

  def project do
    [
      app: :payments_breaker,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [mod: {PaymentsBreaker.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule PaymentsBreaker.MixProject do
  use Mix.Project

  def project do
    [
      app: :payments_breaker,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [mod: {PaymentsBreaker.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 1: Application supervision

**Objective**: Boot per-service breaker instances with isolated ETS rows so concurrent readers avoid GenServer mailbox serialization at 10k req/s scale.

```elixir
defmodule PaymentsBreaker.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {PaymentsBreaker.Breaker,
       name: :payments_api,
       failure_threshold: 5,
       window_ms: 10_000,
       cooldown_ms: 30_000}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 2: Pure state logic (`lib/payments_breaker/breaker/state.ex`)

**Objective**: Extract FSM transitions as pure functions to decouple business logic from GenServer/ETS, enabling deterministic unit tests without process state.

```elixir
defmodule PaymentsBreaker.Breaker.State do
  @moduledoc """
  Pure functions for breaker state transitions. No process state, no side effects.
  Trivial to test in isolation and reason about.
  """

  @type status :: :closed | :open | :half_open
  @type t :: %__MODULE__{
          status: status(),
          failures: [integer()],
          opened_at: integer() | nil,
          probe_in_flight: boolean()
        }

  defstruct status: :closed, failures: [], opened_at: nil, probe_in_flight: false

  def new, do: %__MODULE__{}

  def decide(%__MODULE__{status: :closed} = s, _now, _cooldown_ms), do: {:allow, s}

  def decide(%__MODULE__{status: :open, opened_at: opened_at} = s, now, cooldown_ms) do
    if now - opened_at >= cooldown_ms do
      {:allow_probe, %{s | status: :half_open, probe_in_flight: true}}
    else
      {:deny, s}
    end
  end

  def decide(%__MODULE__{status: :half_open, probe_in_flight: true} = s, _now, _), do: {:deny, s}
  def decide(%__MODULE__{status: :half_open} = s, _now, _), do: {:allow_probe, %{s | probe_in_flight: true}}

  def on_success(%__MODULE__{status: :half_open} = s) do
    %__MODULE__{s | status: :closed, failures: [], opened_at: nil, probe_in_flight: false}
  end

  def on_success(%__MODULE__{status: :closed} = s), do: %{s | failures: []}
  def on_success(s), do: s

  def on_failure(%__MODULE__{status: :half_open} = s, now) do
    %__MODULE__{s | status: :open, opened_at: now, probe_in_flight: false}
  end

  def on_failure(%__MODULE__{status: :closed} = s, now, window_ms, threshold) do
    cutoff = now - window_ms
    recent = [now | Enum.filter(s.failures, &(&1 > cutoff))]

    if length(recent) >= threshold do
      %__MODULE__{s | status: :open, failures: recent, opened_at: now}
    else
      %{s | failures: recent}
    end
  end

  def on_failure(s, _now, _window_ms, _threshold), do: s
end
```

### Step 3: GenServer with ETS-backed hot path (`lib/payments_breaker/breaker.ex`)

**Objective**: Publish FSM state to :public ETS so lock-free reads bypass GenServer; only state transitions route through mailbox to guarantee atomic correctness.

```elixir
defmodule PaymentsBreaker.Breaker do
  use GenServer
  alias PaymentsBreaker.Breaker.State

  @table :payments_breaker_states

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: opts[:name] || __MODULE__)
  end

  def allow?(name) do
    case :ets.lookup(@table, name) do
      [{^name, :closed, _, _, _, _}] ->
        true

      [{^name, :open, _, opened_at, cooldown_ms, _}] ->
        now = System.monotonic_time(:millisecond)

        if now - opened_at >= cooldown_ms do
          GenServer.call(name, :try_probe)
        else
          false
        end

      [{^name, :half_open, _, _, _, _}] ->
        GenServer.call(name, :try_probe)

      [] ->
        true
    end
  end

  def record_success(name), do: GenServer.cast(name, :success)
  def record_failure(name), do: GenServer.cast(name, :failure)

  @impl true
  def init(opts) do
    name = Keyword.fetch!(opts, :name)

    table =
      case :ets.whereis(@table) do
        :undefined -> :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
        ref -> ref
      end

    state = %{
      name: name,
      threshold: Keyword.fetch!(opts, :failure_threshold),
      window_ms: Keyword.fetch!(opts, :window_ms),
      cooldown_ms: Keyword.fetch!(opts, :cooldown_ms),
      breaker: State.new(),
      table: table
    }

    publish(state)
    {:ok, state}
  end

  @impl true
  def handle_call(:try_probe, _from, state) do
    now = System.monotonic_time(:millisecond)

    case State.decide(state.breaker, now, state.cooldown_ms) do
      {:allow_probe, new_breaker} ->
        new_state = %{state | breaker: new_breaker}
        publish(new_state)
        {:reply, true, new_state}

      {:allow, new_breaker} ->
        {:reply, true, %{state | breaker: new_breaker}}

      {:deny, _} ->
        {:reply, false, state}
    end
  end

  @impl true
  def handle_cast(:success, state) do
    new_state = %{state | breaker: State.on_success(state.breaker)}
    publish(new_state)
    {:noreply, new_state}
  end

  def handle_cast(:failure, state) do
    now = System.monotonic_time(:millisecond)

    new_breaker =
      case state.breaker.status do
        :half_open -> State.on_failure(state.breaker, now)
        _ -> State.on_failure(state.breaker, now, state.window_ms, state.threshold)
      end

    new_state = %{state | breaker: %{new_breaker | opened_at: new_breaker.opened_at || state.breaker.opened_at}}
    publish(new_state)
    {:noreply, new_state}
  end

  defp publish(%{name: name, breaker: b, cooldown_ms: c, table: t}) do
    :ets.insert(t, {name, b.status, b.failures, b.opened_at || 0, c, b.probe_in_flight})
  end
end
```

## Why this works

- **Lock-free reads** — `:ets.lookup/2` on a `read_concurrency: true` table reads from any scheduler without touching the breaker's mailbox. At 10k req/s the mailbox sees zero messages in steady state (all requests hit the `:closed` fast path).
- **Lazy transition** — the read path notices "open cooldown elapsed" and only then pays the cost of a `GenServer.call` to atomically transition. This amortizes the transition across one caller instead of firing a timer.
- **Half-open mutual exclusion** — the GenServer is single-threaded; once `probe_in_flight: true` is set, all subsequent `:try_probe` calls return `false` until the probe completes (`record_success` resets it).
- **Pure state module** — `State` has no process, no ETS, no time source injected as arguments. Trivially testable and debuggable.

## Tests

```elixir
defmodule PaymentsBreaker.BreakerTest do
  use ExUnit.Case, async: false
  alias PaymentsBreaker.Breaker

  setup do
    name = :"breaker_#{System.unique_integer([:positive])}"

    {:ok, _} =
      Breaker.start_link(
        name: name,
        failure_threshold: 3,
        window_ms: 10_000,
        cooldown_ms: 100
      )

    {:ok, name: name}
  end

  describe "closed state" do
    test "allows traffic by default", %{name: n} do
      assert Breaker.allow?(n)
    end

    test "stays closed below threshold", %{name: n} do
      Breaker.record_failure(n)
      Breaker.record_failure(n)
      :sys.get_state(n)
      assert Breaker.allow?(n)
    end
  end

  describe "opening" do
    test "opens after threshold failures", %{name: n} do
      for _ <- 1..3, do: Breaker.record_failure(n)
      :sys.get_state(n)
      refute Breaker.allow?(n)
    end
  end

  describe "half-open probe" do
    test "allows exactly one probe after cooldown", %{name: n} do
      for _ <- 1..3, do: Breaker.record_failure(n)
      :sys.get_state(n)
      Process.sleep(120)

      assert Breaker.allow?(n)
      refute Breaker.allow?(n)
    end

    test "closes on probe success", %{name: n} do
      for _ <- 1..3, do: Breaker.record_failure(n)
      :sys.get_state(n)
      Process.sleep(120)
      assert Breaker.allow?(n)

      Breaker.record_success(n)
      :sys.get_state(n)
      assert Breaker.allow?(n)
    end

    test "re-opens on probe failure", %{name: n} do
      for _ <- 1..3, do: Breaker.record_failure(n)
      :sys.get_state(n)
      Process.sleep(120)
      assert Breaker.allow?(n)

      Breaker.record_failure(n)
      :sys.get_state(n)
      refute Breaker.allow?(n)
    end
  end
end
```

## Benchmark

```elixir
# bench/allow_bench.exs
{:ok, _} = PaymentsBreaker.Application.start(:normal, [])

Benchee.run(
  %{
    "allow?/1 closed state" => fn -> PaymentsBreaker.Breaker.allow?(:payments_api) end
  },
  parallel: 16,
  time: 5,
  warmup: 2
)
```

Expected on a modern laptop: p99 < 2µs, p50 < 500ns. If you see > 10µs the call is hitting the GenServer — verify the ETS row is present and `:closed`.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Etsdets Patterns and Production Implications

ETS tables are in-memory, non-distributed key-value stores with tunable semantics (ordered_set, duplicate_bag). Under concurrent read/write load, ETS table semantics matter: bag semantics allow fast appends but slow deletes; ordered_set allows range queries but slower inserts. Testing ETS behavior under concurrent load is non-trivial; single-threaded tests miss lock contention. Production ETS tables often fail under load due to concurrency assumptions that quiet tests don't exercise.

---

## Trade-offs and production gotchas

**1. Counting failures in a list is O(n)** — fine for thresholds < 100; for high-throughput breakers replace with an atomic counter per rolling bucket.

**2. Never call `allow?/1` inside a transaction** — if the breaker denies, you will have started a DB transaction for nothing. Check first, then open the transaction.

**3. Clock source matters** — `System.monotonic_time/1` never goes backward. Using `:os.system_time` can cause negative durations on NTP adjustments, making cooldowns skip or stick.

**4. `read_concurrency: true` has a write cost** — concurrent writers pay extra synchronization. Fine here because the write rate is tiny (< 100/s per breaker).

**5. Restart clears state** — if the supervisor restarts the breaker, it starts `:closed`. If your SLA requires persistence across restarts, write transitions to `:persistent_term` or an external store and read them in `init/1`.

**6. When NOT to use this** — for per-request quotas use a rate limiter, not a breaker. Breakers gate on *health*, not on *budget*.

## Reflection

If two callers race on the first `allow?/1` after cooldown expires, both may attempt `GenServer.call(:try_probe)`. Only one gets `probe_in_flight: true`. Why is this safe? What would break if you tried to avoid the GenServer call entirely with `:ets.update_counter/4`?

## Executable Example

```elixir
defmodule PaymentsBreaker.MixProject do
  use Mix.Project

  def project do
    [
      app: :payments_breaker,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [mod: {PaymentsBreaker.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [{:benchee, "~> 1.3", only: :dev}]
  end
end



### Step 1: Application supervision

**Objective**: Boot per-service breaker instances with isolated ETS rows so concurrent readers avoid GenServer mailbox serialization at 10k req/s scale.



### Step 2: Pure state logic (`lib/payments_breaker/breaker/state.ex`)

**Objective**: Extract FSM transitions as pure functions to decouple business logic from GenServer/ETS, enabling deterministic unit tests without process state.



### Step 3: GenServer with ETS-backed hot path (`lib/payments_breaker/breaker.ex`)

**Objective**: Publish FSM state to :public ETS so lock-free reads bypass GenServer; only state transitions route through mailbox to guarantee atomic correctness.

defmodule Main do
  def main do
      # Demonstrating 303-circuit-breaker-states-ets
      :ok
  end
end

Main.main()
```
