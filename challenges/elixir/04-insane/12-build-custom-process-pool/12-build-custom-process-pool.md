# Custom Dynamic Process Pool

**Project**: `poolex` — a production-grade dynamic worker pool with priority, overflow, and metrics

---

## Project context

You are building `poolex`, a dynamic worker pool from scratch in Elixir. No Poolboy, no Poolex, no existing pooling library. The pool manages concurrent access to a fixed set of worker processes, handles checkout timeouts, survives worker crashes, and exposes real operational metrics.

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

## The problem

You have a limited resource — database connections, HTTP connections to a third-party API, CPU-bound worker processes — that multiple concurrent callers need. Without a pool, you either serialize all callers through one process (bottleneck) or spawn unlimited processes (resource exhaustion). A pool bounds concurrency at the resource limit and queues callers when all workers are busy.

The hard problems are: a caller that times out must be cleanly removed from the queue; a worker that crashes while checked out must be replaced; priority queues must not starve low-priority callers indefinitely; overflow workers must be created on demand and destroyed after use.

---

## Why this design

**GenServer as pool state manager**: all state transitions (checkout, checkin, crash detection) are serialized through the pool GenServer. This simplicity avoids race conditions. The GenServer is not in the hot path for the workers themselves — it only manages assignment. Workers execute concurrently.

**Monitor both workers and waiting callers**: a worker that crashes while checked out sends `{:DOWN, ref, :process, pid, reason}` to the pool server, which then spawns a replacement. A caller that dies while waiting in the queue also sends `{:DOWN, ...}`, allowing the pool to clean the dead caller from the queue rather than delivering a worker to nobody.

**Three separate queues for priority**: a heap-based priority queue requires careful implementation and adds complexity. Three independent queues (`:high`, `:normal`, `:low`) drained in order is equivalent for this use case and trivial to implement correctly.

**Overflow workers separate from pool workers**: overflow workers are created when all pool workers are checked out and overflow > 0. They are not added to the pool's available list — they are destroyed on checkin. This keeps the pool size bounded at max_size after peak load subsides.

---

## Design decisions

**Option A — Round-robin dispatch from a central queue**
- Pros: simple; fair.
- Cons: ignores worker load; can queue behind a slow worker while idle workers sit empty.

**Option B — Least-loaded checkout with worker-self-announced availability** (chosen)
- Pros: workers advertise `:ready` after each task, so checkout picks a truly free worker; naturally adapts to heterogeneous task durations.
- Cons: more coordination messages; the ready-set data structure must be correct under races.

→ Chose **B** because poolboy's real-world pain is head-of-line blocking on uneven task durations; a ready-set design removes it outright.

## Implementation milestones

### Step 1: Create the project

```bash
mix new poolex --sup
cd poolex
mkdir -p lib/poolex test/poolex bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Pool server state machine

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

### Step 4: Priority queue

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

### Step 5: Pool public API

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

### Step 6: Given tests — must pass without modification

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

  test "checkout returns {:ok, pid} when worker available", %{pool: pool} do
    assert {:ok, pid} = Poolex.Pool.checkout(pool)
    assert is_pid(pid)
    Poolex.Pool.checkin(pool, pid)
  end

  test "checkout times out when all workers are busy", %{pool: pool} do
    workers = for _ <- 1..10, do: {:ok, w} = Poolex.Pool.checkout(pool, priority: :normal); w

    assert {:error, :timeout} = Poolex.Pool.checkout(pool, timeout: 100)

    Enum.each(workers, fn w -> Poolex.Pool.checkin(pool, w) end)
  end

  test "100 concurrent checkouts against 10-worker pool: exactly 10 checked out at once", %{pool: pool} do
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

    # The pool must never have more than 10 workers checked out simultaneously
    # (verified by pool metrics, not by message counting here)
    metrics = Poolex.Pool.metrics(pool)
    assert metrics.checked_out <= 10
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

  test "worker crash while checked out triggers replacement" do
    {:ok, pool} = Poolex.Pool.start_link(CrashableWorker, [], min_size: 3, max_size: 3)

    {:ok, worker} = Poolex.Pool.checkout(pool)
    GenServer.cast(worker, :crash)
    Process.sleep(100)

    metrics = Poolex.Pool.metrics(pool)
    assert metrics.pool_size == 3, "replacement worker must be spawned"
  end
end
```

### Step 7: Run the tests

```bash
mix test test/poolex/ --trace
```

### Step 8: Benchmark

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

### Why this works

Workers register with the pool when idle and deregister when busy; `checkout/1` pops from the ready-set or enqueues the caller. Because the set is kept in ETS, multiple callers don't contend on a single dispatcher process.

---

## Benchmark

```elixir
# bench/pool_bench.exs
Benchee.run(%{"checkout" => fn -> Pool.transaction(fn _ -> :ok end) end}, parallel: 50, time: 10)
```

Target: 50,000 checkouts/second on a 100-worker pool; p99 checkout latency < 200 µs.

---

## Trade-off analysis

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

## Common production mistakes

**1. Not monitoring waiting callers**
A caller that dies while in the queue leaves a dead entry. When a worker becomes available, the pool delivers it to the dead caller, which silently discards it. The next caller in the queue waits unnecessarily. Always monitor waiting callers and clean up on `:DOWN`.

**2. Race between worker crash and caller timeout**
Worker crashes at t=0. Caller timeout fires at t=1. Pool tries to reply to the timed-out caller with the replacement worker — but the caller has already received `:error, :timeout`. The pool must check whether the caller's timeout has already fired before delivering the replacement. Track timeout state with a per-caller flag.

**3. Average checkout time calculated as a running sum/count**
A running sum is unbounded and accumulates floating-point error over time. Use an Exponential Moving Average: `ema = alpha * new_sample + (1 - alpha) * ema`. This gives a time-weighted average that adapts to load changes without unbounded memory.

**4. Overflow workers not destroyed on checkin**
An overflow worker checked back in to the pool is incorrectly added to the available pool, causing the pool to permanently grow. Track which workers are overflow workers (e.g., with a MapSet) and stop them instead of recycling them.

## Reflection

- If tasks have bimodal durations (1 ms vs 1 s), does least-loaded still win over round-robin, or do you need priority queuing? Measure the tail latency.
- Compare your design to poolboy. Which pool exhaustion semantics (queue vs reject) would you default to, and why?

---

## Resources

- [Poolboy source](https://github.com/devinus/poolboy) — study `poolboy.erl`, particularly the `checkout` and `handle_checkin` protocol
- Herlihy, M. & Shavit, N. — *The Art of Multiprocessor Programming* — Chapter 10 (Concurrent Queues)
- Little's Law — https://en.wikipedia.org/wiki/Little%27s_law
- [Erlang efficiency guide — process overhead](https://www.erlang.org/doc/efficiency_guide/processes)
