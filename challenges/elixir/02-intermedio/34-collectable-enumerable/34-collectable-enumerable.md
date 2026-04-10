# 34: Collectable y Enumerable

## Prerequisites

- Protocols en Elixir (ejercicio 08)
- `Enum` y `Stream` básicos (ejercicios 11)
- Pattern matching con structs
- Behaviours: qué es un callback `@impl`

---

## Learning Objectives

Al finalizar este ejercicio serás capaz de:

1. Explicar qué callbacks implementa `Enumerable` y por qué `reduce/3` es el único verdaderamente obligatorio
2. Implementar `Enumerable` en una colección propia para que funcione con todo `Enum` y `Stream`
3. Usar `Collectable.into/1` para entender qué ocurre detrás de `Enum.into/2` y las comprensiones `for`
4. Implementar `Collectable` en una colección para que pueda recibir elementos
5. Comprender cómo `Stream` se integra con ambos protocolos sin materializar la colección

---

## Concepts

### El protocolo Enumerable

`Enumerable` define cómo una estructura puede ser recorrida. El protocolo tiene cuatro callbacks:

```elixir
@callback reduce(t(), acc(), reducer()) :: result()
@callback count(t()) :: {:ok, non_neg_integer()} | {:error, module()}
@callback member?(t(), term()) :: {:ok, boolean()} | {:error, module()}
@callback slice(t()) :: {:ok, non_neg_integer(), slicer()} | {:error, module()}
```

Solo `reduce/3` es el que tiene que implementar la lógica real. Los demás pueden delegar en el módulo padre si no se puede hacer de forma eficiente:

```elixir
# Cuando count/member?/slice no pueden ser optimizados:
def count(_), do: {:error, __MODULE__}
def member?(_, _), do: {:error, __MODULE__}
def slice(_), do: {:error, __MODULE__}
```

Al devolver `{:error, __MODULE__}`, Elixir usa `reduce/3` internamente para resolver esas operaciones (de forma menos eficiente, pero correcta).

### La firma de reduce/3

```elixir
# acc es {:cont, acumulador} | {:halt, acumulador} | {:suspend, acumulador}
# reducer es fn elemento, acumulador -> acumulador_resultado
@type acc :: {:cont, term()} | {:halt, term()} | {:suspend, term()}
@type reducer :: (term(), term() -> acc())
@type result :: {:done, term()} | {:halted, term()} | {:suspended, term(), continuation()}
```

El pattern básico para una lista:

```elixir
defimpl Enumerable, for: MyCollection do
  def reduce(_, {:halt, acc}, _fun), do: {:halted, acc}
  def reduce(coll, {:suspend, acc}, fun), do: {:suspended, acc, &reduce(coll, &1, fun)}
  def reduce(%MyCollection{items: []}, {:cont, acc}, _fun), do: {:done, acc}
  def reduce(%MyCollection{items: [h | t]}, {:cont, acc}, fun) do
    reduce(%MyCollection{items: t}, fun.(h, acc), fun)
  end
end
```

### El protocolo Collectable

`Collectable` define cómo se pueden insertar elementos en una estructura. Es la contraparte de `Enumerable`:

```elixir
@callback into(t()) :: {term(), (term(), command() -> t() | term())}
# command() :: {:cont, term()} | :done | :halt
```

`into/1` devuelve una tupla `{accumulator, collector_fun}`. La función recolectora recibe:
- `{:cont, element}` — añadir un elemento
- `:done` — finalizar, devolver la colección construida
- `:halt` — cancelar, liberar recursos si los hay

```elixir
defimpl Collectable, for: MyCollection do
  def into(%MyCollection{} = initial) do
    collector = fn
      acc, {:cont, element} -> MyCollection.add(acc, element)
      acc, :done -> acc
      _acc, :halt -> :ok
    end
    {initial, collector}
  end
end
```

### Enum.into/2 y las comprensiones for

`Enum.into/2` usa `Collectable`:

```elixir
# Estas dos expresiones son equivalentes:
Enum.into([1, 2, 3], MyCollection.new())

for x <- [1, 2, 3], into: MyCollection.new(), do: x
```

Internamente, Elixir llama a `Collectable.into(colectable)`, itera la fuente aplicando `{:cont, elem}` a la función recolectora, y finalmente llama `:done`.

### Stream y la integración

`Stream` es perezoso: sus operaciones devuelven funciones que solo se evalúan cuando algo consume el stream. Lo que hace posible esto es que `Stream.t()` implementa `Enumerable`:

```elixir
# Stream.map devuelve una estructura Stream, no una lista
stream = Stream.map(1..1_000_000, &(&1 * 2))

# La evaluación ocurre aquí, cuando Enum.take llama a reduce
Enum.take(stream, 5)  # => [2, 4, 6, 8, 10]
```

Cualquier colección que implemente `Enumerable` puede alimentar a `Stream`:

```elixir
my_coll = MyCollection.new([1, 2, 3, 4, 5])
my_coll
|> Stream.filter(&rem(&1, 2) == 0)
|> Stream.map(&(&1 * 10))
|> Enum.to_list()
# => [20, 40]
```

---

## Exercises

### Ejercicio 1: PriorityQueue con Enumerable

Implementa una cola de prioridad min-heap simple. Al implementar `Enumerable`, se puede usar con `Enum.sort`, `Enum.map`, `Enum.count`, etc.

```elixir
defmodule Exercise34.PriorityQueue do
  @moduledoc """
  Cola de prioridad mínima usando una lista ordenada como representación interna.

  Implementa Enumerable para integrarse con Enum y Stream.
  Los elementos se enumeran en orden de prioridad ascendente (menor primero).
  """

  defstruct items: []

  @doc "Crea una cola vacía."
  def new, do: %__MODULE__{}

  @doc "Crea una cola desde una lista de {prioridad, valor}."
  def new(items) when is_list(items) do
    sorted = Enum.sort_by(items, &elem(&1, 0))
    %__MODULE__{items: sorted}
  end

  @doc "Inserta un elemento con prioridad dada."
  def push(%__MODULE__{items: items} = q, priority, value) do
    new_items = [{priority, value} | items] |> Enum.sort_by(&elem(&1, 0))
    %{q | items: new_items}
  end

  @doc "Extrae el elemento de menor prioridad. Devuelve {{priority, value}, cola_restante} o :empty."
  def pop(%__MODULE__{items: []}), do: :empty
  def pop(%__MODULE__{items: [head | rest]}), do: {head, %__MODULE__{items: rest}}

  @doc "True si la cola está vacía."
  def empty?(%__MODULE__{items: []}), do: true
  def empty?(_), do: false
end

defimpl Enumerable, for: Exercise34.PriorityQueue do
  alias Exercise34.PriorityQueue

  # Caso base de halt: el consumidor pidió parar
  def reduce(_, {:halt, acc}, _fun), do: {:halted, acc}

  # Caso suspend: el consumidor quiere pausar (usado por Stream)
  def reduce(q, {:suspend, acc}, fun) do
    {:suspended, acc, &reduce(q, &1, fun)}
  end

  # Caso base done: la cola está vacía
  def reduce(%PriorityQueue{items: []}, {:cont, acc}, _fun) do
    # TODO: devuelve {:done, acc}
  end

  # Caso recursivo: extrae el primer elemento y continúa
  def reduce(%PriorityQueue{items: [{_priority, value} | rest]}, {:cont, acc}, fun) do
    # TODO: llama fun.(value, acc) para obtener el nuevo acc
    # y luego llama reduce recursivamente con el resto de la cola
    # Nota: value es lo que se entrega al usuario, no la tupla {priority, value}
  end

  def count(%PriorityQueue{items: items}) do
    # TODO: devuelve {:ok, length(items)}
  end

  def member?(%PriorityQueue{items: items}, {_priority, value} = element) do
    # TODO: devuelve {:ok, Enum.member?(items, element)}
    # Si el argumento no es una tupla {p, v}, devuelve {:ok, false}
  end

  def member?(_, _), do: {:ok, false}

  def slice(%PriorityQueue{}) do
    # No podemos hacer slice eficiente sin acceso por índice O(1)
    # TODO: delega devolviendo {:error, __MODULE__}
  end
end
```

Ejemplo de uso:

```elixir
iex> alias Exercise34.PriorityQueue
iex> pq = PriorityQueue.new([{3, :baja}, {1, :alta}, {2, :media}])
iex> Enum.to_list(pq)
[:alta, :media, :baja]
iex> Enum.count(pq)
3
iex> Enum.min_by(pq, & &1)   # ya viene ordenado, el primero es el mínimo
:alta
iex> pq |> Stream.map(&Atom.to_string/1) |> Enum.join(", ")
"alta, media, baja"
```

### Ejercicio 2: Bag (multiset) con Collectable

Un `Bag` es un multiset: colección que permite duplicados y registra la frecuencia de cada elemento. Implementa `Collectable` para poder usar `Enum.into`, y también implementa `Enumerable` para poder recorrerlo.

```elixir
defmodule Exercise34.Bag do
  @moduledoc """
  Multiset: colección que permite duplicados, almacenando frecuencias.

  Implementa Enumerable y Collectable.

  Al enumerar un Bag, cada elemento aparece tantas veces como su frecuencia.
  """

  defstruct freq: %{}

  def new, do: %__MODULE__{}

  def new(items) when is_list(items) do
    Enum.into(items, %__MODULE__{})
  end

  @doc "Añade un elemento, incrementando su frecuencia."
  def add(%__MODULE__{freq: freq} = bag, element) do
    new_freq = Map.update(freq, element, 1, &(&1 + 1))
    %{bag | freq: new_freq}
  end

  @doc "Devuelve la frecuencia de un elemento (0 si no está)."
  def frequency(%__MODULE__{freq: freq}, element) do
    Map.get(freq, element, 0)
  end

  @doc "Devuelve todos los elementos únicos."
  def elements(%__MODULE__{freq: freq}), do: Map.keys(freq)
end

defimpl Collectable, for: Exercise34.Bag do
  alias Exercise34.Bag

  def into(%Bag{} = initial) do
    # TODO: devuelve {initial, collector_fn}
    # collector_fn debe manejar:
    #   (acc, {:cont, element}) -> Bag.add(acc, element)
    #   (acc, :done) -> acc
    #   (_acc, :halt) -> :ok
  end
end

defimpl Enumerable, for: Exercise34.Bag do
  alias Exercise34.Bag

  def reduce(_, {:halt, acc}, _fun), do: {:halted, acc}
  def reduce(bag, {:suspend, acc}, fun), do: {:suspended, acc, &reduce(bag, &1, fun)}

  def reduce(%Bag{freq: freq}, {:cont, acc}, fun) when map_size(freq) == 0 do
    {:done, acc}
  end

  def reduce(%Bag{freq: freq}, {:cont, acc}, fun) do
    # TODO: convierte el mapa de frecuencias a una lista expandida de elementos
    # Pista: para cada {element, count}, repite element count veces
    # Luego itera esa lista llamando a fun.(element, acc) recursivamente
    #
    # Alternativa elegante: expand_freq/1 -> lista plana
    # y luego reduce sobre esa lista
    #
    # Nota: puedes usar una función auxiliar privada expand_freq(%Bag{})
  end

  def count(%Bag{freq: freq}) do
    # TODO: la cantidad total de elementos (suma de todas las frecuencias)
    total = freq |> Map.values() |> Enum.sum()
    {:ok, total}
  end

  def member?(%Bag{freq: freq}, element) do
    {:ok, Map.has_key?(freq, element)}
  end

  def slice(_), do: {:error, __MODULE__}
end
```

Ejemplo de uso:

```elixir
iex> alias Exercise34.Bag
iex> bag = Bag.new([:a, :b, :a, :c, :a, :b])
iex> Bag.frequency(bag, :a)
3
iex> Bag.frequency(bag, :b)
2
iex> Enum.count(bag)
6
iex> Enum.sort(bag)
[:a, :a, :a, :b, :b, :c]
iex> bag2 = Enum.into([:x, :x, :y], Bag.new())
iex> Enum.member?(bag2, :x)
true
iex> for item <- bag2, do: Atom.to_string(item)
["x", "x", "y"]  # el orden puede variar según la implementación
```

### Ejercicio 3: FileWriter con Collectable.into

Implementa un `FileWriter` que acumula líneas de texto y las escribe a un archivo cuando se llama `:done`. Esto permite usar `Enum.into/2` y comprensiones `for ... into:` para escribir streams de datos a disco.

```elixir
defmodule Exercise34.FileWriter do
  @moduledoc """
  Collectable que escribe líneas a un archivo.

  Permite usar Enum.into/2 y `for ... into:` para escribir streams de texto.

  Cada elemento insertado se escribe como una línea (se añade \\n automáticamente).
  Al recibir :done, cierra el archivo. Al recibir :halt, cierra sin flush.
  """

  defstruct [:path, :mode]

  @doc """
  Crea un FileWriter para el path dado.
  mode puede ser :write (sobrescribe) o :append (añade al final).
  """
  def new(path, mode \\ :write) when mode in [:write, :append] do
    %__MODULE__{path: path, mode: mode}
  end
end

defimpl Collectable, for: Exercise34.FileWriter do
  alias Exercise34.FileWriter

  def into(%FileWriter{path: path, mode: mode}) do
    # TODO:
    # 1. Abre el archivo con File.open!/2 usando los flags [:write] o [:append, :write]
    #    más [:utf8] para soporte de caracteres especiales
    # 2. El acumulador inicial es el IO device (el pid devuelto por File.open!)
    # 3. La collector_fn debe:
    #    - {:cont, line} -> escribe "#{line}\n" con IO.write/2, devuelve el device
    #    - :done -> cierra el archivo con File.close/1, devuelve el %FileWriter{}
    #    - :halt -> cierra el archivo con File.close/1, devuelve :ok
  end
end
```

Ejemplo de uso:

```elixir
iex> alias Exercise34.FileWriter

# Escribir una lista de líneas a un archivo
iex> ["hola", "mundo", "desde", "Elixir"]
...> |> Enum.into(FileWriter.new("/tmp/test_output.txt"))
%Exercise34.FileWriter{path: "/tmp/test_output.txt", mode: :write}

iex> File.read!("/tmp/test_output.txt")
"hola\nmundo\ndesde\nElixir\n"

# Con comprensión for
iex> for i <- 1..5, into: FileWriter.new("/tmp/numbers.txt") do
...>   "Línea #{i}"
...> end
%Exercise34.FileWriter{...}

iex> File.read!("/tmp/numbers.txt")
"Línea 1\nLínea 2\nLínea 3\nLínea 4\nLínea 5\n"

# Modo append
iex> ["extra"] |> Enum.into(FileWriter.new("/tmp/test_output.txt", :append))
iex> File.read!("/tmp/test_output.txt")
"hola\nmundo\ndesde\nElixir\nextra\n"

# Stream pipeline -> FileWriter (sin materializar en memoria)
iex> 1..1_000_000
...> |> Stream.filter(&rem(&1, 2) == 0)
...> |> Stream.map(&"par: #{&1}")
...> |> Stream.take(3)
...> |> Enum.into(FileWriter.new("/tmp/pares.txt"))
```

---

## Common Mistakes

**1. Invertir el acumulador en reduce/3**

El patrón de acumulación en `reduce` construye la lista al revés. Si necesitas orden natural, usa `Enum.reverse` al final o acumula en la dirección correcta:

```elixir
# El acumulador se construye así: el último elemento queda primero
def reduce(%Col{items: [h | t]}, {:cont, acc}, fun) do
  reduce(%Col{items: t}, fun.(h, acc), fun)
end
# Si acc empieza en [], el resultado al finalizar es la lista al revés
# Esto es intencional — Enum.to_list/1 invierte internamente si es necesario
```

**2. Olvidar el caso :halt en reduce**

Sin el caso `{:halt, acc}`, `Stream.take/2`, `Enum.find/2` y cualquier operación que corte la enumeración romperá con `FunctionClauseError`:

```elixir
# SIEMPRE incluir como primera cláusula:
def reduce(_, {:halt, acc}, _fun), do: {:halted, acc}
def reduce(col, {:suspend, acc}, fun), do: {:suspended, acc, &reduce(col, &1, fun)}
```

**3. Collectable: no cerrar recursos en :halt**

`:halt` ocurre cuando el consumidor cancela (ej: error en medio de `Enum.into`). Si no cierras archivos o conexiones ahí, tendrás resource leaks:

```elixir
# Incorrecto — pierde el file handle
(device, :halt) -> :ok

# Correcto
(device, :halt) ->
  File.close(device)
  :ok
```

**4. count/1 devuelve {:ok, n} o {:error, module()}**

El valor de retorno no es el entero directamente:

```elixir
# Incorrecto
def count(%Col{items: items}), do: length(items)

# Correcto
def count(%Col{items: items}), do: {:ok, length(items)}
```

**5. member?/2 devuelve {:ok, boolean()} o {:error, module()}**

```elixir
# Incorrecto
def member?(col, elem), do: elem in col.items

# Correcto
def member?(%Col{items: items}, elem), do: {:ok, elem in items}
```

---

## Verification

```bash
iex -S mix
```

```elixir
# PriorityQueue
iex> alias Exercise34.PriorityQueue
iex> pq = PriorityQueue.new([{2, :b}, {1, :a}, {3, :c}])
iex> Enum.to_list(pq)
[:a, :b, :c]
iex> Enum.count(pq)
3
iex> Enum.member?(pq, {1, :a})
true
iex> pq |> Stream.drop(1) |> Enum.to_list()
[:b, :c]

# Bag
iex> alias Exercise34.Bag
iex> bag = Enum.into([:x, :x, :y, :z, :x], Bag.new())
iex> Bag.frequency(bag, :x)
3
iex> Enum.count(bag)
5
iex> Enum.sort(bag)
[:x, :x, :x, :y, :z]

# FileWriter
iex> alias Exercise34.FileWriter
iex> ["línea 1", "línea 2"] |> Enum.into(FileWriter.new("/tmp/fw_test.txt"))
iex> File.read!("/tmp/fw_test.txt")
"línea 1\nlínea 2\n"
```

Test con ExUnit:

```elixir
defmodule Exercise34Test do
  use ExUnit.Case, async: true

  alias Exercise34.{PriorityQueue, Bag, FileWriter}

  describe "PriorityQueue Enumerable" do
    test "enumera en orden de prioridad ascendente" do
      pq = PriorityQueue.new([{3, :c}, {1, :a}, {2, :b}])
      assert Enum.to_list(pq) == [:a, :b, :c]
    end

    test "count/1 devuelve el número de elementos" do
      pq = PriorityQueue.new([{1, :a}, {2, :b}])
      assert Enum.count(pq) == 2
    end

    test "funciona con Stream" do
      pq = PriorityQueue.new([{1, :a}, {2, :b}, {3, :c}])
      result = pq |> Stream.map(&Atom.to_string/1) |> Enum.to_list()
      assert result == ["a", "b", "c"]
    end

    test "halt funciona con Enum.take" do
      pq = PriorityQueue.new([{1, :a}, {2, :b}, {3, :c}])
      assert Enum.take(pq, 2) == [:a, :b]
    end
  end

  describe "Bag Collectable y Enumerable" do
    test "Enum.into/2 añade elementos" do
      bag = Enum.into([:a, :a, :b], Bag.new())
      assert Bag.frequency(bag, :a) == 2
      assert Bag.frequency(bag, :b) == 1
    end

    test "Enum.count devuelve total incluyendo duplicados" do
      bag = Bag.new([:a, :a, :b])
      assert Enum.count(bag) == 3
    end

    test "for ... into: construye el bag" do
      bag = for x <- [:x, :x, :y], into: Bag.new(), do: x
      assert Bag.frequency(bag, :x) == 2
    end
  end

  describe "FileWriter Collectable" do
    @tmp "/tmp/exercise34_test.txt"

    test "escribe líneas al archivo" do
      ["hola", "mundo"] |> Enum.into(FileWriter.new(@tmp))
      assert File.read!(@tmp) == "hola\nmundo\n"
    end

    test "modo append añade sin sobrescribir" do
      File.write!(@tmp, "existente\n")
      ["nueva"] |> Enum.into(FileWriter.new(@tmp, :append))
      assert File.read!(@tmp) == "existente\nnueva\n"
    end

    test "funciona con stream pipeline" do
      1..5
      |> Stream.map(&"item #{&1}")
      |> Enum.into(FileWriter.new(@tmp))
      content = File.read!(@tmp)
      assert String.contains?(content, "item 1")
      assert String.contains?(content, "item 5")
    end
  end
end
```

---

## Summary

- `Enumerable` hace que cualquier colección funcione con `Enum`, `Stream` y las comprensiones `for`
- Solo `reduce/3` requiere lógica real; `count/1`, `member?/2` y `slice/1` pueden delegar con `{:error, __MODULE__}`
- Los tres casos de `reduce` — `:halt`, `:suspend`, `:cont` — son obligatorios para compatibilidad con `Stream`
- `Collectable` es la contraparte: define cómo recibir elementos para construir una colección
- `Enum.into/2` y `for ... into:` usan `Collectable` internamente
- `Stream` es simplemente una colección que implementa `Enumerable` de forma perezosa

---

## What's Next

- **Ejercicio 35**: NimbleParsec — parsers combinados para texto estructurado
- **GenStage**: productor/consumidor que usa `Enumerable` y back-pressure
- **Flow**: `Stream` distribuido en múltiples procesos, construido sobre los mismos protocolos
- **Ecto.Repo.stream/2**: devuelve un `Stream.t()` que implementa `Enumerable` sobre consultas de base de datos

---

## Resources

- [Documentación oficial Enumerable](https://hexdocs.pm/elixir/Enumerable.html)
- [Documentación oficial Collectable](https://hexdocs.pm/elixir/Collectable.html)
- [Enum.into/2](https://hexdocs.pm/elixir/Enum.html#into/2)
- [Elixir School — Protocols](https://elixirschool.com/en/lessons/intermediate/protocols)
- [Blog: Implementing Enumerable in Elixir](https://dockyard.com/blog/2017/01/17/implementing-enumerable-in-elixir)
