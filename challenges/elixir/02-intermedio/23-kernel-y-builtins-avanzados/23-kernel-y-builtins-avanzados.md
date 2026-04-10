# 23. Kernel y Builtins Avanzados

**Difficulty**: Intermedio

## Prerequisites
- Completed exercises 01–22
- Familiarity with maps, nested data structures, and anonymous functions
- Understanding of GenServer basics (exercise 04)
- Comfortable with pattern matching and the pipe operator

## Learning Objectives
After completing this exercise, you will be able to:
- Invoke functions and anonymous functions dinámicamente con `apply/2` y `apply/3`
- Leer valores anidados en estructuras de datos con `get_in/2`
- Actualizar valores anidados de forma inmutable con `put_in/3` y `update_in/3`
- Eliminar claves anidadas con `pop_in/2`
- Usar `__MODULE__` para referencias de módulo portables y auto-documentadas
- Aplicar `Access` para operaciones sobre colecciones anidadas

## Concepts

### apply/2 y apply/3: invocación dinámica

`apply/3` llama a una función en un módulo dado su nombre como átomo — útil cuando el módulo o la función se determinan en runtime. `apply/2` llama a una función anónima con una lista de argumentos.

```elixir
# apply/3: módulo, función, lista de argumentos
apply(String, :upcase, ["hello"])     # => "HELLO"
apply(Enum, :sum, [[1, 2, 3]])        # => 6
apply(Map, :get, [%{a: 1}, :a, nil]) # => 1

# apply/2: función anónima, lista de argumentos
double = fn x -> x * 2 end
apply(double, [5])   # => 10

# Caso de uso: dispatcher genérico
def dispatch(module, action, args) do
  apply(module, action, args)
end
```

`apply` es especialmente útil cuando construyes sistemas de plugins, dispatchers de comandos, o cuando llamas funciones determinadas por configuración en runtime.

### get_in/2: acceso a estructuras anidadas

`get_in/2` acepta una estructura y una lista de claves (el "path"), y navega la estructura siguiendo ese camino. Retorna `nil` si cualquier nivel intermedio no existe, sin lanzar excepción.

```elixir
user = %{
  name: "Alice",
  address: %{
    city: "Barcelona",
    zip: "08001",
    coords: %{lat: 41.3851, lng: 2.1734}
  }
}

get_in(user, [:address, :city])          # => "Barcelona"
get_in(user, [:address, :coords, :lat])  # => 41.3851
get_in(user, [:address, :country])       # => nil (no existe, no falla)
```

Con `Access.all()` puedes traversar listas:

```elixir
catalog = %{
  products: [
    %{name: "Widget", price: 10.0},
    %{name: "Gadget", price: 25.0}
  ]
}

get_in(catalog, [:products, Access.all(), :name])
# => ["Widget", "Gadget"]
```

### put_in/3 y update_in/3: actualización inmutable

`put_in/3` retorna una copia de la estructura con el valor en el path especificado reemplazado. `update_in/3` toma una función que recibe el valor actual y retorna el nuevo valor.

```elixir
user = %{profile: %{name: "Bob", score: 100}}

# put_in: establece un valor
updated = put_in(user, [:profile, :score], 150)
# => %{profile: %{name: "Bob", score: 150}}

# update_in: transforma el valor actual
boosted = update_in(user, [:profile, :score], &(&1 + 50))
# => %{profile: %{name: "Bob", score: 150}}

# update_in sobre listas con Access.all()
cart = %{items: [%{price: 10}, %{price: 20}]}
with_tax = update_in(cart, [:items, Access.all(), :price], &(&1 * 1.1))
# => %{items: [%{price: 11.0}, %{price: 22.0}]}
```

### pop_in/2: eliminar claves anidadas

`pop_in/2` extrae y retorna el valor en el path, devolviendo una tupla `{valor_eliminado, estructura_actualizada}`.

```elixir
data = %{user: %{name: "Carol", temp_token: "abc123"}}

{token, clean_data} = pop_in(data, [:user, :temp_token])
# token      => "abc123"
# clean_data => %{user: %{name: "Carol"}}
```

### __MODULE__: referencias de módulo portables

`__MODULE__` es una macro que se expande al átomo del módulo actual en tiempo de compilación. Hace el código más mantenible porque si renombras el módulo, no necesitas actualizar todas las referencias internas.

```elixir
defmodule MyApp.Cache do
  @moduledoc "..."

  # Sin __MODULE__ — frágil: si renombras el módulo, esto falla
  def start_link(opts), do: GenServer.start_link(MyApp.Cache, opts, name: MyApp.Cache)

  # Con __MODULE__ — robusto: se actualiza solo al renombrar
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  # Útil también en @behaviour y en child specs
  def child_spec(opts) do
    %{id: __MODULE__, start: {__MODULE__, :start_link, [opts]}}
  end
end
```

Dentro de un módulo, `__MODULE__` siempre se refiere al módulo que lo contiene. En módulos anidados, cada nivel tiene su propio `__MODULE__`.

### Kernel.binding/0: variables en scope actual

`binding/0` retorna una keyword list con todas las variables actualmente en scope y sus valores. Principalmente útil para debugging y metaprogramación.

```elixir
x = 42
name = "Alice"
Kernel.binding()  # => [name: "Alice", x: 42]
```

---

## Exercises

### Exercise 1: apply/2 y apply/3 en la práctica

```elixir
defmodule DynamicDispatch do
  @doc """
  Ejercicio: Usa apply/3 y apply/2 para invocar funciones dinámicamente.
  """

  def run do
    # TODO 1: Usa apply/3 para llamar String.upcase con argumento "hello world"
    # Almacena el resultado en `upcased`
    upcased = # TODO

    # TODO 2: Usa apply/3 para llamar Enum.max con [[3, 1, 4, 1, 5, 9]]
    # Almacena el resultado en `max_val`
    max_val = # TODO

    # TODO 3: Define una función anónima que recibe dos números y retorna su suma
    # Luego usa apply/2 para llamarla con argumentos [10, 32]
    adder = # TODO
    sum = # TODO (usa apply/2 con adder y [10, 32])

    # TODO 4: Implementa dispatch/3 que toma (module, function_name, args)
    # y llama la función. Debe funcionar con cualquier módulo y función.
    # dispatch(String, :reverse, ["elixir"]) => "rixile"

    IO.inspect(upcased, label: "upcase")   # => "HELLO WORLD"
    IO.inspect(max_val, label: "max")      # => 9
    IO.inspect(sum, label: "sum")          # => 42
  end

  # TODO 5: Implementa esta función usando apply/3
  def dispatch(module, function_name, args) when is_atom(module) and is_atom(function_name) do
    # PISTA: apply(módulo, átomo_función, lista_args)
    # TODO
  end
end

DynamicDispatch.run()
```

---

### Exercise 2: get_in con maps anidados

```elixir
defmodule NestedAccess do
  @user %{
    id: 1,
    name: "Alice",
    address: %{
      street: "Carrer de Balmes, 42",
      city: "Barcelona",
      country: %{
        code: "ES",
        name: "Spain"
      }
    },
    preferences: %{
      theme: "dark",
      notifications: %{
        email: true,
        sms: false
      }
    }
  }

  def run do
    # TODO 1: Usa get_in para obtener la ciudad del usuario
    city = # TODO
    IO.inspect(city)   # => "Barcelona"

    # TODO 2: Usa get_in para obtener el código de país (dos niveles en :address)
    country_code = # TODO
    IO.inspect(country_code)   # => "ES"

    # TODO 3: Usa get_in para obtener si las notificaciones SMS están activas
    sms_enabled = # TODO
    IO.inspect(sms_enabled)   # => false

    # TODO 4: Usa get_in para acceder a una clave que NO existe (:phone)
    # dentro de :address. Verifica que retorna nil sin crashear.
    phone = # TODO
    IO.inspect(phone)   # => nil

    # TODO 5: Dado este catálogo con lista de productos:
    catalog = %{
      products: [
        %{name: "Widget", category: "tools"},
        %{name: "Gadget", category: "electronics"},
        %{name: "Donut", category: "food"}
      ]
    }
    # Usa get_in con Access.all() para obtener la lista de nombres de productos
    names = # TODO (usa Access.all())
    IO.inspect(names)   # => ["Widget", "Gadget", "Donut"]
  end
end

NestedAccess.run()
```

---

### Exercise 3: put_in para actualización inmutable

```elixir
defmodule NestedUpdate do
  def run do
    user = %{
      id: 42,
      profile: %{
        name: "Bob",
        email: "bob@example.com",
        address: %{
          city: "Madrid",
          zip: "28001"
        }
      }
    }

    # TODO 1: Usa put_in para cambiar el email a "bob.new@example.com"
    # PISTA: el path es [:profile, :email]
    updated_email = # TODO
    IO.inspect(get_in(updated_email, [:profile, :email]))  # => "bob.new@example.com"

    # TODO 2: Usa put_in para agregar el campo :zip "28050" al address
    # (reemplaza el valor actual)
    updated_zip = # TODO
    IO.inspect(get_in(updated_zip, [:profile, :address, :zip]))  # => "28050"

    # TODO 3: Usa put_in para agregar una clave nueva :country "Spain"
    # dentro de :address (que no existía antes)
    with_country = # TODO
    IO.inspect(get_in(with_country, [:profile, :address, :country]))  # => "Spain"

    # Verifica que user original no fue modificado (inmutabilidad)
    IO.inspect(user.profile.email)  # => "bob@example.com" (sin cambios)
  end
end

NestedUpdate.run()
```

---

### Exercise 4: update_in con Access.all()

```elixir
defmodule CartOperations do
  def run do
    cart = %{
      user_id: 99,
      items: [
        %{name: "Book", price: 15.0, qty: 2},
        %{name: "Pen", price: 2.5, qty: 10},
        %{name: "Notebook", price: 8.0, qty: 3}
      ],
      discount: 0.0
    }

    # TODO 1: Usa update_in con Access.all() para aplicar un 10% de impuesto
    # a todos los precios (multiplica cada :price por 1.1)
    # PISTA: update_in(cart, [:items, Access.all(), :price], &(&1 * 1.1))
    with_tax = # TODO
    IO.inspect(get_in(with_tax, [:items, Access.all(), :price]))
    # => [16.5, 2.75, 8.8] (aproximado)

    # TODO 2: Usa update_in para incrementar en 1 la qty de todos los items
    more_items = # TODO
    IO.inspect(get_in(more_items, [:items, Access.all(), :qty]))
    # => [3, 11, 4]

    # TODO 3: Usa update_in para establecer el discount a 5.0
    # PISTA: el path es [:discount], la función puede ser fn _ -> 5.0 end
    discounted = # TODO
    IO.inspect(discounted.discount)   # => 5.0

    # TODO 4: Usa pop_in para extraer el primer item del cart
    # PISTA: Access.at(0) accede al elemento en índice 0 de una lista
    {first_item, remaining_cart} = pop_in(cart, [:items, Access.at(0)])
    IO.inspect(first_item.name)          # => "Book"
    IO.inspect(length(remaining_cart.items))  # => 2
  end
end

CartOperations.run()
```

---

### Exercise 5: __MODULE__ en GenServer

```elixir
defmodule CounterServer do
  use GenServer

  # TODO 1: Usa __MODULE__ en start_link en lugar de escribir CounterServer
  # Esto hace el código portable si renombras el módulo
  def start_link(initial_value \\ 0) do
    # MAL: GenServer.start_link(CounterServer, initial_value, name: CounterServer)
    # BIEN: usa __MODULE__ en ambas posiciones
    GenServer.start_link(# TODO, initial_value, name: # TODO)
  end

  # TODO 2: Usa __MODULE__ en child_spec para hacer portable el child spec
  # Un supervisor usará este spec para arrancar el proceso
  def child_spec(opts) do
    %{
      id: # TODO,  # debe ser __MODULE__
      start: {# TODO, :start_link, [opts]},  # debe ser __MODULE__
      restart: :permanent,
      type: :worker
    }
  end

  # GenServer callbacks (no modificar)
  @impl true
  def init(initial_value), do: {:ok, initial_value}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state, state}

  @impl true
  def handle_cast(:increment, state), do: {:noreply, state + 1}

  # TODO 3: Implementa la función get/0 usando __MODULE__ como nombre del servidor
  # en GenServer.call. Evita escribir CounterServer explícitamente.
  def get do
    GenServer.call(# TODO, :get)
  end

  # TODO 4: Implementa increment/0 usando __MODULE__
  def increment do
    GenServer.cast(# TODO, :increment)
  end

  # TODO 5: En IEx, verifica que __MODULE__ evalúa al átomo del módulo:
  # iex> CounterServer.__info__(:module)
  # CounterServer
  # iex> # Dentro de un módulo, __MODULE__ se expande en compilación
  # Prueba: ¿Qué valor tiene __MODULE__ en un módulo anidado?

  defmodule Stats do
    # TODO: ¿Qué valor retornaría __MODULE__ aquí?
    # Escribe tu respuesta como comentario
    # __MODULE__ => ??? (pista: incluye el módulo padre)
    def module_name, do: __MODULE__
  end
end

# Test
{:ok, _pid} = CounterServer.start_link(0)
CounterServer.increment()
CounterServer.increment()
IO.inspect(CounterServer.get())   # => 2
IO.inspect(CounterServer.Stats.module_name())  # => CounterServer.Stats
```

---

## Common Mistakes

### apply con función de módulo incorrecta

```elixir
# MAL: apply/2 espera una función anónima, no un átomo
apply(:upcase, ["hello"])   # Error

# MAL: apply/3 espera el módulo como átomo, no como string
apply("String", :upcase, ["hello"])   # Error

# BIEN:
apply(String, :upcase, ["hello"])     # => "HELLO"
apply(&String.upcase/1, ["hello"])    # => "HELLO" (con apply/2)
```

### get_in con atom keys en maps con string keys

```elixir
# MAL: Si el map tiene keys de string, no funcionan atom keys
data = %{"name" => "Alice"}
get_in(data, [:name])     # => nil (no encontró la clave)

# BIEN:
get_in(data, ["name"])    # => "Alice"
```

### put_in no crea niveles intermedios inexistentes

```elixir
# MAL: Si :address no existe, put_in lanza KeyError
user = %{name: "Bob"}
put_in(user, [:address, :city], "Madrid")  # ** (KeyError)

# BIEN: asegúrate de que los niveles intermedios existen,
# o usa Map.put para crear el primer nivel
user = Map.put(user, :address, %{})
put_in(user, [:address, :city], "Madrid")  # OK
```

### __MODULE__ en módulos anidados

```elixir
defmodule Outer do
  def outer_module, do: __MODULE__   # => Outer

  defmodule Inner do
    def inner_module, do: __MODULE__  # => Outer.Inner (no Outer!)
  end
end
```

### update_in con Access.all() modifica la lista original

```elixir
# update_in siempre retorna una nueva estructura — Elixir es inmutable
original = %{items: [%{price: 10}]}
updated = update_in(original, [:items, Access.all(), :price], &(&1 * 2))
# original.items[0].price sigue siendo 10 — no fue modificado
```

---

## Try It Yourself

Implementa `deep_merge/2`, una función que mezcla dos maps anidados. Para cada clave: si ambos values son maps, hace merge recursivo; si no, el valor del segundo map sobreescribe al primero.

```elixir
defmodule DeepMerge do
  @doc """
  Merges two nested maps deeply.

  ## Examples

      iex> DeepMerge.deep_merge(%{a: %{x: 1, y: 2}}, %{a: %{y: 99, z: 3}})
      %{a: %{x: 1, y: 99, z: 3}}

      iex> DeepMerge.deep_merge(%{a: 1, b: 2}, %{b: 99, c: 3})
      %{a: 1, b: 99, c: 3}
  """
  def deep_merge(base, override) when is_map(base) and is_map(override) do
    # TODO: Usa Map.merge/3 con una función de resolución de conflictos.
    # La función de resolución recibe (key, base_val, override_val).
    # Si ambos valores son maps, llama deep_merge recursivamente.
    # Si no, retorna override_val.
    # PISTA: Map.merge(base, override, fn _key, base_val, override_val -> ... end)
  end
end

# Tests
IO.inspect DeepMerge.deep_merge(
  %{user: %{name: "Alice", prefs: %{theme: "light", lang: "es"}}},
  %{user: %{prefs: %{theme: "dark"}, role: "admin"}}
)
# => %{user: %{name: "Alice", prefs: %{theme: "dark", lang: "es"}, role: "admin"}}
```
