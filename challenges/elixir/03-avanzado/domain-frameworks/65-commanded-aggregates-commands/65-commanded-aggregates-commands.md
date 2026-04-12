# Commanded aggregates and commands

**Project**: `commanded_aggregates` — event-sourced bank account aggregate with commands, events and invariants.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

You are joining a fintech that maintains a core banking ledger. The incumbent system is a CRUD Postgres database with a trigger-based audit log — auditors complain it is unreliable and business wants temporal queries ("show me the balance as of 2024-12-31 23:59"). The team decided to rewrite the ledger using CQRS + event sourcing with [Commanded](https://github.com/commanded/commanded), the de-facto Elixir library for this pattern.

Your task is the **write side**: the aggregate that enforces the invariants of a bank account. Projections and read models come in exercise 66. This exercise focuses on the three pillars of the command side: the aggregate module, commands (validated intent), and events (immutable facts). You will learn how Commanded dispatches commands, how aggregate state is rebuilt from events, and why invariants belong in the aggregate — not in the database.

The business rules, distilled after three workshops with the treasury team:

1. An account cannot be debited below a configurable overdraft limit.
2. An account cannot be closed if its balance is non-zero.
3. A closed account rejects any further commands — no zombie writes.
4. Every state transition must be auditable via the event log — no hidden updates.

```
commanded_aggregates/
├── lib/
│   └── commanded_aggregates/
│       ├── application.ex
│       ├── router.ex
│       ├── commands/
│       │   ├── open_account.ex
│       │   ├── deposit.ex
│       │   ├── withdraw.ex
│       │   └── close_account.ex
│       ├── events/
│       │   ├── account_opened.ex
│       │   ├── money_deposited.ex
│       │   ├── money_withdrawn.ex
│       │   └── account_closed.ex
│       └── account.ex
├── test/
│   └── commanded_aggregates/
│       └── account_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Command vs event: intent vs fact

A **command** is a request that may be rejected (`DepositMoney`, imperative, future tense). An **event** is an immutable fact that already happened (`MoneyDeposited`, past tense, cannot be rejected). The aggregate is the only place where commands are validated and converted into events.

```
 Client ──Command──▶ Router ──▶ Aggregate ──Event──▶ EventStore ──▶ Projections
                                    │
                                    └── rejects with {:error, reason}
```

Events are the source of truth. State is a function of events: `state = fold(events)`. This is why replay works: apply all events in order and you get the current state.

### 2. The aggregate lifecycle in Commanded

Commanded spawns one GenServer per aggregate instance (identified by `identity:` in the router). The first time a command targets `account-123`, Commanded:

1. Starts an `Account` process.
2. Loads every event for stream `account-123` from the event store.
3. Applies each event to an empty `%Account{}` via `apply/2`.
4. Passes the command to `execute/2`, which returns an event (or a list) or `{:error, ...}`.
5. Appends the event(s) to the stream atomically.
6. Applies the event to update in-memory state.

Subsequent commands reuse the in-memory state — no rehydration until the process is idle-evicted (default 1 minute).

### 3. `execute/2` vs `apply/2` — the two sides of the aggregate

```elixir
# execute: command → event (may reject)
execute(%Account{status: :closed}, %Deposit{}) ->
  {:error, :account_closed}

# apply: event → state (never fails — events are facts)
apply(%Account{balance: b}, %MoneyDeposited{amount: a}) ->
  %Account{account | balance: b + a}
```

`execute/2` is pure and may fail. `apply/2` is pure and MUST NEVER fail — if it fails during replay, the aggregate is permanently broken and the stream unusable. Never put `raise` or I/O in `apply/2`.

### 4. Invariants live in the aggregate, not in the database

A Postgres `CHECK (balance >= -overdraft)` enforces the invariant at write time, but cannot express cross-event invariants ("cannot close if balance != 0") without triggers or application code. The aggregate centralizes every rule: if `execute/2` returns an event, the rule was satisfied. Read-side projections can trust the events.

### 5. Stream identity = aggregate boundary

Commanded routes commands to aggregates by a key extracted from the command. Every command for `account-123` ends up in the same process, serialized. This gives you **strong consistency per aggregate** without locks — the mailbox is the lock.

Choose your aggregate boundary carefully: too big (`Bank`) and every command serializes through one process. Too small (`Transaction`) and you cannot enforce cross-entity invariants (e.g., balance).

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule CommandedAggregates.MixProject do
  use Mix.Project

  def project do
    [
      app: :commanded_aggregates,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {CommandedAggregates.Application, []}]
  end

  defp deps do
    [
      {:commanded, "~> 1.4"},
      {:jason, "~> 1.4"}
    ]
  end
end
```

### Step 2: Commands — plain structs with enforced keys

```elixir
# lib/commanded_aggregates/commands/open_account.ex
defmodule CommandedAggregates.Commands.OpenAccount do
  @enforce_keys [:account_id, :owner, :overdraft_limit]
  defstruct [:account_id, :owner, :overdraft_limit]

  @type t :: %__MODULE__{
          account_id: String.t(),
          owner: String.t(),
          overdraft_limit: non_neg_integer()
        }
end

# lib/commanded_aggregates/commands/deposit.ex
defmodule CommandedAggregates.Commands.Deposit do
  @enforce_keys [:account_id, :amount]
  defstruct [:account_id, :amount]
end

# lib/commanded_aggregates/commands/withdraw.ex
defmodule CommandedAggregates.Commands.Withdraw do
  @enforce_keys [:account_id, :amount]
  defstruct [:account_id, :amount]
end

# lib/commanded_aggregates/commands/close_account.ex
defmodule CommandedAggregates.Commands.CloseAccount do
  @enforce_keys [:account_id]
  defstruct [:account_id]
end
```

### Step 3: Events — past-tense facts

```elixir
# lib/commanded_aggregates/events/account_opened.ex
defmodule CommandedAggregates.Events.AccountOpened do
  @derive Jason.Encoder
  defstruct [:account_id, :owner, :overdraft_limit, :opened_at]
end

# lib/commanded_aggregates/events/money_deposited.ex
defmodule CommandedAggregates.Events.MoneyDeposited do
  @derive Jason.Encoder
  defstruct [:account_id, :amount, :new_balance, :deposited_at]
end

# lib/commanded_aggregates/events/money_withdrawn.ex
defmodule CommandedAggregates.Events.MoneyWithdrawn do
  @derive Jason.Encoder
  defstruct [:account_id, :amount, :new_balance, :withdrawn_at]
end

# lib/commanded_aggregates/events/account_closed.ex
defmodule CommandedAggregates.Events.AccountClosed do
  @derive Jason.Encoder
  defstruct [:account_id, :closed_at]
end
```

### Step 4: The aggregate — `lib/commanded_aggregates/account.ex`

```elixir
defmodule CommandedAggregates.Account do
  @moduledoc """
  Event-sourced bank account aggregate.

  State is derived from events. `execute/2` validates commands and produces events;
  `apply/2` folds events into state. `apply/2` must never raise — it runs during replay.
  """

  alias CommandedAggregates.Commands.{OpenAccount, Deposit, Withdraw, CloseAccount}
  alias CommandedAggregates.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn, AccountClosed}

  @type status :: :pending | :open | :closed

  @type t :: %__MODULE__{
          account_id: String.t() | nil,
          owner: String.t() | nil,
          balance: integer(),
          overdraft_limit: non_neg_integer(),
          status: status()
        }

  defstruct account_id: nil,
            owner: nil,
            balance: 0,
            overdraft_limit: 0,
            status: :pending

  # ----- execute/2 : command → event(s) or {:error, reason} -----------------

  @spec execute(t(), struct()) :: struct() | [struct()] | {:error, atom()} | nil
  def execute(%__MODULE__{status: :pending}, %OpenAccount{} = cmd) do
    %AccountOpened{
      account_id: cmd.account_id,
      owner: cmd.owner,
      overdraft_limit: cmd.overdraft_limit,
      opened_at: DateTime.utc_now()
    }
  end

  def execute(%__MODULE__{status: :open}, %OpenAccount{}),
    do: {:error, :already_open}

  def execute(%__MODULE__{status: :closed}, _cmd),
    do: {:error, :account_closed}

  def execute(%__MODULE__{status: :open} = state, %Deposit{amount: amt}) when amt > 0 do
    %MoneyDeposited{
      account_id: state.account_id,
      amount: amt,
      new_balance: state.balance + amt,
      deposited_at: DateTime.utc_now()
    }
  end

  def execute(%__MODULE__{}, %Deposit{amount: amt}) when amt <= 0,
    do: {:error, :invalid_amount}

  def execute(%__MODULE__{status: :open} = state, %Withdraw{amount: amt}) when amt > 0 do
    new_balance = state.balance - amt

    if new_balance < -state.overdraft_limit do
      {:error, :overdraft_exceeded}
    else
      %MoneyWithdrawn{
        account_id: state.account_id,
        amount: amt,
        new_balance: new_balance,
        withdrawn_at: DateTime.utc_now()
      }
    end
  end

  def execute(%__MODULE__{}, %Withdraw{amount: amt}) when amt <= 0,
    do: {:error, :invalid_amount}

  def execute(%__MODULE__{status: :open, balance: 0} = state, %CloseAccount{}) do
    %AccountClosed{account_id: state.account_id, closed_at: DateTime.utc_now()}
  end

  def execute(%__MODULE__{status: :open}, %CloseAccount{}),
    do: {:error, :non_zero_balance}

  def execute(%__MODULE__{status: :pending}, _),
    do: {:error, :account_not_open}

  # ----- apply/2 : event → new state (NEVER raises) --------------------------

  @spec apply(t(), struct()) :: t()
  def apply(%__MODULE__{} = acc, %AccountOpened{} = e) do
    %__MODULE__{
      acc
      | account_id: e.account_id,
        owner: e.owner,
        overdraft_limit: e.overdraft_limit,
        status: :open,
        balance: 0
    }
  end

  def apply(%__MODULE__{} = acc, %MoneyDeposited{new_balance: nb}),
    do: %__MODULE__{acc | balance: nb}

  def apply(%__MODULE__{} = acc, %MoneyWithdrawn{new_balance: nb}),
    do: %__MODULE__{acc | balance: nb}

  def apply(%__MODULE__{} = acc, %AccountClosed{}),
    do: %__MODULE__{acc | status: :closed}
end
```

### Step 5: Lifespan and router — `lib/commanded_aggregates/router.ex`

```elixir
defmodule CommandedAggregates.Account.Lifespan do
  @behaviour Commanded.Aggregates.AggregateLifespan

  @impl true
  def after_event(_event), do: :timer.minutes(5)
  @impl true
  def after_command(_command), do: :timer.minutes(5)
  @impl true
  def after_error(_error), do: :stop
end

defmodule CommandedAggregates.Router do
  use Commanded.Commands.Router

  alias CommandedAggregates.Account
  alias CommandedAggregates.Commands.{OpenAccount, Deposit, Withdraw, CloseAccount}

  identify(Account, by: :account_id, prefix: "account-")

  dispatch([OpenAccount, Deposit, Withdraw, CloseAccount],
    to: Account,
    lifespan: Account.Lifespan
  )
end
```

### Step 6: Application — `lib/commanded_aggregates/application.ex`

```elixir
defmodule CommandedAggregates.App do
  use Commanded.Application,
    otp_app: :commanded_aggregates,
    event_store: [
      adapter: Commanded.EventStore.Adapters.InMemory,
      serializer: Commanded.Serialization.JsonSerializer
    ]

  router(CommandedAggregates.Router)
end

defmodule CommandedAggregates.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [CommandedAggregates.App]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 7: Tests — `test/commanded_aggregates/account_test.exs`

```elixir
defmodule CommandedAggregates.AccountTest do
  use ExUnit.Case, async: false

  alias CommandedAggregates.App
  alias CommandedAggregates.Commands.{OpenAccount, Deposit, Withdraw, CloseAccount}

  setup do
    start_supervised!(App)
    id = "acc-" <> Integer.to_string(System.unique_integer([:positive]))
    %{id: id}
  end

  describe "OpenAccount" do
    test "opens a new account", %{id: id} do
      assert :ok =
               App.dispatch(%OpenAccount{account_id: id, owner: "Alice", overdraft_limit: 0})
    end

    test "rejects second open", %{id: id} do
      :ok = App.dispatch(%OpenAccount{account_id: id, owner: "A", overdraft_limit: 0})

      assert {:error, :already_open} =
               App.dispatch(%OpenAccount{account_id: id, owner: "A", overdraft_limit: 0})
    end
  end

  describe "Deposit / Withdraw" do
    setup %{id: id} do
      :ok = App.dispatch(%OpenAccount{account_id: id, owner: "A", overdraft_limit: 100})
      :ok
    end

    test "deposit increases balance", %{id: id} do
      assert :ok = App.dispatch(%Deposit{account_id: id, amount: 500})
    end

    test "rejects non-positive deposit", %{id: id} do
      assert {:error, :invalid_amount} = App.dispatch(%Deposit{account_id: id, amount: 0})
      assert {:error, :invalid_amount} = App.dispatch(%Deposit{account_id: id, amount: -10})
    end

    test "withdraw within overdraft is allowed", %{id: id} do
      :ok = App.dispatch(%Deposit{account_id: id, amount: 50})
      assert :ok = App.dispatch(%Withdraw{account_id: id, amount: 100})
    end

    test "withdraw beyond overdraft is rejected", %{id: id} do
      assert {:error, :overdraft_exceeded} =
               App.dispatch(%Withdraw{account_id: id, amount: 500})
    end
  end

  describe "CloseAccount" do
    test "rejects close with non-zero balance", %{id: id} do
      :ok = App.dispatch(%OpenAccount{account_id: id, owner: "A", overdraft_limit: 0})
      :ok = App.dispatch(%Deposit{account_id: id, amount: 10})

      assert {:error, :non_zero_balance} = App.dispatch(%CloseAccount{account_id: id})
    end

    test "closes when balance is zero", %{id: id} do
      :ok = App.dispatch(%OpenAccount{account_id: id, owner: "A", overdraft_limit: 0})
      assert :ok = App.dispatch(%CloseAccount{account_id: id})
    end

    test "closed account rejects further commands", %{id: id} do
      :ok = App.dispatch(%OpenAccount{account_id: id, owner: "A", overdraft_limit: 0})
      :ok = App.dispatch(%CloseAccount{account_id: id})

      assert {:error, :account_closed} =
               App.dispatch(%Deposit{account_id: id, amount: 10})
    end
  end
end
```

### Step 8: Run

```bash
mix deps.get
mix test
```

---

## Trade-offs and production gotchas

**1. `apply/2` must be total and deterministic**
A raising `apply/2` poisons the stream — every subsequent replay crashes. If you need a side-effect in response to an event, use an event handler or process manager, never `apply/2`.

**2. Command validation vs aggregate invariants**
Structural validation (required fields, positive amounts, string lengths) should happen BEFORE dispatch — use Ecto.Changeset in a command factory. The aggregate enforces invariants that require state (balance, status). Pushing structural checks into the aggregate wastes replay cost.

**3. Aggregate size matters**
Long-lived aggregates accumulate thousands of events. A `GlobalLedger` aggregate with a million events takes seconds to rehydrate. Split by natural boundary (per-account) and introduce snapshotting (exercise 266) once streams exceed ~1000 events.

**4. Event schema evolution**
Events live forever in the event store. Renaming a field or deleting an event type breaks replay for historical data. Use **upcasting** (translating old events to new schemas at load time) instead of altering stored events. Plan this before shipping.

**5. Don't leak `DateTime.utc_now()` into `apply/2`**
Timestamps belong on the event, captured in `execute/2`. Replaying events years later must produce the exact same state — reading `DateTime.utc_now()` in `apply/2` makes state non-deterministic.

**6. The aggregate is NOT a query model**
Never expose aggregate state to controllers. Projections (exercise 66) build optimized read models. Querying the aggregate forces a rehydration and serializes with writes.

**7. Lifespan eviction trade-off**
Too short: every command pays the rehydration cost. Too long: memory pressure with many accounts. `:timer.minutes(5)` is a reasonable default; tune by measuring `Commanded.Aggregates.Aggregate` process count and average recovery time.

**8. When NOT to use event sourcing**
If your domain has no temporal reasoning, no audit needs, and mostly CRUD flows — a relational schema is simpler, faster, and better understood by your team. Event sourcing adds operational complexity (event store, projections, upcasters); buy it only when the business case is explicit.

---

## Performance notes

Measure aggregate throughput against the in-memory adapter:

```elixir
{time, _} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      CommandedAggregates.App.dispatch(%CommandedAggregates.Commands.Deposit{
        account_id: "acc-bench",
        amount: 1
      })
    end
  end)

IO.puts("1000 deposits in #{div(time, 1000)} ms")
```

On modern hardware with the in-memory adapter expect ~5–10k deposits/sec on a single aggregate (serialized through its GenServer). With `commanded_eventstore_adapter` (EventStoreDB) or `commanded_ecto_projections` backed by Postgres, expect 1–3k/sec dominated by `fsync`. Scale horizontally by having many aggregate instances — one account's bottleneck does not limit the others.

---

## Resources

- [Commanded hexdocs](https://hexdocs.pm/commanded/Commanded.html) — canonical API reference
- [`commanded/commanded` on GitHub](https://github.com/commanded/commanded) — source and design docs
- [Getting Started guide](https://github.com/commanded/commanded/blob/master/guides/Getting%20Started.md) — by Commanded's author Ben Smith
- [Greg Young — "CQRS Documents"](https://cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf) — foundational paper
- [Martin Fowler — "Event Sourcing"](https://martinfowler.com/eaaDev/EventSourcing.html) — pattern overview
- [Versioning in an Event Sourced System — Greg Young](https://leanpub.com/esversioning/read) — schema evolution deep-dive
