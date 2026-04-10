# Ejercicio 69: Broadway + Kafka — Data Pipelines de Producción

**Nivel**: Avanzado  
**Tiempo estimado**: 90–120 min  
**Módulo**: Concurrencia y procesamiento de mensajes  

---

## Contexto

Estás construyendo la capa de ingesta de eventos para una plataforma de analytics.
El sistema recibe millones de eventos de usuario por día desde múltiples fuentes,
los normaliza y los persiste en PostgreSQL para análisis posterior.

Broadway es el framework de Elixir para construir pipelines de procesamiento
de mensajes concurrentes. Está diseñado para integrarse con sistemas de mensajería
como Kafka, RabbitMQ o SQS y proporciona:

- Procesamiento concurrente con back-pressure automático
- Batching configurable para operaciones bulk
- Acking garantizado (at-least-once delivery)
- Supervisión integrada con reinicio automático

---

## Setup del Proyecto

```bash
mix new event_pipeline --sup
cd event_pipeline
```

**`mix.exs`**:
```elixir
defp deps do
  [
    {:broadway, "~> 1.0"},
    {:broadway_kafka, "~> 0.4"},
    {:jason, "~> 1.4"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, ">= 0.0.0"},
    {:telemetry_metrics, "~> 1.0"},
    {:telemetry_poller, "~> 1.0"}
  ]
end
```

---

## Parte 1: Pipeline Básico de Ingesta de Eventos

### Contexto del dominio

Los eventos llegan como JSON con esta estructura:

```json
{
  "event_id": "uuid-v4",
  "user_id": 42,
  "type": "page_view",
  "properties": {"page": "/home", "referrer": "google.com"},
  "occurred_at": "2026-04-10T12:00:00Z"
}
```

### Schema Ecto

**`lib/event_pipeline/events/event.ex`**:
```elixir
defmodule EventPipeline.Events.Event do
  use Ecto.Schema

  @primary_key {:id, :binary_id, autogenerate: true}

  schema "events" do
    field :event_id, :string
    field :user_id, :integer
    field :type, :string
    field :properties, :map
    field :occurred_at, :utc_datetime_usec
    field :ingested_at, :utc_datetime_usec

    timestamps(type: :utc_datetime_usec)
  end
end
```

### El Pipeline Broadway

**`lib/event_pipeline/ingestion_pipeline.ex`**:
```elixir
defmodule EventPipeline.IngestionPipeline do
  use Broadway

  alias Broadway.Message
  alias EventPipeline.{Repo, Events.Event}

  require Logger

  @kafka_hosts [{"kafka.internal", 9092}]
  @topic "user-events"
  @consumer_group "event-ingestion-v1"

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module:
          {BroadwayKafka.Producer,
           hosts: @kafka_hosts,
           group_id: @consumer_group,
           topics: [@topic],
           # Procesar al menos una partición por consumer
           offset_reset_policy: :latest,
           # Nunca procesar más de 10_000 mensajes pendientes
           fetch_max_bytes: 10_240}
      ],
      processors: [
        # 10 workers paralelos, cada uno procesa un mensaje a la vez
        default: [concurrency: 10, max_demand: 5]
      ],
      batchers: [
        # Batcher para inserciones normales
        db_insert: [
          concurrency: 5,
          batch_size: 100,
          batch_timeout: 1_000
        ],
        # Batcher para el Dead Letter Queue
        dlq: [
          concurrency: 2,
          batch_size: 50,
          batch_timeout: 500
        ]
      ]
    )
  end

  # --- Processors: un mensaje a la vez ---

  @impl true
  def handle_message(_processor, %Message{data: raw} = message, _context) do
    case decode_and_validate(raw) do
      {:ok, event} ->
        message
        |> Message.update_data(fn _ -> event end)
        |> Message.put_batcher(:db_insert)

      {:error, reason} ->
        Logger.warning("Evento inválido descartado",
          reason: inspect(reason),
          raw: String.slice(raw, 0, 200)
        )
        # Mensaje fallido va al batcher DLQ
        message
        |> Message.failed(reason)
        |> Message.put_batcher(:dlq)
    end
  end

  # --- Batchers: lotes de mensajes ---

  @impl true
  def handle_batch(:db_insert, messages, _batch_info, _context) do
    now = DateTime.utc_now()

    rows =
      Enum.map(messages, fn %Message{data: event} ->
        Map.put(event, :ingested_at, now)
      end)

    case Repo.insert_all(Event, rows,
           on_conflict: :nothing,
           conflict_target: :event_id
         ) do
      {count, nil} ->
        Logger.debug("Batch insertado", count: count)
        messages

      # insert_all/3 no devuelve {:error, _}, falla con excepción.
      # El rescue en Broadway atrapa la excepción y marca los mensajes como fallidos.
    end
  end

  @impl true
  def handle_batch(:dlq, messages, _batch_info, _context) do
    # Publicar al topic de DLQ para análisis posterior.
    # En producción usarías un producer Kafka aquí.
    Enum.each(messages, fn msg ->
      Logger.error("Evento enviado a DLQ",
        reason: inspect(msg.status),
        data: inspect(msg.data)
      )
    end)

    messages
  end

  # --- Helpers privados ---

  defp decode_and_validate(raw) when is_binary(raw) do
    with {:ok, data} <- Jason.decode(raw),
         {:ok, event} <- validate_event(data) do
      {:ok, event}
    end
  end

  defp validate_event(%{
         "event_id" => event_id,
         "user_id" => user_id,
         "type" => type,
         "occurred_at" => occurred_at
       })
       when is_binary(event_id) and is_integer(user_id) and is_binary(type) do
    with {:ok, dt, _} <- DateTime.from_iso8601(occurred_at) do
      {:ok,
       %{
         event_id: event_id,
         user_id: user_id,
         type: type,
         properties: %{},
         occurred_at: dt
       }}
    end
  end

  defp validate_event(data), do: {:error, {:invalid_schema, data}}
end
```

---

## Parte 2: Dead Letter Queue con Reintentos

Implementa un mecanismo donde los mensajes que fallan se reencolan
con metadatos de reintento. Cuando superan 3 intentos, se publican
definitivamente al topic `user-events-dlq`.

**`lib/event_pipeline/dlq_handler.ex`**:
```elixir
defmodule EventPipeline.DLQHandler do
  @moduledoc """
  Gestiona mensajes fallidos: reintentos exponenciales y publicación a DLQ.

  Estrategia: los mensajes fallidos llevan un header `x-retry-count`.
  Si el contador es menor a @max_retries, se reencolan al topic original
  con backoff. Si supera el límite, van al topic DLQ permanente.
  """

  @max_retries 3
  @original_topic "user-events"
  @dlq_topic "user-events-dlq"

  alias KafkaEx.Protocol.Produce.Message, as: KafkaMessage

  def handle_failed_messages(messages) do
    {retry, dead} =
      Enum.split_with(messages, fn msg ->
        retry_count(msg) < @max_retries
      end)

    publish_retries(retry)
    publish_dlq(dead)

    # Broadway espera que devolvamos los mensajes tal cual
    messages
  end

  defp retry_count(%Broadway.Message{metadata: %{headers: headers}}) do
    case List.keyfind(headers, "x-retry-count", 0) do
      {_, count} -> String.to_integer(count)
      nil -> 0
    end
  end

  defp retry_count(_), do: 0

  defp publish_retries(messages) do
    Enum.each(messages, fn msg ->
      count = retry_count(msg)
      backoff_ms = :timer.seconds(2 ** count)

      # Reencolar con header actualizado después del backoff
      Process.sleep(backoff_ms)

      kafka_msg = %KafkaMessage{
        key: nil,
        value: Jason.encode!(msg.data),
        headers: [{"x-retry-count", Integer.to_string(count + 1)}]
      }

      KafkaEx.produce(@original_topic, 0, kafka_msg)
    end)
  end

  defp publish_dlq(messages) do
    Enum.each(messages, fn msg ->
      payload = %{
        original_data: msg.data,
        failed_at: DateTime.utc_now(),
        reason: inspect(msg.status),
        retry_count: retry_count(msg)
      }

      kafka_msg = %KafkaMessage{
        key: nil,
        value: Jason.encode!(payload)
      }

      KafkaEx.produce(@dlq_topic, 0, kafka_msg)
    end)
  end
end
```

---

## Parte 3: Rate Limiting con Token Bucket en ETS

Cuando el pipeline necesita llamar a una API externa durante el procesamiento
(enriquecimiento de datos, geolocalización, etc.), debes limitar la tasa
de requests para no saturar el servicio.

**`lib/event_pipeline/rate_limiter.ex`**:
```elixir
defmodule EventPipeline.RateLimiter do
  @moduledoc """
  Token bucket usando ETS para rate limiting distribuido entre workers.

  Cada llamada a `acquire/1` consume un token. Si no hay tokens disponibles,
  bloquea hasta que el refill los reponga. Thread-safe gracias a `update_counter`
  atómico de ETS.
  """

  use GenServer

  @table :rate_limiter_tokens
  # Tokens disponibles por ventana de tiempo
  @default_rate 100
  @default_window_ms 1_000

  def start_link(opts) do
    rate = Keyword.get(opts, :rate, @default_rate)
    window_ms = Keyword.get(opts, :window_ms, @default_window_ms)
    GenServer.start_link(__MODULE__, {rate, window_ms}, name: __MODULE__)
  end

  @doc """
  Adquiere un token o bloquea hasta que haya uno disponible.
  Devuelve `:ok` cuando se puede proceder.
  """
  def acquire(timeout \\ 5_000) do
    deadline = System.monotonic_time(:millisecond) + timeout
    do_acquire(deadline)
  end

  defp do_acquire(deadline) do
    case try_consume_token() do
      :ok ->
        :ok

      :empty ->
        remaining = deadline - System.monotonic_time(:millisecond)

        if remaining <= 0 do
          {:error, :timeout}
        else
          # Esperar un tick pequeño antes de reintentar
          Process.sleep(10)
          do_acquire(deadline)
        end
    end
  end

  # Decremento atómico: si el valor ya es 0, no decrementa (guard en ETS)
  defp try_consume_token do
    case :ets.update_counter(@table, :tokens, {2, -1, 0, 0}) do
      n when n >= 0 -> :ok
      _ -> :empty
    end
  catch
    :error, :badarg -> :empty
  end

  # --- GenServer callbacks ---

  @impl true
  def init({rate, window_ms}) do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    :ets.insert(@table, {:tokens, rate})

    # Timer periódico para reponer tokens
    {:ok, _ref} = :timer.send_interval(window_ms, :refill)

    {:ok, %{rate: rate}}
  end

  @impl true
  def handle_info(:refill, %{rate: rate} = state) do
    :ets.insert(@table, {:tokens, rate})
    {:noreply, state}
  end
end
```

**Uso del rate limiter en el pipeline**:
```elixir
def handle_message(_processor, message, _context) do
  # Bloquea si se supera el límite antes de llamar a la API externa
  :ok = EventPipeline.RateLimiter.acquire()

  enriched = ExternalAPI.enrich(message.data)
  Message.update_data(message, fn _ -> enriched end)
end
```

---

## Parte 4: Telemetría y Observabilidad

Broadway emite eventos de telemetría automáticamente. Adjunta handlers
para métricas de producción.

**`lib/event_pipeline/telemetry.ex`**:
```elixir
defmodule EventPipeline.Telemetry do
  use Supervisor

  import Telemetry.Metrics

  def start_link(arg) do
    Supervisor.start_link(__MODULE__, arg, name: __MODULE__)
  end

  @impl true
  def init(_arg) do
    children = [
      {:telemetry_poller,
       measurements: [
         {EventPipeline.RateLimiter, :report_tokens, []}
       ],
       period: :timer.seconds(10)}
    ]

    :telemetry.attach_many(
      "broadway-pipeline-metrics",
      [
        [:broadway, :processor, :message, :stop],
        [:broadway, :batcher, :stop],
        [:broadway, :processor, :message, :exception]
      ],
      &__MODULE__.handle_event/4,
      nil
    )

    Supervisor.init(children, strategy: :one_for_one)
  end

  def handle_event([:broadway, :processor, :message, :stop], measurements, meta, _) do
    Logger.info("Mensaje procesado",
      duration_ms: System.convert_time_unit(measurements.duration, :native, :millisecond),
      pipeline: meta.name
    )
  end

  def handle_event([:broadway, :batcher, :stop], measurements, meta, _) do
    Logger.info("Batch completado",
      batch_size: meta.batch_size,
      duration_ms: System.convert_time_unit(measurements.duration, :native, :millisecond)
    )
  end

  def handle_event([:broadway, :processor, :message, :exception], _measurements, meta, _) do
    Logger.error("Excepción en processor",
      kind: meta.kind,
      reason: inspect(meta.reason)
    )
  end

  def metrics do
    [
      summary("broadway.processor.message.stop.duration",
        unit: {:native, :millisecond},
        tags: [:name]
      ),
      counter("broadway.processor.message.exception.count", tags: [:name]),
      summary("broadway.batcher.stop.duration",
        unit: {:native, :millisecond},
        tags: [:name, :batcher]
      )
    ]
  end
end
```

---

## Ejercicios Propuestos

### Ejercicio A: Back-pressure adaptativo

Modifica la configuración de `max_demand` del processor para que se ajuste
dinámicamente según la latencia de inserción en base de datos.
Cuando el percentil 95 de `Repo.insert_all` supere 500ms, reduce `max_demand`
de 5 a 1. Investiga `Broadway.update_producer/3`.

### Ejercicio B: Particionamiento por `user_id`

Todos los eventos del mismo usuario deben procesarse en orden (sin reordenar).
Usa `partition_by` en el batcher para garantizar que mensajes del mismo `user_id`
vayan al mismo batcher worker:

```elixir
batchers: [
  db_insert: [
    concurrency: 5,
    batch_size: 100,
    partition_by: fn %Message{data: event} ->
      rem(event.user_id, 5)
    end
  ]
]
```

### Ejercicio C: Circuit breaker para el batcher

Si el batcher de DB falla 5 veces consecutivas en menos de 30 segundos,
el circuit breaker debe abrirse y redirigir todos los mensajes al batcher `:dlq`
sin intentar la inserción. Implementa el circuit breaker con un GenServer
que mantenga el estado `{:open, since}` o `:closed`.

---

## Tests

```elixir
defmodule EventPipeline.IngestionPipelineTest do
  use ExUnit.Case, async: false

  alias EventPipeline.IngestionPipeline

  test "procesa evento JSON válido correctamente" do
    raw = Jason.encode!(%{
      event_id: "test-123",
      user_id: 1,
      type: "page_view",
      occurred_at: "2026-04-10T12:00:00Z"
    })

    message = %Broadway.Message{
      data: raw,
      acknowledger: Broadway.NoopAcknowledger.init()
    }

    result = IngestionPipeline.handle_message(:default, message, %{})

    assert result.batcher == :db_insert
    assert result.data.event_id == "test-123"
  end

  test "mensaje inválido va al batcher DLQ" do
    message = %Broadway.Message{
      data: "not-json{{{",
      acknowledger: Broadway.NoopAcknowledger.init()
    }

    result = IngestionPipeline.handle_message(:default, message, %{})

    assert result.batcher == :dlq
    assert result.status == {:failed, _reason} = result.status
  end
end
```

---

## Preguntas de Comprensión

1. ¿Por qué Broadway garantiza at-least-once delivery y no exactly-once?
   ¿Cómo mitiga el `on_conflict: :nothing` el problema de duplicados?

2. `handle_batch/4` recibe mensajes ya procesados por `handle_message/3`.
   Si un mensaje fue marcado como fallido en `handle_message/3`, ¿llega
   al batcher `:db_insert` o al `:dlq`? ¿Por qué?

3. El rate limiter usa `Process.sleep/1` dentro de `do_acquire/1`.
   ¿Qué problema introduce esto cuando tienes 10 processor workers
   esperando tokens simultáneamente? ¿Cómo lo resolverías?

4. ¿Cuál es la diferencia entre `concurrency: 10` en processors
   y `concurrency: 5` en batchers? ¿Pueden ambos modificarse en caliente?

---

## Recursos

- [Broadway Docs](https://hexdocs.pm/broadway)
- [BroadwayKafka](https://hexdocs.pm/broadway_kafka)
- [Broadway Architecture](https://elixir-broadway.org/docs/architecture)
- [Sagas and DLQ patterns](https://microservices.io/patterns/data/saga.html)
