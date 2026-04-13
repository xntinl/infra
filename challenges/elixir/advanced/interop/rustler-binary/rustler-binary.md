# Rustler Binaries — Zero-Copy Term Handling

**Project**: `rustler_binary` — a NIF crate that processes large binaries (XOR mask, byte histogram, UTF-8 validation) without copying bytes between BEAM and Rust.

---

## The business problem

The moment a NIF author graduates from toy integers to real workloads, binaries show up. A video frame is 8 MB. A request body is 2 MB. A database row is 200 KB. Decoding each term to an owned `String` or `Vec<u8>` copies every byte — twice if the NIF also returns a new binary. For a 10 MB input that's 40 MB of allocation per call, plus GC pressure.

Rustler supports **zero-copy binaries**: the `Binary<'a>` type is a borrow over the BEAM's binary heap with a lifetime tied to the NIF invocation. `OwnedBinary` is an allocation that the NIF writes into and releases back to BEAM without an intermediate copy. Tools like `tokenizers`, `html5ever_elixir`, and `explorer` move gigabytes per second through NIFs by treating binaries as slices, never as `String`.

The tricky part is lifetime management: a `Binary<'a>` must not outlive the NIF call. You cannot store it in a NIF resource or send it across threads without first copying. And "sub-binaries" (slices of a larger binary) keep the parent alive in BEAM's garbage collector — a common memory leak pattern in applications that cache sub-slices of giant binaries.

## Project structure

```
rustler_binary/
├── lib/rustler_binary/native.ex
├── native/rustler_binary_nif/
│   ├── Cargo.toml
│   └── src/lib.rs
├── test/rustler_binary_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `native/rustler_binary_nif/Cargo.toml`

**Objective**: Declare cdylib target so BEAM dynamically loads the Rust-compiled shared object at module init.

```toml
[package]
name = "rustler_binary_nif"
version = "0.1.0"
edition = "2021"

[lib]
name = "rustler_binary_nif"
crate-type = ["cdylib"]

[dependencies]
rustler = "0.32"
```

### Step 2: `native/rustler_binary_nif/src/lib.rs`

**Objective**: Zero-copy borrow inputs via Binary<'a> and return OwnedBinary results to move multi-MB payloads without heap copies.

```rust
use rustler::{Binary, Env, OwnedBinary, NifResult, Error};

#[rustler::nif]
fn byte_len(data: Binary) -> usize {
    data.as_slice().len()
}

#[rustler::nif]
fn xor_mask<'a>(env: Env<'a>, data: Binary<'a>, mask: Binary<'a>) -> NifResult<Binary<'a>> {
    let src = data.as_slice();
    let m = mask.as_slice();

    if m.is_empty() {
        return Err(Error::BadArg);
    }

    let mut out = OwnedBinary::new(src.len()).ok_or(Error::Atom("alloc_failed"))?;
    let dst = out.as_mut_slice();

    for (i, &b) in src.iter().enumerate() {
        dst[i] = b ^ m[i % m.len()];
    }

    Ok(out.release(env))
}

#[rustler::nif]
fn histogram(data: Binary) -> [u64; 256] {
    let mut counts = [0u64; 256];
    for &b in data.as_slice() {
        counts[b as usize] += 1;
    }
    counts
}

#[rustler::nif]
fn is_valid_utf8(data: Binary) -> bool {
    std::str::from_utf8(data.as_slice()).is_ok()
}

rustler::init!(
    "Elixir.RustlerBinary.Native",
    [byte_len, xor_mask, histogram, is_valid_utf8]
);
```

### `lib/rustler_binary.ex`

```elixir
defmodule RustlerBinary do
  @moduledoc """
  Rustler Binaries — Zero-Copy Term Handling.

  Alternatives considered and discarded:.
  """
end
```

### `lib/rustler_binary/native.ex`

**Objective**: Provide typed stubs with `:nif_not_loaded` so Dialyzer validates zero-copy binary ops even pre-compilation.

```elixir
defmodule RustlerBinary.Native do
  @moduledoc """
  Ejercicio: Rustler Binaries — Zero-Copy Term Handling.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use Rustler, otp_app: :rustler_binary, crate: "rustler_binary_nif"

  @spec byte_len(binary()) :: non_neg_integer()
  def byte_len(_data), do: :erlang.nif_error(:nif_not_loaded)

  @spec xor_mask(binary(), binary()) :: binary()
  def xor_mask(_data, _mask), do: :erlang.nif_error(:nif_not_loaded)

  @spec histogram(binary()) :: [non_neg_integer()]
  def histogram(_data), do: :erlang.nif_error(:nif_not_loaded)

  @spec is_valid_utf8(binary()) :: boolean()
  def is_valid_utf8(_data), do: :erlang.nif_error(:nif_not_loaded)
end
```

### Step 5: `test/rustler_binary_test.exs`

**Objective**: Assert round-trip XOR symmetry and empty-mask rejection to catch regressions in lifetime handling and `BadArg` propagation across the FFI boundary.

```elixir
defmodule RustlerBinaryTest do
  use ExUnit.Case, async: true
  doctest RustlerBinary.Native
  alias RustlerBinary.Native

  describe "RustlerBinary" do
    test "byte_len on large binary" do
      bin = :crypto.strong_rand_bytes(10_000_000)
      assert Native.byte_len(bin) == 10_000_000
    end

    test "xor_mask is its own inverse" do
      data = "the quick brown fox"
      mask = "key"
      masked = Native.xor_mask(data, mask)
      assert Native.xor_mask(masked, mask) == data
    end

    test "xor_mask rejects empty mask" do
      assert_raise ArgumentError, fn -> Native.xor_mask("hello", "") end
    end

    test "histogram counts every byte" do
      counts = Native.histogram(<<0, 0, 1, 1, 1, 255>>)
      assert Enum.at(counts, 0) == 2
      assert Enum.at(counts, 1) == 3
      assert Enum.at(counts, 255) == 1
      assert Enum.sum(counts) == 6
    end

    test "utf8 validation" do
      assert Native.is_valid_utf8("hello")
      assert Native.is_valid_utf8("日本語")
      refute Native.is_valid_utf8(<<0xFF, 0xFE>>)
    end
  end
end
```

---

### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Advanced Considerations: NIF Guards and Scheduler Isolation

When binary processing moves into Rustler, the safety model shifts. A NIF call blocks its scheduler thread until the Rust code returns. On systems with 16 scheduler threads, a 100 ms NIF leaves 15 schedulers running — but that one thread starves any BEAM process assigned to it. Heavy NIFs demand explicit thought about time budgets and escape hatches.

**Scheduler directives** control this: by default, NIFs run on "normal" schedulers. Adding the `dirty_cpu` attribute offloads to a separate thread pool (configurable via `+SDcpu` flag), freeing the main scheduler for interactive work. In the benchmark above, `xor_mask` on 100 MB takes ~50 ms — borderline. For production, measure with your actual workload; 5–10 ms on normal schedulers is safe, 100+ ms demands dirty-cpu.

**Lifetime guards** also matter. A `Binary<'a>` holds a reference into the BEAM's heap. If the NIF yields to async code or a background thread, the BEAM may trigger garbage collection, invalidating the pointer. Rustler's type system enforces that `Binary` cannot escape the call boundary, but vigilance is needed when using `enif_*` C APIs directly or calling native libraries that spawn threads.

**Port alternatives** exist when you need more isolation. Instead of NIFs, a separate OS process (via `Port.open/2` with `{:spawn, cmd}`) communicates over stdin/stdout, incurring serialization overhead but gaining true process isolation. A hung or crashing external process cannot freeze the BEAM VM. For untrusted code, network services, or long-running tasks, a Port is safer than a NIF, even at 10–100× latency cost.

**Resource cleanup** is implicit in NIFs but requires discipline. An `OwnedBinary` that allocates 1 GB but crashes before `release(env)` leaks memory back to BEAM's allocator. Using `OwnedBinary` as a guard (wrapped in a custom struct with Drop impl or Rust's RAII) helps, but Elixir developers must reason in binary ownership terms. Contrast this with pure Elixir, where the garbage collector owns all memory.

---

## Deep Dive: Interop Patterns and Production Implications

Interop with native code (NIFs, ports, C extensions) introduces failure modes that pure Elixir code doesn't have: segfaults, memory leaks, deadlocks with the Erlang emulator. Testing interop requires separate test suites for the native layer and integration tests that exercise the boundary.

---

## Trade-offs and production gotchas

**1. `Binary` lifetime is per-call.** You cannot stash `Binary<'a>` in a NIF resource or pass across threads. If you need to keep bytes, copy them into an `OwnedBinary` first.

**2. `OwnedBinary::new` can fail.** On huge allocations it returns `None`. Always handle it; don't `.unwrap()` in production.

**3. Sub-binary keeps parent alive.** Returning a slice of the input binary via Rustler (if you do it naively with raw terms) pins the parent. For independence, copy.

**4. Histogram returns a 256-element list.** That list materializes 256 BEAM integers on every call — ~4–8 KB. For hot loops, consider returning a `Binary` of 256×8 bytes and decoding in Elixir.

**5. UTF-8 validation is not free.** For 100 MB of data, validation costs ~300 ms. If input is trusted, skip it.

**6. SIMD is tempting but tricky.** Rust's `std::simd` is nightly-only; `packed_simd` adds a huge dep. For byte scans, LLVM autovectorizes tight loops well — benchmark before reaching for intrinsics.

**7. Scheduler time on big inputs.** `xor_mask` on 100 MB takes 50–100 ms. That's dirty-scheduler territory.

**8. When NOT to use this.** Small (< 10 KB) or infrequent binaries — the NIF call overhead (~100 ns) makes pure Elixir fine. Operations already available in `:crypto` or `:erlang.binary_*` — they're NIFs too.

---

## Benchmark

```elixir
data = :crypto.strong_rand_bytes(1_000_000)

Benchee.run(%{
  "native xor_mask"  => fn -> RustlerBinary.Native.xor_mask(data, "key") end,
  "elixir xor_mask"  => fn ->
    for <<b <- data>>, into: <<>>, do: <<Bitwise.bxor(b, ?k)>>
  end
})
# Rust NIF:  ~1 ms
# Elixir:    ~200 ms (comprehension + binary append)
```

~200× speedup for a trivial byte loop. The gap widens as inputs grow.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

### `script/main.exs`
```elixir

**Objective**: Zero-copy borrow inputs via Binary<'a> and return OwnedBinary results to move multi-MB payloads without heap copies.

**Objective**: Provide typed stubs with `:nif_not_loaded` so Dialyzer validates zero-copy binary ops even pre-compilation.

### `mix.exs`
**Objective**: Register the `rustler_crates` entry in release mode so `mix compile` triggers `cargo build --release` and caches the artifact under `_build`.

**Objective**: Assert round-trip XOR symmetry and empty-mask rejection to catch regressions in lifetime handling and `BadArg` propagation across the FFI boundary.

defmodule Main do
  def main do
      # Demonstrating 161-rustler-binary
      :ok
  end
end

Main.main()
```

---

## Why Rustler Binaries — Zero-Copy Term Handling matters

Mastering **Rustler Binaries — Zero-Copy Term Handling** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/rustler_binary_test.exs`

```elixir
defmodule RustlerBinaryTest do
  use ExUnit.Case, async: true

  doctest RustlerBinary

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert RustlerBinary.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. BEAM binary anatomy

```
 Heap binary (≤ 64 bytes)      Refc binary (> 64 bytes, shared)
 ┌────────────┐                ┌──────────────┐     ┌────────────────┐
 │ tag:binary │                │ tag:ProcBin  │────▶│  refc heap     │
 │ size: 20   │                │ size: 1048576│     │  1 MB of data  │
 │ data[...]  │                │ ptr  ────────┼────▶│  refcount: 2   │
 └────────────┘                └──────────────┘     └────────────────┘
                                                    shared across procs
```

Small binaries live inline on the process heap. Large ones (> 64 bytes) live in a **refcounted heap** shared across processes. A `Binary<'a>` in Rust is a pointer + length into one of these.

### 2. Rustler's `Binary`, `OwnedBinary`, `NewBinary`

| Type | Purpose | Copies? |
|---|---|---|
| `Binary<'a>` | Read-only borrow of an input term | No |
| `&[u8]` (from `data.as_slice()`) | Slice view | No |
| `OwnedBinary` | Allocate-then-write scratch buffer | Internal only |
| `NewBinary<'a>` (0.30+) | Fast small-binary allocator | No |
| `String` (decoded from term) | Owned UTF-8 copy | Yes |

For input: decode as `Binary<'a>`. For output: `OwnedBinary::new(len)`, fill via `as_mut_slice`, `release(env)`.

### 3. Sub-binary GC trap

### 4. UTF-8 validation cost

`String::from_utf8(bytes.to_vec())` does: **copy + validate**. For inputs that are known UTF-8 (e.g., JSON from a trusted source), use `std::str::from_utf8_unchecked` **only if** you've validated elsewhere. Otherwise the copy + validate cost is roughly 3 ns/byte on modern x86 — noticeable at GB scale.

### 5. `enif_inspect_binary` vs `enif_make_binary`

Low-level BEAM C API:
- `enif_inspect_binary` — get a pointer + length to an existing term (no copy).
- `enif_alloc_binary` / `enif_make_binary` — allocate a new binary (one copy from your buffer).

Rustler wraps these behind `Binary` and `OwnedBinary`.

### 6. Returning iodata

If you generate output in chunks (compression, encoding), build a `Vec<Binary>` or a tuple list and let Elixir flatten with `IO.iodata_to_binary/1`. Avoids one big `OwnedBinary::new` allocation.

---
