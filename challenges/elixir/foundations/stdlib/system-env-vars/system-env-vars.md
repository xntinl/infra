# Environment variables and runtime config

**Project**: `env_config` — loads and validates required env vars at boot, fails fast if anything is missing.

---

## Project structure

```
env_config/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   └── env_config.ex
├── test/
│   └── env_config_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

---

## The business problem
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

## Why accumulate errors and not fail on the first missing var

The naive pattern — `System.fetch_env!/1` once per var — reports one problem, raises, and exits. Ops fixes that var, redeploys, hits the next missing var, redeploys again, and so on. A ten-variable service can take ten deploys to boot. Accumulating every failure into one `RuntimeError` message means ops sees the full list on the first try and fixes them in a single edit. This is the same reasoning behind Ecto changesets: the user doesn't want to learn their form has errors one field at a time.

---

## Design decisions

**Option A — read env vars inline at each call site with `System.fetch_env!/1`**
- Pros: no loader module; failures point to the exact caller in the stacktrace; zero indirection.
- Cons: failure on first missing var forces one redeploy per problem; no type coercion (everything is a string); no place to document which vars the service needs.

**Option B — schema-driven loader with `load!/1` called from `runtime.exs`, accumulates errors** (chosen)
- Pros: one boot-time report listing every missing/invalid var; type coercion (`:integer`, `:boolean`, `:url`) centralised; schema doubles as documentation; required/optional distinction is explicit; empty string (`FOO=`) treated as missing, matching operator intent.
- Cons: extra module; tiny indirection between "what the service reads" and "where it reads it".

Chose **B** because the cost of a single missed deploy cycle at 03:00 (when ops is on-call) dwarfs the cost of maintaining a 100-line loader. Centralising env var policy also prevents the "grep for `System.get_env` and pray you found them all" migration pattern.

---

## Implementation

### `mix.exs`
```elixir
defmodule EnvConfig.MixProject do
  use Mix.Project

  def project do
    [
      app: :env_config,
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

### Step 1: Create the project

**Objective**: runtime.exs (boot-time) reads env vars, not config.exs (compile-time); fail-fast with full error list.

```bash
mix new env_config
cd env_config
mkdir -p config
```

### `lib/env_config.ex`

**Objective**: System.fetch_env/1 returns {:ok|:error}; accumulate all failures before raising with complete list.

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
    # Minimal check — a stricter validator can layer scheme whitelisting
    # and host format on top; here we just confirm it parses as http(s).
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

**Objective**: Compile-time config only; env vars go in runtime.exs, not here — reading env at compile-time is wrong.

```elixir
import Config

# Compile-time config only. NEVER read env vars here — this file runs on the
# build machine, not in production. See runtime.exs for env-derived values.
config :env_config, :app_name, "env_config_demo"
```

### Step 4: `config/runtime.exs`

**Objective**: Call env_config.load!/0 here; boot fails with complete error list if any required var is missing/invalid.

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

**Objective**: Test type coercion (":false" is truthy string; parse to bool), missing vars, empty strings as missing.

```elixir
defmodule EnvConfigTest do
  use ExUnit.Case, async: false
  doctest EnvConfig
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

**Objective**: --warnings-as-errors catches unused env schema fields; test coverage validates all failures reported.

```bash
mix test
```

### Why this works

`System.fetch_env/1` (not `fetch_env!/1`) returns `{:ok, val} | :error`, which lets the reducer keep going after a missing var and collect every problem. The spec shape `{NAME, type, opts}` keeps required-ness, defaults, and coercion together at a single callsite so ops can read `runtime.exs` and know exactly what the service needs. `load!/1` raises a `RuntimeError` with the full error list joined into one message, so the release fails to boot — OTP's supervision tree treats that as an abort rather than a restart loop, which is the correct behaviour for bad configuration.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== EnvConfig: demo ===\n")

    result_1 = Mix.env()
    IO.puts("Demo 1: #{inspect(result_1)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create a simple example demonstrating the key concepts:

```elixir
# Example code demonstrating module concepts
IO.puts("Example: Read the Implementation section above and run the code samples in iex")
```

## Benchmark

<!-- benchmark N/A: boot-time configuration, runs once per release start; microsecond cost is irrelevant -->

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

## Reflection

1. `load!/1` raises on any invalid var and the release fails to boot. A platform team runs hundreds of releases and wants a "degraded mode" where non-critical vars can be missing. Would you add a `:severity` flag per spec, split `load!/1` into `load_required!/1` + `load_optional/1`, or push the policy out to each service's `runtime.exs`? Where should "this var is optional in staging but required in prod" live?
2. The loader treats empty strings as missing. In the ops team's `.env` convention, `FOO=` actually means "explicitly disabled" (different from "unset"). How would you distinguish `:unset | :disabled | {:set, value}` in the spec and the return shape without breaking existing callers? What's the minimum change to the current API?

---

## Resources

- [`System` — Elixir stdlib](https://hexdocs.pm/elixir/System.html)
- [Config and releases](https://hexdocs.pm/elixir/Config.html) — `config_env/0`, `config_target/0`
- [Mix releases — runtime configuration](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-runtime-configuration)

---

## Why Environment variables and runtime config matters

Mastering **Environment variables and runtime config** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/env_config_test.exs`

```elixir
defmodule EnvConfigTest do
  use ExUnit.Case, async: true

  doctest EnvConfig

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert EnvConfig.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. `System.get_env/1` Reads Environment Variables
Environment variables are the standard way to pass configuration to applications (API keys, database URLs, feature flags).

### 2. Never Hardcode Secrets
Always read secrets from environment. This prevents accidentally committing secrets to version control.

### 3. Compile-Time vs Runtime
`System.get_env/1` reads at runtime. For compile-time configuration, use module attributes and environment-specific builds.

---
