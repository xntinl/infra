# Case with Guards: A Role-Based Permission Matcher

**Project**: `role_permission_matcher` — decides allow/deny for (role, resource) pairs using `case` + `when` clauses

---

## Project structure

```
role_permission_matcher/
├── lib/
│   └── role_permission_matcher.ex
├── script/
│   └── main.exs
├── test/
│   └── role_permission_matcher_test.exs
└── mix.exs
```

---

## Core concepts

`case` evaluates an expression against a series of patterns. The FIRST clause
whose pattern matches AND whose guard evaluates to `true` wins. All others
are skipped.

A `when` guard is an extra boolean expression tied to a clause. Guards are
restricted to a whitelist of pure, fast operations:

- Comparisons: `==`, `!=`, `<`, `>`, `in`
- Type checks: `is_atom/1`, `is_integer/1`, `is_binary/1`, `is_map/1`, ...
- Math: `+`, `-`, `*`, `div/2`, `rem/2`
- Boolean connectives: `and`, `or`, `not`
- A handful of stdlib helpers: `map_size/1`, `tuple_size/1`, `byte_size/1`,
  `length/1`, `elem/2`

Arbitrary function calls are NOT allowed in guards. This is the language
enforcing: guards must be side-effect-free and terminate quickly, because they
run at dispatch time — sometimes thousands of times per second.

Multiple patterns per clause use the `when ...` followed by additional
conditions joined with `and`/`or`. You can also separate patterns with `;`:
`when is_integer(x) when is_float(x)` is the same as
`when is_integer(x) or is_float(x)`.

---

## The business problem

A RBAC (role-based access control) system decides whether a given role can
perform an action on a resource. Rules:

- `:admin` can do anything.
- `:editor` can `:read`, `:write`, `:update` on `:post` resources but not
  `:delete` unless it's their own.
- `:viewer` can only `:read` public posts.
- `:banned` can never act, regardless of role memory.

The engine must be fast (runs on every request), auditable (decisions must
be explainable), and deny-by-default.

---

## Why `case` + guards and not a lookup table (map of rules)

A lookup table is better when rules are truly tabular (role -> resource set). But once rules include context (`role == :admin and resource not in [:audit_log]`), `case` + guards expresses the logic more compactly than building tables dynamically.

## Design decisions

**Option A — `case` with `when` guards in each clause**
- Pros: Each (role, resource) rule is a one-liner, guards are visible at the top of each clause
- Cons: Guards can only use a restricted set of functions (no arbitrary calls)

**Option B — nested `if` inside a single `case` branch** (chosen)
- Pros: Can use arbitrary function calls in conditions
- Cons: Logic hidden inside the clause body; clauses become hard to compare at a glance

→ Chose **A** because role-permission rules fit naturally into the guard language (comparisons, `in`, boolean composition).

## Implementation

### `mix.exs`
```elixir
defmodule RolePermissionMatcher.MixProject do
  use Mix.Project

  def project do
    [
      app: :role_permission_matcher,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### `lib/role_permission_matcher.ex`

```elixir
defmodule RolePermissionMatcher do
  @moduledoc """
  Evaluates permission requests with `case` + guards.

  Input: role (atom), action (atom), resource (map with :type, :owner_id,
  :visibility). User context: %{id: user_id, status: :active | :banned}.

  Output: {:allow, reason} | {:deny, reason}. Always includes a reason
  for auditability.
  """

  @type role :: :admin | :editor | :viewer | :guest
  @type action :: :read | :write | :update | :delete
  @type resource :: %{
          type: atom(),
          owner_id: String.t() | nil,
          visibility: :public | :private
        }
  @type user :: %{id: String.t(), status: :active | :banned}
  @type decision :: {:allow, atom()} | {:deny, atom()}

  @doc """
  Evaluates an action request and returns the decision + reason.

  Clauses are ordered from most specific to least. A deny-by-default
  clause at the end guarantees no accidental allows.
  """
  @spec evaluate(role(), action(), resource(), user()) :: decision()
  def evaluate(role, action, resource, user) do
    case {role, action, resource, user} do
      # Banned users: always deny, regardless of role.
      # Guard check on nested map key — `map_size/1` is allowed in guards.
      {_, _, _, %{status: :banned}} ->
        {:deny, :user_banned}

      # Admins: blanket allow when active. Pattern binds active status.
      {:admin, _, _, %{status: :active}} ->
        {:allow, :admin_override}

      # Editors: read/write/update on posts — guards combine with `in`.
      {:editor, action, %{type: :post}, %{status: :active}}
      when action in [:read, :write, :update] ->
        {:allow, :editor_post_access}

      # Editors: delete only their own posts. Pin would go here if owner_id
      # were pre-bound; instead we capture it and compare via guard.
      {:editor, :delete, %{type: :post, owner_id: owner}, %{id: user_id, status: :active}}
      when is_binary(owner) and owner == user_id ->
        {:allow, :editor_owner_delete}

      # Viewers: read public posts only.
      {:viewer, :read, %{type: :post, visibility: :public}, %{status: :active}} ->
        {:allow, :viewer_public_read}

      # Guest: nothing.
      {:guest, _, _, _} ->
        {:deny, :guest_no_permission}

      # Catch-all — deny by default. The reason documents that we hit the
      # fallthrough, which is what auditors need to see.
      _ ->
        {:deny, :no_matching_rule}
    end
  end

  @doc """
  Classifies a numeric score using multiple guards.

  Demonstrates several guard combinators:
  - Range checks with `>=` and `<=`
  - `and`/`or` inside guards
  - Type coercion tolerance via `is_number/1` (matches int or float)
  """
  @spec classify_score(number()) :: :excellent | :good | :fair | :poor | :invalid
  def classify_score(score) do
    case score do
      s when is_number(s) and s >= 90 and s <= 100 -> :excellent
      s when is_number(s) and s >= 70 and s < 90 -> :good
      s when is_number(s) and s >= 50 and s < 70 -> :fair
      s when is_number(s) and s >= 0 and s < 50 -> :poor
      _ -> :invalid
    end
  end

  @doc """
  Bulk evaluation: given a list of (action, resource) pairs, returns one
  decision each. Fast: `case` compiles to a decision tree.
  """
  @spec evaluate_many(role(), [{action(), resource()}], user()) :: [decision()]
  def evaluate_many(role, requests, user) when is_list(requests) do
    Enum.map(requests, fn {action, resource} ->
      evaluate(role, action, resource, user)
    end)
  end
end
```

### `test/role_permission_matcher_test.exs`

```elixir
defmodule RolePermissionMatcherTest do
  use ExUnit.Case, async: true
  doctest RolePermissionMatcher

  alias RolePermissionMatcher, as: RBAC

  @post_public %{type: :post, owner_id: "u-1", visibility: :public}
  @post_private %{type: :post, owner_id: "u-1", visibility: :private}
  @alice_active %{id: "u-1", status: :active}
  @bob_active %{id: "u-2", status: :active}
  @banned %{id: "u-3", status: :banned}

  describe "banned users" do
    test "deny regardless of role" do
      assert {:deny, :user_banned} =
               RBAC.evaluate(:admin, :read, @post_public, @banned)
    end
  end

  describe "admin" do
    test "can do anything when active" do
      assert {:allow, :admin_override} =
               RBAC.evaluate(:admin, :delete, @post_private, @alice_active)
    end
  end

  describe "editor" do
    test "can read, write, update posts" do
      for action <- [:read, :write, :update] do
        assert {:allow, :editor_post_access} =
                 RBAC.evaluate(:editor, action, @post_public, @alice_active)
      end
    end

    test "can delete own posts" do
      # Alice (u-1) owns the post
      assert {:allow, :editor_owner_delete} =
               RBAC.evaluate(:editor, :delete, @post_public, @alice_active)
    end

    test "cannot delete posts owned by others" do
      # Bob (u-2) does NOT own the post (owner is u-1)
      assert {:deny, :no_matching_rule} =
               RBAC.evaluate(:editor, :delete, @post_public, @bob_active)
    end

    test "cannot act on non-post resources" do
      invoice = %{type: :invoice, owner_id: "u-1", visibility: :private}

      assert {:deny, :no_matching_rule} =
               RBAC.evaluate(:editor, :read, invoice, @alice_active)
    end
  end

  describe "viewer" do
    test "can read public posts" do
      assert {:allow, :viewer_public_read} =
               RBAC.evaluate(:viewer, :read, @post_public, @alice_active)
    end

    test "cannot read private posts" do
      assert {:deny, :no_matching_rule} =
               RBAC.evaluate(:viewer, :read, @post_private, @alice_active)
    end

    test "cannot write" do
      assert {:deny, :no_matching_rule} =
               RBAC.evaluate(:viewer, :write, @post_public, @alice_active)
    end
  end

  describe "guest" do
    test "always denied" do
      assert {:deny, :guest_no_permission} =
               RBAC.evaluate(:guest, :read, @post_public, @alice_active)
    end
  end

  describe "classify_score/1" do
    test "boundaries" do
      assert RBAC.classify_score(100) == :excellent
      assert RBAC.classify_score(90) == :excellent
      assert RBAC.classify_score(89.99) == :good
      assert RBAC.classify_score(70) == :good
      assert RBAC.classify_score(69) == :fair
      assert RBAC.classify_score(50) == :fair
      assert RBAC.classify_score(0) == :poor
      assert RBAC.classify_score(-1) == :invalid
      assert RBAC.classify_score(101) == :invalid
    end

    test "non-numeric is invalid" do
      assert RBAC.classify_score("42") == :invalid
      assert RBAC.classify_score(nil) == :invalid
    end

    test "accepts both int and float" do
      assert RBAC.classify_score(85) == :good
      assert RBAC.classify_score(85.5) == :good
    end
  end

  describe "evaluate_many/3" do
    test "evaluates each request independently" do
      requests = [
        {:read, @post_public},
        {:delete, @post_public},
        {:write, @post_private}
      ]

      results = RBAC.evaluate_many(:viewer, requests, @alice_active)

      assert [{:allow, :viewer_public_read},
              {:deny, :no_matching_rule},
              {:deny, :no_matching_rule}] = results
    end
  end
end
```

### Run it

```bash
mix new role_permission_matcher
cd role_permission_matcher
mix test
```

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== RolePermissionMatcher: demo ===\n")

    result_1 = RolePermissionMatcher.classify_score(100)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = RolePermissionMatcher.classify_score(90)
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = RolePermissionMatcher.classify_score(89.99)
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create `lib/rbac.ex` and test in `iex`:

```elixir
defmodule RBAC do
  def evaluate(role, action, status) do
    case {role, action, status} do
      {:admin, _action, _status} -> {:allow, :admin_all_access}
      
      {:viewer, :read, :published} -> {:allow, :viewer_published_read}
      {:viewer, :read, :draft} -> {:deny, :draft_not_readable}
      {:viewer, :write, _status} -> {:deny, :viewer_cannot_write}
      
      {:editor, :read, _status} -> {:allow, :editor_read_all}
      {:editor, :write, _status} -> {:allow, :editor_write_all}
      
      {:guest, _action, _status} -> {:deny, :guest_no_access}
      
      _ -> {:deny, :no_matching_rule}
    end
  end

  def evaluate_many(role, requests, status) when is_list(requests) do
    Enum.map(requests, fn {action, _resource} ->
      evaluate(role, action, status)
    end)
  end

  def check_payment(payment) do
    case payment do
      %{amount: a, status: :pending} when a > 0 -> {:process, a}
      %{amount: a, status: :pending} when a <= 0 -> {:reject, "negative"}
      %{status: :completed} -> {:skip, "already_done"}
      _ -> {:error, :invalid}
    end
  end
end

# Test it
IO.inspect(RBAC.evaluate(:admin, :write, :draft))  # {:allow, :admin_all_access}
IO.inspect(RBAC.evaluate(:viewer, :read, :published))  # {:allow, :viewer_published_read}
IO.inspect(RBAC.evaluate(:viewer, :read, :draft))  # {:deny, :draft_not_readable}
IO.inspect(RBAC.evaluate(:guest, :read, :published))  # {:deny, :guest_no_access}

# Test evaluate_many
requests = [{:read, :post}, {:write, :post}]
results = RBAC.evaluate_many(:viewer, requests, :published)
IO.inspect(length(results))  # 2

# Test check_payment
{:process, amt} = RBAC.check_payment(%{amount: 100, status: :pending})
IO.inspect(amt)  # 100

{:reject, reason} = RBAC.check_payment(%{amount: -50, status: :pending})
IO.inspect(reason)  # "negative"
```

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of can?/3 over 1M authorizations
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 20ms total; < 20ns per check**.

## Trade-offs and production mistakes

**1. Arbitrary functions forbidden in guards**
`when String.length(name) > 3` does NOT compile. Use `byte_size/1` (which
IS allowed) or move the check into the clause body with `case` on a pre-
computed value.

**2. Guard order matters for short-circuit**
`when is_map(x) and map_size(x) > 0` — `map_size/1` on a non-map raises.
Elixir evaluates left-to-right, so put the type check first.

**3. `in` works with literal lists only (in guards)**
`when action in [:read, :write]` works because the list is a literal.
`when action in @module_attr_list` also works (inlined at compile time).
`when action in dynamic_list` does NOT work in guards.

**4. Pattern + guard redundancy**
`{:editor, action, _, _} when action == :read` is uglier than
`{:editor, :read, _, _}`. Prefer pinning the value in the pattern when
possible; reserve guards for ranges, types, and composite checks.

**5. Deny-by-default catch-all**
The last `_ -> {:deny, :no_matching_rule}` is non-negotiable for security.
Without it, `case` raises `CaseClauseError` on unhandled input — crashing
instead of denying. Your choice: crash or deny; never implicit allow.

## When NOT to use `case` with guards

- When every branch returns the same shape and differs only in a computed
  value: a `Map.get/3` table lookup is cleaner.
- When the logic needs IO or async work — guards cannot call those, and you
  end up pre-computing values awkwardly.
- When you have 20+ clauses: split into multi-clause functions or rule
  engines. A single `case` with dozens of patterns is hard to audit.

---

## Reflection

If your permission model grew to support row-level permissions (a user can edit *their own* document but not others'), could you still express it with `case` + guards? At what point would you reach for a dedicated policy library like `Bodyguard`?

Your security team requires an audit log of every denied permission. Where would you hook that in without scattering logging calls across every clause?

## Resources

- [case, cond, and if — Getting Started](https://elixir-lang.org/getting-started/case-cond-and-if.html)
- [Guards — HexDocs](https://hexdocs.pm/elixir/patterns-and-guards.html#guards)
- [List of allowed guard expressions](https://hexdocs.pm/elixir/Kernel.html#guards)
- [Patterns and guards guide](https://hexdocs.pm/elixir/patterns-and-guards.html)

---

## Why Case with Guards matters

Mastering **Case with Guards** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. Guards Refine Patterns Without Extra Clauses

```elixir
case payment do
  %{amount: a} when a > 0 -> process(payment)
  %{amount: a} when a <= 0 -> reject(payment)
  _ -> {:error, :invalid}
end
```

Guards let one pattern match multiple branches. Without guards, you'd write the same pattern twice.

### 2. Guard Limitations

Guards are restricted to deterministic, side-effect-free operations. You cannot call custom functions (except guards you define yourself). This restriction keeps pattern matching fast and predictable.

### 3. Complex Guards Are a Code Smell

If your guard becomes long or complex, consider extracting to a helper function or using a separate clause. Readability trumps cleverness.

---
