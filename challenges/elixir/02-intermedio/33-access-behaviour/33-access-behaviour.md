# 33: Access Behaviour

## Prerequisites

- Pattern matching avanzado (ejercicio 09)
- Protocols y behaviours (ejercicios 07-08)
- Structs y mapas en Elixir
- `Enum`, `Kernel` básicos

---

## Learning Objectives

Al finalizar este ejercicio serás capaz de:

1. Usar `get_in/2`, `put_in/3`, `update_in/3` y `pop_in/2` para navegar y modificar estructuras anidadas sin boilerplate
2. Entender qué callbacks implementa el behaviour `Access` (`fetch/2`, `get_and_update/3`, `pop/2`)
3. Implementar `Access` en un struct propio para que funcione con los operadores de path
4. Componer accessors con `Access.key/1`, `Access.at/1` y `Access.filter/1`
5. Reconocer el patrón *lens* básico que Access habilita en Elixir

---

## Concepts

### ¿Qué es el behaviour Access?

`Access` es un behaviour de Elixir que define cómo se accede a la estructura interna de un dato. Cualquier módulo que lo implemente puede usarse como "paso" en los path operators (`get_in`, `put_in`, etc.).

Los tres callbacks obligatorios son:

```elixir
@callback fetch(term(), term()) :: {:ok, term()} | :error
@callback get_and_update(term(), term(), (term() -> {term(), term()} | :pop)) ::
            {term(), term()}
@callback pop(term(), term()) :: {term(), term()}
```

### get_in / put_in / update_in / pop_in

Estos macros/funciones permiten operar sobre estructuras anidadas usando una lista de claves como "path":

```elixir
data = %{user: %{profile: %{name: "Ana", age: 30}}}

# Leer anidado
get_in(data, [:user, :profile, :name])
# => "Ana"

# Escribir anidado (devuelve estructura nueva, es inmutable)
put_in(data, [:user, :profile, :name], "Beatriz")
# => %{user: %{profile: %{name: "Beatriz", age: 30}}}

# Actualizar con función
update_in(data, [:user, :profile, :age], &(&1 + 1))
# => %{user: %{profile: %{name: "Ana", age: 31}}}

# Extraer y eliminar
{val, rest} = pop_in(data, [:user, :profile, :age])
# val => 30, rest => %{user: %{profile: %{name: "Ana"}}}
```

También existe la forma de macro con la sintaxis de corchetes:

```elixir
put_in data[:user][:profile][:name], "Carlos"
```

### Access.fetch/2

`Access.fetch/2` es la función de bajo nivel que implementan los mapas y listas de palabras clave. Devuelve `{:ok, valor}` o `:error`:

```elixir
Access.fetch(%{a: 1}, :a)   # => {:ok, 1}
Access.fetch(%{a: 1}, :b)   # => :error
Access.fetch([a: 1], :a)    # => {:ok, 1}
```

### Accessors estándar

`Access` incluye funciones que generan accessors (funciones de orden superior) listos para usar en paths:

```elixir
# Access.key/1 — para structs y mapas, con clave atom
data = %{users: [%{name: "Ana"}, %{name: "Bob"}]}
get_in(data, [Access.key(:users), Access.at(0), Access.key(:name)])
# => "Ana"

# Access.at/1 — para listas, por índice
get_in([10, 20, 30], [Access.at(1)])
# => 20

# Access.filter/1 — devuelve sublista de elementos que cumplen predicado
update_in(data, [Access.key(:users), Access.filter(&(&1.name != "Ana"))], fn user ->
  Map.put(user, :active, false)
end)
```

### Implementación del behaviour en un struct

```elixir
defmodule Config do
  defstruct [:env, :values]

  @behaviour Access

  @impl Access
  def fetch(%Config{values: values}, key) do
    Access.fetch(values, key)
  end

  @impl Access
  def get_and_update(%Config{values: values} = config, key, fun) do
    {get, new_values} = Access.get_and_update(values, key, fun)
    {get, %{config | values: new_values}}
  end

  @impl Access
  def pop(%Config{values: values} = config, key) do
    {val, new_values} = Access.pop(values, key)
    {val, %{config | values: new_values}}
  end
end
```

Con esta implementación, `Config` puede usarse como paso en paths:

```elixir
cfg = %Config{env: :prod, values: %{timeout: 5000, retries: 3}}
get_in(cfg, [:timeout])          # => 5000
put_in(cfg, [:timeout], 10_000)  # => %Config{values: %{timeout: 10000, retries: 3}}
```

### El patrón Lens básico

Un *lens* es una abstracción que encapsula cómo leer y escribir en una parte de una estructura. En Elixir, los accessors de `Access` son lenses simples:

```elixir
# Un lens es una función que recibe un getter/setter y actúa sobre una estructura
lens_name = Access.key(:name)

# Se puede componer
lens_first_user_name = [Access.key(:users), Access.at(0), Access.key(:name)]

get_in(data, lens_first_user_name)
update_in(data, lens_first_user_name, &String.upcase/1)
```

---

## Exercises

### Ejercicio 1: JSON anidado con get_in / update_in

Tienes una estructura que representa la respuesta de una API REST con usuarios y sus órdenes. Debes acceder y modificar campos específicos usando paths.

```elixir
defmodule Exercise33.NestedAccess do
  @moduledoc """
  Manipulación de JSON anidado usando get_in, put_in, update_in y pop_in.
  """

  @api_response %{
    "status" => "ok",
    "data" => %{
      "users" => [
        %{
          "id" => 1,
          "name" => "Ana García",
          "orders" => [
            %{"id" => 101, "total" => 250.0, "status" => "delivered"},
            %{"id" => 102, "total" => 89.5, "status" => "pending"}
          ]
        },
        %{
          "id" => 2,
          "name" => "Bob Martínez",
          "orders" => [
            %{"id" => 201, "total" => 420.0, "status" => "pending"}
          ]
        }
      ]
    }
  }

  @doc """
  Devuelve el nombre del primer usuario.

  ## Ejemplo

      iex> Exercise33.NestedAccess.first_user_name()
      "Ana García"
  """
  def first_user_name do
    # TODO: usa get_in con Access.key("data"), Access.key("users"), Access.at(0)
    # y Access.key("name") para obtener el nombre sin pattern matching explícito
  end

  @doc """
  Devuelve todos los totales de órdenes del usuario en el índice dado.

  ## Ejemplo

      iex> Exercise33.NestedAccess.order_totals(0)
      [250.0, 89.5]
  """
  def order_totals(user_index) do
    # TODO: usa get_in con Access.at(user_index) y Access.all() (o un map manual)
    # para extraer los totales de todas las órdenes del usuario
  end

  @doc """
  Marca todas las órdenes pendientes de un usuario como "processing".

  ## Ejemplo

      iex> updated = Exercise33.NestedAccess.process_pending_orders(1)
      iex> get_in(updated, ["data", "users", Access.at(1), "orders", Access.at(0), "status"])
      "processing"
  """
  def process_pending_orders(user_index) do
    # TODO: usa update_in con Access.filter para seleccionar solo órdenes cuyo
    # "status" == "pending" y actualiza el status a "processing"
    # Path: ["data", "users", Access.at(user_index), "orders", Access.filter(...)]
  end

  @doc """
  Extrae y elimina el campo "status" del response raíz.
  Devuelve {status_value, response_sin_status}.

  ## Ejemplo

      iex> {status, rest} = Exercise33.NestedAccess.pop_status()
      iex> status
      "ok"
      iex> Map.has_key?(rest, "status")
      false
  """
  def pop_status do
    # TODO: usa pop_in sobre @api_response con el path correcto
  end

  # Datos accesibles para pruebas
  def api_response, do: @api_response
end
```

### Ejercicio 2: Implementar Access en un struct propio

Implementa el behaviour `Access` para un struct `RingBuffer` que mantiene un buffer circular de tamaño fijo. Al implementar `Access`, podrás usar `get_in`, `put_in` y `pop_in` con índices sobre el buffer.

```elixir
defmodule Exercise33.RingBuffer do
  @moduledoc """
  Buffer circular de tamaño fijo con soporte para el behaviour Access.

  Permite usar get_in/put_in/pop_in con índices enteros.
  Los índices negativos acceden desde el final (como Python).
  """

  @behaviour Access

  defstruct [:size, :data, :head]

  @doc """
  Crea un buffer circular vacío de tamaño `size`.
  """
  def new(size) when is_integer(size) and size > 0 do
    %__MODULE__{
      size: size,
      data: :array.new(size, default: nil),
      head: 0
    }
  end

  @doc """
  Inserta un elemento en la posición actual del head y avanza el puntero.
  """
  def push(%__MODULE__{size: size, data: data, head: head} = buf, value) do
    new_data = :array.set(head, value, data)
    %{buf | data: new_data, head: rem(head + 1, size)}
  end

  # Normaliza índices negativos (ej: -1 => size - 1)
  defp normalize_index(index, size) when index < 0, do: size + index
  defp normalize_index(index, _size), do: index

  @impl Access
  def fetch(%__MODULE__{size: size, data: data}, index) do
    # TODO: normaliza el índice con normalize_index/2
    # Si el índice normalizado está fuera de [0, size-1], devuelve :error
    # Si el valor en :array.get/2 es nil, devuelve :error
    # Caso exitoso: devuelve {:ok, value}
  end

  @impl Access
  def get_and_update(%__MODULE__{size: size, data: data} = buf, index, fun) do
    # TODO: obtiene el valor actual en el índice (nil si fuera de rango)
    # Aplica fun.(current_value) — puede devolver {get, update} o :pop
    # Caso :pop: llama a pop/2
    # Caso {get, new_value}: actualiza :array en el índice, devuelve {get, nuevo_buf}
    # Normaliza el índice antes de operar
  end

  @impl Access
  def pop(%__MODULE__{size: size, data: data} = buf, index) do
    # TODO: extrae el valor en el índice (normalizando), lo reemplaza por nil en data
    # Devuelve {valor_extraido, buf_actualizado}
    # Si el índice está fuera de rango, devuelve {nil, buf}
  end

  @doc """
  Convierte el buffer a lista (puede contener nils para posiciones vacías).
  """
  def to_list(%__MODULE__{size: size, data: data}) do
    for i <- 0..(size - 1), do: :array.get(i, data)
  end
end
```

Ejemplo de uso esperado en iex:

```elixir
iex> buf = Exercise33.RingBuffer.new(3)
iex> buf = buf |> RingBuffer.push(10) |> RingBuffer.push(20) |> RingBuffer.push(30)
iex> get_in(buf, [0])
10
iex> get_in(buf, [-1])
30
iex> {val, buf2} = pop_in(buf, [1])
iex> val
20
iex> Exercise33.RingBuffer.to_list(buf2)
[10, nil, 30]
iex> put_in(buf, [0], 99) |> Exercise33.RingBuffer.to_list()
[99, 20, 30]
```

### Ejercicio 3: Lenses compuestos sobre Access

Crea un módulo `Lens` que proporcione helpers para componer paths de Access de forma más expresiva y reutilizable.

```elixir
defmodule Exercise33.Lens do
  @moduledoc """
  Lenses compuestos sobre el behaviour Access de Elixir.

  Un lens es simplemente una lista de accessors que forma un path.
  Este módulo proporciona combinadores para construirlos de forma expresiva.
  """

  @type lens :: [term()]

  @doc """
  Lens que apunta a una clave de mapa o struct.

  ## Ejemplo

      iex> Exercise33.Lens.key(:name) |> Exercise33.Lens.get(%{name: "Ana"})
      "Ana"
  """
  def key(k), do: [Access.key(k)]

  @doc """
  Lens que apunta a una clave de mapa con string.
  """
  def key_str(k), do: [k]

  @doc """
  Lens que apunta a un índice de lista.
  """
  def at(index), do: [Access.at(index)]

  @doc """
  Lens que selecciona elementos de lista que cumplen el predicado.
  """
  def filter(pred), do: [Access.filter(pred)]

  @doc """
  Compone dos lenses en secuencia (el resultado es un path más profundo).

  ## Ejemplo

      iex> lens = Exercise33.Lens.key(:users) |> Exercise33.Lens.then(Exercise33.Lens.at(0))
      iex> Exercise33.Lens.get(lens, %{users: ["Ana", "Bob"]})
      "Ana"
  """
  def then(lens_a, lens_b) do
    # TODO: concatena las dos listas de accessors
  end

  @doc """
  Obtiene el valor apuntado por el lens en la estructura.
  """
  def get(lens, structure) do
    # TODO: usa get_in/2 con lens como path
  end

  @doc """
  Actualiza el valor apuntado por el lens aplicando la función.
  """
  def over(lens, structure, fun) do
    # TODO: usa update_in/3 con lens como path
  end

  @doc """
  Establece el valor apuntado por el lens.
  """
  def set(lens, structure, value) do
    # TODO: usa put_in/3 con lens como path
  end

  @doc """
  Extrae y elimina el valor apuntado por el lens.
  """
  def pop(lens, structure) do
    # TODO: usa pop_in/2 con lens como path
  end

  @doc """
  Crea un lens que navega por una lista de claves anidadas.

  ## Ejemplo

      iex> lens = Exercise33.Lens.path([:user, :profile, :name])
      iex> Exercise33.Lens.get(lens, %{user: %{profile: %{name: "Ana"}}})
      "Ana"
  """
  def path(keys) when is_list(keys) do
    # TODO: convierte cada clave en un Access.key/1 y concatena todo
    # Pista: Enum.flat_map(keys, &key/1)
  end
end
```

Ejemplo de uso compuesto:

```elixir
iex> alias Exercise33.Lens
iex> data = %{users: [%{name: "Ana", score: 100}, %{name: "Bob", score: 80}]}

# Leer el score del primer usuario
iex> lens = Lens.key(:users) |> Lens.then(Lens.at(0)) |> Lens.then(Lens.key(:score))
iex> Lens.get(lens, data)
100

# Incrementar el score del segundo usuario
iex> Lens.over(
...>   Lens.key(:users) |> Lens.then(Lens.at(1)) |> Lens.then(Lens.key(:score)),
...>   data,
...>   &(&1 + 10)
...> )
%{users: [%{name: "Ana", score: 100}, %{name: "Bob", score: 90}]}

# Poner a false a todos los usuarios que no sean "Ana"
iex> Lens.over(
...>   Lens.key(:users) |> Lens.then(Lens.filter(&(&1.name != "Ana"))),
...>   data,
...>   &Map.put(&1, :active, false)
...> )
```

---

## Common Mistakes

**1. Confundir `Access.key/1` con el acceso directo por átomo**

```elixir
# Incorrecto — falla en structs porque los structs no implementan
# el acceso con corchetes de mapa para claves arbitrarias
get_in(my_struct, [:field])  # puede fallar

# Correcto para structs
get_in(my_struct, [Access.key(:field)])
```

**2. Mutar en vez de devolver la estructura actualizada**

`put_in` y `update_in` son funciones puras. El resultado debe capturarse:

```elixir
# Error: la variable original no cambia
put_in(data, [:user, :name], "Carlos")
IO.inspect(data)  # sigue siendo el original

# Correcto
data = put_in(data, [:user, :name], "Carlos")
```

**3. `get_and_update/3` debe devolver `{get, update}` o `:pop`**

```elixir
# Incorrecto — la función pasada devuelve solo el nuevo valor
get_and_update(map, :key, fn _v -> "nuevo" end)  # error en runtime

# Correcto
get_and_update(map, :key, fn v -> {v, "nuevo"} end)
# o
get_and_update(map, :key, fn _v -> :pop end)
```

**4. Índices negativos no soportados por defecto**

Las listas de Elixir no soportan índices negativos en `Access.at/1`. Solo `Access.at(n)` con `n >= 0` funciona en listas estándar.

**5. `Access.filter/1` siempre devuelve lista**

Aunque el path apunte a un solo elemento, `Access.filter` trabaja sobre colecciones. El resultado de un `get_in` con `filter` es siempre una lista:

```elixir
get_in([%{a: 1}, %{a: 2}], [Access.filter(&(&1.a > 1))])
# => [%{a: 2}]  — lista, no el elemento directamente
```

---

## Verification

```bash
# Crea un proyecto mix o usa iex directamente
iex -S mix

# Ejercicio 1
iex> Exercise33.NestedAccess.first_user_name()
"Ana García"

iex> Exercise33.NestedAccess.order_totals(0)
[250.0, 89.5]

iex> updated = Exercise33.NestedAccess.process_pending_orders(1)
iex> get_in(updated, ["data", "users", Access.at(1), "orders", Access.at(0), "status"])
"processing"

iex> {status, rest} = Exercise33.NestedAccess.pop_status()
iex> status
"ok"

# Ejercicio 2
iex> buf = Exercise33.RingBuffer.new(4)
iex> buf = Enum.reduce(1..4, buf, &Exercise33.RingBuffer.push(&2, &1))
iex> get_in(buf, [0])
1
iex> get_in(buf, [-1])
4
iex> {2, _} = pop_in(buf, [1])

# Ejercicio 3
iex> alias Exercise33.Lens
iex> data = %{users: [%{name: "Ana", score: 100}]}
iex> lens = Lens.key(:users) |> Lens.then(Lens.at(0)) |> Lens.then(Lens.key(:name))
iex> Lens.get(lens, data)
"Ana"
iex> Lens.set(lens, data, "Beatriz")
%{users: [%{name: "Beatriz", score: 100}]}
```

Test con ExUnit:

```elixir
defmodule Exercise33Test do
  use ExUnit.Case, async: true

  alias Exercise33.{NestedAccess, RingBuffer, Lens}

  describe "NestedAccess" do
    test "first_user_name/0 devuelve el nombre del primer usuario" do
      assert NestedAccess.first_user_name() == "Ana García"
    end

    test "order_totals/1 devuelve todos los totales del usuario 0" do
      assert NestedAccess.order_totals(0) == [250.0, 89.5]
    end

    test "process_pending_orders/1 cambia pending a processing" do
      updated = NestedAccess.process_pending_orders(1)
      status = get_in(updated, ["data", "users", Access.at(1), "orders", Access.at(0), "status"])
      assert status == "processing"
    end

    test "pop_status/0 extrae el campo status" do
      {status, rest} = NestedAccess.pop_status()
      assert status == "ok"
      refute Map.has_key?(rest, "status")
    end
  end

  describe "RingBuffer Access" do
    setup do
      buf =
        RingBuffer.new(4)
        |> RingBuffer.push(10)
        |> RingBuffer.push(20)
        |> RingBuffer.push(30)
        |> RingBuffer.push(40)

      {:ok, buf: buf}
    end

    test "fetch/2 obtiene elemento por índice positivo", %{buf: buf} do
      assert get_in(buf, [0]) == 10
      assert get_in(buf, [2]) == 30
    end

    test "fetch/2 obtiene elemento por índice negativo", %{buf: buf} do
      assert get_in(buf, [-1]) == 40
      assert get_in(buf, [-2]) == 30
    end

    test "pop/2 extrae y elimina el elemento", %{buf: buf} do
      {val, updated} = pop_in(buf, [1])
      assert val == 20
      assert RingBuffer.to_list(updated) == [10, nil, 30, 40]
    end

    test "put_in actualiza un elemento", %{buf: buf} do
      updated = put_in(buf, [0], 99)
      assert RingBuffer.to_list(updated) == [99, 20, 30, 40]
    end
  end

  describe "Lens" do
    test "composición de lenses con then/2" do
      data = %{users: [%{name: "Ana"}]}
      lens = Lens.key(:users) |> Lens.then(Lens.at(0)) |> Lens.then(Lens.key(:name))
      assert Lens.get(lens, data) == "Ana"
    end

    test "path/1 crea lens desde lista de claves" do
      data = %{a: %{b: %{c: 42}}}
      lens = Lens.path([:a, :b, :c])
      assert Lens.get(lens, data) == 42
    end

    test "over/3 transforma el valor" do
      data = %{score: 10}
      result = Lens.over(Lens.key(:score), data, &(&1 * 2))
      assert result == %{score: 20}
    end
  end
end
```

---

## Summary

- `Access` es un behaviour con tres callbacks: `fetch/2`, `get_and_update/3`, `pop/2`
- Los macros `get_in`, `put_in`, `update_in`, `pop_in` usan estos callbacks para navegar paths anidados
- `Access.key/1`, `Access.at/1`, `Access.filter/1` son accessors listos para componer en paths
- Implementar `Access` en un struct propio lo hace compatible con toda la maquinaria de paths
- El patrón *lens* emerge naturalmente de componer listas de accessors, sin necesidad de librerías externas para casos simples

---

## What's Next

- **Ejercicio 34**: Collectable y Enumerable — cómo se integran los protocolos que hacen posible `Enum.map`, `Enum.into`, y `for`
- **Ecto.Changeset**: usa `put_in`/`Access` internamente para gestionar campos anidados
- **Librería Lens**: [`lens`](https://hex.pm/packages/lens) en Hex ofrece lenses de orden superior más potentes
- **Elixir 1.17+**: `Access.all/0` como accessor de "todos los elementos" en listas

---

## Resources

- [Documentación oficial `Access`](https://hexdocs.pm/elixir/Access.html)
- [Kernel — get_in/put_in/update_in](https://hexdocs.pm/elixir/Kernel.html#get_in/2)
- [Elixir School — Access](https://elixirschool.com/en/lessons/intermediate/access_behaviour)
- [Blog: Lenses in Elixir](https://www.elixirnewbie.com/articles/access-behaviour-in-elixir)
