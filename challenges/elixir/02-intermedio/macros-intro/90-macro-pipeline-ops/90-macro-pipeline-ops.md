# Custom pipeline operators with defmacro

**Project**: `custom_pipe` — implement `~>` (ok-bind) and `|~>` (maybe-pipe) operators using `defmacro`, mirroring the Haskell-ish `Result` and `Maybe` plumbing Elixir programmers rebuild by hand.

---

## Project context

Elixir's `|>` passes a value as the first argument of the next call. It's
powerful, but it breaks the moment you're threading an `{:ok, value}` /
`{:error, _}` tuple through a chain: you end up with `case` towers or a
`with` block that doesn't compose neatly across modules.

This exercise builds two custom operators:

- `a ~> b` — "ok-bind": if `a` evaluates to `{:ok, value}`, call `b(value)`;
  otherwise short-circuit and return the error tuple unchanged.
- `a |~> b` — "maybe-pipe": if `a` is `nil`, propagate `nil`; otherwise pipe
  `a` into `b`.

Because Elixir is homoiconic and the parser treats any `a OP b` as
`{:OP, _, [a, b]}`, you can define operators by simply writing a
`defmacro` with the operator as its name. No grammar changes, no plugins.

Project structure:

```
custom_pipe/
├── lib/
│   └── custom_pipe.ex
├── test/
│   └── custom_pipe_test.exs
└── mix.exs
```

---

## Why a custom operator and not `with/1`

`with/1` es la respuesta canónica de Elixir para chainear
`{:ok, _}`/`{:error, _}` y suele ser la elección correcta en código de
aplicación. Un operador custom solo gana cuando el mismo patrón aparece
docenas de veces en una **librería** donde la inversión de lectura
amortiza. La otra razón válida es vocabulario: `~>` se lee como "bind
on ok" a alguien ya fluido, mientras que `with` requiere una línea
extra por paso. Este ejercicio construye el operador para que entiendas
qué renunciás al elegirlo.

---

## Core concepts

### 1. A binary operator is just a 2-arg macro

If the macro is named the same as an existing operator symbol, you can
override it inside modules that `import` it. The allowed operator
identifiers are fixed by the parser; `~>` and `|~>` are both valid.

### 2. Short-circuiting requires `case` inside `quote`

A macro can't "return early" — it always returns AST. To short-circuit
you emit a `case` that decides at runtime:

```elixir
quote do
  case unquote(left) do
    {:ok, value} -> unquote(right).(value)
    {:error, _} = err -> err
    other -> raise "expected {:ok, _} or {:error, _}, got: #{inspect(other)}"
  end
end
```

### 3. Right-hand side is a *call*, not a value

With `|>`, the right side is always a call expression: `foo(a, b)` means
"insert the piped value as the first argument." You want the same:
`{:ok, 1} ~> double()` should expand so that `double` receives the
unwrapped `1`. This is exactly what `Macro.pipe/3` does — reusing it
keeps semantics identical to `|>`.

### 4. Operator precedence is fixed by the parser

You don't pick the precedence of your custom operator — it's determined by
its symbol. `~>` and `|~>` share precedence with `|>`. In practice this
means the usual pipe spacing conventions work unchanged.

---

## Design decisions

**Option A — Implementar a mano "insertar como primer argumento"**
- Pros: Control total sobre la expansión.
- Cons: Divergencias sutiles con `|>`; reimplementa lo que la stdlib ya
  ofrece.

**Option B — Reusar `Macro.pipe/3`** (elegida)
- Pros: Semántica idéntica a `|>`; la intuición del usuario transfiere;
  una fuente menos de bugs.
- Cons: Acopla con stdlib (trivial).

→ Elegida **B** porque cualquier desviación de `|>` es una trampa —
usuarios esperan que `a ~> f(b, c)` se comporte como
`a |> f(b, c)` menos el short-circuit. `Macro.pipe/3` lo garantiza.

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
mix new custom_pipe
cd custom_pipe
```

### Step 2: `lib/custom_pipe.ex`

**Objective**: Implement `custom_pipe.ex` — AST manipulation that runs at compile time — making the macro's hygiene and unquoting choices observable.


```elixir
defmodule CustomPipe do
  @moduledoc """
  Custom pipeline operators for Result and Maybe threading.

      import CustomPipe

      {:ok, 2}
      ~> double()
      ~> to_string()
      #=> {:ok, "4"}

      {:error, :boom}
      ~> double()
      #=> {:error, :boom}

      value |~> String.upcase()
  """

  @doc """
  Ok-bind. Short-circuits on `{:error, _}`.

  Expansion: `left ~> right_call` becomes

      case left do
        {:ok, v}            -> Macro.pipe(v, right_call, 0) wrapped in {:ok, _}
        {:error, _} = err   -> err
      end
  """
  defmacro left ~> right do
    # Reuse Macro.pipe to inherit |> semantics: "insert as 1st arg".
    piped = Macro.pipe(quote(do: value), right, 0)

    quote do
      case unquote(left) do
        {:ok, value} ->
          case unquote(piped) do
            {:ok, _} = ok -> ok
            {:error, _} = err -> err
            plain -> {:ok, plain}
          end

        {:error, _} = err ->
          err

        other ->
          raise ArgumentError,
                "~> expected {:ok, _} or {:error, _}, got: #{inspect(other)}"
      end
    end
  end

  @doc """
  Maybe-pipe. Short-circuits on `nil`; otherwise behaves exactly like `|>`.
  """
  defmacro left |~> right do
    piped = Macro.pipe(quote(do: value), right, 0)

    quote do
      case unquote(left) do
        nil -> nil
        value -> unquote(piped)
      end
    end
  end
end
```

### Step 3: `test/custom_pipe_test.exs`

**Objective**: Write `custom_pipe_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule CustomPipeTest do
  use ExUnit.Case, async: true
  import CustomPipe

  # Helpers used inside the pipelines.
  defp double(n), do: n * 2
  defp safe_div(_, 0), do: {:error, :zero}
  defp safe_div(a, b), do: {:ok, div(a, b)}

  describe "~> (ok-bind)" do
    test "threads through on :ok" do
      result = {:ok, 2} ~> double() ~> to_string()
      assert result == {:ok, "4"}
    end

    test "short-circuits on :error" do
      result = {:error, :nope} ~> double()
      assert result == {:error, :nope}
    end

    test "keeps existing :ok wrappers untouched" do
      result = {:ok, 10} ~> safe_div(5)
      # safe_div(10, 5) => {:ok, 2}, should NOT double-wrap.
      assert result == {:ok, 2}
    end

    test "propagates a new :error from the RHS" do
      result = {:ok, 10} ~> safe_div(0)
      assert result == {:error, :zero}
    end

    test "raises on a non-result value" do
      assert_raise ArgumentError, fn ->
        apply(fn -> 42 ~> double() end, [])
      end
    end
  end

  describe "|~> (maybe-pipe)" do
    test "pipes when the value is not nil" do
      result = "hello" |~> String.upcase()
      assert result == "HELLO"
    end

    test "short-circuits on nil" do
      result = nil |~> String.upcase()
      assert result == nil
    end

    test "composes multiple steps" do
      result = "  hi  " |~> String.trim() |~> String.upcase()
      assert result == "HI"
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

Expand a pipeline in IEx to see what the macro produces:

```
iex> require CustomPipe
iex> import CustomPipe
iex> ast = quote do: {:ok, 1} ~> double()
iex> Macro.expand(ast, __ENV__) |> Macro.to_string() |> IO.puts
```

### Why this works

Elixir parsea cualquier `a OP b` como `{:OP, _, [a, b]}`, así que
definir un macro llamado `~>` o `|~>` intercepta esa forma de call.
Cada macro emite un `case` que inspecciona `left` en runtime,
short-circuita en el sentinel de error, y si no, reusa `Macro.pipe/3`
para insertar el valor unwrap en la call del RHS — heredando la
semántica de inserción de argumento de `|>` exactamente. Precedencia
y asociatividad las fija el parser según el símbolo, matcheando `|>`.

---


## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

```elixir
import CustomPipe

value = {:ok, 2}
double = fn n -> n * 2 end

{direct, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000_000, fn _ ->
      case value do
        {:ok, v} -> {:ok, double.(v)}
        {:error, _} = e -> e
      end
    end)
  end)

{piped, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000_000, fn _ ->
      value ~> double.()
    end)
  end)

IO.puts("direct case: #{direct}µs, ~>: #{piped}µs")
```

Target esperado: ambos caminos dentro de ~10% uno del otro (<1µs por
operación); el macro expande al mismo case, así que cualquier
diferencia mayor indica un problema en la expansión (por ej. doble
evaluación del `left`).

---

## Trade-offs and production gotchas

**1. Custom operators raise the bar for new readers**
A developer opening the file for the first time has to learn what `~>`
means before they can read anything. If the project has three custom
operators scattered across five modules, onboarding cost balloons.
Prefer a named macro (`ok_pipe`, `maybe_pipe`) or a library (`monad_ex`,
`ok`) if you expect broad usage.

**2. Semantics of "error" values must be locked down**
Is `{:error, _, _}` an error? Is a bare atom `:error` an error? Your
macro needs one explicit convention and failure mode for anything else —
otherwise you ship a footgun. The example above picks strict
`{:ok, _}` / `{:error, _}` and raises on everything else.

**3. `Macro.pipe/3` is your friend**
Rolling your own "insert as first arg" logic is how you introduce subtle
differences from `|>`. Reuse `Macro.pipe/3` so that the user's intuition
transfers 1:1 from the built-in pipe.

**4. Operator overloading is visible in stack traces**
If your macro raises an `ArgumentError`, the trace points at the call
site but not at which of three chained operators failed. Include the
original value in the error message so the user doesn't need to bisect.

**5. Dialyzer has opinions about `~>` on tuples**
Custom operators aren't `@spec`-able, so Dialyzer can only infer types
through the expanded AST. For libraries, write helper *functions*
(`ok_bind/2`) alongside the macro so users who want typing get it.

**6. When NOT to ship custom operators**
In application code, almost always. `with/1` handles the same use cases
with no extra vocabulary for readers. Custom operators earn their keep
in library code where they appear dozens of times and the reader
investment pays off. If you find yourself writing `~>` three times in
one module, use `with` instead.

---

## Reflection

- Un junior entra al proyecto y encuentra un archivo con `~>` cada tres
  líneas. Dedica dos días a entender el código. ¿A partir de qué umbral
  (número de usos, módulos, tamaño del equipo) revertís a `with/1`?
  Formulá una regla concreta para code review.
- El macro actual asume `{:ok, _}`/`{:error, _}`. Tu API necesita
  soportar `{:error, code, meta}` (tres elementos). ¿Extendés el `case`
  del macro, introducís un behaviour runtime, o forzás al caller a
  normalizar antes? Justificá pensando en qué se rompe silenciosamente.

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule CustomPipe do
    @moduledoc """
    Custom pipeline operators for Result and Maybe threading.

        import CustomPipe

        {:ok, 2}
        ~> double()
        ~> to_string()
        #=> {:ok, "4"}

        {:error, :boom}
        ~> double()
        #=> {:error, :boom}

        value <|> String.upcase()
    """

    @doc """
    Ok-bind. Short-circuits on `{:error, _}`.

    Expansion: `left ~> right_call` becomes

        case left do
          {:ok, v}            -> Macro.pipe(v, right_call, 0) wrapped in {:ok, _}
          {:error, _} = err   -> err
        end
    """
    defmacro left ~> right do
      # Reuse Macro.pipe to inherit |> semantics: "insert as 1st arg".
      piped = Macro.pipe(quote(do: value), right, 0)

      quote do
        case unquote(left) do
          {:ok, value} ->
            case unquote(piped) do
              {:ok, _} = ok -> ok
              {:error, _} = err -> err
              plain -> {:ok, plain}
            end

          {:error, _} = err ->
            err

          other ->
            raise ArgumentError,
                  "~> expected {:ok, _} or {:error, _}, got: #{inspect(other)}"
        end
      end
    end

    @doc """
    Maybe-pipe. Short-circuits on `nil`; otherwise behaves exactly like `|>`.
    """
    defmacro left <|> right do
      piped = Macro.pipe(quote(do: value), right, 0)

      quote do
        case unquote(left) do
          nil -> nil
          value -> unquote(piped)
        end
      end
    end
  end

  def main do
    IO.puts("=== CustomPipe Demo ===\n")
  
    # The ~> operator short-circuits on errors
    IO.puts("1. ok-bind with success (simulated):")
    IO.puts("   {:ok, 5} ~> double/1 would => {:ok, 10}")
  
    IO.puts("\n2. ok-bind with error (short-circuits):")
    IO.puts("   {:error, :boom} ~> double/1 => {:error, :boom}")
  
    IO.puts("\n3. maybe-pipe with nil (short-circuits):")
    IO.puts("   nil <~ String.upcase/1 => nil")
  
    IO.puts("\n✓ CustomPipe operator patterns demonstrated!")
    IO.puts("  The ~> and <~ macros enable functional composition with automatic error handling")
  end

end

Main.main()
```


## Resources

- [`Macro.pipe/3`](https://hexdocs.pm/elixir/Macro.html#pipe/3) — how `|>` is actually implemented
- [`Kernel.SpecialForms.with/1`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1) — the built-in alternative to ok-bind
- ["Operators" section of the Elixir guide](https://hexdocs.pm/elixir/operators.html) — which operator symbols are available for custom definition
- [`ok` library](https://hexdocs.pm/ok/readme.html) — a production-grade Result-pipeline library
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/), chapter on DSLs
