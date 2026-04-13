# Atoms and Symbols: Building an Order State Machine

**Project**: `order_fsm` — a finite state machine for order processing using atoms as states

---

## Why atoms are an architectural decision

Atoms in Elixir are constants whose name is their value. They live in a global
atom table — a fixed-size, never-garbage-collected lookup structure in the BEAM VM.
This makes them perfect for representing a fixed set of known states, but dangerous
when created from external input.

For a senior developer coming from Java or Go, atoms replace the role of enums,
string constants, and sentinel values. The critical difference: atom comparison is
O(1) pointer comparison, not O(n) byte comparison. With thousands of state transitions
per second, this matters.

The `{:ok, value}` / `{:error, reason}` pattern appears in every Elixir API because
atoms make error handling explicit and pattern-matchable. You cannot accidentally
ignore an `:error` the way you can ignore a `null` return in other languages.

---

## The business problem

An e-commerce system needs to track order lifecycle with strict state transitions.
Not every transition is valid — you cannot ship a cancelled order, and you cannot
cancel a delivered order. The state machine must:

1. Define valid states as atoms
2. Enforce valid transitions with pattern matching
3. Safely parse state strings from external systems (webhooks, APIs)
4. Never create atoms from untrusted input

---

## Project structure

```
order_fsm/
├── lib/
│   └── order_fsm/
│       ├── order.ex
│       └── state.ex
├── test/
│   └── order_fsm/
│       ├── order_test.exs
│       └── state_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Design decisions

**Option A — atom-based state machine with `String.to_existing_atom/1`**
- Pros: Compile-time safety via `@valid_states`, O(1) comparisons, idiomatic Elixir
- Cons: Must be careful with external input (atom table is never GC'd)

**Option B — string-based state machine with explicit pattern matching on strings** (chosen)
- Pros: No atom table pressure, safe for untrusted input by default
- Cons: Slower comparisons, loses pattern-matching ergonomics, each state transition is a string compare

→ Chose **A** because the order lifecycle has a fixed, known set of states and we validate external input through `parse/1` before any atom creation.

## Implementation

### `mix.exs`
```elixir
defmodule OrderFsm.MixProject do
  use Mix.Project

  def project do
    [
      app: :order_fsm,
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
### `lib/order_fsm.ex`

```elixir
defmodule OrderFsm do
  @moduledoc """
  Atoms and Symbols: Building an Order State Machine.

  Atoms in Elixir are constants whose name is their value. They live in a global.
  """
end
```
### `lib/order_fsm/state.ex`

```elixir
defmodule OrderFsm.State do
  @moduledoc """
  Defines valid order states and transitions.

  States are atoms representing the order lifecycle:
  :pending -> :confirmed -> :shipped -> :delivered
                         -> :cancelled (from pending or confirmed only)

  External input (API, webhooks) arrives as strings and must be
  converted via parse/1 — never with String.to_atom/1.
  """

  @valid_states [:pending, :confirmed, :shipped, :delivered, :cancelled]

  @transitions %{
    pending: [:confirmed, :cancelled],
    confirmed: [:shipped, :cancelled],
    shipped: [:delivered],
    delivered: [],
    cancelled: []
  }

  @doc """
  Returns all valid order states.
  """
  @spec valid_states() :: [atom()]
  def valid_states, do: @valid_states

  @doc """
  Returns the allowed next states from a given state.

  ## Examples

      iex> OrderFsm.State.allowed_transitions(:pending)
      [:confirmed, :cancelled]

      iex> OrderFsm.State.allowed_transitions(:delivered)
      []

  """
  @spec allowed_transitions(atom()) :: [atom()]
  def allowed_transitions(state) when state in @valid_states do
    Map.fetch!(@transitions, state)
  end

  @doc """
  Checks if a transition from one state to another is valid.

  ## Examples

      iex> OrderFsm.State.valid_transition?(:pending, :confirmed)
      true

      iex> OrderFsm.State.valid_transition?(:delivered, :cancelled)
      false

  """
  @spec valid_transition?(atom(), atom()) :: boolean()
  def valid_transition?(from, to)
      when from in @valid_states and to in @valid_states do
    to in Map.fetch!(@transitions, from)
  end

  def valid_transition?(_, _), do: false

  @doc """
  Safely parses a string into a valid state atom.

  Uses String.to_existing_atom/1 so it never creates new atoms from
  external input. Then verifies the atom is actually a valid state —
  not just any atom that happens to exist in the VM.

  ## Examples

      iex> OrderFsm.State.parse("confirmed")
      {:ok, :confirmed}

      iex> OrderFsm.State.parse("hacked_value")
      {:error, :unknown_state}

  """
  @spec parse(String.t()) :: {:ok, atom()} | {:error, :unknown_state}
  def parse(string) when is_binary(string) do
    atom =
      try do
        String.to_existing_atom(string)
      rescue
        ArgumentError in RuntimeError -> nil
      end

    if atom in @valid_states do
      {:ok, atom}
    else
      {:error, :unknown_state}
    end
  end

  @doc """
  Returns true if the state is terminal (no further transitions possible).

  ## Examples

      iex> OrderFsm.State.terminal?(:delivered)
      true

      iex> OrderFsm.State.terminal?(:pending)
      false

  """
  @spec terminal?(atom()) :: boolean()
  def terminal?(state) when state in @valid_states do
    Map.fetch!(@transitions, state) == []
  end
end
```
### `lib/order_fsm/order.ex`

```elixir
defmodule OrderFsm.Order do
  @moduledoc """
  Represents an order and manages state transitions.

  Each transition is validated against the state machine rules.
  Invalid transitions return {:error, reason} instead of raising,
  following the Elixir convention for expected failures.
  """

  @type t :: %{
          id: String.t(),
          state: atom(),
          items: [String.t()],
          history: [{atom(), atom(), DateTime.t()}]
        }

  @doc """
  Creates a new order in the :pending state.

  ## Examples

      iex> order = OrderFsm.Order.new("ORD-001", ["Widget A", "Gadget B"])
      iex> order.state
      :pending

  """
  @spec new(String.t(), [String.t()]) :: t()
  def new(id, items) when is_binary(id) and is_list(items) do
    %{
      id: id,
      state: :pending,
      items: items,
      history: []
    }
  end

  @doc """
  Transitions an order to a new state.

  Records the transition in the history with a timestamp.
  Returns {:error, reason} if the transition is not allowed.

  ## Examples

      iex> order = OrderFsm.Order.new("ORD-001", ["Widget"])
      iex> {:ok, confirmed} = OrderFsm.Order.transition(order, :confirmed)
      iex> confirmed.state
      :confirmed

      iex> order = OrderFsm.Order.new("ORD-001", ["Widget"])
      iex> OrderFsm.Order.transition(order, :delivered)
      {:error, :invalid_transition}

  """
  @spec transition(t(), atom()) :: {:ok, t()} | {:error, :invalid_transition}
  def transition(%{state: current} = order, target) do
    if OrderFsm.State.valid_transition?(current, target) do
      now = DateTime.utc_now()

      updated =
        order
        |> Map.put(:state, target)
        |> Map.update!(:history, &[{current, target, now} | &1])

      {:ok, updated}
    else
      {:error, :invalid_transition}
    end
  end

  @doc """
  Applies a sequence of transitions, stopping at the first failure.

  Returns {:ok, final_order} if all transitions succeed, or
  {:error, reason, last_successful_order} if any fails.

  ## Examples

      iex> order = OrderFsm.Order.new("ORD-001", ["Widget"])
      iex> {:ok, delivered} = OrderFsm.Order.transition_chain(order, [:confirmed, :shipped, :delivered])
      iex> delivered.state
      :delivered

  """
  @spec transition_chain(t(), [atom()]) ::
          {:ok, t()} | {:error, :invalid_transition, t()}
  def transition_chain(order, states) do
    Enum.reduce_while(states, {:ok, order}, fn target, {:ok, current} ->
      case transition(current, target) do
        {:ok, updated} -> {:cont, {:ok, updated}}
        {:error, reason} -> {:halt, {:error, reason, current}}
      end
    end)
  end

  @doc """
  Returns the transition history in chronological order.
  """
  @spec history(t()) :: [{atom(), atom(), DateTime.t()}]
  def history(%{history: history}), do: Enum.reverse(history)
end
```
### `test/order_fsm_test.exs`
```elixir
defmodule OrderFsm.StateTest do
  use ExUnit.Case, async: true
  doctest OrderFsm.Order

  alias OrderFsm.State

  describe "valid_transition?/2" do
    test "pending can move to confirmed" do
      assert State.valid_transition?(:pending, :confirmed)
    end

    test "pending can move to cancelled" do
      assert State.valid_transition?(:pending, :cancelled)
    end

    test "pending cannot jump to delivered" do
      refute State.valid_transition?(:pending, :delivered)
    end

    test "delivered cannot move anywhere" do
      refute State.valid_transition?(:delivered, :cancelled)
      refute State.valid_transition?(:delivered, :pending)
    end

    test "shipped can only move to delivered" do
      assert State.valid_transition?(:shipped, :delivered)
      refute State.valid_transition?(:shipped, :cancelled)
    end

    test "unknown states return false" do
      refute State.valid_transition?(:nonexistent, :pending)
    end
  end

  describe "parse/1" do
    test "parses all valid state strings" do
      for state <- State.valid_states() do
        string = Atom.to_string(state)
        assert {:ok, ^state} = State.parse(string)
      end
    end

    test "returns error for unknown state string" do
      assert {:error, :unknown_state} = State.parse("processing")
    end

    test "returns error for valid atoms that are not states" do
      assert {:error, :unknown_state} = State.parse("ok")
    end

    test "returns error for empty string" do
      assert {:error, :unknown_state} = State.parse("")
    end
  end

  describe "terminal?/1" do
    test "delivered is terminal" do
      assert State.terminal?(:delivered)
    end

    test "cancelled is terminal" do
      assert State.terminal?(:cancelled)
    end

    test "pending is not terminal" do
      refute State.terminal?(:pending)
    end
  end
end
```
```elixir
defmodule OrderFsm.OrderTest do
  use ExUnit.Case, async: true
  doctest OrderFsm.Order

  alias OrderFsm.Order

  describe "new/2" do
    test "creates order in pending state" do
      order = Order.new("ORD-001", ["Widget A"])
      assert order.state == :pending
      assert order.id == "ORD-001"
      assert order.items == ["Widget A"]
      assert order.history == []
    end
  end

  describe "transition/2" do
    test "valid transition updates state" do
      order = Order.new("ORD-001", ["Widget"])
      assert {:ok, confirmed} = Order.transition(order, :confirmed)
      assert confirmed.state == :confirmed
    end

    test "invalid transition returns error" do
      order = Order.new("ORD-001", ["Widget"])
      assert {:error, :invalid_transition} = Order.transition(order, :delivered)
    end

    test "records transition in history" do
      order = Order.new("ORD-001", ["Widget"])
      {:ok, confirmed} = Order.transition(order, :confirmed)
      assert [{:pending, :confirmed, %DateTime{}}] = confirmed.history
    end

    test "original order is not mutated" do
      order = Order.new("ORD-001", ["Widget"])
      {:ok, _confirmed} = Order.transition(order, :confirmed)
      assert order.state == :pending
    end
  end

  describe "transition_chain/2" do
    test "applies full lifecycle" do
      order = Order.new("ORD-001", ["Widget"])

      assert {:ok, delivered} =
               Order.transition_chain(order, [:confirmed, :shipped, :delivered])

      assert delivered.state == :delivered
      assert length(Order.history(delivered)) == 3
    end

    test "stops at first invalid transition" do
      order = Order.new("ORD-001", ["Widget"])

      assert {:error, :invalid_transition, partial} =
               Order.transition_chain(order, [:confirmed, :delivered])

      assert partial.state == :confirmed
    end
  end
end
```
### Run the tests

```bash
mix test --trace
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== OrderFsm: demo ===\n")

    result_1 = OrderFsm.State.allowed_transitions(:pending)
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = OrderFsm.State.allowed_transitions(:delivered)
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = OrderFsm.State.parse("confirmed")
    IO.puts("Demo 3: #{inspect(result_3)}")

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

## The atom table security problem

The BEAM atom table has a default limit of 1,048,576 atoms. Atoms are never garbage
collected. This creates a real denial-of-service vector:

```elixir
# NEVER do this with external input
def handle_webhook(%{"status" => status_string}) do
  status = String.to_atom(status_string)  # Creates a NEW atom every call
  # ...
end
```
An attacker sending unique status strings in API requests exhausts the atom table
and crashes the entire VM. The fix is always the same pattern:

1. Use `String.to_existing_atom/1` (raises if the atom does not exist)
2. Verify the atom is in your allowed set

Or skip atoms entirely and use a hardcoded `case` on the string:

```elixir
def parse_status("pending"), do: {:ok, :pending}
def parse_status("confirmed"), do: {:ok, :confirmed}
def parse_status(_), do: {:error, :unknown}
```
This approach creates no new atoms and compiles to efficient pattern matching.

---

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of transition/2
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```
Target: **< 1µs per transition on modern hardware**.

## Common production mistakes

**1. `true`, `false`, and `nil` are atoms**
`is_atom(true)` returns `true`. This means `:true == true` evaluates to `true`.
Do not confuse boolean atoms with your domain atoms.

**2. Atom ordering is alphabetical, not insertion order**
`[:declined, :approved, :pending] |> Enum.sort()` returns
`[:approved, :declined, :pending]`. Never rely on definition order.

**3. Atoms across nodes must match exactly**
In a distributed Erlang cluster, atoms are compared by name. If node A uses
`:confirmed` and node B uses `:Confirmed`, they are different atoms. Stick to
lowercase snake_case.

**4. Module names are atoms**
`is_atom(Enum)` returns `true`. Every module name is an atom prefixed with `Elixir.`
internally (`Enum` is actually `:"Elixir.Enum"`). This means module references
consume atom table entries too.

---

```elixir
defmodule OrderFsm.State do
  @valid_states [:pending, :confirmed, :shipped, :delivered, :cancelled]
  @transitions %{
    pending: [:confirmed, :cancelled],
    confirmed: [:shipped],
    shipped: [:delivered],
    delivered: [],
    cancelled: []
  }

  def valid_states, do: @valid_states
  def allowed_transitions(state) when state in @valid_states, do: Map.fetch!(@transitions, state)
  def valid_transition?(from, to) when from in @valid_states and to in @valid_states, do: to in Map.fetch!(@transitions, from)
  def valid_transition?(_, _), do: false
  def parse(state_str) do
    try do
      state = String.to_existing_atom(state_str)
      if state in @valid_states, do: {:ok, state}, else: {:error, :unknown_state}
    rescue
      _  -> {:error, :unknown_state}
    end
  end
  def terminal?(:delivered), do: true
  def terminal?(:cancelled), do: true
  def terminal?(_), do: false
end

defmodule OrderFsm.Order do
  defstruct [:id, :items, :state, :history]
  def new(id, items), do: %__MODULE__{id: id, items: items, state: :pending, history: []}
  def transition(%{state: current} = order, target) do
    if OrderFsm.State.valid_transition?(current, target) do
      updated = order |> Map.put(:state, target) |> Map.update!(:history, &[{current, target, DateTime.utc_now()} | &1])
      {:ok, updated}
    else
      {:error, :invalid_transition}
    end
  end
  def transition_chain(order, states) do
    Enum.reduce_while(states, {:ok, order}, fn target, {:ok, current} ->
      case transition(current, target) do
        {:ok, updated} -> {:cont, {:ok, updated}}
        {:error, reason} -> {:halt, {:error, reason, current}}
      end
    end)
  end
  def history(%{history: history}), do: Enum.reverse(history)
end

def main do
  IO.puts("=== OrderFsm Test ===\n")

  # Test 1: Create order
  IO.puts("Test 1: create order")
  order = OrderFsm.Order.new("ORD-001", ["Widget"])
  IO.puts("  State: #{order.state}")
  assert order.state == :pending

  # Test 2: Valid transition
  IO.puts("\nTest 2: valid transition")
  {:ok, confirmed} = OrderFsm.Order.transition(order, :confirmed)
  IO.puts("  New state: #{confirmed.state}")
  assert confirmed.state == :confirmed

  # Test 3: Invalid transition
  IO.puts("\nTest 3: invalid transition")
  {:error, :invalid_transition} = OrderFsm.Order.transition(order, :delivered)
  IO.puts("  Correctly rejected")

  # Test 4: Transition chain
  IO.puts("\nTest 4: transition chain")
  {:ok, delivered} = OrderFsm.Order.transition_chain(order, [:confirmed, :shipped, :delivered])
  IO.puts("  Final state: #{delivered.state}")
  assert delivered.state == :delivered

  # Test 5: Terminal state check
  IO.puts("\nTest 5: terminal state")
  is_terminal = OrderFsm.State.terminal?(delivered.state)
  IO.puts("  Is delivered terminal: #{is_terminal}")
  assert is_terminal == true

  IO.puts("\n=== All tests passed! ===")
end

defp assert(condition) do
  unless condition, do: raise "Assertion failed!"
end

main()
```
## Reflection

If your e-commerce platform had 50 different order states (B2B with custom workflows per customer), would you still use atoms, or model states as a struct field with a string? Why?

How would you add a new state `:returned` without breaking existing orders already in the `:delivered` state?

## Resources

- [Atom — HexDocs](https://hexdocs.pm/elixir/Atom.html)
- [Erlang atom table limits](https://www.erlang.org/doc/efficiency_guide/advanced.html)
- [String.to_existing_atom/1 — HexDocs](https://hexdocs.pm/elixir/String.html#to_existing_atom/1)
- [Pattern matching — Elixir Getting Started](https://elixir-lang.org/getting-started/pattern-matching.html)

---

## Why Atoms and Symbols matters

Mastering **Atoms and Symbols** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. Atom Table as a Global Resource

The BEAM maintains a single atom table per VM. Atoms are constants, and every atom ever created persists until the VM shuts down—unlike strings, which are garbage-collected. For system code, this is fine; you control atom creation at compile time. For untrusted input (webhooks, API payloads), creating atoms is a denial-of-service vector.

A malicious actor sending unique payloads can exhaust the atom table and crash the VM. The fix: never use `String.to_atom/1` on external input. Always use `String.to_existing_atom/1` (raises if the atom doesn't exist), or skip atoms for untrusted data.

### 2. Atoms Enable Pattern-Matchable Errors

The `{:ok, value}` and `{:error, reason}` pattern is the foundation of Elixir's error handling. It's idiomatic because pattern matching on atoms is compile-time-checked and O(1). Compare to nullable return types where `null` is silent. Atoms force errors to be explicit and pattern-matchable in your code.

### 3. Module Names Are Atoms

`is_atom(Enum)` returns `true`. Every module name is an atom prefixed internally with `Elixir.`. This means module references consume atom table entries too. In a distributed Erlang cluster, atoms must match exactly.

---
