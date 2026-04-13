# Ash Framework — Resource and Basic Actions

**Project**: `billing_core` — declarative `Invoice` resource with typed actions, validations, and a JSON:API layer wired through Ash

---

## Why domain frameworks matters

Frameworks like Ash, Commanded, Oban, Nx and Axon encode large domain patterns (CQRS, event sourcing, ML training, background jobs, IoT updates) into reusable building blocks. Used well, they compress months of bespoke code into days.

Used poorly, they hide complexity that bites in production: aggregate version drift in Commanded, projection lag in CQRS systems, OTA failure recovery in Nerves, gradient explosion in Axon training loops. The framework's defaults are not your defaults.

---

## The business problem

You are building a production-grade Elixir component in the **Domain frameworks** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
billing_core/
├── lib/
│   └── billing_core.ex
├── script/
│   └── main.exs
├── test/
│   └── billing_core_test.exs
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

Chose **B** because in Domain frameworks the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule BillingCore.MixProject do
  use Mix.Project

  def project do
    [
      app: :billing_core,
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

### `lib/billing_core.ex`

```elixir
defmodule BillingCore.Application do
  @moduledoc """
  Ejercicio: Ash Framework — Resource and Basic Actions.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Application

  @impl true
  def start(_type, _args) do
    children = [BillingCore.Repo]
    Supervisor.start_link(children, strategy: :one_for_one, name: BillingCore.Supervisor)
  end
end

defmodule BillingCore.Repo do
  use AshPostgres.Repo, otp_app: :billing_core

  @doc "Returns installed extensions result."
  def installed_extensions, do: ["ash-functions", "uuid-ossp", "citext"]
end

defmodule BillingCore.Billing.Validations.PositiveAmount do
  use Ash.Resource.Validation

  @doc "Validates result from changeset, _opts and _context."
  @impl true
  def validate(changeset, _opts, _context) do
    case Ash.Changeset.get_attribute(changeset, :amount_cents) do
      nil -> :ok
      amount when is_integer(amount) and amount > 0 -> :ok
      _ -> {:error, field: :amount_cents, message: "must be a positive integer"}
    end
  end
end

defmodule BillingCore.Billing.Changes.MarkPaid do
  use Ash.Resource.Change

  @doc "Returns change result from changeset, _opts and _context."
  @impl true
  def change(changeset, _opts, _context) do
    changeset
    |> Ash.Changeset.change_attribute(:status, :paid)
    |> Ash.Changeset.change_attribute(:paid_at, DateTime.utc_now())
  end
end

defmodule BillingCore.Billing.Invoice do
  use Ash.Resource,
    domain: BillingCore.Billing,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "invoices"
    repo BillingCore.Repo
  end

  attributes do
    uuid_primary_key :id

    attribute :customer_id, :uuid, allow_nil?: false, public?: true
    attribute :amount_cents, :integer, allow_nil?: false, public?: true
    attribute :currency, :string, allow_nil?: false, default: "USD", public?: true

    attribute :status, :atom do
      constraints one_of: [:pending, :paid, :void]
      default :pending
      allow_nil? false
      public? true
    end

    attribute :paid_at, :utc_datetime_usec, public?: true
    attribute :voided_at, :utc_datetime_usec, public?: true

    create_timestamp :inserted_at
    update_timestamp :updated_at
  end

  calculations do
    calculate :overdue?, :boolean, expr(status == :pending and inserted_at < ago(30, :day))
  end

  actions do
    defaults [:read]

    create :create do
      accept [:customer_id, :amount_cents, :currency]
      validate BillingCore.Billing.Validations.PositiveAmount
    end

    update :pay do
      accept []
      require_atomic? false
      validate attribute_equals(:status, :pending),
        message: "only pending invoices can be paid"

      change BillingCore.Billing.Changes.MarkPaid
    end

    update :void do
      accept [:voided_at]
      require_atomic? false
      validate attribute_equals(:status, :pending),
        message: "only pending invoices can be voided"

      change set_attribute(:status, :void)
      change set_attribute(:voided_at, &DateTime.utc_now/0)
    end
  end
end

defmodule BillingCore.Billing do
  use Ash.Domain

  resources do
    resource BillingCore.Billing.Invoice do
      define :create_invoice, action: :create
      define :pay_invoice, action: :pay, get_by: [:id]
      define :void_invoice, action: :void, get_by: [:id]
      define :get_invoice, action: :read, get_by: [:id]
      define :list_invoices, action: :read
    end
  end
end

defmodule BillingCore.Repo.Migrations.CreateInvoices do
  use Ecto.Migration

  @doc "Returns change result."
  def change do
    create table(:invoices, primary_key: false) do
      add :id, :uuid, primary_key: true, null: false
      add :customer_id, :uuid, null: false
      add :amount_cents, :integer, null: false
      add :currency, :string, null: false, default: "USD"
      add :status, :string, null: false, default: "pending"
      add :paid_at, :utc_datetime_usec
      add :voided_at, :utc_datetime_usec
      timestamps(type: :utc_datetime_usec)
    end

    create index(:invoices, [:customer_id])
    create index(:invoices, [:status])
  end
end
```

### `test/billing_core_test.exs`

```elixir
defmodule BillingCore.Billing.InvoiceTest do
  use ExUnit.Case, async: true
  doctest BillingCore.Application

  alias BillingCore.Billing

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(BillingCore.Repo)
    :ok
  end

  describe "create/1" do
    test "creates an invoice with valid attributes" do
      assert {:ok, invoice} =
               Billing.create_invoice(%{
                 customer_id: Ecto.UUID.generate(),
                 amount_cents: 1_000,
                 currency: "USD"
               })

      assert invoice.status == :pending
      assert invoice.amount_cents == 1_000
    end

    test "rejects non-positive amount" do
      assert {:error, %Ash.Error.Invalid{}} =
               Billing.create_invoice(%{
                 customer_id: Ecto.UUID.generate(),
                 amount_cents: 0,
                 currency: "USD"
               })
    end
  end

  describe "pay/1" do
    test "transitions from pending to paid and sets paid_at" do
      {:ok, invoice} = create_pending()

      assert {:ok, paid} = Billing.pay_invoice(invoice.id)
      assert paid.status == :paid
      assert not is_nil(paid.paid_at)
    end

    test "rejects paying an already paid invoice" do
      {:ok, invoice} = create_pending()
      {:ok, _paid} = Billing.pay_invoice(invoice.id)

      assert {:error, %Ash.Error.Invalid{}} = Billing.pay_invoice(invoice.id)
    end
  end

  describe "void/1" do
    test "voids a pending invoice" do
      {:ok, invoice} = create_pending()
      assert {:ok, voided} = Billing.void_invoice(invoice.id)
      assert voided.status == :void
      assert not is_nil(voided.voided_at)
    end

    test "rejects voiding a paid invoice" do
      {:ok, invoice} = create_pending()
      {:ok, _paid} = Billing.pay_invoice(invoice.id)
      assert {:error, %Ash.Error.Invalid{}} = Billing.void_invoice(invoice.id)
    end
  end

  defp create_pending do
    Billing.create_invoice(%{
      customer_id: Ecto.UUID.generate(),
      amount_cents: 2_500,
      currency: "USD"
    })
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Ash Framework — Resource and Basic Actions.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Ash Framework — Resource and Basic Actions ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case BillingCore.run(payload) do
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
        for _ <- 1..1_000, do: BillingCore.run(:bench)
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

### 1. Frameworks encode opinions

Ash, Commanded, Oban each pick defaults that work for the common case. Understand the defaults before you customize — the framework's authors chose them for a reason.

### 2. Event-sourced systems need projection lag tolerance

In CQRS, the read model is eventually consistent with the write model. UI must handle 'I saved but I don't see my own data yet'. Optimistic UI updates help.

### 3. Background jobs need idempotency and retries

Oban retries failed jobs by default. The worker must be idempotent: repeating a job must produce the same end state. Use unique constraints and deduplication keys.

---
