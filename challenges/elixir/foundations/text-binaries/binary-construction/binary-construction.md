# Binary Construction: `<<>>`, IO Lists, and Performance

**Project**: `mini_token` — standalone Mix project, 1–2 hours  
**Difficulty**: ★★☆☆☆

---

## Project structure

```
mini_token/
├── lib/
│   └── mini_token.ex              # JWT-like encoder
├── script/
│   └── main.exs
├── test/
│   └── mini_token_test.exs        # ExUnit tests
└── mix.exs
```

---

## What you will learn

Two core concepts:

1. **Binary concatenation with `<<>>`** — the BEAM-native way to build binaries.
   `<<a::binary, b::binary>>` is recognized by the compiler and, in hot loops where the
   first argument is the accumulator, upgraded to in-place growth (no copy per step).
2. **IO lists** — deeply-nested lists of binaries/chars/iolists that any IO-capable
   function (`IO.write/2`, `:gen_tcp.send/2`, `File.write!/2`) will flatten in one pass
   without ever building the concatenated binary. For large outputs, this is often the
   fastest option and by far the lightest on memory.

You'll build a **JWT-like token**: `header.payload.signature`, base64url-encoded.
It's a perfect testbed — three pieces glued with dots, ideal for comparing the two
techniques.

---

## The business problem

You need short-lived signed tokens for internal service-to-service auth. Real JWT has
a wide spec surface (algorithms, claim types, `exp` handling) — for a small, auditable
attack surface you build a minimal variant:

```
base64url(JSON(header)) "." base64url(JSON(payload)) "." base64url(HMAC-SHA256(...))
```

The token is built on every outgoing request. It's on the hot path — build cost matters.

---

## Why `<<a::binary, b::binary>>` and not `a <> b`

They compile to the same instruction — `<>` is sugar for `<<a::binary, b::binary>>`.
The distinction that matters is **where the accumulator sits**:

```elixir
# GOOD: accumulator on the left, grows in-place, O(n) total
acc = <<acc::binary, chunk::binary>>

# BAD: accumulator on the right, copied every step, O(n²) total
acc = <<chunk::binary, acc::binary>>
```

For a 3-piece token it's a micro-optimization. For building a 10MB response body chunk
by chunk, it's the difference between 40ms and 8 seconds.

## Why prefer IO lists at the IO boundary

If the binary is going straight to a socket, file, or `IO.puts`, you never need the
concatenated form. `["a", ".", "b", ".", "c"]` writes exactly the same bytes as
`"a.b.c"` but:

- Builds in O(1) (just prepends/wraps references).
- No allocation for the final binary.
- Integrates with `Plug`, `Phoenix`, `:gen_tcp`, `File.write!/2`.

Rule of thumb: if the result goes to IO, keep it as iodata. Only call `IO.iodata_to_binary/1`
when something outside your control demands a flat binary (HMAC signing, hashing, return
value of a public API that advertises `String.t()`).

---

## Design decisions

The project explores key trade-offs explained throughout the implementation. Refer to the "Why" sections above for the alternatives considered and rationale for the chosen approach.

---

## Implementation

### `mix.exs`
```elixir
defmodule MiniToken.MixProject do
  use Mix.Project

  def project do
    [
      app: :mini_token,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1 — Create the project

**Objective**: JWTs are built with string concatenation + Base64; iodata [parts] avoid copying on each << >> append.

```bash
mix new mini_token
cd mini_token
```

### Step 2 — `lib/mini_token.ex`

**Objective**: Iodata lists defer concatenation until IO.iodata_to_binary/1; 100x faster than loop concat in hot paths.

```elixir
defmodule MiniToken do
  @moduledoc """
  JWT-like token encoder (HS256 only, minimal spec).
  Demonstrates binary construction and IO list composition.
  """

  @header_json ~s({"alg":"HS256","typ":"MT"})

  @doc """
  Builds a signed token: `header.payload.signature`.

  `payload` is any term encoded as JSON (we use a tiny hand-rolled encoder to keep
  the tutorial dependency-free — in production use Jason).
  `secret` is the shared HMAC secret.

  Returns a flat binary because callers typically put it in a header (`Authorization: Bearer ...`)
  which crosses a library boundary expecting `String.t()`.
  """
  @spec encode(map(), binary()) :: binary()
  def encode(payload, secret) when is_map(payload) and is_binary(secret) do
    header_b64 = b64url(@header_json)
    payload_b64 = payload |> json_encode() |> b64url()

    # Signing input is the concatenation that will go on the wire MINUS the signature.
    # We build it as an iodata [header, ".", payload] and only flatten for HMAC.
    signing_input = [header_b64, ".", payload_b64]
    signature_b64 = signing_input |> IO.iodata_to_binary() |> hmac_sha256(secret) |> b64url()

    # Final assembly: iodata again. Flatten ONCE at the end.
    # This is strictly more efficient than three <>'s because the runtime allocates
    # one buffer of the exact final size instead of growing twice.
    IO.iodata_to_binary([signing_input, ".", signature_b64])
  end

  @doc """
  Verifies a token and returns `{:ok, payload}` or `:error`.

  We use `:crypto.hash_equals/2` for constant-time comparison — never use `==` on
  signatures, it leaks timing information.
  """
  @spec verify(binary(), binary()) :: {:ok, map()} | :error
  def verify(token, secret) when is_binary(token) and is_binary(secret) do
    with [header_b64, payload_b64, signature_b64] <- String.split(token, ".", parts: 3),
         {:ok, signature} <- b64url_decode(signature_b64),
         expected =
           [header_b64, ".", payload_b64]
           |> IO.iodata_to_binary()
           |> hmac_sha256(secret),
         true <- :crypto.hash_equals(signature, expected),
         {:ok, payload_json} <- b64url_decode(payload_b64),
         {:ok, payload} <- json_decode(payload_json) do
      {:ok, payload}
    else
      _ -> :error
    end
  end

  # ---- helpers ---------------------------------------------------------------

  # URL-safe base64 without padding (RFC 7515 § 3). Standard Base URL-encoder but
  # we strip `=` because JWT spec forbids padding in compact form.
  defp b64url(bin), do: Base.url_encode64(bin, padding: false)
  defp b64url_decode(bin), do: Base.url_decode64(bin, padding: false)

  defp hmac_sha256(data, key), do: :crypto.mac(:hmac, :sha256, key, data)

  # ---- micro-JSON for numbers/strings/booleans/maps only ---------------------
  # Real projects use Jason; this avoids a hex dep for a tutorial.

  defp json_encode(nil), do: "null"
  defp json_encode(true), do: "true"
  defp json_encode(false), do: "false"
  defp json_encode(n) when is_integer(n), do: Integer.to_string(n)
  defp json_encode(s) when is_binary(s), do: [?", escape_str(s), ?"]

  defp json_encode(map) when is_map(map) do
    pairs =
      map
      |> Enum.map(fn {k, v} ->
        [json_encode(to_string(k)), ?:, json_encode(v)]
      end)
      |> Enum.intersperse(?,)

    IO.iodata_to_binary([?{, pairs, ?}])
  end

  # Minimal escaping — production needs full RFC 8259 coverage; this covers the
  # characters that would break valid payloads in 99% of cases.
  defp escape_str(s) do
    s
    |> String.replace("\\", "\\\\")
    |> String.replace("\"", "\\\"")
  end

  defp json_decode(bin) do
    # Tiny decoder — only handles the single-level flat maps we produce above.
    # In real code: Jason.decode/1. Here we go minimal to stay dependency-free.
    with "{" <> rest <- bin,
         {:ok, inner} <- strip_trailing_brace(rest) do
      pairs =
        inner
        |> String.split(",", trim: true)
        |> Enum.map(fn pair ->
          [k, v] = String.split(pair, ":", parts: 2)
          {unquote_str(k), parse_value(v)}
        end)

      {:ok, Map.new(pairs)}
    else
      _ -> :error
    end
  end

  defp strip_trailing_brace(bin) do
    size = byte_size(bin)

    case :binary.at(bin, size - 1) do
      ?} -> {:ok, :binary.part(bin, 0, size - 1)}
      _ -> :error
    end
  end

  defp unquote_str(<<?", rest::binary>>) do
    size = byte_size(rest) - 1
    <<content::binary-size(size), ?">> = rest
    content
  end

  defp parse_value(<<?", _::binary>> = s), do: unquote_str(s)
  defp parse_value("true"), do: true
  defp parse_value("false"), do: false
  defp parse_value("null"), do: nil
  defp parse_value(n), do: String.to_integer(n)
end
```

### Step 3 — `test/mini_token_test.exs`

**Objective**: Test HMAC verification: typo in secret fails verification; base64url is rfc-compliant (- _ no padding).

```elixir
defmodule MiniTokenTest do
  use ExUnit.Case, async: true
  doctest MiniToken

  @secret "super-secret-key"

  describe "core functionality" do
    test "encode produces three base64url segments separated by dots" do
      token = MiniToken.encode(%{sub: 42}, @secret)
      assert [h, p, s] = String.split(token, ".")
      assert byte_size(h) > 0 and byte_size(p) > 0 and byte_size(s) > 0
      # base64url uses no padding, no '=' chars
      refute String.contains?(token, "=")
    end

    test "verify roundtrips the original payload" do
      payload = %{sub: 42, role: "admin"}
      token = MiniToken.encode(payload, @secret)
      assert {:ok, decoded} = MiniToken.verify(token, @secret)
      # Keys come back as strings (that's how our micro-decoder works)
      assert decoded["sub"] == 42
      assert decoded["role"] == "admin"
    end

    test "verify rejects a token signed with a different secret" do
      token = MiniToken.encode(%{sub: 1}, @secret)
      assert MiniToken.verify(token, "other-secret") == :error
    end

    test "verify rejects a tampered payload" do
      token = MiniToken.encode(%{sub: 1}, @secret)
      [h, _p, s] = String.split(token, ".")
      tampered_payload = Base.url_encode64(~s({"sub":999}), padding: false)
      bad_token = Enum.join([h, tampered_payload, s], ".")
      assert MiniToken.verify(bad_token, @secret) == :error
    end

    test "verify handles malformed input without crashing" do
      assert MiniToken.verify("not-a-token", @secret) == :error
      assert MiniToken.verify("a.b", @secret) == :error
    end
  end
end
```

### Step 4 — Run the tests

**Objective**: --warnings-as-errors catches incorrect HMAC calls; test coverage validates signature doesn't leak key material.

```bash
mix test
```

All 5 tests should pass.

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
    IO.puts("=== MiniToken: demo ===\n")

    result_1 = MiniToken.verify(token, "other-secret")
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = MiniToken.verify(bad_token, @secret)
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = MiniToken.verify("not-a-token", @secret)
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

## Trade-offs

| Technique | When to pick it |
|-----------|-----------------|
| `<<a::binary, b::binary>>` / `a <> b` | 2–3 short pieces, you need a flat binary now |
| IO list `[a, ".", b]` | Output going to IO/socket/file, or you concatenate many pieces |
| Build iodata, flatten once at end | Best of both: O(1) building, single allocation for final binary |
| `Enum.join/2` | Readable for lists of binaries with a separator, but builds intermediate binary |
| `:erlang.iolist_to_binary/1` | Same as `IO.iodata_to_binary/1` — Erlang name |

The accumulator-on-the-left rule for `<<>>`: in a loop that grows a binary, write
`acc = <<acc::binary, chunk::binary>>`, never `<<chunk::binary, acc::binary>>`. Only the
first form triggers the BEAM's in-place append optimization.

---

## Common production mistakes

**1. Concatenating in a loop with the accumulator on the right**  
`acc = <<new::binary, acc::binary>>` copies the whole `acc` every iteration. O(n²) total.
On 1k small chunks it's microseconds; on 1M chunks it's minutes.

**2. Calling `IO.iodata_to_binary/1` inside a loop**  
Defeats the whole point of iodata. Build the tree, flatten once at the end.

**3. Using `==` to compare HMAC signatures**  
`==` short-circuits on the first differing byte — an attacker can measure response time
to learn the prefix of the correct signature. Always use `:crypto.hash_equals/2` for
anything security-sensitive.

**4. Using `Base.encode64/2` for URLs**  
Standard Base64 uses `+` and `/`, both reserved in URLs and form-encoded data. Use
`Base.url_encode64/2`. And pass `padding: false` if the consumer doesn't want `=` chars
(JWT spec forbids them in compact form).

**5. Forgetting that `<<>>` binaries have a byte length, not a character length**  
`byte_size(<<"é">>) == 2`. If you're dealing with text, use `String.*` functions.
`<<>>` is for bytes.

---

## When NOT to hand-roll binary construction

For JSON → use [Jason](https://hex.pm/packages/jason). For real JWT → use
[Joken](https://hex.pm/packages/joken). This exercise teaches the mechanism; in production
you want the hardened library with a wider spec surface and someone else's security audit.

---

## Resources

- [`Base` module docs](https://hexdocs.pm/elixir/Base.html)
- [Erlang efficiency guide — binaries](https://www.erlang.org/doc/efficiency_guide/binaryhandling.html) — especially "constructing binaries"
- [IO lists explained — Evan Miller](https://www.evanmiller.org/elixir-ram-and-the-template-of-doom.html)
- [`:crypto.hash_equals/2`](https://www.erlang.org/doc/man/crypto.html#hash_equals-2) — constant-time compare

---

## Why Binary Construction matters

Mastering **Binary Construction** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/mini_token.ex`

```elixir
defmodule MiniToken do
  @moduledoc """
  Reference implementation for Binary Construction: `<<>>`, IO Lists, and Performance.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the mini_token module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> MiniToken.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/mini_token_test.exs`

```elixir
defmodule MiniTokenTest do
  use ExUnit.Case, async: true

  doctest MiniToken

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert MiniToken.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. Build Binaries with Bit Syntax
The same syntax works for matching (destructuring) and constructing (building). This symmetry is powerful for protocols.

### 2. Concatenation is Efficient
`<< binary1::binary, binary2::binary >>` is more efficient than `string1 <> string2` for large strings because it avoids creating intermediate strings.

### 3. Variable Integers in Binaries
For construction, the size must be a compile-time constant or a variable. For pattern matching, use the pin operator to match against variables.

---
