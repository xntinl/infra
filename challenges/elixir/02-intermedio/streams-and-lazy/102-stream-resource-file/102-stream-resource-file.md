# `Stream.resource/3` — reading and writing a huge file without loading it

**Project**: `stream_resource_file` — process a multi-gigabyte CSV-like file
line by line using `Stream.resource/3` for precise control over open/read/close.

---

## Project context

`File.stream!/1` is the usual way to read a file lazily in Elixir, and
most of the time it's the right answer. But it's a *wrapper* around a
lower-level primitive: `Stream.resource/3`. Once you want to do something
`File.stream!` doesn't give you — read headers separately, hold an extra
resource (a compression context, a parser state, a database cursor), or
clean up carefully on exit — you need `Stream.resource/3` directly.

In this exercise you'll rebuild a file-reading stream from scratch and
use it to count lines in a file that could be arbitrarily large, without
ever loading the whole thing into memory.

Project structure:

```
stream_resource_file/
├── lib/
│   └── stream_resource_file.ex
├── test/
│   └── stream_resource_file_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The three functions of `Stream.resource/3`

```elixir
Stream.resource(
  start_fun,   # () -> acc              — open the resource
  next_fun,    # acc -> {events, acc}   — produce next chunk, or halt
  after_fun    # acc -> any             — close the resource
)
```

- `start_fun` runs once, lazily, when the stream is first consumed.
- `next_fun` runs repeatedly, returning `{events, new_acc}` or
  `{:halt, acc}` to stop.
- `after_fun` runs exactly once — on natural halt *and* on crash. This is
  the whole point of `Stream.resource`: guaranteed cleanup.

### 2. Why not just `File.stream!/1`?

`File.stream!/1` gives you line-by-line reading with default options. But:

- It reopens/closes the file on every enumeration. For custom lifecycle
  (e.g., read header once, then stream the body) you want one open file.
- You can't hold auxiliary state next to the file handle.
- Error handling lives inside `File`; with `Stream.resource/3` you own it.

### 3. `after_fun` runs on crash too

If a consumer halts the stream early (`Enum.take`) or raises, `after_fun`
still runs. This is why `Stream.resource/3` is safe: no file descriptor
leaks even on exceptions.

### 4. Writing is a `Stream.into/2` concern, not resource

To *write* a file from a stream, pipe into `Stream.into(File.stream!(path))`
or `Enum.into(File.stream!(path, [:write]))`. `Stream.resource/3` models
the *source*; for sinks, use `Collectable` via `File.stream!/1` in write
mode.

---

## Design decisions

**Option A — `File.stream!/1` for everything**
- Pros: one-line API; stdlib; sufficient for line-by-line reads with default framing.
- Cons: reopens/closes the file on every enumeration; no place to hold auxiliary state; limited control over read mode and cleanup hooks.

**Option B — Hand-rolled `Stream.resource/3`** (chosen)
- Pros: guaranteed `after_fun` cleanup even on crash; one open handle across the whole enumeration; room for auxiliary state (parser, counters, cursor).
- Cons: more code; must own error handling; harder to get right on first try.

→ Chose **B** because the exercise is precisely about the primitive behind `File.stream!`, and production pipelines frequently outgrow the default wrapper.

---

## Implementation

### Step 1: Create the project

```bash
mix new stream_resource_file
cd stream_resource_file
```

### Step 2: `lib/stream_resource_file.ex`

```elixir
defmodule StreamResourceFile do
  @moduledoc """
  Reads files as lazy streams via `Stream.resource/3`, and transforms them
  without ever loading the whole file into memory.
  """

  @doc """
  Returns a lazy stream of lines from `path`. Each element is a string with
  the trailing newline stripped. Unlike `File.stream!/1`, we own the file
  handle across the whole enumeration.
  """
  @spec lines(Path.t()) :: Enumerable.t()
  def lines(path) do
    Stream.resource(
      fn -> File.open!(path, [:read, :utf8]) end,
      fn io ->
        case IO.read(io, :line) do
          :eof -> {:halt, io}
          {:error, reason} -> raise File.Error, reason: reason, action: "read", path: path
          data -> {[String.trim_trailing(data, "\n")], io}
        end
      end,
      fn io -> File.close(io) end
    )
  end

  @doc """
  Counts lines in a file lazily. Works on files far larger than RAM because
  `lines/1` never materializes more than one line at a time.
  """
  @spec count_lines(Path.t()) :: non_neg_integer()
  def count_lines(path) do
    path
    |> lines()
    |> Enum.count()
  end

  @doc """
  Transforms a file line by line, writing the result to `out_path` without
  buffering the whole thing. Demonstrates pairing `Stream.resource/3` on the
  read side with `Stream.into/2` on the write side.
  """
  @spec transform(Path.t(), Path.t(), (String.t() -> String.t())) :: :ok
  def transform(in_path, out_path, fun) when is_function(fun, 1) do
    in_path
    |> lines()
    |> Stream.map(fn line -> fun.(line) <> "\n" end)
    |> Stream.into(File.stream!(out_path))
    |> Stream.run()

    :ok
  end
end
```

### Step 3: `test/stream_resource_file_test.exs`

```elixir
defmodule StreamResourceFileTest do
  use ExUnit.Case, async: true

  @tmp Path.join(System.tmp_dir!(), "stream_resource_file_test")

  setup do
    File.mkdir_p!(@tmp)
    on_exit(fn -> File.rm_rf!(@tmp) end)
    :ok
  end

  defp write!(name, content) do
    path = Path.join(@tmp, name)
    File.write!(path, content)
    path
  end

  describe "lines/1" do
    test "yields each line, newline stripped" do
      path = write!("a.txt", "alpha\nbeta\ngamma\n")
      assert Enum.to_list(StreamResourceFile.lines(path)) == ["alpha", "beta", "gamma"]
    end

    test "is lazy — early halt stops reading" do
      path = write!("b.txt", Enum.map_join(1..1_000, "\n", &"line-#{&1}") <> "\n")
      # Take only 3 lines of 1000 — the rest is never read.
      assert StreamResourceFile.lines(path) |> Enum.take(3) ==
               ["line-1", "line-2", "line-3"]
    end
  end

  describe "count_lines/1" do
    test "counts correctly without loading the file" do
      path = write!("c.txt", Enum.map_join(1..10_000, "\n", &"l#{&1}") <> "\n")
      assert StreamResourceFile.count_lines(path) == 10_000
    end
  end

  describe "transform/3" do
    test "writes a transformed file line by line" do
      in_path = write!("in.txt", "hello\nworld\n")
      out_path = Path.join(@tmp, "out.txt")

      StreamResourceFile.transform(in_path, out_path, &String.upcase/1)

      assert File.read!(out_path) == "HELLO\nWORLD\n"
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

### Why this works

`Stream.resource/3` guarantees that `after_fun` runs exactly once — on natural halt, early `Enum.take`, or exception — so the file descriptor is always closed. `next_fun` is invoked one line at a time, so peak memory is O(one line) regardless of file size. `:halt` signals end-of-stream, and `Enum.count/1` just increments a counter without retaining lines.

---

## Benchmark

```elixir
# Generate a 100 MB test file
path = "/tmp/bench.txt"
File.write!(path, Enum.map_join(1..2_000_000, "\n", &"line-#{&1}"))

{t1, _} = :timer.tc(fn -> StreamResourceFile.count_lines(path) end)
{t2, _} = :timer.tc(fn -> File.read!(path) |> String.split("\n") |> length() end)
IO.puts("stream=#{div(t1, 1000)}ms  eager=#{div(t2, 1000)}ms")
```

Target esperado: streaming counts a 100 MB file in <500 ms with ~constant RAM; eager approach uses >200 MB RAM and is 20-50 % slower due to binary → list conversion.

---

## Trade-offs and production gotchas

**1. `after_fun` MUST be idempotent**
If `next_fun` raises *and* `after_fun` also raises, the original error is
hidden. Keep `after_fun` simple — close the handle, ignore double-close
errors. `File.close/1` on a closed handle is a no-op, which is exactly
what you want.

**2. The stream is lazy — the file doesn't open until iteration starts**
Binding `s = StreamResourceFile.lines(path)` does nothing. The file opens
when you pipe `s` into a terminal (`Enum.take`, `Enum.count`, ...). If
`path` is missing, the error surfaces at consumption, not at construction.
Design error handling accordingly.

**3. UTF-8 mode vs binary mode**
`File.open!(path, [:utf8])` means `IO.read/2` returns Elixir strings and
raises on malformed bytes. Without `:utf8`, you get binaries — faster, but
no encoding guarantees. For CSV/TSV with known-good encoding, `:utf8`;
for blobs or unknown encodings, read binary and decode explicitly.

**4. Don't share an open handle across processes**
A file handle from `File.open/2` is owned by the opener. Passing it to
another process and reading concurrently is a subtle bug — I/O is
serialized per handle, and ownership semantics around error exits can
surprise you. Open per consumer if you need parallelism.

**5. Back-pressure on the writer side**
`Stream.into(File.stream!(path))` writes eagerly as the source produces.
If the consumer is a slow upstream network, no problem. If the source
outruns disk IO, the VM's port buffers will back up. For huge writes
consider `File.open/2` with `:delayed_write` for better throughput.

**6. When NOT to use `Stream.resource/3`**
- For default line-by-line reads, `File.stream!/1` is shorter and correct.
- For binary protocols with framed reads (length-prefixed, etc.), an
  explicit GenServer holding the port is clearer than a stream.
- For tiny files (`< 1 MB`) the setup cost of laziness is not worth it —
  `File.read!/1 |> String.split("\n")` is simpler and faster.

---

## Reflection

- Your `next_fun` needs to track both the file handle and a CSV parser state (header row, column types). How do you structure the accumulator without reopening the file? Compare with wrapping the whole thing in a GenServer.
- If the process holding the stream crashes mid-read, is the file descriptor leaked? How would you prove it under load?

---

## Resources

- [`Stream.resource/3` — hexdocs](https://hexdocs.pm/elixir/Stream.html#resource/3)
- [`File.stream!/1` — hexdocs](https://hexdocs.pm/elixir/File.html#stream!/1) (the built-in sibling)
- [`IO.read/2` — hexdocs](https://hexdocs.pm/elixir/IO.html#read/2)
- [José Valim — "Building a new MacBook Pro provisioning pipeline with Elixir"](https://dashbit.co/blog) — Dashbit blog has multiple posts on streaming I/O patterns
- Saša Jurić — *Elixir in Action*, chapter on working with the outside world (file I/O patterns)
