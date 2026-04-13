# Unicode Normalization and Slug Generation

**Project**: `slugger` — standalone Mix project, 1–2 hours  
**Difficulty**: ★★☆☆☆

---

## Project structure

```
slugger/
├── lib/
│   └── slugger.ex                 # normalize + slugify
├── test/
│   └── slugger_test.exs           # ExUnit tests
└── mix.exs
```

---

## What you will learn

Two core concepts:

1. **Unicode normalization (NFC vs NFD)** — the same visible character can be stored as
   one composed codepoint (`"ñ"` = `U+00F1`) or two decomposed ones (`"n" + U+0303`
   combining tilde). Search, comparison, and slugification break unless you normalize first.
2. **Grapheme vs codepoint** — `String.length/1` counts graphemes (what users see as "one
   character"), `String.to_charlist/1` returns codepoints. Emojis and accented letters
   expose the difference immediately.

---

## The business problem

You're building a URL slug generator for a multilingual CMS. A blog post titled
`"Cómo compré café en São Paulo 🇧🇷"` must become `"como-compre-cafe-em-sao-paulo"` —
lowercase, ASCII-only, hyphen-separated, emoji stripped. Two rules that matter:

1. `"café"` typed on macOS often comes in NFC (`c-a-f-é`, 4 codepoints) but the same
   word pasted from Linux may arrive in NFD (`c-a-f-e-◌́`, 5 codepoints). The output must
   be identical regardless of input form.
2. `"ñ"` should become `"n"`, not be stripped. Users who search for `canon` must find
   the article titled `Cañón`.

---

## Why NFD first, then strip combining marks

The idiomatic "strip accents" recipe is:

1. Normalize to **NFD** — separates the base letter from the diacritic.
   `"é"` (1 codepoint) becomes `"e"` + `U+0301` (2 codepoints).
2. Drop codepoints in the Unicode **Combining Diacritical Marks** block (`U+0300..U+036F`).
3. What remains is ASCII-compatible "e" with no accent.

This works for Latin-based scripts (`é è ê ñ ç ã ö ü`). It does **not** work for scripts
where the diacritic is semantic (Arabic, Hebrew, Thai) — for those, a full transliteration
library is the right answer. Fail fast: know the scope of your algorithm.

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


### Step 1 — Create the project

**Objective**: Unicode normalization (NFC vs NFD) affects string comparison — café ≠ café (visually identical, different bytes).

```bash
mix new slugger
cd slugger
```

### Step 2 — `lib/slugger.ex`

**Objective**: NFD decomposition + lowercase + ASCII-only removal is the only correct way to generate slug duplicates from accents.

```elixir
defmodule Slugger do
  @moduledoc """
  URL-safe slug generation via Unicode normalization.
  """

  # Unicode "Combining Diacritical Marks" block.
  # Any codepoint in this range is an accent/diacritic attached to the previous letter.
  @combining_min 0x0300
  @combining_max 0x036F

  @doc """
  Turns a free-form title into a URL-safe slug.

  Examples:

      Slugger.slugify("Cómo compré café en São Paulo 🇧🇷")
      #=> "como-compre-cafe-em-sao-paulo"
  """
  @spec slugify(String.t()) :: String.t()
  def slugify(title) when is_binary(title) do
    title
    |> String.normalize(:nfd)
    |> strip_combining_marks()
    |> String.downcase()
    |> replace_non_alnum_with_hyphen()
    |> String.trim("-")
  end

  # Walk the string by codepoint (not grapheme) — after NFD, combining marks ARE
  # standalone codepoints that we want to drop individually.
  defp strip_combining_marks(str) do
    str
    |> String.to_charlist()
    |> Enum.reject(fn cp -> cp >= @combining_min and cp <= @combining_max end)
    |> List.to_string()
  end

  # Anything that's not ASCII [a-z 0-9] becomes a hyphen. Consecutive hyphens collapse.
  # Regex runs on the already-normalized/ASCII-fied string, so no Unicode concerns remain.
  defp replace_non_alnum_with_hyphen(str) do
    str
    |> String.replace(~r/[^a-z0-9]+/, "-")
  end

  @doc """
  Returns true if `a` and `b` are the same user-visible text, regardless of
  Unicode composition form. Useful for deduplication and equality checks on
  text coming from different OSes.
  """
  @spec equivalent?(String.t(), String.t()) :: boolean()
  def equivalent?(a, b) when is_binary(a) and is_binary(b) do
    String.normalize(a, :nfc) == String.normalize(b, :nfc)
  end
end
```

### Step 3 — `test/slugger_test.exs`

**Objective**: Test composed vs decomposed inputs; URL safety means no spaces, punctuation, or non-ASCII — slugs must be deterministic.

```elixir
defmodule SluggerTest do
  use ExUnit.Case, async: true

  describe "slugify/1" do
    test "strips Latin diacritics" do
      assert Slugger.slugify("Cañón") == "canon"
      assert Slugger.slugify("café") == "cafe"
      assert Slugger.slugify("São Paulo") == "sao-paulo"
    end

    test "removes emojis and flags" do
      assert Slugger.slugify("Hello 🌍 world 🇧🇷") == "hello-world"
    end

    test "collapses whitespace and punctuation into single hyphens" do
      assert Slugger.slugify("Hello,   world!!!") == "hello-world"
    end

    test "trims leading and trailing hyphens" do
      assert Slugger.slugify("!!!hello!!!") == "hello"
    end

    test "produces identical output for NFC and NFD inputs" do
      nfc = "café" |> String.normalize(:nfc)
      nfd = "café" |> String.normalize(:nfd)
      # Sanity: the two forms are NOT byte-equal.
      assert nfc != nfd
      # But both slugify to the same string.
      assert Slugger.slugify(nfc) == Slugger.slugify(nfd)
    end

    test "lowercases ASCII" do
      assert Slugger.slugify("HELLO") == "hello"
    end
  end

  describe "equivalent?/2" do
    test "treats NFC and NFD representations as equal" do
      assert Slugger.equivalent?(
               String.normalize("café", :nfc),
               String.normalize("café", :nfd)
             )
    end

    test "still rejects genuinely different strings" do
      refute Slugger.equivalent?("café", "cafe")
    end
  end
end
```

### Step 4 — Run the tests

**Objective**: --warnings-as-errors finds dead code in sanitizers; test coverage validates all line-ending scenarios.

```bash
mix test
```

All 8 tests should pass.

---




## Executable Example

Create a simple example demonstrating the key concepts:

```elixir
# Example code demonstrating text and binary concepts
IO.puts("Example: Read the Implementation section above and run the code samples in iex")
```

## Key Concepts

### 1. Unicode Normalization Combines Equivalent Characters
Some characters have multiple representations: é can be a single codepoint or a combination of e + acute accent. These look identical but have different byte sequences.

### 2. NFC vs NFD
NFC (Composed) — shorter, preferred for storage and comparison. NFD (Decomposed) — useful for systems that need individual components. For most applications, normalize to NFC.

### 3. Comparison After Normalization
Always normalize before comparing user-supplied text to canonical forms. Otherwise, semantically identical strings fail equality checks.

---
## Trade-offs

| Form | Size | Use case |
|------|------|----------|
| NFC (composed) | Smaller (1 codepoint per accented letter) | Default for storage, display, JSON payloads |
| NFD (decomposed) | Larger | Algorithmic processing (stripping accents, collation) |
| NFKC / NFKD | Aggressive (compatibility decomposition) | Search — maps `"ﬁ"` ligature to `"fi"` |

`String.length/1` uses **graphemes** so `"🇧🇷"` (flag = 2 regional indicator codepoints)
counts as 1. `byte_size/1` of the same string is 8. Pick the right metric for the job.

---

## Common production mistakes

**1. Using `byte_size/1` where you meant `String.length/1`**  
`byte_size("é") == 2` in NFC, `3` in NFD, `1` in Latin-1. If you're enforcing a user-facing
character limit, use `String.length/1` (graphemes). `byte_size` is for allocation and wire
formats only.

**2. Normalizing inconsistently between read and write**  
If you write NFC to the DB but compare against user input in its original form, you get
phantom duplicates (`"café"` vs `"café"` looking identical but comparing `!=`). Normalize
at both ends of every boundary (input, storage, comparison).

**3. Assuming `String.downcase/1` is ASCII-only**  
It's Unicode-aware: `"İ" |> String.downcase()` returns `"i̇"` (with combining dot). Usually
what you want, but surprising if you assumed "English only".

**4. Iterating with `String.to_charlist/1` and forgetting about graphemes**  
For a string like `"👨‍👩‍👧"` (family emoji = 5 codepoints joined by ZWJ), iterating
codepoints splits it. Use `String.graphemes/1` if you need user-visible units.

---

## When NOT to use this approach

- For non-Latin scripts (Arabic, Thai, Chinese, Cyrillic): stripping combining marks either
  does nothing or mangles the text. Use a transliteration library (e.g. the `slugify` hex
  package which wraps Unidecode) or romanization rules specific to the source script.
- If you need semantic equality (searching `"color"` matches `"colour"`), normalization
  won't help — that's a thesaurus/stemming problem.

---

## Resources

- [`String.normalize/2` docs](https://hexdocs.pm/elixir/String.html#normalize/2)
- [Unicode Normalization FAQ — unicode.org](https://unicode.org/faq/normalization.html)
- [`String.graphemes/1` vs `codepoints/1`](https://hexdocs.pm/elixir/String.html#graphemes/1)
- [`slugify` hex package](https://hex.pm/packages/slugify) — reference implementation for non-Latin scripts
