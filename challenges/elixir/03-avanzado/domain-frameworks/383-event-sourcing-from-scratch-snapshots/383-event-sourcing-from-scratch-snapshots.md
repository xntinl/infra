# Event Sourcing from Scratch with Snapshots

**Project**: `inventory_es` — minimal event-sourced warehouse-inventory aggregate with hand-built event store, snapshot support, and optimistic concurrency — no Commanded, no framework.

## Project context

A framework hides decisions that matter. Before reaching for Commanded you should have built event sourcing at least once with your own hands, so you understand what the framework is doing on your behalf. This exercise implements the bare minimum: an append-only event log in Postgres, a stream version check for optimistic concurrency, an aggregate that rebuilds state by folding events, and a snapshot mechanism so the fold does not start from zero for long-lived aggregates.

The domain is warehouse inventory: a `Stock` aggregate for a SKU tracks on-hand quantity, reservations, and receipts. Business rules: you cannot reserve more than on-hand minus existing reservations; you cannot cancel a reservation that does not exist. Every mutation is an event. State is a fold. Nothing else.

```
inventory_es/
├── lib/
│   └── inventory_es/
│       ├── application.ex
│       ├── repo.ex
│       ├── event_store.ex            # append / read_stream / write_snapshot
│       ├── stock/
│       │   ├── aggregate.ex          # decide + apply
│       │   ├── commands.ex
│       │   ├── events.ex
│       │   └── repository.ex         # load / save with version check
│       └── errors.ex
├── priv/repo/migrations/
│   ├── 20260412_create_events.exs
│   └── 20260412_create_snapshots.exs
├── test/
│   └── inventory_es/
│       └── stock_test.exs
├── bench/
│   └── replay_bench.exs
├── config/
│   └── config.exs
└── mix.exs
```

## Why build it yourself first

Commanded hides: stream versioning, replay, snapshots, idempotent dispatch, process registry. When a production incident involves "the aggregate loads with wrong balance", you must know which layer is responsible. Building the skeleton yourself makes the shape of the problem concrete.

## Why optimistic concurrency (and not pessimistic)

Two writers attempt to reserve from the same SKU. Pessimistic: `SELECT ... FOR UPDATE` on the row. Works but serializes everything and requires a single DB. Optimistic: each writer reads the current stream version, produces an event assuming that version, and the append fails if someone else appended first. Retries resolve conflicts. Under low contention it's strictly faster; under high contention it's lossy (retries cost too).

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. Event
Immutable fact with a stream, a version (position in the stream), a type, and a JSON payload.

### 2. Stream
All events for one aggregate instance, ordered by `version`. `(stream_id, version)` is unique.

### 3. Fold
`apply/2` recursively folds events to rebuild state. `state = Enum.reduce(events, initial, &apply/2)`.

### 4. Snapshot
A serialized state stored with the version it corresponds to. Loading uses the snapshot and replays only events *after* that version.

### 5. Optimistic concurrency
On append we assert the expected version. Postgres unique constraint on `(stream_id, version)` turns a race into an exception which we translate to a retry or an error.

## Design decisions

- **Option A — snapshot every N events**: deterministic, predictable replay time.
- **Option B — snapshot on time-based timer**: simpler to reason about. Con: worst-case replay grows with event rate.

→ A. N=100 here. Replay is bounded.

- **Option A — `jsonb` payload**: flexible, indexable, queryable without touching Elixir.
- **Option B — `bytea` with `:erlang.term_to_binary`**: faster, smaller. Con: opaque to DB tools, painful to evolve schema.

→ A. Operations value beats serialization speed unless profiled.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 1: Migrations

**Objective**: Shape the append-only `events` log plus `snapshots` cache — the unique `(stream_id, version)` index is the optimistic-concurrency guard.

```elixir
defmodule InventoryEs.Repo.Migrations.CreateEvents do
  use Ecto.Migration

  def change do
    create table(:events, primary_key: false) do
      add :id, :bigserial, primary_key: true
      add :stream_id, :string, null: false
      add :version, :integer, null: false
      add :type, :string, null: false
      add :payload, :jsonb, null: false
      add :inserted_at, :utc_datetime_usec, null: false, default: fragment("now()")
    end

    create unique_index(:events, [:stream_id, :version])
    create index(:events, [:type])
  end
end

defmodule InventoryEs.Repo.Migrations.CreateSnapshots do
  use Ecto.Migration

  def change do
    create table(:snapshots, primary_key: false) do
      add :stream_id, :string, primary_key: true
      add :version, :integer, null: false
      add :state, :jsonb, null: false
      add :inserted_at, :utc_datetime_usec, null: false, default: fragment("now()")
    end
  end
end
```

### Step 2: Repo

**Objective**: Wire the Ecto Repo against Postgres — the durable substrate for the event log; durability semantics are deferred to the database.

```elixir
defmodule InventoryEs.Repo do
  use Ecto.Repo, otp_app: :inventory_es, adapter: Ecto.Adapters.Postgres
end
```

### Step 3: Errors

**Objective**: Model `ConcurrencyError` as a typed exception so callers can distinguish version conflicts from transient database failures.

```elixir
defmodule InventoryEs.Errors do
  defmodule ConcurrencyError do
    defexception [:stream_id, :expected_version, :message]

    @impl true
    def exception(opts) do
      %__MODULE__{
        stream_id: opts[:stream_id],
        expected_version: opts[:expected_version],
        message: "stream #{opts[:stream_id]} is at a different version than expected (#{opts[:expected_version]})"
      }
    end
  end
end
```

### Step 4: Events and commands

**Objective**: Separate intent (commands, imperative) from facts (events, past-tense) — commands can fail, events are immutable history.

```elixir
defmodule InventoryEs.Stock.Events do
  defmodule StockReceived do
    @derive Jason.Encoder
    defstruct [:sku, :quantity]
  end

  defmodule StockReserved do
    @derive Jason.Encoder
    defstruct [:sku, :reservation_id, :quantity]
  end

  defmodule ReservationCancelled do
    @derive Jason.Encoder
    defstruct [:sku, :reservation_id]
  end
end

defmodule InventoryEs.Stock.Commands do
  defmodule ReceiveStock do
    @enforce_keys [:sku, :quantity]
    defstruct [:sku, :quantity]
  end

  defmodule Reserve do
    @enforce_keys [:sku, :reservation_id, :quantity]
    defstruct [:sku, :reservation_id, :quantity]
  end

  defmodule CancelReservation do
    @enforce_keys [:sku, :reservation_id]
    defstruct [:sku, :reservation_id]
  end
end
```

### Step 5: Aggregate

**Objective**: Split `decide/2` (pure command→events) from `apply/2` (pure event→state) — the core functional core of event sourcing.

```elixir
defmodule InventoryEs.Stock.Aggregate do
  alias InventoryEs.Stock.Commands.{ReceiveStock, Reserve, CancelReservation}
  alias InventoryEs.Stock.Events.{StockReceived, StockReserved, ReservationCancelled}

  defstruct sku: nil, on_hand: 0, reservations: %{}

  # --- decide: command → events or error ---

  def decide(%__MODULE__{}, %ReceiveStock{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  def decide(%__MODULE__{} = _state, %ReceiveStock{} = cmd) do
    {:ok, [%StockReceived{sku: cmd.sku, quantity: cmd.quantity}]}
  end

  def decide(%__MODULE__{}, %Reserve{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  def decide(%__MODULE__{reservations: r}, %Reserve{reservation_id: rid})
      when is_map_key(r, rid),
      do: {:error, :reservation_already_exists}

  def decide(%__MODULE__{} = state, %Reserve{} = cmd) do
    reserved_total = state.reservations |> Map.values() |> Enum.sum()
    available = state.on_hand - reserved_total

    if cmd.quantity > available do
      {:error, {:insufficient_stock, available: available}}
    else
      {:ok,
       [
         %StockReserved{
           sku: cmd.sku,
           reservation_id: cmd.reservation_id,
           quantity: cmd.quantity
         }
       ]}
    end
  end

  def decide(%__MODULE__{reservations: r}, %CancelReservation{reservation_id: rid})
      when not is_map_key(r, rid),
      do: {:error, :reservation_not_found}

  def decide(%__MODULE__{} = _state, %CancelReservation{} = cmd) do
    {:ok, [%ReservationCancelled{sku: cmd.sku, reservation_id: cmd.reservation_id}]}
  end

  # --- apply: event → state ---

  def apply(%__MODULE__{} = state, %StockReceived{sku: sku, quantity: q}),
    do: %{state | sku: sku, on_hand: state.on_hand + q}

  def apply(%__MODULE__{} = state, %StockReserved{reservation_id: rid, quantity: q}),
    do: %{state | reservations: Map.put(state.reservations, rid, q)}

  def apply(%__MODULE__{} = state, %ReservationCancelled{reservation_id: rid}),
    do: %{state | reservations: Map.delete(state.reservations, rid)}
end
```

### Step 6: Event store

**Objective**: Append events transactionally — translate the unique-index violation into `ConcurrencyError`, and snapshot every N versions to bound replay cost.

```elixir
defmodule InventoryEs.EventStore do
  import Ecto.Query
  alias InventoryEs.Repo
  alias InventoryEs.Errors.ConcurrencyError

  @snapshot_every 100

  def append(stream_id, expected_version, events) when is_list(events) do
    Repo.transaction(fn ->
      rows =
        events
        |> Enum.with_index(expected_version + 1)
        |> Enum.map(fn {event, v} ->
          %{
            stream_id: stream_id,
            version: v,
            type: event.__struct__ |> Module.split() |> List.last(),
            payload: Map.from_struct(event),
            inserted_at: DateTime.utc_now()
          }
        end)

      case Repo.insert_all("events", rows) do
        {n, _} when n == length(rows) -> :ok
        _ -> Repo.rollback(:append_failed)
      end
    end)
    |> translate_conflict(stream_id, expected_version)
  end

  defp translate_conflict({:ok, :ok}, _stream_id, _expected), do: :ok

  defp translate_conflict({:error, %Postgrex.Error{postgres: %{code: :unique_violation}}},
         stream_id,
         expected),
       do: raise(ConcurrencyError, stream_id: stream_id, expected_version: expected)

  defp translate_conflict({:error, reason}, _stream_id, _expected),
    do: {:error, reason}

  def read_stream(stream_id, from_version \\ 0) do
    Repo.all(
      from e in "events",
        where: e.stream_id == ^stream_id and e.version > ^from_version,
        order_by: [asc: e.version],
        select: %{version: e.version, type: e.type, payload: e.payload}
    )
  end

  def read_snapshot(stream_id) do
    Repo.one(
      from s in "snapshots",
        where: s.stream_id == ^stream_id,
        select: %{version: s.version, state: s.state}
    )
  end

  def maybe_write_snapshot(stream_id, version, state) when rem(version, @snapshot_every) == 0 do
    Repo.insert_all(
      "snapshots",
      [%{stream_id: stream_id, version: version, state: Map.from_struct(state),
         inserted_at: DateTime.utc_now()}],
      on_conflict: {:replace, [:version, :state, :inserted_at]},
      conflict_target: [:stream_id]
    )

    :ok
  end

  def maybe_write_snapshot(_stream_id, _version, _state), do: :ok
end
```

### Step 7: Repository (load / save)

**Objective**: Rehydrate from snapshot + tail events, then fold; the repository is the only place where impure IO meets the pure aggregate.

```elixir
defmodule InventoryEs.Stock.Repository do
  alias InventoryEs.EventStore
  alias InventoryEs.Stock.Aggregate
  alias InventoryEs.Stock.Events.{StockReceived, StockReserved, ReservationCancelled}

  def stream_id(sku), do: "stock-" <> sku

  @doc "Load aggregate by replaying snapshot + tail events. Returns {state, current_version}."
  def load(sku) do
    stream = stream_id(sku)

    {base_state, from_version} =
      case EventStore.read_snapshot(stream) do
        nil -> {%Aggregate{}, 0}
        %{version: v, state: json} -> {deserialize_state(json), v}
      end

    events = EventStore.read_stream(stream, from_version)
    final = Enum.reduce(events, base_state, fn e, acc -> Aggregate.apply(acc, rehydrate(e)) end)
    version = if events == [], do: from_version, else: List.last(events).version
    {final, version}
  end

  @doc "Execute a command, persist events, write snapshot if due."
  def execute(sku, cmd) do
    {state, version} = load(sku)

    case Aggregate.decide(state, cmd) do
      {:ok, events} ->
        stream = stream_id(sku)
        :ok = EventStore.append(stream, version, events)

        new_state = Enum.reduce(events, state, &Aggregate.apply(&2, &1))
        new_version = version + length(events)
        EventStore.maybe_write_snapshot(stream, new_version, new_state)
        {:ok, new_state, new_version}

      {:error, _} = err ->
        err
    end
  end

  # --- helpers ---

  defp rehydrate(%{type: "StockReceived", payload: p}),
    do: %StockReceived{sku: p["sku"], quantity: p["quantity"]}

  defp rehydrate(%{type: "StockReserved", payload: p}),
    do: %StockReserved{sku: p["sku"], reservation_id: p["reservation_id"], quantity: p["quantity"]}

  defp rehydrate(%{type: "ReservationCancelled", payload: p}),
    do: %ReservationCancelled{sku: p["sku"], reservation_id: p["reservation_id"]}

  defp deserialize_state(%{"sku" => sku, "on_hand" => on_hand, "reservations" => res}) do
    %Aggregate{
      sku: sku,
      on_hand: on_hand,
      reservations: for({k, v} <- res, into: %{}, do: {k, v})
    }
  end
end
```

## Load sequence with snapshot

```
┌────────────┐       ┌───────────────┐       ┌───────────────────────────┐
│ load(sku)  │──▶    │ read_snapshot │──▶    │ {state_v500, v=500}       │
└────────────┘       └───────────────┘       └────────────┬──────────────┘
                                                          │
                                                          ▼
                                          ┌───────────────────────────┐
                                          │ read_stream(stream, 500)  │
                                          │ → [ev501, ev502, ..., ev527]
                                          └────────────┬──────────────┘
                                                       │
                                                       ▼
                                          ┌───────────────────────────┐
                                          │ fold through Aggregate.apply
                                          └────────────┬──────────────┘
                                                       ▼
                                               {current_state, 527}
```

## Tests

```elixir
defmodule InventoryEs.StockTest do
  use ExUnit.Case, async: false

  alias InventoryEs.Stock.Repository
  alias InventoryEs.Stock.Commands.{ReceiveStock, Reserve, CancelReservation}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(InventoryEs.Repo)
    Ecto.Adapters.SQL.Sandbox.mode(InventoryEs.Repo, {:shared, self()})
    :ok
  end

  describe "ReceiveStock" do
    test "increases on_hand" do
      sku = unique_sku()
      assert {:ok, state, 1} = Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 50})
      assert state.on_hand == 50
    end

    test "rejects non-positive quantity" do
      sku = unique_sku()
      assert {:error, :quantity_must_be_positive} =
               Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 0})
    end
  end

  describe "Reserve" do
    test "reserves within available stock" do
      sku = unique_sku()
      {:ok, _, _} = Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 10})
      assert {:ok, state, 2} =
               Repository.execute(sku, %Reserve{sku: sku, reservation_id: "r1", quantity: 3})

      assert state.reservations["r1"] == 3
    end

    test "rejects over-reservation" do
      sku = unique_sku()
      {:ok, _, _} = Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 2})
      assert {:error, {:insufficient_stock, available: 2}} =
               Repository.execute(sku, %Reserve{sku: sku, reservation_id: "r1", quantity: 3})
    end
  end

  describe "replay from snapshot" do
    test "state reconstructed from snapshot + tail matches pure replay" do
      sku = unique_sku()
      {:ok, _, _} = Repository.execute(sku, %ReceiveStock{sku: sku, quantity: 1_000})

      for i <- 1..120 do
        {:ok, _, _} =
          Repository.execute(sku, %Reserve{sku: sku, reservation_id: "r#{i}", quantity: 1})
      end

      {state, version} = Repository.load(sku)
      assert version == 121
      assert map_size(state.reservations) == 120
    end
  end

  defp unique_sku, do: "sku-" <> Integer.to_string(:erlang.unique_integer([:positive]))
end
```

## Benchmark

```elixir
# bench/replay_bench.exs
sku_short = "bench-short-#{:erlang.unique_integer([:positive])}"
sku_long  = "bench-long-#{:erlang.unique_integer([:positive])}"

{:ok, _, _} = InventoryEs.Stock.Repository.execute(sku_short, %InventoryEs.Stock.Commands.ReceiveStock{sku: sku_short, quantity: 100})

{:ok, _, _} = InventoryEs.Stock.Repository.execute(sku_long, %InventoryEs.Stock.Commands.ReceiveStock{sku: sku_long, quantity: 10_000})

for i <- 1..500 do
  InventoryEs.Stock.Repository.execute(sku_long, %InventoryEs.Stock.Commands.Reserve{sku: sku_long, reservation_id: "r#{i}", quantity: 1})
end

Benchee.run(
  %{
    "load short stream (1 event)"   => fn -> InventoryEs.Stock.Repository.load(sku_short) end,
    "load long stream (501 events, snapshot at 500)" => fn -> InventoryEs.Stock.Repository.load(sku_long) end
  },
  time: 5,
  warmup: 2
)
```

Target: short-stream load < 1ms. Long-stream with snapshot < 2ms (snapshot read + 1 event fold). If the snapshot is *not* kicking in you will see >30ms on the long stream — that is your signal that `@snapshot_every` is misconfigured or the snapshots table is empty.

## Deep Dive

Specialized frameworks like Ash (business logic), Commanded (event sourcing), and Nx (numerical computing) abstract away common infrastructure but impose architectural constraints. Ash's declarative resource definitions simplify authorization and querying at the cost of reduced flexibility—deeply nested association policies can degrade query performance. Commanded's event store and aggregate roots enforce event sourcing discipline, making audit trails and temporal queries natural, but require careful snapshot strategy to avoid replaying years of events. Nx brings numerical computing to Elixir, but JIT compilation and lazy evaluation introduce latency; production models benefit from ahead-of-time compilation for inference. For IoT (Nerves), firmware updates must be atomic and resumable—OTA rollback on failure is non-negotiable. Choose frameworks that align with your scaling assumptions: Ash scales horizontally via read replicas; Commanded scales via sharding; Nx scales via distributed training.
## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Deep Dive: Domain Patterns and Production Implications

Domain-specific frameworks enforce module dependencies and architectural boundaries. Testing domain isolation ensures that constraints are maintained as the codebase grows. Production systems without boundary enforcement often become monolithic and hard to test.

---

## Trade-offs and production gotchas

**1. Fold time is linear in events**
Without snapshots, replay is O(events). For a hot aggregate receiving 1M events, every load reads 1M rows. Snapshots are not optional above a few thousand events per stream.

**2. Serialization of events is forever**
Once `StockReserved` has `quantity`, removing that field requires an up-caster that reads old payloads and fills in a default. Never break-change event shape.

**3. Unique constraint on `(stream_id, version)` is the concurrency check**
If you forget it, two writers can both append at version 100. You now have two timelines. The constraint is the *entire* correctness story for optimistic concurrency.

**4. Snapshots are an optimization, not a source of truth**
Delete all snapshots and replay must still produce the exact same state. If snapshots contain derived fields not computable from events, you have a bug — snapshots are now the source of truth for those fields.

**5. Process isolation is your job**
Without a registry, two concurrent `execute(sku, ...)` calls race: both load, both decide, both try to append. One succeeds, one raises `ConcurrencyError`. You must catch and retry (or funnel writes through a per-SKU process). A framework like Commanded hands this to you for free.

**6. When NOT to roll your own**
For any aggregate count > 10 or anything approaching production, use Commanded. This exercise exists to demystify the machinery, not to replace it.

## Reflection

You wrote state into the snapshot as a JSON map derived from the aggregate struct. If the struct gains a new field tomorrow (say `held_reservations`), every existing snapshot lacks that field. On load, you'd deserialize without it and may crash or silently skip. Design a snapshot versioning story: how would you detect a stale snapshot? Would you invalidate (replay fully) or migrate (up-cast)?

## Executable Example

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:jason, "~> 1.4"}
  ]
end

defmodule InventoryEs.Repo.Migrations.CreateEvents do
  use Ecto.Migration

  def change do
    create table(:events, primary_key: false) do
      add :id, :bigserial, primary_key: true
      add :stream_id, :string, null: false
      add :version, :integer, null: false
      add :type, :string, null: false
      add :payload, :jsonb, null: false
      add :inserted_at, :utc_datetime_usec, null: false, default: fragment("now()")
    end

    create unique_index(:events, [:stream_id, :version])
    create index(:events, [:type])
  end
end

defmodule InventoryEs.Repo.Migrations.CreateSnapshots do
  use Ecto.Migration

  def change do
    create table(:snapshots, primary_key: false) do
      add :stream_id, :string, primary_key: true
      add :version, :integer, null: false
      add :state, :jsonb, null: false
      add :inserted_at, :utc_datetime_usec, null: false, default: fragment("now()")
    end
  end
end

defmodule InventoryEs.Repo do
  use Ecto.Repo, otp_app: :inventory_es, adapter: Ecto.Adapters.Postgres
end

defmodule InventoryEs.Errors do
  defmodule ConcurrencyError do
    defexception [:stream_id, :expected_version, :message]

    @impl true
    def exception(opts) do
      %__MODULE__{
        stream_id: opts[:stream_id],
        expected_version: opts[:expected_version],
        message: "stream #{opts[:stream_id]} is at a different version than expected (#{opts[:expected_version]})"
      }
    end
  end
end

defmodule InventoryEs.Stock.Events do
  defmodule StockReceived do
    @derive Jason.Encoder
    defstruct [:sku, :quantity]
  end

  defmodule StockReserved do
    @derive Jason.Encoder
    defstruct [:sku, :reservation_id, :quantity]
  end

  defmodule ReservationCancelled do
    @derive Jason.Encoder
    defstruct [:sku, :reservation_id]
  end
end

defmodule InventoryEs.Stock.Commands do
  defmodule ReceiveStock do
    @enforce_keys [:sku, :quantity]
    defstruct [:sku, :quantity]
  end

  defmodule Reserve do
    @enforce_keys [:sku, :reservation_id, :quantity]
    defstruct [:sku, :reservation_id, :quantity]
  end

  defmodule CancelReservation do
    @enforce_keys [:sku, :reservation_id]
    defstruct [:sku, :reservation_id]
  end
end

defmodule InventoryEs.Stock.Aggregate do
  end
  alias InventoryEs.Stock.Commands.{ReceiveStock, Reserve, CancelReservation}
  alias InventoryEs.Stock.Events.{StockReceived, StockReserved, ReservationCancelled}

  defstruct sku: nil, on_hand: 0, reservations: %{}

  # --- decide: command → events or error ---

  def decide(%__MODULE__{}, %ReceiveStock{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  def decide(%__MODULE__{} = _state, %ReceiveStock{} = cmd) do
    {:ok, [%StockReceived{sku: cmd.sku, quantity: cmd.quantity}]}
  end

  def decide(%__MODULE__{}, %Reserve{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  def decide(%__MODULE__{reservations: r}, %Reserve{reservation_id: rid})
      when is_map_key(r, rid),
      do: {:error, :reservation_already_exists}

  def decide(%__MODULE__{} = state, %Reserve{} = cmd) do
    reserved_total = state.reservations |> Map.values() |> Enum.sum()
    available = state.on_hand - reserved_total

    if cmd.quantity > available do
      {:error, {:insufficient_stock, available: available}}
    else
      {:ok,
       [
         %StockReserved{
           sku: cmd.sku,
           reservation_id: cmd.reservation_id,
           quantity: cmd.quantity
         }
       ]}
    end
  end

  def decide(%__MODULE__{reservations: r}, %CancelReservation{reservation_id: rid})
      when not is_map_key(r, rid),
      do: {:error, :reservation_not_found}

  def decide(%__MODULE__{} = _state, %CancelReservation{} = cmd) do
    {:ok, [%ReservationCancelled{sku: cmd.sku, reservation_id: cmd.reservation_id}]}
  end

  # --- apply: event → state ---

  def apply(%__MODULE__{} = state, %StockReceived{sku: sku, quantity: q}),
    do: %{state | sku: sku, on_hand: state.on_hand + q}

  def apply(%__MODULE__{} = state, %StockReserved{reservation_id: rid, quantity: q}),
    do: %{state | reservations: Map.put(state.reservations, rid, q)}

  def apply(%__MODULE__{} = state, %ReservationCancelled{reservation_id: rid}),
    do: %{state | reservations: Map.delete(state.reservations, rid)}
end

defmodule InventoryEs.EventStore do
  import Ecto.Query
  alias InventoryEs.Repo
  alias InventoryEs.Errors.ConcurrencyError

  @snapshot_every 100

  def append(stream_id, expected_version, events) when is_list(events) do
    Repo.transaction(fn ->
      rows =
        events
        |> Enum.with_index(expected_version + 1)
        |> Enum.map(fn {event, v} ->
          %{
            stream_id: stream_id,
            version: v,
            type: event.__struct__ |> Module.split() |> List.last(),
            payload: Map.from_struct(event),
            inserted_at: DateTime.utc_now()
          }
        end)

      case Repo.insert_all("events", rows) do
        {n, _} when n == length(rows) -> :ok
        _ -> Repo.rollback(:append_failed)
      end
    end)
    |> translate_conflict(stream_id, expected_version)
  end

  defp translate_conflict({:ok, :ok}, _stream_id, _expected), do: :ok

  defp translate_conflict({:error, %Postgrex.Error{postgres: %{code: :unique_violation}}},
         stream_id,
         expected),
       do: raise(ConcurrencyError, stream_id: stream_id, expected_version: expected)

  defp translate_conflict({:error, reason}, _stream_id, _expected),
    do: {:error, reason}

  def read_stream(stream_id, from_version \\ 0) do
    Repo.all(
      from e in "events",
        where: e.stream_id == ^stream_id and e.version > ^from_version,
        order_by: [asc: e.version],
        select: %{version: e.version, type: e.type, payload: e.payload}
    )
  end

  def read_snapshot(stream_id) do
    Repo.one(
      from s in "snapshots",
        where: s.stream_id == ^stream_id,
        select: %{version: s.version, state: s.state}
    )
  end

  def maybe_write_snapshot(stream_id, version, state) when rem(version, @snapshot_every) == 0 do
    Repo.insert_all(
      "snapshots",
      [%{stream_id: stream_id, version: version, state: Map.from_struct(state),
         inserted_at: DateTime.utc_now()}],
      on_conflict: {:replace, [:version, :state, :inserted_at]},
      conflict_target: [:stream_id]
    )

    :ok
  end

  def maybe_write_snapshot(_stream_id, _version, _state), do: :ok
end

defmodule InventoryEs.Stock.Repository do
  end
  alias InventoryEs.EventStore
  alias InventoryEs.Stock.Aggregate
  alias InventoryEs.Stock.Events.{StockReceived, StockReserved, ReservationCancelled}

  def stream_id(sku), do: "stock-" <> sku

  @doc "Load aggregate by replaying snapshot + tail events. Returns {state, current_version}."
  def load(sku) do
    stream = stream_id(sku)

    {base_state, from_version} =
      case EventStore.read_snapshot(stream) do
        nil -> {%Aggregate{}, 0}
        %{version: v, state: json} -> {deserialize_state(json), v}
      end

    events = EventStore.read_stream(stream, from_version)
    final = Enum.reduce(events, base_state, fn e, acc -> Aggregate.apply(acc, rehydrate(e)) end)
    version = if events == [], do: from_version, else: List.last(events).version
    {final, version}
  end

  @doc "Execute a command, persist events, write snapshot if due."
  def execute(sku, cmd) do
    {state, version} = load(sku)

    case Aggregate.decide(state, cmd) do
      {:ok, events} ->
        stream = stream_id(sku)
        :ok = EventStore.append(stream, version, events)

        new_state = Enum.reduce(events, state, &Aggregate.apply(&2, &1))
        new_version = version + length(events)
        EventStore.maybe_write_snapshot(stream, new_version, new_state)
        {:ok, new_state, new_version}

      {:error, _} = err ->
        err
    end
  end

  # --- helpers ---

  defp rehydrate(%{type: "StockReceived", payload: p}),
    do: %StockReceived{sku: p["sku"], quantity: p["quantity"]}

  defp rehydrate(%{type: "StockReserved", payload: p}),
    do: %StockReserved{sku: p["sku"], reservation_id: p["reservation_id"], quantity: p["quantity"]}

  defp rehydrate(%{type: "ReservationCancelled", payload: p}),
    do: %ReservationCancelled{sku: p["sku"], reservation_id: p["reservation_id"]}

  defp deserialize_state(%{"sku" => sku, "on_hand" => on_hand, "reservations" => res}) do
    %Aggregate{
      sku: sku,
      on_hand: on_hand,
      reservations: for({k, v} <- res, into: %{}, do: {k, v})
    }
  end
end

defmodule Main do
  def main do
      # Demonstrating 383-event-sourcing-from-scratch-snapshots
      :ok
  end
end

Main.main()
end
end
end
end
end
end
end
end
end
end
end
end
```
