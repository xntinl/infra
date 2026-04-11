# Advanced Ecto Queries

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` umbrella has accumulated months of production data: request logs, client
usage records, rate-limiting events, billing entries. The analytics team now needs queries
that go beyond `Repo.all(Client)`: ranked usage reports, dynamic filters from the admin
dashboard, atomic multi-table updates, and batch processing of millions of rows without
loading them all into memory.

All queries in this exercise live in `gateway_core`.

Project structure:

```
api_gateway_umbrella/apps/gateway_core/
├── lib/gateway_core/
│   ├── analytics.ex            # ← you implement this
│   ├── billing/
│   │   └── transfers.ex        # ← and this
│   └── client_filters.ex       # ← and this
└── test/gateway_core/
    ├── analytics_test.exs      # given tests
    ├── billing_transfers_test.exs
    └── client_filters_test.exs
```

---

## Why these patterns matter in production

- **Window functions with `fragment`**: analytics dashboards need ranked data per category.
  Doing the ranking in Elixir requires loading all rows first. Doing it in SQL means the DB
  returns only the ranked results — orders of magnitude less data over the wire.

- **`dynamic/2` for optional filters**: building SQL strings by concatenation is an
  injection risk and produces inconsistent query plans. `dynamic/2` composes boolean
  expressions at the Ecto layer — a single consistent WHERE clause, never string manipulation.

- **`Ecto.Multi`**: any operation that modifies more than one table must be atomic. Without
  `Multi`, a process crash between two `Repo.update` calls leaves the database inconsistent.

- **`Repo.stream`**: loading 5 million billing records with `Repo.all` allocates gigabytes
  of heap. `Repo.stream` uses PostgreSQL server-side cursors — it fetches rows in chunks
  and processes them before fetching the next batch.

---

## Implementation

### Part 1: Window functions with `fragment/1`

The analytics team needs, per client, their request count and rank within their plan tier.

```elixir
# lib/gateway_core/analytics.ex
defmodule GatewayCore.Analytics do
  import Ecto.Query
  alias GatewayCore.{Repo, RequestLog}

  @doc """
  Returns request counts per client with rank within their plan tier.

  Uses PostgreSQL `rank() OVER (PARTITION BY ...)` via fragment.
  Ecto does not expose window functions as macros — fragment is the correct tool.
  """
  def client_usage_ranking do
    from(r in RequestLog,
      join: c in assoc(r, :client),
      group_by: [r.client_id, c.name, c.plan],
      select: %{
        client_name:  c.name,
        plan:         c.plan,
        request_count: count(r.id),
        rank: fragment(
          "rank() OVER (PARTITION BY ? ORDER BY count(?) DESC)",
          c.plan,
          r.id
        )
      }
    )
    |> Repo.all()
  end

  @doc """
  Running total of requests per client, ordered by time.
  Demonstrates cumulative window function.
  """
  def client_request_running_totals(client_id) do
    from(r in RequestLog,
      where: r.client_id == ^client_id,
      select: %{
        ts:            r.inserted_at,
        request_count: 1,
        running_total: fragment(
          "sum(1) OVER (ORDER BY ? ROWS UNBOUNDED PRECEDING)",
          r.inserted_at
        )
      },
      order_by: r.inserted_at
    )
    |> Repo.all()
  end

  @doc """
  Clients whose total requests exceed the average across all clients.

  Uses subquery/1 — the average is computed in the DB, no Elixir round-trip.
  """
  def above_average_clients do
    avg_subquery =
      from(r in RequestLog,
        select: avg(r.duration_ms)
      )

    from(r in RequestLog,
      join: c in assoc(r, :client),
      group_by: [r.client_id, c.name],
      having: avg(r.duration_ms) > subquery(avg_subquery),
      select: %{client_name: c.name, avg_duration_ms: avg(r.duration_ms)}
    )
    |> Repo.all()
  end
end
```

### Part 2: Dynamic filters with `dynamic/2`

The admin dashboard has 6 optional filter fields. Never concatenate SQL strings.

```elixir
# lib/gateway_core/client_filters.ex
defmodule GatewayCore.ClientFilters do
  import Ecto.Query
  alias GatewayCore.{Repo, Client}

  @doc """
  Searches clients with optional filters from the admin dashboard.

  Accepted params (all optional, string-keyed):
    "plan"         — "free" | "pro" | "enterprise"
    "active"       — "true" | "false"
    "min_requests" — integer
    "max_requests" — integer
    "name_like"    — substring match (case-insensitive)
    "created_after" — ISO date string
  """
  @spec search(map()) :: [Client.t()]
  def search(params) when is_map(params) do
    Client
    |> where(^build_filters(params))
    |> join(:left, [c], r in assoc(c, :request_logs), as: :logs)
    |> group_by([c], c.id)
    |> select([c, logs: r], %{client: c, request_count: count(r.id)})
    |> order_by([c], asc: c.name)
    |> Repo.all()
  end

  defp build_filters(params) do
    Enum.reduce(params, dynamic(true), fn
      {"plan", plan}, acc ->
        dynamic([c], ^acc and c.plan == ^plan)

      {"active", "true"}, acc ->
        dynamic([c], ^acc and c.active == true)

      {"active", "false"}, acc ->
        dynamic([c], ^acc and c.active == false)

      {"min_requests", n}, acc ->
        # TODO: requires a subquery or join — this is a hint, not a complete solution
        # HINT: dynamic/2 can reference joins by name: dynamic([logs: r], count(r.id) >= ^n)
        acc

      {"name_like", q}, acc ->
        dynamic([c], ^acc and ilike(c.name, ^"%#{q}%"))

      {"created_after", date_str}, acc ->
        case Date.from_iso8601(date_str) do
          {:ok, date} -> dynamic([c], ^acc and c.inserted_at >= ^date)
          _           -> acc
        end

      _unknown, acc ->
        acc
    end)
  end
end
```

### Part 3: `Ecto.Multi` for atomic operations

The billing system debits a client's quota and inserts an audit record. Both must succeed
or both must fail.

```elixir
# lib/gateway_core/billing/transfers.ex
defmodule GatewayCore.Billing.Transfers do
  import Ecto.Query
  alias GatewayCore.{Repo, Client, BillingAudit}
  alias Ecto.Multi

  @doc """
  Deducts `amount` requests from `client_id`'s quota and records the audit entry.

  Returns {:ok, %{client: updated_client, audit: audit_entry}}
  or {:error, failed_step, reason, changes_so_far}.
  """
  @spec deduct_quota(integer(), pos_integer(), String.t()) ::
          {:ok, map()} | {:error, atom(), term(), map()}
  def deduct_quota(client_id, amount, reason) do
    Multi.new()
    |> Multi.run(:client, fn repo, _ ->
      case repo.get(Client, client_id) do
        nil    -> {:error, :client_not_found}
        client -> {:ok, client}
      end
    end)
    |> Multi.run(:check_quota, fn _repo, %{client: client} ->
      if client.quota_remaining >= amount do
        {:ok, client}
      else
        {:error, :insufficient_quota}
      end
    end)
    |> Multi.run(:debit, fn repo, %{client: client} ->
      client
      |> Client.changeset(%{quota_remaining: client.quota_remaining - amount})
      |> repo.update()
    end)
    |> Multi.insert(:audit, fn %{client: client} ->
      %BillingAudit{
        client_id: client.id,
        amount:    amount,
        reason:    reason,
        occurred_at: DateTime.utc_now()
      }
    end)
    |> Repo.transaction()
  end
end
```

Usage pattern:

```elixir
case GatewayCore.Billing.Transfers.deduct_quota(client_id, 1_000, "api_batch_request") do
  {:ok, %{client: client, audit: _}} ->
    Logger.info("Quota deducted: client=#{client.id} remaining=#{client.quota_remaining}")

  {:error, :check_quota, :insufficient_quota, _} ->
    {:error, :quota_exceeded}

  {:error, :client, :client_not_found, _} ->
    {:error, :not_found}

  {:error, failed_step, reason, _changes} ->
    Logger.error("Transfer failed at #{failed_step}: #{inspect(reason)}")
    {:error, :internal}
end
```

### Part 4: `Repo.stream` for large datasets

The compliance team runs monthly recalculations over all request logs. Loading everything
into memory is not viable.

```elixir
# lib/gateway_core/analytics.ex (continued)
defmodule GatewayCore.Analytics do
  # ... (previous functions)

  @doc """
  Recalculates billing for all unprocessed request logs.

  Uses Repo.stream to process rows in chunks without loading all into memory.
  Must run inside a transaction — PostgreSQL server-side cursors require it.
  """
  def recalculate_billing do
    query = from(r in RequestLog,
      where: r.billing_processed == false,
      select: r
    )

    Repo.transaction(fn ->
      query
      |> Repo.stream(max_rows: 500)       # PostgreSQL cursor: 500 rows per fetch
      |> Stream.map(&compute_billing_entry/1)
      |> Stream.chunk_every(200)           # batch inserts of 200 rows
      |> Enum.each(fn batch ->
        Repo.insert_all(BillingEntry, batch, on_conflict: :nothing)
      end)
    end, timeout: :infinity)
  end

  defp compute_billing_entry(%RequestLog{} = log) do
    %{
      client_id:    log.client_id,
      request_id:   log.id,
      cost:         calculate_cost(log.duration_ms, log.bytes_transferred),
      computed_at:  DateTime.utc_now()
    }
  end

  defp calculate_cost(duration_ms, bytes) do
    # Billing formula: base rate + duration factor + bandwidth factor
    base = Decimal.new("0.0001")
    dur  = Decimal.mult(base, Decimal.new(div(duration_ms, 100)))
    bw   = Decimal.mult(Decimal.new("0.00001"), Decimal.new(div(bytes, 1024)))
    Decimal.add(dur, bw)
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/gateway_core/client_filters_test.exs
defmodule GatewayCore.ClientFiltersTest do
  use GatewayCore.DataCase

  alias GatewayCore.ClientFilters

  test "empty params returns all clients" do
    insert_list(3, :client)
    results = ClientFilters.search(%{})
    assert length(results) == 3
  end

  test "filters by plan" do
    insert(:client, plan: :free)
    insert(:client, plan: :pro)
    insert(:client, plan: :pro)

    results = ClientFilters.search(%{"plan" => "pro"})
    assert length(results) == 2
    assert Enum.all?(results, fn %{client: c} -> c.plan == :pro end)
  end

  test "filters by active status" do
    insert(:client, active: true)
    insert(:client, active: false)

    results = ClientFilters.search(%{"active" => "false"})
    assert length(results) == 1
    assert hd(results).client.active == false
  end

  test "filters by name_like (case-insensitive)" do
    insert(:client, name: "Acme Corp")
    insert(:client, name: "Beta Ltd")

    results = ClientFilters.search(%{"name_like" => "acme"})
    assert length(results) == 1
    assert hd(results).client.name == "Acme Corp"
  end

  test "multiple filters compose with AND" do
    insert(:client, plan: :pro, active: true,  name: "Active Pro")
    insert(:client, plan: :pro, active: false, name: "Inactive Pro")
    insert(:client, plan: :free, active: true, name: "Active Free")

    results = ClientFilters.search(%{"plan" => "pro", "active" => "true"})
    assert length(results) == 1
    assert hd(results).client.name == "Active Pro"
  end
end
```

```elixir
# test/gateway_core/billing_transfers_test.exs
defmodule GatewayCore.Billing.TransfersTest do
  use GatewayCore.DataCase

  alias GatewayCore.Billing.Transfers
  alias GatewayCore.Client

  test "deducts quota and creates audit entry" do
    client = insert(:client, quota_remaining: 10_000)

    assert {:ok, %{client: updated, audit: audit}} =
      Transfers.deduct_quota(client.id, 1_000, "api_batch")

    assert updated.quota_remaining == 9_000
    assert audit.amount == 1_000
    assert audit.reason == "api_batch"
  end

  test "fails with :insufficient_quota when quota is too low" do
    client = insert(:client, quota_remaining: 500)

    assert {:error, :check_quota, :insufficient_quota, _} =
      Transfers.deduct_quota(client.id, 1_000, "too_much")

    # Verify quota was NOT changed (transaction rolled back)
    assert GatewayCore.Repo.get!(Client, client.id).quota_remaining == 500
  end

  test "fails with :client_not_found for unknown client" do
    assert {:error, :client, :client_not_found, _} =
      Transfers.deduct_quota(-1, 100, "test")
  end
end
```

### Step 6: Run the tests

```bash
mix test test/gateway_core/analytics_test.exs \
         test/gateway_core/client_filters_test.exs \
         test/gateway_core/billing_transfers_test.exs \
         --trace
```

Debug any query with:

```elixir
# In iex -S mix:
{sql, params} = GatewayCore.Repo.to_sql(:all, GatewayCore.ClientFilters.search(%{"plan" => "pro"}))
IO.puts(sql)
```

---

## Trade-off analysis

| Technique | When to use | When NOT to use |
|-----------|-------------|-----------------|
| `fragment/1` for window functions | SQL-level aggregation that Ecto macros don't support | Simple counts, sums — use Ecto macros |
| `dynamic/2` | Optional filters from user input | Fixed WHERE clauses — use `where/3` directly |
| `Ecto.Multi` | Any change touching > 1 table | Single-table operations |
| `Repo.stream` | > 100k rows; batch processing | Small result sets — overhead of cursor outweighs benefit |
| `subquery/1` | Value depends on same DB | External computation — fetch separately |

Reflection: `Repo.stream` requires `timeout: :infinity` on the wrapping transaction. In a
web request context, this is dangerous — a slow stream blocks the DB connection indefinitely.
Where should `recalculate_billing/0` be called? (Hint: Oban worker with `timeout/1` set.)

---

## Common production mistakes

**1. Using `Repo.all` without LIMIT on production tables**
A table with 10 million rows loaded via `Repo.all` allocates the entire dataset in the
process heap. Always add `limit/2` for user-facing queries; use `Repo.stream` for batch jobs.

**2. `dynamic/2` with user-controlled field names**
`dynamic([c], ^acc and field(c, ^String.to_atom(user_input)) == ^value)` is safe for values
but dangerous for field names if not validated against a whitelist. Always validate field
names explicitly before converting to atoms.

**3. `Ecto.Multi` step returning `{:error, reason}` vs raising**
If a Multi step raises, Ecto rolls back the transaction and re-raises. If it returns
`{:error, reason}`, Ecto rolls back and returns `{:error, step_name, reason, changes}`.
Decide which pattern you want and be consistent — mixing both makes error handling confusing.

**4. `Repo.stream` outside a transaction**
PostgreSQL server-side cursors require an active transaction. Calling `Repo.stream` without
wrapping in `Repo.transaction` raises `DBConnection.ConnectionError` at runtime.

**5. Window functions in `having/2`**
Window functions are not allowed in `HAVING` clauses (only aggregate functions are). Wrap
the window function result in a subquery if you need to filter on it.

---

## Resources

- [`fragment/1`](https://hexdocs.pm/ecto/Ecto.Query.API.html#fragment/1) — inject raw SQL safely
- [`dynamic/2`](https://hexdocs.pm/ecto/Ecto.Query.html#dynamic/2) — composable boolean expressions
- [`Ecto.Multi`](https://hexdocs.pm/ecto/Ecto.Multi.html) — named, composable transactions
- [`Repo.stream/2`](https://hexdocs.pm/ecto/Ecto.Repo.html#c:stream/2) — cursor-based streaming
- [PostgreSQL window functions](https://www.postgresql.org/docs/current/tutorial-window.html) — complete reference
