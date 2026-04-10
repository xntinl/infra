# 5. Tuples and Pattern Matching Intro

**Difficulty**: Basico

## Prerequisites
- Haber completado los ejercicios 01 al 04
- Familiaridad con atoms (ejercicio 02) — el patrón `{:ok, value}` es central aquí
- IEx disponible

## Learning Objectives
After completing this exercise, you will be able to:
- Crear tuplas y acceder a sus elementos con `elem/2`
- Usar el operador `=` como match (unificación), no como asignación
- Desestructurar tuplas para extraer valores en variables
- Aplicar el pin operator `^` para evitar rebinding
- Ignorar valores irrelevantes con el wildcard `_`

## Concepts

### Tuplas: Colecciones de Tamaño Fijo
Una tupla es una colección ordenada de elementos de tamaño fijo. Se crea con llaves `{}` y puede contener elementos de cualquier tipo — números, atoms, strings, otras tuplas. Su tamaño se determina en tiempo de compilación y no cambia.

Las tuplas se almacenan de forma contigua en la BEAM heap, lo que hace que el acceso por índice sea O(1) y extremadamente rápido. Por otro lado, agregar o quitar elementos de una tupla requiere crear una nueva — por eso no son apropiadas para colecciones dinámicas. Para eso están las listas.

```elixir
# Tuplas de distintos tamaños y tipos
{}                              # tupla vacía
{1, 2, 3}                       # tupla de integers
{:ok, 42}                       # el patrón ok/value
{:error, "not found"}           # el patrón error/reason
{:error, :timeout}              # reason como atom
{"Alice", 30, :admin}           # mezcla de tipos
{{1, 2}, {3, 4}}                # tuplas anidadas

# Acceder elementos por índice (base 0)
elem({:ok, 42}, 0)     # :ok
elem({:ok, 42}, 1)     # 42

# Tamaño de la tupla
tuple_size({1, 2, 3})  # 3
tuple_size({:ok})      # 1
```

### El Operador = : Match, No Asignación
Este es el concepto más importante de Elixir y el que más diferencia al lenguaje de los lenguajes imperativos. En Elixir, `=` es el **operador de match**. Cuando escribes `x = 5`, Elixir intenta hacer que el lado izquierdo "coincida" con el lado derecho. Si el lado izquierdo es una variable no asignada, el match siempre tiene éxito y la variable se liga al valor.

El binding de variables es un efecto secundario del match, no su propósito principal. El propósito del `=` es verificar que una estructura coincide con un patrón. Esto se llama **unificación** en la terminología de lenguajes de programación lógica.

```elixir
# En lenguajes imperativos, = asigna:
# x = 5  (Python/Java) → x recibe el valor 5

# En Elixir, = intenta hacer match:
x = 5     # Match exitoso: x se liga a 5

# El match funciona en ambas direcciones conceptualmente
5 = x     # Match exitoso: 5 == 5 ✓
6 = x     # ** (MatchError) — 6 no coincide con 5

# El poder del match: extraer partes de una estructura
{a, b, c} = {1, 2, 3}   # a = 1, b = 2, c = 3
```

### Desestructuración: Extraer Valores
El match no solo verifica igualdad — también puede extraer valores de estructuras complejas en variables. Esto se llama **desestructuración** o **destructuring**.

La desestructuración es la forma idiomática de Elixir para acceder a los componentes de una tupla. En lugar de `elem(result, 1)` para acceder al segundo elemento, se usa el match `{:ok, value} = result` — que además verifica que el primer elemento es `:ok`.

```elixir
# Desestructuración básica
{a, b} = {1, 2}
# a = 1, b = 2

# El atom en el match actúa como guardia — debe coincidir exactamente
{:ok, value} = {:ok, 42}
# value = 42

# Si el atom no coincide, el match falla
{:ok, value} = {:error, "oops"}
# ** (MatchError) no match of right hand side value: {:error, "oops"}

# Desestructuración anidada
{:ok, {name, age}} = {:ok, {"Alice", 30}}
# name = "Alice", age = 30

# Uso real: procesar resultado de una función
case File.read("config.txt") do
  {:ok, content} -> process(content)
  {:error, reason} -> handle_error(reason)
end
```

### El Pin Operator ^: Match sin Rebinding
Por defecto, cuando una variable aparece en el lado izquierdo de `=`, Elixir la rebinda al nuevo valor. El pin operator `^` previene el rebinding — fuerza a que la variable se use como valor fijo para el match.

El `^` se pronuncia "pin" y su nombre viene de la idea de "fijar" (pin) una variable a su valor actual para que no cambie durante el match.

```elixir
x = 1

# Sin pin: x se rebinda a 2
x = 2
# x es ahora 2

x = 1

# Con pin: x mantiene su valor 1 y se usa para el match
^x = 1    # Match exitoso: 1 == 1 ✓
^x = 2    # ** (MatchError) — 2 no coincide con x que vale 1

# Caso de uso: verificar que una respuesta contiene el ID esperado
expected_id = 42
{:ok, ^expected_id} = {:ok, 42}   # Match exitoso
{:ok, ^expected_id} = {:ok, 99}   # ** MatchError — el ID no es el esperado

# En listas (se verá en ejercicios posteriores)
y = 5
[^y, second] = [5, 10]
# second = 10

[^y, second] = [99, 10]
# ** (MatchError)
```

### Wildcard _: Ignorar Valores
El wildcard `_` es un placeholder especial que siempre hace match con cualquier valor pero no lo liga a ninguna variable. Se usa cuando necesitas el match para que funcione pero no te importa el valor en esa posición.

También puedes usar variables que empiezan con `_` (como `_reason`, `_id`) — estas hacen match y ligan el valor, pero el compilador suprime la advertencia de "variable no usada". La diferencia es que `_` no puede usarse después del match, mientras que `_reason` sí.

```elixir
# Ignorar un elemento específico
{_, second, _} = {1, 2, 3}
# second = 2
# El primer y tercer elemento son ignorados

# Ignorar el reason en un error cuando solo quieres saber que falló
{:error, _} = {:error, "some complex error message we don't need"}
# Match exitoso, el error se ignora

# _ no se puede usar después del match
{_, b} = {1, 2}
_         # ** (CompileError) — _ no está disponible para leer

# _variable sí puede usarse, pero suprime el warning
{_first, second} = {1, 2}
second    # 2
# _first  # Esto funciona pero es inusual — mejor no hacerlo

# Múltiples _ son independientes — cada uno puede coincidir con algo diferente
{_, _, third} = {1, "different", 3}
# third = 3
```

## Exercises

### Exercise 1: Creating Tuples and Accessing Elements
Crea tuplas de distintos tamaños y tipos, y accede a sus elementos con `elem/2`.

```elixir
$ iex

# Tuplas simples
iex> {1, 2, 3}
{1, 2, 3}

iex> {:ok, 42}
{:ok, 42}

iex> {:error, "not found"}
{:error, "not found"}

# Tupla con tipos mixtos
iex> {"Alice", 30, :admin, true}
{"Alice", 30, :admin, true}

# Acceder por índice — base 0
iex> elem({:ok, 42}, 0)
:ok

iex> elem({:ok, 42}, 1)
42

iex> elem({"Alice", 30, :admin}, 2)
:admin

# Tamaño de la tupla
iex> tuple_size({1, 2, 3})
3

iex> tuple_size({:ok})
1

iex> tuple_size({})
0

# Tuplas son valores — se puede usar directamente el resultado
iex> result = {:ok, 100}
{:ok, 100}

iex> elem(result, 1) * 2
200
```

Expected output:
```
iex> elem({:ok, 42}, 0)
:ok
iex> elem({:ok, 42}, 1)
42
iex> tuple_size({1, 2, 3})
3
```

### Exercise 2: Pattern Match as Assignment
El caso más simple de pattern matching: ligar variables usando `=`.

```elixir
# Match básico — ambos lados deben coincidir
iex> x = 42
42

iex> x
42

# Match con tupla — desestructuración
iex> {a, b} = {1, 2}
{1, 2}

iex> a
1

iex> b
2

# El mismo valor en ambos lados siempre funciona
iex> 42 = 42
42

# Pero valores distintos fallan
iex> 42 = 43
# ** (MatchError) no match of right hand side value: 43

# Rebinding: Elixir permite reasignar variables
iex> x = 1
1
iex> x = 2
2
iex> x
2

# Match con tupla de tres elementos
iex> {first, second, third} = {"apple", "banana", "cherry"}
{"apple", "banana", "cherry"}

iex> first
"apple"

iex> second
"banana"

iex> third
"cherry"
```

Expected output:
```
iex> {a, b} = {1, 2}
{1, 2}
iex> a
1
iex> b
2
```

### Exercise 3: Nested Pattern Match
Desestructura tuplas anidadas para extraer valores profundamente anidados en una sola expresión.

```elixir
# Tupla anidada
iex> {{x, y}, z} = {{1, 2}, 3}
{{1, 2}, 3}

iex> x
1

iex> y
2

iex> z
3

# El patrón idiomático: {:ok, payload} donde payload es una tupla
iex> {:ok, {name, age}} = {:ok, {"Alice", 30}}
{:ok, {"Alice", 30}}

iex> name
"Alice"

iex> age
30

# Tres niveles de anidamiento
iex> {:ok, {:user, {id, email}}} = {:ok, {:user, {99, "alice@example.com"}}}
{:ok, {:user, {99, "alice@example.com"}}}

iex> id
99

iex> email
"alice@example.com"

# Con atoms como discriminadores en cada nivel
iex> {:response, :success, {200, "OK", "body content"}} =
...>   {:response, :success, {200, "OK", "body content"}}

iex> # El match fue exitoso — todos los atoms coincidieron
```

Expected output:
```
iex> {:ok, {name, age}} = {:ok, {"Alice", 30}}
{:ok, {"Alice", 30}}
iex> name
"Alice"
iex> age
30
```

### Exercise 4: Matching with Atoms — El Patrón Idiomático de Elixir
Usa pattern matching con atoms para procesar resultados de funciones de forma segura.

```elixir
# Simular funciones que retornan {:ok, value} o {:error, reason}
iex> success_result = {:ok, %{id: 1, name: "Alice"}}
{:ok, %{id: 1, name: "Alice"}}

iex> error_result = {:error, :not_found}
{:error, :not_found}

# Match exitoso — el atom :ok coincide
iex> {:ok, user} = success_result
{:ok, %{id: 1, name: "Alice"}}

iex> user
%{id: 1, name: "Alice"}

iex> user.name
"Alice"

# Match fallido — :ok no coincide con :error
iex> {:ok, user} = error_result
# ** (MatchError) no match of right hand side value: {:error, :not_found}

# La forma correcta de manejar ambos casos es con case
iex> case error_result do
...>   {:ok, data} -> "Got data: #{inspect(data)}"
...>   {:error, reason} -> "Error: #{reason}"
...> end
"Error: not_found"

iex> case success_result do
...>   {:ok, data} -> "Got data: #{data.name}"
...>   {:error, reason} -> "Error: #{reason}"
...> end
"Got data: Alice"
```

Expected output:
```
iex> {:ok, user} = {:ok, %{id: 1, name: "Alice"}}
{:ok, %{id: 1, name: "Alice"}}
iex> user.name
"Alice"
```

### Exercise 5: Pin Operator — Match sin Rebinding
El pin operator `^` te permite usar el valor actual de una variable en un match, previniendo que se rebinde.

```elixir
# Sin pin — la variable se rebinda
iex> x = 1
1

iex> x = 2
2

iex> x
2

# Con pin — la variable se usa como valor fijo
iex> x = 1
1

iex> ^x = 1
1

iex> ^x = 2
# ** (MatchError) no match of right hand side value: 2

# Caso de uso: verificar que el ID en la respuesta coincide con el esperado
iex> expected_id = 42
42

iex> {:ok, ^expected_id} = {:ok, 42}
{:ok, 42}

iex> {:ok, ^expected_id} = {:ok, 99}
# ** (MatchError) no match of right hand side value: {:ok, 99}

# Ejemplo práctico: verificar que una operación devuelve el usuario correcto
iex> user_id = 7
7

iex> response = {:ok, %{id: 7, name: "Alice"}}
{:ok, %{id: 7, name: "Alice"}}

iex> {:ok, %{id: ^user_id, name: name}} = response
{:ok, %{id: 7, name: "Alice"}}

iex> name
"Alice"
```

Expected output:
```
iex> x = 1
1
iex> ^x = 1
1
iex> ^x = 2
** (MatchError) no match of right hand side value: 2
iex> expected_id = 42
42
iex> {:ok, ^expected_id} = {:ok, 42}
{:ok, 42}
```

### Exercise 6: Wildcard _ — Ignorar Valores en el Match
Usa `_` para hacer match con elementos que no necesitas capturar.

```elixir
# Ignorar el primer y tercer elemento
iex> {_, second, _} = {1, 2, 3}
{1, 2, 3}

iex> second
2

# Ignorar el primer elemento de una tupla ok/error/value
iex> {_, _, value} = {:response, :success, 42}
{:response, :success, 42}

iex> value
42

# Solo verificar que el status es :ok sin importar el valor
iex> {:ok, _} = {:ok, "anything here"}
{:ok, "anything here"}

iex> {:ok, _} = {:ok, 12345}
{:ok, 12345}

# _ no está disponible después del match
iex> {_, b} = {1, 2}
{1, 2}

iex> b
2

# _variable — suprime warning pero sí está disponible
iex> {_first, second} = {10, 20}
{10, 20}

iex> second
20

# Uso real: ignorar el error cuando solo quieres continuar
iex> case {:error, "complex error we don't care about"} do
...>   {:ok, data} -> data
...>   {:error, _} -> nil
...> end
nil
```

Expected output:
```
iex> {_, second, _} = {1, 2, 3}
{1, 2, 3}
iex> second
2
iex> {:ok, _} = {:ok, "anything"}
{:ok, "anything"}
```

## Common Mistakes

### Mistake 1: Pensar que = es asignación en todos los contextos
**Wrong:**
```elixir
# Intentar "reasignar" la variable a usando el patrón de Elixir
x = 5
x = x + 1    # Esto funciona en Elixir (rebinding)
# Pero no es lo que parece — no modifica x, crea un nuevo binding

# El error real ocurre cuando se espera que el match funcione como mutación
{:ok, count} = {:ok, 0}
{:ok, count} = {:ok, count + 1}   # Esto funciona, pero count es una nueva variable
# En un proceso con estado, esto no "actualiza" el estado global
```
**Error:** No hay error de compilación, pero el modelo mental es incorrecto. En Elixir, las variables son inmutables — cada `=` crea un nuevo binding en el mismo nombre, no modifica el valor existente.
**Why:** Elixir sigue el modelo de variables de Erlang: una variable existe en un scope y su valor no cambia. Cuando haces `x = x + 1`, creas un nuevo binding que oculta al anterior. Los datos originales no se modifican.
**Fix:**
```elixir
# El patrón correcto es pensar en transformaciones, no mutaciones
initial = {:ok, 0}
{:ok, count} = initial
updated = {:ok, count + 1}   # nuevo valor, no mutación
{:ok, new_count} = updated
# new_count es 1
```

### Mistake 2: MatchError no manejado
**Wrong:**
```elixir
# Código que asume siempre éxito sin manejar el caso de error
def get_user_name(id) do
  {:ok, user} = UserRepo.find(id)   # MatchError si find retorna {:error, ...}
  user.name
end
```
**Error:** `** (MatchError) no match of right hand side value: {:error, :not_found}` — el proceso muere con un error no manejado.
**Why:** El match `{:ok, user} = ...` fallará con MatchError si la función retorna `{:error, ...}`. En Elixir, un proceso que falla con MatchError muere y genera un log de error. Esto puede ser aceptable (deja que el supervisor lo reinicie) o catastrófico dependiendo del contexto.
**Fix:**
```elixir
# Opción 1: usar case para manejar ambos casos explícitamente
def get_user_name(id) do
  case UserRepo.find(id) do
    {:ok, user} -> {:ok, user.name}
    {:error, reason} -> {:error, reason}
  end
end

# Opción 2: usar la versión "bang" que lanza excepción explícita
# Muchas librerías proveen función/1 y función!/1
# La versión ! lanza excepción en lugar de retornar {:error, ...}
user = UserRepo.find!(id)   # Lanza si no encuentra
user.name
```

### Mistake 3: Confundir _x con _
**Wrong:**
```elixir
# Pensar que _reason es lo mismo que _ y no puede usarse
{:error, _reason} = {:error, "timeout after 30s"}
# Intentar usar _reason y asumir que no funciona
IO.puts("Ignoring error")
# _reason   ← comentado porque "no se puede usar"
```
**Error:** No hay error técnico, pero se pierde información útil por confusión conceptual.
**Why:** `_` es un wildcard especial que no crea ningún binding. `_reason` es una variable normal que sí crea un binding y puede usarse — el guión bajo solo suprime el warning del compilador de "variable no usada".
**Fix:**
```elixir
# _ — no crea binding, no puede usarse después
{:error, _} = {:error, "timeout"}
# _ es inaccesible después del match

# _reason — sí crea binding, puede usarse, pero no da warning de "no usada"
{:error, _reason} = {:error, "timeout"}
# _reason está disponible si la necesitas
IO.puts("Debug: error was #{_reason}")   # Esto funciona

# Convención: usa _ cuando genuinamente no necesitas el valor
#             usa _name cuando quieres documentar qué es pero no usarlo
```

## Verification
```bash
$ iex
iex> {a, b, c} = {1, 2, 3}
{1, 2, 3}
iex> a
1
iex> {:ok, value} = {:ok, 42}
{:ok, 42}
iex> value
42
iex> x = 10
10
iex> ^x = 10
10
iex> {_, second, _} = {"ignored", "important", "also ignored"}
{"ignored", "important", "also ignored"}
iex> second
"important"
iex> elem({:ok, 42}, 1)
42
```

## Summary
- **Key concepts**: Tuplas como colecciones de tamaño fijo, `=` como operador de match (no asignación), desestructuración para extraer valores, pin operator `^` para match sin rebinding, wildcard `_` para ignorar valores
- **What you practiced**: Crear tuplas, acceder con `elem/2`, desestructurar con `=`, usar `{:ok, value}` y `{:error, reason}`, proteger matches con `^`, ignorar valores con `_`
- **Important to remember**: `=` en Elixir es el operador de match — el binding de variables es un efecto secundario. Cuando el patrón no coincide, el proceso falla con MatchError. Esto es intencional — Elixir prefiere el fallo explícito al éxito silencioso con datos incorrectos.

## What's Next
En los siguientes ejercicios explorarás **listas** — la estructura de datos de colección dinámica de Elixir — y aprenderás pattern matching con listas usando `[head | tail]`. Luego verás cómo `case`, `cond`, y `with` te permiten encadenar múltiples matches de forma legible.

## Resources
- [The Elixir Getting Started Guide — Pattern Matching](https://elixir-lang.org/getting-started/pattern-matching.html)
- [Elixir Docs - Tuple](https://hexdocs.pm/elixir/Tuple.html)
- [Pattern Matching in Elixir — Comprehensive Guide](https://elixirschool.com/en/lessons/basics/pattern_matching)
