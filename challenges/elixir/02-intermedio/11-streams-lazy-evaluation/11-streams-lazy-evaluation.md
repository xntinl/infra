# Streams and Lazy Evaluation

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system writes job results to a log file and needs to read them back for
reporting. A log file with 10 million entries cannot be loaded into memory with
`File.read!`. The system also needs to generate scheduling sequences (cron trigger times)
that are conceptually infinite — you stop when you have enough, not when the sequence ends.

Streams solve both problems: they produce values on demand, one at a time, without
materializing the entire sequence in memory.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       └── log_reader.ex        # ← you implement this
├── test/
│   └── task_queue/
│       └── streams_test.exs     # given tests — must pass without modification
└── mix.exs
```

---

## Eager vs lazy — the memory model

`Enum` is eager: it processes the entire collection and materializes the result before
returning. Pipelines like `list |> Enum.filter(...) |> Enum.map(...)` allocate two
intermediate lists.

`Stream` is lazy: each element passes through the entire pipeline before the next element
enters. No intermediate collection is allocated. The pipeline does not execute at all until
a terminal function (`Enum.to_list`, `Enum.take`, `Stream.run`) consumes it.

```
Enum (eager):
  input → [filter → intermediate list] → [map → final list]

Stream (lazy):
  element_1 → filter → map → consumer
  element_2 → filter → map → consumer
  element_3 → filter (dropped)
  element_4 → filter → map → consumer
```

This matters when:
- The input is larger than available memory (file, network stream, paginated API).
- You only need the first N results — lazy evaluation stops early.
- The collection is conceptually infinite (sequence generators).

This does not matter when:
- The full result is needed and it fits in memory — `Enum` is simpler.
- The collection is small — lazy overhead (function calls per element) exceeds the savings.

---

## The business problem

`TaskQueue.LogReader` processes task_queue log files:

1. Read a large log file line by line, parsing each line into a structured map.
2. Filter, transform, and aggregate without loading the file into memory.
3. Generate a stream of scheduled cron trigger times using `Stream.iterate`.
4. Implement a bounded retry stream: given a failing operation, retry with backoff until
   success or max attempts reached.

---

## Implementation

### Step 1: `lib/task_queue/log_reader.ex`

```elixir
defmodule TaskQueue.LogReader do
  @moduledoc """
  Processes task_queue log files using lazy streams.
  File I/O never loads more than one line into memory at a time.
  """

  # Log line format: "timestamp|job_id|status|duration_ms|worker_id"
  # Example: "1712345678000|job_abc|ok|123|worker_1"

  @doc """
  Returns a lazy stream of parsed log entries from a file.
  Each element is a map: %{timestamp: integer, job_id: string, status: atom,
                            duration_ms: integer, worker_id: string}
  Lines that do not match the expected format are silently skipped.
  """
  @spec stream_file(Path.t()) :: Enumerable.t()
  def stream_file(path) do
    # HINT: File.stream!(path) produces a stream of lines (with trailing newline)
    # HINT: Stream.map to String.trim each line
    # HINT: Stream.filter to keep non-empty lines
    # HINT: Stream.map to parse_line/1
    # HINT: Stream.filter to reject :error (keep only {:ok, entry} values)
    # HINT: Stream.map to unwrap {:ok, entry} → entry
    # TODO: implement
  end

  @doc """
  Returns the count of entries with the given status in the log file.
  Processes the file as a stream — O(1) memory regardless of file size.
  """
  @spec count_by_status(Path.t(), atom()) :: non_neg_integer()
  def count_by_status(path, status) do
    # HINT: stream_file(path) |> Stream.filter(...) |> Enum.count()
    # TODO: implement
  end

  @doc """
  Returns the first `n` failed entries from the log file.
  Stops reading the file after finding n failures — does not scan the whole file.
  """
  @spec first_failures(Path.t(), pos_integer()) :: [map()]
  def first_failures(path, n) do
    # HINT: stream_file(path) |> Stream.filter(status == :error) |> Enum.take(n)
    # TODO: implement
  end

  @doc """
  Generates an infinite stream of cron trigger timestamps starting from `start_ms`,
  with each trigger `interval_ms` apart.

  Returns a Stream that yields integers (Unix millisecond timestamps).
  The caller controls how many to take.
  """
  @spec cron_schedule_stream(pos_integer(), pos_integer()) :: Enumerable.t()
  def cron_schedule_stream(start_ms, interval_ms) do
    # HINT: Stream.iterate(start_ms, fn ts -> ts + interval_ms end)
    # This produces an infinite stream — never enumerate it without Enum.take/Stream.take
    # TODO: implement
  end

  @doc """
  Returns the next `count` trigger timestamps starting at or after `after_ms`.
  """
  @spec next_triggers(pos_integer(), pos_integer(), pos_integer()) :: [pos_integer()]
  def next_triggers(after_ms, interval_ms, count) do
    # HINT: cron_schedule_stream(after_ms, interval_ms) |> Enum.take(count)
    # TODO: implement
  end

  @doc """
  Retries `operation` (a zero-arg function) up to `max_attempts` times with
  exponential backoff between attempts.

  Returns {:ok, result} on first success or {:error, :max_attempts} on final failure.
  Uses Stream.resource/3 to model the retry sequence as a lazy stream.
  """
  @spec retry_stream((() -> {:ok, any()} | {:error, any()}), pos_integer()) ::
          {:ok, any()} | {:error, :max_attempts}
  def retry_stream(operation, max_attempts) do
    1..max_attempts
    |> Stream.map(fn attempt ->
      backoff_ms = if attempt > 1, do: (100 * :math.pow(2, attempt - 2)) |> round(), else: 0
      if backoff_ms > 0, do: Process.sleep(backoff_ms)
      {attempt, operation.()}
    end)
    |> Stream.drop_while(fn {_attempt, result} ->
      # HINT: keep iterating (drop) while result is {:error, _}
      # Stop (keep) when result is {:ok, _}
      # TODO: implement
    end)
    |> Enum.take(1)
    |> case do
      [{_attempt, {:ok, _} = success}] -> success
      _ -> {:error, :max_attempts}
    end
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  @spec parse_line(String.t()) :: {:ok, map()} | :error
  defp parse_line(line) do
    case String.split(line, "|") do
      [ts_str, job_id, status_str, duration_str, worker_id] ->
        with {ts, ""} <- Integer.parse(ts_str),
             {duration_ms, ""} <- Integer.parse(duration_str),
             status when status in [:ok, :error, :timeout] <-
               String.to_existing_atom(status_str) do
          {:ok, %{
            timestamp: ts,
            job_id: job_id,
            status: status,
            duration_ms: duration_ms,
            worker_id: worker_id
          }}
        else
          _ -> :error
        end

      _ ->
        :error
    end
  end
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/streams_test.exs
defmodule TaskQueue.StreamsTest do
  use ExUnit.Case, async: true

  alias TaskQueue.LogReader

  @log_content """
  1712345678000|job_001|ok|50|worker_1
  1712345679000|job_002|error|200|worker_1
  1712345680000|job_003|ok|30|worker_2
  1712345681000|job_004|error|100|worker_2
  1712345682000|job_005|ok|500|worker_1
  1712345683000|job_006|timeout|80|worker_3
  INVALID_LINE
  """

  setup do
    path = Path.join(System.tmp_dir!(), "task_queue_test_#{:rand.uniform(999_999)}.log")
    File.write!(path, @log_content)
    on_exit(fn -> File.rm(path) end)
    {:ok, path: path}
  end

  describe "stream_file/1" do
    test "parses valid lines and skips invalid ones", %{path: path} do
      entries = LogReader.stream_file(path) |> Enum.to_list()
      assert length(entries) == 6
      assert Enum.all?(entries, &is_map/1)
      assert Enum.all?(entries, &Map.has_key?(&1, :job_id))
    end

    test "correctly parses job_id and status", %{path: path} do
      first = LogReader.stream_file(path) |> Enum.at(0)
      assert first.job_id == "job_001"
      assert first.status == :ok
      assert first.duration_ms == 50
    end
  end

  describe "count_by_status/2" do
    test "counts ok entries", %{path: path} do
      assert 3 = LogReader.count_by_status(path, :ok)
    end

    test "counts error entries", %{path: path} do
      assert 2 = LogReader.count_by_status(path, :error)
    end
  end

  describe "first_failures/2" do
    test "returns at most n failures", %{path: path} do
      failures = LogReader.first_failures(path, 1)
      assert length(failures) == 1
      assert hd(failures).status == :error
    end

    test "returns all failures when n exceeds count", %{path: path} do
      failures = LogReader.first_failures(path, 100)
      assert length(failures) == 2
    end
  end

  describe "cron_schedule_stream/2" do
    test "generates trigger times at the given interval" do
      triggers = LogReader.next_triggers(1_000_000, 3_600_000, 3)
      assert [1_000_000, 4_600_000, 8_200_000] = triggers
    end

    test "stream is infinite — Enum.take stops it without error" do
      stream = LogReader.cron_schedule_stream(0, 1_000)
      first_10 = Enum.take(stream, 10)
      assert length(first_10) == 10
      assert Enum.at(first_10, 0) == 0
      assert Enum.at(first_10, 9) == 9_000
    end
  end

  describe "retry_stream/2" do
    test "returns result on first success" do
      op = fn -> {:ok, :result} end
      assert {:ok, :result} = LogReader.retry_stream(op, 3)
    end

    test "retries and succeeds on second attempt" do
      calls = Agent.start_link(fn -> 0 end) |> elem(1)
      op = fn ->
        n = Agent.get_and_update(calls, fn n -> {n + 1, n + 1} end)
        if n < 2, do: {:error, :not_yet}, else: {:ok, :success}
      end
      assert {:ok, :success} = LogReader.retry_stream(op, 5)
      Agent.stop(calls)
    end

    test "returns error after max attempts" do
      op = fn -> {:error, :always_fails} end
      assert {:error, :max_attempts} = LogReader.retry_stream(op, 3)
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/task_queue/streams_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Stream (lazy) | Enum (eager) | File.read! + String.split |
|--------|--------------|-------------|--------------------------|
| Memory for 1 GB file | O(1) — one line at a time | O(n) — full file in memory | Crashes on large files |
| Time to first result | Immediate | After full processing | After full file read |
| Composability | High — pipeline of transformations | High | Low |
| Can stop early? | Yes — `Enum.take` stops processing | No — entire collection is traversed | No |
| Overhead per element | Higher — function call chain | Lower — direct traversal | Lowest |
| Infinite sequences | Yes — `Stream.iterate`, `Stream.cycle` | No | No |

Reflection question: `retry_stream/2` uses `Stream.drop_while` to skip failed attempts
and `Enum.take(1)` to get the first success. What happens if you replace `Enum.take(1)`
with `Enum.to_list()`? Why does the backoff `Process.sleep` still happen for all attempts
even though only the successful one is returned?

---

## Common production mistakes

**1. Calling `Enum.to_list` on an infinite stream**
`Stream.iterate(0, &(&1 + 1)) |> Enum.to_list()` will run forever, consuming all memory.
Always pair infinite streams with `Enum.take/2` or `Stream.take/2`.

**2. Side effects in `Stream.map` without a terminal function**
```elixir
# This does NOTHING — the stream is defined but never consumed
File.stream!("big.log") |> Stream.map(&IO.puts/1)

# Correct — Enum.run/1 consumes the stream for side effects
File.stream!("big.log") |> Stream.map(&IO.puts/1) |> Stream.run()
```

**3. Forgetting that `Stream.map` is not `Enum.map`**
`Stream.map(collection, fn)` returns a lazy stream description, not a list.
`Enum.map(collection, fn)` executes immediately and returns a list.
Use `Stream.map` in pipelines; use `Enum.map` when you need the result immediately.

**4. Using streams for small in-memory collections**
Streams have per-element overhead from the function call chain. For a list of 100
items, `Enum.map` is faster than `Stream.map`. Streams pay off at thousands of items
or when memory is the constraint.

---

## Resources

- [Stream — HexDocs](https://hexdocs.pm/elixir/Stream.html)
- [File.stream!/1 — HexDocs](https://hexdocs.pm/elixir/File.html#stream!/1)
- [Stream.resource/3 — HexDocs](https://hexdocs.pm/elixir/Stream.html#resource/3) — for wrapping stateful external resources
- [Enum vs Stream — Elixir School](https://elixirschool.com/en/lessons/basics/enum#lazy-evaluation-2)
