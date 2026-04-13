# Ecto-Style Schema DSL from Scratch

**Project**: `schema_dsl` — build a miniature schema DSL — `schema "users" do field :name, :string ... end` — that generates a struct, cast/validate functions, and a `dump/1` to map. Understand how Ecto.Schema is wired.

---

## Project context

You are writing a framework that needs structured records but does not want to pull
all of Ecto. You want the familiar Ecto-like ergonomics:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule MyApp.User do
  use SchemaDSL

  schema "users" do
    field :name,    :string
    field :age,     :integer, default: 18
    field :active?, :boolean, default: true
    field :email,   :string, required: true
  end
end

MyApp.User.cast(%{"name" => "Ada", "age" => "37", "email" => "a@b.co"})
# => {:ok, %MyApp.User{name: "Ada", age: 37, email: "a@b.co", active?: true}}
```

This is a classic teaching exercise because it exercises: `__using__`, accumulator
attributes, `defstruct` generation, `@before_compile`, type-aware casting, and
compile-time error validation.

```
schema_dsl/
├── lib/
│   └── schema_dsl/
│       ├── schema.ex           # __using__ + schema/2 + field/3 macros
│       ├── types.ex            # cast/2 for supported types
│       └── casting.ex          # cast/2 driver
├── test/
│   └── schema_dsl_test.exs
└── mix.exs
```

---

## Why compile-time DSL and not runtime validation

Runtime validation needs a schema description in data form and branches at every call. A compile-time DSL converts the description into a `def` clause per field with the type baked in, giving the fast path for free.

---

## Core concepts

### 1. `defstruct` generated from accumulated fields

Every `field :x, :type, default: val` appends `{:x, :type, opts}` to a module
attribute. At `@before_compile`, read the list and emit `defstruct [{:x, default}, ...]`.

### 2. Cast coerces string input to typed values

Typical consumers (HTTP controllers, CSV importers) pass all strings. `cast/1`
must coerce:

```
"37"   -> 37        # :integer
"true" -> true      # :boolean
"2024-01-01" -> ~D[2024-01-01] # :date
```

Per-type cast functions keep the code open to extension.

### 3. Validation: required fields

The DSL records `required: true` in the field options. `cast/1` checks presence
after coercion and emits `{:error, [{:email, :missing}, ...]}` if needed.

### 4. `source` table and `__schema__/1`

Ecto exposes metadata via `User.__schema__(:source)`, `User.__schema__(:fields)`.
Implement the same introspection: callers can list fields, their types, etc.

### 5. Keep types pluggable

Hardcoding `:string | :integer | :boolean | :date` is fine for a teaching project
but brittle. Wrap casting in `Types.cast(type, value)` so a user can later add
`:decimal`, `:uuid`, etc. via a `@callback`.

---

## Design decisions

**Option A — plain struct + hand-written validation**
- Pros: zero macros; trivially readable.
- Cons: scales poorly across tens of schemas; validation drifts from shape.

**Option B — compile-time DSL that emits struct + validators** (chosen)
- Pros: single declaration becomes struct, spec, validators, and docs; mirrors Ecto.Schema.
- Cons: macro layer to maintain; debugging requires reading expanded code.

→ Chose **B** because the app has many schemas with parallel validation rules; the DSL removes the per-schema boilerplate.

---

## Implementation

### Step 1: `lib/schema_dsl/types.ex`

**Objective**: Implement cast/2 for :string, :integer, :boolean, :float, :date so schema casting is type-aware and extensible.

```elixir
defmodule SchemaDSL.Types do
  @moduledoc "Type casting for the schema DSL."

  @type t :: :string | :integer | :boolean | :float | :date

  @spec cast(t(), term()) :: {:ok, term()} | :error
  def cast(_type, nil), do: {:ok, nil}

  def cast(:string, v) when is_binary(v), do: {:ok, v}
  def cast(:string, v) when is_atom(v), do: {:ok, Atom.to_string(v)}
  def cast(:string, v) when is_integer(v), do: {:ok, Integer.to_string(v)}

  def cast(:integer, v) when is_integer(v), do: {:ok, v}

  def cast(:integer, v) when is_binary(v) do
    case Integer.parse(v) do
      {n, ""} -> {:ok, n}
      _ -> :error
    end
  end

  def cast(:float, v) when is_float(v), do: {:ok, v}
  def cast(:float, v) when is_integer(v), do: {:ok, v * 1.0}

  def cast(:float, v) when is_binary(v) do
    case Float.parse(v) do
      {f, ""} -> {:ok, f}
      _ -> :error
    end
  end

  def cast(:boolean, v) when is_boolean(v), do: {:ok, v}
  def cast(:boolean, "true"), do: {:ok, true}
  def cast(:boolean, "false"), do: {:ok, false}

  def cast(:date, %Date{} = d), do: {:ok, d}

  def cast(:date, v) when is_binary(v) do
    case Date.from_iso8601(v) do
      {:ok, d} -> {:ok, d}
      _ -> :error
    end
  end

  def cast(_type, _value), do: :error

  @spec supported?(t()) :: boolean()
  def supported?(t), do: t in [:string, :integer, :boolean, :float, :date]
end
```

### Step 2: `lib/schema_dsl/casting.ex`

**Objective**: Implement cast/3 driver that accumulates cast errors and missing-required errors in one Enum.reduce pass.

```elixir
defmodule SchemaDSL.Casting do
  @moduledoc false

  alias SchemaDSL.Types

  @type field_spec :: {atom(), Types.t(), keyword()}

  @spec cast([field_spec()], module(), map()) :: {:ok, struct()} | {:error, keyword()}
  def cast(fields, module, params) when is_map(params) do
    initial = struct(module)

    {result, errors} =
      Enum.reduce(fields, {initial, []}, fn {name, type, opts}, {acc, errs} ->
        raw = Map.get(params, Atom.to_string(name)) || Map.get(params, name)

        case {raw, Keyword.get(opts, :required, false)} do
          {nil, true} ->
            {acc, [{name, :missing} | errs]}

          {nil, false} ->
            {acc, errs}

          {value, _} ->
            case Types.cast(type, value) do
              {:ok, cast} -> {Map.put(acc, name, cast), errs}
              :error -> {acc, [{name, {:cast_failed, type}} | errs]}
            end
        end
      end)

    case errors do
      [] -> {:ok, result}
      errs -> {:error, Enum.reverse(errs)}
    end
  end
end
```

### Step 3: `lib/schema_dsl/schema.ex`

**Objective**: Implement __using__, schema/2, field/2-3 macros with @before_compile to emit defstruct, __schema__/1, cast/1, dump/1.

```elixir
defmodule SchemaDSL do
  @moduledoc """
  Ecto-style compile-time schema DSL.

  Usage:

      defmodule User do
        use SchemaDSL

        schema "users" do
          field :name, :string, required: true
          field :age, :integer, default: 0
        end
      end
  """

  alias SchemaDSL.Types

  defmacro __using__(_) do
    quote do
      import SchemaDSL, only: [schema: 2]
      Module.register_attribute(__MODULE__, :schema_fields, accumulate: true)
    end
  end

  defmacro schema(source, do: block) do
    quote do
      @schema_source unquote(source)
      import SchemaDSL, only: [field: 2, field: 3]
      unquote(block)
      @before_compile SchemaDSL
    end
  end

  defmacro field(name, type, opts \\ []) do
    quote bind_quoted: [name: name, type: type, opts: opts] do
      unless SchemaDSL.Types.supported?(type) do
        raise CompileError,
          description: "unsupported type #{inspect(type)} for field #{inspect(name)}"
      end

      @schema_fields {name, type, opts}
    end
  end

  defmacro __before_compile__(env) do
    fields = env.module |> Module.get_attribute(:schema_fields) |> Enum.reverse()
    source = Module.get_attribute(env.module, :schema_source)

    struct_defaults =
      for {name, _type, opts} <- fields do
        {name, Keyword.get(opts, :default)}
      end

    field_names = Enum.map(fields, &elem(&1, 0))

    quote do
      defstruct unquote(Macro.escape(struct_defaults))

      @spec __schema__(:source) :: String.t()
      @spec __schema__(:fields) :: [atom()]
      @spec __schema__(:specs) :: [{atom(), atom(), keyword()}]
      def __schema__(:source), do: unquote(source)
      def __schema__(:fields), do: unquote(field_names)
      def __schema__(:specs), do: unquote(Macro.escape(fields))

      @spec cast(map()) :: {:ok, struct()} | {:error, keyword()}
      def cast(params) when is_map(params) do
        SchemaDSL.Casting.cast(unquote(Macro.escape(fields)), __MODULE__, params)
      end

      @spec dump(struct()) :: map()
      def dump(%__MODULE__{} = struct) do
        Map.take(struct, unquote(field_names))
      end
    end
  end
end
```

### Step 4: Sample schema

**Objective**: Declare User schema with multiple types and defaults to demonstrate DSL usage realistically.

```elixir
defmodule SchemaDSL.Sample.User do
  use SchemaDSL

  schema "users" do
    field :name, :string, required: true
    field :age, :integer, default: 18
    field :active?, :boolean, default: true
    field :email, :string, required: true
    field :joined_on, :date
  end
end
```

### Step 5: Tests

**Objective**: Assert introspection, casts succeed/fail correctly, defaults apply, dump/1 works, compile-time rejects unsupported types.

```elixir
defmodule SchemaDSLTest do
  use ExUnit.Case, async: true

  alias SchemaDSL.Sample.User

  describe "introspection" do
    test "source is the given table name" do
      assert User.__schema__(:source) == "users"
    end

    test "fields lists declared field names" do
      assert User.__schema__(:fields) ==
               [:name, :age, :active?, :email, :joined_on]
    end
  end

  describe "cast/1 — happy path" do
    test "coerces strings to typed values" do
      assert {:ok, %User{name: "Ada", age: 37, email: "a@b.c", active?: true}} =
               User.cast(%{"name" => "Ada", "age" => "37", "email" => "a@b.c"})
    end

    test "default is used when absent" do
      assert {:ok, %User{age: 18}} =
               User.cast(%{"name" => "X", "email" => "x@y.z"})
    end

    test "accepts atom or string keys" do
      assert {:ok, %User{name: "Mix"}} =
               User.cast(%{name: "Mix", email: "m@m.m"})
    end

    test "parses ISO date" do
      assert {:ok, %User{joined_on: ~D[2024-01-15]}} =
               User.cast(%{"name" => "X", "email" => "a@b", "joined_on" => "2024-01-15"})
    end
  end

  describe "cast/1 — errors" do
    test "required field missing" do
      {:error, errs} = User.cast(%{"name" => "X"})
      assert {:email, :missing} in errs
    end

    test "bad type returns cast_failed" do
      {:error, errs} = User.cast(%{"name" => "X", "email" => "e@e", "age" => "abc"})
      assert {:age, {:cast_failed, :integer}} in errs
    end
  end

  describe "dump/1" do
    test "returns the field-only map" do
      {:ok, user} = User.cast(%{"name" => "A", "email" => "a@b"})
      dumped = User.dump(user)
      assert dumped.name == "A"
      assert Map.has_key?(dumped, :active?)
      refute Map.has_key?(dumped, :__struct__)
    end
  end

  describe "compile-time errors" do
    test "unsupported type raises" do
      assert_raise CompileError, ~r/unsupported type/, fn ->
        Code.compile_string("""
        defmodule Bad do
          use SchemaDSL
          schema "bad" do
            field :x, :uuid
          end
        end
        """)
      end
    end
  end
end
```

### Why this works

The DSL collects field declarations via `accumulate: true` attributes. `@before_compile` reads the list, emits a `defstruct`, a `__schema__/1` introspection function, and a `validate/1` function whose body is generated from the field list. Each generated clause pattern-matches against the expected type.

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

**1. `:default` values are literals only.** You cannot pass a function reference
`default: &DateTime.utc_now/0` because it gets escaped by `Macro.escape/1`. Ecto
supports `:default` callbacks via a different mechanism. Document the limitation.

**2. String vs atom keys.** Controllers receive `%{"name" => "X"}`; internal code
`%{name: "X"}`. Supporting both is required but doubles map lookups; pick one at
the boundary and document.

**3. Type plugin contract.** The current `Types` module hardcodes casts. For a real
library, define `@callback cast(term, opts) :: {:ok, term} | :error` and look up
types in a registry.

**4. Circular struct refs.** If field `:owner` references `User` (self-reference),
the struct module is not yet compiled when the schema macro runs. Use `atom`
references instead of module references where possible.

**5. Embedded structs vs associations.** This DSL only handles scalars. Extending
with `embeds_one/2` requires a recursive `cast/1` — non-trivial to get right.

**6. Error formatting.** Returning `{:error, keyword()}` is minimal. Ecto's
`Changeset.traverse_errors/2` gives nested error trees; pick the shape that fits
your consumers.

**7. `dump/1` vs JSON encode.** `dump/1` here returns a map; JSON encoding with
`Jason` needs `@derive Jason.Encoder` on the struct. Either add the derive to the
generated `defstruct` or document that users must do it themselves.

**8. When NOT to use your own DSL.** If your app already uses Ecto for DB, use
`Ecto.Schema` + `embedded_schema/1` even for non-persistent data. The payoff from
reinventing the wheel is pedagogical, not practical.

---

## Benchmark

```elixir
# bench/schema_bench.exs
alias SchemaDSL.Sample.User

params = %{"name" => "Ada", "age" => "37", "email" => "a@b.co", "active?" => "true"}

Benchee.run(%{
  "cast 4 fields" => fn -> User.cast(params) end
})
```

Expect ~4–8 µs per cast — dominated by map operations.

---

## Reflection

- Your DSL now has three builders (field, embeds, has_many). What prevents it from collapsing into a pile of special cases? Where do you draw the architectural line?
- If a user wanted to add a custom validator, would you extend the DSL or let them override a generated function? Which one preserves introspection?

---

## Resources

- [Ecto.Schema source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex) — canonical
- [Ecto.Changeset source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/changeset.ex) — cast/4
- [*Metaprogramming Elixir*, ch. 7](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — DSL study
- [Ash.Resource — DSL architecture](https://github.com/ash-project/ash)
- [`Module.register_attribute/3`](https://hexdocs.pm/elixir/Module.html#register_attribute/3)
- [Dashbit blog — on DSL design](https://dashbit.co/blog)
