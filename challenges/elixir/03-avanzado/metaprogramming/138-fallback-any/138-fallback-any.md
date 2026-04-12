# `@fallback_to_any true` — Deep Dive and Performance Cost

**Project**: `fallback_any` — build a protocol both with and without `@fallback_to_any`, benchmark dispatch in each mode with consolidated vs unconsolidated compilation, and understand where the performance cost comes from.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

You are writing `Cache.Serializer`, a protocol that serializes any cache value to a
binary. Most types are covered by explicit impls (Integer, BitString, Map, Atom, List).
You want unknown types to succeed with a generic fallback (`:erlang.term_to_binary/1`)
rather than raise `Protocol.UndefinedError`. The obvious answer is
`@fallback_to_any true`.

But someone on the team claims the fallback "slows every protocol call even when a
specific impl exists". You want to prove or disprove this empirically, and understand
what exactly the compiler does when `@fallback_to_any` is on vs off.

```
fallback_any/
├── lib/
│   └── fallback_any/
│       ├── with_fallback.ex       # @fallback_to_any true
│       ├── without_fallback.ex    # explicit impls only
│       └── bench_driver.ex
├── test/
│   └── fallback_any_test.exs
├── bench/
│   └── dispatch_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. What `@fallback_to_any true` compiles to

When a protocol has `@fallback_to_any true` and a `defimpl ... for: Any`,
consolidated dispatch becomes (conceptually):

```
def encode(v) when is_integer(v), do: Impl.Integer.encode(v)
def encode(v) when is_binary(v),  do: Impl.BitString.encode(v)
# ... all specific impls ...
def encode(v),                    do: Impl.Any.encode(v)   # catch-all
```

Without the fallback:

```
def encode(v) when is_integer(v), do: Impl.Integer.encode(v)
# ...
def encode(v),                    do: raise Protocol.UndefinedError, …
```

The extra clause cost is one comparison — negligible.

### 2. Where the cost actually comes from

The overhead people blame on `@fallback_to_any` is usually:

- **Unconsolidated mode**: dispatch walks a runtime list
- **`Any` impl doing heavy work**: `inspect/1`, `term_to_binary/1` with large terms
- **Cache misses**: the `Any` clause invalidates BEAM's type-test prediction on very
  branch-heavy code

### 3. `Protocol.impl_for/1` and `impl_for!/1`

You can ask which impl handles a value:

```
Cache.Serializer.impl_for(42)        # => Cache.Serializer.Integer
Cache.Serializer.impl_for(%Weird{})  # => Cache.Serializer.Any (with fallback) OR nil
```

`impl_for!/1` raises for `nil`. Useful for unit tests of fallback behavior.

### 4. `Protocol.UndefinedError`

Raised at the dispatch site when no impl matches and no `Any` fallback exists.
Includes the value, its type, and a hint about consolidation:

```
** (Protocol.UndefinedError) protocol Cache.Serializer not implemented for %Weird{}…
```

### 5. Consolidation + fallback interaction

Consolidation "freezes" the impl list. If `@fallback_to_any` is on but no `defimpl
for: Any` block exists, consolidation emits a fallback that raises — equivalent
to no fallback. The annotation by itself does nothing without an `Any` impl.

---

## Implementation

### Step 1: `lib/fallback_any/with_fallback.ex`

```elixir
defprotocol FallbackAny.WithFallback do
  @moduledoc "Protocol with Any fallback — handles unknown values."
  @fallback_to_any true

  @spec encode(term()) :: binary()
  def encode(value)
end

defimpl FallbackAny.WithFallback, for: Integer do
  def encode(i), do: <<i::signed-64>>
end

defimpl FallbackAny.WithFallback, for: BitString do
  def encode(bin) when is_binary(bin), do: bin
end

defimpl FallbackAny.WithFallback, for: Atom do
  def encode(a), do: Atom.to_string(a)
end

defimpl FallbackAny.WithFallback, for: List do
  def encode(list), do: :erlang.term_to_binary(list)
end

defimpl FallbackAny.WithFallback, for: Map do
  def encode(m), do: :erlang.term_to_binary(m)
end

defimpl FallbackAny.WithFallback, for: Any do
  def encode(value), do: :erlang.term_to_binary(value)
end
```

### Step 2: `lib/fallback_any/without_fallback.ex`

```elixir
defprotocol FallbackAny.WithoutFallback do
  @moduledoc "Same protocol without Any — raises on unknown values."

  @spec encode(term()) :: binary()
  def encode(value)
end

defimpl FallbackAny.WithoutFallback, for: Integer do
  def encode(i), do: <<i::signed-64>>
end

defimpl FallbackAny.WithoutFallback, for: BitString do
  def encode(bin) when is_binary(bin), do: bin
end

defimpl FallbackAny.WithoutFallback, for: Atom do
  def encode(a), do: Atom.to_string(a)
end

defimpl FallbackAny.WithoutFallback, for: List do
  def encode(list), do: :erlang.term_to_binary(list)
end

defimpl FallbackAny.WithoutFallback, for: Map do
  def encode(m), do: :erlang.term_to_binary(m)
end
```

### Step 3: `lib/fallback_any/bench_driver.ex`

```elixir
defmodule FallbackAny.BenchDriver do
  @moduledoc "Drives many dispatches for benchmarking."

  alias FallbackAny.{WithFallback, WithoutFallback}

  @common_values [42, "hello", :ok, [1, 2, 3], %{a: 1}]
  @unknown_value %{__struct__: :SomeWeirdType, x: 1}

  def common_with_fallback do
    Enum.each(@common_values, &WithFallback.encode/1)
  end

  def common_without_fallback do
    Enum.each(@common_values, &WithoutFallback.encode/1)
  end

  def fallback_only do
    WithFallback.encode(@unknown_value)
  end
end
```

### Step 4: Tests

```elixir
defmodule FallbackAnyTest do
  use ExUnit.Case, async: true

  alias FallbackAny.{WithFallback, WithoutFallback}

  describe "WithFallback (has Any)" do
    test "encodes known types via their specific impl" do
      assert is_binary(WithFallback.encode(42))
      assert WithFallback.encode("abc") == "abc"
      assert WithFallback.encode(:ok) == "ok"
    end

    test "encodes unknown struct via Any impl" do
      defmodule Weird, do: defstruct(x: 1)
      bin = WithFallback.encode(%Weird{})
      assert is_binary(bin)
      assert :erlang.binary_to_term(bin) == %Weird{x: 1}
    end

    test "impl_for returns Any for unknown" do
      defmodule Weird2, do: defstruct(a: 1)
      assert WithFallback.impl_for(%Weird2{}) == FallbackAny.WithFallback.Any
    end
  end

  describe "WithoutFallback (no Any)" do
    test "encodes known types" do
      assert is_binary(WithoutFallback.encode(42))
    end

    test "raises Protocol.UndefinedError on unknown struct" do
      defmodule Weird3, do: defstruct(y: 1)

      assert_raise Protocol.UndefinedError, fn ->
        WithoutFallback.encode(%Weird3{})
      end
    end

    test "impl_for returns nil for unknown" do
      defmodule Weird4, do: defstruct(z: 1)
      assert WithoutFallback.impl_for(%Weird4{}) == nil
    end
  end

  describe "consolidation" do
    test "both protocols are consolidated in test env" do
      assert WithFallback.__protocol__(:consolidated?)
      assert WithoutFallback.__protocol__(:consolidated?)
    end
  end
end
```

### Step 5: Benchmark

```elixir
# bench/dispatch_bench.exs
alias FallbackAny.BenchDriver

Benchee.run(
  %{
    "common types — with fallback"    => fn -> BenchDriver.common_with_fallback() end,
    "common types — without fallback" => fn -> BenchDriver.common_without_fallback() end,
    "fallback-only (Any impl)"        => fn -> BenchDriver.fallback_only() end
  },
  time: 5,
  warmup: 2
)
```

**Expected results (consolidated, prod)**:

| Scenario | ips |
| --- | --- |
| common types — with fallback | ~5 M |
| common types — without fallback | ~5 M |
| fallback-only | ~500 K (term_to_binary dominates) |

The fallback's presence itself is essentially free; the `Any` impl's *work* is what
you pay for when unknown types arrive.

---

## Trade-offs and production gotchas

**1. The fallback must still exist.** `@fallback_to_any true` without `defimpl for:
Any` raises at compile time. This is the compiler protecting you.

**2. `Any` impl as a trap.** Beginners put `raise "not implemented"` in `Any`. That
defeats the purpose — use `@fallback_to_any false` or omit it, so consolidation
emits a better `Protocol.UndefinedError` with type info.

**3. `Any` + `inspect/1` is slow.** If `Any.encode(v)` calls `inspect/1` on large
maps, you'll see 100× latency for unknown types. Prefer structural serialization
like `term_to_binary`.

**4. Consolidation hides bugs.** In dev (unconsolidated), dispatch walks through
every known impl — including impls from other apps. In prod (consolidated), only
the impls present at compile time are considered. A library added at runtime via
`Code.load_file/1` will NOT be picked up.

**5. `@fallback_to_any` and structs.** If no specific `defimpl` exists for
`%MyStruct{}`, dispatch falls through `Any`. Developers often assume protocol
methods "just work" on their structs — silently picking the `Any` path.
Document clearly which protocols your structs are expected to cover.

**6. Dialyzer spec on `Any.encode`.** The fallback's spec should be
`term() :: binary()` — otherwise Dialyzer complains that the overall protocol spec
is weaker than claimed.

**7. Hot code reload.** Adding a new `defimpl` at runtime in a consolidated release
requires re-consolidation. See `Protocol.consolidate/2` (exercise 23).

**8. When NOT to use `@fallback_to_any`.** If every expected type has an impl, turn
it OFF — you'll get clearer error messages for the 1% "surprise" type you didn't
anticipate, rather than silent mis-serialization.

---

## Resources

- [`Protocol` — hexdocs.pm](https://hexdocs.pm/elixir/Protocol.html)
- [Elixir source: Protocol.consolidate](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/protocol.ex)
- [Jason.Encoder fallback discussion](https://github.com/michalmuskala/jason#readme)
- [Inspect protocol — Any fallback in core](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/inspect.ex)
- [Erlang: `term_to_binary/1`](https://www.erlang.org/doc/man/erlang.html#term_to_binary-1)
- [Fred Hébert — "Erlang types vs protocols"](https://ferd.ca/)
