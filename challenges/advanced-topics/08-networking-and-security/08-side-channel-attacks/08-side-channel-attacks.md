<!--
type: reference
difficulty: advanced
section: [08-networking-and-security]
concepts: [timing-attacks, constant-time-comparison, cache-timing, flush-reload, spectre-meltdown, retpolines, kpti, hmac-timing]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [cryptographic-primitives, cpu-architecture-for-programmers, cache-coherence-basics]
papers: [Kocher 1996 — Timing Attacks on Implementations of Diffie-Hellman RSA DSS and Other Systems, Yarom & Falkner 2014 — FLUSH+RELOAD, Kocher et al. 2019 — Spectre Attacks]
industry_use: [libsodium, ring, aws-nitro, signal-protocol, wireguard, tpm-firmware]
language_contrast: high
-->

# Side-Channel Attacks

> A cryptographic implementation is only as secure as its timing behavior — an attacker
> who can measure execution time in microseconds can recover secret keys without breaking
> any mathematical assumption.

## Mental Model

Side-channel attacks exploit information leaked by a system's *physical behavior* — timing,
power consumption, electromagnetic emissions, cache access patterns — rather than attacking
the cryptographic algorithm directly. The algorithm may be mathematically perfect; the
implementation leaks the secret through a different channel.

The most impactful side channel in software is timing. Consider a string comparison:

```go
func equal(a, b string) bool {
    return a == b  // VULNERABLE: returns early on first mismatch
}
```

In Go (and virtually every language), `==` on byte sequences uses an early-exit comparison:
it returns `false` as soon as the first differing byte is found. This means that if `a`
is a secret value, the time to return `false` for `b = "z..."` is different from
`b = secret[:1] + "..."` (which matches the first byte and takes slightly longer). An
attacker who can make many requests and measure response times can discover the secret
byte-by-byte, one byte per ~2^8 = 256 guesses. For a 16-byte HMAC, that is 16 * 256 = 4096
requests — trivially fast.

The standard defense is **constant-time comparison**: compute the comparison result for all
bytes before returning, using only bitwise operations that take the same number of cycles
regardless of the input values.

Cache-timing attacks operate at the hardware level. Modern CPUs cache recently accessed
memory; a cache hit is ~4 cycles, a cache miss is ~100–200 cycles. If an implementation
uses secret-dependent table lookups (e.g., AES S-boxes in software), an attacker sharing
the same CPU core (co-tenant in cloud, another process on the same physical machine) can
measure cache access patterns via FLUSH+RELOAD or Prime+Probe to reconstruct the secret.

Spectre (2018) is a different class: it exploits CPU speculative execution to read memory
that the process is not allowed to access. The branch predictor speculatively executes
past a bounds check; the speculative execution reads secret memory into the cache; a
FLUSH+RELOAD measurement reveals the cache-resident secret. Spectre cannot be fixed purely
in software — it requires hardware mitigations (retpolines for indirect branches, IBRS/STIBP
for cross-SMT attacks) with measurable performance costs.

## Core Concepts

### Constant-Time Operations

A constant-time operation is one whose execution time does not depend on the secret input.
The key constraints:

1. **No early exit**: all bytes are compared, always.
2. **No secret-dependent branches**: `if secret == value` branches are forbidden.
3. **No secret-dependent memory accesses**: table lookups indexed by secret values leak
   via cache timing.

Constant-time operations use:
- Bitwise AND, OR, XOR (always same number of cycles)
- Arithmetic without carry-dependent branching
- `cmov` instructions (conditional move, no branch)

The XOR accumulation pattern for constant-time equality:

```
diff = 0
for each byte i:
    diff |= (a[i] ^ b[i])
return diff == 0  # 0 if and only if all bytes were equal
```

This touches all bytes unconditionally. The final `== 0` check is on a non-secret value
(the accumulated diff), so it is safe.

### Cache-Timing Attack Primitives

**FLUSH+RELOAD**: attacker flushes a cache line, victim accesses memory, attacker measures
reload time. If fast (cache hit), the victim accessed that line; if slow (cache miss), it
did not. Requires shared memory between attacker and victim (e.g., shared library pages
or memory-mapped files in the same VM).

**Prime+Probe**: attacker fills a cache set with their own data (Prime), victim runs,
attacker re-accesses their data (Probe) and measures time. Evicted lines indicate victim
accessed the same cache set. Does not require shared memory — works across processes and
across VMs if the attacker can identify physical cache sets.

**Victim's AES S-box attack**: AES in software uses precomputed tables (S-boxes). The
table entry accessed depends on the plaintext XOR key byte. An attacker on the same CPU
can measure which S-box entries were accessed, narrowing the key byte to a few candidates.
The fix: AES-NI instructions (hardware AES that does not use a software table).

### Spectre and Meltdown

**Meltdown (CVE-2017-5754)**: exploits out-of-order execution. The kernel maps physical
memory into the kernel's virtual address space. Meltdown reads kernel memory by triggering
a fault (access to kernel address from user space), but the OoO engine executes subsequent
instructions before the fault is raised, loading kernel memory into CPU registers and then
into the cache. A FLUSH+RELOAD measurement reveals the loaded byte. Mitigation: KPTI
(Kernel Page Table Isolation) — map kernel pages into a separate address space that is
not present during user-space execution. Cost: ~5–25% performance overhead for syscall-
heavy workloads.

**Spectre (CVE-2017-5753, variant 1)**: exploits branch misprediction. The branch predictor
speculatively executes code past a bounds check. The speculative code reads an out-of-bounds
array element, uses it to index another array (accessing a cache line), and then the
misprediction is corrected (the speculative results are discarded from the architectural
state — but not from the cache). A FLUSH+RELOAD reveals the cache-resident index. Mitigation:
`lfence` after bounds checks (Spectre variant 1), retpolines for indirect branches (variant 2).
No complete software mitigation exists for all Spectre variants.

## Implementation: Go

```go
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"time"
)

// VULNERABLE: time-leaking comparison.
// An attacker who can measure response time can discover the expected value
// one byte at a time by observing when the comparison takes longer.
func vulnerableEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false // VULNERABLE: early exit leaks match length
		}
	}
	return true
}

// SECURE: constant-time comparison using crypto/subtle.
// This is a standard library function; use it instead of writing your own.
// The implementation uses cmov-style bitwise operations to avoid branching.
func secureEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// SECURE (manual): demonstrates the XOR-accumulate pattern.
// Only write this yourself if you need to understand what subtle.ConstantTimeCompare does.
// In production, always use subtle.ConstantTimeCompare.
func manualConstantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		// Length comparison is safe to branch on — length is not a secret.
		// However, to prevent timing on the length itself when the lengths differ
		// by 1, some implementations always run the inner loop. For MAC comparison,
		// both sides should have the same fixed length anyway.
		return false
	}

	var diff uint8
	for i := range a {
		diff |= a[i] ^ b[i] // accumulate differences — no branch on secret
	}
	return diff == 0 // diff == 0 iff all bytes were equal
}

// verifyHMAC verifies an HMAC-SHA256 tag using constant-time comparison.
// This is the canonical use case for subtle.ConstantTimeCompare:
// you have a computed MAC and a provided MAC, and you must not leak
// whether the prefix of the provided MAC is correct.
func verifyHMAC(message, providedMAC, key []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	expectedMAC := mac.Sum(nil)

	// SECURE: constant-time comparison prevents MAC forgery via timing oracle.
	// Without this, an attacker can forge a valid MAC by guessing one byte at a time.
	return subtle.ConstantTimeCompare(expectedMAC, providedMAC) == 1
}

// VULNERABLE: verifyHMACBad leaks timing information about the expected MAC.
func verifyHMACBad(message, providedMAC, key []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	expectedMAC := mac.Sum(nil)

	// VULNERABLE: bytes.Equal (and ==) use early-exit comparison.
	// An attacker who can measure response times can recover expectedMAC byte-by-byte.
	return string(expectedMAC) == string(providedMAC)
}

// demonstrateTimingLeak measures the difference in comparison time between
// a correct first byte and an incorrect first byte.
// In a real attack, this difference is ~nanoseconds and requires statistical averaging
// over thousands of samples. This demonstration uses a tight loop to amplify the signal.
func demonstrateTimingLeak(secret []byte) {
	correct := make([]byte, len(secret))
	copy(correct, secret)
	correct[0] = secret[0] // correct first byte

	wrong := make([]byte, len(secret))
	copy(wrong, secret)
	wrong[0] ^= 0xFF // wrong first byte

	const iterations = 1_000_000

	start := time.Now()
	for range iterations {
		_ = vulnerableEqual(secret, correct)
	}
	correctTime := time.Since(start)

	start = time.Now()
	for range iterations {
		_ = vulnerableEqual(secret, wrong)
	}
	wrongTime := time.Since(start)

	fmt.Printf("Timing leak demo (vulnerable comparison):\n")
	fmt.Printf("  Correct first byte: %v\n", correctTime)
	fmt.Printf("  Wrong first byte:   %v\n", wrongTime)
	fmt.Printf("  Difference: %v (should be near zero for constant-time)\n",
		correctTime-wrongTime)

	// Now show constant-time comparison has no leakage
	start = time.Now()
	for range iterations {
		_ = secureEqual(secret, correct)
	}
	ctCorrect := time.Since(start)

	start = time.Now()
	for range iterations {
		_ = secureEqual(secret, wrong)
	}
	ctWrong := time.Since(start)

	fmt.Printf("\nConstant-time comparison:\n")
	fmt.Printf("  Correct first byte: %v\n", ctCorrect)
	fmt.Printf("  Wrong first byte:   %v\n", ctWrong)
	fmt.Printf("  Difference: %v (should be near zero)\n", ctCorrect-ctWrong)
}

// selectNoTimingLeak demonstrates constant-time conditional selection.
// Returns a if cond == 1, b if cond == 0.
// Used to implement cryptographic algorithms without secret-dependent branches.
func constantTimeSelect(cond uint8, a, b []byte) []byte {
	if len(a) != len(b) {
		panic("slices must be same length")
	}
	// subtle.ConstantTimeSelect computes (cond & a[i]) | (^cond & b[i]) per byte.
	mask := subtle.ConstantTimeByteEq(cond, 1) // returns 0xFF if cond==1, 0x00 otherwise
	result := make([]byte, len(a))
	for i := range a {
		result[i] = byte(subtle.ConstantTimeSelect(mask, int(a[i]), int(b[i])))
	}
	return result
}

// AES-NI availability: Go's crypto/aes uses AES-NI on x86-64 automatically.
// To verify: go tool nm ./binary | grep -i "aes"
// If you see references to _gcmAesEnc or _gcmAesDec, the assembly path is used.
// These are not vulnerable to cache-timing because they use dedicated AES instructions.

func main() {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(err)
	}

	fmt.Printf("Secret (hex): %s\n\n", hex.EncodeToString(secret))
	demonstrateTimingLeak(secret)

	// HMAC verification
	message := []byte("important payload")
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	tag := mac.Sum(nil)

	fmt.Printf("\nHMAC verification (correct): %v\n", verifyHMAC(message, tag, key))
	fmt.Printf("HMAC verification (tampered): %v\n", verifyHMAC(message, append(tag[:len(tag)-1], 0xFF), key))
}
```

### Go-specific considerations

**`crypto/subtle.ConstantTimeCompare` is the only correct tool.** Never implement
constant-time comparison yourself in production code. The `subtle` package is part of the
standard library and is tested against timing leaks. The `subtle.ConstantTimeByteEq`,
`subtle.ConstantTimeSelect`, and `subtle.ConstantTimeLessOrEq` functions cover most
cryptographic use cases.

**Go does not guarantee compiler non-optimization of constant-time code.** The Go
compiler may theoretically optimize away "dead" comparisons in constant-time code if it
can prove they do not affect the result. The `crypto/subtle` package is implemented in
assembly (or uses `//go:nosplit` directives) to prevent this. When writing custom
constant-time code, verify with `go tool objdump` that the compiler did not optimize it.

**GC pauses introduce timing noise.** Go's garbage collector introduces latency variance
that makes precise timing attacks harder to mount from the network (the attack requires
statistical averaging). However, this does not make early-exit comparisons safe — attacks
can be mounted locally or with enough samples. Do not rely on GC noise as a defense.

**`hmac.Equal` uses `subtle.ConstantTimeCompare` internally.** In Go, `hmac.Equal(mac1, mac2)`
is constant-time. Use it instead of writing `subtle.ConstantTimeCompare` directly when
comparing HMACs.

## Implementation: Rust

```rust
use subtle::{Choice, ConstantTimeEq, ConstantTimeLess};
use hmac::{Hmac, Mac};
use sha2::Sha256;

type HmacSha256 = Hmac<Sha256>;

/// VULNERABLE: early-exit byte comparison.
/// DO NOT USE for secrets. Shown here for contrast with the constant-time version.
fn vulnerable_equal(a: &[u8], b: &[u8]) -> bool {
    a == b // VULNERABLE: Rust's == on slices is NOT constant-time
}

/// SECURE: constant-time comparison using the `subtle` crate.
/// `subtle::ConstantTimeEq` implements `ct_eq` which returns a `Choice` —
/// a type that wraps a u8 and is designed to be difficult to accidentally optimize.
fn secure_equal(a: &[u8], b: &[u8]) -> bool {
    // ConstantTimeEq on slices is constant-time only if both slices have the same length.
    // If lengths differ, return false immediately (length is not secret in MAC comparison).
    if a.len() != b.len() {
        return false;
    }
    a.ct_eq(b).into()
}

/// SECURE: verify an HMAC-SHA256 tag using constant-time comparison.
fn verify_hmac(message: &[u8], provided_mac: &[u8], key: &[u8]) -> bool {
    let mut mac = HmacSha256::new_from_slice(key).expect("valid key length");
    mac.update(message);

    // hmac::Mac::verify_slice uses constant-time comparison internally.
    // This is the idiomatic way to verify HMACs in Rust.
    mac.verify_slice(provided_mac).is_ok()
}

/// Demonstrate `subtle::Choice` for constant-time conditional logic.
/// `Choice` wraps a `u8` (0 or 1) and disables short-circuit evaluation,
/// making `if choice { ... }` patterns structurally impossible.
fn constant_time_select(cond: Choice, if_true: u8, if_false: u8) -> u8 {
    // This expands to: mask = (cond as u8).wrapping_neg() [all 1s or all 0s]
    //                  (mask & if_true) | (!mask & if_false)
    let mask = cond.unwrap_u8().wrapping_neg();
    (mask & if_true) | (!mask & if_false)
}

/// Constant-time byte-wise selection between two slices.
fn ct_select_slice(cond: Choice, if_true: &[u8], if_false: &[u8]) -> Vec<u8> {
    assert_eq!(if_true.len(), if_false.len());
    if_true.iter().zip(if_false.iter())
        .map(|(&a, &b)| constant_time_select(cond, a, b))
        .collect()
}

/// Demonstrate timing leak measurement.
/// In a real attack, this requires thousands of samples and statistical analysis.
/// The loop here amplifies the signal to make it visible.
fn demonstrate_timing(secret: &[u8]) {
    use std::time::Instant;

    let mut correct = secret.to_vec();
    correct[0] = secret[0];

    let mut wrong = secret.to_vec();
    wrong[0] ^= 0xFF;

    const ITERATIONS: usize = 1_000_000;

    let start = Instant::now();
    for _ in 0..ITERATIONS {
        let _ = vulnerable_equal(secret, &correct);
    }
    let correct_time = start.elapsed();

    let start = Instant::now();
    for _ in 0..ITERATIONS {
        let _ = vulnerable_equal(secret, &wrong);
    }
    let wrong_time = start.elapsed();

    println!("Vulnerable comparison:");
    println!("  Correct first byte: {:?}", correct_time);
    println!("  Wrong first byte:   {:?}", wrong_time);
    println!("  Difference: {:?}", correct_time.saturating_sub(wrong_time));

    let start = Instant::now();
    for _ in 0..ITERATIONS {
        let _ = secure_equal(secret, &correct);
    }
    let ct_correct = start.elapsed();

    let start = Instant::now();
    for _ in 0..ITERATIONS {
        let _ = secure_equal(secret, &wrong);
    }
    let ct_wrong = start.elapsed();

    println!("\nConstant-time (subtle) comparison:");
    println!("  Correct first byte: {:?}", ct_correct);
    println!("  Wrong first byte:   {:?}", ct_wrong);
    println!("  Difference: {:?}", ct_correct.saturating_sub(ct_wrong));
}

/// The `subtle` crate's `Choice` type prevents accidental non-constant-time use.
/// Demonstrating that you CANNOT accidentally branch on a Choice value:
///
/// This code does NOT compile:
///   let c: Choice = ...;
///   if c { ... }  // ERROR: Choice does not implement bool coercion
///
/// You must explicitly call `.into()` or `.unwrap_u8()`, making the
/// "I'm converting to bool" decision explicit.
fn subtle_choice_safety() {
    let a = [1u8, 2, 3];
    let b = [1u8, 2, 3];
    let c = [1u8, 2, 4];

    let eq_ab: Choice = a.ct_eq(&b);
    let eq_ac: Choice = a.ct_eq(&c);

    // Boolean conversion is explicit
    println!("a == b: {}", bool::from(eq_ab));
    println!("a == c: {}", bool::from(eq_ac));

    // Bitwise operations on Choice are constant-time
    let either_eq = eq_ab | eq_ac; // OR without branching
    println!("a==b OR a==c: {}", bool::from(either_eq));
}

fn main() {
    use rand::RngCore;
    let mut rng = rand::thread_rng();

    let mut secret = [0u8; 32];
    rng.fill_bytes(&mut secret);

    demonstrate_timing(&secret);
    subtle_choice_safety();

    let mut key = [0u8; 32];
    rng.fill_bytes(&mut key);

    let message = b"important payload";
    let mut mac = HmacSha256::new_from_slice(&key).expect("key");
    mac.update(message);
    let tag = mac.finalize().into_bytes();

    println!("\nHMAC verify (correct): {}", verify_hmac(message, &tag, &key));
    let mut bad_tag = tag.to_vec();
    bad_tag[0] ^= 0xFF;
    println!("HMAC verify (tampered): {}", verify_hmac(message, &bad_tag, &key));
}
```

### Rust-specific considerations

**The `subtle` crate is the standard for constant-time operations in Rust.** It is used
by `ring`, `dalek-cryptography`, `rustls`, and every other serious Rust crypto library.
The `ConstantTimeEq`, `ConditionallySelectable`, and `ConstantTimeLess` traits provide
the primitives you need. Do not implement your own.

**`subtle::Choice` prevents accidental bool coercion.** The `Choice` type wraps `u8`
and does not implement `From<Choice> for bool` without explicit `.into()`. This forces
you to acknowledge that you are converting a constant-time value to a branching value.
It does not prevent unsafe code from doing so, but it prevents accidental cases.

**Rust's compiler can optimize constant-time code.** Like Go, the Rust compiler may
optimize away "dead" bitwise operations. The `subtle` crate uses `core::hint::black_box`
and platform-specific techniques (inline assembly on some targets) to prevent this.
Do not write your own constant-time comparison with raw bitwise operations and trust the
compiler not to optimize it.

**`hmac::Mac::verify_slice` is the correct HMAC verification API.** It calls
`subtle::ConstantTimeEq` internally. Use it instead of computing the expected HMAC and
comparing manually.

**Spectre mitigations in Rust.** There is no standard Rust API for inserting `lfence`
instructions for Spectre variant 1 mitigation. Use the `speculative-rs` crate or inline
assembly (`core::arch::asm!`) for security-critical indexed array accesses in software
that runs in a shared environment (e.g., sandbox escapes).

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Constant-time comparison | `crypto/subtle.ConstantTimeCompare` | `subtle::ConstantTimeEq` |
| HMAC verification | `hmac.Equal()` | `hmac::Mac::verify_slice()` |
| Constant-time select | `subtle.ConstantTimeSelect` | `subtle::ConditionallySelectable` |
| Type-enforced constant-time | No — function call convention | `subtle::Choice` prevents bool coercion |
| Compiler optimization risk | Present — verify with `go tool objdump` | Present — `subtle` uses `black_box` |
| AES-NI usage | Automatic in `crypto/aes` | Automatic in `ring`'s AES backend |
| Cache-timing for software AES | Risk if AES-NI absent | `ring` falls back to BoringSSL's bitsliced AES |
| Spectre mitigation | Manual `lfence` via `//go:noescape` asm | `speculative-rs` or inline assembly |
| Meltdown mitigation | OS-level KPTI (transparent) | OS-level KPTI (transparent) |
| Timing noise from GC | Present (complicates remote attacks) | Absent (no GC) |

## Production War Stories

**Lucky13 (TLS 1.2, 2013).** Lucky13 exploited a timing difference in MAC verification
for CBC-padded TLS records. Records with longer padding took more time to process because
the MAC was computed over more bytes. The attacker could distinguish the padding length
from network timing, enabling CBC padding oracle attacks. The fix: constant-time MAC
computation over a fixed-length prefix (independent of actual padding). OpenSSL,
GnuTLS, and NSS all had different implementations of the fix — and several were incorrect
on the first attempt. TLS 1.3 removes CBC modes entirely.

**OpenSSL RSA timing attack (CVE-2018-0737, 2018).** OpenSSL's RSA key generation was
non-constant-time: the Miller-Rabin primality test ran in variable time based on the
candidate prime's bit pattern. An attacker with access to the key generation process
(e.g., co-tenant on a cloud VM, or a malicious process on the same machine) could
reconstruct the generated private key from timing measurements. The fix: add dummy
iterations to normalize the primality test timing.

**BLEED (iSEC Partners, 2014).** Similar to Heartbleed but in a different implementation:
a bounds check failure that exposed heap memory. The analysis of Heartbleed's impact was
that timing attacks on TLS sessions were theoretically possible even before the memory
disclosure, because OpenSSL's HMAC verification was not constant-time at the time.

**Cloudflare's Browser in the Middle (BITMi) observation (2021).** Cloudflare researchers
demonstrated that QUIC implementations that use non-constant-time comparison for connection
ID matching could leak information about active connection IDs to an observer on the same
physical server. The mitigation: connection ID matching must be constant-time.

**Spectre in production (2018–present).** After the Spectre disclosure, browser vendors
reduced `performance.now()` resolution to ~5ms (from ~5µs) to make Spectre-based attacks
harder to mount from JavaScript. This broke many performance measurement tools. Server-
side systems deployed retpolines (which have ~10% performance cost for indirect call-heavy
workloads) and KPTI (which has ~5–25% cost for syscall-heavy workloads). Cloud providers
rolled out microcode updates, added CPU-level isolation between VMs, and deployed Intel
hardware patches. The total economic cost of Spectre/Meltdown mitigations across the
industry was estimated at billions of dollars.

## Security Analysis

**Attack distance taxonomy.** Timing attacks are classified by attacker proximity:

| Attack Type | Attacker Position | Example |
|-------------|-------------------|---------|
| Local timing | Same process | Measure `time.Now()` around comparison |
| Cross-process | Same core (no hyperthreading) | Cache-timing via Prime+Probe |
| Cross-SMT | Hyperthreading sibling | SMT-based port contention attacks |
| Cross-VM | Same physical host | FLUSH+RELOAD via shared memory |
| Network | Separate machine | Lucky13, BEAST, CRIME |
| Remote with oracle | HTTP endpoint | MAC forgery via response-time comparison |

**Defense depth.** No single mitigation covers all attack types. The defense stack:
1. Constant-time operations for all secret comparisons (defends against remote timing).
2. AES-NI for AES operations (defends against software AES cache-timing).
3. No secret-dependent branches in critical paths (defends against cross-process cache).
4. Retpolines + IBRS for indirect calls (defends against Spectre variant 2).
5. KPTI for kernel isolation (defends against Meltdown).
6. Hardware patches (fixes specific Spectre/Meltdown variants in microcode).

**The FLUSH+RELOAD requirement.** FLUSH+RELOAD requires shared memory between attacker
and victim. In a containerized environment, containers on the same host share the kernel
and may share library pages. An attacker in one container can mount FLUSH+RELOAD against
another container if they share physical memory mappings. The mitigation at the
infrastructure level: dedicate physical hosts to sensitive workloads (no co-tenancy).

## Common Pitfalls

1. **Using `==` for HMAC, session token, or password hash comparison.** This is the
   single most common timing attack surface in web applications. Every HMAC verification,
   every API key comparison, every session token validation must use `crypto/subtle.
   ConstantTimeCompare` (Go) or `subtle::ConstantTimeEq` (Rust). No exceptions.

2. **Length check before constant-time comparison.** If you check `len(a) != len(b)` and
   return immediately, and the length itself is secret (e.g., a variable-length token),
   you leak the length. For fixed-size secrets (32-byte HMACs, session tokens), length
   is not secret and the fast-path length check is safe.

3. **Non-constant-time string formatting before comparison.** Some developers compute
   `hex.EncodeToString(expectedHMAC) == hex.EncodeToString(providedHMAC)` thinking the
   hex encoding "normalizes" timing. It does not — `==` on strings is still early-exit.

4. **Trusting compiler optimizations to preserve constant-time properties.** A `for` loop
   with early `return` removed might look constant-time but the compiler may introduce
   a conditional branch as an optimization. Use `crypto/subtle` / `subtle` crate to get
   functions that are verified to be constant-time across Go/Rust compiler versions.

5. **Not accounting for timing noise sources.** Running timing attack tests on a laptop
   with CPU frequency scaling, turbocharged cores, and background processes will show
   large variance that masks constant-time differences. Test on dedicated hardware with
   CPU frequency fixed (`cpupower frequency-set -g performance`), turbocharged disabled,
   and no concurrent workloads.

## Exercises

**Exercise 1** (30 min): Write a Go microbenchmark that measures the average time for
`subtle.ConstantTimeCompare` and `bytes.Equal` on a 32-byte slice where: (a) the first
byte differs, (b) the last byte differs, and (c) all bytes match. Use `testing.B` and
plot the results. Verify that `bytes.Equal` shows different timing for cases (a) and (b),
while `subtle.ConstantTimeCompare` does not.

**Exercise 2** (2–4h): Implement a timing attack in Go against a vulnerable HMAC
verification endpoint. Set up an HTTP handler that verifies an HMAC tag using
`bytes.Equal` (vulnerable). From the client side, send requests with incrementally correct
tag bytes (correct first byte, then correct first two bytes, etc.) and measure response
time. Plot the timing data. Show that you can recover the correct tag byte-by-byte using
the timing oracle.

**Exercise 3** (4–8h): Implement the same HTTP handler using `hmac.Equal` (constant-time)
and repeat the timing attack. Show that the timing oracle no longer works. Add statistical
analysis: run 10,000 requests per probe byte, compute mean and standard deviation, show
that the distributions overlap. This exercise makes "constant-time prevents timing attacks"
empirically concrete.

**Exercise 4** (8–15h): Implement a FLUSH+RELOAD cache-timing measurement tool in Rust.
Using `std::arch::x86_64::_mm_clflush` and `std::arch::x86_64::__rdtsc`, implement
a function that: (a) flushes a cache line, (b) triggers a victim function that reads
a known address, (c) measures the time to reload the cache line. Demonstrate that you
can distinguish "victim accessed this address" from "victim did not access this address"
with >95% accuracy. Apply this to measure which entries of a small lookup table a victim
function accessed (simulating an AES S-box attack against software AES without AES-NI).

## Further Reading

### Foundational Papers

- Kocher — "Timing Attacks on Implementations of Diffie-Hellman, RSA, DSS, and Other
  Systems" (CRYPTO 1996). The original timing attack paper. Demonstrates that key bits
  can be recovered from timing measurements of modular exponentiation.
- Bernstein — "Cache-Timing Attacks on AES" (2005, freely available). Shows that AES in
  software without AES-NI leaks key information via cache timing.
- Yarom and Falkner — "FLUSH+RELOAD: A High Resolution, Low Noise, L3 Cache Side-Channel
  Attack" (USENIX Security 2014). The modern cache-timing attack primitive.
- Kocher, Horn et al. — "Spectre Attacks: Exploiting Speculative Execution" (IEEE S&P
  2019). The Spectre paper.
- Lipp et al. — "Meltdown: Reading Kernel Memory from User Space" (USENIX Security 2018).
  The Meltdown paper.

### Books

- Jean-Philippe Aumasson — "Serious Cryptography" (No Starch, 2017). Chapter 15 covers
  implementation attacks: timing, fault, DPA.
- Stefan Mangard, Elisabeth Oswald, Thomas Popp — "Power Analysis Attacks: Revealing the
  Secrets of Smart Cards" (Springer, 2007). The textbook on side-channel analysis from
  power measurements, which shares the constant-time methodology with timing attacks.

### Production Code to Read

- `crypto/subtle` (Go stdlib, `src/crypto/subtle/`) — the constant-time operations.
  Read `constant_time.go` to see the XOR-accumulate pattern and `//go:noescape` assembly.
- `subtle` crate (`dalek-cryptography/subtle`) — the `Choice` type implementation.
  Read `lib.rs` to understand how `black_box` prevents compiler optimization.
- OpenSSL `ssl/record/ssl3_record.c` — the Lucky13 fix. Study the `ssl3_cbc_digest_record`
  function to see how constant-time MAC verification is implemented at the library level.

### Security Advisories / CVEs to Study

- CVE-2018-0737 (OpenSSL RSA key generation timing) — non-constant-time primality test
  during RSA key generation.
- CVE-2013-0169 (Lucky13) — timing attack on CBC padding validation in TLS.
- CVE-2017-5753 / CVE-2017-5715 (Spectre variants 1 and 2).
- CVE-2017-5754 (Meltdown).
- CVE-2019-1125 (SWAPGS, a Spectre variant affecting Windows and Linux kernel entry).
