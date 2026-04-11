# Bitstrings and Advanced Binaries

## Goal

Build a `task_queue` binary protocol encoder/decoder using Elixir's `<<>>` syntax. Learn to define fixed-format binary frames with explicit field sizes and endianness, parse them with pattern matching, and handle UTF-8 codepoints vs raw bytes.

---

## The binary frame format

External agents communicate over TCP using a fixed-format binary frame:

```
| magic (4 bytes: "TSKQ") | version (1 byte) | type (1 byte) | length (2 bytes) | payload |
```

- `magic` -- detects frame boundaries and rejects misaligned data
- `version` -- protocol version, currently `1`
- `type` -- `0x01` = job_request, `0x02` = job_result, `0x03` = heartbeat, `0xFF` = error
- `length` -- byte size of the payload, big-endian 16-bit
- `payload` -- UTF-8 binary (JSON-encoded job data), up to 65535 bytes

---

## Why `<<>>` instead of string manipulation

String concatenation for binary protocols is error-prone: byte order, alignment, and field sizes are invisible. `<<>>` makes them explicit:

```elixir
# String concatenation -- opaque, order-dependent
<<0xDE, 0xAD>> <> <<1>> <> <<byte_size(payload)::16>> <> payload

# Binary syntax -- field names, sizes, and endianness are all visible
<<0xDE, 0xAD, version::8, byte_size(payload)::big-16, payload::binary>>
```

Pattern matching on the same syntax makes encode/decode symmetric -- the parser is literally the inverse of the encoder.

---

## Why `::utf8` for strings

Strings in Elixir are UTF-8 binaries. A multibyte character like `e` occupies 2 bytes. Using `::8` for character iteration treats multibyte characters as separate bytes, producing corrupt output. The `::utf8` specifier extracts complete codepoints:

```elixir
<<head::utf8, rest::binary>> = "hello"
# head => 104 ('h'), rest => "ello"  -- correct for any character
```

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps, do: []
end
```

### Step 2: `lib/task_queue/protocol.ex` -- binary frame encoder/decoder

```elixir
defmodule TaskQueue.Protocol do
  @moduledoc """
  Binary framing protocol for communication between task_queue and external agents.

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
    <<@magic::binary, @version::8, type_byte::8, length::big-16, payload::binary>>
  end

  @doc """
  Decodes a binary frame into `{:ok, %{type: atom, payload: binary}}` or
  `{:error, :invalid_frame}`.

  The decoder pattern-matches on the magic bytes first, rejecting any frame that
  does not start with "TSKQ". The `binary-size(length)` constraint ensures the
  payload field matches exactly the declared length -- if the frame is truncated,
  the pattern match fails and returns `{:error, :invalid_frame}`.

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
        type_atom = byte_to_type(type_byte)
        {:ok, %{type: type_atom, payload: payload}}

      _ ->
        {:error, :invalid_frame}
    end
  end

  @doc """
  Returns the number of UTF-8 codepoints in a binary string.

  This is different from `byte_size/1` for strings containing multibyte characters.

  ## Examples

      iex> TaskQueue.Protocol.codepoint_count("cafe")
      4

      iex> TaskQueue.Protocol.codepoint_count("hello")
      5

  """
  @spec codepoint_count(binary()) :: non_neg_integer()
  def codepoint_count(<<>>), do: 0

  def codepoint_count(binary) when is_binary(binary) do
    <<_::utf8, rest::binary>> = binary
    1 + codepoint_count(rest)
  end

  @doc """
  Splits a binary at byte position `n`.

  ## Examples

      iex> TaskQueue.Protocol.split_at("HelloWorld", 5)
      {"Hello", "World"}

  """
  @spec split_at(binary(), non_neg_integer()) :: {binary(), binary()}
  def split_at(binary, n) when is_binary(binary) and n >= 0 do
    <<head::binary-size(n), rest::binary>> = binary
    {head, rest}
  end

  defp type_to_byte(:job_request), do: @type_job_request
  defp type_to_byte(:job_result),  do: @type_job_result
  defp type_to_byte(:heartbeat),   do: @type_heartbeat
  defp type_to_byte(:error),       do: @type_error

  defp byte_to_type(@type_job_request), do: :job_request
  defp byte_to_type(@type_job_result),  do: :job_result
  defp byte_to_type(@type_heartbeat),   do: :heartbeat
  defp byte_to_type(@type_error),       do: :error
  defp byte_to_type(_),                 do: :unknown
end
```

### Step 3: Tests

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
      truncated = binary_part(frame, 0, byte_size(frame) - 1)
      assert {:error, :invalid_frame} = Protocol.decode(truncated)
    end
  end

  describe "codepoint_count/1 -- UTF-8 awareness" do
    test "ASCII string -- bytes equal codepoints" do
      assert Protocol.codepoint_count("hello") == 5
    end

    test "multibyte characters counted as one codepoint each" do
      assert Protocol.codepoint_count("cafe") == 4
      assert Protocol.codepoint_count("naive") == 5
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

### Step 4: Run

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

---

## Common production mistakes

**1. `byte_size` vs `String.length` confusion**
`byte_size("cafe")` may differ from `String.length("cafe")` for multibyte strings. For the `length` field in a binary protocol, you always want `byte_size`.

**2. No `binary-size(length)` in payload pattern**
Without the size constraint, `payload::binary` captures everything remaining -- including bytes from the next frame in a TCP stream.

**3. `::8` for UTF-8 strings**
`<<head::8, rest::binary>>` splits on byte boundaries. Multibyte characters are silently corrupted. Use `::utf8` for character iteration.

**4. Endianness mismatch**
The default in `<<>>` is big-endian. If the counterpart uses little-endian, specify `::little-16` explicitly.

**5. Building frames with string concatenation**
`"TSKQ" <> <<version>> <> <<length::16>> <> payload` works but obscures endianness. The `<<>>` form is explicit and preferred.

---

## Resources

- [Binaries, strings, and charlists -- Elixir official guide](https://elixir-lang.org/getting-started/binaries-strings-and-char-lists.html)
- [Bitstring syntax -- Erlang reference manual](https://www.erlang.org/doc/reference_manual/expressions.html#bit-strings-and-bitstrings)
- [`:binary` module -- Erlang standard library](https://www.erlang.org/doc/man/binary.html)
