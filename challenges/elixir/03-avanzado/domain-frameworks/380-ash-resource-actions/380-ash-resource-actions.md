# Ash Framework — Resource and Basic Actions

**Project**: `billing_core` — declarative `Invoice` resource with typed actions, validations, and a JSON:API layer wired through Ash.

## Project context

You are building the billing domain of a SaaS platform. The team previously wrote Phoenix contexts by hand: every entity had a hand-written `create_*`, `update_*`, `list_*`, and `get_*` function, hand-written changesets, hand-written authorization, hand-written query helpers, and a Phoenix controller that was 90% boilerplate. As the number of resources grew past twenty, every new field required touching five files.

Ash Framework turns a resource into a declaration: fields, relationships, actions (including custom ones), validations, policies, and calculations all live in one module. Ash then generates the changesets, query layer, JSON:API / GraphQL endpoints, and even extension points like pub/sub or state machines. The promise is: describe *what* the resource is, not *how* to manipulate it.

We target Ash 3.x with `ash_postgres` as the data layer and `ash_json_api` as the API layer. The goal of this exercise is to build the `Invoice` resource with four actions (`read`, `create`, `pay`, `void`), a custom validation, a calculation, and a code interface so the domain is callable from any context.

```
billing_core/
├── lib/
│   └── billing_core/
│       ├── application.ex
│       ├── billing.ex                      # Ash domain (formerly "api")
│       └── billing/
│           ├── invoice.ex                  # Ash resource
│           ├── changes/
│           │   └── mark_paid.ex            # custom change module
│           └── validations/
│               └── positive_amount.ex      # custom validation
├── priv/repo/migrations/
│   └── 20260412_create_invoices.exs
├── test/
│   └── billing_core/
│       └── invoice_test.exs
├── config/
│   └── config.exs
└── mix.exs
```

## Why Ash and not hand-written Phoenix contexts

Hand-written contexts give you maximum flexibility, but you pay for it every time the model changes. Ash generates the same machinery from a declaration:

- `Ecto.Changeset` pipelines → `actions` with validations and changes
- `Repo.all/one/preload` → `Ash.Query` with selectable fields and calculations
- `Bodyguard` / hand-rolled auth → `policies` at the resource level
- Phoenix controller + `open_api_spex` → `ash_json_api` / `ash_graphql` extensions
- `Broadway` side effects → `notifiers` (pub/sub) triggered by actions

You still drop into raw Ecto when you need raw SQL. The trade-off is a steeper learning curve and a more opinionated structure.

## Why a "domain" module

Ash 3.x renamed `Api` to `Domain`. The domain groups resources that belong together and exposes a `code_interface` so the rest of the app calls `Billing.create_invoice!(attrs)` instead of `Ash.create!(Invoice, attrs, domain: Billing)`. This is the seam between Ash and the rest of your codebase.

## Core concepts

### 1. Resource
A module `use Ash.Resource` with four DSL blocks: `attributes`, `relationships`, `actions`, `calculations`. Everything that is state or behaviour of the entity lives there.

### 2. Action
An action is a named operation (`:read`, `:create`, `:pay`, `:void`) with its own accept list, validations, and changes. Unlike a changeset, it is a first-class entity with a name, so authorization policies and notifiers target it directly.

### 3. Change
A module implementing `Ash.Resource.Change` that mutates the changeset during an action. Changes are composable and reusable across actions (e.g. `set_paid_at`).

### 4. Validation
A module implementing `Ash.Resource.Validation` that accepts or rejects a changeset. Lives separate from changes so you can validate without mutating.

### 5. Calculation
A derived field computed at read time (not stored). Ash can push the computation down to SQL when the data layer supports it.

### 6. Code interface
Functions generated on the domain module (`Billing.create_invoice/1`, `Billing.pay_invoice/2`). They are the public entry points of the domain.

## Design decisions

- **Option A — one action per state transition (`:pay`, `:void`)**: explicit, policy-targetable, easy to audit. Con: more actions to maintain.
- **Option B — generic `:update` that accepts `status`**: fewer actions. Con: policies and notifiers cannot target the specific transition; the state machine is implicit.

→ We pick Option A. State transitions in billing are audit-relevant events; naming them explicitly lets us attach policies ("only the billing role can void") and notifiers ("publish `invoice_voided` to Kafka") without guards inside the action body.

- **Option A — custom validation module**: reusable, testable in isolation, can be pattern-matched by error handling.
- **Option B — inline `validate` with anonymous function**: quick for one-offs. Con: untestable, not reusable, produces less descriptive errors.

→ Option A for `positive_amount`: the rule appears in multiple resources (Invoice, Refund, CreditNote) so we extract it up front.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule BillingCore.MixProject do
  use Mix.Project

  def project do
    [
      app: :billing_core,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {BillingCore.Application, []}
    ]
  end

  defp deps do
    [
      {:ash, "~> 3.4"},
      {:ash_postgres, "~> 2.4"},
      {:ecto_sql, "~> 3.12"},
      {:postgrex, "~> 0.19"},
      {:jason, "~> 1.4"}
    ]
  end
end
```

### Step 1: Application and Repo

**Objective**: Install `ash-functions`, `uuid-ossp`, `citext` in the Repo — Ash expressions compile to SQL and need these extensions to execute.

```elixir
defmodule BillingCore.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [BillingCore.Repo]
    Supervisor.start_link(children, strategy: :one_for_one, name: BillingCore.Supervisor)
  end
end

defmodule BillingCore.Repo do
  use AshPostgres.Repo, otp_app: :billing_core

  def installed_extensions, do: ["ash-functions", "uuid-ossp", "citext"]
end
```

### Step 2: Custom validation — positive amount

**Objective**: Encapsulate the `amount > 0` rule as an `Ash.Resource.Validation` — reusable across actions, surfaced as a structured field error.

```elixir
defmodule BillingCore.Billing.Validations.PositiveAmount do
  use Ash.Resource.Validation

  @impl true
  def validate(changeset, _opts, _context) do
    case Ash.Changeset.get_attribute(changeset, :amount_cents) do
      nil -> :ok
      amount when is_integer(amount) and amount > 0 -> :ok
      _ -> {:error, field: :amount_cents, message: "must be a positive integer"}
    end
  end
end
```

### Step 3: Custom change — mark as paid

**Objective**: Model the `status → :paid` transition as an `Ash.Resource.Change` — named changes make policies, notifiers, and audit hooks composable.

```elixir
defmodule BillingCore.Billing.Changes.MarkPaid do
  use Ash.Resource.Change

  @impl true
  def change(changeset, _opts, _context) do
    changeset
    |> Ash.Changeset.change_attribute(:status, :paid)
    |> Ash.Changeset.change_attribute(:paid_at, DateTime.utc_now())
  end
end
```

### Step 4: The resource

**Objective**: Declare `:pay` and `:void` as named state transitions with `attribute_equals` guards — each transition is addressable, policy-aware, and testable.

```elixir
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
```

### Step 5: The domain

**Objective**: Expose actions through a code interface (`pay_invoice`, `void_invoice`) — callers never touch changesets, so the data layer stays swappable.

```elixir
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
```

### Step 6: Migration

**Objective**: Define the database migration: Migration.

```elixir
defmodule BillingCore.Repo.Migrations.CreateInvoices do
  use Ecto.Migration

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

### Step 7: Configuration

**Objective**: Configure the runtime wiring for: Configuration.

```elixir
# config/config.exs
import Config

config :billing_core,
  ecto_repos: [BillingCore.Repo],
  ash_domains: [BillingCore.Billing]

config :billing_core, BillingCore.Repo,
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  database: "billing_core_#{Mix.env()}"
```

## Why this works

Every state transition is a named action, so policies, notifiers, and telemetry can target `:pay` vs `:void` without inspecting the changeset. The `validate attribute_equals(:status, :pending)` guards the transition atomically — if two concurrent `pay` actions race, Ash lifts the validation into SQL (or re-runs it) depending on `require_atomic?`. The calculation `overdue?` is pushed down to SQL by `ash_postgres`: `list_invoices(load: [:overdue?])` produces a single query, not N+1.

The code interface is the only surface the rest of the app sees. That means later we can change the data layer (say to ETS for a cache variant) without touching callers.

## Tests

```elixir
defmodule BillingCore.Billing.InvoiceTest do
  use ExUnit.Case, async: false

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

## Benchmark

```elixir
# bench/invoice_bench.exs
customer_id = Ecto.UUID.generate()

{:ok, invoice} =
  BillingCore.Billing.create_invoice(%{
    customer_id: customer_id,
    amount_cents: 1_000,
    currency: "USD"
  })

Benchee.run(
  %{
    "get_invoice" => fn -> BillingCore.Billing.get_invoice!(invoice.id) end,
    "list_invoices (limit 50)" => fn ->
      BillingCore.Billing.list_invoices!(query: [limit: 50])
    end
  },
  time: 5,
  warmup: 2
)
```

Target on a warm Postgres connection pool: `get_invoice` p50 < 700µs, `list_invoices` < 3ms for 50 rows. Anything above 5ms p50 usually means the domain is being recompiled on every call or the connection pool is saturated.

## Deep Dive

Specialized frameworks like Ash (business logic), Commanded (event sourcing), and Nx (numerical computing) abstract away common infrastructure but impose architectural constraints. Ash's declarative resource definitions simplify authorization and querying at the cost of reduced flexibility—deeply nested association policies can degrade query performance. Commanded's event store and aggregate roots enforce event sourcing discipline, making audit trails and temporal queries natural, but require careful snapshot strategy to avoid replaying years of events. Nx brings numerical computing to Elixir, but JIT compilation and lazy evaluation introduce latency; production models benefit from ahead-of-time compilation for inference. For IoT (Nerves), firmware updates must be atomic and resumable—OTA rollback on failure is non-negotiable. Choose frameworks that align with your scaling assumptions: Ash scales horizontally via read replicas; Commanded scales via sharding; Nx scales via distributed training.
## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Deep Dive: Domain Patterns and Production Implications

Domain-specific frameworks enforce module dependencies and architectural boundaries. Testing domain isolation ensures that constraints are maintained as the codebase grows. Production systems without boundary enforcement often become monolithic and hard to test.

---

## Trade-offs and production gotchas

**1. `require_atomic?: false` hides a lock**
Ash 3 defaults update actions to atomic SQL updates. `require_atomic? false` falls back to "read then update", which is vulnerable to lost updates if two workers race. Either keep it atomic (preferred) or wrap callers in `Ash.transaction/2` with `SELECT ... FOR UPDATE`.

**2. `accept [:voided_at]` accepts user input for a server-set field**
The example above intentionally shows the footgun: any field in `accept` is settable by callers via the code interface. Server-controlled fields (paid_at, voided_at) should be set by `change` modules only and kept out of `accept`. Review every `accept` list in code review.

**3. Generating JSON:API endpoints without policies**
Wiring `ash_json_api` exposes every action over HTTP. Without `policies`, every request is authorized. Add `use Ash.Policy.Authorizer` to the resource and define policies before exposing HTTP.

**4. Forgetting `public?: true` on attributes**
Ash 3 changed the default: attributes are private to the resource unless marked `public?: true`. Private attributes are invisible to the code interface and API layer. Every `attribute` you intend to expose needs the flag.

**5. `calculate` that the data layer cannot push down**
A calculation defined with `expr(...)` pushes to SQL. A calculation defined with a Module callback runs in Elixir per-row after the fetch — effectively N+1. When performance matters, keep calculations in `expr`.

**6. When NOT to use Ash**
If your domain has three CRUD resources and no plans to grow, the ceremony outweighs the benefit. Ash pays off starting around 8–10 resources with shared policies, notifiers, or a public API.

## Reflection

The `:pay` action guards `status == :pending` via a validation. Under high concurrency two callers could pass the validation and both try to mark the invoice paid. Should the guarantee live in Elixir (Ash validation), in SQL (a `CHECK` constraint plus unique partial index on `paid_at`), or both? Consider failure modes: what do you tell the second caller, and what does the audit log show?

## Resources

- [Ash Framework docs](https://hexdocs.pm/ash/)
- [Ash 3 upgrade guide](https://hexdocs.pm/ash/upgrading-to-3-0.html)
- [ash_postgres](https://hexdocs.pm/ash_postgres/)
- [Dashbit: Idioms vs frameworks (José Valim)](https://dashbit.co/blog/)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
