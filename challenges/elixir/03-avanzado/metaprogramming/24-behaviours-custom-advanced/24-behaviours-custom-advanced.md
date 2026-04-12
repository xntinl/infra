# Advanced Behaviours with Optional Callbacks and Runtime Checks

**Project**: `behaviours_advanced` — a plugin system that defines a behaviour with required, optional, and versioned callbacks, enforces them at compile time, and performs runtime `ensure_loaded?` checks for hot-reloaded plugins.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

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
└── mix.exs
```

---

## Core concepts

### 1. `@callback` vs `@optional_callbacks`

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

## Implementation

### Step 1: `lib/behaviours_advanced/notifier.ex`

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

### Step 2: `lib/behaviours_advanced/plugins/slack.ex`

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

### Step 3: `lib/behaviours_advanced/plugins/sms.ex`

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

### Step 4: `lib/behaviours_advanced/plugins/partial.ex`

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

### Step 5: `lib/behaviours_advanced/registry.ex`

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
    e -> {:error, Exception.message(e)}
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

### Step 6: Tests

```elixir
defmodule BehavioursAdvanced.NotifierTest do
  use ExUnit.Case, async: true

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

## Resources

- [`Module` — hexdocs.pm](https://hexdocs.pm/elixir/Module.html) — `@callback`, `@optional_callbacks`
- [`Behaviour` discussion in Elixir docs](https://hexdocs.pm/elixir/typespecs.html#behaviours)
- [Plug behaviour source](https://github.com/elixir-plug/plug/blob/main/lib/plug.ex) — canonical
- [Phoenix.Channel behaviour](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/channel.ex) — real optional callbacks
- [Code.ensure_loaded — Elixir docs](https://hexdocs.pm/elixir/Code.html#ensure_loaded/1)
- [José Valim on behaviours](https://dashbit.co/blog) — Dashbit
- [Erlang docs — behaviours](https://www.erlang.org/doc/design_principles/des_princ.html#behaviours)
