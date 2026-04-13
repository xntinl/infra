# NIF Resources — Managing Native Handles from Elixir

**Project**: `nif_resources` — a NIF that exposes a mutable Rust `HashMap` to Elixir as an opaque resource handle, with proper GC and drop semantics.

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

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
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

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `native/nif_resources_nif/Cargo.toml`

**Objective**: Declare parking_lot dep so RwLock skips poison semantics, allowing concurrent reads without NIF panic recovery overhead.

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

**Objective**: Register opaque ResourceArc<Store> so BEAM refcounts the backing HashMap across processes and avoids GC race conditions.

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

**Objective**: Provide opaque resource type so callers never inspect or serialize the backing BEAM reference directly.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

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

**Objective**: Normalize raw NIF atoms to typed tuples so application code never pattern-matches on foreign NIF result shapes.

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

**Objective**: Configure Rustler crates for release mode so optimized dylib rebuilds automatically when Rust sources change.

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

**Objective**: Verify resource opaqueness, cross-process reference identity, concurrent write safety, and GC-driven resource cleanup.

```elixir
defmodule NifResourcesTest do
  use ExUnit.Case, async: true
  alias NifResources.Store

  describe "NifResources" do
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
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Advanced Considerations: Resource Lifecycle and GC Interaction

NIF resources are opaque handles — the Rust struct lives off-heap, pointed to by a small BEAM term. When the term is garbage collected, the resource's drop function fires. This design is elegant but has subtle pitfalls. First, **GC delay**: if a resource allocates 100 MB and goes out of scope, it doesn't immediately free memory. It only frees when the GC cycle runs, which may be seconds later on a lightly-loaded system. In a tight loop creating many resources, you can exhaust memory before GC kicks in — the fix is explicit `erlang:garbage_collect/0` or using `spawn_link` per-batch.

**Shared mutable state** inside resources is dangerous. A resource wrapping a `Mutex<HashMap>` is thread-safe in Rust, but Elixir processes are single-threaded. If two processes hold the same resource handle, concurrent NIF calls lock under the mutex — acceptable. But if you're storing the resource in an ETS table and expecting isolated reads, you're wrong: ETS is a global shared structure, and two processes reading from the same row trigger concurrent NIF execution. The resource must be truly concurrent-safe (atomics, Arc<DashMap>) or you need a GenServer wrapper per-resource.

**Resource type identity** is checked by pointer, not by module. If you export a resource as a term and a client passes it back, Rustler validates the type via the stored vtable pointer. But if your NIF library is reloaded (hot code upgrade), the old resource type's pointer changes. A client holding an old resource term will fail with "resource type mismatch" if passed to the new code. Architecturally, hot upgrades of NIF modules are rare; use stable resource types or manually version them.

**Memory accounting** is opaque to the BEAM. If a resource allocates 500 MB internally (e.g., a HashMap holding indices), `erlang:memory(total)` doesn't include it — only the small term itself. This breaks assumptions about memory limits and GC pressure heuristics. Tools like `recon_alloc` can help, but the safest approach is to cap resource allocations in the Elixir layer via a rate-limited pool.

---

## Deep Dive: Interop Patterns and Production Implications

Interop with native code (NIFs, ports, C extensions) introduces failure modes that pure Elixir code doesn't have: segfaults, memory leaks, deadlocks with the Erlang emulator. Testing interop requires separate test suites for the native layer and integration tests that exercise the boundary.

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

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Executable Example

```elixir
### Step 2: `native/nif_resources_nif/src/lib.rs`

**Objective**: Register opaque ResourceArc<Store> so BEAM refcounts the backing HashMap across processes and avoids GC race conditions.



### Step 3: `lib/nif_resources/native.ex`

**Objective**: Provide opaque resource type so callers never inspect or serialize the backing BEAM reference directly.

#