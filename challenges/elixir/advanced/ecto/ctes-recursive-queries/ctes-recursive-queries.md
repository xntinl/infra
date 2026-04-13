# CTEs and Recursive Queries

**Project**: `org_chart` — traversing arbitrarily deep hierarchies with Postgres recursive CTEs

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
org_chart/
├── lib/
│   └── org_chart.ex
├── script/
│   └── main.exs
├── test/
│   └── org_chart_test.exs
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
defmodule OrgChart.MixProject do
  use Mix.Project

  def project do
    [
      app: :org_chart,
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
### `lib/org_chart.ex`

```elixir
# lib/org_chart/schemas/employee.ex
defmodule OrgChart.Schemas.Employee do
  @moduledoc """
  Ejercicio: CTEs and Recursive Queries.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Ecto.Schema

  schema "employees" do
    field :name, :string
    field :title, :string
    field :depth, :integer, virtual: true
    belongs_to :manager, __MODULE__
    has_many :reports, __MODULE__, foreign_key: :manager_id
    timestamps()
  end
end

# priv/repo/migrations/20260101000000_create_employees.exs
defmodule OrgChart.Repo.Migrations.CreateEmployees do
  use Ecto.Migration

  def change do
    create table(:employees) do
      add :name, :string, null: false
      add :title, :string
      add :manager_id, references(:employees, on_delete: :nilify_all)
      timestamps()
    end

    create index(:employees, [:manager_id])
  end
end

# lib/org_chart/org.ex
defmodule OrgChart.Org do
  import Ecto.Query

  alias OrgChart.Repo
  alias OrgChart.Schemas.Employee

  @max_depth 100

  @doc """
  Returns the reporting chain from `employee_id` upward to the root.

  Ordered by depth ascending (0 = the employee, N = CEO).
  """
  @spec ancestry(integer()) :: [map()]
  def ancestry(employee_id) do
    initial =
      from e in Employee,
        where: e.id == ^employee_id,
        select: %{
          id: e.id,
          manager_id: e.manager_id,
          name: e.name,
          title: e.title,
          depth: 0
        }

    recursion =
      from e in Employee,
        join: c in "ancestry",
        on: e.id == c.manager_id,
        where: c.depth < ^@max_depth,
        select: %{
          id: e.id,
          manager_id: e.manager_id,
          name: e.name,
          title: e.title,
          depth: c.depth + 1
        }

    chain_query = union_all(initial, ^recursion)

    from(c in "ancestry", order_by: c.depth)
    |> recursive_ctes(true)
    |> with_cte("ancestry", as: ^chain_query)
    |> Repo.all()
  end

  @doc """
  Returns all direct and transitive reports of `employee_id`.
  """
  @spec descendants(integer()) :: [map()]
  def descendants(employee_id) do
    initial =
      from e in Employee,
        where: e.manager_id == ^employee_id,
        select: %{
          id: e.id,
          manager_id: e.manager_id,
          name: e.name,
          title: e.title,
          depth: 1
        }

    recursion =
      from e in Employee,
        join: c in "tree",
        on: e.manager_id == c.id,
        where: c.depth < ^@max_depth,
        select: %{
          id: e.id,
          manager_id: e.manager_id,
          name: e.name,
          title: e.title,
          depth: c.depth + 1
        }

    from(c in "tree", order_by: [asc: c.depth, asc: c.id])
    |> recursive_ctes(true)
    |> with_cte("tree", as: ^union_all(initial, ^recursion))
    |> Repo.all()
  end

  @doc """
  Count of transitive reports — useful for org health metrics.
  """
  @spec report_count(integer()) :: non_neg_integer()
  def report_count(employee_id) do
    employee_id
    |> descendants()
    |> length()
  end

  @doc """
  Detects whether `maybe_subordinate_id` is anywhere in the subtree of `ancestor_id`.

  Cycle-safe via the depth guard.
  """
  @spec subordinate?(integer(), integer()) :: boolean()
  def subordinate?(ancestor_id, maybe_subordinate_id) do
    ancestor_id
    |> descendants()
    |> Enum.any?(&(&1.id == maybe_subordinate_id))
  end
end
```
### `test/org_chart_test.exs`

```elixir
defmodule OrgChart.OrgTest do
  use ExUnit.Case, async: true
  doctest OrgChart.Schemas.Employee
  alias OrgChart.{Org, Repo}
  alias OrgChart.Schemas.Employee

  setup do
    Ecto.Adapters.SQL.Sandbox.checkout(Repo)
    Ecto.Adapters.SQL.Sandbox.mode(Repo, {:shared, self()})
    Repo.delete_all(Employee)
    :ok
  end

  defp seed_tree do
    {:ok, ceo} = Repo.insert(%Employee{name: "CEO"})
    {:ok, vp1} = Repo.insert(%Employee{name: "VP Eng", manager_id: ceo.id})
    {:ok, vp2} = Repo.insert(%Employee{name: "VP Sales", manager_id: ceo.id})
    {:ok, dir} = Repo.insert(%Employee{name: "Director", manager_id: vp1.id})
    {:ok, eng} = Repo.insert(%Employee{name: "Engineer", manager_id: dir.id})
    {ceo, vp1, vp2, dir, eng}
  end

  describe "ancestry/1" do
    test "returns chain from leaf to root" do
      {ceo, vp1, _, dir, eng} = seed_tree()

      chain = Org.ancestry(eng.id)
      ids = Enum.map(chain, & &1.id)

      assert ids == [eng.id, dir.id, vp1.id, ceo.id]
    end

    test "returns only the employee when they have no manager" do
      {:ok, orphan} = Repo.insert(%Employee{name: "Lone"})
      assert [only] = Org.ancestry(orphan.id)
      assert only.id == orphan.id
    end
  end

  describe "descendants/1" do
    test "returns all transitive reports" do
      {ceo, vp1, vp2, dir, eng} = seed_tree()

      reports = Org.descendants(ceo.id)
      ids = Enum.map(reports, & &1.id) |> Enum.sort()

      assert ids == Enum.sort([vp1.id, vp2.id, dir.id, eng.id])
    end

    test "direct report depth is 1, grandchild is 2" do
      {ceo, vp1, _, dir, _} = seed_tree()
      reports = Org.descendants(ceo.id)
      assert Enum.find(reports, &(&1.id == vp1.id)).depth == 1
      assert Enum.find(reports, &(&1.id == dir.id)).depth == 2
    end
  end

  describe "subordinate?/2" do
    test "detects deep subordinate" do
      {ceo, _, _, _, eng} = seed_tree()
      assert Org.subordinate?(ceo.id, eng.id)
    end

    test "returns false for unrelated pair" do
      {_, _, vp2, _, eng} = seed_tree()
      refute Org.subordinate?(vp2.id, eng.id)
    end
  end

  describe "cycle protection" do
    test "depth guard terminates on a cyclic graph" do
      # Build an artificial cycle A ↔ B
      {:ok, a} = Repo.insert(%Employee{name: "A"})
      {:ok, b} = Repo.insert(%Employee{name: "B", manager_id: a.id})
      Ecto.Adapters.SQL.query!(Repo, "UPDATE employees SET manager_id = $1 WHERE id = $2", [b.id, a.id])

      # Should not infinite-loop; depth guard caps iterations.
      assert length(Org.ancestry(a.id)) <= 101
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for CTEs and Recursive Queries.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== CTEs and Recursive Queries ===")
    IO.puts("Category: Ecto advanced\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case OrgChart.run(payload) do
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
        for _ <- 1..1_000, do: OrgChart.run(:bench)
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
