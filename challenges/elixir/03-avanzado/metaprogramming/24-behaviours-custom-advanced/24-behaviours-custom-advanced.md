# Advanced Behaviours with Compile-Time Validation

## Project context

You are building `api_gateway`, an internal HTTP gateway that routes traffic to microservices.
The gateway supports pluggable middleware components,
and teams contribute new middleware constantly. Without a formal contract, middleware
modules vary in their function signatures, miss required callbacks, or skip optional
lifecycle hooks — causing runtime crashes that only appear when a specific request
hits the missing code path.

The solution is a formal `ApiGateway.Middleware.Behaviour` that:
1. Defines required and optional callbacks with full typespecs
2. Validates at compile time (via `@before_compile`) that every required callback
   is implemented — catching missing implementations at `mix compile`, not in prod
3. Provides default implementations for optional callbacks via `__using__/1`

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── middleware/
│           └── behaviour.ex
├── test/
│   └── api_gateway/
│       └── middleware/
│           └── behaviour_test.exs
└── mix.exs
```

The `ApiGateway.Conn` struct used by the behaviour is defined as:

```elixir
defmodule ApiGateway.Conn do
  @moduledoc "Represents an in-flight HTTP connection through the gateway."
  defstruct [:method, :path, :status, :remote_ip, :assigns]
end
```

---

## The business problem

Two requirements:

1. **Compile-time contract enforcement**: if a developer creates a middleware module
   and forgets to implement `call/2`, the build must fail with a clear message
   (`"Module Foo.Bar claims to be a middleware but does not implement call/2"`).
   A runtime crash on the first request is unacceptable.

2. **Optional lifecycle hooks**: middleware can optionally implement `init/1`
   (called once at startup to validate options), `on_error/3` (called when an
   exception escapes), and `telemetry_prefix/0` (returns the telemetry event
   prefix). Middleware that doesn't implement these gets sensible defaults:
   `init/1` returns its argument unchanged, `on_error/3` re-raises.

---

## Behaviours vs Protocols — when to use which

| | Behaviour | Protocol |
|---|-----------|---------|
| Polymorphism over | Module identity | Value type |
| Dispatch | Explicit — `Module.function()` | Implicit — `Protocol.function(value)` |
| Compile-time check | Via `@before_compile` or Dialyzer | Via `Protocol.impl_for!/1` in tests |
| Default impl | Yes — via `__using__/1` | Yes — via `for: Any` |
| Use case | Plugins, adapters, drivers | Data transformation, formatting |

Middleware is a *module-level* contract: the pipeline calls `Module.call(conn, opts)`.
This is a behaviour, not a protocol — no value dispatch is needed.

---

## `@optional_callbacks` — how they work

```elixir
@optional_callbacks [init: 1, on_error: 3, telemetry_prefix: 0]
@callback init(opts :: keyword()) :: keyword()
@callback on_error(conn :: Conn.t(), error :: Exception.t(), stacktrace :: list()) :: Conn.t()
@callback telemetry_prefix() :: [atom()]
```

`@optional_callbacks` tells the compiler not to emit a warning when a module
that `@behaviour MyBehaviour` does not implement these callbacks. The behaviour
module itself can then check `function_exported?(mod, :init, 1)` at runtime to
decide whether to call the optional implementation or use the default.

---

## `@macrocallback` — when the implementation must be a macro

```elixir
@macrocallback transform_opts(opts :: Macro.t()) :: Macro.t()
```

`@macrocallback` is used when the implementing module must provide a macro, not a
function. This is rare — it's used by DSLs (like `Ecto.Schema`) where the callback
must inject quoted code into the caller's module. For the middleware behaviour, all
callbacks are regular functions.

---

## Implementation

### Step 1: `lib/api_gateway/middleware/behaviour.ex`

```elixir
defmodule ApiGateway.Middleware.Behaviour do
  @moduledoc """
  Behaviour contract for all ApiGateway middleware modules.

  Required callbacks:
    - call/2: processes a connection; must be implemented by every middleware

  Optional callbacks (have defaults via __using__/1):
    - init/1: validates options at startup; default returns opts unchanged
    - on_error/3: handles escaped exceptions; default re-raises
    - telemetry_prefix/0: prefix for :telemetry events; default uses module name

  Usage:
    defmodule ApiGateway.Middleware.Auth do
      use ApiGateway.Middleware.Behaviour

      @impl true
      def call(conn, opts) do
        # ... auth logic
        conn
      end

      @impl true
      def init(opts) do
        Keyword.validate!(opts, [:realm, :required])
      end
    end

  The __using__/1 macro injects three things into the implementing module:
  1. @behaviour declaration (enables compiler callback checking)
  2. Default implementations for all optional callbacks (overridable)
  3. @before_compile hook that validates call/2 is explicitly defined
  """

  alias ApiGateway.Conn

  # ── Required callbacks ──────────────────────────────────────────────────────

  @doc """
  Processes an in-flight connection.
  Must return the (possibly modified) conn.
  """
  @callback call(conn :: Conn.t(), opts :: keyword()) :: Conn.t()

  # ── Optional callbacks ──────────────────────────────────────────────────────

  @optional_callbacks [init: 1, on_error: 3, telemetry_prefix: 0]

  @doc """
  Called once when the middleware is initialized (at pipeline build time).
  Use to validate options and raise ArgumentError for bad configuration.
  Default: returns opts unchanged.
  """
  @callback init(opts :: keyword()) :: keyword()

  @doc """
  Called when an exception escapes from call/2.
  Must return a conn (to send an error response) or re-raise.
  Default: re-raises the exception with the original stacktrace.
  """
  @callback on_error(conn :: Conn.t(), error :: Exception.t(), stacktrace :: list()) :: Conn.t()

  @doc """
  Returns the :telemetry event name prefix for this middleware.
  Default: [:api_gateway, :middleware, <module_name_snake_case>]
  """
  @callback telemetry_prefix() :: [atom()]

  # ── __using__/1 — injects defaults and registers compile-time validation ────

  defmacro __using__(_opts) do
    quote do
      @behaviour ApiGateway.Middleware.Behaviour

      # Default implementations for optional callbacks.
      # Implementing modules may override these with @impl true.

      @impl ApiGateway.Middleware.Behaviour
      def init(opts), do: opts

      @impl ApiGateway.Middleware.Behaviour
      def on_error(_conn, error, stacktrace), do: reraise(error, stacktrace)

      @impl ApiGateway.Middleware.Behaviour
      def telemetry_prefix do
        # Derive the telemetry prefix from the module name.
        # Takes the last component of the module name (e.g., "Auth" from
        # "ApiGateway.Middleware.Auth"), converts it to snake_case, and
        # builds the standard telemetry prefix path.
        last_part =
          __MODULE__
          |> Module.split()
          |> List.last()
          |> Macro.underscore()
          |> String.to_atom()

        [:api_gateway, :middleware, last_part]
      end

      # Allow implementing modules to override the defaults
      defoverridable [init: 1, on_error: 3, telemetry_prefix: 0]

      # Register compile-time validation
      @before_compile ApiGateway.Middleware.Behaviour
    end
  end

  # ── Compile-time validation ──────────────────────────────────────────────────

  defmacro __before_compile__(env) do
    module = env.module

    # Module.defines?/3 checks if the function is explicitly defined in this module.
    # At this point the module body has been fully evaluated, so all `def` declarations
    # are visible. If call/2 is not defined, the module cannot function as middleware.
    unless Module.defines?(module, {:call, 2}, :def) do
      raise CompileError,
        file: env.file,
        line: env.line,
        description:
          "#{inspect(module)} uses ApiGateway.Middleware.Behaviour " <>
          "but does not implement the required callback call/2"
    end

    :ok
  end

  # ── Runtime helpers for the pipeline ────────────────────────────────────────

  @doc """
  Calls init/1 on `module` if it exports the function, otherwise returns opts unchanged.
  Used by the pipeline builder to initialize each middleware.
  """
  @spec maybe_init(module(), keyword()) :: keyword()
  def maybe_init(module, opts) do
    if function_exported?(module, :init, 1) do
      module.init(opts)
    else
      opts
    end
  end

  @doc """
  Calls on_error/3 on `module` if it exports the function,
  otherwise re-raises the error.
  """
  @spec maybe_on_error(module(), Conn.t(), Exception.t(), list()) :: Conn.t()
  def maybe_on_error(module, conn, error, stacktrace) do
    if function_exported?(module, :on_error, 3) do
      module.on_error(conn, error, stacktrace)
    else
      reraise(error, stacktrace)
    end
  end

  @doc """
  Returns true if `module` is a valid middleware (implements the behaviour).
  Uses behaviour_info/1 introspection.

  First checks Code.ensure_loaded?/1 to avoid errors for non-existent modules,
  then inspects the module's :behaviour attribute list to verify this specific
  behaviour is declared.
  """
  @spec middleware?(module()) :: boolean()
  def middleware?(module) do
    Code.ensure_loaded?(module) &&
      module.__info__(:attributes)
      |> Keyword.get_values(:behaviour)
      |> List.flatten()
      |> Enum.member?(__MODULE__)
  end
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/behaviour_test.exs
defmodule ApiGateway.Middleware.BehaviourTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Middleware.Behaviour, as: MWBehaviour

  # ---------------------------------------------------------------------------
  # A correct middleware implementation
  # ---------------------------------------------------------------------------

  defmodule ValidMiddleware do
    use ApiGateway.Middleware.Behaviour

    @impl true
    def call(conn, _opts), do: Map.put(conn, :valid_mw, true)
  end

  # A middleware with all optional callbacks overridden
  defmodule FullMiddleware do
    use ApiGateway.Middleware.Behaviour

    @impl true
    def call(conn, _opts), do: Map.put(conn, :full_mw, true)

    @impl true
    def init(opts), do: Keyword.put(opts, :initialized, true)

    @impl true
    def on_error(_conn, _error, _stacktrace), do: %{status: 500, error: true}

    @impl true
    def telemetry_prefix(), do: [:api_gateway, :middleware, :full]
  end

  # ---------------------------------------------------------------------------
  # Tests
  # ---------------------------------------------------------------------------

  describe "required callback enforcement" do
    test "module implementing call/2 compiles without error" do
      # ValidMiddleware was defined above — if it compiled, this passes
      assert function_exported?(ValidMiddleware, :call, 2)
    end

    test "compile-time error for missing call/2" do
      assert_raise CompileError, ~r/call\/2/, fn ->
        defmodule MissingCallMiddleware do
          use ApiGateway.Middleware.Behaviour
          # Intentionally no call/2
        end
      end
    end
  end

  describe "default implementations" do
    test "init/1 default returns opts unchanged" do
      opts = [realm: "admin", timeout: 5_000]
      assert ValidMiddleware.init(opts) == opts
    end

    test "telemetry_prefix/0 default returns a list of atoms" do
      prefix = ValidMiddleware.telemetry_prefix()
      assert is_list(prefix)
      assert Enum.all?(prefix, &is_atom/1)
      assert length(prefix) >= 2
    end
  end

  describe "optional callback overrides" do
    test "init/1 override is called instead of default" do
      opts = [foo: :bar]
      result = FullMiddleware.init(opts)
      assert result[:initialized] == true
      assert result[:foo] == :bar
    end

    test "telemetry_prefix/0 override returns custom prefix" do
      assert FullMiddleware.telemetry_prefix() == [:api_gateway, :middleware, :full]
    end

    test "on_error/3 override returns a map instead of re-raising" do
      result = FullMiddleware.on_error(%{}, %RuntimeError{message: "test"}, [])
      assert result.error == true
    end
  end

  describe "maybe_init/2 and maybe_on_error/4" do
    test "maybe_init/2 calls init when exported" do
      opts = MWBehaviour.maybe_init(FullMiddleware, [])
      assert opts[:initialized] == true
    end

    test "maybe_init/2 returns opts unchanged when init not exported" do
      # ValidMiddleware uses the default init (defoverridable + re-defined as default)
      # But the default is exported — use a plain module without use
      defmodule NoInitModule do
        @behaviour ApiGateway.Middleware.Behaviour
        def call(conn, _), do: conn
      end

      opts = [key: :value]
      assert MWBehaviour.maybe_init(NoInitModule, opts) == opts
    end
  end

  describe "middleware?/1" do
    test "returns true for a module using the behaviour" do
      assert MWBehaviour.middleware?(ValidMiddleware) == true
    end

    test "returns false for a plain module" do
      defmodule PlainModule do
        def hello, do: :world
      end

      assert MWBehaviour.middleware?(PlainModule) == false
    end

    test "returns false for non-existent module" do
      assert MWBehaviour.middleware?(NonExistent.Module.XYZ) == false
    end
  end

  describe "behaviour_info introspection" do
    test "required callbacks includes call/2" do
      all_callbacks = MWBehaviour.behaviour_info(:callbacks)
      assert {:call, 2} in all_callbacks
    end

    test "optional callbacks includes init/1, on_error/3, telemetry_prefix/0" do
      optional = MWBehaviour.behaviour_info(:optional_callbacks)
      assert {:init, 1} in optional
      assert {:on_error, 3} in optional
      assert {:telemetry_prefix, 0} in optional
    end

    test "call/2 is NOT in optional callbacks" do
      optional = MWBehaviour.behaviour_info(:optional_callbacks)
      refute {:call, 2} in optional
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/api_gateway/middleware/behaviour_test.exs --trace
```

---

## Trade-off analysis

| Approach | Compile-time safety | Runtime overhead | Default impls | Polymorphism |
|----------|--------------------|-----------------|--------------|----|
| `@behaviour` + `@before_compile` check | High — missing callbacks = compile error | None | Via `__using__` + `defoverridable` | Module-level only |
| `@behaviour` only (no extra check) | Medium — Dialyzer warns, compiler does not | None | Via `__using__` + `defoverridable` | Module-level only |
| Protocol | Low (unless `impl_for!/1` in tests) | O(1) consolidated | Via `for: Any` | Value-type dispatch |
| Plain function contract (docs only) | None | None | N/A | None |

**When `@before_compile` validation is worth it**: when the behaviour is widely
implemented by many teams (internal or external contributors) and a missing
callback causes a hard crash rather than a graceful error. Gateway middleware is
a perfect fit: `call/2` is load-bearing; forgetting it means every request crashes.

---

## Common production mistakes

**1. Not using `defoverridable` before providing defaults**
If you define a default `def init(opts)` in the `__using__` quote block without
`defoverridable [init: 1]`, implementing modules that define their own `init/1`
get a compile warning about redefining a function. Always call `defoverridable`
before providing defaults.

**2. Checking `function_exported?/3` for `@optional_callbacks` in the wrong place**
`function_exported?(mod, :init, 1)` returns `true` even when the implementing
module uses the default `init/1` injected by `__using__`. If you want to know
whether the implementing module *explicitly overrode* the callback, check
`Module.defines?(mod, {:init, 1}, :def)` at compile time, or compare the function
body — but this is rarely needed in practice.

**3. Using `@macrocallback` when you mean `@callback`**
`@macrocallback` requires the implementing module to define a `defmacro`, not a
`def`. Mixing the two raises a confusing error. Use `@macrocallback` only when
the callback must inject quoted code into the caller at compile time — almost
never the case for middleware.

**4. Skipping `Code.ensure_loaded?/1` before `behaviour_info/1`**
Calling `SomeModule.behaviour_info(:callbacks)` on a module that doesn't exist
or hasn't been loaded raises `UndefinedFunctionError`. Always check
`Code.ensure_loaded?(mod)` first when doing runtime behaviour introspection.

**5. `@before_compile` validation running too early**
`@before_compile` runs after the module body is fully evaluated. Do NOT use
`Module.defines?/3` inside the `__using__` quote block (which runs when `use` is
called, before the function definitions). The call will return `false` for every
function. Put the validation exclusively in `__before_compile__/1`.

---

## Resources

- [Elixir `@behaviour` documentation](https://hexdocs.pm/elixir/Module.html#module-behaviour) — `@callback`, `@optional_callbacks`, `behaviour_info/1`
- [GenServer source — Elixir stdlib](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/gen_server.ex) — production example of `__using__`, `defoverridable`, and `@before_compile` validation
- [Plug behaviour](https://github.com/elixir-plug/plug/blob/master/lib/plug.ex) — real-world middleware behaviour with `init/1` and `call/2`
- [Dialyzer and behaviours](https://hexdocs.pm/mix/Mix.Tasks.Dialyzer.html) — Dialyzer catches missing callbacks without `@before_compile`, but only if you run it
- [Elixir guide: Behaviours](https://elixir-lang.org/getting-started/typespecs-and-behaviours.html) — official intro to the behaviour system
