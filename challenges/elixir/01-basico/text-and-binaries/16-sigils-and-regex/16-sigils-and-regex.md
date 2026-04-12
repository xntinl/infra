# Sigils and Regex: Building a Log Filter CLI

**Project**: `log_filter` — a CLI tool that extracts IPs, status codes, and error lines from nginx access logs

**Difficulty**: ★★☆☆☆
**Estimated time**: 2 hours

---

## Why sigils matter for a senior developer

Elixir sigils (`~r`, `~w`, `~s`, `~D`, `~U`, `~c`) are compile-time constructors for
common data types. They are not syntactic sugar for "strings with magic" — each sigil
produces a specific struct or value at compile time, often with validation.

Coming from Ruby or Python, the instinct is to treat `~r/foo/` as a regex literal.
In Elixir it is: a call to `Regex.compile!/2` evaluated at compile time, returning a
`%Regex{}` struct. Invalid patterns fail the build, not the first request in production.

Understanding sigils matters when you:

- Parse structured text (logs, CSVs, config files) where regex is the right tool
- Build lists of atoms or strings where `~w` collapses five lines into one
- Handle dates and datetimes where `~D[2025-01-01]` is safer than string parsing
- Write heredocs with `~S` to preserve escapes verbatim (critical for shell scripts)

---

## The business problem

Your SRE team exports nginx access logs nightly. Before alerting on anomalies, you
need a CLI that:

1. Reads a log file line by line
2. Extracts the client IP, HTTP status code, and request path from each line
3. Filters lines matching an optional status code range (e.g. `--status 5xx`)
4. Filters by IP pattern (e.g. `--ip 10.0.0.*`)
5. Emits a summary: total lines, matches, unique IPs

Log lines follow the nginx combined format:

```
192.168.1.10 - - [12/Apr/2026:10:15:32 +0000] "GET /api/users HTTP/1.1" 200 1234 "-" "curl/8.0"
10.0.0.5 - - [12/Apr/2026:10:15:33 +0000] "POST /api/login HTTP/1.1" 503 89 "-" "Mozilla/5.0"
```

---

## Project structure

```
log_filter/
├── lib/
│   └── log_filter/
│       ├── cli.ex
│       ├── parser.ex
│       └── filter.ex
├── test/
│   └── log_filter/
│       ├── parser_test.exs
│       └── filter_test.exs
├── test/fixtures/
│   └── access.log
└── mix.exs
```

---

## Core concepts applied here

### Sigil `~r` — compiled regex

`~r/pattern/flags` returns a `%Regex{}` struct. The pattern is compiled at module
load time. Flags: `i` (case insensitive), `u` (Unicode), `s` (dot matches newlines),
`m` (multiline). Use `~r/.../` for simple patterns and `~r{...}` when the pattern
contains slashes — you avoid escaping.

### Sigil `~w` — word lists

`~w[get post put delete]a` produces `[:get, :post, :put, :delete]`. The modifier `a`
means atoms, `c` means charlists, default is strings. For fixed lists of known values
(HTTP methods, log levels, status families), `~w` is the idiomatic constructor.

### Named captures vs numbered captures

`Regex.named_captures/2` returns a `%{"ip" => ..., "status" => ...}` map. Named
captures make parsers readable and refactor-safe. Numbered captures (`Regex.run/2`)
return positional lists — fine for one-off parses, brittle for structured data.

---

## Implementation

### Step 1: Create the project

```bash
mix new log_filter
cd log_filter
mkdir -p test/fixtures
```

### Step 2: `mix.exs`

```elixir
defmodule LogFilter.MixProject do
  use Mix.Project

  def project do
    [
      app: :log_filter,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      escript: [main_module: LogFilter.CLI]
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps, do: []
end
```

### Step 3: `lib/log_filter/parser.ex`

```elixir
defmodule LogFilter.Parser do
  @moduledoc """
  Parses nginx combined log format lines into structured maps using regex
  with named captures.
  """

  # Compiled at module load time. If the pattern is invalid, compilation fails.
  @line_regex ~r/
    ^(?<ip>\d+\.\d+\.\d+\.\d+)       # client IP
    \s-\s-\s
    \[(?<timestamp>[^\]]+)\]          # [dd/Mon/yyyy:HH:MM:SS +0000]
    \s"(?<method>[A-Z]+)\s            # request method
    (?<path>[^\s]+)                   # request path
    \s[^"]+"                          # HTTP version (skipped)
    \s(?<status>\d{3})                # status code
    \s(?<bytes>\d+|-)                 # response size
  /x

  @valid_methods ~w[GET POST PUT PATCH DELETE HEAD OPTIONS]

  @spec parse_line(String.t()) :: {:ok, map()} | {:error, :invalid_format}
  def parse_line(line) when is_binary(line) do
    case Regex.named_captures(@line_regex, line) do
      nil ->
        {:error, :invalid_format}

      captures ->
        {:ok, normalize(captures)}
    end
  end

  @spec valid_method?(String.t()) :: boolean()
  def valid_method?(method), do: method in @valid_methods

  # Convert string captures into properly typed fields.
  # Done here to keep the rest of the pipeline type-safe.
  defp normalize(captures) do
    %{
      ip: captures["ip"],
      timestamp: captures["timestamp"],
      method: captures["method"],
      path: captures["path"],
      status: String.to_integer(captures["status"]),
      bytes: parse_bytes(captures["bytes"])
    }
  end

  defp parse_bytes("-"), do: 0
  defp parse_bytes(n), do: String.to_integer(n)
end
```

### Step 4: `lib/log_filter/filter.ex`

```elixir
defmodule LogFilter.Filter do
  @moduledoc """
  Applies status-range and IP-pattern filters to parsed log entries.
  """

  # Status families are expressed as ranges so a membership check is O(1).
  @families %{
    "1xx" => 100..199,
    "2xx" => 200..299,
    "3xx" => 300..399,
    "4xx" => 400..499,
    "5xx" => 500..599
  }

  @spec matches?(map(), keyword()) :: boolean()
  def matches?(entry, opts) do
    status_ok?(entry, opts[:status]) and ip_ok?(entry, opts[:ip])
  end

  defp status_ok?(_entry, nil), do: true

  defp status_ok?(entry, family) when is_map_key(@families, family) do
    entry.status in @families[family]
  end

  defp status_ok?(entry, exact) when is_binary(exact) do
    case Integer.parse(exact) do
      {n, ""} -> entry.status == n
      _ -> false
    end
  end

  defp ip_ok?(_entry, nil), do: true

  defp ip_ok?(entry, pattern) do
    # Convert a shell-style pattern ("10.0.0.*") into an anchored regex.
    # Escaping dots first avoids "10a0a0a1" matching "10.0.0.1".
    regex =
      pattern
      |> String.replace(".", "\\.")
      |> String.replace("*", ".*")
      |> then(&Regex.compile!("^#{&1}$"))

    Regex.match?(regex, entry.ip)
  end
end
```

### Step 5: `lib/log_filter/cli.ex`

```elixir
defmodule LogFilter.CLI do
  @moduledoc """
  Entry point for the log_filter escript.
  """

  alias LogFilter.{Parser, Filter}

  @spec main([String.t()]) :: :ok
  def main(argv) do
    case parse_args(argv) do
      {:run, path, opts} -> run(path, opts)
      :help -> print_help()
    end
  end

  defp parse_args(argv) do
    switches = [status: :string, ip: :string, help: :boolean]
    aliases = [s: :status, i: :ip, h: :help]

    case OptionParser.parse(argv, switches: switches, aliases: aliases) do
      {opts, [path], []} ->
        if opts[:help], do: :help, else: {:run, path, opts}

      _ ->
        :help
    end
  end

  defp run(path, opts) do
    stats =
      path
      |> File.stream!()
      |> Stream.map(&String.trim_trailing/1)
      |> Stream.map(&Parser.parse_line/1)
      |> Enum.reduce(initial_stats(), fn
        {:ok, entry}, acc -> accumulate(acc, entry, opts)
        {:error, _}, acc -> Map.update!(acc, :invalid, &(&1 + 1))
      end)

    print_summary(stats)
    :ok
  end

  defp initial_stats do
    %{total: 0, matched: 0, invalid: 0, unique_ips: MapSet.new()}
  end

  defp accumulate(acc, entry, opts) do
    acc = Map.update!(acc, :total, &(&1 + 1))

    if Filter.matches?(entry, opts) do
      IO.puts("#{entry.ip} #{entry.status} #{entry.method} #{entry.path}")

      acc
      |> Map.update!(:matched, &(&1 + 1))
      |> Map.update!(:unique_ips, &MapSet.put(&1, entry.ip))
    else
      acc
    end
  end

  defp print_summary(stats) do
    IO.puts(:stderr, """

    --- summary ---
    total lines:  #{stats.total}
    matched:      #{stats.matched}
    invalid:      #{stats.invalid}
    unique IPs:   #{MapSet.size(stats.unique_ips)}
    """)
  end

  defp print_help do
    IO.puts("""
    Usage: log_filter <file> [options]

      -s, --status  Status family (1xx|2xx|3xx|4xx|5xx) or exact code (e.g. 404)
      -i, --ip      IP pattern with * wildcards (e.g. 10.0.0.*)
      -h, --help    Show this help
    """)
  end
end
```

### Step 6: Test fixture

```
# test/fixtures/access.log
192.168.1.10 - - [12/Apr/2026:10:15:32 +0000] "GET /api/users HTTP/1.1" 200 1234 "-" "curl/8.0"
10.0.0.5 - - [12/Apr/2026:10:15:33 +0000] "POST /api/login HTTP/1.1" 503 89 "-" "Mozilla/5.0"
10.0.0.7 - - [12/Apr/2026:10:15:34 +0000] "GET /api/orders HTTP/1.1" 404 0 "-" "curl/8.0"
192.168.1.10 - - [12/Apr/2026:10:15:35 +0000] "DELETE /api/users/1 HTTP/1.1" 500 45 "-" "curl/8.0"
malformed line that will not parse
10.0.0.5 - - [12/Apr/2026:10:15:36 +0000] "GET / HTTP/1.1" 200 512 "-" "Mozilla/5.0"
```

### Step 7: Tests

```elixir
# test/log_filter/parser_test.exs
defmodule LogFilter.ParserTest do
  use ExUnit.Case, async: true

  alias LogFilter.Parser

  describe "parse_line/1" do
    test "parses a valid nginx log line" do
      line =
        ~s(192.168.1.10 - - [12/Apr/2026:10:15:32 +0000] "GET /api/users HTTP/1.1" 200 1234 "-" "curl/8.0")

      assert {:ok, entry} = Parser.parse_line(line)
      assert entry.ip == "192.168.1.10"
      assert entry.method == "GET"
      assert entry.path == "/api/users"
      assert entry.status == 200
      assert entry.bytes == 1234
    end

    test "parses a line with '-' as byte count" do
      line =
        ~s(10.0.0.1 - - [12/Apr/2026:10:15:32 +0000] "HEAD /ping HTTP/1.1" 204 - "-" "curl/8.0")

      assert {:ok, entry} = Parser.parse_line(line)
      assert entry.bytes == 0
    end

    test "rejects malformed lines" do
      assert {:error, :invalid_format} = Parser.parse_line("not a log line")
    end
  end

  describe "valid_method?/1" do
    test "accepts standard HTTP methods" do
      for m <- ~w[GET POST PUT PATCH DELETE HEAD OPTIONS] do
        assert Parser.valid_method?(m), "expected #{m} to be valid"
      end
    end

    test "rejects unknown methods" do
      refute Parser.valid_method?("CONNECT")
      refute Parser.valid_method?("get")
    end
  end
end
```

```elixir
# test/log_filter/filter_test.exs
defmodule LogFilter.FilterTest do
  use ExUnit.Case, async: true

  alias LogFilter.Filter

  @entry %{ip: "10.0.0.5", status: 503, method: "POST", path: "/x", bytes: 10}

  describe "status family filter" do
    test "matches 5xx family for a 503 response" do
      assert Filter.matches?(@entry, status: "5xx")
    end

    test "does not match 4xx family for a 503 response" do
      refute Filter.matches?(@entry, status: "4xx")
    end

    test "matches exact status code" do
      assert Filter.matches?(@entry, status: "503")
      refute Filter.matches?(@entry, status: "500")
    end
  end

  describe "IP pattern filter" do
    test "matches exact IP" do
      assert Filter.matches?(@entry, ip: "10.0.0.5")
    end

    test "matches wildcard pattern" do
      assert Filter.matches?(@entry, ip: "10.0.0.*")
      assert Filter.matches?(@entry, ip: "10.*")
    end

    test "does not match different subnet" do
      refute Filter.matches?(@entry, ip: "192.168.*")
    end

    test "does not allow dot to match literal dot collision" do
      # "10.0.0.5" must NOT match "10X0X0X5" with dot-as-any
      refute Filter.matches?(%{@entry | ip: "10X0X0X5"}, ip: "10.0.0.5")
    end
  end

  describe "combined filters" do
    test "all filters must pass (AND semantics)" do
      assert Filter.matches?(@entry, status: "5xx", ip: "10.*")
      refute Filter.matches?(@entry, status: "2xx", ip: "10.*")
    end

    test "no filters means match everything" do
      assert Filter.matches?(@entry, [])
    end
  end
end
```

### Step 8: Run and verify

```bash
mix deps.get
mix compile --warnings-as-errors
mix test --trace
mix escript.build
./log_filter test/fixtures/access.log --status 5xx
./log_filter test/fixtures/access.log --ip "10.0.0.*"
```

---

## Trade-off analysis

| Aspect                   | Regex with named captures (this)   | String.split / Enum.at               |
|--------------------------|------------------------------------|--------------------------------------|
| Readability              | self-documenting via names         | magic indices, brittle               |
| Performance              | ~2-5 µs per line                   | faster but incorrect for quoted data |
| Handles malformed input  | returns `{:error, :invalid_format}`| silently produces wrong fields       |
| Refactoring cost         | low (rename in pattern only)       | high (index shifts break everything) |

For throughput > 100k lines/s consider NimbleParsec or a DFA. For SRE tooling on
millions of lines per night, this regex approach is plenty fast.

---

## Common production mistakes

**1. Recompiling regex per call**
`Regex.compile!("^\\d+$")` inside a hot function recompiles on every invocation.
Always assign to a module attribute (`@line_regex ~r/.../`). Sigils in attributes
compile once.

**2. Unescaped dots in IP patterns**
`"10.0.0.5"` as a regex pattern means "10, any char, 0, any char, 0, any char, 5".
Replace `.` with `\\.` before compiling any user-supplied pattern that is not
already a regex.

**3. Using `Regex.run/2` for more than 2-3 captures**
Positional captures are fine for `~r/(\w+):(\w+)/` but become unreadable at 5+
groups. Switch to `Regex.named_captures/2` — your future self will thank you.

**4. Greedy matching across lines**
`.*` does not match newlines by default. If your input is multiline and you want
greedy across lines, add the `s` flag (`~r/.../s`). If you forget, patterns silently
return `nil` on multiline data.

**5. `String.to_atom/1` on untrusted input**
Never convert user-provided strings (HTTP methods from a log) to atoms dynamically.
The atom table is not garbage collected and fills up. Use `~w[...]a` for known
values and keep user input as strings.

---

## When NOT to use regex here

- If you control the log format end-to-end, emit structured JSON logs and decode
  with `Jason.decode!/1`. It is faster, safer, and self-describing.
- For deeply nested or recursive grammars (e.g. SQL, JSON itself), use a parser
  combinator library like NimbleParsec. Regex is not Turing-complete and will
  eventually lose.
- For fixed-position binary formats, use pattern matching on binaries
  (`<<header::binary-size(4), rest::binary>>`) instead of regex.

---

## Resources

- [Regex module — HexDocs](https://hexdocs.pm/elixir/Regex.html)
- [Sigils — Elixir Getting Started](https://hexdocs.pm/elixir/sigils.html)
- [PCRE pattern syntax](https://www.pcre.org/original/doc/html/pcrepattern.html) — Elixir's regex engine
- [NimbleParsec](https://hexdocs.pm/nimble_parsec/NimbleParsec.html) — when regex is not enough
