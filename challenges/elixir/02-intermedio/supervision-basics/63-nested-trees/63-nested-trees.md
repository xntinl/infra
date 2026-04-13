# Nested supervision trees — failure isolation by design

**Project**: `nested_trees` — a root supervisor with two independent subtree supervisors; a crash-loop in one subtree must not affect the other.

---

## Project context

Real OTP applications aren't one flat supervisor — they're a **tree**.
Each subtree is a bounded failure domain with its own restart budget,
strategy, and children. Crashes stay inside the subtree where they
occurred unless they're severe enough to blow the subtree's restart
intensity, at which point the subtree dies and its parent decides what
to do.

This exercise builds the minimal two-subtree shape you'll see scaled up
in every production app: a root supervisor whose children are two other
supervisors, each with its own workers. A worker crash-loop in subtree A
must not touch subtree B.

Project structure:

```
nested_trees/
├── lib/
│   ├── nested_trees.ex
│   ├── nested_trees/
│   │   ├── worker.ex
│   │   ├── subtree_a.ex
│   │   ├── subtree_b.ex
│   │   └── root.ex
├── test/
│   └── nested_trees_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a flat supervisor?** Blast radius is the whole app; nesting lets subtrees fail independently.

## Core concepts

### 1. A supervisor is a `:supervisor`-type child

```elixir
children = [
  NestedTrees.SubtreeA,  # type: :supervisor
  NestedTrees.SubtreeB   # type: :supervisor
]
```

When a supervisor is the child of another supervisor, the parent's
strategy applies at the subtree level. `:one_for_one` at the root means
subtree A's death does not restart subtree B.

### 2. Each subtree has its OWN restart intensity

The root's budget counts subtree restarts. Each subtree's budget counts
its own children's restarts. A worker crashing inside subtree A exhausts
A's budget before it affects the root — that's the isolation boundary.

```
    Root (budget: 3/5s)
     │
     ├── Subtree A (budget: 3/5s) ── workers
     └── Subtree B (budget: 3/5s) ── workers
```

Crashing a worker in A many times: A exhausts its budget, A dies. Root
counts that as ONE failure and decides whether to restart A. B is
entirely untouched.

### 3. `:infinity` shutdown is correct for supervisor-type children

When the root shuts down, it must let subtrees propagate shutdown to
their own children before collecting them. That can legitimately take
unbounded time (the subtree's workers may each have their own 5s
shutdowns). Hence the default for supervisor children is
`shutdown: :infinity`.

### 4. Blast radius as an architectural tool

Grouping children by "what fails together should live together" is the
core design principle. If `Repo` dying shouldn't take down `WebEndpoint`,
put them under separate subtrees with a `:one_for_one` root.

---

## Design decisions

**Option A — flat supervisor**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — nested supervision trees (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because blast radius control — a subtree crash shouldn't take down unrelated siblings.


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
mix new nested_trees
cd nested_trees
```

### Step 2: `lib/nested_trees/worker.ex`

**Objective**: Implement `worker.ex` — a worker whose crash behavior is the whole point — it exists so the supervisor strategy can be observed.


```elixir
defmodule NestedTrees.Worker do
  @moduledoc """
  Trivial worker used to demonstrate subtree-level isolation. Registered
  by name so tests can find it.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, :ok, name: name)
  end

  @spec crash(atom()) :: :ok
  def crash(name), do: GenServer.cast(name, :crash)

  @impl true
  def init(:ok), do: {:ok, %{}}

  @impl true
  def handle_cast(:crash, _s), do: raise("boom")
end
```

### Step 3: `lib/nested_trees/subtree_a.ex` and `subtree_b.ex`

**Objective**: Provide `lib/nested_trees/subtree_a.ex` and `subtree_b.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule NestedTrees.SubtreeA do
  @moduledoc """
  Tight restart budget (1/5s) so a single crash exhausts it and we can
  observe the subtree dying — which is the point of this demo.
  """

  use Supervisor

  def start_link(opts), do: Supervisor.start_link(__MODULE__, :ok, Keyword.take(opts, [:name]))

  @impl true
  def init(:ok) do
    children = [{NestedTrees.Worker, [name: :a_worker]}]
    Supervisor.init(children, strategy: :one_for_one, max_restarts: 1, max_seconds: 5)
  end
end

defmodule NestedTrees.SubtreeB do
  @moduledoc "Generous restart budget (10/10s) — B should not be affected by A's failures anyway."

  use Supervisor

  def start_link(opts), do: Supervisor.start_link(__MODULE__, :ok, Keyword.take(opts, [:name]))

  @impl true
  def init(:ok) do
    children = [{NestedTrees.Worker, [name: :b_worker]}]
    Supervisor.init(children, strategy: :one_for_one, max_restarts: 10, max_seconds: 10)
  end
end
```

### Step 4: `lib/nested_trees/root.ex`

**Objective**: Implement `root.ex` — a worker whose crash behavior is the whole point — it exists so the supervisor strategy can be observed.


```elixir
defmodule NestedTrees.Root do
  @moduledoc """
  Root supervisor. :one_for_one at the root means if a subtree dies
  (having exhausted its own intensity), the sibling subtree is untouched.
  """

  use Supervisor

  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, :ok, opts)

  @impl true
  def init(:ok) do
    children = [
      Supervisor.child_spec({NestedTrees.SubtreeA, [name: :subtree_a]}, id: :subtree_a),
      Supervisor.child_spec({NestedTrees.SubtreeB, [name: :subtree_b]}, id: :subtree_b)
    ]

    # Generous root budget so the test can crash SubtreeA multiple times
    # and see it keep coming back.
    Supervisor.init(children, strategy: :one_for_one, max_restarts: 10, max_seconds: 10)
  end
end
```

### Step 5: `test/nested_trees_test.exs`

**Objective**: Write `nested_trees_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule NestedTreesTest do
  use ExUnit.Case, async: false

  setup do
    start_supervised!(NestedTrees.Root)
    :ok
  end

  test "both subtrees boot and host their workers" do
    assert Process.whereis(:subtree_a) |> is_pid()
    assert Process.whereis(:subtree_b) |> is_pid()
    assert Process.whereis(:a_worker) |> is_pid()
    assert Process.whereis(:b_worker) |> is_pid()
  end

  test "crashing a worker in A twice exhausts A's budget; B is untouched" do
    old_a_sup = Process.whereis(:subtree_a)
    old_b_sup = Process.whereis(:subtree_b)
    old_b_worker = Process.whereis(:b_worker)

    # SubtreeA's budget is 1/5s. First crash → worker restarts.
    # Second crash → worker restart exceeds the budget, SubtreeA dies.
    ref_a_sup = Process.monitor(old_a_sup)

    w1 = Process.whereis(:a_worker)
    NestedTrees.Worker.crash(:a_worker)
    # Wait for the first restart to happen.
    wait_until_different_pid(:a_worker, w1)

    w2 = Process.whereis(:a_worker)
    NestedTrees.Worker.crash(:a_worker)

    # SubtreeA exhausts its intensity and dies.
    assert_receive {:DOWN, ^ref_a_sup, :process, ^old_a_sup, _reason}, 1_500

    # Root restarts SubtreeA.
    new_a_sup = wait_until_new_pid(:subtree_a, old_a_sup)
    assert new_a_sup != old_a_sup

    # SubtreeB and its worker were NEVER touched.
    assert Process.whereis(:subtree_b) == old_b_sup
    assert Process.whereis(:b_worker) == old_b_worker

    _ = w2
  end

  defp wait_until_different_pid(name, old, timeout \\ 500) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_wait(name, old, deadline, :different)
  end

  defp wait_until_new_pid(name, old, timeout \\ 1_500) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_wait(name, old, deadline, :new)
  end

  defp do_wait(name, old, deadline, mode) do
    case Process.whereis(name) do
      pid when is_pid(pid) and pid != old -> pid
      _ ->
        if System.monotonic_time(:millisecond) > deadline do
          flunk("#{mode} pid for #{inspect(name)} never appeared")
        else
          Process.sleep(10)
          do_wait(name, old, deadline, mode)
        end
    end
  end
end
```

### Step 6: Run

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

**1. Flat trees look simpler but couple failure domains**
A tree of ten workers under one supervisor shares one restart budget. A
single misbehaving worker can exhaust it and bring down the nine healthy
ones. Nesting into subtrees costs two files and buys you real isolation.

**2. Subtree boundaries should follow the failure domain**
Group things that SHOULD fail together (a connection pool and the
services that share it). Separate things that SHOULDN'T (the web tier
from the background job tier). Architectural diagrams of OTP apps are
mostly drawings of this boundary.

**3. Too many nesting levels = harder to reason about**
Each level adds indirection. 3–4 levels is typical for a mature app.
Beyond that you're probably over-structuring — consider whether some
subtrees are really just workers.

**4. Shutdown propagates top-down, not in parallel**
When the root stops, subtree A is terminated before subtree B even
starts terminating (following the children list order, reversed). Plan
for this in apps with long drains.

**5. When NOT to nest**
Prototype apps or services with a single cohesive responsibility. A
background-job-only service probably has Supervisor → [queue, worker
pool] and that's fine. Premature nesting is architecture theater.

---


## Reflection

- Diseñá el árbol de supervisión para un servicio con DB pool + HTTP clients + job queue. Dibujá las fronteras y justificá cada subtree.

## Resources

- [Elixir getting started — Supervisor and Application](https://hexdocs.pm/elixir/supervisor-and-application.html)
- [Erlang OTP Design Principles — Supervision Trees](https://www.erlang.org/doc/design_principles/sup_princ.html)
- ["Designing Elixir Systems with OTP" — Bruce Tate & James Gray (Pragmatic)](https://pragprog.com/titles/jgotp/designing-elixir-systems-with-otp/)


## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.
