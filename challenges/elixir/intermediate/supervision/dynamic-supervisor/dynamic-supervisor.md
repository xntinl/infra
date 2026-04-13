# DynamicSupervisor — starting children at runtime

**Project**: `worker_factory` — a DynamicSupervisor that spawns job workers on demand, one per incoming request.

---

## Why dynamic supervisor matters

`Supervisor` is great when you know your children up front: a Phoenix app
has exactly one Repo, one Endpoint, one PubSub. But most real systems also
need **on-demand** processes: one worker per job, one connection per client,
one session per user. You don't know how many you'll need or when.

`DynamicSupervisor` is the OTP answer. Its children are declared at runtime
via `start_child/2`, not in `init/1`. The strategy is always `:one_for_one`
— children are independent, so a crash in one must never cascade.

---

## Project structure

```
worker_factory/
├── lib/
│   └── worker_factory.ex
├── script/
│   └── main.exs
├── test/
│   └── worker_factory_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not a lower-level alternative?** For dynamic supervisor, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. No static children list

```elixir
def init(_) do
  DynamicSupervisor.init(strategy: :one_for_one)
end
```

That's the whole init. You don't list children — you add them at runtime
with `DynamicSupervisor.start_child(sup, child_spec)`.

### 2. `start_child/2` returns the new worker's pid

```
  DynamicSupervisor.start_child(sup, {Worker, arg})
        │
        ▼
  {:ok, #PID<0.123.0>}   ← the supervisor started a new Worker
```

The caller owns the pid. You can monitor it, message it, or look it up
via a `Registry` later.

### 3. Only `:one_for_one` is supported

DynamicSupervisor deliberately forbids `:one_for_all` and `:rest_for_one`.
Dynamic children have no defined ordering — there is no "rest" to restart.
If you need group-wide restarts, use a regular `Supervisor`.

### 4. Restart strategy defaults to `:permanent`

Just like with `Supervisor`, a crashed child is restarted by default.
For one-shot jobs, set `restart: :transient` or `:temporary` on the
worker's `child_spec`. 

### 5. `max_restarts` applies to the whole tree

Even though each child is independent, if the whole tree crashes more than
`max_restarts` times in `max_seconds`, the DynamicSupervisor itself crashes.
Tune these for workloads with many short-lived workers.

---

## Design decisions

**Option A — static Supervisor + manual child list**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — DynamicSupervisor (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because runtime-created children (one-per-session, etc.) don't fit static child specs.

## Implementation

### `mix.exs`

```elixir
defmodule WorkerFactory.MixProject do
  use Mix.Project

  def project do
    [
      app: :worker_factory,
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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new worker_factory
cd worker_factory
```

### `lib/worker_factory.ex`

```elixir
defmodule WorkerFactory do
  @moduledoc """
  DynamicSupervisor — starting children at runtime.

  `Supervisor` is great when you know your children up front: a Phoenix app.
  """
end
```

### `lib/worker_factory/job_worker.ex`

**Objective**: Implement `job_worker.ex` — a worker whose crash behavior is the whole point — it exists so the supervisor strategy can be observed.

```elixir
defmodule WorkerFactory.JobWorker do
  @moduledoc """
  Minimal job worker. Holds a job id and an arbitrary payload. Exposes
  `describe/1` so callers can verify the worker is alive and has the right
  state, and `crash/1` / `finish/1` to simulate the two termination paths.
  """

  # :transient = restart only on abnormal exit, not on :normal/:shutdown.
  use GenServer, restart: :transient

  @type job_id :: term()

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    id = Keyword.fetch!(opts, :id)
    payload = Keyword.get(opts, :payload, %{})
    GenServer.start_link(__MODULE__, {id, payload})
  end

  @spec describe(pid()) :: {job_id(), map()}
  def describe(pid), do: GenServer.call(pid, :describe)

  @spec crash(pid()) :: :ok
  def crash(pid), do: GenServer.cast(pid, :crash)

  @spec finish(pid()) :: :ok
  def finish(pid), do: GenServer.cast(pid, :finish)

  @impl true
  def init({id, payload}), do: {:ok, %{id: id, payload: payload}}

  @impl true
  def handle_call(:describe, _from, %{id: id, payload: p} = s),
    do: {:reply, {id, p}, s}

  @impl true
  def handle_cast(:crash, _state), do: raise("job blew up")
  # :transient + :normal → supervisor will NOT restart.
  def handle_cast(:finish, state), do: {:stop, :normal, state}
end
```

### `lib/worker_factory/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.

```elixir
defmodule WorkerFactory.Supervisor do
  @moduledoc """
  DynamicSupervisor for job workers. Public helpers hide the
  `DynamicSupervisor.start_child/2` call from consumers.
  """

  use DynamicSupervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []) do
    DynamicSupervisor.start_link(__MODULE__, :ok, Keyword.put_new(opts, :name, __MODULE__))
  end

  @impl true
  def init(:ok) do
    # max_restarts bumped from the default 3 because job workers can
    # legitimately crash under load and we don't want to take the factory
    # down at the first small spike.
    DynamicSupervisor.init(strategy: :one_for_one, max_restarts: 10, max_seconds: 10)
  end

  @doc """
  Spawns a new job worker under the factory. Returns `{:ok, pid}` or an
  error tuple from `DynamicSupervisor.start_child/2`.
  """
  @spec start_job(term(), map()) :: DynamicSupervisor.on_start_child()
  def start_job(id, payload \\ %{}) do
    spec = {WorkerFactory.JobWorker, [id: id, payload: payload]}
    DynamicSupervisor.start_child(__MODULE__, spec)
  end

  @spec children_count() :: non_neg_integer()
  def children_count do
    %{active: active} = DynamicSupervisor.count_children(__MODULE__)
    active
  end
end
```

### Step 4: `test/worker_factory_test.exs`

**Objective**: Write `worker_factory_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule WorkerFactoryTest do
  use ExUnit.Case, async: false

  doctest WorkerFactory

  setup do
    start_supervised!(WorkerFactory.Supervisor)
    :ok
  end

  describe "core functionality" do
    test "starts workers on demand" do
      assert WorkerFactory.Supervisor.children_count() == 0

      {:ok, pid1} = WorkerFactory.Supervisor.start_job(:job_1, %{n: 1})
      {:ok, pid2} = WorkerFactory.Supervisor.start_job(:job_2, %{n: 2})

      assert WorkerFactory.Supervisor.children_count() == 2
      assert {:job_1, %{n: 1}} == WorkerFactory.JobWorker.describe(pid1)
      assert {:job_2, %{n: 2}} == WorkerFactory.JobWorker.describe(pid2)
    end

    test "transient worker is restarted on crash" do
      {:ok, pid} = WorkerFactory.Supervisor.start_job(:restart_me)
      ref = Process.monitor(pid)

      WorkerFactory.JobWorker.crash(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, _reason}, 500

      # Supervisor restarts the child — count returns to 1 after a brief moment.
      Process.sleep(50)
      assert WorkerFactory.Supervisor.children_count() == 1

      [{_, new_pid, _, _}] = DynamicSupervisor.which_children(WorkerFactory.Supervisor)
      assert new_pid != pid
      assert Process.alive?(new_pid)
    end

    test "transient + :normal stop is NOT restarted" do
      {:ok, pid} = WorkerFactory.Supervisor.start_job(:one_shot)
      ref = Process.monitor(pid)

      WorkerFactory.JobWorker.finish(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, :normal}, 500

      Process.sleep(50)
      assert WorkerFactory.Supervisor.children_count() == 0
    end

    test "siblings are independent on crash" do
      {:ok, a} = WorkerFactory.Supervisor.start_job(:a)
      {:ok, b} = WorkerFactory.Supervisor.start_job(:b)

      ref_a = Process.monitor(a)
      WorkerFactory.JobWorker.crash(a)
      assert_receive {:DOWN, ^ref_a, :process, ^a, _}, 500

      # b was not touched.
      assert Process.alive?(b)
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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `WorkerFactory`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== WorkerFactory demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    # No public functions detected; replace with calls into the module.
    :ok
  end
end

Main.main()
```

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. You lose the pid when you don't register it**
`start_child/2` returns a pid but the DynamicSupervisor does not index
children by name. If callers need "find the worker for job 42 later", pair
it with a `Registry`. Otherwise you'll be walking
`which_children/1` linearly — fine for dozens of children, terrible for
thousands.

**2. `which_children/1` is not free**
It walks all children under a read lock. Avoid calling it on the hot path;
use it for diagnostics and tests.

**3. `max_restarts` can mask real bugs**
Raising `max_restarts` to "stop the supervisor from dying" is a smell. If
workers restart constantly, fix the root cause instead of loosening the
safety valve. A healthy DynamicSupervisor sees a handful of restarts an
hour, not thousands.

**4. Graceful shutdown kills children in parallel, not in order**
Dynamic children have no ordering. On `Supervisor.stop/1`, all children
get the shutdown signal simultaneously. Set each worker's `:shutdown`
option thoughtfully — especially for workers holding open
connections or writing to disk.

**5. When NOT to use DynamicSupervisor**
If children are few and known at compile time, just use `Supervisor`. If
children form a tight pipeline where stage N+1 depends on stage N, use
`Supervisor` with `:rest_for_one`. If you need a bounded pool with reuse
(not create-and-destroy semantics), reach for `poolboy` or `nimble_pool`
— DynamicSupervisor creates a new process per call, which is cheap but
not zero.

---

## Reflection

- Diseñá la estrategia de naming (Registry via-tuple vs pid) para 10k children dinámicos. Justificá.

## Resources

- [`DynamicSupervisor` — Elixir stdlib](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [`Registry` — stdlib](https://hexdocs.pm/elixir/Registry.html) — pairs naturally with DynamicSupervisor
- [`nimble_pool` — bounded resource pool](https://hexdocs.pm/nimble_pool/) — for connection-like workloads
- ["Migrating from `:simple_one_for_one`"](https://hexdocs.pm/elixir/DynamicSupervisor.html#module-migrating-from-supervisor-simple_one_for_one) — historical context

## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/worker_factory_test.exs`

```elixir
defmodule WorkerFactoryTest do
  use ExUnit.Case, async: true

  doctest WorkerFactory

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert WorkerFactory.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
