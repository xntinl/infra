# 15. Structs and Basic Validation

**Difficulty**: Basico

---

## Prerequisites

- Módulos y visibilidad (ejercicio 14)
- Maps y keyword lists (ejercicio 07)
- Pattern matching básico (ejercicio 05)
- Guards básicos (`when is_binary/1`, `when is_integer/1`)

---

## Learning Objectives

- Definir structs con `defstruct` dentro de un módulo
- Crear instancias de structs con valores por defecto y personalizados
- Acceder a campos con dot notation: `user.name`
- Actualizar structs de forma inmutable con la syntax `%Struct{original | field: value}`
- Hacer pattern matching sobre structs
- Distinguir structs de maps simples y entender cuándo usar cada uno
- Implementar funciones constructor con validación básica

---

## Concepts

### ¿Qué es un struct?

Un struct es un map con:
1. Un módulo asociado que le da su tipo
2. Campos definidos en compile time (no pueden añadirse campos arbitrarios)
3. Valores por defecto para cada campo

```elixir
defmodule User do
  defstruct name: "", age: 0, active: true
end
```

### Crear instancias

```elixir
# Con valores por defecto
empty_user = %User{}
IO.inspect(empty_user)
# => %User{active: true, age: 0, name: ""}

# Con valores personalizados
alice = %User{name: "Alice", age: 30}
IO.inspect(alice)
# => %User{active: true, age: 30, name: "Alice"}

# Solo algunos campos (el resto usa defaults)
partial = %User{name: "Bob"}
IO.inspect(partial)
# => %User{active: true, age: 0, name: "Bob"}
```

### Acceder a campos

```elixir
alice = %User{name: "Alice", age: 30, active: true}

alice.name    # => "Alice"   — dot notation
alice.age     # => 30
alice.active  # => true

# También funciona la syntax de map con atom key
alice[:name]  # => "Alice"
```

### Actualizar campos — inmutabilidad

Como todo en Elixir, los structs son inmutables. Para "actualizar" un campo
debes crear un nuevo struct:

```elixir
alice = %User{name: "Alice", age: 30}

# Crea un NUEVO struct con age modificado; alice no cambia
senior_alice = %User{alice | age: 31}

IO.inspect(alice.age)         # 30 — sin cambios
IO.inspect(senior_alice.age)  # 31 — nuevo struct
```

La syntax `%Struct{original | field: new_value}` solo puede cambiar campos que
ya existen en `defstruct`. Intentar añadir campos nuevos lanza `KeyError`.

### Pattern matching con structs

```elixir
defmodule User do
  defstruct name: "", age: 0, active: true

  def greet(%User{name: name}) do
    "Hello, #{name}!"
  end

  def status(%User{active: true}),  do: "Usuario activo"
  def status(%User{active: false}), do: "Usuario inactivo"
end

alice = %User{name: "Alice", age: 30}
User.greet(alice)        # => "Hello, Alice!"
User.status(alice)       # => "Usuario activo"
```

El pattern `%User{name: name}` hace dos cosas a la vez:
1. Verifica que el valor sea un struct de tipo `User`
2. Extrae el campo `name` a la variable `name`

### Struct vs Map

```elixir
# Map: cualquier clave, cualquier tipo, sin módulo asociado
map = %{name: "Alice", age: 30}

# Struct: campos fijos, tipo asociado al módulo
struct = %User{name: "Alice", age: 30}

# is_struct verifica el tipo
is_struct(struct)        # => true
is_struct(struct, User)  # => true
is_struct(map, User)     # => false

# Un struct también es un map
is_map(struct)           # => true

# Diferencia clave: los structs NO tienen campos arbitrarios
# %User{nickname: "ally"}  # => KeyError en compile time
```

| Aspecto | Map | Struct |
|---------|-----|--------|
| Campos | Cualquiera, dinámicos | Definidos en compile time |
| Tipo | Solo `%{}` | Módulo específico |
| Defaults | No | Sí, en `defstruct` |
| Pattern match por tipo | No | Sí, con `%User{}` |
| Campos extra | Sí | No (KeyError) |

### Función constructor con validación

La convención en Elixir es definir `new/1` o `new/n` como función pública
que valida los datos antes de crear el struct:

```elixir
defmodule User do
  defstruct name: "", age: 0, active: true

  def new(name, age) when is_binary(name) and is_integer(age) and age >= 0 do
    {:ok, %User{name: name, age: age}}
  end

  def new(_name, _age) do
    {:error, "name must be a string and age a non-negative integer"}
  end
end

User.new("Alice", 30)   # => {:ok, %User{active: true, age: 30, name: "Alice"}}
User.new("Bob", -5)     # => {:error, "name must be a string and age a non-negative integer"}
User.new(123, 30)       # => {:error, "name must be a string and age a non-negative integer"}
```

---

## Exercises

### Ejercicio 1: Definir un struct básico

```elixir
defmodule User do
  @moduledoc "Representa un usuario del sistema."

  defstruct name: "", age: 0, active: true
end

# Con todos los defaults
empty = %User{}
IO.inspect(empty)

# Con valores personalizados
alice = %User{name: "Alice", age: 30}
IO.inspect(alice)

# Solo cambiando active
inactive = %User{name: "Bob", age: 25, active: false}
IO.inspect(inactive)
```

**Expected output:**
```
%User{active: true, age: 0, name: ""}
%User{active: true, age: 30, name: "Alice"}
%User{active: false, age: 25, name: "Bob"}
```

---

### Ejercicio 2: Crear instancias y explorar errores de campos inexistentes

```elixir
defmodule Product do
  defstruct name: "", price: 0.0, stock: 0
end

laptop = %Product{name: "Laptop Pro", price: 999.99, stock: 10}
IO.inspect(laptop)

# Campo con default
no_stock = %Product{name: "Widget", price: 4.99}
IO.inspect(no_stock)

# Intentar campo inexistente lanza error en compile time.
# Descomenta la siguiente línea para ver el error:
# invalid = %Product{name: "X", color: "red"}
# => ** (KeyError) key :color not found
```

**Expected output:**
```
%Product{name: "Laptop Pro", price: 999.99, stock: 10}
%Product{name: "Widget", price: 4.99, stock: 0}
```

---

### Ejercicio 3: Acceso a campos con dot notation

```elixir
defmodule Point do
  defstruct x: 0, y: 0
end

p = %Point{x: 3, y: 7}

IO.inspect(p.x)
IO.inspect(p.y)

# También funciona la sintaxis de map
IO.inspect(p[:x])

# Calcular distancia al origen
distance = :math.sqrt(p.x * p.x + p.y * p.y)
IO.inspect(Float.round(distance, 4))
```

**Expected output:**
```
3
7
3
7.6158
```

---

### Ejercicio 4: Actualizar structs — inmutabilidad

```elixir
defmodule User do
  defstruct name: "", age: 0, active: true
end

alice = %User{name: "Alice", age: 30}
IO.puts("Original: age = #{alice.age}")

# Crear nuevo struct con age modificado
older_alice = %User{alice | age: alice.age + 1}
IO.puts("Updated:  age = #{older_alice.age}")
IO.puts("Original unchanged: age = #{alice.age}")

# Puedes cambiar múltiples campos a la vez
retired = %User{alice | age: 65, active: false}
IO.inspect(retired)
IO.inspect(alice)
```

**Expected output:**
```
Original: age = 30
Updated:  age = 31
Original unchanged: age = 30
%User{active: false, age: 65, name: "Alice"}
%User{active: true, age: 30, name: "Alice"}
```

---

### Ejercicio 5: Pattern matching con structs

```elixir
defmodule User do
  defstruct name: "", age: 0, active: true
end

defmodule UserFormatter do
  def greet(%User{name: name, active: true}) do
    "Hello, #{name}! (cuenta activa)"
  end

  def greet(%User{name: name, active: false}) do
    "Hello, #{name}. (cuenta inactiva)"
  end

  def label(%User{age: age}) when age >= 18 do
    :adult
  end

  def label(%User{}) do
    :minor
  end
end

alice = %User{name: "Alice", age: 30, active: true}
bob   = %User{name: "Bob", age: 16, active: false}

IO.puts(UserFormatter.greet(alice))
IO.puts(UserFormatter.greet(bob))
IO.inspect(UserFormatter.label(alice))
IO.inspect(UserFormatter.label(bob))
```

**Expected output:**
```
Hello, Alice! (cuenta activa)
Hello, Bob. (cuenta inactiva)
:adult
:minor
```

---

### Ejercicio 6: Constructor con validación básica

```elixir
defmodule User do
  defstruct name: "", age: 0, active: true

  @doc """
  Crea un nuevo User con validación básica.

  Retorna `{:ok, %User{}}` si los datos son válidos,
  o `{:error, reason}` si no lo son.
  """
  def new(name, age)
      when is_binary(name) and name != "" and
           is_integer(age) and age >= 0 and age <= 150 do
    {:ok, %User{name: name, age: age}}
  end

  def new("", _age), do: {:error, "name cannot be empty"}
  def new(_name, age) when not is_integer(age), do: {:error, "age must be an integer"}
  def new(_name, age) when age < 0, do: {:error, "age cannot be negative"}
  def new(_name, _age), do: {:error, "invalid arguments"}
end

# Casos válidos
IO.inspect(User.new("Alice", 30))
IO.inspect(User.new("Bob", 0))

# Casos inválidos
IO.inspect(User.new("", 25))
IO.inspect(User.new("Carol", -5))
IO.inspect(User.new("Dave", "treinta"))
```

**Expected output:**
```
{:ok, %User{active: true, age: 30, name: "Alice"}}
{:ok, %User{active: true, age: 0, name: "Bob"}}
{:error, "name cannot be empty"}
{:error, "age cannot be negative"}
{:error, "age must be an integer"}
```

---

## Common Mistakes

### Error 1: Crear struct sin `defstruct` en el módulo

```elixir
# WRONG — el módulo no tiene defstruct
defmodule Foo do
  def hello, do: "hello"
end

%Foo{name: "Alice"}
```

```
** (UndefinedFunctionError) function Foo.__struct__/1 is undefined
```

**Why**: Los structs requieren `defstruct` dentro del módulo. Sin él, el módulo
no tiene la función `__struct__/1` que habilita la syntax `%Foo{}`.

**Fix**:
```elixir
defmodule Foo do
  defstruct name: ""  # definir campos antes de usarlos
end

%Foo{name: "Alice"}  # ahora funciona
```

---

### Error 2: Actualizar un campo que no existe en el struct

```elixir
# WRONG — intentar añadir un campo que no está en defstruct
defmodule User do
  defstruct name: "", age: 0
end

user = %User{name: "Alice"}
%User{user | email: "alice@example.com"}
```

```
** (KeyError) key :email not found in: %User{age: 0, name: "Alice"}
```

**Why**: La syntax `%User{original | field: value}` solo puede cambiar campos
que ya existen en el struct. No puede añadir campos nuevos — eso rompería
la garantía de que el struct tiene exactamente los campos definidos.

**Fix**: Añade el campo a `defstruct`:
```elixir
defmodule User do
  defstruct name: "", age: 0, email: ""   # añadir email
end

user  = %User{name: "Alice"}
user2 = %User{user | email: "alice@example.com"}  # ahora funciona
```

---

### Error 3: Pensar que los structs soportan herencia

```elixir
# WRONG — no hay herencia de structs en Elixir
defmodule Animal do
  defstruct name: ""
end

defmodule Dog do
  # No puedes "extender" Animal aquí
  defstruct Animal, breed: ""  # esto no es sintaxis válida
end
```

**Why**: Elixir no tiene herencia orientada a objetos. Los structs son
simplemente maps con metadata de módulo. No hay jerarquía de tipos.

**Fix**: Usa composición — incluye el struct como campo, o comparte lógica
mediante funciones de módulo:
```elixir
defmodule Animal do
  defstruct name: "", species: ""
end

defmodule Dog do
  defstruct animal: %Animal{}, breed: ""

  def new(name, breed) do
    %Dog{
      animal: %Animal{name: name, species: "Canis lupus familiaris"},
      breed: breed
    }
  end
end
```

---

### Error 4: Usar Map.get con structs cuando dot notation falla de forma confusa

```elixir
defmodule User do
  defstruct name: "", age: 0
end

user = %User{name: "Alice"}

# Intentar acceder a un campo inexistente con dot notation
user.email
```

```
** (KeyError) key :email not found in: %User{age: 0, name: "Alice"}
```

**Why**: El struct no tiene el campo `:email`. A diferencia de `Map.get/3`,
el dot notation lanza `KeyError` inmediatamente.

**Fix**: Usa `Map.get/3` con default si el campo es opcional, o añade el campo
al struct:
```elixir
# Si el campo puede no existir — aunque en structs bien definidos esto raro
email = Map.get(user, :email, "no email")  # => "no email"

# Lo correcto: definir el campo en defstruct con default nil
defmodule User do
  defstruct name: "", age: 0, email: nil
end
user = %User{name: "Alice"}
user.email  # => nil  — siempre tiene el campo, puede ser nil
```

---

## Verification

```bash
iex
```

```elixir
# Definir struct
defmodule Point do
  defstruct x: 0, y: 0
end

# Crear instancias
p1 = %Point{}
p2 = %Point{x: 3, y: 4}
IO.inspect(p1)  # %Point{x: 0, y: 0}
IO.inspect(p2)  # %Point{x: 3, y: 4}

# Acceso
p2.x   # => 3
p2.y   # => 4

# Actualización inmutable
p3 = %Point{p2 | x: 10}
p2.x   # => 3  (sin cambios)
p3.x   # => 10 (nuevo struct)

# Pattern matching
%Point{x: x, y: y} = p2
IO.inspect({x, y})  # => {3, 4}

# is_struct
is_struct(p2)         # => true
is_struct(p2, Point)  # => true
is_map(p2)            # => true (también es un map)
is_struct(%{x: 3})    # => false (map simple)
```

---

## Summary

- `defstruct field: default` dentro de un módulo define los campos del struct.
- `%Module{}` crea un struct con defaults; `%Module{field: value}` con valores.
- El acceso es con dot notation: `struct.field`. También funciona `struct[:field]`.
- La actualización crea un nuevo struct: `%Module{original | field: new_val}`.
- No se pueden añadir campos fuera de los definidos en `defstruct` — KeyError.
- Los structs también son maps (`is_map/1` retorna true), pero con tipo.
- `is_struct(x, Module)` verifica que `x` sea exactamente ese tipo de struct.
- La convención `def new/n` encapsula creación + validación.
- No existe herencia de structs — usa composición.

---

## What's Next

- Protocolos — implementar behavior polimórfico para tus structs (ej. `String.Chars`)
- Ecto.Schema — structs para bases de datos con validaciones completas
- `Access` behaviour — acceso dinámico a campos de structs

---

## Resources

- [Structs — Elixir Getting Started](https://elixir-lang.org/getting-started/structs.html)
- [defstruct — HexDocs](https://hexdocs.pm/elixir/Kernel.html#defstruct/1)
- [Elixir School — Structs](https://elixirschool.com/en/lessons/basics/structs)
- [Composing Elixir structs](https://hexdocs.pm/elixir/Map.html)
