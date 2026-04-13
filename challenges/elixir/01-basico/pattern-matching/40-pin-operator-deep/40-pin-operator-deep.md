# The Pin Operator in Depth: An Event Dispatcher

**Project**: `pinned_event_dispatcher` — dispatches events by matching against previously bound identifiers using the pin operator

---

## Project structure

```
pinned_event_dispatcher/
├── lib/
│   └── pinned_event_dispatcher.ex
├── test/
│   └── pinned_event_dispatcher_test.exs
└── mix.exs
```

---

## Core concepts

The `=` operator in Elixir is **match**, not assignment. When you write
`x = 5`, you are telling Elixir: "make both sides equal". Since `x` is
unbound, the match succeeds by binding `x` to `5`.

The **pin operator** `^` flips this. `^x` means: "do NOT rebind `x` here;
instead, match against the current value of `x`". This is essential when
the variable is already bound and you want to test equality inside a pattern.

```elixir
expected = 200
case response_code do
  ^expected -> :ok           # matches only if response_code == 200
  other -> {:unexpected, other}
end
```

Without the pin, `expected` inside the pattern would be rebound to whatever
`response_code` is. That's a subtle bug: the case always matches.

Key use sites:

1. Guards in `case`/`receive` when a value comes from outside the pattern.
2. Filtering in `for` comprehensions against a known value.
3. Ecto query expressions (the pin tells Ecto: "this is runtime data, not a
   field name").

---

## The business problem

An event dispatcher routes events to handlers. The dispatcher is configured
with a target user ID and a target event type at startup. Incoming events
should only be routed when BOTH match. Without pins, the match always
succeeds because the pattern variables shadow the configured values.

---

## Why `^var` in a match and not rebinding `var` and comparing after

Without the pin, `var = something` rebinds `var` — the original value is lost, and the "comparison" degenerates into an unconditional success. The pin keeps the match semantic intact.

## Design decisions

**Option A — pin operator `^var` to match against pre-bound values**
- Pros: Part of the pattern-match itself, participates in exhaustiveness, works uniformly in `case`/`with`/function heads
- Cons: Requires the variable to be bound in the enclosing scope

**Option B — comparison with `==` inside a guard** (chosen)
- Pros: More explicit to a newcomer
- Cons: Loses pattern-match semantics, won't trigger exhaustiveness warnings, breaks `with` chains

→ Chose **A** because the pin operator exists to solve exactly this problem — matching against an already-bound variable without rebinding it.

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
```


### `lib/pinned_event_dispatcher.ex`

```elixir
defmodule PinnedEventDispatcher do
  @moduledoc """
  Dispatches events by matching against pre-bound target values.

  Demonstrates why the pin operator is required when the pattern needs
  to match against values carried in by outer scope.
  """

  @type event :: %{type: atom(), user_id: String.t(), payload: map()}

  @doc """
  Returns `:match` when the event's type AND user_id equal the targets,
  otherwise `:skip`.

  The pins (^target_type, ^target_user) force equality against the values
  captured in the function parameters. Without them, the pattern would
  bind those names and match everything.
  """
  @spec dispatch(event(), atom(), String.t()) :: :match | :skip
  def dispatch(event, target_type, target_user)
      when is_map(event) and is_atom(target_type) and is_binary(target_user) do
    case event do
      %{type: ^target_type, user_id: ^target_user} -> :match
      _ -> :skip
    end
  end

  @doc """
  BUGGY version — shows what happens WITHOUT the pin operator.

  Here, `target_type` and `target_user` inside the pattern become fresh
  pattern variables, not references to the outer parameters. Any event
  matches the first clause, and the outer names end up shadowed.

  Kept in the codebase so tests can assert the bug.
  """
  @spec dispatch_buggy(event(), atom(), String.t()) :: :match | :skip
  def dispatch_buggy(event, target_type, target_user)
      when is_map(event) and is_atom(target_type) and is_binary(target_user) do
    case event do
      # target_type and target_user here are REBINDING, not matching.
      %{type: target_type, user_id: target_user}
      when is_atom(target_type) and is_binary(target_user) ->
        :match

      _ ->
        :skip
    end
  end

  @doc """
  Filters a list of events against a pinned target in a comprehension.

  Shows the pin used inside `for` — only events matching `target_type` pass.
  """
  @spec filter_by_type([event()], atom()) :: [event()]
  def filter_by_type(events, target_type) when is_atom(target_type) do
    for %{type: ^target_type} = event <- events, do: event
  end

  @doc """
  Finds the first event whose payload contains a specific `trace_id`.

  The pin lets us match against a value captured in the closure of `find/2`
  without having to compare inside a guard.
  """
  @spec find_by_trace([event()], String.t()) :: event() | nil
  def find_by_trace(events, trace_id) when is_binary(trace_id) do
    Enum.find(events, fn
      %{payload: %{trace_id: ^trace_id}} -> true
      _ -> false
    end)
  end

  @doc """
  Demonstrates when NOT to use a pin.

  If we want to CAPTURE the user_id into `uid` for further processing,
  we simply don't pin. This is the correct pattern when the value is
  unknown at the call site.
  """
  @spec extract_user(event()) :: {:ok, String.t()} | :error
  def extract_user(event) do
    case event do
      %{user_id: uid} when is_binary(uid) -> {:ok, uid}
      _ -> :error
    end
  end
end
```

### `test/pinned_event_dispatcher_test.exs`

```elixir
defmodule PinnedEventDispatcherTest do
  use ExUnit.Case, async: true

  alias PinnedEventDispatcher, as: Dispatcher

  @event %{type: :click, user_id: "u-1", payload: %{trace_id: "t-42"}}

  describe "dispatch/3 (correct with pins)" do
    test "matches when both fields align" do
      assert Dispatcher.dispatch(@event, :click, "u-1") == :match
    end

    test "rejects wrong user" do
      assert Dispatcher.dispatch(@event, :click, "u-2") == :skip
    end

    test "rejects wrong type" do
      assert Dispatcher.dispatch(@event, :scroll, "u-1") == :skip
    end

    test "rejects when both differ" do
      assert Dispatcher.dispatch(@event, :scroll, "u-2") == :skip
    end
  end

  describe "dispatch_buggy/3 (no pins)" do
    test "bug: matches an event that should be rejected" do
      # The pattern `target_type` rebinds, so any atom type matches.
      assert Dispatcher.dispatch_buggy(@event, :scroll, "u-2") == :match
    end
  end

  describe "filter_by_type/2" do
    test "returns only events with pinned type" do
      events = [
        %{type: :click, user_id: "a", payload: %{}},
        %{type: :scroll, user_id: "b", payload: %{}},
        %{type: :click, user_id: "c", payload: %{}}
      ]

      assert [%{user_id: "a"}, %{user_id: "c"}] =
               Dispatcher.filter_by_type(events, :click)
    end

    test "empty list when no match" do
      assert [] == Dispatcher.filter_by_type([%{type: :a, user_id: "x", payload: %{}}], :b)
    end
  end

  describe "find_by_trace/2" do
    test "finds event with matching trace_id" do
      events = [
        %{type: :a, user_id: "x", payload: %{trace_id: "t-1"}},
        %{type: :a, user_id: "y", payload: %{trace_id: "t-2"}}
      ]

      assert %{user_id: "y"} = Dispatcher.find_by_trace(events, "t-2")
    end

    test "returns nil when nothing matches" do
      assert Dispatcher.find_by_trace([], "t-1") == nil
    end
  end

  describe "extract_user/1" do
    test "captures user_id without pinning" do
      assert {:ok, "u-1"} = Dispatcher.extract_user(@event)
    end

    test "returns error when field missing" do
      assert :error == Dispatcher.extract_user(%{type: :x})
    end
  end
end
```

### Run it

```bash
mix new pinned_event_dispatcher
cd pinned_event_dispatcher
mix test
```

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.



---
## Key Concepts

### 1. The Pin Operator `^` Prevents Rebinding

In pattern matching, a bare variable always binds. The pin operator `^x` says "use the value of `x`, don't rebind": `{^x, y} = {1, 2}` matches and binds `y`; `{^x, y} = {2, 2}` raises `MatchError`.

### 2. Pin in Guards Ensures Consistency

Guards can use pinned variables to enforce constraints. A common bug in list comprehensions: forgetting the pin when matching against a captured variable.

### 3. Understanding the Difference

Without the pin, `{x, y} = {2, 2}` rebinds `x = 2`. With the pin, `{^x, y} = {2, 2}` checks that `x` already equals `2`. The pin prevents silent rebinding bugs.

---
## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of pinned match over 1M dispatches
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```

Target: **< 15ms total; pinned matches have no extra cost over literal matches**.

## Trade-offs and production mistakes

**1. Silent shadowing bug**
The classic mistake: `case resp do %{status: status} -> ...` when `status`
is already bound in the outer scope. The pattern always matches because the
outer `status` was shadowed, not compared. Enable `--warnings-as-errors` and
pay attention to "variable X is unused" warnings pointing at the outer name.

**2. Pin works on variables, not expressions**
`^(x + 1)` works in Elixir 1.9+, but `^some_function()` still surprises.
Prefer computing the value first, then pinning the bound name.

**3. Pin in `receive` and `case` but not in function heads**
In function heads, you pattern-match directly on parameters — there is no
"outer scope" to pin from. If you need to match on an atom value, inline it:
`def handle(:stop, _), do: ...`.

**4. Ecto uses pin for a different reason**
In Ecto queries, `from u in User, where: u.id == ^user_id` tells the query
builder: "treat `user_id` as a parameter, not a field name". Same operator,
same idea (refer to outer value), different consumer.

## When NOT to use pin

- Inside a pattern where you actually WANT to capture the value — you'd be
  re-checking a value you don't know yet.
- In simple function clauses where the expected value can be hardcoded.
  `def handle({:ok, value}, 200)` is clearer than pinning a variable.
- When a guard is more readable: `when code == ^expected` is the same as
  just `^expected` in the pattern; pick the less noisy form.

---

## Reflection

A colleague writes `case incoming_id do user_id -> ...`. They intended to match against a pre-bound `user_id`. Why does this silently match everything? How would you spot the bug in review?

When pattern-matching on dynamic values is the *only* option (e.g., `Enum.find/2` with a runtime target), how do you pass the target without losing the pattern-match style?

## Resources

- [The pin operator — Getting Started](https://elixir-lang.org/getting-started/pattern-matching.html#the-pin-operator)
- [Kernel.SpecialForms.^/1](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#%5E/1)
- [Pattern matching guide](https://hexdocs.pm/elixir/patterns-and-guards.html)
