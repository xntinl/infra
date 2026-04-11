# Mini-Language Compiler

**Project**: `mlang` — a statically-typed mini-language compiler targeting the BEAM virtual machine

---

## Project context

You are building `mlang`, a compiler for a statically-typed mini-language that targets the BEAM. Source programs are compiled to Core Erlang, which OTP then compiles to native `.beam` bytecode. The pipeline covers all classic compiler phases: lexing, Pratt parsing, type checking via unification, and Core Erlang code generation. Compiled programs can call any Erlang or Elixir function directly.

Project structure:

```
mlang/
├── lib/
│   └── mlang/
│       ├── application.ex           # compiler supervisor
│       ├── lexer.ex                 # tokenizer: identifiers, keywords, operators, literals
│       ├── ast.ex                   # typed AST node structs with source position
│       ├── parser.ex                # Pratt parser: expression precedence, statements
│       ├── type_checker.ex          # unification-based type inference: Int, Float, Bool, String, Fn
│       ├── codegen.ex               # Core Erlang emitter: modules, functions, closures
│       ├── closure_converter.ex     # lambda lifting: free variables → explicit parameters
│       ├── error.ex                 # structured error: file, line, column, message
│       └── compiler.ex              # public API: compile_file/1, compile_string/2
├── test/
│   └── mlang/
│       ├── lexer_test.exs           # token positions, keyword recognition
│       ├── parser_test.exs          # operator precedence, error recovery
│       ├── type_checker_test.exs    # inference, type mismatch errors
│       ├── codegen_test.exs         # emitted Core Erlang compiles and runs correctly
│       └── interop_test.exs         # call Elixir/Erlang functions from mlang
├── bench/
│   └── mlang_bench.exs
└── mix.exs
```

---

## The problem

Writing a compiler that targets a real runtime is harder than writing an interpreter: you cannot evaluate eagerly — you must emit code that runs correctly when loaded into the BEAM later. Closures are not native to Core Erlang modules (which are flat sets of functions), so free variables must be explicitly threaded through as parameters (lambda lifting or closure conversion). The type checker must prove at compile time that no operation is applied to the wrong type, which requires unification — the same algorithm used by Hindley-Milner type inference.

---

## Why this design

**Pratt parser for expression precedence**: recursive descent naturally handles statements but struggles with operator precedence. A Pratt parser assigns binding power to each token; the `parse_expr(min_bp)` loop consumes tokens as long as their left-binding power exceeds `min_bp`. Adding a new operator requires only registering its binding power — no grammar rewrite.

**Unification-based type inference**: each expression is assigned a type variable. Type constraints are collected as unification equations (`T1 = T2`). Robinson's unification algorithm solves the equations by finding a substitution. Type errors are reported when unification fails (e.g., `Int` cannot unify with `Bool`).

**Core Erlang as a compilation target**: Core Erlang is a well-documented intermediate language accepted by the OTP compiler. It is simpler than BEAM bytecode (explicit modules, functions with clauses, `let` expressions) but still rich enough to express closures, pattern matching, and inter-module calls. The `cerl` module in OTP provides an API for building Core Erlang ASTs programmatically.

**Closure conversion before codegen**: Core Erlang functions are defined at module level and cannot capture variables from enclosing scopes. The closure converter transforms each lambda that captures free variables into a named function whose first argument is a tuple of captured values. The original call site passes the captured variables explicitly.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new mlang --sup
cd mlang
mkdir -p lib/mlang test/mlang bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Lexer

```elixir
# lib/mlang/lexer.ex
defmodule Mlang.Lexer do
  @moduledoc """
  Tokenizes mlang source text.

  Token types (all include {line, col} position):
    {:ident, name, pos}
    {:int, n, pos}
    {:float, f, pos}
    {:string, s, pos}
    {:bool, true | false, pos}
    {:kw, :if | :else | :fn | :let | :return | :while | :break | :continue, pos}
    {:op, :+ | :- | :* | :/ | := | :== | :!= | :< | :> | :<= | :>=, pos}
    {:lparen | :rparen | :lbrace | :rbrace | :comma | :semicolon | :colon | :arrow, pos}
    {:eof, pos}
  """

  def tokenize(source, filename \\ "<string>") do
    # TODO: walk source tracking {line, col}, accumulate tokens
    # HINT: handle // line comments (skip to newline)
    # HINT: multi-char operators: ==, !=, <=, >=, ->
    # HINT: string literals allow \" and \\ escapes
    # HINT: on unrecognized character, add to error list and continue (don't stop)
  end
end
```

### Step 4: AST node structs

```elixir
# lib/mlang/ast.ex
defmodule Mlang.AST do
  @moduledoc "Typed AST node structs. Each node carries a pos field and a type field (nil until type-checked)."

  defmodule IntLit,     do: defstruct [:value, :pos, :type]
  defmodule FloatLit,   do: defstruct [:value, :pos, :type]
  defmodule BoolLit,    do: defstruct [:value, :pos, :type]
  defmodule StringLit,  do: defstruct [:value, :pos, :type]
  defmodule Ident,      do: defstruct [:name,  :pos, :type]
  defmodule BinOp,      do: defstruct [:op, :left, :right, :pos, :type]
  defmodule UnaryOp,    do: defstruct [:op, :expr, :pos, :type]
  defmodule Call,       do: defstruct [:callee, :args, :pos, :type]
  defmodule IfExpr,     do: defstruct [:cond, :then, :else_, :pos, :type]
  defmodule FnDef,      do: defstruct [:name, :params, :return_type, :body, :pos, :type]
  defmodule Let,        do: defstruct [:name, :value, :pos, :type]
  defmodule Assign,     do: defstruct [:name, :value, :pos, :type]
  defmodule Return,     do: defstruct [:value, :pos, :type]
  defmodule Block,      do: defstruct [:stmts, :pos, :type]
  defmodule Module_,    do: defstruct [:name, :defs, :pos]
end
```

### Step 5: Pratt parser

```elixir
# lib/mlang/parser.ex
defmodule Mlang.Parser do
  @moduledoc """
  Pratt parser (top-down operator precedence).

  Each token has a left-binding power (lbp). parse_expr(min_bp) consumes tokens
  whose lbp > min_bp. This naturally handles left-associativity, right-associativity,
  and mixed-precedence expressions.

  Binding powers (higher = tighter binding):
    =            → 1  (assignment, right-associative)
    == !=        → 3
    < > <= >=    → 4
    + -          → 5
    * /          → 6
    unary -      → 7
    call ()      → 8
  """

  @doc "Returns {:ok, Mlang.AST.Module_} or {:error, [Mlang.Error.t()]}."
  def parse(tokens, filename \\ "<string>") do
    # TODO: top-level loop: parse fn definitions, let bindings
    # TODO: parse_expr(tokens, min_bp) → {expr_node, remaining_tokens}
    # TODO: nud (null denotation): prefix position — literals, ident, unary -, (expr)
    # TODO: led (left denotation): infix position — binary ops, function call
    # TODO: collect all errors, return all at end rather than stopping at first
  end

  # Binding powers
  defp lbp(:==), do: 3
  defp lbp(:!=), do: 3
  defp lbp(:<),  do: 4
  defp lbp(:>),  do: 4
  defp lbp(:+),  do: 5
  defp lbp(:-),  do: 5
  defp lbp(:*),  do: 6
  defp lbp(:/),  do: 6
  defp lbp(:lparen), do: 8  # function call
  defp lbp(_),   do: 0
end
```

### Step 6: Type checker

```elixir
# lib/mlang/type_checker.ex
defmodule Mlang.TypeChecker do
  @moduledoc """
  Unification-based type inference.

  Types:
    :int | :float | :bool | :string | {:fn, [param_types], return_type} | {:var, id}

  Algorithm:
    1. Assign a fresh type variable {:var, N} to each expression node.
    2. Collect constraints: e.g., the condition of `if` must be :bool.
    3. Unify all constraints using Robinson's algorithm.
    4. Apply the resulting substitution to all type variables.
    5. Annotate each AST node's :type field with the resolved type.
  """

  def check(ast_module) do
    # TODO: walk AST, generate constraints, unify, annotate
    # TODO: return {:ok, annotated_module} or {:error, [type_errors]}
  end

  defp unify(t1, t2, subst) do
    # TODO: Robinson's unification
    # {:var, id} unifies with anything (apply substitution first)
    # identical ground types unify trivially
    # {:fn, p1, r1} unifies with {:fn, p2, r2} iff all params unify pairwise and return types unify
    # mismatch → {:error, "type mismatch: #{inspect t1} vs #{inspect t2}"}
  end

  defp fresh_var(counter) do
    # TODO: {:var, counter}, counter + 1
  end
end
```

### Step 7: Code generator

```elixir
# lib/mlang/codegen.ex
defmodule Mlang.Codegen do
  @moduledoc """
  Emits Core Erlang from a type-checked and closure-converted AST.

  Uses the OTP :cerl module to construct Core Erlang AST nodes:
    :cerl.c_module/3   — module declaration
    :cerl.c_fun/2      — function definition
    :cerl.c_call/3     — inter-module call (for Erlang/Elixir interop)
    :cerl.c_apply/2    — local function application
    :cerl.c_let/3      — let binding
    :cerl.c_case/2     — pattern matching / if-else
    :cerl.c_literal/1  — integer, float, atom, binary literals

  To compile the emitted Core Erlang:
    :compile.forms(core_ast, [:from_core])  → {:ok, mod_name, beam_binary}
    :code.load_binary(mod_name, ~c"mlang", beam_binary) → {:module, mod_name}
  """

  def emit(ast_module) do
    # TODO: emit a :cerl module node from Mlang.AST.Module_
    # TODO: each FnDef → :cerl.c_fun with body
    # TODO: BinOp on numbers → :cerl.c_call(:erlang, op_atom, [left, right])
    # TODO: IfExpr → :cerl.c_case with two clauses (true, false on condition)
    # TODO: Call to Module.function → :cerl.c_call(module, function, args)
  end
end
```

### Step 8: Given tests — must pass without modification

```elixir
# test/mlang/codegen_test.exs
defmodule Mlang.CodegenTest do
  use ExUnit.Case, async: true

  defp compile_and_load(source, mod_name) do
    {:ok, ast}     = Mlang.Lexer.tokenize(source) |> then(fn {:ok, t} -> Mlang.Parser.parse(t) end)
    {:ok, typed}   = Mlang.TypeChecker.check(ast)
    {:ok, converted} = Mlang.ClosureConverter.convert(typed)
    {:ok, core}    = Mlang.Codegen.emit(converted)
    {:ok, ^mod_name, beam} = :compile.forms(core, [:from_core])
    {:module, ^mod_name}   = :code.load_binary(mod_name, ~c"mlang_test", beam)
    mod_name
  end

  test "compiled fibonacci runs correctly" do
    source = """
    fn fib(n: Int) -> Int {
      if n < 2 { return n; }
      return fib(n - 1) + fib(n - 2);
    }
    """
    mod = compile_and_load(source, :mlang_fib)
    assert mod.fib(10) == 55
    assert mod.fib(0)  == 0
  end

  test "closure captures free variable" do
    source = """
    fn make_adder(n: Int) -> Fn(Int) -> Int {
      return fn(x: Int) -> Int { return x + n; };
    }
    """
    mod = compile_and_load(source, :mlang_adder)
    add5 = mod.make_adder(5)
    assert add5.(10) == 15
  end
end
```

```elixir
# test/mlang/type_checker_test.exs
defmodule Mlang.TypeCheckerTest do
  use ExUnit.Case, async: true

  defp type_check(source) do
    {:ok, tokens} = Mlang.Lexer.tokenize(source)
    {:ok, ast}    = Mlang.Parser.parse(tokens)
    Mlang.TypeChecker.check(ast)
  end

  test "type mismatch on if condition" do
    source = """
    fn bad(x: Int) -> Int {
      if x + 1 { return 0; } else { return 1; }
    }
    """
    {:error, errors} = type_check(source)
    assert Enum.any?(errors, fn e -> e.message =~ "bool" end)
  end

  test "correct arithmetic infers Int return type" do
    source = """
    fn add(a: Int, b: Int) -> Int { return a + b; }
    """
    {:ok, typed_module} = type_check(source)
    [fn_def] = typed_module.defs
    assert fn_def.type == {:fn, [:int, :int], :int}
  end
end
```

### Step 9: Run the tests

```bash
mix test test/mlang/ --trace
```

### Step 10: Benchmark

```elixir
# bench/mlang_bench.exs
fib_source = """
fn fib(n: Int) -> Int {
  if n < 2 { return n; }
  return fib(n - 1) + fib(n - 2);
}
"""

tokens_result = Mlang.Lexer.tokenize(fib_source)
{:ok, tokens} = tokens_result
{:ok, ast}    = Mlang.Parser.parse(tokens)
{:ok, typed}  = Mlang.TypeChecker.check(ast)

Benchee.run(
  %{
    "lex + parse (100 tokens)"      => fn -> Mlang.Lexer.tokenize(fib_source) end,
    "type check (fib)"              => fn -> Mlang.TypeChecker.check(ast) end,
    "full compile fib → .beam"      => fn ->
      {:ok, t}  = Mlang.Lexer.tokenize(fib_source)
      {:ok, a}  = Mlang.Parser.parse(t)
      {:ok, ty} = Mlang.TypeChecker.check(a)
      {:ok, cc} = Mlang.ClosureConverter.convert(ty)
      {:ok, core} = Mlang.Codegen.emit(cc)
      :compile.forms(core, [:from_core])
    end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

---

## Trade-off analysis

| Aspect | Core Erlang target (this impl) | BEAM bytecode direct | Elixir AST (macros) |
|--------|-------------------------------|---------------------|---------------------|
| Complexity | moderate (cerl API available) | high (undocumented format) | low (OTP does it for you) |
| Portability | OTP-version-stable | fragile across OTP versions | Elixir-version-dependent |
| Optimization | passes through OTP optimizer | none (you emit final bytecode) | full Elixir compiler pipeline |
| Interop | any Erlang/Elixir call | any | Elixir only |
| Debug info | limited | none unless you emit it | full source maps |
| Suitable for | research compilers, DSLs | performance-critical bytecode hacks | Elixir DSLs and macro systems |

Reflection: closure conversion threads free variables through function signatures. How does the calling convention change for a closure-converted function versus a plain function? What overhead does this introduce at call sites where closures are passed as first-class values?

---

## Common production mistakes

**1. Pratt parser not handling right-associativity**
Assignment (`=`) is right-associative: `a = b = 1` means `a = (b = 1)`. A Pratt parser handles this by using `lbp - 1` as the minimum binding power when recursing on the right operand. Using `lbp` instead produces left-associativity for all operators.

**2. Type variables not substituted before unification**
If `{:var, 1}` was previously unified to `:int`, and a new constraint `{:var, 1} = :bool` arrives, the check must first apply the current substitution to `{:var, 1}` before attempting unification. Failing to apply the substitution produces incorrect "type conflict" errors.

**3. Closure converter not capturing transitively**
A closure that calls another closure inside it may have transitive free variables. If `fn(x) { fn(y) { x + y } }` is converted, the inner closure's free variable `x` comes from the outer closure's parameter, not the module scope. The converter must track free variables recursively through nested lambdas.

**4. Core Erlang module not exporting public functions**
`:cerl.c_module/3` requires an explicit export list. Functions not in the export list are unreachable from outside the module. The code generator must include all public functions (or all functions during development) in the export attribute.

---

## Resources

- Core Erlang 1.0.3 Specification — [it.uu.se/research/group/hipe/cerl](https://www.it.uu.se/research/group/hipe/cerl/) — the normative reference for the target IR
- OTP `:cerl` module documentation — [erlang.org/doc/man/cerl.html](https://www.erlang.org/doc/man/cerl.html) — API for building Core Erlang ASTs
- Ball, T. — *Writing A Compiler In Go* — Pratt parser and code generation concepts transfer directly
- Nystrom, R. — *Crafting Interpreters* — [craftinginterpreters.com](https://craftinginterpreters.com) — free; excellent on Pratt parsing and closures
- Pierce, B.C. — *Types and Programming Languages* — MIT Press — chapters 22–23 cover type reconstruction and unification
- Appel, A.W. — *Compiling with Continuations* — closure conversion and lambda lifting
