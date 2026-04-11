# Phoenix LiveComponent: Encapsulated UI State

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` dashboard (exercise 51) has grown. The `DashboardLive` module now handles
pagination logic, sortable tables, search inputs, and modals — all in one 300-line file.
When a user sorts a table, the entire page re-renders even though only the table changed.
You need to extract these pieces into LiveComponents: stateful, isolated UI units that own
their interaction logic.

Project structure for this exercise:

```
api_gateway_umbrella/apps/gateway_api/
├── lib/gateway_api_web/
│   └── components/
│       ├── metrics_table_component.ex     # sortable, paginated table
│       ├── autocomplete_component.ex      # search with debounce
│       └── confirm_modal_component.ex     # reusable confirmation modal
└── test/gateway_api_web/components/
    └── metrics_table_component_test.exs   # given tests
```

---

## When to use LiveComponent vs LiveView vs function component

```
LiveView
  +-- LiveComponent (stateful, own event handling, no route)
        +-- function component (stateless, pure assigns -> HTML)
```

Use a **LiveComponent** when:
- The piece of UI has its own interaction state (pagination, sort order, open/closed)
- The parent should not need to know the implementation details
- Multiple instances of the same UI can exist on one page independently

Use a **function component** when:
- The UI is purely presentational — it takes assigns and renders HTML
- There is no interaction state to manage

The boundary: **the component owns its interaction state; the parent owns the data**.

---

## Key mechanics

**`phx-target={@myself}`**: routes the event to this component instance, not to the
parent LiveView. Without this, all events bubble up to the LiveView.

**`update/2`**: called every time the parent passes new assigns. Use it to transform
parent data into component-local state. The rule: `assign/3` for data the parent controls;
`assign_new/3` for state the component initializes once and then manages itself.

**`send(self(), msg)`**: inside a LiveComponent, `self()` is the parent LiveView's PID —
not the component's PID (components share the LiveView process). This is the idiomatic way
for a component to notify its parent.

**`send_update/3`**: allows the parent to push new assigns into a specific component by ID,
without re-rendering the entire page.

---

## Implementation

### Step 1: `lib/gateway_api_web/components/metrics_table_component.ex`

A sortable, paginated data table that manages its own sort order and page state.
The parent provides `rows` and `columns`; the component handles all interaction.

```elixir
defmodule GatewayApiWeb.MetricsTableComponent do
  use GatewayApiWeb, :live_component

  @per_page 15

  @impl true
  def update(%{rows: rows, columns: columns} = _assigns, socket) do
    socket =
      socket
      |> assign(rows: rows, columns: columns)
      # assign_new: initialize state the component manages; does NOT overwrite on parent re-render
      |> assign_new(:sort_col, fn -> nil end)
      |> assign_new(:sort_dir, fn -> :asc end)
      |> assign_new(:page, fn -> 1 end)
      |> apply_sort_and_paginate()

    {:ok, socket}
  end

  @impl true
  def handle_event("sort", %{"col" => col}, socket) do
    col = String.to_existing_atom(col)
    dir =
      if socket.assigns.sort_col == col and socket.assigns.sort_dir == :asc,
        do: :desc,
        else: :asc

    socket =
      socket
      |> assign(sort_col: col, sort_dir: dir, page: 1)
      |> apply_sort_and_paginate()

    {:noreply, socket}
  end

  def handle_event("page", %{"n" => n}, socket) do
    socket =
      socket
      |> assign(page: String.to_integer(n))
      |> apply_sort_and_paginate()

    {:noreply, socket}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div>
      <table class="w-full border-collapse text-sm">
        <thead>
          <tr class="border-b border-gray-700">
            <%= for col <- @columns do %>
              <th class="text-left py-2 px-3 cursor-pointer hover:bg-gray-800"
                  phx-click="sort"
                  phx-value-col={col.field}
                  phx-target={@myself}>
                <%= col.label %>
                <%= sort_indicator(@sort_col, @sort_dir, col.field) %>
              </th>
            <% end %>
          </tr>
        </thead>
        <tbody>
          <%= for row <- @visible_rows do %>
            <tr class="border-b border-gray-800 hover:bg-gray-900">
              <%= for col <- @columns do %>
                <td class="py-2 px-3"><%= Map.get(row, col.field) %></td>
              <% end %>
            </tr>
          <% end %>
        </tbody>
      </table>

      <%= if @total_pages > 1 do %>
        <div class="flex gap-2 mt-3 justify-end text-sm">
          <%= for n <- 1..@total_pages do %>
            <button phx-click="page"
                    phx-value-n={n}
                    phx-target={@myself}
                    class={if n == @page, do: "font-bold underline text-blue-400", else: "text-gray-400 hover:text-white"}>
              <%= n %>
            </button>
          <% end %>
        </div>
      <% end %>
    </div>
    """
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  defp apply_sort_and_paginate(socket) do
    %{rows: rows, sort_col: col, sort_dir: dir, page: page} = socket.assigns

    sorted =
      if col do
        Enum.sort_by(rows, &Map.get(&1, col), dir)
      else
        rows
      end

    total_pages = max(1, ceil(length(sorted) / @per_page))
    # Clamp page to valid range if rows were removed
    page = min(page, total_pages)
    visible = sorted |> Enum.drop((page - 1) * @per_page) |> Enum.take(@per_page)

    assign(socket, visible_rows: visible, total_pages: total_pages, page: page)
  end

  defp sort_indicator(col, dir, field) when col == field,
    do: if(dir == :asc, do: " ↑", else: " ↓")
  defp sort_indicator(_, _, _), do: ""
end
```

Usage in `DashboardLive`:

```heex
<.live_component
  module={GatewayApiWeb.MetricsTableComponent}
  id="request-log"
  rows={@recent_requests}
  columns={[
    %{field: :client_id,    label: "Client"},
    %{field: :path,         label: "Path"},
    %{field: :status,       label: "Status"},
    %{field: :duration_ms,  label: "Duration (ms)"},
    %{field: :ts,           label: "Time"}
  ]}
/>
```

### Step 2: `lib/gateway_api_web/components/autocomplete_component.ex`

A search input with debounce that calls a parent-provided `fetch_fn` to load suggestions.
Selecting an item notifies the parent LiveView via `send(self(), ...)`.

```elixir
defmodule GatewayApiWeb.AutocompleteComponent do
  use GatewayApiWeb, :live_component

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
    # Notify the parent LiveView. self() here is the parent's PID — this is intentional.
    send(self(), {:autocomplete_selected, socket.assigns.id, value})
    {:noreply, assign(socket, query: value, suggestions: [], open: false)}
  end

  def handle_event("clear", _params, socket) do
    {:noreply, assign(socket, query: "", suggestions: [], open: false)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div class="relative">
      <div class="flex items-center gap-1">
        <input
          name="query"
          value={@query}
          placeholder={assigns[:placeholder] || "Search..."}
          phx-change="search"
          phx-debounce="300"
          phx-target={@myself}
          autocomplete="off"
          class="border rounded px-3 py-1.5 w-full text-sm"
        />
        <%= if @query != "" do %>
          <button phx-click="clear" phx-target={@myself}
                  class="text-gray-400 hover:text-gray-700 px-1">x</button>
        <% end %>
      </div>

      <%= if @open and @suggestions != [] do %>
        <ul class="absolute z-10 w-full bg-white border rounded shadow-lg mt-1
                   max-h-48 overflow-y-auto text-sm">
          <%= for s <- @suggestions do %>
            <li phx-click="select"
                phx-value-value={s}
                phx-target={@myself}
                class="px-3 py-1.5 hover:bg-blue-50 cursor-pointer">
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

Parent LiveView receiving the notification:

```elixir
@impl true
def handle_info({:autocomplete_selected, "client-search", value}, socket) do
  {:noreply, assign(socket, selected_client: value)}
end
```

### Step 3: `lib/gateway_api_web/components/confirm_modal_component.ex`

A reusable confirmation modal. The parent opens it via `send_update/3` and
receives `:modal_confirmed` or `:modal_closed` messages.

```elixir
defmodule GatewayApiWeb.ConfirmModalComponent do
  use GatewayApiWeb, :live_component

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
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
           phx-window-keydown="close"
           phx-key="Escape"
           phx-target={@myself}>
        <div class="bg-white rounded-lg shadow-xl p-6 max-w-md w-full mx-4">
          <div class="flex justify-between items-start mb-4">
            <h2 class="text-lg font-semibold"><%= assigns[:title] || "Confirm" %></h2>
            <button phx-click="close" phx-target={@myself}
                    class="text-gray-400 hover:text-gray-600 text-xl leading-none">x</button>
          </div>

          <div class="mb-6 text-gray-700">
            <%= render_slot(@inner_block) %>
          </div>

          <div class="flex justify-end gap-3">
            <button phx-click="close" phx-target={@myself}
                    class="px-4 py-2 border rounded text-gray-700 hover:bg-gray-50">
              Cancel
            </button>
            <button phx-click="confirm" phx-target={@myself}
                    class="px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700">
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

Parent LiveView using the modal:

```elixir
def handle_event("open_reset_modal", %{"host" => host}, socket) do
  send_update(GatewayApiWeb.ConfirmModalComponent, id: "reset-circuit-modal", open: true)
  {:noreply, assign(socket, pending_reset_host: host)}
end

def handle_info({:modal_confirmed, "reset-circuit-modal"}, socket) do
  :ets.delete(:circuit_breaker, socket.assigns.pending_reset_host)
  {:noreply, assign(socket, pending_reset_host: nil)}
end

def handle_info({:modal_closed, _}, socket) do
  {:noreply, assign(socket, pending_reset_host: nil)}
end
```

```heex
<.live_component module={GatewayApiWeb.ConfirmModalComponent}
                 id="reset-circuit-modal"
                 title="Reset circuit breaker">
  <p>This will immediately allow traffic to the service again.
     Make sure the service is healthy before resetting.</p>
</.live_component>
```

### Step 4: Given tests — must pass without modification

```elixir
# test/gateway_api_web/components/metrics_table_component_test.exs
defmodule GatewayApiWeb.MetricsTableComponentTest do
  use GatewayApiWeb.ConnCase
  import Phoenix.LiveViewTest

  alias GatewayApiWeb.MetricsTableComponent

  defmodule TestLive do
    use GatewayApiWeb, :live_view

    @impl true
    def mount(_params, _session, socket) do
      rows = for i <- 1..20 do
        %{client_id: "client-#{i}", path: "/api/test", status: 200, duration_ms: i * 10}
      end
      {:ok, assign(socket, rows: rows)}
    end

    @impl true
    def render(assigns) do
      ~H"""
      <.live_component
        module={GatewayApiWeb.MetricsTableComponent}
        id="test-table"
        rows={@rows}
        columns={[
          %{field: :client_id, label: "Client"},
          %{field: :duration_ms, label: "Duration"}
        ]}
      />
      """
    end
  end

  test "renders table with first page" do
    {:ok, view, html} = live_isolated(build_conn(), TestLive)
    assert html =~ "client-1"
    refute html =~ "client-16"  # page 2
  end

  test "click column header sorts ascending then descending" do
    {:ok, view, _html} = live_isolated(build_conn(), TestLive)

    html = view |> element("th[phx-value-col=duration_ms]") |> render_click()
    assert html =~ "↑"

    html = view |> element("th[phx-value-col=duration_ms]") |> render_click()
    assert html =~ "↓"
  end

  test "pagination shows page 2" do
    {:ok, view, _html} = live_isolated(build_conn(), TestLive)

    html = view |> element("button[phx-value-n='2']") |> render_click()
    assert html =~ "client-16"
  end
end
```

### Step 5: Run the tests

```bash
mix test test/gateway_api_web/components/ --trace
```

---

## Trade-off analysis

| Aspect | LiveComponent | LiveView (separate route) | Function component |
|--------|--------------|--------------------------|-------------------|
| Own state | yes | yes | no |
| Own route | no | yes | no |
| Process isolation | no (shares parent process) | yes (own process) | no |
| Event routing | phx-target={@myself} | automatic | no events |
| Inter-component comm | send/send_update | PubSub | n/a |
| Re-render scope | component subtree | full LiveView | parent decides |

Reflection: the `AutocompleteComponent` calls `socket.assigns.fetch_fn.(query)` on every
keystroke (after debounce). This function runs synchronously in the LiveView process. For
a DB-backed search, this blocks the process for the query duration. How would you make
this non-blocking? (Hint: `Task.async` + `handle_info`)

---

## Common production mistakes

**1. Omitting `phx-target={@myself}` on interactive elements**
Without `phx-target`, events bubble to the parent LiveView. The LiveView has no
`handle_event("sort", ...)` — the event is silently ignored or crashes. Always verify
which process should handle each event.

**2. Using `assign_new/3` for data the parent controls**
`assign_new/3` only assigns if the key is absent. If the parent passes new `rows` and
the component uses `assign_new(:rows, fn -> rows end)`, the component never sees the
updated rows. Use `assign(socket, assigns)` for parent-controlled data.

**3. IDs that are not unique per instance**
If two `MetricsTableComponent` instances both have `id="table"`, Phoenix maps their events
to the same state. Always use IDs that are unique per page: `id={"table-#{@context}"}`.

**4. `self()` confusion**
Inside a LiveComponent, `self()` is the parent LiveView's PID. This means
`send(self(), msg)` correctly reaches `handle_info` in the parent. But it also means you
cannot send a message to "just the component" — components share their parent's process.

**5. Expensive computation in `render/1`**
`render/1` is called after every assign change. Sorting a list of 10,000 rows in `render/1`
runs on every diff, even when nothing changed that requires a sort. Move computation to
`update/2` or the event handlers, store the result as an assign, and read it in `render/1`.

---

## Resources

- [Phoenix LiveComponent docs](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveComponent.html) — lifecycle and communication patterns
- [`send_update/3`](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.html#send_update/3) — parent-to-component communication
- [LiveView JS commands](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.JS.html) — client-side transitions without round-trips
- [Phoenix.LiveViewTest — `live_isolated/3`](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveViewTest.html#live_isolated/3) — testing components in isolation
