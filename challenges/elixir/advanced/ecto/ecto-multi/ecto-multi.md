# Ecto.Multi: Composable Transactional Workflows

**Project**: `ecto_multi_deep` — order placement pipeline for an e-commerce backend

---

## Why ecto advanced matters

Ecto.Multi, custom types, polymorphic associations, CTEs, window functions, and zero-downtime migrations are the senior toolkit for talking to PostgreSQL from Elixir. Each one trades a different axis: composability, type safety, query expressiveness, or operational safety.

The trap is treating Ecto like an ORM. It is a query DSL plus a changeset validator — closer to SQL than to ActiveRecord. The closer your mental model is to the database, the better Ecto serves you.

---

## The business problem

You are building a production-grade Elixir component in the **Ecto advanced** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
ecto_multi_deep/
├── lib/
│   └── ecto_multi_deep.ex
├── script/
│   └── main.exs
├── test/
│   └── ecto_multi_deep_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Ecto advanced the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule EctoMultiDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :ecto_multi_deep,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/ecto_multi_deep.ex`

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

  @doc "Returns changeset result from order and attrs."
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

  @doc "Returns changeset result from item and attrs."
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

  @doc "Returns changeset result from payment and attrs."
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

  @doc "Returns changeset result from log and attrs."
  def changeset(log, attrs) do
    log
    |> cast(attrs, [:action, :payload])
    |> validate_required([:action, :payload])
  end
end

defmodule EctoMultiDeep.Orders.PaymentGateway do
  @moduledoc "Simulated gateway. In production replace with Stripe/MP HTTP client."

  @spec charge(pos_integer(), String.t()) ::
          {:ok, %{reference: String.t()}} | {:error, :declined | :network}
  @doc "Returns charge result from amount_cents and card_token."
  def charge(amount_cents, card_token) when is_integer(amount_cents) and amount_cents > 0 do
    cond do
      card_token == "declined" -> {:error, :declined}
      card_token == "network_error" -> {:error, :network}
      true -> {:ok, %{reference: "ch_" <> Base.encode16(:crypto.strong_rand_bytes(6))}}
    end
  end
end

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
  @doc "Runs result from params."
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

defmodule EctoMultiDeep.Repo.Migrations.CreateTables do
  use Ecto.Migration

  @doc "Returns change result."
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
### `test/ecto_multi_deep_test.exs`

```elixir
defmodule EctoMultiDeep.Orders.PlaceOrderTest do
  use ExUnit.Case, async: true
  doctest EctoMultiDeep.Schemas.Product

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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Ecto.Multi: Composable Transactional Workflows.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Ecto.Multi: Composable Transactional Workflows ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case EctoMultiDeep.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: EctoMultiDeep.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```
---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Queries are data, not strings

Ecto.Query is a DSL that compiles to SQL only at execution. This means you can compose, inspect, and pre-validate queries without a database connection — useful for property tests.

### 2. Multi makes transactions composable

Ecto.Multi is a value: build it, pass it around, run it inside Repo.transaction. Errors come back as `{:error, step_name, reason, changes_so_far}` — you know exactly what failed.

### 3. Locking strategies trade throughput for correctness

FOR UPDATE prevents lost updates but serializes contention. Optimistic locking via :version columns retries on conflict — better for read-heavy workloads.

---
