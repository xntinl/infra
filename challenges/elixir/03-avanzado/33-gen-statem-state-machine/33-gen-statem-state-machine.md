# 33 — :gen_statem y State Machines Complejas

**Nivel**: Avanzado  
**Tema**: Modelar comportamiento complejo con `:gen_statem` de Erlang

---

## Contexto

Cuando un sistema tiene **comportamiento que depende del estado actual**, modelarlo
con un GenServer normal lleva a código lleno de `if state == :X do ... end` disperso
por toda la lógica. `:gen_statem` (o su wrapper Elixir `GenStateMachine`) hace
el estado **explícito**: cada estado tiene sus propios handlers, y las transiciones
son eventos primero-clase.

### ¿Cuándo usar una state machine?

- El sistema tiene estados bien definidos y mutuamente excluyentes
- Las respuestas a los mismos mensajes difieren según el estado actual
- Las transiciones tienen efectos secundarios (timeouts, acciones)
- Necesitas diferir (postpone) eventos para el próximo estado

### `:gen_statem` vs `GenServer`

| | GenServer | :gen_statem |
|---|---|---|
| Estado | Un término opaco | Estado explícito + datos |
| Handlers | `handle_call/3`, `handle_cast/2` | Por estado o global |
| Timeouts | `{:noreply, state, timeout}` | State timeout, event timeout, generic timeout |
| Diferir eventos | Manual y error-prone | `postpone` built-in |
| Complejidad | Simple | Mayor setup, mayor claridad |

### Callback modes

`:gen_statem` ofrece dos modos:

**`state_functions`**: cada estado es una función separada.

```elixir
# La función se llama según el estado actual
def locked(:cast, :coin, data), do: {:next_state, :unlocked, data}
def locked(:cast, :push, data), do: {:keep_state, data}

def unlocked(:cast, :push, data), do: {:next_state, :locked, data}
def unlocked(:cast, :coin, data), do: {:keep_state, data}  # coin ignorado
```

**`handle_event_function`**: un solo `handle_event/4` con pattern matching.

```elixir
def handle_event(:cast, :coin, :locked,   data), do: {:next_state, :unlocked, data}
def handle_event(:cast, :push, :unlocked, data), do: {:next_state, :locked,   data}
def handle_event(_type, _event, _state,   data), do: {:keep_state, data}
```

### Acciones y transiciones

```elixir
# Retornos posibles de los handlers:
{:next_state, NewState, NewData}
{:next_state, NewState, NewData, Actions}
{:keep_state, NewData}
{:keep_state, NewData, Actions}
{:keep_state_and_data}          # shorthand
{:stop, Reason, NewData}
{:reply, From, Reply}           # acción para responder calls

# Actions — lista de cosas a hacer durante la transición:
[
  {:reply, from, :ok},                            # responder un call
  {:next_event, :cast, :internal_event},          # generar evento interno
  {:state_timeout, 5000, :timeout_expired},       # timeout de estado
  {:timeout, 1000, :generic_timeout},             # timeout genérico
  :postpone                                        # diferir el evento actual
]
```

### Tipos de timeout en :gen_statem

```elixir
# State timeout: se cancela automáticamente al cambiar de estado
{:state_timeout, ms, event_content}
# Llega como: :state_timeout

# Generic timeout (con nombre): persiste entre estados, se puede cancelar
{:timeout, ms, event_content}
# Llega como: :timeout

# Event timeout: se cancela si llega cualquier evento
{:event_timeout, ms, event_content}
# Llega como: :event_timeout

# Cancelar un timeout:
{:state_timeout, :cancel}
{:timeout, :cancel, :my_timer_name}
```

### Postpone — diferir eventos

`postpone` es una acción que hace que el evento actual sea "reprocesado" en el
próximo estado. Es útil cuando un evento llega "antes de tiempo" y quieres
manejarlo cuando el sistema esté listo.

```elixir
# Si estamos en estado :starting y llega un :request antes de estar listos:
def handle_event(:cast, :request, :starting, data) do
  {:keep_state, data, [:postpone]}
  # El :request se reintentará cuando el estado cambie a :ready
end
```

---

## Ejercicio 1 — Semáforo con Transiciones Temporizadas

Implementa un semáforo de tráfico usando `:gen_statem` con callback mode
`state_functions`. Las transiciones ocurren automáticamente por timeout.

### Estados y transiciones

```
:red ──(30s)──▶ :green ──(25s)──▶ :yellow ──(5s)──▶ :red
```

### Requisitos

- `start_link/0` — arranca en estado `:red`
- `current_state/1` — retorna el estado actual (sin cambiar nada)
- `force_transition/1` — fuerza la transición inmediata (para tests)
- Cada transición automática usa `{:state_timeout, ms, :transition}`
- El state timeout se cancela automáticamente al cambiar de estado (comportamiento nativo)
- Loguear cada transición con `Logger.info/1`

### Uso esperado

```elixir
{:ok, pid} = TrafficLight.start_link()
TrafficLight.current_state(pid)   #=> :red

# Después de 30 segundos:
TrafficLight.current_state(pid)   #=> :green

# Forzar transición:
TrafficLight.force_transition(pid)
TrafficLight.current_state(pid)   #=> :yellow
```

### Hints

<details>
<summary>Hint 1 — Estructura con :gen_statem y state_functions</summary>

```elixir
defmodule TrafficLight do
  @behaviour :gen_statem

  def start_link, do: :gen_statem.start_link(__MODULE__, :ok, [])
  def current_state(pid), do: :gen_statem.call(pid, :get_state)
  def force_transition(pid), do: :gen_statem.cast(pid, :force)

  @impl true
  def callback_mode, do: :state_functions

  @impl true
  def init(:ok) do
    # {:ok, initial_state, initial_data, [initial_actions]}
    {:ok, :red, %{}, [{:state_timeout, 30_000, :transition}]}
  end
end
```
</details>

<details>
<summary>Hint 2 — Funciones de estado para :red</summary>

```elixir
def red({:call, from}, :get_state, data) do
  {:keep_state, data, [{:reply, from, :red}]}
end

def red(:cast, :force, data) do
  Logger.info("TrafficLight: red → green (forced)")
  {:next_state, :green, data, [{:state_timeout, 25_000, :transition}]}
end

def red(:state_timeout, :transition, data) do
  Logger.info("TrafficLight: red → green (auto)")
  {:next_state, :green, data, [{:state_timeout, 25_000, :transition}]}
end
```

Repite el patrón para `:green` y `:yellow` con sus duraciones y estados destino.
</details>

<details>
<summary>Hint 3 — Estado :yellow y el ciclo completo</summary>

```elixir
def yellow(:state_timeout, :transition, data) do
  Logger.info("TrafficLight: yellow → red (auto)")
  {:next_state, :red, data, [{:state_timeout, 30_000, :transition}]}
end

def yellow(:cast, :force, data) do
  Logger.info("TrafficLight: yellow → red (forced)")
  {:next_state, :red, data, [{:state_timeout, 30_000, :transition}]}
end

def yellow({:call, from}, :get_state, data) do
  {:keep_state, data, [{:reply, from, :yellow}]}
end
```

El state timeout se cancela automáticamente cuando el estado cambia —
no necesitas cancelarlo manualmente.
</details>

---

## Ejercicio 2 — TCP Connection State Machine

Simula el handshake TCP usando `:gen_statem` con `handle_event_function`.
No es un TCP real — simulamos los estados del protocolo recibiendo "eventos" de red.

### Estados del protocolo TCP simplificado

```
:closed ──(:listen)──▶ :listen ──(:syn)──▶ :syn_received ──(:ack)──▶ :established
                                                                              │
                                                                         (:fin)
                                                                              ▼
                                                                          :closed
```

### Requisitos

- `start_link/0` — arranca en `:closed`
- `listen/1` — evento que mueve `:closed` → `:listen`
- `receive_syn/1` — llega SYN, mueve `:listen` → `:syn_received` (envía SYN-ACK)
- `receive_ack/1` — llega ACK, mueve `:syn_received` → `:established`
- `send_data/2` — envía datos (sólo funciona en `:established`)
- `close/1` — cierra la conexión (desde cualquier estado → `:closed`)
- State timeout en `:syn_received` de 30 segundos — si no llega ACK, vuelve a `:listen`
- Usar `handle_event_function` para ver cómo difiere de `state_functions`

### Uso esperado

```elixir
{:ok, pid} = TcpStateMachine.start_link()

TcpStateMachine.listen(pid)        #=> :ok
TcpStateMachine.receive_syn(pid)   #=> {:ok, :syn_ack_sent}
TcpStateMachine.receive_ack(pid)   #=> {:ok, :established}
TcpStateMachine.send_data(pid, "Hello!")  #=> {:ok, :sent}

# Intentar enviar datos en estado incorrecto:
{:ok, pid2} = TcpStateMachine.start_link()
TcpStateMachine.send_data(pid2, "Hello!")  #=> {:error, :not_established}
```

### Hints

<details>
<summary>Hint 1 — Callback mode y handle_event/4</summary>

```elixir
defmodule TcpStateMachine do
  @behaviour :gen_statem

  def callback_mode, do: :handle_event_function

  # handle_event(event_type, event_content, current_state, data)
  def handle_event({:call, from}, :listen, :closed, data) do
    {:next_state, :listen, data, [{:reply, from, :ok}]}
  end

  def handle_event({:call, from}, :listen, state, data) when state != :closed do
    {:keep_state, data, [{:reply, from, {:error, {:already, state}}}]}
  end
end
```

La ventaja de `handle_event_function`: puedes usar guards para estados múltiples
sin duplicar código. La desventaja: el `handle_event/4` puede volverse largo.
</details>

<details>
<summary>Hint 2 — SYN, SYN-ACK, y state timeout</summary>

```elixir
def handle_event({:call, from}, :receive_syn, :listen, data) do
  # Simular envío de SYN-ACK
  IO.puts("TCP: Enviando SYN-ACK")
  actions = [
    {:reply, from, {:ok, :syn_ack_sent}},
    {:state_timeout, 30_000, :handshake_timeout}
  ]
  {:next_state, :syn_received, data, actions}
end

def handle_event(:state_timeout, :handshake_timeout, :syn_received, data) do
  IO.puts("TCP: Handshake timeout, volviendo a :listen")
  {:next_state, :listen, data}
end
```

Al transicionar a `:established`, el state timeout de `:syn_received` se cancela
automáticamente — no necesitas hacer nada.
</details>

<details>
<summary>Hint 3 — close desde cualquier estado y catchall</summary>

```elixir
# close funciona desde cualquier estado
def handle_event({:call, from}, :close, _state, data) do
  {:next_state, :closed, data, [{:reply, from, :ok}]}
end

# send_data sólo en :established
def handle_event({:call, from}, {:send_data, _data}, :established, data) do
  IO.puts("TCP: Enviando datos")
  {:keep_state, data, [{:reply, from, {:ok, :sent}}]}
end

def handle_event({:call, from}, {:send_data, _}, _state, data) do
  {:keep_state, data, [{:reply, from, {:error, :not_established}}]}
end

# Catchall: ignorar eventos no manejados
def handle_event(_type, _event, _state, data) do
  {:keep_state, data}
end
```
</details>

---

## Ejercicio 3 — Circuit Breaker como State Machine

Implementa el patrón **Circuit Breaker** usando `:gen_statem`. El circuit breaker
protege llamadas a servicios externos que pueden fallar, evitando cascadas de errores.

### Estados y lógica

```
:closed ──(failures >= threshold)──▶ :open ──(timeout)──▶ :half_open
   ▲                                                            │
   └──────────────(success)────────────────────────────────────┘
   ▲                                                            │
   └──────────────(failure)──────────────────────────── :open ─┘
```

- **`:closed`** — normal. Deja pasar las llamadas. Cuenta fallos consecutivos.
- **`:open`** — rechaza todas las llamadas inmediatamente (`{:error, :circuit_open}`).
  Después de un timeout (ej. 10 segundos), transiciona a `:half_open`.
- **`:half_open`** — deja pasar UNA llamada de prueba. Si tiene éxito → `:closed`.
  Si falla → `:open` con timeout reiniciado.

### Requisitos

- `start_link/1` — opciones: `threshold: N, timeout: ms`
- `call/2` — `call(cb, fun)` donde `fun` es una función `0-arity` que puede lanzar o retornar `{:error, _}`
- Configuración via data del state machine (no hardcoded)
- En `:closed`: contar fallos consecutivos. Un éxito resetea el contador.
- En `:open`: state timeout para transicionar a `:half_open`
- En `:half_open`: postpone si llega segunda llamada mientras la prueba está en vuelo

### Uso esperado

```elixir
{:ok, cb} = CircuitBreaker.start_link(threshold: 3, timeout: 10_000)

# Llamadas normales en :closed
CircuitBreaker.call(cb, fn -> {:ok, "resultado"} end)  #=> {:ok, "resultado"}

# Simular 3 fallos → abre el circuito
for _ <- 1..3 do
  CircuitBreaker.call(cb, fn -> {:error, "fallo"} end)
end

# Circuito abierto — rechaza sin llamar a la función
CircuitBreaker.call(cb, fn -> {:ok, "debería ejecutarse"} end)
#=> {:error, :circuit_open}

# Después de 10s → :half_open → una llamada de prueba exitosa → :closed
```

### Hints

<details>
<summary>Hint 1 — Estado inicial y estructura de datos</summary>

```elixir
defmodule CircuitBreaker do
  @behaviour :gen_statem

  defstruct [:threshold, :timeout_ms, :failure_count, :test_in_flight]

  def start_link(opts) do
    data = %__MODULE__{
      threshold:    Keyword.get(opts, :threshold, 5),
      timeout_ms:   Keyword.get(opts, :timeout, 10_000),
      failure_count: 0,
      test_in_flight: false
    }
    :gen_statem.start_link(__MODULE__, data, [])
  end

  def callback_mode, do: :handle_event_function

  def init(data), do: {:ok, :closed, data}
end
```
</details>

<details>
<summary>Hint 2 — Estado :closed con contador y apertura</summary>

```elixir
def handle_event({:call, from}, {:call, fun}, :closed, data) do
  result = safe_call(fun)
  case result do
    {:ok, _} ->
      {:keep_state, %{data | failure_count: 0}, [{:reply, from, result}]}
    {:error, _} ->
      new_count = data.failure_count + 1
      if new_count >= data.threshold do
        actions = [
          {:reply, from, result},
          {:state_timeout, data.timeout_ms, :open_timeout}
        ]
        {:next_state, :open, %{data | failure_count: 0}, actions}
      else
        {:keep_state, %{data | failure_count: new_count}, [{:reply, from, result}]}
      end
  end
end

defp safe_call(fun) do
  try do
    case fun.() do
      {:error, _} = err -> err
      result -> {:ok, result}
    end
  rescue
    e -> {:error, Exception.message(e)}
  end
end
```
</details>

<details>
<summary>Hint 3 — Estado :open y :half_open con postpone</summary>

```elixir
# :open — rechazar todo
def handle_event({:call, from}, {:call, _fun}, :open, data) do
  {:keep_state, data, [{:reply, from, {:error, :circuit_open}}]}
end

def handle_event(:state_timeout, :open_timeout, :open, data) do
  {:next_state, :half_open, %{data | test_in_flight: false}}
end

# :half_open — una llamada de prueba, postpone las demás
def handle_event({:call, from}, {:call, fun}, :half_open, %{test_in_flight: false} = data) do
  result = safe_call(fun)
  case result do
    {:ok, _} ->
      {:next_state, :closed, %{data | test_in_flight: false, failure_count: 0},
       [{:reply, from, result}]}
    {:error, _} ->
      actions = [
        {:reply, from, result},
        {:state_timeout, data.timeout_ms, :open_timeout}
      ]
      {:next_state, :open, %{data | test_in_flight: false}, actions}
  end
end

def handle_event({:call, _from}, {:call, _fun}, :half_open, %{test_in_flight: true} = data) do
  # Diferir la llamada hasta que el estado cambie
  {:keep_state, data, [:postpone]}
end
```
</details>

---

## Trade-offs a considerar

### `state_functions` vs `handle_event_function`

| | `state_functions` | `handle_event_function` |
|---|---|---|
| Organización | Por estado (una función por estado) | Por evento (lógica agrupada por evento) |
| Código compartido | Repetición o helpers privados | `handle_event` con guards |
| Legibilidad | Clara cuando los estados tienen lógica muy diferente | Clara cuando el mismo evento se maneja igual en múltiples estados |
| Tooling (dialyzer) | Más fácil de typespec | Más difícil |

Para la mayoría de state machines con pocos estados y muchos eventos compartidos,
`handle_event_function` resulta más conciso. Para máquinas con muchos estados y
lógica radicalmente diferente por estado, `state_functions` es más legible.

### Postpone — una herramienta poderosa pero cara

`postpone` reinserta el evento en la cola de eventos para el próximo estado.
Si el estado cambia frecuentemente sin procesar el evento postponed, el evento
se reinserta repetidamente — O(n) por cambio de estado. Usar postpone sólo cuando:
- El evento es válido pero llegó "antes de tiempo"
- El tiempo en el estado intermedio es corto y predecible

### :gen_statem vs GenStateMachine (Hex)

[GenStateMachine](https://hex.pm/packages/gen_state_machine) es un wrapper Elixir
de `:gen_statem` que ofrece sintaxis más idiomática. Si usas `:gen_statem` directamente,
defines `@behaviour :gen_statem` y usas atoms Erlang para los callback modes.
En proyectos Elixir puros, considera la librería — agrega ergonomía sin overhead.

### Circuit Breaker — state machine vs proceso estadístico

El circuit breaker que implementamos es determinístico. Una implementación de producción
podría usar una ventana deslizante para el conteo de errores (no sólo consecutivos),
o medir latencia P99 además de errores binarios. La state machine no cambia —
sólo el criterio de transición `:closed` → `:open` se vuelve más sofisticado.

---

## One possible solution

<details>
<summary>Ver solución (spoiler)</summary>

```elixir
# Ejercicio 1: TrafficLight
defmodule TrafficLight do
  @behaviour :gen_statem
  require Logger

  def start_link, do: :gen_statem.start_link(__MODULE__, :ok, [])
  def current_state(pid), do: :gen_statem.call(pid, :get_state)
  def force_transition(pid), do: :gen_statem.cast(pid, :force)

  @impl true
  def callback_mode, do: :state_functions

  @impl true
  def init(:ok), do: {:ok, :red, %{}, [{:state_timeout, 30_000, :transition}]}

  def red({:call, from}, :get_state, data),
    do: {:keep_state, data, [{:reply, from, :red}]}
  def red(:cast, :force, data) do
    Logger.info("red → green (forced)")
    {:next_state, :green, data, [{:state_timeout, 25_000, :transition}]}
  end
  def red(:state_timeout, :transition, data) do
    Logger.info("red → green (auto)")
    {:next_state, :green, data, [{:state_timeout, 25_000, :transition}]}
  end

  def green({:call, from}, :get_state, data),
    do: {:keep_state, data, [{:reply, from, :green}]}
  def green(:cast, :force, data) do
    Logger.info("green → yellow (forced)")
    {:next_state, :yellow, data, [{:state_timeout, 5_000, :transition}]}
  end
  def green(:state_timeout, :transition, data) do
    Logger.info("green → yellow (auto)")
    {:next_state, :yellow, data, [{:state_timeout, 5_000, :transition}]}
  end

  def yellow({:call, from}, :get_state, data),
    do: {:keep_state, data, [{:reply, from, :yellow}]}
  def yellow(:cast, :force, data) do
    Logger.info("yellow → red (forced)")
    {:next_state, :red, data, [{:state_timeout, 30_000, :transition}]}
  end
  def yellow(:state_timeout, :transition, data) do
    Logger.info("yellow → red (auto)")
    {:next_state, :red, data, [{:state_timeout, 30_000, :transition}]}
  end
end

# Ejercicio 3: CircuitBreaker
defmodule CircuitBreaker do
  @behaviour :gen_statem

  defstruct [:threshold, :timeout_ms, failure_count: 0]

  def start_link(opts) do
    data = %__MODULE__{
      threshold:  Keyword.get(opts, :threshold, 5),
      timeout_ms: Keyword.get(opts, :timeout, 10_000)
    }
    :gen_statem.start_link(__MODULE__, data, [])
  end

  def call(cb, fun), do: :gen_statem.call(cb, {:call, fun})

  @impl true
  def callback_mode, do: :handle_event_function

  @impl true
  def init(data), do: {:ok, :closed, data}

  @impl true
  def handle_event({:call, from}, {:call, fun}, :closed, data) do
    case safe_call(fun) do
      {:ok, _} = ok ->
        {:keep_state, %{data | failure_count: 0}, [{:reply, from, ok}]}
      {:error, _} = err ->
        count = data.failure_count + 1
        if count >= data.threshold do
          {:next_state, :open, %{data | failure_count: 0},
           [{:reply, from, err}, {:state_timeout, data.timeout_ms, :try_half_open}]}
        else
          {:keep_state, %{data | failure_count: count}, [{:reply, from, err}]}
        end
    end
  end

  def handle_event({:call, from}, {:call, _}, :open, data),
    do: {:keep_state, data, [{:reply, from, {:error, :circuit_open}}]}

  def handle_event(:state_timeout, :try_half_open, :open, data),
    do: {:next_state, :half_open, data}

  def handle_event({:call, from}, {:call, fun}, :half_open, data) do
    case safe_call(fun) do
      {:ok, _} = ok ->
        {:next_state, :closed, %{data | failure_count: 0}, [{:reply, from, ok}]}
      {:error, _} = err ->
        {:next_state, :open, data,
         [{:reply, from, err}, {:state_timeout, data.timeout_ms, :try_half_open}]}
    end
  end

  defp safe_call(fun) do
    try do
      case fun.() do
        {:error, _} = e -> e
        v -> {:ok, v}
      end
    rescue
      e -> {:error, Exception.message(e)}
    end
  end
end
```

</details>
