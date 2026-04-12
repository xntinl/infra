# Accumulator Module Attributes for DSL Builders

**Project**: `attr_accumulator` — build a lightweight route registry DSL using `Module.register_attribute/3` with `accumulate: true`, and understand the difference vs regular attributes.

**Difficulty**: ★★★☆☆
**Estimated time**: 3–5 hours

---

## Project context

You are building `tiny_router`, a minimalist HTTP routing module where each route is
declared at the top of the user's module:

```elixir
defmodule MyApp.Router do
  use TinyRouter

  route :get,  "/users",       MyApp.UserController, :index
  route :post, "/users",       MyApp.UserController, :create
  route :get,  "/users/:id",   MyApp.UserController, :show
end
```

At compile time, each `route/4` call must record its arguments somewhere so that
`@before_compile` can emit a single `dispatch/2` function matching all of them.
The natural storage mechanism is an **accumulating module attribute**:
`@routes {verb, path, controller, action}` repeated N times.

Regular `@foo = X; @foo = Y; @foo` returns `Y` — it overwrites. Accumulator attributes
build up a list. Every large compile-time DSL in Elixir — Ecto, Phoenix, Plug, Ash —
uses them.

```
attr_accumulator/
├── lib/
│   └── attr_accumulator/
│       ├── tiny_router.ex          # the DSL
│       └── dispatcher.ex           # compile-time helper
├── test/
│   └── tiny_router_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Module.register_attribute(mod, :name, accumulate: true)`

Before the first write, you must register the attribute. After that, every `@name x`
prepends `x` to the list stored under `:name`. Reading `@name` returns the reversed
accumulation order (most recent first) — so at `@before_compile` you usually
`Enum.reverse/1` to get declaration order.

### 2. Persisted attributes

Adding `persist: true` causes the attribute to be embedded in the BEAM chunk metadata,
retrievable at runtime via `Module.get_attribute/2` or
`YourModule.__info__(:attributes)`. Useful for introspection APIs like
"list all registered routes from outside this module".

### 3. `@before_compile` reads, emits

The convention:

```
defmacro __using__(_) do
  quote do
    Module.register_attribute(__MODULE__, :routes, accumulate: true)
    import MyDSL
    @before_compile MyDSL
  end
end

defmacro __before_compile__(env) do
  routes = env.module |> Module.get_attribute(:routes) |> Enum.reverse()
  # emit one function clause per route
end
```

### 4. Accumulation is NOT append-to-list — it IS prepend

```
@routes :a
@routes :b
@routes :c
# Module.get_attribute(:routes) == [:c, :b, :a]
```

O(1) prepend, reverse at read time.

### 5. Lifecycle gotcha: first-write semantics

Without `register_attribute`, writing `@routes :a` once then `@routes :b` silently
overwrites. The error only surfaces when you get a list with one element. Always
register early, before any `@routes` write.

---

## Implementation

### Step 1: `lib/attr_accumulator/dispatcher.ex`

```elixir
defmodule AttrAccumulator.Dispatcher do
  @moduledoc "Turns accumulated route tuples into a dispatch/2 function body."

  @spec build_dispatch([{atom(), String.t(), module(), atom()}]) :: Macro.t()
  def build_dispatch([]) do
    quote do
      def dispatch(_, _), do: {:error, :no_routes}
      def routes, do: []
    end
  end

  def build_dispatch(routes) do
    clauses =
      for {verb, path, controller, action} <- routes do
        {match_pattern, param_extraction} = compile_path_pattern(path)

        quote do
          def dispatch(unquote(verb), unquote(match_pattern)) do
            params = unquote(param_extraction)
            {:ok, {unquote(controller), unquote(action), params}}
          end
        end
      end

    fallback =
      quote do
        def dispatch(_verb, _path), do: {:error, :not_found}
      end

    list_fun =
      quote do
        def routes, do: unquote(Macro.escape(routes))
      end

    quote do
      (unquote_splicing(clauses))
      unquote(fallback)
      unquote(list_fun)
    end
  end

  defp compile_path_pattern(path) do
    segments = String.split(path, "/", trim: true)

    {match_segments, bindings} =
      Enum.map_reduce(segments, [], fn seg, acc ->
        case seg do
          ":" <> name ->
            var = Macro.var(String.to_atom(name), __MODULE__)
            {var, [{String.to_atom(name), var} | acc]}

          literal ->
            {literal, acc}
        end
      end)

    match_pattern = quote do: unquote(match_segments)

    param_extraction =
      quote do
        %{unquote_splicing(for {k, v} <- bindings, do: {k, v})}
      end

    {match_pattern, param_extraction}
  end
end
```

### Step 2: `lib/attr_accumulator/tiny_router.ex`

```elixir
defmodule AttrAccumulator.TinyRouter do
  @moduledoc """
  Compile-time DSL for defining routes.

  Usage:

      use AttrAccumulator.TinyRouter

      route :get, "/users", MyApp.UserController, :index
      route :post, "/users", MyApp.UserController, :create
  """

  alias AttrAccumulator.Dispatcher

  defmacro __using__(_opts) do
    quote do
      Module.register_attribute(__MODULE__, :tiny_routes, accumulate: true, persist: true)
      import AttrAccumulator.TinyRouter, only: [route: 4]
      @before_compile AttrAccumulator.TinyRouter
    end
  end

  defmacro route(verb, path, controller, action)
           when is_atom(verb) and is_binary(path) do
    quote bind_quoted: [verb: verb, path: path, controller: controller, action: action] do
      @tiny_routes {verb, path, controller, action}
    end
  end

  defmacro __before_compile__(env) do
    routes = env.module |> Module.get_attribute(:tiny_routes) |> Enum.reverse()
    Dispatcher.build_dispatch(routes)
  end
end
```

### Step 3: Example user module

```elixir
defmodule AttrAccumulator.Sample.Router do
  use AttrAccumulator.TinyRouter

  route :get,  "/users",           MyApp.UserController, :index
  route :post, "/users",           MyApp.UserController, :create
  route :get,  "/users/:id",       MyApp.UserController, :show
  route :put,  "/users/:id",       MyApp.UserController, :update
end
```

### Step 4: Tests

```elixir
defmodule AttrAccumulator.TinyRouterTest do
  use ExUnit.Case, async: true

  alias AttrAccumulator.Sample.Router

  describe "dispatch/2" do
    test "matches static GET /users" do
      assert {:ok, {MyApp.UserController, :index, %{}}} =
               Router.dispatch(:get, ["users"])
    end

    test "matches parameterized /users/:id" do
      assert {:ok, {MyApp.UserController, :show, %{id: "42"}}} =
               Router.dispatch(:get, ["users", "42"])
    end

    test "different verb does not match" do
      assert {:error, :not_found} = Router.dispatch(:delete, ["users"])
    end

    test "unknown path returns :not_found" do
      assert {:error, :not_found} = Router.dispatch(:get, ["unknown"])
    end
  end

  describe "routes/0 introspection" do
    test "returns the declaration-order list" do
      [first | _] = Router.routes()
      assert first == {:get, "/users", MyApp.UserController, :index}
    end

    test "count equals declarations" do
      assert length(Router.routes()) == 4
    end
  end

  describe "persist: true" do
    test "__info__(:attributes) contains :tiny_routes" do
      attrs = Router.__info__(:attributes)
      assert Keyword.has_key?(attrs, :tiny_routes)
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. Forgetting to `register_attribute`.** The first `@routes :x` behaves like a
regular attribute, and `Module.get_attribute/2` later returns `:x` (not `[:x]`).
Classic silent bug. Always register in `__using__`.

**2. Reverse at read time, not at write.** Prepend is O(1); building the list
forward by `++` on every write is O(n) per route and makes large DSL modules
pathologically slow to compile.

**3. `persist: true` has a cost.** Persisted attributes are embedded in the BEAM
chunks. For thousands of entries this inflates `.beam` size and slows module
loading. Use it only for APIs that are actually consumed at runtime.

**4. Accumulation across siblings is impossible.** Attributes live on a single
module. Cross-module aggregation (e.g. "list every route in the app") requires
either a registry process or an after-compile scan of `Application.spec/2`.

**5. Path parsing at compile time.** `compile_path_pattern/1` here splits a binary
into segments. For very long paths with many dynamic segments, consider a more
expressive pattern language (wildcards, constraints) — see `Phoenix.Router` for
the reference implementation.

**6. Docs drift.** Generated `dispatch/2` has no per-route `@doc`. If the routes are
the API, consider emitting `@doc` and `@spec` per clause — or emit a sibling
`routes_doc/0` that feeds into your doc generator.

**7. Hot-reloading breaks accumulation.** Recompiling a single file wipes and
rebuilds its accumulator; if you rely on two files contributing to the same
attribute (you cannot, but a beginner might try via shared `use`), you end up with
only the most recently compiled module's routes.

**8. When NOT to use this.** If the list is short (< 5) and static, a plain
module-level `@routes [...]` literal is simpler and equally fast. DSL pays off
past ~10 declarations.

---

## Benchmark

Compile time is the only axis. For 1000 generated `dispatch/2` clauses, compile
takes ~200–500 ms on a modern laptop; runtime dispatch is a single jump (< 200 ns).

```elixir
# bench/route_bench.exs
Benchee.run(%{
  "dispatch /users"    => fn -> AttrAccumulator.Sample.Router.dispatch(:get, ["users"]) end,
  "dispatch /users/:id" => fn -> AttrAccumulator.Sample.Router.dispatch(:get, ["users", "99"]) end
})
```

---

## Resources

- [`Module.register_attribute/3` — hexdocs.pm](https://hexdocs.pm/elixir/Module.html#register_attribute/3)
- [Phoenix.Router source](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/router.ex) — canonical accumulator DSL
- [Ecto.Schema](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex) — `@ecto_fields` accumulator
- [*Metaprogramming Elixir* — Chris McCord, ch. 4](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/)
- [`Module.get_attribute/2`](https://hexdocs.pm/elixir/Module.html#get_attribute/2)
- [Dashbit blog — on DSL internals](https://dashbit.co/blog)
