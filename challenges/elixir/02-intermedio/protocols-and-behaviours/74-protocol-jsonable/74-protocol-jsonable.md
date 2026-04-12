# Deep `Jsonable` protocol — `@fallback_to_any` and `@derive`

**Project**: `jsonable_deep` — a `Jsonable` protocol with a `@fallback_to_any true` default, a reflective `Any` impl for structs, and `@derive Jsonable` for opt-in auto-encoding.

---

## Project context

A basic `Jsonable` protocol works for primitives and collections but
fails loudly on any struct — because protocols dispatch on the concrete type,
and a struct is its own type. In a real app you have hundreds of structs:
writing `defimpl Jsonable, for: MyStruct` for every one is tedious.

Elixir gives you two escape hatches:

1. **`@fallback_to_any true`** — declare an impl for `Any` that catches anything
   without a specific impl.
2. **`@derive Jsonable`** — let a struct opt in to a generated impl with one line.

This exercise combines both: a reflective `Any` impl that handles structs by
converting them to maps, and a deriver that lets users pick which fields to
expose.

Project structure:

```
jsonable_deep/
├── lib/
│   ├── jsonable.ex
│   ├── jsonable/impls.ex
│   └── sample_structs.ex
├── test/
│   └── jsonable_deep_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `@fallback_to_any true` enables an `Any` impl

```elixir
defprotocol Jsonable do
  @fallback_to_any true
  def to_json(value)
end

defimpl Jsonable, for: Any do
  def to_json(_), do: "..."
end
```

Without `@fallback_to_any`, a `defimpl for: Any` is rejected. With it, types
that have no specific impl fall through to the `Any` clause.

### 2. `@derive Jsonable` generates per-struct impls

```elixir
@derive {Jsonable, only: [:id, :name]}
defstruct [:id, :name, :password]
```

The deriver macro (defined in the protocol's `Any` impl with `defmacro __deriving__/3`)
synthesizes a `defimpl` for the struct at compile time. Options let the
caller whitelist or blacklist fields.

### 3. `Map.from_struct/1` is the reflective trick

The `Any` impl typically converts a struct to a map with `Map.from_struct/1`
(which drops the `__struct__` key) and recurses. This gives sensible default
encoding for any struct without a hand-written impl.

### 4. `@derive` beats fallback — it's explicit and field-selective

`@fallback_to_any` is convenient but encodes everything, including fields you
meant to keep private. `@derive {Jsonable, only: [...]}` is the production
choice: explicit, auditable, and easy to review.

---

## Why combine `@fallback_to_any` with `@derive`

**`@fallback_to_any` only.** Every struct encodes — including sensitive fields you forgot to exclude. Password/token leaks are a matter of time.

**`@derive` only (no fallback).** Every struct needs an explicit `@derive Jsonable` line. Forgetting one surfaces as `Protocol.UndefinedError` at runtime, not at compile time.

**Both (chosen).** `@derive {Jsonable, only: [...]}` for structs where you want explicit, field-selective encoding; reflective fallback for quick-and-dirty audit types (logs, introspection tools) where every field is OK to emit. The fallback raises on types that should never serialize (PIDs, refs, funs).

---

## Design decisions

**Option A — Hand-write `defimpl Jsonable, for: MyStruct` on every struct**
- Pros: Zero macros, fully auditable per struct.
- Cons: N files to maintain; adding a field means editing two places; tempting to cut corners.

**Option B — `@fallback_to_any` + `__deriving__/3` with `:only` / `:except`** (chosen)
- Pros: `@derive {Jsonable, only: [:id, :name]}` is one line on each struct and enforces a whitelist; compile-time deriver means no runtime reflection on hot paths; the `Any` fallback catches audit-style structs without ceremony but refuses unserializable types.
- Cons: Two escape hatches coexist — easy to pick the wrong one for sensitive data; `@derive` must be placed before `defstruct`.

→ Chose **B** because it matches how production libraries (Jason, Ecto) ship derivation: explicit, field-selective, compile-time-generated.

---

## Implementation

### Step 1: Create the project

```bash
mix new jsonable_deep
cd jsonable_deep
```

### Step 2: `lib/jsonable.ex`

```elixir
defprotocol Jsonable do
  @moduledoc """
  Recursive JSON encoder. Falls back to `Any` for unmatched types, and
  supports `@derive {Jsonable, only: [...]}` on structs for opt-in
  field-level control.
  """

  @fallback_to_any true

  @spec to_json(t) :: String.t()
  def to_json(value)
end
```

### Step 3: `lib/jsonable/impls.ex`

```elixir
defmodule Jsonable.Impls do
  @moduledoc false

  defimpl Jsonable, for: Integer do
    def to_json(i), do: Integer.to_string(i)
  end

  defimpl Jsonable, for: Float do
    def to_json(f), do: Float.to_string(f)
  end

  defimpl Jsonable, for: Atom do
    def to_json(nil), do: "null"
    def to_json(true), do: "true"
    def to_json(false), do: "false"
    def to_json(atom), do: ~s("#{Atom.to_string(atom)}")
  end

  defimpl Jsonable, for: BitString do
    def to_json(s) when is_binary(s) do
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
    # Plain maps (non-struct). Structs with an impl use their own.
    def to_json(map) do
      pairs =
        Enum.map_join(map, ",", fn {k, v} ->
          Jsonable.to_json(to_string(k)) <> ":" <> Jsonable.to_json(v)
        end)

      "{" <> pairs <> "}"
    end
  end

  defimpl Jsonable, for: Any do
    @moduledoc """
    Catch-all for structs (and other types) without an explicit impl. Also
    implements `__deriving__/3` — the hook for `@derive {Jsonable, only: [...]}`.
    """

    # Compile-time hook invoked when a struct uses `@derive Jsonable`.
    defmacro __deriving__(module, struct, options) do
      fields = fields_for(struct, options)

      quote do
        defimpl Jsonable, for: unquote(module) do
          def to_json(value) do
            pairs =
              unquote(fields)
              |> Enum.map_join(",", fn field ->
                # Encode the key as a JSON string and recurse on the value.
                key_json = Jsonable.to_json(Atom.to_string(field))
                value_json = Jsonable.to_json(Map.fetch!(value, field))
                key_json <> ":" <> value_json
              end)

            "{" <> pairs <> "}"
          end
        end
      end
    end

    # Runtime fallback: for structs without @derive, convert to map and recurse.
    # Throws for types we actively refuse to encode (PIDs, refs, funs) to avoid
    # silently stringifying values that will never round-trip.
    def to_json(%_{} = struct) do
      struct
      |> Map.from_struct()
      |> Jsonable.to_json()
    end

    def to_json(other) do
      raise Protocol.UndefinedError,
        protocol: Jsonable,
        value: other,
        description: "no Jsonable implementation for #{inspect(other)}"
    end

    # Compute the list of fields for the derived impl, honoring :only / :except.
    defp fields_for(struct, options) do
      all_fields = struct |> Map.from_struct() |> Map.keys()

      cond do
        only = Keyword.get(options, :only) -> only
        except = Keyword.get(options, :except) -> all_fields -- except
        true -> all_fields
      end
    end
  end
end
```

### Step 4: `lib/sample_structs.ex`

```elixir
defmodule SampleStructs do
  @moduledoc "Structs used by the test suite to exercise derive and fallback."

  defmodule Point do
    @derive Jsonable
    @enforce_keys [:x, :y]
    defstruct [:x, :y]
  end

  defmodule User do
    # Whitelist: :password must NOT appear in the output.
    @derive {Jsonable, only: [:id, :name]}
    @enforce_keys [:id, :name, :password]
    defstruct [:id, :name, :password]
  end

  defmodule AuditRow do
    # No @derive — exercises the Any fallback via Map.from_struct/1.
    @enforce_keys [:table, :changes]
    defstruct [:table, :changes]
  end
end
```

### Step 5: `test/jsonable_deep_test.exs`

```elixir
defmodule JsonableDeepTest do
  use ExUnit.Case, async: true
  alias SampleStructs.{Point, User, AuditRow}

  describe "derived impl (explicit, field-selective)" do
    test "encodes all fields when @derive Jsonable is bare" do
      encoded = Jsonable.to_json(%Point{x: 1, y: 2})
      # Map key order is not guaranteed — accept either permutation.
      assert encoded in [~s({"x":1,"y":2}), ~s({"y":2,"x":1})]
    end

    test "respects :only — sensitive fields are not encoded" do
      user = %User{id: 1, name: "Jane", password: "secret"}
      encoded = Jsonable.to_json(user)
      refute encoded =~ "password"
      refute encoded =~ "secret"
      assert encoded =~ ~s("name":"Jane")
    end
  end

  describe "Any fallback (reflective, no @derive)" do
    test "structs without @derive still encode via Map.from_struct/1" do
      row = %AuditRow{table: "users", changes: %{name: "new"}}
      encoded = Jsonable.to_json(row)
      assert encoded =~ ~s("table":"users")
      assert encoded =~ "changes"
    end

    test "unsupported type raises Protocol.UndefinedError" do
      assert_raise Protocol.UndefinedError, fn ->
        # A PID has no specific impl and no sensible fallback.
        Jsonable.to_json(self())
      end
    end
  end

  describe "composition with primitives" do
    test "a struct nested in a list encodes correctly" do
      encoded = Jsonable.to_json([%Point{x: 1, y: 2}, %Point{x: 3, y: 4}])
      # Enough to verify recursion — exact order of keys within each object
      # is map-ordering-dependent.
      assert encoded =~ "\"x\":1"
      assert encoded =~ "\"x\":3"
    end
  end
end
```

### Step 6: Run

```bash
mix test
```

### Why this works

`defmacro __deriving__/3` runs at compile time when a struct adds `@derive Jsonable`, generating a dedicated `defimpl` with exactly the fields you asked for. At call time, dispatch finds the derived impl directly — no reflection, no cost. Structs without `@derive` fall through to `Any.to_json/1`, which converts via `Map.from_struct/1` and recurses; unserializable values (PIDs, refs, funs) hit the second `Any.to_json/1` clause and raise `Protocol.UndefinedError` instead of producing garbage.

---

## Benchmark

```elixir
user = %SampleStructs.User{id: 1, name: "Jane", password: "secret"}
row = %SampleStructs.AuditRow{table: "users", changes: %{name: "new"}}

{derived_time, _} =
  :timer.tc(fn -> Enum.each(1..100_000, fn _ -> Jsonable.to_json(user) end) end)

{fallback_time, _} =
  :timer.tc(fn -> Enum.each(1..100_000, fn _ -> Jsonable.to_json(row) end) end)

IO.puts("derived=#{derived_time / 100_000} µs  fallback=#{fallback_time / 100_000} µs")
```

Target esperado: el impl derivado debería ser ~2–5x más rápido que el fallback reflectivo (evita `Map.from_struct/1` + recursión sobre el map). Diferencias mayores suelen indicar que el protocolo no se consolidó.

---

## Trade-offs and production gotchas

**1. `@fallback_to_any` silences missing-impl errors globally**
Every type you forgot now quietly goes through the fallback. That's convenient
until the fallback produces wrong output and you can't tell what's using it.
Always pair fallback with a blacklist: reject PIDs, refs, functions, and
anything that doesn't serialize cleanly.

**2. `@derive` must come BEFORE `defstruct`**
The deriver runs as an attribute hook; putting it after `defstruct` is
silently ignored. No warning. Many hours lost here.

**3. Field ordering is NOT stable across Elixir versions**
Maps don't guarantee key order, so `Jason.encode(user)` and
`Jsonable.to_json(user)` can produce different strings for the same input.
Don't use the output as a cache key or a signature without canonicalization.

**4. The deriver is a macro — keep it small**
Everything in the `quote` block is compiled once per deriving struct. Heavy
logic there slows compilation and bloats beam files. Compute the field list
at compile time, emit minimal runtime code.

**5. When NOT to use `@fallback_to_any`**
Libraries should almost never enable it. A missing impl is a contract signal
to the user — "this type is unsupported, please implement". Fallback makes
the error silent. Use it only in app-internal protocols where you control
both sides.

---

## Reflection

- You roll this out across a service with 200 structs. Half are already `@derive`d, half rely on the `Any` fallback. A security review requires "no PII in logs". How do you use `@fallback_to_any` vs `@derive` to enforce that, and which would you revoke at the protocol level?
- `@derive {Jsonable, only: [...]}` is a whitelist. Would you prefer `:except` (blacklist) for audit structs, or does whitelist-by-default belong on *every* struct? Argue one position.

---

## Resources

- [`Protocol` — `@fallback_to_any` and derivation](https://hexdocs.pm/elixir/Protocol.html)
- [`Jason.Encoder` — a real-world `@derive` protocol](https://hexdocs.pm/jason/Jason.Encoder.html)
- ["Writing extensible Elixir with Protocols" — Dashbit](https://dashbit.co/blog/writing-extensible-elixir-with-protocols)
