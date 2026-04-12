# Behaviours and callbacks — defining a Storage contract

**Project**: `storage_behaviour` — a pluggable key/value `Storage` behaviour with an in-memory implementation and an ETS-backed implementation sharing the same contract.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You're building a library that reads and writes small pieces of state, and you
don't yet know where that state will live: maybe a map in memory for tests,
maybe ETS in production, maybe Redis later. Hard-coding one of them into every
caller is painful — every swap becomes a rewrite.

Behaviours are Elixir's (and Erlang's) answer to "interfaces you can type-check".
A behaviour declares a set of `@callback` signatures. Any module that claims
`@behaviour MyThing` is expected to implement all of them, and the compiler
warns if any are missing. This is how `GenServer`, `Supervisor`, and `Plug`
are defined — and it's how you should define your own swappable adapters.

This exercise defines `Storage` as a behaviour, implements two adapters, and
shows how callers depend on the behaviour module (the contract), not on a
specific implementation.

Project structure:

```
storage_behaviour/
├── lib/
│   ├── storage.ex
│   ├── storage/in_memory.ex
│   └── storage/ets_store.ex
├── test/
│   └── storage_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `@callback` declares a function signature

```elixir
@callback get(key :: term()) :: {:ok, term()} | :error
```

The `::` is a typespec — names before `::` are documentation, the type after
is the contract. The compiler emits warnings if an `@behaviour` module is
missing a callback or implements it with the wrong arity.

### 2. `@optional_callbacks` for "nice to have" functions

```elixir
@optional_callbacks [clear: 0]
```

An implementer may or may not provide `clear/0`. Callers should check with
`function_exported?/3` before invoking optional callbacks.

### 3. `@impl true` makes intent explicit

Marking an implementation with `@impl true` asks the compiler: "this must be a
callback from the behaviour I declared; warn me if I got the name or arity
wrong". It turns typos into compiler errors instead of silent runtime mistakes.

### 4. Behaviours dispatch explicitly — not magically

Unlike protocols, behaviours don't dispatch on the value's type. The caller
picks the module: `impl.get(key)`. Behaviours are for "pick an adapter at
config time", protocols are for "dispatch on this value's shape".

---

## Implementation

### Step 1: Create the project

```bash
mix new storage_behaviour
cd storage_behaviour
```

### Step 2: `lib/storage.ex`

```elixir
defmodule Storage do
  @moduledoc """
  A pluggable key/value storage contract. Implementations must provide
  `get/1`, `put/2`, and `delete/1`. `clear/0` is optional.

  Callers should depend on this module's documented contract and accept the
  implementation module as configuration, not as a hard-coded reference.
  """

  @type key :: term()
  @type value :: term()

  @callback get(key) :: {:ok, value} | :error
  @callback put(key, value) :: :ok
  @callback delete(key) :: :ok
  @callback clear() :: :ok

  @optional_callbacks [clear: 0]

  @doc """
  Convenience dispatcher: given an implementation module and a key, fetch
  with a default. Exists to show that higher-level helpers can live in the
  behaviour module itself.
  """
  @spec fetch(module(), key, value) :: value
  def fetch(impl, key, default) do
    case impl.get(key) do
      {:ok, value} -> value
      :error -> default
    end
  end
end
```

### Step 3: `lib/storage/in_memory.ex`

```elixir
defmodule Storage.InMemory do
  @moduledoc """
  Process-dictionary-backed `Storage` implementation. Good for single-process
  tests; do NOT use across processes — the process dictionary is per-process.
  """

  @behaviour Storage

  @impl true
  def get(key) do
    case Process.get({__MODULE__, key}, :__absent__) do
      :__absent__ -> :error
      value -> {:ok, value}
    end
  end

  @impl true
  def put(key, value) do
    Process.put({__MODULE__, key}, value)
    :ok
  end

  @impl true
  def delete(key) do
    Process.delete({__MODULE__, key})
    :ok
  end

  @impl true
  def clear do
    # Iterate our own namespaced keys so we don't wipe unrelated state.
    for {{__MODULE__, _} = k, _} <- Process.get(), do: Process.delete(k)
    :ok
  end
end
```

### Step 4: `lib/storage/ets_store.ex`

```elixir
defmodule Storage.EtsStore do
  @moduledoc """
  ETS-backed `Storage` implementation. The table is created lazily on first
  use and shared across processes (`:public` + `:named_table`).
  """

  @behaviour Storage

  @table :storage_ets_store

  @impl true
  def get(key) do
    ensure_table()

    case :ets.lookup(@table, key) do
      [{^key, value}] -> {:ok, value}
      [] -> :error
    end
  end

  @impl true
  def put(key, value) do
    ensure_table()
    :ets.insert(@table, {key, value})
    :ok
  end

  @impl true
  def delete(key) do
    ensure_table()
    :ets.delete(@table, key)
    :ok
  end

  @impl true
  def clear do
    ensure_table()
    :ets.delete_all_objects(@table)
    :ok
  end

  # ETS tables are owned by the process that creates them. For a standalone
  # demo we create with :public so any process can read/write.
  defp ensure_table do
    case :ets.whereis(@table) do
      :undefined -> :ets.new(@table, [:named_table, :public, :set])
      _tid -> @table
    end
  end
end
```

### Step 5: `test/storage_test.exs`

```elixir
defmodule StorageTest do
  # async: false because EtsStore uses a named, globally-shared ETS table.
  use ExUnit.Case, async: false

  # Run the exact same suite against every implementation — this is the
  # whole point of the behaviour: interchangeable backends.
  for impl <- [Storage.InMemory, Storage.EtsStore] do
    describe "#{inspect(impl)} conforms to Storage" do
      setup do
        unquote(impl).clear()
        :ok
      end

      test "put then get returns the stored value" do
        assert :ok = unquote(impl).put(:a, 1)
        assert {:ok, 1} = unquote(impl).get(:a)
      end

      test "get on missing key returns :error" do
        assert :error = unquote(impl).get(:missing)
      end

      test "delete removes the key" do
        unquote(impl).put(:a, 1)
        unquote(impl).delete(:a)
        assert :error = unquote(impl).get(:a)
      end

      test "Storage.fetch/3 uses the adapter and falls back" do
        unquote(impl).put(:present, "hi")
        assert "hi" = Storage.fetch(unquote(impl), :present, "default")
        assert "default" = Storage.fetch(unquote(impl), :missing, "default")
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

**1. Behaviours enforce shape at compile time, not semantics**
The compiler checks that `get/1` exists with arity 1. It does NOT check that
your `get/1` is referentially transparent, or that `put/2` actually persists
anything. Contracts beyond shape must be expressed in docs and tests.

**2. Optional callbacks require `function_exported?/3` at the call site**
Forgetting to guard an optional callback call crashes callers against
implementations that skipped it. Always check before calling.

**3. `@impl true` is not optional in practice**
Without `@impl true`, renaming a callback in the behaviour fails silently —
the old function stays defined and unused, and a new caller errors at
runtime. Always annotate.

**4. Typespecs in `@callback` are documentation, not runtime checks**
Dialyzer *can* check them statically, but nothing checks at runtime that
`get/1` really returns `{:ok, term}` or `:error`. Defensive programming at
the contract boundary (a `case` + catch-all) remains your responsibility.

**5. When NOT to use a behaviour**
If the "contract" is really "one function that might do different things
depending on the value", that's a protocol, not a behaviour. And if there
will only ever be one implementation, skip the ceremony entirely — a direct
module call is clearer.

---

## Resources

- [Typespecs and behaviours — Elixir guide](https://hexdocs.pm/elixir/typespecs.html)
- [`@behaviour` and `@callback`](https://hexdocs.pm/elixir/Module.html#module-behaviour)
- [ETS User Guide — Erlang docs](https://www.erlang.org/doc/man/ets.html)
- ["Mocks and explicit contracts" — José Valim](http://blog.plataformatec.com.br/2015/10/mocks-and-explicit-contracts/) — why behaviours are the foundation of testable code
