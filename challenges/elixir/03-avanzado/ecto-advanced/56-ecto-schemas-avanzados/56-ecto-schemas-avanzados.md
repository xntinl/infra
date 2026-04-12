# Advanced Ecto Schemas

## Overview

Implement advanced Ecto schema patterns for an API gateway: polymorphic audit events,
embedded schemas stored as JSONB, multi-tenant query scoping with `put_query_prefix/2`,
preload optimization to prevent N+1 queries, and transactional side effects with
`prepare_changes/1`.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── repo.ex
│       ├── schemas/
│       │   ├── client.ex
│       │   ├── request_log.ex
│       │   ├── request_metadata.ex
│       │   ├── request_header.ex
│       │   └── billing_entry.ex
│       ├── audit/
│       │   ├── event.ex
│       │   └── events.ex
│       ├── tenant/
│       │   └── scope.ex
│       └── analytics/
│           └── preloader.ex
└── test/
    └── api_gateway/
        ├── audit_event_test.exs
        ├── request_log_test.exs
        └── tenant_scope_test.exs
```

---

## Why these patterns matter in production

- **Polymorphic associations**: audit events reference clients, workers, and webhook
  endpoints -- all different tables. One `audit_events` table with `auditable_type` /
  `auditable_id` handles all three without tripling the schema.

- **`embeds_one` / `embeds_many`**: request metadata (headers, query params, TLS info)
  changes shape frequently. Storing it as JSONB with an `embedded_schema` gives full Ecto
  validation without a JOIN.

- **`put_query_prefix/2`**: enterprise clients get isolated PostgreSQL schemas. A missing
  prefix silently reads the wrong tenant's data. The `Scope` module makes accidental
  cross-tenant reads structurally impossible.

- **Preload optimization**: the admin dashboard loads clients with their last 5 request
  logs. Without explicit preloads, Ecto issues one query per client (N+1).

---

## Implementation

### Part 1: Polymorphic audit events

```elixir
# lib/api_gateway/audit/event.ex
defmodule ApiGateway.Audit.Event do
  use Ecto.Schema
  import Ecto.Changeset

  @auditable_types ["Client", "Worker", "WebhookEndpoint"]

  schema "audit_events" do
    field :action,         :string
    field :actor_id,       :integer
    field :actor_type,     :string
    field :auditable_id,   :integer
    field :auditable_type, :string
    field :metadata,       :map, default: %{}
    timestamps(updated_at: false)
  end

  @required_fields ~w(action auditable_id auditable_type)a
  @optional_fields ~w(actor_id actor_type metadata)a

  @spec changeset(t(), map()) :: Ecto.Changeset.t()
  def changeset(event, attrs) do
    event
    |> cast(attrs, @required_fields ++ @optional_fields)
    |> validate_required(@required_fields)
    |> validate_inclusion(:auditable_type, @auditable_types)
    |> validate_length(:action, min: 1, max: 100)
  end
end
```

```sql
-- Migration
CREATE TABLE audit_events (
  id              BIGSERIAL PRIMARY KEY,
  action          VARCHAR(100) NOT NULL,
  actor_id        INTEGER,
  actor_type      VARCHAR(50),
  auditable_id    INTEGER NOT NULL,
  auditable_type  VARCHAR(50) NOT NULL,
  metadata        JSONB DEFAULT '{}',
  inserted_at     TIMESTAMP NOT NULL
);

CREATE INDEX audit_events_on_auditable ON audit_events (auditable_type, auditable_id);
CREATE INDEX audit_events_on_inserted_at ON audit_events (inserted_at DESC);
```

#### Query module

```elixir
# lib/api_gateway/audit/events.ex
defmodule ApiGateway.Audit.Events do
  import Ecto.Query
  alias ApiGateway.{Repo, Audit.Event}

  @doc """
  Returns audit events for a given entity struct.

  ## Examples

      Events.for(%Client{id: 1})
      Events.for(%Worker{id: 42}, limit: 20)
  """
  @spec for(struct(), keyword()) :: [Event.t()]
  def for(auditable, opts \\ []) do
    type = auditable.__struct__ |> Module.split() |> List.last()
    limit = Keyword.get(opts, :limit, 50)

    from(e in Event,
      where: e.auditable_type == ^type and e.auditable_id == ^auditable.id,
      order_by: [desc: e.inserted_at],
      limit: ^limit
    )
    |> Repo.all()
  end

  @doc """
  Records a new audit event. Raises on validation failure.
  """
  @spec record!(struct(), String.t(), map()) :: Event.t()
  def record!(auditable, action, metadata \\ %{}) do
    type = auditable.__struct__ |> Module.split() |> List.last()

    %Event{}
    |> Event.changeset(%{
      auditable_type: type,
      auditable_id: auditable.id,
      action: action,
      metadata: metadata
    })
    |> Repo.insert!()
  end

  @doc """
  Returns the count of events per action for a given entity.
  """
  @spec action_summary(struct()) :: [%{action: String.t(), count: integer()}]
  def action_summary(auditable) do
    type = auditable.__struct__ |> Module.split() |> List.last()

    from(e in Event,
      where: e.auditable_type == ^type and e.auditable_id == ^auditable.id,
      group_by: e.action,
      select: %{action: e.action, count: count(e.id)}
    )
    |> Repo.all()
  end
end
```

---

### Part 2: Embedded request metadata

Each request log stores HTTP metadata as JSONB alongside the row.

```elixir
# lib/api_gateway/schemas/request_header.ex
defmodule ApiGateway.RequestHeader do
  use Ecto.Schema
  import Ecto.Changeset

  embedded_schema do
    field :name,  :string
    field :value, :string
  end

  @spec changeset(t(), map()) :: Ecto.Changeset.t()
  def changeset(header, attrs) do
    header
    |> cast(attrs, [:name, :value])
    |> validate_required([:name, :value])
    |> validate_format(:name, ~r/^[a-z][a-z0-9-]*$/)
  end
end
```

```elixir
# lib/api_gateway/schemas/request_metadata.ex
defmodule ApiGateway.RequestMetadata do
  use Ecto.Schema
  import Ecto.Changeset

  embedded_schema do
    field :user_agent,   :string
    field :remote_ip,    :string
    field :tls_version,  :string
    field :request_id,   :string
    field :referer,      :string
    embeds_many :custom_headers, ApiGateway.RequestHeader, on_replace: :delete
  end

  @spec changeset(t(), map()) :: Ecto.Changeset.t()
  def changeset(meta, attrs) do
    meta
    |> cast(attrs, [:user_agent, :remote_ip, :tls_version, :request_id, :referer])
    |> cast_embed(:custom_headers)
    |> validate_format(:remote_ip, ~r/^\d{1,3}(\.\d{1,3}){3}$|^[0-9a-f:]+$/i)
    |> validate_inclusion(:tls_version, ["TLSv1.2", "TLSv1.3", nil])
  end
end
```

```elixir
# lib/api_gateway/schemas/request_log.ex
defmodule ApiGateway.RequestLog do
  use Ecto.Schema
  import Ecto.Changeset

  schema "request_logs" do
    field :client_id,        :integer
    field :method,           :string
    field :path,             :string
    field :status,           :integer
    field :duration_ms,      :integer
    field :bytes_transferred, :integer
    field :billing_processed, :boolean, default: false

    embeds_one :metadata, ApiGateway.RequestMetadata, on_replace: :update

    timestamps()
  end

  @spec changeset(t(), map()) :: Ecto.Changeset.t()
  def changeset(log, attrs) do
    log
    |> cast(attrs, [:client_id, :method, :path, :status, :duration_ms, :bytes_transferred])
    |> validate_required([:client_id, :method, :path, :status])
    |> validate_inclusion(:method, ~w(GET POST PUT PATCH DELETE HEAD OPTIONS))
    |> validate_number(:status, greater_than_or_equal_to: 100, less_than: 600)
    |> cast_embed(:metadata)
  end
end
```

```sql
ALTER TABLE request_logs ADD COLUMN metadata JSONB;
CREATE INDEX request_logs_metadata_gin ON request_logs USING GIN (metadata);
```

Usage:

```elixir
attrs = %{
  client_id: 1, method: "GET", path: "/api/v1/users", status: 200,
  duration_ms: 45, bytes_transferred: 1_024,
  metadata: %{
    remote_ip: "10.0.0.1", tls_version: "TLSv1.3",
    user_agent: "GatewayClient/2.0",
    custom_headers: [%{name: "x-request-id", value: "abc-123"}]
  }
}

changeset = ApiGateway.RequestLog.changeset(%ApiGateway.RequestLog{}, attrs)
changeset.valid?   # true -- metadata validated recursively
```

---

### Part 3: Multi-tenancy with `put_query_prefix/2`

Enterprise clients get their own PostgreSQL schema. All queries for a tenant must target
their schema -- a query missing the prefix silently reads the wrong data.

```elixir
# lib/api_gateway/tenant/scope.ex
defmodule ApiGateway.Tenant.Scope do
  import Ecto.Query
  alias ApiGateway.Repo

  @valid_tenant_pattern ~r/^[a-z][a-z0-9_]{0,62}$/

  @doc """
  Applies the tenant prefix to an Ecto query.
  Raises ArgumentError if the tenant ID does not match the safe pattern.
  """
  @spec scope(Ecto.Queryable.t(), String.t()) :: Ecto.Query.t()
  def scope(query, tenant_id) do
    validate_tenant_id!(tenant_id)
    query |> Ecto.Queryable.to_query() |> put_query_prefix("tenant_#{tenant_id}")
  end

  @doc "Repo.all scoped to a tenant."
  @spec all(Ecto.Queryable.t(), String.t()) :: [struct()]
  def all(query, tenant_id) do
    validate_tenant_id!(tenant_id)
    scope(query, tenant_id) |> Repo.all()
  end

  @doc "Repo.get scoped to a tenant."
  @spec get(module(), integer(), String.t()) :: struct() | nil
  def get(schema, id, tenant_id) do
    validate_tenant_id!(tenant_id)
    Repo.get(schema, id, prefix: "tenant_#{tenant_id}")
  end

  @doc "Repo.insert scoped to a tenant."
  @spec insert(Ecto.Changeset.t(), String.t()) :: {:ok, struct()} | {:error, Ecto.Changeset.t()}
  def insert(changeset, tenant_id) do
    validate_tenant_id!(tenant_id)
    Repo.insert(changeset, prefix: "tenant_#{tenant_id}")
  end

  @doc "Repo.update scoped to a tenant."
  @spec update(Ecto.Changeset.t(), String.t()) :: {:ok, struct()} | {:error, Ecto.Changeset.t()}
  def update(changeset, tenant_id) do
    validate_tenant_id!(tenant_id)
    Repo.update(changeset, prefix: "tenant_#{tenant_id}")
  end

  defp validate_tenant_id!(tenant_id) do
    unless Regex.match?(@valid_tenant_pattern, tenant_id) do
      raise ArgumentError, "unsafe tenant_id: #{inspect(tenant_id)}"
    end
  end
end
```

The generated SQL targets the tenant's schema:

```sql
-- Scope.all(Client, "acme_corp")
SELECT c0."id", c0."name" FROM "tenant_acme_corp"."clients" AS c0

-- Scope.all(Client, "globex")
SELECT c0."id", c0."name" FROM "tenant_globex"."clients" AS c0
```

A Plug injects the tenant at the request boundary:

```elixir
# lib/api_gateway_web/plugs/tenant_plug.ex
defmodule ApiGatewayWeb.TenantPlug do
  import Plug.Conn

  def init(opts), do: opts

  def call(conn, _opts) do
    tenant_id = get_req_header(conn, "x-tenant-id") |> List.first()

    cond do
      is_nil(tenant_id) ->
        conn |> send_resp(400, "Missing X-Tenant-Id header") |> halt()

      not Regex.match?(~r/^[a-z][a-z0-9_]{0,62}$/, tenant_id) ->
        conn |> send_resp(400, "Invalid tenant identifier") |> halt()

      true ->
        assign(conn, :tenant_id, tenant_id)
    end
  end
end
```

---

### Part 4: Preload optimization for the admin dashboard

```elixir
# lib/api_gateway/analytics/preloader.ex
defmodule ApiGateway.Analytics.Preloader do
  import Ecto.Query
  alias ApiGateway.{Repo, Client, RequestLog}

  @doc """
  Loads all clients for the dashboard with their recent request logs preloaded.

  Uses two queries total (not N+1):
    1. SELECT * FROM clients ORDER BY name
    2. SELECT * FROM request_logs WHERE client_id IN (...) ORDER BY inserted_at DESC
  """
  @spec clients_with_recent_logs(integer()) :: [Client.t()]
  def clients_with_recent_logs(log_limit \\ 5) do
    recent_logs =
      from(r in RequestLog,
        order_by: [desc: r.inserted_at],
        limit: ^log_limit
      )

    from(c in Client,
      order_by: c.name,
      preload: [request_logs: ^recent_logs]
    )
    |> Repo.all()
  end

  @doc """
  Loads a single client with all associations needed for the detail page.

  Uses a JOIN for billing (needed for the WHERE clause) and a separate
  preload for request_logs.
  """
  @spec client_detail(integer()) :: Client.t() | nil
  def client_detail(client_id) do
    recent_logs =
      from(r in RequestLog,
        order_by: [desc: r.inserted_at],
        limit: 10
      )

    from(c in Client,
      left_join: b in assoc(c, :billing_entries), as: :billing,
      where: c.id == ^client_id,
      preload: [billing_entries: b, request_logs: ^recent_logs]
    )
    |> Repo.one()
  end

  @doc """
  Detects potential N+1: returns true if the struct has a loaded association.
  Use in tests to catch missing preloads before they hit production.
  """
  @spec loaded?(struct(), atom()) :: boolean()
  def loaded?(struct, assoc_name) do
    case Map.get(struct, assoc_name) do
      %Ecto.Association.NotLoaded{} -> false
      _                             -> true
    end
  end
end
```

---

### Part 5: `prepare_changes/1` for transactional side effects

When a client's plan changes, the rate limiter ETS table must be updated atomically
with the DB write.

```elixir
# lib/api_gateway/schemas/client.ex
defmodule ApiGateway.Client do
  use Ecto.Schema
  import Ecto.Changeset

  schema "clients" do
    field :name,             :string
    field :plan,             Ecto.Enum, values: [:free, :pro, :enterprise]
    field :active,           :boolean, default: true
    field :quota_remaining,  :integer
    has_many :request_logs,   ApiGateway.RequestLog
    has_many :billing_entries, ApiGateway.BillingEntry
    timestamps()
  end

  @spec changeset(t(), map()) :: Ecto.Changeset.t()
  def changeset(client, attrs) do
    client
    |> cast(attrs, [:name, :plan, :active, :quota_remaining])
    |> validate_required([:name, :plan])
  end

  @doc """
  Changeset for plan upgrades. Records an audit event and refreshes the
  rate limiter ETS table inside the same transaction.

  prepare_changes/1 runs ONLY if the changeset is valid AND inside the DB transaction.
  """
  @spec plan_upgrade_changeset(t(), map()) :: Ecto.Changeset.t()
  def plan_upgrade_changeset(client, attrs) do
    client
    |> cast(attrs, [:plan, :quota_remaining])
    |> validate_required([:plan])
    |> validate_change(:plan, fn :plan, new_plan ->
      if plan_rank(new_plan) > plan_rank(client.plan),
        do: [],
        else: [plan: "can only upgrade, not downgrade"]
    end)
    |> prepare_changes(&record_plan_change_audit/1)
    |> prepare_changes(&refresh_rate_limiter/1)
  end

  defp record_plan_change_audit(changeset) do
    old_plan = changeset.data.plan
    new_plan = get_change(changeset, :plan)

    if new_plan && new_plan != old_plan do
      changeset.repo.insert!(%ApiGateway.Audit.Event{
        auditable_type: "Client",
        auditable_id: changeset.data.id,
        action: "plan_upgraded",
        metadata: %{from: old_plan, to: new_plan}
      })
    end

    changeset
  end

  defp refresh_rate_limiter(changeset) do
    if new_plan = get_change(changeset, :plan) do
      :ets.insert(:rate_limiter_config, {changeset.data.id, quota_for(new_plan)})
    end

    changeset
  end

  defp plan_rank(:free),       do: 1
  defp plan_rank(:pro),        do: 2
  defp plan_rank(:enterprise), do: 3

  defp quota_for(:free),       do: 1_000
  defp quota_for(:pro),        do: 50_000
  defp quota_for(:enterprise), do: :unlimited
end
```

---

### Step 6: Tests

```elixir
# test/api_gateway/audit_event_test.exs
defmodule ApiGateway.Audit.EventTest do
  use ApiGateway.DataCase

  alias ApiGateway.Audit.{Event, Events}

  test "record! creates an audit event for a client" do
    client = insert(:client)

    event = Events.record!(client, "rate_limited", %{limit: 1000, actual: 1500})

    assert event.auditable_type == "Client"
    assert event.auditable_id   == client.id
    assert event.action         == "rate_limited"
    assert event.metadata       == %{"limit" => 1000, "actual" => 1500}
  end

  test "for/1 returns events for the given entity only" do
    client_a = insert(:client)
    client_b = insert(:client)

    Events.record!(client_a, "created")
    Events.record!(client_a, "updated")
    Events.record!(client_b, "created")

    events = Events.for(client_a)
    assert length(events) == 2
    assert Enum.all?(events, fn e -> e.auditable_id == client_a.id end)
  end

  test "for/1 respects the limit option" do
    client = insert(:client)
    Enum.each(1..10, fn _ -> Events.record!(client, "ping") end)

    assert length(Events.for(client, limit: 3)) == 3
  end

  test "changeset rejects unknown auditable_type" do
    attrs = %{action: "created", auditable_id: 1, auditable_type: "Invoice"}
    cs = Event.changeset(%Event{}, attrs)
    refute cs.valid?
    assert {:auditable_type, _} = hd(cs.errors)
  end

  test "action_summary groups events by action" do
    client = insert(:client)
    Events.record!(client, "rate_limited")
    Events.record!(client, "rate_limited")
    Events.record!(client, "updated")

    summary = Events.action_summary(client)
    by_action = Map.new(summary, fn %{action: a, count: c} -> {a, c} end)

    assert by_action["rate_limited"] == 2
    assert by_action["updated"] == 1
  end
end
```

```elixir
# test/api_gateway/request_log_test.exs
defmodule ApiGateway.RequestLogTest do
  use ApiGateway.DataCase

  alias ApiGateway.RequestLog

  @valid_attrs %{
    client_id: 1, method: "GET", path: "/api/v1/resources", status: 200,
    duration_ms: 42, bytes_transferred: 512,
    metadata: %{
      remote_ip: "10.0.0.1", tls_version: "TLSv1.3",
      user_agent: "TestAgent/1.0", request_id: "req-abc",
      custom_headers: [%{name: "x-trace-id", value: "trace-xyz"}]
    }
  }

  test "valid request log with embedded metadata" do
    cs = RequestLog.changeset(%RequestLog{}, @valid_attrs)
    assert cs.valid?
  end

  test "embedded metadata is validated recursively" do
    attrs = put_in(@valid_attrs, [:metadata, :tls_version], "SSLv3")
    cs = RequestLog.changeset(%RequestLog{}, attrs)
    refute cs.valid?
    refute cs.changes.metadata.valid?
  end

  test "custom header name must be lowercase" do
    attrs = put_in(@valid_attrs, [:metadata, :custom_headers], [%{name: "X-Trace", value: "1"}])
    cs = RequestLog.changeset(%RequestLog{}, attrs)
    refute cs.valid?
  end

  test "metadata is preserved as JSONB on insert" do
    {:ok, log} = %RequestLog{}
    |> RequestLog.changeset(@valid_attrs)
    |> ApiGateway.Repo.insert()

    loaded = ApiGateway.Repo.get!(RequestLog, log.id)
    assert loaded.metadata.remote_ip == "10.0.0.1"
    assert loaded.metadata.tls_version == "TLSv1.3"
    assert hd(loaded.metadata.custom_headers).name == "x-trace-id"
  end

  test "missing method is invalid" do
    attrs = Map.delete(@valid_attrs, :method)
    cs = RequestLog.changeset(%RequestLog{}, attrs)
    refute cs.valid?
  end
end
```

```elixir
# test/api_gateway/tenant_scope_test.exs
defmodule ApiGateway.Tenant.ScopeTest do
  use ApiGateway.DataCase

  alias ApiGateway.Tenant.Scope
  alias ApiGateway.Client

  setup do
    ApiGateway.Repo.query!("CREATE SCHEMA IF NOT EXISTS tenant_test_co")
    ApiGateway.Repo.query!("""
      CREATE TABLE IF NOT EXISTS tenant_test_co.clients
        (LIKE public.clients INCLUDING ALL)
    """)
    on_exit(fn ->
      ApiGateway.Repo.query!("DROP SCHEMA tenant_test_co CASCADE")
    end)
    :ok
  end

  test "scope/2 applies the correct PostgreSQL schema prefix" do
    {sql, _} = ApiGateway.Repo.to_sql(:all, Scope.scope(Client, "test_co"))
    assert sql =~ ~s("tenant_test_co"."clients")
  end

  test "all/2 reads from the tenant schema" do
    ApiGateway.Repo.insert!(%Client{name: "Tenant Client", plan: :pro}, prefix: "tenant_test_co")

    results = Scope.all(Client, "test_co")
    assert length(results) == 1
    assert hd(results).name == "Tenant Client"
  end

  test "all/2 does not read from the default schema" do
    insert(:client, name: "Default Schema Client")

    results = Scope.all(Client, "test_co")
    assert results == []
  end

  test "scope/2 raises on unsafe tenant_id" do
    assert_raise ArgumentError, fn ->
      Scope.scope(Client, "../../etc/passwd")
    end

    assert_raise ArgumentError, fn ->
      Scope.scope(Client, "'; DROP TABLE clients; --")
    end
  end

  test "insert/2 writes to the tenant schema" do
    {:ok, client} =
      %Client{}
      |> Client.changeset(%{name: "New Tenant Client", plan: :free})
      |> Scope.insert("test_co")

    assert client.name == "New Tenant Client"
    assert Scope.all(Client, "test_co") |> length() == 1
    assert ApiGateway.Repo.all(Client) |> length() == 0
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/audit_event_test.exs \
         test/api_gateway/request_log_test.exs \
         test/api_gateway/tenant_scope_test.exs \
         --trace
```

---

## Trade-off analysis

| Pattern | When to use | When NOT to use |
|---------|-------------|-----------------|
| Polymorphic `auditable_type/id` | One event type targets many entity types | When referential integrity is critical -- use separate tables with FK constraints |
| `embeds_one` / `embeds_many` | Data queried only with parent; schema changes frequently | Data queried independently; needs FK constraints or its own indexes |
| `put_query_prefix/2` | Hard isolation between tenants; different data retention policies | Shared data across tenants; schema-per-tenant migration overhead is unacceptable |
| `preload:` keyword in query | Need to filter/order by the association | No filter needed -- use `preload/2` call for cleaner separation |
| `prepare_changes/1` | Single side effect tightly coupled to the DB write | Multiple independent side effects -- use `Ecto.Multi` for explicitness |

---

## Common production mistakes

**1. No composite index on `(auditable_type, auditable_id)`**
Every `Events.for/1` call does a full table scan without this index. Add the composite
index at migration time.

**2. `embeds_many` without `on_replace: :delete`**
The default `on_replace` behavior for embeds is `:raise`. Use `on_replace: :delete` to
allow the embedded list to be replaced via `cast_embed/3`.

**3. Building the tenant prefix from raw user input**
`put_query_prefix(query, "tenant_" <> conn.params["tenant"])` allows any string as a
prefix. Always validate the tenant ID against a strict regex before building the prefix.

**4. `preload: [request_logs: ^query]` with `limit` applies globally, not per-parent**
Adding `limit: 5` to a preload query applies `LIMIT 5` to the whole result set, not per
client. To get N rows per parent, use a window function: `ROW_NUMBER() OVER (PARTITION BY
client_id ORDER BY inserted_at DESC)` and filter `row_number <= 5` in a subquery.

**5. `prepare_changes/1` for ETS writes is not fully atomic**
`prepare_changes/1` runs inside the PostgreSQL transaction, but ETS writes are not
transactional. If the ETS write succeeds and then the DB transaction rolls back, the ETS
state is inconsistent. For ETS state derived from DB state, prefer updating ETS *after*
the `{:ok, result}` from `Repo.update/1`.

---

## Resources

- [`Ecto.Schema.embeds_one/3`](https://hexdocs.pm/ecto/Ecto.Schema.html#embeds_one/3) -- embedded schemas and JSONB
- [`Ecto.Query.put_query_prefix/2`](https://hexdocs.pm/ecto/Ecto.Query.html#put_query_prefix/2) -- schema-per-tenant queries
- [`Ecto.Changeset.prepare_changes/2`](https://hexdocs.pm/ecto/Ecto.Changeset.html#prepare_changes/2) -- transactional side effects
- [`Ecto.Repo.preload/3`](https://hexdocs.pm/ecto/Ecto.Repo.html#c:preload/3) -- batch preloading with custom queries
- [PostgreSQL schemas](https://www.postgresql.org/docs/current/ddl-schemas.html) -- multi-tenancy isolation
- [Triplex](https://hexdocs.pm/triplex/readme.html) -- library for schema-per-tenant migrations
