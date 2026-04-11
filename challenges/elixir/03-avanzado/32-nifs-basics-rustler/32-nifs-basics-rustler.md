# NIFs with Rustler

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` hashes request signatures for cache keys and idempotency checks. The current
Elixir implementation of SHA-256 uses `:crypto`, which is adequate for low throughput.
Under sustained load (10,000 req/s), profiling shows `:crypto.hash/2` accounts for 8% of
CPU time. The team wants to explore whether a Rust NIF can improve this, and to understand
the risks before committing.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       └── cache/
│           ├── hasher.ex               # ← you implement this (Elixir wrapper)
│           └── hasher_bench.exs
├── native/
│   └── gateway_hasher/
│       ├── Cargo.toml                  # ← you create this
│       └── src/
│           └── lib.rs                  # ← and this
├── test/
│   └── api_gateway/
│       └── cache/
│           └── hasher_test.exs         # given tests
├── bench/
│   └── hasher_bench.exs
└── mix.exs
```

---

## The business problem

The gateway hashes request payloads to produce cache keys. The hash must be:
- Deterministic and collision-resistant (SHA-256 is specified)
- Fast enough to not appear in p99 latency profiles
- Safe — a crash in the hasher must not take down the entire gateway

The team has three options: `:crypto` (Erlang NIF in C), a Rust NIF via Rustler, or a
pure Elixir fallback. Your task is to implement the Rust NIF, benchmark all three, and
document the trade-offs so the team can make an informed decision.

---

## What is a NIF and why it is dangerous

A **Native Implemented Function** is a function written in native code (C or Rust) and
loaded directly into the BEAM VM. From Elixir's perspective it looks like any other
function. The critical difference:

> **A NIF that blocks for more than 1 ms degrades all BEAM schedulers. A NIF that
> crashes takes down the entire VM — no supervisor can catch it.**

BEAM schedulers are preemptive for BEAM bytecode: each process gets a *reduction budget*
and yields. NIFs are **not preemptible** — they run to completion on the scheduler thread.
A NIF that takes 10 ms blocks that scheduler thread for 10 ms, increasing latency for all
processes scheduled on it.

```
Normal BEAM code:                NIF (wrong, > 1ms):
  Process A <- 1ms slice          Scheduler thread
  Process B <- 1ms slice          |
  Process C <- 1ms slice          | NIF running (10ms) <- ALL processes wait
  Process A <- 1ms slice          |
```

The solution for long-running work: **dirty schedulers**. OTP provides separate thread
pools (`DirtyCpu` and `DirtyIo`) for NIFs that exceed 1 ms. Dirty NIFs run off the main
scheduler threads and do not block other processes.

---

## Why Rustler instead of C NIFs

| | C NIF | Rustler (Rust) |
|---|---|---|
| Memory safety | Manual — use-after-free crashes VM | Guaranteed by borrow checker |
| Panics | VM crash | Caught by Rustler >= 0.31, converted to process exit |
| Build integration | `Makefile` or `cc_precompiler` | `mix compile` via Cargo |
| Ecosystem | C/C++ libraries | crates.io (sha2, ring, etc.) |
| Performance | Maximum | Equivalent — zero overhead |

---

## Implementation

### Step 1: Add Rustler to `mix.exs`

```elixir
defp deps do
  [
    {:rustler, "~> 0.34"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: Scaffold the Rust crate

```bash
mix rustler.new
# When prompted: otp_app = api_gateway, module = ApiGateway.Cache.GatewayHasher
# This creates native/gateway_hasher/
```

### Step 3: `native/gateway_hasher/Cargo.toml`

```toml
[package]
name = "gateway_hasher"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]

[dependencies]
rustler = "0.34"
sha2 = "0.10"
```

### Step 4: `native/gateway_hasher/src/lib.rs`

The Rust NIF exposes three functions. `sha256` is the production function, marked for the
dirty CPU scheduler because payloads can exceed 10 KB and the hash may take >1ms.
`sha256_blocking` is for benchmarking only — it runs on a regular scheduler to demonstrate
the latency impact. `divide` demonstrates explicit error propagation via `NifResult` instead
of panicking.

```rust
use rustler::{Binary, NifResult};
use sha2::{Digest, Sha256};

/// Computes SHA-256 of `data` and returns the digest as a 32-byte binary.
///
/// Marked DirtyCpu because payloads can be large (up to 1 MB in the gateway).
/// Any NIF that may take > 1ms must use a dirty scheduler.
#[rustler::nif(schedule = "DirtyCpu")]
fn sha256(data: Binary) -> Vec<u8> {
    Sha256::digest(data.as_slice()).to_vec()
}

/// Same computation without dirty scheduler — for benchmarking the impact.
/// DO NOT use in production for payloads > ~10 KB.
#[rustler::nif]
fn sha256_blocking(data: Binary) -> Vec<u8> {
    Sha256::digest(data.as_slice()).to_vec()
}

/// Divides two integers. Returns {:error, :division_by_zero} instead of panicking.
/// Demonstrates NifResult for explicit error propagation — never use panic in NIFs.
#[rustler::nif]
fn divide(a: i64, b: i64) -> NifResult<i64> {
    if b == 0 {
        return Err(rustler::Error::Atom("division_by_zero"));
    }
    Ok(a / b)
}

rustler::init!("Elixir.ApiGateway.Cache.GatewayHasher", [sha256, sha256_blocking, divide]);
```

### Step 5: `lib/api_gateway/cache/hasher.ex`

The Elixir wrapper module loads the Rust NIF at startup via `use Rustler`. The placeholder
functions (`sha256/1`, `sha256_blocking/1`, `divide/2`) are replaced by the native
implementations at load time. If Rust compilation fails, the placeholders raise
`:nif_not_loaded`, providing a clear error instead of a silent failure.

The public API (`hash_nif/1`, `hash_crypto/1`, `hash_elixir/1`) normalizes all three
strategies to return a 64-character lowercase hex string, enabling direct comparison.

```elixir
defmodule ApiGateway.Cache.Hasher do
  @moduledoc """
  Request payload hasher for cache key generation.

  Exposes three implementations for benchmarking:
    - hash_crypto/1    — :crypto.hash(:sha256, data), Erlang NIF in C
    - hash_nif/1       — Rust NIF via Rustler (dirty CPU scheduler)
    - hash_elixir/1    — Pure Elixir fallback using :crypto (noted in benchmarks)

  All return a 64-character lowercase hex string.
  """

  # ---------------------------------------------------------------------------
  # Rust NIF — loaded at startup; placeholder raises if Rust compilation failed
  # ---------------------------------------------------------------------------

  use Rustler,
    otp_app: :api_gateway,
    crate: "gateway_hasher"

  # Placeholders replaced by Rust implementations at load time
  def sha256(_data), do: :erlang.nif_error(:nif_not_loaded)
  def sha256_blocking(_data), do: :erlang.nif_error(:nif_not_loaded)
  def divide(_a, _b), do: :erlang.nif_error(:nif_not_loaded)

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @spec hash_nif(binary()) :: String.t()
  def hash_nif(data) when is_binary(data) do
    data
    |> sha256()
    |> Base.encode16(case: :lower)
  end

  @spec hash_crypto(binary()) :: String.t()
  def hash_crypto(data) when is_binary(data) do
    :crypto.hash(:sha256, data)
    |> Base.encode16(case: :lower)
  end

  @spec hash_elixir(binary()) :: String.t()
  def hash_elixir(data) when is_binary(data) do
    # Pure Elixir SHA-256 is impractical to implement correctly in a tutorial.
    # This delegates to :crypto as a fallback. In production, you would use
    # a pure-Elixir library if native code were truly unavailable.
    # The benchmark comparison between hash_nif and hash_crypto is the
    # meaningful one — both are native code (Rust vs C).
    hash_crypto(data)
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/cache/hasher_test.exs
defmodule ApiGateway.Cache.HasherTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Cache.Hasher

  @known_input "hello world"
  @known_sha256 "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

  describe "all strategies produce the same hash" do
    test "hash_nif matches known SHA-256" do
      assert Hasher.hash_nif(@known_input) == @known_sha256
    end

    test "hash_crypto matches known SHA-256" do
      assert Hasher.hash_crypto(@known_input) == @known_sha256
    end

    test "all three agree on random input" do
      data = :crypto.strong_rand_bytes(1024)
      assert Hasher.hash_nif(data) == Hasher.hash_crypto(data)
    end
  end

  describe "divide/2" do
    test "returns quotient for valid inputs" do
      assert {:ok, 5} = Hasher.divide(10, 2)
    end

    test "returns error for division by zero without crashing the VM" do
      assert {:error, :division_by_zero} = Hasher.divide(10, 0)
      # VM must still be alive
      assert {:ok, 1} = Hasher.divide(5, 5)
    end
  end
end
```

### Step 7: Benchmark

```elixir
# bench/hasher_bench.exs
alias ApiGateway.Cache.Hasher

inputs = %{
  "1 KB"  => :crypto.strong_rand_bytes(1_024),
  "10 KB" => :crypto.strong_rand_bytes(10_240),
  "1 MB"  => :crypto.strong_rand_bytes(1_048_576)
}

Benchee.run(
  %{
    "hash_crypto"   => fn data -> Hasher.hash_crypto(data) end,
    "hash_nif"      => fn data -> Hasher.hash_nif(data) end,
    "sha256_blocking (no dirty)" =>
      fn data -> Base.encode16(Hasher.sha256_blocking(data), case: :lower) end
  },
  inputs: inputs,
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/hasher_bench.exs
```

**Expected observations**:
- `hash_nif` (dirty) and `hash_crypto` should be comparable — both call C/Rust native code.
- `sha256_blocking` with large inputs will impact p99 latency of other concurrent operations.
  To observe this, run a concurrent ping-pong benchmark while `sha256_blocking` processes 1 MB.

---

## Trade-off analysis

| Aspect | Rust NIF (dirty) | `:crypto` (C NIF, OTP) | Port (external process) |
|--------|-----------------|------------------------|-------------------------|
| Throughput | Highest | High | Lower (IPC overhead) |
| Crash isolation | Process exit (Rustler >= 0.31) | VM crash on C bug | OS process isolated |
| Scheduler blocking | No (dirty) | No (OTP uses dirty) | No (async Port) |
| Build complexity | Rust toolchain required | None (OTP) | None |
| Memory safety | Borrow checker | Manual (OTP team's problem) | Language-level |
| When to choose | Hot path, CPU-bound, 3rd-party Rust crates | Standard hashing/crypto | Long-running, > 100ms operations |

The 1 ms rule:
- NIF runs < 1 ms on typical inputs -> regular NIF is safe
- NIF runs > 1 ms (large payloads) -> use `DirtyCpu`
- NIF runs > 1 s -> consider a Port instead

---

## Common production mistakes

**1. Not using `DirtyCpu` for large data**
`sha256_blocking` on a 10 MB payload takes ~10 ms. That blocks the scheduler thread for
10 ms. With 8 scheduler threads and 10 concurrent hashing requests, all 80 ms of blocking
is serialized — p99 latency explodes.

**2. Using `panic!` instead of `NifResult`**
In Rustler < 0.31, a `panic!` crashed the VM. In Rustler >= 0.31, panics are caught and
converted to a process exit. However, relying on panic catching is still bad practice.
Always return `Err(rustler::Error::Atom("reason"))` for expected error conditions.

**3. Forgetting `crate-type = ["cdylib"]` in Cargo.toml**
Without `cdylib`, Cargo produces a static library that the BEAM cannot load at runtime.
The mix compilation will succeed but `nif_not_loaded` will be raised at call time.

**4. Calling `sha256` from a test process with `async: true`**
The NIF itself is thread-safe. However, if the NIF has global mutable state (none in this
example), concurrent tests could race. Always verify thread safety in the Rust code before
enabling `async: true` in tests.

**5. Not measuring before optimizing**
`:crypto.hash/2` is itself a dirty NIF written by the OTP team. For SHA-256, it is
extremely unlikely that a Rustler NIF will be meaningfully faster. Always profile first.
In this exercise the goal is to understand the mechanism, not necessarily to beat OTP.

---

## Resources

- [Rustler — GitHub](https://github.com/rusterlium/rustler)
- [Rustler — HexDocs](https://hexdocs.pm/rustler/Rustler.html)
- [OTP dirty schedulers — Erlang docs](https://www.erlang.org/doc/man/erl.html#+SDcpu)
- [NIFs — Erlang reference manual](https://www.erlang.org/doc/man/erl_nif.html)
- [sha2 crate — crates.io](https://crates.io/crates/sha2)
