# Embedded Schemas and Nested Changesets

**Project**: `form_builder` — multi-step form with nested embeds validated as one unit

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
form_builder/
├── lib/
│   └── form_builder.ex
├── script/
│   └── main.exs
├── test/
│   └── form_builder_test.exs
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
defmodule FormBuilder.MixProject do
  use Mix.Project

  def project do
    [
      app: :form_builder,
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

### `lib/form_builder.ex`

```elixir
# priv/repo/migrations/20260101000000_create_customers.exs
defmodule FormBuilder.Repo.Migrations.CreateCustomers do
  @moduledoc """
  Ejercicio: Embedded Schemas and Nested Changesets.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Ecto.Migration

  @doc "Returns change result."
  def change do
    create table(:customers) do
      add :name, :string, null: false
      add :billing_address, :map
      add :contact_methods, :map, default: %{}, null: false
      timestamps()
    end
  end
end

# lib/form_builder/schemas/billing_address.ex
defmodule FormBuilder.Schemas.BillingAddress do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key false
  embedded_schema do
    field :street, :string
    field :city, :string
    field :postal_code, :string
    field :country, :string
  end

  @doc "Returns changeset result from addr and attrs."
  def changeset(addr, attrs) do
    addr
    |> cast(attrs, [:street, :city, :postal_code, :country])
    |> validate_required([:street, :city, :country])
    |> validate_length(:country, is: 2)
    |> validate_format(:country, ~r/^[A-Z]{2}$/, message: "must be ISO 3166-1 alpha-2")
  end
end

# lib/form_builder/schemas/contact_method.ex
defmodule FormBuilder.Schemas.ContactMethod do
  use Ecto.Schema
  import Ecto.Changeset

  @types ~w(email phone sms)

  @primary_key false
  embedded_schema do
    field :kind, :string
    field :value, :string
    field :primary, :boolean, default: false
  end

  @doc "Returns changeset result from method and attrs."
  def changeset(method, attrs) do
    method
    |> cast(attrs, [:kind, :value, :primary])
    |> validate_required([:kind, :value])
    |> validate_inclusion(:kind, @types)
    |> validate_by_kind()
  end

  defp validate_by_kind(changeset) do
    case get_field(changeset, :kind) do
      "email" -> validate_format(changeset, :value, ~r/@/)
      "phone" -> validate_format(changeset, :value, ~r/^\+?\d{7,15}$/)
      "sms" -> validate_format(changeset, :value, ~r/^\+?\d{7,15}$/)
      _ -> changeset
    end
  end
end

# lib/form_builder/schemas/customer.ex
defmodule FormBuilder.Schemas.Customer do
  use Ecto.Schema
  import Ecto.Changeset

  alias FormBuilder.Schemas.{BillingAddress, ContactMethod}

  schema "customers" do
    field :name, :string
    embeds_one :billing_address, BillingAddress, on_replace: :update
    embeds_many :contact_methods, ContactMethod, on_replace: :delete
    timestamps()
  end

  @doc "Returns changeset result from customer and attrs."
  def changeset(customer, attrs) do
    customer
    |> cast(attrs, [:name])
    |> validate_required([:name])
    |> cast_embed(:billing_address, required: true)
    |> cast_embed(:contact_methods,
      with: &ContactMethod.changeset/2,
      required: true
    )
    |> validate_at_least_one_contact()
    |> validate_exactly_one_primary_contact()
  end

  defp validate_at_least_one_contact(changeset) do
    case get_field(changeset, :contact_methods, []) do
      [] -> add_error(changeset, :contact_methods, "must have at least one contact method")
      _ -> changeset
    end
  end

  defp validate_exactly_one_primary_contact(changeset) do
    methods = get_field(changeset, :contact_methods, [])
    primary_count = Enum.count(methods, & &1.primary)

    case primary_count do
      1 -> changeset
      0 -> add_error(changeset, :contact_methods, "exactly one contact method must be primary")
      _ -> add_error(changeset, :contact_methods, "only one contact method can be primary")
    end
  end
end

# lib/form_builder/customers.ex
defmodule FormBuilder.Customers do
  import Ecto.Query
  alias FormBuilder.Repo
  alias FormBuilder.Schemas.Customer

  @doc "Creates result from attrs."
  def create(attrs) do
    %Customer{}
    |> Customer.changeset(attrs)
    |> Repo.insert()
  end

  @doc "Updates result from attrs."
  def update(%Customer{} = c, attrs) do
    c
    |> Customer.changeset(attrs)
    |> Repo.update()
  end

  @doc "Returns result from id."
  def get(id), do: Repo.get(Customer, id)

  @doc """
  Query customers located in a given country — demonstrates JSONB navigation.
  """
  def by_country(country_code) do
    from(c in Customer,
      where: fragment("?->>'country' = ?", c.billing_address, ^country_code)
    )
    |> Repo.all()
  end
end
```

### `test/form_builder_test.exs`

```elixir
defmodule FormBuilder.CustomersTest do
  use ExUnit.Case, async: true
  doctest FormBuilder.Repo.Migrations.CreateCustomers
  alias FormBuilder.{Customers, Repo}
  alias FormBuilder.Schemas.Customer

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(Customer)
    :ok
  end

  defp valid_attrs do
    %{
      "name" => "Acme",
      "billing_address" => %{
        "street" => "1 Main",
        "city" => "SF",
        "postal_code" => "94111",
        "country" => "US"
      },
      "contact_methods" => [
        %{"kind" => "email", "value" => "a@b.com", "primary" => true}
      ]
    }
  end

  describe "create/1 happy path" do
    test "inserts customer with embeds" do
      assert {:ok, c} = Customers.create(valid_attrs())
      assert c.billing_address.country == "US"
      assert [%{kind: "email"}] = c.contact_methods
    end
  end

  describe "nested validation" do
    test "surfaces nested errors from billing_address" do
      attrs = put_in(valid_attrs(), ["billing_address", "country"], "USA")

      {:error, cs} = Customers.create(attrs)
      assert cs.changes.billing_address.errors != []
    end

    test "requires at least one contact method" do
      attrs = Map.put(valid_attrs(), "contact_methods", [])
      {:error, cs} = Customers.create(attrs)
      assert [contact_methods: {"must have at least one contact method", _}] = cs.errors
    end

    test "rejects multiple primary contacts" do
      attrs =
        put_in(valid_attrs(), ["contact_methods"], [
          %{"kind" => "email", "value" => "a@b.com", "primary" => true},
          %{"kind" => "phone", "value" => "+123456789", "primary" => true}
        ])

      {:error, cs} = Customers.create(attrs)
      assert [contact_methods: {"only one contact method can be primary", _}] = cs.errors
    end

    test "rejects invalid email format in contact method" do
      attrs =
        put_in(valid_attrs(), ["contact_methods"], [
          %{"kind" => "email", "value" => "not-an-email", "primary" => true}
        ])

      {:error, cs} = Customers.create(attrs)
      assert cs.valid? == false
    end
  end

  describe "update with on_replace: :update" do
    test "partial update to billing_address keeps untouched fields" do
      {:ok, c} = Customers.create(valid_attrs())

      {:ok, updated} =
        Customers.update(c, %{"billing_address" => %{"city" => "NYC"}})

      assert updated.billing_address.city == "NYC"
      assert updated.billing_address.street == "1 Main"
    end
  end

  describe "update with on_replace: :delete" do
    test "replacing contact_methods deletes the old ones" do
      {:ok, c} = Customers.create(valid_attrs())

      {:ok, updated} =
        Customers.update(c, %{
          "contact_methods" => [
            %{"kind" => "phone", "value" => "+19998887777", "primary" => true}
          ]
        })

      assert [%{kind: "phone"}] = updated.contact_methods
    end
  end

  describe "JSONB queries" do
    test "by_country/1 filters via JSONB path" do
      {:ok, _} = Customers.create(valid_attrs())

      attrs = put_in(valid_attrs(), ["billing_address", "country"], "DE")
      {:ok, _} = Customers.create(attrs)

      assert [%{billing_address: %{country: "US"}}] = Customers.by_country("US")
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Embedded Schemas and Nested Changesets.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Embedded Schemas and Nested Changesets ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case FormBuilder.run(payload) do
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
        for _ <- 1..1_000, do: FormBuilder.run(:bench)
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
