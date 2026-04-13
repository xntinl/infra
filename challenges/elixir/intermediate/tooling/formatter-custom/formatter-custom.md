# Customizing `mix format` with `.formatter.exs`

**Project**: `formatter_custom` — a project with a fine-tuned
`.formatter.exs`: custom `:locals_without_parens`, imported deps,
multiple file globs, and a (demo) formatter plugin.

---

## Why formatter custom matters

`mix format` is Elixir's built-in code formatter. It runs on every file
you save (if your editor is wired up), it runs on every commit (if you
add it to a pre-commit hook), and it runs on every CI build (if you
added `mix format --check-formatted` to your pipeline, which you should).

Out of the box the formatter does almost everything you want. The few
cases where it falls short — DSLs that use parens-free function calls,
imported deps that add their own DSLs, non-Elixir files like HEEx — are
handled by `.formatter.exs`.

This exercise teaches the four knobs you'll turn in practice:

1. `:inputs` — which file globs to format (the #1 mistake is omitting
   files).
2. `:locals_without_parens` — so your DSL isn't re-parenthesized.
3. `:import_deps` — so Ecto's `from`, Phoenix's `socket`, etc. are
   respected automatically.
4. `:plugins` — for HEEx, Surface, Markdown, or any non-.ex/.exs file.

---

## Project structure

```
formatter_custom/
├── lib/
│   └── formatter_custom.ex
├── script/
│   └── main.exs
├── test/
│   └── formatter_custom_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `.formatter.exs` is a plain keyword list

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 100,
  locals_without_parens: [
    given: 1, given: 2,
    check: 1, check: 2
  ],
  import_deps: [:ecto, :phoenix],
  plugins: [HEExFormatter]  # if you use phoenix_live_view
]
```

Every option is optional — the formatter has sensible defaults. You add a
key only when you need to deviate.

### 2. `:locals_without_parens` — the DSL escape hatch

Elixir's formatter always adds parens: `assert(x == 1)`. In test DSLs this
is noise — we want `assert x == 1`. Add `{:assert, 2}` to the list and
the formatter respects the parens-free style.

The list contains `{name, arity}` tuples. `:*` as arity means "any arity":

```elixir
locals_without_parens: [defcheck: :*]
```

### 3. `:import_deps` — inherit from dependencies

Many libraries ship a `.formatter.exs` with their own DSL entries. For
example, Ecto declares `from: 2, from: 3, ...` so `from u in User` doesn't
become `from(u in User)`. Add `import_deps: [:ecto]` and your formatter
picks up Ecto's config automatically.

Without this, you would manually copy 40+ entries into your config and
keep them in sync forever. Don't.

### 4. `:plugins` — extending the formatter to non-`.ex` files

Phoenix LiveView's HEEx templates are `.heex`. The base formatter has no
idea how to format them. `phoenix_live_view` ships `Phoenix.LiveView.HTMLFormatter`,
which you add to `:plugins`. Then `mix format` handles `.heex` too.

Plugins implement the `Mix.Tasks.Format` behaviour (two callbacks:
`features/1` and `format/2`).

---

## Why `mix format` and not Prettier-style configurability

The deliberate lack of options (no "indent: 4", no "single vs double
quotes") is the feature: every Elixir codebase looks the same, so
cross-project reviews and copy-paste work without visual noise.
Prettier's success proved the same point in JS. The place for stylistic
flexibility is Credo (suggestions), not the formatter (mechanical).

---

## Design decisions

**Option A — Accept the defaults and never customize `.formatter.exs`**
- Pros: Zero config; matches what `mix new` emits.
- Cons: Breaks on DSLs (your test-like macros get parenthesized);
  silently skips files outside `lib/` and `test/`; doesn't handle
  `.heex` or other non-`.ex` files.

**Option B — Curated `.formatter.exs` with `:inputs`, `:locals_without_parens`,
`:import_deps`, and `:plugins`** (chosen)
- Pros: The formatter covers every file in the repo; DSLs stay readable;
  third-party libraries pipe their own rules in via `:import_deps`;
  `.heex` and similar files get formatted too.
- Cons: More config surface to maintain; `:import_deps` requires the
  dep's `.formatter.exs` to be compiled before formatting works.

→ Chose **B** because the defaults only cover the trivial case (a plain
  library with no DSL); once you have Ecto, Phoenix, or a project DSL,
  customization is the cheapest way to preserve readable code.

---

## Implementation

### `mix.exs`

```elixir
defmodule FormatterCustom.MixProject do
  use Mix.Project

  def project do
    [
      app: :formatter_custom,
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
mix new formatter_custom
cd formatter_custom
```

### Step 2: A tiny DSL to demonstrate `:locals_without_parens`

**Objective**: Provide A tiny DSL to demonstrate `:locals_without_parens` — these are the supporting fixtures the main module depends on to make its concept demonstrable.

`lib/formatter_custom/dsl.ex`:

```elixir
defmodule FormatterCustom.Dsl do
  @moduledoc """
  A toy "spec" DSL. The macros are intentionally designed to read well
  WITHOUT parentheses — `given :input, 42` looks like prose, while
  `given(:input, 42)` looks like code. The formatter config makes this
  style possible.
  """

  defmacro __using__(_opts) do
    quote do
      import FormatterCustom.Dsl
      Module.register_attribute(__MODULE__, :examples, accumulate: true)
      @before_compile FormatterCustom.Dsl
    end
  end

  @doc """
  Records an example case: `given :input_name, value`.
  """
  defmacro given(name, value) do
    quote do
      @examples {unquote(name), unquote(value)}
    end
  end

  @doc """
  Records an expected value: `check :output_name, value`.
  """
  defmacro check(name, value) do
    quote do
      @examples {:check, unquote(name), unquote(value)}
    end
  end

  defmacro __before_compile__(_env) do
    quote do
      def __examples__, do: Enum.reverse(@examples)
    end
  end
end
```

### Step 3: A module using the DSL — the style you want preserved

**Objective**: Provide A module using the DSL — the style you want preserved — these are the supporting fixtures the main module depends on to make its concept demonstrable.

`lib/formatter_custom.ex`:

```elixir
defmodule FormatterCustom do
  @moduledoc """
  Uses `FormatterCustom.Dsl`. The lines `given :input, 1` stay un-parenthesized
  ONLY because `.formatter.exs` lists `given: 2` and `check: 2` under
  `:locals_without_parens`. Remove that entry and `mix format` will rewrite
  to `given(:input, 1)`.
  """
  use FormatterCustom.Dsl

  given :input, 1
  given :input, 2
  check :output, 42
end
```

### Step 4: The formatter configuration — `.formatter.exs`

**Objective**: Provide The formatter configuration — `.formatter.exs` — these are the supporting fixtures the main module depends on to make its concept demonstrable.

```elixir
[
  # 1) Which files to format. Omitting this means `mix format` ONLY touches
  #    files you pass on the command line — not what most people expect.
  inputs: [
    "{mix,.formatter}.exs",
    "{config,lib,test}/**/*.{ex,exs}"
  ],

  # 2) Preferred line width. 98 is Elixir stdlib's default; 100 is common.
  line_length: 100,

  # 3) Our DSL's macros — never add parens around them.
  locals_without_parens: [
    given: 2,
    check: 2
  ],

  # 4) Inherit formatting DSL entries from deps that declare them.
  #    Ecto, Phoenix, Phoenix LiveView all ship their own .formatter.exs.
  #    Uncomment when you actually depend on them:
  # import_deps: [:ecto, :phoenix, :phoenix_live_view],

  # 5) Plugins extend the formatter to non-Elixir files.
  #    Uncomment when you use LiveView's HEEx templates:
  # plugins: [Phoenix.LiveView.HTMLFormatter],

  # 6) By default, `subdirectories:` makes umbrellas work out of the box.
  subdirectories: ["apps/*"]
]
```

### Step 5: `test/dsl_test.exs`

**Objective**: Write `dsl_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule FormatterCustom.DslTest do
  use ExUnit.Case, async: true

  doctest FormatterCustom.Dsl

  describe "core functionality" do
    test "__examples__ accumulates givens and checks" do
      examples = FormatterCustom.__examples__()
      assert {:input, 1} in examples
      assert {:input, 2} in examples
      assert {:check, :output, 42} in examples
    end
  end
end
```

### Step 6: Run it — and then break it on purpose

**Objective**: Run it — and then break it on purpose.

```bash
mix test
mix format --check-formatted   # should succeed

# Now remove `given: 2, check: 2` from locals_without_parens. Run:
mix format
# Look at lib/formatter_custom.ex — the formatter rewrote:
#   given :input, 1   →   given(:input, 1)
# Put the entries back and re-format.
```

Wire formatting into CI:

```elixir
# mix.exs
defp aliases do
  [
    check: ["format --check-formatted", "credo --strict", "test"]
  ]
end
```

And into Git (optional, with `husky`, `lefthook`, or a plain
`.git/hooks/pre-commit`):

```sh
#!/bin/sh
mix format --check-formatted
```

### Why this works

`.formatter.exs` is evaluated by Mix at format time; the keyword list
is the whole API surface. `:locals_without_parens` tells the formatter
"these names are known parens-free calls, don't rewrite them" — which
is all a DSL needs to survive formatting. `:import_deps` delegates
upstream libraries' formatter configs so Ecto's `from/2`, Phoenix's
`socket/3`, and similar DSLs are respected without manual copying.
`:inputs` defines the world `mix format` sees — anything outside the
glob is silently skipped, which is why CI's `--check-formatted` is the
safety net.

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule FormatterCustom.Dsl do
    @moduledoc """
    A toy "spec" DSL. The macros are intentionally designed to read well
    WITHOUT parentheses — `given :input, 42` looks like prose, while
    `given(:input, 42)` looks like code. The formatter config makes this
    style possible.
    """

    defmacro __using__(_opts) do
      quote do
        import FormatterCustom.Dsl
        Module.register_attribute(__MODULE__, :examples, accumulate: true)
        @before_compile FormatterCustom.Dsl
      end
    end

    @doc """
    Records an example case: `given :input_name, value`.
    """
    defmacro given(name, value) do
      quote do
        @examples {unquote(name), unquote(value)}
      end
    end

    @doc """
    Records an expected value: `check :output_name, value`.
    """
    defmacro check(name, value) do
      quote do
        @examples {:check, unquote(name), unquote(value)}
      end
    end

    defmacro __before_compile__(_env) do
      quote do
        def __examples__, do: Enum.reverse(@examples)
      end
    end
  end

  def main do
    IO.puts("=== Formatter Demo ===
  ")
  
    # Demo: Custom code formatter
  IO.puts("1. mix format - auto-format code")
  IO.puts("2. .formatter.exs configuration")
  IO.puts("3. Custom formatting rules")

  IO.puts("
  ✓ Formatter demo completed!")
  end

end

Main.main()
```

## Benchmark

<!-- benchmark N/A: the formatter is I/O bound; wall time scales with
     file count. On a ~20k LOC codebase, `mix format` typically finishes
     in under 2 seconds. The metric that matters is pass/fail, not
     throughput. -->

---

## Trade-offs and production gotchas

**1. `:inputs` is the most common misconfiguration**
The default Mix-generated `.formatter.exs` only covers `lib/` and `test/`.
If you add `config/`, `priv/`, scripts, or an umbrella layout, your files
silently don't get formatted. When CI yells "unformatted", 9 times out of
10 the fix is an `:inputs` glob, not reformatting.

**2. `:line_length` is a SOFT limit**
The formatter tries to respect it but will not break things that can't be
broken (long string literals, long atom names). Don't expect every line to
fit — expect MOST to.

**3. `:locals_without_parens` is for YOUR DSL only**
If you add `{:from, 2}` to your config but don't use Ecto, you just
whitelisted a name that shouldn't be whitelisted. Prefer `:import_deps`
for third-party DSLs — they're maintained upstream.

**4. The formatter is opinionated and non-configurable**
You cannot disable "add space after comma" or "2-space indent". This is a
feature. If you want a configurable formatter, you want Credo, not
`mix format`. The opinionated nature is what makes the Elixir ecosystem
consistent.

**5. Editor integrations need a running Elixir**
`mix format` needs the project's deps compiled to resolve `:import_deps`.
If your editor plugin runs format-on-save before `deps.get`, it'll emit
warnings. Use `elixir-ls` or an LSP that knows about Mix.

**6. When NOT to customize the formatter**
- You're tempted to work around a Credo issue by reformatting — fix the
  code instead.
- You want to "turn off formatting in this file" — use `# credo:disable`
  for Credo, but `mix format` has no such escape hatch (except
  `#! formatter:skip-file`, which is rare and strongly discouraged).

---

## Reflection

- Your team ships a library with a public DSL (parens-free macros).
  Consumers don't know about your `.formatter.exs` and see `mix format`
  rewrite their code. How would you distribute the formatter rules so
  consumers inherit them automatically, and what's the boundary between
  your library's concerns and theirs?
- A new file type (say, `.sql` migrations) lands in the repo. Would you
  write a formatter plugin, use an external tool via a pre-commit hook,
  or leave it unformatted? What factors drive the decision (team size,
  edit frequency, available libraries)?

## Resources

- [`Mix.Tasks.Format`](https://hexdocs.pm/mix/Mix.Tasks.Format.html) — every option in `.formatter.exs` documented
- [`Code.format_string!/2`](https://hexdocs.pm/elixir/Code.html#format_string!/2) — the underlying function
- [Phoenix LiveView HTMLFormatter](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.HTMLFormatter.html) — the canonical plugin example
- [Elixir stdlib `.formatter.exs`](https://github.com/elixir-lang/elixir/blob/main/.formatter.exs) — real-world config
- [Writing a formatter plugin](https://hexdocs.pm/mix/Mix.Tasks.Format.html#module-plugins) — the behaviour you implement

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

### `lib/formatter_custom.ex`

```elixir
defmodule FormatterCustom do
  @moduledoc """
  Reference implementation for Customizing `mix format` with `.formatter.exs`.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the formatter_custom module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> FormatterCustom.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/formatter_custom_test.exs`

```elixir
defmodule FormatterCustomTest do
  use ExUnit.Case, async: true

  doctest FormatterCustom

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert FormatterCustom.run(:noop) == :ok
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
