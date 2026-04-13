# `Macro.escape/1` — Quoted Values as Runtime Literals

**Project**: `escape_quoted` — pass complex data structures (maps with regex, structs, nested tuples) from compile time into generated code using `Macro.escape/1`, and learn why naive interpolation breaks.

---

## The business problem

You wrote a macro that reads a YAML config file at compile time and bakes the parsed
map into the generated code. When you tried:

### `mix.exs`
```elixir
defmodule QuotedAsLiteral.MixProject do
  use Mix.Project

  def project do
    [
      app: :quoted_as_literal,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [# No external dependencies — pure Elixir]
  end
end
```
```elixir
defmacro load_config(path) do
  config = YAML.parse_file!(path)
  quote do
    def config, do: unquote(config)
  end
end
```
…compilation exploded with "invalid quoted expression". The map contained `Regex`
structs and tuples with atoms, and Elixir's AST does not treat arbitrary terms as
valid quoted forms. The fix: `Macro.escape/1`, which wraps any term into a quoted
expression that evaluates back to the original term at runtime.

This matters for every macro that wants to embed non-trivial data: Ecto embedded
schemas, Phoenix route tables, compile-time-loaded JSON catalogs.

## Project structure

```
escape_quoted/
├── lib/
│   └── escape_quoted/
│       ├── config_loader.ex       # load_config/1 macro
│       ├── compile_catalog.ex     # embeds a list of structs
│       └── regex_map.ex           # shows why raw unquote fails
├── test/
│   └── escape_quoted_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why quoted literals and not runtime terms

A runtime term is computed on every call and opaque to the compiler. A quoted literal is a constant in the BEAM chunk, meaning the VM can share it across callers and the compiler can fold dependent expressions.

---

## Design decisions

**Option A — store data as Elixir terms and encode on demand**
- Pros: flexibility; editable at runtime.
- Cons: repeated work on the hot path; lose compile-time validation.

**Option B — quote the term once and splice as a literal** (chosen)
- Pros: zero runtime cost; compiler can constant-fold.
- Cons: recompile to change; must be `Macro.escape`-safe.

→ Chose **B** because the term is fixed at build time and reads hot.

---

## Implementation

### `lib/escape_quoted.ex`

```elixir
defmodule EscapeQuoted do
  @moduledoc """
  `Macro.escape/1` — Quoted Values as Runtime Literals.

  A runtime term is computed on every call and opaque to the compiler. A quoted literal is a constant in the BEAM chunk, meaning the VM can share it across callers and the compiler....
  """
end
```
### `lib/escape_quoted/config_loader.ex`

**Objective**: Read JSON file at compile time via @external_resource and bake parsed map via Macro.escape into config/0.

```elixir
defmodule EscapeQuoted.ConfigLoader do
  @moduledoc """
  Macro that reads a JSON file at compile time and bakes it into `config/0`.
  Because JSON parses into maps (with any mix of types), we cannot interpolate
  the result directly — we must `Macro.escape/1`.
  """

  defmacro load_config(path) do
    abs_path = Path.expand(path, __CALLER__.file |> Path.dirname())
    content = File.read!(abs_path)
    term = :json.decode(content)

    quote do
      @external_resource unquote(abs_path)

      @spec config() :: map()
      def config, do: unquote(Macro.escape(term))
    end
  end
end
```
### `lib/escape_quoted/compile_catalog.ex`

**Objective**: Embed Product structs with regex patterns via Macro.escape so complex nested terms round-trip correctly.

```elixir
defmodule EscapeQuoted.CompileCatalog do
  @moduledoc """
  Embeds a table of product records at compile time. Records contain regex
  validators and structured tuples — must be escaped.
  """

  defmodule Product do
    defstruct [:sku, :name, :sku_pattern, :tags]
  end

  @catalog [
    %Product{
      sku: "SKU-001",
      name: "Widget",
      sku_pattern: ~r/^SKU-\d{3}$/,
      tags: [{:color, :red}, {:size, :m}]
    },
    %Product{
      sku: "SKU-002",
      name: "Gadget",
      sku_pattern: ~r/^SKU-\d{3}$/,
      tags: [{:color, :blue}, {:size, :l}]
    }
  ]

  defmacro __using__(_) do
    catalog = @catalog

    quote do
      @spec catalog() :: [unquote(__MODULE__).Product.t()]
      def catalog, do: unquote(Macro.escape(catalog))

      @spec find(String.t()) :: {:ok, unquote(__MODULE__).Product.t()} | :error
      def find(sku) do
        catalog()
        |> Enum.find(&(&1.sku == sku))
        |> case do
          nil -> :error
          p -> {:ok, p}
        end
      end
    end
  end
end
```
### `lib/escape_quoted/regex_map.ex`

**Objective**: Comment out bad_macro/0 to document failure mode; show good_macro using escape so contrast is studied.

```elixir
defmodule EscapeQuoted.RegexMap do
  @moduledoc """
  Demonstrates the failure mode. The `bad_macro/0` below will NOT compile
  if the map contains a regex; `good_macro/0` uses escape and compiles fine.
  """

  # This function is shown commented-out because it fails to compile.
  #
  # defmacro bad_macro do
  #   term = %{re: ~r/abc/}
  #   quote do
  #     def data, do: unquote(term)  # -> compile error
  #   end
  # end

  defmacro good_macro do
    term = %{re: ~r/abc/}
    escaped = Macro.escape(term)

    quote do
      def data, do: unquote(escaped)
    end
  end
end
```
### Step 4: Sample consumers

**Objective**: Use macros in client modules to confirm Macro.escape round-trip preserves struct and regex identity at runtime.

```elixir
defmodule EscapeQuoted.Sample.Catalog do
  use EscapeQuoted.CompileCatalog
end

defmodule EscapeQuoted.Sample.Regex do
  require EscapeQuoted.RegexMap
  EscapeQuoted.RegexMap.good_macro()
end
```
### `test/escape_quoted_test.exs`

**Objective**: Verify regexes still match and nested tuples survive escape so the literal truly equals the pre-escape value.

```elixir
defmodule EscapeQuotedTest do
  use ExUnit.Case, async: true
  doctest EscapeQuoted.Sample.Catalog

  alias EscapeQuoted.Sample.{Catalog, Regex}
  alias EscapeQuoted.CompileCatalog.Product

  describe "compile-embedded catalog" do
    test "catalog/0 returns the original structs" do
      products = Catalog.catalog()
      assert length(products) == 2
      assert %Product{sku: "SKU-001"} = hd(products)
    end

    test "regexes survive the escape round-trip" do
      %Product{sku_pattern: re} = hd(Catalog.catalog())
      assert Regex.match?(re, "SKU-123")
      refute Regex.match?(re, "BAD-ID")
    end

    test "nested tuples survive" do
      %Product{tags: tags} = hd(Catalog.catalog())
      assert {:color, :red} in tags
    end

    test "find/1 returns :ok or :error" do
      assert {:ok, %Product{name: "Widget"}} = Catalog.find("SKU-001")
      assert :error = Catalog.find("SKU-999")
    end
  end

  describe "regex map macro" do
    test "good_macro compiled correctly" do
      %{re: re} = Regex.data()
      assert Regex.match?(re, "abc")
    end
  end
end
```
### Why this works

Inside a macro, `Macro.escape/1` turns any Elixir term into AST that reconstructs the term when unquoted. Splicing that AST into the emitted code produces a literal the compiler stores once per module and references by pointer.

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

**1. Escape is deep.** `Macro.escape/1` walks the entire term recursively. For large
configs (MB of JSON) this is slow at compile time and inflates BEAM files.

**2. Functions cannot be escaped.** `Macro.escape(&Enum.map/2)` raises. If you need
to reference a function, emit `&Module.fun/2` syntax in the quoted output — not
an escaped closure.

**3. PIDs, references, ports cannot be escaped.** They have no stable serialized
form. Any term you embed at compile time must be purely data.

**4. Protocol implementations against escaped structs.** At compile time, some
protocol impls (e.g. `Enumerable`) may not yet be consolidated — consolidate
before the macro runs (problem rare in library code, common in umbrella apps).

**5. `@external_resource` is mandatory for file-backed macros.** Without it, Mix
does not know to rebuild when the file changes. Result: stale config in the
compiled beam.

**6. `:unquote` option corner cases.** `Macro.escape(term, unquote: true)` treats
any `{:unquote, _, [x]}` in the term as an escape marker. If your data happens to
contain such tuples (rare but possible), you will get silent double-expansion.

**7. Struct field additions.** If you escape `%Foo{a: 1}` in version 1 and later add
a `:b` field with a default, the recompiled struct gets `:b`, but pre-compiled
beams retain the old shape. Recompile the dependent modules.

**8. When NOT to use this.** If the data changes at runtime, or you need
environment-specific values, load it at runtime instead: `:persistent_term`,
ETS, or `Application.get_env/2`.

---

## Benchmark

```elixir
# bench/escape_bench.exs
big = for i <- 1..1_000, into: %{}, do: {i, {:entry, i, ~r/pattern/}}

Benchee.run(%{
  "escape 1k-entry map" => fn -> Macro.escape(big) end
})
```
Expect ~5–20 ms for a 1k-entry map. Compile-time only.

---

## Reflection

- If the data changes twice a day via a CMS, does quoting still win? At what change frequency does the recompile cost outweigh the runtime savings?
- `Macro.escape/1` fails on functions and PIDs. Which shapes of data force you away from this pattern, and what is the fallback?

---

### `script/main.exs`
```elixir
defmodule EscapeQuotedTest do
  use ExUnit.Case, async: true
  doctest EscapeQuoted.Sample.Catalog

  alias EscapeQuoted.Sample.{Catalog, Regex}
  alias EscapeQuoted.CompileCatalog.Product

  describe "compile-embedded catalog" do
    test "catalog/0 returns the original structs" do
      products = Catalog.catalog()
      assert length(products) == 2
      assert %Product{sku: "SKU-001"} = hd(products)
    end

    test "regexes survive the escape round-trip" do
      %Product{sku_pattern: re} = hd(Catalog.catalog())
      assert Regex.match?(re, "SKU-123")
      refute Regex.match?(re, "BAD-ID")
    end

    test "nested tuples survive" do
      %Product{tags: tags} = hd(Catalog.catalog())
      assert {:color, :red} in tags
    end

    test "find/1 returns :ok or :error" do
      assert {:ok, %Product{name: "Widget"}} = Catalog.find("SKU-001")
      assert :error = Catalog.find("SKU-999")
    end
  end

  describe "regex map macro" do
    test "good_macro compiled correctly" do
      %{re: re} = Regex.data()
      assert Regex.match?(re, "abc")
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate Macro.escape for embedding complex data at compile time
      data = %{
        name: "config",
        pattern: ~r/\d+/,
        nested: %{x: 1, y: 2}
      }

      # Escape the data so it can be used in generated code
      escaped = Macro.escape(data)

      IO.puts("✓ Original data:")
      IO.inspect(data, label: "  ")

      IO.puts("✓ Escaped (as quoted expression):")
      IO.inspect(escaped, label: "  ")

      # In real scenario: use escaped data in quote block
      # quote do: unquote(escaped)

      assert is_map(data), "Original is map"
      assert is_map(escaped) or is_tuple(escaped), "Escaped representation"

      IO.puts("✓ Macro.escape: complex data serialization working")
  end
end

Main.main()
```
---

## Why `Macro.escape/1` — Quoted Values as Runtime Literals matters

Mastering **`Macro.escape/1` — Quoted Values as Runtime Literals** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts

### 1. What counts as a "valid quoted expression"

Legal as-is without escaping:

- Atoms, integers, floats
- Bitstrings / binaries
- Empty list `[]`, `nil`, `true`, `false`
- 2-tuples of literals
- Lists of literals

Everything else (maps, 3-tuples, structs, regexes) must be either escaped or expressed
explicitly as its AST form (`{:%{}, [], [...]}` etc.).

### 2. `Macro.escape/1`

Produces a quoted expression that, when evaluated, yields the original term.

```
Macro.escape(%{a: 1, b: {:tuple, :inside}})
#=> {:%{}, [], [a: 1, b: {:{}, [], [:tuple, :inside]}]}
```

Drop that into `unquote(...)` and the generated code builds the same map at runtime.

### 3. `Macro.escape/2` and `:unquote` option

`Macro.escape(term, unquote: true)` treats any `{:unquote, _, [inner]}` nodes inside
the term as escape hatches — allowing partial escape with live injection:

```
Macro.escape({:a, unquote(my_var)}, unquote: true)
```

useful when building templates.

### 4. Alternatives: `external_resource` + `@`

For reading a file at compile time, `@external_resource` makes Mix re-compile when
the file changes, and `@config File.read!(path) |> parse` stores the parsed term
as a module attribute — retrievable via `@config` or `Module.get_attribute/2`.
Attributes go through `Macro.escape/1` under the hood.

### 5. Structs need their module loaded

`%SomeStruct{}` is a valid term, but `Macro.escape(%SomeStruct{})` produces
`{:%, _, [SomeStruct, {:%{}, _, [...]}]}` — that needs `SomeStruct` to be
compiled before the macro runs. Watch for circular deps.

---
