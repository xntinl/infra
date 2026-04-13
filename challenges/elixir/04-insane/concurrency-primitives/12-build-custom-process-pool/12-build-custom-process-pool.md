# Build a Custom Dynamic Process Pool

**Project**: `poolex` — A production-grade dynamic worker pool with priority queuing, overflow workers, and real-time metrics.

**Learning Goal**: Understand queue-based resource management, why monitors beat links for worker crash recovery, and how priority queuing prevents starvation.

Project structure:

```
poolex/
├── lib/
│   └── poolex/
│       ├── application.ex           # supervisor: pool_server + dynamic worker supervisor
│       ├── pool.ex                  # public API: checkout, checkin, metrics
│       ├── pool_server.ex           # GenServer: state machine, queue, monitors
│       ├── worker_sup.ex            # DynamicSupervisor for worker processes
│       ├── priority_queue.ex        # three-queue implementation: :high, :normal, :low
│       └── metrics.ex               # EMA for checkout duration, counters
├── test/
│   └── poolex/
│       ├── pool_test.exs            # checkout, checkin, timeout, concurrent access
│       ├── crash_test.exs           # worker crash detection and replacement
│       ├── priority_test.exs        # priority queue ordering
│       ├── resize_test.exs          # grow under load, shrink on idle
│       └── overflow_test.exs        # overflow workers: create on demand, destroy on checkin
├── bench/
│   └── poolex_bench.exs
└── mix.exs
```

---

## The Problem

You have limited resources (DB connections, API calls, worker processes) but multiple concurrent callers. Without a pool:
- Serialize all callers = bottleneck
- Spawn unlimited = resource exhaustion

A pool must:
- Bound concurrency at resource limit
- Queue callers when all workers busy
- Clean up on caller timeout
- Replace crashed workers
- Prevent starvation with priority queuing
- Destroy overflow workers after use

---

## Key Concepts

### GenServer as Pool State Manager

All state transitions (checkout, checkin, crash recovery) serialize through the pool GenServer. This avoids race conditions. Workers themselves execute concurrently — the GenServer just manages assignment.

### Monitor Both Workers and Callers

- **Worker crash**: sends `{:DOWN, ref, ...}` → pool spawns replacement
- **Caller timeout**: sends `{:DOWN, ref, ...}` → pool cleans queue instead of delivering to dead caller

### Three Priority Queues

A heap requires careful implementation. Three separate FIFO queues (`:high`, `:normal`, `:low`) drained in order is equivalent, simpler, and correct.

### Overflow Workers Separate from Pool

Overflow workers are spawned on demand when all pool workers checked out. They are NOT added to the available pool — they're destroyed on checkin. This keeps the pool size bounded at `max_size`.

### Design Decisions

| Option | Pros | Cons | Chosen? |
|--------|------|------|---------|
| **A: Round-robin** | simple, fair | head-of-line blocking | No |
| **B: Least-loaded** | adapts to uneven durations | more messages | **Yes** |

**Rationale**: Real-world pain is uneven task durations blocking FIFO queues. Least-loaded avoids this entirely.

## Full Project Structure

```
poolex/
├── mix.exs                          # Project configuration
├── lib/
│   ├── poolex.ex                   # Module docstring
│   └── poolex/
│       ├── pool.ex                 # Public API: checkout, checkin, metrics
│       ├── pool_server.ex          # GenServer: state machine, queue, monitors
│       ├── priority_queue.ex       # Three-queue: :high, :normal, :low
│       └── metrics.ex              # EMA: checkout duration, counters
├── test/
│   ├── test_helper.exs             # ExUnit config
│   └── poolex/
│       ├── pool_test.exs           # checkout, checkin, timeout, concurrency
│       ├── crash_test.exs          # worker crash → replacement
│       ├── priority_test.exs       # priority queue ordering
│       └── overflow_test.exs       # overflow: create on demand, destroy on checkin
├── bench/
│   └── poolex_bench.exs            # Benchee: checkout/call/checkin throughput
└── .gitignore
```

## Implementation milestones

### Step 1: Project Setup

**Objective**: Separate pool server, priority queue, and metrics for unit testing.

```bash
mix new poolex --sup
cd poolex
mkdir -p lib/poolex test/poolex bench
```

### Step 2: Dependencies (mix.exs)

**Objective**: Only `benchee`. Hand-roll the pool mechanics, monitors, and priority queue.

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Pool Server State Machine

**Objective**: Serialize checkout/checkin through GenServer with bidirectional monitoring for self-healing.

```elixir
# lib/poolex/pool_server.ex
defmodule Poolex.PoolServer do
  use GenServer

  @moduledoc """
  Pool state machine.

  State:
    workers:         %{pid => ref}   — available workers and their monitor refs
    checked_out:     %{pid => ref}   — checked-out workers and their monitor refs
    waiting:         PriorityQueue   — callers waiting for a worker
    caller_monitors: %{ref => from}  — monitor refs for waiting callers

  Transitions:
    checkout (worker available):  move pid from workers → checked_out, reply to caller
    checkout (no worker):         add caller to waiting queue, monitor caller
    checkin:                      move pid from checked_out → workers OR deliver to next waiter
    worker crash:                 remove from checked_out, spawn replacement, deliver to next waiter
    caller crash (waiting):       remove from waiting queue, demonitor
    idle_timeout:                 if workers > min_size, stop one idle worker
  """

  def start_link(worker_module, worker_args, opts) do
    GenServer.start_link(__MODULE__, {worker_module, worker_args, opts})
  end

  def init({worker_module, worker_args, opts}) do
    min_size     = opts[:min_size]     || 5
    max_size     = opts[:max_size]     || 10
    overflow     = opts[:overflow]     || 0
    idle_timeout = opts[:idle_timeout] || 60_000

    workers =
      for _ <- 1..min_size, into: %{} do
        {:ok, pid} = apply(worker_module, :start_link, [worker_args])
        ref = Process.monitor(pid)
        {pid, ref}
      end

    state = %{
      worker_module: worker_module,
      worker_args: worker_args,
      workers: workers,
      checked_out: %{},
      waiting: Poolex.PriorityQueue.new(),
      caller_monitors: %{},
      overflow_workers: MapSet.new(),
      min_size: min_size,
      max_size: max_size,
      overflow: overflow,
      idle_timeout: idle_timeout
    }

    {:ok, state}
  end

  def handle_call({:checkout, priority, timeout}, from, state) do
    priority = priority || :normal

    case Map.keys(state.workers) do
      [worker_pid | _] ->
        {ref, workers} = Map.pop(state.workers, worker_pid)
        checked_out = Map.put(state.checked_out, worker_pid, ref)
        {:reply, {:ok, worker_pid}, %{state | workers: workers, checked_out: checked_out}}

      [] ->
        total = map_size(state.checked_out) + map_size(state.workers)

        if total < state.max_size + state.overflow do
          {:ok, pid} = apply(state.worker_module, :start_link, [state.worker_args])
          ref = Process.monitor(pid)
          checked_out = Map.put(state.checked_out, pid, ref)
          overflow = MapSet.put(state.overflow_workers, pid)
          {:reply, {:ok, pid}, %{state | checked_out: checked_out, overflow_workers: overflow}}
        else
          caller_ref = Process.monitor(elem(from, 0))
          timer_ref = Process.send_after(self(), {:checkout_timeout, caller_ref}, timeout || 5_000)
          waiting = Poolex.PriorityQueue.push(state.waiting, {from, caller_ref, timer_ref}, priority)
          caller_monitors = Map.put(state.caller_monitors, caller_ref, from)
          {:noreply, %{state | waiting: waiting, caller_monitors: caller_monitors}}
        end
    end
  end

  def handle_call(:metrics, _from, state) do
    metrics = %{
      pool_size: map_size(state.workers) + map_size(state.checked_out),
      checked_out: map_size(state.checked_out),
      available: map_size(state.workers),
      waiting: Poolex.PriorityQueue.size(state.waiting)
    }
    {:reply, metrics, state}
  end

  def handle_cast({:checkin, worker_pid}, state) do
    case Map.pop(state.checked_out, worker_pid) do
      {nil, _} ->
        {:noreply, state}
      {ref, checked_out} ->
        state = %{state | checked_out: checked_out}

        if MapSet.member?(state.overflow_workers, worker_pid) do
          Process.demonitor(ref, [:flush])
          Process.exit(worker_pid, :shutdown)
          {:noreply, %{state | overflow_workers: MapSet.delete(state.overflow_workers, worker_pid)}}
        else
          case Poolex.PriorityQueue.pop(state.waiting) do
            {:ok, {from, caller_ref, timer_ref}, new_waiting} ->
              Process.demonitor(caller_ref, [:flush])
              Process.cancel_timer(timer_ref)
              caller_monitors = Map.delete(state.caller_monitors, caller_ref)
              new_checked_out = Map.put(state.checked_out, worker_pid, ref)
              GenServer.reply(from, {:ok, worker_pid})
              {:noreply, %{state | waiting: new_waiting, checked_out: new_checked_out, caller_monitors: caller_monitors}}

            {:empty, _} ->
              workers = Map.put(state.workers, worker_pid, ref)
              {:noreply, %{state | workers: workers}}
          end
        end
    end
  end

  def handle_info({:DOWN, ref, :process, pid, _reason}, state) do
    cond do
      Map.has_key?(state.checked_out, pid) ->
        {_, checked_out} = Map.pop(state.checked_out, pid)
        {:ok, new_pid} = apply(state.worker_module, :start_link, [state.worker_args])
        new_ref = Process.monitor(new_pid)
        state = %{state | checked_out: checked_out}

        case Poolex.PriorityQueue.pop(state.waiting) do
          {:ok, {from, caller_ref, timer_ref}, new_waiting} ->
            Process.demonitor(caller_ref, [:flush])
            Process.cancel_timer(timer_ref)
            caller_monitors = Map.delete(state.caller_monitors, caller_ref)
            new_checked = Map.put(state.checked_out, new_pid, new_ref)
            GenServer.reply(from, {:ok, new_pid})
            {:noreply, %{state | waiting: new_waiting, checked_out: new_checked, caller_monitors: caller_monitors}}

          {:empty, _} ->
            workers = Map.put(state.workers, new_pid, new_ref)
            {:noreply, %{state | workers: workers}}
        end

      Map.has_key?(state.caller_monitors, ref) ->
        {from, caller_monitors} = Map.pop(state.caller_monitors, ref)
        waiting = Poolex.PriorityQueue.remove(state.waiting, fn {f, _, _} -> f == from end)
        {:noreply, %{state | caller_monitors: caller_monitors, waiting: waiting}}

      true ->
        {:noreply, state}
    end
  end

  def handle_info({:checkout_timeout, caller_ref}, state) do
    case Map.pop(state.caller_monitors, caller_ref) do
      {nil, _} ->
        {:noreply, state}
      {from, caller_monitors} ->
        Process.demonitor(caller_ref, [:flush])
        waiting = Poolex.PriorityQueue.remove(state.waiting, fn {f, _, _} -> f == from end)
        GenServer.reply(from, {:error, :timeout})
        {:noreply, %{state | waiting: waiting, caller_monitors: caller_monitors}}
    end
  end
end
```

### Step 4: Priority Queue

**Objective**: Three separate FIFO queues drained high → normal → low.

```elixir
# lib/poolex/priority_queue.ex
defmodule Poolex.PriorityQueue do
  @moduledoc """
  Three-level priority queue using separate FIFO queues per level.
  Drain order: :high first, then :normal, then :low.
  """

  defstruct high: :queue.new(), normal: :queue.new(), low: :queue.new()

  @spec new() :: %__MODULE__{}
  def new, do: %__MODULE__{}

  def push(%__MODULE__{} = pq, item, :high),   do: %{pq | high:   :queue.in(item, pq.high)}
  def push(%__MODULE__{} = pq, item, :normal), do: %{pq | normal: :queue.in(item, pq.normal)}
  def push(%__MODULE__{} = pq, item, :low),    do: %{pq | low:    :queue.in(item, pq.low)}

  @spec pop(%__MODULE__{}) :: {:ok, term(), %__MODULE__{}} | {:empty, %__MODULE__{}}
  def pop(%__MODULE__{} = pq) do
    cond do
      not :queue.is_empty(pq.high) ->
        {{:value, item}, new_high} = :queue.out(pq.high)
        {:ok, item, %{pq | high: new_high}}

      not :queue.is_empty(pq.normal) ->
        {{:value, item}, new_normal} = :queue.out(pq.normal)
        {:ok, item, %{pq | normal: new_normal}}

      not :queue.is_empty(pq.low) ->
        {{:value, item}, new_low} = :queue.out(pq.low)
        {:ok, item, %{pq | low: new_low}}

      true ->
        {:empty, pq}
    end
  end

  @spec remove(%__MODULE__{}, (term() -> boolean()) | term()) :: %__MODULE__{}
  def remove(%__MODULE__{} = pq, predicate) when is_function(predicate, 1) do
    %{pq |
      high: queue_reject(pq.high, predicate),
      normal: queue_reject(pq.normal, predicate),
      low: queue_reject(pq.low, predicate)
    }
  end

  def remove(%__MODULE__{} = pq, item) do
    remove(pq, fn x -> x == item end)
  end

  defp queue_reject(queue, predicate) do
    queue
    |> :queue.to_list()
    |> Enum.reject(predicate)
    |> :queue.from_list()
  end

  def size(%__MODULE__{} = pq) do
    :queue.len(pq.high) + :queue.len(pq.normal) + :queue.len(pq.low)
  end
end
```

### Step 5: Pool Public API

**Objective**: Clean wrapper over GenServer internals.

```elixir
# lib/poolex/pool.ex
defmodule Poolex.Pool do
  @moduledoc """
  Public API for the Poolex worker pool.
  Wraps the PoolServer GenServer with a clean interface.
  """

  @spec start_link(module(), term(), keyword()) :: {:ok, pid()}
  def start_link(worker_module, worker_args, opts \\ []) do
    Poolex.PoolServer.start_link(worker_module, worker_args, opts)
  end

  @spec checkout(pid(), keyword()) :: {:ok, pid()} | {:error, :timeout}
  def checkout(pool, opts \\ []) do
    priority = Keyword.get(opts, :priority, :normal)
    timeout = Keyword.get(opts, :timeout, 5_000)
    GenServer.call(pool, {:checkout, priority, timeout}, timeout + 1_000)
  end

  @spec checkin(pid(), pid()) :: :ok
  def checkin(pool, worker_pid) do
    GenServer.cast(pool, {:checkin, worker_pid})
  end

  @spec metrics(pid()) :: map()
  def metrics(pool) do
    GenServer.call(pool, :metrics)
  end
end
```

### Step 6: Tests — Contract as Specs

**Objective**: Freeze timeout cleanup, crash replacement, and concurrent fairness as executable tests.

```elixir
# test/poolex/pool_test.exs
defmodule Poolex.PoolTest do
  use ExUnit.Case, async: false

  defmodule EchoWorker do
    use GenServer
    def start_link(_), do: GenServer.start_link(__MODULE__, :ok)
    def init(:ok), do: {:ok, :idle}
    def handle_call(:ping, _from, state), do: {:reply, :pong, state}
  end

  setup do
    {:ok, pool} = Poolex.Pool.start_link(EchoWorker, [], min_size: 5, max_size: 10)
    {:ok, pool: pool}
  end

  describe "checkout and checkin" do
    test "returns {:ok, pid} when worker available", %{pool: pool} do
      assert {:ok, pid} = Poolex.Pool.checkout(pool)
      assert is_pid(pid)
      Poolex.Pool.checkin(pool, pid)
    end

    test "times out when all workers are busy", %{pool: pool} do
      workers = for _ <- 1..10, do: {:ok, w} = Poolex.Pool.checkout(pool, priority: :normal); w
      assert {:error, :timeout} = Poolex.Pool.checkout(pool, timeout: 100)
      Enum.each(workers, fn w -> Poolex.Pool.checkin(pool, w) end)
    end
  end

  describe "concurrent behavior" do
    test "100 concurrent checkouts against 10-worker pool respects limit", %{pool: pool} do
      parent = self()

      tasks = for _ <- 1..100 do
        Task.async(fn ->
          case Poolex.Pool.checkout(pool, timeout: 5_000) do
            {:ok, pid} ->
              send(parent, {:checked_out, pid})
              Process.sleep(50)
              Poolex.Pool.checkin(pool, pid)
            {:error, :timeout} ->
              send(parent, :timeout)
          end
        end)
      end

      Task.await_many(tasks, 30_000)
      metrics = Poolex.Pool.metrics(pool)
      assert metrics.checked_out <= 10
    end
  end
end
```

```elixir
# test/poolex/crash_test.exs
defmodule Poolex.CrashTest do
  use ExUnit.Case, async: false

  defmodule CrashableWorker do
    use GenServer
    def start_link(_), do: GenServer.start_link(__MODULE__, :ok)
    def init(:ok), do: {:ok, :ok}
    def handle_cast(:crash, _), do: raise "intentional crash"
  end

  describe "crash handling" do
    test "worker crash while checked out triggers replacement" do
      {:ok, pool} = Poolex.Pool.start_link(CrashableWorker, [], min_size: 3, max_size: 3)

      {:ok, worker} = Poolex.Pool.checkout(pool)
      GenServer.cast(worker, :crash)
      Process.sleep(100)

      metrics = Poolex.Pool.metrics(pool)
      assert metrics.pool_size == 3
    end
  end
end
```

---

## Quick Start

**Prerequisites**: Elixir 1.14+, OTP 25+

**Setup**:
```bash
mix new poolex --sup
cd poolex
mkdir -p lib/poolex test/poolex bench
```

**Run tests** (serially — pool is a singleton):
```bash
mix test test/poolex/ --trace
```

**Interactive example**:
```bash
iex -S mix
```

Then in iex:
```elixir
defmodule EchoWorker do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok)
  def init(:ok), do: {:ok, :idle}
  def handle_call(:ping, _from, state), do: {:reply, :pong, state}
end

{:ok, pool} = Poolex.Pool.start_link(EchoWorker, [], min_size: 5, max_size: 10)

# Checkout a worker
{:ok, worker} = Poolex.Pool.checkout(pool, timeout: 5000)
GenServer.call(worker, :ping)  # => :pong
Poolex.Pool.checkin(pool, worker)

# Check pool metrics
metrics = Poolex.Pool.metrics(pool)
IO.inspect(metrics)
```

---

## Benchmark

**Objective**: Measure checkout/call/checkin throughput under concurrent load.

**Setup**:
```elixir
# bench/poolex_bench.exs
defmodule NoopWorker do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, :ok)
  def init(:ok), do: {:ok, :ok}
  def handle_call(:noop, _from, state), do: {:reply, :ok, state}
end

{:ok, pool} = Poolex.Pool.start_link(NoopWorker, [], min_size: 10, max_size: 10)

Benchee.run(
  %{
    "checkout + call + checkin" => fn ->
      {:ok, w} = Poolex.Pool.checkout(pool)
      GenServer.call(w, :noop)
      Poolex.Pool.checkin(pool, w)
    end
  },
  parallel: 20,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

**Run**:
```bash
mix run bench/poolex_bench.exs
```

**Expected Results**:
- Idle checkout: 10–20 µs
- Saturated checkout (queued): 100–500 µs
- Throughput (100 workers, 200 concurrent): 50k–100k ops/sec
- Overflow creation: < 1 ms

**Interpretation**:
GenServer mailbox contention limits peak throughput. Priority queuing should reduce p99 for high-priority callers by 50–70% vs. FIFO under mixed workloads.

---

## Reflection

These questions deepen your understanding:

1. **Bimodal Durations**: If tasks have bimodal durations (1 ms vs 1 s), does least-loaded still win over round-robin? Measure the tail latency.

2. **Poolboy Comparison**: Compare your design to poolboy. Which exhaustion semantics would you default to (queue vs reject), and why?

---

## Trade-off Analysis

| Aspect | Your implementation | Poolboy | NimblePool |
|--------|--------------------|---------|----|
| Worker type | any GenServer | any OTP process | lazy init, manual protocol |
| Priority support | three levels | none | none |
| Overflow | configurable | configurable | not supported |
| Metrics | built-in | none | none |
| State | GenServer | Gen FSM | GenServer |
| Idle shrink | yes | yes | no |

After running the benchmark, record your measured throughput (checkout+call+checkin ops/sec) for comparison.

Architectural question: Little's Law states `L = λW` (mean queue length = arrival rate × mean wait time). Given a pool of 10 workers and an arrival rate of 200 req/second with mean worker hold time of 40ms, what is the expected queue depth? What does that imply about checkout timeout configuration?

---

## Common Production Mistakes

**1. Not monitoring waiting callers**
Dead callers in the queue cause workers to be delivered to nobody. Always monitor waiting callers and clean up on `:DOWN`.

**2. Race between worker crash and caller timeout**
Worker crashes while caller timeout is firing. Pool tries to deliver replacement to a caller that already got `:error, :timeout`. Check timeout state before delivering.

**3. Running average for checkout latency**
Running sum/count accumulates unbounded error. Use Exponential Moving Average instead: `ema = alpha * sample + (1 - alpha) * ema`.

**4. Overflow workers not destroyed on checkin**
Overflow workers recycled into the pool cause it to permanently grow. Track overflow workers separately and stop them, not recycle them.

---

## Resources

- [Poolboy source](https://github.com/devinus/poolboy) — study `poolboy.erl`, particularly the `checkout` and `handle_checkin` protocol
- Herlihy, M. & Shavit, N. — *The Art of Multiprocessor Programming* — Chapter 10 (Concurrent Queues)
- Little's Law — https://en.wikipedia.org/wiki/Little%27s_law
- [Erlang efficiency guide — process overhead](https://www.erlang.org/doc/efficiency_guide/processes)
