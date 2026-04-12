# `{:via, Module, term}` patterns for GenServer naming

**Project**: `via_tuple_patterns` — common via-tuple usages: Registry via, custom `:via` module, and tagged names.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You've seen `{:via, Registry, {MyReg, key}}` in a dozen codebases but
never written one yourself, and you've never noticed that `:via` is an
open protocol — any module exporting four functions can back a name.
This exercise wires up two concrete patterns you'll meet in the wild:

1. The everyday case: `{:via, Registry, ...}` for dynamic per-key GenServers.
2. The educational case: a handwritten `:via` module that wraps ETS directly,
   so you see exactly what the protocol is.

If you work with libraries like Horde, Syn, or custom routers, you're already
consuming the `:via` protocol. Writing one yourself once demystifies it for
good.

Project structure:

```
via_tuple_patterns/
├── lib/
│   ├── via_tuple_patterns.ex
│   ├── via_tuple_patterns/application.ex
│   ├── via_tuple_patterns/worker.ex
│   └── via_tuple_patterns/ets_via.ex
├── test/
│   └── via_tuple_patterns_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The `:via` protocol is four functions

Any module can be a `:via` backend if it exports:

```elixir
register_name(term, pid) :: :yes | :no
whereis_name(term)       :: pid | :undefined
unregister_name(term)    :: term
send(term, message)      :: pid
```

That's it. GenServer, `:gen_statem`, `:gen_event`, and friends all use
those four functions when you pass `name: {:via, Module, term}`.

### 2. The canonical form

```elixir
{:via, Registry, {RegistryName, key}}
{:via, Registry, {RegistryName, key, value}}   # key + extra metadata
```

The third element is passed as the second argument to the via module's
functions. For `Registry` it's always `{registry_name, key}` or
`{registry_name, key, value}`.

### 3. Centralize via-tuple construction

Don't sprinkle `{:via, Registry, {MyReg, ...}}` across the codebase.
Build it in one private helper so the registry name, key format, and
module reference live in one place:

```elixir
defp via(key), do: {:via, Registry, {MyApp.Registry, key}}
```

Refactoring from `Registry` to `Horde.Registry` or your own
implementation later becomes a one-line change.

### 4. Registered ≠ alive

A via tuple resolves to a pid via `whereis_name/1`. If the lookup returns
`:undefined`, any `GenServer.call` using it raises `:noproc`. The
canonical pattern is: try the call, rescue `:noproc`, start the process,
retry. Or, wrap it in a `find_or_start` helper like the chat-rooms
exercise.

---

## Implementation

### Step 1: Create the project

```bash
mix new via_tuple_patterns --sup
cd via_tuple_patterns
```

### Step 2: `lib/via_tuple_patterns/application.ex`

```elixir
defmodule ViaTuplePatterns.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Registry, keys: :unique, name: ViaTuplePatterns.Registry},
      ViaTuplePatterns.EtsVia  # custom via backend, see below
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ViaTuplePatterns.Supervisor)
  end
end
```

### Step 3: `lib/via_tuple_patterns/worker.ex`

```elixir
defmodule ViaTuplePatterns.Worker do
  @moduledoc """
  A trivial GenServer used to demonstrate via-tuple naming. `start_link/1`
  accepts any `name:` option and is agnostic to which backend is used.
  """

  use GenServer

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    tag = Keyword.get(opts, :tag, :unnamed)
    GenServer.start_link(__MODULE__, tag, name: name)
  end

  def tag(server), do: GenServer.call(server, :tag)

  @impl true
  def init(tag), do: {:ok, tag}

  @impl true
  def handle_call(:tag, _from, tag), do: {:reply, tag, tag}
end
```

### Step 4: `lib/via_tuple_patterns.ex` — the Registry-backed pattern

```elixir
defmodule ViaTuplePatterns do
  @moduledoc """
  The everyday case: a `Registry`-backed via tuple exposed through a
  single `via/1` helper, plus a `find_or_start/1` convenience.
  """

  alias ViaTuplePatterns.Worker

  @registry ViaTuplePatterns.Registry

  @doc "Centralized via-tuple construction. Callers never build it manually."
  @spec via(term()) :: {:via, module(), term()}
  def via(key), do: {:via, Registry, {@registry, key}}

  @doc """
  Returns a running worker pid for `key`, starting one if necessary.
  Safe under concurrent access — the registry's unique constraint plus
  `{:error, {:already_started, pid}}` handling resolve the race.
  """
  @spec find_or_start(term()) :: {:ok, pid()}
  def find_or_start(key) do
    case Registry.lookup(@registry, key) do
      [{pid, _}] ->
        {:ok, pid}

      [] ->
        case Worker.start_link(name: via(key), tag: key) do
          {:ok, pid} -> {:ok, pid}
          {:error, {:already_started, pid}} -> {:ok, pid}
        end
    end
  end

  @doc "Call via the registered name, not the pid."
  def tag(key), do: Worker.tag(via(key))
end
```

### Step 5: `lib/via_tuple_patterns/ets_via.ex` — a handwritten via backend

```elixir
defmodule ViaTuplePatterns.EtsVia do
  @moduledoc """
  A minimal hand-rolled `:via` backend backed by a public ETS table. Its
  only purpose is to show that the `:via` protocol is just four functions
  — there is no magic.

  Not production-grade: no monitor on registered pids, so dead entries
  linger. `Registry` exists precisely to solve that cleanup problem.
  """

  use GenServer

  @table __MODULE__

  # ── Lifecycle ───────────────────────────────────────────────────────────

  def start_link(_opts \\ []) do
    GenServer.start_link(__MODULE__, [], name: __MODULE__)
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    {:ok, nil}
  end

  # ── The `:via` protocol ─────────────────────────────────────────────────

  @doc "Register pid under key. Returns :yes on success, :no if taken."
  def register_name(key, pid) do
    if :ets.insert_new(@table, {key, pid}), do: :yes, else: :no
  end

  @doc "Return pid registered under key, or :undefined."
  def whereis_name(key) do
    case :ets.lookup(@table, key) do
      [{^key, pid}] -> if Process.alive?(pid), do: pid, else: :undefined
      [] -> :undefined
    end
  end

  @doc "Remove key if present."
  def unregister_name(key) do
    :ets.delete(@table, key)
    key
  end

  @doc "Deliver a message to the pid registered under key."
  def send(key, msg) do
    case whereis_name(key) do
      :undefined -> exit({:badarg, {key, msg}})
      pid -> Kernel.send(pid, msg); pid
    end
  end
end
```

### Step 6: `test/via_tuple_patterns_test.exs`

```elixir
defmodule ViaTuplePatternsTest do
  use ExUnit.Case, async: false

  alias ViaTuplePatterns.{Worker, EtsVia}

  describe "Registry-backed via tuple" do
    test "find_or_start returns the same pid for the same key" do
      {:ok, a} = ViaTuplePatterns.find_or_start(:alpha)
      {:ok, b} = ViaTuplePatterns.find_or_start(:alpha)
      assert a == b
    end

    test "call via the name, not the pid" do
      {:ok, _} = ViaTuplePatterns.find_or_start(:beta)
      assert ViaTuplePatterns.tag(:beta) == :beta
    end

    test "different keys resolve to different processes" do
      {:ok, a} = ViaTuplePatterns.find_or_start(:x)
      {:ok, b} = ViaTuplePatterns.find_or_start(:y)
      refute a == b
    end
  end

  describe "custom via backend (EtsVia)" do
    test "GenServer.start_link accepts our handwritten via module" do
      key = {:ets_via, System.unique_integer()}
      name = {:via, EtsVia, key}

      {:ok, pid} = Worker.start_link(name: name, tag: :hello)

      assert EtsVia.whereis_name(key) == pid
      assert Worker.tag(name) == :hello

      GenServer.stop(pid)
    end

    test "double registration fails at the via layer" do
      key = {:ets_via, :dup}
      name = {:via, EtsVia, key}

      {:ok, _} = Worker.start_link(name: name, tag: 1)
      # GenServer start returns {:error, {:already_started, ...}} because
      # our register_name returned :no on the second try.
      assert {:error, {:already_started, _}} = Worker.start_link(name: name, tag: 2)
    end
  end
end
```

### Step 7: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Never construct via tuples inline outside of one helper**
If `{:via, Registry, {MyReg, key}}` appears in 20 files, swapping the
backend is a 20-file diff. One `defp via(key)` is a 1-file diff and makes
the caller code read better.

**2. Via lookup is not free**
Each `GenServer.call(via_tuple, ...)` calls `whereis_name/1` (usually an
ETS read) before sending. On hot paths with stable pids, cache the pid
explicitly and call by pid.

**3. `whereis_name/1` can race with death**
Even `Registry` has a window where a just-died pid is still in the table.
Callers must be prepared to catch `:noproc`. Write it once in a helper
like `safe_call/2` that restarts the process if needed.

**4. Handwritten via backends without monitoring leak entries**
The `EtsVia` above is educational — dead pids stay in the table until
overwritten. Real backends monitor every registered pid and clean on
`:DOWN`. `Registry` does this for you; that's the main reason to use it.

**5. Via naming does not work across nodes unless the backend does**
`Registry` is local. `:global` is cluster-wide. Horde is cluster-wide with
handoff. Know which you have before shipping.

**6. When NOT to use a via tuple**
For a single, fixed, compile-time-known process, `name: MyServer` (atom)
is simpler, faster, and documents intent better than a via tuple. Reach
for via when the set of names is dynamic.

---

## Resources

- [`GenServer` — name registration options](https://hexdocs.pm/elixir/GenServer.html#module-name-registration)
- [`Registry` — `{:via, Registry, ...}` usage](https://hexdocs.pm/elixir/Registry.html#module-using-in-via)
- [Erlang `:global` module — the oldest via backend](https://www.erlang.org/doc/man/global.html)
- [Horde.Registry — drop-in distributed Registry](https://hexdocs.pm/horde/Horde.Registry.html)
