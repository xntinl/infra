# Event Sourcing + CQRS Framework

**Project**: `eventsource` — a generic event sourcing and CQRS framework with snapshotting and process managers

---

## Project context

You are building `eventsource`, a framework the domain team will use to build write-heavy, audit-trail-required systems. The state of every aggregate is derived by replaying an append-only event log. Reads come from projections (denormalized read models) updated asynchronously. Process managers (sagas) coordinate multi-aggregate workflows with compensation.

Project structure:

```
eventsource/
├── lib/
│   └── eventsource/
│       ├── application.ex
│       ├── store/
│       │   ├── event_store.ex       # ← append-only log, optimistic locking
│       │   └── snapshot_store.ex    # ← state snapshots for fast replay
│       ├── aggregate.ex             # ← behaviour: init/0, apply/2, handle/2
│       ├── command_handler.ex       # ← load → replay → handle → append → publish
│       ├── event_bus.ex             # ← pubsub for projections + process managers
│       ├── projection.ex            # ← behaviour: handle_event/2, read model in ETS
│       ├── process_manager.ex       # ← behaviour: handle_event/2 → emit commands
│       ├── snapshot.ex              # ← snapshot trigger + transparent replay
│       └── upcaster.ex              # ← event schema migration: v1 → v2
├── test/
│   └── eventsource/
│       ├── event_store_test.exs
│       ├── aggregate_test.exs
│       ├── command_handler_test.exs
│       ├── projection_test.exs
│       ├── snapshot_test.exs
│       └── process_manager_test.exs
├── bench/
│   └── replay_bench.exs
└── mix.exs
```

---

## The business problem

The accounting team needs a complete audit trail — every state change must be attributable to a specific command from a specific user at a specific time. Traditional CRUD databases overwrite state. Event sourcing never overwrites: every change is an appended event. The current state is derived by replaying events, and any past state is available by replaying up to a given sequence number.

Two trade-offs shape every architectural decision:

1. **Eventual consistency in projections** — commands write events; projections update asynchronously. Queries may return data that is milliseconds behind writes. This is acceptable in most domains and enables independent scaling of reads and writes.
2. **Event versioning complexity** — once an event schema is deployed and events are persisted, changing the schema requires an upcasting layer. Events from 3 years ago must still be replayable by current aggregates.

---

## Why optimistic locking without database transactions

Traditional relational databases use `BEGIN TRANSACTION; UPDATE; COMMIT` for concurrency control. An event store has no mutable rows to lock. Instead, it uses optimistic concurrency:

- Each stream has a `version` (sequence number of the last event).
- `append(stream_id, events, expected_version)` checks: if current version == expected_version, append; otherwise return `{:error, :version_conflict}`.
- The command handler retries on conflict by replaying the aggregate state again with the new events and re-executing the command.

This is optimistic because conflicts are expected to be rare. Under high contention on the same aggregate, retry rates increase — a signal that you need to partition the aggregate differently.

---

## Implementation

### Step 1: Create the project

```bash
mix new eventsource --sup
cd eventsource
mkdir -p lib/eventsource/store
mkdir -p test/eventsource bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/eventsource/store/event_store.ex`

```elixir
defmodule Eventsource.Store.EventStore do
  use GenServer

  @moduledoc """
  Append-only event log, partitioned by stream_id.

  Each stream is an ordered list of events with consecutive sequence numbers
  starting at 0. Appending to a stream requires specifying the expected version
  (the sequence number of the last known event, or -1 for new streams).

  Optimistic locking: if the expected version does not match the actual version
  at append time, return {:error, :version_conflict}. This prevents two concurrent
  command handlers from both appending to the same aggregate simultaneously.

  Persistence: uses DETS for durability. The table is a set with key = {stream_id, seq}.
  DETS provides O(1) point lookup and ordered range scans.

  Design question: why per-{stream_id, seq} keys rather than per-stream keys?
  With per-stream keys ({stream_id => [events]}), reading 1 event from a stream
  with 10k events loads all 10k events into memory. Per-event keys enable streaming
  reads and selective loading for snapshotted aggregates.
  """

  @table :eventsource_event_store

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Appends events to a stream with optimistic locking.
  Returns {:ok, new_version} or {:error, :version_conflict}.
  """
  @spec append(String.t(), [map()], integer()) :: {:ok, integer()} | {:error, :version_conflict}
  def append(stream_id, events, expected_version) do
    GenServer.call(__MODULE__, {:append, stream_id, events, expected_version})
  end

  @doc """
  Reads events from a stream, optionally starting from a specific sequence number.
  Returns a list of %{stream_id, seq, event_type, payload, timestamp, metadata}.
  """
  @spec read_stream(String.t(), non_neg_integer()) :: [map()]
  def read_stream(stream_id, from_seq \\ 0) do
    # Read directly from DETS — no GenServer call needed for reads
    # TODO: :dets.match_object(@table, {{stream_id, :"$1"}, :"$2"})
    # TODO: filter seq >= from_seq and sort by seq
    []
  end

  @doc """
  Returns the current version (last sequence number) of a stream.
  Returns -1 for a new (non-existent) stream.
  """
  @spec stream_version(String.t()) :: integer()
  def stream_version(stream_id) do
    # TODO: :dets.match(@table, {{stream_id, :"$1"}, :_}) |> Enum.max(fn -> -1 end)
    -1
  end

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    path = Keyword.get(opts, :path, 'event_store.dets')
    {:ok, _} = :dets.open_file(@table, type: :set, file: path)
    {:ok, %{table: @table}}
  end

  @impl true
  def handle_call({:append, stream_id, events, expected_version}, _from, state) do
    current_version = stream_version(stream_id)

    if current_version != expected_version do
      {:reply, {:error, :version_conflict}, state}
    else
      ts = System.system_time(:millisecond)
      new_events =
        Enum.with_index(events, current_version + 1)
        |> Enum.map(fn {event, seq} ->
          entry = %{
            stream_id: stream_id,
            seq: seq,
            event_type: event.type,
            payload: event.payload,
            timestamp: ts,
            metadata: Map.get(event, :metadata, %{})
          }
          {{stream_id, seq}, entry}
        end)

      # TODO: :dets.insert(@table, new_events)
      # TODO: publish events to EventBus
      new_version = current_version + length(events)
      {:reply, {:ok, new_version}, state}
    end
  end
end
```

### Step 4: `lib/eventsource/aggregate.ex`

```elixir
defmodule Eventsource.Aggregate do
  @moduledoc """
  Behaviour for event-sourced aggregates.

  An aggregate:
  1. Has an initial state (`init/0`)
  2. Handles commands (`handle/2`) → produces events (does NOT mutate state)
  3. Applies events to state (`apply/2`) → produces new state

  The framework:
  1. Loads events from the event store for this aggregate's stream
  2. Applies events in order using `apply/2` to rebuild the current state
  3. Calls `handle/2` with the command
  4. Appends the resulting events to the stream (with version check)
  5. Publishes events to the event bus

  Why does `handle/2` not mutate state?
  If `handle/2` mutated state directly, we could not derive the state from events.
  The events are the truth; the state is derived. Keeping them separate guarantees
  that replaying events from any point always produces the correct state.
  """

  @callback init() :: term()
  @callback apply(state :: term(), event :: map()) :: term()
  @callback handle(state :: term(), command :: map()) ::
    {:ok, [map()]} | {:error, term()}

  defmacro __using__(_opts) do
    quote do
      @behaviour Eventsource.Aggregate

      def load(aggregate_id) do
        Eventsource.CommandHandler.load_aggregate(__MODULE__, aggregate_id)
      end
    end
  end
end
```

### Step 5: `lib/eventsource/command_handler.ex`

```elixir
defmodule Eventsource.CommandHandler do
  @moduledoc """
  Orchestrates the command execution lifecycle.

  Flow:
  1. Determine the aggregate stream_id from the command
  2. Check for a snapshot (fast path)
  3. Load and replay events from the event store (from snapshot version if snapshot exists)
  4. Call aggregate.handle/2 with the current state and command
  5. Append new events with expected_version check (retry on conflict)
  6. Publish new events to the event bus

  The handler is stateless — it does not hold aggregate state between commands.
  Each command execution is a fresh load-replay-handle-append cycle.
  Using GenServer per aggregate (cached in a DynamicSupervisor) is an optimization:
  the aggregate state is cached in the process, avoiding full replay on every command.
  Start with stateless for correctness, optimize with caching later.
  """

  @max_retries 3

  @spec execute(module(), String.t(), map(), non_neg_integer()) ::
    {:ok, [map()]} | {:error, term()}
  def execute(aggregate_module, aggregate_id, command, retry \\ 0) do
    with {:ok, state, version} <- load_aggregate(aggregate_module, aggregate_id),
         {:ok, events} <- aggregate_module.handle(state, command),
         {:ok, _new_version} <- Eventsource.Store.EventStore.append(aggregate_id, events, version) do
      # TODO: publish events to EventBus
      {:ok, events}
    else
      {:error, :version_conflict} when retry < @max_retries ->
        # Reload state and retry — another command was applied concurrently
        execute(aggregate_module, aggregate_id, command, retry + 1)

      error ->
        error
    end
  end

  @doc "Loads and replays an aggregate from the event store (or snapshot)."
  @spec load_aggregate(module(), String.t()) :: {:ok, term(), integer()}
  def load_aggregate(aggregate_module, aggregate_id) do
    # Check snapshot store first
    {base_state, from_seq} =
      case Eventsource.Store.SnapshotStore.get(aggregate_id) do
        {:ok, %{state: state, version: v}} -> {state, v + 1}
        :not_found -> {aggregate_module.init(), 0}
      end

    events = Eventsource.Store.EventStore.read_stream(aggregate_id, from_seq)

    final_state =
      Enum.reduce(events, base_state, fn event, state ->
        # Apply upcasting before passing to aggregate
        upcasted_event = Eventsource.Upcaster.upcast(event)
        aggregate_module.apply(state, upcasted_event)
      end)

    version = Eventsource.Store.EventStore.stream_version(aggregate_id)
    {:ok, final_state, version}
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/eventsource/event_store_test.exs
defmodule Eventsource.Store.EventStoreTest do
  use ExUnit.Case, async: false

  alias Eventsource.Store.EventStore

  setup do
    # Use a temp file for each test
    :ok
  end

  test "appends to a new stream starting at version -1" do
    events = [%{type: :created, payload: %{name: "Alice"}}]
    assert {:ok, 0} = EventStore.append("users-1", events, -1)
  end

  test "version conflict when expected_version is wrong" do
    EventStore.append("users-2", [%{type: :created, payload: %{}}], -1)
    assert {:error, :version_conflict} =
      EventStore.append("users-2", [%{type: :updated, payload: %{}}], -1)
  end

  test "reads events in order" do
    EventStore.append("users-3", [%{type: :created, payload: %{name: "Bob"}}], -1)
    EventStore.append("users-3", [%{type: :name_changed, payload: %{name: "Robert"}}], 0)

    events = EventStore.read_stream("users-3")
    assert length(events) == 2
    assert Enum.at(events, 0).event_type == :created
    assert Enum.at(events, 1).event_type == :name_changed
  end

  test "read_stream from_seq skips earlier events" do
    EventStore.append("users-4", [%{type: :created, payload: %{}}], -1)
    EventStore.append("users-4", [%{type: :updated, payload: %{}}], 0)

    events = EventStore.read_stream("users-4", 1)
    assert length(events) == 1
    assert hd(events).seq == 1
  end
end
```

```elixir
# test/eventsource/aggregate_test.exs
defmodule Eventsource.AggregateTest do
  use ExUnit.Case, async: false

  # Define a test aggregate
  defmodule Counter do
    use Eventsource.Aggregate

    def init, do: %{count: 0, id: nil}

    def apply(state, %{event_type: :incremented, payload: %{by: n}}) do
      %{state | count: state.count + n}
    end

    def apply(state, %{event_type: :created, payload: %{id: id}}) do
      %{state | id: id}
    end

    def handle(_state, %{type: :increment, by: n}) when n > 0 do
      {:ok, [%{type: :incremented, payload: %{by: n}}]}
    end

    def handle(_state, %{type: :increment}) do
      {:error, :invalid_amount}
    end
  end

  test "command produces events that update state" do
    {:ok, events} = Counter.handle(%{count: 5, id: "c1"}, %{type: :increment, by: 3})
    assert [%{type: :incremented, payload: %{by: 3}}] = events

    new_state = Counter.apply(%{count: 5, id: "c1"}, %{event_type: :incremented, payload: %{by: 3}})
    assert new_state.count == 8
  end

  test "full lifecycle: execute command and reload" do
    {:ok, _} = Eventsource.CommandHandler.execute(Counter, "counter-1", %{type: :increment, by: 5})
    {:ok, _} = Eventsource.CommandHandler.execute(Counter, "counter-1", %{type: :increment, by: 3})

    {:ok, state, _version} = Eventsource.CommandHandler.load_aggregate(Counter, "counter-1")
    assert state.count == 8
  end
end
```

### Step 7: Run the tests

```bash
mix test test/eventsource/ --trace
```

---

## Trade-off analysis

| Aspect | Event sourcing | Traditional CRUD | CQRS without event sourcing |
|--------|---------------|------------------|----------------------------|
| Audit trail | inherent | manual (add columns) | optional |
| Time-travel debugging | free | requires backup | not available |
| Read performance | eventual (projection lag) | immediate | immediate |
| Write performance | log append + projection | index update | similar to write side |
| Schema evolution | upcasting required | migration required | migration required |
| Conceptual complexity | high | low | medium |

Reflection: a projection rebuilds its read model by replaying the entire event store. With 10 million events and a 1µs replay rate per event, rebuild takes about 10 seconds. In a live system, clients query the stale read model during rebuild. How would you handle this in production? (Hint: blue/green projection rebuild.)

---

## Common production mistakes

**1. Aggregates with side effects in `apply/2`**
`apply/2` must be a pure function — it maps `(state, event) → new_state`. If it sends emails, writes to a database, or calls external APIs, replaying events (for snapshotting, debugging, or recovery) will trigger those side effects again. Side effects belong in projections or process managers.

**2. Projections not idempotent**
The event bus guarantees at-least-once delivery. A projection might receive the same event twice (network retry, crash during processing). If the projection is not idempotent (applying the event twice produces a different result than once), the read model becomes corrupted. Key on `{stream_id, seq}` to deduplicate.

**3. Snapshots not including the version**
A snapshot stores `{state, version}`. Without the version, you cannot know at which point in the event stream the snapshot was taken, so you cannot correctly resume loading from `snapshot_version + 1`. Always store the version with the snapshot.

**4. Long aggregate streams without snapshots**
An aggregate with 100k events replays 100k events on every command. If commands arrive at 100/s and replay takes 100ms, the command handler is the bottleneck before your application even starts doing real work. Snapshot every N events (configurable; start with N=100).

**5. Upcasting after aggregate `apply/2` instead of before**
The upcaster transforms events from old schema to current schema. It must run on raw events from the store, before they are passed to the aggregate. If the aggregate's `apply/2` receives raw v1 events when it only knows v2, it silently ignores unknown fields, producing wrong state.

---

## Resources

- [Greg Young — CQRS Documents](https://cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf) — the canonical CQRS and event sourcing document; 60 pages that define the vocabulary this exercise uses
- ["Implementing Domain-Driven Design"](https://www.informit.com/store/implementing-domain-driven-design-9780321834577) — Vaughn Vernon — aggregates, events, and process managers with concrete examples
- [EventStore documentation](https://developers.eventstore.com/) — the production event store; study the stream concept and optimistic concurrency model
- ["Versioning in an Event Sourced System"](https://leanpub.com/esversioning) — Greg Young — free e-book on upcasting and event schema evolution strategies
- ["Domain Modeling Made Functional"](https://pragprog.com/titles/swdddf/domain-modeling-made-functional/) — Wlaschin — the functional approach to domain modeling translates directly to Elixir
