# Advanced Behaviours with Optional Callbacks and Runtime Checks

**Project**: `behaviours_advanced` — a plugin system that defines a behaviour with required, optional, and versioned callbacks, enforces them at compile time, and performs runtime `ensure_loaded?` checks for hot-reloaded plugins.

---

## The business problem

You run a multi-tenant notification service with a plugin architecture. Third parties
drop in modules that implement `Notifier` (Slack, SMS, Email, Push, Webhook). Each
plugin must:

- Implement `send_notification/2` — required
- Optionally implement `healthcheck/0` — if absent, the supervisor skips it
- Optionally implement `format_preview/1` — used by the admin UI
- Export a `version/0` that matches a minimum

A misconfigured plugin today (missing required callback, wrong arity) crashes at boot.
You want compile-time feedback plus runtime tolerance when optional callbacks are
absent. This is what OTP, Plug, and Phoenix Channels do — their behaviours
mix `@callback` and `@optional_callbacks`, and the host uses
`function_exported?/3` before calling an optional one.

## Project structure

```
behaviours_advanced/
├── lib/
│   └── behaviours_advanced/
│       ├── notifier.ex              # the behaviour contract
│       ├── registry.ex              # loads + validates plugins at runtime
│       ├── plugins/
│       │   ├── slack.ex             # full impl
│       │   ├── sms.ex               # full impl
│       │   └── partial.ex           # missing optional callbacks
│       └── application.ex
├── test/
│   └── notifier_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why behaviours and not duck typing

Duck typing works until someone forgets a callback and the error appears in production. `@callback` plus `@behaviour` lets the compiler warn at the implementer's module, at compile time, before anyone runs a test.

---

## Design decisions

**Option A — define a module that callers duck-type against**
- Pros: no ceremony; callers just implement the right functions.
- Cons: no compile-time check, no documentation, no Dialyzer help.

**Option B — explicit `@callback` + optional callbacks** (chosen)
- Pros: compile-time warnings, introspection via `behaviour_info/1`, Dialyzer coverage.
- Cons: one more module to maintain; requires discipline to keep `@spec`s accurate.

→ Chose **B** because contracts deserve names; duck typing loses the compiler's help for nothing.

---

## Implementation

### `lib/behaviours_advanced.ex`

```elixir
defmodule BehavioursAdvanced do
  @moduledoc """
  Advanced Behaviours with Optional Callbacks and Runtime Checks.

  Duck typing works until someone forgets a callback and the error appears in production. `@callback` plus `@behaviour` lets the compiler warn at the implementer's module, at....
  """
end
```

### `lib/behaviours_advanced/notifier.ex`

**Objective**: Define @callback + @optional_callbacks contracts; @after_compile enforces version/0 floor at compile time.

```elixir
defmodule BehavioursAdvanced.Notifier do
  @moduledoc """
  Behaviour for notification plugins.

  Required:
    * `send_notification/2`
    * `version/0`

  Optional:
    * `healthcheck/0`
    * `format_preview/1`
  """

  @type user :: %{required(:id) => String.t(), optional(atom()) => term()}
  @type msg :: String.t()

  @callback send_notification(user(), msg()) :: :ok | {:error, term()}
  @callback version() :: Version.t() | String.t()
  @callback healthcheck() :: :ok | {:error, term()}
  @callback format_preview(msg()) :: String.t()

  @optional_callbacks [healthcheck: 0, format_preview: 1]

  @minimum_version "1.0.0"

  defmacro __using__(_opts) do
    quote do
      @behaviour BehavioursAdvanced.Notifier
      @after_compile BehavioursAdvanced.Notifier
    end
  end

  @doc false
  def __after_compile__(env, _bytecode) do
    ensure_version!(env.module)
  end

  @spec ensure_version!(module()) :: :ok
  def ensure_version!(mod) do
    case function_exported?(mod, :version, 0) do
      false ->
        raise CompileError,
          description: "#{inspect(mod)} must implement version/0"

      true ->
        v = mod.version() |> to_version()
        min = Version.parse!(@minimum_version)

        if Version.compare(v, min) == :lt do
          raise CompileError,
            description:
              "#{inspect(mod)} reports version #{v}, minimum required is #{@minimum_version}"
        end

        :ok
    end
  end

  defp to_version(%Version{} = v), do: v
  defp to_version(bin) when is_binary(bin), do: Version.parse!(bin)
end
```

### `lib/behaviours_advanced/plugins/slack.ex`

**Objective**: Implement all required and optional callbacks with @impl true annotations to define conformance baseline.

```elixir
defmodule BehavioursAdvanced.Plugins.Slack do
  use BehavioursAdvanced.Notifier

  @impl true
  def send_notification(%{id: _id}, msg) do
    # In real code: HTTP POST to Slack webhook.
    {:ok, :sent_via_slack_with: msg}
    :ok
  end

  @impl true
  def version, do: "1.2.0"

  @impl true
  def healthcheck, do: :ok

  @impl true
  def format_preview(msg), do: "[slack] " <> String.slice(msg, 0, 80)
end
```

### `lib/behaviours_advanced/plugins/sms.ex`

**Objective**: Omit format_preview/1 optional callback; enforce 160-byte message length guard on send_notification/2.

```elixir
defmodule BehavioursAdvanced.Plugins.SMS do
  use BehavioursAdvanced.Notifier

  @impl true
  def send_notification(%{id: _id}, msg) when byte_size(msg) <= 160, do: :ok
  def send_notification(_, _), do: {:error, :too_long}

  @impl true
  def version, do: "1.0.0"

  @impl true
  def healthcheck do
    case :rand.uniform(100) do
      n when n > 5 -> :ok
      _ -> {:error, :carrier_down}
    end
  end

  # deliberately omits format_preview/1 — it's optional
end
```

### `lib/behaviours_advanced/plugins/partial.ex`

**Objective**: Implement only required callbacks; prove optional ones are truly optional without compile error.

```elixir
defmodule BehavioursAdvanced.Plugins.Partial do
  @moduledoc "Minimal plugin — only implements required callbacks."
  use BehavioursAdvanced.Notifier

  @impl true
  def send_notification(_user, _msg), do: :ok

  @impl true
  def version, do: "1.0.0"
end
```

### `lib/behaviours_advanced/registry.ex`

**Objective**: Fan out to plugins via required send_notification/2; gate optional calls with function_exported?/3.

```elixir
defmodule BehavioursAdvanced.Registry do
  @moduledoc "Runtime coordinator: dispatches to plugins, gracefully handling optional callbacks."

  @spec dispatch([module()], map(), String.t()) :: %{required(module()) => :ok | {:error, term()}}
  def dispatch(plugins, user, msg) do
    plugins
    |> Enum.map(fn mod -> {mod, safe_send(mod, user, msg)} end)
    |> Map.new()
  end

  @spec healthchecks([module()]) :: %{required(module()) => :ok | {:error, term()} | :not_implemented}
  def healthchecks(plugins) do
    plugins
    |> Enum.map(fn mod -> {mod, call_optional(mod, :healthcheck, [])} end)
    |> Map.new()
  end

  @spec previews([module()], String.t()) :: %{required(module()) => String.t() | :not_implemented}
  def previews(plugins, msg) do
    plugins
    |> Enum.map(fn mod -> {mod, call_optional(mod, :format_preview, [msg])} end)
    |> Map.new()
  end

  defp safe_send(mod, user, msg) do
    mod.send_notification(user, msg)
  rescue
    e in RuntimeError -> {:error, Exception.message(e)}
  end

  defp call_optional(mod, fun, args) do
    _ = Code.ensure_loaded(mod)
    arity = length(args)

    if function_exported?(mod, fun, arity) do
      apply(mod, fun, args)
    else
      :not_implemented
    end
  end
end
```

### `test/behaviours_advanced_test.exs`

**Objective**: Assert required callbacks work, optional ones return :not_implemented safely, and version floor blocks old impls.

```elixir
defmodule BehavioursAdvanced.NotifierTest do
  use ExUnit.Case, async: true
  doctest BehavioursAdvanced.Registry

  alias BehavioursAdvanced.Plugins.{Slack, SMS, Partial}
  alias BehavioursAdvanced.Registry

  @user %{id: "u-1"}

  describe "dispatch/3 — required callback" do
    test "calls send_notification on every plugin" do
      result = Registry.dispatch([Slack, SMS, Partial], @user, "hello")
      assert result[Slack] == :ok
      assert result[SMS] == :ok
      assert result[Partial] == :ok
    end

    test "SMS rejects messages over 160 bytes" do
      long = String.duplicate("a", 200)
      result = Registry.dispatch([SMS], @user, long)
      assert result[SMS] == {:error, :too_long}
    end
  end

  describe "healthchecks/1 — optional callback" do
    test "Slack and SMS return :ok or an error" do
      result = Registry.healthchecks([Slack, SMS])
      assert result[Slack] in [:ok]
      assert result[SMS] in [:ok, {:error, :carrier_down}]
    end

    test "Partial plugin returns :not_implemented" do
      result = Registry.healthchecks([Partial])
      assert result[Partial] == :not_implemented
    end
  end

  describe "previews/2 — optional callback" do
    test "Slack formats a preview" do
      result = Registry.previews([Slack], "hi there")
      assert result[Slack] =~ "[slack]"
    end

    test "SMS and Partial return :not_implemented" do
      result = Registry.previews([SMS, Partial], "hi")
      assert result[SMS] == :not_implemented
      assert result[Partial] == :not_implemented
    end
  end

  describe "version enforcement" do
    test "plugin below minimum version fails to compile" do
      code = """
      defmodule TooOld do
        use BehavioursAdvanced.Notifier
        @impl true
        def send_notification(_, _), do: :ok
        @impl true
        def version, do: "0.9.0"
      end
      """

      assert_raise CompileError, ~r/minimum required is 1.0.0/, fn ->
        Code.compile_string(code)
      end
    end
  end
end
```

### Why this works

`@behaviour Mod` triggers the compiler to check that every `@callback` in `Mod` has a matching `def` in the implementer. `@optional_callbacks` relaxes that check per callback. The behaviour module itself carries `@callback` attributes that `behaviour_info/1` exposes at runtime.

---

## Key Concepts: Custom Behaviour Definition and Enforcement

You can define your own custom behaviour (not just using OTP's built-in ones). Define callbacks with `@callback` and optionally `@optional_callbacks`. Then modules can `@behaviour` your custom behaviour.

Example: `@callback perform(args :: any()) :: :ok | {:error, reason :: any()}` defines a callback that all implementations must provide. At compile time, Elixir checks that implementing modules provide all non-optional callbacks. This is powerful for plugin systems: define the interface, let plugins implement it, and compose them at runtime.

## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---

## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

---

## Trade-offs and production gotchas

**1. `function_exported?/3` returns false for unloaded modules.** Under releases,
modules load lazily. Always call `Code.ensure_loaded/1` first when you cannot rely on
the module having been referenced in code.

**2. `@after_compile` runs once per impl but does NOT see other impls.** Cross-module
coherence checks (e.g. "exactly one plugin exports a slug") require a registry pattern,
not `@after_compile`.

**3. `@impl true` is advisory but critical.** Without it, renaming a callback silently
passes. Enforce `warnings_as_errors: true` in CI and `@impl true` everywhere.

**4. Optional callback wrappers cost a dispatch.** The `call_optional/3` path does a
`function_exported?` check per call. For very hot paths, cache the boolean in ETS on
boot.

**5. Behaviours do NOT enforce return types.** `@callback send_notification(...) :: :ok`
is a Dialyzer contract, not a runtime check. Tests remain necessary.

**6. Versioning gotcha — Version.parse!/1 crashes on garbage.** Wrap the parse in a
more helpful error or document the constraint.

**7. `@callback` with a private type.** If the behaviour declares `@type user :: ...`,
impls do NOT inherit it — they must redeclare or reference the full name.

**8. When NOT to use optional callbacks.** If 80% of impls will override the
"optional" callback, it is not optional — just make it required and provide a default
via `defoverridable` + `__using__`.

---

## Benchmark

```elixir
# bench/behaviour_bench.exs
alias BehavioursAdvanced.Registry
alias BehavioursAdvanced.Plugins.{Slack, SMS, Partial}

Benchee.run(%{
  "dispatch — 3 plugins"   => fn -> Registry.dispatch([Slack, SMS, Partial], %{id: "u"}, "hi") end,
  "healthchecks — optional" => fn -> Registry.healthchecks([Slack, SMS, Partial]) end
})
```

Expect ~1–3 µs for `dispatch/3` per plugin and ~0.2 µs for the optional check (which
is mostly `function_exported?` lookup, cached in the VM after first call).

---

## Reflection

- Your behaviour has grown to 15 callbacks. Is that a signal that the contract is too big, or that the domain is irreducibly complex? How do you decide?
- If you had to version a behaviour without breaking existing implementers, what does that look like? What is the migration story?

---

### `script/main.exs`
```elixir
defmodule BehavioursAdvanced.NotifierTest do
  use ExUnit.Case, async: true
  doctest BehavioursAdvanced.Registry

  alias BehavioursAdvanced.Plugins.{Slack, SMS, Partial}
  alias BehavioursAdvanced.Registry

  @user %{id: "u-1"}

  describe "dispatch/3 — required callback" do
    test "calls send_notification on every plugin" do
      result = Registry.dispatch([Slack, SMS, Partial], @user, "hello")
      assert result[Slack] == :ok
      assert result[SMS] == :ok
      assert result[Partial] == :ok
    end

    test "SMS rejects messages over 160 bytes" do
      long = String.duplicate("a", 200)
      result = Registry.dispatch([SMS], @user, long)
      assert result[SMS] == {:error, :too_long}
    end
  end

  describe "healthchecks/1 — optional callback" do
    test "Slack and SMS return :ok or an error" do
      result = Registry.healthchecks([Slack, SMS])
      assert result[Slack] in [:ok]
      assert result[SMS] in [:ok, {:error, :carrier_down}]
    end

    test "Partial plugin returns :not_implemented" do
      result = Registry.healthchecks([Partial])
      assert result[Partial] == :not_implemented
    end
  end

  describe "previews/2 — optional callback" do
    test "Slack formats a preview" do
      result = Registry.previews([Slack], "hi there")
      assert result[Slack] =~ "[slack]"
    end

    test "SMS and Partial return :not_implemented" do
      result = Registry.previews([SMS, Partial], "hi")
      assert result[SMS] == :not_implemented
      assert result[Partial] == :not_implemented
    end
  end

  describe "version enforcement" do
    test "plugin below minimum version fails to compile" do
      code = """
      defmodule TooOld do
        use BehavioursAdvanced.Notifier
        @impl true
        def send_notification(_, _), do: :ok
        @impl true
        def version, do: "0.9.0"
      end
      """

      assert_raise CompileError, ~r/minimum required is 1.0.0/, fn ->
        Code.compile_string(code)
      end
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate behaviour with optional callbacks and runtime checks
      defmodule PluginBehaviour do
        @callback required_callback(term()) :: term()
        @callback optional_callback(term()) :: term() | :not_implemented

        @doc false
        def spec(callback) do
          "Plugin callback spec for #{inspect(callback)}"
        end
      end

      defmodule MyPlugin do
        @behaviour PluginBehaviour

        def required_callback(data) do
          {:processed, data}
        end

        # optional_callback not implemented, falls back to default
      end

      # Runtime check
      result = MyPlugin.required_callback("test")

      IO.inspect(result, label: "✓ Behaviour callback result")

      # Check if optional callback exists
      optional_impl = function_exported?(MyPlugin, :optional_callback, 1)
      IO.puts("✓ Optional callback implemented: #{optional_impl}")

      assert match?({:processed, "test"}, result), "Required callback works"

      IO.puts("✓ Advanced behaviours: optional callbacks and runtime checks working")
  end
end

Main.main()
```

---

## Why Advanced Behaviours with Optional Callbacks and Runtime Checks matters

Mastering **Advanced Behaviours with Optional Callbacks and Runtime Checks** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts

### 1. `@callback` vs `@optional_callbacks`

### `mix.exs`
```elixir
defmodule BehavioursCustomAdvanced.MixProject do
  use Mix.Project

  def project do
    [
      app: :behaviours_custom_advanced,
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
@callback send_notification(user :: map(), msg :: String.t()) :: :ok | {:error, term()}
@callback healthcheck() :: :ok | {:error, term()}
@callback format_preview(msg :: String.t()) :: String.t()

@optional_callbacks [healthcheck: 0, format_preview: 1]
```

The compiler will warn (via `@behaviour Notifier`) when a required callback is
missing. Optional callbacks are only checked when the host actively calls them.

### 2. Compile-time validation hook: `@after_compile`

`@after_compile` runs a function after the implementing module finishes compiling. You
receive `env` and the `bytecode`. This is where you can cross-check arities, version
numbers, or enforce additional project-specific rules.

### 3. Runtime `function_exported?/3` gate

```
if function_exported?(mod, :healthcheck, 0) do
  mod.healthcheck()
else
  :skip
end
```

`function_exported?/3` returns `false` before the module is loaded; call
`Code.ensure_loaded?(mod)` first in release mode, because modules are lazily loaded.

### 4. `@impl true` annotations

`@impl true` on a function tells the compiler "I intend this as behaviour impl". If
the behaviour changes a signature and you forget to update, you get an explicit warning
instead of silent drift. Use it on every callback implementation.

### 5. Versioned behaviours

A behaviour can evolve. Having impls declare `version/0` and the host compare against
`@minimum_version` gives you graceful compatibility. This is how Phoenix handles
`endpoint/init` signature changes across majors.

---
