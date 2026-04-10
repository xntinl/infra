# Ejercicio 53: Phoenix Channels para WebSockets

## Tema
Comunicación bidireccional en tiempo real con Phoenix Channels.

## Conceptos clave
- `use Phoenix.Channel` + `join/3` para autorizar conexiones
- `handle_in/3` para procesar mensajes entrantes del cliente
- `push/3` para enviar al socket individual, `broadcast!/3` para enviar a todos
- `intercept/1` + `handle_out/3` para filtrar broadcasts salientes
- Assigns del socket para almacenar estado por conexión
- Client JS: `new Socket`, `channel.join()`, `channel.on("event", cb)`

---

## Ejercicio 1: Multiplayer Game — Sincronización de posición

### Contexto
Un juego multijugador necesita sincronizar la posición `(x, y)` de cada jugador
en tiempo real. Todos los participantes de una sala deben ver los movimientos
de los demás, pero **no deben recibir su propio movimiento** (evita eco).

### Problema
Dado el siguiente channel incompleto, identifica qué falla y corrígelo:

```elixir
defmodule MyAppWeb.GameChannel do
  use Phoenix.Channel

  # ERROR: falta intercept — los jugadores reciben su propio "move"
  # intercept ["move"]

  def join("game:" <> game_id, %{"token" => token}, socket) do
    case GameAuth.verify(token, game_id) do
      {:ok, player_id} ->
        socket = assign(socket, :player_id, player_id)
        {:ok, socket}

      :error ->
        {:error, %{reason: "unauthorized"}}
    end
  end

  def handle_in("move", %{"x" => x, "y" => y}, socket) do
    payload = %{
      player_id: socket.assigns.player_id,
      x: x,
      y: y
    }
    broadcast!(socket, "move", payload)
    {:noreply, socket}
  end

  # ERROR: sin este callback el broadcast llega también al emisor
  # def handle_out("move", payload, socket) do
  #   if payload.player_id != socket.assigns.player_id do
  #     push(socket, "move", payload)
  #   end
  #   {:noreply, socket}
  # end
end
```

### Solución completa

```elixir
defmodule MyAppWeb.GameChannel do
  use Phoenix.Channel

  # intercept declara qué eventos salientes pasan por handle_out/3
  # Sin esto, broadcast! envía directo a TODOS, incluyendo el emisor
  intercept ["move"]

  def join("game:" <> game_id, %{"token" => token}, socket) do
    case GameAuth.verify(token, game_id) do
      {:ok, player_id} ->
        socket = assign(socket, :player_id, player_id)
        # Notificar a los demás que este jugador entró
        broadcast!(socket, "player_joined", %{player_id: player_id})
        {:ok, socket}

      :error ->
        {:error, %{reason: "unauthorized"}}
    end
  end

  def handle_in("move", %{"x" => x, "y" => y}, socket) do
    payload = %{
      player_id: socket.assigns.player_id,
      x: x,
      y: y
    }
    # Broadcast a todos en "game:{game_id}" — handle_out filtrará
    broadcast!(socket, "move", payload)
    {:noreply, socket}
  end

  # handle_out intercepta el broadcast ANTES de enviarlo al socket
  # Permite filtrar, transformar o descartar el evento por conexión
  def handle_out("move", payload, socket) do
    if payload.player_id != socket.assigns.player_id do
      push(socket, "move", payload)
    end

    # Siempre retorna {:noreply, socket}, incluso si no se hace push
    {:noreply, socket}
  end
end
```

### Client JS equivalente

```javascript
// Conectar al servidor Phoenix
const socket = new Socket("/socket", { params: { token: userToken } })
socket.connect()

const channel = socket.channel("game:sala-42", {})

channel.join()
  .receive("ok", () => console.log("Unido a la partida"))
  .receive("error", ({ reason }) => console.error("Error:", reason))

// Enviar posición al servidor
document.addEventListener("mousemove", (e) => {
  channel.push("move", { x: e.clientX, y: e.clientY })
})

// Recibir posiciones de OTROS jugadores (el propio no llega — filtrado en handle_out)
channel.on("move", ({ player_id, x, y }) => {
  updatePlayerPosition(player_id, x, y)
})

channel.on("player_joined", ({ player_id }) => {
  addPlayerToGame(player_id)
})
```

### Error común #1: olvidar `intercept`

Sin `intercept ["move"]`, el callback `handle_out/3` **nunca se ejecuta**.
Phoenix solo enruta el broadcast por `handle_out` cuando el evento está
declarado en `intercept`. Resultado: cada jugador ve su propio movimiento,
causando duplicación visual y posibles loops.

### Error común #2: retornar `{:ok, payload, socket}` en handle_out

```elixir
# INCORRECTO — handle_out no acepta payload como retorno
def handle_out("move", payload, socket) do
  {:ok, payload, socket}  # Esto genera un error en runtime
end

# CORRECTO
def handle_out("move", payload, socket) do
  push(socket, "move", payload)
  {:noreply, socket}
end
```

---

## Ejercicio 2: Collaborative Document Editing — Operational Transforms básico

### Contexto
Un editor colaborativo donde múltiples usuarios editan el mismo documento.
Cada operación tiene un tipo (`insert` o `delete`), una posición y contenido.
El servidor debe retransmitir la operación a todos **excepto al emisor**.

### Problema
El siguiente código tiene un bug sutil en cómo se maneja el broadcast:

```elixir
defmodule MyAppWeb.DocChannel do
  use Phoenix.Channel

  def join("doc:" <> doc_id, _params, socket) do
    # ERROR: no se asigna user_id al socket — handle_in no podrá usarlo
    {:ok, socket}
  end

  def handle_in("operation", %{"type" => type, "pos" => pos, "content" => content}, socket) do
    op = %{
      type: type,
      pos: pos,
      content: content,
      # ERROR: socket.assigns.user_id no existe — crasheará
      author: socket.assigns.user_id,
      ts: System.system_time(:millisecond)
    }

    # ERROR: broadcast! envía a TODOS, incluido el autor
    broadcast!(socket, "operation", op)
    {:reply, {:ok, %{ts: op.ts}}, socket}
  end
end
```

### Solución completa

```elixir
defmodule MyAppWeb.DocChannel do
  use Phoenix.Channel

  intercept ["operation"]

  def join("doc:" <> doc_id, %{"user_id" => user_id}, socket) do
    socket =
      socket
      |> assign(:user_id, user_id)
      |> assign(:doc_id, doc_id)

    # Enviar estado actual del documento solo al que se une
    current_doc = DocStore.get(doc_id)
    {:ok, %{content: current_doc}, socket}
  end

  def handle_in("operation", %{"type" => type, "pos" => pos, "content" => content}, socket) do
    op = %{
      type: type,
      pos: pos,
      content: content,
      author: socket.assigns.user_id,
      ts: System.system_time(:millisecond)
    }

    # Persistir la operación antes de hacer broadcast
    :ok = DocStore.apply_operation(socket.assigns.doc_id, op)

    broadcast!(socket, "operation", op)

    # Confirmar al emisor con el timestamp del servidor
    {:reply, {:ok, %{ts: op.ts}}, socket}
  end

  def handle_out("operation", %{author: author} = op, socket) do
    # No reenviar al autor — él ya aplicó la operación localmente
    if author != socket.assigns.user_id do
      push(socket, "operation", op)
    end

    {:noreply, socket}
  end
end
```

### Client JS equivalente

```javascript
const channel = socket.channel("doc:mi-documento", { user_id: currentUserId })

channel.join()
  .receive("ok", ({ content }) => initEditor(content))

// Enviar operación de inserción
function insertText(pos, text) {
  channel.push("operation", { type: "insert", pos, content: text })
    .receive("ok", ({ ts }) => console.log("Op confirmada en:", ts))
    .receive("error", (err) => rollbackOperation())
}

// Recibir operaciones de otros colaboradores
channel.on("operation", (op) => {
  applyRemoteOperation(op)  // Aplicar transform y actualizar el editor
})
```

### Reconciliación de conflictos simple

Cuando dos usuarios editan simultáneamente la misma posición, las operaciones
pueden llegar en distinto orden. Una estrategia básica es usar el timestamp
del servidor como desempate:

```elixir
defmodule DocStore do
  # Aplicar operación con transformación básica de posición
  # Si op.ts < last_op.ts, la posición puede necesitar ajuste
  def apply_operation(doc_id, op) do
    Agent.update(__MODULE__, fn state ->
      doc = Map.get(state, doc_id, %{content: "", ops: []})
      adjusted_op = maybe_transform(op, doc.ops)
      new_content = apply_to_content(doc.content, adjusted_op)
      Map.put(state, doc_id, %{content: new_content, ops: [adjusted_op | doc.ops]})
    end)
  end

  defp maybe_transform(op, [last | _]) when op.ts < last.ts do
    # Ajustar posición si hay operaciones posteriores al timestamp del op
    %{op | pos: op.pos + String.length(last.content)}
  end
  defp maybe_transform(op, _), do: op
end
```

---

## Ejercicio 3: Live Notifications con filtros por preferencias de usuario

### Contexto
Un sistema de notificaciones en tiempo real donde cada usuario configura
qué categorías quiere recibir. El canal debe filtrar notificaciones según
las preferencias almacenadas en el socket assign.

### Problema
Detecta los dos errores en este código:

```elixir
defmodule MyAppWeb.NotificationChannel do
  use Phoenix.Channel

  # ERROR 1: sin intercept, handle_out nunca se ejecuta
  # intercept ["notification"]

  def join("notifications:" <> user_id, _params, socket) do
    # ERROR 2: no se cargan las preferencias en el socket
    {:ok, socket}
  end

  def handle_out("notification", payload, socket) do
    # Este callback NUNCA se ejecuta sin intercept ["notification"]
    if payload.category in socket.assigns.preferences do
      push(socket, "notification", payload)
    end

    {:noreply, socket}
  end
end
```

### Solución completa

```elixir
defmodule MyAppWeb.NotificationChannel do
  use Phoenix.Channel

  # Necesario para que handle_out intercepte el evento antes de enviar
  intercept ["notification"]

  def join("notifications:" <> user_id, %{"token" => token}, socket) do
    case UserAuth.verify_token(token) do
      {:ok, ^user_id} ->
        prefs = UserPreferences.get_categories(user_id)

        socket =
          socket
          |> assign(:user_id, user_id)
          |> assign(:preferences, prefs)  # Cargar preferencias en el socket

        {:ok, socket}

      _ ->
        {:error, %{reason: "unauthorized"}}
    end
  end

  def handle_in("update_preferences", %{"categories" => categories}, socket) do
    # Permitir actualización de preferencias sin reconectar
    socket = assign(socket, :preferences, categories)
    {:reply, :ok, socket}
  end

  def handle_out("notification", payload, socket) do
    if payload.category in socket.assigns.preferences do
      push(socket, "notification", payload)
    end

    {:noreply, socket}
  end
end
```

### Emitir una notificación desde el servidor

```elixir
defmodule MyApp.Notifications do
  alias MyAppWeb.Endpoint

  def broadcast_notification(user_id, category, message) do
    Endpoint.broadcast(
      "notifications:#{user_id}",
      "notification",
      %{category: category, message: message, ts: DateTime.utc_now()}
    )
  end
end
```

### Client JS equivalente

```javascript
const channel = socket.channel("notifications:user-123", { token: authToken })

channel.join()
  .receive("ok", () => console.log("Notificaciones activas"))

// Recibir solo notificaciones según preferencias (filtrado en servidor)
channel.on("notification", ({ category, message, ts }) => {
  showToast(category, message)
})

// Actualizar preferencias sin reconectar
function updatePreferences(categories) {
  channel.push("update_preferences", { categories })
    .receive("ok", () => console.log("Preferencias actualizadas"))
}
```

---

## Resumen de errores comunes

| Error | Síntoma | Solución |
|-------|---------|---------|
| Omitir `intercept ["evento"]` | `handle_out/3` nunca se ejecuta | Declarar `intercept` con los eventos a interceptar |
| No asignar estado en `join/3` | `socket.assigns.clave` falla en runtime | Usar `assign/3` dentro de `join/3` antes de retornar |
| Retornar `{:ok, payload, socket}` en `handle_out` | Error en runtime | Usar `push/3` + `{:noreply, socket}` |
| `broadcast!` sin `handle_out` para filtrar | El emisor recibe su propio mensaje | Interceptar y filtrar por `socket.assigns.user_id` |
| No manejar `{:error, reason}` en `join/3` | Clientes no autorizados se unen | Retornar `{:error, %{reason: "..."}}` en caso de fallo |

---

## Cómo probar

```bash
# Crear la app Phoenix (solo para contexto, no ejecutar en el ejercicio)
mix phx.new my_game --no-ecto
cd my_game

# El channel va en: lib/my_game_web/channels/game_channel.ex
# El socket va en:  lib/my_game_web/channels/user_socket.ex

# En user_socket.ex agregar:
# channel "game:*", MyAppWeb.GameChannel
```

```elixir
# Test básico del channel con Phoenix.ChannelTest
defmodule MyAppWeb.GameChannelTest do
  use MyAppWeb.ChannelCase

  setup do
    {:ok, _, socket} =
      MyAppWeb.UserSocket
      |> socket("user_id", %{player_id: "p1"})
      |> subscribe_and_join(MyAppWeb.GameChannel, "game:sala-1", %{"token" => "valid"})

    %{socket: socket}
  end

  test "move no llega al emisor", %{socket: socket} do
    push(socket, "move", %{"x" => 10, "y" => 20})
    refute_push "move", %{player_id: "p1"}
  end

  test "move llega a otros jugadores" do
    {:ok, _, other_socket} =
      MyAppWeb.UserSocket
      |> socket("user_id", %{player_id: "p2"})
      |> subscribe_and_join(MyAppWeb.GameChannel, "game:sala-1", %{"token" => "valid"})

    push(other_socket, "move", %{"x" => 5, "y" => 15})
    assert_push "move", %{player_id: "p2", x: 5, y: 15}
  end
end
```
