# 11. Enum Module and Immutability

**Difficulty**: Basico

---

## Prerequisites

- Listas en Elixir (ejercicio 06)
- Pattern matching básico (ejercicio 05)
- Funciones anónimas básicas (conocimiento general)

---

## Learning Objectives

- Entender que Elixir es inmutable: ninguna operación modifica la lista original
- Usar `Enum.map/2` para transformar cada elemento de una colección
- Usar `Enum.filter/2` para seleccionar elementos que cumplen un predicado
- Usar `Enum.reduce/3` para acumular un resultado desde una colección
- Usar `Enum.each/2` para ejecutar side effects sobre cada elemento
- Distinguir cuándo usar `Enum` (eager) vs `Stream` (lazy)
- Aplicar capture syntax `&` como shorthand de funciones anónimas

---

## Concepts

### Immutability: Los datos nunca se modifican

En Elixir, los datos son **inmutables**. Cuando usas `Enum.map`, no estás modificando
la lista original — estás creando una **nueva lista** con los resultados transformados.

```elixir
original = [1, 2, 3]
doubled  = Enum.map(original, fn x -> x * 2 end)

IO.inspect(original)  # [1, 2, 3]  — sin cambios
IO.inspect(doubled)   # [2, 4, 6]  — nueva lista
```

Esto es fundamentalmente diferente a lenguajes como Python o JavaScript, donde
`list.push()` o `list.sort()` modifican la lista en el lugar.

### `Enum.map/2`: Transformar cada elemento

`map` aplica una función a cada elemento y retorna una **nueva lista** del mismo tamaño.

```elixir
# Función anónima completa
Enum.map([1, 2, 3], fn x -> x * x end)
# => [1, 4, 9]

# Con capture syntax
Enum.map([1, 2, 3], &(&1 * &1))
# => [1, 4, 9]

# Con strings
Enum.map(["alice", "bob", "carol"], fn name -> String.upcase(name) end)
# => ["ALICE", "BOB", "CAROL"]
```

### `Enum.filter/2`: Seleccionar por predicado

`filter` retorna una nueva lista con solo los elementos donde el predicado retorna `true`.

```elixir
# Solo números pares
Enum.filter([1, 2, 3, 4, 5], fn x -> rem(x, 2) == 0 end)
# => [2, 4]

# Solo strings largos
Enum.filter(["hi", "hello", "hey", "greetings"], fn s -> String.length(s) > 3 end)
# => ["hello", "greetings"]
```

### `Enum.reduce/3`: Acumular un resultado

`reduce` colapsa una colección a un solo valor. Requiere un valor inicial (acumulador).

```elixir
# Sumar todos los elementos (acumulador inicia en 0)
Enum.reduce([1, 2, 3, 4], 0, fn x, acc -> acc + x end)
# => 10

# Construir un string desde una lista
Enum.reduce(["a", "b", "c"], "", fn letter, acc -> acc <> letter end)
# => "abc"

# Encontrar el máximo
Enum.reduce([3, 1, 4, 1, 5, 9], 0, fn x, acc -> if x > acc, do: x, else: acc end)
# => 9
```

### `Enum.each/2`: Side effects, no transformación

`each` es para **efectos secundarios** (imprimir, escribir a disco, enviar mensajes).
Siempre retorna `:ok`, nunca retorna los elementos transformados.

```elixir
result = Enum.each(["alice", "bob"], fn name ->
  IO.puts("Hello, #{name}!")
end)
# Imprime:
# Hello, alice!
# Hello, bob!

IO.inspect(result)  # :ok
```

### Enum vs Stream: Eager vs Lazy

`Enum` procesa **todos los elementos inmediatamente** (eager).
`Stream` construye un pipeline **sin ejecutarlo** hasta que se necesita (lazy).

```elixir
# Enum: procesa toda la lista en cada paso
[1, 2, 3, 4, 5]
|> Enum.map(&(&1 * 2))    # crea lista intermedia [2, 4, 6, 8, 10]
|> Enum.filter(&(&1 > 4)) # crea lista final [6, 8, 10]

# Stream: construye el pipeline, ejecuta una sola vez al final
[1, 2, 3, 4, 5]
|> Stream.map(&(&1 * 2))    # no ejecuta nada aún
|> Stream.filter(&(&1 > 4)) # no ejecuta nada aún
|> Enum.to_list()           # ejecuta todo: [6, 8, 10]
```

Para colecciones pequeñas usa `Enum`. Para colecciones muy grandes o infinitas, `Stream`.

### Capture syntax `&`

`&` es shorthand para crear funciones anónimas. `&1` es el primer argumento.

```elixir
# Equivalentes:
fn x -> x * 2 end
&(&1 * 2)

# Function capture: referencia a una función existente
fn x -> String.upcase(x) end
&String.upcase/1   # módulo.función/aridad

# Con dos argumentos
fn x, y -> x + y end
&(&1 + &2)
```

---

## Exercises

### Ejercicio 1: map — transformar elementos

```elixir
# Multiplica cada número por 2
result = Enum.map([1, 2, 3], fn x -> x * 2 end)
IO.inspect(result)
```

**Expected output:**
```
[2, 4, 6]
```

---

### Ejercicio 2: filter — seleccionar por condición

```elixir
# Filtra solo los números pares
evens = Enum.filter([1, 2, 3, 4, 5], fn x -> rem(x, 2) == 0 end)
IO.inspect(evens)

# También puedes filtrar por valor negativo (rechazar impares)
odds = Enum.filter([1, 2, 3, 4, 5], fn x -> rem(x, 2) != 0 end)
IO.inspect(odds)
```

**Expected output:**
```
[2, 4]
[1, 3, 5]
```

---

### Ejercicio 3: reduce — acumular resultado

```elixir
# Suma todos los elementos, acumulador inicia en 0
total = Enum.reduce([1, 2, 3, 4], 0, fn x, acc -> acc + x end)
IO.inspect(total)

# Concatenar strings
sentence = Enum.reduce(["Elixir", " ", "is", " ", "fun"], "", fn word, acc ->
  acc <> word
end)
IO.inspect(sentence)
```

**Expected output:**
```
10
"Elixir is fun"
```

---

### Ejercicio 4: each — side effects

```elixir
# each ejecuta una acción por cada elemento
result = Enum.each(["alice", "bob"], fn name ->
  IO.puts("Hello, #{name}!")
end)

# ¿Qué retorna each?
IO.inspect(result)
```

**Expected output:**
```
Hello, alice!
Hello, bob!
:ok
```

---

### Ejercicio 5: Capture syntax `&`

```elixir
# Cuadrado de cada número — forma larga
squares_long = Enum.map([1, 2, 3], fn x -> x * x end)
IO.inspect(squares_long)

# Cuadrado de cada número — con capture syntax
squares_short = Enum.map([1, 2, 3], &(&1 * &1))
IO.inspect(squares_short)

# Function capture: referencia a función existente
uppercased = Enum.map(["hello", "world"], &String.upcase/1)
IO.inspect(uppercased)
```

**Expected output:**
```
[1, 4, 9]
[1, 4, 9]
["HELLO", "WORLD"]
```

---

### Ejercicio 6: Inmutabilidad en acción

```elixir
# La lista original NUNCA se modifica
original = [1, 2, 3]
doubled  = Enum.map(original, &(&1 * 2))
filtered = Enum.filter(original, &(&1 > 1))

IO.puts("original:")
IO.inspect(original)

IO.puts("doubled:")
IO.inspect(doubled)

IO.puts("filtered:")
IO.inspect(filtered)

IO.puts("original again (unchanged):")
IO.inspect(original)
```

**Expected output:**
```
original:
[1, 2, 3]
doubled:
[2, 4, 6]
filtered:
[2, 3]
original again (unchanged):
[1, 2, 3]
```

---

### Ejercicio 7: Chaining con pipe operator

```elixir
# Procesar una lista en pasos encadenados:
# 1. Filtrar positivos
# 2. Multiplicar por 10
# 3. Sumar todo

result =
  [1, -2, 3, -4, 5]
  |> Enum.filter(&(&1 > 0))
  |> Enum.map(&(&1 * 10))
  |> Enum.sum()

IO.inspect(result)

# Mismo resultado, sin pipe (difícil de leer):
result2 = Enum.sum(Enum.map(Enum.filter([1, -2, 3, -4, 5], &(&1 > 0)), &(&1 * 10)))
IO.inspect(result2)
```

**Expected output:**
```
90
90
```

---

## Common Mistakes

### Error 1: Pensar que `Enum.map` muta la lista original

```elixir
# WRONG — pensando que map modifica original_list
original_list = [1, 2, 3]
Enum.map(original_list, &(&1 * 2))
IO.inspect(original_list)  # espero [2, 4, 6]...
```

```
# Error conceptual — no es un error de compilación, pero el resultado sorprende:
[1, 2, 3]  # original_list sigue siendo [1, 2, 3]
```

**Why**: `Enum.map` nunca modifica la lista original. Crea y retorna una nueva lista.

**Fix**:
```elixir
original_list = [1, 2, 3]
doubled = Enum.map(original_list, &(&1 * 2))  # asigna el resultado
IO.inspect(doubled)  # [2, 4, 6]
```

---

### Error 2: Usar `Enum.each` cuando quieres transformar

```elixir
# WRONG — each retorna :ok, no la lista transformada
result = Enum.each([1, 2, 3], fn x -> x * 2 end)
IO.inspect(result)
```

```
:ok   # no es [2, 4, 6]
```

**Why**: `each` es para side effects (imprimir, loggear, etc.), no para transformar.
Siempre retorna `:ok`.

**Fix**:
```elixir
# Para transformar, usa map
result = Enum.map([1, 2, 3], fn x -> x * 2 end)
IO.inspect(result)  # [2, 4, 6]
```

---

### Error 3: `Enum.sum` sobre lista vacía

```elixir
# Sorpresa: esto NO lanza error
result = Enum.sum([])
IO.inspect(result)
```

```
0
```

**Why**: El acumulador de `sum` tiene identidad `0` para la suma. Una lista vacía
reducida con suma da el elemento neutro (0). Lo mismo aplica a `Enum.reduce([], 0, ...)`.

**Fix**: Si necesitas distinguir lista vacía de "suma = 0", verifica antes:
```elixir
list = []
if Enum.empty?(list) do
  IO.puts("Lista vacía")
else
  IO.inspect(Enum.sum(list))
end
```

---

### Error 4: Confundir `rem/2` con `mod` de otros lenguajes

```elixir
# WRONG — en algunos lenguajes negativo % 2 da resultados distintos
IO.inspect(rem(-3, 2))  # ¿cuánto da?
```

```
-1   # en Elixir, rem/2 sigue el signo del dividendo
```

**Why**: `rem(-3, 2)` en Elixir es `-1`, no `1`. Si necesitas siempre positivo:

**Fix**:
```elixir
# Para verificar paridad de forma segura:
is_even = fn x -> rem(abs(x), 2) == 0 end
IO.inspect(is_even.(-4))  # true
```

---

## Verification

Abre IEx y ejecuta cada ejercicio:

```bash
iex
```

```elixir
# Ejercicio 1
Enum.map([1, 2, 3], fn x -> x * 2 end)
# => [2, 4, 6]

# Ejercicio 2
Enum.filter([1, 2, 3, 4, 5], fn x -> rem(x, 2) == 0 end)
# => [2, 4]

# Ejercicio 3
Enum.reduce([1, 2, 3, 4], 0, fn x, acc -> acc + x end)
# => 10

# Ejercicio 4
Enum.each(["alice", "bob"], fn name -> IO.puts("Hello, #{name}!") end)
# Hello, alice!
# Hello, bob!
# :ok

# Ejercicio 5
Enum.map(["hello", "world"], &String.upcase/1)
# => ["HELLO", "WORLD"]

# Ejercicio 6 — inmutabilidad
original = [1, 2, 3]
_doubled = Enum.map(original, &(&1 * 2))
original
# => [1, 2, 3]

# Ejercicio 7
[1, -2, 3, -4, 5] |> Enum.filter(&(&1 > 0)) |> Enum.map(&(&1 * 10)) |> Enum.sum()
# => 90
```

Para explorar más funciones del módulo Enum:

```elixir
# En IEx:
Enum.__info__(:functions) |> Keyword.keys() |> Enum.sort()
```

---

## Summary

- **Inmutabilidad**: `Enum.map`, `Enum.filter`, `Enum.reduce` nunca modifican la
  colección original — siempre retornan una nueva colección.
- **map**: transforma cada elemento → nueva lista del mismo tamaño.
- **filter**: selecciona elementos → nueva lista de igual o menor tamaño.
- **reduce**: colapsa a un valor → cualquier tipo (número, string, mapa, etc.).
- **each**: side effects → siempre retorna `:ok`.
- **Capture syntax `&`**: shorthand para funciones simples (`&(&1 * 2)`) y para
  capturar funciones existentes (`&String.upcase/1`).
- **Enum vs Stream**: Enum es eager (inmediato), Stream es lazy (diferido).

---

## What's Next

- **Ejercicio 12**: Pipe operator `|>` para encadenar transformaciones de forma legible
- **Ejercicio 13**: Funciones anónimas y closures en profundidad
- **Stream module**: colecciones lazy para grandes conjuntos de datos

---

## Resources

- [Enum module — HexDocs](https://hexdocs.pm/elixir/Enum.html)
- [Elixir School — Enum](https://elixirschool.com/en/lessons/basics/enum)
- [Stream module — HexDocs](https://hexdocs.pm/elixir/Stream.html)
