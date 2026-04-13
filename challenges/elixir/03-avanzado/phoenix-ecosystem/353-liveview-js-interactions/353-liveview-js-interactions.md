# LiveView.JS for Client Interactions without Custom JavaScript

**Project**: `ui_kit` — a LiveView module demonstrating modal, dropdown, toast, and tab interactions driven entirely by `Phoenix.LiveView.JS` with no custom JavaScript.

## Project context

You inherit a LiveView app where every small interaction (open modal, close dropdown, fade toast) triggers a round trip to the server. Users on high-latency networks see 300ms UI lag. The team considered writing custom JS hooks, but there are 40+ interactions and maintaining them splits knowledge between Elixir and a `app.js` grab bag.

`Phoenix.LiveView.JS` solves this: it emits a serialized list of client-side commands (show/hide, toggle, dispatch, push, transition) that LV's JS runtime executes. No custom code. The commands compose — `JS.push("save") |> JS.hide(to: "#modal") |> JS.transition("fade-out", to: "#overlay")` executes in order on the client, with the server push interleaved.

```
ui_kit/
├── lib/
│   └── ui_kit_web/
│       ├── endpoint.ex
│       ├── router.ex
│       └── live/
│           └── gallery_live.ex
├── test/
│   └── ui_kit_web/
│       └── live/
│           └── gallery_live_test.exs
└── mix.exs
```

## Why LiveView.JS and not custom hooks

Custom `phx-hook` hooks live in a separate JS bundle; they require ESBuild, imports, mounted/updated lifecycle code, and manual DOM manipulation. They split your feature across two languages. Bugs happen at the boundary.

`LiveView.JS` is a declarative command list serialized as a JSON array. The built-in LV JS client interprets it. All of modal/dropdown/toast/tab patterns are covered by the primitives: `show`, `hide`, `toggle`, `add_class`, `remove_class`, `toggle_class`, `set_attribute`, `remove_attribute`, `dispatch`, `focus`, `push`, `transition`, `exec`.

**Why not Alpine.js?** Alpine is fine but it duplicates state that LiveView already tracks. Every `x-data` is a place where client and server can disagree. `JS` keeps a single source of truth.

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
### 1. Commands are data

`JS.show(to: "#modal")` returns `%Phoenix.LiveView.JS{ops: [["show", %{"to" => "#modal"}]]}`. Piping `JS` commands appends to `:ops`. The struct is rendered into `phx-click`, `phx-keydown`, `phx-window-keyup`, etc.

### 2. Selectors

`:to` accepts any CSS selector. `:to` relative selectors are also supported: `{:inner, "#modal .close"}` scopes to a subtree.

### 3. `JS.push` mixed with client-only ops

```elixir
JS.push("save", value: %{form_id: "signup"}) |> JS.hide(to: "#modal")
```

The client executes: (1) send `save` event with the value, (2) hide the modal. The hide is optimistic — it happens before the server replies. If the server crashes the event, the UI is already closed.

### 4. `JS.transition`

Adds a class for the duration of the animation, then removes it. Pairs with Tailwind classes like `transition-all duration-200`.

### 5. `JS.exec`

Fires another element's `data-show`, `data-hide`, etc. Useful for "clicking outside closes modal" — register `phx-click-away={JS.exec("data-cancel", to: "#modal")}`.

## Design decisions

- **Option A — all round trips to the server**: simplest, works offline. Cost: UI lag on every click.
- **Option B — custom Alpine components**: fast, but dual state model.
- **Option C — `Phoenix.LiveView.JS` for purely-client ops**: client-only for show/hide/toggle, server round trip only when state changes. One language, one mental model.

Chosen: Option C. Reserve round trips for operations that need server authority (save, delete, mutation of `:assign`).

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule UiKit.MixProject do
  use Mix.Project
  def project, do: [app: :ui_kit, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_live_view, "~> 1.0"},
      {:phoenix_html, "~> 4.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"},
      {:floki, "~> 0.36", only: :test}
    ]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
JS.push("save", value: %{form_id: "signup"}) |> JS.hide(to: "#modal")
```

The client executes: (1) send `save` event with the value, (2) hide the modal. The hide is optimistic — it happens before the server replies. If the server crashes the event, the UI is already closed.

### 4. `JS.transition`

Adds a class for the duration of the animation, then removes it. Pairs with Tailwind classes like `transition-all duration-200`.

### 5. `JS.exec`

Fires another element's `data-show`, `data-hide`, etc. Useful for "clicking outside closes modal" — register `phx-click-away={JS.exec("data-cancel", to: "#modal")}`.

## Design decisions

- **Option A — all round trips to the server**: simplest, works offline. Cost: UI lag on every click.
- **Option B — custom Alpine components**: fast, but dual state model.
- **Option C — `Phoenix.LiveView.JS` for purely-client ops**: client-only for show/hide/toggle, server round trip only when state changes. One language, one mental model.

Chosen: Option C. Reserve round trips for operations that need server authority (save, delete, mutation of `:assign`).

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule UiKit.MixProject do
  use Mix.Project
  def project, do: [app: :ui_kit, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_live_view, "~> 1.0"},
      {:phoenix_html, "~> 4.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"},
      {:floki, "~> 0.36", only: :test}
    ]
  end
end
```

### Step 1: The LiveView — `lib/ui_kit_web/live/gallery_live.ex`

**Objective**: Compose `Phoenix.LiveView.JS` commands server-side so modal, tab, and focus transitions run on the client without a socket round-trip.

```elixir
defmodule UiKitWeb.GalleryLive do
  use Phoenix.LiveView
  alias Phoenix.LiveView.JS

  @impl true
  def mount(_params, _session, socket) do
    {:ok, assign(socket, tab: "photos", saved?: false)}
  end

  @impl true
  def handle_event("change_tab", %{"tab" => tab}, socket) do
    {:noreply, assign(socket, tab: tab)}
  end

  def handle_event("save", %{"name" => name}, socket) do
    # Simulated persistence. Broadcast could go here in real code.
    {:noreply, assign(socket, saved?: true, saved_name: name)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div>
      <button phx-click={show_modal("modal-new")}>New item</button>

      <div id="modal-new" class="modal hidden" phx-click-away={hide_modal("modal-new")}
           phx-window-keydown={hide_modal("modal-new")} phx-key="escape">
        <form phx-submit={JS.push("save") |> hide_modal("modal-new")}>
          <input name="name" phx-mounted={JS.focus()} />
          <button type="submit">Save</button>
          <button type="button" phx-click={hide_modal("modal-new")}>Cancel</button>
        </form>
      </div>

      <div id="toast" class="toast hidden">Saved!</div>

      <nav>
        <button :for={t <- ~w(photos videos audio)}
                phx-click={JS.push("change_tab", value: %{tab: t})}>
          {String.capitalize(t)}
        </button>
      </nav>

      <section id={"panel-" <> @tab}>
        Current tab: {@tab}
      </section>

      <div :if={@saved?} id="flash-holder" phx-mounted={show_toast()}>
        {@saved_name} saved
      </div>
    </div>
    """
  end

  # --- JS command helpers ---------------------------------------------------

  defp show_modal(id) do
    JS.remove_class("hidden", to: "#" <> id)
    |> JS.transition({"ease-out duration-200", "opacity-0", "opacity-100"}, to: "#" <> id)
    |> JS.focus_first(to: "#" <> id)
  end

  defp hide_modal(id) do
    JS.transition({"ease-in duration-150", "opacity-100", "opacity-0"}, to: "#" <> id)
    |> JS.add_class("hidden", to: "#" <> id, transition: {"ease-in duration-150", "", ""})
  end

  defp show_toast do
    JS.remove_class("hidden", to: "#toast")
    |> JS.transition({"ease-out duration-200", "opacity-0", "opacity-100"}, to: "#toast")
    |> JS.add_class("hidden", to: "#toast", time: 3_000)
  end
end
```

## Why this works

Every `JS` helper returns a struct that `Phoenix.HTML` renders as a JSON string in the `phx-click` attribute. The LV JS client runtime parses and executes the ops in order. Animations use CSS transitions, not JS timers — the browser handles them at GPU-accelerated frame rate.

`phx-mounted={JS.focus()}` runs once when the element enters the DOM. That makes the modal auto-focus its first field without a timer.

`phx-key="escape"` plus `phx-window-keydown` gives global ESC-to-close without listening to every keypress on the document in custom JS.

## Tests — `test/ui_kit_web/live/gallery_live_test.exs`

```elixir
defmodule UiKitWeb.GalleryLiveTest do
  use ExUnit.Case, async: true
  import Phoenix.LiveViewTest
  @endpoint UiKitWeb.Endpoint

  setup do
    {:ok, conn: Phoenix.ConnTest.build_conn()}
  end

  describe "modal" do
    test "new-item button emits a show command", %{conn: conn} do
      {:ok, _view, html} = live(conn, "/")
      assert html =~ ~s(phx-click=)
      assert html =~ ~s("remove_class","hidden")
    end

    test "modal element starts hidden", %{conn: conn} do
      {:ok, _view, html} = live(conn, "/")
      assert html =~ ~s(id="modal-new" class="modal hidden")
    end
  end

  describe "tabs" do
    test "clicking a tab pushes change_tab", %{conn: conn} do
      {:ok, view, _} = live(conn, "/")
      render_click(view, "change_tab", %{"tab" => "videos"})
      assert render(view) =~ "Current tab: videos"
    end
  end

  describe "save flow" do
    test "submit assigns saved? true", %{conn: conn} do
      {:ok, view, _} = live(conn, "/")
      render_submit(view, "save", %{"name" => "sunset.jpg"})
      assert render(view) =~ "sunset.jpg saved"
    end
  end
end
```

## Benchmark

`JS` commands have zero runtime cost on the server — they are computed at render time and serialized once per mount. The benchmark here measures render iodata size.

```elixir
# bench/js_render_bench.exs
assigns = %{tab: "photos", saved?: false, saved_name: nil}

{us, iodata} =
  :timer.tc(fn ->
    Phoenix.HTML.Safe.to_iodata(UiKitWeb.GalleryLive.render(assigns))
  end)

bytes = IO.iodata_length(iodata)
IO.puts("render time: #{us}µs, iodata bytes: #{bytes}")
```

**Expected**: render < 200µs, iodata < 4 KB. If you see > 20 KB, you are probably embedding the same `JS` command in a loop — extract it to a partial.

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---


## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

**1. Optimistic hides mislead the user on server failures.** `JS.push("delete") |> JS.hide(to: "#row-7")` hides the row even if the server rejects the delete. Use `JS.push/2` with a loading state (`JS.add_class("loading", to: "#row-7")`) and remove it in the server handler on error.

**2. `JS.exec/2` selectors must survive re-renders.** If the target element is inside a LiveComponent that re-mounts, the `data-*` attribute may change. Prefer stable ids.

**3. `JS.transition` conflicts with CSS `transition-*` utilities.** If you also set `transition-all` in the class list, two animations race. Let `JS.transition` drive it.

**4. `phx-window-keydown` listens on every key.** Always pair with `phx-key="<specific>"` to avoid event storms.

**5. Serialized ops have a size limit.** The `phx-click` attribute is plain HTML — browsers accept very long values, but payloads > 16 KB hurt HTML compression. Split huge command chains into named partials.

**6. When NOT to use `JS`.** Complex state machines (a WYSIWYG editor, a drag-and-drop canvas) belong in a proper JS component. `JS` shines for show/hide/toggle/focus/tiny-transitions.

## Reflection

You are asked to implement "inline edit" on a table cell: click shows an input, Enter saves, Escape reverts. Sketch (in Elixir) the `JS` command chains for each of those three actions. Which of them need a server round trip and which are purely client?


## Executable Example

```elixir
defmodule UiKitWeb.GalleryLive do
  use Phoenix.LiveView
  alias Phoenix.LiveView.JS

  @impl true
  def mount(_params, _session, socket) do
    {:ok, assign(socket, tab: "photos", saved?: false)}
  end

  @impl true
  def handle_event("change_tab", %{"tab" => tab}, socket) do
    {:noreply, assign(socket, tab: tab)}
  end

  def handle_event("save", %{"name" => name}, socket) do
    # Simulated persistence. Broadcast could go here in real code.
    {:noreply, assign(socket, saved?: true, saved_name: name)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <div>
      <button phx-click={show_modal("modal-new")}>New item</button>

      <div id="modal-new" class="modal hidden" phx-click-away={hide_modal("modal-new")}
           phx-window-keydown={hide_modal("modal-new")} phx-key="escape">
        <form phx-submit={JS.push("save") |> hide_modal("modal-new")}>
          <input name="name" phx-mounted={JS.focus()} />
          <button type="submit">Save</button>
          <button type="button" phx-click={hide_modal("modal-new")}>Cancel</button>
        </form>
      </div>

      <div id="toast" class="toast hidden">Saved!</div>

      <nav>
        <button :for={t <- ~w(photos videos audio)}
                phx-click={JS.push("change_tab", value: %{tab: t})}>
          {String.capitalize(t)}
        </button>
      </nav>

      <section id={"panel-" <> @tab}>
        Current tab: {@tab}
      </section>

      <div :if={@saved?} id="flash-holder" phx-mounted={show_toast()}>
        {@saved_name} saved
      </div>
    </div>
    """
  end

  # --- JS command helpers ---------------------------------------------------

  defp show_modal(id) do
    JS.remove_class("hidden", to: "#" <> id)
    |> JS.transition({"ease-out duration-200", "opacity-0", "opacity-100"}, to: "#" <> id)
    |> JS.focus_first(to: "#" <> id)
  end

  defp hide_modal(id) do
    JS.transition({"ease-in duration-150", "opacity-100", "opacity-0"}, to: "#" <> id)
    |> JS.add_class("hidden", to: "#" <> id, transition: {"ease-in duration-150", "", ""})
  end

  defp show_toast do
    JS.remove_class("hidden", to: "#toast")
    |> JS.transition({"ease-out duration-200", "opacity-0", "opacity-100"}, to: "#toast")
    |> JS.add_class("hidden", to: "#toast", time: 3_000)
  end
end

defmodule Main do
  def main do
    IO.puts("✓ LiveView.JS for Client Interactions without Custom JavaScript")
  - Demonstrating core concepts
    - Implementation patterns and best practices
  end
end

Main.main()
```
