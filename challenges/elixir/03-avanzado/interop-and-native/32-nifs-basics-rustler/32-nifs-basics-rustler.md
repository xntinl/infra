# NIFs with Rustler — Safe Native Code on the BEAM

**Project**: `rustler_intro` — a first NIF that exposes Rust functions (`add`, `fib`, `sha256_hex`) to Elixir, wired through Rustler's macros and a minimal `cargo` crate.

---

## Project context

Native Implemented Functions (NIFs) let Elixir call C-compatible native code directly inside the BEAM process. They're an order of magnitude faster than Ports for tight CPU work — no IPC, no serialization, shared address space — but the trade-off is severe: **a segfault in a NIF crashes the entire BEAM VM**. There's no supervision that saves you from a `null pointer dereference` in C.

[Rustler](https://github.com/rusterlium/rustler) solves this: Rust's memory safety eliminates the most common causes of NIF crashes (UAF, buffer overrun, double-free). You still have to respect BEAM scheduler rules (no NIF call should run more than ~1 ms on a normal scheduler), but segfaults from safe Rust are virtually impossible.

Real projects using Rustler in production include: `explorer` (Elixir DataFrames backed by Polars), `html5ever_elixir` (Servo's HTML parser), `ex_rated` (token bucket), and Phoenix LiveView tests use Rustler-backed crypto helpers. Dashbit built `tokenizers` (HuggingFace tokenizers via Rustler) to power their Bumblebee ML work.

This exercise builds the minimum viable NIF: a crate with `add`, a recursive `fib`, and a SHA-256 hex function that returns a binary. You'll learn the full path: crate layout, `#[rustler::nif]`, `init!`, encoding/decoding terms, and loading the dylib from Elixir.

```
rustler_intro/
├── lib/
│   └── rustler_intro/
│       └── native.ex               # Elixir side — NIF stubs
├── native/
│   └── rustler_intro_nif/
│       ├── Cargo.toml
│       └── src/lib.rs              # Rust NIF code
├── test/
│   └── rustler_intro_test.exs
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

### 1. What a NIF really is

A NIF is a C function registered with the BEAM scheduler. When Elixir calls `Native.add(1, 2)`, the BEAM looks up the function pointer, converts each Erlang term to a C type, jumps into native code, runs it, converts the return value back, and resumes.

```
BEAM scheduler thread
       │
       │   call Native.add(1, 2)
       ▼
    ERL_NIF_TERM add(env, argc, argv)
       │   decode 1, 2   (C)
       │   run addition
       │   encode result
       ▼
    returns ERL_NIF_TERM → BEAM resumes
```

The NIF runs **on the scheduler thread**. If it takes longer than a few milliseconds, it blocks that scheduler, hurting latency for unrelated processes. Rule: keep NIFs under 1 ms. If longer, use **dirty schedulers** for long-running operations or **yielding NIFs** to return control periodically.

### 2. Rustler's macros

```rust
#[rustler::nif]
fn add(a: i64, b: i64) -> i64 { a + b }

rustler::init!("Elixir.RustlerIntro.Native", [add]);
```

- `#[rustler::nif]` generates the C shim (term decode/encode, error handling).
- `rustler::init!` generates the module's NIF table that the BEAM loads.
- The module name must **exactly match** the Elixir module, prefixed with `Elixir.`.

### 3. Term encoding and decoding

Rustler provides `Encoder`/`Decoder` traits implemented for primitives, `String`, `Vec<T>`, `Binary`, and `#[derive(NifStruct)]` custom types.

| Elixir term | Rust type |
|---|---|
| `integer` | `i64`, `u64`, `i32` |
| `float` | `f64` |
| `atom` | `rustler::Atom` |
| `binary` | `rustler::Binary` or `&[u8]` (zero-copy) or `String` (copied UTF-8) |
| `list` | `Vec<T>` |
| `tuple` | `(T1, T2, ...)` |
| `map` | `HashMap<K, V>` or `#[derive(NifMap)]` |

**Zero-copy binaries** (`Binary`) are critical: decoding a 10 MB binary into `String` copies 10 MB. Using `Binary` passes a pointer.

### 4. The NIF stub in Elixir

```elixir
defmodule RustlerIntro.Native do
  use Rustler, otp_app: :rustler_intro, crate: "rustler_intro_nif"

  def add(_a, _b), do: :erlang.nif_error(:nif_not_loaded)
  def fib(_n),      do: :erlang.nif_error(:nif_not_loaded)
  def sha256_hex(_bin), do: :erlang.nif_error(:nif_not_loaded)
end
```

The body `:erlang.nif_error(:nif_not_loaded)` is what runs **if the dylib fails to load** (wrong arch, missing file). If loading succeeds, the Rust implementation replaces the stub at module-load time.

### 5. Scheduler time rule

The BEAM scheduler expects each reduction (cooperative yield point) to run ~1 µs. A NIF is a single reduction by default. If your NIF runs for 10 ms, you've starved that scheduler for 10,000× the expected time. Symptoms: tail-latency spikes, message queue pileups on unrelated processes, ETS contention.

Rule of thumb:
- **< 1 ms** — regular NIF is fine.
- **1–100 ms** — use `DirtyCpu` or `DirtyIo` schedulers.
- **> 100 ms** — chunk work with `enif_schedule_nif` (yielding NIF) or use a Port.

### 6. Build and load

`mix compile` runs `cargo build --release` inside `native/<crate>/`. The resulting `.so`/`.dylib`/`.dll` is placed in `priv/native/`. When `RustlerIntro.Native` first loads, BEAM looks at the `otp_app: :rustler_intro` attribute, finds `priv/native/librustler_intro_nif.{so,dylib,dll}`, and loads it.

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

### Step 1: `mix.exs`

**Objective**: Wire Rustler compiler plugin so `mix compile` auto-invokes cargo and lands optimized dylib in priv/native.

```elixir
defmodule RustlerIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :rustler_intro,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: deps(),
      compilers: [:rustler] ++ Mix.compilers(),
      rustler_crates: rustler_crates()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [{:rustler, "~> 0.32"}]
  end

  defp rustler_crates do
    [rustler_intro_nif: [path: "native/rustler_intro_nif", mode: :release]]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule RustlerIntro.Native do
  use Rustler, otp_app: :rustler_intro, crate: "rustler_intro_nif"

  def add(_a, _b), do: :erlang.nif_error(:nif_not_loaded)
  def fib(_n),      do: :erlang.nif_error(:nif_not_loaded)
  def sha256_hex(_bin), do: :erlang.nif_error(:nif_not_loaded)
end
```

The body `:erlang.nif_error(:nif_not_loaded)` is what runs **if the dylib fails to load** (wrong arch, missing file). If loading succeeds, the Rust implementation replaces the stub at module-load time.

### 5. Scheduler time rule

The BEAM scheduler expects each reduction (cooperative yield point) to run ~1 µs. A NIF is a single reduction by default. If your NIF runs for 10 ms, you've starved that scheduler for 10,000× the expected time. Symptoms: tail-latency spikes, message queue pileups on unrelated processes, ETS contention.

Rule of thumb:
- **< 1 ms** — regular NIF is fine.
- **1–100 ms** — use `DirtyCpu` or `DirtyIo` schedulers.
- **> 100 ms** — chunk work with `enif_schedule_nif` (yielding NIF) or use a Port.

### 6. Build and load

`mix compile` runs `cargo build --release` inside `native/<crate>/`. The resulting `.so`/`.dylib`/`.dll` is placed in `priv/native/`. When `RustlerIntro.Native` first loads, BEAM looks at the `otp_app: :rustler_intro` attribute, finds `priv/native/librustler_intro_nif.{so,dylib,dll}`, and loads it.

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

### Step 1: `mix.exs`

**Objective**: Wire Rustler compiler plugin so `mix compile` auto-invokes cargo and lands optimized dylib in priv/native.

```elixir
defmodule RustlerIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :rustler_intro,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: deps(),
      compilers: [:rustler] ++ Mix.compilers(),
      rustler_crates: rustler_crates()
    ]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [{:rustler, "~> 0.32"}]
  end

  defp rustler_crates do
    [rustler_intro_nif: [path: "native/rustler_intro_nif", mode: :release]]
  end
end
```

### Step 2: `native/rustler_intro_nif/Cargo.toml`

**Objective**: Declare cdylib target and pull cryptographic deps so the shared object exposes typed NIF functions to Elixir.

```toml
[package]
name = "rustler_intro_nif"
version = "0.1.0"
edition = "2021"

[lib]
name = "rustler_intro_nif"
crate-type = ["cdylib"]

[dependencies]
rustler = "0.32"
sha2 = "0.10"
hex  = "0.4"
```

### Step 3: `native/rustler_intro_nif/src/lib.rs`

**Objective**: Reject overflow-prone Fibonacci and zero-copy SHA256 output into OwnedBinary to avoid unnecessary heap copies.

```rust
use rustler::{Binary, Env, NifResult, Error};
use sha2::{Digest, Sha256};

#[rustler::nif]
fn add(a: i64, b: i64) -> i64 {
    a + b
}

#[rustler::nif]
fn fib(n: u32) -> NifResult<u64> {
    if n > 92 {
        return Err(Error::BadArg);
    }
    let (mut a, mut b): (u64, u64) = (0, 1);
    for _ in 0..n {
        let t = a.wrapping_add(b);
        a = b;
        b = t;
    }
    Ok(a)
}

#[rustler::nif]
fn sha256_hex<'a>(env: Env<'a>, data: Binary<'a>) -> Binary<'a> {
    let digest = Sha256::digest(data.as_slice());
    let hex = hex::encode(digest);
    let mut out = rustler::OwnedBinary::new(hex.len()).unwrap();
    out.as_mut_slice().copy_from_slice(hex.as_bytes());
    out.release(env)
}

rustler::init!("Elixir.RustlerIntro.Native", [add, fib, sha256_hex]);
```

### Step 4: `lib/rustler_intro/native.ex`

**Objective**: Provide typed stubs with `:nif_not_loaded` fallback so Dialyzer type-checks FFI calls even if compilation fails.

```elixir
defmodule RustlerIntro.Native do
  @moduledoc """
  Rust-backed primitives. All functions raise `:nif_not_loaded` if the shared
  object is missing — usually a sign the Rust crate failed to compile for the
  current target arch.
  """

  use Rustler, otp_app: :rustler_intro, crate: "rustler_intro_nif"

  @spec add(integer(), integer()) :: integer()
  def add(_a, _b), do: :erlang.nif_error(:nif_not_loaded)

  @spec fib(non_neg_integer()) :: non_neg_integer()
  def fib(_n), do: :erlang.nif_error(:nif_not_loaded)

  @spec sha256_hex(binary()) :: binary()
  def sha256_hex(_data), do: :erlang.nif_error(:nif_not_loaded)
end
```

### Step 5: `test/rustler_intro_test.exs`

**Objective**: Assert FFI correctness: Fibonacci bounds, overflow rejection, and deterministic SHA256 on edge cases and payloads.

```elixir
defmodule RustlerIntroTest do
  use ExUnit.Case, async: true

  alias RustlerIntro.Native

  describe "add/2" do
    test "positive integers" do
      assert Native.add(2, 3) == 5
    end

    test "negative integers" do
      assert Native.add(-10, 4) == -6
    end
  end

  describe "fib/1" do
    test "base cases" do
      assert Native.fib(0) == 0
      assert Native.fib(1) == 1
    end

    test "n=10 is 55" do
      assert Native.fib(10) == 55
    end

    test "rejects overflow" do
      assert_raise ArgumentError, fn -> Native.fib(200) end
    end
  end

  describe "sha256_hex/1" do
    test "known vector" do
      expected =
        "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

      assert Native.sha256_hex("hello") == expected
    end

    test "empty input" do
      assert Native.sha256_hex("") ==
               "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    end
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.


## Key Concepts: Native Code Integration and Performance Boundaries

Rustler is a framework for binding Rust functions as Elixir NIFs (Native Implemented Functions). When you call a NIF, the Erlang VM pauses that scheduler thread and executes Rust code directly—no message passing, no process switching. This is why NIFs are valuable for CPU-bound work: CPU-heavy algorithms in Rust can be 100x faster than equivalent Elixir.

The catch: NIFs are **not concurrent** on the same scheduler thread. If a NIF blocks (e.g., on I/O), it blocks the entire scheduler. The solution is dirty NIFs (`:dirty_cpu` or `:dirty_io`), which run on separate thread pools and don't block normal scheduling. Another gotcha: Rust code can panic, which crashes the entire BEAM VM. Proper error handling and testing are mandatory. Use NIFs sparingly: for crypto, compression, numerical compute. Use Ports for long-running external processes instead.


## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Advanced Considerations: NIF Isolation and Scheduler Integration

NIF calls run atomically on a scheduler thread, blocking all other processes on that scheduler until the function returns. For operations exceeding ~1 millisecond, this starvation becomes visible: heartbeat processes delay, ETS owner replies hang, supervision timeouts fire. The BEAM's dirty scheduler pool (8 CPU + 10 IO by default) isolates long NIFs from the main scheduler ring, but they're still a finite resource.

Understanding scheduler capacity is critical. Each dirty CPU scheduler can run ~1,000 100-microsecond operations per second, or ~5 100-millisecond operations. Beyond that, callers queue. A GenServer pool capping concurrency and applying backpressure prevents cascade failures: if the dirty pool saturates, reject new work immediately instead of queuing unboundedly.

Resource management inside NIFs differs from pure Elixir. A `Binary<'a>` is a borrow tied to the NIF call; it cannot escape to threads or be stored in resources. An `OwnedBinary` allocation isn't visible to BEAM's garbage collector, so memory limits must be enforced in the Elixir layer. Hybrid architectures (Port processes for I/O, NIFs for CPU work) offer better observability and failure isolation than trying to do everything in a single NIF crate.

---


## Deep Dive: Interop Patterns and Production Implications

Interop with native code (NIFs, ports, C extensions) introduces failure modes that pure Elixir code doesn't have: segfaults, memory leaks, deadlocks with the Erlang emulator. Testing interop requires separate test suites for the native layer and integration tests that exercise the boundary.

---

## Trade-offs and production gotchas

**1. Scheduler blocking.** A regular NIF over 1 ms degrades overall tail-latency. Benchmark with `:timer.tc/1` — if p99 > 1 ms, switch to a dirty scheduler.

**2. NIF reload pitfalls.** Hot code upgrades of a NIF module can fail if the dylib changed ABI. Rustler regenerates the shim, but OTP releases require the dylib in the release's `priv/` dir. Don't assume dev builds work in releases.

**3. Binary copies are silent killers.** Accepting `String` in Rust copies. Accept `Binary` for zero-copy and convert only if you truly need an owned `String`.

**4. Cross-compiling for releases.** If your dev box is Apple Silicon and prod is Linux x86_64, `mix release` won't produce the right dylib. Use `rustler_precompiled` (precompiled binaries uploaded to GitHub releases) — this is what `explorer`, `tokenizers` do.

**5. Panics turn into errors.** Rustler catches Rust panics and raises `ErlangError` in Elixir. You don't crash the VM — but you do lose the stack trace. Prefer `Result<T, Error>` over panicking.

**6. Atom creation.** Creating atoms dynamically in Rust leaks atom-table space — same rule as Elixir. Use `rustler::atoms! { ok, error, ... }` at compile time.

**7. Dirty scheduler saturation.** If 10 NIFs run simultaneously on 10 dirty CPU schedulers, the 11th queues behind them. Sizing: `erl +SDcpu N +SDio M` — defaults are usually fine but monitor `:erlang.statistics(:scheduler_wall_time)`.

**8. When NOT to use this.** Network I/O — use a Port or a proper Elixir library; you block a scheduler waiting on sockets. Anything longer than 100 ms — use a Port, keep the BEAM responsive. First-time native work — the tooling investment is real; `System.cmd/3` or a Port may be 90% as good at 10% the effort.

---

## Performance notes

Quick Benchee on `fib(30)`:

```elixir
Benchee.run(%{
  "native fib(30)"  => fn -> Native.fib(30) end,
  "elixir fib(30)"  => fn -> fib_elixir(30) end
})
# native:  ~15 ns   (iterative in Rust)
# elixir:  ~150 µs  (iterative in Elixir)
# ~10,000× faster
```

For SHA-256 of 1 MB:

```elixir
"native sha256" => fn -> Native.sha256_hex(large_binary) end
"crypto sha256" => fn -> :crypto.hash(:sha256, large_binary) |> Base.encode16(case: :lower) end
```

`:crypto` is usually within 10–20% because it's also a NIF (OpenSSL-backed). The lesson: if there's already an OTP `:crypto` equivalent, reach for it before writing a NIF.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- https://github.com/rusterlium/rustler — Rustler source + README
- https://docs.rs/rustler/latest/rustler/ — Rust-side API docs
- https://hexdocs.pm/rustler/ — Elixir-side reference
- https://www.erlang.org/doc/man/erl_nif.html — underlying `erl_nif.h` spec
- https://hexdocs.pm/rustler_precompiled/ — production precompiled-dylib workflow
- https://dashbit.co/blog/rustler-precompiled — Dashbit on shipping NIFs in releases
- https://github.com/elixir-nx/explorer — real-world Rustler NIF (Polars bindings)
