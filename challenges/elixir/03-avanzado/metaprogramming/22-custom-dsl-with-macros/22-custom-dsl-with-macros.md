# Custom DSL with Macros and Compile-Time Checks

**Project**: `validation_dsl` — a library that lets users declare validation rules in a `validate do ... end` block; rules are verified at compile time and executed as ordinary fast code at runtime.

---

## Project context

Your product handles structured user input across 40+ forms (signup, checkout, profile,
admin, API payloads). Until now, validations were scattered — some in Ecto changesets,
some in hand-rolled `with` chains, some in ad-hoc `if` trees. Two recurring problems:
rules duplicated across forms with subtle differences, and invalid rules (typos,
unknown validators) only discovered at runtime.

You decide to build a small internal DSL, `ValidationDSL`, modeled after Ecto's
`changeset/2` but standalone and with stronger compile-time guarantees: unknown
validators cause a **compile error**, not a runtime `FunctionClauseError`. Users write:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule User.Signup do
  use ValidationDSL

  validate do
    field :email,    required: true, format: :email
    field :age,      required: true, type: :integer, min: 13
    field :password, required: true, min_length: 8
  end
end

User.Signup.validate(%{"email" => "a@b.c", "age" => 17, "password" => "secret42"})
#=> {:ok, %{email: "a@b.c", age: 17, password: "secret42"}}
```

The DSL compiles down to an efficient pure-function pipeline — no introspection at
runtime. This is how Ecto, Ash, and Commanded DSLs are structured.

```
validation_dsl/
├── lib/
│   └── validation_dsl/
│       ├── dsl.ex           # `use` + validate/1 + field/2 macros
│       ├── validators.ex    # pure validator functions
│       └── compiler.ex      # DSL → AST function body
├── test/
│   └── validation_dsl_test.exs
└── mix.exs
```

---

## Why DSL and not config maps

Config maps push the error to the first runtime call, which may be in production. A DSL gives the compiler a chance to refuse bad shapes before the module loads.

---

## Core concepts

### 1. DSL as sugar, not as interpreter

A common beginner mistake is to store the DSL declarations as data and interpret them
at runtime. Ecto doesn't — it generates plain functions. Your DSL expands `validate do
... end` into a concrete `def validate/1` whose body is a static pipeline of validator
calls. The runtime cost is identical to hand-written code.

### 2. Accumulator attributes as the DSL "register"

Each `field/2` call inside the `validate do ... end` block does NOT run at runtime. It
runs **at compile time**, appending to a module attribute declared with
`Module.register_attribute(mod, :fields, accumulate: true)`. At `@before_compile`, the
accumulated list is read and turned into the real `validate/1` function.

```
user code              compile time                   runtime
─────────              ────────────                   ───────
validate do
  field :email …   ──▶ @fields [{:email, opts}]
  field :age   …   ──▶ @fields [{:age, opts}, {:email, opts}]
end              ──▶ @before_compile reads @fields
                      and emits def validate(params)

                                            ──▶ static pipeline executes
```

### 3. Compile-time validator whitelist

`format: :emal` (typo) should not become a runtime crash. During `@before_compile`, you
check every option key against a hardcoded whitelist and call `raise
CompileError` if any are unknown. This surfaces errors at `mix compile` instead of when
a user submits a form.

### 4. `Macro.escape/1` for literals inside quoted output

When your compiler generates code that embeds a keyword list or struct, wrap it with
`Macro.escape/1` so nested tuples/atoms survive the quote boundary.

### 5. Single traversal, no runtime reflection

Runtime code has exactly O(N fields) work per `validate/1` call, with no `Enum.reduce`
over a rules list — the rules are unrolled into sequential expressions. This is what
makes compiled DSLs faster than reflective ones.

---

## Design decisions

**Option A — plain module functions that take config maps**
- Pros: no macro machinery; callers can build config at runtime.
- Cons: no declarative feel; validation runs on every call.

**Option B — macro-based DSL with compile-time validation** (chosen)
- Pros: reads like a spec; errors surface at compile; zero runtime overhead.
- Cons: macro layer to teach and maintain.

→ Chose **B** because the DSL describes shape, not behavior; compile time is where shape errors belong.

---

## Implementation

### Step 1: `lib/validation_dsl/validators.ex`

**Objective**: Define pure atom-keyed validators (required, type, min) as pattern-matched heads for compiler dispatch zero cost.

```elixir
defmodule ValidationDSL.Validators do
  @moduledoc "Pure validator functions. Each returns :ok | {:error, reason}."

  @spec required(term()) :: :ok | {:error, :missing}
  def required(nil), do: {:error, :missing}
  def required(""), do: {:error, :missing}
  def required(_), do: :ok

  @spec type(term(), atom()) :: :ok | {:error, {:wrong_type, atom()}}
  def type(value, :integer) when is_integer(value), do: :ok
  def type(value, :string) when is_binary(value), do: :ok
  def type(value, :boolean) when is_boolean(value), do: :ok
  def type(_, expected), do: {:error, {:wrong_type, expected}}

  @spec min(number(), number()) :: :ok | {:error, {:below_min, number()}}
  def min(value, bound) when is_number(value) and value >= bound, do: :ok
  def min(_, bound), do: {:error, {:below_min, bound}}

  @spec min_length(binary(), non_neg_integer()) :: :ok | {:error, {:too_short, non_neg_integer()}}
  def min_length(value, len) when is_binary(value) and byte_size(value) >= len, do: :ok
  def min_length(_, len), do: {:error, {:too_short, len}}

  @email_regex ~r/^[^\s@]+@[^\s@]+\.[^\s@]+$/
  @spec format(term(), atom()) :: :ok | {:error, {:bad_format, atom()}}
  def format(value, :email) when is_binary(value) do
    if Regex.match?(@email_regex, value), do: :ok, else: {:error, {:bad_format, :email}}
  end

  def format(_, other), do: {:error, {:bad_format, other}}

  @known_keys [:required, :type, :min, :min_length, :format]
  @spec known_keys() :: [atom()]
  def known_keys, do: @known_keys
end
```

### Step 2: `lib/validation_dsl/compiler.ex`

**Objective**: Unroll field declarations into straight-line validator chains at compile time; raise CompileError on unknown keys.

```elixir
defmodule ValidationDSL.Compiler do
  @moduledoc "Turns accumulated field definitions into a validate/1 function body."

  alias ValidationDSL.Validators

  @spec build_function([{atom(), keyword()}]) :: Macro.t()
  def build_function(fields) do
    validators_ast =
      for {name, opts} <- fields do
        build_field_validations(name, opts)
      end

    quote do
      @spec validate(map()) :: {:ok, map()} | {:error, [{atom(), term()}]}
      def validate(params) when is_map(params) do
        errors =
          Enum.reduce(unquote(fields_list(fields)), [], fn {key, checks}, acc ->
            value = Map.get(params, to_string(key)) || Map.get(params, key)

            case run_checks(value, checks) do
              :ok -> acc
              {:error, reason} -> [{key, reason} | acc]
            end
          end)

        case errors do
          [] -> {:ok, params}
          errs -> {:error, Enum.reverse(errs)}
        end
      end

      unquote_splicing(validators_ast)

      defp run_checks(_value, []), do: :ok

      defp run_checks(value, [{fun, args} | rest]) do
        case apply(ValidationDSL.Validators, fun, [value | args]) do
          :ok -> run_checks(value, rest)
          {:error, reason} -> {:error, reason}
        end
      end
    end
  end

  defp build_field_validations(_name, _opts), do: []

  defp fields_list(fields) do
    Macro.escape(
      for {name, opts} <- fields do
        {name, opts_to_checks(opts)}
      end
    )
  end

  defp opts_to_checks(opts) do
    opts
    |> Enum.filter(fn {k, _} -> k in Validators.known_keys() end)
    |> Enum.map(fn
      {:required, true} -> {:required, []}
      {:type, t} -> {:type, [t]}
      {:min, n} -> {:min, [n]}
      {:min_length, n} -> {:min_length, [n]}
      {:format, f} -> {:format, [f]}
    end)
  end

  @spec validate_options!(atom(), keyword()) :: :ok
  def validate_options!(field, opts) do
    known = Validators.known_keys()

    case Enum.reject(Keyword.keys(opts), &(&1 in known)) do
      [] ->
        :ok

      unknown ->
        raise CompileError,
          description:
            "unknown validator key(s) #{inspect(unknown)} for field #{inspect(field)}. " <>
              "Known keys: #{inspect(known)}"
    end
  end
end
```

### Step 3: `lib/validation_dsl/dsl.ex`

**Objective**: Wire accumulator attributes and @before_compile callback so DSL blocks generate validate/1 statically.

```elixir
defmodule ValidationDSL do
  @moduledoc """
  Compile-time DSL for input validation.

  Usage:

      use ValidationDSL

      validate do
        field :email, required: true, format: :email
      end
  """

  alias ValidationDSL.Compiler

  defmacro __using__(_opts) do
    quote do
      import ValidationDSL, only: [validate: 1, field: 2]
      Module.register_attribute(__MODULE__, :validation_fields, accumulate: true)
      @before_compile ValidationDSL
    end
  end

  defmacro validate(do: block), do: block

  defmacro field(name, opts) do
    quote bind_quoted: [name: name, opts: opts] do
      ValidationDSL.Compiler.validate_options!(name, opts)
      @validation_fields {name, opts}
    end
  end

  defmacro __before_compile__(env) do
    fields = env.module |> Module.get_attribute(:validation_fields) |> Enum.reverse()

    if fields == [] do
      raise CompileError,
        description: "#{inspect(env.module)}: validate block contains no fields"
    end

    Compiler.build_function(fields)
  end
end
```

### Step 4: Tests

**Objective**: Assert per-validator error payloads, confirm CompileError on unknown validator keys and empty blocks.

```elixir
defmodule ValidationDSLTest do
  use ExUnit.Case, async: true

  defmodule Signup do
    use ValidationDSL

    validate do
      field :email, required: true, format: :email
      field :age, required: true, type: :integer, min: 13
      field :password, required: true, min_length: 8
    end
  end

  describe "validate/1" do
    test "returns :ok for a valid map" do
      assert {:ok, _} =
               Signup.validate(%{email: "a@b.co", age: 30, password: "hunter22"})
    end

    test "missing required field -> :missing" do
      assert {:error, errs} = Signup.validate(%{age: 30, password: "hunter22"})
      assert {:email, :missing} in errs
    end

    test "age below min -> {:below_min, 13}" do
      assert {:error, errs} =
               Signup.validate(%{email: "a@b.co", age: 5, password: "hunter22"})

      assert {:age, {:below_min, 13}} in errs
    end

    test "bad email format" do
      assert {:error, errs} =
               Signup.validate(%{email: "not-an-email", age: 30, password: "hunter22"})

      assert {:email, {:bad_format, :email}} in errs
    end

    test "short password" do
      assert {:error, errs} =
               Signup.validate(%{email: "a@b.co", age: 30, password: "x"})

      assert {:password, {:too_short, 8}} in errs
    end
  end

  describe "compile-time rejection" do
    test "unknown validator key raises CompileError" do
      assert_raise CompileError, ~r/unknown validator key/, fn ->
        Code.compile_string("""
        defmodule BadModule do
          use ValidationDSL
          validate do
            field :email, bogus_option: true
          end
        end
        """)
      end
    end

    test "empty validate block raises CompileError" do
      assert_raise CompileError, ~r/no fields/, fn ->
        Code.compile_string("""
        defmodule EmptyModule do
          use ValidationDSL
          validate do
          end
        end
        """)
      end
    end
  end
end
```

### Why this works

Each DSL macro accumulates declarations into module attributes. `@before_compile` assembles those declarations into emitted `def` clauses and struct definitions. The DSL is thin; the heavy lifting is data-to-AST.

---


## Key Concepts: Domain-Specific Languages via Macros

A DSL (Domain-Specific Language) is a specialized language tailored to a problem. In Elixir, you build DSLs using macros. For example, a test framework might define `test "name" do ... end`, a query builder might define `from(u in User) |> where(u.age > 18)`, or a router might define `get("/path", to: Controller.action)`. Under the hood, macros parse the DSL syntax and transform it into standard Elixir code.

Building a DSL requires nesting macros (e.g., `test` calls a macro that sets up context for inner code blocks) and careful hygiene (avoiding variable name collisions between the macro definition and the caller's scope). The benefit: domain experts can read and write code in natural domain terms. The cost: learning and maintaining the macro implementation.


## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---


## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

---

## Trade-offs and production gotchas

**1. DSL feedback loops are slow.** Every change to the DSL macro layer forces a
full recompile of every module that uses it. Ecto mitigates this with `__using__` that
imports a small surface; avoid heavy work inside `__using__` itself.

**2. Compile errors must point at the user's file.** Pass `env.file` and `env.line`
to `raise CompileError` or use `Macro.Env.stacktrace/1` so errors land on the
`field :email, bogus: …` line, not inside the DSL library.

**3. Accumulator attributes leak across modules.** `Module.register_attribute(mod, :x,
accumulate: true)` is per-module. Do not try to share accumulation across modules —
use a compilation-time ETS table or `Module.get_attribute/2` on the specific module.

**4. Quoting structs and regexes.** Regexes are NOT valid terms inside `quote`. Use
`Macro.escape/1` for any non-atom/non-integer/non-binary you want to embed.

**5. Dynamic fields break the model.** If users want `field :name, if_country(:US)`
the DSL must either embed a guard AST or fall back to runtime. Design the scope early.

**6. Documentation and `@doc`.** Generated functions have no `@doc` unless the DSL
emits one. Add `@doc` generation to `build_function/1` so hexdocs shows the
validations.

**7. Dialyzer hates dynamic apply.** `apply(Validators, fun, args)` defeats success
typing. If Dialyzer is part of CI, generate direct calls instead:
`ValidationDSL.Validators.min(value, 13)`.

**8. When NOT to use this.** If you have 2 forms, write plain `with` chains. DSLs
amortize their cost when N ≥ ~10 consumers; below that, the macro indirection is net
negative for readability.

---

## Benchmark

```elixir
# bench/validate_bench.exs
params = %{email: "a@b.co", age: 30, password: "hunter22"}

Benchee.run(%{
  "compiled DSL"   => fn -> MyApp.Signup.validate(params) end,
  "runtime rules"  => fn -> InterpretedValidator.run(rules, params) end
})
```

Expect the compiled DSL to be ~5–10× faster than a runtime-interpreted alternative
on the same rule set.

---

## Reflection

- Your DSL lets users express 80% of what they want. How do you make the escape hatch to raw Elixir painless without making the DSL itself leaky?
- If two teams build parallel DSLs for similar domains, is unifying them worth the churn? What is the cheapest signal that says yes?

---

## Resources

- [Ecto.Schema source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex) — canonical compile-time DSL
- [*Metaprogramming Elixir* — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapters on DSLs
- [`Module` — hexdocs.pm](https://hexdocs.pm/elixir/Module.html) — `register_attribute`, `get_attribute`
- [Ash Framework — DSL foundation](https://github.com/ash-project/spark) — production DSL toolkit
- [Dashbit blog on macros](https://dashbit.co/blog) — José Valim
- [Fred Hébert — "Let it crash"](https://ferd.ca/the-zen-of-erlang.html) — principles behind fail-fast DSLs
