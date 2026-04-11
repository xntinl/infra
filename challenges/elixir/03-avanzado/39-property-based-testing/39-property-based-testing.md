# Property-Based Testing with StreamData

**Project**: `api_gateway` — a standalone HTTP gateway exercise

---

## Project context

You are building `api_gateway`, an HTTP gateway that routes traffic to microservices. The
gateway has two modules with non-trivial pure logic: a binary parser for HTTP request lines
and a topic matcher for the event bus. Both are good candidates for property-based testing:
the parser must produce consistent results for all valid inputs, and the topic matcher has
rules that hold for all possible topic strings.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── middleware/
│       │   └── parser.ex            # HTTP request line parser (defined below)
│       └── event_bus/
│           └── topic_matcher.ex     # wildcard topic matcher (defined below)
├── test/
│   └── api_gateway/
│       ├── middleware/
│       │   └── parser_properties_test.exs   # given tests
│       └── event_bus/
│           └── topic_matcher_properties_test.exs
└── mix.exs
```

---

## The business problem

Unit tests check specific examples. Properties check invariants:

- **Parser roundtrip**: for any valid HTTP request line, `parse(serialize(request)) == request`
- **Topic matcher completeness**: for any topic `"a.b.c"`, the pattern `"a.*.*"` matches it
- **Token bucket monotonicity**: after N allowed requests, the N+1th is denied (for any N <= capacity)

These invariants are hard to verify exhaustively with hand-written examples. StreamData
generates hundreds of cases and shrinks failures to their minimal reproducing input.

---

## Algorithm: property-based testing in three steps

```
1. Define a generator: what inputs are valid?
2. Write a property: what must always be true for those inputs?
3. Run check all: StreamData generates 100 cases, tries to falsify the property
   If falsified: shrinks to minimal failing case and reports it
```

---

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    {:stream_data, "~> 0.6"}
  ]
end
```

### Step 2: Topic matcher module

This module handles `*` (single-segment wildcard), `#` (multi-segment wildcard),
and literal segments. It is used by the property tests below.

```elixir
defmodule ApiGateway.EventBus.TopicMatcher do
  @moduledoc "Wildcard topic matching for the event bus."

  @type compiled :: list(:single | :multi | String.t())

  @doc """
  Compile a pattern to a list of segment tokens.
  Call once at subscribe time, not on every publish.
  """
  @spec compile(String.t()) :: compiled()
  def compile(pattern) do
    pattern
    |> String.split(".")
    |> Enum.map(fn
      "*" -> :single
      "#" -> :multi
      seg -> seg
    end)
  end

  @doc "Return true if `topic` matches the compiled pattern."
  @spec matches?(compiled(), String.t()) :: boolean()
  def matches?(compiled_pattern, topic) do
    segments = String.split(topic, ".")
    do_match(compiled_pattern, segments)
  end

  defp do_match([], []),               do: true
  defp do_match([:multi | _], _),      do: true
  defp do_match([:single | rp], [_ | rt]), do: do_match(rp, rt)
  defp do_match([seg | rp], [seg | rt]),   do: do_match(rp, rt)
  defp do_match(_, _),                 do: false
end
```

### Step 3: Given tests — must pass without modification

The parser properties test uses custom generators for HTTP methods, paths, and versions.
Each generator produces only valid values, so every generated request line is well-formed.
The properties verify invariants that hold for all valid inputs.

```elixir
# test/api_gateway/middleware/parser_properties_test.exs
defmodule ApiGateway.Middleware.ParserPropertiesTest do
  use ExUnit.Case, async: true
  use ExUnitProperties

  @moduledoc """
  Properties for ApiGateway.Middleware.Parser.
  Focuses on invariants that hold for all valid HTTP request lines.
  """

  # Generator for valid HTTP methods
  defp gen_method do
    one_of([
      constant("GET"),
      constant("POST"),
      constant("PUT"),
      constant("DELETE"),
      constant("PATCH")
    ])
  end

  # Generator for valid URL paths (no spaces, starts with /)
  defp gen_path do
    ExUnitProperties.gen all segment_count <- integer(1..5),
                             segments <- list_of(
                               string(:alphanumeric, min_length: 1, max_length: 10),
                               length: segment_count
                             ) do
      "/" <> Enum.join(segments, "/")
    end
  end

  # Generator for valid HTTP versions
  defp gen_version do
    one_of([constant("HTTP/1.0"), constant("HTTP/1.1"), constant("HTTP/2.0")])
  end

  # Generator for a complete valid request line: "METHOD /path HTTP/version"
  defp gen_request_line do
    ExUnitProperties.gen all method  <- gen_method(),
                             path    <- gen_path(),
                             version <- gen_version() do
      "#{method} #{path} #{version}"
    end
  end

  property "parse: method is always one of the known HTTP methods" do
    check all line <- gen_request_line() do
      [method | _] = String.split(line, " ", parts: 3)
      assert method in ~w(GET POST PUT DELETE PATCH)
    end
  end

  property "parse: path always starts with /" do
    check all line <- gen_request_line() do
      [_method, path | _] = String.split(line, " ", parts: 3)
      assert String.starts_with?(path, "/")
    end
  end

  property "parse: version always matches HTTP/N.N format" do
    check all line <- gen_request_line() do
      parts = String.split(line, " ", parts: 3)
      version = List.last(parts)
      assert String.match?(version, ~r/^HTTP\/\d+\.\d+$/)
    end
  end

  property "roundtrip: parse(serialize(req)) == req" do
    check all method  <- gen_method(),
              path    <- gen_path(),
              version <- gen_version() do
      original = %{method: method, path: path, version: version}
      serialized = "#{method} #{path} #{version}"

      [m, p, v] = String.split(serialized, " ", parts: 3)
      parsed = %{method: m, path: p, version: v}

      assert parsed == original
    end
  end

  property "parse: splitting on space always yields at least 3 parts for valid lines" do
    check all line <- gen_request_line() do
      parts = String.split(line, " ", parts: 3)
      assert length(parts) == 3
    end
  end
end
```

The topic matcher properties test verifies wildcard matching invariants. The `#` wildcard
must match every topic. The `*` wildcard must match exactly one segment. Literal patterns
must match themselves and no other topic.

```elixir
# test/api_gateway/event_bus/topic_matcher_properties_test.exs
defmodule ApiGateway.EventBus.TopicMatcherPropertiesTest do
  use ExUnit.Case, async: true
  use ExUnitProperties

  alias ApiGateway.EventBus.TopicMatcher

  # Generator for a single topic segment (no dots, no wildcards)
  defp gen_segment do
    string(:alphanumeric, min_length: 1, max_length: 10)
  end

  # Generator for a complete topic: "seg1.seg2.seg3"
  defp gen_topic(min_depth \\ 1, max_depth \\ 4) do
    ExUnitProperties.gen all depth    <- integer(min_depth..max_depth),
                             segments <- list_of(gen_segment(), length: depth) do
      Enum.join(segments, ".")
    end
  end

  property "#' pattern matches every topic" do
    check all topic <- gen_topic() do
      assert TopicMatcher.matches?(TopicMatcher.compile("#"), topic)
    end
  end

  property "exact pattern matches itself and nothing else (different depth)" do
    check all topic <- gen_topic(2, 4) do
      compiled = TopicMatcher.compile(topic)
      assert TopicMatcher.matches?(compiled, topic)
    end
  end

  property "'*' at any position matches exactly one segment" do
    check all prefix_segs <- list_of(gen_segment(), min_length: 0, max_length: 2),
              suffix_segs <- list_of(gen_segment(), min_length: 0, max_length: 2),
              wild_seg    <- gen_segment() do
      topic_segs   = prefix_segs ++ [wild_seg] ++ suffix_segs
      topic        = Enum.join(topic_segs, ".")

      pattern_segs = prefix_segs ++ ["*"] ++ suffix_segs
      pattern      = Enum.join(pattern_segs, ".")

      assert TopicMatcher.matches?(TopicMatcher.compile(pattern), topic)
    end
  end

  property "'*' does NOT match zero segments" do
    check all prefix <- gen_topic(1, 3) do
      pattern = prefix <> ".*"
      refute TopicMatcher.matches?(TopicMatcher.compile(pattern), prefix)
    end
  end

  property "compile is deterministic: same pattern always gives same result" do
    check all topic <- gen_topic() do
      compiled1 = TopicMatcher.compile(topic)
      compiled2 = TopicMatcher.compile(topic)
      assert compiled1 == compiled2
    end
  end

  property "a topic matches itself as a literal pattern" do
    check all topic <- gen_topic() do
      assert TopicMatcher.matches?(TopicMatcher.compile(topic), topic)
    end
  end

  property "two different literal topics do not match each other" do
    check all seg1 <- gen_segment(),
              seg2 <- gen_segment(),
              seg1 != seg2 do
      refute TopicMatcher.matches?(TopicMatcher.compile(seg1), seg2)
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/middleware/parser_properties_test.exs --trace
mix test test/api_gateway/event_bus/topic_matcher_properties_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Property-based test | Example-based unit test |
|--------|--------------------|-----------------------|
| Edge case discovery | Automatic (hundreds of cases) | Manual (you think of them) |
| Regression precision | Properties hold for all inputs | Specific inputs regression |
| Readability | Higher abstraction | More concrete |
| Debugging | Shrunk minimal case | Exact failing input |
| Best for | Invariants, roundtrips, pure functions | Business rules, error messages, UI |

| Generator combinator | Use when |
|---------------------|---------|
| `map/2` | Transform output of a generator |
| `filter/2` | Exclude values — use sparingly (> 90% pass rate needed) |
| `gen all ... do` | Compose dependent generators (later values depend on earlier) |
| `one_of/1` | Choose from a fixed set of options |
| `list_of/2` | Generate variable-length lists with optional `min_length`, `max_length` |

---

## Common production mistakes

**1. Writing trivially true properties**
`assert is_list(Enum.sort(list))` is always true and tests nothing. A property must be
falsifiable — ask yourself: "what code change would make this property fail?"

**2. Using `filter/2` aggressively**
StreamData discards filtered values. If `filter/2` discards 80% of generated values,
the test needs 5x more time to reach the configured `max_runs`. Use `map/2` instead:
generate exactly what you need rather than generating broadly and throwing most away.

**3. Properties with side effects**
`check all` runs 100 iterations. If each iteration inserts into a database, your test
writes 100 rows. Properties should test pure functions or use per-iteration cleanup.

**4. Ignoring the shrunk failure case**
When a property fails, StreamData reports the original failing case AND the minimal
shrunk case. The shrunk case is the actionable one — it is the smallest input that
still triggers the bug. Always read the shrunk case, not the original.

**5. Properties that duplicate unit tests**
Do not write `property "sort([1,2,3]) == [1,2,3]"`. Write
`property "for any list, sort is idempotent"`. Properties complement unit tests by
covering the infinite space between explicit examples.

---

## Resources

- [StreamData — HexDocs](https://hexdocs.pm/stream_data/StreamData.html)
- [ExUnitProperties — HexDocs](https://hexdocs.pm/stream_data/ExUnitProperties.html)
- [Property-Based Testing with PropEr, Erlang, and Elixir — Fred Hebert & Laura Castro](https://pragprog.com/titles/fhproper/property-based-testing-with-proper-erlang-and-elixir/)
- [The Anatomy of a Property — Fred Hebert](https://ferd.ca/the-anatomy-of-a-property.html)
