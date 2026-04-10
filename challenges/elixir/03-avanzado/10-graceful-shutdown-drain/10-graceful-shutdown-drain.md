# 10 — Graceful Shutdown & Drain

**Difficulty**: Avanzado  
**Estimated time**: 90–120 min  
**Topics**: Production Operations, Shutdown Lifecycle, Process Draining, Mix.Release

---

## Context

Un servidor que cierra abruptamente cuando recibe SIGTERM tiene tres consecuencias en producción:
1. Requests en vuelo son cortados → errores 500 para los clientes
2. Transacciones a medias → inconsistencias en la base de datos
3. Mensajes en procesamiento se pierden → pérdida de datos

Graceful shutdown es el proceso de señalizar al sistema que "no aceptes más trabajo nuevo, termina el trabajo en vuelo, y luego ciérrrate limpiamente". En sistemas de alta disponibilidad (deploys zero-downtime, blue-green, rolling), esto es un requisito, no un nice-to-have.

Elixir y OTP tienen mecanismos específicos para implementar esto correctamente. El reto es entender el orden de eventos y los puntos de extensión disponibles.

---

## Concepts

### El Ciclo de Shutdown en OTP

```
SIGTERM recibido
    ↓
Application.stop/1 (callback)
    ↓
Supervisor termina children en orden inverso al startup
    ↓
Cada child recibe señal de shutdown:
    - Supervisor hijo: propaga a sus propios children
    - Worker (GenServer): llama terminate/2
    ↓
Si el child no termina en `shutdown` ms → :brutal_kill
    ↓
Application.prep_stop/1 (callback, antes del stop de children)
    ↓
Sistema termina
```

El punto clave: el tiempo que tiene cada proceso para hacer su cleanup está definido por el campo `:shutdown` de su child spec.

---

### `:shutdown` en Child Spec

```elixir
def child_spec(opts) do
  %{
    id: __MODULE__,
    start: {__MODULE__, :start_link, [opts]},
    restart: :permanent,
    shutdown: 5_000,       # ms para hacer cleanup antes de ser matado
    # shutdown: :brutal_kill  # sin tiempo de cleanup — muerte inmediata
    # shutdown: :infinity     # espera indefinidamente (¡peligroso!)
    type: :worker
  }
end
```

**Valores**:
- `5_000` (default) — 5 segundos de cleanup
- `:brutal_kill` — termina inmediatamente, sin llamar `terminate/2`
- `:infinity` — sin timeout (solo para supervisores que deben esperar a todos sus children)

Para supervisores, el default es `:infinity` — correcto, porque el supervisor debe esperar a que todos sus children terminen.

---

### `terminate/2` en GenServer

```elixir
defmodule MyServer do
  use GenServer

  # Solo se llama si:
  # 1. El proceso tiene :trap_exit activado, O
  # 2. El supervisor envía un shutdown signal (no un brutal_kill)
  def terminate(reason, state) do
    # Hacer cleanup:
    # - Cerrar conexiones
    # - Flush buffers
    # - Log del shutdown
    # - Completar transacciones pendientes

    Logger.info("#{__MODULE__} terminating: #{inspect(reason)}")
    # No es necesario retornar nada específico
    :ok
  end
end
```

**Importante**: `terminate/2` NO es llamado en todas las situaciones:
- Crash por excepción no capturada → `terminate/2` puede no ejecutar
- `:brutal_kill` → `terminate/2` no ejecuta nunca
- Exit signal de otro proceso → solo ejecuta si `:trap_exit` está activo

Para garantizar que `terminate/2` ejecuta en el shutdown normal del supervisor, el proceso debe estar bajo un supervisor y el `:shutdown` no debe ser `:brutal_kill`.

---

### Process.flag(:trap_exit, true)

```elixir
def init(opts) do
  # Activar trap_exit hace que los exit signals lleguen como mensajes
  # en lugar de terminar el proceso
  Process.flag(:trap_exit, true)
  {:ok, initial_state(opts)}
end

def handle_info({:EXIT, _from, reason}, state) do
  # Recibimos exit signal como mensaje — podemos hacer cleanup
  Logger.info("Got EXIT signal: #{inspect(reason)}")
  {:stop, reason, state}
end
```

**Cuándo usar**: solo en procesos que necesitan reaccionar a la muerte de procesos enlazados, o en supervisores. Para GenServers normales, `terminate/2` es suficiente.

---

### SIGTERM en Elixir

En producción con `mix release`, SIGTERM es manejado automáticamente por la VM de Erlang:

```bash
# Shutdown graceful (SIGTERM)
bin/myapp stop

# Shutdown abrupto (SIGKILL — sin cleanup)
kill -9 $(bin/myapp pid)
```

La VM envía SIGTERM a todos los procesos del sistema Erlang de forma orquestada. El timeout total está configurado en `config/runtime.exs` o en el release:

```elixir
# config/runtime.exs
config :myapp, :shutdown_timeout, 30_000  # 30 segundos total

# rel/overlays/bin/myapp (en releases)
# La VM tiene su propio shutdown timeout: RELEASE_SHUTDOWN_TIMEOUT
```

---

### Mix.Release y Graceful Shutdown

```elixir
# mix.exs
def project do
  [
    releases: [
      myapp: [
        steps: [:assemble],
        # Tiempo que la VM espera en el shutdown (suma de todos los terminates)
        # Si un proceso tarda más que su :shutdown, será brutal_killed
      ]
    ]
  ]
end
```

En containers (Kubernetes), el shutdown flow es:
1. Pod recibe señal de terminación
2. k8s envía SIGTERM al proceso 1 (la VM Erlang)
3. La VM inicia el shutdown graceful
4. Si en `terminationGracePeriodSeconds` no terminó → k8s envía SIGKILL

Configuración correcta: `terminationGracePeriodSeconds` debe ser mayor que el shutdown timeout más largo de tus procesos.

---

## Exercise 1 — HTTP Server Graceful Shutdown

### Problem

Implementa un servidor HTTP (simulado con GenServer) que, al recibir una señal de shutdown:
1. Deja de aceptar nuevas conexiones inmediatamente
2. Espera a que todas las conexiones en vuelo terminen (con timeout)
3. Cierra limpiamente

```elixir
defmodule MyApp.HTTPServer do
  use GenServer
  require Logger

  @shutdown_timeout 30_000  # 30s para que requests en vuelo terminen

  # Estado: %{
  #   accepting: boolean — ¿aceptando nuevas conexiones?
  #   active_requests: MapSet de request_ids en vuelo
  #   shutdown_from: nil | pid — si estamos en shutdown, quién espera
  # }

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # API pública
  def handle_request(request_id, handler_fn) do
    GenServer.call(__MODULE__, {:new_request, request_id, handler_fn})
  end

  def finish_request(request_id) do
    GenServer.cast(__MODULE__, {:request_done, request_id})
  end

  def init(_opts) do
    Process.flag(:trap_exit, true)
    {:ok, %{accepting: true, active_requests: MapSet.new(), shutdown_from: nil}}
  end

  def handle_call({:new_request, request_id, handler_fn}, _from, state) do
    cond do
      not state.accepting ->
        {:reply, {:error, :server_draining}, state}

      true ->
        # Lanzar la request en background
        # Cuando termine, debe llamar finish_request/1
        Task.start(fn ->
          handler_fn.()
          finish_request(request_id)
        end)

        new_state = %{state | active_requests: MapSet.put(state.active_requests, request_id)}
        {:reply, {:ok, request_id}, new_state}
    end
  end

  def handle_cast({:request_done, request_id}, state) do
    new_active = MapSet.delete(state.active_requests, request_id)
    new_state = %{state | active_requests: new_active}

    # Si estamos en shutdown y ya no hay requests activas → terminar
    if state.shutdown_from && MapSet.size(new_active) == 0 do
      # Tu implementación aquí — notificar que podemos terminar
    end

    {:noreply, new_state}
  end

  def terminate(reason, state) do
    # Tu implementación aquí — drain de requests en vuelo
    Logger.info("HTTPServer shutting down. Reason: #{inspect(reason)}")
    Logger.info("Active requests: #{MapSet.size(state.active_requests)}")

    # Si hay requests activas, esperar hasta @shutdown_timeout
    # Si pasan @shutdown_timeout ms, terminar de todas formas

    :ok
  end
end
```

**Tu tarea**:
1. Implementa `terminate/2` con drain de requests activas
2. Implementa el mecanismo de notificación cuando todas las requests terminan
3. Añade un timeout en `terminate/2`: si en 30s no vaciaron, cierra igual
4. Asegúrate de que el child spec tiene un `:shutdown` apropiado para el timeout de drain

### Hints

<details>
<summary>Hint 1 — Bloquear en terminate/2 con receive</summary>

`terminate/2` puede bloquear con `receive` mientras espera que las requests activas terminen. Cuando `handle_cast({:request_done, ...})` detecta que estamos en shutdown y `active_requests` está vacío, envía un mensaje al proceso para desbloquearlo.

```elixir
def terminate(_reason, state) do
  if MapSet.size(state.active_requests) > 0 do
    receive do
      :all_requests_done -> :ok
    after
      @shutdown_timeout -> Logger.warning("Shutdown timeout — dropping #{MapSet.size(state.active_requests)} requests")
    end
  end
  :ok
end
```

El problema: `terminate/2` ejecuta en el mismo proceso que maneja los mensajes. Si bloqueas con `receive`, el proceso no puede procesar los `handle_cast` de `request_done`. Necesitas una estrategia diferente.

</details>

<details>
<summary>Hint 2 — Usar un proceso auxiliar para el drain</summary>

Una forma de resolver el deadlock de terminate/receive: spawna un proceso auxiliar que haga el blocking wait, y el GenServer sigue procesando mensajes hasta que el auxiliar notifica que terminó.

Pero esto complica la semántica de shutdown. Alternativa más simple: en vez de bloquear en `terminate/2`, usar el mecanismo de shutdown de OTP correctamente:

1. El supervisor envía `:shutdown` signal al GenServer
2. Como el GenServer tiene `trap_exit: true`, recibe `{:EXIT, supervisor_pid, :shutdown}` como mensaje
3. Procesas el shutdown en `handle_info({:EXIT, ...})` con tiempo disponible
4. Llamas `{:stop, :shutdown, new_state}` cuando estás listo

</details>

<details>
<summary>Hint 3 — Child spec con shutdown largo</summary>

```elixir
def child_spec(opts) do
  %{
    id: __MODULE__,
    start: {__MODULE__, :start_link, [opts]},
    restart: :permanent,
    shutdown: 35_000,  # 5s más que @shutdown_timeout para margen
    type: :worker
  }
end
```

Si el `:shutdown` del child spec es menor que el tiempo que necesitas para el drain, el supervisor hará `:brutal_kill` antes de que termines el cleanup.

</details>

### One Possible Solution

<details>
<summary>Ver solución (intenta resolverlo primero)</summary>

```elixir
defmodule MyApp.HTTPServer do
  use GenServer
  require Logger

  @drain_timeout 30_000

  def child_spec(opts) do
    %{
      id: __MODULE__,
      start: {__MODULE__, :start_link, [opts]},
      restart: :permanent,
      shutdown: @drain_timeout + 5_000,  # margen de 5s sobre el drain timeout
      type: :worker
    }
  end

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def handle_request(request_id, handler_fn) do
    GenServer.call(__MODULE__, {:new_request, request_id, handler_fn})
  end

  def finish_request(request_id) do
    GenServer.cast(__MODULE__, {:request_done, request_id})
  end

  def init(_opts) do
    Process.flag(:trap_exit, true)
    {:ok, %{accepting: true, active_requests: MapSet.new(), shutdown_from: nil}}
  end

  def handle_call({:new_request, _request_id, _handler_fn}, _from, %{accepting: false} = state) do
    {:reply, {:error, :server_draining}, state}
  end

  def handle_call({:new_request, request_id, handler_fn}, _from, state) do
    server = self()
    Task.start(fn ->
      handler_fn.()
      send(server, {:request_done, request_id})
    end)

    {:reply, {:ok, request_id},
     %{state | active_requests: MapSet.put(state.active_requests, request_id)}}
  end

  # Recibir via send (no cast) para que funcione dentro de terminate
  def handle_info({:request_done, request_id}, state) do
    new_active = MapSet.delete(state.active_requests, request_id)

    if state.shutdown_from && MapSet.size(new_active) == 0 do
      send(state.shutdown_from, :drain_complete)
    end

    {:noreply, %{state | active_requests: new_active}}
  end

  def handle_info({:EXIT, _from, reason}, state) do
    Logger.info("HTTPServer received EXIT signal: #{inspect(reason)}")
    # Dejar de aceptar requests nuevas
    state = %{state | accepting: false}

    if MapSet.size(state.active_requests) == 0 do
      {:stop, reason, state}
    else
      Logger.info("Draining #{MapSet.size(state.active_requests)} active requests...")
      # Registrar quién espera el drain (self — usamos un proceso auxiliar)
      drain_pid = self()
      state = %{state | shutdown_from: drain_pid}

      # Esperar el drain con timeout en un proceso auxiliar
      # que luego llama a stop
      parent = self()
      spawn(fn ->
        receive do
          :drain_complete ->
            Logger.info("All requests drained, shutting down")
            GenServer.stop(parent, reason)
        after
          @drain_timeout ->
            Logger.warning("Drain timeout exceeded, forcing shutdown")
            GenServer.stop(parent, reason)
        end
      end)

      {:noreply, state}
    end
  end

  def terminate(reason, state) do
    Logger.info("HTTPServer terminated. Reason: #{inspect(reason)}. " <>
                "Remaining requests: #{MapSet.size(state.active_requests)}")
    :ok
  end
end
```

</details>

---

## Exercise 2 — GenServer Drain: Procesar Cola Antes de Terminar

### Problem

Un `WorkQueue` GenServer acumula trabajo pendiente. Al recibir shutdown, debe:
1. Dejar de aceptar trabajo nuevo
2. Procesar todo el trabajo pendiente en la cola
3. Solo entonces terminar

```elixir
defmodule MyApp.WorkQueue do
  use GenServer
  require Logger

  # Estado: %{queue: :queue.t(), accepting: boolean, processing: boolean}

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def enqueue(work_item) do
    GenServer.call(__MODULE__, {:enqueue, work_item})
  end

  def init(_opts) do
    Process.flag(:trap_exit, true)
    {:ok, %{queue: :queue.new(), accepting: true, processing: false}}
  end

  def handle_call({:enqueue, _item}, _from, %{accepting: false} = state) do
    {:reply, {:error, :queue_draining}, state}
  end

  def handle_call({:enqueue, item}, _from, state) do
    new_queue = :queue.in(item, state.queue)
    new_state = %{state | queue: new_queue}

    # Si no estamos procesando, empezar
    unless state.processing do
      send(self(), :process_next)
    end

    {:reply, :ok, %{new_state | processing: true}}
  end

  def handle_info(:process_next, state) do
    case :queue.out(state.queue) do
      {:empty, _queue} ->
        # Cola vacía — ¿estamos en shutdown?
        # Tu implementación aquí

      {{:value, item}, rest_queue} ->
        # Procesar el item
        process_item(item)
        send(self(), :process_next)
        {:noreply, %{state | queue: rest_queue}}
    end
  end

  def handle_info({:EXIT, _from, _reason}, state) do
    Logger.info("WorkQueue draining before shutdown. Items pending: #{:queue.len(state.queue)}")
    # Tu implementación aquí — dejar de aceptar, terminar cuando cola vacía
  end

  defp process_item(item) do
    Logger.info("Processing: #{inspect(item)}")
    Process.sleep(100)  # simula trabajo
  end
end
```

**Tu tarea**:
1. Implementa el drain en `handle_info({:EXIT, ...})` y en `handle_info(:process_next)`
2. El proceso debe procesar TODA la cola antes de terminar
3. Añade un timeout máximo de drain: si la cola no vacía en 60s, termina con warning
4. Implementa `MyApp.WorkQueue.child_spec/1` con shutdown apropiado

### Hints

<details>
<summary>Hint 1 — Estado de "draining"</summary>

Añade un campo `:draining` al state. Cuando el proceso recibe la señal de EXIT, setea `:draining: true` y `:accepting: false`. En `handle_info(:process_next)`, cuando la cola queda vacía y `:draining: true`, llamas `{:stop, :shutdown, state}`.

</details>

<details>
<summary>Hint 2 — Timeout de drain con Process.send_after</summary>

```elixir
def handle_info({:EXIT, _from, reason}, state) do
  Process.send_after(self(), :drain_timeout, 60_000)
  {:noreply, %{state | accepting: false, draining: true}}
end

def handle_info(:drain_timeout, state) do
  Logger.warning("Drain timeout. #{:queue.len(state.queue)} items lost.")
  {:stop, :shutdown, state}
end
```

</details>

<details>
<summary>Hint 3 — :queue en Erlang</summary>

```elixir
queue = :queue.new()
queue = :queue.in(item, queue)    # añadir al final (FIFO)

case :queue.out(queue) do
  {{:value, item}, rest} -> process item, continue with rest
  {:empty, _}            -> queue is empty
end

:queue.len(queue)  # número de elementos
```

</details>

---

## Exercise 3 — Database Connection Pool Cleanup

### Problem

Un pool de conexiones a PostgreSQL debe, en shutdown:
1. Dejar de entregar nuevas conexiones (checkout returns error)
2. Esperar a que todos los checkouts activos sean devueltos (checkin)
3. Cerrar todas las conexiones limpiamente (no abruptamente)
4. Si alguna conexión no es devuelta en tiempo, forzar el cierre

```elixir
defmodule MyApp.DBPool do
  use GenServer
  require Logger

  @checkin_timeout 10_000  # 10s para que los checkins pendientes lleguen

  # Estado: %{
  #   connections: [conn],           # conexiones disponibles
  #   checked_out: %{ref => conn},   # conexiones en uso
  #   accepting: boolean,
  #   shutdown: boolean
  # }

  def start_link(opts) do
    pool_size = Keyword.get(opts, :pool_size, 5)
    GenServer.start_link(__MODULE__, pool_size, name: __MODULE__)
  end

  def checkout() do
    GenServer.call(__MODULE__, :checkout, 5_000)
  end

  def checkin(ref) do
    GenServer.cast(__MODULE__, {:checkin, ref})
  end

  def init(pool_size) do
    Process.flag(:trap_exit, true)
    connections = Enum.map(1..pool_size, &open_connection/1)
    {:ok, %{connections: connections, checked_out: %{}, accepting: true, shutdown: false}}
  end

  def handle_call(:checkout, _from, %{accepting: false} = state) do
    {:reply, {:error, :pool_shutting_down}, state}
  end

  def handle_call(:checkout, _from, state) do
    case state.connections do
      [] ->
        {:reply, {:error, :pool_empty}, state}

      [conn | rest] ->
        ref = make_ref()
        new_state = %{state |
          connections: rest,
          checked_out: Map.put(state.checked_out, ref, conn)
        }
        {:reply, {:ok, ref, conn}, new_state}
    end
  end

  def handle_cast({:checkin, ref}, state) do
    # Tu implementación aquí
    # 1. Devolver la conexión al pool
    # 2. Si estamos en shutdown y checked_out está vacío → terminar
  end

  def handle_info({:EXIT, _from, _reason}, state) do
    Logger.info("DBPool shutdown initiated. Connections checked out: #{map_size(state.checked_out)}")
    # Tu implementación aquí
  end

  defp open_connection(n) do
    %{id: n, status: :connected}  # simulado
  end

  defp close_connection(conn) do
    Logger.info("Closing connection #{conn.id}")
    # En producción: :pgsql.close(conn) o Postgrex.disconnect(conn)
  end
end
```

**Tu tarea**:
1. Implementa `handle_cast({:checkin, ...})` con lógica de shutdown
2. Implementa `handle_info({:EXIT, ...})` para iniciar el drain del pool
3. Cierra todas las conexiones disponibles inmediatamente en shutdown (las checked-out se cierran cuando se devuelven)
4. Añade timeout: si en 10s alguna conexión no fue devuelta, ciérrala forzosamente

### Hints

<details>
<summary>Hint 1 — Cerrar conexiones disponibles inmediatamente</summary>

En el momento del shutdown, las conexiones que están en `state.connections` (no checked out) se pueden cerrar inmediatamente — nadie las está usando. Solo necesitas esperar por las conexiones en `state.checked_out`.

```elixir
def handle_info({:EXIT, _from, reason}, state) do
  # Cerrar las disponibles ahora mismo
  Enum.each(state.connections, &close_connection/1)

  if map_size(state.checked_out) == 0 do
    {:stop, reason, %{state | connections: [], accepting: false}}
  else
    Process.send_after(self(), :force_close, @checkin_timeout)
    {:noreply, %{state | connections: [], accepting: false, shutdown: true}}
  end
end
```

</details>

<details>
<summary>Hint 2 — Forzar cierre de conexiones huérfanas</summary>

```elixir
def handle_info(:force_close, state) do
  if map_size(state.checked_out) > 0 do
    Logger.warning("Force closing #{map_size(state.checked_out)} connections that were not checked in")
    Enum.each(state.checked_out, fn {_ref, conn} -> close_connection(conn) end)
  end
  {:stop, :shutdown, %{state | checked_out: %{}}}
end
```

</details>

<details>
<summary>Hint 3 — Notificar a los callers de checkout fallido durante shutdown</summary>

Los procesos que tienen conexiones checked-out durante el shutdown deben devolverlas. Pero ¿cómo saben que el pool se está apagando?

Una opción: el pool monitorea los procesos que hacen checkout (`Process.monitor/1`). Si el proceso muere sin hacer checkin, el pool cierra la conexión automáticamente. Esto es exactamente lo que hace Ecto's pool.

</details>

---

## Common Mistakes

### Mistake 1 — `:brutal_kill` para todos los workers

```elixir
def child_spec(opts) do
  %{
    id: __MODULE__,
    start: {__MODULE__, :start_link, [opts]},
    shutdown: :brutal_kill  # ← "rápido y fácil"
  }
end

# Consecuencias:
# - terminate/2 nunca se llama
# - Requests en vuelo cortadas abruptamente
# - Conexiones no cerradas → pool exhaustion en el próximo deploy
# - Mensajes en procesamiento perdidos
```

`:brutal_kill` solo es apropiado para procesos que verdaderamente no tienen estado que limpiar (e.g., workers CPU-bound sin I/O ni estado externo).

---

### Mistake 2 — Olvidar configurar Process.flag(:trap_exit, true)

```elixir
defmodule MyServer do
  use GenServer

  def init(_) do
    # Sin trap_exit, cuando el supervisor envía :shutdown,
    # el proceso recibe un exit signal y muere sin llamar terminate/2
    # si el motivo no es :normal o :shutdown
    {:ok, %{}}
  end

  # Este terminate PUEDE no ejecutar si el shutdown reason es inusual
  def terminate(reason, state) do
    cleanup(state)
  end
end
```

Para garantizar que `terminate/2` siempre ejecuta durante el shutdown normal del supervisor, activa `Process.flag(:trap_exit, true)` en `init/1`.

---

### Mistake 3 — Shutdown timeout menor que el trabajo en vuelo

```elixir
def child_spec(opts) do
  %{
    id: __MODULE__,
    start: {__MODULE__, :start_link, [opts]},
    shutdown: 1_000  # 1 segundo
  }
end

# Si una request HTTP tarda 5 segundos en completarse:
# → el supervisor espera 1s
# → brutal_kill al proceso
# → request cortada, posible inconsistencia en DB
```

Regla: el `:shutdown` debe ser mayor que el tiempo máximo que puede tardar tu cleanup. Para un HTTP server que sirve requests de hasta 30s, el shutdown debería ser al menos 35s.

---

### Mistake 4 — Hacer trabajo costoso en terminate/2

```elixir
def terminate(_reason, state) do
  # MAL: enviar emails de notificación en terminate
  Enum.each(state.pending_emails, &Mailer.send_now/1)  # puede tardar mucho

  # MAL: hacer queries a DB en terminate cuando DB pool también está cerrando
  Repo.insert(%AuditLog{event: "server_shutdown"})
end
```

`terminate/2` debe ser rápido y no depender de otros procesos que también están en proceso de shutdown. Las dependencias entre terminates son frágiles porque el orden de shutdown puede cambiar.

---

## Production Patterns

### Pattern 1 — Shutdown hook en Application

```elixir
defmodule MyApp.Application do
  use Application

  def start(_type, _args) do
    children = [...]
    Supervisor.start_link(children, strategy: :one_for_one)
  end

  # Llamado ANTES de que los supervisores terminen sus children
  def prep_stop(state) do
    Logger.info("Application shutdown initiated at #{DateTime.utc_now()}")
    # Alertar a sistema de monitoreo: "comenzando shutdown graceful"
    Telemetry.execute([:myapp, :shutdown, :started], %{}, %{})
    state
  end

  # Llamado DESPUÉS de que todos los supervisores terminaron
  def stop(_state) do
    Logger.info("Application shutdown complete")
    Telemetry.execute([:myapp, :shutdown, :complete], %{}, %{})
  end
end
```

### Pattern 2 — Health check que reporta "draining"

```elixir
defmodule MyApp.HealthController do
  def check(conn, _params) do
    case MyApp.HTTPServer.status() do
      :accepting ->
        json(conn, %{status: "ok"})

      :draining ->
        # Kubernetes lee esto y deja de enviar tráfico nuevo
        conn
        |> put_status(503)
        |> json(%{status: "draining"})
    end
  end
end
```

### Pattern 3 — Kubernetes terminationGracePeriodSeconds

```yaml
# kubernetes deployment
spec:
  template:
    spec:
      terminationGracePeriodSeconds: 60  # debe ser > shutdown timeout de la app
      containers:
        - name: myapp
          lifecycle:
            preStop:
              exec:
                # Dar tiempo a que el load balancer deje de enviar tráfico
                command: ["/bin/sleep", "5"]
```

El `preStop` hook introduce un delay antes de que el proceso reciba SIGTERM. Esto da tiempo al load balancer para remover el pod de la rotación antes de que comience el shutdown, evitando nuevas conexiones durante el drain.

---

## Resources

- [GenServer.terminate/2 — HexDocs](https://hexdocs.pm/elixir/GenServer.html#c:terminate/2)
- [Application behaviour callbacks — HexDocs](https://hexdocs.pm/elixir/Application.html)
- [Mix.Release — HexDocs](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
- [Erlang OTP — Shutdown](https://www.erlang.org/doc/design_principles/sup_princ.html#shutdown)
- [Kubernetes Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)
- [Zero-Downtime Deploys with Elixir — Saša Jurić](https://www.theerlangelist.com/)
