# =============================================================================
# Ejercicio 31: Patrones de Concurrencia — Fan-out, Fan-in, Aggregator
# Nivel: Intermedio
# =============================================================================
#
# El patrón fan-out/fan-in es fundamental en sistemas concurrentes:
#   Fan-out: distribuir trabajo a múltiples workers en paralelo
#   Fan-in:  recolectar resultados de todos los workers
#
# Conceptos clave:
#   - Task.async/1 para lanzar trabajo asíncrono
#   - Task.await/2 para esperar un resultado específico
#   - Task.await_many/2 para recolectar múltiples tareas
#   - Task.yield/2 para timeout no-bloqueante
#   - Manejo de resultados parciales cuando algunos workers fallan
#
# Para correr: elixir exercise.exs
# =============================================================================

# =============================================================================
# SECCIÓN 1: Fan-out básico con Task.async
# =============================================================================
#
# Task.async/1 crea un proceso que ejecuta la función y envía el resultado
# al proceso padre. Task.await/2 espera ese resultado (default: 5 segundos).
#
# La pareja async/await es la forma más simple de paralelismo en Elixir.

IO.puts("=== Sección 1: Fan-out Básico ===\n")

# Simular trabajo costoso (I/O, consulta externa, etc.)
defmodule Work do
  def process_item(item, delay_ms \\ 100) do
    Process.sleep(delay_ms)
    {:ok, item * 2}
  end

  def fetch_from_api(url) do
    # Simular latencia de red variable
    delay = :rand.uniform(200) + 50
    Process.sleep(delay)
    {:ok, "response from #{url}", delay}
  end
end

# TODO 1: Implementa FanOut.parallel_map/2 que:
#   - Recibe una lista de items y una función
#   - Lanza una Task.async por cada item
#   - Retorna la lista de resultados en el MISMO ORDEN que los inputs
#   - Usa Task.await_many/1 (o Task.await/2 en un Enum.map)
#
# Ejemplo:
#   FanOut.parallel_map([1, 2, 3, 4, 5], &Work.process_item/1)
#   # => [{:ok, 2}, {:ok, 4}, {:ok, 6}, {:ok, 8}, {:ok, 10}]
#
# Pista: Task.async devuelve una %Task{} struct. Guarda las tasks EN ORDEN.
#
# Tu código aquí:

defmodule FanOut do
  def parallel_map(items, fun) do
    # --- FIN TODO 1 ---
  end
end

IO.puts("Procesando 5 items en paralelo...")
start = :erlang.monotonic_time(:millisecond)
results = FanOut.parallel_map([1, 2, 3, 4, 5], &Work.process_item/1)
elapsed = :erlang.monotonic_time(:millisecond) - start
IO.puts("Resultados: #{inspect(results)}")
IO.puts("Tiempo: ~#{elapsed}ms (secuencial sería ~500ms)\n")

# =============================================================================
# SECCIÓN 2: Fan-in con Task.await_many
# =============================================================================
#
# Task.await_many/2 espera una LISTA de tasks y retorna sus resultados en orden.
# Es más eficiente que mapear Task.await sobre cada task individualmente.

IO.puts("=== Sección 2: Fan-in con await_many ===\n")

# TODO 2: Implementa un sistema que consulta múltiples "endpoints" en paralelo
#   y agrega todos los resultados en una sola lista.
#
#   ParallelFetcher.fetch_all/1 recibe una lista de URLs y:
#     1. Lanza Task.async para cada URL (usa Work.fetch_from_api/1)
#     2. Usa Task.await_many/2 con timeout de 3000ms para recolectar resultados
#     3. Retorna una lista de {:ok, response, delay_ms} en el orden original
#
# Tu código aquí:

defmodule ParallelFetcher do
  def fetch_all(urls) do
    # --- FIN TODO 2 ---
  end
end

urls = [
  "https://api1.example.com/data",
  "https://api2.example.com/data",
  "https://api3.example.com/data"
]

IO.puts("Fetching #{length(urls)} URLs en paralelo...")
start = :erlang.monotonic_time(:millisecond)
fetch_results = ParallelFetcher.fetch_all(urls)
elapsed = :erlang.monotonic_time(:millisecond) - start
IO.puts("Resultados (#{elapsed}ms total):")
Enum.each(fetch_results, fn {:ok, resp, delay} ->
  IO.puts("  #{resp} (tardó #{delay}ms)")
end)
IO.puts("")

# =============================================================================
# SECCIÓN 3: Timeout handling — tareas lentas
# =============================================================================
#
# Cuando una tarea tarda más de lo esperado, necesitamos manejar el timeout.
# Task.yield/2 (no-bloqueante) es preferible a Task.await/2 para esto:
#   - {:ok, result}  → tarea completó dentro del timeout
#   - nil            → tarea aún no terminó (timeout)
#   - {:exit, reason} → tarea falló
#
# Después de yield que retorna nil, SIEMPRE hacer Task.shutdown/2 para limpiar.

IO.puts("=== Sección 3: Timeout Handling ===\n")

defmodule SlowWork do
  def slow_process(item, delay_ms) do
    Process.sleep(delay_ms)
    {:ok, "processed: #{item}"}
  end
end

# TODO 3: Implementa SafeFanOut.run_with_timeout/3 que:
#   - Recibe items, una función, y un timeout en ms
#   - Lanza cada item como Task.async
#   - Usa Task.yield/2 para esperar resultados con timeout
#   - Si una tarea no completa a tiempo, retorna {:timeout, item}
#   - Si completa, retorna {:ok, resultado}
#   - SIEMPRE limpia las tareas pendientes con Task.shutdown/2
#   - Retorna una lista de {status, value} tuples
#
# Ejemplo de resultado esperado:
#   [{:ok, "processed: fast"}, {:timeout, "slow_item"}, {:ok, "processed: ok"}]
#
# Pista:
#   tasks_with_items = Enum.map(items, fn item -> {Task.async(fn -> fun.(item) end), item} end)
#   Enum.map(tasks_with_items, fn {task, item} ->
#     case Task.yield(task, timeout) do
#       {:ok, result} -> {:ok, result}
#       nil ->
#         Task.shutdown(task, :brutal_kill)
#         {:timeout, item}
#     end
#   end)
#
# Tu código aquí:

defmodule SafeFanOut do
  def run_with_timeout(items, fun, timeout_ms) do
    # --- FIN TODO 3 ---
  end
end

# Items con distintas velocidades: rápido, muy lento, rápido
items_with_delays = [
  {"item-fast-1", 50},
  {"item-slow",   2000},  # superará el timeout de 300ms
  {"item-fast-2", 100}
]

results_with_timeout = SafeFanOut.run_with_timeout(
  items_with_delays,
  fn {name, delay} -> SlowWork.slow_process(name, delay) end,
  300
)

IO.puts("Resultados con timeout 300ms:")
Enum.each(results_with_timeout, fn result ->
  IO.puts("  #{inspect(result)}")
end)
IO.puts("")

# =============================================================================
# SECCIÓN 4: Partial results — lo que llegó a tiempo
# =============================================================================
#
# En sistemas reales, preferimos resultados parciales a fallar completamente.
# Si 4 de 5 APIs responden, procesamos esas 4 y descartamos el timeout.

IO.puts("=== Sección 4: Resultados Parciales ===\n")

# TODO 4: Implementa BestEffortFanOut.collect_available/3 que:
#   - Lanza tasks para todos los items
#   - Espera con yield hasta el timeout
#   - SEPARA resultados en dos listas:
#       :successes → lista de resultados que llegaron a tiempo
#       :timeouts  → lista de items que no completaron
#   - Retorna %{successes: [...], timeouts: [...], success_rate: float}
#     donde success_rate = successes / total * 100
#
# Tu código aquí:

defmodule BestEffortFanOut do
  def collect_available(items, fun, timeout_ms) do
    # --- FIN TODO 4 ---
  end
end

# Simular consultas de precios donde algunas APIs son lentas
price_queries = [
  {:api_alpha,   fn -> Process.sleep(80);  {:ok, 99.99}  end},
  {:api_beta,    fn -> Process.sleep(500); {:ok, 89.99}  end}, # timeout
  {:api_gamma,   fn -> Process.sleep(120); {:ok, 109.99} end},
  {:api_delta,   fn -> Process.sleep(600); {:ok, 94.99}  end}, # timeout
  {:api_epsilon, fn -> Process.sleep(90);  {:ok, 97.99}  end}
]

partial_results = BestEffortFanOut.collect_available(
  price_queries,
  fn {_name, fun} -> fun.() end,
  250
)

IO.puts("Resultados parciales de 5 APIs (timeout 250ms):")
IO.puts("  Exitosos: #{length(partial_results.successes)}")
IO.puts("  Timeouts: #{inspect(partial_results.timeouts)}")
IO.puts("  Tasa de éxito: #{partial_results.success_rate}%\n")

# =============================================================================
# SECCIÓN 5: Aggregator — proceso que combina resultados
# =============================================================================
#
# Un aggregator es un GenServer que recolecta resultados de múltiples
# workers y los combina. Útil cuando los workers son procesos de larga vida
# que envían resultados de forma asíncrona.

IO.puts("=== Sección 5: Aggregator Pattern ===\n")

defmodule ResultAggregator do
  use GenServer

  # API pública
  def start_link(expected_count) do
    GenServer.start_link(__MODULE__, expected_count)
  end

  def submit_result(pid, worker_id, result) do
    GenServer.cast(pid, {:result, worker_id, result})
  end

  # Espera hasta que lleguen todos los resultados (o timeout)
  def wait_for_all(pid, timeout \\ 5000) do
    GenServer.call(pid, :get_all, timeout)
  end

  def partial_results(pid) do
    GenServer.call(pid, :get_partial)
  end

  @impl GenServer
  def init(expected_count) do
    state = %{
      expected: expected_count,
      received: 0,
      results: %{},
      waiters: []
    }
    {:ok, state}
  end

  @impl GenServer
  def handle_cast({:result, worker_id, result}, state) do
    new_results  = Map.put(state.results, worker_id, result)
    new_received = state.received + 1
    new_state    = %{state | results: new_results, received: new_received}

    # Notificar a los waiters si ya llegaron todos
    new_state =
      if new_received >= state.expected do
        Enum.each(state.waiters, fn from ->
          GenServer.reply(from, {:complete, new_results})
        end)
        %{new_state | waiters: []}
      else
        new_state
      end

    {:noreply, new_state}
  end

  @impl GenServer
  def handle_call(:get_all, from, state) do
    if state.received >= state.expected do
      {:reply, {:complete, state.results}, state}
    else
      # Suspender al caller hasta que lleguen todos los resultados
      {:noreply, %{state | waiters: [from | state.waiters]}}
    end
  end

  def handle_call(:get_partial, _from, state) do
    {:reply, state.results, state}
  end
end

# TODO 5: Usa el ResultAggregator para recolectar resultados de workers paralelos
#
#   Pasos:
#     1. {:ok, agg} = ResultAggregator.start_link(4)  # esperamos 4 workers
#     2. Lanza 4 Tasks que cada una:
#        a. Simula trabajo con Process.sleep(random)
#        b. Llama a ResultAggregator.submit_result(agg, worker_id, resultado)
#     3. Llama a ResultAggregator.wait_for_all(agg, 3000)
#     4. Muestra los resultados aggregados
#
# Tu código aquí:

IO.puts("Lanzando 4 workers con aggregator...\n")

# --- FIN TODO 5 ---

# =============================================================================
# SECCIÓN 6: Worker Pool manual
# =============================================================================
#
# En lugar de lanzar N tasks para N items, un pool limita la concurrencia.
# Esto protege recursos (conexiones DB, rate limits de API).

IO.puts("=== Sección 6: Concurrencia Controlada ===\n")

defmodule ControlledFanOut do
  # Procesa items con máximo `concurrency` tareas simultáneas
  def run(items, fun, concurrency: max_concurrent) do
    items
    |> Enum.chunk_every(max_concurrent)
    |> Enum.flat_map(fn chunk ->
      chunk
      |> Enum.map(&Task.async(fn -> fun.(&1) end))
      |> Task.await_many(10_000)
    end)
  end
end

IO.puts("Procesando 10 items con max 3 concurrentes:")
start = :erlang.monotonic_time(:millisecond)

results = ControlledFanOut.run(
  1..10 |> Enum.to_list(),
  fn i ->
    Process.sleep(100)
    i * i
  end,
  concurrency: 3
)

elapsed = :erlang.monotonic_time(:millisecond) - start
IO.puts("Resultados: #{inspect(results)}")
IO.puts("Tiempo: ~#{elapsed}ms (4 batches × ~100ms)\n")

# =============================================================================
# SECCIÓN 7: Manejo de errores en Tasks
# =============================================================================
#
# Si una Task lanza una excepción, Task.await/2 la re-lanza en el proceso padre.
# Para manejo suave, captura el error dentro de la función de la task.

IO.puts("=== Sección 7: Errores en Tasks ===\n")

defmodule SafeTask do
  def async_safe(fun) do
    Task.async(fn ->
      try do
        {:ok, fun.()}
      rescue
        e -> {:error, Exception.message(e)}
      catch
        :exit, reason -> {:error, {:exit, reason}}
      end
    end)
  end
end

risky_items = [1, 2, 0, 3, "boom", 4]

safe_results =
  risky_items
  |> Enum.map(fn item ->
    SafeTask.async_safe(fn ->
      if item == 0, do: raise("División por cero!")
      if is_binary(item), do: raise("Tipo inválido: #{item}")
      100 / item
    end)
  end)
  |> Task.await_many(5000)

IO.puts("Resultados con manejo de errores:")
Enum.zip(risky_items, safe_results)
|> Enum.each(fn {item, result} ->
  IO.puts("  item=#{inspect(item)} → #{inspect(result)}")
end)
IO.puts("")

# =============================================================================
# SECCIÓN 8: Task.Supervisor para tasks fault-tolerant
# =============================================================================
#
# Task.async no está supervisado por el supervisor del sistema.
# Task.Supervisor.async/3 sí lo está — si el proceso padre muere,
# el supervisor limpia las tasks huérfanas.

IO.puts("=== Sección 8: Task.Supervisor ===\n")

{:ok, sup} = Task.Supervisor.start_link()

tasks = Enum.map(1..5, fn i ->
  Task.Supervisor.async(sup, fn ->
    Process.sleep(:rand.uniform(100))
    {:worker, i, i * 10}
  end)
end)

supervised_results = Task.await_many(tasks, 5000)
IO.puts("Resultados supervisados:")
Enum.each(supervised_results, fn result ->
  IO.puts("  #{inspect(result)}")
end)
IO.puts("")

# =============================================================================
# SECCIÓN 9: TRY IT YOURSELF
# =============================================================================
#
# Implementa un sistema de consulta de precios que:
#
# 1. Tiene 5 "APIs externas" simuladas (funciones con sleep random 50-300ms)
#    Cada una retorna {:ok, precio} o, con 20% de probabilidad, lanza un error
#
# 2. PriceAggregator.get_best_price/1 recibe una lista de fns de API y:
#    a. Consulta todas en paralelo con Task.async
#    b. Usa timeout de 400ms
#    c. Recoge SOLO los resultados exitosos (ignora timeouts y errores)
#    d. Si no hay resultados exitosos, retorna {:error, :all_failed}
#    e. Si hay al menos uno, retorna {:ok, precio_promedio, count_disponible}
#       donde precio_promedio = promedio de los precios recibidos
#
# 3. Muestra un reporte: cuántas APIs respondieron, cuántas tardaron demasiado,
#    y el precio promedio calculado.
#
# Ejemplo de API simulada:
#   fn ->
#     if :rand.uniform(5) == 1, do: raise("API down")
#     Process.sleep(:rand.uniform(250) + 50)
#     {:ok, 90.0 + :rand.uniform(20) * 1.0}
#   end

IO.puts("=== SECCIÓN 9: Try It Yourself ===\n")
IO.puts("Implementa PriceAggregator abajo:\n")

defmodule PriceAggregator do
  # Tu implementación aquí
end

# APIs simuladas
api_fns = Enum.map(1..5, fn i ->
  fn ->
    if :rand.uniform(5) == 1, do: raise("API #{i} down!")
    Process.sleep(:rand.uniform(250) + 50)
    {:ok, Float.round(90.0 + :rand.uniform(20) * 1.0, 2)}
  end
end)

IO.puts("--- Test PriceAggregator ---")
case PriceAggregator.get_best_price(api_fns) do
  {:ok, avg_price, count} ->
    IO.puts("Precio promedio: $#{avg_price} (de #{count} APIs disponibles)")
  {:error, :all_failed} ->
    IO.puts("Error: ninguna API disponible")
end

# =============================================================================
# ERRORES COMUNES
# =============================================================================
IO.puts("\n=== Errores Comunes ===\n")
IO.puts("""
1. Crear Tasks dentro de Tasks sin supervisión:
   Las tasks anidadas no son hijas del supervisor — pueden quedar huérfanas.
   Usa Task.Supervisor.async/3 para tasks críticas.

2. No hacer Task.shutdown después de un timeout:
   Si Task.yield devuelve nil y no haces shutdown, el proceso sigue vivo
   consumiendo recursos. Siempre: Task.shutdown(task, :brutal_kill) tras timeout.

3. await_many vs map(await):
   Task.await_many/2 es más eficiente — espera en todos simultáneamente.
   Enum.map(tasks, &Task.await/1) espera de forma SECUENCIAL (aunque los tasks
   son paralelos, el await bloquea por orden).

4. No preservar el orden:
   Task.async_stream preserva el orden de los resultados.
   Si usas spawn/send manual, necesitas incluir el índice en el mensaje.

5. Timeout demasiado corto con await_many:
   El timeout en await_many aplica a CADA task individualmente, no al total.
   Si tienes 10 tasks con timeout 1s, podrías esperar hasta 10 segundos.
""")
