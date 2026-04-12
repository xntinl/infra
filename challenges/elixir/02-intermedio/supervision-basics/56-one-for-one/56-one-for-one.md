# `:one_for_one` — isolate sibling failures

**Project**: `one_for_one_demo` — three independent workers where a crash in one does not disturb the others.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

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

## Core concepts

### 1. "Only the crashed child" — nothing else moves

```
        Supervisor(:one_for_one)
         │
         ├── Counter :a  (value=5)   ← crashes, restarts, value=0
         ├── Counter :b  (value=3)   ← untouched, still value=3
         └── Counter :c  (value=7)   ← untouched, still value=7
```

Compare to `:one_for_all` (exercise 57) where every sibling would also be
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
supervisor itself dies. See exercise 59.

---

## Implementation

### Step 1: Create the project

```bash
mix new one_for_one_demo
cd one_for_one_demo
```

### Step 2: `lib/one_for_one_demo/counter.ex`

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

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Cached pids in siblings become stale on restart**
If sibling B holds A's pid in its state and A restarts, B's pid is now
useless. Avoid this by looking up by name (`Process.whereis/1` or a
`Registry`) at the point of use, not at init time.

**2. Restart intensity is global to the supervisor**
`:one_for_one` doesn't mean "each child has its own restart budget". The
budget is shared. One badly-behaved child can blow the whole tree's
intensity limit. Exercise 59 covers the math.

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
`:one_for_all` (exercise 57) or `:rest_for_one` (exercise 58) instead.

---

## Resources

- [`Supervisor` strategies](https://hexdocs.pm/elixir/Supervisor.html#module-strategies)
- [Erlang `supervisor` — Restart strategies](https://www.erlang.org/doc/man/supervisor.html#restart-strategies)
- [The Little Elixir & OTP Guidebook — Ch. 7 "Supervisors"](https://www.manning.com/books/the-little-elixir-and-otp-guidebook)
