# 4. Strings and Binaries Basics

**Difficulty**: Basico

## Prerequisites
- Haber completado el ejercicio 01-setup-and-mix
- Familiaridad básica con atoms (ejercicio 02)
- IEx disponible

## Learning Objectives
After completing this exercise, you will be able to:
- Crear strings UTF-8 y verificar su encoding con funciones del módulo `String`
- Usar interpolación de strings con `#{}`
- Concatenar strings con el operador `<>`
- Distinguir entre strings (binarios) y charlists (listas de codepoints)
- Aplicar las funciones más comunes del módulo `String`

## Concepts

### Strings son Binarios UTF-8
En Elixir, los strings se escriben entre comillas dobles y son secuencias de bytes UTF-8. No son arrays de caracteres como en C, ni objetos como en Java. Son binarios — secuencias de bytes — donde cada "carácter" puede ocupar entre 1 y 4 bytes dependiendo del codepoint Unicode.

Esta representación tiene una implicación importante: `String.length/1` retorna el número de graphemes (caracteres visibles), mientras que `byte_size/1` retorna el número de bytes. Para texto ASCII puro son iguales, pero para texto con acentos, caracteres asiáticos, o emojis, pueden diferir significativamente.

```elixir
# Strings básicos
"hello"
"Hello, World!"
"Café"           # UTF-8: 'é' ocupa 2 bytes
"こんにちは"      # UTF-8: cada carácter ocupa 3 bytes
"🎉"             # UTF-8: emoji ocupa 4 bytes

# String.length cuenta graphemes (caracteres visibles)
String.length("hello")    # 5
String.length("Café")     # 4  — cuatro letras visibles
String.length("🎉")       # 1  — un emoji

# byte_size cuenta bytes raw
byte_size("hello")    # 5   — ASCII: 1 byte por carácter
byte_size("Café")     # 5   — 'C','a','f' = 3 bytes, 'é' = 2 bytes
byte_size("🎉")       # 4   — emoji usa 4 bytes en UTF-8
```

### Charlists: El Otro Tipo de "String"
Elixir hereda de Erlang el tipo charlist — una lista de integers donde cada integer es el codepoint Unicode de un carácter. Se escriben entre comillas simples: `'hello'`. Son fundamentalmente diferentes a los strings de doble comilla.

Los charlists son necesarios cuando interactúas con APIs de Erlang — muchas funciones de Erlang esperan charlists, no binarios. En código Elixir puro, usa siempre strings de doble comilla. Solo usa charlists cuando una librería Erlang lo requiera.

```elixir
# Charlist — comilla simple
'hello'
# equivale a [104, 101, 108, 108, 111] — lista de codepoints

# String — comilla doble
"hello"
# es un binario: <<104, 101, 108, 108, 111>>

# Son tipos completamente distintos
is_list('hello')      # true  — charlist es una lista
is_binary("hello")    # true  — string es un binario

# IEx muestra charlists como strings si todos los codepoints son ASCII imprimibles
# Esto puede ser confuso al principio
[104, 101, 108, 108, 111]  # IEx muestra: 'hello'

# Convertir entre ambos
to_charlist("hello")    # 'hello' => [104, 101, 108, 108, 111]
to_string('hello')      # "hello"
List.to_string('hello') # "hello"
```

### Interpolación de Strings
La interpolación permite insertar el valor de cualquier expresión Elixir dentro de un string usando la sintaxis `#{expresión}`. La expresión dentro de `#{}` puede ser cualquier código Elixir válido — variables, llamadas a funciones, operaciones aritméticas, etc.

```elixir
name = "Alice"
age = 30

# Interpolación básica
"Hello, #{name}!"
# "Hello, Alice!"

# Expresiones complejas dentro de #{}
"#{name} will be #{age + 1} next year"
# "Alice will be 31 next year"

# Llamadas a funciones
"Name in uppercase: #{String.upcase(name)}"
# "Name in uppercase: ALICE"

# Cualquier valor se convierte a string automáticamente via to_string/1
"Pi is approximately #{:math.pi()}"
# "Pi is approximately 3.141592653589793"

"Today's status: #{:active}"
# "Today's status: active"
```

### Concatenación con <>
El operador `<>` concatena binarios (strings). Es equivalente al `+` de Python o Java para strings, pero con un nombre diferente para evitar confusión con la suma aritmética.

```elixir
"Hello" <> " " <> "World"
# "Hello World"

first = "John"
last = "Doe"
full_name = first <> " " <> last
# "John Doe"

# Concatenar en un loop o con Enum.join es más eficiente que <> repetido
words = ["Hello", "from", "Elixir"]
Enum.join(words, " ")
# "Hello from Elixir"
```

### Funciones Esenciales del Módulo String
El módulo `String` provee funciones para manipular strings UTF-8 de forma correcta — respetando la codificación y los graphemes compuestos.

```elixir
# Transformaciones de caso
String.upcase("hello")          # "HELLO"
String.downcase("HELLO")        # "hello"
String.capitalize("hello world") # "Hello world"

# Longitud y contenido
String.length("hello")          # 5
String.contains?("hello", "ell") # true
String.starts_with?("hello", "he") # true
String.ends_with?("hello", "lo")   # true

# División y unión
String.split("hello world", " ")  # ["hello", "world"]
String.split("a,b,c", ",")        # ["a", "b", "c"]
Enum.join(["a", "b", "c"], "-")    # "a-b-c"

# Recorte de espacios
String.trim("  hello  ")          # "hello"
String.trim_leading("  hello  ")  # "hello  "
String.trim_trailing("  hello  ") # "  hello"

# Reemplazar
String.replace("hello world", "world", "Elixir")  # "hello Elixir"

# Invertir
String.reverse("hello")  # "olleh"

# Obtener parte de un string
String.slice("hello world", 0, 5)  # "hello"
String.slice("hello world", 6, 5)  # "world"
```

### Heredocs — Strings Multilínea
Para strings que abarcan múltiples líneas, Elixir ofrece la sintaxis heredoc con triple comilla.

```elixir
# Heredoc básico
message = """
Hola, Elixir!
Este es un string
de múltiples líneas.
"""

# El heredoc elimina el indentado base automáticamente
query = """
  SELECT *
  FROM users
  WHERE active = true
  """

# También existe ~s para heredocs con escape
html = ~s"""
<div class="container">
  <p>Hello, #{name}!</p>
</div>
"""
```

## Exercises

### Exercise 1: Creating Strings and UTF-8
Experimenta con strings UTF-8 y la diferencia entre `String.length/1` y `byte_size/1`.

```elixir
$ iex

# String ASCII simple
iex> "Hello, Elixir!"
"Hello, Elixir!"

iex> String.length("Hello, Elixir!")
14

iex> byte_size("Hello, Elixir!")
14

# String con caracteres UTF-8 multibyte
iex> "Café"
"Café"

iex> String.length("Café")
4

iex> byte_size("Café")
5

# Verificar que es un binario
iex> is_binary("Hello")
true

# Inspeccionar los bytes raw de un string
iex> :binary.bin_to_list("café")
[99, 97, 102, 195, 169]

# 'é' es dos bytes: 195, 169 (0xC3, 0xA9) en UTF-8
```

Expected output:
```
iex> String.length("Café")
4
iex> byte_size("Café")
5
iex> is_binary("Café")
true
```

### Exercise 2: String Interpolation
Practica la interpolación usando variables, cálculos, y llamadas a funciones.

```elixir
# Definir variables
iex> name = "Alice"
"Alice"

iex> birth_year = 1990
1990

# Interpolación básica
iex> "Hello, #{name}!"
"Hello, Alice!"

# Interpolación con expresión aritmética
iex> "Hello, #{name}! You are #{2026 - birth_year} years old."
"Hello, Alice! You are 36 years old."

# Interpolación con llamada a función
iex> "Name in uppercase: #{String.upcase(name)}"
"Name in uppercase: ALICE"

# Interpolación con atom (se convierte a string automáticamente)
iex> status = :active
iex> "User status: #{status}"
"User status: active"

# Interpolación anidada (no recomendada, pero funciona)
iex> items = 3
iex> "You have #{items} item#{if items == 1, do: "", else: "s"}."
"You have 3 items."
```

Expected output:
```
iex> "Hello, #{name}! You are #{2026 - birth_year} years old."
"Hello, Alice! You are 36 years old."
```

### Exercise 3: Concatenation with <>
Usa el operador `<>` para construir strings a partir de partes más pequeñas.

```elixir
# Concatenación básica
iex> "Hello" <> " " <> "World"
"Hello World"

# Construir un nombre completo
iex> first = "John"
"John"

iex> last = "Doe"
"Doe"

iex> full_name = first <> " " <> last
"John Doe"

# Concatenar con números — debes convertir primero
iex> "Answer: " <> Integer.to_string(42)
"Answer: 42"

# Alternativa más idiomática: interpolación
iex> "Answer: #{42}"
"Answer: 42"

# Concatenar muchos items — Enum.join es más eficiente
iex> parts = ["Elixir", "is", "awesome"]
iex> Enum.join(parts, " ")
"Elixir is awesome"

# <> en pattern matching — extraer prefijo o sufijo
iex> "Hello" <> rest = "Hello World"
"Hello World"

iex> rest
" World"
```

Expected output:
```
iex> "Hello" <> " " <> "World"
"Hello World"
iex> first <> " " <> last
"John Doe"
iex> Enum.join(["Elixir", "is", "awesome"], " ")
"Elixir is awesome"
```

### Exercise 4: String Functions
Practica las funciones más útiles del módulo `String`.

```elixir
# Transformaciones de caso
iex> String.upcase("hello world")
"HELLO WORLD"

iex> String.downcase("HELLO WORLD")
"hello world"

iex> String.capitalize("hello world")
"Hello world"

# División
iex> String.split("hello world elixir", " ")
["hello", "world", "elixir"]

iex> String.split("a,b,,c", ",")
["a", "b", "", "c"]

# Eliminar partes vacías
iex> String.split("a,b,,c", ",", trim: true)
["a", "b", "c"]

# Búsqueda y verificación
iex> String.contains?("hello world", "world")
true

iex> String.starts_with?("hello", "he")
true

iex> String.ends_with?("hello", "lo")
true

# Recorte y reemplazo
iex> String.trim("   hello   ")
"hello"

iex> String.replace("hello world", "world", "Elixir")
"hello Elixir"

# Invertir y rebanar
iex> String.reverse("hello")
"olleh"

iex> String.slice("hello world", 6, 5)
"world"
```

Expected output:
```
iex> String.upcase("hello world")
"HELLO WORLD"
iex> String.split("a,b,c", ",")
["a", "b", "c"]
iex> String.trim("   hello   ")
"hello"
iex> String.reverse("hello")
"olleh"
```

### Exercise 5: Charlists vs Strings
Entiende la diferencia entre charlists (comilla simple) y strings (comilla doble).

```elixir
# Charlist — comilla simple
iex> 'hello'
'hello'

iex> is_list('hello')
true

iex> is_binary('hello')
false

# String — comilla doble
iex> "hello"
"hello"

iex> is_binary("hello")
true

iex> is_list("hello")
false

# La diferencia interna
iex> 'hello' == [104, 101, 108, 108, 111]
true

# IEx puede mostrar listas como charlists si todos los valores son ASCII imprimibles
iex> [104, 101, 108, 108, 111]
'hello'

# Conversión entre tipos
iex> to_charlist("hello")
'hello'

iex> to_string('hello')
"hello"

# Una función que espera string rechaza charlist
iex> String.upcase('hello')
# ** (FunctionClauseError) — String.upcase espera un binary, no una lista

# Solución: convertir primero
iex> String.upcase(to_string('hello'))
"HELLO"
```

Expected output:
```
iex> is_list('hello')
true
iex> is_binary("hello")
true
iex> to_charlist("hello")
'hello'
iex> to_string('hello')
"hello"
```

### Exercise 6: String Inspection — Graphemes y Bytes
Explora la representación interna de strings UTF-8 con las funciones avanzadas del módulo `String`.

```elixir
# Graphemes — unidades visibles de texto (puede ser más de un codepoint)
iex> String.graphemes("café")
["c", "a", "f", "é"]

iex> String.graphemes("hello")
["h", "e", "l", "l", "o"]

# Codepoints — valores Unicode de cada carácter
iex> String.codepoints("café")
["c", "a", "f", "é"]

# Length vs byte_size
iex> String.length("café")
4

iex> byte_size("café")
5

# Carácter especial — emoji
iex> String.length("🎉 party")
7

iex> byte_size("🎉 party")
10

# Iterar sobre graphemes
iex> "café" |> String.graphemes() |> Enum.each(&IO.puts/1)
c
a
f
é
:ok

# Verificar si un string es válido UTF-8
iex> String.valid?("hello")
true

iex> String.valid?(<<0xFF, 0xFE>>)
false

# Contar ocurrencias
iex> "hello world hello" |> String.split("hello") |> length() |> Kernel.-(1)
2
```

Expected output:
```
iex> String.graphemes("café")
["c", "a", "f", "é"]
iex> String.length("café")
4
iex> byte_size("café")
5
iex> String.valid?("hello")
true
```

## Common Mistakes

### Mistake 1: Usar + para concatenar strings
**Wrong:**
```elixir
iex> "hello" + " world"
```
**Error:** `** (ArithmeticError) bad argument in arithmetic expression`
**Why:** En Elixir, `+` es solo para números. No hay sobrecarga de operadores en el lenguaje, y strings no son números. El compilador no convierte strings a números automáticamente.
**Fix:**
```elixir
# Usar el operador <> para concatenación
iex> "hello" <> " world"
"hello world"

# O usar interpolación
iex> greeting = "hello"
iex> "#{greeting} world"
"hello world"
```

### Mistake 2: Confundir charlist con string al pasar a funciones
**Wrong:**
```elixir
# Intentar usar funciones String con un charlist
iex> name = 'Alice'    # charlist (comilla simple)
iex> String.upcase(name)
```
**Error:** `** (FunctionClauseError) no function clause matching in String.upcase/1` — `String.upcase` espera un binario, no una lista.
**Why:** `'Alice'` es una lista de integers `[65, 108, 105, 99, 101]`, no un binario. Las funciones del módulo `String` trabajan exclusivamente con binarios.
**Fix:**
```elixir
# Opción 1: usar comillas dobles desde el principio
name = "Alice"
String.upcase(name)   # "ALICE"

# Opción 2: convertir la charlist a string
name = 'Alice'
String.upcase(to_string(name))   # "ALICE"
```

### Mistake 3: String.length != byte_size para UTF-8
**Wrong:**
```elixir
# Truncar a 10 caracteres usando byte_size para calcular
def truncate(str, max_chars) do
  if byte_size(str) > max_chars do
    binary_part(str, 0, max_chars) <> "..."  # INCORRECTO para UTF-8
  else
    str
  end
end

# truncate("café résumé", 5) puede cortar en medio de un carácter multibyte
# produciendo un string UTF-8 inválido
```
**Error:** No siempre hay error inmediato, pero el resultado puede ser un binario inválido o con caracteres cortados.
**Why:** `byte_size/1` cuenta bytes, no caracteres. Un solo grapheme como `é` ocupa 2 bytes. Cortar en el byte 5 de "café" crea un binario inválido.
**Fix:**
```elixir
def truncate(str, max_chars) do
  if String.length(str) > max_chars do
    # String.slice respeta los límites de graphemes
    String.slice(str, 0, max_chars) <> "..."
  else
    str
  end
end

# Ahora truncate("café résumé", 5) retorna "café ..."
```

## Verification
```bash
$ iex
iex> String.upcase("hello elixir")
"HELLO ELIXIR"
iex> "Hello, #{"World"}!"
"Hello, World!"
iex> "Hello" <> " " <> "World"
"Hello World"
iex> String.length("café")
4
iex> byte_size("café")
5
iex> is_list('hello')
true
iex> is_binary("hello")
true
iex> String.split("a,b,c", ",")
["a", "b", "c"]
```

## Summary
- **Key concepts**: Strings son binarios UTF-8, `String.length/1` vs `byte_size/1`, interpolación con `#{}`, concatenación con `<>`, charlists como alternativa de Erlang
- **What you practiced**: Crear strings UTF-8, interpolar expresiones, concatenar con `<>`, usar `String.upcase/downcase/split/trim/replace`, distinguir charlist de string
- **Important to remember**: Siempre usa comillas dobles `"..."` para strings en Elixir. Las comillas simples `'...'` crean charlists (listas de integers), no strings. `String.length("café") == 4` pero `byte_size("café") == 5` — UTF-8 es multibyte.

## What's Next
En el siguiente ejercicio **05-tuples-and-pattern-matching-intro** aprenderás sobre las tuplas y el operador de match `=` como fundamento del paradigma de Elixir. Entenderás por qué `=` en Elixir no es asignación sino unificación.

## Resources
- [The Elixir Getting Started Guide — Strings](https://elixir-lang.org/getting-started/binaries-strings-and-char-lists.html)
- [Elixir Docs - String](https://hexdocs.pm/elixir/String.html)
- [Unicode in Elixir](https://elixir-lang.org/blog/2013/04/17/elixir-v0-8-0-released/)
