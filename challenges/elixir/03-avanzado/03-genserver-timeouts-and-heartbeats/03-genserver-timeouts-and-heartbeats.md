# GenServer Timeouts, Heartbeats & Circuit Breaker State Machine

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The circuit breaker component (started in exercise 01)
currently transitions between states based on failure counts, but it has no way to
detect when an upstream service becomes **healthy again**. A breaker that opened at
3 AM stays open forever unless someone manually resets it.

You need to add:
1. A **heartbeat** that probes open circuits every 30 seconds
2. A **call timeout** that prevents the gateway router from hanging on slow upstreams
3. A **watchdog** that health-checks a pool of circuit breaker workers and restarts
   unresponsive ones

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── circuit_breaker/
│           ├── worker.ex        # ← extend with heartbeat + half-open logic
│           ├── supervisor.ex    # already exists
│           └── watchdog.ex      # ← you implement this
├── test/
│   └── api_gateway/
│       └── circuit_breaker/
│           ├── worker_test.exs          # given tests — must pass
│           └── watchdog_test.exs        # given tests — must pass
└── mix.exs
```

---

## Three distinct timeout mechanisms

The word "timeout" is overloaded in GenServer. Conflating these three causes hard-to-debug
production incidents.

**1. Call timeout (caller-side deadline)**
`GenServer.call(pid, msg, 5_000)` raises `{:timeout, ...}` in the *calling* process if
no reply arrives in 5 seconds. The GenServer itself is unaffected — it may still process
the call and send a reply into the dead caller's mailbox. This is a client concern.

**2. Inactivity timeout (server-side idle detector)**
Returning `{:reply, val, state, 30_000}` from a callback schedules a `:timeout` message
to the GenServer after 30 seconds of no messages. Used for hibernation, cleanup, or
self-termination. Resets automatically on every callback return.

**3. Explicit timer (scheduled messages)**
`:timer.send_interval/2` or `Process.send_after/3` inject arbitrary messages into the
mailbox on a schedule. Use this for heartbeats and periodic probes. Unlike the inactivity
timeout, explicit timers do NOT reset on message receipt — they fire unconditionally.

```
Mechanism        │ Where it runs   │ Resets on msg?  │ Cancels itself?
─────────────────┼─────────────────┼─────────────────┼────────────────
Call timeout     │ Caller process  │ N/A             │ On reply
Inactivity timer │ GenServer       │ Yes             │ Never (fires once)
send_interval    │ Timer wheel     │ No              │ Only with cancel/1
```

---

## Circuit breaker state machine

The full three-state machine the circuit breaker worker must implement:

```
           ┌──────────────────────────────────────────────┐
           │  failures >= threshold                        │
           ▼                                              │
        :closed ──────────────────────────────────────▶ :open
           ▲                                              │
           │  probe succeeds                              │ recovery_window_ms elapsed
           │                                              ▼
        :half_open ◀─────────────────────────────────────┘
           │
           │ probe fails → back to :open
           └──────────────────────────────────────────▶ :open
```

The `:open` state is useless without a timer that eventually tries to recover.
That timer is the heartbeat.

---

## Implementation

### Step 1: Extend `CircuitBreaker.Worker` with heartbeat

```elixir
defmodule ApiGateway.CircuitBreaker.Worker do
  use GenServer
  require Logger

  @failure_threshold   5
  @recovery_window_ms  30_000
  @heartbeat_ms        30_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(service_name) do
    GenServer.start_link(__MODULE__, service_name)
  end

  def record_success(pid), do: GenServer.cast(pid, :success)
  def record_failure(pid), do: GenServer.cast(pid, :failure)
  def status(pid), do: GenServer.call(pid, :status)
  def ping(pid), do: GenServer.call(pid, :ping, 1_000)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(service_name) do
    # TODO: start the heartbeat timer with :timer.send_interval/2
    # Store the timer ref in state so you can cancel it in terminate/2
    #
    # State fields:
    #   :service       — service name (string)
    #   :status        — :closed | :open | :half_open
    #   :failures      — consecutive failure count
    #   :opened_at     — monotonic timestamp when circuit opened (nil if closed)
    #   :timer_ref     — ref returned by :timer.send_interval/2
    #
    # HINT: {:ok, timer_ref} = :timer.send_interval(@heartbeat_ms, :heartbeat)
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call(:status, _from, state) do
    {:reply, state.status, state}
  end

  @impl true
  def handle_call(:ping, _from, state) do
    # Used by the watchdog to verify the worker is alive and responsive
    {:reply, :pong, state}
  end

  @impl true
  def handle_cast(:success, state) do
    # TODO: reset failures; if :half_open → :closed; log recovery
    # HINT: if state.status == :half_open, log "circuit closed for #{state.service}"
  end

  @impl true
  def handle_cast(:failure, state) do
    # TODO:
    # If :closed and failures + 1 >= @failure_threshold → open the circuit
    # If :half_open → re-open the circuit
    # If :open → do nothing (already open)
    # Record opened_at when transitioning to :open
  end

  @impl true
  def handle_info(:heartbeat, %{status: :open} = state) do
    # TODO: check if recovery window has elapsed
    # If yes: transition to :half_open and log "probing #{state.service}"
    # If no: stay :open
    #
    # HINT: System.monotonic_time(:millisecond) - state.opened_at >= @recovery_window_ms
  end

  @impl true
  def handle_info(:heartbeat, state) do
    # Not :open — nothing to do
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, state) do
    # TODO: cancel the heartbeat timer to avoid orphan timer entries
    # HINT: :timer.cancel(state.timer_ref)
    :ok
  end
end
```

### Step 2: `lib/api_gateway/circuit_breaker/watchdog.ex`

```elixir
defmodule ApiGateway.CircuitBreaker.Watchdog do
  use GenServer
  require Logger

  @check_interval_ms 10_000
  @ping_timeout_ms   1_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Starts the watchdog with a supervisor reference and initial worker registry.
  registry is a map of %{service_name => pid}.
  """
  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns current {service_name => pid} registry."
  def registry, do: GenServer.call(__MODULE__, :registry)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    supervisor = Keyword.fetch!(opts, :supervisor)
    registry   = Keyword.get(opts, :registry, %{})

    # TODO: schedule the health check loop
    # HINT: {:ok, _ref} = :timer.send_interval(@check_interval_ms, :health_check)

    {:ok, %{supervisor: supervisor, registry: registry}}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call(:registry, _from, state) do
    {:reply, state.registry, state}
  end

  @impl true
  def handle_info(:health_check, state) do
    # TODO: check all workers in parallel using Task.async_stream/3
    # For each worker:
    #   1. Call Worker.ping(pid) with @ping_timeout_ms
    #   2. If it returns :pong → healthy, keep in registry
    #   3. If it raises/exits → unresponsive:
    #      a. Kill the worker: Process.exit(pid, :kill)
    #      b. Restart via DynamicSupervisor.start_child(state.supervisor, ...)
    #      c. If restart succeeds → update registry with new pid
    #      d. If restart fails → log error and remove from registry
    #
    # HINT: use Task.async_stream with max_concurrency: map_size(state.registry)
    # HINT: catch errors with on_timeout: :kill_task and handle {:exit, reason}
    #
    # Return {:noreply, %{state | registry: updated_registry}}
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/circuit_breaker/worker_test.exs
defmodule ApiGateway.CircuitBreaker.WorkerTest do
  use ExUnit.Case, async: true

  alias ApiGateway.CircuitBreaker.Worker

  describe "heartbeat-driven recovery" do
    test "transitions open → half_open after recovery window" do
      {:ok, pid} = Worker.start_link("slow-upstream")

      # Open the circuit
      for _ <- 1..5, do: Worker.record_failure(pid)
      Process.sleep(10)
      assert Worker.status(pid) == :open

      # Simulate recovery window elapsed by sending :heartbeat directly
      # (avoids waiting 30 real seconds in a test)
      # First manipulate opened_at so the window check passes
      :sys.replace_state(pid, fn state ->
        %{state | opened_at: System.monotonic_time(:millisecond) - 31_000}
      end)

      send(pid, :heartbeat)
      Process.sleep(20)

      assert Worker.status(pid) == :half_open
    end

    test "half_open → closed on success" do
      {:ok, pid} = Worker.start_link("recovering-upstream")
      for _ <- 1..5, do: Worker.record_failure(pid)
      Process.sleep(10)
      :sys.replace_state(pid, fn s -> %{s | opened_at: s.opened_at - 31_000} end)
      send(pid, :heartbeat)
      Process.sleep(20)

      Worker.record_success(pid)
      Process.sleep(10)
      assert Worker.status(pid) == :closed
    end

    test "half_open → open on failure" do
      {:ok, pid} = Worker.start_link("flapping-upstream")
      for _ <- 1..5, do: Worker.record_failure(pid)
      Process.sleep(10)
      :sys.replace_state(pid, fn s -> %{s | opened_at: s.opened_at - 31_000} end)
      send(pid, :heartbeat)
      Process.sleep(20)
      assert Worker.status(pid) == :half_open

      Worker.record_failure(pid)
      Process.sleep(10)
      assert Worker.status(pid) == :open
    end

    test "heartbeat does nothing when circuit is closed" do
      {:ok, pid} = Worker.start_link("healthy")
      send(pid, :heartbeat)
      Process.sleep(20)
      assert Worker.status(pid) == :closed
    end
  end
end
```

```elixir
# test/api_gateway/circuit_breaker/watchdog_test.exs
defmodule ApiGateway.CircuitBreaker.WatchdogTest do
  use ExUnit.Case, async: false

  alias ApiGateway.CircuitBreaker.{Worker, Watchdog}

  setup do
    {:ok, sup} = DynamicSupervisor.start_link(strategy: :one_for_one)

    # Start 3 workers under the dynamic supervisor
    workers =
      for name <- ["svc-a", "svc-b", "svc-c"] do
        {:ok, pid} = DynamicSupervisor.start_child(sup, {Worker, name})
        {name, pid}
      end
      |> Map.new()

    {:ok, _wd} =
      Watchdog.start_link(supervisor: sup, registry: workers)

    on_exit(fn -> DynamicSupervisor.stop(sup) end)
    %{supervisor: sup, workers: workers}
  end

  test "registry contains all 3 workers initially", %{workers: workers} do
    assert map_size(Watchdog.registry()) == 3
    for {_name, pid} <- workers do
      assert Map.values(Watchdog.registry()) |> Enum.member?(pid)
    end
  end

  test "unresponsive worker is restarted and registry is updated", %{workers: workers} do
    {"svc-a", old_pid} = Enum.find(workers, fn {k, _} -> k == "svc-a" end)

    # Kill the worker without going through the supervisor
    Process.exit(old_pid, :kill)
    Process.sleep(50)

    # Trigger a health check immediately
    send(Process.whereis(Watchdog), :health_check)
    Process.sleep(200)

    registry = Watchdog.registry()
    assert Map.has_key?(registry, "svc-a")

    new_pid = registry["svc-a"]
    assert is_pid(new_pid)
    assert new_pid != old_pid
    assert Process.alive?(new_pid)
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/circuit_breaker/ --trace
```

---

## Trade-off analysis

| Mechanism | Use case | Key pitfall |
|-----------|----------|-------------|
| Call timeout (caller-side) | Prevent caller from hanging on slow upstream | GenServer still processes the call; orphan reply in dead mailbox |
| Inactivity timeout (server-side) | Hibernate idle workers, self-terminate | Heartbeats reset the clock — may never fire if service is active |
| `:timer.send_interval` | Periodic heartbeat, health probe | Does NOT auto-cancel on process death — always cancel in `terminate/2` |
| `trap_exit` | React to linked process deaths | Suppresses supervisor kill signals; must handle all exit reasons |

Reflection question: the watchdog uses `Task.async_stream` to check all workers in
parallel. What is the worst-case latency if you checked them sequentially with
a 1-second call timeout each for 20 workers?

---

## Common production mistakes

**1. Confusing call timeout with inactivity timeout**
`GenServer.call(pid, msg, 5_000)` raises in the *caller* after 5 s.
`{:reply, val, state, 5_000}` fires `:timeout` in the *GenServer* after 5 s of idle.
Mixing them up causes impossible-to-reproduce bugs where the server self-messages
thinking it is idle but it is actually the caller's deadline.

**2. Not cancelling `:timer.send_interval` in `terminate/2`**
The timer wheel entry persists after the process dies. On the next restart a new timer
is created. Over time, dead timer entries accumulate. Always store the timer ref in state
and call `:timer.cancel(state.timer_ref)` in `terminate/2`.

**3. Trapping exits without handling all exit reasons**
`Process.flag(:trap_exit, true)` converts ALL linked exits into messages, including
`{:EXIT, pid, :normal}` and `{:EXIT, pid, :shutdown}`. If your supervisor sends
`:shutdown` and you only handle `:normal`, the shutdown message accumulates unhandled
and OTP emits timeout warnings during application stop.

**4. Sequential health checks in the watchdog**
Checking N workers one by one with `GenServer.call(pid, :ping, 1_000)` means worst-case
latency is `N * 1_000` ms. For 20 workers that is 20 seconds — longer than the check
interval itself. Use `Task.async_stream` with `max_concurrency: N` to run all checks
in parallel. Total time becomes `1 * timeout` regardless of N.

---

## Resources

- [Erlang docs — `:timer` module](https://www.erlang.org/doc/man/timer.html)
- [Martin Fowler — Circuit Breaker pattern](https://martinfowler.com/bliki/CircuitBreaker.html)
- [HexDocs — GenServer callbacks](https://hexdocs.pm/elixir/GenServer.html)
- [Fred Hébert — Erlang in Anger, ch. 4](https://www.erlang-in-anger.com/) — error handling patterns (free PDF)
