# Protocol vs behaviour — solving the same problem both ways

**Project**: `proto_vs_behaviour` — the exact same "describe a shape's area" problem, solved once with a protocol (dispatch on value type) and once with a behaviour (explicit module dispatch), so the trade-offs become concrete.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

"Should I use a protocol or a behaviour?" is the most-asked question in
intermediate Elixir. The docs explain each separately, but comparison is
what actually builds intuition. This exercise solves the same problem —
compute the area of a geometric shape — with both mechanisms, side by side.

You'll see:

- The protocol version dispatches on the **value**'s type (the struct).
  The caller writes `Shape.area(rectangle)` and dispatch is implicit.
- The behaviour version dispatches on a **module** passed explicitly.
  The caller writes `ShapeCalc.area(ShapeCalc.Rectangle, %{w: 3, h: 4})`.

When you're done you'll have a fast mental heuristic: "am I dispatching
on a value I already have, or am I picking a strategy/adapter at call
time?"

Project structure:

```
proto_vs_behaviour/
├── lib/
│   ├── shape.ex                 # protocol
│   ├── shape/impls.ex           # protocol impls
│   ├── shape_calc.ex            # behaviour
│   ├── shape_calc/rectangle.ex  # behaviour impl
│   └── shape_calc/circle.ex     # behaviour impl
├── test/
│   └── proto_vs_behaviour_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Protocol: dispatch on the first argument's type

```elixir
Shape.area(%Rectangle{...})  # dispatches to defimpl Shape, for: Rectangle
```

The protocol runtime uses the value's `__struct__` (or built-in type) to
pick the impl. Zero ceremony at the call site.

### 2. Behaviour: dispatch by explicit module

```elixir
ShapeCalc.area(ShapeCalc.Rectangle, %{w: 3, h: 4})
```

The caller NAMES the module. No introspection of the value. The value can
be a plain map — it doesn't need to be a typed struct.

### 3. Protocol extension requires a struct per type

Adding a new shape means defining a new struct AND a `defimpl` for it.
Value and impl are coupled. That's often a good fit (a shape *is* the
data). It's awkward when one "type" has many computational interpretations
(e.g., pricing) — then you want a behaviour/strategy.

### 4. Behaviour extension is orthogonal to data

Adding a new shape means adding a new module; the value stays a plain
map. You can even have two behaviour impls that interpret the same map
differently ("area" vs "perimeter" at different call sites). That's
strictly more flexible but costs a bit at the call site.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new proto_vs_behaviour
cd proto_vs_behaviour
```

### Step 2: `lib/shape.ex` (protocol)

**Objective**: Provide `lib/shape.ex` (protocol) — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defprotocol Shape do
  @moduledoc """
  Protocol version: dispatch on the value's struct. Callers don't pick an
  implementation — they just pass the value.
  """

  @spec area(t) :: float()
  def area(shape)
end
```

### Step 3: `lib/shape/impls.ex` (protocol impls + structs)

**Objective**: Provide `lib/shape/impls.ex` (protocol impls + structs) — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule Shape.Rectangle do
  @enforce_keys [:width, :height]
  defstruct [:width, :height]
  @type t :: %__MODULE__{width: number(), height: number()}
end

defmodule Shape.Circle do
  @enforce_keys [:radius]
  defstruct [:radius]
  @type t :: %__MODULE__{radius: number()}
end

defimpl Shape, for: Shape.Rectangle do
  def area(%{width: w, height: h}), do: w * h * 1.0
end

defimpl Shape, for: Shape.Circle do
  def area(%{radius: r}), do: :math.pi() * r * r
end
```

### Step 4: `lib/shape_calc.ex` (behaviour)

**Objective**: Provide `lib/shape_calc.ex` (behaviour) — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule ShapeCalc do
  @moduledoc """
  Behaviour version: dispatch by explicit module. The data is a plain map;
  the caller chooses the interpreter.
  """

  @callback area(map()) :: float()

  @doc "Dispatch to the caller-chosen module."
  @spec area(module(), map()) :: float()
  def area(impl, data) when is_atom(impl) and is_map(data) do
    impl.area(data)
  end
end
```

### Step 5: `lib/shape_calc/rectangle.ex` and `circle.ex`

**Objective**: Provide `lib/shape_calc/rectangle.ex` and `circle.ex` — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
defmodule ShapeCalc.Rectangle do
  @behaviour ShapeCalc

  @impl ShapeCalc
  def area(%{w: w, h: h}), do: w * h * 1.0
end

defmodule ShapeCalc.Circle do
  @behaviour ShapeCalc

  @impl ShapeCalc
  def area(%{r: r}), do: :math.pi() * r * r
end
```

### Step 6: `test/proto_vs_behaviour_test.exs`

**Objective**: Write `proto_vs_behaviour_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule ProtoVsBehaviourTest do
  use ExUnit.Case, async: true

  describe "protocol dispatch (by value type)" do
    test "Shape.area on a Rectangle" do
      assert Shape.area(%Shape.Rectangle{width: 3, height: 4}) == 12.0
    end

    test "Shape.area on a Circle" do
      assert_in_delta Shape.area(%Shape.Circle{radius: 2}), 12.566, 0.001
    end

    test "an untyped value raises Protocol.UndefinedError" do
      assert_raise Protocol.UndefinedError, fn ->
        Shape.area(%{width: 3, height: 4})
      end
    end
  end

  describe "behaviour dispatch (by explicit module)" do
    test "ShapeCalc.area with Rectangle module on a plain map" do
      assert ShapeCalc.area(ShapeCalc.Rectangle, %{w: 3, h: 4}) == 12.0
    end

    test "ShapeCalc.area with Circle module on a plain map" do
      assert_in_delta ShapeCalc.area(ShapeCalc.Circle, %{r: 2}), 12.566, 0.001
    end

    test "wrong module for the data crashes loudly" do
      # Dispatch is by module; the wrong impl will not pattern-match the map.
      assert_raise FunctionClauseError, fn ->
        ShapeCalc.area(ShapeCalc.Rectangle, %{r: 2})
      end
    end
  end

  describe "same computation, different dispatch" do
    test "both compute the same area for a rectangle" do
      proto_result = Shape.area(%Shape.Rectangle{width: 3, height: 4})
      beh_result = ShapeCalc.area(ShapeCalc.Rectangle, %{w: 3, h: 4})
      assert proto_result == beh_result
    end
  end
end
```

### Step 7: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Prefer a protocol when the type IS the dispatch key**
If every caller has a typed value (a struct) and the operation is inherent
to the type ("this shape has an area"), a protocol reads better. No module
name at call sites; dispatch is implicit and fast after consolidation.

**2. Prefer a behaviour when dispatch is a runtime/config choice**
Strategy, Adapter, and interpreter patterns
are all behaviour-shaped. The value doesn't tell you which algorithm to
run — the caller does. Forcing this into a protocol means inventing a
wrapper struct per strategy, which is backwards.

**3. Protocols bind impls to types forever**
Adding a second "area for rectangle" interpretation (e.g., an approximated
area for a rendering layer) can't live as a second protocol impl — you'd
have to invent a new type. Behaviours don't have this problem; you just
add another module.

**4. Consolidation vs dynamic adapters**
Consolidated protocols are faster than behaviour dispatch by a small
constant. Unless you're in a hot loop, the difference doesn't matter.
Optimize for clarity first, measure before rewriting.

**5. When NOT to use either**
If there's exactly one implementation and no near-term need for another,
skip both and write a module with plain functions. Protocols and
behaviours are machinery for variation; with no variation, the machinery
adds cost without benefit.

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule Shape.Rectangle do
    @enforce_keys [:width, :height]
    defstruct [:width, :height]
    @type t :: %__MODULE__{width: number(), height: number()}
  end

  defmodule Shape.Circle do
    @enforce_keys [:radius]
    defstruct [:radius]
    @type t :: %__MODULE__{radius: number()}
  end

  defimpl Shape, for: Shape.Rectangle do
    def area(%{width: w, height: h}), do: w * h * 1.0
  end

  defimpl Shape, for: Shape.Circle do
    def area(%{radius: r}), do: :math.pi() * r * r
  end

  def main do
    IO.puts("Shape OK")
  end

end

Main.main()
```


## Resources

- [Protocols — Elixir guide](https://hexdocs.pm/elixir/protocols.html)
- [Behaviours — Elixir guide](https://hexdocs.pm/elixir/typespecs.html)
- ["Writing extensible Elixir with Protocols" — Dashbit](https://dashbit.co/blog/writing-extensible-elixir-with-protocols)
- ["Mocks and explicit contracts" — José Valim](http://blog.plataformatec.com.br/2015/10/mocks-and-explicit-contracts/)


## Key Concepts

Protocols and behaviors are Elixir's mechanism for ad-hoc and static polymorphism. They solve different problems and are often confused.

**Protocols:**
Dispatch based on the type/struct of the first argument at runtime. A protocol defines a contract (e.g., `Enumerable`); any type can implement it by adding a corresponding implementation block. Protocols excel when you control neither the type nor the caller — e.g., a library that needs to iterate any collection. The fallback is `:any` — if no specific implementation exists, the `:any` handler is tried. This enables "optional" protocol implementations.

**Behaviours:**
Static polymorphism enforced at compile time. A module implements a behavior by defining callbacks (functions). Behaviors are about contracts between modules, not types. Use when you need multiple implementations of the same interface and the caller chooses which to use (e.g., different database adapters, different strategies). Callbacks are checked at compile time — missing a required callback is a compiler error.

**Architectural patterns:**
Behaviors excel in plugin systems (user defines modules conforming to the behavior). Protocols excel in type-driven dispatch (any type can conform). Mix both: a behavior can require that its callbacks operate on types that implement a protocol. Example: `MyAdapter` behavior requiring callbacks that work with `Enumerable` types.
