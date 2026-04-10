# 43 — Build a Cache Server (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 5-7 horas  
**Área**: GenServer · ETS · Algoritmos · Concurrencia

---

## Contexto

Un cache server de producción no es solo un mapa en memoria. Debe manejar expiración automática,
política de evicción cuando la memoria se agota, estadísticas de eficiencia y, opcionalmente,
write-through a un almacén de datos subyacente. La clave está en elegir las estructuras de datos
correctas para cada operación: O(1) lookup, O(1) evicción LRU, O(1) actualización TTL.

---

## Arquitectura propuesta

```
┌─────────────────────────────────────────────────────────────┐
│                  Cache.Server (GenServer)                   │
│                                                             │
│  ets_table:   :ets.new — lectura concurrente sin lock       │
│  lru_order:   %{key => node_ref} en lista doblemente enlaz. │
│  ttl_index:   :gb_trees — ordenado por expiry_time          │
│  config:      %{max_size, default_ttl, write_through_fn}    │
│  stats:       %{hits, misses, evictions, writes}            │
└─────────────────────────────────────────────────────────────┘
                    │
        ┌───────────┴───────────┐
        │                       │
┌───────▼──────┐       ┌────────▼────────┐
│ TTL Expirer  │       │  Stats Sampler  │
│ (GenServer)  │       │  (GenServer)    │
│ :timer each  │       │  calcula rates  │
│ 1s, limpia   │       │  cada segundo   │
│ expirados    │       └─────────────────┘
└──────────────┘
```

### Estructuras de datos críticas

```
ETS table (lectura O(1), concurrente):
  key → {value, expiry_monotonic_time}

LRU — doubly linked list (evicción O(1)):
  head ←→ [MRU] ←→ ... ←→ [LRU] ←→ tail
  Map: key → node_pid | node_ref

TTL index — :gb_trees (ordenado por tiempo):
  expiry_time → [key1, key2, ...]  (varios keys pueden expirar juntos)
```

---

## Ejercicio 1 — Cache básico con TTL

Implementa el cache con get/put/delete y expiración automática.

### Interfaz pública

```elixir
{:ok, _} = Cache.start_link(max_size: 1000, default_ttl_ms: 60_000)

Cache.put("user:42", %{name: "Alice"})
Cache.put("session:abc", token, ttl_ms: 300_000)

Cache.get("user:42")     # => {:ok, %{name: "Alice"}}
Cache.get("user:99")     # => {:miss}

Cache.delete("user:42")  # => :ok
Cache.flush()            # => :ok — limpia todo

Cache.size()             # => 1
Cache.keys()             # => ["session:abc"]
```

### Implementación con ETS

```elixir
defmodule Cache.Server do
  use GenServer

  defstruct [:table, :config, stats: %{hits: 0, misses: 0, evictions: 0, writes: 0}]

  def init(opts) do
    table = :ets.new(:cache_table, [:set, :protected, read_concurrency: true])
    config = %{
      max_size:    Keyword.get(opts, :max_size, 1_000),
      default_ttl: Keyword.get(opts, :default_ttl_ms, 60_000),
      write_through_fn: Keyword.get(opts, :write_through_fn, nil)
    }
    {:ok, %__MODULE__{table: table, config: config}}
  end

  # GET puede ir directo a ETS sin pasar por el GenServer
  def handle_call({:get, key}, _from, state) do
    now = System.monotonic_time(:millisecond)
    result = case :ets.lookup(state.table, key) do
      [{^key, value, expiry}] when expiry > now -> {:ok, value}
      [{^key, _, _}]                            -> :ets.delete(state.table, key); {:miss}
      []                                        -> {:miss}
    end
    stat_key = if match?({:ok, _}, result), do: :hits, else: :misses
    {:reply, result, update_stat(state, stat_key)}
  end
end
```

### Requisitos

- ETS con `read_concurrency: true` — los `get/1` bypasean el GenServer usando `:ets.lookup`
- TTL almacenado como `System.monotonic_time(:millisecond) + ttl_ms`
- TTL Expirer usa `:timer.send_interval(1_000, :expire)` para limpiar periódicamente
- `flush/0` hace `:ets.delete_all_objects/1`
- Tests: get hit, get miss, expiración automática, delete, flush

### Lectura directa a ETS (bypassing GenServer)

```elixir
# En el módulo facade Cache:
def get(key) do
  # Llamada directa a ETS — O(1), sin contención en el GenServer
  now = System.monotonic_time(:millisecond)
  case :ets.lookup(:cache_table, key) do
    [{^key, value, expiry}] when expiry > now -> {:ok, value}
    _                                          -> {:miss}
  end
end
# ↑ Solo put/delete/flush pasan por el GenServer (escrituras serializadas)
```

---

## Ejercicio 2 — LRU Eviction

Cuando el cache alcanza `max_size`, evicta la entrada menos recientemente usada.

### Política LRU

- Cada `get` exitoso mueve la clave al frente (Most Recently Used)
- Cada `put` inserta al frente
- Cuando `size >= max_size`, se elimina la entrada del final (LRU)

### Implementación con lista en el estado

```elixir
# Representación simple: lista ordenada [MRU, ..., LRU]
# Operación más costosa: O(n) para mover al frente
# Para producción real usar un mapa + lista doble — aquí simplificamos

defmodule Cache.LRU do
  @doc "Mueve key al frente. Si no existe, la inserta."
  def touch(order, key) do
    [key | List.delete(order, key)]
  end

  @doc "Elimina la clave LRU (última) y la devuelve."
  def evict_lru([]),    do: {nil, []}
  def evict_lru(order) do
    lru_key = List.last(order)
    {lru_key, Enum.drop(order, -1)}
  end

  @doc "Implementación O(1) con mapa de posiciones (reto adicional)"
  # Para O(1) real, implementar doubly-linked list con Map de referencias
end
```

### Requisitos

- Implementación inicial: lista simple O(n) para mover al frente — suficiente para < 10k entradas
- Reto: implementar O(1) con mapa `key → position` y lista doblemente enlazada (en Elixir puro)
- `get/1` exitoso: registra acceso como `:touch` vía GenServer (o cast asíncrono)
- `put/1`: si `size >= max_size`, evictar LRU antes de insertar
- Stats: incrementar `evictions` en cada evicción
- Tests: insertar max_size+1 elementos, verificar que LRU fue eliminado; acceder a un elemento lo salva de evicción

---

## Ejercicio 3 — Write-through y write-behind

Soporte para estrategias de escritura a un almacén subyacente.

### Write-through (síncrono)

```elixir
# El put espera a que el write-through complete antes de confirmar
Cache.start_link(
  max_size: 500,
  write_through_fn: fn key, value ->
    MyDB.insert(key, value)   # si falla, el put falla
  end
)

Cache.put("product:1", product)
# 1. Llama write_through_fn (puede lanzar)
# 2. Si tiene éxito, actualiza ETS
# 3. Si falla, no actualiza el cache
```

### Write-behind (asíncrono)

```elixir
# El put responde inmediatamente, la escritura a DB ocurre en background
Cache.start_link(
  max_size: 500,
  write_behind_fn: fn key, value ->
    Task.start(fn -> MyDB.insert(key, value) end)
  end,
  write_behind_retry: 3
)
```

### Cache-aside pattern (documentar, no implementar)

```elixir
# El caller gestiona el cache manualmente:
def get_user(id) do
  case Cache.get("user:#{id}") do
    {:ok, user} -> user
    {:miss}     ->
      user = DB.get_user(id)
      Cache.put("user:#{id}", user, ttl_ms: 300_000)
      user
  end
end
```

### Requisitos

- `write_through_fn` es una función `(key, value) -> :ok | {:error, reason}`
- Si `write_through_fn` lanza o retorna `{:error, _}`, el `put/2` retorna `{:error, reason}`
- `write_behind_fn` se llama con `Task.start` — never bloquea el GenServer
- Configurar ambos es un error de configuración — falla en `init/1`
- Tests: write-through exitoso, write-through fallido (no actualiza cache), write-behind con mock

---

## Ejercicio 4 — Estadísticas y warm-up

Estadísticas ricas y capacidad de pre-poblar el cache.

### API de estadísticas

```elixir
Cache.stats()
# => %{
#      size:           423,
#      max_size:       1000,
#      utilization:    0.423,
#      hits:           15_234,
#      misses:         3_421,
#      hit_rate:       0.816,
#      miss_rate:      0.184,
#      evictions:      892,
#      writes:         4_313,
#      avg_ttl_ms:     58_432,
#      oldest_entry:   "product:99",
#      newest_entry:   "user:42"
#    }
```

### Warm-up del cache

```elixir
Cache.warm_up(fn ->
  DB.get_top_products(100)
  |> Enum.map(fn p -> {"product:#{p.id}", p, ttl_ms: 600_000} end)
end)
# => {:ok, loaded: 100, errors: 0}
```

### Requisitos

- `hit_rate = hits / (hits + misses)` — calculado en `stats/0`, no mantenido en tiempo real
- `utilization = size / max_size`
- `warm_up/1` acepta función que retorna lista de `{key, value, opts}` — inserta en batch
- Stats accesibles sin bloquear reads (leer stats vía GenServer call está bien)
- `reset_stats/0` pone a cero contadores de hits/misses/evictions (no borra entradas)
- Tests: verificar hit_rate correcto, warm_up carga correctamente, reset_stats

### Estructura del proyecto

```
lib/
├── cache/
│   ├── application.ex     # Inicia Cache.Supervisor
│   ├── supervisor.ex      # Cache.Server + TTLExpirer + StatsSampler
│   ├── server.ex          # GenServer principal con ETS
│   ├── lru.ex             # Módulo de política LRU
│   ├── ttl_expirer.ex     # GenServer que expira TTLs
│   └── stats.ex           # Cálculos de estadísticas
test/
├── cache/
│   ├── server_test.exs
│   ├── lru_test.exs
│   ├── ttl_test.exs
│   ├── write_through_test.exs
│   └── stats_test.exs
```

---

## Criterios de aceptación

- [ ] `get/1` lee directamente de ETS sin pasar por el GenServer (lectura concurrente)
- [ ] TTL expira automáticamente — un `get` post-expiración retorna `{:miss}`
- [ ] LRU evicta correctamente la entrada menos usada cuando `size >= max_size`
- [ ] Write-through falla atómicamente — cache no actualizado si DB falla
- [ ] Write-behind responde inmediatamente y escribe en background
- [ ] `stats/0` calcula `hit_rate` correctamente después de múltiples operaciones
- [ ] `warm_up/1` carga entradas en batch respetando `max_size`
- [ ] TTL Expirer limpia entradas expiradas sin intervención manual

---

## Retos adicionales (opcional)

- LRU O(1) real con doubly linked list usando `Map` como estructura auxiliar
- Segmentación: múltiples ETS tables (sharding por hash de key) para reducir contención
- Compresión transparente de valores grandes (`:erlang.term_to_binary` + `:zlib.compress`)
- Exportar métricas a `:telemetry` para integración con Prometheus
