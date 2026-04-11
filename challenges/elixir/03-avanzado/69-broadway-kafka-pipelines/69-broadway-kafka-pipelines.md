# Broadway + Kafka — Data Pipelines

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. Every request that passes through the gateway is logged
as a usage event: client ID, service name, method, status code, latency, timestamp.
At peak traffic the gateway handles 50,000 req/min. Persisting each event
synchronously in the request path is not an option — it would add 5–20ms to every
response. Instead, the gateway publishes events to Kafka and a separate pipeline
process consumes them, normalises them, and persists them to PostgreSQL.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── pipeline/
│       │   ├── usage_pipeline.ex       # ← you implement this
│       │   ├── event_normalizer.ex     # ← you implement this
│       │   └── dlq_handler.ex          # ← you implement this
│       ├── usage_events/
│       │   └── usage_event.ex          # Ecto schema — given
│       └── application.ex             # already exists — add pipeline to supervision tree
├── test/
│   └── api_gateway/
│       └── pipeline/
│           └── usage_pipeline_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Three pipeline requirements from the analytics team:

1. **Normalisation**: events arrive as raw JSON with inconsistent field names from
   different gateway versions. The pipeline must translate them to a canonical schema
   before inserting.
2. **Deduplication**: Kafka provides at-least-once delivery. The same event can arrive
   twice after a rebalance. Inserting duplicates corrupts usage metrics.
3. **Dead letter queue**: malformed events (missing required fields, unparseable JSON)
   must not block the pipeline. They must go to a separate topic for later inspection,
   without losing the position of the well-formed events around them.

---

## Why Broadway and not a raw GenServer consuming Kafka

A raw GenServer that calls `KafkaEx.stream/2` in a loop handles one message at a time
and has no back-pressure. Under burst load, the GenServer mailbox fills faster than
messages are processed. When the GenServer crashes, it restarts from an uncommitted
offset — replaying messages already processed.

Broadway addresses all three problems:

1. **Concurrency with back-pressure**: `max_demand` controls how many messages
   each processor requests from the producer. The producer stops delivering when
   processors are saturated.
2. **Batching**: `batchers` aggregate messages from multiple processors and call
   `handle_batch/4` once per batch. A single `Repo.insert_all/2` for 100 events is
   ~20x more efficient than 100 individual inserts.
3. **Acknowledgement**: Broadway acks messages only after `handle_batch/4` returns.
   If the process crashes, Kafka replays from the last committed offset.

---

## Why `handle_message/3` and `handle_batch/4` are separate

`handle_message/3` runs per-message. It is stateless — it receives one raw message
and returns it validated, normalised, and tagged with a batcher. No I/O should happen
here unless absolutely necessary (a fast local ETS dedup check is acceptable).

`handle_batch/4` runs per-batch. It is the right place for I/O: a single bulk insert,
a single Kafka produce call to the DLQ, a single HTTP request to an enrichment service.
Doing I/O in `handle_message/3` means one network round-trip per message instead of
one per batch of 100 — a 100x throughput difference.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
{:broadway, "~> 1.0"},
{:broadway_kafka, "~> 0.4"},
{:jason, "~> 1.4"}
```

### Step 2: Ecto schema — `lib/api_gateway/usage_events/usage_event.ex`

```elixir
defmodule ApiGateway.UsageEvents.UsageEvent do
  use Ecto.Schema

  @primary_key {:id, :binary_id, autogenerate: true}

  schema "gateway_usage_events" do
    field :event_id,    :string         # idempotency key from the producer
    field :client_id,   :string
    field :service,     :string
    field :method,      :string
    field :status_code, :integer
    field :latency_ms,  :integer
    field :occurred_at, :utc_datetime_usec
    field :ingested_at, :utc_datetime_usec

    timestamps(type: :utc_datetime_usec)
  end
end
```

### Step 3: Event normalizer — `lib/api_gateway/pipeline/event_normalizer.ex`

```elixir
defmodule ApiGateway.Pipeline.EventNormalizer do
  @moduledoc """
  Parses raw Kafka message bytes into a canonical usage event map.

  Gateway versions prior to 2.0 sent `client` instead of `client_id` and
  `duration_ms` instead of `latency_ms`. This module normalises both schemas
  into the canonical form expected by UsageEvent.

  Returns {:ok, event_map} or {:error, reason}.
  The returned map contains atom keys matching UsageEvent schema fields.
  """

  @required_fields ~w[event_id client_id service method status_code latency_ms occurred_at]

  @doc """
  Parses and normalises a raw binary event payload.
  """
  def normalise(raw) when is_binary(raw) do
    with {:ok, data} <- Jason.decode(raw),
         {:ok, canonical} <- to_canonical(data),
         :ok <- validate_required(canonical) do
      {:ok, canonical}
    end
  end

  def normalise(_), do: {:error, :not_binary}

  defp to_canonical(data) do
    # TODO: build the canonical map by extracting fields.
    # Handle legacy field names:
    #   "client" → :client_id (if "client_id" is absent)
    #   "duration_ms" → :latency_ms (if "latency_ms" is absent)
    #   "ts" or "timestamp" → :occurred_at (if "occurred_at" is absent)
    # Parse :occurred_at from ISO8601 string using DateTime.from_iso8601/1.
    # Return {:ok, map_with_atom_keys} or {:error, {:parse_error, field, value}}.

    {:ok, %{}}  # placeholder — replace with actual logic
  end

  defp validate_required(canonical) do
    # TODO: check that all @required_fields keys are present in canonical
    # and none of them are nil.
    # Return :ok or {:error, {:missing_fields, [field_names]}}.
    :ok
  end
end
```

### Step 4: Pipeline — `lib/api_gateway/pipeline/usage_pipeline.ex`

```elixir
defmodule ApiGateway.Pipeline.UsagePipeline do
  @moduledoc """
  Broadway pipeline that consumes usage events from Kafka and persists them
  to PostgreSQL in bulk.

  Flow:
    Kafka → handle_message/3 (validate + normalise, per message)
          → handle_batch/4 :db_insert (Repo.insert_all, per 100 messages)
          → handle_batch/4 :dlq (log + publish to DLQ topic, per 50 failed)

  Exactly-once semantics are approximated via:
    1. on_conflict: :nothing in insert_all (dedup by event_id)
    2. Kafka consumer group with manual ack (Broadway manages this)
  """

  use Broadway

  alias Broadway.Message
  alias ApiGateway.{Repo, UsageEvents.UsageEvent}
  alias ApiGateway.Pipeline.{EventNormalizer, DLQHandler}

  require Logger

  @kafka_hosts [{"kafka.internal", 9092}]
  @topic "gateway-usage-events"
  @consumer_group "usage-pipeline-v1"

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {BroadwayKafka.Producer,
          hosts: @kafka_hosts,
          group_id: @consumer_group,
          topics: [@topic],
          offset_reset_policy: :latest}
      ],
      processors: [
        default: [concurrency: 10, max_demand: 5]
      ],
      batchers: [
        db_insert: [
          concurrency: 3,
          batch_size: 100,
          batch_timeout: 1_000
        ],
        dlq: [
          concurrency: 1,
          batch_size: 50,
          batch_timeout: 500
        ]
      ]
    )
  end

  @impl true
  def handle_message(_processor, %Message{data: raw} = message, _context) do
    # TODO: call EventNormalizer.normalise(raw)
    # On {:ok, event}: update message data with event, put_batcher(:db_insert)
    # On {:error, reason}: call Message.failed(message, reason), put_batcher(:dlq)
    message
  end

  @impl true
  def handle_batch(:db_insert, messages, _batch_info, _context) do
    now = DateTime.utc_now()

    rows =
      Enum.map(messages, fn %Message{data: event} ->
        # TODO: merge :ingested_at into the event map
        Map.put(event, :ingested_at, now)
      end)

    # TODO: call Repo.insert_all(UsageEvent, rows,
    #   on_conflict: :nothing, conflict_target: :event_id)
    # Log the count of inserted rows.
    # Return messages unchanged (Broadway needs the original list back).

    messages
  end

  @impl true
  def handle_batch(:dlq, messages, _batch_info, _context) do
    # TODO: call DLQHandler.handle/1 with the failed messages
    # Return messages unchanged.
    messages
  end
end
```

### Step 5: DLQ handler — `lib/api_gateway/pipeline/dlq_handler.ex`

```elixir
defmodule ApiGateway.Pipeline.DLQHandler do
  @moduledoc """
  Handles failed usage events.

  In this implementation the DLQ handler logs each failure with structured
  metadata and records a Telemetry event. In production, replace the Logger
  call with a Kafka produce to the "gateway-usage-events-dlq" topic.

  Idempotency: the DLQ handler does not attempt re-delivery. Each failed
  message is logged once and discarded. The Kafka offset advances, so the
  message will not be replayed unless the consumer group is reset.
  """

  require Logger

  @doc """
  Processes a list of failed Broadway messages.
  Returns the list unchanged (Broadway requires it).
  """
  def handle(messages) when is_list(messages) do
    Enum.each(messages, fn msg ->
      # TODO: extract the failure reason from msg.status
      # msg.status is {:failed, reason} for messages that called Message.failed/2
      # Log at :error level with fields: reason, raw_data (truncated to 200 chars)
      # Emit :telemetry.execute([:api_gateway, :pipeline, :dlq], %{count: 1}, %{})

      Logger.error("Usage event sent to DLQ",
        status: inspect(msg.status),
        data: inspect(msg.data) |> String.slice(0, 200)
      )
    end)

    messages
  end
end
```

### Step 6: Supervision — `lib/api_gateway/application.ex`

Add the pipeline to the supervision tree:

```elixir
# In ApiGateway.Application.start/2, add to children:
ApiGateway.Pipeline.UsagePipeline
```

### Step 7: Given tests — must pass without modification

```elixir
# test/api_gateway/pipeline/usage_pipeline_test.exs
defmodule ApiGateway.Pipeline.UsagePipelineTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Pipeline.{UsagePipeline, EventNormalizer}

  # Test handle_message/3 directly — no Kafka required.
  # Broadway.Message requires an acknowledger tuple {module, ack_ref, ack_data}.
  # Broadway.NoopAcknowledger is a built-in that silently discards ack/nack calls,
  # which is correct for unit tests that bypass the full pipeline.

  defp make_message(data) do
    %Broadway.Message{
      data: data,
      acknowledger: {Broadway.NoopAcknowledger, :ok, :ok}
    }
  end

  defp valid_event_json(overrides \\ %{}) do
    base = %{
      event_id: "evt-#{:rand.uniform(9_999_999)}",
      client_id: "c-test",
      service: "billing",
      method: "GET",
      status_code: 200,
      latency_ms: 45,
      occurred_at: "2026-04-10T12:00:00Z"
    }

    Jason.encode!(Map.merge(base, overrides))
  end

  describe "handle_message/3" do
    test "valid event is routed to :db_insert batcher" do
      msg = make_message(valid_event_json())
      result = UsagePipeline.handle_message(:default, msg, %{})
      assert result.batcher == :db_insert
      assert is_map(result.data)
      assert result.data.client_id == "c-test"
    end

    test "invalid JSON is routed to :dlq batcher and marked failed" do
      msg = make_message("not-json{{")
      result = UsagePipeline.handle_message(:default, msg, %{})
      assert result.batcher == :dlq
      assert {:failed, _reason} = result.status
    end

    test "event missing required field is routed to :dlq" do
      incomplete = Jason.encode!(%{event_id: "x", client_id: "c-1"})
      msg = make_message(incomplete)
      result = UsagePipeline.handle_message(:default, msg, %{})
      assert result.batcher == :dlq
    end
  end

  describe "EventNormalizer.normalise/1" do
    test "normalises canonical event" do
      raw = valid_event_json()
      assert {:ok, event} = EventNormalizer.normalise(raw)
      assert event.client_id == "c-test"
      assert %DateTime{} = event.occurred_at
    end

    test "maps legacy 'client' field to :client_id" do
      raw = Jason.encode!(%{
        event_id: "evt-leg-1",
        client: "c-legacy",
        service: "auth",
        method: "POST",
        status_code: 201,
        duration_ms: 80,
        occurred_at: "2026-04-10T08:00:00Z"
      })

      assert {:ok, event} = EventNormalizer.normalise(raw)
      assert event.client_id == "c-legacy"
      assert event.latency_ms == 80
    end

    test "returns error for non-binary input" do
      assert {:error, :not_binary} = EventNormalizer.normalise(42)
    end

    test "returns error for missing required fields" do
      raw = Jason.encode!(%{event_id: "evt-x", client_id: "c-x"})
      assert {:error, {:missing_fields, _fields}} = EventNormalizer.normalise(raw)
    end
  end
end
```

### Step 8: Run the tests

```bash
mix test test/api_gateway/pipeline/ --trace
```

---

## Trade-off analysis

| Aspect | Broadway + Kafka | GenServer + KafkaEx loop | Oban (database queue) |
|--------|-----------------|--------------------------|-----------------------|
| Back-pressure | Built-in (`max_demand`) | Manual or absent | Configurable |
| Batching | Built-in (batchers) | Manual | Configurable |
| At-least-once | Built-in (ack after batch) | Manual (ack timing) | Yes (DB-backed) |
| Ordering guarantee | Per-partition | Per-consumer | No (concurrent workers) |
| Throughput ceiling | Very high (millions/min) | Medium | Medium |
| Durability | Kafka (external) | Kafka (external) | PostgreSQL |
| Operational complexity | Medium (Kafka cluster) | Medium | Low (DB only) |

Reflection question: `handle_batch/4` calls `Repo.insert_all/2` with
`on_conflict: :nothing`. If Kafka replays 100 events after a consumer rebalance and
50 of them were already inserted in a previous batch, `insert_all` silently ignores
the 50 duplicates. But Broadway still acks all 100 messages — including the 50 that
produced no DB rows. Is this correct behaviour? What would happen if you used
`on_conflict: :replace_all` instead?

---

## Common production mistakes

**1. Performing I/O in `handle_message/3`**
`handle_message/3` is called once per message across all processor workers. A database
read or HTTP call in `handle_message/3` adds a full round-trip to every single message's
critical path. Validate in `handle_message/3`, do I/O in `handle_batch/4`.

**2. Not returning the messages list from `handle_batch/4`**
`handle_batch/4` must return the messages list (possibly with failures marked). If you
return `:ok` or anything else, Broadway raises a function clause error at runtime.

**3. `batch_timeout` too long in development**
With `batch_size: 100` and `batch_timeout: 1_000`, a test that sends 1 message will
wait up to 1 second for the batch to flush. Tests are slow. Set `batch_timeout: 50`
in the test environment.

**4. Forgetting `offset_reset_policy: :earliest` for replay**
The default is `:latest` — Broadway ignores messages that arrived before the consumer
group connected. To replay historical events after a bug fix, set
`offset_reset_policy: :earliest` and reset the consumer group offset in Kafka.

**5. Shared consumer group across environments**
If `dev` and `staging` share the same `group_id`, they compete for partitions.
One environment sees half the messages. Always include the environment in
`@consumer_group`.

---

## Resources

- [Broadway](https://hexdocs.pm/broadway) — pipeline configuration, back-pressure, acking
- [BroadwayKafka](https://hexdocs.pm/broadway_kafka) — Kafka producer, offset management, partition assignment
- [Broadway Architecture](https://elixir-broadway.org/docs/architecture) — producer/processor/batcher model
- [Ecto insert_all](https://hexdocs.pm/ecto/Ecto.Repo.html#c:insert_all/3) — on_conflict, conflict_target, returning
- [Kafka Consumer Groups](https://kafka.apache.org/documentation/#intro_consumers) — partition assignment, rebalancing
