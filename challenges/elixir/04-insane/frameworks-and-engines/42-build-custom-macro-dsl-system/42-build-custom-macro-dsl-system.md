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

### Step 2: `mix.exs`

**Objective**: Keep dependencies to Benchee alone, proving the DSL can be built on stdlib macros without third-party metaprogramming helpers.


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

### Step 3: `lib/dsl_kit/state_machine.ex`

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

### Step 4: `lib/dsl_kit/validation.ex`

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
# test/dsl_kit/state_machine_test.exs
defmodule DslKit.StateMachineTest do
  use ExUnit.Case, async: true

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
# test/dsl_kit/validation_test.exs
defmodule DslKit.ValidationTest do
  use ExUnit.Case, async: true

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
# test/dsl_kit/compose_test.exs
defmodule DslKit.ComposeTest do
  use ExUnit.Case, async: true

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

## Benchmark

```elixir
# Minimal timing harness — replace with Benchee for production measurement.
{time_us, _result} = :timer.tc(fn ->
  # exercise the hot path N times
  for _ <- 1..10_000, do: :ok
end)

IO.puts("average: #{time_us / 10_000} µs per op")
def main do
  IO.puts("[DslKit.StateMachineTest] GenServer demo")
  :ok
end

```

Target: DSL compilation should add <50ms to a module with 1000 DSL statements.

## Key Concepts: Event Sourcing and Immutable Logs

Event sourcing inverts the traditional database model: instead of storing current state, store every state-changing event in an immutable log. The current state is derived by replaying events from the start.

This shift has profound implications:
- **Audit trail is free**: Every change is a named event with timestamp and actor.
- **Temporal queries are simple**: Replay events up to a past date to see historical state.
- **Concurrency is safe**: Events are immutable and append-only, eliminating race conditions on state mutations.
- **Testability is easier**: Given a sequence of events, the state is deterministic; no mocks needed.

The BEAM is naturally suited for this pattern. Each aggregate (e.g., Account) is a GenServer that receives commands, validates them against current state, publishes an event if valid, then applies the event to update local state. The OTP supervision tree ensures persistence across restarts; the event log (in a database) survives the entire system.

The downside: evolving schemas is hard. If you rename a field or split an event type, old events still use the old structure. Solutions include versioning (introduce `withdrew_v2` alongside `withdrew_v1`) or upcasting (projection functions that translate old events to new). Frameworks like Commanded automate this.

Another challenge: reads require replaying events, which is slow for 10-year-old aggregates with millions of events. Solution: snapshots. Periodically serialize current state; replay only events after the snapshot. This trades disk space for query speed, a worthwhile tradeoff for most systems.

**Production insight**: Event sourcing is powerful for audit-heavy systems (banking, compliance), but unnecessary overhead for simple CRUD apps. Choose event sourcing when the audit trail or temporal queries justify the implementation complexity.

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
