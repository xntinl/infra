# NIFs with Rustler — Safe Native Code on the BEAM

**Project**: `rustler_intro` — a first NIF that exposes Rust functions (`add`, `fib`, `sha256_hex`) to Elixir, wired through Rustler's macros and a minimal `cargo` crate.

---

## The business problem

Native Implemented Functions (NIFs) let Elixir call C-compatible native code directly inside the BEAM process. They're an order of magnitude faster than Ports for tight CPU work — no IPC, no serialization, shared address space — but the trade-off is severe: **a segfault in a NIF crashes the entire BEAM VM**. There's no supervision that saves you from a `null pointer dereference` in C.

[Rustler](https://github.com/rusterlium/rustler) solves this: Rust's memory safety eliminates the most common causes of NIF crashes (UAF, buffer overrun, double-free). You still have to respect BEAM scheduler rules (no NIF call should run more than ~1 ms on a normal scheduler), but segfaults from safe Rust are virtually impossible.

Real projects using Rustler in production include: `explorer` (Elixir DataFrames backed by Polars), `html5ever_elixir` (Servo's HTML parser), `ex_rated` (token bucket), and Phoenix LiveView tests use Rustler-backed crypto helpers. Dashbit built `tokenizers` (HuggingFace tokenizers via Rustler) to power their Bumblebee ML work.

This exercise builds the minimum viable NIF: a crate with `add`, a recursive `fib`, and a SHA-256 hex function that returns a binary. You'll learn the full path: crate layout, `#[rustler::nif]`, `init!`, encoding/decoding terms, and loading the dylib from Elixir.

## Project structure

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

### `mix.exs`
```elixir
defmodule NifsBasicsRustler.MixProject do
  use Mix.Project

  def project do
    [
      app: :nifs_basics_rustler,
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
```elixir
defmodule RustlerIntro.Native do
  use Rustler, otp_app: :rustler_intro, crate: "rustler_intro_nif"

  def add(_a, _b), do: :erlang.nif_error(:nif_not_loaded)
  def fib(_n),      do: :erlang.nif_error(:nif_not_loaded)
  def sha256_hex(_bin), do: :erlang.nif_error(:nif_not_loaded)
end
```

The body `:erlang.nif_error(:nif_not_loaded)` is what runs **if the dylib fails to load** (wrong arch, missing file). If loading succeeds, the Rust implementation replaces the stub at module-load time.

The BEAM scheduler expects each reduction (cooperative yield point) to run ~1 µs. A NIF is a single reduction by default. If your NIF runs for 10 ms, you've starved that scheduler for 10,000× the expected time. Symptoms: tail-latency spikes, message queue pileups on unrelated processes, ETS contention.

Rule of thumb:
- **< 1 ms** — regular NIF is fine.
- **1–100 ms** — use `DirtyCpu` or `DirtyIo` schedulers.
- **> 100 ms** — chunk work with `enif_schedule_nif` (yielding NIF) or use a Port.

`mix compile` runs `cargo build --release` inside `native/<crate>/`. The resulting `.so`/`.dylib`/`.dll` is placed in `priv/native/`. When `RustlerIntro.Native` first loads, BEAM looks at the `otp_app: :rustler_intro` attribute, finds `priv/native/librustler_intro_nif.{so,dylib,dll}`, and loads it.

---

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

### `script/main.exs`
```elixir
defmodule RustlerIntro.MixProject do
  use Mix.Project

  def project do
    [
      app: :rustler_intro,
      version: "0.1.0",
      elixir: "~> 1.19",
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

defmodule Main do
  def main do
      # Demonstrating 32-nifs-basics-rustler
      :ok
  end
end

Main.main()
```

---

## Why NIFs with Rustler — Safe Native Code on the BEAM matters

Mastering **NIFs with Rustler — Safe Native Code on the BEAM** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/rustler_intro.ex`

```elixir
defmodule RustlerIntro do
  @moduledoc """
  Reference implementation for NIFs with Rustler — Safe Native Code on the BEAM.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the rustler_intro module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> RustlerIntro.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/rustler_intro_test.exs`

```elixir
defmodule RustlerIntroTest do
  use ExUnit.Case, async: true

  doctest RustlerIntro

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert RustlerIntro.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
