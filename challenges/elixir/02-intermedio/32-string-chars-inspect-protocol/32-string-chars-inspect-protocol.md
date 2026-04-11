# String.Chars and Inspect Protocols

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` logs job execution details, renders job summaries in CLI output, and includes job context in error messages. Currently every log line calls `inspect/1` or manually extracts fields — the resulting strings are verbose, contain internal implementation details, and are inconsistent across modules.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── job.ex                  # ← you implement String.Chars and Inspect here
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── protocol_impl_test.exs  # given tests — must pass without modification
└─�� mix.exs
```

---

## The business problem

Current log output for a failed job looks like this:

```
[error] Job failed: %TaskQueue.Job{id: "abc-123", type: "send_email",
  args: %{to: "user@example.com", subject: "Welcome"}, status: :failed,
  retry_count: 2, last_error: "timeout", inserted_at: ~U[2024-01-15 10:30:00Z]}
```

The ops team needs:
1. **String interpolation** — `"Processing #{job}"` should produce `"Processing send_email:abc-123"` — a compact identifier, not the full struct dump
2. **Inspectable output** — `inspect(job, pretty: true)` should show only fields relevant for debugging, hiding internal implementation details like raw timestamps
3. **Consistent format** — job references in logs, error messages, and CLI output must look the same everywhere without manual field extraction

---

## Why `String.Chars` and `Inspect` and not `to_string/1` or custom functions

You could define `def job_to_string(job)` in `TaskQueue.Job` and call it everywhere. The problem is that:
- String interpolation `"#{job}"` calls `to_string(job)` which calls `String.Chars.to_string/1` — if not implemented, it raises `Protocol.UndefinedError`
- `IO.puts(job)` and `Logger.info(job)` both invoke `String.Chars`
- `IO.inspect(job)` and `dbg(job)` both invoke `Inspect`

Implementing these protocols means your struct works naturally everywhere strings and inspection are needed — no special function calls required.

---

## The two protocols

**`String.Chars`** controls what `to_string(value)` and string interpolation `"#{value}"` produce. It should return a human-readable representation suitable for end-user output — not debug output. For `TaskQueue.Job`, this is a compact identifier like `"send_email:abc-123 [failed]"`.

**`Inspect`** controls what `inspect(value)` produces. It is used by IEx, `IO.inspect/2`, and the `dbg/1` macro. It should return a string that could be pasted back into Elixir code and evaluated, or at minimum a clear debug representation. For `TaskQueue.Job`, this is `#TaskQueue.Job<id: "abc-123", type: "send_email", status: :failed>`.

The key difference: `String.Chars` is for users; `Inspect` is for developers.

---

## Implementation

### Step 1: `lib/task_queue/job.ex` — struct with protocol implementations

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
  @doc """
  Returns a compact human-readable string for the job.

  Format: "{type}:{id} [{status}]"
  If id is nil, uses "new" as the id placeholder.
  """
  def to_string(%TaskQueue.Job{id: id, type: type, status: status}) do
    "#{type}:#{id || "new"} [#{status}]"
  end
end

defimpl Inspect, for: TaskQueue.Job do
  import Inspect.Algebra

  @doc """
  Returns a developer-friendly debug representation.

  Shows id, type, status, and retry_count. Hides args (potentially large)
  and scheduled_at (implementation detail).

  Format: #TaskQueue.Job<id: "abc-123", type: "send_email", status: :failed, retries: 2>
  """
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

### Step 2: Given tests — must pass without modification

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
      # If String.Chars is not implemented, this raises Protocol.UndefinedError
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
      # IO.inspect returns its argument — this verifies the Inspect protocol works
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

  # Helper for testing IO.puts output
  defp capture_io(fun) do
    ExUnit.CaptureIO.capture_io(fun)
  end
end
```

### Step 3: Run the tests

```bash
mix test test/task_queue/protocol_impl_test.exs --trace
```

---

## Trade-off analysis

| Approach | String interpolation | Debug output | Hides sensitive fields | Location |
|----------|---------------------|--------------|----------------------|----------|
| `String.Chars` + `Inspect` | automatic via `#{}` | automatic via `inspect/1` | yes (Inspect) | protocol implementations |
| Custom `to_string_job/1` function | must call explicitly | N/A | yes | module function |
| Default struct inspect | N/A | yes but verbose | no — shows all fields | automatic |
| `@derive [Inspect]` with except | N/A | yes, filtered | yes | module attribute |

Reflection question: `@derive [Inspect, except: [:args, :scheduled_at]]` would hide those fields automatically without a custom implementation. When would you write a full `defimpl Inspect` instead of using `@derive`?

Answer: When you need a custom format that `@derive` cannot produce — for example, the `#TaskQueue.Job<...>` format with `retries:` instead of `retry_count:`, or computing derived fields (like duration from timestamps), or conditionally showing fields based on status. `@derive` only supports `:only` and `:except` for filtering fields; it cannot rename, transform, or conditionally display them.

---

## Common production mistakes

**1. `String.Chars` returning a binary that includes secrets**

If `args` contains API keys or passwords and your `to_string` includes all args, every log line in production leaks credentials. The `String.Chars` implementation should include only identifier fields.

**2. Calling `to_string` in `defimpl Inspect`**

The `Inspect` protocol expects a string formatted for developer inspection, not user display. Using `to_string(value)` inside `inspect/2` breaks `IO.inspect` formatting and `dbg` output:

```elixir
# Wrong — uses String.Chars in Inspect, loses developer context
def inspect(job, _opts) do
  to_string(job)
end

# Right — returns a developer-readable representation
def inspect(job, opts) do
  "#TaskQueue.Job<id: #{Kernel.inspect(job.id, opts)}, ...>"
end
```

**3. Raising in `String.Chars` for nil fields**

`to_string/1` is called in string interpolation inside `rescue` clauses and log handlers. If it raises, you lose the original error and see a confusing `Protocol.UndefinedError` or pattern match failure instead.

**4. Not testing with `IO.inspect`**

Your `Inspect` implementation must return the struct unchanged from `IO.inspect/2`. If `inspect/2` returns a string instead of an `Inspect.Algebra` document, `IO.inspect` may not return the original value:

```elixir
# Test that IO.inspect is identity on your struct
assert IO.inspect(job) == job
```

**5. `@derive Inspect` with `:only` ignoring the `:id` field**

If you use `@derive [Inspect, only: [:type, :status]]` and omit `:id`, debug output is ambiguous — you cannot distinguish two different jobs of the same type and status in IEx.

---

## Resources

- [String.Chars protocol — official docs](https://hexdocs.pm/elixir/String.Chars.html)
- [Inspect protocol — official docs](https://hexdocs.pm/elixir/Inspect.html)
- [Inspect.Algebra — building custom inspect output](https://hexdocs.pm/elixir/Inspect.Algebra.html)
- [Implementing protocols — Elixir official guide](https://elixir-lang.org/getting-started/protocols.html)
