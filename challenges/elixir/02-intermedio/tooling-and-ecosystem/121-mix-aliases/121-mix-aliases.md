# Composing tasks with Mix aliases

**Project**: `aliases_demo` — a project that shows how to chain, override,
and inline Mix aliases for CI-friendly composed commands.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

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

## Implementation

### Step 1: Create the project

```bash
mix new aliases_demo
cd aliases_demo
```

### Step 2: `mix.exs` — four representative aliases

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

```elixir
defmodule AliasesDemoTest do
  use ExUnit.Case, async: true

  test "app/0 returns the OTP app name" do
    assert AliasesDemo.app() == :aliases_demo
  end
end
```

### Step 5: Exercise the aliases

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

## Resources

- [`Mix.Project` aliases docs](https://hexdocs.pm/mix/Mix.Project.html#module-aliases) — the canonical reference
- [`mix cmd`](https://hexdocs.pm/mix/Mix.Tasks.Cmd.html) — running shell commands inside an alias
- [Elixir's own `mix.exs`](https://github.com/elixir-lang/elixir/blob/main/mix.exs) — real-world aliases at scale
- [Phoenix-generated `mix.exs` aliases](https://hexdocs.pm/phoenix/up_and_running.html) — the standard `setup` and `assets.*` aliases
