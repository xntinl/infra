# 16 — ETS Avanzado: Concurrencia

## Prerequisites

- ETS básico: `new/2`, `insert/2`, `lookup/2`, `delete/2`
- GenServer intermedio con estado propio
- Comprensión de procesos y schedulers en la BEAM
- Benchmarking básico con `:timer.tc/1`

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Configurar ETS para workloads de alta concurrencia con `:read_concurrency` y `:write_concurrency`
2. Usar `decentralized_counters` para eliminar contención en contadores
3. Realizar range queries eficientes con tablas `:ordered_set`
4. Escribir match specs con `:ets.fun2ms` sin escribirlas a mano
5. Elegir cuándo ETS supera a GenServer como almacén de estado
6. Implementar un rate limiter distribuido sin cuello de botella central

---

## Concepts

### 1. Opciones de concurrencia en ETS

Por defecto, ETS protege el acceso concurrente con un lock global por tabla. En aplicaciones de alta carga, esto crea contención. Las dos banderas clave son:

```elixir
:ets.new(:my_cache, [
  :set,
  :public,
  {:read_concurrency, true},
  {:write_concurrency, true}
])
```

**`:read_concurrency true`**: usa read-write locks en lugar de locks exclusivos. Múltiples lecturas concurrentes no se bloquean entre sí. Eficaz cuando las lecturas superan ampliamente a las escrituras (ratio > 10:1).

**`:write_concurrency true`**: divide la tabla internamente en segmentos (shards). Escrituras a claves distintas pueden ocurrir en paralelo si caen en segmentos diferentes. Añade overhead fijo de memoria y es contraproducente si hay muchas operaciones de tabla completa (`tab2list/1`, `first/1`, `next/2`).

**Cuándo usar cada bandera:**

| Escenario | Configuración recomendada |
|-----------|--------------------------|
| Cache con muchas lecturas | `read_concurrency: true` |
| Contador distribuido | `write_concurrency: true` + `decentralized_counters: true` |
| Mix equilibrado | ambas `true` |
| Iteración frecuente de tabla completa | ninguna (overhead no vale la pena) |

### 2. `decentralized_counters`

Disponible desde OTP 23. Elimina la contención en contadores al mantener copias por scheduler:

```elixir
:ets.new(:counters, [
  :set,
  :public,
  {:write_concurrency, true},
  {:decentralized_counters, true}
])

# update_counter es atómico y sin lock global
:ets.update_counter(:counters, :requests, {2, 1}, {:requests, 0})
```

El trade-off: leer el valor total requiere sumar los parciales de cada scheduler, lo que hace que las lecturas sean más costosas. Ideal para casos donde escribes mucho y lees poco (métricas, contadores de rate limiting).

### 3. `:ordered_set` para range queries

La estructura `:set` usa un hash table — O(1) para lookup exacto pero sin orden. `:ordered_set` usa un árbol AVL — O(log n) para lookup pero permite iterar en orden y hacer range queries:

```elixir
:ets.new(:events, [:ordered_set, :public, :named_table])

# Insertar eventos con timestamp como clave
:ets.insert(:events, {1_700_000_001, :login, "user_1"})
:ets.insert(:events, {1_700_000_002, :purchase, "user_2"})
:ets.insert(:events, {1_700_000_005, :logout, "user_1"})

# Range query: eventos entre t1 y t2
defmodule EventLog do
  def range(table, from, to) do
    match_spec = [
      {{:"$1", :"$2", :"$3"},
       [{:>=, :"$1", from}, {:"=<", :"$1", to}],
       [{{:"$1", :"$2", :"$3"}}]}
    ]
    :ets.select(table, match_spec)
  end
end
```

### 4. Match specs y `:ets.fun2ms`

Las match specs son poderosas pero su sintaxis manual es propensa a errores. `:ets.fun2ms/1` compila una función Elixir/Erlang a una match spec en tiempo de compilación:

```elixir
# Requiere importar :ets para que fun2ms funcione en Elixir
import :ets, only: [fun2ms: 1]

# Equivalente al match_spec del ejemplo anterior, pero legible:
ms = :ets.fun2ms(fn {ts, type, user}
  when ts >= 1_700_000_001 and ts =< 1_700_000_005 ->
    {ts, type, user}
end)

:ets.select(:events, ms)
```

> **Nota**: `fun2ms` solo funciona en tiempo de compilación (se expande como macro). No puede usarse con funciones anónimas guardadas en variables en runtime.

### 5. ETS como alternativa a GenServer para estado de alta concurrencia

Un GenServer serializa todo el acceso a su estado — es un cuello de botella por diseño. Para estado que es ampliamente leído y raramente escrito, ETS con `read_concurrency: true` elimina ese cuello de botella:

```elixir
# Patrón: GenServer como propietario y escritor, ETS para lecturas
defmodule ConfigStore do
  use GenServer

  @table :config_store

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def init(_) do
    :ets.new(@table, [:set, :public, :named_table, {:read_concurrency, true}])
    {:ok, %{}}
  end

  # Las lecturas van directamente a ETS — no pasan por el proceso
  def get(key), do: :ets.lookup(@table, key) |> case do
    [{^key, value}] -> {:ok, value}
    [] -> :error
  end

  # Solo las escrituras pasan por el GenServer (serialización controlada)
  def put(key, value), do: GenServer.call(__MODULE__, {:put, key, value})

  def handle_call({:put, key, value}, _from, state) do
    :ets.insert(@table, {key, value})
    {:reply, :ok, state}
  end
end
```

Este patrón escala lecturas a N schedulers sin contención, mientras mantiene consistencia en escrituras.

### 6. `:ets.select_count` y operaciones bulk

Para operaciones de análisis sobre tablas grandes:

```elixir
# Contar elementos que cumplen condición sin materializarlos todos
import :ets, only: [fun2ms: 1]

ms = :ets.fun2ms(fn {_ts, :error, _user} -> true end)
error_count = :ets.select_count(:events, ms)

# select_delete: eliminar en batch con match spec
expired_ms = :ets.fun2ms(fn {ts, _, _} when ts < 1_700_000_003 -> true end)
:ets.select_delete(:events, expired_ms)
```

---

## Exercises

### Exercise 1 — Benchmark GenServer vs ETS para lecturas concurrentes

**Problem**

Mide empíricamente la diferencia de throughput entre un GenServer tradicional y ETS con `read_concurrency` cuando N procesos hacen lecturas concurrentes. El objetivo no es el número absoluto sino entender cuándo el cambio vale la pena.

Implementa:
1. `SlowStore` — GenServer que guarda un mapa en estado y responde a `get/1` con `call`
2. `FastStore` — ETS con `read_concurrency: true`, reads directas sin GenServer
3. Una función `benchmark/2` que lanza N tareas concurrentes, cada una haciendo K lecturas, y reporta el tiempo total y throughput (ops/sec)

**Hints**

- Usa `Task.async_stream/3` con `max_concurrency: N` para simular carga concurrente
- Precarga datos antes de medir (10-100 claves está bien)
- Mide con `System.monotonic_time(:millisecond)` antes y después
- Prueba con N = 1, 10, 50, 100 tareas concurrentes para ver la curva

**One possible solution**

```elixir
defmodule SlowStore do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)
  def init(state), do: {:ok, state}

  def load(data) when is_map(data), do: GenServer.call(__MODULE__, {:load, data})
  def get(key), do: GenServer.call(__MODULE__, {:get, key})

  def handle_call({:load, data}, _from, _state), do: {:reply, :ok, data}
  def handle_call({:get, key}, _from, state), do: {:reply, Map.get(state, key), state}
end

defmodule FastStore do
  @table :fast_store

  def start do
    :ets.new(@table, [:set, :public, :named_table, {:read_concurrency, true}])
  end

  def load(data) when is_map(data) do
    Enum.each(data, fn {k, v} -> :ets.insert(@table, {k, v}) end)
  end

  def get(key) do
    case :ets.lookup(@table, key) do
      [{^key, value}] -> value
      [] -> nil
    end
  end
end

defmodule ConcurrencyBenchmark do
  @keys Enum.map(1..100, &"key_#{&1}")
  @data Map.new(@keys, &{&1, :rand.uniform(1_000_000)})

  def run do
    {:ok, _} = SlowStore.start_link([])
    SlowStore.load(@data)

    FastStore.start()
    FastStore.load(@data)

    for concurrency <- [1, 10, 50, 100] do
      slow = benchmark_slow(concurrency, 500)
      fast = benchmark_fast(concurrency, 500)

      IO.puts("""
      Concurrency: #{concurrency} tasks × 500 reads
        GenServer: #{slow.total_ms}ms  (#{slow.ops_per_sec} ops/sec)
        ETS:       #{fast.total_ms}ms  (#{fast.ops_per_sec} ops/sec)
        Speedup:   #{Float.round(slow.total_ms / max(fast.total_ms, 1), 1)}x
      """)
    end
  end

  defp benchmark_slow(concurrency, reads_per_task) do
    t0 = System.monotonic_time(:millisecond)

    1..concurrency
    |> Task.async_stream(fn _ ->
      for _ <- 1..reads_per_task do
        SlowStore.get(Enum.random(@keys))
      end
    end, max_concurrency: concurrency, timeout: 30_000)
    |> Stream.run()

    t1 = System.monotonic_time(:millisecond)
    total_ops = concurrency * reads_per_task
    elapsed = t1 - t0

    %{
      total_ms: elapsed,
      ops_per_sec: round(total_ops / max(elapsed, 1) * 1000)
    }
  end

  defp benchmark_fast(concurrency, reads_per_task) do
    t0 = System.monotonic_time(:millisecond)

    1..concurrency
    |> Task.async_stream(fn _ ->
      for _ <- 1..reads_per_task do
        FastStore.get(Enum.random(@keys))
      end
    end, max_concurrency: concurrency, timeout: 30_000)
    |> Stream.run()

    t1 = System.monotonic_time(:millisecond)
    total_ops = concurrency * reads_per_task
    elapsed = t1 - t0

    %{
      total_ms: elapsed,
      ops_per_sec: round(total_ops / max(elapsed, 1) * 1000)
    }
  end
end

ConcurrencyBenchmark.run()
```

**Trade-off analysis**: El GenServer escala linealmente peor con más concurrencia porque serializa todas las llamadas. ETS con `read_concurrency` escala casi horizontalmente hasta el número de schedulers. El punto de inflexión suele estar entre 5-10 procesos concurrentes: por debajo, GenServer es suficientemente rápido; por encima, ETS gana de forma significativa.

---

### Exercise 2 — Range query con `:ordered_set` y `fun2ms`

**Problem**

Implementa un log de eventos en memoria usando `:ordered_set` con timestamps como clave. El sistema debe:
1. Aceptar inserciones con timestamp Unix (microsegundos)
2. Soportar queries por rango de tiempo `[from, to]`
3. Soportar filtrado por tipo de evento dentro del rango
4. Implementar un TTL de purga: eliminar eventos anteriores a N segundos

El reto está en escribir las match specs de forma legible usando `fun2ms`.

**Hints**

- La clave del tuple debe ser `{timestamp, id_unico}` para evitar colisiones (dos eventos pueden tener el mismo timestamp)
- `fun2ms` debe usarse dentro de una función, no en el top-level del módulo
- Para rangos, los guards `>=` y `=<` funcionan en match specs
- `select_delete/2` es atómico y eficiente para TTL cleanup

**One possible solution**

```elixir
defmodule EventLog do
  @table :event_log

  def new do
    :ets.new(@table, [:ordered_set, :public, :named_table])
  end

  def insert(type, data) when is_atom(type) do
    ts = System.os_time(:microsecond)
    # clave compuesta para unicidad sin sacrificar ordenación temporal
    id = :erlang.unique_integer([:monotonic, :positive])
    :ets.insert(@table, {{ts, id}, type, data})
  end

  # Trae todos los eventos en el rango [from_ts, to_ts] (microsegundos)
  def range(from_ts, to_ts) do
    ms = build_range_ms(from_ts, to_ts, :_)
    :ets.select(@table, ms)
  end

  # Trae eventos en rango filtrados por tipo
  def range_by_type(from_ts, to_ts, type) when is_atom(type) do
    ms = build_range_ms(from_ts, to_ts, type)
    :ets.select(@table, ms)
  end

  # Elimina eventos anteriores a `max_age_seconds` segundos
  def purge_older_than(max_age_seconds) do
    cutoff = System.os_time(:microsecond) - max_age_seconds * 1_000_000

    ms = :ets.fun2ms(fn {{ts, _id}, _type, _data} when ts < cutoff -> true end)
    deleted = :ets.select_delete(@table, ms)
    {:purged, deleted}
  end

  def count, do: :ets.info(@table, :size)

  # fun2ms no puede recibir variables capturadas desde el exterior directamente,
  # así que construimos el match spec manualmente para el rango variable
  defp build_range_ms(from_ts, to_ts, :_) do
    [
      {
        {{:"$1", :"$2"}, :"$3", :"$4"},
        [{:>=, :"$1", from_ts}, {:"=<", :"$1", to_ts}],
        [{{{{:"$1", :"$2"}}, :"$3", :"$4"}}]
      }
    ]
  end

  defp build_range_ms(from_ts, to_ts, type) do
    [
      {
        {{:"$1", :"$2"}, :"$3", :"$4"},
        [{:>=, :"$1", from_ts}, {:"=<", :"$1", to_ts}, {:"=:=", :"$3", type}],
        [{{{{:"$1", :"$2"}}, :"$3", :"$4"}}]
      }
    ]
  end
end

# Demo
EventLog.new()

now = System.os_time(:microsecond)
EventLog.insert(:login, %{user: "alice"})
Process.sleep(1)
EventLog.insert(:purchase, %{user: "alice", amount: 99.9})
Process.sleep(1)
EventLog.insert(:login, %{user: "bob"})
Process.sleep(1)
EventLog.insert(:error, %{code: 500, path: "/api/items"})
Process.sleep(1)
later = System.os_time(:microsecond)

IO.inspect(EventLog.range(now, later), label: "all events in range")
IO.inspect(EventLog.range_by_type(now, later, :login), label: "only :login events")

{:purged, n} = EventLog.purge_older_than(0)
IO.puts("Purged #{n} events (cutoff = now)")
IO.puts("Remaining: #{EventLog.count()}")
```

**Trade-off analysis**: `:ordered_set` cuesta ~30-40% más memoria que `:set` para las mismas entradas y el lookup exacto es O(log n) vs O(1). El beneficio es range queries sin full scan y ordenación garantizada sin sort posterior. Úsalo cuando tus access patterns sean temporales o requieran iteración en orden.

---

### Exercise 3 — Rate limiter concurrente sin bottleneck

**Problem**

Implementa un rate limiter de ventana fija (fixed window) que permita N peticiones por proceso/cliente en W segundos. El reto es hacerlo sin un GenServer central que sea cuello de botella: el estado debe vivir en ETS y el cleanup debe ser lazy (no un proceso timer centralizado).

Requisitos:
1. `RateLimiter.check(client_id, limit, window_seconds)` — retorna `{:ok, remaining}` o `{:error, :rate_limited, retry_after_ms}`
2. Thread-safe: múltiples procesos pueden llamar con el mismo `client_id` concurrentemente
3. Sin GenServer, sin proceso supervisor: solo ETS + operaciones atómicas
4. Cleanup lazy: entradas expiradas se eliminan cuando se accede a ellas

**Hints**

- La clave puede ser `{client_id, window_number}` donde `window_number = div(unix_seconds, window_seconds)`
- `update_counter/4` con valor inicial es atómico y retorna el nuevo valor
- El cleanup lazy es: si `window_number` de la entrada es anterior al actual, resetear
- Para `retry_after_ms`: `(window_number + 1) * window_seconds * 1000 - unix_ms_now`

**One possible solution**

```elixir
defmodule RateLimiter do
  @table :rate_limiter

  def start do
    if :ets.whereis(@table) == :undefined do
      :ets.new(@table, [
        :set,
        :public,
        :named_table,
        {:write_concurrency, true},
        {:read_concurrency, true},
        {:decentralized_counters, true}
      ])
    end
  end

  @spec check(term(), pos_integer(), pos_integer()) ::
    {:ok, non_neg_integer()} | {:error, :rate_limited, non_neg_integer()}
  def check(client_id, limit, window_seconds) do
    now_ms = System.os_time(:millisecond)
    now_sec = div(now_ms, 1000)
    window_number = div(now_sec, window_seconds)
    key = {client_id, window_number}

    count = :ets.update_counter(@table, key, {2, 1}, {key, 0})

    if count <= limit do
      {:ok, limit - count}
    else
      # Calcula cuándo expira la ventana actual
      next_window_start_ms = (window_number + 1) * window_seconds * 1000
      retry_after_ms = next_window_start_ms - now_ms
      {:error, :rate_limited, max(retry_after_ms, 0)}
    end
  end

  # Cleanup de entradas viejas — llamar periódicamente o desde un proceso de mantenimiento
  def cleanup(window_seconds) do
    now_sec = div(System.os_time(:millisecond), 1000)
    current_window = div(now_sec, window_seconds)

    ms = :ets.fun2ms(fn {{_client, w}, _count} when w < current_window -> true end)
    deleted = :ets.select_delete(@table, ms)
    {:cleaned, deleted}
  end
end

# Demo de uso concurrente
RateLimiter.start()

limit = 5
window = 10  # segundos

results =
  1..20
  |> Task.async_stream(fn i ->
    client = "user_#{rem(i, 3)}"  # 3 clientes distintos
    result = RateLimiter.check(client, limit, window)
    {client, i, result}
  end, max_concurrency: 20)
  |> Enum.map(fn {:ok, r} -> r end)

Enum.each(results, fn {client, req, result} ->
  case result do
    {:ok, remaining} ->
      IO.puts("#{client} req##{req}: ALLOWED (#{remaining} left)")
    {:error, :rate_limited, retry_ms} ->
      IO.puts("#{client} req##{req}: BLOCKED (retry in #{retry_ms}ms)")
  end
end)
```

**Trade-off analysis**: Este diseño escala perfectamente horizontalmente porque no hay proceso central. La contención solo ocurre en la misma clave `{client_id, window}`, que es inherente al problema. `decentralized_counters` ayuda cuando hay muchos clientes distintos. La desventaja: el cleanup lazy puede acumular entradas viejas en workloads con muchos clientes únicos — se necesita un cleanup periódico para producción.

---

## Common Mistakes

**Usar `read_concurrency` con tablas donde iteras frecuentemente**
`tab2list/1`, `first/1`, `last/1`, `next/2` adquieren un lock completo sobre la tabla independientemente de la configuración. Si tu workload implica iterar la tabla completa con frecuencia, `read_concurrency: true` no ayuda y añade overhead.

**Asumir que `write_concurrency` hace todas las escrituras sin lock**
Operaciones como `delete_all_objects/1` o `delete/1` de una clave que afecta a múltiples objetos aún requieren coordinación. La concurrencia se aplica a nivel de segmento (shard), no por operación individual.

**Usar `:ordered_set` donde no necesitas orden**
El árbol AVL de `:ordered_set` tiene overhead constante vs el hash table de `:set`. Para lookups exactos sin range queries, `:set` siempre es más rápido.

**Escribir match specs a mano en lugar de usar `fun2ms`**
Las match specs manuales son propensas a errores de arity en los guards (`:>=` vs `>=`, etc.). `fun2ms` es más seguro y el overhead de compilación es cero en runtime porque es una macro.

**Olvidar que `fun2ms` no captura variables del scope externo**
`fun2ms` expande en tiempo de compilación. No puede usar variables del scope de la función que la contiene. Cuando necesitas match specs dinámicas, debes construirlas manualmente como listas de tuples.

**Crear la tabla ETS dentro de un proceso efímero**
El proceso propietario de una tabla ETS es quien la creó. Si ese proceso muere, la tabla se elimina (a menos que uses `:heir`). Siempre crea tablas en procesos supervisados y de vida larga (Application, GenServer permanente).

---

## Verification

Ejecuta los ejercicios con:

```bash
# Ejercicio 1: benchmark
elixir 16-ets-advanced-concurrent.md  # o copiar el código en un .exs

# Verificación manual del benchmark:
# - Con concurrency=1, GenServer y ETS deben ser similares
# - Con concurrency=50+, ETS debe ser 3-10x más rápido
# - Si ambos son iguales, verificar que :read_concurrency está activado
```

```elixir
# Verificación del rate limiter
RateLimiter.start()

# Debe permitir exactamente 5 requests
results = for _ <- 1..7, do: RateLimiter.check("test_user", 5, 60)
allowed = Enum.count(results, &match?({:ok, _}, &1))
blocked = Enum.count(results, &match?({:error, :rate_limited, _}, &1))

IO.puts("Allowed: #{allowed}, Blocked: #{blocked}")
# Expected: Allowed: 5, Blocked: 2
```

```elixir
# Verificación del event log
EventLog.new()
EventLog.insert(:a, "x")
EventLog.insert(:b, "y")

count_before = EventLog.count()
{:purged, _} = EventLog.purge_older_than(0)
count_after = EventLog.count()

IO.puts("Before: #{count_before}, After: #{count_after}")
# count_after debe ser 0
```

---

## Summary

ETS con opciones de concurrencia es una de las herramientas más poderosas de la BEAM para estado compartido de alta performance. Los patrones clave son:

- **`read_concurrency: true`**: elimina contención en workloads read-heavy
- **`write_concurrency: true` + `decentralized_counters: true`**: contadores sin lock global
- **`:ordered_set`**: cuando necesitas orden o range queries; acepta el overhead de O(log n)
- **`fun2ms`**: match specs legibles y correctas sin escribirlas a mano
- **ETS + GenServer**: GenServer como propietario/escritor, ETS para lecturas directas — patrón estándar para config y caches

La regla práctica: si mides contención en un GenServer con VisualVM o `:sys.get_status/1` y ves un mailbox que crece, ETS es la solución natural.

---

## What's Next

- **Ejercicio 17**: DETS para persistencia — cuando los datos deben sobrevivir reinicios
- **Ejercicio 19**: Cache patterns con ETS — TTL, LRU, cache stampede
- **Ejercicio 20**: `:atomics` y `:counters` para counters lock-free de más bajo nivel
- **Ejercicio 25**: BEAM schedulers y reductions — entender cómo los schedulers afectan a ETS

---

## Resources

- [Erlang ETS documentation](https://www.erlang.org/doc/man/ets.html) — referencia completa de opciones y operaciones
- [Erlang efficiency guide — ETS](https://www.erlang.org/doc/efficiency_guide/tablesDatabases.html) — cuándo usar qué tipo de tabla
- [`:ets.fun2ms` documentation](https://www.erlang.org/doc/man/ms_transform.html) — cómo funciona la transformación
- [Saša Jurić — "The Erlang Runtime System"](https://www.oreilly.com/library/view/the-erlang-runtime/9781800560818/) — capítulo sobre schedulers y concurrencia en ETS
