# Protocol Consolidation and Dispatch Performance

**Project**: `proto_consolidation` — measure the runtime cost of Elixir protocols before and after consolidation, implement a custom protocol with `@fallback_to_any`, and understand when consolidation matters.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

You're profiling a hot serialization path in an API gateway. Every request calls a
protocol-backed function (`Encoder.encode/1`) ~12 times on structs of 6 different types.
Under load you see 15% CPU on `:code.purge` and dispatch shuffle. A senior engineer says
"check if protocols are consolidated."

Elixir protocols are polymorphic function dispatch — you write `defprotocol Encoder`,
multiple `defimpl` clauses, and call `Encoder.encode(value)`. The compiler generates a
dispatcher per protocol that branches on the argument's type. **Consolidation** is a
post-compile step that collapses that dispatcher into a single lookup table mapping
the 6 known types directly to their impls, eliminating runtime searching.

Without consolidation (dev mode, `mix.exs` default) dispatch is O(log n) per call with
atom comparisons; with consolidation it is a single VM-level branch. On hot paths this
is a 3–10× difference.

```
proto_consolidation/
├── lib/
│   └── proto_consolidation/
│       ├── encoder.ex          # defprotocol + @fallback_to_any
│       ├── impls.ex            # defimpl for Integer, BitString, Map, Any
│       └── runner.ex           # dispatches encode/1 millions of times for bench
├── test/
│   └── encoder_test.exs
├── bench/
│   └── dispatch_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. What the compiler does for a protocol

```
defprotocol Encoder do
  def encode(value)
end
```

The compiler emits a module `Encoder` with a `encode/1` that, **when unconsolidated**,
looks roughly like:

```
def encode(value) do
  impl = Protocol.assert_impl!(Encoder, value) # walks known impls
  impl.encode(value)
end
```

After consolidation, the module is rewritten to:

```
def encode(value) when is_integer(value), do: Encoder.Integer.encode(value)
def encode(value) when is_binary(value),  do: Encoder.BitString.encode(value)
def encode(value) when is_map(value),     do: Encoder.Map.encode(value)
def encode(value),                         do: Encoder.Any.encode(value)
```

Same semantics, single jump table instead of a reflection-style lookup.

### 2. `consolidate_protocols`

`mix.exs` controls when consolidation runs:

```elixir
def project do
  [
    consolidate_protocols: Mix.env() != :dev,
    # ...
  ]
end
```

Default: **true in prod/test, false in dev**. This is on purpose — consolidation
"freezes" the impl list, so hot-reloading new `defimpl` in dev wouldn't take effect.

### 3. `@fallback_to_any`

Adding `@fallback_to_any true` plus `defimpl MyProto, for: Any` lets the protocol
accept values whose exact type has no specific impl. Useful for reasonable defaults
(e.g. `Inspect` for any struct), but it changes dispatch characteristics:
unconsolidated, every call walks until it matches or falls through to `Any`.
Consolidated, `Any` becomes the default branch.

### 4. Consolidation at runtime with `Protocol.consolidate/2`

You can consolidate programmatically for debugging or hot code-reload scenarios:

```
{:ok, binary} = Protocol.consolidate(Encoder, [Integer, BitString, Map, Any])
:code.load_binary(Encoder, 'encoder.beam', binary)
```

In production you almost never call this directly — Mix handles it.

### 5. Costs of not consolidating

- Dispatch: 3–10× slower
- `:code.get_object_code/1` churn during live reload
- `Protocol.Unconsolidated` warnings
- Dialyzer false positives on `impl_for/1`

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule ProtoConsolidation.MixProject do
  use Mix.Project

  def project do
    [
      app: :proto_consolidation,
      version: "0.1.0",
      elixir: "~> 1.16",
      consolidate_protocols: Mix.env() != :dev,
      deps: [{:benchee, "~> 1.3", only: :dev}]
    ]
  end

  def application, do: [extra_applications: [:logger]]
end
```

### Step 2: `lib/proto_consolidation/encoder.ex`

```elixir
defprotocol ProtoConsolidation.Encoder do
  @moduledoc """
  Encodes a value to an iodata-compatible representation. Used in the API
  gateway's response path.
  """

  @fallback_to_any true

  @spec encode(term()) :: iodata()
  def encode(value)
end
```

### Step 3: `lib/proto_consolidation/impls.ex`

```elixir
defimpl ProtoConsolidation.Encoder, for: Integer do
  def encode(i), do: Integer.to_string(i)
end

defimpl ProtoConsolidation.Encoder, for: BitString do
  def encode(bin) when is_binary(bin), do: [?", bin, ?"]
end

defimpl ProtoConsolidation.Encoder, for: List do
  alias ProtoConsolidation.Encoder

  def encode(list) do
    inner = list |> Enum.map(&Encoder.encode/1) |> Enum.intersperse(?,)
    [?[, inner, ?]]
  end
end

defimpl ProtoConsolidation.Encoder, for: Map do
  alias ProtoConsolidation.Encoder

  def encode(map) do
    inner =
      map
      |> Enum.map(fn {k, v} ->
        [Encoder.encode(to_string(k)), ?:, Encoder.encode(v)]
      end)
      |> Enum.intersperse(?,)

    [?{, inner, ?}]
  end
end

defimpl ProtoConsolidation.Encoder, for: Atom do
  def encode(nil), do: "null"
  def encode(true), do: "true"
  def encode(false), do: "false"
  def encode(atom), do: [?", Atom.to_string(atom), ?"]
end

defimpl ProtoConsolidation.Encoder, for: Any do
  def encode(value), do: [?", inspect(value), ?"]
end
```

### Step 4: `lib/proto_consolidation/runner.ex`

```elixir
defmodule ProtoConsolidation.Runner do
  @moduledoc "Driver used by the bench and tests to exercise dispatch."

  alias ProtoConsolidation.Encoder

  @spec encode_many([term()]) :: iodata()
  def encode_many(values), do: Enum.map(values, &Encoder.encode/1)

  @spec is_consolidated?(module()) :: boolean()
  def is_consolidated?(protocol) do
    protocol.__protocol__(:consolidated?)
  end
end
```

### Step 5: Tests

```elixir
defmodule ProtoConsolidation.EncoderTest do
  use ExUnit.Case, async: true

  alias ProtoConsolidation.Encoder
  alias ProtoConsolidation.Runner

  test "encodes integers" do
    assert IO.iodata_to_binary(Encoder.encode(42)) == "42"
  end

  test "encodes strings with quotes" do
    assert IO.iodata_to_binary(Encoder.encode("hello")) == ~s("hello")
  end

  test "encodes lists recursively" do
    assert IO.iodata_to_binary(Encoder.encode([1, "a"])) == ~s([1,"a"])
  end

  test "encodes maps with string keys" do
    out = IO.iodata_to_binary(Encoder.encode(%{"id" => 1}))
    assert out =~ ~s("id")
    assert out =~ ~s(1)
  end

  test "falls back to Any for unknown struct" do
    defmodule Weird, do: defstruct(x: 1)
    assert IO.iodata_to_binary(Encoder.encode(%Weird{})) =~ "Weird"
  end

  test "protocol is consolidated under :test env" do
    assert Runner.is_consolidated?(Encoder)
  end
end
```

### Step 6: Benchmark — measured consolidation gain

```elixir
# bench/dispatch_bench.exs
alias ProtoConsolidation.Encoder

values = [
  1, "hello", :ok, [1, 2, 3], %{a: 1, b: "x"}, true, nil
]

Benchee.run(
  %{
    "protocol dispatch (consolidated)" => fn ->
      Enum.each(values, &Encoder.encode/1)
    end,
    "manual case dispatch (baseline)" => fn ->
      Enum.each(values, fn
        v when is_integer(v) -> Integer.to_string(v)
        v when is_binary(v) -> [?", v, ?"]
        v when is_list(v) -> "[...]"
        v when is_map(v) -> "{...}"
        v when is_atom(v) -> Atom.to_string(v)
        v -> inspect(v)
      end)
    end
  },
  time: 5,
  warmup: 2
)
```

Running with `MIX_ENV=prod mix run bench/dispatch_bench.exs` and then toggling
`consolidate_protocols: false`, you will see protocol dispatch move from ~1.05× the
manual case cost to ~3–5× slower. The overhead exists but it is predictable.

---

## Trade-offs and production gotchas

**1. Dev mode is slower by design.** Do not panic when a flame graph shows
`Protocol.Undefined` resolution — re-run under `MIX_ENV=prod` for real numbers.

**2. `@fallback_to_any` has hidden cost.** Every type without a specific impl goes
through `Encoder.Any`. If `Any.encode` calls `inspect/1` on complex structs, this is
expensive. Provide specific impls for hot types.

**3. Consolidation freezes impls.** A release built with `consolidate_protocols: true`
will not see a `defimpl` added via `:code.load_binary` unless you re-consolidate. For
plugin-style architectures, either disable consolidation or call
`Protocol.consolidate/2` after loading new modules.

**4. `Protocol.UndefinedError` in prod but not dev.** Happens when a new type appears
that was not known when consolidation ran. Detect with tests that cover all expected
types plus the `Any` fallback.

**5. Dialyzer and protocols.** Dialyzer may flag `value :: term()` as too broad.
Tighten with `@type t :: integer() | binary() | list() | map() | atom()` inside the
`defprotocol` block using `@type t`.

**6. Struct derivation.** `@derive` (exercise 137) is how `Inspect` and `Jason.Encoder`
reach structs without forcing users to write `defimpl`. Understand it alongside
consolidation.

**7. Protocol.Consolidated check.** At runtime verify with
`MyProtocol.__protocol__(:consolidated?)`. Many production bugs stem from releases
built in the wrong env.

**8. When NOT to use protocols.** For 2–3 known types, a plain `case` is faster and
more explicit. Protocols amortize when the set is open or has 5+ impls.

---

## Resources

- [`Protocol` — hexdocs.pm](https://hexdocs.pm/elixir/Protocol.html) — including `consolidate/2`
- [Elixir docs: protocol consolidation](https://hexdocs.pm/mix/Mix.Tasks.Compile.Protocols.html)
- [Jason source — consolidated Jason.Encoder](https://github.com/michalmuskala/jason/blob/master/lib/encoder.ex)
- [José Valim — "Protocols in Elixir"](https://dashbit.co/blog) — Dashbit
- [BEAM Book — type test instructions](https://blog.stenmans.org/theBeamBook/) — free
- [Erlang efficiency guide](https://www.erlang.org/doc/efficiency_guide/introduction.html)
- [Fred Hébert — "What is a protocol anyway?"](https://ferd.ca/)
