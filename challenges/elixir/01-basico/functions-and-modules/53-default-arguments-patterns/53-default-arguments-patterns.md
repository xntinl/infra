# Default Arguments with `\\`

**Project**: `api_client_defaults` — a small HTTP-ish client with configurable endpoint and timeout

**Difficulty**: ★☆☆☆☆
**Estimated time**: 1–2 hours

---

## Project structure

```
api_client_defaults/
├── lib/
│   └── api_client_defaults/
│       └── client.ex        # request/3 with default args
├── test/
│   └── api_client_defaults_test.exs
└── mix.exs
```

---

## What you will learn

1. **Default argument syntax `\\`** — how `def f(a, b \\ :default)` works.
2. **Clause generation** — the compiler expands default args into multiple clauses, and that
   has surprising implications when combined with multi-clause functions.

---

## The concept in 60 seconds

In Elixir you declare a default with `\\`:

```elixir
def request(path, timeout_ms \\ 5_000), do: do_request(path, timeout_ms)
```

The compiler generates:

```elixir
def request(path), do: request(path, 5_000)
def request(path, timeout_ms), do: do_request(path, timeout_ms)
```

That expansion is the source of every surprise people hit with defaults. Once you see it,
the rules below stop feeling arbitrary.

---

## Why defaults are useful here

An API client has knobs (base URL, timeout, retries) that callers should be able to override
but almost never want to specify. Defaults let the common path stay a one-liner (`Client.request("/ping")`)
while still allowing full control (`Client.request("/ping", base_url: "...", timeout_ms: 1_000)`).

---

## Implementation

### Step 1 — Create the project

```bash
mix new api_client_defaults
cd api_client_defaults
```

### Step 2 — `lib/api_client_defaults/client.ex`

```elixir
defmodule ApiClientDefaults.Client do
  @moduledoc """
  Minimal API client illustrating default arguments.

  We do NOT perform real HTTP here — the point is how defaults behave.
  `do_request/3` returns a shaped map so tests can assert on it.
  """

  @default_base_url "https://api.example.com"
  @default_timeout_ms 5_000

  @type opts :: [base_url: String.t(), timeout_ms: pos_integer()]

  @doc """
  Sends a request to `path` with optional overrides.

  `opts` is a keyword list because we want named overrides — not positional.
  Using `\\\\ []` gives callers a one-arg call site while keeping a single clause body.
  """
  @spec request(String.t(), opts()) :: %{url: String.t(), timeout_ms: pos_integer()}
  def request(path, opts \\ []) when is_binary(path) do
    base_url = Keyword.get(opts, :base_url, @default_base_url)
    timeout_ms = Keyword.get(opts, :timeout_ms, @default_timeout_ms)

    do_request(path, base_url, timeout_ms)
  end

  # Defaults on a helper with positional args — shown here to demonstrate clause generation.
  # In real code, prefer keyword lists (as in request/2) for anything beyond one or two opts.
  defp do_request(path, base_url \\ @default_base_url, timeout_ms \\ @default_timeout_ms) do
    %{url: base_url <> path, timeout_ms: timeout_ms}
  end
end
```

### Step 3 — `test/api_client_defaults_test.exs`

```elixir
defmodule ApiClientDefaultsTest do
  use ExUnit.Case, async: true

  alias ApiClientDefaults.Client

  describe "request/2 defaults" do
    test "uses defaults when no opts are given" do
      assert Client.request("/ping") ==
               %{url: "https://api.example.com/ping", timeout_ms: 5_000}
    end

    test "overrides only base_url" do
      result = Client.request("/ping", base_url: "https://staging.example.com")
      assert result.url == "https://staging.example.com/ping"
      assert result.timeout_ms == 5_000
    end

    test "overrides only timeout_ms" do
      result = Client.request("/ping", timeout_ms: 1_000)
      assert result.url == "https://api.example.com/ping"
      assert result.timeout_ms == 1_000
    end

    test "overrides both" do
      result = Client.request("/ping", base_url: "https://x.test", timeout_ms: 250)
      assert result == %{url: "https://x.test/ping", timeout_ms: 250}
    end
  end

  describe "guards apply to every generated clause" do
    test "rejects non-binary path" do
      assert_raise FunctionClauseError, fn -> Client.request(:not_a_string) end
    end
  end
end
```

### Step 4 — Run the tests

```bash
mix test
```

All 5 tests pass.

---

## Trade-offs

| Style | When to pick |
|---|---|
| Positional defaults `def f(a, b \\\\ 1, c \\\\ 2)` | ≤ 2 optional args, order is obvious |
| Keyword list with `Keyword.get/3` | ≥ 3 optional args, or names carry meaning |
| Struct-based config | The "opts" outlive a single call (reused across many requests) |
| NimbleOptions schema | Library public API where bad opts must error loudly |

**When NOT to use default arguments:**

- **Multi-clause functions with different bodies.** Defaults must be declared in a **separate
  header clause** without a body, otherwise the compiler errors. See pitfall #1 below.
- **More than 2 positional options.** The call site becomes unreadable positionally —
  switch to a keyword list.

---

## Common production mistakes

**1. Defaults across multi-clause functions**

This does **not** compile:

```elixir
def greet(name, greeting \\ "Hello"), do: "#{greeting}, #{name}!"
def greet(:admin, greeting),           do: "#{greeting}, boss."
```

You must declare a header clause with the defaults and no body:

```elixir
def greet(name, greeting \\ "Hello")
def greet(:admin, greeting), do: "#{greeting}, boss."
def greet(name, greeting),   do: "#{greeting}, #{name}!"
```

**2. Default that evaluates at call time, not compile time**

The right-hand side of `\\` is evaluated **every call**, not once at compile time:

```elixir
def log(msg, ts \\ DateTime.utc_now()), do: IO.puts("#{ts}: #{msg}")
```

That is usually fine, but if the default is expensive, precompute it.

**3. Guards and defaults**

The guard applies to **all generated clauses**. If the guard rejects the default value,
the zero-arg call crashes at runtime. Always pick defaults that satisfy the guard.

**4. Using atoms as "flags" instead of keyword lists**

`def send(msg, :sync)` vs `def send(msg, :async)` hides configuration behind positional
atoms. Keyword lists self-document: `send(msg, mode: :async)`.

**5. Default of mutable-looking values**

Lists and maps used as defaults are fine (Elixir is immutable), but people coming from
Python sometimes worry about "shared default" bugs. There is no shared mutation to worry
about — but do precompute large defaults at compile time using module attributes.

---

## Resources

- [Elixir — Default arguments](https://hexdocs.pm/elixir/modules-and-functions.html#default-arguments)
- [Keyword module docs](https://hexdocs.pm/elixir/Keyword.html)
- [NimbleOptions](https://hexdocs.pm/nimble_options/) — validated option schemas for libraries
