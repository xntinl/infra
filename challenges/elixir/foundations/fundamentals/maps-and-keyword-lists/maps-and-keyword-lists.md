# Maps and Keyword Lists: Building a Configuration System

**Project**: `app_config` — a layered configuration system with deep merge, validation, and keyword list options

---

## Why Elixir has both maps and keyword lists

Coming from other languages, having two associative data structures seems redundant.
It is not. Maps and keyword lists serve fundamentally different purposes:

**Maps** (`%{key: value}`):
- Keys are unique
- Any type can be a key
- O(log n) lookup
- Pattern matchable
- Use for: data, records, configuration state

**Keyword lists** (`[key: value]`):
- Duplicate keys allowed
- Keys must be atoms
- O(n) lookup
- Preserves insertion order
- Use for: function options, DSL syntax, ordered parameters

The critical insight: keyword lists are the idiomatic way to pass options to functions
in Elixir. Every time you see `Repo.all(User, limit: 10, order_by: :name)`, those
options are a keyword list. Maps are for the data you process; keyword lists are for
how you configure that processing.

---

## The business problem

Build a configuration system that:

1. Defines defaults as a map
2. Loads file-based overrides (simulated as maps)
3. Applies environment variable overrides
4. Deep-merges all layers (nested maps merge recursively)
5. Validates the final configuration
6. Uses keyword lists for function options (like `:required`, `:transform`)

---

## Project structure

```
app_config/
├── lib/
│   └── app_config.ex
├── script/
│   └── main.exs
├── test/
│   └── app_config_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — layered deep-merge with maps for config values**
- Pros: Supports arbitrary key types, O(log n) access, pattern-matchable
- Cons: No ordering guarantees (irrelevant for config), slightly more ceremony than keyword lists

**Option B — flat Keyword list at every level** (chosen)
- Pros: Ordered, supports duplicate keys, natural for compile-time option passing
- Cons: Keys must be atoms, O(n) access, not suitable for large nested trees

→ Chose **A** because application config is a nested tree with heterogeneous key sources; maps handle that cleanly while keyword lists fit better at the API boundary. Keep keyword lists at the function-call boundary (options).

## Implementation

### `mix.exs`
```elixir
defmodule AppConfig.MixProject do
  use Mix.Project

  def project do
    [
      app: :app_config,
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
    []
  end
end
```
### `lib/app_config.ex`

```elixir
defmodule AppConfig do
  @moduledoc """
  Layered configuration system with deep merge and validation.

  Configuration flows through three layers:
  1. Defaults (compiled into the module)
  2. File overrides (loaded at startup)
  3. Environment variables (read at runtime)

  Each layer deep-merges into the previous, with later layers winning.
  """

  @defaults %{
    server: %{
      host: "localhost",
      port: 4000,
      pool_size: 10
    },
    database: %{
      host: "localhost",
      port: 5432,
      name: "app_dev",
      pool_size: 10,
      ssl: false
    },
    logging: %{
      level: :info,
      format: "json"
    }
  }

  @doc """
  Returns the default configuration.
  """
  @spec defaults() :: map()
  def defaults, do: @defaults

  @doc """
  Builds the final configuration by merging all layers.

  Options (keyword list):
    - `:file_config` — map of file-based overrides (default: %{})
    - `:env_config` — map of environment variable overrides (default: %{})
    - `:validate` — whether to validate the result (default: true)

  ## Examples

      iex> {:ok, config} = AppConfig.build(file_config: %{server: %{port: 8080}})
      iex> config.server.port
      8080

      iex> {:ok, config} = AppConfig.build(file_config: %{server: %{port: 8080}})
      iex> config.server.host
      "localhost"

  """
  @spec build(keyword()) :: {:ok, map()} | {:error, [String.t()]}
  def build(opts \\ []) do
    file_config = Keyword.get(opts, :file_config, %{})
    env_config = Keyword.get(opts, :env_config, %{})
    validate? = Keyword.get(opts, :validate, true)

    config =
      @defaults
      |> deep_merge(file_config)
      |> deep_merge(env_config)

    if validate? do
      case validate(config) do
        :ok -> {:ok, config}
        {:error, errors} -> {:error, errors}
      end
    else
      {:ok, config}
    end
  end

  @doc """
  Deep merges two maps recursively.

  When both values for a key are maps, they are merged recursively.
  Otherwise, the right value wins.

  ## Examples

      iex> AppConfig.deep_merge(%{a: %{b: 1, c: 2}}, %{a: %{c: 3, d: 4}})
      %{a: %{b: 1, c: 3, d: 4}}

      iex> AppConfig.deep_merge(%{a: 1}, %{a: 2})
      %{a: 2}

      iex> AppConfig.deep_merge(%{a: 1}, %{b: 2})
      %{a: 1, b: 2}

  """
  @spec deep_merge(map(), map()) :: map()
  def deep_merge(left, right) when is_map(left) and is_map(right) do
    Map.merge(left, right, fn _key, left_val, right_val ->
      if is_map(left_val) and is_map(right_val) do
        deep_merge(left_val, right_val)
      else
        right_val
      end
    end)
  end

  @doc """
  Fetches a nested value from the config using a key path.

  ## Examples

      iex> config = %{server: %{host: "localhost", port: 4000}}
      iex> AppConfig.get_in_config(config, [:server, :port])
      {:ok, 4000}

      iex> config = %{server: %{host: "localhost"}}
      iex> AppConfig.get_in_config(config, [:server, :missing])
      :error

  """
  @spec get_in_config(map(), [atom()]) :: {:ok, term()} | :error
  def get_in_config(config, keys) when is_map(config) and is_list(keys) do
    case get_in(config, keys) do
      nil -> :error
      value -> {:ok, value}
    end
  end

  @doc """
  Validates the configuration against rules.

  Returns :ok or {:error, list_of_messages}.

  ## Examples

      iex> config = %{server: %{port: 4000, host: "localhost", pool_size: 10}, database: %{port: 5432, host: "localhost", name: "db", pool_size: 10, ssl: false}, logging: %{level: :info, format: "json"}}
      iex> AppConfig.validate(config)
      :ok

  """
  @spec validate(map()) :: :ok | {:error, [String.t()]}
  def validate(config) do
    errors =
      []
      |> validate_port(config, [:server, :port])
      |> validate_port(config, [:database, :port])
      |> validate_non_empty_string(config, [:server, :host])
      |> validate_non_empty_string(config, [:database, :host])
      |> validate_non_empty_string(config, [:database, :name])
      |> validate_positive_integer(config, [:server, :pool_size])
      |> validate_positive_integer(config, [:database, :pool_size])
      |> validate_log_level(config)

    case errors do
      [] -> :ok
      errs -> {:error, Enum.reverse(errs)}
    end
  end

  # --- Private validation helpers ---

  @spec validate_port([String.t()], map(), [atom()]) :: [String.t()]
  defp validate_port(errors, config, path) do
    case get_in(config, path) do
      port when is_integer(port) and port > 0 and port <= 65535 -> errors
      port -> ["#{format_path(path)} must be a port (1-65535), got: #{inspect(port)}" | errors]
    end
  end

  @spec validate_non_empty_string([String.t()], map(), [atom()]) :: [String.t()]
  defp validate_non_empty_string(errors, config, path) do
    case get_in(config, path) do
      s when is_binary(s) and byte_size(s) > 0 -> errors
      val -> ["#{format_path(path)} must be a non-empty string, got: #{inspect(val)}" | errors]
    end
  end

  @spec validate_positive_integer([String.t()], map(), [atom()]) :: [String.t()]
  defp validate_positive_integer(errors, config, path) do
    case get_in(config, path) do
      n when is_integer(n) and n > 0 -> errors
      val -> ["#{format_path(path)} must be a positive integer, got: #{inspect(val)}" | errors]
    end
  end

  @spec validate_log_level([String.t()], map()) :: [String.t()]
  defp validate_log_level(errors, config) do
    valid_levels = [:debug, :info, :warning, :error]

    case get_in(config, [:logging, :level]) do
      level when level in valid_levels -> errors
      level -> ["logging.level must be one of #{inspect(valid_levels)}, got: #{inspect(level)}" | errors]
    end
  end

  @spec format_path([atom()]) :: String.t()
  defp format_path(path) do
    path |> Enum.map(&Atom.to_string/1) |> Enum.join(".")
  end
end
```
**Why this works:**

- `build/1` takes a keyword list of options. `Keyword.get/3` provides defaults for
  each option. This is the idiomatic Elixir pattern for optional function parameters —
  not overloaded arities, not a map of options.
- `deep_merge/2` uses `Map.merge/3` with a resolver function. When both values are maps,
  it recurses. Otherwise, the right value wins. This handles arbitrarily nested configs.
- Validation collects all errors into a list (prepending for O(1)) and reverses at the
  end. This means you see ALL validation failures at once, not just the first one.
- `get_in/2` is a built-in Elixir function that navigates nested maps using a key path.
  It returns `nil` for missing keys at any depth.

### `test/app_config_test.exs`
```elixir
defmodule AppConfigTest do
  use ExUnit.Case, async: true

  doctest AppConfig

  describe "build/1" do
    test "returns defaults when no overrides given" do
      assert {:ok, config} = AppConfig.build()
      assert config.server.host == "localhost"
      assert config.server.port == 4000
      assert config.database.name == "app_dev"
    end

    test "file config overrides defaults" do
      assert {:ok, config} = AppConfig.build(file_config: %{server: %{port: 8080}})
      assert config.server.port == 8080
      assert config.server.host == "localhost"
    end

    test "env config overrides file config" do
      assert {:ok, config} =
               AppConfig.build(
                 file_config: %{server: %{port: 8080}},
                 env_config: %{server: %{port: 9090}}
               )

      assert config.server.port == 9090
    end

    test "deep merges preserve sibling keys" do
      assert {:ok, config} =
               AppConfig.build(file_config: %{database: %{name: "app_prod"}})

      assert config.database.name == "app_prod"
      assert config.database.host == "localhost"
      assert config.database.pool_size == 10
    end

    test "returns errors for invalid config" do
      assert {:error, errors} =
               AppConfig.build(file_config: %{server: %{port: -1}})

      assert Enum.any?(errors, &String.contains?(&1, "port"))
    end

    test "skips validation when validate: false" do
      assert {:ok, _config} =
               AppConfig.build(
                 file_config: %{server: %{port: -1}},
                 validate: false
               )
    end
  end

  describe "deep_merge/2" do
    test "merges flat maps" do
      assert AppConfig.deep_merge(%{a: 1}, %{b: 2}) == %{a: 1, b: 2}
    end

    test "right wins for conflicting keys" do
      assert AppConfig.deep_merge(%{a: 1}, %{a: 2}) == %{a: 2}
    end

    test "recursively merges nested maps" do
      left = %{a: %{b: 1, c: 2}}
      right = %{a: %{c: 3, d: 4}}
      assert AppConfig.deep_merge(left, right) == %{a: %{b: 1, c: 3, d: 4}}
    end

    test "replaces map with non-map" do
      left = %{a: %{b: 1}}
      right = %{a: "overridden"}
      assert AppConfig.deep_merge(left, right) == %{a: "overridden"}
    end
  end

  describe "get_in_config/2" do
    test "fetches nested value" do
      config = %{server: %{port: 4000}}
      assert {:ok, 4000} = AppConfig.get_in_config(config, [:server, :port])
    end

    test "returns :error for missing key" do
      config = %{server: %{port: 4000}}
      assert :error = AppConfig.get_in_config(config, [:server, :missing])
    end

    test "returns :error for missing nested path" do
      config = %{server: %{port: 4000}}
      assert :error = AppConfig.get_in_config(config, [:missing, :deep, :path])
    end
  end

  describe "validate/1" do
    test "passes for valid defaults" do
      assert :ok = AppConfig.validate(AppConfig.defaults())
    end

    test "collects multiple errors" do
      bad_config = %{
        server: %{port: -1, host: "", pool_size: 0},
        database: %{port: 99999, host: "ok", name: "ok", pool_size: 5, ssl: false},
        logging: %{level: :info, format: "json"}
      }

      assert {:error, errors} = AppConfig.validate(bad_config)
      assert length(errors) >= 3
    end

    test "validates log level" do
      config = AppConfig.defaults() |> put_in([:logging, :level], :trace)
      assert {:error, errors} = AppConfig.validate(config)
      assert Enum.any?(errors, &String.contains?(&1, "logging.level"))
    end
  end
end
```
### Run the tests

```bash
mix test --trace
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== AppConfig: demo ===\n")

    result_1 = AppConfig.deep_merge(%{a: %{b: 1, c: 2}}, %{a: %{c: 3, d: 4}})
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = AppConfig.deep_merge(%{a: 1}, %{a: 2})
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = AppConfig.deep_merge(%{a: 1}, %{b: 2})
    IO.puts("Demo 3: #{inspect(result_3)}")

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

---

Create a file `lib/app_config.ex` with the `AppConfig` module above, then test it in `iex`:

```elixir
defmodule AppConfig do
  @defaults %{
    server: %{
      host: "localhost",
      port: 4000,
      pool_size: 10
    },
    database: %{
      host: "localhost",
      port: 5432,
      name: "app_dev",
      pool_size: 10,
      ssl: false
    },
    logging: %{
      level: :info,
      format: "json"
    }
  }

  def defaults, do: @defaults

  def build(opts \\ []) do
    file_config = Keyword.get(opts, :file_config, %{})
    env_config = Keyword.get(opts, :env_config, %{})
    validate? = Keyword.get(opts, :validate, true)

    config =
      @defaults
      |> deep_merge(file_config)
      |> deep_merge(env_config)

    if validate? do
      case validate(config) do
        :ok -> {:ok, config}
        {:error, errors} -> {:error, errors}
      end
    else
      {:ok, config}
    end
  end

  def deep_merge(left, right) when is_map(left) and is_map(right) do
    Map.merge(left, right, fn _key, left_val, right_val ->
      if is_map(left_val) and is_map(right_val) do
        deep_merge(left_val, right_val)
      else
        right_val
      end
    end)
  end

  def get_in_config(config, keys) when is_map(config) and is_list(keys) do
    case get_in(config, keys) do
      nil -> :error
      value -> {:ok, value}
    end
  end

  def validate(config) do
    errors =
      []
      |> validate_port(config, [:server, :port])
      |> validate_port(config, [:database, :port])
      |> validate_non_empty_string(config, [:server, :host])
      |> validate_non_empty_string(config, [:database, :host])
      |> validate_non_empty_string(config, [:database, :name])
      |> validate_positive_integer(config, [:server, :pool_size])
      |> validate_positive_integer(config, [:database, :pool_size])
      |> validate_log_level(config)

    case errors do
      [] -> :ok
      errs -> {:error, Enum.reverse(errs)}
    end
  end

  defp validate_port(errors, config, path) do
    case get_in(config, path) do
      port when is_integer(port) and port > 0 and port <= 65535 -> errors
      _ -> ["Port at #{inspect(path)} must be 1..65535" | errors]
    end
  end

  defp validate_non_empty_string(errors, config, path) do
    case get_in(config, path) do
      str when is_binary(str) and byte_size(str) > 0 -> errors
      _ -> ["String at #{inspect(path)} must be non-empty" | errors]
    end
  end

  defp validate_positive_integer(errors, config, path) do
    case get_in(config, path) do
      n when is_integer(n) and n > 0 -> errors
      _ -> ["Integer at #{inspect(path)} must be > 0" | errors]
    end
  end

  defp validate_log_level(errors, config) do
    case get_in(config, [:logging, :level]) do
      level when level in [:debug, :info, :warning, :error] -> errors
      _ -> ["Log level must be one of :debug, :info, :warning, :error" | errors]
    end
  end
end

# Test it:
{:ok, config} = AppConfig.build(file_config: %{server: %{port: 8080}})
IO.inspect(config.server.port)  # 8080 - overridden by file_config
IO.inspect(config.server.host)  # "localhost" - kept from defaults
IO.inspect(config.database.name)  # "app_dev" - from defaults

{:ok, _} = AppConfig.build()  # Valid config with all defaults
IO.puts("Valid config built successfully")

{:error, errors} = AppConfig.build(file_config: %{server: %{port: 99999}})
IO.inspect(errors)  # Port validation fails
```
---

## Map access: dot notation vs bracket notation

```elixir
config = %{server: %{port: 4000}}

# Dot notation — only works with atom keys, raises on missing key
config.server.port  # => 4000
config.server.missing  # => KeyError

# Bracket notation — works with any key type, returns nil on missing
config[:server][:port]  # => 4000
config[:server][:missing]  # => nil

# get_in — navigates nested structures with a path
get_in(config, [:server, :port])  # => 4000

# Map.get with default — single level only
Map.get(config.server, :timeout, 5000)  # => 5000
```
Use dot notation when you know the key exists (structs, validated configs). Use
bracket notation or `Map.get/3` when the key might be missing.

---

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of deep_merge of two 5-level configs
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```
Target: **< 50µs for a config tree with < 1000 keys total**.

## Common production mistakes

**1. Confusing keyword lists and maps**
`[port: 4000]` is a keyword list. `%{port: 4000}` is a map. They look similar
but behave differently. Keyword lists allow duplicate keys; maps do not.
`Keyword.get/2` works on keyword lists; `Map.get/2` works on maps.

**2. Using `Map.merge/2` for nested configs**
`Map.merge(%{a: %{b: 1, c: 2}}, %{a: %{c: 3}})` produces `%{a: %{c: 3}}` —
it replaces the entire nested map, losing `b: 1`. Always use deep merge for
nested configurations.

**3. Modifying maps in keyword list position**
`fun(%{key: val})` passes a map. `fun(key: val)` passes a keyword list. They are
not interchangeable. If a function expects `opts` as a keyword list, do not pass a map.

**4. Forgetting that `get_in` returns nil for any missing key in the path**
`get_in(%{a: 1}, [:b, :c])` returns `nil`, not an error. If `nil` is a valid
value in your domain, use `Map.fetch/2` which distinguishes missing from nil.

**5. Pattern matching maps is partial**
`%{port: port} = %{port: 4000, host: "localhost"}` succeeds — maps pattern
match on a subset. This is different from tuples, which must match exactly.

---

## Reflection

If your config tree had 100k keys (e.g., feature flags per tenant), would you still deep-merge in memory, or move to ETS with per-tenant namespaces? What changes at that scale?

Why does Elixir enforce atom keys for keyword lists but not for maps? How does that constraint affect API design?

## Resources

- [Maps — Elixir Getting Started](https://elixir-lang.org/getting-started/keywords-and-maps.html)
- [Keyword — HexDocs](https://hexdocs.pm/elixir/Keyword.html)
- [Map — HexDocs](https://hexdocs.pm/elixir/Map.html)
- [Access — HexDocs](https://hexdocs.pm/elixir/Access.html)
- [get_in/2 — Kernel](https://hexdocs.pm/elixir/Kernel.html#get_in/2)

---

## Why Maps and Keyword Lists matters

Mastering **Maps and Keyword Lists** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. Maps Are Key-Value Stores, Keyword Lists Are Ordered Tuples
Maps use any term as a key and unordered lookup. Keyword lists are ordered lists of tuples—each key can appear multiple times. Choose maps for data, keyword lists for options.

### 2. Keyword Lists Allow Duplicate Keys
A keyword list can have multiple entries for the same key. Accessing with `[]` returns the first value. Use `Keyword.get_values/2` to get all values.

### 3. Pattern Matching on Maps Matches Partial Keys
Only keys you pattern-match on need to be present. Extra keys are ignored. This makes maps flexible for APIs where you don't control all fields.

---
