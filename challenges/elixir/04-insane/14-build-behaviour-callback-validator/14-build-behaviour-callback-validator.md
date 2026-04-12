# Compile-Time Behaviour Callback Validator

**Project**: `behaviour_check` — a static analysis tool that enforces strict behaviour compliance at compile time

---

## Project context

You are building `behaviour_check`, a Mix compiler and Mix task that enforces behaviour compliance beyond what the Elixir compiler provides. Missing callbacks become errors, type mismatches become warnings, and undocumented implementations become warnings — all at `mix compile` time.

Project structure:

```
behaviour_check/
├── lib/
│   └── behaviour_check/
│       ├── application.ex           # starts nothing; framework for compile hooks
│       ├── validator.ex             # core validator: checks modules, emits diagnostics
│       ├── callback_loader.ex       # reads @callback specs from behaviour modules
│       ├── impl_loader.ex           # reads @spec and @doc from implementing modules via :beam_lib
│       ├── type_checker.ex          # structural comparison of spec ASTs
│       ├── inheritance.ex           # resolves BehaviourB extends BehaviourA callbacks
│       └── compiler.ex              # Mix.Compiler implementation: hooks into mix compile
├── mix_tasks/
│   └── mix/tasks/behaviour/check.ex # mix behaviour.check task
├── test/
│   └── behaviour_check/
│       ├── validator_test.exs        # missing required, optional warning, type mismatch
│       ├── inheritance_test.exs      # inherited callbacks enforced
│       ├── documentation_test.exs    # missing @doc warning
│       └── mix_task_test.exs         # exit code 1 on violations
├── bench/
│   └── validator_bench.exs
└── mix.exs
```

---

## The problem

The Elixir compiler emits a warning when a module declares `@behaviour MyBehaviour` but does not implement a required callback. It does not check types, it does not enforce `@doc`, and it does not support behaviour inheritance. In a large codebase with many behaviours, these gaps lead to silent API drift — implementing modules that satisfy the compiler but violate the contract their behaviour defines.

This tool closes those gaps by reading module metadata from `.beam` files after compilation and emitting structured diagnostics.

---

## Why this design

**`@after_compile` hooks**: the Elixir compiler calls `@after_compile` callbacks after a module is compiled but before the build finishes. This is the correct point to inspect the compiled module — the beam file exists, all attributes are finalized, but the build process can still emit errors or warnings.

**`:beam_lib` for spec extraction**: the Elixir compiler embeds `@spec`, `@callback`, and `@doc` metadata in the `.beam` file's `"ExCk"` chunk (Elixir type information) and `abstract_code` chunk (Erlang abstract forms). `:beam_lib.chunks/2` retrieves this metadata without loading the module.

**Structural spec comparison**: you cannot compare spec types by string equality. `String.t()` and `binary()` are equivalent; `[atom()]` and `list(atom())` are equivalent. Structural comparison walks both type ASTs and returns true if they denote the same type. You do not need a complete type checker — a conservative approximation that catches obvious mismatches is sufficient.

**Mix compiler integration**: implementing the `Mix.Compiler` behaviour allows `behaviour_check` to run automatically as part of `mix compile`. The compiler receives a list of modules that were compiled in this pass and can emit diagnostics against them.

---

## Design decisions

**Option A — Runtime reflection with `Code.ensure_loaded/1` and `function_exported?/3`**
- Pros: works without macro magic; easy to debug.
- Cons: errors surface at runtime, not at compile time — exactly the opposite of what a validator should give you.

**Option B — Compile-time `@after_compile` hook that inspects the module** (chosen)
- Pros: invalid implementations fail `mix compile`, not at runtime; integrates with the editor; errors point at source lines.
- Cons: macro code is harder to read and test; must account for module attributes not finalized until after compile.

→ Chose **B** because a behaviour validator exists precisely to catch bugs before runtime; doing it at runtime defeats its entire purpose.

## Implementation milestones

### Step 1: Create the project

```bash
mix new behaviour_check --sup
cd behaviour_check
mkdir -p lib/behaviour_check mix_tasks/mix/tasks/behaviour test/behaviour_check bench
```

### Step 2: `mix.exs` — no external dependencies needed

The validator uses only OTP's `:beam_lib` and Elixir's `Code` module.

### Step 3: Callback loader

```elixir
# lib/behaviour_check/callback_loader.ex
defmodule BehaviourCheck.CallbackLoader do
  @moduledoc """
  Reads @callback and @optional_callbacks from a behaviour module.
  Returns lists of {name, arity, type_spec} for required and optional callbacks.
  """

  @spec load(module()) :: {[{atom(), non_neg_integer()}], [{atom(), non_neg_integer()}]}
  def load(behaviour_module) do
    all_callbacks = behaviour_module.behaviour_info(:callbacks)

    optional =
      try do
        behaviour_module.behaviour_info(:optional_callbacks)
      rescue
        _ -> []
      end

    required = all_callbacks -- optional
    {required, optional}
  end

  defp beam_path(module) do
    :code.which(module)
  end
end
```

### Step 4: Validator

```elixir
# lib/behaviour_check/validator.ex
defmodule BehaviourCheck.Validator do
  @moduledoc """
  Validates a module against its declared behaviours.
  Returns a list of diagnostics: {:error | :warning, message, location}.
  """

  def validate(module) do
    behaviours = module.__info__(:attributes)[:behaviour] || []
    Enum.flat_map(behaviours, fn behaviour ->
      validate_against(module, behaviour)
    end)
  end

  defp validate_against(module, behaviour) do
    {required, optional} = BehaviourCheck.CallbackLoader.load(behaviour)
    implemented = module.__info__(:functions)

    missing_required  = check_missing_required(required, implemented, module)
    missing_optional  = check_missing_optional(optional, implemented, module)
    type_mismatches   = check_type_specs(required ++ optional, module, behaviour)
    missing_docs      = check_documentation(required ++ optional, module)

    missing_required ++ missing_optional ++ type_mismatches ++ missing_docs
  end

  defp check_missing_required(callbacks, implemented, module) do
    implemented_set = MapSet.new(implemented)

    Enum.flat_map(callbacks, fn {name, arity} ->
      if MapSet.member?(implemented_set, {name, arity}) do
        []
      else
        location = {to_string(module), 0}
        [{:error, "missing required callback #{name}/#{arity}", location}]
      end
    end)
  end

  defp check_missing_optional(callbacks, implemented, module) do
    implemented_set = MapSet.new(implemented)

    Enum.flat_map(callbacks, fn {name, arity} ->
      if MapSet.member?(implemented_set, {name, arity}) do
        []
      else
        location = {to_string(module), 0}
        [{:warning, "optional callback #{name}/#{arity} not implemented", location}]
      end
    end)
  end

  defp check_type_specs(_callbacks, _module, _behaviour), do: []

  defp check_documentation(_callbacks, _module), do: []
end
```

### Step 5: Mix compiler

```elixir
# lib/behaviour_check/compiler.ex
defmodule BehaviourCheck.Compiler do
  @moduledoc "Mix compiler that runs behaviour validation after each compile pass."

  use Mix.Task.Compiler

  @impl true
  def run(argv) do
    # Ensure modules are compiled first
    Mix.Task.run("compile.elixir", argv)

    modules = get_project_modules()
    diagnostics = Enum.flat_map(modules, &BehaviourCheck.Validator.validate/1)

    errors   = Enum.filter(diagnostics, fn {sev, _, _} -> sev == :error end)
    warnings = Enum.filter(diagnostics, fn {sev, _, _} -> sev == :warning end)

    Enum.each(warnings, fn {_, msg, loc} -> Mix.shell().info("warning: #{msg} at #{inspect(loc)}") end)
    Enum.each(errors,   fn {_, msg, loc} -> Mix.shell().error("error: #{msg} at #{inspect(loc)}") end)

    if Enum.any?(errors), do: {:error, diagnostics}, else: {:ok, diagnostics}
  end

  defp get_project_modules do
    compile_path = Mix.Project.compile_path()

    Path.wildcard(Path.join(compile_path, "*.beam"))
    |> Enum.map(fn beam_file ->
      beam_file
      |> String.to_charlist()
      |> :beam_lib.info()
      |> case do
        {:ok, {module, _}} -> module
        _ -> nil
      end
    end)
    |> Enum.reject(&is_nil/1)
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/behaviour_check/validator_test.exs
defmodule BehaviourCheck.ValidatorTest do
  use ExUnit.Case, async: true

  # Define a test behaviour
  defmodule TestBehaviour do
    @callback required_fn(atom()) :: {:ok, term()} | {:error, term()}
    @callback optional_fn(integer()) :: boolean()
    @optional_callbacks [optional_fn: 1]
  end

  # Missing required callback
  defmodule MissingRequired do
    @behaviour TestBehaviour
    # does NOT implement required_fn/1
    def optional_fn(_), do: true
  end

  # Missing optional callback
  defmodule MissingOptional do
    @behaviour TestBehaviour
    def required_fn(_), do: {:ok, :done}
    # does NOT implement optional_fn/1
  end

  test "missing required callback emits :error diagnostic" do
    diagnostics = BehaviourCheck.Validator.validate(MissingRequired)
    errors = Enum.filter(diagnostics, fn {sev, _, _} -> sev == :error end)

    assert Enum.any?(errors, fn {_, msg, _} ->
      String.contains?(msg, "required_fn/1")
    end), "expected error about missing required_fn/1, got: #{inspect(errors)}"
  end

  test "missing optional callback emits :warning diagnostic" do
    diagnostics = BehaviourCheck.Validator.validate(MissingOptional)
    warnings = Enum.filter(diagnostics, fn {sev, _, _} -> sev == :warning end)

    assert Enum.any?(warnings, fn {_, msg, _} ->
      String.contains?(msg, "optional_fn/1")
    end)
  end

  test "complete implementation emits no diagnostics" do
    defmodule CompleteImpl do
      @behaviour TestBehaviour
      @doc "Creates something"
      def required_fn(_), do: {:ok, :done}
      @doc "Checks something"
      def optional_fn(_), do: true
    end

    assert [] = BehaviourCheck.Validator.validate(CompleteImpl)
  end
end
```

```elixir
# test/behaviour_check/inheritance_test.exs
defmodule BehaviourCheck.InheritanceTest do
  use ExUnit.Case, async: true

  defmodule BehaviourA do
    @callback foo(atom()) :: :ok
  end

  defmodule BehaviourB do
    use BehaviourA
    @callback bar(integer()) :: boolean()
  end

  defmodule MissingFoo do
    @behaviour BehaviourB
    # implements bar but not foo
    def bar(_), do: true
  end

  test "module missing inherited callback emits error" do
    diagnostics = BehaviourCheck.Validator.validate(MissingFoo)
    errors = Enum.filter(diagnostics, fn {sev, _, _} -> sev == :error end)

    assert Enum.any?(errors, fn {_, msg, _} ->
      String.contains?(msg, "foo/1")
    end)
  end
end
```

### Step 7: Run the tests

```bash
mix test test/behaviour_check/ --trace
```

### Step 8: Test the Mix task

```bash
mix behaviour.check
echo "Exit code: $?"
```

Expected: exit code 0 on a clean project, exit code 1 if any violations exist.

### Why this works

The `@after_compile` callback inspects the module's `__info__(:functions)` and compares it to the behaviour's `@callback` list, raising `CompileError` with a precise message and source location. This makes the validator fully declarative at the use-site.

---

## Benchmark

```elixir
# bench/validator_bench.exs
:timer.tc(fn -> Code.compile_file("lib/sample.ex") end)
```

Target: Validation adds < 100 ms to `mix compile` even on a 200-module project.

---

## Trade-off analysis

| Aspect | Your validator | Dialyzer | Elixir compiler default |
|--------|----------------|----------|------------------------|
| Execution point | `mix compile` | `mix dialyzer` (separate run) | `mix compile` |
| Required callback check | error | warning | warning |
| Type mismatch check | structural (conservative) | full type inference | none |
| Optional callback | warning | none | none |
| Documentation enforcement | warning | none | none |
| Speed | fast (metadata only) | slow (full PLT analysis) | fast |
| False positives | possible (structural only) | low | n/a |

Architectural question: your structural type checker is conservative — it may miss some mismatches and flag some valid implementations. What are the cases where structural comparison is insufficient? What would you need to implement a complete type equivalence check?

---

## Common production mistakes

**1. Running validation before the beam files exist**
If your `@after_compile` hook fires before the module's beam file is written to disk, `:beam_lib` cannot find it. Ensure the hook path matches the actual output path from `Mix.Project.compile_path/0`.

**2. Comparing spec strings instead of AST nodes**
`atom()` and `Atom.t()` are equivalent but not string-equal. You must parse both spec strings into AST with `Code.string_to_quoted/1` and compare the AST trees.

**3. Not handling behaviours that are Erlang modules**
Erlang behaviour modules store callback info in `module.behaviour_info(:callbacks)`, not in Elixir's `@callback` attributes. Your loader must handle both cases.

**4. Emitting errors for optional callbacks**
Required and optional callbacks have different enforcement rules. Confusing them causes false positives that block compilation for valid modules.

## Reflection

- If a behaviour has 50 optional callbacks, should your validator warn on missing optional ones, or silently accept? Make a policy argument.
- How would you extend this to validate callback *types* (not just arity) using `@spec`? Sketch the approach.

---

## Resources

- [Elixir `Module` source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/module.ex) — how Elixir stores module attributes
- [Erlang abstract format](https://www.erlang.org/doc/apps/erts/absform) — the format returned by `:beam_lib.chunks/2`
- [`:beam_lib` documentation](https://www.erlang.org/doc/man/beam_lib)
- McCord, C. — *Metaprogramming Elixir* — Chapters 4–5 on `__using__` and compiler hooks
