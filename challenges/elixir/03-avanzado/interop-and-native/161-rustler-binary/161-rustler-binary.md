# Rustler Binaries — Zero-Copy Term Handling

**Project**: `rustler_binary` — a NIF crate that processes large binaries (XOR mask, byte histogram, UTF-8 validation) without copying bytes between BEAM and Rust.

---

## Project context

The moment a NIF author graduates from toy integers to real workloads, binaries show up. A video frame is 8 MB. A request body is 2 MB. A database row is 200 KB. Decoding each term to an owned `String` or `Vec<u8>` copies every byte — twice if the NIF also returns a new binary. For a 10 MB input that's 40 MB of allocation per call, plus GC pressure.

Rustler supports **zero-copy binaries**: the `Binary<'a>` type is a borrow over the BEAM's binary heap with a lifetime tied to the NIF invocation. `OwnedBinary` is an allocation that the NIF writes into and releases back to BEAM without an intermediate copy. Tools like `tokenizers`, `html5ever_elixir`, and `explorer` move gigabytes per second through NIFs by treating binaries as slices, never as `String`.

The tricky part is lifetime management: a `Binary<'a>` must not outlive the NIF call. You cannot store it in a NIF resource or send it across threads without first copying. And "sub-binaries" (slices of a larger binary) keep the parent alive in BEAM's garbage collector — a common memory leak pattern in applications that cache sub-slices of giant binaries.

```
rustler_binary/
├── lib/rustler_binary/native.ex
├── native/rustler_binary_nif/
│   ├── Cargo.toml
│   └── src/lib.rs
├── test/rustler_binary_test.exs
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

```elixir
big = :crypto.strong_rand_bytes(100_000_000)   # 100 MB
<<_header::binary-size(100), rest::binary>> = big
# 'rest' is a sub-binary referencing 'big' — 'big' cannot be GC'd
```

As long as `rest` is reachable, the 100 MB parent stays alive. Rustler NIFs that return sub-binaries of the input propagate this — sometimes a NIF that "returns 100 bytes" actually pins a 100 MB object. Fix: copy via `OwnedBinary` if the result should be independent.

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

### Step 3: `lib/rustler_binary/native.ex`

```elixir
defmodule RustlerBinary.Native do
  @moduledoc "Zero-copy binary operations."

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

### Step 4: `mix.exs`

```elixir
defmodule RustlerBinary.MixProject do
  use Mix.Project

  def project do
    [
      app: :rustler_binary,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: [{:rustler, "~> 0.32"}, {:benchee, "~> 1.3", only: :dev}],
      rustler_crates: [rustler_binary_nif: [path: "native/rustler_binary_nif", mode: :release]]
    ]
  end

  def application, do: [extra_applications: [:logger]]
end
```

### Step 5: `test/rustler_binary_test.exs`

```elixir
defmodule RustlerBinaryTest do
  use ExUnit.Case, async: true
  alias RustlerBinary.Native

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
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

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

## Resources

- https://docs.rs/rustler/latest/rustler/struct.Binary.html — `Binary` API
- https://docs.rs/rustler/latest/rustler/struct.OwnedBinary.html — `OwnedBinary`
- https://www.erlang.org/doc/man/erl_nif.html#enif_inspect_binary — underlying C API
- https://www.erlang.org/doc/efficiency_guide/binaryhandling.html — BEAM binary internals
- https://github.com/elixir-nx/explorer — Rustler + big binaries in production
- https://ferd.ca/erlang-s-big-deletions.html — sub-binary leak war story
