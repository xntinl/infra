# 10. Comprehensions Avanzadas

**Difficulty**: Intermedio

---

## Prerequisites

- `for` básico en Elixir
- Enum.map/filter/reduce
- Pattern matching en listas y mapas
- Ejercicio 09: Pattern Matching Avanzado

---

## Learning Objectives

1. Usar `for` con múltiples generators para producir productos cartesianos
2. Aplicar filtros dentro de comprehensions para reducir resultados
3. Redirigir el output con la opción `:into` (Map, MapSet, String...)
4. Deduplicar resultados con `:uniq`
5. Limitar resultados con `:take`
6. Hacer map comprehensions que producen mapas directamente
7. Combinar generators, filtros y opciones en una sola expresión declarativa

---

## Concepts

### Sintaxis básica

```elixir
# Generator simple
for x <- [1, 2, 3], do: x * 2
# [2, 4, 6]

# Con filtro (guard en el generator)
for x <- 1..10, rem(x, 2) == 0, do: x
# [2, 4, 6, 8, 10]
```

### Múltiples Generators — Producto Cartesiano

Cada generator adicional itera sobre todos los valores del anterior:

```elixir
for x <- [1, 2], y <- [:a, :b], do: {x, y}
# [{1, :a}, {1, :b}, {2, :a}, {2, :b}]

# El orden importa: x es el bucle externo, y el interno
for suit <- [:corazones, :picas], rank <- [1, 2, 3] do
  {rank, suit}
end
# [{1, :corazones}, {2, :corazones}, {3, :corazones},
#  {1, :picas},     {2, :picas},     {3, :picas}]
```

### Filtros dentro del `for`

Pueden aparecer entre generators o después del último:

```elixir
# Filtro entre generators — descarta combinaciones antes de seguir
for x <- 1..5, x != 3, y <- 1..x, do: {x, y}
# No genera filas donde x == 3

# Múltiples condiciones de filtro (todas deben ser verdaderas)
for x <- 1..100,
    rem(x, 3) == 0,
    rem(x, 5) == 0 do
  x
end
# [15, 30, 45, 60, 75, 90]
```

### Opción `:into`

Por defecto `for` devuelve una lista. Con `:into` puedes dirigir el output a cualquier tipo que implemente `Collectable`:

```elixir
# Map comprehension — clave y valor en cada iteración
for {k, v} <- [a: 1, b: 2, c: 3], into: %{} do
  {k, v * 10}
end
# %{a: 10, b: 20, c: 30}

# MapSet — automáticamente deduplica
for x <- [1, 2, 2, 3, 3, 3], into: MapSet.new() do
  x
end
# MapSet<[1, 2, 3]>

# String — concatenación caracter a caracter
for c <- String.graphemes("hola"), into: "" do
  String.upcase(c)
end
# "HOLA"
```

### Opción `:uniq`

Deduplica el resultado final sin necesidad de `Enum.uniq/1`:

```elixir
for x <- [1, 2, 2, 3], y <- [0, 0], uniq: true, do: x + y
# [1, 2, 3]  <- sin duplicados
```

### Opción `:take`

Limita el número de elementos generados (lazy stopping):

```elixir
for x <- 1..1_000_000, rem(x, 7) == 0, take: 5, do: x
# [7, 14, 21, 28, 35]  <- para en 5, no recorre el millón entero
```

### Pattern Matching en Generators

Los generators pueden hacer matching; los elementos que no coincidan son ignorados silenciosamente:

```elixir
# Solo procesa las tuplas {:ok, valor}
resultados = [{:ok, 1}, {:error, "fallo"}, {:ok, 2}, {:error, "otro"}]

for {:ok, v} <- resultados, do: v
# [1, 2]  <- los :error se descartan automáticamente
```

---

## Exercises

### Exercise 1: Tabla de Multiplicar

Genera y formatea una tabla de multiplicar completa usando comprehensions.

```elixir
# Archivo: lib/tabla_multiplicar.ex

defmodule TablaMultiplicar do
  @doc """
  Genera la tabla de multiplicar de 1 a n como lista de tuplas {a, b, resultado}.
  """
  # TODO: Usar for con dos generators: a <- 1..n, b <- 1..n
  # Devolver {a, b, a*b}
  def generar(n) when is_integer(n) and n > 0 do
  end

  @doc """
  Genera solo los resultados donde el producto es divisible por divisor.
  """
  # TODO: Añadir filtro al for de generar/1
  # Pista: rem(a * b, divisor) == 0
  def generar_divisibles(n, divisor) when divisor > 0 do
  end

  @doc """
  Convierte la lista de tuplas a un mapa %{{a, b} => resultado}.
  """
  # TODO: Usar for ... into: %{}
  # Clave: {a, b}, valor: resultado
  def como_mapa(n) do
  end

  @doc """
  Formatea la tabla como string listo para imprimir.
  Cada fila: "2 x 3 = 6"
  Filas separadas por salto de línea.
  """
  # TODO: Generar la tabla, mapear cada tupla a string, unir con "\n"
  def imprimir(n) do
    n
    |> generar()
    |> Enum.map(fn {a, b, resultado} ->
      # TODO: formatear como "a x b = resultado"
    end)
    |> Enum.join("\n")
  end

  @doc """
  Devuelve los N primeros productos únicos de la tabla de multiplicar de 1..max,
  ordenados de menor a mayor.
  """
  # TODO: Usar for con uniq: true y take: n
  # Luego ordenar con Enum.sort/1
  def primeros_productos_unicos(max, n) do
  end
end
```

**Verificación esperada:**

```elixir
TablaMultiplicar.generar(3)
# [{1,1,1},{1,2,2},{1,3,3},{2,1,2},{2,2,4},{2,3,6},{3,1,3},{3,2,6},{3,3,9}]

TablaMultiplicar.generar_divisibles(5, 6)
# [{2,3,6},{3,2,6},{3,4,12},{4,3,12},{2,6,...} ...] <- solo múltiplos de 6

mapa = TablaMultiplicar.como_mapa(3)
mapa[{2, 3}]  # 6
mapa[{3, 3}]  # 9

IO.puts(TablaMultiplicar.imprimir(3))
# 1 x 1 = 1
# 1 x 2 = 2
# ...
# 3 x 3 = 9

TablaMultiplicar.primeros_productos_unicos(5, 6)
# [1, 2, 3, 4, 5, 6]
```

---

### Exercise 2: Parser de Productos Cross-Join

Procesa un catálogo de productos y variantes usando comprehensions multi-generator para generar todas las combinaciones de SKU.

```elixir
# Archivo: lib/sku_generator.ex

defmodule SKUGenerator do
  @doc """
  Dado un producto con variantes, genera todos los SKUs posibles.

  producto = %{
    codigo: "CAMISA",
    tallas: ["S", "M", "L", "XL"],
    colores: ["ROJO", "AZUL", "NEGRO"],
    materiales: ["ALG", "POL"]  # puede ser nil (sin variante de material)
  }

  SKU resultante: "CAMISA-S-ROJO-ALG"
  """
  def generar_skus(%{codigo: codigo, tallas: tallas, colores: colores, materiales: nil}) do
    # TODO: for con dos generators: talla <- tallas, color <- colores
    # SKU: "#{codigo}-#{talla}-#{color}"
  end

  def generar_skus(%{codigo: codigo, tallas: tallas, colores: colores, materiales: materiales}) do
    # TODO: for con tres generators: talla, color, material
    # SKU: "#{codigo}-#{talla}-#{color}-#{material}"
  end

  @doc """
  Genera SKUs de múltiples productos en un solo listado plano.
  Descarta productos con lista de tallas vacía.
  """
  # TODO: for con generator de productos, filtro (tallas no vacías), luego...
  # Pista: no puedes hacer for anidado directamente en Elixir para aplanar.
  # Usa Enum.flat_map + generar_skus/1 en el cuerpo del for
  def catalogo_completo(productos) do
    productos
    |> Enum.filter(fn
      # TODO: filtrar productos sin tallas
    end)
    |> Enum.flat_map(&generar_skus/1)
  end

  @doc """
  Agrupa los SKUs por color extraído del SKU (segunda parte al splitear por "-").
  Devuelve %{color => [skus]}
  """
  # TODO: for con matching en el generator para extraer el color,
  # luego into: %{} agrupando — pero Enum.group_by es más natural aquí.
  # Usa Enum.group_by/2 con una función que extrae el color del SKU.
  # Pista: String.split(sku, "-") |> Enum.at(2) — posición 0=codigo, 1=talla, 2=color
  def agrupar_por_color(skus) do
  end

  @doc """
  Filtra SKUs que coincidan con una talla y devuelve solo los códigos de producto únicos.
  """
  # TODO: for con filtro String.contains?(sku, "-#{talla}-"), into: MapSet.new()
  # Luego extraer el código (primera parte al split)
  def productos_en_talla(skus, talla) do
    for sku <- skus,
        # TODO: condición que filtre por talla
        into: MapSet.new() do
      # TODO: extraer el código del producto del SKU
    end
  end
end
```

**Verificación esperada:**

```elixir
camisa = %{
  codigo: "CAMISA",
  tallas: ["S", "M"],
  colores: ["ROJO", "AZUL"],
  materiales: nil
}

SKUGenerator.generar_skus(camisa)
# ["CAMISA-S-ROJO", "CAMISA-S-AZUL", "CAMISA-M-ROJO", "CAMISA-M-AZUL"]

pantalon = %{
  codigo: "PANT",
  tallas: ["S", "M"],
  colores: ["NEGRO"],
  materiales: ["ALG", "POL"]
}

SKUGenerator.generar_skus(pantalon)
# ["PANT-S-NEGRO-ALG", "PANT-S-NEGRO-POL", "PANT-M-NEGRO-ALG", "PANT-M-NEGRO-POL"]

skus = SKUGenerator.catalogo_completo([camisa, pantalon])
# 4 + 4 = 8 SKUs en total

SKUGenerator.agrupar_por_color(skus)
# %{"ROJO" => [...], "AZUL" => [...], "NEGRO" => [...]}

SKUGenerator.productos_en_talla(skus, "S")
# MapSet<["CAMISA", "PANT"]>
```

---

### Exercise 3: Grouping y Agregación con Comprehensions

Implementa un sistema de análisis de ventas que usa comprehensions para agrupar, filtrar y agregar datos.

```elixir
# Archivo: lib/ventas_analyzer.ex

defmodule Venta do
  defstruct [:id, :producto, :categoria, :cantidad, :precio, :fecha, :region]
end

defmodule VentasAnalyzer do
  @doc """
  Genera el reporte de ventas por región y categoría.
  Devuelve lista de %{region: r, categoria: c, total: t, unidades: u}.
  Solo incluye combinaciones con al menos min_unidades vendidas.
  """
  def reporte_region_categoria(ventas, min_unidades \\ 0) do
    # Paso 1: Obtener combinaciones únicas de {region, categoria}
    # TODO: for con uniq: true extrayendo {region, categoria} de cada venta
    combinaciones =
      for %Venta{region: r, categoria: c} <- ventas, uniq: true do
        {r, c}
      end

    # Paso 2: Para cada combinación, calcular totales
    # TODO: for con generator de combinaciones
    # Filtrar ventas del grupo, calcular suma de (cantidad * precio) y suma de cantidades
    # Filtrar grupos con unidades >= min_unidades
    for {region, categoria} <- combinaciones,
        grupo = Enum.filter(ventas, fn
          # TODO: pattern match para filtrar ventas de esta region y categoria
        end),
        total   = Enum.sum(for %Venta{cantidad: c, precio: p} <- grupo, do: c * p),
        unidades = Enum.sum(for %Venta{cantidad: c} <- grupo, do: c),
        unidades >= min_unidades do
      # TODO: devolver mapa con region, categoria, total (redondeado 2 decimales), unidades
    end
  end

  @doc """
  Encuentra las N categorías más vendidas (por unidades totales).
  """
  # TODO: for para sumar unidades por categoría -> into: %{}
  # acumulando con Map.update/4
  # Luego ordenar y tomar los N primeros
  def top_categorias(ventas, n) do
    # Pista: usa Enum.reduce en lugar de for si el into: %{} es complejo
    # para acumular sumas por categoría
    categorias_totales =
      Enum.reduce(ventas, %{}, fn %Venta{categoria: cat, cantidad: cant}, acc ->
        # TODO: acumular cant en acc[cat]
      end)

    categorias_totales
    |> Enum.sort_by(fn {_cat, total} ->
      # TODO: ordenar por total descendente
    end, :desc)
    |> Enum.take(n)
  end

  @doc """
  Genera una matriz de correlación entre regiones y categorías.
  Devuelve %{region => %{categoria => total_euros}}.
  Regiones sin ventas de una categoría tienen 0.
  """
  def matriz_region_categoria(ventas) do
    regiones    = for %Venta{region: r} <- ventas, uniq: true, do: r
    categorias  = for %Venta{categoria: c} <- ventas, uniq: true, do: c

    # TODO: for región <- regiones, into: %{} que construye
    # el mapa interno: for categoría <- categorías, into: %{}
    # calculando el total de ventas para esa celda (puede ser 0.0)
    for region <- regiones, into: %{} do
      fila =
        for categoria <- categorias, into: %{} do
          total =
            ventas
            |> Enum.filter(fn
              # TODO: filtrar por region y categoria
            end)
            |> Enum.sum_by(fn %Venta{cantidad: c, precio: p} -> c * p end)

          # TODO: devolver {categoria, total}
        end

      # TODO: devolver {region, fila}
    end
  end
end
```

**Nota**: `Enum.sum_by/2` existe desde Elixir 1.18. Si usas una versión anterior, reemplaza por `Enum.reduce(enum, 0, fn v, acc -> acc + f.(v) end)`.

**Verificación esperada:**

```elixir
ventas = [
  %Venta{id: 1, producto: "Silla", categoria: "Muebles", cantidad: 10, precio: 99.0,  fecha: "2026-01", region: "Norte"},
  %Venta{id: 2, producto: "Mesa",  categoria: "Muebles", cantidad: 5,  precio: 199.0, fecha: "2026-01", region: "Sur"},
  %Venta{id: 3, producto: "Lamp",  categoria: "Hogar",   cantidad: 20, precio: 29.0,  fecha: "2026-02", region: "Norte"},
  %Venta{id: 4, producto: "Silla", categoria: "Muebles", cantidad: 3,  precio: 99.0,  fecha: "2026-02", region: "Sur"},
  %Venta{id: 5, producto: "Lamp",  categoria: "Hogar",   cantidad: 8,  precio: 29.0,  fecha: "2026-02", region: "Sur"},
]

VentasAnalyzer.reporte_region_categoria(ventas)
# [
#   %{region: "Norte", categoria: "Muebles", total: 990.0,  unidades: 10},
#   %{region: "Norte", categoria: "Hogar",   total: 580.0,  unidades: 20},
#   %{region: "Sur",   categoria: "Muebles", total: 1292.0, unidades: 8},
#   %{region: "Sur",   categoria: "Hogar",   total: 232.0,  unidades: 8},
# ]

VentasAnalyzer.reporte_region_categoria(ventas, 10)
# Solo filas con >= 10 unidades

VentasAnalyzer.top_categorias(ventas, 2)
# [{"Muebles", 18}, {"Hogar", 28}]  <- ordenadas por unidades desc

matriz = VentasAnalyzer.matriz_region_categoria(ventas)
matriz["Norte"]["Muebles"]  # 990.0
matriz["Sur"]["Hogar"]      # 232.0
```

---

## Common Mistakes

### 1. Generator sobre una sola colección cuando se necesita aplanar

```elixir
# MAL — genera lista de listas, no aplana
for departamento <- departamentos do
  for empleado <- departamento.empleados, do: empleado.nombre
end
# [["Ana", "Bob"], ["Carlos"]]

# BIEN — for con dos generators aplana automáticamente
for departamento <- departamentos,
    empleado <- departamento.empleados do
  empleado.nombre
end
# ["Ana", "Bob", "Carlos"]
```

### 2. Confundir filtro con generator

```elixir
# MAL — intentar usar <- para filtrar (error de sintaxis o semántica incorrecta)
for x <- lista, y <- Enum.filter(lista, &(&1 > 0)) do  # costoso: re-filtra en cada x
  {x, y}
end

# BIEN — filtro como condición booleana
for x <- lista, y <- lista, y > 0 do
  {x, y}
end
```

### 3. Usar for cuando Enum es más legible

```elixir
# MAL — comprehension innecesariamente compleja
for {k, v} <- mapa, v > 0, into: %{}, do: {k, v * 2}

# BIEN — equivalente más legible
mapa
|> Enum.filter(fn {_k, v} -> v > 0 end)
|> Map.new(fn {k, v} -> {k, v * 2} end)
```

### 4. Olvidar que :uniq compara por igualdad estructural

```elixir
# Si los structs tienen campos diferentes, no se consideran duplicados
for %MiStruct{id: id, ts: _ts} <- lista, uniq: true, do: id
# uniq: true aquí aplica al id extraído, no al struct completo — esto SÍ funciona bien

# Pero si haces:
for s <- lista, uniq: true, do: s
# Compara la struct entera, incluyendo :ts — puede no deduplicar como esperas
```

---

## Verification

```bash
mix compile
mix test

iex -S mix
```

```elixir
# Smoke tests en iex:

# Exercise 1
TablaMultiplicar.generar(3) |> length()          # 9
TablaMultiplicar.generar_divisibles(5, 4)         # solo múltiplos de 4
TablaMultiplicar.como_mapa(4)[{4, 4}]             # 16
TablaMultiplicar.primeros_productos_unicos(5, 5)  # [1, 2, 3, 4, 5]

# Exercise 2
skus = SKUGenerator.generar_skus(%{
  codigo: "TEST",
  tallas: ["S", "M"],
  colores: ["R", "B"],
  materiales: nil
})
length(skus)  # 4

# Exercise 3
# (con los datos de ventas del ejercicio)
```

---

## Summary

Las comprehensions en Elixir son mucho más poderosas que los list comprehensions de otros lenguajes. Los múltiples generators crean productos cartesianos de forma declarativa, los filtros entre generators recortan el espacio de búsqueda antes de seguir, y las opciones `:into`, `:uniq` y `:take` eliminan la necesidad de encadenar transformaciones post-procesamiento. El resultado es código más expresivo y frecuentemente más eficiente que cadenas largas de `Enum.map |> Enum.filter |> Enum.uniq`.

## What's Next

**11. Streams y Lazy Evaluation** — procesamiento de datos con evaluación diferida, ficheros de millones de líneas y streams infinitos.

## Resources

- [Elixir Docs — Comprehensions](https://elixir-lang.org/getting-started/comprehensions.html)
- [for/1 special form](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#for/1)
- [Collectable Protocol](https://hexdocs.pm/elixir/Collectable.html)
- [Elixir School — Comprehensions](https://elixirschool.com/en/lessons/basics/comprehensions)
