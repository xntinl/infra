# =============================================================================
# Ejercicio 35: Parser Combinators — NimbleCSV y NimbleParsec
# Nivel: Intermedio
# =============================================================================
#
# Parsear texto estructurado es una tarea común en Elixir: CSV, logs,
# configuraciones, protocolos de red. NimbleCSV y NimbleParsec son las
# herramientas estándar del ecosistema para esto.
#
# Conceptos clave:
#   - NimbleCSV.define/2 — generar módulos de parsing CSV
#   - parse_string/2 — parsear CSV desde string
#   - parse_stream/2 — parsear CSV en modo streaming
#   - NimbleParsec — DSL para combinadores de parser
#   - defparsec/2 — definir parsers componibles
#
# Dependencias en mix.exs:
#   {:nimble_csv, "~> 1.2"},
#   {:nimble_parsec, "~> 1.4"}
#
# Para este ejercicio, simulamos NimbleCSV y NimbleParsec con implementaciones
# básicas para que el archivo pueda ejecutarse con: elixir exercise.exs
# En un proyecto Mix real, usa las dependencias de Hex.
# =============================================================================

IO.puts("=== Ejercicio 35: NimbleCSV y NimbleParsec ===\n")
IO.puts("NOTA: Este ejercicio requiere un proyecto Mix con las dependencias:")
IO.puts("  {:nimble_csv, \"~> 1.2\"}")
IO.puts("  {:nimble_parsec, \"~> 1.4\"}")
IO.puts("Crea el proyecto con: mix new csv_parser --module CsvParser\n")

# =============================================================================
# SECCIÓN 1: NimbleCSV.define — crear un parser CSV custom
# =============================================================================
#
# NimbleCSV.define/2 genera un módulo completo de parsing CSV mediante
# metaprogramación en tiempo de compilación. Es extremadamente rápido.
#
# Opciones disponibles:
#   separator: ","      — el delimitador de campos
#   escape: "\""        — carácter de escape para campos con delimitador
#   newlines: ["\n"]    — separadores de línea
#   headers: true       — si la primera fila es header (defecto: true con parse_string)
#   trim_bom: true      — eliminar BOM UTF-8 al inicio del archivo

IO.puts("=== Sección 1: NimbleCSV.define ===\n")

IO.puts("""
# En tu proyecto Mix, agrega en mix.exs:
#   defp deps do
#     [{:nimble_csv, "~> 1.2"}]
#   end
# Luego en tu módulo:

defmodule CsvParser.Parsers do
  # TODO 1: Define un parser CSV estándar con coma como separador
  #   NimbleCSV.define(MyCSV, separator: ",", escape: "\\\"")
  #
  # Define también un parser TSV (tab-separated):
  #   NimbleCSV.define(MyTSV, separator: "\\t", escape: "\\\"")
  #
  # Y un parser de punto-y-coma (común en Europa):
  #   NimbleCSV.define(MySemicolonCSV, separator: ";", escape: "\\\"")
  #
  # Tu código aquí:
  # NimbleCSV.define(MyCSV, separator: ",", escape: "\\"")
  # NimbleCSV.define(MyTSV, separator: "\\t", escape: "\\"")
end
""")

# Simulación de NimbleCSV para demostración standalone
defmodule SimulatedCSV do
  @moduledoc "Simulación de NimbleCSV.define(MyCSV, separator: \\",\\")"

  def parse_string(string, opts \\ []) do
    skip_headers = Keyword.get(opts, :skip_headers, 1)
    lines = String.split(string, "\n", trim: true)
    lines
    |> Enum.drop(skip_headers)
    |> Enum.map(&parse_line/1)
  end

  defp parse_line(line) do
    parse_fields(line, [], "", false)
  end

  defp parse_fields("", fields, current, _in_quotes) do
    Enum.reverse([current | fields])
  end
  defp parse_fields("\"" <> rest, fields, current, false) do
    parse_fields(rest, fields, current, true)
  end
  defp parse_fields("\"" <> rest, fields, current, true) do
    parse_fields(rest, fields, current, false)
  end
  defp parse_fields("," <> rest, fields, current, false) do
    parse_fields(rest, [current | fields], "", false)
  end
  defp parse_fields(<<c::utf8, rest::binary>>, fields, current, in_quotes) do
    parse_fields(rest, fields, current <> <<c::utf8>>, in_quotes)
  end

  def parse_enumerable(enum, opts \\ []) do
    skip_headers = Keyword.get(opts, :skip_headers, 1)
    enum
    |> Stream.map(&String.trim_trailing(&1, "\n"))
    |> Stream.reject(&(&1 == ""))
    |> Stream.drop(skip_headers)
    |> Stream.map(&parse_line/1)
  end
end

csv_data = """
name,email,age,city
Alice,alice@example.com,30,Madrid
Bob,bob@example.com,25,Barcelona
Carol,"carol, smith",35,Valencia
Dave,dave@example.com,28,"New York, NY"
"""

IO.puts("CSV de ejemplo:")
IO.puts(csv_data)

# =============================================================================
# SECCIÓN 2: parse_string — parsear CSV desde memoria
# =============================================================================
#
# MyCSV.parse_string/2 convierte un string CSV a una lista de listas.
# Cada elemento de la lista interior es un campo como string.
# Por defecto OMITE la primera fila (asumiendo que es header).
#
# Para incluir el header: parse_string(str, skip_headers: 0)

IO.puts("=== Sección 2: parse_string ===\n")

IO.puts("""
# TODO 2: En tu módulo, usando MyCSV definido arriba:
#
#   rows = MyCSV.parse_string(csv_data)
#   # rows es una lista de listas de strings
#
#   Luego:
#   A) Cuenta cuántas filas hay (sin el header)
#   B) Extrae los nombres (primera columna) de todas las filas
#   C) Filtra usuarios mayores de 27 años (columna "age", convertir a int)
#   D) Crea un mapa %{nombre => ciudad} para cada usuario
#
# Código de ejemplo (para ejecutar en tu proyecto):
""")

# Simulación del comportamiento esperado
rows = SimulatedCSV.parse_string(csv_data)

IO.puts("A) Filas parseadas: #{length(rows)}")

names = Enum.map(rows, fn [name | _] -> name end)
IO.puts("B) Nombres: #{inspect(names)}")

older_than_27 = Enum.filter(rows, fn [_, _, age, _] ->
  String.to_integer(age) > 27
end)
IO.puts("C) Mayores de 27: #{inspect(Enum.map(older_than_27, &hd/1))}")

name_city_map = Map.new(rows, fn [name, _, _, city] -> {name, city} end)
IO.puts("D) Nombre => Ciudad: #{inspect(name_city_map)}")
IO.puts("")

# =============================================================================
# SECCIÓN 3: Parser TSV (Tab-Separated Values)
# =============================================================================
#
# Cambiar el separador es trivial con NimbleCSV:
#   NimbleCSV.define(MyTSV, separator: "\t", escape: "\"")
# Los archivos TSV son comunes en exports de bases de datos y Excel.

IO.puts("=== Sección 3: Custom Delimiter — TSV ===\n")

IO.puts("""
# TODO 3: Define MyTSV con separator: "\\t" y parsea este contenido:
#
tsv_data = \"\"\"
product\\tprice\\tstock
Widget\\t9.99\\t100
Gadget\\t24.99\\t50
Gizmo\\t4.99\\t200
\"\"\"
#
#   A) Parsea el TSV y muestra los productos
#   B) Filtra productos con stock > 75
#   C) Calcula el total de inventario (price * stock para cada uno)
#
# En tu proyecto Mix:
# rows = MyTSV.parse_string(tsv_data)
""")

# Simulación de TSV
defmodule SimulatedTSV do
  def parse_string(string, opts \\ []) do
    skip = Keyword.get(opts, :skip_headers, 1)
    string
    |> String.split("\n", trim: true)
    |> Enum.drop(skip)
    |> Enum.map(&String.split(&1, "\t"))
  end
end

tsv_data = "product\tprice\tstock\nWidget\t9.99\t100\nGadget\t24.99\t50\nGizmo\t4.99\t200\n"
tsv_rows = SimulatedTSV.parse_string(tsv_data)

IO.puts("TSV parseado:")
Enum.each(tsv_rows, fn [product, price, stock] ->
  IO.puts("  #{product}: $#{price} (stock: #{stock})")
end)

high_stock = Enum.filter(tsv_rows, fn [_, _, stock] ->
  String.to_integer(stock) > 75
end)
IO.puts("Stock > 75: #{inspect(Enum.map(high_stock, &hd/1))}")

total = Enum.sum(Enum.map(tsv_rows, fn [_, price, stock] ->
  String.to_float(price) * String.to_integer(stock)
end))
IO.puts("Valor total de inventario: $#{Float.round(total, 2)}\n")

# =============================================================================
# SECCIÓN 4: Stream CSV — procesamiento lazy de archivos grandes
# =============================================================================
#
# Para archivos CSV grandes (millones de filas), parse_string carga TODO en memoria.
# MyCSV.parse_stream/2 procesa el archivo en modo streaming:
#   File.stream!("huge.csv") |> MyCSV.parse_stream() |> Stream.take(100) |> Enum.to_list()
#
# Esto usa memoria O(1) — solo mantiene la fila actual en memoria.

IO.puts("=== Sección 4: Stream CSV ===\n")

IO.puts("""
# TODO 4: En tu proyecto Mix, crea un archivo grande y procésalo en streaming.
#
# Ejemplo de archivo "data.csv" (generado):
#   File.write!("data.csv", "id,value,category\\n")
#   for i <- 1..100_000 do
#     File.write!("data.csv", "#{i},#{:rand.uniform(1000)},cat_#{rem(i, 5)}\\n", [:append])
#   end
#
# Procesar en streaming:
#   alias MyProject.MyCSV
#
#   stats = File.stream!("data.csv")
#     |> MyCSV.parse_stream()           # lazy: no carga todo en memoria
#     |> Stream.map(fn [id, value, cat] ->
#          {String.to_integer(id),
#           String.to_integer(value),
#           cat}
#        end)
#     |> Stream.filter(fn {_, _, cat} -> cat == "cat_0" end)
#     |> Stream.take(100)               # solo primeros 100 de cat_0
#     |> Enum.to_list()
#
#   IO.puts("Filas de cat_0: #{length(stats)}")
""")

# Simulación de streaming con una "fuente" de datos grande
IO.puts("Simulando Stream CSV con 1000 filas en memoria:")

header = "id,value,category"
csv_lines = [header | for(i <- 1..1000, do: "#{i},#{:rand.uniform(100)},cat_#{rem(i, 5)}")]

stream_result =
  csv_lines
  |> SimulatedCSV.parse_enumerable()
  |> Stream.map(fn [id, value, cat] ->
    {String.to_integer(id), String.to_integer(value), cat}
  end)
  |> Stream.filter(fn {_, _, cat} -> cat == "cat_0" end)
  |> Stream.take(5)
  |> Enum.to_list()

IO.puts("Primeros 5 de cat_0:")
Enum.each(stream_result, fn {id, val, cat} ->
  IO.puts("  ID #{id}: #{val} (#{cat})")
end)
IO.puts("")

# =============================================================================
# SECCIÓN 5: NimbleParsec — introducción a combinadores de parser
# =============================================================================
#
# NimbleParsec permite definir parsers como combinaciones de piezas simples.
# Es como escribir una gramática BNF pero en Elixir.
# Los parsers se compilan a código eficiente en tiempo de compilación.
#
# Combinadores básicos:
#   integer(min: n)          — parsea un entero de mínimo n dígitos
#   utf8_char([?a..?z])      — parsea un carácter específico
#   string("keyword")        — parsea una string literal
#   repeat/3                 — repite un combinador
#   optional/2               — hace un combinador opcional
#   ignore/2                 — parsea pero descarta
#   concat/3                 — concatena dos parsers

IO.puts("=== Sección 5: NimbleParsec ===\n")

IO.puts("""
# TODO 5: En tu proyecto Mix con {:nimble_parsec, "~> 1.4"}:
#
# defmodule MyParser do
#   import NimbleParsec
#
#   # Parser de número entero (positivo)
#   defparsec :number, integer(min: 1)
#
#   # Parser de identificador (letras y guión_bajo)
#   identifier =
#     ascii_char([?a..?z, ?A..?Z, ?_])
#     |> repeat(ascii_char([?a..?z, ?A..?Z, ?0..?9, ?_]))
#     |> reduce({List, :to_string, []})
#
#   defparsec :identifier, identifier
#
#   # Parser de assignment: "key = value"
#   whitespace = ignore(repeat(ascii_char([?\\s, ?\\t])))
#
#   defparsec :assignment,
#     identifier
#     |> concat(whitespace)
#     |> ignore(string("="))
#     |> concat(whitespace)
#     |> concat(integer(min: 1))
#
# end
#
# Uso:
#   {:ok, [42], "", %{}, _, _} = MyParser.number("42")
#   {:ok, ["my_var"], "", %{}, _, _} = MyParser.identifier("my_var")
#   {:ok, ["timeout", 5000], "", %{}, _, _} = MyParser.assignment("timeout = 5000")
""")

# Simulación de NimbleParsec para demostración standalone
defmodule SimpleParsec do
  @moduledoc "Simulación básica del comportamiento de NimbleParsec"

  def parse_number(input) do
    case Integer.parse(input) do
      {n, rest} -> {:ok, [n], rest, %{}, {1, 0}, byte_size(input) - byte_size(rest)}
      :error    -> {:error, "expected integer", input, %{}, {1, 0}, 0}
    end
  end

  def parse_identifier(input) do
    {id, rest} = consume_identifier(input, "")
    if id == "",
      do: {:error, "expected identifier", input, %{}, {1, 0}, 0},
      else: {:ok, [id], rest, %{}, {1, 0}, byte_size(id)}
  end

  defp consume_identifier("", acc), do: {acc, ""}
  defp consume_identifier(<<c, rest::binary>>, acc)
       when c in ?a..?z or c in ?A..?Z or c == ?_ or (acc != "" and c in ?0..?9) do
    consume_identifier(rest, acc <> <<c>>)
  end
  defp consume_identifier(rest, acc), do: {acc, rest}

  def parse_assignment(input) do
    input_stripped = String.trim(input)
    with {:ok, [key], rest, _, _, _} <- parse_identifier(input_stripped),
         rest2 = String.trim(rest),
         <<"=", rest3::binary>> <- rest2,
         rest4 = String.trim(rest3),
         {:ok, [value], "", _, _, _} <- parse_number(rest4) do
      {:ok, [key, value], "", %{}, {1, 0}, byte_size(input)}
    else
      _ -> {:error, "invalid assignment", input, %{}, {1, 0}, 0}
    end
  end
end

IO.puts("NimbleParsec simulado — combinadores básicos:")

# Parsear número
{:ok, [num], _, _, _, _} = SimpleParsec.parse_number("42 rest")
IO.puts("parse_number(\"42\"): #{num}")

# Parsear identificador
{:ok, [id], _, _, _, _} = SimpleParsec.parse_identifier("my_var extra")
IO.puts("parse_identifier(\"my_var\"): #{id}")

# Parsear assignment
{:ok, [key, val], _, _, _, _} = SimpleParsec.parse_assignment("timeout = 5000")
IO.puts("parse_assignment(\"timeout = 5000\"): key=#{key}, val=#{val}")

# Error
result = SimpleParsec.parse_number("not_a_number")
IO.puts("parse_number(\"not_a_number\"): #{inspect(result)}\n")

# =============================================================================
# SECCIÓN 6: NimbleParsec — parser de expresiones simples
# =============================================================================

IO.puts("=== Sección 6: Parser de Expresiones ===\n")

IO.puts("""
# En tu proyecto Mix, un parser de expresiones aritméticas simples:
#
# defmodule ExprParser do
#   import NimbleParsec
#
#   whitespace = ignore(repeat(ascii_char([?\\s, ?\\t])))
#   number = integer(min: 1)
#
#   operator =
#     choice([
#       string("+"),
#       string("-"),
#       string("*"),
#       string("/")
#     ])
#
#   defparsec :expression,
#     number
#     |> concat(whitespace)
#     |> concat(operator)
#     |> concat(whitespace)
#     |> concat(number)
#
# end
#
# Uso:
#   ExprParser.expression("10 + 5")  → {:ok, [10, "+", 5], ...}
#   ExprParser.expression("3 * 7")   → {:ok, [3, "*", 7], ...}
""")

# Simulación
defmodule ExprParser do
  def expression(input) do
    with {n1, rest1} when rest1 != "" <- Integer.parse(String.trim(input)),
         rest2 = String.trim(rest1),
         <<op, rest3::binary>> when op in [?+, ?-, ?*, ?/] <- rest2,
         {n2, ""} <- Integer.parse(String.trim(rest3)) do
      {:ok, [n1, <<op>>, n2], "", %{}, {1, 0}, byte_size(input)}
    else
      _ -> {:error, "invalid expression", input, %{}, {1, 0}, 0}
    end
  end

  def eval([a, "+", b]), do: a + b
  def eval([a, "-", b]), do: a - b
  def eval([a, "*", b]), do: a * b
  def eval([a, "/", b]) when b != 0, do: div(a, b)
end

IO.puts("Simulación de parser de expresiones:")

for expr <- ["10 + 5", "3 * 7", "20 - 8", "15 / 3"] do
  {:ok, parts, _, _, _, _} = ExprParser.expression(expr)
  result = ExprParser.eval(parts)
  IO.puts("  #{expr} = #{result}")
end
IO.puts("")

# =============================================================================
# SECCIÓN 7: Procesamiento de CSV con transformaciones
# =============================================================================
#
# El patrón real de NimbleCSV en producción:
# parsear → transformar → agregar

IO.puts("=== Sección 7: Pipeline CSV Completo ===\n")

sales_csv = """
date,product,quantity,unit_price,region
2024-01-15,Widget,10,9.99,NORTH
2024-01-15,Gadget,5,24.99,SOUTH
2024-01-16,Widget,8,9.99,EAST
2024-01-16,Gizmo,20,4.99,NORTH
2024-01-17,Gadget,3,24.99,WEST
2024-01-17,Widget,15,9.99,SOUTH
2024-01-18,Gizmo,12,4.99,EAST
"""

IO.puts("Sales CSV:")
IO.puts(sales_csv)

rows = SimulatedCSV.parse_string(sales_csv)

# Transformar a mapas tipados
sales =
  rows
  |> Enum.map(fn [date, product, qty, price, region] ->
    %{
      date: date,
      product: product,
      quantity: String.to_integer(qty),
      unit_price: String.to_float(price),
      region: String.to_atom(String.downcase(region)),
      total: String.to_integer(qty) * String.to_float(price)
    }
  end)

# Aggregaciones
total_revenue = sales |> Enum.map(& &1.total) |> Enum.sum()
by_product = sales |> Enum.group_by(& &1.product)
by_region = sales |> Enum.group_by(& &1.region)

IO.puts("Total revenue: $#{Float.round(total_revenue, 2)}")

IO.puts("\nRevenue por producto:")
Enum.each(by_product, fn {product, rows} ->
  rev = rows |> Enum.map(& &1.total) |> Enum.sum()
  IO.puts("  #{product}: $#{Float.round(rev, 2)}")
end)

IO.puts("\nRevenue por región:")
Enum.each(by_region, fn {region, rows} ->
  rev = rows |> Enum.map(& &1.total) |> Enum.sum()
  IO.puts("  #{region}: $#{Float.round(rev, 2)}")
end)
IO.puts("")

# =============================================================================
# SECCIÓN 8: Generación de CSV
# =============================================================================
#
# NimbleCSV también puede GENERAR CSV, no solo parsearlo.
# MyCSV.dump_to_iodata/1 convierte lista de listas a CSV.

IO.puts("=== Sección 8: Generar CSV ===\n")

IO.puts("""
# En tu proyecto con NimbleCSV:
#   header = [["id", "name", "email", "created_at"]]
#   rows = for user <- users do
#     [user.id, user.name, user.email, DateTime.to_string(user.created_at)]
#   end
#   csv_content = MyCSV.dump_to_iodata(header ++ rows)
#   File.write!("export.csv", csv_content)
#
#   # O en streaming:
#   MyCSV.dump_to_stream(Stream.concat(header, rows))
#   |> Stream.into(File.stream!("huge_export.csv"))
#   |> Stream.run()
""")

# Simulación de generación CSV
defmodule CsvGenerator do
  def to_csv(rows, separator \\ ",") do
    rows
    |> Enum.map(fn row ->
      row
      |> Enum.map(fn field ->
        field = to_string(field)
        if String.contains?(field, [separator, "\n", "\""]) do
          "\"#{String.replace(field, "\"", "\\\"")}\""
        else
          field
        end
      end)
      |> Enum.join(separator)
    end)
    |> Enum.join("\n")
  end
end

export_data = [
  ["id", "name", "total"],
  [1, "Alice", 1250.50],
  [2, "Bob", 875.25],
  [3, "Carol, Smith", 2100.00]
]

generated_csv = CsvGenerator.to_csv(export_data)
IO.puts("CSV generado:")
IO.puts(generated_csv)
IO.puts("")

# =============================================================================
# SECCIÓN 9: TRY IT YOURSELF
# =============================================================================
#
# Implementa un parser de formato INI usando las técnicas aprendidas.
#
# Formato INI a parsear:
#   [database]
#   host = localhost
#   port = 5432
#   name = myapp_prod
#
#   [cache]
#   host = redis.local
#   port = 6379
#   ttl = 3600
#
#   [logging]
#   level = info
#   file = /var/log/myapp.log
#
# El parser debe:
#
# IniParser.parse/1 recibe un string INI y retorna:
#   {:ok, %{
#     "database" => %{"host" => "localhost", "port" => "5432", "name" => "myapp_prod"},
#     "cache"    => %{"host" => "redis.local", "port" => "6379", "ttl" => "3600"},
#     "logging"  => %{"level" => "info", "file" => "/var/log/myapp.log"}
#   }}
#   {:error, reason}
#
# Reglas de parsing:
#   - Líneas que empiezan con # son comentarios (ignorar)
#   - Líneas vacías son ignoradas
#   - [section] define una nueva sección
#   - key = value define una entrada (trim whitespace en key y value)
#   - Una key sin sección activa retorna {:error, "key before section: key"}
#
# IniParser.get/3 recibe (parsed, section, key, default \\ nil):
#   IniParser.get(config, "database", "port") → "5432"
#   IniParser.get(config, "database", "missing", "default") → "default"
#
# En un proyecto Mix real, esto se implementaría con NimbleParsec:
#   section_header = ignore(string("[")) |> utf8_string([?a..?z, ?_], min: 1) |> ignore(string("]"))
#   key_value = identifier |> ignore(whitespace) |> ignore(string("=")) |> ignore(whitespace) |> value_string
#   ...

IO.puts("=== SECCIÓN 9: Try It Yourself — Parser INI ===\n")

ini_content = """
# Configuración de la aplicación
# Generado automáticamente

[database]
host = localhost
port = 5432
name = myapp_prod
pool_size = 10

[cache]
host = redis.local
port = 6379
ttl = 3600

# Logging config
[logging]
level = info
file = /var/log/myapp.log
max_size = 100MB
"""

IO.puts("INI content a parsear:")
IO.puts(ini_content)

defmodule IniParser do
  @doc """
  Parsea un string en formato INI y retorna un mapa anidado.
  Secciones como keys externas, key=value como entries internas.
  """
  def parse(content) do
    # Tu implementación aquí
    # Pista: String.split(content, "\\n") + Enum.reduce para acumular estado
    # Estado: {sección_actual, mapa_acumulado}
    {:ok, %{}}
  end

  @doc "Accede a un valor del INI parseado con fallback opcional."
  def get(parsed, section, key, default \\ nil) do
    # Tu implementación aquí
    default
  end
end

IO.puts("--- Tests de IniParser ---")
case IniParser.parse(ini_content) do
  {:ok, config} ->
    IO.puts("Secciones: #{inspect(Map.keys(config))}")
    IO.puts("DB host: #{IniParser.get(config, "database", "host")}")
    IO.puts("DB port: #{IniParser.get(config, "database", "port")}")
    IO.puts("Cache TTL: #{IniParser.get(config, "cache", "ttl")}")
    IO.puts("Log level: #{IniParser.get(config, "logging", "level")}")
    IO.puts("Missing (default): #{IniParser.get(config, "database", "missing", "5000")}")
  {:error, reason} ->
    IO.puts("Error: #{reason}")
end

# Test de error: key antes de sección
ini_invalid = "orphan_key = value\n[section]\nkey = val\n"
IO.puts("\nINI inválido (key antes de sección):")
IO.puts(inspect(IniParser.parse(ini_invalid)))

# =============================================================================
# ERRORES COMUNES
# =============================================================================
IO.puts("\n=== Errores Comunes ===\n")
IO.puts("""
1. Olvidar skip_headers al parsear con NimbleCSV:
   Por defecto parse_string/1 omite la PRIMERA fila (asume que es header).
   Si parseas datos sin header, usa parse_string(data, skip_headers: 0).
   Si quieres el header como datos, usa skip_headers: 0.

2. Asumir que todos los campos son strings:
   NimbleCSV siempre retorna strings. Debes convertir tú mismo:
     String.to_integer(age_str)
     String.to_float(price_str)
     Date.from_iso8601!(date_str)

3. No manejar campos con comillas o comas:
   "Carol, Smith" en CSV está entrecomillado: \\"Carol, Smith\\".
   NimbleCSV lo maneja automáticamente — no hagas split manual en comas.

4. Usar File.read! en lugar de File.stream! para archivos grandes:
   File.read! carga TODO el archivo en memoria.
   File.stream! + parse_stream hace streaming línea a línea (O(1) memoria).

5. NimbleParsec: no compilar el módulo antes de usarlo:
   Los parsers se generan en tiempo de compilación con defparsec.
   No puedes definir y usar en el mismo módulo fuera de un proyecto Mix
   — necesitas mezclar la compilación.

6. Campos vacíos en CSV:
   La fila "Alice,,30" producirá ["Alice", "", "30"].
   El campo vacío es "" (string vacío), no nil.
   Maneja esto explícitamente: if field == "", do: nil, else: field
""")
