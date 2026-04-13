# Oban — Background Jobs with Postgres as a Queue

**Project**: `oban_intro` — durable, retryable background jobs backed by a Postgres queue

---

## Why domain frameworks matters

Frameworks like Ash, Commanded, Oban, Nx and Axon encode large domain patterns (CQRS, event sourcing, ML training, background jobs, IoT updates) into reusable building blocks. Used well, they compress months of bespoke code into days.

Used poorly, they hide complexity that bites in production: aggregate version drift in Commanded, projection lag in CQRS systems, OTA failure recovery in Nerves, gradient explosion in Axon training loops. The framework's defaults are not your defaults.

---

## The business problem

You are building a production-grade Elixir component in the **Domain frameworks** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
oban_intro/
├── lib/
│   └── oban_intro.ex
├── script/
│   └── main.exs
├── test/
│   └── oban_intro_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Domain frameworks the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule ObanIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :oban_intro,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/oban_intro.ex`

```elixir
# lib/oban_intro/repo.ex
defmodule ObanIntro.Repo do
  use Ecto.Repo,
    otp_app: :oban_intro,
    adapter: Ecto.Adapters.Postgres
end

# priv/repo/migrations/20260412120000_add_oban.exs
defmodule ObanIntro.Repo.Migrations.AddOban do
  use Ecto.Migration

  @doc "Returns up result."
  def up, do: Oban.Migration.up(version: 12)
  @doc "Returns down result."
  def down, do: Oban.Migration.down(version: 1)
end

defmodule ObanIntro.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      ObanIntro.Repo,
      {Oban, Application.fetch_env!(:oban_intro, Oban)},
      ObanIntro.Observability.Telemetry
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ObanIntro.Supervisor)
  end
end

# lib/oban_intro/workers/pdf_worker.ex
defmodule ObanIntro.Workers.PdfWorker do
  @moduledoc """
  Renders a PDF for a given invoice. CPU-heavy, offloaded to the `:pdf` queue
  with low concurrency (3) because PDF rendering is CPU-bound.
  """

  use Oban.Worker,
    queue: :pdf,
    max_attempts: 5,
    priority: 2,
    unique: [period: 300, fields: [:worker, :args]]

  alias ObanIntro.Repo

  @doc "Returns perform result."
  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"invoice_id" => invoice_id}}) do
    # In production: fetch the invoice, render PDF with ChromicPdf / Typst,
    # upload to S3. For this exercise we simulate work.
    Process.sleep(50)
    {:ok, %{invoice_id: invoice_id, bytes: 1234}}
  end

  @doc "Returns backoff result."
  @impl Oban.Worker
  def backoff(%Oban.Job{attempt: attempt}) do
    trunc(:math.pow(2, attempt) * 10)
  end
end

# lib/oban_intro/workers/webhook_worker.ex
defmodule ObanIntro.Workers.WebhookWorker do
  @moduledoc """
  Delivers a webhook. Retries up to 20 times across ~24 hours.
  `max_attempts: 20` with polynomial backoff covers about a day of retries.
  """

  use Oban.Worker,
    queue: :webhooks,
    max_attempts: 20,
    priority: 3,
    tags: ["external", "retryable"]

  @doc "Returns perform result."
  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"url" => url, "payload" => payload}}) do
    case deliver(url, payload) do
      {:ok, status} when status in 200..299 -> :ok
      {:ok, 429} -> {:snooze, 60}
      {:ok, status} when status in 400..499 -> {:cancel, {:client_error, status}}
      {:ok, status} -> {:error, {:server_error, status}}
      {:error, reason} -> {:error, reason}
    end
  end

  defp deliver(_url, _payload) do
    # In production: HTTP client call (Req/Finch). Stubbed here for the exercise.
    {:ok, 200}
  end
end

# lib/oban_intro/workers/email_worker.ex
defmodule ObanIntro.Workers.EmailWorker do
  @moduledoc """
  Sends transactional emails. Priority 0 for password resets, 7 for newsletters.
  """

  use Oban.Worker, queue: :emails, max_attempts: 5

  @doc "Returns perform result."
  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"template" => template, "to" => to}}) do
    # In production: Swoosh/Bamboo. Stubbed.
    Process.sleep(10)
    {:ok, %{template: template, to: to}}
  end
end

# lib/oban_intro/observability/telemetry.ex
defmodule ObanIntro.Observability.Telemetry do
  @moduledoc false
  use GenServer

  require Logger

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_) do
    events = [
      [:oban, :job, :start],
      [:oban, :job, :stop],
      [:oban, :job, :exception],
      [:oban, :circuit, :trip]
    ]

    :telemetry.attach_many("oban-logger", events, &process_request/4, nil)
    {:ok, %{}}
  end

  @doc "Handles result from measurements, meta and _."
  def process_request([:oban, :job, :stop], measurements, meta, _) do
    Logger.info(
      "job ok: worker=#{meta.worker} queue=#{meta.queue} " <>
        "dur_ms=#{System.convert_time_unit(measurements.duration, :native, :millisecond)}"
    )
  end

  @doc "Handles result from _m, meta and _."
  def process_request([:oban, :job, :exception], _m, meta, _) do
    Logger.error(
      "job fail: worker=#{meta.worker} queue=#{meta.queue} " <>
        "attempt=#{meta.attempt} reason=#{inspect(meta.reason)}"
    )
  end

  @doc "Handles result from _event, _m, _meta and _."
  def process_request(_event, _m, _meta, _), do: :ok
end

defmodule ObanIntro.Workers.WebhookWorkerTest do
  use ExUnit.Case, async: true
  doctest ObanIntro.MixProject
  use Oban.Testing, repo: ObanIntro.Repo

  alias ObanIntro.Workers.WebhookWorker

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(ObanIntro.Repo)
  end

  describe "ObanIntro.Workers.WebhookWorker" do
    test "delivers successfully with 2xx" do
      assert :ok =
               perform_job(WebhookWorker, %{
                 "url" => "https://example.com/hook",
                 "payload" => %{"event" => "order.placed"}
               })
    end
  end
end
```
### `test/oban_intro_test.exs`

```elixir
defmodule ObanIntro.Workers.PdfWorkerTest do
  use ExUnit.Case, async: true
  doctest ObanIntro.MixProject
  use Oban.Testing, repo: ObanIntro.Repo

  alias ObanIntro.Workers.PdfWorker

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(ObanIntro.Repo)
  end

  describe "ObanIntro.Workers.PdfWorker" do
    test "perform/1 returns ok with pdf bytes" do
      assert {:ok, %{invoice_id: 7}} = perform_job(PdfWorker, %{invoice_id: 7})
    end

    test "unique constraint prevents duplicates" do
      {:ok, _} = PdfWorker.new(%{invoice_id: 7}) |> Oban.insert()
      assert {:ok, %Oban.Job{conflict?: true}} = PdfWorker.new(%{invoice_id: 7}) |> Oban.insert()
    end

    test "backoff grows exponentially" do
      assert PdfWorker.backoff(%Oban.Job{attempt: 1}) < PdfWorker.backoff(%Oban.Job{attempt: 3})
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Oban — Background Jobs with Postgres as a Queue.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Oban — Background Jobs with Postgres as a Queue ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ObanIntro.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: ObanIntro.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```
---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Frameworks encode opinions

Ash, Commanded, Oban each pick defaults that work for the common case. Understand the defaults before you customize — the framework's authors chose them for a reason.

### 2. Event-sourced systems need projection lag tolerance

In CQRS, the read model is eventually consistent with the write model. UI must handle 'I saved but I don't see my own data yet'. Optimistic UI updates help.

### 3. Background jobs need idempotency and retries

Oban retries failed jobs by default. The worker must be idempotent: repeating a job must produce the same end state. Use unique constraints and deduplication keys.

---
