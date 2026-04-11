# Protocols: Type-Based Polymorphism

## Why protocols

Protocols let you add serialization behaviour to existing types without modifying them.
This is how `Inspect`, `Enumerable`, and `String.Chars` work in the standard library.

The key difference from behaviours:
- Behaviour dispatch: `handler_module.execute(job, ctx)` — you choose the module explicitly.
- Protocol dispatch: `Serializable.to_map(value)` — Elixir chooses the implementation
  automatically based on the runtime type of `value`.

---

## The business problem

Two protocols:

1. `TaskQueue.Serializable` — converts a value to a plain map suitable for JSON encoding.
2. `TaskQueue.Exportable` — converts a value to a human-readable log line string.

Both protocols must work for:
- `TaskQueue.Types.JobResult` — a struct holding outcome and metadata.
- `TaskQueue.Types.QueueStats` — a struct with queue depth and throughput counts.
- Native Elixir types: maps, lists, tuples.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── serializable.ex
│       ├── exportable.ex
│       └── types/
│           ├── job_result.ex
│           └── queue_stats.ex
├── test/
│   └── task_queue/
│       └── protocols_test.exs
└── mix.exs
```

---

## Implementation

### `lib/task_queue/serializable.ex`

```elixir
defprotocol TaskQueue.Serializable do
  @moduledoc """
  Converts a value to a plain map for JSON serialization.
  All keys must be strings. Nested values must also be serializable.
  """

  @fallback_to_any true

  @spec to_map(t()) :: map()
  def to_map(value)
end

defimpl TaskQueue.Serializable, for: Any do
  def to_map(value), do: %{"__opaque__" => inspect(value)}
end

defimpl TaskQueue.Serializable, for: Map do
  def to_map(map) do
    Enum.into(map, %{}, fn {k, v} ->
      {to_string(k), TaskQueue.Serializable.to_map(v)}
    end)
  end
end

defimpl TaskQueue.Serializable, for: List do
  def to_map(list) do
    %{"items" => Enum.map(list, &TaskQueue.Serializable.to_map/1)}
  end
end

defimpl TaskQueue.Serializable, for: Tuple do
  def to_map(tuple) do
    %{"tuple" => tuple |> Tuple.to_list() |> Enum.map(&TaskQueue.Serializable.to_map/1)}
  end
end

defimpl TaskQueue.Serializable, for: Integer do
  def to_map(n), do: %{"value" => n, "type" => "integer"}
end

defimpl TaskQueue.Serializable, for: Atom do
  def to_map(nil), do: %{"value" => nil}
  def to_map(bool) when is_boolean(bool), do: %{"value" => bool}
  def to_map(atom), do: %{"value" => Atom.to_string(atom), "type" => "atom"}
end
```

The `Map` implementation recursively calls `TaskQueue.Serializable.to_map/1` on each
value, which dispatches through the protocol again. The `@fallback_to_any true` directive
prevents `Protocol.UndefinedError` crashes when an unexpected type reaches the serializer.

### `lib/task_queue/exportable.ex`

```elixir
defprotocol TaskQueue.Exportable do
  @moduledoc """
  Converts a value to a single-line string for log export.
  Format: [TYPE] key=value key=value
  """

  @fallback_to_any true

  @spec to_log_line(t()) :: String.t()
  def to_log_line(value)
end

defimpl TaskQueue.Exportable, for: Any do
  def to_log_line(value), do: "[UNKNOWN] value=#{inspect(value)}"
end
```

### `lib/task_queue/types/job_result.ex`

```elixir
defmodule TaskQueue.Types.JobResult do
  @moduledoc "Struct representing the outcome of a single job execution attempt."

  @enforce_keys [:job_id, :outcome, :duration_ms, :attempts]
  defstruct [:job_id, :outcome, :duration_ms, :attempts, :value, :error]

  @doc "Creates a successful result."
  def ok(job_id, value, duration_ms, attempts) do
    %__MODULE__{job_id: job_id, outcome: :ok, duration_ms: duration_ms,
                attempts: attempts, value: value, error: nil}
  end

  @doc "Creates a failed result."
  def error(job_id, reason, duration_ms, attempts) do
    %__MODULE__{job_id: job_id, outcome: :error, duration_ms: duration_ms,
                attempts: attempts, value: nil, error: reason}
  end
end

defimpl TaskQueue.Serializable, for: TaskQueue.Types.JobResult do
  alias TaskQueue.Types.JobResult

  def to_map(%JobResult{} = r) do
    base = %{
      "job_id" => r.job_id,
      "outcome" => Atom.to_string(r.outcome),
      "duration_ms" => r.duration_ms,
      "attempts" => r.attempts
    }

    case r.outcome do
      :ok -> Map.put(base, "value", inspect(r.value))
      :error -> Map.put(base, "error", inspect(r.error))
    end
  end
end

defimpl TaskQueue.Exportable, for: TaskQueue.Types.JobResult do
  alias TaskQueue.Types.JobResult

  def to_log_line(%JobResult{outcome: :ok} = r) do
    "[JOB_OK] job_id=#{r.job_id} duration_ms=#{r.duration_ms} attempts=#{r.attempts}"
  end

  def to_log_line(%JobResult{outcome: :error} = r) do
    "[JOB_ERROR] job_id=#{r.job_id} error=\"#{inspect(r.error)}\" duration_ms=#{r.duration_ms} attempts=#{r.attempts}"
  end
end
```

### `lib/task_queue/types/queue_stats.ex`

```elixir
defmodule TaskQueue.Types.QueueStats do
  @moduledoc "Point-in-time snapshot of queue metrics."

  defstruct [
    :snapshot_at,
    :queue_depth,
    :jobs_processed_total,
    :jobs_failed_total,
    :avg_duration_ms
  ]
end

defimpl TaskQueue.Serializable, for: TaskQueue.Types.QueueStats do
  alias TaskQueue.Types.QueueStats

  def to_map(%QueueStats{} = s) do
    %{
      "snapshot_at" => s.snapshot_at,
      "queue_depth" => s.queue_depth,
      "jobs_processed_total" => s.jobs_processed_total,
      "jobs_failed_total" => s.jobs_failed_total,
      "avg_duration_ms" => s.avg_duration_ms
    }
  end
end

defimpl TaskQueue.Exportable, for: TaskQueue.Types.QueueStats do
  alias TaskQueue.Types.QueueStats

  def to_log_line(%QueueStats{} = s) do
    "[QUEUE_STATS] depth=#{s.queue_depth} processed=#{s.jobs_processed_total} " <>
    "failed=#{s.jobs_failed_total} avg_ms=#{s.avg_duration_ms}"
  end
end
```

### Tests

```elixir
# test/task_queue/protocols_test.exs
defmodule TaskQueue.ProtocolsTest do
  use ExUnit.Case, async: true

  alias TaskQueue.Serializable
  alias TaskQueue.Exportable
  alias TaskQueue.Types.{JobResult, QueueStats}

  describe "Serializable — native types" do
    test "Map with atom keys produces string keys" do
      result = Serializable.to_map(%{job_id: "j1", status: :ok})
      assert Map.has_key?(result, "job_id")
      assert Map.has_key?(result, "status")
    end

    test "List wraps items under 'items' key" do
      result = Serializable.to_map([1, 2, 3])
      assert %{"items" => [_ | _]} = result
    end

    test "Tuple wraps elements under 'tuple' key" do
      result = Serializable.to_map({:ok, 42})
      assert %{"tuple" => [_, _]} = result
    end

    test "nil atom serializes to nil value" do
      assert %{"value" => nil} = Serializable.to_map(nil)
    end

    test "fallback Any wraps opaque value" do
      result = Serializable.to_map(make_ref())
      assert Map.has_key?(result, "__opaque__")
    end
  end

  describe "Serializable — JobResult" do
    test "ok result includes outcome and value" do
      r = JobResult.ok("j1", :done, 123, 1)
      m = Serializable.to_map(r)
      assert m["job_id"] == "j1"
      assert m["outcome"] == "ok"
      assert Map.has_key?(m, "value")
    end

    test "error result includes outcome and error" do
      r = JobResult.error("j2", :timeout, 5_000, 3)
      m = Serializable.to_map(r)
      assert m["outcome"] == "error"
      assert Map.has_key?(m, "error")
      assert m["attempts"] == 3
    end
  end

  describe "Exportable — JobResult" do
    test "ok result log line contains [JOB_OK] and job_id" do
      r = JobResult.ok("j3", :result, 50, 1)
      line = Exportable.to_log_line(r)
      assert String.contains?(line, "[JOB_OK]")
      assert String.contains?(line, "j3")
    end

    test "error result log line contains [JOB_ERROR] and error info" do
      r = JobResult.error("j4", :network_error, 3_000, 2)
      line = Exportable.to_log_line(r)
      assert String.contains?(line, "[JOB_ERROR]")
      assert String.contains?(line, "j4")
    end
  end

  describe "Exportable — QueueStats" do
    test "stats log line contains [QUEUE_STATS] and depth" do
      stats = %QueueStats{
        snapshot_at: System.os_time(:millisecond),
        queue_depth: 7,
        jobs_processed_total: 200,
        jobs_failed_total: 5,
        avg_duration_ms: 88
      }

      line = Exportable.to_log_line(stats)
      assert String.contains?(line, "[QUEUE_STATS]")
      assert String.contains?(line, "depth=7")
    end
  end

  describe "Protocol dispatch" do
    test "same function dispatches correctly across multiple types" do
      values = [
        %{key: "val"},
        [1, 2],
        {:ok, :done},
        JobResult.ok("jx", :x, 1, 1)
      ]

      for v <- values do
        result = Serializable.to_map(v)
        assert is_map(result)
      end
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/protocols_test.exs --trace
```

---

## Common production mistakes

**1. Implementing for `Map` when you want a struct**
Structs are not `Map` from a protocol's perspective. `defimpl Serializable, for: Map`
is never called for `%JobResult{}`. Implement for the struct module explicitly.

**2. Forgetting `@fallback_to_any` and hitting Protocol.UndefinedError in production**
Without a fallback, any new data type causes a runtime crash.

**3. Infinite recursion in recursive protocol implementations**
Ensure every branch eventually reaches a concrete implementation (Integer, Atom, etc.).

**4. `for: Any` fallback that swallows type errors silently**
A fallback returning `%{}` hides bugs. Log or tag the opaque representation.

---

## Resources

- [Protocol — HexDocs](https://hexdocs.pm/elixir/Protocol.html)
- [Elixir Getting Started: Protocols](https://elixir-lang.org/getting-started/protocols.html)
- [Inspect Protocol](https://hexdocs.pm/elixir/Inspect.html)
