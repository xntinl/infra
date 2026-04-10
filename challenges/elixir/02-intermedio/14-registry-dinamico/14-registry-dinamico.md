# 14 — Registry Dinámico

## Prerequisites

- GenServer básico (Ejercicio 04)
- Supervisor básico (Ejercicio 05)
- DynamicSupervisor o procesos spawn (Ejercicio 01)
- ETS Básico (Ejercicio 13) — Registry usa ETS internamente

---

## Learning Objectives

Al terminar este ejercicio serás capaz de:

1. Arrancar y configurar un `Registry` con claves únicas o duplicadas
2. Registrar procesos bajo nombres arbitrarios con `Registry.register/3`
3. Buscar procesos por nombre con `Registry.lookup/2`
4. Iterar sobre todos los procesos registrados con `Registry.dispatch/3`
5. Entender la limpieza automática cuando un proceso registrado muere
6. Usar Registry como base para sistemas de PubSub y service discovery
7. Distinguir cuándo usar Registry vs ETS directamente

---

## Concepts

### ¿Qué es Registry?

`Registry` es un módulo de Elixir (desde 1.4) que proporciona un registro de procesos por nombre arbitrario. Internamente usa ETS, pero añade semántica de proceso: cuando un proceso registrado muere, su entrada se elimina automáticamente.

Diferencias clave con ETS puro:

| | ETS directo | Registry |
|---|---|---|
| Limpieza al morir proceso | Manual | Automática |
| Clave | Cualquier término | Cualquier término |
| Valor asociado | Cualquier término | Cualquier término (metadata) |
| Soporte claves duplicadas | Con :bag | Con `keys: :duplicate` |
| Dispatch a grupos | Manual | `Registry.dispatch/3` |

### Arrancar un Registry

Registry debe arrancarse como proceso, normalmente en el árbol de supervisión:

```elixir
# En el Supervisor
children = [
  {Registry, keys: :unique, name: MyApp.Registry},
  # ... otros hijos
]

# O directamente para tests/iex
{:ok, _} = Registry.start_link(keys: :unique, name: MyRegistry)
```

Opciones:
- `keys: :unique` — una sola entrada por clave (error si ya registrada)
- `keys: :duplicate` — múltiples entradas por clave (útil para PubSub)

### Registrar un proceso

Un proceso solo puede registrarse a sí mismo. La clave es arbitraria; el valor (metadata) también.

```elixir
# Dentro del proceso que quiere registrarse:
Registry.register(MyApp.Registry, "room:lobby", %{joined_at: DateTime.utc_now()})

# Retorna:
# {:ok, owner}           — registrado con éxito
# {:error, {:already_registered, pid}}  — clave ya existe (con :unique)
```

### Buscar por nombre

```elixir
# Retorna lista de {pid, value} para todos los procesos con esa clave
Registry.lookup(MyApp.Registry, "room:lobby")
# => [{#PID<0.123.0>, %{joined_at: ~U[2024-01-01 00:00:00Z]}}]

# Con :unique, la lista tiene 0 o 1 elemento
case Registry.lookup(MyApp.Registry, "room:lobby") do
  [{pid, _meta}] -> {:found, pid}
  []             -> :not_found
end
```

### Dispatch a grupos

`Registry.dispatch/3` llama a una función por cada proceso registrado bajo una clave:

```elixir
# Útil para broadcast a todos los subscriptores de un topic
Registry.dispatch(MyApp.Registry, "topic:news", fn entries ->
  for {pid, _meta} <- entries do
    send(pid, {:news, "Breaking: Elixir 2.0 released"})
  end
end)
```

### Uso con via tuple

Registry se integra nativamente con los árboles de supervisión a través de `{:via, Registry, {name, key}}`:

```elixir
defmodule RoomServer do
  use GenServer

  def start_link(room_id) do
    name = {:via, Registry, {MyApp.Registry, "room:#{room_id}"}}
    GenServer.start_link(__MODULE__, room_id, name: name)
  end

  # Ahora se puede llamar sin conocer el PID:
  def get_state(room_id) do
    GenServer.call({:via, Registry, {MyApp.Registry, "room:#{room_id}"}}, :get_state)
  end
end
```

### Limpieza automática

Cuando un proceso registrado muere (normal o anormalmente), Registry elimina su entrada automáticamente. Esto elimina la necesidad de limpiar manualmente como con ETS puro.

```elixir
{:ok, pid} = Agent.start(fn -> :ok end)
Registry.register(MyRegistry, :my_key, :meta)
Registry.lookup(MyRegistry, :my_key)  # => [{pid, :meta}]

Agent.stop(pid)
Registry.lookup(MyRegistry, :my_key)  # => []  — limpiado automáticamente
```

### Selección de particiones

Registry acepta `partitions:` para escalar en sistemas multi-core:

```elixir
{Registry, keys: :unique, name: MyApp.Registry, partitions: System.schedulers_online()}
```

---

## Exercises

### Ejercicio 1 — Game Room Registry con lookup por nombre

Implementa un sistema de salas de juego donde cada sala es un GenServer registrado por nombre. Nuevos jugadores pueden unirse a salas sin conocer el PID.

```elixir
# Archivo: lib/game_room.ex

defmodule GameRoom do
  use GenServer

  @registry GameRoom.Registry

  # --- API pública ---

  @doc """
  Arranca una nueva sala de juego con el nombre dado.
  Retorna error si ya existe una sala con ese nombre.

      iex> GameRoom.create("lobby")
      {:ok, #PID<0.123.0>}

      iex> GameRoom.create("lobby")
      {:error, :already_exists}
  """
  def create(room_name) do
    # TODO: Arrancar un GenServer usando {:via, Registry, {@registry, room_name}}
    # Si la sala ya existe, Registry devuelve {:error, {:already_registered, pid}}
    # Traduce ese error a {:error, :already_exists}
    #
    # Pista:
    # case GenServer.start_link(__MODULE__, room_name, name: via_tuple(room_name)) do
    #   {:ok, pid}                                -> {:ok, pid}
    #   {:error, {:already_registered, _pid}}     -> {:error, :already_exists}
    #   {:error, reason}                          -> {:error, reason}
    # end
  end

  @doc """
  Busca una sala por nombre. Retorna el PID o :not_found.

      iex> GameRoom.find("lobby")
      {:ok, #PID<0.123.0>}

      iex> GameRoom.find("inexistente")
      :not_found
  """
  def find(room_name) do
    # TODO: Usar Registry.lookup/2 para buscar la sala
    # Recuerda: con :unique, lookup retorna [] o [{pid, meta}]
  end

  @doc """
  Hace que el proceso llamador se una a la sala.
  Retorna :ok o {:error, :not_found}.
  """
  def join(room_name, player_name) do
    case find(room_name) do
      {:ok, pid} -> GenServer.call(pid, {:join, player_name})
      :not_found -> {:error, :not_found}
    end
  end

  @doc """
  Retorna la lista de jugadores en la sala.
  """
  def players(room_name) do
    case find(room_name) do
      {:ok, pid} -> GenServer.call(pid, :players)
      :not_found -> {:error, :not_found}
    end
  end

  @doc """
  Envía un mensaje a todos los jugadores en la sala.
  """
  def broadcast(room_name, message) do
    case find(room_name) do
      {:ok, pid} -> GenServer.cast(pid, {:broadcast, message})
      :not_found -> {:error, :not_found}
    end
  end

  # --- Internals ---

  defp via_tuple(room_name) do
    # TODO: Construir la via tuple para Registry
    # {:via, Registry, {@registry, room_name}}
  end

  # --- GenServer callbacks ---

  def init(room_name) do
    # TODO: Estado inicial de la sala
    # {:ok, %{name: room_name, players: [], created_at: DateTime.utc_now()}}
  end

  def handle_call({:join, player_name}, _from, state) do
    # TODO: Añadir player_name a la lista de jugadores si no está ya
    # {:reply, :ok, %{state | players: [player_name | state.players]}}
  end

  def handle_call(:players, _from, state) do
    # {:reply, state.players, state}
  end

  def handle_cast({:broadcast, message}, state) do
    # TODO: Imprimir el mensaje (IO.puts) o notificar a jugadores registrados
    # {:noreply, state}
  end
end
```

```elixir
# Archivo: lib/game_room/application.ex (o añadir al supervisor de la app)

defmodule GameRoom.Supervisor do
  use Supervisor

  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    children = [
      # TODO: Incluir el Registry como primer hijo
      # {Registry, keys: :unique, name: GameRoom.Registry}
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

```elixir
# Archivo: test/game_room_test.exs

defmodule GameRoomTest do
  use ExUnit.Case

  setup do
    # Arrancar Registry y Supervisor para cada test
    {:ok, _} = Registry.start_link(keys: :unique, name: GameRoom.Registry)
    :ok
  end

  describe "create/1" do
    test "crea una sala nueva" do
      assert {:ok, pid} = GameRoom.create("test_room")
      assert is_pid(pid)
    end

    test "falla si la sala ya existe" do
      GameRoom.create("duplicada")
      assert {:error, :already_exists} = GameRoom.create("duplicada")
    end

    test "distintas salas son procesos independientes" do
      {:ok, pid1} = GameRoom.create("sala1")
      {:ok, pid2} = GameRoom.create("sala2")
      assert pid1 != pid2
    end
  end

  describe "find/1" do
    test "encuentra una sala existente" do
      {:ok, original_pid} = GameRoom.create("find_me")
      assert {:ok, found_pid} = GameRoom.find("find_me")
      assert original_pid == found_pid
    end

    test "retorna :not_found para salas inexistentes" do
      assert :not_found = GameRoom.find("no_existe")
    end

    test "retorna :not_found tras la muerte del proceso" do
      {:ok, pid} = GameRoom.create("efimera")
      Process.exit(pid, :kill)
      Process.sleep(10)
      assert :not_found = GameRoom.find("efimera")
    end
  end

  describe "join/2 y players/1" do
    test "los jugadores pueden unirse a una sala" do
      GameRoom.create("game1")
      GameRoom.join("game1", "ana")
      GameRoom.join("game1", "bob")

      players = GameRoom.players("game1")
      assert "ana" in players
      assert "bob" in players
    end

    test "join a sala inexistente retorna error" do
      assert {:error, :not_found} = GameRoom.join("no_sala", "jugador")
    end
  end
end
```

---

### Ejercicio 2 — Service Discovery con Registry

Implementa un sistema donde múltiples instancias de servicios se registran con metadatos (capacidad, endpoint) y los clientes pueden descubrir y seleccionar la instancia más apropiada.

```elixir
# Archivo: lib/service_registry.ex

defmodule ServiceRegistry do
  @registry ServiceRegistry.Reg

  @doc """
  Registra el proceso actual como instancia de un servicio.

  ## Parámetros
  - `service_name` — nombre del servicio (ej. :payment, :email)
  - `metadata` — mapa con información de la instancia

  ## Ejemplo

      ServiceRegistry.register(:payment, %{
        endpoint: "http://payment-1:4000",
        capacity: 100,
        healthy: true
      })
  """
  def register(service_name, metadata) do
    # TODO: Registrar con Registry usando keys: :duplicate
    # (múltiples instancias del mismo servicio)
    # Registry.register(@registry, service_name, metadata)
  end

  @doc """
  Lista todas las instancias registradas de un servicio con sus metadatos.

      iex> ServiceRegistry.instances(:payment)
      [
        {#PID<0.1.0>, %{endpoint: "http://payment-1:4000", capacity: 100}},
        {#PID<0.2.0>, %{endpoint: "http://payment-2:4000", capacity: 50}}
      ]
  """
  def instances(service_name) do
    # TODO: Usar Registry.lookup/2 — retorna [{pid, meta}]
  end

  @doc """
  Selecciona la instancia más adecuada para manejar una solicitud.
  Criterio: instancia healthy con mayor capacidad disponible.

  Retorna `{:ok, {pid, metadata}}` o `{:error, :no_instances}`.
  """
  def select_best(service_name) do
    # TODO: Obtener todas las instancias del servicio
    # Filtrar las que tienen healthy: true en metadata
    # Ordenar por capacity descendente
    # Retornar la primera o {:error, :no_instances} si no hay ninguna
  end

  @doc """
  Llama a una función en todas las instancias de un servicio.
  Útil para operaciones de broadcast (ej. invalidar caché en todas las instancias).
  """
  def broadcast(service_name, message) do
    # TODO: Usar Registry.dispatch/3 para enviar mensaje a todos los procesos
    # del servicio
    Registry.dispatch(@registry, service_name, fn entries ->
      # TODO: iterar entries y hacer send(pid, message) a cada uno
    end)
  end

  @doc """
  Actualiza los metadatos del proceso actual para un servicio.
  """
  def update_metadata(service_name, new_metadata) do
    # TODO: Usar Registry.update_value/3
    # Registry.update_value(@registry, service_name, fn _old -> new_metadata end)
  end

  @doc """
  Retorna el número de instancias registradas de un servicio.
  """
  def count(service_name) do
    # TODO: Usar Registry.count_match/3 o length de instances/1
  end

  def child_spec(_opts) do
    # TODO: Para poder incluirlo en un Supervisor
    Registry.child_spec(keys: :duplicate, name: @registry)
  end
end
```

```elixir
# Archivo: test/service_registry_test.exs

defmodule ServiceRegistryTest do
  use ExUnit.Case

  setup do
    {:ok, _} = Registry.start_link(keys: :duplicate, name: ServiceRegistry.Reg)
    :ok
  end

  describe "register/2 e instances/1" do
    test "registra el proceso con metadatos" do
      ServiceRegistry.register(:mailer, %{endpoint: "smtp://localhost", capacity: 50})
      instances = ServiceRegistry.instances(:mailer)

      assert length(instances) == 1
      [{pid, meta}] = instances
      assert pid == self()
      assert meta.capacity == 50
    end

    test "multiples procesos pueden registrarse bajo el mismo servicio" do
      # Arrancar procesos que se registran
      parent = self()

      pid1 =
        spawn(fn ->
          ServiceRegistry.register(:api, %{capacity: 100, healthy: true})
          send(parent, :registered)
          receive do: (:stop -> :ok)
        end)

      pid2 =
        spawn(fn ->
          ServiceRegistry.register(:api, %{capacity: 50, healthy: true})
          send(parent, :registered)
          receive do: (:stop -> :ok)
        end)

      # Esperar que ambos se registren
      assert_receive :registered
      assert_receive :registered

      assert ServiceRegistry.count(:api) == 2

      send(pid1, :stop)
      send(pid2, :stop)
    end

    test "las instancias desaparecen cuando el proceso muere" do
      pid =
        spawn(fn ->
          ServiceRegistry.register(:ephemeral, %{})
          receive do: (:stop -> :ok)
        end)

      Process.sleep(10)
      assert ServiceRegistry.count(:ephemeral) == 1

      send(pid, :stop)
      Process.sleep(10)
      assert ServiceRegistry.count(:ephemeral) == 0
    end
  end

  describe "select_best/1" do
    test "selecciona la instancia healthy con mayor capacidad" do
      parent = self()

      _pid_low =
        spawn(fn ->
          ServiceRegistry.register(:compute, %{capacity: 10, healthy: true})
          send(parent, :ready)
          receive do: (:stop -> :ok)
        end)

      _pid_high =
        spawn(fn ->
          ServiceRegistry.register(:compute, %{capacity: 90, healthy: true})
          send(parent, :ready)
          receive do: (:stop -> :ok)
        end)

      assert_receive :ready
      assert_receive :ready

      {:ok, {_pid, meta}} = ServiceRegistry.select_best(:compute)
      assert meta.capacity == 90
    end

    test "ignora instancias no healthy" do
      parent = self()

      spawn(fn ->
        ServiceRegistry.register(:db, %{capacity: 100, healthy: false})
        send(parent, :ready)
        receive do: (:stop -> :ok)
      end)

      assert_receive :ready
      assert {:error, :no_instances} = ServiceRegistry.select_best(:db)
    end

    test "retorna error cuando no hay instancias" do
      assert {:error, :no_instances} = ServiceRegistry.select_best(:nonexistent)
    end
  end
end
```

---

### Ejercicio 3 — PubSub sobre Registry

Implementa un sistema PubSub (publish/subscribe) simple usando `Registry` con `keys: :duplicate`. Los suscriptores se registran bajo el nombre del topic; los publishers hacen dispatch a ese topic.

```elixir
# Archivo: lib/pub_sub.ex

defmodule PubSub do
  @registry PubSub.Registry

  @doc """
  Suscribe el proceso actual a un topic.
  Un proceso puede suscribirse a múltiples topics.

      iex> PubSub.subscribe("news:tech")
      :ok
  """
  def subscribe(topic) do
    # TODO: Registrar el proceso actual bajo el topic
    # El valor (metadata) puede ser :subscriber o cualquier cosa útil
    # Registry.register(@registry, topic, :subscriber)
    # Simplificar el resultado: :ok
  end

  @doc """
  Cancela la suscripción del proceso actual a un topic.
  """
  def unsubscribe(topic) do
    # TODO: Usar Registry.unregister/2 para eliminar la entrada
    # Registry.unregister(@registry, topic)
  end

  @doc """
  Publica un mensaje en un topic. Todos los suscriptores recibirán
  el mensaje en su mailbox como {:pubsub, topic, message}.

      iex> PubSub.publish("news:tech", %{title: "Elixir 2.0"})
      :ok
  """
  def publish(topic, message) do
    # TODO: Usar Registry.dispatch/3 para enviar el mensaje a todos los suscriptores
    # El mensaje que recibe cada proceso debe tener la forma:
    # {:pubsub, topic, message}
    Registry.dispatch(@registry, topic, fn entries ->
      # TODO: Iterar entries y hacer send/2
    end)

    :ok
  end

  @doc """
  Retorna la lista de PIDs suscritos a un topic.
  """
  def subscribers(topic) do
    # TODO: Usar Registry.lookup/2 y extraer solo los PIDs
    @registry
    |> Registry.lookup(topic)
    |> Enum.map(fn {pid, _meta} -> pid end)
  end

  @doc """
  Retorna el número de suscriptores de un topic.
  """
  def subscriber_count(topic) do
    # TODO: Implementar eficientemente
  end

  def child_spec(_opts) do
    Registry.child_spec(keys: :duplicate, name: @registry)
  end
end
```

```elixir
# Archivo: lib/pub_sub/subscriber.ex

defmodule PubSub.Subscriber do
  @moduledoc """
  GenServer de ejemplo que actúa como suscriptor de PubSub.
  Útil para tests y demostraciones.
  """

  use GenServer

  def start_link(topics) when is_list(topics) do
    GenServer.start_link(__MODULE__, topics)
  end

  @doc """
  Retorna todos los mensajes recibidos por este suscriptor.
  """
  def messages(pid) do
    GenServer.call(pid, :messages)
  end

  def init(topics) do
    # TODO: Suscribirse a todos los topics de la lista
    # Enum.each(topics, &PubSub.subscribe/1)
    {:ok, %{messages: []}}
  end

  def handle_info({:pubsub, topic, message}, state) do
    # TODO: Almacenar el mensaje recibido en el estado
    # {:noreply, %{state | messages: [{topic, message} | state.messages]}}
  end

  def handle_call(:messages, _from, state) do
    {:reply, Enum.reverse(state.messages), state}
  end
end
```

```elixir
# Archivo: test/pub_sub_test.exs

defmodule PubSubTest do
  use ExUnit.Case

  setup do
    {:ok, _} = Registry.start_link(keys: :duplicate, name: PubSub.Registry)
    :ok
  end

  describe "subscribe/1 y publish/1" do
    test "el suscriptor recibe mensajes publicados en su topic" do
      {:ok, sub} = PubSub.Subscriber.start_link(["sports"])

      PubSub.publish("sports", "Gol!")
      Process.sleep(10)

      messages = PubSub.Subscriber.messages(sub)
      assert [{"sports", "Gol!"}] = messages
    end

    test "multiples suscriptores reciben el mismo mensaje" do
      {:ok, sub1} = PubSub.Subscriber.start_link(["breaking"])
      {:ok, sub2} = PubSub.Subscriber.start_link(["breaking"])

      PubSub.publish("breaking", "Noticia importante")
      Process.sleep(10)

      assert [{"breaking", "Noticia importante"}] = PubSub.Subscriber.messages(sub1)
      assert [{"breaking", "Noticia importante"}] = PubSub.Subscriber.messages(sub2)
    end

    test "mensajes de topics distintos no se mezclan" do
      {:ok, tech_sub} = PubSub.Subscriber.start_link(["tech"])
      {:ok, sports_sub} = PubSub.Subscriber.start_link(["sports"])

      PubSub.publish("tech", "Nuevo lenguaje")
      PubSub.publish("sports", "Nuevo record")
      Process.sleep(10)

      tech_messages = PubSub.Subscriber.messages(tech_sub)
      sports_messages = PubSub.Subscriber.messages(sports_sub)

      assert length(tech_messages) == 1
      assert length(sports_messages) == 1
      assert hd(tech_messages) == {"tech", "Nuevo lenguaje"}
      assert hd(sports_messages) == {"sports", "Nuevo record"}
    end

    test "un suscriptor puede recibir mensajes de multiples topics" do
      {:ok, sub} = PubSub.Subscriber.start_link(["alpha", "beta"])

      PubSub.publish("alpha", :msg_a)
      PubSub.publish("beta", :msg_b)
      Process.sleep(10)

      messages = PubSub.Subscriber.messages(sub)
      assert length(messages) == 2
      topics = Enum.map(messages, fn {topic, _} -> topic end)
      assert "alpha" in topics
      assert "beta" in topics
    end
  end

  describe "unsubscribe/1" do
    test "el proceso deja de recibir mensajes tras desuscribirse" do
      PubSub.subscribe("temp_topic")
      PubSub.unsubscribe("temp_topic")

      PubSub.publish("temp_topic", :mensaje)
      Process.sleep(10)

      # El proceso no debería haber recibido el mensaje
      refute_receive {:pubsub, "temp_topic", :mensaje}, 50
    end
  end

  describe "subscribers/1" do
    test "lista los PIDs suscritos al topic" do
      {:ok, sub1} = PubSub.Subscriber.start_link(["list_test"])
      {:ok, sub2} = PubSub.Subscriber.start_link(["list_test"])

      subs = PubSub.subscribers("list_test")
      assert sub1 in subs
      assert sub2 in subs
      assert length(subs) == 2
    end

    test "topic sin suscriptores retorna lista vacia" do
      assert [] = PubSub.subscribers("nadie_aqui")
    end

    test "suscriptor muerto desaparece de la lista" do
      {:ok, sub} = PubSub.Subscriber.start_link(["ephemeral_topic"])
      assert sub in PubSub.subscribers("ephemeral_topic")

      Process.exit(sub, :kill)
      Process.sleep(10)

      refute sub in PubSub.subscribers("ephemeral_topic")
    end
  end

  describe "subscriber_count/1" do
    test "cuenta correctamente los suscriptores" do
      assert PubSub.subscriber_count("count_test") == 0

      {:ok, _} = PubSub.Subscriber.start_link(["count_test"])
      assert PubSub.subscriber_count("count_test") == 1

      {:ok, _} = PubSub.Subscriber.start_link(["count_test"])
      assert PubSub.subscriber_count("count_test") == 2
    end
  end
end
```

---

## Common Mistakes

### 1. Intentar registrar un proceso desde otro proceso

```elixir
# INCORRECTO — solo el proceso dueño puede registrarse
spawn(fn ->
  Registry.register(MyReg, :key, :meta)  # OK — este proceso se registra a sí mismo
end)

# También INCORRECTO — no puedes registrar otro proceso
other_pid = spawn(fn -> :timer.sleep(10_000) end)
Registry.register(MyReg, :key, other_pid)  # ArgumentError — other_pid no es self()
```

### 2. Usar `:unique` cuando se necesita `:duplicate`

```elixir
# Con :unique, solo puede haber un proceso por clave
# Esto es correcto para "sala de juego con nombre único"
# pero INCORRECTO para "múltiples suscriptores al mismo topic"

# PubSub NECESITA :duplicate:
{Registry, keys: :duplicate, name: PubSub.Registry}

# Si usas :unique por error, solo un proceso puede suscribirse a cada topic
# Los demás recibirán {:error, {:already_registered, pid}}
```

### 3. Olvidar que Registry debe estar en el árbol de supervisión

```elixir
# Si el Registry no está supervisado, muere y todos los registros se pierden
# Siempre incluirlo como hijo del Supervisor de la aplicación

children = [
  {Registry, keys: :unique, name: MyApp.Registry},  # primero
  MyApp.SomeWorker,                                   # después
]
```

### 4. No manejar el caso de proceso ya registrado

```elixir
# Con :unique, register falla si la clave ya existe
case Registry.register(MyReg, :my_service, %{}) do
  {:ok, _owner}                           -> :ok
  {:error, {:already_registered, pid}}    -> {:error, :already_running, pid}
end

# No ignorar el error — puede significar que hay un proceso zombie o duplicado
```

### 5. Confundir Registry.unregister con la limpieza automática

```elixir
# La limpieza automática ocurre cuando el PROCESO MUERE
# Registry.unregister es para eliminar una suscripción manualmente
# (ej. cuando un proceso quiere dejar de escuchar un topic sin morir)

# Si el proceso muere sin llamar unregister, Registry igual limpia la entrada
# No necesitas hacer cleanup manual en terminate/2 — Registry lo maneja

# SÍ necesitas unregister cuando quieres cancelar una suscripción específica
# mientras el proceso sigue vivo
def handle_cast(:leave_topic, state) do
  Registry.unregister(MyReg, "my_topic")
  {:noreply, state}
end
```

### 6. Usar Registry para state, no para lookup de procesos

```elixir
# Registry no es una base de datos — es para encontrar procesos
# No almacenes state complejo en la metadata de Registry

# INCORRECTO — Registry como store de datos
Registry.register(MyReg, :config, %{db_url: "...", pool_size: 10, timeout: 5000, ...})

# CORRECTO — Registry solo para encontrar el proceso; el proceso guarda su estado
Registry.register(MyReg, :config_server, %{})
# El ConfigServer GenServer mantiene el estado internamente
```

---

## Verification

```bash
# Crear proyecto
mix new registry_exercises --module RegistryExercises
cd registry_exercises

# Ejecutar todos los tests
mix test

# Tests individuales
mix test test/game_room_test.exs
mix test test/service_registry_test.exs
mix test test/pub_sub_test.exs

# Explorar Registry en iex
iex -S mix
iex> {:ok, _} = Registry.start_link(keys: :unique, name: Demo)
iex> Registry.register(Demo, :my_key, %{hello: :world})
iex> Registry.lookup(Demo, :my_key)
iex> Registry.count(Demo)

# Inspeccionar el ETS subyacente de un Registry
iex> Registry.info(Demo)
```

Salida esperada al pasar todos los tests:

```
Finished in 0.12 seconds
22 tests, 0 failures
```

---

## Summary

Registry es la capa de abstracción sobre ETS para registro de procesos con semántica de ciclo de vida:

| Operación | Función | Notas |
|---|---|---|
| Registrar proceso | `Registry.register/3` | Solo el proceso puede registrarse a sí mismo |
| Buscar por nombre | `Registry.lookup/2` | Retorna `[{pid, meta}]` |
| Broadcast a grupo | `Registry.dispatch/3` | Llama función con todas las entradas |
| Cancelar registro | `Registry.unregister/2` | Manual; automático al morir el proceso |
| Actualizar metadata | `Registry.update_value/3` | Solo el proceso propietario |
| Contar entradas | `Registry.count_match/3` | Con match spec |

| Configuración | Cuándo usar |
|---|---|
| `keys: :unique` | Una instancia por nombre (salas, servicios singulares) |
| `keys: :duplicate` | Múltiples procesos por topic (PubSub, workers de un pool) |
| `partitions:` | Sistemas de alta concurrencia multi-core |

---

## What's Next

- **Ejercicio 15 — Spec y Tipado**: Añadir `@spec` y `@type` al código de Registry para documentación de tipos.
- **Ejercicio 16 — Testing ExUnit**: Cómo testear procesos con Registry, setup y on_exit.
- **Ejercicio 26 — DynamicSupervisor**: Combinar DynamicSupervisor + Registry para salas dinámicas supervisadas.
- Explora `Phoenix.PubSub` — la implementación de producción que Elixir/Phoenix usa, construida sobre principios similares.

---

## Resources

- [Elixir Docs — Registry](https://hexdocs.pm/elixir/Registry.html)
- [Elixir Blog — Registry announcement](https://elixir-lang.org/blog/2017/01/05/elixir-v1-4-0-released/)
- [Elixir Forum — Registry patterns](https://elixirforum.com/t/registry-patterns-and-best-practices)
- [Phoenix.PubSub source](https://github.com/phoenixframework/phoenix_pubsub) — implementación de referencia
- [Elixir in Action — Chapter 12: Registry](https://www.manning.com/books/elixir-in-action-second-edition)
