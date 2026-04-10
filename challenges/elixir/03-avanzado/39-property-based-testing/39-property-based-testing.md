# 39. Property-Based Testing con StreamData

**Difficulty**: Avanzado

## Prerequisites
- Dominio de ExUnit y testing en Elixir
- Comprensión de generadores y tipos algebraicos
- Experiencia con pruebas unitarias convencionales
- Familiaridad con `Enum`, transformaciones de datos y funciones puras

## Learning Objectives
After completing this exercise, you will be able to:
- Escribir propiedades que se mantienen para toda entrada válida, no solo casos específicos
- Usar generadores primitivos de StreamData (`integer`, `binary`, `list_of`, `map_of`)
- Combinar generadores con `map/2`, `filter/2` y `bind/2` para generar datos complejos
- Definir generadores custom para tipos de dominio (Users, Orders, etc.)
- Comprender el proceso de shrinking para encontrar el caso mínimo que rompe una propiedad
- Distinguir cuándo usar property-based testing vs unit testing convencional

## Concepts

### Property-based testing: el cambio de mentalidad

En testing convencional, piensas en ejemplos concretos:
```
"si la lista es [3, 1, 2], sort retorna [1, 2, 3]"
```

En property-based testing, piensas en invariantes:
```
"para CUALQUIER lista de enteros, la lista ordenada tiene la misma longitud que la original"
"para CUALQUIER lista de enteros, sort(sort(list)) == sort(list)"
"para CUALQUIER lista de enteros, todos los elementos están en orden ascendente después de sort"
```

La herramienta genera cientos o miles de ejemplos aleatorios y verifica que la propiedad se mantiene en todos.

### StreamData: generadores y propiedades

```elixir
# mix.exs
{:stream_data, "~> 0.6"}

defmodule MyTest do
  use ExUnit.Case
  use ExUnitProperties  # Habilita el macro check all/2

  property "la longitud de sort(list) == longitud de list" do
    check all list <- list_of(integer()) do
      sorted = Enum.sort(list)
      assert length(sorted) == length(list)
    end
  end
end
```

### Generadores primitivos

```elixir
StreamData.integer()              # Cualquier entero
StreamData.integer(1..100)        # Entero en rango
StreamData.float()                # Cualquier float
StreamData.boolean()              # true | false
StreamData.binary()               # Binario aleatorio
StreamData.string(:alphanumeric)  # String alphanumerico
StreamData.atom(:alphanumeric)    # Átomo
StreamData.list_of(gen)           # Lista de elementos generados por gen
StreamData.map_of(key_gen, val_gen) # Mapa con claves/valores generados
StreamData.one_of([gen1, gen2])   # Elegir uno de los generadores
StreamData.constant(value)        # Siempre el mismo valor
StreamData.tuple({gen1, gen2})    # Tupla con elementos generados
```

### Combinadores

```elixir
# map: transformar el resultado de un generador
non_negative = StreamData.map(StreamData.integer(), &abs/1)

# filter: descartar valores que no cumplen una condición
positive = StreamData.filter(StreamData.integer(), &(&1 > 0))
# CUIDADO: filter puede hacer el generador lento si filtra muchos valores

# bind: usar el resultado de un generador para crear otro
# (para generar datos dependientes)
list_with_elem = StreamData.bind(
  StreamData.list_of(StreamData.integer(), min_length: 1),
  fn list ->
    index = StreamData.integer(0..(length(list) - 1))
    StreamData.map(index, fn i -> {list, Enum.at(list, i)} end)
  end
)
```

### Shrinking: el superpoder

Cuando una propiedad falla, StreamData no solo reporta el caso que falló — intenta encontrar el **caso mínimo** que sigue fallando. Este proceso se llama shrinking.

```
Ejemplo: propiedad "no hay duplicados" falla con [5, 3, 1, 3, 7, 2]
Shrinking encuentra: [3, 3]  ← caso mínimo que reproduce el fallo
```

Shrinking es automático en StreamData — está integrado en el diseño de los generadores.

### Cuándo usar property-based testing

| Situation | Property-based | Unit test |
|---|---|---|
| Funciones puras con invariantes matemáticos | ✓ | |
| Parsing/serialización (roundtrip) | ✓ | |
| Algoritmos de ordenación/búsqueda | ✓ | |
| Casos de negocio específicos | | ✓ |
| Errores con ejemplos concretos (regression) | | ✓ |
| Lógica de UI o efectos secundarios | | ✓ |

## Exercises

### Exercise 1: Basic Properties — Invariantes de sort y manipulación de listas

Escribe propiedades que capturan invariantes matemáticos de funciones de lista.

```elixir
defmodule ListPropertiesTest do
  use ExUnit.Case
  use ExUnitProperties

  # Módulo bajo prueba (funciones "a implementar" para el ejercicio)
  defmodule MyList do
    @doc "Ordena una lista de enteros en orden ascendente"
    def sort(list), do: Enum.sort(list)

    @doc "Elimina duplicados preservando el orden de primera aparición"
    def unique(list), do: Enum.uniq(list)

    @doc "Invierte una lista"
    def reverse(list), do: Enum.reverse(list)

    @doc "Aplana una lista de listas un nivel"
    def flatten(list_of_lists), do: Enum.concat(list_of_lists)
  end

  # ===== Propiedades de sort/1 =====

  property "sort: la longitud se preserva" do
    check all list <- list_of(integer()) do
      # TODO: Verificar que length(sort(list)) == length(list)
      assert length(MyList.sort(list)) == length(list)
    end
  end

  property "sort: el resultado es idempotente (sort(sort(x)) == sort(x))" do
    check all list <- list_of(integer()) do
      # TODO: Verificar que ordenar dos veces es igual que ordenar una
      sorted = MyList.sort(list)
      assert MyList.sort(sorted) == sorted
    end
  end

  property "sort: el resultado está en orden no-decreciente" do
    check all list <- list_of(integer()) do
      sorted = MyList.sort(list)
      # TODO: Verificar que cada elemento es <= al siguiente
      # Pista: Enum.chunk_every/4 con step: 1 da pares consecutivos
      pairs = Enum.zip(sorted, Enum.drop(sorted, 1))
      assert Enum.all?(pairs, fn {a, b} -> a <= b end)
    end
  end

  property "sort: todos los elementos del original están en el resultado" do
    check all list <- list_of(integer()) do
      sorted = MyList.sort(list)
      # TODO: Verificar que Enum.sort(list) == Enum.sort(sorted)
      # (mismos elementos, no importa el orden de comparación)
      assert Enum.sort(list) == Enum.sort(sorted)
    end
  end

  # ===== Propiedades de reverse/1 =====

  property "reverse: double reverse es identidad" do
    check all list <- list_of(integer()) do
      # TODO: list |> reverse |> reverse == list
      assert list |> MyList.reverse() |> MyList.reverse() == list
    end
  end

  property "reverse: preserva la longitud" do
    check all list <- list_of(integer()) do
      assert length(MyList.reverse(list)) == length(list)
    end
  end

  # ===== Propiedades de unique/1 =====

  property "unique: el resultado no tiene duplicados" do
    check all list <- list_of(integer()) do
      unique = MyList.unique(list)
      # TODO: Verificar que length(unique) == length(Enum.uniq(unique))
      # (el propio resultado no tiene duplicados)
      assert length(unique) == length(Enum.uniq(unique))
    end
  end

  property "unique: todos los elementos del resultado estaban en el original" do
    check all list <- list_of(integer()) do
      unique = MyList.unique(list)
      # TODO: Verificar que cada elem de unique está en list
      assert Enum.all?(unique, &(&1 in list))
    end
  end

  # ===== Propiedades de flatten/1 =====

  property "flatten: la longitud total es la suma de las longitudes internas" do
    check all list_of_lists <- list_of(list_of(integer())) do
      flattened = MyList.flatten(list_of_lists)
      total_length = Enum.sum(Enum.map(list_of_lists, &length/1))
      # TODO: Verificar la longitud del resultado
      assert length(flattened) == total_length
    end
  end
end

# mix test test/list_properties_test.exs
# StreamData ejecuta 100 casos por defecto (configurable con max_runs)
```

**Hints**:
- Las propiedades más poderosas son las que capturan **múltiples invariantes**: longitud, contenido, orden — no solo uno
- Si una propiedad pasa siempre pero es trivialmente true (ej: `assert true`), no estás testeando nada. La propiedad debe ser falsificable
- `check all` puede generar múltiples valores en el mismo bloque: `check all a <- integer(), b <- integer() do`

**One possible solution** (sparse):
```elixir
# sort idempotente — ya resuelto arriba

# sort en orden no-decreciente:
pairs = Enum.zip(sorted, Enum.drop(sorted, 1))
assert Enum.all?(pairs, fn {a, b} -> a <= b end)

# unique sin duplicados:
assert length(unique) == length(Enum.uniq(unique))
```

---

### Exercise 2: Custom Generator — Generador de Users válidos

Crea generadores de datos de dominio complejos combinando generadores primitivos.

```elixir
defmodule UserGeneratorTest do
  use ExUnit.Case
  use ExUnitProperties

  # Estructura de dominio
  defmodule User do
    @enforce_keys [:id, :email, :age, :role]
    defstruct [:id, :email, :age, :role, :name]

    @roles [:admin, :editor, :viewer]

    def valid?(%__MODULE__{} = user) do
      user.id > 0 &&
        String.contains?(user.email, "@") &&
        user.age >= 18 && user.age <= 120 &&
        user.role in @roles &&
        (is_nil(user.name) || String.length(user.name) > 0)
    end
  end

  # ===== Generadores custom =====

  def gen_email do
    # TODO: Generar emails válidos del formato "word@word.tld"
    # Pista: usar string(:alphanumeric, min_length: 1) para las partes
    # y constant/1 para el "@" y "."
    ExUnitProperties.gen all(
      local   <- string(:alphanumeric, min_length: 1, max_length: 20),
      domain  <- string(:alphanumeric, min_length: 2, max_length: 15),
      tld     <- one_of([constant("com"), constant("org"), constant("net"), constant("io")])
    ) do
      "#{local}@#{domain}.#{tld}"
    end
  end

  def gen_role do
    # TODO: Generar uno de los roles válidos
    one_of([constant(:admin), constant(:editor), constant(:viewer)])
  end

  def gen_user do
    # TODO: Generar un User válido combinando los generadores anteriores
    ExUnitProperties.gen all(
      id    <- positive_integer(),
      email <- gen_email(),
      age   <- integer(18..120),
      role  <- gen_role(),
      name  <- one_of([
        constant(nil),
        string(:alphanumeric, min_length: 1, max_length: 50)
      ])
    ) do
      %User{id: id, email: email, age: age, role: role, name: name}
    end
  end

  def gen_admin do
    # TODO: Usar gen_user pero filtrar/mapear para que role == :admin
    # Opción A (filter): más simple pero descarta ~66% de valores
    # Opción B (map): más eficiente — mapear gen_user para fijar role
    StreamData.map(gen_user(), fn user -> %{user | role: :admin} end)
  end

  def gen_users_with_min(n) do
    # TODO: Generar lista de al menos n users
    list_of(gen_user(), min_length: n)
  end

  # ===== Tests con generadores custom =====

  property "todo user generado por gen_user es válido" do
    check all user <- gen_user() do
      # TODO: Verificar User.valid?(user)
      assert User.valid?(user), "User inválido: #{inspect(user)}"
    end
  end

  property "todo admin generado por gen_admin tiene role :admin" do
    check all admin <- gen_admin() do
      assert admin.role == :admin
      # TODO: También verificar que el user es válido
      assert User.valid?(admin)
    end
  end

  property "emails generados siempre contienen @" do
    check all email <- gen_email() do
      assert String.contains?(email, "@")
      # TODO: Verificar que hay exactamente un @ y partes non-empty
      parts = String.split(email, "@")
      assert length(parts) == 2
      assert Enum.all?(parts, &(String.length(&1) > 0))
    end
  end

  property "gen_users_with_min genera al menos n users" do
    check all n     <- integer(1..10),
              users <- gen_users_with_min(n) do
      assert length(users) >= n
    end
  end

  property "roundtrip: serializar y deserializar un user preserva los datos" do
    # Propiedad de roundtrip — muy común en property testing para serialización
    check all user <- gen_user() do
      # Simular serialización JSON (con Jason en producción)
      # Aquí usamos inspect/1 y Code.eval_string/1 para la demo
      serialized = %{
        id:    user.id,
        email: user.email,
        age:   user.age,
        role:  to_string(user.role),  # JSON no soporta átomos
        name:  user.name
      }

      deserialized = %User{
        id:    serialized.id,
        email: serialized.email,
        age:   serialized.age,
        role:  String.to_existing_atom(serialized.role),
        name:  serialized.name
      }

      # TODO: Verificar que el user original y el deserializado son iguales
      assert user == deserialized
    end
  end
end

# mix test test/user_generator_test.exs
```

**Hints**:
- `ExUnitProperties.gen all(gen1 <- ..., gen2 <- ...) do ... end` es el macro para construir generadores compuestos
- `StreamData.map/2` es la forma más eficiente de hacer variaciones (eg. fijar un campo); `filter/2` es costoso si descarta muchos valores
- Los generadores son lazy — no generan todos los valores a la vez. Puedes combinarlos sin preocuparte por memoria

**One possible solution** (sparse):
```elixir
# gen_role:
def gen_role do
  one_of([constant(:admin), constant(:editor), constant(:viewer)])
end

# gen_admin (vía map, eficiente):
def gen_admin do
  StreamData.map(gen_user(), fn user -> %{user | role: :admin} end)
end

# Propiedad roundtrip — verificación:
assert user == deserialized
```

---

### Exercise 3: Shrinking — Encontrar el Caso Mínimo que Rompe una Propiedad

Introduce bugs deliberados en funciones y observa cómo StreamData encuentra el caso mínimo.

```elixir
defmodule ShrinkingDemoTest do
  use ExUnit.Case
  use ExUnitProperties

  # Función con un bug sutil — falla para ciertos enteros
  defmodule BuggyMath do
    @doc """
    Intenta calcular el máximo de una lista.
    BUG: Falla si algún elemento es exactamente 42 (número "mágico").
    """
    def max_value([]), do: {:error, :empty_list}
    def max_value(list) do
      result = Enum.max(list)
      if result == 42, do: raise(ArgumentError, "No soportado: 42"), else: {:ok, result}
    end

    @doc """
    Concatena dos listas y las ordena.
    BUG: Falla si alguna lista tiene más de 100 elementos.
    """
    def merge_sorted(a, b) do
      if length(a) + length(b) > 100 do
        raise RuntimeError, "Overflow: demasiados elementos"
      end
      Enum.sort(a ++ b)
    end

    @doc """
    Calcula n factorial.
    BUG: Falla para n >= 13 (overflow de entero en la implementación fake).
    """
    def factorial(0), do: 1
    def factorial(n) when n > 0 and n < 13, do: n * factorial(n - 1)
    def factorial(n) when n >= 13, do: raise(ArithmeticError, "Overflow para n=#{n}")
  end

  # ===== Demostración de shrinking =====

  @tag :skip  # Quitar :skip para ver el fallo y el shrinking
  property "DEMO: BuggyMath.max_value nunca lanza excepción para lista no-vacía" do
    # Esta propiedad FALLARÁ — y StreamData hará shrinking
    check all list <- list_of(integer(), min_length: 1) do
      # La función debería siempre retornar {:ok, _} para listas no-vacías
      # BUG: falla cuando max(list) == 42
      assert match?({:ok, _}, BuggyMath.max_value(list))
    end
    # Después de fallar, StreamData reportará algo como:
    # Input: list = [42]   (shrinking encontró el caso mínimo)
  end

  @tag :skip  # Quitar :skip para ver el fallo y el shrinking
  property "DEMO: merge_sorted es conmutativo (a++b == b++a después de sort)" do
    check all a <- list_of(integer()),
              b <- list_of(integer()) do
      # BUG: falla si length(a) + length(b) > 100
      merged_ab = BuggyMath.merge_sorted(a, b)
      merged_ba = BuggyMath.merge_sorted(b, a)
      assert merged_ab == merged_ba
    end
    # Shrinking encontrará el caso mínimo: dos listas que sumen 101 elementos
  end

  # ===== Cómo LEER el output de shrinking =====

  @tag :manual_exploration  # Test para explorar shrinking manualmente
  test "explorar shrinking manualmente" do
    # Cuando una propiedad falla, StreamData imprime:
    #
    # 1. El caso original que falló (puede ser grande y complejo)
    # 2. "Shrinking .........." (puntos = intentos de shrinking)
    # 3. El caso mínimo que aún falla
    #
    # Ejemplo de output:
    # ** (ExUnit.AssertionError) Expected truthy, got false
    #
    # Generated values (seed: 12345):
    # * #1:
    #   [42]   ← ¡El caso mínimo!

    IO.puts("""
    Para ver shrinking en acción:
    1. Quita el @tag :skip de la primera property
    2. Ejecuta: mix test test/shrinking_demo_test.exs
    3. Observa el output:
       - "Generated values" = caso mínimo que StreamData encontró
       - Los puntos "...." = intentos de reducción
    4. El caso mínimo debería ser [42] — no una lista grande
    """)
  end

  # ===== Propiedades correctas para las mismas funciones =====

  property "max_value: resultado está en la lista original" do
    check all list <- list_of(integer(0..41), min_length: 1) do
      # Usar rango 0..41 para evitar el bug de 42 — en este test queremos que pase
      {:ok, max} = BuggyMath.max_value(list)
      assert max in list
    end
  end

  property "factorial: resultado siempre es positivo para n en 0..12" do
    check all n <- integer(0..12) do
      result = BuggyMath.factorial(n)
      assert is_integer(result)
      assert result > 0
      # Propiedad adicional: n! >= (n-1)! para n > 0
      if n > 0 do
        prev = BuggyMath.factorial(n - 1)
        assert result >= prev
      end
    end
  end

  property "merge_sorted para listas pequeñas: resultado es sorted(a ++ b)" do
    check all a <- list_of(integer(), max_length: 40),
              b <- list_of(integer(), max_length: 40) do
      # max 80 elementos total — dentro del límite de 100
      result = BuggyMath.merge_sorted(a, b)
      expected = Enum.sort(a ++ b)
      assert result == expected
    end
  end

  # ===== Propiedad avanzada: generador condicional =====

  property "generador que produce pares (list, índice_válido)" do
    # Generar una lista y un índice válido para esa lista
    # Ejemplo de bind/2 para generar datos dependientes
    check all {list, index} <- ExUnitProperties.gen all(
                list <- list_of(integer(), min_length: 1),
                index <- integer(0..(length(list) - 1))
              ) do
      elem = Enum.at(list, index)
      # TODO: Verificar que el elemento obtenido está en la lista
      assert elem in list
      # TODO: Verificar que el índice es válido (no retorna nil)
      assert not is_nil(elem)
    end
  end
end

# Ejecutar solo las propiedades que deben pasar:
# mix test test/shrinking_demo_test.exs --exclude manual_exploration

# Para ver el shrinking en acción, desactivar @tag :skip en las properties
# y ejecutar: mix test test/shrinking_demo_test.exs --only skip
```

**Hints**:
- Para observar el shrinking, introduce un bug deliberado o quita el `@tag :skip` de las demos
- El seed del generador se imprime en el output: puedes reproducir exactamente el mismo fallo con `check all ..., seed: 12345 do`
- `integer(0..41)` en el ejercicio es una restricción del generador para evitar el bug de 42 y hacer que la propiedad pase — en producción no harías esto, sino que arreglarías el bug

**One possible solution** (sparse):
```elixir
# Propiedad generador de pares:
check all {list, index} <- gen all(
            list <- list_of(integer(), min_length: 1),
            index <- integer(0..(length(list) - 1))
          ) do
  elem = Enum.at(list, index)
  assert elem in list
  assert not is_nil(elem)
end
```

## Common Mistakes

### Mistake 1: Propiedades que siempre son verdaderas (trivialmente)
```elixir
# ❌ Esta propiedad nunca puede fallar — no testea nada
property "sort retorna una lista" do
  check all list <- list_of(integer()) do
    assert is_list(Enum.sort(list))  # Siempre true
  end
end

# ✓ Capturar invariantes reales que podrían romperse
property "sort: todos los elementos consecutivos están en orden" do
  check all list <- list_of(integer()) do
    sorted = Enum.sort(list)
    Enum.zip(sorted, Enum.drop(sorted, 1))
    |> Enum.each(fn {a, b} -> assert a <= b end)
  end
end
```

### Mistake 2: filter/2 que descarta demasiados valores
```elixir
# ❌ Si el generador filtra el 99% de los valores, los tests son muy lentos
# y StreamData puede fallar con "too many discards"
even_numbers = StreamData.filter(StreamData.integer(), fn n ->
  rem(n, 100) == 0  # Solo múltiplos de 100 — muy restrictivo
end)

# ✓ Usar map para construir exactamente lo que necesitas
even_numbers = StreamData.map(StreamData.integer(), fn n -> n * 2 end)
```

### Mistake 3: Propiedad con efectos secundarios no idempotentes
```elixir
# ❌ El check all se ejecuta 100 veces; si hay efectos secundarios,
# el estado del sistema cambia en cada iteración — los tests se acoplan
property "insertar en DB" do
  check all user <- gen_user() do
    {:ok, _} = Repo.insert(user)  # Inserta 100 veces en la DB real
    # ...
  end
end

# ✓ Las propiedades deben ser sobre funciones puras, o limpiar el estado
# En tests de DB, usar Ecto Sandbox y rollback automático
```

### Mistake 4: No leer el caso de shrinking con atención
```elixir
# Cuando falla una property, el output más importante es:
# Generated values (seed: XXXXX):
# * #1:
#   [42]   ← ESTE es el caso mínimo, no el que falló originalmente

# El caso original puede ser [1, 42, 7, -3, 100] — confuso
# El caso shrunk es [42] — inmediatamente actionable
# Siempre mira el caso shrunk primero
```

## Verification
```bash
# Ejecutar properties que deben pasar
mix test test/list_properties_test.exs
mix test test/user_generator_test.exs

# Ejecutar shrinking demos (fallarán intencionalmente)
# Primero quitar @tag :skip de las properties de BuggyMath
mix test test/shrinking_demo_test.exs 2>&1 | head -50
# Observar el caso shrunk en el output

# Configurar max_runs para más cobertura
# check all list <- list_of(integer()), max_runs: 1000 do
```

Checklist de verificación:
- [ ] Las propiedades capturan invariantes reales, no trivialidades
- [ ] Los generadores custom producen valores válidos en el 100% de los casos
- [ ] `filter/2` se usa con moderación — preferir `map/2`
- [ ] El shrinking reduce casos complejos al mínimo reproducible
- [ ] Las propiedades de roundtrip verifican que serializar → deserializar preserva el valor

## Summary
- Property-based testing complementa (no reemplaza) unit testing: captura edge cases que nunca escribirías manualmente
- Los generadores se componen: de primitivos a tipos de dominio complejos con `gen all`
- `map/2` es más eficiente que `filter/2` — construye exactamente lo que necesitas
- Shrinking es automático y el feature más valioso: transforma "algo falló" en "el caso mínimo es X"
- Las mejores propiedades: roundtrip, idempotencia, conmutatividad, invariantes de tamaño

## What's Next
**40-bypass-http-testing**: Aprende a testear módulos que hacen llamadas HTTP reales, usando Bypass para levantar un servidor HTTP real en los tests.

## Resources
- [StreamData — HexDocs](https://hexdocs.pm/stream_data/StreamData.html)
- [ExUnitProperties — HexDocs](https://hexdocs.pm/stream_data/ExUnitProperties.html)
- [Property-Based Testing with PropEr, Erlang, and Elixir — O'Reilly](https://pragprog.com/titles/fhproper/property-based-testing-with-proper-erlang-and-elixir/)
- [The Anatomy of a Property — Fred Hebert](https://ferd.ca/the-anatomy-of-a-property.html)
