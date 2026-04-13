# `@fallback_to_any true` — Deep Dive and Performance Cost

**Project**: `fallback_any` — build a protocol both with and without `@fallback_to_any`, benchmark dispatch in each mode with consolidated vs unconsolidated compilation, and understand where the performance cost comes from.

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

## Why fallback_to_any and not exhaustive impls

Requiring `defimpl` for every type means every new struct in every consumer is a potential production crash. `@fallback_to_any` routes unknown types to a single `defimpl Protocol, for: Any` implementation, which can raise cleanly, log, or return a typed error.

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Metaprogramming-specific insight:**
Code generation is powerful and dangerous. Every macro you write is a place where intent is hidden. Use macros sparingly, only when they eliminate genuine boilerplate. If your macro is more than 10 lines, you probably need a function or data structure instead. Future maintainers will thank you.
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

## Design decisions

**Option A — require `defimpl` for every type in the system**
- Pros: exhaustiveness; no surprise silent fallback; clearest stacktraces.
- Cons: impossible for open-world types; breaks for any user-defined struct not known at library time.

**Option B — `@fallback_to_any` with a sane default** (chosen)
- Pros: works on types the library has never seen; avoids `Protocol.UndefinedError` cascades.
- Cons: the default may be wrong for specific types and fail late; harder to notice missing impls.

→ Chose **B** because the library ships to callers with structs we cannot enumerate; a safe default plus opt-in consolidation gives the ergonomics without giving up correctness.

---

## Implementation

### Step 1: `lib/fallback_any/with_fallback.ex`

**Objective**: Define protocol with @fallback_to_any true and defimpl for: Any so unknown types route to term_to_binary safely.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

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

**Objective**: Mirror protocol without @fallback_to_any so baseline raises Protocol.UndefinedError on unknown types.

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

**Objective**: Drive both protocols on known/unknown inputs so benchmarks isolate fallback dispatch cost from specific-impl cost.

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

**Objective**: Assert fallback routes unknown structs, no-fallback raises UndefinedError, impl_for returns appropriate values, consolidation confirmed.

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

**Objective**: Quantify dispatch overhead so the Any fallback's real cost on hot and cold paths is not assumed.

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

### Why this works

When the compiler emits dispatch for a protocol, `@fallback_to_any` adds a final clause that matches `Any`. At runtime, when no matching impl exists for a given type, the VM falls through to the `Any` clause. Protocol consolidation still works and elides the dispatch in release mode.

---

## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---


## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

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
requires re-consolidation. See `Protocol.consolidate/2`.

**8. When NOT to use `@fallback_to_any`.** If every expected type has an impl, turn
it OFF — you'll get clearer error messages for the 1% "surprise" type you didn't
anticipate, rather than silent mis-serialization.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: fallback path within 10 ns of direct impl; no measurable overhead after consolidation.

---

## Reflection

- A library you depend on uses `@fallback_to_any` and silently returns `nil` for your struct. How would you detect this in CI without reading every dependency?
- If you had to design a protocol where silent fallback is dangerous (e.g. serialization), would you enable `@fallback_to_any` at all? What would you do instead?

---


## Executable Example

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

defmodule Main do
  def main do
      # Demonstrate @fallback_to_any performance implications
      defprotocol WithFallback do
        @fallback_to_any true
        def process(data)
      end

      defimpl WithFallback, for: String do
        def process(s), do: {:string, s}
      end

      # Fallback implementation for any type
      defimpl WithFallback, for: Any do
        def process(data), do: {:any, data}
      end

      # Benchmark dispatch
      t0 = System.monotonic_time(:microsecond)
      result1 = WithFallback.process("hello")
      t1 = System.monotonic_time(:microsecond)

      result2 = WithFallback.process(42)
      t2 = System.monotonic_time(:microsecond)

      string_time = t1 - t0
      any_time = t2 - t1

      IO.puts("✓ String dispatch: #{string_time} µs → #{inspect(result1)}")
      IO.puts("✓ Any dispatch: #{any_time} µs → #{inspect(result2)}")
      IO.puts("✓ Cost ratio: #{Float.round(any_time / max(string_time, 1), 2)}")

      assert match?({:string, _}, result1), "String impl works"
      assert match?({:any, _}, result2), "Any fallback works"

      IO.puts("✓ @fallback_to_any: performance comparison working")
  end
end

Main.main()
```
