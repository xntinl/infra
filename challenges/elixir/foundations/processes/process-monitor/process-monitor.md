# Monitors — observing processes without dying with them

**Project**: `worker_watcher` — a standalone watcher that observes a pool of workers, counts their deaths, and stays alive regardless.

---

## Project structure

```
worker_watcher/
├── lib/
│   └── worker_watcher.ex
├── test/
│   └── worker_watcher_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

---

## The business problem
Links are bidirectional and kill both sides (unless you trap exits). Sometimes
that's the opposite of what you want: "tell me when that process dies, but leave
me alone". That's a **monitor**.

In this exercise you build a watcher — no GenServer, no Supervisor — that:

1. Spawns N worker processes.
2. Monitors each of them.
3. Reports deaths without dying.
4. Survives even if every worker crashes.

Project structure:

```
worker_watcher/
├── lib/
│   └── worker_watcher.ex
├── test/
│   └── worker_watcher_test.exs
└── mix.exs
```

---

## Core concepts

### 1. A monitor is unidirectional

`Process.monitor(pid)` returns a reference. When `pid` dies (for any reason, normal
or abnormal), the monitoring process receives ONE message:

```elixir
{:DOWN, ref, :process, pid, reason}
```

The monitored process neither knows nor cares that it's being watched. If the watcher
dies first, the monitor is cleaned up silently.

### 2. Monitor vs link — the decision

| Property | Link | Monitor |
|----------|------|---------|
| Direction | bidirectional | unidirectional |
| On crash, other dies? | yes (unless trapping) | no |
| Fires on `:normal` exit? | no | YES |
| Multiple between same pair | no | yes (each with its own ref) |
| Cleanup | automatic | ref-based (`Process.demonitor/2`) |

Rule of thumb: **link** when two processes have a shared lifetime. **Monitor** when
one process needs to react to another's death but is otherwise independent.

### 3. Monitors fire on normal exits too

A linked `:normal` exit is silent. A monitored `:normal` exit delivers
`{:DOWN, ref, :process, pid, :normal}`. This catches people out — if your code
only handles "abnormal" deaths, remember that monitors don't make that distinction.

### 4. `Process.demonitor(ref, [:flush])`

If you're done watching, demonitor. The `:flush` option also removes any
already-queued `:DOWN` message from your mailbox for that ref — useful to avoid
a stale message triggering dead-pid logic later.

---

## Why monitor and not link + trap_exit

- A linked process + `trap_exit` achieves the "don't die with it" goal, but every linked death in the system now arrives in your mailbox — you can't selectively observe one target.
- Monitors are **per-target, per-call, ref-keyed** — you decide which observations you want, and you can stop observing without touching the target process.
- Monitors fire on `:normal` exits; links don't. For a watcher that counts ALL deaths (including clean stops), that's the right semantics.

---

## Design decisions

**Option A — link + trap_exit in the watcher**
- Pros: one mechanism for both "supervise" and "observe".
- Cons: trap_exit is a global flag on the process — you can't be trapping for some targets and not others; noisy if you only want to watch one worker in a busy process.

**Option B — `Process.monitor/1` per worker** (chosen)
- Pros: per-target refs give precise control; demonitor stops watching a single target without affecting others; no global process flag.
- Cons: monitors have a small bookkeeping cost per target; can leak if you forget to `demonitor/2`.

→ Chose **B** because the watcher's job is strictly observation with a fine-grained count. Trap_exit is overkill and leaks observability into every other linked relationship the watcher may later acquire.

---

## Implementation

### `mix.exs`
```elixir
defmodule WorkerWatcher.MixProject do
  use Mix.Project

  def project do
    [
      app: :worker_watcher,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Scaffold a project focused on one module so monitor semantics are isolated from supervision trees and GenServer callbacks.

```bash
mix new worker_watcher
cd worker_watcher
```

### `lib/worker_watcher.ex`

**Objective**: Use `Process.monitor/1` to receive `:DOWN` messages so the watcher observes worker deaths without the bidirectional coupling of a link.

```elixir
defmodule WorkerWatcher do
  @moduledoc """
  Spawns workers, monitors each, and counts deaths. The watcher is independent
  of the workers — it uses monitors (not links), so worker crashes never affect it.
  """

  @doc """
  Starts a watcher process. It returns a pid that will accept `:spawn_worker`
  and `:stats` messages. See `spawn_worker/1` and `stats/1`.
  """
  @spec start() :: pid()
  def start do
    # The watcher does NOT trap exits and does NOT link to its workers —
    # it uses monitors, which are immune to worker crashes by design.
    spawn(fn -> watcher_loop(%{refs: %{}, dead: 0}) end)
  end

  @doc "Asks the watcher to spawn+monitor a new worker that will crash on :crash."
  @spec spawn_worker(pid()) :: :ok
  def spawn_worker(watcher) do
    send(watcher, {:spawn_worker, self()})
    :ok
  end

  @doc "Returns {alive_count, dead_count}."
  @spec stats(pid()) :: {non_neg_integer(), non_neg_integer()}
  def stats(watcher) do
    send(watcher, {:stats, self()})

    receive do
      {:stats_reply, stats} -> stats
    after
      1_000 -> raise "watcher unresponsive"
    end
  end

  # --- internals -------------------------------------------------------------

  defp watcher_loop(state) do
    receive do
      {:spawn_worker, reply_to} ->
        # Use spawn (not spawn_link) — we are NOT tied to the worker's fate.
        worker = spawn(&worker_loop/0)

        # Monitor is unidirectional: worker dies -> we get a :DOWN message.
        # Worker has no idea it's being monitored. Watcher is unaffected by its crash.
        ref = Process.monitor(worker)

        send(reply_to, {:worker_spawned, worker})

        watcher_loop(%{state | refs: Map.put(state.refs, ref, worker)})

      {:stats, reply_to} ->
        alive = map_size(state.refs)
        send(reply_to, {:stats_reply, {alive, state.dead}})
        watcher_loop(state)

      # The central message of this exercise: a monitored process died.
      # Shape is always {:DOWN, ref, :process, pid, reason}.
      {:DOWN, ref, :process, _pid, _reason} ->
        # Drop the ref from our tracking map and increment the death counter.
        # We're still running — the worker's crash did not touch us.
        watcher_loop(%{
          state
          | refs: Map.delete(state.refs, ref),
            dead: state.dead + 1
        })
    end
  end

  defp worker_loop do
    receive do
      :crash -> exit(:boom)
      :stop -> :ok
    end
  end
end
```

### Step 3: `test/worker_watcher_test.exs`

**Objective**: Assert the watcher stays alive after every worker crash and that each `:DOWN` is keyed by the correct monitor reference, never a stray pid.

```elixir
defmodule WorkerWatcherTest do
  use ExUnit.Case, async: true
  doctest WorkerWatcher

  describe "monitor semantics" do
    test "watcher survives worker crashes" do
      watcher = WorkerWatcher.start()
      watcher_ref = Process.monitor(watcher)

      # Spawn three workers and collect their pids.
      workers =
        for _ <- 1..3 do
          :ok = WorkerWatcher.spawn_worker(watcher)

          receive do
            {:worker_spawned, w} -> w
          after
            500 -> flunk("did not receive worker pid")
          end
        end

      # Crash all of them.
      Enum.each(workers, &send(&1, :crash))

      # Give the watcher a moment to process every :DOWN.
      Process.sleep(50)

      # Watcher is still alive — monitors do not propagate death.
      refute_receive {:DOWN, ^watcher_ref, _, _, _}, 100
      assert Process.alive?(watcher)

      # And it counted the deaths.
      assert {0, 3} = WorkerWatcher.stats(watcher)
    end

    test "monitor fires on normal exit too (unlike links)" do
      watcher = WorkerWatcher.start()

      :ok = WorkerWatcher.spawn_worker(watcher)

      worker =
        receive do
          {:worker_spawned, w} -> w
        end

      # :stop causes a normal exit. Links would NOT propagate this.
      # Monitors fire anyway — that's a key behavioral difference.
      send(worker, :stop)
      Process.sleep(50)

      assert {0, 1} = WorkerWatcher.stats(watcher)
    end
  end

  describe "direct monitor usage" do
    test ":DOWN message shape is {:DOWN, ref, :process, pid, reason}" do
      {pid, ref} = spawn_monitor(fn -> exit(:pow) end)

      assert_receive {:DOWN, ^ref, :process, ^pid, :pow}, 500
    end

    test "demonitor with :flush removes a queued :DOWN message" do
      # Start a short-lived process and grab its ref.
      {pid, ref} = spawn_monitor(fn -> :ok end)

      # Wait for it to die and queue :DOWN.
      Process.sleep(20)

      # Flush both the monitor and any queued message for the ref.
      Process.demonitor(ref, [:flush])

      refute_receive {:DOWN, ^ref, :process, ^pid, _}, 50
    end
  end
end
```

### Step 4: Run

**Objective**: Run the suite to confirm monitored crashes never propagate back and the watcher's death counter converges to the expected value.

```bash
mix test
```

### Why this works

`Process.monitor/1` registers a unidirectional watch in the BEAM's monitor table keyed by a unique reference. When the target dies, the scheduler delivers exactly one `{:DOWN, ref, :process, pid, reason}` message and discards the monitor entry. The watcher has no `trap_exit` flag and no link to the workers, so worker crashes cannot reach it by any path other than the `:DOWN` message — which is just a regular message in the mailbox, handled in order. `Process.demonitor(ref, [:flush])` is the only way to reliably stop watching AND remove any already-queued `:DOWN`, which is what prevents the "stale death" bug.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== WorkerWatcher: demo ===\n")

    result_1 = Mix.env()
    IO.puts("Demo 1: #{inspect(result_1)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create `lib/monitor_demo.ex` and test in `iex`:

```elixir
defmodule MonitorDemo do
  def observed_worker(name) do
    pid = spawn(fn -> worker_loop(name) end)
    ref = Process.monitor(pid)
    {pid, ref}
  end

  def worker_loop(name) do
    receive do
      :stop -> IO.puts("#{name} stopping")
      msg -> IO.puts("#{name} got: #{inspect(msg)}"); worker_loop(name)
    after
      5000 -> worker_loop(name)
    end
  end

  def observe(pid, ref) do
    receive do
      {:DOWN, ^ref, :process, ^pid, reason} ->
        IO.inspect({:process_down, pid, reason})
    after
      1000 -> IO.puts("Process still alive")
    end
  end
end

# Test it
{pid, ref} = MonitorDemo.observed_worker("worker1")
send(pid, "hello")
Process.sleep(100)

send(pid, :stop)
MonitorDemo.observe(pid, ref)
```

## Benchmark

<!-- benchmark N/A: tema conceptual — monitor setup and :DOWN delivery are sub-microsecond BEAM internals; a meaningful benchmark would measure the BEAM itself, not user code. -->

---

## Trade-offs and production gotchas

**1. Monitors leak if you don't demonitor**
If you monitor a long-lived process and then lose interest, the monitor stays.
When the process eventually dies, you get a `:DOWN` you no longer want — and
possibly a pattern-match crash because the rest of your code has moved on.
Call `Process.demonitor(ref, [:flush])` when done.

**2. Monitors fire on `:normal` — links don't**
This trips people converting link code to monitor code. Handle `:normal` in
your `:DOWN` clause or pattern-match only the reasons you care about.

**3. You can't monitor an already-dead pid safely as a "missed the crash" signal**
`Process.monitor(dead_pid)` immediately sends you `{:DOWN, ref, :process, pid, :noproc}`.
That's fine — just be ready to handle `:noproc` as "it was already gone".

**4. Multiple monitors on the same target are independent**
Each call to `Process.monitor/1` returns a fresh ref and produces its own `:DOWN`.
This is useful when two subsystems each need their own signal, but it means you
can't treat "did I already monitor this pid?" as a boolean — track your own refs.

**5. When NOT to use monitors**
If you need the watcher to die with the target (co-lifetime), use a link.
If you need automatic restart, use a Supervisor. Monitors are the building block
for "tell me, I'll decide" logic — they are not a restart mechanism on their own.

---

## Reflection

- Your watcher monitors 50k workers. Each `:DOWN` adds a tiny amount of work. Under a surge, the watcher's mailbox backs up and `stats/1` latency balloons. Is the bottleneck the monitor count, the `:DOWN` processing, or the mailbox contention with `:stats`? How would you profile it, and what architectural change (partition the watcher? offload counting to ETS?) fixes the right one?
- You treat `:DOWN` with reason `:noproc` as "the target was already dead" and increment the death counter the same as a crash. Is conflating these two reasons a bug in your metric, or a reasonable simplification? When does it stop being reasonable?

---

## Resources

- [`Process.monitor/1`](https://hexdocs.pm/elixir/Process.html#monitor/1)
- [`spawn_monitor/1`](https://hexdocs.pm/elixir/Kernel.html#spawn_monitor/1) — one-call spawn + monitor
- ["Monitors" in Learn You Some Erlang](https://learnyousomeerlang.com/errors-and-processes#monitors) — clear visual explanation

---

## Why Monitors — observing processes without dying with them matters

Mastering **Monitors — observing processes without dying with them** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/worker_watcher_test.exs`

```elixir
defmodule WorkerWatcherTest do
  use ExUnit.Case, async: true

  doctest WorkerWatcher

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert WorkerWatcher.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. Monitors Are One-Way Exit Notifications
Unlike links (bidirectional), monitors are one-way. If the monitored process dies, the monitor receives a `DOWN` message. The monitoring process is not affected.

### 2. Monitors Don't Create Bidirectional Coupling
With links, if either process exits, both die (unless one traps exits). With monitors, the monitoring process remains unaffected.

### 3. Multiple Monitors on One Process
You can monitor the same process multiple times. Each monitor sends its own `DOWN` message.

---
