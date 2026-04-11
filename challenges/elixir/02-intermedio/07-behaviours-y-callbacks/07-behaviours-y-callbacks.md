# Behaviours: Module Contracts

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system currently has a single Worker that executes job payloads directly.
In practice, different job types need different execution strategies: some jobs should be
retried on failure, others should time out after a deadline, others should emit metrics.
Rather than a monolithic Worker with growing conditional logic, you define a behaviour:
a contract that every job handler must implement.

This is the same mechanism OTP uses for GenServer, Supervisor, and Application — you have
been using behaviours since exercise 04 without realising it.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── job_handler.ex           # ← the behaviour definition (you implement this)
│       ├── handlers/
│       │   ├── default_handler.ex   # ← you implement this
│       │   └── retrying_handler.ex  # ← you implement this
│       └── worker.ex                # updated to accept a configurable handler
├── test/
│   └── task_queue/
│       └── job_handler_test.exs     # given tests — must pass without modification
└── mix.exs
```

---

## Why behaviours and not plain functions

You could pass a handler as an anonymous function: `Worker.start_link(handler: &my_fn/1)`.
That works for a single callback. Behaviours add value when:

1. The contract has **multiple callbacks** that must work together — a handler needs both
   `execute/2` and `on_failure/3`.
2. You want **compile-time verification** — missing a callback is caught before runtime.
3. You want **documentation at the callsite** — `@impl TaskQueue.JobHandler` tells the
   reader exactly which contract this function satisfies.
4. The handler module has **its own state or configuration** beyond what can be captured
   in a closure.

The distinction between behaviours and protocols: behaviours dispatch on the **module**
(you call `handler_module.execute(job, ctx)`). Protocols dispatch on the **type of a
value** (you call `MyProtocol.some_fn(value)` and the dispatch is automatic based on
`value`'s type). Use behaviours for pluggable strategy modules; use protocols when you
want to add capabilities to existing data types.

---

## The business problem

`TaskQueue.JobHandler` is a behaviour that every job handler must implement:

- `execute/2` — runs the job. Returns `{:ok, result}` or `{:error, reason}`.
- `on_failure/3` — called after a failed execution. Returns `:retry | :abort`.
- `max_attempts/0` — how many times to attempt the job before aborting.
- `timeout_ms/0` — maximum milliseconds a single attempt may run.

Two implementations:
- `DefaultHandler` — one attempt, no retry, 10s timeout.
- `RetryingHandler` — three attempts with exponential backoff, 5s timeout per attempt.

---

## Implementation

### Step 1: `lib/task_queue/job_handler.ex`

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

  @doc """
  Executes the job. May be called multiple times if max_attempts > 1.
  Receives the job map and a context with current attempt number.
  """
  @callback execute(job(), context()) :: result()

  @doc """
  Called after each failed attempt. Returns :retry to try again or :abort to give up.
  Receives the job, the failure reason, and the current attempt context.
  """
  @callback on_failure(job(), reason :: any(), context()) :: failure_action()

  @doc "Maximum number of execution attempts before the job is marked :failed."
  @callback max_attempts() :: pos_integer()

  @doc "Timeout in milliseconds for a single execution attempt."
  @callback timeout_ms() :: pos_integer()

  # ---------------------------------------------------------------------------
  # Provided helper — dispatches through the behaviour
  # ---------------------------------------------------------------------------

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

### Step 2: `lib/task_queue/handlers/default_handler.ex`

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
    # HINT: job.payload is expected to be a function (-> any())
    # Call it and return {:ok, result}
    # If it returns an error tuple, pass it through
    # TODO: implement
  end

  @impl TaskQueue.JobHandler
  def on_failure(_job, reason, _ctx) do
    # DefaultHandler never retries — always abort
    # HINT: return :abort
    # TODO: implement
  end
end
```

### Step 3: `lib/task_queue/handlers/retrying_handler.ex`

```elixir
defmodule TaskQueue.Handlers.RetryingHandler do
  @moduledoc """
  Handler with up to 3 attempts and exponential backoff between retries.
  Timeout: 5s per attempt.
  """

  @behaviour TaskQueue.JobHandler
  require Logger

  @impl TaskQueue.JobHandler
  # HINT: return 3
  def max_attempts do
    # TODO: implement
  end

  @impl TaskQueue.JobHandler
  # HINT: return 5_000
  def timeout_ms do
    # TODO: implement
  end

  @impl TaskQueue.JobHandler
  def execute(job, ctx) do
    Logger.debug("RetryingHandler attempt #{ctx.attempt} for job #{job.id}")
    # HINT: same as DefaultHandler — call job.payload.() and wrap result
    # TODO: implement
  end

  @impl TaskQueue.JobHandler
  def on_failure(job, reason, ctx) do
    # Exponential backoff: 100ms * 2^(attempt-1)
    backoff_ms = 100 * :math.pow(2, ctx.attempt - 1) |> round()
    Logger.warning("Job #{job.id} failed (attempt #{ctx.attempt}): #{inspect(reason)}. Retrying in #{backoff_ms}ms")
    Process.sleep(backoff_ms)
    # HINT: return :retry — the behaviour infrastructure handles max_attempts
    # TODO: implement
  end
end
```

### Step 4: Given tests — must pass without modification

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

### Step 5: Run the tests

```bash
mix test test/task_queue/job_handler_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Behaviour (this exercise) | Anonymous function | Protocol |
|--------|--------------------------|-------------------|---------|
| Multiple related callbacks | Yes — all grouped in one contract | No — one function per argument | Yes, but for data types |
| Compile-time verification | Yes — missing @impl → warning | No | Yes |
| Module-level state/config | Yes — module attributes, compile_env | Via closure capture | No |
| Dispatch | Static — you pass the module | Static — you pass the function | Dynamic — based on value type |
| When to use | Pluggable strategy modules | Simple, single-function plug | Extending behaviour of data types |

Reflection question: `RetryingHandler.on_failure/3` sleeps inside the handler module,
blocking the Worker process. What is the consequence for the Worker's throughput, and how
would you redesign the retry mechanism to avoid blocking?

---

## Common production mistakes

**1. `@behaviour` without `@impl` on each callback**
Declaring `@behaviour TaskQueue.JobHandler` without `@impl` on each function means the
compiler does not verify completeness. A missing callback surfaces only at runtime when
`handler_module.on_failure/3` is called.

**2. Confusing behaviour dispatch with protocol dispatch**
Behaviours: you call `handler_module.execute(job, ctx)` explicitly.
Protocols: you call `MyProtocol.fn(value)` and Elixir dispatches automatically based on
`value`'s type. If you find yourself writing `if is_struct(value, Foo), do: ...`, a
protocol is probably the right tool.

**3. Putting business logic in the behaviour module**
The behaviour module (JobHandler) should define the contract and at most provide
helpers that work with any implementation. Business logic belongs in the concrete
handler modules. Mixing them makes the contract harder to implement.

**4. Optional callbacks without `@optional_callbacks`**
If you add `@callback maybe_log/2` but some handlers should not need it, declare it:
`@optional_callbacks [maybe_log: 2]`. Without this, every implementor gets a compiler
warning for not implementing it.

---

## Resources

- [Behaviours — Elixir official docs](https://hexdocs.pm/elixir/behaviours.html)
- [Module @callback — HexDocs](https://hexdocs.pm/elixir/Module.html#module-behaviour)
- [Typespecs and behaviours — HexDocs](https://hexdocs.pm/elixir/typespecs.html)
- [Elixir School: Behaviours](https://elixirschool.com/en/lessons/advanced/behaviours)
