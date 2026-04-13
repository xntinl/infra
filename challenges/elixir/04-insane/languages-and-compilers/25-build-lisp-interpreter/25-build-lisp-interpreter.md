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
│       ├── evaluator.ex             # eval/2: expressions x environments → values
│       ├── environment.ex           # lexical scope: linked chain of maps
│       ├── special_forms.ex         # define, lambda, if, begin, let, let*, letrec, cond, quote
│       ├── stdlib.ex                # built-in procedures: arithmetic, list ops, predicates
│       ├── tco.ex                   # trampoline: {:tail_call, expr, env} → loop
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

A Lisp interpreter appears simple: an evaluator is just `eval(expr, env)` applied recursively. The hard part is that Lisp programs are naturally tail-recursive — `(define (loop n) (loop (+ n 1)))` is perfectly valid and must run forever without growing the call stack. The BEAM supports tail calls between Elixir functions but not across the recursive `eval/2` calls inside the interpreter. Without an explicit trampolining mechanism, `(fact 1000000 1)` overflows the BEAM process stack.

---

## Why this design

**Lexical environments as linked maps**: an environment is `%{bindings: map, parent: env | nil}`. Symbol lookup walks the chain from child to parent. `define` adds to the current frame; `set!` mutates the frame that owns the binding.

**Trampoline for TCO**: instead of calling `eval/2` directly in tail position, the evaluator returns `{:tail_call, expr, env}`. The trampoline loops until it gets a non-thunk value. This converts the recursive interpreter call stack into a heap-allocated loop.

**Homoiconicity via Elixir data structures**: Lisp code and data share the same representation. A parsed s-expression `(+ 1 2)` is the Elixir list `[:+, 1, 2]`. `quote` returns the unevaluated list directly.

**REPL with paren-balance detection**: a single readline does not make a complete expression if parentheses are unbalanced. The REPL counts open/close parens and continues reading lines until balanced.

---

## Design decisions

**Option A — Compile to bytecode then interpret the bytecode**
- Pros: faster steady-state; easier to add JIT later.
- Cons: two layers to build, test, and debug; overkill for a didactic Lisp.

**Option B — Tree-walking interpreter** (chosen)
- Pros: the code maps 1:1 to the evaluation rules; closures, macros, and tail-call optimization are easy to express directly.
- Cons: slower than bytecode; not a production strategy.

→ Chose **B** because the point of this exercise is to make evaluation semantics visible — a tree walker is the most honest implementation.

## Implementation milestones

### Step 1: Create the project

**Objective**: Bootstrap a supervised Mix app with `lib/`, `test/`, and `bench/` carved out up front — every later phase drops into a slot that already exists.


```bash
mix new schemia --sup
cd schemia
mkdir -p lib/schemia test/schemia bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Benchee only in dev — the interpreter itself is stdlib-only so there is no runtime surface to audit or version.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Lexer

**Objective**: Hand-roll a character-level scanner that tags every token with its line so the parser can report errors with position, not just "bad syntax".


```elixir
# lib/schemia/lexer.ex
defmodule Schemia.Lexer do
  @moduledoc """
  Tokenizes Scheme source text into a list of tokens.
  """

  @doc "Tokenizes source code. Returns {:ok, tokens} or {:error, message}."
  @spec tokenize(String.t()) :: {:ok, [tuple()]} | {:error, String.t()}
  def tokenize(source) when is_binary(source) do
    chars = String.to_charlist(source)
    {:ok, do_tokenize(chars, 1, [])}
  end

  defp do_tokenize([], _line, acc), do: Enum.reverse(acc)

  defp do_tokenize([?\n | rest], line, acc), do: do_tokenize(rest, line + 1, acc)
  defp do_tokenize([c | rest], line, acc) when c in [?\s, ?\t, ?\r], do: do_tokenize(rest, line, acc)

  defp do_tokenize([?; | rest], line, acc) do
    remaining = Enum.drop_while(rest, &(&1 != ?\n))
    do_tokenize(remaining, line, acc)
  end

  defp do_tokenize([?( | rest], line, acc), do: do_tokenize(rest, line, [{:lparen, line} | acc])
  defp do_tokenize([?) | rest], line, acc), do: do_tokenize(rest, line, [{:rparen, line} | acc])
  defp do_tokenize([?' | rest], line, acc), do: do_tokenize(rest, line, [{:quote_shorthand, line} | acc])

  defp do_tokenize([?" | rest], line, acc) do
    {str, remaining, new_line} = read_string(rest, line, [])
    do_tokenize(remaining, new_line, [{:string, List.to_string(str), line} | acc])
  end

  defp do_tokenize([?# , ?t | rest], line, acc), do: do_tokenize(rest, line, [{:bool, true, line} | acc])
  defp do_tokenize([?# , ?f | rest], line, acc), do: do_tokenize(rest, line, [{:bool, false, line} | acc])

  defp do_tokenize([c | _] = chars, line, acc) when c in ?0..?9 or (c == ?- and length(chars) > 1) do
    {token_chars, remaining} = read_number_or_symbol(chars)
    token_str = List.to_string(token_chars)

    token =
      case Integer.parse(token_str) do
        {n, ""} -> {:integer, n, line}
        _ ->
          case Float.parse(token_str) do
            {f, ""} -> {:float, f, line}
            _ -> {:symbol, token_str, line}
          end
      end

    do_tokenize(remaining, line, [token | acc])
  end

  defp do_tokenize([c | _] = chars, line, acc) when c not in [?(, ?), ?\s, ?\t, ?\n, ?\r] do
    {sym_chars, remaining} = Enum.split_while(chars, &(&1 not in [?(, ?), ?\s, ?\t, ?\n, ?\r, ?;]))
    name = List.to_string(sym_chars)
    do_tokenize(remaining, line, [{:symbol, name, line} | acc])
  end

  defp read_string([], line, acc), do: {Enum.reverse(acc), [], line}
  defp read_string([?" | rest], line, acc), do: {Enum.reverse(acc), rest, line}
  defp read_string([?\\, ?" | rest], line, acc), do: read_string(rest, line, [?" | acc])
  defp read_string([?\\, ?\\ | rest], line, acc), do: read_string(rest, line, [?\\ | acc])
  defp read_string([?\\, ?n | rest], line, acc), do: read_string(rest, line, [?\n | acc])
  defp read_string([?\n | rest], line, acc), do: read_string(rest, line + 1, [?\n | acc])
  defp read_string([c | rest], line, acc), do: read_string(rest, line, [c | acc])

  defp read_number_or_symbol(chars) do
    Enum.split_while(chars, &(&1 not in [?(, ?), ?\s, ?\t, ?\n, ?\r, ?;]))
  end
end
```

### Step 4: Parser

**Objective**: Exploit Lisp's homoiconicity — tokens collapse directly into nested Elixir lists, so the AST is the data and no tree-type is needed.


```elixir
# lib/schemia/parser.ex
defmodule Schemia.Parser do
  @moduledoc """
  Converts a flat token list to nested Elixir structures representing s-expressions.
  """

  @doc "Parses tokens into a list of expressions."
  @spec parse([tuple()]) :: {:ok, [term()]} | {:error, {integer(), String.t()}}
  def parse(tokens) do
    {exprs, remaining} = parse_all(tokens, [])

    case remaining do
      [] -> {:ok, Enum.reverse(exprs)}
      [{:rparen, line} | _] -> {:error, {line, "unexpected )"}}
      _ -> {:ok, Enum.reverse(exprs)}
    end
  end

  defp parse_all([], acc), do: {acc, []}
  defp parse_all([{:rparen, _} | _] = tokens, acc), do: {acc, tokens}

  defp parse_all(tokens, acc) do
    {expr, rest} = read_expr(tokens)
    parse_all(rest, [expr | acc])
  end

  defp read_expr([{:lparen, _line} | rest]) do
    {elements, remaining} = read_list(rest, [])
    {elements, remaining}
  end

  defp read_expr([{:quote_shorthand, _line} | rest]) do
    {expr, remaining} = read_expr(rest)
    {[:quote, expr], remaining}
  end

  defp read_expr([{:integer, n, _} | rest]), do: {n, rest}
  defp read_expr([{:float, f, _} | rest]), do: {f, rest}
  defp read_expr([{:string, s, _} | rest]), do: {s, rest}
  defp read_expr([{:bool, b, _} | rest]), do: {b, rest}
  defp read_expr([{:symbol, name, _} | rest]), do: {String.to_atom(name), rest}

  defp read_list([{:rparen, _} | rest], acc) do
    {Enum.reverse(acc), rest}
  end

  defp read_list([], acc) do
    {Enum.reverse(acc), []}
  end

  defp read_list(tokens, acc) do
    {expr, rest} = read_expr(tokens)
    read_list(rest, [expr | acc])
  end
end
```

### Step 5: Environment

**Objective**: Represent scope as a linked chain of immutable frames so closures capture by reference and `set!` can locate the owning frame deterministically.


```elixir
# lib/schemia/environment.ex
defmodule Schemia.Environment do
  @moduledoc """
  Lexical environment as a linked chain of frames.
  """

  @doc "Creates a new environment frame."
  @spec new(map() | nil) :: map()
  def new(parent \\ nil), do: %{bindings: %{}, parent: parent}

  @doc "Creates a child frame with the given bindings."
  @spec extend(map() | nil, map()) :: map()
  def extend(parent, bindings) when is_map(bindings) do
    %{bindings: bindings, parent: parent}
  end

  @doc "Looks up a symbol in the environment chain."
  @spec lookup(map(), atom()) :: term()
  def lookup(%{bindings: bindings, parent: parent}, name) do
    case Map.fetch(bindings, name) do
      {:ok, value} -> value
      :error ->
        if parent do
          lookup(parent, name)
        else
          raise "Unbound variable: #{name}"
        end
    end
  end

  @doc "Defines a binding in the current frame. Returns updated env."
  @spec define(map(), atom(), term()) :: map()
  def define(%{bindings: bindings} = env, name, value) do
    %{env | bindings: Map.put(bindings, name, value)}
  end

  @doc "Mutates the binding in the frame that owns it."
  @spec set!(map(), atom(), term()) :: map()
  def set!(%{bindings: bindings, parent: parent} = env, name, value) do
    if Map.has_key?(bindings, name) do
      %{env | bindings: Map.put(bindings, name, value)}
    else
      if parent do
        %{env | parent: set!(parent, name, value)}
      else
        raise "Unbound variable: #{name}"
      end
    end
  end
end
```

### Step 6: TCO trampoline

**Objective**: Return `{:tail_call, expr, env}` thunks from tail positions and loop them here, so deep recursion lives on the heap instead of the BEAM call stack.


```elixir
# lib/schemia/tco.ex
defmodule Schemia.TCO do
  @moduledoc """
  Trampoline for tail call optimization.
  Converts recursive interpreter calls into a flat while-loop equivalent.
  """

  @doc "Runs the evaluator with trampolining until a final value is produced."
  @spec run(term()) :: term()
  def run({:tail_call, expr, env}) do
    result = Schemia.Evaluator.eval(expr, env)
    run(result)
  end

  def run(value), do: value
end
```

### Step 7: Evaluator

**Objective**: Pattern-match each form with its own `eval/2` clause — special forms intercepted before argument evaluation, tail positions returning thunks instead of recursing.


```elixir
# lib/schemia/evaluator.ex
defmodule Schemia.Evaluator do
  @moduledoc """
  Evaluates s-expressions in a given environment.
  Returns a value OR {:tail_call, expr, env} for TCO.
  """

  alias Schemia.{Environment, TCO}

  @doc "Evaluates an expression in the given environment."
  @spec eval(term(), map()) :: term()
  def eval(expr, env) when is_integer(expr) or is_float(expr), do: expr
  def eval(expr, env) when is_binary(expr), do: expr
  def eval(true, _env), do: true
  def eval(false, _env), do: false
  def eval(expr, env) when is_atom(expr), do: Environment.lookup(env, expr)

  def eval([:quote, x], _env), do: x

  def eval([:if, test_expr, then_expr], env) do
    if TCO.run(eval(test_expr, env)) do
      {:tail_call, then_expr, env}
    else
      nil
    end
  end

  def eval([:if, test_expr, then_expr, else_expr], env) do
    if TCO.run(eval(test_expr, env)) do
      {:tail_call, then_expr, env}
    else
      {:tail_call, else_expr, env}
    end
  end

  def eval([:define, name, body], env) when is_atom(name) do
    value = TCO.run(eval(body, env))
    Environment.define(env, name, value)
    value
  end

  def eval([:lambda, params, body], env) do
    {:closure, params, body, env}
  end

  def eval([:begin | exprs], env) do
    eval_begin(exprs, env)
  end

  def eval([:let, bindings_list, body], env) do
    new_bindings =
      Enum.reduce(bindings_list, %{}, fn [name, val_expr], acc ->
        value = TCO.run(eval(val_expr, env))
        Map.put(acc, name, value)
      end)

    child_env = Environment.extend(env, new_bindings)
    {:tail_call, body, child_env}
  end

  def eval([:"set!", name, value_expr], env) do
    value = TCO.run(eval(value_expr, env))
    Environment.set!(env, name, value)
    value
  end

  def eval([:cond | clauses], env) do
    eval_cond(clauses, env)
  end

  def eval([f_expr | arg_exprs], env) do
    func = TCO.run(eval(f_expr, env))
    args = Enum.map(arg_exprs, fn a -> TCO.run(eval(a, env)) end)
    apply_func(func, args, env)
  end

  def eval([], _env), do: []

  defp eval_begin([last], env), do: {:tail_call, last, env}

  defp eval_begin([head | tail], env) do
    TCO.run(eval(head, env))
    eval_begin(tail, env)
  end

  defp eval_cond([], _env), do: nil

  defp eval_cond([[:else | body] | _], env) do
    eval_begin(body, env)
  end

  defp eval_cond([[test | body] | rest], env) do
    if TCO.run(eval(test, env)) do
      eval_begin(body, env)
    else
      eval_cond(rest, env)
    end
  end

  defp apply_func({:closure, params, body, closure_env}, args, _call_env) do
    bindings = Enum.zip(params, args) |> Map.new()
    child_env = Environment.extend(closure_env, bindings)
    {:tail_call, body, child_env}
  end

  defp apply_func(builtin, args, _env) when is_function(builtin) do
    builtin.(args)
  end
end
```

### Step 8: Standard library

**Objective**: Expose BEAM primitives as Elixir functions bound in the root frame so stdlib calls are a plain Map lookup — no dispatch layer, no wrapping type.


```elixir
# lib/schemia/stdlib.ex
defmodule Schemia.Stdlib do
  @moduledoc """
  Built-in procedures bound in the root environment.
  """

  alias Schemia.{Environment, Evaluator, TCO}

  @doc "Returns the root environment with all standard library bindings."
  @spec root_env() :: map()
  def root_env do
    Environment.extend(nil, bindings())
  end

  defp bindings do
    %{
      :+ => fn args -> Enum.reduce(args, 0, &+/2) end,
      :- => fn [a, b] -> a - b end,
      :* => fn args -> Enum.reduce(args, 1, &*/2) end,
      :/ => fn [a, b] -> div(a, b) end,
      := => fn [a, b] -> a == b end,
      :< => fn [a, b] -> a < b end,
      :> => fn [a, b] -> a > b end,
      :<= => fn [a, b] -> a <= b end,
      :>= => fn [a, b] -> a >= b end,
      :car   => fn [[h | _]] -> h end,
      :cdr   => fn [[_ | t]] -> t end,
      :cons  => fn [h, t] -> [h | t] end,
      :list  => fn args -> args end,
      :length => fn [l] -> length(l) end,
      :append => fn [a, b] -> a ++ b end,
      :"null?"  => fn [[]] -> true; [_] -> false end,
      :"pair?"  => fn [[_ | _]] -> true; [_] -> false end,
      :"number?" => fn [n] -> is_number(n) end,
      :"symbol?" => fn [s] -> is_atom(s) end,
      :"string?" => fn [s] -> is_binary(s) end,
      :"equal?" => fn [a, b] -> a == b end,
      :not    => fn [x] -> !x end,
      :abs    => fn [x] -> abs(x) end,
      :modulo => fn [a, b] -> rem(a, b) end,
      :map => fn [func, lst] ->
        Enum.map(lst, fn item -> TCO.run(Evaluator.eval([func, [:quote, item]], Environment.new())) end)
      end,
      :filter => fn [func, lst] ->
        Enum.filter(lst, fn item -> TCO.run(Evaluator.eval([func, [:quote, item]], Environment.new())) end)
      end,
      :apply => fn [func | args] ->
        flat_args = List.flatten(args)
        TCO.run(Evaluator.eval([func | Enum.map(flat_args, fn a -> [:quote, a] end)], Environment.new()))
      end,
      :display => fn [x] -> IO.write(inspect_val(x)); x end,
      :newline => fn [] -> IO.write("\n"); :ok end
    }
  end

  defp inspect_val(x) when is_atom(x), do: Atom.to_string(x)
  defp inspect_val(x) when is_list(x), do: "(" <> Enum.map_join(x, " ", &inspect_val/1) <> ")"
  defp inspect_val(x), do: inspect(x)
end
```

### Step 9: Given tests — must pass without modification

**Objective**: Pin the public contract with a frozen suite — if the interpreter drifts, these tests are the single source of truth that call it out.


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

**Objective**: Run the suite end-to-end with `--trace` so failures name the exact stage — reader, env, eval, or trampoline — without guesswork.


```bash
mix test test/schemia/ --trace
```

### Step 11: Benchmark

**Objective**: Compare tree-recursive `fib` against tail-recursive `fact` — the former exposes allocation cost, the latter proves the trampoline stays flat.


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

compile_and_run.(fib_source)

Benchee.run(
  %{
    "fib(20) — tree recursion"     => fn -> compile_and_run.("(fib 20)") end,
    "fact(10000) — tail recursion" => fn ->
      compile_and_run.("""
      (define (fact n acc) (if (= n 0) acc (fact (- n 1) (* n acc))))
      (fact 10000 1)
      """)
    end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

### Why this works

**Evaluation semantics (denotational)**: Each AST node evaluates deterministically in an environment context. The evaluator is a function `eval : Expr × Env → Value | {tail_call, Expr, Env}`. Lexical scope is preserved by threading the environment chain — symbol lookup is a deterministic walk from the current frame to the root.

**Lisp invariant (homoiconicity)**: Programs and data have the same representation. `(quote x)` returns `x` unevaluated, making meta-programming (macros, code inspection) a first-class operation. This is enforced by the parser, which produces plain Elixir lists for s-expressions.

**Closure capture semantics**: A closure `{:closure, params, body, capture_env}` carries the environment at definition time. When applied, a child frame extends the capture environment (not the call environment), so free variables are resolved in lexical scope. Formally:

```
apply({closure, params, body, capture_env}, args, call_env) =
  let bindings = zip(params, args) in
  eval(body, extend(capture_env, bindings))
```

This ensures that `(define (make-adder n) (lambda (x) (+ x n)))` produces a closure that always refers to the original `n`, even if `n` is later shadowed in a different scope.

**Tail call optimization (trampoline)**: Instead of recursing directly, tail positions return `{:tail_call, expr, env}`. The TCO loop consumes thunks until a final value is reached:

```
run({:tail_call, expr, env}) = run(eval(expr, env))
run(value) = value
```

This converts the interpreter's call stack into a heap-allocated loop, allowing deeply recursive programs (e.g., `(fact 1000000 1)`) to run indefinitely without stack overflow.

**Unification between Elixir and Scheme semantics**: The evaluator operates on Elixir data structures (atoms, lists, numbers). Standard library functions are plain Elixir functions. This provides a transparent bridge: Lisp code can call Erlang functions directly via the stdlib, and vice versa.

---

## Benchmark workload

El benchmark debe contrastar dos patrones distintos:

1. **Tree recursion** (`fib`): expone el costo de asignación — cada llamada crea una función closure temporal.
2. **Tail recursion** (`fact`): trampolina mantiene el heap plano — sin crecimiento de stack.

```elixir
# bench/schemia_bench.exs — WORKLOAD REALISTA
alias Schemia.{Lexer, Parser, Evaluator, Stdlib, TCO}

defp eval_str(source) do
  {:ok, tokens} = Lexer.tokenize(source)
  {:ok, exprs}  = Parser.parse(tokens)
  env = Stdlib.root_env()
  Enum.reduce(exprs, {nil, env}, fn expr, {_v, e} ->
    {TCO.run(Evaluator.eval(expr, e)), e}
  end)
  |> elem(0)
end

# Define functions once
eval_str("""
(define (fib n)
  (if (< n 2)
    n
    (+ (fib (- n 1)) (fib (- n 2)))))
(define (fact n acc)
  (if (= n 0)
    acc
    (fact (- n 1) (* n acc))))
""")

Benchee.run(
  %{
    "fib(20) — tree recursion, 2^20 allocations" => fn -> eval_str("(fib 20)") end,
    "fact(1000) — tail recursion, O(1) stack" => fn -> eval_str("(fact 1000 1)") end,
    "arithmetic x100 — (+ 1 2 3 ... 100)" => fn -> eval_str("(+ 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20)") end,
    "list cons x50 — build list recursively" => fn -> 
      eval_str("""
      (define (build-list n acc)
        (if (= n 0)
          acc
          (build-list (- n 1) (cons n acc))))
      (build-list 50 '())
      """)
    end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

**Targets**:
- `fib(20)`: < 10 ms (tree recursion overhead es esperado)
- `fact(1000)`: < 1 ms (trampolina sin crecimiento de stack)
- Simple arithmetic: < 100 µs por operación
- List construction: O(n) sin exponential blowup

---

## Deep Dive: NIF Callbacks and BEAM Scheduling Implications

Native Implemented Functions (NIFs) are C/Rust code called from Elixir. They are fast (no VM overhead) but dangerous: a blocking NIF blocks the entire BEAM scheduler thread, starving all other processes.

**The BEAM scheduler model**: One OS thread per logical scheduler (typically one per CPU core). When a process calls a NIF, the thread executes C code. If the NIF blocks (e.g., calling `read()` on a socket), the thread is blocked, and all 100+ other processes on that scheduler are frozen.

**Problem**: A Rustler NIF that calls `std::fs::File::open()` or `std::net::TcpStream::connect()` may block. Meanwhile, unrelated Erlang processes starve.

**Solutions**:
1. **Dirty schedulers**: Mark the NIF as `:dirty_io` or `:dirty_cpu`. The BEAM reserves separate threads for blocking work. The calling process is moved off the main scheduler; others continue. Trade-off: dirty threads are a limited resource; over-subscribe and throughput drops.
2. **Async via thread pool**: Spawn a C thread from a thread pool, return immediately, and notify Erlang via callback. Complex but non-blocking.
3. **Never block in NIFs**: Only call non-blocking C functions (pure computation, hash functions). Delegate I/O to Erlang processes.

Rustler's `:dirty_io` attribute automates dirty scheduler mapping. For a Rust function that calls a blocking OS API:

```rust
pub fn expensive_operation(a: u32) -> u32 {
    // Blocking operations here, will run on dirty_io scheduler
    std::thread::sleep(Duration::from_secs(1));
    a + 1
}
#[rustler::nif(schedule = "DirtyIo")]
pub fn expensive_operation_nif(a: u32) -> u32 { expensive_operation(a) }
```

**Production pattern**: Reserve dirty schedulers for truly blocking I/O. Measure scheduler utilization to confirm no starvation under load. Prefer Elixir async processes for I/O when possible; they are more observable and composable.

---

## Trade-off analysis

| Aspect | Trampoline (this impl) | CPS transform | Native BEAM tail calls |
|--------|----------------------|---------------|------------------------|
| TCO mechanism | trampoline loop | continuation closures | BEAM call optimization |
| Implementation complexity | moderate | high | requires compiling to modules |
| Mutual recursion | yes | yes | yes |
| Memory per tail call | 1 heap tuple | 1 closure allocation | 0 (overwrite stack frame) |

Reflection: trampolining creates a `{:tail_call, expr, env}` heap tuple on every tail call. How does this compare to native tail-call optimization where the stack frame is reused?

---

## Common production mistakes

**1. Evaluating all subexpressions before detecting special forms**
Special forms like `if`, `lambda`, `quote` must intercept the s-expression before evaluating arguments. A naive `eval([f | args], env) = apply(eval(f), map(eval, args))` evaluates `quote` as an argument, destroying its unevaluated value. Solution: match special form symbols first, before recursing.

**2. Closure captures mutable environment reference**
If environments are mutable Agents or ETS tables, closures will observe state changes made after the closure was created. Scheme semantics require closures to capture the environment snapshot at definition time. Solution: use immutable persistent maps with structural sharing. The parent pointer makes the chain efficient; child frames are lightweight.

**3. Trampoline not applied consistently to all tail positions**
`(begin expr1 expr2 expr3)` must return a thunk only for `expr3`. The wrong approach:
```elixir
eval([:begin | body], env) do
  Enum.each(body, &eval/2)  # BUG: evaluates everything eagerly, not in tail position
end
```
Correct:
```elixir
defp eval_begin([last], env), do: {:tail_call, last, env}
defp eval_begin([head | tail], env) do
  TCO.run(eval(head, env))
  eval_begin(tail, env)
end
```

**4. `apply` not handling variadic argument lists correctly**
`(apply + 1 2 '(3 4))` should call `+` with `[1, 2, 3, 4]`, not `[1, 2, [3, 4]]`. The last argument is a list that must be flattened into the call.

**5. `set!` mutating the wrong frame**
`set!` must locate the *first* frame up the chain that owns the binding and mutate that frame. A naive implementation that mutates the current frame breaks closure correctness:
```scheme
(define (make-counter)
  (let ((n 0))
    (lambda () (set! n (+ n 1)) n)))
```
Both closures returned must mutate the same `n`. Solution: `set!` recursively walks the parent chain until `has_key?(bindings, name)` is true.

**6. Parser not distinguishing symbols from keywords**
`'(define x 1)` is a quoted list; `define` should parse as a symbol, not a keyword. Keywords like `define` are only special in the function position of an unquoted s-expression.

**7. Type confusion between the value `nil` and the empty list `()`**
Scheme represents both as `'()` (empty list), but implementations sometimes conflate them with `false` or `null`. This breaks `(null? '())` and predicates.

## Deep dive: Exceptions vs. non-local exits

Lisp's `catch` and `throw` are non-local control flow operators that bubble an exception up the call stack until a matching `catch` catches it. Implementing this requires either:

1. **Exception-based approach**: raise Elixir exceptions at `throw` and catch them in `catch`.
2. **Continuation-passing style**: each thunk carries a "current exception handler" and `throw` bypasses the normal return path.

The exception approach is simpler and aligns with BEAM semantics. The challenge is ensuring that cleanup code (finalizers) runs on the unwind path — Scheme's `dynamic-wind` guarantees this.

## Reflection

- If you added a bytecode compiler, which Lisp features would force non-trivial encoding decisions?
  - **`call/cc`**: capturing first-class continuations requires the bytecode to represent the call stack as a data structure, not rely on the machine's stack. This forces a virtual stack machine.
  - **`dynamic-wind`**: requires exception handlers to be represented as metadata on activation frames, so unwinding can trigger cleanup callbacks.
- How would you add tail-call optimization without trampolines?
  - **CPS transformation**: convert the entire program to continuation-passing style before evaluating. Each function takes an explicit continuation parameter. Tail calls pass control directly to the continuation.
  - **Bytecode with explicit return codes**: emit instructions like `TAIL_CALL` that signal the BEAM VM to reuse the current stack frame. This requires compiling down to Erlang functions, not interpreting.
  - **Analysis + annotation**: mark which calls are in tail position during parsing, then handle them specially in the evaluator (current approach, but explicit).

---

## Resources

- Abelson, H. & Sussman, G.J. — *Structure and Interpretation of Computer Programs* (SICP)
- Friedman, D.P. & Felleisen, M. — *The Little Schemer*
- R7RS Small Language Specification — [r7rs.org](https://r7rs.org)
- Norvig, P. — [lis.py](https://norvig.com/lispy.html) — 90-line Python Scheme interpreter
- Queinnec, C. — *Lisp in Small Pieces*
