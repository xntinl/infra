# `Macro.escape/1` — Quoted Values as Runtime Literals

**Project**: `escape_quoted` — pass complex data structures (maps with regex, structs, nested tuples) from compile time into generated code using `Macro.escape/1`, and learn why naive interpolation breaks.

**Difficulty**: ★★★★☆
**Estimated time**: 3–4 hours

---

## Project context

You wrote a macro that reads a YAML config file at compile time and bakes the parsed
map into the generated code. When you tried:

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

```
escape_quoted/
├── lib/
│   └── escape_quoted/
│       ├── config_loader.ex       # load_config/1 macro
│       ├── compile_catalog.ex     # embeds a list of structs
│       └── regex_map.ex           # shows why raw unquote fails
├── test/
│   └── escape_quoted_test.exs
└── mix.exs
```

---

## Core concepts

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

## Implementation

### Step 1: `lib/escape_quoted/config_loader.ex`

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

### Step 2: `lib/escape_quoted/compile_catalog.ex`

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

### Step 3: `lib/escape_quoted/regex_map.ex` — showing what fails

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

```elixir
defmodule EscapeQuoted.Sample.Catalog do
  use EscapeQuoted.CompileCatalog
end

defmodule EscapeQuoted.Sample.Regex do
  require EscapeQuoted.RegexMap
  EscapeQuoted.RegexMap.good_macro()
end
```

### Step 5: Tests

```elixir
defmodule EscapeQuotedTest do
  use ExUnit.Case, async: true

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

## Resources

- [`Macro.escape/1` — hexdocs.pm](https://hexdocs.pm/elixir/Macro.html#escape/1)
- [`Macro.escape/2` with `:unquote`](https://hexdocs.pm/elixir/Macro.html#escape/2)
- [`@external_resource` docs](https://hexdocs.pm/elixir/Module.html#module-external_resource)
- [Ecto Schema — compile-time field baking](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex)
- [*Metaprogramming Elixir* — ch. 5](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/)
- [`:persistent_term` docs](https://www.erlang.org/doc/man/persistent_term.html) — runtime-loaded alternative
