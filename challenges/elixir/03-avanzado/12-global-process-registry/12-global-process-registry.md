# 12. Global Process Registry

**Difficulty**: Avanzado

---

## Prerequisites

- Ejercicio 11: Distributed Erlang Clustering
- GenServer avanzado (ejercicios 01-05)
- Conceptos de leader election y consensus básico
- Comprensión de netsplits y sus consecuencias

---

## Learning Objectives

- Registrar procesos globalmente en un cluster con `:global`
- Resolver conflictos de registro con callbacks personalizados
- Implementar singleton processes que sobrevivan a la migración de nodos
- Usar `:pg` para process groups distribuidos
- Entender las limitaciones de `:global` y cuándo usarlo vs alternativas
- Implementar leader election básica sobre `:global`

---

## Concepts

### El módulo :global

`:global` es el módulo OTP para registro de nombres a nivel de cluster. A diferencia del registro local (`Process.register/2`), los nombres registrados con `:global` son visibles desde cualquier nodo del cluster.

```elixir
# Registrar el proceso actual con un nombre global
:global.register_name(:my_singleton, self())
#=> :yes  # registro exitoso
#=> :no   # ya existe un proceso con ese nombre

# Buscar un proceso por nombre global
:global.whereis_name(:my_singleton)
#=> #PID<1.234.0>   # el PID, posiblemente en otro nodo
#=> :undefined      # no encontrado

# Eliminar un registro
:global.unregister_name(:my_singleton)

# Enviar mensaje a un proceso por nombre global
:global.send(:my_singleton, {:hello, self()})
```

**Cómo funciona internamente**: `:global` usa un lock distribuido y sincronización entre nodos para garantizar que solo un proceso tiene un nombre dado. Cuando un nodo nuevo se une al cluster, sincroniza su tabla de registros.

### Resolución de conflictos

El problema fundamental de `:global`: durante un netsplit, dos particiones pueden registrar el mismo nombre independientemente. Cuando la red se recupera, hay un conflicto.

```elixir
# Sin callback — comportamiento por defecto
# :global mata aleatoriamente uno de los dos procesos
:global.register_name(:leader, self())

# Con callback de resolución
resolve = fn name, pid1, pid2 ->
  # Debe retornar el PID ganador, o :none para matar ambos
  # pid1 y pid2 son los procesos en conflicto
  IO.puts("Conflict for #{name}: #{inspect(pid1)} vs #{inspect(pid2)}")
  pid1  # siempre ganar el primero (determinístico por nodo)
end

:global.register_name(:leader, self(), resolve)
```

El callback se llama en el nodo que detecta el conflicto. El PID retornado "gana" y el otro proceso recibe un mensaje `{:global_name_conflict, name}` — el proceso perdedor debe manejarlo.

### Pattern: via tuple con :global

Puedes usar `:global` como backend del mecanismo `{:via, module, name}` de GenServer:

```elixir
defmodule MySingleton do
  use GenServer

  # Arrancar con registro global automático
  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts,
      name: {:global, __MODULE__}
    )
  end

  # Las llamadas usan la via tuple automáticamente
  def get_state do
    GenServer.call({:global, __MODULE__}, :get_state)
  end
end
```

Con `name: {:global, __MODULE__}`, GenServer usa `:global.register_name/2` internamente. Si el proceso muere, el registro se limpia. Las llamadas con `{:global, name}` resuelven automáticamente el nodo correcto.

### Limitaciones críticas de :global

**1. No tolera netsplits bien**: `:global` es CP (consistency over availability). Durante un netsplit, puede rechazar nuevos registros o bloquearse esperando consensus.

**2. Sincronización bloqueante**: Cuando un nodo nuevo se une, hay un período de bloqueo mientras se sincronizan los registros. En clusters grandes o con muchos registros, esto puede ser lento.

**3. Sin garantía de orden**: Los callbacks de resolución pueden llamarse en orden diferente en distintos nodos.

**4. Dead lock potencial**: Si dos nodos intentan registrar el mismo nombre simultáneamente, puede haber deadlock temporal.

**Cuándo usar :global**:
- Clusters pequeños (< 20 nodos)
- Procesos singleton donde la disponibilidad durante netsplit no es crítica
- Leader election con semántica "solo un líder a la vez"
- Prototipado rápido

**Cuándo NO usar :global**:
- Alta disponibilidad requerida durante netsplits
- Muchos registros dinámicos (hundreds/thousands)
- Alta frecuencia de cambios en el cluster

### El módulo :pg — Process Groups

`:pg` (process groups) es diferente a `:global`: en lugar de mapear un nombre a un proceso, mapea un nombre a un **grupo de procesos**. Un proceso puede pertenecer a múltiples grupos.

```elixir
# Crear/unirse a un grupo
:pg.join(:my_scope, :my_group, self())
:pg.join(:my_scope, :workers, self())

# Obtener todos los procesos en un grupo (en todos los nodos)
:pg.get_members(:my_scope, :my_group)
#=> [#PID<0.234.0>, #PID<1.345.0>, #PID<2.456.0>]

# Solo procesos locales
:pg.get_local_members(:my_scope, :my_group)
#=> [#PID<0.234.0>]

# Salir de un grupo
:pg.leave(:my_scope, :my_group, self())
```

El "scope" es un proceso OTP que gestiona el grupo. En Phoenix y muchas librerías, el scope es un módulo configurado en la aplicación:

```elixir
# En application.ex
children = [
  {Phoenix.PubSub, name: MyApp.PubSub},
  # :pg internamente
]

# O directamente:
children = [
  {Registry, keys: :duplicate, name: MyApp.PG}
]
```

**Casos de uso de :pg**:
- Fan-out de mensajes a múltiples trabajadores
- Tracking de usuarios conectados por sala
- Load balancing con selección aleatoria del grupo
- Suscripciones a eventos distribuidos

### Leader Election con :global

Un patrón común: usar `:global.register_name/3` como primitiva de election. El proceso que logra registrar el nombre es el líder.

```elixir
defmodule LeaderElection do
  use GenServer
  require Logger

  @election_interval 5_000

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def am_i_leader? do
    GenServer.call(__MODULE__, :am_i_leader)
  end

  def current_leader do
    case :global.whereis_name(:cluster_leader) do
      :undefined -> nil
      pid -> {pid, node(pid)}
    end
  end

  def init(_opts) do
    schedule_election()
    {:ok, %{leader: false}}
  end

  def handle_info(:attempt_election, state) do
    result = :global.register_name(
      :cluster_leader,
      self(),
      &resolve_conflict/3
    )

    new_state = case result do
      :yes ->
        Logger.info("[Leader] I am the new leader on node #{node()}")
        %{state | leader: true}
      :no ->
        %{state | leader: false}
    end

    schedule_election()
    {:noreply, new_state}
  end

  # Cuando perdemos el liderazgo por conflicto
  def handle_info({:global_name_conflict, :cluster_leader}, state) do
    Logger.warning("[Leader] Lost leadership due to conflict")
    {:noreply, %{state | leader: false}}
  end

  def handle_call(:am_i_leader, _from, state) do
    {:reply, state.leader, state}
  end

  # Estrategia: el nodo con menor nombre lexicográfico gana
  defp resolve_conflict(_name, pid1, pid2) do
    if node(pid1) <= node(pid2), do: pid1, else: pid2
  end

  defp schedule_election do
    Process.send_after(self(), :attempt_election, @election_interval)
  end
end
```

---

## Exercises

### Exercise 1: Singleton Global en Cluster

**Problem**: Implementa un `ConfigServer` que actúa como fuente única de verdad para configuración dinámica del cluster. Solo debe existir una instancia en todo el cluster. Si el nodo donde corre se cae, debe reiniciarse automáticamente en otro nodo.

**Hints**:
- Registra con `name: {:global, __MODULE__}` para el registro automático
- Para restart automático en otro nodo, el supervisor del nodo A necesita saber que el proceso de nodo B murió — esto requiere `:net_kernel.monitor_nodes` combinado con supervisión
- Considera usar un supervisor en cada nodo que intente arrancar el singleton — `:global.register_name` retornará `:no` en el nodo que pierde y ese proceso debe terminar limpiamente
- La clave es el `resolve_conflict` callback para el caso de netsplit recovery

**One possible solution**:

```elixir
defmodule ConfigServer do
  use GenServer

  def start_link(initial_config) do
    case GenServer.start_link(__MODULE__, initial_config,
           name: {:global, __MODULE__}) do
      {:ok, pid} ->
        {:ok, pid}
      {:error, {:already_started, pid}} ->
        # Ya existe en otro nodo — esto es correcto para un singleton
        :ignore
    end
  end

  def get(key) do
    GenServer.call({:global, __MODULE__}, {:get, key})
  end

  def put(key, value) do
    GenServer.call({:global, __MODULE__}, {:put, key, value})
  end

  def init(config), do: {:ok, config}

  def handle_call({:get, key}, _from, config) do
    {:reply, Map.get(config, key), config}
  end

  def handle_call({:put, key, value}, _from, config) do
    {:reply, :ok, Map.put(config, key, value)}
  end
end

# Supervisor que intenta mantener el singleton vivo en este nodo
# si el nodo que lo tenía se cae:
defmodule ConfigServerSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(config) do
    children = [{ConfigServer, config}]
    # :transient: solo restart si termina anormalmente
    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

---

### Exercise 2: Leader Election con :global

**Problem**: Implementa un sistema de leader election donde el líder ejecuta tareas periódicas (ej: cleanup, métricas agregadas). Cuando el líder se cae, uno de los followers debe asumir el liderazgo automáticamente. Incluye un mecanismo para que los followers puedan consultar quién es el líder actual.

**Hints**:
- El truco está en combinar `monitor_nodes` con reintentos de registro
- Cuando detectas `{:nodedown, leader_node}`, intenta registrar el nombre global inmediatamente
- El `resolve_conflict` debe ser determinístico para evitar elecciones oscilantes — usa el nombre del nodo o el uptime
- Los followers deben seguir intentando cada N segundos por si el líder falla silenciosamente
- El líder debe monitorear el proceso de verificación: si `:global.whereis_name` retorna otro PID, abdicó

**One possible solution**:

```elixir
defmodule ClusterLeader do
  use GenServer
  require Logger

  @retry_interval 2_000
  @leader_name :cluster_leader

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def leader_node do
    case :global.whereis_name(@leader_name) do
      :undefined -> nil
      pid -> node(pid)
    end
  end

  def am_i_leader?, do: GenServer.call(__MODULE__, :am_i_leader)

  def init(_opts) do
    :net_kernel.monitor_nodes(true)
    send(self(), :attempt_leadership)
    {:ok, %{is_leader: false, task_timer: nil}}
  end

  def handle_info(:attempt_leadership, state) do
    new_state = try_become_leader(state)
    Process.send_after(self(), :attempt_leadership, @retry_interval)
    {:noreply, new_state}
  end

  def handle_info(:leader_task, %{is_leader: true} = state) do
    # Solo el líder ejecuta esto
    Logger.info("[Leader] Running periodic task on #{node()}")
    timer = Process.send_after(self(), :leader_task, 5_000)
    {:noreply, %{state | task_timer: timer}}
  end

  def handle_info(:leader_task, state), do: {:noreply, state}

  def handle_info({:nodedown, node}, state) do
    Logger.info("[LeaderElection] Node down: #{node}, attempting leadership")
    new_state = try_become_leader(state)
    {:noreply, new_state}
  end

  def handle_info({:global_name_conflict, @leader_name}, state) do
    Logger.warning("[LeaderElection] Lost leadership conflict")
    if state.task_timer, do: Process.cancel_timer(state.task_timer)
    {:noreply, %{state | is_leader: false, task_timer: nil}}
  end

  def handle_call(:am_i_leader, _from, state) do
    {:reply, state.is_leader, state}
  end

  defp try_become_leader(state) do
    case :global.register_name(@leader_name, self(), &resolve_conflict/3) do
      :yes ->
        Logger.info("[LeaderElection] Became leader on #{node()}")
        timer = Process.send_after(self(), :leader_task, 1_000)
        %{state | is_leader: true, task_timer: timer}
      :no ->
        state
    end
  end

  # Nodo con menor nombre léxico gana — determinístico
  defp resolve_conflict(_name, pid1, pid2) do
    if to_string(node(pid1)) <= to_string(node(pid2)), do: pid1, else: pid2
  end
end
```

---

### Exercise 3: Process Groups con :pg

**Problem**: Implementa un sistema de workers distribuidos usando `:pg`. Los workers se registran en grupos por tipo de trabajo. Un dispatcher distribuye trabajo aleatoriamente entre workers disponibles del grupo correcto, con fallback a workers locales si no hay remotos.

**Hints**:
- `:pg` debe iniciarse como parte del supervision tree con un scope name
- `pg.get_members/2` retorna PIDs de todos los nodos — filtra con `node(pid)` para locales vs remotos
- Para selección aleatoria de worker, usa `Enum.random/1`
- Un worker que muere se elimina automáticamente de `:pg` — no necesitas cleanup manual
- El scope de `:pg` debe iniciarse antes que los workers

**One possible solution**:

```elixir
defmodule WorkerRegistry do
  @scope :worker_pg

  def child_spec(_opts) do
    %{
      id: __MODULE__,
      start: {:pg, :start_link, [@scope]},
      type: :worker
    }
  end

  def register(worker_type, pid \\ self()) do
    :pg.join(@scope, worker_type, pid)
  end

  def get_worker(worker_type) do
    case :pg.get_members(@scope, worker_type) do
      [] -> {:error, :no_workers}
      pids -> {:ok, Enum.random(pids)}
    end
  end

  def get_local_worker(worker_type) do
    case :pg.get_local_members(@scope, worker_type) do
      [] -> get_worker(worker_type)  # fallback a remoto
      pids -> {:ok, Enum.random(pids)}
    end
  end

  def worker_count(worker_type) do
    length(:pg.get_members(@scope, worker_type))
  end
end

defmodule Worker do
  use GenServer

  def start_link(worker_type) do
    GenServer.start_link(__MODULE__, worker_type)
  end

  def execute(pid, job), do: GenServer.call(pid, {:execute, job})

  def init(worker_type) do
    WorkerRegistry.register(worker_type)
    {:ok, %{type: worker_type, jobs_done: 0}}
  end

  def handle_call({:execute, job}, _from, state) do
    result = process_job(job)
    {:reply, result, %{state | jobs_done: state.jobs_done + 1}}
  end

  defp process_job(job), do: {:completed, job, node()}
end

defmodule Dispatcher do
  def dispatch(worker_type, job) do
    with {:ok, worker} <- WorkerRegistry.get_local_worker(worker_type) do
      Worker.execute(worker, job)
    end
  end
end
```

---

## Common Mistakes

**`:global.register_name` bloqueante**: La llamada puede bloquearse si el cluster está en proceso de reconfigurarse. Siempre úsala con un timeout o desde dentro de un GenServer donde el timeout es manejado por el caller.

**No manejar `{:global_name_conflict, name}`**: Cuando un proceso pierde un conflicto de nombre, `:global` le envía este mensaje. Si el proceso no lo maneja, queda en su mailbox indefinidamente y puede causar confusión. Siempre agrega `handle_info` para este mensaje.

**Asumir que `:global.whereis_name` es consistente**: Entre el momento que llamas `whereis_name` y usas el PID, el proceso puede morir. Siempre maneja `{:EXIT, pid, reason}` o usa `try/catch` alrededor de GenServer.call con PID remoto.

**No iniciar el scope de `:pg`**: `:pg` requiere que el scope esté iniciado antes de usarlo. Si lo olvidas, obtendrás un error críptico. El scope puede ser un átomo o un módulo configurado en el supervision tree.

**Usar `:global` para alta frecuencia de lookups**: `:global.whereis_name` no es particularmente rápido — implica una búsqueda en tabla distribuida. Para hot paths, cachea el PID localmente y monitorea el proceso para invalidar el caché.

**Netsplit + `:global` = registros huérfanos**: Después de un netsplit y recovery, `:global` sincroniza pero puede haber registros de procesos muertos si el netsplit duró más que el process lifetime. Siempre verifica que el PID retornado por `whereis_name` sigue vivo.

---

## Verification

```elixir
# Verifica registro y lookup básico
:global.register_name(:test_process, self())
#=> :yes

pid = :global.whereis_name(:test_process)
assert pid == self()

# Verifica via tuple con GenServer
{:ok, _} = ConfigServer.start_link(%{env: :test})
assert ConfigServer.get(:env) == :test
ConfigServer.put(:version, "1.0")
assert ConfigServer.get(:version) == "1.0"

# Verifica process groups
{:ok, _} = Worker.start_link(:compute)
{:ok, _} = Worker.start_link(:compute)
assert WorkerRegistry.worker_count(:compute) == 2

{:ok, worker} = WorkerRegistry.get_worker(:compute)
assert is_pid(worker)

{:completed, :my_job, _node} = Dispatcher.dispatch(:compute, :my_job)
```

---

## Summary

`:global` provee registro de nombres a nivel de cluster con garantías de unicidad:

- Semántica CP: sacrifica disponibilidad para garantizar que solo hay un proceso con cada nombre
- Los callbacks de resolución de conflictos son esenciales para comportamiento determinístico tras netsplits
- `:pg` complementa a `:global`: grupos de procesos en lugar de singletons
- Leader election sobre `:global` es el patrón más simple para coordinación básica de cluster
- Para casos de uso más complejos o clusters grandes, considerar Horde (ejercicio 13)

---

## What's Next

- **Exercise 13**: Horde — distribución con CRDTs para mejor tolerancia a particiones que `:global`
- **Exercise 14**: Phoenix PubSub — mensajería distribuida construida sobre `:pg`
- **Exercise 15**: RPC patterns para invocar código en nodos remotos

---

## Resources

- [:global module documentation](https://www.erlang.org/doc/man/global.html)
- [:pg module documentation (OTP 23+)](https://www.erlang.org/doc/man/pg.html)
- [Erlang/OTP: distributed applications](https://www.erlang.org/doc/design_principles/distributed_applications.html)
- [Process Groups in Erlang — Erlang Solutions blog](https://www.erlang-solutions.com/blog/)
- [Conflict resolution in :global — Stack Overflow discussion](https://stackoverflow.com/questions/tagged/erlang+distributed)
