# Advanced Ecto Schemas

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` umbrella now has data flowing through it: request logs, billing records,
client configurations. Over time, the schema layer accumulates patterns the basics don't
cover: audit events that point to multiple entity types, request metadata that lives inside
its row rather than a joined table, per-client data that must never leak across boundaries,
and preload chains that generate hundreds of queries if left unchecked.

All schema work in this exercise lives in `gateway_core`.

Project structure:

```
api_gateway_umbrella/apps/gateway_core/
├── lib/gateway_core/
│   ├── audit/
│   │   ├── event.ex           # ← you implement this
│   │   └── events.ex          # ← and this
│   ├── request_log.ex         # ← embedded metadata
│   ├── tenant/
│   │   └── scope.ex           # ← multi-tenancy helpers
│   └── analytics/
│       └── preloader.ex       # ← N+1 prevention for the dashboard
└── test/gateway_core/
    ├── audit_event_test.exs   # given tests
    ├── request_log_test.exs   # given tests
    └── tenant_scope_test.exs  # given tests
```

---

## Why these patterns matter in production

- **Polymorphic associations**: gateway audit events need to reference clients, workers,
  and webhook endpoints — all different tables. Creating `client_audit_events`,
  `worker_audit_events`, `webhook_audit_events` triples the schema for no semantic gain.
  One `audit_events` table with `auditable_type` / `auditable_id` handles all three.

- **`embeds_one` / `embeds_many`**: request metadata (headers, query params, TLS info)
  changes shape frequently. A separate `request_metadata` table means every request log
  read needs a JOIN. Storing it as JSONB with an `embedded_schema` gives full Ecto
  validation without the join — and preserves a historical snapshot that won't shift if the
  schema evolves.

- **`put_query_prefix/2`**: enterprise clients on the gateway get isolated PostgreSQL
  schemas (`tenant_acme`, `tenant_globex`). A missing prefix on any query reads or writes
  the wrong tenant's data — silently. The `Scope` module wraps every repo call with the
  correct prefix, making accidental cross-tenant reads structurally impossible.

- **Preload optimization**: the admin dashboard loads clients, their last 5 request logs,
  and their billing entries. Without explicit preloads, Ecto issues one query per client to
  fetch logs (N+1). With `join` + named preload, it's two queries total regardless of
  client count.

---

## Implementation

### Part 1: Polymorphic audit events

The gateway needs to audit: who acted, on what, and when. "What" can be a `Client`, a
`Worker`, or a `WebhookEndpoint`. Ecto does not expose polymorphic associations as a
first-class concept — implement it with two fields and a query module.

```elixir
# lib/gateway_core/audit/event.ex
defmodule GatewayCore.Audit.Event do
  use Ecto.Schema
  import Ecto.Changeset

  @auditable_types ["Client", "Worker", "WebhookEndpoint"]

  schema "audit_events" do
    field :action,         :string         # "created" | "updated" | "deleted" | "rate_limited"
    field :actor_id,       :integer        # who triggered the action (client or internal worker)
    field :actor_type,     :string         # "Client" | "system"
    field :auditable_id,   :integer        # the entity being audited
    field :auditable_type, :string         # "Client" | "Worker" | "WebhookEndpoint"
    field :metadata,       :map, default: %{}   # arbitrary context (e.g., %{old_plan: "free"})
    timestamps(updated_at: false)          # audit events are immutable — no updated_at
  end

  @required_fields ~w(action auditable_id auditable_type)a
  @optional_fields ~w(actor_id actor_type metadata)a

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

-- Composite index: most queries filter by entity
CREATE INDEX audit_events_on_auditable ON audit_events (auditable_type, auditable_id);
-- Time-range queries for the dashboard
CREATE INDEX audit_events_on_inserted_at ON audit_events (inserted_at DESC);
```

#### Query module — never let callers build raw polymorphic queries

```elixir
# lib/gateway_core/audit/events.ex
defmodule GatewayCore.Audit.Events do
  import Ecto.Query
  alias GatewayCore.{Repo, Audit.Event}

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

    # TODO: query Event where auditable_type == type and auditable_id == auditable.id
    # Order by inserted_at DESC, apply limit
    # HINT: from(e in Event, where: e.auditable_type == ^type and e.auditable_id == ^auditable.id,
    #             order_by: [desc: e.inserted_at], limit: ^limit)
    # |> Repo.all()
  end

  @doc """
  Records a new audit event. Raises on validation failure.
  Audit events are fire-and-forget — use `record!/4` so failures are loud, not silent.
  """
  @spec record!(struct(), String.t(), map()) :: Event.t()
  def record!(auditable, action, metadata \\ %{}) do
    type = auditable.__struct__ |> Module.split() |> List.last()

    # TODO: build an Event changeset with auditable_type, auditable_id, action, metadata
    # HINT: %Event{}
    #       |> Event.changeset(%{auditable_type: type, auditable_id: auditable.id, action: action, metadata: metadata})
    #       |> Repo.insert!()
  end

  @doc """
  Returns the count of events per action for a given entity (used in the dashboard).
  """
  @spec action_summary(struct()) :: [%{action: String.t(), count: integer()}]
  def action_summary(auditable) do
    type = auditable.__struct__ |> Module.split() |> List.last()

    # TODO: group_by :action, select %{action: e.action, count: count(e.id)}
    # HINT: from(e in Event,
    #         where: e.auditable_type == ^type and e.auditable_id == ^auditable.id,
    #         group_by: e.action,
    #         select: %{action: e.action, count: count(e.id)})
    # |> Repo.all()
  end
end
```

---

### Part 2: Embedded request metadata

Each request log stores HTTP metadata alongside the row. This metadata is queried only
when the full request detail is needed — never filtered or joined. It does not deserve a
table of its own.

```elixir
# lib/gateway_core/request_log.ex
defmodule GatewayCore.RequestLog do
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

    # Stored as JSONB — no join needed to read full request context
    embeds_one :metadata, GatewayCore.RequestMetadata, on_replace: :update

    timestamps()
  end

  def changeset(log, attrs) do
    log
    |> cast(attrs, [:client_id, :method, :path, :status, :duration_ms, :bytes_transferred])
    |> validate_required([:client_id, :method, :path, :status])
    |> validate_inclusion(:method, ~w(GET POST PUT PATCH DELETE HEAD OPTIONS))
    |> validate_number(:status, greater_than_or_equal_to: 100, less_than: 600)
    |> cast_embed(:metadata)  # delegates to RequestMetadata.changeset/2
  end
end

defmodule GatewayCore.RequestMetadata do
  use Ecto.Schema
  import Ecto.Changeset

  # embedded_schema: no table — lives as JSONB in request_logs.metadata
  embedded_schema do
    field :user_agent,   :string
    field :remote_ip,    :string
    field :tls_version,  :string
    field :request_id,   :string
    field :referer,      :string
    # Headers that matter for rate limiting and billing
    embeds_many :custom_headers, GatewayCore.RequestHeader, on_replace: :delete
  end

  def changeset(meta, attrs) do
    meta
    |> cast(attrs, [:user_agent, :remote_ip, :tls_version, :request_id, :referer])
    |> cast_embed(:custom_headers)
    |> validate_format(:remote_ip, ~r/^\d{1,3}(\.\d{1,3}){3}$|^[0-9a-f:]+$/i)
    |> validate_inclusion(:tls_version, ["TLSv1.2", "TLSv1.3", nil])
  end
end

defmodule GatewayCore.RequestHeader do
  use Ecto.Schema
  import Ecto.Changeset

  embedded_schema do
    field :name,  :string
    field :value, :string
  end

  def changeset(header, attrs) do
    header
    |> cast(attrs, [:name, :value])
    |> validate_required([:name, :value])
    # TODO: validate :name is lowercase (HTTP/2 header convention)
    # HINT: validate_format(:name, ~r/^[a-z][a-z0-9-]*$/)
  end
end
```

```sql
-- Migration: metadata column is JSONB, not a separate table
ALTER TABLE request_logs ADD COLUMN metadata JSONB;
-- GIN index enables efficient filtering inside the JSONB (e.g., by remote_ip)
CREATE INDEX request_logs_metadata_gin ON request_logs USING GIN (metadata);
```

The embedded schema provides full Ecto validation without a table join:

```elixir
# iex -S mix
attrs = %{
  client_id: 1, method: "GET", path: "/api/v1/users", status: 200,
  duration_ms: 45, bytes_transferred: 1_024,
  metadata: %{
    remote_ip: "10.0.0.1", tls_version: "TLSv1.3",
    user_agent: "GatewayClient/2.0",
    custom_headers: [%{name: "x-request-id", value: "abc-123"}]
  }
}

changeset = GatewayCore.RequestLog.changeset(%GatewayCore.RequestLog{}, attrs)
changeset.valid?   # true — metadata validated recursively

# Invalid TLS version surfaces in the embedded changeset:
bad = put_in(attrs, [:metadata, :tls_version], "SSLv3")
bad_cs = GatewayCore.RequestLog.changeset(%GatewayCore.RequestLog{}, bad)
bad_cs.changes.metadata.errors
# [tls_version: {"is invalid", [validation: :inclusion, ...]}]
```

---

### Part 3: Multi-tenancy with `put_query_prefix/2`

Enterprise clients on the gateway get their own PostgreSQL schema. All queries for a
tenant must target their schema — a query missing the prefix silently reads the wrong data.

```elixir
# lib/gateway_core/tenant/scope.ex
defmodule GatewayCore.Tenant.Scope do
  import Ecto.Query
  alias GatewayCore.Repo

  @valid_tenant_pattern ~r/^[a-z][a-z0-9_]{0,62}$/

  @doc """
  Applies the tenant prefix to an Ecto query.
  Raises ArgumentError if the tenant ID does not match the safe pattern.
  Never pass user input directly — validate at the controller/socket boundary first.
  """
  @spec scope(Ecto.Queryable.t(), String.t()) :: Ecto.Query.t()
  def scope(query, tenant_id) do
    # TODO: validate tenant_id matches @valid_tenant_pattern (prevent injection)
    # HINT: unless Regex.match?(@valid_tenant_pattern, tenant_id) do
    #         raise ArgumentError, "unsafe tenant_id: #{inspect(tenant_id)}"
    #       end
    # Then: put_query_prefix(query, "tenant_#{tenant_id}")
  end

  @doc "Repo.all scoped to a tenant."
  @spec all(Ecto.Queryable.t(), String.t()) :: [struct()]
  def all(query, tenant_id) do
    # TODO: scope(query, tenant_id) |> Repo.all()
  end

  @doc "Repo.get scoped to a tenant."
  @spec get(module(), integer(), String.t()) :: struct() | nil
  def get(schema, id, tenant_id) do
    # TODO: Repo.get(schema, id, prefix: "tenant_#{tenant_id}")
    # NOTE: validate tenant_id here too — prefix: option is not validated by Ecto
  end

  @doc "Repo.insert scoped to a tenant."
  @spec insert(Ecto.Changeset.t(), String.t()) :: {:ok, struct()} | {:error, Ecto.Changeset.t()}
  def insert(changeset, tenant_id) do
    # TODO: Repo.insert(changeset, prefix: "tenant_#{tenant_id}")
  end

  @doc "Repo.update scoped to a tenant."
  @spec update(Ecto.Changeset.t(), String.t()) :: {:ok, struct()} | {:error, Ecto.Changeset.t()}
  def update(changeset, tenant_id) do
    # TODO: Repo.update(changeset, prefix: "tenant_#{tenant_id}")
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
# lib/gateway_api_web/plugs/tenant_plug.ex
defmodule GatewayApiWeb.TenantPlug do
  import Plug.Conn
  alias GatewayCore.Tenant.Scope

  def init(opts), do: opts

  def call(conn, _opts) do
    tenant_id = get_req_header(conn, "x-tenant-id") |> List.first()

    cond do
      is_nil(tenant_id) ->
        conn |> send_resp(400, "Missing X-Tenant-Id header") |> halt()

      # TODO: validate tenant exists in the tenants registry table
      # HINT: GatewayCore.Tenants.exists?(tenant_id) — prevents schema enumeration
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

The dashboard loads all clients with their last 5 request logs and current billing summary.
Without explicit preloads, Ecto issues N+1 queries — one per client to fetch logs, one per
client to fetch billing. With named preloads, it's three queries regardless of client count.

```elixir
# lib/gateway_core/analytics/preloader.ex
defmodule GatewayCore.Analytics.Preloader do
  import Ecto.Query
  alias GatewayCore.{Repo, Client, RequestLog, BillingEntry}

  @doc """
  Loads all clients for the dashboard with their recent request logs preloaded.

  Uses two queries total:
    1. SELECT * FROM clients ORDER BY name
    2. SELECT * FROM request_logs WHERE client_id IN (...) ORDER BY inserted_at DESC

  Without preload, the template iterating over clients.request_logs triggers
  one query per client — the classic N+1.
  """
  @spec clients_with_recent_logs(integer()) :: [Client.t()]
  def clients_with_recent_logs(log_limit \\ 5) do
    # The preload query is scoped per-client via Ecto's batch preload mechanism.
    # Ecto fetches all client IDs in one query, then runs one query for all logs
    # filtered by those IDs — not one query per client.
    recent_logs =
      from(r in RequestLog,
        # TODO: order by inserted_at DESC, limit ^log_limit
        # IMPORTANT: Ecto's preload with a query applies the limit PER CLIENT when
        # the preload is a keyword list: preload: [request_logs: ^recent_logs]
        # This uses a window function internally — verify with Repo.to_sql/2
        order_by: [desc: r.inserted_at],
        limit: ^log_limit
      )

    # TODO: from(c in Client, order_by: c.name, preload: [request_logs: ^recent_logs])
    # |> Repo.all()
  end

  @doc """
  Loads a single client with all associations needed for the detail page.

  Uses a JOIN for billing (needed for the WHERE clause) and a separate
  preload for request_logs (no filter needed — 2-query approach is more efficient).
  """
  @spec client_detail(integer()) :: Client.t() | nil
  def client_detail(client_id) do
    # TODO:
    # 1. join billing_entries (needed to filter/sort by billing data)
    # 2. preload request_logs separately (no filter on request_logs needed)
    # HINT: from(c in Client,
    #         left_join: b in assoc(c, :billing_entries), as: :billing,
    #         where: c.id == ^client_id,
    #         preload: [billing_entries: :billing, request_logs: ^recent_logs_query])
    # |> Repo.one()
  end

  @doc """
  Detects potential N+1: returns true if the struct has an unloaded association.
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

When a client's plan changes in the gateway, the rate limiter ETS table must be updated
atomically — if the DB write fails, the ETS write must not happen, and vice versa.
`prepare_changes/1` runs inside the changeset's transaction.

```elixir
# lib/gateway_core/client.ex (addition to existing schema)
defmodule GatewayCore.Client do
  use Ecto.Schema
  import Ecto.Changeset
  import Ecto.Query

  schema "clients" do
    field :name,             :string
    field :plan,             Ecto.Enum, values: [:free, :pro, :enterprise]
    field :active,           :boolean, default: true
    field :quota_remaining,  :integer
    has_many :request_logs,   GatewayCore.RequestLog
    has_many :billing_entries, GatewayCore.BillingEntry
    timestamps()
  end

  def changeset(client, attrs) do
    client
    |> cast(attrs, [:name, :plan, :active, :quota_remaining])
    |> validate_required([:name, :plan])
  end

  @doc """
  Changeset for plan upgrades. Records an audit event and refreshes the
  rate limiter ETS table inside the same transaction.

  prepare_changes/1 runs ONLY if the changeset is valid AND inside the DB transaction.
  If Repo.update fails (e.g., unique constraint), the side effects never execute.
  """
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

  # TODO: implement record_plan_change_audit/1
  # Must use changeset.repo (the repo is injected during the transaction)
  # HINT: defp record_plan_change_audit(changeset) do
  #         old_plan = changeset.data.plan
  #         new_plan = get_change(changeset, :plan)
  #         if new_plan && new_plan != old_plan do
  #           changeset.repo.insert!(%GatewayCore.Audit.Event{
  #             auditable_type: "Client",
  #             auditable_id: changeset.data.id,
  #             action: "plan_upgraded",
  #             metadata: %{from: old_plan, to: new_plan}
  #           })
  #         end
  #         changeset  # ALWAYS return the changeset
  #       end

  # TODO: implement refresh_rate_limiter/1
  # Updates the in-memory ETS rate limiter with the new plan's quota
  # HINT: defp refresh_rate_limiter(changeset) do
  #         if new_plan = get_change(changeset, :plan) do
  #           :ets.insert(:rate_limiter_config, {changeset.data.id, quota_for(new_plan)})
  #         end
  #         changeset
  #       end

  defp plan_rank(:free),       do: 1
  defp plan_rank(:pro),        do: 2
  defp plan_rank(:enterprise), do: 3

  defp quota_for(:free),       do: 1_000
  defp quota_for(:pro),        do: 50_000
  defp quota_for(:enterprise), do: :unlimited
end
```

---

### Step 6: Given tests — must pass without modification

```elixir
# test/gateway_core/audit_event_test.exs
defmodule GatewayCore.Audit.EventTest do
  use GatewayCore.DataCase

  alias GatewayCore.Audit.{Event, Events}

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
# test/gateway_core/request_log_test.exs
defmodule GatewayCore.RequestLogTest do
  use GatewayCore.DataCase

  alias GatewayCore.RequestLog

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
    |> GatewayCore.Repo.insert()

    loaded = GatewayCore.Repo.get!(RequestLog, log.id)
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
# test/gateway_core/tenant_scope_test.exs
defmodule GatewayCore.Tenant.ScopeTest do
  use GatewayCore.DataCase

  alias GatewayCore.Tenant.Scope
  alias GatewayCore.Client

  setup do
    # Create the tenant schema for tests
    GatewayCore.Repo.query!("CREATE SCHEMA IF NOT EXISTS tenant_test_co")
    GatewayCore.Repo.query!("""
      CREATE TABLE IF NOT EXISTS tenant_test_co.clients
        (LIKE public.clients INCLUDING ALL)
    """)
    on_exit(fn ->
      GatewayCore.Repo.query!("DROP SCHEMA tenant_test_co CASCADE")
    end)
    :ok
  end

  test "scope/2 applies the correct PostgreSQL schema prefix" do
    {sql, _} = GatewayCore.Repo.to_sql(:all, Scope.scope(Client, "test_co"))
    assert sql =~ ~s("tenant_test_co"."clients")
  end

  test "all/2 reads from the tenant schema" do
    # Insert directly into tenant schema
    GatewayCore.Repo.insert!(%Client{name: "Tenant Client", plan: :pro}, prefix: "tenant_test_co")

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
    # Verify it's in the tenant schema, not the default
    assert Scope.all(Client, "test_co") |> length() == 1
    assert GatewayCore.Repo.all(Client) |> length() == 0
  end
end
```

### Step 7: Run the tests

```bash
mix test test/gateway_core/audit_event_test.exs \
         test/gateway_core/request_log_test.exs \
         test/gateway_core/tenant_scope_test.exs \
         --trace
```

Debug the SQL generated for any query:

```elixir
# In iex -S mix:
alias GatewayCore.{Client, Tenant.Scope}

# Verify prefix is applied:
{sql, _} = GatewayCore.Repo.to_sql(:all, Scope.scope(Client, "acme_corp"))
IO.puts(sql)
# SELECT c0."id", ... FROM "tenant_acme_corp"."clients" AS c0

# Verify preload doesn't generate N+1:
# Enable query logging and count queries during a Preloader.clients_with_recent_logs() call
```

---

## Trade-off analysis

| Pattern | When to use | When NOT to use |
|---------|-------------|-----------------|
| Polymorphic `auditable_type/id` | One event type targets many entity types | When referential integrity is critical — use separate tables with FK constraints |
| `embeds_one` / `embeds_many` | Data queried only with parent; schema changes frequently | Data queried independently; needs FK constraints or its own indexes |
| `put_query_prefix/2` | Hard isolation between tenants; different data retention policies | Shared data across tenants; schema-per-tenant migration overhead is unacceptable |
| `preload:` keyword in query | Need to filter/order by the association | No filter needed — use `preload/2` call for cleaner separation |
| `prepare_changes/1` | Single side effect tightly coupled to the DB write | Multiple independent side effects — use `Ecto.Multi` for explicitness |

---

## Common production mistakes

**1. No composite index on `(auditable_type, auditable_id)`**
Every `Events.for/1` call does a full table scan without this index. Audit tables grow fast
(every API call can produce an event). Add the composite index at migration time — adding
it after millions of rows requires a concurrent index build (`CONCURRENTLY`) to avoid locking.

**2. `embeds_many` without `on_replace: :delete`**
The default `on_replace` behavior for embeds is `:raise` — Ecto raises if you try to
replace the embedded list without specifying. Use `on_replace: :delete` to allow the
embedded list to be replaced wholesale via `cast_embed/3`.

**3. Building the tenant prefix from raw user input**
`put_query_prefix(query, "tenant_" <> conn.params["tenant"])` allows any string as a
prefix. A malicious user passes `public` to read the main schema. Always validate the
tenant ID against an allowlist or a strict regex before building the prefix.

**4. `preload: [request_logs: ^query]` with `limit` applies globally, not per-parent**
A common mistake: adding `limit: 5` to the preload query expecting 5 logs per client. Ecto
fetches all clients' logs in one `WHERE id IN (...)` query — `LIMIT 5` applies to the
whole result set, not per client. To get N rows per parent, use a window function:
`ROW_NUMBER() OVER (PARTITION BY client_id ORDER BY inserted_at DESC)` and filter
`row_number <= 5` in a subquery.

**5. `prepare_changes/1` for ETS writes is not fully atomic**
`prepare_changes/1` runs inside the PostgreSQL transaction, but ETS writes are not
transactional. If the ETS write succeeds and then the DB transaction rolls back (due to a
constraint violation in a subsequent changeset), the ETS state is now inconsistent. For
ETS state derived from DB state, prefer updating ETS *after* the `{:ok, result}` from
`Repo.update/1` — not inside `prepare_changes/1`.

---

## Resources

- [`Ecto.Schema.embeds_one/3`](https://hexdocs.pm/ecto/Ecto.Schema.html#embeds_one/3) — embedded schemas and JSONB storage
- [`Ecto.Query.put_query_prefix/2`](https://hexdocs.pm/ecto/Ecto.Query.html#put_query_prefix/2) — schema-per-tenant queries
- [`Ecto.Changeset.prepare_changes/2`](https://hexdocs.pm/ecto/Ecto.Changeset.html#prepare_changes/2) — transactional side effects
- [`Ecto.Repo.preload/3`](https://hexdocs.pm/ecto/Ecto.Repo.html#c:preload/3) — batch preloading with custom queries
- [PostgreSQL schemas (multi-tenancy)](https://www.postgresql.org/docs/current/ddl-schemas.html) — `search_path` and schema isolation
- [Triplex](https://hexdocs.pm/triplex/readme.html) — library that automates schema-per-tenant migrations in Ecto
