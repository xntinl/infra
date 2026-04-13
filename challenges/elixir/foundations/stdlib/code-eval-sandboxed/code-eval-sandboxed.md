# Code evaluation — why it's dangerous and how to sandbox safely

**Project**: `formula_eval` — evaluates simple arithmetic formulas from user input using AST whitelisting, NOT `Code.eval_string/1`.

---

## Project structure

```
formula_eval/
├── lib/
│   └── formula_eval.ex
├── test/
│   └── formula_eval_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

---

## The business problem
Product wants users to enter formulas like `(salary + bonus) * 0.8` in a spreadsheet
cell. The naive solution is `Code.eval_string(formula, bindings)`. Don't do it.

This exercise builds the right version: parse to AST, walk the tree, reject anything
that isn't a number or one of the four basic operators, then evaluate the safe subset
by hand.

Project structure:

```
formula_eval/
├── lib/
│   └── formula_eval.ex
├── test/
│   └── formula_eval_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Code.eval_string/2` evaluates arbitrary Elixir

`Code.eval_string("File.rm_rf!(\"/\")")` runs. There is no sandbox. Elixir is a
full language with file, network, and system access. Any input that reaches
`Code.eval_string/1` is effectively a remote shell.

This is not a hypothetical. "Let the user enter a small expression" is the #1 path
to RCE in internal tools. The interpreter does not care that you *meant* for the
input to be arithmetic.

### 2. `Code.string_to_quoted/1` parses without executing

It returns `{:ok, ast}` — an Elixir AST. No code runs. This is the safe primitive.
You then walk the AST and either evaluate it yourself or reject anything outside
your whitelist.

### 3. AST shape for arithmetic

```elixir
Code.string_to_quoted!("1 + 2 * 3")
# => {:+, [...], [1, {:*, [...], [2, 3]}]}
```

Numbers are literals (`1`, `2.5`). Operators are three-tuples `{op, meta, args}`.
Anything else — variables, function calls, module references — is out of scope
and must be rejected.

### 4. Whitelisting > blacklisting

You do NOT enumerate dangerous things to block. You enumerate the few safe things
to allow and reject everything else. Any novel AST node should fail closed.

---

## Why AST whitelist and not `Code.eval_string/2` with a custom binding

`Code.eval_string/2` with bindings looks like it restricts what the user can do — after all, it only sees the variables you pass in. It doesn't. The BEAM evaluator happily resolves `File`, `System`, `:os`, and every other module in the VM regardless of what bindings you supply. There is no `safe: true` flag and no way to restrict the evaluator. Even removing macro support still leaves you a full-featured shell: `Kernel.apply/3` is reachable, atoms can be constructed at runtime, and `Code.eval_string("File.rm_rf!(~s(/))")` is a valid expression. The only defence is to parse (which doesn't execute), walk the resulting AST yourself, and evaluate only the node shapes you enumerated as safe. Whitelists fail closed: an operator you forgot to list simply doesn't work; blacklists fail open: something you forgot to block simply runs.

---

## Design decisions

**Option A — `Code.eval_string(formula, [])` with an empty binding**
- Pros: 1 line of code; handles arbitrary arithmetic for free; matches operator precedence.
- Cons: full RCE primitive; empty bindings do NOT restrict module access; no way to sandbox; any input reaching this line is a security incident.

**Option B — blacklist dangerous tokens with a regex before evaluating**
- Pros: tempting "just block `File` and `System`" fix; keeps `Code.eval_string` convenience.
- Cons: inexhaustible bypass surface (atom construction, Unicode lookalikes, hex encoding, macro expansion); every attacker's playground; false sense of security.

**Option C — `Code.string_to_quoted/1` → recursive `safe_eval/1` walking only whitelisted AST nodes (numbers, `+`, `-`, `*`, `/`, unary minus)** (chosen)
- Pros: no code ever runs through the evaluator; new AST node shapes fail closed at the catch-all clause; adding an operator requires a conscious code change; division-by-zero handled explicitly rather than via `ArithmeticError`.
- Cons: manual evaluator to maintain; extending to variables/functions needs its own binding store; slightly slower than native eval (irrelevant at formula scale).

Chose **C** because it is the only option that survives a security review. The maintenance cost is 20 lines; the cost of the alternatives is "we got owned because a user typed a formula".

---

## Implementation

### `mix.exs`
```elixir
defmodule FormulaEval.MixProject do
  use Mix.Project

  def project do
    [
      app: :formula_eval,
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

**Objective**: Never Code.eval_string(user_input) — it's a full shell. Parse AST, whitelist node shapes only.

```bash
mix new formula_eval
cd formula_eval
```

### `lib/formula_eval.ex`

**Objective**: Code.string_to_quoted/1 parses without executing; walk AST, whitelist only {+,-,*,/}, reject all else.

```elixir
defmodule FormulaEval do
  @moduledoc """
  Evaluates arithmetic formulas safely by whitelisting AST nodes.

  Allowed:
    - integer and float literals
    - binary operators: +, -, *, /
    - unary minus
    - parentheses (implicit in the AST)

  Everything else — variables, function calls, atoms, strings, lists, maps,
  module references — is rejected at the AST level. No code is ever executed
  by the interpreter.
  """

  @allowed_binary_ops [:+, :-, :*, :/]

  @doc """
  Evaluates `formula`. Returns `{:ok, number}` or `{:error, reason}`.

  ## Examples

      iex> FormulaEval.eval("1 + 2 * 3")
      {:ok, 7}

      iex> FormulaEval.eval("(10 - 2) / 4")
      {:ok, 2.0}

      iex> FormulaEval.eval("File.rm_rf!(\\"/\\")")
      {:error, :forbidden_expression}
  """
  @spec eval(String.t()) :: {:ok, number()} | {:error, atom()}
  def eval(formula) when is_binary(formula) do
    # Step 1: parse to AST — no execution happens here.
    with {:ok, ast} <- Code.string_to_quoted(formula),
         # Step 2: walk the tree and evaluate by hand, using our whitelist.
         {:ok, result} <- safe_eval(ast) do
      {:ok, result}
    else
      {:error, {_meta, _msg, _token}} -> {:error, :parse_error}
      {:error, reason} -> {:error, reason}
    end
  end

  # --- safe evaluator --------------------------------------------------------

  # Number literals are the leaves of every valid formula.
  defp safe_eval(n) when is_integer(n) or is_float(n), do: {:ok, n}

  # Binary operators: {op, _meta, [left, right]}.
  # We pattern match the operator explicitly against the whitelist — any op
  # outside the list falls through to the catch-all clause below.
  defp safe_eval({op, _meta, [l, r]}) when op in @allowed_binary_ops do
    with {:ok, lv} <- safe_eval(l),
         {:ok, rv} <- safe_eval(r) do
      apply_op(op, lv, rv)
    end
  end

  # Unary minus: `-x` parses as {:-, meta, [x]} — one arg, not two.
  defp safe_eval({:-, _meta, [x]}) do
    with {:ok, v} <- safe_eval(x), do: {:ok, -v}
  end

  # Catch-all: anything else is forbidden. Variables, calls, atoms, all land here.
  # Fail closed — this is the core safety guarantee.
  defp safe_eval(_other), do: {:error, :forbidden_expression}

  # Division by zero must be caught — the BEAM raises ArithmeticError otherwise,
  # which is an uncontrolled exit for our caller.
  defp apply_op(:/, _l, 0), do: {:error, :division_by_zero}
  defp apply_op(:/, _l, 0.0), do: {:error, :division_by_zero}
  defp apply_op(:+, l, r), do: {:ok, l + r}
  defp apply_op(:-, l, r), do: {:ok, l - r}
  defp apply_op(:*, l, r), do: {:ok, l * r}
  defp apply_op(:/, l, r), do: {:ok, l / r}
end
```

### Step 3: `test/formula_eval_test.exs`

**Objective**: Test operator precedence, division by zero (handled explicitly), unary minus, rejection of function calls.

```elixir
defmodule FormulaEvalTest do
  use ExUnit.Case, async: true
  doctest FormulaEval

  describe "eval/1 — happy path" do
    test "evaluates integer arithmetic" do
      assert {:ok, 7} = FormulaEval.eval("1 + 2 * 3")
      assert {:ok, 9} = FormulaEval.eval("(1 + 2) * 3")
    end

    test "evaluates with floats" do
      assert {:ok, 2.5} = FormulaEval.eval("5 / 2")
      assert {:ok, 3.0} = FormulaEval.eval("1.5 * 2")
    end

    test "handles unary minus" do
      assert {:ok, -5} = FormulaEval.eval("-5")
      assert {:ok, -1} = FormulaEval.eval("2 + -3")
    end
  end

  describe "eval/1 — safety" do
    test "rejects function calls" do
      assert {:error, :forbidden_expression} =
               FormulaEval.eval("File.rm_rf!(\"/tmp\")")
    end

    test "rejects variables" do
      assert {:error, :forbidden_expression} = FormulaEval.eval("x + 1")
    end

    test "rejects module references" do
      assert {:error, :forbidden_expression} = FormulaEval.eval(":os.cmd('ls')")
    end

    test "rejects anonymous functions" do
      assert {:error, :forbidden_expression} = FormulaEval.eval("fn -> 1 end")
    end

    test "rejects string literals" do
      # Even innocent-looking things — strings, lists — are out of scope.
      assert {:error, :forbidden_expression} = FormulaEval.eval("\"hello\"")
      assert {:error, :forbidden_expression} = FormulaEval.eval("[1, 2, 3]")
    end

    test "rejects bitwise ops (not in our whitelist)" do
      # `|||` is bitwise OR — a perfectly valid Elixir op but NOT whitelisted.
      # This is the whole point of a whitelist: new ops fail closed.
      assert {:error, :forbidden_expression} = FormulaEval.eval("1 ||| 2")
    end
  end

  describe "eval/1 — errors" do
    test "returns :parse_error on malformed input" do
      assert {:error, :parse_error} = FormulaEval.eval("1 +")
      assert {:error, :parse_error} = FormulaEval.eval("((1)")
    end

    test "catches division by zero instead of crashing" do
      assert {:error, :division_by_zero} = FormulaEval.eval("1 / 0")
      assert {:error, :division_by_zero} = FormulaEval.eval("1 / (2 - 2)")
    end
  end
end
```

### Step 4: Run

**Objective**: --warnings-as-errors finds unused whitelist branches; test coverage validates exploit attempts fail safely.

```bash
mix test
```

### Why this works

`Code.string_to_quoted/1` parses the input to an AST without executing a single expression — it's a pure transformation from source text to a data structure. `safe_eval/1` then pattern matches on the handful of node shapes we allow (integer/float literals, whitelisted binary operators, unary minus). Every other shape — variables `{name, meta, nil}`, function calls `{fun, meta, args}`, module references `{:__aliases__, _, _}`, atoms, strings, tuples, maps — falls through to the catch-all clause and returns `{:error, :forbidden_expression}`. This is the "fail closed" property: an operator you didn't think of simply doesn't work. Division by zero is handled explicitly in `apply_op/3` so `1/0` becomes a tagged error rather than an uncontrolled `ArithmeticError` bubbling up to the caller.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== FormulaEval: demo ===\n")

    result_1 = FormulaEval.eval("1 + 2 * 3")
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = FormulaEval.eval("(10 - 2) / 4")
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = FormulaEval.eval("File.rm_rf!(\\"/\\")")
    IO.puts("Demo 3: #{inspect(result_3)}")

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create a simple example demonstrating the key concepts:

```elixir
# Example code demonstrating module concepts
IO.puts("Example: Read the Implementation section above and run the code samples in iex")
```

## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    formulas = [
      "1 + 2 * 3",
      "(10 - 2) / 4",
      "((1 + 2) * (3 + 4)) / 7",
      "-5 + 3 * -2"
    ]

    {us, _} =
      :timer.tc(fn ->
        Enum.each(1..100_000, fn _ ->
          Enum.each(formulas, &FormulaEval.eval/1)
        end)
      end)

    per = us / (100_000 * length(formulas))
    IO.puts("eval/1: #{Float.round(per, 2)} µs/call")
  end
end

Bench.run()
```

Target: under 20 µs per call. `Code.string_to_quoted/1` dominates; the AST walk itself is a few hundred nanoseconds. If you expect millions of calls per second, cache the parsed AST keyed by the raw formula string.

---

## Trade-offs and production gotchas

**1. `Code.eval_string/1` has no safe mode**
There is no option to disable `File`, `System`, or `:os`. Any input reaching it is
a command execution primitive. Treat it like `eval()` in JavaScript or `exec()` in
Python — never on untrusted data, and "untrusted" includes any input from a form,
API, or database row that was once a form.

**2. The "I'll just block the dangerous atoms" trap**
Attempted blacklists always miss something. Escaping, Unicode lookalikes, creative
atom construction — by the time you've patched all the bypasses, you've rebuilt a
parser anyway. Whitelist from day one.

**3. Variables need a binding store you control**
If you extend this to support `x + y`, do NOT pass `bindings` to `Code.eval_string`.
Add a clause to `safe_eval/1` that looks up variables in a map YOU control:

```elixir
defp safe_eval({name, _meta, nil}, bindings) when is_atom(name) do
  case Map.fetch(bindings, name) do
    {:ok, v} when is_number(v) -> {:ok, v}
    _ -> {:error, :unknown_variable}
  end
end
```

**4. Parsing is cheap, evaluating is bounded**
`Code.string_to_quoted/1` on adversarial input (deeply nested parens) can be slow
but not dangerous — it produces an AST, not side effects. Still worth a length
limit on input (e.g. reject formulas > 1 KB).

**5. When NOT to do any of this**
If users need a real spreadsheet language, use an embedded language designed for
it (Lua via `:luerl`, a DSL via `NimbleParsec`). Hand-rolling an arithmetic
evaluator is fine; hand-rolling a full language is a yak too big to shave.

---

## Reflection

1. Product now wants variables (`salary + bonus`) resolved from a context map the caller controls. The obvious extension is adding a `{name, _meta, nil}` clause with a bindings lookup. What new risks does that open (atom exhaustion from unknown names, lookup of a function-like atom, shadowing of allowed operators), and how do you close them before shipping?
2. A power user asks "can I get `min`, `max`, and `sum` as functions?". You can whitelist specific function names in the AST walker. Where do you draw the line between "safe arithmetic DSL" and "small scripting language"? At what feature set would you stop patching `safe_eval/1` and migrate to a real parser (`NimbleParsec`) or an embedded language (`:luerl`)?

---

## Resources

- [`Code.string_to_quoted/2`](https://hexdocs.pm/elixir/Code.html#string_to_quoted/2)
- ["The Elixir AST" — Elixir docs](https://hexdocs.pm/elixir/syntax-reference.html#the-elixir-ast)
- [`NimbleParsec`](https://hexdocs.pm/nimble_parsec/) — when you outgrow AST whitelisting and need a real parser
- [OWASP — Injection](https://owasp.org/www-community/Injection_Flaws) — the class of bugs this exercise is about

---

## Why Code evaluation — why it's dangerous and how to sandbox safely matters

Mastering **Code evaluation — why it's dangerous and how to sandbox safely** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/formula_eval_test.exs`

```elixir
defmodule FormulaEvalTest do
  use ExUnit.Case, async: true

  doctest FormulaEval

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert FormulaEval.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. `Code.eval_string/1` Executes Elixir Code at Runtime
This is powerful but dangerous. Never eval untrusted code—it has full access to your system.

### 2. No True Sandboxing in Elixir
There is no `safe eval` in Elixir. If you eval untrusted code, assume your system is compromised.

### 3. When to Use
Use `Code.eval_string` for user-defined formulas, configuration that's Elixir code, or trusted plugins. Never for user-supplied input.

---
