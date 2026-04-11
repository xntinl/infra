# Lisp Interpreter

**Project**: `schemia` — a Scheme-dialect Lisp interpreter with TCO, closures, and a REPL

---

## Project context

You are building `schemia`, a complete interpreter for a Scheme-like Lisp dialect. The system implements the full pipeline from raw source text to evaluated results: lexing, parsing, and evaluation with a lexical environment chain. Tail call optimization prevents stack overflow on deeply recursive programs. The interpreter ships with a standard library and an interactive REPL.

Project structure:

```
schemia/
├── lib/
│   └── schemia/
│       ├── application.ex           # REPL supervisor
│       ├── lexer.ex                 # tokenizes source: symbols, numbers, strings, parens
│       ├── parser.ex                # token list → nested Elixir lists (s-expressions)
│       ├── evaluator.ex             # eval/2: expressions × environments → values
│       ├── environment.ex           # lexical scope: linked chain of maps
│       ├── special_forms.ex         # define, lambda, if, begin, let, let*, letrec, cond, quote
│       ├── stdlib.ex                # built-in procedures: arithmetic, list ops, predicates
│       ├── tco.ex                   # trampoline: {:tail_call, thunk} → loop
│       └── repl.ex                  # read-eval-print loop with multi-line support
├── test/
│   └── schemia/
│       ├── lexer_test.exs           # tokenization of symbols, strings, escapes, numbers
│       ├── parser_test.exs          # s-expression structure, parse errors with position
│       ├── evaluator_test.exs       # special forms, closures, define, set!
│       ├── tco_test.exs             # tail calls do not overflow the BEAM stack
│       └── stdlib_test.exs          # map, filter, apply, arithmetic, list primitives
├── bench/
│   └── schemia_bench.exs
└── mix.exs
```

---

## The problem

A Lisp interpreter appears simple: an evaluator is just `eval(expr, env)` applied recursively. The hard part is that Lisp programs are naturally tail-recursive — `(define (loop n) (loop (+ n 1)))` is perfectly valid and must run forever without growing the call stack. The BEAM supports tail calls between Elixir functions but not across the recursive `eval/2` calls inside the interpreter. Without an explicit trampolining mechanism, `(fact 1000000 1)` overflows the BEAM process stack at roughly 10k–50k recursive evaluator calls.

---

## Why this design

**Lexical environments as linked maps**: an environment is `%{bindings: map, parent: env | nil}`. Symbol lookup walks the chain from child to parent. `define` adds to the current frame; `set!` mutates the frame that owns the binding. This exactly models Scheme's environment semantics with no global mutation.

**Trampoline for TCO**: instead of calling `eval/2` directly in tail position, the evaluator returns `{:tail_call, expr, env}`. The `tco.ex` trampoline loops until it gets a non-thunk value. This converts the recursive interpreter call stack into a heap-allocated loop — `(fact 1000000 1)` becomes 1 million iterations of the trampoline, not 1 million stack frames.

**Homoiconicity via Elixir data structures**: Lisp code and data share the same representation. A parsed s-expression `(+ 1 2)` is the Elixir list `[:+, 1, 2]`. `quote` returns the unevaluated list directly. This means `eval` is just a pattern match on Elixir terms.

**REPL with paren-balance detection**: a single readline does not make a complete expression if parentheses are unbalanced. The REPL counts open/close parens and continues reading lines until the expression is balanced before calling the parser.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new schemia --sup
cd schemia
mkdir -p lib/schemia test/schemia bench
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
# lib/schemia/lexer.ex
defmodule Schemia.Lexer do
  @moduledoc """
  Tokenizes Scheme source text.

  Token types:
    {:lparen, line}
    {:rparen, line}
    {:symbol, name, line}
    {:integer, n, line}
    {:float, f, line}
    {:string, s, line}       -- with \" and \\ escape sequences resolved
    {:bool, true | false, line}
    {:quote_shorthand, line} -- the ' character
  """

  def tokenize(source) when is_binary(source) do
    # TODO: walk source character by character, accumulate tokens
    # HINT: use a recursive helper with (chars, line, acc)
    # HINT: skip whitespace and ; line comments
    # HINT: string parsing must handle \" and \\
    # HINT: numbers are tried first; if :float.parse fails, try integer; else it is a symbol
  end
end
```

### Step 4: Parser

```elixir
# lib/schemia/parser.ex
defmodule Schemia.Parser do
  @moduledoc """
  Converts a flat token list to nested Elixir structures representing s-expressions.

  Scheme → Elixir:
    ()       → []
    (a b c)  → [:a, :b, :c]
    (a (b c) d) → [:a, [:b, :c], :d]
    'expr    → [:quote, expr]
    42       → 42
    "hi"     → "hi"
    #t       → true
    #f       → false
  """

  @doc "Returns {:ok, [expr]} or {:error, {line, message}}."
  def parse(tokens) do
    # TODO: recursive descent; read_expr consumes tokens and returns {expr, remaining_tokens}
    # TODO: {:lparen, _} → consume until matching :rparen, collecting sub-expressions
    # TODO: {:quote_shorthand, _} → wrap next expression in [:quote, expr]
    # TODO: mismatched parens → {:error, {line, "unexpected )"}} or "unexpected EOF"
  end
end
```

### Step 5: Environment

```elixir
# lib/schemia/environment.ex
defmodule Schemia.Environment do
  @moduledoc """
  Lexical environment as a linked chain of frames.

  %{bindings: %{atom => value}, parent: env | nil}
  """

  def new(parent \\ nil), do: %{bindings: %{}, parent: parent}

  def extend(parent, bindings) when is_map(bindings) do
    %{bindings: bindings, parent: parent}
  end

  def lookup(env, name) do
    # TODO: check bindings, recurse to parent, raise on not found
    # HINT: raise "Unbound variable: #{name}" at root
  end

  def define(env, name, value) do
    # TODO: add to current frame (creates new frame map, returns updated env)
  end

  def set!(env, name, value) do
    # TODO: find the frame that owns `name`, update it
    # TODO: raise "Unbound variable: #{name}" if not found in any frame
  end
end
```

### Step 6: TCO trampoline

```elixir
# lib/schemia/tco.ex
defmodule Schemia.TCO do
  @moduledoc """
  Trampoline for tail call optimization.

  The evaluator returns {:tail_call, expr, env} when it encounters a call
  in tail position. The trampoline loops until a non-thunk value is produced.

  This converts recursive interpreter calls into a flat while-loop equivalent.
  """

  def run(thunk) do
    # TODO: loop while result is {:tail_call, expr, env}
    # HINT:
    #   defp trampoline({:tail_call, expr, env}), do: trampoline(Schemia.Evaluator.eval(expr, env))
    #   defp trampoline(value), do: value
  end
end
```

### Step 7: Evaluator

```elixir
# lib/schemia/evaluator.ex
defmodule Schemia.Evaluator do
  @moduledoc """
  Evaluates s-expressions in a given environment.

  eval(expr, env) returns a value OR {:tail_call, expr, env} for TCO.

  Dispatch rules:
    integer/float/string/bool   → self-evaluating
    atom                        → environment lookup
    [:quote, x]                 → x (unevaluated)
    [:if, test, then, else_]    → eval test; branch; TCO on result branch
    [:define, name, body]       → eval body, extend env
    [:lambda, params, body]     → {:closure, params, body, env}
    [:begin | exprs]            → eval all; last in tail position
    [:let, bindings, body]      → create new env, eval body
    [f | args]                  → eval f and all args; apply
  """

  alias Schemia.{Environment, TCO}

  def eval(expr, env) do
    # TODO: implement pattern dispatch for all expression forms
    # TODO: self-evaluating: integers, floats, booleans, strings
    # TODO: symbol: Environment.lookup(env, expr)
    # TODO: special forms by first element
    # TODO: procedure application: eval all elements, then apply/3
  end

  defp apply({:closure, params, body, closure_env}, args, _env) do
    # TODO: bind params to args, create child env from closure_env, eval body with TCO
  end
  defp apply(builtin, args, _env) when is_function(builtin) do
    # TODO: call the built-in function directly
  end
end
```

### Step 8: Standard library

```elixir
# lib/schemia/stdlib.ex
defmodule Schemia.Stdlib do
  @moduledoc """
  Built-in procedures bound in the root environment.

  Each entry is {scheme_symbol, elixir_function}.
  Functions receive a list of evaluated arguments.
  """

  def root_env do
    Schemia.Environment.extend(nil, bindings())
  end

  defp bindings do
    %{
      :+ => fn [a, b] -> a + b end,
      :- => fn [a, b] -> a - b end,
      :* => fn [a, b] -> a * b end,
      :/ => fn [a, b] -> a / b end,
      := => fn [a, b] -> a == b end,
      :< => fn [a, b] -> a < b end,
      :> => fn [a, b] -> a > b end,
      :car   => fn [[h | _]] -> h end,
      :cdr   => fn [[_ | t]] -> t end,
      :cons  => fn [h, t] -> [h | t] end,
      :list  => fn args -> args end,
      :"null?"  => fn [[]] -> true; [_] -> false end,
      :"pair?"  => fn [[_ | _]] -> true; [_] -> false end,
      :"number?" => fn [n] -> is_number(n) end,
      :"symbol?" => fn [s] -> is_atom(s) end,
      :"equal?" => fn [a, b] -> a == b end,
      :not    => fn [x] -> !x end,
      :display => fn [x] -> IO.write(inspect_val(x)); x end,
      :newline => fn [] -> IO.write("\n"); :ok end,
      # TODO: implement map, filter, for-each, apply
      # HINT: map applies a closure to each element; it must call Evaluator.eval
    }
  end

  defp inspect_val(x) when is_atom(x), do: Atom.to_string(x)
  defp inspect_val(x), do: inspect(x)
end
```

### Step 9: Given tests — must pass without modification

```elixir
# test/schemia/tco_test.exs
defmodule Schemia.TCOTest do
  use ExUnit.Case, async: true

  alias Schemia.{Lexer, Parser, Evaluator, Stdlib}

  defp eval_str(source) do
    {:ok, tokens} = Lexer.tokenize(source)
    {:ok, exprs}  = Parser.parse(tokens)
    env = Stdlib.root_env()
    Enum.reduce(exprs, {nil, env}, fn expr, {_val, e} ->
      {Evaluator.eval(expr, e), e}
    end)
    |> elem(0)
  end

  test "tail-recursive factorial does not overflow the stack" do
    program = """
    (define (fact n acc)
      (if (= n 0)
        acc
        (fact (- n 1) (* n acc))))
    """
    eval_str(program)

    # 1_000_000 recursive calls — would overflow without TCO
    result = eval_str("(fact 1000000 1)")
    assert is_integer(result) or is_float(result)
  end

  test "mutual tail recursion terminates" do
    program = """
    (define (even? n) (if (= n 0) #t  (odd?  (- n 1))))
    (define (odd?  n) (if (= n 0) #f (even? (- n 1))))
    """
    eval_str(program)
    assert eval_str("(even? 100000)") == true
    assert eval_str("(odd?  100001)") == true
  end
end
```

```elixir
# test/schemia/evaluator_test.exs
defmodule Schemia.EvaluatorTest do
  use ExUnit.Case, async: true

  alias Schemia.{Lexer, Parser, Evaluator, Stdlib}

  defp run(source) do
    {:ok, tokens} = Lexer.tokenize(source)
    {:ok, exprs}  = Parser.parse(tokens)
    env = Stdlib.root_env()
    Enum.reduce(exprs, {nil, env}, fn expr, {_v, e} ->
      {Evaluator.eval(expr, e), e}
    end)
    |> elem(0)
  end

  test "closures capture their definition environment" do
    result = run("""
    (define (make-adder n)
      (lambda (x) (+ x n)))
    (define add5 (make-adder 5))
    (add5 10)
    """)
    assert result == 15
  end

  test "let introduces a new scope" do
    assert run("(let ((x 3) (y 4)) (+ x y))") == 7
  end

  test "set! mutates the owning frame" do
    result = run("""
    (define x 1)
    (set! x 42)
    x
    """)
    assert result == 42
  end

  test "quote returns unevaluated structure" do
    assert run("'(a b c)") == [:a, :b, :c]
  end
end
```

### Step 10: Run the tests

```bash
mix test test/schemia/ --trace
```

### Step 11: Benchmark

```elixir
# bench/schemia_bench.exs
alias Schemia.{Lexer, Parser, Evaluator, Stdlib}

compile_and_run = fn source ->
  {:ok, tokens} = Lexer.tokenize(source)
  {:ok, exprs}  = Parser.parse(tokens)
  env = Stdlib.root_env()
  Enum.each(exprs, fn expr -> Evaluator.eval(expr, env) end)
end

fib_source = """
(define (fib n)
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2)))))
"""

compile_and_run(fib_source)

Benchee.run(
  %{
    "fib(20) — tree recursion"     => fn -> compile_and_run("(fib 20)") end,
    "fact(10000) — tail recursion" => fn ->
      compile_and_run("""
      (define (fact n acc) (if (= n 0) acc (fact (- n 1) (* n acc))))
      (fact 10000 1)
      """)
    end,
    "list construction 1000 items" => fn ->
      compile_and_run("""
      (define (build n acc) (if (= n 0) acc (build (- n 1) (cons n acc))))
      (build 1000 '())
      """)
    end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

---

## Trade-off analysis

| Aspect | Trampoline (this impl) | CPS transform | Native BEAM tail calls |
|--------|----------------------|---------------|------------------------|
| TCO mechanism | trampoline loop on heap | continuation closures | BEAM call optimization |
| Implementation complexity | moderate | high | requires compiling to real modules |
| Mutual recursion | yes (any call can trampoline) | yes | yes |
| Continuation capture (`call/cc`) | not supported | natural | not supported |
| Memory per tail call | 1 heap tuple | 1 closure allocation | 0 (overwrite stack frame) |
| Suitable for | interpreters, prototypes | full Scheme with `call/cc` | compiled targets |

Reflection: trampolining creates a `{:tail_call, expr, env}` heap tuple on every tail call. In a deeply tail-recursive loop, this allocates N tuples that are immediately collected. How does this compare to the memory cost of native tail-call optimization where the stack frame is reused?

---

## Common production mistakes

**1. Evaluating all subexpressions before detecting special forms**
Calling `Enum.map(args, &eval(&1, env))` before checking if the head is a special form (`define`, `lambda`, `if`) evaluates the arguments eagerly. Special forms must intercept the list before argument evaluation.

**2. Closure captures mutable environment reference**
If the environment is mutated after a closure is created (e.g., via `set!`), a closure that holds a reference to the parent map sees the mutation. Use persistent (copy-on-write) maps for bindings so each frame is immutable once created.

**3. Trampoline not applied to `begin` body**
`(begin expr1 expr2 expr3)` must evaluate `expr1` and `expr2` normally and put only `expr3` in tail position. Trampolining all expressions in `begin` returns the wrong value.

**4. `apply` not handling variadic argument lists**
`(apply + '(1 2 3))` must evaluate `+` and then call it with `[1, 2, 3]` as arguments. A naive implementation that always passes the last argument as a list element fails: `(apply + 1 2 '(3 4))` should call `+` with `[1, 2, 3, 4]`.

---

## Resources

- Abelson, H. & Sussman, G.J. — *Structure and Interpretation of Computer Programs* (SICP), MIT Press — the canonical reference; chapter 4 builds a metacircular evaluator
- Friedman, D.P. & Felleisen, M. — *The Little Schemer* — intuition for recursive thinking in Scheme
- R7RS Small Language Specification — [r7rs.org](https://r7rs.org) — normative reference for Scheme semantics
- Norvig, P. — [lis.py](https://norvig.com/lispy.html) — 90-line Python Scheme interpreter; the minimal reference implementation
- Queinnec, C. — *Lisp in Small Pieces* — full treatment of closures, continuations, and compilation
