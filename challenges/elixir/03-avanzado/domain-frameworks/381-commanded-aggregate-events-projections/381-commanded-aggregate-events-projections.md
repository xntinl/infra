# Commanded — Aggregate, Events and Projections

**Project**: `shopping_cart_es` — shopping cart aggregate with domain events and a read-model projection using Commanded on top of `commanded_eventstore_adapter`.

## Project context

You are rebuilding the shopping-cart service. The previous CRUD design had a single `carts` table that the front-end, the billing job, and the analytics pipeline all queried for different reasons. The billing job needed the *sequence* of adds/removes to compute promotions retroactively, but the CRUD table only kept the current state. Event sourcing solves this by persisting the *decisions* (events), and deriving any number of read models.

Commanded is the Elixir toolkit for CQRS + Event Sourcing on the BEAM. It handles aggregate routing, process lifecycles, event dispatch, idempotency, and projection bookkeeping. In this exercise we build one aggregate (`Cart`), three events, three commands, and a read-model projection that maintains a current-state view for UI queries. The event store is `commanded_eventstore_adapter`, which is a pure-Elixir, PostgreSQL-backed event store.

```
shopping_cart_es/
├── lib/
│   └── shopping_cart_es/
│       ├── application.ex
│       ├── commanded_app.ex              # Commanded application
│       ├── router.ex                     # routes commands to aggregates
│       ├── cart/
│       │   ├── aggregate.ex              # the Cart aggregate
│       │   ├── commands.ex               # AddItem, RemoveItem, Checkout
│       │   └── events.ex                 # ItemAdded, ItemRemoved, CartCheckedOut
│       └── projections/
│           └── cart_summary_projector.ex # maintains cart_summaries table
├── priv/repo/migrations/
│   └── 20260412_create_cart_summaries.exs
├── test/
│   └── shopping_cart_es/
│       ├── cart/aggregate_test.exs
│       └── projections/cart_summary_projector_test.exs
├── config/
│   └── config.exs
└── mix.exs
```

## Why event sourcing for a cart

A cart is a short-lived, high-churn entity whose *history* has business value: coupon engines, abandonment analytics, and fraud detection all care about what was added and removed, not just what is in the cart right now. Storing events gives us that history for free. The CRUD alternative requires a parallel audit table that drifts from reality.

## Why Commanded and not rolling your own

Rolling your own event sourcing means writing: event versioning, aggregate loading (replay from snapshot + tail events), concurrency control (stream version check), process registry (one aggregate process per id), dispatch routing, and projection bookkeeping (exactly-once handling of events). Commanded ships all of that. The cost is a specific conceptual model (`Aggregate`, `Router`, `EventHandler`, `ProcessManager`) that you must learn.

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
### 1. Command
A plain struct describing an *intent* ("please add this item"). Commands can be rejected.

### 2. Event
A plain struct describing something that *happened*. Events cannot be rejected — they are facts.

### 3. Aggregate
A stateful, single-threaded decision-maker. It receives a command, produces zero or more events, and applies those events to its own state. The aggregate's state is rebuilt at load time by replaying events.

### 4. Router
Declares which command goes to which aggregate identified by which field.

### 5. Projection
A read-model updater. It subscribes to the event stream and maintains a query-friendly table. Projections are eventually consistent.

### 6. Event store
The append-only log where events are persisted per stream (one stream per aggregate instance).

## Design decisions

- **Option A — aggregate per cart (stream per cart)**: small streams, fast replay, isolated concurrency. Con: many streams.
- **Option B — single "carts" stream with aggregate id in event payload**: easy to subscribe globally. Con: every cart command contends on the same stream version; hot spot.

→ Option A. Streams are cheap; contention is expensive.

- **Option A — projection uses Ecto.Multi to update the read model and the projection offset atomically**: exactly-once semantics. Con: Ecto-coupled.
- **Option B — projection updates read model, then acks**: simpler. Con: at-least-once; you must make every handler idempotent.

→ Option A via `Commanded.Projections.Ecto`, which wraps both updates in a single transaction using a projection versions table.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:commanded, "~> 1.4"},
    {:commanded_eventstore_adapter, "~> 1.4"},
    {:commanded_ecto_projections, "~> 1.4"},
    {:eventstore, "~> 1.4"},
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 1: Commanded application

**Objective**: Implement: Commanded application.

```elixir
defmodule ShoppingCartEs.CommandedApp do
  use Commanded.Application,
    otp_app: :shopping_cart_es,
    event_store: [
      adapter: Commanded.EventStore.Adapters.EventStore,
      event_store: ShoppingCartEs.EventStore
    ]

  router ShoppingCartEs.Router
end

defmodule ShoppingCartEs.EventStore do
  use EventStore, otp_app: :shopping_cart_es
end
```

### Step 2: Commands and events

**Objective**: Define imperative Commands and immutable Events as plain structs enforcing schema via @enforce_keys.

```elixir
defmodule ShoppingCartEs.Cart.Commands do
  defmodule AddItem do
    @enforce_keys [:cart_id, :sku, :quantity, :unit_price_cents]
    defstruct [:cart_id, :sku, :quantity, :unit_price_cents]
  end

  defmodule RemoveItem do
    @enforce_keys [:cart_id, :sku]
    defstruct [:cart_id, :sku]
  end

  defmodule Checkout do
    @enforce_keys [:cart_id]
    defstruct [:cart_id]
  end
end

defmodule ShoppingCartEs.Cart.Events do
  defmodule ItemAdded do
    @derive Jason.Encoder
    defstruct [:cart_id, :sku, :quantity, :unit_price_cents]
  end

  defmodule ItemRemoved do
    @derive Jason.Encoder
    defstruct [:cart_id, :sku]
  end

  defmodule CartCheckedOut do
    @derive Jason.Encoder
    defstruct [:cart_id, :total_cents, :item_count]
  end
end
```

### Step 3: Aggregate

**Objective**: Code execute/2 for command validation and apply/2 for event replay—both side-effect-free state machines.

```elixir
defmodule ShoppingCartEs.Cart.Aggregate do
  alias ShoppingCartEs.Cart.Commands.{AddItem, RemoveItem, Checkout}
  alias ShoppingCartEs.Cart.Events.{ItemAdded, ItemRemoved, CartCheckedOut}

  defstruct cart_id: nil,
            items: %{},
            status: :open

  # --- execute: command → event(s) or error ---

  def execute(%__MODULE__{status: :checked_out}, _cmd),
    do: {:error, :cart_already_checked_out}

  def execute(%__MODULE__{}, %AddItem{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  def execute(%__MODULE__{} = _cart, %AddItem{} = cmd) do
    %ItemAdded{
      cart_id: cmd.cart_id,
      sku: cmd.sku,
      quantity: cmd.quantity,
      unit_price_cents: cmd.unit_price_cents
    }
  end

  def execute(%__MODULE__{items: items}, %RemoveItem{sku: sku}) when not is_map_key(items, sku),
    do: {:error, :item_not_in_cart}

  def execute(%__MODULE__{} = _cart, %RemoveItem{} = cmd) do
    %ItemRemoved{cart_id: cmd.cart_id, sku: cmd.sku}
  end

  def execute(%__MODULE__{items: items}, %Checkout{}) when map_size(items) == 0,
    do: {:error, :cart_is_empty}

  def execute(%__MODULE__{items: items, cart_id: id}, %Checkout{}) do
    total =
      Enum.reduce(items, 0, fn {_sku, %{quantity: q, unit_price_cents: p}}, acc ->
        acc + q * p
      end)

    %CartCheckedOut{cart_id: id, total_cents: total, item_count: map_size(items)}
  end

  # --- apply: event → new state ---

  def apply(%__MODULE__{} = state, %ItemAdded{} = ev) do
    %{
      state
      | cart_id: ev.cart_id,
        items:
          Map.update(
            state.items,
            ev.sku,
            %{quantity: ev.quantity, unit_price_cents: ev.unit_price_cents},
            fn existing -> %{existing | quantity: existing.quantity + ev.quantity} end
          )
    }
  end

  def apply(%__MODULE__{} = state, %ItemRemoved{} = ev) do
    %{state | items: Map.delete(state.items, ev.sku)}
  end

  def apply(%__MODULE__{} = state, %CartCheckedOut{}) do
    %{state | status: :checked_out}
  end
end
```

### Step 4: Router

**Objective**: Register command types with the router and set aggregate identity/lifespan policy for command dispatch.

```elixir
defmodule ShoppingCartEs.Router do
  use Commanded.Commands.Router

  alias ShoppingCartEs.Cart.Aggregate
  alias ShoppingCartEs.Cart.Commands.{AddItem, RemoveItem, Checkout}

  identify(Aggregate, by: :cart_id, prefix: "cart-")

  dispatch([AddItem, RemoveItem, Checkout],
    to: Aggregate,
    lifespan: ShoppingCartEs.Cart.Lifespan
  )
end

defmodule ShoppingCartEs.Cart.Lifespan do
  @behaviour Commanded.Aggregates.AggregateLifespan

  # Hibernate the aggregate process after 5 minutes of inactivity
  def after_event(_event), do: :timer.minutes(5)
  def after_command(_command), do: :timer.minutes(5)
  def after_error(_error), do: :stop
end
```

### Step 5: Projection

**Objective**: Implement: Projection.

```elixir
defmodule ShoppingCartEs.Projections.CartSummaryProjector do
  use Commanded.Projections.Ecto,
    application: ShoppingCartEs.CommandedApp,
    name: "cart_summary_projector",
    repo: ShoppingCartEs.Repo

  alias ShoppingCartEs.Cart.Events.{ItemAdded, ItemRemoved, CartCheckedOut}

  project(%ItemAdded{} = ev, _metadata, fn multi ->
    Ecto.Multi.run(multi, :upsert, fn repo, _ ->
      {:ok,
       repo.insert_all(
         "cart_summaries",
         [
           %{
             cart_id: ev.cart_id,
             item_count: ev.quantity,
             total_cents: ev.quantity * ev.unit_price_cents,
             status: "open",
             updated_at: DateTime.utc_now()
           }
         ],
         on_conflict: {:replace, [:item_count, :total_cents, :updated_at]},
         conflict_target: [:cart_id]
       )}
    end)
  end)

  project(%ItemRemoved{cart_id: id}, _metadata, fn multi ->
    Ecto.Multi.run(multi, :decrement, fn repo, _ ->
      {count, _} =
        repo.update_all(
          "cart_summaries",
          [set: [updated_at: DateTime.utc_now()]],
          returning: false
        )

      {:ok, %{cart_id: id, touched: count}}
    end)
  end)

  project(%CartCheckedOut{} = ev, _metadata, fn multi ->
    Ecto.Multi.run(multi, :finalize, fn repo, _ ->
      {:ok,
       repo.update_all(
         "cart_summaries",
         [set: [status: "checked_out", total_cents: ev.total_cents]],
         returning: false
       )}
    end)
  end)
end
```

### Step 6: Migration

**Objective**: Define the database migration: Migration.

```elixir
defmodule ShoppingCartEs.Repo.Migrations.CreateCartSummaries do
  use Ecto.Migration

  def change do
    create table(:cart_summaries, primary_key: false) do
      add :cart_id, :string, primary_key: true
      add :item_count, :integer, null: false, default: 0
      add :total_cents, :integer, null: false, default: 0
      add :status, :string, null: false, default: "open"
      add :updated_at, :utc_datetime_usec, null: false
    end
  end
end
```

### Step 7: Config

**Objective**: Configure the runtime wiring for: Config.

```elixir
# config/config.exs
import Config

config :shopping_cart_es,
  ecto_repos: [ShoppingCartEs.Repo],
  event_stores: [ShoppingCartEs.EventStore]

config :shopping_cart_es, ShoppingCartEs.EventStore,
  serializer: Commanded.Serialization.JsonSerializer,
  username: "postgres",
  password: "postgres",
  database: "shopping_cart_es_eventstore_#{Mix.env()}",
  hostname: "localhost",
  pool_size: 10
```

## Command flow diagram

```
HTTP / CLI
   │
   ▼
ShoppingCartEs.CommandedApp.dispatch(%AddItem{cart_id: "c1", ...})
   │
   ▼
Router.identify →  aggregate id = "cart-c1"
   │
   ▼
Aggregate process (singleton per cart_id)
   │  1. load state from event store (replay)
   │  2. execute(state, command) → event
   │  3. append event to stream "cart-c1"
   │  4. apply(state, event) → new state (in memory)
   │
   ▼
Event persisted  ─────────────▶ CartSummaryProjector
                                  │
                                  ▼
                              cart_summaries table
```

## Tests

```elixir
defmodule ShoppingCartEs.Cart.AggregateTest do
  use ExUnit.Case, async: true

  alias ShoppingCartEs.Cart.Aggregate
  alias ShoppingCartEs.Cart.Commands.{AddItem, RemoveItem, Checkout}
  alias ShoppingCartEs.Cart.Events.{ItemAdded, ItemRemoved, CartCheckedOut}

  describe "AddItem" do
    test "emits ItemAdded on an open cart" do
      cmd = %AddItem{cart_id: "c1", sku: "SKU1", quantity: 2, unit_price_cents: 500}

      assert %ItemAdded{sku: "SKU1", quantity: 2} =
               Aggregate.execute(%Aggregate{}, cmd)
    end

    test "rejects non-positive quantity" do
      cmd = %AddItem{cart_id: "c1", sku: "SKU1", quantity: 0, unit_price_cents: 500}
      assert {:error, :quantity_must_be_positive} = Aggregate.execute(%Aggregate{}, cmd)
    end
  end

  describe "RemoveItem" do
    test "emits ItemRemoved when the item exists" do
      state = apply_events(%Aggregate{}, [%ItemAdded{cart_id: "c1", sku: "SKU1", quantity: 1, unit_price_cents: 100}])
      assert %ItemRemoved{sku: "SKU1"} = Aggregate.execute(state, %RemoveItem{cart_id: "c1", sku: "SKU1"})
    end

    test "rejects removing an item not in the cart" do
      assert {:error, :item_not_in_cart} =
               Aggregate.execute(%Aggregate{}, %RemoveItem{cart_id: "c1", sku: "missing"})
    end
  end

  describe "Checkout" do
    test "computes total from current items" do
      state =
        apply_events(%Aggregate{}, [
          %ItemAdded{cart_id: "c1", sku: "A", quantity: 2, unit_price_cents: 500},
          %ItemAdded{cart_id: "c1", sku: "B", quantity: 1, unit_price_cents: 300}
        ])

      assert %CartCheckedOut{total_cents: 1_300, item_count: 2} =
               Aggregate.execute(state, %Checkout{cart_id: "c1"})
    end

    test "rejects checkout of empty cart" do
      assert {:error, :cart_is_empty} =
               Aggregate.execute(%Aggregate{}, %Checkout{cart_id: "c1"})
    end

    test "rejects any command after checkout" do
      state =
        apply_events(%Aggregate{}, [
          %ItemAdded{cart_id: "c1", sku: "A", quantity: 1, unit_price_cents: 100},
          %CartCheckedOut{cart_id: "c1", total_cents: 100, item_count: 1}
        ])

      assert {:error, :cart_already_checked_out} =
               Aggregate.execute(state, %AddItem{cart_id: "c1", sku: "X", quantity: 1, unit_price_cents: 1})
    end
  end

  defp apply_events(state, events),
    do: Enum.reduce(events, state, &Aggregate.apply(&2, &1))
end
```

## Benchmark

```elixir
# bench/dispatch_bench.exs
cart_id = Ecto.UUID.generate()

Benchee.run(
  %{
    "dispatch AddItem (warm)" => fn ->
      ShoppingCartEs.CommandedApp.dispatch(%ShoppingCartEs.Cart.Commands.AddItem{
        cart_id: cart_id,
        sku: "SKU1",
        quantity: 1,
        unit_price_cents: 500
      })
    end
  },
  time: 5,
  warmup: 2
)
```

Target on a single-node setup: p50 < 2ms per command including event store append. Above 10ms usually points at an un-pooled event store connection or Postgres fsync on every append.

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

**1. Event schema is forever**
Once an event is in production, its shape is part of the log forever. Renaming fields or changing types requires an up-caster or an event version. Never `@derive`-mutate a struct after release.

**2. Projections are eventually consistent**
The command returns success when the *event* is persisted, not when the projection has caught up. UI code that reads `cart_summaries` right after dispatch will often miss the update. Offer a "read your own writes" path (query the aggregate directly) or use `:strong_consistency` dispatch option.

**3. Aggregate state must fit in memory**
The aggregate process holds the full state after replay. If a cart can accumulate 10k add/remove cycles, replay will be slow and memory will grow. Cap the lifetime (checkout or expire), and consider snapshots for long-lived aggregates.

**4. `:stop` lifespan on error hides errors**
Returning `:stop` from `after_error` terminates the aggregate process. The next command reloads from the event store — slow but correct. Returning `:infinity` keeps a poisoned state in memory; debugging is harder. Prefer `:stop`.

**5. When NOT to use Commanded**
For short-lived CRUD entities with no audit requirement, Commanded is overkill. Use Ash or plain Ecto contexts until the event log has real business value.

## Reflection

The projection is eventually consistent and may lag by seconds on a busy node. If the UI always dispatches `AddItem` and then immediately reads `cart_summaries`, users will see stale data intermittently. Would you fix this by (a) querying the aggregate directly via `Commanded.aggregate_state/3`, (b) introducing `:strong_consistency` dispatch, or (c) designing the UI to be optimistic? What are the failure modes of each?

## Executable Example

```elixir
defp deps do
  [
    {:commanded, "~> 1.4"},
    {:commanded_eventstore_adapter, "~> 1.4"},
    {:commanded_ecto_projections, "~> 1.4"},
    {:eventstore, "~> 1.4"},
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:jason, "~> 1.4"}
  ]


defmodule ShoppingCartEs.CommandedApp do
  use Commanded.Application,
    otp_app: :shopping_cart_es,
    event_store: [
      adapter: Commanded.EventStore.Adapters.EventStore,
      event_store: ShoppingCartEs.EventStore
    ]

  router ShoppingCartEs.Router
end

defmodule ShoppingCartEs.EventStore do
  use EventStore, otp_app: :shopping_cart_es
end


defmodule ShoppingCartEs.Cart.Commands do
  defmodule AddItem do
    @enforce_keys [:cart_id, :sku, :quantity, :unit_price_cents]
    defstruct [:cart_id, :sku, :quantity, :unit_price_cents]
  end

  defmodule RemoveItem do
    @enforce_keys [:cart_id, :sku]
    defstruct [:cart_id, :sku]
  end

  defmodule Checkout do
    @enforce_keys [:cart_id]
    defstruct [:cart_id]
  end
end

defmodule ShoppingCartEs.Cart.Events do
  defmodule ItemAdded do
    @derive Jason.Encoder
    defstruct [:cart_id, :sku, :quantity, :unit_price_cents]
  end

  defmodule ItemRemoved do
    @derive Jason.Encoder
    defstruct [:cart_id, :sku]
  end

  defmodule CartCheckedOut do
    @derive Jason.Encoder
    defstruct [:cart_id, :total_cents, :item_count]
  end
end


defmodule ShoppingCartEs.Cart.Aggregate do
  alias ShoppingCartEs.Cart.Commands.{AddItem, RemoveItem, Checkout}
  alias ShoppingCartEs.Cart.Events.{ItemAdded, ItemRemoved, CartCheckedOut}

  defstruct cart_id: nil,
            items: %{},
            status: :open

  # --- execute: command → event(s) or error ---

  def execute(%__MODULE__{status: :checked_out}, _cmd),
    do: {:error, :cart_already_checked_out}

  def execute(%__MODULE__{}, %AddItem{quantity: q}) when q <= 0,
    do: {:error, :quantity_must_be_positive}

  def execute(%__MODULE__{} = _cart, %AddItem{} = cmd) do
    %ItemAdded{
      cart_id: cmd.cart_id,
      sku: cmd.sku,
      quantity: cmd.quantity,
      unit_price_cents: cmd.unit_price_cents
    }
  end

  def execute(%__MODULE__{items: items}, %RemoveItem{sku: sku}) when not is_map_key(items, sku),
    do: {:error, :item_not_in_cart}

  def execute(%__MODULE__{} = _cart, %RemoveItem{} = cmd) do
    %ItemRemoved{cart_id: cmd.cart_id, sku: cmd.sku}
  end

  def execute(%__MODULE__{items: items}, %Checkout{}) when map_size(items) == 0,
    do: {:error, :cart_is_empty}

  def execute(%__MODULE__{items: items, cart_id: id}, %Checkout{}) do
    total =
      Enum.reduce(items, 0, fn {_sku, %{quantity: q, unit_price_cents: p}}, acc ->
        acc + q * p
      end)

    %CartCheckedOut{cart_id: id, total_cents: total, item_count: map_size(items)}
  end

  # --- apply: event → new state ---

  def apply(%__MODULE__{} = state, %ItemAdded{} = ev) do
    %{
      state
      | cart_id: ev.cart_id,
        items:
          Map.update(
            state.items,
            ev.sku,
            %{quantity: ev.quantity, unit_price_cents: ev.unit_price_cents},
            fn existing -> %{existing | quantity: existing.quantity + ev.quantity} end
          )
    }
  end

  def apply(%__MODULE__{} = state, %ItemRemoved{} = ev) do
    %{state | items: Map.delete(state.items, ev.sku)}
  end

  def apply(%__MODULE__{} = state, %CartCheckedOut{}) do
    %{state | status: :checked_out}
  end
end


defmodule ShoppingCartEs.Router do
  use Commanded.Commands.Router

  alias ShoppingCartEs.Cart.Aggregate
  alias ShoppingCartEs.Cart.Commands.{AddItem, RemoveItem, Checkout}

  identify(Aggregate, by: :cart_id, prefix: "cart-")

  dispatch([AddItem, RemoveItem, Checkout],
    to: Aggregate,
    lifespan: ShoppingCartEs.Cart.Lifespan
  )
end

defmodule ShoppingCartEs.Cart.Lifespan do
  @behaviour Commanded.Aggregates.AggregateLifespan

  # Hibernate the aggregate process after 5 minutes of inactivity
  def after_event(_event), do: :timer.minutes(5)
  def after_command(_command), do: :timer.minutes(5)
  def after_error(_error), do: :stop
end


defmodule ShoppingCartEs.Projections.CartSummaryProjector do
  use Commanded.Projections.Ecto,
    application: ShoppingCartEs.CommandedApp,
    name: "cart_summary_projector",
    repo: ShoppingCartEs.Repo

  alias ShoppingCartEs.Cart.Events.{ItemAdded, ItemRemoved, CartCheckedOut}

  project(%ItemAdded{} = ev, _metadata, fn multi ->
    Ecto.Multi.run(multi, :upsert, fn repo, _ ->
      {:ok,
       repo.insert_all(
         "cart_summaries",
         [
           %{
             cart_id: ev.cart_id,
             item_count: ev.quantity,
             total_cents: ev.quantity * ev.unit_price_cents,
             status: "open",
             updated_at: DateTime.utc_now()
           }
         ],
         on_conflict: {:replace, [:item_count, :total_cents, :updated_at]},
         conflict_target: [:cart_id]
       )}
    end)
  end)

  project(%ItemRemoved{cart_id: id}, _metadata, fn multi ->
    Ecto.Multi.run(multi, :decrement, fn repo, _ ->
      {count, _} =
        repo.update_all(
          "cart_summaries",
          [set: [updated_at: DateTime.utc_now()]],
          returning: false
        )

      {:ok, %{cart_id: id, touched: count}}
    end)
  end)

  project(%CartCheckedOut{} = ev, _metadata, fn multi ->
    Ecto.Multi.run(multi, :finalize, fn repo, _ ->
      {:ok,
       repo.update_all(
         "cart_summaries",
         [set: [status: "checked_out", total_cents: ev.total_cents]],
         returning: false
       )}
    end)
  end)
end


defmodule ShoppingCartEs.Repo.Migrations.CreateCartSummaries do
  use Ecto.Migration

  def change do
    create table(:cart_summaries, primary_key: false) do
      add :cart_id, :string, primary_key: true
      add :item_count, :integer, null: false, default: 0
      add :total_cents, :integer, null: false, default: 0
      add :status, :string, null: false, default: "open"
      add :updated_at, :utc_datetime_usec, null: false
    end
  end
end


# config/config.exs
import Config

config :shopping_cart_es,
  ecto_repos: [ShoppingCartEs.Repo],
  event_stores: [ShoppingCartEs.EventStore]

config :shopping_cart_es, ShoppingCartEs.EventStore,
  serializer: Commanded.Serialization.JsonSerializer,
  username: "postgres",
  password: "postgres",
  database: "shopping_cart_es_eventstore_#{Mix.env()}",
  hostname: "localhost",
  pool_size: 10

defmodule Main do
  def main do
      :ok
  end
end

Main.main()
```
