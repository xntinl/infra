# Variable Rebinding and Scope: A Config Reloader Demo

**Project**: `config_reloader_demo` — shows why rebinding a variable inside a function never mutates the caller's data

---

## Project structure

```
config_reloader_demo/
├── lib/
│   └── config_reloader_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── config_reloader_demo_test.exs
└── mix.exs
```

---

## Core concepts

Elixir allows variable **rebinding** — writing `x = 1` then `x = 2` is legal.
But this is NOT mutation. The second assignment binds the name `x` to a new
value; any reference to the old value elsewhere is untouched.

Coming from Java/Python, the instinct is "passing a map to a function lets it
mutate the map". In Elixir this is impossible. Data is immutable. Functions
return new values; callers keep the original unless they rebind their own
variable to the result.

The second concept is **scope**. `case`, `if`, `cond`, and anonymous functions
introduce a new scope. Rebinding `x` inside an `if` does NOT propagate to the
outer scope. This catches senior devs off-guard constantly.

---

## The business problem

A config reloader reads a config map, adds a derived field (expiry timestamp),
and returns the updated map. Callers who forget to use the return value end
up with stale config. The demo makes the rebinding semantics obvious.

---

## Why explicit return + reassignment in caller and not process dictionary or ETS for "global mutable state"

Process dictionary breaks referential transparency and makes code untestable. Returning new values and reassigning at the call site keeps data flow explicit and functions pure.

## Design decisions

**Option A — rebind within function scope, return new value**
- Pros: Pure-functional, safe across processes, trivial to test
- Cons: Beginners confused that `x = x + 1` inside a function doesn't change the caller's `x`

**Option B — try to mutate caller's binding** (chosen)
- Pros: Would match imperative expectations from other languages
- Cons: Impossible in Elixir — bindings are lexical and immutable references; would require process state or ETS

→ Chose **A** because immutability is the foundation of BEAM concurrency — any other choice breaks the model.

## Implementation

### `mix.exs`
```elixir
defmodule ConfigReloaderDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :config_reloader_demo,
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
### `lib/config_reloader_demo.ex`

```elixir
defmodule ConfigReloaderDemo do
  @moduledoc """
  Demonstrates rebinding vs mutation and scope rules.

  Every function returns a NEW map. The caller's original map is never
  modified, regardless of what happens inside the function.
  """

  @type config :: %{required(:name) => String.t(), optional(atom()) => term()}

  @doc """
  "Reloads" a config by adding a timestamp and version bump.

  Inside this function we rebind `config` three times. None of those
  rebindings affect the caller — the caller still holds the original map.
  """
  @spec reload(config()) :: config()
  def reload(config) when is_map(config) do
    # Rebinding: the name `config` now points to a new map.
    # The caller's variable is unchanged.
    config = Map.put(config, :reloaded_at, System.system_time(:second))
    config = Map.update(config, :version, 1, &(&1 + 1))
    config = Map.put(config, :status, :active)
    config
  end

  @doc """
  Shows that an inner rebinding inside `if` does NOT leak out.

  In Ruby/Python, reassigning inside a conditional would leak. In Elixir
  the outer `status` is untouched by the inner rebinding.
  """
  @spec describe_status(config()) :: String.t()
  def describe_status(config) do
    status = :unknown

    # `status` rebinding inside `if` lives in the `if` scope.
    # Elixir emits a compiler warning if you ignore the returned value.
    status =
      if Map.get(config, :active, false) do
        :running
      else
        status
      end

    Atom.to_string(status)
  end

  @doc """
  Pipeline style — idiomatic Elixir.

  Instead of rebinding `config` line by line, thread the value through
  `|>`. Each step returns a new map; no intermediate names needed.
  """
  @spec reload_pipeline(config()) :: config()
  def reload_pipeline(config) do
    config
    |> Map.put(:reloaded_at, System.system_time(:second))
    |> Map.update(:version, 1, &(&1 + 1))
    |> Map.put(:status, :active)
  end

  @doc """
  Demonstrates that `case` clauses have their own scope.

  The `value` bound inside a clause does NOT leak to the outer function.
  Each clause binds its own `value`.
  """
  @spec classify(integer()) :: {String.t(), integer()}
  def classify(n) when is_integer(n) do
    label =
      case n do
        value when value < 0 -> "negative: #{value}"
        0 -> "zero"
        value when value > 100 -> "large: #{value}"
        value -> "small: #{value}"
      end

    # `value` is NOT in scope here — it was bound inside `case` clauses.
    # Returning `n` (the original parameter) proves the point.
    {label, n}
  end
end
```
### `test/config_reloader_demo_test.exs`

```elixir
defmodule ConfigReloaderDemoTest do
  use ExUnit.Case, async: true
  doctest ConfigReloaderDemo

  alias ConfigReloaderDemo

  describe "reload/1" do
    test "returns a new map with added fields" do
      original = %{name: "api", version: 3}
      reloaded = ConfigReloaderDemo.reload(original)

      assert reloaded.version == 4
      assert reloaded.status == :active
      assert is_integer(reloaded.reloaded_at)
    end

    test "caller's original map is unchanged" do
      # This is the whole point: immutability.
      original = %{name: "api", version: 3}
      _ignored = ConfigReloaderDemo.reload(original)

      assert original == %{name: "api", version: 3}
      refute Map.has_key?(original, :status)
      refute Map.has_key?(original, :reloaded_at)
    end

    test "defaults version to 1 when missing" do
      assert %{version: 1} = ConfigReloaderDemo.reload(%{name: "svc"})
    end
  end

  describe "reload_pipeline/1" do
    test "produces equivalent result to reload/1" do
      config = %{name: "api", version: 10}
      a = ConfigReloaderDemo.reload(config)
      b = ConfigReloaderDemo.reload_pipeline(config)

      # Compare everything except the timestamp which may differ by a second.
      assert Map.delete(a, :reloaded_at) == Map.delete(b, :reloaded_at)
    end
  end

  describe "describe_status/1" do
    test "active config returns running" do
      assert ConfigReloaderDemo.describe_status(%{name: "x", active: true}) == "running"
    end

    test "inactive config keeps outer status" do
      # Proves that the inner rebinding didn't overwrite the outer :unknown
      # in the false branch.
      assert ConfigReloaderDemo.describe_status(%{name: "x", active: false}) == "unknown"
    end
  end

  describe "classify/1" do
    test "case clause bindings stay local" do
      assert {"negative: -5", -5} = ConfigReloaderDemo.classify(-5)
      assert {"zero", 0} = ConfigReloaderDemo.classify(0)
      assert {"small: 42", 42} = ConfigReloaderDemo.classify(42)
      assert {"large: 500", 500} = ConfigReloaderDemo.classify(500)
    end
  end
end
```
### Run it

```bash
mix new config_reloader_demo
cd config_reloader_demo
mix test
```

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== ConfigReloaderDemo: demo ===\n")

    result_1 = ConfigReloaderDemo.describe_status(%{name: "x", active: true})
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = ConfigReloaderDemo.describe_status(%{name: "x", active: false})
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = Mix.env()
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

Create a file `lib/transaction.ex` with the `Transaction` module above, then test in `iex`:

```elixir
defmodule Transaction do
  def new(amount) when is_number(amount) and amount >= 0 do
    %{amount: amount, status: :pending, metadata: %{}}
  end

  def approve(%{status: :pending} = tx) do
    %{tx | status: :approved}
  end

  def approve(tx), do: tx

  def add_metadata(%{metadata: meta} = tx, key, value) do
    %{tx | metadata: Map.put(meta, key, value)}
  end

  def process_value(%{status: :approved, amount: amt} = tx) do
    %{tx | status: :processed, metadata: Map.put(tx.metadata, :processed_at, DateTime.utc_now())}
  end

  def process_value(tx), do: tx

  def fail(%{status: status} = tx, reason) when status in [:pending, :approved] do
    %{tx | status: :failed, metadata: Map.put(tx.metadata, :error, reason)}
  end

  def fail(tx, _reason), do: tx

  def status(%{status: s}), do: s
end

# Demonstrate rebinding and function composition
tx = Transaction.new(100.0)
IO.inspect(tx)  # %{amount: 100.0, status: :pending, metadata: %{}}

tx = Transaction.add_metadata(tx, :user_id, 42)
tx = Transaction.add_metadata(tx, :ref, "TXN-001")
IO.inspect(tx)  # metadata now has both keys

tx = Transaction.approve(tx)
IO.inspect(tx.status)  # :approved

tx = Transaction.process_value(tx)
IO.inspect(tx.status)  # :processed
IO.puts("Transaction processed at: #{inspect(tx.metadata[:processed_at])}")

# Test failure path
tx2 = Transaction.new(50.0)
tx2 = Transaction.fail(tx2, "insufficient_funds")
IO.inspect(tx2.status)  # :failed
IO.inspect(tx2.metadata[:error])  # "insufficient_funds"
```
---

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production mistakes

**1. Forgetting to capture the return value**
```elixir
def process_request(conn) do
  Plug.Conn.put_resp_header(conn, "x-trace", "abc")  # Bug: return ignored
  conn  # returns unmodified conn
end
```
The compiler warns about unused results for most stdlib functions. Listen.

**2. Rebinding inside `if` expecting it to leak out**
```elixir
result = :start
if some_condition, do: (result = :changed)
result  # still :start
```
Always assign the `if` expression's return value: `result = if cond, do: ...`.

**3. Shadowing in `for` and anonymous functions**
`Enum.map(items, fn item -> ... end)` — `item` inside the fn shadows any
outer `item`. Fine, but rename for clarity in nested comprehensions.

**4. "Mutation" illusion with ETS or processes**
GenServer state looks mutable because the same PID holds different state over
time. It is not — each callback returns a new state that replaces the old one.

## When NOT to rebind

- Inside a pipeline, rebinding breaks the flow. Use `|>` all the way.
- When testing immutability invariants — rebinding the test subject hides bugs.
- In complex `with` chains — rebinding the same name across steps defeats the
  purpose of named intermediate values.

---

## Reflection

If you come from Python/JS, name two situations where you'd instinctively reach for mutation and describe how you'd express them functionally in Elixir (accumulator, pipeline, `Enum.reduce`).

How does rebinding differ from mutation at the BEAM level? What does this mean for garbage collection of the old value?

## Resources

- [Pattern matching and rebinding](https://hexdocs.pm/elixir/pattern-matching.html)
- [The match operator](https://elixir-lang.org/getting-started/pattern-matching.html)
- [Scope rules — Elixir docs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#case/2)

---

## Why Variable Rebinding and Scope matters

Mastering **Variable Rebinding and Scope** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. Rebinding Creates a New Binding, Not Mutation

The second `x = x + 1` does not mutate; it creates a new variable `x` in the same scope that shadows the old binding. The old `x` is still there if a function captured it. Closures capture by reference, not by value. If you rely on mutation after binding, you will have bugs.

### 2. Scope Follows Function Boundaries, Not Blocks

Unlike JavaScript or Python, scope in Elixir is determined by function definitions, not by `if`, `case`, or loops. Both branches of an `if` statement bind to the same variable at the function level. This prevents "variable shadowing in one branch" bugs that plague block-scoped languages.

### 3. Pattern Matching is Rebinding

When you write `{x, y} = {1, 2}`, you are binding new variables. If `x` was already bound, pattern matching will attempt to unify (with the pin operator `^`) or create a new binding. Understanding the scoping rules prevents subtle rebinding bugs.

---
