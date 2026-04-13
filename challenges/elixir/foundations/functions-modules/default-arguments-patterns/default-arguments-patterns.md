# Default Arguments with `\\`

**Project**: `api_client_defaults` — a small HTTP-ish client with configurable endpoint and timeout

---

## The business problem

An API client has knobs (base URL, timeout, retries) that callers should be able to override
but almost never want to specify. Defaults let the common path stay a one-liner (`Client.request("/ping")`)
while still allowing full control (`Client.request("/ping", base_url: "...", timeout_ms: 1_000)`).

---

## Project structure

```
api_client_defaults/
├── lib/
│   └── api_client_defaults/
│       └── client.ex        # request/3 with default args
├── script/
│   └── main.exs
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

## Why `\\` defaults and not overloaded arities by hand

- Writing `def request(path), do: request(path, [])` and `def request(path, opts), do: ...` works, but duplicates the call site and drifts: add a third arg and you maintain N! clauses.
- `\\` expands to the same overloads **mechanically** — the compiler guarantees they stay in sync.
- An options struct (`%Request{}`) is cleaner once the option set grows, but for 1–2 knobs it's over-engineering.

---

## Design decisions

**Option A — positional defaults for every knob** (`def request(path, base \\ ..., timeout \\ ...)`)
- Pros: call site is short for the one-knob case (`request(path, "https://x")`).
- Cons: two-knob override forces callers to remember positional order; adding a knob breaks every call site.

**Option B — single keyword list with `Keyword.get/3` defaults** (chosen)
- Pros: every override is named at the call site; adding a knob is backward-compatible; the public signature stays `request(path, opts \\ [])`.
- Cons: one extra dereference per option; typos in keys fail silently unless validated.

→ Chose **B** because API clients grow new knobs over time. Keyword-list-with-defaults scales; positional defaults do not.

---

## Implementation

### `mix.exs`
```elixir
defmodule ApiClientDefaults.MixProject do
  use Mix.Project

  def project do
    [
      app: :api_client_defaults,
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

### Step 1 — Create the project

**Objective**: Create a single-module project so the focus stays on how `\\` default arguments expand into multiple function heads at compile time.

```bash
mix new api_client_defaults
cd api_client_defaults
```

### Step 2 — `lib/api_client_defaults/client.ex`

**Objective**: Use `\\` with a head clause so defaults live at the public API and pattern-matched clauses stay free of the boilerplate.

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

**Objective**: Call the function with every arity combination to prove each expanded head is reachable and defaults behave exactly as at the call site.

```elixir
defmodule ApiClientDefaultsTest do
  use ExUnit.Case, async: true
  doctest ApiClientDefaults.Client

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

**Objective**: Run the suite to confirm the compiler did not generate a second default-args head clause — a silent ambiguity that bites in reviews.

```bash
mix test
```

All 5 tests pass.

### Why this works

The compiler expands `opts \\ []` into a zero-arg wrapper (`request(path)` → `request(path, [])`), so callers get a one-arg call site without any hand-written overload. `Keyword.get/3` supplies the per-knob default only when the caller omits that key — the two defaulting mechanisms layer cleanly because `opts` defaults to an empty list, and every `Keyword.get` then falls back to its own default. The guard `when is_binary(path)` is attached once and applies to both generated clauses.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== ApiClientDefaults: demo ===\n")

    result_1 = ApiClientDefaults.Client.request("/ping")
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = Mix.env()
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = Keyword.get(opts, :base_url, @default_base_url)
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

## Benchmark

```elixir
# bench/defaults.exs
{t_default, _} = :timer.tc(fn ->
  Enum.each(1..1_000_000, fn _ -> ApiClientDefaults.Client.request("/ping") end)
end)

{t_override, _} = :timer.tc(fn ->
  Enum.each(1..1_000_000, fn _ ->
    ApiClientDefaults.Client.request("/ping", base_url: "https://x.test", timeout_ms: 250)
  end)
end)

IO.puts("default: #{t_default} µs   override: #{t_override} µs")
```

Target: < 1 µs per call for both paths on modern hardware. `Keyword.get/3` on a short list is O(n) but n ≤ 2 here — the cost is negligible.

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

## Reflection

- Your client now needs a `:retries` option that defaults to 3 but must be **validated** (integer, 0..10). Where does that validation live — inside `request/2`, in a separate `validate_opts/1`, or as a NimbleOptions schema? What does each choice cost?
- Suppose callers start misspelling `timeout_ms` as `timeout`. Your code silently uses the default. How would you make that a loud failure without breaking existing correct call sites?

---

## Resources

- [Elixir — Default arguments](https://hexdocs.pm/elixir/modules-and-functions.html#default-arguments)
- [Keyword module docs](https://hexdocs.pm/elixir/Keyword.html)
- [NimbleOptions](https://hexdocs.pm/nimble_options/) — validated option schemas for libraries

---

## Why Default Arguments with `\\` matters

Mastering **Default Arguments with `\\`** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/api_client_defaults.ex`

```elixir
defmodule ApiClientDefaults do
  @moduledoc """
  Reference implementation for Default Arguments with `\\`.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the api_client_defaults module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> ApiClientDefaults.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/api_client_defaults_test.exs`

```elixir
defmodule ApiClientDefaultsTest do
  use ExUnit.Case, async: true

  doctest ApiClientDefaults

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ApiClientDefaults.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. Default Arguments Create Multiple Arities
The shorthand `\\` creates two functions: one with and one without the default. Defaults are evaluated in the calling scope.

### 2. Defaults Are Evaluated in the Calling Scope
This is usually what you want, but be careful with mutable defaults (they're not re-evaluated on each call).

### 3. Combine with Guards
You can combine defaults with guards: `def process(x \\ 10) when is_integer(x)`. The guard applies to both arities.

---
