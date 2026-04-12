# Keyword Lists and Building a Small DSL

**Project**: `mini_query` — standalone Mix project, 1–2 hours  
**Difficulty**: ★★☆☆☆

---

## Project structure

```
mini_query/
├── lib/
│   └── mini_query.ex              # query builder DSL
├── test/
│   └── mini_query_test.exs        # ExUnit tests
└── mix.exs
```

---

## What you will learn

Two core concepts:

1. **Keyword lists** — lists of `{atom, value}` tuples with sugar syntax
   (`[select: :all, limit: 10]`). They allow **duplicate keys** (unlike maps)
   and **preserve order** — exactly the two properties a config/DSL needs.
2. **Opts patterns** — the idiomatic `fun(required_args, opts \\ [])` shape seen across
   the standard library (`GenServer.start_link/3`, `File.open/2`, `Ecto.Query.where/3`).
   `Keyword.get/3`, `Keyword.validate!/2`, and destructuring are the core toolkit.

---

## The business problem

You are building an internal reporting tool. Analysts write small queries against an
in-memory dataset without learning SQL. You want the call to read like English:

```elixir
MiniQuery.run(users,
  select: [:name, :age],
  where: [role: :admin],
  order_by: :age,
  limit: 5
)
```

It should return a list of maps with only the selected fields, filtered, sorted, and
capped. Because it's a DSL, the options must be validated — a typo like `selcet:` should
raise a helpful error, not silently return all fields.

---

## Why keyword lists and not maps

Maps look tempting (`%{select: ..., where: ...}`), but:

- Maps have no guaranteed order. `order_by` followed by `limit` is semantically different
  from `limit` followed by `order_by`. Keyword lists preserve source order.
- Map literal syntax (`%{k: v, k2: v2}`) is heavier than keyword sugar (`k: v, k2: v2`).
- The whole standard library's convention is keyword opts. Following it means your users
  don't need to learn a new dialect.

Keyword lists for **options**, maps for **data**. That's the community rule.

---

## Why `Keyword.validate!/2`

`Keyword.validate!/2` (Elixir 1.13+) catches typos at call time and documents the allowed
options in one place. Without it, `selcet: [:name]` silently uses the default for `:select`
and produces wrong output — a debugging nightmare for users of your DSL.

---

## Implementation

### Step 1 — Create the project

```bash
mix new mini_query
cd mini_query
```

Make sure `mix.exs` targets Elixir `~> 1.13` or later (the default for new projects is fine).

### Step 2 — `lib/mini_query.ex`

```elixir
defmodule MiniQuery do
  @moduledoc """
  A tiny in-memory query DSL built on keyword-list options.
  """

  @valid_opts [:select, :where, :order_by, :limit]

  @doc """
  Runs a query over a list of maps.

  ## Options

    * `:select`   — list of atom keys to keep (defaults to all keys).
    * `:where`    — keyword list of equality filters, all AND-ed together.
    * `:order_by` — atom key to sort ascending by. No sort if omitted.
    * `:limit`    — positive integer, max rows to return. No cap if omitted.

  Raises `ArgumentError` if an unknown option is given — fail fast on typos.
  """
  @spec run([map()], keyword()) :: [map()]
  def run(rows, opts \\ []) when is_list(rows) do
    # Validate first: any unknown key raises immediately.
    # Keyword.validate!/2 also documents the allowed set in one place.
    opts = Keyword.validate!(opts, Enum.map(@valid_opts, &{&1, nil}))

    rows
    |> apply_where(opts[:where])
    |> apply_order(opts[:order_by])
    |> apply_limit(opts[:limit])
    |> apply_select(opts[:select])
  end

  # ---- internal stages --------------------------------------------------------
  # Each stage is a no-op when its opt is nil — this keeps `run/2` a clean pipe
  # and avoids nested `case` ladders.

  defp apply_where(rows, nil), do: rows

  defp apply_where(rows, filters) when is_list(filters) do
    # All filters must match (AND). Enum.all?/2 short-circuits on first mismatch.
    Enum.filter(rows, fn row ->
      Enum.all?(filters, fn {k, v} -> Map.get(row, k) == v end)
    end)
  end

  defp apply_order(rows, nil), do: rows
  defp apply_order(rows, key) when is_atom(key), do: Enum.sort_by(rows, &Map.get(&1, key))

  defp apply_limit(rows, nil), do: rows
  defp apply_limit(rows, n) when is_integer(n) and n >= 0, do: Enum.take(rows, n)

  defp apply_select(rows, nil), do: rows

  defp apply_select(rows, keys) when is_list(keys) do
    # Map.take/2 silently drops missing keys — that matches SQL-like semantics
    # where selecting a non-existent column is an error caller's responsibility.
    Enum.map(rows, &Map.take(&1, keys))
  end
end
```

### Step 3 — `test/mini_query_test.exs`

```elixir
defmodule MiniQueryTest do
  use ExUnit.Case, async: true

  @users [
    %{name: "Ada", age: 36, role: :admin},
    %{name: "Bo", age: 24, role: :user},
    %{name: "Cy", age: 29, role: :admin},
    %{name: "Di", age: 41, role: :user}
  ]

  test "returns all rows with no opts" do
    assert MiniQuery.run(@users) == @users
  end

  test "select keeps only requested fields" do
    assert MiniQuery.run(@users, select: [:name]) == [
             %{name: "Ada"},
             %{name: "Bo"},
             %{name: "Cy"},
             %{name: "Di"}
           ]
  end

  test "where filters by equality (AND)" do
    result = MiniQuery.run(@users, where: [role: :admin])
    assert length(result) == 2
    assert Enum.all?(result, &(&1.role == :admin))
  end

  test "order_by sorts ascending" do
    names = MiniQuery.run(@users, order_by: :age) |> Enum.map(& &1.name)
    assert names == ["Bo", "Cy", "Ada", "Di"]
  end

  test "limit caps the result set" do
    assert MiniQuery.run(@users, limit: 2) |> length() == 2
  end

  test "composes all options" do
    result =
      MiniQuery.run(@users,
        where: [role: :admin],
        order_by: :age,
        limit: 1,
        select: [:name]
      )

    assert result == [%{name: "Cy"}]
  end

  test "raises on unknown option (typo protection)" do
    assert_raise ArgumentError, fn ->
      MiniQuery.run(@users, selcet: [:name])
    end
  end
end
```

### Step 4 — Run the tests

```bash
mix test
```

All 7 tests should pass.

---

## Trade-offs

| Shape | When to use | Downside |
|-------|-------------|----------|
| `fun(arg, opts \\ [])` | Public APIs with optional config | Need `Keyword.validate!/2` to catch typos |
| `fun(arg, %Opts{})` struct | Many options, strong types, internal API | Callers must build the struct explicitly |
| Positional args `fun(a, b, c, d)` | 2–3 required args | Unreadable past 3 args, "boolean trap" |
| Map opts `%{select: ...}` | When passing through JSON/RPC boundary | Loses order, verbose literal syntax |

---

## Common production mistakes

**1. `Keyword.get/3` with the wrong default type**  
`Keyword.get(opts, :limit, "10")` returns a string if `:limit` is missing but an integer if
set — a polymorphic return that breaks downstream code. Keep defaults in the same type
as valid values.

**2. Forgetting that keyword lists allow duplicates**  
`[a: 1, a: 2]` is a valid keyword list. `Keyword.get/2` returns the **first** match, silently
hiding the second. If callers pass duplicates, use `Keyword.get_values/2` or normalize with
`Enum.uniq_by/2`.

**3. Mixing atom and string keys**  
A keyword list REQUIRES atom keys — it's part of the definition. `[{"limit", 10}]` is not
a keyword list, it's a plain list of tuples. `Keyword.*` functions will raise. Validate at
the boundary if the input comes from JSON.

**4. Not validating opts**  
Without `Keyword.validate!/2`, a typo silently falls through to the default. Users blame
your code for "not respecting the option". Always validate opts in a public DSL.

---

## When NOT to use keyword lists

- For data that will live in a long-lived struct — use a `%__MODULE__{}` with named fields.
- When you need O(1) lookup by key — keyword is O(n). Convert to a map first.
- When the collection is dynamic-size user data — that's `Map` or `MapSet` territory.

---

## Resources

- [`Keyword` module docs](https://hexdocs.pm/elixir/Keyword.html)
- [`Keyword.validate!/2`](https://hexdocs.pm/elixir/Keyword.html#validate!/2)
- José Valim — ["The case against proplists"](https://elixir-lang.org/getting-started/keywords-and-maps.html)
- `Ecto.Query` source — a production-grade DSL built on keyword lists
