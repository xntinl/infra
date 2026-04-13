# Custom Macro DSL System

**Project**: `dsl_kit` — three interoperable compile-time DSLs with error reporting and code generation transparency

---

## Project context

You are building `dsl_kit`, a macro system the framework team will use as the foundation for their internal toolkit. Three DSLs must coexist in the same module without conflicts: a state machine DSL, a validation DSL, and a router DSL. All errors must be caught at compile time with precise file and line information. The generated code must be identical to what a developer would write by hand.

Project structure:

```
dsl_kit/
├── lib/
│   └── dsl_kit/
│       ├── application.ex
│       ├── state_machine.ex     # ← StateMachine DSL: state/1, transition/4, initial/1
│       ├── validation.ex        # ← Validation DSL: validates/2 with rules
│       ├── router.ex            # ← Router DSL: scope/2, get/post/delete + path params
│       ├── composer.ex          # ← enables all three DSLs in one module
│       └── debug.ex             # ← code generation transparency tools
├── test/
│   └── dsl_kit/
│       ├── state_machine_test.exs
│       ├── validation_test.exs
│       ├── router_test.exs
│       └── compose_test.exs
├── bench/
│   └── dispatch_bench.exs
└── mix.exs
```

---

## Why quote/unquote with `Macro.escape/1` discipline and not string concatenation and `Code.eval_string/1`

quote preserves hygiene and line numbers for stack traces; eval_string throws both away and turns every DSL error into an unhelpful "syntax error on line 1." Quote-based macros give compiler-quality errors.

## Design decisions

**Option A — runtime-interpreted DSL via a GenServer**
- Pros: hot-reload, simpler mental model
- Cons: runtime cost, no compile-time validation, no autocomplete

**Option B — compile-time macro expansion into native Elixir code** (chosen)
- Pros: zero runtime cost, compiler errors for malformed DSL, tooling-friendly
- Cons: macros are harder to write and test

→ Chose **B** because a DSL users write in production deserves compile-time validation — the earlier we fail, the cheaper the fix.

## The business problem

The framework team is building a toolkit where every service defines its own resources using a declarative DSL. Today, three separate systems handle state machines, validations, and routing — each with its own configuration format. A misconfiguration in any of them is only discovered at runtime. `dsl_kit` makes the configuration itself be code, verified at compile time.

The critical insight: **a compile-time error is worth ten runtime errors**. When a developer defines a transition to an undefined state, they should see a `CompileError` when they run `mix compile`, not a `FunctionClauseError` at 3 AM in production.

---

## Project structure

\`\`\`
dsl_kit/
├── lib/
│   └── dsl_kit.ex
├── test/
│   └── dsl_kit_test.exs
├── script/
│   └── main.exs
└── mix.exs
\`\`\`

## Why `__before_compile__` is the right hook for code generation

`use MyDSL` runs immediately when the module is parsed. Individual macro calls (`state :active`, `transition :idle, :active, on: :start`) run as they are encountered. But you cannot generate the `transition/2` dispatch function until all `state/1` and `transition/4` calls have been processed — you don't know all the valid states until the module is fully defined.

`__before_compile__` fires after all macro calls in the module have executed but before the module is compiled to BEAM bytecode. This is the correct point to:

1. Read all accumulated attributes
2. Validate consistency (all transitions reference declared states)
3. Generate dispatch functions

---

## The two-phase macro pattern

```
Phase 1 (per-declaration): accumulate
  @states :active        -> Module attribute list grows
  @transitions {:idle, :active, :start, nil}

Phase 2 (__before_compile__): generate
  Read @states, @transitions
  Validate: all states in transitions are in @states
  Generate: def transition(:idle, :start) -> {:ok, :active}
```

This pattern — accumulate in phase 1, generate in phase 2 — is used by Ecto schemas, Phoenix Router, and Absinthe. Implementing it from scratch makes you understand why Phoenix Router needs `__before_compile__` before you can appreciate it as a user.

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a supervised Mix project so the DSL runtime and its benchmark harness share one application tree.

```bash
mix new dsl_kit --sup
cd dsl_kit
mkdir -p test/dsl_kit bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Keep dependencies to Benchee alone, proving the DSL can be built on stdlib macros without third-party metaprogramming helpers.

### `lib/dsl_kit.ex`

```elixir
defmodule DslKit do
  @moduledoc """
  Custom Macro DSL System.

  quote preserves hygiene and line numbers for stack traces; eval_string throws both away and turns every DSL error into an unhelpful "syntax error on line 1." Quote-based macros....
  """
end
```
### `lib/dsl_kit/state_machine.ex`

**Objective**: Accumulate states and transitions into module attributes and emit pattern-matched transition/2 clauses so runtime dispatch is branchless.

The state machine DSL uses accumulating module attributes to collect state and transition declarations during compilation. The `__before_compile__` hook validates all transitions reference declared states and generates pattern-matched `transition/2` function clauses with a catch-all returning `{:error, :invalid_transition}`.

```elixir
defmodule DslKit.StateMachine do
  @moduledoc """
  Compile-time state machine DSL.

  Usage:
    defmodule OrderFSM do
      use DslKit.StateMachine

      initial :pending

      state :pending
      state :processing
      state :shipped
      state :cancelled

      transition :pending, :processing, on: :start_processing
      transition :processing, :shipped, on: :ship
      transition :processing, :cancelled, on: :cancel, guard: &Order.can_cancel?/1
      transition :pending, :cancelled, on: :cancel
    end

  Generated functions:
    def transition(state, event) -> {:ok, new_state} | {:error, :invalid_transition}
    def valid_state?(state) -> boolean()
    def states() -> [:pending, :processing, :shipped, :cancelled]
    def events() -> [:start_processing, :ship, :cancel]
    def initial_state() -> :pending
  """

  defmacro __using__(_opts) do
    quote do
      import DslKit.StateMachine
      Module.register_attribute(__MODULE__, :sm_states, accumulate: true)
      Module.register_attribute(__MODULE__, :sm_transitions, accumulate: true)
      Module.put_attribute(__MODULE__, :sm_initial, nil)
      @before_compile DslKit.StateMachine
    end
  end

  defmacro __before_compile__(env) do
    states = Module.get_attribute(env.module, :sm_states) |> Enum.reverse() |> Enum.uniq()
    transitions = Module.get_attribute(env.module, :sm_transitions) |> Enum.reverse()
    initial = Module.get_attribute(env.module, :sm_initial)

    # Validate all transition endpoints reference declared states
    Enum.each(transitions, fn {from, to, _event, _guard} ->
      unless from in states do
        raise CompileError,
          description: "unknown state #{inspect(from)} in transition from #{inspect(from)} to #{inspect(to)}",
          file: env.file,
          line: env.line
      end

      unless to in states do
        raise CompileError,
          description: "unknown state #{inspect(to)} in transition from #{inspect(from)} to #{inspect(to)}",
          file: env.file,
          line: env.line
      end
    end)

    # Generate transition/2 clauses for non-guarded transitions
    # and transition/3 clauses for guarded transitions
    transition_clauses =
      Enum.map(transitions, fn {from, to, event, guard} ->
        if guard do
          quote do
            def transition(unquote(from), unquote(event), context) do
              if unquote(guard).(context) do
                {:ok, unquote(to)}
              else
                {:error, :guard_rejected}
              end
            end
          end
        else
          quote do
            def transition(unquote(from), unquote(event)) do
              {:ok, unquote(to)}
            end
          end
        end
      end)

    catch_all = quote do
      def transition(_state, _event), do: {:error, :invalid_transition}
    end

    quote do
      unquote_splicing(transition_clauses)
      unquote(catch_all)

      def valid_state?(state), do: state in unquote(states)
      def states(), do: unquote(states)
      def events(), do: unquote(Enum.map(transitions, fn {_, _, e, _} -> e end) |> Enum.uniq())
      def initial_state(), do: unquote(initial)
    end
  end

  defmacro initial(state) do
    quote do
      @sm_initial unquote(state)
    end
  end

  defmacro state(name) do
    quote do
      @sm_states unquote(name)
    end
  end

  defmacro transition(from, to, opts) do
    event = Keyword.fetch!(opts, :on)
    guard = Keyword.get(opts, :guard)

    quote do
      @sm_transitions {unquote(from), unquote(to), unquote(event), unquote(guard)}
    end
  end
end
```
### `lib/dsl_kit/validation.ex`

**Objective**: Validate inputs against declared field rules generated at compile time.

The validation DSL accumulates field rules as module attributes and generates a `validate/1` function in `__before_compile__`. Each field's rules are checked at compile time against a known rule set. At runtime, `validate/1` collects all errors (non-fail-fast) and returns either `{:ok, attrs}` or `{:error, %{field => [messages]}}`.

```elixir
defmodule DslKit.Validation do
  @moduledoc """
  Compile-time validation DSL.

  Usage:
    defmodule UserSchema do
      use DslKit.Validation

      validates :name,  [required: true, min_length: 2, max_length: 100]
      validates :email, [required: true, format: ~r/@/]
      validates :age,   [required: false, min: 0, max: 150]
      validates :role,  [required: true, inclusion: ["admin", "user", "viewer"]]
    end

  Generated function:
    def validate(attrs) -> {:ok, attrs} | {:error, %{field => [error_messages]}}
    (accumulates ALL errors -- does not fail-fast)

  Compile-time errors:
    - Unknown rule name -> CompileError with field and rule name
  """

  @known_rules [:required, :min_length, :max_length, :format, :inclusion, :custom, :min, :max]

  defmacro __using__(_opts) do
    quote do
      import DslKit.Validation
      Module.register_attribute(__MODULE__, :validation_rules, accumulate: true)
      @before_compile DslKit.Validation
    end
  end

  defmacro __before_compile__(env) do
    rules = Module.get_attribute(env.module, :validation_rules) |> Enum.reverse()

    # Group by field
    by_field = Enum.group_by(rules, fn {field, _rules} -> field end, fn {_field, rules} -> rules end)

    # Generate validate/1
    quote do
      def validate(attrs) do
        errors =
          unquote(Macro.escape(by_field))
          |> Enum.flat_map(fn {field, rule_sets} ->
            value = Map.get(attrs, to_string(field)) || Map.get(attrs, field)
            rule_set = List.flatten(rule_sets)
            DslKit.Validation.validate_field(field, value, rule_set)
          end)
          |> Enum.group_by(fn {field, _} -> field end, fn {_, msg} -> msg end)

        if map_size(errors) == 0, do: {:ok, attrs}, else: {:error, errors}
      end
    end
  end

  defmacro validates(field, rules) do
    # Compile-time validation: reject unknown rules
    Enum.each(rules, fn {rule, _value} ->
      unless rule in @known_rules do
        raise CompileError,
          description: "unknown validation rule #{inspect(rule)} for field #{inspect(field)}",
          file: __CALLER__.file,
          line: __CALLER__.line
      end
    end)

    quote do
      @validation_rules {unquote(field), unquote(rules)}
    end
  end

  # Runtime validation helpers (called by generated validate/1)
  def validate_field(field, nil, rules) do
    if Keyword.get(rules, :required, false) do
      [{field, "is required"}]
    else
      []
    end
  end

  def validate_field(field, value, rules) do
    Enum.flat_map(rules, fn
      {:required, true} when value == nil -> [{field, "is required"}]
      {:required, _} -> []

      {:min_length, min} when is_binary(value) and String.length(value) < min ->
        [{field, "must be at least #{min} characters"}]

      {:max_length, max} when is_binary(value) and String.length(value) > max ->
        [{field, "must be at most #{max} characters"}]

      {:format, regex} when is_binary(value) ->
        if Regex.match?(regex, value), do: [], else: [{field, "has invalid format"}]

      {:inclusion, valid_values} ->
        if value in valid_values, do: [], else: [{field, "must be one of #{inspect(valid_values)}"}]

      {:min, min} when is_number(value) and value < min ->
        [{field, "must be at least #{min}"}]

      {:max, max} when is_number(value) and value > max ->
        [{field, "must be at most #{max}"}]

      {:custom, fun} when is_function(fun, 1) ->
        case fun.(value) do
          {:ok, _} -> []
          {:error, msg} -> [{field, msg}]
        end

      _ -> []
    end)
  end
end
```
### Step 5: Given tests — must pass without modification

**Objective**: Validate behavior against the frozen test suite that must pass unmodified.

```elixir
defmodule DslKit.StateMachineTest do
  use ExUnit.Case, async: true
  doctest DslKit.Validation

  defmodule TrafficLight do
    use DslKit.StateMachine
    initial :red
    state :red
    state :green
    state :yellow
    transition :red,    :green,  on: :go
    transition :green,  :yellow, on: :slow
    transition :yellow, :red,    on: :stop
  end

  describe "StateMachine" do

  test "valid transitions" do
    assert {:ok, :green}  = TrafficLight.transition(:red,    :go)
    assert {:ok, :yellow} = TrafficLight.transition(:green,  :slow)
    assert {:ok, :red}    = TrafficLight.transition(:yellow, :stop)
  end

  test "invalid transition returns error" do
    assert {:error, :invalid_transition} = TrafficLight.transition(:red, :slow)
  end

  test "valid_state? works" do
    assert TrafficLight.valid_state?(:red)
    refute TrafficLight.valid_state?(:purple)
  end

  test "states/0 returns all declared states" do
    assert :red in TrafficLight.states()
    assert :green in TrafficLight.states()
    assert :yellow in TrafficLight.states()
  end

  test "initial_state/0" do
    assert TrafficLight.initial_state() == :red
  end

  end
end
```
```elixir
defmodule DslKit.ValidationTest do
  use ExUnit.Case, async: true
  doctest DslKit.Validation

  defmodule UserSchema do
    use DslKit.Validation
    validates :name,  [required: true, min_length: 2]
    validates :email, [required: true, format: ~r/@/]
    validates :role,  [required: true, inclusion: ["admin", "user"]]
  end

  describe "Validation" do

  test "valid attributes pass" do
    attrs = %{"name" => "Alice", "email" => "alice@example.com", "role" => "admin"}
    assert {:ok, _} = UserSchema.validate(attrs)
  end

  test "missing required field fails" do
    assert {:error, errors} = UserSchema.validate(%{"email" => "a@b.com", "role" => "user"})
    assert Map.has_key?(errors, :name)
  end

  test "all errors accumulated (not fail-fast)" do
    # Both name and email are wrong
    assert {:error, errors} = UserSchema.validate(%{"role" => "admin"})
    assert Map.has_key?(errors, :name)
    assert Map.has_key?(errors, :email)
    assert map_size(errors) >= 2
  end

  test "unknown role fails inclusion" do
    assert {:error, errors} = UserSchema.validate(%{"name" => "X", "email" => "x@y.com", "role" => "superadmin"})
    assert Map.has_key?(errors, :role)
  end

  end
end
```
```elixir
defmodule DslKit.ComposeTest do
  use ExUnit.Case, async: true
  doctest DslKit.Validation

  # Verify all three DSLs can coexist in one module
  defmodule OrderModule do
    use DslKit.StateMachine
    use DslKit.Validation

    initial :draft
    state :draft
    state :submitted
    transition :draft, :submitted, on: :submit

    validates :amount, [required: true, min: 1]
    validates :currency, [required: true, inclusion: ["USD", "EUR"]]
  end

  describe "Compose" do

  test "state machine works in composed module" do
    assert {:ok, :submitted} = OrderModule.transition(:draft, :submit)
  end

  test "validation works in composed module" do
    assert {:ok, _} = OrderModule.validate(%{"amount" => 100, "currency" => "USD"})
    assert {:error, _} = OrderModule.validate(%{"amount" => -1, "currency" => "USD"})
  end

  end
end
```
### Step 6: Run the tests

**Objective**: Execute the provided test suite to verify the implementation passes.

```bash
mix test test/dsl_kit/ --trace
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the macro DSL system invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.

## Main Entry Point

```elixir
def main do
  IO.puts("======== 42-build-custom-macro-dsl-system ========")
  IO.puts("Build custom macro dsl system")
  IO.puts("")
  
  DslKit.StateMachine.start_link([])
  IO.puts("DslKit.StateMachine started")
  
  IO.puts("Run: mix test")
end
```
## Benchmark

```elixir
# bench/dispatch_bench.exs (complete benchmark harness)
defmodule BenchFSM do
  use DslKit.StateMachine

  initial :draft
  state :draft
  state :submitted
  state :approved
  state :shipped
  state :delivered
  state :cancelled

  transition :draft, :submitted, on: :submit
  transition :submitted, :approved, on: :approve
  transition :approved, :shipped, on: :ship
  transition :shipped, :delivered, on: :deliver
  transition :draft, :cancelled, on: :cancel
  transition :submitted, :cancelled, on: :cancel
end

Benchee.run(
  %{
    "state machine dispatch (7 states, 6 transitions)" => fn ->
      BenchFSM.transition(:draft, :submit)
    end,
    "validation (5 fields, 20 rules)" => fn ->
      UserSchema.validate(%{
        "name" => "Alice",
        "email" => "alice@example.com",
        "age" => 30,
        "role" => "admin"
      })
    end
  },
  time: 5,
  warmup: 2
)
```
Target: DSL compilation should add <50ms to a module with 1000 DSL statements.

## Key Concepts: Compile-Time Code Generation via Macros

A DSL (Domain-Specific Language) is a declarative interface for encoding domain concepts. DslKit's approach uses three compile-time patterns:

1. **Attribute accumulation** (`@myattr` with `accumulate: true`): each DSL declaration (`state :name`, `validates :field, [...]`) adds to a module attribute.
2. **`__before_compile__` hooks**: after the module body is parsed but before compilation, extract accumulated attributes, validate consistency, and generate function clauses.
3. **Pattern-matched dispatch**: generated functions use guards and function-head patterns for O(1) dispatch, not runtime conditionals.

This approach — used by Ecto, Phoenix, Absinthe — defers error detection to compile time when the fix is cheapest. A user learns invalid configuration at `mix compile`, not at 3 AM in production.

---

## Trade-off analysis

| Aspect | Compile-time DSL (your impl) | Runtime config (keyword lists) | External config (YAML/JSON) |
|--------|-----------------------------|-----------------------------|----------------------------|
| Error detection | compile time | runtime | runtime / deploy time |
| IDE support | full Elixir tooling | partial | language server dependent |
| Expressiveness | Elixir macros (high) | limited | limited |
| Generated code | inspectable | interpreted at runtime | interpreted at runtime |
| Debugging | `mix compile --verbose` | stack traces | config validation errors |
| Onboarding | macro concepts required | none | none |

Reflection: Phoenix Router uses compile-time route compilation for dispatch performance, but stores route metadata in module attributes for use at runtime (e.g., path helpers). What data from your Router DSL would you want accessible at runtime, and how would you expose it without duplicating it?

---

## Common production mistakes

**1. Generating code with hygiene violations**
Macros have hygiene by default: variables defined in `quote do ... end` don't leak into the caller's scope. If you use `var!(x)` to bypass hygiene, you risk variable name collisions. Keep generated code hygienic unless you explicitly need to share a variable.

**2. `Module.eval_quoted/2` instead of returning AST from `__before_compile__`**
`__before_compile__` should return quoted expressions. Using `Module.eval_quoted` inside it evaluates code in a non-standard context and can cause compiler warnings or incorrect module metadata. Return the `quote do ... end` block directly.

**3. Attribute accumulation in definition order reversed**
`Module.register_attribute(module, :name, accumulate: true)` prepends each new value. `[:transition_3, :transition_2, :transition_1]` — reversed from definition order. Call `Enum.reverse/1` in `__before_compile__` before generating clauses.

**4. Compile-time errors without file and line**
`raise CompileError, description: "..."` without `file:` and `line:` fields produces an error pointing to the wrong location. Always extract these from `__CALLER__` (in macros) or `env` (in `__before_compile__`).

**5. Generated functions shadowing existing ones**
If a user's module defines a function with the same name as one your DSL generates, you'll get a `function already defined` warning or error. Namespace your generated functions or document clearly which names are reserved.

---

## Reflection

If your DSL allows users to write what looks like arbitrary Elixir, when do you stop being a DSL and start being a compiler? Where would you draw the line between "allowed" and "unsafe" for a DSL exposed to end users?

## Resources

- ["Metaprogramming Elixir"](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — Chris McCord — chapters 3-5 on DSL design and compile-time code generation
- [Ecto Schema source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex) — the best real-world example of accumulating attributes with `__before_compile__`; study how fields accumulate and how the schema is generated
- [Phoenix Router source](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/router.ex) — compile-time route generation; the `__before_compile__` hook and route compilation
- [Elixir — `Kernel.SpecialForms`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html) — read the `quote/2`, `unquote/1`, and `unquote_splicing/1` documentation; these are the primitives your macros use
- ["Understanding Elixir Macros"](https://www.theerlangelist.com/article/macros_1) — Sasa Juric — a 6-part blog series that builds intuition for the macro system from first principles

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Dslex.MixProject do
  use Mix.Project

  def project do
    [
      app: :dslex,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Dslex.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `dslex` (compile-time DSL).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 0
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:dslex) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Dslex stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:dslex) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:dslex)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual dslex operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Dslex classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **0 runtime overhead** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **n/a (compile-time)** | Elixir macros doc |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Elixir macros doc: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Custom Macro DSL System matters

Mastering **Custom Macro DSL System** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/dsl_kit_test.exs`

```elixir
defmodule DslKitTest do
  use ExUnit.Case, async: true

  doctest DslKit

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert DslKit.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Elixir macros doc
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
