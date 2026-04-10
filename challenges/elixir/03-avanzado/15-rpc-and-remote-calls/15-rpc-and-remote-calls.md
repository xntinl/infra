# 15. RPC y Remote Calls

**Difficulty**: Avanzado

---

## Prerequisites

- Ejercicio 11: Distributed Erlang Clustering
- Ejercicio 12: Global Process Registry
- GenServer avanzado con timeout handling
- Comprensión de monitores y links de procesos

---

## Learning Objectives

- Ejecutar código remotamente con `:rpc.call` y `:erpc.call`
- Implementar fan-out asíncrono a múltiples nodos con `:rpc.async_call`
- Comparar RPC vs llamadas directas a GenServer en nodos remotos
- Manejar correctamente errores y timeouts en llamadas distribuidas
- Diseñar APIs de computación distribuida resilientes
- Entender el overhead y cuándo RPC es la elección correcta

---

## Concepts

### ¿Qué es RPC en Erlang?

Remote Procedure Call (RPC) en el contexto de Erlang es ejecutar una función en un nodo remoto y obtener el resultado. A diferencia de enviar mensajes a procesos específicos, RPC invoca código arbitrario en el nodo remoto.

Casos de uso:
- Ejecutar operaciones de administración en nodos remotos (recargar config, estadísticas)
- Distribuir computación entre nodos homogéneos
- Invocar funciones de un release en otro nodo cuando no tienes el PID del proceso
- Fan-out: ejecutar la misma función en todos los nodos del cluster

### :rpc.call — la forma clásica

```elixir
# Ejecutar una función en un nodo remoto y esperar el resultado
:rpc.call(:"nodeB@localhost", String, :upcase, ["hello"])
#=> "HELLO"

# Con timeout (en milisegundos)
:rpc.call(:"nodeB@localhost", String, :upcase, ["hello"], 5_000)
#=> "HELLO"
#=> {:badrpc, :timeout}  # si excede el timeout

# Errores posibles:
:rpc.call(:"nodo_inexistente@localhost", IO, :puts, ["hi"])
#=> {:badrpc, :nodedown}

# Si la función lanza una excepción en el nodo remoto:
:rpc.call(:"nodeB@localhost", __MODULE__, :bad_function, [])
#=> {:badrpc, {:EXIT, {%RuntimeError{message: "..."}, stacktrace}}}
```

**Cómo funciona internamente**: `:rpc` usa un proceso servidor llamado `:rex` (Remote EXecution) que corre en cada nodo Erlang. Cuando llamas `:rpc.call(node, m, f, a)`, el mensaje va a `:rex` en el nodo remoto, que ejecuta `apply(m, f, a)` y envía el resultado de vuelta.

Implicación: todas las llamadas `:rpc.call` a un nodo pasan por el mismo proceso `:rex`. Es un cuello de botella potencial si haces muchas llamadas concurrentes. Para alta concurrencia, usa llamadas directas a GenServer o `:erpc` (OTP 23+).

### :rpc.async_call — sin bloquear

```elixir
# Iniciar la llamada remota sin bloquear
key = :rpc.async_call(:"nodeB@localhost", Computation, :heavy_work, [data])

# Hacer otras cosas mientras el nodo remoto trabaja...

# Recoger el resultado cuando lo necesites
result = :rpc.yield(key)
#=> {:value, resultado}  # éxito
#=> :timeout            # si ya expiró (usa nb_yield para timeouts)

# Con timeout en yield
:rpc.nb_yield(key, 5_000)
#=> {:value, resultado}
#=> :timeout
```

El patrón async_call + yield permite **paralelismo real** con múltiples nodos:

```elixir
nodes = Node.list()
keys = Enum.map(nodes, fn node ->
  {node, :rpc.async_call(node, Stats, :collect, [])}
end)

# Recoger resultados de todos los nodos
results = Enum.map(keys, fn {node, key} ->
  case :rpc.nb_yield(key, 10_000) do
    {:value, stats} -> {node, stats}
    :timeout -> {node, {:error, :timeout}}
  end
end)
```

### :erpc — RPC moderno (OTP 23+)

`:erpc` es la reimplementación moderna de `:rpc`, disponible desde OTP 23 (Elixir ~1.12+). Resuelve los problemas de `:rpc`:

```elixir
# Call síncrono — no usa :rex, no hay cuello de botella
:erpc.call(:"nodeB@localhost", String, :upcase, ["hello"])
#=> "HELLO"

# Con timeout
:erpc.call(:"nodeB@localhost", String, :upcase, ["hello"], 5_000)

# Manejo de errores más preciso
try do
  :erpc.call(:"bad@node", IO, :puts, ["hi"])
catch
  :error, {:erpc, :noconnection} -> {:error, :node_down}
  :error, {:erpc, :timeout} -> {:error, :timeout}
  :error, {exception, _stack} -> {:error, exception}
end
```

**Diferencias clave con `:rpc`**:

| Aspecto | `:rpc` | `:erpc` |
|---------|--------|---------|
| Proceso interno | `:rex` (cuello de botella) | Spawn directo |
| Error format | `{:badrpc, reason}` | Excepción con `:erpc` |
| Concurrencia | Limitada por `:rex` | Sin límite |
| OTP version | Siempre disponible | OTP 23+ |
| Performance | Menor | Mayor |

Para nuevos proyectos: preferir `:erpc`. Para compatibilidad con OTP < 23: usar `:rpc`.

### GenServer.call remoto vs RPC

Hay dos formas de llamar a un GenServer en otro nodo:

```elixir
# Forma 1: via tuple {name, node}
GenServer.call({MyServer, :"nodeB@localhost"}, :get_state)

# Forma 2: con PID remoto
remote_pid = :rpc.call(:"nodeB@localhost", Process, :whereis, [:my_server])
GenServer.call(remote_pid, :get_state)

# Forma 3: RPC directo (si no necesitas ir a un GenServer específico)
:rpc.call(:"nodeB@localhost", MyServer, :public_api_function, [arg])
```

**Cuándo usar cada uno**:

GenServer `{name, node}` es ideal cuando:
- El servidor tiene un nombre registrado localmente en su nodo
- Quieres usar la semántica de GenServer (timeout automático de 5s, monitor del proceso)
- El servidor puede migrar entre nodos (cambias solo la lógica de "a qué nodo enviar")

`:rpc.call` es mejor cuando:
- No hay un proceso servidor — quieres ejecutar una función pura remotamente
- La API es stateless
- Necesitas fan-out a múltiples nodos con la misma función

PID remoto directo (`send(remote_pid, msg)`) cuando:
- Ya tienes el PID del proceso remoto (por ejemplo, lo registraste en :global)
- Necesitas la mínima latencia posible
- Haces muchas llamadas al mismo proceso (sin lookup overhead)

### Handling badrpc y timeouts

El error más común en RPC distribuido es no manejar los errores correctamente:

```elixir
defmodule SafeRPC do
  @default_timeout 5_000

  def call(node, module, function, args, timeout \\ @default_timeout) do
    case :rpc.call(node, module, function, args, timeout) do
      {:badrpc, :nodedown} ->
        {:error, {:node_unavailable, node}}

      {:badrpc, :timeout} ->
        {:error, {:timeout, {module, function}}}

      {:badrpc, {:EXIT, {exception, _stack}}} ->
        {:error, {:remote_exception, exception}}

      {:badrpc, reason} ->
        {:error, {:rpc_error, reason}}

      result ->
        {:ok, result}
    end
  end

  def fanout(nodes, module, function, args, timeout \\ @default_timeout) do
    keys = Enum.map(nodes, fn node ->
      {node, :rpc.async_call(node, module, function, args)}
    end)

    Enum.map(keys, fn {node, key} ->
      case :rpc.nb_yield(key, timeout) do
        {:value, {:badrpc, reason}} -> {node, {:error, reason}}
        {:value, result} -> {node, {:ok, result}}
        :timeout -> {node, {:error, :timeout}}
      end
    end)
  end
end
```

### El problema del timeout y las llamadas colgadas

`:rpc.call` con timeout tiene un comportamiento importante: si el timeout expira, la llamada retorna `{:badrpc, :timeout}` al llamador, pero el proceso en el nodo remoto **sigue ejecutando**. No hay cancelación.

```
t=0: nodeA llama :rpc.call(nodeB, Slow, :work, [], 1000)
t=1: nodeB empieza a ejecutar Slow.work()
t=1000: nodeA recibe {:badrpc, :timeout} y continúa
t=5000: nodeB termina Slow.work(), envía resultado a... nadie
```

Para operaciones que deben ser cancelables, usa un enfoque diferente:

```elixir
# En el nodo remoto, el trabajo debe ser un proceso separado que puedas matar:
defmodule CancellableRPC do
  def start_work(caller, timeout) do
    task_pid = spawn(fn ->
      result = do_heavy_work()
      send(caller, {:rpc_result, self(), result})
    end)

    receive do
      {:rpc_result, ^task_pid, result} -> {:ok, result}
    after
      timeout ->
        Process.exit(task_pid, :kill)
        {:error, :timeout}
    end
  end
end
```

---

## Exercises

### Exercise 1: Remote Computation con :rpc y :erpc

**Problem**: Implementa un módulo `DistributedMath` que puede ejecutar operaciones costosas (factoriales grandes, sumas de series, etc.) en un nodo remoto. Compara el comportamiento de `:rpc.call` vs `:erpc.call`: manejo de errores, timeouts, y concurrencia bajo carga.

**Hints**:
- Define funciones públicas puras en `DistributedMath` — las ejecutarás en el nodo remoto via RPC
- Prueba con una función que tarde más que el timeout — observa qué pasa en el nodo remoto después
- Para la comparación de concurrencia, ejecuta 100 llamadas simultáneas con Task.async y mide el throughput
- `:erpc` lanza excepciones en lugar de retornar `{:badrpc, ...}` — el patrón de manejo es diferente
- Prueba también qué pasa cuando el nodo se cae a mitad de una llamada síncrona

**One possible solution**:

```elixir
defmodule DistributedMath do
  # Funciones que se ejecutarán remotamente

  def factorial(0), do: 1
  def factorial(n) when n > 0, do: n * factorial(n - 1)

  def sum_series(n), do: Enum.sum(1..n)

  def slow_computation(ms) do
    Process.sleep(ms)
    {:done, node(), DateTime.utc_now()}
  end
end

defmodule RPCClient do
  @timeout 5_000

  def compute_factorial(node, n) do
    rpc_call(node, DistributedMath, :factorial, [n])
  end

  def compute_factorial_erpc(node, n) do
    erpc_call(node, DistributedMath, :factorial, [n])
  end

  defp rpc_call(node, m, f, a) do
    case :rpc.call(node, m, f, a, @timeout) do
      {:badrpc, :nodedown} -> {:error, :node_down}
      {:badrpc, :timeout} -> {:error, :timeout}
      {:badrpc, {:EXIT, {ex, _}}} -> {:error, {:exception, ex}}
      result -> {:ok, result}
    end
  end

  defp erpc_call(node, m, f, a) do
    try do
      {:ok, :erpc.call(node, m, f, a, @timeout)}
    catch
      :error, {:erpc, :noconnection} -> {:error, :node_down}
      :error, {:erpc, :timeout} -> {:error, :timeout}
      :error, {exception, _} -> {:error, {:exception, exception}}
    end
  end

  # Medir throughput bajo concurrencia
  def concurrent_benchmark(node, concurrency, calls_per_task) do
    {time, results} = :timer.tc(fn ->
      1..concurrency
      |> Enum.map(fn _ ->
        Task.async(fn ->
          Enum.map(1..calls_per_task, fn n ->
            compute_factorial(node, n)
          end)
        end)
      end)
      |> Task.await_many(30_000)
    end)

    total_calls = concurrency * calls_per_task
    %{
      total_ms: time / 1_000,
      total_calls: total_calls,
      calls_per_second: total_calls / (time / 1_000_000)
    }
  end
end
```

---

### Exercise 2: Fanout a Todos los Nodos

**Problem**: Implementa un sistema de "health check distribuido" que ejecuta una función de diagnóstico en todos los nodos del cluster simultáneamente y agrega los resultados. Debe funcionar correctamente cuando algunos nodos están caídos, son lentos, o retornan errores.

**Hints**:
- Usa `Node.list()` para obtener todos los nodos activos y añade `node()` para el local
- Las llamadas deben ser paralelas — no secuenciales con timeout multiplicado
- Define un timeout global para el fanout (no por nodo) con `Task.yield_many/2`
- Implementa un reducer que clasifique resultados en: `:ok`, `:timeout`, `:error`
- El nodo local puede ejecutar la función directamente sin RPC (más eficiente)

**One possible solution**:

```elixir
defmodule NodeDiagnostics do
  # Esta función se ejecuta en cada nodo
  def collect do
    %{
      node: node(),
      memory: :erlang.memory(:total),
      processes: length(Process.list()),
      uptime_ms: :erlang.statistics(:wall_clock) |> elem(0),
      schedulers: :erlang.system_info(:schedulers_online),
      message_queue_len: Process.info(self(), :message_queue_len) |> elem(1)
    }
  end
end

defmodule ClusterHealthCheck do
  @fanout_timeout 10_000
  @per_node_timeout 5_000

  def run do
    all_nodes = [node() | Node.list()]
    start_time = System.monotonic_time(:millisecond)

    tasks = Enum.map(all_nodes, fn target_node ->
      task = Task.async(fn -> execute_on_node(target_node) end)
      {target_node, task}
    end)

    # Esperar todos con timeout global
    results = tasks
    |> Enum.map(fn {target_node, task} ->
      case Task.yield(task, @fanout_timeout) || Task.shutdown(task) do
        {:ok, result} -> {target_node, result}
        nil -> {target_node, {:error, :timeout}}
      end
    end)

    elapsed = System.monotonic_time(:millisecond) - start_time

    %{
      total_nodes: length(all_nodes),
      elapsed_ms: elapsed,
      results: results,
      summary: summarize(results)
    }
  end

  defp execute_on_node(target_node) when target_node == node() do
    # Local: directo, sin RPC
    {:ok, NodeDiagnostics.collect()}
  end

  defp execute_on_node(target_node) do
    case :rpc.call(target_node, NodeDiagnostics, :collect, [], @per_node_timeout) do
      {:badrpc, reason} -> {:error, reason}
      result -> {:ok, result}
    end
  end

  defp summarize(results) do
    Enum.reduce(results, %{ok: [], timeout: [], error: []}, fn
      {node, {:ok, _data}}, acc -> Map.update!(acc, :ok, &[node | &1])
      {node, {:error, :timeout}}, acc -> Map.update!(acc, :timeout, &[node | &1])
      {node, {:error, _}}, acc -> Map.update!(acc, :error, &[node | &1])
    end)
  end
end
```

---

### Exercise 3: Comparación :rpc vs GenServer Directo

**Problem**: Implementa la misma operación de tres formas: (1) `:rpc.call`, (2) GenServer.call con `{module, node}`, y (3) send/receive directo con PID remoto. Benchmarkea las tres en un cluster real, documenta la diferencia de latencia, y define en qué casos cada una es superior.

**Hints**:
- La operación a comparar: un counter que se incrementa y retorna el nuevo valor
- Para el benchmark, usa `:timer.tc/1` con 10,000 iteraciones de cada método
- El método (3) requiere conocer el PID del proceso remoto — usa `:global` o `:rpc.call(node, Process, :whereis, [:counter])` para obtenerlo
- La diferencia entre (1) y (2) es principalmente el overhead del proceso `:rex`
- Documenta el trade-off: cuando (3) es 2x más rápido que (1), ¿qué perdemos? (no timeout, no monitor automático, etc.)

**One possible solution**:

```elixir
defmodule RemoteCounter do
  use GenServer

  def start_link(_) do
    GenServer.start_link(__MODULE__, 0, name: __MODULE__)
  end

  def increment_rpc(node) do
    :rpc.call(node, __MODULE__, :do_increment, [])
  end

  def increment_genserver(node) do
    GenServer.call({__MODULE__, node}, :increment)
  end

  def increment_direct(remote_pid) do
    ref = make_ref()
    send(remote_pid, {:increment, self(), ref})
    receive do
      {:increment_reply, ^ref, value} -> value
    after
      5_000 -> {:error, :timeout}
    end
  end

  # Función llamada via RPC
  def do_increment do
    GenServer.call(__MODULE__, :increment)
  end

  def init(n), do: {:ok, n}

  def handle_call(:increment, _from, n), do: {:reply, n + 1, n + 1}

  def handle_info({:increment, caller, ref}, n) do
    send(caller, {:increment_reply, ref, n + 1})
    {:noreply, n + 1}
  end
end

defmodule Benchmark do
  @iterations 10_000

  def run(remote_node) do
    # Obtener PID remoto para el método directo
    remote_pid = :rpc.call(remote_node, Process, :whereis, [RemoteCounter])

    results = %{
      rpc: measure(fn -> RemoteCounter.increment_rpc(remote_node) end),
      genserver: measure(fn -> RemoteCounter.increment_genserver(remote_node) end),
      direct: measure(fn -> RemoteCounter.increment_direct(remote_pid) end)
    }

    Enum.each(results, fn {method, {avg_us, total_ms}} ->
      IO.puts("#{method}: #{Float.round(avg_us, 2)}μs/op (#{total_ms}ms total)")
    end)

    results
  end

  defp measure(f) do
    {total_us, _} = :timer.tc(fn -> Enum.each(1..@iterations, fn _ -> f.() end) end)
    avg_us = total_us / @iterations
    {avg_us, total_us / 1_000}
  end
end
```

**Resultados típicos esperados** (varían según red y hardware):

| Método | Latencia típica | Overhead | Cuándo usarlo |
|--------|-----------------|----------|---------------|
| `:rpc.call` | 200-500μs | `:rex` process | Funciones stateless, admin |
| `GenServer.call` | 150-400μs | Monitor + lookup | GenServers con nombre |
| `send` directo | 80-200μs | Mínimo | PID conocido, alta frecuencia |

---

## Common Mistakes

**No manejar `{:badrpc, :timeout}` vs `{:badrpc, :nodedown}`**: Son errores distintos con respuestas distintas. Timeout puede ser transitorio — reintentar tiene sentido. Nodedown es persistente — fallar rápido y reportar. Siempre pattern match específicamente.

**Usar `:rpc` para alta concurrencia**: El proceso `:rex` es un único punto de concurrencia por nodo remoto. Si necesitas 1000 llamadas RPC concurrentes al mismo nodo, crea un pool de procesos que hagan llamadas directas, o usa `:erpc` que no tiene este cuello de botella.

**Olvidar que el proceso remoto sigue ejecutando tras timeout**: Esta es la trampa más seria. Si tu función en el nodo remoto tiene efectos secundarios (escribe a DB, modifica estado), puede ejecutar parcialmente y dejar la operación en estado inconsistente. Diseña las operaciones remotas para ser idempotentes o usa sagas con compensación.

**No incluir el nodo local en el fanout**: `Node.list()` no incluye `node()` — el nodo actual. Si haces fanout a `Node.list()` y olvidas añadir `node()`, el nodo local no ejecuta la operación.

**Pasar lambdas como argumentos de RPC**: `:rpc.call(node, Module, :function, [fn -> ... end])` falla porque las lambdas capturan el entorno local y no son serializable. Solo pasa datos simples (strings, numbers, maps, lists) como argumentos.

**Asumir que el módulo existe en el nodo remoto**: Si el release del nodo B no incluye `MyApp.SomeModule`, `:rpc.call` retornará `{:badrpc, {:EXIT, :undef}}`. En clusters heterogéneos o durante rolling deploys, esto es un problema real.

---

## Verification

```elixir
# Verifica :rpc básico en un cluster de dos nodos
remote_node = List.first(Node.list())

# Función simple sin efectos secundarios
result = :rpc.call(remote_node, String, :upcase, ["hello"])
assert result == "HELLO"

# Manejo de error: nodo inexistente
bad_result = :rpc.call(:"fantasma@localhost", IO, :puts, ["hi"])
assert {:badrpc, :nodedown} = bad_result

# Fanout a todos los nodos
all_nodes = [node() | Node.list()]
health = ClusterHealthCheck.run()
assert health.total_nodes == length(all_nodes)
assert health.summary.ok |> length() > 0

# Benchmark (verificar que compila y corre)
if length(Node.list()) > 0 do
  results = Benchmark.run(List.first(Node.list()))
  assert Map.has_key?(results, :rpc)
  assert Map.has_key?(results, :genserver)
  assert Map.has_key?(results, :direct)
end
```

---

## Summary

RPC en Erlang/Elixir tiene múltiples sabores con trade-offs claros:

- `:rpc.call` es el clásico — simple, funciona siempre, pero `:rex` es cuello de botella bajo concurrencia alta
- `:erpc.call` es el reemplazo moderno (OTP 23+) — mejor concurrencia, mejor manejo de errores
- `GenServer.call({module, node})` es la forma natural de llamar GenServers registrados en otros nodos
- `send(remote_pid, msg)` es el más rápido pero requiere conocer el PID y no tiene timeout automático
- Fan-out con `async_call` + `nb_yield` es el patrón para computación paralela en el cluster
- Siempre diseñar las operaciones remotas como idempotentes — los timeouts no cancelan la ejecución remota

---

## What's Next

- **Exercise 16**: ETS avanzado — almacenamiento en memoria compartida local, complemento del estado distribuido
- **Exercise 19**: Cache patterns con ETS — construir cachés eficientes que complementen la distribución
- **Exercise 43**: Build a Job Scheduler — aplicación práctica de RPC para distribución de trabajo

---

## Resources

- [:rpc module documentation](https://www.erlang.org/doc/man/rpc.html)
- [:erpc module documentation (OTP 23+)](https://www.erlang.org/doc/man/erpc.html)
- [Erlang RPC — Fred Hebert's "Stuff Goes Bad"](https://ferd.github.io/stuff-goes-bad/)
- [Distributed Elixir — The Little Elixir and OTP Guidebook](https://www.manning.com/books/the-little-elixir-and-otp-guidebook)
- [Why erpc? — OTP 23 release notes](https://www.erlang.org/news/145)
