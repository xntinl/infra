# Basic GenServer: an in-memory key/value store

**Project**: `kv_store_gs` — a minimal `get/put/delete` key/value store as a GenServer, with proper API separation.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

Almost every Elixir service eventually grows an in-process cache, a session
store, a config holder, or a lookup table that must be shared across
concurrent callers. Before reaching for ETS or a database, the idiomatic
first step is a small GenServer wrapping a map.

This exercise builds that canonical shape: a public API that hides the
GenServer machinery, a single process owning a map, and the three classic
operations (`get`, `put`, `delete`). You will also set up the convention
that the rest of OTP depends on: **callers never call `GenServer.call`
directly — the module exposes a domain-shaped API**.

Project structure:

```
kv_store_gs/
├── lib/
│   └── kv_store_gs.ex
├── test/
│   └── kv_store_gs_test.exs
└── mix.exs
```

---

## Core concepts

### 1. API module vs. server module

Good OTP modules expose domain functions (`KvStoreGs.put(pid, key, val)`)
and hide `GenServer.call/2` inside them. Callers should not know whether
the store is a GenServer, an ETS table, a GenStage pipeline, or a database
— that is an implementation detail. This separation is what lets you
refactor a GenServer into ETS without touching caller code.

### 2. `call` vs. `cast` — the default is `call`

`call/2` is synchronous: you wait for a reply, you find out if the server
crashed, and back-pressure happens naturally because callers block when
the server is busy. `cast/2` is asynchronous: no reply, no crash detection,
and no back-pressure. **Default to `call`** until you have a specific
reason to use `cast` (hot-path writes, telemetry).

### 3. `init/1` runs inside the server process

Code in `init/1` runs in the newly-spawned server process, not the caller.
Heavy work here blocks `start_link/1` from returning — which in turn
blocks the supervisor's startup. If init needs to do I/O, use
`{:ok, state, {:continue, :load}}` so the server starts fast and defers
heavy work to `handle_continue/2`.

### 4. Pattern matching on state

Idiomatic callbacks match on the state directly in the function head:

```elixir
def handle_call({:get, key}, _from, %{} = state) do
  {:reply, Map.get(state, key), state}
end
```

This turns the callback into a pure function of `(request, state) -> {reply, state}`,
which is what makes GenServer code so easy to test.

---

## Implementation

### Step 1: Create the project

```bash
mix new kv_store_gs
cd kv_store_gs
```

### Step 2: `lib/kv_store_gs.ex`

```elixir
defmodule KvStoreGs do
  @moduledoc """
  An in-memory key/value store backed by a single GenServer.

  The public API (`get/2`, `put/3`, `delete/2`) hides all GenServer
  mechanics; callers must not depend on the store being a process.
  """

  use GenServer

  @type key :: term()
  @type value :: term()

  # ── Public API ──────────────────────────────────────────────────────────

  @doc """
  Starts the store. Accepts standard `GenServer` options (e.g. `:name`).
  The initial state is always an empty map.
  """
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, opts)
  end

  @doc "Reads `key`. Returns `nil` if missing — callers can override with a default at the call site."
  @spec get(GenServer.server(), key()) :: value() | nil
  def get(server, key), do: GenServer.call(server, {:get, key})

  @doc "Inserts or replaces `key` with `value`. Synchronous so writes are observable on return."
  @spec put(GenServer.server(), key(), value()) :: :ok
  def put(server, key, value), do: GenServer.call(server, {:put, key, value})

  @doc "Removes `key`. Idempotent — deleting a missing key is a no-op."
  @spec delete(GenServer.server(), key()) :: :ok
  def delete(server, key), do: GenServer.call(server, {:delete, key})

  @doc "Returns the full map. Useful for tests and introspection."
  @spec snapshot(GenServer.server()) :: map()
  def snapshot(server), do: GenServer.call(server, :snapshot)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(%{} = initial), do: {:ok, initial}

  @impl true
  def handle_call({:get, key}, _from, state) do
    {:reply, Map.get(state, key), state}
  end

  def handle_call({:put, key, value}, _from, state) do
    {:reply, :ok, Map.put(state, key, value)}
  end

  def handle_call({:delete, key}, _from, state) do
    {:reply, :ok, Map.delete(state, key)}
  end

  def handle_call(:snapshot, _from, state) do
    {:reply, state, state}
  end
end
```

### Step 3: `test/kv_store_gs_test.exs`

```elixir
defmodule KvStoreGsTest do
  use ExUnit.Case, async: true

  setup do
    {:ok, store} = KvStoreGs.start_link()
    %{store: store}
  end

  describe "put/3 and get/2" do
    test "stores and retrieves a value", %{store: store} do
      assert :ok = KvStoreGs.put(store, :user, "alice")
      assert "alice" = KvStoreGs.get(store, :user)
    end

    test "get on missing key returns nil", %{store: store} do
      assert nil == KvStoreGs.get(store, :missing)
    end

    test "put overwrites existing value", %{store: store} do
      KvStoreGs.put(store, :k, 1)
      KvStoreGs.put(store, :k, 2)
      assert 2 = KvStoreGs.get(store, :k)
    end
  end

  describe "delete/2" do
    test "removes a key", %{store: store} do
      KvStoreGs.put(store, :k, 1)
      assert :ok = KvStoreGs.delete(store, :k)
      assert nil == KvStoreGs.get(store, :k)
    end

    test "is idempotent on missing keys", %{store: store} do
      assert :ok = KvStoreGs.delete(store, :never_existed)
    end
  end

  describe "snapshot/1" do
    test "returns the full map", %{store: store} do
      KvStoreGs.put(store, :a, 1)
      KvStoreGs.put(store, :b, 2)
      assert %{a: 1, b: 2} = KvStoreGs.snapshot(store)
    end
  end

  describe "concurrent access" do
    test "serialized writes produce a deterministic final state", %{store: store} do
      # 50 concurrent writers, each putting their own key. Because the
      # GenServer serializes writes, no updates are lost.
      tasks =
        for i <- 1..50 do
          Task.async(fn -> KvStoreGs.put(store, i, i * 10) end)
        end

      Enum.each(tasks, &Task.await/1)

      snapshot = KvStoreGs.snapshot(store)
      assert map_size(snapshot) == 50
      assert snapshot[7] == 70
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. A GenServer serializes all access — this is the point, and the cost**
Every `get/2` pays the round-trip to the server. Under high read load,
this is the bottleneck. For read-heavy workloads, back the store with
an ETS table (`:set`, `read_concurrency: true`) and use the GenServer
only for writes, or make it a *gatekeeper* that owns the table and
callers read directly.

**2. Large maps in process state hurt GC**
BEAM processes GC their entire heap together. A multi-megabyte map in
state means multi-megabyte GCs on every major collection pause. Keep
large data in ETS/persistent_term; keep the GenServer state small.

**3. `get/2` returning `nil` conflates "missing" and "stored nil"**
If callers may legitimately store `nil`, expose `fetch/2` returning
`{:ok, value} | :error` instead. This exercise chooses simplicity; a
production KV store should not.

**4. Naming: registered name vs. pid**
Registering the server with `name: KvStoreGs` is convenient but creates
global state — only one instance per node. For multi-tenant stores,
start many unnamed instances and pass the pid (or use a `Registry`).

**5. Every crash wipes the map**
GenServer state is purely in memory. If the process crashes — or the
supervisor restarts it — everything is gone. If the data must survive a
crash, persist it (disk, ETS `:dets`, or a database) and reload in `init/1`.

**6. When NOT to use a GenServer KV store**
For shared read-heavy caches across many callers, ETS is strictly better.
For persistent data, use a database. A GenServer KV store is right when
you need serialized writes, a small working set, and the simplicity of
"one process owns the data".

---

## Resources

- [`GenServer` — Elixir stdlib](https://hexdocs.pm/elixir/GenServer.html)
- [`Map` — Elixir stdlib](https://hexdocs.pm/elixir/Map.html)
- [Saša Jurić, *Elixir in Action*](https://www.manning.com/books/elixir-in-action-second-edition) — chapters on GenServer and OTP
- [`:ets` — when you outgrow a map in a GenServer](https://www.erlang.org/doc/man/ets.html)
