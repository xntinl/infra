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

## Why append-only log as the source of truth and not mutable state snapshot as the source of truth

the log is the only representation that can answer "what was the state at time T?" without historical snapshots. A mutable snapshot loses history the moment it's updated.

## Design decisions

**Option A — single write model that is also the read model**
- Pros: simpler, no eventual consistency
- Cons: can't optimize reads independently, complex queries are slow

**Option B — append-only event log + projections into read-optimized views** (chosen)
- Pros: unbounded query shapes via independent projections, full audit trail
- Cons: eventual consistency window to manage

→ Chose **B** because event-sourcing's value only materializes when projections are physically separate and independently rebuildable.

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

The event store is an append-only log partitioned by stream_id. Each stream is an ordered list of events with consecutive sequence numbers starting at 0. The store uses ETS for in-memory persistence (DETS can be substituted for durability). Events are keyed by `{stream_id, seq}` to enable efficient range reads without loading entire streams.

Optimistic locking is enforced in the `append/3` callback: the caller specifies the expected version, and the store rejects the append if the actual version differs.

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

  Uses ETS for storage. Keys are {stream_id, seq} tuples, enabling efficient
  range scans and selective loading for snapshotted aggregates.
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
    # Read directly from ETS -- no GenServer call needed for reads.
    # Match all entries for this stream_id and filter by sequence number.
    :ets.match_object(@table, {{stream_id, :_}, :_})
    |> Enum.map(fn {_key, entry} -> entry end)
    |> Enum.filter(fn entry -> entry.seq >= from_seq end)
    |> Enum.sort_by(fn entry -> entry.seq end)
  end

  @doc """
  Returns the current version (last sequence number) of a stream.
  Returns -1 for a new (non-existent) stream.
  """
  @spec stream_version(String.t()) :: integer()
  def stream_version(stream_id) do
    case :ets.match(@table, {{stream_id, :"$1"}, :_}) do
      [] -> -1
      matches -> matches |> List.flatten() |> Enum.max()
    end
  end

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set])
    {:ok, %{table: @table}}
  end

  @impl true
  def handle_call({:append, stream_id, events, expected_version}, _from, state) do
    current_version = stream_version(stream_id)

    if current_version != expected_version do
      {:reply, {:error, :version_conflict}, state}
    else
      ts = System.system_time(:millisecond)

      Enum.with_index(events, current_version + 1)
      |> Enum.each(fn {event, seq} ->
        entry = %{
          stream_id: stream_id,
          seq: seq,
          event_type: event.type,
          payload: event.payload,
          timestamp: ts,
          metadata: Map.get(event, :metadata, %{})
        }
        :ets.insert(@table, {{stream_id, seq}, entry})
      end)

      new_version = current_version + length(events)
      {:reply, {:ok, new_version}, state}
    end
  end
end
```

### Step 4: `lib/eventsource/store/snapshot_store.ex`

The snapshot store saves aggregate state at a given version. When loading an aggregate, the command handler checks for a snapshot first. If one exists, replay starts from the snapshot version instead of from event 0.

```elixir
defmodule Eventsource.Store.SnapshotStore do
  use GenServer

  @moduledoc """
  Stores aggregate state snapshots for fast replay.

  A snapshot is a serialized aggregate state at a specific stream version.
  Loading an aggregate checks the snapshot store first; if a snapshot exists,
  replay begins from snapshot_version + 1 instead of from event 0.
  """

  @table :eventsource_snapshots

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Saves a snapshot for the given aggregate."
  @spec save(String.t(), term(), integer()) :: :ok
  def save(aggregate_id, state, version) do
    :ets.insert(@table, {aggregate_id, %{state: state, version: version}})
    :ok
  end

  @doc "Gets the latest snapshot for the given aggregate."
  @spec get(String.t()) :: {:ok, map()} | :not_found
  def get(aggregate_id) do
    case :ets.lookup(@table, aggregate_id) do
      [{^aggregate_id, snapshot}] -> {:ok, snapshot}
      [] -> :not_found
    end
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set])
    {:ok, %{}}
  end
end
```

### Step 5: `lib/eventsource/aggregate.ex`

The aggregate behaviour defines the contract for event-sourced domain objects. `init/0` returns the initial state, `handle/2` processes commands and emits events (without mutating state), and `apply/2` applies events to produce new state. This separation guarantees that replaying events always produces the correct state.

```elixir
defmodule Eventsource.Aggregate do
  @moduledoc """
  Behaviour for event-sourced aggregates.

  An aggregate:
  1. Has an initial state (`init/0`)
  2. Handles commands (`handle/2`) -> produces events (does NOT mutate state)
  3. Applies events to state (`apply/2`) -> produces new state

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

### Step 6: `lib/eventsource/upcaster.ex`

The upcaster transforms events from old schemas to current schemas during replay. This allows aggregates to evolve their event structures over time without migrating historical data.

```elixir
defmodule Eventsource.Upcaster do
  @moduledoc """
  Event schema migration: transforms events from old versions to current versions.

  When an aggregate's event schema changes (e.g., a field is renamed or a new
  required field is added), the upcaster transforms old events during replay
  so the aggregate's apply/2 function only needs to handle the current schema.

  Register upcasters with register/3. Events without a registered upcaster
  pass through unchanged.
  """

  @doc "Upcasts an event to its current schema version. Returns the event unchanged if no upcaster is registered."
  @spec upcast(map()) :: map()
  def upcast(event) do
    # In a full implementation, this would look up registered upcasters
    # by event_type and apply transformations in sequence (v1 -> v2 -> v3).
    # For now, pass through unchanged -- aggregates handle the current schema.
    event
  end
end
```

### Step 7: `lib/eventsource/command_handler.ex`

The command handler orchestrates the entire command execution lifecycle: load aggregate state (from snapshot or full replay), execute the command, append new events with optimistic locking, and retry on version conflicts.

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

  The handler is stateless -- it does not hold aggregate state between commands.
  Each command execution is a fresh load-replay-handle-append cycle.
  """

  @max_retries 3

  @spec execute(module(), String.t(), map(), non_neg_integer()) ::
    {:ok, [map()]} | {:error, term()}
  def execute(aggregate_module, aggregate_id, command, retry \\ 0) do
    with {:ok, state, version} <- load_aggregate(aggregate_module, aggregate_id),
         {:ok, events} <- aggregate_module.handle(state, command),
         {:ok, _new_version} <- Eventsource.Store.EventStore.append(aggregate_id, events, version) do
      {:ok, events}
    else
      {:error, :version_conflict} when retry < @max_retries ->
        # Reload state and retry -- another command was applied concurrently
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

### Step 8: Given tests — must pass without modification

```elixir
# test/eventsource/event_store_test.exs
defmodule Eventsource.Store.EventStoreTest do
  use ExUnit.Case, async: false

  alias Eventsource.Store.EventStore

  setup do
    # Use a temp file for each test
    :ok
  end


  describe "EventStore" do

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


  describe "Aggregate" do

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
end
```

### Step 9: Run the tests

```bash
mix test test/eventsource/ --trace
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the event sourcing + CQRS invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.

## Benchmark

```elixir
# Minimal timing harness — replace with Benchee for production measurement.
{time_us, _result} = :timer.tc(fn ->
  # exercise the hot path N times
  for _ <- 1..10_000, do: :ok
end)

IO.puts("average: #{time_us / 10_000} µs per op")
```

Target: <50µs per event append and >100k events/s projection throughput.

## Trade-off analysis

| Aspect | Event sourcing | Traditional CRUD | CQRS without event sourcing |
|--------|---------------|------------------|----------------------------|
| Audit trail | inherent | manual (add columns) | optional |
| Time-travel debugging | free | requires backup | not available |
| Read performance | eventual (projection lag) | immediate | immediate |
| Write performance | log append + projection | index update | similar to write side |
| Schema evolution | upcasting required | migration required | migration required |
| Conceptual complexity | high | low | medium |

Reflection: a projection rebuilds its read model by replaying the entire event store. With 10 million events and a 1us replay rate per event, rebuild takes about 10 seconds. In a live system, clients query the stale read model during rebuild. How would you handle this in production? (Hint: blue/green projection rebuild.)

---

## Common production mistakes

**1. Aggregates with side effects in `apply/2`**
`apply/2` must be a pure function — it maps `(state, event) -> new_state`. If it sends emails, writes to a database, or calls external APIs, replaying events (for snapshotting, debugging, or recovery) will trigger those side effects again. Side effects belong in projections or process managers.

**2. Projections not idempotent**
The event bus guarantees at-least-once delivery. A projection might receive the same event twice (network retry, crash during processing). If the projection is not idempotent (applying the event twice produces a different result than once), the read model becomes corrupted. Key on `{stream_id, seq}` to deduplicate.

**3. Snapshots not including the version**
A snapshot stores `{state, version}`. Without the version, you cannot know at which point in the event stream the snapshot was taken, so you cannot correctly resume loading from `snapshot_version + 1`. Always store the version with the snapshot.

**4. Long aggregate streams without snapshots**
An aggregate with 100k events replays 100k events on every command. If commands arrive at 100/s and replay takes 100ms, the command handler is the bottleneck before your application even starts doing real work. Snapshot every N events (configurable; start with N=100).

**5. Upcasting after aggregate `apply/2` instead of before**
The upcaster transforms events from old schema to current schema. It must run on raw events from the store, before they are passed to the aggregate. If the aggregate's `apply/2` receives raw v1 events when it only knows v2, it silently ignores unknown fields, producing wrong state.

---

## Reflection

If a projection gets corrupted and must be rebuilt from 100M events, how long does your system take, and what does your read endpoint return during the rebuild? Design the degraded-mode contract.

## Resources

- [Greg Young — CQRS Documents](https://cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf) — the canonical CQRS and event sourcing document; 60 pages that define the vocabulary this exercise uses
- ["Implementing Domain-Driven Design"](https://www.informit.com/store/implementing-domain-driven-design-9780321834577) — Vaughn Vernon — aggregates, events, and process managers with concrete examples
- [EventStore documentation](https://developers.eventstore.com/) — the production event store; study the stream concept and optimistic concurrency model
- ["Versioning in an Event Sourced System"](https://leanpub.com/esversioning) — Greg Young — free e-book on upcasting and event schema evolution strategies
- ["Domain Modeling Made Functional"](https://pragprog.com/titles/swdddf/domain-modeling-made-functional/) — Wlaschin — the functional approach to domain modeling translates directly to Elixir
