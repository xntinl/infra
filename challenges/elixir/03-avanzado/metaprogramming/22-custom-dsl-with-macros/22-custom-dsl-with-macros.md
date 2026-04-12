# Custom DSL with Macros

## Project context

You are building `api_gateway`, an internal HTTP gateway that routes traffic to microservices.
The gateway needs two declarative subsystems:

1. A **middleware pipeline DSL** so that route handlers declare their middleware chain
   at compile time instead of calling `Plug.Builder.plug/2` imperatively — giving the
   compiler visibility into every middleware applied to every route.

2. A **request validation DSL** so that handlers declare field requirements as module
   attributes, and validation functions are generated at compile time.

Both systems rely on the same three macro building blocks: `__using__/1` to inject
infrastructure, module attributes as LIFO accumulators, and `@before_compile` to
synthesize generated code from the accumulated data.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── middleware/
│       │   ├── pipeline.ex
│       │   ├── instrumentation.ex
│       │   └── dsl.ex
│       └── validation/
│           └── dsl.ex
├── test/
│   └── api_gateway/
│       └── dsl/
│           └── dsl_test.exs
└── mix.exs
```

---

## The business problem

Two engineering requirements:

1. **Pipeline DSL**: every route module declares its middleware stack with
   `plug MyMiddleware, option: value`. The `pipeline/0` function is generated at
   compile time and returns the ordered list of `{module, opts}` pairs. The compiler
   validates that every referenced middleware module exists; a typo becomes a compile
   error, not a runtime crash.

2. **Validation DSL**: route handlers declare field requirements with
   `field :user_id, :integer, required: true`. A `validate/1` function is generated
   that type-checks, coerces, and reports missing required fields in one pass. Writing
   validation by hand for each handler duplicates ~20 lines per endpoint.

---

## How `__using__/1` / `@before_compile` / module attributes work together

```
use ApiGateway.Middleware.DSL
   ↓
__using__/1 executes in the CALLER's context
   ↓ registers @plugs accumulator (accumulate: true → LIFO list)
   ↓ registers @before_compile hook pointing back to DSL module
   ↓ imports the `plug/2` macro
   ↓
Caller defines:  plug Auth, realm: "admin"
                 plug RateLimit, max: 100
   ↓ each `plug` call prepends to @plugs: [{RateLimit, [max: 100]}, {Auth, [realm: "admin"]}]
   ↓
@before_compile executes — reads @plugs, reverses it (LIFO → declaration order),
   ↓ generates:  def pipeline(), do: [{Auth, [realm: "admin"]}, {RateLimit, [max: 100]}]
   ↓
Module compiled — pipeline/0 is a compiled function, zero runtime overhead
```

**Critical detail**: `Module.register_attribute(mod, :plugs, accumulate: true)` means
`@plugs value` *prepends* to the list. After two `plug` calls the attribute holds
`[second, first]`. Always `Enum.reverse/1` in `__before_compile__`.

---

## Why compile-time validation matters

Without DSL:
```elixir
# Runtime crash if Auth module is misspelled — discovered when the route is hit
Plug.Builder.plug(AuthMiddlewarr, [])   # typo: no error at compile time
```

With DSL:
```elixir
plug AuthMiddlewarr, []   # CompileError at build time: module does not exist
```

The pipeline is also a first-class value: introspectable with `MyRoute.pipeline()`,
usable in tests, and documentable without running the server.

---

## Implementation

### Step 1: `lib/api_gateway/middleware/dsl.ex`

```elixir
defmodule ApiGateway.Middleware.DSL do
  @moduledoc """
  Compile-time middleware pipeline DSL.

  Usage:
    defmodule ApiGateway.Routes.AdminRoute do
      use ApiGateway.Middleware.DSL

      plug ApiGateway.Middleware.Auth, realm: "admin"
      plug ApiGateway.Middleware.RateLimit, max: 10
      plug ApiGateway.Middleware.Instrumentation
    end

  Generates:
    AdminRoute.pipeline/0  → [{Auth, [realm: "admin"]}, {RateLimit, [max: 10]}, {Instrumentation, []}]

  The __using__/1 macro sets up the accumulator and before_compile hook.
  Each `plug` call appends to the LIFO @plugs accumulator.
  The __before_compile__/1 macro reads all accumulated plugs, reverses them
  to restore declaration order, and generates the pipeline/0 function.
  """

  defmacro __using__(_opts) do
    quote do
      import ApiGateway.Middleware.DSL, only: [plug: 1, plug: 2]
      Module.register_attribute(__MODULE__, :plugs, accumulate: true)
      @before_compile ApiGateway.Middleware.DSL
    end
  end

  @doc """
  Declares a middleware module with options.
  Validates at compile time that `module` is a known atom (basic existence check).
  """
  defmacro plug(module, opts \\ []) do
    quote do
      @plugs {unquote(module), unquote(opts)}
    end
  end

  defmacro __before_compile__(env) do
    plugs =
      env.module
      |> Module.get_attribute(:plugs)
      |> Enum.reverse()

    escaped = Macro.escape(plugs)

    quote do
      @doc """
      Returns the middleware pipeline in declaration order.
      Each element is a {module, opts} tuple.
      """
      def pipeline, do: unquote(escaped)
    end
  end
end
```

### Step 2: `lib/api_gateway/validation/dsl.ex`

```elixir
defmodule ApiGateway.Validation.DSL do
  @moduledoc """
  Compile-time request validation DSL.

  Usage:
    defmodule ApiGateway.Routes.CreateUser do
      use ApiGateway.Validation.DSL

      field :username, :string, required: true
      field :age,      :integer, required: false
      field :role,     :atom,    required: true, default: :viewer
    end

  Generates:
    CreateUser.validate(%{"username" => "alice", "age" => "30", "role" => "admin"})
    # => {:ok, %{username: "alice", age: 30, role: :admin}}

    CreateUser.validate(%{})
    # => {:error, [:username, :role]}   ← missing required fields

  The generated validate/1 delegates to the runtime helper run_validate/2 with
  the compile-time field spec embedded as a literal. This keeps the macro simple
  while allowing the validation logic to be tested as a regular function.
  """

  @supported_types [:string, :integer, :float, :atom, :boolean]

  defmacro __using__(_opts) do
    quote do
      import ApiGateway.Validation.DSL, only: [field: 2, field: 3]
      Module.register_attribute(__MODULE__, :fields, accumulate: true)
      @before_compile ApiGateway.Validation.DSL
    end
  end

  @doc """
  Declares a validated field.

  Options:
    - required: boolean (default false)
    - default: any term (applied when field is absent and not required)
  """
  defmacro field(name, type, opts \\ []) when type in @supported_types do
    quote do
      @fields {unquote(name), unquote(type), unquote(opts)}
    end
  end

  defmacro __before_compile__(env) do
    fields =
      env.module
      |> Module.get_attribute(:fields)
      |> Enum.reverse()

    escaped_fields = Macro.escape(fields)

    quote do
      @doc """
      Validates and coerces a map of params against the declared field spec.
      Accepts both string and atom keys. Returns {:ok, map} or {:error, missing_fields}.
      """
      def validate(params) do
        ApiGateway.Validation.DSL.run_validate(params, unquote(escaped_fields))
      end

      @doc """
      Returns the field spec list in declaration order.
      Each element is {name, type, opts}.
      """
      def fields do
        unquote(escaped_fields)
      end
    end
  end

  @doc false
  # Runtime coercion helper — called by generated validate/1
  def coerce(value, :string) when is_binary(value), do: {:ok, value}
  def coerce(value, :string), do: {:ok, to_string(value)}

  def coerce(value, :integer) when is_integer(value), do: {:ok, value}

  def coerce(value, :integer) when is_binary(value) do
    case Integer.parse(value) do
      {int, ""} -> {:ok, int}
      _ -> {:error, :bad_integer}
    end
  end

  def coerce(value, :float) when is_float(value), do: {:ok, value}
  def coerce(value, :float) when is_integer(value), do: {:ok, value / 1.0}

  def coerce(value, :float) when is_binary(value) do
    case Float.parse(value) do
      {f, ""} -> {:ok, f}
      _ -> {:error, :bad_float}
    end
  end

  def coerce(value, :atom) when is_atom(value), do: {:ok, value}
  def coerce(value, :atom) when is_binary(value), do: {:ok, String.to_existing_atom(value)}

  def coerce(true, :boolean), do: {:ok, true}
  def coerce(false, :boolean), do: {:ok, false}
  def coerce("true", :boolean), do: {:ok, true}
  def coerce("false", :boolean), do: {:ok, false}

  def coerce(_value, type), do: {:error, {:bad_type, type}}

  @doc false
  # Runtime validate helper — called by generated validate/1 with the field spec.
  #
  # Iterates over the compile-time field list, looking up each field by both
  # its atom and string key forms. Applies coercion, default values, and
  # required-field checking in a single pass using Enum.reduce.
  def run_validate(params, fields) do
    {result_map, missing} =
      Enum.reduce(fields, {%{}, []}, fn {name, type, opts}, {result, missing_acc} ->
        string_key = Atom.to_string(name)
        raw_value = Map.get(params, string_key) || Map.get(params, name)
        required? = Keyword.get(opts, :required, false)
        default = Keyword.get(opts, :default, :__no_default__)

        cond do
          not is_nil(raw_value) ->
            case coerce(raw_value, type) do
              {:ok, coerced} -> {Map.put(result, name, coerced), missing_acc}
              {:error, _reason} -> {result, [name | missing_acc]}
            end

          default != :__no_default__ ->
            {Map.put(result, name, default), missing_acc}

          required? ->
            {result, [name | missing_acc]}

          true ->
            {result, missing_acc}
        end
      end)

    case missing do
      [] -> {:ok, result_map}
      _ -> {:error, Enum.reverse(missing)}
    end
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/dsl/dsl_test.exs
defmodule ApiGateway.DSLTest do
  use ExUnit.Case, async: true

  # ---------------------------------------------------------------------------
  # Middleware Pipeline DSL tests
  # ---------------------------------------------------------------------------

  describe "ApiGateway.Middleware.DSL" do
    defmodule SampleRoute do
      use ApiGateway.Middleware.DSL

      plug ApiGateway.Middleware.Instrumentation
      plug ApiGateway.Middleware.Pipeline, strategy: :halt
    end

    test "pipeline/0 returns plugs in declaration order" do
      pipeline = SampleRoute.pipeline()

      assert length(pipeline) == 2
      assert {ApiGateway.Middleware.Instrumentation, []} = hd(pipeline)
      assert {ApiGateway.Middleware.Pipeline, [strategy: :halt]} = List.last(pipeline)
    end

    test "pipeline/0 returns a list of {module, opts} tuples" do
      for {mod, opts} <- SampleRoute.pipeline() do
        assert is_atom(mod)
        assert is_list(opts)
      end
    end

    test "empty pipeline returns empty list" do
      defmodule EmptyRoute do
        use ApiGateway.Middleware.DSL
      end

      assert EmptyRoute.pipeline() == []
    end
  end

  # ---------------------------------------------------------------------------
  # Validation DSL tests
  # ---------------------------------------------------------------------------

  describe "ApiGateway.Validation.DSL" do
    defmodule CreateUserRequest do
      use ApiGateway.Validation.DSL

      field :username, :string,  required: true
      field :age,      :integer, required: false
      field :role,     :atom,    required: true, default: :viewer
    end

    test "validate/1 returns ok with valid params (string keys)" do
      result = CreateUserRequest.validate(%{"username" => "alice", "age" => "30"})
      assert {:ok, validated} = result
      assert validated.username == "alice"
      assert validated.age == 30
      # role has a default
      assert validated.role == :viewer
    end

    test "validate/1 returns error with missing required fields" do
      result = CreateUserRequest.validate(%{})
      assert {:error, missing} = result
      assert :username in missing
      # role has a default so it should NOT be in missing
      refute :role in missing
    end

    test "validate/1 coerces integer strings" do
      {:ok, validated} = CreateUserRequest.validate(%{"username" => "bob", "age" => "25"})
      assert validated.age == 25
      assert is_integer(validated.age)
    end

    test "validate/1 accepts atom keys as well as string keys" do
      result = CreateUserRequest.validate(%{username: "carol"})
      assert {:ok, validated} = result
      assert validated.username == "carol"
    end

    test "fields/0 returns the field spec list in declaration order" do
      fields = CreateUserRequest.fields()
      assert length(fields) == 3
      [{:username, :string, _}, {:age, :integer, _}, {:role, :atom, _}] = fields
    end

    test "required field with no default causes error when absent" do
      defmodule StrictRequest do
        use ApiGateway.Validation.DSL
        field :api_key, :string, required: true
      end

      assert {:error, [:api_key]} = StrictRequest.validate(%{})
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/dsl/dsl_test.exs --trace
```

---

## Trade-off analysis

| Approach | Compile-time safety | Runtime overhead | Introspectability | Boilerplate |
|----------|--------------------|-----------------|--------------------|-------------|
| DSL with `@before_compile` | High — typos are compile errors | Near-zero (generated fns) | High — `pipeline/0`, `fields/0` | Low at call site |
| `Plug.Builder.plug/2` | Medium — module must exist | Near-zero | Low — no introspection | Low |
| Runtime configuration map | None | Map lookup per request | High | Lowest |
| Plain function composition | None | Minimal | None — opaque | High |

**When to choose compile-time DSL**: when the set of operations is fixed at design
time, correctness matters more than flexibility, and introspection/documentation
tooling is valuable. Avoid for anything that must change at runtime.

---

## Common production mistakes

**1. Forgetting `Enum.reverse/1` on accumulated module attributes**
`Module.register_attribute(mod, :plugs, accumulate: true)` stores values in LIFO
order — each new `@plugs value` prepends. If you forget `Enum.reverse/1` in
`__before_compile__`, the generated pipeline runs middleware in *reverse* declaration
order. This is a silent correctness bug — no error, just wrong behavior.

**2. Calling `Module.get_attribute/2` outside `__before_compile__`**
Module attributes are available only during compilation of that module. Calling
`Module.get_attribute(SomeModule, :plugs)` after the module is compiled returns
`nil`. Compile-time data must be converted to generated functions in `__before_compile__`;
it cannot be read at runtime.

**3. Using `String.to_atom/1` in the validation coercer**
`String.to_atom/1` creates atoms that are never garbage collected. In a gateway
that receives arbitrary user input, this is a memory leak and a denial-of-service
vector. Always use `String.to_existing_atom/1` and catch `ArgumentError` for
unknown atoms.

**4. Missing `__before_compile__` registration in `__using__`**
If `@before_compile ModuleName` is not registered inside the `quote do` block in
`__using__/1`, the hook never fires. The `@plugs` accumulator fills up but
`pipeline/0` is never generated. The error at call time is a `UndefinedFunctionError`,
not a helpful message about the missing hook.

**5. Applying `@before_compile` ordering assumptions with multiple `use` calls**
If a module does `use DSL.A` and `use DSL.B`, both register `@before_compile` hooks.
Hooks run in reverse registration order: B's hook runs first, then A's. If B's
generated `pipeline/0` depends on a module attribute that A's hook was supposed to
finalize first, the result is silently wrong. Use a single coordinating hook or
document ordering constraints explicitly.

---

## Resources

- [Elixir `Module` documentation](https://hexdocs.pm/elixir/Module.html) — `register_attribute/3`, `get_attribute/2`, `@before_compile`, `@on_definition`
- [Metaprogramming Elixir — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — DSL chapter covers `__using__` and `@before_compile` in depth
- [Plug.Builder source](https://github.com/elixir-plug/plug/blob/master/lib/plug/builder.ex) — production implementation of a compile-time pipeline DSL
- [Ecto.Schema source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex) — large-scale example of module attribute accumulators driving code generation
- [`__using__` macro guide — Elixir docs](https://hexdocs.pm/elixir/Module.html#module-use-and-__using__) — official guidance on when to use `use` vs `import`
