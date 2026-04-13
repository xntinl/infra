# Phoenix.Presence Metas and CRDT Merge Semantics

**Project**: `presence_metas` — per-device presence with multi-tab aware merge/reduce.

---

## The business problem

You're adding a "who's here" widget to a SaaS dashboard. Users log in from a laptop
and also keep a tab open on their phone. The product expects that:

- A single "online" dot per user — regardless of how many tabs/devices they have open.
- The dot only turns off when **every** session closes.
- The tooltip lists every active device, its login timestamp, and the current page.
- The list of active pages per user is derived from all their sessions.

`Phoenix.Presence` solves this precisely — that's what *metas* are for. A presence
entry under a single `key` (the user id) can carry a list of metadata maps, one per
device. Presence's default `fetch/2` callback and an application-specific derivation
via `list/1` reduce that list into the shape the client wants.

The underlying machinery is a real CRDT (a convergent replicated data type). Every
node in the cluster carries its own slice of the presence state; periodic "gossip"
exchanges reconcile them. The merge is associative, commutative, and idempotent — so
convergence happens without a coordinator. This exercise walks through the surface API
**and** the CRDT guarantees that matter in production.

## Project structure

```
presence_metas/
├── lib/
│   └── presence_metas/
│       ├── application.ex
│       ├── endpoint.ex
│       ├── user_socket.ex
│       ├── presence.ex
│       └── channels/
│           └── dashboard_channel.ex
├── test/
│   └── presence_metas/
│       ├── presence_test.exs
│       └── channels/
│           └── dashboard_channel_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why Presence and not a custom GenServer

Presence across nodes needs conflict-free replication. Phoenix.Presence implements an ORSWOT-style CRDT and rides on Phoenix.PubSub. Rebuilding that is months of work for no gain.

---

## Design decisions

**Option A — custom GenServer tracking who is online**
- Pros: full control over state shape.
- Cons: distribution, CRDT conflict resolution, and netsplit recovery are your problem.

**Option B — Phoenix.Presence with per-connection metas** (chosen)
- Pros: distributed, CRDT-backed, netsplit-tolerant; metas carry per-connection state.
- Cons: eventual consistency; metadata size affects gossip cost.

→ Chose **B** because distributed presence with metadata is a textbook CRDT problem; Phoenix.Presence has solved it.

---

## Implementation

### `mix.exs`
```elixir
defmodule PresenceMetas.MixProject do
  use Mix.Project

  def project do
    [
      app: :presence_metas,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```elixir
defmodule PresenceMetas.MixProject do
  use Mix.Project

  def project do
    [app: :presence_metas, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [mod: {PresenceMetas.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:bandit, "~> 1.5"}
    ]
  end
end
```
### `lib/presence_metas.ex`

```elixir
defmodule PresenceMetas do
  @moduledoc """
  Phoenix.Presence Metas and CRDT Merge Semantics.

  Presence across nodes needs conflict-free replication. Phoenix.Presence implements an ORSWOT-style CRDT and rides on Phoenix.PubSub. Rebuilding that is months of work for no gain.
  """
end
```
### `lib/presence_metas/presence.ex`

**Objective**: Implement the module in `lib/presence_metas/presence.ex`.

```elixir
defmodule PresenceMetas.Presence do
  @moduledoc """
  Phoenix.Presence module with custom `fetch/2` to enrich metas.

  `list/1` returns a map of the form:

      %{
        "user_42" => %{
          metas: [%{device: ..., page: ..., online_at: ...}, ...],
          user: %{id: "user_42", display_name: "Alice", avatar: "..."},
          devices: ["laptop", "phone"],
          pages:   ["/dashboard", "/reports/17"]
        }
      }
  """

  use Phoenix.Presence,
    otp_app: :presence_metas,
    pubsub_server: PresenceMetas.PubSub

  @impl true
  def fetch(_topic, presences) do
    # In production this is where you'd batch-load users from the DB.
    # For the exercise we synthesize the profile deterministically.
    for {key, %{metas: metas}} <- presences, into: %{} do
      user = %{id: key, display_name: display_name_of(key)}

      devices =
        metas
        |> Enum.map(& &1.device)
        |> Enum.uniq()
        |> Enum.sort()

      pages =
        metas
        |> Enum.map(& &1.page)
        |> Enum.uniq()
        |> Enum.sort()

      {key, %{metas: metas, user: user, devices: devices, pages: pages}}
    end
  end

  defp display_name_of("user_" <> n), do: "User " <> n
  defp display_name_of(other), do: other
end
```
The `fetch/2` callback is the reduce step of the merge-reduce pattern. The tracker
gives us raw `metas`; `fetch/2` turns that into the domain-shaped object we want the
client to consume. Because it runs *per `list/1` call*, not per presence change, it
absorbs the cost of DB enrichment without slowing the gossip loop.

### `lib/presence_metas/channels/dashboard_channel.ex`

**Objective**: Implement the module in `lib/presence_metas/channels/dashboard_channel.ex`.

```elixir
defmodule PresenceMetas.Channels.DashboardChannel do
  @moduledoc """
  Dashboard channel. Each connected tab calls `track/3` once to register its
  meta. When the channel process dies (tab closed) the meta is pruned
  automatically.
  """

  use Phoenix.Channel

  alias PresenceMetas.Presence

  @impl true
  def join("dashboard:" <> workspace_id, params, socket) do
    user_id = Map.fetch!(params, "user_id")
    device = Map.fetch!(params, "device")
    page = Map.get(params, "page", "/")

    send(self(), :after_join)

    socket =
      socket
      |> assign(:workspace_id, workspace_id)
      |> assign(:user_id, user_id)
      |> assign(:device, device)
      |> assign(:page, page)

    {:ok, socket}
  end

  @impl true
  def handle_info(:after_join, socket) do
    {:ok, _ref} =
      Presence.track(self(), topic(socket), socket.assigns.user_id, %{
        device: socket.assigns.device,
        page: socket.assigns.page,
        online_at: System.system_time(:second)
      })

    push(socket, "presence_state", Presence.list(topic(socket)))
    {:noreply, socket}
  end

  @impl true
  def handle_in("navigate", %{"page" => page}, socket) do
    # Update the meta for *this* tab without dropping the presence.
    # `update/4` replaces the meta in-place while preserving the tracker ref.
    {:ok, _ref} =
      Presence.update(self(), topic(socket), socket.assigns.user_id, fn meta ->
        Map.put(meta, :page, page)
      end)

    {:reply, :ok, assign(socket, :page, page)}
  end

  defp topic(socket), do: "dashboard:" <> socket.assigns.workspace_id
end
```
`Presence.update/4` is the correct primitive when a device changes page. Using
`untrack + track` would emit a spurious `leaves` / `joins` diff that the UI would
interpret as "Alice went offline and came back online on `/billing`" — flickering the
online dot. `update/4` emits a single diff that keeps the ref stable.

### `lib/presence_metas/user_socket.ex`

**Objective**: Implement the module in `lib/presence_metas/user_socket.ex`.

```elixir
defmodule PresenceMetas.UserSocket do
  use Phoenix.Socket

  channel "dashboard:*", PresenceMetas.Channels.DashboardChannel

  @impl true
  def connect(_params, socket, _connect_info), do: {:ok, socket}

  @impl true
  def id(_socket), do: nil
end
```
### `lib/presence_metas/endpoint.ex`

**Objective**: Implement the module in `lib/presence_metas/endpoint.ex`.

```elixir
defmodule PresenceMetas.Endpoint do
  use Phoenix.Endpoint, otp_app: :presence_metas

  socket "/socket", PresenceMetas.UserSocket, websocket: true, longpoll: false
end
```
### `lib/presence_metas/application.ex`

**Objective**: Define the OTP application and supervision tree in `lib/presence_metas/application.ex`.

```elixir
defmodule PresenceMetas.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Phoenix.PubSub, name: PresenceMetas.PubSub},
      PresenceMetas.Presence,
      PresenceMetas.Endpoint
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: PresenceMetas.Supervisor)
  end
end
```
### `test/presence_metas_test.exs`

**Objective**: Add tests that cover the expected behavior and edge cases.

```elixir
# test/presence_metas/presence_test.exs
defmodule PresenceMetas.PresenceTest do
  use ExUnit.Case, async: true
  doctest PresenceMetas

  alias PresenceMetas.Presence

  @topic "dashboard:acme"

  setup do
    # Clean presence table between tests
    for {key, _} <- Presence.list(@topic) do
      for pid <- pids_for(@topic, key), do: Process.exit(pid, :kill)
    end

    Process.sleep(20)
    :ok
  end

  defp pids_for(topic, key) do
    Phoenix.Tracker.list(PresenceMetas.Presence, topic)
    |> Enum.filter(fn {k, _meta} -> k == key end)
    |> Enum.map(fn {_, %{phx_ref: _}} -> nil end)
    |> Enum.reject(&is_nil/1)
  end

  defp track_in_new_process(key, meta) do
    parent = self()

    pid =
      spawn(fn ->
        {:ok, _ref} = Presence.track(self(), @topic, key, meta)
        send(parent, :tracked)
        Process.sleep(:infinity)
      end)

    receive do
      :tracked -> :ok
    after
      500 -> flunk("tracker did not confirm within 500ms")
    end

    pid
  end

  describe "list/1 with custom fetch/2" do
    test "aggregates multiple metas per user" do
      laptop = track_in_new_process("user_42", %{device: "laptop", page: "/", online_at: 1})
      phone  = track_in_new_process("user_42", %{device: "phone",  page: "/r/7", online_at: 2})

      Process.sleep(50)

      assert %{"user_42" => entry} = Presence.list(@topic)
      assert length(entry.metas) == 2
      assert entry.devices == ["laptop", "phone"]
      assert entry.pages == ["/", "/r/7"]
      assert entry.user.display_name == "User 42"

      Process.exit(laptop, :kill)
      Process.exit(phone, :kill)
      Process.sleep(50)
    end

    test "pruning: killing a tracker removes its meta" do
      laptop = track_in_new_process("user_99", %{device: "laptop", page: "/", online_at: 1})
      phone  = track_in_new_process("user_99", %{device: "phone",  page: "/x", online_at: 2})

      Process.sleep(50)
      assert %{"user_99" => %{metas: metas}} = Presence.list(@topic)
      assert length(metas) == 2

      Process.exit(laptop, :kill)
      Process.sleep(80)

      assert %{"user_99" => %{metas: metas_after}} = Presence.list(@topic)
      assert length(metas_after) == 1
      assert hd(metas_after).device == "phone"

      Process.exit(phone, :kill)
      Process.sleep(80)

      refute Map.has_key?(Presence.list(@topic), "user_99")
    end
  end

  describe "CRDT idempotence" do
    test "the list is stable across repeated reads" do
      pid = track_in_new_process("user_1", %{device: "laptop", page: "/", online_at: 1})
      Process.sleep(50)

      snap1 = Presence.list(@topic)
      snap2 = Presence.list(@topic)
      snap3 = Presence.list(@topic)

      assert snap1 == snap2
      assert snap2 == snap3

      Process.exit(pid, :kill)
      Process.sleep(50)
    end
  end
end
```
```elixir
# test/presence_metas/channels/dashboard_channel_test.exs
defmodule PresenceMetas.Channels.DashboardChannelTest do
  use ExUnit.Case, async: true
  doctest PresenceMetas
  import Phoenix.ChannelTest

  @endpoint PresenceMetas.Endpoint

  alias PresenceMetas.{Presence, UserSocket}

  defp join_as(user_id, device, page \\ "/") do
    {:ok, socket} = connect(UserSocket, %{})

    {:ok, _, socket} =
      subscribe_and_join(socket, "dashboard:acme", %{
        "user_id" => user_id,
        "device" => device,
        "page" => page
      })

    socket
  end

  describe "core functionality" do
    test "presence_state pushed on join" do
      _ = join_as("user_1", "laptop", "/")
      assert_push "presence_state", state
      assert Map.has_key?(state, "user_1")
    end

    test "navigate updates the meta in place" do
      socket = join_as("user_1", "laptop", "/")
      assert_push "presence_state", _state

      ref = push(socket, "navigate", %{"page" => "/billing"})
      assert_reply ref, :ok

      # Tracker gossip window
      Process.sleep(50)

      assert %{"user_1" => %{metas: [meta]}} = Presence.list("dashboard:acme")
      assert meta.page == "/billing"
    end
  end
end
```
### Why this works

Each node tracks its own users via `Phoenix.Tracker`. Nodes gossip state deltas using a CRDT that converges regardless of message order. Metas are per-connection data merged into the user's presence record.

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

**1. Metas should be small.** Every meta is gossiped across the cluster on every
change. A 2 KB meta times 10k users times 5 nodes = 100 MB per full reconciliation.
Keep metas to the bare minimum; enrich in `fetch/2` (which is local).

**2. `fetch/2` runs per-call, not per-change.** That's both a blessing (no hot-path
cost) and a curse (you do the DB work every time the client calls `list/1`). If your
dashboard polls presence every second, cache the enrichment — ETS keyed on the user_id,
invalidated on user update.

**3. Key = user_id, NOT session_id.** A common bug: people use `session_id` as the
presence key. Now opening two tabs creates two entries for the same human, and the
dedupe you wanted disappears. The key is the identity; metas are per-device. 

**4. `update/4` vs `untrack + track`.** `update/4` preserves the tracker ref and emits
a clean diff; the sequence `untrack + track` emits a leave then a join, which the
client renders as a flicker. Always `update/4` when mutating an existing meta.

**5. Presence is per-node, gossip is eventual.** Immediately after a `track/3` call,
local `list/1` will see the entry; another node may see it 1–3 seconds later (the
`:broadcast_period` default). If you expect "join + immediate list on another node"
to include the new entry, you'll be surprised. Don't test that from a different node.

**6. Cluster partitions do not heal metas automatically.** If node A goes down while
hosting alice's laptop presence, other nodes will notice after `max_silent_periods` and
prune alice on their side. If node A comes back with the pid still alive, it re-gossips
and alice reappears. This is the CRDT healing itself — but there's a window of tens of
seconds where the cluster disagrees. If your product cannot tolerate that, don't use
Presence; use a database.

**7. `handle_metas/4` lives in the tracker process.** Slow work there stalls all
presence updates for the node. Fan out to a Task.Supervisor or a separate GenServer
if you need to do meaningful work on every change.

**8. When NOT to use this.** If your "online" status has to survive a node dying
(e.g., billing-relevant seat-counting), Presence is the wrong tool — the metas live in
memory only. Persist to Postgres with a last-seen-at timestamp and use Presence as a
UX hint layered on top, not as the source of truth.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```
Target: presence update propagates cluster-wide in tens of ms under normal load; metas add <1 KB per connection.

---

## Reflection

- Your metas grow to 20 KB per connection. What breaks first, and how do you keep the same visible behavior while shrinking the payload?
- During a netsplit, two halves show disjoint users. What does Presence do on heal, and is 'last writer wins' the right policy for your domain?

---

### `script/main.exs`
```elixir
defmodule PresenceMetas.MixProject do
  use Mix.Project

  def project do
    [app: :presence_metas, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [mod: {PresenceMetas.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:bandit, "~> 1.5"}
    ]
  end
end

**Objective**: Implement the module in `lib/presence_metas/presence.ex`.

The `fetch/2` callback is the reduce step of the merge-reduce pattern. The tracker
gives us raw `metas`; `fetch/2` turns that into the domain-shaped object we want the
client to consume. Because it runs *per `list/1` call*, not per presence change, it
absorbs the cost of DB enrichment without slowing the gossip loop.

**Objective**: Implement the module in `lib/presence_metas/channels/dashboard_channel.ex`.

`Presence.update/4` is the correct primitive when a device changes page. Using
`untrack + track` would emit a spurious `leaves` / `joins` diff that the UI would
interpret as "Alice went offline and came back online on `/billing`" — flickering the
online dot. `update/4` emits a single diff that keeps the ref stable.

**Objective**: Implement the module in `lib/presence_metas/user_socket.ex`.

**Objective**: Implement the module in `lib/presence_metas/endpoint.ex`.

**Objective**: Define the OTP application and supervision tree in `lib/presence_metas/application.ex`.

**Objective**: Add tests that cover the expected behavior and edge cases.

defmodule Main do
  def main do
      # Demonstrating 221-presence-metas
      :ok
  end
end

Main.main()
```
---

## Why Phoenix.Presence Metas and CRDT Merge Semantics matters

Mastering **Phoenix.Presence Metas and CRDT Merge Semantics** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts

### 1. Presence shape: key → list of metas

Phoenix.Presence stores state as:

```
%{
  "user_42" => %{
    metas: [
      %{device: "laptop", page: "/dashboard",  online_at: 1_712_000_000},
      %{device: "phone",  page: "/reports/17", online_at: 1_712_000_100}
    ]
  },
  "user_7" => %{
    metas: [
      %{device: "laptop", page: "/billing", online_at: 1_712_000_200}
    ]
  }
}
```

`key` is application-defined — typically the user id. The meta list is append-only
per `track/3` call and prune-on-exit when the tracked pid dies.

### 2. One `track/3` per process

Each call to `Phoenix.Presence.track(pid, topic, key, meta)` registers one meta entry
that stays alive as long as `pid` stays alive. When the channel process exits, that
meta is pruned automatically. To have two metas for one key, you need two processes
calling `track/3` (typically two separate browser tabs each with their own channel pid).

### 3. The CRDT layer underneath

Presence builds on **Phoenix.Tracker**, which implements a delta-CRDT variant of an
OR-Set (observed-remove set). Properties that matter:

- **Associative**: `merge(merge(a, b), c) == merge(a, merge(b, c))`.
- **Commutative**: `merge(a, b) == merge(b, a)`.
- **Idempotent**: `merge(a, a) == a`.

These three properties mean every node can gossip partial updates in any order and
still converge on the same final state. No distributed lock, no consensus protocol.
The gossip interval is controlled by `:broadcast_period` (default 1.5s) and
`:max_silent_periods` (default 2 — after 3s of silence a node is considered down).

### 4. `fetch/2` and `handle_metas/4`

- **`fetch/2`** runs in the process that called `list/1`. It's a place to **enrich**
  the metas with data from the DB (e.g., the user's avatar URL) without blocking the
  tracker process. Returned shape: `%{key => %{metas: metas, extra_fields...}}`.
- **`handle_metas/4`** runs in the tracker process and receives
  `{joins, leaves}` diffs on every presence change. Use it to broadcast reduced
  snapshots (e.g., "user X is now on `/billing`") instead of raw meta lists.

### 5. The join/leave diff

When a channel calls `Phoenix.Channel.push(socket, "presence_state", Presence.list(topic))`
on join, the client sees the full snapshot. Subsequent changes arrive as `presence_diff`
events with `%{joins: ..., leaves: ...}`. The *joins* key for `user_42` when they open
a new tab will show the **new meta only**, not the full list — the client is expected
to accumulate joins/leaves on its end. The canonical Phoenix JS helper `Presence.syncDiff`
does exactly this merge.

---
