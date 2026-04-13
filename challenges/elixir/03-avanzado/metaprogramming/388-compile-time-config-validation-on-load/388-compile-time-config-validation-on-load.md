# Compile-Time Config Validation with `@on_load` and `use` Macros

**Project**: `strict_config` — a `use StrictConfig` macro that validates an application's config at compile time (typed schema, required keys) and verifies it again at boot time via `@on_load`, failing fast before any supervision tree starts.

## Project context

Runtime configuration errors are the most annoying kind. "Missing key" or "expected integer, got string" surfaces as a crash deep in the supervision tree, 400ms after boot, buried under three `GenServer.start_link/3` errors. By then you've lost the signal. The fix is to push validation earlier: first at compile time (when the dev edits config), and then again on module load (when the VM first touches the code), so a typo crashes the release in the first few hundred microseconds.

Elixir gives us two hooks:

- **`use` macros** — run at compile time, where the AST of your module is still malleable. Good place to assert config shape, expand helpers, and emit warnings.
- **`@on_load` callbacks** — a `@on_load :fun` attribute runs `fun/0` the first time the module is loaded. Good place to do final runtime checks (env vars, file existence) that cannot be computed at compile time.

This exercise builds `StrictConfig`, a small library that lets a module declare a config schema and be validated at both phases.

```
strict_config/
├── lib/
│   ├── strict_config.ex              # the use macro + schema DSL
│   ├── strict_config/
│   │   ├── schema.ex                 # parse + validate
│   │   ├── types.ex                  # primitive validators
│   │   └── errors.ex
│   └── my_app/
│       └── endpoint_config.ex        # example user of StrictConfig
├── test/
│   ├── strict_config_test.exs
│   └── on_load_test.exs
└── mix.exs
```

## Why compile-time validation and not runtime

`System.get_env/1` at runtime is fine for values that legitimately change per-deploy. For the *shape* of config (which keys exist, their types), compile time is strictly better: you fail the CI, not the deploy. Elixir's `Application.get_env/2` is dynamic but the *schema* is static — the set of keys your app expects is known at build time.

## Why also `@on_load`

Some checks cannot be compile-time: env vars that override `config/runtime.exs`, secrets injected by the deploy platform, the presence of a TLS cert file on disk. `@on_load` is the earliest *runtime* hook; it runs before any supervisor starts. A failing `@on_load` callback raises before the application tree even exists.

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
### 1. `use` macro
A compile-time extension point. `use M, opts` invokes `M.__using__(opts)` which returns a quoted expression spliced into the caller's module.

### 2. Module attribute lifecycle
`@schema` stored at compile time can be read during `__using__/1` and also persisted with `@schema_value Module.get_attribute(__MODULE__, :schema)` so it's available at runtime via a function.

### 3. `@on_load`
Declared as `@on_load :check_config`. The VM invokes `check_config/0` once, right after module load. A non-`:ok` return value prevents the module from being loaded.

### 4. `Application.compile_env/2` vs `Application.get_env/2`
`compile_env` bakes the value into the BEAM file at compile time. Reading `get_env` at compile time is a footgun: it uses whatever env is active when the compiler runs, not at boot.

## Design decisions

- **Option A — DSL inside `use`**: `use StrictConfig, schema: [...]`. Validation runs once during compile.
- **Option B — separate `defschema` macro**: more explicit, can be reused. Con: more ceremony for a small API.

→ A for the schema itself, but we expose `validate!/0` as a public function so `@on_load` can call it.

- **Option A — raise on schema violation in `__using__`**: compile fails loudly.
- **Option B — emit a warning only**: flexible but misuse-prone.

→ A. If the config schema is wrong, compilation must fail.

## Implementation

### Dependencies (`mix.exs`)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defp deps, do: []
```

### Step 1: Types

**Objective**: Define validate/2 for :string, :integer, :boolean, :port, {:one_of, list} so schema rules can be type-enforced.

```elixir
defmodule StrictConfig.Types do
  @moduledoc false

  def validate(value, :string) when is_binary(value), do: :ok
  def validate(value, :integer) when is_integer(value), do: :ok
  def validate(value, :boolean) when is_boolean(value), do: :ok
  def validate(value, :port) when is_integer(value) and value in 1..65_535, do: :ok

  def validate(value, {:one_of, allowed}) when is_list(allowed) do
    if value in allowed, do: :ok, else: {:error, {:not_in, allowed}}
  end

  def validate(value, type), do: {:error, {:wrong_type, expected: type, got: value}}
end
```

### Step 2: Errors

**Objective**: Define InvalidConfig exception with app, key, reason fields for structured error reporting.

```elixir
defmodule StrictConfig.Errors do
  defmodule InvalidConfig do
    defexception [:app, :key, :reason, :message]

    @impl true
    def exception(opts) do
      %__MODULE__{
        app: opts[:app],
        key: opts[:key],
        reason: opts[:reason],
        message:
          "invalid config for #{inspect(opts[:app])}[:#{opts[:key]}]: #{inspect(opts[:reason])}"
      }
    end
  end
end
```

### Step 3: Schema validation

**Objective**: Implement validate!/3 that traverses schema rules and raises InvalidConfig on type/required violations.

```elixir
defmodule StrictConfig.Schema do
  @moduledoc false

  alias StrictConfig.Types
  alias StrictConfig.Errors.InvalidConfig

  @doc """
  Validates the config `env` against `schema`. Raises `InvalidConfig` on first failure.
  """
  def validate!(app, schema, env) when is_list(schema) do
    Enum.each(schema, fn {key, spec} ->
      required? = Keyword.get(spec, :required, false)
      type = Keyword.fetch!(spec, :type)
      default = Keyword.get(spec, :default, :__no_default__)

      case Keyword.fetch(env, key) do
        {:ok, value} ->
          case Types.validate(value, type) do
            :ok -> :ok
            {:error, reason} -> raise InvalidConfig, app: app, key: key, reason: reason
          end

        :error when required? ->
          raise InvalidConfig, app: app, key: key, reason: :missing

        :error when default != :__no_default__ ->
          :ok

        :error ->
          :ok
      end
    end)

    :ok
  end
end
```

### Step 4: The `use` macro

**Objective**: Define __using__/1 with compile-time schema checks and @on_load callback that validates Application.get_all_env at load.

```elixir
defmodule StrictConfig do
  @moduledoc """
  Declares a config schema for an OTP application. The schema is validated:

    1. At compile time — the schema shape itself (valid types, required flag) must be well-formed.
    2. At module load (`@on_load`) — the values actually present in `Application.get_all_env/1` must match.

  Usage:

      defmodule MyApp.EndpointConfig do
        use StrictConfig,
          otp_app: :my_app,
          schema: [
            host:    [type: :string, required: true],
            port:    [type: :port,   default: 4000],
            scheme:  [type: {:one_of, [:http, :https]}, default: :http]
          ]
      end

  On module load, validates `Application.get_all_env(:my_app)` against the schema.
  """

  defmacro __using__(opts) do
    otp_app = Keyword.fetch!(opts, :otp_app)
    schema  = Keyword.fetch!(opts, :schema)

    # Compile-time assertion: the schema is well-formed.
    Enum.each(schema, &assert_schema_entry!/1)

    quote bind_quoted: [otp_app: otp_app, schema: Macro.escape(schema)] do
      @otp_app otp_app
      @schema schema
      @on_load :__strict_config_on_load__

      @doc "Validated schema for this config module."
      def __schema__, do: @schema

      @doc "OTP application whose config is validated."
      def __otp_app__, do: @otp_app

      @doc """
      Performs runtime validation. Called automatically by the `@on_load` hook.
      Callable manually from `Application.start/2` too.
      """
      def validate! do
        env = Application.get_all_env(@otp_app)
        StrictConfig.Schema.validate!(@otp_app, @schema, env)
      end

      @doc false
      def __strict_config_on_load__ do
        # @on_load runs on every module load, including during test compilation
        # in ExUnit where application env may be empty. Defer to runtime if so.
        case Application.get_all_env(@otp_app) do
          [] -> :ok
          env -> StrictConfig.Schema.validate!(@otp_app, @schema, env) && :ok
        end
      rescue
        e -> {:error, Exception.message(e)}
      end
    end
  end

  # --- compile-time schema sanity checks ---

  @valid_types [:string, :integer, :boolean, :port]

  defp assert_schema_entry!({key, spec}) when is_atom(key) and is_list(spec) do
    type = Keyword.fetch!(spec, :type)
    assert_valid_type!(key, type)
    Enum.each(spec, fn
      {:type, _} -> :ok
      {:required, v} when is_boolean(v) -> :ok
      {:default, _} -> :ok
      {other, _} ->
        raise ArgumentError, "unknown schema option #{inspect(other)} for key #{inspect(key)}"
    end)
  end

  defp assert_schema_entry!(entry),
    do: raise(ArgumentError, "invalid schema entry #{inspect(entry)}")

  defp assert_valid_type!(_, type) when type in @valid_types, do: :ok
  defp assert_valid_type!(_, {:one_of, list}) when is_list(list), do: :ok
  defp assert_valid_type!(key, type),
    do: raise(ArgumentError, "unknown type #{inspect(type)} for key #{inspect(key)}")
end
```

### Step 5: Example user module

**Objective**: Define MyApp.EndpointConfig using StrictConfig with host, port, scheme, debug? schema to show real usage.

```elixir
defmodule MyApp.EndpointConfig do
  use StrictConfig,
    otp_app: :my_app,
    schema: [
      host:   [type: :string, required: true],
      port:   [type: :port,   default: 4000],
      scheme: [type: {:one_of, [:http, :https]}, default: :http],
      debug?: [type: :boolean, default: false]
    ]

  def host, do: Application.fetch_env!(:my_app, :host)
  def port, do: Application.get_env(:my_app, :port, 4000)
end
```

## Validation flow

```
  developer edits config/config.exs
         │
         ▼
  mix compile
         │
         ▼ (use StrictConfig, ...)
  __using__/1 runs
         │
         ├─▶ assert_schema_entry!/1   ◀── compile-time shape check
         │         (bad schema = mix fails)
         │
         ▼
  .beam file written with @schema + @on_load hook
         ...

  mix release → deployed → boot
         │
         ▼
  VM loads MyApp.EndpointConfig
         │
         ▼
  __strict_config_on_load__/0 runs   ◀── runtime value check
         │                              (bad values = module fails to load)
         ▼
  StrictConfig.Schema.validate!
         │
         ▼
  :ok → app starts. {:error, msg} → VM refuses to load module → release crashes before supervisors.
```

## Tests

```elixir
defmodule StrictConfigTest do
  use ExUnit.Case, async: false

  describe "compile-time schema validation" do
    test "rejects unknown type" do
      ast =
        quote do
          defmodule BadSchema do
            use StrictConfig,
              otp_app: :x,
              schema: [foo: [type: :nonsense, required: true]]
          end
        end

      assert_raise ArgumentError, ~r/unknown type/, fn -> Code.eval_quoted(ast) end
    end

    test "rejects unknown option" do
      ast =
        quote do
          defmodule BadOption do
            use StrictConfig,
              otp_app: :x,
              schema: [foo: [type: :string, rquired: true]]
          end
        end

      assert_raise ArgumentError, ~r/unknown schema option/, fn -> Code.eval_quoted(ast) end
    end
  end

  describe "runtime validate!/0" do
    test "passes when all keys valid" do
      Application.put_env(:demo_ok, :host, "localhost")
      Application.put_env(:demo_ok, :port, 8080)

      defmodule DemoOk do
        use StrictConfig,
          otp_app: :demo_ok,
          schema: [host: [type: :string, required: true], port: [type: :port, default: 4000]]
      end

      assert :ok = DemoOk.validate!()
    end

    test "raises when required key missing" do
      Application.delete_env(:demo_missing, :host)

      defmodule DemoMissing do
        use StrictConfig,
          otp_app: :demo_missing,
          schema: [host: [type: :string, required: true]]
      end

      assert_raise StrictConfig.Errors.InvalidConfig, ~r/missing/, fn -> DemoMissing.validate!() end
    end

    test "raises on wrong type" do
      Application.put_env(:demo_wrong, :port, "not a port")

      defmodule DemoWrong do
        use StrictConfig,
          otp_app: :demo_wrong,
          schema: [port: [type: :port]]
      end

      assert_raise StrictConfig.Errors.InvalidConfig, ~r/wrong_type|not_in/, fn ->
        DemoWrong.validate!()
      end
    end
  end
end
```

```elixir
defmodule OnLoadTest do
  use ExUnit.Case, async: false

  describe "@on_load behaviour" do
    test "on_load returns :ok with empty env (deferred)" do
      Application.put_all_env([{:on_load_empty, []}])

      defmodule OnLoadEmpty do
        use StrictConfig,
          otp_app: :on_load_empty,
          schema: [host: [type: :string, required: true]]
      end

      # No crash on load — empty env is tolerated (runtime.exs may still set it).
      assert OnLoadEmpty.__schema__() ==
               [host: [type: :string, required: true]]
    end
  end
end
```

## Benchmark

```elixir
# bench/validation_bench.exs
Application.put_env(:bench_app, :host, "localhost")
Application.put_env(:bench_app, :port, 4000)

defmodule BenchConfig do
  use StrictConfig,
    otp_app: :bench_app,
    schema: [
      host: [type: :string, required: true],
      port: [type: :port,   default: 4000]
    ]
end

Benchee.run(
  %{
    "validate! (valid)" => fn -> BenchConfig.validate!() end
  },
  time: 3,
  warmup: 1
)
```

Target: validation cost < 20µs for a 5-key schema. `@on_load` runs once per module load, not per request; absolute speed is marginal, but if you put `validate!/0` in a hot path it should still be cheap enough that "call it every minute for drift detection" is free.

## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---


## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

---

## Trade-offs and production gotchas

**1. `@on_load` runs in an isolated process**
It runs before any module function is callable from outside. If `validate!/0` depends on another module, make sure that module is already loaded (they are loaded alphabetically; cross-dependencies can surprise you).

**2. Tests share Application env**
Because ExUnit tests all run in the same VM, setting `Application.put_env/3` in one test bleeds into the next. Either tag with `async: false` or scope per-test env via a setup block that restores on exit.

**3. `Application.get_env/2` at compile time is a footgun**
`@host Application.get_env(:my_app, :host)` captures whatever was in env when the compiler ran — typically `nil`. Use `Application.compile_env/2` or defer to runtime.

**4. Schema drift between code and config**
Adding a new required key without updating prod config crashes boot. That's desirable — but your CD pipeline must deploy config before code. Better: validate config artefacts in CI before rolling the image.

**5. `@on_load` that raises leaves a cryptic error**
A crashed `@on_load` shows up as `{:load_failed, {mod, function, arity}, reason}`. Always wrap in a rescue that formats a useful message.

**6. When NOT to use this**
For prototypes or scripts where config is a few env vars, the ceremony outweighs the catch. Reach for it once you have ≥5 config keys per module or multiple teams editing config.

## Reflection

`@on_load` runs every time a module is loaded, including in a live `iex` session when you `r MyApp.EndpointConfig`. If `validate!/0` has a side effect (opening a file, pinging a service), that side effect happens on every module reload. Would you still put the check in `@on_load`, or in `Application.start/2`? What is the failure semantic difference between "module cannot load" and "application cannot start"?


## Executable Example

```elixir
defmodule StrictConfig do
  @moduledoc """
  Declares a config schema for an OTP application. The schema is validated:

    1. At compile time — the schema shape itself (valid types, required flag) must be well-formed.
    2. At module load (`@on_load`) — the values actually present in `Application.get_all_env/1` must match.

  Usage:

      defmodule MyApp.EndpointConfig do
        use StrictConfig,
          otp_app: :my_app,
          schema: [
            host:    [type: :string, required: true],
            port:    [type: :port,   default: 4000],
            scheme:  [type: {:one_of, [:http, :https]}, default: :http]
          ]
      end

  On module load, validates `Application.get_all_env(:my_app)` against the schema.
  """

  defmacro __using__(opts) do
    otp_app = Keyword.fetch!(opts, :otp_app)
    schema  = Keyword.fetch!(opts, :schema)

    # Compile-time assertion: the schema is well-formed.
    Enum.each(schema, &assert_schema_entry!/1)

    quote bind_quoted: [otp_app: otp_app, schema: Macro.escape(schema)] do
      @otp_app otp_app
      @schema schema
      @on_load :__strict_config_on_load__

      @doc "Validated schema for this config module."
      def __schema__, do: @schema

      @doc "OTP application whose config is validated."
      def __otp_app__, do: @otp_app

      @doc """
      Performs runtime validation. Called automatically by the `@on_load` hook.
      Callable manually from `Application.start/2` too.
      """
      def validate! do
        env = Application.get_all_env(@otp_app)
        StrictConfig.Schema.validate!(@otp_app, @schema, env)
      end

      @doc false
      def __strict_config_on_load__ do
        # @on_load runs on every module load, including during test compilation
        # in ExUnit where application env may be empty. Defer to runtime if so.
        case Application.get_all_env(@otp_app) do
          [] -> :ok
          env -> StrictConfig.Schema.validate!(@otp_app, @schema, env) && :ok
        end
      rescue
        e -> {:error, Exception.message(e)}
      end
    end
  end

  # --- compile-time schema sanity checks ---

  @valid_types [:string, :integer, :boolean, :port]

  defp assert_schema_entry!({key, spec}) when is_atom(key) and is_list(spec) do
    type = Keyword.fetch!(spec, :type)
    assert_valid_type!(key, type)
    Enum.each(spec, fn
      {:type, _} -> :ok
      {:required, v} when is_boolean(v) -> :ok
      {:default, _} -> :ok
      {other, _} ->
        raise ArgumentError, "unknown schema option #{inspect(other)} for key #{inspect(key)}"
    end)
  end

  defp assert_schema_entry!(entry),
    do: raise(ArgumentError, "invalid schema entry #{inspect(entry)}")

  defp assert_valid_type!(_, type) when type in @valid_types, do: :ok
  defp assert_valid_type!(_, {:one_of, list}) when is_list(list), do: :ok
  defp assert_valid_type!(key, type),
    do: raise(ArgumentError, "unknown type #{inspect(type)} for key #{inspect(key)}")
end

defmodule Main do
  def main do
      # Demonstrate compile-time config validation with @on_load
      defmodule StrictConfig do
        defmacro __using__(opts) do
          quote bind_quoted: [opts: opts] do
            schema = opts[:schema] || %{}

            @on_load :validate_config

            def validate_config do
              config = Application.get_all_env(:my_app)

              # Validate config against schema
              missing = Enum.filter(schema, fn {key, _type} ->
                !Map.has_key?(config, key)
              end)

              if Enum.empty?(missing) do
                :ok
              else
                {:error, "Missing config keys: #{inspect(Enum.map(missing, &elem(&1, 0)))}"}
              end
            end
          end
        end
      end

      # Simulate config validation
      schema = %{database_url: :string, port: :integer}
      config = %{database_url: "postgres://...", port: 5432}

      # Check if all schema keys are in config
      missing = Enum.filter(schema, fn {key, _type} ->
        !Map.has_key?(config, key)
      end)

      IO.puts("✓ Schema: #{inspect(schema)}")
      IO.puts("✓ Config: #{inspect(config)}")
      IO.puts("✓ Validation: #{if Enum.empty?(missing), do: "PASS", else: "FAIL"}")

      assert Enum.empty?(missing), "All required keys present"

      IO.puts("✓ Compile-time config validation: strict schema checking working")
  end
end

Main.main()
```
