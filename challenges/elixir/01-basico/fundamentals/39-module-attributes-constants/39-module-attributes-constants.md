# Module Attributes and Constants: A Versioned Schema Registry

**Project**: `versioned_schema_registry` — a schema registry module using `@version` and `@supported` attributes as compile-time constants

**Difficulty**: ★★☆☆☆
**Estimated time**: 2 hours

---

## Project structure

```
versioned_schema_registry/
├── lib/
│   └── versioned_schema_registry.ex
├── test/
│   └── versioned_schema_registry_test.exs
└── mix.exs
```

---

## Core concepts

Module attributes in Elixir play three roles:

1. **Compile-time constants** — `@version "1.0.0"`. The value is inlined at
   every use site. Changing `@version` requires recompilation.
2. **Documentation** — `@moduledoc`, `@doc`, `@spec` are special attributes
   consumed by the compiler and ExDoc. They appear in generated documentation
   and power Dialyzer type checking.
3. **Metadata** — `@behaviour`, `@derive`, `@impl` drive language features.

Coming from Java, attributes replace `static final` constants plus Javadoc
annotations. Unlike `const` in JavaScript, module attributes are NOT runtime
variables — they are substituted at compile time. This means:

- `@list [1, 2, 3]` is cheap to read but each reference rebuilds the list.
  For large data, prefer a function that returns the value.
- Attributes cannot be modified at runtime. They freeze when the module compiles.

`@spec` introduces static type information used by Dialyzer. It does NOT
enforce types at runtime — that's what guards and pattern matching are for.

---

## The business problem

A schema registry stores the current schema version and the list of supported
prior versions for backward compatibility. The registry must:

1. Expose the current version as a compile-time constant.
2. Expose supported legacy versions for migration logic.
3. Validate incoming schema requests against supported versions.
4. Generate good documentation for downstream teams.

---

## Implementation

### `lib/versioned_schema_registry.ex`

```elixir
defmodule VersionedSchemaRegistry do
  @moduledoc """
  Schema registry with compile-time version metadata.

  The current version and the list of supported versions are baked into
  the compiled module. Bumping either attribute requires a recompile,
  which is exactly the contract we want for a registry.

  ## Usage

      VersionedSchemaRegistry.current_version()
      #=> "3.2.0"

      VersionedSchemaRegistry.supports?("2.0.0")
      #=> true
  """

  # Compile-time constant. Prefixed with @ — inlined wherever referenced below.
  @version "3.2.0"

  # List attribute: supported legacy versions. Order does not matter here;
  # we convert to a MapSet at compile time for O(1) lookup.
  @supported ["1.0.0", "2.0.0", "2.1.0", "3.0.0", "3.1.0", "3.2.0"]

  # Derived attribute: computed at compile time, not at each call.
  # This is the pattern for expensive constants.
  @supported_set MapSet.new(@supported)

  # External-facing metadata embedded in docs and response payloads.
  @release_channel :stable
  @minimum_client "1.4.0"

  @doc """
  Returns the current schema version.
  """
  @spec current_version() :: String.t()
  def current_version, do: @version

  @doc """
  Returns all supported versions, newest first.
  """
  @spec supported_versions() :: [String.t()]
  def supported_versions do
    # Sorted descending by semver-style string comparison. For true semver
    # sorting use the `Version` module; this is enough for the exercise.
    Enum.sort(@supported, :desc)
  end

  @doc """
  Returns `true` if the given version is supported.

  Uses the compile-time MapSet for O(1) lookup — do not rebuild it at runtime.
  """
  @spec supports?(String.t()) :: boolean()
  def supports?(version) when is_binary(version) do
    MapSet.member?(@supported_set, version)
  end

  @doc """
  Validates a client request against the registry.

  Returns `{:ok, metadata}` when the version is supported, or
  `{:error, reason}` otherwise.
  """
  @spec validate(String.t()) ::
          {:ok, %{version: String.t(), current: String.t(), channel: atom()}}
          | {:error, :unsupported_version}
  def validate(version) when is_binary(version) do
    if supports?(version) do
      {:ok,
       %{
         version: version,
         current: @version,
         channel: @release_channel,
         minimum_client: @minimum_client,
         upgrade_required: version != @version
       }}
    else
      {:error, :unsupported_version}
    end
  end

  @doc """
  Diff between a client's version and the current one.

  Demonstrates `@version` inlined in multiple places — one change in the
  attribute updates all of them.
  """
  @spec lag(String.t()) :: non_neg_integer()
  def lag(client_version) when is_binary(client_version) do
    # Parse "3.2.0" -> [3, 2, 0] and compute absolute distance in patches.
    [a1, a2, a3] = parse(@version)
    [b1, b2, b3] = parse(client_version)
    abs((a1 - b1) * 10_000 + (a2 - b2) * 100 + (a3 - b3))
  end

  defp parse(v), do: v |> String.split(".") |> Enum.map(&String.to_integer/1)
end
```

### `test/versioned_schema_registry_test.exs`

```elixir
defmodule VersionedSchemaRegistryTest do
  use ExUnit.Case, async: true

  alias VersionedSchemaRegistry, as: Registry

  describe "current_version/0" do
    test "returns compile-time baked version" do
      assert Registry.current_version() == "3.2.0"
    end
  end

  describe "supported_versions/0" do
    test "returns versions sorted descending" do
      versions = Registry.supported_versions()
      assert hd(versions) == "3.2.0"
      assert List.last(versions) == "1.0.0"
    end

    test "includes current version" do
      assert Registry.current_version() in Registry.supported_versions()
    end
  end

  describe "supports?/1" do
    test "returns true for current version" do
      assert Registry.supports?("3.2.0")
    end

    test "returns true for legacy versions" do
      assert Registry.supports?("1.0.0")
      assert Registry.supports?("2.1.0")
    end

    test "returns false for unknown versions" do
      refute Registry.supports?("99.0.0")
      refute Registry.supports?("0.0.1")
    end
  end

  describe "validate/1" do
    test "valid request returns full metadata" do
      assert {:ok, meta} = Registry.validate("2.1.0")
      assert meta.version == "2.1.0"
      assert meta.current == "3.2.0"
      assert meta.channel == :stable
      assert meta.upgrade_required == true
    end

    test "current version does not require upgrade" do
      assert {:ok, %{upgrade_required: false}} = Registry.validate("3.2.0")
    end

    test "unknown version returns error" do
      assert {:error, :unsupported_version} = Registry.validate("9.9.9")
    end
  end

  describe "lag/1" do
    test "zero lag when client is current" do
      assert Registry.lag("3.2.0") == 0
    end

    test "minor version lag" do
      assert Registry.lag("3.1.0") == 100
    end

    test "major version lag" do
      assert Registry.lag("2.0.0") == 10_200
    end
  end

  describe "documentation" do
    test "module has a @moduledoc" do
      {:docs_v1, _, _, _, %{"en" => doc}, _, _} = Code.fetch_docs(Registry)
      assert doc =~ "Schema registry"
    end
  end
end
```

### Run it

```bash
mix new versioned_schema_registry
cd versioned_schema_registry
mix test
mix docs  # if ex_doc is configured — @moduledoc renders the module page
```

---

## Trade-offs and production mistakes

**1. Large list attributes rebuild at every call**
```elixir
@huge_list Enum.to_list(1..1_000_000)
def contains?(x), do: x in @huge_list  # rebuilds every call!
```
Fix: compute a `MapSet` at compile time and reference that, or move data to
ETS/persistent_term for runtime lookup.

**2. Attributes are compile-time, not runtime**
Changing `@version "3.2.0"` in source and restarting the app without
recompiling does nothing. Most build tools handle this; hot-code reloading
may not.

**3. `@doc` must precede the function it documents**
Placing `@doc` between function clauses is a common bug — Elixir warns.

**4. `@spec` is not enforced at runtime**
`@spec foo(integer()) :: :ok` does not prevent callers from passing a string.
Run Dialyzer in CI to catch mismatches, or add guards.

**5. Attribute reads accumulate in Module.register_attribute lists**
For accumulating attributes (`@callbacks`), each read returns the full list so
far. For single-value attributes it's the last write. Read the docs of
`Module.register_attribute/3` if you build DSLs.

## When NOT to use module attributes

- For data that changes at runtime — use application config or state.
- For large binaries or deep structures — use `:persistent_term` or external
  files loaded at startup.
- When you need dependency injection — attributes freeze the value, making
  test doubles harder.

---

## Resources

- [Module attributes — Getting Started](https://elixir-lang.org/getting-started/module-attributes.html)
- [@moduledoc, @doc, @spec — HexDocs](https://hexdocs.pm/elixir/writing-documentation.html)
- [Typespecs and behaviours](https://hexdocs.pm/elixir/typespecs.html)
- [Module.register_attribute/3](https://hexdocs.pm/elixir/Module.html#register_attribute/3)
