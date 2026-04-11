# GenServer Hibernation & State Compaction

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`, an internal HTTP gateway. The circuit breaker component
(a previous exercise) spawns one `CircuitBreaker.Worker` process per upstream service.
At peak the gateway tracks 5,000 upstream services. Profiling shows that at any moment
only ~200 workers are actively handling traffic — the other 4,800 are idle, each
consuming ~4 KB of heap. Your ops team is asking why the gateway eats 200 MB of process
heap even when traffic is low.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── circuit_breaker/
│           ├── worker.ex          # ← you implement this
│           └── supervisor.ex      # already exists
├── test/
│   └── api_gateway/
│       └── circuit_breaker/
│           └── worker_test.exs    # given tests — must pass without modification
├── bench/
│   └── hibernation_bench.exs      # benchmark — run at the end
└── mix.exs
```

---

## The business problem

The infra team wants to scale the gateway to 50,000 upstream services. At 4 KB per idle
process that is 200 MB of wasted heap. The solution: `:hibernate` idle workers after
30 seconds of inactivity. A hibernated process runs a full GC on its heap and suspends
until the next message arrives.

---

## Why hibernate and not just kill idle workers

Killing and re-creating a worker on demand involves re-fetching configuration from
the config service (~50 ms), reloading connection pool handles, and re-registering in
the circuit breaker registry. Hibernation preserves all that state at minimal memory
cost. The worker wakes up in microseconds — not milliseconds.

The cost is real: the first message after waking incurs a cold-heap penalty. In
latency-sensitive hot paths, this manifests as P99 spikes. You must measure this
before shipping hibernation to production.

---

## Why state compaction matters

Hibernation runs GC on the current heap. If the state still holds large binaries,
reference-counted sub-binaries, or ETS references pointing to large structures, GC
cannot collect them — memory stays high even after hibernation. Compaction means
explicitly reducing the state to its smallest meaningful representation before
calling `:hibernate`.

```
BEFORE compaction + hibernate:
  state = %{
    service: "payments",
    config: %{...large map, 2 KB...},
    connection_pool: #Reference<...>,
    request_log: [... 500 entries ...],   # 50 KB
    last_error: %RuntimeError{...},
    metrics: %{p99: 12.4, p50: 3.1, ...}
  }
  heap after hibernate: ~52 KB  (log still referenced)

AFTER compaction + hibernate:
  state = %{
    service: "payments",
    status: :open,
    failure_count: 3,
    last_check: 1_712_000_000_000
  }
  heap after hibernate: ~0.5 KB
```

---

## Implementation

### Step 1: `mix.exs` — add recon as a dev dependency

```elixir
# mix.exs
defp deps do
  [
    {:recon, "~> 2.5", only: :dev}
  ]
end
```

### Step 2: `lib/api_gateway/circuit_breaker/worker.ex`

`# TODO` marks what you must implement. `# HINT` gives direction without spoiling
the solution. Do not change the public function signatures — the tests depend on them.

```elixir
defmodule ApiGateway.CircuitBreaker.Worker do
  use GenServer
  require Logger

  @hibernate_after_ms 30_000

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
    # TODO: build initial state and schedule inactivity timeout
    #
    # State fields to include:
    #   :service        — the upstream service name
    #   :status         — :closed | :open | :half_open
    #   :failures       — consecutive failure count
    #   :hibernations   — how many times this worker hibernated (for tests)
    #
    # HINT: return {:ok, state, @hibernate_after_ms} to arm the inactivity timer.
    # The BEAM will send :timeout to handle_info/2 if no message arrives in that window.
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call(:status, _from, state) do
    # TODO: reply with state.status and reset the inactivity timer
    # HINT: {:reply, value, state, @hibernate_after_ms}
  end

  @impl true
  def handle_call(:hibernation_count, _from, state) do
    # TODO: reply with state.hibernations (no timer reset needed here)
  end

  @impl true
  def handle_cast(:success, state) do
    # TODO: reset failures to 0, transition to :closed if :half_open, reset timer
  end

  @impl true
  def handle_cast(:failure, state) do
    # TODO: increment failures; if >= 5 transition to :open; reset timer
    # HINT: use a module attribute @failure_threshold 5
  end

  @impl true
  def handle_info(:timeout, state) do
    # Inactivity timeout fired — compact state and hibernate.
    #
    # HINT: call a private compact/1 function that drops fields which can be
    # recomputed on wake (e.g., derived caches, large logs).
    # Then return {:noreply, compacted_state, :hibernate}.
    #
    # Design question: what fields are safe to drop vs. unsafe to drop?
    # Answer: anything you can recompute from service_name + status + failures.
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp compact(state) do
    # TODO: return the smallest state that preserves correctness.
    # Increment :hibernations so tests can assert hibernation happened.
  end
end
```

### Step 3: Given tests — must pass without modification

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
      # Force immediate hibernation by sending :timeout directly
      send(pid, :timeout)
      Process.sleep(20)

      # Worker must still respond after waking
      assert Worker.status(pid) == :closed
      assert Worker.hibernation_count(pid) == 1
    end

    test "state is preserved across hibernation" do
      {:ok, pid} = Worker.start_link("auth")
      for _ <- 1..3, do: Worker.record_failure(pid)
      Process.sleep(10)

      # Hibernate
      send(pid, :timeout)
      Process.sleep(20)

      # Failure count must survive hibernation
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

### Step 4: Run the tests

```bash
mix test test/api_gateway/circuit_breaker/worker_test.exs --trace
```

All tests fail initially — that is expected. Implement `Worker` until they all pass.

### Step 5: Measure memory savings

Once tests pass, measure the impact using `:recon`:

```elixir
# In iex -S mix
alias ApiGateway.CircuitBreaker.Worker

# Spawn 200 workers
workers = for i <- 1..200 do
  {:ok, pid} = Worker.start_link("service_#{i}")
  pid
end

# Baseline memory
baseline = :recon.proc_count(:memory, 5)
IO.inspect(baseline, label: "top 5 by memory (before hibernation)")

# Force hibernation on all
Enum.each(workers, fn pid -> send(pid, :timeout) end)
Process.sleep(200)

# Post-hibernate memory
after_hib = :recon.proc_count(:memory, 5)
IO.inspect(after_hib, label: "top 5 by memory (after hibernation)")
```

### Step 6: Measure P99 latency impact

```elixir
# bench/hibernation_bench.exs
workers = for i <- 1..50 do
  {:ok, pid} = ApiGateway.CircuitBreaker.Worker.start_link("bench_#{i}")
  pid
end

# Warm up
Enum.each(workers, &ApiGateway.CircuitBreaker.Worker.status/1)

# Baseline P99
latencies_baseline =
  for _ <- 1..10_000 do
    w = Enum.random(workers)
    {us, _} = :timer.tc(fn -> ApiGateway.CircuitBreaker.Worker.status(w) end)
    us
  end

# Force hibernation
Enum.each(workers, fn pid -> send(pid, :timeout) end)
Process.sleep(100)

# Post-hibernation first-call P99
latencies_wake =
  for w <- workers do
    {us, _} = :timer.tc(fn -> ApiGateway.CircuitBreaker.Worker.status(w) end)
    us
  end

p99 = fn list ->
  sorted = Enum.sort(list)
  Enum.at(sorted, floor(length(sorted) * 0.99))
end

IO.puts("Baseline P99:       #{p99.(latencies_baseline)} µs")
IO.puts("Post-hibernate P99: #{p99.(latencies_wake)} µs")
IO.puts("Overhead:           #{p99.(latencies_wake) - p99.(latencies_baseline)} µs")
```

```bash
mix run bench/hibernation_bench.exs
```

**Expected result**: post-hibernate P99 is 50–500 µs higher than baseline.
If the delta is < 10 µs, verify hibernation is actually happening (check `hibernation_count/1`).

---

## Trade-off analysis

Fill this table after running the benchmark.

| Aspect | With hibernation + compaction | Without hibernation | Notes |
|--------|-------------------------------|---------------------|-------|
| Heap per idle worker | ~0.5 KB (estimate) | ~4 KB | Measure with `:recon` |
| Memory for 5,000 idle workers | estimate | ~20 MB | |
| First-call P99 after wake | measure | baseline | Your benchmark |
| Subsequent call P99 | baseline | baseline | |
| Code complexity | Medium (compact/1 logic) | Low | |
| Risk | Low if fields are safe to drop | None | |

Reflection question: what fields in the circuit breaker state are **unsafe** to drop
during compaction? What would happen if you dropped `:failures` by mistake?

---

## Common production mistakes

**1. Using `:timer.send_after` instead of the built-in timeout**
Calling `:timer.send_after(@delay, self(), :timeout)` in every callback and cancelling
the previous reference is error-prone. If one callback forgets to cancel, phantom timers
accumulate. The built-in GenServer timeout (`{:reply, val, state, ms}`) resets itself
automatically on every callback return — zero timer leak risk.

**2. Not compacting before hibernating**
A process holding a 500-entry request log in state will hibernate — but the log is still
referenced from the heap. GC cannot collect it. Memory stays high. Compaction is not
optional: explicitly drop or truncate anything large before calling `:hibernate`.

**3. Hibernating processes that receive frequent messages**
If a circuit breaker worker handles 50 req/s, the inactivity timeout never fires in
practice — good. But if you set the threshold too low (e.g., 500 ms) on a bursty
service (quiet for 600 ms, then a burst), you create a pathological pattern:
hibernate → burst → wake (latency spike) → hibernate. Profile traffic patterns before
choosing the threshold.

**4. Assuming hibernate is free on wake**
The first call to a hibernated process must rebuild the process stack and may trigger
OS paging if the process memory was swapped. On loaded systems, post-hibernation P99
can be 10× higher than baseline. Always measure on hardware similar to production.

**5. Reference-counted binaries defeating compaction**
A state field like `last_request_body: binary` may point into a large shared binary on
the heap. Even after compaction (removing the field), the reference keeps the binary
alive. Use `:erlang.process_info(pid, :binary)` to audit binary references before
and after compaction.

---

## Resources

- [`:erlang.hibernate/3` — Erlang/OTP docs](https://www.erlang.org/doc/man/erlang.html#hibernate-3)
- [`:recon` — Fred Hébert](https://ferd.github.io/recon/) — production-safe introspection
- [Erlang in Anger — Fred Hébert](https://www.erlang-in-anger.com/) — chapter on process memory (free PDF)
- [BEAM Wisdoms — Process Memory Layout](http://beam-wisdoms.clau.se/en/latest/eli5-memory.html)
- [Saša Jurić — Elixir in Action, 2nd ed.](https://www.manning.com/books/elixir-in-action-second-edition) — ch. 12, process internals
