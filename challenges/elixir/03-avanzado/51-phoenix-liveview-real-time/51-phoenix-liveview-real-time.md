# Phoenix LiveView: Real-Time Gateway Dashboard

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` umbrella is in production. The ops team needs a web dashboard to monitor
the gateway in real time: request rates, circuit breaker states, rate-limiter queue depths.
Instead of building a React SPA with a REST polling loop, you'll build it with Phoenix
LiveView — the entire dashboard is server-rendered HTML with WebSocket-pushed diffs.

Project structure for this exercise:

```
api_gateway_umbrella/apps/gateway_api/
├── lib/gateway_api_web/
│   ├── live/
│   │   ├── dashboard_live.ex         # ← you implement this
│   │   ├── dashboard_live.html.heex  # ← and this
│   │   ├── circuit_breaker_live.ex   # ← and this
│   │   └── config_live.ex            # ← and this
│   └── presence.ex                   # ← and this
└── test/gateway_api_web/live/
    └── dashboard_live_test.exs       # given tests
```

---

## Why LiveView over a JavaScript SPA

A React dashboard requires: a REST or GraphQL API, JSON serialization, a client-side state
manager, and a polling or WebSocket layer. Each layer has failure modes and deployment
concerns. LiveView collapses all of this:

```
Browser                    Server
───────                    ──────
HTML render ◀────────── mount/3 assigns initial state
                │
User click ──── handle_event/3 ──▶ update state
                │
                └─────── send diff HTML ──▶ browser patches DOM
```

The server computes diffs; the browser applies them. No JSON. No client state sync. The
tradeoff: all users' state lives in server memory (one LiveView process per connection),
and the server must be reachable via WebSocket.

---

## LiveView lifecycle

```
mount/3         — called once when the LiveView mounts
                  initialize assigns; start PubSub subscriptions; schedule ticks
render/1        — called after every assign change; returns HEEx template
handle_event/3  — called when user interacts (phx-click, phx-submit, phx-change)
handle_info/2   — called when a message arrives (PubSub, Process.send_after, GenServer)
```

The key insight: **the socket is the state, the template is a pure function of the state**.
Phoenix computes the diff between the previous and current render and sends only the changed
HTML fragments over WebSocket.

---

## Implementation

### Step 1: `lib/gateway_api_web/live/dashboard_live.ex`

```elixir
defmodule GatewayApiWeb.DashboardLive do
  use GatewayApiWeb, :live_view

  @tick_ms 1_000
  @history_limit 60   # 60 seconds of history

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket) do
      # Subscribe to gateway metrics published by the telemetry handler
      Phoenix.PubSub.subscribe(GatewayApi.PubSub, "gateway:metrics")
      # Schedule the first tick
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
    # TODO:
    # 1. Re-schedule the next tick
    # 2. Fetch fresh metrics
    # 3. Update :metrics assign
    # 4. Update :history using update/3 — keep last @history_limit entries
    # 5. Update :circuit_breakers
    # HINT: update(socket, :history, fn h -> Enum.take([metrics | h], @history_limit) end)
    {:noreply, socket}
  end

  @impl true
  def handle_info({:metrics_update, metrics}, socket) do
    # PubSub broadcast from the telemetry handler — update state
    # TODO: assign updated metrics
    {:noreply, socket}
  end

  # ---------------------------------------------------------------------------
  # Private helpers — implement these
  # ---------------------------------------------------------------------------

  defp fetch_current_metrics do
    # TODO: read from ETS tables / GenServer state
    # Return a map: %{request_rate: float, error_rate: float, p99_ms: float, ts: DateTime.t()}
    %{request_rate: 0.0, error_rate: 0.0, p99_ms: 0.0, ts: DateTime.utc_now()}
  end

  defp fetch_circuit_breaker_states do
    # TODO: :ets.tab2list(:circuit_breaker) from exercise 45
    # Return [%{host: String.t(), state: atom(), opened_at: integer() | nil}]
    []
  end

  defp fetch_rate_limiter_size do
    # TODO: :ets.info(:rate_limiter_windows, :size)
    0
  end
end
```

```heex
<%# lib/gateway_api_web/live/dashboard_live.html.heex %>
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

  <%# History table — last 60 samples %>
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

### Step 2: `lib/gateway_api_web/live/circuit_breaker_live.ex`

```elixir
defmodule GatewayApiWeb.CircuitBreakerLive do
  use GatewayApiWeb, :live_view

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket), do: Phoenix.PubSub.subscribe(GatewayApi.PubSub, "circuit_breaker:events")
    {:ok, assign(socket, events: [], states: load_states())}
  end

  @impl true
  def handle_event("reset_circuit", %{"host" => host}, socket) do
    # TODO: clear the circuit breaker ETS entry for this host
    # HINT: :ets.delete(:circuit_breaker, host)
    {:noreply, assign(socket, states: load_states())}
  end

  @impl true
  def handle_info({:circuit_state_change, event}, socket) do
    # TODO: prepend event to :events, keep last 50
    {:noreply, socket}
  end

  defp load_states do
    # TODO: read :circuit_breaker ETS table
    []
  end
end
```

### Step 3: `lib/gateway_api_web/live/config_live.ex`

```elixir
defmodule GatewayApiWeb.ConfigLive do
  use GatewayApiWeb, :live_view

  alias GatewayCore.GatewayConfig

  @impl true
  def mount(_params, _session, socket) do
    changeset = GatewayConfig.changeset(%GatewayConfig{}, %{})
    {:ok, assign(socket, form: to_form(changeset), saved: false)}
  end

  @impl true
  def handle_event("validate", %{"gateway_config" => params}, socket) do
    # TODO: validate changeset with action: :validate
    # HINT: changeset |> Map.put(:action, :validate)
    {:noreply, socket}
  end

  @impl true
  def handle_event("save", %{"gateway_config" => params}, socket) do
    # TODO: apply config, update assigns, show confirmation
    {:noreply, assign(socket, saved: true)}
  end
end
```

### Step 4: Add routes to `router.ex`

```elixir
# In gateway_api_web/router.ex
scope "/admin", GatewayApiWeb do
  pipe_through [:browser, :admin_auth]

  live "/dashboard",       DashboardLive
  live "/circuit-breakers", CircuitBreakerLive
  live "/config",           ConfigLive
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/gateway_api_web/live/dashboard_live_test.exs
defmodule GatewayApiWeb.DashboardLiveTest do
  use GatewayApiWeb.ConnCase
  import Phoenix.LiveViewTest

  test "renders dashboard with metrics", %{conn: conn} do
    {:ok, view, html} = live(conn, ~p"/admin/dashboard")
    assert html =~ "api_gateway dashboard"
    assert html =~ "Request rate"
    assert html =~ "Circuit Breakers"
  end

  test "updates when tick fires", %{conn: conn} do
    {:ok, view, _html} = live(conn, ~p"/admin/dashboard")
    # Trigger a tick manually
    send(view.pid, :tick)
    # Wait for re-render
    html = render(view)
    assert html =~ "req/s"
  end

  test "circuit reset button clears ETS state", %{conn: conn} do
    # Seed a broken circuit
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
mix test test/gateway_api_web/live/ --trace
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

Reflection: the DashboardLive process holds the last 60 seconds of history. With 500
concurrent ops users, how much memory does this consume? How would you change the design
if the history window needed to be 24 hours?

---

## Common production mistakes

**1. Not re-enqueuing the tick in `handle_info(:tick, ...)`**
The pattern is: `Process.send_after(self(), :tick, @tick_ms)` at the start of the handler,
not in `mount/3`. If you only send it in `mount/3`, the tick fires once and stops.

**2. Subscribing to PubSub without the `connected?/1` guard**
LiveView renders twice: once server-side (SSR, no WebSocket) and once after the WebSocket
connects. Without the guard, you subscribe during SSR — the process receives broadcasts but
has no way to push diffs. Always guard PubSub subscriptions with `if connected?(socket)`.

**3. Setting `action: :validate` on `phx-submit` event**
`action: :validate` activates error display. It should be set in the `"validate"` handler
(`phx-change`). In the `"save"` handler, the changeset should attempt the real operation —
not be forced into validate-only mode.

**4. Unbounded history list**
Every tick appends to `:history`. Without `Enum.take/2`, after a day of uptime the list
has 86,400 entries. Always cap it: `Enum.take([new | h], @history_limit)`.

**5. Reading ETS directly in the template**
ETS reads inside HEEx templates run on every render, including diffs. Put ETS reads in
`handle_info(:tick, ...)` and store results as assigns. The template should be a pure
function of assigns — no side effects, no external reads.

---

## Resources

- [Phoenix LiveView documentation](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.html) — lifecycle callbacks
- [Phoenix.LiveViewTest](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveViewTest.html) — `live/2`, `render_click/2`, `form/3`
- [LiveView security model](https://hexdocs.pm/phoenix_live_view/security-model.html) — why `connected?` matters
- [Streams in LiveView](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.html#stream/3) — efficient rendering for large lists
