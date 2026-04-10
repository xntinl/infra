# =============================================================================
# Ejercicio 10: Comprehensions Avanzadas
# Difficulty: Intermedio
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - Listas y ranges (1..10)
# - Funciones anónimas y pipe operator
# - Enum.map/2, Enum.filter/2 básicos
# - Maps y acceso a claves

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Escribir comprehensions básicas con `for x <- list, do: expr`
# 2. Filtrar elementos con condiciones en la comprehension
# 3. Generar productos cartesianos con múltiples generadores
# 4. Hacer destructuring de pares {k, v} directamente en el generador
# 5. Usar `into:` para recolectar resultados en estructuras distintas a listas

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# COMPREHENSION BÁSICA:
#
#   for x <- collection, do: expression
#
# Equivale a Enum.map/2 pero con sintaxis declarativa.
# Siempre retorna una lista (por defecto).
#
# FILTRO (guard inline):
#
#   for x <- collection, condition, do: expression
#
# La condición es cualquier expresión que retorne true/false.
# NO es un guard de Elixir — puede llamar funciones propias.
#
# MÚLTIPLES GENERADORES (producto cartesiano):
#
#   for x <- list1, y <- list2, do: {x, y}
#
# Genera todas las combinaciones de x e y.
# Es equivalente a dos Enum.flat_map anidados.
#
# DESTRUCTURING EN EL GENERADOR:
#
#   for {k, v} <- keyword_list, do: {k, v * 2}
#   for %{name: name} <- list_of_maps, do: name
#
# El patrón del generador puede hacer matching complejo.
# Elementos que NO matchean son silenciosamente ignorados.
#
# INTO — recolectar en otra estructura:
#
#   for {k, v} <- list, into: %{}, do: {k, String.upcase(v)}
#   for char <- chars, into: "", do: String.upcase(char)
#
# `into:` acepta cualquier estructura que implemente el protocol Collectable.
# Con into: %{}, cada `do:` debe retornar una tupla {clave, valor}.
#
# FILTRO + MÚLTIPLES GENERADORES:
#
#   for x <- 1..5, y <- 1..5, x != y, do: {x, y}

# =============================================================================
# Exercise 1: Comprehension básica — cuadrados
# =============================================================================
#
# Completa la función `squares/1` que recibe un rango o lista de números
# y retorna una lista con el cuadrado de cada número.
#
# Ejemplo:
#   ComprehensionBasic.squares(1..5)   # => [1, 4, 9, 16, 25]
#   ComprehensionBasic.squares([2, 4]) # => [4, 16]

defmodule ComprehensionBasic do
  def squares(numbers) do
    # TODO: Usa una comprehension `for x <- numbers, do: ...`
    # para retornar la lista de cuadrados
  end
end

# =============================================================================
# Exercise 2: Comprehension con filtro — múltiplos de 3
# =============================================================================
#
# Completa la función `multiples_of_3/1` que recibe un rango/lista y
# retorna solo los elementos que son múltiplos de 3.
#
# Tip: rem(x, 3) == 0
#
# Ejemplo:
#   ComprehensionFilter.multiples_of_3(1..20)
#   # => [3, 6, 9, 12, 15, 18]

defmodule ComprehensionFilter do
  def multiples_of_3(numbers) do
    # TODO: Usa una comprehension con condición de filtro
    # for x <- numbers, <condición>, do: x
  end

  # Bonus: múltiplos de N (función generalizada)
  def multiples_of(numbers, n) do
    # TODO: Mismo patrón pero parametrizado con n
    # rem(x, n) == 0
  end
end

# =============================================================================
# Exercise 3: Múltiples generadores — producto cartesiano
# =============================================================================
#
# Completa las funciones en el módulo CartesianProduct:
#
# `pairs/2`: todas las combinaciones (x, y) de dos listas
#   CartesianProduct.pairs([1, 2], [:a, :b])
#   # => [{1, :a}, {1, :b}, {2, :a}, {2, :b}]
#
# `no_diagonal/1`: pares (x, y) de 1..n donde x != y
#   CartesianProduct.no_diagonal(3)
#   # => [{1, 2}, {1, 3}, {2, 1}, {2, 3}, {3, 1}, {3, 2}]

defmodule CartesianProduct do
  def pairs(list1, list2) do
    # TODO: for x <- list1, y <- list2, do: {x, y}
  end

  def no_diagonal(n) do
    # TODO: Producto cartesiano de 1..n x 1..n filtrando donde x == y
    # for x <- 1..n, y <- 1..n, <condición>, do: {x, y}
  end
end

# =============================================================================
# Exercise 4: Comprehension sobre Map — transformar valores
# =============================================================================
#
# Completa las funciones en MapComprehension:
#
# `double_values/1`: recibe un keyword list y retorna lista de pares
#   con los valores duplicados.
#   MapComprehension.double_values([a: 1, b: 2, c: 5])
#   # => [a: 2, b: 4, c: 10]
#
# `filter_positive/1`: recibe un keyword list y retorna solo los pares
#   donde el valor es positivo (> 0).
#   MapComprehension.filter_positive([a: 3, b: -1, c: 0, d: 7])
#   # => [a: 3, d: 7]
#
# Tip: un keyword list es una lista de tuplas {atom, value}.
#      El destructuring {k, v} funciona directamente como generador.

defmodule MapComprehension do
  def double_values(keyword_list) do
    # TODO: for {k, v} <- keyword_list, do: {k, v * 2}
  end

  def filter_positive(keyword_list) do
    # TODO: for {k, v} <- keyword_list, <condición>, do: {k, v}
  end
end

# =============================================================================
# Exercise 5: into: — recolectar en un Map
# =============================================================================
#
# Completa las funciones en IntoCollector:
#
# `upcase_map/1`: recibe una lista de pares {atom, string} y retorna
#   un Map con los valores convertidos a mayúsculas.
#   IntoCollector.upcase_map([name: "alice", city: "madrid"])
#   # => %{name: "ALICE", city: "MADRID"}
#
# `index_map/1`: recibe una lista de strings y retorna un Map donde
#   cada string es la clave y su longitud es el valor.
#   IntoCollector.index_map(["hello", "world", "elixir"])
#   # => %{"hello" => 5, "world" => 5, "elixir" => 6}
#
# Tip: con into: %{}, el `do:` debe retornar {clave, valor}

defmodule IntoCollector do
  def upcase_map(pairs) do
    # TODO: for {k, v} <- pairs, into: %{}, do: {k, String.upcase(v)}
  end

  def index_map(strings) do
    # TODO: for s <- strings, into: %{}, do: {s, String.length(s)}
  end
end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule ComprehensionTests do
  def run do
    IO.puts("\n=== Verificación: Comprehensions Avanzadas ===\n")

    # Ejercicio 1: squares
    check("squares(1..5)",    ComprehensionBasic.squares(1..5),   [1, 4, 9, 16, 25])
    check("squares([2, 4])",  ComprehensionBasic.squares([2, 4]), [4, 16])

    IO.puts("")

    # Ejercicio 2: filter
    check("multiples_of_3(1..20)",    ComprehensionFilter.multiples_of_3(1..20),    [3, 6, 9, 12, 15, 18])
    check("multiples_of(1..15, 5)",   ComprehensionFilter.multiples_of(1..15, 5),   [5, 10, 15])

    IO.puts("")

    # Ejercicio 3: cartesian product
    check("pairs([1,2], [:a,:b])", CartesianProduct.pairs([1, 2], [:a, :b]),
          [{1, :a}, {1, :b}, {2, :a}, {2, :b}])
    check("no_diagonal(3)", CartesianProduct.no_diagonal(3),
          [{1, 2}, {1, 3}, {2, 1}, {2, 3}, {3, 1}, {3, 2}])

    IO.puts("")

    # Ejercicio 4: map comprehension
    check("double_values([a: 1, b: 2, c: 5])",
          MapComprehension.double_values([a: 1, b: 2, c: 5]),
          [a: 2, b: 4, c: 10])
    check("filter_positive([a: 3, b: -1, c: 0, d: 7])",
          MapComprehension.filter_positive([a: 3, b: -1, c: 0, d: 7]),
          [a: 3, d: 7])

    IO.puts("")

    # Ejercicio 5: into
    check("upcase_map([name: \"alice\", city: \"madrid\"])",
          IntoCollector.upcase_map([name: "alice", city: "madrid"]),
          %{name: "ALICE", city: "MADRID"})
    check("index_map([\"hello\", \"world\", \"elixir\"])",
          IntoCollector.index_map(["hello", "world", "elixir"]),
          %{"hello" => 5, "world" => 5, "elixir" => 6})

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
# ERROR 1: Confundir filtro de comprehension con guard de función
#
#   for x <- list, is_integer(x) and x > 0, do: x   # OK — is_integer sí está permitida
#   for x <- list, my_custom_fn(x), do: x            # También OK — NO es guard de función
#
#   A diferencia de los guards de función/case, los filtros de comprehension
#   SÍ pueden llamar funciones propias arbitrarias.
#
# ERROR 2: Olvidar que into: %{} requiere retornar {k, v}
#
#   for x <- list, into: %{}, do: x         # Error en runtime
#   for x <- list, into: %{}, do: {x, x*2}  # Correcto
#
# ERROR 3: Asumir que los elementos no-matching causan error
#
#   for %{name: name} <- mixed_list, do: name
#   # Los elementos que NO son mapas con :name son ignorados silenciosamente
#   # Esto puede ser un bug difícil de detectar si esperas procesarlos
#
# ERROR 4: Producto cartesiano vs zip
#
#   for x <- [1,2], y <- [:a, :b], do: {x, y}
#   # => [{1,:a},{1,:b},{2,:a},{2,:b}]  (producto — 4 elementos)
#
#   Si quieres zip (pares 1↔:a, 2↔:b), usa Enum.zip/2 en su lugar.
#
# ERROR 5: Comprehension retorna lista, no el mismo tipo de entrada
#
#   for x <- 1..5, do: x * 2
#   # => [2, 4, 6, 8, 10]  (lista, no Range)
#   Usar into: para cambiar la estructura de salida.

# =============================================================================
# Summary
# =============================================================================
#
# - `for x <- coll, do: expr` es la forma básica de comprehension
# - Las condiciones filtran elementos sin error (los que no matchean se omiten)
# - Múltiples generadores producen el producto cartesiano
# - El destructuring en el generador es elegante y potente
# - `into:` permite recolectar en Map, String, o cualquier Collectable

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 11: Streams y evaluación lazy
# - Explorar: `uniq_by:` y otras opciones de comprehension
# - Explorar: comprehensions con `reduce:` (Elixir >= 1.12)

# =============================================================================
# Resources
# =============================================================================
# - https://hexdocs.pm/elixir/comprehensions.html
# - https://hexdocs.pm/elixir/Enum.html
# - Programming Elixir >= 1.6, Cap. 10 — Processing Collections

# =============================================================================
# Try It Yourself (sin solución)
# =============================================================================
#
# Construye una tabla de multiplicar del 1 al 10 usando comprehensions.
# El resultado debe ser una lista de listas, donde cada sublista contiene
# los productos de ese número con 1..10.
#
# Estructura esperada:
#   multiplication_table()
#   # => [
#   #   [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],      # 1 × 1..10
#   #   [2, 4, 6, 8, 10, 12, 14, 16, 18, 20],  # 2 × 1..10
#   #   ...
#   #   [10, 20, 30, 40, 50, 60, 70, 80, 90, 100]
#   # ]
#
# Pistas:
# - Comprehension exterior: for x <- 1..10, do: <inner>
# - Comprehension interior: for y <- 1..10, do: x * y
# - Puedes anidar comprehensions directamente

ComprehensionTests.run()
