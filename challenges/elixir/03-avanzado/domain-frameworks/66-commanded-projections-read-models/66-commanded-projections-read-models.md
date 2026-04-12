# Commanded — Projections and Read Models

## Project context

You are building `api_gateway`, an internal HTTP gateway with an event-sourced billing subsystem. The event store is the source of truth but is not optimized for dashboard queries. You need projections: materialized read models that transform events into queryable database rows. All modules — including commands, events, aggregates, Ecto schemas, projectors, and event handlers — are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── repo.ex                            # Ecto.Repo
│       └── billing/
│           ├── commands/
│           │   ├── provision_client.ex
│           │   ├── record_usage.ex
│           │   └── suspend_client.ex
│           ├── events/
│           │   ├── client_provisioned.ex
│           │   ├── usage_recorded.ex
│           │   └── client_suspended.ex
│           ├── projections/
│           │   └── client_summary.ex          # Ecto schema — read model
│           ├── projectors/
│           │   └── client_projector.ex        # projects events to read model
│           ├── handlers/
│           │   └── overage_notifier.ex        # side-effect handler for alerts
│           └── queries/
│               └── billing_queries.ex         # composable Ecto queries
├── test/
│   └── api_gateway/
│       └── billing/
│           ├── client_projector_test.exs
│           └── overage_notifier_test.exs
└── mix.exs
```

---

## The business problem

Three read-side needs that the event store cannot serve efficiently:

1. **Dashboard**: current balance, usage percentage, overage flag per client — needs a materialized view that is always up to date.
2. **Alerts**: when a client goes over quota, send a notification immediately — this is a side effect, not a query.
3. **Recovery**: when the projector code has a bug, fix it and re-project all events from the beginning without touching the write side.

---

## Why `project/3` receives an `Ecto.Multi`

`Commanded.Projections.Ecto` wraps each event's projection in a database transaction. The `project/3` callback receives an `Ecto.Multi` to which you add operations. Commanded commits all operations atomically, together with updating the projector's position in the event stream. If the commit fails, the projector retries. This gives exactly-once projection semantics.

---

## Why `Event.Handler` for notifications, not `Projections.Ecto`

`Projections.Ecto` is designed for database writes. A notification (email, Slack, webhook) is a side effect — it is not idempotent, and it does not update a database row. `Commanded.Event.Handler` is the correct abstraction: it subscribes to the event stream and calls your `handle/2` for each event. You are responsible for idempotency.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
defp deps do
  [
    {:commanded, "~> 1.4"},
    {:commanded_ecto_projections, "~> 1.4"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.18"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: Events (self-contained definitions)

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

### Step 3: Ecto schema — `lib/api_gateway/billing/projections/client_summary.ex`

```elixir
defmodule ApiGateway.Billing.Projections.ClientSummary do
  @moduledoc """
  Read model for client billing summaries.
  Projected from events by ClientProjector.
  """
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:client_id, :string, autogenerate: false}
  schema "billing_client_summaries" do
    field :plan,              :string
    field :monthly_quota,     :integer, default: 0
    field :cumulative_usage,  :integer, default: 0
    field :status,            :string,  default: "active"
    field :last_event_at,     :utc_datetime
    timestamps()
  end

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(summary, attrs) do
    summary
    |> cast(attrs, [:plan, :monthly_quota, :cumulative_usage, :status, :last_event_at])
  end
end
```

### Step 4: Migration

```elixir
defmodule ApiGateway.Repo.Migrations.CreateBillingClientSummaries do
  use Ecto.Migration

  def change do
    create table(:billing_client_summaries, primary_key: false) do
      add :client_id,        :string,  null: false, primary_key: true
      add :plan,             :string
      add :monthly_quota,    :integer, default: 0, null: false
      add :cumulative_usage, :integer, default: 0, null: false
      add :status,           :string,  default: "active", null: false
      add :last_event_at,    :utc_datetime
      timestamps()
    end

    create index(:billing_client_summaries, [:status])
  end
end
```

### Step 5: `lib/api_gateway/billing/projectors/client_projector.ex`

```elixir
defmodule ApiGateway.Billing.Projectors.ClientProjector do
  @moduledoc """
  Projects billing events to the client_summaries read model.

  Commanded.Projections.Ecto guarantees at-least-once delivery with
  exactly-once semantics via the projection_versions table.
  """

  use Commanded.Projections.Ecto,
    application: ApiGateway.Billing.Application,
    repo:        ApiGateway.Repo,
    name:        "ClientProjector",
    consistency: :strong

  alias ApiGateway.Billing.Projections.ClientSummary
  alias ApiGateway.Billing.Events.{ClientProvisioned, UsageRecorded, ClientSuspended}

  import Ecto.Query

  project(%ClientProvisioned{} = event, _metadata, multi) do
    summary = %ClientSummary{
      client_id: event.client_id,
      plan: event.plan,
      monthly_quota: event.monthly_quota,
      cumulative_usage: 0,
      status: "active"
    }

    Ecto.Multi.insert(multi, :client_summary, summary)
  end

  project(%UsageRecorded{} = event, metadata, multi) do
    last_event_at = Map.get(metadata, :created_at, DateTime.utc_now())

    Ecto.Multi.update_all(
      multi,
      :update_usage,
      from(s in ClientSummary, where: s.client_id == ^event.client_id),
      set: [
        cumulative_usage: event.cumulative_usage,
        last_event_at: last_event_at,
        status:
          fragment(
            "CASE WHEN ? > monthly_quota THEN 'over_quota' ELSE 'active' END",
            ^event.cumulative_usage
          )
      ]
    )
  end

  project(%ClientSuspended{} = event, _metadata, multi) do
    Ecto.Multi.update_all(
      multi,
      :suspend_client,
      from(s in ClientSummary, where: s.client_id == ^event.client_id),
      set: [status: "suspended"]
    )
  end
end
```

The `UsageRecorded` projector uses `Ecto.Multi.update_all/4` with a SQL `CASE` fragment to atomically update the cumulative usage and derive the status in a single query — no read-then-write race condition.

### Step 6: `lib/api_gateway/billing/handlers/overage_notifier.ex`

```elixir
defmodule ApiGateway.Billing.Handlers.OverageNotifier do
  @moduledoc """
  Sends a notification when a client crosses their monthly quota.

  Uses Commanded.Event.Handler (not Projections.Ecto) because this is a
  side effect, not a database write. Idempotency is handled via an ETS
  deduplication table keyed by (client_id, period).
  """

  use Commanded.Event.Handler,
    application: ApiGateway.Billing.Application,
    name:        "OverageNotifier"

  alias ApiGateway.Billing.Events.UsageRecorded
  alias ApiGateway.Billing.Projections.ClientSummary

  require Logger

  @table :overage_notifications_sent

  @spec init() :: :ok
  def init do
    :ets.new(@table, [:named_table, :public, :set])
    :ok
  end

  @spec handle(struct(), map()) :: :ok
  def handle(%UsageRecorded{} = event, _metadata) do
    summary = ApiGateway.Repo.get(ClientSummary, event.client_id)

    cond do
      is_nil(summary) ->
        :ok

      event.cumulative_usage <= summary.monthly_quota ->
        :ok

      already_notified?(event.client_id, event.period) ->
        :ok

      true ->
        send_overage_notification(event)
        mark_notified(event.client_id, event.period)
        :ok
    end
  end

  def handle(_event, _metadata), do: :ok

  defp already_notified?(client_id, period) do
    :ets.lookup(@table, {client_id, period}) != []
  end

  defp mark_notified(client_id, period) do
    :ets.insert(@table, {{client_id, period}, true})
  end

  defp send_overage_notification(event) do
    Logger.warning(
      "OVERAGE ALERT: client #{event.client_id} exceeded quota " <>
        "in period #{event.period} — cumulative usage: #{event.cumulative_usage}"
    )
  end
end
```

### Step 7: `lib/api_gateway/billing/queries/billing_queries.ex`

```elixir
defmodule ApiGateway.Billing.Queries.BillingQueries do
  @moduledoc "Composable Ecto queries for billing read models."
  import Ecto.Query
  alias ApiGateway.Billing.Projections.ClientSummary
  alias ApiGateway.Repo

  @spec get_summary(String.t()) :: %ClientSummary{} | nil
  def get_summary(client_id), do: Repo.get(ClientSummary, client_id)

  @spec top_consumers(pos_integer()) :: [%ClientSummary{}]
  def top_consumers(limit \\ 10) do
    ClientSummary
    |> where([s], s.status in ["active", "over_quota"])
    |> order_by([s], desc: s.cumulative_usage)
    |> limit(^limit)
    |> Repo.all()
  end

  @spec over_quota_clients() :: [%ClientSummary{}]
  def over_quota_clients do
    ClientSummary
    |> where([s], s.status == "over_quota")
    |> Repo.all()
  end

  @spec total_platform_usage() :: integer() | nil
  def total_platform_usage do
    ClientSummary
    |> where([s], s.status != "suspended")
    |> select([s], sum(s.cumulative_usage))
    |> Repo.one()
  end
end
```

### Step 8: Given tests — must pass without modification

```elixir
# test/api_gateway/billing/client_projector_test.exs
defmodule ApiGateway.Billing.Projectors.ClientProjectorTest do
  use ApiGateway.DataCase, async: false

  alias ApiGateway.Billing.Projectors.ClientProjector
  alias ApiGateway.Billing.Projections.ClientSummary
  alias ApiGateway.Billing.Events.{ClientProvisioned, UsageRecorded, ClientSuspended}

  defp project(event, metadata \\ %{created_at: DateTime.utc_now()}) do
    multi = Ecto.Multi.new()
    result_multi = ClientProjector.__project__(event, metadata, multi)
    {:ok, _} = ApiGateway.Repo.transaction(result_multi)
  end

  test "ClientProvisioned creates a summary row" do
    event = %ClientProvisioned{
      client_id: "c-test-1",
      monthly_quota: 5_000,
      plan: "standard",
      provisioned_at: DateTime.utc_now()
    }
    project(event)

    summary = ApiGateway.Repo.get(ClientSummary, "c-test-1")
    assert summary.monthly_quota == 5_000
    assert summary.status == "active"
    assert summary.cumulative_usage == 0
  end

  test "UsageRecorded updates cumulative_usage" do
    ApiGateway.Repo.insert!(%ClientSummary{
      client_id: "c-test-2",
      monthly_quota: 1_000,
      cumulative_usage: 0,
      status: "active"
    })

    event = %UsageRecorded{
      client_id: "c-test-2",
      request_count: 400,
      period: "2026-04",
      cumulative_usage: 400
    }
    project(event)

    summary = ApiGateway.Repo.get(ClientSummary, "c-test-2")
    assert summary.cumulative_usage == 400
    assert summary.status == "active"
  end

  test "UsageRecorded sets over_quota when exceeded" do
    ApiGateway.Repo.insert!(%ClientSummary{
      client_id: "c-test-3",
      monthly_quota: 100,
      cumulative_usage: 0,
      status: "active"
    })

    event = %UsageRecorded{
      client_id: "c-test-3",
      request_count: 150,
      period: "2026-04",
      cumulative_usage: 150
    }
    project(event)

    summary = ApiGateway.Repo.get(ClientSummary, "c-test-3")
    assert summary.status == "over_quota"
  end

  test "ClientSuspended sets status to suspended" do
    ApiGateway.Repo.insert!(%ClientSummary{
      client_id: "c-test-4",
      monthly_quota: 1_000,
      cumulative_usage: 200,
      status: "active"
    })

    event = %ClientSuspended{
      client_id: "c-test-4",
      reason: "non-payment",
      suspended_at: DateTime.utc_now()
    }
    project(event)

    summary = ApiGateway.Repo.get(ClientSummary, "c-test-4")
    assert summary.status == "suspended"
  end
end
```

```elixir
# test/api_gateway/billing/overage_notifier_test.exs
defmodule ApiGateway.Billing.Handlers.OverageNotifierTest do
  use ApiGateway.DataCase, async: false

  alias ApiGateway.Billing.Handlers.OverageNotifier
  alias ApiGateway.Billing.Projections.ClientSummary
  alias ApiGateway.Billing.Events.UsageRecorded

  setup do
    if :ets.whereis(:overage_notifications_sent) == :undefined do
      OverageNotifier.init()
    else
      :ets.delete_all_objects(:overage_notifications_sent)
    end

    :ok
  end

  test "does not notify when within quota" do
    ApiGateway.Repo.insert!(%ClientSummary{
      client_id: "c-ok",
      monthly_quota: 1_000,
      cumulative_usage: 0,
      status: "active"
    })

    event = %UsageRecorded{
      client_id: "c-ok",
      request_count: 200,
      period: "2026-04",
      cumulative_usage: 200
    }

    assert :ok = OverageNotifier.handle(event, %{})
    assert :ets.lookup(:overage_notifications_sent, {"c-ok", "2026-04"}) == []
  end

  test "notifies when over quota and marks as notified" do
    ApiGateway.Repo.insert!(%ClientSummary{
      client_id: "c-over",
      monthly_quota: 100,
      cumulative_usage: 0,
      status: "active"
    })

    event = %UsageRecorded{
      client_id: "c-over",
      request_count: 150,
      period: "2026-04",
      cumulative_usage: 150
    }

    assert :ok = OverageNotifier.handle(event, %{})
    assert [{{"c-over", "2026-04"}, true}] =
             :ets.lookup(:overage_notifications_sent, {"c-over", "2026-04"})
  end

  test "does not notify twice for the same client and period" do
    ApiGateway.Repo.insert!(%ClientSummary{
      client_id: "c-dup",
      monthly_quota: 100,
      cumulative_usage: 0,
      status: "over_quota"
    })

    :ets.insert(:overage_notifications_sent, {{"c-dup", "2026-04"}, true})

    event = %UsageRecorded{
      client_id: "c-dup",
      request_count: 10,
      period: "2026-04",
      cumulative_usage: 200
    }

    assert :ok = OverageNotifier.handle(event, %{})
  end
end
```

### Step 9: Run the tests

```bash
mix test test/api_gateway/billing/ --trace
```

---

## Trade-off analysis

| Aspect | `Projections.Ecto` | `Event.Handler` | Direct aggregate read |
|--------|-------------------|-----------------|----------------------|
| Persistence | Ecto transaction | None (manual) | N/A — replay only |
| Idempotency | Built-in (projection_versions) | Manual | N/A |
| Side effects | Misuse of abstraction | Correct use | N/A |
| Reset & replay | Built-in reset | Manual | Full replay |
| When to use | Read models, dashboards | Notifications, webhooks | Debugging only |

Reflection question: `after_update/3` is called after the Ecto.Multi commits. If the PubSub broadcast fails, does the projection roll back? Is the event re-processed?

---

## Common production mistakes

**1. `project/3` returning a different Multi than the one received**
`Ecto.Multi.new()` in `project/3` loses the transaction context Commanded needs for position tracking.

**2. `consistency: :eventual` in tests**
The command returns before the projection commits. Your test asserts on the read model before it has been updated. Use `:strong` for synchronous tests.

**3. `Event.Handler` without idempotency**
If the handler crashes and restarts, it replays from the last committed position. Without idempotency, notifications are sent twice.

**4. Schema evolution without event upcasting**
If you add a field to `UsageRecorded` and replay old events that lack it, the pattern match may fail. Use Commanded's event upcasting.

---

## Resources

- [Commanded.Projections.Ecto](https://hexdocs.pm/commanded_ecto_projections) — `project/3`, `after_update/3`, reset
- [Commanded Event Handlers](https://hexdocs.pm/commanded/event-handlers.html) — lifecycle, subscriptions
- [Commanded Snapshotting](https://hexdocs.pm/commanded/snapshotting.html) — aggregate replay performance
- [Commanded Event Upcasting](https://hexdocs.pm/commanded/event-upcasting.html) — schema evolution
