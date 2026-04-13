# `@fallback_to_any` — explicit default impls for a `Describable` protocol

**Project**: `fallback_any_demo` — a `Describable` protocol with `@fallback_to_any true` and an `Any` impl, contrasted against the version without fallback to make the trade-offs concrete.

---

## Project context

Elixir protocols dispatch on the concrete type of the first argument. If
no impl matches, the call raises `Protocol.UndefinedError`. That's usually
what you want — it means "this type doesn't support this operation, fix
it". Sometimes, though, a sensible default exists for every type:
`inspect/1` works on anything, `to_string/1` has a reasonable fallback,
and some in-house protocols genuinely should too.

`@fallback_to_any true` plus `defimpl Proto, for: Any` is the mechanism.
This exercise builds a small `Describable` protocol — produces a human
description of a value — and demonstrates:

- How the fallback is declared and wired.
- How specific impls still take precedence over `Any`.
- Why fallback is easy to abuse, and when it's actually appropriate.

Project structure:

```
fallback_any_demo/
├── lib/
│   ├── describable.ex
│   └── describable/impls.ex
├── test/
│   └── describable_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `@fallback_to_any true` must be declared in the protocol

```elixir
defprotocol Describable do
  @fallback_to_any true
  def describe(value)
end
```

Without this attribute, `defimpl Describable, for: Any` is rejected at
compile time — Elixir wants you to be deliberate about opting in.

### 2. Dispatch precedence: specific impl > Any

If both `defimpl Describable, for: Integer` and `defimpl Describable, for: Any`
exist, integers use the specific impl. `Any` only fires when nothing else
matches. This is what makes the pattern useful: provide a default, override
per type as needed.

### 3. The fallback runs AFTER struct dispatch

A struct with no specific impl falls through to `Any` just like any other
value. If you want different defaults for structs vs primitives, branch
inside the `Any` impl with `is_struct/1` — don't try to declare two fallbacks.

### 4. Fallback hides missing impls — deliberate design only

The ergonomic win is "everything works". The cost is "you won't notice when
the default is wrong". Only enable fallback when the default is objectively
correct for all foreseeable types, or when the protocol is app-internal and
you accept the trade-off.

---

## Why enable `@fallback_to_any` here (and not for contract protocols)

**No fallback.** Every missing impl raises `Protocol.UndefinedError`. Loud, correct, and the right default for *contract* protocols (encoding, serialization, comparison).

**`@fallback_to_any true` + `Any` impl (chosen for this descriptive use case).** `Describable` is cosmetic — a debugging aid — where a weak default (`inspect/1`) is strictly better than a crash. The `Any` impl branches on `is_struct/1` so structs get a shape-aware description.

The rule: fallback for descriptive/cosmetic protocols, no fallback for contractual ones.

---

## Design decisions

**Option A — Skip `@fallback_to_any`; force every type to have a specific impl**
- Pros: Missing impls fail at the call site, preventing silent degradation.
- Cons: Every new type (including user structs) requires boilerplate; wrong for a descriptive protocol.

**Option B — `@fallback_to_any` with an `Any` impl that branches on `is_struct/1`** (chosen)
- Pros: Ergonomic default for every value; specific impls still override; readers can see the two defaults (struct vs everything-else) in one place.
- Cons: Silently hides missing impls; if a specific type needs richer output you must remember to add the impl, not rely on the fallback.

→ Chose **B** because `Describable` is deliberately cosmetic — a missing per-type description is a nit, not a bug, and the fallback is objectively safe via `inspect/1`.

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
mix new fallback_any_demo
cd fallback_any_demo
```

### Step 2: `lib/describable.ex`

**Objective**: Implement `describable.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defprotocol Describable do
  @moduledoc """
  Produces a short human description of a value. Provides a generic fallback
  via `@fallback_to_any true` so callers don't need to special-case unknown
  types, while still allowing rich per-type descriptions.
  """

  @fallback_to_any true

  @doc "Return a short human description string for `value`."
  @spec describe(t) :: String.t()
  def describe(value)
end
```

### Step 3: `lib/describable/impls.ex`

**Objective**: Implement `impls.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defmodule Describable.Impls do
  @moduledoc false

  defimpl Describable, for: Integer do
    # Specific impls beat the Any fallback.
    def describe(i), do: "an integer: #{i}"
  end

  defimpl Describable, for: BitString do
    def describe(s) when is_binary(s) do
      "a string of length #{byte_size(s)}"
    end
  end

  defimpl Describable, for: List do
    def describe(list) do
      "a list of #{length(list)} element(s)"
    end
  end

  defimpl Describable, for: Any do
    @moduledoc """
    Fallback. Branches on whether the value is a struct so structs get a
    shape-aware description, while bare values (tuples, PIDs, etc.) fall
    through to `inspect/1`.
    """

    def describe(value) when is_struct(value) do
      %mod{} = value
      # Show the struct name and the field count — enough to orient a reader.
      field_count = value |> Map.from_struct() |> map_size()
      "a #{inspect(mod)} struct with #{field_count} field(s)"
    end

    def describe(other) do
      # Catch-all: inspect gives a faithful, unambiguous representation.
      "a value: #{inspect(other)}"
    end
  end
end
```

### Step 4: `test/describable_test.exs`

**Objective**: Write `describable_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule DescribableTest do
  use ExUnit.Case, async: true

  defmodule Point do
    @enforce_keys [:x, :y]
    defstruct [:x, :y]
  end

  describe "specific impls take precedence over Any" do
    test "integer uses the Integer impl" do
      assert Describable.describe(42) == "an integer: 42"
    end

    test "string uses the BitString impl" do
      assert Describable.describe("hello") == "a string of length 5"
    end

    test "list uses the List impl" do
      assert Describable.describe([1, 2, 3]) == "a list of 3 element(s)"
    end
  end

  describe "Any fallback covers types with no specific impl" do
    test "struct falls through to Any and reports module + field count" do
      description = Describable.describe(%Point{x: 1, y: 2})
      assert description =~ "DescribableTest.Point"
      assert description =~ "2 field(s)"
    end

    test "tuple falls through to Any and uses inspect" do
      assert Describable.describe({:ok, 1}) == "a value: {:ok, 1}"
    end

    test "PID falls through to Any" do
      description = Describable.describe(self())
      assert description =~ ~r/a value: #PID<.+>/
    end
  end

  describe "adding a specific impl overrides the fallback" do
    # This is a property, not a runtime-configurable thing: the moment you
    # compile a `defimpl Describable, for: Float`, floats stop using Any.
    # We test the current dispatch choice for float (no specific impl → Any).
    test "float currently uses Any because no Float impl exists" do
      assert Describable.describe(3.14) == "a value: 3.14"
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Why this works

`@fallback_to_any true` is the only way Elixir accepts `defimpl Describable, for: Any` — the compiler rejects it otherwise, which is the deliberate opt-in. Dispatch precedence is "specific impl first, `Any` last", so adding `defimpl Describable, for: Float` automatically takes floats off the fallback path. Branching inside `Any` on `is_struct/1` keeps the two defaults (struct shape vs everything-else `inspect`) in a single readable place.

---

## Benchmark

<!-- benchmark N/A: descriptive protocol with trivial impls — the interesting behavior is dispatch precedence, not throughput. -->

---

## Trade-offs and production gotchas

**1. `@fallback_to_any` silences a class of bugs**
Without fallback, "I forgot to implement `Describable` for `Money`" surfaces
as a loud `Protocol.UndefinedError` at the call site — and you fix it.
With fallback, it surfaces as "the UI shows `a value: #Money<...>`" buried
three screens deep. Only enable fallback when a wrong-but-safe default is
better than a crash.

**2. `Any` impls are a dumping ground — keep them small**
It's tempting to cram branching logic into `Any` (`is_struct/1`, `is_map/1`,
`is_list/1`...) and end up reimplementing dispatch. If you're doing that,
split the branches into explicit `defimpl` clauses — the compiler will
optimize each one and the intent reads better.

**3. Consolidation interacts badly with partial recompiles**
Adding a new `defimpl` for a type that previously hit `Any` requires
reconsolidating the protocol. If you see "this type still uses the fallback"
after adding an impl, run `mix compile --force` or `mix clean`.

**4. Library protocols should almost NEVER enable fallback**
Consumers depend on the missing-impl error to know which types they must
handle. A library that silently swallows unknown types with a generic
default makes integration harder, not easier.

**5. When NOT to use `@fallback_to_any`**
Any time the protocol represents a *contract* (encoding, serialization,
comparison). Use it only for protocols that are genuinely descriptive or
cosmetic (logging, debugging summaries), where a weak default is always
better than a crash.

---

## Reflection

- Take a serialization protocol (like `Jason.Encoder`) and argue whether `@fallback_to_any` would help or hurt. What class of production bugs does each choice cause — and which is more expensive to diagnose?
- Your `Any` impl currently has two clauses (struct vs other). A teammate keeps adding branches (`is_tuple/1`, `is_map/1`, `is_list/1`). At what point should those branches become separate `defimpl` blocks instead, and what signal tells you you've crossed the line?

---

## Resources

- [`Protocol` module — `@fallback_to_any`](https://hexdocs.pm/elixir/Protocol.html#module-fallback-to-any)
- [`Inspect` protocol source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/inspect.ex) — a real protocol that uses `Any`
- ["Protocols vs Behaviours" — Dashbit](https://dashbit.co/blog/writing-extensible-elixir-with-protocols)


## Key Concepts

Protocols and behaviors are Elixir's mechanism for ad-hoc and static polymorphism. They solve different problems and are often confused.

**Protocols:**
Dispatch based on the type/struct of the first argument at runtime. A protocol defines a contract (e.g., `Enumerable`); any type can implement it by adding a corresponding implementation block. Protocols excel when you control neither the type nor the caller — e.g., a library that needs to iterate any collection. The fallback is `:any` — if no specific implementation exists, the `:any` handler is tried. This enables "optional" protocol implementations.

**Behaviours:**
Static polymorphism enforced at compile time. A module implements a behavior by defining callbacks (functions). Behaviors are about contracts between modules, not types. Use when you need multiple implementations of the same interface and the caller chooses which to use (e.g., different database adapters, different strategies). Callbacks are checked at compile time — missing a required callback is a compiler error.

**Architectural patterns:**
Behaviors excel in plugin systems (user defines modules conforming to the behavior). Protocols excel in type-driven dispatch (any type can conform). Mix both: a behavior can require that its callbacks operate on types that implement a protocol. Example: `MyAdapter` behavior requiring callbacks that work with `Enumerable` types.
