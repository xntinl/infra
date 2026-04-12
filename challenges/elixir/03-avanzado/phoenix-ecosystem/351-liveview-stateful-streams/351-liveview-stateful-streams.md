# LiveView Stateful Components with Streams

**Project**: `feed_live` — a social feed rendering thousands of posts with constant server memory using LiveView streams and stateful function components.

## Project context

You are building the feed view of an internal social platform. The product manager wants "infinite scroll", the ability to prepend newly published posts in real time, and edit-in-place for the author. The team already tried a naive `assign(:posts, list)` implementation: a 5k-post list pushed 4MB of diff on every insert and the LiveView process stayed at 80MB per connected user. With 2k concurrent users the node ran out of memory in 20 minutes.

The fix is `Phoenix.LiveView.stream/3`. Streams move the source-of-truth out of the LiveView process: items are sent to the browser once, referenced by DOM id, and the server keeps no copy after the render. Pair streams with stateful function components (`Phoenix.LiveComponent`) to isolate re-renders: editing one post must not re-render all others.

```
feed_live/
├── lib/
│   ├── feed_live/
│   │   ├── application.ex
│   │   └── posts.ex
│   └── feed_live_web/
│       ├── endpoint.ex
│       ├── router.ex
│       ├── components/
│       │   └── post_component.ex
│       └── live/
│           └── feed_live.ex
├── test/
│   └── feed_live_web/
│       └── live/
│           └── feed_live_test.exs
├── bench/
│   └── stream_vs_assign_bench.exs
└── mix.exs
```

## Why streams and not `assign(:posts, list)`

`assign(:posts, list)` forces LiveView to keep the full list in the process heap. Every mutation produces a diff against the previous list; for N items the diff computation is O(N). On insert at head, LiveView re-sends the full list because positions shift.

A `stream` is a sparse reference the server hands the client; the client reconciles DOM nodes by id. The server does not store the items between renders. Diffing an insert is O(1): "prepend this one tuple". The server heap stays flat.

**Why not `phx-update="prepend"` with a plain list?** That was the idiom before LV 0.18. It still keeps the list on the server and leaks memory on reconnect (the whole list is re-rendered from scratch). Streams were introduced precisely to solve that.

## Core concepts

### 1. `stream/3` and `stream_insert/3`

`stream(socket, :posts, initial_items, opts)` registers a stream under the key `:posts`. `opts` accepts `:limit` (negative caps the oldest entries, positive caps the newest), `:reset` (replaces the entire stream), and `:at` (insert position; `0` is head, `-1` is tail).

### 2. DOM contract

Streams require the container to declare `phx-update="stream"` and each child to carry `id={id}`. LiveView injects synthetic ids as `"#{stream_name}-#{item_id}"`. Your items need a stable `id` field — usually the database primary key.

### 3. Stateful `LiveComponent` per row

A function component without `:id` is stateless: it re-renders when its parent renders. A `live_component` with `:id` is stateful: LiveView tracks it across renders and only invokes `update/2` when its assigns actually change. For 5k rows, this saves thousands of render calls on each message.

### 4. `stream_delete/3` vs `stream_delete_by_dom_id/3`

`stream_delete(socket, :posts, %Post{id: 7})` requires the full struct. `stream_delete_by_dom_id(socket, :posts, "posts-7")` only needs the DOM id — useful in handlers where you do not have the struct handy.

## Design decisions

- **Option A — one big LiveView with the list in `assign`**: simple, works for < 50 items. For anything scrolling, heap grows linearly per session.
- **Option B — stream + stateless row partials**: memory flat but row-level interactions (inline edit) re-render the whole feed.
- **Option C — stream + `LiveComponent` per row**: memory flat AND row edits are scoped. More code per row.

We choose Option C. The stateful component cost (one ETS-backed cid per row) is paid once; the render isolation is worth it.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule FeedLive.MixProject do
  use Mix.Project

  def project do
    [
      app: :feed_live,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [mod: {FeedLive.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_live_view, "~> 1.0"},
      {:phoenix_html, "~> 4.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"},
      {:floki, "~> 0.36", only: :test},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Context — `lib/feed_live/posts.ex`

```elixir
defmodule FeedLive.Posts do
  @moduledoc """
  Fake persistence layer. In production this would be Ecto.
  The important contract: every post has a stable integer id.
  """

  defstruct [:id, :author, :body, :inserted_at]

  def page(offset, limit) do
    for i <- (offset + 1)..(offset + limit) do
      %__MODULE__{
        id: i,
        author: "user_#{rem(i, 97)}",
        body: "Post number #{i}",
        inserted_at: DateTime.utc_now()
      }
    end
  end

  def new(id, body) do
    %__MODULE__{id: id, author: "me", body: body, inserted_at: DateTime.utc_now()}
  end
end
```

### Step 2: Row component — `lib/feed_live_web/components/post_component.ex`

```elixir
defmodule FeedLiveWeb.PostComponent do
  @moduledoc """
  Stateful row. Stores its own `editing?` flag so toggling edit
  mode on row 42 does not touch rows 41 or 43.
  """
  use Phoenix.LiveComponent

  @impl true
  def mount(socket) do
    {:ok, assign(socket, editing?: false, draft: nil)}
  end

  @impl true
  def update(%{post: post} = assigns, socket) do
    {:ok, socket |> assign(assigns) |> assign_new(:draft, fn -> post.body end)}
  end

  @impl true
  def handle_event("toggle-edit", _, socket) do
    {:noreply, update(socket, :editing?, &(not &1))}
  end

  def handle_event("save", %{"body" => body}, socket) do
    send(self(), {:post_updated, socket.assigns.post.id, body})
    {:noreply, assign(socket, editing?: false, draft: body)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <article id={@id} class="post">
      <header>{@post.author}</header>
      <%= if @editing? do %>
        <form phx-submit="save" phx-target={@myself}>
          <input name="body" value={@draft} />
          <button type="submit">Save</button>
        </form>
      <% else %>
        <p>{@post.body}</p>
        <button phx-click="toggle-edit" phx-target={@myself}>Edit</button>
      <% end %>
    </article>
    """
  end
end
```

### Step 3: Parent LiveView — `lib/feed_live_web/live/feed_live.ex`

```elixir
defmodule FeedLiveWeb.FeedLive do
  use Phoenix.LiveView

  alias FeedLive.Posts
  alias FeedLiveWeb.PostComponent

  @page_size 20

  @impl true
  def mount(_params, _session, socket) do
    initial = Posts.page(0, @page_size)

    socket =
      socket
      |> assign(offset: @page_size, next_id: 10_000)
      |> stream(:posts, initial, limit: -200)

    {:ok, socket}
  end

  @impl true
  def handle_event("load-more", _, socket) do
    next = Posts.page(socket.assigns.offset, @page_size)

    socket =
      socket
      |> update(:offset, &(&1 + @page_size))
      |> stream(:posts, next, at: -1)

    {:noreply, socket}
  end

  def handle_event("publish", %{"body" => body}, socket) do
    id = socket.assigns.next_id
    post = Posts.new(id, body)

    socket =
      socket
      |> update(:next_id, &(&1 + 1))
      |> stream_insert(:posts, post, at: 0)

    {:noreply, socket}
  end

  def handle_event("delete", %{"id" => dom_id}, socket) do
    {:noreply, stream_delete_by_dom_id(socket, :posts, dom_id)}
  end

  @impl true
  def handle_info({:post_updated, id, body}, socket) do
    updated = %Posts{id: id, author: "me", body: body, inserted_at: DateTime.utc_now()}
    {:noreply, stream_insert(socket, :posts, updated)}
  end

  @impl true
  def render(assigns) do
    ~H"""
    <section>
      <form phx-submit="publish">
        <input name="body" placeholder="What's on your mind?" />
        <button type="submit">Post</button>
      </form>

      <div id="posts" phx-update="stream">
        <.live_component
          :for={{dom_id, post} <- @streams.posts}
          module={PostComponent}
          id={dom_id}
          post={post}
        />
      </div>

      <button phx-click="load-more">Load more</button>
    </section>
    """
  end
end
```

### Step 4: Wire the router — `lib/feed_live_web/router.ex`

```elixir
defmodule FeedLiveWeb.Router do
  use Phoenix.Router
  import Phoenix.LiveView.Router

  pipeline :browser do
    plug :accepts, ["html"]
    plug :fetch_session
    plug :fetch_live_flash
    plug :put_root_layout, html: {FeedLiveWeb.Layouts, :root}
    plug :protect_from_forgery
  end

  scope "/", FeedLiveWeb do
    pipe_through :browser
    live "/", FeedLive
  end
end
```

## Why this works

The server never holds more than the currently-visible 200 items (capped by `limit: -200`). When `stream_insert(:posts, post, at: 0)` runs, LiveView sends `{"s": [[0, ["posts-10000", rendered_html]]]}` — a single tuple diff, not the whole list. `LiveComponent` keeps a per-row `cid`: its state (editing flag, draft) lives in a separate mailbox entry and updates propagate only to that component's iodata.

## Tests — `test/feed_live_web/live/feed_live_test.exs`

```elixir
defmodule FeedLiveWeb.FeedLiveTest do
  use ExUnit.Case, async: true
  import Phoenix.LiveViewTest

  @endpoint FeedLiveWeb.Endpoint

  setup do
    {:ok, conn: Phoenix.ConnTest.build_conn()}
  end

  describe "initial render" do
    test "renders the first page of posts", %{conn: conn} do
      {:ok, view, html} = live(conn, "/")
      assert html =~ "Post number 1"
      assert html =~ "Post number 20"
      refute html =~ "Post number 21"
      assert has_element?(view, "#posts-1")
    end
  end

  describe "pagination" do
    test "load-more appends at the tail", %{conn: conn} do
      {:ok, view, _} = live(conn, "/")
      view |> element("button", "Load more") |> render_click()
      assert has_element?(view, "#posts-21")
      assert has_element?(view, "#posts-40")
    end
  end

  describe "publishing" do
    test "a new post is prepended and isolated per component", %{conn: conn} do
      {:ok, view, _} = live(conn, "/")

      html =
        view
        |> form("form", %{"body" => "Hello stream"})
        |> render_submit()

      assert html =~ "Hello stream"
      assert has_element?(view, "#posts-10000")
    end
  end

  describe "deletion" do
    test "delete removes the row from the DOM", %{conn: conn} do
      {:ok, view, _} = live(conn, "/")
      render_hook(view, "delete", %{"id" => "posts-1"})
      refute has_element?(view, "#posts-1")
    end
  end
end
```

## Benchmark — `bench/stream_vs_assign_bench.exs`

```elixir
# Compare heap size after inserting 5_000 posts via assign vs stream.
# Run with: mix run bench/stream_vs_assign_bench.exs
defmodule Bench do
  def heap_words, do: Process.info(self(), :total_heap_size) |> elem(1)

  def assign_variant do
    list = for i <- 1..5_000, do: %{id: i, body: String.duplicate("x", 200)}
    :erlang.garbage_collect(self())
    before = heap_words()
    _ = Enum.reduce(1..200, list, fn i, acc -> [%{id: 10_000 + i, body: "new"} | acc] end)
    heap_words() - before
  end

  def stream_variant do
    # Simulates stream: server only keeps the latest diff, not the list.
    :erlang.garbage_collect(self())
    before = heap_words()
    _ = Enum.each(1..200, fn i -> send(self(), {:insert, i}) end)
    :timer.sleep(1)
    heap_words() - before
  end
end

IO.puts("assign delta (words):  #{Bench.assign_variant()}")
IO.puts("stream delta (words):  #{Bench.stream_variant()}")
```

**Expected**: stream delta < 500 words (flat), assign delta > 50_000 words (grows with list size).

## Trade-offs and production gotchas

**1. Items must have a stable `id`.** If you use `System.unique_integer/0` on render, the DOM ids drift and reconciliation breaks. Use the database primary key.

**2. `@streams.posts` is not enumerable outside templates.** You cannot call `Enum.count(@streams.posts)` in `handle_event`. If you need a count, track it in a separate assign (`assign(:total, n)`) and keep it in sync.

**3. `stream_configure` must run before the first `stream`.** Calling `stream_configure(:posts, dom_id: &"post-#{&1.id}")` after `stream/3` is a no-op. Configure in `mount/3` before the first insert.

**4. `limit: -N` silently drops oldest.** If your UX expects "scroll up to see older", a negative limit will evict them. Use positive limits when the head is the scroll anchor, negative when the tail is.

**5. `LiveComponent` id collisions across streams.** If two streams insert rows with the same DOM id prefix, their `cid` collides. Always namespace: `posts-#{id}`, `comments-#{id}`.

**6. When NOT to use streams.** If the list is small (< 100) and mutates rarely, `assign` is simpler and the diffs are cheap. Streams add a mental tax; reserve them for lists that grow.

## Reflection

You are asked to render a "last 3 notifications" dropdown. The list is always small, always prepends, and clears on click. Do you use `stream` or `assign`? Defend your choice against a reviewer who insists streams are "always better".

## Resources

- [Phoenix.LiveView.stream/3 — hexdocs](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveView.html#stream/4)
- [LiveView 0.18 release notes — introduction of streams](https://github.com/phoenixframework/phoenix_live_view/blob/main/CHANGELOG.md)
- [LiveComponent lifecycle — hexdocs](https://hexdocs.pm/phoenix_live_view/Phoenix.LiveComponent.html)
- [Chris McCord on streams (Dashbit blog)](https://dashbit.co/blog)
