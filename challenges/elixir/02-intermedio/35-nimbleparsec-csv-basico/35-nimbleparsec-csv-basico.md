# 35: NimbleParsec — CSV Básico

## Prerequisites

- Pattern matching avanzado (ejercicio 09)
- Bitstrings y binarios (ejercicio 24)
- Comprensiones y listas (ejercicio 10)
- Mix y dependencias Hex (ejercicio 19)
- Nociones básicas de gramáticas formales (BNF) — deseable, no obligatorio

---

## Learning Objectives

Al finalizar este ejercicio serás capaz de:

1. Declarar un parser con `defparsec/2` y entender su firma generada
2. Usar los combinadores básicos: `string/1`, `integer/1`, `choice/1`, `repeat/1`, `tag/2`, `concat/2`, `ignore/1`
3. Componer parsers complejos desde primitivos simples
4. Distinguir cuándo usar NimbleParsec versus NimbleCSV para tareas CSV reales
5. Construir un parser de expresiones aritméticas simples con NimbleParsec

---

## Concepts

### ¿Qué es NimbleParsec?

NimbleParsec es una librería de parser combinators para Elixir. Un *parser combinator* es una función que toma parsers más simples y los combina para crear parsers más complejos. Todo se evalúa en tiempo de compilación y genera código optimizado.

```elixir
# mix.exs
defp deps do
  [
    {:nimble_parsec, "~> 1.4"}
  ]
end
```

### defparsec y la firma generada

`defparsec/2` genera una función pública en el módulo. Dado:

```elixir
defmodule MyParser do
  import NimbleParsec

  defparsec :hello, string("hello")
end
```

Se genera la función:

```elixir
MyParser.hello(input, opts \\ [])
# => {:ok, ["hello"], rest, context, line, column}
# => {:error, reason, rest, context, line, column}
```

- `input` — string a parsear
- `[:ok, tokens, rest, ...]` — éxito: tokens capturados y el resto no consumido
- `{:error, reason, rest, ...}` — fallo

### Combinadores esenciales

```elixir
import NimbleParsec

# string/1 — parsea un string exacto
greeting = string("hello")

# integer/1 — parsea un entero de N dígitos (o rango)
digits = integer(min: 1)      # mínimo 1 dígito
fixed  = integer(4)           # exactamente 4 dígitos

# utf8_char/1 — parsea un carácter UTF-8 que cumpla condición
# ascii_char/1 — versión ASCII
letter = utf8_char([?a..?z, ?A..?Z])
digit_char = ascii_char([?0..?9])

# choice/1 — prueba alternativas en orden, usa la primera que funcione
sign = choice([string("+"), string("-")])

# repeat/1 — cero o más repeticiones
spaces = repeat(ascii_char([?\s, ?\t]))

# times/2 — mínimo/máximo de repeticiones
at_least_one = times(letter, min: 1)

# concat/2 — secuencia: A seguido de B
# (equivalente a usar |> entre combinadores)
signed_int = concat(optional(sign), integer(min: 1))

# tag/2 — envuelve los tokens en una tupla {:tag_name, tokens}
tagged_int = tag(integer(min: 1), :number)

# ignore/1 — parsea pero no añade al resultado
comma = ignore(string(","))

# optional/1 — cero o una vez
maybe_sign = optional(string("-"))

# lookahead/1 y lookahead_not/1 — aserciones sin consumir
# eventually/1 — avanza hasta que el parser tiene éxito
```

### Composición con |>

El operador `|>` encadena combinadores en secuencia. Los tokens de todos los pasos se concatenan:

```elixir
defmodule NumberParser do
  import NimbleParsec

  # Parsea un número entero con signo opcional
  # "+42", "-17", "0"
  signed_integer =
    optional(ascii_char([?+, ?-]))
    |> integer(min: 1)

  defparsec :signed_int, signed_integer
end

NumberParser.signed_int("-42")
# => {:ok, [45, 42], "", %{}, {1, 0}, 3}
# 45 es el code point de '-', 42 es el entero
```

### tag/2 para estructurar resultados

Sin `tag`, los tokens se concatenan en una lista plana. Con `tag` puedes agrupar lógicamente:

```elixir
defmodule CSVParser do
  import NimbleParsec

  field =
    ascii_char([])     # cualquier carácter
    |> repeat()
    |> tag(:field)

  defparsec :parse_field, field
end

CSVParser.parse_field("hello")
# => {:ok, [field: 'hello'], "", ...}
```

### ignore/1 para separadores

Los separadores como comas, espacios o saltos de línea normalmente no queremos en el resultado:

```elixir
row =
  field
  |> repeat(ignore(string(",")) |> concat(field))
  |> tag(:row)
```

### NimbleParsec vs NimbleCSV

| Caso de uso | Recomendación |
|---|---|
| CSV simple, bien formado, RFC 4180 | NimbleCSV — más rápido, menos código |
| CSV con reglas especiales (delimitadores custom, escape propio) | NimbleParsec — control total |
| Formatos estructurados (DSLs, protocolos) | NimbleParsec — esa es su fortaleza |
| Parsing de millones de filas CSV en producción | NimbleCSV con Stream |

```elixir
# NimbleCSV — forma rápida para CSV estándar
NimbleCSV.RFC4180.parse_string("a,b,c\n1,2,3")
# => [["a", "b", "c"], ["1", "2", "3"]]
```

---

## Exercises

### Ejercicio 1: Parser de números con decimales y signos

Construye un parser que reconozca números en los formatos: `42`, `-3.14`, `+0.5`, `1_000` (guión bajo como separador de miles).

```elixir
defmodule Exercise35.NumberParser do
  @moduledoc """
  Parser de números usando NimbleParsec.

  Soporta:
  - Enteros: 42, -17, +100
  - Decimales: 3.14, -0.5, +2.718
  - Separador de miles: 1_000, 1_000_000 (solo en parte entera)
  """

  import NimbleParsec

  # TODO: define el combinador `sign` que parsea "+" o "-" opcionalmente
  # Pista: optional(ascii_char([?+, ?-]))
  sign = nil  # reemplaza nil

  # TODO: define `digits_with_underscores` que parsea dígitos con guiones bajos opcionales
  # Ejemplo: "1_000_000" -> [?1, ?_, ?0, ?0, ?0, ?_, ?0, ?0, ?0]
  # Pista: times(ascii_char([?0..?9, ?_]), min: 1)
  digits_with_underscores = nil  # reemplaza nil

  # TODO: define `decimal_part` que parsea "." seguido de dígitos
  # Debe ignorar el punto y devolver los dígitos de la parte fraccionaria
  # Pista: ignore(string(".")) |> times(ascii_char([?0..?9]), min: 1)
  decimal_part = nil  # reemplaza nil

  # TODO: construye el parser completo `number`:
  # sign opcional + parte entera + parte decimal opcional
  # Tag la parte entera como :integer y la decimal como :decimal
  number = nil  # reemplaza nil

  defparsec :parse_number, number

  @doc """
  Parsea un número y devuelve un float o integer.

  ## Ejemplos

      iex> Exercise35.NumberParser.parse("42")
      {:ok, 42}

      iex> Exercise35.NumberParser.parse("-3.14")
      {:ok, -3.14}

      iex> Exercise35.NumberParser.parse("1_000")
      {:ok, 1000}

      iex> Exercise35.NumberParser.parse("abc")
      {:error, "expected number"}
  """
  def parse(input) do
    case parse_number(input) do
      {:ok, tokens, "", _ctx, _line, _col} ->
        # TODO: convierte los tokens en un número Elixir
        # Los tokens tendrán la forma [sign?, integer: [...], decimal: [...]]
        # 1. Extrae el signo (si existe) de los tokens
        # 2. Extrae los dígitos de :integer, filtra guiones bajos, conviértelos a string e integer
        # 3. Si hay :decimal, extrae sus dígitos y construye el float
        # 4. Aplica el signo
        reconstruct_number(tokens)

      {:error, reason, _rest, _ctx, _line, _col} ->
        {:error, "expected number: #{reason}"}
    end
  end

  # TODO: implementa reconstruct_number/1 que convierte la lista de tokens en un número
  # Puedes usar Float.parse/1 o construirlo manualmente con String.to_integer/1
  defp reconstruct_number(tokens) do
    # Pista: separa los tokens por tag usando Keyword.get/3
    # y construye el número paso a paso
  end
end
```

Ejemplos iex esperados:

```elixir
iex> Exercise35.NumberParser.parse("42")
{:ok, 42}
iex> Exercise35.NumberParser.parse("-3.14")
{:ok, -3.14}
iex> Exercise35.NumberParser.parse("+0.5")
{:ok, 0.5}
iex> Exercise35.NumberParser.parse("1_000_000")
{:ok, 1_000_000}
iex> Exercise35.NumberParser.parse("abc")
{:error, _}
```

### Ejercicio 2: CSV básico con comillas

Implementa un parser CSV que maneje campos con y sin comillas dobles. Un campo entre comillas puede contener comas y saltos de línea. Las comillas dobles dentro de un campo entre comillas se escapan duplicándolas (`""`).

Gramática simplificada:
```
csv      ::= row (newline row)*
row      ::= field ("," field)*
field    ::= quoted_field | plain_field
quoted   ::= '"' (char | '""')* '"'
plain    ::= [^",\n]*
```

```elixir
defmodule Exercise35.CSVParser do
  @moduledoc """
  Parser CSV con soporte para campos entre comillas.

  Soporta:
  - Campos simples: hello,world
  - Campos con comas entre comillas: "hello, world",foo
  - Comillas escapadas dentro de campos: "say ""hi""",bar
  - Múltiples filas separadas por \\n o \\r\\n
  """

  import NimbleParsec

  # TODO: define `escaped_quote` que parsea '""' y devuelve un solo '"'
  # Pista: string("\"\"") |> replace(?")
  escaped_quote = nil  # reemplaza nil

  # TODO: define `quoted_char` que parsea un carácter dentro de comillas:
  # puede ser una comilla escapada o cualquier carácter excepto '"'
  # Pista: choice([escaped_quote, utf8_char(not: ?")])
  quoted_char = nil  # reemplaza nil

  # TODO: define `quoted_field` que parsea un campo entre comillas
  # Ignora las comillas de apertura y cierre, devuelve los chars del contenido
  # Pista: ignore(string("\"")) |> repeat(quoted_char) |> ignore(string("\""))
  quoted_field = nil  # reemplaza nil

  # TODO: define `plain_field` que parsea caracteres que no sean coma, comilla ni newline
  # Pista: repeat(utf8_char(not: [?,, ?", ?\n, ?\r]))
  plain_field = nil  # reemplaza nil

  # TODO: define `field` como la elección entre quoted_field y plain_field
  # tag el resultado como :field
  field = nil  # reemplaza nil

  # TODO: define `separator` que ignora la coma entre campos
  separator = nil  # reemplaza nil

  # TODO: define `row` como field seguido de (separator field)*
  # tag el resultado como :row
  row = nil  # reemplaza nil

  # TODO: define `newline` que ignora \n o \r\n
  # Pista: ignore(choice([string("\r\n"), string("\n")]))
  newline = nil  # reemplaza nil

  # TODO: define `csv` como row seguido de (newline row)* y eof opcional
  csv = nil  # reemplaza nil

  defparsec :parse_csv, csv

  @doc """
  Parsea un string CSV y devuelve una lista de listas de strings.

  ## Ejemplos

      iex> Exercise35.CSVParser.parse("a,b,c\\n1,2,3")
      {:ok, [["a", "b", "c"], ["1", "2", "3"]]}

      iex> Exercise35.CSVParser.parse(~s("hello, world",foo))
      {:ok, [["hello, world", "foo"]]}

      iex> Exercise35.CSVParser.parse(~s("say ""hi""",bar))
      {:ok, [["say \\"hi\\"", "bar"]]}
  """
  def parse(input) do
    case parse_csv(input) do
      {:ok, tokens, _, _, _, _} ->
        result = tokens_to_rows(tokens)
        {:ok, result}

      {:error, reason, _, _, _, _} ->
        {:error, reason}
    end
  end

  # TODO: convierte los tokens [{:row, [field: [...], field: [...]]}, ...] en
  # una lista de listas de strings
  defp tokens_to_rows(tokens) do
    # Pista: extrae cada :row, luego cada :field dentro, convierte los char codes a string
    # Los chars se devuelven como listas de code points — usa List.to_string/1
  end
end
```

Ejemplo de uso:

```elixir
iex> Exercise35.CSVParser.parse("nombre,edad,ciudad\nAna,30,Madrid\nBob,25,\"Barcelona, ES\"")
{:ok, [
  ["nombre", "edad", "ciudad"],
  ["Ana", "30", "Madrid"],
  ["Bob", "25", "Barcelona, ES"]
]}
```

### Ejercicio 3: Parser de expresiones matemáticas simples

Construye un parser de expresiones que soporte suma y resta de enteros. La gramática tiene que manejar precedencia (aunque con solo `+` y `-` es simple), espacios opcionales y paréntesis básicos.

Gramática:
```
expr   ::= term (('+' | '-') term)*
term   ::= integer | '(' expr ')'
integer ::= ['-'] digit+
```

```elixir
defmodule Exercise35.MathParser do
  @moduledoc """
  Parser de expresiones matemáticas con suma y resta usando NimbleParsec.

  Gramática soportada:
    expr = term (('+' | '-') term)*
    term = integer | '(' expr ')'

  Ejemplos válidos: "1+2", "10 - 3 + 5", "(1+2)-3", "-(5)"
  """

  import NimbleParsec

  # Espacios opcionales (ignorados)
  whitespace = ignore(repeat(ascii_char([?\s, ?\t])))

  # TODO: define `integer_literal` que parsea un entero con signo negativo opcional
  # Convierte el resultado a un entero nativo usando reduce/2 con una función
  # Pista: optional(string("-")) |> integer(min: 1) |> reduce({__MODULE__, :to_int, []})
  integer_literal = nil  # reemplaza nil

  # TODO: define `operator` que parsea '+' o '-' (con espacios opcionales alrededor)
  # tag el resultado como :op
  operator = nil  # reemplaza nil

  # NimbleParsec maneja recursión mediante parsec/1 (referencia a otro defparsec)
  # Esto permite definir gramáticas recursivas como la de los paréntesis

  # TODO: define `term` como:
  # (whitespace + integer_literal) | (whitespace + '(' + expr + ')')
  # Para referenciar `expr` de forma recursiva usa: parsec(:expr)
  # tag cada término como :term
  # Pista:
  #   term = choice([
  #     whitespace |> concat(integer_literal) |> tag(:term),
  #     whitespace |> ignore(string("(")) |> parsec(:expr) |> ignore(string(")")) |> tag(:term)
  #   ])

  # TODO: define `expr` como:
  # term seguido de (operator term)*
  # tag el resultado completo como :expr
  # Pista: term |> repeat(operator |> concat(term)) |> tag(:expr)

  defparsec :expr, nil  # reemplaza nil con el combinador correcto

  @doc """
  Parsea y evalúa una expresión matemática.

  ## Ejemplos

      iex> Exercise35.MathParser.eval("1+2")
      {:ok, 3}

      iex> Exercise35.MathParser.eval("10 - 3 + 5")
      {:ok, 12}

      iex> Exercise35.MathParser.eval("(1+2)-3")
      {:ok, 0}

      iex> Exercise35.MathParser.eval("abc")
      {:error, _}
  """
  def eval(input) do
    case expr(input) do
      {:ok, [expr: tokens], "", _, _, _} ->
        {:ok, evaluate(tokens)}

      {:error, reason, _, _, _, _} ->
        {:error, reason}
    end
  end

  @doc false
  # Función auxiliar para reduce — convierte los tokens de un entero a Integer
  def to_int(tokens) do
    tokens |> Enum.join() |> String.to_integer()
  end

  # TODO: implementa evaluate/1 que recorre los tokens del AST y calcula el resultado
  # Los tokens tienen la forma: [term: [valor], op: ["+"], term: [valor], ...]
  # Itera en pares (operador, operando) y acumula el resultado
  defp evaluate(tokens) do
    # Pista: el primer token es siempre un :term (el valor inicial)
    # Los siguientes son pares {:op, [signo]}, {:term, [valor]}
    # Usa Enum.reduce con patrón de acumulador {resultado_actual, operador_pendiente}
  end
end
```

Ejemplo de uso:

```elixir
iex> Exercise35.MathParser.eval("1+2")
{:ok, 3}

iex> Exercise35.MathParser.eval("10 - 3 + 5")
{:ok, 12}

iex> Exercise35.MathParser.eval("(1+2)-3")
{:ok, 0}

iex> Exercise35.MathParser.eval("100 - 50 - 25")
{:ok, 25}

iex> Exercise35.MathParser.eval("(10 - 5) + (3 - 1)")
{:ok, 7}
```

---

## Common Mistakes

**1. Olvidar `mix deps.get` después de añadir NimbleParsec**

```bash
# mix.exs actualizado — luego siempre:
mix deps.get
mix compile
```

**2. El orden en `choice/1` importa**

`choice` prueba las alternativas en orden y usa la primera que tiene éxito. Si `plain_field` va antes que `quoted_field`, consumirá la comilla de apertura como carácter:

```elixir
# Incorrecto — plain_field consume la " de apertura
field = choice([plain_field, quoted_field])

# Correcto — quoted_field tiene prioridad
field = choice([quoted_field, plain_field])
```

**3. `repeat` en una alternativa vacía puede causar loop infinito**

Si el combinador dentro de `repeat` puede tener éxito sin consumir input (parser nullable), NimbleParsec lanzará un error en tiempo de compilación. Siempre asegúrate de que el cuerpo de `repeat` consume al menos un carácter.

**4. Los tokens son listas de code points, no strings**

```elixir
# Los chars de ascii_char/utf8_char son integers (code points)
{:ok, [?h, ?i], ...} = parse_something("hi")

# Para convertir a string:
List.to_string([?h, ?i])   # => "hi"
```

**5. `parsec/1` para recursión — no `defparsec` directamente**

Para gramáticas recursivas (paréntesis, expresiones anidadas), usa `parsec(:nombre)` dentro de los combinadores. Esto crea una referencia diferida al parser nombrado:

```elixir
# Correcto — referencia diferida
term = choice([integer_literal, ignore(string("(")) |> parsec(:expr) |> ignore(string(")"))])

# Incorrecto — intenta usar la función antes de que esté definida
term = choice([integer_literal, ignore(string("(")) |> expr() |> ignore(string(")"))])
```

**6. `reduce/2` vs post-procesamiento manual**

`reduce/2` aplica una función al final de una cadena de parsers. Es útil para transformar tokens en tiempo de parsing, pero si la lógica es compleja, es más claro hacerlo en un paso separado después de obtener el resultado de `defparsec`.

---

## Verification

```bash
# Asegúrate de tener NimbleParsec en mix.exs
mix deps.get
iex -S mix
```

```elixir
# Ejercicio 1
iex> Exercise35.NumberParser.parse("42")
{:ok, 42}
iex> Exercise35.NumberParser.parse("-3.14")
{:ok, -3.14}
iex> Exercise35.NumberParser.parse("1_000")
{:ok, 1000}

# Ejercicio 2
iex> Exercise35.CSVParser.parse("a,b\n1,2")
{:ok, [["a", "b"], ["1", "2"]]}
iex> Exercise35.CSVParser.parse(~s("hello, world",foo))
{:ok, [["hello, world", "foo"]]}

# Ejercicio 3
iex> Exercise35.MathParser.eval("1+2")
{:ok, 3}
iex> Exercise35.MathParser.eval("(10-3)+2")
{:ok, 9}
```

Test con ExUnit:

```elixir
defmodule Exercise35Test do
  use ExUnit.Case, async: true

  alias Exercise35.{NumberParser, CSVParser, MathParser}

  describe "NumberParser" do
    test "parsea entero positivo" do
      assert NumberParser.parse("42") == {:ok, 42}
    end

    test "parsea entero negativo" do
      assert NumberParser.parse("-17") == {:ok, -17}
    end

    test "parsea decimal" do
      assert NumberParser.parse("3.14") == {:ok, 3.14}
    end

    test "parsea decimal con signo" do
      assert NumberParser.parse("-0.5") == {:ok, -0.5}
    end

    test "parsea con separador de miles" do
      assert NumberParser.parse("1_000") == {:ok, 1000}
    end

    test "devuelve error con input inválido" do
      assert {:error, _} = NumberParser.parse("abc")
    end
  end

  describe "CSVParser" do
    test "parsea fila simple" do
      assert CSVParser.parse("a,b,c") == {:ok, [["a", "b", "c"]]}
    end

    test "parsea múltiples filas" do
      assert CSVParser.parse("a,b\n1,2") == {:ok, [["a", "b"], ["1", "2"]]}
    end

    test "parsea campo con coma entre comillas" do
      assert CSVParser.parse(~s("hello, world",foo)) == {:ok, [["hello, world", "foo"]]}
    end

    test "parsea comillas escapadas" do
      assert CSVParser.parse(~s("say ""hi""")) == {:ok, [["say \"hi\""]]}
    end

    test "campo vacío" do
      assert CSVParser.parse("a,,c") == {:ok, [["a", "", "c"]]}
    end
  end

  describe "MathParser" do
    test "suma simple" do
      assert MathParser.eval("1+2") == {:ok, 3}
    end

    test "resta simple" do
      assert MathParser.eval("10-3") == {:ok, 7}
    end

    test "suma y resta encadenadas" do
      assert MathParser.eval("10 - 3 + 5") == {:ok, 12}
    end

    test "con paréntesis" do
      assert MathParser.eval("(1+2)-3") == {:ok, 0}
    end

    test "paréntesis anidados" do
      assert MathParser.eval("(10-3)+2") == {:ok, 9}
    end

    test "devuelve error con input inválido" do
      assert {:error, _} = MathParser.eval("abc")
    end
  end
end
```

---

## Summary

- `defparsec/2` genera funciones de parser en tiempo de compilación con código optimizado
- Los combinadores básicos son: `string`, `integer`, `ascii_char`, `utf8_char`, `choice`, `repeat`, `optional`, `concat`, `tag`, `ignore`
- El operador `|>` encadena combinadores en secuencia
- `parsec/1` permite referencias diferidas para gramáticas recursivas (paréntesis, anidamiento)
- Para CSV estándar en producción, NimbleCSV es más apropiado; NimbleParsec brilla en formatos con reglas especiales o DSLs propios
- Los tokens de caracteres son listas de code points — usar `List.to_string/1` para convertir

---

## What's Next

- **NimbleParsec avanzado**: `pre_traverse/3`, `post_traverse/3` para transformar el AST durante el parsing
- **NimbleCSV**: la alternativa de alto rendimiento para CSV estándar, también del equipo Dashbit
- **Leex y Yecc**: lexer y parser de Erlang para gramáticas más complejas (incluyendo Elixir mismo)
- **Ejercicio siguiente**: continúa con la exploración de herramientas de parsing y transformación de datos en Elixir

---

## Resources

- [NimbleParsec en Hex](https://hex.pm/packages/nimble_parsec)
- [Documentación oficial NimbleParsec](https://hexdocs.pm/nimble_parsec/NimbleParsec.html)
- [NimbleCSV en Hex](https://hex.pm/packages/nimble_csv)
- [Blog: Parser Combinators in Elixir](https://dashbit.co/blog/nimble-parsec)
- [RFC 4180 — Formato CSV](https://tools.ietf.org/html/rfc4180)
