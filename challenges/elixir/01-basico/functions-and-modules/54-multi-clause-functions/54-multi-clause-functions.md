# Multi-Clause Functions and Pattern Matching on Arguments

**Project**: `order_state_machine` — transitions an order through `pending → paid → shipped → delivered`

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project structure

```
order_state_machine/
├── lib/
│   └── order_state_machine/
│       └── order.ex         # transition/2 with one clause per valid transition
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

## Implementation

### Step 1 — Create the project

```bash
mix new order_state_machine
cd order_state_machine
```

### Step 2 — `lib/order_state_machine/order.ex`

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

```elixir
defmodule OrderStateMachineTest do
  use ExUnit.Case, async: true

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

```bash
mix test
```

All 10 tests pass.

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
See exercise 53 — defaults must go in a header clause when you also have multiple bodies.

**4. Overloading clauses with subtly different types**
`def f(n) when is_integer(n)` and `def f(s) when is_binary(s)` side by side is fine,
but hides two different operations behind one name. Often two functions (`f_int/1`,
`f_str/1`) read better.

**5. Using clauses for validation only**
`def save(%User{email: nil}), do: {:error, ...}` scattered across 10 clauses becomes a
validation maze. Validate once upstream with `with`, then call a clean `save/1`.

---

## Resources

- [Elixir — Pattern matching](https://hexdocs.pm/elixir/pattern-matching.html)
- [Elixir — Case, cond, and if](https://hexdocs.pm/elixir/case-cond-and-if.html)
- [`:gen_statem` docs](https://www.erlang.org/doc/man/gen_statem.html) — when you outgrow plain functions
