# 14. Phoenix PubSub Advanced

**Difficulty**: Avanzado

---

## Prerequisites

- Ejercicio 11: Distributed Erlang Clustering
- Ejercicio 12: Global Process Registry (`:pg`)
- Phoenix Framework básico (o disposición a leer la API)
- Comprensión del modelo de mensajería pub/sub

---

## Learning Objectives

- Usar `Phoenix.PubSub` para mensajería distribuida dentro de un cluster
- Distinguir `broadcast`, `local_broadcast`, y `direct_broadcast`
- Implementar `Phoenix.Presence` para tracking de usuarios en tiempo real
- Construir un sistema de chat multi-sala distribuido
- Entender los adaptadores de PubSub y cuándo escribir uno propio
- Manejar backpressure y suscripciones en sistemas de alta carga

---

## Concepts

### Phoenix.PubSub: la capa de mensajería distribuida

`Phoenix.PubSub` es el sistema de mensajería que sustenta Phoenix Channels, LiveView, y Presence. No requiere Phoenix en sí — es una librería independiente que cualquier aplicación Elixir puede usar.

El modelo es clásico **pub/sub**:
- Los procesos se **suscriben** a topics (strings arbitrarios)
- Cualquier proceso puede **publicar** en un topic
- Todos los suscriptores del topic (en cualquier nodo del cluster) reciben el mensaje

```elixir
# Suscribir el proceso actual al topic "room:lobby"
Phoenix.PubSub.subscribe(MyApp.PubSub, "room:lobby")

# Publicar a todos los suscriptores del topic en el cluster
Phoenix.PubSub.broadcast(MyApp.PubSub, "room:lobby", {:new_message, "hello"})

# El proceso suscrito recibe:
receive do
  {:new_message, text} -> IO.puts("Received: #{text}")
end
```

### Adaptadores: PG2 vs noop vs custom

El comportamiento distribuido de PubSub depende del adaptador:

```elixir
# En application.ex
children = [
  {Phoenix.PubSub, name: MyApp.PubSub, adapter: Phoenix.PubSub.PG2}
]
```

**`Phoenix.PubSub.PG2`** (default): usa `:pg` (anteriormente `:pg2`) bajo el capó. Los topics son grupos de proceso. Cuando publicas, el mensaje se envía a todos los PIDs en el grupo, en todos los nodos. Es el adaptador de producción estándar.

**Adaptador customizado**: puedes implementar el comportamiento `Phoenix.PubSub.Adapter` para usar Redis, Kafka, o cualquier otro backend. Útil cuando necesitas:
- Mensajes persistentes (los procesos que se unen después del broadcast lo reciben)
- Auditoría de todos los mensajes
- Bridging entre clusters Erlang separados

### broadcast vs local_broadcast vs direct_broadcast

```elixir
# broadcast: envía a TODOS los nodos del cluster
Phoenix.PubSub.broadcast(MyApp.PubSub, "room:1", msg)

# local_broadcast: solo nodo actual, sin saltar a otros nodos
Phoenix.PubSub.local_broadcast(MyApp.PubSub, "room:1", msg)

# direct_broadcast: envía solo al nodo especificado
Phoenix.PubSub.direct_broadcast(target_node, MyApp.PubSub, "room:1", msg)
```

**Cuándo usar cada uno**:

`broadcast` es el caso general — los usuarios conectados a cualquier nodo deben recibir el mensaje. Úsalo para la gran mayoría de casos.

`local_broadcast` es útil cuando sabes que el efecto es local: por ejemplo, invalidar un caché en memoria (cada nodo tiene su propio caché), o notificar a procesos locales de un evento del sistema. Evita el overhead de saltar a otros nodos.

`direct_broadcast` es raro — cuando necesitas enviar a un nodo específico deliberadamente. Útil en arquitecturas donde ciertos nodos son "coordinadores" para un subconjunto de topics.

### Suscripción y cleanup automático

Phoenix.PubSub monitorea el proceso suscriptor. Cuando el proceso muere (por cualquier razón), se desuscribe automáticamente. Esto evita memory leaks en sistemas con muchos procesos de corta vida.

```elixir
# No necesitas unsuscribe en terminate — es automático
# Pero si quieres desuscribir explícitamente antes de terminar:
Phoenix.PubSub.unsubscribe(MyApp.PubSub, "room:lobby")
```

Si quieres saber cuántos suscriptores tiene un topic (para debug o métricas):

```elixir
Phoenix.PubSub.node_name(MyApp.PubSub)
# Internamente usa :pg — puedes consultar directamente:
:pg.get_members(:phx_pubsub, "phx:room:lobby")
```

### Phoenix.Presence

`Presence` construye sobre PubSub para tracking de quién está conectado a qué. La diferencia con un simple set de "usuarios online" es que Presence usa CRDTs para reconciliar el estado en clusters:

```elixir
defmodule MyApp.Presence do
  use Phoenix.Presence,
    otp_app: :my_app,
    pubsub_server: MyApp.PubSub
end
```

```elixir
# Trackear que el usuario actual está en "room:lobby"
MyApp.Presence.track(self(), "room:lobby", user_id, %{
  name: "Alice",
  joined_at: DateTime.utc_now()
})

# Listar quién está en "room:lobby" ahora
presences = MyApp.Presence.list("room:lobby")
# %{
#   "user-123" => %{metas: [%{name: "Alice", joined_at: ...}]},
#   "user-456" => %{metas: [%{name: "Bob", joined_at: ...}]}
# }
```

**Presence diff**: cuando el estado cambia, Presence envía un "diff" al topic:

```elixir
# Los suscriptores del topic reciben:
{:user_joined, %{joins: %{"user-789" => %{metas: [...]}}, leaves: %{}}}
{:user_left, %{joins: %{}, leaves: %{"user-123" => %{metas: [...]}}}}
```

**Metas múltiples**: un usuario puede estar conectado desde múltiples dispositivos. Cada conexión agrega una entrada a su lista de `metas`:

```elixir
presences = %{
  "user-123" => %{
    metas: [
      %{device: "mobile", joined_at: ...},  # Tab 1
      %{device: "desktop", joined_at: ...}   # Tab 2
    ]
  }
}
```

### Presence en clusters distribuidos

La magia de Presence es que funciona correctamente con netsplits. Cada nodo mantiene su propia tabla de presencias y las sincroniza via PubSub. Si hay un netsplit, cuando la red se recupera, los estados se reconcilian usando los CRDTs subyacentes.

Esto significa que no puedes tener "doble presencia" permanente — si el netsplit termina, el sistema converge al estado correcto.

---

## Exercises

### Exercise 1: Multi-room Chat Distribuido

**Problem**: Implementa un servidor de chat donde los usuarios pueden unirse a salas y enviar mensajes. El sistema debe funcionar correctamente en un cluster de múltiples nodos: un usuario conectado al nodo A debe recibir mensajes de usuarios conectados al nodo B si ambos están en la misma sala.

**Hints**:
- Cada sala es un topic en PubSub: `"room:#{room_id}"`
- Usa un GenServer por conexión de usuario que se suscribe a los topics relevantes
- El GenServer de conexión recibe los mensajes de PubSub en `handle_info`
- Para listar usuarios en una sala, combina Presence tracking con `Presence.list/2`
- Prueba el escenario distribuido: arranca dos nodos, conecta usuarios a cada uno, y verifica que los mensajes fluyen

**One possible solution**:

```elixir
defmodule Chat.RoomServer do
  use GenServer
  require Logger

  def start_link({user_id, user_name}) do
    GenServer.start_link(__MODULE__, {user_id, user_name})
  end

  def join_room(pid, room_id), do: GenServer.call(pid, {:join, room_id})
  def leave_room(pid, room_id), do: GenServer.call(pid, {:leave, room_id})
  def send_message(pid, room_id, text), do: GenServer.cast(pid, {:send, room_id, text})

  def init({user_id, user_name}) do
    {:ok, %{user_id: user_id, user_name: user_name, rooms: []}}
  end

  def handle_call({:join, room_id}, _from, state) do
    topic = "room:#{room_id}"
    Phoenix.PubSub.subscribe(MyApp.PubSub, topic)

    MyApp.Presence.track(self(), topic, state.user_id, %{
      name: state.user_name,
      node: node(),
      joined_at: DateTime.utc_now()
    })

    Phoenix.PubSub.broadcast(MyApp.PubSub, topic, {
      :user_joined,
      %{user_id: state.user_id, name: state.user_name, node: node()}
    })

    {:reply, :ok, %{state | rooms: [room_id | state.rooms]}}
  end

  def handle_call({:leave, room_id}, _from, state) do
    topic = "room:#{room_id}"
    Phoenix.PubSub.unsubscribe(MyApp.PubSub, topic)

    Phoenix.PubSub.broadcast(MyApp.PubSub, topic, {
      :user_left,
      %{user_id: state.user_id, name: state.user_name}
    })

    {:reply, :ok, %{state | rooms: List.delete(state.rooms, room_id)}}
  end

  def handle_cast({:send, room_id, text}, state) do
    Phoenix.PubSub.broadcast(MyApp.PubSub, "room:#{room_id}", {
      :new_message,
      %{
        from: state.user_id,
        name: state.user_name,
        text: text,
        timestamp: DateTime.utc_now(),
        from_node: node()
      }
    })
    {:noreply, state}
  end

  # Recibir mensajes del chat
  def handle_info({:new_message, msg}, state) do
    Logger.info("[#{state.user_name}] #{msg.name}: #{msg.text} (from #{msg.from_node})")
    {:noreply, state}
  end

  def handle_info({:user_joined, %{name: name}}, state) do
    Logger.info("[Chat] #{name} joined")
    {:noreply, state}
  end

  def handle_info({:user_left, %{name: name}}, state) do
    Logger.info("[Chat] #{name} left")
    {:noreply, state}
  end

  # Presence diffs
  def handle_info(%{event: "presence_diff"}, state), do: {:noreply, state}
end
```

---

### Exercise 2: Presence en Cluster

**Problem**: Extiende el chat del Exercise 1 para mostrar en tiempo real quién está en cada sala, desde qué nodo están conectados, y cuántas pestañas abiertas tienen. El sistema debe reflejar correctamente las desconexiones incluso cuando el nodo del usuario se cae (no solo desconexión limpia).

**Hints**:
- Phoenix.Presence detecta procesos muertos automáticamente — cuando el proceso de conexión muere, el usuario "desaparece" del Presence
- Para simular netsplit: desconecta un nodo con `Node.disconnect/1` y observa que los usuarios de ese nodo eventualmente desaparecen del Presence
- Los `metas` en Presence pueden tener cualquier información — incluye `node: node()` para ver la distribución
- `Presence.list/2` retorna el estado actual; los cambios llegan como `%{event: "presence_diff", payload: diff}` en el topic

**One possible solution**:

```elixir
defmodule Chat.PresenceTracker do
  def users_in_room(room_id) do
    "room:#{room_id}"
    |> MyApp.Presence.list()
    |> Enum.map(fn {user_id, %{metas: metas}} ->
      %{
        user_id: user_id,
        connections: length(metas),
        nodes: Enum.map(metas, & &1.node) |> Enum.uniq(),
        first_joined: metas |> Enum.map(& &1.joined_at) |> Enum.min()
      }
    end)
  end

  def user_count(room_id) do
    "room:#{room_id}"
    |> MyApp.Presence.list()
    |> map_size()
  end

  def users_per_node(room_id) do
    "room:#{room_id}"
    |> MyApp.Presence.list()
    |> Enum.flat_map(fn {user_id, %{metas: metas}} ->
      Enum.map(metas, fn meta -> {meta.node, user_id} end)
    end)
    |> Enum.group_by(&elem(&1, 0), &elem(&1, 1))
    |> Map.new(fn {node, users} -> {node, Enum.uniq(users)} end)
  end
end

# GenServer que suscribe a diffs de presencia y loguea cambios
defmodule Chat.PresenceLogger do
  use GenServer

  def start_link(room_id) do
    GenServer.start_link(__MODULE__, room_id)
  end

  def init(room_id) do
    Phoenix.PubSub.subscribe(MyApp.PubSub, "room:#{room_id}")
    {:ok, %{room_id: room_id}}
  end

  def handle_info(%{event: "presence_diff", payload: diff}, state) do
    Enum.each(diff.joins, fn {user_id, _} ->
      IO.puts("[Presence] User joined: #{user_id}")
    end)

    Enum.each(diff.leaves, fn {user_id, _} ->
      IO.puts("[Presence] User left: #{user_id}")
    end)

    {:noreply, state}
  end

  def handle_info(_, state), do: {:noreply, state}
end
```

---

### Exercise 3: Custom PubSub Adapter

**Problem**: Implementa un adaptador de PubSub que persiste todos los mensajes en ETS antes de distribuirlos. Los procesos que se suscriben **después** de que se publicó un mensaje deben poder recibir los últimos N mensajes del topic (semantica "replay"). Implementa el comportamiento `Phoenix.PubSub.Adapter`.

**Hints**:
- El comportamiento requiere implementar `broadcast/3`, `broadcast_from/3`, y `direct_broadcast/4`
- Usa ETS para el buffer de mensajes: `{topic, timestamp, message}` como schema
- La tabla ETS debe ser shared entre todos los procesos del nodo
- El replay se puede implementar como una función separada: `replay(pubsub, topic, from: datetime)`
- Delega la distribución real al adaptador PG2 subyacente — no reimplementes lo que ya funciona
- Para testing, verifica que un proceso que se une tarde recibe los mensajes históricos

**One possible solution**:

```elixir
defmodule Chat.PersistentPubSub do
  @behaviour Phoenix.PubSub.Adapter

  @max_history 100

  def child_spec(opts) do
    %{
      id: __MODULE__,
      start: {__MODULE__, :start_link, [opts]},
      type: :supervisor
    }
  end

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    table = :"#{name}_history"
    :ets.new(table, [:ordered_set, :public, :named_table])

    # Delegar a PG2 para la distribución real
    Phoenix.PubSub.PG2.start_link(opts)
  end

  # Guardar en ETS y luego distribuir con PG2
  def broadcast(pubsub, topic, message) do
    persist_message(pubsub, topic, message)
    Phoenix.PubSub.PG2.broadcast(pubsub, topic, message)
  end

  def broadcast_from(pubsub, from_pid, topic, message) do
    persist_message(pubsub, topic, message)
    Phoenix.PubSub.PG2.broadcast_from(pubsub, from_pid, topic, message)
  end

  def direct_broadcast(node, pubsub, topic, message) do
    persist_message(pubsub, topic, message)
    Phoenix.PubSub.PG2.direct_broadcast(node, pubsub, topic, message)
  end

  # API adicional: replay de mensajes históricos
  def replay(pubsub, topic, since \\ nil) do
    table = :"#{pubsub}_history"
    pattern = {{topic, :"$1", :"$2"}, [], [{{:"$1", :"$2"}}]}
    messages = :ets.select(table, [pattern])

    filtered = if since do
      Enum.filter(messages, fn {ts, _msg} -> ts >= since end)
    else
      messages
    end

    filtered
    |> Enum.sort_by(&elem(&1, 0))
    |> Enum.map(&elem(&1, 1))
  end

  defp persist_message(pubsub, topic, message) do
    table = :"#{pubsub}_history"
    timestamp = System.monotonic_time(:microsecond)
    key = {topic, timestamp, make_ref()}

    :ets.insert(table, {key, message})
    prune_old_messages(table, topic)
  end

  defp prune_old_messages(table, topic) do
    # Mantener solo los últimos @max_history mensajes por topic
    count = :ets.select_count(table, [{{{topic, :"$1", :"$2"}, :"$3"}, [], [true]}])

    if count > @max_history do
      # Eliminar los más viejos
      oldest_key = :ets.first(table)
      if oldest_key != :"$end_of_table" do
        :ets.delete(table, oldest_key)
      end
    end
  end
end
```

---

## Common Mistakes

**Suscribirse sin desuscribirse en procesos de larga vida**: PubSub limpia las suscripciones cuando el proceso muere, pero si el proceso tiene una vida muy larga y se une/sale de muchos topics dinámicamente, los topics viejos pueden acumular overhead. Llama `unsubscribe/2` explícitamente cuando ya no necesitas el topic.

**broadcast en un loop de alta frecuencia**: Cada `broadcast/3` con el adaptador PG2 implica enviar el mensaje a todos los PIDs suscriptores en todos los nodos. Si tienes 1000 suscriptores y haces 100 broadcasts/segundo, son 100,000 mensajes/segundo. Agrupa o usa `local_broadcast` cuando sea apropiado.

**Confundir Presence.track con suscripción**: `Presence.track/4` trackea que un proceso está "presente" en un topic — no lo suscribe a mensajes del topic. Para recibir mensajes, además debes llamar `PubSub.subscribe/2` separadamente.

**No iniciar Presence en el supervision tree**: `Phoenix.Presence` requiere que el módulo esté en el supervision tree de la aplicación. Si solo usas PubSub sin iniciar Presence, las llamadas a `Presence.track` fallarán.

**Usar topics muy amplios**: `Phoenix.PubSub.broadcast(pubsub, "users", msg)` con miles de usuarios suscriptos es un fan-out masivo. Prefiere topics granulares: `"user:#{user_id}"` en lugar de un topic global.

**No manejar messages en el mailbox de GenServer**: Si un proceso se suscribe a un topic de alto volumen pero procesa lentamente, su mailbox crece sin límite. Considera usar `local_broadcast` y procesar en un proceso separado con backpressure, o usa `GenStage`/`Broadway` para flujos de alta carga.

---

## Verification

```elixir
# Verifica broadcast básico
test_pid = self()
Phoenix.PubSub.subscribe(MyApp.PubSub, "test:topic")
Phoenix.PubSub.broadcast(MyApp.PubSub, "test:topic", {:hello, "world"})

assert_receive {:hello, "world"}, 1_000

# Verifica Presence tracking
{:ok, conn} = Chat.RoomServer.start_link({"user-1", "Alice"})
Chat.RoomServer.join_room(conn, "lobby")

users = Chat.PresenceTracker.users_in_room("lobby")
assert Enum.any?(users, fn u -> u.user_id == "user-1" end)

# Cuando el proceso muere, debe desaparecer de Presence
Process.exit(conn, :kill)
:timer.sleep(100)  # dar tiempo a Presence para procesar

users_after = Chat.PresenceTracker.users_in_room("lobby")
refute Enum.any?(users_after, fn u -> u.user_id == "user-1" end)
```

---

## Summary

Phoenix.PubSub es la columna vertebral de mensajería en Elixir distribuido:

- `broadcast` para fan-out a todo el cluster, `local_broadcast` para eficiencia en el nodo local
- El adaptador PG2 usa `:pg` internamente — entender `:pg` ayuda a depurar PubSub
- `Phoenix.Presence` agrega tracking de estado con CRDTs sobre PubSub — tolerante a netsplits
- Los adaptadores custom permiten añadir persistencia, replay, o backends alternativos
- El diseño de topics es crucial para el rendimiento: más granular = menos fan-out innecesario

---

## What's Next

- **Exercise 15**: RPC — invocación directa de código remoto vs mensajería pub/sub
- **Exercise 53**: Phoenix Channels — PubSub exposed al browser via WebSockets
- **Exercise 54**: Phoenix Presence en Channels — integración completa con el frontend

---

## Resources

- [Phoenix.PubSub documentation](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html)
- [Phoenix.Presence documentation](https://hexdocs.pm/phoenix/Phoenix.Presence.html)
- [Phoenix PubSub GitHub](https://github.com/phoenixframework/phoenix_pubsub)
- [Chris McCord on PubSub internals — ElixirConf](https://www.youtube.com/watch?v=ViSdHuSBM68)
- [Phoenix Presence deep dive — Dockyard blog](https://dockyard.com/blog/2016/03/25/what-makes-phoenix-presence-special-sneak-peek)
