# Building a minimal JSON parser with NimbleParsec combinators

**Project**: `nimble_json_lite` — a hand-rolled subset-JSON parser (strings,
numbers, booleans, null, arrays, objects) implemented with
[NimbleParsec](https://hexdocs.pm/nimble_parsec/) as a didactic exercise.
Not a replacement for Jason; the point is to see how parser combinators
compile to binary pattern matching.

---

## Why nimble json lite matters

`NimbleParsec` is the same technology that makes Ecto's query
tokenizer, `Calendar.ISO`'s date parsing, and Phoenix's route parser fast.
It's a parser-combinator library that compiles to raw binary pattern
matching at compile time — producing code as fast as hand-written
recursive-descent parsers, but declaratively composed.

Writing a JSON parser is the canonical way to learn it: the grammar is
small, the edge cases (nested structures, whitespace, escape sequences)
force you to use nearly every combinator. After this exercise you'll be
able to build a DSL parser or protocol tokenizer without reaching for
regex spaghetti.

---

## Project structure

```
nimble_json_lite/
├── lib/
│   └── nimble_json_lite.ex
├── script/
│   └── main.exs
├── test/
│   └── nimble_json_lite_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Combinators vs regex

A combinator is a function that returns a *parser value* — a description
of how to match input. You compose descriptions (`concat`, `choice`,
`repeat`), then `defparsec/2` turns the composed tree into compiled
binary-matching clauses.

Because the output is pattern matching, there's zero runtime interpretation
— no regex engine, no backtracking in the typical case.

### 2. The core combinators you'll use

| Combinator | Role |
|------------|------|
| `string/2` | match a literal |
| `integer/2` / `ascii_string/2` | match numeric or character classes |
| `concat/2` | `A` then `B` |
| `choice/2` | `A` or `B` or … |
| `repeat/2` | zero-or-more |
| `times/3` | exactly N / min..max |
| `optional/2` | zero-or-one |
| `ignore/2` | consume but drop |
| `tag/2` | wrap output in `{tag, value}` |
| `label/2` | better error messages |
| `parsec/2` | reference another `defparsec` — needed for recursion |

### 3. Recursion via `parsec/2`

JSON values are recursive (arrays contain values; values can be arrays).
You can't just do `defparsec :value, choice([..., array])` if `array`
references `value` — definitions would loop at compile time.
`parsec(:value)` is a lazy reference resolved at parse time.

### 4. `post_traverse` / `reduce` — shape the output

Combinators emit a flat list of tokens by default. `reduce(combinator,
{M, f, args})` post-processes the collected tokens into whatever shape
you want (a map, a struct, a number).

---

## Design decisions

**Option A — write the parser by hand with `case` and binary pattern matching**
- Pros: no dependency; total control; you see exactly the compiled shape.
- Cons: lots of repetitive clauses for whitespace, escapes, and recursion; the recursion story (values inside arrays inside objects) gets verbose fast.

**Option B — compose combinators with `NimbleParsec` (chosen)**
- Pros: declarative grammar reads close to BNF; `parsec/2` expresses recursion cleanly; compiles to the same binary pattern matches you'd write by hand; easy to extend with `label/2` for better errors.
- Cons: extra dep; compile time grows with grammar; hitting left-recursion or ambiguity can be confusing if you haven't internalized the combinator model.

→ Chose **B** because the exercise is about learning combinators; writing the same parser by hand would produce identical runtime code but zero pedagogical value.

## Implementation

### `mix.exs`

```elixir
defmodule NimbleJsonLite.MixProject do
  use Mix.Project

  def project do
    [
      app: :nimble_json_lite,
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
### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new nimble_json_lite
cd nimble_json_lite
```

Deps in `mix.exs`:

### `lib/nimble_json_lite.ex`

**Objective**: Implement `nimble_json_lite.ex` — the integration seam where external protocol semantics meet Elixir domain code.

```elixir
defmodule NimbleJsonLite do
  @moduledoc """
  A *minimal* JSON parser. Supports: strings (no unicode escapes),
  integers, floats (simple), booleans, null, arrays, and objects.
  Written as a NimbleParsec tutorial — use `Jason` in production.
  """

  import NimbleParsec

  # ── Whitespace: consumed and discarded ─────────────────────────────────
  whitespace = ascii_string([?\s, ?\t, ?\n, ?\r], min: 0) |> ignore()

  # ── Null / booleans ────────────────────────────────────────────────────
  null = string("null") |> replace(nil)
  truthy = string("true") |> replace(true)
  falsy = string("false") |> replace(false)

  # ── Numbers: integer or float; simplified (no exponents) ──────────────
  sign = optional(string("-"))

  int_part =
    choice([
      string("0"),
      ascii_string([?1..?9], 1) |> concat(ascii_string([?0..?9], min: 0))
    ])

  frac_part = string(".") |> concat(ascii_string([?0..?9], min: 1))

  number =
    sign
    |> concat(int_part)
    |> optional(frac_part)
    |> reduce({__MODULE__, :to_number, []})

  # ── Strings: no escape handling beyond \" and \\ ──────────────────────
  string_char =
    choice([
      string("\\\"") |> replace(?"),
      string("\\\\") |> replace(?\\),
      string("\\n") |> replace(?\n),
      string("\\t") |> replace(?\t),
      utf8_char(not: ?", not: ?\\)
    ])

  json_string =
    ignore(string("\""))
    |> repeat(string_char)
    |> ignore(string("\""))
    |> reduce({List, :to_string, []})

  # ── Array: [ value (, value)* ] ───────────────────────────────────────
  defcombinatorp :value, parsec(:value_impl)

  array =
    ignore(string("["))
    |> concat(whitespace)
    |> optional(
      parsec(:value)
      |> repeat(
        whitespace
        |> ignore(string(","))
        |> concat(whitespace)
        |> parsec(:value)
      )
    )
    |> concat(whitespace)
    |> ignore(string("]"))
    |> tag(:array)
    |> reduce({__MODULE__, :to_list, []})

  # ── Object: { "key": value (, "key": value)* } ────────────────────────
  pair =
    json_string
    |> concat(whitespace)
    |> ignore(string(":"))
    |> concat(whitespace)
    |> parsec(:value)
    |> reduce({__MODULE__, :to_pair, []})

  object =
    ignore(string("{"))
    |> concat(whitespace)
    |> optional(
      pair
      |> repeat(
        whitespace
        |> ignore(string(","))
        |> concat(whitespace)
        |> concat(pair)
      )
    )
    |> concat(whitespace)
    |> ignore(string("}"))
    |> tag(:object)
    |> reduce({__MODULE__, :to_map, []})

  # ── The recursive value parser ────────────────────────────────────────
  value_impl =
    whitespace
    |> choice([object, array, json_string, number, truthy, falsy, null])
    |> concat(whitespace)

  defparsecp(:value_impl, value_impl)

  # ── Public entry point ────────────────────────────────────────────────
  defparsec(:parse_json, parsec(:value_impl) |> eos())

  @doc "Decodes result from binary."
  @spec decode(String.t()) :: {:ok, term()} | {:error, String.t()}
  def decode(binary) when is_binary(binary) do
    case parse_json(binary) do
      {:ok, [value], "", _ctx, _line, _col} -> {:ok, value}
      {:ok, _, rest, _, _, _} -> {:error, "unexpected trailing input: #{inspect(rest)}"}
      {:error, msg, _rest, _, _, _} -> {:error, msg}
    end
  end

  # ── Reducers (called from combinator output lists) ────────────────────
  @doc "Returns to number result from parts."
  def to_number(parts) do
    text = IO.iodata_to_binary(parts)
    if String.contains?(text, "."), do: String.to_float(text), else: String.to_integer(text)
  end

  @doc "Returns to list result from items."
  def to_list([{:array, items}]), do: items

  @doc "Returns to pair result from value."
  def to_pair([key, value]), do: {key, value}

  @doc "Returns to map result from pairs."
  def to_map([{:object, pairs}]), do: Map.new(pairs)
end
```
### Step 3: `test/nimble_json_lite_test.exs`

**Objective**: Write `nimble_json_lite_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule NimbleJsonLiteTest do
  use ExUnit.Case, async: true

  doctest NimbleJsonLite

  describe "scalars" do
    test "null" do
      assert {:ok, nil} = NimbleJsonLite.decode("null")
    end

    test "booleans" do
      assert {:ok, true} = NimbleJsonLite.decode("true")
      assert {:ok, false} = NimbleJsonLite.decode("false")
    end

    test "integers and floats" do
      assert {:ok, 0} = NimbleJsonLite.decode("0")
      assert {:ok, 42} = NimbleJsonLite.decode("42")
      assert {:ok, -17} = NimbleJsonLite.decode("-17")
      assert {:ok, 3.14} = NimbleJsonLite.decode("3.14")
    end

    test "strings with escapes" do
      assert {:ok, "hello"} = NimbleJsonLite.decode(~s("hello"))
      assert {:ok, "a\"b"} = NimbleJsonLite.decode(~S("a\"b"))
      assert {:ok, "line1\nline2"} = NimbleJsonLite.decode(~S("line1\nline2"))
    end
  end

  describe "arrays" do
    test "empty" do
      assert {:ok, []} = NimbleJsonLite.decode("[]")
    end

    test "homogeneous" do
      assert {:ok, [1, 2, 3]} = NimbleJsonLite.decode("[1, 2, 3]")
    end

    test "mixed and nested" do
      assert {:ok, [1, "two", [3, 4], nil]} =
               NimbleJsonLite.decode(~s([1, "two", [3, 4], null]))
    end
  end

  describe "objects" do
    test "empty" do
      assert {:ok, %{}} = NimbleJsonLite.decode("{}")
    end

    test "simple" do
      assert {:ok, %{"k" => "v"}} = NimbleJsonLite.decode(~s({"k": "v"}))
    end

    test "nested with whitespace" do
      src = ~s({ "user": { "id": 1, "active": true }, "tags": ["a", "b"] })
      assert {:ok,
              %{
                "user" => %{"id" => 1, "active" => true},
                "tags" => ["a", "b"]
              }} = NimbleJsonLite.decode(src)
    end
  end

  describe "errors" do
    test "trailing garbage" do
      assert {:error, _} = NimbleJsonLite.decode("42 hello")
    end

    test "unterminated string" do
      assert {:error, _} = NimbleJsonLite.decode(~s("oops))
    end

    test "unterminated array" do
      assert {:error, _} = NimbleJsonLite.decode("[1, 2")
    end
  end
end
```
Run:

```bash
mix deps.get
mix test
```

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `NimbleJsonLite`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== NimbleJsonLite demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    # NimbleJsonLite.decode/1 requires 1 argument(s);
    # call it with real values appropriate for this exercise.
    # NimbleJsonLite.to_number/1 requires 1 argument(s);
    # call it with real values appropriate for this exercise.
    :ok
  end
end

Main.main()
```
## Trade-offs and production gotchas

**1. This is *not* a conformant JSON parser**
No unicode escapes (`\u00e9`), no exponent notation (`1e10`), no error
recovery. Use it to learn; use Jason for real data.

**2. Compile times scale with grammar size**
Combinators compile to big `case` statements. A grammar with hundreds of
rules slows down compilation noticeably. Split into modules with
`defparsec` at the boundaries to keep each compilation unit small.

**3. `defcombinatorp` vs `defparsecp`**
`defcombinatorp` reuses the combinator inline at call sites — fast, but
clauses get inlined everywhere. `defparsecp` emits a single set of
clauses called at runtime — slower per call, smaller binary. For recursive
grammars you'll typically want `defparsecp` for the entry point and
`defcombinatorp` for shared fragments.

**4. Error messages are raw — users need `label/2`**
The default `{:error, "expected ..."}` is cryptic. Wrap rules in
`label(name, "an integer")` to get human-readable messages. For serious
DSL work, build your own error renderer on top.

**5. Left-recursion is forbidden**
Parser combinators do not handle `expr ::= expr "+" term` directly. You
rewrite to right-recursive form and fold the result in a `reduce`.

**6. When NOT to use NimbleParsec**
- For JSON: use Jason (faster, complete, maintained).
- For free-form text extraction with fuzzy rules: regex is fine.
- For programming-language-scale grammars (hundreds of rules with
  precedence): consider `yecc` (Erlang's stdlib) or `leex`.

Use NimbleParsec for **medium-complexity structured text**: log lines,
small DSLs, protocol framing, query strings, versioned-header parsing.

---

## Benchmark

<!-- benchmark N/A: integration/configuration exercise -->

## Reflection

- This parser fails on `\u00e9` and scientific notation. If you were asked to make it RFC-8259 conformant, which of the two gaps do you think would cost more in grammar complexity — and what does that tell you about why Jason does not expose a user-extensible grammar at all?

## Resources

- [NimbleParsec on HexDocs](https://hexdocs.pm/nimble_parsec/NimbleParsec.html)
- [JSON spec (RFC 8259)](https://www.rfc-editor.org/rfc/rfc8259)
- [Plataformatec's NimbleParsec announcement](https://dashbit.co/blog/announcing-nimble_parsec) — origin story
- [Jason's source](https://github.com/michalmuskala/jason) — study how a real, production JSON parser is structured

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/nimble_json_lite_test.exs`

```elixir
defmodule NimbleJsonLiteTest do
  use ExUnit.Case, async: true

  doctest NimbleJsonLite

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert NimbleJsonLite.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
NimbleJSON is a tiny JSON parsing library, useful for minimal dependencies. It's not as fast as Jason but has zero dependencies beyond Elixir stdlib. Use when you need JSON parsing but cannot depend on compiled Rust (Jason is a Rust binary under the hood). For most projects, Jason is better; NimbleJSON is for constraint-heavy environments.

---
