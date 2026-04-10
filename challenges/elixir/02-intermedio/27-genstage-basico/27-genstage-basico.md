# 27. GenStage Básico

**Difficulty**: Intermedio

## Prerequisites
- Completed exercises 01–26
- Strong understanding of GenServer (exercise 04)
- Familiarity with supervision trees
- Understanding of backpressure as a concept

## Learning Objectives
After completing this exercise, you will be able to:
- Implementar un Producer de GenStage que responde a demanda con `handle_demand/2`
- Implementar un Consumer que procesa eventos con `handle_events/3`
- Implementar un ProducerConsumer que transforma eventos en el pipeline
- Conectar stages con `GenStage.sync_subscribe/3`
- Entender cómo el backpressure se propaga automáticamente en GenStage
- Construir pipelines de procesamiento de datos con múltiples etapas

## Concepts

### ¿Qué es GenStage y por qué backpressure?

GenStage es una librería para construir pipelines de procesamiento de datos con backpressure automático. El problema que resuelve: si un productor de datos es más rápido que el consumidor, sin control el consumidor se inunda y el sistema colapsa (OOM, latencia infinita, timeouts).

Con GenStage, los consumidores anuncian cuántos eventos están listos para procesar (demanda). Los productores solo envían datos cuando hay demanda — nunca más de lo que el consumidor puede manejar. Esto invierte el control de flujo del "push" tradicional a un modelo "pull".

```
Producer          Consumer
   |  <-- demand(10) --|
   |-- events(10) ---> |
   |                   | (procesa 10, listo para más)
   |  <-- demand(10) --|
   |-- events(10) ---> |
```

### El modelo de roles

GenStage define tres roles:

| Rol | Descripción | Ejemplo |
|-----|-------------|---------|
| `:producer` | Genera eventos, responde a demanda | Leer de DB, Kafka, cola |
| `:consumer` | Consume eventos, no produce nada | Escribir a DB, enviar email |
| `:producer_consumer` | Transforma: consume y produce | Parsear, filtrar, enriquecer |

```elixir
# Producer
use GenStage

def init(:producer), do: {:producer, initial_state}
def handle_demand(demand, state) -> {:noreply, events, new_state}

# Consumer
def init(:consumer), do: {:consumer, initial_state}
def handle_events(events, _from, state) -> {:noreply, [], new_state}

# ProducerConsumer
def init(:producer_consumer), do: {:producer_consumer, initial_state}
def handle_events(events, _from, state) -> {:noreply, transformed_events, new_state}
```

### handle_demand/2: el corazón del Producer

`handle_demand/2` se llama cuando un consumidor solicita más eventos. El primer argumento es la cantidad demandada (un entero positivo). El Producer debe retornar `{:noreply, events, state}` donde `events` es una lista de hasta `demand` elementos.

```elixir
defmodule NumberProducer do
  use GenStage

  def start_link(opts \\ []), do: GenStage.start_link(__MODULE__, 0, opts)

  def init(_), do: {:producer, 0}  # state = último número producido

  def handle_demand(demand, last) when demand > 0 do
    events = Enum.to_list(last + 1 .. last + demand)
    {:noreply, events, last + demand}
  end
end
```

El Producer también puede retornar menos eventos que la demanda — GenStage acumulará el déficit y lo pedirá de nuevo más tarde. Puede retornar `[]` si no hay datos disponibles.

### handle_events/3: procesamiento en Consumer y ProducerConsumer

`handle_events/3` recibe la lista de eventos, la referencia del suscriptor (puedes ignorarla), y el estado. Un Consumer retorna `{:noreply, [], state}` — la lista vacía indica que no produce nada más. Un ProducerConsumer retorna `{:noreply, transformed_events, state}`.

```elixir
defmodule PrintConsumer do
  use GenStage

  def start_link(opts \\ []), do: GenStage.start_link(__MODULE__, :ok, opts)

  def init(:ok), do: {:consumer, :ok}

  def handle_events(events, _from, state) do
    Enum.each(events, fn event ->
      IO.inspect(event, label: "Received")
    end)
    {:noreply, [], state}  # consumer siempre retorna lista vacía
  end
end
```

### Conectar stages con sync_subscribe

`GenStage.sync_subscribe/3` conecta un consumidor a un productor. El consumidor le dice al productor cuántos eventos quiere inicialmente (`:max_demand`).

```elixir
{:ok, producer} = NumberProducer.start_link()
{:ok, consumer} = PrintConsumer.start_link()

GenStage.sync_subscribe(consumer, to: producer, max_demand: 5)
# El consumer pide 5 eventos, producer responde con 5,
# consumer los procesa, pide 5 más, y así sucesivamente
```

### Fan-out: múltiples consumidores para un producer

Un producer puede tener múltiples consumidores. GenStage distribuye los eventos entre todos los consumidores de forma round-robin por defecto (dispatcher `:demand`).

```elixir
{:ok, producer} = NumberProducer.start_link()
{:ok, consumer1} = PrintConsumer.start_link()
{:ok, consumer2} = PrintConsumer.start_link()

GenStage.sync_subscribe(consumer1, to: producer, max_demand: 10)
GenStage.sync_subscribe(consumer2, to: producer, max_demand: 10)
# Los eventos se reparten entre consumer1 y consumer2
```

### Dependency setup

GenStage no viene con Elixir/OTP — es una dependencia de Hex:

```elixir
# mix.exs
defp deps do
  [{:gen_stage, "~> 1.2"}]
end
```

---

## Exercises

### Exercise 1: Producer simple con handle_demand

```elixir
defmodule CounterProducer do
  use GenStage

  @moduledoc """
  Producer que genera una secuencia infinita de enteros empezando en 1.
  """

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, 0, opts)
  end

  # TODO 1: Implementa init/1
  # Debe retornar {:producer, 0}
  # El state (0) es el último número emitido
  def init(initial) do
    # TODO: {:producer, initial}
  end

  # TODO 2: Implementa handle_demand/2
  # Recibe: demand (cuántos eventos pide el consumer), state (último número emitido)
  # Debe generar exactamente `demand` eventos (números enteros consecutivos)
  # Retorna: {:noreply, events, new_state}
  # PISTA: events = Enum.to_list(state + 1 .. state + demand)
  def handle_demand(demand, last_number) when demand > 0 do
    # TODO
  end
end

# Test básico — el producer debe emitir números cuando se le pide
{:ok, producer} = CounterProducer.start_link()

# Para testear sin consumer, podemos usar GenStage internamente:
# Normalmente conectarías un consumer, pero aquí verificamos el callback:
state_after = %{last: 0}
# Simulación: demand de 5 debe generar [1, 2, 3, 4, 5]
# (el test real se hace con un consumer en el ejercicio 4)
IO.puts("Producer iniciado — ver ejercicio 4 para test completo")
```

---

### Exercise 2: Consumer simple con handle_events

```elixir
defmodule LogConsumer do
  use GenStage

  @moduledoc """
  Consumer que imprime cada evento recibido con un timestamp.
  """

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, %{count: 0}, opts)
  end

  # TODO 1: Implementa init/1
  # Debe retornar {:consumer, initial_state}
  # El state es un map %{count: 0} para contar eventos procesados
  def init(state) do
    # TODO: {:consumer, state}
  end

  # TODO 2: Implementa handle_events/3
  # Recibe: events (lista), _from (referencia del suscriptor), state
  # Para cada evento: imprime "Processing event: #{inspect(event)}"
  # Actualiza state.count sumando length(events)
  # Retorna: {:noreply, [], new_state}
  # IMPORTANTE: el consumer siempre retorna lista VACÍA como segundo elemento
  def handle_events(events, _from, state) do
    # TODO: Enum.each(events, fn event -> IO.puts("Processing: #{inspect(event)}") end)
    # TODO: actualiza el count
    # TODO: {:noreply, [], %{state | count: state.count + length(events)}}
  end

  # TODO 3: Agrega una función pública processed_count/1 que retorna
  # cuántos eventos ha procesado. Usa GenStage.call o GenServer.call.
  def processed_count(pid) do
    # TODO: GenServer.call(pid, :get_count)
  end

  # TODO 4: Implementa el handler para :get_count
  def handle_call(:get_count, _from, state) do
    # TODO: {:reply, state.count, state}
  end
end
```

---

### Exercise 3: ProducerConsumer — transformar eventos en el pipeline

```elixir
defmodule StringTransformer do
  use GenStage

  @moduledoc """
  ProducerConsumer que recibe strings y los transforma:
  - Convierte a uppercase
  - Agrega un prefijo "[PROCESSED]"
  - Filtra strings vacíos
  """

  def start_link(opts \\ []) do
    GenStage.start_link(__MODULE__, :ok, opts)
  end

  # TODO 1: Implementa init/1
  # Un ProducerConsumer retorna {:producer_consumer, state}
  def init(:ok) do
    # TODO: {:producer_consumer, :ok}
  end

  # TODO 2: Implementa handle_events/3
  # Recibe strings, aplica las transformaciones, retorna los transformados
  # Pipeline de transformación:
  # 1. Filtra strings vacíos con Enum.reject(&(&1 == ""))
  # 2. Convierte a uppercase con String.upcase
  # 3. Agrega prefijo "[PROCESSED] " con String.replace o <> operator
  # Retorna: {:noreply, transformed_events, state}
  def handle_events(events, _from, state) do
    transformed = events
      |> Enum.reject(fn event -> event == "" end)
      # TODO: |> Enum.map(&String.upcase/1)
      # TODO: |> Enum.map(&("[PROCESSED] " <> &1))

    # TODO: {:noreply, transformed, state}
  end
end

# Test de la transformación de forma aislada:
input = ["hello", "", "world", "elixir", ""]
expected_count = 3  # filtra los 2 vacíos

# La transformación debe resultar en:
# ["[PROCESSED] HELLO", "[PROCESSED] WORLD", "[PROCESSED] ELIXIR"]
```

---

### Exercise 4: Conectar el pipeline con sync_subscribe

```elixir
defmodule PipelineDemo do
  def run do
    # El pipeline completo:
    # StringSource -> StringTransformer -> LogConsumer

    # TODO 1: Define StringSource como producer de strings
    defmodule StringSource do
      use GenStage

      @words ["hello", "world", "", "elixir", "rocks", "", "functional", "programming"]

      def start_link(opts \\ []), do: GenStage.start_link(__MODULE__, @words, opts)

      def init(words), do: {:producer, words}

      def handle_demand(demand, []) do
        # Sin más datos, retorna lista vacía (el pipeline se detiene naturalmente)
        {:noreply, [], []}
      end

      def handle_demand(demand, words) do
        # TODO: toma min(demand, length(words)) elementos
        # PISTA: Enum.split(words, demand) retorna {tomados, resto}
        {to_emit, remaining} = # TODO
        {:noreply, to_emit, remaining}
      end
    end

    # TODO 2: Inicia los tres stages
    {:ok, source}      = StringSource.start_link()
    {:ok, transformer} = StringTransformer.start_link()
    {:ok, consumer}    = LogConsumer.start_link()

    # TODO 3: Suscribe el transformer al source
    # PISTA: GenStage.sync_subscribe(transformer, to: source, max_demand: 10)
    # TODO

    # TODO 4: Suscribe el consumer al transformer
    # PISTA: GenStage.sync_subscribe(consumer, to: transformer, max_demand: 10)
    # TODO

    # Espera que el pipeline procese todos los eventos
    :timer.sleep(500)

    # TODO 5: Verifica cuántos eventos procesó el consumer
    # Deben ser 6 (8 palabras - 2 strings vacíos filtrados por el transformer)
    count = LogConsumer.processed_count(consumer)
    IO.inspect(count)   # => 6

    # Output esperado en el consumer:
    # Processing: "[PROCESSED] HELLO"
    # Processing: "[PROCESSED] WORLD"
    # Processing: "[PROCESSED] ELIXIR"
    # Processing: "[PROCESSED] ROCKS"
    # Processing: "[PROCESSED] FUNCTIONAL"
    # Processing: "[PROCESSED] PROGRAMMING"
  end
end

PipelineDemo.run()
```

---

### Exercise 5: Backpressure en acción — consumer lento

```elixir
defmodule BackpressureDemo do
  @moduledoc """
  Demuestra que un consumer lento frena automáticamente al producer.
  Sin GenStage, el producer inundaría al consumer.
  Con GenStage, el producer solo emite cuando hay demanda.
  """

  defmodule FastProducer do
    use GenStage

    def start_link(opts \\ []) do
      GenStage.start_link(__MODULE__, 0, opts)
    end

    def init(n), do: {:producer, n}

    def handle_demand(demand, n) when demand > 0 do
      IO.puts("Producer: demand received for #{demand} events (state: #{n})")
      events = Enum.to_list(n + 1 .. n + demand)
      IO.puts("Producer: emitting #{length(events)} events")
      {:noreply, events, n + demand}
    end
  end

  defmodule SlowConsumer do
    use GenStage

    def start_link(delay_ms, opts \\ []) do
      GenStage.start_link(__MODULE__, delay_ms, opts)
    end

    # TODO 1: Implementa init/1
    # state = delay_ms (el tiempo de sleep simulando trabajo lento)
    # retorna {:consumer, delay_ms}
    def init(delay_ms) do
      # TODO
    end

    # TODO 2: Implementa handle_events/3
    # Para cada evento: duerme `state` ms (simula procesamiento lento)
    # Imprime "Consumer: processed event #{event}"
    # El consumer solo pide más eventos DESPUÉS de procesar los actuales
    # Esto crea backpressure: el producer no puede adelantarse
    def handle_events(events, _from, delay_ms) do
      Enum.each(events, fn event ->
        :timer.sleep(delay_ms)
        # TODO: IO.puts("Consumer: processed event #{event}")
      end)
      # TODO: {:noreply, [], delay_ms}
    end
  end

  def run do
    {:ok, producer} = FastProducer.start_link()
    {:ok, consumer} = SlowConsumer.start_link(100)  # 100ms por evento

    # TODO 3: Conecta consumer al producer con max_demand: 2
    # Esto significa el consumer pide máximo 2 eventos a la vez
    # Observa en el output cómo el producer ESPERA que el consumer procese
    # antes de emitir más eventos (backpressure natural)
    GenStage.sync_subscribe(consumer, to: producer, max_demand: # TODO)

    # Espera 1 segundo y observa cuántos eventos se procesaron
    # Con backpressure: ~5 eventos en 500ms (2 eventos × 100ms, luego 2 más, etc.)
    :timer.sleep(700)

    # TODO 4: Explica en un comentario por qué el producer no desborda
    # al consumer aunque el producer puede generar eventos infinitamente rápido.
    # ¿Qué mecanismo de GenStage lo previene?
    # RESPUESTA: ...
  end
end

BackpressureDemo.run()
```

---

## Common Mistakes

### Retornar eventos en el Consumer

```elixir
# MAL: el consumer retorna eventos (GenStage lanza error)
def handle_events(events, _from, state) do
  processed = Enum.map(events, &process/1)
  {:noreply, processed, state}   # ERROR: consumer no puede producir
end

# BIEN: el consumer siempre retorna lista vacía
def handle_events(events, _from, state) do
  Enum.each(events, &process/1)
  {:noreply, [], state}   # lista vacía obligatoria en consumer
end
```

### Olvidar agregar gen_stage como dependencia

```elixir
# GenStage no es parte de Elixir/OTP — es una librería externa
# mix.exs:
defp deps do
  [{:gen_stage, "~> 1.2"}]  # sin esto, 'use GenStage' falla
end
```

### Producer que retorna más eventos que la demanda

```elixir
# Técnicamente no es un error fatal, pero viola el contrato
# Si demand = 5 y retornas 100 eventos, GenStage los bufferiza internamente
# Es mejor respetar exactamente la demanda
def handle_demand(demand, state) do
  events = take_exactly(demand, state)   # no más de `demand`
  {:noreply, events, new_state}
end
```

### Sincronía en sync_subscribe — bloquea hasta que el producer responde

```elixir
# sync_subscribe es síncrono — espera a que el producer confirme
# Si el producer está muerto o bloqueado, sync_subscribe cuelga
# Alternativa: async_subscribe (no espera confirmación)
GenStage.async_subscribe(consumer, to: producer, max_demand: 10)
```

### Ignorar el manejo de estado vacío en el Producer

```elixir
# MAL: crashea cuando no hay más datos
def handle_demand(demand, []) do
  {to_emit, rest} = Enum.split([], demand)   # Enum.split de vacío es seguro
  {:noreply, to_emit, rest}   # retorna [] — correcto, el pipeline se detiene
end

# Si quieres señalar fin del stream, puedes llamar GenStage.cancel:
def handle_demand(_demand, []) do
  GenStage.cancel({:down, :normal})   # O simplemente retornar []
  {:noreply, [], []}
end
```

---

## Try It Yourself

Implementa un pipeline de procesamiento de logs con tres etapas:

```elixir
defmodule LogPipeline do
  @moduledoc """
  Pipeline: LogGenerator -> LogParser -> LogStorage

  LogGenerator (Producer):
  - Genera entradas de log como strings crudos
  - Formato: "LEVEL timestamp message"
  - Ejemplo: "ERROR 1712000000 Connection refused"

  LogParser (ProducerConsumer):
  - Parsea cada string y produce un map
  - %{level: :error, timestamp: 1712000000, message: "Connection refused"}
  - Filtra logs de nivel :debug (no los pasa al siguiente stage)

  LogStorage (Consumer):
  - Agrupa logs por nivel en su state
  - Mantiene un Map %{error: [...], warn: [...], info: [...]}
  - Expone get_stats/1 que retorna cuántos hay de cada nivel
  """

  defmodule LogGenerator do
    use GenStage

    @sample_logs [
      "INFO 1712000001 Application started",
      "DEBUG 1712000002 Loading config",
      "ERROR 1712000003 Database connection failed",
      "WARN 1712000004 Retry attempt 1",
      "INFO 1712000005 Reconnected successfully",
      "DEBUG 1712000006 Cache hit ratio: 0.92",
      "ERROR 1712000007 Timeout waiting for response",
      "WARN 1712000008 Memory usage above 80%",
      "INFO 1712000009 Request processed in 45ms",
    ]

    def start_link(opts \\ []), do: GenStage.start_link(__MODULE__, @sample_logs, opts)

    # TODO: Implementa init/1 y handle_demand/2
  end

  defmodule LogParser do
    use GenStage

    # TODO: Implementa init/1 y handle_events/3
    # parse_log/1 debe parsear "LEVEL timestamp message" en un map
    # Filtra :debug usando Enum.reject
    defp parse_log(raw) do
      # TODO: String.split(raw, " ", parts: 3) para obtener [level, ts, msg]
      # Convierte level a átomo lowercase: String.downcase |> String.to_atom
      # Convierte timestamp a entero: String.to_integer
    end
  end

  defmodule LogStorage do
    use GenStage

    def start_link(opts \\ []) do
      initial = %{error: [], warn: [], info: []}
      GenStage.start_link(__MODULE__, initial, opts)
    end

    # TODO: Implementa init/1, handle_events/3, y get_stats/1
    # handle_events agrupa cada log en state[log.level] = [log | state[log.level]]
    # get_stats/1 retorna %{error: count, warn: count, info: count}
  end

  def run do
    {:ok, generator} = LogGenerator.start_link()
    {:ok, parser}    = LogParser.start_link()
    {:ok, storage}   = LogStorage.start_link()

    # TODO: Conecta el pipeline
    GenStage.sync_subscribe(parser,   to: generator, max_demand: 5)
    GenStage.sync_subscribe(storage,  to: parser,    max_demand: 5)

    :timer.sleep(500)

    stats = LogStorage.get_stats(storage)
    IO.inspect(stats)
    # => %{error: 2, warn: 2, info: 3}  (debug logs filtrados)
  end
end

LogPipeline.run()
```

**Checklist**:
- [ ] `LogGenerator` emite los sample logs en orden, respetando la demanda
- [ ] `LogParser` parsea correctamente el formato "LEVEL timestamp message"
- [ ] `LogParser` filtra entradas de nivel `:debug` (no llegan a LogStorage)
- [ ] `LogStorage` agrupa por nivel y expone `get_stats/1`
- [ ] El pipeline maneja el fin natural de datos (producer vacío retorna `[]`)
- [ ] Backpressure funciona: el storage controla el ritmo del pipeline
