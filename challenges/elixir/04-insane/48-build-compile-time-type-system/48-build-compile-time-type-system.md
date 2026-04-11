# Compile-time Type System for Elixir Macros

**Project**: `type_check` — Compile-time type annotation macro that raises CompileError on type mismatches

## Project context

Your team maintains a large Elixir codebase. Dialyzer catches some type bugs but runs after compilation in a separate pass. When a developer passes a string to a function expecting an integer, the error appears at runtime in production, not at compile time in the editor. The team wants a lightweight annotation system that catches obvious type mismatches during `mix compile`.

You will build `TypeCheck`: a compile-time type annotation macro that raises `CompileError` with precise file and line information when function argument types or return types are violated. No runtime overhead. No external processes. Pure macro expansion.

## Why build a type checker as macros and not as a Mix task or Dialyzer plugin

A Mix task or Dialyzer plugin runs after compilation. The compilation has already succeeded — you cannot prevent a binary from being built. Macros run during compilation. A `CompileError` raised inside a macro stops the compilation of the current module immediately, with the file path and line number extracted from AST metadata. The developer sees the error in the same `mix compile` run as a syntax error.

The trade-off: macros only see the AST of the current module, not of callers. You can verify that the function body is consistent with its annotation, and you can verify call sites where the argument types are statically known (literals, immediately preceding assignments). You cannot track types across module boundaries without a global type database — that is what Dialyzer's PLT is.

## Why type inference is bottom-up (post-order AST traversal)

To infer the type of `x + 1`, you need to know the types of `x` and `1` first. The type of a parent node depends on the types of its children. This is bottom-up inference: `Macro.postwalk/3` annotates leaf nodes first, then propagates up. `Macro.prewalk/3` would be used for top-down propagation (applying known types down from annotations into sub-expressions).

## Why Hindley-Milner is overkill for this exercise

Full HM inference solves type equations globally, including across function calls with polymorphic functions. For single-function body inference (which is all we implement here), a simpler constraint-propagation approach suffices: walk the AST bottom-up, infer a type for each node, and check it against the declared type at the return position. Unification is only needed for generic type variables (`List.t(T)`).

## Project Structure

```
type_check/
├── mix.exs
├── lib/
│   ├── type_check/
│   │   ├── types.ex           # Type algebra: primitives, union, generic, never, any
│   │   ├── infer.ex           # AST type inference: postwalk + type annotation
│   │   ├── check.ex           # Type compatibility: subtype?, unify/2
│   │   ├── error.ex           # CompileError construction with file/line/expression
│   │   ├── spec.ex            # @spec generation from TypeCheck annotations
│   │   └── annotation.ex      # Parse @type_checked annotation from def AST
│   └── type_check.ex          # Public macro: __using__, @type_checked
├── test/
│   ├── types_test.exs
│   ├── infer_test.exs
│   ├── check_test.exs
│   └── integration_test.exs
└── bench/
    └── compile_overhead.exs   # Measures compile time with/without TypeCheck
```

### Step 1: Type algebra

```elixir
defmodule TypeCheck.Types do
  @type primitive :: :integer | :float | :string | :boolean | :atom | :nil | :any | :never
  @type t ::
    {:primitive, primitive()} |
    {:union, [t()]} |
    {:list, t()} |
    {:map, t(), t()} |
    {:tuple, [t()]} |
    {:struct, module()} |
    {:var, atom()}  # type variable

  def integer, do: {:primitive, :integer}
  def string, do: {:primitive, :string}
  def boolean, do: {:primitive, :boolean}
  def atom, do: {:primitive, :atom}
  def nil_type, do: {:primitive, :nil}
  def any, do: {:primitive, :any}
  def never, do: {:primitive, :never}

  def list(inner), do: {:list, inner}
  def map(k, v), do: {:map, k, v}
  def struct(mod), do: {:struct, mod}

  @doc "Construct a union type; apply simplification rules."
  def union(types) do
    types
    |> Enum.flat_map(fn
      {:union, inner} -> inner  # flatten nested unions
      t -> [t]
    end)
    |> Enum.uniq()
    |> then(fn
      ts when ts == [] -> never()
      ts ->
        if Enum.any?(ts, &(&1 == any())), do: any(), else: {:union, ts}
    end)
  end

  @doc "Parse a type from an AST node (used at compile time in annotation parsing)"
  def from_ast({:integer, _, _}), do: integer()
  def from_ast({:__aliases__, _, [mod]}), do: struct(mod)
  def from_ast({{:., _, [{:__aliases__, _, [:List]}, :t]}, _, [inner]}), do: list(from_ast(inner))
  def from_ast({:|, _, [left, right]}), do: union([from_ast(left), from_ast(right)])
  def from_ast({:string, _, _}), do: string()
  def from_ast(atom) when is_atom(atom), do: {:primitive, atom}

  @doc "Generate Dialyzer-compatible @spec type from TypeCheck type"
  def to_spec_ast({:primitive, :integer}), do: {:integer, [], []}
  def to_spec_ast({:primitive, :string}), do: {{:., [], [{:__aliases__, [], [:String]}, :t]}, [], []}
  def to_spec_ast({:primitive, :boolean}), do: {:boolean, [], []}
  def to_spec_ast({:primitive, :any}), do: {:any, [], []}
  def to_spec_ast({:primitive, :nil}), do: {:nil, [], []}
  def to_spec_ast({:union, types}), do: Enum.reduce(types, &{:|, [], [&2, to_spec_ast(&1)]})
  def to_spec_ast({:list, inner}), do: {:list, [], [to_spec_ast(inner)]}
  def to_spec_ast({:struct, mod}), do: {:%, [], [{:__aliases__, [], [mod]}, {:%{}, [], []}]}
end
```

### Step 2: Type inference

```elixir
defmodule TypeCheck.Infer do
  alias TypeCheck.Types

  @doc """
  Infer the type of an AST expression.
  Returns {:ok, type} or {:error, {line, expr_ast, message}}.
  Uses postwalk: children are typed before parents.
  """
  def infer(ast, env \\ %{}) do
    {_ast, result} = Macro.postwalk(ast, {:ok, env, nil}, fn node, acc ->
      case acc do
        {:error, _} = err -> {node, err}
        {:ok, type_env, _} -> {node, infer_node(node, type_env)}
      end
    end)
    result
  end

  defp infer_node(node, env) do
    case node do
      # Literals
      n when is_integer(n) -> {:ok, env, Types.integer()}
      f when is_float(f) -> {:ok, env, {:primitive, :float}}
      s when is_binary(s) -> {:ok, env, Types.string()}
      true -> {:ok, env, Types.boolean()}
      false -> {:ok, env, Types.boolean()}
      nil -> {:ok, env, Types.nil_type()}

      # Variable reference
      {var, meta, ctx} when is_atom(var) and is_atom(ctx) ->
        type = Map.get(env, var, Types.any())
        {:ok, env, type}

      # Arithmetic: integer op integer → integer
      {:+, _, [l, r]} -> infer_binop(l, r, Types.integer(), Types.integer(), env)
      {:-, _, [l, r]} -> infer_binop(l, r, Types.integer(), Types.integer(), env)
      {:*, _, [l, r]} -> infer_binop(l, r, Types.integer(), Types.integer(), env)

      # String concatenation
      {:<>, meta, [_l, _r]} ->
        # TODO: verify l and r are both String; return String
        # TODO: if not, return {:error, {meta[:line], node, "expected String for <>"}}
        {:ok, env, Types.string()}

      # Boolean operators
      {:and, _, _} -> {:ok, env, Types.boolean()}
      {:or, _, _} -> {:ok, env, Types.boolean()}
      {:not, _, _} -> {:ok, env, Types.boolean()}

      # If expression: type is union of branch types
      {:if, meta, [_cond, [do: then_branch, else: else_branch]]} ->
        # TODO: infer type of then_branch and else_branch
        # TODO: if both are the same type, return that type
        # TODO: else return union of both types
        {:ok, env, Types.any()}

      # Unknown: return :any
      _ -> {:ok, env, Types.any()}
    end
  end

  defp infer_binop(_l, _r, _expected_l, _expected_r, env) do
    # TODO: check that left and right operands match expected types
    # TODO: raise error with line if not
    {:ok, env, Types.integer()}
  end
end
```

### Step 3: Type compatibility

```elixir
defmodule TypeCheck.Check do
  alias TypeCheck.Types

  @doc "Returns true if actual type is a subtype of expected type"
  def subtype?(_actual, {:primitive, :any}), do: true
  def subtype?({:primitive, :never}, _expected), do: true
  def subtype?(same, same), do: true
  def subtype?({:union, actuals}, expected) do
    Enum.all?(actuals, &subtype?(&1, expected))
  end
  def subtype?(actual, {:union, expecteds}) do
    Enum.any?(expecteds, &subtype?(actual, &1))
  end
  def subtype?({:list, a}, {:list, b}), do: subtype?(a, b)
  def subtype?({:struct, mod}, {:struct, mod}), do: true
  def subtype?(_actual, _expected), do: false

  @doc "Unify two types for generic type variable resolution. Returns {:ok, bindings} or :error."
  def unify({:var, name}, type, bindings) do
    case Map.fetch(bindings, name) do
      {:ok, existing} -> if existing == type, do: {:ok, bindings}, else: :error
      :error -> {:ok, Map.put(bindings, name, type)}
    end
  end
  def unify(type, {:var, name}, bindings), do: unify({:var, name}, type, bindings)
  def unify({:list, a}, {:list, b}, bindings), do: unify(a, b, bindings)
  def unify(same, same, bindings), do: {:ok, bindings}
  def unify(_, _, _), do: :error

  @doc "Check that actual type satisfies expected type; raise CompileError on mismatch"
  def assert_compatible!(actual, expected, ast_node, file) do
    unless subtype?(actual, expected) do
      line = get_line(ast_node)
      expr_str = Macro.to_string(ast_node)
      raise CompileError,
        file: file,
        line: line,
        description: """
        Type mismatch:
          Expression: #{expr_str}
          Expected:   #{inspect(expected)}
          Got:        #{inspect(actual)}
        """
    end
  end

  defp get_line({_, meta, _}) when is_list(meta), do: meta[:line] || 0
  defp get_line(_), do: 0
end
```

### Step 4: Annotation parsing

```elixir
defmodule TypeCheck.Annotation do
  alias TypeCheck.Types

  @doc """
  Parse a @type_checked def annotation.
  Input: the AST of `def f(x :: Integer, y :: String) :: Boolean do ... end`
  Output: {function_name, [{arg_name, type}], return_type, body_ast}
  """
  def parse_def(def_ast) do
    case def_ast do
      {:def, _meta, [{:"::", _, [{name, _, args_ast}]}, [do: body]]} ->
        # TODO: separate args_ast from return type annotation
        # HINT: return type is the second arg of the outer ::
        # TODO: parse each arg: {arg_name, arg_type} from :: nodes
        # TODO: return {name, arg_types, return_type, body}
        {:error, :not_implemented}
      _ ->
        {:error, :not_annotated}
    end
  end

  @doc "Parse a :: annotation from an argument AST node"
  def parse_arg_annotation({:"::", _meta, [{name, _, _}, type_ast]}) do
    {name, Types.from_ast(type_ast)}
  end
  def parse_arg_annotation({name, _, _}) do
    {name, Types.any()}
  end
end
```

### Step 5: Main macro

```elixir
defmodule TypeCheck do
  alias TypeCheck.{Infer, Check, Annotation, Types, Spec}

  defmacro __using__(_opts) do
    quote do
      import TypeCheck, only: [type_checked: 1]
      Module.register_attribute(__MODULE__, :type_checked_specs, accumulate: true)
      @before_compile TypeCheck
    end
  end

  defmacro __before_compile__(env) do
    specs = Module.get_attribute(env.module, :type_checked_specs)
    quote do
      # TODO: emit @spec declarations for each function in :type_checked_specs
      # TODO: Spec.generate_spec(name, arg_types, return_type) → @spec AST
      unquote_splicing(Enum.map(specs, &Spec.to_quoted/1))
    end
  end

  defmacro type_checked(def_ast) do
    case Annotation.parse_def(def_ast) do
      {:error, :not_annotated} ->
        # Pass through unannotated defs unchanged
        def_ast
      {:error, reason} ->
        raise CompileError,
          file: __CALLER__.file,
          line: __CALLER__.line,
          description: "TypeCheck annotation parse error: #{inspect(reason)}"
      {:ok, name, arg_types, return_type, body} ->
        file = __CALLER__.file

        # Build type environment from argument annotations
        type_env = Map.new(arg_types)

        # Infer type of function body
        case Infer.infer(body, type_env) do
          {:ok, _env, inferred_return} ->
            Check.assert_compatible!(inferred_return, return_type, body, file)
          {:error, {line, expr_ast, message}} ->
            raise CompileError,
              file: file,
              line: line,
              description: message
        end

        # Accumulate spec for @before_compile
        quote do
          Module.put_attribute(__MODULE__, :type_checked_specs,
            {unquote(name), unquote(Macro.escape(arg_types)), unquote(Macro.escape(return_type))})
          unquote(def_ast)
        end
    end
  end
end
```

## Given tests

```elixir
# test/types_test.exs
defmodule TypeCheck.TypesTest do
  use ExUnit.Case, async: true
  alias TypeCheck.Types

  test "union flattens nested unions" do
    t = Types.union([Types.union([Types.integer(), Types.string()]), Types.boolean()])
    assert t == {:union, [Types.integer(), Types.string(), Types.boolean()]}
  end

  test "union absorbed by any" do
    t = Types.union([Types.integer(), Types.any()])
    assert t == Types.any()
  end

  test "union deduplication" do
    t = Types.union([Types.integer(), Types.integer()])
    assert t == {:union, [Types.integer()]}
  end

  test "empty union is never" do
    t = Types.union([])
    assert t == Types.never()
  end
end

# test/check_test.exs
defmodule TypeCheck.CheckTest do
  use ExUnit.Case, async: true
  alias TypeCheck.{Check, Types}

  test "integer is subtype of integer" do
    assert Check.subtype?(Types.integer(), Types.integer())
  end

  test "integer is subtype of any" do
    assert Check.subtype?(Types.integer(), Types.any())
  end

  test "integer is not subtype of string" do
    refute Check.subtype?(Types.integer(), Types.string())
  end

  test "integer is subtype of Integer | String" do
    union = Types.union([Types.integer(), Types.string()])
    assert Check.subtype?(Types.integer(), union)
  end

  test "boolean is not subtype of Integer | String" do
    union = Types.union([Types.integer(), Types.string()])
    refute Check.subtype?(Types.boolean(), union)
  end

  test "List.t(Integer) is subtype of List.t(Integer)" do
    assert Check.subtype?(Types.list(Types.integer()), Types.list(Types.integer()))
  end

  test "List.t(String) is not subtype of List.t(Integer)" do
    refute Check.subtype?(Types.list(Types.string()), Types.list(Types.integer()))
  end
end

# test/infer_test.exs
defmodule TypeCheck.InferTest do
  use ExUnit.Case, async: true
  alias TypeCheck.{Infer, Types}

  test "integer literal infers as Integer" do
    {:ok, _env, t} = Infer.infer(42)
    assert t == Types.integer()
  end

  test "string literal infers as String" do
    {:ok, _env, t} = Infer.infer("hello")
    assert t == Types.string()
  end

  test "true infers as Boolean" do
    {:ok, _env, t} = Infer.infer(true)
    assert t == Types.boolean()
  end

  test "arithmetic infers as Integer" do
    ast = quote do: 1 + 2
    {:ok, _env, t} = Infer.infer(ast)
    assert t == Types.integer()
  end

  test "string concat infers as String" do
    ast = quote do: "foo" <> "bar"
    {:ok, _env, t} = Infer.infer(ast)
    assert t == Types.string()
  end
end

# test/integration_test.exs
defmodule TypeCheck.IntegrationTest do
  use ExUnit.Case, async: true

  test "annotated function compiles cleanly when types match" do
    assert {:module, _mod, _, _} = Code.eval_string("""
      defmodule TypeCheckTest.ValidModule do
        use TypeCheck
        type_checked def add(x :: integer, y :: integer) :: integer do
          x + y
        end
      end
    """)
  end

  test "annotated function raises CompileError when return type mismatches" do
    assert_raise CompileError, ~r/Type mismatch/, fn ->
      Code.eval_string("""
        defmodule TypeCheckTest.InvalidReturn do
          use TypeCheck
          type_checked def wrong(x :: integer) :: string do
            x + 1
          end
        end
      """)
    end
  end

  test "CompileError includes line number" do
    try do
      Code.eval_string("""
        defmodule TypeCheckTest.LineTest do
          use TypeCheck
          type_checked def bad(x :: integer) :: string do
            x + 1
          end
        end
      """)
    rescue
      e in CompileError ->
        assert e.line != nil and e.line > 0
    end
  end

  test "union type: Integer | String accepts both" do
    assert {:module, _, _, _} = Code.eval_string("""
      defmodule TypeCheckTest.UnionOk do
        use TypeCheck
        type_checked def accept_either(x :: integer | string) :: integer | string do
          x
        end
      end
    """)
  end

  test "@spec is generated from type_checked annotation" do
    {:module, mod, _, _} = Code.eval_string("""
      defmodule TypeCheckTest.SpecGen do
        use TypeCheck
        type_checked def double(x :: integer) :: integer do
          x * 2
        end
      end
    """)
    specs = mod.__info__(:functions)
    assert {:double, 1} in specs
    # Verify @spec was generated (accessible via Code.Typespec)
    {:ok, specs} = Code.Typespec.fetch_specs(mod)
    assert Enum.any?(specs, fn {{name, _arity}, _} -> name == :double end)
  end
end
```

## Benchmark

```elixir
# bench/compile_overhead.exs
# Run with: mix run bench/compile_overhead.exs
defmodule TypeCheck.Bench.CompileOverhead do
  @num_functions 50

  def run do
    baseline_code = generate_module(false)
    annotated_code = generate_module(true)

    # Measure baseline (no TypeCheck)
    baseline_times = Enum.map(1..5, fn _ ->
      {us, _} = :timer.tc(fn -> Code.eval_string(baseline_code) end)
      us
    end)

    # Measure with TypeCheck
    annotated_times = Enum.map(1..5, fn _ ->
      {us, _} = :timer.tc(fn -> Code.eval_string(annotated_code) end)
      us
    end)

    baseline_avg = Enum.sum(baseline_times) / 5
    annotated_avg = Enum.sum(annotated_times) / 5
    overhead_pct = (annotated_avg - baseline_avg) / baseline_avg * 100

    IO.puts("Baseline avg:  #{Float.round(baseline_avg / 1000, 1)} ms")
    IO.puts("Annotated avg: #{Float.round(annotated_avg / 1000, 1)} ms")
    IO.puts("Overhead:      #{Float.round(overhead_pct, 1)}%")
    IO.puts("Target:        < 10%")
    IO.puts("Pass:          #{if overhead_pct < 10, do: "YES", else: "NO"}")
  end

  defp generate_module(with_type_check) do
    header = if with_type_check, do: "use TypeCheck\n", else: ""
    functions = Enum.map(1..@num_functions, fn i ->
      if with_type_check do
        """
        type_checked def func_#{i}(x :: integer, y :: integer) :: integer do
          x + y + #{i}
        end
        """
      else
        """
        def func_#{i}(x, y) do
          x + y + #{i}
        end
        """
      end
    end)
    |> Enum.join("\n")

    """
    defmodule Bench.CompileTest#{:rand.uniform(1_000_000)} do
      #{header}
      #{functions}
    end
    """
  end
end

TypeCheck.Bench.CompileOverhead.run()
```

## Trade-off analysis

| Design decision | Selected approach | Alternative | Trade-off |
|---|---|---|---|
| When to type-check | During macro expansion (compile-time) | Separate Mix task (post-compile) | Macro: blocks compilation on error; task: can't prevent binary creation |
| Type inference scope | Single function body | Cross-function (whole module) | Whole-module: catches more bugs; single-body: O(1) per function, no global state |
| Type representation | Tagged tuples `{:primitive, :integer}` | Protocol-based struct | Protocol: extensible; tagged tuple: pattern matchable, no allocation, faster |
| Generic type variables | Unification via binding map | Template instantiation | Unification: correct; template: simpler but fails for recursive generics |
| Dialyzer integration | Generate `@spec` from annotations | Separate `@spec` required | Generated: no duplication; separate: user can express types TypeCheck cannot |
| Error verbosity | Full file/line/expr/expected/got | Short message | Full: slower string building; essential for developer productivity |

## Common production mistakes

**Stripping AST metadata before type inference.** `quote/2` preserves line numbers in the `:meta` field of each AST node. `Macro.escape/1` and some macro operations strip this metadata. If you strip metadata before running type inference, every `CompileError` reports line 0. Always pass the original, unmodified AST to the type checker.

**Not handling `_` (ignored variable) in annotations.** A function like `def f(_ :: Integer) :: String` has an argument whose name is `_`. Storing `{:_, type}` in the type environment conflicts if there are multiple `_` arguments. Rename ignored arguments to `_arg_0`, `_arg_1`, etc. during annotation parsing.

**Treating `if` without an `else` as having type `nil | return_type`.** An `if` without an `else` branch returns `nil` when the condition is false. `Infer.infer` must emit a union type, not just the `then` branch type. Omitting this makes the type checker accept `if cond, do: 1` as `Integer` when it should be `Integer | nil`.

**Macro expansion order with `@before_compile`.** The `@type_checked_specs` attribute is accumulated by `type_checked` macro calls. `@before_compile` runs after all function definitions. If a user calls `type_checked` inside a `use` or another macro that runs after `@before_compile`, the spec generation misses those functions. Document that `type_checked` must appear in the top-level module body, not inside other macros.

**Not testing with `--warnings-as-errors`.** The generated `@spec` may conflict with an existing `@spec` if the user accidentally wrote both. This produces a warning by default. Enable `--warnings-as-errors` in tests to catch this immediately.

## Resources

- Pierce — "Types and Programming Languages" (MIT Press, 2002) Chapters 1–15 (type theory foundation)
- McCord — "Metaprogramming Elixir" (Pragmatic Bookshelf, 2015)
- Milner — "A Theory of Type Polymorphism in Programming" (1978) — JCSS 17(3) (Hindley-Milner original)
- Siek & Taha — "Gradual Typing for Functional Languages" (2006) (partial type checking design)
- Elixir `Macro` module documentation — https://hexdocs.pm/elixir/Macro.html (postwalk, prewalk, to_string)
- Elixir `Code.Typespec` documentation — https://hexdocs.pm/elixir/Code.Typespec.html (spec introspection)
- Erlang Dialyzer source — `lib/dialyzer/erl_types.erl` in OTP (success typing reference)
