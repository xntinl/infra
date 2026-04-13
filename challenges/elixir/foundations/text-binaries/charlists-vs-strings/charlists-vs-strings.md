# Charlists vs Strings: Building a Binary Protocol Parser

**Project**: `proto_parser` — parses a simple binary message format (fixed header + typed payload)

**Difficulty**: ★★☆☆☆
**Estimated time**: 2 hours

---

## Why this distinction matters for a senior developer

In Elixir there are TWO text types with incompatible representations:

- `"hello"` — a **binary** (UTF-8 encoded bytes). This is what you call "string".
- `~c"hello"` or `[?h, ?e, ?l, ?l, ?o]` — a **charlist** (a list of integer
  codepoints). This is the Erlang native string type.

They print differently, compare unequally, and are accepted by different libraries:

```
"hello" == ~c"hello"    # false
is_binary("hello")      # true
is_list(~c"hello")      # true
```

You will encounter charlists every time you call into Erlang's standard library
(`:file`, `:inet`, `:crypto`, `:ssh`). The Erlang/OTP world predates Unicode binaries
by 20 years and still uses lists of codepoints as the default text type.

Understanding the distinction matters when you:

- Call Erlang libraries that return `{:error, 'enoent'}` (note the single quotes)
- Read binary protocol formats byte by byte
- Interop with C ports via `:erlang.port_command/2`
- Parse fixed-width network packets (TCP, UDP, custom over sockets)

### The `?a` codepoint syntax

`?a` is the integer codepoint of the character `a` — exactly `97`. It is a
compile-time constant and the idiomatic way to write "the byte value of this
character" when you are pattern matching binaries. Far more readable than `97`.

```
?A == 65
?0 == 48
?\n == 10
?\s == 32      # space
```

---

## The business problem

An IoT device vendor ships sensors that emit a proprietary binary message every
second over TCP. You need a parser that extracts structured data from the raw
bytes. Message layout:

```
 offset  size  field          notes
 ------  ----  -------------  ----------------------------------------------
 0       1     magic          must be 0xAC or message is rejected
 1       1     version        only version 1 is supported
 2       2     device_id      big-endian unsigned 16
 4       1     type           1=temperature, 2=humidity, 3=pressure
 5       4     timestamp      big-endian unsigned 32 (unix seconds)
 9       2     payload_len    big-endian unsigned 16 (bytes)
 11      N     payload        ASCII string of `payload_len` bytes
```

Your parser must:

1. Accept a binary and return a structured map or an error atom
2. Reject malformed headers (wrong magic, unsupported version)
3. Convert the ASCII payload (a charlist-friendly format) into a proper Elixir string
4. Handle trailing bytes gracefully — additional data after the declared
   `payload_len` means the caller passed a multi-message buffer

---

## Project structure

```
proto_parser/
├── lib/
│   └── proto_parser/
│       ├── message.ex
│       └── decoder.ex
├── script/
│   └── main.exs
├── test/
│   └── proto_parser/
│       └── decoder_test.exs
└── mix.exs
```

---

## Core concepts applied here

### Binary pattern matching

Elixir's `<<>>` syntax is the fastest way to parse a binary protocol. Each segment
declares a size and type:

```elixir
<<magic::8, version::8, device_id::big-unsigned-integer-size(16), rest::binary>>
```

- `::8` — 8 bits (1 byte)
- `::big-unsigned-integer-size(16)` — 16-bit big-endian unsigned integer
- `::binary` — the remaining bytes as a binary
- `::binary-size(n)` — exactly `n` bytes as a binary

The match either binds all segments or fails. Zero-copy: no intermediate slices.

### `?a` in binary matches

You can match a literal byte by codepoint:

```elixir
<<?H, ?T, ?T, ?P, rest::binary>> = request
```

This is readable and type-safe — `?H` is always the byte 72.

### Charlist ↔ binary conversion

| From          | To            | Function              |
|---------------|---------------|-----------------------|
| binary        | charlist      | `String.to_charlist/1`|
| charlist      | binary        | `List.to_string/1`    |
| integer       | byte (in bin) | `<<n::8>>`            |

When an Erlang function returns `'enoent'` (a charlist), convert with
`List.to_string/1` before comparing to `"enoent"` or your match will silently fail.

---

## Design decisions

The implementation below was chosen for clarity and idiomatic Elixir style. Pattern matching, immutability, and small focused functions guide every clause. Trade-offs are discussed inline within each module's `@moduledoc`.

---

## Implementation

### Step 1: Create the project

**Objective**: Charlists ('string') ≠ binaries ("string") — Erlang libraries return charlist errors; compare against atoms.

```bash
mix new proto_parser
cd proto_parser
```

### `mix.exs`
**Objective**: Boilerplate; focus on how tests depend on module structure — module aliases must match namespaces.

```elixir
defmodule ProtoParser.MixProject do
  use Mix.Project

  def project do
    [
      app: :proto_parser,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    []
  end

end
```

### `lib/proto_parser.ex`

```elixir
defmodule ProtoParser do
  @moduledoc """
  Charlists vs Strings: Building a Binary Protocol Parser.

  In Elixir there are TWO text types with incompatible representations:.
  """
end
```

### `lib/proto_parser/message.ex`

**Objective**: Structs with @enforce_keys prevent partial construction; payload: String.t() forces downstream code to not guess charlist.

```elixir
defmodule ProtoParser.Message do
  @moduledoc """
  A decoded sensor message. Keeps fields typed — payload is always a
  String.t(), never a charlist, so downstream code never has to guess.
  """

  @enforce_keys [:device_id, :type, :timestamp, :payload]
  defstruct [:device_id, :type, :timestamp, :payload]

  @type sensor_type :: :temperature | :humidity | :pressure

  @type t :: %__MODULE__{
          device_id: non_neg_integer(),
          type: sensor_type(),
          timestamp: non_neg_integer(),
          payload: String.t()
        }
end
```

### `lib/proto_parser/decoder.ex`

**Objective**: Binary pattern matching <<::big-unsigned-integer-size(16)>> compiles to native byte ops; zero-copy, O(1) parsing.

```elixir
defmodule ProtoParser.Decoder do
  @moduledoc """
  Decodes a binary message into a %ProtoParser.Message{} struct.
  """

  alias ProtoParser.Message

  @magic 0xAC
  @supported_version 1

  @doc """
  Decodes the leading message from `binary`. Returns the decoded message
  along with any trailing bytes from the buffer (for stream decoding).
  """
  @spec decode(binary()) ::
          {:ok, Message.t(), binary()}
          | {:error, :bad_magic | :unsupported_version | :truncated | :unknown_type}
  def decode(binary) when is_binary(binary) do
    with {:ok, header, rest} <- decode_header(binary),
         {:ok, payload, trailing} <- decode_payload(rest, header.payload_len),
         {:ok, type_atom} <- type_to_atom(header.type) do
      message = %Message{
        device_id: header.device_id,
        type: type_atom,
        timestamp: header.timestamp,
        # Payload arrives as an ASCII binary already; we normalize to a
        # proper String.t() and strip any trailing NUL padding the vendor
        # may have added to reach a fixed frame size.
        payload: payload |> String.trim_trailing(<<0>>)
      }

      {:ok, message, trailing}
    end
  end

  # Match all 11 header bytes in a single pattern. Each ::N declares bit width.
  # big-unsigned is the default for integer/size(16+) but we write it explicitly
  # because wire protocols deserve zero ambiguity.
  defp decode_header(<<
         @magic,
         @supported_version,
         device_id::big-unsigned-integer-size(16),
         type::8,
         timestamp::big-unsigned-integer-size(32),
         payload_len::big-unsigned-integer-size(16),
         rest::binary
       >>) do
    {:ok,
     %{
       device_id: device_id,
       type: type,
       timestamp: timestamp,
       payload_len: payload_len
     }, rest}
  end

  # Wrong magic byte: distinguish from truncation for better diagnostics.
  defp decode_header(<<byte, _rest::binary>>) when byte != @magic,
    do: {:error, :bad_magic}

  # Matches when header starts with correct magic but wrong version.
  defp decode_header(<<@magic, v, _rest::binary>>) when v != @supported_version,
    do: {:error, :unsupported_version}

  defp decode_header(_short), do: {:error, :truncated}

  defp decode_payload(binary, payload_len) when byte_size(binary) >= payload_len do
    <<payload::binary-size(payload_len), trailing::binary>> = binary
    {:ok, payload, trailing}
  end

  defp decode_payload(_binary, _payload_len), do: {:error, :truncated}

  defp type_to_atom(1), do: {:ok, :temperature}
  defp type_to_atom(2), do: {:ok, :humidity}
  defp type_to_atom(3), do: {:ok, :pressure}
  defp type_to_atom(_), do: {:error, :unknown_type}

  @doc """
  Encoder used by tests and upstream producers. Symmetric to decode/1.
  """
  @spec encode(Message.t()) :: binary()
  def encode(%Message{} = msg) do
    type_byte =
      case msg.type do
        :temperature -> 1
        :humidity -> 2
        :pressure -> 3
      end

    payload_bin = msg.payload
    payload_len = byte_size(payload_bin)

    <<
      @magic,
      @supported_version,
      msg.device_id::big-unsigned-integer-size(16),
      type_byte::8,
      msg.timestamp::big-unsigned-integer-size(32),
      payload_len::big-unsigned-integer-size(16),
      payload_bin::binary
    >>
  end
end
```

### `test/proto_parser_test.exs`

**Objective**: Test truncation vs corruption as distinct errors; encode/decode round-trip validates symmetry and payload padding.

```elixir
defmodule ProtoParser.DecoderTest do
  use ExUnit.Case, async: true
  doctest ProtoParser.Decoder

  alias ProtoParser.{Decoder, Message}

  describe "decode/1 — happy path" do
    test "decodes a well-formed temperature message" do
      payload = "23.4C"

      binary = <<
        0xAC,
        1,
        0x00, 0x2A,
        1,
        0x65, 0x8B, 0x70, 0x00,
        0x00, byte_size(payload),
        payload::binary
      >>

      assert {:ok, msg, ""} = Decoder.decode(binary)
      assert msg.device_id == 42
      assert msg.type == :temperature
      assert msg.timestamp == 0x658B_7000
      assert msg.payload == "23.4C"
    end

    test "round-trips through encode/decode" do
      original = %Message{
        device_id: 1024,
        type: :humidity,
        timestamp: 1_700_000_000,
        payload: "55%"
      }

      encoded = Decoder.encode(original)
      assert {:ok, decoded, ""} = Decoder.decode(encoded)
      assert decoded == original
    end

    test "returns trailing bytes when buffer contains more than one message" do
      msg1 = Decoder.encode(%Message{device_id: 1, type: :pressure, timestamp: 1, payload: "A"})
      msg2 = Decoder.encode(%Message{device_id: 2, type: :pressure, timestamp: 2, payload: "B"})

      buffer = msg1 <> msg2

      assert {:ok, decoded1, rest} = Decoder.decode(buffer)
      assert decoded1.device_id == 1
      assert rest == msg2

      assert {:ok, decoded2, ""} = Decoder.decode(rest)
      assert decoded2.device_id == 2
    end

    test "strips trailing NUL padding from payload" do
      padded = "OK" <> <<0, 0, 0>>

      binary =
        Decoder.encode(%Message{
          device_id: 7,
          type: :temperature,
          timestamp: 0,
          payload: padded
        })

      assert {:ok, msg, ""} = Decoder.decode(binary)
      assert msg.payload == "OK"
    end
  end

  describe "decode/1 — error cases" do
    test "rejects wrong magic byte" do
      assert {:error, :bad_magic} = Decoder.decode(<<0x00, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0>>)
    end

    test "rejects unsupported version" do
      assert {:error, :unsupported_version} =
               Decoder.decode(<<0xAC, 99, 0, 0, 0, 0, 0, 0, 0, 0, 0>>)
    end

    test "reports truncated header" do
      assert {:error, :truncated} = Decoder.decode(<<0xAC, 1, 0, 1>>)
    end

    test "reports truncated payload" do
      # header says payload_len = 10, but only 3 bytes follow
      binary = <<0xAC, 1, 0, 1, 1, 0, 0, 0, 0, 0, 10, "abc">>
      assert {:error, :truncated} = Decoder.decode(binary)
    end

    test "rejects unknown type byte" do
      binary = <<0xAC, 1, 0, 1, 99, 0, 0, 0, 0, 0, 0>>
      assert {:error, :unknown_type} = Decoder.decode(binary)
    end
  end

  describe "charlist vs string interop" do
    test "?A codepoint equals 65 — readable byte literals in binary matches" do
      assert ?A == 65
      assert ?\n == 10
    end

    test "charlist and string with same content are NOT equal" do
      # This is the trap every Elixir newcomer falls into.
      refute ~c"hello" == "hello"
      assert is_list(~c"hello")
      assert is_binary("hello")
    end

    test "converting Erlang-style charlist payload to String.t()" do
      # Simulates receiving a payload from an Erlang library call.
      erlang_style = ~c"enoent"
      assert List.to_string(erlang_style) == "enoent"
    end
  end
end
```

### Step 6: Run and verify

**Objective**: --warnings-as-errors catches unused imports and variable shadowing; test failures surface at build time, not in production.

```bash
mix compile --warnings-as-errors
mix test --trace
```

---

---

Create a simple example demonstrating the key concepts:

```elixir
# Example code demonstrating text and binary concepts
IO.puts("Example: Read the Implementation section above and run the code samples in iex")
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== ProtoParser: demo ===\n")

    msg = %ProtoParser.Message{
      device_id: 42,
      type: :temperature,
      timestamp: 0x658B_7000,
      payload: "23.4C"
    }

    encoded = ProtoParser.Decoder.encode(msg)
    IO.puts("Demo 1 - encoded bytes: #{inspect(encoded, limit: :infinity)}")

    {:ok, decoded, _rest} = ProtoParser.Decoder.decode(encoded)
    IO.puts("Demo 2 - decoded round-trip: #{inspect(decoded)}")

    # Charlist vs binary string
    charlist = ~c"hello"
    binary = "hello"
    IO.puts("Demo 3 - charlist: #{inspect(charlist)} | binary: #{inspect(binary)}")

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

## Trade-off analysis

| Aspect                 | Binary pattern match (this) | :binary.part/3 + manual offsets | External parser (NimbleParsec) |
|------------------------|------------------------------|----------------------------------|--------------------------------|
| Lines of code          | ~15 per message              | ~30                              | ~20 DSL + learning curve       |
| Performance            | native-speed, zero copy      | extra slice allocations          | native-speed                   |
| Readability            | high (declarative)           | low (index arithmetic)           | high (for complex grammars)    |
| Endianness safety      | explicit in match            | easy to get wrong                | declared once                  |
| Fit                    | fixed wire formats           | ad-hoc extraction                | complex / recursive grammars   |

For simple fixed-layout protocols, binary pattern matching is the best tool in any
language, not just Elixir. Reach for NimbleParsec only when the grammar has
recursion or lookahead.

---

## Common production mistakes

**1. Comparing a charlist to a string**
```
File.read("/nope") == {:error, "enoent"}   # ALWAYS false
File.read("/nope") == {:error, :enoent}    # correct — :file returns atoms in Elixir wrappers
```
When calling raw Erlang (`:file.read_file/1`), you often get charlist errors.
Convert first with `List.to_string/1` or match against `~c"enoent"`.

**2. Forgetting endianness**
Network protocols are big-endian by default ("network byte order"). Your CPU is
likely little-endian. Always declare `::big` or `::little` explicitly when
parsing wire formats. The default in Elixir's `<<>>` IS big for integers, but
being explicit makes the code self-documenting.

**3. Using `byte_size/1` vs `String.length/1`**
`byte_size("á") == 2` but `String.length("á") == 1`. For binary protocols, you
almost always want `byte_size/1` — that is what the `payload_len` field measures.

**4. Mixing `<<?A>>` and `"A"` expecting equality by form**
They are equal in value (`<<?A>> == "A"` is `true`, both are the binary `<<65>>`),
but `<<?A>>` expresses intent: "a byte literal". Use it in matchers, use string
syntax in regular code.

**5. Not distinguishing truncation from corruption**
If your protocol runs over TCP, "truncated" means "read more from the socket".
"Bad magic" means "discard and resync". Returning a single `:error` loses that
distinction. Always tag error atoms precisely.

---

## When NOT to use binary pattern matching

- The format is self-describing (JSON, MessagePack, Protobuf) — use the
  respective library. Reinventing Protobuf's variable-length integers is a
  career-limiting move.
- The grammar is recursive or context-sensitive. Use NimbleParsec or a real
  parser generator.
- Byte order and alignment vary dynamically (some nightmare legacy protocols).
  Use a lookup table of segment descriptors instead of hard-coded matches.

---

## Resources

- [Binaries, strings, and charlists — HexDocs](https://hexdocs.pm/elixir/binaries-strings-and-charlists.html)
- [Erlang `:file` module](https://www.erlang.org/doc/man/file.html) — canonical example of charlist-returning API
- [`:binary` Erlang module](https://www.erlang.org/doc/man/binary.html) — lower-level binary operations
- [Joe Armstrong on bit syntax (2007 talk)](https://www.youtube.com/results?search_query=joe+armstrong+bit+syntax) — origin of Erlang's binary pattern matching

---

## Key concepts
### 1. Charlists Are Lists of Character Codes

```elixir
iex> 'hello'
[104, 101, 108, 108, 111]

iex> "hello"
"hello"
```

Single quotes create charlists (lists of integers). Double quotes create strings (UTF-8 binaries). They are not interchangeable. Erlang functions sometimes expect charlists; modern Elixir prefers strings.

### 2. Pattern Matching Differs

```elixir
[h | t] = 'hello'  # h = 104, t = [101, 108, 108, 111]
[h | t] = "hello"  # ERROR: strings don't support head/tail pattern matching directly
```

Charlists match as lists; strings don't. This is a common gotcha when interfacing with Erlang libraries.

### 3. Use Strings, Not Charlists

Modern Elixir uses strings (binaries) everywhere. Charlists remain for Erlang interop. Unless you're calling Erlang functions that expect charlists, stick with strings.

---
