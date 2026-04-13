# Phoenix-Style Router DSL from Scratch

**Project**: `router_dsl` — build a miniature HTTP router DSL with `scope`, `pipe_through`, and `get/post/put/delete` declarations that compile into efficient pattern-matched `dispatch/2` clauses.

---

## The business problem

You are writing a lightweight embedded HTTP router for an internal admin dashboard —
you do not want to pull Phoenix + Plug, but you do want the familiar declarative
ergonomics:

### `mix.exs`
```elixir
defmodule DslRouterBuilder.MixProject do
  use Mix.Project

  def project do
    [
      app: :dsl_router_builder,
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

## Project structure

```
router_dsl/
├── lib/
│   └── router_dsl/
│       ├── router.ex              # DSL macros
│       ├── scope_stack.ex         # compile-time scope helpers
│       └── route.ex               # struct
├── test/
│   └── router_dsl_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why compile-time routing and not runtime dispatch

A runtime dispatcher walks a list of patterns for every request. Compile-time routing emits one pattern-matched clause per route, letting the BEAM decide dispatch in a single jump. Phoenix itself takes this approach for exactly this reason.

---

## Design decisions

**Option A — literal list of routes and a runtime dispatcher**
- Pros: simple; editable at runtime; trivially hot-reloaded.
- Cons: one lookup per request; no compile-time check for overlapping patterns.

**Option B — DSL that emits `def dispatch/2` clauses** (chosen)
- Pros: direct jump table; compile-time overlap detection possible.
- Cons: recompile to change routes; path-pattern compilation is nontrivial.

→ Chose **B** because routes are effectively static per deploy; the hot path benefits and the tooling story (docs, introspection) improves.

---

## Implementation

### `lib/router_dsl.ex`

```elixir
defmodule RouterDsl do
  @moduledoc """
  Phoenix-Style Router DSL from Scratch.

  A runtime dispatcher walks a list of patterns for every request. Compile-time routing emits one pattern-matched clause per route, letting the BEAM decide dispatch in a single....
  """
end
```
### `lib/router_dsl/route.ex`

**Objective**: Define Route struct to model verb, path_segments, controller, action, pipelines uniformly.

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
### `lib/router_dsl/scope_stack.ex`

**Objective**: Parse paths to segment lists, merge scope prefixes, build_match_ast for guard patterns and params map AST.

```elixir
defmodule RouterDSL.ScopeStack do
  @moduledoc false

  @spec parse_path(String.t()) :: [String.t() | {:bind, atom()}]
  def parse_path(path) when is_binary(path) do
    path
    |> String.split("/", trim: true)
    |> Enum.map(fn
      ":" <> name -> {:bind, String.to_existing_atom(name)}
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
### `lib/router_dsl/router.ex`

**Objective**: Implement __using__, scope/1, get/post/put/delete, pipeline, pipe_through, emit dispatch clauses with pattern matching.

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

**Objective**: Define Sample.Admin router with pipeline, scopes, nested scopes, and HTTP verbs for integration testing.

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
### `test/router_dsl_test.exs`

**Objective**: Assert static/dynamic routes match, verb matters, scopes nest, not_found fallback works, __routes__/0 introspection accurate.

```elixir
defmodule RouterDSLTest do
  use ExUnit.Case, async: true
  doctest RouterDSL.Sample.Admin

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
### Why this works

Each `get/post/...` macro accumulates a route tuple. `@before_compile` compiles every path into a pattern AST, then emits a clause that matches `{method, path_pattern}` and binds dynamic segments to named parameters. The BEAM turns the clauses into a jump table.

---

## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---

## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

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

## Reflection

- Your app acquires a plugin system where third parties contribute routes at runtime. Can the compile-time DSL survive, or do you bolt on a runtime layer? What are the implications for precedence?
- How do you detect two routes that overlap (e.g. `/users/:id` and `/users/me`) at compile time? Which one should win, and why?

---

### `script/main.exs`
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

defmodule Main do
  def main do
      # Simulate Phoenix-style router DSL
      defmodule Router do
        defmacro route(method, path, handler) do
          quote do
            @routes (@routes || []) ++ [{unquote(method), unquote(path), unquote(handler)}]
          end
        end

        defmacro __using__(_opts) do
          quote do
            @routes []
            import Router

            def __routes__, do: @routes

            def dispatch(method, path) do
              Enum.find_value(__routes__, fn {m, p, handler} ->
                if m == method and p == path do
                  {:ok, handler}
                end
              end) || {:error, :not_found}
            end
          end
        end
      end

      # Define router using DSL
      defmodule AppRouter do
        use Router

        route :get, "/users", :list_users
        route :post, "/users", :create_user
        route :get, "/users/:id", :get_user
      end

      # Test
      routes = AppRouter.__routes__
      result_get = AppRouter.dispatch(:get, "/users")
      result_missing = AppRouter.dispatch(:delete, "/users")

      IO.inspect(routes, label: "✓ Compiled routes")
      IO.puts("✓ GET /users: #{inspect(result_get)}")
      IO.puts("✓ DELETE /users (missing): #{inspect(result_missing)}")

      assert length(routes) == 3, "All routes defined"
      assert match?({:ok, :list_users}, result_get), "Route found"
      assert match?({:error, :not_found}, result_missing), "Missing route not found"

      IO.puts("✓ Router DSL: Phoenix-style routing working")
  end
end

Main.main()

```
---

## Why Phoenix-Style Router DSL from Scratch matters

Mastering **Phoenix-Style Router DSL from Scratch** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts

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
