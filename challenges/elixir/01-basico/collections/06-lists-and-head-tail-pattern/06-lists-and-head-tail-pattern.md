# Lists and Head/Tail: Building a Markdown-to-HTML Converter

**Project**: `md_lite` — a subset markdown-to-HTML converter using recursive list processing

---

## Why lists work differently in Elixir

Elixir lists are linked lists, not arrays. This has fundamental performance
implications that surprise developers coming from Python, Java, or Go:

- **Prepend is O(1)**: `[new | list]` creates a new head pointing to the existing list.
  No copying.
- **Append is O(n)**: `list ++ [new]` must traverse and copy the entire list to add
  one element at the end.
- **Length is O(n)**: `length(list)` traverses the entire list. There is no cached
  size field.
- **Random access is O(n)**: `Enum.at(list, 5)` walks 5 nodes.

This makes lists ideal for sequential processing (head/tail recursion) and terrible
for random access. The pattern `[head | tail]` destructures a list into its first
element and the rest — this is the fundamental building block for recursive list
processing.

```elixir
iex> [head | tail] = [1, 2, 3, 4]
iex> head
1
iex> tail
[2, 3, 4]

iex> [a, b | rest] = [1, 2, 3, 4]
iex> {a, b, rest}
{1, 2, [3, 4]}
```

---

## The business problem

Build a simple markdown-to-HTML converter that handles:

1. Headings (`# H1`, `## H2`, `### H3`)
2. Paragraphs (plain text lines)
3. Unordered lists (`- item`)
4. Code blocks (lines between `` ``` `` fences)
5. Bold (`**text**`) and inline code (`` `code` ``)

Process the input as a list of lines, using head/tail recursion to handle
multi-line constructs (like code blocks) that require state across lines.

---

## Project structure

```
md_lite/
├── lib/
│   └── md_lite.ex
├── test/
│   └── md_lite_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — build the output with `list ++ [chunk]` (append as you go)**
- Pros: reads in natural order; no final reverse; matches how many developers coming from Python/JS first think about the problem.
- Cons: each `++` copies the entire accumulator — O(n²) over the whole document; visible slowness on long markdown inputs.

**Option B — prepend to accumulator, reverse once at the end** (chosen)
- Pros: O(n) total; idiomatic in Elixir and OTP; each recursive step stays O(1); the final `Enum.reverse/1` is explicit and cheap.
- Cons: the accumulator is "backwards" during processing, which can confuse readers unfamiliar with the pattern; you must remember the reverse.

Chose **B** because even a moderate-size markdown file (a few thousand lines) exposes the O(n²) cliff of option A — and the idiom is universal in Elixir list processing, so learning it pays off well beyond this exercise.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```


### `lib/md_lite.ex`

```elixir
defmodule MdLite do
  @moduledoc """
  Converts a subset of Markdown to HTML using recursive list processing.

  Demonstrates head/tail pattern matching, accumulator-based recursion,
  and the difference between prepend (O(1)) and append (O(n)) on lists.
  """

  @doc """
  Converts a markdown string to HTML.

  ## Examples

      iex> MdLite.to_html("# Hello")
      "<h1>Hello</h1>"

      iex> MdLite.to_html("## World")
      "<h2>World</h2>"

      iex> MdLite.to_html("- apple\\n- banana")
      "<ul>\\n<li>apple</li>\\n<li>banana</li>\\n</ul>"

  """
  @spec to_html(String.t()) :: String.t()
  def to_html(markdown) when is_binary(markdown) do
    markdown
    |> String.split("\n")
    |> process_lines([])
    |> Enum.reverse()
    |> Enum.join("\n")
  end

  @doc """
  Processes a list of markdown lines into HTML lines using head/tail recursion.

  Uses an accumulator (prepend-based) for O(1) per line. The result is
  reversed at the end — the standard pattern for building lists efficiently.

  This function is public for educational purposes. In production, use to_html/1.
  """
  @spec process_lines([String.t()], [String.t()]) :: [String.t()]
  def process_lines([], acc), do: acc

  # Code block — consume lines until closing fence
  def process_lines(["```" <> _lang | rest], acc) do
    {code_lines, remaining} = consume_code_block(rest, [])
    code_html = "<pre><code>#{Enum.join(code_lines, "\n")}</code></pre>"
    process_lines(remaining, [code_html | acc])
  end

  # Headings — match leading # characters
  def process_lines(["### " <> text | rest], acc) do
    process_lines(rest, ["<h3>#{inline(text)}</h3>" | acc])
  end

  def process_lines(["## " <> text | rest], acc) do
    process_lines(rest, ["<h2>#{inline(text)}</h2>" | acc])
  end

  def process_lines(["# " <> text | rest], acc) do
    process_lines(rest, ["<h1>#{inline(text)}</h1>" | acc])
  end

  # Unordered list — consume consecutive "- " lines
  def process_lines(["- " <> _ | _] = lines, acc) do
    {list_items, remaining} = consume_list_items(lines, [])

    items_html =
      list_items
      |> Enum.map(fn item -> "<li>#{inline(item)}</li>" end)
      |> Enum.join("\n")

    list_html = "<ul>\n#{items_html}\n</ul>"
    process_lines(remaining, [list_html | acc])
  end

  # Empty line — skip
  def process_lines(["" | rest], acc) do
    process_lines(rest, acc)
  end

  # Paragraph — any other text
  def process_lines([line | rest], acc) do
    process_lines(rest, ["<p>#{inline(line)}</p>" | acc])
  end

  @doc """
  Processes inline markdown formatting: bold and inline code.

  ## Examples

      iex> MdLite.inline("This is **bold** text")
      "This is <strong>bold</strong> text"

      iex> MdLite.inline("Use `Enum.map/2` here")
      "Use <code>Enum.map/2</code> here"

  """
  @spec inline(String.t()) :: String.t()
  def inline(text) do
    text
    |> replace_inline_code()
    |> replace_bold()
  end

  # --- Private functions ---

  @spec consume_code_block([String.t()], [String.t()]) ::
          {[String.t()], [String.t()]}
  defp consume_code_block([], acc), do: {Enum.reverse(acc), []}

  defp consume_code_block(["```" | rest], acc) do
    {Enum.reverse(acc), rest}
  end

  defp consume_code_block([line | rest], acc) do
    escaped = html_escape(line)
    consume_code_block(rest, [escaped | acc])
  end

  @spec consume_list_items([String.t()], [String.t()]) ::
          {[String.t()], [String.t()]}
  defp consume_list_items(["- " <> item | rest], acc) do
    consume_list_items(rest, [item | acc])
  end

  defp consume_list_items(remaining, acc) do
    {Enum.reverse(acc), remaining}
  end

  @spec replace_inline_code(String.t()) :: String.t()
  defp replace_inline_code(text) do
    Regex.replace(~r/`([^`]+)`/, text, "<code>\\1</code>")
  end

  @spec replace_bold(String.t()) :: String.t()
  defp replace_bold(text) do
    Regex.replace(~r/\*\*([^*]+)\*\*/, text, "<strong>\\1</strong>")
  end

  @spec html_escape(String.t()) :: String.t()
  defp html_escape(text) do
    text
    |> String.replace("&", "&amp;")
    |> String.replace("<", "&lt;")
    |> String.replace(">", "&gt;")
  end
end
```

### Why this works

- `process_lines/2` is tail-recursive with an accumulator. Each call prepends to
  `acc` (O(1)) and passes the tail of the input list. The result is reversed once
  at the end in `to_html/1`.
- Code blocks and list items use helper functions that "consume" lines from the
  input until a terminator is found. They return both the consumed content and the
  remaining lines. This is a common pattern for stateful parsing with immutable data.
- `consume_code_block/2` stops at the closing `` ``` `` fence and returns the rest
  of the input for further processing. If no fence is found (end of input), it
  treats all remaining lines as code.
- The heading clauses are ordered `###`, `##`, `#` — most specific first. This is
  critical because `"## Title"` would match `"# " <> rest` (with `rest = "# Title"`).

### Tests

```elixir
# test/md_lite_test.exs
defmodule MdLiteTest do
  use ExUnit.Case, async: true

  doctest MdLite

  describe "headings" do
    test "converts h1" do
      assert MdLite.to_html("# Title") == "<h1>Title</h1>"
    end

    test "converts h2" do
      assert MdLite.to_html("## Subtitle") == "<h2>Subtitle</h2>"
    end

    test "converts h3" do
      assert MdLite.to_html("### Section") == "<h3>Section</h3>"
    end
  end

  describe "paragraphs" do
    test "wraps plain text in <p>" do
      assert MdLite.to_html("Hello world") == "<p>Hello world</p>"
    end

    test "handles multiple paragraphs" do
      input = "First paragraph\n\nSecond paragraph"
      result = MdLite.to_html(input)
      assert result =~ "<p>First paragraph</p>"
      assert result =~ "<p>Second paragraph</p>"
    end
  end

  describe "unordered lists" do
    test "converts list items" do
      input = "- apple\n- banana\n- cherry"
      result = MdLite.to_html(input)
      assert result =~ "<ul>"
      assert result =~ "<li>apple</li>"
      assert result =~ "<li>banana</li>"
      assert result =~ "<li>cherry</li>"
      assert result =~ "</ul>"
    end

    test "text after list is not in the list" do
      input = "- item\nParagraph after"
      result = MdLite.to_html(input)
      assert result =~ "</ul>"
      assert result =~ "<p>Paragraph after</p>"
    end
  end

  describe "code blocks" do
    test "wraps code in pre/code tags" do
      input = "```elixir\nEnum.map(list, &fun/1)\n```"
      result = MdLite.to_html(input)
      assert result =~ "<pre><code>"
      assert result =~ "Enum.map(list, &amp;fun/1)"
      assert result =~ "</code></pre>"
    end

    test "escapes HTML inside code blocks" do
      input = "```\n<script>alert('xss')</script>\n```"
      result = MdLite.to_html(input)
      assert result =~ "&lt;script&gt;"
      refute result =~ "<script>"
    end
  end

  describe "inline formatting" do
    test "converts bold" do
      assert MdLite.inline("This is **bold** text") ==
               "This is <strong>bold</strong> text"
    end

    test "converts inline code" do
      assert MdLite.inline("Use `Enum.map/2` here") ==
               "Use <code>Enum.map/2</code> here"
    end

    test "handles multiple bold in one line" do
      result = MdLite.inline("**one** and **two**")
      assert result == "<strong>one</strong> and <strong>two</strong>"
    end
  end

  describe "combined document" do
    test "processes a full markdown document" do
      input = """
      # My Document

      This is a paragraph with **bold** text.

      ## List Section

      - first item
      - second item

      ```
      code_here()
      ```

      Final paragraph.
      """

      result = MdLite.to_html(String.trim(input))
      assert result =~ "<h1>My Document</h1>"
      assert result =~ "<strong>bold</strong>"
      assert result =~ "<h2>List Section</h2>"
      assert result =~ "<li>first item</li>"
      assert result =~ "<pre><code>"
      assert result =~ "<p>Final paragraph.</p>"
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---



---
## Key Concepts

### 1. Lists Are Singly Linked, Head-Prepended

A list is a chain of cells: `[H | T]` where `H` is the head and `T` is the tail. Prepending is O(1), but appending is O(n). Always prepend to lists in loops. If you find yourself appending, use `Enum.reduce` to build the list in reverse, then `Enum.reverse/1`.

### 2. Pattern Matching on Head/Tail is Idiomatic

This pattern is fundamental—many Enum functions are built on it. Every recursive list function terminates with a base case for `[]`. If you forget it, you get a `FunctionClauseError`.

### 3. O(n) vs O(1) Operations Matter at Scale

In a real-time system processing 10,000 events/second, choosing between O(1) prepend and O(n) append determines whether you keep up. Always be aware of the cost of list operations.

---
## Benchmark

```elixir
# bench.exs — compare O(n) prepend+reverse vs O(n²) append
defmodule Bench do
  def prepend_reverse(n) do
    Enum.reduce(1..n, [], fn x, acc -> [x | acc] end) |> Enum.reverse()
  end

  def append(n) do
    Enum.reduce(1..n, [], fn x, acc -> acc ++ [x] end)
  end

  def run do
    {t_prepend, _} = :timer.tc(fn -> prepend_reverse(10_000) end)
    {t_append, _} = :timer.tc(fn -> append(10_000) end)

    IO.puts("prepend+reverse 10k: #{t_prepend} µs")
    IO.puts("append          10k: #{t_append} µs  (expect ~100–1000× slower)")

    # Also measure MdLite on a realistic document
    doc = String.duplicate("# Heading\n\nA paragraph with **bold**.\n\n", 500)
    {t_md, _} = :timer.tc(fn -> MdLite.to_html(doc) end)
    IO.puts("MdLite.to_html 2000 lines: #{t_md} µs")
  end
end

Bench.run()
```

Target: `prepend_reverse(10_000)` well under 2 ms; `append(10_000)` typically 100–1000× slower on modern hardware — that ratio IS the lesson. For the converter itself, expect <30 ms for a 2000-line document.

---

## Prepend vs append performance

This is the single most important list performance fact in Elixir:

```elixir
# O(1) — creates a new node pointing to existing list
[new_element | existing_list]

# O(n) — must traverse and copy the entire list
existing_list ++ [new_element]
```

The idiomatic pattern is:

1. Build your list by prepending (O(1) per element)
2. Reverse once at the end (O(n) total)

Total cost: O(n). If you appended each element: O(n^2).

```elixir
# Bad — O(n²) total
Enum.reduce(1..10_000, [], fn x, acc -> acc ++ [x] end)

# Good — O(n) total
1..10_000 |> Enum.reduce([], fn x, acc -> [x | acc] end) |> Enum.reverse()
```

---

## Common production mistakes

**1. Using `++` in a loop**
Every `list ++ [element]` copies the entire list. For 10,000 elements, that is
~50 million copy operations. Always prepend and reverse.

**2. Checking `length(list) > 0` instead of pattern matching**
`length(list)` is O(n). `list != []` or `match?([_ | _], list)` is O(1).

**3. Using `Enum.at/2` for sequential access**
If you need `Enum.at(list, 0)`, `Enum.at(list, 1)`, etc., you are using a list
as an array. Either destructure with `[a, b | _] = list` or use a different
data structure.

**4. Forgetting that `[head | tail]` does not match empty lists**
`[h | t] = []` raises `MatchError`. Always handle the `[]` base case first
in recursive functions.

**5. Deeply nested `[head | [second | rest]]` matching**
While valid, deep nesting is hard to read. Prefer `[first, second | rest]`
which is equivalent and clearer.

---

## Reflection

1. Your markdown converter processes one line at a time. A new requirement arrives: tables (`| a | b |`) that span variable row counts. Would you add another `consume_*` helper, or switch to a two-pass parser (first tokenize, then assemble)? What does each choice cost in terms of state and readability?
2. If the input came as a `Stream` instead of a fully loaded string (e.g. a large markdown file read line-by-line), what changes in the recursion? Would you still reverse at the end, or can you emit HTML lazily?

---

## Resources

- [Lists — Elixir Getting Started](https://elixir-lang.org/getting-started/basic-types.html#linked-lists)
- [Enum — HexDocs](https://hexdocs.pm/elixir/Enum.html)
- [Recursion — Elixir Getting Started](https://elixir-lang.org/getting-started/recursion.html)
- [Erlang efficiency guide — lists](https://www.erlang.org/doc/efficiency_guide/listhandling.html)
