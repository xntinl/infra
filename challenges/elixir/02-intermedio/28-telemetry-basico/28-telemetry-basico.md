# 28. Telemetry Básico

**Difficulty**: Intermedio

## Prerequisites
- Completed exercises 01–27
- Understanding of GenServer and process-based architecture
- Familiarity with anonymous functions and maps
- Basic understanding of observability concepts (metrics, logging)

## Learning Objectives
After completing this exercise, you will be able to:
- Emitir eventos de telemetría con `:telemetry.execute/3`
- Registrar handlers que responden a eventos con `:telemetry.attach/4`
- Desregistrar handlers con `:telemetry.detach/1`
- Instrumentar funciones propias midiendo duración y resultados
- Asociar múltiples handlers a un mismo evento
- Construir un sistema básico de métricas usando telemetría

## Concepts

### ¿Qué es Telemetry?

Telemetry es una librería de Erlang/OTP (disponible en Elixir como dependencia de Hex) que provee un bus de eventos de instrumentación. Los módulos emiten eventos con mediciones y metadatos, y los handlers se suscriben a esos eventos para procesar (loguear, enviar a Prometheus, calcular estadísticas, etc.).

El modelo es similar a un sistema publish/subscribe, pero síncrono y de baja latencia. Telemetry es la base de la instrumentación en Phoenix, Ecto, Oban, Broadway, y prácticamente toda librería seria del ecosistema Elixir.

```
Código de aplicación → :telemetry.execute → Handlers suscritos
                                               ├── Logger handler
                                               ├── Metrics aggregator
                                               └── Alerting handler
```

### :telemetry.execute/3: emitir eventos

Un evento de telemetría tiene tres partes:

1. **Nombre del evento**: una lista de átomos que forma un path, como `[:my_app, :repo, :query]`
2. **Measurements**: un map con valores numéricos medidos, como `%{duration: 45, result_count: 12}`
3. **Metadata**: un map con contexto adicional, como `%{query: "SELECT ...", table: "users"}`

```elixir
# Emitir un evento cuando termina una request HTTP
:telemetry.execute(
  [:my_app, :http, :request, :stop],      # nombre del evento
  %{duration: 123, status: 200},           # measurements (numéricos)
  %{method: "GET", path: "/api/users"}     # metadata (cualquier dato)
)
```

La convención de nombres en el ecosistema Elixir es:
- `[:app, :component, :action, :start]` — inicio de operación
- `[:app, :component, :action, :stop]` — fin de operación (con duración)
- `[:app, :component, :action, :exception]` — cuando ocurrió un error

### :telemetry.attach/4: suscribir handlers

`attach/4` registra una función que se llamará cada vez que se emita el evento especificado.

```elixir
:telemetry.attach(
  "my-handler-id",                          # ID único del handler (string)
  [:my_app, :http, :request, :stop],        # evento al que se suscribe
  &MyApp.Metrics.handle_event/4,            # función handler
  %{some: "config"}                         # configuración del handler (se pasa al handler)
)
```

La función handler tiene la firma:

```elixir
def handle_event(event_name, measurements, metadata, config) do
  # event_name: [:my_app, :http, :request, :stop]
  # measurements: %{duration: 123, status: 200}
  # metadata: %{method: "GET", path: "/api/users"}
  # config: %{some: "config"} (lo que pasaste a attach/4)
  :ok
end
```

### :telemetry.detach/1: desregistrar handlers

`detach/1` quita un handler por su ID. Es importante desregistrar handlers cuando un proceso que los usa termina, para evitar llamadas a funciones de módulos ya descargados.

```elixir
:telemetry.detach("my-handler-id")
```

### Instrumentar funciones: medir duración

La forma más común de instrumentar código es medir el tiempo de ejecución de una operación:

```elixir
defmodule MyApp.Instrumented do
  def perform(arg) do
    start_time = System.monotonic_time()

    result = do_the_work(arg)

    duration = System.monotonic_time() - start_time

    :telemetry.execute(
      [:my_app, :perform, :stop],
      %{duration: duration},
      %{arg: arg, result: inspect(result)}
    )

    result
  end
end
```

El tiempo se mide con `System.monotonic_time/0` (no se ve afectado por cambios en el reloj del sistema). La unidad es la nativa del sistema (normalmente nanosegundos), convertible con `System.convert_time_unit/3`.

### :telemetry.span/3: instrumentación automática start/stop

`telemetry.span/3` es un helper que emite automáticamente eventos `:start`, `:stop`, y `:exception`:

```elixir
:telemetry.span(
  [:my_app, :database, :query],   # prefijo del evento
  %{query: "SELECT ..."},          # metadata de start
  fn ->
    result = execute_query()
    {result, %{rows: length(result)}}   # {resultado, measurements adicionales}
  end
)
# Emite automáticamente:
# [:my_app, :database, :query, :start]   con measurements: %{system_time: ...}
# [:my_app, :database, :query, :stop]    con measurements: %{duration: ..., rows: ...}
# [:my_app, :database, :query, :exception] si hubo excepción
```

### Setup como dependencia

```elixir
# mix.exs
defp deps do
  [{:telemetry, "~> 1.2"}]
end
```

---

## Exercises

### Exercise 1: Execute básico — emitir tu primer evento

```elixir
defmodule TelemetryBasics do
  def run do
    # TODO 1: Emite un evento de telemetría que representa una petición HTTP completada
    # Nombre del evento: [:my_app, :request, :stop]
    # Measurements: %{duration: 45, status_code: 200}
    # Metadata: %{path: "/api/users", method: "GET", user_id: 123}
    :telemetry.execute(
      # TODO: lista de átomos para el nombre
      # TODO: map de measurements
      # TODO: map de metadata
    )

    # TODO 2: Emite un evento para una consulta a base de datos
    # Nombre: [:my_app, :repo, :query, :stop]
    # Measurements: %{duration: 12, result_count: 5}
    # Metadata: %{source: "users", query: "SELECT * FROM users WHERE id = $1"}
    :telemetry.execute(
      # TODO
    )

    # TODO 3: Emite un evento para un error de procesamiento
    # Nombre: [:my_app, :worker, :exception]
    # Measurements: %{count: 1}
    # Metadata: %{error: :timeout, job_id: "job-42", queue: "emails"}
    # TODO

    # TODO 4: ¿Qué pasa si no hay ningún handler registrado para el evento?
    # Ejecuta el evento y verifica que NO lanza excepción
    # (telemetry simplemente descarta el evento si nadie lo escucha)
    :telemetry.execute([:no_one, :listening], %{value: 1}, %{})
    IO.puts("OK: execute sin handlers no lanza excepción")

    # TODO 5: Emite un evento con measurements vacíos y metadata vacía
    # (ambos son obligatorios como maps, pero pueden ser vacíos)
    :telemetry.execute([:minimal, :event], %{}, %{})
    IO.puts("OK: execute con maps vacíos es válido")
  end
end

TelemetryBasics.run()
```

---

### Exercise 2: Attach handler — registrar y recibir eventos

```elixir
defmodule TelemetryHandler do
  @moduledoc """
  Módulo con handlers de telemetría para diferentes tipos de eventos.
  """

  # TODO 1: Implementa handle_http_event/4 que recibe eventos HTTP
  # y los imprime en formato legible:
  # "[HTTP] GET /api/users → 200 (45ms)"
  def handle_http_event(event_name, measurements, metadata, _config) do
    duration_ms = # TODO: convierte measurements.duration a ms
    # System.convert_time_unit(measurements.duration, :native, :millisecond)
    # (si usaste System.monotonic_time) o simplemente usa el valor directamente

    IO.puts("[HTTP] #{metadata.method} #{metadata.path} → #{measurements.status_code} (#{measurements.duration}ms)")
  end

  # TODO 2: Implementa handle_db_event/4 que recibe eventos de DB
  # Formato: "[DB] query on 'users' returned 5 rows in 12ms"
  def handle_db_event(_event_name, measurements, metadata, _config) do
    # TODO
  end

  # TODO 3: Implementa register_all/0 que registra ambos handlers
  # usando :telemetry.attach con IDs únicos:
  # - "http-logger" para [:my_app, :request, :stop]
  # - "db-logger" para [:my_app, :repo, :query, :stop]
  def register_all do
    :telemetry.attach(
      "http-logger",
      [:my_app, :request, :stop],
      # TODO: referencia a handle_http_event/4
      %{}
    )

    :telemetry.attach(
      "db-logger",
      # TODO: evento de DB
      &__MODULE__.handle_db_event/4,
      %{}
    )
  end

  def run do
    # TODO 4: Registra los handlers
    register_all()

    # Emite eventos y verifica que se disparan los handlers
    :telemetry.execute(
      [:my_app, :request, :stop],
      %{duration: 45, status_code: 200},
      %{path: "/api/users", method: "GET"}
    )
    # Output esperado: "[HTTP] GET /api/users → 200 (45ms)"

    :telemetry.execute(
      [:my_app, :repo, :query, :stop],
      %{duration: 12, result_count: 5},
      %{source: "users", query: "SELECT ..."}
    )
    # Output esperado: "[DB] query on 'users' returned 5 rows in 12ms"

    # TODO 5: Verifica que attach retorna :ok en éxito
    # y {:error, :already_exists} si intentas registrar el mismo ID dos veces
    result = :telemetry.attach("http-logger", [:any, :event], &handle_http_event/4, %{})
    IO.inspect(result)   # => {:error, :already_exists}
  end
end

TelemetryHandler.run()
```

---

### Exercise 3: Detach — desregistrar handlers

```elixir
defmodule TelemetryDetach do
  def log_handler(event, measurements, metadata, _config) do
    IO.puts("EVENT: #{inspect(event)} | #{inspect(measurements)}")
  end

  def run do
    # Registra el handler
    :telemetry.attach("detach-demo", [:demo, :event], &log_handler/4, %{})

    # Este execute SÍ dispara el handler
    :telemetry.execute([:demo, :event], %{value: 1}, %{})
    # Output: "EVENT: [:demo, :event] | %{value: 1}"

    # TODO 1: Desregistra el handler usando su ID
    # PISTA: :telemetry.detach("detach-demo")
    result = # TODO
    IO.inspect(result)   # => :ok

    # TODO 2: Emite el mismo evento de nuevo
    # Verifica que YA NO se imprime nada (el handler fue desregistrado)
    :telemetry.execute([:demo, :event], %{value: 2}, %{})
    IO.puts("(si no ves 'EVENT:' arriba de este mensaje, detach funcionó)")

    # TODO 3: ¿Qué retorna detach si el ID no existe?
    result2 = :telemetry.detach("handler-que-no-existe")
    IO.inspect(result2)   # => {:error, :not_found}

    # TODO 4: Implementa safe_detach/1 que nunca falla
    # Si el handler existe, lo quita y retorna :ok
    # Si no existe, retorna :ok igual (idempotente)
    IO.inspect(safe_detach("cualquier-id"))   # => :ok siempre

    # TODO 5: Prueba el patrón típico de lifecycle:
    # register → usar → detach (en una función de test o cleanup)
    with_handler("temp-handler", [:temp, :event], fn ->
      :telemetry.execute([:temp, :event], %{x: 42}, %{})
    end)
    # El handler es registrado, la función se ejecuta, luego se detach automáticamente
  end

  # TODO: Implementa safe_detach/1
  def safe_detach(handler_id) do
    # TODO: llama :telemetry.detach y maneja ambos casos (:ok y {:error, :not_found})
  end

  # TODO: Implementa with_handler/3 que registra un handler temporal,
  # ejecuta fun, y siempre desregistra al final (aunque fun lance excepción)
  def with_handler(id, event, fun) do
    :telemetry.attach(id, event, &log_handler/4, %{})
    try do
      fun.()
    after
      :telemetry.detach(id)
    end
  end
end

TelemetryDetach.run()
```

---

### Exercise 4: Instrumentar una función — medir duración real

```elixir
defmodule InstrumentedDB do
  @moduledoc """
  Simula una base de datos con operaciones instrumentadas con telemetría.
  """

  # TODO 1: Implementa query/2 que:
  # 1. Registra el tiempo de inicio con System.monotonic_time()
  # 2. Ejecuta do_query(table, filter)
  # 3. Calcula la duración
  # 4. Emite [:my_app, :db, :query, :stop] con:
  #    measurements: %{duration: duration_native, rows: length(result)}
  #    metadata: %{table: table, filter: filter}
  # 5. Retorna el resultado de do_query
  def query(table, filter \\ %{}) do
    start = # TODO: System.monotonic_time()
    result = do_query(table, filter)
    duration = # TODO: System.monotonic_time() - start

    :telemetry.execute(
      # TODO: nombre del evento
      # TODO: measurements
      # TODO: metadata
    )

    result
  end

  # TODO 2: Implementa insert/2 usando el mismo patrón
  # Evento: [:my_app, :db, :insert, :stop]
  # Measurements: %{duration: duration}
  # Metadata: %{table: table, record: record}
  def insert(table, record) do
    # TODO
  end

  # TODO 3: Implementa measure/2, una función de orden superior
  # que envuelve cualquier función con telemetría
  # measure([:my_app, :cache, :get], fn -> Cache.get(key) end)
  def measure(event_name, fun) when is_function(fun, 0) do
    start = System.monotonic_time()
    result = fun.()
    duration = System.monotonic_time() - start
    # TODO: emite event_name con %{duration: duration} y %{}
    result
  end

  # Simulaciones de operaciones de DB
  defp do_query(table, _filter) do
    :timer.sleep(:rand.uniform(50))  # simula latencia
    [{:id, 1, :table, table}, {:id, 2, :table, table}]
  end

  defp do_insert(_table, record) do
    :timer.sleep(:rand.uniform(20))
    Map.put(record, :id, :rand.uniform(1000))
  end

  def run do
    # Registra un handler que loguea las duraciones
    :telemetry.attach_many(
      "db-metrics",
      [
        [:my_app, :db, :query, :stop],
        [:my_app, :db, :insert, :stop]
      ],
      fn event, %{duration: d} = _m, meta, _ ->
        ms = System.convert_time_unit(d, :native, :millisecond)
        action = List.last(List.delete_at(event, -1))
        IO.puts("[DB Metrics] #{action} on #{meta.table}: #{ms}ms")
      end,
      %{}
    )

    # TODO 4: Llama query y insert y verifica que los handlers se disparan
    result1 = query("users", %{active: true})
    IO.inspect(length(result1), label: "rows returned")

    result2 = insert("orders", %{user_id: 1, amount: 99.99})
    IO.inspect(result2, label: "inserted record")

    # TODO 5: Usa measure/2 para instrumentar una operación inline
    measure([:my_app, :computation, :stop], fn ->
      :timer.sleep(10)
      Enum.sum(1..1000)
    end)
  end
end

InstrumentedDB.run()
```

---

### Exercise 5: Múltiples handlers para el mismo evento

```elixir
defmodule MultiHandlerDemo do
  @moduledoc """
  Demuestra que múltiples handlers pueden suscribirse al mismo evento.
  Cada handler es independiente y se ejecuta en el mismo proceso que ejecutó execute/3.
  """

  # Handler 1: logger
  def logger_handler(_event, measurements, metadata, _config) do
    # TODO 1: Imprime un log estructurado:
    # "[LOG] event=request.stop status=200 duration=45ms path=/api"
    IO.puts("[LOG] event=request.stop status=#{measurements.status_code} duration=#{measurements.duration}ms path=#{metadata.path}")
  end

  # Handler 2: contador de requests por status code
  def counter_handler(_event, measurements, _metadata, config) do
    # TODO 2: Incrementa un contador en un Agent o ETS
    # Por simplicidad, usa el Agent pasado en config
    # config = %{agent: agent_pid}
    agent = config.agent
    Agent.update(agent, fn counts ->
      status = measurements.status_code
      Map.update(counts, status, 1, &(&1 + 1))
    end)
  end

  # Handler 3: alertas para status codes de error
  def alert_handler(_event, measurements, metadata, _config) do
    # TODO 3: Si status_code >= 500, imprime una alerta:
    # "[ALERT] 5xx error: GET /api/data"
    if measurements.status_code >= 500 do
      IO.puts("[ALERT] #{measurements.status_code} error: #{metadata.method} #{metadata.path}")
    end
  end

  def run do
    # Inicia el Agent para el contador
    {:ok, agent} = Agent.start_link(fn -> %{} end)

    # TODO 4: Registra los 3 handlers para el mismo evento
    # [:my_app, :http, :request, :stop]
    # IDs: "http-logger", "http-counter", "http-alert"
    :telemetry.attach("http-logger",  [:my_app, :http, :request, :stop], &logger_handler/4, %{})
    :telemetry.attach("http-counter", [:my_app, :http, :request, :stop], &counter_handler/4, %{agent: agent})
    :telemetry.attach("http-alert",   [:my_app, :http, :request, :stop], # TODO: función + config)

    # Simula varias requests
    events = [
      {200, "GET",  "/api/users"},
      {200, "POST", "/api/users"},
      {404, "GET",  "/api/missing"},
      {500, "GET",  "/api/broken"},
      {200, "GET",  "/api/users"},
      {503, "POST", "/api/data"},
    ]

    Enum.each(events, fn {status, method, path} ->
      :telemetry.execute(
        [:my_app, :http, :request, :stop],
        %{duration: :rand.uniform(200), status_code: status},
        %{method: method, path: path}
      )
    end)

    # Verifica el estado del contador
    counts = Agent.get(agent, & &1)
    IO.inspect(counts)
    # => %{200 => 3, 404 => 1, 500 => 1, 503 => 1}

    # TODO 5: Desregistra todos los handlers al final
    Enum.each(["http-logger", "http-counter", "http-alert"], &:telemetry.detach/1)
    IO.puts("All handlers detached")
  end
end

MultiHandlerDemo.run()
```

---

## Common Mistakes

### Usar strings en el nombre del evento

```elixir
# MAL: el nombre debe ser una lista de átomos
:telemetry.execute("my_app.request.stop", %{duration: 10}, %{})
# Esto no falla inmediatamente pero los handlers no se dispararán
# porque están suscritos a listas de átomos

# BIEN:
:telemetry.execute([:my_app, :request, :stop], %{duration: 10}, %{})
```

### Handler que lanza excepción silencia los handlers posteriores

```elixir
# Si tu handler lanza una excepción, Telemetry la captura y loguea como warning
# pero el evento sigue propagándose a otros handlers
# No confíes en que el orden de ejecución es garantizado
def my_handler(_event, measurements, _meta, _config) do
  # Si esto lanza, otros handlers SÍ se ejecutarán pero este ya no
  Map.fetch!(measurements, :required_key)
end
```

### Registrar el mismo handler ID dos veces

```elixir
# MAL: attach con ID ya existente retorna {:error, :already_exists}
# y el handler ORIGINAL sigue activo (el nuevo se ignora)
:telemetry.attach("my-handler", [:event], &fun/4, %{})
:telemetry.attach("my-handler", [:event], &other_fun/4, %{})   # silently ignored!

# BIEN: usa IDs únicos o detach antes de volver a registrar
:telemetry.detach("my-handler")
:telemetry.attach("my-handler", [:event], &other_fun/4, %{})
```

### Measurements con tipos no numéricos

```elixir
# MAL: measurements debe contener solo valores numéricos
# (la librería de métricas no puede agregar strings)
:telemetry.execute([:event], %{status: "success"}, %{})

# BIEN: measurements para números, metadata para el resto
:telemetry.execute([:event], %{duration: 45, count: 1}, %{status: "success"})
```

### Olvidar detach en tests o procesos de vida corta

```elixir
# Si un proceso registra un handler y termina sin hacer detach,
# el handler permanece en la tabla de telemetría pero apunta a
# una función de un módulo posiblemente descargado
# En tests: siempre detach en on_exit
setup do
  :telemetry.attach("test-handler", [:event], &handler/4, %{})
  on_exit(fn -> :telemetry.detach("test-handler") end)
end
```

---

## Try It Yourself

Implementa un sistema de métricas que registra automáticamente el número de llamadas y el tiempo promedio de ejecución para cada función instrumentada.

```elixir
defmodule MetricsCollector do
  @moduledoc """
  Sistema de métricas basado en Telemetry que:
  - Cuenta llamadas por evento
  - Calcula duración promedio
  - Expone un reporte con get_report/0
  """
  use GenServer

  # API
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)
  def get_report, do: GenServer.call(__MODULE__, :get_report)
  def reset, do: GenServer.cast(__MODULE__, :reset)

  @impl true
  def init(_) do
    # TODO: Registra un handler wildcard... no existe en telemetry
    # En cambio, registra handlers para los eventos específicos que quieres trackear
    # Para este ejercicio, trackea: [:my_app, :db, :query, :stop]
    # y [:my_app, :http, :request, :stop]
    :telemetry.attach("metrics-db", [:my_app, :db, :query, :stop], &handle_event/4, %{collector: self()})
    :telemetry.attach("metrics-http", [:my_app, :http, :request, :stop], &handle_event/4, %{collector: self()})

    {:ok, %{}}
  end

  # El handler envía el evento al GenServer para procesarlo
  def handle_event(event_name, %{duration: duration} = _measurements, _metadata, %{collector: pid}) do
    event_key = Enum.join(event_name, ".")
    GenServer.cast(pid, {:record, event_key, duration})
  end

  @impl true
  def handle_cast({:record, event_key, duration}, state) do
    # TODO: Actualiza el state para event_key
    # State tiene forma: %{"my_app.db.query.stop" => %{count: N, total_duration: X}}
    # Calcula el promedio como total_duration / count cuando se pide el reporte
    new_entry = case Map.get(state, event_key) do
      nil ->
        # TODO: nueva entrada con count: 1, total_duration: duration
      existing ->
        # TODO: incrementa count y suma duration
    end
    {:noreply, Map.put(state, event_key, new_entry)}
  end

  @impl true
  def handle_cast(:reset, _state), do: {:noreply, %{}}

  @impl true
  def handle_call(:get_report, _from, state) do
    report = Map.new(state, fn {event, %{count: count, total_duration: total}} ->
      avg_ms = System.convert_time_unit(div(total, count), :native, :millisecond)
      {event, %{calls: count, avg_duration_ms: avg_ms}}
    end)
    {:reply, report, state}
  end

  @impl true
  def terminate(_reason, _state) do
    :telemetry.detach("metrics-db")
    :telemetry.detach("metrics-http")
  end
end

# Demo
{:ok, _} = MetricsCollector.start_link()

# Simula eventos
for _ <- 1..5 do
  :telemetry.execute([:my_app, :db, :query, :stop],
    %{duration: System.monotonic_time()},
    %{table: "users"})
  :timer.sleep(10)
end

for _ <- 1..3 do
  :telemetry.execute([:my_app, :http, :request, :stop],
    %{duration: System.monotonic_time(), status_code: 200},
    %{path: "/api"})
end

:timer.sleep(100)

report = MetricsCollector.get_report()
IO.inspect(report)
# => %{
#   "my_app.db.query.stop" => %{calls: 5, avg_duration_ms: N},
#   "my_app.http.request.stop" => %{calls: 3, avg_duration_ms: N}
# }
```

**Checklist**:
- [ ] `MetricsCollector` se inicia como GenServer y registra handlers en `init/1`
- [ ] `handle_event/4` envía datos al GenServer vía `GenServer.cast`
- [ ] El state acumula `count` y `total_duration` por evento
- [ ] `get_report/0` calcula el promedio correctamente
- [ ] `reset/0` vacía las métricas acumuladas
- [ ] `terminate/2` desregistra ambos handlers al cerrarse el GenServer
- [ ] Los handlers no fallan si `duration` no está en measurements (cláusula de guardia)
