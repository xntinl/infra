# Phoenix LiveView: Real-Time Gateway Dashboard

## Overview

Build a real-time web dashboard for an API gateway using Phoenix LiveView. The dashboard
displays request rates, circuit breaker states, and rate-limiter queue depths -- all
server-rendered HTML with WebSocket-pushed diffs. No JavaScript SPA, no REST polling loop.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway_web/
│       ├── live/
│       │   ├── dashboard_live.ex
│       │   ├── dashboard_live.html.heex
│       │   ├── circuit_breaker_live.ex
│       │   └── config_live.ex
│       └── router.ex
└── test/
    └── api_gateway_web/live/
        └── dashboard_live_test.exs
```

---

## Why LiveView over a JavaScript SPA

A React dashboard requires: a REST or GraphQL API, JSON serialization, a client-side state
manager, and a polling or WebSocket layer. LiveView collapses all of this:

```
Browser                    Server
-------                    ------
HTML render <----------- mount/3 assigns initial state
                |
User click ---- handle_event/3 --> update state
                |
                +------- send diff HTML --> browser patches DOM
```

The server computes diffs; the browser applies them. No JSON. No client state sync. The
tradeoff: all users' state lives in server memory (one LiveView process per connection).

---

## LiveView lifecycle

```
mount/3         -- called once when the LiveView mounts
                  initialize assigns; start PubSub subscriptions; schedule ticks
render/1        -- called after every assign change; returns HEEx template
handle_event/3  -- called when user interacts (phx-click, phx-submit, phx-change)
handle_info/2   -- called when a message arrives (PubSub, Process.send_after, GenServer)
```

The key insight: **the socket is the state, the template is a pure function of the state**.

---

## Implementation

### Step 1: `lib/api_gateway_web/live/dashboard_live.ex`

The dashboard LiveView subscribes to PubSub for real-time metrics and uses a timer tick
to periodically refresh ETS-backed data.

```elixir
defmodule ApiGatewayWeb.DashboardLive do
  use ApiGatewayWeb, :live_view

  @tick_ms 1_000
  @history_limit 60

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket) do
      Phoenix.PubSub.subscribe(ApiGateway.PubSub, "gateway:metrics")
      Process.send_after(self(), :tick, @tick_ms)
    end

    metrics = fetch_current_metrics()

    socket = assign(socket,
      metrics: metrics,
      history: [metrics],
      circuit_breakers: fetch_circuit_breaker_states(),
      rate_limiter_size: fetch_rate_limiter_size()
    )

    {:ok, socket}
  end

  @impl true
  def handle_info(:tick, socket) do
    Process.send_after(self(), :tick, @tick_ms)

    metrics = fetch_current_metrics()

    socket =
      socket
      |> assign(:metrics, metrics)
      |> update(:history, fn h -> Enum.take([metrics | h], @history_limit) end)
      |> assign(:circuit_breakers, fetch_circuit_breaker_states())
      |> assign(:rate_limiter_size, fetch_rate_limiter_size())

    {:noreply, socket}
  end

  @impl true
  def handle_info({:metrics_update, metrics}, socket) do
    socket = assign(socket, :metrics, metrics)
    {:noreply, socket}
  end

  defp fetch_current_metrics do
    %{request_rate: 0.0, error_rate: 0.0, p99_ms: 0.0, ts: DateTime.utc_now()}
  end

  defp fetch_circuit_breaker_states do
    case :ets.whereis(:circuit_breaker) do
      :undefined ->
        []

      _ref ->
        :ets.tab2list(:circuit_breaker)
        |> Enum.filter(fn entry -> is_binary(elem(entry, 0)) end)
        |> Enum.map(fn {host, state, meta} ->
          %{host: host, state: state, opened_at: if(state == :open, do: meta, else: nil)}
        end)
    end
  end

  defp fetch_rate_limiter_size do
    case :ets.whereis(:rate_limiter_windows) do
      :undefined -> 0
      _ref -> :ets.info(:rate_limiter_windows, :size)
    end
  end
end
```

```heex
<%# lib/api_gateway_web/live/dashboard_live.html.heex %>
<div class="font-mono p-6 bg-gray-950 text-green-400 min-h-screen">
  <h1 class="text-2xl mb-6">api_gateway dashboard</h1>

  <%# Metrics row %>
  <div class="grid grid-cols-3 gap-4 mb-8">
    <div class="border border-green-800 p-4 rounded">
      <div class="text-sm text-green-600">Request rate</div>
      <div class="text-3xl"><%= Float.round(@metrics.request_rate, 1) %> req/s</div>
    </div>
    <div class="border border-green-800 p-4 rounded">
      <div class="text-sm text-green-600">Error rate</div>
      <div class="text-3xl"><%= Float.round(@metrics.error_rate * 100, 2) %>%</div>
    </div>
    <div class="border border-green-800 p-4 rounded">
      <div class="text-sm text-green-600">p99 latency</div>
      <div class="text-3xl"><%= Float.round(@metrics.p99_ms, 1) %> ms</div>
    </div>
  </div>

  <%# Circuit breaker states %>
  <h2 class="text-lg mb-3">Circuit Breakers</h2>
  <table class="w-full mb-8 border-collapse">
    <thead>
      <tr class="text-green-600 text-left">
        <th class="pb-2">Host</th>
        <th class="pb-2">State</th>
        <th class="pb-2">Since</th>
      </tr>
    </thead>
    <tbody>
      <%= for cb <- @circuit_breakers do %>
        <tr class={if cb.state == :open, do: "text-red-400", else: ""}>
          <td class="py-1"><%= cb.host %></td>
          <td class="py-1"><%= cb.state %></td>
          <td class="py-1">
            <%= if cb.state == :open, do: "#{System.monotonic_time(:millisecond) - cb.opened_at}ms ago" %>
          </td>
        </tr>
      <% end %>
    </tbody>
  </table>

  <%# History table -- last 60 samples %>
  <h2 class="text-lg mb-3">Last 60s</h2>
  <div class="overflow-y-auto max-h-64">
    <table class="w-full">
      <tbody>
        <%= for s <- @history do %>
          <tr class="text-sm border-b border-green-900">
            <td class="pr-4 text-green-600"><%= Calendar.strftime(s.ts, "%H:%M:%S") %></td>
            <td class="pr-4"><%= Float.round(s.request_rate, 1) %> req/s</td>
            <td class="pr-4"><%= Float.round(s.error_rate * 100, 2) %>% errors</td>
            <td><%= Float.round(s.p99_ms, 1) %>ms p99</td>
          </tr>
        <% end %>
      </tbody>
    </table>
  </div>
</div>
```

### Step 2: `lib/api_gateway_web/live/circuit_breaker_live.ex`

Allows ops to view circuit breaker states and reset them manually via a button.

```elixir
defmodule ApiGatewayWeb.CircuitBreakerLive do
  use ApiGatewayWeb, :live_view

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket), do: Phoenix.PubSub.subscribe(ApiGateway.PubSub, "circuit_breaker:events")
    {:ok, assign(socket, events: [], states: load_states())}
  end

  @impl true
  def handle_event("reset_circuit", %{"host" => host}, socket) do
    :ets.delete(:circuit_breaker, host)
    {:noreply, assign(socket, states: load_states())}
  end

  @impl true
  def handle_info({:circuit_state_change, event}, socket) do
    events = Enum.take([event | socket.assigns.events], 50)
    {:noreply, assign(socket, events: events, states: load_states())}
  end

  defp load_states do
    case :ets.whereis(:circuit_breaker) do
      :undefined ->
        []

      _ref ->
        :ets.tab2list(:circuit_breaker)
        |> Enum.filter(fn entry -> is_binary(elem(entry, 0)) end)
        |> Enum.map(fn {host, state, meta} ->
          %{host: host, state: state, opened_at: if(state == :open, do: meta, else: nil)}
        end)
    end
  end
end
```

### Step 3: `lib/api_gateway_web/live/config_live.ex`

A form-based LiveView for updating gateway configuration with inline validation.

```elixir
defmodule ApiGatewayWeb.ConfigLive do
  use ApiGatewayWeb, :live_view

  alias ApiGateway.GatewayConfig

  @impl true
  def mount(_params, _session, socket) do
    changeset = GatewayConfig.changeset(%GatewayConfig{}, %{})
    {:ok, assign(socket, form: to_form(changeset), saved: false)}
  end

  @impl true
  def handle_event("validate", %{"gateway_config" => params}, socket) do
    changeset =
      %GatewayConfig{}
      |> GatewayConfig.changeset(params)
      |> Map.put(:action, :validate)

    {:noreply, assign(socket, form: to_form(changeset), saved: false)}
  end

  @impl true
  def handle_event("save", %{"gateway_config" => params}, socket) do
    case GatewayConfig.apply(params) do
      {:ok, _config} ->
        {:noreply, assign(socket, saved: true)}

      {:error, changeset} ->
        {:noreply, assign(socket, form: to_form(changeset))}
    end
  end
end
```

### Step 4: Add routes to `router.ex`

```elixir
scope "/admin", ApiGatewayWeb do
  pipe_through [:browser, :admin_auth]

  live "/dashboard",       DashboardLive
  live "/circuit-breakers", CircuitBreakerLive
  live "/config",           ConfigLive
end
```

### Step 5: Tests

```elixir
# test/api_gateway_web/live/dashboard_live_test.exs
defmodule ApiGatewayWeb.DashboardLiveTest do
  use ApiGatewayWeb.ConnCase
  import Phoenix.LiveViewTest

  test "renders dashboard with metrics", %{conn: conn} do
    {:ok, view, html} = live(conn, ~p"/admin/dashboard")
    assert html =~ "api_gateway dashboard"
    assert html =~ "Request rate"
    assert html =~ "Circuit Breakers"
  end

  test "updates when tick fires", %{conn: conn} do
    {:ok, view, _html} = live(conn, ~p"/admin/dashboard")
    send(view.pid, :tick)
    html = render(view)
    assert html =~ "req/s"
  end

  test "circuit reset button clears ETS state", %{conn: conn} do
    :ets.insert(:circuit_breaker, {"test-host", :open, System.monotonic_time(:millisecond)})

    {:ok, view, _html} = live(conn, ~p"/admin/circuit-breakers")
    assert render(view) =~ "test-host"

    view |> element("[phx-click=reset_circuit][phx-value-host=test-host]") |> render_click()
    refute render(view) =~ "test-host"
  end

  test "config form shows validation errors inline", %{conn: conn} do
    {:ok, view, _html} = live(conn, ~p"/admin/config")

    html = view
    |> form("#config-form", gateway_config: %{rate_limit_per_minute: -1})
    |> render_change()

    assert html =~ "must be greater than"
  end
end
```

### Step 6: Run the tests

```bash
mix test test/api_gateway_web/live/ --trace
```

---

## Trade-off analysis

| Aspect | LiveView | React + REST polling | React + WebSocket |
|--------|---------|---------------------|-------------------|
| State location | server (one process / conn) | client | client |
| Network payload | HTML diffs (small) | full JSON responses | custom messages |
| Real-time updates | push from server | polling interval | push |
| JavaScript required | minimal (phoenix.js) | full framework | full framework |
| Server memory | 1 process per connection | none | connection overhead |
| Testability | `Phoenix.LiveViewTest` | Cypress / RTL | Cypress / RTL |

---

## Common production mistakes

**1. Not re-enqueuing the tick in `handle_info(:tick, ...)`**
The pattern is: `Process.send_after(self(), :tick, @tick_ms)` at the start of the handler,
not only in `mount/3`. If you only send it in `mount/3`, the tick fires once and stops.

**2. Subscribing to PubSub without the `connected?/1` guard**
LiveView renders twice: once server-side (SSR, no WebSocket) and once after the WebSocket
connects. Without the guard, you subscribe during SSR -- the process receives broadcasts but
has no way to push diffs. Always guard PubSub subscriptions with `if connected?(socket)`.

**3. Setting `action: :validate` on `phx-submit` event**
`action: :validate` activates error display. It should be set in the `"validate"` handler
(`phx-change`). In the `"save"` handler, the changeset should attempt the real operation.

**4. Unbounded history list**
Every tick appends to `:history`. Without `Enum.take/2`, after a day of uptime the list
has 86,400 entries. Always cap it: `Enum.take([new | h], @history_limit)`.

**5. Reading ETS directly in the template**
ETS reads inside HEEx templates run on every render. Put ETS reads in `handle_info(:tick, ...)`
and store results as assigns. The template should be a pure function of assigns.

---

## Resources

- [Phoenix LiveView documentation](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.html) -- lifecycle callbacks
- [Phoenix.LiveViewTest](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveViewTest.html) -- `live/2`, `render_click/2`, `form/3`
- [LiveView security model](https://hexdocs.pm/phoenix_live_view/security-model.html) -- why `connected?` matters
- [Streams in LiveView](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.html#stream/3) -- efficient rendering for large lists
