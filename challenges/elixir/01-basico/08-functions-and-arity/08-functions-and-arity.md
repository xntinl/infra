# 8. Functions and Arity

**Difficulty**: Basico

## Prerequisites

- Haber completado los ejercicios 01–07
- Conocimiento de pattern matching (ejercicio 05) y maps (ejercicio 07)
- Un proyecto Mix creado con `mix new` o IEx disponible

## Learning Objectives

- Definir funciones públicas con `def` y privadas con `defp`
- Entender que la arity es parte del identificador de una función
- Escribir múltiples cláusulas de una misma función con patrones distintos
- Usar argumentos con valor por defecto con `\\`
- Proteger cláusulas con guards (`when`)
- Leer la notación `Module.function/arity`

## Concepts

### def y defp

Las funciones en Elixir se definen dentro de módulos:

```elixir
defmodule MathUtils do
  # Función pública: accesible desde fuera del módulo
  def add(a, b), do: a + b

  # Función privada: solo accesible dentro de MathUtils
  defp validate_positive(n) when n > 0, do: :ok
  defp validate_positive(_), do: :error
end

MathUtils.add(2, 3)           # 5
# MathUtils.validate_positive(5) # UndefinedFunctionError
```

La forma compacta `do:` es equivalente al bloque `do...end`:

```elixir
# Estas dos formas son idénticas
def greet(name), do: "Hello, #{name}!"

def greet(name) do
  "Hello, #{name}!"
end
```

### Arity: el número de argumentos es parte del nombre

En Elixir, `greet/1` y `greet/2` son funciones **distintas** aunque compartan nombre:

```elixir
defmodule Greeter do
  def greet(name), do: "Hello, #{name}!"
  def greet(name, lang) when lang == :es, do: "Hola, #{name}!"
  def greet(name, lang) when lang == :fr, do: "Bonjour, #{name}!"
end

Greeter.greet("Alice")           # "Hello, Alice!"
Greeter.greet("Alice", :es)      # "Hola, Alice!"
Greeter.greet("Alice", :fr)      # "Bonjour, Alice!"
```

La notación `Module.function/arity` identifica exactamente qué función es:
- `String.upcase/1` — recibe 1 argumento
- `Enum.map/2` — recibe 2 argumentos
- `Map.get/3` — recibe 3 argumentos

### Multiple Clauses — pattern matching en parámetros

Puedes definir varias cláusulas de la misma función. Elixir evalúa de arriba a abajo y usa la primera que hace match:

```elixir
defmodule Status do
  def describe(:ok),    do: "Operation succeeded"
  def describe(:error), do: "Operation failed"
  def describe(other),  do: "Unknown status: #{inspect(other)}"
end

Status.describe(:ok)      # "Operation succeeded"
Status.describe(:error)   # "Operation failed"
Status.describe(:pending) # "Unknown status: :pending"
```

**El orden importa**: las cláusulas más específicas siempre deben ir primero.

### Destruir estructuras en los parámetros

El pattern matching ocurre directamente en los parámetros:

```elixir
defmodule Geometry do
  # Destruir tupla en el parámetro
  def area({:rect, width, height}), do: width * height
  def area({:circle, radius}),      do: :math.pi() * radius * radius

  # Destruir map en el parámetro
  def greet(%{name: name, role: :admin}), do: "Admin #{name}"
  def greet(%{name: name}),               do: "User #{name}"
end

Geometry.area({:rect, 4, 5})     # 20
Geometry.area({:circle, 3})      # 28.274...
```

### Default Arguments

El operador `\\` define un valor por defecto:

```elixir
defmodule Formatter do
  def repeat(str, times \\ 1) do
    String.duplicate(str, times)
  end
end

Formatter.repeat("ha")     # "ha"      — times = 1
Formatter.repeat("ha", 3)  # "hahaha"  — times = 3
```

Los default arguments generan múltiples variantes: `repeat/1` y `repeat/2` en este caso.

### Guards — `when`

Los guards filtran cláusulas con condiciones booleanas. Solo funciones puras están permitidas:

```elixir
defmodule NumberUtils do
  def classify(n) when n < 0,  do: :negative
  def classify(0),              do: :zero
  def classify(n) when n > 0,  do: :positive
end

NumberUtils.classify(-5)  # :negative
NumberUtils.classify(0)   # :zero
NumberUtils.classify(42)  # :positive
```

Funciones permitidas en guards: `is_integer/1`, `is_binary/1`, `is_list/1`, `is_map/1`, `is_atom/1`, `is_nil/1`, comparaciones (`==`, `<`, `>`, `>=`, `<=`), operadores lógicos (`and`, `or`, `not`), `rem/2`, `abs/1`, `length/1` en binarios, entre otras. **`String.length/1` y `length/1` en listas no son válidos en guards.**

## Exercises

### Exercise 1: Funciones básicas — forma compacta y bloque

```elixir
defmodule BasicMath do
  # Forma compacta (una sola expresión)
  def add(a, b), do: a + b
  def subtract(a, b), do: a - b
  def multiply(a, b), do: a * b

  # Forma con bloque (múltiples expresiones)
  def divide(a, b) do
    if b == 0 do
      {:error, "division by zero"}
    else
      {:ok, a / b}
    end
  end
end

BasicMath.add(3, 4)         # 7
BasicMath.subtract(10, 3)   # 7
BasicMath.multiply(4, 5)    # 20
BasicMath.divide(10, 2)     # {:ok, 5.0}
BasicMath.divide(10, 0)     # {:error, "division by zero"}
```

**Expected output:**

```
iex> BasicMath.add(3, 4)
7
iex> BasicMath.subtract(10, 3)
7
iex> BasicMath.divide(10, 2)
{:ok, 5.0}
iex> BasicMath.divide(10, 0)
{:error, "division by zero"}
```

---

### Exercise 2: Multiple Clauses — pattern matching en el parámetro

```elixir
defmodule HttpStatus do
  def describe(200), do: "OK"
  def describe(201), do: "Created"
  def describe(404), do: "Not Found"
  def describe(500), do: "Internal Server Error"
  def describe(code), do: "Unknown status: #{code}"

  # Pattern matching con átomos
  def outcome(:ok),    do: "Success"
  def outcome(:error), do: "Failure"
  def outcome(_),      do: "Unknown"
end

HttpStatus.describe(200)   # "OK"
HttpStatus.describe(404)   # "Not Found"
HttpStatus.describe(302)   # "Unknown status: 302"
HttpStatus.outcome(:ok)    # "Success"
HttpStatus.outcome(:other) # "Unknown"
```

**Expected output:**

```
iex> HttpStatus.describe(200)
"OK"
iex> HttpStatus.describe(404)
"Not Found"
iex> HttpStatus.describe(302)
"Unknown status: 302"
iex> HttpStatus.outcome(:ok)
"Success"
iex> HttpStatus.outcome(:other)
"Unknown"
```

---

### Exercise 3: Destruir estructuras en parámetros

```elixir
defmodule ShapeArea do
  # Destruir tuplas directamente en los parámetros
  def area({:square, side}),            do: side * side
  def area({:rect, width, height}),     do: width * height
  def area({:circle, radius}),          do: :math.pi() * radius * radius
  def area({:triangle, base, height}),  do: base * height / 2

  # Destruir map en el parámetro
  def full_name(%{first: first, last: last}), do: "#{first} #{last}"

  # Destruir lista — extraer primer elemento
  def first_item([head | _]), do: head
  def first_item([]),         do: nil
end

ShapeArea.area({:square, 5})         # 25
ShapeArea.area({:rect, 4, 6})        # 24
ShapeArea.area({:circle, 3})         # ~28.27
ShapeArea.full_name(%{first: "John", last: "Doe"})  # "John Doe"
ShapeArea.first_item([10, 20, 30])   # 10
ShapeArea.first_item([])             # nil
```

**Expected output:**

```
iex> ShapeArea.area({:square, 5})
25
iex> ShapeArea.area({:rect, 4, 6})
24
iex> ShapeArea.area({:circle, 3})
28.274333882308138
iex> ShapeArea.full_name(%{first: "John", last: "Doe"})
"John Doe"
iex> ShapeArea.first_item([10, 20, 30])
10
iex> ShapeArea.first_item([])
nil
```

---

### Exercise 4: Default Arguments

```elixir
defmodule Greeter do
  # greeting tiene valor por defecto "Hello"
  def greet(name, greeting \\ "Hello") do
    "#{greeting}, #{name}!"
  end

  # Repetir con default de 1
  def repeat(message, times \\ 1) do
    String.duplicate(message, times)
  end
end

Greeter.greet("Alice")             # "Hello, Alice!"
Greeter.greet("Alice", "Hi")       # "Hi, Alice!"
Greeter.greet("Alice", "Hola")     # "Hola, Alice!"
Greeter.repeat("ha")               # "ha"
Greeter.repeat("ha", 3)            # "hahaha"

# Los default args crean múltiples variantes — arity 1 y arity 2:
# Greeter.greet/1 y Greeter.greet/2 existen como funciones separadas
```

**Expected output:**

```
iex> Greeter.greet("Alice")
"Hello, Alice!"
iex> Greeter.greet("Alice", "Hi")
"Hi, Alice!"
iex> Greeter.greet("Alice", "Hola")
"Hola, Alice!"
iex> Greeter.repeat("ha", 3)
"hahaha"
```

---

### Exercise 5: Guards con `when`

```elixir
defmodule NumberClassifier do
  def factorial(0), do: 1
  def factorial(n) when is_integer(n) and n > 0 do
    n * factorial(n - 1)
  end
  def factorial(_), do: {:error, "Must be a non-negative integer"}

  def sign(n) when n > 0,  do: :positive
  def sign(0),              do: :zero
  def sign(n) when n < 0,  do: :negative

  def classify_list(list) when is_list(list) and list == [], do: :empty
  def classify_list(list) when is_list(list),                do: {:has_elements, length(list)}
  def classify_list(_),                                      do: :not_a_list
end

NumberClassifier.factorial(5)        # 120
NumberClassifier.factorial(0)        # 1
NumberClassifier.factorial(-1)       # {:error, "Must be a non-negative integer"}
NumberClassifier.sign(42)            # :positive
NumberClassifier.sign(0)             # :zero
NumberClassifier.sign(-10)           # :negative
NumberClassifier.classify_list([])   # :empty
NumberClassifier.classify_list([1,2,3])  # {:has_elements, 3}
NumberClassifier.classify_list("x") # :not_a_list
```

**Expected output:**

```
iex> NumberClassifier.factorial(5)
120
iex> NumberClassifier.factorial(0)
1
iex> NumberClassifier.factorial(-1)
{:error, "Must be a non-negative integer"}
iex> NumberClassifier.sign(42)
:positive
iex> NumberClassifier.classify_list([])
:empty
iex> NumberClassifier.classify_list([1, 2, 3])
{:has_elements, 3}
```

---

### Exercise 6: Funciones privadas con `defp`

```elixir
defmodule PasswordChecker do
  # Función pública: punto de entrada
  def validate(password) do
    cond do
      not long_enough?(password) -> {:error, "Too short (min 8 chars)"}
      not has_digit?(password)   -> {:error, "Must contain a digit"}
      true                       -> :ok
    end
  end

  # Funciones privadas: detalles de implementación
  defp long_enough?(str), do: String.length(str) >= 8
  defp has_digit?(str),   do: String.match?(str, ~r/[0-9]/)
end

PasswordChecker.validate("abc")           # {:error, "Too short (min 8 chars)"}
PasswordChecker.validate("abcdefgh")      # {:error, "Must contain a digit"}
PasswordChecker.validate("abcdefg1")      # :ok

# Intentar llamar a una función privada desde afuera:
# PasswordChecker.long_enough?("test")
# ** (UndefinedFunctionError) function PasswordChecker.long_enough?/1 is undefined or private
```

**Expected output:**

```
iex> PasswordChecker.validate("abc")
{:error, "Too short (min 8 chars)"}
iex> PasswordChecker.validate("abcdefgh")
{:error, "Must contain a digit"}
iex> PasswordChecker.validate("abcdefg1")
:ok
iex> PasswordChecker.long_enough?("test")
** (UndefinedFunctionError) function PasswordChecker.long_enough?/1 is undefined or private
```

## Common Mistakes

### Error 1: Confundir funciones con distinta arity

```elixir
# greet/1 y greet/2 son funciones DISTINTAS
defmodule Greeter do
  def greet(name), do: "Hello, #{name}!"
  def greet(name, lang), do: "Hola #{name} en #{lang}"
end

# WRONG: asumir que son "versiones" de la misma función
# Son identificadas como Greeter.greet/1 y Greeter.greet/2

# La notación Module.function/arity es la referencia completa:
fun = &Greeter.greet/1   # captura específicamente la de 1 argumento
fun.("Alice")            # "Hello, Alice!"
```

### Error 2: Argumentos por defecto con múltiples cláusulas

```elixir
# WRONG: definir default en múltiples cláusulas lanza warning o error
defmodule BadGreeter do
  # def greet(name, lang \\ :en), do: "en: #{name}"   # ERROR si hay otra cláusula
  # def greet(name, :es), do: "es: #{name}"
end

# FIX: declarar el default en una cláusula cabecera separada
defmodule GoodGreeter do
  def greet(name, lang \\ :en)  # Solo la declaración, sin body

  def greet(name, :en), do: "Hello, #{name}!"
  def greet(name, :es), do: "Hola, #{name}!"
  def greet(name, _),   do: "Hi, #{name}!"
end
```

### Error 3: Funciones no permitidas en guards

```elixir
# WRONG: String.length/1 no está permitida en guards
# def process(str) when String.length(str) > 10, do: :long

# FIX: usar byte_size/1 (sí permitida) o mover la condición al body
def process(str) when byte_size(str) > 10, do: :long  # aproximación para ASCII
def process(str) do
  if String.length(str) > 10, do: :long, else: :short
end
```

### Error 4: Cláusula más general antes que la específica

```elixir
# WRONG: la cláusula catch-all _ intercepta todo — las siguientes nunca se ejecutan
defmodule BadStatus do
  def describe(_),      do: "unknown"  # demasiado general — primero
  def describe(:ok),    do: "success"  # NUNCA se alcanza
  def describe(:error), do: "failure"  # NUNCA se alcanza
end

# FIX: las cláusulas específicas siempre primero
defmodule GoodStatus do
  def describe(:ok),    do: "success"
  def describe(:error), do: "failure"
  def describe(_),      do: "unknown"  # catch-all al final
end
```

## Verification

```bash
# Crear un archivo de prueba
cat > /tmp/test_functions.exs << 'EOF'
defmodule MathTest do
  def add(a, b), do: a + b

  def factorial(0), do: 1
  def factorial(n) when is_integer(n) and n > 0, do: n * factorial(n - 1)

  def sign(n) when n > 0, do: :positive
  def sign(0),             do: :zero
  def sign(n) when n < 0, do: :negative

  def greet(name, greeting \\ "Hello"), do: "#{greeting}, #{name}!"
end

IO.puts MathTest.add(3, 4)            # 7
IO.puts MathTest.factorial(5)         # 120
IO.puts inspect(MathTest.sign(-3))    # :negative
IO.puts MathTest.greet("Alice")       # Hello, Alice!
IO.puts MathTest.greet("Alice", "Hi") # Hi, Alice!
EOF

elixir /tmp/test_functions.exs
```

**Expected output:**

```
7
120
:negative
Hello, Alice!
Hi, Alice!
```

## Summary

- `def` define funciones públicas, `defp` define funciones privadas — ambas van dentro de módulos
- La **arity** (número de argumentos) es parte del identificador: `greet/1 ≠ greet/2`
- Las cláusulas se evalúan **de arriba a abajo** — pon las más específicas primero
- Los argumentos por defecto (`\\`) generan variantes adicionales de la función
- Los **guards** (`when`) filtran cláusulas con condiciones — solo funciones puras son válidas en guards
- La notación `Module.function/arity` identifica unívocamente cualquier función en Elixir

## What's Next

- **09-control-flow-if-case-cond**: Combinar funciones con `if`, `case` y `cond`
- **10-recursion-and-tail-call-optimization**: Usar multiple clauses para escribir funciones recursivas

## Resources

- [Elixir Docs — def/defp](https://hexdocs.pm/elixir/Kernel.html#def/2)
- [Elixir Getting Started — Modules and Functions](https://elixir-lang.org/getting-started/modules-and-functions.html)
- [Elixir School — Functions](https://elixirschool.com/en/lessons/basics/functions)
- [Elixir Getting Started — Guards](https://elixir-lang.org/getting-started/case-cond-and-if.html#guards)
