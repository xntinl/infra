# 19 — Cache Patterns con ETS

## Prerequisites

- ETS avanzado con opciones de concurrencia (ejercicio 16)
- GenServer con `handle_info` y `Process.send_after`
- Comprensión de race conditions y concurrencia en la BEAM
- Pattern matching avanzado y estructuras de datos

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Implementar TTL cache con cleanup automático y sin memory leaks
2. Prevenir cache stampede con singleton fetch usando `:global` o registros locales
3. Diseñar una LRU cache con evicción basada en access tracking
4. Elegir entre write-through, write-behind y read-through según el caso de uso
5. Implementar cache invalidation sin race conditions
6. Razonar sobre los trade-offs de consistencia vs performance en cada patrón

---

## Concepts

### 1. Read-through, Write-through y Write-behind

Estos tres patrones definen cómo el cache se relaciona con el almacén de datos subyacente:

```
Read-through:
  get(key) → cache miss → fetch from source → populate cache → return

Write-through:
  put(key, value) → write to cache AND source synchronously → return

Write-behind (write-back):
  put(key, value) → write to cache immediately → schedule async write to source
```

**Read-through** es el más común. Mantiene el cache "lazy": solo se populan las claves que se leen. El riesgo es el cache stampede (ver sección 3).

**Write-through** garantiza consistencia: el cache y el source siempre están sincronizados. Es más lento en escrituras pero elimina la ventana de inconsistencia.

**Write-behind** maximiza write throughput: la escritura al source ocurre en background. El riesgo es perder datos si el proceso cae antes del flush. Útil para métricas, analytics, y datos donde la pérdida ocasional es aceptable.

### 2. TTL con timestamp en el valor

La forma más eficiente de implementar TTL en ETS es almacenar el tiempo de expiración como parte del valor:

```elixir
# Formato: {key, value, expires_at_monotonic}
# expires_at en monotonic time (System.monotonic_time) evita problemas con NTP

def put(table, key, value, ttl_ms) do
  expires_at = System.monotonic_time(:millisecond) + ttl_ms
  :ets.insert(table, {key, value, expires_at})
end

def get(table, key) do
  now = System.monotonic_time(:millisecond)
  case :ets.lookup(table, key) do
    [{^key, value, expires_at}] when expires_at > now ->
      {:ok, value}
    [{^key, _value, _expired}] ->
      # Lazy eviction: eliminar al acceder
      :ets.delete(table, key)
      :miss
    [] ->
      :miss
  end
end
```

**System.monotonic_time vs System.os_time**: el tiempo monotónico solo avanza hacia adelante y no se ve afectado por ajustes de NTP/clock skew. Ideal para TTL. `os_time` puede retroceder (DST, NTP sync) lo que causaría expiración prematura o tardía.

### 3. Cache stampede y cómo prevenirlo

Un cache stampede ocurre cuando múltiples procesos concurrentes detectan el mismo cache miss y lanzan la misma query costosa simultáneamente:

```
t=0: 100 procesos piden key "popular_item"
t=0: key está expirada → 100 cache misses simultáneos
t=0: 100 procesos lanzan query a DB
t=0: DB recibe 100 queries idénticas → saturación
t=1: 100 resultados llegan → 99 se descartan, 1 popula el cache
```

La solución es el patrón **singleton fetch**: solo un proceso hace el fetch, los demás esperan:

```elixir
# Patrón con :global (funciona en cluster):
def get_or_fetch(key, fetch_fn, ttl_ms) do
  case cache_get(key) do
    {:ok, value} -> {:ok, value}
    :miss ->
      # Solo un proceso ejecuta fetch_fn para esta key
      case :global.trans({:cache_fetch, key}, fn ->
        # Double-check dentro del lock (otra instancia pudo haberlo populado)
        case cache_get(key) do
          {:ok, value} -> value
          :miss ->
            value = fetch_fn.()
            cache_put(key, value, ttl_ms)
            value
        end
      end) do
        value when not is_nil(value) -> {:ok, value}
        _aborted -> {:error, :fetch_failed}
      end
  end
end
```

Alternativa más liviana con `:ets.insert_new/2` (solo un proceso puede insertar una centinela):

```elixir
# Usar ETS como mutex lightweight
sentinel_key = {:fetching, key}

if :ets.insert_new(lock_table, {sentinel_key, self()}) do
  # Este proceso gana — hace el fetch
  try do
    value = fetch_fn.()
    cache_put(key, value, ttl_ms)
    :ets.delete(lock_table, sentinel_key)
    {:ok, value}
  rescue
    e ->
      :ets.delete(lock_table, sentinel_key)
      reraise e, __STACKTRACE__
  end
else
  # Otro proceso está fetching — esperar con backoff
  wait_for_cache(key, 10, fetch_fn, ttl_ms)
end
```

### 4. LRU (Least Recently Used) eviction

Una LRU cache tiene tamaño fijo y desaloja el elemento menos recientemente accedido cuando está llena. En ETS, se implementa con una tabla de access tracking:

```elixir
# Tabla de datos: {key, value, expires_at}
# Tabla de accesos: {last_access_time, key}

# En cada get: actualizar el tiempo de último acceso
def touch(access_table, key) do
  now = System.monotonic_time(:microsecond)
  # unique_integer garantiza unicidad si dos accesos ocurren en el mismo microsegundo
  :ets.insert(access_table, {:erlang.unique_integer([:monotonic]), key, now})
end

# Evicción: eliminar las K entradas con access_time más antiguo
def evict(cache_table, access_table, count) do
  # ordered_set en access_table permite tomar los K primeros (más viejos)
  oldest = take_oldest(access_table, count)
  Enum.each(oldest, fn {time_key, cache_key, _ts} ->
    :ets.delete(cache_table, cache_key)
    :ets.delete(access_table, time_key)
  end)
end
```

### 5. Cleanup automático con scheduler

Para TTL, el cleanup lazy (en get) evita memory leaks pero solo si esas claves se vuelven a acceder. Para claves que expiran y nunca se vuelven a leer, necesitas un cleanup periódico:

```elixir
defmodule CacheScheduler do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  def init(%{table: table, interval_ms: interval}) do
    schedule_cleanup(interval)
    {:ok, %{table: table, interval: interval}}
  end

  def handle_info(:cleanup, state) do
    now = System.monotonic_time(:millisecond)
    ms = :ets.fun2ms(fn {_k, _v, expires_at} when expires_at =< now -> true end)
    deleted = :ets.select_delete(state.table, ms)

    if deleted > 0, do: IO.puts("Cache cleanup: #{deleted} expired entries removed")

    schedule_cleanup(state.interval)
    {:noreply, state}
  end

  defp schedule_cleanup(interval), do: Process.send_after(self(), :cleanup, interval)
end
```

---

## Exercises

### Exercise 1 — TTL cache con cleanup automático

**Problem**

Implementa un `TTLCache` completo con:
1. `get/1` con lazy eviction de claves expiradas
2. `put/3` con TTL configurable por entrada
3. Proceso de cleanup periódico que elimina entradas expiradas aunque no se accedan
4. `stats/0` que retorna `{total: N, expired: N, fresh: N}` sin eliminar nada
5. `get_or_put/3` — retorna el valor si existe y no expiró, sino llama `fetch_fn` y lo guarda

**Hints**

- El scheduler de cleanup debe ser un proceso separado supervisado junto al GenServer propietario
- Usa `System.monotonic_time(:millisecond)` para timestamps de TTL
- `stats/0` puede usar `select_count` con dos match specs distintas
- `get_or_put` es una operación de solo lectura fuera del GenServer (lee directo de ETS)

**One possible solution**

```elixir
defmodule TTLCache do
  use GenServer

  @table :ttl_cache
  @cleanup_interval 30_000  # 30 segundos

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # API — lecturas son directas a ETS, escrituras van por GenServer
  def get(key) do
    now = System.monotonic_time(:millisecond)
    case :ets.lookup(@table, key) do
      [{^key, value, expires_at}] when expires_at > now ->
        {:ok, value}
      [{^key, _value, _expired}] ->
        :ets.delete(@table, key)
        :miss
      [] ->
        :miss
    end
  end

  def put(key, value, ttl_ms \\ 60_000) do
    expires_at = System.monotonic_time(:millisecond) + ttl_ms
    :ets.insert(@table, {key, value, expires_at})
    :ok
  end

  def get_or_put(key, fetch_fn, ttl_ms \\ 60_000) do
    case get(key) do
      {:ok, value} ->
        {:ok, value, :hit}
      :miss ->
        value = fetch_fn.()
        put(key, value, ttl_ms)
        {:ok, value, :miss}
    end
  end

  def delete(key), do: :ets.delete(@table, key)

  def stats do
    now = System.monotonic_time(:millisecond)

    total = :ets.info(@table, :size)

    expired_ms = :ets.fun2ms(fn {_k, _v, expires_at} when expires_at =< now -> true end)
    expired = :ets.select_count(@table, expired_ms)

    %{total: total, expired: expired, fresh: total - expired}
  end

  def flush, do: :ets.delete_all_objects(@table)

  # GenServer callbacks
  def init(opts) do
    table = :ets.new(@table, [
      :set,
      :public,
      :named_table,
      {:read_concurrency, true},
      {:write_concurrency, true}
    ])

    interval = Keyword.get(opts, :cleanup_interval, @cleanup_interval)
    schedule_cleanup(interval)

    {:ok, %{table: table, interval: interval}}
  end

  def handle_info(:cleanup, state) do
    now = System.monotonic_time(:millisecond)
    ms = :ets.fun2ms(fn {_k, _v, expires_at} when expires_at =< now -> true end)
    deleted = :ets.select_delete(@table, ms)

    if deleted > 0 do
      IO.puts("[TTLCache] Cleanup: removed #{deleted} expired entries")
    end

    schedule_cleanup(state.interval)
    {:noreply, state}
  end

  defp schedule_cleanup(interval) do
    Process.send_after(self(), :cleanup, interval)
  end
end

# Demo
{:ok, _} = TTLCache.start_link(cleanup_interval: 2_000)

TTLCache.put("hot_key", "value_a", 5_000)    # expira en 5s
TTLCache.put("cold_key", "value_b", 500)     # expira en 500ms

IO.inspect(TTLCache.get("hot_key"), label: "immediate get")       # {:ok, "value_a"}
IO.inspect(TTLCache.get("cold_key"), label: "immediate get cold") # {:ok, "value_b"}

Process.sleep(600)  # esperar a que cold_key expire

IO.inspect(TTLCache.get("cold_key"), label: "after expiry")       # :miss
IO.inspect(TTLCache.stats(), label: "stats")

{:ok, val, source} = TTLCache.get_or_put("new_key", fn ->
  IO.puts("Fetching from source...")
  "fetched_value"
end, 10_000)

IO.puts("Value: #{val}, Source: #{source}")  # Source: miss

{:ok, val2, source2} = TTLCache.get_or_put("new_key", fn -> "never_called" end, 10_000)
IO.puts("Value: #{val2}, Source: #{source2}")  # Source: hit

GenServer.stop(TTLCache)
```

---

### Exercise 2 — Cache stampede prevention

**Problem**

Implementa `StampedeCache` que garantiza que una función de fetch costosa se ejecute **como máximo una vez** para cada clave, incluso si 100 procesos hacen miss concurrentemente.

Requisitos:
1. Usa `ETS insert_new` como mecanismo de lock (sin librerías externas)
2. El proceso que gana el lock hace el fetch; los demás esperan con poll + backoff
3. Si el proceso ganador falla, libera el lock y propaga el error
4. Timeout configurable para no bloquear indefinidamente
5. Métricas: contar fetch_executions y wait_events

**Hints**

- La tabla de locks puede ser una segunda tabla ETS separada de la de datos
- El poll puede usar `Process.sleep(5)` + reintentar hasta el timeout
- Usar `try/rescue` en el ganador para garantizar liberación del lock en caso de error
- El lock ETS debe tener una clave diferente a la clave de datos para no colisionar

**One possible solution**

```elixir
defmodule StampedeCache do
  use GenServer

  @cache_table :stampede_cache
  @lock_table :stampede_locks
  @default_ttl 60_000
  @lock_timeout 10_000
  @poll_interval 5

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def get_or_fetch(key, fetch_fn, ttl_ms \\ @default_ttl, timeout \\ @lock_timeout) do
    case direct_get(key) do
      {:ok, value} ->
        {:ok, value}
      :miss ->
        with_singleton_fetch(key, fetch_fn, ttl_ms, timeout)
    end
  end

  def stats do
    GenServer.call(__MODULE__, :stats)
  end

  # Lectura directa sin pasar por GenServer
  defp direct_get(key) do
    now = System.monotonic_time(:millisecond)
    case :ets.lookup(@cache_table, key) do
      [{^key, value, expires_at}] when expires_at > now -> {:ok, value}
      [{^key, _, _}] ->
        :ets.delete(@cache_table, key)
        :miss
      [] -> :miss
    end
  end

  defp with_singleton_fetch(key, fetch_fn, ttl_ms, timeout) do
    lock_key = {:lock, key}

    if :ets.insert_new(@lock_table, {lock_key, self(), System.monotonic_time(:millisecond)}) do
      # Ganamos el lock — somos responsables del fetch
      GenServer.cast(__MODULE__, :increment_fetch)

      try do
        value = fetch_fn.()
        expires_at = System.monotonic_time(:millisecond) + ttl_ms
        :ets.insert(@cache_table, {key, value, expires_at})
        {:ok, value}
      rescue
        e ->
          :ets.delete(@lock_table, lock_key)
          reraise e, __STACKTRACE__
      after
        :ets.delete(@lock_table, lock_key)
      end
    else
      # Perdimos el lock — esperar a que el ganador popule el cache
      GenServer.cast(__MODULE__, :increment_wait)
      wait_for_key(key, timeout)
    end
  end

  defp wait_for_key(key, timeout) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_wait(key, deadline)
  end

  defp do_wait(key, deadline) do
    if System.monotonic_time(:millisecond) >= deadline do
      {:error, :timeout}
    else
      case direct_get(key) do
        {:ok, value} -> {:ok, value}
        :miss ->
          Process.sleep(@poll_interval)
          do_wait(key, deadline)
      end
    end
  end

  # GenServer callbacks
  def init(_opts) do
    :ets.new(@cache_table, [:set, :public, :named_table, {:read_concurrency, true}, {:write_concurrency, true}])
    :ets.new(@lock_table, [:set, :public, :named_table, {:write_concurrency, true}])

    {:ok, %{fetch_count: 0, wait_count: 0}}
  end

  def handle_cast(:increment_fetch, state), do: {:noreply, %{state | fetch_count: state.fetch_count + 1}}
  def handle_cast(:increment_wait, state), do: {:noreply, %{state | wait_count: state.wait_count + 1}}
  def handle_call(:stats, _from, state), do: {:reply, state, state}
end

# Demo: 50 procesos concurrentes, misma clave
{:ok, _} = StampedeCache.start_link()

fetch_count_actual = :counters.new(1, [:atomics])

results =
  1..50
  |> Task.async_stream(fn _i ->
    StampedeCache.get_or_fetch("expensive_key", fn ->
      :counters.add(fetch_count_actual, 1, 1)
      Process.sleep(50)  # simula fetch costoso
      "computed_value"
    end, 30_000)
  end, max_concurrency: 50, timeout: 15_000)
  |> Enum.map(fn {:ok, r} -> r end)

ok_count = Enum.count(results, &match?({:ok, _}, &1))
actual_fetches = :counters.get(fetch_count_actual, 1)

IO.puts("50 concurrent requests → #{ok_count} succeeded")
IO.puts("Actual fetch executions: #{actual_fetches}")  # debe ser 1, no 50
IO.inspect(StampedeCache.stats(), label: "cache stats")
# fetch_count: 1, wait_count: 49

GenServer.stop(StampedeCache)
```

---

### Exercise 3 — LRU eviction cache

**Problem**

Implementa una LRU cache con capacidad máxima N. Cuando se supera la capacidad, desaloja el elemento menos recientemente usado.

Diseño:
- Tabla de datos ETS `:set`: `{key, value}`
- Tabla de accesos ETS `:ordered_set`: `{monotonic_time, key}` — permite tomar los más antiguos eficientemente
- Tabla de índice inverso: `{key, last_access_time}` — para actualizar el access timestamp

**Hints**

- Al hacer `get`, actualizar el access timestamp requiere: eliminar el timestamp antiguo, insertar el nuevo
- `System.monotonic_time(:nanosecond)` reduce colisiones en el ordered_set
- La evicción puede ser lazy (al insertar) o proactiva (al superar max - buffer)
- Considera usar un GenServer para serializar writes y evitar race conditions en la evicción

**One possible solution**

```elixir
defmodule LRUCache do
  use GenServer

  @data_table :lru_data
  @access_table :lru_access  # ordered_set: {time, key}
  @index_table :lru_index    # set: {key, time}

  def start_link(opts) do
    max_size = Keyword.fetch!(opts, :max_size)
    GenServer.start_link(__MODULE__, max_size, name: __MODULE__)
  end

  def get(key) do
    case :ets.lookup(@data_table, key) do
      [{^key, value}] ->
        # Actualizar access time — write ligero, puede ir por GenServer
        GenServer.cast(__MODULE__, {:touch, key})
        {:ok, value}
      [] ->
        :miss
    end
  end

  def put(key, value) do
    GenServer.call(__MODULE__, {:put, key, value})
  end

  def delete(key) do
    GenServer.call(__MODULE__, {:delete, key})
  end

  def size, do: :ets.info(@data_table, :size)

  # Callbacks
  def init(max_size) do
    :ets.new(@data_table, [:set, :public, :named_table, {:read_concurrency, true}])
    :ets.new(@access_table, [:ordered_set, :protected, :named_table])
    :ets.new(@index_table, [:set, :protected, :named_table])

    {:ok, %{max_size: max_size}}
  end

  def handle_cast({:touch, key}, state) do
    update_access_time(key)
    {:noreply, state}
  end

  def handle_call({:put, key, value}, _from, state) do
    existing = :ets.lookup(@data_table, key)

    :ets.insert(@data_table, {key, value})
    update_access_time(key)

    # Si es nueva entrada (no update), verificar capacidad
    if existing == [] do
      evict_if_needed(state.max_size)
    end

    {:reply, :ok, state}
  end

  def handle_call({:delete, key}, _from, state) do
    remove_from_all_tables(key)
    {:reply, :ok, state}
  end

  defp update_access_time(key) do
    # Eliminar acceso anterior
    case :ets.lookup(@index_table, key) do
      [{^key, old_time}] -> :ets.delete(@access_table, old_time)
      [] -> :ok
    end

    # Insertar nuevo acceso
    new_time = System.monotonic_time(:nanosecond)
    :ets.insert(@access_table, {new_time, key})
    :ets.insert(@index_table, {key, new_time})
  end

  defp remove_from_all_tables(key) do
    case :ets.lookup(@index_table, key) do
      [{^key, time}] ->
        :ets.delete(@access_table, time)
        :ets.delete(@index_table, key)
      [] -> :ok
    end
    :ets.delete(@data_table, key)
  end

  defp evict_if_needed(max_size) do
    current_size = :ets.info(@data_table, :size)

    if current_size > max_size do
      evict_oldest(current_size - max_size)
    end
  end

  defp evict_oldest(count) do
    # ordered_set: first/next da los elementos en orden ascendente (más viejos primero)
    do_evict(:ets.first(@access_table), count)
  end

  defp do_evict(:"$end_of_table", _), do: :ok
  defp do_evict(_, 0), do: :ok
  defp do_evict(time_key, remaining) do
    case :ets.lookup(@access_table, time_key) do
      [{^time_key, key}] ->
        next = :ets.next(@access_table, time_key)
        remove_from_all_tables(key)
        do_evict(next, remaining - 1)
      [] ->
        do_evict(:ets.next(@access_table, time_key), remaining)
    end
  end
end

# Demo
{:ok, _} = LRUCache.start_link(max_size: 3)

LRUCache.put("a", 1)
LRUCache.put("b", 2)
LRUCache.put("c", 3)

IO.puts("Size after 3 inserts: #{LRUCache.size()}")  # 3

# Acceder a "a" para que no sea el LRU
LRUCache.get("a")
Process.sleep(1)  # dar tiempo al cast de touch

# Insertar "d" — debe desalojar "b" (el menos recientemente usado)
LRUCache.put("d", 4)

IO.puts("Size after 4th insert (max 3): #{LRUCache.size()}")  # 3
IO.inspect(LRUCache.get("a"), label: "a")  # {:ok, 1}
IO.inspect(LRUCache.get("b"), label: "b (evicted)")  # :miss
IO.inspect(LRUCache.get("c"), label: "c")  # {:ok, 3}
IO.inspect(LRUCache.get("d"), label: "d")  # {:ok, 4}

GenServer.stop(LRUCache)
```

**Trade-off analysis**: El LRU implementado aquí serializa todas las escrituras (incluyendo los touch de acceso) en el GenServer. Esto crea contención bajo alta carga de lecturas. Una optimización es hacer los touch async (cast en lugar de call), aceptando que el orden LRU puede ser ligeramente aproximado bajo carga. Para producción de alta concurrencia, considera una aproximación clock-hand (aproximación de LRU con less contention) o usar una librería como `cachex` que ya resuelve estos trade-offs.

---

## Common Mistakes

**No usar `monotonic_time` para TTL**
`System.system_time` puede retroceder con NTP. Si el reloj se ajusta hacia atrás, las entradas parecen "no expiradas" más tiempo del esperado. Siempre usa `System.monotonic_time` para cálculos de duración y TTL.

**Stampede prevention con lock pero sin double-check**
El patrón correcto requiere verificar el cache otra vez dentro del lock. Sin double-check, dos procesos que compiten muy cerca pueden hacer dos fetches si el primero termina antes de que el segundo entre al lock:

```elixir
# Incorrecto — sin double-check
if :ets.insert_new(locks, {key, self()}) do
  value = fetch_fn.()  # puede ejecutarse dos veces en race condition extrema
  cache_put(key, value)
end

# Correcto — con double-check dentro del lock
if :ets.insert_new(locks, {key, self()}) do
  case direct_get(key) do  # verificar de nuevo
    {:ok, value} -> value   # otro proceso ya lo populó
    :miss ->
      value = fetch_fn.()
      cache_put(key, value)
      value
  end
end
```

**LRU con writes concurrentes sin serialización**
Si dos procesos intentan actualizar el access time de la misma clave simultáneamente, pueden insertar dos entradas en el `ordered_set` de accesos con el mismo `key` pero distinto `time`, creando inconsistencias. La serialización vía GenServer o el uso de transacciones ETS (`:ets.select_replace`) es necesaria.

**Cleanup que elimina mientras se itera**
Nunca elimines de una tabla ETS mientras la iteras con `first/next`. Usa `select_delete` con match spec o recopila las claves primero y elimina en un segundo paso.

**Asumir que ETS es siempre más rápido que un mapa en el proceso**
Para estado privado de un proceso con pocas lecturas/escrituras, un mapa Erlang en el estado del proceso es más rápido (sin IPC ni locking). ETS es superior cuando múltiples procesos leen concurrentemente.

---

## Verification

```elixir
# Verificar TTL expiry
{:ok, _} = TTLCache.start_link(cleanup_interval: 100)
TTLCache.put("k", "v", 200)  # expira en 200ms
{:ok, _} = TTLCache.get("k")
Process.sleep(300)
:miss = TTLCache.get("k")
IO.puts("TTL expiry: OK")
GenServer.stop(TTLCache)
```

```elixir
# Verificar que stampede solo ejecuta fetch 1 vez
{:ok, _} = StampedeCache.start_link()
counter = :counters.new(1, [])

1..20
|> Task.async_stream(fn _ ->
  StampedeCache.get_or_fetch("test", fn ->
    :counters.add(counter, 1, 1)
    Process.sleep(30)
    "result"
  end)
end, max_concurrency: 20, timeout: 5_000)
|> Enum.to_list()

fetches = :counters.get(counter, 1)
IO.puts("Fetch executions: #{fetches}")  # debe ser 1
GenServer.stop(StampedeCache)
```

```elixir
# Verificar LRU eviction order
{:ok, _} = LRUCache.start_link(max_size: 2)
LRUCache.put("x", 1)
LRUCache.put("y", 2)
LRUCache.get("x")  # x es ahora el más reciente

LRUCache.put("z", 3)  # y debe ser evicted (LRU)

:miss = LRUCache.get("y")
{:ok, 1} = LRUCache.get("x")
{:ok, 3} = LRUCache.get("z")
IO.puts("LRU eviction: OK")
GenServer.stop(LRUCache)
```

---

## Summary

Los patrones de cache son uno de los temas más ricos en diseño de sistemas distribuidos. Los conceptos clave de este ejercicio:

- **TTL con monotonic time**: inmunidad a NTP, eviction lazy + cleanup periódico para zero memory leaks
- **Stampede prevention**: `insert_new` como mutex lightweight, siempre con double-check
- **LRU**: `ordered_set` como priority queue de accesos, serialización de writes en GenServer
- **Write-through vs write-behind**: consistencia fuerte vs throughput máximo — decisión de negocio, no técnica

Para producción con alta carga, evalúa `Cachex` (abstracción sobre ETS con todas estas estrategias integradas) antes de implementar tu propio cache.

---

## What's Next

- **Ejercicio 20**: `:atomics` y `:counters` — contadores lock-free de más bajo nivel que ETS
- **Ejercicio 36**: Circuit breaker patterns — cache como parte de un sistema de resiliencia
- **Cachex library**: [hexdocs.pm/cachex](https://hexdocs.pm/cachex) — implementación de producción con todas las estrategias

---

## Resources

- [ETS documentation — Erlang](https://www.erlang.org/doc/man/ets.html)
- [System.monotonic_time/1 — Elixir docs](https://hexdocs.pm/elixir/System.html#monotonic_time/1)
- [Cachex — production ETS cache library](https://hexdocs.pm/cachex/Cachex.html)
- ["Designing Data-Intensive Applications" — Martin Kleppmann](https://www.oreilly.com/library/view/designing-data-intensive-applications/9781491903063/) — capítulo sobre caching
