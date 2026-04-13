# Dynamic Registry for named chat rooms

**Project**: `chat_rooms_registry` — look up chat-room processes by name using a unique-keyed `Registry`.

---

## Project context

You're building the naming layer for a chat service. Every room has a
human-readable name (`"general"`, `"dev-ops"`, `"coffee"`) and exactly one
GenServer behind it. Rooms come and go dynamically: users create them at
runtime, they disappear when empty, and you can't know the full set up
front — so you can't use atom names (you'd leak atoms) and you don't need
cluster-wide naming (rooms are node-local here).

`Registry` in `:unique` mode is the canonical answer: string keys, automatic
cleanup when a room process dies, and no atom-table pressure. Combined with
`DynamicSupervisor`, it gives you "find-or-spawn a room by name" in a few
lines.

Project structure:

```
chat_rooms_registry/
├── lib/
│   ├── chat_rooms_registry.ex
│   ├── chat_rooms_registry/application.ex
│   ├── chat_rooms_registry/room.ex
│   └── chat_rooms_registry/rooms.ex
├── test/
│   └── chat_rooms_registry_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Registry` is an ETS-backed naming store

A `Registry` is a supervision tree wrapping one (or several, if partitioned)
ETS tables. Keys are arbitrary Elixir terms — usually strings or tuples — and
values are associated metadata. Lookups are O(1), reads are lock-free, and
entries are automatically removed when the owning process dies. It's local
to one node; for cross-node naming use `:global` or `:pg`.

### 2. `:unique` vs `:duplicate` keys

```
:unique     one pid per key → "name this process"
:duplicate  many pids per key → "subscribe to this topic"
```

For chat rooms, you want `:unique` — each room name maps to exactly one room
process. Registering the same key twice returns
`{:error, {:already_registered, pid}}`, which is the hook for "find or spawn".

### 3. `{:via, Registry, {name, key}}` — the via tuple

GenServer (and most OTP-shaped modules) accept a `name:` option of the form
`{:via, Module, term}`. The module must export `register_name/2`,
`whereis_name/1`, `unregister_name/1`, and `send/2`. `Registry` implements
that protocol, so you can `GenServer.start_link(Room, arg, name: via_tuple)`
and later `GenServer.call(via_tuple, :msg)` without ever holding the pid.

### 4. Automatic cleanup via process monitoring

`Registry` monitors every registered process. When a process dies — normal
exit, crash, kill — its entry is removed. There's a subtlety: the cleanup
is asynchronous, so a `lookup/2` right after a crash can briefly return a
dead pid. In tests, wait on `Process.monitor/1` + `:DOWN` before asserting
the registry is empty.

---

## Why Registry and not `Process.register/2` or `:global`

**`Process.register/2` with atom names.** Simple and fast, but atom names are never garbage-collected. Room names come from user input — atom-based naming is a memory leak. Viable only for a known, fixed set of servers.

**`:global` / Horde / Syn.** Cluster-wide naming with distributed consensus. Overkill for node-local rooms, and every registration pays a network round-trip. Use them when rooms must survive node failover.

**`Registry` (chosen).** ETS-backed, string keys, O(1) lookup, automatic cleanup on process death, zero atom-table pressure, and plugs into OTP via `:via` tuples.

---

## Design decisions

**Option A — `DynamicSupervisor` + manual pid tracking**
- Pros: Explicit control, no external dependency beyond stdlib.
- Cons: You re-implement monitor-based cleanup, race handling on concurrent `find_or_start`, and name→pid lookup yourself.

**Option B — `Registry` (`:unique`) + `DynamicSupervisor` + `:via` tuple** (chosen)
- Pros: Lookup, naming, and cleanup are handled by stdlib; `:via` keeps call sites pid-free; the race on concurrent starts collapses into `{:error, {:already_started, pid}}`.
- Cons: Cleanup is asynchronous, so a `lookup/2` immediately after a crash can briefly return a dead pid.

→ Chose **B** because the stdlib primitives solve naming, monitoring, and the start-race in a few lines, and the asynchronous-cleanup caveat is bounded and documented.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {DOWN},
    {already_registered},
    {already_started},
    {error},
    {exunit},
    {genserver},
    {noreply},
    {ok},
    {post},
    {reply},
    {via},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new chat_rooms_registry --sup
cd chat_rooms_registry
```

The `--sup` flag generates an `Application` module — we'll extend it to
start the `Registry` and a `DynamicSupervisor`.

### Step 2: `lib/chat_rooms_registry/application.ex`

**Objective**: Wire `application.ex` to start the supervision tree that starts the Registry before any via-tuple lookup can happen.


```elixir
defmodule ChatRoomsRegistry.Application do
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Unique registry: one pid per room name. `keys: :unique` is required
      # for `:via` usage — duplicate registries do not support naming.
      {Registry, keys: :unique, name: ChatRoomsRegistry.Registry},
      # DynamicSupervisor hosts room processes started at runtime.
      {DynamicSupervisor, strategy: :one_for_one, name: ChatRoomsRegistry.RoomSup}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ChatRoomsRegistry.Supervisor)
  end
end
```

Wire it in `mix.exs`:

```elixir
def application do
  [extra_applications: [:logger], mod: {ChatRoomsRegistry.Application, []}]
end
```

### Step 3: `lib/chat_rooms_registry/room.ex`

**Objective**: Implement `room.ex` — the naming/lookup strategy that decides how processes are addressed under concurrency and failure.


```elixir
defmodule ChatRoomsRegistry.Room do
  @moduledoc """
  A single chat room. State is just a list of messages; what matters for
  this exercise is that the room is *addressable by name* via the Registry.
  """

  use GenServer

  # ── Public API ──────────────────────────────────────────────────────────

  @doc "Starts a room registered under `name` in the shared Registry."
  @spec start_link(String.t()) :: GenServer.on_start()
  def start_link(name) when is_binary(name) do
    GenServer.start_link(__MODULE__, name, name: via(name))
  end

  @doc "Appends a message to the room. Accepts the room name, not a pid."
  @spec post(String.t(), String.t()) :: :ok
  def post(name, msg), do: GenServer.cast(via(name), {:post, msg})

  @doc "Returns all messages in the room (newest last)."
  @spec history(String.t()) :: [String.t()]
  def history(name), do: GenServer.call(via(name), :history)

  # Construct the `:via` tuple once — every caller uses this helper so the
  # registry name is defined in one place.
  defp via(name), do: {:via, Registry, {ChatRoomsRegistry.Registry, name}}

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(name), do: {:ok, %{name: name, messages: []}}

  @impl true
  def handle_cast({:post, msg}, %{messages: msgs} = state) do
    {:noreply, %{state | messages: msgs ++ [msg]}}
  end

  @impl true
  def handle_call(:history, _from, %{messages: msgs} = state) do
    {:reply, msgs, state}
  end
end
```

### Step 4: `lib/chat_rooms_registry/rooms.ex`

**Objective**: Implement `rooms.ex` — the naming/lookup strategy that decides how processes are addressed under concurrency and failure.


```elixir
defmodule ChatRoomsRegistry.Rooms do
  @moduledoc """
  Façade over the Registry + DynamicSupervisor pair. `find_or_start/1` is
  the usual "get me a room by name, spawning one if needed" operation.
  """

  alias ChatRoomsRegistry.Room

  @registry ChatRoomsRegistry.Registry
  @sup ChatRoomsRegistry.RoomSup

  @doc """
  Returns `{:ok, pid}` for the room named `name`, starting it under the
  DynamicSupervisor if it does not yet exist. Idempotent and safe to call
  from many processes concurrently.
  """
  @spec find_or_start(String.t()) :: {:ok, pid()}
  def find_or_start(name) do
    case Registry.lookup(@registry, name) do
      [{pid, _}] ->
        {:ok, pid}

      [] ->
        # The Registry itself handles the concurrent race: a second starter
        # will receive `{:error, {:already_started, pid}}` from start_child,
        # and we return that pid.
        case DynamicSupervisor.start_child(@sup, {Room, name}) do
          {:ok, pid} -> {:ok, pid}
          {:error, {:already_started, pid}} -> {:ok, pid}
        end
    end
  end

  @doc "List currently registered room names."
  @spec list() :: [String.t()]
  def list do
    # select/2 with a match spec is the cheapest way to enumerate keys.
    Registry.select(@registry, [{{:"$1", :_, :_}, [], [:"$1"]}])
  end

  @doc "Stop a room by name. Returns :ok whether or not it existed."
  @spec stop(String.t()) :: :ok
  def stop(name) do
    case Registry.lookup(@registry, name) do
      [{pid, _}] -> DynamicSupervisor.terminate_child(@sup, pid)
      [] -> :ok
    end

    :ok
  end
end
```

### Step 5: `test/chat_rooms_registry_test.exs`

**Objective**: Write `chat_rooms_registry_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule ChatRoomsRegistryTest do
  use ExUnit.Case, async: false

  alias ChatRoomsRegistry.{Room, Rooms}

  setup do
    # Each test starts from a clean slate.
    for name <- Rooms.list(), do: Rooms.stop(name)
    :ok
  end

  describe "find_or_start/1" do
    test "starts a new room the first time it is requested" do
      assert {:ok, pid} = Rooms.find_or_start("general")
      assert Process.alive?(pid)
      assert "general" in Rooms.list()
    end

    test "returns the same pid on subsequent calls" do
      {:ok, pid1} = Rooms.find_or_start("dev-ops")
      {:ok, pid2} = Rooms.find_or_start("dev-ops")
      assert pid1 == pid2
    end

    test "different names get different processes" do
      {:ok, a} = Rooms.find_or_start("a")
      {:ok, b} = Rooms.find_or_start("b")
      refute a == b
    end
  end

  describe "post/2 and history/1" do
    test "messages are addressable by room name, not pid" do
      {:ok, _} = Rooms.find_or_start("coffee")
      Room.post("coffee", "hello")
      Room.post("coffee", "world")
      assert Room.history("coffee") == ["hello", "world"]
    end
  end

  describe "automatic cleanup on crash" do
    test "registry entry disappears when the room dies" do
      {:ok, pid} = Rooms.find_or_start("ephemeral")
      ref = Process.monitor(pid)

      # Force a non-normal exit. The DynamicSupervisor default child spec
      # is :permanent, so the supervisor will try to restart the room;
      # for the registry-cleanup demo, we also terminate via the supervisor.
      DynamicSupervisor.terminate_child(ChatRoomsRegistry.RoomSup, pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}, 500

      # Wait for the async Registry cleanup to drain.
      :ok = wait_until(fn -> Registry.lookup(ChatRoomsRegistry.Registry, "ephemeral") == [] end)
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

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

`Registry` in `:unique` mode gives you a lock-free ETS index keyed by room name, and its monitor on each registered pid guarantees entries vanish when the owner dies. The `:via` tuple lets every call site address rooms by name without ever touching pids, and the `DynamicSupervisor` turns `find_or_start/1` into a race-safe idempotent operation thanks to `{:already_started, pid}`.

---


## Key Concepts: Dynamic Process Registration and Discovery

The `Registry` module is a key-value store for process names. Unlike `Process.register/2` which stores pids in a global atoms-based namespace (limited to atoms as names), `Registry` lets you store any term as a key and associate multiple pids with one key. It's the standard way to implement pub-sub, named worker pools, or gatekeeper patterns.

Registries are local to a node (not distributed). Each registry is a GenServer-backed table that you include in your supervision tree. When you register a pid with a key, the registry monitors the pid and auto-removes the entry on exit. The `match/2` and `lookup/2` functions let you query by key or pattern. The gotcha: registration is synchronous, so registering many pids in a loop can become a bottleneck—use bulk operations or Registry.dispatch/3 to send messages directly without fetching pids first.


## Benchmark

```elixir
{:ok, _} = ChatRoomsRegistry.Rooms.find_or_start("bench")

{time, _} =
  :timer.tc(fn ->
    Enum.each(1..100_000, fn _ ->
      Registry.lookup(ChatRoomsRegistry.Registry, "bench")
    end)
  end)

IO.puts("avg lookup: #{time / 100_000} µs")
```

Target esperado: <1 µs por `Registry.lookup/2` en hardware moderno (read-concurrency ETS).

---

## Trade-offs and production gotchas

**1. Registry is local-node only**
`Registry` lives in ETS on one node. If you distribute rooms across a
cluster you need `:global`, `:pg`, or a library like Horde or Syn. Don't
discover halfway through production that your single-node registry can't
follow the room to another node.

**2. Registration cleanup is asynchronous**
When a process dies, its entries are removed by the registry's monitor
handler — not atomically with the death. A `lookup/2` immediately after a
crash can still return the dead pid. Consumers should handle `:noproc`
errors from `GenServer.call` gracefully.

**3. Prefer strings/tuples over dynamic atoms**
The classic reason to use `Registry` instead of `Process.register/2` is
that atom names are never garbage-collected. If your keys come from user
input, atom-based naming is a memory leak waiting to happen.

**4. `:via` only works with `:unique` registries**
The via protocol requires a single pid per name. Attempting to use a
`:duplicate` registry as `{:via, Registry, ...}` crashes at runtime.
Duplicate registries are for pubsub-style dispatch, not for naming.

**5. Don't use `Registry` as a database**
It's fast, but it's still a process-level cache keyed by pid liveness. If
you need persistence, durable lookups, or history, back it with a real
store and keep the `Registry` as a pid-locator only.

**6. When NOT to use Registry**
For a fixed, known-at-compile-time set of named servers (a single cache,
a single scheduler), `Process.register/2` with an atom name is simpler and
marginally faster. Reach for `Registry` when names are dynamic or when you
need duplicate-key pubsub semantics.

---

## Reflection

- If rooms need to follow users across a 5-node cluster (a user on node B joins a room whose process lives on node A), would you still reach for `Registry`, or switch to Horde/Syn? What changes in the `find_or_start/1` contract?
- A room that crashes is auto-restarted by the `DynamicSupervisor` under a new pid, but the `Registry` entry is removed asynchronously. How would you design the client retry loop so callers never observe a stale `:noproc`?

---

## Resources

- [`Registry` — Elixir stdlib](https://hexdocs.pm/elixir/Registry.html)
- [`DynamicSupervisor` — Elixir stdlib](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Demystifying the Registry module in Elixir — Arpan Ghoshal](https://arpanghoshal3.medium.com/demystifying-the-registry-module-in-elixir-f0e07e770ec0)
- [José Valim / Dashbit — "What's new in Elixir 1.4" (Registry announcement)](https://dashbit.co/blog/whats-new-in-elixir-1-4)


## Key Concepts

Registry patterns in Elixir provide distributed name resolution through a central registry process. Unlike traditional naming services, Elixir registries are per-node by default but can be partitioned globally. Process name resolution follows a lookup chain: local registry → distributed registry (if configured) → `:global` → fallback mechanisms.

**Critical concepts:**
- **Via tuple pattern** `{:via, module, name}`: Enables pluggable naming backends. The registry module intercepts `:whereis`, `:register`, `:unregister` calls, allowing both local and distributed strategies.
- **Partitioned registries** (`Registry.start_link(partitions: 8)`): Reduce contention by sharding the registry across multiple ETS tables. Each partition handles independent name lookups, improving throughput under high concurrency.
- **Clustering implications**: Global registries across nodes require consensus. Elixir's registry design favors availability (CAP theorem) — a node can register locally and replicate asynchronously. This is why `:global` exists separately from local registries.

**Senior-level gotcha**: Mixing local and global registration without explicit sync logic can cause "phantom" processes — a process registered locally appears available to local callers but fails remote calls. Always make registry scope explicit in your architecture.
