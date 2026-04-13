# Composing tasks with Mix aliases

**Project**: `aliases_demo` — a project that shows how to chain, override,
and inline Mix aliases for CI-friendly composed commands.

---

## Project context

Mix aliases are shortcuts defined in `mix.exs` under the `:aliases` key.
They let you compose multiple Mix tasks (and raw shell commands, and
inline functions) behind a single name. Every serious Elixir project
defines at least two: a `mix setup` that bootstraps the repo, and a
`mix check` that runs the full lint + test suite.

This exercise builds both, plus demonstrates the three less-obvious
alias features:

1. Overriding an existing task (`test: ["ecto.create --quiet", "test"]`).
2. Inline anonymous functions as alias steps.
3. Passing argv through to underlying tasks.

Project structure:

```
aliases_demo/
├── lib/
│   └── aliases_demo.ex
├── test/
│   └── aliases_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. An alias is a list of steps

Each step is one of:

```elixir
"compile"                              # a task name
"cmd echo hello"                       # shell command via `mix cmd`
&AliasesDemo.MixHelpers.banner/1       # an inline function receiving args
```

Mix runs them left to right, stopping on the first failure.

### 2. Override an existing task

If your alias name matches a built-in (`test`, `compile`, `deps.get`), your
alias **replaces** it. But you can still call the original by including
its name in the steps:

```elixir
aliases: [test: ["ecto.create --quiet", "ecto.migrate --quiet", "test"]]
```

The `"test"` inside the list refers to the ORIGINAL task, not the alias
itself (that would loop forever — Mix prevents it).

### 3. Argv forwarding

When a user runs `mix check --stale`, Mix appends `--stale` to the LAST
step of the alias — unless your alias steps have explicit args, in which
case nothing is forwarded. Rule of thumb: if you want to support flag
pass-through, leave the final step as a bare `"test"` or equivalent.

### 4. Inline functions receive `args`

```elixir
aliases: [greet: [&Mix.shell().info/1, "app.start"]]
```

A function reference as a step is called with the alias's argv. Useful for
printing banners, setting env vars, or branching based on arguments.

---

## Why aliases and not shell scripts

A `scripts/check.sh` wrapping `mix format && mix credo && mix test` works
but lives outside Mix: it has no argv pass-through, no ordered halt-on-
failure semantics tied to Mix tasks, and it can't call inline Elixir
functions. Aliases are first-class Mix citizens — editor integrations,
release tooling, and `mix help` all see them. A shell script is a black
box to everything above it.

---

## Design decisions

**Option A — One mega-alias (`all: [fmt, credo, dialyzer, test, docs]`)**
- Pros: Single entry point; "just run `mix all`".
- Cons: 10-minute runs block every commit; no way to skip the slow steps
  locally; CI and dev share the same command, which is rarely right.

**Option B — Two aliases: `setup` + `check`** (chosen)
- Pros: `setup` runs once on clone; `check` is the fast pre-push pipeline.
  Slow steps (dialyzer, full docs) get their own named alias that CI
  calls explicitly.
- Cons: More alias names to remember; requires documenting the intent of
  each in `mix.exs`.

→ Chose **B** because "one command to rule them all" always ends up too
  slow to run before every commit, and once that happens nobody runs it.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {credo},
    {exunit},
    {mix},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new aliases_demo
cd aliases_demo
```

### Step 2: `mix.exs` — four representative aliases

**Objective**: Edit `mix.exs` — four representative aliases, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.


```elixir
defmodule AliasesDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :aliases_demo,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: deps(),
      aliases: aliases()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      # tooling-only; gives `mix credo` and `mix format` consistency
      {:credo, "~> 1.7", only: [:dev, :test], runtime: false}
    ]
  end

  defp aliases do
    [
      # 1) Bootstrap the repo: deps + compile. Run on fresh clones / CI.
      setup: ["deps.get", "compile"],

      # 2) Composed lint/test command. The final "test" receives any flags
      #    the user passes — `mix check --stale` → `mix test --stale`.
      check: [
        "format --check-formatted",
        "credo --strict",
        "test"
      ],

      # 3) Override: extend the built-in `test` task with a banner first.
      #    Inside the list, "test" refers to the ORIGINAL Mix test task.
      test: [&banner/1, "test"],

      # 4) Shell out — useful for non-Mix steps (docker, curl, …).
      "ci.docs": ["docs", "cmd tar -czf docs.tar.gz doc/"]
    ]
  end

  # Called with the argv of the `test` alias. Returns :ok so Mix continues.
  defp banner(_args) do
    Mix.shell().info("══ aliases_demo test suite ══")
    :ok
  end
end
```

### Step 3: Trivial `lib/aliases_demo.ex`

**Objective**: Trivial `lib/aliases_demo.ex`.


```elixir
defmodule AliasesDemo do
  @moduledoc """
  This module has no interesting behavior — the exercise is about `mix.exs`.
  """

  @doc "Returns the project's OTP app name."
  @spec app() :: atom()
  def app, do: :aliases_demo
end
```

### Step 4: `test/aliases_demo_test.exs`

**Objective**: Write `aliases_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule AliasesDemoTest do
  use ExUnit.Case, async: true

  test "app/0 returns the OTP app name" do
    assert AliasesDemo.app() == :aliases_demo
  end
end
```

### Step 5: Exercise the aliases

**Objective**: Exercise the aliases.


```bash
# Fresh bootstrap
mix setup

# Run the banner + test
mix test

# Run the full check suite (will also run Credo and format-check)
mix check

# Forward an argument to the final step:
mix check --stale   # -> `mix test --stale` internally

# Build docs + tar them
mix ci.docs
```

Open `mix.exs` and tweak the alias order / steps, then re-run to see how
Mix halts on the first failure.

### Why this works

Each alias is a plain list of steps Mix executes left-to-right, halting
on the first non-zero return. Overriding `test` preserves the original
task because Mix resolves `"test"` inside the list to the built-in, not
the alias. Argv forwarding targets the last step, so keeping `"test"`
bare at the end lets users run `mix check --stale` without code changes.

---

## Benchmark

<!-- benchmark N/A: alias dispatch is compile-time composition; overhead
     is a constant microsecond-scale function call before the first real
     task runs. Wall time is dominated by the tasks themselves. -->

---

## Trade-offs and production gotchas

**1. Argv forwarding is subtle**
Mix appends CLI args to the LAST step only, and only when that step has
no explicit args in `mix.exs`. If you write `test: ["test --trace"]`,
`mix test --stale` does NOT append `--stale` — `--trace` is already there.
Either parameterize with a function or split into two aliases.

**2. Overriding `test` can break tooling**
`mix test` is called by editors, CI, and VS Code extensions. If your alias
adds slow steps (a DB reset on every run), your dev loop slows down and
your IDE probably doesn't pass the flags you expect. Keep `mix test`
lightweight; use `mix check` for the heavy suite.

**3. Shell commands bypass Mix**
`"cmd docker build ..."` runs a raw shell, subject to the user's `$PATH`
and OS. On Windows the command may not even exist. Prefer pure-Mix steps
when cross-platform portability matters.

**4. Aliases are not available in releases**
`mix.exs` is not part of a release — `bin/my_app alias check` does not
exist. Aliases are a build/CI tool, not a runtime one. For in-release
operations, write a release module (`lib/my_app/release.ex`).

**5. Alias recursion is forbidden**
`aliases: [foo: ["foo"]]` is rejected by Mix (with a clear error). But
`aliases: [foo: ["bar"], bar: ["foo"]]` loops — Mix catches this with
"cyclic alias". Keep the graph shallow.

**6. When NOT to use an alias**
- The composition is a single task — just call the task directly.
- The steps include branching logic — write a Mix task instead (easier
  to test, has proper argv parsing).
- You need conditional execution based on env vars — a task with
  `if System.get_env/1` is clearer than four aliases for four envs.

---

## Reflection

- Your CI takes 12 minutes: `mix check` runs format, credo, dialyzer,
  test, and a coverage report. Developers complain the pre-commit hook
  is too slow, so they start skipping it. How do you split the alias to
  keep CI thorough while giving devs a sub-30-second local check?
- A teammate writes `aliases: [release: [&build_assets/1, "phx.gen.release"]]`
  where `build_assets/1` shells out to `npm run build`. A year later the
  npm step fails silently in one CI runner because `$PATH` differs. What
  refactor would make this failure loud and portable?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule AliasesDemo do
    @moduledoc """
    This module has no interesting behavior — the exercise is about `mix.exs`.
    """

    @doc "Returns the project's OTP app name."
    @spec app() :: atom()
    def app, do: :aliases_demo
  end

  def main do
    IO.puts("=== Aliases Demo ===
  ")
  
    # Demo: Mix aliases
  IO.puts("1. alias test: 'test --cover --trace'")
  IO.puts("2. Shortcuts for common workflows")
  IO.puts("3. Defined in mix.exs")

  IO.puts("
  ✓ Mix aliases demo completed!")
  end

end

Main.main()
```


## Resources

- [`Mix.Project` aliases docs](https://hexdocs.pm/mix/Mix.Project.html#module-aliases) — the canonical reference
- [`mix cmd`](https://hexdocs.pm/mix/Mix.Tasks.Cmd.html) — running shell commands inside an alias
- [Elixir's own `mix.exs`](https://github.com/elixir-lang/elixir/blob/main/mix.exs) — real-world aliases at scale
- [Phoenix-generated `mix.exs` aliases](https://hexdocs.pm/phoenix/up_and_running.html) — the standard `setup` and `assets.*` aliases


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
