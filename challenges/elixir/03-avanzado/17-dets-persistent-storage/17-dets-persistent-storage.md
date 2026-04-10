# 17 — DETS: Persistent Storage

## Prerequisites

- ETS básico y avanzado (ejercicios 15-16)
- File I/O en Erlang/Elixir: `File.open/2`, paths
- Conceptos de crash recovery y durabilidad
- GenServer como proceso supervisor de recursos

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Abrir, usar y cerrar tablas DETS correctamente
2. Implementar inserción, lookup, match y select sobre DETS
3. Recuperarte de una tabla DETS corrupta con `:dets.repair/1`
4. Entender los trade-offs de DETS vs ETS vs Mnesia
5. Implementar el patrón cache L1 (ETS) + L2 (DETS) para persistencia con performance
6. Razonar sobre cuándo DETS es la herramienta correcta y cuándo no lo es

---

## Concepts

### 1. Qué es DETS

DETS (Disk Erlang Term Storage) es el equivalente persistente de ETS. Los datos se escriben a disco y sobreviven reinicios del proceso o del nodo. La API es casi idéntica a ETS, pero con diferencias críticas:

```elixir
# ETS: en memoria, desaparece con el proceso
table = :ets.new(:my_table, [:set])

# DETS: en disco, persiste entre reinicios
{:ok, ref} = :dets.open_file(:my_dets, [type: :set, file: ~c"./my_dets.dat"])
```

**Diferencias fundamentales con ETS:**

| Característica | ETS | DETS |
|---------------|-----|------|
| Almacenamiento | RAM | Disco |
| Velocidad | O(1) RAM | I/O bound |
| Límite de tamaño | RAM disponible | 2GB por tabla |
| Tipos disponibles | set, bag, duplicate_bag, ordered_set | set, bag, duplicate_bag |
| Concurrencia | Excelente con flags | Limitada (file lock) |
| Crash recovery | Sin estado = sin recovery | Archivo + header de integridad |

### 2. API de DETS

```elixir
# Abrir (crea si no existe)
{:ok, ref} = :dets.open_file(:config_store, [
  type: :set,
  file: ~c"/tmp/config.dets",
  # ram_file: true  # carga todo en RAM, más rápido pero sin persistencia incremental
  # repair: false   # no reparar si está corrupto (falla rápido)
  # auto_save: 5000 # flush a disco cada 5 segundos (default: 180_000ms)
])

# Insertar
:dets.insert(ref, {:db_host, "postgres.prod.example.com"})
:dets.insert(ref, {:db_port, 5432})
:dets.insert(ref, {:feature_flags, %{dark_mode: true, beta: false}})

# Lookup
[{:db_host, host}] = :dets.lookup(ref, :db_host)

# Match (pattern matching sobre el objeto)
:dets.match(ref, {:"$1", "postgres.prod.example.com"})
# => [[:db_host]]

# Select con match spec
ms = [{{:"$1", :"$2"}, [], [{{:"$1", :"$2"}}]}]
:dets.select(ref, ms)

# Iterar todos los objetos (equivale a tab2list en ETS)
:dets.traverse(ref, fn obj ->
  IO.inspect(obj)
  :continue  # o {:done, result} para parar
end)

# Flush explícito a disco
:dets.sync(ref)

# Cerrar — SIEMPRE cerrar o los datos pueden perderse/corromperse
:dets.close(ref)
```

### 3. Crash recovery con `:dets.repair`

Si el proceso cierra abruptamente sin llamar a `close/1`, DETS puede detectar el estado inconsistente y reparar automáticamente al siguiente `open_file`:

```elixir
# Por defecto, DETS intenta reparar automáticamente
# El log muestra: "** DETS: repair needed for ./config.dets"
{:ok, ref} = :dets.open_file(:config, [file: ~c"./config.dets"])

# Para reparar manualmente (tabla cerrada):
:dets.repair(~c"./broken.dets")
# => :ok | {:error, reason}

# Para fallar rápido en lugar de reparar (útil en tests):
case :dets.open_file(:strict_table, [file: ~c"./data.dets", repair: false]) do
  {:ok, ref} -> ref
  {:error, {:needs_repair, _file}} ->
    # Decisión explícita: reparar o restaurar desde backup
    raise "DETS corrupted — restore from backup"
end
```

La reparación puede ser lenta para tablas grandes — planifica tiempo de startup si la base de datos es grande.

### 4. Limitaciones críticas de DETS

**Límite de 2GB**: DETS no puede crecer más de 2GB por archivo. Para datasets más grandes, Mnesia o SQLite (via Exqlite) son alternativas.

**Sin concurrencia de escritura**: DETS no tiene equivalente a `write_concurrency`. Escrituras concurrentes al mismo archivo se serializan vía file lock del OS, con riesgo de corrupción si no se usa correctamente.

**auto_save no garantiza durabilidad por operación**: Por defecto, DETS hace sync a disco cada 3 minutos. `insert/2` puede retornar `:ok` sin que el dato esté en disco. Para durabilidad real, llama `sync/1` después de cada escritura crítica o usa `auto_save: 0` (sync en cada escritura, muy lento).

### 5. DETS vs ETS vs Mnesia: cuándo usar cada uno

```
Necesito persistencia?
├── No → ETS (velocidad máxima)
└── Sí
    ├── Dataset < 2GB, acceso simple, un nodo?
    │   ├── Necesito queries complejas? → No → DETS
    │   └── Sí → Mnesia disc_copies o SQLite
    └── Cluster multi-nodo o replicación?
        └── Mnesia (disc_copies + schema replicado)
```

DETS encaja bien para: configuración persistida, caches con TTL que sobreviven reinicios, logs de auditoría simples, feature flags que no requieren queries complejas.

### 6. Patrón L1/L2 cache

El patrón más común en producción combina ETS (L1, fast) con DETS (L2, persistent):

```elixir
defmodule TwoLevelCache do
  # L1: ETS — reads en nanosegundos
  # L2: DETS — persiste entre reinicios, reads en microsegundos

  def get(key) do
    case ets_get(key) do
      {:ok, value} ->
        {:ok, value}  # cache hit L1
      :miss ->
        case dets_get(key) do
          {:ok, value} ->
            ets_put(key, value)  # warm up L1
            {:ok, value}
        :miss ->
          :miss
        end
    end
  end

  def put(key, value) do
    ets_put(key, value)   # L1 inmediato
    dets_put(key, value)  # L2 persistente
  end
end
```

---

## Exercises

### Exercise 1 — Persistent config store

**Problem**

Implementa un `ConfigStore` supervisado que persiste configuración de la aplicación en DETS. El store debe:
1. Sobrevivir reinicios: al levantar el proceso, cargar la configuración desde disco
2. Exponer `get/1`, `put/2`, `delete/1`, `all/0`
3. Agrupar configuración por namespace: `put(:db, :host, "localhost")` vs `put(:cache, :ttl, 300)`
4. Al cerrar limpiamente (`terminate/2`), sincronizar y cerrar DETS

**Hints**

- El GenServer es el propietario del file handle de DETS; guárdalo en el estado
- En `init/1`, abre DETS; en `terminate/2`, cierra con `:dets.close/1`
- Para namespaces, la clave puede ser `{namespace, key}`
- `Application.app_dir/2` es útil para paths relativos al release; para ejercicio usa `System.tmp_dir!()`

**One possible solution**

```elixir
defmodule ConfigStore do
  use GenServer

  @table :config_store

  # API pública — lecturas van directo a ETS para velocidad
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def get(namespace, key), do: get({namespace, key})
  def get(key) do
    case :dets.lookup(@table, key) do
      [{^key, value}] -> {:ok, value}
      [] -> :error
    end
  end

  def put(namespace, key, value), do: put({namespace, key}, value)
  def put(key, value), do: GenServer.call(__MODULE__, {:put, key, value})

  def delete(namespace, key), do: delete({namespace, key})
  def delete(key), do: GenServer.call(__MODULE__, {:delete, key})

  def all do
    :dets.match(@table, {:"$1", :"$2"})
    |> Enum.map(fn [k, v] -> {k, v} end)
  end

  def all(namespace) do
    ms = [
      {{{namespace, :"$1"}, :"$2"}, [], [{{:"$1", :"$2"}}]}
    ]
    :dets.select(@table, ms)
    |> Enum.map(fn {k, v} -> {k, v} end)
  end

  # GenServer callbacks
  def init(opts) do
    path = Keyword.get(opts, :path, Path.join(System.tmp_dir!(), "config_store.dets"))

    {:ok, ref} = :dets.open_file(@table, [
      type: :set,
      file: String.to_charlist(path),
      auto_save: 5_000  # sync cada 5 segundos
    ])

    count = :dets.info(ref, :size)
    IO.puts("ConfigStore: opened DETS with #{count} existing entries at #{path}")

    {:ok, %{dets: ref, path: path}}
  end

  def handle_call({:put, key, value}, _from, state) do
    :ok = :dets.insert(state.dets, {key, value})
    {:reply, :ok, state}
  end

  def handle_call({:delete, key}, _from, state) do
    :ok = :dets.delete(state.dets, key)
    {:reply, :ok, state}
  end

  def terminate(_reason, state) do
    :dets.sync(state.dets)
    :dets.close(state.dets)
    IO.puts("ConfigStore: DETS closed cleanly")
  end
end

# Demo: simula restart
{:ok, _} = ConfigStore.start_link()

ConfigStore.put(:db, :host, "postgres.prod.example.com")
ConfigStore.put(:db, :port, 5432)
ConfigStore.put(:cache, :ttl, 300)
ConfigStore.put(:feature, :dark_mode, true)

IO.inspect(ConfigStore.get(:db, :host), label: "db.host")
IO.inspect(ConfigStore.all(:db), label: "all :db config")
IO.inspect(ConfigStore.all(), label: "all config")

# Simular shutdown
GenServer.stop(ConfigStore)

# Simular restart — debe recuperar los datos
{:ok, _} = ConfigStore.start_link()
IO.inspect(ConfigStore.get(:db, :host), label: "db.host after restart")
# Expected: {:ok, "postgres.prod.example.com"}
```

---

### Exercise 2 — Append-only log con DETS

**Problem**

Implementa un log de auditoría append-only. Los logs de auditoría tienen características especiales:
- Nunca se modifican: solo se añaden entradas
- Se consultan por rango de tiempo o por `entity_id`
- Deben sobrevivir crashes
- No se puede perder ninguna entrada confirmada

Implementa `AuditLog.append/3`, `AuditLog.query_by_entity/2`, `AuditLog.query_by_range/2` y una función de compactación que archiva entradas antiguas a un archivo separado.

**Hints**

- Usa `bag` en lugar de `set` para permitir múltiples entradas con la misma clave (entity_id)
- La clave primaria del log puede ser `entity_id`, pero necesitas un timestamp en el valor para filtrar por rango
- Para durabilidad real, llama `sync/1` después de cada `append`
- La compactación implica abrir un segundo archivo DETS, copiar, y cerrar

**One possible solution**

```elixir
defmodule AuditLog do
  use GenServer

  # Formato de entrada: {entity_id, {timestamp, action, metadata}}
  # Tipo: :bag — múltiples entradas por entity_id

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def append(entity_id, action, metadata \\ %{}) do
    GenServer.call(__MODULE__, {:append, entity_id, action, metadata})
  end

  def query_by_entity(entity_id, limit \\ 100) do
    GenServer.call(__MODULE__, {:query_entity, entity_id, limit})
  end

  def query_by_range(from_ts, to_ts) do
    GenServer.call(__MODULE__, {:query_range, from_ts, to_ts})
  end

  def compact(older_than_seconds) do
    GenServer.call(__MODULE__, {:compact, older_than_seconds}, 30_000)
  end

  # Callbacks
  def init(opts) do
    path = Keyword.get(opts, :path, Path.join(System.tmp_dir!(), "audit_log.dets"))
    archive_path = Keyword.get(opts, :archive_path, Path.join(System.tmp_dir!(), "audit_log_archive.dets"))

    {:ok, ref} = :dets.open_file(:audit_log, [
      type: :bag,
      file: String.to_charlist(path)
    ])

    {:ok, %{dets: ref, path: path, archive_path: archive_path}}
  end

  def handle_call({:append, entity_id, action, metadata}, _from, state) do
    ts = System.os_time(:microsecond)
    entry = {entity_id, {ts, action, metadata}}

    :ok = :dets.insert(state.dets, entry)
    :ok = :dets.sync(state.dets)  # durabilidad garantizada por entrada

    {:reply, {:ok, ts}, state}
  end

  def handle_call({:query_entity, entity_id, limit}, _from, state) do
    entries =
      :dets.lookup(state.dets, entity_id)
      |> Enum.sort_by(fn {_id, {ts, _, _}} -> ts end, :desc)
      |> Enum.take(limit)
      |> Enum.map(fn {_id, entry} -> entry end)

    {:reply, entries, state}
  end

  def handle_call({:query_range, from_ts, to_ts}, _from, state) do
    ms = [
      {
        {:"$1", {:"$2", :"$3", :"$4"}},
        [{:>=, :"$2", from_ts}, {:"=<", :"$2", to_ts}],
        [{{:"$1", :"$2", :"$3", :"$4"}}]
      }
    ]

    entries = :dets.select(state.dets, ms)
    {:reply, entries, state}
  end

  def handle_call({:compact, older_than_seconds}, _from, state) do
    cutoff_ts = System.os_time(:microsecond) - older_than_seconds * 1_000_000

    # Seleccionar entradas antiguas
    old_entries_ms = [
      {
        {:"$1", {:"$2", :"$3", :"$4"}},
        [{:<, :"$2", cutoff_ts}],
        [{{:"$1", {:"$2", :"$3", :"$4"}}}]
      }
    ]

    old_entries = :dets.select(state.dets, old_entries_ms)

    # Abrir archivo de archivo histórico
    {:ok, archive_ref} = :dets.open_file(:audit_archive, [
      type: :bag,
      file: String.to_charlist(state.archive_path)
    ])

    Enum.each(old_entries, fn {entity_id, {ts, action, meta}} ->
      :dets.insert(archive_ref, {entity_id, {ts, action, meta}})
    end)

    :dets.sync(archive_ref)
    :dets.close(archive_ref)

    # Eliminar entradas antiguas del log principal
    delete_ms = [
      {
        {:"$1", {:"$2", :"$3", :"$4"}},
        [{:<, :"$2", cutoff_ts}],
        [true]
      }
    ]
    deleted = :dets.select_delete(state.dets, delete_ms)
    :dets.sync(state.dets)

    {:reply, {:compacted, length(old_entries), :archived, deleted}, state}
  end

  def terminate(_reason, state) do
    :dets.sync(state.dets)
    :dets.close(state.dets)
  end
end

# Demo
{:ok, _} = AuditLog.start_link()

{:ok, _} = AuditLog.append("user_1", :login, %{ip: "1.2.3.4"})
{:ok, _} = AuditLog.append("user_1", :view_page, %{path: "/dashboard"})
{:ok, _} = AuditLog.append("user_2", :login, %{ip: "5.6.7.8"})
{:ok, _} = AuditLog.append("user_1", :purchase, %{amount: 99.9})

IO.inspect(AuditLog.query_by_entity("user_1"), label: "user_1 audit trail")

t_start = System.os_time(:microsecond) - 1_000_000
t_end = System.os_time(:microsecond)
IO.inspect(AuditLog.query_by_range(t_start, t_end), label: "last second")

GenServer.stop(AuditLog)
```

---

### Exercise 3 — Cache con DETS backing + ETS fronting

**Problem**

Implementa un `PersistentCache` con dos capas:
- **L1 (ETS)**: respuestas inmediatas, sin I/O, con TTL en memoria
- **L2 (DETS)**: persistencia entre reinicios, con TTL almacenado en el valor

Comportamiento esperado:
- `get/1`: L1 hit → retorna inmediato. L1 miss → consulta L2 → si existe y no expiró, carga en L1 y retorna. Si no existe o expiró en L2, retorna `:miss`
- `put/3`: escribe en L1 y L2 simultáneamente
- Al iniciar, no carga todo L2 en L1 (lazy warming)
- Cleanup periódico de L1 (proceso separado) y L2 lazy (en get)

**Hints**

- El valor en DETS incluye `{value, expires_at}` donde `expires_at` es `System.os_time(:millisecond) + ttl_ms`
- El ETS puede usar `{value, expires_at}` también, eliminando la necesidad de un proceso de cleanup si haces eviction lazy en get
- Para el cleanup periódico de ETS, usa `Process.send_after(self(), :cleanup, interval)` en `handle_info`

**One possible solution**

```elixir
defmodule PersistentCache do
  use GenServer

  @l1_table :persistent_cache_l1
  @l2_table :persistent_cache_l2
  @cleanup_interval_ms 60_000  # cleanup L1 cada minuto

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def get(key) do
    case l1_get(key) do
      {:ok, value} ->
        {:ok, value}
      :miss ->
        l2_get_and_warm(key)
    end
  end

  def put(key, value, ttl_ms \\ 300_000) do
    GenServer.call(__MODULE__, {:put, key, value, ttl_ms})
  end

  def invalidate(key) do
    GenServer.call(__MODULE__, {:invalidate, key})
  end

  # L1: lectura directa sin pasar por GenServer
  defp l1_get(key) do
    now = System.os_time(:millisecond)
    case :ets.lookup(@l1_table, key) do
      [{^key, value, expires_at}] when expires_at > now -> {:ok, value}
      [{^key, _value, _expired}] ->
        :ets.delete(@l1_table, key)
        :miss
      [] -> :miss
    end
  end

  defp l2_get_and_warm(key) do
    now = System.os_time(:millisecond)
    case :dets.lookup(@l2_table, key) do
      [{^key, value, expires_at}] when expires_at > now ->
        # Warm L1 con TTL restante
        remaining_ttl = expires_at - now
        :ets.insert(@l1_table, {key, value, expires_at})
        {:ok, value}
      [{^key, _value, _expired}] ->
        # Lazy eviction en L2
        :dets.delete(@l2_table, key)
        :miss
      [] ->
        :miss
    end
  end

  # GenServer callbacks
  def init(opts) do
    path = Keyword.get(opts, :path, Path.join(System.tmp_dir!(), "persistent_cache.dets"))

    :ets.new(@l1_table, [:set, :public, :named_table, {:read_concurrency, true}])

    {:ok, dets_ref} = :dets.open_file(@l2_table, [
      type: :set,
      file: String.to_charlist(path),
      auto_save: 10_000
    ])

    # Programar cleanup periódico de L1
    Process.send_after(self(), :cleanup_l1, @cleanup_interval_ms)

    {:ok, %{dets: dets_ref}}
  end

  def handle_call({:put, key, value, ttl_ms}, _from, state) do
    expires_at = System.os_time(:millisecond) + ttl_ms
    :ets.insert(@l1_table, {key, value, expires_at})
    :dets.insert(state.dets, {key, value, expires_at})
    {:reply, :ok, state}
  end

  def handle_call({:invalidate, key}, _from, state) do
    :ets.delete(@l1_table, key)
    :dets.delete(state.dets, key)
    {:reply, :ok, state}
  end

  def handle_info(:cleanup_l1, state) do
    now = System.os_time(:millisecond)
    ms = :ets.fun2ms(fn {_k, _v, expires_at} when expires_at =< now -> true end)
    deleted = :ets.select_delete(@l1_table, ms)

    if deleted > 0, do: IO.puts("L1 cleanup: removed #{deleted} expired entries")

    Process.send_after(self(), :cleanup_l1, @cleanup_interval_ms)
    {:noreply, state}
  end

  def terminate(_reason, state) do
    :dets.sync(state.dets)
    :dets.close(state.dets)
  end
end

# Demo
{:ok, _} = PersistentCache.start_link()

PersistentCache.put("user:1:profile", %{name: "Alice", age: 30}, 60_000)
PersistentCache.put("user:2:profile", %{name: "Bob", age: 25}, 5_000)

IO.inspect(PersistentCache.get("user:1:profile"), label: "L1 hit")
IO.inspect(PersistentCache.get("user:99:profile"), label: "Miss")

PersistentCache.invalidate("user:1:profile")
IO.inspect(PersistentCache.get("user:1:profile"), label: "After invalidation")
# Expected: :miss

GenServer.stop(PersistentCache)

# Restart: user:1 no existe (expiró o fue invalidado), user:2 puede aún estar
{:ok, _} = PersistentCache.start_link()
IO.inspect(PersistentCache.get("user:2:profile"), label: "After restart (L2 hit if not expired)")
```

**Trade-off analysis**: El patrón L1/L2 añade complejidad de doble escritura y posible inconsistencia temporal (L1 puede tener un valor que ya fue invalidado en L2 por otro nodo). Para aplicaciones de un solo nodo, el patrón es sólido. Para multi-nodo, necesitas invalidación distribuida (Phoenix.PubSub o similar).

---

## Common Mistakes

**No cerrar DETS antes de que el proceso muera**
Si el proceso propietario muere sin llamar `close/1`, DETS marca el archivo como "necesita reparación". En el siguiente arranque, la reparación puede tardar varios segundos para tablas grandes. Siempre implementa `terminate/2` en el GenServer que posee DETS.

**Usar DETS para escrituras de alta frecuencia**
DETS hace flush a disco y esto implica syscalls. Para más de ~1000 escrituras/segundo, DETS se convierte en un cuello de botella severo. En ese caso considera escribir en batches o usar `auto_save` con sync manual solo en momentos críticos.

**Asumir que `insert/2` garantiza persistencia**
La escritura va a un buffer en memoria primero. Solo `sync/1` o el proceso de `auto_save` la escribe a disco. Si el nodo cae entre `insert` y `sync`, la entrada se pierde. Para auditoría o datos críticos, llama `sync/1` explícitamente.

**Abrir la misma tabla DETS desde múltiples procesos**
DETS tiene un lock de file-level. Abrir el mismo archivo desde dos procesos puede causar corrupción o errores inesperados. El propietario debe ser un único proceso. Para acceso concurrente, el GenServer propietario serializa los accesos.

**Usar DETS cuando el dataset supera 2GB**
DETS tiene un límite de 2GB. Cerca del límite, las operaciones se ralentizan. Planifica la migración a Mnesia o PostgreSQL bien antes de llegar al límite.

---

## Verification

```elixir
# Verificar persistencia entre reinicios
{:ok, _} = ConfigStore.start_link(path: "/tmp/test_config.dets")
ConfigStore.put(:test, :value, 42)
GenServer.stop(ConfigStore)

{:ok, _} = ConfigStore.start_link(path: "/tmp/test_config.dets")
{:ok, 42} = ConfigStore.get(:test, :value)
IO.puts("Persistence OK")
GenServer.stop(ConfigStore)

# Limpiar archivo de test
File.rm("/tmp/test_config.dets")
```

```elixir
# Verificar comportamiento de reparación
# (Requiere crear un archivo DETS y matar el proceso abruptamente)
spawn(fn ->
  {:ok, _} = :dets.open_file(:crash_test, [file: ~c"/tmp/crash_test.dets", type: :set])
  :dets.insert(:crash_test, {:key, :value})
  # No llamamos close — simula crash
  Process.exit(self(), :kill)
end)

Process.sleep(100)

# Al reabrir, DETS debe reparar automáticamente (verás un warning en el log)
{:ok, ref} = :dets.open_file(:crash_test, [file: ~c"/tmp/crash_test.dets", type: :set])
IO.inspect(:dets.lookup(ref, :key), label: "after repair")
:dets.close(ref)
File.rm("/tmp/crash_test.dets")
```

---

## Summary

DETS es la opción de persistencia más simple en el ecosistema Erlang/Elixir: cero dependencias externas, API casi idéntica a ETS, y crash recovery automático. Sus limitaciones (2GB, sin `:ordered_set`, concurrencia de escritura limitada) la hacen inadecuada como base de datos principal, pero perfecta para configuración persistida, caches durables, y logs de tamaño moderado.

El patrón más robusto en producción combina ETS (velocidad) + DETS (durabilidad): lecturas en nanosegundos, escrituras aseguradas, y un GenServer como punto único de coordinación.

---

## What's Next

- **Ejercicio 18**: Mnesia — persistencia con transacciones ACID, replicación y queries complejas
- **Ejercicio 19**: Cache patterns con ETS — TTL, LRU, stampede prevention
- PostgreSQL via Ecto para datos relacionales y queries complejas a escala

---

## Resources

- [Erlang DETS documentation](https://www.erlang.org/doc/man/dets.html) — referencia completa de la API
- [DETS vs ETS comparison](https://www.erlang.org/doc/efficiency_guide/tablesDatabases.html) — guía de eficiencia oficial
- [Erlang in Anger — Chapter on introspection](https://www.erlang-in-anger.com/) — sección sobre storage en producción
