# ExUnit tags, `:skip`, `:exclude`, and CI filtering

**Project**: `exunit_tags` — a `FeatureFlags` module with tests tagged by speed
(`:fast` / `:slow`), by external dependency (`:integration`, `:external`), and
by environment (`:skip`).

---

## Project context

Your CI takes 12 minutes. 11 of them are three integration tests hitting a
real Redis. You want a "fast lane" that runs in 30 seconds for every PR
and a "full lane" that runs nightly. ExUnit tags make this trivial — no
plugins, no separate test runner.

Tags also let you temporarily skip a flaky test (`@tag :skip`), mark an
OS-specific test, or gate a test behind `mix test --only integration`.

## Why ExUnit tags and not X

**Why not separate test directories (`test/fast`, `test/integration`)?**
Because you'd lose co-location — the integration test for module `X` lives
right next to the unit test for `X`. Directory splits force duplication.

**Why not a separate test runner or plugin?** You'd fight two tools; ExUnit
tags handle it natively with `--include` / `--exclude` / `--only`.

**Why not comments + grep?** Tags are first-class metadata that `mix test`
understands; comments require human discipline. The moment CI depends on it,
you want a structured check.

Project structure:

```
exunit_tags/
├── lib/
│   └── feature_flags.ex
├── test/
│   ├── feature_flags_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Core concepts

### 1. `@tag` vs `@moduletag` vs `@describetag`

```elixir
@moduletag :integration    # applies to every test in the module
@describetag :slow         # applies to every test in the current describe
@tag :external             # applies to the next test only
@tag timeout: 60_000       # tags can carry values too
```

### 2. Configuring defaults in `test_helper.exs`

```elixir
ExUnit.start(exclude: [:integration, :external])
```

Everything tagged `:integration` or `:external` is skipped by default. Turn
them on with `mix test --include integration`.

### 3. `--only`, `--include`, `--exclude`

- `--only tag` — run **only** tests with that tag.
- `--include tag` — add tests with that tag to the default run.
- `--exclude tag` — remove tests with that tag from the default run.

`--only integration` implies `--include integration`.

### 4. `@tag :skip` is the "don't run this right now" escape hatch

`ExUnit.start(exclude: [:skip])` in `test_helper.exs` plus `@tag :skip` on
an individual test is the cleanest way to park a flaky test without
deleting it. Always leave a comment explaining *why* it's skipped.

---

## Design decisions

**Option A — Include everything by default, exclude slow locally**
- Pros: CI sees everything; developers can opt out.
- Cons: Every `mix test` hits the network by default; painful locally.

**Option B — Exclude `:integration`/`:external` by default, include in CI** (chosen)
- Pros: `mix test` stays fast for developers; CI opts in explicitly.
- Cons: A developer could forget to run integration tests before pushing.

→ Chose **B** because the fast-feedback loop beats the safety net; CI is
the safety net. Pair it with a pre-push hook or a required CI job.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new exunit_tags
cd exunit_tags
```

### Step 2: `lib/feature_flags.ex`

**Objective**: Implement `feature_flags.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule FeatureFlags do
  @moduledoc """
  A deterministic feature-flag checker used to demonstrate tagged tests.
  """

  @flags %{
    "new_checkout" => true,
    "dark_mode" => true,
    "legacy_api" => false
  }

  @spec enabled?(String.t()) :: boolean()
  def enabled?(flag) when is_binary(flag), do: Map.get(@flags, flag, false)

  @spec all() :: %{String.t() => boolean()}
  def all, do: @flags

  @doc "Simulates a slow external check. Used in `:slow` tests."
  @spec expensive_check(String.t()) :: boolean()
  def expensive_check(flag) do
    Process.sleep(200)
    enabled?(flag)
  end
end
```

### Step 3: `test/test_helper.exs`

**Objective**: Implement `test_helper.exs` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
# Skip `:integration`, `:external`, and `:skip` by default.
# Include them explicitly with: `mix test --include integration`
ExUnit.start(exclude: [:integration, :external, :skip])
```

### Step 4: `test/feature_flags_test.exs`

**Objective**: Write `feature_flags_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.


```elixir
defmodule FeatureFlagsTest do
  use ExUnit.Case, async: true

  describe "enabled?/1 — fast checks" do
    @describetag :fast

    test "returns true for a known-on flag" do
      assert FeatureFlags.enabled?("new_checkout")
    end

    test "returns false for a known-off flag" do
      refute FeatureFlags.enabled?("legacy_api")
    end

    test "returns false for an unknown flag" do
      refute FeatureFlags.enabled?("does_not_exist")
    end
  end

  describe "expensive_check/1 — slow path" do
    @describetag :slow

    test "expensive check still returns the same result" do
      assert FeatureFlags.expensive_check("new_checkout") == true
    end
  end

  describe "integration — runs only with --include integration" do
    @describetag :integration

    test "treat this as if it hit a real flag service" do
      # In a real suite this would call a network service.
      assert FeatureFlags.enabled?("dark_mode")
    end
  end

  describe "skipped tests" do
    # Parked: re-enable after MR #1234 fixes the underlying race.
    @tag :skip
    test "TODO: re-enable after flaky provider is fixed" do
      flunk("should not have run")
    end
  end

  describe "per-test timeout via tag" do
    @tag timeout: 500
    test "tight timeout — passes because work is fast" do
      assert FeatureFlags.enabled?("new_checkout")
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
# Fast lane: excludes :integration, :external, :skip by default.
mix test

# Only fast tests, nothing else:
mix test --only fast

# Include integration tests for the nightly run:
mix test --include integration

# Exclude slow tests as well for a minimal check:
mix test --exclude slow
```

### Why this works

`ExUnit.start(exclude: [...])` establishes the default filter. `@tag`,
`@describetag`, and `@moduletag` attach metadata to the test struct; the
runner compares each test's tags against the include/exclude sets before
running it. CLI flags layer on top: `--include` unmasks an excluded tag,
`--only` inverts — run **only** the listed tag. This gives per-run control
without changing code.

---

## Benchmark

<!-- benchmark N/A: tema organizacional; el "benchmark" relevante es
"tiempo total del suite por modo", medible con `mix test --trace` y
comparando con/sin `--include integration`. -->

---

## Trade-offs and production gotchas

**1. `--only` hides typos silently**
`mix test --only slwo` runs zero tests and exits with status 0. Your CI
thinks everything passed. Pair `--only` with `--exit-on-first-failure`
and an expected minimum count if you depend on tag filtering in CI.

**2. `ExUnit.start(exclude: [...])` is global**
You configure it once in `test_helper.exs`. That means every developer on
the team sees the same default tag policy — good for consistency, but
someone running `mix test` locally might miss failing integration tests
they forgot to include.

**3. `@tag :skip` is not a comment**
Leaving dozens of `@tag :skip` around is how you end up with a dead test
suite. Require a linked issue in the comment and close the issue by
un-skipping or deleting the test.

**4. Tags do not propagate into child processes**
A tag is just metadata on the test struct. If your test spawns a GenServer
that logs something, the logger has no idea it's inside a `:slow` test.
Use `ExUnit.configure(capture_log: true)` or `ExUnit.CaptureLog`.

**5. When NOT to use tags**
If you're tempted to add a tag per module, you probably want two separate
`describe` blocks or two separate test files. Tags shine for cross-cutting
concerns (speed, externality), not organization.

---

## Reflection

- Your team adds `@tag :skip` to three tests in one week, each with a
  different ticket number. Design a CI rule (or a simple script) that
  warns when skipped tests accumulate beyond a threshold. What shape
  does the warning take?
- You inherit a 2 000-test suite with no tags and a 40-minute CI run.
  Where do you start adding tags, and how do you validate the split is
  correct before trusting CI with it?

---

## Resources

- [`ExUnit.Case` — tags and filtering](https://hexdocs.pm/ex_unit/ExUnit.Case.html#module-tags)
- [`ExUnit.configure/1`](https://hexdocs.pm/ex_unit/ExUnit.html#configure/1)
- [`mix test` task — CLI flags](https://hexdocs.pm/mix/Mix.Tasks.Test.html)


## Key Concepts

ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.
