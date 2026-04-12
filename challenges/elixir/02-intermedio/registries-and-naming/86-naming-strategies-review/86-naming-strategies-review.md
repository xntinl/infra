# Naming strategies review — pids, atoms, via tuples, `:global`, Registry

**Project**: `naming_review` — a capstone exercise that juxtaposes the five naming strategies in one codebase, with a comparative table to internalize the trade-offs.

---

## Project context

You've now seen each naming strategy in isolation. This exercise is the
review: a single mix project that spawns the same `Counter` GenServer five
different ways (unnamed pid, atom, via-Registry, `:global`, and `:pg`
group membership) and exposes the API to manipulate each. The test suite
exercises the distinctive behavior of each strategy — scope, liveness
cleanup, dynamic keys, duplicates — so you can map situations to tools
without hesitation.

Keep this file next to your daily notes; the comparative table at the
bottom is the kind of quick-reference you'll actually consult in PR
reviews.

Project structure:

```
naming_review/
├── lib/
│   ├── naming_review.ex
│   ├── naming_review/application.ex
│   └── naming_review/counter.ex
├── test/
│   └── naming_review_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The five options

| # | Strategy | Address form |
|---|---|---|
| 1 | unnamed pid | `pid` returned by `start_link` |
| 2 | atom name | `:my_server` via `Process.register/2` |
| 3 | via-Registry | `{:via, Registry, {Reg, key}}` |
| 4 | `:global` | `{:global, term}` |
| 5 | `:pg` group (not a name, a membership) | group atom, 1..N pids |

Note that `:pg` is slightly different in kind: it's a set membership,
not a single-pid name, so it fills the "many subscribers" role that
Registry-duplicate fills on one node.

### 2. The three dimensions that matter

When choosing, ask:

- **Scope** — one node or cluster?
- **Cardinality** — one pid per name (unique) or many (duplicate)?
- **Key source** — closed compile-time set, or dynamic / user-derived?

Everything else — performance, ergonomics — is secondary, because those
three dimensions rule out most options up front.

### 3. The decision procedure

```
need a name at all?
├─ no → just pass the pid around
└─ yes → cluster-wide?
         ├─ no → dynamic/large key space?
         │      ├─ yes → Registry (+ via tuple)
         │      └─ no  → atom name
         └─ yes → single pid per name?
                  ├─ yes → :global (or Horde.Registry)
                  └─ no  → :pg group
```

### 4. When names become liabilities

Every named process is a singleton and a coupling point. Two tests
running `async: true` that both register `:cache` will collide. A crash
+ restart window briefly has `:cache` unregistered and callers will
crash with `:noproc`. Default to pids; introduce names only when you
need them for addressing.

---

## Why pids first, and names only when addressing demands it

Naming is a coupling point: every consumer of a name is tied to it, every collision is a cross-test bug, and every restart window briefly has the name unregistered. The five strategies exist to cover distinct *addressing needs*, not to be picked by vibes.

**Pid.** The default. Use it whenever the caller already holds the reference.

**Atom.** Compile-time-known singletons only. Leak-prone with dynamic keys.

**`Registry` + `:via`.** Dynamic local addressing with automatic cleanup. The workhorse for per-entity processes.

**`:global`.** Cluster-wide singletons. Expensive, netsplit-sensitive — reserve for leaders/schedulers.

**`:pg` group.** Cluster-wide *membership*, not single-pid naming. For fan-out and pubsub across nodes.

---

## Design decisions

**Option A — Expose one façade (e.g., always via-Registry) and hide the rest**
- Pros: Simpler API surface; every caller looks the same.
- Cons: Loses the dimensions of the problem (scope, cardinality) that the choice encodes; forces dynamic-local semantics onto cluster-singleton cases.

**Option B — Five sibling helpers, one per strategy, with the same `Counter` subject** (chosen)
- Pros: The comparative table becomes executable; the tests isolate each strategy's distinctive behavior (collision, cleanup, scope, membership); easy to extract the table for PR review.
- Cons: Bigger surface area; without discipline, different concepts end up using different strategies ad hoc.

→ Chose **B** because the goal is a review capstone, not production architecture. In production, wrap each concept behind its own façade.

---

## Implementation

### Step 1: Create the project

```bash
mix new naming_review --sup
cd naming_review
```

### Step 2: `lib/naming_review/counter.ex`

```elixir
defmodule NamingReview.Counter do
  @moduledoc """
  A trivial GenServer used as a subject for each naming strategy.
  """

  use GenServer

  def start_link(opts) do
    case Keyword.get(opts, :name) do
      nil -> GenServer.start_link(__MODULE__, 0)
      name -> GenServer.start_link(__MODULE__, 0, name: name)
    end
  end

  def bump(server), do: GenServer.call(server, :bump)
  def value(server), do: GenServer.call(server, :value)

  @impl true
  def init(n), do: {:ok, n}

  @impl true
  def handle_call(:bump, _from, n), do: {:reply, n + 1, n + 1}
  def handle_call(:value, _from, n), do: {:reply, n, n}
end
```

### Step 3: `lib/naming_review/application.ex`

```elixir
defmodule NamingReview.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Registry, keys: :unique, name: NamingReview.Registry},
      %{id: :review_pg, start: {:pg, :start_link, [NamingReview.Scope]}}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: NamingReview.Supervisor)
  end
end
```

### Step 4: `lib/naming_review.ex`

```elixir
defmodule NamingReview do
  @moduledoc """
  One API, five naming strategies. Each starter returns the "address"
  the caller should use to reach the counter — a pid for the unnamed
  case, an atom or via-tuple for the named cases, and a group atom for
  the `:pg` case (which is membership-based, so it also returns the pid).
  """

  alias NamingReview.Counter

  @registry NamingReview.Registry
  @scope NamingReview.Scope

  # 1. Unnamed pid ─ simplest, most flexible.
  @spec start_unnamed() :: pid()
  def start_unnamed do
    {:ok, pid} = Counter.start_link([])
    pid
  end

  # 2. Atom name ─ singleton, compile-time known.
  @spec start_atom(atom()) :: atom()
  def start_atom(name) when is_atom(name) do
    {:ok, _pid} = Counter.start_link(name: name)
    name
  end

  # 3. Via Registry ─ dynamic, local, auto-cleanup.
  @spec start_via(term()) :: {:via, module(), term()}
  def start_via(key) do
    name = {:via, Registry, {@registry, key}}
    {:ok, _pid} = Counter.start_link(name: name)
    name
  end

  # 4. :global ─ cluster-wide singleton.
  @spec start_global(term()) :: {:global, term()}
  def start_global(name) do
    {:ok, _pid} = Counter.start_link(name: {:global, name})
    {:global, name}
  end

  # 5. :pg group ─ cluster-wide multi-member.
  @spec start_in_group(atom()) :: pid()
  def start_in_group(group) do
    {:ok, pid} = Counter.start_link([])
    :ok = :pg.join(@scope, group, pid)
    pid
  end

  @doc "Send :bump to every pid in `group`, returning their new values."
  @spec bump_group(atom()) :: [integer()]
  def bump_group(group) do
    for pid <- :pg.get_members(@scope, group), do: Counter.bump(pid)
  end
end
```

### Step 5: `test/naming_review_test.exs`

```elixir
defmodule NamingReviewTest do
  use ExUnit.Case, async: false

  alias NamingReview.Counter

  describe "1. unnamed pid" do
    test "address by pid, nobody else can find it" do
      pid = NamingReview.start_unnamed()
      assert Counter.bump(pid) == 1
      # No way to look it up by name — that's the point.
    end
  end

  describe "2. atom name" do
    test "address by atom, double-registration fails" do
      :ok = ensure_stopped(:review_atom)
      NamingReview.start_atom(:review_atom)
      assert Counter.bump(:review_atom) == 1
      # Cannot start a second one under the same atom.
      assert {:error, {:already_started, _}} = Counter.start_link(name: :review_atom)
    end
  end

  describe "3. via Registry" do
    test "dynamic keys, automatic cleanup on death" do
      key = "review-#{System.unique_integer()}"
      name = NamingReview.start_via(key)
      assert Counter.bump(name) == 1

      [{pid, _}] = Registry.lookup(NamingReview.Registry, key)
      ref = Process.monitor(pid)
      GenServer.stop(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}, 500

      wait_until(fn -> Registry.lookup(NamingReview.Registry, key) == [] end)
    end
  end

  describe "4. :global" do
    test "address by arbitrary term, lookup via :global.whereis_name" do
      term = {:review_global, System.unique_integer()}
      name = NamingReview.start_global(term)
      assert Counter.bump(name) == 1
      assert is_pid(:global.whereis_name(term))
    end
  end

  describe "5. :pg group" do
    test "multiple pids in the same group, broadcast via bump_group" do
      group = :"review_group_#{System.unique_integer()}"

      pids = for _ <- 1..3, do: NamingReview.start_in_group(group)
      assert length(:pg.get_members(NamingReview.Scope, group)) == length(pids)

      results = NamingReview.bump_group(group)
      assert Enum.sort(results) == [1, 1, 1]

      Enum.each(pids, &GenServer.stop/1)
    end
  end

  defp ensure_stopped(name) do
    case Process.whereis(name) do
      nil -> :ok
      pid -> GenServer.stop(pid)
    end
  end

  defp wait_until(fun, deadline \\ 500) do
    cond do
      fun.() -> :ok
      deadline <= 0 -> flunk("timeout")
      true -> (Process.sleep(10); wait_until(fun, deadline - 10))
    end
  end
end
```

### Step 6: Run

```bash
mix test
```

### Why this works

Each starter returns the natural *address* for its strategy (pid, atom, via-tuple, `{:global, term}`, or a group atom + pid), and every test exercises exactly the axis where that strategy differs from the others: the atom test asserts on collision, the Registry test on auto-cleanup, the `:global` test on arbitrary-term keys, and the `:pg` test on multi-pid membership with `bump_group/1`. Keeping the subject (`Counter`) constant across strategies is the design choice that makes the comparative table concrete instead of aspirational.

---

## Benchmark

<!-- benchmark N/A: review/capstone exercise — per-strategy benchmarks live in exercises 82, 83, and 85. -->

---

## Comparative table

| Criterion | pid | atom | `:via` + Registry | `:global` | `:pg` group |
|---|---|---|---|---|---|
| Scope | local | local | local | cluster | cluster |
| Cardinality | 1 | 1 | 1 (unique) / N (duplicate) | 1 | N |
| Key type | N/A | atom only | any term | any term | atom (group name) |
| Dynamic keys safe | — | no (atom leak) | yes | yes | yes |
| Auto cleanup on death | N/A | manual | yes | yes | yes |
| Cost per registration | — | O(1) | O(1) | O(cluster) | O(local + async gossip) |
| Netsplit behavior | N/A | N/A | N/A | conflict resolver kicks in | membership diverges, heals on reconnect |
| Typical use | ad-hoc workers | singleton services | per-entity GenServers | cluster singleton | pub/sub, fan-out |

---

## Trade-offs and production gotchas

**1. Default to pids — names are a commitment**
A named process is a shared resource. Every consumer of the name is
coupled to it. Use pids for private collaboration, names only where
external addressing is required.

**2. Atoms are only safe for compile-time keys**
Any keyspace that comes from user input or runtime construction must
not use `Process.register/2`. The atom table fills silently and
eventually the VM dies. `Registry` is the correct choice.

**3. `:global` is slow and netsplit-sensitive**
Use it only for truly cluster-singleton processes (leader election,
schedulers). Understand the conflict resolver — the default
`:random_exit` silently kills one of two colliding pids, which may be
surprising during netsplit recovery.

**4. `:pg` is for membership, not single-pid naming**
Don't force `:pg` to do unique-name work with `get_members |> hd`; that's
a race. Use `:global` or Horde.Registry for cluster-wide unique names.

**5. Registry is local — don't assume it isn't**
The number of production incidents caused by "our Registry somehow
doesn't see the process on the other node" is large. It never did. Read
the docs once; bookmark the scope column in the table above.

**6. When NOT to use any strategy at all**
For one-off workers, Tasks, short-lived computations — no naming. Pass
the pid (or its monitor ref) explicitly. Naming costs complexity and
test-isolation pain; don't pay for it unless you need the address.

---

## Reflection

- Walk through a real feature in your current codebase (e.g., per-user session GenServer, per-order workflow, a single leader scheduler) and map it to the decision tree. Does your current choice survive the three-dimensions test (scope, cardinality, key source)?
- A junior on your team proposes using `:global` for everything "to be safe". In 3 bullets and one concrete failure mode, explain why that's expensive and wrong.

---

## Resources

- [`Registry` — Elixir stdlib](https://hexdocs.pm/elixir/Registry.html)
- [`:global` — Erlang/OTP](https://www.erlang.org/doc/man/global.html)
- [`:pg` — Erlang/OTP 23+](https://www.erlang.org/doc/man/pg.html)
- [`GenServer` — name registration](https://hexdocs.pm/elixir/GenServer.html#module-name-registration)
- [Saša Jurić — "To spawn, or not to spawn?"](https://www.theerlangelist.com/article/spawn_or_not)
