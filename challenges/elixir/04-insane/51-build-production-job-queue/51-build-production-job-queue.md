# 51. Build a Production Job Queue (Oban-like)

**Difficulty**: Insane

## Prerequisites

- Ecto y PostgreSQL: queries, transacciones, `select ... for update skip locked`
- GenServer, Supervisor trees, y DynamicSupervisor
- Cron expressions: parsing y evaluación de schedules
- Telemetry: `telemetry` library, eventos y métricas
- Comprensión de `LISTEN/NOTIFY` de PostgreSQL para notificaciones push
- Manejo de concurrencia: limitación de workers por queue, semáforos

## Problem Statement

Construye un sistema de background job processing respaldado por PostgreSQL, con API similar a Oban, que garantice at-least-once delivery, retry con backoff exponencial, jobs únicos, scheduling cron, y telemetry completo.

El diseño fundamental: los jobs viven en una tabla PostgreSQL (`jobs`). Insertar un job es una transacción de base de datos — si la transacción que crea el job hace rollback, el job no existe. Esto resuelve el problema clásico de "inserté el job pero la transacción falló". El job queue usa `SELECT ... FOR UPDATE SKIP LOCKED` para dequeue atómicamente: múltiples workers pueden ejecutar este query concurrentemente y cada job es reclamado por exactamente un worker.

El ciclo de vida de un job: `available` → `executing` → (`completed` | `retryable` | `discarded`). Un job `retryable` tiene `scheduled_at` en el futuro (backoff exponencial); cuando llega su momento, vuelve a `available`. Un job `discarded` alcanzó `max_attempts` y no se reintenta más.

Los workers son módulos que implementan `use MyQueue.Worker`. El worker declara su queue y configuración (`max_attempts`, backoff function). El job se despacha al worker correcto según el campo `worker` en la fila de la base de datos. El sistema descubre workers disponibles en tiempo de arranque via `Application.spec`.

La **unicidad** previene jobs duplicados: un job único tiene un `unique_key` (hash de sus args) y un `unique_period` (ventana de tiempo). Si intentas insertar un job cuyo `unique_key` existe en estado no-discarded dentro de la ventana, la inserción devuelve `{:ok, %Job{conflict: true}}` sin insertar duplicado.

**PostgreSQL LISTEN/NOTIFY** reemplaza el polling: cuando un job se inserta, un trigger de PostgreSQL emite `NOTIFY jobs_new`. El queue escucha este canal y despacha inmediatamente, sin esperar el siguiente ciclo de polling.

## Acceptance Criteria

- [ ] Worker API: `use MyQueue.Worker, queue: :default, max_attempts: 3` inyecta el comportamiento; el módulo implementa `perform(%Job{})` que retorna `:ok`, `{:ok, result}`, `{:error, reason}`, o lanza una excepción; cualquier resultado no-ok o excepción en `perform` marca el job como `retryable` si quedan intentos, o `discarded` si no; el worker puede declarar `backoff(attempt)` para personalizar el tiempo entre reintentos
- [ ] Múltiples queues: el sistema soporta queues con nombres y concurrencias configurables: `queues: [default: 10, critical: 25, bulk: 2]`; cada queue tiene su propio pool de workers (Tasks supervisados); los workers de una queue no compiten con los de otra; la concurrencia es el máximo de jobs ejecutándose simultáneamente en esa queue; cambiar la concurrencia en runtime es posible sin reiniciar
- [ ] Scheduled jobs: `MyQueue.Worker.new(args, scheduled_at: ~U[2026-05-01 08:00:00Z])` inserta el job con `state: available` y `scheduled_at` en el futuro; el poller solo toma jobs donde `scheduled_at <= now() AND state = 'available'`; `schedule_in: 3600` es un alias para `scheduled_at: DateTime.add(DateTime.utc_now(), 3600, :second)`; los jobs scheduled pueden cancelarse antes de su ejecución con `cancel_job(id)`
- [ ] Retry con backoff: cuando `perform` falla, el sistema incrementa `attempt` y calcula el próximo `scheduled_at` usando la función de backoff del worker (default: `15 * 2 ** attempt` segundos); el error y stacktrace se guardan en `errors` (lista jsonb en la fila); cuando `attempt == max_attempts`, el estado pasa a `discarded`; el campo `errors` contiene el historial completo de intentos con timestamps para diagnóstico
- [ ] Unique jobs: `MyQueue.Worker.new(args, unique: [period: 60, fields: [:args, :queue]])` calcula un hash SHA-256 de los campos especificados; si existe un job con ese hash en estado `available`, `executing`, o `retryable` dentro del período (en segundos), no inserta y retorna `{:ok, %Job{conflict: true, conflict_job_id: id}}`; la unicidad usa un índice parcial de PostgreSQL para eficiencia; el período `0` significa dedup indefinido hasta que el job se complete o descarte
- [ ] Estados del job: la tabla `jobs` tiene columna `state` con valores `available`, `executing`, `completed`, `retryable`, `discarded`, `cancelled`; las transiciones son unidireccionales excepto `retryable → available` (cuando llega la hora del retry); `executing` tiene `attempted_at` y `attempted_by` (node + pid string); jobs en `executing` durante más de `execution_timeout` sin heartbeat se regresan a `available` automáticamente (rescue de jobs huérfanos)
- [ ] Pruning: `MyQueue.Pruner` es un proceso que corre periódicamente (configurable, default cada hora) y elimina jobs `completed` y `discarded` más viejos que la ventana de retención (default 7 días para completed, 30 días para discarded); la configuración es `retain_completed_for: {7, :days}`, `retain_discarded_for: {30, :days}`; el pruner procesa en batches para evitar locks de tabla masivos; emite eventos de telemetry con el número de jobs eliminados
- [ ] Cron jobs: `use MyQueue.Worker, cron: "0 8 * * *", timezone: "America/New_York"` registra el worker para ejecución periódica; el scheduler evalúa qué cron workers deben dispararse cada minuto; usa unicidad implícita para evitar disparar el mismo cron job dos veces en el mismo minuto (si el scheduler corre en múltiples nodos); soporta sintaxis cron estándar de 5 campos y las extensiones `@hourly`, `@daily`, `@weekly`; los cron jobs respetan la timezone configurada para calcular el próximo disparo
- [ ] Telemetry: emite eventos `:telemetry.execute([:my_queue, :job, :start], measurements, metadata)` al inicio; `[:my_queue, :job, :stop]` al completar (con `duration` en microsegundos y `result: :success | :failure`); `[:my_queue, :job, :exception]` en excepciones (con `kind`, `reason`, `stacktrace`); `[:my_queue, :queue, :poll]` en cada ciclo del poller (con `queue`, `jobs_found`); `[:my_queue, :pruner, :run]` en cada ciclo del pruner (con `deleted_count`); los metadata incluyen el `%Job{}` completo para facilitar logging
- [ ] LISTEN/NOTIFY: un trigger PostgreSQL en `INSERT INTO jobs` emite `NOTIFY jobs_available, '<queue_name>'`; el sistema mantiene una conexión dedicada a PostgreSQL en modo `LISTEN jobs_available`; cuando llega la notificación, el poller de esa queue se activa inmediatamente para tomar el job; esto reduce la latencia desde segundos (polling interval) a milisegundos; el sistema funciona correctamente aunque NOTIFY se pierda (el polling periódico es el fallback)

## What You Will Learn

- Por qué PostgreSQL es un message broker legítimo: `FOR UPDATE SKIP LOCKED` como primitiva de queue y sus garantías transaccionales
- At-least-once delivery: la diferencia entre at-most-once, at-least-once y exactly-once, y por qué el último es casi imposible sin coordinación externa
- Diseño de workers con backoff exponencial: por qué el jitter aleatorio es importante para evitar thundering herd en reintento masivo
- Unique jobs e idempotency: la diferencia entre deduplicación en enqueue y idempotencia en ejecución
- PostgreSQL LISTEN/NOTIFY como sistema de eventos push: cómo una base de datos puede eliminar polling innecesario
- Telemetry como contrato de observabilidad: diseñar para que el sistema sea instrumentable sin acoplamiento al monitoring

## Hints

- Comienza por el schema SQL: tabla `jobs` con columnas `id`, `state`, `queue`, `worker`, `args` (jsonb), `errors` (jsonb array), `attempt`, `max_attempts`, `scheduled_at`, `attempted_at`, `attempted_by`, `inserted_at`; los índices correctos son cruciales: `(state, queue, scheduled_at)` es el índice principal del poller
- Para el poller: `Repo.transaction(fn -> Repo.query!("SELECT * FROM jobs WHERE state = 'available' AND queue = $1 AND scheduled_at <= now() ORDER BY scheduled_at LIMIT $2 FOR UPDATE SKIP LOCKED", [queue, concurrency]) end)` es el corazón del sistema
- El DynamicSupervisor de workers: cuando el poller toma N jobs, lanza N `Task.Supervisor.async_nolink` tasks; cuando el task completa (éxito o error), actualiza el estado del job en una transacción y libera el slot de concurrencia
- Para los cron jobs: al arrancar, calcula el próximo disparo para cada worker cron y programa un timer; cuando dispara, inserta el job y recalcula el siguiente disparo; usa el unique job mechanism con `period: 60` para evitar inserciones dobles en clusters multi-nodo
- El rescue de huérfanos: un proceso periódico busca jobs en estado `executing` con `attempted_at < now() - execution_timeout`; los regresa a `available` incrementando `attempt`; este es el mecanismo que salva jobs cuando un nodo cae abruptamente en mitad de la ejecución
- Para LISTEN/NOTIFY: usa `Postgrex.Notifications` que ya gestiona la conexión de escucha; el callback recibe `{channel, payload}`; el payload es el nombre de la queue afectada para que solo el poller de esa queue se active

## Reference Material

- Oban source code: https://github.com/sorentwo/oban (el estándar de referencia de la industria Elixir)
- "Background Jobs Best Practices" — Brandur Leach: https://brandur.org/job-drain
- PostgreSQL `SELECT FOR UPDATE SKIP LOCKED` documentation y casos de uso
- Sidekiq architecture: https://github.com/sidekiq/sidekiq/wiki/Architecture (para comparar con el modelo Ruby)
- "Transactional Outbox Pattern" — microservices.io (el patrón que hace que la inserción del job sea atómica con la transacción de negocio)
- Telemetry library: https://hex.pm/packages/telemetry

## Difficulty Rating ★★★★★★★

La dificultad no está en ninguna feature individual sino en la corrección bajo fallo: un job queue que pierde jobs bajo carga o ejecuta jobs dos veces en edge cases no es aceptable en producción. Garantizar at-least-once sin duplicación excesiva, manejar correctamente los timeouts de ejecución, el rescue de huérfanos que no provoca ejecuciones dobles, y el cron distribuido sin disparos múltiples son problemas de correctitud que requieren pensar en invariantes de base de datos y condiciones de carrera concurrentes.

## Estimated Time

40–55 horas
