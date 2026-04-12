# Generating documentation with ExDoc

**Project**: `docs_demo` — a library with rich docs, doctests, cross-module
links, and an `extras/` guide, rendered by ExDoc to the same HTML you see
on hexdocs.pm.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

ExDoc is the tool that renders Elixir's module docs into the site you've
been reading for the last 20 exercises. If you publish to Hex, ExDoc runs
on their build server and the output becomes your `hexdocs.pm/<pkg>` page.
If you don't publish, running `mix docs` locally gives you an identical
site in `doc/`.

This exercise shows the full documentation pipeline:

1. `@moduledoc` + `@doc` with Markdown, typespecs, and examples.
2. Doctests so examples in docs are TESTED on every CI run.
3. Cross-module links with the backtick notation.
4. Extras: free-form guides (a `GUIDE.md`) rendered alongside module docs.
5. `mix docs` configured with `source_url`, logo, groups, and extras.

Project structure:

```
docs_demo/
├── lib/
│   ├── docs_demo.ex
│   └── docs_demo/
│       ├── string_utils.ex
│       └── math_utils.ex
├── guides/
│   └── getting_started.md
├── test/
│   ├── string_utils_test.exs
│   └── math_utils_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `@moduledoc` and `@doc` are just Markdown

```elixir
defmodule MathUtils do
  @moduledoc """
  Numeric helpers.

  ## Quick start

      iex> MathUtils.square(3)
      9
  """
end
```

Two rules you'll forget:

- The docstring must be `"""`-delimited — single-line `@doc "foo"` works
  but can't contain Markdown sections.
- `@moduledoc false` hides a module from docs entirely. Use it for internal
  helpers so your public surface stays clean.

### 2. Doctests: examples that run on CI

Any block indented under `## Examples` that looks like `iex>` prompts is
executed as a test when the test file calls `doctest ModuleName`.

```elixir
@doc """
## Examples

    iex> MathUtils.square(3)
    9
"""
```

**If the example is wrong, `mix test` fails.** Your docs cannot drift from
reality. This is the single highest-ROI habit in the Elixir ecosystem.

### 3. Cross-module references

Inside a docstring, backticks with a `Module.function/arity` shape become
hyperlinks:

| Markdown                   | Rendered link            |
|----------------------------|--------------------------|
| `` `String.upcase/1` ``    | → the stdlib doc         |
| `` `MathUtils` ``          | → your own module        |
| `` `t:Enumerable.t/0` ``   | → a type                 |
| `` `c:GenServer.init/1` ``| → a callback             |

Prefixes: `t:` for types, `c:` for callbacks, `mfa` (bare) for functions.

### 4. Extras — long-form guides

Add `extras: ["guides/getting_started.md"]` to `mix docs` config and ExDoc
renders Markdown files as sibling pages to the API reference. Use extras
for tutorials, migration guides, and design rationale that don't belong on
any one module.

---

## Implementation

### Step 1: Create the project

```bash
mix new docs_demo
cd docs_demo
```

### Step 2: Add ExDoc to `mix.exs`

```elixir
defmodule DocsDemo.MixProject do
  use Mix.Project

  @source_url "https://github.com/example/docs_demo"
  @version "0.1.0"

  def project do
    [
      app: :docs_demo,
      version: @version,
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps(),

      # --- ExDoc-specific configuration ---
      name: "DocsDemo",
      source_url: @source_url,
      homepage_url: @source_url,
      docs: docs()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end

  defp docs do
    [
      main: "readme",
      source_url: @source_url,
      source_ref: "v#{@version}",
      extras: [
        "README.md",
        "guides/getting_started.md": [title: "Getting Started"]
      ],
      groups_for_modules: [
        "Utilities": [
          DocsDemo.StringUtils,
          DocsDemo.MathUtils
        ]
      ],
      groups_for_docs: [
        "Math": &(&1[:section] == :math),
        "Strings": &(&1[:section] == :strings)
      ]
    ]
  end
end
```

### Step 3: `lib/docs_demo.ex` — the entry-point module

```elixir
defmodule DocsDemo do
  @moduledoc """
  Entry point for `DocsDemo`.

  This library is a demo of the ExDoc pipeline. See:

    * `DocsDemo.StringUtils` — text helpers.
    * `DocsDemo.MathUtils` — numeric helpers.

  It also ships with the ["Getting Started"](getting_started.html) guide.
  """

  @doc """
  Returns the library version.

  ## Examples

      iex> is_binary(DocsDemo.version())
      true
  """
  @spec version() :: String.t()
  def version, do: Application.spec(:docs_demo, :vsn) |> to_string()
end
```

### Step 4: `lib/docs_demo/string_utils.ex`

```elixir
defmodule DocsDemo.StringUtils do
  @moduledoc """
  Small string helpers, used to demonstrate `@doc`, `@spec`, and doctests.

  See also `DocsDemo.MathUtils` for the numeric counterpart.
  """

  @doc section: :strings
  @doc """
  Shouts a string by uppercasing and appending an exclamation mark.

  Delegates the uppercase step to `String.upcase/1`.

  ## Examples

      iex> DocsDemo.StringUtils.shout("hello")
      "HELLO!"

      iex> DocsDemo.StringUtils.shout("")
      "!"
  """
  @spec shout(String.t()) :: String.t()
  def shout(text) when is_binary(text), do: String.upcase(text) <> "!"

  @doc section: :strings
  @doc """
  Reverses a string.

  ## Examples

      iex> DocsDemo.StringUtils.reverse("abc")
      "cba"
  """
  @spec reverse(String.t()) :: String.t()
  def reverse(text) when is_binary(text), do: String.reverse(text)

  # Internal helper; @doc false hides it from ExDoc output.
  @doc false
  def __private_helper__, do: :ok
end
```

### Step 5: `lib/docs_demo/math_utils.ex`

```elixir
defmodule DocsDemo.MathUtils do
  @moduledoc """
  Numeric helpers.

  Use with `DocsDemo.StringUtils` for the full "hello world" experience.
  """

  @typedoc "A non-negative integer used as a count or size."
  @type non_neg :: non_neg_integer()

  @doc section: :math
  @doc """
  Returns the square of a number.

  ## Examples

      iex> DocsDemo.MathUtils.square(3)
      9

      iex> DocsDemo.MathUtils.square(-4)
      16
  """
  @spec square(number()) :: number()
  def square(n) when is_number(n), do: n * n

  @doc section: :math
  @doc """
  Clamps `n` to the range `[min, max]`.

  Raises `ArgumentError` if `min > max`.

  ## Examples

      iex> DocsDemo.MathUtils.clamp(5, 0, 10)
      5

      iex> DocsDemo.MathUtils.clamp(-1, 0, 10)
      0

      iex> DocsDemo.MathUtils.clamp(99, 0, 10)
      10
  """
  @spec clamp(number(), number(), number()) :: number()
  def clamp(_n, min, max) when min > max, do: raise(ArgumentError, "min > max")
  def clamp(n, min, _max) when n < min, do: min
  def clamp(n, _min, max) when n > max, do: max
  def clamp(n, _min, _max), do: n
end
```

### Step 6: `guides/getting_started.md`

```markdown
# Getting Started

`DocsDemo` is a demo library for learning ExDoc. Install by adding
`{:docs_demo, "~> 0.1"}` to your deps, then use `DocsDemo.MathUtils.square/1`
or `DocsDemo.StringUtils.shout/1` from anywhere in your code.
```

### Step 7: Tests (with doctests enabled)

`test/math_utils_test.exs`:

```elixir
defmodule DocsDemo.MathUtilsTest do
  use ExUnit.Case, async: true

  doctest DocsDemo.MathUtils

  test "clamp/3 raises when min > max" do
    assert_raise ArgumentError, fn -> DocsDemo.MathUtils.clamp(1, 10, 0) end
  end
end
```

`test/string_utils_test.exs`:

```elixir
defmodule DocsDemo.StringUtilsTest do
  use ExUnit.Case, async: true

  doctest DocsDemo.StringUtils
end
```

### Step 8: Build the docs

```bash
mix deps.get
mix test        # doctests + normal tests
mix docs        # generates doc/index.html
open doc/index.html
```

You get the full hexdocs.pm UX locally: sidebar with extras, module groups,
search, "source" links pointing back at GitHub.

---

## Trade-offs and production gotchas

**1. Doctests are tests — they fail the build**
This is a feature, not a bug. If you change behavior without updating
docs, CI fails. But it means you can't put speculative/aspirational
examples in docs — only things that work right now. Use `## Examples` for
real examples and plain prose for everything else.

**2. `@moduledoc false` vs omitting it**
Omitting `@moduledoc` emits a compiler warning (in `--warnings-as-errors`
it breaks CI). Modules that are genuinely internal should have explicit
`@moduledoc false` — it tells both the compiler and the reader this is
intentional.

**3. `source_ref:` matters for Hex**
Without `source_ref: "v#{@version}"`, the "Source" links in docs point to
the default branch — which will drift. For a published package, `source_ref`
pins links to the exact tag.

**4. Extras order matters**
The `extras:` list order is the sidebar order. Put your "Getting Started"
first, migration guides next, reference last.

**5. Doctests are slow if you have many**
Each doctest is a separate ExUnit test with its own setup. Thousands of
doctests can noticeably slow the suite. Keep doctests for API illustration;
use `describe`-grouped unit tests for edge cases.

**6. When NOT to add a doctest**
- The example depends on wall-clock time, randomness, or environment (fails
  on CI).
- The output is a huge map/list (the doctest becomes unreadable).
- The behavior is platform-specific.

For those, describe the behavior in prose and test it in the test file.

---

## Resources

- [ExDoc](https://hexdocs.pm/ex_doc/) — main docs, configuration options
- [Writing documentation — the Elixir guide](https://hexdocs.pm/elixir/writing-documentation.html) — idioms for `@moduledoc`, `@doc`, `@typedoc`
- [ExUnit.DocTest](https://hexdocs.pm/ex_unit/ExUnit.DocTest.html) — doctest mechanics and escape hatches
- [Hex.pm — "Publishing docs"](https://hex.pm/docs/publish#publishing-docs) — how `mix hex.publish` uploads to hexdocs.pm
