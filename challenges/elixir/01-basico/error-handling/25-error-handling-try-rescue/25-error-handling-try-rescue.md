# Error Handling: try/rescue/else/after and Custom Exceptions

## Goal

Build a `task_queue` worker with structured error handling using custom exceptions (`defexception`), `try/rescue` boundaries that convert exceptions into `{:error, reason}` tuples, `after` clauses for resource cleanup, and `reraise` for log-and-propagate patterns.

---

## Why `try/rescue` and not just `{:ok, _} | {:error, _}`

The functional `{:ok, value} | {:error, reason}` style is preferred for expected error paths -- validation failures, resource not found, rate limit exceeded. These are not exceptional.

`try/rescue` is for truly exceptional conditions: bugs in job handler code, unexpected raises from third-party libraries, protocol violations from external agents. You cannot pattern-match your way out of a `RuntimeError` raised inside a `Jason.decode!` call.

The practical rule:
- Job validation failure -> `{:error, :invalid_job}`
- External HTTP call returns 404 -> `{:error, :not_found}`
- External HTTP call raises a transport error -> `rescue` and return `{:error, {:transport, reason}}`
- Bug in job handler code -> `rescue`, log with full stacktrace, `reraise` to let the supervisor restart

---

## Why `defexception` and not plain atoms

Atoms like `:timeout` are too coarse for diagnosis. `defexception` defines structured exception types with named fields. When rescued, `e.job_id` and `e.reason` are available for structured logging -- not just a string message.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps, do: []
end
```

### Step 2: `lib/task_queue/worker.ex` -- custom exceptions and error handling

The Worker defines two custom exception types as nested modules. `JobError` carries a `job_id` and `reason` for domain-level failures. `AgentError` carries an `agent_address` and `status_code` for communication failures. The `exception/1` callback customizes how the exception is built from keyword options.

`execute/1` wraps all work in `try/rescue` so it never raises -- callers always get `{:ok, result}` or `{:error, reason}`.

`execute_with_agent/2` demonstrates the `after` clause: the connection is opened *before* the try block (so it is always bound), and `close_agent_connection/1` runs regardless of success or failure. The `reraise` in the rescue clause preserves the original stacktrace with `__STACKTRACE__` -- using an empty list `[]` instead would point to the rescue clause, losing the original origin.

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

  Never raises -- all exceptions are caught and converted to error tuples.

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

### Step 3: Tests

```elixir
# test/task_queue/error_handling_test.exs
defmodule TaskQueue.ErrorHandlingTest do
  use ExUnit.Case, async: true

  alias TaskQueue.Worker
  alias TaskQueue.Worker.{JobError, AgentError}

  describe "Worker.execute/1 -- error handling" do
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

  describe "Worker.execute_with_agent/2 -- after cleanup" do
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

  describe "JobError -- custom exception" do
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

  describe "AgentError -- custom exception" do
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

### Step 4: Run

```bash
mix test test/task_queue/error_handling_test.exs --trace
```

---

## Trade-off analysis

| Approach | Use case | Risk |
|----------|----------|------|
| `{:ok, _} / {:error, _}` | Expected failures in normal flow | Not composable with code that raises |
| `try/rescue` | Unexpected raises from libraries | Overuse turns normal flow into exception-driven code |
| `defexception` | Structured error context | Requires discipline -- don't add fields you never read |
| `reraise e, __STACKTRACE__` | Log-and-propagate pattern | Forgetting `__STACKTRACE__` loses the original origin |
| `after` clause | Resource cleanup | Value of `after` is discarded -- do not rely on its return |

---

## Common production mistakes

**1. Rescuing `Exception` -- catching everything including bugs**
Rescue only what you know how to handle. Catching `Exception` masks bugs in your own code.

**2. `reraise` without `__STACKTRACE__`**
Using `reraise e, []` points the stacktrace to the rescue clause, not the original raise.

**3. Using `try/rescue` for control flow that should use `with`**
`Map.fetch!/2` + rescue is slower and semantically misleading vs `Map.fetch/2` + case.

**4. Binding variables inside `try` that `after` depends on**
If `try` raises before the binding, `after` gets a `NameError`. Bind resources *before* `try`.

**5. Expecting `after` to change the return value**
The `after` clause's return value is always discarded.

---

## Resources

- [try, catch, and rescue -- Elixir official guide](https://elixir-lang.org/getting-started/try-catch-and-rescue.html)
- [Exception module -- official docs](https://hexdocs.pm/elixir/Exception.html)
- [defexception -- Kernel docs](https://hexdocs.pm/elixir/Kernel.html#defexception/1)
- [reraise/2 -- Kernel.SpecialForms](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#try/1)
