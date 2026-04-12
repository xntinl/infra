# CTEs and Recursive Queries

**Project**: `org_chart` — traversing arbitrarily deep hierarchies with Postgres recursive CTEs.

---

## Project context

An HR product displays the reporting chain for any employee: from them upward to the CEO,
and downward to all direct and indirect reports. The `employees` table has a self-referential
`manager_id`. The depth is unbounded (in practice 3–7 levels, but the algorithm must handle
more).

Computing transitive closure in the app layer is N round-trips. Postgres recursive CTEs
resolve the entire ancestry or descendancy in one query, with cycle detection.

```
org_chart/
├── lib/
│   └── org_chart/
│       ├── application.ex
│       ├── repo.ex
│       ├── org.ex
│       └── schemas/
│           └── employee.ex
├── priv/repo/migrations/
├── test/org_chart/
│   └── org_test.exs
├── bench/org_bench.exs
└── mix.exs
```

---

## Why recursive CTEs and not adjacency loops

```elixir
# Loop in Elixir — N queries for depth N
def ancestry(emp_id) do
  Stream.unfold(emp_id, fn
    nil -> nil
    id ->
      case Repo.get(Employee, id) do
        nil -> nil
        emp -> {emp, emp.manager_id}
      end
  end)
  |> Enum.to_list()
end
```

For depth 7, this is 7 sequential queries. Latency is additive; at 5 ms per query, 35 ms
per call. Running on every page load is untenable.

A recursive CTE returns the whole chain in one query. Postgres handles the recursion
internally; the planner can apply index-only scans and hash aggregation.

Alternatives to recursive CTE:

- **Materialized path** (`path = "/1/5/42"`) — fast reads, painful moves.
- **Nested set (Celko)** — fast `IS_ANCESTOR` queries, rebalancing on insert is O(N).
- **Closure table** — precomputed ancestor rows; fast reads, 2× storage, expensive writes.

For small-to-medium hierarchies with frequent writes, recursive CTE over an adjacency list
is the lightest.

---

## Core concepts

### 1. `WITH RECURSIVE` structure

```sql
WITH RECURSIVE chain AS (
  -- anchor: starting row
  SELECT id, manager_id, name, 0 AS depth
  FROM employees
  WHERE id = 42

  UNION ALL

  -- recursive term: one step up per iteration
  SELECT e.id, e.manager_id, e.name, c.depth + 1
  FROM employees e
  JOIN chain c ON e.id = c.manager_id
  WHERE c.depth < 100                        -- cycle/depth guard
)
SELECT * FROM chain ORDER BY depth;
```

- Anchor produces seed rows.
- Recursive term references the CTE name; runs until no new rows.
- `UNION ALL` is required (no dedup across iterations — faster and often wanted).
- `UNION` (without ALL) implicitly dedupes, which serves as a cycle guard at a cost.

### 2. Ecto's `recursive_ctes/2` and `with_cte/3`

```elixir
initial_query = from(e in Employee, where: e.id == ^id, select: %{...})
recursion_query = from(e in Employee, join: c in "chain", on: e.id == c.manager_id, select: %{...})

chain_query = union_all(initial_query, ^recursion_query)

from(c in "chain", ...)
|> recursive_ctes(true)
|> with_cte("chain", as: ^chain_query)
```

The CTE body is a combined query built with `union_all/2`. Ecto compiles this to the
`WITH RECURSIVE chain AS (...)` prefix.

### 3. Cycle protection

A malformed graph (A → B → A) makes the recursion infinite. Two defenses:

- **Depth guard**: `WHERE depth < 100` in the recursive term.
- **Path tracking**: carry a visited-set as an array column, stop when the candidate is
  already in it.

We use both — depth for speed, path for correctness on truly cyclic data.

---

## Design decisions

- **Option A — recursive CTE with UNION ALL + depth guard**: fast, bounded.
  Pros: small code, good planner behavior. Cons: needs `recursive_ctes(true)` explicit.
- **Option B — closure table**: precompute every (ancestor, descendant) pair.
  Pros: O(1) reads. Cons: O(N²) storage, writes touch ancestry rows transactionally.

We use **Option A**. Closure tables pay off past ~1M rows or when reads dominate 1000:1.

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

### Step 1: Schema and migration

```elixir
# lib/org_chart/schemas/employee.ex
defmodule OrgChart.Schemas.Employee do
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
```

```elixir
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
```

### Step 2: Recursive ancestry (up the chain)

```elixir
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

---

## Why this works

- One round-trip instead of N: recursion happens entirely inside Postgres.
- `recursive_ctes(true)` enables the `WITH RECURSIVE` keyword — a required flag in Ecto.
- The CTE `"ancestry"` is referenced by name in the recursive arm via `join: c in "ancestry"`;
  Ecto treats it as a virtual table.
- The `select:` maps (not structs) are used because the CTE result merges rows from two
  distinct selects with a synthetic `depth`. Using `select: e` (the full schema) would
  require the recursion to produce matching schema fields, including virtual `:depth`.

---

## Data flow — `ancestry(7)`

```
WITH RECURSIVE ancestry AS (
    anchor:    SELECT id=7, manager_id=5, name="Ada", depth=0
UNION ALL
    step 1:    SELECT id=5, manager_id=3, name="Bob", depth=1
    step 2:    SELECT id=3, manager_id=1, name="Cara", depth=2
    step 3:    SELECT id=1, manager_id=NULL, name="CEO", depth=3
    (manager_id=NULL joins to zero rows — recursion terminates)
)
SELECT * FROM ancestry ORDER BY depth
```

Postgres materializes `ancestry` iteratively, joining step N against rows from step N-1.

---

## Tests

```elixir
# test/org_chart/org_test.exs
defmodule OrgChart.OrgTest do
  use ExUnit.Case, async: false
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

---

## Benchmark

```elixir
# bench/org_bench.exs
alias OrgChart.{Org, Repo}
alias OrgChart.Schemas.Employee

Repo.delete_all(Employee)
{:ok, root} = Repo.insert(%Employee{name: "root"})
parent_id = root.id

{_last_id, _} =
  Enum.reduce(1..7, {parent_id, nil}, fn depth, {pid, _} ->
    {:ok, child} = Repo.insert(%Employee{name: "d#{depth}", manager_id: pid})
    {child.id, nil}
  end)

# Add 500 random leaf employees under root to stress descendants
for n <- 1..500 do
  Repo.insert!(%Employee{name: "leaf-#{n}", manager_id: root.id})
end

deepest = Repo.one(from e in Employee, order_by: [desc: e.id], limit: 1)

Benchee.run(
  %{
    "ancestry depth 7"       => fn -> Org.ancestry(deepest.id) end,
    "descendants 500 leaves" => fn -> Org.descendants(root.id) end
  },
  time: 3, warmup: 1
)
```

**Target**: `ancestry depth 7` under 2 ms, `descendants 500 leaves` under 5 ms. If slower,
verify the `manager_id` index is being used (`EXPLAIN ANALYZE`).

---

## Trade-offs and production gotchas

**1. `recursive_ctes(true)` must be set explicitly.** Ecto defaults to non-recursive CTEs;
forgetting this flag produces a SQL error about self-reference.

**2. `UNION ALL` vs `UNION`.** `UNION ALL` is faster and does not dedupe — fine when the
graph is a tree. For DAGs with shared ancestors (rare in org charts, common in BOM
systems), `UNION` provides implicit dedup at a cost.

**3. Infinite recursion on cyclic data.** The depth guard is essential. A production-grade
version also tracks visited IDs in an array column and checks membership with `NOT (id = ANY(path))`.

**4. Large descendancies blow up memory.** Querying `descendants(CEO)` returns every
employee. Always page or limit. For bulk analytics, use a materialized view refreshed
nightly.

**5. Recursive CTE planning is opaque.** `EXPLAIN ANALYZE` shows the iterative plan, but
the per-iteration cost is estimated, not measured. Benchmark against real data sizes.

**6. When NOT to use recursive CTEs.** If you need transitive closure for every read and
writes are rare, a closure table is 10× faster on read. If your hierarchy is bounded
(max 3 levels), three explicit LEFT JOINs are simpler and faster.

---

## Reflection

Your org chart has 50k employees, max depth 7. `descendants(CEO)` takes 80 ms — acceptable
for admin pages, too slow for a team roster page hit 1000 req/s. What precomputation
structure do you introduce (materialized view? closure table? denormalized `depth` and
`path` on each row?) and what is the write amplification when the VP Eng moves to the
VP Sales org — how many rows change?

---

## Resources

- [Postgres — WITH queries (CTEs)](https://www.postgresql.org/docs/current/queries-with.html)
- [`Ecto.Query.recursive_ctes/2`](https://hexdocs.pm/ecto/Ecto.Query.html#recursive_ctes/2)
- [`Ecto.Query.with_cte/3`](https://hexdocs.pm/ecto/Ecto.Query.html#with_cte/3)
- [Joe Celko — "Trees and Hierarchies in SQL for Smarties"](https://www.elsevier.com/books/joe-celkos-trees-and-hierarchies-in-sql-for-smarties/celko/978-0-12-387733-8)
