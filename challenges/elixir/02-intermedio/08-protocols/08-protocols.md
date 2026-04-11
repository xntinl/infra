# Protocols: Type-Based Polymorphism

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system now routes jobs through pluggable handlers (exercise 07). The next
need is **serialization**: job results, errors, and queue stats must be exported to a
monitoring dashboard, a log file, and an HTTP API. Each destination expects a different
format, and the data types involved are diverse — job maps, error tuples, handler results,
and runtime stats.

Protocols are the right mechanism: they let you add serialization behaviour to existing
types without modifying them. This is how the standard library's `Inspect`, `Enumerable`,
and `String.Chars` protocols work.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── serializable.ex          # ← the protocol (you implement this)
│       ├── exportable.ex            # ← second protocol (you implement this)
│       └── types/
│           ├── job_result.ex        # ← struct + protocol impls (you implement this)
│           └── queue_stats.ex       # ← struct + protocol impls (you implement this)
├── test/
│   └── task_queue/
│       └── protocols_test.exs       # given tests — must pass without modification
└── mix.exs
```

---

## Why protocols and not behaviours here

The previous exercise used behaviours because you were plugging in entire strategy modules.
Protocols solve a different problem: you have **existing data types** (maps, tuples, structs
defined in other modules) and you want to add the same operation to all of them without
modifying their source code.

The key difference:

- Behaviour dispatch: `handler_module.execute(job, ctx)` — you choose the module explicitly.
- Protocol dispatch: `Serializable.to_map(value)` — Elixir chooses the implementation
  automatically based on the runtime type of `value`.

If your team adds a new result type six months from now, they implement the protocol in
their module without touching yours. Open/closed principle via language mechanics.

---

## The business problem

Two protocols:

1. `TaskQueue.Serializable` — converts a value to a plain map suitable for JSON encoding.
   Needed by the HTTP API export.

2. `TaskQueue.Exportable` — converts a value to a human-readable log line string.
   Needed by the log file export.

Both protocols must work for:
- `TaskQueue.Types.JobResult` — a struct holding `{:ok, value} | {:error, reason}` and
  metadata.
- `TaskQueue.Types.QueueStats` — a struct with queue depth, throughput, and error counts.
- Native Elixir types: maps, lists, tuples (used in job payloads).

---

## Implementation

### Step 1: `lib/task_queue/serializable.ex`

```elixir
defprotocol TaskQueue.Serializable do
  @moduledoc """
  Converts a value to a plain map for JSON serialization.
  All keys must be strings. Nested values must also be serializable.
  """

  @fallback_to_any true

  @doc "Returns a map with string keys suitable for JSON.encode!/1."
  @spec to_map(t()) :: map()
  def to_map(value)
end

# Fallback: unknown types become an opaque string representation
defimpl TaskQueue.Serializable, for: Any do
  def to_map(value), do: %{"__opaque__" => inspect(value)}
end

defimpl TaskQueue.Serializable, for: Map do
  def to_map(map) do
    # HINT: Enum.into(map, %{}, fn {k, v} -> {to_string(k), TaskQueue.Serializable.to_map(v)} end)
    # This recursively serializes nested values via the protocol
    # TODO: implement
  end
end

defimpl TaskQueue.Serializable, for: List do
  def to_map(list) do
    # HINT: wrap the list: %{"items" => Enum.map(list, &TaskQueue.Serializable.to_map/1)}
    # TODO: implement
  end
end

defimpl TaskQueue.Serializable, for: Tuple do
  def to_map(tuple) do
    # HINT: %{"tuple" => tuple |> Tuple.to_list() |> Enum.map(&TaskQueue.Serializable.to_map/1)}
    # TODO: implement
  end
end

defimpl TaskQueue.Serializable, for: Integer do
  # Integers are already JSON-safe — return as-is wrapped in a consistent envelope
  def to_map(n), do: %{"value" => n, "type" => "integer"}
end

defimpl TaskQueue.Serializable, for: Atom do
  def to_map(nil), do: %{"value" => nil}
  def to_map(bool) when is_boolean(bool), do: %{"value" => bool}
  def to_map(atom), do: %{"value" => Atom.to_string(atom), "type" => "atom"}
end
```

### Step 2: `lib/task_queue/exportable.ex`

```elixir
defprotocol TaskQueue.Exportable do
  @moduledoc """
  Converts a value to a single-line string for log export.
  The format is structured: [TYPE] key=value key=value
  """

  @fallback_to_any true

  @doc "Returns a log-formatted string."
  @spec to_log_line(t()) :: String.t()
  def to_log_line(value)
end

defimpl TaskQueue.Exportable, for: Any do
  def to_log_line(value), do: "[UNKNOWN] value=#{inspect(value)}"
end
```

### Step 3: `lib/task_queue/types/job_result.ex`

```elixir
defmodule TaskQueue.Types.JobResult do
  @moduledoc "Struct representing the outcome of a single job execution attempt."

  @enforce_keys [:job_id, :outcome, :duration_ms, :attempts]
  defstruct [:job_id, :outcome, :duration_ms, :attempts, :value, :error]

  # outcome: :ok | :error

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
    # HINT: build a string-keyed map with all fields
    # For :ok outcome, include "value" key
    # For :error outcome, include "error" key as inspect(r.error)
    # TODO: implement
  end
end

defimpl TaskQueue.Exportable, for: TaskQueue.Types.JobResult do
  alias TaskQueue.Types.JobResult

  def to_log_line(%JobResult{outcome: :ok} = r) do
    # Format: [JOB_OK] job_id=xxx duration_ms=123 attempts=1
    # HINT: "[JOB_OK] job_id=#{r.job_id} duration_ms=#{r.duration_ms} attempts=#{r.attempts}"
    # TODO: implement
  end

  def to_log_line(%JobResult{outcome: :error} = r) do
    # Format: [JOB_ERROR] job_id=xxx error="reason" duration_ms=123 attempts=3
    # TODO: implement
  end
end
```

### Step 4: `lib/task_queue/types/queue_stats.ex`

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
    # HINT: all fields, string keys, snapshot_at as Unix ms integer
    # TODO: implement
  end
end

defimpl TaskQueue.Exportable, for: TaskQueue.Types.QueueStats do
  alias TaskQueue.Types.QueueStats

  def to_log_line(%QueueStats{} = s) do
    # Format: [QUEUE_STATS] depth=5 processed=100 failed=2 avg_ms=45
    # TODO: implement
  end
end
```

### Step 5: Given tests — must pass without modification

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

      # All must return a map without raising
      for v <- values do
        result = Serializable.to_map(v)
        assert is_map(result)
      end
    end
  end
end
```

### Step 6: Run the tests

```bash
mix test test/task_queue/protocols_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Protocol | Behaviour | Plain function with `cond/case` |
|--------|----------|-----------|--------------------------------|
| Open to extension | Yes — add `defimpl` anywhere | No — need to modify the dispatcher | No — modify the function |
| Dispatch | Runtime, by type | Explicit module argument | Explicit type check at call site |
| Performance | Compiled dispatch table — very fast | Direct module call — fast | Pattern match — fast |
| `@fallback_to_any` | Yes — safe default for unknown types | N/A | Explicit `_` clause |
| When to use | Adding capabilities to data types | Pluggable module strategies | Small, stable type sets |

Reflection question: the `Serializable` implementation for `List` wraps the list in
`%{"items" => [...]}` to produce a valid map. But if the consumer expects a JSON array,
not an object, this design is wrong. What protocol function signature change would let
the implementation return a list directly, and what would break?

---

## Common production mistakes

**1. Implementing for `Map` when you want a struct**
Structs are not `Map` from a protocol's perspective. A `defimpl Serializable, for: Map`
clause is never called for `%JobResult{}`. Implement for the struct module explicitly.

**2. Forgetting `@fallback_to_any` and hitting Protocol.UndefinedError in production**
Without a fallback, any new data type that reaches your protocol causes a runtime crash.
Add `@fallback_to_any true` with a safe `for: Any` implementation so new types fail
gracefully and observably.

**3. Infinite recursion in recursive protocol implementations**
If `Serializable.to_map` for `Map` calls itself on values without a base case for
non-map types, you will stackoverflow on deeply nested structures. Ensure every branch
eventually reaches a concrete implementation.

**4. Implementing protocols for `Any` that swallow type errors silently**
A `for: Any` fallback that returns `%{}` for unknown types can hide bugs. Log or tag
the opaque representation so unknown types are discoverable in monitoring.

---

## Resources

- [Protocol — HexDocs](https://hexdocs.pm/elixir/Protocol.html)
- [Elixir Getting Started: Protocols](https://elixir-lang.org/getting-started/protocols.html)
- [Enumerable Protocol source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/enumerable.ex) — study how the standard library uses protocols
- [Inspect Protocol](https://hexdocs.pm/elixir/Inspect.html) — the protocol you call every time you use `IO.inspect`
