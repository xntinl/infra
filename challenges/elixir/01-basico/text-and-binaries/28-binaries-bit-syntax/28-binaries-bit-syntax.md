# Binaries and Bit Syntax: Building a PNG Header Parser

**Project**: `packet_parser` — a library that parses PNG file headers and a small custom TCP-style framed packet

**Difficulty**: ★★☆☆☆
**Estimated time**: 2-3 hours

---

## Core concepts in this exercise

1. **Bitstring syntax `<<>>`** — the primitive Elixir uses to construct and pattern match on raw bytes and bits.
2. **`size`, `unit`, and `binary` modifiers** — how to declare exact widths when matching headers, magic numbers, and variable-length payloads.

Everything else (file IO, testing) is scaffolding. Focus on reading the match patterns.

---

## Why this matters for a senior developer

Every non-trivial Elixir system eventually touches raw bytes:

- Parsing protocols (HTTP/2 frames, MQTT, Postgres wire protocol, WebSocket frames)
- Reading binary file formats (PNG, WAV, custom logs)
- Writing to `:gen_tcp` sockets that speak length-prefixed framing
- Interfacing with C libraries through NIFs or ports

Elixir's bit syntax is not a niche feature — it is the way this is done. Understanding
`<<magic::binary-size(8), width::unsigned-32, ...>>` is as fundamental as understanding
`case` or `with`. A senior developer never reaches for a third-party "byte parser"
library when the standard syntax is simpler and faster.

---

## Project structure

```
packet_parser/
├── lib/
│   └── packet_parser/
│       ├── png.ex
│       └── frame.ex
├── test/
│   └── packet_parser/
│       ├── png_test.exs
│       └── frame_test.exs
└── mix.exs
```

---

## The business problem

You're onboarding a log ingestion service. Upstream producers send two kinds of binary payloads:

1. **PNG screenshots** uploaded for debugging — you must reject anything whose first
   8 bytes are not the PNG magic signature, and extract image dimensions from the
   IHDR chunk.
2. **Length-prefixed frames** over TCP: a 4-byte big-endian length, 1-byte type code,
   then a payload of exactly `length - 1` bytes. You must split a byte stream into
   individual frames without losing partial data across reads.

Both problems are classic bit syntax exercises.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"jason", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: PNG headers are fixed layout (8 bytes); IHDR is a named chunk — binary matching validates struct before parsing.

```bash
mix new packet_parser
cd packet_parser
```

### Step 2: `mix.exs`

**Objective**: Boilerplate; focus on how module organization separates PNG header from frame buffer logic.

```elixir
defmodule PacketParser.MixProject do
  use Mix.Project

  def project do
    [
      app: :packet_parser,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end
end
```

No external dependencies — bit syntax is built into the language.

### Step 3: `lib/packet_parser/png.ex`

**Objective**: PNG width/height are 32-bit big-endian; failing to check bounds on IHDR before reading body loses all diagnostics.

```elixir
defmodule PacketParser.PNG do
  @moduledoc """
  Parses the fixed header and IHDR chunk of a PNG file.

  PNG layout (first 33 bytes):
    - 8 bytes: magic signature  <<137, 80, 78, 71, 13, 10, 26, 10>>
    - 4 bytes: IHDR length (always 13 for IHDR)
    - 4 bytes: chunk type "IHDR"
    - 4 bytes: width  (unsigned, big-endian)
    - 4 bytes: height (unsigned, big-endian)
    - 1 byte:  bit depth
    - 1 byte:  color type
    - ... (rest ignored)
  """

  # The PNG signature. Declared as a module attribute so it's inlined at compile
  # time into the match pattern — no runtime allocation for the comparison bytes.
  @magic <<137, 80, 78, 71, 13, 10, 26, 10>>

  @type info :: %{
          width: non_neg_integer(),
          height: non_neg_integer(),
          bit_depth: non_neg_integer(),
          color_type: non_neg_integer()
        }

  @doc """
  Parses the header of a PNG binary and returns its dimensions.

  Returns `{:ok, info}` for valid PNGs, `{:error, reason}` otherwise.
  """
  @spec parse(binary()) :: {:ok, info()} | {:error, atom()}
  def parse(
        # Why `binary-size(8)` for the magic:
        #   We match the fixed 8-byte signature by pinning it against the module
        #   attribute. The compiler turns this into a byte-for-byte equality check.
        <<@magic::binary-size(8),
          # 13 is the fixed IHDR payload length. Matching it literally rejects
          # corrupted files early without us having to validate later.
          13::unsigned-32,
          # "IHDR" is 4 ASCII bytes. `binary-size(4)` matches them positionally.
          "IHDR"::binary-size(4),
          # `unsigned-32` is shorthand for `unsigned-integer-size(32)-big`.
          # PNG is explicitly big-endian — we state it for clarity.
          width::unsigned-big-32,
          height::unsigned-big-32,
          bit_depth::unsigned-8,
          color_type::unsigned-8,
          _rest::binary>>
      ) do
    {:ok,
     %{
       width: width,
       height: height,
       bit_depth: bit_depth,
       color_type: color_type
     }}
  end

  # The magic bytes are present but the IHDR chunk is malformed or truncated.
  def parse(<<@magic::binary-size(8), _rest::binary>>), do: {:error, :malformed_ihdr}

  # Anything without the magic signature isn't a PNG at all.
  def parse(<<_::binary>>), do: {:error, :not_a_png}
end
```

**Why this works:**

- `@magic` is inlined into the match. The BEAM compares the first 8 bytes against a
  known literal — no extra allocation, no runtime parsing.
- The unit modifier defaults to `1` for `binary` (1 byte per unit) and `1` for
  `integer` (1 bit per unit). So `size(32)` on an integer means 32 bits (4 bytes)
  and `size(8)` on a binary means 8 bytes. This is a frequent source of confusion.
- Multiple `parse/1` clauses form a dispatch table. More specific patterns come
  first; the catch-all `<<_::binary>>` handles anything else without crashing.

### Step 4: `lib/packet_parser/frame.ex`

**Objective**: Length-prefixed streams allow clients to buffer variable payloads; split reads across multiple socket calls.

```elixir
defmodule PacketParser.Frame do
  @moduledoc """
  Decodes length-prefixed frames from a streaming byte buffer.

  Frame layout:
    - 4 bytes: total length (big-endian unsigned)
    - 1 byte:  type code
    - N bytes: payload, where N = length - 1

  `decode_stream/1` returns all complete frames plus the leftover buffer,
  so the caller can accumulate more bytes from a TCP socket.
  """

  @type frame :: %{type: byte(), payload: binary()}

  @doc """
  Extracts every complete frame from `buffer`.

  Returns `{frames, leftover}`. `leftover` is the unparsed tail — keep it and
  prepend it to the next read from the socket.
  """
  @spec decode_stream(binary()) :: {[frame()], binary()}
  def decode_stream(buffer), do: decode_stream(buffer, [])

  # Full frame available: length header + body fit inside `rest`.
  # We decrement length by 1 because the type byte is counted in `length`.
  defp decode_stream(
         <<length::unsigned-big-32, type::unsigned-8, rest::binary>> = buffer,
         acc
       )
       when byte_size(rest) >= length - 1 do
    payload_size = length - 1
    <<payload::binary-size(payload_size), tail::binary>> = rest
    frame = %{type: type, payload: payload}
    # Prepend + reverse at the end is O(n). Appending with `++` would be O(n^2).
    decode_stream(tail, [frame | acc])
    # `buffer` is bound but unused here — keeping the bind makes the guard readable.
    |> tap(fn _ -> buffer end)
  end

  # Not enough bytes for a complete frame yet — stop and return what we have.
  defp decode_stream(buffer, acc), do: {Enum.reverse(acc), buffer}

  @doc """
  Encodes a single frame. Useful for tests and for the write side of the stream.
  """
  @spec encode(byte(), binary()) :: binary()
  def encode(type, payload) when is_integer(type) and is_binary(payload) do
    # `byte_size(payload) + 1` because the length header counts the type byte
    # plus the payload bytes.
    length = byte_size(payload) + 1
    <<length::unsigned-big-32, type::unsigned-8, payload::binary>>
  end
end
```

**Why this works:**

- The guard `byte_size(rest) >= length - 1` is the streaming trick: we only consume
  a frame when the body is fully buffered. Short reads leave the unfinished header
  in `buffer` untouched.
- `<<payload::binary-size(payload_size), tail::binary>>` slices the binary without
  copying — the BEAM uses a sub-binary reference counting scheme. This is what makes
  bit syntax viable for high-throughput protocols.
- Recursion is tail-call optimized, so decoding a buffer with thousands of frames
  runs in constant stack space.

### Step 5: Tests — `test/packet_parser/png_test.exs`

**Objective**: Test magic bytes first (failing fast) and impossible dimensions (width=0); invalid PNG halts early.

```elixir
defmodule PacketParser.PNGTest do
  use ExUnit.Case, async: true

  alias PacketParser.PNG

  # Build a minimal valid PNG header plus the IHDR chunk. No real image data.
  defp png_header(width, height) do
    <<137, 80, 78, 71, 13, 10, 26, 10>> <>
      <<13::unsigned-32>> <>
      "IHDR" <>
      <<width::unsigned-big-32, height::unsigned-big-32>> <>
      <<8::unsigned-8, 6::unsigned-8>>
  end

  describe "parse/1" do
    test "extracts width and height from a valid header" do
      assert {:ok, %{width: 640, height: 480, bit_depth: 8, color_type: 6}} =
               PNG.parse(png_header(640, 480))
    end

    test "supports very large dimensions (4-byte unsigned range)" do
      # 2^31 - 1 is a realistic max; PNG spec actually allows up to 2^31 - 1.
      assert {:ok, %{width: 2_147_483_647}} =
               PNG.parse(png_header(2_147_483_647, 1))
    end

    test "rejects a truncated file with the correct magic" do
      assert {:error, :malformed_ihdr} =
               PNG.parse(<<137, 80, 78, 71, 13, 10, 26, 10, 0, 0>>)
    end

    test "rejects a file without the PNG signature" do
      assert {:error, :not_a_png} = PNG.parse("definitely not a png")
    end

    test "rejects an empty binary" do
      assert {:error, :not_a_png} = PNG.parse(<<>>)
    end
  end
end
```

### Step 6: Tests — `test/packet_parser/frame_test.exs`

**Objective**: Test incomplete frames, exact boundaries, and multi-frame buffers; frames are the real case in practice.

```elixir
defmodule PacketParser.FrameTest do
  use ExUnit.Case, async: true

  alias PacketParser.Frame

  describe "encode/2 and decode_stream/1 round-trip" do
    test "decodes a single complete frame" do
      buffer = Frame.encode(1, "hello")
      assert {[%{type: 1, payload: "hello"}], <<>>} = Frame.decode_stream(buffer)
    end

    test "decodes multiple frames concatenated back-to-back" do
      buffer = Frame.encode(1, "a") <> Frame.encode(2, "bb") <> Frame.encode(3, "ccc")

      assert {
               [
                 %{type: 1, payload: "a"},
                 %{type: 2, payload: "bb"},
                 %{type: 3, payload: "ccc"}
               ],
               <<>>
             } = Frame.decode_stream(buffer)
    end

    test "keeps a trailing partial frame as leftover" do
      complete = Frame.encode(1, "done")
      # Only the first 3 bytes of the next frame's length header — not enough to parse.
      partial = <<0, 0, 0>>
      assert {[%{type: 1, payload: "done"}], ^partial} =
               Frame.decode_stream(complete <> partial)
    end

    test "returns no frames when the buffer is smaller than the header" do
      assert {[], <<1, 2>>} = Frame.decode_stream(<<1, 2>>)
    end

    test "handles an empty payload (length = 1, type only)" do
      buffer = Frame.encode(9, <<>>)
      assert {[%{type: 9, payload: <<>>}], <<>>} = Frame.decode_stream(buffer)
    end
  end
end
```

### Step 7: Run and verify

**Objective**: --warnings-as-errors forces exhaustive matches on frame types; missing a case in production is catastrophic.

```bash
mix test --trace
mix compile --warnings-as-errors
```

All 10 tests must pass. If a test fails with `MatchError` inside `decode_stream`,
check the `size` modifier — mixing bits and bytes is the most common mistake here.

---


## Key Concepts

### 1. `::binary` Matches the Remainder
`::binary` is greedy—it takes everything remaining. Useful for splitting off a prefix and keeping the rest.

### 2. Type Specifiers Control Interpretation
The specifier changes how bytes are interpreted. For UTF-8 strings, use `::utf8`. For raw bytes, use `::integer` or no specifier.

### 3. Bit Syntax Compiles Efficiently
Pattern matching on binaries compiles to optimized bytecode. It's faster than manual slicing. Always prefer bit syntax for binary protocols.

---
## Trade-off analysis

| Aspect                    | Bit syntax (this approach) | Third-party parser library | Manual `:binary` module |
|---------------------------|----------------------------|----------------------------|-------------------------|
| Readability of format spec | Layout matches the RFC     | Hidden behind combinators  | Arithmetic on offsets   |
| Performance               | Compiler-optimized match   | Overhead of combinator DSL | Slice-by-slice copies   |
| Error messages            | `MatchError` on bad input  | Library-specific           | Manual                  |
| Streaming support         | Natural via recursion      | Library-dependent          | Manual buffer mgmt      |
| Learning curve            | Steep once, trivial after  | Easy start, hard to debug  | Tedious                 |

Prefer bit syntax every time the format is documented byte-by-byte. Reach for a
combinator library (`NimbleParsec`) only for text grammars with backtracking.

---

## Common production mistakes

**1. Confusing `size` on integers vs binaries**
`size(4)` on an integer means 4 bits. `size(4)` on a binary means 4 bytes. If you
see wildly wrong numbers, you are almost certainly off by a factor of 8.

**2. Forgetting `big` or `little`**
On BEAM, the default for `integer` is `big-endian`. Most network protocols are
big-endian so this is usually fine, but file formats (especially WAV, BMP) are
often little-endian. Always state it explicitly in production parsers.

**3. Unsigned vs signed**
`<<-1::signed-8>>` produces `<<255>>`. If you omit `unsigned`, you may match
negative integers for high bytes. Be explicit: `unsigned-8`, `signed-16`.

**4. Copying sub-binaries by accident**
`Kernel.binary_part/3` returns a copy. `<<x::binary-size(n), _rest::binary>> = bin`
returns a reference. For streaming thousands of frames, the difference is a memory
leak vs constant memory.

**5. Using `++` instead of prepend + reverse in recursive accumulators**
`acc ++ [item]` is O(n). `[item | acc]` + `Enum.reverse/1` at the end is O(n) total.
The tests above will still pass with the wrong version, but production throughput
will collapse.

---

## When NOT to use bit syntax

- Parsing text protocols with nested structure (JSON, XML) — use `Jason`, `Saxy`.
- Parsing text grammars with operator precedence — use `NimbleParsec`.
- Writing anything where the format changes per release — bit syntax is positional
  and brittle against format drift. Use a self-describing format (Protobuf, MessagePack).

---

## Resources

- [Elixir `Kernel.SpecialForms` — bitstring](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#%3C%3C%3E%3E/1) — the authoritative reference for every modifier
- [Erlang bit syntax tutorial](https://www.erlang.org/doc/programming_examples/bit_syntax.html) — deeper coverage of `unit`, `signed`, endian
- [PNG specification — chapter 5](https://www.w3.org/TR/PNG/#5DataRep) — the bytes you're matching
- [Learn You Some Erlang — Starting Out (for real)](https://learnyousomeerlang.com/starting-out-for-real#bit-syntax) — classic walkthrough with diagrams
