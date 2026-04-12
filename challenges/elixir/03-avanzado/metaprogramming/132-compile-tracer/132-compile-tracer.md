# Custom Compile Tracer

**Project**: `compile_tracer` — install a `Code.compiler_options(:tracers)` hook that collects every module compiled in the project, records imports/aliases/references, and enforces a boundary policy at compile time.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

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

```
compile_tracer/
├── lib/
│   └── compile_tracer/
│       ├── tracer.ex            # implements the tracer callbacks
│       ├── policy.ex            # declares allowed cross-context calls
│       └── graph.ex             # ETS-backed recorder
├── test/
│   └── tracer_test.exs
└── mix.exs
```

---

## Core concepts

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

## Implementation

### Step 1: `lib/compile_tracer/policy.ex`

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

### Step 2: `lib/compile_tracer/graph.ex`

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

### Step 3: `lib/compile_tracer/tracer.ex`

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

```elixir
import Config

config :compile_tracer, :enable_tracer, true

if config_env() != :test do
  # real projects toggle this here; tests install/remove dynamically
end
```

### Step 5: Tests — install the tracer, compile in-memory modules

```elixir
defmodule CompileTracer.TracerTest do
  use ExUnit.Case, async: false

  alias CompileTracer.{Graph, Tracer}

  setup do
    Graph.init()
    previous = Code.get_compiler_option(:tracers) || []
    Code.put_compiler_option(:tracers, [Tracer | previous])
    on_exit(fn -> Code.put_compiler_option(:tracers, previous) end)
    :ok
  end

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
```

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

---

## Resources

- [`Code.compiler_options/1` + tracers — hexdocs.pm](https://hexdocs.pm/elixir/Code.html#compiler_options/1)
- [Boundary library — source](https://github.com/sasa1977/boundary) — Saša Jurić
- [Elixir 1.10 release notes — tracer API introduction](https://elixir-lang.org/blog/2020/01/27/elixir-v1-10-0-released/)
- [José Valim — "Mix compilers and tracers"](https://dashbit.co/blog) — Dashbit
- [Credo — how it uses traversal, not tracers](https://github.com/rrrene/credo)
- [Macro.Env docs](https://hexdocs.pm/elixir/Macro.Env.html)
