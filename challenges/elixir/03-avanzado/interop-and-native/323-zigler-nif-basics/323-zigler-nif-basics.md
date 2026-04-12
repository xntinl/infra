# Zigler — NIFs Written in Zig

**Project**: `crypto_primitives` — implement constant-time comparison and HKDF-Expand directly in Zig for zero-allocation cryptographic primitives callable from Elixir.

## Project context

A security-sensitive backend needs low-level primitives: constant-time byte comparison
(HMAC verification), HKDF-Expand for deriving keys, and a fast XOR over large buffers for
stream-cipher masking. Elixir's `:crypto` module covers most needs but for one hot path —
verifying MAC tags on millions of WebSocket frames per second — the cost of going through
OpenSSL FFI every time is measurable.

Zig is a good fit for small, verifiable native code: no hidden allocator, explicit error
unions, compile-time evaluation, and a standard library that already contains HKDF. Zigler
provides the bridge: inline Zig snippets inside `.ex` files, compiled to a NIF at build
time, with `beam.env` and `beam.term` abstractions mirroring Rustler's ergonomics.

```
crypto_primitives/
├── lib/
│   └── crypto_primitives/
│       ├── application.ex
│       └── zig.ex
├── test/crypto_primitives/zig_test.exs
├── bench/zig_bench.exs
└── mix.exs
```

## Why Zig and not Rust

Both are good choices. The differences that matter for NIFs:

- **Compilation speed**: Zig compiles 2–5x faster than Rust for small codebases. On a
  laptop, a full rebuild of a 200-line NIF is sub-second.
- **Allocator control**: Zig makes allocators explicit parameters (`std.heap.c_allocator`,
  `arena_allocator`). You cannot accidentally heap-allocate in a hot path.
- **Binary size**: Zigler builds are typically 30–40% smaller than the Rust equivalent,
  which matters for container images.
- **Unsafe by default**: Zig has no borrow checker. You trade compile-time safety for
  compile speed. For cryptographic primitives that are self-contained and well-tested,
  this is an acceptable trade.

Use Rust when the code is large and needs the borrow checker. Use Zig when the code is
small and the compile-time budget matters.

## Why inline Zig and not a separate crate

Zigler's killer feature: Zig source lives as a sigil inside the `.ex` file. You read the
Zig and the Elixir side-by-side — no `native/` directory to navigate. For primitives that
are < 200 lines of Zig, this colocation is strictly better than a separate crate.

For larger Zig codebases, Zigler also supports importing external `.zig` files.

## Core concepts

### 1. The `~Z` sigil

```elixir
~Z"""
pub fn add(a: i64, b: i64) i64 {
    return a + b;
}
"""
```

Every `pub fn` becomes a NIF with the same name in the enclosing module, unless suppressed
with `@nif` attributes.

### 2. `beam.env` and term conversion

For primitive types (i64, u8, f64, []u8), Zigler auto-generates encoders and decoders. For
custom types, you write `pub fn encode(env, value) beam.term` and `pub fn decode(env, term) T`.

### 3. Binary views are zero-copy

A `[]u8` parameter maps to a view into the Elixir binary's byte buffer. No copy, no allocation.
Valid only for the call duration.

### 4. Threading

Zigler supports `@dirty_cpu` and `@dirty_io` attributes on `pub fn` declarations to schedule
the function on a dirty pool, same semantics as Rustler.

## Design decisions

- **Option A — pure Zig primitives (no std.crypto)**: control exactly what runs, easier to
  audit. More code to write.
- **Option B — wrap std.crypto**: minimal code, benefits from Zig's audited crypto.

→ For HKDF we use **Option B** (`std.crypto.kdf.hkdf.HkdfSha256.expand`). For constant-time
  compare we write **Option A** because the Zig stdlib's `mem.eql` is not constant-time.

- **Option A — return a new binary**: caller gets a fresh binary.
- **Option B — mutate a pre-allocated binary**: avoids copies. Dangerous without ownership.

→ We use **Option A** via `beam.make_binary` which allocates a BEAM-managed binary. Zero
  risk of use-after-free.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule CryptoPrimitives.MixProject do
  use Mix.Project

  def project do
    [
      app: :crypto_primitives,
      version: "0.1.0",
      elixir: "~> 1.17",
      deps: [
        {:zigler, "~> 0.13"},
        {:benchee, "~> 1.3", only: :dev}
      ]
    ]
  end

  def application, do: [extra_applications: [:logger, :crypto]]
end
```

Zigler requires the Zig compiler on `$PATH`. Version is pinned by Zigler; check
`mix zig.ls` after adding the dep.

### Step 1: The Zig module (`lib/crypto_primitives/zig.ex`)

```elixir
defmodule CryptoPrimitives.Zig do
  @moduledoc """
  Inline Zig NIFs for cryptographic primitives.

  All functions are pure: they read from input binaries (zero-copy views) and
  return freshly-allocated BEAM binaries. No shared mutable state.
  """
  use Zig, otp_app: :crypto_primitives

  ~Z"""
  const std = @import("std");
  const beam = @import("beam");

  /// Constant-time compare: returns true iff a and b are equal.
  /// Running time depends only on max(len(a), len(b)), not on the contents.
  /// MUST be used for MAC / signature verification.
  pub fn ct_equal(a: []const u8, b: []const u8) bool {
      if (a.len != b.len) return false;
      var diff: u8 = 0;
      var i: usize = 0;
      while (i < a.len) : (i += 1) {
          diff |= a[i] ^ b[i];
      }
      return diff == 0;
  }

  /// XOR two equal-length byte strings into a new binary.
  /// Used for stream-cipher masking and one-time pads.
  pub fn xor_bytes(env: beam.env, a: []const u8, b: []const u8) !beam.term {
      if (a.len != b.len) return error.UnequalLengths;
      var out = try beam.allocator.alloc(u8, a.len);
      defer beam.allocator.free(out);
      var i: usize = 0;
      while (i < a.len) : (i += 1) {
          out[i] = a[i] ^ b[i];
      }
      return beam.make_binary(env, out);
  }

  /// HKDF-Expand (RFC 5869, section 2.3) using SHA-256.
  /// prk: pseudorandom key (output of HKDF-Extract).
  /// info: context and application-specific info.
  /// length: desired output length in bytes (1..8160).
  pub fn hkdf_expand_sha256(env: beam.env,
                            prk:  []const u8,
                            info: []const u8,
                            length: u32) !beam.term {
      if (length == 0 or length > 8160) return error.InvalidLength;
      var out = try beam.allocator.alloc(u8, length);
      defer beam.allocator.free(out);
      std.crypto.kdf.hkdf.HkdfSha256.expand(out, info, prk[0..32].*);
      return beam.make_binary(env, out);
  }
  """
end
```

### Step 2: Application (`lib/crypto_primitives/application.ex`)

```elixir
defmodule CryptoPrimitives.Application do
  use Application

  @impl true
  def start(_, _) do
    Supervisor.start_link([], strategy: :one_for_one, name: CryptoPrimitives.Supervisor)
  end
end
```

## Why this works

```
Elixir call
    │
    ▼
Zigler dispatch — decode [:binary, :binary] into []const u8 views
    │             (zero-copy: views into the refc binary heap)
    ▼
Zig function  — constant-time loop, no branch on data
    │
    ▼
beam.make_binary — allocates a BEAM-owned binary and copies out
    │
    ▼
returned as Elixir binary, managed by BEAM GC
```

- **No timing side-channels**: `ct_equal` XORs all bytes and ORs the result. No early
  exit means the total time is proportional to the declared length, not to where the
  first differing byte is.
- **Explicit allocator**: every allocation goes through `beam.allocator`, which is the
  BEAM's own. Errors propagate up as Zig error unions — Zigler converts them to
  `{:error, :UnequalLengths}` etc.
- **Stack-only hot path**: `ct_equal` does no heap work at all. Runs entirely in registers.

## Tests (`test/crypto_primitives/zig_test.exs`)

```elixir
defmodule CryptoPrimitives.ZigTest do
  use ExUnit.Case, async: true
  alias CryptoPrimitives.Zig

  describe "ct_equal/2" do
    test "returns true for identical binaries" do
      assert Zig.ct_equal(<<1, 2, 3, 4>>, <<1, 2, 3, 4>>)
    end

    test "returns false for different contents" do
      refute Zig.ct_equal(<<1, 2, 3, 4>>, <<1, 2, 3, 5>>)
    end

    test "returns false for different lengths" do
      refute Zig.ct_equal(<<1, 2, 3>>, <<1, 2, 3, 4>>)
    end

    test "empty binaries compare equal" do
      assert Zig.ct_equal(<<>>, <<>>)
    end
  end

  describe "xor_bytes/2" do
    test "XOR is self-inverse" do
      a = :crypto.strong_rand_bytes(64)
      b = :crypto.strong_rand_bytes(64)
      c = Zig.xor_bytes(a, b)
      assert Zig.xor_bytes(c, b) == a
    end

    test "unequal lengths raise" do
      assert_raise ErlangError, fn ->
        Zig.xor_bytes(<<1, 2, 3>>, <<1, 2>>)
      end
    end
  end

  describe "hkdf_expand_sha256/3" do
    test "deterministic output for same inputs" do
      prk = :crypto.strong_rand_bytes(32)
      info = "context"
      o1 = Zig.hkdf_expand_sha256(prk, info, 42)
      o2 = Zig.hkdf_expand_sha256(prk, info, 42)
      assert o1 == o2
      assert byte_size(o1) == 42
    end

    test "different info yields different output" do
      prk = :crypto.strong_rand_bytes(32)
      o1 = Zig.hkdf_expand_sha256(prk, "a", 32)
      o2 = Zig.hkdf_expand_sha256(prk, "b", 32)
      refute o1 == o2
    end

    test "length 0 or > 8160 is rejected" do
      prk = :crypto.strong_rand_bytes(32)
      assert_raise ErlangError, fn -> Zig.hkdf_expand_sha256(prk, "", 0) end
      assert_raise ErlangError, fn -> Zig.hkdf_expand_sha256(prk, "", 9000) end
    end
  end
end
```

## Benchmark (`bench/zig_bench.exs`)

```elixir
data_a = :crypto.strong_rand_bytes(32)
data_b = :crypto.strong_rand_bytes(32)
prk = :crypto.strong_rand_bytes(32)

Benchee.run(
  %{
    "ct_equal 32B (Zig)" => fn -> CryptoPrimitives.Zig.ct_equal(data_a, data_b) end,
    ":crypto HMAC verify (reference)" => fn ->
      # Not a direct substitute — gives a sense of OpenSSL-side cost.
      :crypto.hash(:sha256, data_a)
    end,
    "xor_bytes 1KB (Zig)" => fn ->
      CryptoPrimitives.Zig.xor_bytes(
        :crypto.strong_rand_bytes(1024),
        :crypto.strong_rand_bytes(1024)
      )
    end,
    "HKDF-Expand 32B (Zig)" => fn ->
      CryptoPrimitives.Zig.hkdf_expand_sha256(prk, "ctx", 32)
    end
  },
  time: 5, warmup: 2
)
```

**Expected**: `ct_equal` < 100ns (less than any `GenServer.call` round trip), `xor_bytes`
for 1KB < 1µs, HKDF-Expand for 32 bytes output < 3µs. If numbers are > 10x higher,
verify the release build (`mix compile --env prod` or `MIX_ENV=prod`).

## Trade-offs and production gotchas

**1. Zig ABI instability.** Zig is pre-1.0. Every minor version can break source. Pin the
Zig version in Zigler config; plan for a migration every 6–12 months.

**2. No borrow checker.** A Zig NIF with a use-after-free segfaults the BEAM just as hard
as C. Review every allocator lifetime. Prefer stack allocations.

**3. `[]const u8` is a view.** Storing a `[]const u8` in a global or passing to a thread
that outlives the call is undefined behavior — the BEAM may have already freed or moved
the underlying binary. Copy to an owned buffer if you need it.

**4. `beam.allocator.alloc` failure is possible.** Under heavy memory pressure the BEAM
can refuse. Always handle the error union (`try`) rather than unwrapping.

**5. Constant-time-ness is a Zig guarantee, not a BEAM guarantee.** If you add any
`if (data[i] == ...)` branch on secret data, you reintroduce timing leaks. Audit
transforms manually; compilers love to insert branches.

**6. When NOT to use Zigler.** For code already maintained in Rust, stay with Rustler.
Zigler is the right choice for new, small, performance-sensitive primitives where Zig's
compile speed and explicit allocator model give real daily-workflow benefits.

## Reflection

`hkdf_expand_sha256` currently allocates a fresh BEAM binary of `length` bytes per call.
For an application deriving many keys rapidly from the same PRK, what API shape (hint:
pre-allocate a larger buffer and slice inside Elixir, or batch multiple infos into one
NIF call) would reduce allocation pressure without compromising the one-call-per-key
clarity?

## Resources

- [Zigler hex docs](https://hexdocs.pm/zigler/)
- [Zig language reference](https://ziglang.org/documentation/master/)
- [RFC 5869 — HKDF](https://datatracker.ietf.org/doc/html/rfc5869)
- [Timing-safe comparison — Go's `subtle.ConstantTimeCompare`](https://pkg.go.dev/crypto/subtle#ConstantTimeCompare)
