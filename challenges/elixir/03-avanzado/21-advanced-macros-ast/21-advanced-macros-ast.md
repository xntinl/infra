# Advanced Macros and AST Manipulation

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway has middleware functions that validate,
transform, and log requests. The engineering team wants tooling to:
1. Inspect what any middleware does at compile time without running it
2. Automatically instrument middleware with timing and logging by transforming its AST
3. Analyze which variables a middleware function reads from the connection struct

These requirements push you into AST manipulation — reading, traversing, and
transforming the abstract syntax tree before code is compiled.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── middleware/
│       │   ├── pipeline.ex
│       │   └── instrumentation.ex
│       └── dev/
│           └── ast_tools.ex
├── test/
│   └── api_gateway/
│       └── middleware/
│           └── instrumentation_test.exs
└── mix.exs
```

---

## The business problem

Three tooling requirements:

1. **Compile-time inspection**: during code review, developers want to print the AST
   of any expression to understand what the compiler sees. The macro must print at
   compile time (not runtime) and still return the correct value.

2. **Automatic instrumentation**: instead of manually wrapping every middleware function
   with timing code, a macro should transform the function body at compile time to
   add `System.monotonic_time` measurements and emit a telemetry event.

3. **Variable analysis**: a static analysis tool that lists all variables referenced
   in a block of code — useful for dead code analysis and security audits.

---

## The Elixir AST format

Every Elixir expression, before compilation, is represented as nested tuples:

```elixir
quote do: 1 + 2
#=> {:+, [context: Elixir, imports: [{2, Kernel}]], [1, 2]}

quote do: foo(bar)
#=> {:foo, [line: 1], [{:bar, [line: 1], Elixir}]}

# Variables vs function calls — distinguished by the third element:
quote do: x         #=> {:x, [], Elixir}       # variable: atom context
quote do: x()       #=> {:x, [], []}            # call: empty list args
quote do: x(1)      #=> {:x, [], [1]}           # call with args: non-empty list
```

The critical distinction: `{name, meta, context}` where `context` is an atom
(or `nil`) for variables, and a list for function calls.

---

## Why `Macro.escape` matters

Inside a `quote do ... end` block, `unquote(expr)` inserts `expr` as **code to execute**.
`unquote(Macro.escape(expr))` inserts the AST of `expr` as **data** (a literal value
that produces the AST tuple when evaluated).

```elixir
expr = quote do: 1 + 2   # => {:+, [], [1, 2]}

# Without Macro.escape:
quote do: IO.inspect(unquote(expr))
# expands to: IO.inspect(1 + 2)  ← evaluates to IO.inspect(3)

# With Macro.escape:
quote do: IO.inspect(unquote(Macro.escape(expr)))
# expands to: IO.inspect({:+, [], [1, 2]})  ← prints the AST tuple
```

---

## Implementation

### Step 1: `lib/api_gateway/dev/ast_tools.ex`

```elixir
defmodule ApiGateway.Dev.ASTTools do
  @moduledoc """
  Compile-time AST inspection and analysis utilities.
  These macros operate at compile time — all output is produced during compilation,
  not during test or production runtime.
  """

  @doc """
  Prints the AST of `expr` at compile time, then evaluates and returns `expr` normally.

  Usage:
    import ApiGateway.Dev.ASTTools
    result = ast_inspect(conn.method == "GET")
    # During compilation prints the AST and code representation.
    # At runtime, result = true or false depending on conn.method.
  """
  defmacro ast_inspect(expr) do
    ast_str = Macro.to_string(expr)
    escaped = Macro.escape(expr)

    quote do
      IO.inspect(unquote(escaped), label: "[AST] #{unquote(ast_str)}")
      unquote(expr)
    end
  end

  @doc """
  Analyzes a block of code at compile time and returns the list of variable names
  referenced in it, deduplicated and sorted.

  This is a compile-time operation: the returned list is available as a literal
  value at the call site. The block is NOT executed.

  Uses Macro.postwalk to traverse the AST bottom-up. Variables are identified by
  the three-tuple {name, meta, context} where context is an atom (not a list).
  Function calls have a list as the third element, so they are excluded.
  Variables starting with underscore are excluded by convention (they signal
  intentionally unused bindings).

  Usage:
    vars = ApiGateway.Dev.ASTTools.referenced_vars do
      method = conn.method
      path = conn.request_path
      _ignored = conn.body_params
    end
    # vars == [:conn, :method, :path]
  """
  defmacro referenced_vars(do: block) do
    {_ast, vars} =
      Macro.postwalk(block, [], fn
        # Variables: third element is an atom (not a list)
        {name, _meta, ctx} = node, acc
        when is_atom(name) and not is_list(ctx) ->
          {node, [name | acc]}

        node, acc ->
          {node, acc}
      end)

    result =
      vars
      |> Enum.reject(fn name ->
        name == :_ or String.starts_with?(to_string(name), "_")
      end)
      |> Enum.uniq()
      |> Enum.sort()

    # Macro.escape converts the list to an AST that produces the list at runtime
    Macro.escape(result)
  end
end
```

### Step 2: `lib/api_gateway/middleware/instrumentation.ex`

```elixir
defmodule ApiGateway.Middleware.Instrumentation do
  @moduledoc """
  Compile-time AST transformation that wraps function bodies with timing and telemetry.

  Usage:
    defmodule ApiGateway.Middleware.AuthCheck do
      import ApiGateway.Middleware.Instrumentation

      instrument def call(conn, _opts) do
        # ... middleware logic
        conn
      end
    end

  The `instrument` macro transforms the AST of the function body to:
    1. Record start time with System.monotonic_time(:microsecond)
    2. Execute the original body
    3. Emit a :telemetry event with elapsed time and function metadata

  The macro pattern-matches on the AST structure of a `def` form to extract
  the function name, arguments, and body. It then wraps the body in timing
  code and re-emits the `def` with the instrumented body.
  """

  @doc """
  Wraps a function definition with automatic timing instrumentation.
  Transforms the function body at compile time — no runtime overhead
  beyond the timing calls themselves.
  """
  defmacro instrument({:def, meta, [{name, fun_meta, args}, [do: body]]}) do
    module_ref = __CALLER__.module
    fun_name_str = to_string(name)

    instrumented_body =
      quote do
        __start__ = System.monotonic_time(:microsecond)

        result =
          try do
            unquote(body)
          rescue
            e ->
              elapsed = System.monotonic_time(:microsecond) - __start__
              :telemetry.execute(
                [:api_gateway, :middleware, :exception],
                %{duration_us: elapsed},
                %{module: unquote(module_ref), function: unquote(fun_name_str), error: e}
              )
              reraise e, __STACKTRACE__
          end

        elapsed = System.monotonic_time(:microsecond) - __start__

        :telemetry.execute(
          [:api_gateway, :middleware, :call],
          %{duration_us: elapsed},
          %{module: unquote(module_ref), function: unquote(fun_name_str)}
        )

        result
      end

    {:def, meta, [{name, fun_meta, args}, [do: instrumented_body]]}
  end

  @doc """
  Counts the number of timing instrumentation injections a `do` block would receive
  if passed through `instrument/1`. Used in testing to verify transformation behavior.

  Traverses the AST with Macro.prewalk, counting every {:def, _, _} node found.
  Each such node represents one function definition that would be instrumented.
  """
  @spec count_instrument_sites(Macro.t()) :: non_neg_integer()
  def count_instrument_sites(ast) do
    {_ast, count} =
      Macro.prewalk(ast, 0, fn
        {:def, _meta, _args} = node, acc -> {node, acc + 1}
        node, acc -> {node, acc}
      end)

    count
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/instrumentation_test.exs
defmodule ApiGateway.Middleware.InstrumentationTest do
  use ExUnit.Case, async: true

  import ApiGateway.Middleware.Instrumentation
  alias ApiGateway.Dev.ASTTools

  # ---------------------------------------------------------------------------
  # ASTTools tests
  # ---------------------------------------------------------------------------

  describe "referenced_vars/1" do
    test "returns sorted, deduplicated variable names" do
      vars =
        ASTTools.referenced_vars do
          method = conn.method
          path = conn.request_path
          result = String.upcase(method) <> path
          _ignored = "not counted"
        end

      # conn, method, path, result — conn appears multiple times but deduped
      assert :conn in vars
      assert :method in vars
      assert :path in vars
      assert :result in vars
      # _ignored must be excluded
      refute :_ignored in vars
      # List must be sorted
      assert vars == Enum.sort(vars)
    end

    test "excludes variables starting with underscore" do
      vars =
        ASTTools.referenced_vars do
          _private = 1
          _also_private = 2
          public = 3
        end

      assert vars == [:public]
    end

    test "returns empty list for a block with no variables" do
      vars =
        ASTTools.referenced_vars do
          1 + 2
          "hello"
        end

      assert vars == []
    end
  end

  # ---------------------------------------------------------------------------
  # Instrumentation macro tests
  # ---------------------------------------------------------------------------

  describe "instrument/1" do
    defmodule SampleMiddleware do
      import ApiGateway.Middleware.Instrumentation

      instrument def process(conn, _opts) do
        Map.put(conn, :processed, true)
      end
    end

    setup do
      # Attach a telemetry handler to capture events
      handler_id = :test_handler
      test_pid = self()

      :telemetry.attach(
        handler_id,
        [:api_gateway, :middleware, :call],
        fn event, measurements, metadata, _config ->
          send(test_pid, {:telemetry, event, measurements, metadata})
        end,
        nil
      )

      on_exit(fn -> :telemetry.detach(handler_id) end)
      :ok
    end

    test "executes the original function logic correctly" do
      result = SampleMiddleware.process(%{method: "GET"}, [])
      assert result == %{method: "GET", processed: true}
    end

    test "emits a telemetry event with duration" do
      SampleMiddleware.process(%{}, [])

      assert_receive {:telemetry, [:api_gateway, :middleware, :call], measurements, metadata},
                     1_000

      assert is_integer(measurements.duration_us)
      assert measurements.duration_us >= 0
      assert metadata.function == "process"
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/middleware/instrumentation_test.exs --trace
```

---

## Trade-off analysis

| Technique | Compile-time cost | Runtime overhead | Debuggability |
|-----------|------------------|-----------------|---------------|
| `Macro.prewalk` | O(n) AST nodes | Zero | Hard — errors show AST positions |
| `Macro.postwalk` | O(n) AST nodes | Zero | Hard — same as prewalk |
| `Macro.traverse` | O(n) AST nodes | Zero | Hardest — pre+post in one pass |
| `Code.eval_quoted` | O(n) | Runtime eval cost | Worst — no compiler optimizations |
| Regular function | None | Normal | Best |

Reflection: `referenced_vars` is a compile-time macro — the list of variables
is a literal in the compiled beam file. What happens if you call it with a block
that references variables that don't exist yet? Does the macro care? Why?

---

## Common production mistakes

**1. Using `Code.eval_quoted` in production hot paths**
`Code.eval_quoted` bypasses the compiler — no type checking, no optimization, no
dialyzer analysis. Use it only for developer tooling or one-time startup operations.

**2. Not using `Macro.escape` when passing AST as data**
Without `Macro.escape`, `unquote(some_ast)` inside a `quote` block injects the
code for evaluation, not the AST structure as a value. The difference is subtle
and causes confusing runtime errors.

**3. Writing match specs manually when `fun2ms` suffices**
AST manipulation macros are powerful but complex. For ETS match specs specifically,
`:ets.fun2ms/1` handles the transformation correctly within its constraints.
Only write AST transformation when `fun2ms` cannot (dynamic runtime arguments).

**4. Ignoring `@before_compile` ordering with multiple `use` calls**
If a module uses two DSLs that both register `@before_compile` hooks, the hooks
run in reverse registration order. Hooks that depend on each other must account
for this ordering or use a single coordinating hook.

**5. Accumulating module attributes in LIFO order**
`Module.register_attribute(mod, :rules, accumulate: true)` inserts in LIFO order.
`@rules :a; @rules :b` gives `[:b, :a]` when read. Always `Enum.reverse/1` the
accumulator in `@before_compile`.

---

## Resources

- [Elixir `Macro` module documentation](https://hexdocs.pm/elixir/Macro.html) — `prewalk`, `postwalk`, `traverse`, `escape`
- [Metaprogramming Elixir — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — the definitive book on Elixir macros
- [Quote and unquote — Elixir guides](https://elixir-lang.org/getting-started/meta/quote-and-unquote.html)
- [`:telemetry` library](https://hexdocs.pm/telemetry/readme.html) — the instrumentation standard in the Elixir ecosystem
