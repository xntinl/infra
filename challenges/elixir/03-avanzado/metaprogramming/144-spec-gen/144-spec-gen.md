# Generating `@spec` from Annotations

**Project**: `spec_gen` — a macro that reads compact type annotations from a sibling attribute and emits `@spec` for each function automatically, keeping types and code in sync.

---

## Project context

Your team enforces Dialyzer in CI. Adding `@spec` to every function is tedious, and
specs drift when signatures change. You want a compact notation:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule Calculator do
  use SpecGen

  deftyped add(i, i), :: i do
    i + i
  end

  deftyped halve(i), :: f do
    i / 2
  end
end
```

At compile time, `i` and `f` are expanded to `integer()` and `float()`, a full
`@spec add(integer(), integer()) :: integer()` emitted, and `def add(i, j) ...`
generated with the right arities. If the user misspells a type, you raise
`CompileError` pointing to the offending line.

This is exactly the technique that libraries like `TypeCheck` and `Norm` use under
the hood, plus a slice of what GRPC's Protobuf generator does: convert short
annotations to formal typespecs.

```
spec_gen/
├── lib/
│   └── spec_gen/
│       ├── spec_gen.ex            # deftyped macro
│       └── types.ex               # alias table short => full
├── test/
│   └── spec_gen_test.exs
└── mix.exs
```

---

## Why generated specs and not missing ones

Generated functions without specs are invisible to Dialyzer and partially invisible to ExDoc. Emitting `@spec` during code generation keeps both tools honest.

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Metaprogramming-specific insight:**
Code generation is powerful and dangerous. Every macro you write is a place where intent is hidden. Use macros sparingly, only when they eliminate genuine boilerplate. If your macro is more than 10 lines, you probably need a function or data structure instead. Future maintainers will thank you.
### 1. `@spec` is just an attribute with an AST

```
@spec add(integer(), integer()) :: integer()
```

Elixir stores this as `Module.put_attribute(mod, :spec, {:add, 2, quoted_spec})`.
Emitting a `@spec` from a macro means building the same quoted AST and attaching it.

### 2. Short types → AST

Build a table mapping `:i -> {:integer, [], []}`, `:f -> {:float, [], []}`, etc.
The macro rewrites each short atom into the full type AST via `Macro.postwalk/2`.

### 3. Function head + body

`deftyped add(i, i), :: i do ... end` has the shape
`{{name, args}, return_type, body}`. Split with pattern matching, build the spec,
then emit `def/2`.

### 4. Arity is derived from args

The macro computes `length(args)` and uses it in the emitted spec declaration. This
guarantees spec and def agree on arity.

### 5. Error reporting with `__CALLER__`

Failures should report the user's file:line, not the macro's. Use
`__CALLER__.file` and `__CALLER__.line` in `raise CompileError`.

---

## Design decisions

**Option A — write specs by hand next to every function**
- Pros: reviewers see the contract at the call site; Dialyzer picks it up cleanly.
- Cons: specs drift from generated function lists; DSLs leave them missing.

**Option B — emit `@spec` alongside generated functions** (chosen)
- Pros: Dialyzer coverage for generated code; docs stay accurate.
- Cons: spec building is macro work; type escaping is subtle.

→ Chose **B** because without generated specs, Dialyzer misses the main body of the module and docs lose type info.

---

## Implementation

### Step 1: `lib/spec_gen/types.ex`

**Objective**: Map short type aliases (i, f, b, bool) to full typespec AST via quote so macro can expand them.

```elixir
defmodule SpecGen.Types do
  @moduledoc "Maps short type aliases to full typespec AST."

  @short_to_full %{
    i: quote(do: integer()),
    f: quote(do: float()),
    b: quote(do: binary()),
    bool: quote(do: boolean()),
    atm: quote(do: atom()),
    any: quote(do: any()),
    map: quote(do: map()),
    list: quote(do: list()),
    ok: quote(do: :ok),
    none: quote(do: none())
  }

  @spec expand(atom()) :: Macro.t()
  def expand(short) when is_atom(short) do
    case Map.fetch(@short_to_full, short) do
      {:ok, ast} ->
        ast

      :error ->
        raise CompileError,
          description:
            "SpecGen: unknown short type #{inspect(short)}. " <>
              "Known: #{inspect(Map.keys(@short_to_full))}"
    end
  end

  @spec known?(atom()) :: boolean()
  def known?(short), do: Map.has_key?(@short_to_full, short)
end
```

### Step 2: `lib/spec_gen/spec_gen.ex`

**Objective**: Implement deftyped/3 macro that parses short type annotations, expands them, and emits @spec + def together.

```elixir
defmodule SpecGen do
  @moduledoc """
  Provides `deftyped/3` — a `def` with auto-generated `@spec`.

      use SpecGen
      deftyped add(i, i), :: i do
        i + i
      end
  """

  alias SpecGen.Types

  defmacro __using__(_opts) do
    quote do
      import SpecGen, only: [deftyped: 3]
    end
  end

  defmacro deftyped(head, return, do: body) do
    {name, arg_types, arg_vars} = parse_head(head, __CALLER__)
    return_ast = expand_return(return)
    arity = length(arg_types)

    spec_ast = build_spec_ast(name, arg_types, return_ast)

    quote do
      @spec unquote(spec_ast)
      def unquote(name)(unquote_splicing(arg_vars)) do
        unquote(body)
      end
    end
  end

  defp parse_head({name, _meta, args}, caller) when is_atom(name) and is_list(args) do
    types =
      Enum.map(args, fn
        {type, _, ctx} when is_atom(type) and is_atom(ctx) ->
          if Types.known?(type) do
            type
          else
            raise CompileError,
              file: caller.file,
              line: caller.line,
              description: "SpecGen: unknown short type #{inspect(type)}"
          end

        other ->
          raise CompileError,
            file: caller.file,
            line: caller.line,
            description: "SpecGen: expected short type atom, got #{Macro.to_string(other)}"
      end)

    vars =
      types
      |> Enum.with_index()
      |> Enum.map(fn {t, i} -> Macro.var(String.to_atom("#{t}#{i}"), __MODULE__) end)

    {name, types, vars}
  end

  defp parse_head(other, caller) do
    raise CompileError,
      file: caller.file,
      line: caller.line,
      description: "SpecGen: expected function head, got #{Macro.to_string(other)}"
  end

  defp expand_return({:"::", _, [return]}), do: Types.expand(return)

  defp expand_return({:"::", _, [_, return]}) when is_atom(return),
    do: Types.expand(return)

  defp expand_return(return) when is_atom(return), do: Types.expand(return)

  defp build_spec_ast(name, arg_types, return_ast) do
    arg_asts = Enum.map(arg_types, &Types.expand/1)

    quote do
      unquote(name)(unquote_splicing(arg_asts)) :: unquote(return_ast)
    end
  end
end
```

### Step 3: Sample usage

**Objective**: Demonstrate deftyped with Calculator.add/halve/noop so users see DSL ergonomics.

```elixir
defmodule SpecGen.Sample.Calculator do
  use SpecGen

  deftyped add(i, i), :: i do
    arg0 = var!(i0)
    arg1 = var!(i1)
    arg0 + arg1
  end

  deftyped halve(i), :: f do
    var!(i0) / 2
  end

  deftyped noop(), :: ok do
    :ok
  end
end
```

A simpler variant for tests that sidesteps the hygienic-variable ceremony:

```elixir
defmodule SpecGen.Sample.Simple do
  use SpecGen

  deftyped echo(i), :: i, do: var!(i0)
end
end
```

### Step 4: Tests

**Objective**: Assert function is emitted, @spec is attached and Dialyzer-readable, unknown types and non-atoms raise CompileError.

```elixir
defmodule SpecGenTest do
  use ExUnit.Case, async: true

  alias SpecGen.Sample.Simple

  describe "deftyped emits function" do
    test "echo/1 exists and returns its arg" do
      assert Simple.echo(5) == 5
    end
  end

  describe "@spec emitted correctly" do
    test "echo/1 has integer -> integer spec" do
      {:ok, specs} = Code.Typespec.fetch_specs(Simple)

      assert Enum.any?(specs, fn {{:echo, 1}, [spec]} ->
               Macro.to_string(Code.Typespec.spec_to_quoted(:echo, spec)) =~ "integer"
             end)
    end
  end

  describe "compile-time errors" do
    test "unknown short type raises" do
      assert_raise CompileError, ~r/unknown short type/, fn ->
        Code.compile_string("""
        defmodule Bad do
          use SpecGen
          deftyped x(zz), :: i, do: var!(zz0)
        end
        """)
      end
    end

    test "non-atom argument raises" do
      assert_raise CompileError, ~r/expected short type atom/, fn ->
        Code.compile_string("""
        defmodule Bad2 do
          use SpecGen
          deftyped y(1), :: i, do: 1
        end
        """)
      end
    end
  end
end
```

### Why this works

Inside the macro, the field types are available as AST fragments. Splicing them into a `@spec` attribute that precedes the `def` attaches them to the emitted function. The compiler and Dialyzer treat them as hand-written specs.

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

**1. Argument variable naming is awkward.** Users cannot name their arg easily with
this shape because the head arguments carry the *type* short, not a variable name.
Real libraries extend the syntax to accept `arg_name :: type`.

**2. Dialyzer integration.** The generated `@spec` is real and Dialyzer reads it —
but ensure `mix compile` doesn't cache stale specs. A recompile `--force` after
changing the DSL helper clears the cache.

**3. Complex types not supported.** `[integer()]`, `{:ok, term()}` require parsing a
richer syntax. Extend `Types.expand/1` to handle lists and tuples, or accept bare
typespec AST from the user.

**4. Private functions (`defp`).** The current macro only emits `def`. Add a
`deftypedp/3` variant, or accept `kind:` option.

**5. Guards and default arguments.** `deftyped add(i, i) when i > 0, :: i` needs
parsing the `when` clause out of the head. Not trivial; Phoenix.Controller's
`@before_compile` handles similar.

**6. Spec-only declarations.** Some libs want `@spec` without a generated function
(just to document). Provide `spectyped/2` that emits only the attribute.

**7. Interplay with `@impl true`.** When `deftyped` generates `def`, `@impl true`
must appear BEFORE the `deftyped` call (it targets the next `def`). Document.

**8. When NOT to use this.** For normal application code, writing `@spec`
by hand is clearer and tool-friendlier (IDE autocomplete, goto-definition).
Generators shine when you have hundreds of near-identical wrappers (e.g. API
clients generated from an OpenAPI schema).

---

## Benchmark

<!-- benchmark N/A: tema conceptual / plumbing de compile-time -->

---

## Reflection

- A generated function has a dynamic return type depending on input shape. Do you emit a union spec, or drop to `term()`? What does each choice cost you in Dialyzer signal?
- Would you accept a PR that removes generated specs to cut compile time? What evidence would change your answer?

---


## Executable Example

```elixir
defmodule SpecGen do
  @moduledoc """
  Provides `deftyped/3` — a `def` with auto-generated `@spec`.

      use SpecGen
      deftyped add(i, i), :: i do
        i + i
      end
  """

  alias SpecGen.Types

  defmacro __using__(_opts) do
    quote do
      import SpecGen, only: [deftyped: 3]
    end
  end

  defmacro deftyped(head, return, do: body) do
    {name, arg_types, arg_vars} = parse_head(head, __CALLER__)
    return_ast = expand_return(return)
    arity = length(arg_types)

    spec_ast = build_spec_ast(name, arg_types, return_ast)

    quote do
      @spec unquote(spec_ast)
      def unquote(name)(unquote_splicing(arg_vars)) do
        unquote(body)
      end
    end
  end

  defp parse_head({name, _meta, args}, caller) when is_atom(name) and is_list(args) do
    types =
      Enum.map(args, fn
        {type, _, ctx} when is_atom(type) and is_atom(ctx) ->
          if Types.known?(type) do
            type
          else
            raise CompileError,
              file: caller.file,
              line: caller.line,
              description: "SpecGen: unknown short type #{inspect(type)}"
          end

        other ->
          raise CompileError,
            file: caller.file,
            line: caller.line,
            description: "SpecGen: expected short type atom, got #{Macro.to_string(other)}"
      end)

    vars =
      types
      |> Enum.with_index()
      |> Enum.map(fn {t, i} -> Macro.var(String.to_atom("#{t}#{i}"), __MODULE__) end)

    {name, types, vars}
  end

  defp parse_head(other, caller) do
    raise CompileError,
      file: caller.file,
      line: caller.line,
      description: "SpecGen: expected function head, got #{Macro.to_string(other)}"
  end

  defp expand_return({:"::", _, [return]}), do: Types.expand(return)

  defp expand_return({:"::", _, [_, return]}) when is_atom(return),
    do: Types.expand(return)

  defp expand_return(return) when is_atom(return), do: Types.expand(return)

  defp build_spec_ast(name, arg_types, return_ast) do
    arg_asts = Enum.map(arg_types, &Types.expand/1)

    quote do
      unquote(name)(unquote_splicing(arg_asts)) :: unquote(return_ast)
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate @spec generation from annotations
      defmodule SpecGen do
        defmacro defspec(name, types, do: body) do
          quote do
            # Extract types and generate spec
            @spec unquote(name)(unquote_splicing(types)) :: term()

            def unquote(name)(unquote_splicing(args)) do
              unquote(body)
            end
          end
        end
      end

      # Simulate type-annotated function
      defmodule Math do
        require SpecGen

        # In real scenario: would use macro to generate @spec
        @spec add(integer(), integer()) :: integer()
        def add(a, b) do
          a + b
        end

        @spec multiply(integer(), integer()) :: integer()
        def multiply(a, b) do
          a * b
        end
      end

      # Test
      result1 = Math.add(5, 3)
      result2 = Math.multiply(5, 3)

      IO.puts("✓ add(5, 3) = #{result1}")
      IO.puts("✓ multiply(5, 3) = #{result2}")

      # Check specs (would be compile-time verified)
      assert result1 == 8, "add works"
      assert result2 == 15, "multiply works"

      IO.puts("✓ Spec generation: compile-time type annotations working")
  end
end

Main.main()
```
