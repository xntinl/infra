# 11. Streams y Lazy Evaluation

**Difficulty**: Intermedio

---

## Prerequisites

- Enum.map/filter/reduce
- Comprensión de evaluación ansiosa (eager)
- Ficheros y IO básico en Elixir
- Ejercicio 10: Comprehensions Avanzadas

---

## Learning Objectives

1. Entender la diferencia entre evaluación eager (Enum) y lazy (Stream)
2. Construir pipelines de Stream sin consumir memoria hasta el final
3. Usar `Stream.iterate/2` y `Stream.cycle/1` para streams infinitos
4. Procesar ficheros grandes línea a línea con `Stream.resource/3` y `File.stream!/1`
5. Combinar streams con `Stream.flat_map/2` y `Stream.transform/3`
6. Saber cuándo elegir Stream sobre Enum y viceversa

---

## Concepts

### Eager vs Lazy

Con `Enum`, cada operación materializa una colección completa en memoria:

```elixir
# Eager — crea 3 listas intermedias
[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
|> Enum.map(&(&1 * 2))      # [2, 4, 6, 8, 10, 12, 14, 16, 18, 20]
|> Enum.filter(&(&1 > 10))  # [12, 14, 16, 18, 20]
|> Enum.take(3)             # [12, 14, 16]
```

Con `Stream`, las operaciones se encadenan como una descripción y solo se ejecutan cuando se necesita un resultado:

```elixir
# Lazy — no hace nada hasta el Enum.to_list/take final
[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
|> Stream.map(&(&1 * 2))
|> Stream.filter(&(&1 > 10))
|> Enum.take(3)   # <- aquí se ejecuta todo de una vez, elemento a elemento
# [12, 14, 16]
```

La diferencia de rendimiento es enorme cuando trabajas con colecciones grandes o potencialmente infinitas.

### Funciones principales de Stream

```elixir
# Transformación
Stream.map(stream, fun)
Stream.flat_map(stream, fun)   # fun devuelve enumerable, se aplana
Stream.filter(stream, pred)
Stream.reject(stream, pred)

# Limitación
Stream.take(stream, n)         # toma los primeros n elementos
Stream.take_while(stream, pred)
Stream.drop(stream, n)
Stream.drop_while(stream, pred)

# Generación
Stream.iterate(inicial, fun)   # fun(inicial), fun(fun(inicial)), ...
Stream.repeatedly(fun)         # llama fun infinitamente
Stream.cycle(enumerable)       # repite el enumerable sin fin

# Combinación
Stream.zip(s1, s2)
Stream.concat(s1, s2)
Stream.with_index(stream)

# Control
Stream.chunk_every(stream, n)
Stream.each(stream, fun)       # side effects, devuelve stream original
```

### Stream.iterate — Streams Infinitos

```elixir
# Fibonacci infinito
fib = Stream.iterate({0, 1}, fn {a, b} -> {b, a + b} end)
      |> Stream.map(&elem(&1, 0))

Enum.take(fib, 10)
# [0, 1, 1, 2, 3, 5, 8, 13, 21, 34]

# Números naturales
naturales = Stream.iterate(1, &(&1 + 1))
Enum.take(naturales, 5)  # [1, 2, 3, 4, 5]

# Potencias de 2
potencias = Stream.iterate(1, &(&1 * 2))
Enum.take(potencias, 8)  # [1, 2, 4, 8, 16, 32, 64, 128]
```

### File.stream! — Ficheros Grandes

```elixir
# Sin cargar todo en memoria — línea a línea
"datos.csv"
|> File.stream!()
|> Stream.map(&String.trim/1)
|> Stream.filter(&(String.length(&1) > 0))
|> Stream.map(&parsear_linea/1)
|> Enum.each(&insertar_en_db/1)

# chunk_every para procesar en batches
"datos.csv"
|> File.stream!()
|> Stream.map(&String.trim/1)
|> Stream.chunk_every(1000)    # procesa 1000 líneas a la vez
|> Enum.each(&insertar_batch/1)
```

### Stream.resource — Streams Personalizados

`Stream.resource/3` es la primitiva de bajo nivel para crear streams desde recursos externos:

```elixir
# Estructura: Stream.resource(iniciar_fn, siguiente_fn, limpiar_fn)
# - iniciar_fn: devuelve el estado inicial del recurso
# - siguiente_fn: recibe estado, devuelve {[elementos], nuevo_estado} o {:halt, estado}
# - limpiar_fn: libera el recurso al terminar

Stream.resource(
  fn -> File.open!("datos.csv", [:read]) end,   # abrir fichero
  fn file ->
    case IO.read(file, :line) do
      :eof  -> {:halt, file}
      linea -> {[String.trim(linea)], file}
    end
  end,
  fn file -> File.close(file) end               # cerrar siempre
)
|> Enum.take(5)
```

---

## Exercises

### Exercise 1: Procesar CSV de 1M líneas sin cargar en RAM

Implementa un procesador de fichero CSV que lea, valide, transforme y agregue datos en streaming.

```elixir
# Archivo: lib/csv_streamer.ex

defmodule CSVStreamer do
  @moduledoc """
  Procesador de CSV gigante usando Stream para O(1) de memoria.

  Formato del CSV (con cabecera):
  id,producto,categoria,cantidad,precio,fecha
  1,Silla,Muebles,10,99.50,2026-01-15
  2,Mesa,Muebles,3,249.00,2026-01-16
  ...
  """

  @doc """
  Devuelve un Stream de líneas del fichero, descartando la cabecera y líneas vacías.
  No carga el fichero en memoria.
  """
  def leer_stream(path) do
    # TODO: File.stream!/1 -> Stream.drop(1) para saltar cabecera
    # -> Stream.map para String.trim
    # -> Stream.reject para líneas vacías (String.length == 0)
  end

  @doc """
  Parsea una línea CSV a un mapa.
  Devuelve {:ok, mapa} o {:error, razon}.
  """
  def parsear_linea(linea) do
    case String.split(linea, ",") do
      [id, producto, categoria, cantidad_str, precio_str, fecha] ->
        with {cantidad, ""} <- Integer.parse(cantidad_str),
             {precio, ""}   <- Float.parse(precio_str) do
          {:ok, %{
            id:        id,
            producto:  producto,
            categoria: categoria,
            cantidad:  cantidad,
            precio:    precio,
            fecha:     fecha
          }}
        else
          _ -> {:error, "Formato numérico inválido en: #{linea}"}
        end

      _ ->
        {:error, "Número de columnas incorrecto: #{linea}"}
    end
  end

  @doc """
  Stream de mapas válidos. Las líneas con error se descartan (y se loguean a stderr).
  """
  def stream_valido(path) do
    path
    |> leer_stream()
    |> Stream.map(&parsear_linea/1)
    |> Stream.each(fn
      {:error, razon} -> IO.warn("Línea descartada: #{razon}")
      _               -> :ok
    end)
    |> Stream.filter(fn
      # TODO: pasar solo los {:ok, _}
    end)
    |> Stream.map(fn
      # TODO: extraer el mapa del {:ok, mapa}
    end)
  end

  @doc """
  Calcula el total de ventas (suma de cantidad * precio) por categoría.
  Lee el fichero UNA sola vez. Devuelve %{categoria => total}.
  """
  def totales_por_categoria(path) do
    path
    |> stream_valido()
    |> Enum.reduce(%{}, fn %{categoria: cat, cantidad: c, precio: p}, acc ->
      # TODO: actualizar acc[cat] sumando c * p
      # Pista: Map.update(acc, cat, c * p, fn prev -> prev + c * p end)
    end)
  end

  @doc """
  Devuelve las primeras n filas de una categoría específica sin leer el fichero entero.
  """
  def primeras_de_categoria(path, categoria, n) do
    path
    |> stream_valido()
    |> Stream.filter(fn %{categoria: cat} ->
      # TODO: filtrar por categoría
    end)
    |> Enum.take(n)
    # Enum.take/2 para el stream cuando ya tiene n elementos — no lee más del fichero
  end

  @doc """
  Genera un fichero de reporte CSV con los totales por categoría.
  Escribe línea a línea sin construir el string completo en memoria.
  """
  def escribir_reporte(totales, output_path) do
    # totales es un mapa %{categoria => total}
    lineas =
      [
        "categoria,total_euros\n"
        | totales
          |> Enum.sort_by(fn {_cat, total} -> total end, :desc)
          |> Enum.map(fn {cat, total} ->
            # TODO: formatear como "#{cat},#{:erlang.float_to_binary(total, [decimals: 2])}\n"
          end)
      ]

    # TODO: Usar File.write!/2 con el contenido construido como IOList
    # Para ficheros muy grandes, usar Stream.into/2 o File.open + IO.write en stream
    File.write!(output_path, lineas)
  end
end
```

**Script de generación de datos de prueba** (crea un CSV de prueba):

```elixir
# En iex para generar datos de prueba:
defmodule CSVGen do
  def generar(path, n_filas) do
    categorias = ["Muebles", "Hogar", "Electrónica", "Ropa", "Libros"]
    productos  = ["Item A", "Item B", "Item C", "Item D", "Item E"]

    cabecera = "id,producto,categoria,cantidad,precio,fecha\n"
    filas =
      1..n_filas
      |> Stream.map(fn i ->
        cat = Enum.at(categorias, rem(i, 5))
        prod = Enum.at(productos, rem(i, 5))
        cant = rem(i, 10) + 1
        precio = 10.0 + rem(i, 100)
        fecha = "2026-01-#{String.pad_leading(to_string(rem(i, 28) + 1), 2, "0")}"
        "#{i},#{prod},#{cat},#{cant},#{precio},#{fecha}\n"
      end)
      |> Enum.to_list()

    File.write!(path, [cabecera | filas])
    IO.puts("Generadas #{n_filas} filas en #{path}")
  end
end

CSVGen.generar("/tmp/ventas.csv", 100_000)
```

**Verificación esperada:**

```elixir
# Con el fichero generado:
totales = CSVStreamer.totales_por_categoria("/tmp/ventas.csv")
# %{"Muebles" => 123456.0, "Hogar" => 98765.0, ...}

CSVStreamer.primeras_de_categoria("/tmp/ventas.csv", "Muebles", 3)
# [%{id: ..., categoria: "Muebles", ...}, ...]  <- solo 3 elementos

CSVStreamer.escribir_reporte(totales, "/tmp/reporte.csv")
# Escribe fichero CSV con categorías ordenadas por total desc
```

---

### Exercise 2: Fibonacci y Streams Infinitos

Implementa varios streams infinitos útiles y funciones sobre ellos.

```elixir
# Archivo: lib/infinite_streams.ex

defmodule InfiniteStreams do
  @doc """
  Stream infinito de números de Fibonacci: 0, 1, 1, 2, 3, 5, 8, 13, ...
  """
  # TODO: Stream.iterate con estado {a, b} = {0, 1}
  # Cada paso: {b, a + b}
  # Extraer el primer elemento de la tupla con Stream.map
  def fibonacci do
  end

  @doc """
  Stream infinito de números primos usando criba de Eratóstenes incremental.
  Enfoque simplificado: para cada n, verificar si es primo.
  """
  # TODO: Stream.iterate(2, &(&1 + 1)) — todos los enteros desde 2
  # Stream.filter con es_primo?/1
  def primos do
    Stream.iterate(2, &(&1 + 1))
    |> Stream.filter(&es_primo?/1)
  end

  # TODO: Implementar es_primo?/1
  # Un número n es primo si no tiene divisores entre 2 y floor(sqrt(n))
  # Pista: Enum.all?(2..trunc(:math.sqrt(n)), fn d -> rem(n, d) != 0 end)
  # Caso base: n < 2 -> false
  defp es_primo?(n) when n < 2, do: false
  defp es_primo?(2),             do: true
  defp es_primo?(n) do
  end

  @doc """
  Stream infinito que cicla sobre una lista de colores.
  """
  # TODO: Stream.cycle/1 sobre la lista de colores
  def semaforo_colores do
    Stream.cycle([:rojo, :amarillo, :verde])
  end

  @doc """
  Devuelve los primeros n números de Fibonacci mayores que min_value.
  """
  # TODO: Stream.drop_while + Enum.take
  def fibonacci_mayores_que(min_value, n) do
  end

  @doc """
  Devuelve el N-ésimo número primo (1-indexed).
  """
  # TODO: Stream.drop + Stream.take o Enum.at
  def primo_n(n) when n > 0 do
  end

  @doc """
  Zip de fibonacci con primos: [{fib_n, primo_n}] para los primeros n pares.
  """
  # TODO: Stream.zip + Enum.take
  def fib_primo_pairs(n) do
  end

  @doc """
  Genera un stream de "ventanas deslizantes" de tamaño window sobre otro stream.
  Ej: ventana([1,2,3,4,5], 3) -> [[1,2,3],[2,3,4],[3,4,5]]
  """
  # TODO: Usar Stream.chunk_every(stream, window, 1, :discard)
  def ventanas(stream, window) do
  end

  @doc """
  Detecta si una secuencia de Fibonacci tiene un patrón de suma de dígitos
  repetido. Devuelve el primer n donde sum_digits(fib(n)) == target.
  """
  # TODO: fibonacci() |> Stream.with_index |> Stream.find
  # sum_digits: convertir a string, split en chars, parsear y sumar
  def fibonacci_con_suma_digitos(target) do
  end
end
```

**Verificación esperada:**

```elixir
Enum.take(InfiniteStreams.fibonacci(), 10)
# [0, 1, 1, 2, 3, 5, 8, 13, 21, 34]

Enum.take(InfiniteStreams.primos(), 10)
# [2, 3, 5, 7, 11, 13, 17, 19, 23, 29]

InfiniteStreams.semaforo_colores() |> Enum.take(7)
# [:rojo, :amarillo, :verde, :rojo, :amarillo, :verde, :rojo]

InfiniteStreams.fibonacci_mayores_que(100, 5)
# [144, 233, 377, 610, 987]

InfiniteStreams.primo_n(10)
# 29

InfiniteStreams.fib_primo_pairs(5)
# [{0,2},{1,3},{1,5},{2,7},{3,11}]

InfiniteStreams.ventanas(1..6, 3) |> Enum.to_list()
# [[1,2,3],[2,3,4],[3,4,5],[4,5,6]]

InfiniteStreams.fibonacci_con_suma_digitos(9)
# devuelve {valor_fib, indice} donde los dígitos suman 9
```

---

### Exercise 3: Merge de Múltiples Streams

Implementa un sistema de merge e intercalado de streams, útil para combinar fuentes de datos concurrentes.

```elixir
# Archivo: lib/stream_merger.ex

defmodule StreamMerger do
  @doc """
  Intercala elementos de múltiples streams en round-robin.
  Termina cuando el stream más corto se agota.

  Ej: interleave([[1,2,3],[a,b,c],[x,y,z]]) -> [1,a,x, 2,b,y, 3,c,z]
  """
  # TODO: Stream.zip_with/2 para zipar los streams
  # zip_with devuelve una función que combina los elementos en tupla o lista
  # Luego Stream.flat_map para aplanar
  def interleave(streams) do
    streams
    |> Stream.zip_with(fn elementos -> elementos end)
    |> Stream.flat_map(fn elementos ->
      # TODO: convertir la lista de elementos en una lista (ya lo es)
    end)
  end

  @doc """
  Merge de streams ordenados — asume que cada stream está ordenado.
  Devuelve un stream con todos los elementos en orden.

  Solo funciona con streams finitos (materializa en Enum.sort final).
  """
  # TODO: Stream.concat todos los streams, luego Enum.sort al final
  # Para merge verdaderamente lazy necesitaríamos un heap, aquí simplificamos
  def merge_sorted(streams) do
    streams
    |> Stream.concat()
    |> Enum.sort()
  end

  @doc """
  Combina dos streams aplicando una función binaria a cada par de elementos.
  Como Stream.zip pero con transformación.
  Ej: zip_map([1,2,3], [10,20,30], &+/2) -> [11, 22, 33]
  """
  # TODO: Stream.zip(s1, s2) |> Stream.map(fn {a, b} -> fun.(a, b) end)
  def zip_map(stream1, stream2, fun) do
  end

  @doc """
  Particiona un stream en dos según un predicado.
  Devuelve {stream_true, stream_false}.

  NOTA: Para streams lazy puros, la partición requiere materializar.
  Esta implementación hace Enum.split_with y devuelve las listas.
  """
  def partition(stream, pred) do
    # TODO: Enum.split_with/2
  end

  @doc """
  Toma streams de eventos de múltiples "sensores" y los normaliza
  a un formato común añadiendo el nombre del sensor.

  sensores = [
    {"sensor_1", stream_de_lecturas_1},
    {"sensor_2", stream_de_lecturas_2},
    ...
  ]

  Cada lectura es un número. El resultado es un stream de:
  %{sensor: nombre, valor: lectura, timestamp: monotonic_ms}
  """
  def normalizar_sensores(sensores) do
    sensores
    |> Enum.map(fn {nombre, stream} ->
      # TODO: Stream.map sobre el stream del sensor
      # Para cada lectura, construir el mapa con sensor, valor y timestamp
      # Pista: :erlang.monotonic_time(:millisecond)
      Stream.map(stream, fn lectura ->
      end)
    end)
    |> Stream.concat()
    # concat aplana la lista de streams en un solo stream secuencial
    # (no es un merge concurrente real, es secuencial)
  end

  @doc """
  Agrupa elementos consecutivos de un stream que cumplan el mismo predicado.
  chunk_by — si pred(elem) cambia de valor, empieza un nuevo chunk.

  Ej: agrupar_consecutivos([1,1,2,3,3,3,1], &(&1)) -> [[1,1],[2],[3,3,3],[1]]
  """
  # TODO: Stream.chunk_by/2 con la función dada
  def agrupar_consecutivos(stream, fun) do
  end

  @doc """
  Aplica una función acumuladora que puede emitir múltiples valores por elemento.
  Útil para "expandir" elementos del stream.

  Ej: expand([1,2,3], fn n -> 1..n end) -> [1, 1,2, 1,2,3]
  """
  # TODO: Stream.flat_map/2
  def expand(stream, fun) do
  end
end
```

**Verificación esperada:**

```elixir
# interleave
StreamMerger.interleave([[1, 2, 3], [:a, :b, :c], [:x, :y, :z]])
|> Enum.to_list()
# [1, :a, :x, 2, :b, :y, 3, :c, :z]

# merge_sorted
StreamMerger.merge_sorted([[1, 4, 7], [2, 5, 8], [3, 6, 9]])
# [1, 2, 3, 4, 5, 6, 7, 8, 9]

# zip_map
StreamMerger.zip_map([1, 2, 3], [10, 20, 30], &+/2)
|> Enum.to_list()
# [11, 22, 33]

StreamMerger.zip_map(
  InfiniteStreams.fibonacci(),
  InfiniteStreams.primos(),
  fn f, p -> f + p end
) |> Enum.take(5)
# [2, 4, 6, 9, 14]  (fib + primo para los primeros 5)

# partition
{pares, impares} = StreamMerger.partition(1..10, &(rem(&1, 2) == 0))
pares    # [2, 4, 6, 8, 10]
impares  # [1, 3, 5, 7, 9]

# agrupar_consecutivos
StreamMerger.agrupar_consecutivos([1, 1, 2, 3, 3, 3, 1], &(&1))
|> Enum.to_list()
# [[1, 1], [2], [3, 3, 3], [1]]

# expand
StreamMerger.expand([1, 2, 3], fn n -> 1..n end)
|> Enum.to_list()
# [1, 1, 2, 1, 2, 3]
```

---

## Common Mistakes

### 1. Olvidar que Stream no hace nada hasta el terminal

```elixir
# MAL — cree que esto imprime algo
stream = Stream.map([1, 2, 3], &IO.puts/1)  # solo crea la descripción

# BIEN — necesita un terminal (Enum.*) para ejecutar
Stream.map([1, 2, 3], &IO.puts/1) |> Stream.run()
# o
Stream.each([1, 2, 3], &IO.puts/1) |> Stream.run()
```

### 2. Crear stream infinito y olvidar limitarlo

```elixir
# MAL — bloquea para siempre (o hasta OOM)
Stream.iterate(1, &(&1 + 1)) |> Enum.to_list()

# BIEN — siempre limitar streams infinitos con take o similares
Stream.iterate(1, &(&1 + 1)) |> Enum.take(1000)
```

### 3. Usar Stream cuando Enum es suficiente

```elixir
# INNECESARIO — para listas pequeñas, Stream tiene overhead
small_list = [1, 2, 3, 4, 5]
result = small_list |> Stream.map(&(&1 * 2)) |> Enum.to_list()

# MEJOR — Enum directo para colecciones que caben en memoria
result = Enum.map(small_list, &(&1 * 2))
```

### 4. File.stream! sin gestión de errores

```elixir
# MAL — falla si el fichero no existe o no hay permisos
File.stream!("/ruta/que/no/existe")
|> Enum.each(&IO.puts/1)

# BIEN — verificar existencia o manejar la excepción
case File.stat("/ruta/fichero.csv") do
  {:ok, _} ->
    File.stream!("/ruta/fichero.csv") |> Enum.each(&IO.puts/1)
  {:error, reason} ->
    {:error, "No se puede abrir el fichero: #{reason}"}
end
```

### 5. Stream.resource sin limpiar el recurso

```elixir
# MAL — el fichero queda abierto si el stream se abandona a medias
Stream.resource(
  fn -> File.open!("datos.txt") end,
  fn f -> case IO.read(f, :line) do
    :eof -> {:halt, f}
    l    -> {[l], f}
  end end,
  fn _f -> :ok end   # <- no cierra el fichero!
)

# BIEN — siempre cerrar en la función de limpieza
Stream.resource(
  fn -> File.open!("datos.txt") end,
  fn f -> case IO.read(f, :line) do
    :eof -> {:halt, f}
    l    -> {[l], f}
  end end,
  fn f -> File.close(f) end   # <- siempre se ejecuta, incluso en errores
)
```

---

## Verification

```bash
# Generar fichero de prueba
iex -S mix
# > CSVGen.generar("/tmp/ventas_test.csv", 10_000)

# Ejecutar tests
mix test

# Verificar uso de memoria (no debe crecer con ficheros grandes)
mix run -e 'CSVStreamer.totales_por_categoria("/tmp/ventas_test.csv") |> IO.inspect()'
```

```elixir
# Smoke tests en iex:

# Exercise 1
CSVStreamer.primeras_de_categoria("/tmp/ventas_test.csv", "Muebles", 3)

# Exercise 2
InfiniteStreams.fibonacci() |> Enum.take(15)
InfiniteStreams.primos() |> Enum.take(20)
InfiniteStreams.primo_n(100)   # el primo 100

# Exercise 3
StreamMerger.interleave([1..3, 4..6, 7..9]) |> Enum.to_list()
StreamMerger.agrupar_consecutivos(
  Stream.iterate(1, fn x -> if rem(x, 3) == 0, do: x + 1, else: x + 1 end) |> Enum.take(9),
  &(rem(&1, 3))
)
```

---

## Summary

`Stream` es la herramienta correcta cuando los datos son demasiado grandes para caber en memoria, cuando la fuente es potencialmente infinita, o cuando el pipeline tiene múltiples etapas de filtrado que reducen drásticamente el volumen. La clave es que `Stream` construye una descripción del pipeline que solo se materializa en el momento del terminal (`Enum.to_list`, `Enum.take`, `Enum.reduce`, etc.), permitiendo al runtime procesar elemento a elemento sin colecciones intermedias.

## What's Next

**12. Macros y Quote/Unquote** — metaprogramación en Elixir: cómo el compilador representa el código y cómo escribir macros que generan código en tiempo de compilación.

## Resources

- [Elixir Docs — Stream](https://hexdocs.pm/elixir/Stream.html)
- [Stream Guide](https://elixir-lang.org/getting-started/enumerables-and-streams.html)
- [File.stream!/1](https://hexdocs.pm/elixir/File.html#stream!/1)
- [Elixir School — Streams](https://elixirschool.com/en/lessons/intermediate/concurrency)
- [José Valim — Lazy Elixir](https://www.youtube.com/watch?v=5TxA0KCGmSU)
