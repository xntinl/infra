# 51. Phoenix LiveView — Real-time Dashboard

**Difficulty**: Avanzado

---

## Prerequisites

- Phoenix Framework básico (rutas, controllers, templates)
- Ecto y changesets
- GenServer y mensajes (`send/2`, `Process.send_after/3`)
- Phoenix.PubSub
- HEEx templates

---

## Learning Objectives

1. Montar un LiveView con estado reactivo usando `mount/3` y `assign/3`
2. Manejar eventos del cliente con `handle_event/3`
3. Actualizar el socket periódicamente con `handle_info/2`
4. Suscribirse a PubSub para datos multiusuario en tiempo real
5. Validar formularios en vivo con changesets y `phx-change`

---

## Concepts

### LiveView lifecycle: mount → render → handle_event / handle_info

Un LiveView arranca en `mount/3`, donde inicializas el socket con `assign`. Desde ese momento, cada vez que el socket cambia, Phoenix re-renderiza **solo el diff** del template HEEx — sin recargar la página ni escribir JavaScript.

El patrón más común para "ticks" periódicos es enviar un mensaje a sí mismo y reencolarlo dentro del handler:

```elixir
defmodule MyAppWeb.MetricsLive do
  use MyAppWeb, :live_view

  @tick_ms 1_000

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket), do: Process.send_after(self(), :tick, @tick_ms)
    {:ok, assign(socket, cpu: 0.0, mem: 0.0, history: [])}
  end

  @impl true
  def handle_info(:tick, socket) do
    Process.send_after(self(), :tick, @tick_ms)
    metrics = fetch_metrics()

    socket =
      socket
      |> assign(cpu: metrics.cpu, mem: metrics.mem)
      |> update(:history, fn h -> Enum.take([metrics | h], 60) end)

    {:noreply, socket}
  end

  defp fetch_metrics do
    # :cpu_sup.avg1() devuelve carga * 256; normaliza a %
    cpu = :cpu_sup.avg1() / 256 * 100
    mem_info = :memsup.get_system_memory_data()
    total = mem_info[:total_memory]
    free  = mem_info[:free_memory]
    %{cpu: Float.round(cpu, 1), mem: Float.round((total - free) / total * 100, 1)}
  end
end
```

### PubSub en mount: datos compartidos entre sesiones

Suscribirse en `mount` permite que cualquier proceso publique datos y todos los LiveViews suscritos los reciban simultáneamente. El guard `connected?(socket)` evita la doble suscripción durante el render estático inicial (SSR).

```elixir
@impl true
def mount(_params, _session, socket) do
  if connected?(socket) do
    Phoenix.PubSub.subscribe(MyApp.PubSub, "system:metrics")
  end
  {:ok, assign(socket, metrics: %{})}
end

@impl true
def handle_info({:metrics_update, data}, socket) do
  {:noreply, assign(socket, metrics: data)}
end

# En cualquier otro proceso:
# Phoenix.PubSub.broadcast(MyApp.PubSub, "system:metrics", {:metrics_update, data})
```

### Validación en tiempo real con phx-change y changesets

`phx-change` dispara `handle_event("validate", params, socket)` con cada keystroke. El changeset se aplica con `action: :validate` para activar los errores sin intentar persistir.

```elixir
@impl true
def handle_event("validate", %{"user" => params}, socket) do
  changeset =
    %User{}
    |> User.changeset(params)
    |> Map.put(:action, :validate)

  {:noreply, assign(socket, form: to_form(changeset))}
end
```

```heex
<.form for={@form} phx-change="validate" phx-submit="save">
  <.input field={@form[:email]} label="Email" />
  <.input field={@form[:name]}  label="Name"  />
  <.button>Save</.button>
</.form>
```

---

## Exercises

### Exercise 1: Métricas en tiempo real con barra ASCII

**Problem**: Un equipo de ops necesita un dashboard en la terminal — pero también en el browser — que muestre CPU y memoria con un historial de 30 segundos. Sin JS externo. El gráfico debe ser una barra ASCII que cambia de color según el umbral.

**Hints**:
1. Usa `connected?(socket)` para no disparar el tick en el render SSR inicial.
2. `update(:history, fn h -> Enum.take([new | h], 30) end)` mantiene la ventana deslizante sin crear listas nuevas innecesariamente.
3. En HEEx, usa una función helper `bar/1` que devuelva una string de `█` repetidos para representar el porcentaje.

**One possible solution**:

```elixir
# lib/my_app_web/live/metrics_live.ex
defmodule MyAppWeb.MetricsLive do
  use MyAppWeb, :live_view

  @tick_ms 1_000
  @bar_width 30

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket), do: Process.send_after(self(), :tick, @tick_ms)
    sample = sample_metrics()
    {:ok, assign(socket, cpu: sample.cpu, mem: sample.mem, history: [sample])}
  end

  @impl true
  def handle_info(:tick, socket) do
    Process.send_after(self(), :tick, @tick_ms)
    sample = sample_metrics()

    socket =
      socket
      |> assign(cpu: sample.cpu, mem: sample.mem)
      |> update(:history, &Enum.take([sample | &1], 30))

    {:noreply, socket}
  end

  def bar(pct) do
    filled = round(pct / 100 * @bar_width)
    empty  = @bar_width - filled
    String.duplicate("█", filled) <> String.duplicate("░", empty)
  end

  defp sample_metrics do
    # Simulación; reemplaza con :cpu_sup / :memsup en producción
    %{
      cpu: Float.round(:rand.uniform() * 100, 1),
      mem: Float.round(40 + :rand.uniform() * 40, 1),
      ts:  DateTime.utc_now()
    }
  end
end
```

```heex
<%# lib/my_app_web/live/metrics_live.html.heex %>
<div class="font-mono p-4 bg-gray-900 text-green-400 min-h-screen">
  <h1 class="text-xl mb-4">System Metrics</h1>

  <div class="mb-2">
    CPU  <%= @cpu %>%  [<%= bar(@cpu) %>]
  </div>
  <div class="mb-6">
    MEM  <%= @mem %>%  [<%= bar(@mem) %>]
  </div>

  <h2 class="mb-2">Last 30s</h2>
  <table>
    <tbody>
      <%= for s <- @history do %>
        <tr>
          <td class="pr-4"><%= Calendar.strftime(s.ts, "%H:%M:%S") %></td>
          <td class="pr-2">CPU <%= s.cpu %>%</td>
          <td>MEM <%= s.mem %>%</td>
        </tr>
      <% end %>
    </tbody>
  </table>
</div>
```

---

### Exercise 2: Chat room con Phoenix.Presence

**Problem**: Una aplicación SaaS necesita un chat interno donde los usuarios vean en tiempo real quién está conectado. Al desconectarse, el nombre desaparece de la lista sin polling.

**Hints**:
1. Llama a `Presence.track/4` solo cuando `connected?(socket)` es `true`; en caso contrario Presence intenta registrar un PID que no tiene canal WebSocket activo.
2. `handle_info(%Phoenix.Socket.Broadcast{event: "presence_diff"}, socket)` es el mensaje que PubSub envía cuando alguien entra o sale — úsalo para refrescar la lista.
3. `Presence.list("room:lobby")` devuelve un mapa `%{user_id => %{metas: [...]}}` — transfórmalo con `Map.keys/1` para obtener los nombres.

**One possible solution**:

```elixir
# lib/my_app_web/live/chat_live.ex
defmodule MyAppWeb.ChatLive do
  use MyAppWeb, :live_view
  alias MyAppWeb.Presence

  @topic "room:lobby"

  @impl true
  def mount(_params, session, socket) do
    username = session["username"] || "anon-#{:rand.uniform(999)}"

    if connected?(socket) do
      Phoenix.PubSub.subscribe(MyApp.PubSub, @topic)
      Presence.track(self(), @topic, username, %{joined_at: System.system_time(:second)})
    end

    users = list_users()
    {:ok, assign(socket, username: username, messages: [], users: users, draft: "")}
  end

  @impl true
  def handle_event("send_msg", %{"message" => text}, socket) when text != "" do
    msg = %{user: socket.assigns.username, text: text, at: Time.utc_now()}
    Phoenix.PubSub.broadcast(MyApp.PubSub, @topic, {:new_message, msg})
    {:noreply, assign(socket, draft: "")}
  end
  def handle_event("send_msg", _params, socket), do: {:noreply, socket}

  @impl true
  def handle_info({:new_message, msg}, socket) do
    {:noreply, update(socket, :messages, &(&1 ++ [msg]))}
  end

  def handle_info(%Phoenix.Socket.Broadcast{event: "presence_diff"}, socket) do
    {:noreply, assign(socket, users: list_users())}
  end

  defp list_users do
    Presence.list(@topic) |> Map.keys()
  end
end
```

```heex
<%# lib/my_app_web/live/chat_live.html.heex %>
<div class="flex h-screen">
  <aside class="w-48 bg-gray-100 p-4">
    <h2 class="font-bold mb-2">Online (<%= length(@users) %>)</h2>
    <ul>
      <%= for u <- @users do %>
        <li class={if u == @username, do: "font-bold", else: ""}><%= u %></li>
      <% end %>
    </ul>
  </aside>

  <main class="flex-1 flex flex-col p-4">
    <div class="flex-1 overflow-y-auto space-y-1 mb-4">
      <%= for m <- @messages do %>
        <p><span class="font-semibold"><%= m.user %></span>: <%= m.text %></p>
      <% end %>
    </div>

    <.form for={%{}} phx-submit="send_msg">
      <div class="flex gap-2">
        <input name="message" value={@draft} placeholder="Type…"
               class="flex-1 border rounded px-2 py-1" />
        <.button>Send</.button>
      </div>
    </.form>
  </main>
</div>
```

---

### Exercise 3: Validación de formulario en tiempo real

**Problem**: Un formulario de registro debe mostrar errores de validación a medida que el usuario escribe — sin esperar al submit — y deshabilitar el botón si el changeset es inválido.

**Hints**:
1. `phx-change="validate"` dispara el evento con cada cambio de campo. `phx-submit="save"` solo se dispara al enviar.
2. Asigna el changeset con `action: :validate` para que `form_has_errors?` y `input_validations` de Phoenix.HTML funcionen correctamente.
3. Puedes deshabilitar el botón con `disabled={not @form.source.valid?}` directamente desde el assign del form.

**One possible solution**:

```elixir
# lib/my_app_web/live/registration_live.ex
defmodule MyAppWeb.RegistrationLive do
  use MyAppWeb, :live_view
  alias MyApp.Accounts.User

  @impl true
  def mount(_params, _session, socket) do
    changeset = User.changeset(%User{}, %{})
    {:ok, assign(socket, form: to_form(changeset))}
  end

  @impl true
  def handle_event("validate", %{"user" => params}, socket) do
    changeset =
      %User{}
      |> User.changeset(params)
      |> Map.put(:action, :validate)

    {:noreply, assign(socket, form: to_form(changeset))}
  end

  @impl true
  def handle_event("save", %{"user" => params}, socket) do
    case MyApp.Accounts.create_user(params) do
      {:ok, _user} ->
        {:noreply, push_navigate(socket, to: ~p"/dashboard")}

      {:error, changeset} ->
        {:noreply, assign(socket, form: to_form(changeset))}
    end
  end
end
```

```heex
<%# lib/my_app_web/live/registration_live.html.heex %>
<div class="max-w-md mx-auto mt-16 p-6 border rounded-lg">
  <h1 class="text-2xl font-bold mb-6">Create account</h1>

  <.form for={@form} phx-change="validate" phx-submit="save">
    <.input field={@form[:name]}                 label="Full name"        />
    <.input field={@form[:email]}  type="email"  label="Email"            />
    <.input field={@form[:password]} type="password" label="Password (min 8)" />

    <.button disabled={not @form.source.valid?} class="mt-4 w-full">
      Create account
    </.button>
  </.form>
</div>
```

```elixir
# lib/my_app/accounts/user.ex (Ecto schema relevante)
defmodule MyApp.Accounts.User do
  use Ecto.Schema
  import Ecto.Changeset

  schema "users" do
    field :name,     :string
    field :email,    :string
    field :password, :string, virtual: true
    timestamps()
  end

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:name, :email, :password])
    |> validate_required([:name, :email, :password])
    |> validate_format(:email, ~r/@/)
    |> validate_length(:password, min: 8)
    |> unique_constraint(:email)
  end
end
```

---

## Common Mistakes

**1. No guardar el tick en `handle_info` — el reloj se detiene**

```elixir
# MAL: solo manda el primer tick desde mount, nunca reencola
def handle_info(:tick, socket) do
  {:noreply, assign(socket, data: fetch())}
end

# BIEN: reencola dentro del handler
def handle_info(:tick, socket) do
  Process.send_after(self(), :tick, 1_000)
  {:noreply, assign(socket, data: fetch())}
end
```

**2. Suscribirse sin el guard `connected?/1` — duplica eventos**

```elixir
# MAL: se suscribe también durante el render estático (SSR)
def mount(_p, _s, socket) do
  Phoenix.PubSub.subscribe(MyApp.PubSub, "topic")
  {:ok, socket}
end

# BIEN
def mount(_p, _s, socket) do
  if connected?(socket), do: Phoenix.PubSub.subscribe(MyApp.PubSub, "topic")
  {:ok, socket}
end
```

**3. Olvidar `action: :validate` en el changeset — los errores no aparecen**

```elixir
# MAL: sin :action, Phoenix.HTML no muestra los errores
changeset = User.changeset(%User{}, params)

# BIEN
changeset = %User{} |> User.changeset(params) |> Map.put(:action, :validate)
```

**4. Acumular mensajes sin límite — memory leak en chat**

```elixir
# MAL: la lista crece indefinidamente
update(socket, :messages, &(&1 ++ [msg]))

# BIEN: mantén solo los últimos N
update(socket, :messages, fn msgs -> Enum.take(msgs ++ [msg], -200) end)
```

---

## Verification

```bash
# Crea un proyecto Phoenix con LiveView si no tienes uno
mix phx.new my_app --live
cd my_app
mix ecto.create

# Genera la ruta en router.ex
# live "/metrics", MetricsLive
# live "/chat",    ChatLive
# live "/register", RegistrationLive

mix phx.server
# Abre http://localhost:4000/metrics — los valores deben cambiar cada segundo
# Abre /chat en dos tabs — ambas deben ver la lista de usuarios actualizada
# Abre /register — los errores deben aparecer mientras escribes
```

Para Presence necesitas agregar el módulo en `lib/my_app_web/presence.ex`:

```elixir
defmodule MyAppWeb.Presence do
  use Phoenix.Presence,
    otp_app: :my_app,
    pubsub_server: MyApp.PubSub
end
```

Y registrarlo en `lib/my_app/application.ex` dentro de `children`:

```elixir
MyAppWeb.Presence
```

---

## Summary

| Callback | Cuándo se llama | Para qué |
|---|---|---|
| `mount/3` | Al conectar el socket | Inicializar assigns, suscribir PubSub, arrancar ticks |
| `handle_event/3` | Evento del cliente (click, submit, change) | Mutar estado, persistir datos |
| `handle_info/2` | Mensaje recibido (PubSub, send_after, send) | Actualizar estado por eventos externos |
| `render/1` | Después de cualquier cambio en assigns | Generar el diff HEEx — llamado automáticamente |

El modelo mental clave: **el socket es el estado, el template es una función del estado**. Phoenix calcula el diff y envía solo los bytes que cambiaron.

---

## What's Next

- **Ejercicio 52**: LiveComponent para encapsular piezas de UI con estado propio
- `push_event/3` + JS hooks para integrar librerías JS (Chart.js, etc.)
- `phx-hook` para DOM callbacks en el cliente
- Streams (`stream/3`, `stream_insert/3`) para listas grandes sin re-renderizar todo

---

## Resources

- [Phoenix LiveView Docs](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.html)
- [Phoenix.Presence](https://hexdocs.pm/phoenix/Phoenix.Presence.html)
- [HEEx template syntax](https://hexdocs.pm/phoenix_live_view/assigns-eex.html)
- [LiveView Security](https://hexdocs.pm/phoenix_live_view/security-model.html)
