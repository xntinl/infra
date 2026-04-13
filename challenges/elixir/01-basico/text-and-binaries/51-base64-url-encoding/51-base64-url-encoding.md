# Base64URL, URI Encoding, and Signed URLs

**Project**: `signed_url` — standalone Mix project, 1–2 hours  
**Difficulty**: ★★☆☆☆

---

## Project structure

```
signed_url/
├── lib/
│   └── signed_url.ex              # URL signer + verifier
├── test/
│   └── signed_url_test.exs        # ExUnit tests
└── mix.exs
```

---

## What you will learn

Two core concepts:

1. **`Base.url_encode64/2` vs `Base.encode64/2`** — standard Base64 uses `+` and `/`,
   both illegal in URL path segments and ambiguous in query strings (`+` decodes to space
   under form-urlencoded rules). URL-safe Base64 substitutes `-` and `_`. Use it
   whenever the output crosses a URL or a filename.
2. **`URI.encode_www_form/1` vs `URI.encode/2`** — one is for query values
   (spaces → `+`), the other for path segments (spaces → `%20`). Getting it wrong
   produces URLs that look right in a browser but break on the server.

---

## The business problem

You host user-generated media behind a short-lived signed URL (like S3 pre-signed URLs).
Given a filename and an expiry timestamp, produce:

```
https://cdn.example.com/files/report.pdf?exp=1713091200&sig=<base64url-hmac>
```

Requirements:

- The signature binds `path + exp` so an attacker can't swap either.
- After `exp` passes, verification must reject the URL.
- The signature must be safe to paste into a URL — no `+/=` chars.
- Query values must be properly form-encoded (spaces, unicode, punctuation).

---

## Why `Base.url_encode64(..., padding: false)`

Base64 pads with `=` to make the length a multiple of 4. `=` in a URL gets percent-encoded
by well-behaved clients (`%3D`) but passed through raw by lax ones, producing two different
URLs that decode to the same bytes. Stripping padding removes the ambiguity. You can always
re-add padding before decoding if you need to interop with a padded producer.

## Why HMAC and not a plain hash

`SHA256(secret <> path <> exp)` looks signed but is vulnerable to length-extension attacks
on raw SHA-2 (not SHA-3). HMAC is the construction designed specifically for "keyed hash"
and avoids the footgun. `:crypto.mac(:hmac, :sha256, key, data)` is the call to make.
Never invent your own MAC.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"phoenix", "~> 1.0"},
    {:"plug", "~> 1.0"},
  ]
end
```


### Step 1 — Create the project

**Objective**: Signed URLs use HMAC to prove intent; URL-safe Base64 is RFC 4648 section 5 — standard Base64 breaks in URLs.

```bash
mix new signed_url
cd signed_url
```

### Step 2 — `lib/signed_url.ex`

**Objective**: HMAC-SHA256 + Base64url proves URL authenticity without database lookups; constant-time equality prevents timing attacks.

```elixir
defmodule SignedUrl do
  @moduledoc """
  HMAC-signed URL generation and verification, RFC-correct on the encoding side.
  """

  @doc """
  Builds a signed URL.

  * `base_url` — e.g. `"https://cdn.example.com"`.
  * `path` — e.g. `"/files/my report.pdf"`. May contain spaces/unicode; we encode it.
  * `expires_at` — Unix timestamp (seconds) when the URL stops working.
  * `secret` — shared HMAC secret.

  The signature covers the **encoded** path plus the expiry — that way the bytes the
  server sees on incoming requests are the same bytes we signed, no ambiguity.
  """
  @spec build(String.t(), String.t(), integer(), binary()) :: String.t()
  def build(base_url, path, expires_at, secret)
      when is_integer(expires_at) and is_binary(secret) do
    # Path components must use percent-encoding, NOT form encoding (no `+` for spaces).
    # URI.encode/2 with a safety predicate keeps `/` unescaped so path structure survives.
    encoded_path = encode_path(path)
    exp_str = Integer.to_string(expires_at)

    signature = sign(encoded_path, exp_str, secret)

    # Query values use form encoding (`+` for space). Here our values are already
    # URL-safe, but we go through URI.encode_query/1 to keep the habit.
    query = URI.encode_query(exp: exp_str, sig: signature)

    base_url <> encoded_path <> "?" <> query
  end

  @doc """
  Verifies a signed URL.

  Returns `:ok`, `{:error, :expired}`, or `{:error, :invalid_signature}`.
  """
  @spec verify(String.t(), binary(), (-> integer())) ::
          :ok | {:error, :expired | :invalid_signature | :malformed}
  def verify(url, secret, now_fn \\ &default_now/0) when is_binary(secret) do
    with %URI{path: path, query: query} when is_binary(path) and is_binary(query) <-
           URI.parse(url),
         %{"exp" => exp_str, "sig" => sig} <- URI.decode_query(query),
         {exp, ""} <- Integer.parse(exp_str),
         true <- exp > now_fn.() || {:error, :expired},
         expected = sign(path, exp_str, secret),
         true <- constant_time_equal?(sig, expected) do
      :ok
    else
      {:error, _} = err -> err
      false -> {:error, :invalid_signature}
      _ -> {:error, :malformed}
    end
  end

  # ---- internals -------------------------------------------------------------

  defp sign(encoded_path, exp_str, secret) do
    # The signing string uses an explicit separator (`|`) so that `path="a"` + `exp="b"`
    # hashes differently from `path="ab"` + `exp=""` — a classic canonicalization bug.
    data = encoded_path <> "|" <> exp_str

    :crypto.mac(:hmac, :sha256, secret, data)
    |> Base.url_encode64(padding: false)
  end

  # URI.encode/2 with a predicate: keep `/` as-is so we don't turn a path into one big
  # escaped segment. Everything else that isn't an unreserved URI char gets percent-encoded.
  defp encode_path(path) do
    URI.encode(path, &(&1 == ?/ or URI.char_unreserved?(&1)))
  end

  defp default_now, do: System.system_time(:second)

  # Constant-time comparison prevents timing attacks.
  # We compare the decoded bytes because Base.url_decode64 may return different-length
  # binaries for invalid input; :crypto.hash_equals/2 requires equal-length inputs.
  defp constant_time_equal?(a, b) when is_binary(a) and is_binary(b) do
    byte_size(a) == byte_size(b) and :crypto.hash_equals(a, b)
  end
end
```

### Step 3 — `test/signed_url_test.exs`

**Objective**: Test signature tampering (one byte change fails), expiry boundaries (now vs 1s ago), and URL encoding roundtrip.

```elixir
defmodule SignedUrlTest do
  use ExUnit.Case, async: true

  @secret "test-secret"
  @base "https://cdn.example.com"

  test "build produces a parseable URL" do
    url = SignedUrl.build(@base, "/files/report.pdf", 2_000_000_000, @secret)
    assert %URI{scheme: "https", host: "cdn.example.com", path: "/files/report.pdf"} =
             URI.parse(url)

    assert %{"exp" => "2000000000", "sig" => sig} =
             url |> URI.parse() |> Map.fetch!(:query) |> URI.decode_query()

    # URL-safe Base64: no +, /, or = in the signature.
    refute String.contains?(sig, "+")
    refute String.contains?(sig, "/")
    refute String.contains?(sig, "=")
  end

  test "percent-encodes spaces and unicode in the path" do
    url = SignedUrl.build(@base, "/files/my report ñ.pdf", 2_000_000_000, @secret)
    # Space must be %20 in a path, not +
    assert String.contains?(url, "/files/my%20report%20")
    # ñ (U+00F1) is UTF-8 0xC3 0xB1 → %C3%B1
    assert String.contains?(url, "%C3%B1")
  end

  test "verify accepts a valid URL before expiry" do
    exp = 2_000_000_000
    url = SignedUrl.build(@base, "/files/report.pdf", exp, @secret)
    assert SignedUrl.verify(url, @secret, fn -> exp - 1 end) == :ok
  end

  test "verify rejects an expired URL" do
    exp = 1_000
    url = SignedUrl.build(@base, "/files/report.pdf", exp, @secret)
    assert SignedUrl.verify(url, @secret, fn -> exp + 1 end) == {:error, :expired}
  end

  test "verify rejects a tampered path" do
    url = SignedUrl.build(@base, "/files/report.pdf", 2_000_000_000, @secret)
    tampered = String.replace(url, "report.pdf", "admin.pdf")
    assert SignedUrl.verify(tampered, @secret) == {:error, :invalid_signature}
  end

  test "verify rejects a URL signed with a different secret" do
    url = SignedUrl.build(@base, "/files/report.pdf", 2_000_000_000, @secret)
    assert SignedUrl.verify(url, "other-secret") == {:error, :invalid_signature}
  end
end
```

### Step 4 — Run the tests

**Objective**: --warnings-as-errors catches incorrect padding removal; test coverage validates HMAC doesn't silently accept bad sigs.

```bash
mix test
```

All 6 tests should pass.

---


## Key Concepts

### 1. `Base.encode64/1` and `Base.decode64/1` Handle Encoding
Base64 is text-safe encoding for binary data. It's 33% larger but uses only ASCII characters.

### 2. URL-Safe Encoding
URL-safe Base64 uses `-` and `_` instead of `+` and `/`, and omits padding. Use this for URLs and JSON.

### 3. Padding Matters
Base64 with padding is standard. Without padding, some decoders fail. Use the appropriate variant for your context.

---
## Trade-offs

| Encoder | Alphabet | Padding | Use case |
|---------|----------|---------|----------|
| `Base.encode64/2` | `A-Z a-z 0-9 + /` | `=` | Email (MIME), classic Base64 fields |
| `Base.url_encode64/2` | `A-Z a-z 0-9 - _` | optional `=` | URLs, filenames, JWT segments |
| `URI.encode/2` | Percent-encoding | n/a | Path segments (`/foo%20bar`) |
| `URI.encode_www_form/1` | Percent-encoding + `+` for space | n/a | Query values, form bodies |
| `Base.encode16/2` | `0-9 A-F` | n/a | Hex — human-readable, 2x size of Base64 |

---

## Common production mistakes

**1. Using `Base.encode64/2` in a URL**  
The `/` in the output breaks URL path matching. The `+` decodes to a space under
`application/x-www-form-urlencoded` rules, corrupting the value. Always use
`Base.url_encode64/2` for URL-bound data.

**2. Using `URI.encode/2` for query values**  
Spaces come out as `%20`, which is valid but not what most servers *produce*. Mixing
`%20` and `+` in the same URL won't break a correct server but confuses debugging.
Use `URI.encode_www_form/1` for query values.

**3. Signing the unencoded path**  
The server sees the encoded bytes. If you sign `"my file.pdf"` but the request arrives
as `"my%20file.pdf"`, HMAC disagrees. Always canonicalize (here: always encode first,
then sign the encoded form) before hashing.

**4. `==` for signature comparison**  
Leaks timing. Use `:crypto.hash_equals/2` — it's designed for constant-time compare.

**5. Forgetting the separator in signing input**  
`sign(path <> exp)` lets an attacker move bytes between fields. `sign("a", "bc")` and
`sign("ab", "c")` produce the same hash. Always use an unambiguous separator that can't
appear in the encoded inputs (here `|`, which never appears in URL-encoded data).

**6. Using system time directly in verify**  
Makes the function untestable without freezing the clock. Inject time as a callback
(`now_fn`) — the implementation is shorter and tests are deterministic.

---

## When NOT to roll your own

For S3/GCS pre-signed URLs: use the official SDK — they embed complex canonicalization
rules the cloud providers enforce. For general-purpose signed tokens with expiry, look at
[`Plug.Crypto.sign/3`](https://hexdocs.pm/plug_crypto/Plug.Crypto.html#sign/4) which Phoenix
uses under the hood. This exercise teaches the mechanism so you can evaluate those libraries
instead of black-boxing them.

---

## Resources

- [`Base` docs](https://hexdocs.pm/elixir/Base.html)
- [`URI` docs](https://hexdocs.pm/elixir/URI.html)
- [RFC 4648 — Base16, Base32, Base64](https://datatracker.ietf.org/doc/html/rfc4648) — see §5 for URL-safe alphabet
- [RFC 3986 — URI generic syntax](https://datatracker.ietf.org/doc/html/rfc3986) — path vs query encoding rules
- [`Plug.Crypto.sign/4`](https://hexdocs.pm/plug_crypto/Plug.Crypto.html#sign/4) — production-grade signed payloads
