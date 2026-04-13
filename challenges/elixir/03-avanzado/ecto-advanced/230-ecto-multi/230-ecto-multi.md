# Ecto.Multi: Composable Transactional Workflows

**Project**: `ecto_multi_deep` — order placement pipeline for an e-commerce backend.

---

## Project context

Placing an order atomically requires five database writes: decrement product stock, insert
the order, insert N line items, insert a payment row, record an audit log. If any step
fails (stock would go negative, payment gateway rejects the card, audit table is down),
every prior step must roll back. You also want to return meaningful errors — "stock for
SKU-42 is insufficient" — without swallowing the error inside a generic
`Repo.transaction/1`.

`Ecto.Multi` is the idiomatic answer in Ecto. It's a data structure that accumulates named
steps. You pass the `Multi` to `Repo.transaction/1`; it runs each step in order in a single
DB transaction, and if any step returns `{:error, reason}` it rolls back and returns
`{:error, failed_step_name, reason, changes_so_far}`. Because the `Multi` is data, you can
compose functions that return `Multi`s, branch conditionally, and test them without a
database connection.

This exercise builds a full order-placement pipeline using `Multi.insert`, `Multi.update`,
`Multi.run`, `Multi.merge`, and `Multi.append`. It demonstrates the split between
"declarative steps" and "dynamic steps that depend on prior results".

---

```
ecto_multi_deep/
├── lib/
│   └── ecto_multi_deep/
│       ├── application.ex
│       ├── repo.ex
│       ├── schemas/
│       │   ├── product.ex
│       │   ├── order.ex
│       │   ├── line_item.ex
│       │   ├── payment.ex
│       │   └── audit_log.ex
│       └── orders/
│           ├── place_order.ex        # the Multi pipeline
│           └── payment_gateway.ex    # simulated external service
├── priv/repo/migrations/20260101000000_create_tables.exs
├── test/ecto_multi_deep/place_order_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. `Multi` is data

`Ecto.Multi.new() |> Multi.insert(:order, changeset)` returns a `%Multi{}` struct that
has NOT touched the database. Nothing runs until `Repo.transaction/1` receives it. This
data-orientation enables composition, inspection, and pure unit tests.

### 2. Step names are unique keys

Every step has a name (atom). The name must be unique in the entire `Multi`. On success,
`Repo.transaction/1` returns `{:ok, %{order: order_struct, payment: pmt, ...}}` — a map
keyed by step name. On failure:
`{:error, :stock_decrement, reason, %{order: already_created_order}}` — you see where it
failed and what had been created so far (logically rolled back, but accessible for
error reporting).

### 3. Declarative vs dynamic steps

| Function | Purpose |
|----------|---------|
| `Multi.insert(multi, name, changeset)` | insert a static changeset |
| `Multi.update(multi, name, changeset)` | update a static changeset |
| `Multi.delete(multi, name, struct_or_changeset)` | delete |
| `Multi.run(multi, name, fun)` | arbitrary function receiving `repo, changes_so_far` |
| `Multi.merge(multi, fun)` | produce a new `Multi` based on prior results |
| `Multi.append(multi, other_multi)` | concatenate two pre-built Multis |

`Multi.run` is the escape hatch for "do something that isn't a changeset"
(HTTP call, side effect). Its return value is `{:ok, value}` or `{:error, reason}`.

### 4. Composition across modules

Each context function that writes to the DB can return a `Multi`. The caller then appends
or merges them. This makes transactional boundaries explicit in the type signature:

```
Orders.place_order_multi(params) :: Ecto.Multi.t()
Payments.charge_multi(order, card) :: Ecto.Multi.t()
Audit.log_multi(actor, action) :: Ecto.Multi.t()
```

A webhook endpoint can compose all three without knowing their internals.

### 5. When to avoid side effects in `Multi.run`

External API calls inside a transaction are dangerous: the transaction is holding DB locks
while waiting for the network. If the payment gateway is slow, all other transactions
using the same rows wait. Preferred pattern: issue the gateway call OUTSIDE the Multi and
pass the result in as data — or use a two-phase commit via `Oban` jobs.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Schemas (concise)

**Objective**: Define Ecto schemas (Order, LineItem, Payment, AuditLog) with proper associations so Multi chains across aggregates in one DB transaction.

```elixir
defmodule EctoMultiDeep.Schemas.Product do
  use Ecto.Schema

  schema "products" do
    field :sku, :string
    field :price_cents, :integer
    field :stock, :integer
    timestamps()
  end
end

defmodule EctoMultiDeep.Schemas.Order do
  use Ecto.Schema
  import Ecto.Changeset

  schema "orders" do
    field :customer_email, :string
    field :total_cents, :integer
    field :status, :string, default: "pending"
    has_many :line_items, EctoMultiDeep.Schemas.LineItem
    timestamps()
  end

  def changeset(order, attrs) do
    order
    |> cast(attrs, [:customer_email, :total_cents, :status])
    |> validate_required([:customer_email, :total_cents])
  end
end

defmodule EctoMultiDeep.Schemas.LineItem do
  use Ecto.Schema
  import Ecto.Changeset

  schema "line_items" do
    field :sku, :string
    field :quantity, :integer
    field :unit_price_cents, :integer
    belongs_to :order, EctoMultiDeep.Schemas.Order
  end

  def changeset(item, attrs) do
    item
    |> cast(attrs, [:sku, :quantity, :unit_price_cents, :order_id])
    |> validate_required([:sku, :quantity, :unit_price_cents])
    |> validate_number(:quantity, greater_than: 0)
  end
end

defmodule EctoMultiDeep.Schemas.Payment do
  use Ecto.Schema
  import Ecto.Changeset

  schema "payments" do
    field :amount_cents, :integer
    field :gateway_reference, :string
    field :status, :string
    belongs_to :order, EctoMultiDeep.Schemas.Order
    timestamps()
  end

  def changeset(payment, attrs) do
    payment
    |> cast(attrs, [:amount_cents, :gateway_reference, :status, :order_id])
    |> validate_required([:amount_cents, :gateway_reference, :status, :order_id])
  end
end

defmodule EctoMultiDeep.Schemas.AuditLog do
  use Ecto.Schema
  import Ecto.Changeset

  schema "audit_logs" do
    field :action, :string
    field :payload, :map
    timestamps(updated_at: false)
  end

  def changeset(log, attrs) do
    log
    |> cast(attrs, [:action, :payload])
    |> validate_required([:action, :payload])
  end
end
```

### Step 2: Simulated payment gateway

**Objective**: Implement PaymentGateway stub so tests drive :declined/:network errors deterministically without external HTTP.

```elixir
defmodule EctoMultiDeep.Orders.PaymentGateway do
  @moduledoc "Simulated gateway. In production replace with Stripe/MP HTTP client."

  @spec charge(pos_integer(), String.t()) ::
          {:ok, %{reference: String.t()}} | {:error, :declined | :network}
  def charge(amount_cents, card_token) when is_integer(amount_cents) and amount_cents > 0 do
    cond do
      card_token == "declined" -> {:error, :declined}
      card_token == "network_error" -> {:error, :network}
      true -> {:ok, %{reference: "ch_" <> Base.encode16(:crypto.strong_rand_bytes(6))}}
    end
  end
end
```

### Step 3: The `Multi` pipeline

**Objective**: Build Multi pipeline: stock validation, order insert, line items merge, payment, audit — one step fails, all roll back atomically.

```elixir
defmodule EctoMultiDeep.Orders.PlaceOrder do
  @moduledoc """
  Places an order atomically. Uses Ecto.Multi to chain stock decrement, order creation,
  line items, payment, and audit log into a single transaction.
  """

  alias Ecto.Multi
  alias EctoMultiDeep.Repo
  alias EctoMultiDeep.Orders.PaymentGateway
  alias EctoMultiDeep.Schemas.{AuditLog, LineItem, Order, Payment, Product}

  @type line_params :: %{sku: String.t(), quantity: pos_integer()}
  @type params :: %{
          customer_email: String.t(),
          card_token: String.t(),
          lines: [line_params()]
        }

  @spec run(params()) ::
          {:ok, map()}
          | {:error, atom(), term(), map()}
          | {:error, :payment_failed, atom()}
  def run(params) do
    # Charge the gateway OUTSIDE the transaction to avoid holding DB locks during network I/O.
    total = compute_total(params.lines)

    case PaymentGateway.charge(total, params.card_token) do
      {:error, reason} ->
        {:error, :payment_failed, reason}

      {:ok, %{reference: reference}} ->
        params
        |> build_multi(total, reference)
        |> Repo.transaction()
    end
  end

  defp build_multi(params, total, reference) do
    Multi.new()
    |> Multi.run(:validate_stock, fn repo, _ -> validate_stock(repo, params.lines) end)
    |> Multi.run(:decrement_stock, fn repo, %{validate_stock: products} ->
      decrement_stock(repo, products, params.lines)
    end)
    |> Multi.insert(
      :order,
      Order.changeset(%Order{}, %{
        customer_email: params.customer_email,
        total_cents: total,
        status: "confirmed"
      })
    )
    |> Multi.merge(fn %{order: order} -> insert_line_items_multi(order, params.lines) end)
    |> Multi.insert(:payment, fn %{order: order} ->
      Payment.changeset(%Payment{}, %{
        amount_cents: total,
        gateway_reference: reference,
        status: "captured",
        order_id: order.id
      })
    end)
    |> Multi.insert(:audit, fn %{order: order} ->
      AuditLog.changeset(%AuditLog{}, %{
        action: "order.placed",
        payload: %{order_id: order.id, total_cents: total}
      })
    end)
  end

  defp validate_stock(repo, lines) do
    skus = Enum.map(lines, & &1.sku)

    products =
      repo.all(
        from p in Product,
          where: p.sku in ^skus,
          lock: "FOR UPDATE"
      )

    missing = skus -- Enum.map(products, & &1.sku)
    if missing != [], do: {:error, {:missing_skus, missing}}, else: {:ok, products}
  end

  defp decrement_stock(repo, products, lines) do
    by_sku = Map.new(products, &{&1.sku, &1})

    Enum.reduce_while(lines, {:ok, %{}}, fn line, {:ok, acc} ->
      case Map.get(by_sku, line.sku) do
        nil ->
          {:halt, {:error, {:unknown_sku, line.sku}}}

        %Product{stock: stock} when stock < line.quantity ->
          {:halt, {:error, {:insufficient_stock, line.sku}}}

        product ->
          {:ok, updated} =
            product
            |> Ecto.Changeset.change(stock: product.stock - line.quantity)
            |> repo.update()

          {:cont, {:ok, Map.put(acc, product.sku, updated)}}
      end
    end)
  end

  defp insert_line_items_multi(order, lines) do
    Enum.reduce(lines, Multi.new(), fn line, multi ->
      Multi.insert(
        multi,
        {:line_item, line.sku},
        LineItem.changeset(%LineItem{}, %{
          sku: line.sku,
          quantity: line.quantity,
          unit_price_cents: line.unit_price_cents,
          order_id: order.id
        })
      )
    end)
  end

  defp compute_total(lines),
    do: Enum.reduce(lines, 0, fn l, acc -> acc + l.quantity * l.unit_price_cents end)

  import Ecto.Query, only: [from: 2]
end
```

### Step 4: Migrations and tests

**Objective**: Assert Multi atomicity: inject midstream failures, verify zero order/payment/audit rows persist on rollback.

```elixir
defmodule EctoMultiDeep.Repo.Migrations.CreateTables do
  use Ecto.Migration

  def change do
    create table(:products) do
      add :sku, :string, null: false
      add :price_cents, :integer, null: false
      add :stock, :integer, null: false, default: 0
      timestamps()
    end
    create unique_index(:products, [:sku])

    create table(:orders) do
      add :customer_email, :string, null: false
      add :total_cents, :integer, null: false
      add :status, :string, null: false
      timestamps()
    end

    create table(:line_items) do
      add :sku, :string, null: false
      add :quantity, :integer, null: false
      add :unit_price_cents, :integer, null: false
      add :order_id, references(:orders, on_delete: :delete_all), null: false
    end

    create table(:payments) do
      add :amount_cents, :integer, null: false
      add :gateway_reference, :string, null: false
      add :status, :string, null: false
      add :order_id, references(:orders, on_delete: :restrict), null: false
      timestamps()
    end

    create table(:audit_logs) do
      add :action, :string, null: false
      add :payload, :map, null: false
      timestamps(updated_at: false)
    end
  end
end
```

```elixir
defmodule EctoMultiDeep.Orders.PlaceOrderTest do
  use ExUnit.Case, async: false

  alias EctoMultiDeep.Repo
  alias EctoMultiDeep.Orders.PlaceOrder
  alias EctoMultiDeep.Schemas.{Order, Product}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    p = Repo.insert!(%Product{sku: "A1", price_cents: 1000, stock: 5})
    {:ok, product: p}
  end

  describe "run/1" do
    test "commits everything on success", %{product: product} do
      params = %{
        customer_email: "a@x",
        card_token: "valid",
        lines: [%{sku: product.sku, quantity: 2, unit_price_cents: 1000}]
      }

      assert {:ok, %{order: %Order{status: "confirmed"}}} = PlaceOrder.run(params)
      assert %{stock: 3} = Repo.reload!(product)
    end

    test "rolls back entirely on insufficient stock", %{product: product} do
      params = %{
        customer_email: "a@x",
        card_token: "valid",
        lines: [%{sku: product.sku, quantity: 999, unit_price_cents: 1000}]
      }

      assert {:error, :decrement_stock, {:insufficient_stock, "A1"}, _changes} =
               PlaceOrder.run(params)

      assert %{stock: 5} = Repo.reload!(product)
      assert Repo.aggregate(Order, :count) == 0
    end

    test "does not open a transaction when payment fails" do
      params = %{
        customer_email: "a@x",
        card_token: "declined",
        lines: [%{sku: "A1", quantity: 1, unit_price_cents: 1000}]
      }

      assert {:error, :payment_failed, :declined} = PlaceOrder.run(params)
      assert Repo.aggregate(Order, :count) == 0
    end
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive

Ecto queries compile to SQL, but the translation is not always obvious. Complex preload patterns spawn subqueries for each association level—a naive nested preload can explode into hundreds of queries. Window functions and CTEs (Common Table Expressions) exist in Ecto but require raw fragments, making the boundary between Elixir and SQL explicit. For high-throughput systems, consider schemaless queries and streaming to defer memory allocation; loading 1M records as `Ecto.Repo.all/2` marshals everything into memory. Multi-tenancy via row-level database policies is cleaner than application-level filtering and leverages PostgreSQL's built-in enforcement. Zero-downtime migrations require careful orchestration: add columns before code that uses them, remove columns after code stops referencing them. Lock contention on hot rows kills throughput—use FOR UPDATE in transactions and understand when Ecto's optimistic locking is sufficient.
## Advanced Considerations

Advanced Ecto usage at scale requires understanding transaction semantics, locking strategies, and query performance under concurrent load. Ecto transactions are database transactions, not application-level transactions; they don't isolate against application-level concurrency issues. Using `:serializable` isolation level prevents anomalies but significantly impacts throughput. The choice between row-level locking with `for_update()` and optimistic locking with version columns affects both concurrency and latency. Deadlocks are not failures in Ecto; they're expected outcomes that require retry logic and careful key ordering to minimize.

Preload optimization is subtle — using `preload` for related data prevents N+1 queries but can create large intermediate result sets that exceed memory limits. Pagination with preloads requires careful consideration of whether to paginate before or after preloading related data. Custom types and schemaless queries provide flexibility but bypass Ecto's validation layer, creating opportunities for subtle bugs where invalid data sneaks into your database. The interaction between Ecto's change tracking and ETS caching can create stale data issues if not carefully managed across process boundaries.

Zero-downtime migrations require a different mental model than traditional migration scripts. Adding a column is fast; backfilling millions of rows is slow and can lock tables. Deploying code that expects the new column before the migration completes causes failures. Implement feature flags and dual-write patterns for truly zero-downtime deployments. Full-text search with PostgreSQL's tsearch requires careful index maintenance and stop-word configuration; performance characteristics change dramatically with language-specific settings and custom dictionaries.


## Deep Dive: Ecto Patterns and Production Implications

Ecto queries are composable, built up incrementally with pipes. Testing queries requires understanding that a query is lazy—until you call Repo.all, Repo.one, or Repo.update_all, no SQL is executed. This allows for property-based testing of query builders without hitting the database. Production bugs in complex queries often stem from incorrect scoping or ambiguous joins.

---

## Trade-offs and production gotchas

**1. Don't put slow I/O inside the transaction**
A `Multi.run(fn _, _ -> HTTPClient.post(...) end)` holds a DB connection and row locks for
the duration of the HTTP call. 50 concurrent orders can exhaust your pool. Call external
services before the transaction and pass results in as data.

**2. Long `Multi` chains become hard to reason about**
Once you have 10+ steps, the map of `changes_so_far` becomes a soup. Extract sub-pipelines
(`insert_line_items_multi/2`) and compose via `Multi.merge/2` or `Multi.append/2`.

**3. `Multi.run` name collisions**
Using the same atom twice silently replaces the first step — no compile-time check. Always
namespace dynamic names: `{:line_item, sku}` instead of `:line_item`.

**4. Row locks are acquired in step order**
If step 1 locks products sorted ascending and another transaction locks them descending,
you have a deadlock. Always sort rows by PK before taking `FOR UPDATE` locks.

**5. `Repo.transaction/1` rolls back on any throw/raise too**
An exception in `Multi.run` rolls back, but the error is wrapped in `Ecto.Adapters.SQL.abort_tx`.
Tests that `assert_raise` fail because the exception becomes an `:error` tuple. Wrap the
Multi in a top-level `try/catch` only if you truly need to observe the exception.

**6. Savepoints nest weirdly**
Nesting `Repo.transaction` inside a `Multi.run` creates a savepoint, not a new transaction.
A failure inside the nested transaction rolls the savepoint, not the whole thing. If you
want full rollback, return `{:error, reason}` from the `Multi.run`.

**7. Postgres aborts invalidate the whole TX**
If one step raises a constraint violation and you catch it in a `Multi.run`, subsequent
DB calls in the same transaction fail with `current transaction is aborted`. Return
`{:error, reason}` from the failing step instead of catching.

**8. When NOT to use this**
For cross-service workflows (pay with Stripe + ship with FedEx + notify Slack), `Multi`
cannot guarantee atomicity across services. Use a saga orchestrator (Commanded, Oban
workflows) with compensating transactions instead.

---

## Performance notes

A `Multi` with N steps issues N+2 SQL statements (`BEGIN`, N steps, `COMMIT`). Each
roundtrip is ~200μs on localhost, ~1–2ms across AZs. At 10+ steps the transaction is
network-bound, not CPU-bound. Batch inserts (`Repo.insert_all`) where possible:

```elixir
Multi.insert_all(multi, :line_items, LineItem, rows, returning: [:id])
```

One statement instead of N.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [`Ecto.Multi` — hexdocs](https://hexdocs.pm/ecto/Ecto.Multi.html) — every function with examples.
- [Dashbit: "Composable transactions with Ecto.Multi"](https://dashbit.co/blog/composable-transactions-with-ecto-multi) — José's canonical write-up.
- [Hex.pm source — `Hexpm.Accounts.User`](https://github.com/hexpm/hexpm) — real Multi pipelines in production.
- [Postgres: Transaction Isolation](https://www.postgresql.org/docs/current/transaction-iso.html) — understand what your Multi actually commits.
- [Saša Jurić: "Towards Maintainable Elixir"](https://www.theerlangelist.com/article/spawn_or_not) — context functions returning Multis.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
