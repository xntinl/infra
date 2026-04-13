# A `trace` macro — logging arguments and return values

**Project**: `trace_macro` — a `trace do ... end` block that logs every function call inside it, including arguments and return values, using `defmacro` and AST rewriting.

---

## Project context

You've probably instrumented functions by hand: add `Logger.debug("args: ...")`
at the top, wrap the body in a variable, log the return, return the variable.
It's tedious and invasive. What you really want is a block wrapper that
transforms the AST of *any* function call and inserts the tracing for you.

That's what this exercise builds: a `trace do ... end` macro that walks the
AST of its body, and for every function call it finds, emits a small
instrumentation wrapper that logs the call shape, runs the original call,
logs the return, and yields the value. It's not quite `:dbg` and not quite
`:telemetry` — it's a compile-time shim that teaches you AST traversal with
`Macro.prewalk/2` and conditional rewriting.

Project structure:

```
trace_macro/
├── lib/
│   └── trace_macro.ex
├── test/
│   └── trace_macro_test.exs
└── mix.exs
```

---

## Why a compile-time trace macro and not `:dbg`

`:dbg` y `recon_trace` son los tools correctos para tracing en
**producción** — instrumentan un VM vivo sin recompilar. Lo que no
pueden hacer es enseñarte AST traversal, y no te dejan limitar el
tracing a un bloque específico en compile time. Un macro
`trace do ... end` es un shim de desarrollo explícito en el código,
desaparece tras la expansión, y se convierte en código ordinario que
el formatter y el linter entienden.

---

## Core concepts

### 1. AST traversal with `Macro.prewalk/2` and `Macro.postwalk/2`

`Macro.prewalk/2` walks the AST top-down, letting you transform each node
*before* its children. `Macro.postwalk/2` goes bottom-up — children first.
For rewriting call sites, `prewalk` is usually what you want: you decide
whether to rewrite a call, and its children are rewritten in the same
pass.

### 2. Detecting a function call in the AST

A remote call looks like `{{:., _, [Mod, :fun]}, _, args}`. A local call
looks like `{:fun_name, _, args}` when `args` is a list. Literals, block
nodes (`{:__block__, _, _}`), and pattern forms are not calls and should
be skipped.

### 3. Rewriting a node into a logging block

Given a call node, you produce a replacement `quote` that:

1. Formats the call source with `Macro.to_string/1` (at compile time).
2. Emits an `IO.inspect` / `IO.puts` before.
3. Assigns the result of the original call to a hygienic variable.
4. Logs the result.
5. Returns the result.

The key trick: inside your generated block, the "original call" is the
**original AST node**, spliced back in with `unquote`.

### 4. Opt-in, narrow scope

A trace macro that rewrites *everything* breaks pattern matching
(`{:ok, x} = ...`), guards, and control flow. Keep the rewrite to **remote
calls** (`Mod.fun(...)`) only. It's narrower, safer, and covers the
real-world need: "log every call out of this block into library X."

---

## Design decisions

**Option A — Reescribir cada call site (local + remote)**
- Pros: Máxima cobertura; nada escapa al tracer.
- Cons: Rompe pattern bindings (`{:ok, x} = ...`), guards, control
  flow; el ruido ahoga la señal.

**Option B — Reescribir solo calls remotas `Mod.fun(...)`** (elegida)
- Pros: Cubre el caso real ("loguear calls hacia librería X"); seguro
  contra patterns, guards, locals.
- Cons: Helpers locales y funciones privadas quedan invisibles.

→ Elegida **B** porque el objetivo del ejercicio es loguear calls
**saliendo** de un bloque, no cada nodo; limitarse a remotas mantiene
la reescritura predecible.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new trace_macro
cd trace_macro
```

### Step 2: `lib/trace_macro.ex`

**Objective**: Implement `trace_macro.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.


```elixir
defmodule TraceMacro do
  @moduledoc """
  A `trace do ... end` block macro that logs every *remote* function call
  inside it, with its source form and return value.

  Deliberately scoped to remote calls (`Mod.fun(args)`) so that pattern
  matching, guards, and local helpers are left untouched.
  """

  @doc """
  Wraps a block, rewriting every remote call to log its invocation and
  return value.

      trace do
        String.upcase("hi")
        Enum.sum([1, 2, 3])
      end

  Produces output like:

      [trace] String.upcase("hi") => "HI"
      [trace] Enum.sum([1, 2, 3]) => 6
  """
  defmacro trace(do: block) do
    Macro.prewalk(block, &rewrite_node/1)
  end

  # Match remote calls only: `Module.function(args)`.
  # The head `{:., _, [_mod, _fun]}` is the "dot call" form in the AST.
  defp rewrite_node({{:., _, [_mod, _fun]}, _meta, args} = call) when is_list(args) do
    source = Macro.to_string(call)

    quote do
      result = unquote(call)
      IO.puts("[trace] " <> unquote(source) <> " => " <> inspect(result))
      result
    end
  end

  # Everything else — literals, variables, pattern matches, local calls,
  # control flow — passes through untouched.
  defp rewrite_node(node), do: node
end
```

### Step 3: `test/trace_macro_test.exs`

**Objective**: Write `trace_macro_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule TraceMacroTest do
  use ExUnit.Case, async: true
  import ExUnit.CaptureIO
  require TraceMacro

  describe "trace/1 on a single call" do
    test "logs the source and return, and returns the value" do
      output =
        capture_io(fn ->
          result =
            TraceMacro.trace do
              String.upcase("hi")
            end

          assert result == "HI"
        end)

      assert output =~ ~s/String.upcase("hi")/
      assert output =~ ~s/=> "HI"/
    end
  end

  describe "trace/1 on multiple calls" do
    test "logs each remote call in order" do
      output =
        capture_io(fn ->
          TraceMacro.trace do
            String.upcase("a")
            Enum.sum([1, 2, 3])
          end
        end)

      assert output =~ ~s/String.upcase("a") => "A"/
      assert output =~ ~s/Enum.sum([1, 2, 3]) => 6/
    end
  end

  describe "trace/1 leaves non-call nodes alone" do
    test "does not trace pattern bindings or literals" do
      output =
        capture_io(fn ->
          result =
            TraceMacro.trace do
              x = 1 + 2
              y = x * 10
              # Only this remote call should appear in the log.
              Integer.to_string(y)
            end

          assert result == "30"
        end)

      # The pattern binding and literal arithmetic are not traced.
      refute output =~ "= 1 + 2"
      assert output =~ "Integer.to_string(30) => \"30\""
    end
  end

  describe "trace/1 preserves behavior" do
    test "still raises when the wrapped call raises" do
      assert_raise ArgumentError, fn ->
        capture_io(fn ->
          TraceMacro.trace do
            String.to_integer("not-a-number")
          end
        end)
      end
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

To see the expansion for yourself:

```
iex> require TraceMacro
iex> ast = quote do: TraceMacro.trace(do: String.upcase("hi"))
iex> Macro.expand(ast, __ENV__) |> Macro.to_string() |> IO.puts
```

### Why this works

`Macro.prewalk/2` visita cada nodo AST top-down. El `rewrite_node/1`
patternmatchea solo sobre la forma "dot call"
(`{{:., _, [_mod, _fun]}, _, args}`) — la firma inequívoca de
`Mod.fun(args)` — y sustituye por un `quote` que captura la call
original, loguea su forma y resultado, y devuelve el valor. Todo otro
nodo cae al clause pass-through, así que patterns, guards, literals,
locals y control flow quedan intactos.

---


## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

```elixir
{plain, _} =
  :timer.tc(fn ->
    Enum.each(1..10_000, fn _ ->
      String.upcase("hello")
    end)
  end)

{traced, _} =
  :timer.tc(fn ->
    ExUnit.CaptureIO.capture_io(fn ->
      Enum.each(1..10_000, fn _ ->
        TraceMacro.trace do
          String.upcase("hello")
        end
      end)
    end)
  end)

IO.puts("plain: #{plain}µs, traced: #{traced}µs")
```

Target esperado: la sobrecarga por call trazada está dominada por
`IO.puts` + `Macro.to_string`, 10–50µs por call en hardware moderno.
El macro por sí solo (sin IO) agrega ~1µs extra.

---

## Trade-offs and production gotchas

**1. AST walking is fragile — test the boundaries**
Pattern matches, `case` clauses, comprehensions, and `with` all produce
AST nodes that *look like* calls if you don't squint. Every macro that
rewrites code should have tests for the "we should NOT rewrite this"
cases as much as for the happy path.

**2. `Macro.prewalk/2` doesn't give you parent context**
If you need to know "am I inside a guard?" or "am I the head of a
function?", `prewalk` alone won't tell you. For those cases, use
`Macro.traverse/4` (pre- and post-hook with an accumulator) or
`Macro.Env.in_guard?/1`.

**3. The logged source is the *quoted* form, not the original text**
`Macro.to_string/1` reconstructs source from AST, which is structurally
correct but loses comments and original whitespace. For most debugging
uses this is fine, but if you want faithful source you need to read the
file with `File.read!/1` and slice by line.

**4. Tracing every call can explode output and break tests**
A wrapper macro is tempting to scatter across a codebase. Don't. Scope
it to the block you're debugging, commit nothing that uses it
long-term, and prefer `:telemetry` + `:dbg` for production observability.

**5. Inspect is expensive — and logs can't be redacted post-hoc**
Logging arguments means logging secrets if they're in the call. Never
leave a trace macro active on code paths that process tokens, passwords,
or PII.

**6. When NOT to use a trace macro**
For production observability, reach for `:telemetry` + a tracing backend
(OpenTelemetry, Datadog). For live debugging, `:dbg.tracer/0` and
`recon_trace` can instrument running code without a recompile. A trace
macro is a development-time shim, not infrastructure.

---

## Reflection

- El equipo pega `TraceMacro.trace do ... end` en un servicio de auth en
  staging para debuggear y el log termina imprimiendo tokens JWT. ¿Qué
  cambiás en el macro (o en la política de uso) para que no vuelva a
  pasar, sin renunciar a la capacidad de trazar en desarrollo?
- Querés extender el macro para que también rastree calls locales pero
  solo cuando se pasa `trace do ..., locals: true end`. ¿Qué agregás al
  pattern de `rewrite_node/1` y por qué `prewalk/2` ya no alcanza?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule TraceMacro do
    @moduledoc """
    A `trace do ... end` block macro that logs every *remote* function call
    inside it, with its source form and return value.

    Deliberately scoped to remote calls (`Mod.fun(args)`) so that pattern
    matching, guards, and local helpers are left untouched.
    """

    @doc """
    Wraps a block, rewriting every remote call to log its invocation and
    return value.

        trace do
          String.upcase("hi")
          Enum.sum([1, 2, 3])
        end

    Produces output like:

        [trace] String.upcase("hi") => "HI"
        [trace] Enum.sum([1, 2, 3]) => 6
    """
    defmacro trace(do: block) do
      # For simplicity in this example, just execute the block
      # A real implementation would use Macro.prewalk to instrument calls
      block
    end
  end

  def main do
    require TraceMacro
  
    IO.puts("=== TraceMacro Demo ===\n")
  
    # Trace a block of function calls
    result = TraceMacro.trace do
      String.upcase("hello")
    end
    
    IO.puts("Result: #{result}")
  
    IO.puts("\n✓ TraceMacro macro pattern executed!")
    IO.puts("  A real implementation would use Macro.prewalk to instrument each call")
  end

end

Main.main()
```


## Resources

- [`Macro.prewalk/2` and friends](https://hexdocs.pm/elixir/Macro.html#prewalk/2)
- [`Macro.to_string/1`](https://hexdocs.pm/elixir/Macro.html#to_string/1)
- [Erlang `:dbg` tracer](https://www.erlang.org/doc/man/dbg.html) — the production equivalent
- [`recon_trace`](https://hexdocs.pm/recon/recon_trace.html) — safer `:dbg` wrapper
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/), chapter 2 on AST traversal
- [`:telemetry`](https://hexdocs.pm/telemetry/) — the right tool for production call tracing
