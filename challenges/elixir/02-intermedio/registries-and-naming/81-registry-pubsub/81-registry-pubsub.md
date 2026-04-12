# Local pub/sub with a duplicate-keyed Registry

**Project**: `local_pubsub` — a minimal in-process pub/sub bus built on `Registry` in `:duplicate` mode.

---

## Project context

You need a lightweight "publish to a topic, many listeners receive" primitive
for a single node — for example, to notify a pool of websocket handlers that
an entity changed, or to broadcast cache invalidations between a few
GenServers in the same app. You don't want Phoenix.PubSub (too heavy for
non-Phoenix apps) and you don't want your own ETS plumbing.

`Registry` in `:duplicate` mode is exactly this primitive. Each subscriber
registers under a topic key; publishing dispatches the message to every
registered process. It's a couple dozen lines and it scales well to
thousands of subscribers per topic on one node.

This is in fact how `Phoenix.PubSub.PG2` used to be built, and it's the
canonical "local pubsub" pattern in the Elixir ecosystem.

Project structure:

```
local_pubsub/
├── lib/
│   ├── local_pubsub.ex
│   └── local_pubsub/application.ex
├── test/
│   └── local_pubsub_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `:duplicate` keys = many pids per key

```
:unique     {"room:1", pid_a}                 one owner per key
:duplicate  {"room:1", pid_a}, {"room:1", pid_b}, {"room:1", pid_c}
```

A duplicate registry is a multi-map from key → `[{pid, value}]`. The same
pid can even register multiple entries under the same key — each is
independently removable. This is the shape you want for topic subscriptions.

### 2. `Registry.dispatch/3` — the fan-out primitive

```elixir
Registry.dispatch(MyReg, "topic", fn entries ->
  for {pid, _value} <- entries, do: send(pid, {:broadcast, msg})
end)
```

`dispatch/3` invokes your callback once per partition with the list of
`{pid, value}` entries for that key. Compared to `lookup/2` + manual
iteration, it's safer (no snapshot-then-send race) and it integrates with
`:partitions` to fan out in parallel.

### 3. The subscriber is the registered process

Whoever calls `Registry.register/3` is the subscriber — the pid stored is
`self()`. There is no "subscription object" to keep around: when the
subscriber dies, the registry removes the entry automatically. That's
a huge ergonomic win over ad-hoc lists of pids.

### 4. Values as filters / metadata

The third argument to `register/3` is arbitrary metadata stored alongside
the pid. Common uses: filter expressions, subscriber-type tags, or message
transforms applied before `send/2`. For a starter pubsub, pass `nil`.

---

## Why Registry `:duplicate` and not Phoenix.PubSub or a `GenServer` of subscribers

**Phoenix.PubSub.** Clustered, pluggable adapters (PG2, Redis), and integrates with Phoenix channels. Overkill when the whole system is a single node and you don't want Phoenix as a dependency.

**A `GenServer` holding a list of subscriber pids.** Serializes every publish through one mailbox, requires you to monitor and prune dead subscribers yourself, and does not parallelize fan-out.

**`Registry` (`:duplicate`) (chosen).** ETS-backed, zero bespoke cleanup code, `dispatch/3` fans out without the publisher holding a lock, and the same primitive scales into partitions when the subscriber count grows.

---

## Design decisions

**Option A — Roll your own `GenServer` subscriber list**
- Pros: Full control over dispatch order and filtering logic.
- Cons: You re-implement monitor-based cleanup, the publish path serializes through one process, and parallel fan-out is on you.

**Option B — `Registry` in `:duplicate` mode + `dispatch/3`** (chosen)
- Pros: Subscriptions clean up automatically when subscribers die; `dispatch/3` is partition-aware; scales to thousands of subscribers per topic; no extra process hop on publish.
- Cons: Delivery is best-effort `send/2` (no acks, no backpressure); every subscriber must match the envelope pattern.

→ Chose **B** because the auto-cleanup and non-serialized fan-out are load-bearing for a pubsub primitive, and the caveats are the same ones you accept with any in-BEAM pubsub.

---

## Implementation

### Step 1: Create the project

```bash
mix new local_pubsub --sup
cd local_pubsub
```

### Step 2: `lib/local_pubsub/application.ex`

```elixir
defmodule LocalPubsub.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # :duplicate lets the same topic have many subscribers.
      # No partitions here — a partitioned variant is a separate exercise.
      {Registry, keys: :duplicate, name: LocalPubsub.Registry}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: LocalPubsub.Supervisor)
  end
end
```

### Step 3: `lib/local_pubsub.ex`

```elixir
defmodule LocalPubsub do
  @moduledoc """
  A tiny local pub/sub bus backed by a `:duplicate`-keyed Registry.

  Subscribers register themselves under a topic; publishers call
  `broadcast/2`, which fans the message out to every subscriber's mailbox.
  Subscriptions are automatically cleaned up when subscribers exit.
  """

  @registry LocalPubsub.Registry

  @type topic :: String.t()
  @type message :: term()

  @doc """
  Subscribes the calling process to `topic`. The process will receive
  `{:pubsub, topic, message}` for every subsequent `broadcast/2`.

  Optional `filter` is an arbitrary metadata term stored with the entry —
  `broadcast/2` delivers it back so the subscriber can implement
  server-side filtering.
  """
  @spec subscribe(topic(), term()) :: :ok
  def subscribe(topic, filter \\ nil) when is_binary(topic) do
    {:ok, _} = Registry.register(@registry, topic, filter)
    :ok
  end

  @doc """
  Unsubscribes the calling process from `topic`. Does nothing if the
  caller was not subscribed.
  """
  @spec unsubscribe(topic()) :: :ok
  def unsubscribe(topic) when is_binary(topic) do
    Registry.unregister(@registry, topic)
  end

  @doc """
  Publishes `message` to everyone subscribed to `topic`. Returns the
  number of subscribers delivered to.

  We use `dispatch/3` instead of `lookup/2` + manual iteration because
  `dispatch/3` is partition-aware and does not materialize the full
  subscriber list when running against a partitioned registry.
  """
  @spec broadcast(topic(), message()) :: non_neg_integer()
  def broadcast(topic, message) when is_binary(topic) do
    counter = :counters.new(1, [:atomics])

    Registry.dispatch(@registry, topic, fn entries ->
      for {pid, _filter} <- entries do
        send(pid, {:pubsub, topic, message})
        :counters.add(counter, 1, 1)
      end
    end)

    :counters.get(counter, 1)
  end

  @doc """
  Returns the current number of subscribers for `topic`. Useful for
  debugging and for "nobody listening, skip the expensive payload" checks.
  """
  @spec subscribers(topic()) :: non_neg_integer()
  def subscribers(topic) when is_binary(topic) do
    length(Registry.lookup(@registry, topic))
  end
end
```

### Step 4: `test/local_pubsub_test.exs`

```elixir
defmodule LocalPubsubTest do
  use ExUnit.Case, async: false

  describe "subscribe/2 and broadcast/2" do
    test "delivers to one subscriber" do
      :ok = LocalPubsub.subscribe("news")
      assert LocalPubsub.broadcast("news", :hello) == 1
      assert_receive {:pubsub, "news", :hello}, 100
    end

    test "delivers to many subscribers" do
      parent = self()

      pids =
        for i <- 1..5 do
          spawn_link(fn ->
            LocalPubsub.subscribe("chatter")
            send(parent, {:ready, i})
            receive do
              {:pubsub, "chatter", msg} -> send(parent, {:got, i, msg})
            end
          end)
        end

      for i <- 1..5, do: assert_receive({:ready, ^i}, 200)

      assert LocalPubsub.broadcast("chatter", :ping) == 5
      for i <- 1..5, do: assert_receive({:got, ^i, :ping}, 200)

      Enum.each(pids, &Process.exit(&1, :normal))
    end

    test "does not deliver to unrelated topics" do
      :ok = LocalPubsub.subscribe("topic-a")
      assert LocalPubsub.broadcast("topic-b", :ignore) == 0
      refute_receive {:pubsub, _, _}, 50
    end
  end

  describe "unsubscribe/1" do
    test "stops delivery to the caller" do
      :ok = LocalPubsub.subscribe("once")
      :ok = LocalPubsub.unsubscribe("once")
      LocalPubsub.broadcast("once", :silence)
      refute_receive {:pubsub, _, _}, 50
    end
  end

  describe "auto-cleanup on subscriber death" do
    test "dead subscribers are dropped before the next broadcast" do
      parent = self()

      pid =
        spawn(fn ->
          LocalPubsub.subscribe("vanish")
          send(parent, :subscribed)
          receive do :stop -> :ok end
        end)

      assert_receive :subscribed, 200
      assert LocalPubsub.subscribers("vanish") == 1

      ref = Process.monitor(pid)
      send(pid, :stop)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}, 200

      # Registry cleanup is async; wait briefly.
      wait_until(fn -> LocalPubsub.subscribers("vanish") == 0 end)
    end
  end

  defp wait_until(fun, deadline \\ 500) do
    cond do
      fun.() -> :ok
      deadline <= 0 -> flunk("timeout")
      true -> (Process.sleep(10); wait_until(fun, deadline - 10))
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

### Why this works

`Registry` in `:duplicate` mode is a multi-map keyed by topic, stored in read-concurrent ETS and maintained by a monitor on every subscriber pid. `dispatch/3` walks the partition's entries and runs the fan-out callback without holding a lock on the registry, so the publisher never blocks registrations or other publishes. Because the subscriber pid is the unit of identity, dead subscribers are reaped without any bookkeeping on your part.

---

## Benchmark

```elixir
for i <- 1..1_000 do
  spawn(fn ->
    LocalPubsub.subscribe("bench")
    receive do _ -> :ok end
  end)
end

:timer.sleep(50)

{time, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000, fn _ -> LocalPubsub.broadcast("bench", :ping) end)
  end)

IO.puts("avg broadcast to 1k subs: #{time / 1_000} µs")
```

Target esperado: <200 µs por broadcast a 1k suscriptores locales (single partition, hardware moderno).

---

## Trade-offs and production gotchas

**1. Delivery is best-effort, not reliable**
`send/2` is a fire-and-forget over a local mailbox. If a subscriber is
slow, the BEAM will happily queue messages until it runs out of memory.
For reliable delivery you need acknowledgements, backpressure, or a real
message bus.

**2. Subscribers must handle the message pattern**
Every subscriber pid must have a `receive` or `handle_info` matching
`{:pubsub, topic, message}`. A GenServer without that clause will log a
warning on every broadcast. Document the envelope shape like any other API.

**3. No ordering guarantee across subscribers**
Two subscribers to the same topic may see messages in different orders
relative to each other (they won't reorder for a single subscriber from a
single publisher). Don't build CRDT-style invariants on the bus.

**4. One registry, many topics — not "one registry per topic"**
You only need a single `Registry` for the entire application; topics are
just keys. Starting a registry per topic is a common beginner mistake —
it serializes registration through the ALERT process and gives you nothing.

**5. `dispatch/3` runs in the caller process by default**
A slow callback blocks the publisher. If you're dispatching to thousands
of subscribers and doing non-trivial work per subscriber, use partitions
and pass `parallel: true` to fan the dispatch across scheduler cores.

**6. When NOT to use Registry pubsub**
Cross-node pubsub: use `Phoenix.PubSub` (clustered backends like `PG2`
or Redis) or `:pg`. Durable queues: use RabbitMQ, Kafka, or Broadway.
Registry pubsub is local, in-memory, and ephemeral — perfect for a single
node, wrong for anything else.

---

## Reflection

- Suppose your publisher produces 10k events/sec to a topic with 5k subscribers, and subscribers occasionally block on I/O. At what point does `send/2` fan-out stop being viable, and what replaces it — partitions with `parallel: true`, a bounded `GenStage`, or something else?
- How would you change the API (or the envelope) to support "replay the last N messages on subscribe" without retroactively making the bus stateful?

---

## Resources

- [`Registry` — duplicate keys and dispatch](https://hexdocs.pm/elixir/Registry.html#module-using-as-a-pubsub)
- [`Registry.dispatch/3`](https://hexdocs.pm/elixir/Registry.html#dispatch/4)
- [`Phoenix.PubSub`](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html) — what you want when local is not enough
- [José Valim — "Announcing Registry" (blog)](https://dashbit.co/blog/whats-new-in-elixir-1-4)
