# Broadway + Kafka — Data Pipelines

## Project context

You are building `api_gateway`, an internal HTTP gateway. Every request that passes through the gateway is logged as a usage event. At peak traffic the gateway handles 50,000 req/min. Persisting each event synchronously in the request path is not an option. Instead, the gateway publishes events to Kafka and a separate Broadway pipeline consumes them, normalizes them, and persists them to PostgreSQL. All modules are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── repo.ex                    # Ecto.Repo
│       ├── pipeline/
│       │   ├── usage_pipeline.ex      # Broadway pipeline
│       │   ├── event_normalizer.ex    # parses and normalizes raw events
│       │   └── dlq_handler.ex         # dead letter queue handler
│       └── usage_events/
│           └── usage_event.ex         # Ecto schema
├── test/
│   └── api_gateway/
│       └── pipeline/
│           └── usage_pipeline_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Three pipeline requirements from the analytics team:

1. **Normalization**: events arrive as raw JSON with inconsistent field names from different gateway versions. The pipeline must translate them to a canonical schema before inserting.
2. **Deduplication**: Kafka provides at-least-once delivery. The same event can arrive twice after a rebalance. Inserting duplicates corrupts usage metrics.
3. **Dead letter queue**: malformed events must not block the pipeline. They go to a separate topic for later inspection.

---

## Why Broadway and not a raw GenServer consuming Kafka

A raw GenServer that calls `KafkaEx.stream/2` has no back-pressure, handles one message at a time, and replays already-processed messages on crash. Broadway addresses all three with `max_demand` back-pressure, batcher-based bulk inserts, and post-batch acknowledgement.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
defp deps do
  [
    {:broadway, "~> 1.0"},
    {:broadway_kafka, "~> 0.4"},
    {:jason, "~> 1.4"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.18"}
  ]
end
```

### Step 2: Ecto schema — `lib/api_gateway/usage_events/usage_event.ex`

```elixir
defmodule ApiGateway.UsageEvents.UsageEvent do
  @moduledoc "Schema for persisted usage events."
  use Ecto.Schema

  @primary_key {:id, :binary_id, autogenerate: true}

  schema "gateway_usage_events" do
    field :event_id,    :string
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
  `duration_ms` instead of `latency_ms`. This module normalizes both schemas
  into the canonical form expected by UsageEvent.

  Returns {:ok, event_map} or {:error, reason}.
  """

  @required_fields ~w[event_id client_id service method status_code latency_ms occurred_at]a

  @doc "Parses and normalizes a raw binary event payload."
  @spec normalise(binary()) :: {:ok, map()} | {:error, term()}
  def normalise(raw) when is_binary(raw) do
    with {:ok, data} <- Jason.decode(raw),
         {:ok, canonical} <- to_canonical(data),
         :ok <- validate_required(canonical) do
      {:ok, canonical}
    end
  end

  def normalise(_), do: {:error, :not_binary}

  defp to_canonical(data) do
    occurred_at = data["occurred_at"] || data["ts"] || data["timestamp"]

    parsed_time =
      case occurred_at do
        nil -> nil
        ts when is_binary(ts) ->
          case DateTime.from_iso8601(ts) do
            {:ok, dt, _} -> dt
            {:error, _} -> nil
          end
        _ -> nil
      end

    status_code =
      case data["status_code"] do
        s when is_integer(s) -> s
        s when is_binary(s) ->
          case Integer.parse(s) do
            {n, ""} -> n
            _ -> nil
          end
        _ -> nil
      end

    latency_ms =
      case data["latency_ms"] || data["duration_ms"] do
        l when is_integer(l) -> l
        l when is_binary(l) ->
          case Integer.parse(l) do
            {n, ""} -> n
            _ -> nil
          end
        _ -> nil
      end

    canonical = %{
      event_id:    data["event_id"],
      client_id:   data["client_id"] || data["client"],
      service:     data["service"],
      method:      data["method"],
      status_code: status_code,
      latency_ms:  latency_ms,
      occurred_at: parsed_time
    }

    {:ok, canonical}
  end

  defp validate_required(canonical) do
    missing =
      @required_fields
      |> Enum.filter(fn field -> is_nil(Map.get(canonical, field)) end)

    if missing == [] do
      :ok
    else
      {:error, {:missing_fields, missing}}
    end
  end
end
```

The normalizer handles legacy field names: `"client"` maps to `:client_id`, `"duration_ms"` maps to `:latency_ms`, and `"ts"` or `"timestamp"` map to `:occurred_at`. The `occurred_at` field is parsed from ISO 8601 into a `DateTime` struct.

### Step 4: Pipeline — `lib/api_gateway/pipeline/usage_pipeline.ex`

```elixir
defmodule ApiGateway.Pipeline.UsagePipeline do
  @moduledoc """
  Broadway pipeline that consumes usage events from Kafka and persists them
  to PostgreSQL in bulk.

  Flow:
    Kafka -> handle_message/3 (validate + normalize, per message)
          -> handle_batch/4 :db_insert (Repo.insert_all, per 100 messages)
          -> handle_batch/4 :dlq (log + publish to DLQ topic, per 50 failed)

  Exactly-once semantics approximated via on_conflict: :nothing in insert_all.
  """

  use Broadway

  alias Broadway.Message
  alias ApiGateway.Pipeline.{EventNormalizer, DLQHandler}

  require Logger

  @kafka_hosts [{"kafka.internal", 9092}]
  @topic "gateway-usage-events"
  @consumer_group "usage-pipeline-v1"

  @spec start_link(keyword()) :: GenServer.on_start()
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
    case EventNormalizer.normalise(raw) do
      {:ok, event} ->
        message
        |> Message.put_data(event)
        |> Message.put_batcher(:db_insert)

      {:error, reason} ->
        message
        |> Message.failed(reason)
        |> Message.put_batcher(:dlq)
    end
  end

  @impl true
  def handle_batch(:db_insert, messages, _batch_info, _context) do
    now = DateTime.utc_now()

    rows =
      Enum.map(messages, fn %Message{data: event} ->
        Map.put(event, :ingested_at, now)
      end)

    {inserted, _} = ApiGateway.Repo.insert_all(
      ApiGateway.UsageEvents.UsageEvent,
      rows,
      on_conflict: :nothing,
      conflict_target: :event_id
    )

    Logger.info("Inserted #{inserted} usage events (batch of #{length(rows)})")

    messages
  end

  @impl true
  def handle_batch(:dlq, messages, _batch_info, _context) do
    DLQHandler.handle(messages)
    messages
  end
end
```

### Step 5: DLQ handler — `lib/api_gateway/pipeline/dlq_handler.ex`

```elixir
defmodule ApiGateway.Pipeline.DLQHandler do
  @moduledoc """
  Handles failed usage events by logging them with structured metadata.
  In production, replace the Logger call with a Kafka produce to a DLQ topic.
  """

  require Logger

  @doc "Processes a list of failed Broadway messages."
  @spec handle([Broadway.Message.t()]) :: [Broadway.Message.t()]
  def handle(messages) when is_list(messages) do
    Enum.each(messages, fn msg ->
      Logger.error("Usage event sent to DLQ",
        status: inspect(msg.status),
        data: inspect(msg.data) |> String.slice(0, 200)
      )

      :telemetry.execute([:api_gateway, :pipeline, :dlq], %{count: 1}, %{})
    end)

    messages
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/pipeline/usage_pipeline_test.exs
defmodule ApiGateway.Pipeline.UsagePipelineTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Pipeline.{UsagePipeline, EventNormalizer}

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

### Step 7: Run the tests

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
| Operational complexity | Medium (Kafka cluster) | Medium | Low (DB only) |

Reflection question: `handle_batch/4` calls `Repo.insert_all/2` with `on_conflict: :nothing`. If Kafka replays 100 events after a rebalance and 50 were already inserted, `insert_all` silently ignores the 50 duplicates. Broadway still acks all 100. Is this correct?

---

## Common production mistakes

**1. Performing I/O in `handle_message/3`**
`handle_message/3` is called once per message. A database read here adds a full round-trip to every message's critical path. Validate in `handle_message/3`, do I/O in `handle_batch/4`.

**2. Not returning the messages list from `handle_batch/4`**
`handle_batch/4` must return the messages list. Returning `:ok` causes a function clause error.

**3. Forgetting `offset_reset_policy: :earliest` for replay**
The default is `:latest` — Broadway ignores historical messages. To replay after a bug fix, set `:earliest` and reset the consumer group offset.

**4. Shared consumer group across environments**
If `dev` and `staging` share the same `group_id`, they compete for partitions. Include the environment in `@consumer_group`.

---

## Resources

- [Broadway](https://hexdocs.pm/broadway) — pipeline configuration, back-pressure, acking
- [BroadwayKafka](https://hexdocs.pm/broadway_kafka) — Kafka producer, offset management
- [Broadway Architecture](https://elixir-broadway.org/docs/architecture) — producer/processor/batcher model
- [Ecto insert_all](https://hexdocs.pm/ecto/Ecto.Repo.html#c:insert_all/3) — on_conflict, conflict_target
