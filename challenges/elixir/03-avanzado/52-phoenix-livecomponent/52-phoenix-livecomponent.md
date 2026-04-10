# 52. Phoenix LiveComponent — UI con Estado Encapsulado

**Difficulty**: Avanzado

---

## Prerequisites

- Ejercicio 51 (LiveView lifecycle: mount, handle_event, handle_info)
- HEEx templates y `phx-*` bindings
- Ecto changesets
- Phoenix.PubSub (opcional para el ejercicio de modal)

---

## Learning Objectives

1. Diferenciar cuándo usar LiveComponent vs LiveView vs componente funcional
2. Encapsular estado y lógica dentro de un LiveComponent con `update/2`
3. Dirigir eventos al componente correcto con `phx-target={@myself}`
4. Comunicar desde un hijo al padre sin acoplarlos directamente
5. Usar `send_update/3` para actualizar un componente desde el LiveView padre

---

## Concepts

### LiveComponent vs LiveView vs componente funcional

Un **componente funcional** (`def card(assigns)`) no tiene estado ni lifecycle: solo recibe assigns y renderiza. Un **LiveView** tiene su propio socket y WebSocket. Un **LiveComponent** es el punto medio: vive dentro de un LiveView, tiene estado propio, recibe eventos y puede comunicar hacia arriba.

```
LiveView (socket propio, ruta propia)
  └── LiveComponent (estado encapsulado, sin ruta)
        └── componente funcional (sin estado, puro)
```

Usa LiveComponent cuando:
- La pieza de UI tiene su propia lógica de interacción (paginación, autocomplete)
- Quieres que el padre no sepa los detalles de implementación
- El estado del componente no afecta al resto de la página

### phx-target y @myself

Sin `phx-target`, todos los eventos suben al LiveView padre. Con `phx-target={@myself}`, el evento va directamente al LiveComponent que lo generó. `@myself` es un assign inyectado automáticamente por Phoenix — el ID interno del componente.

```elixir
defmodule MyAppWeb.CounterComponent do
  use MyAppWeb, :live_component

  @impl true
  def update(%{initial: n}, socket) do
    {:ok, assign(socket, count: n)}
  end

  @impl true
  def handle_event("inc", _params, socket) do
    {:noreply, update(socket, :count, &(&1 + 1))}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div>
      <span><%= @count %></span>
      <button phx-click="inc" phx-target={@myself}>+</button>
    </div>
    """
  end
end
```

```heex
<%# En el LiveView padre %>
<.live_component module={MyAppWeb.CounterComponent} id="c1" initial={0} />
<.live_component module={MyAppWeb.CounterComponent} id="c2" initial={10} />
```

Sin `phx-target`, ambos botones enviarían "inc" al LiveView padre. Con `@myself`, cada componente maneja su propio evento.

### update/2 y send_update/3

`update/2` se llama cada vez que el padre pasa nuevos assigns al componente. Es el punto de entrada para transformar datos externos en estado interno. `send_update/3` permite que el padre (u otro proceso) actualice un componente sin re-renderizar toda la página.

```elixir
# update/2 en el LiveComponent
@impl true
def update(%{items: items} = assigns, socket) do
  sorted = Enum.sort_by(items, & &1.name)
  {:ok, assign(socket, assigns) |> assign(sorted_items: sorted)}
end

# send_update/3 desde el padre — útil tras un broadcast de PubSub
def handle_info({:data_updated, items}, socket) do
  send_update(MyAppWeb.TableComponent, id: "main-table", items: items)
  {:noreply, socket}
end
```

### Comunicar del hijo al padre

Un LiveComponent no puede llamar directamente a funciones del padre. El patrón idiomático es `send/2` al `socket.parent_pid`:

```elixir
# Dentro del LiveComponent
def handle_event("select", %{"value" => val}, socket) do
  send(self(), {:selected, val})        # se envía al LiveView padre
  {:noreply, socket}
end

# En el LiveView padre
def handle_info({:selected, val}, socket) do
  {:noreply, assign(socket, selection: val)}
end
```

> `self()` dentro de un LiveComponent es el PID del LiveView padre, no del componente.

---

## Exercises

### Exercise 1: DataTable con paginación y ordenamiento

**Problem**: Una aplicación de backoffice necesita mostrar tablas de datos con paginación y ordenamiento por columna. El estado de qué columna está ordenada y en qué página se encuentra debe vivir dentro del componente — el padre solo pasa la lista cruda de filas.

**Hints**:
1. Guarda `{:sort_col, :sort_dir, :page}` en el estado del componente via `update/2` cuando los assigns del padre cambian.
2. Los eventos `"sort"` y `"page"` deben ir a `phx-target={@myself}` para no contaminar el LiveView padre.
3. Calcula las filas visibles en una función privada `paginate(rows, page, per_page)` llamada desde `update/2` y desde los handlers — así el render siempre recibe `@visible_rows` ya listo.

**One possible solution**:

```elixir
# lib/my_app_web/components/data_table_component.ex
defmodule MyAppWeb.DataTableComponent do
  use MyAppWeb, :live_component

  @per_page 10

  @impl true
  def update(%{rows: rows, columns: columns} = _assigns, socket) do
    socket =
      socket
      |> assign(rows: rows, columns: columns)
      |> assign_new(:sort_col, fn -> nil end)
      |> assign_new(:sort_dir, fn -> :asc end)
      |> assign_new(:page, fn -> 1 end)
      |> apply_sort_and_page()

    {:ok, socket}
  end

  @impl true
  def handle_event("sort", %{"col" => col}, socket) do
    col = String.to_existing_atom(col)
    dir = if socket.assigns.sort_col == col and socket.assigns.sort_dir == :asc, do: :desc, else: :asc
    socket = socket |> assign(sort_col: col, sort_dir: dir, page: 1) |> apply_sort_and_page()
    {:noreply, socket}
  end

  def handle_event("page", %{"n" => n}, socket) do
    socket = socket |> assign(page: String.to_integer(n)) |> apply_sort_and_page()
    {:noreply, socket}
  end

  defp apply_sort_and_page(%{assigns: %{rows: rows, sort_col: col, sort_dir: dir, page: page}} = socket) do
    sorted =
      if col do
        Enum.sort_by(rows, &Map.get(&1, col), if(dir == :asc, do: :asc, else: :desc))
      else
        rows
      end

    total_pages = max(1, ceil(length(sorted) / @per_page))
    visible = sorted |> Enum.drop((page - 1) * @per_page) |> Enum.take(@per_page)
    assign(socket, visible_rows: visible, total_pages: total_pages)
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div>
      <table class="w-full border-collapse">
        <thead>
          <tr>
            <%= for col <- @columns do %>
              <th class="border p-2 cursor-pointer hover:bg-gray-100"
                  phx-click="sort"
                  phx-value-col={col.field}
                  phx-target={@myself}>
                <%= col.label %>
                <%= if @sort_col == col.field, do: if(@sort_dir == :asc, do: "↑", else: "↓") %>
              </th>
            <% end %>
          </tr>
        </thead>
        <tbody>
          <%= for row <- @visible_rows do %>
            <tr class="border-b hover:bg-gray-50">
              <%= for col <- @columns do %>
                <td class="p-2"><%= Map.get(row, col.field) %></td>
              <% end %>
            </tr>
          <% end %>
        </tbody>
      </table>

      <div class="flex gap-2 mt-4 justify-end">
        <%= for n <- 1..@total_pages do %>
          <button phx-click="page" phx-value-n={n} phx-target={@myself}
                  class={if n == @page, do: "font-bold underline", else: ""}>
            <%= n %>
          </button>
        <% end %>
      </div>
    </div>
    """
  end
end
```

```heex
<%# Uso en el LiveView padre %>
<.live_component
  module={MyAppWeb.DataTableComponent}
  id="users-table"
  rows={@users}
  columns={[
    %{field: :name,  label: "Name"},
    %{field: :email, label: "Email"},
    %{field: :role,  label: "Role"}
  ]}
/>
```

---

### Exercise 2: Autocomplete con debounce

**Problem**: Un formulario de búsqueda necesita un input que muestre sugerencias mientras el usuario escribe, sin disparar una query por cada tecla. Al seleccionar una sugerencia, el LiveView padre debe ser notificado para actualizar su propio estado (p.ej., filtrar resultados).

**Hints**:
1. Usa `phx-debounce="300"` en el input para que el evento `"search"` solo se dispare 300ms después de que el usuario deja de escribir — sin código JavaScript.
2. El componente mantiene `suggestions` en su estado; el padre nunca las ve.
3. Para notificar al padre, usa `send(self(), {:autocomplete_selected, value})` desde el LiveComponent — `self()` apunta al PID del LiveView padre.

**One possible solution**:

```elixir
# lib/my_app_web/components/autocomplete_component.ex
defmodule MyAppWeb.AutocompleteComponent do
  use MyAppWeb, :live_component

  @impl true
  def update(%{fetch_fn: _} = assigns, socket) do
    {:ok, assign(socket, assigns) |> assign(query: "", suggestions: [], open: false)}
  end

  @impl true
  def handle_event("search", %{"query" => q}, socket) when byte_size(q) >= 2 do
    suggestions = socket.assigns.fetch_fn.(q)
    {:noreply, assign(socket, query: q, suggestions: suggestions, open: true)}
  end
  def handle_event("search", %{"query" => q}, socket) do
    {:noreply, assign(socket, query: q, suggestions: [], open: false)}
  end

  def handle_event("select", %{"value" => value}, socket) do
    # Notifica al padre
    send(self(), {:autocomplete_selected, value})
    {:noreply, assign(socket, query: value, suggestions: [], open: false)}
  end

  def handle_event("clear", _params, socket) do
    {:noreply, assign(socket, query: "", suggestions: [], open: false)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div class="relative">
      <div class="flex">
        <input
          name="query"
          value={@query}
          placeholder={assigns[:placeholder] || "Search…"}
          phx-change="search"
          phx-debounce="300"
          phx-target={@myself}
          class="border rounded px-3 py-1 w-full"
          autocomplete="off"
        />
        <%= if @query != "" do %>
          <button phx-click="clear" phx-target={@myself} class="ml-1 text-gray-400">✕</button>
        <% end %>
      </div>

      <%= if @open and @suggestions != [] do %>
        <ul class="absolute z-10 w-full bg-white border rounded shadow-lg mt-1 max-h-48 overflow-y-auto">
          <%= for s <- @suggestions do %>
            <li phx-click="select"
                phx-value-value={s}
                phx-target={@myself}
                class="px-3 py-1 hover:bg-blue-50 cursor-pointer">
              <%= s %>
            </li>
          <% end %>
        </ul>
      <% end %>
    </div>
    """
  end
end
```

```elixir
# En el LiveView padre
defmodule MyAppWeb.SearchLive do
  use MyAppWeb, :live_view

  @impl true
  def mount(_params, _session, socket) do
    {:ok, assign(socket, results: [], selected: nil)}
  end

  # Callback del componente hijo via send/2
  @impl true
  def handle_info({:autocomplete_selected, value}, socket) do
    results = MyApp.Search.run(value)
    {:noreply, assign(socket, selected: value, results: results)}
  end
end
```

```heex
<%# lib/my_app_web/live/search_live.html.heex %>
<div class="max-w-lg mx-auto mt-10 space-y-6">
  <.live_component
    module={MyAppWeb.AutocompleteComponent}
    id="country-search"
    placeholder="Type a country…"
    fetch_fn={&MyApp.Countries.search/1}
  />

  <%= if @selected do %>
    <p>Selected: <strong><%= @selected %></strong></p>
    <ul>
      <%= for r <- @results do %>
        <li><%= r %></li>
      <% end %>
    </ul>
  <% end %>
</div>
```

---

### Exercise 3: Modal reutilizable con slot

**Problem**: Varios flujos de la aplicación (confirmar borrado, editar perfil, ver detalles) necesitan un modal. El modal debe ser un componente reutilizable que: muestre cualquier contenido via slot, se abra/cierre sin recargar la página, y notifique al padre cuando se confirma o cancela.

**Hints**:
1. Un modal de este tipo puede ser un **componente funcional** (sin estado) si el padre controla el flag `show`. El padre alterna `@show_modal` con `handle_event("open_modal"/"close_modal")`.
2. Para la versión con estado propio, usa LiveComponent y guarda `open: false` internamente — el padre llama a `send_update/3` para abrirlo.
3. La tecla Escape para cerrar se implementa con un JS hook (`phx-key="Escape" phx-window-keydown="close"`), sin necesidad de JavaScript manual.

**One possible solution (LiveComponent con estado)**:

```elixir
# lib/my_app_web/components/modal_component.ex
defmodule MyAppWeb.ModalComponent do
  use MyAppWeb, :live_component

  @impl true
  def update(assigns, socket) do
    {:ok, assign(socket, assigns) |> assign_new(:open, fn -> false end)}
  end

  @impl true
  def handle_event("close", _params, socket) do
    send(self(), {:modal_closed, socket.assigns.id})
    {:noreply, assign(socket, open: false)}
  end

  def handle_event("confirm", _params, socket) do
    send(self(), {:modal_confirmed, socket.assigns.id})
    {:noreply, assign(socket, open: false)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <%= if @open do %>
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
           phx-window-keydown="close"
           phx-key="Escape"
           phx-target={@myself}>
        <div class="bg-white rounded-lg shadow-xl p-6 max-w-md w-full mx-4">
          <div class="flex justify-between items-start mb-4">
            <h2 class="text-lg font-semibold"><%= assigns[:title] || "Dialog" %></h2>
            <button phx-click="close" phx-target={@myself} class="text-gray-400 hover:text-gray-600">✕</button>
          </div>

          <div class="mb-6">
            <%= render_slot(@inner_block) %>
          </div>

          <div class="flex justify-end gap-3">
            <button phx-click="close"   phx-target={@myself} class="px-4 py-2 border rounded">
              Cancel
            </button>
            <button phx-click="confirm" phx-target={@myself} class="px-4 py-2 bg-blue-600 text-white rounded">
              Confirm
            </button>
          </div>
        </div>
      </div>
    <% end %>
    """
  end
end
```

```elixir
# En el LiveView padre
defmodule MyAppWeb.UsersLive do
  use MyAppWeb, :live_view

  @impl true
  def mount(_params, _session, socket) do
    {:ok, assign(socket, users: list_users(), deleting: nil)}
  end

  def handle_event("open_delete", %{"id" => id}, socket) do
    send_update(MyAppWeb.ModalComponent, id: "delete-modal", open: true)
    {:noreply, assign(socket, deleting: id)}
  end

  @impl true
  def handle_info({:modal_confirmed, "delete-modal"}, socket) do
    MyApp.Accounts.delete_user(socket.assigns.deleting)
    {:noreply, assign(socket, users: list_users(), deleting: nil)}
  end

  def handle_info({:modal_closed, _id}, socket) do
    {:noreply, assign(socket, deleting: nil)}
  end

  defp list_users, do: MyApp.Accounts.list_users()
end
```

```heex
<%# lib/my_app_web/live/users_live.html.heex %>
<div>
  <h1 class="text-2xl font-bold mb-4">Users</h1>

  <ul class="space-y-2">
    <%= for user <- @users do %>
      <li class="flex justify-between items-center border-b py-2">
        <span><%= user.name %> – <%= user.email %></span>
        <button phx-click="open_delete" phx-value-id={user.id}
                class="text-red-500 hover:text-red-700 text-sm">
          Delete
        </button>
      </li>
    <% end %>
  </ul>

  <.live_component module={MyAppWeb.ModalComponent} id="delete-modal" title="Confirm deletion">
    <p>This action cannot be undone. Are you sure?</p>
  </.live_component>
</div>
```

---

## Common Mistakes

**1. Olvidar `phx-target` — todos los eventos van al padre**

```heex
<%# MAL: "sort" sube al LiveView, que no tiene ese handle_event %>
<th phx-click="sort" phx-value-col="name">Name</th>

<%# BIEN: el componente maneja su propio evento %>
<th phx-click="sort" phx-value-col="name" phx-target={@myself}>Name</th>
```

**2. Usar `assign_new/3` en lugar de `assign/3` para estado reactivo a cambios del padre**

```elixir
# MAL: assign_new solo asigna la primera vez, ignora cambios posteriores del padre
def update(%{items: items}, socket) do
  {:ok, assign_new(socket, :items, fn -> items end)}
end

# BIEN: assign/3 actualiza siempre; assign_new solo para estado interno inicial
def update(%{items: items} = assigns, socket) do
  {:ok, socket |> assign(assigns) |> assign_new(:sort_col, fn -> nil end)}
end
```

**3. `self()` en un LiveComponent apunta al LiveView padre, no al componente**

```elixir
# Esto es correcto — envia al LiveView padre
def handle_event("select", %{"v" => v}, socket) do
  send(self(), {:selected, v})
  {:noreply, socket}
end

# No existe un "pid del componente" — los LiveComponents comparten el proceso del LiveView
```

**4. No usar `id` único por instancia — Phoenix reutiliza el estado equivocado**

```heex
<%# MAL: mismo id para dos instancias distintas %>
<.live_component module={MyAppWeb.CounterComponent} id="counter" initial={0} />
<.live_component module={MyAppWeb.CounterComponent} id="counter" initial={10} />

<%# BIEN: id único por instancia %>
<.live_component module={MyAppWeb.CounterComponent} id="counter-a" initial={0} />
<.live_component module={MyAppWeb.CounterComponent} id="counter-b" initial={10} />
```

---

## Verification

```bash
# Añade las rutas en router.ex:
# live "/search",  SearchLive
# live "/users",   UsersLive

mix phx.server

# DataTable: abre /admin/users, haz click en cabeceras — debe ordenar
# Autocomplete: escribe en el input — las sugerencias deben aparecer ≥300ms después de parar
# Modal: click "Delete" → modal abre; Escape o Cancel → cierra; Confirm → elimina y cierra
```

Para el autocomplete, implementa una función de búsqueda simple para pruebas:

```elixir
# En MyApp.Countries (para test)
def search(query) do
  ~w(Argentina Australia Austria Belgium Brazil Canada Chile China Denmark Egypt)
  |> Enum.filter(&String.contains?(String.downcase(&1), String.downcase(query)))
end
```

---

## Summary

| Concepto | Cuándo usarlo |
|---|---|
| Componente funcional (`def c(assigns)`) | UI sin estado, solo visual |
| LiveComponent | UI con estado encapsulado, interacción propia |
| LiveView | Página completa, ruta propia |
| `phx-target={@myself}` | El componente maneja su propio evento |
| `send(self(), msg)` | Componente notifica al padre |
| `send_update/3` | Padre actualiza un componente sin re-render completo |
| `assign_new/3` | Inicializar estado interno que el padre no controla |

La regla de oro: **el componente es dueño de su estado interno; el padre es dueño de los datos que pasa**. Cualquier cruce de esa línea es un smell de diseño.

---

## What's Next

- `Phoenix.LiveView.JS` para transiciones CSS sin round-trip al servidor
- Streams (`stream/3`) para listas de miles de filas con DOM patching eficiente
- Uploading de archivos con `allow_upload/3` y `consume_uploaded_entries/3`
- Testing de LiveView con `Phoenix.LiveViewTest` — `live/2`, `element/2`, `render_click/2`

---

## Resources

- [Phoenix LiveComponent Docs](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveComponent.html)
- [LiveView JS Commands](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.JS.html)
- [LiveView Testing Guide](https://hexdocs.pm/phoenix_live_view/testing.html)
- [Slots in HEEx](https://hexdocs.pm/phoenix_live_view/Phoenix.Component.html#module-slots)
