# 12. Pipe Operator and Composition

**Difficulty**: Basico

---

## Prerequisites

- Funciones básicas en Elixir (ejercicio 08)
- Módulo Enum (ejercicio 11)
- String functions básicas

---

## Learning Objectives

- Entender qué hace el operador `|>` y cómo transforma el código
- Reescribir llamadas anidadas como pipelines lineales y legibles
- Saber que `data |> func(arg2)` equivale a `func(data, arg2)`
- Usar `IO.inspect/2` con `label:` para depurar pipelines paso a paso
- Aplicar pipeline thinking: modelar transformaciones como pasos secuenciales
- Reconocer cuándo un pipeline tiene demasiados pasos y debe extraerse

---

## Concepts

### El operador `|>`: qué hace exactamente

`|>` toma el valor de la izquierda y lo pasa como **primer argumento** a la función
de la derecha.

```elixir
# Sin pipe — se lee de adentro hacia afuera
String.downcase(String.trim("  Hello World  "))
# => "hello world"

# Con pipe — se lee de arriba hacia abajo
"  Hello World  "
|> String.trim()
|> String.downcase()
# => "hello world"
```

Ambas expresiones son **idénticas**. El pipe es azúcar sintáctica — no cambia la
semántica, solo la disposición visual.

### Regla del primer argumento

El valor del lado izquierdo siempre se inserta como el **primer argumento**:

```elixir
# data |> func(a, b)  ==  func(data, a, b)

[3, 1, 4, 1, 5]
|> Enum.sort()                   # Enum.sort([3, 1, 4, 1, 5])
|> Enum.uniq()                   # Enum.uniq([1, 3, 4, 5])
|> Enum.reverse()                # Enum.reverse([1, 3, 4, 5])
# => [5, 4, 3, 1]

# Con argumentos adicionales
"hello world"
|> String.split(" ")             # String.split("hello world", " ")
|> Enum.map(&String.capitalize/1)
|> Enum.join(", ")               # Enum.join(["Hello", "World"], ", ")
# => "Hello, World"
```

### Composición vs anidación

La anidación profunda se lee de adentro hacia afuera, lo cual es cognitivamente costoso.
Los pipelines se leen en orden de ejecución.

```elixir
# Anidación — ¿cuál se ejecuta primero?
Enum.sum(Enum.map(Enum.filter([1, 2, 3, 4, 5], &(rem(&1, 2) == 0)), &(&1 * 10)))

# Pipeline — cada paso es obvio
[1, 2, 3, 4, 5]
|> Enum.filter(&(rem(&1, 2) == 0))  # paso 1: [2, 4]
|> Enum.map(&(&1 * 10))             # paso 2: [20, 40]
|> Enum.sum()                       # paso 3: 60
```

### Depurar con `IO.inspect/2`

`IO.inspect` retorna su argumento sin modificarlo, lo que permite insertarlo en
cualquier punto del pipeline para ver el estado intermedio.

```elixir
[1, 2, 3]
|> IO.inspect(label: "original")
|> Enum.map(&(&1 * 2))
|> IO.inspect(label: "doubled")
|> Enum.filter(&(&1 > 3))
|> IO.inspect(label: "filtered")
```

```
original: [1, 2, 3]
doubled: [2, 4, 6]
filtered: [4, 6]
```

### Pipeline thinking

Modelar una transformación como una serie de pasos secuenciales:

1. ¿Qué dato tengo al inicio?
2. ¿Qué pasos lo transforman hasta el resultado final?
3. Cada paso recibe el output del anterior como primer argumento.

```elixir
# Dado: lista de usuarios (maps) con :name y :age
# Quiero: nombres (string) de usuarios mayores de edad, ordenados

users = [
  %{name: "Alice", age: 30},
  %{name: "Bob", age: 16},
  %{name: "Carol", age: 25}
]

result =
  users
  |> Enum.filter(fn u -> u.age >= 18 end)   # solo adultos
  |> Enum.map(fn u -> u.name end)            # extraer nombres
  |> Enum.sort()                             # ordenar
# => ["Alice", "Carol"]
```

---

## Exercises

### Ejercicio 1: Sin pipe — funciones anidadas

```elixir
# Difícil de leer: se ejecuta de adentro hacia afuera
result = String.downcase(String.trim("  Hello World  "))
IO.inspect(result)

# ¿En qué orden se ejecutan las funciones?
# 1. String.trim/1
# 2. String.downcase/1
```

**Expected output:**
```
"hello world"
```

---

### Ejercicio 2: Con pipe — mismo resultado, más legible

```elixir
# Fácil de leer: se ejecuta de arriba hacia abajo
result =
  "  Hello World  "
  |> String.trim()
  |> String.downcase()

IO.inspect(result)
```

**Expected output:**
```
"hello world"
```

---

### Ejercicio 3: Pipe con múltiples argumentos

```elixir
# |> pasa el dato como PRIMER argumento
# Los argumentos adicionales se añaden después
result =
  [3, 1, 4, 1, 5, 9, 2, 6]
  |> Enum.sort()
  |> Enum.uniq()
  |> Enum.reverse()

IO.inspect(result)

# Equivalente sin pipe:
result2 = Enum.reverse(Enum.uniq(Enum.sort([3, 1, 4, 1, 5, 9, 2, 6])))
IO.inspect(result2)
```

**Expected output:**
```
[9, 6, 5, 4, 3, 2, 1]
[9, 6, 5, 4, 3, 2, 1]
```

---

### Ejercicio 4: Depurar con `IO.inspect`

```elixir
# IO.inspect retorna su argumento — se puede insertar en cualquier paso
[1, 2, 3]
|> IO.inspect(label: "original")
|> Enum.map(&(&1 * 2))
|> IO.inspect(label: "doubled")
|> Enum.filter(&(&1 > 3))
|> IO.inspect(label: "filtered")
```

**Expected output:**
```
original: [1, 2, 3]
doubled: [2, 4, 6]
filtered: [4, 6]
```

---

### Ejercicio 5: Pipeline con strings — iniciales

```elixir
# Dado un nombre completo, extrae las iniciales separadas por punto
initials =
  "alice marie smith"
  |> String.split()                    # ["alice", "marie", "smith"]
  |> Enum.map(&String.first/1)         # ["a", "m", "s"]
  |> Enum.map(&String.upcase/1)        # ["A", "M", "S"]
  |> Enum.join(".")                    # "A.M.S"

IO.inspect(initials)
```

**Expected output:**
```
"A.M.S"
```

---

### Ejercicio 6: Pipeline real — procesar lista de usuarios

```elixir
users = [
  %{name: "Alice", age: 30, active: true},
  %{name: "Bob", age: 16, active: true},
  %{name: "Carol", age: 25, active: false},
  %{name: "Dave", age: 22, active: true},
  %{name: "Eve", age: 17, active: false}
]

# Quiero: nombres de usuarios activos y adultos, ordenados alfabéticamente
result =
  users
  |> Enum.filter(fn u -> u.active end)           # solo activos
  |> Enum.filter(fn u -> u.age >= 18 end)        # solo adultos
  |> Enum.map(fn u -> u.name end)                # extraer nombres
  |> Enum.sort()                                 # orden alfabético

IO.inspect(result)
```

**Expected output:**
```
["Alice", "Dave"]
```

---

## Common Mistakes

### Error 1: Creer que `|>` pasa el dato como último argumento

```elixir
# WRONG — confusión sobre la posición del argumento
data = [1, 2, 3]

# ¿Esto llama Enum.member?(1, data) o Enum.member?(data, 1)?
result = data |> Enum.member?(1)
IO.inspect(result)
```

```
true
```

**Why**: `data |> Enum.member?(1)` es `Enum.member?(data, 1)` — el dato va como
**primer** argumento, no último. En este caso funciona bien, pero si esperas que
vaya al final, tendrás bugs sutiles.

**Fix**: Siempre recuerda: `data |> func(a, b)` = `func(data, a, b)`.
Si la función que necesitas no acepta el dato como primer argumento, usa un wrapper:
```elixir
# Función donde el dato NO es el primer argumento
data = "needle"
haystack = "the needle in a haystack"

# NO puedes hacer: data |> String.contains?(haystack)
# porque String.contains?(string, pattern) espera haystack como primer arg

# Wrapper explícito:
result = haystack |> String.contains?(data)
IO.inspect(result)  # true
```

---

### Error 2: Expresión que no es función al final del pipe

```elixir
# WRONG — no puedes pipar a un valor literal
result = [1, 2, 3] |> length
```

```
** (CompileError) undefined function length/0
```

**Why**: `length` sin paréntesis se interpreta como una llamada a función de aridad 0,
no como `length([1, 2, 3])`.

**Fix**: Usa paréntesis siempre en el lado derecho del pipe:
```elixir
result = [1, 2, 3] |> length()
IO.inspect(result)  # 3

# O simplemente:
result = [1, 2, 3] |> Kernel.length()
```

---

### Error 3: Pipeline excesivo — dificulta la lectura

```elixir
# WRONG — demasiados pasos sin nombre, difícil de entender qué hace
result =
  raw_data
  |> step_a()
  |> step_b()
  |> step_c()
  |> step_d()
  |> step_e()
  |> step_f()
  |> step_g()
  |> step_h()
  |> step_i()
  |> step_j()
```

**Why**: Un pipeline de 10+ pasos es tan difícil de seguir como el código anidado.
Pierde el beneficio de legibilidad.

**Fix**: Extrae sub-pipelines en funciones con nombres descriptivos:
```elixir
defp parse_and_validate(data) do
  data
  |> step_a()
  |> step_b()
  |> step_c()
end

defp transform_and_format(data) do
  data
  |> step_d()
  |> step_e()
  |> step_f()
end

result =
  raw_data
  |> parse_and_validate()
  |> transform_and_format()
```

---

## Verification

```bash
iex
```

```elixir
# Verificar la regla del primer argumento
"  hello  " |> String.trim()
# => "hello"

# Equivalente explícito
String.trim("  hello  ")
# => "hello"

# Pipe con argumento adicional
"hello world" |> String.split(" ")
# => ["hello", "world"]

# IO.inspect no altera el valor
[1, 2, 3] |> IO.inspect(label: "lista") |> Enum.sum()
# lista: [1, 2, 3]
# => 6

# Pipeline completo
"alice marie smith"
|> String.split()
|> Enum.map(&String.first/1)
|> Enum.map(&String.upcase/1)
|> Enum.join(".")
# => "A.M.S"
```

---

## Summary

- `|>` pasa el valor de la izquierda como **primer argumento** a la función de la derecha.
- `data |> func(a, b)` es exactamente `func(data, a, b)`.
- Los pipelines se leen en orden de ejecución — más legibles que la anidación.
- `IO.inspect(label: "step")` es transparente: retorna su argumento sin modificarlo.
- Si necesitas pasar el dato como argumento no-primero, usa un wrapper o lambda.
- Pipelines de 2-5 pasos son ideales. Más de eso → extraer en funciones con nombre.

---

## What's Next

- **Ejercicio 13**: Funciones anónimas y closures — cómo crearlas y capturar variables
- **Ejercicio 14**: Módulos y visibilidad de funciones
- Combinando pipes con pattern matching en funciones

---

## Resources

- [Pipe operator — Elixir docs](https://hexdocs.pm/elixir/Kernel.html#%7C%3E/2)
- [Elixir School — Pipe operator](https://elixirschool.com/en/lessons/basics/pipe_operator)
- [IO.inspect — HexDocs](https://hexdocs.pm/elixir/IO.html#inspect/2)
