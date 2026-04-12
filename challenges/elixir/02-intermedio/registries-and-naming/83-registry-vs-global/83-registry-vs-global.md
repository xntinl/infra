# Registry vs :global vs {:via, Registry} — choosing a name backend

**Project**: `naming_compared` — the same GenServer registered three ways, with a test suite highlighting the differences.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You're reviewing a teammate's PR. They used `:global` to name a cache
process "because names should be global". The cache is one-per-node and
never accessed across nodes. That's not just overkill — it's *wrong*,
because `:global` runs a cluster-wide consensus every time a name is
registered, and it blocks during netsplit resolution.

This exercise stands up one GenServer and registers it three ways:
`Process.register/2` with an atom, `{:global, ...}`, and
`{:via, Registry, ...}`. Tests exercise lookup, naming collisions, and
automatic cleanup, so you can see exactly what each backend does and
doesn't give you.

Project structure:

```
naming_compared/
├── lib/
│   ├── naming_compared.ex
│   ├── naming_compared/application.ex
│   └── naming_compared/counter.ex
├── test/
│   └── naming_compared_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Atom names — `Process.register/2`

Fastest, simplest, local. The registered name *is* an atom in the VM's
atom table. Two hard constraints: atoms are never garbage-collected (so
names must come from a closed set) and registration is node-local
(`Node.spawn/2` to a remote node cannot look them up).

Good for: a known, bounded set of long-lived processes. The repo, the
cache, the scheduler.

### 2. `:global` — cluster-wide consensus

`:global` maintains a distributed name table synchronized across all
connected nodes via a dedicated resolver process. Registering a name runs
a global lock (`:global.trans/2`) that pauses other registrations until
the name is committed. It resolves name conflicts after netsplits using
a pluggable resolver (`:random_exit`, `:notify_all`, or custom).

Good for: a genuinely *singleton* process across a cluster — one job
scheduler, one leader. Expensive (every registration is a multi-node
round-trip) and liable to weirdness during netsplit.

### 3. `Registry` + `:via` — local, atom-free, dynamic

`Registry` is node-local like atoms, but accepts arbitrary terms as keys
and cleans up automatically on process exit. Via tuples give you
transparent `GenServer.call(name, ...)` without holding the pid. No
cluster involvement, no atom leak, very fast.

Good for: anything dynamic and local — per-session processes, per-room
chat servers, per-entity workers.

### 4. The decision table (preview)

| Criterion | atom `register` | `:global` | `Registry + :via` |
|---|---|---|---|
| Scope | local | cluster | local |
| Dynamic keys | atom leak | ok | ok |
| Registration cost | O(1) | O(cluster) | O(1) |
| Cleanup on death | manual unregister | automatic | automatic |
| Works with `{:via, ...}` | no (use `name:`) | yes | yes |
| Netsplit behavior | N/A | conflict resolver runs | N/A |

---

## Implementation

### Step 1: Create the project

```bash
mix new naming_compared --sup
cd naming_compared
```

### Step 2: `lib/naming_compared/counter.ex`

```elixir
defmodule NamingCompared.Counter do
  @moduledoc """
  A tiny GenServer used as a test subject for three naming backends. It
  exposes `bump/1` and `value/1` — all that matters is how you address it.
  """

  use GenServer

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, 0, name: name)
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

### Step 3: `lib/naming_compared/application.ex`

```elixir
defmodule NamingCompared.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Registry, keys: :unique, name: NamingCompared.Registry}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: NamingCompared.Supervisor)
  end
end
```

### Step 4: `lib/naming_compared.ex`

```elixir
defmodule NamingCompared do
  @moduledoc """
  Helpers to start the same counter via three naming strategies.
  """

  alias NamingCompared.Counter

  @doc "Atom name — `Process.register/2` under the hood."
  def start_atom(name) when is_atom(name) do
    Counter.start_link(name: name)
  end

  @doc ":global name — cluster-wide synchronized table."
  def start_global(name) do
    Counter.start_link(name: {:global, name})
  end

  @doc "Registry + via tuple — local, dynamic, atom-safe."
  def start_via(key) do
    Counter.start_link(name: {:via, Registry, {NamingCompared.Registry, key}})
  end
end
```

### Step 5: `test/naming_compared_test.exs`

```elixir
defmodule NamingComparedTest do
  use ExUnit.Case, async: false

  alias NamingCompared.Counter

  describe "atom name (Process.register/2)" do
    test "address the server by bare atom" do
      {:ok, _} = NamingCompared.start_atom(:counter_a)
      assert Counter.bump(:counter_a) == 1
      assert Counter.value(:counter_a) == 1
    end

    test "double-registration fails at start_link" do
      {:ok, _} = NamingCompared.start_atom(:counter_dup)
      assert {:error, {:already_started, _}} = NamingCompared.start_atom(:counter_dup)
    end
  end

  describe ":global name" do
    test "address the server via {:global, term}" do
      {:ok, _} = NamingCompared.start_global({:counter, :g})
      assert Counter.bump({:global, {:counter, :g}}) == 1
      # `:global.whereis_name/1` is the underlying resolver.
      assert is_pid(:global.whereis_name({:counter, :g}))
    end

    test "can use any term — not restricted to atoms" do
      {:ok, _} = NamingCompared.start_global("string-name")
      assert Counter.value({:global, "string-name"}) == 0
    end
  end

  describe "Registry + via tuple" do
    test "address the server via {:via, Registry, {reg, key}}" do
      key = "dyn-#{System.unique_integer()}"
      {:ok, _} = NamingCompared.start_via(key)
      via = {:via, Registry, {NamingCompared.Registry, key}}
      assert Counter.bump(via) == 1
    end

    test "entry disappears when process dies (automatic cleanup)" do
      key = "vanish-#{System.unique_integer()}"
      {:ok, pid} = NamingCompared.start_via(key)
      ref = Process.monitor(pid)

      GenServer.stop(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}, 500

      wait_until(fn ->
        Registry.lookup(NamingCompared.Registry, key) == []
      end)
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

---

## Trade-offs and production gotchas

**1. `:global` is expensive and not as "safe" as it sounds**
Every registration acquires a cluster-wide lock. In a healthy cluster
that's milliseconds; during a netsplit it's until the split heals. A
name conflict after a heal is resolved by killing one of the two pids
(`:random_exit` is the default resolver) — effectively, silent data loss
if you weren't expecting it. Only use `:global` for truly cluster-singleton
processes and document the conflict resolver explicitly.

**2. Atom names are a memory leak when keys are user-provided**
`:room_#{user_input}` → new atom every time → atom table fills → VM
crashes. If your key isn't from a closed set of compile-time atoms, use
`Registry` instead.

**3. Registry does not cross nodes**
It's a single-node naming store. `Node.spawn/2` and `GenServer.call` with
a via tuple to a remote node's registry will not find the process. For
cluster-wide dynamic naming, reach for Horde or Syn.

**4. Mixing backends for the same concept breeds bugs**
Don't register the same logical entity under both `:global` and a local
registry "to cover both cases". You'll spend a day debugging a split-brain
of your own making. Pick one backend per concept and wrap it in a façade.

**5. Dead entries linger momentarily in Registry**
Cleanup happens on the registry's monitor handler — async. In the
micro-window between death and cleanup, `lookup/2` can return a dead pid.
Callers must handle `:noproc` from `GenServer.call` gracefully.

**6. When NOT to use any of these**
For ad-hoc, short-lived workers passed by pid (Task, Flow), don't
register at all. Naming has cost; naming things you don't look up by name
is pointless.

---

## Resources

- [`:global` — Erlang/OTP docs](https://www.erlang.org/doc/man/global.html)
- [`Registry` — Elixir stdlib](https://hexdocs.pm/elixir/Registry.html)
- [`GenServer` — name registration](https://hexdocs.pm/elixir/GenServer.html#module-name-registration)
- [Horde — distributed supervisor + registry](https://hexdocs.pm/horde/readme.html)
- [Saša Jurić — "To spawn, or not to spawn?"](https://www.theerlangelist.com/article/spawn_or_not) — when naming makes sense
