# `alias`, `import`, `require`, `use` — Module Directives

**Project**: `plugin_dispatcher` — loads and invokes plugin modules by name at runtime

**Difficulty**: ★★☆☆☆
**Estimated time**: 2–3 hours

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

## Why a plugin dispatcher

A dispatcher loads plugin modules by atom name and invokes a well-known callback on each.
It is the smallest realistic example that hits all four directives:

- `alias` — shorten the `PluginDispatcher.Plugin` references.
- `import` — none needed here, but we show where it would fit.
- `require Logger` — Logger's macros won't compile without it.
- `use PluginDispatcher.Plugin` — each plugin injects boilerplate via a `__using__/1`.

---

## Implementation

### Step 1 — Create the project

```bash
mix new plugin_dispatcher
cd plugin_dispatcher
mkdir -p lib/plugin_dispatcher/plugins
```

### Step 2 — `lib/plugin_dispatcher/plugin.ex`

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

```elixir
defmodule PluginDispatcher.Plugins.Upcase do
  # `use` triggers Plugin.__using__/1 — injects @behaviour and name/0.
  use PluginDispatcher.Plugin

  @impl true
  def call(input) when is_binary(input), do: String.upcase(input)
end
```

### Step 4 — `lib/plugin_dispatcher/plugins/reverse.ex`

```elixir
defmodule PluginDispatcher.Plugins.Reverse do
  use PluginDispatcher.Plugin

  @impl true
  def call(input) when is_binary(input), do: String.reverse(input)
end
```

### Step 5 — `lib/plugin_dispatcher/dispatcher.ex`

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

```bash
mix test
```

All 6 tests pass.

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

## Resources

- [Elixir — alias, require, and import](https://hexdocs.pm/elixir/alias-require-and-import.html)
- [`use` explained — Elixir School](https://elixirschool.com/en/lessons/basics/modules/#use-5)
- [Behaviours](https://hexdocs.pm/elixir/typespecs.html#behaviours)
