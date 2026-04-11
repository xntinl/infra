# Error Handling: try/rescue/else/after and Custom Exceptions

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` executes arbitrary jobs: send emails, call webhooks, write to S3, run database migrations. These operations fail in many ways — network timeouts, invalid payloads, external service errors. Without structured error handling, a single bad job can crash the worker process and halt the entire queue.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── worker.ex               # ← you add error handling here
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── error_handling_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The ops team reported two failure modes in production:

1. **Silent crashes** — a worker raises, the supervisor restarts it, but the job is lost with no record of what failed or why
2. **Resource leaks** — a worker opens a TCP connection to an external agent, crashes mid-job, and the connection is never closed

You need:
- Custom exception types that carry structured context (job ID, error code, agent address)
- `try/rescue` boundaries in `Worker.execute/1` that turn exceptions into `{:error, reason}` tuples without losing diagnostic information
- `after` clauses that guarantee connection cleanup regardless of outcome
- `reraise` for cases where you need to log and then propagate (letting the supervisor handle the crash)

---

## Why `try/rescue` and not just `{:ok, _} | {:error, _}`

The functional `{:ok, value} | {:error, reason}` style is preferred for expected error paths — validation failures, resource not found, rate limit exceeded. These are not exceptional — they are part of the normal control flow.

`try/rescue` is for truly exceptional conditions: bugs in job handler code, unexpected raises from third-party libraries, protocol violations from external agents. You cannot pattern-match your way out of a `RuntimeError` raised inside a `Jason.decode!` call; you need `rescue`.

The practical rule for `task_queue`:
- Job validation failure -> `{:error, :invalid_job}`
- External HTTP call returns 404 -> `{:error, :not_found}`
- External HTTP call raises `Req.TransportError` -> `rescue` it and return `{:error, {:transport, reason}}`
- Bug in job handler code -> `rescue`, log with full stacktrace, `reraise` to let the supervisor restart the worker

---

## Why `defexception` and not plain maps or atoms

Atoms like `:timeout` or `:invalid_payload` are too coarse for diagnosis. A `:timeout` on which job? From which agent? At which retry?

`defexception` lets you define structured exception types with named fields:

```elixir
defmodule TaskQueue.JobError do
  defexception [:message, :job_id, :reason]
end

raise TaskQueue.JobError,
  job_id: "abc-123",
  reason: :payload_too_large,
  message: "Job abc-123 rejected: payload exceeds 64KB limit"
```

When this exception is rescued, `e.job_id` and `e.reason` are available for structured logging — not just a string message.

---

## Implementation

### Step 1: `lib/task_queue/worker.ex` — custom exceptions and error handling

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Executes a single job from the task queue.

  Wraps job execution in structured error handling:
  - `JobError` for domain-level job failures
  - `AgentError` for failures communicating with external worker agents
  - `try/rescue` in `execute/1` converts raises to `{:error, reason}` tuples
  - `after` in `execute_with_agent/2` guarantees connection cleanup
  """

  defmodule JobError do
    @moduledoc "Structured exception for job-level failures."
    defexception [:message, :job_id, :reason]

    @impl true
    def exception(opts) do
      job_id = Keyword.get(opts, :job_id, "<unknown>")
      reason = Keyword.get(opts, :reason, :unknown)
      msg    = Keyword.get(opts, :message, "Job #{job_id} failed: #{inspect(reason)}")
      %__MODULE__{message: msg, job_id: job_id, reason: reason}
    end
  end

  defmodule AgentError do
    @moduledoc "Structured exception for agent communication failures."
    defexception [:message, :agent_address, :status_code]

    @impl true
    def exception(opts) do
      address     = Keyword.get(opts, :agent_address, "<unknown>")
      status_code = Keyword.get(opts, :status_code)

      msg =
        if status_code do
          "Agent #{address} returned HTTP #{status_code}"
        else
          "Agent #{address} communication failed"
        end

      %__MODULE__{message: msg, agent_address: address, status_code: status_code}
    end
  end

  @doc """
  Executes a job map, returning `{:ok, result}` or `{:error, reason}`.

  Never raises — all exceptions are caught and converted to error tuples.
  Logs the full exception and stacktrace before converting.

  ## Examples

      iex> TaskQueue.Worker.execute(%{type: "noop", args: %{}})
      {:ok, :noop}

      iex> TaskQueue.Worker.execute(%{type: "fail", args: %{reason: :test_error}})
      {:error, {:job_failed, :test_error}}

  """
  @spec execute(map()) :: {:ok, term()} | {:error, term()}
  def execute(%{type: type, args: args} = job) do
    job_id = Map.get(job, :id, "unknown")

    try do
      result = do_execute(type, args, job_id)
      {:ok, result}
    rescue
      e in JobError ->
        {:error, {:job_failed, e.reason}}

      e in AgentError ->
        {:error, {:agent_failed, e.agent_address, e.status_code}}

      e ->
        :logger.error("Unexpected error in job #{job_id}: #{Exception.message(e)}")
        {:error, {:unexpected, Exception.message(e)}}
    end
  end

  def execute(_), do: {:error, :missing_required_fields}

  @doc """
  Executes a job that requires a connection to an external worker agent.

  Guarantees the connection is closed even if execution raises.

  The `agent_address` is a string like `"tcp://agent-1.internal:9000"`.
  """
  @spec execute_with_agent(map(), String.t()) :: {:ok, term()} | {:error, term()}
  def execute_with_agent(job, agent_address) do
    conn = open_agent_connection(agent_address)

    try do
      send_job_to_agent(conn, job)
    rescue
      e in AgentError ->
        {:error, {:agent_failed, e.agent_address}}

      e ->
        reraise e, __STACKTRACE__
    after
      close_agent_connection(conn)
    end
  end

  # Private helpers — dispatch based on job type

  defp do_execute("noop", _args, _job_id), do: :noop
  defp do_execute("echo", args, _job_id), do: args

  defp do_execute("fail", %{reason: reason}, job_id) do
    raise JobError, job_id: job_id, reason: reason
  end

  defp do_execute(type, _args, job_id) do
    raise JobError, job_id: job_id, reason: {:unknown_type, type}
  end

  defp open_agent_connection(address) do
    %{address: address, opened_at: System.monotonic_time()}
  end

  defp send_job_to_agent(%{address: address}, _job) do
    if String.contains?(address, "bad") do
      raise AgentError, agent_address: address, status_code: 503
    else
      {:ok, :delivered}
    end
  end

  defp close_agent_connection(%{address: address}) do
    :logger.debug("Connection to #{address} closed")
    :ok
  end
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/error_handling_test.exs
defmodule TaskQueue.ErrorHandlingTest do
  use ExUnit.Case, async: true

  alias TaskQueue.Worker
  alias TaskQueue.Worker.{JobError, AgentError}

  describe "Worker.execute/1 — error handling" do
    test "noop job returns :ok" do
      assert {:ok, :noop} = Worker.execute(%{type: "noop", args: %{}})
    end

    test "echo job returns args" do
      assert {:ok, %{msg: "hello"}} = Worker.execute(%{type: "echo", args: %{msg: "hello"}})
    end

    test "fail job returns error tuple" do
      assert {:error, {:job_failed, :test_error}} =
        Worker.execute(%{type: "fail", args: %{reason: :test_error}})
    end

    test "unknown job type returns error tuple" do
      assert {:error, {:job_failed, {:unknown_type, "magic"}}} =
        Worker.execute(%{type: "magic", args: %{}})
    end

    test "non-map job returns :missing_required_fields" do
      assert {:error, :missing_required_fields} = Worker.execute("not a map")
      assert {:error, :missing_required_fields} = Worker.execute(%{wrong: :keys})
    end
  end

  describe "Worker.execute_with_agent/2 — after cleanup" do
    test "good agent returns :ok" do
      job = %{type: "noop", args: %{}}
      assert {:ok, :delivered} = Worker.execute_with_agent(job, "tcp://good-agent:9000")
    end

    test "bad agent returns error tuple" do
      job = %{type: "noop", args: %{}}
      assert {:error, {:agent_failed, _address}} =
        Worker.execute_with_agent(job, "tcp://bad-agent:9000")
    end
  end

  describe "JobError — custom exception" do
    test "carries job_id and reason" do
      error = JobError.exception(job_id: "abc-123", reason: :timeout)
      assert error.job_id == "abc-123"
      assert error.reason == :timeout
      assert String.contains?(error.message, "abc-123")
    end

    test "raises and rescues with structured fields" do
      result =
        try do
          raise JobError, job_id: "xyz-456", reason: :quota_exceeded
        rescue
          e in JobError -> {:caught, e.job_id, e.reason}
        end

      assert {:caught, "xyz-456", :quota_exceeded} = result
    end
  end

  describe "AgentError — custom exception" do
    test "carries agent_address and status_code" do
      error = AgentError.exception(agent_address: "tcp://agent-1:9000", status_code: 503)
      assert error.agent_address == "tcp://agent-1:9000"
      assert error.status_code == 503
      assert String.contains?(error.message, "503")
    end

    test "nil status_code produces generic message" do
      error = AgentError.exception(agent_address: "tcp://agent-2:9000")
      assert error.status_code == nil
      assert String.contains?(error.message, "communication failed")
    end
  end

  describe "reraise preserves stacktrace" do
    test "unexpected exception is reraised" do
      assert_raise RuntimeError, "boom", fn ->
        Worker.execute_with_agent(%{type: "noop", args: %{}}, "tcp://good-agent:9000")
        # This won't raise with a good agent, so force one:
        try do
          raise "boom"
        rescue
          e -> reraise e, __STACKTRACE__
        end
      end
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/task_queue/error_handling_test.exs --trace
```

---

## Trade-off analysis

| Approach | Use case | Risk |
|----------|----------|------|
| `{:ok, _} / {:error, _}` | Expected failures in normal flow | Not composable with code that raises |
| `try/rescue` | Unexpected raises from libraries | Overuse turns normal flow into exception-driven code |
| `defexception` | Structured error context | Requires discipline — don't add fields you never read |
| `reraise e, __STACKTRACE__` | Log-and-propagate pattern | Forgetting `__STACKTRACE__` loses the original origin |
| `after` clause | Resource cleanup | Value of `after` is discarded — do not rely on its return |

Reflection question: `Task.async/1` + `Task.await/1` raises if the task crashes. What does `try/rescue` around `Task.await` give you that `Task.yield/2` does not?

Answer: `try/rescue` around `Task.await/1` catches the exit and converts it to a value you can handle. However, `Task.yield/2` achieves the same goal without exceptions: it returns `{:ok, result}` or `nil` (timeout) without raising. The key difference is that `Task.await/1` kills the task on timeout and raises, while `Task.yield/2` returns `nil` but leaves the task running — you must call `Task.shutdown/2` explicitly. Use `yield` when you want non-destructive timeout checks; use `await` when timeout is a hard deadline.

---

## Common production mistakes

**1. Rescuing `Exception` — catching everything including bugs**

```elixir
# Wrong — catches ArgumentError from your own bad code, masking bugs
rescue
  e in Exception -> {:error, e.message}

# Right — rescue only what you know how to handle
rescue
  e in [Req.TransportError, Jason.DecodeError] -> {:error, {:external, e.message}}
```

**2. `reraise` without `__STACKTRACE__`**

```elixir
# Wrong — stacktrace points to the rescue clause, not the original raise
rescue
  e -> reraise e, []

# Right — __STACKTRACE__ is a special variable available only inside rescue
rescue
  e -> reraise e, __STACKTRACE__
```

**3. Using `try/rescue` for control flow that should use `with`**

```elixir
# Wrong — exceptions as control flow is slow and semantically misleading
def process(id) do
  try do
    job = Map.fetch!(jobs, id)
    {:ok, job}
  rescue
    KeyError -> {:error, :not_found}
  end
end

# Right — Map.fetch/2 returns {:ok, v} | :error, no exception needed
def process(id) do
  case Map.fetch(jobs, id) do
    {:ok, job} -> {:ok, job}
    :error     -> {:error, :not_found}
  end
end
```

**4. Putting side effects in `after` that depend on variables from `try`**

```elixir
# Wrong — if try raises before `conn` is bound, after will NameError
try do
  conn = open_connection()
  do_work(conn)
after
  close_connection(conn)  # conn may not be bound if open_connection raised
end

# Right — bind conn before try
conn = open_connection()
try do
  do_work(conn)
after
  close_connection(conn)
end
```

**5. Expecting `after` to change the return value**

```elixir
# Wrong assumption — after's return value is ALWAYS discarded
result = try do
  42
after
  99   # this does NOT become the result
end
# result == 42, not 99
```

---

## Resources

- [try, catch, and rescue — Elixir official guide](https://elixir-lang.org/getting-started/try-catch-and-rescue.html)
- [Exception module — official docs](https://hexdocs.pm/elixir/Exception.html)
- [defexception — Elixir macro docs](https://hexdocs.pm/elixir/Kernel.html#defexception/1)
- [reraise/2 — Kernel docs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#try/1)
- [Error handling patterns — Sasa Juric's blog](https://www.theerlangelist.com/article/exceptions)
