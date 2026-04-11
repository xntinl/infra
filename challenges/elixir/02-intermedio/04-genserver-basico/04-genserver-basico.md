# GenServer: Stateful Process with a Clear API

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system needs a **queue server**: a process that accepts incoming job
submissions from multiple clients, maintains an ordered list of pending jobs, and dispenses
them to workers on demand. The raw process from exercise 01 worked, but GenServer brings
two things that matter in production: a structured callback model that separates the
public API from internal state management, and built-in integration with Supervisor.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── worker_process.ex    # exercise 01
│       ├── task_registry.ex     # exercise 02
│       ├── batch_runner.ex      # exercise 03
│       └── queue_server.ex
├── test/
│   └── task_queue/
│       └── queue_server_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## Why GenServer and not Agent

Agent works when you only need to read and transform a value. The queue server has
richer needs:

- `push/1` is fire-and-forget: the caller does not wait for confirmation.
- `pop/0` is synchronous: the caller blocks until a job is returned.
- `pop/0` when the queue is empty should return `{:error, :empty}` — not crash.
- A periodic cleanup timer must remove jobs that have been queued longer than a deadline.

Agent only exposes `get/update/get_and_update`. GenServer exposes `call` (synchronous),
`cast` (asynchronous), and `handle_info` (timer messages and raw sends). The queue server
needs all three.

---

## The critical rule: two layers, two processes

Every GenServer module has two distinct layers:

```
Client process             GenServer process
─────────────              ─────────────────
push/1          ──cast──▶  handle_cast   (runs here, modifies state)
pop/0           ──call──▶  handle_call   (runs here, returns reply)
                           handle_info   (runs here, receives timer)
```

The public functions (`push/1`, `pop/0`) run **in the caller's process**. The callbacks
(`handle_call`, `handle_cast`, `handle_info`) run **in the GenServer process**. `self()`
inside a callback is the GenServer's PID, not the caller's. Mixing these up is the most
common GenServer bug.

---

## The business problem

`TaskQueue.QueueServer` manages a FIFO queue of pending jobs:

- `push/1` — adds a job to the back of the queue. Fire-and-forget.
- `pop/0` — takes the front job off the queue. Synchronous. Returns `{:ok, job}` or
  `{:error, :empty}`.
- `peek/0` — returns the front job without removing it.
- `size/0` — returns the current queue length.
- `flush/0` — removes all jobs older than `@job_ttl_ms`, returns how many were removed.
- The server schedules a periodic cleanup every `@cleanup_interval_ms`.

---

## Implementation

### Step 1: `lib/task_queue/queue_server.ex`

```elixir
defmodule TaskQueue.QueueServer do
  use GenServer
  require Logger

  @cleanup_interval_ms 30_000
  # Jobs queued for longer than this are considered stale and removed by cleanup
  @job_ttl_ms 300_000

  @type job :: %{id: String.t(), payload: any(), queued_at: integer()}

  # ---------------------------------------------------------------------------
  # Public API — runs in the CALLER's process
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Adds a job to the back of the queue. Returns immediately."
  @spec push(any()) :: :ok
  def push(payload) do
    job = %{
      id: generate_id(),
      payload: payload,
      queued_at: System.monotonic_time(:millisecond)
    }

    GenServer.cast(__MODULE__, {:push, job})
  end

  @doc "Takes the front job off the queue. Blocks until the server replies."
  @spec pop() :: {:ok, job()} | {:error, :empty}
  def pop do
    GenServer.call(__MODULE__, :pop)
  end

  @doc "Returns the front job without removing it."
  @spec peek() :: {:ok, job()} | {:error, :empty}
  def peek do
    GenServer.call(__MODULE__, :peek)
  end

  @doc "Returns the current number of jobs in the queue."
  @spec size() :: non_neg_integer()
  def size do
    GenServer.call(__MODULE__, :size)
  end

  @doc "Manually triggers cleanup of stale jobs. Returns the number removed."
  @spec flush() :: non_neg_integer()
  def flush do
    GenServer.call(__MODULE__, :flush)
  end

  # ---------------------------------------------------------------------------
  # GenServer callbacks — runs in the SERVER's process
  # ---------------------------------------------------------------------------

  @impl GenServer
  def init(_opts) do
    # Schedule the first cleanup
    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    Logger.info("QueueServer started")
    # State: a list of jobs in FIFO order (head = front of queue)
    {:ok, []}
  end

  @impl GenServer
  def handle_cast({:push, job}, state) do
    {:noreply, state ++ [job]}
  end

  @impl GenServer
  def handle_call(:pop, _from, state) do
    case state do
      [] ->
        {:reply, {:error, :empty}, state}

      [job | rest] ->
        {:reply, {:ok, job}, rest}
    end
  end

  @impl GenServer
  def handle_call(:peek, _from, state) do
    case state do
      [] -> {:reply, {:error, :empty}, state}
      [job | _] -> {:reply, {:ok, job}, state}
    end
  end

  @impl GenServer
  def handle_call(:size, _from, state) do
    {:reply, length(state), state}
  end

  @impl GenServer
  def handle_call(:flush, _from, state) do
    cutoff = System.monotonic_time(:millisecond) - @job_ttl_ms
    remaining = Enum.filter(state, fn job -> job.queued_at > cutoff end)
    removed = length(state) - length(remaining)

    if removed > 0 do
      Logger.info("QueueServer cleanup: removed #{removed} stale jobs")
    end

    {:reply, removed, remaining}
  end

  @impl GenServer
  def handle_info(:cleanup, state) do
    cutoff = System.monotonic_time(:millisecond) - @job_ttl_ms
    remaining = Enum.filter(state, fn job -> job.queued_at > cutoff end)
    removed = length(state) - length(remaining)

    if removed > 0 do
      Logger.info("QueueServer cleanup: removed #{removed} stale jobs")
    end

    # Reschedule next cleanup
    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:noreply, remaining}
  end

  @impl GenServer
  def handle_info(unexpected, state) do
    Logger.warning("QueueServer received unexpected message: #{inspect(unexpected)}")
    {:noreply, state}
  end

  @impl GenServer
  def terminate(reason, state) do
    Logger.info("QueueServer terminating (reason: #{inspect(reason)}, queue size: #{length(state)})")
    :ok
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  defp generate_id do
    :crypto.strong_rand_bytes(8) |> Base.url_encode64(padding: false)
  end
end
```

The `handle_cast({:push, job}, state)` appends the job to the end of the list with
`state ++ [job]`. This is O(n), which is acceptable for moderate queue sizes. For
high-throughput queues, a `:queue` (Erlang's double-ended queue) provides O(1)
enqueue/dequeue — that optimization is explored in exercise 13 with ETS.

The `handle_call(:flush, ...)` uses the same cleanup logic as `handle_info(:cleanup, ...)`.
The difference: `:flush` is triggered manually and returns the count of removed jobs
to the caller. `:cleanup` is triggered by the timer and returns nothing — it is
internal housekeeping.

The `handle_info` catch-all clause logs unexpected messages at warning level. Without
this, an unexpected message would crash the GenServer with a `{:EXIT, ...}` signal.

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/queue_server_test.exs
defmodule TaskQueue.QueueServerTest do
  use ExUnit.Case, async: false
  # async: false — tests share the named GenServer

  alias TaskQueue.QueueServer

  setup do
    case Process.whereis(QueueServer) do
      nil -> :ok
      pid -> GenServer.stop(pid)
    end

    {:ok, _} = QueueServer.start_link()
    :ok
  end

  describe "push/1 and pop/0" do
    test "pop returns jobs in FIFO order" do
      QueueServer.push("job_a")
      QueueServer.push("job_b")
      QueueServer.push("job_c")
      # Give the server time to process the casts
      Process.sleep(10)

      assert {:ok, %{payload: "job_a"}} = QueueServer.pop()
      assert {:ok, %{payload: "job_b"}} = QueueServer.pop()
      assert {:ok, %{payload: "job_c"}} = QueueServer.pop()
    end

    test "pop returns {:error, :empty} on an empty queue" do
      assert {:error, :empty} = QueueServer.pop()
    end
  end

  describe "peek/0" do
    test "returns front job without removing it" do
      QueueServer.push("peek_job")
      Process.sleep(10)

      assert {:ok, %{payload: "peek_job"}} = QueueServer.peek()
      assert {:ok, %{payload: "peek_job"}} = QueueServer.peek()
      assert 1 = QueueServer.size()
    end
  end

  describe "size/0" do
    test "reflects queue length after push and pop" do
      assert 0 = QueueServer.size()
      QueueServer.push(:a)
      QueueServer.push(:b)
      Process.sleep(10)
      assert 2 = QueueServer.size()
      QueueServer.pop()
      assert 1 = QueueServer.size()
    end
  end

  describe "flush/0" do
    test "removes stale jobs and returns the count" do
      # Insert jobs with a manipulated queued_at via direct state access
      stale_job = %{id: "stale", payload: :stale, queued_at: 0}
      fresh_job = %{id: "fresh", payload: :fresh, queued_at: System.monotonic_time(:millisecond)}
      :sys.replace_state(QueueServer, fn _state -> [stale_job, fresh_job] end)

      removed = QueueServer.flush()
      assert removed == 1
      assert 1 = QueueServer.size()
      assert {:ok, %{id: "fresh"}} = QueueServer.pop()
    end
  end

  describe "concurrent access" do
    test "handles concurrent pushes without losing jobs" do
      tasks = Enum.map(1..50, fn n -> Task.async(fn -> QueueServer.push(n) end) end)
      Task.await_many(tasks, 5_000)
      Process.sleep(50)
      assert 50 = QueueServer.size()
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/task_queue/queue_server_test.exs --trace
```

---

## Trade-off analysis

| Aspect | cast (push) | call (pop/peek) | handle_info (cleanup) |
|--------|------------|-----------------|----------------------|
| Caller blocks? | No | Yes, until reply | N/A — internal |
| Ordering guarantee | Messages are ordered in mailbox | Same | Timer fires after interval |
| Failure visibility | Caller never knows if cast failed | Caller gets exit signal on crash | Lost silently |
| When to use | Fire-and-forget writes | Reads that need a result | Timers, monitor signals |

Reflection question: `push/1` uses `cast` so the caller does not wait. But what if the
queue is full and you want to apply backpressure? What would you change, and what is
the cost in terms of caller throughput?

---

## Common production mistakes

**1. `@impl GenServer` missing on callbacks**
Without `@impl`, the compiler cannot warn you when a callback name is misspelled.
`handle_csat` instead of `handle_cast` compiles silently — the message is never handled.

**2. Slow work inside `handle_call`**
`handle_call` blocks the GenServer for its entire duration. If your `:pop` callback
does a database read, all other callers wait. Extract slow work to a Task and use
`GenServer.reply/2` to respond asynchronously.

**3. Forgetting `handle_info` for unexpected messages**
Process monitors, `:DOWN` messages, and stray `send` calls all arrive as `handle_info`.
Without a catch-all clause, the GenServer crashes on the first unexpected message.

**4. Using `call` for fire-and-forget writes**
`push/1` uses `cast` because the caller does not need confirmation. Using `call` here
would halve throughput for no benefit — every push would wait for the server to ack.

**5. State mutation outside callbacks**
State is only valid inside callbacks. Never store the state in a module attribute or
ETS outside the GenServer — that defeats the entire point of process isolation.

---

## Resources

- [GenServer — HexDocs](https://hexdocs.pm/elixir/GenServer.html)
- [GenServer.call/3 — HexDocs](https://hexdocs.pm/elixir/GenServer.html#call/3)
- [Mix and OTP: GenServer](https://elixir-lang.org/getting-started/mix-otp/genserver.html)
- [Saša Jurić — The Soul of Erlang](https://www.youtube.com/watch?v=JvBT4XBdoUE) — 45-minute talk on why the process model exists
