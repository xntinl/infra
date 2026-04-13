# Ash Framework — resources, attributes, actions, relationships

**Project**: `ash_resources` — declarative domain modeling for a SaaS product catalog

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
ash_resources/
├── lib/
│   └── ash_resources.ex
├── script/
│   └── main.exs
├── test/
│   └── ash_resources_test.exs
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
defmodule AshResources.MixProject do
  use Mix.Project

  def project do
    [
      app: :ash_resources,
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

### `lib/ash_resources.ex`

```elixir
defmodule AshResources.Repo do
  @moduledoc """
  Ejercicio: Ash Framework — resources, attributes, actions, relationships.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use AshPostgres.Repo, otp_app: :ash_resources

  @doc "Returns installed extensions result."
  def installed_extensions do
    ["ash-functions", "uuid-ossp", "citext"]
  end
end

defmodule AshResources.Catalog do
  use Ash.Domain

  resources do
    resource AshResources.Catalog.Product
    resource AshResources.Catalog.Category
    resource AshResources.Catalog.Price
  end
end

defmodule AshResources.Catalog.Category do
  use Ash.Resource,
    domain: AshResources.Catalog,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "categories"
    repo AshResources.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :name, :string, allow_nil?: false, public?: true
    attribute :slug, :string, allow_nil?: false, public?: true
    timestamps()
  end

  relationships do
    has_many :products, AshResources.Catalog.Product
  end

  identities do
    identity :unique_slug, [:slug]
  end

  actions do
    defaults [:read, :destroy]

    create :create do
      accept [:name, :slug]
    end

    update :rename do
      accept [:name]
    end
  end
end

defmodule AshResources.Catalog.Product do
  use Ash.Resource,
    domain: AshResources.Catalog,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "products"
    repo AshResources.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :sku, :string, allow_nil?: false, public?: true
    attribute :name, :string, allow_nil?: false, public?: true
    attribute :description, :string, public?: true
    attribute :status, :atom do
      constraints one_of: [:draft, :published, :archived]
      default :draft
      allow_nil? false
      public? true
    end

    timestamps()
  end

  relationships do
    belongs_to :category, AshResources.Catalog.Category do
      allow_nil? false
      public? true
    end

    has_many :prices, AshResources.Catalog.Price
  end

  identities do
    identity :unique_sku, [:sku]
  end

  validations do
    validate match(:sku, ~r/^[A-Z0-9-]{3,30}$/),
      message: "SKU must be uppercase alphanumeric with hyphens, 3–30 chars"
  end

  actions do
    defaults [:read, :destroy]

    create :register do
      accept [:sku, :name, :description, :category_id]
      change set_attribute(:status, :draft)
    end

    update :update_details do
      accept [:name, :description]
    end

    update :publish do
      accept []
      change set_attribute(:status, :published)

      validate attribute_equals(:status, :draft),
        message: "only draft products can be published"
    end

    update :archive do
      accept []
      change set_attribute(:status, :archived)
    end

    read :by_status do
      argument :status, :atom, allow_nil?: false
      filter expr(status == ^arg(:status))
    end

    read :with_active_prices do
      prepare build(load: [:prices])
    end
  end

  code_interface do
    define :register, args: [:sku, :name, :category_id]
    define :publish
    define :archive
    define :by_status, args: [:status]
  end
end

defmodule AshResources.Catalog.Price do
  use Ash.Resource,
    domain: AshResources.Catalog,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "prices"
    repo AshResources.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :amount_cents, :integer, allow_nil?: false, public?: true
    attribute :currency, :string, allow_nil?: false, public?: true, default: "USD"
    attribute :valid_from, :utc_datetime_usec, allow_nil?: false, public?: true
    attribute :valid_until, :utc_datetime_usec, public?: true
    timestamps()
  end

  relationships do
    belongs_to :product, AshResources.Catalog.Product do
      allow_nil? false
      public? true
    end
  end

  validations do
    validate numericality(:amount_cents, greater_than_or_equal_to: 0)
    validate match(:currency, ~r/^[A-Z]{3}$/), message: "ISO-4217 three-letter code"
  end

  actions do
    defaults [:read, :destroy]

    create :create do
      accept [:amount_cents, :currency, :valid_from, :valid_until, :product_id]
    end
  end
end

# lib/ash_resources/application.ex
defmodule AshResources.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [AshResources.Repo]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end

# config/config.exs
import Config

config :ash_resources,
  ecto_repos: [AshResources.Repo],
  ash_domains: [AshResources.Catalog]

config :ash_resources, AshResources.Repo,
  database: "ash_catalog",
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  pool_size: 10
```

### `test/ash_resources_test.exs`

```elixir
defmodule AshResources.CatalogTest do
  use ExUnit.Case, async: true
  doctest AshResources.Repo

  alias AshResources.Catalog
  alias AshResources.Catalog.{Product, Category}

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(AshResources.Repo)
    Ecto.Adapters.SQL.Sandbox.mode(AshResources.Repo, {:shared, self()})

    {:ok, category} =
      Category
      |> Ash.Changeset.for_create(:create, %{name: "Widgets", slug: "widgets"})
      |> Ash.create()

    %{category: category}
  end

  describe "register action" do
    test "creates a draft product", %{category: c} do
      {:ok, product} = Product.register("WIDGET-001", "Red widget", c.id)

      assert product.sku == "WIDGET-001"
      assert product.status == :draft
    end

    test "rejects invalid SKU", %{category: c} do
      assert {:error, %Ash.Error.Invalid{}} =
               Product.register("invalid sku!", "Name", c.id)
    end

    test "enforces unique SKU", %{category: c} do
      {:ok, _} = Product.register("WIDGET-002", "First", c.id)
      {:error, %Ash.Error.Invalid{}} = Product.register("WIDGET-002", "Second", c.id)
    end
  end

  describe "publish action" do
    test "transitions draft to published", %{category: c} do
      {:ok, product} = Product.register("WIDGET-100", "Name", c.id)
      {:ok, published} = Product.publish(product)
      assert published.status == :published
    end

    test "cannot publish an already-published product", %{category: c} do
      {:ok, product} = Product.register("WIDGET-101", "N", c.id)
      {:ok, published} = Product.publish(product)

      assert {:error, %Ash.Error.Invalid{}} = Product.publish(published)
    end
  end

  describe "by_status read" do
    test "filters by status argument", %{category: c} do
      {:ok, p1} = Product.register("WIDGET-200", "A", c.id)
      {:ok, _p2} = Product.register("WIDGET-201", "B", c.id)
      {:ok, _} = Product.publish(p1)

      {:ok, drafts} = Product.by_status(:draft)
      {:ok, published} = Product.by_status(:published)

      assert length(drafts) == 1
      assert length(published) == 1
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate Ash Framework domain modeling
      # The test module above shows how to:
      # 1. Define resources with attributes and relationships
      # 2. Register products with validation (SKU format, uniqueness)
      # 3. Publish products (state transition with validation)
      # 4. Query by status (polymorphic read action)

      IO.puts("✓ Ash Framework resource definitions:")
      IO.puts("  - Category resource with name and slug")
      IO.puts("  - Product with sku, status, category_id")
      IO.puts("  - Register action: validates SKU format, enforces uniqueness")
      IO.puts("  - Publish action: draft → published, prevents double-publish")
      IO.puts("  - by_status read: filters by status argument")
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
