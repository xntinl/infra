# GenServer Hibernation & State Compaction

## Goal

Build a `CircuitBreaker.Worker` GenServer that hibernates after a period of inactivity to reduce memory usage across thousands of idle processes. The worker implements a three-state circuit breaker (`:closed`, `:open`, `:half_open`) and compacts its state before hibernating so the BEAM garbage collector can reclaim the maximum amount of heap memory.

---

## Why hibernation matters

The BEAM allocates a private heap to every process. Even idle processes hold their last state on the heap, consuming memory. `:erlang.hibernate/3` runs a full GC on the process heap and suspends it until the next message arrives. For a gateway tracking 5,000 upstream services where only ~200 are active at any moment, hibernation can reduce heap usage from ~200 MB to ~2.5 MB for the idle processes.

The cost: the first message after waking incurs a cold-heap penalty (the process must rebuild its stack and heap). This manifests as a P99 latency spike of 50-500 microseconds. You must measure this before shipping to production.

---

## Why state compaction matters

Hibernation runs GC on the current heap, but if the state still holds large binaries, request logs, or metrics caches, GC cannot collect them. Compaction explicitly reduces the state to its smallest meaningful representation before calling `:hibernate`.

```
BEFORE compaction + hibernate:
  state = %{service: "payments", config: %{...2 KB...}, request_log: [... 500 entries ...]}
  heap after hibernate: ~52 KB  (log still referenced)

AFTER compaction + hibernate:
  state = %{service: "payments", status: :open, failure_count: 3}
  heap after hibernate: ~0.5 KB
```

---

## Key pattern: built-in GenServer timeout

Every GenServer callback can return a timeout as the last element of the return tuple. When no message arrives within that timeout, the BEAM delivers a `:timeout` message to `handle_info/2`. This is simpler and safer than managing explicit timer references with `:timer.send_after/2` because the timeout resets automatically on every callback return -- zero timer leak risk.

---

## Full implementation

### `mix.exs`

```elixir
defp deps do
  [
    {:recon, "~> 2.5", only: :dev}
  ]
end
```

### `lib/api_gateway/circuit_breaker/worker.ex`

```elixir
defmodule ApiGateway.CircuitBreaker.Worker do
  use GenServer
  require Logger

  @hibernate_after_ms 30_000
  @failure_threshold 5

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Records a successful call to the upstream service.
  Resets the inactivity timer.
  """
  @spec record_success(pid()) :: :ok
  def record_success(pid), do: GenServer.cast(pid, :success)

  @doc """
  Records a failed call. When failures exceed the threshold the circuit opens.
  Resets the inactivity timer.
  """
  @spec record_failure(pid()) :: :ok
  def record_failure(pid), do: GenServer.cast(pid, :failure)

  @doc """
  Returns the current circuit state: :closed | :open | :half_open.
  Resets the inactivity timer.
  """
  @spec status(pid()) :: :closed | :open | :half_open
  def status(pid), do: GenServer.call(pid, :status)

  @doc """
  Returns the number of times this worker has hibernated.
  Used in tests to assert hibernation happened.
  """
  @spec hibernation_count(pid()) :: non_neg_integer()
  def hibernation_count(pid), do: GenServer.call(pid, :hibernation_count)

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  def start_link(service_name) do
    GenServer.start_link(__MODULE__, service_name)
  end

  @impl true
  def init(service_name) do
    state = %{
      service: service_name,
      status: :closed,
      failures: 0,
      hibernations: 0
    }

    # The third element arms the inactivity timer. If no message arrives
    # within @hibernate_after_ms, the BEAM sends :timeout to handle_info/2.
    {:ok, state, @hibernate_after_ms}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call(:status, _from, state) do
    {:reply, state.status, state, @hibernate_after_ms}
  end

  @impl true
  def handle_call(:hibernation_count, _from, state) do
    {:reply, state.hibernations, state}
  end

  @impl true
  def handle_cast(:success, state) do
    new_status = if state.status == :half_open, do: :closed, else: state.status
    new_state = %{state | failures: 0, status: new_status}
    {:noreply, new_state, @hibernate_after_ms}
  end

  @impl true
  def handle_cast(:failure, state) do
    new_failures = state.failures + 1

    new_status =
      if new_failures >= @failure_threshold do
        :open
      else
        state.status
      end

    new_state = %{state | failures: new_failures, status: new_status}
    {:noreply, new_state, @hibernate_after_ms}
  end

  @impl true
  def handle_info(:timeout, state) do
    # Inactivity timeout fired -- compact state and hibernate.
    compacted = compact(state)
    {:noreply, compacted, :hibernate}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  # Returns the smallest state that preserves correctness.
  # In a production worker with richer state (request logs, metrics caches,
  # connection pool handles), those derived fields would be dropped here
  # and rebuilt lazily on wake.
  defp compact(state) do
    %{
      service: state.service,
      status: state.status,
      failures: state.failures,
      hibernations: state.hibernations + 1
    }
  end
end
```

### Tests

```elixir
# test/api_gateway/circuit_breaker/worker_test.exs
defmodule ApiGateway.CircuitBreaker.WorkerTest do
  use ExUnit.Case, async: true

  alias ApiGateway.CircuitBreaker.Worker

  describe "normal operation" do
    test "starts closed" do
      {:ok, pid} = Worker.start_link("payments")
      assert Worker.status(pid) == :closed
    end

    test "opens after 5 consecutive failures" do
      {:ok, pid} = Worker.start_link("inventory")
      for _ <- 1..5, do: Worker.record_failure(pid)
      Process.sleep(10)
      assert Worker.status(pid) == :open
    end

    test "success resets failure count" do
      {:ok, pid} = Worker.start_link("shipping")
      for _ <- 1..3, do: Worker.record_failure(pid)
      Worker.record_success(pid)
      Process.sleep(10)
      assert Worker.status(pid) == :closed
    end
  end

  describe "hibernation" do
    test "hibernates after inactivity and wakes correctly" do
      {:ok, pid} = Worker.start_link("dormant-service")
      send(pid, :timeout)
      Process.sleep(20)

      assert Worker.status(pid) == :closed
      assert Worker.hibernation_count(pid) == 1
    end

    test "state is preserved across hibernation" do
      {:ok, pid} = Worker.start_link("auth")
      for _ <- 1..3, do: Worker.record_failure(pid)
      Process.sleep(10)

      send(pid, :timeout)
      Process.sleep(20)

      # Two more failures should open the circuit (3 + 2 = 5)
      for _ <- 1..2, do: Worker.record_failure(pid)
      Process.sleep(10)
      assert Worker.status(pid) == :open
    end

    test "hibernation count increments on each hibernate" do
      {:ok, pid} = Worker.start_link("catalog")
      send(pid, :timeout)
      Process.sleep(20)
      send(pid, :timeout)
      Process.sleep(20)
      assert Worker.hibernation_count(pid) == 2
    end
  end
end
```

### Measuring memory savings with `:recon`

```elixir
# In iex -S mix
alias ApiGateway.CircuitBreaker.Worker

workers = for i <- 1..200 do
  {:ok, pid} = Worker.start_link("service_#{i}")
  pid
end

baseline = :recon.proc_count(:memory, 5)
IO.inspect(baseline, label: "top 5 by memory (before hibernation)")

Enum.each(workers, fn pid -> send(pid, :timeout) end)
Process.sleep(200)

after_hib = :recon.proc_count(:memory, 5)
IO.inspect(after_hib, label: "top 5 by memory (after hibernation)")
```

---

## How it works

1. **Inactivity timer**: every callback returns `@hibernate_after_ms` as the last element. The BEAM resets this countdown on every callback invocation. If no message arrives within 30 seconds, a `:timeout` message is delivered to `handle_info/2`.

2. **Compaction**: `compact/1` creates a new map containing only the essential fields. In a production worker with request logs, metrics caches, and connection pool handles, those derived fields would be dropped here and rebuilt lazily when the worker wakes.

3. **Hibernate**: returning `{:noreply, compacted_state, :hibernate}` tells the BEAM to run a full GC on the process heap and suspend until the next message arrives. The first message after waking rebuilds the stack -- this is the cold-heap penalty.

4. **Automatic reset**: the built-in GenServer timeout resets on every callback return. No explicit timer cancellation is needed, unlike `:timer.send_after/2` which requires manual management and risks phantom timer accumulation.

---

## Common production mistakes

**1. Using `:timer.send_after` instead of the built-in timeout**
Calling `:timer.send_after(@delay, self(), :timeout)` in every callback and cancelling the previous reference is error-prone. If one callback forgets to cancel, phantom timers accumulate.

**2. Not compacting before hibernating**
A process holding a 500-entry request log hibernates, but the log is still referenced from the heap. GC cannot collect it. Compaction is not optional.

**3. Hibernating processes that receive frequent messages**
If a worker handles 50 req/s, the inactivity timeout never fires -- good. But if you set the threshold too low on a bursty service, you create a pathological pattern: hibernate -> burst -> wake (latency spike) -> hibernate.

**4. Reference-counted binaries defeating compaction**
A state field like `last_request_body: binary` may point into a large shared binary. Even after compaction (removing the field), the reference keeps the binary alive. Use `:erlang.process_info(pid, :binary)` to audit binary references.

---

## Resources

- [`:erlang.hibernate/3` -- Erlang/OTP docs](https://www.erlang.org/doc/man/erlang.html#hibernate-3)
- [`:recon` -- Fred Hebert](https://ferd.github.io/recon/)
- [Erlang in Anger -- Fred Hebert](https://www.erlang-in-anger.com/) -- chapter on process memory
- [BEAM Wisdoms -- Process Memory Layout](http://beam-wisdoms.clau.se/en/latest/eli5-memory.html)
