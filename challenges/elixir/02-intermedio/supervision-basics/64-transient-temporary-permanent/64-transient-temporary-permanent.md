# `:permanent`, `:transient`, `:temporary` — picking the right restart policy

**Project**: `restart_strategies_demo` — three workers, one of each restart strategy, demonstrating exactly when each triggers a restart.

---

## Project context

A child spec's `:restart` field has three values, and they encode
genuinely different lifecycle contracts:

- `:permanent` — "this child should always be running; restart on any exit"
- `:transient` — "restart only if it crashed; normal/shutdown exits are fine"
- `:temporary` — "never restart; when it's gone, it's gone"

The defaults (`:permanent` for `use GenServer`) are usually right, but
the other two are essential for one-shot jobs, dynamic workers, and
cleanup tasks. Picking the wrong one either wastes work (restarting a
job that finished correctly) or silently drops failures (never
restarting a permanently-important worker).

Project structure:

```
restart_strategies_demo/
├── lib/
│   ├── restart_strategies_demo.ex
│   ├── restart_strategies_demo/
│   │   ├── worker.ex
│   │   └── supervisor.ex
├── test/
│   └── restart_strategies_demo_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not `:permanent` for all?** One-shot jobs shouldn't restart forever on failure; user sessions should restart only on abnormal exits.

## Core concepts

### 1. The decision matrix

```
                          Exit reason
                  :normal     :shutdown   abnormal (raise, :kill, etc.)
  :permanent   →  RESTART     RESTART     RESTART
  :transient   →  no restart  no restart  RESTART
  :temporary   →  no restart  no restart  no restart
```

`:permanent` ignores the exit reason. `:transient` only restarts on
abnormal exits. `:temporary` never restarts, period.

### 2. `:permanent` — the right default for long-running services

Things you expect to be up for the life of the VM: database pools,
PubSub, the Phoenix endpoint, caches. If it exits at all, something is
wrong and restart is the right action.

### 3. `:transient` — the right choice for one-shot jobs with possible failure

A worker that processes a single job and returns `:normal` should be
`:transient`: if it finishes normally, we don't want it restarted (the
job is done). If it crashes, we DO want a restart so the job gets
another attempt.

### 4. `:temporary` — fire-and-forget, no restart ever

Best for workers whose failure is logged elsewhere (via Task + supervisor
tracking, Oban jobs) and where a second attempt would do more harm than
good. Also a fit for legitimately-ephemeral helpers.

### 5. `:temporary` children are NOT counted in shutdown counts

Neither are they "remembered" — once a temporary child exits, the
supervisor drops all references. You can't query it via
`which_children/1` afterward.

---

## Design decisions

**Option A — `:permanent` for everything**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — restart type chosen per child role (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because one-shot jobs should be `:temporary`; workers `:permanent`; user sessions often `:transient`.


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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new restart_strategies_demo
cd restart_strategies_demo
```

### Step 2: `lib/restart_strategies_demo/worker.ex`

**Objective**: Implement `worker.ex` — a worker whose crash behavior is the whole point — it exists so the supervisor strategy can be observed.


```elixir
defmodule RestartStrategiesDemo.Worker do
  @moduledoc """
  Minimal worker that can exit via three paths:
   * `finish/1` → GenServer.stop with :normal
   * `crash/1`  → raises (abnormal exit)
   * `shut/1`   → GenServer.stop with :shutdown

  The restart strategy is set by the spec in Supervisor, not the worker
  itself, so we can mount the same module with three different policies.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, :ok, name: name)
  end

  @spec finish(atom()) :: :ok
  def finish(name), do: GenServer.stop(name, :normal)

  @spec crash(atom()) :: :ok
  def crash(name), do: GenServer.cast(name, :crash)

  @spec shut(atom()) :: :ok
  def shut(name), do: GenServer.stop(name, :shutdown)

  @impl true
  def init(:ok), do: {:ok, %{}}

  @impl true
  def handle_cast(:crash, _s), do: raise("boom")
end
```

### Step 3: `lib/restart_strategies_demo/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.


```elixir
defmodule RestartStrategiesDemo.Supervisor do
  @moduledoc """
  Mounts three instances of Worker with contrasting restart strategies.
  """

  use Supervisor

  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, :ok, opts)

  @impl true
  def init(:ok) do
    children = [
      Supervisor.child_spec({RestartStrategiesDemo.Worker, [name: :perm]}, id: :perm, restart: :permanent),
      Supervisor.child_spec({RestartStrategiesDemo.Worker, [name: :trans]}, id: :trans, restart: :transient),
      Supervisor.child_spec({RestartStrategiesDemo.Worker, [name: :temp]}, id: :temp, restart: :temporary)
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

### Step 4: `test/restart_strategies_demo_test.exs`

**Objective**: Write `restart_strategies_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule RestartStrategiesDemoTest do
  use ExUnit.Case, async: false

  alias RestartStrategiesDemo.Worker

  setup do
    start_supervised!(RestartStrategiesDemo.Supervisor)
    :ok
  end

  describe ":permanent" do
    test "restarts on normal exit" do
      old = Process.whereis(:perm)
      ref = Process.monitor(old)
      Worker.finish(:perm)
      assert_receive {:DOWN, ^ref, :process, ^old, :normal}, 500

      new = wait_until_new_pid(:perm, old)
      assert new != nil and new != old
    end

    test "restarts on abnormal exit" do
      old = Process.whereis(:perm)
      ref = Process.monitor(old)
      Worker.crash(:perm)
      assert_receive {:DOWN, ^ref, :process, ^old, _}, 500

      new = wait_until_new_pid(:perm, old)
      assert new != nil and new != old
    end
  end

  describe ":transient" do
    test "does NOT restart on normal exit" do
      old = Process.whereis(:trans)
      ref = Process.monitor(old)
      Worker.finish(:trans)
      assert_receive {:DOWN, ^ref, :process, ^old, :normal}, 500

      Process.sleep(100)
      assert Process.whereis(:trans) == nil
    end

    test "DOES restart on abnormal exit" do
      old = Process.whereis(:trans)
      ref = Process.monitor(old)
      Worker.crash(:trans)
      assert_receive {:DOWN, ^ref, :process, ^old, _}, 500

      new = wait_until_new_pid(:trans, old)
      assert new != nil and new != old
    end

    test "does NOT restart on :shutdown" do
      old = Process.whereis(:trans)
      # Restart so we have a fresh instance (previous test may have killed it).
      _ = start_supervised({Task, fn -> :ok end})
      _ = old

      # Use the current live pid.
      pid = Process.whereis(:trans) || old
      ref = Process.monitor(pid)
      Worker.shut(:trans)
      assert_receive {:DOWN, ^ref, :process, ^pid, :shutdown}, 500

      Process.sleep(100)
      assert Process.whereis(:trans) == nil
    end
  end

  describe ":temporary" do
    test "does NOT restart on ANY exit" do
      old = Process.whereis(:temp)
      ref = Process.monitor(old)
      Worker.crash(:temp)
      assert_receive {:DOWN, ^ref, :process, ^old, _}, 500

      Process.sleep(100)
      assert Process.whereis(:temp) == nil
    end
  end

  defp wait_until_new_pid(name, old, timeout \\ 500) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_wait(name, old, deadline)
  end

  defp do_wait(name, old, deadline) do
    case Process.whereis(name) do
      pid when is_pid(pid) and pid != old -> pid
      _ ->
        if System.monotonic_time(:millisecond) > deadline do
          nil
        else
          Process.sleep(10)
          do_wait(name, old, deadline)
        end
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `:permanent` + `stop(:normal)` = restart loop you didn't ask for**
If your "clean" shutdown path calls `GenServer.stop(pid, :normal)` and
the child is `:permanent`, the supervisor will faithfully restart it.
Either use `:transient`, or stop the supervisor instead of the child.

**2. `:transient` matches a common mental model best**
"Clean exits stay clean; crashes get another try" is what most people
intuit when they hear "restart". Consider it the right default for
worker-per-job designs.

**3. `:temporary` children are invisible after exit**
They don't appear in `which_children/1` once gone. Don't rely on the
supervisor for audit — log the outcome before exiting, or use a parent
that tracks monitors.

**4. Restart strategy is PER child spec, not per module**
Two instances of the same module can have different strategies. That's
the point of custom `child_spec/1` overrides.

**5. When NOT to hand-pick — let the default stand**
For a standard long-running service, `:permanent` is correct. Don't
change it unless you have a concrete reason. The two other values are
tools for specific situations, not expressions of taste.

---


## Reflection

- Un worker reintenta un job y si falla 3 veces, no debe reiniciar. ¿Qué restart type usás y cómo implementás el límite?

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule RestartStrategiesDemo.Worker do
    @moduledoc """
    Minimal worker that can exit via three paths:
     * `finish/1` → GenServer.stop with :normal
     * `crash/1`  → raises (abnormal exit)
     * `shut/1`   → GenServer.stop with :shutdown

    The restart strategy is set by the spec in Supervisor, not the worker
    itself, so we can mount the same module with three different policies.
    """

    use GenServer

    @spec start_link(keyword()) :: GenServer.on_start()
    def start_link(opts) do
      name = Keyword.fetch!(opts, :name)
      GenServer.start_link(__MODULE__, :ok, name: name)
    end

    @spec finish(atom()) :: :ok
    def finish(name), do: GenServer.stop(name, :normal)

    @spec crash(atom()) :: :ok
    def crash(name), do: GenServer.cast(name, :crash)

    @spec shut(atom()) :: :ok
    def shut(name), do: GenServer.stop(name, :shutdown)

    @impl true
    def init(:ok), do: {:ok, %{}}

    @impl true
    def handle_cast(:crash, _s), do: raise("boom")
  end

  def main do
    IO.puts("RestartStrategiesDemo OK")
  end

end

Main.main()
```


## Resources

- [`Supervisor` — restart values](https://hexdocs.pm/elixir/Supervisor.html#module-restart-values-restart)
- [Erlang `supervisor` — restart type](https://www.erlang.org/doc/man/supervisor.html#type-restart)
- ["The ABCs of OTP" — Justin Schneck](https://www.youtube.com/watch?v=8mXqxBBvNdk)


## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.
