# Strings and Binaries: Building a Log Parser

**Project**: `log_parser` — a structured data extractor for nginx access logs using binary pattern matching

---

## Why binaries matter in Elixir

In Elixir, strings are UTF-8 encoded binaries. This is not just an implementation
detail — it fundamentally affects how you process text. A string like `"hello"` is
a contiguous sequence of bytes in memory, and Elixir gives you direct access to
those bytes through binary pattern matching.

For a senior developer coming from Java or Python, where strings are opaque objects
with method calls, this is a paradigm shift. Binary pattern matching lets you write
parsers that are both readable and fast — no regex needed for structured formats.

```elixir
# A string IS a binary
iex> is_binary("hello")
true

iex> byte_size("hello")
5

# UTF-8 means some characters take multiple bytes
iex> byte_size("José")
5  # é is 2 bytes

iex> String.length("José")
4  # 4 graphemes
```

---

## The business problem

Your infrastructure team needs to parse nginx access logs to extract structured data
for monitoring dashboards. The standard nginx combined log format looks like:

```
192.168.1.1 - frank [10/Oct/2024:13:55:36 -0700] "GET /api/users HTTP/1.1" 200 2326
```

Build a parser that:

1. Extracts IP, method, path, status code, and response size
2. Uses binary pattern matching where possible (fast path)
3. Falls back to string functions for complex fields
4. Handles malformed lines gracefully

---

## Project structure

```
log_parser/
├── lib/
│   └── log_parser.ex
├── test/
│   └── log_parser_test.exs
└── mix.exs
```

---

## Implementation

### `lib/log_parser.ex`

```elixir
defmodule LogParser do
  @moduledoc """
  Parses nginx combined log format into structured maps.

  Uses a combination of binary pattern matching and String functions
  to extract fields from log lines efficiently.
  """

  @type entry :: %{
          ip: String.t(),
          user: String.t(),
          timestamp: String.t(),
          method: String.t(),
          path: String.t(),
          protocol: String.t(),
          status: non_neg_integer(),
          size: non_neg_integer()
        }

  @doc """
  Parses a single nginx log line into a structured map.

  Returns {:ok, entry} on success, {:error, reason} on failure.

  ## Examples

      iex> line = ~s(192.168.1.1 - frank [10/Oct/2024:13:55:36 -0700] "GET /api/users HTTP/1.1" 200 2326)
      iex> {:ok, entry} = LogParser.parse_line(line)
      iex> entry.ip
      "192.168.1.1"
      iex> entry.method
      "GET"
      iex> entry.status
      200

  """
  @spec parse_line(String.t()) :: {:ok, entry()} | {:error, String.t()}
  def parse_line(line) when is_binary(line) do
    with {:ok, ip, rest} <- extract_ip(line),
         {:ok, user, rest} <- extract_user(rest),
         {:ok, timestamp, rest} <- extract_timestamp(rest),
         {:ok, method, path, protocol, rest} <- extract_request(rest),
         {:ok, status, size} <- extract_status_and_size(rest) do
      {:ok,
       %{
         ip: ip,
         user: user,
         timestamp: timestamp,
         method: method,
         path: path,
         protocol: protocol,
         status: status,
         size: size
       }}
    end
  end

  @doc """
  Parses multiple log lines, returning only successful parses.

  Returns a list of entries and a count of failed lines.

  ## Examples

      iex> lines = [
      ...>   ~s(10.0.0.1 - - [01/Jan/2024:00:00:00 +0000] "GET / HTTP/1.1" 200 512),
      ...>   "garbage line",
      ...>   ~s(10.0.0.2 - admin [01/Jan/2024:00:00:01 +0000] "POST /login HTTP/1.1" 302 0)
      ...> ]
      iex> {entries, failed_count} = LogParser.parse_lines(lines)
      iex> length(entries)
      2
      iex> failed_count
      1

  """
  @spec parse_lines([String.t()]) :: {[entry()], non_neg_integer()}
  def parse_lines(lines) when is_list(lines) do
    {entries, failures} =
      lines
      |> Enum.reduce({[], 0}, fn line, {entries, failures} ->
        case parse_line(line) do
          {:ok, entry} -> {[entry | entries], failures}
          {:error, _} -> {entries, failures + 1}
        end
      end)

    {Enum.reverse(entries), failures}
  end

  @doc """
  Extracts the HTTP method from a request string using binary pattern matching.

  This demonstrates how binary matching can be faster than regex for
  known-format strings.

  ## Examples

      iex> LogParser.extract_method("GET /api/users HTTP/1.1")
      {:ok, "GET"}

      iex> LogParser.extract_method("POST /api/users HTTP/1.1")
      {:ok, "POST"}

  """
  @spec extract_method(String.t()) :: {:ok, String.t()} | {:error, :unknown_method}
  def extract_method("GET " <> _), do: {:ok, "GET"}
  def extract_method("POST " <> _), do: {:ok, "POST"}
  def extract_method("PUT " <> _), do: {:ok, "PUT"}
  def extract_method("DELETE " <> _), do: {:ok, "DELETE"}
  def extract_method("PATCH " <> _), do: {:ok, "PATCH"}
  def extract_method("HEAD " <> _), do: {:ok, "HEAD"}
  def extract_method("OPTIONS " <> _), do: {:ok, "OPTIONS"}
  def extract_method(_), do: {:error, :unknown_method}

  @doc """
  Checks if an IP address matches a known subnet prefix.

  Uses binary prefix matching — O(1) comparison against the prefix bytes.

  ## Examples

      iex> LogParser.internal_ip?("10.0.0.1")
      true

      iex> LogParser.internal_ip?("192.168.1.100")
      true

      iex> LogParser.internal_ip?("8.8.8.8")
      false

  """
  @spec internal_ip?(String.t()) :: boolean()
  def internal_ip?("10." <> _), do: true
  def internal_ip?("172.16." <> _), do: true
  def internal_ip?("172.17." <> _), do: true
  def internal_ip?("192.168." <> _), do: true
  def internal_ip?("127." <> _), do: true
  def internal_ip?(_), do: false

  @doc """
  Classifies HTTP status codes into categories.

  ## Examples

      iex> LogParser.status_category(200)
      :success

      iex> LogParser.status_category(404)
      :client_error

      iex> LogParser.status_category(503)
      :server_error

  """
  @spec status_category(non_neg_integer()) :: atom()
  def status_category(code) when code >= 200 and code < 300, do: :success
  def status_category(code) when code >= 300 and code < 400, do: :redirect
  def status_category(code) when code >= 400 and code < 500, do: :client_error
  def status_category(code) when code >= 500 and code < 600, do: :server_error
  def status_category(_), do: :unknown

  # --- Private extraction functions ---

  @spec extract_ip(String.t()) :: {:ok, String.t(), String.t()} | {:error, String.t()}
  defp extract_ip(line) do
    case String.split(line, " ", parts: 2) do
      [ip, rest] -> {:ok, ip, rest}
      _ -> {:error, "cannot extract IP from: #{truncate(line)}"}
    end
  end

  @spec extract_user(String.t()) :: {:ok, String.t(), String.t()} | {:error, String.t()}
  defp extract_user(line) do
    # Format: "- username " or "- - "
    case String.split(line, " ", parts: 3) do
      [_ident, user, rest] -> {:ok, user, rest}
      _ -> {:error, "cannot extract user from: #{truncate(line)}"}
    end
  end

  @spec extract_timestamp(String.t()) ::
          {:ok, String.t(), String.t()} | {:error, String.t()}
  defp extract_timestamp(line) do
    with "[" <> rest <- line,
         {timestamp, "] " <> remainder} <- split_on_bracket(rest) do
      {:ok, timestamp, remainder}
    else
      _ -> {:error, "cannot extract timestamp from: #{truncate(line)}"}
    end
  end

  @spec split_on_bracket(String.t()) :: {String.t(), String.t()} | :error
  defp split_on_bracket(string) do
    case String.split(string, "]", parts: 2) do
      [before, after_bracket] -> {before, "]" <> after_bracket}
      _ -> :error
    end
  end

  @spec extract_request(String.t()) ::
          {:ok, String.t(), String.t(), String.t(), String.t()} | {:error, String.t()}
  defp extract_request(line) do
    with "\"" <> rest <- line,
         [request_str, remainder] <- String.split(rest, "\"", parts: 2),
         [method, path, protocol] <- String.split(request_str, " ", parts: 3) do
      {:ok, method, path, protocol, String.trim_leading(remainder)}
    else
      _ -> {:error, "cannot extract request from: #{truncate(line)}"}
    end
  end

  @spec extract_status_and_size(String.t()) ::
          {:ok, non_neg_integer(), non_neg_integer()} | {:error, String.t()}
  defp extract_status_and_size(rest) do
    parts = rest |> String.trim() |> String.split(" ", parts: 2)

    with [status_str, size_str | _] <- parts,
         {status, ""} <- Integer.parse(status_str),
         {size, _} <- Integer.parse(String.trim(size_str)) do
      {:ok, status, size}
    else
      _ -> {:error, "cannot extract status/size from: #{truncate(rest)}"}
    end
  end

  @spec truncate(String.t()) :: String.t()
  defp truncate(string) when byte_size(string) > 50 do
    String.slice(string, 0, 50) <> "..."
  end

  defp truncate(string), do: string
end
```

**Why this works:**

- `extract_method/1` uses binary prefix matching (`"GET " <> _`). The BEAM compiles
  this to a direct byte comparison — no string scanning, no regex compilation. For
  HTTP methods, which are a fixed set of short prefixes, this is the optimal approach.
- `internal_ip?/1` matches IP prefixes as binary heads. This is O(1) against each prefix.
- `parse_line/1` uses `with` to chain extraction steps. Each step returns the extracted
  value and the remaining string. If any step fails, the entire parse fails with a
  descriptive error.
- `Integer.parse/1` returns `{integer, rest}` or `:error` — never raises. This is
  safer than `String.to_integer/1` which raises on invalid input.

### Binary pattern matching explained

```elixir
# Match a known prefix and capture the rest
"GET " <> rest = "GET /api/users"
# rest => "/api/users"

# Match specific bytes (UTF-8 code points)
<<first_byte, _rest::binary>> = "Hello"
# first_byte => 72 (ASCII 'H')

# Match a fixed-length prefix
<<ip_bytes::binary-size(7), _::binary>> = "1.2.3.4 - user"
# ip_bytes => "1.2.3.4"  (only works when length is known)

# Pattern match on UTF-8 characters
<<char::utf8, rest::binary>> = "José"
# char => 74 (code point for 'J')
# rest => "osé"
```

The `<< >>` syntax is the binary pattern matching operator. `binary-size(n)` matches
exactly `n` bytes. `utf8` matches one UTF-8 code point (which may be multiple bytes).

### Tests

```elixir
# test/log_parser_test.exs
defmodule LogParserTest do
  use ExUnit.Case, async: true

  doctest LogParser

  @valid_line ~s(192.168.1.1 - frank [10/Oct/2024:13:55:36 -0700] "GET /api/users HTTP/1.1" 200 2326)

  describe "parse_line/1" do
    test "parses a valid nginx log line" do
      assert {:ok, entry} = LogParser.parse_line(@valid_line)
      assert entry.ip == "192.168.1.1"
      assert entry.user == "frank"
      assert entry.method == "GET"
      assert entry.path == "/api/users"
      assert entry.protocol == "HTTP/1.1"
      assert entry.status == 200
      assert entry.size == 2326
    end

    test "parses line with dash user" do
      line = ~s(10.0.0.1 - - [01/Jan/2024:00:00:00 +0000] "POST /login HTTP/1.1" 302 0)
      assert {:ok, entry} = LogParser.parse_line(line)
      assert entry.user == "-"
      assert entry.method == "POST"
      assert entry.status == 302
    end

    test "returns error for malformed line" do
      assert {:error, _reason} = LogParser.parse_line("garbage")
    end

    test "returns error for empty string" do
      assert {:error, _reason} = LogParser.parse_line("")
    end
  end

  describe "parse_lines/1" do
    test "parses multiple lines and counts failures" do
      lines = [
        @valid_line,
        "bad line",
        ~s(10.0.0.2 - - [01/Jan/2024:00:00:01 +0000] "GET / HTTP/1.1" 200 512)
      ]

      {entries, failed} = LogParser.parse_lines(lines)
      assert length(entries) == 2
      assert failed == 1
    end

    test "handles all-valid input" do
      {entries, failed} = LogParser.parse_lines([@valid_line])
      assert length(entries) == 1
      assert failed == 0
    end

    test "handles empty input" do
      {entries, failed} = LogParser.parse_lines([])
      assert entries == []
      assert failed == 0
    end
  end

  describe "extract_method/1" do
    test "recognizes all standard HTTP methods" do
      assert {:ok, "GET"} = LogParser.extract_method("GET /path HTTP/1.1")
      assert {:ok, "POST"} = LogParser.extract_method("POST /path HTTP/1.1")
      assert {:ok, "PUT"} = LogParser.extract_method("PUT /path HTTP/1.1")
      assert {:ok, "DELETE"} = LogParser.extract_method("DELETE /path HTTP/1.1")
      assert {:ok, "PATCH"} = LogParser.extract_method("PATCH /path HTTP/1.1")
      assert {:ok, "HEAD"} = LogParser.extract_method("HEAD /path HTTP/1.1")
      assert {:ok, "OPTIONS"} = LogParser.extract_method("OPTIONS /path HTTP/1.1")
    end

    test "returns error for unknown method" do
      assert {:error, :unknown_method} = LogParser.extract_method("UNKNOWN /path")
    end
  end

  describe "internal_ip?/1" do
    test "recognizes RFC 1918 addresses" do
      assert LogParser.internal_ip?("10.0.0.1")
      assert LogParser.internal_ip?("192.168.1.100")
      assert LogParser.internal_ip?("172.16.0.1")
      assert LogParser.internal_ip?("127.0.0.1")
    end

    test "rejects public addresses" do
      refute LogParser.internal_ip?("8.8.8.8")
      refute LogParser.internal_ip?("203.0.113.1")
    end
  end

  describe "status_category/1" do
    test "classifies HTTP status codes" do
      assert LogParser.status_category(200) == :success
      assert LogParser.status_category(201) == :success
      assert LogParser.status_category(301) == :redirect
      assert LogParser.status_category(404) == :client_error
      assert LogParser.status_category(500) == :server_error
      assert LogParser.status_category(503) == :server_error
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---

## Strings vs binaries vs charlists

| Type | Internal | Example | Use when |
|------|----------|---------|----------|
| String (binary) | UTF-8 bytes | `"hello"` | Always — default in Elixir |
| Charlist | List of code points | `'hello'` | Erlang interop only |
| Iodata | Nested lists/binaries | `["hello", " ", "world"]` | Building output without copying |

A common mistake: `'hello'` is NOT a string in Elixir. It is a charlist (a list
of integers). `'hello' == [104, 101, 108, 108, 111]` is `true`. You encounter
charlists when calling Erlang functions directly.

---

## Common production mistakes

**1. `String.length/1` vs `byte_size/1`**
`String.length("José")` returns 4 (graphemes). `byte_size("José")` returns 5 (bytes).
Use `byte_size/1` for binary protocol work and storage calculations. Use
`String.length/1` for user-facing character counts.

**2. Using regex when pattern matching suffices**
For fixed-format strings (HTTP methods, IP prefixes, status codes), binary pattern
matching compiles to direct byte comparison. Regex compiles to a state machine.
The pattern match is both faster and more readable for these cases.

**3. Building strings with concatenation in a loop**
`result = result <> chunk` copies the entire binary on each iteration.
Use iodata lists instead: `[result | chunk]` then `IO.iodata_to_binary/1` at the end.

**4. `String.to_integer/1` on untrusted input**
`String.to_integer("abc")` raises `ArgumentError`. Use `Integer.parse/1` which
returns `:error` for invalid input. Always prefer the non-raising variant for
external data.

**5. Forgetting that string interpolation copies**
`"Hello, #{name}"` creates a new binary. In hot loops, build iodata instead.

---

## Resources

- [String — HexDocs](https://hexdocs.pm/elixir/String.html)
- [Binary pattern matching — Elixir Getting Started](https://elixir-lang.org/getting-started/binaries-strings-and-charlists.html)
- [IO data — Elixir Getting Started](https://elixir-lang.org/getting-started/io-and-the-file-system.html)
- [Regex — HexDocs](https://hexdocs.pm/elixir/Regex.html)
