# Advanced Ecto Queries

## Overview

Build advanced query patterns for an API gateway's analytics and billing system: window
functions via `fragment/1`, dynamic filters with `dynamic/2`, atomic multi-table operations
with `Ecto.Multi`, and streaming millions of rows with `Repo.stream`. All code lives in
the gateway's core domain layer.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── repo.ex
│       ├── schemas/
│       │   ├── client.ex
│       │   ├── request_log.ex
│       │   └── billing_audit.ex
│       ├── analytics.ex
│       ├── billing/
│       │   └── transfers.ex
│       └── client_filters.ex
└── test/
    └── api_gateway/
        ├── analytics_test.exs
        ├── billing_transfers_test.exs
        └── client_filters_test.exs
```

---

## Why these patterns matter in production

- **Window functions with `fragment`**: analytics dashboards need ranked data per category.
  Doing the ranking in Elixir requires loading all rows first. Doing it in SQL means the DB
  returns only the ranked results.

- **`dynamic/2` for optional filters**: building SQL strings by concatenation is an injection
  risk. `dynamic/2` composes boolean expressions at the Ecto layer safely.

- **`Ecto.Multi`**: any operation that modifies more than one table must be atomic. Without
  `Multi`, a crash between two `Repo.update` calls leaves the database inconsistent.

- **`Repo.stream`**: loading 5 million billing records with `Repo.all` allocates gigabytes
  of heap. `Repo.stream` uses PostgreSQL server-side cursors -- fetching rows in chunks.

---

## Implementation

### Part 1: Window functions with `fragment/1`

```elixir
# lib/api_gateway/analytics.ex
defmodule ApiGateway.Analytics do
  import Ecto.Query
  alias ApiGateway.{Repo, RequestLog, BillingEntry}

  @doc """
  Returns request counts per client with rank within their plan tier.

  Uses PostgreSQL `rank() OVER (PARTITION BY ...)` via fragment.
  Ecto does not expose window functions as macros -- fragment is the correct tool.
  """
  @spec client_usage_ranking() :: [map()]
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
  @spec client_request_running_totals(integer()) :: [map()]
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
  Clients whose average request duration exceeds the global average.

  Uses subquery/1 -- the average is computed in the DB, no Elixir round-trip.
  """
  @spec above_average_clients() :: [map()]
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

  @doc """
  Recalculates billing for all unprocessed request logs.

  Uses Repo.stream to process rows in chunks without loading all into memory.
  Must run inside a transaction -- PostgreSQL server-side cursors require it.
  """
  @spec recalculate_billing() :: {:ok, any()} | {:error, any()}
  def recalculate_billing do
    query = from(r in RequestLog,
      where: r.billing_processed == false,
      select: r
    )

    Repo.transaction(fn ->
      query
      |> Repo.stream(max_rows: 500)
      |> Stream.map(&compute_billing_entry/1)
      |> Stream.chunk_every(200)
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
    base = Decimal.new("0.0001")
    dur  = Decimal.mult(base, Decimal.new(div(duration_ms, 100)))
    bw   = Decimal.mult(Decimal.new("0.00001"), Decimal.new(div(bytes, 1024)))
    Decimal.add(dur, bw)
  end
end
```

### Part 2: Dynamic filters with `dynamic/2`

The admin dashboard has 6 optional filter fields. Never concatenate SQL strings.

```elixir
# lib/api_gateway/client_filters.ex
defmodule ApiGateway.ClientFilters do
  import Ecto.Query
  alias ApiGateway.{Repo, Client}

  @doc """
  Searches clients with optional filters from the admin dashboard.

  Accepted params (all optional, string-keyed):
    "plan"         -- "free" | "pro" | "enterprise"
    "active"       -- "true" | "false"
    "min_requests" -- integer
    "max_requests" -- integer
    "name_like"    -- substring match (case-insensitive)
    "created_after" -- ISO date string
  """
  @spec search(map()) :: [map()]
  def search(params) when is_map(params) do
    Client
    |> where(^build_filters(params))
    |> join(:left, [c], r in assoc(c, :request_logs), as: :logs)
    |> group_by([c], c.id)
    |> maybe_having(params)
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

  defp maybe_having(query, %{"min_requests" => n}) when is_integer(n) do
    having(query, [c, logs: r], count(r.id) >= ^n)
  end

  defp maybe_having(query, %{"max_requests" => n}) when is_integer(n) do
    having(query, [c, logs: r], count(r.id) <= ^n)
  end

  defp maybe_having(query, _params), do: query
end
```

### Part 3: `Ecto.Multi` for atomic operations

The billing system debits a client's quota and inserts an audit record. Both must succeed
or both must fail.

```elixir
# lib/api_gateway/billing/transfers.ex
defmodule ApiGateway.Billing.Transfers do
  import Ecto.Query
  alias ApiGateway.{Repo, Client, BillingAudit}
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
case ApiGateway.Billing.Transfers.deduct_quota(client_id, 1_000, "api_batch_request") do
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

### Step 4: Tests

```elixir
# test/api_gateway/client_filters_test.exs
defmodule ApiGateway.ClientFiltersTest do
  use ApiGateway.DataCase

  alias ApiGateway.ClientFilters

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
# test/api_gateway/billing_transfers_test.exs
defmodule ApiGateway.Billing.TransfersTest do
  use ApiGateway.DataCase

  alias ApiGateway.Billing.Transfers
  alias ApiGateway.Client

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

    assert ApiGateway.Repo.get!(Client, client.id).quota_remaining == 500
  end

  test "fails with :client_not_found for unknown client" do
    assert {:error, :client, :client_not_found, _} =
      Transfers.deduct_quota(-1, 100, "test")
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/client_filters_test.exs \
         test/api_gateway/billing_transfers_test.exs \
         --trace
```

Debug any query with:

```elixir
# In iex -S mix:
{sql, params} = ApiGateway.Repo.to_sql(:all, ApiGateway.ClientFilters.search(%{"plan" => "pro"}))
IO.puts(sql)
```

---

## Trade-off analysis

| Technique | When to use | When NOT to use |
|-----------|-------------|-----------------|
| `fragment/1` for window functions | SQL-level aggregation that Ecto macros don't support | Simple counts, sums -- use Ecto macros |
| `dynamic/2` | Optional filters from user input | Fixed WHERE clauses -- use `where/3` directly |
| `Ecto.Multi` | Any change touching > 1 table | Single-table operations |
| `Repo.stream` | > 100k rows; batch processing | Small result sets -- cursor overhead outweighs benefit |
| `subquery/1` | Value depends on same DB | External computation -- fetch separately |

---

## Common production mistakes

**1. Using `Repo.all` without LIMIT on production tables**
A table with 10 million rows loaded via `Repo.all` allocates the entire dataset in the
process heap. Always add `limit/2` for user-facing queries; use `Repo.stream` for batch jobs.

**2. `dynamic/2` with user-controlled field names**
`dynamic([c], field(c, ^String.to_atom(user_input)) == ^value)` is dangerous for field names
if not validated against a whitelist. Always validate field names before converting to atoms.

**3. `Ecto.Multi` step returning `{:error, reason}` vs raising**
If a Multi step raises, Ecto rolls back and re-raises. If it returns `{:error, reason}`,
Ecto rolls back and returns `{:error, step_name, reason, changes}`. Be consistent.

**4. `Repo.stream` outside a transaction**
PostgreSQL server-side cursors require an active transaction. Calling `Repo.stream` without
wrapping in `Repo.transaction` raises `DBConnection.ConnectionError`.

**5. Window functions in `having/2`**
Window functions are not allowed in `HAVING` clauses. Wrap the window function result in a
subquery if you need to filter on it.

---

## Resources

- [`fragment/1`](https://hexdocs.pm/ecto/Ecto.Query.API.html#fragment/1) -- inject raw SQL safely
- [`dynamic/2`](https://hexdocs.pm/ecto/Ecto.Query.html#dynamic/2) -- composable boolean expressions
- [`Ecto.Multi`](https://hexdocs.pm/ecto/Ecto.Multi.html) -- named, composable transactions
- [`Repo.stream/2`](https://hexdocs.pm/ecto/Ecto.Repo.html#c:stream/2) -- cursor-based streaming
- [PostgreSQL window functions](https://www.postgresql.org/docs/current/tutorial-window.html) -- complete reference
