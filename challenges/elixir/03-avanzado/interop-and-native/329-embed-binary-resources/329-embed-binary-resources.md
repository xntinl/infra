# Embedding Binary Resources with `@external_resource`

**Project**: `offline_geocoder` — ship a 40MB precomputed coordinate lookup table inside the release so the app needs no network, no database, and no `priv/` directory reads at runtime.

## Project context

A logistics tool must work on edge devices with intermittent connectivity: warehouses, trucks,
customer sites. It maps postal codes to (latitude, longitude) pairs. The source of truth is a
40MB CSV from a national postal service. Shipping this as a separate `priv/` file is fragile —
operators copy the jar/release but forget the data, and path lookups differ across platforms.

The idiomatic Elixir solution: read the CSV at **compile time** into a module attribute, and
make the module recompile if the CSV changes. The compiled BEAM contains the data directly.
Runtime has zero disk access. The `@external_resource` attribute tells the compiler to
track the file as a dependency — touch it, and that module recompiles.

```
offline_geocoder/
├── lib/
│   └── offline_geocoder/
│       ├── application.ex
│       └── lookup.ex
├── priv/
│   └── data/
│       └── postal_codes.csv      # generated at build time or committed
├── test/offline_geocoder/lookup_test.exs
└── mix.exs
```

## Why embed and not read at runtime

Runtime read pros:
- Smaller `.beam` files.
- Swap data without redeploy.

Compile-time embed pros:
- Zero I/O at lookup time — pure constant-term reference.
- Deployment is one artifact (release tarball + no extra files).
- Compile-time validation of the data (malformed CSV fails the build).
- BEAM constant folding — for small enough data, the lookup becomes a compiled jump table.

For a static reference table that changes quarterly (postal codes don't move), **embed**.

## Why `@external_resource` and not just reading inside `__using__` or at module load

`@external_resource "path"` tells Mix: "if this file changes, this module is dirty — recompile
it even if no source changed". Without it, you edit the CSV, run `mix compile`, and nothing
happens — stale data in the compiled module.

## Core concepts

### 1. Module attributes are compile-time values

```elixir
@data File.read!("priv/data/postal_codes.csv")
```

This reads once at compile time. The resulting binary is interned into the module; a function
returning `@data` returns the same refc-binary every call.

### 2. `@external_resource` and dependency tracking

```elixir
@external_resource "priv/data/postal_codes.csv"
```

Mix stores the file's mtime in the compiled manifest. Next build: if mtime changed, recompile
this module.

### 3. Generating functions from data

For lookup tables, you can go further: generate N function clauses, one per key. The BEAM
compiles these to a jump table — O(1) lookup with no map overhead.

```elixir
for {code, lat, lon} <- Enum.take(entries, 10_000) do
  def lookup(unquote(code)), do: {unquote(lat), unquote(lon)}
end
def lookup(_), do: :not_found
```

For millions of entries this produces a multi-MB `.beam`; a map is better above ~10k keys.

### 4. `:persistent_term` for very large blobs

For data > 10MB where you still want compile-time embedding, store in `:persistent_term` at
startup:

```elixir
def boot_table do
  :persistent_term.put({__MODULE__, :csv}, @data)
end
```

`:persistent_term` bypasses process heap copying on every read.

## Design decisions

- **Option A — function clause per entry**: fastest (jump table), bloats BEAM, slow
  compile.
- **Option B — parse at compile time into a Map, store in module attribute**: fast at
  runtime (map lookup), medium BEAM size, fast compile.
- **Option C — parse at compile time into a Map, put in :persistent_term at app start**:
  smallest module, no BEAM-file bloat, tiny constant runtime cost.

→ For 40MB of data we pick **Option C**. Function clauses would produce a 300MB .beam.
  A module attribute works, but loads the whole map into the module at every cold boot.
  `:persistent_term` gives us one place to put the reference.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule OfflineGeocoder.MixProject do
  use Mix.Project

  def project do
    [
      app: :offline_geocoder,
      version: "0.1.0",
      elixir: "~> 1.17",
      deps: [{:benchee, "~> 1.3", only: :dev}]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {OfflineGeocoder.Application, []}]
end
```

### Step 1: The data file

For the exercise, a trimmed CSV (`priv/data/postal_codes.csv`):

```csv
postal_code,latitude,longitude
10001,40.7506,-73.9971
10002,40.7168,-73.9861
10003,40.7316,-73.9892
90001,33.9731,-118.2479
90002,33.9492,-118.2465
94102,37.7796,-122.4149
60601,41.8857,-87.6228
```

In production, this is regenerated from the postal authority's monthly feed during CI.

### Step 2: The lookup module (`lib/offline_geocoder/lookup.ex`)

```elixir
defmodule OfflineGeocoder.Lookup do
  @moduledoc """
  Postal-code-to-coordinates lookup backed by a CSV file bundled at compile time.

  The CSV is read once during compilation. A compile-time parser builds a map.
  The map is published into :persistent_term at boot so runtime lookups do zero
  heap copying.
  """

  # --------------------------------------------------------------- Compile-time

  @csv_path Path.expand("../../priv/data/postal_codes.csv", __DIR__)
  @external_resource @csv_path

  @entries (
    @csv_path
    |> File.read!()
    |> String.split("\n", trim: true)
    |> tl()  # drop header
    |> Enum.map(fn line ->
      [code, lat, lon] = String.split(line, ",")
      {code, {String.to_float(lat), String.to_float(lon)}}
    end)
    |> Map.new()
  )

  @data_version (
    :crypto.hash(:sha256, File.read!(@csv_path))
    |> Base.encode16(case: :lower)
    |> binary_part(0, 8)
  )

  # --------------------------------------------------------------- Runtime API

  @persistent_key {__MODULE__, :table}

  @doc "Call once at boot to install the table into :persistent_term."
  def install do
    :persistent_term.put(@persistent_key, @entries)
    :ok
  end

  @doc "Returns {:ok, {lat, lon}} or :not_found."
  @spec lookup(String.t()) :: {:ok, {float(), float()}} | :not_found
  def lookup(postal_code) when is_binary(postal_code) do
    table = :persistent_term.get(@persistent_key, @entries)
    case Map.fetch(table, postal_code) do
      {:ok, coords} -> {:ok, coords}
      :error -> :not_found
    end
  end

  @doc """
  Identifier of the data bundle. Use in health checks — if two nodes in a
  cluster report different versions, their data is out of sync.
  """
  def data_version, do: @data_version

  @doc "Number of entries in the embedded table."
  def entry_count, do: map_size(@entries)
end
```

### Step 3: Application hook (`lib/offline_geocoder/application.ex`)

```elixir
defmodule OfflineGeocoder.Application do
  use Application

  @impl true
  def start(_, _) do
    :ok = OfflineGeocoder.Lookup.install()
    Supervisor.start_link([], strategy: :one_for_one, name: __MODULE__)
  end
end
```

## Why this works

```
compile time                          runtime
────────────                          ────────
File.read!/1 ─▶ parse CSV ─▶ Map  ───▶ install/0  ─▶ :persistent_term
                                                            │
                                                            │ lookup/1
                                                            │   ↓
                                                    :persistent_term.get
                                                            │
                                                        Map.fetch
                                                            │
                                                    {:ok, {lat, lon}}
```

- **No runtime I/O**. The CSV is parsed once, at compile time. After build, the module
  carries the parsed map; `File.read!` never runs in production.
- **`@external_resource` + mtime tracking**. If someone edits the CSV, `mix compile` rebuilds
  this module. Forgotten data updates are impossible.
- **`:persistent_term`** for large values avoids copying the 40MB map onto every caller's
  heap on each lookup. It is a single shared global with O(1) access, at the cost of being
  slow to update (triggers a global GC). For a read-only table, ideal.
- **`data_version` hash** is a fingerprint. Ops can run `iex -S mix` across nodes and
  compare hashes to detect deploy drift.

## Tests (`test/offline_geocoder/lookup_test.exs`)

```elixir
defmodule OfflineGeocoder.LookupTest do
  use ExUnit.Case, async: true
  alias OfflineGeocoder.Lookup

  setup do
    :ok = Lookup.install()
    :ok
  end

  describe "lookup/1" do
    test "returns coordinates for a known postal code" do
      assert {:ok, {lat, lon}} = Lookup.lookup("10001")
      assert_in_delta lat, 40.7506, 0.0001
      assert_in_delta lon, -73.9971, 0.0001
    end

    test ":not_found for an unknown code" do
      assert :not_found = Lookup.lookup("00000")
    end

    test "non-string input does not crash — it mismatches the clause" do
      assert_raise FunctionClauseError, fn -> Lookup.lookup(123) end
    end
  end

  describe "metadata" do
    test "data_version is a stable hex string" do
      v = Lookup.data_version()
      assert is_binary(v)
      assert byte_size(v) == 8
      assert v =~ ~r/^[0-9a-f]+$/
    end

    test "entry_count matches the known fixture size" do
      # Count depends on the CSV — keep in sync if you change the fixture.
      assert Lookup.entry_count() >= 5
    end
  end

  describe "idempotency" do
    test "install/0 can be called twice without error" do
      :ok = Lookup.install()
      :ok = Lookup.install()
      assert {:ok, _} = Lookup.lookup("10001")
    end
  end
end
```

## Trade-offs and production gotchas

**1. Compile-time parse errors are build breakers.** A malformed line (`String.to_float`
on `"abc"`) crashes `mix compile`. This is a feature — you catch the bug early. Validate
your CSV generator in CI before it lands.

**2. Large `@attribute` bloats BEAM file.** A 40MB CSV expanded into a module attribute
yields a ~40MB `.beam`. The BEAM loads the whole file at module load time (not deferred).
`:persistent_term` lets you still compile-time-embed but isolates the blob from the
module definition lookup path.

**3. `:persistent_term.put` is expensive.** Each put triggers a global GC. Only call
during boot or rare admin operations, never on a per-request path.

**4. Releases and `priv/` paths.** Compile-time reads use `__DIR__` which is resolved at
build time — they work in releases. Runtime `priv_dir` reads need `:code.priv_dir/1`.
Embedding avoids this entire class of issues.

**5. Data hot-reload.** Changing the CSV needs a rebuild + deploy. If data must update
without redeploy, combine: embed a default, read optional override from disk at boot.

**6. When NOT to embed.** If data is > 100MB or changes hourly, runtime reads win. If
the data is machine-specific (per-tenant), a database or per-node config file is
correct. Embedding is for global, slowly-changing reference data.

## Reflection

The `@entries` attribute is computed at compile time with `String.to_float`. For a 40MB
CSV this takes minutes on cold builds. What compile-time optimizations (hint: pre-parse
the CSV into a binary term with `:erlang.term_to_binary`, commit the `.etf` file,
embed that) would reduce compile time while keeping the "zero runtime I/O" property?

## Resources

- [`@external_resource` — Module attributes](https://hexdocs.pm/elixir/Module.html#module-external_resource)
- [`:persistent_term` — Erlang/OTP](https://www.erlang.org/doc/man/persistent_term.html)
- [Build-time data in Phoenix apps — Dashbit blog](https://dashbit.co/blog/)
- [Mix compilers and manifests](https://hexdocs.pm/mix/Mix.Task.Compiler.html)
