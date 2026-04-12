# GenServer Timeouts, Heartbeats & Circuit Breaker State Machine

## Goal

Build a circuit breaker worker with a heartbeat that probes open circuits for recovery, and a watchdog process that health-checks a pool of workers in parallel and restarts unresponsive ones. This exercise covers three distinct timeout mechanisms in GenServer and the full three-state circuit breaker machine.

---

## Three distinct timeout mechanisms

The word "timeout" is overloaded in GenServer. Conflating these causes hard-to-debug production incidents.

**1. Call timeout (caller-side deadline)**
`GenServer.call(pid, msg, 5_000)` raises `{:timeout, ...}` in the *calling* process if no reply arrives in 5 seconds. The GenServer itself is unaffected.

**2. Inactivity timeout (server-side idle detector)**
Returning `{:reply, val, state, 30_000}` from a callback schedules a `:timeout` message to the GenServer after 30 seconds of no messages. Resets automatically on every callback return.

**3. Explicit timer (scheduled messages)**
`:timer.send_interval/2` or `Process.send_after/3` inject arbitrary messages into the mailbox on a schedule. Unlike the inactivity timeout, explicit timers do NOT reset on message receipt -- they fire unconditionally.

```
Mechanism          | Where it runs   | Resets on msg?  | Cancels itself?
-------------------+-----------------+-----------------+----------------
Call timeout       | Caller process  | N/A             | On reply
Inactivity timer   | GenServer       | Yes             | Never (fires once)
send_interval      | Timer wheel     | No              | Only with cancel/1
```

---

## Circuit breaker state machine

```
           failures >= threshold
:closed ----------------------------------------> :open
   ^                                                |
   |  probe succeeds                                | recovery_window_ms elapsed
   |                                                v
:half_open <----------------------------------------
   |
   | probe fails -> back to :open
   +----------------------------------------------> :open
```

---

## Full implementation

### `lib/api_gateway/circuit_breaker/worker.ex`

The heartbeat is an explicit `:timer.send_interval/2` timer that fires unconditionally every `@heartbeat_ms` milliseconds. When it fires and the circuit is `:open`, the worker checks whether enough time has passed since the circuit opened. If yes, it transitions to `:half_open`. The next success closes the circuit; the next failure re-opens it.

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

  @spec record_success(pid()) :: :ok
  def record_success(pid), do: GenServer.cast(pid, :success)

  @spec record_failure(pid()) :: :ok
  def record_failure(pid), do: GenServer.cast(pid, :failure)

  @spec status(pid()) :: :closed | :open | :half_open
  def status(pid), do: GenServer.call(pid, :status)

  @spec ping(pid()) :: :pong
  def ping(pid), do: GenServer.call(pid, :ping, 1_000)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(service_name) do
    {:ok, timer_ref} = :timer.send_interval(@heartbeat_ms, :heartbeat)

    state = %{
      service: service_name,
      status: :closed,
      failures: 0,
      opened_at: nil,
      timer_ref: timer_ref
    }

    {:ok, state}
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
    {:reply, :pong, state}
  end

  @impl true
  def handle_cast(:success, state) do
    new_status =
      case state.status do
        :half_open ->
          Logger.info("Circuit closed for #{state.service}")
          :closed
        other ->
          other
      end

    {:noreply, %{state | failures: 0, status: new_status}}
  end

  @impl true
  def handle_cast(:failure, state) do
    new_failures = state.failures + 1

    new_state =
      case state.status do
        :closed when new_failures >= @failure_threshold ->
          Logger.warning("Circuit opened for #{state.service} after #{new_failures} failures")
          %{state | failures: new_failures, status: :open, opened_at: System.monotonic_time(:millisecond)}

        :half_open ->
          Logger.warning("Circuit re-opened for #{state.service} (probe failed)")
          %{state | failures: new_failures, status: :open, opened_at: System.monotonic_time(:millisecond)}

        _ ->
          %{state | failures: new_failures}
      end

    {:noreply, new_state}
  end

  @impl true
  def handle_info(:heartbeat, %{status: :open} = state) do
    elapsed = System.monotonic_time(:millisecond) - state.opened_at

    if elapsed >= @recovery_window_ms do
      Logger.info("Probing #{state.service} -- transitioning to half_open")
      {:noreply, %{state | status: :half_open}}
    else
      {:noreply, state}
    end
  end

  @impl true
  def handle_info(:heartbeat, state) do
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, state) do
    :timer.cancel(state.timer_ref)
    :ok
  end
end
```

### `lib/api_gateway/circuit_breaker/watchdog.ex`

The watchdog periodically health-checks all circuit breaker workers in parallel. If a worker is unresponsive, the watchdog kills it and asks the DynamicSupervisor to restart it.

```elixir
defmodule ApiGateway.CircuitBreaker.Watchdog do
  use GenServer
  require Logger

  @check_interval_ms 10_000
  @ping_timeout_ms   1_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns current {service_name => pid} registry."
  @spec registry() :: %{String.t() => pid()}
  def registry, do: GenServer.call(__MODULE__, :registry)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    supervisor = Keyword.fetch!(opts, :supervisor)
    registry   = Keyword.get(opts, :registry, %{})

    {:ok, _ref} = :timer.send_interval(@check_interval_ms, :health_check)
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
    updated_registry =
      state.registry
      |> Task.async_stream(
        fn {name, pid} -> {name, pid, check_worker(pid)} end,
        max_concurrency: max(map_size(state.registry), 1),
        timeout: @ping_timeout_ms + 500,
        on_timeout: :kill_task
      )
      |> Enum.reduce(state.registry, fn result, reg ->
        handle_check_result(result, reg, state.supervisor)
      end)

    {:noreply, %{state | registry: updated_registry}}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp check_worker(pid) do
    try do
      GenServer.call(pid, :ping, @ping_timeout_ms)
      :healthy
    catch
      :exit, _ -> :unresponsive
    end
  end

  defp handle_check_result({:ok, {_name, _pid, :healthy}}, registry, _sup) do
    registry
  end

  defp handle_check_result({:ok, {name, pid, :unresponsive}}, registry, sup) do
    Logger.warning("Watchdog: #{name} unresponsive -- restarting")
    Process.exit(pid, :kill)

    case DynamicSupervisor.start_child(sup, {ApiGateway.CircuitBreaker.Worker, name}) do
      {:ok, new_pid} ->
        Logger.info("Watchdog: #{name} restarted as #{inspect(new_pid)}")
        Map.put(registry, name, new_pid)

      {:error, reason} ->
        Logger.error("Watchdog: failed to restart #{name}: #{inspect(reason)}")
        Map.delete(registry, name)
    end
  end

  defp handle_check_result({:exit, reason}, registry, _sup) do
    Logger.error("Watchdog: health check task crashed: #{inspect(reason)}")
    registry
  end
end
```

### Tests

```elixir
# test/api_gateway/circuit_breaker/worker_test.exs
defmodule ApiGateway.CircuitBreaker.WorkerTest do
  use ExUnit.Case, async: true

  alias ApiGateway.CircuitBreaker.Worker

  describe "heartbeat-driven recovery" do
    test "transitions open -> half_open after recovery window" do
      {:ok, pid} = Worker.start_link("slow-upstream")

      for _ <- 1..5, do: Worker.record_failure(pid)
      Process.sleep(10)
      assert Worker.status(pid) == :open

      :sys.replace_state(pid, fn state ->
        %{state | opened_at: System.monotonic_time(:millisecond) - 31_000}
      end)

      send(pid, :heartbeat)
      Process.sleep(20)

      assert Worker.status(pid) == :half_open
    end

    test "half_open -> closed on success" do
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

    test "half_open -> open on failure" do
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

    Process.exit(old_pid, :kill)
    Process.sleep(50)

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

---

## How it works

1. **Heartbeat via `:timer.send_interval`**: fires every 30 seconds regardless of activity. Unlike the built-in GenServer inactivity timeout, it does NOT reset on message receipt.

2. **Recovery window**: when the heartbeat fires and the circuit is `:open`, the worker checks elapsed time since opening. Only after `@recovery_window_ms` does it transition to `:half_open`.

3. **Parallel health checks**: the watchdog uses `Task.async_stream` to check all workers simultaneously. With a 1-second timeout per worker and 20 workers, parallel checks take ~1 second total vs 20 seconds sequentially.

4. **Timer cleanup**: `terminate/2` cancels the heartbeat timer to avoid orphan timer entries that persist after process death.

---

## Common production mistakes

**1. Confusing call timeout with inactivity timeout**
`GenServer.call(pid, msg, 5_000)` raises in the *caller* after 5s. `{:reply, val, state, 5_000}` fires `:timeout` in the *GenServer* after 5s of idle. Mixing them up causes impossible-to-reproduce bugs.

**2. Not cancelling `:timer.send_interval` in `terminate/2`**
The timer wheel entry persists after the process dies. Over time, dead timer entries accumulate. Always cancel in `terminate/2`.

**3. Sequential health checks in the watchdog**
Checking N workers one by one with `GenServer.call(pid, :ping, 1_000)` means worst-case latency is `N * 1_000` ms. Use `Task.async_stream` to run all checks in parallel.

---

## Resources

- [Erlang docs -- `:timer` module](https://www.erlang.org/doc/man/timer.html)
- [Martin Fowler -- Circuit Breaker pattern](https://martinfowler.com/bliki/CircuitBreaker.html)
- [HexDocs -- GenServer callbacks](https://hexdocs.pm/elixir/GenServer.html)
