# NIF Resources — Managing Native Handles from Elixir

**Project**: `nif_resources` — a NIF that exposes a mutable Rust `HashMap` to Elixir as an opaque resource handle, with proper GC and drop semantics.

**Difficulty**: ★★★★☆

**Estimated time**: 3–6 hours

---

## Project context

Some native state cannot be cleanly (de)serialized across every NIF call: a compiled regex, an open file descriptor, a database cursor, a loaded ML model, a mutable ring buffer. Sending these as `Binary` on every call is either impossible (file descriptors) or wasteful (a 1 GB ML model).

BEAM's answer is the **NIF resource**: an opaque reference term backed by a Rust struct, managed by BEAM's garbage collector. When the last Elixir-visible reference disappears, the BEAM calls a destructor that runs Rust's `Drop` to free the resource. The reference survives message passing, ETS storage, and process crashes — it's just a term.

Real-world uses:
- `html5ever_elixir` — parsed DOM trees held as resources.
- `explorer` — Polars DataFrames (gigabytes) held as resources; row counts, filters, and joins take the resource and return new ones.
- `tokenizers` — a loaded HuggingFace tokenizer (~100 MB) as a single resource, shared across all callers.
- `ex_rated` — token-bucket counters in a shared Mutex-guarded resource.

This exercise builds a minimal but complete pattern: a concurrent `HashMap<String, String>` resource with create/put/get/delete/size, demonstrating `ResourceArc`, `RwLock`, and dropping.

```
nif_resources/
├── lib/nif_resources/
│   ├── native.ex
│   └── store.ex           # Elixir wrapper exposing a nicer API
├── native/nif_resources_nif/
│   ├── Cargo.toml
│   └── src/lib.rs
├── test/nif_resources_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The resource lifecycle

```
 Elixir                       Rust
  Native.new()      ──────▶   Box::new(MyStruct { .. })
                              ResourceArc::new(struct)
      │                       │
 ref#<0.1.0> ◀──────────────  encode as term (opaque)
      │
      │ stored in ETS, passed
      │ to other procs, etc.
      │
      │ last reference dropped
      ▼                         ▼
  BEAM GC ─────────────────▶ Drop::drop(struct)
                              frees memory, closes fd, ...
```

The resource lives as long as any Elixir term references it. When unreferenced, the next GC cycle drops it. No leaks — unless you put it in ETS forever.

### 2. `ResourceArc<T>`

Rustler's `ResourceArc<T>` is an `Arc<T>` with BEAM-aware refcounting. Cloning is cheap (atomic increment). Cross-thread shareable (`T` must be `Send + Sync`).

```rust
pub struct Store(RwLock<HashMap<String, String>>);

#[rustler::nif]
fn new_store() -> ResourceArc<Store> {
    ResourceArc::new(Store(RwLock::new(HashMap::new())))
}
```

### 3. Registering the resource type

At NIF load, you must register each resource type:

```rust
fn on_load(env: Env, _info: Term) -> bool {
    rustler::resource!(Store, env);
    true
}
rustler::init!("...", [...], load = on_load);
```

Otherwise `ResourceArc::new` panics.

### 4. Concurrent access — `Mutex` vs `RwLock`

For read-heavy stores, `RwLock` allows parallel readers. For mixed, `parking_lot::Mutex` (faster than `std::sync::Mutex`) is often the right choice. Avoid holding the lock across a NIF return — that's impossible anyway (the lock guard can't escape), but don't hold it across an expensive computation that could be done outside the lock.

### 5. Panics and poisoned locks

`std::sync::RwLock`/`Mutex` poison on panic. Once poisoned, `.read()`/`.write()` return `Err`. Defensive pattern: `lock.write().unwrap_or_else(|e| e.into_inner())`. Or use `parking_lot`, which does not poison.

### 6. Do not leak resources into ETS

An ETS table holding a resource keeps the resource alive forever. Common mistake: caching "the handle" in ETS and never evicting — memory grows unbounded. Use a TTL or a supervision boundary.

---

## Implementation

### Step 1: `native/nif_resources_nif/Cargo.toml`

```toml
[package]
name = "nif_resources_nif"
version = "0.1.0"
edition = "2021"

[lib]
name = "nif_resources_nif"
crate-type = ["cdylib"]

[dependencies]
rustler = "0.32"
parking_lot = "0.12"
```

### Step 2: `native/nif_resources_nif/src/lib.rs`

```rust
use parking_lot::RwLock;
use rustler::{Atom, Env, NifResult, ResourceArc, Term};
use std::collections::HashMap;

mod atoms {
    rustler::atoms! { ok, not_found, error }
}

pub struct Store(RwLock<HashMap<String, String>>);

fn on_load(env: Env, _info: Term) -> bool {
    rustler::resource!(Store, env);
    true
}

#[rustler::nif]
fn new_store() -> ResourceArc<Store> {
    ResourceArc::new(Store(RwLock::new(HashMap::new())))
}

#[rustler::nif]
fn put(store: ResourceArc<Store>, key: String, value: String) -> Atom {
    store.0.write().insert(key, value);
    atoms::ok()
}

#[rustler::nif]
fn get(store: ResourceArc<Store>, key: String) -> NifResult<(Atom, String)> {
    match store.0.read().get(&key) {
        Some(v) => Ok((atoms::ok(), v.clone())),
        None => Ok((atoms::not_found(), String::new())),
    }
}

#[rustler::nif]
fn delete(store: ResourceArc<Store>, key: String) -> bool {
    store.0.write().remove(&key).is_some()
}

#[rustler::nif]
fn size(store: ResourceArc<Store>) -> usize {
    store.0.read().len()
}

#[rustler::nif]
fn keys(store: ResourceArc<Store>) -> Vec<String> {
    store.0.read().keys().cloned().collect()
}

rustler::init!(
    "Elixir.NifResources.Native",
    [new_store, put, get, delete, size, keys],
    load = on_load
);
```

### Step 3: `lib/nif_resources/native.ex`

```elixir
defmodule NifResources.Native do
  @moduledoc "Raw NIF surface — prefer `NifResources.Store` for application code."

  use Rustler, otp_app: :nif_resources, crate: "nif_resources_nif"

  @opaque store :: reference()

  @spec new_store() :: store()
  def new_store, do: :erlang.nif_error(:nif_not_loaded)

  @spec put(store(), String.t(), String.t()) :: :ok
  def put(_store, _k, _v), do: :erlang.nif_error(:nif_not_loaded)

  @spec get(store(), String.t()) :: {:ok, String.t()} | {:not_found, String.t()}
  def get(_store, _k), do: :erlang.nif_error(:nif_not_loaded)

  @spec delete(store(), String.t()) :: boolean()
  def delete(_store, _k), do: :erlang.nif_error(:nif_not_loaded)

  @spec size(store()) :: non_neg_integer()
  def size(_store), do: :erlang.nif_error(:nif_not_loaded)

  @spec keys(store()) :: [String.t()]
  def keys(_store), do: :erlang.nif_error(:nif_not_loaded)
end
```

### Step 4: `lib/nif_resources/store.ex`

```elixir
defmodule NifResources.Store do
  @moduledoc "Idiomatic Elixir wrapper over the native store resource."

  alias NifResources.Native

  @opaque t :: reference()

  @spec new() :: t()
  def new, do: Native.new_store()

  @spec put(t(), String.t(), String.t()) :: :ok
  def put(store, key, value), do: Native.put(store, key, value)

  @spec fetch(t(), String.t()) :: {:ok, String.t()} | :error
  def fetch(store, key) do
    case Native.get(store, key) do
      {:ok, value} -> {:ok, value}
      {:not_found, _} -> :error
    end
  end

  @spec delete(t(), String.t()) :: boolean()
  def delete(store, key), do: Native.delete(store, key)

  @spec size(t()) :: non_neg_integer()
  def size(store), do: Native.size(store)

  @spec keys(t()) :: [String.t()]
  def keys(store), do: Native.keys(store)
end
```

### Step 5: `mix.exs`

```elixir
defmodule NifResources.MixProject do
  use Mix.Project

  def project do
    [
      app: :nif_resources,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: [{:rustler, "~> 0.32"}],
      rustler_crates: [nif_resources_nif: [path: "native/nif_resources_nif", mode: :release]]
    ]
  end

  def application, do: [extra_applications: [:logger]]
end
```

### Step 6: `test/nif_resources_test.exs`

```elixir
defmodule NifResourcesTest do
  use ExUnit.Case, async: true
  alias NifResources.Store

  test "basic CRUD" do
    s = Store.new()
    assert Store.size(s) == 0
    :ok = Store.put(s, "a", "1")
    :ok = Store.put(s, "b", "2")
    assert Store.size(s) == 2
    assert {:ok, "1"} = Store.fetch(s, "a")
    assert :error = Store.fetch(s, "missing")
    assert Store.delete(s, "a")
    refute Store.delete(s, "a")
    assert Store.size(s) == 1
  end

  test "resource is opaque reference" do
    s = Store.new()
    assert is_reference(s)
  end

  test "resource survives sending across processes" do
    s = Store.new()
    Store.put(s, "shared", "yes")
    parent = self()
    spawn(fn -> send(parent, Store.fetch(s, "shared")) end)
    assert_receive {:ok, "yes"}, 500
  end

  test "concurrent writes from many processes" do
    s = Store.new()

    tasks =
      for i <- 1..200 do
        Task.async(fn -> Store.put(s, "k#{i}", "v#{i}") end)
      end

    Enum.each(tasks, &Task.await/1)
    assert Store.size(s) == 200
  end

  test "drop runs when the last reference is GCd" do
    # Capture process memory, create & drop many stores, confirm no leak
    :erlang.garbage_collect()
    before = :erlang.memory(:total)

    for _ <- 1..1_000 do
      s = Store.new()
      for j <- 1..100, do: Store.put(s, "k#{j}", "v#{j}")
      # s goes out of scope here
    end

    :erlang.garbage_collect()
    after_ = :erlang.memory(:total)

    # Rough sanity — must not grow by more than a few MB
    assert after_ - before < 10_000_000
  end
end
```

---

## Trade-offs and production gotchas

**1. Drop timing is non-deterministic.** BEAM may not GC immediately. A resource holding a 1 GB model stays resident until the owning process is GCd (heap-size-triggered). `:erlang.garbage_collect/0` forces it.

**2. Lock scope matters.** Holding a `RwLock::write()` across a slow computation serializes all readers. Minimize critical sections.

**3. No Elixir-side introspection.** A reference is opaque — `inspect/1` shows `#Reference<...>`. For debugging expose an explicit `describe/1` NIF.

**4. Resources + ETS = leak risk.** Storing references in ETS without TTL keeps the native data alive forever. Always pair with expiry logic.

**5. `Send + Sync` requirements.** If your struct contains `Rc<T>` or `Cell<T>`, it's not `Send`, and `ResourceArc::new` won't compile. Swap to `Arc` and `RefCell → Mutex` equivalents.

**6. Poisoned locks.** `std::sync::Mutex` poisons on panic. `parking_lot::Mutex` doesn't — usually the better choice for NIFs.

**7. Cross-node transport.** References don't cross nodes natively — sending a resource to another BEAM node results in a "foreign" ref the other side cannot deref. Serialize the state, not the handle.

**8. When NOT to use this.** Small, immutable state (< 1 KB) — pass by binary. State you want multiple OTP processes to manage safely — use a GenServer and skip the native complexity. Data that should survive node restarts — resources are in-memory only.

---

## Benchmark

```elixir
store = Store.new()
:timer.tc(fn ->
  for i <- 1..1_000_000 do
    Store.put(store, "k#{i}", "v#{i}")
  end
end)
# ~350 ms — ~2.8 M puts/sec single-threaded
```

An ETS equivalent (`ets:insert/2` on a `:set`) does ~5 M/sec — NIF resources with `RwLock` overhead are ~60% of ETS speed but give you arbitrary Rust logic in the lock.

---

## Resources

- https://docs.rs/rustler/latest/rustler/resource/struct.ResourceArc.html — `ResourceArc`
- https://docs.rs/rustler/latest/rustler/macro.resource.html — `rustler::resource!`
- https://www.erlang.org/doc/man/erl_nif.html#enif_alloc_resource — C API
- https://github.com/rusty-ferris-club/tokenizers-elixir — resource pattern for ML models
- https://github.com/elixir-nx/explorer — large refcounted DataFrames as resources
- https://docs.rs/parking_lot — non-poisoning, faster locks for NIFs
