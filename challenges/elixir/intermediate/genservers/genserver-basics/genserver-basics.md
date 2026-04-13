# Basic GenServer: an in-memory key/value store

**Project**: `kv_store_gs` — a minimal `get/put/delete` key/value store as a GenServer, with proper API separation.

---

## Why genserver basico matters

Almost every Elixir service eventually grows an in-process cache, a session
store, a config holder, or a lookup table that must be shared across
concurrent callers. Before reaching for ETS or a database, the idiomatic
first step is a small GenServer wrapping a map.

This exercise builds that canonical shape: a public API that hides the
GenServer machinery, a single process owning a map, and the three classic
operations (`get`, `put`, `delete`). You will also set up the convention
that the rest of OTP depends on: **callers never call `GenServer.call`
directly — the module exposes a domain-shaped API**.

---

## Project structure

```
kv_store_gs/
├── lib/
│   └── kv_store_gs.ex
├── script/
│   └── main.exs
├── test/
│   └── kv_store_gs_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not a lower-level alternative?** For genserver basico, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

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

## Design decisions

**Option A — a bare process with `receive`**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — a GenServer (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because OTP gives us timeouts, code_change, supervision, and introspection for free.

## Implementation

### `mix.exs`

```elixir
defmodule KvStoreGs.MixProject do
  use Mix.Project

  def project do
    [
      app: :kv_store_gs,
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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new kv_store_gs
cd kv_store_gs
```

### `lib/kv_store_gs.ex`

**Objective**: Implement `kv_store_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.

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

**Objective**: Write `kv_store_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule KvStoreGsTest do
  use ExUnit.Case, async: true

  doctest KvStoreGs

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
  Runnable demo of `KvStoreGs`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== KvStoreGs demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    {:ok, _pid} = KvStoreGs.start_link([])
    :ok
  end
end

Main.main()
```

## Key Concepts: GenServer Message Handling and Concurrency

A GenServer processes messages sequentially through its mailbox. When you call `GenServer.call(pid, request)`, your process blocks until the server replies. When you call `GenServer.cast(pid, request)`, your process continues immediately. This is the fundamental trade-off: `call` gives you request-response semantics and backpressure (the caller waits), while `cast` is fire-and-forget (but you lose feedback).

The `{:reply, response, new_state}` return tuple from `handle_call` combines acknowledgment with state transition. If you return `{:noreply, new_state}`, the client is left waiting forever unless you send a reply later via a separate `send` or never reply (timeout triggers an error on the client side). For performance-sensitive paths, batch several casts before a call, or replace GenServer with plain ETS if you only need reads. The gotcha: a slow `handle_call` callback blocks all other calls from all clients—the server processes one message at a time. This is why monitoring and timeouts matter.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

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

## Reflection

- Un colega propone un GenServer para guardar una constante de configuración. ¿Qué le recomendás y por qué?

## Resources

- [`GenServer` — Elixir stdlib](https://hexdocs.pm/elixir/GenServer.html)
- [`Map` — Elixir stdlib](https://hexdocs.pm/elixir/Map.html)
- [Saša Jurić, *Elixir in Action*](https://www.manning.com/books/elixir-in-action-second-edition) — chapters on GenServer and OTP
- [`:ets` — when you outgrow a map in a GenServer](https://www.erlang.org/doc/man/ets.html)

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/kv_store_gs_test.exs`

```elixir
defmodule KvStoreGsTest do
  use ExUnit.Case, async: true

  doctest KvStoreGs

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert KvStoreGs.run(:noop) == :ok
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
