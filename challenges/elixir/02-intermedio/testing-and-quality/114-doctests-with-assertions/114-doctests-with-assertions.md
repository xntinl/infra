# Doctests: executable examples in `@doc`

**Project**: `doctests_demo` — a `StringKit` module whose `@doc` examples
are executed as tests via `doctest`.

---

## Project context

Documentation lies. Not on purpose — the code changes, the example doesn't,
and now your `@doc` shows a function signature that hasn't existed in two
versions. Elixir's `doctest` solves this by running every `iex>` example
in your `@doc` as a test. If the example drifts from reality, CI fails.

This is doubly valuable because `ex_doc` renders `@doc` as the HTML
documentation on HexDocs — the same examples your users copy-paste are
the ones your CI verifies. One artifact, two jobs.

## Why doctests and not X

**Why not tests-only + code comments?** Because comments go stale silently.
Doctests fail CI the moment the example drifts, so they stay current by
construction.

**Why not tests-only (skip the `@doc` examples)?** Because the HexDocs page
is the first thing users read. Copy-pasteable, verified examples are the
single highest-leverage piece of docs you can ship.

**Why not replace unit tests with doctests?** Doctests are poor at edge
cases and error paths — cramming corner cases into `@doc` clutters the
docs. Use both: doctests for happy-path illustrations, `_test.exs` for
the rest.

Project structure:

```
doctests_demo/
├── lib/
│   └── string_kit.ex
├── test/
│   ├── string_kit_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Core concepts

### 1. `iex>` prompt is the doctest marker

Anything after `iex>` in an `@doc` block is evaluated. The line(s)
following — without a prompt — are the expected output.

```elixir
@doc """
    iex> StringKit.reverse_words("hello world")
    "world hello"
"""
```

Note the 4-space indent — it's required for the Markdown code block.

### 2. `doctest Module` inserts the tests

In your test file:

```elixir
defmodule StringKitTest do
  use ExUnit.Case, async: true
  doctest StringKit
end
```

That single line generates one test per `iex>` example across every
function of the module.

### 3. Multiline expressions and multiple assertions

```elixir
@doc """
    iex> s = StringKit.reverse_words("a b c")
    iex> String.length(s)
    5
"""
```

Successive `iex>` lines share bindings. The expected output only appears
after the last `iex>` of the group.

### 4. Doctests are not a replacement for unit tests

They're great for showing how to USE a function. They're terrible for
edge cases, error paths, or anything that makes the docs noisy. Rule of
thumb: one happy-path example per public function. Put corner cases in
`_test.exs`.

---

## Design decisions

**Option A — All examples as doctests**
- Pros: Everything is rendered in HexDocs and verified.
- Cons: Docs become noisy with edge cases; readers scan past them.

**Option B — One happy-path doctest per function + separate edge tests** (chosen)
- Pros: Docs stay skimmable; CI still verifies the copy-pasteable example.
- Cons: Edge cases live in two places (test file), not in docs.

→ Chose **B**. Public readers want the "how do I call this?" example;
private tests cover the "but what about…" cases.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
    {ok},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new doctests_demo
cd doctests_demo
```

### Step 2: `lib/string_kit.ex`

**Objective**: Implement `string_kit.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule StringKit do
  @moduledoc """
  A tiny string-utility module demonstrating doctests. Every public
  function carries at least one `iex>` example that doubles as a test.
  """

  @doc """
  Reverses the order of whitespace-separated words in a string.

  ## Examples

      iex> StringKit.reverse_words("hello world")
      "world hello"

      iex> StringKit.reverse_words("a b c d")
      "d c b a"

      iex> StringKit.reverse_words("")
      ""

  """
  @spec reverse_words(String.t()) :: String.t()
  def reverse_words(""), do: ""

  def reverse_words(str) when is_binary(str) do
    str
    |> String.split(" ", trim: true)
    |> Enum.reverse()
    |> Enum.join(" ")
  end

  @doc """
  Counts how many times `needle` appears in `haystack`. Non-overlapping.

  ## Examples

      iex> StringKit.count_occurrences("abababab", "ab")
      4

      iex> StringKit.count_occurrences("no matches here", "xyz")
      0

  A longer example with intermediate bindings:

      iex> s = String.duplicate("na", 5)
      iex> StringKit.count_occurrences(s, "na")
      5

  """
  @spec count_occurrences(String.t(), String.t()) :: non_neg_integer()
  def count_occurrences(_haystack, ""), do: 0

  def count_occurrences(haystack, needle) do
    haystack
    |> String.split(needle)
    |> length()
    |> Kernel.-(1)
  end

  @doc """
  Parses a positive integer. Returns `:error` on any malformed input.

  ## Examples

      iex> StringKit.parse_positive("42")
      {:ok, 42}

      iex> StringKit.parse_positive("-5")
      :error

      iex> StringKit.parse_positive("abc")
      :error

  """
  @spec parse_positive(String.t()) :: {:ok, pos_integer()} | :error
  def parse_positive(str) when is_binary(str) do
    case Integer.parse(str) do
      {n, ""} when n > 0 -> {:ok, n}
      _ -> :error
    end
  end
end
```

### Step 3: `test/string_kit_test.exs`

**Objective**: Write `string_kit_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.


```elixir
defmodule StringKitTest do
  use ExUnit.Case, async: true

  # This single line generates one test per `iex>` example in StringKit.
  doctest StringKit

  # Doctests cover the happy path; put edge cases, error paths, and
  # property-style checks in regular tests below.
  describe "count_occurrences/2 edge cases" do
    test "empty needle returns 0 (convention in this module)" do
      assert StringKit.count_occurrences("any", "") == 0
    end

    test "needle longer than haystack returns 0" do
      assert StringKit.count_occurrences("ab", "abcd") == 0
    end
  end

  describe "parse_positive/1 — non-doctestable cases" do
    test "rejects zero" do
      # Not in the doctest because we already showed `-5` — one edge is enough there.
      assert StringKit.parse_positive("0") == :error
    end

    test "rejects trailing garbage" do
      assert StringKit.parse_positive("42abc") == :error
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
mix test --trace
```

Each `iex>` example shows up as its own test line.

### Why this works

`ExUnit.DocTest` parses each `@doc` block at compile time, extracts every
`iex>` line, and generates one ExUnit test per example. Shared bindings
across successive `iex>` lines are handled by evaluating the group as a
single script; the expected output is compared with `==`. Because the
generator runs at compile time, CI failures point exactly to the offending
file:line pair in the source module, not in the test file.

---

## Benchmark

<!-- benchmark N/A: doctests son aserciones puras; no hay workload. -->

---

## Trade-offs and production gotchas

**1. Doctests are brittle for non-deterministic output**
Functions that return pids, refs, timestamps, or map-ordering-dependent
output will fail intermittently as doctests. Either stub them, or keep
doctests off those functions and write regular tests instead.

**2. Indentation matters — a lot**
The `iex>` lines must be indented consistently inside the `@doc` block.
A stray space flips the Markdown code block and your example becomes
prose, silently un-tested.

**3. Multi-line expected output uses no prompt on continuation**
```
iex> Enum.map([1,2,3], & &1 * 2)
[2, 4, 6]
```
But if the output spans multiple lines (a map printed in pretty form),
match on the whole thing verbatim — no `iex>` on continuation lines.

**4. Doctests run in your test module's process**
Which means they share process dictionary, ETS tables, etc. with other
tests in the same module. Rarely matters, but can surprise.

**5. When NOT to use doctests**
For modules with heavy state (GenServers, Ecto schemas), for anything
involving IO or the network, or for error-path testing. Doctests shine
for **pure utility functions** where the example is also the tutorial.

---

## Reflection

- Your module's function returns a map whose key ordering isn't guaranteed.
  Write a doctest that passes deterministically, and explain why the
  naive version fails on some runs.
- A team mate wants to doctest a function that calls `DateTime.utc_now/0`.
  What's your counter-proposal, and what's the minimal refactor that
  makes the function doctestable?

---

## Resources

- [`ExUnit.DocTest`](https://hexdocs.pm/ex_unit/ExUnit.DocTest.html)
- [`@doc` — writing documentation](https://hexdocs.pm/elixir/writing-documentation.html)
- [`ex_doc`](https://hexdocs.pm/ex_doc/) — renders `@doc` into HTML
- ["Writing Documentation" — Elixir guides](https://hexdocs.pm/elixir/writing-documentation.html#doctests)


## Key Concepts

ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.
