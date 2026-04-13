# Stream.resource and Lazy Infinite Sequences

**Project**: `log_tailer` — a `tail -f`-style lazy file watcher built with `Stream.resource/3`

---

## Project structure

```
log_tailer/
├── lib/
│   └── log_tailer.ex              # tailer + lazy helpers
├── test/
│   └── log_tailer_test.exs        # ExUnit tests
└── mix.exs
```

---

## What you will learn

Two core concepts:

1. **`Stream.unfold/2`** — builds a lazy infinite (or finite) sequence from a seed value.
   Pure, no side effects, no cleanup. Ideal for mathematical sequences or in-memory generators.
2. **`Stream.resource/3`** — like `unfold/2` but with explicit `start` and `after` functions
   that manage a stateful external resource (file handle, socket, db cursor). The `after`
   callback is **guaranteed to run** even if the consumer halts early or raises.

The mental model: both are "pull-based" — nothing is computed until a terminal operation
(`Enum.take/2`, `Enum.to_list/1`, `Stream.run/1`) requests values.

---

## The business problem

You need a `tail -f`-style log watcher for your own monitoring scripts:

- Open a log file and read the existing bytes.
- When more lines are appended by another process, read them too — never re-read old lines.
- On halt (exception, `take`, timeout), close the file handle cleanly. A leaked FD is a
  production bug that shows up two weeks later as "too many open files".

You'll also build a simple `Stream.unfold/2` helper for a Fibonacci-like lazy sequence,
to contrast the stateless case with the stateful one.

---

## Why `Stream.resource` and not `File.stream!/3`

`File.stream!/3` reads a file line by line, but it stops at EOF. For a growing log file,
EOF is not "end of data" — it's "no data right now, try again later". `Stream.resource`
lets you own the loop: on EOF, sleep briefly and retry instead of halting.

`File.stream!/3` also closes on error but not on early halt in all runtime versions.
`Stream.resource` has explicit cleanup semantics — the `after` function runs no matter what.

---

## Design decisions

**Option A — spawn a GenServer that reads the file and sends lines via `send/2`**
- Pros: explicit process model; back-pressure via selective receive; restartable under a supervisor.
- Cons: heavy for a read-only tailer; consumers must implement their own mailbox handling; halting cleanly requires a bespoke protocol; you've just reinvented a worse `Stream`.

**Option B — `Stream.resource/3` with `start_fun` opening the file, `next_fun` reading chunks, `after_fun` closing the handle** (chosen)
- Pros: cleanup on early halt (exceptions, `take/1`) is guaranteed by the runtime; composes with any `Stream`/`Enum` function; no process lifecycle to manage; the stream IS the abstraction.
- Cons: `next_fun` must handle the "no data yet" case explicitly (sleep + return `{[], state}`); poll interval is a trade-off between latency and CPU; not suitable for thousands of concurrent tailers (each holds an FD).

Chose **B** because the semantics we need — open once, yield as data arrives, close on any exit — are exactly what `Stream.resource/3` provides, with the runtime handling the cleanup contract for free.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### Step 1 — Create the project

**Objective**: Build single module so start/next/after callbacks are side-by-side and resource lifecycle is linear and proven.

```bash
mix new log_tailer
cd log_tailer
```

### Step 2 — `lib/log_tailer.ex`

**Objective**: Contrast stateless Stream.unfold with resource-backed Stream.resource so guaranteed cleanup vs pure lazy is clear.

```elixir
defmodule LogTailer do
  @moduledoc """
  Lazy helpers around Stream.unfold/2 and Stream.resource/3.
  """

  @doc """
  Infinite lazy Fibonacci sequence starting at 0, 1.

  Nothing is computed until a terminal operation consumes values.
  Pure function, no side effects — `Stream.unfold/2` is the right choice.
  """
  @spec fibonacci() :: Enumerable.t()
  def fibonacci do
    # unfold returns {value, next_state} to continue, or nil to halt.
    # Here we never return nil — the sequence is infinite by design.
    Stream.unfold({0, 1}, fn {a, b} -> {a, {b, a + b}} end)
  end

  @doc """
  Tails a file like `tail -f`. Emits lines (without trailing `\\n`) as they appear.

  * `path` — file to tail. Must exist when tailing starts.
  * `poll_ms` — how long to sleep between EOF polls. Default 100ms.

  Returns a lazy `Enumerable.t()`. The caller decides when to stop (`Enum.take/2`,
  `Stream.take_while/2`, timeout, etc.). On halt the file handle is closed.
  """
  @spec tail(Path.t(), pos_integer()) :: Enumerable.t()
  def tail(path, poll_ms \\ 100) do
    Stream.resource(
      # start_fun: open the file, return it as the initial accumulator.
      # :raw + :read_ahead is the fastest combination for sequential reads.
      fn -> File.open!(path, [:read, :raw, :read_ahead]) end,

      # next_fun: read one line. On EOF, sleep and retry.
      # We never return {:halt, _} ourselves — the consumer decides when to stop.
      fn file ->
        case IO.read(file, :line) do
          :eof ->
            Process.sleep(poll_ms)
            {[], file}

          {:error, reason} ->
            # Fail fast: surface the real error instead of an empty stream.
            raise File.Error, reason: reason, action: "read line from", path: path

          line when is_binary(line) ->
            {[String.trim_trailing(line, "\n")], file}
        end
      end,

      # after_fun: always runs — on normal completion, halt, or exception.
      # This is the whole reason we use Stream.resource over Stream.unfold here.
      fn file -> File.close(file) end
    )
  end
end
```

### Step 3 — `test/log_tailer_test.exs`

**Objective**: Force `Enum.take` on an infinite fib stream and an appended-to file to prove laziness and `after_fun` cleanup both fire.

```elixir
defmodule LogTailerTest do
  use ExUnit.Case, async: true

  describe "fibonacci/0" do
    test "produces the expected sequence lazily" do
      assert LogTailer.fibonacci() |> Enum.take(8) == [0, 1, 1, 2, 3, 5, 8, 13]
    end

    test "is truly infinite — Enum.take doesn't hang" do
      # If fibonacci/0 were eager, this would never return.
      assert LogTailer.fibonacci() |> Enum.take(1_000) |> length() == 1_000
    end
  end

  describe "tail/2" do
    setup do
      path = Path.join(System.tmp_dir!(), "log_tailer_#{System.unique_integer([:positive])}.log")
      File.write!(path, "line1\nline2\n")
      on_exit(fn -> File.rm(path) end)
      {:ok, path: path}
    end

    test "reads existing lines", %{path: path} do
      # take/2 halts the stream — after_fun must close the file.
      assert LogTailer.tail(path, 10) |> Enum.take(2) == ["line1", "line2"]
    end

    test "picks up lines appended after the stream started", %{path: path} do
      # Spawn a writer that appends a line 50ms after we start tailing.
      parent = self()

      spawn(fn ->
        Process.sleep(50)
        File.write!(path, "line3\n", [:append])
        send(parent, :written)
      end)

      lines = LogTailer.tail(path, 20) |> Enum.take(3)
      assert_receive :written, 1_000
      assert lines == ["line1", "line2", "line3"]
    end
  end
end
```

### Step 4 — Run the tests

**Objective**: Confirm the infinite-sequence test actually terminates, proving the terminal operation drives demand rather than the stream.

```bash
mix test
```

All 4 tests should pass.

### Why this works

`Stream.resource/3` gives three callbacks that map to the lifecycle of an external resource: `start_fun` opens the file exactly once and returns the state (the `File.io_device`), `next_fun` returns `{lines, new_state}` each time the consumer pulls — returning `{[], state}` on EOF after a short sleep to avoid busy-looping — and `after_fun` runs exactly once when the stream halts, whether from `Enum.take/1`, an exception in the consumer, or the file being deleted. Because the stream is lazy, the file is never opened until a terminal operation pulls; because `after_fun` is guaranteed, file descriptors never leak. `Stream.unfold/2` is the stateless cousin — same pull model, no cleanup needed.

---


## Key Concepts

### 1. Streams Are Lazy—Transformations Don't Execute Until Consumed
Each transformation is queued but not executed. Only when you call `Enum.to_list()` or `Enum.each()` do they execute.

### 2. Infinite Streams Are Possible
An infinite sequence that only produces needed elements. This is impossible with lists. Streams enable lazy computation.

### 3. Memory Efficiency at Scale
For a file with 1 billion lines, `Stream.map` processes one line at a time, not loading the entire file into memory.

---
## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    path = Path.join(System.tmp_dir!(), "bench_log_#{System.unique_integer([:positive])}.log")

    # Pre-populate with 100k lines
    File.write!(path, Enum.map_join(1..100_000, "\n", &"line-#{&1}") <> "\n")

    {us, lines} =
      :timer.tc(fn ->
        LogTailer.tail(path, 0)
        |> Enum.take(100_000)
      end)

    IO.puts("tail + take(100k): #{us} µs (#{us / 100_000} µs/line)")
    IO.puts("lines read: #{length(lines)}")

    File.rm!(path)
  end
end

Bench.run()
```

Target: under 2 µs per line in steady state (file already populated, no sleep needed). The interesting metric is latency from write-to-read when new lines arrive — measure by having one process write while another tails, then record the delta.

---

## Trade-offs

| Tool | State | Cleanup | Use case |
|------|-------|---------|----------|
| `Stream.unfold/2` | Pure accumulator | None needed | Math sequences, in-memory generators |
| `Stream.resource/3` | External handle | `after_fun` always runs | Files, sockets, db cursors, external APIs |
| `Stream.iterate/2` | Pure function of previous value | None | Simple "x, f(x), f(f(x))" chains |
| `Enum.*` eager | N/A | N/A | Finite data that fits in memory |

---

## Common production mistakes

**1. Using `Stream.unfold/2` for file tailing**  
No cleanup callback. The file handle leaks on `Enum.take/2` or on exception.
Under load (thousands of short-lived tailers) you hit `:emfile` — OS file descriptor limit.

**2. Busy-looping on EOF**  
Returning `{[], file}` without `Process.sleep/1` pegs one CPU core at 100%.
Always sleep a small amount (50–500ms depending on latency tolerance).

**3. Forgetting that Stream is lazy**  
`LogTailer.tail(path)` alone does nothing — it returns a recipe. You need `Enum.take/2`,
`Enum.to_list/1`, `Stream.run/1`, or a pipe into `Enum.*` to actually execute it.

**4. Reading with the default (non-`:raw`) file mode inside a hot loop**  
Goes through an extra process per read. For a single tailer it's imperceptible; at 1k
tailers it matters. `[:raw, :read_ahead]` bypasses the file-server process.

---

## When NOT to use `Stream.resource`

If the data fits in memory and you read it once, `File.read!/1` + `String.split/2` is
shorter and equally fast. Streams shine when data is unbounded, very large, or sourced
from a connection you must close.

If you need random access or need to rewind, you want a `File` handle directly, not a
lazy stream — streams are strictly forward-only.

---

## Reflection

1. Your tailer polls every 50 ms. For a monitoring tool consuming 1000 log files, that's 20k polls/sec — overkill when most files are idle. Would you switch to `:file_event` watchers (fs library), aggregate multiple files into one stream, or something else? How does `Stream.resource/3` compose (or not) with inotify-style event sources?
2. `after_fun` is guaranteed to run — unless the BEAM is killed by the OS (`kill -9`). What files, sockets, or external state would still leak in that case? Is it the stream's responsibility to protect against it, or does the supervisor tree handle it one level up?

---

## Resources

- [`Stream.resource/3` docs](https://hexdocs.pm/elixir/Stream.html#resource/3)
- [`Stream.unfold/2` docs](https://hexdocs.pm/elixir/Stream.html#unfold/2)
- [Elixir School — Streams](https://elixirschool.com/en/lessons/basics/enum#streams)
- Dave Thomas — *Programming Elixir*, chapter on lazy streams
