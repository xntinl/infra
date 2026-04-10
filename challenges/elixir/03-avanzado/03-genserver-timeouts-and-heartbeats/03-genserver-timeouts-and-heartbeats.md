# 3. GenServer Timeouts, Heartbeats & Circuit Breakers

**Difficulty**: Avanzado

## Prerequisites
- Mastered: GenServer callbacks, `handle_info/2`, `handle_call/3`
- Mastered: Process linking, monitoring — `Process.monitor/1`, `Process.link/1`
- Familiarity with: Circuit breaker pattern, health-check systems, OTP supervision trees

## Learning Objectives
- Analyze the difference between GenServer call timeouts, inactivity timeouts, and heartbeat intervals
- Design a watchdog process that monitors a pool of GenServers and restarts unhealthy ones
- Evaluate circuit breaker state transitions and when to open, half-open, and close the breaker
- Implement `process_flag(:trap_exit, true)` to handle linked process deaths gracefully

## Concepts

### Three Distinct Timeout Mechanisms

In GenServer, "timeout" is overloaded — it refers to three different mechanisms with
different semantics:

**1. Call timeout** — the caller-side deadline. `GenServer.call(pid, msg, 5_000)` means
"if no reply arrives in 5 seconds, raise `{:timeout, ...}` in the calling process."
The GenServer itself is unaffected — it may still process the call and send a reply into
a dead mailbox. This is a client-side concern.

**2. Inactivity timeout** — the server-side idle detector. Returning
`{:reply, val, state, 30_000}` from a callback schedules a `:timeout` message to be
delivered to the GenServer after 30 seconds of no messages. When `handle_info(:timeout, state)`
fires, the process knows it has been idle. Use this for cleanup, hibernation, or self-termination.

**3. Explicit timer** — `:timer.send_interval/2` or `:timer.send_after/2`. These are
Erlang timer wheel entries that send an arbitrary message to a process on a schedule.
Use this for heartbeats, periodic polling, and TTL expiry.

```elixir
defmodule TimeoutDemo do
  use GenServer

  @idle_ms 30_000

  def init(_) do
    # Schedule a heartbeat every 5 seconds
    {:ok, timer_ref} = :timer.send_interval(5_000, :heartbeat)
    {:ok, %{timer: timer_ref, last_beat: nil}, @idle_ms}
  end

  # Inactivity timeout — no external messages for 30s
  def handle_info(:timeout, state) do
    {:stop, :normal, state}
  end

  # Periodic heartbeat from :timer
  def handle_info(:heartbeat, state) do
    # Still return with idle timeout to reset the inactivity clock
    {:noreply, %{state | last_beat: DateTime.utc_now()}, @idle_ms}
  end
end
```

### Trapping Exits for Graceful Dependency Monitoring

By default, if a process linked to a GenServer exits with a non-normal reason, the
GenServer is killed. Setting `Process.flag(:trap_exit, true)` in `init/1` converts
linked process deaths into messages: `{:EXIT, pid, reason}` delivered to `handle_info`.
This lets the GenServer react to dependency failures without crashing itself.

Use trap_exit when:
- The GenServer manages a pool of workers and needs to restart failed workers
- The GenServer holds a connection and needs to reconnect on link failure
- You need to distinguish between intentional shutdown (`:normal`) and crashes

Do NOT trap exits blindly — it suppresses supervisor kill signals and can prevent
clean shutdown in some OTP patterns.

```elixir
def init(_opts) do
  Process.flag(:trap_exit, true)
  workers = spawn_and_link_workers(10)
  {:ok, %{workers: workers}}
end

def handle_info({:EXIT, dead_pid, reason}, state) do
  require Logger
  Logger.warning("Worker #{inspect(dead_pid)} died: #{inspect(reason)}")
  new_worker = spawn_and_link_worker()
  workers = [new_worker | List.delete(state.workers, dead_pid)]
  {:noreply, %{state | workers: workers}}
end
```

### Circuit Breaker Pattern

A circuit breaker protects a GenServer (or any client) from repeatedly calling a failing
dependency. It tracks recent failures and transitions through three states:

- **Closed** (normal): calls pass through. Failures increment a counter.
- **Open**: failure threshold exceeded. Calls fail immediately without hitting the dependency.
- **Half-open**: after a recovery window, one probe call is allowed. If it succeeds,
  the breaker closes; if it fails, it opens again.

Implementing this inside a GenServer gives you a single-writer, race-free state machine:

```elixir
defmodule CircuitBreaker do
  use GenServer

  @failure_threshold 5
  @recovery_window_ms 30_000

  defstruct state: :closed, failures: 0, opened_at: nil

  def call(server, fun) do
    GenServer.call(server, {:call, fun})
  end

  def handle_call({:call, fun}, _from, %{state: :open} = cb) do
    if recovered?(cb) do
      execute_probe(fun, cb)
    else
      {:reply, {:error, :circuit_open}, cb}
    end
  end

  def handle_call({:call, fun}, _from, %{state: :closed} = cb) do
    case safe_call(fun) do
      {:ok, result} ->
        {:reply, {:ok, result}, %{cb | failures: 0}}

      {:error, reason} ->
        new_cb = record_failure(cb)
        {:reply, {:error, reason}, new_cb}
    end
  end

  defp record_failure(%{failures: f} = cb) when f + 1 >= @failure_threshold do
    %{cb | state: :open, failures: f + 1, opened_at: System.monotonic_time(:millisecond)}
  end
  defp record_failure(cb), do: %{cb | failures: cb.failures + 1}

  defp recovered?(cb) do
    now = System.monotonic_time(:millisecond)
    now - cb.opened_at >= @recovery_window_ms
  end

  defp safe_call(fun) do
    {:ok, fun.()}
  rescue
    e -> {:error, e}
  end

  # ... execute_probe implementation
end
```

### Trade-offs

| Mechanism | Use Case | Pitfall |
|---|---|---|
| Call timeout (client-side) | Prevent caller from hanging forever | GenServer still processes; orphan replies |
| Inactivity timeout (server-side) | Cleanup idle processes | Heartbeats reset the clock — may never fire |
| `:timer.send_interval` | Regular heartbeats, polling | Does not auto-cancel — must store ref and cancel |
| `trap_exit` | Survive linked process deaths | Suppresses supervisor kill signals |
| Circuit breaker | Prevent cascade failures | Half-open probe logic is subtle to implement correctly |

---

## Exercises

### Exercise 1: Self-Terminating Idle GenServer

**Problem**: You have a `JobProcessor` GenServer created on-demand by a
`DynamicSupervisor` for each incoming job batch. Once the batch is processed, the
GenServer should terminate itself after 30 seconds of inactivity — no external
cleanup call required. If a new job arrives before the 30s window, the timer resets.

**Requirements**:
- GenServer terminates with `:normal` after 30s idle
- Receiving any `handle_call` or `handle_cast` resets the idle timer
- On termination, log `"JobProcessor #{id} shutting down after idle timeout"`
- Test: spawn the GenServer, wait 35s, verify the process is dead

**Hints**:
- Return `{:noreply, state, 30_000}` from every callback — this is how the timer resets
- `handle_info(:timeout, state)` should call `{:stop, :normal, state}` — GenServer
  will call `terminate/2` and then the process exits cleanly
- The DynamicSupervisor handles the exit; since it's `:normal`, no restart occurs
- To verify the process is dead in tests: `assert not Process.alive?(pid)` after sleeping

**One possible solution**:
```elixir
defmodule JobProcessor do
  use GenServer
  require Logger

  @idle_timeout 30_000

  def start_link(batch_id) do
    GenServer.start_link(__MODULE__, batch_id)
  end

  def process_job(pid, job), do: GenServer.cast(pid, {:job, job})
  def get_results(pid), do: GenServer.call(pid, :results, 5_000)

  def init(batch_id) do
    Logger.info("JobProcessor #{batch_id} started")
    {:ok, %{id: batch_id, results: [], count: 0}, @idle_timeout}
  end

  def handle_cast({:job, job}, state) do
    result = do_work(job)
    new_state = %{state | results: [result | state.results], count: state.count + 1}
    {:noreply, new_state, @idle_timeout}
  end

  def handle_call(:results, _from, state) do
    {:reply, state.results, state, @idle_timeout}
  end

  def handle_info(:timeout, state) do
    Logger.info("JobProcessor #{state.id} shutting down after idle timeout")
    {:stop, :normal, state}
  end

  def terminate(:normal, state) do
    Logger.info("JobProcessor #{state.id} terminated (processed #{state.count} jobs)")
    :ok
  end

  defp do_work(job), do: {:result, job, System.monotonic_time()}
end
```

---

### Exercise 2: Dependency Heartbeat Monitor

**Problem**: Your `DatabaseProxy` GenServer wraps a database connection. Every 5 seconds
it must ping the database. If 3 consecutive pings fail, the proxy marks itself as
`:unhealthy` and returns `{:error, :db_unavailable}` to all callers until the database
recovers. When a ping succeeds after a failure, the proxy resets its failure counter
and marks itself `:healthy` again.

**Requirements**:
- Heartbeat every 5 seconds using `:timer.send_interval/2`
- Track `consecutive_failures` in state
- After 3 failures: transition `health` field to `:unhealthy`
- Any `handle_call` to `:unhealthy` proxy returns `{:error, :db_unavailable}` immediately
- Recovery: a successful heartbeat resets `consecutive_failures` to 0 and sets `health: :healthy`
- Expose `health_status/1` function that returns `{:healthy | :unhealthy, failures: n}`

**Hints**:
- Store the timer ref returned by `:timer.send_interval/2` in state so you can cancel
  it in `terminate/2` with `:timer.cancel/1`
- The `ping_db/1` function should be simulated: return `:ok` 70% of the time and
  `{:error, :timeout}` 30% — use `:rand.uniform(10) < 7` for simulation
- Pattern match on `state.health` at the top of `handle_call` clauses to implement
  the fail-fast behavior cleanly

**One possible solution**:
```elixir
defmodule DatabaseProxy do
  use GenServer
  require Logger

  @heartbeat_interval 5_000
  @failure_threshold 3

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def query(sql), do: GenServer.call(__MODULE__, {:query, sql})
  def health_status, do: GenServer.call(__MODULE__, :health_status)

  def init(_opts) do
    {:ok, timer} = :timer.send_interval(@heartbeat_interval, :heartbeat)
    state = %{
      health: :healthy,
      consecutive_failures: 0,
      timer: timer
    }
    {:ok, state}
  end

  def handle_call({:query, _sql}, _from, %{health: :unhealthy} = state) do
    {:reply, {:error, :db_unavailable}, state}
  end

  def handle_call({:query, sql}, _from, state) do
    result = execute_query(sql)
    {:reply, result, state}
  end

  def handle_call(:health_status, _from, state) do
    status = {state.health, failures: state.consecutive_failures}
    {:reply, status, state}
  end

  def handle_info(:heartbeat, state) do
    new_state =
      case ping_db() do
        :ok ->
          if state.consecutive_failures > 0 do
            Logger.info("DatabaseProxy recovered after #{state.consecutive_failures} failures")
          end
          %{state | health: :healthy, consecutive_failures: 0}

        {:error, reason} ->
          failures = state.consecutive_failures + 1
          Logger.warning("DatabaseProxy ping failed (#{failures}): #{inspect(reason)}")
          health = if failures >= @failure_threshold, do: :unhealthy, else: state.health
          %{state | consecutive_failures: failures, health: health}
      end

    {:noreply, new_state}
  end

  def terminate(_reason, state) do
    :timer.cancel(state.timer)
  end

  defp ping_db do
    if :rand.uniform(10) < 7, do: :ok, else: {:error, :timeout}
  end

  defp execute_query(sql), do: {:ok, "result_for_#{sql}"}
end
```

---

### Exercise 3: Watchdog GenServer

**Problem**: You run a pool of 10 `Cache.Worker` processes (from Exercise 01). Build a
`Watchdog` GenServer that monitors all 10 workers. Every 10 seconds, it sends a health
check (`GenServer.call` with a 1-second timeout) to each worker. Workers that do not
respond in time are killed and restarted via `DynamicSupervisor`. The watchdog must
itself be resilient: if a restart fails, it logs the error and skips that worker for
the next cycle.

**Requirements**:
- Watchdog has a registry of `{worker_id, pid}` pairs
- Health check: `GenServer.call(pid, :ping, 1_000)` — workers implement `handle_call(:ping, ...)`
- Unresponsive workers: kill with `Process.exit(pid, :kill)`, restart via supervisor,
  update registry with new pid
- Failed restarts: log error, remove from registry (do not leave zombie entries)
- Expose `registry/1` so tests can inspect current pid assignments

**Hints**:
- Use `Task.async_stream/3` to health-check all workers in parallel — sequential checks
  would take up to `10 * 1s = 10s` per cycle, which is too slow
- Catch `{:exit, {:timeout, _}}` from Task results to identify unresponsive workers
- `DynamicSupervisor.start_child(MySupervisor, {Cache.Worker, {id, nil}})` returns
  `{:ok, new_pid}` or `{:error, reason}` — handle both
- The watchdog itself should be started under a different supervisor than the workers
  it monitors, so it is not affected by worker crashes

**One possible solution**:
```elixir
defmodule Watchdog do
  use GenServer
  require Logger

  @check_interval 10_000
  @call_timeout 1_000

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def registry, do: GenServer.call(__MODULE__, :registry)

  def init(%{supervisor: sup, workers: initial_workers}) do
    {:ok, timer} = :timer.send_interval(@check_interval, :health_check)
    state = %{supervisor: sup, registry: Map.new(initial_workers), timer: timer}
    {:ok, state}
  end

  def handle_call(:registry, _from, state) do
    {:reply, state.registry, state}
  end

  def handle_info(:health_check, state) do
    updated_registry =
      state.registry
      |> Task.async_stream(
        fn {id, pid} -> {id, pid, check_worker(pid)} end,
        max_concurrency: 10,
        timeout: @call_timeout + 500
      )
      |> Enum.reduce(state.registry, fn result, registry ->
        handle_check_result(result, registry, state.supervisor)
      end)

    {:noreply, %{state | registry: updated_registry}}
  end

  defp check_worker(pid) do
    try do
      GenServer.call(pid, :ping, @call_timeout)
      :healthy
    catch
      :exit, _ -> :unresponsive
    end
  end

  defp handle_check_result({:ok, {id, pid, :healthy}}, registry, _sup) do
    registry
  end

  defp handle_check_result({:ok, {id, pid, :unresponsive}}, registry, sup) do
    Logger.warning("Watchdog: worker #{id} (#{inspect(pid)}) unresponsive — restarting")
    Process.exit(pid, :kill)

    case DynamicSupervisor.start_child(sup, {Cache.Worker, {id, nil}}) do
      {:ok, new_pid} ->
        Logger.info("Watchdog: worker #{id} restarted as #{inspect(new_pid)}")
        Map.put(registry, id, new_pid)

      {:error, reason} ->
        Logger.error("Watchdog: failed to restart worker #{id}: #{inspect(reason)}")
        Map.delete(registry, id)
    end
  end

  defp handle_check_result({:exit, reason}, registry, _sup) do
    Logger.error("Watchdog: health check task crashed: #{inspect(reason)}")
    registry
  end
end
```

---

## Common Mistakes

### Mistake: Confusing Call Timeout With Server Inactivity Timeout

`GenServer.call(pid, msg, 5_000)` and `{:reply, val, state, 5_000}` are completely
different mechanisms. The first is a client-side deadline that raises in the caller.
The second is a server-side inactivity detector that sends `:timeout` to the server.
Mixing them up leads to debugging sessions where the server seems to timeout
immediately — often because someone returned `{:reply, val, state, 1_000}` thinking
it sets the call deadline, when it actually makes the server self-message in 1s.

### Mistake: Not Cancelling :timer.send_interval in terminate/2

`:timer.send_interval/2` creates an entry in the Erlang timer wheel. When the process
dies, the timer is NOT automatically cancelled unless explicitly done in `terminate/2`
with `:timer.cancel(ref)`. On a restarted process, this means the old timer keeps firing
and sending messages to a dead pid — which are silently dropped but accumulate in the
timer wheel, creating a small memory and performance drain over time.

### Mistake: Trapping Exits Without Handling All Exit Reasons

`Process.flag(:trap_exit, true)` converts ALL linked process exits into messages,
including `{:EXIT, pid, :normal}`. If your supervisor sends a shutdown exit signal
(`:shutdown` or `{:shutdown, reason}`) to the GenServer, and you only handle `:normal`,
the `:shutdown` message accumulates in the mailbox unhandled. This delays clean
shutdown and can cause OTP timeout warnings during application stop.

### Mistake: Sequential Health Checks in Watchdog

Checking N workers one by one with `GenServer.call` makes the total time proportional
to `N * timeout`. For 10 workers with a 1-second timeout, worst case is 10 seconds —
which exceeds the check interval itself. Use `Task.async_stream` with
`max_concurrency: N` to run all checks in parallel. Total time becomes `1 * timeout`
regardless of N (up to your concurrency limit).

---

## Summary
- GenServer timeout flavors are distinct: call timeout (client), inactivity timeout
  (server idle), and explicit timer (`:timer.send_interval`) — never conflate them
- `trap_exit` converts crashes into messages; use it when you need to react to linked
  process death without dying yourself — but handle all exit reasons
- Circuit breakers belong in the GenServer that wraps the dependency, giving a
  race-free state machine for open/closed/half-open transitions
- Health check watchdogs must use parallel checks (Task.async_stream) to avoid
  O(N) worst-case timing

## What's Next
Exercise 04 — Back-pressure with Internal Queues: combine GenServer with `:queue`
to build bounded, priority-aware work queues that prevent overload.

## Resources
- Erlang docs — `:timer` module: https://www.erlang.org/doc/man/timer.html
- Martin Fowler — Circuit Breaker pattern: https://martinfowler.com/bliki/CircuitBreaker.html
- Erlang/OTP — `proc_lib` and trap_exit semantics
- Fred Hébert — "Stuff Goes Bad: Erlang in Anger" ch. 4 (error handling)
