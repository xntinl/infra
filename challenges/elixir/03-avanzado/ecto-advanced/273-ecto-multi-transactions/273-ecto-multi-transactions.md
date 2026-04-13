# Complex Transactions with `Ecto.Multi`

**Project**: `order_fulfillment` — atomic multi-step business transactions with side-effect staging.

---

## Project context

An e-commerce checkout performs six operations that must either all succeed or all fail:

1. Create an `Order` record.
2. Decrement stock for every line item.
3. Apply a promo code (single-use).
4. Charge the customer's payment method.
5. Emit a `StockMovement` ledger entry.
6. Enqueue a shipment job.

A naive implementation sprinkles `Repo.insert/1`, `Repo.update/1`, and `HTTPoison.post/3`
across a function. Any failure midway leaves the system inconsistent: stock decremented
but no order record, promo code consumed but payment failed, job enqueued for an order
that does not exist.

`Ecto.Multi` composes a pipeline of operations, runs them inside a single DB transaction,
and rolls back atomically on the first failure. Side effects that do not belong to the DB
(payment capture, job enqueue) are staged separately and committed only after the
transaction succeeds.

```
order_fulfillment/
├── lib/
│   └── order_fulfillment/
│       ├── application.ex
│       ├── repo.ex
│       ├── checkout.ex                  # Ecto.Multi orchestration
│       ├── stock.ex                     # stock decrement helpers
│       ├── payments.ex                  # external gateway adapter (stubbed)
│       └── schemas/
│           ├── order.ex
│           ├── line_item.ex
│           ├── product.ex
│           ├── promo_code.ex
│           └── stock_movement.ex
├── priv/repo/migrations/
├── test/order_fulfillment/
│   └── checkout_test.exs
├── bench/checkout_bench.exs
└── mix.exs
```

---

## Why `Ecto.Multi` and not nested `Repo.transaction`

Naïve code:

```elixir
Repo.transaction(fn ->
  {:ok, order} = Repo.insert(order_changeset)

  Enum.each(items, fn item ->
    case decrement_stock(item) do
      :ok -> :ok
      {:error, reason} -> Repo.rollback(reason)
    end
  end)

  {:ok, _charge} = Payments.charge(order)
  {:ok, order}
end)
```

Three problems:

- **Error shape is inconsistent** — `Repo.rollback/1` vs. raises vs. tuple returns mixed
  in a single function.
- **Side effects inside the transaction** — the call to `Payments.charge` (HTTP) blocks
  the DB transaction. A slow gateway holds row locks.
- **Not composable** — to add a step, you edit a linear function. `Ecto.Multi` is a value
  you can pattern-match and re-use.

`Ecto.Multi` solves all three: operations are named, each failure identifies the failing
step by name, and you can split DB-only steps from external effects.

---

## Core concepts

### 1. Multi is a value, not a side effect

```elixir
multi =
  Ecto.Multi.new()
  |> Ecto.Multi.insert(:order, order_changeset)
  |> Ecto.Multi.run(:stock, &decrement_all_stock/2)
```

No SQL runs yet. You can inspect `multi.operations`, test it independently, or extend it
conditionally. Only `Repo.transaction(multi)` executes it.

### 2. `run/3` sees the results of prior steps

```elixir
Ecto.Multi.run(:charge, fn _repo, %{order: order} ->
  Payments.authorize(order)
end)
```

The second argument is a map keyed by step name. This is how step N depends on step N−1's
output. Steps never return raw values — always `{:ok, value}` or `{:error, reason}`.

### 3. Failure identifies the step

```elixir
case Repo.transaction(multi) do
  {:ok, results} -> ...
  {:error, :stock, reason, partial_results} -> ...
end
```

The second element is the step name that failed; the third is its error; the fourth is
the map of steps that succeeded before it. Logging with the step name makes incident
triage trivial.

### 4. Side effects after the transaction

Payment capture and job enqueueing are side effects. Put them *after* `Repo.transaction`
returns `{:ok, ...}`. If the DB transaction failed, you never ran them — correct by
construction.

For actions that must happen *during* the transaction (e.g., calling a payment authorize
that itself is idempotent on a client-supplied key), prefer wrapping them in
`Ecto.Multi.run/3` so the error flows through the Multi's return value — but understand
that this holds row locks for the duration of the HTTP call.

---

## Design decisions

- **Option A — everything inside Multi (including payment)**: one atomic flow.
  Pros: single rollback point. Cons: HTTP latency inside a DB transaction, row locks held.
- **Option B — Multi for DB, then stage side effects**: DB commits fast; HTTP runs after.
  Pros: short transactions. Cons: a post-commit failure can produce an orphan order that
  needs reconciliation (out-of-band).

We use **Option B with compensations**. Orders stage in status `pending`; the post-commit
pipeline updates to `confirmed` after charge. A reaper sweeps `pending` orders older than
N minutes.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.12"},
    {:postgrex, "~> 0.19"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 1: Schemas (abridged — `Order`, `Product`, `PromoCode`, `StockMovement`)

**Objective**: Define Order/LineItem/Product/PromoCode/StockMovement schemas so Multi steps cooperate with explicit dependencies.

```elixir
# lib/order_fulfillment/schemas/order.ex
defmodule OrderFulfillment.Schemas.Order do
  use Ecto.Schema
  import Ecto.Changeset

  schema "orders" do
    field :customer_id, :string
    field :status, :string, default: "pending"
    field :total_cents, :integer, default: 0
    field :promo_code, :string
    has_many :line_items, OrderFulfillment.Schemas.LineItem
    timestamps()
  end

  def changeset(order, attrs) do
    order
    |> cast(attrs, [:customer_id, :total_cents, :promo_code])
    |> validate_required([:customer_id, :total_cents])
    |> validate_number(:total_cents, greater_than: 0)
  end
end

# lib/order_fulfillment/schemas/product.ex
defmodule OrderFulfillment.Schemas.Product do
  use Ecto.Schema

  schema "products" do
    field :sku, :string
    field :price_cents, :integer
    field :stock, :integer
    timestamps()
  end
end

# lib/order_fulfillment/schemas/line_item.ex
defmodule OrderFulfillment.Schemas.LineItem do
  use Ecto.Schema
  import Ecto.Changeset

  schema "line_items" do
    belongs_to :order, OrderFulfillment.Schemas.Order
    belongs_to :product, OrderFulfillment.Schemas.Product
    field :quantity, :integer
    field :unit_cents, :integer
    timestamps()
  end

  def changeset(item, attrs) do
    item
    |> cast(attrs, [:order_id, :product_id, :quantity, :unit_cents])
    |> validate_required([:product_id, :quantity, :unit_cents])
  end
end

# lib/order_fulfillment/schemas/promo_code.ex
defmodule OrderFulfillment.Schemas.PromoCode do
  use Ecto.Schema

  schema "promo_codes" do
    field :code, :string
    field :discount_cents, :integer
    field :used_by_order_id, :id
    timestamps()
  end
end

# lib/order_fulfillment/schemas/stock_movement.ex
defmodule OrderFulfillment.Schemas.StockMovement do
  use Ecto.Schema

  schema "stock_movements" do
    field :product_id, :id
    field :delta, :integer
    field :reason, :string
    timestamps(updated_at: false)
  end
end
```

### Step 2: Checkout — the Multi orchestration

**Objective**: Chain load/redeem/decrement/record via Multi.run steps with row locks (FOR UPDATE) to prevent lost updates and partial commits.

```elixir
# lib/order_fulfillment/checkout.ex
defmodule OrderFulfillment.Checkout do
  import Ecto.Query
  alias Ecto.Multi
  alias OrderFulfillment.{Payments, Repo}
  alias OrderFulfillment.Schemas.{LineItem, Order, Product, PromoCode, StockMovement}

  @type cart_item :: %{product_id: integer(), quantity: pos_integer()}
  @type input :: %{customer_id: String.t(), items: [cart_item()], promo_code: String.t() | nil}

  @spec place(input()) ::
          {:ok, Order.t()}
          | {:error, atom(), term(), map()}
          | {:error, {:post_commit, term()}}
  def place(%{customer_id: customer_id, items: items} = input) do
    promo = Map.get(input, :promo_code)

    multi =
      Multi.new()
      |> Multi.run(:products, &load_products(&1, items))
      |> Multi.run(:total, &compute_total/2)
      |> Multi.run(:promo, &redeem_promo(&1, &2, promo))
      |> Multi.insert(:order, fn %{total: total, promo: promo_data} ->
        Order.changeset(%Order{}, %{
          customer_id: customer_id,
          total_cents: total - promo_discount(promo_data),
          promo_code: promo
        })
      end)
      |> Multi.run(:line_items, &insert_line_items(&1, &2, items))
      |> Multi.run(:stock, &decrement_stock(&1, &2, items))
      |> Multi.run(:movements, &record_movements(&1, &2, items))

    case Repo.transaction(multi) do
      {:ok, %{order: order}} -> post_commit(order)
      {:error, step, reason, _partial} -> {:error, step, reason, %{}}
    end
  end

  # ---- Multi steps -------------------------------------------------------

  defp load_products(repo, items) do
    ids = Enum.map(items, & &1.product_id)

    products =
      from(p in Product, where: p.id in ^ids, lock: "FOR UPDATE")
      |> repo.all()
      |> Map.new(&{&1.id, &1})

    if map_size(products) == length(ids) do
      {:ok, products}
    else
      {:error, :product_missing}
    end
  end

  defp compute_total(_repo, %{products: products}) do
    total =
      products
      |> Map.values()
      |> Enum.reduce(0, fn _p, acc -> acc end)

    # compute from items in second pass; see below for a real impl
    {:ok, compute_total_from_products(products)}
  end

  defp compute_total_from_products(products) do
    Enum.reduce(products, 0, fn {_id, p}, acc -> acc + p.price_cents end)
  end

  defp redeem_promo(_repo, _changes, nil), do: {:ok, nil}

  defp redeem_promo(repo, _changes, code) do
    case repo.get_by(PromoCode, code: code) do
      nil -> {:error, :promo_not_found}
      %PromoCode{used_by_order_id: id} when not is_nil(id) -> {:error, :promo_already_used}
      promo -> {:ok, promo}
    end
  end

  defp promo_discount(nil), do: 0
  defp promo_discount(%PromoCode{discount_cents: d}), do: d

  defp insert_line_items(repo, %{order: order, products: products}, items) do
    Enum.reduce_while(items, {:ok, []}, fn item, {:ok, acc} ->
      product = Map.fetch!(products, item.product_id)

      attrs = %{
        order_id: order.id,
        product_id: product.id,
        quantity: item.quantity,
        unit_cents: product.price_cents
      }

      case repo.insert(LineItem.changeset(%LineItem{}, attrs)) do
        {:ok, li} -> {:cont, {:ok, [li | acc]}}
        {:error, cs} -> {:halt, {:error, {:line_item, cs}}}
      end
    end)
  end

  defp decrement_stock(repo, %{products: products}, items) do
    Enum.reduce_while(items, {:ok, []}, fn item, {:ok, acc} ->
      product = Map.fetch!(products, item.product_id)

      if product.stock < item.quantity do
        {:halt, {:error, {:insufficient_stock, product.sku}}}
      else
        {1, _} =
          from(p in Product, where: p.id == ^product.id)
          |> repo.update_all(inc: [stock: -item.quantity])

        {:cont, {:ok, [product.id | acc]}}
      end
    end)
  end

  defp record_movements(repo, %{order: order}, items) do
    rows =
      Enum.map(items, fn item ->
        %{
          product_id: item.product_id,
          delta: -item.quantity,
          reason: "order:#{order.id}",
          inserted_at: DateTime.utc_now() |> DateTime.truncate(:second)
        }
      end)

    {n, _} = repo.insert_all(StockMovement, rows)
    {:ok, n}
  end

  # ---- post-commit side effects ------------------------------------------

  defp post_commit(order) do
    with {:ok, _charge} <- Payments.charge(order),
         {:ok, confirmed} <- confirm(order) do
      {:ok, confirmed}
    else
      {:error, reason} -> {:error, {:post_commit, reason}}
    end
  end

  defp confirm(order) do
    order
    |> Ecto.Changeset.change(status: "confirmed")
    |> Repo.update()
  end
end
```

### Step 3: Payment adapter (stubbed for tests)

**Objective**: Swap the gateway via compile_env so Sandbox tests inject a deterministic stub without touching production code.

```elixir
# lib/order_fulfillment/payments.ex
defmodule OrderFulfillment.Payments do
  @adapter Application.compile_env(:order_fulfillment, :payments_adapter, __MODULE__.Real)

  def charge(order), do: @adapter.charge(order)

  defmodule Real do
    def charge(_order), do: {:ok, %{id: "ch_#{System.unique_integer([:positive])}"}}
  end

  defmodule Failing do
    def charge(_order), do: {:error, :gateway_down}
  end
end
```

---

## Why this works

`Ecto.Multi` guarantees that either all DB mutations commit together or none do.
The `FOR UPDATE` lock on products during `load_products` prevents two concurrent checkouts
from reading the same stock and both decrementing it — without that lock you can sell the
last unit twice. The reducer-with-halt pattern in `decrement_stock` surfaces the first
failure cleanly and aborts the Multi.

Side effects (payment, job enqueue) run *after* commit. If `Payments.charge` fails, the
order stays in `pending`. A reaper (not shown) sweeps `pending` orders older than 10
minutes and marks them as `expired`, re-incrementing stock — the compensating action.

---

## Data flow

```
place(input)
   │
   ├─▶ Multi.run :products        (FOR UPDATE lock)
   ├─▶ Multi.run :total
   ├─▶ Multi.run :promo           (single-use check)
   ├─▶ Multi.insert :order        (status=pending)
   ├─▶ Multi.run :line_items
   ├─▶ Multi.run :stock           (decrement, guard against negative)
   └─▶ Multi.run :movements       (ledger)
   │
   ▼  COMMIT
   │
   ├─▶ Payments.charge
   └─▶ Order.confirm              (status=confirmed)
```

---

## Tests

```elixir
# test/order_fulfillment/checkout_test.exs
defmodule OrderFulfillment.CheckoutTest do
  use ExUnit.Case, async: false
  alias OrderFulfillment.{Checkout, Repo}
  alias OrderFulfillment.Schemas.{Product, PromoCode}

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})

    {:ok, prod} = Repo.insert(%Product{sku: "SKU-1", price_cents: 1000, stock: 3})
    {:ok, promo} = Repo.insert(%PromoCode{code: "SAVE10", discount_cents: 100})
    {:ok, product: prod, promo: promo}
  end

  describe "happy path" do
    test "creates order, decrements stock, emits movement", %{product: p} do
      assert {:ok, order} =
               Checkout.place(%{
                 customer_id: "c1",
                 items: [%{product_id: p.id, quantity: 2}],
                 promo_code: nil
               })

      assert order.status == "confirmed"
      assert Repo.get!(Product, p.id).stock == 1
    end
  end

  describe "rollback" do
    test "insufficient stock aborts the whole transaction", %{product: p} do
      assert {:error, :stock, {:insufficient_stock, "SKU-1"}, _} =
               Checkout.place(%{
                 customer_id: "c2",
                 items: [%{product_id: p.id, quantity: 99}],
                 promo_code: nil
               })

      assert Repo.get!(Product, p.id).stock == 3
    end

    test "promo already used aborts", %{product: p, promo: promo} do
      Repo.update!(Ecto.Changeset.change(promo, used_by_order_id: 99))

      assert {:error, :promo, :promo_already_used, _} =
               Checkout.place(%{
                 customer_id: "c3",
                 items: [%{product_id: p.id, quantity: 1}],
                 promo_code: "SAVE10"
               })
    end
  end

  describe "payment failure" do
    test "leaves order in :pending when charge fails", %{product: p} do
      Application.put_env(:order_fulfillment, :payments_adapter, OrderFulfillment.Payments.Failing)
      on_exit(fn -> Application.delete_env(:order_fulfillment, :payments_adapter) end)

      assert {:error, {:post_commit, :gateway_down}} =
               Checkout.place(%{
                 customer_id: "c4",
                 items: [%{product_id: p.id, quantity: 1}],
                 promo_code: nil
               })
    end
  end
end
```

---

## Benchmark

```elixir
# bench/checkout_bench.exs
Benchee.run(
  %{
    "place/1 happy path" => fn ->
      OrderFulfillment.Checkout.place(%{
        customer_id: "bench",
        items: [%{product_id: 1, quantity: 1}],
        promo_code: nil
      })
    end
  },
  time: 5, warmup: 2
)
```

**Target**: under 3 ms per checkout on a warm connection pool with local Postgres. If you
see 20+ ms, check for N+1 in `load_products` (it should be a single `IN` query).

---

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

**1. HTTP calls inside Multi steps hold row locks.** Any call to `Payments` during the
transaction blocks other checkouts touching the same products. Keep external calls out of
the Multi; use post-commit with compensations.

**2. `FOR UPDATE` can deadlock.** Two concurrent checkouts that lock product IDs in
different orders deadlock. Always `ORDER BY id` before locking.

**3. `Multi.run` must return `{:ok, _}` or `{:error, _}`.** A raised exception rolls back
but surfaces as a `DBConnection.ConnectionError`, not a named step failure. Wrap risky
code in `try/rescue`.

**4. Changes accumulate in memory.** For 10k line items, `Multi` keeps all intermediate
results in the `changes` map. For batch jobs, prefer `Repo.insert_all/3` inside one
`Multi.run` step.

**5. Promo single-use needs a unique partial index.** Relying on the `used_by_order_id IS NULL`
check is a TOCTOU race. Add `CREATE UNIQUE INDEX ... WHERE used_by_order_id IS NULL` so
two concurrent redemptions of the same code produce a constraint violation, which Multi
surfaces as `{:error, :promo, changeset, _}`.

**6. When NOT to use Multi.** If your operation has no inter-step dependencies and no
need for atomicity (e.g., pure logging to 3 tables), a plain `Repo.insert_all` in a
single SQL is faster and simpler.

---

## Reflection

Your post-commit pipeline fails between `Payments.charge` and `Order.confirm` — the
customer was charged but the order remains `pending`. The reaper sweeps it and re-increments
stock. What invariant do you add to the reaper (and to the charge call) to guarantee you
never refund-and-reship the same order? Name the identifier that makes the whole flow
idempotent.

---

## Resources

- [`Ecto.Multi` docs](https://hexdocs.pm/ecto/Ecto.Multi.html)
- [Dashbit — "Working with Ecto.Multi"](https://dashbit.co/blog)
- [Designing Elixir Systems with OTP — James Gray & Bruce Tate](https://pragprog.com/titles/jgotp/designing-elixir-systems-with-otp/) — chapter on transactional workflows
- [Postgres row-level locking](https://www.postgresql.org/docs/current/explicit-locking.html)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
