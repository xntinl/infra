# 7. Maps and Keyword Lists

**Difficulty**: Basico

## Prerequisites

- Haber completado los ejercicios 01–06
- Conocimiento básico de átomos (ejercicio 02) y pattern matching (ejercicio 05)
- IEx disponible en tu terminal

## Learning Objectives

- Crear y acceder a maps con atom keys y string keys
- Actualizar maps de forma inmutable con `%{map | key: value}` y `Map.put/3`
- Entender qué son las keyword lists y cuándo usarlas en lugar de maps
- Aplicar pattern matching sobre maps para extraer valores
- Conocer las funciones esenciales del módulo `Map`

## Concepts

### Maps: estructura de datos clave-valor

Un map asocia claves con valores. En Elixir existen dos notaciones según el tipo de clave:

```elixir
# Con atom keys: notación compacta con ":"
user = %{name: "Alice", age: 30, active: true}

# Con string keys: notación con "=>"
config = %{"host" => "localhost", "port" => 5432}

# Mixed (posible, pero evitar — elige un estilo consistente)
mixed = %{:name => "Bob", "role" => "admin"}

# Map vacío
empty = %{}
```

### Acceso a valores en un Map

```elixir
user = %{name: "Alice", age: 30}

# Acceso con punto: SOLO funciona con atom keys
user.name    # "Alice"
user.age     # 30

# Acceso con corchetes: funciona con cualquier key
user[:name]  # "Alice"
user[:name]  # "Alice"

# Map.get/3: permite un valor por defecto si la key no existe
Map.get(user, :name, "unknown")     # "Alice"
Map.get(user, :email, "unknown")    # "unknown"

# Acceso con string keys: SOLO con corchetes o Map.get
config = %{"host" => "localhost"}
config["host"]                       # "localhost"
# config.host                        # KeyError!
```

### Actualizar un Map

Los maps en Elixir son **inmutables**. Actualizar devuelve un nuevo map:

```elixir
user = %{name: "Alice", age: 30}

# Syntax de actualización: SOLO para keys que ya existen
updated = %{user | age: 31}
# %{name: "Alice", age: 31}

# Map.put/3: agrega o actualiza, incluso si la key es nueva
with_email = Map.put(user, :email, "alice@example.com")
# %{name: "Alice", age: 30, email: "alice@example.com"}

# Map.delete/2: elimina una key
without_age = Map.delete(user, :age)
# %{name: "Alice"}

# Map.merge/2: combina dos maps (el segundo gana en conflictos)
Map.merge(%{a: 1, b: 2}, %{b: 99, c: 3})
# %{a: 1, b: 99, c: 3}
```

### Pattern Matching en Maps

El pattern matching en maps extrae solo las keys que especificas — las demás son ignoradas:

```elixir
user = %{name: "Alice", age: 30, role: :admin}

# Extraer name y age (role es ignorado)
%{name: name, age: age} = user
name  # "Alice"
age   # 30

# Match parcial en una función
def greet(%{name: name}), do: "Hello, #{name}!"
greet(user)  # "Hello, Alice!"

# Match con condición de valor (el valor debe ser exactamente :admin)
%{role: :admin} = user  # ok — Alice es admin
```

### Keyword Lists

Una keyword list es una lista de tuplas `{atom, valor}` con sintaxis especial:

```elixir
# Estas dos formas son equivalentes
[name: "Alice", age: 30]
[{:name, "Alice"}, {:age, 30}]

# Acceso por key (devuelve el primero que encuentre)
opts = [timeout: 5000, retries: 3]
opts[:timeout]   # 5000
opts[:retries]   # 3
opts[:missing]   # nil

# Las keyword lists permiten keys duplicadas
[status: :ok, status: :error]  # válido
```

### Maps vs Keyword Lists: cuándo usar cada uno

| Característica | Map | Keyword List |
|---|---|---|
| Orden garantizado | No | Sí (orden de inserción) |
| Keys duplicadas | No | Sí |
| Acceso O(1) | Sí | No — O(n) |
| Uso típico | Datos con estructura | Opciones de funciones |

```elixir
# Maps: para modelar datos con estructura fija
user = %{name: "Alice", age: 30}

# Keyword lists: para opciones de funciones (patrón muy común en Elixir)
File.read!("file.txt", encoding: :utf8)
GenServer.start_link(MyServer, [], name: MyServer)
```

## Exercises

### Exercise 1: Crear Maps

```elixir
# Map con atom keys (la forma más común en Elixir)
user = %{name: "Alice", age: 30, role: :admin}

# Map con string keys (común para datos externos — JSON, config)
config = %{"host" => "localhost", "port" => 5432, "ssl" => false}

# Map anidado
company = %{
  name: "Acme Corp",
  address: %{city: "New York", country: "USA"},
  employees: 500
}

# Map vacío
empty = %{}

# Verificar
is_map(user)     # true
map_size(user)   # 3
map_size(empty)  # 0
```

**Expected output:**

```
iex> user = %{name: "Alice", age: 30, role: :admin}
%{age: 30, name: "Alice", role: :admin}
iex> config = %{"host" => "localhost", "port" => 5432}
%{"host" => "localhost", "port" => 5432}
iex> is_map(user)
true
iex> map_size(user)
3
```

---

### Exercise 2: Acceder a valores

```elixir
user = %{name: "Alice", age: 30, role: :admin}

# Con punto (solo atom keys)
user.name    # "Alice"
user.age     # 30

# Con corchetes (cualquier key)
user[:name]  # "Alice"
user[:role]  # :admin

# Key inexistente: punto lanza error, corchetes devuelve nil
# user.email        # ** (KeyError) key :email not found in: ...
user[:email]        # nil

# Map.get con default
Map.get(user, :name, "unknown")    # "Alice"
Map.get(user, :email, "unknown")   # "unknown"

# Map con string keys — SOLO corchetes
config = %{"host" => "localhost", "port" => 5432}
config["host"]                    # "localhost"
Map.get(config, "port", 3000)     # 5432
Map.get(config, "timeout", 30)    # 30 (default)
```

**Expected output:**

```
iex> user = %{name: "Alice", age: 30, role: :admin}
%{age: 30, name: "Alice", role: :admin}
iex> user.name
"Alice"
iex> user[:role]
:admin
iex> user[:email]
nil
iex> Map.get(user, :email, "unknown")
"unknown"
iex> config = %{"host" => "localhost", "port" => 5432}
%{"host" => "localhost", "port" => 5432}
iex> config["host"]
"localhost"
```

---

### Exercise 3: Actualizar Maps

```elixir
user = %{name: "Alice", age: 30}

# Syntax de actualización — requiere que la key exista
older_user = %{user | age: 31}
older_user  # %{age: 31, name: "Alice"}

# Map.put — agrega o sobreescribe, no requiere key existente
with_email = Map.put(user, :email, "alice@example.com")
# %{age: 30, email: "alice@example.com", name: "Alice"}

# Map.put_new — solo agrega si la key NO existe
Map.put_new(user, :age, 99)    # no modifica age — ya existe
Map.put_new(user, :email, "x") # agrega email — no existía

# Map.delete — eliminar una key
Map.delete(user, :age)
# %{name: "Alice"}

# Encadenar actualizaciones con pipe
user
|> Map.put(:email, "alice@example.com")
|> Map.put(:role, :user)
|> Map.delete(:age)
# %{email: "alice@example.com", name: "Alice", role: :user}
```

**Expected output:**

```
iex> user = %{name: "Alice", age: 30}
%{age: 30, name: "Alice"}
iex> %{user | age: 31}
%{age: 31, name: "Alice"}
iex> Map.put(user, :email, "alice@example.com")
%{age: 30, email: "alice@example.com", name: "Alice"}
iex> Map.delete(user, :age)
%{name: "Alice"}
```

---

### Exercise 4: Pattern Matching en Maps

```elixir
user = %{name: "Alice", age: 30, role: :admin}

# Extraer múltiples valores en una sola operación
%{name: name, role: role} = user
name   # "Alice"
role   # :admin

# El pattern NO necesita incluir todas las keys
%{name: name} = user   # ok — extrae solo name
name   # "Alice"

# Match con valor literal — útil en case y funciones
case user do
  %{role: :admin} -> "Es administrador"
  %{role: :user}  -> "Es usuario normal"
  _               -> "Rol desconocido"
end
# "Es administrador"

# En parámetros de función
defmodule Greeter do
  def greet(%{name: name, role: :admin}), do: "Admin #{name}, bienvenido"
  def greet(%{name: name}), do: "Hola, #{name}"
end

Greeter.greet(%{name: "Alice", role: :admin})  # "Admin Alice, bienvenido"
Greeter.greet(%{name: "Bob", role: :user})     # "Hola, Bob"
```

**Expected output:**

```
iex> user = %{name: "Alice", age: 30, role: :admin}
%{age: 30, name: "Alice", role: :admin}
iex> %{name: name, role: role} = user
%{age: 30, name: "Alice", role: :admin}
iex> name
"Alice"
iex> role
:admin
iex> case user do %{role: :admin} -> "Es administrador"; _ -> "otro" end
"Es administrador"
```

---

### Exercise 5: Keyword Lists

```elixir
# Crear keyword lists
opts = [timeout: 5000, retries: 3, verbose: false]

# Acceder con corchetes — devuelve el primer match
opts[:timeout]   # 5000
opts[:retries]   # 3
opts[:missing]   # nil

# Son equivalentes a listas de tuplas
[{:timeout, 5000}, {:retries, 3}] == [timeout: 5000, retries: 3]
# true

# Keyword.get con default
Keyword.get(opts, :timeout, 30_000)    # 5000
Keyword.get(opts, :missing, :default)  # :default

# Keys duplicadas — solo Maps las rechazan
duped = [status: :ok, status: :error]
duped[:status]                   # :ok — devuelve el primero
Keyword.get_values(duped, :status)  # [:ok, :error]
```

**Expected output:**

```
iex> opts = [timeout: 5000, retries: 3, verbose: false]
[timeout: 5000, retries: 3, verbose: false]
iex> opts[:timeout]
5000
iex> opts[:missing]
nil
iex> [{:timeout, 5000}, {:retries, 3}] == [timeout: 5000, retries: 3]
true
iex> Keyword.get(opts, :missing, :default)
:default
```

---

### Exercise 6: Funciones del módulo Map

```elixir
user = %{name: "Alice", age: 30, role: :admin}

# Obtener todas las keys
Map.keys(user)
# [:age, :name, :role]  (orden no garantizado)

# Obtener todos los valores
Map.values(user)
# [30, "Alice", :admin]

# Verificar si una key existe
Map.has_key?(user, :name)    # true
Map.has_key?(user, :email)   # false

# Combinar dos maps
Map.merge(%{a: 1, b: 2}, %{b: 99, c: 3})
# %{a: 1, b: 99, c: 3}

# Map.merge con función de resolución de conflictos
Map.merge(%{a: 1, b: 2}, %{b: 99, c: 3}, fn _key, old, new -> old + new end)
# %{a: 1, b: 101, c: 3}

# Convertir a lista de tuplas y viceversa
Map.to_list(%{a: 1, b: 2})
# [a: 1, b: 2]  (o [{:a, 1}, {:b, 2}])

Map.new([{:x, 10}, {:y, 20}])
# %{x: 10, y: 20}
```

**Expected output:**

```
iex> user = %{name: "Alice", age: 30, role: :admin}
%{age: 30, name: "Alice", role: :admin}
iex> Map.keys(user)
[:age, :name, :role]
iex> Map.has_key?(user, :name)
true
iex> Map.has_key?(user, :email)
false
iex> Map.merge(%{a: 1, b: 2}, %{b: 99, c: 3})
%{a: 1, b: 99, c: 3}
iex> Map.to_list(%{a: 1, b: 2})
[a: 1, b: 2]
```

## Common Mistakes

### Error 1: Usar `.` con string keys

```elixir
config = %{"host" => "localhost", "port" => 5432}

# WRONG: lanza KeyError
# config.host  # ** (KeyError) key :host not found

# FIX: usar corchetes o Map.get
config["host"]                  # "localhost"
Map.get(config, "host", nil)    # "localhost"
```

### Error 2: Usar `%{map | key: val}` para agregar una key nueva

```elixir
user = %{name: "Alice", age: 30}

# WRONG: lanza KeyError porque :email no existe en user
# %{user | email: "alice@example.com"}
# ** (KeyError) key :email not found in: %{age: 30, name: "Alice"}

# FIX: usar Map.put/3 para agregar keys nuevas
Map.put(user, :email, "alice@example.com")
# %{age: 30, email: "alice@example.com", name: "Alice"}
```

### Error 3: Asumir orden en Maps

```elixir
# WRONG: los maps NO garantizan orden de keys
user = %{c: 3, a: 1, b: 2}
# En IEx se muestra como %{a: 1, b: 2, c: 3} — pero NO asumas ese orden en código

# FIX: si necesitas orden, usa una keyword list o Enum.sort
Map.to_list(user) |> Enum.sort_by(fn {k, _} -> k end)
# [a: 1, b: 2, c: 3]
```

### Error 4: Confundir Map.put_new con Map.put

```elixir
user = %{name: "Alice", age: 30}

# Map.put: siempre sobreescribe
Map.put(user, :age, 99)      # %{age: 99, name: "Alice"}

# Map.put_new: solo inserta si la key NO existe
Map.put_new(user, :age, 99)  # %{age: 30, name: "Alice"} — age no se modifica
Map.put_new(user, :email, "alice@example.com")  # sí inserta, email no existía
```

## Verification

Ejecuta en IEx para verificar tu comprensión:

```bash
iex
```

```elixir
# Prueba 1: acceso atom vs string keys
m1 = %{name: "Alice"}
m2 = %{"name" => "Alice"}
m1.name       # "Alice"
# m2.name     # KeyError!
m2["name"]    # "Alice"

# Prueba 2: update seguro vs KeyError
user = %{name: "Bob", age: 25}
Map.put(user, :city, "Madrid")          # ok — agrega nueva key
# %{user | city: "Madrid"}              # KeyError — city no existe

# Prueba 3: pattern matching extrae solo lo que necesitas
%{name: n} = %{name: "Carol", age: 40, role: :user}
n  # "Carol"

# Prueba 4: merge con resolución de conflictos
Map.merge(%{score: 10}, %{score: 5}, fn _, a, b -> a + b end)
# %{score: 15}

# Prueba 5: keyword list vs map
kw = [a: 1, a: 2]
kw[:a]                    # 1 (primero)
Keyword.get_values(kw, :a)  # [1, 2]
```

## Summary

- Los **maps** son la estructura de datos clave-valor principal en Elixir — O(1) en acceso
- Atom keys: acceso con `.` o `[]`. String keys: solo con `[]` o `Map.get/3`
- `%{map | key: val}` actualiza keys **existentes**; `Map.put/3` agrega o sobreescribe
- **Keyword lists** son listas de tuplas — mantienen orden, permiten duplicados, usadas para opciones de funciones
- Pattern matching en maps extrae solo las keys que declares — las demás se ignoran

## What's Next

- **08-functions-and-arity**: Definir funciones y usar pattern matching en sus parámetros
- **09-control-flow**: Usar maps en `case` y `cond` para control de flujo

## Resources

- [Elixir Docs — Map](https://hexdocs.pm/elixir/Map.html)
- [Elixir Docs — Keyword](https://hexdocs.pm/elixir/Keyword.html)
- [Elixir School — Maps](https://elixirschool.com/en/lessons/basics/collections#maps-6)
- [Elixir Getting Started — Key-Value Stores](https://elixir-lang.org/getting-started/keywords-and-maps.html)
