# Commanded — Aggregates and Commands

**Project**: `api_gateway` — audit and billing subsystem using Event Sourcing

---

## Project context

You're building `api_gateway`. The billing team needs an exact, immutable record of
every API usage event: when a client registered, how much capacity they provisioned,
every overage. The ops team needs a full audit trail of gateway configuration changes.

Event Sourcing is the right model here: instead of updating a row in a database, you
append an event to a stream. The current state is always derivable by replaying the
stream. Commanded is the Elixir framework for Event Sourcing and CQRS.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── billing/
│       │   ├── application.ex          # Commanded.Application
│       │   ├── router.ex               # Commands → Aggregates
│       │   ├── commands/
│       │   │   ├── provision_client.ex
│       │   │   ├── record_usage.ex
│       │   │   └── suspend_client.ex
│       │   ├── events/
│       │   │   ├── client_provisioned.ex
│       │   │   ├── usage_recorded.ex
│       │   │   └── client_suspended.ex
│       │   └── aggregates/
│       │       └── client_account.ex   # ← you implement this
├── test/
│   └── api_gateway/
│       └── billing/
│           └── client_account_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

A `ClientAccount` aggregate tracks:

1. **Provisioning** — a client registers with a monthly quota (requests/month)
2. **Usage recording** — every request batch records how many requests were used
3. **Suspension** — when a client exceeds quota or is manually suspended, no more
   usage can be recorded
4. **Optimistic concurrency** — two concurrent usage-recording commands for the same
   client must not silently overwrite each other

The aggregate enforces all business rules. If a command violates a rule, `execute/2`
returns `{:error, reason}`. No event is emitted. No state changes. The event store is
not touched.

---

## Why `execute/2` and `apply/2` are separate functions

`execute/2` contains business logic — it can fail. It receives the current aggregate
state and a command. It either returns an event (or list of events) or `{:error, reason}`.

`apply/2` is purely a state reducer. It takes the aggregate state and an event and
returns the new state. It must never fail and must have no side effects. If it could
fail, replaying 10,000 events to reconstruct an aggregate would be unpredictable.

This separation is not a Commanded convention — it is derived from the fundamental
constraint of Event Sourcing: an event that has been persisted represents something
that already happened. You cannot "undo" it by failing the apply.

---

## Why `apply/2` must never query the database

During aggregate reconstruction (replay), Commanded calls `apply/2` for every event
in the stream, potentially thousands of times. If `apply/2` made a database query,
reconstruction would be N database calls. Beyond performance, it would create a
temporal paradox: you are reconstructing past state from events, but you are querying
the current state of the database (which may have been updated by events that haven't
been replayed yet in this reconstruction).

---

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    {:commanded, "~> 1.4"},
    {:commanded_eventstore_adapter, "~> 1.4"},
    {:eventstore, "~> 1.4"}
  ]
end
```

### Step 2: Commands

```elixir
# lib/api_gateway/billing/commands/provision_client.ex
defmodule ApiGateway.Billing.Commands.ProvisionClient do
  defstruct [:client_id, :monthly_quota, :plan]
end

# lib/api_gateway/billing/commands/record_usage.ex
defmodule ApiGateway.Billing.Commands.RecordUsage do
  defstruct [:client_id, :request_count, :period]
end

# lib/api_gateway/billing/commands/suspend_client.ex
defmodule ApiGateway.Billing.Commands.SuspendClient do
  defstruct [:client_id, :reason]
end
```

### Step 3: Events

```elixir
# lib/api_gateway/billing/events/client_provisioned.ex
defmodule ApiGateway.Billing.Events.ClientProvisioned do
  defstruct [:client_id, :monthly_quota, :plan, :provisioned_at]
end

# lib/api_gateway/billing/events/usage_recorded.ex
defmodule ApiGateway.Billing.Events.UsageRecorded do
  defstruct [:client_id, :request_count, :period, :cumulative_usage]
end

# lib/api_gateway/billing/events/client_suspended.ex
defmodule ApiGateway.Billing.Events.ClientSuspended do
  defstruct [:client_id, :reason, :suspended_at]
end
```

### Step 4: `lib/api_gateway/billing/aggregates/client_account.ex`

```elixir
defmodule ApiGateway.Billing.Aggregates.ClientAccount do
  @moduledoc """
  Aggregate root for client billing accounts.

  Business rules:
  - A client can only be provisioned once (status :new → :active)
  - Usage can only be recorded on :active accounts
  - A suspended account rejects all commands except re-provisioning (not in scope here)
  - Usage that would exceed monthly_quota emits UsageRecorded BUT sets status :over_quota
    (soft limit — we record the usage, alert separately)
  """

  defstruct client_id:       nil,
            monthly_quota:   0,
            cumulative_usage: 0,
            plan:            nil,
            status:          :new   # :new | :active | :suspended | :over_quota

  alias ApiGateway.Billing.Commands.{ProvisionClient, RecordUsage, SuspendClient}
  alias ApiGateway.Billing.Events.{ClientProvisioned, UsageRecorded, ClientSuspended}

  # -- Command handlers --

  def execute(%__MODULE__{status: :new}, %ProvisionClient{} = cmd) do
    # TODO: validate monthly_quota > 0 and plan is non-nil
    # TODO: return a %ClientProvisioned{} event with provisioned_at: DateTime.utc_now()
  end

  def execute(%__MODULE__{status: s}, %ProvisionClient{}) when s != :new do
    {:error, :already_provisioned}
  end

  def execute(%__MODULE__{status: :active} = account, %RecordUsage{} = cmd) do
    # TODO: validate request_count > 0
    # TODO: compute new cumulative_usage = account.cumulative_usage + cmd.request_count
    # TODO: return %UsageRecorded{cumulative_usage: new_cumulative, ...}
    # Note: do NOT reject if over quota — record usage and let the projector alert
  end

  def execute(%__MODULE__{status: :over_quota} = account, %RecordUsage{} = cmd) do
    # TODO: same as :active — still record usage even when over quota
    # The soft-limit behavior: we log it, we charge overage, we don't block
  end

  def execute(%__MODULE__{status: :suspended}, %RecordUsage{}) do
    {:error, :account_suspended}
  end

  def execute(%__MODULE__{status: :new}, %RecordUsage{}) do
    {:error, :account_not_provisioned}
  end

  def execute(%__MODULE__{status: :active}, %SuspendClient{} = cmd) do
    # TODO: return %ClientSuspended{suspended_at: DateTime.utc_now(), reason: cmd.reason}
  end

  def execute(%__MODULE__{status: :suspended}, %SuspendClient{}) do
    {:error, :already_suspended}
  end

  # -- State mutators --

  def apply(%__MODULE__{} = account, %ClientProvisioned{} = event) do
    # TODO: return account with client_id, monthly_quota, plan set and status: :active
  end

  def apply(%__MODULE__{} = account, %UsageRecorded{} = event) do
    # TODO: update cumulative_usage to event.cumulative_usage
    # TODO: if cumulative_usage > monthly_quota → status: :over_quota, else keep :active
    # HINT: the status transition must be derived from the event data, not from cmd data
    #       (apply receives the event, not the command)
  end

  def apply(%__MODULE__{} = account, %ClientSuspended{}) do
    %__MODULE__{account | status: :suspended}
  end
end
```

### Step 5: Router and Application

```elixir
# lib/api_gateway/billing/router.ex
defmodule ApiGateway.Billing.Router do
  use Commanded.Commands.Router

  alias ApiGateway.Billing.Aggregates.ClientAccount
  alias ApiGateway.Billing.Commands.{ProvisionClient, RecordUsage, SuspendClient}

  identify ClientAccount, by: :client_id

  dispatch [ProvisionClient, RecordUsage, SuspendClient],
    to: ClientAccount
end
```

```elixir
# lib/api_gateway/billing/application.ex
defmodule ApiGateway.Billing.Application do
  use Commanded.Application, otp_app: :api_gateway

  router ApiGateway.Billing.Router
end
```

```elixir
# config/config.exs
config :api_gateway, ApiGateway.Billing.Application,
  event_store: [
    adapter: Commanded.EventStore.Adapters.InMemory
  ]
```

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/billing/client_account_test.exs
defmodule ApiGateway.Billing.Aggregates.ClientAccountTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Billing.Aggregates.ClientAccount
  alias ApiGateway.Billing.Commands.{ProvisionClient, RecordUsage, SuspendClient}
  alias ApiGateway.Billing.Events.{ClientProvisioned, UsageRecorded, ClientSuspended}

  # Rebuilds aggregate state by applying a list of events
  defp build_state(events) do
    Enum.reduce(events, %ClientAccount{}, &ClientAccount.apply(&2, &1))
  end

  describe "ProvisionClient" do
    test "provisions a new account" do
      cmd = %ProvisionClient{client_id: "c-1", monthly_quota: 10_000, plan: "standard"}
      assert %ClientProvisioned{monthly_quota: 10_000} = ClientAccount.execute(%ClientAccount{}, cmd)
    end

    test "fails when already provisioned" do
      state = build_state([
        %ClientProvisioned{client_id: "c-1", monthly_quota: 10_000, plan: "standard",
                           provisioned_at: DateTime.utc_now()}
      ])
      cmd = %ProvisionClient{client_id: "c-1", monthly_quota: 10_000, plan: "standard"}
      assert {:error, :already_provisioned} = ClientAccount.execute(state, cmd)
    end
  end

  describe "RecordUsage" do
    setup do
      state = build_state([
        %ClientProvisioned{client_id: "c-2", monthly_quota: 1_000, plan: "basic",
                           provisioned_at: DateTime.utc_now()}
      ])
      {:ok, state: state}
    end

    test "records usage within quota", %{state: state} do
      cmd = %RecordUsage{client_id: "c-2", request_count: 500, period: "2026-04"}
      assert %UsageRecorded{cumulative_usage: 500} = ClientAccount.execute(state, cmd)
    end

    test "records usage even when over quota (soft limit)", %{state: state} do
      over_state = build_state([
        %ClientProvisioned{client_id: "c-2", monthly_quota: 100, plan: "basic",
                           provisioned_at: DateTime.utc_now()},
        %UsageRecorded{client_id: "c-2", request_count: 90, period: "2026-04", cumulative_usage: 90}
      ])
      cmd = %RecordUsage{client_id: "c-2", request_count: 50, period: "2026-04"}
      assert %UsageRecorded{cumulative_usage: 140} = ClientAccount.execute(over_state, cmd)
    end

    test "rejects usage on suspended account" do
      state = build_state([
        %ClientProvisioned{client_id: "c-3", monthly_quota: 1_000, plan: "basic",
                           provisioned_at: DateTime.utc_now()},
        %ClientSuspended{client_id: "c-3", reason: "non-payment", suspended_at: DateTime.utc_now()}
      ])
      cmd = %RecordUsage{client_id: "c-3", request_count: 1, period: "2026-04"}
      assert {:error, :account_suspended} = ClientAccount.execute(state, cmd)
    end

    test "rejects usage on unprovisioned account" do
      cmd = %RecordUsage{client_id: "c-new", request_count: 1, period: "2026-04"}
      assert {:error, :account_not_provisioned} = ClientAccount.execute(%ClientAccount{}, cmd)
    end
  end

  describe "apply/2 — state reconstruction" do
    test "cumulative usage and status transition tracked correctly" do
      events = [
        %ClientProvisioned{client_id: "c-4", monthly_quota: 100, plan: "basic",
                           provisioned_at: DateTime.utc_now()},
        %UsageRecorded{client_id: "c-4", request_count: 60, period: "2026-04", cumulative_usage: 60},
        %UsageRecorded{client_id: "c-4", request_count: 50, period: "2026-04", cumulative_usage: 110}
      ]

      state = build_state(events)
      assert state.cumulative_usage == 110
      assert state.status == :over_quota
    end

    test "status remains :active when under quota" do
      events = [
        %ClientProvisioned{client_id: "c-5", monthly_quota: 1_000, plan: "pro",
                           provisioned_at: DateTime.utc_now()},
        %UsageRecorded{client_id: "c-5", request_count: 500, period: "2026-04", cumulative_usage: 500}
      ]
      state = build_state(events)
      assert state.status == :active
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/billing/client_account_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Event Sourcing (Commanded) | CRUD with Ecto | Audit log table |
|--------|---------------------------|----------------|-----------------|
| Historical state | Full replay from events | Lost on update | Append-only, query-able |
| Temporal queries | Replay to any point in time | Not possible | Only logged fields |
| Concurrency conflicts | `expected_version` in event store | DB locks or optimistic lock | No protection |
| Read performance | Requires projection (exercise 66) | Direct query | Direct query |
| Debugging past bugs | Replay with fixed `apply/2` | Impossible | Partial |
| Schema changes | Migrate events (upcasting) | `ALTER TABLE` | `ALTER TABLE` |

Reflection question: the `apply/2` function derives `status: :over_quota` from
`event.cumulative_usage > account.monthly_quota`. This comparison happens during
replay. What happens if you change the quota threshold in a future business rule
and replay historical events? Are past state reconstructions still correct?

---

## Common production mistakes

**1. Side effects in `apply/2`**
Sending emails, calling HTTP endpoints, or querying the database in `apply/2` will
fire those effects on every replay. A 10,000-event stream would send 10,000 emails.
All side effects belong in event handlers (exercise 66), not in `apply/2`.

**2. `execute/2` not covering all state combinations**
If `execute(%__MODULE__{status: :over_quota}, %SuspendClient{})` falls through to a
non-matching clause, Commanded raises `FunctionClauseError` at runtime. Add explicit
clauses for every `{status, command}` combination your domain allows.

**3. Large aggregate state causing slow replay**
An aggregate with 100,000 events takes significant time to replay on each command.
The solution is snapshots (exercise 66): persist the aggregate state every N events
and replay only from the last snapshot.

**4. Using `DateTime.utc_now()` in `apply/2`**
`apply/2` is called during replay. Using `DateTime.utc_now()` in `apply/2` means the
reconstructed state has different timestamps depending on when you replay. Use the
timestamp from the event (set by `execute/2` when the event was first created).

**5. Forgetting `identify` in the router**
Without `identify ClientAccount, by: :client_id`, Commanded cannot route commands to
the correct aggregate instance. Every dispatch will fail with a routing error.

---

## Resources

- [Commanded docs](https://hexdocs.pm/commanded) — aggregate lifecycle, routing, event store adapters
- [CQRS pattern — Martin Fowler](https://martinfowler.com/bliki/CQRS.html) — why read and write models diverge
- [Event Sourcing — Greg Young](https://cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf) — the original document
- [Commanded testing](https://hexdocs.pm/commanded/testing.html) — `AggregateCase` for integration tests
