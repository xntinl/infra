# =============================================================================
# Ejercicio 20: Contadores Atómicos de Alta Performance
# Difficulty: Avanzado
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - ETS avanzado (Ejercicio 16): :write_concurrency, partitioned ETS
# - Concurrencia BEAM: schedulers, Task.async_stream
# - Conceptos de operaciones atómicas y memory ordering
# - :erlang.system_info/1 para información del runtime

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Usar :ets.update_counter/3 para incrementos atómicos sin locks explícitos
# 2. Implementar métricas multi-dimensionales con :atomics
# 3. Usar :counters para contadores que escalan linealmente con schedulers
# 4. Diseñar un sistema de métricas de alta frecuencia (>1M ops/s)
# 5. Comparar trade-offs entre ETS, :atomics, y :counters para casos concretos

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# EL PROBLEMA DE LOS CONTADORES GLOBALES:
#
#   En un sistema con 16 schedulers BEAM y 1M requests/segundo, un contador
#   global compartido se convierte en un cuello de botella:
#   - Agent: serializado (1 proceso → queue de 1M ops/s)
#   - GenServer: igual
#   - ETS sin concurrencia: lock global por operación
#
# SOLUCIÓN 1 — ETS update_counter:
#
#   :ets.update_counter(table, key, {pos, increment})
#
#   Operación atómica provista por el runtime BEAM.
#   NO requiere lock explícito — el scheduler de ETS lo maneja internamente.
#   Mucho más rápido que Agent/GenServer, pero sigue habiendo contención
#   si muchos schedulers escriben la misma key.
#
#   Con :write_concurrency: reduce contención al aplicar lock striping.
#
# SOLUCIÓN 2 — :atomics (OTP 21+):
#
#   ref = :atomics.new(N, [])           # array de N enteros de 64 bits
#   :atomics.add(ref, index, increment) # atómico, sin lock
#   :atomics.get(ref, index)            # lectura atómica
#
#   Ideal para N contadores independientes (métricas por endpoint).
#   Operaciones de hardware-level (CAS — Compare-And-Swap).
#   Más rápido que ETS para writes porque no hay table locks.
#   Sin contención: cada índice es independiente en el array.
#
#   Opciones:
#   - [] (default): unsigned, signed flag false → unsigned 64-bit
#   - [signed: true] → signed 64-bit
#
# SOLUCIÓN 3 — :counters (OTP 23+):
#
#   ref = :counters.new(N, [:write_concurrency])
#   :counters.add(ref, index, increment)
#   :counters.get(ref, index)
#
#   Diseñado específicamente para contadores distribuidos por scheduler.
#   Internamente mantiene UN contador POR SCHEDULER por cada índice.
#   El get/2 suma todos los contadores de todos los schedulers.
#
#   Resultado: CERO contención en writes.
#   Trade-off: get/2 es más lento (suma N schedulers × M índices).
#
# COMPARACIÓN:
#
# | Característica    | ETS update_counter | :atomics         | :counters        |
# |-------------------|--------------------|------------------|------------------|
# | Write throughput  | Alto               | Muy Alto         | Máximo           |
# | Read throughput   | Alto               | Muy Alto         | Medio (suma)     |
# | Contención        | Baja (con stripe)  | Muy Baja (CAS)   | Nula             |
# | N contadores      | Múltiples keys     | Array de N       | Array de N       |
# | Persistencia      | No                 | No               | No               |
# | Distribución      | Un nodo            | Un nodo          | Un nodo          |
# | API               | Key-Value          | Array con índice | Array con índice |
# | Caso ideal        | Cache + contadores | Métricas N-dim   | Rate counters    |
#
# CUÁNDO USAR CADA UNO:
#   ETS update_counter → cuando ya tienes ETS por otras razones (cache+counter)
#   :atomics → métricas independientes por dimensión (por endpoint, por user)
#   :counters → rate limiting, request counters, métricas de alta frecuencia

# =============================================================================
# Exercise 1: ETS Atomic Counter para Requests/Second
# =============================================================================
#
# Implementa `RequestCounter` — un contador de requests atómico con ETS
# que soporta múltiples métricas (total, por método HTTP, por status code).
#
# Schema ETS: {:set} con key = métrica_atom, value = integer
# Tabla: :request_counters con :write_concurrency
#
# 1. `start/0` — crea la tabla ETS e inicializa todos los contadores en 0.
#    Contadores a inicializar:
#    - :total — requests totales
#    - :get, :post, :put, :delete — por método HTTP
#    - :ok_200, :not_found_404, :error_500 — por status
#    Retorna {:ok, table_ref}
#
# 2. `record_request(method, status)` — registra un request.
#    Incrementa atómicamente:
#    - :total en 1
#    - el counter del method en 1 (ej: :get)
#    - el counter del status en 1 (ej: :ok_200)
#    Usar :ets.update_counter(table, key, {2, 1}) — la tupla {pos, increment}
#    indica posición 2 (el value, ya que pos 1 es el key) e incremento 1.
#    NOTA: la tupla almacenada en ETS es {key, count}, entonces pos=2.
#
# 3. `get(metric)` — retorna el valor actual del contador.
#    Retorna integer (0 si no existe).
#
# 4. `reset/0` — resetea todos los contadores a 0.
#    Usar :ets.update_counter(table, key, {2, -current_value}) para cada key.
#    O simplemente hacer delete + insert de cada key en 0.
#
# 5. `snapshot/0` — retorna un mapa con todos los contadores actuales.
#    %{total: 1523, get: 890, post: 633, ...}
#
# 6. `benchmark(n_processes, n_requests_each)` — benchmark de throughput:
#    - Lanza n_processes Tasks concurrentes
#    - Cada Task llama record_request/2 n_requests_each veces
#    - Mide el tiempo total y calcula ops/segundo
#    - Retorna %{total_ops: n, duration_ms: ms, ops_per_second: ops}
#
# Por qué :ets.update_counter es atómico:
#   BEAM implementa update_counter como una operación de hardware usando
#   instrucciones atómicas del CPU (xadd en x86). No hay un mutex Elixir
#   involucrado — la atomicidad viene del silicon.
#   Esto lo hace ~10-50x más rápido que pasar por un Agent/GenServer.

# TODO: Implementa RequestCounter
# defmodule RequestCounter do
#   @table :request_counters
#   @metrics [:total, :get, :post, :put, :delete, :ok_200, :not_found_404, :error_500]
#
#   def start do
#     table = :ets.new(@table, [:named_table, :public, :set, :write_concurrency])
#     Enum.each(@metrics, fn metric ->
#       :ets.insert(@table, {metric, 0})
#     end)
#     {:ok, table}
#   end
#
#   def record_request(method, status) do
#     :ets.update_counter(@table, :total, {2, 1})
#     :ets.update_counter(@table, method, {2, 1})
#     :ets.update_counter(@table, status, {2, 1})
#   end
#
#   def get(metric) do
#     case :ets.lookup(@table, metric) do
#       [{^metric, count}] -> count
#       [] -> 0
#     end
#   end
#
#   def reset do
#     Enum.each(@metrics, fn metric ->
#       :ets.insert(@table, {metric, 0})
#     end)
#   end
#
#   def snapshot do
#     Enum.map(@metrics, fn metric ->
#       {metric, get(metric)}
#     end)
#     |> Map.new()
#   end
#
#   def benchmark(n_processes, n_requests_each) do
#     reset()
#     methods = [:get, :post, :put, :delete]
#     statuses = [:ok_200, :not_found_404, :error_500]
#
#     {micros, _} = :timer.tc(fn ->
#       1..n_processes
#       |> Task.async_stream(fn _ ->
#         Enum.each(1..n_requests_each, fn _ ->
#           method = Enum.random(methods)
#           status = Enum.random(statuses)
#           record_request(method, status)
#         end)
#       end, max_concurrency: n_processes, timeout: 60_000)
#       |> Stream.run()
#     end)
#
#     total_ops = n_processes * n_requests_each
#     duration_ms = div(micros, 1000)
#     ops_per_second = round(total_ops / (micros / 1_000_000))
#
#     %{total_ops: total_ops, duration_ms: duration_ms, ops_per_second: ops_per_second}
#   end
# end

# =============================================================================
# Exercise 2: :atomics para Métricas N-Dimensionales
# =============================================================================
#
# Implementa `EndpointMetrics` — métricas por endpoint usando :atomics.
#
# Tenemos N endpoints y queremos trackear por cada uno:
# - requests_count: total de requests
# - total_latency_ms: suma de latencias (para calcular promedio)
# - error_count: requests con error
#
# Estructura: array de atomics con tamaño N × 3 (3 métricas por endpoint).
# Índice de endpoint E y métrica M → índice en array = (E - 1) * 3 + M
# (atomics usan índices 1-based)
#
# 1. `new(n_endpoints)` — crea un array de atomics para N endpoints.
#    Retorna un "ref" opaco que contiene el atomics ref y n_endpoints.
#    Representar como %{ref: atomics_ref, n_endpoints: n}
#
# 2. `record(metrics_ref, endpoint_idx, latency_ms, is_error?)` — registra.
#    endpoint_idx es 1-based (1 al N).
#    - Incrementa requests_count del endpoint en 1
#    - Suma latency_ms al total_latency_ms
#    - Si is_error?, incrementa error_count en 1
#
# 3. `get_stats(metrics_ref, endpoint_idx)` — retorna stats de un endpoint.
#    Retorna:
#    %{
#      requests: n,
#      avg_latency_ms: float (total_latency / requests, 0.0 si requests == 0),
#      errors: n,
#      error_rate: float (errors / requests, 0.0 si requests == 0)
#    }
#
# 4. `get_all_stats(metrics_ref)` — retorna lista de stats para todos los endpoints.
#    [{endpoint_idx, %{...}}, ...]
#
# 5. `reset(metrics_ref)` — resetea todas las métricas.
#    Usar :atomics.put(ref, index, 0) para cada índice.
#
# Por qué :atomics es mejor que ETS aquí:
#   - ETS tiene overhead de hash table (buscar key en buckets)
#   - :atomics es un array contiguo en memoria — acceso O(1) directo
#   - Las operaciones atómicas de :atomics son instrucciones de hardware puras
#   - Para N << 1000 endpoints, el array cabe en cache L1/L2 del CPU
#     → latencia de acceso < 1ns vs ~50ns para un miss de cache de ETS
#
# Cálculo de índice (1-based):
#   requests_idx(e) = (e - 1) * 3 + 1
#   latency_idx(e) = (e - 1) * 3 + 2
#   errors_idx(e) = (e - 1) * 3 + 3
#   Total elementos: n_endpoints * 3

# TODO: Implementa EndpointMetrics
# defmodule EndpointMetrics do
#   def new(n_endpoints) do
#     # 3 métricas por endpoint: requests, latency, errors
#     ref = :atomics.new(n_endpoints * 3, signed: false)
#     %{ref: ref, n_endpoints: n_endpoints}
#   end
#
#   def record(%{ref: ref}, endpoint_idx, latency_ms, is_error?) do
#     req_idx = (endpoint_idx - 1) * 3 + 1
#     lat_idx = (endpoint_idx - 1) * 3 + 2
#     err_idx = (endpoint_idx - 1) * 3 + 3
#
#     :atomics.add(ref, req_idx, 1)
#     :atomics.add(ref, lat_idx, latency_ms)
#     if is_error?, do: :atomics.add(ref, err_idx, 1)
#   end
#
#   def get_stats(%{ref: ref}, endpoint_idx) do
#     req_idx = (endpoint_idx - 1) * 3 + 1
#     lat_idx = (endpoint_idx - 1) * 3 + 2
#     err_idx = (endpoint_idx - 1) * 3 + 3
#
#     requests = :atomics.get(ref, req_idx)
#     total_latency = :atomics.get(ref, lat_idx)
#     errors = :atomics.get(ref, err_idx)
#
#     avg_latency = if requests > 0, do: total_latency / requests, else: 0.0
#     error_rate = if requests > 0, do: errors / requests, else: 0.0
#
#     %{
#       requests: requests,
#       avg_latency_ms: avg_latency,
#       errors: errors,
#       error_rate: error_rate
#     }
#   end
#
#   def get_all_stats(%{n_endpoints: n} = metrics_ref) do
#     Enum.map(1..n, fn idx ->
#       {idx, get_stats(metrics_ref, idx)}
#     end)
#   end
#
#   def reset(%{ref: ref, n_endpoints: n}) do
#     Enum.each(1..(n * 3), fn idx ->
#       :atomics.put(ref, idx, 0)
#     end)
#   end
# end

# =============================================================================
# Exercise 3: :counters Distribuidos por Scheduler
# =============================================================================
#
# Implementa `RateLimiter` — rate limiter de alta performance usando :counters.
#
# Un rate limiter necesita:
# - Contar requests en una ventana de tiempo
# - Decidir si el request está permitido (rate < limit)
# - Reset periódico del contador
#
# Con :counters, cada scheduler tiene su propio sub-contador.
# El write es zero-contention.
# El read (get_count) suma todos los schedulers — más lento pero raro.
#
# Implementación:
#
# 1. `new(requests_per_second, n_dimensions)` — crea el rate limiter.
#    n_dimensions: cuántos rate limiters independientes (ej: uno por endpoint).
#    Usar :counters.new(n_dimensions, [:write_concurrency])
#    Retorna %{ref: counters_ref, limit: rps, n_dims: n, reset_interval_ms: 1000}
#
# 2. `allow?(limiter, dimension_idx)` — verifica si el request está permitido.
#    a) Obtener el count actual: :counters.get(ref, dimension_idx)
#    b) Si count < limit: incrementar y retornar true
#    c) Si count >= limit: retornar false (rate limited)
#    Retorna true | false
#    NOTA: hay una race condition sutil aquí — el check-then-act no es atómico.
#    Para un rate limiter aproximado (como los de métricas), esto es aceptable.
#    Para un rate limiter exacto, usar :atomics con CAS loop.
#
# 3. `get_count(limiter, dimension_idx)` — retorna el count actual.
#    Retorna integer
#
# 4. `reset(limiter, dimension_idx)` — resetea el contador de esa dimensión.
#    Usar :counters.put(ref, dimension_idx, 0)
#
# 5. `reset_all(limiter)` — resetea todos los contadores.
#
# 6. `benchmark_vs_ets(n_ops)` — compara throughput de :counters vs ETS:
#    a) Crear un ETS counter (1 key: :count)
#    b) Crear un :counters con 1 dimensión
#    c) Lanzar System.schedulers_online() Tasks, cada uno hace n_ops/(schedulers) increments
#    d) Medir tiempo para ETS y para :counters
#    Retorna %{ets_ops_per_sec: n, counters_ops_per_sec: n, speedup: float}
#
# Por qué :counters escala linealmente:
#   Internamente, :counters.new(N, [:write_concurrency]) crea una estructura donde
#   cada scheduler tiene su propia copia del array de N contadores.
#   Cuando el scheduler S incrementa la dimensión D, actualiza su copia local.
#   Cuando se lee con :counters.get(ref, D), se suma la copia de todos los schedulers.
#
#   Resultado: N schedulers × M dimensions = N×M slots independientes.
#   Write: siempre local al scheduler = zero contention.
#   Read: O(N schedulers) — pero se hace raramente.

# TODO: Implementa RateLimiter
# defmodule RateLimiter do
#   def new(requests_per_second, n_dimensions) do
#     ref = :counters.new(n_dimensions, [:write_concurrency])
#     %{
#       ref: ref,
#       limit: requests_per_second,
#       n_dims: n_dimensions,
#       reset_interval_ms: 1_000
#     }
#   end
#
#   def allow?(%{ref: ref, limit: limit}, dimension_idx) do
#     current = :counters.get(ref, dimension_idx)
#     if current < limit do
#       :counters.add(ref, dimension_idx, 1)
#       true
#     else
#       false
#     end
#   end
#
#   def get_count(%{ref: ref}, dimension_idx) do
#     :counters.get(ref, dimension_idx)
#   end
#
#   def reset(%{ref: ref}, dimension_idx) do
#     :counters.put(ref, dimension_idx, 0)
#   end
#
#   def reset_all(%{ref: ref, n_dims: n}) do
#     Enum.each(1..n, fn idx ->
#       :counters.put(ref, idx, 0)
#     end)
#   end
#
#   def benchmark_vs_ets(n_ops) do
#     n_schedulers = System.schedulers_online()
#     ops_per_task = div(n_ops, n_schedulers)
#
#     # Benchmark ETS
#     if :ets.whereis(:bench_counter) == :undefined do
#       :ets.new(:bench_counter, [:named_table, :public, :write_concurrency])
#       :ets.insert(:bench_counter, {:count, 0})
#     end
#     :ets.insert(:bench_counter, {:count, 0})  # reset
#
#     {ets_micros, _} = :timer.tc(fn ->
#       1..n_schedulers
#       |> Task.async_stream(fn _ ->
#         Enum.each(1..ops_per_task, fn _ ->
#           :ets.update_counter(:bench_counter, :count, {2, 1})
#         end)
#       end, max_concurrency: n_schedulers, timeout: 60_000)
#       |> Stream.run()
#     end)
#
#     # Benchmark :counters
#     counter_ref = :counters.new(1, [:write_concurrency])
#
#     {counters_micros, _} = :timer.tc(fn ->
#       1..n_schedulers
#       |> Task.async_stream(fn _ ->
#         Enum.each(1..ops_per_task, fn _ ->
#           :counters.add(counter_ref, 1, 1)
#         end)
#       end, max_concurrency: n_schedulers, timeout: 60_000)
#       |> Stream.run()
#     end)
#
#     ets_ops = round(n_ops / (ets_micros / 1_000_000))
#     counters_ops = round(n_ops / (counters_micros / 1_000_000))
#     speedup = Float.round(counters_ops / ets_ops, 2)
#
#     %{
#       ets_ops_per_sec: ets_ops,
#       counters_ops_per_sec: counters_ops,
#       speedup: speedup
#     }
#   end
# end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule AtomicsCountersTests do
  def run do
    IO.puts("\n=== Verificación: Contadores Atómicos de Alta Performance ===\n")

    test_request_counter()
    test_endpoint_metrics()
    test_rate_limiter()

    IO.puts("\n=== Verificación completada ===")
  end

  defp test_request_counter do
    IO.puts("--- Exercise 1: ETS Atomic Counter (RequestCounter) ---")

    if :ets.whereis(:request_counters) != :undefined do
      :ets.delete(:request_counters)
    end

    {:ok, _} = RequestCounter.start()

    check("inicial total = 0", RequestCounter.get(:total), 0)

    RequestCounter.record_request(:get, :ok_200)
    RequestCounter.record_request(:get, :ok_200)
    RequestCounter.record_request(:post, :error_500)

    check("total = 3", RequestCounter.get(:total), 3)
    check("get = 2", RequestCounter.get(:get), 2)
    check("post = 1", RequestCounter.get(:post), 1)
    check("error_500 = 1", RequestCounter.get(:error_500), 1)

    snap = RequestCounter.snapshot()
    check("snapshot tiene :total", Map.has_key?(snap, :total), true)
    check("snapshot total = 3", snap.total, 3)

    RequestCounter.reset()
    check("reset: total = 0", RequestCounter.get(:total), 0)

    result = RequestCounter.benchmark(20, 10_000)
    IO.puts("  [benchmark] #{result.total_ops} ops en #{result.duration_ms}ms — #{result.ops_per_second} ops/s")
    check("benchmark total_ops correcto", result.total_ops, 200_000)
    check("ops_per_second > 100_000", result.ops_per_second > 100_000, true)
  end

  defp test_endpoint_metrics do
    IO.puts("\n--- Exercise 2: :atomics — EndpointMetrics ---")

    metrics = EndpointMetrics.new(5)

    EndpointMetrics.record(metrics, 1, 120, false)
    EndpointMetrics.record(metrics, 1, 80, false)
    EndpointMetrics.record(metrics, 1, 200, true)
    EndpointMetrics.record(metrics, 2, 50, false)

    stats1 = EndpointMetrics.get_stats(metrics, 1)
    check("endpoint 1 requests", stats1.requests, 3)
    check("endpoint 1 errors", stats1.errors, 1)
    check("endpoint 1 avg_latency", stats1.avg_latency_ms, 400 / 3)
    check("endpoint 1 error_rate", Float.round(stats1.error_rate, 4), Float.round(1/3, 4))

    stats2 = EndpointMetrics.get_stats(metrics, 2)
    check("endpoint 2 requests", stats2.requests, 1)
    check("endpoint 2 errors", stats2.errors, 0)

    stats_unused = EndpointMetrics.get_stats(metrics, 5)
    check("endpoint sin datos", stats_unused.requests, 0)
    check("endpoint sin datos avg", stats_unused.avg_latency_ms, 0.0)

    all = EndpointMetrics.get_all_stats(metrics)
    check("get_all_stats count", length(all), 5)

    EndpointMetrics.reset(metrics)
    stats_reset = EndpointMetrics.get_stats(metrics, 1)
    check("reset: requests = 0", stats_reset.requests, 0)
  end

  defp test_rate_limiter do
    IO.puts("\n--- Exercise 3: :counters — RateLimiter ---")

    limiter = RateLimiter.new(10, 3)  # 10 req/s, 3 endpoints

    # Endpoint 1: hasta 10 requests permitidos
    results = Enum.map(1..12, fn _ -> RateLimiter.allow?(limiter, 1) end)
    allowed = Enum.count(results, & &1)
    denied = Enum.count(results, &(!&1))

    check("10 permitidos", allowed, 10)
    check("2 rechazados", denied, 2)

    # Endpoint 2 es independiente — no afectado por endpoint 1
    check("endpoint 2 independiente", RateLimiter.allow?(limiter, 2), true)

    count = RateLimiter.get_count(limiter, 1)
    check("get_count endpoint 1", count, 10)

    RateLimiter.reset(limiter, 1)
    check("reset endpoint 1", RateLimiter.get_count(limiter, 1), 0)
    check("reset no afecta endpoint 2", RateLimiter.get_count(limiter, 2), 1)

    RateLimiter.reset_all(limiter)
    check("reset_all endpoint 2 = 0", RateLimiter.get_count(limiter, 2), 0)

    IO.puts("  Ejecutando benchmark counters vs ETS (puede tardar ~10s)...")
    result = RateLimiter.benchmark_vs_ets(500_000)
    IO.puts("  [benchmark] ETS: #{result.ets_ops_per_sec} ops/s | :counters: #{result.counters_ops_per_sec} ops/s | speedup: #{result.speedup}x")
    check("speedup >= 1.0 (counters no es más lento)", result.speedup >= 1.0, true)
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
# ERROR 1: :ets.update_counter sin registro previo → badarg
#
#   :ets.new(:t, [:named_table, :public])
#   :ets.update_counter(:t, :count, {2, 1})  # ← badarg, :count no existe
#
#   Solución: siempre insertar el registro inicial con valor 0 antes de usar update_counter.
#   :ets.insert(:t, {:count, 0})
#
# ERROR 2: :atomics con índice 0 → badarg (son 1-based)
#
#   ref = :atomics.new(5, [])
#   :atomics.add(ref, 0, 1)  # ← badarg, índices son 1..5
#
#   Solución: los índices de :atomics y :counters son SIEMPRE 1-based.
#   El cálculo para endpoint E es: (E - 1) * 3 + 1 (nunca empieza en 0).
#
# ERROR 3: Asumir que :counters.get es barato
#
#   Para :write_concurrency, :counters.get suma los valores de todos los schedulers.
#   En un sistema con 32 schedulers y 1000 dimensiones, get_all es O(32 × 1000).
#   No llamar get en el hot path — reservarlo para sampling periódico.
#
# ERROR 4: Race condition en allow? del rate limiter
#
#   current = :counters.get(ref, idx)  # read
#   if current < limit do              # check
#     :counters.add(ref, idx, 1)       # act
#   end
#
#   Entre el read y el add, otro proceso puede haber incrementado el contador.
#   Esto puede resultar en más de `limit` requests siendo permitidos.
#   Para un rate limiter exacto: usar :atomics con compare_exchange/4 (CAS loop).
#   Para un rate limiter aproximado (métricas): esta aproximación es aceptable.
#
# ERROR 5: Usar :atomics para contadores negativos sin :signed
#
#   ref = :atomics.new(1, [])     # default: signed: false (unsigned)
#   :atomics.add(ref, 1, -1)      # ← en unsigned, -1 = 2^64 - 1 → overflow
#
#   Solución: usar :atomics.new(1, [signed: true]) si necesitas decrementos.

# =============================================================================
# One Possible Solution (sparse)
# =============================================================================
#
# RequestCounter.record_request/2:
#   :ets.update_counter(:request_counters, :total, {2, 1})
#   :ets.update_counter(:request_counters, method, {2, 1})
#   :ets.update_counter(:request_counters, status, {2, 1})
#
# EndpointMetrics.record/4:
#   req_idx = (endpoint_idx - 1) * 3 + 1
#   lat_idx = req_idx + 1
#   err_idx = req_idx + 2
#   :atomics.add(ref, req_idx, 1)
#   :atomics.add(ref, lat_idx, latency_ms)
#   if is_error?, do: :atomics.add(ref, err_idx, 1)
#
# RateLimiter.allow?/2 (aproximado):
#   current = :counters.get(ref, dimension_idx)
#   if current < limit do
#     :counters.add(ref, dimension_idx, 1)
#     true
#   else
#     false
#   end

# =============================================================================
# Deep Dive: Hardware-Level Atomics
# =============================================================================
#
# :atomics y :ets.update_counter usan instrucciones atómicas de hardware:
#
# x86_64: LOCK XADD — atómicamente suma e intercambia en memoria
#   Garantiza que ningún otro CPU puede ver el estado intermedio.
#   Single-cycle operation en L1 cache hit (~1-4 cycles = <1ns).
#   Con miss de cache: ~100ns (buscar en L2/L3/RAM).
#
# ARM64: LDADD (ARMv8.1+) — Load-Add atomic
#   Similar a LOCK XADD pero para arquitectura ARM.
#   En Apple Silicon (M1/M2/M3), muy eficiente.
#
# Compare-And-Swap (CAS) en :atomics:
#   :atomics.compare_exchange(ref, index, expected, desired)
#   Solo actualiza si el valor actual == expected.
#   Para implementar rate limiters exactos o estructuras lock-free.
#
# Memory ordering:
#   :atomics por defecto usa "sequentially consistent" ordering.
#   Esto es el más fuerte y el más lento.
#   Para métricas donde el orden exacto no importa, se puede usar
#   :relaxed en lenguajes como Rust/C++. En Elixir, no es configurable.

# =============================================================================
# Summary
# =============================================================================
#
# Los tres niveles de contadores atómicos en BEAM:
#
# - ETS update_counter: para cuando ya usas ETS (cache + counter en una tabla)
# - :atomics: para métricas N-dimensionales de alta frecuencia (métricas por endpoint)
# - :counters: para cuando la escritura es el bottleneck (rate limiting, request counting)
#
# Regla de oro:
#   Si lees el valor raramente pero escribes constantemente → :counters
#   Si lees y escribes con frecuencia similar → :atomics o ETS
#   Si ya tienes una tabla ETS → update_counter

# =============================================================================
# What's Next
# =============================================================================
# - Revisar ejercicios 16-20 completos: ETS, DETS, Mnesia, Cache, Counters
# - Explorar: Telemetry (biblioteca de métricas de OTP) — usa :atomics internamente
# - Explorar: :persistent_term — alternativa a ETS para datos que casi nunca cambian
# - Explorar: Phoenix LiveDashboard — métricas en tiempo real usando Telemetry

# =============================================================================
# Resources
# =============================================================================
# - https://www.erlang.org/doc/man/atomics.html
# - https://www.erlang.org/doc/man/counters.html
# - https://www.erlang.org/doc/man/ets.html#update_counter-3
# - https://hexdocs.pm/telemetry/readme.html
# - OTP Design Principles — Efficiency Guide: processes and tables

AtomicsCountersTests.run()
