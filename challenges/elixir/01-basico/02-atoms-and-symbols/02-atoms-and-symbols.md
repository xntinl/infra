# 2. Atoms and Symbols

**Difficulty**: Basico

## Prerequisites
- Haber completado el ejercicio 01-setup-and-mix
- Tener IEx disponible (`iex` o `iex -S mix`)

## Learning Objectives
After completing this exercise, you will be able to:
- Crear y reconocer atoms como identificadores únicos e inmutables
- Usar el patrón `{:ok, value}` / `{:error, reason}` que aparece en todo el ecosistema Elixir
- Distinguir cuándo usar atoms versus strings
- Entender que `true`, `false`, y `nil` son atoms especiales
- Convertir entre atoms y strings con las funciones del módulo `Atom`

## Concepts

### ¿Qué es un Atom?
Un atom es una constante cuyo nombre es su propio valor. Se escriben con dos puntos `:` como prefijo: `:ok`, `:error`, `:hello`. A diferencia de los strings, los atoms no contienen datos arbitrarios — son identificadores fijos que se definen en tiempo de compilación.

El nombre "atom" viene de la idea de que son valores indivisibles y no pueden cambiar. Dos atoms con el mismo nombre son idénticos en todo el sistema — no hay diferencia entre el `:ok` que defines en un módulo y el `:ok` de otro. Esta propiedad los hace extremadamente eficientes para comparaciones.

```elixir
# Atoms básicos
:ok
:error
:hello
:not_found
:timeout

# Atoms con espacios o caracteres especiales requieren comillas dobles
:"hello world"
:"some-atom-with-dashes"
:"123_starts_with_number"

# La comparación de atoms es O(1) — se compara por referencia, no por contenido
:ok == :ok    # true
:ok == :error # false
```

### El Patrón {:ok, value} / {:error, reason}
Este es uno de los patrones más importantes en Elixir. En lugar de lanzar excepciones, las funciones en Elixir retornan tuplas `{:ok, resultado}` cuando tienen éxito, y `{:error, razón}` cuando fallan. Este patrón es omnipresente en el ecosistema y hace el manejo de errores explícito y legible.

La razón de usar atoms en este patrón es eficiencia y claridad. Al hacer pattern matching, Elixir verifica el atom primero (comparación O(1)) antes de acceder al resto de la tupla. Además, `:ok` y `:error` son inequívocos — su significado es universalmente entendido en Elixir.

```elixir
# Ejemplos del patrón ok/error en funciones reales
File.read("existing_file.txt")
# {:ok, "contenido del archivo"}

File.read("nonexistent.txt")
# {:error, :enoent}

Integer.parse("42")
# {42, ""}

Integer.parse("not_a_number")
# :error

# Funciones propias siguiendo el patrón
def find_user(id) when id > 0 do
  # Simula buscar en base de datos
  {:ok, %{id: id, name: "Alice"}}
end

def find_user(_id) do
  {:error, :not_found}
end
```

### Atoms en Memoria: La Atom Table
Los atoms se almacenan en una tabla global en la VM de Erlang (la BEAM). Cada atom único ocupa espacio en esta tabla y **nunca se elimina durante la vida del proceso**. Esto significa que crear un atom nuevo tiene un costo y que crear atoms dinámicamente (desde input del usuario, por ejemplo) es un riesgo de seguridad — un atacante podría agotar la memoria enviando strings únicos que se conviertan en atoms.

```elixir
# Seguro: atoms hardcodeados en código fuente
status = :active

# PELIGROSO en producción: convertir input externo a atom
user_input = params["status"]
atom_status = String.to_atom(user_input)  # Crea atom nuevo si no existe — ¡riesgo!

# Seguro: usar String.to_existing_atom/1 que falla si el atom no existe
atom_status = String.to_existing_atom(user_input)  # Solo funciona con atoms ya definidos
```

### Atoms Especiales: true, false, nil
En Elixir, los valores booleanos y `nil` son atoms especiales. No existe un tipo `Boolean` separado — `true` es simplemente el atom `:true`, `false` es `:false`, y `nil` es `:nil`. Esta unificación simplifica el sistema de tipos.

```elixir
# true, false y nil son atoms
is_atom(true)   # true
is_atom(false)  # true
is_atom(nil)    # true

# Son equivalentes a sus versiones con dos puntos
true == :true   # true
false == :false # true
nil == :nil     # true

# Operaciones booleanas funcionan con atoms
true and false  # false
true or false   # true
not true        # false
```

## Exercises

### Exercise 1: Creating Atoms in IEx
Abre IEx y experimenta con la creación y verificación de atoms. Este ejercicio te dará familiaridad con la sintaxis básica.

```elixir
# Abre IEx con: iex

# Atoms básicos
iex> :ok
:ok

iex> :error
:error

iex> :hello
:hello

# Atom con espacios — requiere comillas dobles
iex> :"hello world"
:"hello world"

# Verificar que algo es un atom
iex> is_atom(:ok)
true

iex> is_atom("ok")
false

# Inspeccionar el tipo
iex> i(:ok)
# Term
#   :ok
# Data type
#   Atom
# Reference modules
#   Atom
```

Expected output:
```
iex> :ok
:ok
iex> is_atom(:ok)
true
iex> is_atom("ok")
false
```

### Exercise 2: Atoms vs Strings — Comparación de Eficiencia
Compara el comportamiento de atoms y strings para entender por qué los atoms son preferidos como identificadores.

```elixir
# Comparación de atoms: O(1) — compara referencia en atom table
iex> :hello == :hello
true

iex> :hello == :world
false

# Comparación de strings: O(n) — compara byte a byte
iex> "hello" == "hello"
true

# Los atoms con el mismo nombre siempre son idénticos
iex> :my_status === :my_status
true

# Un string con el mismo contenido es IGUAL pero no el mismo objeto
iex> "my_status" == "my_status"
true

# Convertir entre atom y string
iex> Atom.to_string(:hello)
"hello"

iex> String.to_atom("hello")
:hello

# Los atoms se ordenan por su nombre en comparaciones
iex> :apple < :banana
true

iex> :zebra > :apple
true
```

Expected output:
```
iex> :hello == :hello
true
iex> Atom.to_string(:hello)
"hello"
iex> String.to_atom("hello")
:hello
```

### Exercise 3: Tuples with Atoms — El Patrón ok/error
Crea tuplas con el patrón `{:ok, value}` y `{:error, reason}` para simular respuestas de funciones.

```elixir
# Tupla de éxito
iex> {:ok, 42}
{:ok, 42}

iex> {:ok, "user found"}
{:ok, "user found"}

# Tupla de error
iex> {:error, "not found"}
{:error, "not found"}

# El reason puede ser otro atom (común en Elixir)
iex> {:error, :not_found}
{:error, :not_found}

iex> {:error, :timeout}
{:error, :timeout}

iex> {:error, :unauthorized}
{:error, :unauthorized}

# Tuplas anidadas (resultado complejo)
iex> {:ok, %{id: 1, name: "Alice", role: :admin}}
{:ok, %{id: 1, name: "Alice", role: :admin}}

# Acceder elementos de una tupla con elem/2
iex> result = {:ok, 42}
{:ok, 42}

iex> elem(result, 0)
:ok

iex> elem(result, 1)
42
```

Expected output:
```
iex> {:ok, 42}
{:ok, 42}
iex> {:error, :not_found}
{:error, :not_found}
iex> elem({:ok, 42}, 1)
42
```

### Exercise 4: Pattern Matching Introduction with Atoms
El verdadero poder de las tuplas con atoms se ve con pattern matching. Usamos `=` para "desestructurar" el contenido de una tupla.

```elixir
# Match exitoso: el patrón coincide con la tupla
iex> {:ok, value} = {:ok, 42}
{:ok, 42}

# Ahora 'value' está vinculado al número 42
iex> value
42

# Match con error
iex> {:error, reason} = {:error, :not_found}
{:error, :not_found}

iex> reason
:not_found

# Match que falla: el atom :ok no coincide con :error
iex> {:ok, value} = {:error, "something went wrong"}
# ** (MatchError) no match of right hand side value: {:error, "something went wrong"}

# Caso de uso real: procesar resultado de File.read/1
iex> result = {:ok, "file content here"}
{:ok, "file content here"}

iex> {:ok, content} = result
{:ok, "file content here"}

iex> content
"file content here"
```

Expected output:
```
iex> {:ok, value} = {:ok, 42}
{:ok, 42}
iex> value
42
iex> {:error, reason} = {:error, :not_found}
{:error, :not_found}
iex> reason
:not_found
```

### Exercise 5: Boolean Atoms — true, false, nil
Confirma que los valores booleanos y nil son atoms en Elixir y experimenta con operaciones booleanas.

```elixir
# Verificar que true, false, nil son atoms
iex> is_atom(true)
true

iex> is_atom(false)
true

iex> is_atom(nil)
true

# Son equivalentes a sus formas con dos puntos
iex> true == :true
true

iex> nil == :nil
true

# Operaciones booleanas
iex> true and false
false

iex> true or false
true

iex> not true
false

# nil es "falsy" en Elixir (junto con false)
iex> nil || "default value"
"default value"

iex> false || "fallback"
"fallback"

iex> :ok || "not reached"
:ok

# is_nil/1 verifica específicamente nil
iex> is_nil(nil)
true

iex> is_nil(false)
false

iex> is_nil(:ok)
false
```

Expected output:
```
iex> is_atom(true)
true
iex> true == :true
true
iex> nil || "default value"
"default value"
```

### Exercise 6: Atom Functions — Módulo Atom
Explora las funciones disponibles en el módulo `Atom` para convertir y manipular atoms.

```elixir
# Atom a string
iex> Atom.to_string(:hello)
"hello"

iex> Atom.to_string(:ok)
"ok"

iex> Atom.to_string(true)
"true"

# String a atom — CREA el atom si no existe
iex> String.to_atom("hello")
:hello

iex> String.to_atom("my_custom_atom")
:my_custom_atom

# String.to_existing_atom/1 — FALLA si el atom no existe previamente
iex> String.to_existing_atom("ok")    # :ok ya existe
:ok

iex> String.to_existing_atom("random_nonexistent_atom_xyz")
# ** (ArgumentError) errors were found at the given arguments:
#   * 1st argument: not an already existing atom

# Inspeccionar el atom
iex> inspect(:hello)
":hello"

# Usar atoms como claves en maps (sintaxis especial)
iex> user = %{name: "Alice", role: :admin, active: true}
%{name: "Alice", role: :admin, active: true}

iex> user.role
:admin

iex> user[:name]
"Alice"
```

Expected output:
```
iex> Atom.to_string(:hello)
"hello"
iex> String.to_atom("hello")
:hello
iex> String.to_existing_atom("ok")
:ok
```

## Common Mistakes

### Mistake 1: String.to_atom/1 desde input del usuario
**Wrong:**
```elixir
# En un controlador web o endpoint de API
def handle_request(params) do
  status = String.to_atom(params["status"])  # PELIGROSO
  update_user(status)
end
```
**Error:** No hay error inmediato, pero cada valor único de `params["status"]` crea un atom nuevo en la atom table que **nunca se libera**. Un atacante enviando strings únicos puede agotar la memoria del sistema.
**Why:** La atom table tiene un límite por defecto (1,048,576 atoms en BEAM). Al agotarla, la VM se detiene.
**Fix:**
```elixir
# Opción 1: usar String.to_existing_atom/1 — falla si el atom no existe previamente
def handle_request(params) do
  status = String.to_existing_atom(params["status"])
  update_user(status)
rescue
  ArgumentError -> {:error, :invalid_status}
end

# Opción 2: validar contra una lista de atoms permitidos
@valid_statuses [:active, :inactive, :pending]

def parse_status(str) when str in ["active", "inactive", "pending"] do
  String.to_existing_atom(str)
end
def parse_status(_), do: {:error, :invalid_status}
```

### Mistake 2: Confundir el nombre del módulo con un atom
**Wrong:**
```elixir
# Pensar que :string es el módulo String
iex> :string.upcase("hello")
# Esto llama a funciones Erlang, no a módulos Elixir
# En algunos casos funciona, en otros no, y confunde
```
**Error:** En Elixir, los módulos son atoms — `String` es el atom `:"Elixir.String"`. Pero la convención es usar el nombre del módulo directamente, no su atom.
**Why:** Los módulos Elixir tienen el prefijo `"Elixir."` en su atom interno. `:string` sin prefijo es el módulo de strings de Erlang — diferente API.
**Fix:**
```elixir
# Usar el nombre del módulo Elixir directamente
iex> String.upcase("hello")
"HELLO"

# Si necesitas el atom del módulo
iex> is_atom(String)
true

iex> String == :"Elixir.String"
true
```

### Mistake 3: Usar strings donde atoms son más apropiados
**Wrong:**
```elixir
# Usando strings para estados fijos y conocidos en tiempo de compilación
def process(event) do
  case event["type"] do
    "user_created" -> handle_user_created(event)
    "order_placed" -> handle_order_placed(event)
    "payment_failed" -> handle_payment_failed(event)
  end
end
```
**Error:** No hay error de compilación, pero es menos eficiente y más propenso a errores tipográficos silenciosos.
**Why:** Las comparaciones de strings son O(n). Con atoms son O(1). Además, un typo en un string (`"user_creatd"`) no produce error — simplemente no hace match. Un typo en un atom puede producir un warning del compilador.
**Fix:**
```elixir
# Usar atoms para identificadores fijos
def process(event) do
  case event.type do
    :user_created -> handle_user_created(event)
    :order_placed -> handle_order_placed(event)
    :payment_failed -> handle_payment_failed(event)
  end
end
```

## Verification
```bash
$ iex
iex> :ok
:ok
iex> is_atom(:ok)
true
iex> is_atom(true)
true
iex> {:ok, value} = {:ok, 42}
{:ok, 42}
iex> value
42
iex> Atom.to_string(:hello)
"hello"
iex> String.to_atom("world")
:world
```

## Summary
- **Key concepts**: Atoms como identificadores únicos e inmutables, patrón `{:ok, value}` / `{:error, reason}`, `true`/`false`/`nil` como atoms especiales, atom table global
- **What you practiced**: Crear atoms, comparar atoms con strings, desestructurar tuplas con pattern matching, usar `Atom.to_string/1` y `String.to_atom/1`
- **Important to remember**: Nunca uses `String.to_atom/1` con input externo — usa `String.to_existing_atom/1` o valida primero. Los atoms nunca se liberan de la memoria.

## What's Next
En el siguiente ejercicio **03-numbers-and-arithmetic** aprenderás sobre los tipos numéricos de Elixir: integers, floats, y las operaciones aritméticas incluyendo las diferencias importantes entre `/`, `div/2`, y `rem/2`.

## Resources
- [The Elixir Getting Started Guide — Atoms](https://elixir-lang.org/getting-started/basic-types.html#atoms)
- [Elixir Docs - Atom](https://hexdocs.pm/elixir/Atom.html)
- [Erlang Atom Table — Why it matters](https://www.erlang.org/doc/efficiency_guide/advanced.html)
