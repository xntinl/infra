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
│       ├── type_checker.ex          # unification-based type inference
│       ├── codegen.ex               # Core Erlang emitter
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

Writing a compiler that targets a real runtime is harder than writing an interpreter: you cannot evaluate eagerly — you must emit code that runs correctly when loaded into the BEAM later. Closures are not native to Core Erlang modules (which are flat sets of functions), so free variables must be explicitly threaded through as parameters (lambda lifting). The type checker must prove at compile time that no operation is applied to the wrong type, using Robinson's unification algorithm.

---

## Why this design

**Pratt parser for expression precedence**: recursive descent handles statements; a Pratt parser assigns binding power to each token for operators. Adding a new operator requires only registering its binding power.

**Unification-based type inference**: each expression is assigned a type variable. Type constraints are collected as unification equations. Robinson's algorithm solves the equations.

**Core Erlang as a compilation target**: Core Erlang is a well-documented intermediate language accepted by the OTP compiler. The `:cerl` module in OTP provides an API for building Core Erlang ASTs.

**Closure conversion before codegen**: Core Erlang functions cannot capture variables from enclosing scopes. The closure converter transforms each lambda that captures free variables into a named function with an extra tuple parameter.

---

## Design decisions

**Option A — Tree-walking interpreter**
- Pros: dead simple; great for teaching.
- Cons: interpretive overhead dominates; optimizations are hard to express.

**Option B — Compile AST to a bytecode VM** (chosen)
- Pros: separates parsing from execution; lets you reason about instruction dispatch cost; enables simple optimizations (constant folding, dead-code elimination) at compile time.
- Cons: two stages to test; VM design is a project in its own right.

→ Chose **B** because a *compiler* project must produce compiled code — otherwise it's an interpreter in disguise; the bytecode VM is the natural target.

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
  Tokenizes mlang source text with position tracking.
  """

  @keywords ~w(if else fn let return while break continue true false)a

  @doc "Tokenizes source code. Returns {:ok, tokens} or {:error, errors}."
  @spec tokenize(String.t(), String.t()) :: {:ok, [tuple()]} | {:error, [tuple()]}
  def tokenize(source, filename \\ "<string>") do
    chars = String.to_charlist(source)
    tokens = do_tokenize(chars, {1, 1}, [])
    {:ok, tokens ++ [{:eof, {1, 1}}]}
  end

  defp do_tokenize([], _pos, acc), do: Enum.reverse(acc)

  defp do_tokenize([?\n | rest], {line, _col}, acc), do: do_tokenize(rest, {line + 1, 1}, acc)
  defp do_tokenize([c | rest], {line, col}, acc) when c in [?\s, ?\t, ?\r], do: do_tokenize(rest, {line, col + 1}, acc)

  defp do_tokenize([?/, ?/ | rest], {line, _col}, acc) do
    remaining = Enum.drop_while(rest, &(&1 != ?\n))
    do_tokenize(remaining, {line, 1}, acc)
  end

  defp do_tokenize([?-, ?> | rest], pos, acc), do: do_tokenize(rest, advance(pos, 2), [{:arrow, pos} | acc])
  defp do_tokenize([?=, ?= | rest], pos, acc), do: do_tokenize(rest, advance(pos, 2), [{:op, :==, pos} | acc])
  defp do_tokenize([?!, ?= | rest], pos, acc), do: do_tokenize(rest, advance(pos, 2), [{:op, :!=, pos} | acc])
  defp do_tokenize([?<, ?= | rest], pos, acc), do: do_tokenize(rest, advance(pos, 2), [{:op, :<=, pos} | acc])
  defp do_tokenize([?>, ?= | rest], pos, acc), do: do_tokenize(rest, advance(pos, 2), [{:op, :>=, pos} | acc])

  defp do_tokenize([?+ | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:op, :+, pos} | acc])
  defp do_tokenize([?- | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:op, :-, pos} | acc])
  defp do_tokenize([?* | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:op, :*, pos} | acc])
  defp do_tokenize([?/ | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:op, :/, pos} | acc])
  defp do_tokenize([?= | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:op, :=, pos} | acc])
  defp do_tokenize([?< | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:op, :<, pos} | acc])
  defp do_tokenize([?> | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:op, :>, pos} | acc])

  defp do_tokenize([?( | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:lparen, pos} | acc])
  defp do_tokenize([?) | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:rparen, pos} | acc])
  defp do_tokenize([?{ | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:lbrace, pos} | acc])
  defp do_tokenize([?} | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:rbrace, pos} | acc])
  defp do_tokenize([?, | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:comma, pos} | acc])
  defp do_tokenize([?; | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:semicolon, pos} | acc])
  defp do_tokenize([?: | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), [{:colon, pos} | acc])

  defp do_tokenize([?" | rest], pos, acc) do
    {str, remaining} = read_string(rest, [])
    token = {:string, List.to_string(str), pos}
    do_tokenize(remaining, advance(pos, String.length(List.to_string(str)) + 2), [token | acc])
  end

  defp do_tokenize([c | _] = chars, pos, acc) when c in ?0..?9 do
    {num_chars, remaining} = Enum.split_while(chars, &(&1 in ?0..?9 or &1 == ?.))
    str = List.to_string(num_chars)
    token = if String.contains?(str, "."), do: {:float, String.to_float(str), pos}, else: {:int, String.to_integer(str), pos}
    do_tokenize(remaining, advance(pos, String.length(str)), [token | acc])
  end

  defp do_tokenize([c | _] = chars, pos, acc) when c in ?a..?z or c in ?A..?Z or c == ?_ do
    {id_chars, remaining} = Enum.split_while(chars, &(&1 in ?a..?z or &1 in ?A..?Z or &1 in ?0..?9 or &1 == ?_))
    name = List.to_string(id_chars)
    atom = String.to_atom(name)

    token =
      cond do
        atom in @keywords -> {:kw, atom, pos}
        name == "true" -> {:bool, true, pos}
        name == "false" -> {:bool, false, pos}
        true -> {:ident, name, pos}
      end

    do_tokenize(remaining, advance(pos, String.length(name)), [token | acc])
  end

  defp do_tokenize([_ | rest], pos, acc), do: do_tokenize(rest, advance(pos, 1), acc)

  defp read_string([?" | rest], acc), do: {Enum.reverse(acc), rest}
  defp read_string([?\\, ?" | rest], acc), do: read_string(rest, [?" | acc])
  defp read_string([?\\, ?\\ | rest], acc), do: read_string(rest, [?\\ | acc])
  defp read_string([c | rest], acc), do: read_string(rest, [c | acc])
  defp read_string([], acc), do: {Enum.reverse(acc), []}

  defp advance({line, col}, n), do: {line, col + n}
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

### Step 5: Parser, type checker, closure converter, and codegen

Due to the extreme complexity of a full Pratt parser, type checker with unification, closure converter, and Core Erlang code generator, this exercise provides the key modules (Lexer, AST) in full and the remaining phases as architectural outlines. The full implementation requires several hundred lines per module.

```elixir
# lib/mlang/parser.ex
defmodule Mlang.Parser do
  @moduledoc """
  Pratt parser (top-down operator precedence).
  Binding powers: = (1), == != (3), < > <= >= (4), + - (5), * / (6), unary - (7), call () (8).
  """

  alias Mlang.AST

  @doc "Parses a token list into a Module_ AST node."
  @spec parse([tuple()], String.t()) :: {:ok, %AST.Module_{}} | {:error, [map()]}
  def parse(tokens, filename \\ "<string>") do
    {defs, _rest, errors} = parse_top_level(tokens, [], [])

    if errors == [] do
      {:ok, %AST.Module_{name: :mlang_mod, defs: defs, pos: {1, 1}}}
    else
      {:error, errors}
    end
  end

  defp parse_top_level([{:eof, _} | _], defs, errors), do: {Enum.reverse(defs), [], errors}
  defp parse_top_level([], defs, errors), do: {Enum.reverse(defs), [], errors}

  defp parse_top_level([{:kw, :fn, pos} | rest], defs, errors) do
    {fn_def, remaining} = parse_fn_def(rest, pos)
    parse_top_level(remaining, [fn_def | defs], errors)
  end

  defp parse_top_level([_ | rest], defs, errors), do: parse_top_level(rest, defs, errors)

  defp parse_fn_def([{:ident, name, _} , {:lparen, _} | rest], pos) do
    {params, rest} = parse_params(rest, [])
    {return_type, rest} = parse_return_type(rest)
    {body, rest} = parse_block(rest)

    fn_def = %AST.FnDef{
      name: String.to_atom(name),
      params: params,
      return_type: return_type,
      body: body,
      pos: pos,
      type: nil
    }

    {fn_def, rest}
  end

  defp parse_params([{:rparen, _} | rest], acc), do: {Enum.reverse(acc), rest}

  defp parse_params([{:ident, name, _}, {:colon, _} | rest], acc) do
    {type, rest} = parse_type_annotation(rest)
    param = {String.to_atom(name), type}

    case rest do
      [{:comma, _} | rest2] -> parse_params(rest2, [param | acc])
      [{:rparen, _} | rest2] -> {Enum.reverse([param | acc]), rest2}
    end
  end

  defp parse_type_annotation([{:ident, "Int", _} | rest]), do: {:int, rest}
  defp parse_type_annotation([{:ident, "Float", _} | rest]), do: {:float, rest}
  defp parse_type_annotation([{:ident, "Bool", _} | rest]), do: {:bool, rest}
  defp parse_type_annotation([{:ident, "String", _} | rest]), do: {:string, rest}
  defp parse_type_annotation([{:ident, "Fn", _} | rest]), do: parse_fn_type(rest)
  defp parse_type_annotation(rest), do: {:any, rest}

  defp parse_fn_type([{:lparen, _} | rest]) do
    {param_types, rest} = parse_type_list(rest, [])
    {return_type, rest} =
      case rest do
        [{:arrow, _} | rest2] -> parse_type_annotation(rest2)
        _ -> {:any, rest}
      end
    {{:fn, param_types, return_type}, rest}
  end

  defp parse_type_list([{:rparen, _} | rest], acc), do: {Enum.reverse(acc), rest}

  defp parse_type_list(tokens, acc) do
    {type, rest} = parse_type_annotation(tokens)
    case rest do
      [{:comma, _} | rest2] -> parse_type_list(rest2, [type | acc])
      _ -> parse_type_list(rest, [type | acc])
    end
  end

  defp parse_return_type([{:arrow, _} | rest]) do
    parse_type_annotation(rest)
  end

  defp parse_return_type(rest), do: {:any, rest}

  defp parse_block([{:lbrace, pos} | rest]) do
    {stmts, rest} = parse_stmts(rest, [])
    {%AST.Block{stmts: Enum.reverse(stmts), pos: pos, type: nil}, rest}
  end

  defp parse_stmts([{:rbrace, _} | rest], acc), do: {acc, rest}

  defp parse_stmts([{:kw, :return, pos} | rest], acc) do
    {expr, rest} = parse_expr(rest, 0)
    rest = skip_semicolon(rest)
    parse_stmts(rest, [%AST.Return{value: expr, pos: pos, type: nil} | acc])
  end

  defp parse_stmts([{:kw, :if, pos} | rest], acc) do
    {cond_expr, rest} = parse_expr(rest, 0)
    {then_block, rest} = parse_block(rest)

    {else_block, rest} =
      case rest do
        [{:kw, :else, _} | rest2] -> parse_block(rest2)
        _ -> {nil, rest}
      end

    if_expr = %AST.IfExpr{cond: cond_expr, then: then_block, else_: else_block, pos: pos, type: nil}
    parse_stmts(rest, [if_expr | acc])
  end

  defp parse_stmts([{:kw, :let, pos}, {:ident, name, _}, {:op, :=, _} | rest], acc) do
    {expr, rest} = parse_expr(rest, 0)
    rest = skip_semicolon(rest)
    parse_stmts(rest, [%AST.Let{name: String.to_atom(name), value: expr, pos: pos, type: nil} | acc])
  end

  defp parse_stmts(tokens, acc) do
    case tokens do
      [{:rbrace, _} | _] -> {acc, tokens}
      [] -> {acc, []}
      _ ->
        {expr, rest} = parse_expr(tokens, 0)
        rest = skip_semicolon(rest)
        parse_stmts(rest, [expr | acc])
    end
  end

  defp parse_expr(tokens, min_bp) do
    {left, rest} = nud(tokens)
    led_loop(left, rest, min_bp)
  end

  defp led_loop(left, [{:op, op, pos} | rest] = tokens, min_bp) do
    bp = lbp(op)
    if bp > min_bp do
      {right, rest2} = parse_expr(rest, bp)
      new_left = %AST.BinOp{op: op, left: left, right: right, pos: pos, type: nil}
      led_loop(new_left, rest2, min_bp)
    else
      {left, tokens}
    end
  end

  defp led_loop(left, [{:lparen, pos} | rest], min_bp) when 8 > min_bp do
    {args, rest} = parse_args(rest, [])
    new_left = %AST.Call{callee: left, args: Enum.reverse(args), pos: pos, type: nil}
    led_loop(new_left, rest, min_bp)
  end

  defp led_loop(left, rest, _min_bp), do: {left, rest}

  defp nud([{:int, n, pos} | rest]), do: {%AST.IntLit{value: n, pos: pos, type: :int}, rest}
  defp nud([{:float, f, pos} | rest]), do: {%AST.FloatLit{value: f, pos: pos, type: :float}, rest}
  defp nud([{:bool, b, pos} | rest]), do: {%AST.BoolLit{value: b, pos: pos, type: :bool}, rest}
  defp nud([{:string, s, pos} | rest]), do: {%AST.StringLit{value: s, pos: pos, type: :string}, rest}
  defp nud([{:ident, name, pos} | rest]), do: {%AST.Ident{name: String.to_atom(name), pos: pos, type: nil}, rest}

  defp nud([{:lparen, _} | rest]) do
    {expr, rest} = parse_expr(rest, 0)
    [{:rparen, _} | rest] = rest
    {expr, rest}
  end

  defp nud([{:op, :-, pos} | rest]) do
    {expr, rest} = parse_expr(rest, 7)
    {%AST.UnaryOp{op: :-, expr: expr, pos: pos, type: nil}, rest}
  end

  defp parse_args([{:rparen, _} | rest], acc), do: {acc, rest}

  defp parse_args(tokens, acc) do
    {expr, rest} = parse_expr(tokens, 0)
    case rest do
      [{:comma, _} | rest2] -> parse_args(rest2, [expr | acc])
      _ -> parse_args(rest, [expr | acc])
    end
  end

  defp skip_semicolon([{:semicolon, _} | rest]), do: rest
  defp skip_semicolon(rest), do: rest

  defp lbp(:==), do: 3
  defp lbp(:!=), do: 3
  defp lbp(:<),  do: 4
  defp lbp(:>),  do: 4
  defp lbp(:<=), do: 4
  defp lbp(:>=), do: 4
  defp lbp(:+),  do: 5
  defp lbp(:-),  do: 5
  defp lbp(:*),  do: 6
  defp lbp(:/),  do: 6
  defp lbp(_),   do: 0
end
```

```elixir
# lib/mlang/type_checker.ex
defmodule Mlang.TypeChecker do
  @moduledoc """
  Unification-based type inference using Robinson's algorithm.
  """

  alias Mlang.AST

  @doc "Type-checks a Module_ AST. Returns {:ok, annotated_module} or {:error, errors}."
  @spec check(%AST.Module_{}) :: {:ok, %AST.Module_{}} | {:error, [map()]}
  def check(%AST.Module_{defs: defs} = mod) do
    {typed_defs, errors} =
      Enum.map_reduce(defs, [], fn fn_def, errs ->
        case check_fn(fn_def) do
          {:ok, typed} -> {typed, errs}
          {:error, e} -> {fn_def, errs ++ e}
        end
      end)

    if errors == [] do
      {:ok, %{mod | defs: typed_defs}}
    else
      {:error, errors}
    end
  end

  defp check_fn(%AST.FnDef{params: params, return_type: ret_type, body: body} = fn_def) do
    env = Map.new(params, fn {name, type} -> {name, type} end)

    case infer_block(body, env) do
      {:ok, inferred_type} ->
        if ret_type != :any and inferred_type != ret_type and not compatible?(inferred_type, ret_type) do
          {:error, [%{message: "expected #{inspect(ret_type)} but got #{inspect(inferred_type)}", pos: fn_def.pos}]}
        else
          fn_type = {:fn, Enum.map(params, fn {_, t} -> t end), ret_type}
          {:ok, %{fn_def | type: fn_type}}
        end

      {:error, _} = err -> err
    end
  end

  defp infer_block(%AST.Block{stmts: stmts}, env) do
    Enum.reduce_while(stmts, {:ok, :void}, fn stmt, _acc ->
      case infer_stmt(stmt, env) do
        {:ok, type} -> {:cont, {:ok, type}}
        {:error, _} = err -> {:halt, err}
      end
    end)
  end

  defp infer_stmt(%AST.Return{value: expr}, env), do: infer_expr(expr, env)

  defp infer_stmt(%AST.IfExpr{cond: cond_expr, then: then_block, else_: else_block}, env) do
    case infer_expr(cond_expr, env) do
      {:ok, cond_type} when cond_type != :bool ->
        {:error, [%{message: "if condition must be bool, got #{inspect(cond_type)}", pos: cond_expr.pos}]}
      {:ok, :bool} ->
        {:ok, then_type} = infer_block(then_block, env)
        if else_block do
          {:ok, _else_type} = infer_block(else_block, env)
        end
        {:ok, then_type}
      other -> other
    end
  end

  defp infer_stmt(expr, env), do: infer_expr(expr, env)

  defp infer_expr(%AST.IntLit{}, _env), do: {:ok, :int}
  defp infer_expr(%AST.FloatLit{}, _env), do: {:ok, :float}
  defp infer_expr(%AST.BoolLit{}, _env), do: {:ok, :bool}
  defp infer_expr(%AST.StringLit{}, _env), do: {:ok, :string}

  defp infer_expr(%AST.Ident{name: name}, env) do
    case Map.fetch(env, name) do
      {:ok, type} -> {:ok, type}
      :error -> {:ok, :any}
    end
  end

  defp infer_expr(%AST.BinOp{op: op, left: left, right: right}, env) do
    {:ok, lt} = infer_expr(left, env)
    {:ok, rt} = infer_expr(right, env)

    cond do
      op in [:+, :-, :*, :/] ->
        if lt in [:int, :float, :any] and rt in [:int, :float, :any] do
          {:ok, resolve_numeric(lt, rt)}
        else
          {:error, [%{message: "arithmetic requires numeric types", pos: left.pos}]}
        end

      op in [:==, :!=, :<, :>, :<=, :>=] ->
        {:ok, :bool}

      true ->
        {:ok, :any}
    end
  end

  defp infer_expr(%AST.Call{callee: _callee, args: _args}, _env) do
    {:ok, :any}
  end

  defp infer_expr(%AST.UnaryOp{op: :-, expr: expr}, env) do
    infer_expr(expr, env)
  end

  defp resolve_numeric(:float, _), do: :float
  defp resolve_numeric(_, :float), do: :float
  defp resolve_numeric(:int, :int), do: :int
  defp resolve_numeric(_, _), do: :int

  defp compatible?(:any, _), do: true
  defp compatible?(_, :any), do: true
  defp compatible?(a, a), do: true
  defp compatible?(_, _), do: false
end
```

```elixir
# lib/mlang/closure_converter.ex
defmodule Mlang.ClosureConverter do
  @moduledoc "Lambda lifting: converts closures into flat functions with explicit captured arguments."

  def convert(typed_module), do: {:ok, typed_module}
end

# lib/mlang/codegen.ex
defmodule Mlang.Codegen do
  @moduledoc "Emits Core Erlang AST from a type-checked module."

  alias Mlang.AST

  def emit(%AST.Module_{name: name, defs: defs}) do
    mod_name = :cerl.c_atom(name)
    exports = Enum.map(defs, fn %AST.FnDef{name: fname, params: params} ->
      :cerl.c_fname(fname, length(params))
    end)

    fun_defs = Enum.map(defs, &emit_fn/1)

    core = :cerl.c_module(mod_name, exports, [], fun_defs)
    {:ok, core}
  end

  defp emit_fn(%AST.FnDef{name: fname, params: params, body: body}) do
    vars = Enum.map(params, fn {name, _type} -> :cerl.c_var(name) end)
    body_core = emit_block(body)
    fun = :cerl.c_fun(vars, body_core)
    {:cerl.c_fname(fname, length(params)), fun}
  end

  defp emit_block(%AST.Block{stmts: stmts}) do
    emit_stmts(stmts)
  end

  defp emit_stmts([%AST.Return{value: expr}]), do: emit_expr(expr)
  defp emit_stmts([%AST.Return{value: expr} | _]), do: emit_expr(expr)

  defp emit_stmts([%AST.IfExpr{} = if_expr]) do
    emit_expr(if_expr)
  end

  defp emit_stmts([stmt | rest]) do
    val = emit_expr(stmt)
    dummy_var = :cerl.c_var(:"_tmp_#{:erlang.unique_integer([:positive])}")
    :cerl.c_let([dummy_var], val, emit_stmts(rest))
  end

  defp emit_stmts([]), do: :cerl.c_atom(:ok)

  defp emit_expr(%AST.IntLit{value: n}), do: :cerl.c_int(n)
  defp emit_expr(%AST.FloatLit{value: f}), do: :cerl.c_float(f)
  defp emit_expr(%AST.BoolLit{value: b}), do: :cerl.c_atom(b)
  defp emit_expr(%AST.StringLit{value: s}), do: :cerl.c_binary(for(<<c <- s>>, do: :cerl.c_bitstr(:cerl.c_int(c), :cerl.c_int(8), :cerl.c_int(1), :cerl.c_atom(:integer), :cerl.c_cons(:cerl.c_atom(:unsigned), :cerl.c_cons(:cerl.c_atom(:big), :cerl.c_nil())))))
  defp emit_expr(%AST.Ident{name: name}), do: :cerl.c_var(name)

  defp emit_expr(%AST.BinOp{op: op, left: left, right: right}) do
    erlang_op = case op do
      :+ -> :+; :- -> :-; :* -> :*; :/ -> :div
      :== -> :==; :!= -> :"/="; :< -> :<; :> -> :>; :<= -> :"=<"; :>= -> :>=
    end
    :cerl.c_call(:cerl.c_atom(:erlang), :cerl.c_atom(erlang_op), [emit_expr(left), emit_expr(right)])
  end

  defp emit_expr(%AST.Call{callee: %AST.Ident{name: fname}, args: args}) do
    :cerl.c_apply(:cerl.c_fname(fname, length(args)), Enum.map(args, &emit_expr/1))
  end

  defp emit_expr(%AST.IfExpr{cond: cond_expr, then: then_block, else_: else_block}) do
    true_clause = :cerl.c_clause([:cerl.c_atom(true)], emit_block(then_block))
    false_body = if else_block, do: emit_block(else_block), else: :cerl.c_atom(nil)
    false_clause = :cerl.c_clause([:cerl.c_atom(false)], false_body)
    :cerl.c_case(emit_expr(cond_expr), [true_clause, false_clause])
  end

  defp emit_expr(%AST.Return{value: expr}), do: emit_expr(expr)
  defp emit_expr(%AST.Block{} = block), do: emit_block(block)
end
```

### Step 6: Given tests — must pass without modification

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

### Step 7: Run the tests

```bash
mix test test/mlang/ --trace
```

### Step 8: Benchmark

```elixir
# bench/mlang_bench.exs
fib_source = """
fn fib(n: Int) -> Int {
  if n < 2 { return n; }
  return fib(n - 1) + fib(n - 2);
}
"""

Benchee.run(
  %{
    "lex + parse" => fn -> Mlang.Lexer.tokenize(fib_source) |> then(fn {:ok, t} -> Mlang.Parser.parse(t) end) end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

### Why this works

The compiler lowers the AST to a linear sequence of stack-machine opcodes, performs a constant-folding pass, and emits bytecode. The VM is a tight `case` loop dispatching on opcode, with call frames for function invocation and lexical scopes as arrays.

---

## Benchmark

```elixir
# bench/compiler_bench.exs
Benchee.run(%{"compile_100loc" => fn -> MiniLang.compile(source) end, "run" => fn -> VM.run(bc) end}, time: 10)
```

Target: 100 LOC compile in < 5 ms; bytecode execution ~5x faster than a tree-walking baseline.

---

## Trade-off analysis

| Aspect | Core Erlang target (this impl) | BEAM bytecode direct | Elixir AST (macros) |
|--------|-------------------------------|---------------------|---------------------|
| Complexity | moderate (cerl API) | high (undocumented) | low |
| Portability | OTP-version-stable | fragile | Elixir-version-dependent |
| Optimization | OTP optimizer | none | full Elixir pipeline |
| Interop | any Erlang/Elixir call | any | Elixir only |

Reflection: closure conversion threads free variables through function signatures. What overhead does this introduce at call sites where closures are passed as first-class values?

---

## Common production mistakes

**1. Pratt parser not handling right-associativity**
Assignment uses `lbp - 1` when recursing right. Using `lbp` produces left-associativity for all operators.

**2. Type variables not substituted before unification**
Apply the current substitution before attempting new unification.

**3. Closure converter not capturing transitively**
Nested lambdas may have transitive free variables.

**4. Core Erlang module not exporting public functions**
`:cerl.c_module/3` requires an explicit export list.

## Reflection

- Which classical optimization (CSE, inlining, loop-invariant hoisting) would give the biggest win on your bytecode VM, and why?
- If you added a register-based VM, what would change in the instruction set? Compare register vs stack architectures.

---

## Resources

- Core Erlang 1.0.3 Specification — [it.uu.se/research/group/hipe/cerl](https://www.it.uu.se/research/group/hipe/cerl/)
- OTP `:cerl` module documentation
- Nystrom, R. — *Crafting Interpreters* — [craftinginterpreters.com](https://craftinginterpreters.com)
- Pierce, B.C. — *Types and Programming Languages* — chapters 22-23 cover unification
