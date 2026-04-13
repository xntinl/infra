# `alias`, `import`, `require`, `use` — Module Directives

**Project**: `plugin_dispatcher` — loads and invokes plugin modules by name at runtime

---

## Project structure

```
plugin_dispatcher/
├── lib/
│   └── plugin_dispatcher/
│       ├── dispatcher.ex       # uses alias + require
│       ├── plugin.ex           # behaviour + `use` macro
│       └── plugins/
│           ├── upcase.ex
│           └── reverse.ex
├── test/
│   └── plugin_dispatcher_test.exs
└── mix.exs
```

---

## What you will learn

1. **`alias`, `import`, `require`, `use`** — what each one actually does and when to reach for it.
2. **Runtime module lookup** — how to resolve a module by atom name and invoke a function on it.

---

## The four directives in 60 seconds

| Directive | What it does | Typical trigger |
|---|---|---|
| `alias Foo.Bar` | Lets you write `Bar` instead of `Foo.Bar` | Any long module path used > 1 time |
| `import Foo` | Brings `Foo`'s functions into scope (no prefix) | DSLs, test helpers, `ExUnit.Case` |
| `require Foo` | Ensures `Foo` is compiled so you can use its macros | You are about to call a macro from `Foo` |
| `use Foo` | Calls `Foo.__using__/1`, which injects code | Framework hooks: `use GenServer`, `use ExUnit.Case` |

`use` is just sugar for `require Foo; Foo.__using__(opts)`. That is the whole trick.

---

## Why a behaviour + `use` macro and not a plain module list

- Keeping a global `@plugins [Upcase, Reverse]` list works, but loses compile-time checks that every plugin implements `call/1` and `name/0`.
- A behaviour documents the contract; `use PluginDispatcher.Plugin` injects the boilerplate (`@behaviour`, default `name/0`) so plugin authors can't forget it.
- Protocols are another option, but protocols dispatch on **data type**, not module name — not the right shape here.

---

## Design decisions

**Option A — each plugin registers itself via `@on_load` or a compile-time hook**
- Pros: truly pluggable; adding a file is enough.
- Cons: hidden magic; harder to debug when a plugin doesn't show up; requires `Code.ensure_loaded/1` dance.

**Option B — static registry map in the dispatcher + behaviour enforced by `use`** (chosen)
- Pros: one canonical place lists every plugin; compiler warns if a plugin is missing a callback; tests trivially enumerate plugins.
- Cons: adding a plugin requires editing `dispatcher.ex`.

→ Chose **B** because explicit registration beats auto-discovery in small-to-medium systems. When the plugin count exceeds ~20, reconsider.

---

## Why a plugin dispatcher

A dispatcher loads plugin modules by atom name and invokes a well-known callback on each.
It is the smallest realistic example that hits all four directives:

- `alias` — shorten the `PluginDispatcher.Plugin` references.
- `import` — none needed here, but we show where it would fit.
- `require Logger` — Logger's macros won't compile without it.
- `use PluginDispatcher.Plugin` — each plugin injects boilerplate via a `__using__/1`.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"plug", "~> 1.0"},
  ]
end
```


### Dependencies (`mix.exs`)

```elixir
defp deps do
  # No runtime deps — stdlib only. Logger ships with Elixir.
  []
end
```

### Step 1 — Create the project

**Objective**: Split the dispatcher, behaviour, and plugins into distinct files so `alias`, `require`, and `use` each have a natural place to appear.

```bash
mix new plugin_dispatcher
cd plugin_dispatcher
mkdir -p lib/plugin_dispatcher/plugins
```

### Step 2 — `lib/plugin_dispatcher/plugin.ex`

**Objective**: Combine `@callback` and a `use` macro so every plugin is forced to declare `@behaviour`, making missing callbacks a compile-time error.

```elixir
defmodule PluginDispatcher.Plugin do
  @moduledoc """
  Behaviour every plugin must implement, plus a `use` macro that wires it up.
  """

  @callback name() :: atom()
  @callback call(input :: String.t()) :: String.t()

  @doc """
  `use PluginDispatcher.Plugin` expands to:
    - @behaviour PluginDispatcher.Plugin
    - default name/0 derived from the module's last segment
    - the module registered on compile, so the dispatcher can discover it
  """
  defmacro __using__(_opts) do
    quote do
      @behaviour PluginDispatcher.Plugin

      # Derive a default name from the module alias — Plugins.Upcase -> :upcase.
      # Modules override this if they want a different registered name.
      @impl true
      def name do
        __MODULE__
        |> Module.split()
        |> List.last()
        |> String.downcase()
        |> String.to_atom()
      end

      defoverridable name: 0
    end
  end
end
```

### Step 3 — `lib/plugin_dispatcher/plugins/upcase.ex`

**Objective**: Provide a concrete plugin that demonstrates `use PluginDispatcher.Plugin` wiring up the behaviour contract automatically.

```elixir
defmodule PluginDispatcher.Plugins.Upcase do
  # `use` triggers Plugin.__using__/1 — injects @behaviour and name/0.
  use PluginDispatcher.Plugin

  @impl true
  def call(input) when is_binary(input), do: String.upcase(input)
end
```

### Step 4 — `lib/plugin_dispatcher/plugins/reverse.ex`

**Objective**: Add a second plugin to show how `alias` in the dispatcher resolves multiple implementations without hardcoding their full module paths.

```elixir
defmodule PluginDispatcher.Plugins.Reverse do
  use PluginDispatcher.Plugin

  @impl true
  def call(input) when is_binary(input), do: String.reverse(input)
end
```

### Step 5 — `lib/plugin_dispatcher/dispatcher.ex`

**Objective**: Resolve plugins at runtime via atom dispatch so new plugins register declaratively without editing the core dispatcher.

```elixir
defmodule PluginDispatcher.Dispatcher do
  @moduledoc """
  Resolves a plugin by atom name and invokes its `call/1` callback.
  """

  # `alias` — avoids writing PluginDispatcher.Plugins.X repeatedly.
  alias PluginDispatcher.Plugins.{Upcase, Reverse}

  # `require Logger` — Logger.info/1 is a macro; without `require`, this fails to compile.
  require Logger

  @registry %{
    upcase: Upcase,
    reverse: Reverse
  }

  @spec dispatch(atom(), String.t()) :: {:ok, String.t()} | {:error, :unknown_plugin}
  def dispatch(plugin_name, input) do
    case Map.fetch(@registry, plugin_name) do
      {:ok, module} ->
        Logger.info("dispatching plugin #{plugin_name}")
        # Runtime dispatch: call the function on the resolved module.
        {:ok, module.call(input)}

      :error ->
        {:error, :unknown_plugin}
    end
  end

  @spec known_plugins() :: [atom()]
  def known_plugins, do: Map.keys(@registry)
end
```

### Step 6 — `test/plugin_dispatcher_test.exs`

**Objective**: Assert the dispatcher handles unknown plugin names with an explicit error tuple rather than a raw `UndefinedFunctionError`.

```elixir
defmodule PluginDispatcherTest do
  use ExUnit.Case, async: true

  alias PluginDispatcher.Dispatcher
  alias PluginDispatcher.Plugins.{Upcase, Reverse}

  describe "dispatch/2" do
    test "routes to :upcase plugin" do
      assert Dispatcher.dispatch(:upcase, "hello") == {:ok, "HELLO"}
    end

    test "routes to :reverse plugin" do
      assert Dispatcher.dispatch(:reverse, "abc") == {:ok, "cba"}
    end

    test "returns error for unknown plugin" do
      assert Dispatcher.dispatch(:noop, "hello") == {:error, :unknown_plugin}
    end
  end

  describe "plugin behaviour" do
    test "default name/0 is derived from module alias" do
      assert Upcase.name() == :upcase
      assert Reverse.name() == :reverse
    end
  end

  describe "registry" do
    test "known_plugins/0 lists registered names" do
      assert Enum.sort(Dispatcher.known_plugins()) == [:reverse, :upcase]
    end
  end
end
```

### Step 7 — Run the tests

**Objective**: Run the suite to confirm the `use`-driven behaviour contract catches missing callbacks at compile, not at first invocation.

```bash
mix test
```

All 6 tests pass.

### Why this works

`alias` is pure lexical rename — no runtime cost. `require Logger` forces Logger's compiled `.beam` to be loaded before the macro expands, so `Logger.info/1` can compile to a conditional call gated by the log level. `use PluginDispatcher.Plugin` injects the `@behaviour` attribute and a default `name/0` implementation at compile time; `defoverridable` lets plugins replace it. Module dispatch via `module.call(input)` is a regular BEAM remote call — not reflection, not slow.

---


## Key Concepts

### 1. `alias` Creates a Shorthand
`alias MyApp.User` lets you write `User` instead of `MyApp.User`. `alias` does not import functions; it only shortens the module name.

### 2. `import` Brings Functions into Local Scope
`import Enum` lets you write `map([1,2,3], &(&1 * 2))` instead of `Enum.map`. `import` is powerful but can cause name collisions. Use sparingly.

### 3. `require` Ensures Compile-Time Code
Macros are compile-time code. `require` ensures the module is loaded before the macro is invoked. Without `require`, macros fail.

---
## Benchmark

```elixir
# bench/dispatch.exs
{t_static, _} = :timer.tc(fn ->
  Enum.each(1..1_000_000, fn _ ->
    PluginDispatcher.Plugins.Upcase.call("hello")
  end)
end)

{t_dynamic, _} = :timer.tc(fn ->
  Enum.each(1..1_000_000, fn _ ->
    PluginDispatcher.Dispatcher.dispatch(:upcase, "hello")
  end)
end)

IO.puts("direct: #{t_static} µs   via dispatcher: #{t_dynamic} µs")
```

Target: dispatcher overhead < 2 µs per call (map lookup + `Logger.info/1` macro + remote call). If `Logger` is disabled, the macro compiles out and the gap narrows to the map lookup only.

---

## Trade-offs

| Directive | Overuse risk |
|---|---|
| `alias` | Almost no risk — use freely |
| `import` | Shadows kernel functions; makes it hard to grep for where `foo/1` comes from |
| `require` | Compile-time dependency — may slow large compilations |
| `use` | Hides code injection; debugging a `use`-heavy module means reading macros |

**Rule of thumb**: reach for `alias` first, `import` sparingly, `require` only when you
need a macro, and `use` only when the framework explicitly tells you to.

---

## Common production mistakes

**1. `import` without `only:` or `except:`**
`import Enum` pulls **every** `Enum` function into scope. Two imports with overlapping
names become a shadowing mess. Always scope: `import Enum, only: [map: 2, filter: 2]`.

**2. Using `use` when `alias` would do**
`use Foo` runs a macro. If all you need is a short name, `alias Foo` is cheaper and
more transparent.

**3. Forgetting `require Logger`**
`Logger.info/1` is a macro so the call is compiled away when `:logger` level is higher.
Without `require`, you get a confusing compile error that looks like a typo.

**4. Cyclic `use` graphs**
Module A `use`s B which `use`s A. The compiler detects some cycles but not all patterns.
Keep `__using__` macros leaf-like: they inject code, they do not pull in other `use`s.

**5. Alias collisions**
`alias MyApp.User; alias Accounts.User` — the second wins silently. Rename with
`alias Accounts.User, as: AccountUser` to avoid ambiguity.

---

## Reflection

- A new team member keeps reaching for `use` where `alias` would do. What rubric would you hand them to decide in three seconds which directive to pick?
- You want plugins to live in a separate OTP application and be loaded at runtime (true plug-ins, not compile-time). What changes in the dispatcher, and why does the `@registry` map stop being viable?

---

## Resources

- [Elixir — alias, require, and import](https://hexdocs.pm/elixir/alias-require-and-import.html)
- [`use` explained — Elixir School](https://elixirschool.com/en/lessons/basics/modules/#use-5)
- [Behaviours](https://hexdocs.pm/elixir/typespecs.html#behaviours)
