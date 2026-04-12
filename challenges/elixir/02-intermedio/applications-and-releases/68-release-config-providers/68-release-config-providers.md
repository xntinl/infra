# `Config.Provider` — load JSON config at release boot

**Project**: `config_providers_demo` — a custom `Config.Provider` that
reads a JSON file on the target machine at release boot and merges
it into the application environment, before any app starts.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Your ops team wants to manage production config in a sealed JSON file
distributed separately from the release tarball — separate lifecycle,
different file permissions, optionally hot-swappable. `runtime.exs` and
env vars can work for this, but they don't compose well when the config
is large, nested, or managed by a different tool (Vault, Consul, a
sidecar that drops a JSON at a known path).

`Config.Provider` is the official extension point: a module that runs
*after* `runtime.exs` (or *instead* of it) and writes into the same
application environment. This exercise builds one end-to-end that
reads JSON.

Project structure:

```
config_providers_demo/
├── config/
│   ├── config.exs
│   └── runtime.exs
├── lib/
│   ├── config_providers_demo.ex
│   └── config_providers_demo/
│       ├── application.ex
│       └── json_config_provider.ex
├── priv/
│   └── sample_config.json
├── test/
│   └── json_config_provider_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Config.Provider` behaviour

Two callbacks:

```elixir
@callback init(term()) :: term()
@callback load(config :: keyword(), state :: term()) :: keyword()
```

- `init/1` — runs at **assembly time** (`mix release`). Validate and
  stash a state that will be serialized into the release.
- `load/2` — runs at **boot time**, before any app starts. Must return
  the full merged keyword list to become the application environment.

```
mix release ──▶ init/1 (assembly)
boot        ──▶ load/2 (on the target) ──▶ applications started
```

### 2. Why after `runtime.exs` is a feature, not a bug

Providers always run *after* `runtime.exs`, so they get last word. That
lets you express sane defaults in `runtime.exs` and have the provider
layer environment-specific overrides on top.

### 3. `Config.Reader.merge/2` — the correct merge

Application config is deeply nested keyword lists. Shallow merges lose
keys silently. `Config.Reader.merge/2` does the right thing — always
use it to combine the existing config with provider output.

### 4. No Jason during assembly

`init/1` runs inside `mix release` — you don't have your runtime deps
loaded the same way. Keep `init/1` trivial (path validation) and decode
the JSON inside `load/2` where the release environment is live.

---

## Implementation

### Step 1: Create the project and add Jason

```bash
mix new config_providers_demo --sup
cd config_providers_demo
mkdir -p config priv
```

Add `{:jason, "~> 1.4"}` to `mix.exs` deps, and declare the provider
in the release:

```elixir
defp deps, do: [{:jason, "~> 1.4"}]

defp releases do
  [
    config_providers_demo: [
      include_executables_for: [:unix],
      config_providers: [
        # Path resolved at boot time on the target machine. Use a
        # release-relative path or an env var — NOT a dev-machine path.
        {ConfigProvidersDemo.JsonConfigProvider,
         path: {:system, "APP_CONFIG_JSON", "/etc/config_providers_demo/config.json"}}
      ]
    ]
  ]
end
```

### Step 2: `priv/sample_config.json`

```json
{
  "config_providers_demo": {
    "endpoint": "https://prod.example.com",
    "pool_size": 50,
    "feature_flags": { "new_ui": true }
  }
}
```

### Step 3: `lib/config_providers_demo/json_config_provider.ex`

```elixir
defmodule ConfigProvidersDemo.JsonConfigProvider do
  @moduledoc """
  Loads a JSON file at release boot and merges it into the Elixir
  application environment.

  The path is resolved from the provider options, supporting either a
  literal string or `{:system, "ENV", default}` to read an env var.
  """

  @behaviour Config.Provider

  @impl true
  def init(opts) do
    # Assembly time: only validate shape. DO NOT touch the filesystem —
    # the target host is not the build host.
    path = Keyword.fetch!(opts, :path)
    {:path, path}
  end

  @impl true
  def load(config, {:path, path_spec}) do
    # Boot time on the target: now we can read env vars and files.
    path = resolve_path(path_spec)

    case File.read(path) do
      {:ok, body} ->
        json = Jason.decode!(body)
        provider_config = to_keyword(json)
        Config.Reader.merge(config, provider_config)

      {:error, :enoent} ->
        # Missing file is a deployment error — crash loudly, don't silently
        # boot with partial config.
        raise "config provider: file not found at #{inspect(path)}"
    end
  end

  defp resolve_path({:system, env, default}) do
    System.get_env(env, default)
  end

  defp resolve_path(path) when is_binary(path), do: path

  # Convert a JSON map with string keys into the keyword/atom structure
  # the Elixir config system expects. We restrict atom creation to the
  # known application list to avoid unbounded atom table growth.
  defp to_keyword(json) when is_map(json) do
    Enum.map(json, fn {app_str, app_config} ->
      app = String.to_existing_atom(app_str)
      {app, to_keyword_keys(app_config)}
    end)
  end

  defp to_keyword_keys(map) when is_map(map) do
    Enum.map(map, fn {k, v} ->
      {String.to_atom(k), to_value(v)}
    end)
  end

  defp to_value(v) when is_map(v), do: to_keyword_keys(v)
  defp to_value(v), do: v
end
```

### Step 4: `lib/config_providers_demo/application.ex` and `lib/config_providers_demo.ex`

```elixir
defmodule ConfigProvidersDemo.Application do
  @moduledoc false
  use Application
  require Logger

  @impl true
  def start(_type, _args) do
    Logger.info("config on boot: #{inspect(ConfigProvidersDemo.snapshot())}")
    Supervisor.start_link([], strategy: :one_for_one, name: ConfigProvidersDemo.Supervisor)
  end
end
```

```elixir
defmodule ConfigProvidersDemo do
  @spec snapshot() :: keyword()
  def snapshot, do: Application.get_all_env(:config_providers_demo)
end
```

### Step 5: `test/json_config_provider_test.exs`

```elixir
defmodule ConfigProvidersDemo.JsonConfigProviderTest do
  use ExUnit.Case, async: false
  alias ConfigProvidersDemo.JsonConfigProvider

  @tmp_path System.tmp_dir!() |> Path.join("cpd_#{System.unique_integer([:positive])}.json")

  setup do
    File.write!(@tmp_path, ~s|{"config_providers_demo":{"endpoint":"http://from-json","pool_size":77}}|)
    on_exit(fn -> File.rm(@tmp_path) end)
    :ok
  end

  test "load/2 merges JSON into existing config" do
    state = JsonConfigProvider.init(path: @tmp_path)

    existing = [config_providers_demo: [endpoint: "http://default", unrelated: :kept]]
    merged = JsonConfigProvider.load(existing, state)

    assert get_in(merged, [:config_providers_demo, :endpoint]) == "http://from-json"
    assert get_in(merged, [:config_providers_demo, :pool_size]) == 77
    # Pre-existing unrelated keys survive the merge.
    assert get_in(merged, [:config_providers_demo, :unrelated]) == :kept
  end

  test "missing file raises a clear error" do
    state = JsonConfigProvider.init(path: "/nonexistent/path.json")
    assert_raise RuntimeError, ~r/file not found/, fn ->
      JsonConfigProvider.load([], state)
    end
  end

  test "{:system, ...} path resolves from env var" do
    System.put_env("CPD_TEST_PATH", @tmp_path)
    state = JsonConfigProvider.init(path: {:system, "CPD_TEST_PATH", "/dev/null"})

    assert [{:config_providers_demo, _}] = JsonConfigProvider.load([], state)
  after
    System.delete_env("CPD_TEST_PATH")
  end
end
```

### Step 6: Build and run the release

```bash
MIX_ENV=prod mix release
APP_CONFIG_JSON=$PWD/priv/sample_config.json \
  _build/prod/rel/config_providers_demo/bin/config_providers_demo eval \
    "IO.inspect ConfigProvidersDemo.snapshot()"
```

---

## Trade-offs and production gotchas

**1. Atoms from JSON are dangerous**
`String.to_atom/1` can exhaust the atom table if user-controlled input
reaches it. Always restrict to a known schema (`String.to_existing_atom/1`
for top-level keys) and bound the depth of conversion.

**2. Providers run before Logger is fully configured**
If your provider crashes during `load/2`, the error output can be
terser than usual. Test providers thoroughly with unit tests — don't
debug them first on a live node.

**3. `config/runtime.exs` is simpler — use providers only when you need them**
Providers shine when config comes from a different source (JSON/YAML,
Consul, Vault). For env vars, plain `runtime.exs` is lighter.

**4. Don't hit the network in `load/2`**
Every boot will pay the latency, and a transient network blip becomes a
boot failure. If remote config is required, cache it to disk out of band
and have the provider read the cache.

**5. When NOT to use a custom provider**
If you're reading env vars, `runtime.exs` does it. If you want TOML,
there's `toml` provider packages on Hex. Roll your own only when no
existing solution fits.

---

## Resources

- [`Config.Provider`](https://hexdocs.pm/elixir/Config.Provider.html)
- [`Config.Reader.merge/2`](https://hexdocs.pm/elixir/Config.Reader.html#merge/2)
- [Mix releases — Config providers](https://hexdocs.pm/mix/Mix.Tasks.Release.html#module-config-providers)
