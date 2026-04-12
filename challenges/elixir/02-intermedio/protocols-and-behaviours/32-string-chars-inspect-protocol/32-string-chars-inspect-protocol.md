# String.Chars and Inspect Protocols

## Goal

Build a `task_queue` Job struct that implements the `String.Chars` protocol for human-readable string interpolation and the `Inspect` protocol for developer-friendly debug output. Learn why these protocols control how your structs appear in logs, IEx, and error messages.

---

## The two protocols

**`String.Chars`** controls what `to_string(value)` and string interpolation `"#{value}"` produce. It should return a human-readable representation for end-user output. For a Job, this is a compact identifier like `"send_email:abc-123 [failed]"`.

**`Inspect`** controls what `inspect(value)` produces. It is used by IEx, `IO.inspect/2`, and `dbg/1`. It should return a developer-friendly debug representation. For a Job, this is `#TaskQueue.Job<id: "abc-123", type: "send_email", status: :failed, retries: 2>`.

The key difference: `String.Chars` is for users; `Inspect` is for developers.

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
    [extra_applications: [:logger, :crypto]]
  end

  defp deps, do: []
end
```

### Step 2: `lib/task_queue/job.ex` -- struct with protocol implementations

The struct uses `@enforce_keys [:type]` to catch missing required fields at compile time. The `new/2` function generates a random hex ID using `:crypto.strong_rand_bytes/1`.

The `String.Chars` implementation returns a compact `"type:id [status]"` format. If `id` is nil (not yet persisted), it renders as `"new"`.

The `Inspect` implementation uses `Inspect.Algebra.concat/1` to build the output. It deliberately hides `args` (potentially large, may contain secrets) and `scheduled_at` (implementation detail). The field `retry_count` is renamed to `retries` for brevity in debug output.

```elixir
defmodule TaskQueue.Job do
  @moduledoc """
  Represents a unit of work in the task queue.

  Implements `String.Chars` for human-readable string interpolation
  and `Inspect` for developer-friendly debug output.
  """

  @enforce_keys [:type]

  defstruct [
    :id,
    :type,
    :last_error,
    :scheduled_at,
    args: %{},
    status: :pending,
    retry_count: 0
  ]

  @type t :: %__MODULE__{
    id:           String.t() | nil,
    type:         String.t(),
    args:         map(),
    status:       :pending | :running | :completed | :failed,
    retry_count:  non_neg_integer(),
    last_error:   String.t() | nil,
    scheduled_at: DateTime.t() | nil
  }

  @doc """
  Creates a new Job struct with a generated ID.

  ## Examples

      iex> job = TaskQueue.Job.new("send_email", %{to: "user@example.com"})
      iex> job.type
      "send_email"
      iex> job.status
      :pending

  """
  @spec new(String.t(), map()) :: t()
  def new(type, args \\ %{}) when is_binary(type) do
    %__MODULE__{
      id:   generate_id(),
      type: type,
      args: args
    }
  end

  defp generate_id do
    :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)
  end
end

defimpl String.Chars, for: TaskQueue.Job do
  def to_string(%TaskQueue.Job{id: id, type: type, status: status}) do
    "#{type}:#{id || "new"} [#{status}]"
  end
end

defimpl Inspect, for: TaskQueue.Job do
  import Inspect.Algebra

  def inspect(%TaskQueue.Job{id: id, type: type, status: status, retry_count: retries}, opts) do
    inner =
      Enum.join(
        [
          "id: #{Kernel.inspect(id)}",
          "type: #{Kernel.inspect(type)}",
          "status: #{Kernel.inspect(status)}",
          "retries: #{retries}"
        ],
        ", "
      )

    concat(["#TaskQueue.Job<", inner, ">"])
  end
end
```

### Step 3: Tests

```elixir
# test/task_queue/protocol_impl_test.exs
defmodule TaskQueue.ProtocolImplTest do
  use ExUnit.Case, async: true

  alias TaskQueue.Job

  describe "String.Chars implementation" do
    test "to_string/1 returns compact format" do
      job = %Job{id: "abc-123", type: "send_email", status: :pending, retry_count: 0}
      assert to_string(job) == "send_email:abc-123 [pending]"
    end

    test "string interpolation uses String.Chars" do
      job = %Job{id: "xyz-456", type: "run_report", status: :running, retry_count: 1}
      assert "Job: #{job}" == "Job: run_report:xyz-456 [running]"
    end

    test "nil id renders as 'new'" do
      job = %Job{id: nil, type: "heartbeat", status: :pending, retry_count: 0}
      assert to_string(job) == "heartbeat:new [pending]"
    end

    test "failed status is included" do
      job = %Job{id: "j1", type: "send_sms", status: :failed, retry_count: 3}
      assert String.contains?(to_string(job), "failed")
    end

    test "IO.puts does not raise" do
      job = %Job{id: "j2", type: "noop", status: :completed, retry_count: 0}
      assert capture_io(fn -> IO.puts(job) end) =~ "noop:j2"
    end
  end

  describe "Inspect implementation" do
    test "inspect/1 returns developer format" do
      job = %Job{id: "abc-123", type: "send_email", status: :failed, retry_count: 2}
      result = inspect(job)
      assert result =~ "#TaskQueue.Job<"
      assert result =~ "abc-123"
      assert result =~ "send_email"
      assert result =~ "failed"
      assert result =~ "retries: 2"
    end

    test "inspect does not expose args" do
      job = %Job{
        id: "j3",
        type: "send_email",
        args: %{to: "user@example.com", password: "secret"},
        status: :pending,
        retry_count: 0
      }
      result = inspect(job)
      refute result =~ "password"
      refute result =~ "secret"
    end

    test "inspect does not expose scheduled_at" do
      job = %Job{
        id: "j4",
        type: "noop",
        status: :pending,
        retry_count: 0,
        scheduled_at: ~U[2024-01-15 10:00:00Z]
      }
      result = inspect(job)
      refute result =~ "scheduled_at"
    end

    test "IO.inspect returns the job unchanged" do
      job = %Job{id: "j5", type: "noop", status: :pending, retry_count: 0}
      assert IO.inspect(job) == job
    end
  end

  describe "Job.new/2" do
    test "creates a job with a generated id" do
      job = Job.new("send_email", %{to: "user@example.com"})
      assert job.type == "send_email"
      assert job.args == %{to: "user@example.com"}
      assert is_binary(job.id)
      assert String.length(job.id) > 0
      assert job.status == :pending
      assert job.retry_count == 0
    end

    test "two jobs have different ids" do
      job1 = Job.new("noop")
      job2 = Job.new("noop")
      assert job1.id != job2.id
    end
  end

  defp capture_io(fun) do
    ExUnit.CaptureIO.capture_io(fun)
  end
end
```

### Step 4: Run

```bash
mix test test/task_queue/protocol_impl_test.exs --trace
```

---

## Trade-off analysis

| Approach | String interpolation | Debug output | Hides sensitive fields | Location |
|----------|---------------------|--------------|----------------------|----------|
| `String.Chars` + `Inspect` | automatic via `#{}` | automatic via `inspect/1` | yes (Inspect) | protocol implementations |
| Custom `to_string_job/1` | must call explicitly | N/A | yes | module function |
| Default struct inspect | N/A | yes but verbose | no -- shows all fields | automatic |
| `@derive [Inspect]` with except | N/A | yes, filtered | yes | module attribute |

`@derive [Inspect, except: [:args, :scheduled_at]]` would hide those fields automatically. Write a full `defimpl Inspect` when you need a custom format that `@derive` cannot produce -- like `#TaskQueue.Job<...>` with `retries:` instead of `retry_count:`, or computing derived fields.

---

## Common production mistakes

**1. `String.Chars` returning a binary that includes secrets**
If `args` contains API keys, every log line leaks credentials. Only include identifier fields.

**2. Calling `to_string` in `defimpl Inspect`**
The `Inspect` protocol expects developer context. Using `to_string(value)` inside `inspect/2` loses developer context.

**3. Raising in `String.Chars` for nil fields**
`to_string/1` is called in string interpolation inside `rescue` clauses and log handlers. If it raises, you lose the original error.

**4. Not testing with `IO.inspect`**
Your `Inspect` implementation must return the struct unchanged from `IO.inspect/2`.

**5. `@derive Inspect` with `:only` ignoring the `:id` field**
Debug output becomes ambiguous -- you cannot distinguish two different jobs of the same type and status.

---

## Resources

- [String.Chars protocol -- official docs](https://hexdocs.pm/elixir/String.Chars.html)
- [Inspect protocol -- official docs](https://hexdocs.pm/elixir/Inspect.html)
- [Inspect.Algebra -- building custom inspect output](https://hexdocs.pm/elixir/Inspect.Algebra.html)
- [Implementing protocols -- Elixir official guide](https://elixir-lang.org/getting-started/protocols.html)
