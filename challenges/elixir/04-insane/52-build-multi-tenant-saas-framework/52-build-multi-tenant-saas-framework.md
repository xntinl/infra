# 52. Build a Multi-tenant SaaS Framework

## Context

Your team is building a B2B SaaS product: a project management tool sold to companies. Each company (tenant) has its own users, projects, and data. The first version used a single `tenant_id` column on every table. It worked, but a bug in one query caused tenant A's data to appear in tenant B's response. The data leak cost two enterprise contracts.

The team decides to rebuild with strict isolation: PostgreSQL Row Level Security policies that make it physically impossible for the database to return tenant A's rows to a connection scoped to tenant B. Feature flags per tenant for gradual rollout. Stripe billing integrated with plan enforcement. Provisioning that creates a tenant in the database, runs migrations, and creates a Stripe customer atomically — or rolls everything back on failure.

You will build `TenantFramework`: the foundational layer for a multi-tenant SaaS application.

## Why RLS over schema-per-tenant for most deployments

Schema-per-tenant gives the strongest isolation: each tenant's data is in a separate PostgreSQL schema. But it has operational costs: running migrations requires iterating over N schemas, connection pooling by schema is complex, and at 10,000 tenants, the schema count becomes unwieldy.

RLS (Row Level Security) uses a single schema with a `tenant_id` column and database-enforced policies. The policy `CREATE POLICY tenant_isolation ON projects USING (tenant_id = current_setting('app.current_tenant')::uuid)` makes it impossible for a query on connection A to see rows belonging to a different tenant — even if the application code has a bug. The policy is enforced by the database engine, below application code.

The trade-off: schema-per-tenant provides complete table-level isolation (one tenant's table can be vacuumed, backed up, or deleted independently); RLS provides row-level isolation with simpler operations.

## Why ETS for rate limiting and feature flags at request time

At 10k requests/second, a GenServer rate limiter is a single-process bottleneck. ETS with `:ets.update_counter/3` provides atomic increment/decrement without a process boundary. Feature flag evaluation at request time also must be sub-millisecond — an ETS lookup is ~100ns. A database query is ~5ms. Serving 10k requests/second with per-request flag evaluation from the database consumes the entire latency budget on flag lookups alone.

## Why atomic provisioning matters

Tenant provisioning touches three systems: the database (create schema or seed rows), run migrations, and Stripe (create customer). If Stripe fails after the database is set up, you have an orphaned tenant with no billing customer. You cannot easily roll back the database at this point. The solution: run the database operations inside a PostgreSQL transaction; create the Stripe customer only if the database transaction is ready to commit; if Stripe fails, roll back the transaction. The Stripe operation is the last step and is not inside the database transaction (Stripe is not a database), but the database state is never committed if Stripe fails.

## Project Structure

```
tenant_framework/
├── mix.exs
├── priv/
│   └── repo/
│       └── migrations/
├── lib/
│   └── tenant_framework/
│       ├── tenant.ex              # Tenant schema: id, slug, plan, status, stripe_customer_id
│       ├── repo.ex                # Custom Ecto.Repo that sets app.current_tenant on every checkout
│       ├── plug/
│       │   ├── tenant_resolver.ex # Resolve tenant from subdomain/header/JWT
│       │   └── rate_limiter.ex    # Token bucket per tenant in ETS
│       ├── provisioning.ex        # Atomic create: schema/RLS + migrations + seed + Stripe
│       ├── flags/
│       │   ├── flag.ex            # Flag schema: name, enabled, rollout_pct
│       │   ├── cache.ex           # ETS cache + PubSub subscriber
│       │   └── evaluator.ex       # Flag evaluation: consistent hash for rollout
│       ├── metering/
│       │   ├── counter.ex         # ETS counters per tenant per metric
│       │   └── flusher.ex         # Periodic flush to PostgreSQL
│       ├── billing/
│       │   ├── webhook_handler.ex # Stripe webhook signature verification + routing
│       │   └── plan_enforcer.ex   # Check usage against plan limits at request time
│       └── admin/
│           ├── tenant_list.ex     # Admin API: list, suspend, impersonate
│           └── impersonation.ex   # Switch tenant context for admin session
├── test/
│   ├── isolation_test.exs         # Property-based: data never leaks cross-tenant
│   ├── provisioning_test.exs      # Atomic rollback on Stripe failure
│   ├── rate_limiter_test.exs
│   ├── flags_test.exs
│   └── billing_test.exs
└── bench/
    └── resolution_overhead.exs
```

## Step 1 — Custom Ecto.Repo with tenant scoping

```elixir
defmodule TenantFramework.Repo do
  use Ecto.Repo,
    otp_app: :tenant_framework,
    adapter: Ecto.Adapters.Postgres

  @doc """
  Checkout a connection and SET LOCAL app.current_tenant for RLS.
  All queries within the checkout use the tenant context.
  """
  def checkout_with_tenant(tenant_id, fun) do
    checkout(fn ->
      query("SET LOCAL app.current_tenant = $1", [to_string(tenant_id)])
      fun.()
    end)
  end

  @doc "Execute a query scoped to the current tenant (from process dictionary)"
  def execute_in_tenant(fun) do
    tenant_id = Process.get(:current_tenant_id) ||
      raise "No tenant context. Call Plug.TenantResolver before executing queries."
    checkout_with_tenant(tenant_id, fun)
  end
end
```

## Step 2 — Tenant resolution plug

```elixir
defmodule TenantFramework.Plug.TenantResolver do
  import Plug.Conn
  alias TenantFramework.Tenant

  @doc "Resolve tenant from subdomain, X-Tenant-ID header, or JWT claim"
  def init(opts), do: opts

  def call(conn, _opts) do
    case resolve(conn) do
      {:ok, tenant} ->
        # Store in process dictionary for Repo and downstream plugs
        Process.put(:current_tenant_id, tenant.id)
        Process.put(:current_tenant, tenant)
        assign(conn, :current_tenant, tenant)
      {:error, :not_found} ->
        conn |> send_resp(404, "Tenant not found") |> halt()
      {:error, :suspended} ->
        conn |> send_resp(403, "Tenant suspended") |> halt()
    end
  end

  defp resolve(conn) do
    cond do
      slug = subdomain(conn) -> lookup_by_slug(slug)
      header = get_req_header(conn, "x-tenant-id") |> List.first() -> lookup_by_slug(header)
      token = get_jwt_tenant(conn) -> lookup_by_id(token)
      true -> {:error, :not_found}
    end
  end

  defp subdomain(conn) do
    host = conn.host
    # Extract "acme" from "acme.app.com"
    case String.split(host, ".") do
      [sub | _rest] when sub not in ["www", "app", "api"] -> sub
      _ -> nil
    end
  end

  defp get_jwt_tenant(conn) do
    with [auth | _] <- get_req_header(conn, "authorization"),
         "Bearer " <> token <- auth,
         {:ok, claims} <- verify_jwt(token),
         tenant_id when not is_nil(tenant_id) <- claims["tenant"] do
      tenant_id
    else
      _ -> nil
    end
  end

  defp verify_jwt(token) do
    # TODO: verify HS256 JWT using application secret
    # HINT: reuse JWT verification from exercise 34 (API Gateway)
    {:error, :not_implemented}
  end

  defp lookup_by_slug(slug) do
    case TenantFramework.Repo.get_by(Tenant, slug: slug) do
      nil -> {:error, :not_found}
      %{status: "suspended"} = t -> {:error, :suspended}
      tenant -> {:ok, tenant}
    end
  end

  defp lookup_by_id(id) do
    case TenantFramework.Repo.get(Tenant, id) do
      nil -> {:error, :not_found}
      %{status: "suspended"} -> {:error, :suspended}
      tenant -> {:ok, tenant}
    end
  end
end
```

## Step 3 — Tenant provisioning

```elixir
defmodule TenantFramework.Provisioning do
  alias TenantFramework.{Repo, Tenant}

  @doc """
  Create a tenant atomically.
  Steps (all or nothing):
  1. Insert tenant row (in transaction)
  2. Create Stripe customer (outside DB transaction, last step)
  3. If Stripe fails: rollback DB
  """
  def create_tenant(attrs) do
    # Phase 1: all database work in a transaction
    db_result = Repo.transaction(fn ->
      with {:ok, tenant} <- insert_tenant(attrs),
           :ok <- setup_rls_seed(tenant),
           {:ok, tenant} <- seed_default_data(tenant) do
        tenant
      else
        {:error, reason} -> Repo.rollback(reason)
      end
    end)

    case db_result do
      {:ok, tenant} ->
        # Phase 2: Stripe (outside transaction; if this fails, rollback DB manually)
        case create_stripe_customer(tenant) do
          {:ok, stripe_customer_id} ->
            Repo.update!(Tenant.changeset(tenant, %{stripe_customer_id: stripe_customer_id}))
            {:ok, Repo.get!(Tenant, tenant.id)}
          {:error, reason} ->
            # Roll back the tenant creation
            Repo.delete!(tenant)
            {:error, {:stripe_failed, reason}}
        end
      {:error, reason} ->
        {:error, reason}
    end
  end

  defp insert_tenant(attrs) do
    %Tenant{}
    |> Tenant.changeset(attrs)
    |> Repo.insert()
  end

  defp setup_rls_seed(tenant) do
    # For RLS mode: no schema creation needed; the tenant_id in rows is the isolation mechanism
    # For schema mode: create and migrate the tenant schema here
    # TODO: insert default settings row for this tenant
    :ok
  end

  defp seed_default_data(tenant) do
    # TODO: insert default roles: [:admin, :member, :viewer]
    # TODO: insert admin user from attrs
    {:ok, tenant}
  end

  defp create_stripe_customer(tenant) do
    # TODO: HTTP POST to Stripe /v1/customers with tenant email and metadata
    # TODO: return {:ok, customer_id} or {:error, reason}
    {:ok, "cus_test_#{tenant.id}"}
  end
end
```

## Step 4 — Rate limiter

```elixir
defmodule TenantFramework.Plug.RateLimiter do
  import Plug.Conn
  @table :tenant_rate_limiter

  def init(opts), do: opts

  def call(conn, _opts) do
    tenant = conn.assigns[:current_tenant]
    if tenant && rate_limited?(tenant) do
      conn
      |> put_resp_header("retry-after", "1")
      |> send_resp(429, "Rate limit exceeded")
      |> halt()
    else
      conn
    end
  end

  @doc "Returns true if tenant has exceeded their rate limit"
  def rate_limited?(tenant) do
    limit = tenant.plan_rate_limit || 1000  # requests per second
    window_ms = 1000
    now_window = div(System.monotonic_time(:millisecond), window_ms)
    key = {tenant.id, now_window}

    # Atomic increment: if result > limit, rate limited
    count = :ets.update_counter(@table, key, {2, 1, limit + 1, limit + 1},
                                {key, 0})
    count > limit
  end

  def init_table do
    :ets.new(@table, [:named_table, :public, :set, {:write_concurrency, true}])
  end
end
```

## Step 5 — Feature flags

```elixir
defmodule TenantFramework.Flags.Cache do
  use GenServer
  alias Phoenix.PubSub

  @table :feature_flags

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, %{})
  end

  def init(_) do
    :ets.new(@table, [:named_table, :public, :set])
    PubSub.subscribe(TenantFramework.PubSub, "feature_flags:updates")
    reload_all_flags()
    {:ok, %{}}
  end

  def handle_info({:flag_updated, flag}, state) do
    :ets.insert(@table, {flag.name, flag})
    {:noreply, state}
  end

  defp reload_all_flags do
    # TODO: load all flags from database, insert into ETS
    :ok
  end
end

defmodule TenantFramework.Flags.Evaluator do
  @table :feature_flags

  @doc "Evaluate a feature flag for a tenant. O(1) ETS lookup."
  def enabled?(flag_name, tenant_id) do
    case :ets.lookup(@table, flag_name) do
      [{_, flag}] -> evaluate_flag(flag, tenant_id)
      [] -> false  # Unknown flags default to disabled
    end
  end

  defp evaluate_flag(%{enabled: false}, _tenant_id), do: false
  defp evaluate_flag(%{enabled: true, rollout_pct: 100}, _tenant_id), do: true
  defp evaluate_flag(%{enabled: true, rollout_pct: pct}, tenant_id) do
    # Consistent hash: same tenant always gets the same variant
    hash = :erlang.phash2(tenant_id, 100)
    hash < pct
  end
end
```

## Step 6 — Billing webhook handler

```elixir
defmodule TenantFramework.Billing.WebhookHandler do
  import Plug.Conn

  @stripe_tolerance_seconds 300

  def handle(conn) do
    with {:ok, body, conn} <- Plug.Conn.read_body(conn),
         sig = get_req_header(conn, "stripe-signature") |> List.first(),
         :ok <- verify_signature(body, sig),
         {:ok, event} <- Jason.decode(body),
         :ok <- process_event(event) do
      send_resp(conn, 200, "ok")
    else
      {:error, :invalid_signature} ->
        # Log full payload for audit
        send_resp(conn, 400, "invalid signature")
      {:error, :already_processed} ->
        send_resp(conn, 200, "already processed")
      {:error, reason} ->
        send_resp(conn, 500, inspect(reason))
    end
  end

  defp verify_signature(body, sig_header) do
    secret = Application.get_env(:tenant_framework, :stripe_webhook_secret)
    # TODO: parse sig_header for "t=timestamp,v1=hash"
    # TODO: compute HMAC-SHA256 over "timestamp.body" with secret
    # TODO: compare with v1 hash using constant-time comparison
    # TODO: verify timestamp is within @stripe_tolerance_seconds of now
    :ok
  end

  defp process_event(%{"id" => event_id, "type" => type} = event) do
    # Idempotency: check if event was already processed
    case TenantFramework.Repo.get_by(TenantFramework.ProcessedEvent, stripe_event_id: event_id) do
      nil ->
        handle_event_type(type, event)
        TenantFramework.Repo.insert!(%TenantFramework.ProcessedEvent{stripe_event_id: event_id})
        :ok
      _existing ->
        {:error, :already_processed}
    end
  end

  defp handle_event_type("customer.subscription.updated", event) do
    # TODO: extract tenant by stripe_customer_id
    # TODO: update tenant plan to new plan from event
    # TODO: invalidate rate limit cache if limits changed
    :ok
  end

  defp handle_event_type("customer.subscription.deleted", event) do
    # TODO: downgrade tenant to free plan
    :ok
  end

  defp handle_event_type("invoice.paid", event) do
    # TODO: reset monthly usage counters if applicable
    :ok
  end

  defp handle_event_type(_type, _event), do: :ok
end
```

## Given tests

```elixir
# test/isolation_test.exs
defmodule TenantFramework.IsolationTest do
  use ExUnit.Case, async: false
  use ExUnitProperties
  alias TenantFramework.{Repo, Provisioning}

  property "data inserted under tenant A is invisible from tenant B" do
    check all(
      tenant_a_name <- string(:alphanumeric, min_length: 5),
      tenant_b_name <- string(:alphanumeric, min_length: 5),
      value <- string(:printable),
      min_runs: 20
    ) do
      {:ok, tenant_a} = Provisioning.create_tenant(%{slug: "a-#{tenant_a_name}", email: "a@test.com"})
      {:ok, tenant_b} = Provisioning.create_tenant(%{slug: "b-#{tenant_b_name}", email: "b@test.com"})

      # Insert data as tenant A
      Repo.checkout_with_tenant(tenant_a.id, fn ->
        Repo.insert!(%TenantFramework.Project{name: value, tenant_id: tenant_a.id})
      end)

      # Query as tenant B — should see no results
      results = Repo.checkout_with_tenant(tenant_b.id, fn ->
        Repo.all(TenantFramework.Project)
      end)

      assert results == [], "Tenant B saw #{length(results)} items from Tenant A"

      # Cleanup
      Repo.delete!(tenant_a)
      Repo.delete!(tenant_b)
    end
  end

  test "provisioning rolls back when Stripe fails" do
    # Mock Stripe to fail
    count_before = Repo.aggregate(TenantFramework.Tenant, :count)
    TenantFramework.StripeMock.force_error()
    result = Provisioning.create_tenant(%{slug: "fail-test-#{:rand.uniform(9999)}", email: "x@x.com"})
    assert {:error, {:stripe_failed, _}} = result
    count_after = Repo.aggregate(TenantFramework.Tenant, :count)
    assert count_after == count_before
    TenantFramework.StripeMock.reset()
  end
end

# test/rate_limiter_test.exs
defmodule TenantFramework.RateLimiterTest do
  use ExUnit.Case, async: false
  alias TenantFramework.Plug.RateLimiter

  setup do
    try do :ets.delete(:tenant_rate_limiter) rescue _ -> :ok end
    RateLimiter.init_table()
    :ok
  end

  test "tenant at limit is rate limited, tenant below is not" do
    tenant = %{id: "tenant-1", plan_rate_limit: 10}
    # Consume all 10 tokens
    for _ <- 1..10, do: refute(RateLimiter.rate_limited?(tenant))
    # 11th request should be limited
    assert RateLimiter.rate_limited?(tenant)
  end

  test "different tenants have independent buckets" do
    a = %{id: "tenant-a", plan_rate_limit: 5}
    b = %{id: "tenant-b", plan_rate_limit: 5}
    for _ <- 1..5, do: RateLimiter.rate_limited?(a)
    assert RateLimiter.rate_limited?(a)
    refute RateLimiter.rate_limited?(b)
  end
end

# test/flags_test.exs
defmodule TenantFramework.FlagsTest do
  use ExUnit.Case, async: false
  alias TenantFramework.Flags.{Cache, Evaluator}

  test "disabled flag returns false for all tenants" do
    flag = %{name: "new_feature", enabled: false, rollout_pct: 100}
    :ets.insert(:feature_flags, {"new_feature", flag})
    refute Evaluator.enabled?("new_feature", "any-tenant")
  end

  test "100% rollout flag returns true for all tenants" do
    flag = %{name: "full_rollout", enabled: true, rollout_pct: 100}
    :ets.insert(:feature_flags, {"full_rollout", flag})
    assert Evaluator.enabled?("full_rollout", "tenant-1")
    assert Evaluator.enabled?("full_rollout", "tenant-2")
  end

  test "50% rollout is consistent: same tenant always gets same result" do
    flag = %{name: "half_rollout", enabled: true, rollout_pct: 50}
    :ets.insert(:feature_flags, {"half_rollout", flag})
    tenant_id = "consistent-tenant-id"
    result1 = Evaluator.enabled?("half_rollout", tenant_id)
    result2 = Evaluator.enabled?("half_rollout", tenant_id)
    assert result1 == result2
  end
end

# test/billing_test.exs
defmodule TenantFramework.BillingTest do
  use ExUnit.Case, async: false

  test "webhook with invalid signature returns 400" do
    conn = build_conn(:post, "/webhooks/stripe", "payload")
    |> put_req_header("stripe-signature", "v1=invalid_sig,t=#{System.system_time(:second)}")
    conn = TenantFramework.Billing.WebhookHandler.handle(conn)
    assert conn.status == 400
  end

  test "duplicate webhook event is idempotent" do
    event_id = "evt_test_#{System.unique_integer()}"
    event = Jason.encode!(%{"id" => event_id, "type" => "invoice.paid", "data" => %{}})
    # First processing
    first_result = process_valid_webhook(event)
    assert first_result == :ok
    # Second processing (same event)
    second_result = process_valid_webhook(event)
    assert second_result == {:error, :already_processed}
  end

  defp process_valid_webhook(body) do
    # Build a valid signature for testing
    secret = Application.get_env(:tenant_framework, :stripe_webhook_secret, "test_secret")
    ts = System.system_time(:second)
    sig = :crypto.mac(:hmac, :sha256, secret, "#{ts}.#{body}") |> Base.encode16(case: :lower)
    sig_header = "t=#{ts},v1=#{sig}"
    # Direct call to process (bypassing HTTP layer)
    TenantFramework.Billing.WebhookHandler.process_verified(body, sig_header)
  end
end
```

## Trade-offs

| Isolation strategy | Schema-per-tenant | RLS (row-level security) | Trade-off |
|---|---|---|---|
| Isolation strength | Complete table isolation | Row-level; requires correct policy | RLS: a missing policy means a potential leak; schema: impossible to mix data |
| Migration complexity | Must iterate N schemas | Single schema, standard migrations | Schema: migration time = O(N tenants); RLS: O(1) |
| Connection pooling | Complex (pool per schema) | Simple (single pool) | Schema: can limit PgBouncer effectiveness; RLS: full connection pool sharing |
| Data locality | Per-tenant backup/export trivial | Requires WHERE tenant_id=X everywhere | Schema: operational flexibility; RLS: simpler code, harder ops |

| Rate limiting approach | ETS `update_counter` | GenServer per tenant | Redis cluster |
|---|---|---|---|
| Throughput | ~10M ops/s (concurrent) | ~500k msg/s (serialized) | ~1M ops/s (network bound) |
| Failure mode | ETS owned by supervisor; survives process crashes | If GenServer crashes, state lost | Redis down = no rate limiting |
| Cluster-wide accuracy | Node-local only | Node-local only | Accurate across cluster |

## Production mistakes

**Forgetting to SET LOCAL in checkout_with_tenant.** `SET app.current_tenant = X` sets the session variable globally for the connection. If that connection is returned to the pool without resetting it, the next request using that connection sees the wrong tenant. Use `SET LOCAL` (transaction-scoped) or always reset after use. With `Ecto.Repo.checkout/1`, the transaction boundary ensures `SET LOCAL` is automatically reset on `COMMIT` or `ROLLBACK`.

**Not verifying the Stripe webhook timestamp.** An attacker who captures a valid Stripe webhook payload can replay it indefinitely without timestamp validation. Always check that the `t=` value in the signature header is within 5 minutes of the current time. Store processed event IDs with a TTL larger than the tolerance window to handle duplicates.

**ETS rate limiter without cleanup.** The rate limiter table accumulates one key per `{tenant_id, window}`. At 1-second windows with 1000 tenants, that is 1000 keys per second. After 24 hours: 86.4M keys consuming gigabytes of memory. A background process must periodically delete keys from expired windows: `{tenant_id, w}` where `w < div(now_ms, 1000) - 2`.

**Feature flag evaluation that falls back to false silently.** When an unknown flag is evaluated (flag not in ETS), returning `false` by default is safe. But if the ETS table is empty due to a failed cache reload, all features silently disable. Log a warning when the flags table is empty, and distinguish between "flag explicitly disabled" and "flag unknown."

**Provisioning tenant creation not being idempotent on retry.** If the network call to Stripe times out, the caller retries `create_tenant`. If the first attempt created the database rows but the Stripe call timed out (not failed — timed out), retrying inserts a duplicate tenant row. Add a unique constraint on `slug` and handle the unique violation in provisioning to detect and recover partial creations.

## Resources

- PostgreSQL Row Level Security — https://www.postgresql.org/docs/current/ddl-rowsecurity.html
- Triplex library source — https://github.com/ateliware/triplex (schema-per-tenant reference)
- Stripe Webhooks documentation — https://stripe.com/docs/webhooks
- Stripe Webhook signature verification — https://stripe.com/docs/webhooks/signatures
- AWS Multi-tenant SaaS Architecture whitepaper — https://docs.aws.amazon.com/whitepapers/latest/saas-architecture-fundamentals/
- Fowler — "Patterns of Enterprise Application Architecture" (Multi-tenancy chapter)
- Oban documentation — https://hexdocs.pm/oban/ (job queue for meter flushing)
