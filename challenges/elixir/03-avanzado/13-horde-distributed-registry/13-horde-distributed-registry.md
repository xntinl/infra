# 13. Horde: Distributed Registry

**Difficulty**: Avanzado

---

## Prerequisites

- Ejercicio 11: Distributed Erlang Clustering
- Ejercicio 12: Global Process Registry (`:global` y `:pg`)
- GenServer y supervisores avanzados (ejercicios 01-10)
- Conceptos básicos de CRDTs (Conflict-free Replicated Data Types)

---

## Learning Objectives

- Usar `Horde.Registry` para registro distribuido con eventual consistency
- Implementar supervisión distribuida con `Horde.DynamicSupervisor`
- Entender delta CRDTs y por qué permiten mejor tolerancia a particiones
- Configurar cluster membership dinámica en Horde
- Comparar trade-offs entre Horde, `:global`, y `Registry` local
- Implementar handoff de procesos entre nodos

---

## Concepts

### ¿Por qué Horde?

`:global` usa locks distribuidos — si la red es lenta o hay partición, las operaciones se bloquean o fallan. `Registry` local es rápido pero solo visible en un nodo. Horde ofrece un punto medio:

| Aspecto | `:global` | `Registry` | `Horde.Registry` |
|---------|-----------|------------|------------------|
| Scope | Cluster | Local | Cluster |
| Consistency | Strong (CP) | N/A | Eventual (AP) |
| Netsplit behavior | Bloquea | N/A | Diverge + reconcilia |
| Lookup speed | Lento | O(1) | O(1) local |
| Escala | ~20 nodos | Ilimitado | ~50-100 nodos |
| Overhead | Alto | Mínimo | Medio |

Horde es AP: disponible durante netsplits, reconcilia cuando la red se recupera. Esto significa que temporalmente puede haber dos procesos con el mismo nombre en particiones distintas — cuando la red se recupera, Horde los reconcilia matando el "perdedor" según una función de resolución.

### Delta CRDTs: la clave técnica

Un **CRDT** (Conflict-free Replicated Data Type) es una estructura de datos que puede ser modificada de forma concurrente en múltiples réplicas y siempre converge al mismo estado, sin coordinación.

Un **delta CRDT** envía solo el "delta" (cambio incremental) en lugar del estado completo:

```
Nodo A: {alice: pid1}
Nodo B: {bob: pid2}

Sin delta CRDTs: A envía {alice: pid1} a B, B envía {bob: pid2} a A
Con delta CRDTs: A envía "agregué alice:pid1" a B, B envía "agregué bob:pid2" a A

Resultado en ambos: {alice: pid1, bob: pid2}
```

Horde usa delta CRDTs implementados en la librería `DeltaCrdt`. El registro se propaga eventualmente a todos los nodos sin necesidad de coordinación central ni locks.

### Configuración básica de Horde

```elixir
# mix.exs
defp deps do
  [
    {:horde, "~> 0.9"}
  ]
end
```

```elixir
# application.ex
def start(_type, _args) do
  children = [
    # Registry distribuido
    {Horde.Registry, [
      name: MyApp.Registry,
      keys: :unique,
      members: :auto  # detección automática via libcluster o similar
    ]},

    # Supervisor dinámico distribuido
    {Horde.DynamicSupervisor, [
      name: MyApp.DistSupervisor,
      strategy: :one_for_one,
      members: :auto
    ]}
  ]

  Supervisor.start_link(children, strategy: :one_for_one)
end
```

### Horde.Registry

```elixir
# Registrar con via tuple (compatible con GenServer)
def start_link(name) do
  GenServer.start_link(__MODULE__, name,
    name: {:via, Horde.Registry, {MyApp.Registry, name}}
  )
end

# Lookup
case Horde.Registry.lookup(MyApp.Registry, "user:alice") do
  [{pid, _meta}] -> {:ok, pid}
  [] -> {:error, :not_found}
end

# Registrar manualmente
Horde.Registry.register(MyApp.Registry, "session:xyz", %{started_at: DateTime.utc_now()})

# Listar todos los registros (útil para debug)
Horde.Registry.select(MyApp.Registry, [{{:"$1", :"$2", :"$3"}, [], [{{:"$1", :"$2", :"$3"}}]}])
```

### Horde.DynamicSupervisor

Funciona como `DynamicSupervisor` pero distribuye procesos por el cluster:

```elixir
# Iniciar un child — puede arrancar en cualquier nodo del cluster
Horde.DynamicSupervisor.start_child(
  MyApp.DistSupervisor,
  {MyWorker, [id: "worker-1", name: "worker-1"]}
)

# Horde decide en qué nodo arrancar basándose en la distribución actual
# Si el nodo donde estaba el proceso se cae, Horde lo reinicia en otro nodo
```

**Estrategia de distribución**: Horde usa consistent hashing por defecto para distribuir procesos entre nodos. Cuando un nodo se une o se va, solo una fracción de los procesos migra.

### Cluster membership: el paso crítico

Horde necesita saber qué nodos forman el cluster. Hay dos formas:

**1. Manual** (para pruebas y desarrollo):

```elixir
# Agregar un nodo al registry de Horde
Horde.Cluster.set_members(MyApp.Registry, [
  {MyApp.Registry, :"nodeA@localhost"},
  {MyApp.Registry, :"nodeB@localhost"}
])

# Lo mismo para el supervisor
Horde.Cluster.set_members(MyApp.DistSupervisor, [
  {MyApp.DistSupervisor, :"nodeA@localhost"},
  {MyApp.DistSupervisor, :"nodeB@localhost"}
])
```

**2. Automático con libcluster** (para producción):

```elixir
# En application.ex, antes de Horde:
{Cluster.Supervisor, [
  [gossip: [strategy: Cluster.Strategy.Gossip]],
  [name: MyApp.ClusterSupervisor]
]},

# Y un proceso que escucha eventos de libcluster y actualiza Horde:
defmodule MyApp.HordeMembership do
  use GenServer

  def init(_) do
    :net_kernel.monitor_nodes(true)
    sync_horde_members()
    {:ok, []}
  end

  def handle_info({:nodeup, _node}, state) do
    sync_horde_members()
    {:noreply, state}
  end

  def handle_info({:nodedown, _node}, state) do
    sync_horde_members()
    {:noreply, state}
  end

  defp sync_horde_members do
    members = [node() | Node.list()]
    horde_members = Enum.map(members, &{MyApp.Registry, &1})
    Horde.Cluster.set_members(MyApp.Registry, horde_members)
    # Igual para DynamicSupervisor...
  end
end
```

### Handoff de procesos

Cuando un nodo abandona el cluster gracefully, Horde puede "pasar" los procesos a otros nodos. Para implementar handoff, el worker debe implementar el callback `Horde.DynamicSupervisor` handoff protocol:

```elixir
defmodule MyWorker do
  use GenServer

  # Horde llama esto antes de terminar el proceso por handoff
  def handle_call({:handoff, handoff_state}, _from, state) do
    # Retornar el estado que debe transferirse al nuevo proceso
    {:reply, {:handoff, state}, state}
  end

  # Horde llama init/1 con {:handoff, state} cuando es un proceso de handoff
  def init({:handoff, previous_state}) do
    # Restaurar desde el estado del proceso anterior
    {:ok, previous_state}
  end

  def init(initial_state) do
    {:ok, initial_state}
  end
end
```

---

## Exercises

### Exercise 1: Registry con Failover Automático

**Problem**: Implementa un sistema de sesiones de usuario donde cada sesión es un proceso registrado con el user ID como nombre. Si el nodo donde corre la sesión se cae, Horde debe reiniciar la sesión en otro nodo automáticamente. La sesión debe recordar su estado (historial de acciones) tras el restart.

**Hints**:
- La sesión necesita persistir su estado en algún store externo (ETS compartido, o simplemente simula con un Agent local al nodo del Registry)
- Usa `{:via, Horde.Registry, {MyApp.Registry, user_id}}` para el nombre
- Inicia la sesión con `Horde.DynamicSupervisor.start_child/2`
- El failover es automático — cuando el nodo muere, Horde reinicia el child en otro nodo
- Para probar: mata el nodo donde corre la sesión y verifica que aparece en otro nodo

**One possible solution**:

```elixir
defmodule SessionServer do
  use GenServer

  def start_link({user_id, initial_state}) do
    GenServer.start_link(
      __MODULE__,
      {user_id, initial_state},
      name: via(user_id)
    )
  end

  def child_spec({user_id, initial_state}) do
    %{
      id: {__MODULE__, user_id},
      start: {__MODULE__, :start_link, [{user_id, initial_state}]},
      restart: :transient
    }
  end

  defp via(user_id) do
    {:via, Horde.Registry, {MyApp.Registry, "session:#{user_id}"}}
  end

  def add_action(user_id, action) do
    GenServer.call(via(user_id), {:add_action, action})
  end

  def get_history(user_id) do
    GenServer.call(via(user_id), :get_history)
  end

  def start_session(user_id) do
    Horde.DynamicSupervisor.start_child(
      MyApp.DistSupervisor,
      {__MODULE__, {user_id, []}}
    )
  end

  def init({user_id, initial_history}) do
    {:ok, %{user_id: user_id, history: initial_history, node: node()}}
  end

  def handle_call({:add_action, action}, _from, state) do
    new_history = [action | state.history]
    {:reply, :ok, %{state | history: new_history}}
  end

  def handle_call(:get_history, _from, state) do
    {:reply, {state.history, state.node}, state}
  end
end
```

---

### Exercise 2: Distributed Supervisor con Handoff

**Problem**: Implementa workers de larga duración que acumulan estado (ej: contadores de métricas por servicio). Cuando añades un nuevo nodo al cluster, Horde rebalancea y migra algunos workers al nuevo nodo. El worker debe transferir su estado acumulado (no perder métricas) durante la migración.

**Hints**:
- Implementa `handle_call({:handoff, handoff_state}, ...)` en el worker
- El worker debe poder inicializarse tanto fresh como desde handoff state: `init({:handoff, state})` vs `init(initial_args)`
- Para observar el handoff, usa `:sys.get_state/1` antes y después de mover el nodo
- Horde no implementa handoff automático en versiones recientes — puede que necesites implementarlo manualmente via `terminate/2`
- Documenta el trade-off: qué pasa si el nodo muere antes de completar el handoff

**One possible solution**:

```elixir
defmodule MetricsWorker do
  use GenServer

  def start_link({service_name, initial_state}) do
    GenServer.start_link(
      __MODULE__,
      {service_name, initial_state},
      name: via(service_name)
    )
  end

  def child_spec({service_name, initial_state}) do
    %{
      id: {__MODULE__, service_name},
      start: {__MODULE__, :start_link, [{service_name, initial_state}]},
      restart: :transient
    }
  end

  defp via(name) do
    {:via, Horde.Registry, {MyApp.Registry, "metrics:#{name}"}}
  end

  def record(service_name, metric, value) do
    GenServer.cast(via(service_name), {:record, metric, value})
  end

  def report(service_name) do
    GenServer.call(via(service_name), :report)
  end

  # Init desde estado fresco
  def init({service_name, []}) do
    {:ok, %{service: service_name, metrics: %{}, running_on: node()}}
  end

  # Init desde handoff — restaurar estado previo
  def init({_service_name, handoff_state}) when is_map(handoff_state) do
    {:ok, Map.put(handoff_state, :running_on, node())}
  end

  def handle_cast({:record, metric, value}, state) do
    new_metrics = Map.update(state.metrics, metric, [value], &[value | &1])
    {:noreply, %{state | metrics: new_metrics}}
  end

  def handle_call(:report, _from, state) do
    summary = Map.new(state.metrics, fn {k, vs} ->
      {k, %{count: length(vs), sum: Enum.sum(vs), avg: Enum.sum(vs) / length(vs)}}
    end)
    {:reply, {summary, state.running_on}, state}
  end

  # Handoff: exportar estado antes de terminar
  def terminate(:handoff, state) do
    # Horde iniciará un nuevo proceso en otro nodo con este estado
    state
  end

  def terminate(_reason, _state), do: :ok
end
```

---

### Exercise 3: Horde vs :global vs Registry — Comparación

**Problem**: Implementa el mismo contador distribuido (registrado por nombre, único en el cluster) usando las tres alternativas: `Horde.Registry`, `:global`, y `Registry` local con un proceso de routing. Mide y documenta: latencia de lookup, comportamiento durante netsplit simulado, y complejidad de implementación.

**Hints**:
- Para simular netsplit con múltiples terminales IEx: `Node.disconnect/1` para separar nodos y reconectarlos después
- Para medir latencia: `:timer.tc/1` o `Benchee`
- El Router con Registry local necesita replicar los registros via `:global` o `:pg` — implementa el mínimo viable
- El objetivo no es "cuál es mejor" sino entender cuándo usar cada uno
- Documenta el comportamiento en estas condiciones: nodo normal, netsplit activo, recovery post-netsplit

**One possible solution**:

```elixir
# Implementación 1: Con :global
defmodule Counter.Global do
  use GenServer

  def start_link(name) do
    case GenServer.start_link(__MODULE__, name,
           name: {:global, {__MODULE__, name}}) do
      {:ok, pid} -> {:ok, pid}
      {:error, {:already_started, pid}} -> {:ok, pid}
    end
  end

  def increment(name), do: GenServer.call({:global, {__MODULE__, name}}, :incr)
  def value(name), do: GenServer.call({:global, {__MODULE__, name}}, :value)

  def init(_name), do: {:ok, 0}
  def handle_call(:incr, _from, n), do: {:reply, n + 1, n + 1}
  def handle_call(:value, _from, n), do: {:reply, n, n}
end

# Implementación 2: Con Horde.Registry
defmodule Counter.Horde do
  use GenServer

  defp via(name), do: {:via, Horde.Registry, {MyApp.Registry, {__MODULE__, name}}}

  def start_link(name) do
    Horde.DynamicSupervisor.start_child(
      MyApp.DistSupervisor,
      %{id: {__MODULE__, name}, start: {__MODULE__, :do_start, [name]}}
    )
  end

  def do_start(name) do
    GenServer.start_link(__MODULE__, name, name: via(name))
  end

  def increment(name), do: GenServer.call(via(name), :incr)
  def value(name), do: GenServer.call(via(name), :value)

  def init(_name), do: {:ok, 0}
  def handle_call(:incr, _from, n), do: {:reply, n + 1, n + 1}
  def handle_call(:value, _from, n), do: {:reply, n, n}
end

# Benchmark comparativo
defmodule Counter.Benchmark do
  def run(name, iterations \\ 1_000) do
    {global_time, _} = :timer.tc(fn ->
      Enum.each(1..iterations, fn _ -> Counter.Global.value(name) end)
    end)

    {horde_time, _} = :timer.tc(fn ->
      Enum.each(1..iterations, fn _ -> Counter.Horde.value(name) end)
    end)

    %{
      global_us_per_op: global_time / iterations,
      horde_us_per_op: horde_time / iterations,
      ratio: horde_time / global_time
    }
  end
end
```

**Resultados típicos esperados** (para documentar en tu análisis):

| Operación | `:global` | `Horde.Registry` |
|-----------|-----------|------------------|
| Lookup local | ~50-200μs | ~1-5μs |
| Lookup remoto | ~100-500μs | ~1-5μs |
| Register | ~1-5ms | ~1-10ms |
| Netsplit behavior | Bloquea | AP, diverge |
| Recovery | Automático | Automático |

---

## Common Mistakes

**No actualizar cluster membership en Horde**: Horde no detecta nodos automáticamente (a menos que uses `members: :auto` con libcluster). Si añades un nodo y no llamas `Horde.Cluster.set_members/2`, ese nodo no participa en la distribución.

**Confundir eventual consistency con inconsistencia permanente**: Horde converge — después de un netsplit, los estados se reconcilian. El "problema" es la ventana de inconsistencia durante el netsplit. Para muchos casos de uso, esta ventana es aceptable.

**No manejar el proceso "perdedor" post-netsplit**: Cuando Horde reconcilia dos registros del mismo nombre, mata uno. El proceso que muere recibirá una señal de terminación normal. Si tiene subscribers o estado externo, necesita cleanup. Implementa `terminate/2` defensivamente.

**Usar Horde para workloads de alta frecuencia**: Horde tiene overhead por la sincronización CRDT. No es un reemplazo de ETS para lookups de millones de ops/segundo. Para hot paths, cachea el PID localmente.

**members: :auto sin libcluster**: `members: :auto` en Horde requiere que los nodos estén conectados via libcluster o similar. Si los nodos se conectan manualmente con `Node.connect/1`, Horde no sabrá de ellos automáticamente — usa `Horde.Cluster.set_members/2` manualmente o implementa el listener de `monitor_nodes`.

**Handoff incompleto**: El handoff de Horde no es atómico. Si el nodo muere durante el handoff, el estado puede perderse. Para estado crítico, usa un store externo (Postgres, Redis, ETS compartido) además del estado en el proceso.

---

## Verification

```elixir
# Verifica que el registro funciona en el nodo local
{:ok, _pid} = SessionServer.start_session("user-123")
:ok = SessionServer.add_action("user-123", :login)
{[{:login}], _node} = SessionServer.get_history("user-123")

# Verifica que Horde.Registry puede hacer lookup
[{pid, _}] = Horde.Registry.lookup(MyApp.Registry, "session:user-123")
assert is_pid(pid)
assert Process.alive?(pid)

# Verifica distribución entre nodos (requiere cluster activo)
nodes = [node() | Node.list()]
sessions = Enum.map(["u1", "u2", "u3", "u4"], fn uid ->
  {:ok, pid} = SessionServer.start_session(uid)
  {uid, node(pid)}
end)

# Con múltiples nodos, los procesos deben distribuirse
running_nodes = sessions |> Enum.map(&elem(&1, 1)) |> Enum.uniq()
assert length(running_nodes) > 1  # distribuidos en más de un nodo
```

---

## Summary

Horde llena el espacio entre `Registry` local y `:global` distribuido:

- Usa delta CRDTs para replicación eventual sin coordinación central
- AP por diseño: disponible durante netsplits, reconcilia después
- `Horde.Registry` para nombres únicos distribuidos con lookup O(1) local
- `Horde.DynamicSupervisor` para procesos distribuidos con restart automático en otros nodos
- La configuración de membership es el punto más crítico — automatizar con libcluster en producción
- Los trade-offs son reales: eventual consistency implica ventanas de inconsistencia

---

## What's Next

- **Exercise 14**: Phoenix PubSub — mensajería distribuida que complementa Horde
- **Exercise 15**: RPC — invocación de código remoto, diferente al registro de procesos
- **Exercise 43**: Build a Cache Server — aplicación práctica de los conceptos de distribución

---

## Resources

- [Horde documentation](https://hexdocs.pm/horde/Horde.html)
- [Horde GitHub — derekkraan](https://github.com/derekkraan/horde)
- [DeltaCrdt — la librería CRDT bajo Horde](https://hexdocs.pm/delta_crdt/DeltaCrdt.html)
- [Understanding CRDTs — Martin Kleppmann](https://martin.kleppmann.com/2020/07/06/crdt-hard-parts-hydra.html)
- [libcluster — cluster formation automática](https://hexdocs.pm/libcluster/readme.html)
- [Horde: a distributed supervisor and registry — Derek Kraan's talk](https://www.youtube.com/watch?v=EZFLPG7V7RM)
