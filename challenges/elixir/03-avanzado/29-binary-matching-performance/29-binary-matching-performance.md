# Binary Matching Performance

**Project**: `api_gateway` — a standalone HTTP gateway exercise

---

## Project context

You are building `api_gateway`, an HTTP gateway that routes traffic to microservices. The gateway
receives raw HTTP/1.1 request lines over TCP and must parse them at high throughput. The infra
team measured that the current parser (using `String.split` and `Regex`) becomes the bottleneck
above 5,000 req/s on a single node.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── middleware/
│           ├── parser.ex           # ← main implementation
│           └── parser_bench.exs    # ← benchmark
├── test/
│   └── api_gateway/
│       └── middleware/
│           └── parser_test.exs     # given tests — must pass without modification
├── bench/
│   └── parser_bench.exs
└── mix.exs
```

---

## The business problem

The gateway's TCP layer receives raw request lines like:

```
GET /users/123/profile?include=avatar&format=json HTTP/1.1
```

The current implementation uses `Regex.run/3` on every request. Under load profiling, the parser
accounts for 22% of CPU time. Your task: implement a binary-matching parser that exploits
BEAM's match context optimization to bring this below 3%.

---

## Why binary matching beats Regex for this use case

`Regex` in BEAM compiles to PCRE, which is general-purpose. It must handle backtracking,
Unicode codepoints, and arbitrary patterns. For a fixed-structure protocol like HTTP, that
generality is wasted.

Binary matching with `<<>>` in BEAM:

1. **Compiles to a cursor** — the runtime creates a *match context* that advances a pointer
   through the binary without copying it.
2. **Length-prefixed slices are O(1)** — `<<_::binary-size(n), rest::binary>>` creates a
   sub-binary that references the original allocation.
3. **Pattern dispatch is exhaustive** — the compiler generates efficient jump tables for
   multi-clause functions that match on the same binary.

The critical optimization is the **match context reuse**. When a function is tail-recursive
and each clause matches the *same binary argument*, BEAM reuses the internal cursor:

```elixir
# BEAM reuses one cursor across all recursive calls — zero allocations on the hot path
defp scan_until_space(<<0x20, rest::binary>>, acc), do: {IO.iodata_to_binary(acc), rest}
defp scan_until_space(<<byte, rest::binary>>, acc), do: scan_until_space(rest, [acc, byte])
defp scan_until_space(<<>>, acc), do: {IO.iodata_to_binary(acc), ""}
```

Context reuse is **broken** when you pass the binary through an intermediate function before
matching it again. The second match creates a new cursor:

```elixir
# Context broken — normalize/1 creates a new reference; BEAM cannot share the cursor
defp parse(binary) do
  binary = normalize(binary)  # intermediate call
  case binary do
    <<0x47, rest::binary>> -> {:get, rest}
    _ -> :error
  end
end
```

---

## Sub-binaries and the reference trap

```elixir
original = :crypto.strong_rand_bytes(1_000_000)

# This creates a SUB-BINARY — a reference into `original`, not a copy
<<_::binary-size(100), slice::binary-size(50), _::binary>> = original

# `slice` is 50 bytes but keeps `original` (1 MB) alive in the GC
# If you store `slice` in a long-lived structure, `original` cannot be collected

# Force a copy to release the reference:
independent = :binary.copy(slice)
```

Rule of thumb: if you extract a small slice from a large binary and store it beyond the
current stack frame, call `:binary.copy/1`.

---

## Implementation

### Step 1: Create the project

```bash
mix new api_gateway --sup
cd api_gateway
mkdir -p lib/api_gateway/middleware
mkdir -p test/api_gateway/middleware
mkdir -p bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/api_gateway/middleware/parser.ex`

The module implements three parsing strategies for the same HTTP request line format.
All three return the identical result shape, allowing direct benchmarking comparison.

The binary matching strategy (`parse_binary/1`) is the target implementation. It scans
the input left-to-right using tail-recursive helpers that preserve BEAM's match context.
Each helper accumulates bytes into an iolist (O(1) per step) and converts to a binary
only at the boundary between tokens.

```elixir
defmodule ApiGateway.Middleware.Parser do
  @moduledoc """
  Parses HTTP/1.1 request lines using binary matching.

  Implements three strategies for benchmarking:
    - parse_regex/1      — baseline, uses compiled Regex
    - parse_split/1      — intermediate, uses String.split
    - parse_binary/1     — target implementation, uses <<>> matching

  All three must return the same shape:
    {:ok, %{method: String.t(), path: String.t(), query: String.t(), version: String.t()}}
    | {:error, :invalid_request_line}
  """

  @request_regex ~r/^(\w+)\s+([^?\s]+)(?:\?([^\s]*))?\s+HTTP\/(\d+\.\d+)$/

  # ---------------------------------------------------------------------------
  # Strategy 1: Regex baseline
  # ---------------------------------------------------------------------------

  @spec parse_regex(binary()) :: {:ok, map()} | {:error, :invalid_request_line}
  def parse_regex(line) do
    case Regex.run(@request_regex, line, capture: :all_but_first) do
      [method, path, query, version] ->
        {:ok, %{method: method, path: path, query: query, version: version}}

      [method, path, version] ->
        {:ok, %{method: method, path: path, query: "", version: version}}

      nil ->
        {:error, :invalid_request_line}
    end
  end

  # ---------------------------------------------------------------------------
  # Strategy 2: String.split intermediate
  # ---------------------------------------------------------------------------

  @spec parse_split(binary()) :: {:ok, map()} | {:error, :invalid_request_line}
  def parse_split(line) do
    case String.split(line, " ", parts: 3) do
      [method, path_query, "HTTP/" <> version] ->
        {path, query} = split_path_query(path_query)
        {:ok, %{method: method, path: path, query: query, version: String.trim(version)}}

      _ ->
        {:error, :invalid_request_line}
    end
  end

  defp split_path_query(path_query) do
    case String.split(path_query, "?", parts: 2) do
      [path, query] -> {path, query}
      [path] -> {path, ""}
    end
  end

  # ---------------------------------------------------------------------------
  # Strategy 3: Binary matching — target implementation
  # ---------------------------------------------------------------------------

  @spec parse_binary(binary()) :: {:ok, map()} | {:error, :invalid_request_line}
  def parse_binary(line) do
    scan_method(line, [])
  end

  # Scans bytes until 0x20 (space), accumulates the method into an iolist.
  # When the space delimiter is found, the accumulated iolist is converted to
  # a binary and scanning continues with scan_path/3.
  defp scan_method(<<0x20, rest::binary>>, acc) do
    method = IO.iodata_to_binary(acc)
    scan_path(rest, [], method)
  end

  # Each byte is appended to the iolist accumulator. Because the same binary
  # argument (`rest`) flows directly into the next recursive call without
  # rebinding, BEAM reuses the match context — zero allocations per byte.
  defp scan_method(<<byte, rest::binary>>, acc) do
    scan_method(rest, [acc, byte])
  end

  defp scan_method(<<>>, _acc), do: {:error, :invalid_request_line}

  # Scans bytes until 0x3F ("?") or 0x20 (space).
  # 0x3F means a query string follows; 0x20 means no query, jump to version.
  defp scan_path(<<0x3F, rest::binary>>, acc, method) do
    path = IO.iodata_to_binary(acc)
    scan_query(rest, [], method, path)
  end

  defp scan_path(<<0x20, rest::binary>>, acc, method) do
    path = IO.iodata_to_binary(acc)
    scan_version(rest, method, path, "")
  end

  defp scan_path(<<byte, rest::binary>>, acc, method) do
    scan_path(rest, [acc, byte], method)
  end

  defp scan_path(<<>>, _acc, _method), do: {:error, :invalid_request_line}

  # Scans bytes until 0x20 (space), accumulates the query string.
  defp scan_query(<<0x20, rest::binary>>, acc, method, path) do
    query = IO.iodata_to_binary(acc)
    scan_version(rest, method, path, query)
  end

  defp scan_query(<<byte, rest::binary>>, acc, method, path) do
    scan_query(rest, [acc, byte], method, path)
  end

  defp scan_query(<<>>, _acc, _method, _path), do: {:error, :invalid_request_line}

  # Matches the literal "HTTP/" prefix and extracts the version string.
  # String.trim/1 handles any trailing whitespace (e.g., \r\n from raw TCP).
  defp scan_version(<<"HTTP/", version::binary>>, method, path, query) do
    {:ok, %{method: method, path: path, query: query, version: String.trim(version)}}
  end

  defp scan_version(_, _, _, _), do: {:error, :invalid_request_line}
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/parser_test.exs
defmodule ApiGateway.Middleware.ParserTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Middleware.Parser

  @lines [
    {"GET /users/123 HTTP/1.1",
     %{method: "GET", path: "/users/123", query: "", version: "1.1"}},
    {"POST /orders?dry_run=true HTTP/1.1",
     %{method: "POST", path: "/orders", query: "dry_run=true", version: "1.1"}},
    {"DELETE /sessions/abc HTTP/1.1",
     %{method: "DELETE", path: "/sessions/abc", query: "", version: "1.1"}},
    {"GET /search?q=elixir&page=2&per_page=10 HTTP/1.1",
     %{method: "GET", path: "/search", query: "q=elixir&page=2&per_page=10", version: "1.1"}}
  ]

  for strategy <- [:parse_regex, :parse_split, :parse_binary] do
    describe "#{strategy}/1" do
      for {line, expected} <- @lines do
        test "parses: #{line}", %{} do
          assert {:ok, result} = apply(Parser, unquote(strategy), [unquote(line)])
          assert result == unquote(Macro.escape(expected))
        end
      end

      test "returns error for malformed input" do
        assert {:error, :invalid_request_line} =
                 apply(Parser, unquote(strategy), ["not a valid request line"])
      end
    end
  end

  describe "all three strategies return identical results" do
    for {line, _} <- @lines do
      test "agree on: #{line}" do
        regex  = Parser.parse_regex(unquote(line))
        split  = Parser.parse_split(unquote(line))
        binary = Parser.parse_binary(unquote(line))

        assert regex == split
        assert split == binary
      end
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/middleware/parser_test.exs --trace
```

All tests should pass. The binary parser scans left-to-right through the input,
splitting on space (0x20) and question mark (0x3F) delimiters, accumulating bytes
into iolists for O(1) per-step allocation.

### Step 6: Benchmark

```elixir
# bench/parser_bench.exs
lines =
  for method <- ["GET", "POST", "PUT", "DELETE"],
      path <- ["/users/1", "/orders/abc/items", "/search"],
      query <- ["", "page=1&per_page=20", "q=test&sort=asc"],
      do: "#{method} #{path}#{if query != "", do: "?#{query}", else: ""} HTTP/1.1"

Benchee.run(
  %{
    "regex"  => fn -> Enum.each(lines, &ApiGateway.Middleware.Parser.parse_regex/1) end,
    "split"  => fn -> Enum.each(lines, &ApiGateway.Middleware.Parser.parse_split/1) end,
    "binary" => fn -> Enum.each(lines, &ApiGateway.Middleware.Parser.parse_binary/1) end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  memory_time: 2,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/parser_bench.exs
```

**Expected result**: `parse_binary` should be 2-4x faster than `parse_regex` and allocate
significantly less memory. If binary and regex are equivalent, verify that your `scan_*`
helpers do not rebind the binary before matching it.

---

## Trade-off analysis

| Aspect | Binary matching | Regex | `String.split` |
|--------|----------------|-------|----------------|
| Allocations on hot path | O(1) with context reuse | O(n) PCRE overhead | O(parts) |
| Handles binary protocols (non-UTF8) | Yes | No | No |
| Expressive power for irregular text | Low | High | Medium |
| Length-prefixed fields (`<<len::16, data::binary-size(len)>>`) | Native | Impossible | Impossible |
| Maintenance cost | High — must know protocol | Low — self-documenting | Medium |

Reflection: when would Regex still be the better choice in `api_gateway`? Consider the
`X-Forwarded-For` header, which can contain a comma-separated list of IPs with optional
port numbers and IPv6 brackets.

---

## Common production mistakes

**1. Breaking the match context with an intermediate function**
If you call `normalize(binary)` before the `<<>>` match, BEAM creates a new cursor for
every call. The optimization only applies when the same binary flows directly into the
`<<>>` clause head without rebinding.

**2. Storing sub-binaries from large request bodies**
If you extract a path segment from a 64 KB HTTP/2 frame and store it in a long-lived
ETS table, the entire 64 KB frame stays in memory. Call `:binary.copy/1` on any slice
you intend to store beyond the current request lifecycle.

**3. Accumulating bytes into a new binary inside the recursive loop**
```elixir
# Every iteration allocates a new binary — O(n^2) total allocations
defp scan(<<b, rest::binary>>, acc), do: scan(rest, acc <> <<b>>)

# Accumulate into an iolist instead — O(1) per step, one copy at the end
defp scan(<<b, rest::binary>>, acc), do: scan(rest, [acc | <<b>>])
```

**4. Using `String.split` on binary protocols**
`String.split` validates UTF-8 encoding. For raw TCP frames or binary protocols, this
adds unnecessary overhead and may raise on non-UTF8 bytes.

**5. Ignoring endianness for multi-byte fields**
Network byte order is big-endian. BEAM's default `::integer` is also big-endian, so
`<<len::16>>` matches network integers correctly. Little-endian file formats need
`<<len::16-little>>` explicitly.

---

## Resources

- [Erlang efficiency guide — Binary handling](https://www.erlang.org/doc/efficiency_guide/binaryhandling.html)
- [The BEAM Book — Binary matching chapter](https://happi.github.io/theBeamBook/#binary-handling)
- [`:binary` module — Erlang docs](https://www.erlang.org/doc/man/binary.html)
- [Benchee](https://github.com/bencheeorg/benchee)
