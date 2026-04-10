# 46 — Oban Background Jobs (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 5-6 horas  
**Área**: Oban · PostgreSQL · Queues · Workers · Scheduling

---

## Contexto

Oban es la librería estándar de background jobs en el ecosistema Elixir. Usa PostgreSQL como
backend, garantiza at-least-once delivery, soporta unique jobs, scheduling futuro y múltiples
queues con diferentes prioridades y concurrencias. En este capstone aprenderás a configurar Oban
correctamente y construir workers robustos del mundo real.

---

## Arquitectura propuesta

```
┌──────────────────────────────────────────────────────────────┐
│                     MyApp (Phoenix)                          │
│                                                              │
│  Context.Users.create_user/1                                 │
│    → EmailWorker.new(%{user_id: id}) |> Oban.insert()        │
│                                                              │
│  Context.Reports.generate/1                                  │
│    → ReportWorker.new(args, scheduled_at: +1hr) |> insert()  │
└──────────────────────────────────────────────────────────────┘
                           │
┌──────────────────────────▼───────────────────────────────────┐
│                     Oban                                     │
│                                                              │
│  Queue: :critical  (concurrency: 10)  → NotificationWorker  │
│  Queue: :default   (concurrency: 20)  → EmailWorker         │
│  Queue: :mailers   (concurrency: 5)   → EmailWorker (SMTP)  │
│  Queue: :reports   (concurrency: 2)   → ReportWorker        │
└──────────────────────────────────────────────────────────────┘
                           │
                    PostgreSQL
               oban_jobs / oban_peers
```

### Setup del proyecto

```elixir
# mix.exs
defp deps do
  [
    {:oban, "~> 2.17"},
    {:phoenix, "~> 1.7"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, ">= 0.0.0"}
  ]
end
```

---

## Ejercicio 1 — Configuración de Oban con múltiples queues

Configura Oban con 4 queues y la migración de base de datos correspondiente.

### Migración de Oban

```elixir
# priv/repo/migrations/YYYYMMDDHHMMSS_add_oban_jobs_table.exs
defmodule MyApp.Repo.Migrations.AddObanJobsTable do
  use Ecto.Migration

  def up,   do: Oban.Migration.up(version: 12)
  def down, do: Oban.Migration.down(version: 1)
end
```

### Configuración en config.exs

```elixir
# config/config.exs
config :my_app, Oban,
  repo: MyApp.Repo,
  plugins: [
    {Oban.Plugins.Pruner, max_age: 60 * 60 * 24 * 7},   # pruning: 7 días
    {Oban.Plugins.Lifeline, rescue_after: :timer.minutes(30)},
    Oban.Plugins.Stager
  ],
  queues: [
    critical: [limit: 10],
    default:  [limit: 20],
    mailers:  [limit: 5],
    reports:  [limit: 2]
  ]

# config/runtime.exs — en producción, deshabilitar en test
if config_env() == :test do
  config :my_app, Oban, testing: :inline
end
```

### Application setup

```elixir
defmodule MyApp.Application do
  def start(_type, _args) do
    children = [
      MyApp.Repo,
      {Oban, Application.fetch_env!(:my_app, Oban)},
      MyAppWeb.Endpoint
    ]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Requisitos

- Migración ejecutable con `mix ecto.migrate`
- 4 queues con concurrencias distintas que reflejen prioridades de negocio
- Pruner configurado para limpiar jobs completados > 7 días
- Lifeline para rescatar jobs stuck después de 30 minutos
- Tests: verificar que Oban inicia correctamente en test mode (`testing: :inline`)

---

## Ejercicio 2 — Los tres workers: Email, Report, Notification

Implementa tres workers con comportamientos distintos.

### EmailWorker

```elixir
defmodule MyApp.Workers.EmailWorker do
  use Oban.Worker,
    queue: :mailers,
    max_attempts: 5,
    unique: [period: 300, fields: [:args]]  # no duplicar mismo email en 5 min

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"type" => "welcome", "user_id" => user_id}}) do
    user = MyApp.Accounts.get_user!(user_id)
    MyApp.Mailer.send_welcome_email(user)
    # Retornar :ok implica éxito
    :ok
  end

  def perform(%Oban.Job{args: %{"type" => "password_reset", "email" => email, "token" => token}}) do
    MyApp.Mailer.send_password_reset(email, token)
    :ok
  end

  # Si retorna {:error, reason} → reintenta con backoff
  # Si retorna {:cancel, reason} → no reintenta (error permanente)
  # Si lanza excepción → reintenta con backoff
  def perform(%Oban.Job{args: %{"type" => type}}) do
    {:cancel, "unknown email type: #{type}"}
  end
end
```

### ReportWorker

```elixir
defmodule MyApp.Workers.ReportWorker do
  use Oban.Worker,
    queue: :reports,
    max_attempts: 3,
    unique: [period: 3600, fields: [:args, :worker]]

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"report_type" => type, "user_id" => uid, "format" => fmt}}) do
    with {:ok, data}   <- MyApp.Reports.generate(type, user_id: uid),
         {:ok, output} <- MyApp.Reports.render(data, format: fmt),
         :ok           <- MyApp.Storage.save_report(uid, output) do
      # Notificar al usuario que el reporte está listo
      NotificationWorker.new(%{
        user_id:  uid,
        type:     "report_ready",
        metadata: %{report_type: type, format: fmt}
      })
      |> Oban.insert()
      :ok
    else
      {:error, :user_not_found} -> {:cancel, "user #{uid} not found"}
      {:error, reason}          -> {:error, reason}  # retry
    end
  end

  # Timeout: matar el job si tarda más de 5 minutos
  def timeout(_job), do: :timer.minutes(5)
end
```

### NotificationWorker

```elixir
defmodule MyApp.Workers.NotificationWorker do
  use Oban.Worker,
    queue: :critical,
    max_attempts: 10,
    unique: [period: 60, fields: [:args]]

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"user_id" => uid, "type" => type} = args}) do
    user = MyApp.Accounts.get_user!(uid)

    channels_to_notify(user, type)
    |> Enum.each(fn channel ->
      notify(channel, user, type, args["metadata"] || %{})
    end)

    :ok
  end

  defp channels_to_notify(user, "report_ready"), do: [:email, :push]
  defp channels_to_notify(user, "payment_failed"), do: [:email, :sms, :push]
  defp channels_to_notify(_, _), do: [:push]

  defp notify(:push, user, type, metadata) do
    MyApp.PushNotifier.send(user.device_token, type, metadata)
  end
  defp notify(:email, user, type, metadata) do
    MyApp.Mailer.send_notification(user.email, type, metadata)
  end
  defp notify(:sms, user, type, metadata) do
    MyApp.SMS.send(user.phone, type, metadata)
  end
end
```

### Requisitos

- Cada worker tiene `max_attempts` apropiado para su criticidad
- `EmailWorker` y `NotificationWorker` usan unique jobs para evitar duplicados
- `ReportWorker` encadena un `NotificationWorker` al completar (job chaining)
- `{:cancel, reason}` para errores permanentes (usuario no existe, tipo inválido)
- `{:error, reason}` para errores transitorios (DB down, servicio externo)
- Tests en modo `testing: :inline` — verificar resultado sin queue real

---

## Ejercicio 3 — Unique Jobs y Scheduled Jobs

Patrones avanzados de Oban para evitar duplicados y scheduling futuro.

### Unique Jobs

```elixir
# Evitar duplicar el mismo email de bienvenida por usuario
EmailWorker.new(%{type: "welcome", user_id: 42})
|> Oban.insert()
# => {:ok, %Oban.Job{conflict?: false, ...}}

EmailWorker.new(%{type: "welcome", user_id: 42})
|> Oban.insert()
# => {:ok, %Oban.Job{conflict?: true, ...}}  — no inserta duplicado

# Unique por user_id + report_type (ignora otros args)
ReportWorker.new(%{report_type: "monthly", user_id: 5, format: "pdf"})
|> Oban.insert()
```

### Configuración de uniqueness

```elixir
# Unique en base a args específicos
use Oban.Worker,
  unique: [
    period: 3_600,              # ventana de 1 hora
    fields: [:args, :worker],   # considera args completos + nombre del worker
    keys: [:user_id, :type]     # solo estas keys de args (opcional)
  ]

# Unique que incluye estado (no encolar si ya hay uno en :available o :executing)
use Oban.Worker,
  unique: [
    period: :infinity,
    states: [:available, :scheduled, :executing, :retryable]
  ]
```

### Scheduled Jobs

```elixir
# Enviar recordatorio en 24 horas
EmailWorker.new(
  %{type: "subscription_reminder", user_id: user.id},
  scheduled_at: DateTime.add(DateTime.utc_now(), 24 * 3600, :second)
)
|> Oban.insert()

# Reporte mensual: primer día del mes a las 6am UTC
ReportWorker.new(
  %{report_type: "monthly", user_id: admin.id, format: "csv"},
  scheduled_at: next_first_of_month_at(~T[06:00:00])
)
|> Oban.insert()

# Helper para calcular fecha
defp next_first_of_month_at(time) do
  today = Date.utc_today()
  first_next = Date.beginning_of_month(Date.add(today, 32))
  DateTime.new!(first_next, time, "Etc/UTC")
end
```

### Requisitos

- Tests de unique: insertar el mismo job dos veces, verificar `conflict?: true` en el segundo
- Tests de scheduled: insertar con `scheduled_at` en el futuro, verificar estado `:scheduled`
- Usar `Oban.Testing.assert_enqueued/1` y `refute_enqueued/1` en tests
- Documentar cuándo usar `unique: [period: :infinity]` vs un período específico

---

## Ejercicio 4 — Monitoreo, Pruning y Telemetry

Observabilidad del sistema de jobs en producción.

### Telemetry handlers de Oban

```elixir
defmodule MyApp.ObanLogger do
  require Logger

  def attach do
    events = [
      [:oban, :job, :start],
      [:oban, :job, :stop],
      [:oban, :job, :exception],
      [:oban, :circuit, :open],
      [:oban, :circuit, :trip]
    ]

    :telemetry.attach_many("oban-logger", events, &handle_event/4, [])
  end

  def handle_event([:oban, :job, :stop], %{duration: duration}, meta, _) do
    ms = System.convert_time_unit(duration, :native, :millisecond)
    Logger.info(
      "[Oban] #{meta.worker} #{meta.queue} #{meta.state} #{ms}ms " <>
      "attempt=#{meta.attempt}/#{meta.max_attempts}"
    )
  end

  def handle_event([:oban, :job, :exception], _measurements, meta, _) do
    Logger.error(
      "[Oban] #{meta.worker} failed: #{inspect(meta.reason)}",
      job: meta.job,
      stacktrace: meta.stacktrace
    )
  end

  def handle_event([:oban, :circuit, :open], _, %{name: name}, _) do
    Logger.warning("[Oban] Circuit opened for queue: #{name}")
  end
end
```

### Queries de monitoreo

```elixir
defmodule MyApp.ObanMonitor do
  import Ecto.Query

  def queue_stats do
    from(j in Oban.Job,
      group_by: [j.queue, j.state],
      select: {j.queue, j.state, count(j.id)}
    )
    |> MyApp.Repo.all()
    |> Enum.group_by(fn {queue, _, _} -> queue end, fn {_, state, count} -> {state, count} end)
  end

  def failed_jobs(limit \\ 20) do
    from(j in Oban.Job,
      where: j.state == "discarded",
      order_by: [desc: j.attempted_at],
      limit: ^limit,
      select: %{worker: j.worker, args: j.args, errors: j.errors, attempted_at: j.attempted_at}
    )
    |> MyApp.Repo.all()
  end

  def retry_all_failed do
    from(j in Oban.Job, where: j.state == "discarded")
    |> MyApp.Repo.all()
    |> Enum.each(&Oban.retry_job(&1))
  end
end
```

### Oban.Web (conceptual)

```elixir
# En router.ex — dashboard web para monitoreo
if Mix.env() == :dev do
  scope "/" do
    pipe_through :browser
    forward "/oban", Oban.Web.Router
  end
end

# Oban.Web provee:
# - Dashboard de jobs por queue y estado
# - Retry manual de jobs discarded
# - Cancelar jobs pending/scheduled
# - Ver errores y stacktraces
# - Métricas de throughput en tiempo real
```

### Pruning automático

```elixir
# Plugin Pruner: limpia jobs en estados finales
{Oban.Plugins.Pruner,
  max_age: 60 * 60 * 24 * 7,     # 7 días para :completed
  limit: 10_000                   # máximo 10k jobs eliminados por ciclo
}

# Para compliance, puedes necesitar retener más tiempo:
{Oban.Plugins.Pruner,
  max_age: 60 * 60 * 24 * 90,    # 90 días
  states: [:discarded, :cancelled] # solo limpiar estos estados
}
```

### Requisitos

- Telemetry handler adjunto en `Application.start/2`
- `ObanMonitor.queue_stats/0` retorna mapa de queue → %{state => count}
- `ObanMonitor.retry_all_failed/0` funciona con `Oban.retry_job/1`
- Tests de workers con `Oban.Testing` helpers (`perform_job/2`, `assert_enqueued/1`)
- Documentar cuándo usar `testing: :inline` vs `testing: :manual`

### Estructura del proyecto

```
lib/
├── my_app/
│   ├── application.ex
│   ├── workers/
│   │   ├── email_worker.ex
│   │   ├── report_worker.ex
│   │   └── notification_worker.ex
│   └── oban_monitor.ex
├── my_app_web/
│   └── router.ex              # Oban.Web mount (dev only)
priv/
└── repo/migrations/
    └── *_add_oban_jobs_table.exs
test/
├── my_app/
│   └── workers/
│       ├── email_worker_test.exs
│       ├── report_worker_test.exs
│       └── notification_worker_test.exs
```

---

## Criterios de aceptación

- [ ] Migración de Oban ejecuta sin errores
- [ ] 4 queues configuradas con concurrencias correctas
- [ ] `EmailWorker` cancela en tipo desconocido, reintenta en error transitorio
- [ ] `ReportWorker` encadena `NotificationWorker` al completar
- [ ] Unique jobs previenen duplicados (verificado en tests)
- [ ] `scheduled_at` inserta jobs en estado `:scheduled`
- [ ] Telemetry handler loguea `start`, `stop` y `exception`
- [ ] Pruner configurado para limpiar jobs > 7 días
- [ ] Tests usan `Oban.Testing.assert_enqueued/1` correctamente

---

## Retos adicionales (opcional)

- Oban Pro: `Oban.Pro.Workers.Batch` para procesar lotes con callback on_complete
- Rate limiting a nivel de queue: `paused: true` + resume programático
- Metrics dashboard custom con LiveView que muestre throughput en tiempo real
- Testing con jobs reales en Sandbox PostgreSQL: `Oban.Testing.with_testing_mode(:manual, fn)`
