# `:one_for_one` — isolate sibling failures

**Project**: `one_for_one_demo` — three independent workers where a crash in one does not disturb the others.

---

## Project context

`:one_for_one` is the default supervision strategy and, statistically, the
one you'll use 90% of the time. It says: **if a child crashes, restart only
that child — leave the siblings alone**. This is exactly what you want
when children are independent workers (cache shards, connection pools,
background pollers) that don't share mutable state.

The goal of this exercise is to feel the isolation viscerally: start three
workers, crash one, and prove via tests that the other two keep running
with their state intact.

Project structure:

```
one_for_one_demo/
├── lib/
│   ├── one_for_one_demo.ex
│   ├── one_for_one_demo/
│   │   ├── counter.ex
│   │   └── supervisor.ex
├── test/
│   └── one_for_one_demo_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not the other strategies?** Each encodes a different coupling assumption; picking the wrong one either over-restarts or under-restarts.

## Core concepts

### 1. "Only the crashed child" — nothing else moves

```
        Supervisor(:one_for_one)
         │
         ├── Counter :a  (value=5)   ← crashes, restarts, value=0
         ├── Counter :b  (value=3)   ← untouched, still value=3
         └── Counter :c  (value=7)   ← untouched, still value=7
```

Compare to `:one_for_all` where every sibling would also be
restarted and lose state.

### 2. Independent state is the prerequisite

`:one_for_one` is only safe when siblings do **not** share mutable state
through each other's pids. If `:b` stores `:a`'s pid and keeps sending it
messages, when `:a` restarts, `:b` will be holding a stale pid and start
sending messages into a black hole. In that case either use `Registry`
(so `:b` can re-lookup `:a`) or use `:one_for_all`.

### 3. Restart cadence is still bounded

Even with `:one_for_one`, the supervisor tracks `max_restarts`/`max_seconds`
across ALL children. If the *tree as a whole* restarts too many times, the
supervisor itself dies. 

---

## Design decisions

**Option A — `:one_for_all`**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `:one_for_one` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because children are independent; restarting siblings on one failure is unnecessary blast radius.


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
mix new one_for_one_demo
cd one_for_one_demo
```

### Step 2: `lib/one_for_one_demo/counter.ex`

**Objective**: Implement `counter.ex` — a worker whose crash behavior is the whole point — it exists so the supervisor strategy can be observed.


```elixir
defmodule OneForOneDemo.Counter do
  @moduledoc """
  A named counter GenServer used to demonstrate sibling isolation under
  :one_for_one. Crashing one counter must not reset the others.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, 0, name: name)
  end

  @spec bump(atom()) :: :ok
  def bump(name), do: GenServer.cast(name, :bump)

  @spec value(atom()) :: non_neg_integer()
  def value(name), do: GenServer.call(name, :value)

  @spec crash(atom()) :: :ok
  def crash(name), do: GenServer.cast(name, :crash)

  @impl true
  def init(n), do: {:ok, n}

  @impl true
  def handle_cast(:bump, n), do: {:noreply, n + 1}
  def handle_cast(:crash, _n), do: raise("kaboom")

  @impl true
  def handle_call(:value, _from, n), do: {:reply, n, n}
end
```

### Step 3: `lib/one_for_one_demo/supervisor.ex`

**Objective**: Encode the restart policy in `supervisor.ex` — the supervisor strategy is the lesson; the children exist to make it observable.


```elixir
defmodule OneForOneDemo.Supervisor do
  @moduledoc """
  Starts three independent counters under :one_for_one. A crash in any one
  restarts ONLY that counter; the others keep their state.
  """

  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, :ok, opts)

  @impl true
  def init(:ok) do
    children =
      for name <- [:a, :b, :c] do
        Supervisor.child_spec({OneForOneDemo.Counter, [name: name]}, id: name)
      end

    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

### Step 4: `test/one_for_one_demo_test.exs`

**Objective**: Write `one_for_one_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule OneForOneDemoTest do
  use ExUnit.Case, async: false

  alias OneForOneDemo.Counter

  setup do
    start_supervised!(OneForOneDemo.Supervisor)
    :ok
  end

  test "crashing :a resets only :a — :b and :c keep their state" do
    Counter.bump(:a)
    Counter.bump(:b)
    Counter.bump(:b)
    Counter.bump(:c)

    assert Counter.value(:a) == 1
    assert Counter.value(:b) == 2
    assert Counter.value(:c) == 1

    old_a = Process.whereis(:a)
    ref = Process.monitor(old_a)
    Counter.crash(:a)
    assert_receive {:DOWN, ^ref, :process, ^old_a, _}, 500

    # Wait for the supervisor to restart :a.
    new_a = wait_for_new_pid(:a, old_a)
    assert new_a != nil
    assert new_a != old_a

    # :a's state is lost.
    assert Counter.value(:a) == 0
    # :b and :c are untouched.
    assert Counter.value(:b) == 2
    assert Counter.value(:c) == 1
  end

  defp wait_for_new_pid(name, old, timeout \\ 500) do
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



## Key Concepts: Restart Strategy — One-for-One

With `:one_for_one` restart strategy, when a child crashes, only that child is restarted. Other children are unaffected. This is the most common strategy for worker pools: if one worker dies, restart it, but don't touch the others.

When NOT to use it: if multiple children depend on each other's state (e.g., a primary + backup), restarting only one breaks invariants. Use `:one_for_all` instead (restart all) or `:rest_for_one` (restart the failed child and all started after it).


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Cached pids in siblings become stale on restart**
If sibling B holds A's pid in its state and A restarts, B's pid is now
useless. Avoid this by looking up by name (`Process.whereis/1` or a
`Registry`) at the point of use, not at init time.

**2. Restart intensity is global to the supervisor**
`:one_for_one` doesn't mean "each child has its own restart budget". The
budget is shared. One badly-behaved child can blow the whole tree's
intensity limit.

**3. Independent ≠ stateless**
A counter has state; that's fine. What matters is that the state has no
*semantic* link to the siblings. A balance sheet that must equal the sum
of three account processes IS linked — don't use `:one_for_one` there.

**4. Restart is not free**
Each restart walks through `terminate/2`, link cleanup, new `init/1`. Under
heavy load, a flapping child on `:one_for_one` is cheap per-restart but
adds up. If you see hundreds per minute, look for the cause, not a bigger
budget.

**5. When NOT to use `:one_for_one`**
When siblings depend on each other: shared in-memory state, pipeline
stages, or a parent-built lookup table that references pids. Use
`:one_for_all` or `:rest_for_one` instead.

---


## Reflection

- Dá un caso donde `:one_for_one` parece correcto pero en realidad estás escondiendo un acoplamiento que debería romperse.

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule OneForOneDemo.Counter do
  @moduledoc """
  A named counter GenServer used to demonstrate sibling isolation under
  :one_for_one. Crashing one counter must not reset the others.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, 0, name: name)
  end

  @spec bump(atom()) :: :ok
  def bump(name), do: GenServer.cast(name, :bump)

  @spec value(atom()) :: non_neg_integer()
  def value(name), do: GenServer.call(name, :value)

  @spec crash(atom()) :: :ok
  def crash(name), do: GenServer.cast(name, :crash)

  @impl true
  def init(n), do: {:ok, n}

  @impl true
  def handle_cast(:bump, n), do: {:noreply, n + 1}
  def handle_cast(:crash, _n), do: raise("kaboom")

  @impl true
  def handle_call(:value, _from, n), do: {:reply, n, n}
end

defmodule OneForOneDemo.Supervisor do
  @moduledoc """
  Static supervisor with three independent counters.
  Uses `:one_for_one` strategy: only the crashed child is restarted.
  """

  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, :ok, opts)
  end

  @impl true
  def init(:ok) do
    children = [
      Supervisor.child_spec({OneForOneDemo.Counter, [name: :a]}, id: :a),
      Supervisor.child_spec({OneForOneDemo.Counter, [name: :b]}, id: :b),
      Supervisor.child_spec({OneForOneDemo.Counter, [name: :c]}, id: :c)
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end

# Demonstrate :one_for_one strategy
IO.puts("=== OneForOne Strategy Demo ===")

{:ok, _sup} = OneForOneDemo.Supervisor.start_link()

# Bump counters
OneForOneDemo.Counter.bump(:a)
OneForOneDemo.Counter.bump(:a)
OneForOneDemo.Counter.bump(:b)
OneForOneDemo.Counter.bump(:b)
OneForOneDemo.Counter.bump(:c)

assert OneForOneDemo.Counter.value(:a) == 2
assert OneForOneDemo.Counter.value(:b) == 2
assert OneForOneDemo.Counter.value(:c) == 1

IO.puts("Initial values: a=2, b=2, c=1")

# Crash :a — siblings should be unaffected
old_a = Process.whereis(:a)
ref = Process.monitor(old_a)
OneForOneDemo.Counter.crash(:a)
assert_receive {:DOWN, ^ref, :process, ^old_a, _}, 500

# Wait for restart
Process.sleep(50)
assert OneForOneDemo.Counter.value(:a) == 0
assert OneForOneDemo.Counter.value(:b) == 2
assert OneForOneDemo.Counter.value(:c) == 1

IO.puts("After crashing :a:")
IO.puts("  :a restarted (state reset to 0)")
IO.puts("  :b unaffected (still 2)")
IO.puts("  :c unaffected (still 1)")
IO.puts("All :one_for_one assertions passed!")
```


## Resources

- [`Supervisor` strategies](https://hexdocs.pm/elixir/Supervisor.html#module-strategies)
- [Erlang `supervisor` — Restart strategies](https://www.erlang.org/doc/man/supervisor.html#restart-strategies)
- [The Little Elixir & OTP Guidebook — Ch. 7 "Supervisors"](https://www.manning.com/books/the-little-elixir-and-otp-guidebook)


## Advanced Considerations

Supervision trees encode your application's fault tolerance strategy. The tree structure, restart policy, and shutdown semantics directly determine behavior during crashes, dependencies, and graceful shutdown.

**Supervision tree design:**
A well-designed tree mirrors data/message flow: dependencies point upward. If process A depends on process B, B should be higher in the tree (started first, shut down last). Supervisor strategies (`:one_for_one`, `:one_for_all`, `:rest_for_one`) define the scope of cascading restarts. `:one_for_one` isolates failures (each crash restarts only that child); `:one_for_all` is for tightly-coupled groups (e.g., a reader-writer pair).

**Restart strategies and intensity:**
`max_restarts: 3, max_seconds: 5` means "if 3+ restarts occur in 5 seconds, kill the supervisor." This circuit-breaker pattern prevents restart loops that consume resources. The key decision: should a crashing child take down the whole app (escalate to parent) or just itself? Transient/temporary children exit "cleanly" and don't trigger restarts — useful for request handlers.

**Error propagation and shutdown ordering:**
When a supervisor exits, it sends `:shutdown` to children in reverse start order (LIFO). Children have `shutdown: 5000` milliseconds to terminate gracefully before hard killing. Nested supervisors propagate this signal recursively. Understanding this order prevents resource leaks: a child waiting on another child's graceful shutdown will deadlock if not designed carefully.
