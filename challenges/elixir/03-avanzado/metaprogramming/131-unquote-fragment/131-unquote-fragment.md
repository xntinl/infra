# Unquote Fragments and `unquote_splicing`

**Project**: `unquote_fragment` — generate dozens of specialized functions at compile time using `unquote_splicing` and `bind_quoted` fragments, the same technique used by Ecto's `schema` and Jason's encoder.

---

## Project context

Your billing service exposes 20 currency-related helpers: `to_usd/1`, `to_eur/1`,
`to_gbp/1`, etc. Each is nearly identical — convert a `{amount, from_currency}` tuple
into the target currency using a fixed exchange rate. Writing 20 copies is painful and
inconsistent. Using a single runtime function that pattern-matches on the atom works,
but loses compile-time dispatch and introduces a branch on every call.

The right tool is a compile-time loop with `unquote_splicing`: declare a list of
currencies in a module attribute, then emit one `def to_<code>/1` per element. This
is exactly how Phoenix emits `get/4`, `post/4`, etc. handlers per route, and how Ecto
emits accessors per schema field.

```
unquote_fragment/
├── lib/
│   └── unquote_fragment/
│       ├── rates.ex              # source of truth for currencies
│       ├── converter.ex          # compile-time-generated converters
│       └── registry.ex           # lists generated functions at runtime
├── test/
│   └── converter_test.exs
└── mix.exs
```

---

## Why compile-time generation and not runtime dispatch

**Runtime dispatch** (one `convert/2` with a `case` or map lookup) is simpler
to read, change at runtime, and debug. It pays one map lookup and one branch
per call.

**Compile-time generation** emits one `def to_<code>/1` per row, each with the
rate baked in as a numeric literal. The BEAM compiles these into a jump table:
no lookup, no branch on currency. The cost: recompilation required to add a
currency, larger BEAM file, and surprising stacktraces if you never read the
generated AST.

The rule of thumb: generate when the list is closed at compile time and hot on
the call path. Use runtime dispatch when the list changes, is loaded from a
DB, or performance is not the bottleneck.

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
### 1. `unquote` vs `unquote_splicing`

- `unquote(x)` interpolates a single AST fragment
- `unquote_splicing(xs)` interpolates a list of ASTs in place, without the list wrapper

```
quote do
  [1, unquote_splicing([2, 3]), 4]
end
# ==> [1, 2, 3, 4]
```

Using plain `unquote([2, 3])` would produce `[1, [2, 3], 4]`.

### 2. Fragment generation with a `for` comprehension

The canonical pattern for emitting N definitions:

```
for {name, opts} <- @fields do
  def get(unquote(name)), do: unquote(opts[:default])
end
```

Each iteration emits a fresh `def`. Because they all live in the same module body, they
become alternative clauses the compiler orders and optimizes into a jump table.

### 3. `bind_quoted: [...]`

`quote bind_quoted: [code: code, rate: rate]` freezes `code` and `rate` so they cannot
escape unquote boundaries accidentally. Without `bind_quoted`, `unquote` refers to the
outer variable at each use; with `bind_quoted`, the values are injected once.

### 4. Compile-time vs runtime choice

Generated functions use `def` — they compile to BEAM function clauses. A branching
runtime version (`convert(:usd, ...)` with a `case`) is roughly 2× slower on short
work because of the atom comparison and jump chain; the compiled version maps the
atom directly to code through the BEAM's apply dispatch.

### 5. Listing generated functions

`Module.definitions_in(mod, :def)` returns `[{name, arity}, ...]` — useful at
`@before_compile` for emitting a companion `__functions__/0` that docs can enumerate.

---

## Design decisions

**Option A — single `convert/2` with a runtime map**
- Pros: trivial to add/remove currencies, small BEAM file, no metaprogramming to explain.
- Cons: one `Map.fetch!/2` plus one branch per call; no compile-time guarantees about unknown currencies until the call fires.

**Option B — `unquote_splicing` fragment generation** (chosen)
- Pros: each generated `to_<code>/1` is a pure numeric transform with the rate inlined; compile-time errors for malformed rate tables; matches how Ecto and Phoenix emit their per-field/per-route handlers.
- Cons: adding a currency requires recompile; generated AST is harder to debug without `Macro.to_string/1`.

→ Chose **B** because the currency table is closed and the converter sits on a hot path; the recompile cost is acceptable and the style mirrors canonical Elixir libraries the reader will meet later.

---

## Implementation

### Step 1: `lib/unquote_fragment/rates.ex`

**Objective**: Declare @rates attribute immutable so compile-time loops enumerate and inline exchange rates idempotently.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule UnquoteFragment.Rates do
  @moduledoc "Source of truth for supported currencies and their USD rates."

  @rates [
    {:usd, 1.00},
    {:eur, 1.08},
    {:gbp, 1.27},
    {:jpy, 0.0065},
    {:brl, 0.20},
    {:ars, 0.0011},
    {:mxn, 0.058},
    {:cad, 0.73}
  ]

  @spec all() :: [{atom(), float()}]
  def all, do: @rates

  @spec codes() :: [atom()]
  def codes, do: Enum.map(@rates, &elem(&1, 0))
end
```

### Step 2: `lib/unquote_fragment/converter.ex`

**Objective**: Use unquote_splicing in for-loop to emit to_usd/to_eur/etc as BEAM jump-table entries with inlined rates.

```elixir
defmodule UnquoteFragment.Converter do
  @moduledoc """
  Emits one `to_<code>/1` function per currency using unquote_splicing inside
  a for-comprehension. Each generated function is a pure numeric transform.
  """

  @rates UnquoteFragment.Rates.all()

  for {code, rate} <- @rates do
    fun_name = String.to_atom("to_#{code}")

    @doc """
    Converts `{amount, from_currency}` into #{code |> Atom.to_string() |> String.upcase()}.

    Generated at compile time from the table in `UnquoteFragment.Rates`.
    """
    @spec unquote(fun_name)({number(), atom()}) :: float()
    def unquote(fun_name)({amount, from}) when is_number(amount) and is_atom(from) do
      usd = amount * unquote(Macro.escape(rate_lookup(@rates, :usd_from, from)))
      usd / unquote(rate)
    end
  end

  @doc "Returns all generated function names with their arities."
  @spec generated_functions() :: [{atom(), arity()}]
  def generated_functions do
    :functions
    |> __MODULE__.module_info()
    |> Enum.filter(fn {name, _} -> to_string(name) =~ ~r/^to_/ end)
  end

  # Private helper used at compile time only — selects the rate for a given from-currency.
  defp rate_lookup(rates, :usd_from, code) do
    case Enum.find(rates, fn {c, _} -> c == code end) do
      {^code, rate} -> rate
      nil -> raise "unknown currency #{inspect(code)}"
    end
  end
end
```

Note: the `rate_lookup/3` call lives inside `unquote(...)`, meaning it runs at compile
time. By the time the generated code loads, every `to_xxx/1` has a numeric literal for
its source rate — zero runtime dictionary lookup.

### Step 3: A runtime-dispatch variant for comparison

**Objective**: Implement runtime Map.fetch-based convert/2 to baseline the map-lookup overhead against generated dispatch.

```elixir
defmodule UnquoteFragment.RuntimeConverter do
  @moduledoc "Traditional runtime-branching version — kept for benchmarking."

  @rates Map.new(UnquoteFragment.Rates.all())

  @spec convert({number(), atom()}, atom()) :: float()
  def convert({amount, from}, to) when is_number(amount) do
    from_rate = Map.fetch!(@rates, from)
    to_rate = Map.fetch!(@rates, to)
    amount * from_rate / to_rate
  end
end
```

### Step 4: `lib/unquote_fragment/registry.ex`

**Objective**: Query module_info/1 to introspect and list generated to_*/1 functions for doc generation.

```elixir
defmodule UnquoteFragment.Registry do
  @moduledoc "Demonstrates introspecting the generated functions at runtime."

  @spec list() :: [{atom(), arity()}]
  def list, do: UnquoteFragment.Converter.generated_functions()
end
```

### Step 5: Tests

**Objective**: Assert generated functions exist for all currencies, round-trip correctly, and match runtime variant numerically.

```elixir
defmodule UnquoteFragment.ConverterTest do
  use ExUnit.Case, async: true

  alias UnquoteFragment.Converter
  alias UnquoteFragment.RuntimeConverter

  describe "generated converters" do
    test "to_usd of USD is identity" do
      assert_in_delta Converter.to_usd({100, :usd}), 100.0, 0.001
    end

    test "to_eur converts USD correctly" do
      # 100 USD * 1.00 / 1.08 ≈ 92.59 EUR
      assert_in_delta Converter.to_eur({100, :usd}), 100 / 1.08, 0.01
    end

    test "round-trip identity (within rounding)" do
      {amount, :usd} = {1_000, :usd}
      back = amount |> (&Converter.to_eur({&1, :usd})).() |> (&Converter.to_usd({&1, :eur})).()
      assert_in_delta back, amount, 0.5
    end

    test "all expected functions exist" do
      codes = UnquoteFragment.Rates.codes()
      generated = Converter.generated_functions() |> Map.new()

      for code <- codes do
        fun = String.to_atom("to_#{code}")
        assert Map.has_key?(generated, fun), "expected #{fun}/1 to exist"
      end
    end
  end

  describe "runtime variant parity" do
    test "compiled and runtime produce the same result" do
      assert_in_delta Converter.to_eur({250, :brl}),
                      RuntimeConverter.convert({250, :brl}, :eur),
                      0.001
    end
  end
end
```

### Why this works

The `for` comprehension runs inside the module body at compile time, so each
iteration emits a fresh `def` clause into the same module. The rate is
captured via `unquote(rate)`, which splices a literal float into the AST — by
the time the module loads, every function has its constant baked in. The BEAM
compiler turns the N clauses into a single jump table keyed by function name,
which is why dispatch is effectively free.

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

**1. Compile time scales with the list.** Each iteration emits a new function. For
~50 entries this is cheap; for 5000 (imagine a stock-ticker-per-symbol), compile time
balloons and the BEAM's optimizations become less effective. At that scale, switch
to runtime dispatch + a map.

**2. `unquote` inside a `for` needs care.** Without the surrounding `quote do ... end`,
`unquote` raises. The comprehension lives inside a module body, not a macro call, so
it expands at compile time directly — no explicit `quote` required.

**3. Debug with `Macro.to_string/1`.** When generated code misbehaves, wrap the body in
`quote do ... end` once and `IO.puts(Macro.to_string(...))` at compile time to see the
emitted source.

**4. `@spec unquote(name)` syntax is load-bearing.** `@spec to_usd(...)` with a literal
name works, but when the name is dynamic you must use `@spec unquote(name)(...) :: ...`
and the function signature must match exactly.

**5. Documentation overrides.** Each generated function can have its own `@doc`, but
those `@doc` strings are interpolated — keep them identical in structure so
hexdocs renders consistently.

**6. `bind_quoted` vs bare `unquote`.** With `bind_quoted`, variables are captured
once and injected as literals. Without it, Elixir re-looks up the variable at each
`unquote` site. Prefer `bind_quoted` for clarity unless you are generating distinct
AST fragments per site.

**7. When NOT to use this.** If the list will change at runtime (e.g. currencies loaded
from a DB on startup), generation is useless — the code is frozen at compile time.
Use a runtime map + `Map.fetch!/2`.

---

## Benchmark

```elixir
# bench/generated_vs_runtime.exs
alias UnquoteFragment.{Converter, RuntimeConverter}

Benchee.run(%{
  "generated to_eur/1" => fn -> Converter.to_eur({100, :usd}) end,
  "runtime convert/2"  => fn -> RuntimeConverter.convert({100, :usd}, :eur) end
})
```

Expect the generated version to be ~1.5–3× faster — the map lookup disappears.

Target: generated `to_eur/1` under 50 ns per call on modern hardware; runtime
`convert/2` around 100–150 ns. Gap widens as the currency list grows.

---

## Reflection

- If the currency table grew to 5,000 symbols (one per ticker), would you still
  generate one function per symbol, or switch to a runtime map? At what N does
  the compile-time cost outweigh the dispatch win?
- You are told rates must refresh every 5 minutes from an external feed. How
  does that single requirement change the design, and which parts of the
  current approach survive?

---


## Executable Example

```elixir
defmodule UnquoteFragment.Converter do
  @moduledoc """
  Emits one `to_<code>/1` function per currency using unquote_splicing inside
  a for-comprehension. Each generated function is a pure numeric transform.
  """

  @rates UnquoteFragment.Rates.all()

  for {code, rate} <- @rates do
    fun_name = String.to_atom("to_#{code}")

    @doc """
    Converts `{amount, from_currency}` into #{code |> Atom.to_string() |> String.upcase()}.

    Generated at compile time from the table in `UnquoteFragment.Rates`.
    """
    @spec unquote(fun_name)({number(), atom()}) :: float()
    def unquote(fun_name)({amount, from}) when is_number(amount) and is_atom(from) do
      usd = amount * unquote(Macro.escape(rate_lookup(@rates, :usd_from, from)))
      usd / unquote(rate)
    end
  end

  @doc "Returns all generated function names with their arities."
  @spec generated_functions() :: [{atom(), arity()}]
  def generated_functions do
    :functions
    |> __MODULE__.module_info()
    |> Enum.filter(fn {name, _} -> to_string(name) =~ ~r/^to_/ end)
  end

  # Private helper used at compile time only — selects the rate for a given from-currency.
  defp rate_lookup(rates, :usd_from, code) do
    case Enum.find(rates, fn {c, _} -> c == code end) do
      {^code, rate} -> rate
      nil -> raise "unknown currency #{inspect(code)}"
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate unquote_splicing for code generation
      fields = [{:name, :string}, {:age, :integer}, {:email, :string}]

      # Generate getters using unquote_splicing
      defmodule Record do
        Enum.each(fields, fn {field, _type} ->
          def unquote(:"get_#{field}")(map) do
            Map.get(map, unquote(field))
          end
        end)
      end

      # Test generated functions
      data = %{name: "Alice", age: 30, email: "alice@example.com"}

      name = Record.get_name(data)
      age = Record.get_age(data)
      email = Record.get_email(data)

      IO.puts("✓ Generated getter: get_name → #{name}")
      IO.puts("✓ Generated getter: get_age → #{age}")
      IO.puts("✓ Generated getter: get_email → #{email}")

      assert name == "Alice", "Name getter works"
      assert age == 30, "Age getter works"

      IO.puts("✓ Unquote fragments: compile-time code generation working")
  end
end

Main.main()
```
