# Pattern queries with `:ets.match/2` and `:ets.match_object/2`

**Project**: `match_object_demo` — query ETS by **shape** using match
patterns, wildcards `:_`, and bindings `:"$1"`/`:"$2"`, without reaching for
a full match spec.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Before match specs, there were match **patterns**. `:ets.match/2` and
`:ets.match_object/2` let you query "every tuple whose second field is
`:admin`" with a pattern that looks like ordinary Elixir tuple matching,
plus two symbols: `:_` (wildcard, discarded) and `:"$1"`, `:"$2"`, …
(binding positions, captured and returned).

This is the 80%-case API: simpler than match specs, more restricted,
but enough for most "filter by field shape" queries. In real code you'll
see all three — `match/2`, `match_object/2`, and `select/2` — and it's
worth knowing exactly what each returns.

Project structure:

```
match_object_demo/
├── lib/
│   └── match_object_demo.ex
├── test/
│   └── match_object_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `match/2` vs `match_object/2` — same pattern, different return

Given a table of `{id, name, role}` tuples and the pattern
`{:"$1", :"$2", :admin}`:

- `:ets.match/2` returns a list of **lists of bindings**:
  `[[1, "Alice"], [3, "Carol"]]` — just the `:"$N"` values, in order.
- `:ets.match_object/2` returns the **full matching tuples**:
  `[{1, "Alice", :admin}, {3, "Carol", :admin}]`.

Choose `match/2` when you only want a couple of fields (saves the copy of
the rest); choose `match_object/2` when you want the whole record.

### 2. Wildcard `:_`

`:_` matches anything in that position and is **not returned**. If you don't
care about the third column, use `:_`, not `:"$3"` — it makes the intent
clearer and lets the engine skip capturing it.

### 3. Repeated binding = equality constraint

`{:"$1", :"$1", :_}` matches only tuples where field 1 equals field 2.
This is the pattern-level equivalent of a guard. It's rarely useful on
randomly shaped data but occasionally elegant for "self-referencing" rows.

### 4. Match patterns can't do guards

You can't write "`:"$1" > 10`" in a match pattern. The moment you need a
comparison, you graduate to `:ets.select/2` with a full match spec
(exercise 98). Match patterns = equality and shape only.

### 5. When the first element is bound, there's no key-index optimization

`:ets.match/2` with a pattern that pins the key (`{123, :"$1", :"$2"}` on
a key-position-1 table) is essentially `lookup/2` with a reshape — OTP
doesn't do anything smarter. If the key is known, **use `lookup/2`**; it's
clearer and exactly as fast.

---

## Implementation

### Step 1: Create the project

```bash
mix new match_object_demo
cd match_object_demo
```

### Step 2: `lib/match_object_demo.ex`

```elixir
defmodule MatchObjectDemo do
  @moduledoc """
  A small "users" table storing `{user_id, name, role}` tuples, queried with
  `:ets.match/2` and `:ets.match_object/2` to illustrate pattern-based queries.

  All the query functions accept pre-built patterns so tests can see how the
  pattern shape maps to results.
  """

  @type user :: {integer(), String.t(), atom()}

  @doc "Creates the users table and seeds it with fixtures."
  @spec seed() :: :ets.tid()
  def seed do
    t = :ets.new(:users, [:set, :public])

    :ets.insert(t, [
      {1, "Alice", :admin},
      {2, "Bob", :user},
      {3, "Carol", :admin},
      {4, "Dan", :user},
      {5, "Eve", :guest}
    ])

    t
  end

  @doc """
  Returns only the requested bindings, using `:ets.match/2`.

  Example: `names_by_role(t, :admin)` uses the pattern `{:_, :"$1", :admin}`
  and returns `[["Alice"], ["Carol"]]` — a list-of-lists, one binding per
  `$N` position in the pattern (just `$1` = name here).
  """
  @spec names_by_role(:ets.tid(), atom()) :: [[String.t()]]
  def names_by_role(t, role) do
    # Pattern: id is wildcarded (we don't want it), name is captured as $1,
    # role must literally equal the given atom.
    :ets.match(t, {:_, :"$1", role})
  end

  @doc """
  Returns full tuples for users with the given role, via `:ets.match_object/2`.

  The pattern is exactly the same shape, but we get the whole record back
  instead of just bound positions.
  """
  @spec users_by_role(:ets.tid(), atom()) :: [user()]
  def users_by_role(t, role) do
    :ets.match_object(t, {:_, :_, role})
  end

  @doc """
  Returns `{id, name}` pairs for admins, via `match/2` with two bindings.
  Demonstrates multi-binding extraction without the cost of copying the role.
  """
  @spec admin_id_and_name(:ets.tid()) :: [[term()]]
  def admin_id_and_name(t) do
    # $1 = id, $2 = name, role = literal :admin.
    :ets.match(t, {:"$1", :"$2", :admin})
  end
end
```

### Step 3: `test/match_object_demo_test.exs`

```elixir
defmodule MatchObjectDemoTest do
  use ExUnit.Case, async: true

  setup do
    t = MatchObjectDemo.seed()
    on_exit(fn -> if :ets.info(t) != :undefined, do: :ets.delete(t) end)
    %{t: t}
  end

  describe "names_by_role/2 — `match/2` returns bindings only" do
    test "extracts only the names for admins", %{t: t} do
      result = MatchObjectDemo.names_by_role(t, :admin) |> Enum.sort()
      assert result == [["Alice"], ["Carol"]]
    end

    test "returns [] when no rows match", %{t: t} do
      assert MatchObjectDemo.names_by_role(t, :nonexistent) == []
    end
  end

  describe "users_by_role/2 — `match_object/2` returns full tuples" do
    test "returns every field of every matching row", %{t: t} do
      result = MatchObjectDemo.users_by_role(t, :user) |> Enum.sort()
      assert result == [{2, "Bob", :user}, {4, "Dan", :user}]
    end

    test "single-row match", %{t: t} do
      assert MatchObjectDemo.users_by_role(t, :guest) == [{5, "Eve", :guest}]
    end
  end

  describe "admin_id_and_name/1 — multi-binding `match/2`" do
    test "returns [[id, name], ...] in the order of $1, $2 in the pattern", %{t: t} do
      result = MatchObjectDemo.admin_id_and_name(t) |> Enum.sort()
      assert result == [[1, "Alice"], [3, "Carol"]]
    end
  end

  describe "pattern semantics" do
    test ":_ matches anything and is discarded", %{t: t} do
      # `{:_, :_, :_}` matches every row. match_object returns full tuples.
      all = :ets.match_object(t, {:_, :_, :_})
      assert length(all) == 5
    end

    test "repeated $N enforces equality between positions" do
      # Build a small table where some rows have id == role-ish match.
      t = :ets.new(:twin, [:set, :public])
      :ets.insert(t, [{1, 1, :x}, {2, 3, :y}, {4, 4, :z}])

      # Pattern: first and second field must be equal.
      result = :ets.match_object(t, {:"$1", :"$1", :_}) |> Enum.sort()
      assert result == [{1, 1, :x}, {4, 4, :z}]

      :ets.delete(t)
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. `match/2` is a full scan unless the key is bound**
On a `:set`, if you pin the key in the pattern (`{42, :"$1", :_}`) you get
one-row efficiency — but at that point, `lookup/2` is clearer. If the key
is `:_` or `:"$1"`, the engine scans the whole table. Know your table size.

**2. `match/2` cannot express comparisons**
`>`, `<`, `!=` are not available in match patterns. Hitting this limit is
the signal that it's time for `:ets.select/2` with a match spec
(exercise 98) or `fun2ms`.

**3. Return-shape trap: `match/2` returns a list-of-lists**
Every time. Even if your pattern has one binding, you get `[[v1], [v2], ...]`,
not `[v1, v2, ...]`. Flatten it or use `match_object/2` if that shape is
annoying — the cost of returning whole tuples is often tolerable.

**4. Large results copy a lot of memory**
Every returned tuple / binding is copied from ETS into the caller's heap.
A match that returns 100k rows is 100k term copies. Use `:ets.match/3`
(with continuation) to stream, or add a limit to the caller's logic.

**5. `match_object_delete/2` exists**
For bulk deletes by pattern, `:ets.match_delete/2` deletes every tuple
matching the pattern in one shot. Much faster than `match_object/2` + a
loop of `delete/2` — and atomic from the table's point of view.

**6. When NOT to use match patterns**
- When the key is known: use `lookup/2`.
- When you need comparisons or complex logic: use `select/2` + match spec.
- When you want an ergonomic API: use `fun2ms` (exercise 98) and get a
  match spec generated from a fun.

---

## Resources

- [`:ets.match/2`](https://www.erlang.org/doc/man/ets.html#match-2)
- [`:ets.match_object/2`](https://www.erlang.org/doc/man/ets.html#match_object-2)
- [`:ets.match_delete/2`](https://www.erlang.org/doc/man/ets.html#match_delete-2)
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets) — match patterns walkthrough
- [Erlang match spec reference](https://www.erlang.org/doc/apps/erts/match_spec.html) — for when patterns aren't enough
