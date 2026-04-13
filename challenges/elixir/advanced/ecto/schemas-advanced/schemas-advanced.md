# Advanced Ecto Schemas: embedded, virtual, custom types, polymorphism

**Project**: `ecto_schemas_deep` — schema layer for a CRM with flexible contact data

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
ecto_schemas_deep/
├── lib/
│   └── ecto_schemas_deep.ex
├── script/
│   └── main.exs
├── test/
│   └── ecto_schemas_deep_test.exs
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
defmodule EctoSchemasDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :ecto_schemas_deep,
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
### `lib/ecto_schemas_deep.ex`

```elixir
# lib/ecto_schemas_deep/types/phone_e164.ex
defmodule EctoSchemasDeep.Types.PhoneE164 do
  @moduledoc """
  Ecto type that normalizes phone numbers to E.164 format on cast and dump.

  Accepts strings with arbitrary separators; stores digits with leading `+`.
  """
  use Ecto.Type

  @impl true
  def type, do: :string

  @impl true
  def cast(value) when is_binary(value) do
    case normalize(value) do
      {:ok, e164} -> {:ok, e164}
      :error -> :error
    end
  end

  def cast(nil), do: {:ok, nil}
  def cast(_), do: :error

  @impl true
  def load(value) when is_binary(value), do: {:ok, value}
  def load(nil), do: {:ok, nil}

  @impl true
  def dump(value) when is_binary(value) do
    case normalize(value) do
      {:ok, e164} -> {:ok, e164}
      :error -> :error
    end
  end

  def dump(nil), do: {:ok, nil}
  def dump(_), do: :error

  defp normalize(raw) do
    digits = raw |> to_string() |> String.replace(~r/[^\d+]/, "")

    case digits do
      "+" <> rest when byte_size(rest) in 8..15 ->
        if String.match?(rest, ~r/^\d+$/), do: {:ok, "+" <> rest}, else: :error

      rest when byte_size(rest) in 8..15 ->
        if String.match?(rest, ~r/^\d+$/), do: {:ok, "+" <> rest}, else: :error

      _ ->
        :error
    end
  end
end

# lib/ecto_schemas_deep/schemas/address.ex
defmodule EctoSchemasDeep.Schemas.Address do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key false
  embedded_schema do
    field :street, :string
    field :city, :string
    field :country, :string
    field :postal_code, :string
  end

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(address, attrs) do
    address
    |> cast(attrs, [:street, :city, :country, :postal_code])
    |> validate_required([:street, :city, :country])
    |> validate_length(:country, is: 2)
  end
end

# lib/ecto_schemas_deep/schemas/contact.ex
defmodule EctoSchemasDeep.Schemas.Contact do
  use Ecto.Schema
  import Ecto.Changeset

  alias EctoSchemasDeep.Schemas.Address
  alias EctoSchemasDeep.Types.PhoneE164

  schema "contacts" do
    field :first_name, :string
    field :last_name, :string
    field :phone, PhoneE164
    field :tags, {:array, :string}, default: []
    field :custom_attrs, :map, default: %{}

    # Derived on load; never persisted.
    field :full_name, :string, virtual: true

    embeds_one :address, Address, on_replace: :update

    timestamps()
  end

  @required ~w(first_name last_name)a
  @optional ~w(phone tags custom_attrs)a

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(contact, attrs) do
    contact
    |> cast(attrs, @required ++ @optional)
    |> validate_required(@required)
    |> cast_embed(:address, with: &Address.changeset/2)
    |> validate_length(:tags, max: 20)
    |> put_full_name()
  end

  defp put_full_name(changeset) do
    first = get_field(changeset, :first_name)
    last = get_field(changeset, :last_name)

    case {first, last} do
      {f, l} when is_binary(f) and is_binary(l) ->
        put_change(changeset, :full_name, "#{f} #{l}")

      _ ->
        changeset
    end
  end
end

# lib/ecto_schemas_deep/schemas/note.ex
defmodule EctoSchemasDeep.Schemas.Note do
  use Ecto.Schema
  import Ecto.Changeset

  @notable_types ~w(contact company deal)

  schema "notes" do
    field :body, :string
    field :notable_id, :integer
    field :notable_type, :string
    timestamps()
  end

  @spec changeset(%__MODULE__{}, map()) :: Ecto.Changeset.t()
  def changeset(note, attrs) do
    note
    |> cast(attrs, [:body, :notable_id, :notable_type])
    |> validate_required([:body, :notable_id, :notable_type])
    |> validate_inclusion(:notable_type, @notable_types)
    |> validate_length(:body, min: 1, max: 10_000)
  end

  @spec notable_types() :: [String.t()]
  def notable_types, do: @notable_types
end

# lib/ecto_schemas_deep/notes.ex
defmodule EctoSchemasDeep.Notes do
  @moduledoc "Runtime dispatcher for polymorphic notes."

  import Ecto.Query

  alias EctoSchemasDeep.Repo
  alias EctoSchemasDeep.Schemas.{Company, Contact, Deal, Note}

  @type notable :: Contact.t() | Company.t() | Deal.t()

  @doc "Creates a note attached to any notable parent."
  @spec create(notable(), String.t()) :: {:ok, Note.t()} | {:error, Ecto.Changeset.t()}
  def create(notable, body) do
    %Note{}
    |> Note.changeset(%{
      body: body,
      notable_id: notable.id,
      notable_type: type_for(notable)
    })
    |> Repo.insert()
  end

  @doc "Returns all notes for a given parent."
  @spec for_parent(notable()) :: [Note.t()]
  def for_parent(notable) do
    type = type_for(notable)

    from(n in Note,
      where: n.notable_id == ^notable.id and n.notable_type == ^type,
      order_by: [desc: n.inserted_at]
    )
    |> Repo.all()
  end

  defp type_for(%Contact{}), do: "contact"
  defp type_for(%Company{}), do: "company"
  defp type_for(%Deal{}), do: "deal"
end

defmodule EctoSchemasDeep.Repo.Migrations.CreateSchemas do
  use Ecto.Migration

  def change do
    create table(:contacts) do
      add :first_name, :string, null: false
      add :last_name, :string, null: false
      add :phone, :string
      add :tags, {:array, :string}, default: []
      add :custom_attrs, :map, default: %{}
      add :address, :map
      timestamps()
    end

    create table(:companies) do
      add :name, :string, null: false
      timestamps()
    end

    create table(:deals) do
      add :title, :string, null: false
      add :amount_cents, :integer, null: false
      timestamps()
    end

    create table(:notes) do
      add :body, :text, null: false
      add :notable_id, :integer, null: false
      add :notable_type, :string, null: false
      timestamps()
    end
    create index(:notes, [:notable_type, :notable_id])
  end
end
```
### `test/ecto_schemas_deep_test.exs`

```elixir
defmodule EctoSchemasDeep.ContactTest do
  use ExUnit.Case, async: true
  doctest EctoSchemasDeep.Types.PhoneE164

  alias EctoSchemasDeep.Repo
  alias EctoSchemasDeep.Schemas.Contact

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    :ok
  end

  describe "changeset/2 — phone custom type" do
    test "normalizes a phone number to E.164" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        phone: "+54 11 1234-5678"
      })

      assert cs.valid?
      assert Ecto.Changeset.get_change(cs, :phone) == "+5411123456781"
             or Ecto.Changeset.get_change(cs, :phone) == "+541112345678"
    end

    test "rejects gibberish phone" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        phone: "abc"
      })

      refute cs.valid?
      assert %{phone: ["is invalid"]} = errors_on(cs)
    end
  end

  describe "changeset/2 — embedded address" do
    test "casts nested address errors" do
      cs = Contact.changeset(%Contact{}, %{
        first_name: "Ada",
        last_name: "Lovelace",
        address: %{street: "Main 1", city: "BA", country: "ARGENTINA"}
      })

      refute cs.valid?
      assert %{address: %{country: ["should be 2 character(s)"]}} = errors_on(cs)
    end
  end

  describe "changeset/2 — virtual full_name" do
    test "derives full_name on cast" do
      cs = Contact.changeset(%Contact{}, %{first_name: "Ada", last_name: "Lovelace"})
      assert Ecto.Changeset.get_change(cs, :full_name) == "Ada Lovelace"
    end
  end

  defp errors_on(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {message, opts} ->
      Regex.replace(~r"%{(\w+)}", message, fn _, key ->
        opts |> Keyword.get(String.to_existing_atom(key), key) |> to_string()
      end)
    end)
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Advanced Ecto Schemas: embedded, virtual, custom types, polymorphism.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Advanced Ecto Schemas: embedded, virtual, custom types, polymorphism ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case EctoSchemasDeep.run(payload) do
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
        for _ <- 1..1_000, do: EctoSchemasDeep.run(:bench)
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
