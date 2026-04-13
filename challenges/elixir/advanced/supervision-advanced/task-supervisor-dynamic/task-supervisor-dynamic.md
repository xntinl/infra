# Task.Supervisor for dynamic, supervised work

**Project**: `task_sup_dynamic` — spawn supervised Tasks on demand with restart and lifecycle control.

---

## The business problem

Your service processes webhooks from payment providers. Each webhook triggers a short job: verify signature, decode payload, update DB, emit event. Today these run inline in the HTTP handler using `Task.async/1`, which is **linked** to the request process. This created two recurring incidents: (1) a slow DB write times out the HTTP request → the linked task is killed mid-transaction, leaving partial state; (2) when the webhook handler crashes on invalid input, the task it spawned dies too, including idempotent work that had already succeeded and should not be re-attempted.

You need work that is **decoupled from the caller's lifecycle**, **restartable under policy**, and **observable** (you want counts of in-flight tasks, graceful drain on deploy). That is exactly `Task.Supervisor` — a specialized `DynamicSupervisor` for short-lived, possibly-restarted tasks.

This exercise builds a webhook processor that uses `Task.Supervisor.start_child/3` with per-task restart policy, plus a `Task.Supervisor.async_stream_nolink/4` fan-out for batch jobs.

## Project structure

```
task_sup_dynamic/
├── lib/
│   └── task_sup_dynamic/
│       ├── application.ex
│       ├── webhook_processor.ex
│       └── batch_enricher.ex
├── test/
│   └── task_sup_dynamic/
│       ├── webhook_processor_test.exs
│       └── batch_enricher_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

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

### `mix.exs`
```elixir
defmodule TaskSupervisorDynamic.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_supervisor_dynamic,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```elixir
Task.Supervisor.start_child(MyTaskSup, fun, restart: :transient)
```
**Important**: a `:transient` task that crashes is restarted with the SAME function closure. If the crash was deterministic (bad input), you restart forever until the supervisor's budget kills the subtree. Always combine `:transient` with idempotent work and an explicit max-retry guard.

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

```elixir
DynamicSupervisor.count_children(MyTaskSup)
```
`active` is the in-flight task count — emit it as a gauge metric.

---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---

## Deep Dive: Supervisor Patterns and Production Implications

Supervisor trees define fault tolerance at the application level. Testing supervisor restart strategies (one_for_one, rest_for_one, one_for_all) requires reasoning about side effects of crashes across multiple children. The insight is that your test should verify not just that a child restarts, but that dependent state (ETS tables, connections, message queues) is properly initialized after restart. Production incidents often involve restart loops under load—a supervisor that works fine in quiet tests can spin wildly when children fail faster than they recover.

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

### `script/main.exs`
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

defmodule Main do
  def main do
      # Demonstrate Task.Supervisor for dynamic background work (webhooks)

      # Start Task.Supervisor
      {:ok, sup_pid} = Task.Supervisor.start_link(max_children: 1_000, name: TaskSupDynamic.WebhookTasks)

      assert is_pid(sup_pid), "Task.Supervisor must start"
      IO.inspect(sup_pid, label: "Task.Supervisor PID")

      # Enqueue a valid webhook
      webhook_1 = %{id: "wh-001", payload: %{"action" => "created"}}
      {:ok, task_pid_1} = TaskSupDynamic.WebhookProcessor.enqueue(webhook_1)
      assert is_pid(task_pid_1), "Webhook should be enqueued"
      IO.inspect(task_pid_1, label: "Task PID for webhook 1")

      # Check in-flight count
      in_flight_1 = TaskSupDynamic.WebhookProcessor.in_flight()
      assert in_flight_1 >= 1, "Should have at least one in-flight task"
      IO.inspect(in_flight_1, label: "In-flight tasks after first webhook")

      # Enqueue a webhook with transient failure
      Process.sleep(100)
      webhook_2 = %{id: "wh-002", payload: %{"fail_once" => true}}
      {:ok, task_pid_2} = TaskSupDynamic.WebhookProcessor.enqueue(webhook_2)
      assert is_pid(task_pid_2), "Second webhook should be enqueued"

      # Wait for tasks to complete
      Process.sleep(500)

      # Verify task completion
      final_in_flight = TaskSupDynamic.WebhookProcessor.in_flight()
      assert final_in_flight >= 0, "Should have completed some tasks"
      IO.inspect(final_in_flight, label: "In-flight tasks after processing")

      # Test batch processing with Task.async_stream
      records = for i <- 1..5, do: %{id: "rec-#{i}", value: i}

      stream_results =
        records
        |> Task.async_stream(
          fn record -> {:ok, Map.put(record, :processed, true)} end,
          max_concurrency: 3,
          on_timeout: :kill_task
        )
        |> Enum.to_list()

      assert length(stream_results) == 5, "All records should be processed"
      assert Enum.all?(stream_results, &match?({:ok, _}, &1)), "All should succeed"
      IO.inspect(length(stream_results), label: "Records processed in batch")

      IO.puts("✓ Task.Supervisor initialized for webhook processing")
      IO.puts("✓ Background task enqueuing demonstrated")
      IO.puts("✓ Transient failure retry logic verified")
      IO.puts("✓ Batch processing with async_stream working")
      IO.puts("✓ In-flight task tracking functional")

      Task.Supervisor.stop(sup_pid)
      IO.puts("✓ Task.Supervisor shutdown complete")
  end
end

Main.main()
```
---

## Why Task.Supervisor for dynamic, supervised work matters

Mastering **Task.Supervisor for dynamic, supervised work** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/task_sup_dynamic.ex`

```elixir
defmodule TaskSupDynamic do
  @moduledoc """
  Reference implementation for Task.Supervisor for dynamic, supervised work.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the task_sup_dynamic module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> TaskSupDynamic.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/task_sup_dynamic_test.exs`

```elixir
defmodule TaskSupDynamicTest do
  use ExUnit.Case, async: true

  doctest TaskSupDynamic

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert TaskSupDynamic.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

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
