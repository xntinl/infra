# Commanded — Aggregates and Commands

## Project context

You are building `api_gateway`, an internal HTTP gateway. The billing team needs an exact, immutable record of every API usage event: when a client registered, how much capacity they provisioned, every overage. Event Sourcing is the right model: instead of updating a row, you append an event to a stream. The current state is always derivable by replaying the stream. Commanded is the Elixir framework for Event Sourcing and CQRS. All modules are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       └── billing/
│           ├── application.ex          # Commanded.Application
│           ├── router.ex               # Commands -> Aggregates
│           ├── commands/
│           │   ├── provision_client.ex
│           │   ├── record_usage.ex
│           │   └── suspend_client.ex
│           ├── events/
│           │   ├── client_provisioned.ex
│           │   ├── usage_recorded.ex
│           │   └── client_suspended.ex
│           └── aggregates/
│               └── client_account.ex   # aggregate root
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
3. **Suspension** — when a client exceeds quota or is manually suspended, no more usage can be recorded
4. **Optimistic concurrency** — two concurrent usage-recording commands for the same client must not silently overwrite each other

The aggregate enforces all business rules. If a command violates a rule, `execute/2` returns `{:error, reason}`. No event is emitted.

---

## Why `execute/2` and `apply/2` are separate functions

`execute/2` contains business logic — it can fail. It receives the current aggregate state and a command. It either returns an event (or list of events) or `{:error, reason}`.

`apply/2` is purely a state reducer. It takes the aggregate state and an event and returns the new state. It must never fail and must have no side effects. If it could fail, replaying 10,000 events to reconstruct an aggregate would be unpredictable.

This separation is derived from the fundamental constraint of Event Sourcing: an event that has been persisted represents something that already happened. You cannot "undo" it by failing the apply.

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
  @moduledoc "Command to provision a new client billing account."
  defstruct [:client_id, :monthly_quota, :plan]
end

# lib/api_gateway/billing/commands/record_usage.ex
defmodule ApiGateway.Billing.Commands.RecordUsage do
  @moduledoc "Command to record a batch of API usage for a client."
  defstruct [:client_id, :request_count, :period]
end

# lib/api_gateway/billing/commands/suspend_client.ex
defmodule ApiGateway.Billing.Commands.SuspendClient do
  @moduledoc "Command to suspend a client account."
  defstruct [:client_id, :reason]
end
```

### Step 3: Events

```elixir
# lib/api_gateway/billing/events/client_provisioned.ex
defmodule ApiGateway.Billing.Events.ClientProvisioned do
  @moduledoc "Event emitted when a client account is provisioned."
  defstruct [:client_id, :monthly_quota, :plan, :provisioned_at]
end

# lib/api_gateway/billing/events/usage_recorded.ex
defmodule ApiGateway.Billing.Events.UsageRecorded do
  @moduledoc "Event emitted when API usage is recorded for a client."
  defstruct [:client_id, :request_count, :period, :cumulative_usage]
end

# lib/api_gateway/billing/events/client_suspended.ex
defmodule ApiGateway.Billing.Events.ClientSuspended do
  @moduledoc "Event emitted when a client account is suspended."
  defstruct [:client_id, :reason, :suspended_at]
end
```

### Step 4: `lib/api_gateway/billing/aggregates/client_account.ex`

```elixir
defmodule ApiGateway.Billing.Aggregates.ClientAccount do
  @moduledoc """
  Aggregate root for client billing accounts.

  Business rules:
  - A client can only be provisioned once (status :new -> :active)
  - Usage can only be recorded on :active or :over_quota accounts
  - A suspended account rejects all commands except re-provisioning
  - Usage that exceeds monthly_quota sets status :over_quota (soft limit)
  """

  defstruct client_id:       nil,
            monthly_quota:   0,
            cumulative_usage: 0,
            plan:            nil,
            status:          :new   # :new | :active | :suspended | :over_quota

  alias ApiGateway.Billing.Commands.{ProvisionClient, RecordUsage, SuspendClient}
  alias ApiGateway.Billing.Events.{ClientProvisioned, UsageRecorded, ClientSuspended}

  # -- Command handlers --

  @spec execute(%__MODULE__{}, struct()) :: struct() | {:error, atom()}
  def execute(%__MODULE__{status: :new}, %ProvisionClient{} = cmd) do
    cond do
      is_nil(cmd.monthly_quota) or cmd.monthly_quota <= 0 ->
        {:error, :invalid_quota}

      is_nil(cmd.plan) ->
        {:error, :plan_required}

      true ->
        %ClientProvisioned{
          client_id: cmd.client_id,
          monthly_quota: cmd.monthly_quota,
          plan: cmd.plan,
          provisioned_at: DateTime.utc_now()
        }
    end
  end

  def execute(%__MODULE__{status: s}, %ProvisionClient{}) when s != :new do
    {:error, :already_provisioned}
  end

  def execute(%__MODULE__{status: :active} = account, %RecordUsage{} = cmd) do
    if is_nil(cmd.request_count) or cmd.request_count <= 0 do
      {:error, :invalid_request_count}
    else
      new_cumulative = account.cumulative_usage + cmd.request_count

      %UsageRecorded{
        client_id: cmd.client_id,
        request_count: cmd.request_count,
        period: cmd.period,
        cumulative_usage: new_cumulative
      }
    end
  end

  def execute(%__MODULE__{status: :over_quota} = account, %RecordUsage{} = cmd) do
    if is_nil(cmd.request_count) or cmd.request_count <= 0 do
      {:error, :invalid_request_count}
    else
      new_cumulative = account.cumulative_usage + cmd.request_count

      %UsageRecorded{
        client_id: cmd.client_id,
        request_count: cmd.request_count,
        period: cmd.period,
        cumulative_usage: new_cumulative
      }
    end
  end

  def execute(%__MODULE__{status: :suspended}, %RecordUsage{}) do
    {:error, :account_suspended}
  end

  def execute(%__MODULE__{status: :new}, %RecordUsage{}) do
    {:error, :account_not_provisioned}
  end

  def execute(%__MODULE__{status: :active}, %SuspendClient{} = cmd) do
    %ClientSuspended{
      client_id: cmd.client_id,
      reason: cmd.reason,
      suspended_at: DateTime.utc_now()
    }
  end

  def execute(%__MODULE__{status: :over_quota}, %SuspendClient{} = cmd) do
    %ClientSuspended{
      client_id: cmd.client_id,
      reason: cmd.reason,
      suspended_at: DateTime.utc_now()
    }
  end

  def execute(%__MODULE__{status: :suspended}, %SuspendClient{}) do
    {:error, :already_suspended}
  end

  # -- State mutators --

  @spec apply(%__MODULE__{}, struct()) :: %__MODULE__{}
  def apply(%__MODULE__{} = account, %ClientProvisioned{} = event) do
    %__MODULE__{account |
      client_id: event.client_id,
      monthly_quota: event.monthly_quota,
      plan: event.plan,
      status: :active
    }
  end

  def apply(%__MODULE__{} = account, %UsageRecorded{} = event) do
    new_status =
      if event.cumulative_usage > account.monthly_quota do
        :over_quota
      else
        :active
      end

    %__MODULE__{account |
      cumulative_usage: event.cumulative_usage,
      status: new_status
    }
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
  @moduledoc "Routes commands to the ClientAccount aggregate."
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
  @moduledoc "Commanded application for the billing subsystem."
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
| Historical state | Full replay from events | Lost on update | Append-only |
| Temporal queries | Replay to any point in time | Not possible | Only logged fields |
| Concurrency conflicts | `expected_version` in event store | DB locks | No protection |
| Read performance | Requires projection | Direct query | Direct query |
| Schema changes | Migrate events (upcasting) | `ALTER TABLE` | `ALTER TABLE` |

Reflection question: the `apply/2` function derives `status: :over_quota` from `event.cumulative_usage > account.monthly_quota`. This comparison happens during replay. What happens if you change the quota threshold in a future business rule and replay historical events?

---

## Common production mistakes

**1. Side effects in `apply/2`**
Sending emails or calling HTTP endpoints in `apply/2` fires those effects on every replay. All side effects belong in event handlers, not in `apply/2`.

**2. Using `DateTime.utc_now()` in `apply/2`**
`apply/2` is called during replay. Using `DateTime.utc_now()` means reconstructed state has different timestamps depending on when you replay. Use the timestamp from the event.

**3. Forgetting `identify` in the router**
Without `identify ClientAccount, by: :client_id`, Commanded cannot route commands to the correct aggregate instance.

**4. Large aggregate state causing slow replay**
An aggregate with 100,000 events takes significant time to replay. Use snapshots to persist the aggregate state every N events and replay only from the last snapshot.

---

## Resources

- [Commanded docs](https://hexdocs.pm/commanded) — aggregate lifecycle, routing, event store adapters
- [CQRS pattern — Martin Fowler](https://martinfowler.com/bliki/CQRS.html) — why read and write models diverge
- [Event Sourcing — Greg Young](https://cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf) — the original document
- [Commanded testing](https://hexdocs.pm/commanded/testing.html) — `AggregateCase` for integration tests
