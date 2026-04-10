# 32 - String.Chars e Inspect Protocol

## Prerequisites

- Protocols en Elixir (ejercicio 08)
- Structs y módulos
- Pattern matching básico
- Familiaridad con `IO.puts`, `IO.inspect`, interpolación de strings

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Implementar `String.Chars` para convertir structs propias en strings legibles
2. Implementar `Inspect` para controlar cómo `IO.inspect` representa tus tipos
3. Usar `Inspect.Opts` para personalizar el output: `:limit`, `:printable_limit`, `:pretty`
4. Aplicar coloreado con `Inspect.Algebra` y `Macro.inspect_atom/2`
5. Distinguir cuándo usar cada protocolo y cuál es su contrato

---

## Concepts

### El protocolo String.Chars

`String.Chars` define cómo un valor se convierte a string binario. Se invoca implícitamente en interpolación de strings y en `to_string/1`.

```elixir
# Contrato: implementar to_string/1 que devuelva un String binario
defprotocol String.Chars do
  def to_string(term)
end

# Se invoca en:
"Hola #{value}"       # interpolación
to_string(value)      # función explícita
IO.puts(value)        # IO.puts llama a to_string internamente
```

```elixir
# Implementaciones built-in ya existentes:
to_string(42)         # "42"
to_string(3.14)       # "3.14"
to_string(:atom)      # "atom"
to_string(true)       # "true"
to_string([1,2,3])    # no implementado para listas arbitrarias → error

# Para structs propias: sin implementación, la interpolación lanza Protocol.UndefinedError
```

### El protocolo Inspect

`Inspect` define cómo un valor se representa para debugging. Se invoca por `IO.inspect/2`, `inspect/2`, y el REPL de iex.

```elixir
defprotocol Inspect do
  def inspect(term, opts)  # opts es %Inspect.Opts{}
end

# A diferencia de String.Chars, Inspect puede devolver:
# - Un String directamente
# - Un Inspect.Algebra document (para formato complejo / multilinea)
```

### Inspect.Opts: opciones de formateo

```elixir
# Las opciones se pasan como segundo argumento a IO.inspect/2
IO.inspect(value, pretty: true)         # formato multilinea
IO.inspect(value, limit: 5)             # limitar elementos de listas/mapas
IO.inspect(value, printable_limit: 20)  # limitar chars en strings
IO.inspect(value, width: 40)            # ancho máximo de línea (con pretty: true)
IO.inspect(value, structs: false)       # no usar implementación de Inspect del struct
IO.inspect(value, syntax_colors: [     # colores ANSI
  number: :cyan,
  atom: :blue,
  string: :green,
  nil: :magenta,
  boolean: :magenta
])
```

### Inspect.Algebra: formato estructurado

Para implementaciones avanzadas de `Inspect` que soporten pretty-printing:

```elixir
import Inspect.Algebra

# Primitivos
empty()                     # documento vacío
string("hola")              # texto literal
line()                      # salto de línea
space()                     # espacio
break(" ")                  # espacio que puede convertirse en newline

# Composición
concat(["#MyStruct<", "value", ">"])
nest(doc, 2)                # indenta 2 espacios
group(doc)                  # intenta poner en una línea; si no, expande
```

```elixir
# Patrón típico: to_doc/2 para tipos anidados
defimpl Inspect, for: MyStruct do
  import Inspect.Algebra

  def inspect(%MyStruct{value: v, name: n}, opts) do
    concat([
      "#MyStruct<",
      "name: ",
      to_doc(n, opts),   # respeta las opts del caller (limit, colors, etc.)
      ", value: ",
      to_doc(v, opts),
      ">"
    ])
  end
end
```

---

## Exercises

### Ejercicio 1: Struct Money con to_string

Implementa `String.Chars` para un struct que representa una cantidad monetaria.

```elixir
defmodule Money do
  @moduledoc """
  Representa una cantidad monetaria con precisión de centavos.
  Almacena el monto en la menor unidad (centavos) para evitar errores de float.
  """

  @enforce_keys [:amount_cents, :currency]
  defstruct [:amount_cents, :currency]

  @currencies ~w(USD EUR GBP MXN)

  def new(amount_cents, currency)
      when is_integer(amount_cents) and currency in @currencies do
    %Money{amount_cents: amount_cents, currency: currency}
  end

  # TODO: implementar String.Chars para Money
  # El formato debe ser: "$ 12.50 USD" o "€ 9.99 EUR" según moneda
  # Reglas:
  # - Dividir amount_cents entre 100 para obtener el monto
  # - Mostrar siempre 2 decimales
  # - Usar el símbolo correcto: USD→$, EUR→€, GBP→£, MXN→$
  # - Formato: "SÍMBOLO MONTO MONEDA"
  # Ejemplo: Money.new(1099, "USD") |> to_string()  # "$ 10.99 USD"
  #          Money.new(599, "EUR")  |> to_string()  # "€ 5.99 EUR"

  defimpl String.Chars do
    def to_string(%Money{amount_cents: cents, currency: currency}) do
      # TODO: calcular part entera y decimal
      # whole   = div(cents, 100)
      # decimal = rem(cents, 100)
      # symbol  = currency_symbol(currency)
      # "#{symbol} #{whole}.#{String.pad_leading(to_string(decimal), 2, "0")} #{currency}"
    end

    # TODO: implementar currency_symbol/1 para cada moneda soportada
    defp currency_symbol(currency) do
      # USD -> "$", EUR -> "€", GBP -> "£", MXN -> "$"
    end
  end

  # TODO: implementar Inspect para Money
  # Formato: #Money<$ 10.99 USD>
  # Usar to_string ya implementado dentro del Inspect

  defimpl Inspect do
    def inspect(money, _opts) do
      # TODO: "#Money<#{to_string(money)}>"
    end
  end
end
```

```elixir
# Verificación en iex:
# alias Money

# price = Money.new(2999, "EUR")
# to_string(price)          # "€ 29.99 EUR"
# "El precio es: #{price}"  # "El precio es: € 29.99 EUR"
# IO.puts(price)            # € 29.99 EUR
# IO.inspect(price)         # #Money<€ 29.99 EUR>

# tax = Money.new(299, "USD")
# to_string(tax)            # "$ 2.99 USD"

# Verificar que funciona en listas:
# prices = [Money.new(999, "USD"), Money.new(1499, "EUR")]
# IO.inspect(prices)
# [#Money<$ 9.99 USD>, #Money<€ 14.99 EUR>]

# Caso borde: centavos < 10
# Money.new(5, "GBP") |> to_string()  # "£ 0.05 GBP"
```

---

### Ejercicio 2: Struct Tree con Inspect multilinea

Implementa `Inspect` con `Inspect.Algebra` para un árbol binario que se muestre de forma legible según la profundidad.

```elixir
defmodule Tree do
  @moduledoc """
  Árbol binario genérico.
  """
  defstruct [:value, :left, :right]

  def leaf(value), do: %Tree{value: value, left: nil, right: nil}

  def node(value, left, right), do: %Tree{value: value, left: left, right: right}

  @doc """
  Inserta un valor en un BST (Binary Search Tree).
  """
  def insert(nil, value), do: leaf(value)
  def insert(%Tree{value: v} = t, value) when value < v do
    %{t | left: insert(t.left, value)}
  end
  def insert(%Tree{value: v} = t, value) when value >= v do
    %{t | right: insert(t.right, value)}
  end

  # TODO: implementar Inspect con Inspect.Algebra
  # El formato debe ser:
  #
  # Para árbol pequeño (en una línea):
  #   #Tree<5>            (leaf)
  #   #Tree<5, left: #Tree<3>, right: #Tree<7>>
  #
  # Para árbol grande (multilinea con pretty: true):
  #   #Tree<
  #     5,
  #     left: #Tree<
  #       3,
  #       left: #Tree<1>,
  #       right: #Tree<4>
  #     >,
  #     right: #Tree<7>
  #   >

  defimpl Inspect do
    import Inspect.Algebra

    def inspect(%Tree{value: value, left: nil, right: nil}, opts) do
      # TODO: leaf — formato simple "#Tree<VALUE>"
      # Usar to_doc/2 para el value (respeta opts del caller)
    end

    def inspect(%Tree{value: value, left: left, right: right}, opts) do
      # TODO: nodo con hijos
      # Construir con concat, nest, group, break
      # Usar to_doc/2 para value, left, y right (recursivo)
      #
      # Pista para multilinea:
      # group(
      #   concat([
      #     "#Tree<",
      #     nest(
      #       concat([
      #         break(""),
      #         to_doc(value, opts),
      #         ...children...
      #       ]),
      #       2
      #     ),
      #     break(""),
      #     ">"
      #   ])
      # )
    end

    defp child_doc(nil, _opts), do: empty()
    defp child_doc(child, opts) do
      # TODO: formatear un hijo como ", left: SUBTREE" o ", right: SUBTREE"
    end
  end
end
```

```elixir
# Verificación:
# alias Tree

# leaf = Tree.leaf(5)
# IO.inspect(leaf)
# #Tree<5>

# small = Tree.node(5, Tree.leaf(3), Tree.leaf(7))
# IO.inspect(small)
# #Tree<5, left: #Tree<3>, right: #Tree<7>>

# Árbol más grande
# tree = Enum.reduce([5, 3, 7, 1, 4, 6, 9], nil, &Tree.insert(&2, &1))
# IO.inspect(tree, pretty: true, width: 40)
# #Tree<
#   5,
#   left: #Tree<
#     3,
#     left: #Tree<1>,
#     right: #Tree<4>
#   >,
#   right: #Tree<7, ...>
# >

# Verificar que los límites se respetan:
# IO.inspect(tree, limit: 2)  # acorta la representación
```

---

### Ejercicio 3: Custom Color Coding en Inspect

Implementa un struct `LogEntry` con coloreado ANSI en su representación de Inspect para hacer los logs más legibles en la terminal.

```elixir
defmodule LogEntry do
  @moduledoc """
  Entrada de log con nivel, mensaje y metadatos.
  La representación de Inspect usa colores ANSI según el nivel.
  """

  @levels [:debug, :info, :warn, :error]

  @enforce_keys [:level, :message, :timestamp]
  defstruct [:level, :message, :timestamp, metadata: %{}]

  def new(level, message, metadata \\ %{})
      when level in @levels and is_binary(message) do
    %LogEntry{
      level:     level,
      message:   message,
      timestamp: DateTime.utc_now(),
      metadata:  metadata
    }
  end

  # TODO: implementar String.Chars
  # Formato: "[LEVEL] TIMESTAMP — MESSAGE"
  # Sin colores en to_string (para archivos de log, pipes, etc.)
  # Ejemplo: "[ERROR] 2024-01-15T10:30:00Z — Connection refused"

  defimpl String.Chars do
    def to_string(%LogEntry{level: level, message: msg, timestamp: ts}) do
      # TODO: formatear timestamp como ISO8601 con Calendar.strftime o DateTime.to_iso8601
      # "[#{String.upcase(to_string(level))}] #{ts} — #{msg}"
    end
  end

  # TODO: implementar Inspect con colores según nivel
  # El coloreado solo aplica si opts.syntax_colors está configurado
  # (no asumir que siempre hay colores — respetar el entorno)
  #
  # Colores por nivel:
  # :debug  → :cyan
  # :info   → :green
  # :warn   → :yellow
  # :error  → :red
  #
  # Formato visual:
  # #LogEntry<[ERROR] 2024-01-15T10:30:00Z — Connection refused {port: 5432}>

  defimpl Inspect do
    import Inspect.Algebra

    @level_colors %{
      debug: :cyan,
      info:  :green,
      warn:  :yellow,
      error: :red
    }

    def inspect(%LogEntry{level: level, message: msg, timestamp: ts, metadata: meta}, opts) do
      # TODO: construir la representación coloreada
      # Pista: usar color/3 de Inspect.Algebra si opts.syntax_colors no es []
      # color(doc, :atom, opts)  — aplica el color del tipo :atom según opts
      #
      # Para colores personalizados (no los tipos estándar), usar escape ANSI directamente:
      # ansi_color = Map.get(@level_colors, level)
      # IO.ANSI.format([ansi_color, text, :reset])  — pero solo si opts.syntax_colors != []
      #
      # Formato: "#LogEntry<" <> level_colored <> " " <> timestamp <> " — " <> msg <> meta_part <> ">"
    end

    defp format_meta(meta, _opts) when meta == %{}, do: empty()
    defp format_meta(meta, opts) do
      # TODO: formatear metadata como " {key: value, ...}"
      # Usar to_doc para respetar los opts (limit, colors, etc.)
    end

    defp colors_enabled?(%Inspect.Opts{syntax_colors: colors}), do: colors != []
  end
end
```

```elixir
# Verificación en iex:
# alias LogEntry

# entries = [
#   LogEntry.new(:debug, "Cache hit", %{key: "user:42"}),
#   LogEntry.new(:info,  "Request processed", %{path: "/api/users", ms: 45}),
#   LogEntry.new(:warn,  "Slow query", %{table: "orders", ms: 1200}),
#   LogEntry.new(:error, "Connection refused", %{host: "db.internal", port: 5432})
# ]

# Sin colores (default en pipes/archivos):
# Enum.each(entries, fn e -> IO.puts(to_string(e)) end)
# [DEBUG] 2024-01-15T10:30:00Z — Cache hit
# [INFO]  2024-01-15T10:30:00Z — Request processed
# [WARN]  2024-01-15T10:30:00Z — Slow query
# [ERROR] 2024-01-15T10:30:00Z — Connection refused

# Con colores en iex (iex activa syntax_colors automáticamente):
# IO.inspect(entries, pretty: true)
# (los niveles aparecen con colores ANSI en terminal)

# Forzar colores:
# IO.inspect(entries,
#   pretty: true,
#   syntax_colors: [atom: :blue, string: :green, number: :cyan]
# )

# Verificar String.Chars para logging en archivo:
# log_line = to_string(LogEntry.new(:error, "Disk full"))
# File.write!("/tmp/app.log", log_line <> "\n", [:append])
```

---

## Common Mistakes

**1. Implementar `String.Chars` devolviendo algo que no es un binary**

```elixir
# MAL: to_string debe devolver siempre un String (binary)
defimpl String.Chars, for: Money do
  def to_string(%Money{amount_cents: c}) do
    c / 100  # devuelve un float, no un String!
  end
end

# BIEN:
defimpl String.Chars, for: Money do
  def to_string(%Money{amount_cents: c}) do
    Kernel.to_string(c / 100)  # Kernel.to_string para no recursión infinita
  end
end
```

**2. Confundir `to_string/1` del protocolo con `Kernel.to_string/1`**

```elixir
# Dentro de defimpl String.Chars, el nombre "to_string" hace sombra a Kernel.to_string
defimpl String.Chars, for: MyStruct do
  def to_string(%MyStruct{value: v}) do
    to_string(v)  # recursión infinita si v es otro MyStruct!
    # BIEN: Kernel.to_string(v) o Integer.to_string(v)
  end
end
```

**3. No respetar `opts` en implementaciones de Inspect**

```elixir
# MAL: ignorar opts hace que limit/colors/pretty no funcionen
defimpl Inspect, for: MyStruct do
  def inspect(%MyStruct{items: items}, _opts) do
    "#MyStruct<#{inspect(items)}>"  # items puede ser enorme, sin límite
  end
end

# BIEN: usar to_doc/2 que respeta opts
defimpl Inspect, for: MyStruct do
  import Inspect.Algebra
  def inspect(%MyStruct{items: items}, opts) do
    concat(["#MyStruct<", to_doc(items, opts), ">"])
  end
end
```

**4. Asumir que los colores siempre están disponibles**

```elixir
# MAL: siempre inyectar códigos ANSI
def inspect(term, _opts) do
  "\e[31m#MyStruct\e[0m<...>"  # aparece como basura en archivos de log
end

# BIEN: verificar si los colores están habilitados
def inspect(term, opts) do
  if opts.syntax_colors != [] do
    # versión con color
  else
    # versión plana
  end
end
```

---

## Verification

```elixir
# Verificar String.Chars:
money = Money.new(1099, "USD")
to_string(money)           # "$ 10.99 USD"
"Total: #{money}"          # "Total: $ 10.99 USD"

# Verificar que Protocol.UndefinedError ya no ocurre:
try do
  "#{%{not: :implemented}}"
rescue
  Protocol.UndefinedError -> IO.puts("Error esperado para Map sin implementación")
end

# Verificar Inspect básico:
IO.inspect(money)
# #Money<$ 10.99 USD>

# Verificar Inspect con opts:
entries = Enum.map(1..10, fn i -> Money.new(i * 100, "EUR") end)
IO.inspect(entries, limit: 3)
# [#Money<€ 1.00 EUR>, #Money<€ 2.00 EUR>, #Money<€ 3.00 EUR>, ...]

# Verificar Tree con pretty:
tree = Enum.reduce([5, 3, 7], nil, &Tree.insert(&2, &1))
IO.inspect(tree, pretty: true, width: 40)

# Verificar LogEntry coloreado:
IO.inspect(LogEntry.new(:error, "Disk full"), syntax_colors: [atom: :red])
```

---

## Summary

| Protocolo | Se invoca en | Devuelve | Propósito |
|---|---|---|---|
| `String.Chars` | `to_string/1`, `"#{}"`, `IO.puts` | `String.t()` | Representación legible para usuarios/logs |
| `Inspect` | `inspect/2`, `IO.inspect`, iex REPL | `String.t()` o `Algebra.t()` | Representación para debugging |

Reglas de oro:
- `String.Chars` es para el usuario final o para serialización a texto.
- `Inspect` es para el desarrollador que debuggea.
- Usa `to_doc/2` dentro de `Inspect` para respetar `opts` en tipos anidados.
- Usa `Inspect.Algebra` cuando el formato dependa del tamaño (pretty-printing).
- Nunca asumas que los colores ANSI están disponibles: verifica `opts.syntax_colors`.

---

## What's Next

- **33**: Access behaviour — acceso dinámico con `get_in/put_in`
- **34**: Collectable e Enumerable — protocolos para colecciones personalizadas
- Librería `Jason`: implementa `Jason.Encoder` (protocolo similar) para serialización JSON
- `Kernel.inspect/2`: entender cómo iex usa el protocolo internamente

---

## Resources

- [String.Chars docs](https://hexdocs.pm/elixir/String.Chars.html)
- [Inspect protocol docs](https://hexdocs.pm/elixir/Inspect.html)
- [Inspect.Algebra docs](https://hexdocs.pm/elixir/Inspect.Algebra.html)
- [Inspect.Opts docs](https://hexdocs.pm/elixir/Inspect.Opts.html)
- [Elixir Guide: Protocols](https://elixir-lang.org/getting-started/protocols.html)
