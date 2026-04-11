# Bitstrings and Advanced Binaries

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` needs to communicate over a custom binary protocol with external worker agents — lightweight processes that cannot run the full Erlang VM. The protocol must be compact, deterministic, and parseable without a schema library.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       ├── registry.ex
│       └── protocol.ex         # ← you implement this
├── test/
│   └── task_queue/
│       └── protocol_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The external agents communicate over TCP using a fixed-format binary frame:

```
| magic (4 bytes: "TSKQ") | version (1 byte) | type (1 byte) | length (2 bytes) | payload |
```

- `magic` — used to detect frame boundaries and reject misaligned data
- `version` — protocol version, currently `1`
- `type` — `0x01` = job_request, `0x02` = job_result, `0x03` = heartbeat, `0xFF` = error
- `length` — byte size of the payload, big-endian 16-bit
- `payload` — JSON-encoded job data, up to 65535 bytes

The key insight: Elixir's `<<>>` syntax lets you build and parse this format with the same pattern syntax used for regular data structures.

---

## Why `<<>>` instead of string manipulation

String concatenation for binary protocols is error-prone: byte order, alignment, and field sizes are invisible. `<<>>` makes them explicit:

```elixir
# String concatenation — opaque, order-dependent
<<0xDE, 0xAD>> <> <<1>> <> <<byte_size(payload)::16>> <> payload

# Binary syntax — field names, sizes, and endianness are all visible
<<0xDE, 0xAD, version::8, byte_size(payload)::big-16, payload::binary>>
```

Pattern matching on the same syntax makes encode/decode symmetric — the parser is literally the inverse of the encoder.

---

## Why `::utf8` for strings

Strings in Elixir are UTF-8 binaries. A character like `é` occupies 2 bytes (`<<195, 169>>`). Using `::8` for character-by-character iteration treats multibyte characters as two separate bytes, producing corrupt output. The `::utf8` specifier extracts complete codepoints:

```elixir
<<head::utf8, rest::binary>> = "café"
# head => 99 ('c'), rest => "afé"  — correct

<<head::8, rest::binary>> = "café"
# head => 99 ('c'), rest => "afé"  — happens to work for ASCII prefix
# But for "élan":
<<head::8, rest::binary>> = "élan"
# head => 195 (first byte of 'é'), rest => <<169, 108, 97, 110>>  — wrong
```

---

## Implementation

### Step 1: `lib/task_queue/protocol.ex` — binary frame encoder/decoder

```elixir
defmodule TaskQueue.Protocol do
  @moduledoc """
  Binary framing protocol for communication between `task_queue` and external agents.

  Frame format:
      | magic (4 bytes) | version (1 byte) | type (1 byte) | length (2 bytes) | payload |

  - magic: "TSKQ" (4 ASCII bytes)
  - version: protocol version, currently 1
  - type: 0x01=job_request, 0x02=job_result, 0x03=heartbeat, 0xFF=error
  - length: big-endian uint16, payload byte count
  - payload: UTF-8 binary (JSON-encoded job data)
  """

  @magic "TSKQ"
  @version 1

  @type_job_request  0x01
  @type_job_result   0x02
  @type_heartbeat    0x03
  @type_error        0xFF

  @doc """
  Encodes a message into a binary frame.

  ## Examples

      iex> frame = TaskQueue.Protocol.encode(:job_request, "ping")
      iex> is_binary(frame)
      true
      iex> byte_size(frame)
      12

  """
  @spec encode(atom(), binary()) :: binary()
  def encode(type, payload) when is_binary(payload) do
    type_byte = type_to_byte(type)
    length = byte_size(payload)
    # TODO: construct the frame using <<>>
    # HINT: <<@magic::binary, @version::8, type_byte::8, length::big-16, payload::binary>>
  end

  @doc """
  Decodes a binary frame into `{:ok, %{type: atom, payload: binary}}` or
  `{:error, :invalid_frame}`.

  ## Examples

      iex> frame = TaskQueue.Protocol.encode(:heartbeat, "")
      iex> TaskQueue.Protocol.decode(frame)
      {:ok, %{type: :heartbeat, payload: ""}}

      iex> TaskQueue.Protocol.decode(<<0, 1, 2, 3>>)
      {:error, :invalid_frame}

  """
  @spec decode(binary()) :: {:ok, map()} | {:error, :invalid_frame}
  def decode(frame) do
    case frame do
      <<"TSKQ", _version::8, type_byte::8, length::big-16, payload::binary-size(length)>> ->
        # TODO: convert type_byte to atom with byte_to_type/1
        # TODO: return {:ok, %{type: type_atom, payload: payload}}
        :not_implemented
      _ ->
        {:error, :invalid_frame}
    end
  end

  @doc """
  Returns the number of UTF-8 codepoints in a binary string.

  This is different from `byte_size/1` for strings containing multibyte characters.

  ## Examples

      iex> TaskQueue.Protocol.codepoint_count("café")
      4

      iex> TaskQueue.Protocol.codepoint_count("hello")
      5

  """
  @spec codepoint_count(binary()) :: non_neg_integer()
  def codepoint_count(<<>>), do: 0

  def codepoint_count(binary) when is_binary(binary) do
    # TODO: pattern match the first codepoint using ::utf8 and recurse on the rest
    # HINT: <<_::utf8, rest::binary>> = binary
    #       1 + codepoint_count(rest)
  end

  @doc """
  Splits a binary at byte position `n`.

  ## Examples

      iex> TaskQueue.Protocol.split_at("HelloWorld", 5)
      {"Hello", "World"}

  """
  @spec split_at(binary(), non_neg_integer()) :: {binary(), binary()}
  def split_at(binary, n) when is_binary(binary) and n >= 0 do
    # TODO: use binary-size(n) in a pattern match
    # HINT: <<head::binary-size(n), rest::binary>> = binary
    #       {head, rest}
  end

  # Private helpers

  defp type_to_byte(:job_request), do: @type_job_request
  defp type_to_byte(:job_result),  do: @type_job_result
  defp type_to_byte(:heartbeat),   do: @type_heartbeat
  defp type_to_byte(:error),       do: @type_error

  # TODO: implement byte_to_type/1 — the inverse of type_to_byte/1
  defp byte_to_type(@type_job_request), do: :job_request
  defp byte_to_type(@type_job_result),  do: :job_result
  defp byte_to_type(@type_heartbeat),   do: :heartbeat
  defp byte_to_type(@type_error),       do: :error
  defp byte_to_type(_),                 do: :unknown
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/protocol_test.exs
defmodule TaskQueue.ProtocolTest do
  use ExUnit.Case, async: true

  alias TaskQueue.Protocol

  describe "encode/2 and decode/1 round-trip" do
    test "job_request with payload" do
      payload = ~s({"type":"send_email","to":"user@example.com"})
      frame = Protocol.encode(:job_request, payload)

      assert {:ok, decoded} = Protocol.decode(frame)
      assert decoded.type == :job_request
      assert decoded.payload == payload
    end

    test "heartbeat with empty payload" do
      frame = Protocol.encode(:heartbeat, "")
      assert {:ok, %{type: :heartbeat, payload: ""}} = Protocol.decode(frame)
    end

    test "error frame" do
      frame = Protocol.encode(:error, "timeout")
      assert {:ok, %{type: :error, payload: "timeout"}} = Protocol.decode(frame)
    end

    test "frame has correct byte structure" do
      frame = Protocol.encode(:heartbeat, "ping")
      # magic (4) + version (1) + type (1) + length (2) + payload (4) = 12
      assert byte_size(frame) == 12
    end

    test "rejects frame with wrong magic" do
      bad_frame = <<"XXXX", 1, 0x03, 0, 0>>
      assert {:error, :invalid_frame} = Protocol.decode(bad_frame)
    end

    test "rejects truncated frame" do
      frame = Protocol.encode(:job_request, "data")
      # Remove the last byte — makes payload shorter than declared length
      truncated = binary_part(frame, 0, byte_size(frame) - 1)
      assert {:error, :invalid_frame} = Protocol.decode(truncated)
    end
  end

  describe "codepoint_count/1 — UTF-8 awareness" do
    test "ASCII string — bytes equal codepoints" do
      assert Protocol.codepoint_count("hello") == 5
    end

    test "multibyte characters counted as one codepoint each" do
      assert Protocol.codepoint_count("café") == 4
      assert Protocol.codepoint_count("naïve") == 5
    end

    test "empty string" do
      assert Protocol.codepoint_count("") == 0
    end
  end

  describe "split_at/2" do
    test "splits binary at byte position" do
      assert Protocol.split_at("HelloWorld", 5) == {"Hello", "World"}
    end

    test "split at 0 returns empty prefix" do
      assert Protocol.split_at("Hello", 0) == {"", "Hello"}
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/task_queue/protocol_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `<<>>` binary syntax | String concat `<>` | External library |
|--------|---------------------|--------------------|-----------------|
| Byte order control | explicit per field | none | depends on lib |
| Pattern matching | symmetric with encoding | requires manual parsing | rarely |
| Multibyte char handling | `::utf8` specifier | string functions | library-specific |
| Compile-time validation | yes (match spec errors) | no | no |
| Performance | zero allocation for pattern match | allocates intermediates | varies |

Reflection question: why is `System.monotonic_time` preferred over `System.os_time` for the timestamp field in a binary frame header? Consider NTP clock adjustments and monotonicity guarantees.

---

## Common production mistakes

**1. `byte_size` vs `String.length` confusion**
`byte_size("café")` returns `5` (bytes). `String.length("café")` returns `4` (codepoints). For the `length` field in a binary protocol, you always want `byte_size` — the receiver reads bytes, not codepoints.

**2. No `binary-size(length)` in payload pattern**
Without the size constraint, the `payload::binary` match captures everything remaining in the buffer — including bytes from the next frame in a TCP stream. Always bound the payload: `payload::binary-size(length)`.

**3. `::8` for UTF-8 strings**
`<<head::8, rest::binary>>` splits on byte boundaries, not codepoint boundaries. Multibyte characters are silently corrupted. Use `::utf8` when iterating over string contents character by character.

**4. Endianness mismatch**
The default in `<<>>` is big-endian. If your counterpart uses little-endian (common in x86 binary formats), you must specify `::little-16` explicitly. Mixing is a silent bug — both sides parse valid numbers, just different ones.

**5. Building frames with string concatenation**
`"TSKQ" <> <<version>> <> <<length::16>> <> payload` works but obscures endianness. The `<<>>` form is explicit and preferred.

---

## Resources

- [Binaries, strings, and charlists — Elixir official guide](https://elixir-lang.org/getting-started/binaries-strings-and-char-lists.html)
- [Bitstring syntax — Erlang reference manual](https://www.erlang.org/doc/reference_manual/expressions.html#bit-strings-and-bitstrings)
- [String vs Binary in Elixir — DockYard blog](https://dockyard.com/blog/2015/07/17/understanding-elixir-types)
- [`:binary` module — Erlang standard library](https://www.erlang.org/doc/man/binary.html)
