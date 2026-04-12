# Phoenix LiveView — Real-Time Updates with PubSub

**Project**: `liveview_realtime` — a live trading dashboard that pushes price ticks to every connected browser without polling.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

You're building the operator dashboard of a small crypto exchange. Traders need sub-second
feedback on price changes across ~30 trading pairs. The previous implementation used a React
SPA polling `GET /api/ticks` every 500ms, which produced ~2M requests/hour at peak and
still felt laggy. The team decided to migrate to Phoenix LiveView driven by
`Phoenix.PubSub` so the server pushes deltas as they happen, each browser keeps a
persistent WebSocket, and no HTTP polling is left on the hot path.

Three non-negotiable requirements came out of the post-mortem:

1. **No polling**. Every UI change must originate from a server-side event (a price tick,
   an operator action, or a system broadcast). The browser must never `setInterval` against
   the backend.
2. **Reconnect must be boring**. When a trader's laptop wakes from sleep, the LiveView
   must re-mount without losing the current price snapshot. This requires splitting
   the `mount/3` callback between disconnected (first HTTP render) and connected
   (WebSocket) stages.
3. **Backpressure under burst**. Some pairs emit 200 ticks/second during volatile periods.
   The LiveView must not ship every tick as a separate DOM patch — we buffer and flush
   at a fixed rate so the browser doesn't thrash the layout engine.

Project structure at this point:

```
liveview_realtime/
├── lib/
│   ├── liveview_realtime/
│   │   ├── application.ex            # supervises Endpoint, PubSub, PriceFeed
│   │   ├── price_feed.ex             # simulates the upstream exchange feed
│   │   └── ticker.ex                 # domain — tick struct + helpers
│   └── liveview_realtime_web/
│       ├── endpoint.ex
│       ├── router.ex
│       ├── live/
│       │   └── dashboard_live.ex     # the LiveView you implement
│       └── components/
│           └── layouts.ex
├── test/
│   └── liveview_realtime_web/
│       └── live/
│           └── dashboard_live_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The two-phase `mount/3`

A LiveView mounts twice per session. First over plain HTTP (to produce crawlable,
indexable markup), then over the WebSocket once the JS client connects:

```
Browser                          Server
   │                                │
   │ GET /dashboard                 │
   │───────────────────────────────▶│  mount/3 called, connected?(socket) == false
   │                                │  render static HTML, send it back
   │◀───────────────────────────────│
   │ (HTML rendered, JS boots)      │
   │ WebSocket connect              │
   │───────────────────────────────▶│  mount/3 called AGAIN, connected?(socket) == true
   │                                │  now safe to subscribe to PubSub, start timers
```

Subscribing to PubSub during the first (disconnected) mount would leak processes:
the disconnected mount is a transient request process that exits right after
rendering. Always guard side effects with `if connected?(socket)`.

---

### 2. `Phoenix.PubSub` as the nervous system

`Phoenix.PubSub` is a thin local/distributed pub/sub over `:pg` (process groups).
It's already in your supervision tree because Phoenix starts one per app. Topics are
plain strings; there is no schema enforcement — discipline is on you.

```
                   ┌─────────────────┐
PriceFeed ───broadcast("ticker:BTC")─▶│  Phoenix.PubSub │
                   └────────┬────────┘
                            │ dispatch to local subscribers
              ┌─────────────┼─────────────┐
              ▼             ▼             ▼
       LiveView #1    LiveView #2   LiveView #3
       (BTC tab)      (BTC tab)     (ETH tab — not subscribed)
```

Broadcasting is O(subscribers). For 10k LiveView processes subscribed to the same
topic, one broadcast fans out 10k messages. That's fine — BEAM message passing is
cheap — but it's a cost to remember when you design topic granularity.

---

### 3. `handle_info/2` — the LiveView's mailbox

Every LiveView is a GenServer-like process. `handle_info/2` receives the PubSub
messages. What you return controls re-rendering:

| Return | Effect |
|--------|--------|
| `{:noreply, assign(socket, :price, 42)}` | Re-render diff containing `:price` |
| `{:noreply, socket}` | No re-render (common for side effects) |
| `{:noreply, push_event(socket, "flash", %{})}` | Push a JS event without re-render |

The diff engine compares the previous assigns against the new ones and ships only
the differences as a small JSON payload. The browser patches the DOM in place.

---

### 4. Throttling high-frequency updates

Naive implementation: on every tick, call `assign/3` and re-render. At 200 ticks/s
per pair × 30 pairs × 100 connected clients that's 600k DOM diffs per second —
nothing survives that.

The fix is a per-LiveView buffer with a periodic flush:

```
tick ────▶ handle_info ────▶ buffer (in-memory map in socket)
                                │
                                │  every 100ms (scheduled tick)
                                ▼
                          flush to assigns + re-render
```

The buffer is a map in assigns; the flush is scheduled with `Process.send_after/3`
when the LiveView mounts. This bounds re-render frequency regardless of incoming
tick rate.

---

### 5. Temporary assigns for write-only data

If you render a 500-item price list, Phoenix keeps the full list in socket memory
so it can compute future diffs. For append-only feeds where you only care about
the last N items, declare the assign as temporary:

```elixir
{:ok, assign(socket, :ticks, []), temporary_assigns: [ticks: []]}
```

After each render, Phoenix resets `:ticks` to `[]` on the server side. The client
still has the HTML; the server frees the memory. This is the foundation that the
LiveView Streams API (exercise 213) generalizes.

---

## Implementation

### Step 1: Create the project

```bash
mix phx.new liveview_realtime --no-ecto --no-mailer
cd liveview_realtime
```

### Step 2: The domain — `lib/liveview_realtime/ticker.ex`

```elixir
defmodule LiveviewRealtime.Ticker do
  @moduledoc """
  Price tick value object. Immutable and comparable.

  The `ts` field uses `System.monotonic_time(:millisecond)` so ticks can be
  compared across processes on the same node. For cross-node ordering you'd
  need a hybrid logical clock — out of scope here.
  """

  @type pair :: String.t()
  @type t :: %__MODULE__{
          pair: pair(),
          price: float(),
          ts: integer()
        }

  defstruct [:pair, :price, :ts]

  @spec new(pair(), float()) :: t()
  def new(pair, price) when is_binary(pair) and is_float(price) do
    %__MODULE__{pair: pair, price: price, ts: System.monotonic_time(:millisecond)}
  end

  @spec topic(pair()) :: String.t()
  def topic(pair), do: "ticker:" <> pair
end
```

### Step 3: The feed simulator — `lib/liveview_realtime/price_feed.ex`

```elixir
defmodule LiveviewRealtime.PriceFeed do
  @moduledoc """
  Simulates an upstream exchange feed. In production this would be a WebSocket
  client to Binance, Coinbase, etc. Here we generate random walks.
  """
  use GenServer

  alias LiveviewRealtime.Ticker

  @pairs ~w(BTC-USD ETH-USD SOL-USD ADA-USD DOT-USD)
  @tick_interval_ms 50

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    state =
      Map.new(@pairs, fn pair ->
        {pair, 100.0 + :rand.uniform() * 900.0}
      end)

    schedule_tick()
    {:ok, state}
  end

  @impl true
  def handle_info(:tick, state) do
    new_state =
      Map.new(state, fn {pair, price} ->
        delta = (:rand.uniform() - 0.5) * price * 0.002
        new_price = price + delta
        tick = Ticker.new(pair, new_price)
        Phoenix.PubSub.broadcast(LiveviewRealtime.PubSub, Ticker.topic(pair), {:tick, tick})
        {pair, new_price}
      end)

    schedule_tick()
    {:noreply, new_state}
  end

  defp schedule_tick, do: Process.send_after(self(), :tick, @tick_interval_ms)
end
```

### Step 4: The LiveView — `lib/liveview_realtime_web/live/dashboard_live.ex`

```elixir
defmodule LiveviewRealtimeWeb.DashboardLive do
  use LiveviewRealtimeWeb, :live_view

  alias LiveviewRealtime.Ticker

  @flush_interval_ms 100
  @pairs ~w(BTC-USD ETH-USD SOL-USD ADA-USD DOT-USD)

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket) do
      Enum.each(@pairs, fn pair ->
        Phoenix.PubSub.subscribe(LiveviewRealtime.PubSub, Ticker.topic(pair))
      end)

      schedule_flush()
    end

    socket =
      socket
      |> assign(:prices, Map.new(@pairs, &{&1, nil}))
      |> assign(:buffer, %{})
      |> assign(:tick_count, 0)

    {:ok, socket}
  end

  @impl true
  def handle_info({:tick, %Ticker{} = tick}, socket) do
    buffer = Map.put(socket.assigns.buffer, tick.pair, tick)
    {:noreply, assign(socket, buffer: buffer, tick_count: socket.assigns.tick_count + 1)}
  end

  @impl true
  def handle_info(:flush, socket) do
    schedule_flush()

    case socket.assigns.buffer do
      empty when empty == %{} ->
        {:noreply, socket}

      buffer ->
        prices = Map.merge(socket.assigns.prices, buffer)
        {:noreply, assign(socket, prices: prices, buffer: %{})}
    end
  end

  defp schedule_flush, do: Process.send_after(self(), :flush, @flush_interval_ms)

  @impl true
  def render(assigns) do
    ~H"""
    <div class="dashboard">
      <h1>Live Prices</h1>
      <p>Ticks received: <span id="tick-count">{@tick_count}</span></p>
      <table>
        <thead><tr><th>Pair</th><th>Price</th></tr></thead>
        <tbody>
          <tr :for={{pair, tick} <- @prices} id={"row-#{pair}"}>
            <td>{pair}</td>
            <td>{format_price(tick)}</td>
          </tr>
        </tbody>
      </table>
    </div>
    """
  end

  defp format_price(nil), do: "—"
  defp format_price(%Ticker{price: p}), do: :erlang.float_to_binary(p, decimals: 2)
end
```

### Step 5: Router and supervision

```elixir
# lib/liveview_realtime_web/router.ex
scope "/", LiveviewRealtimeWeb do
  pipe_through :browser
  live "/", DashboardLive, :index
end
```

```elixir
# lib/liveview_realtime/application.ex — children list
children = [
  LiveviewRealtimeWeb.Telemetry,
  {Phoenix.PubSub, name: LiveviewRealtime.PubSub},
  LiveviewRealtimeWeb.Endpoint,
  LiveviewRealtime.PriceFeed
]
```

### Step 6: Tests — `test/liveview_realtime_web/live/dashboard_live_test.exs`

```elixir
defmodule LiveviewRealtimeWeb.DashboardLiveTest do
  use LiveviewRealtimeWeb.ConnCase, async: true
  import Phoenix.LiveViewTest

  alias LiveviewRealtime.Ticker

  describe "mount lifecycle" do
    test "renders static HTML on disconnected mount", %{conn: conn} do
      {:ok, _view, html} = live(conn, "/")
      assert html =~ "Live Prices"
      assert html =~ "BTC-USD"
    end

    test "subscribes to PubSub only when connected", %{conn: conn} do
      {:ok, view, _html} = live(conn, "/")
      tick = Ticker.new("BTC-USD", 42_000.0)
      Phoenix.PubSub.broadcast(LiveviewRealtime.PubSub, Ticker.topic("BTC-USD"), {:tick, tick})

      Process.sleep(200)

      assert render(view) =~ "42000.00"
    end
  end

  describe "tick buffering" do
    test "coalesces multiple ticks into a single render", %{conn: conn} do
      {:ok, view, _html} = live(conn, "/")

      for price <- [100.0, 200.0, 300.0] do
        tick = Ticker.new("ETH-USD", price)
        Phoenix.PubSub.broadcast(LiveviewRealtime.PubSub, Ticker.topic("ETH-USD"), {:tick, tick})
      end

      Process.sleep(200)
      html = render(view)
      assert html =~ "300.00"
      refute html =~ "100.00"
      refute html =~ "200.00"
    end
  end
end
```

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Topic granularity vs. broadcast amplification**
One topic per pair means subscribers only wake for pairs they care about. One
global `"ticker:*"` topic means every LiveView wakes on every tick regardless
of filtering. Prefer fine-grained topics even if you have more of them —
BEAM handles topic registries (`:pg`) efficiently; it doesn't handle unnecessary
process wakeups well.

**2. `Process.sleep` in tests is a code smell that sometimes wins**
Real LiveView tests should use `render_async/1` or `assert_receive`. But for
interval-based flushes there's no clean signal to await without instrumenting
the LiveView. Keep the sleep short (200ms) and accept the determinism cost,
or expose a testing-only `flush_now` handler.

**3. Memory growth from stale buffer**
If a LiveView is slow to process its mailbox (e.g., GC pause), ticks pile up in
the mailbox *and* in the buffer map. Consider bounding the buffer to the last
N pairs. For this dashboard with 5 pairs it doesn't matter; for 5000 pairs
it does.

**4. `connected?(socket)` is only accurate during mount**
On reconnect, `mount/3` runs again. Anything you scheduled with `send_after`
is gone with the previous process; you must re-subscribe and re-schedule.
Don't cache work in module attributes or external tables that assume a single
mount.

**5. PubSub is local by default across adapters**
`Phoenix.PubSub` ships with `PG2`/`:pg` adapters that span the cluster when
nodes are connected. If your feed runs on node A and your LiveView on node B,
ensure `libcluster` or equivalent is forming the cluster — otherwise subscribers
on B will never hear broadcasts from A.

**6. Temporary assigns vs. streams**
For append-only feeds, prefer the LiveView Streams API (exercise 213) over
temporary assigns. Streams manage client-side DOM identity; temporary assigns
only free server memory. Streams are the current recommendation from the
Phoenix team.

**7. When NOT to use this pattern**
If your "real-time" requirement is 1-second updates and your page is mostly
read-only reports, server-sent events or a plain AJAX poll are simpler,
easier to cache at the CDN, and don't tie up a WebSocket per reader. LiveView
shines when interactions are bidirectional and sub-second. For passive
dashboards consumed by dozens of viewers, it's overkill.

---

## Performance notes

Measure the end-to-end latency from `PriceFeed` broadcast to DOM patch via
`:telemetry.execute/3` at both ends:

```elixir
:telemetry.execute([:price_feed, :tick], %{ts: System.monotonic_time(:microsecond)}, %{pair: pair})
```

Expected on localhost: median < 500µs between broadcast and LiveView receipt.
The browser render is an additional 1–5ms depending on diff size.

Drop the flush interval to `0` (disable buffering) and observe scheduler load
in `:observer.start()`. You'll see utilization jump sharply above ~200 ticks/s
per client — that's the argument for buffering.

---

## Resources

- [Phoenix LiveView docs](https://hexdocs.pm/phoenix_live_view/) — read the full `Phoenix.LiveView` module doc
- [`Phoenix.PubSub`](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html) — adapter model and semantics
- [Chris McCord — LiveView announcement](https://dockyard.com/blog/2018/12/12/phoenix-liveview-interactive-real-time-apps-no-need-to-write-javascript)
- [LiveView performance: temporary assigns](https://hexdocs.pm/phoenix_live_view/assigns-eex.html#temporary-assigns)
- [`:pg` — Erlang process groups](https://www.erlang.org/doc/man/pg.html) — the primitive under PubSub
- [Dashbit blog — Operable Phoenix](https://dashbit.co/blog/operable-phoenix) — supervision for real-time apps
