# Linting Elixir with Credo

**Project**: `credo_setup` — a library with a real `.credo.exs`, a
disabled check, a priority-raised check, and a hand-written custom check
that fires on TODO comments without an author.

---

## Project context

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

Project structure:

```
credo_setup/
├── lib/
│   ├── credo_setup.ex
│   └── my_checks/
│       └── todo_with_author.ex
├── test/
│   └── credo_setup_test.exs
├── .credo.exs
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

### Step 1: Create the project

```bash
mix new credo_setup
cd credo_setup
```

Add Credo to `mix.exs`:

```elixir
defp deps do
  [
    {:credo, "~> 1.7", only: [:dev, :test], runtime: false}
  ]
end
```

Then:

```bash
mix deps.get
mix credo gen.config
```

This writes `.credo.exs` with every default — the best base to tweak from.

### Step 2: Edit `.credo.exs` — curated recommendations

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

```elixir
defmodule CredoSetupTest do
  use ExUnit.Case, async: true

  test "double/1 multiplies by two" do
    assert CredoSetup.double(2) == 4
    assert CredoSetup.double(-3) == -6
  end
end
```

### Step 6: Add a `mix check` alias

`mix.exs`:

```elixir
defp aliases do
  [
    check: ["format --check-formatted", "credo --strict", "test"]
  ]
end
```

### Step 7: Run

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

---

## Resources

- [Credo docs](https://hexdocs.pm/credo/overview.html)
- [Credo check reference](https://hexdocs.pm/credo/check_list.html) — every built-in check
- ["Writing a custom check"](https://hexdocs.pm/credo/custom_checks.html) — AST walkers, params, explanations
- [`Credo.Check`](https://hexdocs.pm/credo/Credo.Check.html) — the behaviour
- [`Credo.SourceFile`](https://hexdocs.pm/credo/Credo.SourceFile.html) — access to text and AST
