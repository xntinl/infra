# 42 — Build an Event Bus (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 5-7 horas  
**Área**: GenServer · Registry · PubSub · Concurrencia

---

## Contexto

Los sistemas distribuidos modernos se comunican mediante eventos. Un Event Bus robusto desacopla
productores de consumidores, soporta patrones de fanout, filtrado por tópico y tolera fallos en
handlers individuales sin afectar al resto del sistema.

En este capstone construirás un Event Bus en Elixir puro, sin Phoenix.PubSub ni librerías externas,
que sea supervisado, persistente en memoria y observable mediante métricas en tiempo real.

---

## Arquitectura propuesta

```
┌──────────────────────────────────────────────────────┐
│                  EventBus (GenServer)                │
│                                                      │
│  subscriptions: %{topic => [{pid, handler_fn}]}      │
│  history:       %{topic => :queue.queue()}           │
│  dlq:           :queue.queue()                       │
│  metrics:       %{events_total, failed, rate}        │
└──────────┬────────────────────────┬─────────────────┘
           │ publish                │ subscribe
    ┌──────▼──────┐          ┌──────▼──────┐
    │  Producer   │          │  Consumer   │
    │  (any proc) │          │  (handler)  │
    └─────────────┘          └─────────────┘
           │ wildcard match
    ┌──────▼──────────────────────────────────┐
    │  TopicMatcher                           │
    │  "orders.*" → matches "orders.created" │
    └─────────────────────────────────────────┘
           │ on handler failure
    ┌──────▼──────┐
    │  Dead Letter│
    │  Queue (DLQ)│
    └─────────────┘
```

### Árbol de supervisión

```
EventBusApp
└── EventBus.Supervisor
    ├── EventBus.Server          (GenServer principal)
    ├── EventBus.MetricsSampler  (GenServer — calcula rate c/1s)
    └── EventBus.DLQWorker       (GenServer — reintenta DLQ)
```

---

## Ejercicio 1 — Core PubSub básico

Implementa el GenServer principal con suscripción y publicación sin wildcards.

### Interfaz pública

```elixir
EventBus.subscribe("orders.created", fn event -> IO.inspect(event) end)
EventBus.subscribe("orders.created", fn event -> MyModule.handle(event) end)

EventBus.publish("orders.created", %{order_id: 42, amount: 99.9})
# => Ambos handlers reciben el evento de forma asíncrona

EventBus.unsubscribe("orders.created", handler_fn)
```

### Requisitos

- Suscripciones almacenadas como `%{topic => [{ref, handler_fn}]}` — `ref` permite unsubscribe
- `publish/2` no bloquea al productor — despacha con `Task.start/1` por handler
- Monitor a los pids de los suscriptores; limpiar suscripciones si el proceso muere
- El GenServer nunca debe crashear por un handler defectuoso
- Tests: suscripción múltiple, publicación fanout, cleanup al morir el suscriptor

### Esqueleto de módulo

```elixir
defmodule EventBus.Server do
  use GenServer

  defstruct subscriptions: %{}, history: %{}, dlq: :queue.new(), metrics: %{}

  # --- API pública ---
  def subscribe(topic, handler_fn), do: GenServer.call(__MODULE__, {:subscribe, topic, handler_fn})
  def unsubscribe(topic, ref),      do: GenServer.cast(__MODULE__, {:unsubscribe, topic, ref})
  def publish(topic, event),        do: GenServer.cast(__MODULE__, {:publish, topic, event})

  # --- Callbacks ---
  def init(_), do: {:ok, %__MODULE__{}}

  def handle_cast({:publish, topic, event}, state) do
    # 1. Encontrar handlers para este topic
    # 2. Despachar con Task.start — nunca bloquear
    # 3. Actualizar history
    # 4. Actualizar métricas
    {:noreply, state}
  end

  def handle_info({:DOWN, ref, :process, pid, _}, state) do
    # Limpiar suscripciones del proceso muerto
    {:noreply, state}
  end
end
```

---

## Ejercicio 2 — Wildcard topic matching

Añade soporte para suscripciones con wildcards tipo `"orders.*"` y `"#"` (match-all).

### Reglas de matching

| Pattern       | Matchea                                     |
|---------------|---------------------------------------------|
| `"orders.*"`  | `"orders.created"`, `"orders.updated"`      |
| `"*.created"` | `"orders.created"`, `"users.created"`       |
| `"#"`         | Todo                                        |
| `"orders.#"`  | `"orders.created"`, `"orders.items.added"` |
| `"orders.created"` | Solo ese topic exacto                  |

### Requisitos

- Módulo separado `EventBus.TopicMatcher` con función `matches?(pattern, topic) :: boolean`
- Patrones compilados a regex o split/match eficiente — no regex en el hot path
- `publish/2` evalúa todos los patrones registrados contra el topic publicado
- Benchmark: 10_000 subscriptions, 1_000 publishes/s — latencia < 5ms p99
- Tests: tabla completa de casos de matching, incluyendo edge cases

### Implementación de TopicMatcher

```elixir
defmodule EventBus.TopicMatcher do
  @doc """
  Compila un pattern a una estructura eficiente para matching.
  Llama a compile/1 al registrar la suscripción, no en cada publish.
  """
  def compile(pattern) do
    pattern
    |> String.split(".")
    |> Enum.map(fn
      "*" -> :single  # matchea exactamente un segmento
      "#" -> :multi   # matchea cero o más segmentos
      seg -> seg      # literal
    end)
  end

  def matches?(compiled_pattern, topic) do
    segments = String.split(topic, ".")
    do_match(compiled_pattern, segments)
  end

  defp do_match([], []), do: true
  defp do_match([:multi | _], _), do: true
  defp do_match([:single | rest_p], [_ | rest_t]), do: do_match(rest_p, rest_t)
  defp do_match([seg | rest_p], [seg | rest_t]), do: do_match(rest_p, rest_t)
  defp do_match(_, _), do: false
end
```

---

## Ejercicio 3 — Historial y Dead Letter Queue

Añade persistencia en memoria del historial de eventos y manejo de handlers fallidos.

### Historial por topic

```elixir
EventBus.history("orders.created", limit: 10)
# => [%{event: %{...}, published_at: ~U[2024-01-01 ...], topic: "orders.created"}, ...]

EventBus.history("orders.*", limit: 50)
# => eventos de todos los topics que matcheen el patrón
```

### Dead Letter Queue

```elixir
EventBus.dlq_list()
# => [{topic, event, error, failed_at, attempt_count}]

EventBus.dlq_retry_all()
# => {:ok, retried: 3, still_failed: 1}

EventBus.dlq_purge()
# => :ok
```

### Requisitos

- Historial: `:queue.queue()` por topic, capacidad configurable (default 100)
- Cuando un handler lanza excepción: rescatar, loguear y añadir a DLQ
- DLQ entries incluyen: topic, event original, error, timestamp, intentos
- `DLQWorker` reintenta la DLQ cada 30 segundos con backoff por intento
- La DLQ tiene capacidad máxima — si se llena, descarta los más antiguos
- Tests: handler que falla, verificar aparición en DLQ, reintento exitoso

### Gestión del historial

```elixir
defp add_to_history(history, topic, event_entry, max_history) do
  queue = Map.get(history, topic, :queue.new())
  queue = :queue.in(event_entry, queue)
  queue =
    if :queue.len(queue) > max_history do
      {_, q} = :queue.out(queue)  # drop el más antiguo
      q
    else
      queue
    end
  Map.put(history, topic, queue)
end
```

---

## Ejercicio 4 — Métricas y observabilidad

Implementa métricas en tiempo real accesibles como datos estructurados.

### API de métricas

```elixir
EventBus.metrics()
# => %{
#      events_total: 15_432,
#      events_per_second: 142.3,
#      active_subscriptions: 48,
#      failed_handlers: 23,
#      dlq_depth: 5,
#      topics: %{
#        "orders.created" => %{published: 5_123, subscribers: 3},
#        "users.signup"   => %{published: 892,   subscribers: 1}
#      }
#    }
```

### MetricsSampler

```elixir
defmodule EventBus.MetricsSampler do
  use GenServer

  # Muestrea cada 1000ms, calcula events/second como delta
  def init(_) do
    :timer.send_interval(1_000, :sample)
    {:ok, %{last_total: 0}}
  end

  def handle_info(:sample, %{last_total: last} = state) do
    current = EventBus.Server.get_events_total()
    rate = current - last
    EventBus.Server.update_rate(rate)
    {:noreply, %{state | last_total: current}}
  end
end
```

### Requisitos

- Contadores atómicos con `:atomics` para events_total y failed_handlers (no bloquear el GenServer)
- `events_per_second` calculado por MetricsSampler como ventana deslizante de 1 segundo
- `topics` como mapa interno actualizado en cada publish (ETS opcional para lectura concurrente)
- Endpoint de inspección: `EventBus.inspect_subscription(ref)` devuelve stats por suscriptor
- Tests de métricas: publicar N eventos, verificar contadores correctos

### Referencia de implementación completa

```
lib/
├── event_bus/
│   ├── application.ex        # Application callback, inicia Supervisor
│   ├── supervisor.ex         # Supervisor con Server + Sampler + DLQWorker
│   ├── server.ex             # GenServer principal
│   ├── topic_matcher.ex      # Wildcard matching
│   ├── dlq_worker.ex         # Retry de DLQ en background
│   └── metrics_sampler.ex    # Rate calculation
test/
├── event_bus/
│   ├── server_test.exs
│   ├── topic_matcher_test.exs
│   ├── dlq_test.exs
│   └── metrics_test.exs
```

---

## Criterios de aceptación

- [ ] `subscribe/2`, `publish/2`, `unsubscribe/2` funcionan correctamente
- [ ] Wildcards `*` y `#` matchean según la tabla de reglas
- [ ] Un handler que lanza excepción no afecta a otros handlers del mismo evento
- [ ] El historial por topic limita a N entradas (configurable)
- [ ] La DLQ captura fallos y permite reintentos manuales y automáticos
- [ ] `metrics/0` devuelve `events_per_second` actualizado cada segundo
- [ ] El árbol de supervisión reinicia componentes sin perder suscripciones (state recovery)
- [ ] Tests cubren: fanout, wildcard, DLQ, cleanup de procesos muertos, métricas

---

## Retos adicionales (opcional)

- Persistencia de historial a DETS para sobrevivir reinicios
- Filtros por predicado: `subscribe("orders.*", filter: fn e -> e.amount > 100 end, handler: fn)`
- Back-pressure: si un handler tarda > N ms, pausar publicaciones para ese suscriptor
- Tracing con `:telemetry` en cada publish/subscribe
