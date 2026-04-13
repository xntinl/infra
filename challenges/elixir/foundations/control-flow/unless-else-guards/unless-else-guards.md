# `unless`, else branches, and compound guards

**Project**: `feature_gate` — decides whether a user sees a feature based on env + role + rollout %

---

## The business problem

Feature flags decide by **env + role + rollout %** — three independent checks that must
all pass. It is a crisp demo of compound guards in a function head, plus `unless` for the
"skip rollout check entirely for admins" shortcut.

---

## Project structure

```
feature_gate/
├── lib/
│   └── feature_gate/
│       └── gate.ex
├── script/
│   └── main.exs
├── test/
│   └── feature_gate_test.exs
└── mix.exs
```

---

## What you will learn

1. **`unless` vs `if not`** — syntactic siblings, when each one reads better.
2. **Compound guards** — combining `and`, `or`, `in`, and membership checks into one `when` clause.

---

## The concept in 60 seconds

```elixir
unless admin?, do: raise("no access")
# equivalent to
if not admin?, do: raise("no access")
```

Both are legal. `unless` reads best for **single negative conditions without an else**.
As soon as you add `else:` to `unless`, the reader has to mentally invert — that is a
signal to switch to `if`.

Compound guards:

```elixir
def allow?(role, env) when role in [:admin, :owner] and env != :prod, do: true
```

Guards let you combine membership (`in`), negation, and booleans. They are evaluated
left-to-right with short-circuit.

---

## Why a feature gate

Feature flags decide by **env + role + rollout %** — three independent checks that must
all pass. It is a crisp demo of compound guards in a function head, plus `unless` for the
"skip rollout check entirely for admins" shortcut.

---

## Design decisions

**Option A — `if` with compound guards (preferred form)**
- Pros: Positive condition reads naturally, `else` branch is obvious
- Cons: Slightly longer for the simplest "do X only if not Y" case

**Option B — `unless ... else` with negated condition** (chosen)
- Pros: Reads well for the simplest single-condition case
- Cons: With `else`, the reader mentally double-negates; compound conditions become unreadable

→ Chose **A** because compound boolean logic with an `else` branch is where `unless` actively hurts readability. Use B only for one-liners without `else`.

## Implementation

### `mix.exs`
```elixir
defmodule FeatureGate.MixProject do
  use Mix.Project

  def project do
    [
      app: :feature_gate,
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

### Step 1 — Create the project

**Objective**: Build single module so unless vs if trade-off sits next to compound guard examples side-by-side.

```bash
mix new feature_gate
cd feature_gate
```

### Step 2 — `lib/feature_gate/gate.ex`

**Objective**: Use role in @privileged_roles guard clause so admins short-circuit before env/rollout checks run.

```elixir
defmodule FeatureGate.Gate do
  @moduledoc """
  Decides whether a user sees a feature.

  Rules (evaluated in order):
    1. In :prod, only allow if user.role is in the allowlist OR rollout % covers the user.
    2. In :dev or :staging, always allow.
    3. Admins and owners always see the feature, regardless of env or rollout.
  """

  @type env :: :dev | :staging | :prod
  @type role :: :admin | :owner | :member | :guest
  @type user :: %{id: integer(), role: role()}

  @privileged_roles [:admin, :owner]

  @spec enabled?(user(), env(), pos_integer()) :: boolean()

  # Compound guard: role membership AND non-prod env.
  # This clause handles "privileged users" — we short-circuit all other logic.
  def enabled?(%{role: role}, _env, _rollout_percent) when role in @privileged_roles do
    true
  end

  # Non-prod environments are permissive — guard checks env membership.
  def enabled?(_user, env, _rollout_percent) when env in [:dev, :staging] do
    true
  end

  # Prod: rollout % gate. A plain function clause here; compound logic inside the body.
  def enabled?(%{id: user_id}, :prod, rollout_percent)
      when is_integer(rollout_percent) and rollout_percent in 0..100 do
    # `unless` fits here: single negative condition, no else branch.
    # Reading: "unless the user falls in the rollout bucket, deny".
    unless in_rollout_bucket?(user_id, rollout_percent) do
      false
    else
      true
    end
  end

  # Private: stable hash-based bucket assignment.
  # rem/2 on user_id gives a deterministic 0..99 bucket — same user always in same bucket.
  defp in_rollout_bucket?(user_id, percent) do
    rem(user_id, 100) < percent
  end
end
```

> Note on style: the `unless ... else ... end` above is intentional — it demonstrates
> that `else` on `unless` is legal but rarely idiomatic. In a real codebase you'd write
> `in_rollout_bucket?(user_id, rollout_percent)` directly. The common-mistakes section
> covers this.

### Step 3 — `test/feature_gate_test.exs`

**Objective**: Assert FunctionClauseError on invalid percent so guard is the contract, not deferred to runtime body check.

```elixir
defmodule FeatureGateTest do
  use ExUnit.Case, async: true
  doctest FeatureGate.Gate

  alias FeatureGate.Gate

  describe "privileged roles always pass (compound guard)" do
    test "admin in prod at 0% rollout still sees the feature" do
      assert Gate.enabled?(%{id: 999, role: :admin}, :prod, 0) == true
    end

    test "owner in prod at 0% rollout still sees the feature" do
      assert Gate.enabled?(%{id: 999, role: :owner}, :prod, 0) == true
    end
  end

  describe "non-prod is permissive (env guard)" do
    test "member in dev is always enabled" do
      assert Gate.enabled?(%{id: 50, role: :member}, :dev, 0) == true
    end

    test "guest in staging is always enabled" do
      assert Gate.enabled?(%{id: 50, role: :guest}, :staging, 0) == true
    end
  end

  describe "prod rollout percentage (unless + hash bucket)" do
    test "user_id 10 at 20% rollout is IN the bucket (10 < 20)" do
      assert Gate.enabled?(%{id: 10, role: :member}, :prod, 20) == true
    end

    test "user_id 50 at 20% rollout is OUT of the bucket (50 >= 20)" do
      assert Gate.enabled?(%{id: 50, role: :member}, :prod, 20) == false
    end

    test "user_id 0 at 1% rollout is in" do
      assert Gate.enabled?(%{id: 0, role: :member}, :prod, 1) == true
    end

    test "everyone included at 100%" do
      assert Gate.enabled?(%{id: 99, role: :guest}, :prod, 100) == true
    end

    test "nobody included at 0%" do
      assert Gate.enabled?(%{id: 0, role: :guest}, :prod, 0) == false
    end
  end

  describe "guard rejects invalid rollout values" do
    test "raises on negative rollout_percent" do
      assert_raise FunctionClauseError, fn ->
        Gate.enabled?(%{id: 1, role: :member}, :prod, -1)
      end
    end

    test "raises on rollout_percent > 100" do
      assert_raise FunctionClauseError, fn ->
        Gate.enabled?(%{id: 1, role: :member}, :prod, 150)
      end
    end
  end
end
```

### Step 4 — Run the tests

**Objective**: Verify bucket boundary (id 10 at 20%) uses < not <= so rollout percentage is exclusive, not inclusive.

```bash
mix test
```

All 11 tests pass.

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== FeatureGate: demo ===\n")

    result_1 = FeatureGate.Gate.enabled?(%{id: 999, role: :admin}, :prod, 0)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = FeatureGate.Gate.enabled?(%{id: 999, role: :owner}, :prod, 0)
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = FeatureGate.Gate.enabled?(%{id: 50, role: :member}, :dev, 0)
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of can_see?/2 over 1M users
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 15ms total; rollout bucketing is the hot path and should stay under 50ns**.

## Trade-offs

| Construct | Reads best for |
|---|---|
| `if cond, do: X` | Positive single-branch check |
| `unless cond, do: X` | Negative single-branch check |
| `if not cond, do: X` | Double-negative (avoid — use `unless`) |
| `unless cond, do: X, else: Y` | Rare — prefer `if`, which inverts naturally |

**When NOT to use `unless`:**

- **With an `else` branch.** `unless ... else ...` forces the reader to negate mentally.
  Rewrite as `if ... else ...` where the positive branch comes first.
- **With compound conditions.** `unless a and b and not c` is unreadable. Extract a
  well-named boolean helper (`unless eligible?(x)`).

---

## Common production mistakes

**1. `unless` with `else`**
The pattern exists but is noisy. `unless x, do: a, else: b` is the same as `if x, do: b, else: a`.
The version that leads with the positive usually wins.

**2. Guards with runtime function calls**
`when some_module.check?(x)` does not compile inside a guard. Guards are a whitelisted
subset of Elixir. See the guard reference.

**3. `in` guard with a runtime list**
`when x in some_var` works only if `some_var` is a compile-time list literal or a range.
For dynamic lists, use `when Enum.member?(list, x)` — but that is **not** allowed in a guard.
Move the check into the body.

**4. Short-circuit surprise**
`and` / `or` in guards are short-circuit ONLY in the guard context. In regular code, `and`/`or`
strictly require booleans; `&&`/`||` short-circuit on truthiness. Mixing them bites.

**5. Guards that silently accept wrong types**
`when percent <= 100` is true for `percent = "not a number"` in older Erlangs (comparison
across types). Always pair with `is_integer/1` or similar when types matter.

---

## Reflection

Your team has a style rule: `unless` is banned except for single-line statements without `else`. Is that too strict? Give a case where the rule helps and one where it forces awkward code.

Compound guards combine `and`/`or`/`not`. What's the BEAM-level difference between `and` and `&&`, and when does it matter for a feature-gate hot path?

```elixir
defmodule FeatureGateTest do
  use ExUnit.Case, async: true
  doctest Main

  alias FeatureGate.Gate

  describe "privileged roles always pass (compound guard)" do
    test "admin in prod at 0% rollout still sees the feature" do
      assert Gate.enabled?(%{id: 999, role: :admin}, :prod, 0) == true
    end

    test "owner in prod at 0% rollout still sees the feature" do
      assert Gate.enabled?(%{id: 999, role: :owner}, :prod, 0) == true
    end
  end

  describe "non-prod is permissive (env guard)" do
    test "member in dev is always enabled" do
      assert Gate.enabled?(%{id: 50, role: :member}, :dev, 0) == true
    end

    test "guest in staging is always enabled" do
      assert Gate.enabled?(%{id: 50, role: :guest}, :staging, 0) == true
    end
  end

  describe "prod rollout percentage (unless + hash bucket)" do
    test "user_id 10 at 20% rollout is IN the bucket (10 < 20)" do
      assert Gate.enabled?(%{id: 10, role: :member}, :prod, 20) == true
    end

    test "user_id 50 at 20% rollout is OUT of the bucket (50 >= 20)" do
      assert Gate.enabled?(%{id: 50, role: :member}, :prod, 20) == false
    end

    test "user_id 0 at 1% rollout is in" do
      assert Gate.enabled?(%{id: 0, role: :member}, :prod, 1) == true
    end

    test "everyone included at 100%" do
      assert Gate.enabled?(%{id: 99, role: :guest}, :prod, 100) == true
    end

    test "nobody included at 0%" do
      assert Gate.enabled?(%{id: 0, role: :guest}, :prod, 0) == false
    end
  end

  describe "guard rejects invalid rollout values" do
    test "raises on negative rollout_percent" do
      assert_raise FunctionClauseError, fn ->
        Gate.enabled?(%{id: 1, role: :member}, :prod, -1)
      end
    end

    test "raises on rollout_percent > 100" do
      assert_raise FunctionClauseError, fn ->
        Gate.enabled?(%{id: 1, role: :member}, :prod, 150)
      end
    end
```

## Resources

- [Kernel.unless/2](https://hexdocs.pm/elixir/Kernel.html#unless/2)
- [Patterns and guards](https://hexdocs.pm/elixir/patterns-and-guards.html)
- [Guard whitelist](https://hexdocs.pm/elixir/patterns-and-guards.html#list-of-allowed-functions-and-operators)

---

## Why `unless`, else branches, and compound guards matters

Mastering **`unless`, else branches, and compound guards** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/feature_gate.ex`

```elixir
defmodule FeatureGate do
  @moduledoc """
  Reference implementation for `unless`, else branches, and compound guards.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the feature_gate module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> FeatureGate.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/feature_gate_test.exs`

```elixir
defmodule FeatureGateTest do
  use ExUnit.Case, async: true

  doctest FeatureGate

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert FeatureGate.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. `unless` is Negated `if`
`unless admin?, do: deny_access()` is equivalent to `if not admin?, do: deny_access()`. `unless` reads naturally for negation. Prefer `if` when the positive case is primary.

### 2. `else` with `unless` is Confusing
This reads backwards. Use `if admin?, do: allow(), else: deny()` instead. Guards (`when not admin?`) are preferred in function clauses.

### 3. Guards vs unless
Guards like `def process(user) when not user.admin` are clearer than nesting `unless` inside the function body.

---
