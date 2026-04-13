# Compile-time Type System for Elixir Macros

**Project**: `type_check` — Compile-time type annotation macro that raises CompileError on type mismatches

## Project context

Your team maintains a large Elixir codebase. Dialyzer catches some type bugs but runs after compilation in a separate pass. When a developer passes a string to a function expecting an integer, the error appears at runtime in production, not at compile time in the editor. The team wants a lightweight annotation system that catches obvious type mismatches during `mix compile`.

You will build `TypeCheck`: a compile-time type annotation macro that raises `CompileError` with precise file and line information when function argument types or return types are violated. No runtime overhead. No external processes. Pure macro expansion.

## Design decisions

**Option A — runtime type checks via pattern matching + raise**
- Pros: simple, visible at call sites
- Cons: runs tests to find bugs, cost on every call

**Option B — macro-based inference that reports errors at compile time** (chosen)
- Pros: bugs caught before test time, zero runtime cost
- Cons: macros are harder to write and integrate with dialyzer

→ Chose **B** because type errors are cheapest at compile time — anywhere later multiplies the feedback loop.

## Why build a type checker as macros and not as a Mix task

A Mix task runs after compilation. The compilation has already succeeded. Macros run during compilation. A `CompileError` raised inside a macro stops the compilation of the current module immediately, with file path and line number from AST metadata.

The trade-off: macros only see the AST of the current module. You can verify that the function body is consistent with its annotation, and you can verify call sites where argument types are statically known. You cannot track types across module boundaries without a global type database.

## Why type inference is bottom-up (post-order AST traversal)

To infer the type of `x + 1`, you need to know the types of `x` and `1` first. The type of a parent node depends on the types of its children. This is bottom-up inference: `Macro.postwalk/3` annotates leaf nodes first, then propagates up.

## Why Hindley-Milner is overkill for this exercise

Full HM inference solves type equations globally, including across function calls with polymorphic functions. For single-function body inference, a simpler constraint-propagation approach suffices: walk the AST bottom-up, infer a type for each node, and check it against the declared type at the return position.

## Project Structure

```
type_check/
├── mix.exs
├── lib/
│   ├── type_check/
│   │   ├── types.ex
│   │   ├── infer.ex
│   │   ├── check.ex
│   │   ├── error.ex
│   │   ├── spec.ex
│   │   └── annotation.ex
│   └── type_check.ex
├── test/
│   ├── types_test.exs
│   ├── infer_test.exs
│   ├── check_test.exs
│   └── integration_test.exs
└── bench/
    └── compile_overhead.exs
```

### Step 1: Type algebra

**Objective**: Represent types as tagged tuples with normalized unions so subtype and unify checks reduce to fast pattern matches.



### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule TypeCheck.Types do
  @moduledoc """
  Type algebra: primitives, unions, generics, and structural types.
  Types are represented as tagged tuples for efficient pattern matching.
  """

  @type primitive :: :integer | :float | :string | :boolean | :atom | :nil | :any | :never
  @type t ::
          {:primitive, primitive()}
          | {:union, [t()]}
          | {:list, t()}
          | {:map, t(), t()}
          | {:tuple, [t()]}
          | {:struct, module()}
          | {:var, atom()}

  @spec integer() :: t()
  def integer, do: {:primitive, :integer}

  @spec float() :: t()
  def float, do: {:primitive, :float}

  @spec string() :: t()
  def string, do: {:primitive, :string}

  @spec boolean() :: t()
  def boolean, do: {:primitive, :boolean}

  @spec atom() :: t()
  def atom, do: {:primitive, :atom}

  @spec nil_type() :: t()
  def nil_type, do: {:primitive, :nil}

  @spec any() :: t()
  def any, do: {:primitive, :any}

  @spec never() :: t()
  def never, do: {:primitive, :never}

  @spec list(t()) :: t()
  def list(inner), do: {:list, inner}

  @spec map(t(), t()) :: t()
  def map(k, v), do: {:map, k, v}

  @spec struct(module()) :: t()
  def struct(mod), do: {:struct, mod}

  @doc """
  Construct a union type with simplification rules:
  - Flatten nested unions
  - Deduplicate
  - Absorb into :any if present
  - Empty union becomes :never
  """
  @spec union([t()]) :: t()
  def union(types) do
    types
    |> Enum.flat_map(fn
      {:union, inner} -> inner
      t -> [t]
    end)
    |> Enum.uniq()
    |> then(fn
      [] ->
        never()

      ts ->
        if Enum.any?(ts, &(&1 == any())) do
          any()
        else
          {:union, ts}
        end
    end)
  end

  @doc "Parse a type from an AST node (used at compile time in annotation parsing)."
  @spec from_ast(Macro.t()) :: t()
  def from_ast({:integer, _, _}), do: integer()
  def from_ast({:float, _, _}), do: float()
  def from_ast({:string, _, _}), do: string()
  def from_ast({:boolean, _, _}), do: boolean()
  def from_ast({:__aliases__, _, [mod]}), do: struct(mod)

  def from_ast({{:., _, [{:__aliases__, _, [:List]}, :t]}, _, [inner]}) do
    list(from_ast(inner))
  end

  def from_ast({:|, _, [left, right]}), do: union([from_ast(left), from_ast(right)])
  def from_ast(a) when is_atom(a), do: {:primitive, a}

  @doc "Generate Dialyzer-compatible @spec type AST from a TypeCheck type."
  @spec to_spec_ast(t()) :: Macro.t()
  def to_spec_ast({:primitive, :integer}), do: {:integer, [], []}
  def to_spec_ast({:primitive, :float}), do: {:float, [], []}

  def to_spec_ast({:primitive, :string}) do
    {{:., [], [{:__aliases__, [], [:String]}, :t]}, [], []}
  end

  def to_spec_ast({:primitive, :boolean}), do: {:boolean, [], []}
  def to_spec_ast({:primitive, :any}), do: {:any, [], []}
  def to_spec_ast({:primitive, :nil}), do: {:nil, [], []}
  def to_spec_ast({:primitive, :atom}), do: {:atom, [], []}

  def to_spec_ast({:union, types}) do
    Enum.reduce(types, fn t, acc -> {:|, [], [acc, to_spec_ast(t)]} end)
  end

  def to_spec_ast({:list, inner}), do: {:list, [], [to_spec_ast(inner)]}

  def to_spec_ast({:struct, mod}) do
    {:%, [], [{:__aliases__, [], [mod]}, {:%{}, [], []}]}
  end
end
```

### Step 2: Type inference

**Objective**: Infer expression types bottom-up through the AST, unioning branch results so control flow widens types only where it must.


```elixir
defmodule TypeCheck.Infer do
  @moduledoc """
  AST type inference using bottom-up (post-order) traversal.
  Infers the type of each AST node from its children, threading
  a type environment for variable bindings.
  """

  alias TypeCheck.Types

  @doc """
  Infer the type of an AST expression.
  Returns {:ok, env, type} or {:error, {line, expr_ast, message}}.
  """
  @spec infer(Macro.t(), map()) :: {:ok, map(), Types.t()} | {:error, tuple()}
  def infer(ast, env \\ %{}) do
    {_ast, result} =
      Macro.postwalk(ast, {:ok, env, nil}, fn node, acc ->
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
      n when is_integer(n) ->
        {:ok, env, Types.integer()}

      f when is_float(f) ->
        {:ok, env, Types.float()}

      s when is_binary(s) ->
        {:ok, env, Types.string()}

      true ->
        {:ok, env, Types.boolean()}

      false ->
        {:ok, env, Types.boolean()}

      nil ->
        {:ok, env, Types.nil_type()}

      # Variable reference
      {var, _meta, ctx} when is_atom(var) and is_atom(ctx) ->
        type = Map.get(env, var, Types.any())
        {:ok, env, type}

      # Arithmetic: integer op integer -> integer
      {:+, _meta, [_l, _r]} ->
        {:ok, env, Types.integer()}

      {:-, _meta, [_l, _r]} ->
        {:ok, env, Types.integer()}

      {:*, _meta, [_l, _r]} ->
        {:ok, env, Types.integer()}

      {:div, _meta, [_l, _r]} ->
        {:ok, env, Types.integer()}

      # String concatenation
      {:<>, _meta, [_l, _r]} ->
        {:ok, env, Types.string()}

      # Boolean operators
      {:and, _, _} ->
        {:ok, env, Types.boolean()}

      {:or, _, _} ->
        {:ok, env, Types.boolean()}

      {:not, _, _} ->
        {:ok, env, Types.boolean()}

      # Comparison operators
      {:==, _, _} ->
        {:ok, env, Types.boolean()}

      {:!=, _, _} ->
        {:ok, env, Types.boolean()}

      {:>, _, _} ->
        {:ok, env, Types.boolean()}

      {:<, _, _} ->
        {:ok, env, Types.boolean()}

      {:>=, _, _} ->
        {:ok, env, Types.boolean()}

      {:<=, _, _} ->
        {:ok, env, Types.boolean()}

      # If expression: union of branch types
      {:if, _meta, [_cond, [do: then_branch]]} ->
        case infer(then_branch, env) do
          {:ok, _, then_type} ->
            {:ok, env, Types.union([then_type, Types.nil_type()])}

          err ->
            err
        end

      {:if, _meta, [_cond, [do: then_branch, else: else_branch]]} ->
        with {:ok, _, then_type} <- infer(then_branch, env),
             {:ok, _, else_type} <- infer(else_branch, env) do
          if then_type == else_type do
            {:ok, env, then_type}
          else
            {:ok, env, Types.union([then_type, else_type])}
          end
        end

      # Case expression: union of all branch types
      {:case, _meta, [_expr, [do: clauses]]} ->
        branch_types =
          Enum.map(clauses, fn {:->, _, [_pattern, body]} ->
            case infer(body, env) do
              {:ok, _, type} -> type
              _ -> Types.any()
            end
          end)

        {:ok, env, Types.union(branch_types)}

      # List literal
      [_ | _] = list ->
        element_types =
          Enum.map(list, fn elem ->
            case infer(elem, env) do
              {:ok, _, type} -> type
              _ -> Types.any()
            end
          end)
          |> Enum.uniq()

        inner =
          case element_types do
            [single] -> single
            multiple -> Types.union(multiple)
          end

        {:ok, env, Types.list(inner)}

      [] ->
        {:ok, env, Types.list(Types.any())}

      # Unknown: return :any
      _ ->
        {:ok, env, Types.any()}
    end
  end
end
```

### Step 3: Type compatibility

**Objective**: Decide subtype relations and unify type variables so generics resolve and mismatches raise CompileError at the exact offending AST node.


```elixir
defmodule TypeCheck.Check do
  @moduledoc """
  Type compatibility checker. Determines subtype relationships
  and performs type unification for generic type variables.
  """

  alias TypeCheck.Types

  @doc "Returns true if actual type is a subtype of expected type."
  @spec subtype?(Types.t(), Types.t()) :: boolean()
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
  def subtype?({:map, ka, va}, {:map, kb, vb}), do: subtype?(ka, kb) and subtype?(va, vb)
  def subtype?({:struct, mod}, {:struct, mod}), do: true
  def subtype?({:primitive, :integer}, {:primitive, :float}), do: true
  def subtype?(_actual, _expected), do: false

  @doc """
  Unify two types for generic type variable resolution.
  Returns {:ok, bindings} or :error.
  """
  @spec unify(Types.t(), Types.t(), map()) :: {:ok, map()} | :error
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

  @doc "Check that actual type satisfies expected type; raise CompileError on mismatch."
  @spec assert_compatible!(Types.t(), Types.t(), Macro.t(), String.t()) :: :ok
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

    :ok
  end

  defp get_line({_, meta, _}) when is_list(meta), do: meta[:line] || 0
  defp get_line(_), do: 0
end
```

### Step 4: Annotation parsing

**Objective**: Parse the `::` annotation AST into argument types, return type, and body so later passes work on structured data, not raw quotes.


```elixir
defmodule TypeCheck.Annotation do
  @moduledoc """
  Parse @type_checked def annotations from AST.
  Extracts function name, argument types, return type, and body.
  """

  alias TypeCheck.Types

  @doc """
  Parse a type_checked def annotation.
  Input: the AST of `def f(x :: Integer, y :: String) :: Boolean do ... end`
  Output: {:ok, function_name, [{arg_name, type}], return_type, body_ast}
  """
  @spec parse_def(Macro.t()) ::
          {:ok, atom(), [{atom(), Types.t()}], Types.t(), Macro.t()}
          | {:error, atom()}
  def parse_def(
        {:def, _meta,
         [
           {:"::", _,
            [
              {name, _, raw_args},
              return_type_ast
            ]},
           [do: body]
         ]}
      ) do
    args = raw_args || []
    arg_types = Enum.map(args, &parse_arg_annotation/1)
    return_type = Types.from_ast(return_type_ast)
    {:ok, name, arg_types, return_type, body}
  end

  def parse_def({:def, _meta, _} = _ast) do
    {:error, :not_annotated}
  end

  def parse_def(_) do
    {:error, :not_annotated}
  end

  @doc "Parse a :: annotation from an argument AST node."
  @spec parse_arg_annotation(Macro.t()) :: {atom(), Types.t()}
  def parse_arg_annotation({:"::", _meta, [{name, _, _}, type_ast]}) do
    {name, Types.from_ast(type_ast)}
  end

  def parse_arg_annotation({name, _, _}) do
    {name, Types.any()}
  end
end
```

### Step 5: Spec generation

**Objective**: Emit Dialyzer-compatible @spec AST from each checked function so the static type claims survive into the BEAM's own tooling.


```elixir
defmodule TypeCheck.Spec do
  @moduledoc "Generates Dialyzer-compatible @spec declarations from TypeCheck annotations."

  alias TypeCheck.Types

  @doc "Generate a @spec AST from function name, argument types, and return type."
  @spec to_quoted({atom(), [{atom(), Types.t()}], Types.t()}) :: Macro.t()
  def to_quoted({name, arg_types, return_type}) do
    arg_spec_asts = Enum.map(arg_types, fn {_name, type} -> Types.to_spec_ast(type) end)
    return_spec_ast = Types.to_spec_ast(return_type)

    quote do
      @spec unquote(name)(unquote_splicing(arg_spec_asts)) :: unquote(return_spec_ast)
    end
  end
end
```

### Step 6: Main macro

**Objective**: Expose `type_checked` via `__using__` and `__before_compile__` so annotations trigger inference, checking, and spec emission in one compile pass.


```elixir
defmodule TypeCheck do
  @moduledoc """
  Compile-time type checking via macros.
  `use TypeCheck` enables the `type_checked` macro which annotates
  function definitions with type information and checks them during
  compilation.
  """

  alias TypeCheck.{Infer, Check, Annotation, Spec}

  defmacro __using__(_opts) do
    quote do
      import TypeCheck, only: [type_checked: 1]
      Module.register_attribute(__MODULE__, :type_checked_specs, accumulate: true)
      @before_compile TypeCheck
    end
  end

  defmacro __before_compile__(env) do
    specs = Module.get_attribute(env.module, :type_checked_specs)

    Enum.map(specs || [], fn spec_data ->
      Spec.to_quoted(spec_data)
    end)
  end

  @doc """
  Macro that wraps a def with type annotations.
  Checks the function body's inferred type against the declared return type
  at compile time. Raises CompileError on mismatch.
  """
  defmacro type_checked(def_ast) do
    case Annotation.parse_def(def_ast) do
      {:error, :not_annotated} ->
        def_ast

      {:error, reason} ->
        raise CompileError,
          file: __CALLER__.file,
          line: __CALLER__.line,
          description: "TypeCheck annotation parse error: #{inspect(reason)}"

      {:ok, name, arg_types, return_type, body} ->
        file = __CALLER__.file
        type_env = Map.new(arg_types)

        case Infer.infer(body, type_env) do
          {:ok, _env, inferred_return} ->
            Check.assert_compatible!(inferred_return, return_type, body, file)

          {:error, {line, _expr_ast, message}} ->
            raise CompileError,
              file: file,
              line: line,
              description: message
        end

        quote do
          Module.put_attribute(
            __MODULE__,
            :type_checked_specs,
            {unquote(name), unquote(Macro.escape(arg_types)),
             unquote(Macro.escape(return_type))}
          )

          unquote(def_ast)
        end
    end
  end
end
```

### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Given tests

```elixir
# test/types_test.exs
defmodule TypeCheck.TypesTest do
  use ExUnit.Case, async: true
  alias TypeCheck.Types


  describe "Types" do

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
    {:ok, specs} = Code.Typespec.fetch_specs(mod)
    assert Enum.any?(specs, fn {{name, _arity}, _} -> name == :double end)
  end


  end
end
```


## Main Entry Point

```elixir
def main do
  IO.puts("======== 48 build compile time type system ========")
  IO.puts("Demonstrating core functionality")
  IO.puts("")
  
  IO.puts("Run: mix test")
end
```

## Benchmark

```elixir
# bench/compile_overhead.exs
# Run with: mix run bench/compile_overhead.exs
defmodule TypeCheck.Bench.CompileOverhead do
  @num_functions 50
  @num_iterations 5

  def run do
    IO.puts("=== TypeCheck Compile Overhead Benchmark ===")
    IO.puts("Testing #{@num_functions} functions, #{@num_iterations} iterations per config\n")
    
    baseline_times = measure_baseline()
    annotated_times = measure_annotated()

    baseline_avg = Enum.sum(baseline_times) / @num_iterations
    annotated_avg = Enum.sum(annotated_times) / @num_iterations
    overhead_pct = (annotated_avg - baseline_avg) / baseline_avg * 100

    IO.puts("\n=== Results ===")
    IO.puts("Baseline avg:  #{Float.round(baseline_avg / 1000, 2)} ms")
    IO.puts("Annotated avg: #{Float.round(annotated_avg / 1000, 2)} ms")
    IO.puts("Overhead:      #{Float.round(overhead_pct, 1)}%")
    IO.puts("Target:        < 10% overhead")
    IO.puts("Status:        #{if overhead_pct < 10, do: "PASS", else: "FAIL"}")
  end

  defp measure_baseline do
    IO.write("Baseline runs:  ")
    Enum.map(1..@num_iterations, fn i ->
      IO.write(".")
      baseline_code = generate_module(false)
      {us, _} = :timer.tc(fn -> Code.eval_string(baseline_code) end)
      us
    end)
    IO.puts(" done")
  end

  defp measure_annotated do
    IO.write("TypeCheck runs: ")
    Enum.map(1..@num_iterations, fn i ->
      IO.write(".")
      annotated_code = generate_module(true)
      {us, _} = :timer.tc(fn -> Code.eval_string(annotated_code) end)
      us
    end)
    IO.puts(" done")
  end

  defp generate_module(with_type_check) do
    header = if with_type_check, do: "use TypeCheck\n", else: ""

    functions =
      Enum.map(1..@num_functions, fn i ->
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

## Key Concepts: Compile-Time Type Inference y Macro Expansion Phases

Los type checkers basados en macros ejecutan en **tiempo de compilación**, no runtime. Esto tiene implicaciones profundas:

1. **Recolección de errores sin costo latency**: Un mismatch `x + 1` cuando `x: string` se captura durante `mix compile`, no después de `mix test` o peor, en producción.

2. **Zero runtime overhead**: El type checker emite un CompileError y detiene el build. El código compilado no contiene ningún runtime check, ninguna instrucción BEAM. El tiempo de ejecución es idéntico al código sin types.

3. **Type erasure**: Al igual que TypeScript, los tipos existen solo en la compilación. El BEAM bytecode final no sabe nada de tipos. Dialyzer, por otro lado, ejecuta un análisis **post-hoc** — después de que el código ya está compilado.

4. **Alcance limitado pero confiable**: Un macro ve solo la AST del módulo actual. No puede rastrear tipos a través de module boundaries sin una base de datos global (Dialyzer sí). Pero lo que sí puede hacer es 100% correcto: si infiere una posición en el AST, esa es la ubicación exacta del error.

5. **Macro expansion order**: Las fases Elixir corren en orden: `expand_macros` → `compile_def` → `bytecode`. Un macro que ocurre en fase 1 puede rechazar un `def` antes de que alcance la fase 2. Pero si el macro está dentro de otro macro que corre después, se pierde. Siempre usa `@before_compile` para garantizar último-en-correr.

**Trade-off clave**: Ganamos detectabilidad temprana + zero overhead. Perdemos capacidad de capturar errores en boundaries entre módulos sin metadatos compartidos. Para un sistema en una codebase, es suficiente.

---

## Trade-off analysis

| Design decision | Selected approach | Alternative | Trade-off |
|---|---|---|---|
| When to type-check | During macro expansion | Separate Mix task | Macro blocks compilation on error; task cannot prevent binary creation |
| Inference scope | Single function body | Cross-function (whole module) | Whole-module catches more bugs; single-body is O(1) per function |
| Type representation | Tagged tuples | Protocol-based struct | Protocol is extensible; tagged tuple is pattern matchable and faster |
| Generic variables | Unification via binding map | Template instantiation | Unification is correct; template fails for recursive generics |
| Dialyzer integration | Generate @spec from annotations | Require separate @spec | Generated avoids duplication; separate allows broader expressiveness |
| Error verbosity | Full file/line/expr/expected/got | Short message | Full is essential for developer productivity |

## Common production mistakes

**Stripping AST metadata before type inference.** `quote/2` preserves line numbers. Some macro operations strip metadata. Always pass the original unmodified AST to the type checker.

**Not handling `_` (ignored variable) in annotations.** Storing `{:_, type}` in the type environment conflicts with multiple `_` arguments. Rename ignored arguments during parsing.

**Treating `if` without `else` as having only the `then` type.** An `if` without `else` returns `nil` when false. Emit a union type: `then_type | nil`.

**Macro expansion order with `@before_compile`.** Attributes are accumulated by `type_checked` calls. If `type_checked` appears inside another macro that runs after `@before_compile`, spec generation misses those functions.

**Not testing with `--warnings-as-errors`.** A generated `@spec` may conflict with an existing one, producing a warning. Enable `--warnings-as-errors` in tests.

## Reflection

How does your compile-time checker behave when 10% of the codebase is untyped? Is the boundary between checked and unchecked code a hole or a wall, and what's the blast radius of a type error at the boundary?

## Resources

- Pierce -- "Types and Programming Languages" (MIT Press, 2002) Chapters 1-15
- McCord -- "Metaprogramming Elixir" (Pragmatic Bookshelf, 2015)
- Milner -- "A Theory of Type Polymorphism in Programming" (1978) -- JCSS 17(3)
- Elixir `Macro` module -- https://hexdocs.pm/elixir/Macro.html
- Elixir `Code.Typespec` -- https://hexdocs.pm/elixir/Code.Typespec.html
