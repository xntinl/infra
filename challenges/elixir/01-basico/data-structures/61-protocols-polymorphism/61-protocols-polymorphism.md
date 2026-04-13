# Protocols for Runtime Polymorphism

**Project**: `jsonish` — a serializer that turns strings, integers, and custom structs into a JSON-like string

---

## Project structure

```
jsonish/
├── lib/
│   ├── jsonish.ex          # the protocol
│   ├── jsonish/
│   │   ├── builtins.ex     # impls for built-in types
│   │   └── user.ex         # custom struct + its impl
└── test/
    └── jsonish_test.exs
```

---

## The business problem

You want one entry point — `Jsonish.encode(value)` — that works for any type you or your
users add later, without modifying a central dispatcher. A `case` statement per type
becomes a maintenance pit: every new type means editing a shared file.

Protocols solve this. Each type declares its own implementation. The dispatch happens
at runtime based on the value's tag.

---

## Core concepts

### `defprotocol`

A protocol is a dispatch table keyed by the value's type. You declare the function
signatures; implementations live elsewhere. The protocol itself has no logic.

### `defimpl`

Each `defimpl Protocol, for: Type` registers an implementation. Elixir resolves dispatch
by inspecting the value: structs dispatch on the struct module, other values on their
built-in kind (`Integer`, `BitString`, `List`, `Map`, `Tuple`, `Atom`, `Function`,
`PID`, `Port`, `Reference`, `Any`).

### `@fallback_to_any`

With `@fallback_to_any true` + `defimpl Protocol, for: Any`, unknown types get a default
impl. Without it, dispatch raises `Protocol.UndefinedError`. Use `fallback_to_any`
sparingly — it turns "missing impl" from a loud error into silent behavior.

---

## Why protocols and not a giant `case`

A `case` dispatcher forces every new type to edit a central file. That turns type extensibility into a cross-team merge conflict — and worse, third-party libraries cannot add support for your protocol without forking you. Protocols invert the dependency: the type owner writes the impl, the protocol owner never learns about new types.

Behaviours sound similar but answer a different question. Behaviours dispatch when the **caller** chooses the module; protocols dispatch when the **value** already carries its type. Serialization is value-driven (you hold a value, you want JSON), so protocols fit.

---

## Design decisions

**Option A — central `case`/`cond` dispatcher in a single encoder module**
- Pros: no metaprogramming; trivial to read the whole dispatch table at once; fastest for tiny fixed type sets.
- Cons: every new type edits the same file; third parties cannot extend; the module grows without bound; consolidation benefits unavailable.

**Option B — `defprotocol` + per-type `defimpl`** (chosen)
- Pros: open extension (any module can `defimpl Jsonish, for: MyType`); consolidated dispatch at release time is near-constant cost; impls colocate with their type for discoverability.
- Cons: discovery requires knowing to search for `defimpl`; `@fallback_to_any` is tempting and silently dangerous; missing impl error is runtime, not compile-time.

Chose **B** because the type set is open — user structs must participate without us knowing about them — and the consolidation pass neutralizes the runtime-dispatch cost in releases.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"jason", "~> 1.0"},
    {:"poison", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Lay out protocol, impls, and struct in separate files to mirror the open/closed boundary — new types extend the system without editing it.

```bash
mix new jsonish
cd jsonish
mkdir -p lib/jsonish
```

### Step 2: `lib/jsonish.ex` — the protocol

**Objective**: Declare the dispatch table with `defprotocol` so the encoding function has one public signature and zero type-specific branches.

```elixir
defprotocol Jsonish do
  @moduledoc """
  Encodes a term into a JSON-like string.

  Not RFC 8259 compliant — this is an exercise in polymorphism,
  not a replacement for Jason/Poison.
  """

  # @fallback_to_any is off by default. We want a hard error for
  # unsupported types — silent fallback masks missing impls.
  @spec encode(t) :: String.t()
  def encode(value)
end
```

### Step 3: `lib/jsonish/builtins.ex` — impls for standard types

**Objective**: Provide `defimpl` cases for stdlib types to prove dispatch works on tags the caller does not own and cannot modify.

```elixir
defimpl Jsonish, for: Integer do
  # Integer is straightforward — delegate to the Integer module.
  def encode(n), do: Integer.to_string(n)
end

defimpl Jsonish, for: BitString do
  # BitString catches all binaries (Elixir strings ARE binaries).
  # We escape quotes but not unicode — again, not production JSON.
  def encode(s) when is_binary(s) do
    escaped = String.replace(s, ~s("), ~s(\\"))
    ~s("#{escaped}")
  end
end

defimpl Jsonish, for: List do
  # Recursive dispatch: each element re-enters the protocol.
  # This is why protocols compose — we don't know or care what's inside.
  def encode(list) do
    inner = list |> Enum.map(&Jsonish.encode/1) |> Enum.join(",")
    "[" <> inner <> "]"
  end
end

defimpl Jsonish, for: Atom do
  # `nil`, `true`, `false` are atoms in Elixir.
  def encode(nil), do: "null"
  def encode(true), do: "true"
  def encode(false), do: "false"
  def encode(atom), do: ~s("#{Atom.to_string(atom)}")
end
```

### Step 4: `lib/jsonish/user.ex` — custom struct with its own impl

**Objective**: Ship a user-defined struct with its own impl colocated, showing how new types plug into the protocol without touching the core.

```elixir
defmodule Jsonish.User do
  @enforce_keys [:id, :email]
  defstruct [:id, :email, :admin?]

  @type t :: %__MODULE__{id: integer(), email: String.t(), admin?: boolean()}
end

# The impl for a struct. We encode it as a JSON object with explicit keys.
# We intentionally skip `:admin?` to illustrate: the impl controls the shape.
defimpl Jsonish, for: Jsonish.User do
  def encode(%Jsonish.User{id: id, email: email}) do
    ~s({"id":#{Jsonish.encode(id)},"email":#{Jsonish.encode(email)}})
  end
end
```

### Step 5: `test/jsonish_test.exs`

**Objective**: Verify dispatch across builtin and user types, plus the `Protocol.UndefinedError` path when no impl matches.

```elixir
defmodule JsonishTest do
  use ExUnit.Case, async: true

  test "encodes integers" do
    assert Jsonish.encode(42) == "42"
  end

  test "encodes strings with escaped quotes" do
    assert Jsonish.encode(~s(hello "world")) == ~s("hello \\"world\\"")
  end

  test "encodes nil, true, false as JSON literals" do
    assert Jsonish.encode(nil) == "null"
    assert Jsonish.encode(true) == "true"
    assert Jsonish.encode(false) == "false"
  end

  test "encodes heterogeneous lists via recursive dispatch" do
    assert Jsonish.encode([1, "two", nil]) == ~s([1,"two",null])
  end

  test "encodes a custom struct using its own impl" do
    user = %Jsonish.User{id: 7, email: "a@b.com", admin?: true}
    # The impl skips :admin? — this asserts the impl controls the shape.
    assert Jsonish.encode(user) == ~s({"id":7,"email":"a@b.com"})
  end

  test "raises Protocol.UndefinedError for unsupported types" do
    # No impl for Tuple — we want a loud error, not silent fallback.
    assert_raise Protocol.UndefinedError, fn -> Jsonish.encode({:a, :b}) end
  end
end
```

### Step 6: Run the tests

**Objective**: Run the suite to confirm each impl is reachable and that missing impls raise loudly at the dispatch site.

```bash
mix test
```

### Why this works

The protocol file declares the contract with zero logic — all the per-type code lives in `defimpl` blocks next to the type they serve. Elixir resolves dispatch at call time by inspecting the value's `__struct__` (for structs) or built-in kind tag (for integers, bitstrings, etc.), which is O(1) after protocol consolidation. No impl means a loud `Protocol.UndefinedError`, which is the correct failure mode: a missing case is a bug, not a default-to-`nil` moment.

---


## Key Concepts

### 1. Protocols Define a Behavior Contract Across Types

Protocols are like interfaces. Types implement them by providing specific behavior. This avoids long `if` chains checking types.

### 2. Protocol Dispatch is Dynamic

When you call a protocol function, Elixir checks the type at runtime and calls the correct implementation. This is polymorphism without classes.

### 3. Built-in Protocols

`Inspect`, `Enumerable`, `String.Chars`, `Collectable` are protocols built into Elixir. Implementing them for your types makes your code integrate seamlessly with the standard library.

---
## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    values = [
      "hello",
      42,
      %Jsonish.User{id: 1, name: "Ana", email: "a@b.com"}
    ]

    {us, _} =
      :timer.tc(fn ->
        Enum.each(1..100_000, fn _ ->
          Enum.each(values, &Jsonish.encode/1)
        end)
      end)

    IO.puts("encode x300k values: #{us} µs (#{us / 300_000} µs/call)")
  end
end

Bench.run()
```

Target: under 2 µs per `encode/1` call in `:prod` with consolidated protocols. Without consolidation the cost can be 5–10× higher — run `mix compile --force` in `:prod` to confirm the impact.

---

## Trade-offs

| Mechanism | Dispatch by | When to use |
|-----------|-------------|-------------|
| `case` statement | value shape | closed set of types known at one site |
| Pattern-matched function clauses | value shape in one module | logic stays inside that module |
| Protocols | value's type tag, across modules | open set — third-party types may add impls |
| Behaviours | module passed explicitly | caller picks impl; no runtime dispatch on data |

Protocols = **value-driven** polymorphism. Behaviours = **module-driven**: the caller picks an implementation module explicitly, typically at configuration or startup time, instead of dispatching on the value itself.

---

## Common production mistakes

**1. `@fallback_to_any true` as a shortcut**
It hides missing implementations. You ship, a new type sneaks in, the `Any` impl returns
nonsense, and you debug for an hour. Prefer the loud `Protocol.UndefinedError`.

**2. Putting business logic in the protocol definition**
The protocol file should have signatures and docs only. Logic in `defprotocol` cannot be
overridden per type.

**3. Forgetting `Protocol.consolidate/1` in releases**
In `:prod`, Mix consolidates protocols for faster dispatch. If you build a release without
consolidation (e.g. custom build steps), every call pays a lookup cost. `mix release`
does this by default — do not disable `consolidate_protocols`.

**4. One impl per `defimpl`, duplicated across similar types**
If `Map` and `Keyword` share 90% of logic, extract a helper module and call it from both
impls. `defimpl` does not support inheritance.

---

## When NOT to use

- **Single call site, fixed set of types**: a `case` is clearer. Protocols add indirection.
- **You need to pick the impl explicitly**: use a behaviour. Protocols dispatch on the value — you cannot say "use impl X for this value of type Y".
- **Performance-critical inner loops**: protocol dispatch has a (small) cost. For a 100M-iteration hot loop, specialize manually.

---

## Reflection

1. Jason exposes `Jason.Encoder` as a protocol. If you build a payment system where every monetary value must serialize with exactly two decimals, would you add a `defimpl Jason.Encoder, for: Money` in your app — or wrap `Jason.encode/1` in a higher-level `Payment.encode/1` that normalizes first? What are the blast radii of each?
2. The project chose to let unknown types raise `Protocol.UndefinedError`. Imagine a multi-tenant SaaS where a customer's plugin returns arbitrary data. Would you still refuse `@fallback_to_any`, or is there a middle ground (e.g. `Any` impl that returns `{:error, :unsupported}`)?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule JsonishTest do
  use ExUnit.Case, async: true

  test "encodes integers" do
    assert Jsonish.encode(42) == "42"
  end

  test "encodes strings with escaped quotes" do
    assert Jsonish.encode(~s(hello "world")) == ~s("hello \\"world\\"")
  end

  test "encodes nil, true, false as JSON literals" do
    assert Jsonish.encode(nil) == "null"
    assert Jsonish.encode(true) == "true"
    assert Jsonish.encode(false) == "false"
  end

  test "encodes heterogeneous lists via recursive dispatch" do
    assert Jsonish.encode([1, "two", nil]) == ~s([1,"two",null])
  end

  test "encodes a custom struct using its own impl" do
    user = %Jsonish.User{id: 7, email: "a@b.com", admin?: true}
    # The impl skips :admin? — this asserts the impl controls the shape.
    assert Jsonish.encode(user) == ~s({"id":7,"email":"a@b.com"})
  end

  test "raises Protocol.UndefinedError for unsupported types" do
    # No impl for Tuple — we want a loud error, not silent fallback.
    assert_raise Protocol.UndefinedError, fn -> Jsonish.encode({:a, :b}) end
  end
end
```

## Resources

- [Elixir docs — `defprotocol`](https://hexdocs.pm/elixir/Kernel.html#defprotocol/2)
- [Elixir docs — Protocols guide](https://hexdocs.pm/elixir/protocols.html)
- [Jason source](https://github.com/michalmuskala/jason/blob/master/lib/encoder.ex) — real-world protocol-based JSON encoder
