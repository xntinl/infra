# =============================================================================
# Ejercicio 30: Process Dictionary y Módulos Erlang Clave
# Nivel: Intermedio
# =============================================================================
#
# Cada proceso BEAM tiene su propio "diccionario de proceso": un mapa mutable
# privado accesible solo dentro del proceso. Es una forma de estado global
# DENTRO de un proceso, sin GenServer.
#
# Conceptos clave:
#   - Process.put/2, Process.get/1, Process.delete/1
#   - Process.info/2 para introspección de procesos
#   - :sys.get_state/1 para inspeccionar GenServers
#   - Módulos Erlang de utilidad: :erlang, :net_kernel
#
# Para correr: elixir exercise.exs
# =============================================================================

# =============================================================================
# SECCIÓN 1: Process Dictionary — get, put, delete
# =============================================================================
#
# El process dictionary es un key-value store local al proceso.
# Casos de uso legítimos: request context, memoización local, correlación IDs.
# ADVERTENCIA: úsalo con moderación — el estado implícito dificulta el testing.

IO.puts("=== Sección 1: Process Dictionary Básico ===\n")

# TODO 1: Implementa un módulo RequestContext que use el process dictionary
#   para almacenar y recuperar el contexto de una petición HTTP.
#
#   Debe incluir:
#     - put_request_id/1  : guarda el ID con Process.put(:request_id, id)
#     - get_request_id/0  : recupera el ID con Process.get(:request_id)
#     - put_user_id/1     : guarda el user_id
#     - get_user_id/0     : recupera el user_id
#     - clear/0           : elimina :request_id y :user_id del diccionario
#     - all/0             : retorna Process.get() — TODAS las entradas del dict
#
# Tu código aquí:

defmodule RequestContext do
  # --- FIN TODO 1 ---
end

# Uso del RequestContext
RequestContext.put_request_id("req-abc-123")
RequestContext.put_user_id(42)

IO.puts("Request ID: #{RequestContext.get_request_id()}")
IO.puts("User ID: #{RequestContext.get_user_id()}")
IO.puts("Todo el contexto: #{inspect(RequestContext.all())}")

RequestContext.clear()
IO.puts("Después de clear — Request ID: #{inspect(RequestContext.get_request_id())}\n")

# Demostración: el diccionario es LOCAL al proceso
parent_pid = self()
RequestContext.put_request_id("parent-req-999")

child_pid = spawn(fn ->
  # El hijo tiene su PROPIO diccionario — no hereda el del padre
  child_req_id = RequestContext.get_request_id()
  send(parent_pid, {:child_context, child_req_id})
end)

receive do
  {:child_context, val} ->
    IO.puts("Diccionario del hijo (no hereda del padre): #{inspect(val)}")
end

IO.puts("Diccionario del padre intacto: #{RequestContext.get_request_id()}\n")

# =============================================================================
# SECCIÓN 2: Process.info/2 — Introspección de procesos
# =============================================================================
#
# Process.info/2 devuelve información sobre un proceso vivo.
# Información disponible: :message_queue_len, :memory, :status,
# :current_function, :dictionary, :links, :monitors, :trap_exit, etc.

IO.puts("=== Sección 2: Process.info ===\n")

# TODO 2: Implementa ProcessInspector.report/1 que recibe un PID y
#   retorna un mapa con esta información del proceso:
#     :pid        → el PID recibido
#     :memory     → memoria en bytes (Process.info(pid, :memory))
#     :queue_len  → largo de la cola de mensajes (:message_queue_len)
#     :status     → estado del proceso (:status)
#     :reductions → número de reducciones ejecutadas (:reductions)
#
#   Si el proceso no existe, retorna {:error, :not_found}
#
# Pista: Process.info/2 retorna nil si el proceso no existe
#
# Tu código aquí:

defmodule ProcessInspector do
  # --- FIN TODO 2 ---
end

# Inspeccionar el proceso actual
report = ProcessInspector.report(self())
IO.puts("Reporte del proceso actual:")
IO.puts("  Memoria: #{report.memory} bytes")
IO.puts("  Cola de mensajes: #{report.queue_len}")
IO.puts("  Status: #{report.status}")
IO.puts("  Reducciones: #{report.reductions}")

# Enviar mensajes sin receiveir para ver la cola crecer
send(self(), :msg1)
send(self(), :msg2)
send(self(), :msg3)
report2 = ProcessInspector.report(self())
IO.puts("\nDespués de 3 mensajes en cola:")
IO.puts("  Cola de mensajes: #{report2.queue_len}")

# Limpiar la cola
receive do :msg1 -> :ok end
receive do :msg2 -> :ok end
receive do :msg3 -> :ok end

# Proceso muerto
dead_pid = spawn(fn -> :ok end)
Process.sleep(10) # darle tiempo a terminar
IO.puts("\nProceso muerto: #{inspect(ProcessInspector.report(dead_pid))}\n")

# =============================================================================
# SECCIÓN 3: :sys.get_state — Inspeccionar GenServer en vivo
# =============================================================================
#
# :sys es un módulo Erlang para depuración de procesos OTP.
# :sys.get_state/1 extrae el estado interno de un GenServer SIN detenerlo.
# :sys.get_status/1 da info de nivel OTP (más detallada).
# Útil en producción para diagnóstico sin reiniciar procesos.

IO.puts("=== Sección 3: :sys.get_state con GenServer ===\n")

defmodule CounterServer do
  use GenServer

  def start_link(initial \\ 0) do
    GenServer.start_link(__MODULE__, initial)
  end

  def increment(pid, by \\ 1), do: GenServer.cast(pid, {:increment, by})
  def decrement(pid, by \\ 1), do: GenServer.cast(pid, {:decrement, by})
  def value(pid),              do: GenServer.call(pid, :value)
  def reset(pid),              do: GenServer.cast(pid, :reset)

  @impl GenServer
  def init(initial), do: {:ok, %{count: initial, operations: 0}}

  @impl GenServer
  def handle_call(:value, _from, state), do: {:reply, state.count, state}

  @impl GenServer
  def handle_cast({:increment, by}, state) do
    {:noreply, %{state | count: state.count + by, operations: state.operations + 1}}
  end

  def handle_cast({:decrement, by}, state) do
    {:noreply, %{state | count: state.count - by, operations: state.operations + 1}}
  end

  def handle_cast(:reset, state) do
    {:noreply, %{state | count: 0, operations: state.operations + 1}}
  end
end

# TODO 3: Arranca el CounterServer, realiza operaciones y usa :sys.get_state
#   para inspeccionar el estado interno sin llamar a CounterServer.value/1.
#
#   Pasos:
#     1. {:ok, pid} = CounterServer.start_link(10)
#     2. Hacer algunas operaciones (increment, decrement)
#     3. state = :sys.get_state(pid)
#     4. IO.puts con el estado interno (count y operations)
#     5. Comparar con CounterServer.value(pid)
#
# Tu código aquí:

# --- FIN TODO 3 ---

# =============================================================================
# SECCIÓN 4: Módulos Erlang de utilidad
# =============================================================================
#
# Elixir corre sobre la BEAM y tiene acceso directo a todos los módulos Erlang.
# Los más útiles para el día a día: :erlang, :os, :timer, :math

IO.puts("=== Sección 4: Módulos Erlang ===\n")

# TODO 4: Usa los módulos Erlang para obtener y mostrar:
#
#   A) Tiempo actual:
#      - :erlang.timestamp() → {megasec, sec, microsec} — tiempo UNIX
#      - :erlang.monotonic_time(:millisecond) → ms desde inicio de VM (para benchmarks)
#      - :erlang.system_time(:second) → segundos UNIX (más moderno)
#
#   B) Memoria del sistema:
#      - :erlang.memory() → keyword list con :total, :processes, :ets, :atom, etc.
#      - :erlang.memory(:total) → solo el total en bytes
#
#   C) Info del sistema:
#      - :erlang.system_info(:otp_release) → versión OTP (charlist)
#      - :erlang.system_info(:process_count) → procesos actuales
#      - :erlang.system_info(:schedulers_online) → schedulers activos
#
#   Muestra cada valor con IO.puts formateado.
#
# Tu código aquí:

IO.puts("\n-- Tiempo --")
# A) Tu código aquí

IO.puts("\n-- Memoria --")
# B) Tu código aquí

IO.puts("\n-- Sistema --")
# C) Tu código aquí

# --- FIN TODO 4 ---

IO.puts("")

# =============================================================================
# SECCIÓN 5: Node info — sistemas distribuidos
# =============================================================================
#
# En sistemas distribuidos BEAM, múltiples nodos se conectan en una red.
# node/0 retorna el nombre del nodo actual.
# Node.list/0 retorna los nodos conectados.
# :net_kernel proporciona funciones de control de la red.

IO.puts("=== Sección 5: Node Info ===\n")

# TODO 5: Implementa NodeInfo.report/0 que retorna un mapa con:
#     :current_node  → node()
#     :connected     → Node.list() (lista de nodos conectados)
#     :node_count    → length(Node.list()) + 1 (incluye el actual)
#     :cookie        → Node.get_cookie() — identifica el cluster
#     :alive         → Node.alive?() — si el nodo está en modo distribuido
#
#   Luego muestra el reporte con IO.puts.
#
#   Nota: En modo single-node (sin --name), node() retorna :nonode@nohost
#         y Node.alive?() retorna false. Eso es normal.
#
# Tu código aquí:

defmodule NodeInfo do
  # --- FIN TODO 5 ---
end

node_report = NodeInfo.report()
IO.puts("Nodo actual: #{node_report.current_node}")
IO.puts("Nodos conectados: #{inspect(node_report.connected)}")
IO.puts("Total nodos en cluster: #{node_report.node_count}")
IO.puts("Cookie: #{node_report.cookie}")
IO.puts("¿Modo distribuido?: #{node_report.alive}\n")

# =============================================================================
# SECCIÓN 6: Benchmark con monotonic_time
# =============================================================================
#
# :erlang.monotonic_time/1 es ideal para medir duración de operaciones
# porque no se ve afectado por cambios de reloj del sistema.

IO.puts("=== Sección 6: Benchmark con monotonic_time ===\n")

defmodule Benchmark do
  def measure(label, fun) do
    start = :erlang.monotonic_time(:microsecond)
    result = fun.()
    finish = :erlang.monotonic_time(:microsecond)
    elapsed_us = finish - start
    IO.puts("#{label}: #{elapsed_us} μs (#{elapsed_us / 1_000} ms)")
    result
  end
end

# Comparar map vs for comprehension
Benchmark.measure("Enum.map 100k", fn ->
  Enum.map(1..100_000, &(&1 * 2))
end)

Benchmark.measure("for compreh 100k", fn ->
  for x <- 1..100_000, do: x * 2
end)

Benchmark.measure("Stream.map 100k", fn ->
  1..100_000 |> Stream.map(&(&1 * 2)) |> Enum.to_list()
end)

IO.puts("")

# =============================================================================
# SECCIÓN 7: Process dictionary para memoización
# =============================================================================
#
# Un uso válido del process dictionary es la memoización dentro de un proceso:
# resultados costosos de computar se almacenan y reutilizan en la misma llamada.

IO.puts("=== Sección 7: Memoización con Process Dictionary ===\n")

defmodule Memo do
  def fibonacci(n) do
    cache_key = {:fib_cache, n}
    case Process.get(cache_key) do
      nil ->
        result = compute_fib(n)
        Process.put(cache_key, result)
        result
      cached ->
        cached
    end
  end

  defp compute_fib(0), do: 0
  defp compute_fib(1), do: 1
  defp compute_fib(n), do: fibonacci(n - 1) + fibonacci(n - 2)
end

Benchmark.measure("Fibonacci(35) con memo", fn ->
  Memo.fibonacci(35)
end)

Benchmark.measure("Fibonacci(35) segunda vez (todo cacheado)", fn ->
  Memo.fibonacci(35)
end)

IO.puts("Fib(35) = #{Memo.fibonacci(35)}\n")

# =============================================================================
# SECCIÓN 8: Introspección avanzada con :erlang.process_info
# =============================================================================
#
# :erlang.process_info/2 es la versión Erlang de Process.info/2.
# Algunos campos solo están en la versión Erlang: :backtrace, :current_stacktrace

IO.puts("=== Sección 8: Introspección Avanzada ===\n")

# Crear un proceso que mantiene estado en su loop
loop_pid = spawn(fn ->
  Process.put(:my_name, "worker-loop")
  # Simular trabajo
  :timer.sleep(5000)
end)

Process.sleep(10)

# Inspeccionar el proceso hijo
{:dictionary, dict} = :erlang.process_info(loop_pid, :dictionary)
IO.puts("Diccionario del proceso hijo: #{inspect(dict)}")

{:current_function, {mod, fun, arity}} = :erlang.process_info(loop_pid, :current_function)
IO.puts("Función actual: #{mod}.#{fun}/#{arity}")

{:status, status} = :erlang.process_info(loop_pid, :status)
IO.puts("Status: #{status}")

Process.exit(loop_pid, :kill)
IO.puts("")

# =============================================================================
# SECCIÓN 9: TRY IT YOURSELF
# =============================================================================
#
# Implementa un Request Tracking Middleware usando process dictionary.
#
# El objetivo: propagar un request_id automáticamente a través de toda
# la call stack sin pasarlo explícitamente como parámetro.
#
# Implementa el módulo RequestTracker con:
#
#   start_request/0
#     - Genera un UUID simple (puede ser un entero random o :crypto.strong_rand_bytes(8) |> Base.encode16())
#     - Lo guarda en el process dictionary
#     - Registra el timestamp de inicio (:erlang.monotonic_time(:microsecond))
#     - Retorna el request_id generado
#
#   current_request_id/0
#     - Retorna el request_id actual del process dictionary
#     - Si no hay request activo, retorna :no_request
#
#   log/2 (level, message)
#     - Obtiene el request_id del dictionary
#     - Imprime: "[{level}] [{request_id}] {message}"
#     - Ejemplo: "[INFO] [REQ-ABC123] Processing user 42"
#
#   end_request/0
#     - Calcula la duración desde el inicio
#     - Limpia el process dictionary (request_id y timestamp)
#     - Retorna la duración en ms
#
# Demostración:
#   req_id = RequestTracker.start_request()
#   RequestTracker.log(:info, "Request recibido")
#   RequestTracker.log(:info, "Procesando...")
#   duration = RequestTracker.end_request()
#   IO.puts("Request #{req_id} completado en #{duration}ms")

IO.puts("=== SECCIÓN 9: Try It Yourself ===\n")
IO.puts("Implementa RequestTracker abajo:\n")

defmodule RequestTracker do
  # Tu implementación aquí
end

# Tests de tu implementación
IO.puts("--- Tests de RequestTracker ---")

req_id = RequestTracker.start_request()
IO.puts("Request iniciado: #{req_id}")

RequestTracker.log(:info, "Validando parámetros")
RequestTracker.log(:info, "Consultando base de datos")
RequestTracker.log(:warn, "Cache miss — consultando fuente")
RequestTracker.log(:info, "Respuesta preparada")

:timer.sleep(5) # simular trabajo
duration = RequestTracker.end_request()
IO.puts("Request completado en #{duration}ms")
IO.puts("Request ID después de end: #{inspect(RequestTracker.current_request_id())}")

# =============================================================================
# ERRORES COMUNES
# =============================================================================
IO.puts("\n=== Errores Comunes ===\n")
IO.puts("""
1. Usar el process dictionary como estado global del sistema:
   Es LOCAL al proceso. Si el proceso muere, se pierde TODO.
   No lo uses para comunicación entre procesos — usa mensajes.

2. Confundir :erlang.monotonic_time con :erlang.system_time:
   - monotonic: nunca retrocede, ideal para medir DURACIÓN
   - system: tiempo de pared (wall clock), puede cambiar con NTP/DST

3. :sys.get_state/1 no es seguro en producción bajo alta carga:
   Detiene brevemente el proceso (sys.suspend). Úsalo con cuidado.
   Preferir GenServer.call/2 con una función :debug si es crítico.

4. Olvidar limpiar el process dictionary:
   Los procesos de un pool (Poolboy, etc.) son reutilizados.
   Si no limpias el contexto, el siguiente request verá datos del anterior.

5. :erlang.process_info/2 vs Process.info/2:
   Son equivalentes, pero :erlang.process_info devuelve tuplas {key, val}
   mientras que Process.info puede retornar directamente el valor o nil.
""")
