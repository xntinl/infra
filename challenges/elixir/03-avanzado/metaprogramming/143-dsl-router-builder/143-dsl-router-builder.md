# Phoenix-Style Router DSL from Scratch

**Project**: `router_dsl` — build a miniature HTTP router DSL with `scope`, `pipe_through`, and `get/post/put/delete` declarations that compile into efficient pattern-matched `dispatch/2` clauses.

**Difficulty**: ★★★★☆
**Estimated time**: 5–6 hours

---

## Project context

You are writing a lightweight embedded HTTP router for an internal admin dashboard —
you do not want to pull Phoenix + Plug, but you do want the familiar declarative
ergonomics:

```elixir
defmodule Admin.Router do
  use RouterDSL

  pipeline :browser do
    plug :fetch_session
    plug :authenticate
  end

  scope "/admin" do
    pipe_through :browser

    get    "/users",        UserController,    :index
    get    "/users/:id",    UserController,    :show
    post   "/users",        UserController,    :create
    delete "/users/:id",    UserController,    :delete

    scope "/reports" do
      get "/weekly", ReportController, :weekly
    end
  end
end
```

Users call `Admin.Router.dispatch(conn, "GET", "/admin/users/42")` and get back
`{UserController, :show, %{id: "42"}, [:fetch_session, :authenticate]}`. This is a
condensed Phoenix.Router: compile-time collection, scope prefixing, pipeline
accumulation, and generated `dispatch/3` clauses.

```
router_dsl/
├── lib/
│   └── router_dsl/
│       ├── router.ex              # DSL macros
│       ├── scope_stack.ex         # compile-time scope helpers
│       └── route.ex               # struct
├── test/
│   └── router_dsl_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Scopes are compile-time stacks

`scope "/admin" do ... end` pushes `"/admin"` onto a stack; every nested `get` reads
the current stack and composes its full path. `end` pops. Track via a module
attribute that is overwritten (not accumulator).

### 2. Pipelines are named groups of plugs

`pipeline :browser do plug :a; plug :b end` records
`{:browser, [:a, :b]}` into a module attribute. `pipe_through :browser` pushes those
onto the current scope's plug stack.

### 3. Path segments with binds

`"/users/:id"` becomes match `["users", id]` where `id` binds. The compile-time
helper splits the literal path into segments and turns `:id` into a `Macro.var(:id)`.

### 4. Emitted `dispatch/3` uses pattern matching

Each route emits one clause:

```
def dispatch(conn, "GET", ["admin", "users", id]) do
  {UserController, :show, %{id: id}, [:fetch_session, :authenticate]}
end
```

With a fallback returning `{:error, :not_found}`.

### 5. Order matters

Routes are tried in declaration order. Static segments should come before dynamic
ones — we preserve insertion order by reversing the accumulator at the end.

---

## Implementation

### Step 1: `lib/router_dsl/route.ex`

```elixir
defmodule RouterDSL.Route do
  @moduledoc false

  @type t :: %__MODULE__{
          verb: atom(),
          path_segments: [String.t() | {:bind, atom()}],
          controller: module(),
          action: atom(),
          pipelines: [atom()]
        }

  defstruct [:verb, :path_segments, :controller, :action, :pipelines]
end
```

### Step 2: `lib/router_dsl/scope_stack.ex`

```elixir
defmodule RouterDSL.ScopeStack do
  @moduledoc false

  @spec parse_path(String.t()) :: [String.t() | {:bind, atom()}]
  def parse_path(path) when is_binary(path) do
    path
    |> String.split("/", trim: true)
    |> Enum.map(fn
      ":" <> name -> {:bind, String.to_atom(name)}
      literal -> literal
    end)
  end

  @spec merge_prefix([String.t() | {:bind, atom()}], [String.t() | {:bind, atom()}]) :: [
          String.t() | {:bind, atom()}
        ]
  def merge_prefix(prefix, path), do: prefix ++ path

  @spec build_match_ast([String.t() | {:bind, atom()}]) :: Macro.t()
  def build_match_ast(segments) do
    Enum.map(segments, fn
      {:bind, name} -> Macro.var(name, nil)
      literal when is_binary(literal) -> literal
    end)
  end

  @spec build_params_map([String.t() | {:bind, atom()}]) :: Macro.t()
  def build_params_map(segments) do
    pairs =
      for {:bind, name} <- segments do
        {name, Macro.var(name, nil)}
      end

    quote do: %{unquote_splicing(pairs)}
  end
end
```

### Step 3: `lib/router_dsl/router.ex`

```elixir
defmodule RouterDSL do
  @moduledoc """
  Compile-time HTTP router DSL.

      use RouterDSL

      scope "/api" do
        get "/users", UserController, :index
      end
  """

  alias RouterDSL.{Route, ScopeStack}

  defmacro __using__(_opts) do
    quote do
      Module.register_attribute(__MODULE__, :routes, accumulate: true)
      Module.register_attribute(__MODULE__, :pipelines_map, accumulate: false)
      @scope_prefix []
      @current_pipelines []
      @pipelines_map %{}
      import RouterDSL, only: [scope: 2, pipeline: 2, pipe_through: 1, plug: 1,
                               get: 3, post: 3, put: 3, delete: 3, patch: 3]
      @before_compile RouterDSL
    end
  end

  # ------------------------------------------------------------------
  # Scope + pipeline directives
  # ------------------------------------------------------------------

  defmacro scope(path, do: block) do
    quote do
      parent_prefix = @scope_prefix
      parent_pipelines = @current_pipelines
      @scope_prefix parent_prefix ++ RouterDSL.ScopeStack.parse_path(unquote(path))
      unquote(block)
      @scope_prefix parent_prefix
      @current_pipelines parent_pipelines
    end
  end

  defmacro pipeline(name, do: block) do
    quote do
      @pipeline_current []
      import RouterDSL, only: [plug: 1]
      unquote(block)
      @pipelines_map Map.put(@pipelines_map, unquote(name), Enum.reverse(@pipeline_current))
    end
  end

  defmacro plug(name) do
    quote bind_quoted: [name: name] do
      @pipeline_current [name | @pipeline_current]
    end
  end

  defmacro pipe_through(name) do
    quote do
      plugs = Map.fetch!(@pipelines_map, unquote(name))
      @current_pipelines @current_pipelines ++ plugs
    end
  end

  # ------------------------------------------------------------------
  # HTTP verbs
  # ------------------------------------------------------------------

  for verb <- [:get, :post, :put, :delete, :patch] do
    defmacro unquote(verb)(path, controller, action) do
      verb = unquote(verb) |> Atom.to_string() |> String.upcase()
      verb_atom = unquote(verb)

      quote bind_quoted: [
              verb: verb,
              verb_atom: verb_atom,
              path: path,
              controller: controller,
              action: action
            ] do
        full =
          @scope_prefix ++ RouterDSL.ScopeStack.parse_path(path)

        @routes %RouterDSL.Route{
          verb: verb,
          path_segments: full,
          controller: controller,
          action: action,
          pipelines: @current_pipelines
        }
      end
    end
  end

  # ------------------------------------------------------------------
  # Compilation
  # ------------------------------------------------------------------

  defmacro __before_compile__(env) do
    routes = env.module |> Module.get_attribute(:routes) |> Enum.reverse()

    clauses =
      for %Route{} = r <- routes do
        match = ScopeStack.build_match_ast(r.path_segments)
        params = ScopeStack.build_params_map(r.path_segments)

        quote do
          def dispatch(_conn, unquote(r.verb), unquote(match)) do
            {:ok,
             {unquote(r.controller), unquote(r.action), unquote(params), unquote(r.pipelines)}}
          end
        end
      end

    fallback =
      quote do
        def dispatch(_conn, _verb, _segments), do: {:error, :not_found}

        @spec __routes__() :: [RouterDSL.Route.t()]
        def __routes__, do: unquote(Macro.escape(routes))
      end

    quote do
      (unquote_splicing(clauses))
      unquote(fallback)
    end
  end
end
```

### Step 4: Example router

```elixir
defmodule RouterDSL.Sample.Admin do
  use RouterDSL

  pipeline :browser do
    plug :fetch_session
    plug :authenticate
  end

  scope "/admin" do
    pipe_through :browser

    get    "/users",     UserController, :index
    get    "/users/:id", UserController, :show
    post   "/users",     UserController, :create
    delete "/users/:id", UserController, :delete

    scope "/reports" do
      get "/weekly", ReportController, :weekly
    end
  end
end
```

### Step 5: Tests

```elixir
defmodule RouterDSLTest do
  use ExUnit.Case, async: true

  alias RouterDSL.Sample.Admin

  describe "dispatch/3" do
    test "static GET" do
      assert {:ok, {UserController, :index, %{}, [:fetch_session, :authenticate]}} =
               Admin.dispatch(:conn, "GET", ["admin", "users"])
    end

    test "dynamic segment binds" do
      assert {:ok, {UserController, :show, %{id: "42"}, _}} =
               Admin.dispatch(:conn, "GET", ["admin", "users", "42"])
    end

    test "POST /admin/users" do
      assert {:ok, {UserController, :create, %{}, _}} =
               Admin.dispatch(:conn, "POST", ["admin", "users"])
    end

    test "nested scope" do
      assert {:ok, {ReportController, :weekly, %{}, _}} =
               Admin.dispatch(:conn, "GET", ["admin", "reports", "weekly"])
    end

    test "not found" do
      assert {:error, :not_found} =
               Admin.dispatch(:conn, "GET", ["missing"])
    end

    test "wrong verb is not found" do
      assert {:error, :not_found} =
               Admin.dispatch(:conn, "GET", ["admin", "users"]) ==
                 Admin.dispatch(:conn, "PATCH", ["admin", "users"])
    end
  end

  describe "introspection" do
    test "__routes__/0 returns all compiled routes" do
      routes = Admin.__routes__()
      assert length(routes) == 5
      assert Enum.any?(routes, &(&1.action == :weekly))
    end

    test "pipelines propagate into scoped routes" do
      routes = Admin.__routes__()

      assert Enum.all?(routes, fn r ->
               r.pipelines == [:fetch_session, :authenticate]
             end)
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. Scope stack is tricky.** Since scopes push/pop via module attributes, nested
scopes require proper variable shadowing inside the macro. Using local vars in the
quote block (`parent_prefix = @scope_prefix`) is the cleanest way.

**2. Clause ordering matters.** A static route `/users/new` must come before
`/users/:id` — otherwise `:id` binds to `"new"`. The DSL preserves insertion order,
but users must be aware.

**3. Path conflicts are silent.** Two different scopes accidentally declaring the
same path produce two clauses; the first wins. Emit a compile warning when the
accumulator sees a duplicate `{verb, segments}` tuple.

**4. `head` method and `options`.** Real routers handle HEAD/OPTIONS automatically
when GET is declared. Extend the DSL to emit HEAD for every GET.

**5. Wildcard and glob segments.** `/*rest` is not supported here. Phoenix's
`*glob` pattern requires a different match shape — not difficult to add but was
deliberately out of scope.

**6. Compile time for big routers.** A router with 500 routes emits 500 function
clauses; compile time is a few seconds, runtime dispatch is ~100 ns. This matches
Phoenix.

**7. Pipelines are compile-time only.** The emitted pipelines list is inserted as
a literal. Runtime pipeline swaps are not possible — matches how Phoenix works,
use feature flags if you need dynamic behavior.

**8. When NOT to build this.** For anything beyond internal tools, use Phoenix.
Phoenix handles HEAD/OPTIONS, HEAD-derived GET, content negotiation, catch-alls,
verification, and a thousand other edge cases.

---

## Benchmark

```elixir
# bench/dispatch_bench.exs
alias RouterDSL.Sample.Admin

Benchee.run(%{
  "static dispatch" => fn -> Admin.dispatch(:conn, "GET", ["admin", "users"]) end,
  "dynamic dispatch" => fn -> Admin.dispatch(:conn, "GET", ["admin", "users", "99"]) end,
  "not found" => fn -> Admin.dispatch(:conn, "GET", ["missing"]) end
})
```

Expect ~80–200 ns per dispatch — the BEAM pattern-matching jump.

---

## Resources

- [Phoenix.Router source](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/router.ex) — reference implementation
- [Plug.Router — minimal router](https://github.com/elixir-plug/plug/blob/main/lib/plug/router.ex)
- [*Programming Phoenix* — Chris McCord](https://pragprog.com/titles/phoenix14/programming-phoenix-1-4/) — router chapter
- [*Metaprogramming Elixir* — ch. 6](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — DSL building
- [Dashbit blog on Phoenix internals](https://dashbit.co/blog)
- [BEAM pattern matching efficiency](https://blog.stenmans.org/theBeamBook/)
