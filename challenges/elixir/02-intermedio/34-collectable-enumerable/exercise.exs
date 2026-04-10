# =============================================================================
# Ejercicio 34: Collectable y Enumerable para Tipos Custom
# Nivel: Intermedio
# =============================================================================
#
# Enumerable permite que tus tipos funcionen con Enum.*, Stream.* y for.
# Collectable permite que tus tipos sean el destino de Enum.into y for...into.
# Juntos hacen que tus tipos sean ciudadanos de primera clase en Elixir.
#
# Conceptos clave:
#   - Enumerable.reduce/3 — la operación fundamental de toda enumeración
#   - Enumerable.count/1  — optimización O(1) del conteo
#   - Enumerable.member?/2 — optimización de búsqueda
#   - Collectable.into/1  — acumular elementos en tu estructura
#
# Para correr: elixir exercise.exs
# =============================================================================

# =============================================================================
# SECCIÓN 1: Tipo Queue y Enumerable.reduce
# =============================================================================
#
# Enumerable.reduce/3 es la función FUNDAMENTAL.
# Toda función de Enum y Stream se puede implementar en términos de reduce.
# Por eso, implementando reduce correctamente, obtienes TODO el ecosistema.
#
# La firma es: reduce(enumerable, acc, fun) → result
# Donde acc puede ser: {:cont, val}, {:halt, val}, {:suspend, val}
#
# Reducer pattern:
#   reduce(enum, {:cont, acc}, fun) → recorre aplicando fun a cada elemento
#   Si fun retorna {:cont, new_acc}     → continuar
#   Si fun retorna {:halt, new_acc}     → detener (usado por Enum.take, etc.)
#   Si fun retorna {:suspend, new_acc}  → pausar (usado por Stream)

IO.puts("=== Sección 1: Enumerable.reduce para Queue ===\n")

defmodule Queue do
  @moduledoc """
  Cola FIFO (First In, First Out) basada en dos listas.
  La lista `front` tiene los primeros elementos listos para dequeue.
  La lista `back` acumula los nuevos elementos (en orden inverso).
  """

  defstruct front: [], back: [], size: 0

  def new(), do: %__MODULE__{}

  def enqueue(%Queue{back: back, size: size} = q, item) do
    %{q | back: [item | back], size: size + 1}
  end

  def dequeue(%Queue{front: [], back: []} = q),
    do: {:empty, q}
  def dequeue(%Queue{front: [], back: back} = q),
    do: dequeue(%{q | front: Enum.reverse(back), back: []})
  def dequeue(%Queue{front: [h | t], size: size} = q),
    do: {h, %{q | front: t, size: size - 1}}

  def empty?(%Queue{front: [], back: []}), do: true
  def empty?(_),                           do: false

  # Helper para convertir a lista (para reduce interno)
  def to_list(%Queue{front: front, back: back}),
    do: front ++ Enum.reverse(back)
end

# TODO 1: Implementa Enumerable para Queue.
#   La función más importante es reduce/3.
#
#   Pista para reduce/3:
#     defimpl Enumerable, for: Queue do
#       def reduce(queue, {:halt, acc}, _fun), do: {:halted, acc}
#       def reduce(queue, {:suspend, acc}, fun), do: {:suspended, acc, &reduce(queue, &1, fun)}
#       def reduce(%Queue{front: [], back: []}, {:cont, acc}, _fun), do: {:done, acc}
#       def reduce(queue, {:cont, acc}, fun) do
#         {item, rest} = Queue.dequeue(queue)
#         reduce(rest, fun.(item, acc), fun)
#       end
#       ...
#     end
#
#   También implementa:
#     count/1   → {:ok, queue.size}   (O(1) porque guardamos el size)
#     member?/2 → recorre la cola buscando el elemento
#     slice/1   → {:error, __MODULE__}  (no implementar slice por ahora)
#
# Tu código aquí:

# --- FIN TODO 1 ---

# Construir una queue
q = Queue.new()
    |> Queue.enqueue(1)
    |> Queue.enqueue(2)
    |> Queue.enqueue(3)
    |> Queue.enqueue(4)
    |> Queue.enqueue(5)

IO.puts("Enum.to_list: #{inspect(Enum.to_list(q))}")
IO.puts("Enum.sum: #{Enum.sum(q)}")
IO.puts("Enum.map *2: #{inspect(Enum.map(q, &(&1 * 2)))}")
IO.puts("Enum.filter par: #{inspect(Enum.filter(q, &(rem(&1, 2) == 0)))}")
IO.puts("Enum.count: #{Enum.count(q)}")
IO.puts("Enum.member? 3: #{Enum.member?(q, 3)}")
IO.puts("Enum.member? 9: #{Enum.member?(q, 9)}\n")

# =============================================================================
# SECCIÓN 2: Collectable.into — construir desde Enum.into
# =============================================================================
#
# Collectable.into/1 retorna {initial_acc, collector_fn}.
# El collector_fn recibe:
#   ({acc, {:cont, item}}) → agregar item, retornar nuevo acc
#   ({acc, :done})         → finalizar, retornar la estructura final
#   ({acc, :halt})         → cancelar operación
#
# Con esto, Enum.into(list, %Queue{}) funciona.

IO.puts("=== Sección 2: Collectable.into para Queue ===\n")

# TODO 2: Implementa Collectable para Queue.
#   Esto permite hacer:
#     Enum.into([1, 2, 3], Queue.new())
#     for x <- 1..5, into: Queue.new(), do: x * 2
#
#   Pista:
#     defimpl Collectable, for: Queue do
#       def into(initial_queue) do
#         collector = fn
#           queue, {:cont, item} -> Queue.enqueue(queue, item)
#           queue, :done         -> queue
#           _queue, :halt        -> :ok
#         end
#         {initial_queue, collector}
#       end
#     end
#
# Tu código aquí:

# --- FIN TODO 2 ---

# Ahora estos deben funcionar:
q_from_list = Enum.into([10, 20, 30, 40], Queue.new())
IO.puts("Queue desde lista: #{inspect(Enum.to_list(q_from_list))}")

q_from_for = for x <- 1..5, into: Queue.new(), do: x * 10
IO.puts("Queue desde for: #{inspect(Enum.to_list(q_from_for))}")

# Combinar: filtrar y meter en queue
even_q = Enum.into(Enum.filter(1..10, &(rem(&1, 2) == 0)), Queue.new())
IO.puts("Queue de pares: #{inspect(Enum.to_list(even_q))}\n")

# =============================================================================
# SECCIÓN 3: Verificar compatibilidad con Enum
# =============================================================================
#
# Con Enumerable + Collectable implementados, el tipo es completamente
# compatible con el ecosistema Elixir.

IO.puts("=== Sección 3: Compatibilidad con Enum ===\n")

q_large = Enum.into(1..20, Queue.new())

IO.puts("Enum.max: #{Enum.max(q_large)}")
IO.puts("Enum.min: #{Enum.min(q_large)}")
IO.puts("Enum.take 3: #{inspect(Enum.take(q_large, 3))}")
IO.puts("Enum.drop 17: #{inspect(Enum.drop(q_large, 17))}")
IO.puts("Enum.reduce sum: #{Enum.reduce(q_large, 0, &+/2)}")
IO.puts("Enum.any? > 15: #{Enum.any?(q_large, &(&1 > 15))}")
IO.puts("Enum.all? > 0: #{Enum.all?(q_large, &(&1 > 0))}")
IO.puts("Enum.sort desc: #{inspect(Enum.sort(q_large, :desc) |> Enum.take(5))}")
IO.puts("Enum.group_by par/impar: #{inspect(Enum.group_by(q_large, &(rem(&1, 2) == 0)))}\n")

# =============================================================================
# SECCIÓN 4: Stream compatibility — lazy enumeration
# =============================================================================
#
# Si Enumerable implementa la suspensión correctamente,
# el tipo es automáticamente compatible con Stream.*

IO.puts("=== Sección 4: Stream Compatibility ===\n")

# TODO 4: Demuestra que la Queue funciona con Stream.*
#
#   A) Stream.map sobre la queue (lazy)
#   B) Stream.filter sobre la queue
#   C) Stream.take (debería ser O(k) no O(n))
#   D) Combinación de streams antes de evaluar con Enum.to_list
#
# Ejemplo:
#   big_q = Enum.into(1..1000, Queue.new())
#   result = big_q
#     |> Stream.filter(&(rem(&1, 2) == 0))
#     |> Stream.map(&(&1 * 3))
#     |> Stream.take(5)
#     |> Enum.to_list()
#   IO.puts("Stream pipeline: #{inspect(result)}")
#
# Tu código aquí:

# --- FIN TODO 4 ---

# =============================================================================
# SECCIÓN 5: Optimized count — O(1) en lugar de O(n)
# =============================================================================
#
# Sin implementar count/1, Enum.count/1 recorre TODOS los elementos (O(n)).
# Si tu tipo mantiene el tamaño internamente, puedes retornar {:ok, n}
# para un count O(1). Esto es lo que hace List.length vs map_size.

IO.puts("=== Sección 5: Count Optimizado ===\n")

# Verificar que count es O(1) para Queue
# (La implementación en TODO 1 ya debe haber hecho esto con {:ok, queue.size})

big_queue = Enum.into(1..10_000, Queue.new())

start = :erlang.monotonic_time(:microsecond)
count = Enum.count(big_queue)
elapsed = :erlang.monotonic_time(:microsecond) - start

IO.puts("Count de 10,000 elementos: #{count}")
IO.puts("Tiempo (debe ser <1ms si es O(1)): #{elapsed}μs")

# Comparar con slice (no implementado, debe decir {:error, __MODULE__})
IO.puts("Slice: #{inspect(Enumerable.slice(big_queue))}\n")

# =============================================================================
# SECCIÓN 6: Implementar ambos para RingBuffer
# =============================================================================
#
# Un RingBuffer (buffer circular) es una estructura de tamaño fijo
# que sobrescribe los datos más antiguos cuando se llena.

IO.puts("=== Sección 6: RingBuffer con Enumerable + Collectable ===\n")

defmodule RingBuffer do
  defstruct [:capacity, :data, :size]

  def new(capacity) when capacity > 0 do
    %__MODULE__{capacity: capacity, data: :queue.new(), size: 0}
  end

  def push(%RingBuffer{data: data, size: size, capacity: cap} = rb, item)
      when size < cap do
    %{rb | data: :queue.in(item, data), size: size + 1}
  end
  def push(%RingBuffer{data: data, capacity: cap} = rb, item) do
    # Lleno: eliminar el más viejo
    {_, new_data} = :queue.out(data)
    %{rb | data: :queue.in(item, new_data)}
  end

  def to_list(%RingBuffer{data: data}), do: :queue.to_list(data)
end

defimpl Enumerable, for: RingBuffer do
  def reduce(rb, acc, fun) do
    Enumerable.reduce(RingBuffer.to_list(rb), acc, fun)
  end

  def count(%RingBuffer{size: size}), do: {:ok, size}

  def member?(%RingBuffer{} = rb, element) do
    {:ok, Enum.member?(RingBuffer.to_list(rb), element)}
  end

  def slice(_), do: {:error, __MODULE__}
end

defimpl Collectable, for: RingBuffer do
  def into(%RingBuffer{} = initial) do
    collector = fn
      rb, {:cont, item} -> RingBuffer.push(rb, item)
      rb, :done         -> rb
      _rb, :halt        -> :ok
    end
    {initial, collector}
  end
end

# Probar RingBuffer capacity 5
rb = RingBuffer.new(5)
rb = Enum.reduce(1..8, rb, &RingBuffer.push(&2, &1))

IO.puts("RingBuffer(5) con 8 elementos insertados:")
IO.puts("  Contenido: #{inspect(Enum.to_list(rb))}")
IO.puts("  Size: #{Enum.count(rb)}")
IO.puts("  Sum: #{Enum.sum(rb)}")

# Usar Collectable
rb2 = Enum.into(1..3, RingBuffer.new(5))
IO.puts("  Desde into: #{inspect(Enum.to_list(rb2))}\n")

# =============================================================================
# SECCIÓN 7: Slice — para acceso aleatorio eficiente
# =============================================================================
#
# Implementar slice/1 retornando {:ok, size, fun} habilita acceso aleatorio.
# La función fun.(start, length) retorna una porción de los elementos.
# Tipos que NO tienen acceso aleatorio eficiente deben retornar {:error, Mod}.

IO.puts("=== Sección 7: Slice ===\n")

# Ejemplo con una struct que SÍ puede implementar slice eficientemente
defmodule IndexedArray do
  defstruct [:data]

  def new(list), do: %__MODULE__{data: List.to_tuple(list)}
  def size(%__MODULE__{data: data}), do: tuple_size(data)
  def at(%__MODULE__{data: data}, i), do: elem(data, i)
end

defimpl Enumerable, for: IndexedArray do
  def reduce(%IndexedArray{data: data}, acc, fun) do
    size = tuple_size(data)
    reduce_tuple(data, 0, size, acc, fun)
  end

  defp reduce_tuple(_, i, size, {:cont, acc}, _fun) when i >= size, do: {:done, acc}
  defp reduce_tuple(_, _, _, {:halt, acc}, _fun), do: {:halted, acc}
  defp reduce_tuple(data, i, size, {:cont, acc}, fun) do
    reduce_tuple(data, i + 1, size, fun.(elem(data, i), acc), fun)
  end
  defp reduce_tuple(data, i, size, {:suspend, acc}, fun) do
    {:suspended, acc, &reduce_tuple(data, i, size, &1, fun)}
  end

  def count(%IndexedArray{data: data}), do: {:ok, tuple_size(data)}

  def member?(%IndexedArray{data: data}, element) do
    {:ok, Enum.member?(Tuple.to_list(data), element)}
  end

  def slice(%IndexedArray{data: data}) do
    size = tuple_size(data)
    {:ok, size, fn start, len -> Enum.map(start..(start+len-1), &elem(data, &1)) end}
  end
end

arr = IndexedArray.new([10, 20, 30, 40, 50, 60, 70, 80, 90, 100])
IO.puts("IndexedArray — Enum.slice(2, 4): #{inspect(Enum.slice(arr, 2, 4))}")
IO.puts("IndexedArray — Enum.at(5): #{inspect(Enum.at(arr, 5))}")
IO.puts("IndexedArray — Enum.count: #{Enum.count(arr)}\n")

# =============================================================================
# SECCIÓN 8: for...into y comprensión
# =============================================================================

IO.puts("=== Sección 8: for...into con tipos custom ===\n")

# Con Collectable implementado, la Queue funciona en comprehensions
result_q = for x <- 1..10,
               rem(x, 2) == 0,  # filtro: solo pares
               into: Queue.new(),
               do: x * x        # transformación: cuadrado

IO.puts("for pares al cuadrado en Queue: #{inspect(Enum.to_list(result_q))}")

# Anidado
matrix_q = for row <- 1..3,
               col <- 1..3,
               into: Queue.new(),
               do: {row, col}

IO.puts("Pares (fila, col) en Queue: #{inspect(Enum.to_list(matrix_q))}")

# Map → Queue
data = %{a: 1, b: 2, c: 3}
kv_q = Enum.into(data, Queue.new())
IO.puts("Map a Queue: #{inspect(Enum.to_list(kv_q))}\n")

# =============================================================================
# SECCIÓN 9: TRY IT YOURSELF
# =============================================================================
#
# Implementa un tipo Bag (multiset) completamente compatible con Enum y Collectable.
#
# Un Bag es como un Set, pero permite duplicados.
# Internamente almacena cada elemento y su conteo.
#
# Estructura:
#   %Bag{counts: %{"apple" => 3, "banana" => 1, "cherry" => 2}}
#
# Funciones del módulo Bag:
#   Bag.new/0           → bag vacío
#   Bag.add/2           → agrega un elemento (incrementa conteo)
#   Bag.add/3           → agrega N copias de un elemento
#   Bag.remove/2        → elimina una copia (decrementa, elimina si llega a 0)
#   Bag.count/2         → cuántas veces aparece un elemento
#   Bag.total/1         → total de elementos (con repeticiones)
#   Bag.unique/1        → elementos únicos (sin repetición)
#
# Enumerable debe iterar los elementos CON repetición:
#   Bag con {apple: 2, banana: 1} → itera: apple, apple, banana
#   Enum.count retorna el total (con repeticiones)
#   Enum.member? busca si el elemento existe (al menos 1 vez)
#
# Collectable permite:
#   Enum.into(["a", "b", "a", "c", "b", "a"], Bag.new())
#   → %Bag{counts: %{"a" => 3, "b" => 2, "c" => 1}}
#   for x <- list, into: Bag.new(), do: x

IO.puts("=== SECCIÓN 9: Try It Yourself ===\n")
IO.puts("Implementa Bag (multiset) abajo:\n")

defmodule Bag do
  defstruct counts: %{}

  def new(), do: %__MODULE__{}

  # Tu implementación de add/2, add/3, remove/2, count/2, total/1, unique/1
end

# Implementa los protocolos aquí:
# defimpl Enumerable, for: Bag do ...
# defimpl Collectable, for: Bag do ...

# Tests de tu implementación
IO.puts("--- Tests de Bag ---")

bag = Bag.new()
      |> Bag.add("apple")
      |> Bag.add("apple")
      |> Bag.add("banana")
      |> Bag.add("apple")
      |> Bag.add("cherry", 2)

IO.puts("Count apple: #{Bag.count(bag, "apple")}")       # 3
IO.puts("Count cherry: #{Bag.count(bag, "cherry")}")     # 2
IO.puts("Total: #{Bag.total(bag)}")                      # 8
IO.puts("Unique: #{inspect(Bag.unique(bag) |> Enum.sort())}")

IO.puts("\nEnumerable:")
IO.puts("Enum.count: #{Enum.count(bag)}")                # 8 (con repeticiones)
IO.puts("Enum.member? apple: #{Enum.member?(bag, "apple")}")  # true
IO.puts("Enum.member? grape: #{Enum.member?(bag, "grape")}")  # false
sorted = Enum.sort(bag)
IO.puts("Sorted (con repeticiones): #{inspect(sorted)}")

IO.puts("\nCollectable:")
bag2 = Enum.into(["x", "y", "x", "z", "x"], Bag.new())
IO.puts("Count x: #{Bag.count(bag2, "x")}")              # 3
IO.puts("Count y: #{Bag.count(bag2, "y")}")              # 1

bag3 = for fruit <- ["mango", "mango", "papaya", "mango"], into: Bag.new(), do: fruit
IO.puts("Mango (for...into): #{Bag.count(bag3, "mango")}")  # 3

# =============================================================================
# ERRORES COMUNES
# =============================================================================
IO.puts("\n=== Errores Comunes ===\n")
IO.puts("""
1. No manejar {:halt, acc} en reduce:
   Enum.take, Enum.find, Enum.any? detienen la reducción con halt.
   Si no retornas {:halted, acc} inmediatamente, el reduce sigue adelante
   y nunca termina (bucle infinito o proceso completo innecesariamente).

2. No manejar {:suspend, acc} en reduce:
   Stream.* necesita poder pausar la enumeración.
   Sin {:suspended, acc, cont_fn}, los Streams no funcionan.
   La función de continuación es: &reduce(remaining, &1, fun)

3. Retornar {:error, __MODULE__} en count y member?:
   Esto es válido pero ineficiente — Elixir hará count/member? usando reduce.
   Si puedes retornar {:ok, n} y {:ok, bool}, es mucho más eficiente.

4. Collectable.into retorna mal:
   Debe retornar {initial_acc, collector_fn} — una TUPLA.
   El collector_fn recibe la tupla {acc, command}, no dos argumentos.

5. Estado mutable en Collectable:
   El patrón funcional requiere que collector retorne el nuevo estado.
   No uses variables de proceso (Agent, etc.) para el acumulador — pasa
   el estado como primer elemento de la tupla.
""")
