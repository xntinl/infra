# Processes: spawn, send, and receive

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

You are building `task_queue`, an OTP-based task management system. At this point the
project is brand new. The first concrete need is a **worker process** that can receive
job descriptions, execute them, and report results back to a coordinator.

Before reaching GenServer, you need to understand the raw primitives underneath it:
`spawn`, `send`, and `receive`. Every OTP abstraction is built on exactly these three.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── worker_process.ex
│       └── accumulator.ex
├── test/
│   └── task_queue/
│       └── worker_process_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## Why learn raw processes before GenServer

GenServer is the right tool for production workers. But if you do not understand `spawn`,
`send`, and `receive`, GenServer will feel like magic — and magic breaks in unpredictable
ways. When a GenServer mailbox fills up, when a message is never matched, when a process
silently hangs: diagnosing these requires knowing exactly what happens at the process level.

This exercise builds that foundation.

---

## Why BEAM processes are different from OS threads

A BEAM process is not an OS thread. It has its own heap, its own garbage collector, and
its own mailbox. Creating a million BEAM processes is normal; creating a million OS threads
is not. The key consequence: **process isolation**. If one process crashes, its heap is
collected and no other process is affected. This is the mechanical basis of "let it crash".

The mailbox is a queue of messages in arrival order. `receive` scans the mailbox from the
front, trying to pattern-match each message against its clauses. A message that does not
match any clause stays in the mailbox for the next `receive`. This means unhandled messages
accumulate — a real production problem if not designed carefully.

---

## The business problem

The task_queue system needs a primitive worker loop: a process that receives a `{:run, job_fn,
reply_to}` message, executes `job_fn.()`, and sends the result back to `reply_to`. The process
must also handle a `:stop` message for graceful shutdown. It should not crash if `job_fn`
raises — instead it should send `{:error, reason}` back to the caller.

---

## Implementation

### Step 1: Create the project

```bash
mix new task_queue --sup
cd task_queue
mkdir -p lib/task_queue
mkdir -p test/task_queue
```

### Step 2: `lib/task_queue/worker_process.ex`

```elixir
defmodule TaskQueue.WorkerProcess do
  @moduledoc """
  A raw-process worker built with spawn, send, and receive.

  This is the conceptual predecessor to TaskQueue.Worker (which uses GenServer).
  Understanding this module makes GenServer internals transparent.
  """

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Spawns a worker process and returns its PID.
  The worker runs a receive loop waiting for job messages.
  """
  @spec start() :: pid()
  def start do
    spawn(fn -> loop() end)
  end

  @doc """
  Spawns a linked worker process.
  If the worker crashes, the calling process receives an exit signal.
  Use this when the caller must know about worker failures.
  """
  @spec start_link() :: pid()
  def start_link do
    spawn_link(fn -> loop() end)
  end

  @doc """
  Sends a job to the worker and synchronously waits for the result.

  Returns `{:ok, result}` or `{:error, reason}`.
  Raises if the worker does not respond within `timeout_ms`.
  """
  @spec run_job(pid(), (-> any()), pos_integer()) :: {:ok, any()} | {:error, any()}
  def run_job(worker_pid, job_fn, timeout_ms \\ 5_000) do
    send(worker_pid, {:run, job_fn, self()})

    receive do
      {:result, value} -> {:ok, value}
      {:error, reason} -> {:error, reason}
    after
      timeout_ms -> raise RuntimeError, "Worker did not respond within #{timeout_ms}ms"
    end
  end

  @doc """
  Sends a stop signal to the worker. Returns immediately without waiting.
  """
  @spec stop(pid()) :: :ok
  def stop(worker_pid) do
    send(worker_pid, :stop)
    :ok
  end

  # ---------------------------------------------------------------------------
  # Private — the receive loop
  # ---------------------------------------------------------------------------

  defp loop do
    receive do
      {:run, job_fn, reply_to} ->
        try do
          value = job_fn.()
          send(reply_to, {:result, value})
        rescue
          e -> send(reply_to, {:error, e})
        end

        loop()

      :stop ->
        :ok
    end
  end
end
```

The `loop/0` function is recursive: after handling a `:run` message, it calls itself to
wait for the next message. The `:stop` branch does **not** call `loop/0` — the process
exits normally by returning `:ok` from the function.

The `try/rescue` inside the `:run` handler ensures that a failing job does not crash the
worker. Instead, the exception is captured and sent back as `{:error, e}`. The worker
remains alive and ready for the next job. This is the manual equivalent of what GenServer
does automatically with its callback error handling.

Why call `loop()` inside each branch rather than after the `receive` block? Because
pattern matching is exhaustive — if `:stop` arrives and we called `loop()` unconditionally
after the receive, the process would loop forever instead of terminating.

### Step 3: `lib/task_queue/accumulator.ex`

A process that holds a list of completed task results. This demonstrates stateful
receive loops — the same pattern GenServer uses internally.

```elixir
defmodule TaskQueue.Accumulator do
  @moduledoc """
  A stateful process that accumulates task results.
  State is carried as a recursive function argument — no shared memory, no locks.
  """

  @spec start() :: pid()
  def start do
    spawn(fn -> loop([]) end)
  end

  @doc "Records a completed task result."
  @spec record(pid(), any()) :: :ok
  def record(pid, result) do
    send(pid, {:record, result})
    :ok
  end

  @doc "Synchronously fetches the accumulated list of results."
  @spec fetch(pid(), pos_integer()) :: [any()] | {:error, :timeout}
  def fetch(pid, timeout_ms \\ 1_000) do
    send(pid, {:fetch, self()})
    receive do
      {:results, list} -> list
    after
      timeout_ms -> {:error, :timeout}
    end
  end

  @doc "Clears all accumulated results."
  @spec clear(pid()) :: :ok
  def clear(pid) do
    send(pid, :clear)
    :ok
  end

  @spec stop(pid()) :: :ok
  def stop(pid) do
    send(pid, :stop)
    :ok
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  defp loop(results) do
    receive do
      {:record, result} ->
        loop([result | results])

      {:fetch, reply_to} ->
        send(reply_to, {:results, Enum.reverse(results)})
        loop(results)

      :clear ->
        loop([])

      :stop ->
        :ok
    end
  end
end
```

The accumulator uses `[result | results]` to prepend new entries (O(1)) and
`Enum.reverse/1` on fetch to return them in insertion order. This is the standard
Elixir pattern for building lists incrementally — appending with `results ++ [result]`
would be O(n) on every insert, which degrades to O(n^2) over time.

The state (`results`) is carried as a function argument through recursive calls.
Each branch decides independently whether to recurse (`:record`, `:fetch`, `:clear`)
or terminate (`:stop`). This is exactly what GenServer does internally with its
`{:noreply, new_state}` and `{:stop, reason, state}` return tuples.

### Step 4: Given tests — must pass without modification

```elixir
# test/task_queue/worker_process_test.exs
defmodule TaskQueue.WorkerProcessTest do
  use ExUnit.Case, async: true

  alias TaskQueue.WorkerProcess
  alias TaskQueue.Accumulator

  describe "WorkerProcess.run_job/3" do
    test "executes a successful job and returns the result" do
      worker = WorkerProcess.start()
      assert {:ok, 42} = WorkerProcess.run_job(worker, fn -> 40 + 2 end)
      WorkerProcess.stop(worker)
    end

    test "captures exceptions and returns {:error, reason}" do
      worker = WorkerProcess.start()
      assert {:error, _reason} = WorkerProcess.run_job(worker, fn -> raise "boom" end)
      # Worker must still be alive after an error
      assert {:ok, :alive} = WorkerProcess.run_job(worker, fn -> :alive end)
      WorkerProcess.stop(worker)
    end

    test "worker exits cleanly on :stop" do
      worker = WorkerProcess.start_link()
      ref = Process.monitor(worker)
      WorkerProcess.stop(worker)
      assert_receive {:DOWN, ^ref, :process, ^worker, :normal}, 500
    end

    test "run_job raises on timeout" do
      worker = WorkerProcess.start()

      assert_raise RuntimeError, fn ->
        WorkerProcess.run_job(worker, fn -> Process.sleep(200) end, 50)
      end

      WorkerProcess.stop(worker)
    end
  end

  describe "Accumulator" do
    test "records results and fetches them in insertion order" do
      acc = Accumulator.start()
      Accumulator.record(acc, {:ok, "job_1"})
      Accumulator.record(acc, {:ok, "job_2"})
      Accumulator.record(acc, {:error, :timeout})

      assert [
               {:ok, "job_1"},
               {:ok, "job_2"},
               {:error, :timeout}
             ] = Accumulator.fetch(acc)

      Accumulator.stop(acc)
    end

    test "clear resets the result list" do
      acc = Accumulator.start()
      Accumulator.record(acc, :a)
      Accumulator.record(acc, :b)
      Accumulator.clear(acc)
      assert [] = Accumulator.fetch(acc)
      Accumulator.stop(acc)
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/task_queue/worker_process_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Raw processes (this exercise) | GenServer | Task |
|--------|------------------------------|-----------|------|
| Boilerplate | High — manual protocol | Low — OTP provides structure | Minimal |
| Error handling | Manual try/rescue in loop | Built-in crash semantics | Automatic propagation |
| Observability | None out of the box | sys.get_state, :sys.trace | Task.await result |
| Supervision | Manual monitor/link wiring | Works with Supervisor directly | Task.Supervisor |
| When to use | Learning / extreme control | Production servers with state | Concurrent one-shot work |

Reflection question: the accumulator uses a list prepended with `[result | results]` and
reversed on `fetch`. Why not append with `results ++ [result]`? What is the time complexity
of each, and when does it matter in a task queue with 10,000 entries per minute?

---

## Common production mistakes

**1. Unhandled messages growing the mailbox**
If your receive loop does not have a catch-all clause, every unexpected message stays
in the mailbox permanently. Under load, a process with a million unmatched messages
will consume gigabytes of memory. Add a `_other -> :ok` clause and log unexpected messages.

**2. Missing `after` in receive for external calls**
A receive without `after` blocks forever if the expected message never arrives. Any receive
that depends on a message from another process should have a timeout. The standard is 5,000ms
for interactive calls.

**3. Forgetting to capture `self()` before spawn**
Inside a `spawn` closure, `self()` returns the PID of the spawned process, not the parent.
Capture the parent PID in a variable before `spawn` if the child needs to reply.

**4. Using spawn where spawn_link belongs**
Bare `spawn` creates an isolated process. If it crashes, the parent never knows. In an OTP
tree, workers should be linked (via GenServer + Supervisor) so failures propagate correctly.

**5. Calling loop() after every receive branch unconditionally**
If the `:stop` branch also calls `loop()`, the process never terminates. Each branch must
decide explicitly whether to continue the loop.

---

## Resources

- [Process module — HexDocs](https://hexdocs.pm/elixir/Process.html)
- [Kernel.spawn_link/1 — HexDocs](https://hexdocs.pm/elixir/Kernel.html#spawn_link/1)
- [Elixir Getting Started: Processes](https://elixir-lang.org/getting-started/processes.html)
- [The BEAM Book — Erik Stenman](https://github.com/happi/theBeamBook) — chapters on process structure and mailboxes
