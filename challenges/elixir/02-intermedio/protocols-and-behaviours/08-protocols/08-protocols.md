# Protocols — the `Jsonable` dispatch contract

**Project**: `jsonable_proto` — a `Jsonable` protocol that turns primitives, lists and maps into JSON strings via polymorphic dispatch.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You have a bunch of different value types — strings, integers, lists, maps —
and you want a single function `to_json/1` that Does The Right Thing for each.
In an OO language you'd use inheritance or interfaces; in a dynamic language
you'd pattern-match on every call site. Elixir protocols give you a third
option: **polymorphic dispatch on the value's type**, defined in one place,
extensible to new types without modifying the protocol itself.

This is the same mechanism `Enum`, `Inspect`, `String.Chars`, and `Jason.Encoder`
all use under the hood. Understanding protocols is understanding how Elixir
libraries stay extensible.

Project structure:

```
jsonable_proto/
├── lib/
│   ├── jsonable.ex
│   └── jsonable/impls.ex
├── test/
│   └── jsonable_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `defprotocol` declares the contract

```elixir
defprotocol Jsonable do
  @spec to_json(t) :: String.t()
  def to_json(value)
end
```

The first argument of each protocol function drives dispatch — its type picks
the implementation.

### 2. `defimpl` provides one implementation per type

```elixir
defimpl Jsonable, for: Integer do
  def to_json(i), do: Integer.to_string(i)
end
```

Elixir supports these built-in targets: `Atom`, `BitString` (Strings),
`Float`, `Function`, `Integer`, `List`, `Map`, `PID`, `Port`, `Reference`,
`Tuple`, and any user-defined struct module.

### 3. Protocol consolidation

At compile time, `mix` consolidates all protocols: every implementation is
baked into a single optimized dispatch table. Unconsolidated protocols work
(slower) in development; consolidated protocols are the production form.

### 4. Protocols dispatch on type; behaviours on module choice

A protocol answers "what does this value know how to do?". A behaviour
answers "which adapter am I using?". They solve different problems — don't
use one where the other fits.

---

## Implementation

### Step 1: Create the project

```bash
mix new jsonable_proto
cd jsonable_proto
```

### Step 2: `lib/jsonable.ex`

```elixir
defprotocol Jsonable do
  @moduledoc """
  Turns a value into a compact JSON string. Dispatches on the type of the
  first argument — add `defimpl Jsonable, for: MyType` to extend.
  """

  @doc "Encode `value` as a JSON string."
  @spec to_json(t) :: String.t()
  def to_json(value)
end
```

### Step 3: `lib/jsonable/impls.ex`

```elixir
defmodule Jsonable.Impls do
  @moduledoc """
  Container module that groups all built-in `Jsonable` implementations.
  Having them in one file keeps the wire-up visible; you could also split
  them across files — protocol consolidation doesn't care.
  """

  defimpl Jsonable, for: Integer do
    def to_json(i), do: Integer.to_string(i)
  end

  defimpl Jsonable, for: Float do
    def to_json(f), do: Float.to_string(f)
  end

  defimpl Jsonable, for: Atom do
    # nil and booleans are atoms in Elixir; JSON has dedicated literals for them.
    def to_json(nil), do: "null"
    def to_json(true), do: "true"
    def to_json(false), do: "false"
    def to_json(atom), do: ~s("#{Atom.to_string(atom)}")
  end

  defimpl Jsonable, for: BitString do
    # BitString is the protocol name for Strings.
    def to_json(s) when is_binary(s) do
      # Minimal JSON escaping — production should use a real encoder.
      escaped = s |> String.replace("\\", "\\\\") |> String.replace("\"", "\\\"")
      ~s("#{escaped}")
    end
  end

  defimpl Jsonable, for: List do
    def to_json(list) do
      "[" <> Enum.map_join(list, ",", &Jsonable.to_json/1) <> "]"
    end
  end

  defimpl Jsonable, for: Map do
    def to_json(map) do
      pairs =
        Enum.map_join(map, ",", fn {k, v} ->
          # JSON object keys must be strings.
          Jsonable.to_json(to_string(k)) <> ":" <> Jsonable.to_json(v)
        end)

      "{" <> pairs <> "}"
    end
  end
end
```

### Step 4: `test/jsonable_test.exs`

```elixir
defmodule JsonableTest do
  use ExUnit.Case, async: true

  describe "primitives" do
    test "integers and floats" do
      assert Jsonable.to_json(42) == "42"
      assert Jsonable.to_json(3.14) == "3.14"
    end

    test "booleans and nil map to JSON literals" do
      assert Jsonable.to_json(true) == "true"
      assert Jsonable.to_json(false) == "false"
      assert Jsonable.to_json(nil) == "null"
    end

    test "strings are quoted and escaped" do
      assert Jsonable.to_json("hi") == ~s("hi")
      assert Jsonable.to_json(~s(a "b" c)) == ~s("a \\"b\\" c")
    end

    test "atoms (non-boolean, non-nil) become strings" do
      assert Jsonable.to_json(:foo) == ~s("foo")
    end
  end

  describe "collections" do
    test "list of mixed primitives" do
      assert Jsonable.to_json([1, "two", true, nil]) == ~s([1,"two",true,null])
    end

    test "map with atom keys is stringified" do
      # Map ordering is not guaranteed, so parse the two possibilities.
      encoded = Jsonable.to_json(%{a: 1, b: 2})
      assert encoded in [~s({"a":1,"b":2}), ~s({"b":2,"a":1})]
    end

    test "nested list of maps" do
      encoded = Jsonable.to_json([%{n: 1}, %{n: 2}])
      assert encoded == ~s([{"n":1},{"n":2}])
    end
  end

  describe "extensibility" do
    test "an unimplemented type raises Protocol.UndefinedError" do
      # Tuples intentionally have no impl — dispatch must fail explicitly.
      assert_raise Protocol.UndefinedError, fn ->
        Jsonable.to_json({:a, :b})
      end
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Unconsolidated protocols are slower in dev**
In `:dev`, every call walks the module list until it finds a matching `defimpl`.
In `:prod` (or after `mix compile --force`), protocols are consolidated into a
single dispatch function. If your benchmarks look suspiciously slow, check
whether `Protocol.Consolidation` ran.

**2. Adding a `defimpl` in a dependency won't consolidate**
If library A defines a protocol and library B defines an impl, your app must
recompile the protocol to pick it up. `mix deps.compile --force` if you see
`Protocol.UndefinedError` for a type you *know* has an implementation.

**3. Protocols dispatch on the CONCRETE type, not a parent**
No inheritance. A struct that looks like a map is NOT dispatched to the `Map`
impl unless you explicitly `defimpl Jsonable, for: MyStruct do ...`. That's
a feature (predictability) but surprises newcomers.

**4. `@fallback_to_any true` exists — use it sparingly**
You can declare a protocol fallback to `Any`, then define `defimpl Proto, for: Any`
as a catch-all. Convenient, but it hides missing implementations behind a
generic default. Prefer explicit impls per type. See exercise 75 for a
focused dive.

**5. When NOT to use a protocol**
If the behavior depends on more than the first argument's type (e.g., the
result depends on two argument types jointly), a protocol doesn't fit — use
a module with pattern matching, or multiple dispatched arguments via a
different design.

---

## Resources

- [Protocols — Elixir guide](https://hexdocs.pm/elixir/protocols.html)
- [`Protocol` module](https://hexdocs.pm/elixir/Protocol.html)
- [`Jason.Encoder` source](https://github.com/michalmuskala/jason/blob/master/lib/encoder.ex) — a production-grade protocol
- ["Polymorphism in Elixir" — Dashbit blog](https://dashbit.co/blog/writing-extensible-elixir-with-protocols)
