# 34. GenStage Avanzado: Dispatchers, ConsumerSupervisor y Demand Buffering

**Difficulty**: Avanzado

## Prerequisites
- Sólido dominio de GenServer y OTP supervision trees
- Experiencia con GenStage básico (producer/consumer)
- Comprensión de back-pressure y demand-driven pipelines
- Familiaridad con `DynamicSupervisor` y supervisión dinámica

## Learning Objectives
After completing this exercise, you will be able to:
- Usar `GenStage.PartitionDispatcher` para enrutar eventos a consumers específicos según su contenido
- Usar `GenStage.BroadcastDispatcher` para fanout de eventos a todos los consumers
- Implementar `ConsumerSupervisor` para procesar cada evento en un proceso temporal dedicado
- Manejar demand buffering cuando el producer no puede satisfacer demanda inmediatamente
- Diseñar pipelines GenStage de producción con múltiples etapas y topologías complejas

## Concepts

### GenStage Dispatchers: quién recibe qué

Por defecto, GenStage usa `GenStage.DemandDispatcher` — distribuye eventos round-robin entre consumers. Para casos avanzados existen tres dispatchers especializados:

**BroadcastDispatcher**: cada evento se envía a TODOS los consumers suscritos. Útil para fanout (notificaciones, invalidación de caché distribuida).

```elixir
defmodule MyProducer do
  use GenStage

  def init(_) do
    {:producer, [], dispatcher: GenStage.BroadcastDispatcher}
  end
end
```

**PartitionDispatcher**: enruta cada evento a exactamente UN consumer, determinado por una función hash/partition. Garantiza que eventos del mismo "grupo" siempre van al mismo consumer (ordering garantizado por partición).

```elixir
defmodule MyProducer do
  use GenStage

  def init(_) do
    {:producer, [],
     dispatcher: {GenStage.PartitionDispatcher,
       partitions: [:payments, :signups, :events],
       hash: fn event ->
         # Devuelve {evento, partición_destino}
         partition = determine_partition(event)
         {event, partition}
       end
     }}
  end
end
```

El consumer se suscribe indicando su partición:
```elixir
GenStage.sync_subscribe(consumer, to: producer, partition: :payments)
```

**DemandDispatcher** (default): distribuye eventos a consumers que tienen demand pendiente. Admite `min_demand` y `max_demand`.

### ConsumerSupervisor: escalado automático por evento

`ConsumerSupervisor` es un consumer especial que actúa como supervisor. Cada evento recibido lanza un worker temporal (`Task` o proceso dedicado). El número de procesos vivos simultáneamente está acotado por `max_demand`:

```elixir
defmodule MyWorkerSupervisor do
  use ConsumerSupervisor

  def start_link(opts) do
    ConsumerSupervisor.start_link(__MODULE__, opts)
  end

  def init(_opts) do
    children = [
      # Cada evento lanzará un proceso de este spec
      %{id: MyWorker, start: {MyWorker, :start_link, []}, restart: :temporary}
    ]

    # max_demand = max procesos simultáneos
    opts = [strategy: :one_for_one, subscribe_to: [{MyProducer, max_demand: 10}]]
    ConsumerSupervisor.init(children, opts)
  end
end
```

El worker recibe el evento como argumento de `start_link`:
```elixir
defmodule MyWorker do
  use Task

  def start_link(event) do
    Task.start_link(__MODULE__, :run, [event])
  end

  def run(event) do
    # Procesar el evento — el proceso muere al terminar
    process(event)
  end
end
```

### Demand Buffering: cuando el producer es lento

El challenge más complejo de GenStage: el producer no tiene datos inmediatamente cuando los consumers piden. Hay que almacenar la demanda pendiente y satisfacerla cuando los datos estén disponibles.

```elixir
defmodule SlowProducer do
  use GenStage

  def init(_) do
    # Estado: {buffer_de_eventos, demand_pendiente}
    {:producer, {[], 0}}
  end

  def handle_demand(demand, {buffer, pending_demand}) do
    total_demand = demand + pending_demand
    {to_emit, remaining_buffer} = Enum.split(buffer, total_demand)
    remaining_demand = total_demand - length(to_emit)

    # Si hay eventos en buffer, emitirlos. Si no, guardar la demand.
    {:noreply, to_emit, {remaining_buffer, remaining_demand}}
  end

  # Cuando llegan datos externos (ej: desde una fuente async)
  def handle_info({:new_data, items}, {buffer, pending_demand}) do
    new_buffer = buffer ++ items
    {to_emit, remaining_buffer} = Enum.split(new_buffer, pending_demand)
    remaining_demand = pending_demand - length(to_emit)
    {:noreply, to_emit, {remaining_buffer, remaining_demand}}
  end
end
```

### Trade-offs de diseño en GenStage

| Dispatcher | Cuándo usarlo | Consideraciones |
|---|---|---|
| DemandDispatcher | Distribución equitativa de carga | No hay garantía de ordering entre consumers |
| BroadcastDispatcher | Notificaciones, cache invalidation | Todos los consumers deben suscribirse antes de enviar eventos |
| PartitionDispatcher | Ordering por clave, sharding | Requiere función hash determinista |
| ConsumerSupervisor | CPU-bound work por evento | Max concurrency = max_demand |

En producción: `max_demand` es el parámetro de tuning más importante. Demasiado alto → memory pressure. Demasiado bajo → latencia alta por round-trip de demand.

## Exercises

### Exercise 1: PartitionDispatcher — Pipeline con enrutamiento por tipo de evento

Implementa un pipeline que recibe eventos de distintos tipos (`{:payment, data}`, `{:signup, data}`, `{:click, data}`) y los enruta a consumers especializados según su tipo.

```elixir
defmodule EventRouter do
  @moduledoc """
  Pipeline GenStage con PartitionDispatcher.

  Topología:
    EventProducer --> [PaymentConsumer, SignupConsumer, ClickConsumer]

  Cada consumer solo recibe eventos de su tipo.
  """

  defmodule EventProducer do
    use GenStage

    def start_link(events) do
      GenStage.start_link(__MODULE__, events, name: __MODULE__)
    end

    def init(events) do
      # TODO: Configurar PartitionDispatcher con partitions: [:payment, :signup, :click]
      # La función hash debe extraer el tipo del evento (primer elemento de la tupla)
      # y devolver {event, tipo}
      {
        :producer,
        events,
        dispatcher: {
          GenStage.PartitionDispatcher,
          # TODO: completar opciones
        }
      }
    end

    def handle_demand(demand, events) do
      {to_emit, remaining} = Enum.split(events, demand)
      {:noreply, to_emit, remaining}
    end
  end

  defmodule PaymentConsumer do
    use GenStage

    def start_link(producer) do
      GenStage.start_link(__MODULE__, producer, name: __MODULE__)
    end

    def init(producer) do
      # TODO: suscribirse al producer con partition: :payment
      {:consumer, :ok, subscribe_to: [{producer, partition: :payment, max_demand: 10}]}
    end

    def handle_events(events, _from, state) do
      # TODO: procesar eventos — cada evento es {:payment, data}
      # Imprimir "PaymentConsumer procesó: #{inspect(data)}"
      Enum.each(events, fn {:payment, data} ->
        IO.puts("PaymentConsumer procesó: #{inspect(data)}")
      end)
      {:noreply, [], state}
    end
  end

  defmodule SignupConsumer do
    use GenStage

    def start_link(producer) do
      GenStage.start_link(__MODULE__, producer, name: __MODULE__)
    end

    def init(producer) do
      # TODO: suscribirse con partition: :signup
    end

    def handle_events(events, _from, state) do
      # TODO: imprimir "SignupConsumer procesó: #{inspect(data)}" para cada evento
    end
  end

  defmodule ClickConsumer do
    use GenStage

    def start_link(producer) do
      GenStage.start_link(__MODULE__, producer, name: __MODULE__)
    end

    def init(producer) do
      # TODO: suscribirse con partition: :click
    end

    def handle_events(events, _from, state) do
      # TODO: imprimir "ClickConsumer procesó: #{inspect(data)}" para cada evento
    end
  end

  def run do
    events = [
      {:payment, %{amount: 100, currency: "USD"}},
      {:signup, %{email: "alice@example.com"}},
      {:click, %{button: "buy_now"}},
      {:payment, %{amount: 50, currency: "EUR"}},
      {:signup, %{email: "bob@example.com"}},
      {:click, %{button: "learn_more"}},
    ]

    {:ok, producer}  = EventProducer.start_link(events)
    {:ok, _payment}  = PaymentConsumer.start_link(producer)
    {:ok, _signup}   = SignupConsumer.start_link(producer)
    {:ok, _click}    = ClickConsumer.start_link(producer)

    # Dar tiempo a que se procesen todos los eventos
    :timer.sleep(500)
  end
end

# Test it:
# EventRouter.run()
# Esperado:
# PaymentConsumer procesó: %{amount: 100, currency: "USD"}
# SignupConsumer procesó: %{email: "alice@example.com"}
# ClickConsumer procesó: %{button: "buy_now"}
# ... (en orden no determinístico entre consumers, pero cada tipo va al correcto)
```

**Hints**:
- `PartitionDispatcher` requiere que la función `:hash` devuelva `{event, partition_key}`, no solo `partition_key`
- Los consumers deben suscribirse ANTES de que el producer empiece a enviar eventos; usa `sync_subscribe` si necesitas control del orden
- Si un consumer no está suscrito a la partición correcta, los eventos para esa partición se descartan silenciosamente — verifica en las opciones

**One possible solution** (sparse):
```elixir
# En EventProducer.init/1:
dispatcher: {
  GenStage.PartitionDispatcher,
  partitions: [:payment, :signup, :click],
  hash: fn {type, _data} = event -> {event, type} end
}

# En SignupConsumer.init/1:
{:consumer, :ok, subscribe_to: [{producer, partition: :signup, max_demand: 10}]}

# En ClickConsumer.handle_events/3:
Enum.each(events, fn {:click, data} ->
  IO.puts("ClickConsumer procesó: #{inspect(data)}")
end)
{:noreply, [], state}
```

---

### Exercise 2: ConsumerSupervisor — Cada evento en su propio proceso

Implementa un pipeline donde cada evento se procesa en un proceso `Task` temporal, con concurrencia limitada por `max_demand`.

```elixir
defmodule ParallelProcessor do
  @moduledoc """
  ConsumerSupervisor que lanza un Task por cada evento.
  Útil para trabajo CPU-bound o IO-bound donde queremos
  paralelismo real pero limitado.
  """

  defmodule JobProducer do
    use GenStage

    def start_link(jobs) do
      GenStage.start_link(__MODULE__, jobs, name: __MODULE__)
    end

    def init(jobs) do
      {:producer, jobs}
    end

    def handle_demand(demand, jobs) do
      {to_emit, remaining} = Enum.split(jobs, demand)
      {:noreply, to_emit, remaining}
    end
  end

  defmodule JobWorker do
    # Task que procesa un job. Recibe el job en start_link/1.
    # Debe ser :temporary — si falla, ConsumerSupervisor no lo reinicia.
    use Task, restart: :temporary

    def start_link(job) do
      Task.start_link(__MODULE__, :run, [job])
    end

    def run(job) do
      # TODO: Simular trabajo con :timer.sleep(job.duration_ms)
      # Imprimir "Worker #{inspect(self())} procesando job #{job.id}"
      # Al terminar: "Worker #{inspect(self())} completó job #{job.id}"
    end
  end

  defmodule JobSupervisor do
    use ConsumerSupervisor

    def start_link(opts) do
      ConsumerSupervisor.start_link(__MODULE__, opts, name: __MODULE__)
    end

    def init(_opts) do
      children = [
        # TODO: Spec para JobWorker — debe ser :temporary
        # El spec debe referenciar JobWorker.start_link/1
      ]

      opts = [
        strategy: :one_for_one,
        # TODO: suscribirse a JobProducer con max_demand: 5
        # max_demand: 5 significa máximo 5 jobs simultáneos
        subscribe_to: [
          # TODO: completar
        ]
      ]

      ConsumerSupervisor.init(children, opts)
    end
  end

  def run do
    jobs = Enum.map(1..20, fn i ->
      %{id: i, duration_ms: :rand.uniform(200) + 50}
    end)

    {:ok, _producer}    = JobProducer.start_link(jobs)
    {:ok, _supervisor}  = JobSupervisor.start_link([])

    # Esperar a que todos los jobs terminen
    :timer.sleep(3_000)
    IO.puts("Todos los jobs completados")
  end
end

# Test it:
# ParallelProcessor.run()
# Esperado: ver workers procesando en paralelo (máx 5 simultáneos)
# Los PID de los workers son distintos — cada job tiene su propio proceso
```

**Hints**:
- `ConsumerSupervisor.init/2` recibe la lista de child specs y opciones de supervisión + suscripción juntas
- El child spec para un `Task` con `restart: :temporary` debe tener `restart: :temporary` explícito para que ConsumerSupervisor no reinicie workers fallidos (eso rompería el flow de back-pressure)
- `max_demand: 5` en `subscribe_to` controla directamente cuántos procesos simultáneos habrá — es el parámetro de concurrencia

**One possible solution** (sparse):
```elixir
# En JobWorker.run/1:
def run(job) do
  IO.puts("Worker #{inspect(self())} procesando job #{job.id}")
  :timer.sleep(job.duration_ms)
  IO.puts("Worker #{inspect(self())} completó job #{job.id}")
end

# En JobSupervisor.init/1:
children = [
  %{id: JobWorker, start: {JobWorker, :start_link, []}, restart: :temporary}
]
opts = [
  strategy: :one_for_one,
  subscribe_to: [{JobProducer, max_demand: 5}]
]
ConsumerSupervisor.init(children, opts)
```

---

### Exercise 3: Demand Buffering — Producer que satisface demand de forma asíncrona

Implementa un producer que recibe datos de una fuente externa lenta (simulada con `send/2` asíncrono). Debe almacenar la demand pendiente y satisfacerla cuando los datos lleguen.

```elixir
defmodule AsyncProducer do
  @moduledoc """
  Producer que no tiene datos inmediatamente cuando se le pide.
  Almacena la demand pendiente y emite cuando los datos llegan
  via handle_info.

  Estado: {buffer, pending_demand}
  - buffer: eventos ya disponibles pero no pedidos aún
  - pending_demand: demand recibida pero no satisfecha aún
  """
  use GenStage

  def start_link do
    GenStage.start_link(__MODULE__, {[], 0}, name: __MODULE__)
  end

  # API pública: inyectar datos desde fuera (simula fuente externa)
  def push(items) when is_list(items) do
    send(__MODULE__, {:new_data, items})
  end

  def init(state) do
    {:producer, state}
  end

  def handle_demand(demand, {buffer, pending_demand}) do
    # TODO: Calcular total demand acumulada
    # TODO: Tomar del buffer lo que se pueda emitir ahora
    # TODO: Guardar demand no satisfecha en el estado
    # Si hay eventos en buffer para emitir → {:noreply, events, nuevo_estado}
    # Si no hay eventos → {:noreply, [], {buffer, total_demand}}
    total_demand = demand + pending_demand
    {to_emit, remaining_buffer} = Enum.split(buffer, total_demand)
    remaining_demand = total_demand - length(to_emit)
    {:noreply, to_emit, {remaining_buffer, remaining_demand}}
  end

  def handle_info({:new_data, items}, {buffer, pending_demand}) do
    # TODO: Añadir items al buffer
    # TODO: Emitir lo que se pueda satisfacer con pending_demand
    # TODO: Actualizar buffer y pending_demand en el estado
  end

  def handle_info(_msg, state), do: {:noreply, [], state}
end

defmodule SlowConsumer do
  use GenStage

  def start_link(producer) do
    GenStage.start_link(__MODULE__, producer, name: __MODULE__)
  end

  def init(producer) do
    {:consumer, %{received: 0},
     subscribe_to: [{producer, min_demand: 0, max_demand: 5}]}
  end

  def handle_events(events, _from, %{received: count} = state) do
    Enum.each(events, fn event ->
      IO.puts("Consumer recibió: #{inspect(event)}")
    end)
    {:noreply, [], %{state | received: count + length(events)}}
  end
end

defmodule DemandBufferingDemo do
  def run do
    {:ok, producer} = AsyncProducer.start_link()
    {:ok, _consumer} = SlowConsumer.start_link(producer)

    # El consumer pedirá datos inmediatamente.
    # El producer no tiene nada todavía — acumula la demand.
    :timer.sleep(100)
    IO.puts("--- Inyectando primera tanda de datos ---")
    AsyncProducer.push([1, 2, 3])

    :timer.sleep(200)
    IO.puts("--- Inyectando segunda tanda ---")
    AsyncProducer.push([4, 5, 6, 7, 8])

    :timer.sleep(200)
    IO.puts("--- Inyectando más datos ---")
    AsyncProducer.push(Enum.to_list(9..15))

    :timer.sleep(500)
  end
end

# Test it:
# DemandBufferingDemo.run()
# Esperado: el consumer recibe todos los items en el orden correcto,
# aunque llegaron en tandas distintas y con demand pendiente acumulada.
```

**Hints**:
- El invariante clave: `pending_demand * buffer_size == 0`. O tienes demand pendiente (buffer vacío) o tienes buffer (demand ya satisfecha). Nunca ambos.
- En `handle_info({:new_data, items}, ...)`: el nuevo buffer es `buffer ++ items`. Luego `Enum.split(new_buffer, pending_demand)` decide qué emitir.
- `min_demand: 0` en el consumer es importante para evitar que el sistema pida datos antes de que los haya; en producción ajusta según tu caso.

**One possible solution** (sparse):
```elixir
def handle_info({:new_data, items}, {buffer, pending_demand}) do
  new_buffer = buffer ++ items
  {to_emit, remaining_buffer} = Enum.split(new_buffer, pending_demand)
  remaining_demand = pending_demand - length(to_emit)
  {:noreply, to_emit, {remaining_buffer, remaining_demand}}
end
```

## Common Mistakes

### Mistake 1: Función hash de PartitionDispatcher con signature incorrecta
```elixir
# ❌ Devolver solo la partición — PartitionDispatcher necesita el evento también
hash: fn {type, _} -> type end

# ✓ Devolver {event, partition_key}
hash: fn {type, _data} = event -> {event, type} end
```

### Mistake 2: ConsumerSupervisor con restart: :permanent en workers
```elixir
# ❌ Si el worker falla, ConsumerSupervisor intenta reiniciarlo,
# pero no tiene el evento — el pipeline se rompe
%{id: MyWorker, start: {MyWorker, :start_link, []}}  # restart: :permanent por defecto

# ✓ Siempre :temporary en ConsumerSupervisor workers
%{id: MyWorker, start: {MyWorker, :start_link, []}, restart: :temporary}
```

### Mistake 3: Invariante de demand buffering violado
```elixir
# ❌ Acumular tanto buffer como demand pendiente
# Esto es imposible en un producer correcto —
# si tienes demand, deberías emitir desde el buffer inmediatamente

# ✓ Al recibir demand: emite del buffer si lo hay, o acumula demand
# Al recibir datos: emite para satisfacer demand si la hay, o acumula en buffer
```

### Mistake 4: No usar sync_subscribe cuando el orden importa
```elixir
# ❌ Con start_link asíncrono, el consumer puede no estar listo cuando el producer emite
{:ok, producer} = MyProducer.start_link(events)
{:ok, consumer} = MyConsumer.start_link([])  # puede perderse los primeros eventos

# ✓ Para garantizar que el consumer está suscrito antes de que fluyan datos:
GenStage.sync_subscribe(consumer, to: producer)
```

## Verification
```bash
# En IEx:
iex> c("34-genstage-advanced.exs")

# Exercise 1
iex> EventRouter.run()
# Cada consumer solo debe imprimir SUS eventos

# Exercise 2
iex> ParallelProcessor.run()
# Máximo 5 workers simultáneos visible en los timestamps

# Exercise 3
iex> DemandBufferingDemo.run()
# Todos los números del 1 al 15 recibidos en orden
```

Checklist de verificación:
- [ ] PartitionDispatcher enruta eventos al consumer correcto (ningún consumer recibe eventos de otro tipo)
- [ ] ConsumerSupervisor lanza procesos temporales — cada evento tiene su propio PID
- [ ] La concurrencia en Exercise 2 no supera `max_demand`
- [ ] El demand buffer nunca tiene simultáneamente pending_demand > 0 y buffer no vacío
- [ ] Todos los eventos son procesados, ninguno se pierde

## Summary
- `PartitionDispatcher` garantiza que eventos del mismo "tipo" van siempre al mismo consumer, habilitando ordering por partición
- `BroadcastDispatcher` es fanout — todos los consumers reciben todos los eventos
- `ConsumerSupervisor` es el patrón correcto para paralelizar trabajo por evento; `max_demand` es el throttle de concurrencia
- Demand buffering es el challenge core de producers asíncronos: mantener el invariante `pending * buffer == 0`
- En producción, GenStage pipelines se orquestan con Broadway para añadir batching, acking y observabilidad

## What's Next
**35-broadway-data-pipelines**: Broadway es la capa de producción sobre GenStage. Añade batching, acknowledgment, rate limiting y soporte nativo para Kafka, SQS, RabbitMQ.

## Resources
- [GenStage Dispatchers — HexDocs](https://hexdocs.pm/gen_stage/GenStage.html#module-dispatchers)
- [ConsumerSupervisor — HexDocs](https://hexdocs.pm/gen_stage/ConsumerSupervisor.html)
- [GenStage in Practice — José Valim](https://elixir-lang.org/blog/2016/07/14/announcing-genstage/)
- [Demand-driven back-pressure with GenStage](https://www.erlang-solutions.com/blog/gen-stage-behind-the-scenes/)
