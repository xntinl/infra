# Rustler Dirty Schedulers — Long-Running NIFs Without Starving BEAM

**Project**: `rustler_dirty` — a NIF that runs CPU-heavy work (Argon2id password hashing, prime sieve) on dirty CPU schedulers, plus an I/O NIF on dirty IO schedulers.

**Difficulty**: ★★★★☆

**Estimated time**: 3–6 hours

---

## Project context

The BEAM's preemptive scheduler is the feature that makes Elixir systems stay responsive under load — every function call costs "reductions", and at ~2000 reductions the current process is preempted to let others run. NIFs break this contract: they run atomically, as a single reduction. A NIF that takes 50 ms blocks its scheduler for 50 ms — and with 8 schedulers on an 8-core box, 8 such NIFs in parallel block the entire VM.

This is why naive password hashing in a NIF ruins servers: bcrypt at cost 12 takes ~200 ms, blocking a scheduler, which delays heartbeats, ETS owner replies, and supervisor timeouts. The BEAM added **dirty schedulers** specifically for this use case: a separate thread pool (8 CPU + 10 IO by default) that runs long NIFs without touching the main scheduler pool.

Rustler exposes dirty schedulers via a single attribute: `#[rustler::nif(schedule = "DirtyCpu")]`. In production, `argon2_elixir`, `bcrypt_elixir`, `jason_native`, and `explorer`'s heavy Polars operations all run on dirty schedulers. This exercise builds `rustler_dirty` — a small set of NIFs that let you see scheduler starvation vs dirty scheduling with your own eyes.

```
rustler_dirty/
├── lib/rustler_dirty/native.ex
├── native/rustler_dirty_nif/
│   ├── Cargo.toml
│   └── src/lib.rs
├── test/rustler_dirty_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Scheduler types

```
BEAM process
  ├── Scheduler 1 (online)  ──▶ runs BEAM procs + regular NIFs
  ├── Scheduler 2 (online)  ──▶ runs BEAM procs + regular NIFs
  ├── ...
  ├── Scheduler N (online)
  ├── DirtyCpu 1 ─────────▶ runs CPU-bound NIFs (flagged DirtyCpu)
  ├── ...
  ├── DirtyCpu 8
  ├── DirtyIo 1 ─────────▶ runs blocking-I/O NIFs (flagged DirtyIo)
  └── DirtyIo 10
```

Defaults: `erl +SDcpu #logical_cores +SDio 10`. Dirty schedulers are OS threads separate from the scheduler pool — a blocked dirty scheduler doesn't affect BEAM responsiveness.

### 2. When each flavor applies

| Flavor | Use case | Cost |
|---|---|---|
| Regular | < 1 ms of CPU work | None |
| `DirtyCpu` | CPU-bound > 1 ms (hashing, compression, math) | Context switch overhead (~1 µs) |
| `DirtyIo` | Blocking I/O (file, socket, C library that calls `read(2)`) | Ditto |
| Yielding NIF | Cancellable long work | Code complexity — manual |

Rule: if `cargo bench` shows the NIF hits > 1 ms p99, flag it Dirty. If it's *way* over 1 ms (> 100 ms) and interruptibility matters, use a yielding NIF.

### 3. Rustler syntax

```rust
#[rustler::nif(schedule = "DirtyCpu")]
fn argon2_hash(password: Binary, salt: Binary) -> String { ... }

#[rustler::nif(schedule = "DirtyIo")]
fn read_big_file(path: String) -> Vec<u8> { ... }
```

### 4. Observability: `scheduler_wall_time`

```elixir
:erlang.system_flag(:scheduler_wall_time, true)
before = :erlang.statistics(:scheduler_wall_time)
# ... workload ...
after_ = :erlang.statistics(:scheduler_wall_time)
```

Each entry is `{scheduler_id, active_time, total_time}`. The ratio tells you scheduler utilization. If DirtyCpu schedulers are pinned at 100% while regulars idle, you're correctly offloading.

### 5. Dirty scheduler saturation

8 DirtyCpu schedulers mean 9 parallel long NIFs queue the 9th. Under heavy load, dirty scheduler queue time can become a bottleneck worse than the original blocking issue (because the scheduler can't interleave work). Monitor with `:msacc` (Microstate Accounting) — it shows dirty scheduler queue time explicitly.

### 6. Cost of the context switch

Moving a NIF call onto a dirty scheduler is not free — BEAM migrates the call to another OS thread, touches a mutex, and runs. Measured overhead: ~1–3 µs. If your NIF runs in 500 ns, dirty scheduling makes it slower. Only worth it when the NIF itself is ≥ 100 µs.

---

## Implementation

### Step 1: `native/rustler_dirty_nif/Cargo.toml`

```toml
[package]
name = "rustler_dirty_nif"
version = "0.1.0"
edition = "2021"

[lib]
name = "rustler_dirty_nif"
crate-type = ["cdylib"]

[dependencies]
rustler = "0.32"
argon2 = "0.5"
```

### Step 2: `native/rustler_dirty_nif/src/lib.rs`

```rust
use argon2::{Argon2, PasswordHasher, PasswordVerifier, PasswordHash};
use argon2::password_hash::{SaltString, rand_core::OsRng};
use rustler::{Binary, NifResult, Error};
use std::{thread, time::Duration};

#[rustler::nif(schedule = "DirtyCpu")]
fn argon2_hash(password: Binary) -> NifResult<String> {
    let salt = SaltString::generate(&mut OsRng);
    let argon2 = Argon2::default();

    argon2
        .hash_password(password.as_slice(), &salt)
        .map(|h| h.to_string())
        .map_err(|_| Error::Atom("hash_failed"))
}

#[rustler::nif(schedule = "DirtyCpu")]
fn argon2_verify(password: Binary, hash: String) -> bool {
    let Ok(parsed) = PasswordHash::new(&hash) else { return false };
    Argon2::default()
        .verify_password(password.as_slice(), &parsed)
        .is_ok()
}

#[rustler::nif(schedule = "DirtyCpu")]
fn count_primes(upper: u64) -> u64 {
    if upper < 2 { return 0 }
    let n = upper as usize;
    let mut sieve = vec![true; n + 1];
    sieve[0] = false; sieve[1] = false;
    let mut i = 2usize;
    while i * i <= n {
        if sieve[i] {
            let mut j = i * i;
            while j <= n { sieve[j] = false; j += i; }
        }
        i += 1;
    }
    sieve.iter().filter(|&&b| b).count() as u64
}

#[rustler::nif(schedule = "DirtyIo")]
fn blocking_sleep(ms: u64) -> u64 {
    thread::sleep(Duration::from_millis(ms));
    ms
}

rustler::init!(
    "Elixir.RustlerDirty.Native",
    [argon2_hash, argon2_verify, count_primes, blocking_sleep]
);
```

### Step 3: `lib/rustler_dirty/native.ex`

```elixir
defmodule RustlerDirty.Native do
  @moduledoc "NIFs scheduled on dirty CPU or dirty IO pools."
  use Rustler, otp_app: :rustler_dirty, crate: "rustler_dirty_nif"

  @spec argon2_hash(binary()) :: String.t()
  def argon2_hash(_password), do: :erlang.nif_error(:nif_not_loaded)

  @spec argon2_verify(binary(), String.t()) :: boolean()
  def argon2_verify(_password, _hash), do: :erlang.nif_error(:nif_not_loaded)

  @spec count_primes(non_neg_integer()) :: non_neg_integer()
  def count_primes(_upper), do: :erlang.nif_error(:nif_not_loaded)

  @spec blocking_sleep(non_neg_integer()) :: non_neg_integer()
  def blocking_sleep(_ms), do: :erlang.nif_error(:nif_not_loaded)
end
```

### Step 4: `mix.exs`

```elixir
defmodule RustlerDirty.MixProject do
  use Mix.Project

  def project do
    [
      app: :rustler_dirty,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: [{:rustler, "~> 0.32"}, {:benchee, "~> 1.3", only: :dev}],
      rustler_crates: [rustler_dirty_nif: [path: "native/rustler_dirty_nif", mode: :release]]
    ]
  end

  def application, do: [extra_applications: [:logger]]
end
```

### Step 5: `test/rustler_dirty_test.exs`

```elixir
defmodule RustlerDirtyTest do
  use ExUnit.Case, async: true
  alias RustlerDirty.Native

  test "argon2 hash + verify roundtrip" do
    hash = Native.argon2_hash("secret")
    assert String.starts_with?(hash, "$argon2")
    assert Native.argon2_verify("secret", hash)
    refute Native.argon2_verify("wrong", hash)
  end

  test "count_primes up to 100 = 25" do
    assert Native.count_primes(100) == 25
  end

  test "schedulers stay responsive during heavy NIF" do
    parent = self()

    # Start long dirty NIF in one task
    heavy = Task.async(fn -> Native.count_primes(50_000_000) end)

    # Meanwhile a snappy heartbeat process
    heartbeat =
      Task.async(fn ->
        for i <- 1..20 do
          send(parent, {:beat, i, System.monotonic_time(:millisecond)})
          Process.sleep(5)
        end
      end)

    Task.await(heartbeat)
    Task.await(heavy, 60_000)

    # All 20 heartbeats arrived within ~150 ms total — scheduler was not starved
    beats = for i <- 1..20, do: (receive do {:beat, ^i, t} -> t end)
    span = List.last(beats) - hd(beats)
    assert span < 200, "heartbeat span was #{span}ms — scheduler stalled"
  end

  test "dirty IO blocking sleep does not block regular scheduler" do
    parent = self()
    _t = Task.async(fn -> Native.blocking_sleep(500) end)
    send(parent, :live)
    assert_receive :live, 50
  end
end
```

---

## Trade-offs and production gotchas

**1. Overhead for fast paths.** Flagging `DirtyCpu` on a 100 ns function makes it 20× slower. Benchmark first; flag only what actually exceeds 1 ms.

**2. Dirty scheduler count is static.** Set at boot with `+SDcpu` and `+SDio`. If your workload shifts, you can't rescale without a restart. Default to `logical_cores` for CPU, 10 for IO.

**3. Saturation cascades.** 100 argon2 hashes queued on 8 DirtyCpu schedulers = ~12 hashes per scheduler, each 200 ms = 2.4 s wait for the last caller. Put a GenServer pool in front to bound concurrency and fail fast.

**4. `DirtyIo` is not async I/O.** The thread *blocks* on the syscall. If you block 10 DirtyIo schedulers on 10 slow file reads, the 11th queues. For true concurrency, use async I/O in Rust (tokio) — see exercise 172.

**5. Panics on dirty schedulers.** Same story — Rustler catches, raises in Elixir. But unwind across the dirty/regular boundary used to have bugs pre-OTP 26; stay on recent OTP.

**6. Observability.** `scheduler_wall_time` only reports regular schedulers by default; pass `:scheduler_wall_time_all` in OTP 25+ to include dirty schedulers.

**7. Cancellation.** A DirtyCpu NIF cannot be killed mid-flight — `Process.exit/2` waits until the NIF returns. For cancellable long work use a yielding NIF or chunk the work and check `env.monitor()`.

**8. When NOT to use this.** Work < 1 ms — regular NIF. Work > 1 s that should be interruptible — yielding NIF or Port. Fire-and-forget I/O — Task + async HTTP client, not a NIF.

---

## Benchmark

```elixir
Benchee.run(%{
  "argon2_hash (dirty)" => fn -> Native.argon2_hash("hunter2") end
})
# ~60 ms per hash on default params. During the hash, other BEAM processes
# remain responsive (scheduler_wall_time shows DirtyCpu at 100%, regular at low %).

# Contrast: same algorithm on a regular scheduler (pretend flag removed):
# - still ~60 ms per hash
# - BUT other processes on that scheduler see p99 latency jump to ~60 ms too.
```

---

## Resources

- https://www.erlang.org/doc/man/erlang.html#system_info-1 — `:dirty_cpu_schedulers`, `:dirty_io_schedulers`
- https://docs.rs/rustler/latest/rustler/attr.nif.html — `schedule` attribute
- https://blog.erlang.org/Scheduler-Locks/ — Erlang/OTP team on scheduler internals
- https://www.erlang.org/doc/apps/erts/erl_nif.html#dirty_nifs — official NIF docs on dirty scheduling
- https://github.com/riverrun/argon2_elixir — production NIF using DirtyCpu
- https://ferd.ca/beam-schedulers.html — Fred Hébert on scheduler behavior
- https://www.erlang.org/doc/man/msacc.html — microstate accounting for dirty scheduler queue time
