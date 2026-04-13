# AST Walker for a Custom Linter

**Project**: `ast_walker` — build a custom Credo-style linter that reads `.ex` files, walks their AST, and emits warnings for patterns you care about (banned modules, missing `@moduledoc`, deprecated functions).

---

## The business problem

Your engineering org has conventions that Credo does not enforce out of the box:

1. Every public module must have `@moduledoc` (not `false`, not missing).
2. `IO.inspect/2` in non-test code is a merge blocker (debugging leftover).
3. `:crypto.rand_bytes/1` is deprecated — use `:crypto.strong_rand_bytes/1`.

You write a small internal tool, `AstWalker.Linter`, that Credo-style reads every
`.ex` file, calls `Code.string_to_quoted/2` to get the AST, and walks it looking
for the violations above. Output mimics Credo's format.

## Project structure

```
ast_walker/
├── lib/
│   └── ast_walker/
│       ├── linter.ex           # entry point — lints files and aggregates issues
│       ├── rules.ex            # individual lint rules
│       └── issue.ex            # struct for findings
├── test/
│   └── linter_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why AST traversal and not regex

Source text is a lossy view of Elixir. The AST has the information the compiler uses — operator associativity, special forms, metadata. Working at the AST level means the transform stays correct as syntax evolves.

---

## Design decisions

**Option A — regex over source text**
- Pros: simple; no compile-time plumbing.
- Cons: breaks on any non-trivial shape; cannot respect scoping; easy to corrupt.

**Option B — `Macro.prewalk/postwalk` traversal** (chosen)
- Pros: operates on the real tree; respects quoting, scope, and special forms.
- Cons: must understand Elixir AST shapes; larger mental model.

→ Chose **B** because any production-worthy transform eventually hits a case regex cannot handle; start with the real structure.

---

## Implementation

### `lib/ast_walker.ex`

```elixir
defmodule AstWalker do
  @moduledoc """
  AST Walker for a Custom Linter.

  Source text is a lossy view of Elixir. The AST has the information the compiler uses — operator associativity, special forms, metadata. Working at the AST level means the....
  """
end
```
### `lib/ast_walker/issue.ex`

**Objective**: Define Issue struct to model lint findings with file, line, column, rule, severity for formatted output.

### `mix.exs`
```elixir
defmodule AstWalker.MixProject do
  use Mix.Project

  def project do
    [
      app: :ast_walker,
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
    [# No external dependencies — pure Elixir]
  end
end
```
```elixir
defmodule AstWalker.Issue do
  @moduledoc false

  @type t :: %__MODULE__{
          file: String.t(),
          line: non_neg_integer(),
          column: non_neg_integer() | nil,
          rule: atom(),
          severity: :info | :warning | :error,
          message: String.t()
        }

  defstruct [:file, :line, :column, :rule, :severity, :message]

  @spec format(t()) :: String.t()
  def format(%__MODULE__{} = issue) do
    sev =
      case issue.severity do
        :info -> "[I]"
        :warning -> "[W]"
        :error -> "[E]"
      end

    col = if issue.column, do: ":#{issue.column}", else: ""
    "#{sev} #{issue.file}:#{issue.line}#{col}  #{issue.rule}  #{issue.message}"
  end
end
```
### `lib/ast_walker/rules.ex`

**Objective**: Implement check_deprecated/2, check_debug_calls/2, check_moduledoc/2 using Macro.prewalk to traverse AST.

```elixir
defmodule AstWalker.Rules do
  @moduledoc "Individual lint rule implementations."

  alias AstWalker.Issue

  @banned_calls [
    {{:crypto, :rand_bytes}, ":crypto.rand_bytes/1 is deprecated, use strong_rand_bytes/1"}
  ]

  @debug_calls [
    {{IO, :inspect}, "IO.inspect/2 left in non-test source"}
  ]

  @spec check_deprecated(Macro.t(), String.t()) :: [Issue.t()]
  def check_deprecated(ast, file) do
    collect_remote_calls(ast, file, @banned_calls, :deprecated_function, :warning)
  end

  @spec check_debug_calls(Macro.t(), String.t()) :: [Issue.t()]
  def check_debug_calls(ast, file) do
    collect_remote_calls(ast, file, @debug_calls, :debug_call_left, :warning)
  end

  @spec check_moduledoc(Macro.t(), String.t()) :: [Issue.t()]
  def check_moduledoc({:defmodule, meta, [_alias, [do: body]]}, file) do
    if has_moduledoc?(body) do
      []
    else
      [
        %Issue{
          file: file,
          line: Keyword.get(meta, :line, 1),
          column: Keyword.get(meta, :column),
          rule: :missing_moduledoc,
          severity: :warning,
          message: "module missing @moduledoc"
        }
      ]
    end
  end

  def check_moduledoc(_, _), do: []

  defp has_moduledoc?({:__block__, _, stmts}), do: Enum.any?(stmts, &moduledoc_node?/1)
  defp has_moduledoc?(single), do: moduledoc_node?(single)

  defp moduledoc_node?({:@, _, [{:moduledoc, _, [content]}]}) when content != false, do: true
  defp moduledoc_node?(_), do: false

  defp collect_remote_calls(ast, file, list, rule, severity) do
    {_ast, acc} =
      Macro.prewalk(ast, [], fn node, acc ->
        case match_remote(node, list) do
          {:match, msg, meta} ->
            issue = %Issue{
              file: file,
              line: Keyword.get(meta, :line, 0),
              column: Keyword.get(meta, :column),
              rule: rule,
              severity: severity,
              message: msg
            }

            {node, [issue | acc]}

          :nomatch ->
            {node, acc}
        end
      end)

    Enum.reverse(acc)
  end

  defp match_remote({{:., _, [{:__aliases__, _, parts}, fun]}, meta, _args}, list) do
    check_tuple({Module.concat(parts), fun}, meta, list)
  end

  defp match_remote({{:., _, [mod, fun]}, meta, _args}, list) when is_atom(mod) do
    check_tuple({mod, fun}, meta, list)
  end

  defp match_remote(_, _), do: :nomatch

  defp check_tuple({mod, fun}, meta, list) do
    case Enum.find(list, fn {{m, f}, _} -> m == mod and f == fun end) do
      {_, msg} -> {:match, msg, meta}
      nil -> :nomatch
    end
  end
end
```
### `lib/ast_walker/linter.ex`

**Objective**: Parse source files via Code.string_to_quoted, run rules, aggregate and sort issues by file/line/column.

```elixir
defmodule AstWalker.Linter do
  @moduledoc "Lints a list of files, aggregating issues from all rules."

  alias AstWalker.{Issue, Rules}

  @spec lint_file(String.t()) :: [Issue.t()]
  def lint_file(path) do
    source = File.read!(path)

    case Code.string_to_quoted(source, columns: true) do
      {:ok, ast} -> run_rules(ast, path)
      {:error, _} -> [parse_error(path)]
    end
  end

  @spec lint_files([String.t()]) :: [Issue.t()]
  def lint_files(paths) do
    paths
    |> Enum.flat_map(&lint_file/1)
    |> Enum.sort_by(&{&1.file, &1.line, &1.column})
  end

  @spec run_rules(Macro.t(), String.t()) :: [Issue.t()]
  def run_rules(ast, file) do
    moduledoc_issues =
      ast
      |> collect_modules()
      |> Enum.flat_map(&Rules.check_moduledoc(&1, file))

    Rules.check_deprecated(ast, file) ++
      Rules.check_debug_calls(ast, file) ++
      moduledoc_issues
  end

  defp collect_modules(ast) do
    {_, acc} =
      Macro.prewalk(ast, [], fn
        {:defmodule, _, _} = node, acc -> {node, [node | acc]}
        other, acc -> {other, acc}
      end)

    Enum.reverse(acc)
  end

  defp parse_error(path) do
    %Issue{
      file: path,
      line: 0,
      column: nil,
      rule: :parse_error,
      severity: :error,
      message: "file did not parse"
    }
  end
end
```
### `test/ast_walker_test.exs`

**Objective**: Assert missing @moduledoc detected, deprecated functions flagged, clean code passes, formatting is stable.

```elixir
defmodule AstWalker.LinterTest do
  use ExUnit.Case, async: true
  doctest AstWalker.Linter

  alias AstWalker.{Linter, Issue}

  describe "run_rules/2" do
    test "flags missing @moduledoc" do
      ast = quote do: (defmodule Foo, do: (def x, do: 1))
      issues = Linter.run_rules(ast, "x.ex")
      assert Enum.any?(issues, &(&1.rule == :missing_moduledoc))
    end

    test "accepts @moduledoc string" do
      ast =
        quote do
          defmodule Foo do
            @moduledoc "ok"
            def x, do: 1
          end
        end

      issues = Linter.run_rules(ast, "x.ex")
      refute Enum.any?(issues, &(&1.rule == :missing_moduledoc))
    end

    test "rejects @moduledoc false" do
      ast =
        quote do
          defmodule Foo do
            @moduledoc false
            def x, do: 1
          end
        end

      issues = Linter.run_rules(ast, "x.ex")
      assert Enum.any?(issues, &(&1.rule == :missing_moduledoc))
    end

    test "flags :crypto.rand_bytes/1" do
      ast = quote do: :crypto.rand_bytes(16)
      issues = Linter.run_rules(ast, "x.ex")
      assert Enum.any?(issues, &(&1.rule == :deprecated_function))
    end

    test "flags IO.inspect/2" do
      ast = quote do: IO.inspect("debug")
      issues = Linter.run_rules(ast, "x.ex")
      assert Enum.any?(issues, &(&1.rule == :debug_call_left))
    end

    test "does not flag Enum.map — clean code passes" do
      ast = quote do: Enum.map([1, 2, 3], fn x -> x * 2 end)
      issues = Linter.run_rules(ast, "x.ex")
      assert issues == []
    end
  end

  describe "Issue.format/1" do
    test "formats with severity marker" do
      issue = %Issue{
        file: "lib/a.ex",
        line: 10,
        column: 3,
        rule: :missing_moduledoc,
        severity: :warning,
        message: "bad"
      }

      assert Issue.format(issue) == "[W] lib/a.ex:10:3  missing_moduledoc  bad"
    end
  end
end
```
### Why this works

`Macro.prewalk/2` visits parent then children, `Macro.postwalk/2` visits children then parent, both threading an accumulator. Picking the right direction controls whether a node can see its already-rewritten subtree. Returning the node unchanged skips it; returning a replacement grafts it in.

---

## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---

## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

---

## Trade-offs and production gotchas

**1. `quote do: (defmodule Foo, do: ...)` vs source strings.** The `quote` in tests
uses a different AST shape than `Code.string_to_quoted/2` does for `defmodule Foo
do ... end`. Prefer `string_to_quoted/2` in production and tests that simulate real
user input.

**2. Macro expansion is off by default.** `Code.string_to_quoted/2` returns the
RAW AST. `with` and `|>` look like 3-tuples, not expanded clauses. Decide early
whether to `Macro.expand/2` before linting.

**3. Line/column metadata may be missing.** Not every AST node has `:line`. Default
to 0 and warn in output rather than crash.

**4. Aliases and remote calls.** `alias MyApp.Helper, as: H; H.call()` appears as
`{:__aliases__, _, [:H]}`. To match the real module, use `Macro.Env` during
expansion — or run the linter post-expansion.

**5. Rule composition order.** Rules that return lists of issues must not mutate
the AST. Stick to `prewalk/3` with accumulators, never `prewalk/2`.

**6. False positives in tests and scripts.** `IO.inspect/2` is legitimate in
`.exs` and `test/`. Filter files by path before linting.

**7. Performance on large repos.** Linting 5000 files sequentially takes minutes.
Use `Task.async_stream/3` with `max_concurrency: System.schedulers_online()`.

**8. When NOT to build your own linter.** Credo supports custom checks via
`Credo.Check`. If your rules fit the Credo model (most do), use it and write a
plugin. Build your own only for deeply custom traversal needs.

---

## Benchmark

```elixir
# bench/linter_bench.exs
paths = Path.wildcard("lib/**/*.ex")

Benchee.run(%{
  "lint 50 files sequential" => fn -> AstWalker.Linter.lint_files(Enum.take(paths, 50)) end,
  "lint 50 files parallel"   => fn ->
    paths
    |> Enum.take(50)
    |> Task.async_stream(&AstWalker.Linter.lint_file/1, max_concurrency: System.schedulers_online())
    |> Enum.to_list()
  end
})
```
Expect parallel to be 4–8× faster on modern hardware.

---

## Reflection

- A transform needs to rewrite only variables bound in an outer `fn`. Which walker direction do you pick, and how do you track scope in the accumulator?
- If your walker emits valid AST that the compiler later rejects, where is the bug — in your rules or in your pattern matching? How do you bisect it?

---

### `script/main.exs`
```elixir
defmodule AstWalker.Rules do
  @moduledoc "Individual lint rule implementations."

  alias AstWalker.Issue

  @banned_calls [
    {{:crypto, :rand_bytes}, ":crypto.rand_bytes/1 is deprecated, use strong_rand_bytes/1"}
  ]

  @debug_calls [
    {{IO, :inspect}, "IO.inspect/2 left in non-test source"}
  ]

  @spec check_deprecated(Macro.t(), String.t()) :: [Issue.t()]
  def check_deprecated(ast, file) do
    collect_remote_calls(ast, file, @banned_calls, :deprecated_function, :warning)
  end

  @spec check_debug_calls(Macro.t(), String.t()) :: [Issue.t()]
  def check_debug_calls(ast, file) do
    collect_remote_calls(ast, file, @debug_calls, :debug_call_left, :warning)
  end

  @spec check_moduledoc(Macro.t(), String.t()) :: [Issue.t()]
  def check_moduledoc({:defmodule, meta, [_alias, [do: body]]}, file) do
    if has_moduledoc?(body) do
      []
    else
      [
        %Issue{
          file: file,
          line: Keyword.get(meta, :line, 1),
          column: Keyword.get(meta, :column),
          rule: :missing_moduledoc,
          severity: :warning,
          message: "module missing @moduledoc"
        }
      ]
    end
  end

  def check_moduledoc(_, _), do: []

  defp has_moduledoc?({:__block__, _, stmts}), do: Enum.any?(stmts, &moduledoc_node?/1)
  defp has_moduledoc?(single), do: moduledoc_node?(single)

  defp moduledoc_node?({:@, _, [{:moduledoc, _, [content]}]}) when content != false, do: true
  defp moduledoc_node?(_), do: false

  defp collect_remote_calls(ast, file, list, rule, severity) do
    {_ast, acc} =
      Macro.prewalk(ast, [], fn node, acc ->
        case match_remote(node, list) do
          {:match, msg, meta} ->
            issue = %Issue{
              file: file,
              line: Keyword.get(meta, :line, 0),
              column: Keyword.get(meta, :column),
              rule: rule,
              severity: severity,
              message: msg
            }

            {node, [issue | acc]}

          :nomatch ->
            {node, acc}
        end
      end)

    Enum.reverse(acc)
  end

  defp match_remote({{:., _, [{:__aliases__, _, parts}, fun]}, meta, _args}, list) do
    check_tuple({Module.concat(parts), fun}, meta, list)
  end

  defp match_remote({{:., _, [mod, fun]}, meta, _args}, list) when is_atom(mod) do
    check_tuple({mod, fun}, meta, list)
  end

  defp match_remote(_, _), do: :nomatch

  defp check_tuple({mod, fun}, meta, list) do
    case Enum.find(list, fn {{m, f}, _} -> m == mod and f == fun end) do
      {_, msg} -> {:match, msg, meta}
      nil -> :nomatch
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate AST walker for custom linting
      code = """
      defmodule MyModule do
        def process_value(data) do
          HTTPoison.get("url")
          Enum.map(data, &(&1 * 2))
        end
      end
      """

      # Parse code to AST
      {:ok, ast} = Code.string_to_quoted(code)

      # Walk AST and find banned modules
      banned = [:HTTPoison]
      violations = []

      Macro.prewalk(ast, fn node ->
        case node do
          {:__aliases__, _, mod_parts} ->
            mod_atom = Module.concat(mod_parts)
            if mod_atom in banned do
              violations = [mod_atom | violations]
            end
          _ -> nil
        end
        node
      end)

      IO.puts("✓ Banned module calls found: #{inspect(violations)}")

      # Find function calls
      calls = []
      Macro.prewalk(ast, fn node ->
        case node do
          {func, _, _} when is_atom(func) and func != :defmodule ->
            calls = [func | calls]
          _ -> nil
        end
        node
      end)

      IO.puts("✓ Function calls found: #{inspect(Enum.uniq(calls))}")

      assert Enum.any?(violations, &(&1 == HTTPoison)), "Banned module detected"

      IO.puts("✓ AST walker: custom linter working")
  end
end

Main.main()
```
---

## Why AST Walker for a Custom Linter matters

Mastering **AST Walker for a Custom Linter** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts

### 1. `Code.string_to_quoted/2`

Parses source into an AST without executing it:

```
{:ok, ast} = Code.string_to_quoted(File.read!(path), columns: true)
```

`columns: true` keeps column info in `meta` — needed for good error reporting.

### 2. Walking with `Macro.prewalk/3` (accumulator version)

```
Macro.prewalk(ast, [], fn node, issues ->
  {node, check_node(node, issues)}
end)
```

You ignore the transformed-node output (don't modify); the accumulator
collects issues.

### 3. Pattern matching AST nodes

- `{:defmodule, meta, [alias_ast, [do: body]]}` — module definition
- `{{:., _, [mod, fun]}, _, args}` — remote function call
- `{:@, _, [{:moduledoc, _, [content]}]}` — module attribute assignment

Having a cheat-sheet of these shapes makes rule writing fast.

### 4. Module-level state

Some rules need to know "are we inside a defmodule?". Track with a stack in the
accumulator — push on entering, pop on exiting. This is hard with prewalk; easier
with `Macro.traverse/4`.

### 5. Issue reporting

Each finding carries `file`, `line`, `column`, `rule`, `message`. Format as:

```
[W] lib/foo.ex:12:5  deprecated_function  :crypto.rand_bytes/1 is deprecated, use strong_rand_bytes/1
```

---
