# 30 — Erlang Interop Avanzado

**Nivel**: Avanzado  
**Tema**: Usar módulos Erlang avanzados directamente desde Elixir

---

## Contexto

Elixir corre sobre la BEAM y tiene acceso directo a **toda la biblioteca estándar de Erlang**.
Muchas estructuras de datos y algoritmos de Erlang no tienen equivalente directo en Elixir o
en su biblioteca `Enum`/`Map`. Cuando el rendimiento importa o necesitas una semántica específica,
ir directamente a los módulos Erlang es la elección correcta.

La convención es usar átomos prefijados con `:` para acceder a módulos Erlang:

```elixir
:lists.sort([3, 1, 2])         # módulo Erlang lists
:maps.filter(fn _, v -> v > 0 end, %{a: 1, b: -1})
:queue.new()                   # double-ended queue
```

### Módulos clave y cuándo usarlos

| Módulo | Uso principal | Cuándo preferirlo |
|---|---|---|
| `:lists` | `keyfind`, `keysort`, `keyreplace`, `usort` | Búsqueda en keyword tuples, sort con dedup |
| `:maps` | `filter/2`, `fold/3`, `iterator/1`, `next/1` | Iteración lazy sobre maps grandes |
| `:queue` | Double-ended queue amortizado O(1) | Colas FIFO/LIFO en GenServer state |
| `:gb_trees` | Árbol balanceado ordenado | Priority queues, rango ordenado eficiente |
| `:gb_sets` | Set con orden | Membership + orden sin duplicados |
| `:ordsets` | Set como lista ordenada | Sets pequeños, union/intersection eficiente |
| `:proplists` | Property lists estilo Erlang | Interop con código Erlang existente |

### Conversión de tipos Erlang ↔ Elixir

La mayoría de tipos son compatibles directamente. Los casos que requieren conversión explícita:

```elixir
# Charlists (listas de codepoints) vs binaries
'hello'             # charlist Erlang  → lista de integers [104, 101, 108, 108, 111]
"hello"             # binary Elixir   → <<104, 101, 108, 108, 111>>

to_charlist("hello")    # binary → charlist
to_string('hello')      # charlist → binary (String)
List.to_atom('hello')   # charlist → atom

# Records de Erlang (no existen en Elixir nativo)
# Se acceden como tuples donde el primer elemento es el nombre del record
{:person, "Alice", 30}   # record Erlang #person{name="Alice", age=30}
```

### `:lists` — funciones no en Enum

```elixir
# keyfind: busca en lista de tuples por posición N (1-indexed en Erlang)
:lists.keyfind(:alice, 1, [{:alice, 30}, {:bob, 25}])
#=> {:alice, 30}

# keysort: sort por clave en posición N
:lists.keysort(2, [{:bob, 25}, {:alice, 30}])
#=> [{:bob, 25}, {:alice, 30}]

# usort: sort + dedup en una pasada (más eficiente que sort + uniq)
:lists.usort([3, 1, 2, 1, 3])
#=> [1, 2, 3]

# keyreplace: reemplaza el primer tuple donde key en posición N coincide
:lists.keyreplace(:alice, 1, [{:alice, 30}, {:bob, 25}], {:alice, 31})
#=> [{:alice, 31}, {:bob, 25}]
```

### `:queue` — semántica y performance

`:queue` implementa una cola funcional con O(1) amortizado en enqueue y dequeue
usando dos listas internas (in/out). Es inmutable y thread-safe como toda estructura
de datos en Elixir/Erlang.

```elixir
q = :queue.new()
q = :queue.in(:a, q)    # enqueue al final
q = :queue.in(:b, q)
q = :queue.in(:c, q)

:queue.out(q)           #=> {{:value, :a}, rest_queue}
:queue.out_r(q)         #=> {{:value, :c}, rest_queue}  # dequeue desde el final
:queue.len(q)           #=> 3
:queue.is_empty(q)      #=> false
```

### `:gb_trees` — árboles balanceados

```elixir
t = :gb_trees.empty()
t = :gb_trees.insert(3, :c, t)
t = :gb_trees.insert(1, :a, t)
t = :gb_trees.insert(2, :b, t)

:gb_trees.smallest(t)   #=> {1, :a}
:gb_trees.largest(t)    #=> {3, :c}
:gb_trees.get(2, t)     #=> :b
:gb_trees.to_list(t)    #=> [{1, :a}, {2, :b}, {3, :c}]
```

---

## Ejercicio 1 — Cola con `:queue` en GenServer

Un `JobQueue` GenServer actualmente usa una lista para encolar trabajos.
Cada `enqueue` hace `state ++ [job]` — O(n). Reemplazalo con `:queue`
para obtener O(1) amortizado en ambas operaciones.

### Requisitos

- El GenServer expone: `enqueue/2`, `dequeue/1`, `peek/1`, `size/1`, `drain/1`
- `drain/1` retorna todos los elementos en orden FIFO y deja la cola vacía
- El estado interno debe ser un `:queue`, no una lista
- `dequeue/1` cuando la cola está vacía retorna `{:error, :empty}`
- `peek/1` retorna `{:ok, item}` o `{:error, :empty}` sin modificar la cola

### Uso esperado

```elixir
{:ok, pid} = JobQueue.start_link()

JobQueue.enqueue(pid, {:job, 1})
JobQueue.enqueue(pid, {:job, 2})
JobQueue.enqueue(pid, {:job, 3})

JobQueue.peek(pid)     #=> {:ok, {:job, 1}}
JobQueue.dequeue(pid)  #=> {:ok, {:job, 1}}
JobQueue.size(pid)     #=> 2

JobQueue.drain(pid)    #=> [{:job, 2}, {:job, 3}]
JobQueue.size(pid)     #=> 0
```

### Hints

<details>
<summary>Hint 1 — Estado inicial y enqueue</summary>

```elixir
defmodule JobQueue do
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, :ok, opts)

  def init(:ok), do: {:ok, :queue.new()}

  def handle_call({:enqueue, job}, _from, queue) do
    {:reply, :ok, :queue.in(job, queue)}
  end
end
```

`:queue.in/2` agrega al final de la cola. Para agregar al frente usa `:queue.in_r/2`.
</details>

<details>
<summary>Hint 2 — Dequeue y peek con pattern matching</summary>

```elixir
def handle_call(:dequeue, _from, queue) do
  case :queue.out(queue) do
    {{:value, item}, new_queue} -> {:reply, {:ok, item}, new_queue}
    {:empty, _} -> {:reply, {:error, :empty}, queue}
  end
end

def handle_call(:peek, _from, queue) do
  case :queue.peek(queue) do
    {:value, item} -> {:reply, {:ok, item}, queue}
    :empty -> {:reply, {:error, :empty}, queue}
  end
end
```
</details>

<details>
<summary>Hint 3 — drain con to_list</summary>

```elixir
def handle_call(:drain, _from, queue) do
  items = :queue.to_list(queue)
  {:reply, items, :queue.new()}
end
```

`:queue.to_list/1` preserva el orden FIFO. Es O(n) pero inevitable para retornar todos los elementos.
</details>

---

## Ejercicio 2 — Priority Queue con `:gb_trees`

Implementa un módulo `PriorityQueue` (no GenServer, módulo funcional puro) que use
`:gb_trees` como backing store. Las claves son prioridades numéricas (menor = mayor prioridad).

### Requisitos

- `new/0` — crea una priority queue vacía
- `insert/3` — `insert(pq, priority, value)` — inserta con prioridad dada
- `peek_min/1` — retorna `{:ok, {priority, value}}` o `{:error, :empty}` sin extraer
- `pop_min/1` — retorna `{{:ok, {priority, value}}, new_pq}` o `{{:error, :empty}, pq}`
- `size/1` — número de elementos
- `to_sorted_list/1` — todos los elementos en orden de prioridad ascendente

### Manejo de prioridades duplicadas

`:gb_trees` no permite claves duplicadas (insert/3 lanza si la clave existe).
Usa `update/3` cuando la clave existe, o modela la clave como `{priority, sequence_number}`
para permitir múltiples items con la misma prioridad.

### Uso esperado

```elixir
pq = PriorityQueue.new()
pq = PriorityQueue.insert(pq, 3, :low_priority_task)
pq = PriorityQueue.insert(pq, 1, :urgent_task)
pq = PriorityQueue.insert(pq, 2, :normal_task)

PriorityQueue.peek_min(pq)    #=> {:ok, {1, :urgent_task}}

{{:ok, {1, :urgent_task}}, pq} = PriorityQueue.pop_min(pq)
PriorityQueue.to_sorted_list(pq)
#=> [{2, :normal_task}, {3, :low_priority_task}]
```

### Hints

<details>
<summary>Hint 1 — Estructura con contador para duplicados</summary>

```elixir
defmodule PriorityQueue do
  # State: {tree, counter}
  # key en el tree: {priority, seq} para permitir duplicados con misma prioridad

  def new(), do: {:gb_trees.empty(), 0}

  def insert({tree, seq}, priority, value) do
    new_tree = :gb_trees.insert({priority, seq}, value, tree)
    {new_tree, seq + 1}
  end
end
```

La clave compuesta `{priority, seq}` garantiza unicidad. El ordering natural de
tuples en Erlang/Elixir ordena primero por priority, luego por seq — exactamente lo que queremos.
</details>

<details>
<summary>Hint 2 — peek_min y pop_min</summary>

```elixir
def peek_min({tree, _seq}) do
  case :gb_trees.is_empty(tree) do
    true -> {:error, :empty}
    false ->
      {{priority, _seq}, value} = :gb_trees.smallest(tree)
      {:ok, {priority, value}}
  end
end

def pop_min({tree, seq}) do
  case :gb_trees.is_empty(tree) do
    true -> {{:error, :empty}, {tree, seq}}
    false ->
      {{priority, _}, value, new_tree} = :gb_trees.take_smallest(tree)
      {{:ok, {priority, value}}, {new_tree, seq}}
  end
end
```
</details>

<details>
<summary>Hint 3 — to_sorted_list</summary>

```elixir
def to_sorted_list({tree, _seq}) do
  # gb_trees.to_list retorna [{key, value}] en orden ascendente por key
  tree
  |> :gb_trees.to_list()
  |> Enum.map(fn {{priority, _seq}, value} -> {priority, value} end)
end
```
</details>

---

## Ejercicio 3 — Convertidor Erlang ↔ Elixir

Una librería Erlang legacy retorna datos en formato nativo Erlang: charlists,
records (tuples), y proplists. Escribe un módulo `ErlangConverter` que limpie
estos datos y los convierta a estructuras idiomáticas de Elixir.

### Formato de entrada (datos Erlang legacy)

```erlang
%% Record Erlang: #user{name="Alice", age=30, tags=["admin", "user"]}
{user, 'Alice', 30, ["admin", "user"]}

%% Proplist Erlang: [{key, value}, ...]
[{name, 'Alice'}, {age, 30}, {active, true}]

%% Lista de charlists
["hello", "world"]   %% en Erlang, pero en Elixir: ['hello', 'world']
```

### Requisitos

- `record_to_map/2` — convierte un record tuple a map dado el esquema de campos
- `proplist_to_map/1` — convierte proplist Erlang a map Elixir (keys como atoms, strings como strings)
- `deep_charlist_to_string/1` — convierte recursivamente charlists a strings en cualquier estructura
- `normalize_atom/1` — convierte atoms Erlang (pueden ser charlists en algunos contextos) a strings legibles

### Uso esperado

```elixir
record = {:user, 'Alice', 30, ['admin', 'user']}
ErlangConverter.record_to_map(record, [:name, :age, :tags])
#=> %{name: "Alice", age: 30, tags: ["admin", "user"]}

proplist = [{:name, 'Bob'}, {:age, 25}, {:active, true}]
ErlangConverter.proplist_to_map(proplist)
#=> %{name: "Bob", age: 25, active: true}

ErlangConverter.deep_charlist_to_string(['hello', ['world', '!']])
#=> ["hello", ["world", "!"]]
```

### Hints

<details>
<summary>Hint 1 — record_to_map: skip el primer elemento (nombre del record)</summary>

```elixir
def record_to_map(record, fields) when is_tuple(record) do
  # El primer elemento es el nombre del record, lo saltamos con tl
  values = record |> Tuple.to_list() |> tl()

  fields
  |> Enum.zip(values)
  |> Enum.into(%{})
  |> Map.new(fn {k, v} -> {k, deep_charlist_to_string(v)} end)
end
```
</details>

<details>
<summary>Hint 2 — proplist con :proplists</summary>

```elixir
def proplist_to_map(proplist) do
  # :proplists.get_keys retorna lista de claves únicas
  # :proplists.get_value busca un valor por clave
  keys = :proplists.get_keys(proplist)

  Map.new(keys, fn key ->
    value = :proplists.get_value(key, proplist)
    {key, deep_charlist_to_string(value)}
  end)
end
```
</details>

<details>
<summary>Hint 3 — deep_charlist_to_string con pattern matching</summary>

```elixir
def deep_charlist_to_string(value) when is_list(value) do
  case :io_lib.deep_char_list(value) do
    true  -> List.to_string(value)         # es una charlist → convertir
    false -> Enum.map(value, &deep_charlist_to_string/1)  # es una lista normal
  end
end
def deep_charlist_to_string(value) when is_tuple(value) do
  value |> Tuple.to_list() |> Enum.map(&deep_charlist_to_string/1) |> List.to_tuple()
end
def deep_charlist_to_string(value), do: value
```

`:io_lib.deep_char_list/1` es una función Erlang que detecta si una lista
es una charlist (todos los elementos son codepoints válidos).
</details>

---

## Trade-offs a considerar

### `:queue` vs lista para colas

| | Lista | `:queue` |
|---|---|---|
| Enqueue al final | O(n) — `list ++ [item]` | O(1) amortizado |
| Dequeue del frente | O(1) — pattern match | O(1) amortizado |
| Peek | O(1) — `hd(list)` | O(1) |
| Memoria | Menor overhead | Dos listas internas |

Usa `:queue` cuando el enqueue al final es frecuente. Para stacks (LIFO), una lista es perfecta.

### `:gb_trees` vs `Map` para datos ordenados

`:gb_trees` mantiene orden en el árbol — `smallest/1`, `largest/1`, y `to_list/1` son O(log n)
o O(n) respectivamente y siempre en orden. Un `Map` no tiene orden garantizado.
Paga el costo de O(log n) en insert/delete (vs O(1) amortizado en Map) a cambio de
operaciones ordenadas eficientes.

### Charlists — cuándo no convertir

Si tu código Elixir llama a funciones Erlang que esperan charlists (`:file.read_file/1`,
`:io.format/2`), pasar strings binarios funciona en muchos casos, pero no siempre.
Cuando trabajas en la frontera Erlang/Elixir de forma intensiva, considera mantener
charlists dentro del módulo de interop y convertir sólo en la API pública.

---

## One possible solution

<details>
<summary>Ver solución (spoiler)</summary>

```elixir
# Ejercicio 1: JobQueue
defmodule JobQueue do
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, :ok, opts)
  def enqueue(pid, job),  do: GenServer.call(pid, {:enqueue, job})
  def dequeue(pid),       do: GenServer.call(pid, :dequeue)
  def peek(pid),          do: GenServer.call(pid, :peek)
  def size(pid),          do: GenServer.call(pid, :size)
  def drain(pid),         do: GenServer.call(pid, :drain)

  @impl true
  def init(:ok), do: {:ok, :queue.new()}

  @impl true
  def handle_call({:enqueue, job}, _from, queue),
    do: {:reply, :ok, :queue.in(job, queue)}

  def handle_call(:dequeue, _from, queue) do
    case :queue.out(queue) do
      {{:value, item}, rest} -> {:reply, {:ok, item}, rest}
      {:empty, _}            -> {:reply, {:error, :empty}, queue}
    end
  end

  def handle_call(:peek, _from, queue) do
    case :queue.peek(queue) do
      {:value, item} -> {:reply, {:ok, item}, queue}
      :empty         -> {:reply, {:error, :empty}, queue}
    end
  end

  def handle_call(:size, _from, queue),
    do: {:reply, :queue.len(queue), queue}

  def handle_call(:drain, _from, queue),
    do: {:reply, :queue.to_list(queue), :queue.new()}
end

# Ejercicio 2: PriorityQueue
defmodule PriorityQueue do
  def new(), do: {:gb_trees.empty(), 0}

  def insert({tree, seq}, priority, value) do
    {:gb_trees.insert({priority, seq}, value, tree), seq + 1}
  end

  def peek_min({tree, _}) do
    if :gb_trees.is_empty(tree) do
      {:error, :empty}
    else
      {{p, _}, v} = :gb_trees.smallest(tree)
      {:ok, {p, v}}
    end
  end

  def pop_min({tree, seq}) do
    if :gb_trees.is_empty(tree) do
      {{:error, :empty}, {tree, seq}}
    else
      {{p, _}, v, t2} = :gb_trees.take_smallest(tree)
      {{:ok, {p, v}}, {t2, seq}}
    end
  end

  def size({tree, _}), do: :gb_trees.size(tree)

  def to_sorted_list({tree, _}) do
    tree
    |> :gb_trees.to_list()
    |> Enum.map(fn {{p, _}, v} -> {p, v} end)
  end
end

# Ejercicio 3: ErlangConverter
defmodule ErlangConverter do
  def record_to_map(record, fields) when is_tuple(record) do
    values = record |> Tuple.to_list() |> tl()
    fields
    |> Enum.zip(values)
    |> Map.new(fn {k, v} -> {k, deep_charlist_to_string(v)} end)
  end

  def proplist_to_map(proplist) do
    proplist
    |> :proplists.get_keys()
    |> Map.new(fn k ->
      {k, deep_charlist_to_string(:proplists.get_value(k, proplist))}
    end)
  end

  def deep_charlist_to_string(v) when is_list(v) do
    if :io_lib.deep_char_list(v) do
      List.to_string(v)
    else
      Enum.map(v, &deep_charlist_to_string/1)
    end
  end
  def deep_charlist_to_string(v) when is_tuple(v) do
    v |> Tuple.to_list() |> Enum.map(&deep_charlist_to_string/1) |> List.to_tuple()
  end
  def deep_charlist_to_string(v), do: v
end
```

</details>
