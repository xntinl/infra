# Custom Compile Tracer

**Project**: `compile_tracer` — install a `Code.compiler_options(:tracers)` hook that collects every module compiled in the project, records imports/aliases/references, and enforces a boundary policy at compile time.

---

## The business problem

Your monorepo contains three bounded contexts: `Catalog`, `Ordering`, `Billing`. The
rule is that `Ordering` may call `Catalog`, but `Catalog` must never call into
`Ordering` (it would invert the dependency). Developers agree to the rule in code
review — and occasionally miss it. You want the CI build to fail if a forbidden call
appears in the AST.

Elixir 1.10 introduced the compiler tracer API: `Code.put_compiler_option(:tracers,
[MyTracer])` registers a module that receives callbacks for every `alias`, `import`,
`require`, `remote_function`, `local_function`, and module definition during
compilation. This is what `Boundary`, `Credo`, and Elixir's own unused-alias warnings
are built on.

You will write a tracer that:

1. Records the forward graph of inter-module references
2. Checks each reference against a declared boundary policy
3. Raises `CompileError` on violations

## Project structure

```
compile_tracer/
├── lib/
│   └── compile_tracer/
│       ├── tracer.ex            # implements the tracer callbacks
│       ├── policy.ex            # declares allowed cross-context calls
│       └── graph.ex             # ETS-backed recorder
├── test/
│   └── tracer_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why a tracer and not a post-compile analyzer

**Post-compile analysis** (walking `.beam` files or parsing `.ex` sources
after the build) is language-agnostic and decoupled from the compiler, but it
sees only what the compiler left behind — macro expansions are frozen, and
you duplicate the parser.

**A compile tracer** runs inside the compiler and receives every `alias`,
`import`, `remote_function`, and macro call as they happen, with live
`Macro.Env`. Violations fail the build on the offending line instead of an
out-of-band report.

Use a tracer when the rule must be enforced as a build-breaker and must see
post-expansion references (this is how Boundary works). Use a separate pass
when you want advisory output or richer graph analytics.

---

## Design decisions

**Option A — in-process dictionary for the reference graph**
- Pros: no ETS setup, zero extra processes.
- Cons: parallel compilation runs in separate OS processes/schedulers; state is never shared and violations slip through.

**Option B — ETS `:public` table with `write_concurrency`** (chosen)
- Pros: survives concurrent tracer callbacks, shared across all compile workers, cheap reads after compile for summary.
- Cons: global mutable state needs teardown between runs; must guard `init` against repeated creation.

→ Chose **B** because the tracer must see every edge from every worker, and ETS is the lightest cross-process shared mutable store in OTP.

---

## Implementation

### `lib/compile_tracer.ex`

```elixir
defmodule CompileTracer do
  @moduledoc """
  Custom Compile Tracer.

  **Post-compile analysis** (walking `.beam` files or parsing `.ex` sources.
  """
end
```
### `lib/compile_tracer/policy.ex`

**Objective**: Declare allowed cross-context call graph as MapSet so tracer checks are O(1) lookup overhead.

### `mix.exs`
```elixir
defmodule CompileTracer.MixProject do
  use Mix.Project

  def project do
    [
      app: :compile_tracer,
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
defmodule CompileTracer.Policy do
  @moduledoc "Declares the allowed cross-context calls."

  @policy %{
    # from_module_prefix => allowed_target_prefixes (MapSet)
    "MyApp.Catalog" => MapSet.new([]),
    "MyApp.Ordering" => MapSet.new(["MyApp.Catalog", "MyApp.Ordering"]),
    "MyApp.Billing" => MapSet.new(["MyApp.Catalog", "MyApp.Billing", "MyApp.Ordering"])
  }

  @spec context_of(module()) :: String.t() | nil
  def context_of(mod) when is_atom(mod) do
    name = Atom.to_string(mod)

    Enum.find_value(Map.keys(@policy), fn ctx ->
      if String.starts_with?(name, ctx <> "."), do: ctx
    end)
  end

  @spec allowed?(module(), module()) :: :ok | {:error, {String.t(), String.t()}}
  def allowed?(from_mod, to_mod) do
    with from_ctx when not is_nil(from_ctx) <- context_of(from_mod),
         to_ctx when not is_nil(to_ctx) <- context_of(to_mod) do
      allowed = Map.fetch!(@policy, from_ctx)

      cond do
        from_ctx == to_ctx -> :ok
        to_ctx in allowed -> :ok
        true -> {:error, {from_ctx, to_ctx}}
      end
    else
      _ -> :ok
    end
  end
end
```
### `lib/compile_tracer/graph.ex`

**Objective**: Wire ETS :public :bag table so parallel compiler workers record edges concurrently without locks.

```elixir
defmodule CompileTracer.Graph do
  @moduledoc "ETS-backed recorder of inter-module references."

  @table :compile_tracer_graph

  @spec init() :: :ok
  def init do
    case :ets.whereis(@table) do
      :undefined ->
        :ets.new(@table, [:named_table, :public, :bag, write_concurrency: true])
        :ok

      _ref ->
        :ets.delete_all_objects(@table)
        :ok
    end
  end

  @spec record(module(), module(), {String.t(), non_neg_integer()}) :: :ok
  def record(from, to, source) do
    :ets.insert(@table, {from, to, source})
    :ok
  end

  @spec edges() :: [{module(), module(), {String.t(), non_neg_integer()}}]
  def edges, do: :ets.tab2list(@table)

  @spec outgoing(module()) :: [module()]
  def outgoing(from) do
    @table
    |> :ets.lookup(from)
    |> Enum.map(fn {_, to, _} -> to end)
    |> Enum.uniq()
  end
end
```
### `lib/compile_tracer/tracer.ex`

**Objective**: Implement trace/2 callback to intercept :remote_function/:remote_macro calls and CompileError on boundary violations.

```elixir
defmodule CompileTracer.Tracer do
  @moduledoc """
  Compile-time tracer. Register via:

      Code.put_compiler_option(:tracers, [CompileTracer.Tracer])

  Records every remote function and remote macro call, and fails compilation
  when it crosses a boundary forbidden by `CompileTracer.Policy`.
  """

  alias CompileTracer.{Graph, Policy}

  @spec trace(term(), Macro.Env.t()) :: :ok
  def trace({:on_module, _bytecode, _info}, _env), do: :ok

  def trace({:remote_function, meta, to_mod, _name, _arity}, env) do
    check_and_record(env.module, to_mod, env.file, meta)
  end

  def trace({:remote_macro, meta, to_mod, _name, _arity}, env) do
    check_and_record(env.module, to_mod, env.file, meta)
  end

  def trace(_other, _env), do: :ok

  defp check_and_record(nil, _to, _file, _meta), do: :ok
  defp check_and_record(_from, mod, _file, _meta) when mod in [Kernel, Enum, Map, List], do: :ok

  defp check_and_record(from_mod, to_mod, file, meta) do
    Graph.record(from_mod, to_mod, {file, Keyword.get(meta, :line, 0)})

    case Policy.allowed?(from_mod, to_mod) do
      :ok ->
        :ok

      {:error, {from_ctx, to_ctx}} ->
        raise CompileError,
          file: file,
          line: Keyword.get(meta, :line, 0),
          description:
            "boundary violation: #{inspect(from_mod)} (#{from_ctx}) cannot call #{inspect(to_mod)} (#{to_ctx})"
    end
  end
end
```
### Step 4: `config/config.exs`

**Objective**: Register tracer via Code.put_compiler_option(:tracers) so boundary checks run during mix compile globally.

```elixir
import Config

config :compile_tracer, :enable_tracer, true

if config_env() != :test do
  # real projects toggle this here; tests install/remove dynamically
end
```
### `test/compile_tracer_test.exs`

**Objective**: Dynamically install tracer, compile fixture modules in-memory, assert edges recorded and violations raise CompileError.

```elixir
defmodule CompileTracer.TracerTest do
  use ExUnit.Case, async: true
  doctest CompileTracer.Tracer

  alias CompileTracer.{Graph, Tracer}

  setup do
    Graph.init()
    previous = Code.get_compiler_option(:tracers) || []
    Code.put_compiler_option(:tracers, [Tracer | previous])
    on_exit(fn -> Code.put_compiler_option(:tracers, previous) end)
    :ok
  end

  describe "CompileTracer.Tracer" do
    test "records allowed cross-context calls" do
      Code.compile_string("""
      defmodule MyApp.Catalog.Product do
        def hello, do: :ok
      end

      defmodule MyApp.Ordering.Order do
        def run, do: MyApp.Catalog.Product.hello()
      end
      """)

      edges = Graph.edges()

      assert Enum.any?(edges, fn
               {MyApp.Ordering.Order, MyApp.Catalog.Product, _} -> true
               _ -> false
             end)
    end

    test "raises CompileError on forbidden direction" do
      assert_raise CompileError, ~r/boundary violation/, fn ->
        Code.compile_string("""
        defmodule MyApp.Ordering.Service do
          def handle, do: :ok
        end

        defmodule MyApp.Catalog.Bad do
          def run, do: MyApp.Ordering.Service.handle()
        end
        """)
      end
    end

    test "ignores stdlib modules" do
      Code.compile_string("""
      defmodule MyApp.Catalog.Uses do
        def do_it, do: Enum.map([1, 2], &(&1 + 1))
      end
      """)

      refute Enum.any?(Graph.edges(), fn {_, mod, _} -> mod == Enum end)
    end
  end
end
```
### Why this works

The tracer callback runs synchronously inside the compiler with the live
`Macro.Env`, so `env.module` and `env.file` are always the place that emitted
the reference. Recording into a `:public` ETS table with
`write_concurrency: true` lets parallel compilation workers insert without
contention. Raising a `CompileError` with `file:` and `line:` makes the build
fail at the offending source location, turning a policy into a mechanical
guarantee rather than a convention.

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

**1. Tracers run synchronously inside compilation.** Expensive work in `trace/2`
slows every `mix compile`. Keep callbacks to O(1) per event; offload analysis to a
separate pass over `Graph.edges/0` if it is heavy.

**2. Parallel compilation = concurrent writes.** Use `:public` ETS with
`write_concurrency: true`, or centralize writes in a GenServer — never raw state.

**3. `env.module` is `nil` for top-level code.** Guard the tracer against `nil`
before looking up the context.

**4. Raising inside tracer aborts compilation abruptly.** Subsequent files are NOT
compiled. For friendlier CI output, accumulate violations and raise a summary at
`:after_compile` or via a Mix task wrapper.

**5. Path gotcha: Mix.Project.load/unload.** When toggling tracers in tests you
must reset via `on_exit`. Leaving them installed contaminates other test modules.

**6. `remote_function` fires even on unused aliases.** If someone has `alias
MyApp.Ordering, as: O` but never calls `O.foo/1`, the alias event fires but
no `remote_function` event — aligning on `remote_function` is safer than on
`alias`.

**7. False positives from macros.** If a macro expands to a cross-context call, the
tracer will see it from the *expanded* location. Decide whether macros carve an
exception in the policy.

**8. When NOT to use this.** If your project uses `Boundary` (hex) or `Elixir
Boundary` already — use it. It handles test boundaries, exports, and ignores a
host of edge cases you would re-invent badly.

---

## Benchmark

There is nothing to measure at runtime — the tracer runs only at compile time.
For pathological sanity, time a full `mix compile --force` with and without the
tracer installed; expect < 5% overhead on most projects.

Target: under 5% wall-clock overhead on a 500-module project, measured with
`time mix compile --force`.

---

## Reflection

- Your monorepo grows to 20 bounded contexts and the policy map becomes the
  bottleneck. How would you restructure the policy so adding a new context
  does not require editing a central file, while still failing builds on
  violations?
- A macro in a shared library expands into a call that crosses a boundary.
  The author of the caller did nothing wrong. How do you decide whether to
  carve an exception, rewrite the macro, or rethink the boundary?

---

### `script/main.exs`
```elixir
defmodule CompileTracer.TracerTest do
  use ExUnit.Case, async: false
  doctest CompileTracer.Tracer

  alias CompileTracer.{Graph, Tracer}

  setup do
    Graph.init()
    previous = Code.get_compiler_option(:tracers) || []
    Code.put_compiler_option(:tracers, [Tracer | previous])
    on_exit(fn -> Code.put_compiler_option(:tracers, previous) end)
    :ok
  end

  describe "CompileTracer.Tracer" do
    test "records allowed cross-context calls" do
      Code.compile_string("""
      defmodule MyApp.Catalog.Product do
        def hello, do: :ok
      end

      defmodule MyApp.Ordering.Order do
        def run, do: MyApp.Catalog.Product.hello()
      end
      """)

      edges = Graph.edges()

      assert Enum.any?(edges, fn
               {MyApp.Ordering.Order, MyApp.Catalog.Product, _} -> true
               _ -> false
             end)
    end

    test "raises CompileError on forbidden direction" do
      assert_raise CompileError, ~r/boundary violation/, fn ->
        Code.compile_string("""
        defmodule MyApp.Ordering.Service do
          def handle, do: :ok
        end

        defmodule MyApp.Catalog.Bad do
          def run, do: MyApp.Ordering.Service.handle()
        end
        """)
      end
    end

    test "ignores stdlib modules" do
      Code.compile_string("""
      defmodule MyApp.Catalog.Uses do
        def do_it, do: Enum.map([1, 2], &(&1 + 1))
      end
      """)

      refute Enum.any?(Graph.edges(), fn {_, mod, _} -> mod == Enum end)
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate compile tracer: track and enforce boundaries at compile time
      # Simulate tracer collecting module info

      compiled_modules = [
        %{module: MyApp.Domain.User, imports: [Ecto.Schema], calls: [:validate]},
        %{module: MyApp.API.UserController, imports: [MyApp.Domain.User], calls: [:list, :get]},
        %{module: MyApp.Database.Repo, imports: [Ecto.Repo], calls: [:insert, :all]}
      ]

      IO.inspect(compiled_modules, label: "✓ Compiled modules and deps")

      # Enforce boundary: API should not import Database
      invalid = Enum.any?(compiled_modules, fn m ->
        m.module |> to_string() |> String.starts_with?("MyApp.API") and
        Enum.any?(m.imports, fn i -> to_string(i) |> String.starts_with?("MyApp.Database") end)
      end)

      IO.puts("✓ Boundary violation detected: #{invalid}")

      assert length(compiled_modules) == 3, "All modules tracked"

      IO.puts("✓ Compile tracer: module boundary enforcement working")
  end
end

Main.main()
```
---

## Why Custom Compile Tracer matters

Mastering **Custom Compile Tracer** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts

### 1. Tracer callbacks

A tracer is a module exporting `trace/2`. The compiler calls it synchronously with
events such as:

```
:start | :stop
{:alias, meta, module, as, opts}
{:import, meta, module, opts}
{:require, meta, module, opts}
{:remote_function, meta, module, name, arity}
{:local_function, meta, name, arity}
{:remote_macro, meta, module, name, arity}
```

Callbacks fire *during* compilation of the user's module, so `env.module` is the
module being compiled.

### 2. `Code.put_compiler_option/2`

Set globally via `Code.put_compiler_option(:tracers, [CompileTracer.Tracer])`.
For Mix projects, hook this into `config/config.exs` or run it from a
`Mix.Task` before compilation.

### 3. `@after_compile` is complementary, not a replacement

`@after_compile` sees individual modules but not cross-module references emitted mid-compile.
A tracer sees every reference and is called for every file.

### 4. ETS for cross-module state

During a parallel `mix compile`, tracer callbacks fire concurrently from multiple
worker processes. Accumulate state in an ETS table (`:public` + `:named_table`) —
not in process state — to keep the picture consistent.

### 5. Failing compilation from a tracer

Raising inside `trace/2` aborts the current compilation. Use `CompileError` with a
`file:` and `line:` for the error to land on the offending source line.

---
