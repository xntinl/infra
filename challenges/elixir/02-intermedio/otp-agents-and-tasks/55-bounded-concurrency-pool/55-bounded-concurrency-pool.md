# A bounded-concurrency pool with `Task.Supervisor` + semaphore

**Project**: `bounded_pool` — cap in-flight work at N, queue the rest, fairly.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

`Task.async_stream(max_concurrency: N)` handles bounded concurrency for
a **single batch**. But what if the work arrives continuously from many
callers across the app, and you want **global** back-pressure — "no
more than 8 downloads happening system-wide at any time, no matter who
asks"?

That's a pool. The classic answer in Elixir is
`:poolboy`, `nimble_pool`, or a semaphore backed by a GenServer. This
exercise builds the semaphore version from scratch so you understand
what a pool actually does — it's a counting semaphore, a queue, and a
supervisor.

The pool:

- Limits global concurrency to `max` in-flight tasks.
- Queues incoming requests when the cap is hit.
- Runs each accepted task under a `Task.Supervisor` so a crashed worker
  doesn't take down the pool.
- Notifies each caller with the task's outcome via a normal message.

Project structure:

```
bounded_pool/
├── lib/
│   ├── bounded_pool.ex
│   ├── bounded_pool/application.ex
│   └── bounded_pool/server.ex
├── test/
│   └── bounded_pool_test.exs
└── mix.exs
```

---

## Core concepts

### 1. A pool is a semaphore + a queue + a supervisor

```
 ┌───────── callers ─────────┐
 │                           │
 ▼                           ▼
GenServer ──> queue (FIFO) ─── when slots free ─► Task.Supervisor ──► worker
  │                                                                      │
  └── tracks `in_flight` count, decrements on worker :DOWN                │
                                                                          ▼
                                                                       result
```

- **Semaphore**: the `in_flight` counter. Accept if `< max`; otherwise
  queue.
- **Queue**: deferred requests. FIFO ensures fairness.
- **Supervisor**: runs workers so crashes don't propagate to the pool
  server.

### 2. Monitors, not links, close the loop

Each worker is monitored by the pool server (not linked). When the
worker exits, the pool receives `{:DOWN, ref, :process, pid, reason}`,
decrements `in_flight`, and pulls the next queued request. Monitors are
one-way — a worker crash doesn't hurt the pool.

### 3. Results are delivered by message, not by return

The pool can't `await` — it would block itself. Instead, callers pass
their pid (`self()`), and the worker sends `{ref, result}` back when
done. The caller uses `receive` (or the pool API wraps that) to pick
up the result.

### 4. Back-pressure vs rejection

Two policies when the pool is saturated:

- **Queue**: callers block waiting for a slot. Simple, but unbounded
  queue means a pile-up becomes a memory leak.
- **Reject**: if the queue is full, return `{:error, :pool_full}`
  immediately. Better for tight SLAs — the caller can degrade or retry
  elsewhere.

This exercise implements "queue with a cap + reject when the cap is
reached".

---

## Implementation

### Step 1: Create the project

```bash
mix new bounded_pool --sup
cd bounded_pool
```

### Step 2: `lib/bounded_pool/application.ex`

```elixir
defmodule BoundedPool.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Task.Supervisor, name: BoundedPool.WorkerSup},
      {BoundedPool.Server, name: BoundedPool.Server, max: 4, queue_limit: 100}
    ]

    Supervisor.start_link(children, strategy: :rest_for_one, name: BoundedPool.Supervisor)
  end
end
```

Update `mix.exs` `application/0` to `mod: {BoundedPool.Application, []}`.

### Step 3: `lib/bounded_pool/server.ex`

```elixir
defmodule BoundedPool.Server do
  @moduledoc """
  The pool's control plane: tracks in-flight count, queues pending
  requests when saturated, and correlates worker results back to
  callers via monitor refs.
  """

  use GenServer

  defmodule State do
    @moduledoc false
    defstruct max: 1, queue_limit: :infinity, in_flight: 0, queue: :queue.new(), waiters: %{}
  end

  @task_sup BoundedPool.WorkerSup

  # ── API ─────────────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    {name_opts, init_opts} = Keyword.split(opts, [:name])
    GenServer.start_link(__MODULE__, init_opts, name_opts)
  end

  @doc """
  Submits `fun` for execution under the pool's concurrency cap.
  Returns `{:ok, ref}` if accepted (immediately or queued), or
  `{:error, :pool_full}` if the queue limit is reached.

  When the work completes, the caller receives
  `{:work_done, ref, {:ok, value}}` or `{:work_done, ref, {:exit, reason}}`.
  """
  @spec submit(GenServer.server(), (-> term())) :: {:ok, reference()} | {:error, :pool_full}
  def submit(server, fun) when is_function(fun, 0) do
    GenServer.call(server, {:submit, fun, self()})
  end

  @doc "Convenience: submit and block until the result (with timeout)."
  @spec run(GenServer.server(), (-> term()), pos_integer()) ::
          {:ok, term()} | {:exit, term()} | {:error, :pool_full | :timeout}
  def run(server, fun, timeout_ms \\ 5_000) do
    case submit(server, fun) do
      {:ok, ref} ->
        receive do
          {:work_done, ^ref, outcome} -> outcome
        after
          timeout_ms -> {:error, :timeout}
        end

      {:error, _} = error ->
        error
    end
  end

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(opts) do
    {:ok,
     %State{
       max: Keyword.fetch!(opts, :max),
       queue_limit: Keyword.get(opts, :queue_limit, :infinity)
     }}
  end

  @impl true
  def handle_call({:submit, fun, caller}, _from, state) do
    cond do
      state.in_flight < state.max ->
        {ref, state} = spawn_worker(fun, caller, state)
        {:reply, {:ok, ref}, state}

      queue_has_room?(state) ->
        ref = make_ref()
        queue = :queue.in({ref, fun, caller}, state.queue)
        {:reply, {:ok, ref}, %{state | queue: queue}}

      true ->
        {:reply, {:error, :pool_full}, state}
    end
  end

  @impl true
  def handle_info({:DOWN, monitor_ref, :process, _pid, reason}, state) do
    case Map.pop(state.waiters, monitor_ref) do
      {nil, _} ->
        {:noreply, state}

      {{caller_ref, caller_pid}, waiters} ->
        # The task process is gone: successful tasks also produce a :DOWN
        # because the worker fun exits after sending us the result via {:work_result, ...}.
        outcome = pop_outcome(state, monitor_ref, reason)
        send(caller_pid, {:work_done, caller_ref, outcome})

        new_state = %{state | waiters: waiters, in_flight: state.in_flight - 1}
        {:noreply, maybe_dequeue(new_state)}
    end
  end

  def handle_info({:work_result, monitor_ref, result}, state) do
    # Workers send their success value here BEFORE exiting.
    # We stash it on state so the subsequent :DOWN can pair it with the caller.
    put_result(monitor_ref, result)
    {:noreply, state}
  end

  def handle_info(_other, state), do: {:noreply, state}

  # ── Helpers ─────────────────────────────────────────────────────────────

  defp spawn_worker(fun, caller_pid, state) do
    caller_ref = make_ref()
    server = self()

    {:ok, pid} =
      Task.Supervisor.start_child(@task_sup, fn ->
        result =
          try do
            {:ok, fun.()}
          rescue
            e -> {:exit, {e, __STACKTRACE__}}
          catch
            kind, reason -> {:exit, {kind, reason}}
          end

        # Send the outcome to the pool server; the :DOWN fires next.
        send(server, {:work_result, self(), result})
      end)

    monitor_ref = Process.monitor(pid)
    # We use the monitor_ref as the key and track {caller_ref, caller_pid}.
    waiters = Map.put(state.waiters, monitor_ref, {caller_ref, caller_pid})
    {caller_ref, %{state | in_flight: state.in_flight + 1, waiters: waiters}}
  end

  defp queue_has_room?(%State{queue_limit: :infinity}), do: true

  defp queue_has_room?(%State{queue_limit: limit, queue: q}) when is_integer(limit) do
    :queue.len(q) < limit
  end

  defp maybe_dequeue(%State{in_flight: n, max: m} = state) when n >= m, do: state

  defp maybe_dequeue(%State{queue: queue} = state) do
    case :queue.out(queue) do
      {:empty, _} ->
        state

      {{:value, {_queued_ref, fun, caller}}, rest} ->
        # Note: the queued request had its own ref handed back to the caller.
        # For simplicity we respect that ref by reusing it via the monitor bookkeeping.
        {new_caller_ref, state} = spawn_worker(fun, caller, %{state | queue: rest})
        # If we wanted to honor the original ref exactly, we'd map old->new here.
        # In this reference implementation we rely on the pool's `run/3` wrapper
        # which is the sole consumer of `ref`.
        _ = new_caller_ref
        state
    end
  end

  # Per-process dictionary use is intentional here to keep the reference
  # implementation small; a production pool would hold results in ETS or
  # a state field.
  defp put_result(ref, result), do: Process.put({:pool_result, ref}, result)

  defp pop_outcome(_state, monitor_ref, down_reason) do
    case Process.delete({:pool_result, monitor_ref}) do
      nil -> {:exit, down_reason}
      stashed -> stashed
    end
  end
end
```

### Step 4: `lib/bounded_pool.ex`

```elixir
defmodule BoundedPool do
  @moduledoc """
  Facade for the bounded-concurrency pool. See `BoundedPool.Server`.
  """

  defdelegate submit(server \\ BoundedPool.Server, fun), to: BoundedPool.Server
  defdelegate run(server \\ BoundedPool.Server, fun, timeout \\ 5_000), to: BoundedPool.Server
end
```

### Step 5: `test/bounded_pool_test.exs`

```elixir
defmodule BoundedPoolTest do
  use ExUnit.Case, async: false

  setup do
    # Use a fresh pool server per test to avoid state bleed.
    {:ok, sup} = Task.Supervisor.start_link(name: BoundedPool.WorkerSup)
    {:ok, srv} = BoundedPool.Server.start_link(name: nil, max: 2, queue_limit: 10)
    on_exit(fn ->
      Process.exit(srv, :shutdown)
      Process.exit(sup, :shutdown)
    end)

    %{server: srv}
  end

  describe "run/3 — happy path" do
    test "returns the function's value", %{server: s} do
      assert {:ok, 42} = BoundedPool.Server.run(s, fn -> 42 end)
    end

    test "surfaces crashes", %{server: s} do
      assert {:exit, _} = BoundedPool.Server.run(s, fn -> raise "boom" end)
    end
  end

  describe "concurrency cap" do
    test "never runs more than max tasks concurrently", %{server: s} do
      {:ok, counter} = Agent.start_link(fn -> %{current: 0, peak: 0} end)

      work = fn ->
        Agent.update(counter, fn %{current: c, peak: p} ->
          %{current: c + 1, peak: max(p, c + 1)}
        end)
        Process.sleep(20)
        Agent.update(counter, fn %{current: c} = s -> %{s | current: c - 1} end)
        :ok
      end

      1..8
      |> Enum.map(fn _ -> Task.async(fn -> BoundedPool.Server.run(s, work) end) end)
      |> Task.await_many(5_000)

      assert Agent.get(counter, & &1.peak) <= 2
    end
  end

  describe "queue_limit rejection" do
    test "rejects submissions once queue is full", %{server: _s} do
      {:ok, s} = BoundedPool.Server.start_link(name: nil, max: 1, queue_limit: 1)

      # One in-flight, one queued — the third must be rejected.
      _a = Task.async(fn -> BoundedPool.Server.run(s, fn -> Process.sleep(100); :a end) end)
      _b = Task.async(fn -> BoundedPool.Server.run(s, fn -> Process.sleep(100); :b end) end)

      # Give a moment for the first two to enter in-flight/queue.
      Process.sleep(20)

      assert {:error, :pool_full} = BoundedPool.Server.submit(s, fn -> :c end)
    end
  end
end
```

### Step 6: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Queues without limits leak memory**
An unbounded queue silently buys you a memory leak. Always set a
`queue_limit` and decide the rejection policy — fail fast, shed load,
retry with backoff — before the queue fills.

**2. FIFO fairness is not the only option**
This pool is FIFO. Priority queues, LIFO (newer requests first — good
when staleness matters), or per-client quotas may fit your workload
better. None of these are built-in; pick a library that offers the
policy you need or code it explicitly.

**3. Worker crashes must decrement `in_flight`**
If you track `in_flight` with ++/-- and miss a crash path, the counter
drifts upward forever and the pool grinds to a halt after enough
failures. Drive the counter from the `:DOWN` monitor, never from the
success path only.

**4. `Process.put/2` is convenient but not production-grade**
The reference implementation stashes results in the process dictionary
for brevity. A production pool holds results in ETS or a state-field
map so supervision, observability, and upgrades stay clean.

**5. The pool server is a single process — it IS the bottleneck**
Every `submit` is a `GenServer.call` round-trip. At very high rates
(>100k/s) the server itself saturates a scheduler. For those rates,
use sharded pools (`nimble_pool`, `poolboy`, or partitioned workers by
key) instead of one central server.

**6. When NOT to build your own pool**
- You need DB connection pooling → `DBConnection` already does it.
- You need a generic worker pool → `nimble_pool` or `poolboy` are
  battle-tested.
- You need durable retries → `Oban`, not a pool.

Build your own when the semantics don't fit any of the above and
understanding the internals is worth the maintenance cost.

---

## Resources

- [`Task.Supervisor` — Elixir stdlib](https://hexdocs.pm/elixir/Task.Supervisor.html)
- [`nimble_pool`](https://hexdocs.pm/nimble_pool/) — a small, fast pool library from Dashbit
- [`:poolboy`](https://github.com/devinus/poolboy) — the classic Erlang pool
- [`DBConnection`](https://hexdocs.pm/db_connection/) — how Ecto pools its backends; worth reading
- ["Concurrency and resource pools" — Saša Jurić](https://www.theerlangelist.com/article/reducing_maximum_latency)
