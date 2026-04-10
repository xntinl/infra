# 6. Lists and Head/Tail Pattern

**Difficulty**: Basico

## Prerequisites

- Haber completado los ejercicios 01–05
- Conocimiento básico de pattern matching (ejercicio 05)
- IEx disponible en tu terminal

## Learning Objectives

- Entender que las listas en Elixir son listas enlazadas, no arrays
- Dominar el patrón `[head | tail]` para descomponer listas
- Distinguir cuándo usar prepend (`[x | list]`) vs append (`list ++ [x]`) y por qué importa
- Aplicar funciones básicas del módulo `Enum` sobre listas

## Concepts

### Las listas son listas enlazadas

En Elixir, una lista es una estructura recursiva. Internamente, `[1, 2, 3]` es:

```elixir
[1 | [2 | [3 | []]]]
```

Cada nodo contiene un valor (head) y un puntero al siguiente nodo (tail). La lista vacía `[]` termina la cadena.

```elixir
# Estas expresiones son equivalentes
[1, 2, 3] == [1 | [2 | [3 | []]]]  # true

# Puedes construirlas manualmente
[1 | [2 | [3 | []]]]  # [1, 2, 3]
```

### Head y Tail

El operador `|` separa el primer elemento (head) del resto (tail):

```elixir
[head | tail] = [1, 2, 3]
head  # 1
tail  # [2, 3]

# Las funciones hd/1 y tl/1 hacen lo mismo
hd([1, 2, 3])  # 1
tl([1, 2, 3])  # [2, 3]
```

### Prepend es O(1), Append es O(n)

Prepend crea un nuevo nodo que apunta a la lista existente — operación constante:

```elixir
# O(1): solo crea un nodo nuevo
[0 | [1, 2, 3]]  # [0, 1, 2, 3]
```

Append con `++` recorre toda la lista izquierda para llegar al final:

```elixir
# O(n): recorre [1, 2, 3] completa para agregar [4]
[1, 2, 3] ++ [4]  # [1, 2, 3, 4]
```

Cuando construyas listas en recursión, usa prepend y luego `Enum.reverse/1` al final. Esto es el patrón idiomático en Elixir.

### Operadores y funciones de listas

```elixir
# Concatenación
[1, 2] ++ [3, 4]         # [1, 2, 3, 4]

# Diferencia (elimina primera ocurrencia de cada elemento del lado derecho)
[1, 2, 3, 2] -- [2]      # [1, 3, 2]

# Longitud
length([1, 2, 3])         # 3

# Head y Tail explícitos (lanzan error en lista vacía)
hd([10, 20, 30])          # 10
tl([10, 20, 30])          # [20, 30]
```

### Enum básico

```elixir
# map: transforma cada elemento
Enum.map([1, 2, 3], fn x -> x * 2 end)    # [2, 4, 6]

# filter: conserva elementos que cumplen la condición
Enum.filter([1, 2, 3, 4], fn x -> rem(x, 2) == 0 end)  # [2, 4]

# reduce: acumula un valor recorriendo la lista
Enum.reduce([1, 2, 3], 0, fn x, acc -> x + acc end)    # 6
```

## Exercises

### Exercise 1: Crear listas de distintos tipos

```elixir
# En IEx:

# Lista de enteros
nums = [1, 2, 3, 4, 5]

# Lista de átomos
statuses = [:ok, :error, :pending]

# Lista mixta (Elixir permite tipos distintos en la misma lista)
mixed = ["hello", 42, :ok, true, 3.14]

# Lista vacía
empty = []

# Verificar tipos
is_list(nums)    # true
is_list(empty)   # true
length(mixed)    # 5
```

**Expected output:**

```
iex> nums = [1, 2, 3, 4, 5]
[1, 2, 3, 4, 5]
iex> statuses = [:ok, :error, :pending]
[:ok, :error, :pending]
iex> mixed = ["hello", 42, :ok, true, 3.14]
["hello", 42, :ok, true, 3.14]
iex> empty = []
[]
iex> is_list(nums)
true
iex> length(mixed)
5
```

---

### Exercise 2: Head y Tail con pattern matching

```elixir
# Descomponer una lista en head y tail
[head | tail] = [1, 2, 3]
head   # 1
tail   # [2, 3]

# Usando hd/1 y tl/1
hd([10, 20, 30])   # 10
tl([10, 20, 30])   # [20, 30]

# Lista de un solo elemento
[solo | resto] = [42]
solo   # 42
resto  # []

# La lista vacía NO tiene head ni tail
# Esto lanza MatchError:
# [h | t] = []
```

**Expected output:**

```
iex> [head | tail] = [1, 2, 3]
[1, 2, 3]
iex> head
1
iex> tail
[2, 3]
iex> hd([10, 20, 30])
10
iex> tl([10, 20, 30])
[20, 30]
iex> [solo | resto] = [42]
[42]
iex> solo
42
iex> resto
[]
```

---

### Exercise 3: Pattern matching con múltiples elementos

```elixir
# Extraer los dos primeros elementos y el resto
[first, second | rest] = [1, 2, 3, 4, 5]
first   # 1
second  # 2
rest    # [3, 4, 5]

# Extraer tres elementos
[a, b, c | _] = [:x, :y, :z, :w]
a  # :x
b  # :y
c  # :z

# Ignorar el head con _
[_ | tail] = [100, 200, 300]
tail  # [200, 300]

# Match exacto de una lista de 3 elementos
[x, y, z] = [10, 20, 30]
x + y + z  # 60
```

**Expected output:**

```
iex> [first, second | rest] = [1, 2, 3, 4, 5]
[1, 2, 3, 4, 5]
iex> first
1
iex> second
2
iex> rest
[3, 4, 5]
iex> [a, b, c | _] = [:x, :y, :z, :w]
[:x, :y, :z, :w]
iex> a
:x
iex> [_ | tail] = [100, 200, 300]
[100, 200, 300]
iex> tail
[200, 300]
```

---

### Exercise 4: Prepend vs Append — diferencia de rendimiento

```elixir
list = [1, 2, 3]

# Prepend: O(1) — crea un nodo nuevo que apunta a list
new_list_prepend = [0 | list]
# [0, 1, 2, 3]

# Append: O(n) — recorre toda la lista izquierda
new_list_append = list ++ [4]
# [1, 2, 3, 4]

# Concatenar dos listas
combined = [1, 2] ++ [3, 4]
# [1, 2, 3, 4]

# Diferencia de listas: elimina primera ocurrencia de cada elemento
[1, 2, 3, 2, 1] -- [2, 1]
# [3, 2, 1]  — solo elimina la primera ocurrencia de cada uno

# Patrón idiomático: construir con prepend y revertir al final
# (verás esto más en el ejercicio de recursión)
Enum.reverse([0 | [1 | [2 | []]]])
# [2, 1, 0]
```

**Expected output:**

```
iex> list = [1, 2, 3]
[1, 2, 3]
iex> [0 | list]
[0, 1, 2, 3]
iex> list ++ [4]
[1, 2, 3, 4]
iex> [1, 2] ++ [3, 4]
[1, 2, 3, 4]
iex> [1, 2, 3, 2, 1] -- [2, 1]
[3, 2, 1]
```

---

### Exercise 5: Funciones de lista — length, sum, member

```elixir
nums = [10, 20, 30, 40, 50]

# Longitud
length(nums)                     # 5

# Suma de todos los elementos
Enum.sum(nums)                   # 150

# ¿Contiene un elemento?
Enum.member?(nums, 30)           # true
Enum.member?(nums, 99)           # false

# Mínimo y máximo
Enum.min(nums)                   # 10
Enum.max(nums)                   # 50

# Ordenar
Enum.sort([3, 1, 4, 1, 5])      # [1, 1, 3, 4, 5]
Enum.sort([3, 1, 4], :desc)     # [4, 3, 1]

# Primer y último elemento (sin pattern matching)
List.first(nums)                 # 10
List.last(nums)                  # 50
```

**Expected output:**

```
iex> nums = [10, 20, 30, 40, 50]
[10, 20, 30, 40, 50]
iex> length(nums)
5
iex> Enum.sum(nums)
150
iex> Enum.member?(nums, 30)
true
iex> Enum.member?(nums, 99)
false
iex> Enum.min(nums)
10
iex> Enum.max(nums)
50
iex> Enum.sort([3, 1, 4, 1, 5])
[1, 1, 3, 4, 5]
```

---

### Exercise 6: Enum.map — transformar listas

```elixir
# Duplicar cada número
Enum.map([1, 2, 3], fn x -> x * 2 end)
# [2, 4, 6]

# Convertir a string
Enum.map([1, 2, 3], fn x -> Integer.to_string(x) end)
# ["1", "2", "3"]

# Elevar al cuadrado
Enum.map([1, 2, 3, 4, 5], fn x -> x * x end)
# [1, 4, 9, 16, 25]

# Con función nombrada existente (notación de captura)
Enum.map(["hello", "world"], &String.upcase/1)
# ["HELLO", "WORLD"]

# filter: conservar solo pares
Enum.filter([1, 2, 3, 4, 5, 6], fn x -> rem(x, 2) == 0 end)
# [2, 4, 6]

# Combinar map y filter
[1, 2, 3, 4, 5]
|> Enum.filter(fn x -> rem(x, 2) != 0 end)   # [1, 3, 5]
|> Enum.map(fn x -> x * 10 end)               # [10, 30, 50]
```

**Expected output:**

```
iex> Enum.map([1, 2, 3], fn x -> x * 2 end)
[2, 4, 6]
iex> Enum.map([1, 2, 3], fn x -> Integer.to_string(x) end)
["1", "2", "3"]
iex> Enum.map([1, 2, 3, 4, 5], fn x -> x * x end)
[1, 4, 9, 16, 25]
iex> Enum.map(["hello", "world"], &String.upcase/1)
["HELLO", "WORLD"]
iex> Enum.filter([1, 2, 3, 4, 5, 6], fn x -> rem(x, 2) == 0 end)
[2, 4, 6]
iex> [1, 2, 3, 4, 5] |> Enum.filter(fn x -> rem(x, 2) != 0 end) |> Enum.map(fn x -> x * 10 end)
[10, 30, 50]
```

## Common Mistakes

### Error 1: Usar `++` para construir listas en loops

```elixir
# WRONG: O(n²) — cada iteración recorre toda la lista acumulada
def build_wrong(items) do
  Enum.reduce(items, [], fn x, acc -> acc ++ [x * 2] end)
end

# FIX: prepend O(1) y revertir al final — O(n)
def build_correct(items) do
  items
  |> Enum.reduce([], fn x, acc -> [x * 2 | acc] end)
  |> Enum.reverse()
end
```

### Error 2: Llamar `hd/1` o `tl/1` en lista vacía

```elixir
# WRONG: lanza ArgumentError
hd([])  # ** (ArgumentError) errors were encountered during formatting

# FIX: verificar antes, o usar pattern matching con case
def safe_head(list) do
  case list do
    []        -> nil
    [head | _] -> head
  end
end
```

### Error 3: Asumir acceso O(1) por índice como en arrays

```elixir
# WRONG: Enum.at/2 es O(n) — recorre la lista hasta llegar al índice
Enum.at([1, 2, 3, 4, 5], 4)  # 5, pero recorre 5 elementos

# FIX: si necesitas acceso aleatorio frecuente, usa una tupla (O(1))
elem({1, 2, 3, 4, 5}, 4)  # 5, O(1)

# Las listas son para recorrido secuencial, no para acceso aleatorio
```

### Error 4: Confundir `--` con eliminación de todos los elementos

```elixir
# WRONG: asumir que -- elimina TODAS las ocurrencias
[1, 2, 2, 3] -- [2]
# [1, 2, 3]  — solo elimina la PRIMERA ocurrencia de 2

# FIX: usar Enum.reject para eliminar todas las ocurrencias
Enum.reject([1, 2, 2, 3], fn x -> x == 2 end)
# [1, 3]
```

## Verification

Verifica que entiendes los conceptos ejecutando esto en IEx:

```bash
iex
```

```elixir
# Prueba 1: estructura interna
[1 | [2 | [3 | []]]] == [1, 2, 3]
# Esperado: true

# Prueba 2: pattern matching
[h | t] = [10, 20, 30]
{h, t}
# Esperado: {10, [20, 30]}

# Prueba 3: prepend
[0 | [1, 2, 3]]
# Esperado: [0, 1, 2, 3]

# Prueba 4: Enum pipeline
[1, 2, 3, 4, 5]
|> Enum.filter(fn x -> x > 2 end)
|> Enum.map(fn x -> x * x end)
|> Enum.sum()
# Esperado: 9 + 16 + 25 = 50

# Prueba 5: diferencia de listas
[1, 2, 3, 2, 1] -- [2, 1]
# Esperado: [3, 2, 1]
```

## Summary

- Las listas en Elixir son **listas enlazadas** — `[1, 2, 3]` es azúcar sintáctica para `[1 | [2 | [3 | []]]]`
- El patrón `[head | tail]` es la forma idiomática de descomponer una lista
- **Prepend es O(1)**, append con `++` es O(n) — para construir listas, prepend + `Enum.reverse/1`
- `hd/1` y `tl/1` fallan en lista vacía — usa pattern matching con `case` para manejar este caso
- `Enum.map/2`, `Enum.filter/2` y `Enum.reduce/3` son las funciones de transformación más usadas

## What's Next

- **07-maps-and-keyword-lists**: Datos con clave-valor y opciones con orden
- **10-recursion-and-tail-call-optimization**: Usar `[head | tail]` para escribir tus propias funciones recursivas sobre listas

## Resources

- [Elixir Docs — List](https://hexdocs.pm/elixir/List.html)
- [Elixir Docs — Enum](https://hexdocs.pm/elixir/Enum.html)
- [Elixir School — Collections](https://elixirschool.com/en/lessons/basics/collections)
- [Elixir Getting Started — Lists](https://elixir-lang.org/getting-started/basic-types.html#lists-or-tuples)
