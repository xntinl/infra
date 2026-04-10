# =============================================================================
# Ejercicio 19: Patrones de Caché de Producción con ETS
# Difficulty: Avanzado
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - ETS avanzado (Ejercicio 16): :read_concurrency, match specs
# - GenServer intermedio: handle_cast, handle_info, Process.send_after
# - Task.async_stream para concurrencia
# - Conceptos de cache: hit/miss, TTL, eviction policies

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Implementar read-through cache con TTL usando ETS + Process.send_after
# 2. Prevenir cache stampede con el patrón single-flight
# 3. Implementar LRU eviction con :ordered_set
# 4. Evaluar trade-offs entre patrones de cache para diferentes cargas
# 5. Identificar y corregir race conditions sutiles en cache concurrente

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# PATRONES DE CACHE EN PRODUCCIÓN:
#
# READ-THROUGH:
#   El cliente siempre va al cache. Si hay miss, el cache mismo va a la fuente
#   y almacena el resultado antes de retornar. El cliente nunca accede a la
#   fuente directamente.
#
#   Ventaja: la lógica de fetch está centralizada en el cache.
#   Desventaja: el primer request después de TTL es siempre lento.
#
# WRITE-THROUGH:
#   Toda escritura va simultáneamente al cache Y a la fuente.
#   El cache nunca tiene datos stale porque se actualiza en cada write.
#
#   Ventaja: lectura siempre fast, consistencia fuerte.
#   Desventaja: latencia de escritura = latencia de la fuente.
#
# WRITE-BEHIND (Write-Back):
#   Escritura va primero al cache, luego async a la fuente.
#   El cliente no espera el write a disco/DB.
#
#   Ventaja: writes ultra-rápidos desde perspectiva del cliente.
#   Desventaja: ventana de pérdida de datos (cache crash antes del flush).
#
# TTL (Time-To-Live):
#   Cada entrada tiene un timestamp de expiración.
#   Dos estrategias de cleanup:
#   a) Lazy: verificar TTL en cada read → simpler, datos stale persisten en memoria
#   b) Active: timer que borra entries expiradas → más complejo, mejor para memoria
#
# CACHE STAMPEDE (Thundering Herd):
#   Cuando una entrada popular expira, muchos requests simultáneos detectan el
#   miss y TODOS van a la fuente de datos al mismo tiempo.
#   Con 1000 usuarios concurrentes, esto es 1000 queries a DB simultáneos.
#
#   Solución: Single-Flight Pattern
#   Solo UN request va a la fuente. Los demás esperan el resultado del primero.
#   Implementación: usar un proceso/Agent para trackear "fetches en vuelo".
#
# LRU EVICTION (Least Recently Used):
#   Cuando el cache alcanza su capacidad máxima, elimina el entry que
#   fue accedido hace más tiempo.
#
#   Implementación con ETS ordered_set:
#   - Key compuesta: {timestamp, original_key}
#   - En cada GET: actualizar timestamp (delete + re-insert con nuevo ts)
#   - Al insert: si size >= MAX, borrar el entry con menor timestamp
#     (el primero en el ordered_set, ya que ordena por key)

# =============================================================================
# Simulador de fuente de datos (DB simulada)
# =============================================================================

defmodule FakeDB do
  @doc """
  Simula una consulta lenta a base de datos.
  Retorna {:ok, value} después de un delay aleatorio.
  """
  def fetch(key) do
    # Simular latencia de DB: 50-150ms
    Process.sleep(Enum.random(50..150))
    {:ok, "db_value_for_#{key}_at_#{System.monotonic_time(:millisecond)}"}
  end

  @doc """
  Versión sin delay para tests de correctitud (no de latencia).
  """
  def fetch_instant(key) do
    {:ok, "value_for_#{key}"}
  end
end

# =============================================================================
# Exercise 1: Read-Through Cache con TTL
# =============================================================================
#
# Implementa `TTLCache` — un GenServer que actúa como cache read-through
# con expiración automática de entradas.
#
# Estado interno del GenServer:
#   %{
#     table: reference(),        # ETS table ref
#     ttl_ms: integer(),         # TTL en ms para todas las entradas
#     fetch_fn: (key -> {:ok, value} | {:error, reason})  # función para obtener datos
#   }
#
# Schema de ETS: {key, value, expires_at}
# expires_at = System.monotonic_time(:millisecond) + ttl_ms
#
# Funciones a implementar:
#
# 1. `start_link(opts)` — inicia el GenServer.
#    opts: [ttl_ms: 5000, fetch_fn: &FakeDB.fetch_instant/1]
#    Crea tabla ETS: [:named_table, :public, :set, :read_concurrency]
#
# 2. `get(server, key)` — obtiene el valor:
#    a) Busca en ETS
#    b) Si existe Y no expiró → retorna {:ok, value, :hit}
#    c) Si expiró O no existe → llama a fetch_fn, almacena con TTL, retorna {:ok, value, :miss}
#    d) Si fetch_fn falla → retorna {:error, reason}
#    El GET debe ser lo más rápido posible: leer ETS sin pasar por GenServer.
#    Solo ir al GenServer cuando hay un miss (para el fetch y el store).
#    Hint: hacer el lookup ETS directamente en el proceso del caller.
#
# 3. `put(server, key, value)` — insert manual sin fetch.
#    Almacena con TTL. Útil para pre-popular el cache.
#
# 4. `invalidate(server, key)` — elimina una entrada del cache.
#
# 5. `purge_expired(server)` — elimina todos los entries expirados.
#    Llamar desde handle_info({:purge, ...}) con Process.send_after periódico.
#    Usar :ets.select_delete con match spec que compare expires_at < now.
#    Retorna el número de entries eliminados.
#
# 6. `stats(server)` — retorna %{size: n, hits: n, misses: n}
#    Trackear hits/misses en el estado del GenServer.
#
# CRÍTICO — Separación de lectura:
#   El path de lectura (get) NO debe pasar por el GenServer si hay hit.
#   Usar :ets.lookup directamente en el proceso del caller.
#   Solo el fetch (miss) y el store van a través del GenServer.
#   Esto previene que el GenServer sea un bottleneck en alta concurrencia.
#
# Ejemplo de flujo get/1:
#   now = System.monotonic_time(:millisecond)
#   case :ets.lookup(:ttl_cache, key) do
#     [{^key, value, expires_at}] when expires_at > now ->
#       # HIT: retornar directamente, no tocar el GenServer
#       {:ok, value, :hit}
#     _ ->
#       # MISS: delegar al GenServer para fetch+store
#       GenServer.call(server, {:fetch_and_store, key})
#   end

# TODO: Implementa TTLCache como GenServer
# defmodule TTLCache do
#   use GenServer
#
#   @table :ttl_cache
#   @purge_interval_ms 10_000
#
#   def start_link(opts) do
#     GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
#   end
#
#   def get(server, key) do
#     now = System.monotonic_time(:millisecond)
#     case :ets.lookup(@table, key) do
#       [{^key, value, expires_at}] when expires_at > now ->
#         GenServer.cast(server, :increment_hits)
#         {:ok, value, :hit}
#       _ ->
#         GenServer.call(server, {:fetch_and_store, key}, 10_000)
#     end
#   end
#
#   def put(server, key, value) do
#     GenServer.call(server, {:store, key, value})
#   end
#
#   def invalidate(server, key) do
#     :ets.delete(@table, key)
#   end
#
#   def purge_expired(server) do
#     GenServer.call(server, :purge_expired)
#   end
#
#   def stats(server) do
#     GenServer.call(server, :stats)
#   end
#
#   # --- GenServer callbacks ---
#
#   @impl true
#   def init(opts) do
#     :ets.new(@table, [:named_table, :public, :set, :read_concurrency])
#     schedule_purge()
#     {:ok, %{
#       ttl_ms: Keyword.get(opts, :ttl_ms, 5_000),
#       fetch_fn: Keyword.get(opts, :fetch_fn, &FakeDB.fetch_instant/1),
#       hits: 0,
#       misses: 0
#     }}
#   end
#
#   @impl true
#   def handle_call({:fetch_and_store, key}, _from, state) do
#     case state.fetch_fn.(key) do
#       {:ok, value} ->
#         expires_at = System.monotonic_time(:millisecond) + state.ttl_ms
#         :ets.insert(@table, {key, value, expires_at})
#         {:reply, {:ok, value, :miss}, %{state | misses: state.misses + 1}}
#       {:error, reason} ->
#         {:reply, {:error, reason}, state}
#     end
#   end
#
#   @impl true
#   def handle_call({:store, key, value}, _from, state) do
#     expires_at = System.monotonic_time(:millisecond) + state.ttl_ms
#     :ets.insert(@table, {key, value, expires_at})
#     {:reply, :ok, state}
#   end
#
#   @impl true
#   def handle_call(:purge_expired, _from, state) do
#     now = System.monotonic_time(:millisecond)
#     match_spec = [{{:"$1", :"$2", :"$3"}, [{:<, :"$3", now}], [true]}]
#     n = :ets.select_delete(@table, match_spec)
#     {:reply, n, state}
#   end
#
#   @impl true
#   def handle_call(:stats, _from, state) do
#     stats = %{
#       size: :ets.info(@table, :size),
#       hits: state.hits,
#       misses: state.misses
#     }
#     {:reply, stats, state}
#   end
#
#   @impl true
#   def handle_cast(:increment_hits, state) do
#     {:noreply, %{state | hits: state.hits + 1}}
#   end
#
#   @impl true
#   def handle_info(:purge, state) do
#     now = System.monotonic_time(:millisecond)
#     match_spec = [{{:"$1", :"$2", :"$3"}, [{:<, :"$3", now}], [true]}]
#     :ets.select_delete(@table, match_spec)
#     schedule_purge()
#     {:noreply, state}
#   end
#
#   defp schedule_purge do
#     Process.send_after(self(), :purge, @purge_interval_ms)
#   end
# end

# =============================================================================
# Exercise 2: Single-Flight Pattern (Cache Stampede Prevention)
# =============================================================================
#
# Implementa `SingleFlightCache` — cache que previene el thundering herd.
#
# El problema:
#   Si 1000 procesos piden el mismo key que está expirado:
#   - Sin single-flight: 1000 queries a DB simultáneas
#   - Con single-flight: 1 query a DB, 999 procesos esperan el resultado
#
# Implementación:
#   Usar un Agent (o GenServer) para trackear "fetches en vuelo":
#     %{key => {:pending, [waiting_pids]}} cuando hay un fetch en progreso
#
#   Flujo:
#   1. Proceso A hace get(:my_key) → miss
#   2. A registra :my_key como :pending en el Agent
#   3. A lanza un Task para hacer el fetch real
#   4. Proceso B hace get(:my_key) → miss
#   5. B encuentra :my_key como :pending → B se agrega a waiting_pids
#   6. B se bloquea (espera mensaje o retries)
#   7. El Task de A termina → resultado llega al Agent
#   8. El Agent notifica a todos los waiting_pids
#   9. A Y todos los B reciben el resultado
#
# Módulo a implementar:
#
# 1. `start_link/1` — inicia el SingleFlightCache.
#    Crea ETS para el cache y un Agent para trackear fetches en vuelo.
#    opts: [ttl_ms: 5000, fetch_fn: fun]
#
# 2. `get(server, key)` — obtiene el valor con single-flight:
#    a) Check ETS directo (como TTLCache)
#    b) Si miss: usar GenServer.call para coordinar fetch único
#    c) Si ya hay fetch en vuelo para ese key: esperar resultado
#    d) Si no hay fetch en vuelo: lanzar Task, registrar :pending
#    Retorna {:ok, value} | {:error, reason}
#
# 3. `handle_call({:get_or_fetch, key}, from, state)` — el corazón del single-flight:
#    - Si key está en inflight: agregar `from` a la lista de waiters
#      (GenServer responderá a todos cuando el fetch complete)
#    - Si key NO está en inflight: registrar el fetch, lanzar Task async
#    No reply inmediato — usar {:noreply, state} y responder en handle_info
#
# 4. `handle_info({:fetch_complete, key, result}, state)`:
#    - Almacenar resultado en ETS
#    - Responder a TODOS los waiters: Enum.each(waiters, &GenServer.reply(&1, result))
#    - Limpiar el estado inflight para ese key
#
# Trade-off del single-flight:
#   + Elimina thundering herd completamente
#   + N procesos esperando = 1 query a DB (sin importar N)
#   - Latencia del primer waiter = latencia del fetch completo
#   - Si el fetch falla, TODOS los waiters reciben el error
#   - Complejidad de implementación mayor que cache simple

# TODO: Implementa SingleFlightCache
# defmodule SingleFlightCache do
#   use GenServer
#
#   @table :sf_cache
#   @ttl_ms 5_000
#
#   def start_link(opts) do
#     GenServer.start_link(__MODULE__, opts, name: __MODULE__)
#   end
#
#   def get(key) do
#     now = System.monotonic_time(:millisecond)
#     case :ets.lookup(@table, key) do
#       [{^key, value, expires_at}] when expires_at > now ->
#         {:ok, value}
#       _ ->
#         GenServer.call(__MODULE__, {:get_or_fetch, key}, 15_000)
#     end
#   end
#
#   @impl true
#   def init(opts) do
#     :ets.new(@table, [:named_table, :public, :set, :read_concurrency])
#     {:ok, %{
#       inflight: %{},  # key => [from, ...]
#       ttl_ms: Keyword.get(opts, :ttl_ms, @ttl_ms),
#       fetch_fn: Keyword.get(opts, :fetch_fn, &FakeDB.fetch_instant/1)
#     }}
#   end
#
#   @impl true
#   def handle_call({:get_or_fetch, key}, from, state) do
#     case Map.get(state.inflight, key) do
#       nil ->
#         # Nadie está fetcheando este key aún — somos los primeros
#         fetch_fn = state.fetch_fn
#         server = self()
#         Task.start(fn ->
#           result = fetch_fn.(key)
#           send(server, {:fetch_complete, key, result})
#         end)
#         new_inflight = Map.put(state.inflight, key, [from])
#         {:noreply, %{state | inflight: new_inflight}}
#
#       waiters ->
#         # Alguien ya está fetcheando — nos unimos a la cola de espera
#         new_inflight = Map.put(state.inflight, key, [from | waiters])
#         {:noreply, %{state | inflight: new_inflight}}
#     end
#   end
#
#   @impl true
#   def handle_info({:fetch_complete, key, result}, state) do
#     waiters = Map.get(state.inflight, key, [])
#
#     # Almacenar en cache si fue exitoso
#     case result do
#       {:ok, value} ->
#         expires_at = System.monotonic_time(:millisecond) + state.ttl_ms
#         :ets.insert(@table, {key, value, expires_at})
#         Enum.each(waiters, &GenServer.reply(&1, {:ok, value}))
#       {:error, _reason} = error ->
#         Enum.each(waiters, &GenServer.reply(&1, error))
#     end
#
#     {:noreply, %{state | inflight: Map.delete(state.inflight, key)}}
#   end
# end

# =============================================================================
# Exercise 3: LRU Cache con :ordered_set
# =============================================================================
#
# Implementa `LRUCache` — cache con eviction de entradas menos usadas.
#
# Dos tablas ETS:
#   :lru_data   → {:set}         → {key, value}  (datos principales)
#   :lru_order  → {:ordered_set} → {timestamp, key}  (orden de acceso)
#
# Cuando lru_data tiene MAX entries y hay un nuevo insert:
#   1. Encontrar el entry más antiguo: :ets.first(:lru_order) → {min_ts, old_key}
#   2. Borrar de :lru_data el old_key
#   3. Borrar de :lru_order la entrada {min_ts, old_key}
#   4. Insertar el nuevo entry en ambas tablas
#
# En cada GET que sea hit:
#   1. Borrar la entrada antigua en :lru_order con el timestamp viejo
#   2. Insertar nueva entrada en :lru_order con timestamp actual
#   (El entry en :lru_data no cambia, solo el orden)
#
# Problema de race condition:
#   Si dos procesos leen el mismo key simultáneamente, ambos intentan
#   actualizar :lru_order. Esto puede dejar entradas huérfanas en :lru_order.
#   Para evitarlo, delegar los updates de :lru_order al GenServer.
#
# Funciones a implementar:
#
# 1. `start_link(max_size)` — inicia LRUCache con capacidad máxima.
#
# 2. `get(server, key)` — retorna {:ok, value} | :miss.
#    Actualiza el orden de acceso (fresh timestamp) si hay hit.
#
# 3. `put(server, key, value)` — inserta un entry.
#    Si size >= max_size, evict el LRU entry.
#    Si el key ya existe, actualiza sin eviction.
#
# 4. `size(server)` — retorna el número de entries en el cache.
#
# 5. `evict_lru(state)` — función privada que elimina el entry menos usado.
#    Usa :ets.first(:lru_order) para encontrar el más antiguo.
#    Retorna el key evictado.
#
# Timestamp para ordering:
#   Usar System.monotonic_time(:nanosecond) para evitar colisiones.
#   Dos inserts en el mismo millisecond tendrían el mismo timestamp.
#   Nanoseconds reduce colisiones pero no las elimina completamente.
#   Para un LRU correcto en producción, incluir un counter monotónico.
#
# Alternativa: ETS con timestamp como parte del key compuesto:
#   {:lru_order} con key {timestamp, key} garantiza unicidad si usamos
#   un counter monotónico: {System.monotonic_time(), make_ref()}

# TODO: Implementa LRUCache
# defmodule LRUCache do
#   use GenServer
#
#   @data_table :lru_data
#   @order_table :lru_order
#
#   def start_link(max_size) do
#     GenServer.start_link(__MODULE__, max_size, name: __MODULE__)
#   end
#
#   def get(server, key) do
#     case :ets.lookup(@data_table, key) do
#       [] ->
#         :miss
#       [{^key, value, old_ts}] ->
#         # Actualizar orden — delegamos al GenServer para evitar race conditions
#         GenServer.cast(server, {:update_order, key, old_ts})
#         {:ok, value}
#     end
#   end
#
#   def put(server, key, value) do
#     GenServer.call(server, {:put, key, value})
#   end
#
#   def size(_server) do
#     :ets.info(@data_table, :size)
#   end
#
#   @impl true
#   def init(max_size) do
#     :ets.new(@data_table, [:named_table, :public, :set])
#     :ets.new(@order_table, [:named_table, :public, :ordered_set])
#     {:ok, %{max_size: max_size}}
#   end
#
#   @impl true
#   def handle_call({:put, key, value}, _from, state) do
#     ts = System.monotonic_time(:nanosecond)
#
#     # Si ya existe, limpiar entrada antigua en :lru_order
#     case :ets.lookup(@data_table, key) do
#       [{^key, _old_val, old_ts}] ->
#         :ets.delete(@order_table, old_ts)
#       [] ->
#         # Nuevo entry — verificar si hay que hacer eviction
#         if :ets.info(@data_table, :size) >= state.max_size do
#           evict_lru()
#         end
#     end
#
#     :ets.insert(@data_table, {key, value, ts})
#     :ets.insert(@order_table, {ts, key})
#     {:reply, :ok, state}
#   end
#
#   @impl true
#   def handle_cast({:update_order, key, old_ts}, state) do
#     new_ts = System.monotonic_time(:nanosecond)
#     :ets.delete(@order_table, old_ts)
#     :ets.insert(@order_table, {new_ts, key})
#     # Actualizar el timestamp en :lru_data también
#     case :ets.lookup(@data_table, key) do
#       [{^key, value, _}] -> :ets.insert(@data_table, {key, value, new_ts})
#       [] -> :ok
#     end
#     {:noreply, state}
#   end
#
#   defp evict_lru do
#     case :ets.first(@order_table) do
#       :"$end_of_table" ->
#         nil
#       {_ts, key} = order_key ->
#         :ets.delete(@order_table, order_key)
#         :ets.delete(@data_table, key)
#         key
#     end
#   end
# end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule CachePatternTests do
  def run do
    IO.puts("\n=== Verificación: Patrones de Caché con ETS ===\n")

    test_ttl_cache()
    test_single_flight()
    test_lru_cache()

    IO.puts("\n=== Verificación completada ===")
  end

  defp test_ttl_cache do
    IO.puts("--- Exercise 1: Read-Through Cache con TTL ---")

    if :ets.whereis(:ttl_cache) != :undefined, do: :ets.delete(:ttl_cache)

    {:ok, server} = TTLCache.start_link(
      ttl_ms: 200,
      fetch_fn: &FakeDB.fetch_instant/1,
      name: :test_ttl_cache
    )

    # Hit después de put manual
    TTLCache.put(server, :product_1, "Widget")
    check("get hit manual", match?({:ok, "Widget", :hit}, TTLCache.get(server, :product_1)), true)

    # Miss y fetch
    {_status, _value, hit_or_miss} = TTLCache.get(server, :new_key)
    check("get miss → fetch", hit_or_miss, :miss)

    # Segundo get debe ser hit
    {_status, _value2, hit_or_miss2} = TTLCache.get(server, :new_key)
    check("segundo get → hit", hit_or_miss2, :hit)

    # Esperar TTL y verificar expiración
    Process.sleep(250)
    {_s, _v, hit_or_miss3} = TTLCache.get(server, :new_key)
    check("después de TTL → miss", hit_or_miss3, :miss)

    # Purge manual
    TTLCache.put(server, :to_purge, "temp")
    Process.sleep(250)
    n_purged = TTLCache.purge_expired(server)
    check("purge_expired > 0", n_purged > 0, true)

    stats = TTLCache.stats(server)
    check("stats tiene keys", Map.has_key?(stats, :size), true)

    GenServer.stop(server)
  end

  defp test_single_flight do
    IO.puts("\n--- Exercise 2: Single-Flight (Stampede Prevention) ---")

    if :ets.whereis(:sf_cache) != :undefined, do: :ets.delete(:sf_cache)

    # Fetch contador para verificar que solo se llama una vez
    counter_agent = Agent.start_link(fn -> 0 end) |> elem(1)

    fetch_fn = fn key ->
      Agent.update(counter_agent, & &1 + 1)
      Process.sleep(100)  # simular latencia
      {:ok, "value_for_#{key}"}
    end

    {:ok, _} = SingleFlightCache.start_link(fetch_fn: fetch_fn, name: SingleFlightCache)

    # 10 procesos piden el mismo key simultáneamente
    tasks = Enum.map(1..10, fn _ ->
      Task.async(fn -> SingleFlightCache.get(:hot_key) end)
    end)
    results = Task.await_many(tasks, 5_000)

    fetch_count = Agent.get(counter_agent, & &1)
    check("single-flight: solo 1 fetch para 10 requests", fetch_count, 1)

    all_ok = Enum.all?(results, fn {:ok, _} -> true; _ -> false end)
    check("todos los waiters reciben resultado", all_ok, true)

    # El segundo batch debería ser hit (ya en cache)
    {:ok, _} = SingleFlightCache.get(:hot_key)
    final_count = Agent.get(counter_agent, & &1)
    check("segundo get es cache hit (no fetch extra)", final_count, 1)

    Agent.stop(counter_agent)
    GenServer.stop(SingleFlightCache)
  end

  defp test_lru_cache do
    IO.puts("\n--- Exercise 3: LRU Cache ---")

    if :ets.whereis(:lru_data) != :undefined, do: :ets.delete(:lru_data)
    if :ets.whereis(:lru_order) != :undefined, do: :ets.delete(:lru_order)

    {:ok, server} = LRUCache.start_link(3)

    :ok = LRUCache.put(server, :a, "alpha")
    :ok = LRUCache.put(server, :b, "beta")
    :ok = LRUCache.put(server, :c, "gamma")
    check("size = 3", LRUCache.size(server), 3)

    # Acceder a :a para que sea el más reciente
    {:ok, "alpha"} = LRUCache.get(server, :a)

    # Insertar :d — debe evictar :b (el más antiguo no accedido)
    :ok = LRUCache.put(server, :d, "delta")
    check("size sigue en 3 tras eviction", LRUCache.size(server), 3)
    check("evictado el LRU (:b)", LRUCache.get(server, :b), :miss)
    check(":a sigue (fue accedido recientemente)", LRUCache.get(server, :a), {:ok, "alpha"})
    check(":d existe", LRUCache.get(server, :d), {:ok, "delta"})

    # Update de key existente no debe hacer eviction
    :ok = LRUCache.put(server, :c, "GAMMA_UPDATED")
    check("update no evicta", LRUCache.size(server), 3)
    check("valor actualizado", LRUCache.get(server, :c), {:ok, "GAMMA_UPDATED"})

    GenServer.stop(server)
  end

  defp check(label, actual, expected) do
    if actual == expected do
      IO.puts("  ✓ #{label}")
    else
      IO.puts("  ✗ #{label}")
      IO.puts("    Esperado: #{inspect(expected)}")
      IO.puts("    Obtenido: #{inspect(actual)}")
    end
  end
end

# =============================================================================
# Common Mistakes
# =============================================================================
#
# ERROR 1: El path de lectura (GET hit) pasa por el GenServer
#
#   def get(key), do: GenServer.call(__MODULE__, {:get, key})
#   # ↑ El GenServer es un cuello de botella — serializa todas las lecturas
#   # Con 10k req/s y un GenServer lento, la cola crece sin límite
#
#   Solución: leer ETS directamente en el proceso caller. Solo ir al GenServer
#   para el fetch en miss (que es el camino lento de todos modos).
#
# ERROR 2: Race condition en TTL check + store
#
#   Si dos procesos detectan miss simultáneamente, ambos pueden llegar al
#   GenServer y disparar dos fetches. Sin single-flight, esto es correcto
#   (el segundo write simplemente sobreescribe el primero).
#   Con single-flight, solo uno debe hacer el fetch.
#
# ERROR 3: LRU con timestamps colisionando
#
#   Si dos inserts ocurren en el mismo nanosegundo (posible en multi-core),
#   el second insert sobreescribe el primero en :lru_order.
#   Solución: usar {monotonic_time, make_ref()} como timestamp compuesto.
#
# ERROR 4: No limpiar :lru_order al evictar en LRU
#
#   Al evictar de :lru_data, olvidar borrar la entrada correspondiente
#   en :lru_order deja entradas huérfanas que eventualmente llenan la tabla.
#   Siempre borrar de AMBAS tablas en cada operación.
#
# ERROR 5: GenServer.call timeout en single-flight con fetch lento
#
#   Si el fetch tarda 30s y el call timeout es 5s (default), los waiters
#   reciben timeout pero el fetch continúa. Al completar, intenta responder
#   a pids que ya no están esperando → crash.
#   Solución: usar timeout grande en GenServer.call o manejar el stale waiter.

# =============================================================================
# One Possible Solution (sparse)
# =============================================================================
#
# TTLCache.get/2 — path fast (sin GenServer):
#   now = System.monotonic_time(:millisecond)
#   case :ets.lookup(:ttl_cache, key) do
#     [{^key, value, expires_at}] when expires_at > now ->
#       {:ok, value, :hit}
#     _ ->
#       GenServer.call(server, {:fetch_and_store, key}, 10_000)
#   end
#
# SingleFlightCache.handle_info({:fetch_complete, key, result}, state):
#   waiters = Map.get(state.inflight, key, [])
#   case result do
#     {:ok, value} ->
#       expires_at = System.monotonic_time(:millisecond) + state.ttl_ms
#       :ets.insert(@table, {key, value, expires_at})
#       Enum.each(waiters, &GenServer.reply(&1, {:ok, value}))
#     error ->
#       Enum.each(waiters, &GenServer.reply(&1, error))
#   end
#   {:noreply, %{state | inflight: Map.delete(state.inflight, key)}}
#
# LRUCache.evict_lru (con :ets.first sobre ordered_set):
#   case :ets.first(@order_table) do
#     :"$end_of_table" -> nil
#     {_ts, key} = order_key ->
#       :ets.delete(@order_table, order_key)
#       :ets.delete(@data_table, key)
#       key
#   end

# =============================================================================
# Trade-offs: Patrones de Cache
# =============================================================================
#
# | Patrón          | Consistencia | Latencia Write | Latencia Read | Complejidad |
# |-----------------|-------------|----------------|---------------|-------------|
# | Read-Through    | Eventual    | N/A            | Alta (miss)   | Baja        |
# | Write-Through   | Fuerte      | Alta           | Baja          | Media       |
# | Write-Behind    | Eventual    | Muy Baja       | Baja          | Alta        |
# | Read-Through+TTL| Temporal    | N/A            | Configurable  | Media       |
# | Single-Flight   | Eventual    | N/A            | Alta (miss×1) | Alta        |
# | LRU             | N/A         | Baja           | Baja          | Media       |

# =============================================================================
# Summary
# =============================================================================
#
# Los patrones de cache de producción resuelven problemas específicos:
#
# - TTL: datos que se vuelven stale con el tiempo (precios, sesiones)
# - Single-Flight: recursos costosos de computar con alta demanda concurrente
# - LRU: memoria limitada con acceso frecuente a un subconjunto de datos
# - Write-Through: consistencia fuerte cuando reads >> writes
# - Write-Behind: throughput máximo de escritura cuando pérdida temporal es aceptable

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 20: :atomics y :counters para métricas de alta frecuencia
# - Explorar: Cachex — biblioteca de cache para Elixir con todos estos patrones
# - Explorar: Nebulex — cache distribuida para Elixir con adaptadores

# =============================================================================
# Resources
# =============================================================================
# - https://hexdocs.pm/cachex/Cachex.html
# - https://github.com/nyrissa/nebulex
# - https://www.youtube.com/watch?v=v_uGMNnqHiE — ElixirConf: Caching strategies
# - Designing Data-Intensive Applications — Cap. 5: Replication (cache consistency)

CachePatternTests.run()
