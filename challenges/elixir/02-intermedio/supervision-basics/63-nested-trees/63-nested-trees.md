# Nested supervision trees — failure isolation by design

**Project**: `nested_trees` — a root supervisor with two independent subtree supervisors; a crash-loop in one subtree must not affect the other.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

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

## Implementation

### Step 1: Create the project

```bash
mix new nested_trees
cd nested_trees
```

### Step 2: `lib/nested_trees/worker.ex`

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

```bash
mix test
```

---

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

## Resources

- [Elixir getting started — Supervisor and Application](https://hexdocs.pm/elixir/supervisor-and-application.html)
- [Erlang OTP Design Principles — Supervision Trees](https://www.erlang.org/doc/design_principles/sup_princ.html)
- ["Designing Elixir Systems with OTP" — Bruce Tate & James Gray (Pragmatic)](https://pragprog.com/titles/jgotp/designing-elixir-systems-with-otp/)
