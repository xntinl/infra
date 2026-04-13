# NIF Resource Handles and the Env Lifetime

**Project**: `cache_lmdb` — embed LMDB (memory-mapped key-value store) into the BEAM, exposing opaque cursor and transaction handles as NIF resources with correct destructor semantics.

## Project context

An analytics service needs a persistent cache that outlives process restarts and survives
node crashes. LMDB is a mature, zero-copy, single-writer-many-readers key-value store —
ideal for BEAM workloads because its read transactions never block writers. But LMDB
exposes C handles (`MDB_env`, `MDB_txn`, `MDB_cursor`) that must be created in a specific
order, used only on the thread that created them, and explicitly closed. Leaking a read
transaction is the classic LMDB bug: it blocks the freelist, and within minutes your
database doubles in size.

Exposing these handles safely from Elixir requires **NIF resources** — opaque reference
types with type tags, reference-counted lifetimes, and destructors called by the BEAM
GC when no Elixir term still references them. This is the single most subtle feature
of the NIF API.

```
cache_lmdb/
├── lib/
│   └── cache_lmdb/
│       ├── application.ex
│       └── db.ex
├── native/
│   └── cache_lmdb_nif/
│       ├── Cargo.toml
│       └── src/lib.rs
├── test/cache_lmdb/db_test.exs
└── mix.exs
```

## Why a NIF resource and not a raw pointer integer

Passing a raw pointer as an `u64` to Elixir works for one call, but:
1. Elixir has no way to tell the BEAM "this integer points to memory that needs freeing
   when nobody references it". Memory leaks are inevitable on process crashes.
2. Nothing stops a caller from inventing a fake pointer. Crash or data corruption follows.
3. Type-tagging is manual and error-prone.

A NIF resource solves all three:
- The BEAM tracks refcounts. When the last Elixir term referencing the resource is
  garbage-collected, the destructor runs — the C resource is freed, always.
- The BEAM enforces type tags: passing a `MDB_txn` resource where a `MDB_env` is expected
  raises `:badarg`.
- Type identity is established at `resource_type/0` registration.

## Why `Env<'a>` matters for resources

Every NIF call has an `Env<'a>` with a lifetime. Terms you return must share that lifetime.
Resources are slightly different: a `ResourceArc<T>` is owned by Rust but *shared with* the
BEAM via refcount. It has no lifetime constraint — you can store it in static data, clone
it, and return it from any later call. The BEAM's refcount keeps the underlying T alive as
long as either side holds a reference.

The dangerous mistake: extracting a `&T` or `Binary<'a>` from a resource and returning it
as a top-level term. The `&T` reference has the `Env<'a>` lifetime; after the call, it is
invalid, but the BEAM thinks it is a valid term. That is a UAF waiting to happen.

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
### 1. The resource type table

Each resource kind gets an identifier registered once at module load time. Rustler's
`resource!` macro does this for you.

### 2. The destructor

Called by the BEAM scheduler when refcount drops to zero. The destructor runs on a BEAM
scheduler thread — it must be fast (**< 1ms**) and non-blocking. If closing the resource
is slow (LMDB's `mdb_env_close` can be), spawn a dirty NIF or a cleanup thread.

### 3. Thread affinity

Some C APIs tie handles to the creating thread. LMDB write transactions must be committed
on the same thread that began them. NIFs are not thread-stable — a call may arrive on any
scheduler. For LMDB, we restrict writes to a single GenServer (serialized) and use the
`NoTls` flag so read transactions can migrate.

### 4. `ResourceArc::clone` is cheap

It bumps a refcount, no copy. Pass `ResourceArc<Env>` freely; the BEAM and Rust both own
the refcount logic.

## Design decisions

- **Option A — one resource per handle**: `EnvResource`, `TxnResource`, `CursorResource`.
  Pros: type-safe, BEAM enforces type tags.
  Cons: three resource registrations.
- **Option B — one tagged enum resource**: `Handle { Env(...), Txn(...), ... }`.
  Pros: one type. Cons: lose compile-time type distinctions.

→ **Option A**. The compile-time safety is worth the extra code.

- **Option A — keep the LMDB env as a singleton inside a GenServer**.
- **Option B — multiple envs, one per "database" abstraction**.

→ **Option A** for this exercise — LMDB discourages multiple envs per process.

## Implementation

### Dependencies (`mix.exs`)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule CacheLmdb.MixProject do
  use Mix.Project

  def project do
    [
      app: :cache_lmdb,
      version: "0.1.0",
      elixir: "~> 1.17",
      compilers: [:rustler] ++ Mix.compilers(),
      rustler_crates: [
        cache_lmdb_nif: [path: "native/cache_lmdb_nif", mode: :release]
      ],
      deps: [
        {:rustler, "~> 0.34"}
      ]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {CacheLmdb.Application, []}]
end
```

### Step 1: Cargo manifest (`native/cache_lmdb_nif/Cargo.toml`)

**Objective**: Use heed safe wrapper so NIF avoids raw FFI while exposing LMDB lifetimes to Rust's borrow checker.

```toml
[package]
name = "cache_lmdb_nif"
version = "0.1.0"
edition = "2021"

[lib]
name = "cache_lmdb_nif"
crate-type = ["cdylib"]

[dependencies]
rustler = "0.34"
heed = "0.20"  # safe Rust bindings to LMDB
```

### Step 2: Rust NIF with resources (`native/cache_lmdb_nif/src/lib.rs`)

**Objective**: Serialize writes via Mutex so concurrent schedulers never deadlock on LMDB's single-writer constraint.

```rust
use rustler::{Env, Error, NifResult, OwnedBinary, ResourceArc, Term};
use std::path::PathBuf;
use std::sync::Mutex;

mod atoms {
    rustler::atoms! { ok, error, not_found, invalid_path, io_error }
}

// ------------------------------------------------------------ Resource types

pub struct EnvResource {
    env: heed::Env,
    db:  heed::Database<heed::types::Bytes, heed::types::Bytes>,
    // Mutex serializes writes. LMDB supports one writer at a time anyway; a
    // mutex makes the contention explicit and avoids the classic "two BEAM
    // schedulers both try to begin_rw_txn" deadlock.
    write_lock: Mutex<()>,
}

impl Drop for EnvResource {
    fn drop(&mut self) {
        // heed::Env's own Drop closes the mdb_env cleanly; nothing extra needed.
        // This comment documents the invariant: destructor is fast and non-blocking.
    }
}

// ------------------------------------------------------------- NIF functions

#[rustler::nif]
fn open(path: String, map_size_mb: usize) -> NifResult<ResourceArc<EnvResource>> {
    let path_buf = PathBuf::from(&path);
    std::fs::create_dir_all(&path_buf)
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;

    let env = unsafe {
        heed::EnvOpenOptions::new()
            .map_size(map_size_mb * 1024 * 1024)
            .max_dbs(1)
            .open(&path_buf)
            .map_err(|_| Error::Term(Box::new(atoms::io_error())))?
    };

    let mut wtxn = env.write_txn()
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    let db: heed::Database<heed::types::Bytes, heed::types::Bytes> =
        env.create_database(&mut wtxn, Some("cache"))
            .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    wtxn.commit()
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;

    Ok(ResourceArc::new(EnvResource {
        env,
        db,
        write_lock: Mutex::new(()),
    }))
}

#[rustler::nif]
fn put(handle: ResourceArc<EnvResource>, key: Vec<u8>, value: Vec<u8>) -> NifResult<rustler::Atom> {
    let _guard = handle.write_lock.lock().unwrap();
    let mut wtxn = handle.env.write_txn()
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    handle.db.put(&mut wtxn, &key, &value)
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    wtxn.commit()
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    Ok(atoms::ok())
}

#[rustler::nif]
fn get<'a>(env: Env<'a>, handle: ResourceArc<EnvResource>, key: Vec<u8>) -> NifResult<Term<'a>> {
    let rtxn = handle.env.read_txn()
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    match handle.db.get(&rtxn, &key) {
        Ok(Some(bytes)) => {
            let mut bin = OwnedBinary::new(bytes.len()).unwrap();
            bin.as_mut_slice().copy_from_slice(bytes);
            // Binary carries the lifetime of env — returning it here is safe.
            let term: Term<'a> = bin.release(env).to_term(env);
            Ok(rustler::Encoder::encode(
                &(atoms::ok(), term),
                env,
            ))
        }
        Ok(None) => Ok(rustler::Encoder::encode(&atoms::not_found(), env)),
        Err(_) => Err(Error::Term(Box::new(atoms::io_error()))),
    }
}

#[rustler::nif]
fn delete(handle: ResourceArc<EnvResource>, key: Vec<u8>) -> NifResult<rustler::Atom> {
    let _guard = handle.write_lock.lock().unwrap();
    let mut wtxn = handle.env.write_txn()
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    handle.db.delete(&mut wtxn, &key)
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    wtxn.commit()
        .map_err(|_| Error::Term(Box::new(atoms::io_error())))?;
    Ok(atoms::ok())
}

// ------------------------------------------------------------------- Register

fn load(env: Env, _info: Term) -> bool {
    rustler::resource!(EnvResource, env);
    true
}

rustler::init!(
    "Elixir.CacheLmdb.DB",
    [open, put, get, delete],
    load = load
);
```

### Step 3: Elixir wrapper (`lib/cache_lmdb/db.ex`)

**Objective**: Provide opaque handle so LMDB env closes automatically when BEAM GC drops all references.

```elixir
defmodule CacheLmdb.DB do
  @moduledoc """
  Opaque handle to an embedded LMDB instance.
  The handle (`t()`) is a NIF resource: its lifetime is managed by the BEAM GC.
  When no Elixir term references it, the underlying LMDB env is closed.
  """
  use Rustler, otp_app: :cache_lmdb, crate: :cache_lmdb_nif

  @opaque t :: reference()

  def open(_path, _map_size_mb), do: :erlang.nif_error(:nif_not_loaded)
  def put(_handle, _key, _value), do: :erlang.nif_error(:nif_not_loaded)
  def get(_handle, _key), do: :erlang.nif_error(:nif_not_loaded)
  def delete(_handle, _key), do: :erlang.nif_error(:nif_not_loaded)
end
```

### Step 4: Application wrapper

**Objective**: Minimal supervision since LMDB handles are owned by callers, not a central GenServer.

```elixir
defmodule CacheLmdb.Application do
  use Application
  def start(_, _), do: Supervisor.start_link([], strategy: :one_for_one, name: __MODULE__)
end
```

## Why this works

```
┌─ Elixir process A ──┐       ┌─ Elixir process B ──┐
│ handle = DB.open    │       │ handle ← shared via │
│ (resource ref #123) │       │ send/2 or ETS       │
└──────────┬──────────┘       └──────────┬──────────┘
           │                              │
           └──── both refs bump the refcount of EnvResource in Rust ───┐
                                                                       │
Rust:  ResourceArc<EnvResource>  — refcount = 2                       │
                                                                       │
When both A and B's references go out of scope and GC runs,           │
refcount → 0 → Drop for EnvResource → heed::Env closes mdb_env.       │
                                                                       ▼
                                                          Memory-safe, deterministic close.
```

- **Refcount lifetime**: the BEAM GC and Rust's refcount together guarantee the LMDB env
  is open for exactly as long as someone references it. Never leaks, never double-freed.
- **Type tagging**: passing a random reference to `DB.put/3` raises `:badarg` because
  Rustler checks the resource type at decode time.
- **Write serialization**: a Rust `Mutex` around `write_txn`; concurrent readers are
  unserialized (LMDB's design).

## Tests (`test/cache_lmdb/db_test.exs`)

```elixir
defmodule CacheLmdb.DBTest do
  use ExUnit.Case, async: true
  alias CacheLmdb.DB

  setup do
    path = Path.join(System.tmp_dir!(), "cache_lmdb_test_#{System.unique_integer([:positive])}")
    on_exit(fn -> File.rm_rf!(path) end)
    handle = DB.open(path, 16)
    {:ok, handle: handle}
  end

  describe "put/get roundtrip" do
    test "retrieves stored values", %{handle: h} do
      :ok = DB.put(h, "key1", "value1")
      assert {:ok, "value1"} = DB.get(h, "key1")
    end

    test "missing key returns :not_found", %{handle: h} do
      assert :not_found = DB.get(h, "absent")
    end

    test "overwrite replaces value", %{handle: h} do
      :ok = DB.put(h, "k", "v1")
      :ok = DB.put(h, "k", "v2")
      assert {:ok, "v2"} = DB.get(h, "k")
    end

    test "delete removes the key", %{handle: h} do
      :ok = DB.put(h, "k", "v")
      :ok = DB.delete(h, "k")
      assert :not_found = DB.get(h, "k")
    end
  end

  describe "handle semantics" do
    test "handle is a reference", %{handle: h} do
      assert is_reference(h)
    end

    test "passing wrong term raises badarg" do
      assert_raise ArgumentError, fn -> DB.put(make_ref(), "k", "v") end
    end
  end

  describe "concurrent access" do
    test "100 concurrent reads on the same handle", %{handle: h} do
      :ok = DB.put(h, "shared", "value")

      tasks =
        for _ <- 1..100 do
          Task.async(fn -> DB.get(h, "shared") end)
        end

      assert Enum.all?(Task.await_many(tasks), &match?({:ok, "value"}, &1))
    end
  end
end
```

## Benchmark

```elixir
handle = CacheLmdb.DB.open("/tmp/bench_lmdb", 64)
:ok = CacheLmdb.DB.put(handle, "k", "v")

{us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: CacheLmdb.DB.get(handle, "k")
end)
IO.puts("Avg: #{us / 10_000} µs per op")
```

Target: **<20 µs per op** on modern hardware.

## Advanced Considerations: NIF Isolation and Scheduler Integration

NIF calls run atomically on a scheduler thread, blocking all other processes on that scheduler until the function returns. For operations exceeding ~1 millisecond, this starvation becomes visible: heartbeat processes delay, ETS owner replies hang, supervision timeouts fire. The BEAM's dirty scheduler pool (8 CPU + 10 IO by default) isolates long NIFs from the main scheduler ring, but they're still a finite resource.

Understanding scheduler capacity is critical. Each dirty CPU scheduler can run ~1,000 100-microsecond operations per second, or ~5 100-millisecond operations. Beyond that, callers queue. A GenServer pool capping concurrency and applying backpressure prevents cascade failures: if the dirty pool saturates, reject new work immediately instead of queuing unboundedly.

Resource management inside NIFs differs from pure Elixir. A `Binary<'a>` is a borrow tied to the NIF call; it cannot escape to threads or be stored in resources. An `OwnedBinary` allocation isn't visible to BEAM's garbage collector, so memory limits must be enforced in the Elixir layer. Hybrid architectures (Port processes for I/O, NIFs for CPU work) offer better observability and failure isolation than trying to do everything in a single NIF crate.

---


## Deep Dive: Interop Patterns and Production Implications

Interop with native code (NIFs, ports, C extensions) introduces failure modes that pure Elixir code doesn't have: segfaults, memory leaks, deadlocks with the Erlang emulator. Testing interop requires separate test suites for the native layer and integration tests that exercise the boundary.

---

## Trade-offs and production gotchas

**1. Destructor must be fast.** If your `Drop` blocks on I/O, the BEAM scheduler stalls.
LMDB's `mdb_env_close` is fast for small envs; for multi-GB envs with pending writes,
consider routing closure through a dirty NIF.

**2. Do not leak references into process dictionary.** A long-lived `:persistent_term` or
`:ets` entry holding a resource prevents GC. Intentional for caches; unintentional in
request-handling code leads to unclosed handles accumulating.

**3. Returning `&[u8]` from inside the resource is UB.** Always copy into `OwnedBinary`
for return. The borrow checker catches this in Rust; in C it is the #1 resource-related
segfault cause.

**4. Mutex poisoning.** If a panic occurs while holding `write_lock`, the Mutex is
poisoned. Subsequent calls `.lock().unwrap()` will panic. Either use `.lock().unwrap_or_else`
to recover or treat it as unrecoverable and return `{:error, :poisoned}`.

**5. Map size cannot shrink.** LMDB's `map_size` is the max; once a file grows, it stays.
Choose a size that fits expected lifetime, or implement compaction via `mdb_env_copy2`.

**6. When NOT to use NIF resources.** For objects that need to survive BEAM restarts, use
a process + disk persistence instead. Resources are for in-memory handles only.

## Reflection

The `EnvResource` holds a single database. Real LMDB users open multiple named databases
inside one env. What would change in the resource shape — a separate `DbResource` tied by
a lifetime/parent relationship to `EnvResource`, versus a `HashMap<String, Database>`
inside `EnvResource` — and which choice is safer under the BEAM GC model where refcount
order is not deterministic?

## Executable Example

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end

defmodule CacheLmdb.MixProject do
  end
  use Mix.Project

  def project do
    [
      app: :cache_lmdb,
      version: "0.1.0",
      elixir: "~> 1.17",
      compilers: [:rustler] ++ Mix.compilers(),
      rustler_crates: [
        cache_lmdb_nif: [path: "native/cache_lmdb_nif", mode: :release]
      ],
      deps: [
        {:rustler, "~> 0.34"}
      ]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {CacheLmdb.Application, []}]
end


### Step 2: Rust NIF with resources (`native/cache_lmdb_nif/src/lib.rs`)

**Objective**: Serialize writes via Mutex so concurrent schedulers never deadlock on LMDB's single-writer constraint.



### Step 3: Elixir wrapper (`lib/cache_lmdb/db.ex`)

**Objective**: Provide opaque handle so LMDB env closes automatically when BEAM GC drops all references.



### Step 4: Application wrapper

**Objective**: Minimal supervision since LMDB handles are owned by callers, not a central GenServer.

defmodule Main do
  def main do
      # Demonstrating 325-nif-resource-env
      :ok
  end
end

Main.main()
```
