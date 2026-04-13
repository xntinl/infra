# `case` with Nested Patterns and Guards

**Project**: `http_status_classifier` — classifies HTTP-shaped tuples into actionable categories

---

## The business problem

Real apps have to react differently to `4xx` (caller's fault) vs `5xx` (retry with backoff)
vs network errors (retry immediately, different circuit breaker). Every branch has a distinct
shape: `{:ok, status}`, `{:error, {:http, status}}`, `{:error, {:network, reason}}`. Pattern
matching with guards is how you express "shape + numeric range" in one readable block.

---

## Project structure

```
http_status_classifier/
├── lib/
│   └── http_status_classifier/
│       └── classifier.ex
├── script/
│   └── main.exs
├── test/
│   └── http_status_classifier_test.exs
└── mix.exs
```

---

## What you will learn

1. **Nested patterns in `case`** — pattern match on the *inside* of tuples and maps, not just the outer shape.
2. **Guards attached to clauses** — use `when` to cover numeric ranges without exploding the number of clauses.

---

## The concept in 60 seconds

A `case` clause can match structure **and** ranges simultaneously:

```elixir
case result do
  {:ok, status} when status in 200..299 -> :success
  {:error, {:http, status}} when status in 500..599 -> :server_error
  _ -> :unknown
end
```

Two things happen here that are easy to miss:
- The pattern reaches **inside** the error tuple to extract `{:http, status}`.
- The guard filters by range, something patterns alone cannot do.

This is the Elixir idiom for classifying shaped data. You rarely need `if`/`cond` for it.

---

## Why HTTP classification

Real apps have to react differently to `4xx` (caller's fault) vs `5xx` (retry with backoff)
vs network errors (retry immediately, different circuit breaker). Every branch has a distinct
shape: `{:ok, status}`, `{:error, {:http, status}}`, `{:error, {:network, reason}}`. Pattern
matching with guards is how you express "shape + numeric range" in one readable block.

---

## Design decisions

**Option A — single `case` with deep patterns + guards**
- Pros: No allocation, direct dispatch, rules visible in one place
- Cons: Becomes hard to read beyond 5-6 clauses

**Option B — pre-flatten into a struct then `case`** (chosen)
- Pros: Normalizes the input, decouples shape from classification
- Cons: Extra allocation, more code, indirection

→ Chose **A** because a single-purpose HTTP status classifier is small enough to keep the rules in one `case`. Use B when the same shape is classified in multiple places.

## Implementation

### `mix.exs`
```elixir
defmodule HttpStatusClassifier.MixProject do
  use Mix.Project

  def project do
    [
      app: :http_status_classifier,
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

**Objective**: Create the project scaffold so the classifier lives as a pure library with no runtime app, keeping the focus on deep pattern matching alone.

```bash
mix new http_status_classifier
cd http_status_classifier
```

### Step 2 — `lib/http_status_classifier/classifier.ex`

**Objective**: Exploit deep pattern matching inside one `case` so every HTTP result shape is dispatched by structure, not conditionals, making the taxonomy explicit and total.

```elixir
defmodule HttpStatusClassifier.Classifier do
  @moduledoc """
  Classifies HTTP-shaped results into actionable categories.

  Input shapes:
    {:ok, status}                         — response received
    {:error, {:http, status}}             — response received, non-2xx (e.g. 4xx/5xx)
    {:error, {:network, reason}}          — transport failure (timeout, closed)
    anything else                         — :unknown
  """

  @type http_result ::
          {:ok, non_neg_integer()}
          | {:error, {:http, non_neg_integer()}}
          | {:error, {:network, atom()}}

  @type category ::
          :success
          | :redirect
          | :client_error
          | :server_error
          | :network_timeout
          | :network_closed
          | :unknown

  @spec classify(http_result() | term()) :: category()
  def classify(result) do
    case result do
      # 2xx — success. Guard narrows the range inside the tuple.
      {:ok, status} when status in 200..299 ->
        :success

      # 3xx — redirect (still an :ok tuple because the transport succeeded).
      {:ok, status} when status in 300..399 ->
        :redirect

      # 4xx — client error. Note the nested pattern: {:error, {:http, status}}.
      {:error, {:http, status}} when status in 400..499 ->
        :client_error

      # 5xx — server error. Same nested shape, different range.
      {:error, {:http, status}} when status in 500..599 ->
        :server_error

      # Network-level failures — reason is an atom, not a number.
      # Two clauses because different reasons warrant different retry strategies.
      {:error, {:network, :timeout}} ->
        :network_timeout

      {:error, {:network, :closed}} ->
        :network_closed

      # Catch-all — never silently drop malformed input in production.
      _ ->
        :unknown
    end
  end
end
```

### Step 3 — `test/http_status_classifier_test.exs`

**Objective**: Assert each clause is reachable and that unknown shapes fall through deterministically, so a future edit can never silently drop a case.

```elixir
defmodule HttpStatusClassifierTest do
  use ExUnit.Case, async: true
  doctest HttpStatusClassifier.Classifier

  alias HttpStatusClassifier.Classifier

  describe "2xx/3xx — :ok tuples" do
    test "200 is success" do
      assert Classifier.classify({:ok, 200}) == :success
    end

    test "299 is still success (boundary)" do
      assert Classifier.classify({:ok, 299}) == :success
    end

    test "301 is redirect" do
      assert Classifier.classify({:ok, 301}) == :redirect
    end
  end

  describe "4xx/5xx — :error {:http, _} tuples" do
    test "404 is client_error" do
      assert Classifier.classify({:error, {:http, 404}}) == :client_error
    end

    test "500 is server_error" do
      assert Classifier.classify({:error, {:http, 500}}) == :server_error
    end

    test "599 is still server_error (boundary)" do
      assert Classifier.classify({:error, {:http, 599}}) == :server_error
    end
  end

  describe "network-level failures" do
    test "network timeout" do
      assert Classifier.classify({:error, {:network, :timeout}}) == :network_timeout
    end

    test "network closed" do
      assert Classifier.classify({:error, {:network, :closed}}) == :network_closed
    end
  end

  describe "unknown shapes fall through" do
    test "bare atom" do
      assert Classifier.classify(:huh) == :unknown
    end

    test "status out of all defined ranges" do
      assert Classifier.classify({:ok, 999}) == :unknown
    end

    test "error tuple with unexpected inner tag" do
      assert Classifier.classify({:error, {:weird, :stuff}}) == :unknown
    end
  end
end
```

### Step 4 — Run the tests

**Objective**: Run the suite with warnings-as-errors to catch any non-exhaustive `case` the compiler flags as a missing clause.

```bash
mix test
```

All 11 tests pass.

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== HttpStatusClassifier: demo ===\n")

    result_1 = HttpStatusClassifier.Classifier.classify({:ok, 200})
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = HttpStatusClassifier.Classifier.classify({:ok, 299})
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = HttpStatusClassifier.Classifier.classify({:ok, 301})
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of classify/1 over 1M responses
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 30ms total; each classification ~30ns**.

## Trade-offs

| Style | When to pick |
|---|---|
| `case` with nested patterns + guards | Shaped data, finite categories (this exercise) |
| Multi-clause functions | Same idea, but the dispatch is the entire function body |
| `cond` | Conditions are booleans computed from runtime values, not shapes |
| `if/else` | Exactly two branches, boolean condition |

**When NOT to use deep pattern matching in `case`:**

- **Shapes you do not control.** If the input is arbitrary JSON, decode it to a known
  struct first. Do not match on 4-level-deep map patterns — one missing key and the
  clause silently falls through.
- **Most of the logic is inside each clause.** If every clause runs 20 lines of code,
  extract them into named functions and call them from short clauses.

---

## Common production mistakes

**1. Guard order matters within a single clause**
`when status in 200..299 and is_integer(status)` — the range check already implies
integer-ness. Redundant guards are not wrong but obscure intent.

**2. Missing catch-all**
`case` without a final `_` clause raises `CaseClauseError` on unexpected input. In library
code that may be desirable (fail fast). In user-facing code it is almost always a bug.

**3. Shadowing outer variables**
`case x do y -> y end` — `y` is a **new binding**, not a comparison with an outer `y`.
To compare with an outer variable, use the pin operator: `case x do ^y -> ... end`.

**4. Using guards to do computation**
Guards are restricted to a small whitelist of BIFs. `when Enum.count(list) > 3` does
not compile. If you need that, compute it before the `case` or match on the head/tail.

**5. Overly broad catch-all**
`_ -> :unknown` silently swallows bugs. Log the unexpected shape in the catch-all when
the category is `:unknown` — future-you will thank present-you.

---

## Reflection

Your team adds a rule: classify `429 Too Many Requests` as `:retry_later` only if the `Retry-After` header is present. Where does the new rule go in the `case`, and does its position matter? Why?

How would you unit-test each classification branch without building an HTTP response? What does that imply about input types?

```elixir
defmodule HttpStatusClassifier.Classifier do
  @moduledoc """
  Classifies HTTP-shaped results into actionable categories.

  Input shapes:
    {:ok, status}                         — response received
    {:error, {:http, status}}             — response received, non-2xx (e.g. 4xx/5xx)
    {:error, {:network, reason}}          — transport failure (timeout, closed)
    anything else                         — :unknown
  """

  @type http_result ::
          {:ok, non_neg_integer()}
          | {:error, {:http, non_neg_integer()}}
          | {:error, {:network, atom()}}

  @type category ::
          :success
          | :redirect
          | :client_error
          | :server_error
          | :network_timeout
          | :network_closed
          | :unknown

  @spec classify(http_result() | term()) :: category()
  def classify(result) do
    case result do
      # 2xx — success. Guard narrows the range inside the tuple.
      {:ok, status} when status in 200..299 ->
        :success

      # 3xx — redirect (still an :ok tuple because the transport succeeded).
      {:ok, status} when status in 300..399 ->
        :redirect

      # 4xx — client error. Note the nested pattern: {:error, {:http, status}}.
      {:error, {:http, status}} when status in 400..499 ->
        :client_error

      # 5xx — server error. Same nested shape, different range.
      {:error, {:http, status}} when status in 500..599 ->
        :server_error

      # Network-level failures — reason is an atom, not a number.
      # Two clauses because different reasons warrant different retry strategies.
      {:error, {:network, :timeout}} ->
        :network_timeout

      {:error, {:network, :closed}} ->
        :network_closed

      # Catch-all — never silently drop malformed input in production.
      _ ->
        :unknown
    end
  end
end
```

## Resources

- [Elixir — Case, cond, and if](https://hexdocs.pm/elixir/case-cond-and-if.html)
- [Guards reference](https://hexdocs.pm/elixir/patterns-and-guards.html#guards)
- [Pin operator](https://hexdocs.pm/elixir/pattern-matching.html#the-pin-operator)

---

## Why `case` with Nested Patterns and Guards matters

Mastering **`case` with Nested Patterns and Guards** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/http_status_classifier.ex`

```elixir
defmodule HttpStatusClassifier do
  @moduledoc """
  Reference implementation for `case` with Nested Patterns and Guards.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the http_status_classifier module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> HttpStatusClassifier.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/http_status_classifier_test.exs`

```elixir
defmodule HttpStatusClassifierTest do
  use ExUnit.Case, async: true

  doctest HttpStatusClassifier

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert HttpStatusClassifier.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. Case Patterns Can Be Arbitrarily Complex
You can use nested structures, ranges, guards, and multiple patterns. Each clause is tried in order until one matches.

### 2. Pattern Matching Exhaustiveness
If you don't handle all cases, a `CaseClauseError` is raised at runtime. Dialyzer can warn about missing patterns. Always include a catch-all `_`.

### 3. Case vs If for Complex Logic
For logic with many branches, `case` is cleaner than nested `if` statements. Pattern matching makes intent clear.

---
