# CQRS with Commanded and EventStore

**Project**: `ledger_cqrs` — double-entry ledger where the write model (account aggregate) and the read model (account balances view) are fully separated, using Commanded dispatch on the write side and Ecto queries on the read side.

## Project context

You're implementing a small double-entry ledger that backs a wallet-style product. The write side must enforce invariants ("no negative balance", "debit equals credit"), and the read side must serve sub-millisecond balance lookups for checkout pages. A single normalized model — an `accounts` table updated in-place — fails both goals: it cannot hold the audit trail that regulators demand, and the same rows get locked for both reads and writes.

CQRS (Command/Query Responsibility Segregation) splits those two concerns into two physical models. The write model is a Commanded aggregate backed by an event store. The read model is a plain Postgres table updated by a projection. Commands go to the aggregate. Queries go to the read model. They never share a row lock.

```
ledger_cqrs/
├── lib/
│   └── ledger_cqrs/
│       ├── application.ex
│       ├── app.ex                          # Commanded application
│       ├── router.ex                       # command router
│       ├── write/                          # command side
│       │   ├── account.ex                  # aggregate
│       │   ├── commands.ex                 # OpenAccount, Debit, Credit, Transfer
│       │   ├── events.ex                   # AccountOpened, Debited, Credited
│       │   └── process_managers/
│       │       └── transfer.ex             # saga: Debit source → Credit target
│       ├── read/                           # query side
│       │   ├── balance_projector.ex
│       │   ├── balance.ex                  # Ecto schema
│       │   └── queries.ex                  # public read API
│       └── repo.ex
├── priv/repo/migrations/
│   └── 20260412_create_balances.exs
├── test/
│   └── ledger_cqrs/
│       └── transfer_flow_test.exs
├── config/config.exs
└── mix.exs
```

## Why CQRS on a ledger

A ledger has asymmetric needs. Writes are infrequent but must be serialized per account to preserve invariants. Reads are very frequent and only need the current balance. CRUD tries to satisfy both with one schema; under load each side slows the other. CQRS lets the read side be denormalized, indexed for the query, and replicated to read replicas without any concern for write contention.

## Why process managers for transfers

A transfer is "debit A, credit B, atomically". Across aggregates, true atomicity requires distributed transactions — a well-known failure source. A process manager (saga) orchestrates a sequence of commands with compensation: if `Credit` fails, it issues a compensating `Credit` back to the source. It buys eventual atomicity without 2PC.

## Core concepts

### 1. Write model
Aggregates + events persisted to the event store. The source of truth.

### 2. Read model
One or more projections materialized from the event stream, tuned for specific queries.

### 3. Command handler
In Commanded it is `Aggregate.execute/2`; it validates commands against aggregate state and emits events.

### 4. Process manager
A long-running coordinator that reacts to events by dispatching new commands. Manages its own state in the event store.

### 5. Consistency boundary
An aggregate is the boundary of strong consistency. Anything spanning aggregates is eventually consistent through process managers.

## Design decisions

- **Option A — single `Account` aggregate with `Transfer` process manager**: each account is its own stream, clean isolation.
- **Option B — single `Transfer` aggregate**: atomic per transfer. Con: balances need a derived view, and each account would have events scattered across many streams.

→ A. Streams per account is the natural boundary and matches how we query balances.

- **Option A — balance projection as `insert_all ... on_conflict: :replace`**: simple, idempotent on replay.
- **Option B — event handler that mutates state in-place**: simpler code. Con: replay corrupts balances because handlers must be idempotent.

→ A. Projections must tolerate replay.

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

### Step 1: Commands and events

**Objective**: Implement: Commands and events.

```elixir
defmodule LedgerCqrs.Write.Commands do
  defmodule OpenAccount do
    @enforce_keys [:account_id, :owner]
    defstruct [:account_id, :owner]
  end

  defmodule Debit do
    @enforce_keys [:account_id, :amount_cents, :transfer_id]
    defstruct [:account_id, :amount_cents, :transfer_id]
  end

  defmodule Credit do
    @enforce_keys [:account_id, :amount_cents, :transfer_id]
    defstruct [:account_id, :amount_cents, :transfer_id]
  end

  defmodule StartTransfer do
    @enforce_keys [:transfer_id, :source_id, :target_id, :amount_cents]
    defstruct [:transfer_id, :source_id, :target_id, :amount_cents]
  end
end

defmodule LedgerCqrs.Write.Events do
  defmodule AccountOpened do
    @derive Jason.Encoder
    defstruct [:account_id, :owner]
  end

  defmodule Debited do
    @derive Jason.Encoder
    defstruct [:account_id, :amount_cents, :transfer_id, :new_balance]
  end

  defmodule Credited do
    @derive Jason.Encoder
    defstruct [:account_id, :amount_cents, :transfer_id, :new_balance]
  end

  defmodule TransferRequested do
    @derive Jason.Encoder
    defstruct [:transfer_id, :source_id, :target_id, :amount_cents]
  end

  defmodule DebitFailed do
    @derive Jason.Encoder
    defstruct [:transfer_id, :account_id, :reason]
  end
end
```

### Step 2: Account aggregate

**Objective**: Code execute/2 for debit-credit invariants and apply/2 to fold events into account balance.

```elixir
defmodule LedgerCqrs.Write.Account do
  alias LedgerCqrs.Write.Commands.{OpenAccount, Debit, Credit}
  alias LedgerCqrs.Write.Events.{AccountOpened, Debited, Credited, DebitFailed}

  defstruct account_id: nil, owner: nil, balance: 0, opened?: false

  def execute(%__MODULE__{opened?: false}, %OpenAccount{} = cmd),
    do: %AccountOpened{account_id: cmd.account_id, owner: cmd.owner}

  def execute(%__MODULE__{opened?: true}, %OpenAccount{}),
    do: {:error, :account_already_open}

  def execute(%__MODULE__{opened?: false}, _),
    do: {:error, :account_not_open}

  def execute(%__MODULE__{balance: bal}, %Debit{amount_cents: amt, transfer_id: tid, account_id: aid})
      when amt > bal do
    %DebitFailed{transfer_id: tid, account_id: aid, reason: :insufficient_funds}
  end

  def execute(%__MODULE__{} = state, %Debit{} = cmd) do
    %Debited{
      account_id: cmd.account_id,
      amount_cents: cmd.amount_cents,
      transfer_id: cmd.transfer_id,
      new_balance: state.balance - cmd.amount_cents
    }
  end

  def execute(%__MODULE__{} = state, %Credit{} = cmd) do
    %Credited{
      account_id: cmd.account_id,
      amount_cents: cmd.amount_cents,
      transfer_id: cmd.transfer_id,
      new_balance: state.balance + cmd.amount_cents
    }
  end

  # apply

  def apply(%__MODULE__{} = state, %AccountOpened{} = ev),
    do: %{state | account_id: ev.account_id, owner: ev.owner, opened?: true}

  def apply(%__MODULE__{} = state, %Debited{new_balance: nb}),
    do: %{state | balance: nb}

  def apply(%__MODULE__{} = state, %Credited{new_balance: nb}),
    do: %{state | balance: nb}

  def apply(%__MODULE__{} = state, %DebitFailed{}), do: state
end
```

### Step 3: Transfer process manager

**Objective**: Route multi-aggregate sagas: TransferRequested → Debit source → Credit target with failure compensation.

```elixir
defmodule LedgerCqrs.Write.ProcessManagers.Transfer do
  use Commanded.ProcessManagers.ProcessManager,
    application: LedgerCqrs.App,
    name: "TransferProcessManager"

  alias LedgerCqrs.Write.Commands.{Debit, Credit}
  alias LedgerCqrs.Write.Events.{TransferRequested, Debited, Credited, DebitFailed}

  defstruct [:transfer_id, :source_id, :target_id, :amount_cents, :status]

  # --- interested: start / continue / stop routing ---

  def interested?(%TransferRequested{transfer_id: id}), do: {:start, id}
  def interested?(%Debited{transfer_id: id}) when not is_nil(id), do: {:continue, id}
  def interested?(%Credited{transfer_id: id}) when not is_nil(id), do: {:stop, id}
  def interested?(%DebitFailed{transfer_id: id}), do: {:stop, id}
  def interested?(_), do: false

  # --- handle: event → command ---

  def handle(%__MODULE__{}, %TransferRequested{} = ev) do
    %Debit{
      account_id: ev.source_id,
      amount_cents: ev.amount_cents,
      transfer_id: ev.transfer_id
    }
  end

  def handle(%__MODULE__{} = pm, %Debited{}) do
    %Credit{
      account_id: pm.target_id,
      amount_cents: pm.amount_cents,
      transfer_id: pm.transfer_id
    }
  end

  # --- apply: event → pm state ---

  def apply(%__MODULE__{} = pm, %TransferRequested{} = ev) do
    %{pm | transfer_id: ev.transfer_id, source_id: ev.source_id,
           target_id: ev.target_id, amount_cents: ev.amount_cents, status: :requested}
  end

  def apply(%__MODULE__{} = pm, %Debited{}), do: %{pm | status: :debited}
  def apply(%__MODULE__{} = pm, %Credited{}), do: %{pm | status: :completed}
  def apply(%__MODULE__{} = pm, %DebitFailed{}), do: %{pm | status: :failed}
end
```

### Step 4: Router

**Objective**: Wire HTTP routes for: Router.

```elixir
defmodule LedgerCqrs.Router do
  use Commanded.Commands.Router

  alias LedgerCqrs.Write.Account
  alias LedgerCqrs.Write.Commands.{OpenAccount, Debit, Credit, StartTransfer}
  alias LedgerCqrs.Write.Events.TransferRequested

  identify(Account, by: :account_id, prefix: "account-")

  dispatch([OpenAccount, Debit, Credit], to: Account)

  # StartTransfer dispatches as an event on a "transfers" stream via a middleware or handler.
  # For simplicity, we emit TransferRequested directly.
  middleware(LedgerCqrs.Write.StartTransferMiddleware)

  dispatch(StartTransfer, to: LedgerCqrs.Write.TransferEmitter, identity: :transfer_id)
end
```

### Step 5: Balance projection

**Objective**: Implement: Balance projection.

```elixir
defmodule LedgerCqrs.Read.BalanceProjector do
  use Commanded.Projections.Ecto,
    application: LedgerCqrs.App,
    name: "balance_projector",
    repo: LedgerCqrs.Repo

  alias LedgerCqrs.Write.Events.{AccountOpened, Debited, Credited}

  project(%AccountOpened{} = ev, _meta, fn multi ->
    Ecto.Multi.insert_all(
      multi,
      :open,
      "balances",
      [%{account_id: ev.account_id, owner: ev.owner, balance_cents: 0,
         updated_at: DateTime.utc_now()}],
      on_conflict: :nothing,
      conflict_target: [:account_id]
    )
  end)

  project(%Debited{account_id: id, new_balance: nb}, _meta, fn multi ->
    Ecto.Multi.update_all(
      multi,
      :debit,
      from(b in "balances", where: b.account_id == ^id),
      set: [balance_cents: nb, updated_at: DateTime.utc_now()]
    )
  end)

  project(%Credited{account_id: id, new_balance: nb}, _meta, fn multi ->
    Ecto.Multi.update_all(
      multi,
      :credit,
      from(b in "balances", where: b.account_id == ^id),
      set: [balance_cents: nb, updated_at: DateTime.utc_now()]
    )
  end)

  import Ecto.Query
end
```

### Step 6: Read queries

**Objective**: Implement: Read queries.

```elixir
defmodule LedgerCqrs.Read.Queries do
  import Ecto.Query
  alias LedgerCqrs.Repo

  def balance_of(account_id) do
    Repo.one(from b in "balances", where: b.account_id == ^account_id, select: b.balance_cents)
  end

  def top_balances(limit \\ 10) do
    Repo.all(
      from b in "balances",
        order_by: [desc: b.balance_cents],
        limit: ^limit,
        select: %{account_id: b.account_id, balance_cents: b.balance_cents}
    )
  end
end
```

### Step 7: Migration

**Objective**: Define the database migration: Migration.

```elixir
defmodule LedgerCqrs.Repo.Migrations.CreateBalances do
  use Ecto.Migration

  def change do
    create table(:balances, primary_key: false) do
      add :account_id, :string, primary_key: true
      add :owner, :string, null: false
      add :balance_cents, :bigint, null: false, default: 0
      add :updated_at, :utc_datetime_usec, null: false
    end

    create index(:balances, [:balance_cents])
  end
end
```

## Write-vs-read flow

```
          COMMAND SIDE                              QUERY SIDE
┌──────────────────────────────┐         ┌───────────────────────────────┐
│  StartTransfer  ──▶ Router   │         │                               │
│                     │        │         │                               │
│                     ▼        │         │                               │
│            TransferRequested │  event  │                               │
│                     │        │ stream  │                               │
│                     ▼        │────────▶│  BalanceProjector             │
│          Transfer process mgr │         │        │                      │
│                     │        │         │        ▼                      │
│                  Debit ─▶    │         │   balances table              │
│                 Account A    │         │        │                      │
│                    │ Debited │         │        ▼                      │
│                    ▼         │         │  LedgerCqrs.Read.Queries      │
│                  Credit ─▶   │         │                               │
│                 Account B    │         │                               │
│                   Credited   │         │                               │
└──────────────────────────────┘         └───────────────────────────────┘
```

## Tests

```elixir
defmodule LedgerCqrs.TransferFlowTest do
  use ExUnit.Case, async: false
  import Commanded.Assertions.EventAssertions

  alias LedgerCqrs.App
  alias LedgerCqrs.Write.Commands.{OpenAccount, Credit, StartTransfer}
  alias LedgerCqrs.Write.Events.{Credited, Debited, DebitFailed}

  describe "successful transfer" do
    test "debits source and credits target" do
      src = "src-" <> Ecto.UUID.generate()
      tgt = "tgt-" <> Ecto.UUID.generate()
      tid = Ecto.UUID.generate()

      :ok = App.dispatch(%OpenAccount{account_id: src, owner: "alice"})
      :ok = App.dispatch(%OpenAccount{account_id: tgt, owner: "bob"})
      :ok = App.dispatch(%Credit{account_id: src, amount_cents: 1_000, transfer_id: "seed-#{src}"})

      :ok = App.dispatch(%StartTransfer{transfer_id: tid, source_id: src, target_id: tgt, amount_cents: 400})

      assert_receive_event(App, Debited, fn ev -> ev.transfer_id == tid end, timeout: 2_000)
      assert_receive_event(App, Credited, fn ev -> ev.transfer_id == tid end, timeout: 2_000)
    end
  end

  describe "insufficient funds" do
    test "emits DebitFailed and does not credit target" do
      src = "src-" <> Ecto.UUID.generate()
      tgt = "tgt-" <> Ecto.UUID.generate()
      tid = Ecto.UUID.generate()

      :ok = App.dispatch(%OpenAccount{account_id: src, owner: "alice"})
      :ok = App.dispatch(%OpenAccount{account_id: tgt, owner: "bob"})

      :ok = App.dispatch(%StartTransfer{transfer_id: tid, source_id: src, target_id: tgt, amount_cents: 500})

      assert_receive_event(App, DebitFailed, fn ev -> ev.transfer_id == tid end, timeout: 2_000)
    end
  end
end
```

## Benchmark

```elixir
# bench/read_vs_write_bench.exs
account_id = "bench-" <> Ecto.UUID.generate()
:ok = LedgerCqrs.App.dispatch(%LedgerCqrs.Write.Commands.OpenAccount{account_id: account_id, owner: "bench"})
:ok = LedgerCqrs.App.dispatch(%LedgerCqrs.Write.Commands.Credit{account_id: account_id, amount_cents: 100_000, transfer_id: "seed"})
Process.sleep(200)

Benchee.run(
  %{
    "read balance_of (projection)" => fn -> LedgerCqrs.Read.Queries.balance_of(account_id) end,
    "write Debit (dispatch)" => fn ->
      LedgerCqrs.App.dispatch(%LedgerCqrs.Write.Commands.Debit{
        account_id: account_id,
        amount_cents: 1,
        transfer_id: "t-#{:erlang.unique_integer([:positive])}"
      })
    end
  },
  time: 5,
  warmup: 2
)
```

Expected on a single-node, warm pool: read p50 < 400µs, write p50 1–3ms. The asymmetry is the point: reads are free of the event-store round trip.

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

**1. Read lag is not a bug**
Between command ack and projection update there is a window (typically < 100ms on a single node, seconds on a busy one). UX must tolerate it: optimistic UI, or read-your-own-writes via strong consistency dispatch.

**2. Process managers can live forever**
If your `interested?` never returns `:stop`, the PM state grows until it is trimmed. Always define terminal events.

**3. Missing idempotency in projections**
A replay will re-apply every event. `update_all` with the new balance is idempotent. `balance = balance - amount` is not. Always project to *the new value*, not a delta.

**4. Exact balances vs computed balances**
`new_balance` in the event couples the projection to the aggregate's view. The alternative — recomputing from events in the projection — is slower but lets you fix historical bugs. Pick based on how tolerant you are to retro edits.

**5. When NOT to use CQRS**
If you have no divergence between read and write needs (same shape, same volumes, no audit), CQRS is ceremony for no return.

## Reflection

The `Transfer` process manager is driven by events; if the BEAM restarts between `Debited` and `Credit`, Commanded resumes from the last checkpoint. What happens if the `Credit` dispatch throws? Where do you see the failure, and how do you ensure the source is not stuck debited forever? Design the compensation path.

## Resources

- [Commanded process managers](https://hexdocs.pm/commanded/process-managers.html)
- [Martin Fowler — CQRS](https://martinfowler.com/bliki/CQRS.html)
- [Greg Young — CQRS and event sourcing (YouTube)](https://www.youtube.com/watch?v=JHGkaShoyNs)
- [EventStore (Elixir) docs](https://hexdocs.pm/eventstore/)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
