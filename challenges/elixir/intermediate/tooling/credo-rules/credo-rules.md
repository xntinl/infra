# Linting Elixir with Credo

**Project**: `credo_setup` — a library with a real `.credo.exs`, a
disabled check, a priority-raised check, and a hand-written custom check
that fires on TODO comments without an author.

---

## Why credo rules matters

Credo is the de-facto Elixir linter. It reads your code, checks it against
~70 rules grouped into Readability, Refactoring, Warnings, Consistency,
and Design, and reports issues. Unlike the formatter (mechanical, opinionated,
non-negotiable), Credo is a *set of suggestions* you calibrate to your team.

Every Elixir codebase that lives past a month should run Credo in CI.
This exercise shows you:

1. How to install and scaffold a config with `mix credo gen.config`.
2. How to edit `.credo.exs` to disable, enable, or re-prioritize checks.
3. How to write a custom check — a mini example that flags `# TODO:`
   without a name attached.
4. How to plug Credo into a `mix check` alias that runs in CI.

---

## Project structure

```
credo_setup/
├── lib/
│   └── credo_setup.ex
├── script/
│   └── main.exs
├── test/
│   └── credo_setup_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `.credo.exs` is the single source of truth

```elixir
%{
  configs: [
    %{
      name: "default",
      files: %{included: ["lib/", "test/"], excluded: []},
      strict: true,
      checks: [
        {Credo.Check.Readability.ModuleDoc, false},
        {Credo.Check.Refactor.CyclomaticComplexity, max_complexity: 15},
        {MyChecks.TodoWithAuthor, []}
      ]
    }
  ]
}
```

Each check is either `{Check, opts}` (enabled) or `{Check, false}`
(disabled). Omitted checks use their defaults — Credo warns you when
the config is missing checks it knows about.

### 2. Priorities: `:high`, `:normal`, `:low`, `:ignore`

A project-wide `--min-priority higher` only shows `:high` items, which is
useful in CI to enforce ONLY things you've agreed to block on. Per-check
priority is set with `priority: :high` in the options list.

### 3. `--strict` mode

`mix credo --strict` lowers the bar so even `:low`-priority issues are
reported. Default `mix credo` shows `:normal` and above. In CI, run
`--strict` and `--format oneline` (grep-able output).

### 4. Custom checks — a regular Elixir module

```elixir
defmodule MyChecks.Whatever do
  use Credo.Check, category: :readability, base_priority: :normal

  def run(%SourceFile{} = source, params) do
    # ... return a list of Credo.Issue ...
  end
end
```

`Credo.Check` gives you `format_issue/2` and the `params` plumbing. The
`SourceFile` struct exposes the AST (`Credo.Code.ast/1`) and the raw
text (`SourceFile.source/1`).

---

## Why Credo and not only `mix format` + Dialyzer

The three tools cover different layers. `mix format` is mechanical and
non-negotiable (whitespace, parens). Dialyzer is type-level (success
typing). Credo sits in between: convention and smell detection — things
that are technically correct but violate the team's agreed style
(cyclomatic complexity, stray `IO.inspect`, unused aliases). Drop any
one of the three and you leak that layer into code review.

---

## Design decisions

**Option A — Adopt Credo's defaults verbatim**
- Pros: Zero config; fastest to get running; someone else curated the
  rules.
- Cons: Credo defaults reflect one team's taste (e.g. single-pipe
  warnings); noise ratio is often too high for legacy codebases.

**Option B — Curated `.credo.exs` with disabled/raised checks** (chosen)
- Pros: Every rule is there on purpose — the file is a record of team
  decisions; CI runs `--strict` and blocks on real issues; custom
  checks encode project-specific conventions.
- Cons: The config needs maintenance; adding a new check requires a
  discussion; `requires:` wiring for in-project custom checks is
  another moving part.

→ Chose **B** because a linter whose warnings people ignore is worse
  than no linter — once the ratio of "real issues : noise" drops, the
  signal is lost. Curation keeps it actionable.

---

## Implementation

### `mix.exs`

```elixir
defmodule CredoSetup.MixProject do
  use Mix.Project

  def project do
    [
      app: :credo_setup,
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

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new credo_setup
cd credo_setup
```

Add Credo to `mix.exs`:

Then:

```bash
mix deps.get
mix credo gen.config
```

This writes `.credo.exs` with every default — the best base to tweak from.

### Step 2: Edit `.credo.exs` — curated recommendations

**Objective**: Edit `.credo.exs` — curated recommendations, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.

Keep the generated structure; adjust the `checks:` section like this
(partial; keep the other checks as generated):

```elixir
%{
  configs: [
    %{
      name: "default",
      files: %{
        included: ["lib/", "src/", "test/", "web/", "apps/*/lib/", "apps/*/test/"],
        excluded: [~r"/_build/", ~r"/deps/", ~r"/node_modules/"]
      },
      plugins: [],
      requires: ["lib/my_checks/todo_with_author.ex"],
      strict: true,
      color: true,
      checks: [
        # --- Keep all generated defaults; below are our project tweaks. ---

        # Our codebase uses Markdown freely in docs, so TrailingBlankLine in
        # moduledocs is noise. Disable with `false`.
        {Credo.Check.Readability.TrailingBlankLine, false},

        # We take complexity seriously. Raise priority and tighten threshold.
        {Credo.Check.Refactor.CyclomaticComplexity, priority: :high, max_complexity: 10},

        # Fail CI on any stray IO.inspect / dbg left behind.
        {Credo.Check.Warning.IoInspect, priority: :high},

        # Plug in our custom check.
        {MyChecks.TodoWithAuthor, []}
      ]
    }
  ]
}
```

### Step 3: Write the custom check — `lib/my_checks/todo_with_author.ex`

**Objective**: Write the custom check — `lib/my_checks/todo_with_author.ex`.

```elixir
defmodule MyChecks.TodoWithAuthor do
  @moduledoc """
  Flags `# TODO:` comments that don't include an author tag.

  Good:   `# TODO(alice): refactor this when we move to Ecto 3.12`
  Bad:    `# TODO: refactor`

  Forcing an author makes TODOs assignable and searchable.
  """
  use Credo.Check,
    category: :readability,
    base_priority: :normal,
    explanations: [
      check: """
      A TODO comment without an author ends up anonymous. Add a
      parenthesized handle so the TODO is owned:

          # TODO(alice): lorem ipsum
      """
    ]

  @pattern ~r/#\s*TODO(?!\()/   # matches "# TODO" NOT followed by "("

  @impl true
  def run(%Credo.SourceFile{} = source_file, params \\ []) do
    issue_meta = IssueMeta.for(source_file, params)

    source_file
    |> Credo.SourceFile.source()
    |> String.split("\n")
    |> Enum.with_index(1)
    |> Enum.flat_map(fn {line, line_no} ->
      if Regex.match?(@pattern, line) do
        [issue_for(issue_meta, line_no, line)]
      else
        []
      end
    end)
  end

  defp issue_for(issue_meta, line_no, line) do
    format_issue(issue_meta,
      message: "TODO without author. Use `# TODO(name): ...`.",
      line_no: line_no,
      trigger: String.trim(line)
    )
  end
end
```

### Step 4: Deliberately add a "bad" line — `lib/credo_setup.ex`

**Objective**: Deliberately add a "bad" line — `lib/credo_setup.ex`.

```elixir
defmodule CredoSetup do
  @moduledoc """
  Entry point. Try running `mix credo` and watch both default checks AND
  our custom `TodoWithAuthor` check fire.
  """

  # TODO: write a real implementation         ← will be flagged by our custom check
  # TODO(alice): improve this later           ← will NOT be flagged

  @doc "Trivial function. Doubles the input."
  @spec double(number()) :: number()
  def double(n) when is_number(n), do: n * 2
end
```

### Step 5: `test/credo_setup_test.exs`

**Objective**: Write `credo_setup_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule CredoSetupTest do
  use ExUnit.Case, async: true

  doctest CredoSetup

  describe "core functionality" do
    test "double/1 multiplies by two" do
      assert CredoSetup.double(2) == 4
      assert CredoSetup.double(-3) == -6
    end
  end
end
```

### Step 6: Add a `mix check` alias

**Objective**: Add a `mix check` alias.

`mix.exs`:

```elixir
defp aliases do
  [
    check: ["format --check-formatted", "credo --strict", "test"]
  ]
end
```

### Step 7: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix deps.get
mix test
mix credo            # default priority
mix credo --strict   # everything, including :low
mix check            # what CI should run
```

You should see an issue pointing to the unnamed `TODO:` in
`lib/credo_setup.ex`. Fix it with a name — the issue disappears.

### Why this works

`.credo.exs` is read once per run; the `checks:` list is the single
source of truth (enabled, disabled, re-prioritized, custom). The
custom check is a plain Elixir module implementing the `Credo.Check`
behaviour — Credo compiles it via `requires:` and calls `run/2` with
the source file, expecting a list of `Credo.Issue`. Raw-text matching
works here because the TODO pattern is purely lexical; AST-level checks
use `Credo.Code.prewalk/2` for the same plumbing.

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule MyChecks.TodoWithAuthor do
    @moduledoc """
    Flags `# TODO:` comments that don't include an author tag.

    Good:   `# TODO(alice): refactor this when we move to Ecto 3.12`
    Bad:    `# TODO: refactor`

    Forcing an author makes TODOs assignable and searchable.
    """
    use Credo.Check,
      category: :readability,
      base_priority: :normal,
      explanations: [
        check: """
        A TODO comment without an author ends up anonymous. Add a
        parenthesized handle so the TODO is owned:

            # TODO(alice): lorem ipsum
        """
      ]

    @pattern ~r/#\s*TODO(?!\()/   # matches "# TODO" NOT followed by "("

    @impl true
    def run(%Credo.SourceFile{} = source_file, params \\ []) do
      issue_meta = IssueMeta.for(source_file, params)

      source_file
      |> Credo.SourceFile.source()
      |> String.split("\n")
      |> Enum.with_index(1)
      |> Enum.flat_map(fn {line, line_no} ->
        if Regex.match?(@pattern, line) do
          [issue_for(issue_meta, line_no, line)]
        else
          []
        end
      end)
    end

    defp issue_for(issue_meta, line_no, line) do
      format_issue(issue_meta,
        message: "TODO without author. Use `# TODO(name): ...`.",
        line_no: line_no,
        trigger: String.trim(line)
      )
    end
  end

  def main do
    IO.puts("=== Credo Demo ===
  ")
  
    # Demo: Credo code analysis
  IO.puts("1. mix credo - code style analysis")
  IO.puts("2. Custom rules configuration")
  IO.puts("3. Guides code quality")

  IO.puts("
  ✓ Credo demo completed!")
  end

end

Main.main()
```

## Benchmark

<!-- benchmark N/A: Credo's runtime is dominated by file I/O and AST
     parsing; each check contributes microseconds per file. On a medium
     codebase (~20k LOC), `mix credo --strict` typically runs in under
     3 seconds — well below the threshold where it would slow down CI. -->

---

## Trade-offs and production gotchas

**1. Credo is opinionated — calibrate, don't adopt blindly**
Credo's defaults reflect one team's taste. Some rules (e.g. single-pipe
discouragement) won't fit your project. Disable with `{Check, false}` and
write a comment in `.credo.exs` explaining WHY — future you will thank you.

**2. `requires:` in `.credo.exs` is necessary for custom checks in-project**
If your custom check lives in `lib/my_checks/` inside the same project,
Credo doesn't know to compile it before running. List it in `requires:`.
Checks distributed as packages don't need this.

**3. `Credo.Check.Warning.IoInspect` and `Credo.Check.Warning.Dbg`**
Raise these to `:high` in every project. They catch stray `IO.inspect`
and `dbg` calls — the #1 source of noisy production logs from
Elixir. Make them CI-breaking.

**4. Credo is NOT the formatter**
`mix format` is the formatter, and it is enforced separately. Credo
doesn't reformat code — only points out issues. Some Credo checks (e.g.
`Credo.Check.Readability.AliasOrder`) overlap with formatter behavior;
disable the duplicates to avoid noise.

**5. Custom checks can hit the AST or the raw source**
Our example uses raw text (fine for line-matching), but real checks
usually walk the AST via `Credo.Code.prewalk/2`. Raw text breaks when
someone writes the pattern inside a string literal — AST is more robust
for anything beyond line-level rules.

**6. When NOT to add a check**
- The problem is a type error — Dialyzer is for that, not Credo.
- The rule is "this pattern is forbidden" — Credo is suggestion-level;
  for hard constraints, use a compile-time macro or a custom Mix task
  that fails the build.
- The rule fires on less than 5% of your codebase — it's probably noise,
  not a convention.

---

## Reflection

- Your team adopts a new convention: every public function must have a
  `@spec`. Would you enforce it via a Credo check, a compile-time macro,
  or Dialyzer? What are the tradeoffs in false-positive rate, developer
  friction, and CI cost?
- A legacy codebase you inherit emits 1,200 Credo issues at
  `--strict`. Going through all of them is unrealistic. What strategy
  would you use to surface the bleeding — high-priority checks, scoped
  `files:` globs, ratchet-up-over-time — and why?

## Resources

- [Credo docs](https://hexdocs.pm/credo/overview.html)
- [Credo check reference](https://hexdocs.pm/credo/check_list.html) — every built-in check
- ["Writing a custom check"](https://hexdocs.pm/credo/custom_checks.html) — AST walkers, params, explanations
- [`Credo.Check`](https://hexdocs.pm/credo/Credo.Check.html) — the behaviour
- [`Credo.SourceFile`](https://hexdocs.pm/credo/Credo.SourceFile.html) — access to text and AST

## Deep Dive

Elixir's tooling ecosystem extends beyond the language into DevOps, profiling, and observability. Understanding each tool's role prevents misuse and false optimizations.

**Mix tasks and releases:**
Custom mix tasks (`mix myapp.setup`, `mix myapp.migrate`) encapsulate operational knowledge. Tasks run in the host environment (not the compiled app), so they're ideal for setup, teardown, or scripting. Releases, built with `mix release`, create self-contained OTP applications deployable without Elixir installed. They're immutable: no source code changes after release — all config comes from environment variables or runtime files.

**Debugging and profiling tools:**
- `:observer` (GUI): real-time process tree, metrics, and port inspection
- `Recon`: production-safe introspection (stable even under high load)
- `:eprof`: function-level timing; lower overhead than `:fprof`
- `:fprof`: detailed trace analysis; use only in staging

**Profiling approaches:**
Ceiling profiling (e.g., "which modules consume CPU?") is cheap; go there first with `perf` or `eprof`. Floor profiling (e.g., "which lines in this function are slow?") is expensive; reserve for specific functions. In production, prefer metrics (Prometheus, New Relic) over profiling — continuous profiling has overhead. Store profiling data for post-mortem analysis, not real-time dashboards.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `lib/credo_setup.ex`

```elixir
defmodule CredoSetup do
  @moduledoc """
  Reference implementation for Linting Elixir with Credo.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the credo_setup module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> CredoSetup.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/credo_setup_test.exs`

```elixir
defmodule CredoSetupTest do
  use ExUnit.Case, async: true

  doctest CredoSetup

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert CredoSetup.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
