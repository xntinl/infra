# Streams and Lazy Evaluation

## Eager vs lazy — the memory model

`Enum` is eager: it processes the entire collection and materializes the result before
returning. `Stream` is lazy: each element passes through the entire pipeline before the
next element enters. No intermediate collection is allocated.

This matters when:
- The input is larger than available memory (file, network stream).
- You only need the first N results — lazy evaluation stops early.
- The collection is conceptually infinite (sequence generators).

---

## The business problem

Build a `TaskQueue.LogReader` that processes task queue log files:

1. Read a large log file line by line, parsing each line into a structured map.
2. Filter, transform, and aggregate without loading the file into memory.
3. Generate infinite cron trigger time streams using `Stream.iterate`.
4. Implement a bounded retry stream with backoff.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       └── log_reader.ex
├── test/
│   └── task_queue/
│       └── streams_test.exs
└── mix.exs
```

---

## Implementation

### `lib/task_queue/log_reader.ex`

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
  Each element is a map with :timestamp, :job_id, :status, :duration_ms, :worker_id.
  Lines that do not match the expected format are silently skipped.
  """
  @spec stream_file(Path.t()) :: Enumerable.t()
  def stream_file(path) do
    path
    |> File.stream!()
    |> Stream.map(&String.trim/1)
    |> Stream.reject(&(&1 == ""))
    |> Stream.map(&parse_line/1)
    |> Stream.filter(fn
      {:ok, _} -> true
      :error -> false
    end)
    |> Stream.map(fn {:ok, entry} -> entry end)
  end

  @doc """
  Returns the count of entries with the given status in the log file.
  Processes the file as a stream — O(1) memory regardless of file size.
  """
  @spec count_by_status(Path.t(), atom()) :: non_neg_integer()
  def count_by_status(path, status) do
    path
    |> stream_file()
    |> Stream.filter(fn entry -> entry.status == status end)
    |> Enum.count()
  end

  @doc """
  Returns the first `n` failed entries from the log file.
  Stops reading the file after finding n failures.
  """
  @spec first_failures(Path.t(), pos_integer()) :: [map()]
  def first_failures(path, n) do
    path
    |> stream_file()
    |> Stream.filter(fn entry -> entry.status == :error end)
    |> Enum.take(n)
  end

  @doc """
  Generates an infinite stream of cron trigger timestamps starting from `start_ms`,
  with each trigger `interval_ms` apart.
  """
  @spec cron_schedule_stream(pos_integer(), pos_integer()) :: Enumerable.t()
  def cron_schedule_stream(start_ms, interval_ms) do
    Stream.iterate(start_ms, fn ts -> ts + interval_ms end)
  end

  @doc """
  Returns the next `count` trigger timestamps starting at or after `after_ms`.
  """
  @spec next_triggers(pos_integer(), pos_integer(), pos_integer()) :: [pos_integer()]
  def next_triggers(after_ms, interval_ms, count) do
    after_ms
    |> cron_schedule_stream(interval_ms)
    |> Enum.take(count)
  end

  @doc """
  Retries `operation` up to `max_attempts` times with exponential backoff.

  Returns {:ok, result} on first success or {:error, :max_attempts} on final failure.
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
      match?({:error, _}, result)
    end)
    |> Enum.take(1)
    |> case do
      [{_attempt, {:ok, _} = success}] -> success
      _ -> {:error, :max_attempts}
    end
  end

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

The `stream_file/1` function builds a pipeline of lazy transformations. None execute
until a terminal function (`Enum.to_list`, `Enum.count`, `Enum.take`) consumes the stream.

The `retry_stream/2` function uses `Stream.drop_while/2` to skip failed attempts. As
soon as the first success is found, `Enum.take(1)` stops the stream — no further
attempts are made.

### Tests

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

### Run the tests

```bash
mix test test/task_queue/streams_test.exs --trace
```

---

## Common production mistakes

**1. Calling `Enum.to_list` on an infinite stream**
`Stream.iterate(0, &(&1 + 1)) |> Enum.to_list()` will run forever. Always pair infinite
streams with `Enum.take/2`.

**2. Side effects in `Stream.map` without a terminal function**
```elixir
# Does NOTHING — the stream is defined but never consumed
File.stream!("big.log") |> Stream.map(&IO.puts/1)

# Correct — Stream.run/1 consumes the stream
File.stream!("big.log") |> Stream.map(&IO.puts/1) |> Stream.run()
```

**3. Forgetting that `Stream.map` returns a stream, not a list**
Use `Stream.map` in pipelines; use `Enum.map` when you need the result immediately.

**4. Using streams for small in-memory collections**
Streams have per-element overhead. For a list of 100 items, `Enum.map` is faster.

---

## Resources

- [Stream — HexDocs](https://hexdocs.pm/elixir/Stream.html)
- [File.stream!/1 — HexDocs](https://hexdocs.pm/elixir/File.html#stream!/1)
- [Enum vs Stream — Elixir School](https://elixirschool.com/en/lessons/basics/enum#lazy-evaluation-2)
