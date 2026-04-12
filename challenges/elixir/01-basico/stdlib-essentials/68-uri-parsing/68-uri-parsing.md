# URI parsing and normalization

**Project**: `url_validator` — a tiny library that validates and normalizes URLs before they're stored.

**Difficulty**: ★☆☆☆☆
**Estimated time**: 1 hour

---

## Project context

You're maintaining a link-shortener service. Users submit URLs from forms, mobile apps,
and copy-pasted from emails. Before storing a URL you need to:

1. Reject anything that isn't a valid `http(s)` URL.
2. Normalize equivalent URLs to the same canonical form (so `Example.com/Path` and
   `example.com/Path` don't become two different short links).
3. Resolve relative URLs against a base (for imported links).

Project structure:

```
url_validator/
├── lib/
│   └── url_validator.ex
├── test/
│   └── url_validator_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `URI.parse/1` returns a struct, not a map of strings

`URI.parse/1` never raises. For anything that isn't clearly structured it still returns
a `%URI{}` — with `scheme: nil` and the whole thing dumped into `path`. That's the first
gotcha: a "successful" parse does not mean a valid URL.

```elixir
URI.parse("not a url")
# => %URI{scheme: nil, host: nil, path: "not a url", ...}
```

The rule we enforce: a URL is valid iff `scheme` is `"http"` or `"https"` AND `host` is
a non-empty binary.

### 2. `URI.new/1` (Elixir 1.13+) returns `{:ok, uri}` or `{:error, part}`

Prefer `URI.new/1` over `URI.parse/1` when you actually want to detect malformed input.
`URI.parse/1` is forgiving by design — useful for merging, dangerous for validation.

### 3. `URI.merge/2` resolves relative references per RFC 3986

If a user imports a link like `/about` alongside a base `https://acme.com/blog/`,
`URI.merge/2` gives you `https://acme.com/about`. Don't concatenate strings — merging
handles `..`, `./`, fragments, and query preservation correctly.

### 4. `URI.encode/1` vs `URI.encode_www_form/1`

Different contexts, different rules:

- `URI.encode/1` — percent-encodes reserved chars in a full URI (keeps `/`, `?`, `=`, `&`).
- `URI.encode_www_form/1` — encodes for `application/x-www-form-urlencoded` bodies
  (spaces become `+`, encodes `/` and `=`).

Using the wrong one corrupts query strings in subtle ways (double-encoded slashes).

---

## Implementation

### Step 1: Create the project

```bash
mix new url_validator
cd url_validator
```

### Step 2: `lib/url_validator.ex`

```elixir
defmodule UrlValidator do
  @moduledoc """
  Validates and normalizes HTTP(S) URLs for a link-shortener pipeline.
  """

  @allowed_schemes ~w(http https)

  @doc """
  Validates and normalizes a URL. Returns `{:ok, canonical}` or `{:error, reason}`.

  Normalization rules:
    * scheme and host are lowercased (they're case-insensitive per RFC 3986)
    * path is kept as-is (paths ARE case-sensitive on most servers)
    * default ports (80 for http, 443 for https) are dropped
    * a missing path becomes "/" so "https://a.com" and "https://a.com/" match
  """
  @spec normalize(String.t()) :: {:ok, String.t()} | {:error, atom()}
  def normalize(url) when is_binary(url) do
    # URI.new/1 fails loudly on malformed input — prefer it over URI.parse/1
    # when validation is the goal.
    with {:ok, uri} <- URI.new(url),
         :ok <- check_scheme(uri),
         :ok <- check_host(uri) do
      {:ok, canonicalize(uri)}
    end
  end

  @doc """
  Resolves `relative` against `base`. Both must already be valid absolute URLs
  (for the base) and any reference (for the relative).
  """
  @spec resolve(String.t(), String.t()) :: {:ok, String.t()} | {:error, atom()}
  def resolve(base, relative) when is_binary(base) and is_binary(relative) do
    with {:ok, base_uri} <- URI.new(base),
         :ok <- check_scheme(base_uri),
         {:ok, rel_uri} <- URI.new(relative) do
      # URI.merge/2 handles "..", "./", fragment-only, and absolute references
      # per RFC 3986 section 5. Doing this by hand is a well-known source of bugs.
      {:ok, base_uri |> URI.merge(rel_uri) |> URI.to_string()}
    end
  end

  @doc """
  Encodes a map into a query string safe for use in a URL.

  We use `URI.encode_query/1` which calls `encode_www_form` per pair — the correct
  choice for query strings. `URI.encode/1` on the whole thing would leave `&` and
  `=` intact, which is the opposite of what we want here.
  """
  @spec build_query(map()) :: String.t()
  def build_query(params) when is_map(params), do: URI.encode_query(params)

  # --- private helpers ------------------------------------------------------

  defp check_scheme(%URI{scheme: s}) when s in @allowed_schemes, do: :ok
  defp check_scheme(%URI{scheme: nil}), do: {:error, :missing_scheme}
  defp check_scheme(%URI{}), do: {:error, :unsupported_scheme}

  defp check_host(%URI{host: host}) when is_binary(host) and host != "", do: :ok
  defp check_host(%URI{}), do: {:error, :missing_host}

  defp canonicalize(%URI{} = uri) do
    %URI{
      uri
      | scheme: String.downcase(uri.scheme),
        host: String.downcase(uri.host),
        port: drop_default_port(uri.scheme, uri.port),
        path: uri.path || "/"
    }
    |> URI.to_string()
  end

  defp drop_default_port("http", 80), do: nil
  defp drop_default_port("https", 443), do: nil
  defp drop_default_port(_, port), do: port
end
```

### Step 3: `test/url_validator_test.exs`

```elixir
defmodule UrlValidatorTest do
  use ExUnit.Case, async: true
  doctest UrlValidator

  describe "normalize/1" do
    test "accepts valid http and https URLs" do
      assert {:ok, "https://acme.com/"} = UrlValidator.normalize("https://acme.com")
      assert {:ok, "http://acme.com/path"} = UrlValidator.normalize("http://acme.com/path")
    end

    test "lowercases scheme and host but not path" do
      assert {:ok, "https://acme.com/MixedCase"} =
               UrlValidator.normalize("HTTPS://ACME.com/MixedCase")
    end

    test "drops default ports" do
      assert {:ok, "https://acme.com/"} = UrlValidator.normalize("https://acme.com:443")
      assert {:ok, "http://acme.com/"} = UrlValidator.normalize("http://acme.com:80")
      assert {:ok, "https://acme.com:8443/"} = UrlValidator.normalize("https://acme.com:8443")
    end

    test "rejects non-http schemes" do
      assert {:error, :unsupported_scheme} = UrlValidator.normalize("ftp://acme.com")
      assert {:error, :unsupported_scheme} = UrlValidator.normalize("javascript:alert(1)")
    end

    test "rejects schemeless or hostless input" do
      assert {:error, :missing_scheme} = UrlValidator.normalize("acme.com/path")
      assert {:error, :missing_host} = UrlValidator.normalize("https://")
    end
  end

  describe "resolve/2" do
    test "resolves a relative path against a base" do
      assert {:ok, "https://acme.com/about"} =
               UrlValidator.resolve("https://acme.com/blog/", "/about")
    end

    test "resolves a relative reference with ../" do
      assert {:ok, "https://acme.com/about"} =
               UrlValidator.resolve("https://acme.com/blog/post", "../about")
    end

    test "absolute relative URL wins over base" do
      assert {:ok, "https://other.com/x"} =
               UrlValidator.resolve("https://acme.com/", "https://other.com/x")
    end
  end

  describe "build_query/1" do
    test "encodes spaces and special chars safely" do
      assert UrlValidator.build_query(%{"q" => "hello world", "tag" => "a&b"}) =~
               "q=hello+world"
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

All tests should pass on a clean implementation.

---

## Trade-offs and production gotchas

**1. `URI.parse/1` vs `URI.new/1`**
`parse` never errors — great for library code that wants to be forgiving, terrible for
validation. Use `new` at input boundaries.

**2. Case sensitivity**
Scheme and host are case-insensitive (RFC 3986). Paths are not. Mass-lowercasing the
whole URL silently breaks APIs whose paths depend on case.

**3. IDN / punycode**
`URI` does not convert internationalized domain names (`café.com`) to punycode. If you
store URLs entered by humans, use `:idna` or `IDNA` for that step. Out of scope here,
but a real concern in production.

**4. `URI.encode` vs `URI.encode_www_form`**
Mixing them up double-encodes slashes or corrupts query strings. Rule of thumb:
`encode_www_form` for key/value pairs, `encode` for whole paths.

**5. When NOT to use this**
For a full HTTP client, let `Req`/`Finch`/`:httpc` do their own URL handling. This
module is for validation + normalization at the edges of your system, not for
constructing request targets on the fly.

---

## Resources

- [`URI` — Elixir stdlib](https://hexdocs.pm/elixir/URI.html)
- [RFC 3986 — URI Generic Syntax](https://datatracker.ietf.org/doc/html/rfc3986) — sections 5 (resolution) and 6 (normalization)
- [`:idna` on hex](https://hex.pm/packages/idna) — when you need punycode conversion
