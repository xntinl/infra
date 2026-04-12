# Compile-time configuration macros — generating functions from static data

**Project**: `compile_time_routes` — a simplified `Phoenix.Router`-style DSL that reads a route table at compile time and generates a dispatching function per route.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Phoenix's router isn't magic. When you write

```elixir
get "/users/:id", UserController, :show
```

the `get/3` macro accumulates the route into a module attribute during
compilation. When the module is closed (`__before_compile__` or the end of
`defmodule`), another macro reads the list and **emits one function head
per route** so that the final `dispatch/2` is a plain Erlang pattern match
— no runtime list scanning, no regex against the path.

This exercise rebuilds that pattern in miniature: a DSL with `get`, `post`,
and `match` macros that accumulate routes, plus a `__before_compile__` hook
that generates the `match_route/2` function. You'll see how compile-time
data drives code generation and why the result is faster than any runtime
router can be.

Project structure:

```
compile_time_routes/
├── lib/
│   ├── compile_time_routes.ex
│   └── compile_time_routes/router.ex
├── test/
│   └── compile_time_routes_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Module attributes as compile-time storage

`Module.register_attribute(__MODULE__, :routes, accumulate: true)` turns
`@routes {:get, "/foo", ...}` into a list that grows each time the user
writes a route. The attribute is only readable *during compilation* of
that module — once the module is compiled, the attribute is gone unless
you do something with it.

### 2. `use` and `__using__/1`

`use SomeModule` expands into `SomeModule.__using__(opts)` — a macro that
typically:

1. Registers accumulating attributes.
2. Imports the DSL macros.
3. Sets up a `@before_compile` hook for final code generation.

This is the **3-line idiomatic opener** you'll see in nearly every Elixir
DSL (Ecto schemas, Phoenix controllers, Plug.Router).

### 3. `@before_compile` — the "close the module" hook

`@before_compile TheModule` tells the compiler: "call
`TheModule.__before_compile__(env)` right before finishing *this* module's
compilation." Inside that callback you can read the final list of
accumulated routes and emit the generated functions as one big
`quote` block.

### 4. Pattern-matched dispatch is faster than runtime dispatch

Emitting one function head per route means the runtime lookup is a BEAM
instruction-level pattern match on the arguments. This is the same
reason Phoenix can handle huge route tables without a measurable
dispatch cost.

---

## Implementation

### Step 1: Create the project

```bash
mix new compile_time_routes
cd compile_time_routes
```

### Step 2: `lib/compile_time_routes/router.ex`

```elixir
defmodule CompileTimeRoutes.Router do
  @moduledoc """
  A tiny Phoenix-style router DSL. Using this module:

      defmodule MyRouter do
        use CompileTimeRoutes.Router

        get  "/users",     :list_users
        get  "/users/:id", :show_user
        post "/users",     :create_user
      end

  generates `MyRouter.match_route(method, path)` as a pattern-matched
  function — one head per declared route.
  """

  @doc false
  defmacro __using__(_opts) do
    quote do
      # accumulate: true turns @routes into a growing list.
      Module.register_attribute(__MODULE__, :routes, accumulate: true)

      import CompileTimeRoutes.Router, only: [get: 2, post: 2, match: 3]

      @before_compile CompileTimeRoutes.Router
    end
  end

  @doc "Declares a GET route."
  defmacro get(path, handler), do: accumulate_route(:get, path, handler)

  @doc "Declares a POST route."
  defmacro post(path, handler), do: accumulate_route(:post, path, handler)

  @doc "Declares a route with any method atom."
  defmacro match(method, path, handler) do
    quote do
      @routes {unquote(method), unquote(path), unquote(handler)}
    end
  end

  defp accumulate_route(method, path, handler) do
    quote do
      @routes {unquote(method), unquote(path), unquote(handler)}
    end
  end

  @doc false
  defmacro __before_compile__(env) do
    # Read the final list of routes. Reverse so source order wins.
    routes = Module.get_attribute(env.module, :routes) |> Enum.reverse()

    # For each route, emit one `match_route/2` function head. The final
    # catch-all clause returns `:no_match`.
    clauses =
      for {method, path, handler} <- routes do
        quote do
          def match_route(unquote(method), unquote(path)) do
            {:ok, unquote(handler)}
          end
        end
      end

    quote do
      unquote_splicing(clauses)

      def match_route(_method, _path), do: :no_match
    end
  end
end
```

### Step 3: `lib/compile_time_routes.ex` — a concrete router

```elixir
defmodule CompileTimeRoutes do
  @moduledoc """
  A sample router built with `CompileTimeRoutes.Router`. Exists to prove
  the DSL compiles and dispatches as expected.
  """

  use CompileTimeRoutes.Router

  get "/health", :health
  get "/users", :list_users
  post "/users", :create_user
  match :delete, "/users", :delete_all_users
end
```

### Step 4: `test/compile_time_routes_test.exs`

```elixir
defmodule CompileTimeRoutesTest do
  use ExUnit.Case, async: true

  describe "match_route/2" do
    test "matches declared GET route" do
      assert CompileTimeRoutes.match_route(:get, "/health") == {:ok, :health}
    end

    test "matches declared POST route" do
      assert CompileTimeRoutes.match_route(:post, "/users") == {:ok, :create_user}
    end

    test "matches declared DELETE route via match/3" do
      assert CompileTimeRoutes.match_route(:delete, "/users") == {:ok, :delete_all_users}
    end

    test "returns :no_match for an unknown path" do
      assert CompileTimeRoutes.match_route(:get, "/nope") == :no_match
    end

    test "returns :no_match for a known path with the wrong method" do
      # /health is declared GET, not POST.
      assert CompileTimeRoutes.match_route(:post, "/health") == :no_match
    end
  end

  describe "generated code shape" do
    test "generates one function head per route plus a fallback" do
      # match_route/2 should have enough clauses to cover every route
      # plus the catch-all — proof that the @before_compile hook did
      # its job.
      {:match_route, 2} in CompileTimeRoutes.__info__(:functions) |> assert()
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Compile-time errors happen at compile time, by definition**
If you typo a route, the user won't see it until they recompile. That's
usually fine — in exchange you get pattern-matched dispatch at runtime.
Phoenix goes further and validates handler existence in
`__before_compile__`, which you can do too by calling `Code.ensure_loaded/1`
on the referenced module.

**2. `accumulate: true` is *per-module* and *per-compilation***
If you `use` the router twice, or if the user abuses recompile tricks,
you can end up with duplicate clauses. The compiler then emits a warning
about unreachable clauses — a hint that something upstream is wrong.

**3. Compile-time config changes require a recompile**
This is the biggest day-to-day gotcha. If your routes come from
`Application.get_env/2` at compile time, then you change the config in
`config/runtime.exs`, the router doesn't update. This is why Phoenix
draws a hard line: compile-time config for things that shape code,
runtime config for values.

**4. Order matters**
Pattern-matched clauses are tried top to bottom. A wildcard route
declared before a specific one *shadows* it. Either enforce an order in
`__before_compile__` (e.g., sort by specificity) or document that source
order wins and let the user's tests catch shadowing.

**5. Generated code inflates module size**
A thousand routes means a thousand function heads. The BEAM handles it
fine, but compile time grows and `.beam` files get big. For truly huge
routing tables, a runtime trie is the better data structure.

**6. When NOT to use compile-time generation**
Use runtime dispatch (a map or list lookup) when routes come from
external config, the database, or a tenant-specific setup. Code
generation shines only when the data is static at compile time.

---

## Resources

- [`Phoenix.Router` source](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/router.ex) — the real thing, same pattern at scale
- [`Plug.Router` source](https://github.com/elixir-plug/plug/blob/main/lib/plug/router.ex) — smaller and easier to read top-to-bottom
- [`Module.register_attribute/3`](https://hexdocs.pm/elixir/Module.html#register_attribute/3)
- [`@before_compile` — Module docs](https://hexdocs.pm/elixir/Module.html#module-before_compile-1)
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapter 4 builds a Plug-like DSL using this same pattern
- [Sasa Juric — "Elixir macros, part 5: in-module code generation"](https://www.theerlangelist.com/article/macros_5)
