# Registry and Named Processes: Building a Minimal Event PubSub

**Project**: `evt_pubsub` — a minimal publish/subscribe bus where subscribers
register themselves under a topic via `Registry` and receive events by message.

---

## Project structure

```
evt_pubsub/
├── lib/
│   └── evt_pubsub/
│       ├── application.ex
│       ├── bus.ex
│       └── subscriber.ex
├── script/
│   └── main.exs
├── test/
│   └── evt_pubsub/
│       ├── bus_test.exs
│       └── subscriber_test.exs
├── .formatter.exs
└── mix.exs
```

---

## Core concepts

**Process registration** is how you give a process a *name* you can look up
later, instead of passing pids around. Three levels of machinery exist on
the BEAM:

1. **Local names**: `Process.register(pid, :my_name)` — one process per atom,
   per node. Quick and dirty; not expressive enough for dynamic keys.

2. **`:via` tuples**: a standard protocol (`{:via, Module, term}`) that lets
   OTP (`GenServer`, `Agent`, `Task`) delegate name resolution to any module
   implementing the `:via` callbacks.

3. **`Registry`**: the standard-library module implementing the `:via`
   protocol on top of ETS. It supports two modes:
   - `:unique`  — one process per key (like a local name, but with any term).
   - `:duplicate` — many processes per key (perfect for pub/sub).

The essential insight: `Registry` stores `{key, pid, value}` tuples in ETS.
Lookups are O(1) and **truly concurrent** (no GenServer bottleneck). When a
registered process dies, its entries are removed automatically via
`Process.monitor/1` internally. That automatic cleanup is the single reason
people reach for `Registry` over a hand-rolled ETS table.

---

## The business problem

You're building an event bus for internal subsystems: a component emits
`{:order_created, order_id}`; any number of subscribers interested in the
`:orders` topic should receive the message. Requirements:

1. Publishing is fire-and-forget and O(subscribers).
2. Subscribers register themselves per topic, can subscribe to many topics,
   and are automatically cleaned up when they die.
3. No central bus process — publishing does not go through a bottleneck.

This is a stripped-down `Phoenix.PubSub`. Understanding this is how you
understand `Phoenix.PubSub`.

---

## Why Registry and not a single GenServer bus

- A GenServer-based bus serialises every publish through one mailbox — that mailbox is the bottleneck and a single point of crash for the whole bus.
- `:global` gives cluster scope but also locks and a single resolver process; overkill on one node and expensive for dynamic keys.
- A hand-rolled ETS table gives you the fast lookup but you then implement monitor-based cleanup yourself. `Registry` gives you that cleanup for free.

Registry is the correct primitive precisely because publish is O(subscribers) ETS reads plus direct `send/2` — no central process.

---

## Design decisions

**Option A — `:unique` keys + a separate "subscriber set" data structure per topic**
- Pros: one Registry entry per topic, lookup is O(1).
- Cons: you re-implement the subscriber set and its monitoring yourself; cleanup on subscriber crash becomes manual.

**Option B — `:duplicate` keys, one Registry entry per `{topic, subscriber}` pair** (chosen)
- Pros: Registry's built-in monitor removes the entry on subscriber death; `Registry.dispatch/3` iterates the set in ETS directly; no custom data structure.
- Cons: `subscribe/1` is not idempotent — a subscriber calling twice gets two entries and two deliveries.

→ Chose **B** because the whole point of using Registry is the automatic cleanup. Building on `:unique` forfeits that. Idempotency is a caller-side concern (dedupe before calling `subscribe/1`).

---

## Implementation

### Step 1: Create the project

**Objective**: scaffold a new Mix project and set up the directory layout for the exercise.

```bash
mix new evt_pubsub
cd evt_pubsub
```

### `mix.exs`
**Objective**: declare project metadata and dependencies required for the exercise.

```elixir
defmodule EvtPubsub.MixProject do
  use Mix.Project

  def project do
    [
      app: :evt_pubsub,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {EvtPubsub.Application, []}
    ]
  end

  defp deps do
    []
  end

end
```

### Step 3: `.formatter.exs`

**Objective**: configure the formatter inputs and line length for consistent code style.

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```

### `lib/evt_pubsub.ex`

```elixir
defmodule EvtPubsub do
  @moduledoc """
  Registry and Named Processes: Building a Minimal Event PubSub.

  - A GenServer-based bus serialises every publish through one mailbox — that mailbox is the bottleneck and a single point of crash for the whole bus.
  """
end
```

### `lib/evt_pubsub/application.ex`

**Objective**: implement application — starts the shared registry used as the pub/sub backbone.

```elixir
defmodule EvtPubsub.Application do
  @moduledoc """
  Starts the shared Registry used as the pub/sub backbone.

  NOTE: We are using `Application` only to boot the Registry. This is NOT an
  OTP Supervisor lesson — `Application.start/2` is the standard-library hook
  and `Registry.child_spec/1` handles everything. Supervisors as a topic
  are covered separately.
  """

  use Application

  @registry_name EvtPubsub.Registry

  @impl true
  def start(_type, _args) do
    children = [
      # `:duplicate` so many processes can register under the same topic key.
      # `partitions` scales writes across cores — the default is 1 partition,
      # which is fine for a teaching example.
      {Registry, keys: :duplicate, name: @registry_name}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EvtPubsub.Sup)
  end

  @doc "Returns the registry name so other modules don't hard-code it."
  def registry_name, do: @registry_name
end
```

### `lib/evt_pubsub/bus.ex`

**Objective**: implement bus — pub/sub api built on `registry` in `:duplicate` mode.

```elixir
defmodule EvtPubsub.Bus do
  @moduledoc """
  Pub/sub API built on `Registry` in `:duplicate` mode.

  Publishing does NOT go through a single process — every publisher looks
  up subscribers in ETS and sends directly. That's what makes Registry
  fundamentally different from a GenServer-based bus.
  """

  @registry EvtPubsub.Registry

  @doc """
  Subscribes the CURRENT process to `topic`.

  The `value` stored alongside the pid is arbitrary metadata — here we use it
  to tag subscribers so we could later filter by it. If duplicate subscribes
  from the same process should be idempotent, the caller must check first;
  `Registry.register/3` in `:duplicate` mode will happily create duplicates.
  """
  @spec subscribe(term(), term()) :: {:ok, pid()} | {:error, term()}
  def subscribe(topic, value \\ nil) do
    case Registry.register(@registry, topic, value) do
      {:ok, owner} -> {:ok, owner}
      {:error, _} = err -> err
    end
  end

  @doc """
  Unsubscribes the current process from `topic`.

  Normally you don't need this — when the subscriber process dies, Registry
  removes its entries automatically via an internal monitor. Explicit
  unsubscribe is only useful for long-lived processes that outlive a
  subscription.
  """
  @spec unsubscribe(term()) :: :ok
  def unsubscribe(topic) do
    Registry.unregister(@registry, topic)
  end

  @doc """
  Publishes `message` to every subscriber of `topic`.

  Uses `Registry.dispatch/3`, which runs the callback once per partition with
  the list of `{pid, value}` entries. `parallel: true` fans the callback out
  across schedulers — worth turning on when the subscriber list is large.
  """
  @spec publish(term(), term()) :: :ok
  def publish(topic, message) do
    Registry.dispatch(
      @registry,
      topic,
      fn entries ->
        # Iterate and `send/2` directly. Fire-and-forget — delivery is async
        # and unordered across subscribers (but ordered per subscriber).
        for {pid, _value} <- entries do
          send(pid, {:event, topic, message})
        end
      end,
      parallel: true
    )

    :ok
  end

  @doc """
  Returns the number of active subscribers for `topic`.

  O(1) via ETS; safe to call from any process.
  """
  @spec subscriber_count(term()) :: non_neg_integer()
  def subscriber_count(topic) do
    Registry.count_match(@registry, topic, :_)
  end

  @doc """
  Lists `{pid, value}` tuples for all subscribers of `topic`.

  Prefer `publish/2` or `subscriber_count/1` in normal code — exposing the
  subscriber list encourages callers to bypass the bus.
  """
  @spec subscribers(term()) :: [{pid(), term()}]
  def subscribers(topic) do
    Registry.lookup(@registry, topic)
  end
end
```

### `lib/evt_pubsub/subscriber.ex`

**Objective**: implement subscriber — a tiny spawn-based subscriber process.

```elixir
defmodule EvtPubsub.Subscriber do
  @moduledoc """
  A tiny spawn-based subscriber process.

  Subscribes to a topic, accumulates events in a list, and replies to a
  `{:dump, caller_ref}` probe with everything it has received so far. Useful
  in tests and as a reference implementation — production subscribers will
  usually be GenServers.
  """

  @doc """
  Spawns a subscriber linked to the caller. Returns its pid after it has
  finished registering — this avoids a race where the caller publishes
  before the subscribe has landed.
  """
  @spec start_link(term(), term()) :: pid()
  def start_link(topic, label \\ nil) do
    caller = self()

    pid =
      spawn_link(fn ->
        {:ok, _} = EvtPubsub.Bus.subscribe(topic, label)
        # Signal the caller that registration is complete before we start
        # consuming events. Without this the test could publish too early.
        send(caller, {:ready, self()})
        loop([])
      end)

    receive do
      {:ready, ^pid} -> pid
    after
      1_000 -> raise "subscriber failed to register in time"
    end
  end

  defp loop(events) do
    receive do
      {:event, _topic, _msg} = e ->
        loop([e | events])

      {:dump, caller, ref} ->
        send(caller, {ref, Enum.reverse(events)})
        loop(events)

      :stop ->
        :ok
    end
  end

  @doc """
  Synchronously requests the list of events the subscriber has seen.

  Uses the canonical `make_ref/0` + `^ref` correlation pattern — the only
  safe way to do request/response over raw message passing.
  """
  @spec dump(pid(), timeout()) :: {:ok, [term()]} | {:error, :timeout}
  def dump(pid, timeout \\ 500) do
    ref = make_ref()
    send(pid, {:dump, self(), ref})

    receive do
      {^ref, events} -> {:ok, events}
    after
      timeout -> {:error, :timeout}
    end
  end
end
```

### `test/evt_pubsub_test.exs`

**Objective**: write ExUnit tests covering happy paths and edge cases for the module.

```elixir
defmodule EvtPubsub.BusTest do
  # async: false — we mutate a shared Registry across tests.
  use ExUnit.Case, async: true
  doctest EvtPubsub.Subscriber

  alias EvtPubsub.Bus

  describe "core functionality" do
    test "subscribe/1 + publish/2 delivers the message to the caller" do
      {:ok, _} = Bus.subscribe(:topic_a)
      Bus.publish(:topic_a, {:hello, 1})

      assert_receive {:event, :topic_a, {:hello, 1}}, 200

      Bus.unsubscribe(:topic_a)
    end

    test "delivers to many subscribers for the same topic" do
      parent = self()

      # Spawn three processes that each forward events back to the test.
      pids =
        for i <- 1..3 do
          spawn_link(fn ->
            {:ok, _} = Bus.subscribe(:fanout, i)
            send(parent, {:ready, self()})

            receive do
              {:event, :fanout, msg} -> send(parent, {:got, i, msg})
            after
              1_000 -> :ok
            end
          end)
        end

      # Wait for all three to have registered before publishing.
      for pid <- pids, do: assert_receive({:ready, ^pid}, 500)

      Bus.publish(:fanout, :ping)

      assert_receive {:got, 1, :ping}, 500
      assert_receive {:got, 2, :ping}, 500
      assert_receive {:got, 3, :ping}, 500
    end

    test "subscriber_count/1 reflects active subscribers" do
      assert Bus.subscriber_count(:counts) == 0

      {:ok, _} = Bus.subscribe(:counts)
      assert Bus.subscriber_count(:counts) == 1

      Bus.unsubscribe(:counts)
      assert Bus.subscriber_count(:counts) == 0
    end

    test "dead subscribers are automatically removed" do
      topic = :cleanup

      pid =
        spawn(fn ->
          {:ok, _} = Bus.subscribe(topic)
          receive do: (:stop -> :ok)
        end)

      # Wait until the registration is visible.
      wait_until(fn -> Bus.subscriber_count(topic) == 1 end)

      Process.exit(pid, :kill)

      # Registry monitors subscribers and purges entries on :DOWN. This is the
      # property that makes Registry safe to use without manual cleanup code.
      wait_until(fn -> Bus.subscriber_count(topic) == 0 end)
    end

    test "unrelated topics don't see each other's messages" do
      {:ok, _} = Bus.subscribe(:topic_x)
      Bus.publish(:topic_y, :should_not_arrive)

      refute_receive {:event, :topic_y, _}, 100
      Bus.unsubscribe(:topic_x)
    end

    # Polls a predicate until it returns true or `deadline_ms` elapses.
    defp wait_until(fun, deadline_ms \\ 500, step_ms \\ 10) do
      cond do
        fun.() ->
          :ok

        deadline_ms <= 0 ->
          flunk("wait_until condition never became true")

        true ->
          Process.sleep(step_ms)
          wait_until(fun, deadline_ms - step_ms, step_ms)
      end
    end
  end
end
```

```elixir
defmodule EvtPubsub.SubscriberTest do
  use ExUnit.Case, async: true
  doctest EvtPubsub.Subscriber

  alias EvtPubsub.{Bus, Subscriber}

  describe "core functionality" do
    test "receives and records events from its topic" do
      pid = Subscriber.start_link(:orders, :tester)

      Bus.publish(:orders, {:order_created, 1})
      Bus.publish(:orders, {:order_created, 2})

      # Give Registry.dispatch a moment to deliver.
      Process.sleep(30)

      assert {:ok, events} = Subscriber.dump(pid)
      assert [{:event, :orders, {:order_created, 1}},
              {:event, :orders, {:order_created, 2}}] = events
    end

    test "ignores events from other topics" do
      pid = Subscriber.start_link(:billing)

      Bus.publish(:shipping, :not_for_us)
      Process.sleep(30)

      assert {:ok, []} = Subscriber.dump(pid)
    end
  end
end
```

### Step 8: Run

**Objective**: compile the project, run the tests, and verify the expected behavior.

```bash
mix deps.get
mix compile --warnings-as-errors
mix test
mix format
```

### Why this works

`Registry` is a named ETS table plus a monitor process. `register/3` inserts a `{key, pid, value}` row and monitors the pid; when the pid dies, the monitor callback deletes the row. `lookup/2` and `dispatch/3` read from ETS with concurrent-read semantics — no central process in the publish path. `parallel: true` on dispatch fans the callback out across scheduler partitions so subscriber lists don't serialise. The whole publish path is lock-free in the common case.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== EvtPubsub: demo ===\n")

    result_1 = EvtPubsub.Bus.subscriber_count(:counts)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = Mix.env()
    IO.puts("Demo 2: #{inspect(result_2)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create `lib/named_processes.ex` and test in `iex`:

```elixir
defmodule NamedProcesses do
  def start_registry do
    {:ok, _} = Registry.start_link(keys: :unique, name: :my_registry)
  end

  def start_worker(name) do
    pid = spawn(fn -> worker_loop() end)
    Registry.register(:my_registry, name, :metadata)
    pid
  end

  def send_message(name, msg) do
    case Registry.lookup(:my_registry, name) do
      [{pid, _metadata}] -> send(pid, msg); :ok
      [] -> {:error, :not_found}
    end
  end

  def get_all do
    Registry.select(:my_registry, [{{{:_, :"$1"}, :"$2", :_}, [], [{:"$1", :"$2"}]}])
  end

  defp worker_loop do
    receive do
      msg -> IO.inspect(msg); worker_loop()
      :stop -> :ok
    after
      5000 -> worker_loop()
    end
  end
end

# Test it
NamedProcesses.start_registry()

pid1 = NamedProcesses.start_worker(:alice)
pid2 = NamedProcesses.start_worker(:bob)

:ok = NamedProcesses.send_message(:alice, "Hello Alice")
:ok = NamedProcesses.send_message(:bob, "Hello Bob")

all = NamedProcesses.get_all()
IO.inspect(length(all))  # 2
```

## Benchmark

```elixir
# bench/pubsub.exs
for _ <- 1..1_000 do
  spawn_link(fn ->
    {:ok, _} = EvtPubsub.Bus.subscribe(:hot)
    receive do: (_ -> :ok)
  end)
end

Process.sleep(50)

{t, _} = :timer.tc(fn ->
  Enum.each(1..10_000, fn _ -> EvtPubsub.Bus.publish(:hot, :tick) end)
end)

IO.puts("10k publishes to 1000 subs: #{t} µs — #{t / 10_000} µs/publish")
```

Target: < 500 µs per publish with 1000 subscribers on modern hardware. The cost scales linearly with subscribers (one `send/2` each); the ETS lookup itself is O(1).

---

## Trade-off analysis

| Aspect | `Process.register/2` | `:global` | `Registry` |
|--------|----------------------|-----------|------------|
| Key type | Atom only | Any term | Any term |
| Scope | One node | Whole cluster | One node |
| Mode | Unique | Unique | `:unique` or `:duplicate` |
| Storage | VM internal | Locks + GenServer | ETS, concurrent reads |
| Pub/sub friendly? | No | No | Yes (`:duplicate`) |
| Auto-cleanup on death | Yes | Yes | Yes |

`Registry` is the right default on a single node for any case where the key
is not a compile-time atom. Cross-node pub/sub requires `Phoenix.PubSub`
or similar — don't try to build it on top of `:global`.

| Aspect | Registry-based bus | Single GenServer bus |
|--------|--------------------|-----------------------|
| Publish path | Publisher does `send/2` directly | Publisher `call`s the bus |
| Throughput | Scales with publishers | Bound by the bus mailbox |
| Ordering across subscribers | Not guaranteed | Single source of truth |
| Backpressure | None — fire-and-forget | Natural (call is sync) |

---

## Common production mistakes

**1. Using `:unique` mode for pub/sub.**
`Registry.register/3` in `:unique` mode returns `{:error, {:already_registered, pid}}`
the second time. For pub/sub you MUST use `:duplicate`.

**2. Publishing via the registry owner process.**
`Registry.dispatch/3` can be called from any process — it runs the callback
in the caller. Don't wrap it in a GenServer "just to feel safer"; that
reintroduces the bottleneck Registry was designed to eliminate.

**3. Doing heavy work inside the `dispatch` callback.**
Each subscriber should receive a message and return fast. If the callback
blocks (e.g. a synchronous `GenServer.call`), a slow subscriber stalls the
publisher. Send the event and move on.

**4. Forgetting to start the Registry as a child of the application.**
Calling `Registry.register/3` without first starting the named registry
raises `ArgumentError: unknown registry`. Always add it to your supervision
tree (here, via `EvtPubsub.Application`).

**5. Assuming cluster-wide delivery.**
Local `Registry` only sees processes on the current node. For distributed
pub/sub use `Phoenix.PubSub`, which multiplexes over `Registry` plus
distribution.

**6. Relying on message ordering across subscribers.**
`parallel: true` dispatches in parallel; even without it, `send/2` order
across different destinations is not a guarantee you should lean on.

---

## When NOT to use Registry

- You need cluster-wide lookup — use `:global`, Horde, or libcluster+Syn.
- You have exactly one well-known process — a local `register/2` is simpler.
- You need pub/sub across nodes — use `Phoenix.PubSub`.
- You need rich routing (pattern matching on events) — build on top of
  Registry or use a full event bus; don't abuse `:duplicate` mode.
- Keys are dynamically typed user input — validate them before registering,
  otherwise any caller can flood your registry with garbage keys.

---

## Reflection

- A topic has 10k subscribers and the publisher is publishing 1k events/sec. At what point does `parallel: true` stop helping — subscriber count, event rate, or scheduler count? How would you measure it?
- A slow subscriber turns its mailbox into a memory bomb. Registry offers no backpressure. What do you add — per-subscriber flow control, a buffer GenServer, or drop-on-full — and what does each cost?

---

## Resources

- [`Registry` — HexDocs](https://hexdocs.pm/elixir/Registry.html)
- [`Process` — HexDocs](https://hexdocs.pm/elixir/Process.html#register/2)
- [`:via` tuples — HexDocs](https://hexdocs.pm/elixir/GenServer.html#module-name-registration)
- [Phoenix.PubSub](https://hexdocs.pm/phoenix_pubsub/Phoenix.PubSub.html)
- [Elixir guide — Process Registry](https://hexdocs.pm/elixir/processes.html#registering-processes)

---

## Why Registry and Named Processes matters

Mastering **Registry and Named Processes** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. The Registry Allows Looking Up Processes by Name
`Registry.register` registers a process. `Registry.lookup` finds it by key. Without Registry, you'd need a global process dictionary or ETS table.

### 2. Unique Keys vs Duplicate Keys
Unique — one process per key (singleton resources). Duplicate — multiple processes per key (fan-out).

### 3. Metadata and Monitoring
Registry includes metadata and monitoring. This makes it powerful for dynamic systems where processes are created and destroyed frequently.

---
