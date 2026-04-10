# 35. Broadway para Pipelines de Datos de Producción

**Difficulty**: Avanzado

## Prerequisites
- Experiencia con GenStage (producer/consumer, back-pressure)
- Comprensión de sistemas de mensajería (queues, acking)
- Familiaridad con concurrencia y supervisión OTP
- Conocimiento de patrones de bulk processing (batching)

## Learning Objectives
After completing this exercise, you will be able to:
- Implementar un pipeline Broadway completo con `use Broadway`
- Procesar mensajes individuales con `handle_message/3` y en batch con `handle_batch/4`
- Gestionar acknowledgment (ack/nack) para garantizar at-least-once delivery
- Configurar concurrencia, batching y rate limiting en Broadway
- Implementar dead letter queues para mensajes que no se pueden procesar
- Distinguir los patrones de error handling correctos en pipelines de producción

## Concepts

### Broadway: GenStage con pilas de producción

Broadway es una capa de abstracción sobre GenStage que añade lo que los pipelines de producción necesitan:
- **Acknowledgment**: los mensajes solo se marcan como procesados cuando el pipeline los confirma (at-least-once)
- **Batching**: agrupar mensajes antes de procesarlos (bulk inserts, API calls batch)
- **Concurrencia configurable**: processors y batchers independientes
- **Rate limiting**: controlar throughput para no sobrecargar downstream
- **Producers plug-and-play**: SQS, Kafka, RabbitMQ, etc.

```elixir
defmodule MyPipeline do
  use Broadway

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {BroadwayRabbitMQ.Producer, queue: "my_queue"},
        concurrency: 1
      ],
      processors: [
        default: [concurrency: 10]
      ],
      batchers: [
        db: [batch_size: 100, batch_timeout: 1_000, concurrency: 2]
      ]
    )
  end

  # Llamado para CADA mensaje individual
  def handle_message(_processor, message, _context) do
    message
    |> Broadway.Message.update_data(&parse/1)
    |> Broadway.Message.put_batcher(:db)
  end

  # Llamado para cada LOTE de mensajes del batcher :db
  def handle_batch(:db, messages, _batch_info, _context) do
    data = Enum.map(messages, & &1.data)
    MyRepo.insert_all(MyTable, data)
    messages  # Retornar los mensajes → ack automático
  end
end
```

### Broadway.Message: el envelope

Cada mensaje en Broadway es un `%Broadway.Message{}`. Los campos clave:

```elixir
%Broadway.Message{
  data: any(),         # El payload del mensaje
  acknowledger: ...,   # Cómo ackear (manejado por el producer)
  status: :ok | {:failed, reason} | {:failed, reason, opts}
}
```

Las funciones de transformación:
- `Broadway.Message.update_data(msg, fn data -> ... end)` — transforma el payload
- `Broadway.Message.put_batcher(msg, :nombre)` — asigna a un batcher
- `Broadway.Message.failed(msg, reason)` — marca como fallido
- `Broadway.Message.put_metadata(msg, key, value)` — metadatos extras

### Acknowledgment: garantías de entrega

Broadway implementa at-least-once delivery mediante acking:
- Si `handle_message/3` retorna el mensaje sin `:failed` → **ack** (mensaje procesado)
- Si el mensaje tiene status `:failed` → **nack** (mensaje no procesado, puede re-encolarse)
- Si el proceso crashea antes de ackear → **nack** automático (el mensaje sobrevive)

```elixir
def handle_message(_processor, message, _context) do
  case process(message.data) do
    {:ok, result} ->
      Broadway.Message.update_data(message, fn _ -> result end)

    {:error, reason} ->
      # Marcar como fallido — el producer hará nack
      Broadway.Message.failed(message, reason)
  end
end
```

### Rate Limiting

Broadway soporta rate limiting nativo por tiempo o por unidad:
```elixir
producers: [
  module: {MyProducer, []},
  concurrency: 1,
  rate_limiting: [
    allowed_messages: 1_000,
    interval: 1_000  # 1000 mensajes por segundo
  ]
]
```

### Patrones de producción

**Dead Letter Queue (DLQ)**: segundo batcher que recibe mensajes fallidos para procesarlos separadamente (alertas, reintento con backoff, inspección manual).

**Context**: el tercer argumento de `handle_message/3` y `handle_batch/4` es el contexto pasado en `start_link` — úsalo para inyectar dependencias (repos, clientes HTTP).

## Exercises

### Exercise 1: Basic Broadway Pipeline — Queue simulada

Implementa un pipeline Broadway que procesa mensajes de una queue simulada en memoria.

```elixir
# mix.exs — añadir dependencias:
# {:broadway, "~> 1.0"}

defmodule SimulatedQueueProducer do
  @moduledoc """
  Producer Broadway que actúa como una queue en memoria.
  Útil para tests y demos sin infraestructura externa.
  """
  use GenStage

  def start_link(opts) do
    GenStage.start_link(__MODULE__, opts[:messages] || [], name: __MODULE__)
  end

  def init(messages) do
    {:producer, messages}
  end

  def handle_demand(demand, messages) do
    {to_emit, remaining} = Enum.split(messages, demand)

    broadway_messages = Enum.map(to_emit, fn msg ->
      %Broadway.Message{
        data: msg,
        acknowledger: {Broadway.NoopAcknowledger, nil, nil}
      }
    end)

    {:noreply, broadway_messages, remaining}
  end
end

defmodule BasicPipeline do
  @moduledoc """
  Pipeline Broadway básico.

  Stages:
  1. SimulatedQueueProducer — emite mensajes crudos
  2. handle_message/3 — parsea y valida cada mensaje
  3. handle_batch/4 — procesa lotes de 10 mensajes

  Mensaje de entrada: string JSON o mapa
  Mensaje de salida: mapa con campos validados
  """
  use Broadway

  def start_link(messages) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {SimulatedQueueProducer, messages: messages},
        concurrency: 1
      ],
      processors: [
        # TODO: configurar processors con concurrency: 5
      ],
      batchers: [
        # TODO: configurar batcher :default con batch_size: 10, batch_timeout: 500
      ]
    )
  end

  @impl true
  def handle_message(_processor, message, _context) do
    # TODO: Transformar message.data (string) a mapa via Jason.decode!
    # Si el dato ya es un mapa, dejarlo como está
    # Si falla el parseo, marcar como fallido con Broadway.Message.failed/2
    # Asignar al batcher :default con Broadway.Message.put_batcher/2
    message
    |> Broadway.Message.update_data(fn data ->
      # TODO: implementar parsing
    end)
    |> Broadway.Message.put_batcher(:default)
  end

  @impl true
  def handle_batch(:default, messages, _batch_info, _context) do
    # TODO: Procesar el lote completo
    # Imprimir "Procesando lote de #{length(messages)} mensajes"
    # Imprimir cada message.data
    # Retornar messages para que Broadway los ackee
    IO.puts("Procesando lote de #{length(messages)} mensajes:")
    Enum.each(messages, fn msg ->
      IO.puts("  → #{inspect(msg.data)}")
    end)
    # TODO: retornar messages
  end
end

defmodule BasicPipelineDemo do
  def run do
    messages = Enum.map(1..25, fn i ->
      Jason.encode!(%{id: i, value: "item_#{i}", timestamp: DateTime.utc_now()})
    end)

    {:ok, _pid} = BasicPipeline.start_link(messages)
    :timer.sleep(3_000)
    IO.puts("Pipeline completado")
  end
end

# Test it:
# BasicPipelineDemo.run()
# Esperado: ver 3 lotes (10, 10, 5) siendo procesados
```

**Hints**:
- `Broadway.Message.update_data/2` recibe una función `data -> new_data`; la excepción dentro de esa función hace que Broadway marque el mensaje como fallido automáticamente
- El `:batch_timeout` (ms) garantiza que un lote se procesa aunque no llegue a `batch_size`; sin él, mensajes podrían quedarse esperando indefinidamente si la queue está vacía
- `Broadway.NoopAcknowledger` sirve para tests y demos — en producción el producer real implementa el acking contra SQS/Kafka/etc.

**One possible solution** (sparse):
```elixir
# start_link processors y batchers:
processors: [default: [concurrency: 5]],
batchers: [default: [batch_size: 10, batch_timeout: 500, concurrency: 1]]

# handle_message:
|> Broadway.Message.update_data(fn
  data when is_binary(data) -> Jason.decode!(data)
  data -> data
end)

# handle_batch: retornar messages al final
messages
```

---

### Exercise 2: Batching para Bulk Insert a Base de Datos

Implementa un pipeline que agrupa 100 mensajes para hacer un bulk insert eficiente, con un segundo batcher para mensajes de alta prioridad.

```elixir
defmodule BulkInsertPipeline do
  @moduledoc """
  Pipeline que separa mensajes en dos batchers:
  - :bulk — mensajes normales, lotes de 100, bulk insert a DB
  - :priority — mensajes urgentes, lotes de 1, procesamiento inmediato

  Demuestra routing a múltiples batchers desde handle_message.
  """
  use Broadway

  # Módulo fake que simula Ecto Repo
  defmodule FakeRepo do
    def insert_all(table, records) do
      IO.puts("DB INSERT INTO #{table} (#{length(records)} registros)")
      {:ok, length(records)}
    end
  end

  def start_link(messages) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {SimulatedQueueProducer, messages: messages},
        concurrency: 1
      ],
      processors: [
        default: [concurrency: 10]
      ],
      batchers: [
        # TODO: Configurar batcher :bulk con batch_size: 100, batch_timeout: 2_000
        # TODO: Configurar batcher :priority con batch_size: 1, batch_timeout: 100
      ]
    )
  end

  @impl true
  def handle_message(_processor, message, _context) do
    # TODO: Parsear message.data (mapa con :priority y :payload)
    # Si message.data.priority == :high → put_batcher(:priority)
    # Si no → put_batcher(:bulk)
    # Transformar data para que solo quede el :payload
    parsed = message.data

    message
    |> Broadway.Message.update_data(fn data -> data.payload end)
    |> Broadway.Message.put_batcher(
      # TODO: lógica de routing
    )
  end

  @impl true
  def handle_batch(:bulk, messages, batch_info, _context) do
    records = Enum.map(messages, fn msg ->
      %{data: msg.data, inserted_at: DateTime.utc_now()}
    end)

    # TODO: Llamar FakeRepo.insert_all("events", records)
    # Imprimir el resultado
    # Retornar messages

    IO.puts("Batch ##{batch_info.batch_key} procesado (#{length(messages)} registros)")
    messages
  end

  @impl true
  def handle_batch(:priority, messages, _batch_info, _context) do
    # TODO: Procesar cada mensaje de alta prioridad inmediatamente
    # Imprimir "PRIORITY: #{inspect(msg.data)}" para cada uno
    # Retornar messages
  end
end

defmodule BulkInsertDemo do
  def run do
    # Mezcla de mensajes normales y de alta prioridad
    messages = Enum.map(1..250, fn i ->
      priority = if rem(i, 10) == 0, do: :high, else: :normal
      %{priority: priority, payload: "payload_#{i}"}
    end)

    {:ok, _} = BulkInsertPipeline.start_link(messages)
    :timer.sleep(5_000)
  end
end

# Test it:
# BulkInsertDemo.run()
# Esperado:
# - 25 mensajes de alta prioridad procesados individualmente (PRIORITY:...)
# - 225 mensajes normales en lotes de 100 (2 lotes de 100 + 1 de 25)
```

**Hints**:
- Puedes tener tantos batchers como necesites; cada uno tiene su propio `batch_size` y `concurrency`
- `batch_info.batch_key` identifica qué batcher procesó el lote — útil para logging
- Routing condicional en `handle_message/3` es el patrón para pipelines con múltiples destinos

**One possible solution** (sparse):
```elixir
# handle_message routing:
|> Broadway.Message.put_batcher(
  if parsed.priority == :high, do: :priority, else: :bulk
)

# handle_batch :priority:
Enum.each(messages, fn msg ->
  IO.puts("PRIORITY: #{inspect(msg.data)}")
end)
messages
```

---

### Exercise 3: Error Handling — Ack, Nack y Dead Letter Queue

Implementa manejo robusto de errores: mensajes que fallan van a una dead letter queue en lugar de perderse silenciosamente.

```elixir
defmodule ResilientPipeline do
  @moduledoc """
  Pipeline con manejo explícito de errores:
  - Mensajes procesables → ack → batcher :processed
  - Mensajes con error recuperable → nack (re-encolado)
  - Mensajes con error permanente → batcher :dead_letter

  Implementa el patrón Dead Letter Queue (DLQ).
  """
  use Broadway

  # Simula errores: 10% falla permanente, 20% falla transitoria, 70% éxito
  defp simulate_processing(data) do
    case rem(data.id, 10) do
      n when n < 1 -> {:error, :permanent, "Datos corruptos"}
      n when n < 3 -> {:error, :transient, "Servicio no disponible"}
      _            -> {:ok, Map.put(data, :processed, true)}
    end
  end

  def start_link(messages) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {SimulatedQueueProducer, messages: messages},
        concurrency: 1
      ],
      processors: [
        default: [concurrency: 5]
      ],
      batchers: [
        # TODO: batcher :processed para mensajes exitosos (batch_size: 50)
        # TODO: batcher :dead_letter para mensajes con error permanente (batch_size: 10)
      ]
    )
  end

  @impl true
  def handle_message(_processor, message, _context) do
    case simulate_processing(message.data) do
      {:ok, result} ->
        # TODO: Actualizar data con result y asignar a :processed
        message

      {:error, :permanent, reason} ->
        # TODO: Marcar como fallido Y asignarlo al batcher :dead_letter
        # Pista: primero failed, luego put_batcher — o usa put_metadata para el reason
        message
        |> Broadway.Message.failed(reason)
        |> Broadway.Message.put_batcher(:dead_letter)

      {:error, :transient, _reason} ->
        # TODO: Marcar como fallido sin DLQ — el producer hará nack y re-encolará
        Broadway.Message.failed(message, :transient_error)
    end
  end

  @impl true
  def handle_batch(:processed, messages, _batch_info, _context) do
    # TODO: "Procesados exitosamente: #{length(messages)} mensajes"
    # Listar IDs procesados
    # Retornar messages
    ids = Enum.map(messages, & &1.data.id)
    IO.puts("Procesados exitosamente: #{length(messages)} mensajes — IDs: #{inspect(ids)}")
    messages
  end

  @impl true
  def handle_batch(:dead_letter, messages, _batch_info, _context) do
    # TODO: Log de cada mensaje fallido con su razón
    # En producción: enviar a SQS DLQ, alertar a Sentry, etc.
    IO.puts("=== DEAD LETTER QUEUE: #{length(messages)} mensajes ===")
    Enum.each(messages, fn msg ->
      IO.puts("  DLQ: ID #{msg.data.id} — status: #{inspect(msg.status)}")
    end)
    messages
  end

  @impl true
  def handle_failed(messages, _context) do
    # Callback opcional: llamado para mensajes fallidos que no tienen batcher asignado
    # (los :transient_error en nuestro caso)
    IO.puts("handle_failed: #{length(messages)} mensajes serán re-encolados")
    messages
  end
end

defmodule ResilientPipelineDemo do
  def run do
    messages = Enum.map(1..100, fn i ->
      %{id: i, payload: "data_#{i}"}
    end)

    {:ok, _} = ResilientPipeline.start_link(messages)
    :timer.sleep(5_000)
    IO.puts("Demo completado")
  end
end

# Test it:
# ResilientPipelineDemo.run()
# Esperado:
# - ~70 mensajes en batches de :processed
# - ~10 mensajes en :dead_letter (IDs divisibles por 10)
# - ~20 mensajes re-encolados via handle_failed (IDs con módulo 1 o 2)
```

**Hints**:
- `Broadway.Message.failed/2` marca el mensaje pero NO lo saca del pipeline — puedes hacer `failed` + `put_batcher(:dead_letter)` para enrutarlo al DLQ
- `handle_failed/2` es el callback de último recurso para mensajes fallidos que el producer recibe de vuelta; úsalo para logging o re-encolar en una DLQ externa
- En producción con SQS/Kafka, el producer real implementa nack → el mensaje vuelve a la queue automáticamente; `handle_failed` es la señal de que eso ocurrió

**One possible solution** (sparse):
```elixir
# handle_message para {:ok, result}:
message
|> Broadway.Message.update_data(fn _ -> result end)
|> Broadway.Message.put_batcher(:processed)

# handle_batch :processed — retornar messages al final:
messages

# batchers config:
batchers: [
  processed: [batch_size: 50, batch_timeout: 1_000, concurrency: 2],
  dead_letter: [batch_size: 10, batch_timeout: 500, concurrency: 1]
]
```

## Common Mistakes

### Mistake 1: No retornar messages en handle_batch
```elixir
# ❌ Broadway no puede ackear si no retornas la lista de mensajes
def handle_batch(:default, messages, _, _) do
  Enum.each(messages, &process/1)
  # falta retornar messages
end

# ✓ Siempre retornar messages al final
def handle_batch(:default, messages, _, _) do
  Enum.each(messages, &process/1)
  messages
end
```

### Mistake 2: Lanzar excepciones en handle_message sin atrapar
```elixir
# ❌ Una excepción no atrapada en handle_message crashea el processor
# Broadway lo reinicia, pero el mensaje se pierde (nack sin reintento)
def handle_message(_, message, _) do
  result = Jason.decode!(message.data)  # Si falla → crash
  ...
end

# ✓ Capturar errores y usar Broadway.Message.failed
def handle_message(_, message, _) do
  case Jason.decode(message.data) do
    {:ok, result}    -> Broadway.Message.update_data(message, fn _ -> result end)
    {:error, reason} -> Broadway.Message.failed(message, reason)
  end
end
```

### Mistake 3: Batch_timeout demasiado alto en producción
```elixir
# ❌ Con batch_timeout: 60_000, mensajes esperan hasta 60 segundos
# si el lote no llena batch_size — latencia inaceptable
batchers: [default: [batch_size: 1000, batch_timeout: 60_000]]

# ✓ Balance entre tamaño de lote y latencia máxima aceptable
batchers: [default: [batch_size: 100, batch_timeout: 2_000]]
```

### Mistake 4: Usar handle_failed para re-encolar manualmente
```elixir
# ❌ handle_failed no es para lógica de negocio — es un escape hatch
def handle_failed(messages, _context) do
  Enum.each(messages, fn msg ->
    MyQueue.enqueue(msg.data)  # Re-encolar manualmente duplica mensajes
  end)
  messages
end

# ✓ Dejar que el producer original haga nack (el middleware lo maneja)
def handle_failed(messages, _context) do
  Logger.warning("#{length(messages)} mensajes fallaron y serán re-encolados por el producer")
  messages
end
```

## Verification
```bash
iex> c("35-broadway-data-pipelines.exs")
iex> BasicPipelineDemo.run()
# Verificar: lotes de 10 mensajes

iex> BulkInsertDemo.run()
# Verificar: mensajes priority procesados solos, bulk en lotes de 100

iex> ResilientPipelineDemo.run()
# Verificar: DLQ recibe ~10% de mensajes, handle_failed ~20%
```

Checklist de verificación:
- [ ] `handle_message/3` nunca lanza excepciones no controladas
- [ ] `handle_batch/4` siempre retorna la lista de messages
- [ ] El routing a múltiples batchers funciona según la lógica de negocio
- [ ] Los mensajes en DLQ tienen su `status` correcto (`:failed`)
- [ ] `batch_timeout` garantiza que lotes incompletos se procesen eventualmente

## Summary
- Broadway añade sobre GenStage: acking, batching configurable, concurrencia por stage y rate limiting
- `handle_message/3` es por mensaje; `handle_batch/4` es por lote — úsalos para different processing granularity
- El acknowledgment es automático: mensaje sin `:failed` → ack; con `:failed` → nack al producer
- Dead letter queues se implementan con `failed + put_batcher(:dlq)` — no confundir con `handle_failed`
- En producción, Broadway se combina con producers específicos: `broadway_sqs`, `broadway_kafka`, `broadway_rabbitmq`

## What's Next
**36-circuit-breaker-patterns**: Cuando tu pipeline llama a servicios externos, necesitas circuit breakers para que un servicio caído no derribe todo el pipeline.

## Resources
- [Broadway — HexDocs](https://hexdocs.pm/broadway/Broadway.html)
- [Broadway GitHub](https://github.com/dashbitco/broadway)
- [Broadway.Message — HexDocs](https://hexdocs.pm/broadway/Broadway.Message.html)
- [Building Data Pipelines with Broadway](https://www.youtube.com/watch?v=luHK-RZd5uQ)
