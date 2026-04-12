# ETS as a Shared Buffer with NIF-Side Readers

**Project**: `feature_flags_fast_path` — serve feature flags with sub-microsecond reads from a NIF that reads directly out of an ETS table owned by a GenServer.

## Project context

A feature-flag service is consulted on every HTTP request — millions of calls per second
across the fleet. The existing `:ets.lookup/2` path is already fast (< 500ns), but when
flag values are used inside hot loops (e.g. a request handler checks 40 flags), the BEAM
term copying and call overhead adds up. A NIF that reads the ETS table **directly via the
ETS C API** (`enif_get_resource` on a table reference + `ets_select`) can bypass the term
interface entirely for the common case.

This exercise builds the full pattern: a GenServer owns a `:public, :named_table` ETS
table, a NIF gets a `:compiled_match_spec`-optimized read path, and both sides collaborate
around a single source of truth.

```
feature_flags_fast_path/
├── lib/
│   └── feature_flags_fast_path/
│       ├── application.ex
│       ├── store.ex
│       └── nif.ex
├── native/
│   └── ff_nif/
│       ├── Cargo.toml
│       └── src/lib.rs
├── test/feature_flags_fast_path/store_test.exs
├── bench/ff_bench.exs
└── mix.exs
```

## Why NIF readers on ETS and not a dedicated NIF resource

`:ets` already provides lock-free concurrent reads with `read_concurrency: true`. The
advantage is that Elixir and NIFs can both read the same data without synchronization.
If you used a NIF resource (e.g. a `DashMap` inside Rust), only code going through the
NIF can read it — Elixir tools like `:observer` or `ex_cluster_status` cannot inspect
the state.

Keeping state in ETS preserves observability while letting the NIF be a zero-copy fast
path.

## Why the NIF reads via `enif_whereis_name` and not a handle passed each call

Passing the ETS table name on every call costs: the NIF must `enif_whereis_pid` or
`enif_whereis_name` on a registered atom. `:named_table` makes this a single call.

The alternative — handing a table *reference* (as an `integer`) to the NIF and keeping
it — sounds faster but is wrong: ETS table IDs are opaque and the BEAM makes no stability
promise across runs. Always use the registered name.

## Core concepts

### 1. `read_concurrency: true`

Tells ETS to optimize the read path by duplicating data structures across scheduler-local
caches. Writes become slower; reads are lock-free fast paths. Always set for read-heavy
tables.

### 2. NIF access to ETS

The NIF API exposes `enif_*` functions that open a table by name and iterate. Rustler
wraps parts of this; for full power you drop to `erl_nif.h` directly. We use Rustler's
`env.get_resource` pattern combined with a `:compiled_match_spec`.

### 3. Compiled match specs

`:ets.match_spec_compile/1` returns a compiled form that runs 10x faster than the
interpreted form. The BEAM exposes this via `match_spec_run/2`. For repeated reads of
the same pattern, always compile once and reuse.

### 4. Process-less reads

Because `:public` ETS has no owner for read purposes (owner is only for lifetime), a NIF
can call `:ets.lookup` — which goes through the standard lookup path — without any
GenServer involvement, serialization, or mailbox hop.

## Design decisions

- **Option A — NIF does `ets:lookup_element/3` via the standard path**. Simple; uses
  existing ETS optimizations. Minor overhead of term roundtripping.
- **Option B — NIF links against `libet.so` directly**. Zero translation cost but
  fragile and requires low-level API access.

→ **Option A**. Even senior teams should prefer stability over marginal gains unless
  the benchmark shows Option A is the bottleneck.

- **Option A — cache the compiled match spec in a resource**.
- **Option B — recompile per call**.

→ **Option A**. Compilation is ~10µs vs. ~100ns per read.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule FeatureFlagsFastPath.MixProject do
  use Mix.Project

  def project do
    [
      app: :feature_flags_fast_path,
      version: "0.1.0",
      elixir: "~> 1.17",
      compilers: [:rustler] ++ Mix.compilers(),
      rustler_crates: [
        ff_nif: [path: "native/ff_nif", mode: :release]
      ],
      deps: [
        {:rustler, "~> 0.34"},
        {:benchee, "~> 1.3", only: :dev}
      ]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {FeatureFlagsFastPath.Application, []}]
end
```

### Step 1: ETS owner GenServer

```elixir
defmodule FeatureFlagsFastPath.Store do
  @moduledoc """
  Owns the `:feature_flags` ETS table.

  Table layout:
    key   = binary flag name
    value = boolean

  Writes go through this GenServer (mutation strongly typed).
  Reads can go either through Elixir `ets:lookup` or through the NIF fast path.
  """
  use GenServer

  @table :feature_flags

  # ---- Public API ---------------------------------------------------------

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @doc "Write — serialized through the GenServer."
  def set(flag, value) when is_binary(flag) and is_boolean(value) do
    GenServer.call(__MODULE__, {:set, flag, value})
  end

  @doc "Read via standard ETS — fast enough for most calls."
  def get(flag) when is_binary(flag) do
    case :ets.lookup(@table, flag) do
      [{^flag, value}] -> value
      [] -> false
    end
  end

  # ---- GenServer ---------------------------------------------------------

  @impl true
  def init(_) do
    :ets.new(@table, [
      :named_table,
      :set,
      :public,
      {:read_concurrency, true},
      {:write_concurrency, false}
    ])
    {:ok, %{}}
  end

  @impl true
  def handle_call({:set, flag, value}, _from, state) do
    :ets.insert(@table, {flag, value})
    {:reply, :ok, state}
  end
end
```

### Step 2: NIF fast path (`lib/feature_flags_fast_path/nif.ex`)

```elixir
defmodule FeatureFlagsFastPath.NIF do
  use Rustler, otp_app: :feature_flags_fast_path, crate: :ff_nif

  @doc """
  Reads a flag directly — no GenServer hop, no term copy beyond the boolean
  return. For hot loops checking N flags, this is the right tool.
  """
  def get(_flag), do: :erlang.nif_error(:nif_not_loaded)

  @doc "Reads a list of flags in one NIF call, returning a map."
  def get_many(_flags), do: :erlang.nif_error(:nif_not_loaded)
end
```

### Step 3: Cargo manifest

```toml
# native/ff_nif/Cargo.toml
[package]
name = "ff_nif"
version = "0.1.0"
edition = "2021"

[lib]
name = "ff_nif"
crate-type = ["cdylib"]

[dependencies]
rustler = "0.34"
```

### Step 4: Rust NIF (`native/ff_nif/src/lib.rs`)

```rust
use rustler::{Atom, Encoder, Env, NifResult, Term};
use rustler::types::atom;
use std::collections::HashMap;

mod atoms {
    rustler::atoms! {
        feature_flags,
        not_found,
    }
}

/// Read one flag by calling back into :ets.lookup/2 via the ei interface.
/// We go through the BEAM's ets module rather than linking its internal API —
/// this is portable across BEAM versions and still avoids the outer term copy
/// because Rustler decodes the lookup result in-place.
#[rustler::nif]
fn get<'a>(env: Env<'a>, flag: String) -> NifResult<Term<'a>> {
    // Call :ets.lookup(:feature_flags, flag) and decode result.
    // Rustler doesn't ship an ets call helper, so we build it via
    // rustler::env::SavedTerm + apply. Simplest path for an exercise:
    // go via rustler::env::Env::apply.
    let table = atoms::feature_flags().to_term(env);
    let key = flag.encode(env);

    let lookup_result: Term<'a> = env.apply(
        atom::erlang(),
        rustler::types::atom::Atom::from_str(env, "apply").unwrap(),
        &[
            atoms::feature_flags().to_term(env), // module placeholder overridden below
        ],
    ).map_err(|_| rustler::Error::BadArg)?;

    // The example above is illustrative. In practice we call the ets module
    // more directly — shown in the simpler form below.
    let _ = (table, key, lookup_result);
    // Fallback: read via the bundled BIF wrapper.
    read_from_ets(env, &flag)
}

fn read_from_ets<'a>(env: Env<'a>, flag: &str) -> NifResult<Term<'a>> {
    // :ets.lookup(table, key) returns a list. We decode it.
    let result: Term<'a> = env.apply(
        rustler::types::atom::Atom::from_str(env, "ets").unwrap(),
        rustler::types::atom::Atom::from_str(env, "lookup").unwrap(),
        &[atoms::feature_flags().to_term(env), flag.encode(env)],
    )?;

    // List decode: either [] or [{key, value}]
    let decoded: Vec<(String, bool)> = result.decode().unwrap_or_default();
    if let Some((_k, v)) = decoded.into_iter().next() {
        Ok(v.encode(env))
    } else {
        Ok(false.encode(env))
    }
}

#[rustler::nif]
fn get_many<'a>(env: Env<'a>, flags: Vec<String>) -> NifResult<Term<'a>> {
    let mut out: HashMap<String, bool> = HashMap::with_capacity(flags.len());
    for flag in flags {
        let result: Term<'a> = env.apply(
            rustler::types::atom::Atom::from_str(env, "ets").unwrap(),
            rustler::types::atom::Atom::from_str(env, "lookup").unwrap(),
            &[atoms::feature_flags().to_term(env), flag.encode(env)],
        )?;
        let decoded: Vec<(String, bool)> = result.decode().unwrap_or_default();
        let value = decoded.into_iter().next().map(|(_, v)| v).unwrap_or(false);
        out.insert(flag, value);
    }
    Ok(out.encode(env))
}

rustler::init!("Elixir.FeatureFlagsFastPath.NIF", [get, get_many]);
```

### Step 5: Application

```elixir
defmodule FeatureFlagsFastPath.Application do
  use Application
  def start(_, _) do
    Supervisor.start_link([FeatureFlagsFastPath.Store],
      strategy: :one_for_one, name: __MODULE__)
  end
end
```

## Why this works

```
writer ──GenServer.call──▶ Store ──ets:insert──▶ ETS table :feature_flags
                                                        ▲
                                                        │
                                       lock-free reads  │
                                                        │
Elixir reader ─ets:lookup──────────────────────────────┤
                                                        │
NIF reader    ─env.apply(:ets, :lookup, ...)───────────┘
```

- `read_concurrency: true` gives per-scheduler cache lines: readers on different schedulers
  never contend.
- The NIF does NOT serialize through the GenServer. It calls `:ets.lookup/2` directly via
  `env.apply` — same code path Elixir reads take, same refc-binary sharing for binary
  values.
- `get_many/1` amortizes the NIF↔BEAM transition over a batch. For 40-flag reads in a
  request handler, one `get_many` call is 40x cheaper than 40 individual `get` calls.

## Tests (`test/feature_flags_fast_path/store_test.exs`)

```elixir
defmodule FeatureFlagsFastPath.StoreTest do
  use ExUnit.Case, async: false
  alias FeatureFlagsFastPath.{Store, NIF}

  setup do
    {:ok, _} = start_supervised(Store)
    # Clear any leftover state.
    :ets.delete_all_objects(:feature_flags)
    :ok
  end

  describe "Store.set and Store.get" do
    test "round-trip with boolean" do
      :ok = Store.set("checkout_v2", true)
      assert Store.get("checkout_v2") == true
    end

    test "missing flag returns false (default)" do
      assert Store.get("never_set") == false
    end

    test "overwrite changes value" do
      :ok = Store.set("x", true)
      :ok = Store.set("x", false)
      assert Store.get("x") == false
    end
  end

  describe "NIF fast path" do
    test "NIF.get matches Store.get" do
      :ok = Store.set("a", true)
      :ok = Store.set("b", false)
      assert NIF.get("a") == true
      assert NIF.get("b") == false
      assert NIF.get("c") == false
    end

    test "NIF.get_many returns a map" do
      :ok = Store.set("x", true)
      :ok = Store.set("y", false)
      result = NIF.get_many(["x", "y", "z"])
      assert result["x"] == true
      assert result["y"] == false
      assert result["z"] == false
    end
  end

  describe "concurrent access" do
    test "200 readers see consistent state with interleaved writer" do
      :ok = Store.set("toggle", true)

      reader_fn = fn ->
        for _ <- 1..100 do
          # The flag may be true or false depending on writer progress;
          # either is fine — we only assert no crash.
          _ = NIF.get("toggle")
        end
      end

      writer = Task.async(fn ->
        for i <- 1..1_000, do: Store.set("toggle", rem(i, 2) == 0)
      end)

      readers = for _ <- 1..200, do: Task.async(reader_fn)

      Task.await_many([writer | readers], 10_000)
    end
  end
end
```

## Benchmark (`bench/ff_bench.exs`)

```elixir
{:ok, _} = FeatureFlagsFastPath.Store.start_link(nil)
for i <- 1..1000 do
  :ok = FeatureFlagsFastPath.Store.set("flag_#{i}", rem(i, 2) == 0)
end

flags = for i <- 1..40, do: "flag_#{i}"

Benchee.run(
  %{
    "40x Store.get (Elixir ets:lookup)" => fn ->
      Enum.each(flags, fn f -> FeatureFlagsFastPath.Store.get(f) end)
    end,
    "40x NIF.get" => fn ->
      Enum.each(flags, fn f -> FeatureFlagsFastPath.NIF.get(f) end)
    end,
    "1x NIF.get_many(40)" => fn ->
      FeatureFlagsFastPath.NIF.get_many(flags)
    end
  },
  time: 5, warmup: 2
)
```

**Expected**:
- 40x Elixir: ~20µs
- 40x NIF individual: ~25µs (per-call overhead dominates)
- 1x NIF.get_many: ~8µs

The insight: for ETS reads, pure Elixir is often faster than individual NIF calls due to
NIF transition cost. The NIF wins when you amortize with a batch API.

## Trade-offs and production gotchas

**1. NIF-to-ETS is not zero-cost.** `env.apply` builds a term tuple for the call; it still
pays some encoding cost. For single lookups, Elixir is as fast or faster.

**2. `read_concurrency` vs `write_concurrency`.** Do not enable both on the same table —
they conflict. Choose based on workload. Feature flags are read-heavy → `read_concurrency`.

**3. Silent defaults.** Missing flags return `false`. A typo in a flag name is
indistinguishable from a disabled flag. Log or tag unknown reads in development.

**4. Mutable flags during request handling.** If a request reads `flag_x` twice, it may
see different values if a writer lands between reads. Capture the flag once per request
and thread the value through the handler.

**5. Atom exhaustion.** If flags were stored as atoms, dynamic flag names would exhaust
the atom table (BEAM limit ~1M). We use binaries — no such issue.

**6. When NOT to use NIF fast paths for ETS.** For < 10 reads per call, Elixir is simpler
and equally fast. Reach for a NIF batch API only when profiling proves call overhead
dominates.

## Reflection

`get_many/1` calls `:ets.lookup/2` N times from inside the NIF — effectively N round
trips through the BIF interface. A direct `enif_*` read would collapse these to one
memory-traversal but requires linking internal Erlang headers. Under what load profile
does the Rustler-via-apply approach become the bottleneck, and at what point is
maintaining C-header NIF code worth the 2-3x speedup?

## Resources

- [`:ets` documentation — Erlang/OTP](https://www.erlang.org/doc/man/ets.html)
- [`erl_nif.h` reference](https://www.erlang.org/doc/man/erl_nif.html)
- [Rustler `env.apply` docs](https://docs.rs/rustler/latest/rustler/env/struct.Env.html)
- [Discord's feature flag system write-up](https://discord.com/blog/)
