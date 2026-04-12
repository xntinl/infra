# Background Jobs with Oban

## Overview

Build a durable background job system for an HTTP API gateway using Oban, the standard
library for background processing in the Elixir ecosystem. The gateway generates events --
client registrations, billing threshold alerts, audit log entries -- that must be processed
asynchronously without coupling request latency to email delivery or external API calls.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── repo.ex
│       ├── workers/
│       │   ├── notification_worker.ex  # welcome emails, threshold alerts
│       │   ├── audit_worker.ex         # fire-and-forget audit log writes
│       │   └── report_worker.ex        # slow report generation with chaining
│       └── oban_logger.ex             # telemetry-based structured logging
├── priv/repo/migrations/
│   └── *_add_oban_jobs_table.exs
├── test/
│   └── api_gateway/
│       └── workers/
│           ├── notification_worker_test.exs
│           ├── audit_worker_test.exs
│           └── report_worker_test.exs
└── mix.exs
```

---

## The business problem

Three event types need background processing:

1. **Client registration** -- send welcome notification (email + push); must not duplicate
   if the job is enqueued twice by accident
2. **API request** -- write an audit log entry to a separate analytics store; fire-and-forget,
   high volume
3. **Billing threshold** -- generate a usage report and deliver it; slow (5 min), must chain
   a notification when complete

---

## Why Oban over a custom in-memory scheduler

An in-memory scheduler loses all pending jobs if the node restarts. Oban uses PostgreSQL as
a durable queue:

- Jobs survive node crashes (persisted before enqueue returns)
- At-least-once delivery: Oban's Lifeline plugin re-enqueues jobs that were executing
  when the node died
- Unique jobs: PostgreSQL-level deduplication prevents duplicate processing
- Distributed: multiple nodes share the same queue without coordination code

The cost: a PostgreSQL dependency and the overhead of a DB round-trip per enqueue. For the
event volumes in this system (registrations, threshold alerts), it's acceptable.

---

## Why `{:cancel, reason}` versus `{:error, reason}` matters

Oban distinguishes permanent failures from transient ones:

- `{:error, reason}` -- transient. Oban retries with exponential backoff up to `max_attempts`.
  Use this when the downstream might recover (DB temporarily unavailable, rate limit).
- `{:cancel, reason}` -- permanent. Oban marks the job as cancelled, no retries.
  Use this when retrying is pointless (user not found, invalid input).
- Raising an exception -- Oban treats this as `{:error, ...}` with the exception as the reason.

---

## Implementation

### Step 1: `mix.exs` -- add Oban

```elixir
defp deps do
  [
    {:oban, "~> 2.17"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, ">= 0.0.0"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: Migration

```elixir
# priv/repo/migrations/YYYYMMDDHHMMSS_add_oban_jobs_table.exs
defmodule ApiGateway.Repo.Migrations.AddObanJobsTable do
  use Ecto.Migration

  def up,   do: Oban.Migration.up(version: 12)
  def down, do: Oban.Migration.down(version: 1)
end
```

### Step 3: Configuration

```elixir
# config/config.exs
config :api_gateway, Oban,
  repo: ApiGateway.Repo,
  plugins: [
    {Oban.Plugins.Pruner, max_age: 60 * 60 * 24 * 7},
    {Oban.Plugins.Lifeline, rescue_after: :timer.minutes(30)},
    Oban.Plugins.Stager
  ],
  queues: [
    notifications: [limit: 10],
    audit:         [limit: 50],
    reports:       [limit: 2]
  ]

# config/test.exs
config :api_gateway, Oban, testing: :inline
```

### Step 4: Application setup

```elixir
# lib/api_gateway/application.ex -- add Oban to children
children = [
  ApiGateway.Repo,
  {Oban, Application.fetch_env!(:api_gateway, Oban)}
]
```

### Step 5: `lib/api_gateway/workers/notification_worker.ex`

The notification worker handles welcome emails and threshold alerts. It uses Oban's
`unique` option to prevent duplicate welcome notifications for the same client within
a 5-minute window.

```elixir
defmodule ApiGateway.Workers.NotificationWorker do
  use Oban.Worker,
    queue: :notifications,
    max_attempts: 5,
    unique: [period: 300, fields: [:args]]

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"type" => "welcome", "client_id" => client_id}}) do
    case ApiGateway.Clients.get(client_id) do
      nil ->
        {:cancel, "client #{client_id} not found"}

      client ->
        with :ok <- ApiGateway.Mailer.send_welcome(client),
             :ok <- ApiGateway.PushNotifier.send_welcome(client) do
          :ok
        else
          {:error, :service_unavailable} ->
            {:error, "email service unavailable"}

          {:error, reason} ->
            {:error, reason}
        end
    end
  end

  def perform(%Oban.Job{args: %{"type" => "threshold_alert", "client_id" => client_id, "threshold" => pct}}) do
    case ApiGateway.Clients.get(client_id) do
      nil ->
        {:cancel, "client #{client_id} not found"}

      client ->
        message = "Usage has reached #{pct}% of your plan quota."

        with :ok <- ApiGateway.Mailer.send_alert(client, message),
             :ok <- ApiGateway.PushNotifier.send_alert(client, message) do
          :ok
        else
          {:error, reason} -> {:error, reason}
        end
    end
  end

  def perform(%Oban.Job{args: %{"type" => "report_ready", "client_id" => client_id}}) do
    case ApiGateway.Clients.get(client_id) do
      nil ->
        {:cancel, "client #{client_id} not found"}

      client ->
        ApiGateway.Mailer.send_report_ready(client)
    end
  end

  def perform(%Oban.Job{args: %{"type" => type}}) do
    {:cancel, "unknown notification type: #{type}"}
  end
end
```

### Step 6: `lib/api_gateway/workers/audit_worker.ex`

The audit worker writes event logs to the analytics store. It runs at high volume
on a dedicated queue with 50 concurrent slots.

```elixir
defmodule ApiGateway.Workers.AuditWorker do
  use Oban.Worker,
    queue: :audit,
    max_attempts: 3

  @known_events ~w(api_request client_login client_logout rate_limited config_changed)

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"event" => event, "client_id" => client_id, "metadata" => meta}}) do
    unless event in @known_events do
      return {:cancel, "unknown event type: #{event}"}
    end

    audit_entry = %{
      event: event,
      client_id: client_id,
      metadata: meta,
      recorded_at: DateTime.utc_now()
    }

    case ApiGateway.Analytics.record(audit_entry) do
      :ok ->
        :ok

      {:error, :connection_refused} ->
        {:error, "analytics store unavailable"}

      {:error, reason} ->
        {:error, reason}
    end
  end

  def perform(%Oban.Job{args: %{"event" => event}}) when event not in @known_events do
    {:cancel, "unknown event type: #{event}"}
  end
end
```

### Step 7: `lib/api_gateway/workers/report_worker.ex`

The report worker handles slow, CPU-bound report generation. It chains a notification
job upon completion so the client is informed when their report is ready.

```elixir
defmodule ApiGateway.Workers.ReportWorker do
  use Oban.Worker,
    queue: :reports,
    max_attempts: 3,
    unique: [period: 3_600, fields: [:args, :worker], keys: [:client_id, :period]]

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"client_id" => client_id, "period" => period}}) do
    case ApiGateway.Clients.get(client_id) do
      nil ->
        {:cancel, "client #{client_id} not found"}

      client ->
        with {:ok, report} <- ApiGateway.Reports.generate(client, period),
             {:ok, _stored} <- ApiGateway.Reports.store(report) do
          %{"type" => "report_ready", "client_id" => client_id}
          |> ApiGateway.Workers.NotificationWorker.new()
          |> Oban.insert()

          :ok
        else
          {:error, :timeout} ->
            {:error, "report generation timed out"}

          {:error, reason} ->
            {:error, reason}
        end
    end
  end

  def timeout(_job), do: :timer.minutes(10)
end
```

### Step 8: `lib/api_gateway/oban_logger.ex`

Structured logging for all Oban job lifecycle events.

```elixir
defmodule ApiGateway.ObanLogger do
  require Logger

  def attach do
    events = [
      [:oban, :job, :start],
      [:oban, :job, :stop],
      [:oban, :job, :exception]
    ]
    :telemetry.attach_many("api-gateway-oban", events, &handle_event/4, [])
  end

  def handle_event([:oban, :job, :stop], %{duration: dur}, meta, _) do
    ms = System.convert_time_unit(dur, :native, :millisecond)
    Logger.info("[Oban] #{meta.worker} #{meta.queue} #{meta.state} #{ms}ms " <>
                "attempt=#{meta.attempt}/#{meta.max_attempts}")
  end

  def handle_event([:oban, :job, :exception], _measurements, meta, _) do
    Logger.error("[Oban] #{meta.worker} failed: #{inspect(meta.reason)}",
      job: meta.job,
      stacktrace: meta.stacktrace
    )
  end

  def handle_event([:oban, :job, :start], _, meta, _) do
    Logger.debug("[Oban] #{meta.worker} starting attempt #{meta.attempt}")
  end
end
```

### Step 9: Tests

```elixir
# test/api_gateway/workers/notification_worker_test.exs
defmodule ApiGateway.Workers.NotificationWorkerTest do
  use ApiGateway.DataCase
  use Oban.Testing, repo: ApiGateway.Repo

  alias ApiGateway.Workers.NotificationWorker

  test "welcome job succeeds for existing client" do
    client = insert(:client)

    assert :ok = perform_job(NotificationWorker, %{
      "type"      => "welcome",
      "client_id" => client.id
    })
  end

  test "cancels for unknown notification type" do
    assert {:cancel, _reason} = perform_job(NotificationWorker, %{
      "type"      => "nonexistent_type",
      "client_id" => "any"
    })
  end

  test "unique jobs: second enqueue returns conflict" do
    args = %{"type" => "welcome", "client_id" => "client-42"}

    {:ok, %{conflict?: false}} = NotificationWorker.new(args) |> Oban.insert()
    {:ok, %{conflict?: true}}  = NotificationWorker.new(args) |> Oban.insert()
  end
end
```

```elixir
# test/api_gateway/workers/report_worker_test.exs
defmodule ApiGateway.Workers.ReportWorkerTest do
  use ApiGateway.DataCase
  use Oban.Testing, repo: ApiGateway.Repo

  alias ApiGateway.Workers.{ReportWorker, NotificationWorker}

  test "report job enqueues notification on completion" do
    client = insert(:client)

    assert :ok = perform_job(ReportWorker, %{
      "client_id" => client.id,
      "period"    => "2026-03"
    })

    assert_enqueued(
      worker: NotificationWorker,
      args: %{"type" => "report_ready", "client_id" => client.id}
    )
  end

  test "scheduled jobs are inserted in :scheduled state" do
    {:ok, job} = ReportWorker.new(
      %{"client_id" => "c1", "period" => "2026-04"},
      scheduled_at: DateTime.add(DateTime.utc_now(), 3600, :second)
    ) |> Oban.insert()

    assert job.state == "scheduled"
  end
end
```

### Step 10: Run the tests

```bash
mix test test/api_gateway/workers/ --trace
```

---

## Trade-off analysis

| Aspect | Oban (PostgreSQL) | Custom in-memory scheduler | Raw `Task.async` |
|--------|-------------------|---------------------------|-----------------|
| Durability on crash | yes (DB-persisted) | no | no |
| At-least-once | yes (Lifeline plugin) | no | no |
| Unique jobs | yes (DB constraint) | no | no |
| Distributed | yes (shared DB) | no (single node) | no |
| Latency to enqueue | DB round-trip (~5ms) | none | none |
| Dependencies | Oban + Ecto + Postgres | none | none |
| Observability | Oban Web dashboard | custom | none |

---

## Common production mistakes

**1. Using `{:error, reason}` for invalid arguments**
If a job is enqueued with `client_id: nil` and you return `{:error, "client not found"}`,
Oban retries it 5 times before discarding. Use `{:cancel, reason}` for logic errors that
retrying cannot fix.

**2. Not configuring `testing: :inline` in test.exs**
Without `testing: :inline`, Oban in tests starts its polling infrastructure and jobs run
asynchronously. Tests become flaky and slow. Always set `testing: :inline` for tests.

**3. Overloading the `:default` queue**
All workers in the same queue compete for the concurrency limit. A slow `report_worker`
consuming the limit of `:default` will delay urgent `notification_worker` jobs. Separate
queues by criticality and expected duration.

**4. Not setting `timeout/1` on long-running workers**
A stuck `ReportWorker` holds a slot in the queue forever without a timeout. The Lifeline
plugin rescues jobs after 30 minutes, but a per-job timeout gives you finer control.

**5. `Oban.insert!/1` instead of `Oban.insert/1` in production code**
`insert!/1` raises on error (DB down, constraint violation). In a request handler, this
crashes the request. Use `insert/1` and handle the `{:error, changeset}` case.

---

## Resources

- [Oban documentation](https://hexdocs.pm/oban/Oban.html) -- complete API reference
- [Oban.Testing](https://hexdocs.pm/oban/Oban.Testing.html) -- `perform_job/2`, `assert_enqueued/1`
- [Oban plugins](https://hexdocs.pm/oban/Oban.Plugins.Pruner.html) -- Pruner, Lifeline, Stager
- [Oban Web](https://getoban.pro/oban-web) -- commercial dashboard for monitoring queues
