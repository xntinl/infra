# Task.Supervisor for dynamic, supervised work

**Project**: `task_sup_dynamic` — spawn supervised Tasks on demand with restart and lifecycle control.

---

## Project context

Your service processes webhooks from payment providers. Each webhook triggers a short job: verify signature, decode payload, update DB, emit event. Today these run inline in the HTTP handler using `Task.async/1`, which is **linked** to the request process. This created two recurring incidents: (1) a slow DB write times out the HTTP request → the linked task is killed mid-transaction, leaving partial state; (2) when the webhook handler crashes on invalid input, the task it spawned dies too, including idempotent work that had already succeeded and should not be re-attempted.

You need work that is **decoupled from the caller's lifecycle**, **restartable under policy**, and **observable** (you want counts of in-flight tasks, graceful drain on deploy). That is exactly `Task.Supervisor` — a specialized `DynamicSupervisor` for short-lived, possibly-restarted tasks.

This exercise builds a webhook processor that uses `Task.Supervisor.start_child/3` with per-task restart policy, plus a `Task.Supervisor.async_stream_nolink/4` fan-out for batch jobs.

```
task_sup_dynamic/
├── lib/
│   └── task_sup_dynamic/
│       ├── application.ex
│       ├── webhook_processor.ex
│       └── batch_enricher.ex
└── test/
    └── task_sup_dynamic/
        ├── webhook_processor_test.exs
        └── batch_enricher_test.exs
```

---

## Core concepts

### 1. `Task.async/1` vs `Task.Supervisor.async/3` vs `start_child/3`

| API | Link to caller? | Needs `await`? | Restart policy | Use case |
|---|---|---|---|---|
| `Task.async/await` | yes (bidirectional link + monitor) | yes | none | caller must see the result |
| `Task.Supervisor.async` | yes (linked to caller via supervisor) | yes | none | same, but counted by supervisor |
| `Task.Supervisor.async_nolink` | no (only monitored) | yes | none | caller wants result but should NOT die with task |
| `Task.Supervisor.start_child` | no | no | configurable (`:temporary`/`:transient`) | fire-and-forget, supervisor-managed |

For webhooks: `start_child` with `:transient` — retry on crash, but not on normal completion.

### 2. Restart semantics for Tasks

`Task.Supervisor`'s children default to `restart: :temporary`. A task that completes (normal or crash) is not restarted. This matches "one-shot work". To get retry-on-crash, pass `restart: :transient`:

```elixir
Task.Supervisor.start_child(MyTaskSup, fun, restart: :transient)
```

**Important**: a `:transient` task that crashes is restarted with the SAME function closure. If the crash was deterministic (bad input), you restart forever until the supervisor's budget kills the subtree. Always combine `:transient` with idempotent work and an explicit max-retry guard.

### 3. `async_stream_nolink` for bounded concurrency

```elixir
Task.Supervisor.async_stream_nolink(
  MyTaskSup,
  items,
  fn item -> enrich(item) end,
  max_concurrency: 16,
  timeout: 5_000,
  on_timeout: :kill_task,
  ordered: false
)
```

Key points:

- `max_concurrency` caps in-flight tasks — backpressure is automatic.
- `on_timeout: :kill_task` produces `{:exit, :timeout}` in the stream instead of propagating the exit.
- `ordered: false` yields results in completion order (higher throughput when jobs have variable latency).
- Uses the supervisor's pool — monitored, NOT linked to the caller.

### 4. Draining on shutdown

`Task.Supervisor` inherits from `DynamicSupervisor`. When its parent terminates it:

```
parent sends :shutdown
  ↓
Task.Supervisor sends :shutdown to each child Task
  ↓
each Task has `shutdown` ms to exit (default 5000)
  ↓
if still alive → :brutal_kill
```

Configure with `shutdown:` on the child spec. For webhook jobs that must not be killed mid-transaction, set `shutdown: 30_000` and wrap DB work in a transaction that commits atomically.

### 5. Observability

```elixir
DynamicSupervisor.count_children(MyTaskSup)
# => %{active: 12, specs: 12, supervisors: 0, workers: 12}
```

`active` is the in-flight task count — emit it as a gauge metric.

---

## Why `Task.Supervisor` and not Oban

Oban persists jobs to Postgres: survives VM restarts, supports retries with backoff, has a UI. It is the correct tool when durability crosses process boundaries. The workloads here — verifying a signature, writing a row, emitting an event — are in-flight within one VM lifetime. Paying for a DB round-trip per job is wasteful. `Task.Supervisor` is the OTP-native answer: zero dependencies, in-memory, integrates with Supervisor drain semantics. The cost is that a hard VM crash loses in-flight work; for idempotent webhooks whose producer retries on timeout, that cost is acceptable.

---

## Design decisions

**Option A — `Task.async/await` in the HTTP handler**
- Pros: trivial; result flows back; zero supervision overhead.
- Cons: task is linked to the request; request timeout kills the task mid-transaction; request crash kills idempotent work that already succeeded.

**Option B — `Task.Supervisor.start_child` with `:transient` + max-attempts guard** (chosen)
- Pros: decoupled lifecycle; supervisor-visible retry on crash; observable via `count_children/1`; participates in graceful shutdown drain.
- Cons: must engineer idempotency and an explicit attempt counter to avoid infinite retries on deterministic crashes.

→ Chose **B** because the incidents described ("request timeout kills mid-DB-commit", "handler crash loses idempotent progress") are direct consequences of linked lifecycle — and the Supervisor's observability is needed for deploy-time drain.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```


### Step 1: Application

```elixir
# lib/task_sup_dynamic/application.ex
defmodule TaskSupDynamic.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Task.Supervisor, name: TaskSupDynamic.WebhookTasks},
      {Task.Supervisor, name: TaskSupDynamic.BatchTasks}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: TaskSupDynamic.Supervisor)
  end
end
```

### Step 2: Webhook processor with `:transient` retry and max attempts

```elixir
# lib/task_sup_dynamic/webhook_processor.ex
defmodule TaskSupDynamic.WebhookProcessor do
  @moduledoc """
  Processes webhooks out-of-band of the HTTP request. Each webhook is an
  idempotent job that may retry on crash up to `max_attempts` times.
  """

  @sup TaskSupDynamic.WebhookTasks
  @max_attempts 3

  @type webhook :: %{id: String.t(), payload: map()}

  @doc "Enqueue a webhook for background processing. Returns {:ok, pid}."
  @spec enqueue(webhook()) :: DynamicSupervisor.on_start_child()
  def enqueue(%{id: _id} = webhook) do
    Task.Supervisor.start_child(
      @sup,
      fn -> run(webhook, 1) end,
      restart: :transient,
      shutdown: 30_000
    )
  end

  @doc "Current in-flight count."
  @spec in_flight() :: non_neg_integer()
  def in_flight do
    %{active: n} = DynamicSupervisor.count_children(@sup)
    n
  end

  @doc false
  def run(%{id: id, payload: payload}, attempt) do
    try do
      :ok = verify_signature!(payload)
      :ok = process!(payload)
      {:ok, id}
    rescue
      e in RuntimeError ->
        if attempt >= @max_attempts do
          exit(:normal)
        else
          reraise e, __STACKTRACE__
        end
    end
  end

  defp verify_signature!(%{"sig" => "bad"}), do: raise("invalid signature")
  defp verify_signature!(_), do: :ok

  defp process!(%{"fail_once" => true} = p) do
    key = {:processed, p["id"]}

    case :persistent_term.get(key, :new) do
      :new ->
        :persistent_term.put(key, :done)
        raise "transient db blip"

      :done ->
        :ok
    end
  end

  defp process!(_payload), do: :ok
end
```

### Step 3: Batch enricher with `async_stream_nolink`

```elixir
# lib/task_sup_dynamic/batch_enricher.ex
defmodule TaskSupDynamic.BatchEnricher do
  @moduledoc """
  Fan-out enrichment of a list of records, bounded by max_concurrency.
  Safe against individual task failures — caller does not die.
  """

  @sup TaskSupDynamic.BatchTasks

  @spec enrich_all([map()], keyword()) :: [{:ok, map()} | {:exit, term()}]
  def enrich_all(records, opts \\ []) do
    max_conc = Keyword.get(opts, :max_concurrency, System.schedulers_online() * 2)

    @sup
    |> Task.Supervisor.async_stream_nolink(
      records,
      &enrich_one/1,
      max_concurrency: max_conc,
      timeout: 2_000,
      on_timeout: :kill_task,
      ordered: false
    )
    |> Enum.to_list()
  end

  defp enrich_one(%{"slow" => ms} = rec) do
    Process.sleep(ms)
    Map.put(rec, "enriched", true)
  end

  defp enrich_one(%{"fail" => true}), do: raise("enrichment failure")
  defp enrich_one(rec), do: Map.put(rec, "enriched", true)
end
```

### Step 4: Tests

```elixir
# test/task_sup_dynamic/webhook_processor_test.exs
defmodule TaskSupDynamic.WebhookProcessorTest do
  use ExUnit.Case, async: false

  alias TaskSupDynamic.WebhookProcessor

  test "happy path completes and is not restarted" do
    {:ok, pid} = WebhookProcessor.enqueue(%{id: "wh-1", payload: %{"ok" => true}})
    ref = Process.monitor(pid)
    assert_receive {:DOWN, ^ref, :process, ^pid, :normal}, 1_000
  end

  test "transient failure is retried by the supervisor" do
    id = "wh-retry-#{System.unique_integer()}"
    {:ok, pid} = WebhookProcessor.enqueue(%{id: id, payload: %{"id" => id, "fail_once" => true}})
    ref = Process.monitor(pid)
    assert_receive {:DOWN, ^ref, :process, _, _reason}, 2_000
  end

  test "in_flight reports active tasks" do
    before = WebhookProcessor.in_flight()

    for i <- 1..5 do
      WebhookProcessor.enqueue(%{id: "wh-#{i}", payload: %{"slow" => 200}})
    end

    Process.sleep(50)
    assert WebhookProcessor.in_flight() >= before
  end
end
```

```elixir
# test/task_sup_dynamic/batch_enricher_test.exs
defmodule TaskSupDynamic.BatchEnricherTest do
  use ExUnit.Case, async: true

  alias TaskSupDynamic.BatchEnricher

  test "enriches all records successfully" do
    records = for i <- 1..20, do: %{"i" => i}
    results = BatchEnricher.enrich_all(records, max_concurrency: 4)
    assert length(results) == 20
    assert Enum.all?(results, &match?({:ok, %{"enriched" => true}}, &1))
  end

  test "one failing record does not kill the batch" do
    records = [%{"i" => 1}, %{"fail" => true}, %{"i" => 3}]
    results = BatchEnricher.enrich_all(records, max_concurrency: 2)

    assert length(results) == 3
    assert Enum.count(results, &match?({:exit, _}, &1)) == 1
    assert Enum.count(results, &match?({:ok, _}, &1)) == 2
  end

  test "timeout produces :kill_task exit, not a raise" do
    records = [%{"slow" => 5_000}, %{"slow" => 10}]
    results = BatchEnricher.enrich_all(records, max_concurrency: 2)

    assert Enum.any?(results, &match?({:exit, :timeout}, &1))
    assert Enum.any?(results, &match?({:ok, _}, &1))
  end
end
```

### Why this works

`start_child` monitors (not links) the task, so the HTTP handler crashing leaves the task running. `restart: :transient` gives retry-on-crash while skipping `exit(:normal)` (the attempt-cap escape hatch). `shutdown: 30_000` gives the supervisor 30 s to drain during deploy, matching DB commit windows. For batch fan-out, `async_stream_nolink` with `on_timeout: :kill_task` turns a stuck task into a `{:exit, :timeout}` tuple rather than propagating the exit to the parent — partial results survive individual failures.

---

## Trade-offs and production gotchas

**1. `:transient` without idempotency = infinite loop until budget death.** A deterministic bug (malformed payload) that crashes your task restarts the task forever until `max_restarts`. Always pair `:transient` with either (a) an explicit attempt counter + `exit(:normal)` on exhaustion, or (b) a poison-pill queue for permanent failures.

**2. `Task.async/1` is NOT for background work.** It links to the caller. The second the caller dies, the task is killed mid-flight. Every time you see `Task.async` outside a parent that will `await` within milliseconds, you have a bug waiting to happen. Use `Task.Supervisor.start_child` or `async_nolink`.

**3. `async_stream` timeouts crash the caller by default.** If `on_timeout` is left as `:exit` (the default), a single slow task raises `exit(:timeout)` in the caller. Use `on_timeout: :kill_task` for batch jobs where partial results are acceptable.

**4. `shutdown: 30_000` means deploys wait 30s.** If your release sends `SIGTERM` and the webhook supervisor has 50 in-flight tasks each shutdown-delayed by 30 s, the OS supervisor (K8s, systemd) may send `SIGKILL` first. Coordinate app `shutdown` with the orchestrator's `terminationGracePeriodSeconds`.

**5. Unbounded `start_child` is a memory leak.** `Task.Supervisor` does not cap in-flight tasks. If your enqueuer outpaces your workers, you grow the process table until the VM dies. Enforce a soft cap via `DynamicSupervisor.count_children/1` before `start_child`, or put a backpressure layer (Broadway, GenStage) in front.

**6. Crash logs get noisy fast.** Every `:transient` retry logs a SASL report. With `max_attempts = 3` and 1 000 webhooks/s, a 1 % deterministic failure rate = 10 logs/s × 3 = 30/s. Tag your retries and downgrade non-final attempts to `:info`.

**7. Tasks hold onto binary references.** A `Task.Supervisor.async_stream` over 1 M records where each closure captures a 100 MB binary through closure scope = 100 MB × N concurrency = OOM. Use `max_concurrency` conservatively and extract minimal data into the closure.

**8. When NOT to use this.** If you need durability across VM restarts (job survives a crash), `Task.Supervisor` is the wrong tool — in-memory only. Use Oban, Exq, or a DB-backed queue. `Task.Supervisor` is for in-flight work within one VM.

---

## Benchmark

`Task.Supervisor.start_child` is ~10–20 µs (one `DynamicSupervisor.start_child` call under the hood). The cost is dominated by the closure allocation, not the supervisor. For 100 k/s task creation you'll need to batch — do NOT start 100 k individual tasks, start 100 tasks that each process 1 000 items.

`async_stream_nolink` with `max_concurrency: N` adds one extra `spawn_monitor` per task (~2 µs) plus the `Stream` overhead. Steady-state throughput for pure-Elixir work saturates at ~200 k tasks/s per scheduler before scheduler pressure dominates.

Target: `start_child` latency ≤ 20 µs; `async_stream_nolink` throughput ≥ 50k items/s at `max_concurrency = schedulers × 2` for sub-millisecond work.

---

## Reflection

1. Your webhook volume doubles and deploys now take 4 minutes because `shutdown: 30_000` × 50 in-flight tasks × serial draining. Do you shorten shutdown, add a pre-drain barrier (stop accepting new webhooks first), or move the work to Oban? What invariant breaks under each choice?
2. A deterministic bug turns 0.5% of payloads into a crash that retries 3 times before hitting the attempt cap. At 1k webhooks/s, that is 15 extra task spawns/s + logs. Would you switch to a dead-letter table, a per-payload circuit breaker, or tighten the cap to 1? Use the retry logs as your telemetry source.

---

## Resources

- [`Task.Supervisor` — hexdocs](https://hexdocs.pm/elixir/Task.Supervisor.html) — `start_child`, `async_nolink`, `async_stream_nolink`.
- [`Task` module guide](https://hexdocs.pm/elixir/Task.html) — Task semantics, link vs monitor.
- [DynamicSupervisor — hexdocs](https://hexdocs.pm/elixir/DynamicSupervisor.html) — parent type of Task.Supervisor.
- [Saša Jurić — To spawn, or not to spawn?](https://www.theerlangelist.com/article/spawn_or_not) — when to use Task vs GenServer.
- [Broadway producer internals](https://github.com/dashbitco/broadway/blob/main/lib/broadway/topology/producer_stage.ex) — how a production library uses Task.Supervisor for bounded fan-out.
- [Oban worker execution](https://github.com/sorentwo/oban/blob/main/lib/oban/queue/executor.ex) — durable job queue; contrast with Task.Supervisor's in-memory model.
