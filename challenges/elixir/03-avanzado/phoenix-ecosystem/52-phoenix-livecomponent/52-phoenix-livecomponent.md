# Phoenix LiveComponent — Stateful, Stateless, and Function Components

**Project**: `live_components` — a composable cart UI with per-item state, reusable buttons, and a shared checkout summary.

---

## Project context

You inherited a LiveView that grew past 1,200 lines. It renders a shopping cart, each
line item has its own quantity stepper, a "remove" confirmation modal, a tax breakdown,
and a sticky summary bar. Every interaction — changing a quantity, opening the modal,
applying a promo — flows through the top-level LiveView's `handle_event/3`. That
function is now an unreadable `case` on 23 branches, and every mutation re-renders the
entire tree even when only one line item changed.

The refactor plan is to decompose the page into three kinds of components,
each with a clear purpose:

1. **Function components** (`Phoenix.Component`) — pure render helpers, no state, no
   server round-trip on interaction. Used for design-system atoms: buttons, inputs, cards.
2. **Stateless LiveComponents** — reusable UI blocks that receive all state from the
   parent via assigns, emit events upward via `send` or `phx-target` so the parent
   decides how to react.
3. **Stateful LiveComponents** — own private state (e.g., "is the modal open?")
   that doesn't belong to the parent. They have their own `mount/1`, `update/2`,
   and `handle_event/3`. Crucially, they share the parent's process — they are
   not separate GenServers, they are render scopes with isolated state.

The key production motivation is **targeted re-rendering**. When the quantity of
one line changes, only that component's diff is shipped to the browser; the other
nine line items and the summary bar are unaffected.

Project structure at this point:

```
live_components/
├── lib/
│   ├── live_components/
│   │   ├── application.ex
│   │   └── cart.ex                     # in-memory cart domain
│   └── live_components_web/
│       ├── endpoint.ex
│       ├── router.ex
│       ├── components/
│       │   └── core_components.ex      # function components (button, input, card)
│       ├── live/
│       │   └── cart_live.ex            # parent LiveView
│       └── live_components/
│           ├── line_item_component.ex  # stateful LiveComponent
│           └── summary_component.ex    # stateless LiveComponent
└── test/
    └── live_components_web/
        └── live/
            └── cart_live_test.exs
```

---

## Why LiveComponents and not parent-only state

A LiveView with 20 nested widgets is a maintenance nightmare. LiveComponents let each widget own its own update cycle while sharing the parent process.

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. Three flavors compared

```
                     Function component    Stateless LC        Stateful LC
                     (Phoenix.Component)  (use LiveComponent)  (use LiveComponent + :id)
                     ─────────────────    ─────────────────    ──────────────────────
Has process?         no (compile-time)    no (runs in parent)  no (runs in parent)
Has assigns?         yes (from caller)    yes (from parent)    yes (owned)
Has handle_event?    no                   yes (target parent)  yes (target self)
Has mount/update?    no                   no                   yes
Has :id required?    no                   no                   YES (identity)
Diff scope           inlined into parent  own change tracker   own change tracker
Use case             buttons, labels      modal, tabs, lists   modal with local open/close
```

The `:id` is what makes the difference. Without `:id`, a LiveComponent is **stateless**:
every render recomputes everything from assigns, the server keeps no state for it between
renders. With `:id`, Phoenix allocates a persistent change-tracking slot keyed by
`{module, id}`, and `mount/1` runs once per distinct id.

---

### 2. `update/2` is the lifecycle hook you'll use most

Stateful LiveComponents have `mount/1` (called once per id when the component first
renders) and `update/2` (called every time the parent passes new assigns). The default
`update/2` just merges assigns. You override it when you need to react to external
changes — for example, resetting a local "draft" state when the parent swaps the
underlying record:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
def update(%{line_item: new_item} = assigns, socket) do
  {:ok,
   socket
   |> assign(assigns)
   |> assign(:draft_qty, new_item.qty)}
end
```

`mount/1` fires once. `update/2` fires on every parent render. Conflating the two is
a common source of "my local state keeps getting reset" bugs.

---

### 3. Event routing — `phx-target`

When a stateless LiveComponent emits an event, it bubbles up to the parent LiveView
by default. When you want the event to land in the component itself (the stateful
case), you set `phx-target={@myself}`:

```heex
<button phx-click="toggle" phx-target={@myself}>Open</button>
```

`@myself` is a `%Phoenix.LiveComponent.CID{}` — a compile-time reference to the
current component. The wire protocol uses the CID to route the event back. Without
`phx-target`, the event goes to the parent, and you must pass data back via
`send(self(), ...)` or by making the parent re-render with new assigns.

---

### 4. Targeted diffs — the performance payoff

Phoenix tracks changes per component. When a stateful LC updates its own assigns,
only that component's HTML diff ships to the browser. The outer LiveView does not
re-render:

```
Parent LiveView (assigns: cart, user)
├── LineItemComponent id="item-1" (assigns: item, qty, open?)      ← user clicks +
├── LineItemComponent id="item-2" (assigns: item, qty, open?)      ← NOT re-rendered
├── LineItemComponent id="item-3" (assigns: item, qty, open?)      ← NOT re-rendered
└── SummaryComponent  id="summary" (assigns: total)                ← re-renders if total changed
```

This is a significant win versus monolithic LiveViews: for a cart with 20 line items,
a quantity bump only transmits one component's diff (~200 bytes) instead of the whole
cart's HTML (~20 KB).

---

### 5. When stateful LCs leak memory

Stateful LCs are kept alive on the server for as long as their `:id` appears in the
parent's render. If you scroll an infinite list and components drop out of view, they
drop out of the render tree — and their state is discarded. That's usually what you
want. But if you use a dynamic id (e.g., `id={"item-#{Ecto.UUID.generate()}"}` —
don't do this), every render creates a fresh component. Keep ids stable and derived
from the domain (`item.id`).

---

## Design decisions

**Option A — everything in the parent LiveView**
- Pros: one process, one state; simple flow.
- Cons: parent becomes a god object; every sub-area forces the full tree to re-render logic.

**Option B — LiveComponents for bounded sub-areas** (chosen)
- Pros: isolated state updates; smaller diffs; testable independently.
- Cons: lifecycle adds concepts (update/2, handle_event within component); inter-component communication needs discipline.

→ Chose **B** because past a certain complexity LiveComponents are the only way to keep the parent readable.

---

## Implementation

### Step 1: Create the project

**Objective**: Scaffold a Phoenix app without Ecto since the cart is an in-memory demo owned by the LiveView process.

```bash
mix phx.new live_components --no-ecto --no-mailer
cd live_components
```

### Step 2: The cart domain — `lib/live_components/cart.ex`

**Objective**: Keep cart logic pure functions over `Decimal` so totals stay exact and components stay rendering-only.

```elixir
defmodule LiveComponents.Cart do
  @moduledoc "In-memory cart for the exercise. Not thread-safe; per-LV state."

  @type line_item :: %{id: String.t(), name: String.t(), price: Decimal.t(), qty: pos_integer()}

  @spec seed() :: [line_item()]
  def seed do
    [
      %{id: "sku-1", name: "T-shirt", price: Decimal.new("19.90"), qty: 1},
      %{id: "sku-2", name: "Mug", price: Decimal.new("9.50"), qty: 2},
      %{id: "sku-3", name: "Sticker pack", price: Decimal.new("4.00"), qty: 3}
    ]
  end

  @spec total([line_item()]) :: Decimal.t()
  def total(items) do
    Enum.reduce(items, Decimal.new(0), fn %{price: p, qty: q}, acc ->
      Decimal.add(acc, Decimal.mult(p, q))
    end)
  end

  @spec set_qty([line_item()], String.t(), pos_integer()) :: [line_item()]
  def set_qty(items, id, qty) when qty >= 1 do
    Enum.map(items, fn
      %{id: ^id} = item -> %{item | qty: qty}
      item -> item
    end)
  end

  @spec remove([line_item()], String.t()) :: [line_item()]
  def remove(items, id), do: Enum.reject(items, &(&1.id == id))
end
```

### Step 3: Function components — `lib/live_components_web/components/core_components.ex`

**Objective**: Define design-system atoms as stateless `Phoenix.Component`s with typed `attr`/`slot` contracts so misuse fails at compile time.

```elixir
defmodule LiveComponentsWeb.CoreComponents do
  @moduledoc "Design-system atoms. Pure render helpers, no LiveView state."
  use Phoenix.Component

  attr :kind, :atom, values: [:primary, :secondary, :danger], default: :primary
  attr :rest, :global, include: ~w(phx-click phx-target disabled type)
  slot :inner_block, required: true

  def button(assigns) do
    ~H"""
    <button class={"btn btn-#{@kind}"} {@rest}>
      {render_slot(@inner_block)}
    </button>
    """
  end

  attr :label, :string, required: true
  attr :value, :any, required: true

  def card(assigns) do
    ~H"""
    <div class="card">
      <div class="card-label">{@label}</div>
      <div class="card-value">{@value}</div>
    </div>
    """
  end
end
```

### Step 4: Stateful LiveComponent — `lib/live_components_web/live_components/line_item_component.ex`

**Objective**: Isolate modal and draft-qty state in the component so `phx-target={@myself}` events never touch the parent.

```elixir
defmodule LiveComponentsWeb.LineItemComponent do
  @moduledoc """
  Stateful LiveComponent. Owns the "confirm removal" modal visibility.

  Emits `{:remove, item_id}` and `{:set_qty, item_id, qty}` up to the parent
  via `send/2`. Quantity changes are local until the user confirms, at which
  point we notify the parent — this is the "draft state" pattern.
  """
  use Phoenix.LiveComponent

  import LiveComponentsWeb.CoreComponents

  @impl true
  def mount(socket) do
    {:ok, assign(socket, confirm_open?: false)}
  end

  @impl true
  def update(%{item: item} = assigns, socket) do
    {:ok,
     socket
     |> assign(assigns)
     |> assign(:draft_qty, item.qty)}
  end

  @impl true
  def handle_event("inc", _, socket) do
    {:noreply, assign(socket, draft_qty: socket.assigns.draft_qty + 1)}
  end

  @impl true
  def handle_event("dec", _, socket) do
    {:noreply, assign(socket, draft_qty: max(1, socket.assigns.draft_qty - 1))}
  end

  @impl true
  def handle_event("commit_qty", _, socket) do
    send(self(), {:set_qty, socket.assigns.item.id, socket.assigns.draft_qty})
    {:noreply, socket}
  end

  @impl true
  def handle_event("open_confirm", _, socket) do
    {:noreply, assign(socket, confirm_open?: true)}
  end

  @impl true
  def handle_event("cancel_confirm", _, socket) do
    {:noreply, assign(socket, confirm_open?: false)}
  end

  @impl true
  def handle_event("confirm_remove", _, socket) do
    send(self(), {:remove, socket.assigns.item.id})
    {:noreply, assign(socket, confirm_open?: false)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div class="line-item" id={"li-#{@item.id}"}>
      <span class="name">{@item.name}</span>
      <span class="price">${Decimal.to_string(@item.price)}</span>
      <div class="qty">
        <.button kind={:secondary} phx-click="dec" phx-target={@myself}>-</.button>
        <span class="qty-value">{@draft_qty}</span>
        <.button kind={:secondary} phx-click="inc" phx-target={@myself}>+</.button>
        <.button
          :if={@draft_qty != @item.qty}
          kind={:primary}
          phx-click="commit_qty"
          phx-target={@myself}
        >
          Update
        </.button>
      </div>
      <.button kind={:danger} phx-click="open_confirm" phx-target={@myself}>Remove</.button>

      <div :if={@confirm_open?} class="modal" role="dialog">
        <p>Remove {@item.name} from cart?</p>
        <.button kind={:danger} phx-click="confirm_remove" phx-target={@myself}>Yes</.button>
        <.button kind={:secondary} phx-click="cancel_confirm" phx-target={@myself}>Cancel</.button>
      </div>
    </div>
    """
  end
end
```

### Step 5: Stateless LiveComponent — `lib/live_components_web/live_components/summary_component.ex`

**Objective**: Derive total on every render from parent-passed items so the summary has zero state to keep in sync.

```elixir
defmodule LiveComponentsWeb.SummaryComponent do
  @moduledoc """
  Stateless — no own state, no id required for identity purposes.
  Gets `items` from parent, derives total on every render.
  """
  use Phoenix.LiveComponent

  alias LiveComponents.Cart

  @impl true
  def render(assigns) do
    assigns = assign(assigns, :total, Cart.total(assigns.items))

    ~H"""
    <div class="summary">
      <span>Items: {length(@items)}</span>
      <span>Total: ${Decimal.to_string(@total)}</span>
    </div>
    """
  end
end
```

### Step 6: Parent LiveView — `lib/live_components_web/live/cart_live.ex`

**Objective**: Own the authoritative items list and react to `send/2` messages from components so child events bubble explicitly.

```elixir
defmodule LiveComponentsWeb.CartLive do
  use LiveComponentsWeb, :live_view

  alias LiveComponents.Cart
  alias LiveComponentsWeb.{LineItemComponent, SummaryComponent}

  @impl true
  def mount(_params, _session, socket) do
    {:ok, assign(socket, items: Cart.seed())}
  end

  @impl true
  def handle_info({:set_qty, id, qty}, socket) do
    {:noreply, assign(socket, items: Cart.set_qty(socket.assigns.items, id, qty))}
  end

  @impl true
  def handle_info({:remove, id}, socket) do
    {:noreply, assign(socket, items: Cart.remove(socket.assigns.items, id))}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div class="cart">
      <h1>Cart</h1>
      <.live_component
        :for={item <- @items}
        module={LineItemComponent}
        id={"line-#{item.id}"}
        item={item}
      />
      <.live_component module={SummaryComponent} id="summary" items={@items} />
    </div>
    """
  end
end
```

### Step 7: Tests — `test/live_components_web/live/cart_live_test.exs`

**Objective**: Drive `element/3` and `render_click/1` against component targets so tests hit the exact `phx-target` dispatch path.

```elixir
defmodule LiveComponentsWeb.CartLiveTest do
  use LiveComponentsWeb.ConnCase, async: true
  import Phoenix.LiveViewTest

  describe "line item stepper" do
    test "increments draft qty without contacting parent until commit", %{conn: conn} do
      {:ok, view, _html} = live(conn, "/cart")

      view
      |> element("#li-sku-1 button", "+")
      |> render_click()

      # Draft bumped from 1 to 2 locally; summary still shows 1*19.90 + 2*9.50 + 3*4.00
      assert render(view) =~ "Total: $50.9"
      # Commit
      view |> element("#li-sku-1 button", "Update") |> render_click()
      assert render(view) =~ "Total: $70.8"
    end
  end

  describe "remove confirmation modal" do
    test "opens, cancels, then confirms", %{conn: conn} do
      {:ok, view, _html} = live(conn, "/cart")

      view |> element("#li-sku-2 button", "Remove") |> render_click()
      assert render(view) =~ "Remove Mug from cart?"

      view |> element("#li-sku-2 button", "Cancel") |> render_click()
      refute render(view) =~ "Remove Mug from cart?"

      view |> element("#li-sku-2 button", "Remove") |> render_click()
      view |> element("#li-sku-2 button", "Yes") |> render_click()

      refute render(view) =~ "Mug"
    end
  end
end
```

```bash
mix test
```

### Why this works

A stateful LiveComponent has its own `mount/1`, `update/2`, and `handle_event/3`. Phoenix only re-renders the components whose assigns changed, sending just their portion of the diff. The components live inside the parent LiveView process, so no extra processes are allocated.

---

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---


## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

**1. Stateful LCs share the parent's process**
They are not GenServers. A long computation in `handle_event/3` blocks the
entire LiveView (and every other component on the page). Offload with
`Task.async/1` or `assign_async/3`, never with blocking work.

**2. The `:id` must be stable and unique**
Reusing the same id for two different renders merges the state; generating
a new id every render throws away the state. Derive from domain (`"line-#{item.id}"`).
A UUID per render is the classic anti-pattern.

**3. Event routing ambiguity**
Forgetting `phx-target={@myself}` silently routes the event to the parent.
The parent's `handle_event/3` then fails to pattern-match and crashes with
`FunctionClauseError`. Make parent `handle_event/3` deliberate about which
events it owns; let component events stay in components.

**4. `update/2` vs. `mount/1` for derived state**
Put initialization that doesn't depend on parent assigns in `mount/1`. Put
derivations (like `draft_qty = item.qty`) in `update/2` so they refresh
whenever the parent passes a new item. Don't put them only in `mount/1` — a
new parent-driven update will not refresh them.

**5. Don't over-componentize**
Every LiveComponent has a change-tracking cost. Three LCs inside a LiveView
is fine. Three hundred is not. If a table has 500 rows and each row is a
stateful LC, you've built 500 change trackers. Prefer function components
for rows when there's no per-row state; reserve LCs for the rare case where
a row owns local state (e.g., inline edit).

**6. `send(self(), msg)` vs. `{:noreply, push_patch(socket, ...)}`**
The component communicates with the parent process via `send`. This works
because LCs share the parent's pid. It does NOT work across processes — don't
confuse this with distributed messaging.

**7. Tests must target the right element**
`element("#li-sku-1 button", "+")` is specific. Using `element("button", "+")`
without a scope hits every `+` button on the page and fails non-deterministically
once you add more items. Always scope by a stable id.

**8. When NOT to use this pattern**
For a design system published across products, function components are the
right boundary — they have no runtime dependency on LiveView. If your consumers
include a regular Phoenix controller view, stateless LCs won't work there.
Keep the atoms (buttons, inputs) as `Phoenix.Component`s, not LCs.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: component update diff under 500 bytes; re-render time scales with touched components, not tree size.

---

## Reflection

- Your component needs to trigger a parent state change. You have `send/3` and `send_update/2` and event propagation. Which one and when?
- Every component has its own mount. At 50 components per page, is that a problem? How would you measure?

---


## Executable Example

```elixir
defmodule LiveComponentsWeb.LineItemComponent do
  @moduledoc """
  Stateful LiveComponent. Owns the "confirm removal" modal visibility.

  Emits `{:remove, item_id}` and `{:set_qty, item_id, qty}` up to the parent
  via `send/2`. Quantity changes are local until the user confirms, at which
  point we notify the parent — this is the "draft state" pattern.
  """
  use Phoenix.LiveComponent

  import LiveComponentsWeb.CoreComponents

  @impl true
  def mount(socket) do
    {:ok, assign(socket, confirm_open?: false)}
  end

  @impl true
  def update(%{item: item} = assigns, socket) do
    {:ok,
     socket
     |> assign(assigns)
     |> assign(:draft_qty, item.qty)}
  end

  @impl true
  def handle_event("inc", _, socket) do
    {:noreply, assign(socket, draft_qty: socket.assigns.draft_qty + 1)}
  end

  @impl true
  def handle_event("dec", _, socket) do
    {:noreply, assign(socket, draft_qty: max(1, socket.assigns.draft_qty - 1))}
  end

  @impl true
  def handle_event("commit_qty", _, socket) do
    send(self(), {:set_qty, socket.assigns.item.id, socket.assigns.draft_qty})
    {:noreply, socket}
  end

  @impl true
  def handle_event("open_confirm", _, socket) do
    {:noreply, assign(socket, confirm_open?: true)}
  end

  @impl true
  def handle_event("cancel_confirm", _, socket) do
    {:noreply, assign(socket, confirm_open?: false)}
  end

  @impl true
  def handle_event("confirm_remove", _, socket) do
    send(self(), {:remove, socket.assigns.item.id})
    {:noreply, assign(socket, confirm_open?: false)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div class="line-item" id={"li-#{@item.id}"}>
      <span class="name">{@item.name}</span>
      <span class="price">${Decimal.to_string(@item.price)}</span>
      <div class="qty">
        <.button kind={:secondary} phx-click="dec" phx-target={@myself}>-</.button>
        <span class="qty-value">{@draft_qty}</span>
        <.button kind={:secondary} phx-click="inc" phx-target={@myself}>+</.button>
        <.button
          :if={@draft_qty != @item.qty}
          kind={:primary}
          phx-click="commit_qty"
          phx-target={@myself}
        >
          Update
        </.button>
      </div>
      <.button kind={:danger} phx-click="open_confirm" phx-target={@myself}>Remove</.button>

      <div :if={@confirm_open?} class="modal" role="dialog">
        <p>Remove {@item.name} from cart?</p>
        <.button kind={:danger} phx-click="confirm_remove" phx-target={@myself}>Yes</.button>
        <.button kind={:secondary} phx-click="cancel_confirm" phx-target={@myself}>Cancel</.button>
      </div>
    </div>
    """
  end
end

defmodule Main do
  def main do
    IO.puts("✓ Phoenix LiveComponent — Stateful, Stateless, and Function Components")
  - Demonstrating core concepts
    - Implementation patterns and best practices
  end
end

Main.main()
```
