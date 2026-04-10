# 11. Distributed Erlang Clustering

**Difficulty**: Avanzado

---

## Prerequisites

- GenServer y OTP fundamentals (ejercicios 01-10)
- Conceptos de red: TCP/IP, hostname resolution
- IEx básico y Mix projects
- Comprensión de procesos y PIDs en Elixir

---

## Learning Objectives

- Conectar múltiples nodos Erlang/Elixir en un cluster
- Entender el modelo de distribución de Erlang: fully connected mesh
- Manejar cookies de seguridad para autenticación entre nodos
- Implementar monitoring de nodos y detección de netsplits
- Hacer llamadas remotas a procesos en otros nodos
- Diseñar sistemas resilientes ante particiones de red

---

## Concepts

### El modelo de distribución de Erlang

Erlang usa un modelo de **fully connected mesh**: cada nodo conoce y está conectado directamente a todos los demás nodos del cluster. No hay nodo central ni routing — los mensajes van directo de origen a destino.

Esto tiene implicaciones importantes:
- Con N nodos, hay N*(N-1)/2 conexiones TCP
- Un cluster de 100 nodos tiene ~5000 conexiones — razonable
- Un cluster de 10,000 nodos tendría 50 millones — no escala bien
- En producción, clusters de Erlang rara vez superan los 100-200 nodos

```
Nodo A ←——→ Nodo B
  ↕               ↕
Nodo C ←——→ Nodo D
```

Cada par está conectado directamente via TCP. Cuando A envía un mensaje a D, va directo, sin pasar por B o C.

### Nombres de nodo

Un nodo distribuido necesita un nombre único. Hay dos formatos:

```bash
# Short name — solo funciona en la misma red local
iex --sname mynode

# Long name — incluye hostname, recomendado para producción
iex --name mynode@192.168.1.10
iex --name mynode@hostname.example.com
```

En código, el nombre del nodo actual:

```elixir
node()
#=> :mynode@hostname

Node.self()
#=> :mynode@hostname  # equivalente
```

**Long names vs short names**: los nodos con long names solo pueden conectarse a otros long names, y viceversa. No mezclar.

### Cookies: el mecanismo de seguridad

La "cookie" es un shared secret que todos los nodos del cluster deben compartir. Es el único control de acceso en Erlang distribuido — no hay TLS por defecto, no hay autenticación adicional.

```elixir
# Ver la cookie actual
:erlang.get_cookie()
#=> :mycookie

# Establecer cookie en runtime
:erlang.set_cookie(node(), :mysecretcookie)

# Establecer cookie para un nodo específico
:erlang.set_cookie(:"other@host", :theirpassword)
```

Desde la línea de comandos:

```bash
iex --name nodeA@localhost --cookie mysecretcookie
```

En producción, la cookie debe venir de un secret manager, nunca hardcodeada. El archivo `~/.erlang.cookie` es otro mecanismo — Erlang lo lee automáticamente si no se especifica cookie.

**Seguridad real en producción**: usar WireGuard o VPN para la red del cluster, y TLS para distribución Erlang (`:inet_tls_dist`). La cookie sola no es suficiente.

### Conectar nodos

```elixir
# Conectar a otro nodo — retorna true si exitoso
Node.connect(:"nodeB@localhost")
#=> true

# Listar nodos conectados
Node.list()
#=> [:"nodeB@localhost", :"nodeC@localhost"]

# Verificar si un nodo está disponible
:net_adm.ping(:"nodeB@localhost")
#=> :pong   # disponible
#=> :pang   # no disponible (la p viene de "ping/pang" en sueco)
```

`Node.connect/1` establece la conexión TCP y autentica con la cookie. Si las cookies no coinciden, retorna `false` y logea un error.

### Topología del cluster y :net_kernel

`:net_kernel` es el proceso OTP que gestiona la distribución. En condiciones normales no interactúas directamente con él, pero es útil para monitoring:

```elixir
# Monitorear eventos de nodos (conexiones y desconexiones)
:net_kernel.monitor_nodes(true)

# Ahora recibirás mensajes:
# {:nodeup, :"nodeB@localhost"}
# {:nodedown, :"nodeB@localhost"}

# Con opciones adicionales
:net_kernel.monitor_nodes(true, [node_type: :all])
```

Para desactivar:

```elixir
:net_kernel.monitor_nodes(false)
```

### Enviar mensajes a procesos remotos

Si tienes el PID de un proceso remoto, puedes enviarle mensajes directamente:

```elixir
# Un PID remoto se ve así:
remote_pid = #PID<1.0.1>  # el primer número es el nodo

# Enviar mensaje — funciona igual que local
send(remote_pid, {:hello, "from nodeA"})

# GenServer.call también funciona con PIDs remotos
GenServer.call(remote_pid, :get_state)
```

También puedes registrar procesos por nombre y hacer llamadas remotas:

```elixir
# En nodeB, un GenServer registrado como :my_server
GenServer.call({:my_server, :"nodeB@localhost"}, :get_state)
```

### Netsplits: la realidad de los sistemas distribuidos

Un **netsplit** (partición de red) ocurre cuando los nodos del cluster se separan en grupos que no pueden comunicarse entre sí. Cada grupo cree que los otros nodos están caídos.

```
ANTES:    A ←→ B ←→ C
NETSPLIT: A | B ←→ C

A ve: [B, C] desconectados
B ve: [A desconectado], [C conectado]
C ve: [A desconectado], [B conectado]
```

Las implicaciones son severas:
- Si B y C son primarios de un dato, A puede tener datos stale
- Si A tenía el "líder", B y C pueden elegir otro — split brain
- Cuando la red se recupera, hay conflictos que resolver

**No hay solución perfecta** — el teorema CAP dice que debes elegir:
- CP: consistency + partition tolerance (rechazar escrituras durante netsplit)
- AP: availability + partition tolerance (aceptar escrituras con riesgo de conflicto)

En producción, la mayoría de sistemas críticos usan CP: si el nodo no puede contactar quorum, rechaza operaciones.

### Handling netsplits con monitor_nodes

```elixir
defmodule ClusterMonitor do
  use GenServer
  require Logger

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    :net_kernel.monitor_nodes(true, [node_type: :all])
    {:ok, %{nodes: Node.list()}}
  end

  def handle_info({:nodeup, node, _info}, state) do
    Logger.info("Node joined cluster: #{node}")
    {:noreply, %{state | nodes: [node | state.nodes]}}
  end

  def handle_info({:nodedown, node, _info}, state) do
    Logger.warning("Node left cluster (possible netsplit): #{node}")
    # Aquí irían las acciones de recovery
    {:noreply, %{state | nodes: List.delete(state.nodes, node)}}
  end
end
```

---

## Exercises

### Exercise 1: Conectar dos nodos IEx

**Problem**: Inicia dos nodos IEx en terminales separadas y conéctalos. Verifica la conexión bidireccional, envía mensajes entre ellos, y examina cómo se ven los PIDs remotos.

**Hints**:
- Usa `--name` con long names para ambos nodos
- La cookie debe ser idéntica en ambos
- `Node.connect/1` en uno solo es suficiente — la conexión es bidireccional
- Examina el PID resultante de `spawn` remoto — el primer número identifica el nodo

**One possible solution**:

```elixir
# Terminal 1:
# iex --name nodeA@127.0.0.1 --cookie secretcookie

# Terminal 2:
# iex --name nodeB@127.0.0.1 --cookie secretcookie

# En nodeA:
Node.connect(:"nodeB@127.0.0.1")
#=> true

Node.list()
#=> [:"nodeB@127.0.0.1"]

# Spawn un proceso en nodeB desde nodeA:
pid = Node.spawn(:"nodeB@127.0.0.1", fn ->
  receive do
    {:hello, from} -> IO.puts("Hello from #{inspect(from)}")
  end
end)

# El PID tiene el nodo codificado: #PID<nodo.proceso.serial>
send(pid, {:hello, node()})

# Llamada a función remota:
Node.spawn(:"nodeB@127.0.0.1", IO, :inspect, [node()])
```

---

### Exercise 2: Remote GenServer Calls

**Problem**: Implementa un `CounterServer` que se registra localmente en su nodo. Desde otro nodo, inicia el servidor, haz calls y casts remotos, y maneja el caso donde el nodo remoto no está disponible.

**Hints**:
- Registra el GenServer con `name: __MODULE__` para usar `{Module, node}` desde remoto
- `GenServer.call({Module, remote_node}, msg)` funciona si el módulo está cargado en ambos nodos
- El timeout por defecto de GenServer.call es 5000ms — ajústalo para llamadas remotas lentas
- Prueba desconectar el nodo B mientras hay calls en vuelo — observa el error

**One possible solution**:

```elixir
defmodule CounterServer do
  use GenServer

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, 0, name: __MODULE__)
  end

  def increment(node \\ node()) do
    GenServer.call({__MODULE__, node}, :increment)
  end

  def value(node \\ node()) do
    GenServer.call({__MODULE__, node}, :value)
  end

  def init(initial), do: {:ok, initial}

  def handle_call(:increment, _from, count), do: {:reply, count + 1, count + 1}
  def handle_call(:value, _from, count), do: {:reply, count, count}
end

# En nodeA: iniciar el servidor
{:ok, _} = CounterServer.start_link()

# En nodeB: llamar al servidor en nodeA
remote_node = :"nodeA@127.0.0.1"

# Verificar que el nodo está disponible antes de llamar
case :net_adm.ping(remote_node) do
  :pong ->
    CounterServer.increment(remote_node)
  :pang ->
    {:error, :node_not_available}
end
```

---

### Exercise 3: Netsplit Detection y Recovery

**Problem**: Implementa un `ClusterManager` GenServer que monitorea el estado del cluster, detecta netsplits, y ejecuta una estrategia de recovery configurable. Debe mantener un log de eventos del cluster y permitir consultar el historial.

**Hints**:
- Usa `:net_kernel.monitor_nodes(true, [node_type: :all])` para recibir eventos con metadata
- Los mensajes son `{:nodeup, node, info}` y `{:nodedown, node, info}` con la opción `node_type: :all`
- Implementa una estrategia de "quorum": si pierdes más de la mitad de los nodos, activa modo degradado
- Considera usar un timer para intentar reconexión periódica a nodos conocidos
- El historial puede ser una lista limitada (últimos 100 eventos) para evitar memoria ilimitada

**One possible solution**:

```elixir
defmodule ClusterManager do
  use GenServer
  require Logger

  @reconnect_interval 5_000
  @max_history 100

  defstruct [:known_nodes, :connected_nodes, :history, :degraded_mode]

  def start_link(known_nodes) do
    GenServer.start_link(__MODULE__, known_nodes, name: __MODULE__)
  end

  def cluster_status, do: GenServer.call(__MODULE__, :status)
  def event_history, do: GenServer.call(__MODULE__, :history)

  def init(known_nodes) do
    :net_kernel.monitor_nodes(true, [node_type: :all])
    # Intentar conectar a todos los nodos conocidos al inicio
    Enum.each(known_nodes, &Node.connect/1)

    schedule_reconnect()

    state = %__MODULE__{
      known_nodes: known_nodes,
      connected_nodes: Node.list(),
      history: [],
      degraded_mode: false
    }
    {:ok, state}
  end

  def handle_info({:nodeup, node, _info}, state) do
    event = {DateTime.utc_now(), :nodeup, node}
    Logger.info("[ClusterManager] Node up: #{node}")
    new_connected = Enum.uniq([node | state.connected_nodes])
    new_state = %{state |
      connected_nodes: new_connected,
      history: Enum.take([event | state.history], @max_history),
      degraded_mode: false
    }
    {:noreply, new_state}
  end

  def handle_info({:nodedown, node, _info}, state) do
    event = {DateTime.utc_now(), :nodedown, node}
    Logger.warning("[ClusterManager] Node down: #{node} — possible netsplit")
    new_connected = List.delete(state.connected_nodes, node)

    # Quorum: degraded si perdemos más de la mitad de nodos conocidos
    quorum = length(state.known_nodes) / 2
    degraded = length(new_connected) < quorum

    if degraded, do: Logger.error("[ClusterManager] Below quorum — entering degraded mode")

    new_state = %{state |
      connected_nodes: new_connected,
      history: Enum.take([event | state.history], @max_history),
      degraded_mode: degraded
    }
    {:noreply, new_state}
  end

  def handle_info(:reconnect, state) do
    disconnected = state.known_nodes -- state.connected_nodes -- [node()]
    Enum.each(disconnected, fn n ->
      Logger.info("[ClusterManager] Attempting reconnect to #{n}")
      Node.connect(n)
    end)
    schedule_reconnect()
    {:noreply, state}
  end

  def handle_call(:status, _from, state) do
    status = %{
      current_node: node(),
      connected: state.connected_nodes,
      degraded_mode: state.degraded_mode,
      quorum_size: length(state.known_nodes)
    }
    {:reply, status, state}
  end

  def handle_call(:history, _from, state) do
    {:reply, state.history, state}
  end

  defp schedule_reconnect do
    Process.send_after(self(), :reconnect, @reconnect_interval)
  end
end
```

---

## Common Mistakes

**Cookie mismatch silencioso**: `Node.connect/1` retorna `false` sin error claro cuando las cookies no coinciden. Siempre verifica con `:net_adm.ping/1` y revisa los logs de Erlang (`--logger-level debug`).

**Mezclar short y long names**: Un nodo con `--sname` no puede conectarse a uno con `--name`. El error es confuso porque `Node.connect` simplemente retorna `false`.

**Asumir que los módulos están disponibles en el nodo remoto**: Si haces `Node.spawn(remote, MyModule, :func, [])`, el módulo `MyModule` debe estar compilado y disponible en el nodo remoto. En desarrollo con iex, puedes copiar el beam manualmente. En producción, todos los nodos deben tener el mismo release.

**No manejar `:nodedown` en procesos que dependen de nodos remotos**: Si tienes un GenServer con un PID remoto cacheado, ese PID deja de ser válido cuando el nodo se cae. Siempre monitorea los nodos de los que dependes.

**Split brain silencioso**: El mayor peligro en sistemas distribuidos. Sin quorum ni consensus, dos particiones pueden aceptar escrituras contradictorias. Diseña explícitamente qué pasa durante un netsplit — la respuesta "no me importa" casi siempre está mal.

**localhost vs 127.0.0.1**: Con long names, usa la misma forma en todos los nodos. `nodeA@localhost` y `nodeA@127.0.0.1` son nodos diferentes.

---

## Verification

Para verificar tu implementación del Exercise 3:

```elixir
# Inicia el cluster con 3 nodos
known = [:"nodeA@127.0.0.1", :"nodeB@127.0.0.1", :"nodeC@127.0.0.1"]
{:ok, _} = ClusterManager.start_link(known)

# Verifica estado inicial
%{connected: connected, degraded_mode: false} = ClusterManager.cluster_status()
assert length(connected) == 2  # los otros 2 nodos

# Simula netsplit desconectando un nodo
Node.disconnect(:"nodeB@127.0.0.1")

# Verifica que el manager detectó el evento
:timer.sleep(100)
history = ClusterManager.event_history()
assert {:nodedown, :"nodeB@127.0.0.1"} in Enum.map(history, fn {_, type, node} -> {type, node} end)

# Con 2 de 3 nodos aún conectados (incluyendo el actual), no debe ser degraded
%{degraded_mode: false} = ClusterManager.cluster_status()
```

---

## Summary

Erlang's distribution model es poderoso pero requiere diseño explícito para la resiliencia:

- El modelo fully connected mesh es simple pero no escala más allá de ~200 nodos
- Las cookies proveen autenticación básica — complementar con TLS o VPN en producción
- Los netsplits son inevitables en cualquier sistema distribuido real
- La decisión CP vs AP debe tomarse conscientemente, no por omisión
- `:net_kernel.monitor_nodes/2` es la primitiva fundamental para detectar cambios en el cluster
- El quorum es la herramienta más simple para evitar split brain

---

## What's Next

- **Exercise 12**: Registro global de procesos con `:global` — construcción sobre la capa de distribución
- **Exercise 13**: Horde para distribución con tolerancia a particiones via CRDTs
- **Exercise 14**: Phoenix PubSub distribuido para mensajería en el cluster
- **Exercise 15**: RPC patterns para computación remota

---

## Resources

- [Erlang Distribution Protocol](https://www.erlang.org/doc/apps/erts/erl_dist_protocol.html)
- [net_kernel documentation](https://www.erlang.org/doc/man/net_kernel.html)
- [Distributed Erlang — Erlang docs](https://www.erlang.org/doc/reference_manual/distributed.html)
- [The perils of the perplexed programmer — Fred Hebert on netsplits](https://ferd.ca/the-zen-of-erlang.html)
- [Partisan — alternative distribution layer for large clusters](https://github.com/lasp-lang/partisan)
