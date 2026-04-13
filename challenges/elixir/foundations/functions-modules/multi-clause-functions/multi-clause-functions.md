# Multi-Clause Functions and Pattern Matching on Arguments

**Project**: `order_state_machine` — transitions an order through `pending → paid → shipped → delivered`

---

## The business problem

Every valid order transition is a **pair of states**: `(from, event) → to`. That pair
matches cleanly to a multi-clause function, and invalid transitions become a single
catch-all clause that returns `{:error, :invalid_transition}`. No conditionals, no
lookup tables — just pattern matching.

---

## Project structure

```
order_state_machine/
├── lib/
│   └── order_state_machine/
│       └── order.ex         # transition/2 with one clause per valid transition
├── script/
│   └── main.exs
├── test/
│   └── order_state_machine_test.exs
└── mix.exs
```

---

## What you will learn

1. **Multi-clause functions** — multiple `def` blocks with the same name and arity,
   dispatched by pattern matching on the arguments.
2. **Clause ordering matters** — the compiler tries them top to bottom; the catch-all
   must come last.

---

## The concept in 60 seconds

A single function name can have many clauses. Elixir matches arguments top to bottom
and runs the first matching clause:

```elixir
def greet(:admin),           do: "Hello, boss"
def greet(name) when is_binary(name), do: "Hi, #{name}"
def greet(_),                do: "Unknown caller"
```

This is the Elixir way of doing what OO languages do with `switch` or dispatch tables —
but it is checked at compile time (unreachable clauses warn) and pattern-matches on
structure, not just equality.

---

## Why a state machine is the canonical example

Every valid order transition is a **pair of states**: `(from, event) → to`. That pair
matches cleanly to a multi-clause function, and invalid transitions become a single
catch-all clause that returns `{:error, :invalid_transition}`. No conditionals, no
lookup tables — just pattern matching.

---

## Why multi-clause and not a `case` block

- A `case` inside `transition/2` works, but pushes all dispatch into one giant expression where every branch shares a scope — accidents happen when a variable from one branch is reused.
- Multi-clause dispatch gives each transition its own function head with its own bindings. The compiler warns on unreachable clauses, which `case` does not do as aggressively.
- A lookup `Map` works too, but loses guards and can't encode conditions like "cancel only from pending" without extra glue.

---

## Design decisions

**Option A — single function with `case {from, event} do ... end`**
- Pros: one function body to read; trivial to add logging around the dispatch.
- Cons: long `case` arms drift; compiler warns less aggressively on dead branches; pattern guards become nested.

**Option B — multi-clause `transition/2` with catch-all last** (chosen)
- Pros: each valid transition is its own line; the catch-all makes invalid transitions a single explicit error path; compiler flags unreachable clauses.
- Cons: adding dozens of states bloats the module; clause order becomes load-bearing.

→ Chose **B** because the state space here is small (< 10 transitions) and each transition reads as a data point. For a 100-transition machine, a lookup map or `:gen_statem` wins.

---

## Implementation

### `mix.exs`
```elixir
defmodule OrderStateMachine.MixProject do
  use Mix.Project

  def project do
    [
      app: :order_state_machine,
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

**Objective**: Build minimal library so multi-clause dispatch pattern matching IS the state machine without extra glue.

```bash
mix new order_state_machine
cd order_state_machine
```

### Step 2 — `lib/order_state_machine/order.ex`

**Objective**: Encode valid transitions as clauses, catch-all returns error so caller sees {:error, :invalid_transition} not FunctionClauseError.

```elixir
defmodule OrderStateMachine.Order do
  @moduledoc """
  Order transitions: pending -> paid -> shipped -> delivered.

  Also: pending -> cancelled, paid -> refunded (terminal states).

  Every valid transition is ONE clause of transition/2. Anything else falls
  through to the catch-all and returns `{:error, :invalid_transition}`.
  """

  @type state :: :pending | :paid | :shipped | :delivered | :cancelled | :refunded
  @type event :: :pay | :ship | :deliver | :cancel | :refund

  @spec transition(state(), event()) ::
          {:ok, state()} | {:error, :invalid_transition}

  # Happy path — forward transitions
  def transition(:pending, :pay),    do: {:ok, :paid}
  def transition(:paid, :ship),      do: {:ok, :shipped}
  def transition(:shipped, :deliver), do: {:ok, :delivered}

  # Side paths — cancellation / refund
  def transition(:pending, :cancel), do: {:ok, :cancelled}
  def transition(:paid, :refund),    do: {:ok, :refunded}

  # Catch-all: any (state, event) not enumerated above is invalid.
  # Why two underscore args and not `_, _`: explicit names document the shape
  # at the call site when reading this clause in isolation.
  def transition(_state, _event), do: {:error, :invalid_transition}

  @doc """
  Runs a list of events. Stops at the first error, short-circuiting.
  Uses Enum.reduce_while to bail out cleanly.
  """
  @spec run(state(), [event()]) :: {:ok, state()} | {:error, :invalid_transition}
  def run(initial, events) do
    Enum.reduce_while(events, {:ok, initial}, fn event, {:ok, state} ->
      case transition(state, event) do
        {:ok, next}     -> {:cont, {:ok, next}}
        {:error, _} = e -> {:halt, e}
      end
    end)
  end
end
```

### Step 3 — `test/order_state_machine_test.exs`

**Objective**: Test invalid transitions so catch-all clause is proved to fire and Enum.reduce_while short-circuits correctly.

```elixir
defmodule OrderStateMachineTest do
  use ExUnit.Case, async: true
  doctest OrderStateMachine.Order

  alias OrderStateMachine.Order

  describe "transition/2 — valid transitions" do
    test "pending -> paid" do
      assert Order.transition(:pending, :pay) == {:ok, :paid}
    end

    test "paid -> shipped" do
      assert Order.transition(:paid, :ship) == {:ok, :shipped}
    end

    test "shipped -> delivered" do
      assert Order.transition(:shipped, :deliver) == {:ok, :delivered}
    end

    test "pending -> cancelled" do
      assert Order.transition(:pending, :cancel) == {:ok, :cancelled}
    end

    test "paid -> refunded" do
      assert Order.transition(:paid, :refund) == {:ok, :refunded}
    end
  end

  describe "transition/2 — invalid transitions" do
    test "cannot ship a pending order (must pay first)" do
      assert Order.transition(:pending, :ship) == {:error, :invalid_transition}
    end

    test "cannot refund a shipped order" do
      assert Order.transition(:shipped, :refund) == {:error, :invalid_transition}
    end

    test "terminal state cannot transition" do
      assert Order.transition(:delivered, :ship) == {:error, :invalid_transition}
    end
  end

  describe "run/2 — sequence of events" do
    test "complete happy path" do
      assert Order.run(:pending, [:pay, :ship, :deliver]) == {:ok, :delivered}
    end

    test "short-circuits on first invalid event" do
      assert Order.run(:pending, [:pay, :refund, :ship]) == {:ok, :refunded} or
               Order.run(:pending, [:pay, :deliver]) == {:error, :invalid_transition}

      # The second assertion is the one that exercises short-circuit
      assert Order.run(:pending, [:pay, :deliver]) == {:error, :invalid_transition}
    end
  end
end
```

### Step 4 — Run the tests

**Objective**: Verify reduce_while + multi-clause dispatch halts on first invalid transition without draining remaining events.

```bash
mix test
```

All 10 tests pass.

### Why this works

Each clause is a compiled decision tree branch — the BEAM compiles multi-clause functions into a single jump table when clauses are simple literals like atoms, so dispatch is O(1), not O(n) over the clause list. The catch-all at the end converts every unmatched `(state, event)` pair into a uniform error tuple, so callers never see `FunctionClauseError`. `reduce_while/3` lets `run/2` short-circuit on the first error without an accumulator flag.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== OrderStateMachine: demo ===\n")

    result_1 = OrderStateMachine.Order.transition(:pending, :pay)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = OrderStateMachine.Order.transition(:paid, :ship)
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = OrderStateMachine.Order.transition(:shipped, :deliver)
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
# bench/transitions.exs
{t_valid, _} = :timer.tc(fn ->
  Enum.each(1..1_000_000, fn _ ->
    OrderStateMachine.Order.transition(:pending, :pay)
  end)
end)

{t_invalid, _} = :timer.tc(fn ->
  Enum.each(1..1_000_000, fn _ ->
    OrderStateMachine.Order.transition(:delivered, :ship)
  end)
end)

IO.puts("valid: #{t_valid} µs   invalid: #{t_invalid} µs")
```

Target: < 0.5 µs per call on modern hardware. Valid and invalid paths should be within noise of each other — the BEAM's decision-tree dispatch treats the catch-all as one more branch.

---

## Trade-offs

| Style | When to pick |
|---|---|
| Multi-clause functions | Small, fixed set of cases (≤ ~15), clear structure |
| `case` inside a single function | Logic depends on runtime computation, not just arg shape |
| Lookup map `%{{:pending, :pay} => :paid}` | Transitions are data-driven (loaded from DB/config) |
| `gen_statem` / `:gen_statem` | Stateful process, timeouts, side effects per transition |

**When NOT to use multi-clause functions:**

- **Clauses share most of their body.** If every clause calls the same helper with one
  differing constant, prefer `case` or a lookup table. Duplicated bodies drift over time.
- **Dozens of clauses.** The compiler handles them fine, but readers cannot. Switch to
  a data-driven approach.

---

## Common production mistakes

**1. Catch-all placed first**
A `def f(_, _), do: ...` above specific clauses makes the specific clauses unreachable.
The compiler warns — never ignore "this clause cannot match" warnings.

**2. Forgetting the catch-all**
Without it, an unmatched call raises `FunctionClauseError`. That may be what you want
(fail fast) or not (graceful `{:error, :invalid}`). Be explicit.

**3. Mixing multi-clause with default arguments incorrectly**
When a function has defaults **and** multiple bodies, the compiler requires a header clause
(`def f(a, b \\ :x)` with no body) before the real clauses. Declaring a default inside one
of the bodies is a compile error.

**4. Overloading clauses with subtly different types**
`def f(n) when is_integer(n)` and `def f(s) when is_binary(s)` side by side is fine,
but hides two different operations behind one name. Often two functions (`f_int/1`,
`f_str/1`) read better.

**5. Using clauses for validation only**
`def save(%User{email: nil}), do: {:error, ...}` scattered across 10 clauses becomes a
validation maze. Validate once upstream with `with`, then call a clean `save/1`.

---

## Reflection

- Product asks you to add an **audit trail** (log every transition, valid or invalid). Where does the logging go — inside each clause, wrapped around `transition/2`, or in `run/2`? Which choice keeps the clauses readable?
- Your state machine now has 40 transitions loaded from a config file. Multi-clause functions stop scaling (clauses must be literal at compile time). Would you switch to a lookup `Map`, a `:gen_statem`, or a hybrid? What does each cost you?

---

## Resources

- [Elixir — Pattern matching](https://hexdocs.pm/elixir/pattern-matching.html)
- [Elixir — Case, cond, and if](https://hexdocs.pm/elixir/case-cond-and-if.html)
- [`:gen_statem` docs](https://www.erlang.org/doc/man/gen_statem.html) — when you outgrow plain functions

---

## Why Multi-Clause Functions and Pattern Matching on Arguments matters

Mastering **Multi-Clause Functions and Pattern Matching on Arguments** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/order_state_machine.ex`

```elixir
defmodule OrderStateMachine do
  @moduledoc """
  Reference implementation for Multi-Clause Functions and Pattern Matching on Arguments.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the order_state_machine module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> OrderStateMachine.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/order_state_machine_test.exs`

```elixir
defmodule OrderStateMachineTest do
  use ExUnit.Case, async: true

  doctest OrderStateMachine

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert OrderStateMachine.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. Each Clause is a Separate Pattern Match
Elixir tries each clause in order. The first one whose pattern and guards match executes. This is more powerful than `if/else`.

### 2. Clause Order Matters
If a later clause's pattern is more general, it will never match. Put more specific patterns first.

### 3. Guard Clauses Refine Patterns
Guards let one pattern match multiple branches without creating separate clauses. `when is_integer(x)` vs `when is_binary(x)`.

---
