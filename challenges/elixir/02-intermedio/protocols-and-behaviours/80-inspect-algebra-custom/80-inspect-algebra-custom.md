# Custom `Inspect` with `Inspect.Algebra` — pretty-printing a nested struct

**Project**: `pretty_inspect` — a nested `Order`/`LineItem` struct pair with a hand-rolled `Inspect` impl using `Inspect.Algebra` primitives (`concat`, `nest`, `break`, `group`) for terminal-friendly layout that collapses or wraps based on width.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Default struct inspect looks fine for flat shapes:

```
#Order<id: 1, total: 99, ...>
```

It becomes unreadable the moment you have nested collections:

```
%Order{id: 1, items: [%LineItem{sku: "A", qty: 1, price: 100}, %LineItem{sku: "B", qty: 2, price: 50}, ...]}
```

`Inspect.Algebra` is the pretty-printer Elixir uses internally for IEx
output. It lets you describe a layout *abstractly* — "these chunks should
fit on one line if they can, wrap and indent otherwise" — and the renderer
figures out the actual line breaks given the current terminal width.

Understanding it pays off any time you build a DSL, a test matcher, a
domain model, or a CLI that produces structured output.

Project structure:

```
pretty_inspect/
├── lib/
│   ├── order.ex
│   └── line_item.ex
├── test/
│   └── pretty_inspect_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Inspect.Algebra` documents are declarative

```elixir
concat(["[", nest(line() |> ..., 2), line(), "]"])
```

You describe structure; the renderer decides how to lay it out given a
`:width`. You rarely produce strings directly — you combine primitives
(`concat`, `line`, `break`, `nest`, `group`) and let the algebra do the
layout math.

### 2. `group/1` is "fit on one line if possible"

Inside a `group`, all `break/1`s either all collapse to their flat form or
all expand to line breaks. That's what makes the classic "compact vs
wrapped" rendering work.

### 3. `nest/2` indents a document

Everything under `nest(doc, 2)` gets 2 extra spaces when wrapped. Nested
`nest`s stack additively. Indent levels should be small (2 or 4) — deep
indents produce unreadable output at typical terminal widths.

### 4. `Inspect.Opts` carries the width and depth

Your impl receives `opts` as the second argument. `opts.width` is the
target line length; `Inspect.Algebra.to_doc/2` recurses into children,
propagating the same opts so nested structs also respect the width.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
    {inspect},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new pretty_inspect
cd pretty_inspect
```

### Step 2: `lib/line_item.ex`

**Objective**: Implement `line_item.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defmodule LineItem do
  @moduledoc """
  Order line item. Demonstrates a small hand-rolled inspect with
  `Inspect.Algebra`. Items appear inline when they fit on a line and wrap
  otherwise.
  """

  @enforce_keys [:sku, :qty, :price]
  defstruct [:sku, :qty, :price]

  @type t :: %__MODULE__{sku: String.t(), qty: pos_integer(), price: integer()}
end

defimpl Inspect, for: LineItem do
  import Inspect.Algebra

  def inspect(%LineItem{sku: sku, qty: qty, price: price}, opts) do
    # Build the field list as a doc. `to_doc/2` handles nested inspection
    # using the same opts (so truncation and pretty-printing nest correctly).
    fields =
      concat([
        "sku: ",
        to_doc(sku, opts),
        ", qty: ",
        to_doc(qty, opts),
        ", price: ",
        to_doc(price, opts)
      ])

    # group + nest = "fit on one line, otherwise break and indent".
    concat(["#LineItem<", group(nest(fields, 2)), ">"])
  end
end
```

### Step 3: `lib/order.ex`

**Objective**: Implement `order.ex` — polymorphism via dispatch on the data's type (protocol) or via an explicit contract (behaviour).


```elixir
defmodule Order do
  @moduledoc """
  Order with id, customer, and a list of line items. The custom inspect
  produces terminal-friendly output that collapses to one line for small
  orders and wraps with indentation for larger ones.
  """

  @enforce_keys [:id, :customer, :items]
  defstruct [:id, :customer, :items]

  @type t :: %__MODULE__{id: term(), customer: String.t(), items: [LineItem.t()]}
end

defimpl Inspect, for: Order do
  import Inspect.Algebra

  def inspect(%Order{id: id, customer: customer, items: items}, opts) do
    # Each field becomes a doc chunk separated by commas and soft breaks.
    # `break("")` is an empty break: collapses to nothing when the group
    # fits, becomes a newline when the group wraps.
    body =
      [
        concat(["id: ", to_doc(id, opts)]),
        concat(["customer: ", to_doc(customer, opts)]),
        concat(["items: ", items_doc(items, opts)])
      ]
      |> Enum.reduce(fn chunk, acc ->
        # "," + soft break so wrapping adds a newline, flat keeps a space.
        concat([acc, ",", break(" "), chunk])
      end)

    concat(["#Order<", nest(concat([break(""), body]), 2), break(""), ">"])
    |> group()
  end

  # Render the items list with its own group so it can wrap independently
  # of the outer Order structure.
  defp items_doc([], _opts), do: "[]"

  defp items_doc(items, opts) do
    rendered =
      items
      |> Enum.map(&to_doc(&1, opts))
      |> Enum.reduce(fn item, acc -> concat([acc, ",", break(" "), item]) end)

    concat(["[", nest(concat([break(""), rendered]), 2), break(""), "]"])
    |> group()
  end
end
```

### Step 4: `test/pretty_inspect_test.exs`

**Objective**: Write `pretty_inspect_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule PrettyInspectTest do
  use ExUnit.Case, async: true

  defp render(term, width) do
    # `Inspect.Algebra.format/2` is the renderer. It returns an iodata
    # sequence; we flatten to a string for assertions.
    term
    |> Inspect.Algebra.to_doc(%Inspect.Opts{width: width})
    |> Inspect.Algebra.format(width)
    |> IO.iodata_to_binary()
  end

  describe "flat (one-line) rendering" do
    test "a small LineItem fits on a wide line" do
      item = %LineItem{sku: "A", qty: 1, price: 100}
      assert render(item, 200) == ~s(#LineItem<sku: "A", qty: 1, price: 100>)
    end

    test "a small Order fits on a wide line" do
      order = %Order{
        id: 1,
        customer: "Jane",
        items: [%LineItem{sku: "A", qty: 1, price: 100}]
      }

      rendered = render(order, 200)
      assert rendered =~ ~s(#Order<)
      assert rendered =~ ~s(id: 1)
      assert rendered =~ ~s(customer: "Jane")
      # Single-line form has no newlines.
      refute rendered =~ "\n"
    end
  end

  describe "wrapped rendering (narrow width)" do
    test "an Order with many items wraps to multiple lines" do
      items =
        for i <- 1..5 do
          %LineItem{sku: "SKU#{i}", qty: i, price: i * 100}
        end

      order = %Order{id: 42, customer: "Alice Example", items: items}
      rendered = render(order, 40)

      # Must contain newlines when it can't fit on one 40-wide line.
      assert rendered =~ "\n"
      # Structural markers still present.
      assert rendered =~ "#Order<"
      assert rendered =~ ">"
    end

    test "indentation reflects nesting depth" do
      items = [
        %LineItem{sku: "A", qty: 1, price: 100},
        %LineItem{sku: "B", qty: 2, price: 200}
      ]

      order = %Order{id: 1, customer: "Jane", items: items}
      rendered = render(order, 20)

      # With a 20-wide window the rendering wraps and indents. We only assert
      # that SOME line starts with at least two spaces — the indentation signal.
      lines = String.split(rendered, "\n")
      assert Enum.any?(lines, &String.starts_with?(&1, "  "))
    end
  end

  describe "IO.inspect integration" do
    test "inspect/1 uses our custom impl" do
      # inspect/1 uses the default options, which include a width (~80).
      item = %LineItem{sku: "X", qty: 1, price: 1}
      assert inspect(item) =~ "#LineItem<"
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Always call `to_doc/2`, never stringify children yourself**
If you interpolate child values with `"#{value}"`, you bypass `Inspect` for
those children — numbers become strings, strings lose their quotes, and
the output loses its round-trip property. `to_doc/2` is the only correct
way to embed nested values.

**2. `Inspect.Opts.limit` caps output — respect it**
For a huge collection, the algebra will truncate at `opts.limit`
automatically IF you use `to_doc/2`. If you hand-build the doc with string
concatenation, you'll dump megabytes into IEx and freeze the terminal.

**3. Hand-rolled Inspect can hide bugs**
If your custom inspect reformats or omits fields, a failing test diff won't
show the real state. Debugging then means temporarily swapping to
`IO.inspect(value, structs: false)` to see the raw struct. Keep custom
inspect *loss-free*: never drop fields silently, never compute derived
values, never lie about the shape.

**4. `group/1` is atomic — don't nest non-breaking groups**
Nested groups collapse or expand together as a unit. If the outer group
fits, the inner group never wraps — even if its content is huge.
Ordering matters; put the most-likely-to-wrap content in the outermost
group.

**5. When NOT to write a custom Inspect**
If `@derive {Inspect, only: [...]}` is enough, use that. Custom algebras
are for types where the default structured dump is a readability problem.
For internal dev tools and diagnostic logs, default inspect is nearly
always fine.

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule Order do
    @moduledoc """
    Order with id, customer, and a list of line items. The custom inspect
    produces terminal-friendly output that collapses to one line for small
    orders and wraps with indentation for larger ones.
    """

    @enforce_keys [:id, :customer, :items]
    defstruct [:id, :customer, :items]

    @type t :: %__MODULE__{id: term(), customer: String.t(), items: [LineItem.t()]}
  end

  defimpl Inspect, for: Order do
    import Inspect.Algebra

    def inspect(%Order{id: id, customer: customer, items: items}, opts) do
      # Each field becomes a doc chunk separated by commas and soft breaks.
      # `break("")` is an empty break: collapses to nothing when the group
      # fits, becomes a newline when the group wraps.
      body =
        [
          concat(["id: ", to_doc(id, opts)]),
          concat(["customer: ", to_doc(customer, opts)]),
          concat(["items: ", items_doc(items, opts)])
        ]
        |> Enum.reduce(fn chunk, acc ->
          # "," + soft break so wrapping adds a newline, flat keeps a space.
          concat([acc, ",", break(" "), chunk])
        end)

      concat(["#Order<", nest(concat([break(""), body]), 2), break(""), ">"])
      |> group()
    end

    # Render the items list with its own group so it can wrap independently
    # of the outer Order structure.
    defp items_doc([], _opts), do: "[]"

    defp items_doc(items, opts) do
      rendered =
        items
        |> Enum.map(&to_doc(&1, opts))
        |> Enum.reduce(fn item, acc -> concat([acc, ",", break(" "), item]) end)

      concat(["[", nest(concat([break(""), rendered]), 2), break(""), "]"])
      |> group()
    end
  end

  def main do
    IO.puts("Order OK")
  end

end

Main.main()
```


## Resources

- [`Inspect.Algebra` — Elixir stdlib](https://hexdocs.pm/elixir/Inspect.Algebra.html)
- [`Inspect` protocol](https://hexdocs.pm/elixir/Inspect.html)
- ["A prettier printer" — Philip Wadler](https://homepages.inf.ed.ac.uk/wadler/papers/prettier/prettier.pdf) — the paper the algebra is based on
- [`Inspect` implementations in Elixir source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/inspect.ex) — great reading for idioms


## Key Concepts

Protocols and behaviors are Elixir's mechanism for ad-hoc and static polymorphism. They solve different problems and are often confused.

**Protocols:**
Dispatch based on the type/struct of the first argument at runtime. A protocol defines a contract (e.g., `Enumerable`); any type can implement it by adding a corresponding implementation block. Protocols excel when you control neither the type nor the caller — e.g., a library that needs to iterate any collection. The fallback is `:any` — if no specific implementation exists, the `:any` handler is tried. This enables "optional" protocol implementations.

**Behaviours:**
Static polymorphism enforced at compile time. A module implements a behavior by defining callbacks (functions). Behaviors are about contracts between modules, not types. Use when you need multiple implementations of the same interface and the caller chooses which to use (e.g., different database adapters, different strategies). Callbacks are checked at compile time — missing a required callback is a compiler error.

**Architectural patterns:**
Behaviors excel in plugin systems (user defines modules conforming to the behavior). Protocols excel in type-driven dispatch (any type can conform). Mix both: a behavior can require that its callbacks operate on types that implement a protocol. Example: `MyAdapter` behavior requiring callbacks that work with `Enumerable` types.
