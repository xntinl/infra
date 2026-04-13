# Process links and EXIT propagation

**Project**: `worker_pair` — two linked workers that live and die together.

---

## Project context

You're prototyping a pipeline where a `Producer` and `Consumer` process must be
co-alive: if one crashes, the other is useless and must die too. Later you'll
replace this with a `Supervisor`, but first you need to understand what a link
actually does underneath — because `Supervisor` is built on top of links.

This exercise uses **raw `spawn_link`** and `Process.link/1`. No GenServer.

Project structure:

```
worker_pair/
├── lib/
│   └── worker_pair.ex
├── test/
│   └── worker_pair_test.exs
└── mix.exs
```

---

## Core concepts

### 1. A link is bidirectional

When A links to B, if **either** dies abnormally, an EXIT signal is sent to the
other. Not a message — an EXIT signal. By default that signal propagates and
kills the receiver too. This is the default BEAM behavior.

```
spawn_link creates A ──link──── B
  A crashes  ──EXIT──▶ B       B dies
  B crashes  ──EXIT──▶ A       A dies
```

### 2. `spawn_link/1` vs `spawn/1` + `Process.link/1`

`spawn_link/1` is atomic — the link is in place before the child starts running,
so you can't miss a crash during startup. `spawn/1` then `Process.link/1` has a
race: the child could crash in the gap between spawn and link, and you'd never
see the EXIT.

Use `spawn_link` whenever the link is needed from the start.

### 3. `Process.flag(:trap_exit, true)` turns signals into messages

By default a linked crash kills you. If you `Process.flag(:trap_exit, true)`,
the EXIT signal is converted into a regular message `{:EXIT, from, reason}`
delivered to your mailbox. You're now a *system process* — you decide what
to do instead of dying.

This is how supervisors work: they trap exits, receive `{:EXIT, ...}`, and
restart the child.

### 4. Normal exit does not propagate

If a linked process exits with reason `:normal` (or `:shutdown`), no EXIT is
sent to linked processes. Only abnormal exits (crashes, `exit(:bang)`) propagate.

---

## Why raw links and not `Supervisor`

- `Supervisor` is the production answer, but it **is** built on links + trap_exit. Using it here would hide the very mechanism we're teaching.
- `Process.monitor/1` observes without dying, but it's one-directional — wrong shape when A and B must be *co-alive*.
- `Task.async/1` uses a link, but only for the async-return pattern, not for peer co-lifetime.

Raw `spawn_link` is the primitive; everything else layers on top.

---

## Design decisions

**Option A — `spawn/1` + `Process.link/1` after start**
- Pros: link is explicit and reversible (`unlink/1`).
- Cons: race window — if the child crashes between `spawn` and `link`, you never see the EXIT.

**Option B — `spawn_link/1`** (chosen)
- Pros: atomic link creation; no startup race; the link is in place before the child runs the first instruction.
- Cons: slightly less flexible (you can still `unlink/1` later if needed).

→ Chose **B** because the whole guarantee we're teaching — "if one dies, the other dies" — is invalidated by a startup race. `spawn_link` makes the invariant hold from tick zero.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Dependencies (`mix.exs`)

```elixir
defp deps do
  # BEAM primitives only — no deps.
  []
end
```


### Step 1: Create the project

**Objective**: Create a bare Mix project so the link semantics are studied on raw processes, without Supervisor or GenServer hiding the propagation.

```bash
mix new worker_pair
cd worker_pair
```

### Step 2: `lib/worker_pair.ex`

**Objective**: Show that `spawn_link` bidirectionally binds two processes so an abnormal exit in one tears down the other — the foundation Supervisor builds on.

```elixir
defmodule WorkerPair do
  @moduledoc """
  Demonstrates raw process links and EXIT propagation — no GenServer, no
  Supervisor, just spawn_link and Process primitives.
  """

  @doc """
  Spawns two linked workers. Returns `{pid_a, pid_b}`.

  Both workers simply loop, waiting for a `:crash` message. If either crashes,
  the link ensures the other dies too — demonstrating bidirectional propagation.
  """
  @spec start_pair() :: {pid(), pid()}
  def start_pair do
    parent = self()

    # We spawn A first; A then spawns B linked to itself. That gives us A ── B.
    # We also link A to the parent so the test can observe the pair's death.
    pid_a =
      spawn_link(fn ->
        # A spawns B *linked to A*. Atomic via spawn_link — no race window.
        pid_b = spawn_link(fn -> worker_loop(:b) end)
        send(parent, {:spawned, self(), pid_b})
        worker_loop(:a)
      end)

    # Collect B's pid from A — spawn_link returns only A's pid.
    receive do
      {:spawned, ^pid_a, pid_b} -> {pid_a, pid_b}
    after
      1_000 -> raise "workers did not report their pids"
    end
  end

  # A trivial loop: wait for messages, crash on :crash, echo everything else.
  defp worker_loop(name) do
    receive do
      :crash ->
        # Non-normal exit — this WILL propagate across the link.
        exit({:boom, name})

      {:stop, :normal} ->
        # Normal exits do NOT propagate. The other worker survives.
        :ok

      {:ping, from} ->
        send(from, {:pong, name, self()})
        worker_loop(name)
    end
  end

  @doc """
  Starts a "supervisor-like" process that traps exits and OBSERVES the pair
  crashing, without dying itself. This is the mechanism real supervisors use.
  """
  @spec start_observer(pid()) :: pid()
  def start_observer(notify_to) do
    spawn(fn ->
      # trap_exit converts EXIT signals into plain messages.
      # Without this, linking to a crashing worker would kill us.
      Process.flag(:trap_exit, true)

      receive do
        {:link_to, pid} ->
          Process.link(pid)
          wait_for_exit(notify_to)
      end
    end)
  end

  defp wait_for_exit(notify_to) do
    receive do
      {:EXIT, pid, reason} ->
        # The EXIT became a message because we trapped — we're alive, we observed it.
        send(notify_to, {:observed_exit, pid, reason})
    end
  end
end
```

### Step 3: `test/worker_pair_test.exs`

**Objective**: Assert that `:normal` exits do not propagate while abnormal exits do, exposing the rule that determines when links actually cascade.

```elixir
defmodule WorkerPairTest do
  use ExUnit.Case, async: true

  describe "start_pair/0 — bidirectional propagation" do
    test "crashing A also kills B" do
      # The test process is linked to A by start_pair — we must trap exits
      # so the EXIT from A doesn't kill the test itself.
      Process.flag(:trap_exit, true)

      {pid_a, pid_b} = WorkerPair.start_pair()
      ref_b = Process.monitor(pid_b)

      send(pid_a, :crash)

      # B must go DOWN because it was linked to A.
      assert_receive {:DOWN, ^ref_b, :process, ^pid_b, reason}, 500
      assert reason == {:boom, :a} or match?({:boom, _}, reason)

      # And we, the parent, also got the EXIT from A (because start_pair
      # used spawn_link, linking A to us).
      assert_receive {:EXIT, ^pid_a, {:boom, :a}}, 500
    end

    test "crashing B also kills A" do
      Process.flag(:trap_exit, true)

      {pid_a, pid_b} = WorkerPair.start_pair()
      ref_a = Process.monitor(pid_a)

      send(pid_b, :crash)

      assert_receive {:DOWN, ^ref_a, :process, ^pid_a, _reason}, 500
    end

    test "normal exit does not propagate" do
      Process.flag(:trap_exit, true)

      {pid_a, pid_b} = WorkerPair.start_pair()
      ref_b = Process.monitor(pid_b)

      # A exits normally — B should survive because :normal does not propagate.
      send(pid_a, {:stop, :normal})

      # A goes DOWN normally, but B must still be alive.
      assert_receive {:EXIT, ^pid_a, :normal}, 500
      refute_receive {:DOWN, ^ref_b, :process, _, _}, 100
      assert Process.alive?(pid_b)

      # Cleanup: kill B for the next test.
      Process.exit(pid_b, :kill)
    end
  end

  describe "start_observer/1 — trap_exit lets you survive a linked crash" do
    test "observer sees EXIT as a message and stays alive" do
      test_pid = self()
      observer = WorkerPair.start_observer(test_pid)

      # Spawn a victim process and link the observer to it.
      victim = spawn(fn -> receive do :die -> exit(:kaboom) end end)
      send(observer, {:link_to, victim})
      # Small delay to ensure the link is established before we crash the victim.
      Process.sleep(10)

      send(victim, :die)

      assert_receive {:observed_exit, ^victim, :kaboom}, 500
      assert Process.alive?(observer)
    end
  end
end
```

### Step 4: Run

**Objective**: Run the tests under the supervision of the ExUnit runner — its own trap-exit behaviour is the guardrail that makes linked-crash tests observable.

```bash
mix test
```

### Why this works

`spawn_link/1` inserts the link into the BEAM's link table atomically with process creation — the scheduler cannot interleave anything between "process exists" and "link exists". When a linked process dies abnormally, the BEAM walks its link table and sends EXIT signals to all linked processes; those signals are delivered by the scheduler, not the mailbox, so they bypass normal receive ordering. `trap_exit` doesn't prevent the signal — it converts it at delivery time into a regular `{:EXIT, pid, reason}` message, which is what lets supervisors observe and react instead of dying.

---


## Key Concepts

### 1. Links Create Bidirectional Exit Notifications
When you `spawn_link`, exiting one process sends an exit signal to the other. This is how supervision works.

### 2. Supervisors Trap Exits to Handle Child Failures
With `Process.flag(:trap_exit, true)`, supervisors can monitor and restart child processes.

### 3. Unlinked Processes are Independent
`spawn` creates independent processes. They can crash without affecting the parent. Use `spawn` for fire-and-forget tasks.

---
## Benchmark

<!-- benchmark N/A: tema conceptual — link signalling is sub-microsecond BEAM internals; measuring it adds no insight beyond "essentially free". -->

---

## Trade-offs and production gotchas

**1. `spawn/1` + `Process.link/1` has a startup race**
If the child crashes before `Process.link/1` runs, you miss the EXIT. Always
use `spawn_link` when the link must exist from the very start.

**2. Trapping exits is a commitment, not a free upgrade**
Once you trap exits, every linked death becomes a message you MUST handle.
If you ignore `{:EXIT, ...}` messages, your mailbox grows until the VM runs
out of memory. Only trap exits in processes that actually supervise others.

**3. `Process.exit(pid, :kill)` cannot be trapped**
`:kill` is special — it bypasses `trap_exit`. It's the "force quit" of the BEAM.
Reserve it for actually-stuck processes; using it as a normal shutdown signal
defeats clean termination in your children.

**4. Links are for dependency, monitors are for observation**
If you want to *watch* a process without dying with it and without the ceremony
of `trap_exit`, use `Process.monitor/1` instead — it's one-directional and
doesn't require the trap_exit commitment.

**5. When NOT to use raw links**
In production, almost always use `Supervisor` (or `DynamicSupervisor`). Raw links
are for learning and for the rare case where you need a co-lifetime guarantee
outside the supervision tree.

---

## Reflection

- A process traps exits but never pattern-matches on `{:EXIT, _, _}`. Over a few minutes, the node OOMs. Walk through what exactly fills memory, and why the supervisor-shaped workaround (match and ignore) is both idiomatic and mandatory.
- Your pair ends up with 500 linked worker chains node-wide. One worker crashes; the crash propagates across dozens of processes simultaneously. Is this a design defect or is this *exactly* the "let it crash" philosophy? Justify by pointing to what a `Supervisor` would do differently.

---

## Resources

- [`Process` — Elixir stdlib](https://hexdocs.pm/elixir/Process.html)
- ["Links and monitors" — Elixir getting started](https://hexdocs.pm/elixir/processes.html#links)
- [Joe Armstrong — "Making reliable distributed systems in the presence of software errors"](https://erlang.org/download/armstrong_thesis_2003.pdf) — the thesis that introduced the supervision model
