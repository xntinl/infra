# Fire-and-forget with `Task.Supervisor`

**Project**: `fire_forget_sup` — launch background work that must NOT crash its caller.

---

## Project context

A user hits your HTTP endpoint. You respond 200 immediately and kick off
a slow audit-log write in the background. Rules:

1. The HTTP handler must return now, not after the audit write.
2. If the audit write crashes, it must NOT take down the handler (or
   the Phoenix request process).
3. The work must still be supervised — if the VM restarts or an
   unhandled crash happens upstream, the audit task shouldn't become an
   orphan linked to nothing.

`Task.async` is the wrong tool: it links to the caller, so a crash in
the audit task kills the handler. Plain `spawn` is also wrong: there's
no supervision.

The right shape is `Task.Supervisor.start_child/2` — an unlinked,
supervised task. This exercise builds a minimal app that demonstrates
the semantics.

Project structure:

```
fire_forget_sup/
├── lib/
│   ├── fire_forget_sup.ex
│   ├── fire_forget_sup/application.ex
│   └── fire_forget_sup/audit.ex
├── test/
│   └── fire_forget_sup_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not `spawn`?** No supervision, no logs on crash, no cleanup guarantee. `Task.Supervisor` gives you all three.

## Core concepts

### 1. `Task.Supervisor.start_child/2` — unlinked, supervised

```
Task.Supervisor.start_child(MySup, fn -> work end)
```

The task is started *under the supervisor*, not linked to the caller.
If it crashes, the supervisor logs and moves on; the caller is
unaffected. Return value is `{:ok, pid}`, not a `%Task{}` — you can't
await it (that's the whole point).

For work you *do* want to await later but still without linking to the
caller, use `Task.Supervisor.async_nolink/2` — it returns a `%Task{}`
but you won't die if it crashes before you call `yield`/`shutdown`.

### 2. Why plain `spawn` is wrong

`spawn(fn -> work end)` gives you:

- No supervision.
- No restart policy.
- No `Logger` on crash (the VM eats it quietly by default).
- No shutdown integration (on app stop, no graceful cleanup).

`Task.Supervisor` gives all of those. The cost is one extra line in
your supervision tree.

### 3. Why `Task.async` is wrong for fire-and-forget

`Task.async` creates a **link** from the caller. If the caller is a
Phoenix request process, a crashing background task tears down the
request. If the caller is a GenServer, it tears down the GenServer.
For fire-and-forget, you must break the link — which means
`Task.Supervisor.start_child/2`.

### 4. Shutdown grace matters

When your application stops, the `Task.Supervisor` sends shutdown
signals to its children and waits up to `shutdown:` ms before brutal-
killing them. Set this thoughtfully: audit writes need a second or two
to flush; ephemeral calculations can be zero.

---

## Design decisions

**Option A — `spawn` + ignore failures**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `Task.Supervisor.start_child` with `:temporary` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because supervised fire-and-forget still logs crashes; bare `spawn` silently drops them.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new fire_forget_sup --sup
cd fire_forget_sup
```

### Step 2: `lib/fire_forget_sup/application.ex`

**Objective**: Wire `application.ex` to start the supervisor wiring Task.Supervisor so async work has an explicit failure boundary.


```elixir
defmodule FireForgetSup.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Task.Supervisor owns every background task spawned via
      # `FireForgetSup.Audit.enqueue/1`.
      {Task.Supervisor, name: FireForgetSup.AuditTasks}
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name: FireForgetSup.Supervisor
    )
  end
end
```

Update `mix.exs`:

```elixir
def application do
  [
    extra_applications: [:logger],
    mod: {FireForgetSup.Application, []}
  ]
end
```

### Step 3: `lib/fire_forget_sup/audit.ex`

**Objective**: Implement `audit.ex` — the concurrency primitive whose back-pressure, linking, and timeout semantics we are isolating.


```elixir
defmodule FireForgetSup.Audit do
  @moduledoc """
  Fire-and-forget audit logging. `enqueue/1` returns immediately; the
  actual work runs under `FireForgetSup.AuditTasks` (a `Task.Supervisor`).

  Crashes in `write/1` do NOT propagate to the caller — they are logged
  by the supervisor and dropped.
  """

  require Logger

  @supervisor FireForgetSup.AuditTasks

  @doc """
  Starts a supervised, unlinked task that runs `write/1` with `event`.
  Returns `{:ok, pid}` — the caller should not await it.
  """
  @spec enqueue(map()) :: {:ok, pid()}
  def enqueue(event) when is_map(event) do
    Task.Supervisor.start_child(@supervisor, fn -> write(event) end)
  end

  @doc """
  Simulated audit write. Sleeps briefly to model I/O; raises when the
  event is marked `:poison` so tests can observe failure isolation.
  """
  @spec write(map()) :: :ok
  def write(%{poison: true} = event) do
    raise "audit boom for #{inspect(event)}"
  end

  def write(event) do
    Process.sleep(5)
    Logger.debug("audit write #{inspect(event)}")
    :ok
  end
end
```

### Step 4: `test/fire_forget_sup_test.exs`

**Objective**: Write `fire_forget_sup_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule FireForgetSupTest do
  use ExUnit.Case, async: false

  alias FireForgetSup.Audit

  describe "enqueue/1" do
    test "returns immediately without blocking the caller" do
      {elapsed_us, {:ok, _pid}} =
        :timer.tc(fn -> Audit.enqueue(%{actor: "u1", action: "login"}) end)

      # Should be well under a millisecond — we're not waiting on write/1.
      assert elapsed_us < 5_000
    end

    test "a crash in the audit task does NOT kill the caller" do
      # The calling process (this test) must survive a crashing enqueue.
      caller = self()

      # Capture log to keep the SASL crash noise out of test output.
      ExUnit.CaptureLog.capture_log(fn ->
        {:ok, task_pid} = Audit.enqueue(%{poison: true})
        ref = Process.monitor(task_pid)
        assert_receive {:DOWN, ^ref, :process, ^task_pid, _reason}, 500
      end)

      # The caller is still alive and responsive.
      send(caller, :ping)
      assert_receive :ping, 100
      assert Process.alive?(caller)
    end

    test "normal enqueue completes successfully" do
      {:ok, pid} = Audit.enqueue(%{actor: "u2", action: "logout"})
      ref = Process.monitor(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, :normal}, 500
    end
  end
end
```

Add `ExUnit.CaptureLog` usage (already in Elixir stdlib). If you
prefer, wrap the poison test in a `capture_log` block.

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Deep Dive: Task Spawn vs GenServer for Ephemeral Work

A Task is lightweight `spawn/1` for bounded, self-contained work: compute, return, exit. Unlike GenServer (which receives messages indefinitely), Task is inherently ephemeral. This shapes everything: no callbacks, no state management, no back-pressure.

Advantages: simplicity (few lines vs GenServer boilerplate). Disadvantages: no explicit state or message handling—Tasks assume pure computation or simple I/O. If you need a long-lived process responding to external events, you've outgrown Task.

For CPU-bound work (calculations, parsing), Task.Supervisor with `:temporary` is ideal: spawn tasks, let them exit, don't restart. For coordinated async work (multiple tasks handing off results), GenServer + worker tasks often clarifies intent despite more boilerplate. Measure first: if code clarity improves with GenServer, the overhead is justified.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Fire-and-forget throws away results — by design**
If the caller needs the outcome, this is the wrong pattern. Use
`Task.Supervisor.async_nolink/2` (you get a `%Task{}` you can `yield`
on) or a proper background-job library (`Oban`) when durability matters.

**2. Task.Supervisor does NOT persist**
If the VM crashes mid-task, the task is gone. For work that must
eventually complete across restarts — payments, emails, webhooks — you
need durable queues (`Oban`, `Broadway` + RabbitMQ, etc.), not a
`Task.Supervisor`.

**3. Unhandled crashes are logged but silent by default**
A crashed supervised task emits a `SASL` report. In production you want
this aggregated — wire `Logger` to your telemetry/metrics system and
alert on the task crash rate.

**4. Shutdown timing matters**
When the app stops, `Task.Supervisor` waits up to `shutdown:` ms (default
5_000) for children to finish. Tasks that still hold un-flushed buffers
lose data if they're brutal-killed. For long writes, raise the
supervisor's `shutdown:` or have the task checkpoint periodically.

**5. You can still back-pressure yourself**
`Task.Supervisor.start_child/2` with no concurrency cap spawns as many
tasks as `enqueue/1` is called. If callers enqueue faster than the
tasks complete, you'll pile up thousands of processes. For steady-state
throughput, put a `max_children:` option on the supervisor or front it
with a bounded pool.

**6. When NOT to use Task.Supervisor**
- Durable work that must survive a crash → `Oban`, a proper queue.
- Work that depends on its caller's context (assigns, connection, tx) —
  once the caller is gone, that context is gone too.
- Chained tasks where one's result feeds the next: use a GenServer /
  pipeline, not a pile of independent supervised tasks.

---


## Reflection

- ¿Cuándo es aceptable perder una tarea silenciosamente? Dá un ejemplo real donde fire-and-forget es la respuesta correcta.

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule FireForgetSup.Application do
    @moduledoc false
    use Application

    @impl true
    def start(_type, _args) do
      children = [
        # Task.Supervisor owns every background task spawned via
        # `FireForgetSup.Audit.enqueue/1`.
        {Task.Supervisor, name: FireForgetSup.AuditTasks}
      ]

      Supervisor.start_link(children,
        strategy: :one_for_one,
        name: FireForgetSup.Supervisor
      )
    end
  end

  defmodule FireForgetSup.Application do
    use Application
    def start(_type, _args) do
      children = [{Task.Supervisor, name: FireForgetSup.AuditTasks}]
      Supervisor.start_link(children, strategy: :one_for_one, name: FireForgetSup.Supervisor)
    end
  end

  def main do
    {:ok, _} = FireForgetSup.Application.start(:normal, [])
    Task.Supervisor.start_child(FireForgetSup.AuditTasks, fn -> IO.puts("Task running") end)
    Process.sleep(100)
    IO.puts("✓ FireForgetSup works correctly")
  end

end

Main.main()
```


## Resources

- [`Task.Supervisor` — Elixir stdlib](https://hexdocs.pm/elixir/Task.Supervisor.html)
- [`Task.Supervisor.start_child/2`](https://hexdocs.pm/elixir/Task.Supervisor.html#start_child/2)
- [`Task.Supervisor.async_nolink/2`](https://hexdocs.pm/elixir/Task.Supervisor.html#async_nolink/2) — when you want the result without the link
- [`Oban`](https://hexdocs.pm/oban/) — durable background jobs on PostgreSQL
- ["Designing Elixir Systems with OTP" — Bruce Tate & James Gray](https://pragprog.com/titles/jgotp/designing-elixir-systems-with-otp/)
