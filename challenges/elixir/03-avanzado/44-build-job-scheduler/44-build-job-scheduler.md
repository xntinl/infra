# 44 — Build a Job Scheduler (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 6-8 horas  
**Área**: GenServer · Cron · Task.Supervisor · Backoff · Concurrencia

---

## Contexto

Un scheduler de jobs es el corazón de cualquier sistema backend serio. Debe ejecutar tareas
periódicas (tipo cron), gestionar fallos con reintentos inteligentes, evitar que un job lento
bloquee a otros y proporcionar visibilidad del historial de ejecuciones. Construirás uno desde
cero, sin Quantum ni Oban, usando solo el runtime de Erlang/Elixir.

---

## Arquitectura propuesta

```
┌──────────────────────────────────────────────────────────────┐
│                  Scheduler.Server (GenServer)                │
│                                                              │
│  jobs:     %{job_id => %Job{}}                               │
│  timers:   %{job_id => timer_ref}      — :timer.send_after   │
│  history:  %{job_id => [%Execution{}]} — últimas N por job   │
│  dlq:      [%DLQEntry{}]               — jobs sin retries    │
└──────────────┬───────────────────────────────────────────────┘
               │
   ┌───────────┴──────────────────┐
   │                              │
┌──▼──────────────┐    ┌──────────▼─────────┐
│ CronParser      │    │ Task.Supervisor     │
│ "*/5 * * * *"   │    │ ejecuta jobs con   │
│ → next_run_in   │    │ concurrencia N     │
└─────────────────┘    └────────────────────┘
               │
    ┌──────────▼──────────┐
    │  BackoffCalculator  │
    │  1s, 2s, 4s, 8s... │
    └─────────────────────┘
```

### Modelo de datos

```elixir
defmodule Scheduler.Job do
  defstruct [
    :id,          # UUID o atom
    :fun,         # función de 0 aridad
    :schedule,    # {:cron, expr} | {:every, ms}
    :name,        # string descriptivo
    max_retries:  3,
    timeout_ms:   30_000,
    enabled:      true
  ]
end

defmodule Scheduler.Execution do
  defstruct [
    :job_id,
    :started_at,
    :finished_at,
    :duration_ms,
    :result,      # :ok | {:error, reason}
    :attempt      # 1..max_retries
  ]
end
```

---

## Ejercicio 1 — Scheduler básico con intervalos fijos

Implementa scheduling con `every: ms` y ejecución asíncrona de jobs.

### Interfaz pública

```elixir
{:ok, _} = Scheduler.start_link(max_concurrent: 5, history_size: 20)

job_id = Scheduler.schedule(
  fn -> IO.puts("Tick!") end,
  every: :timer.minutes(5),
  name: "heartbeat"
)

Scheduler.schedule(
  fn -> MyApp.cleanup_sessions() end,
  every: :timer.hours(1),
  name: "session_cleanup",
  max_retries: 3
)

Scheduler.cancel(job_id)       # => :ok
Scheduler.pause(job_id)        # => :ok — no ejecuta pero mantiene schedule
Scheduler.resume(job_id)       # => :ok — reprograma desde ahora
Scheduler.run_now(job_id)      # => :ok — fuerza ejecución inmediata
```

### Timer management

```elixir
# En el GenServer, para cada job programado:
defp schedule_next(job, state) do
  interval_ms = case job.schedule do
    {:every, ms}  -> ms
    {:cron, expr} -> CronParser.next_run_in_ms(expr)
  end

  timer_ref = Process.send_after(self(), {:run_job, job.id}, interval_ms)

  put_in(state.timers[job.id], timer_ref)
end

def handle_info({:run_job, job_id}, state) do
  job = state.jobs[job_id]
  execute_job(job, attempt: 1)
  state = schedule_next(job, state)   # reprogramar inmediatamente
  {:noreply, state}
end
```

### Requisitos

- Cada job tiene su propio timer — `Process.send_after(self(), {:run_job, id}, ms)`
- Al cancelar: `Process.cancel_timer(timer_ref)` y eliminar de `jobs` y `timers`
- `run_now/1`: cancela timer actual, envía `{:run_job, id}` inmediatamente, reprograma
- Ejecución en `Task.Supervisor` para no bloquear el GenServer
- Tests: job se ejecuta después del intervalo, cancel evita ejecución, run_now dispara inmediatamente

---

## Ejercicio 2 — Parser de expresiones Cron

Implementa un parser de cron expressions para scheduling por horario.

### Expresiones soportadas

```
"*/5 * * * *"    — cada 5 minutos
"0 * * * *"      — cada hora en punto
"0 9 * * 1"      — lunes a las 9am
"0 0 1 * *"      — primer día del mes a medianoche
"*/15 9-17 * * 1-5" — cada 15min de 9-17h en días laborales
```

### Estructura del parser

```elixir
defmodule Scheduler.CronParser do
  defstruct [:minute, :hour, :day, :month, :weekday]
  # Cada campo es una de:
  # :any           — "*"
  # {:every, n}    — "*/n"
  # {:range, a, b} — "a-b"
  # {:list, [n]}   — "1,3,5"
  # n (integer)    — valor literal

  def parse(expr) do
    [min, hr, day, mon, wday] = String.split(expr, " ")
    %__MODULE__{
      minute:  parse_field(min, 0..59),
      hour:    parse_field(hr,  0..23),
      day:     parse_field(day, 1..31),
      month:   parse_field(mon, 1..12),
      weekday: parse_field(wday, 0..6)
    }
  end

  @doc "Calcula milisegundos hasta la próxima ejecución desde DateTime.utc_now()"
  def next_run_in_ms(%__MODULE__{} = parsed) do
    now = DateTime.utc_now()
    next = find_next_datetime(parsed, now)
    DateTime.diff(next, now, :millisecond)
  end

  defp parse_field("*", _range), do: :any
  defp parse_field("*/" <> n, _range), do: {:every, String.to_integer(n)}
  # ... implementar range y list
end
```

### Requisitos

- Parser correcto para `*`, `*/n`, `n`, `a-b`, `a,b,c` en cada campo
- `next_run_in_ms/1` retorna ms hasta la próxima ocurrencia (siempre > 0)
- Si el cron debía correr hace 30 segundos, retorna ms hasta la SIGUIENTE ocurrencia
- Tests exhaustivos: tabla de expresiones conocidas con fechas de referencia fijas
- No usar librerías externas de cron parsing (implementar desde cero)

---

## Ejercicio 3 — Retry con backoff exponencial

Implementa lógica de retry cuando un job falla.

### Backoff exponencial

```
Intento 1: ejecuta → falla → espera 1s
Intento 2: ejecuta → falla → espera 2s
Intento 3: ejecuta → falla → espera 4s
Intento 4: ejecuta → falla → espera 8s
Intento max_retries+1: → va a DLQ
```

### Implementación del retry

```elixir
defmodule Scheduler.BackoffCalculator do
  @base_delay_ms 1_000
  @max_delay_ms  300_000  # 5 minutos máximo

  def delay_for_attempt(attempt) do
    delay = @base_delay_ms * :math.pow(2, attempt - 1) |> round()
    min(delay, @max_delay_ms)
  end

  def with_jitter(delay_ms) do
    jitter = :rand.uniform(div(delay_ms, 4))
    delay_ms + jitter
  end
end
```

### Flujo de ejecución con retry

```elixir
def handle_info({:execute_with_retry, job_id, attempt}, state) do
  job = state.jobs[job_id]

  result =
    try do
      Task.Supervisor.async_nolink(Scheduler.TaskSupervisor, job.fun)
      |> Task.await(job.timeout_ms)
      :ok
    rescue
      e -> {:error, Exception.message(e)}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end

  execution = %Execution{
    job_id:      job_id,
    started_at:  DateTime.utc_now(),
    result:      result,
    attempt:     attempt
  }

  state = record_execution(state, execution)

  case {result, attempt >= job.max_retries} do
    {:ok, _}    -> {:noreply, state}
    {_, true}   -> {:noreply, send_to_dlq(state, job, execution)}
    {err, false} ->
      delay = BackoffCalculator.delay_for_attempt(attempt + 1) |> BackoffCalculator.with_jitter()
      Process.send_after(self(), {:execute_with_retry, job_id, attempt + 1}, delay)
      {:noreply, state}
  end
end
```

### Requisitos

- Jitter en el backoff para evitar thundering herd
- Timeout por job: si la tarea no termina en `timeout_ms`, fuerza cancelación
- Cada intento genera una entrada en el historial del job
- Job en DLQ puede ser reintentado manualmente: `Scheduler.retry_dlq(job_id)`
- Tests: job que siempre falla → llega a DLQ con historial de intentos; job que falla 2 veces y luego funciona

---

## Ejercicio 4 — Concurrencia y observabilidad

Control de concurrencia máxima y visibilidad del estado del scheduler.

### Control de concurrencia

```elixir
# Máximo N jobs corriendo en paralelo
Scheduler.start_link(max_concurrent: 5)

# Si hay 5 corriendo y se dispara el 6to:
# Opción A: encolar y ejecutar cuando haya slot
# Opción B: skipar esta ejecución (log warning)
# Implementar Opción B como default, A como opción configurable
```

### Observabilidad

```elixir
Scheduler.list_jobs()
# => [
#   %{id: "cleanup", name: "session_cleanup", status: :running, next_run_in_ms: 42_000},
#   %{id: "heartbeat", name: "heartbeat", status: :scheduled, next_run_in_ms: 180_000},
#   %{id: "report", name: "report", status: :paused, next_run_in_ms: nil}
# ]

Scheduler.job_history("cleanup", limit: 10)
# => [
#   %Execution{started_at: ~U[...], duration_ms: 234, result: :ok, attempt: 1},
#   %Execution{started_at: ~U[...], duration_ms: 15_002, result: {:error, :timeout}, attempt: 1},
#   ...
# ]

Scheduler.dlq()
# => [%{job_id: "flaky_job", failed_at: ~U[...], last_error: "connection refused", attempts: 3}]

Scheduler.stats()
# => %{
#   total_jobs: 5,
#   running: 1,
#   scheduled: 3,
#   paused: 1,
#   dlq_count: 2,
#   executions_today: 142,
#   success_rate: 0.986
# }
```

### Requisitos

- Tracking de jobs actualmente en ejecución con contador atómico o ETS
- `duration_ms` calculado al completar la Task (`System.monotonic_time` diff)
- Historial limitado a `history_size` entradas por job (configurable en start)
- `success_rate` calculado sobre el total de ejecuciones del día (reset en medianoche)
- Tests: max_concurrent=2 con 5 jobs disparados simultáneamente, verificar solo 2 corren

### Estructura del proyecto

```
lib/
├── scheduler/
│   ├── application.ex       # Inicia Supervisor
│   ├── supervisor.ex        # Server + TaskSupervisor
│   ├── server.ex            # GenServer principal
│   ├── job.ex               # Struct + validación
│   ├── execution.ex         # Struct de resultado
│   ├── cron_parser.ex       # Parsing de cron expressions
│   └── backoff_calculator.ex
test/
├── scheduler/
│   ├── server_test.exs
│   ├── cron_parser_test.exs
│   ├── retry_test.exs
│   └── concurrency_test.exs
```

---

## Criterios de aceptación

- [ ] `schedule/2` con `every: ms` ejecuta el job en el intervalo correcto
- [ ] `schedule/2` con `cron: expr` ejecuta en los momentos correctos del cron
- [ ] Cron parser maneja `*`, `*/n`, `n`, `a-b` en todos los campos
- [ ] Un job que falla hace backoff exponencial y eventualmente va a DLQ
- [ ] `run_now/1` dispara ejecución inmediata sin afectar el schedule regular
- [ ] `pause/1` y `resume/1` funcionan correctamente
- [ ] `job_history/2` muestra intentos con duración y resultado
- [ ] `max_concurrent` evita ejecutar más de N jobs simultáneos

---

## Retos adicionales (opcional)

- Persistencia del estado a DETS/ETS para sobrevivir reinicios
- Scheduler distribuido: usar `:global` o Horde para un único scheduler en el cluster
- Priority queues: jobs con prioridad high corren antes que los normal
- Métricas con `:telemetry`: `[:scheduler, :job, :start]` y `[:scheduler, :job, :stop]`
