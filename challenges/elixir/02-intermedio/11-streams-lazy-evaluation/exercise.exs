# =============================================================================
# Ejercicio 11: Streams y Evaluación Lazy
# Difficulty: Intermedio
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - Enum.map/2, Enum.filter/2, Enum.take/2
# - Pipe operator (|>)
# - Rangos (1..n)
# - Comprehensions (ejercicio anterior)

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Crear pipelines lazy con Stream en lugar de Enum
# 2. Entender la diferencia entre evaluación eager y lazy
# 3. Procesar archivos línea a línea sin cargar todo en memoria
# 4. Usar Stream.cycle para secuencias infinitas
# 5. Elegir cuándo usar Stream vs Enum según el caso de uso

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# EAGER vs LAZY:
#
#   # EAGER (Enum): procesa TODA la colección en cada paso
#   1..1_000_000
#   |> Enum.map(&(&1 * 2))      # crea lista de 1M elementos
#   |> Enum.filter(&(rem(&1, 3) == 0))  # crea otra lista filtrada
#   |> Enum.take(5)             # finalmente toma 5
#
#   # LAZY (Stream): construye una "receta", ejecuta solo lo necesario
#   1..1_000_000
#   |> Stream.map(&(&1 * 2))    # solo describe la transformación
#   |> Stream.filter(&(rem(&1, 3) == 0))  # solo describe el filtro
#   |> Enum.take(5)             # AQUÍ se ejecuta, procesa ~12 elementos
#
# MATERIALIZACIÓN:
# Un Stream es una descripción de computación. Se ejecuta ("materializa")
# solo cuando llamas Enum.to_list/1, Enum.take/2, Enum.each/2, etc.
#
# FUNCIONES STREAM PRINCIPALES:
#   Stream.map/2      — transforma elementos
#   Stream.filter/2   — filtra elementos
#   Stream.take/2     — toma los primeros N (fuerza evaluación parcial)
#   Stream.drop/2     — omite los primeros N
#   Stream.take_while/2 — toma mientras condición es true
#   Stream.flat_map/2 — map + flatten
#   Stream.cycle/1    — repite la colección infinitamente
#   Stream.unfold/2   — genera secuencia desde estado inicial
#   Stream.iterate/2  — aplica función sucesivamente: f(seed), f(f(seed)), ...
#   Stream.resource/3 — maneja recursos con setup/teardown (ej: archivos)
#
# FILE STREAMING:
#
#   File.stream!("file.txt")   # retorna un Stream de líneas
#   |> Stream.map(&String.trim/1)
#   |> Enum.take(10)
#
# Ventaja: el archivo NO se carga completo en memoria. Ideal para archivos grandes.
#
# CUÁNDO USAR STREAM vs ENUM:
# - Stream: colecciones grandes/infinitas, pipelines largos, archivos
# - Enum: colecciones pequeñas, operación única, cuando necesitas el resultado completo

# =============================================================================
# Exercise 1: Pipeline lazy básico — sin materializar innecesariamente
# =============================================================================
#
# Completa la función `first_n_doubled/2` que toma los primeros `n` números
# del rango 1..1_000_000, los duplica, y retorna la lista.
#
# IMPORTANTE: usa Stream, no Enum, para el map. Solo Enum.to_list al final.
# Esto demuestra que no se procesan todos los millones de números.
#
# Ejemplo:
#   LazyPipeline.first_n_doubled(5)
#   # => [2, 4, 6, 8, 10]
#
#   LazyPipeline.first_n_doubled(3)
#   # => [2, 4, 6]

defmodule LazyPipeline do
  def first_n_doubled(n) do
    # TODO: Construye el pipeline lazy:
    # 1..1_000_000
    # |> Stream.map(...)
    # |> Stream.take(n)
    # |> Enum.to_list()
  end

  # Bonus: primeros N números que pasan el filtro dado
  def first_n_matching(n, filter_fn) do
    # TODO: 1..1_000_000 |> Stream.filter(filter_fn) |> Stream.take(n) |> Enum.to_list()
  end
end

# =============================================================================
# Exercise 2: File streaming — procesar líneas sin cargar todo en memoria
# =============================================================================
#
# Completa las funciones en FileProcessor.
# El módulo incluye un helper que genera un archivo temporal de prueba.
#
# `first_n_lines/2`: lee las primeras `n` líneas de un archivo,
#   aplicando String.trim/1 a cada una.
#
# `count_lines_containing/2`: cuenta cuántas líneas contienen
#   el substring dado.
#
# Tip: File.stream!/1 retorna un Stream de líneas (incluyen "\n").
#      Usa String.trim/1 para limpiar.
#      Usa String.contains?/2 para buscar substring.

defmodule FileProcessor do
  # Helper: crea un archivo temporal con contenido de prueba
  def create_test_file(path) do
    content = """
    primera línea con datos
    segunda línea de ejemplo
    tercera línea con datos especiales
    cuarta línea sin nada especial
    quinta línea con datos importantes
    sexta línea de relleno
    séptima línea con datos
    octava línea final
    """
    File.write!(path, content)
  end

  def first_n_lines(path, n) do
    # TODO:
    # File.stream!(path)
    # |> Stream.map(&String.trim/1)
    # |> Enum.take(n)
  end

  def count_lines_containing(path, substring) do
    # TODO:
    # File.stream!(path)
    # |> Stream.map(&String.trim/1)
    # |> Stream.filter(&String.contains?(&1, substring))
    # |> Enum.count()
  end
end

# =============================================================================
# Exercise 3: Stream chaining — map + filter + take en pipeline
# =============================================================================
#
# Completa la función `pipeline/3` que:
# 1. Toma un rango/lista como entrada
# 2. Aplica la función `transform_fn` a cada elemento (Stream.map)
# 3. Filtra con `filter_fn` (Stream.filter)
# 4. Retorna los primeros `n` resultados como lista
#
# Esta función demuestra el poder composicional de los Streams.
#
# Ejemplo:
#   StreamChain.pipeline(1..100, &(&1 * &1), &(rem(&1, 10) == 0), 3)
#   # Cuadrados que terminan en 0: [100, 400, 900]
#   # (10²=100, 20²=400, 30²=900)

defmodule StreamChain do
  def pipeline(collection, transform_fn, filter_fn, n) do
    # TODO: construye el pipeline completo
    # collection
    # |> Stream.map(transform_fn)
    # |> Stream.filter(filter_fn)
    # |> Stream.take(n)
    # |> Enum.to_list()
  end
end

# =============================================================================
# Exercise 4: Stream.cycle — secuencias que se repiten infinitamente
# =============================================================================
#
# Completa las funciones en CycleDemo:
#
# `repeating_pattern/2`: dados una lista y un número n, retorna los
#   primeros n elementos de la lista repetida cíclicamente.
#
#   CycleDemo.repeating_pattern([:a, :b, :c], 7)
#   # => [:a, :b, :c, :a, :b, :c, :a]
#
# `round_robin_assign/2`: dados una lista de workers y una lista de tasks,
#   asigna cada task a un worker de manera round-robin.
#   Retorna una lista de {task, worker}.
#
#   CycleDemo.round_robin_assign([:w1, :w2], ["t1", "t2", "t3", "t4"])
#   # => [{"t1", :w1}, {"t2", :w2}, {"t3", :w1}, {"t4", :w2}]
#
# Tip: Stream.cycle + Enum.zip

defmodule CycleDemo do
  def repeating_pattern(list, n) do
    # TODO: Stream.cycle(list) |> Enum.take(n)
  end

  def round_robin_assign(workers, tasks) do
    # TODO: Stream.cycle(workers) |> Enum.zip(tasks) |> Enum.map(fn {w, t} -> {t, w} end)
  end
end

# =============================================================================
# Exercise 5: Comparación de memoria — Enum vs Stream
# =============================================================================
#
# Este ejercicio es demostrativo. Completa ambas implementaciones y observa
# la diferencia en comportamiento (no en resultado, que es el mismo).
#
# Ambas funciones deben retornar la suma de los primeros 1000 números pares
# del rango 1..10_000_000.
#
# `with_enum/0`: versión EAGER con Enum (procesa toda la colección)
# `with_stream/0`: versión LAZY con Stream (procesa solo lo necesario)
#
# Resultado esperado: 2 + 4 + 6 + ... + 2000 = 1_001_000

defmodule MemoryComparison do
  # Versión eager — crea listas intermedias grandes
  def with_enum do
    # TODO:
    # 1..10_000_000
    # |> Enum.filter(&(rem(&1, 2) == 0))  # lista enorme
    # |> Enum.take(1000)
    # |> Enum.sum()
  end

  # Versión lazy — nunca materializa la lista grande
  def with_stream do
    # TODO:
    # 1..10_000_000
    # |> Stream.filter(&(rem(&1, 2) == 0))
    # |> Stream.take(1000)
    # |> Enum.sum()
  end

  # Mide el tiempo de ejecución de una función
  def measure(label, fun) do
    {microseconds, result} = :timer.tc(fun)
    ms = microseconds / 1000
    IO.puts("  #{label}: #{ms}ms → #{result}")
    result
  end
end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule StreamTests do
  def run do
    IO.puts("\n=== Verificación: Streams y Evaluación Lazy ===\n")

    # Ejercicio 1: Pipeline lazy
    check("first_n_doubled(5)",       LazyPipeline.first_n_doubled(5),   [2, 4, 6, 8, 10])
    check("first_n_doubled(3)",       LazyPipeline.first_n_doubled(3),   [2, 4, 6])
    check("first_n_matching(5, par)", LazyPipeline.first_n_matching(5, &(rem(&1, 2) == 0)), [2, 4, 6, 8, 10])

    IO.puts("")

    # Ejercicio 2: File streaming
    tmp_path = "/tmp/elixir_stream_test.txt"
    FileProcessor.create_test_file(tmp_path)

    lines = FileProcessor.first_n_lines(tmp_path, 3)
    check("first_n_lines(path, 3)", length(lines), 3)
    check("first line content", List.first(lines), "primera línea con datos")

    count = FileProcessor.count_lines_containing(tmp_path, "con datos")
    check("count_lines_containing 'con datos'", count, 3)

    File.rm(tmp_path)

    IO.puts("")

    # Ejercicio 3: Stream chaining
    result = StreamChain.pipeline(1..100, &(&1 * &1), &(rem(&1, 10) == 0), 3)
    check("pipeline cuadrados múltiplos de 10", result, [100, 400, 900])

    IO.puts("")

    # Ejercicio 4: Stream.cycle
    check("repeating_pattern([:a,:b,:c], 7)",
          CycleDemo.repeating_pattern([:a, :b, :c], 7),
          [:a, :b, :c, :a, :b, :c, :a])

    check("round_robin_assign([:w1,:w2], tasks)",
          CycleDemo.round_robin_assign([:w1, :w2], ["t1", "t2", "t3", "t4"]),
          [{"t1", :w1}, {"t2", :w2}, {"t3", :w1}, {"t4", :w2}])

    IO.puts("")

    # Ejercicio 5: Comparación
    IO.puts("  Comparación Enum vs Stream (suma 1000 pares):")
    r1 = MemoryComparison.measure("  Enum  ", &MemoryComparison.with_enum/0)
    r2 = MemoryComparison.measure("  Stream", &MemoryComparison.with_stream/0)
    check("mismo resultado", r1, r2)
    check("resultado correcto", r1, 1_001_000)

    IO.puts("\n=== Verificación completada ===")
  end

  defp check(label, actual, expected) do
    if actual == expected do
      IO.puts("  ✓ #{label}")
    else
      IO.puts("  ✗ #{label}")
      IO.puts("    Esperado: #{inspect(expected)}")
      IO.puts("    Obtenido: #{inspect(actual)}")
    end
  end
end

# =============================================================================
# Common Mistakes
# =============================================================================
#
# ERROR 1: Olvidar materializar el Stream
#
#   result = 1..100 |> Stream.map(&(&1 * 2))
#   IO.inspect(result)  # => #Stream<...>  ← no es la lista
#
#   Solución: añadir |> Enum.to_list() o |> Enum.take(n) al final.
#
# ERROR 2: Usar Stream cuando Enum es suficiente
#
#   # Si la colección es pequeña (< 1000 elementos), Enum es igual de rápido
#   # y más legible. Stream agrega overhead de construcción de la "receta".
#   Enum.map([1, 2, 3], &(&1 * 2))  # más directo para listas pequeñas
#
# ERROR 3: File.stream! sin String.trim — líneas con \n
#
#   File.stream!("file.txt") |> Enum.take(2)
#   # => ["primera línea\n", "segunda línea\n"]
#
#   Solución: |> Stream.map(&String.trim/1) para limpiar los saltos de línea.
#
# ERROR 4: Stream.cycle con lista vacía — bucle infinito
#
#   Stream.cycle([]) |> Enum.take(5)  # MatchError o loop
#
#   Siempre asegúrate de que la lista no está vacía antes de cycle.
#
# ERROR 5: Confundir Stream.take/2 con Enum.take/2
#
#   # Stream.take retorna un Stream (lazy, no ejecuta nada)
#   # Enum.take retorna una lista (eager, ejecuta la cadena)
#   1..100 |> Stream.map(&(&1 * 2)) |> Stream.take(5)   # aún lazy
#   1..100 |> Stream.map(&(&1 * 2)) |> Enum.take(5)     # ejecuta y retorna lista

# =============================================================================
# Summary
# =============================================================================
#
# - Stream es el módulo de Elixir para procesamiento lazy de colecciones
# - Las operaciones Stream construyen una "receta" sin ejecutar nada
# - La materialización ocurre al usar funciones de Enum (to_list, take, sum...)
# - File.stream! permite leer archivos grandes línea a línea eficientemente
# - Stream.cycle genera secuencias infinitas que se repiten
# - Prefer Stream cuando: colección grande, pipeline largo, o datos infinitos

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 12: Macros y quote/unquote
# - Explorar: Stream.unfold/2 para secuencias generadas por estado
# - Explorar: Stream.resource/3 para recursos con ciclo de vida (DB cursors)
# - Explorar: Flow (biblioteca) para procesamiento paralelo estilo Stream

# =============================================================================
# Resources
# =============================================================================
# - https://hexdocs.pm/elixir/Stream.html
# - https://hexdocs.pm/elixir/Enum.html
# - Elixir in Action, Cap. 4.2 — Lazy enumerables

# =============================================================================
# Try It Yourself (sin solución)
# =============================================================================
#
# Simula el procesamiento de un archivo CSV con 1 millón de líneas.
# (No crees el archivo real — usa Stream.unfold/2 para generarlo lazy.)
#
# El CSV simulado tiene el formato: "user_id,score,country"
# Ejemplo: "42,87,ES", "43,23,US", "44,95,ES", ...
#
# Implementa:
#   1. Un generador lazy que produce {user_id, score, country} tuples
#      usando Stream.unfold(1, fn id -> {"#{id},#{rem(id*7, 100)},#{if rem(id,3)==0, do: "ES", else: "US"}", id+1} end)
#      y luego parsea cada línea.
#
#   2. Filtra solo los usuarios de "ES" con score > 50
#
#   3. Toma los primeros 100 que cumplan la condición
#
#   4. Cuenta cuántos elementos se procesaron realmente para obtener 100
#      (hint: usa Stream.transform/3 con un contador)
#
# El objetivo es demostrar que Stream procesa solo los elementos necesarios.

StreamTests.run()
