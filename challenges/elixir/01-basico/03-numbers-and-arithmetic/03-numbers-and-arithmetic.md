# 3. Numbers and Arithmetic

**Difficulty**: Basico

## Prerequisites
- Haber completado el ejercicio 01-setup-and-mix
- IEx disponible para experimentar

## Learning Objectives
After completing this exercise, you will be able to:
- Distinguir entre integers y floats y cuándo usar cada uno
- Usar los operadores aritméticos correctamente, especialmente `/` vs `div/2`
- Aplicar `rem/2` entendiendo su comportamiento con negativos
- Convertir entre tipos numéricos y strings
- Escribir funciones con guards numéricos

## Concepts

### Integer vs Float: Dos Tipos Distintos
En Elixir, los enteros (`Integer`) y los números de punto flotante (`Float`) son tipos de datos diferentes. Esta distinción es importante porque algunas operaciones retornan tipos inesperados si no estás consciente de ella. Los integers tienen precisión arbitraria — pueden ser tan grandes como la memoria lo permita, sin overflow. Los floats siguen el estándar IEEE 754 de 64 bits.

Los integers se escriben sin punto decimal: `42`, `0`, `-17`, `1_000_000` (el guión bajo es un separador visual). Los floats siempre tienen un punto decimal: `3.14`, `0.0`, `-2.5`, `1.0e10` (notación científica).

```elixir
# Integers — precisión arbitraria
42
-17
1_000_000        # equivale a 1000000 — el _ es solo visual
0xFF             # hexadecimal: 255
0b1010           # binario: 10
0o777            # octal: 511

# Floats — IEEE 754 de 64 bits
3.14
-2.5
1.0e10           # notación científica: 10_000_000_000.0
1.5e-3           # 0.0015

# Verificar tipos
is_integer(42)   # true
is_float(3.14)   # true
is_number(42)    # true — is_number acepta ambos tipos
```

### Operadores Aritméticos: La Trampa de /
Elixir tiene dos formas de división y es crítico entender la diferencia. El operador `/` siempre retorna un `Float`, incluso cuando ambos operandos son integers y la división es exacta. Para obtener un integer, debes usar `div/2`.

Esta decisión de diseño evita bugs sutiles donde `10 / 2` retorna `5` (integer) en algunos lenguajes pero `10 / 3` retorna `3` (truncado silenciosamente). En Elixir, `/` siempre es división real y `div/2` siempre es división entera — sin ambigüedad.

```elixir
# + - * se comportan como esperas
10 + 5    # 15
10 - 3    # 7
4 * 5     # 20

# / SIEMPRE retorna float
10 / 2    # 5.0  — NO 5
10 / 3    # 3.3333333333333335
9 / 3     # 3.0  — NO 3

# div/2 — división entera, trunca hacia cero
div(10, 3)    # 3
div(10, 2)    # 5
div(-10, 3)   # -3  — trunca hacia cero, NO hacia -infinito

# rem/2 — resto de división entera
rem(10, 3)    # 1
rem(10, 2)    # 0
rem(-7, 3)    # -1  — el signo es el del dividendo

# Potencia (Elixir 1.13+)
2 ** 10   # 1024
2 ** -1   # 0.5  — retorna float cuando el exponente es negativo
```

### Conversión entre Tipos Numéricos
Frecuentemente necesitarás convertir entre integers, floats, y strings. Elixir provee funciones explícitas para esto — no hay coerciones implícitas entre tipos numéricos y strings.

```elixir
# Integer a Float
Integer.to_float(42)    # No existe — usa *1.0 o trunc al revés
42 * 1.0                # 42.0
42 / 1                  # 42.0 — también funciona pero es menos claro

# Float a Integer — varias formas con distinto comportamiento
trunc(3.9)      # 3  — trunca hacia cero
round(3.5)      # 4  — redondea al más cercano (banker's rounding para .5)
floor(3.9)      # 3  — redondea hacia -infinito
ceil(3.1)       # 4  — redondea hacia +infinito

# Integer a String
Integer.to_string(42)        # "42"
Integer.to_string(255, 16)   # "FF"  — base 16
Integer.to_string(10, 2)     # "1010" — base 2

# String a Integer
String.to_integer("42")      # 42
String.to_integer("FF", 16)  # 255

# Float a String
Float.to_string(3.14159)     # "3.14159"
Float.round(3.14159, 2)      # 3.14  — redondea a 2 decimales
```

### Guards Numéricos
Los guards son condiciones que puedes agregar a las definiciones de funciones y cláusulas `case` / `cond`. Los guards numéricos son especialmente útiles para validar que un argumento cumple cierta condición antes de ejecutar el cuerpo de la función.

```elixir
# Guard básico: la función solo acepta enteros positivos
def factorial(0), do: 1
def factorial(n) when is_integer(n) and n > 0 do
  n * factorial(n - 1)
end

# Guards numéricos disponibles:
# is_integer/1, is_float/1, is_number/1
# >, <, >=, <=, ==, !=
# abs/1, div/2, rem/2 (pueden usarse en guards)

# Verificar si un número está en rango
def in_range?(n, min, max) when is_number(n) and n >= min and n <= max, do: true
def in_range?(_, _, _), do: false
```

## Exercises

### Exercise 1: Basic Arithmetic in IEx
Abre IEx y experimenta con las operaciones aritméticas básicas. Presta atención al tipo retornado por cada operación.

```elixir
$ iex

# Suma, resta, multiplicación
iex> 10 + 5
15

iex> 10 - 3
7

iex> 4 * 5
20

# División — observa que retorna float
iex> 10 / 2
5.0

iex> 10 / 3
3.3333333333333335

# Verificar tipos
iex> is_integer(10 + 5)
true

iex> is_float(10 / 2)
true

# Operaciones con notaciones especiales
iex> 1_000_000 + 1_000
1001000

iex> 0xFF + 1
256

iex> 0b1111 + 1
16
```

Expected output:
```
iex> 10 / 2
5.0
iex> is_float(10 / 2)
true
iex> 10 / 3
3.3333333333333335
```

### Exercise 2: Integer Division with div/2 and rem/2
Aprende la diferencia entre la división entera (`div`) y el operador `/`, y entiende cómo `rem` maneja los negativos.

```elixir
# div/2 — siempre retorna integer, trunca hacia cero
iex> div(10, 3)
3

iex> div(10, 2)
5

iex> div(7, 7)
1

# rem/2 — resto, el signo sigue al dividendo
iex> rem(10, 3)
1

iex> rem(10, 2)
0

iex> rem(15, 4)
3

# Comportamiento con negativos — importante conocer
iex> div(-10, 3)
-3

iex> rem(-7, 3)
-1

iex> rem(7, -3)
1

# Verificar la relación: n == div(n, d) * d + rem(n, d)
iex> n = 17; d = 5; div(n, d) * d + rem(n, d) == n
true

# Uso práctico: saber si un número es par
iex> rem(10, 2) == 0
true

iex> rem(7, 2) == 0
false
```

Expected output:
```
iex> div(10, 3)
3
iex> rem(10, 3)
1
iex> rem(-7, 3)
-1
```

### Exercise 3: Power Operator and Math Functions
Elixir 1.13 introdujo el operador `**` para potencias. También existe el módulo `:math` de Erlang para funciones matemáticas avanzadas.

```elixir
# Operador de potencia **
iex> 2 ** 8
256

iex> 2 ** 10
1024

iex> 10 ** 3
1000

# Exponente negativo — retorna float
iex> 2 ** -1
0.5

iex> 10 ** -2
0.01

# Float base
iex> 2.0 ** 3
8.0

# Módulo :math de Erlang para funciones avanzadas
iex> :math.sqrt(16.0)
4.0

iex> :math.pow(2, 10)
1024.0

iex> :math.pi()
3.141592653589793

iex> :math.log(2.718281828)
0.9999999998311266

iex> :math.cos(0)
1.0
```

Expected output:
```
iex> 2 ** 8
256
iex> 2 ** -1
0.5
iex> :math.sqrt(16.0)
4.0
```

### Exercise 4: Numeric Conversions
Practica la conversión entre tipos numéricos y strings usando las funciones de los módulos `Integer`, `Float`, y `String`.

```elixir
# Integer a String
iex> Integer.to_string(42)
"42"

iex> Integer.to_string(255, 16)
"FF"

iex> Integer.to_string(10, 2)
"1010"

# String a Integer
iex> String.to_integer("42")
42

iex> String.to_integer("FF", 16)
255

iex> String.to_integer("1010", 2)
10

# Float a Integer (diferentes modos de redondeo)
iex> trunc(3.9)
3

iex> trunc(-3.9)
-3

iex> round(3.5)
4

iex> round(3.4)
3

iex> floor(3.9)
3

iex> ceil(3.1)
4

# Float con precisión controlada
iex> Float.round(3.14159265, 2)
3.14

iex> Float.round(3.14159265, 4)
3.1416

# Float a String
iex> Float.to_string(3.14)
"3.14"
```

Expected output:
```
iex> Integer.to_string(42)
"42"
iex> String.to_integer("42")
42
iex> Float.round(3.14159265, 2)
3.14
iex> trunc(3.9)
3
```

### Exercise 5: Number Utility Functions
Explora las funciones matemáticas disponibles directamente en el kernel de Elixir (sin prefijo de módulo).

```elixir
# abs/1 — valor absoluto
iex> abs(-5)
5

iex> abs(-3.14)
3.14

iex> abs(0)
0

# max/2 y min/2 — máximo y mínimo
iex> max(3, 7)
7

iex> max(3.14, 3)
3.14

iex> min(3, 7)
3

iex> min(-5, 0)
-5

# Comparaciones numéricas — funciona entre integers y floats
iex> 3 == 3.0
true

iex> 3 === 3.0
false

iex> 5 > 4
true

iex> 5 >= 5
true

# Uso combinado
iex> abs(max(-10, -3))
3

iex> min(abs(-5), abs(-3))
3
```

Expected output:
```
iex> abs(-5)
5
iex> max(3, 7)
7
iex> min(-5, 0)
-5
iex> 3 == 3.0
true
iex> 3 === 3.0
false
```

### Exercise 6: Guards — Funciones con Restricciones Numéricas
Crea un archivo `numbers.ex` y define funciones usando guards para validar los argumentos numéricos.

```elixir
# Crea el archivo lib/numbers.ex en tu proyecto hello_elixir
defmodule Numbers do
  @moduledoc """
  Funciones de demostración de guards numéricos en Elixir.
  """

  @doc """
  Retorna true si n es positivo (> 0), false en caso contrario.

  ## Examples

      iex> Numbers.positive?(5)
      true

      iex> Numbers.positive?(-1)
      false

      iex> Numbers.positive?(0)
      false

  """
  def positive?(n) when is_number(n) and n > 0, do: true
  def positive?(n) when is_number(n), do: false

  @doc """
  Calcula el factorial de n. Solo acepta integers no negativos.

  ## Examples

      iex> Numbers.factorial(0)
      1

      iex> Numbers.factorial(5)
      120

  """
  def factorial(0), do: 1
  def factorial(n) when is_integer(n) and n > 0, do: n * factorial(n - 1)

  @doc """
  Clasifica un número como :positive, :negative, o :zero.

  ## Examples

      iex> Numbers.classify(5)
      :positive

      iex> Numbers.classify(-3)
      :negative

      iex> Numbers.classify(0)
      :zero

  """
  def classify(n) when is_number(n) and n > 0, do: :positive
  def classify(n) when is_number(n) and n < 0, do: :negative
  def classify(0), do: :zero
  def classify(0.0), do: :zero
end
```

```bash
$ iex -S mix
```

```elixir
iex> Numbers.positive?(5)
true

iex> Numbers.positive?(-1)
false

iex> Numbers.factorial(5)
120

iex> Numbers.factorial(0)
1

iex> Numbers.classify(42)
:positive

iex> Numbers.classify(-3.14)
:negative

iex> Numbers.classify(0)
:zero
```

Expected output:
```
iex> Numbers.positive?(5)
true
iex> Numbers.factorial(5)
120
iex> Numbers.classify(42)
:positive
```

## Common Mistakes

### Mistake 1: Usar / esperando un integer
**Wrong:**
```elixir
# Calcular la mitad de una lista para dividirla en dos partes
def split_in_half(list) do
  half = length(list) / 2  # Retorna float!
  Enum.split(list, half)   # Error: Enum.split espera integer
end
```
**Error:** `** (FunctionClauseError) no function clause matching in Enum.split/2` — `Enum.split` espera un integer, no un float.
**Why:** `/` siempre retorna float en Elixir. `6 / 2` es `3.0`, no `3`.
**Fix:**
```elixir
def split_in_half(list) do
  half = div(length(list), 2)  # div/2 retorna integer
  Enum.split(list, half)
end
```

### Mistake 2: rem vs mod — comportamiento con negativos
**Wrong:**
```elixir
# Intentar hacer "modulo real" para trabajar con ángulos o ciclos
def normalize_angle(degrees) do
  rem(degrees, 360)  # INCORRECTO para ángulos negativos
end

# normalize_angle(-90) retorna -90, no 270
```
**Error:** No hay error de compilación. El problema es lógico: `rem(-90, 360)` retorna `-90`, pero el equivalente positivo en el círculo es `270`.
**Why:** `rem/2` preserva el signo del dividendo (como `%` en C/Java). El "modulo matemático" siempre retorna positivo.
**Fix:**
```elixir
def normalize_angle(degrees) do
  result = rem(degrees, 360)
  # Si es negativo, sumar 360 para obtener el equivalente positivo
  if result < 0, do: result + 360, else: result
end

# normalize_angle(-90) ahora retorna 270
```

### Mistake 3: Precisión de floats — IEEE 754
**Wrong:**
```elixir
# Comparar floats directamente esperando exactitud
iex> 0.1 + 0.2 == 0.3
false

# Esto sorprende a muchos programadores nuevos
iex> 0.1 + 0.2
0.30000000000000004
```
**Error:** No es un error de Elixir — es una propiedad fundamental de los floats IEEE 754. No todas las fracciones decimales tienen representación exacta en binario.
**Why:** `0.1` en binario es una fracción infinita repetitiva, como `1/3` en decimal. Al sumar dos aproximaciones, el error se acumula.
**Fix:**
```elixir
# Opción 1: comparar con tolerancia (epsilon)
def float_equal?(a, b, epsilon \\ 1.0e-10) do
  abs(a - b) < epsilon
end

iex> float_equal?(0.1 + 0.2, 0.3)
true

# Opción 2: trabajar con integers (multiplicar por el factor necesario)
# En lugar de $3.14, usar 314 centavos
price_cents = 314
tax_cents = round(price_cents * 0.21)
total_cents = price_cents + tax_cents
```

## Verification
```bash
$ iex
iex> div(10, 3)
3
iex> rem(10, 3)
1
iex> 2 ** 8
256
iex> Float.round(3.14159, 2)
3.14
iex> abs(-42)
42
iex> String.to_integer("100")
100
```

## Summary
- **Key concepts**: Integer vs Float como tipos distintos, `/` siempre retorna float, `div/2` para división entera, `rem/2` para resto, guards numéricos
- **What you practiced**: Operaciones aritméticas básicas, diferencia entre `/` y `div/2`, conversiones entre integers/floats/strings, funciones con guards numéricos
- **Important to remember**: `10 / 2` retorna `5.0` (float), no `5` (integer). Usa `div(10, 2)` para obtener `5`. Y recuerda que floats tienen precisión limitada — nunca los compares directamente con `==`.

## What's Next
En el siguiente ejercicio **04-strings-and-binaries-basics** aprenderás que los strings en Elixir son binarios UTF-8, la diferencia entre strings y charlists, y cómo usar interpolación y las funciones del módulo `String`.

## Resources
- [The Elixir Getting Started Guide — Basic Types](https://elixir-lang.org/getting-started/basic-types.html)
- [Elixir Docs - Integer](https://hexdocs.pm/elixir/Integer.html)
- [Elixir Docs - Float](https://hexdocs.pm/elixir/Float.html)
- [IEEE 754 Floating Point](https://floating-point-gui.de/)
