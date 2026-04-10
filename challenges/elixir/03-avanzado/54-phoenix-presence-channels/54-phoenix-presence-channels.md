# Ejercicio 54: Channel Presence y Room Management

## Tema
Phoenix.Presence para tracking de usuarios online en canales en tiempo real.

## Conceptos clave
- `use Phoenix.Presence` para definir un módulo de presencia
- `Presence.track/3` para registrar un socket en el sistema de presencia
- `Presence.list/1` para obtener todos los usuarios presentes en un topic
- `handle_info {:DOWN, ...}` para detectar desconexiones inesperadas
- Evento `presence_diff` en el cliente: contiene `joins` y `leaves`
- Metadata de presencia: `user_id`, `username`, `role`, `joined_at`, `typing`

---

## Ejercicio 1: Who's Online — Lista de usuarios conectados

### Contexto
Un canal de chat necesita mostrar a todos los participantes quién está
conectado en tiempo real. Al unirse, el usuario aparece en la lista;
al desconectarse, desaparece automáticamente.

### Definir el módulo de Presence

```elixir
# lib/my_app_web/channels/room_presence.ex
defmodule MyAppWeb.RoomPresence do
  use Phoenix.Presence,
    otp_app: :my_app,
    pubsub_server: MyApp.PubSub
end
```

```elixir
# lib/my_app/application.ex — agregar al árbol de supervisión
children = [
  MyApp.Repo,
  MyAppWeb.Endpoint,
  MyAppWeb.RoomPresence  # Debe supervisarse explícitamente
]
```

### Problema
El siguiente channel tiene dos errores relacionados con Presence:

```elixir
defmodule MyAppWeb.RoomChannel do
  use Phoenix.Channel
  alias MyAppWeb.RoomPresence

  def join("room:" <> room_id, %{"username" => username}, socket) do
    socket = assign(socket, :username, username)

    # ERROR 1: track debe llamarse FUERA del join, desde handle_info o
    # con send(self(), :after_join). Llamarlo aquí puede causar race condition
    # porque el socket aún no está completamente inicializado.
    RoomPresence.track(socket, socket.assigns.user_id, %{
      username: username,
      online_at: System.system_time(:second)
    })

    {:ok, socket}
  end

  def handle_info(:after_join, socket) do
    # ERROR 2: push de presences se haría aquí, pero join no lo programó
    {:noreply, socket}
  end
end
```

### Solución completa

```elixir
defmodule MyAppWeb.RoomChannel do
  use Phoenix.Channel
  alias MyAppWeb.RoomPresence

  def join("room:" <> room_id, %{"user_id" => user_id, "username" => username}, socket) do
    socket =
      socket
      |> assign(:user_id, user_id)
      |> assign(:username, username)
      |> assign(:room_id, room_id)

    # Programar el track para después de que join/3 finalice
    # Garantiza que el socket esté completamente inicializado
    send(self(), :after_join)

    {:ok, socket}
  end

  def handle_info(:after_join, socket) do
    # Enviar al nuevo usuario la lista ACTUAL de presencias
    {:ok, _} = RoomPresence.track(socket, socket.assigns.user_id, %{
      username: socket.assigns.username,
      online_at: System.system_time(:second)
    })

    # push de presences solo al socket que acaba de unirse
    push(socket, "presence_state", RoomPresence.list(socket))

    {:noreply, socket}
  end
end
```

### Client JS equivalente

```javascript
import { Presence } from "phoenix"

const channel = socket.channel("room:general", { user_id: "u1", username: "alice" })
const presence = new Presence(channel)

channel.join()

// presence_state es el snapshot inicial cuando te unes
// presence_diff llega en cada cambio posterior (join/leave)
presence.onSync(() => {
  const users = presence.list((id, { metas: [first] }) => ({
    id,
    username: first.username,
    onlineAt: first.online_at
  }))

  renderUserList(users)
})

presence.onJoin((id, current, newPresence) => {
  if (!current) console.log(`${newPresence.metas[0].username} se unió`)
})

presence.onLeave((id, current, leftPresence) => {
  if (current.metas.length === 0) {
    console.log(`${leftPresence.metas[0].username} se desconectó`)
  }
})
```

### Error común: no supervisar RoomPresence

```elixir
# Sin esto, RoomPresence.track/3 lanza:
# ** (exit) no process: the process is not alive or
#    there's no process currently associated with the given name
#
# Solución: agregar al árbol de supervisión en application.ex
children = [
  MyAppWeb.RoomPresence  # OBLIGATORIO
]
```

---

## Ejercicio 2: Typing Indicators — Quién está escribiendo

### Contexto
En un chat grupal, cuando un usuario empieza a escribir, los demás ven
"Alice está escribiendo...". El indicador desaparece automáticamente
después de 3 segundos de inactividad (sin depender del cliente).

### Problema
Este código tiene un bug grave en la gestión del timeout:

```elixir
defmodule MyAppWeb.ChatChannel do
  use Phoenix.Channel
  alias MyAppWeb.RoomPresence

  def join("chat:" <> room_id, %{"user_id" => user_id}, socket) do
    send(self(), :after_join)
    {:ok, assign(socket, :user_id, user_id)}
  end

  def handle_info(:after_join, socket) do
    {:ok, _} = RoomPresence.track(socket, socket.assigns.user_id, %{
      typing: false
    })
    push(socket, "presence_state", RoomPresence.list(socket))
    {:noreply, socket}
  end

  def handle_in("typing_start", _payload, socket) do
    # Actualizar metadata de presencia
    RoomPresence.update(socket, socket.assigns.user_id, fn meta ->
      Map.put(meta, :typing, true)
    end)

    # ERROR: si el cliente envía "typing_start" repetidamente cada segundo,
    # se acumulan N timers. Cada uno llama a typing_stop al expirar.
    # Resultado: el indicador parpadea o se limpia antes de tiempo.
    Process.send_after(self(), :typing_timeout, 3_000)

    {:noreply, socket}
  end

  def handle_info(:typing_timeout, socket) do
    RoomPresence.update(socket, socket.assigns.user_id, fn meta ->
      Map.put(meta, :typing, false)
    end)
    {:noreply, socket}
  end
end
```

### Solución completa — cancelar timer anterior antes de crear uno nuevo

```elixir
defmodule MyAppWeb.ChatChannel do
  use Phoenix.Channel
  alias MyAppWeb.RoomPresence

  def join("chat:" <> room_id, %{"user_id" => user_id, "username" => username}, socket) do
    socket =
      socket
      |> assign(:user_id, user_id)
      |> assign(:username, username)
      |> assign(:typing_timer, nil)  # Inicializar referencia del timer

    send(self(), :after_join)
    {:ok, socket}
  end

  def handle_info(:after_join, socket) do
    {:ok, _} = RoomPresence.track(socket, socket.assigns.user_id, %{
      username: socket.assigns.username,
      typing: false,
      online_at: System.system_time(:second)
    })

    push(socket, "presence_state", RoomPresence.list(socket))
    {:noreply, socket}
  end

  def handle_in("typing_start", _payload, socket) do
    # Cancelar timer previo antes de crear uno nuevo
    # Esto evita la acumulación de timers con keystroke rápido
    if socket.assigns.typing_timer do
      Process.cancel_timer(socket.assigns.typing_timer)
    end

    RoomPresence.update(socket, socket.assigns.user_id, fn meta ->
      Map.put(meta, :typing, true)
    end)

    timer_ref = Process.send_after(self(), :typing_timeout, 3_000)
    {:noreply, assign(socket, :typing_timer, timer_ref)}
  end

  def handle_in("typing_stop", _payload, socket) do
    if socket.assigns.typing_timer do
      Process.cancel_timer(socket.assigns.typing_timer)
    end

    RoomPresence.update(socket, socket.assigns.user_id, fn meta ->
      Map.put(meta, :typing, false)
    end)

    {:noreply, assign(socket, :typing_timer, nil)}
  end

  def handle_info(:typing_timeout, socket) do
    RoomPresence.update(socket, socket.assigns.user_id, fn meta ->
      Map.put(meta, :typing, false)
    end)

    {:noreply, assign(socket, :typing_timer, nil)}
  end
end
```

### Client JS equivalente

```javascript
const channel = socket.channel("chat:general", { user_id: userId, username })
const presence = new Presence(channel)

channel.join()

let typingTimeout = null

messageInput.addEventListener("input", () => {
  channel.push("typing_start", {})

  // El servidor maneja el timeout de 3s — el cliente solo avisa
  // No es necesario que el cliente envíe "typing_stop" salvo al submit
})

messageInput.addEventListener("blur", () => {
  channel.push("typing_stop", {})
})

// Renderizar indicadores de quién escribe
presence.onSync(() => {
  const typing = presence.list((id, { metas: [first] }) => first)
    .filter(u => u.typing && u.user_id !== currentUserId)
    .map(u => u.username)

  renderTypingIndicator(typing)
})
```

---

## Ejercicio 3: Room Capacity Limits — Límite de aforo

### Contexto
Una sala de videollamada tiene un máximo de 10 participantes. Al intentar
unirse cuando la sala está llena, el servidor rechaza la conexión con
un error descriptivo. Los participantes actuales ven el conteo actualizado.

### Problema
El siguiente código tiene una race condition crítica:

```elixir
defmodule MyAppWeb.CallChannel do
  use Phoenix.Channel
  alias MyAppWeb.RoomPresence

  @max_capacity 10

  def join("call:" <> room_id, %{"user_id" => user_id}, socket) do
    current_count = RoomPresence.list("call:#{room_id}") |> map_size()

    if current_count >= @max_capacity do
      {:error, %{reason: "room_full", capacity: @max_capacity}}
    else
      # RACE CONDITION: entre el check y el track, otro usuario puede
      # haberse unido. Con 9 presentes y 2 intentos simultáneos,
      # ambos pasan el check y la sala queda con 11 usuarios.
      send(self(), :after_join)
      {:ok, assign(socket, :user_id, user_id)}
    end
  end
end
```

### Solución — usar Presence como fuente de verdad atómica

La race condition es inherente a sistemas distribuidos. La mitigación más
práctica en Phoenix es aceptar que Presence.track es la operación atómica
real, y agregar una validación post-join:

```elixir
defmodule MyAppWeb.CallChannel do
  use Phoenix.Channel
  alias MyAppWeb.RoomPresence

  @max_capacity 10

  def join("call:" <> room_id, %{"user_id" => user_id, "username" => username}, socket) do
    presences = RoomPresence.list("call:#{room_id}")

    # Verificar que el usuario no esté ya conectado (rejoin tras reconexión)
    already_present = Map.has_key?(presences, user_id)

    cond do
      already_present ->
        # Permitir reconexión sin contar como slot nuevo
        socket =
          socket
          |> assign(:user_id, user_id)
          |> assign(:username, username)
          |> assign(:room_id, room_id)

        send(self(), :after_join)
        {:ok, socket}

      map_size(presences) >= @max_capacity ->
        {:error, %{reason: "room_full", capacity: @max_capacity, current: map_size(presences)}}

      true ->
        socket =
          socket
          |> assign(:user_id, user_id)
          |> assign(:username, username)
          |> assign(:room_id, room_id)

        send(self(), :after_join)
        {:ok, socket}
    end
  end

  def handle_info(:after_join, socket) do
    presences = RoomPresence.list(socket)

    # Segunda verificación DESPUÉS del track para detectar race condition
    # Si la sala ya está llena, desconectar limpiamente
    if map_size(presences) > @max_capacity do
      push(socket, "error", %{reason: "room_full"})
      # Terminar el proceso del channel — el cliente recibirá phx_close
      {:stop, :normal, socket}
    else
      {:ok, _} = RoomPresence.track(socket, socket.assigns.user_id, %{
        username: socket.assigns.username,
        joined_at: System.system_time(:second)
      })

      push(socket, "presence_state", RoomPresence.list(socket))

      # Informar a todos sobre el nuevo aforo
      broadcast!(socket, "room_info", %{
        current_count: map_size(RoomPresence.list(socket)),
        max_capacity: @max_capacity
      })

      {:noreply, socket}
    end
  end

  def handle_in("get_capacity", _payload, socket) do
    count = RoomPresence.list(socket) |> map_size()
    {:reply, {:ok, %{current: count, max: @max_capacity}}, socket}
  end
end
```

### Client JS equivalente

```javascript
const channel = socket.channel("call:sala-privada", { user_id: userId, username })

channel.join()
  .receive("ok", () => {
    console.log("Unido a la llamada")
    initWebRTC()
  })
  .receive("error", ({ reason, capacity, current }) => {
    if (reason === "room_full") {
      showError(`Sala llena (${current}/${capacity} participantes)`)
    }
  })

channel.on("room_info", ({ current_count, max_capacity }) => {
  updateCapacityBadge(current_count, max_capacity)
})

// Detectar si el servidor nos desconecta por race condition
channel.onClose(() => {
  showError("La sala está llena, intenta de nuevo")
})
```

---

## Comparativa: `push` vs `broadcast!` vs `broadcast_from!`

| Función | Destinatario | Caso de uso |
|---------|-------------|-------------|
| `push/3` | Solo el socket actual | Estado inicial, confirmaciones personales |
| `broadcast!/3` | Todos en el topic (incluido emisor) | Eventos globales, con `intercept` para filtrar |
| `broadcast_from!/3` | Todos excepto el emisor | Notificaciones a otros, sin necesidad de `intercept` |

```elixir
# broadcast_from!/3 es equivalente a broadcast! + handle_out con filtro del emisor
# Úsalo cuando no necesitas transformar el payload por socket

def handle_in("message", %{"text" => text}, socket) do
  payload = %{text: text, author: socket.assigns.username}

  # Enviar a todos EXCEPTO el emisor — sin necesitar intercept
  broadcast_from!(socket, "message", payload)

  # Confirmar al emisor
  {:reply, :ok, socket}
end
```

---

## Resumen de errores comunes con Presence

| Error | Síntoma | Solución |
|-------|---------|---------|
| No agregar `RoomPresence` al árbol de supervisión | `no process` en runtime al hacer `track/3` | Agregar a `children` en `application.ex` |
| Llamar `track/3` directamente en `join/3` | Race condition, socket no inicializado | Usar `send(self(), :after_join)` |
| No cancelar timer anterior en typing | Indicador parpadea, desaparece prematuramente | Guardar `timer_ref` en assigns y cancelar con `Process.cancel_timer/1` |
| Asumir que el check de capacidad en `join/3` es atómico | Sala supera el límite bajo carga concurrente | Verificar también en `handle_info(:after_join, ...)` |
| No inicializar assigns en `join/3` | `KeyError` en `handle_in` o `handle_info` | Asignar todos los valores necesarios antes de retornar `{:ok, socket}` |

---

## Test con ChannelCase y Presence

```elixir
defmodule MyAppWeb.CallChannelTest do
  use MyAppWeb.ChannelCase

  alias MyAppWeb.RoomPresence

  test "rechaza join cuando la sala está llena" do
    # Llenar la sala hasta el máximo
    Enum.each(1..10, fn i ->
      {:ok, _, _socket} =
        MyAppWeb.UserSocket
        |> socket("user_#{i}", %{})
        |> subscribe_and_join(
          MyAppWeb.CallChannel,
          "call:sala-test",
          %{"user_id" => "u#{i}", "username" => "User #{i}"}
        )
    end)

    # El undécimo debe ser rechazado
    assert {:error, %{reason: "room_full"}} =
      MyAppWeb.UserSocket
      |> socket("user_11", %{})
      |> subscribe_and_join(
        MyAppWeb.CallChannel,
        "call:sala-test",
        %{"user_id" => "u11", "username" => "User 11"}
      )
  end

  test "presencia se actualiza cuando un usuario se desconecta", %{} do
    {:ok, _, socket1} =
      MyAppWeb.UserSocket
      |> socket("user_1", %{})
      |> subscribe_and_join(
        MyAppWeb.CallChannel,
        "call:sala-dc",
        %{"user_id" => "u1", "username" => "Alice"}
      )

    assert 1 = RoomPresence.list("call:sala-dc") |> map_size()

    Process.exit(socket1.channel_pid, :normal)

    # Dar tiempo al sistema de presencia para procesar la salida
    :timer.sleep(50)

    assert 0 = RoomPresence.list("call:sala-dc") |> map_size()
  end
end
```

---

## Cómo configurar el socket y el endpoint

```elixir
# lib/my_app_web/channels/user_socket.ex
defmodule MyAppWeb.UserSocket do
  use Phoenix.Socket

  channel "room:*",  MyAppWeb.RoomChannel
  channel "chat:*",  MyAppWeb.ChatChannel
  channel "call:*",  MyAppWeb.CallChannel

  def connect(%{"token" => token}, socket, _connect_info) do
    case Phoenix.Token.verify(MyAppWeb.Endpoint, "user socket", token) do
      {:ok, user_id} -> {:ok, assign(socket, :user_id, user_id)}
      {:error, _}    -> :error
    end
  end

  def id(socket), do: "user_socket:#{socket.assigns.user_id}"
end
```

```elixir
# lib/my_app_web/endpoint.ex — montar el socket
socket "/socket", MyAppWeb.UserSocket,
  websocket: true,
  longpoll: false
```
