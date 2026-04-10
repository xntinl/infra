# =============================================================================
# Ejercicio 32: Protocols String.Chars e Inspect
# Nivel: Intermedio
# =============================================================================
#
# Los protocolos String.Chars e Inspect controlan cómo se representan
# tus tipos en texto: el primero para interpolación de strings, el segundo
# para depuración en IEx e IO.inspect.
#
# Conceptos clave:
#   - String.Chars protocol: to_string/1 — usado en interpolación "#{value}"
#   - Inspect protocol: inspect/2 — usado en IO.inspect/2 e IEx
#   - Inspect.Opts: opciones de presentación (color, límites, etc.)
#   - Combinar ambos para una experiencia de desarrollo óptima
#
# Para correr: elixir exercise.exs
# =============================================================================

# =============================================================================
# SECCIÓN 1: String.Chars para struct User
# =============================================================================
#
# String.Chars define cómo un tipo se convierte a String.
# Se invoca automáticamente en interpolación #{} y con to_string/1.
# Si no implementas el protocolo, #{mi_struct} lanzará un error.

IO.puts("=== Sección 1: String.Chars para User ===\n")

defmodule User do
  defstruct [:name, :age, :email, :role]
end

# TODO 1: Implementa String.Chars para User tal que:
#   to_string(%User{name: "Alice", age: 30}) retorne "Alice (30)"
#   to_string(%User{name: "Bob", age: nil})  retorne "Bob"
#   El email y role NO aparecen en la representación de string simple.
#
# Pista:
#   defimpl String.Chars, for: User do
#     def to_string(%User{name: name, age: age}) when not is_nil(age) do
#       "#{name} (#{age})"
#     end
#     def to_string(%User{name: name}), do: name
#   end
#
# Tu código aquí:

# --- FIN TODO 1 ---

alice = %User{name: "Alice", age: 30, email: "alice@example.com", role: :admin}
bob   = %User{name: "Bob",   age: nil, email: "bob@example.com",   role: :user}

IO.puts("Alice: #{alice}")
IO.puts("Bob sin edad: #{bob}")
IO.puts("to_string: #{to_string(alice)}")
IO.puts("Interpolación directa: 'Bienvenido, #{alice}'\n")

# =============================================================================
# SECCIÓN 2: Inspect para representación de depuración
# =============================================================================
#
# Inspect protocol define la representación de depuración.
# Implementarlo permite que IO.inspect y IEx muestren tu tipo
# de forma legible en lugar del formato genérico de struct.
#
# La función recibe (struct, %Inspect.Opts{}) y debe retornar
# un Inspect.Algebra document. Para comenzar, usa Inspect.Algebra.to_doc/2.

IO.puts("=== Sección 2: Inspect para User ===\n")

# TODO 2: Implementa Inspect para User tal que:
#   inspect(%User{name: "Alice", age: 30, email: "alice@example.com", role: :admin})
#   retorne algo como:
#   #User<name="Alice", age=30, role=:admin>
#   (email se omite por privacidad — nunca lo exponemos en debug output)
#
# La representación debe:
#   - Mostrar name entre comillas
#   - Mostrar age como integer
#   - Mostrar role como atom (si no es nil)
#   - NUNCA mostrar email
#
# Pista usando Inspect.Algebra:
#   defimpl Inspect, for: User do
#     import Inspect.Algebra
#
#     def inspect(%User{name: name, age: age, role: role}, _opts) do
#       parts = ["name=#{inspect(name)}", "age=#{inspect(age)}"]
#       parts = if role, do: parts ++ ["role=#{inspect(role)}"], else: parts
#       concat(["#User<", Enum.join(parts, ", "), ">"])
#     end
#   end
#
# Tu código aquí:

# --- FIN TODO 2 ---

IO.puts("Inspect (sin email):")
IO.inspect(alice)
IO.inspect(bob)
IO.inspect([alice, bob])
IO.puts("")

# =============================================================================
# SECCIÓN 3: Inspect.Opts — control de la presentación
# =============================================================================
#
# Inspect.Opts contiene configuración que el caller pasa para controlar
# cómo se debe mostrar el valor. Campos útiles:
#   :structs   — si false, muestra como map normal (útil para debugging interno)
#   :limit     — máximo de items en colecciones
#   :pretty    — si true, usa saltos de línea
#   :syntax_colors — colores ANSI para IEx

IO.puts("=== Sección 3: Inspect.Opts ===\n")

defmodule ColoredPoint do
  defstruct [:x, :y, :color]
end

# TODO 3: Implementa Inspect para ColoredPoint que:
#   - Muestra la representación coloreada SOLO si opts.syntax_colors no está vacío
#   - Si hay colores: "#Point<\e[32mx=3\e[0m, \e[32my=4\e[0m>" (verde para coords)
#   - Si no hay colores: "#Point<x=3, y=4>"
#   - Si opts.structs == false: delega al comportamiento por defecto de Inspect.Map
#   - Opcionalmente muestra :color si no es nil
#
# Pista para colores ANSI:
#   green = IO.ANSI.green()
#   reset = IO.ANSI.reset()
#   "#{green}x=#{x}#{reset}"
#
# Pista para delegar a map:
#   Map.from_struct(point) |> Inspect.Map.inspect(opts)
#
# Tu código aquí:

# --- FIN TODO 3 ---

point = %ColoredPoint{x: 3, y: 4, color: :red}
IO.puts("Inspect básico:")
IO.inspect(point)

IO.puts("Inspect con pretty:")
IO.inspect(point, pretty: true)

IO.puts("Inspect con structs: false:")
IO.inspect(point, structs: false)
IO.puts("")

# =============================================================================
# SECCIÓN 4: Múltiples tipos — Point, Color, Duration
# =============================================================================
#
# Implementar ambos protocolos para varios tipos relacionados.
# Esto demuestra cómo hacer una suite de tipos coherente.

IO.puts("=== Sección 4: Múltiples Tipos ===\n")

defmodule Point do
  defstruct [:x, :y]
end

defmodule Color do
  defstruct [:r, :g, :b, :alpha]
  def new(r, g, b, alpha \\ 255), do: %__MODULE__{r: r, g: g, b: b, alpha: alpha}
end

defmodule Duration do
  defstruct [:hours, :minutes, :seconds]
  def new(h, m, s), do: %__MODULE__{hours: h, minutes: m, seconds: s}
end

# TODO 4: Implementa String.Chars e Inspect para los tres tipos:
#
#   Point:
#     to_string: "(3, 4)"
#     inspect:   "#Point<x=3, y=4>"
#
#   Color:
#     to_string: "#1A2B3C" (hex RGB) o "#1A2B3CFF" si alpha != 255
#     inspect:   "#Color<r=26, g=43, b=60, alpha=255>"
#     Pista para hex: Integer.to_string(r, 16) |> String.pad_leading(2, "0")
#
#   Duration:
#     to_string: "1h 30m 45s" (omite partes que son 0, excepto si todo es 0 → "0s")
#     inspect:   "#Duration<1h 30m 45s>"
#
# Tu código aquí:

# --- FIN TODO 4 ---

p = %Point{x: 3, y: 4}
c = Color.new(26, 43, 60)
c_alpha = Color.new(255, 128, 0, 128)
d1 = Duration.new(1, 30, 45)
d2 = Duration.new(0, 5, 0)
d3 = Duration.new(0, 0, 0)

IO.puts("Point: #{p}")
IO.puts("Color: #{c}")
IO.puts("Color con alpha: #{c_alpha}")
IO.puts("Duration 1h30m45s: #{d1}")
IO.puts("Duration solo minutos: #{d2}")
IO.puts("Duration cero: #{d3}")

IO.puts("\nInspect:")
IO.inspect(p)
IO.inspect(c)
IO.inspect(d1)
IO.puts("")

# =============================================================================
# SECCIÓN 5: Verificar integración con kernel
# =============================================================================
#
# Los protocolos se integran con funciones del kernel:
#   - Kernel.to_string/1 — llama String.Chars.to_string/1
#   - Kernel.inspect/1   — llama Inspect.inspect/2
#   - IO.puts/1 llama implícitamente to_string
#   - IO.inspect/2 llama implícitamente inspect

IO.puts("=== Sección 5: Integración con Kernel ===\n")

IO.puts("--- String.Chars ---")
IO.puts("String.Chars.to_string(point): #{String.Chars.to_string(p)}")
IO.puts("to_string/1 (kernel): #{to_string(p)}")
IO.puts("Interpolación: \"El punto es: #{p}\"")
IO.puts("IO.puts (implícito): ")
IO.puts(p)

IO.puts("\n--- Inspect ---")
IO.puts("Kernel.inspect: #{inspect(c)}")
IO.puts("IO.inspect:")
IO.inspect(c)
IO.puts("IO.inspect con label:")
IO.inspect(d1, label: "duración")
IO.puts("")

# =============================================================================
# SECCIÓN 6: Inspect para colecciones custom
# =============================================================================
#
# Si tu tipo contiene una colección, usa Inspect.Algebra para composición:
# concat, group, nest, break, etc. Esto permite que el formato se adapte
# al ancho disponible (como hace Elixir con sus tipos built-in).

IO.puts("=== Sección 6: Inspect para Colecciones ===\n")

defmodule Grid do
  defstruct [:rows, :cols, :data]

  def new(rows, cols) do
    data = List.duplicate(List.duplicate(0, cols), rows)
    %__MODULE__{rows: rows, cols: cols, data: data}
  end

  def set(%Grid{data: data} = grid, row, col, val) do
    new_row = List.replace_at(Enum.at(data, row), col, val)
    %{grid | data: List.replace_at(data, row, new_row)}
  end
end

defimpl Inspect, for: Grid do
  import Inspect.Algebra

  def inspect(%Grid{rows: rows, cols: cols, data: data}, _opts) do
    header = "Grid(#{rows}×#{cols})"
    rows_str = data
    |> Enum.map(fn row ->
      row
      |> Enum.map(&Integer.to_string/1)
      |> Enum.join(" ")
    end)
    |> Enum.join("\n  ")

    concat(["#", header, "<\n  ", rows_str, "\n>"])
  end
end

grid = Grid.new(3, 3) |> Grid.set(0, 0, 1) |> Grid.set(1, 1, 5) |> Grid.set(2, 2, 9)
IO.puts("Grid personalizado:")
IO.inspect(grid)
IO.puts("")

# =============================================================================
# SECCIÓN 7: Fallback y consolidación de protocolos
# =============================================================================
#
# Si un protocolo implementa `@fallback_to_any true`, puede definir
# una implementación por defecto para tipos que no implementan el protocolo.
# String.Chars e Inspect tienen su propio fallback.

IO.puts("=== Sección 7: Fallback de Protocolos ===\n")

# Tipos built-in ya implementan los protocolos
IO.puts("Integer: #{42}")
IO.puts("Float: #{3.14}")
IO.puts("Atom: #{:hello}")
IO.puts("List: #{inspect([1, 2, 3])}")
IO.puts("Map: #{inspect(%{a: 1})}")
IO.puts("Tuple: #{inspect({:ok, 42})}")
IO.puts("Nil: #{inspect(nil)}")
IO.puts("Boolean: #{inspect(true)}\n")

# =============================================================================
# SECCIÓN 8: Compatibilidad con Logger y Telemetry
# =============================================================================
#
# Los protocolos se usan implícitamente en:
#   - Logger.info("User: #{user}") → llama String.Chars
#   - IO.inspect para debugging
#   - Jason.encode (protocolo separado para JSON)

IO.puts("=== Sección 8: Uso en Contextos Reales ===\n")

require Logger

Logger.configure(level: :info)

user_for_log = %User{name: "Charlie", age: 25, email: "charlie@test.com", role: :editor}

# String.Chars se usa en la interpolación del mensaje de log
Logger.info("Usuario autenticado: #{user_for_log}")
Logger.info("Procesando punto: #{p}")
Logger.info("Duración de la operación: #{d1}")
IO.puts("")

# =============================================================================
# SECCIÓN 9: TRY IT YOURSELF
# =============================================================================
#
# Implementa String.Chars e Inspect para el tipo Money.
#
# Especificación de Money:
#   %Money{amount: 1050, currency: :USD}
#   - amount representa CENTAVOS (integer)
#   - currency es un atom (:USD, :EUR, :MXN, etc.)
#
# String.Chars debe retornar:
#   %Money{amount: 1050, currency: :USD}  → "$10.50 USD"
#   %Money{amount: 999,  currency: :EUR}  → "€9.99 EUR"
#   %Money{amount: 5000, currency: :MXN}  → "$50.00 MXN"
#   (USD y MXN usan "$", EUR usa "€", GBP usa "£", otros usan el código)
#
# Inspect debe retornar:
#   "#Money<$10.50 USD>"
#   (misma representación que to_string pero envuelta en #Money<...>)
#
# Función de conveniencia Money.new/2:
#   Money.new(1050, :USD)  → %Money{amount: 1050, currency: :USD}
#
# Función de aritmética Money.add/2:
#   Money.add(%Money{amount: 100, currency: :USD}, %Money{amount: 200, currency: :USD})
#   → %Money{amount: 300, currency: :USD}
#   Money.add(%Money{currency: :USD}, %Money{currency: :EUR})
#   → {:error, :currency_mismatch}

IO.puts("=== SECCIÓN 9: Try It Yourself ===\n")
IO.puts("Implementa Money abajo:\n")

defmodule Money do
  defstruct [:amount, :currency]

  # Tu implementación aquí (new/2, add/2)
end

# Implementa los protocolos aquí
# defimpl String.Chars, for: Money do ...
# defimpl Inspect, for: Money do ...

# Tests de tu implementación
IO.puts("--- Tests de Money ---")

usd = Money.new(1050, :USD)
eur = Money.new(999,  :EUR)
mxn = Money.new(5000, :MXN)

IO.puts("USD: #{usd}")
IO.puts("EUR: #{eur}")
IO.puts("MXN: #{mxn}")

IO.puts("\nInspect:")
IO.inspect(usd)
IO.inspect(eur)

IO.puts("\nAritmética:")
IO.puts("Suma USD: #{inspect(Money.add(usd, Money.new(50, :USD)))}")
IO.puts("Suma mixta: #{inspect(Money.add(usd, eur))}")

IO.puts("\nInterpolación en log:")
IO.puts("Precio del producto: #{usd}")
IO.puts("Precio en MXN: #{mxn}")

# =============================================================================
# ERRORES COMUNES
# =============================================================================
IO.puts("\n=== Errores Comunes ===\n")
IO.puts("""
1. Confundir String.Chars con Inspect:
   - String.Chars.to_string → para usuarios finales (simple, sin datos internos)
   - Inspect.inspect → para desarrolladores (completo, para depuración)
   Ejemplo: to_string(%Money{amount: 1050, currency: :USD}) → "$10.50"
            inspect(%Money{amount: 1050, currency: :USD})  → "#Money<$10.50 USD>"

2. Olvidar que IO.puts llama to_string, no inspect:
   IO.puts(%User{name: "Alice"}) → usa String.Chars (puede ser solo "Alice")
   IO.inspect(%User{name: "Alice"}) → usa Inspect (muestra la estructura)

3. No implementar String.Chars y usar interpolación → Protocol.UndefinedError:
   Al hacer "#{mi_struct}", Elixir intenta llamar String.Chars.to_string.
   Si no está implementado, lanza un error en TIEMPO DE EJECUCIÓN.

4. Retornar string en lugar de Inspect.Algebra document:
   inspect/2 debe retornar un documento de Inspect.Algebra, no un String.
   Para strings simples usa: Inspect.Algebra.concat([...]) o simplemente
   una string — Inspect.Algebra convierte strings automáticamente.

5. Ignorar Inspect.Opts:
   Si opts.limit está seteado, deberías truncar colecciones largas.
   Si opts.structs == false, delegar al Inspect.Map es lo correcto.
""")
