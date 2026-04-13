# Compile-time configuration macros — generating functions from static data

**Project**: `compile_time_routes` — a simplified `Phoenix.Router`-style DSL that reads a route table at compile time and generates a dispatching function per route.

---

## Why macro compile time config matters

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

---

## Project structure

```
compile_time_routes/
├── lib/
│   └── compile_time_routes.ex
├── script/
│   └── main.exs
├── test/
│   └── compile_time_routes_test.exs
└── mix.exs
```

---

## Why compile-time generation and not a runtime map

Un map en runtime escala linealmente a miles de rutas: cada dispatch
es un lookup. La generación en compile-time cambia tiempo de
compilación por costo runtime — cada ruta se vuelve una cláusula
pattern-match del BEAM, así que el dispatch es O(1) a nivel de
instrucción sin allocación. Para rutas estáticas conocidas al build
gana; para rutas dinámicas por tenant o desde base de datos pierde (un
recompile por cambio es inaceptable). Phoenix elige compile-time
porque web apps tienen rutas estáticas por diseño.

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

## Design decisions

**Option A — Emitir una `match_route/2` por ruta al llamar al macro**
- Pros: Modelo mental simple; sin hook `__before_compile__`.
- Cons: El orden de clauses sigue estrictamente el orden fuente; no se
  pueden reordenar ni validar globalmente.

**Option B — Acumular en `@routes`, emitir vía `__before_compile__`** (elegida)
- Pros: Todas las rutas visibles en un solo lugar antes de emitir
  código — se pueden ordenar, deduplicar, validar existencia del
  handler.
- Cons: Dos macros en vez de una; el hook `@before_compile` agrega un
  nivel de indirección.

→ Elegida **B** porque refleja la forma real de Phoenix/Plug y
habilita las features de vista global que cualquier router no trivial
necesita.

---

## Implementation

### `mix.exs`

```elixir
defmodule CompileTimeRoutes.MixProject do
  use Mix.Project

  def project do
    [
      app: :compile_time_routes,
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
    []
  end
end
```
### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new compile_time_routes
cd compile_time_routes
```

### `lib/compile_time_routes/router.ex`

**Objective**: Implement `router.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.

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
### `lib/compile_time_routes.ex`

**Objective**: Edit `compile_time_routes.ex` — a concrete router, exposing AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.

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

**Objective**: Write `compile_time_routes_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule CompileTimeRoutesTest do
  use ExUnit.Case, async: true

  doctest CompileTimeRoutes

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

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

### Why this works

`Module.register_attribute(..., accumulate: true)` convierte `@routes`
en una lista creciente visible solo durante la compilación del módulo.
Cada macro `get/2`/`post/2` appendea una tupla. El hook
`@before_compile` corre una vez por módulo justo antes de cerrar la
compilación, lee la lista final, y emite una clause
`def match_route/2` por entrada más una catch-all. El resultado es
dispatch pattern-matched puro — la primitiva de lookup más rápida del
BEAM.

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `CompileTimeRoutes`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== CompileTimeRoutes demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    :ok
  end
end

Main.main()
```
## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

```elixir
routes_map = %{
  {:get, "/users"} => :list_users,
  {:post, "/users"} => :create_user,
  {:get, "/health"} => :health
}

{compiled, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000_000, fn _ ->
      CompileTimeRoutes.match_route(:get, "/users")
    end)
  end)

{runtime, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000_000, fn _ ->
      Map.get(routes_map, {:get, "/users"})
    end)
  end)

IO.puts("compiled: #{compiled}µs, runtime map: #{runtime}µs")
```
Target esperado: dispatch compilado <0.5µs por call; Map.get runtime
~1µs. La diferencia crece con el número de rutas.

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

## Reflection

- El equipo de producto agrega "rutas personalizables por tenant" y
  propone leer rutas desde DB al iniciar el router. ¿Seguís con
  compile-time o cambiás a dispatch runtime? Justificá con dos
  criterios (frecuencia de cambio, volumen, latencia).
- El router crece a 3.000 rutas y la compilación pasa de 2s a 45s.
  ¿Qué medís para decidir si el problema es el número de clauses o el
  tamaño del AST por clause, y qué refactorizás primero?

---
## Resources

- [`Phoenix.Router` source](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/router.ex) — the real thing, same pattern at scale
- [`Plug.Router` source](https://github.com/elixir-plug/plug/blob/main/lib/plug/router.ex) — smaller and easier to read top-to-bottom
- [`Module.register_attribute/3`](https://hexdocs.pm/elixir/Module.html#register_attribute/3)
- [`@before_compile` — Module docs](https://hexdocs.pm/elixir/Module.html#module-before_compile-1)
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — chapter 4 builds a Plug-like DSL using this same pattern
- [Sasa Juric — "Elixir macros, part 5: in-module code generation"](https://www.theerlangelist.com/article/macros_5)

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/compile_time_routes_test.exs`

```elixir
defmodule CompileTimeRoutesTest do
  use ExUnit.Case, async: true

  doctest CompileTimeRoutes

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert CompileTimeRoutes.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
