# 22. Documentación con ExDoc

**Difficulty**: Intermedio

## Prerequisites
- Completed exercises 01–21
- Familiarity with Mix projects and `mix.exs`
- Understanding of module and function definitions
- Basic knowledge of Markdown syntax

## Learning Objectives
After completing this exercise, you will be able to:
- Write `@moduledoc` and `@doc` attributes with Markdown-formatted content
- Hide internal functions from public documentation using `@doc false`
- Write verifiable examples inside `@doc` that pass `mix doctest`
- Configure ExDoc in `mix.exs` as a dev/docs dependency
- Generate HTML documentation with `mix docs` and navigate the result
- Structure documentation so it is useful for consumers of your library

## Concepts

### Por qué documentar en Elixir

En Elixir la documentación no es un archivo separado — vive dentro del código como atributos de módulo (`@moduledoc`, `@doc`). Esto tiene consecuencias importantes: la documentación es introspectable en tiempo de ejecución mediante `h/1` en IEx, se puede testear automáticamente con doctests, y se puede publicar en HexDocs cuando publicas una librería.

La cultura del ecosistema Elixir valora la documentación de primera clase. Toda librería de Hex con buena puntuación en HexDocs tiene documentación exhaustiva. ExDoc es la herramienta oficial que convierte esos atributos en un sitio HTML navegable.

```elixir
# En IEx puedes consultar documentación en vivo
iex> h String.upcase
# Muestra el @doc de String.upcase con ejemplos
```

### @moduledoc: documenta el módulo completo

`@moduledoc` aparece inmediatamente después de `defmodule`. Acepta una heredoc de Markdown o `false` para suprimir la documentación del módulo. Debería explicar el propósito del módulo, su alcance, y mostrar al menos un ejemplo de uso completo.

```elixir
defmodule MyApp.Parser do
  @moduledoc """
  Parses structured text files into Elixir data structures.

  This module provides functions to read, validate, and transform
  CSV and JSON formatted content. It is designed for streaming
  large files without loading them fully into memory.

  ## Usage

      iex> MyApp.Parser.parse_line("alice,30,admin")
      {:ok, %{name: "alice", age: 30, role: "admin"}}

  ## Notes

  - All public functions return `{:ok, result}` or `{:error, reason}`
  - Line numbers in error messages are 1-indexed
  """
end
```

### @doc: documenta funciones individuales

`@doc` precede inmediatamente a `def` o `defmacro`. La convención establecida en el ecosistema incluye: una línea de descripción breve, un párrafo de detalle si aplica, y una sección `## Examples` con ejemplos en bloques de código `iex>`.

```elixir
defmodule MyApp.Math do
  @doc """
  Returns the factorial of a non-negative integer.

  Raises `ArgumentError` if `n` is negative.

  ## Examples

      iex> MyApp.Math.factorial(0)
      1

      iex> MyApp.Math.factorial(5)
      120

      iex> MyApp.Math.factorial(-1)
      ** (ArgumentError) n must be non-negative

  """
  def factorial(0), do: 1
  def factorial(n) when n > 0, do: n * factorial(n - 1)
  def factorial(_), do: raise(ArgumentError, "n must be non-negative")
end
```

### @doc false: funciones públicas ocultas

A veces una función debe ser `def` (pública) por razones técnicas — por ejemplo, callbacks de comportamientos, funciones llamadas desde macros, o puntos de extensión — pero no forma parte de la API pública que quieres documentar. `@doc false` la excluye del HTML generado por ExDoc y de `h/1` en IEx.

```elixir
defmodule MyApp.Internal do
  @doc false
  def __introspect__(key), do: Map.get(@registry, key)
  # No aparece en ExDoc aunque sea pública
end
```

### Doctests: ejemplos verificables

Los bloques `iex>` en `@doc` y `@moduledoc` pueden ejecutarse como tests. Mix incluye un macro `doctest ModuleName` que extrae y ejecuta todos esos ejemplos. Esto garantiza que la documentación y el código estén siempre sincronizados.

```elixir
# En test/my_app/math_test.exs
defmodule MyApp.MathTest do
  use ExUnit.Case, async: true
  doctest MyApp.Math  # Ejecuta todos los iex> de @doc y @moduledoc
end
```

Las reglas de formato son estrictas: `iex> expresion` en una línea, el resultado esperado en la siguiente sin prefijo. Expresiones multilinea usan `...>`. Errores esperados usan `** (ExceptionModule) mensaje`.

### Configurar ExDoc en mix.exs

ExDoc se agrega como dependencia de desarrollo. También necesitas configurar los metadatos del proyecto para que HexDocs los use correctamente:

```elixir
# mix.exs
def project do
  [
    app: :my_app,
    version: "0.1.0",
    name: "MyApp",
    source_url: "https://github.com/user/my_app",
    homepage_url: "https://hexdocs.pm/my_app",
    docs: [
      main: "MyApp",          # módulo principal en el sidebar
      extras: ["README.md"]   # archivos Markdown adicionales
    ],
    deps: deps()
  ]
end

defp deps do
  [
    {:ex_doc, "~> 0.31", only: :dev, runtime: false}
  ]
end
```

Después de `mix deps.get`, ejecuta `mix docs` para generar el sitio en `doc/index.html`.

### Secciones y formato avanzado en @doc

ExDoc soporta todo el Markdown estándar más extensiones propias: tablas, bloques de notas (`> ### Note`), y agrupación de funciones con `@doc group: "Category"`. Las secciones más comunes en `@doc` son:

- Una línea de resumen (la primera línea)
- Párrafos de descripción extendida
- `## Arguments` — descripción de parámetros
- `## Returns` — qué retorna y en qué condiciones
- `## Examples` — ejemplos en `iex>`
- `## Raises` — excepciones que puede lanzar

---

## Exercises

### Exercise 1: @moduledoc completo

Completa el `@moduledoc` del siguiente módulo con descripción, un ejemplo de uso completo en `iex>`, y una sección de notas sobre convenciones de retorno.

```elixir
defmodule TextUtils do
  # TODO: Agrega @moduledoc con:
  # 1. Descripción de 2-3 oraciones sobre el propósito del módulo
  # 2. Sección ## Usage con un ejemplo usando TextUtils.word_count/1
  # 3. Sección ## Notes explicando que todas las funciones
  #    retornan {:ok, result} o {:error, reason}
  # PISTA: Usa heredoc triple comillas: @moduledoc """..."""

  def word_count(text) when is_binary(text) do
    count = text |> String.split(~r/\s+/, trim: true) |> length()
    {:ok, count}
  end

  def reverse_words(text) when is_binary(text) do
    reversed = text |> String.split() |> Enum.reverse() |> Enum.join(" ")
    {:ok, reversed}
  end
end
```

**Expected output** (IEx):
```
iex> h TextUtils
# Muestra tu @moduledoc formateado
```

---

### Exercise 2: @doc para cada función pública

Agrega `@doc` a cada función con: descripción breve, descripción de parámetros, y sección `## Examples` con al menos 2 ejemplos en formato `iex>`.

```elixir
defmodule Calculator do
  # TODO: Agrega @doc a add/2
  # Debe incluir:
  # - Línea de descripción: "Adds two numbers together."
  # - Sección ## Examples con:
  #     iex> Calculator.add(2, 3)
  #     5
  #     iex> Calculator.add(-1, 1)
  #     0
  def add(a, b), do: a + b

  # TODO: Agrega @doc a divide/2
  # Debe incluir:
  # - Descripción que mencione que retorna {:error, :division_by_zero} si b == 0
  # - Sección ## Examples con caso exitoso Y caso de división por cero
  #     iex> Calculator.divide(10, 2)
  #     {:ok, 5.0}
  #     iex> Calculator.divide(5, 0)
  #     {:error, :division_by_zero}
  def divide(_a, 0), do: {:error, :division_by_zero}
  def divide(a, b), do: {:ok, a / b}

  # TODO: Agrega @doc a abs_val/1
  # Incluye ejemplos con número positivo, negativo, y cero
  def abs_val(n) when n < 0, do: -n
  def abs_val(n), do: n
end
```

---

### Exercise 3: @doc false para funciones internas

El siguiente módulo expone una función `__registry__/0` que debe ser pública (la usan macros en tiempo de compilación) pero no debe aparecer en la documentación. Aplica `@doc false` correctamente y agrega `@doc` apropiados a las funciones que sí deben documentarse.

```elixir
defmodule EventBus do
  @handlers %{
    user_created: [],
    order_placed: [],
    payment_failed: []
  }

  # TODO: Agrega @doc false aquí
  # Esta función es pública porque la usan macros, pero no
  # debe aparecer en la documentación generada por ExDoc
  def __registry__, do: @handlers

  # TODO: Agrega @doc con descripción y ejemplos para subscribe/2
  # Describe que registra un handler para un tipo de evento
  def subscribe(event_type, handler) when is_atom(event_type) and is_function(handler, 1) do
    {:ok, {event_type, handler}}
  end

  # TODO: Agrega @doc con descripción y ejemplos para emit/2
  # Describe que emite un evento a todos los handlers registrados
  def emit(event_type, payload) when is_atom(event_type) do
    {:ok, {event_type, payload}}
  end
end
```

**Verify**: After adding `@doc false`, running `mix docs` should NOT show `__registry__/0` in the HTML.

---

### Exercise 4: Ejemplos verificables con doctest

Los siguientes `@doc` tienen ejemplos, pero algunos están mal formateados y no pasarán `doctest`. Identifica y corrige todos los problemas.

```elixir
defmodule StringHelper do
  @doc """
  Converts a string to title case.

  ## Examples

      iex> StringHelper.title_case("hello world")
      "Hello World"

      # BUG 1: El siguiente ejemplo tiene el resultado en la misma línea
      iex> StringHelper.title_case("") "".

      # BUG 2: El resultado esperado es incorrecto (upcase no es title case)
      iex> StringHelper.title_case("elixir programming")
      "ELIXIR PROGRAMMING"

      # BUG 3: Falta el prefijo iex> en la expresión
      StringHelper.title_case("one two three")
      "One Two Three"
  """
  def title_case(str) do
    str
    |> String.split()
    |> Enum.map(&String.capitalize/1)
    |> Enum.join(" ")
  end

  @doc """
  Truncates a string to max_length characters, appending "..." if truncated.

  ## Examples

      # TODO: Escribe 3 ejemplos correctos en formato iex>:
      # 1. Un string más corto que max_length (no se trunca)
      # 2. Un string exactamente igual a max_length (no se trunca)
      # 3. Un string más largo que max_length (se trunca con "...")
      # PISTA: truncate("Hello, World!", 5) => "Hello..."
  """
  def truncate(str, max_length) when byte_size(str) <= max_length, do: str
  def truncate(str, max_length), do: String.slice(str, 0, max_length) <> "..."
end
```

---

### Exercise 5: Configurar ExDoc y generar documentación

Configura ExDoc en un proyecto Mix y genera la documentación HTML.

```elixir
# TODO: Completa el archivo mix.exs con la configuración de ExDoc
# Archivo: mix.exs

defmodule MyLibrary.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_library,
      version: "0.1.0",
      elixir: "~> 1.15",
      # TODO 1: Agrega name: "MyLibrary"
      # TODO 2: Agrega source_url: "https://github.com/example/my_library"
      # TODO 3: Agrega la clave :docs con:
      #   - main: "MyLibrary" (módulo que aparece al abrir docs/)
      #   - extras: ["README.md"]
      #   - groups_for_modules: [
      #       "Core": [MyLibrary, MyLibrary.Parser],
      #       "Utilities": [MyLibrary.Utils]
      #     ]
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [
      # TODO 4: Agrega {:ex_doc, "~> 0.31", only: :dev, runtime: false}
      # PISTA: only: :dev significa que no se incluye en producción
      #        runtime: false significa que no se inicia como aplicación
    ]
  end
end

# Después de completar mix.exs:
# $ mix deps.get
# $ mix docs
# Abre doc/index.html en tu navegador
# Verifica que aparecen los módulos organizados en grupos
```

**Verification steps**:
1. `mix deps.get` descarga ExDoc sin errores
2. `mix docs` genera la carpeta `doc/` sin warnings
3. `doc/index.html` se abre en el navegador y muestra tu librería
4. Las funciones con `@doc false` no aparecen en el HTML
5. Los ejemplos en `## Examples` se ven formateados correctamente

---

## Common Mistakes

### Resultado de doctest en línea incorrecta

```elixir
# MAL: el resultado debe estar en la línea siguiente, sin prefijo
iex> String.upcase("hello") "HELLO"

# BIEN:
iex> String.upcase("hello")
"HELLO"
```

### @moduledoc después de los atributos de módulo

```elixir
# MAL: @moduledoc debe ir inmediatamente después de defmodule
defmodule MyModule do
  @behaviour SomeBehaviour
  @moduledoc "..."  # ExDoc lo ignora o muestra warning

# BIEN:
defmodule MyModule do
  @moduledoc "..."
  @behaviour SomeBehaviour
```

### Olvidar escapar caracteres especiales en ejemplos

```elixir
# Si tu output tiene llaves, necesitas mostrarlas exactamente
iex> %{a: 1} |> Map.put(:b, 2)
%{a: 1, b: 2}
# El orden en maps puede variar — usa variables o normaliza para evitar flakiness
```

### ExDoc como dependencia de runtime

```elixir
# MAL: ExDoc en runtime genera warnings y aumenta el tamaño del release
{:ex_doc, "~> 0.31"}

# BIEN:
{:ex_doc, "~> 0.31", only: :dev, runtime: false}
```

### @doc false en funciones privadas (innecesario)

```elixir
# MAL: defp ya es privado, @doc false es redundante e incorrecto
@doc false
defp calculate_internal(x), do: x * 2

# BIEN: @doc false solo tiene sentido en def (funciones públicas)
@doc false
def __protocol_impl__, do: :ok
```

---

## Try It Yourself

Documenta el módulo `MathUtils` completo. Debe tener `@moduledoc` con descripción y ejemplos de uso, y cada una de las 5 funciones debe tener `@doc` con descripción, parámetros, y al menos 2 ejemplos en `iex>` que sean válidos para `doctest`. Agrega también `@spec` a cada función.

```elixir
defmodule MathUtils do
  @moduledoc """
  # TODO: Escribe @moduledoc completo
  # - Descripción del módulo (2-3 oraciones)
  # - Sección ## Usage con un ejemplo end-to-end
  # - Sección ## Notes sobre tipos aceptados
  """

  # TODO: @spec y @doc para cada función

  def square(n), do: n * n

  def cube(n), do: n * n * n

  def clamp(value, min, max) when value < min, do: min
  def clamp(value, _min, max) when value > max, do: max
  def clamp(value, _min, _max), do: value

  def average([]), do: {:error, :empty_list}
  def average(list) do
    {:ok, Enum.sum(list) / length(list)}
  end

  def digits(n) when is_integer(n) and n >= 0 do
    Integer.digits(n)
  end
  def digits(_), do: {:error, :invalid_input}
end

# Genera la documentación y verifica en el navegador:
# $ mix docs && open doc/index.html
```

**Checklist**:
- [ ] `@moduledoc` tiene descripción, `## Usage`, y `## Notes`
- [ ] Cada función tiene `@doc` con al menos 2 ejemplos en `iex>`
- [ ] Cada función tiene `@spec` con tipos concretos
- [ ] `mix doctest MathUtils` pasa sin errores
- [ ] `mix docs` genera HTML sin warnings
- [ ] El HTML muestra todas las funciones con su documentación formateada
