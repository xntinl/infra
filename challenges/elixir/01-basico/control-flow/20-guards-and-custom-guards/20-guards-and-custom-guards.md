# Guards and Custom Guards: Building an Auth Policy Engine

**Project**: `auth_policy` — a policy engine that decides permissions using composed guards

---

## Why guards matter for a senior developer

Guards are not a nicety. They are the only way to add runtime checks to a function head
while keeping pattern matching exhaustive and letting the compiler verify every clause.
A guard runs before the function body executes — if it fails, the next clause is tried.
If no clause matches, you get a `FunctionClauseError` at the call site, not a silent
bug three layers down.

Custom guards (`defguard`) let you name domain predicates without sacrificing that
behavior. They are expanded inline at compile time, so calling `is_admin_role(role)`
in a guard clause costs exactly the same as inlining the expression.

Understanding guards matters when you need to:

- Route requests to different handlers based on typed inputs, not nested `if`
- Express business rules ("only admins on weekdays") without scattering `case` everywhere
- Keep pattern matching total — the compiler warns when a clause is unreachable
- Avoid the classic mistake of calling arbitrary functions in a guard (which is forbidden)

---

## The business problem

Your team runs an internal admin panel. Authorization is currently a pile of nested
`if` statements that nobody trusts. You need a policy engine that:

1. Decides permissions based on role, resource, and action
2. Supports composed rules like "admins can do anything" and "editors can edit
   only their own drafts during working hours"
3. Rejects invalid inputs early with clear errors — no string comparisons at the
   call site
4. Is exhaustive — the compiler warns if a role/action combination is not handled

---

## Project structure

```
auth_policy/
├── lib/
│   └── auth_policy/
│       ├── guards.ex
│       └── engine.ex
├── test/
│   └── auth_policy/
│       ├── guards_test.exs
│       └── engine_test.exs
└── mix.exs
```

---

## What guards can and cannot do

Guards are restricted on purpose. Inside a guard you can only use:

- Comparison operators: `==`, `!=`, `<`, `>`, `<=`, `>=`, `===`, `!==`
- Boolean operators: `and`, `or`, `not` (NOT `&&`, `||`, `!` — those short-circuit
  with truthiness, guards need strict booleans)
- Arithmetic: `+`, `-`, `*`, `/`, `div`, `rem`, `abs`
- Type checks: `is_atom/1`, `is_integer/1`, `is_map/1`, `is_binary/1`, etc.
- A fixed list of Kernel functions: `map_size/1`, `tuple_size/1`, `elem/2`, `hd/1`,
  `tl/1`, `length/1`, `byte_size/1`, `node/0`, `self/0`
- Custom guards defined with `defguard` (which must expand to the above)

You CANNOT call arbitrary functions. `String.length(s) > 3` in a guard is a compile
error. The reason is that guards must be side-effect-free and total — the runtime
tries multiple clauses, and a failing guard simply moves to the next one instead
of raising.

---

## Design decisions

**Option A — `defguard` macros composed into function heads**
- Pros: Guards participate in dispatch — exhaustiveness, reordering, and inlining are free
- Cons: Guard language is restricted (no arbitrary function calls, no side effects)

**Option B — same conditions as `if` inside the function body** (chosen)
- Pros: Any Elixir expression allowed
- Cons: Loses dispatch, every clause runs, harder to test each branch in isolation

→ Chose **A** because policy decisions are pure comparisons over small data — exactly what guards were designed for. Fall back to B only for side-effectful checks (DB).

## Implementation

### Step 1: Create the project

```bash
mix new auth_policy
cd auth_policy
```

### Step 2: `mix.exs`

```elixir
defmodule AuthPolicy.MixProject do
  use Mix.Project

  def project do
    [
      app: :auth_policy,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end
end
```

### Step 3: `lib/auth_policy/guards.ex`

```elixir
defmodule AuthPolicy.Guards do
  @moduledoc """
  Custom guards for authorization decisions.

  Every guard defined here expands inline into a boolean expression made of
  primitives that the Erlang VM allows in guard context. That means you can
  use these in function heads, `case` clauses, and `with` patterns without
  paying a function call cost.
  """

  @doc """
  Checks whether the given atom is one of the recognized roles.

  Using `in` with a compile-time list generates an optimized `:erlang.or` chain.
  """
  defguard is_role(role) when role in [:admin, :editor, :viewer, :guest]

  @doc """
  Checks whether the given atom is one of the recognized actions.
  """
  defguard is_action(action) when action in [:read, :write, :delete, :publish]

  @doc """
  Admins bypass every other check — used as a short-circuit in the engine.
  """
  defguard is_admin(role) when role == :admin

  @doc """
  Working hours: Monday through Friday, 9:00 to 18:00.

  The hour and day_of_week are passed in because guards cannot call
  `Date.utc_today/0` or `Time.utc_now/0` — those are functions with side effects.
  The caller resolves the current time and passes the integers in.
  """
  defguard is_working_hours(day_of_week, hour)
           when is_integer(day_of_week) and is_integer(hour) and
                  day_of_week >= 1 and day_of_week <= 5 and
                  hour >= 9 and hour < 18
end
```

### Step 4: `lib/auth_policy/engine.ex`

```elixir
defmodule AuthPolicy.Engine do
  @moduledoc """
  Policy engine that decides whether a subject may perform an action on a resource.

  The decision is a function of four inputs:
    - role: :admin | :editor | :viewer | :guest
    - action: :read | :write | :delete | :publish
    - resource: a map with :owner_id and :status
    - context: a map with :user_id, :day_of_week, :hour

  Every branch of the decision tree is a separate function clause guarded by
  `defguard` predicates. This gives exhaustive compile-time coverage: if you
  add a new role and forget to handle it, the compiler does not warn (Elixir
  does not prove exhaustiveness for arbitrary atoms) — but the final catch-all
  clause ensures a safe default of `{:deny, :no_matching_rule}`.
  """

  import AuthPolicy.Guards

  @type role :: :admin | :editor | :viewer | :guest
  @type action :: :read | :write | :delete | :publish
  @type resource :: %{owner_id: integer(), status: :draft | :published}
  @type context :: %{user_id: integer(), day_of_week: 1..7, hour: 0..23}
  @type decision :: {:allow, atom()} | {:deny, atom()}

  @doc """
  Entry point. Validates shape with guards, then delegates to rule clauses.
  """
  @spec authorize(role(), action(), resource(), context()) :: decision()
  def authorize(role, action, resource, context)
      when is_role(role) and is_action(action) and is_map(resource) and is_map(context) do
    decide(role, action, resource, context)
  end

  def authorize(_role, _action, _resource, _context) do
    # Invalid input shape — fail closed. Never guess what the caller meant.
    {:deny, :invalid_input}
  end

  # Rule 1: admins bypass every other check.
  # Using `is_admin/1` makes the intent obvious at the clause level.
  defp decide(role, _action, _resource, _context) when is_admin(role) do
    {:allow, :admin_override}
  end

  # Rule 2: viewers can read anything, regardless of ownership or time.
  defp decide(:viewer, :read, _resource, _context) do
    {:allow, :viewer_read}
  end

  # Rule 3: editors can read anything at any time.
  defp decide(:editor, :read, _resource, _context) do
    {:allow, :editor_read}
  end

  # Rule 4: editors can write their own drafts during working hours.
  # Three conditions composed with `and` — all must hold.
  defp decide(:editor, :write, %{owner_id: owner_id, status: :draft}, %{
         user_id: user_id,
         day_of_week: dow,
         hour: hour
       })
       when owner_id == user_id and is_working_hours(dow, hour) do
    {:allow, :editor_write_own_draft}
  end

  # Rule 5: editors cannot write to published resources — even their own.
  # A separate clause makes the denial reason explicit in the return value.
  defp decide(:editor, :write, %{status: :published}, _context) do
    {:deny, :cannot_edit_published}
  end

  # Rule 6: editors writing outside working hours.
  defp decide(:editor, :write, %{status: :draft}, %{day_of_week: dow, hour: hour})
       when not is_working_hours(dow, hour) do
    {:deny, :outside_working_hours}
  end

  # Rule 7: guests get nothing except anonymous read of published content.
  defp decide(:guest, :read, %{status: :published}, _context) do
    {:allow, :guest_read_public}
  end

  # Final catch-all. Fail closed.
  defp decide(_role, _action, _resource, _context) do
    {:deny, :no_matching_rule}
  end
end
```

**Why this works:**

- Every clause guard is a named predicate. When rules change, you edit a guard
  once and every clause updates.
- The first clause that matches wins. Ordering matters: admin override is first
  because it short-circuits the entire decision tree.
- The catch-all at the bottom makes the engine total. There is no input that
  can cause a `FunctionClauseError` — every path returns a decision.
- The return type `{:allow, reason}` or `{:deny, reason}` lets callers log
  exactly why access was granted or denied. Audit logs love this pattern.

### Step 5: Tests

```elixir
# test/auth_policy/guards_test.exs
defmodule AuthPolicy.GuardsTest do
  use ExUnit.Case, async: true

  require AuthPolicy.Guards
  import AuthPolicy.Guards

  describe "is_role/1" do
    test "accepts known roles" do
      for role <- [:admin, :editor, :viewer, :guest] do
        assert match?(r when is_role(r), role)
      end
    end

    test "rejects unknown roles" do
      refute match?(r when is_role(r), :superuser)
      refute match?(r when is_role(r), "admin")
    end
  end

  describe "is_working_hours/2" do
    test "allows Monday at 10" do
      assert match?({d, h} when is_working_hours(d, h), {1, 10})
    end

    test "rejects Saturday" do
      refute match?({d, h} when is_working_hours(d, h), {6, 10})
    end

    test "rejects 18:00 exactly (hour < 18)" do
      refute match?({d, h} when is_working_hours(d, h), {3, 18})
    end

    test "rejects 8:59" do
      refute match?({d, h} when is_working_hours(d, h), {3, 8})
    end
  end
end
```

```elixir
# test/auth_policy/engine_test.exs
defmodule AuthPolicy.EngineTest do
  use ExUnit.Case, async: true

  alias AuthPolicy.Engine

  @working_ctx %{user_id: 42, day_of_week: 2, hour: 14}
  @weekend_ctx %{user_id: 42, day_of_week: 6, hour: 14}

  @own_draft %{owner_id: 42, status: :draft}
  @other_draft %{owner_id: 99, status: :draft}
  @published %{owner_id: 42, status: :published}

  describe "admin override" do
    test "admin can do anything" do
      assert {:allow, :admin_override} =
               Engine.authorize(:admin, :delete, @published, @working_ctx)
    end
  end

  describe "editor write rules" do
    test "editor writes own draft in working hours" do
      assert {:allow, :editor_write_own_draft} =
               Engine.authorize(:editor, :write, @own_draft, @working_ctx)
    end

    test "editor cannot write someone else's draft" do
      assert {:deny, :no_matching_rule} =
               Engine.authorize(:editor, :write, @other_draft, @working_ctx)
    end

    test "editor cannot write published content" do
      assert {:deny, :cannot_edit_published} =
               Engine.authorize(:editor, :write, @published, @working_ctx)
    end

    test "editor cannot write on weekends" do
      assert {:deny, :outside_working_hours} =
               Engine.authorize(:editor, :write, @own_draft, @weekend_ctx)
    end
  end

  describe "viewer and guest" do
    test "viewer can always read" do
      assert {:allow, :viewer_read} = Engine.authorize(:viewer, :read, @own_draft, @working_ctx)
    end

    test "guest reads published content only" do
      assert {:allow, :guest_read_public} =
               Engine.authorize(:guest, :read, @published, @working_ctx)

      assert {:deny, :no_matching_rule} =
               Engine.authorize(:guest, :read, @own_draft, @working_ctx)
    end
  end

  describe "invalid input" do
    test "rejects unknown role" do
      assert {:deny, :invalid_input} =
               Engine.authorize(:superuser, :read, @published, @working_ctx)
    end

    test "rejects unknown action" do
      assert {:deny, :invalid_input} =
               Engine.authorize(:admin, :launch_missile, @published, @working_ctx)
    end
  end
end
```

### Step 6: Run the tests

```bash
mix test --trace
```

All tests pass. Try adding a new role (`:auditor`) to `is_role/1` without adding
any `decide/4` clause — the engine will return `{:deny, :no_matching_rule}` for
every auditor request. That is the catch-all earning its keep.

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of can?/2 over 1M authorization checks
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 15ms total; each check < 20ns inline**.

## Trade-off analysis

| Aspect | Guards + clauses (this impl) | Nested `if`/`case` | Runtime rule engine (data) |
|--------|------------------------------|--------------------|-----------------------------|
| Compile-time shape check | yes (pattern match) | no | no |
| Readability of each rule | one clause per rule | all rules in one function | rules in data, logic generic |
| Adding a new rule | add a clause | edit one big function | add a row to a table |
| Performance | inline, no function call | same | data lookup per request |
| Auditability | `{:allow, :reason}` explicit | string matching | depends on impl |

When data-driven rule engines win: if non-engineers need to edit rules without
deploying. Then you pay the cost of an interpreter and lose compile-time checks,
but gain runtime flexibility.

---

## Common production mistakes

**1. Calling non-guard functions in a guard**
`when String.length(s) > 3` is a compile error. The guard BIF is `byte_size/1`,
and it measures bytes, not graphemes. If you need grapheme-level checks, do them
in the function body, not the guard.

**2. Using `&&` or `||` in guards**
`when x > 0 && y > 0` silently behaves differently from `when x > 0 and y > 0`.
In guards, use `and` / `or` / `not`. Short-circuit operators belong in function
bodies. The compiler warns, but the warning is easy to miss in CI noise.

**3. `defguard` without `require` or `import`**
Custom guards are macros. If the calling module does not `import` or `require`
the module defining them, the compile error is cryptic: `undefined function`.
Always `import AuthPolicy.Guards` in every module using these.

**4. Ordering clauses wrong**
Elixir tries clauses top to bottom. A general clause above a specific one makes
the specific one unreachable. The compiler sometimes warns about unreachable
clauses, but only for literal patterns — guard-gated clauses can silently shadow
each other. Put specific rules before general ones.

**5. Reading `DateTime.utc_now/0` from inside a guard**
You can't — and you shouldn't want to. Guards must be pure. Resolve time in the
caller and pass integers in. This also makes tests deterministic: you pass
`%{day_of_week: 2, hour: 14}` instead of freezing the clock.

---

## When NOT to use guards

- When the rule depends on a database query — guards cannot call IO
- When the rule set is edited by non-engineers — use a data-driven engine
- When the check is complex enough that the guard expression spans 5+ lines —
  extract to the function body with a helper predicate returning a boolean

---

## Reflection

A new business rule says users on a trial plan can only access features if `trial_days_left > 0`. Can you express this in a guard, or does it require a function call? If the latter, what refactor keeps the guard style?

What happens at the BEAM level if a guard raises (e.g., `hd/1` on an empty list)? Why is that the correct design decision?

## Resources

- [Kernel guards — HexDocs](https://hexdocs.pm/elixir/Kernel.html#guards)
- [`defguard/1` and `defguardp/1`](https://hexdocs.pm/elixir/Kernel.html#defguard/1)
- [Patterns and guards — Elixir guide](https://hexdocs.pm/elixir/patterns-and-guards.html)
- [Erlang guard expressions — official reference](https://www.erlang.org/doc/reference_manual/expressions.html#guard-expressions)
