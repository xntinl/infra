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
└── mix.exs
```

---

## Implementation

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
        ArgumentError -> nil
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

### Tests

```elixir
# test/order_fsm/state_test.exs
defmodule OrderFsm.StateTest do
  use ExUnit.Case, async: true

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
# test/order_fsm/order_test.exs
defmodule OrderFsm.OrderTest do
  use ExUnit.Case, async: true

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

---

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

## Resources

- [Atom — HexDocs](https://hexdocs.pm/elixir/Atom.html)
- [Erlang atom table limits](https://www.erlang.org/doc/efficiency_guide/advanced.html)
- [String.to_existing_atom/1 — HexDocs](https://hexdocs.pm/elixir/String.html#to_existing_atom/1)
- [Pattern matching — Elixir Getting Started](https://elixir-lang.org/getting-started/pattern-matching.html)
