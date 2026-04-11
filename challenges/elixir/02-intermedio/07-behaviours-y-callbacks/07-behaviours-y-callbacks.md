# Behaviours: Module Contracts

## Why behaviours

Behaviours let you define a contract that every implementation module must follow. They
add value when:

1. The contract has **multiple callbacks** that must work together.
2. You want **compile-time verification** — missing a callback is caught before runtime.
3. You want **documentation at the callsite** — `@impl MyBehaviour` tells the reader
   exactly which contract this function satisfies.

The distinction between behaviours and protocols: behaviours dispatch on the **module**
(you call `handler_module.execute(job, ctx)`). Protocols dispatch on the **type of a
value** (you call `MyProtocol.some_fn(value)` and dispatch is automatic based on the
value's type). Use behaviours for pluggable strategy modules; use protocols for adding
capabilities to existing data types.

---

## The business problem

Build a `TaskQueue.JobHandler` behaviour that every job handler must implement:

- `execute/2` — runs the job. Returns `{:ok, result}` or `{:error, reason}`.
- `on_failure/3` — called after a failed execution. Returns `:retry | :abort`.
- `max_attempts/0` — how many times to attempt the job before aborting.
- `timeout_ms/0` — maximum milliseconds a single attempt may run.

Two implementations:
- `DefaultHandler` — one attempt, no retry, 10s timeout.
- `RetryingHandler` — three attempts with exponential backoff, 5s timeout per attempt.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── job_handler.ex
│       └── handlers/
│           ├── default_handler.ex
│           └── retrying_handler.ex
├── test/
│   └── task_queue/
│       └── job_handler_test.exs
└── mix.exs
```

---

## Implementation

### `lib/task_queue/job_handler.ex`

```elixir
defmodule TaskQueue.JobHandler do
  @moduledoc """
  Behaviour contract for task_queue job handlers.

  A job handler defines execution policy: how to run a job, what to do on failure,
  how many attempts to allow, and when to give up.
  """

  @type job :: %{id: String.t(), payload: any(), queued_at: integer()}
  @type context :: %{attempt: pos_integer(), worker_id: String.t()}
  @type result :: {:ok, any()} | {:error, any()}
  @type failure_action :: :retry | :abort

  @callback execute(job(), context()) :: result()
  @callback on_failure(job(), reason :: any(), context()) :: failure_action()
  @callback max_attempts() :: pos_integer()
  @callback timeout_ms() :: pos_integer()

  @doc """
  Runs a job through the given handler module, respecting retry and timeout policy.
  Returns {:ok, result} on success or {:error, :max_attempts_exceeded} on final failure.
  """
  @spec run(module(), job()) :: result()
  def run(handler_module, job) do
    do_run(handler_module, job, 1)
  end

  defp do_run(handler_module, job, attempt) do
    max = handler_module.max_attempts()
    timeout = handler_module.timeout_ms()
    ctx = %{attempt: attempt, worker_id: inspect(self())}

    result =
      try do
        task = Task.async(fn -> handler_module.execute(job, ctx) end)
        Task.await(task, timeout)
      catch
        :exit, {:timeout, _} -> {:error, :timeout}
        :exit, reason -> {:error, {:crashed, reason}}
      end

    case result do
      {:ok, _} = success ->
        success

      {:error, reason} when attempt < max ->
        case handler_module.on_failure(job, reason, ctx) do
          :retry -> do_run(handler_module, job, attempt + 1)
          :abort -> {:error, {:aborted_at_attempt, attempt, reason}}
        end

      {:error, reason} ->
        {:error, {:max_attempts_exceeded, reason}}
    end
  end
end
```

The `run/2` function orchestrates the retry loop by calling the handler module's callbacks
at each step. Each attempt runs inside a `Task.async/await` pair to enforce the timeout.
The recursive `do_run/3` increments the attempt counter on each retry. When `attempt`
reaches `max_attempts`, the function returns the final error regardless of what
`on_failure/3` returns.

### `lib/task_queue/handlers/default_handler.ex`

```elixir
defmodule TaskQueue.Handlers.DefaultHandler do
  @moduledoc "Single-attempt handler with a 10s timeout. No retry."

  @behaviour TaskQueue.JobHandler

  @impl TaskQueue.JobHandler
  def max_attempts, do: 1

  @impl TaskQueue.JobHandler
  def timeout_ms, do: 10_000

  @impl TaskQueue.JobHandler
  def execute(job, _ctx) do
    result = job.payload.()
    {:ok, result}
  end

  @impl TaskQueue.JobHandler
  def on_failure(_job, _reason, _ctx) do
    :abort
  end
end
```

### `lib/task_queue/handlers/retrying_handler.ex`

```elixir
defmodule TaskQueue.Handlers.RetryingHandler do
  @moduledoc """
  Handler with up to 3 attempts and exponential backoff between retries.
  Timeout: 5s per attempt.
  """

  @behaviour TaskQueue.JobHandler
  require Logger

  @impl TaskQueue.JobHandler
  def max_attempts, do: 3

  @impl TaskQueue.JobHandler
  def timeout_ms, do: 5_000

  @impl TaskQueue.JobHandler
  def execute(job, ctx) do
    Logger.debug("RetryingHandler attempt #{ctx.attempt} for job #{job.id}")
    result = job.payload.()
    {:ok, result}
  end

  @impl TaskQueue.JobHandler
  def on_failure(job, reason, ctx) do
    backoff_ms = 100 * :math.pow(2, ctx.attempt - 1) |> round()

    Logger.warning(
      "Job #{job.id} failed (attempt #{ctx.attempt}): #{inspect(reason)}. " <>
      "Retrying in #{backoff_ms}ms"
    )

    Process.sleep(backoff_ms)
    :retry
  end
end
```

The RetryingHandler uses exponential backoff: 100ms after the first failure, 200ms after
the second. `on_failure/3` always returns `:retry` — the `JobHandler.run/2` engine
enforces the `max_attempts` limit independently.

### Tests

```elixir
# test/task_queue/job_handler_test.exs
defmodule TaskQueue.JobHandlerTest do
  use ExUnit.Case, async: true

  alias TaskQueue.JobHandler
  alias TaskQueue.Handlers.{DefaultHandler, RetryingHandler}

  defp make_job(payload) do
    %{id: "test_#{:rand.uniform(1_000)}", payload: payload, queued_at: 0}
  end

  describe "DefaultHandler" do
    test "executes a successful job" do
      job = make_job(fn -> 42 end)
      assert {:ok, 42} = JobHandler.run(DefaultHandler, job)
    end

    test "returns error on exception, does not retry" do
      attempts = Agent.start_link(fn -> 0 end) |> elem(1)

      job = make_job(fn ->
        Agent.update(attempts, &(&1 + 1))
        raise "always fails"
      end)

      assert {:error, _} = JobHandler.run(DefaultHandler, job)
      assert 1 = Agent.get(attempts, & &1)
      Agent.stop(attempts)
    end

    test "max_attempts is 1" do
      assert 1 = DefaultHandler.max_attempts()
    end
  end

  describe "RetryingHandler" do
    test "retries up to max_attempts on failure" do
      attempts = Agent.start_link(fn -> 0 end) |> elem(1)

      job = make_job(fn ->
        Agent.update(attempts, &(&1 + 1))
        raise "transient failure"
      end)

      result = JobHandler.run(RetryingHandler, job)
      assert {:error, {:max_attempts_exceeded, _}} = result
      assert 3 = Agent.get(attempts, & &1)
      Agent.stop(attempts)
    end

    test "succeeds on second attempt" do
      attempts = Agent.start_link(fn -> 0 end) |> elem(1)

      job = make_job(fn ->
        n = Agent.get_and_update(attempts, fn n -> {n + 1, n + 1} end)
        if n < 2, do: raise("not yet"), else: :success
      end)

      assert {:ok, :success} = JobHandler.run(RetryingHandler, job)
      Agent.stop(attempts)
    end

    test "max_attempts is 3" do
      assert 3 = RetryingHandler.max_attempts()
    end
  end

  describe "Behaviour contract" do
    test "DefaultHandler implements all required callbacks" do
      behaviours = DefaultHandler.module_info(:attributes)[:behaviour] || []
      assert TaskQueue.JobHandler in behaviours
    end

    test "RetryingHandler implements all required callbacks" do
      behaviours = RetryingHandler.module_info(:attributes)[:behaviour] || []
      assert TaskQueue.JobHandler in behaviours
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/job_handler_test.exs --trace
```

---

## Common production mistakes

**1. `@behaviour` without `@impl` on each callback**
Without `@impl`, the compiler does not verify completeness. A missing callback surfaces
only at runtime.

**2. Confusing behaviour dispatch with protocol dispatch**
Behaviours: you call `handler_module.execute(job, ctx)` explicitly.
Protocols: you call `MyProtocol.fn(value)` and Elixir dispatches automatically based on
the value's type.

**3. Putting business logic in the behaviour module**
The behaviour module should define the contract and helpers. Business logic belongs in
the concrete handler modules.

**4. Optional callbacks without `@optional_callbacks`**
If some handlers should not need a callback, declare it:
`@optional_callbacks [maybe_log: 2]`.

---

## Resources

- [Behaviours — Elixir official docs](https://hexdocs.pm/elixir/behaviours.html)
- [Typespecs and behaviours — HexDocs](https://hexdocs.pm/elixir/typespecs.html)
- [Elixir School: Behaviours](https://elixirschool.com/en/lessons/advanced/behaviours)
