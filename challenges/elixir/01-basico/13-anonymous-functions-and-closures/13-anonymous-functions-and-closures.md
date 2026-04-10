# 13. Anonymous Functions and Closures

**Difficulty**: Basico

---

## Prerequisites

- Funciones nombradas básicas (ejercicio 08)
- Pipe operator (ejercicio 12)
- Módulo Enum (ejercicio 11)

---

## Learning Objectives

- Definir funciones anónimas con `fn ... end`
- Invocar funciones anónimas con el punto obligatorio: `func.(args)`
- Entender qué es un closure y cómo captura variables del scope externo
- Definir múltiples cláusulas en una función anónima
- Escribir funciones anónimas con capture syntax `&`
- Capturar funciones nombradas existentes con `&Módulo.función/aridad`
- Pasar funciones como argumentos (higher-order functions)

---

## Concepts

### Función anónima: `fn ... end`

Una función anónima no tiene nombre. Se asigna a una variable para poder usarla.

```elixir
# Definición
double = fn x -> x * 2 end

# Invocación — NECESITA el punto
double.(5)    # => 10
double.(100)  # => 200
```

La sintaxis completa es:
```
fn <argumentos> -> <cuerpo> end
```

### El punto obligatorio `.()`

Las funciones anónimas (guardadas en variables) se invocan con un punto antes de
los paréntesis. Las funciones nombradas (definidas con `def`) NO llevan punto.

```elixir
# Función anónima — CON punto
double = fn x -> x * 2 end
double.(5)         # correcto
# double(5)        # ERROR: UndefinedFunctionError

# Función nombrada — SIN punto
String.upcase("hello")   # correcto
# String.upcase.("hello") # ERROR: FunctionClauseError
```

El punto es la forma en que Elixir distingue "llamar una variable que contiene
una función" de "llamar una función con ese nombre".

### Funciones de múltiples argumentos

```elixir
add = fn a, b -> a + b end
add.(3, 4)   # => 7

greet = fn name, greeting -> "#{greeting}, #{name}!" end
greet.("Alice", "Hello")  # => "Hello, Alice!"
```

### Closures: capturar el scope externo

Una función anónima **captura** las variables del scope donde fue creada.
Esto se llama closure.

```elixir
factor = 3
triple = fn x -> x * factor end

triple.(7)   # => 21
triple.(10)  # => 30
```

Aunque `factor` es una variable del scope exterior, `triple` la "recuerda"
incluso si el scope original ya no existe.

```elixir
# Fábrica de multiplicadores: retorna una función nueva por cada factor
multiplier = fn factor -> fn x -> x * factor end end

double = multiplier.(2)
triple = multiplier.(3)

double.(5)   # => 10
triple.(5)   # => 15
```

### Inmutabilidad y closures

En Elixir, las variables son inmutables por reasignación — cuando haces
`x = x + 1`, estás creando una nueva variable `x`, no mutando la original.
El closure captura el **valor** en el momento de creación.

```elixir
x = 10
add_x = fn n -> n + x end

x = 999  # reasignas x en el scope exterior — no afecta al closure

add_x.(5)   # => 15  (capturó x = 10, no x = 999)
```

### Múltiples cláusulas en funciones anónimas

```elixir
describe = fn
  :ok    -> "todo salió bien"
  :error -> "algo falló"
  other  -> "valor desconocido: #{inspect(other)}"
end

describe.(:ok)     # => "todo salió bien"
describe.(:error)  # => "algo falló"
describe.(42)      # => "valor desconocido: 42"
```

### Capture syntax `&`

`&` es una forma compacta de escribir funciones anónimas simples.

```elixir
# &1 es el primer argumento, &2 el segundo, etc.
double = &(&1 * 2)
add    = &(&1 + &2)

double.(5)    # => 10
add.(3, 4)    # => 7

# Equivalentes completos:
double_long = fn x    -> x * 2   end
add_long    = fn x, y -> x + y   end
```

### Function capture: `&Módulo.función/aridad`

Captura una función nombrada existente como si fuera una función anónima.

```elixir
upcase_fn = &String.upcase/1
upcase_fn.("hello")   # => "HELLO"

# Muy útil en Enum
Enum.map(["hello", "world"], &String.upcase/1)
# => ["HELLO", "WORLD"]

# Capturar función local
defmodule MyApp do
  def double(x), do: x * 2

  def run do
    Enum.map([1, 2, 3], &double/1)
    # => [2, 4, 6]
  end
end
```

---

## Exercises

### Ejercicio 1: Función anónima básica

```elixir
# Definir y llamar una función anónima
double = fn x -> x * 2 end

IO.inspect(double.(5))
IO.inspect(double.(0))
IO.inspect(double.(-3))
```

**Expected output:**
```
10
0
-6
```

---

### Ejercicio 2: Función anónima con múltiples argumentos

```elixir
add      = fn a, b -> a + b end
multiply = fn a, b -> a * b end
greet    = fn name, greeting -> "#{greeting}, #{name}!" end

IO.inspect(add.(3, 4))
IO.inspect(multiply.(6, 7))
IO.inspect(greet.("Alice", "Hello"))
```

**Expected output:**
```
7
42
"Hello, Alice!"
```

---

### Ejercicio 3: Closure — captura del scope externo

```elixir
# El closure recuerda el valor de factor del momento en que fue creado
multiplier = fn factor ->
  fn x -> x * factor end
end

double = multiplier.(2)
triple = multiplier.(3)
times_ten = multiplier.(10)

IO.inspect(double.(5))
IO.inspect(triple.(5))
IO.inspect(times_ten.(5))
```

**Expected output:**
```
10
15
50
```

---

### Ejercicio 4: Múltiples cláusulas en función anónima

```elixir
# La función anónima usa pattern matching en sus cláusulas
describe = fn
  :ok    -> "operación exitosa"
  :error -> "operación fallida"
  n when is_integer(n) and n > 0 -> "número positivo: #{n}"
  _other -> "valor no reconocido"
end

IO.inspect(describe.(:ok))
IO.inspect(describe.(:error))
IO.inspect(describe.(42))
IO.inspect(describe.("algo"))
```

**Expected output:**
```
"operación exitosa"
"operación fallida"
"número positivo: 42"
"valor no reconocido"
```

---

### Ejercicio 5: Capture syntax `&` como shorthand

```elixir
# Forma larga
square_long = fn x -> x * x end

# Forma corta con &
square_short = &(&1 * &1)

# Ambas son idénticas
IO.inspect(square_long.(4))
IO.inspect(square_short.(4))

# Usando & en Enum.map
results = Enum.map([1, 2, 3, 4, 5], &(&1 * &1))
IO.inspect(results)
```

**Expected output:**
```
16
16
[1, 4, 9, 16, 25]
```

---

### Ejercicio 6: Function capture `&Módulo.función/aridad`

```elixir
# Capturar función de un módulo
upcase = &String.upcase/1
length = &String.length/1

IO.inspect(upcase.("hello"))
IO.inspect(length.("hello"))

# Usar function capture en Enum
uppercased = Enum.map(["hello", "world", "elixir"], &String.upcase/1)
IO.inspect(uppercased)

# IO.puts también es una función capturable
Enum.each([1, 2, 3], &IO.puts/1)
```

**Expected output:**
```
"HELLO"
5
["HELLO", "WORLD", "ELIXIR"]
1
2
3
```

---

## Common Mistakes

### Error 1: Llamar función anónima sin el punto

```elixir
# WRONG — falta el punto
double = fn x -> x * 2 end
double(5)
```

```
** (UndefinedFunctionError) function double/1 is undefined or not exported
```

**Why**: Sin el punto, Elixir busca una función **nombrada** llamada `double/1`
en el módulo actual. El punto es la sintaxis para invocar una función guardada
en una variable.

**Fix**:
```elixir
double = fn x -> x * 2 end
double.(5)   # punto obligatorio
```

---

### Error 2: Poner punto en funciones nombradas

```elixir
# WRONG — las funciones nombradas NO usan punto
String.upcase.("hello")
```

```
** (UndefinedFunctionError) function String.upcase/0 is undefined or not exported
```

**Why**: `String.upcase.("hello")` se interpreta como llamar `String.upcase/0`
(sin args) y luego aplicar el resultado como función. Las funciones nombradas
no usan punto en la invocación.

**Fix**:
```elixir
String.upcase("hello")   # sin punto, argumentos directamente
```

---

### Error 3: Pensar que el closure captura una referencia mutable

```elixir
# ¿Qué imprime esto?
x = 10
add_x = fn n -> n + x end

x = 99  # reasignación en el scope exterior

IO.inspect(add_x.(5))
```

```
15   # no 104
```

**Why**: El closure capturó `x = 10` en el momento de creación. Cuando reasignas
`x = 99`, estás creando una nueva ligadura en el scope — el closure ya tiene
su propia copia del valor original. En Elixir esto nunca es sorprendente porque
los valores son inmutables.

**Fix**: No hay nada que corregir — es el comportamiento correcto. Si necesitas
que el closure use un valor actualizado, debes recrear el closure:
```elixir
x = 10
add_x = fn n -> n + x end

x = 99
add_x_v2 = fn n -> n + x end   # nuevo closure con x = 99

IO.inspect(add_x.(5))     # 15  — closure original
IO.inspect(add_x_v2.(5))  # 104 — nuevo closure
```

---

### Error 4: `&` sin los paréntesis externos

```elixir
# WRONG — & sin envolver en paréntesis
double = & &1 * 2
```

```
** (SyntaxError) unexpected token: &
```

**Why**: La syntax correcta envuelve la expresión en paréntesis: `&(&1 * 2)`.
El `&` externo indica "esto es una función anónima", y la expresión entre
paréntesis es su cuerpo con `&1` como argumento.

**Fix**:
```elixir
double = &(&1 * 2)      # correcto
double = fn x -> x * 2 end  # equivalente, igualmente correcto
```

---

## Verification

```bash
iex
```

```elixir
# Función anónima básica
double = fn x -> x * 2 end
double.(7)
# => 14

# Closure
multiplier = fn factor -> fn x -> x * factor end end
triple = multiplier.(3)
triple.(7)
# => 21

# Múltiples cláusulas
describe = fn
  :ok    -> "success"
  :error -> "failure"
end
describe.(:ok)
# => "success"

# Capture syntax
Enum.map([1, 2, 3], &(&1 * &1))
# => [1, 4, 9]

# Function capture
Enum.map(["hello", "world"], &String.upcase/1)
# => ["HELLO", "WORLD"]

# Verificar que el punto es obligatorio
f = fn x -> x + 1 end
# f(1)   # esto lanzaría UndefinedFunctionError
f.(1)    # => 2
```

---

## Summary

- Funciones anónimas se definen con `fn args -> body end` y se asignan a variables.
- La invocación requiere el punto: `func.(args)`. Sin punto = error.
- Funciones nombradas (`def`) **nunca** usan punto en la invocación.
- Un **closure** captura variables del scope externo en el momento de creación.
- La inmutabilidad de Elixir garantiza que los closures sean predecibles.
- `&(&1 * 2)` es shorthand para `fn x -> x * 2 end`.
- `&String.upcase/1` captura una función nombrada existente.
- Las funciones anónimas pueden pasarse como argumentos (higher-order functions).

---

## What's Next

- **Ejercicio 14**: Módulos y visibilidad — organizar código con `def` y `defp`
- **Ejercicio 15**: Structs — data containers tipados
- Pattern matching avanzado con funciones anónimas de múltiples cláusulas

---

## Resources

- [Anonymous functions — Elixir Getting Started](https://elixir-lang.org/getting-started/basic-types.html#anonymous-functions)
- [Capture operator — Elixir docs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#&/1)
- [Elixir School — Functions](https://elixirschool.com/en/lessons/basics/functions)
