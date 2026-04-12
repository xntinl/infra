# Environment variables and runtime config

**Project**: `env_config` — loads and validates required env vars at boot, fails fast if anything is missing.

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project context

You're shipping a small service to staging. The ops team complained that when
`DATABASE_URL` is misspelled the service *starts* but then crashes on the first
request with an unhelpful error. You need a loader that:

1. Fails at boot with a clear list of every missing/invalid env var.
2. Distinguishes required from optional vars.
3. Coerces types (strings to int/bool) and validates the shape.
4. Works the same way locally (with defaults) and in production (strict).

Project structure:

```
env_config/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   └── env_config.ex
├── test/
│   └── env_config_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `System.get_env/2` vs `System.fetch_env!/1`

`System.get_env("FOO")` returns `nil` if missing — silent failure. `System.get_env("FOO", "default")`
is fine when a default is acceptable.
`System.fetch_env!("FOO")` raises if missing — the right tool for "must exist".

For a loader that reports **all** problems at once, neither is ideal — you don't want to
raise on the first missing var. We use `System.fetch_env/1` which returns `{:ok, val}` or
`:error` and collect errors into a list.

### 2. `config.exs` vs `runtime.exs`

This is the single most misunderstood thing about Elixir config.

- `config/config.exs` runs **at compile time**. Values baked into the release. If you
  write `System.get_env("DATABASE_URL")` here, you're reading the env var of the build
  machine, not of production. This is a silent footgun.
- `config/runtime.exs` runs when the release **starts**. This is where production env
  vars belong. It runs after code is loaded but before applications start.

Rule: **env vars belong in `runtime.exs`**. Anything else is a bug waiting to happen.

### 3. Type coercion

Env vars are always strings. `"false"` is truthy in Elixir (only `false` and `nil`
are falsy). `String.to_integer/1` raises on bad input — use `Integer.parse/1` if
you want graceful error handling.

---

## Implementation

### Step 1: Create the project

```bash
mix new env_config
cd env_config
mkdir -p config
```

### Step 2: `lib/env_config.ex`

```elixir
defmodule EnvConfig do
  @moduledoc """
  Loads a typed, validated configuration from environment variables.
  Accumulates all errors before failing — so ops sees every missing var at once.
  """

  # A spec is: {env_var_name, type, opts}
  # opts: [:required | {:default, term}]
  @type spec :: {String.t(), type :: atom(), keyword()}

  @doc """
  Loads a map of config values from the given specs.
  Returns `{:ok, config}` or `{:error, list_of_reasons}`.
  """
  @spec load([spec()]) :: {:ok, map()} | {:error, [String.t()]}
  def load(specs) when is_list(specs) do
    # Accumulate errors instead of raising at the first — ops wants the full list,
    # not to fix env vars one crash at a time.
    {values, errors} =
      Enum.reduce(specs, {%{}, []}, fn spec, {acc, errs} ->
        case load_one(spec) do
          {:ok, key, value} -> {Map.put(acc, key, value), errs}
          {:error, reason} -> {acc, [reason | errs]}
        end
      end)

    case errors do
      [] -> {:ok, values}
      errs -> {:error, Enum.reverse(errs)}
    end
  end

  @doc """
  Same as `load/1` but raises `RuntimeError` on any failure.
  Use this from `runtime.exs` — fail fast, fail loud, don't boot broken.
  """
  @spec load!([spec()]) :: map()
  def load!(specs) do
    case load(specs) do
      {:ok, config} ->
        config

      {:error, errors} ->
        raise """
        Invalid environment configuration:
          - #{Enum.join(errors, "\n  - ")}
        """
    end
  end

  # --- per-spec loading ------------------------------------------------------

  defp load_one({name, type, opts}) do
    key = name |> String.downcase() |> String.to_atom()

    # fetch_env/1 distinguishes "missing" from "present but empty".
    # We treat empty strings as missing — in practice `FOO=` is a misconfiguration.
    case System.fetch_env(name) do
      {:ok, ""} -> handle_missing(name, key, opts)
      {:ok, raw} -> coerce(name, key, raw, type)
      :error -> handle_missing(name, key, opts)
    end
  end

  defp handle_missing(name, key, opts) do
    cond do
      Keyword.get(opts, :required, false) ->
        {:error, "#{name} is required but not set"}

      Keyword.has_key?(opts, :default) ->
        {:ok, key, Keyword.fetch!(opts, :default)}

      true ->
        {:ok, key, nil}
    end
  end

  # --- coercion --------------------------------------------------------------

  defp coerce(_name, key, raw, :string), do: {:ok, key, raw}

  defp coerce(name, key, raw, :integer) do
    # Integer.parse returns {int, rest}. We only accept fully-consumed input —
    # "12abc" should fail, not silently parse to 12.
    case Integer.parse(raw) do
      {n, ""} -> {:ok, key, n}
      _ -> {:error, "#{name} must be an integer, got #{inspect(raw)}"}
    end
  end

  defp coerce(name, key, raw, :boolean) do
    case String.downcase(raw) do
      v when v in ~w(1 true yes on) -> {:ok, key, true}
      v when v in ~w(0 false no off) -> {:ok, key, false}
      _ -> {:error, "#{name} must be boolean-ish (true/false/1/0/yes/no), got #{inspect(raw)}"}
    end
  end

  defp coerce(name, key, raw, :url) do
    # Minimal check — a full validator lives in exercise 68.
    case URI.new(raw) do
      {:ok, %URI{scheme: s, host: h}} when s in ["http", "https"] and is_binary(h) ->
        {:ok, key, raw}

      _ ->
        {:error, "#{name} must be a valid http(s) URL, got #{inspect(raw)}"}
    end
  end
end
```

### Step 3: `config/config.exs`

```elixir
import Config

# Compile-time config only. NEVER read env vars here — this file runs on the
# build machine, not in production. See runtime.exs for env-derived values.
config :env_config, :app_name, "env_config_demo"
```

### Step 4: `config/runtime.exs`

```elixir
import Config

# This runs when the release BOOTS. This is where env vars belong.
# If env vars are missing, EnvConfig.load!/1 raises with a complete list —
# the release will not start, which is exactly what we want.

if config_env() == :prod do
  cfg =
    EnvConfig.load!([
      {"DATABASE_URL", :url, required: true},
      {"PORT", :integer, default: 4000},
      {"SECRET_KEY_BASE", :string, required: true},
      {"DEBUG", :boolean, default: false}
    ])

  config :env_config, :runtime, cfg
end
```

### Step 5: `test/env_config_test.exs`

```elixir
defmodule EnvConfigTest do
  use ExUnit.Case, async: false
  # async: false because we mutate process-global env vars.

  setup do
    # Clean up any leftover env vars between tests.
    on_exit(fn ->
      for var <- ~w(DEMO_URL DEMO_PORT DEMO_DEBUG DEMO_SECRET) do
        System.delete_env(var)
      end
    end)
  end

  describe "load/1 — required handling" do
    test "returns :error listing every missing required var" do
      assert {:error, errors} =
               EnvConfig.load([
                 {"DEMO_URL", :url, required: true},
                 {"DEMO_SECRET", :string, required: true}
               ])

      assert length(errors) == 2
      assert Enum.any?(errors, &String.contains?(&1, "DEMO_URL"))
      assert Enum.any?(errors, &String.contains?(&1, "DEMO_SECRET"))
    end

    test "treats empty string as missing" do
      System.put_env("DEMO_SECRET", "")

      assert {:error, [msg]} =
               EnvConfig.load([{"DEMO_SECRET", :string, required: true}])

      assert msg =~ "DEMO_SECRET"
    end
  end

  describe "load/1 — defaults" do
    test "applies default when var is unset" do
      assert {:ok, %{demo_port: 4000}} =
               EnvConfig.load([{"DEMO_PORT", :integer, default: 4000}])
    end

    test "env value wins over default" do
      System.put_env("DEMO_PORT", "8080")

      assert {:ok, %{demo_port: 8080}} =
               EnvConfig.load([{"DEMO_PORT", :integer, default: 4000}])
    end
  end

  describe "load/1 — coercion" do
    test "rejects non-integer for :integer type" do
      System.put_env("DEMO_PORT", "not-a-number")

      assert {:error, [msg]} =
               EnvConfig.load([{"DEMO_PORT", :integer, required: true}])

      assert msg =~ "must be an integer"
    end

    test "accepts multiple boolean spellings" do
      for {raw, expected} <- [{"true", true}, {"1", true}, {"no", false}, {"OFF", false}] do
        System.put_env("DEMO_DEBUG", raw)

        assert {:ok, %{demo_debug: ^expected}} =
                 EnvConfig.load([{"DEMO_DEBUG", :boolean, required: true}])
      end
    end

    test "rejects invalid URL" do
      System.put_env("DEMO_URL", "not a url")

      assert {:error, [msg]} =
               EnvConfig.load([{"DEMO_URL", :url, required: true}])

      assert msg =~ "http(s) URL"
    end
  end

  describe "load!/1" do
    test "raises with all errors listed" do
      assert_raise RuntimeError, ~r/DEMO_URL.*DEMO_SECRET/s, fn ->
        EnvConfig.load!([
          {"DEMO_URL", :url, required: true},
          {"DEMO_SECRET", :string, required: true}
        ])
      end
    end
  end
end
```

### Step 6: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. `config.exs` runs at compile time**
The single biggest source of "it works locally but prod reads stale secrets" bugs.
If you see `System.get_env/1` in `config.exs`, move it to `runtime.exs`.

**2. Reporting one error at a time is wrong for config**
Ops deploys, it crashes on missing `DATABASE_URL`, they fix it, deploy again, it
crashes on `SECRET_KEY_BASE`, they fix it... The loader should report *all*
problems in one shot.

**3. Empty string is not "present"**
`FOO=` in a `.env` file sets `FOO` to `""` — almost never what the operator meant.
Treat empty as missing and force them to remove the line or set a real value.

**4. Don't log env values**
Logging the loaded config at boot is convenient but leaks secrets into log stores.
If you must log, redact known secret-bearing keys (`SECRET_`, `TOKEN`, `PASSWORD`, `KEY`).

**5. When NOT to use this**
For tiny scripts or one-off tasks, `System.fetch_env!/1` inline is fine. The extra
machinery pays off when you have ≥ 5 env vars and a real deployment pipeline.

---

## Resources

- [`System` — Elixir stdlib](https://hexdocs.pm/elixir/System.html)
- [Config and releases](https://hexdocs.pm/elixir/Config.html) — `config_env/0`, `config_target/0`
- [Mix releases — runtime configuration](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-runtime-configuration)
