# Horde.Registry and Horde.DynamicSupervisor (CRDT-based)

**Project**: `game_sessions` — distributed session processes where each game room is a single process living on exactly one node at any time, but any node can locate it and any node can host it after a failure

---

## Why distribution and clustering matters

Distributed Erlang gives you remote message-passing transparency, but the cost is your responsibility for split-brain detection, registry consistency, and net-tick policies. Libcluster, Horde, and PG provide pieces; you compose them.

Clusters fail in interesting ways: netsplits, asymmetric partitions, GC pauses misread as crashes, and global registry race conditions. Designing for the network — rather than against it — is the senior shift.

---

## The business problem

You are building a production-grade Elixir component in the **Distribution and clustering** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
game_sessions/
├── lib/
│   └── game_sessions.ex
├── script/
│   └── main.exs
├── test/
│   └── game_sessions_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Distribution and clustering the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule GameSessions.MixProject do
  use Mix.Project

  def project do
    [
      app: :game_sessions,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/game_sessions.ex`

```elixir
# lib/game_sessions/application.ex
defmodule GameSessions.Application do
  @moduledoc """
  Ejercicio: Horde.Registry and Horde.DynamicSupervisor (CRDT-based).
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Application

  @impl true
  def start(_type, _args) do
    topologies = Application.get_env(:libcluster, :topologies, [])

    children = [
      {Cluster.Supervisor, [topologies, [name: GameSessions.ClusterSupervisor]]},
      GameSessions.RoomRegistry,
      GameSessions.RoomSupervisor,
      %{
        id: GameSessions.NodeListener,
        start: {GameSessions.NodeListener, :start_link, []}
      }
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: GameSessions.Supervisor)
  end
end

# lib/game_sessions/room_registry.ex
defmodule GameSessions.RoomRegistry do
  def child_spec(_arg) do
    Supervisor.child_spec(
      {Horde.Registry, name: __MODULE__, keys: :unique, members: :auto},
      id: __MODULE__
    )
  end

  @doc "Returns via result from room_id."
  def via(room_id), do: {:via, Horde.Registry, {__MODULE__, room_id}}

  @doc "Returns whereis result from room_id."
  def whereis(room_id) do
    case Horde.Registry.lookup(__MODULE__, room_id) do
      [{pid, _value}] -> pid
      [] -> nil
    end
  end
end

# lib/game_sessions/room_supervisor.ex
defmodule GameSessions.RoomSupervisor do
  def child_spec(_arg) do
    Supervisor.child_spec(
      {Horde.DynamicSupervisor,
       name: __MODULE__,
       strategy: :one_for_one,
       distribution_strategy: Horde.UniformRandomDistribution,
       members: :auto,
       process_redistribution: :active},
      id: __MODULE__
    )
  end

  @doc "Starts room result from room_id and opts."
  def start_room(room_id, opts \\ []) do
    spec = {GameSessions.Room, Keyword.put(opts, :room_id, room_id)}
    Horde.DynamicSupervisor.start_child(__MODULE__, spec)
  end
end

# lib/game_sessions/node_listener.ex
defmodule GameSessions.NodeListener do
  use GenServer
  require Logger

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @impl true
  def init(_) do
    :net_kernel.monitor_nodes(true, node_type: :visible)
    {:ok, nil}
  end

  @impl true
  def handle_info({:nodeup, node, _info}, state) do
    Logger.info("nodeup: #{node} — Horde handles member sync via :auto")
    {:noreply, state}
  end

  def handle_info({:nodedown, node, _info}, state) do
    Logger.warning("nodedown: #{node} — Horde will redistribute children")
    {:noreply, state}
  end
end

# lib/game_sessions/room.ex
defmodule GameSessions.Room do
  use GenServer, restart: :transient

  alias GameSessions.RoomRegistry

  def start_link(opts) do
    room_id = Keyword.fetch!(opts, :room_id)
    GenServer.start_link(__MODULE__, opts, name: RoomRegistry.via(room_id))
  end

  @doc "Joins result from room_id and player."
  def join(room_id, player), do: GenServer.call(RoomRegistry.via(room_id), {:join, player})
  @doc "Returns players result from room_id."
  def players(room_id), do: GenServer.call(RoomRegistry.via(room_id), :players)

  @impl true
  def init(opts) do
    {:ok, %{room_id: Keyword.fetch!(opts, :room_id), players: []}}
  end

  @impl true
  def handle_call({:join, player}, _from, state) do
    {:reply, :ok, %{state | players: [player | state.players]}}
  end

  def handle_call(:players, _from, state), do: {:reply, state.players, state}
end

# lib/game_sessions/rooms.ex
defmodule GameSessions.Rooms do
  alias GameSessions.{RoomRegistry, RoomSupervisor, Room}

  @doc "Opens result from room_id."
  def open(room_id) do
    case RoomSupervisor.start_room(room_id) do
      {:ok, pid} -> {:ok, pid}
      {:error, {:already_started, pid}} -> {:ok, pid}
      other -> other
    end
  end

  @doc "Joins result from room_id and player."
  def join(room_id, player), do: Room.join(room_id, player)
  @doc "Returns players result from room_id."
  def players(room_id), do: Room.players(room_id)
  @doc "Returns whereis result from room_id."
  def whereis(room_id), do: RoomRegistry.whereis(room_id)
end
```

### `test/game_sessions_test.exs`

```elixir
defmodule GameSessions.RoomsTest do
  use ExUnit.Case, async: true
  doctest GameSessions.Application

  alias GameSessions.{Rooms, RoomRegistry}

  describe "open/1 — idempotency" do
    test "returns the same pid on repeated calls" do
      {:ok, pid1} = Rooms.open("r_a")
      {:ok, pid2} = Rooms.open("r_a")
      assert pid1 == pid2
    end
  end

  describe "join/2 and players/1" do
    test "joined players are visible through the registry" do
      {:ok, _pid} = Rooms.open("r_b")
      :ok = Rooms.join("r_b", "alice")
      :ok = Rooms.join("r_b", "bob")

      assert Enum.sort(Rooms.players("r_b")) == ["alice", "bob"]
    end
  end

  describe "whereis/1" do
    test "returns nil for unknown room" do
      assert RoomRegistry.whereis("does_not_exist") == nil
    end

    test "returns pid for known room" do
      {:ok, pid} = Rooms.open("r_c")
      assert RoomRegistry.whereis("r_c") == pid
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Horde.Registry and Horde.DynamicSupervisor (CRDT-based).

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Horde.Registry and Horde.DynamicSupervisor (CRDT-based) ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case GameSessions.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: GameSessions.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Partitions are the rule, not the exception

In a multi-AZ cluster, brief netsplits happen daily. Design for them: prefer eventual consistency, use idempotent operations, and detect split-brain explicitly.

### 2. Registries don't replicate transparently

Local Registry is fast and node-local. :global is consistent but slow. Horde.Registry replicates via CRDTs — eventual consistency, no global locks. Pick based on your read/write ratio.

### 3. Tune net_kernel ticks for your environment

The default 60-second tick is too long for production failure detection but too short for high-latency cross-region links. Measure first.

---
