# Solution: SHA-256 Hash from Scratch

## Architecture Overview

The implementation mirrors the FIPS 180-4 structure:

1. **Constants** -- 64 round constants `K` and 8 initial hash values `H`, precomputed from prime number roots
2. **Logical functions** -- six bitwise functions (`Ch`, `Maj`, two `Sigma`, two `sigma`) used in the compression rounds
3. **Padding** -- extends the input to a multiple of 64 bytes with the `1` bit, zeros, and 64-bit length
4. **Message schedule** -- expands each 64-byte block from 16 words to 64 words using `sigma0` and `sigma1`
5. **Compression** -- 64 rounds of state manipulation per block, accumulating into the running hash
6. **HMAC** -- wraps SHA-256 with key-derived inner and outer padding per RFC 2104

All arithmetic is `u32` with wrapping addition. The state is eight `u32` values (256 bits total).

## Rust Solution

### Project Setup

```bash
cargo new sha256-scratch
cd sha256-scratch
```

`Cargo.toml`:

```toml
[package]
name = "sha256-scratch"
version = "0.1.0"
edition = "2021"
```

### Source: `src/constants.rs`

```rust
/// First 32 bits of the fractional parts of the cube roots of the first 64 primes.
pub const K: [u32; 64] = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
    0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
    0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
    0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

/// First 32 bits of the fractional parts of the square roots of the first 8 primes.
pub const H_INIT: [u32; 8] = [
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
    0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
];
```

### Source: `src/ops.rs`

```rust
/// Right-rotate a u32 by n bits.
#[inline]
pub fn rotr(x: u32, n: u32) -> u32 {
    x.rotate_right(n)
}

/// Choose: for each bit, if x=1 pick y, else pick z.
#[inline]
pub fn ch(x: u32, y: u32, z: u32) -> u32 {
    (x & y) ^ (!x & z)
}

/// Majority: for each bit, pick the majority of x, y, z.
#[inline]
pub fn maj(x: u32, y: u32, z: u32) -> u32 {
    (x & y) ^ (x & z) ^ (y & z)
}

/// Big sigma 0: used on working variable `a`.
#[inline]
pub fn big_sigma0(x: u32) -> u32 {
    rotr(x, 2) ^ rotr(x, 13) ^ rotr(x, 22)
}

/// Big sigma 1: used on working variable `e`.
#[inline]
pub fn big_sigma1(x: u32) -> u32 {
    rotr(x, 6) ^ rotr(x, 11) ^ rotr(x, 25)
}

/// Small sigma 0: used in message schedule expansion.
#[inline]
pub fn small_sigma0(x: u32) -> u32 {
    rotr(x, 7) ^ rotr(x, 18) ^ (x >> 3)
}

/// Small sigma 1: used in message schedule expansion.
#[inline]
pub fn small_sigma1(x: u32) -> u32 {
    rotr(x, 17) ^ rotr(x, 19) ^ (x >> 10)
}
```

### Source: `src/sha256.rs`

```rust
use crate::constants::{H_INIT, K};
use crate::ops::*;

/// Pad message to a multiple of 64 bytes per FIPS 180-4.
fn pad_message(message: &[u8]) -> Vec<u8> {
    let bit_len = (message.len() as u64) * 8;
    let mut padded = message.to_vec();

    // Append 0x80 (the 1 bit followed by 7 zero bits)
    padded.push(0x80);

    // Pad with zeros until length is 56 mod 64
    while padded.len() % 64 != 56 {
        padded.push(0x00);
    }

    // Append original bit length as 8-byte big-endian
    padded.extend_from_slice(&bit_len.to_be_bytes());

    padded
}

/// Parse a 64-byte block into 16 big-endian u32 words.
fn parse_block(block: &[u8]) -> [u32; 16] {
    let mut words = [0u32; 16];
    for i in 0..16 {
        let offset = i * 4;
        words[i] = u32::from_be_bytes([
            block[offset],
            block[offset + 1],
            block[offset + 2],
            block[offset + 3],
        ]);
    }
    words
}

/// Expand 16 words to 64-word message schedule.
fn expand_schedule(words: &[u32; 16]) -> [u32; 64] {
    let mut w = [0u32; 64];
    w[..16].copy_from_slice(words);
    for i in 16..64 {
        w[i] = small_sigma1(w[i - 2])
            .wrapping_add(w[i - 7])
            .wrapping_add(small_sigma0(w[i - 15]))
            .wrapping_add(w[i - 16]);
    }
    w
}

/// Run 64 compression rounds on a single block.
fn compress(state: &[u32; 8], w: &[u32; 64]) -> [u32; 8] {
    let [mut a, mut b, mut c, mut d, mut e, mut f, mut g, mut h] = *state;

    for i in 0..64 {
        let t1 = h
            .wrapping_add(big_sigma1(e))
            .wrapping_add(ch(e, f, g))
            .wrapping_add(K[i])
            .wrapping_add(w[i]);
        let t2 = big_sigma0(a).wrapping_add(maj(a, b, c));

        h = g;
        g = f;
        f = e;
        e = d.wrapping_add(t1);
        d = c;
        c = b;
        b = a;
        a = t1.wrapping_add(t2);
    }

    [
        state[0].wrapping_add(a),
        state[1].wrapping_add(b),
        state[2].wrapping_add(c),
        state[3].wrapping_add(d),
        state[4].wrapping_add(e),
        state[5].wrapping_add(f),
        state[6].wrapping_add(g),
        state[7].wrapping_add(h),
    ]
}

/// Compute SHA-256 hash of input bytes. Returns 32-byte digest.
pub fn sha256(message: &[u8]) -> [u8; 32] {
    let padded = pad_message(message);
    let mut state = H_INIT;

    for chunk in padded.chunks_exact(64) {
        let words = parse_block(chunk);
        let schedule = expand_schedule(&words);
        state = compress(&state, &schedule);
    }

    let mut digest = [0u8; 32];
    for (i, &word) in state.iter().enumerate() {
        let bytes = word.to_be_bytes();
        digest[i * 4..i * 4 + 4].copy_from_slice(&bytes);
    }
    digest
}

/// Convert 32-byte digest to 64-char hex string.
pub fn hex_digest(digest: &[u8; 32]) -> String {
    digest.iter().map(|b| format!("{:02x}", b)).collect()
}
```

### Source: `src/hmac.rs`

```rust
use crate::sha256::sha256;

const BLOCK_SIZE: usize = 64;
const IPAD: u8 = 0x36;
const OPAD: u8 = 0x5c;

/// HMAC-SHA256 per RFC 2104.
pub fn hmac_sha256(key: &[u8], message: &[u8]) -> [u8; 32] {
    let mut padded_key = [0u8; BLOCK_SIZE];

    if key.len() > BLOCK_SIZE {
        let hashed = sha256(key);
        padded_key[..32].copy_from_slice(&hashed);
    } else {
        padded_key[..key.len()].copy_from_slice(key);
    }

    // Inner hash: SHA256((key XOR ipad) || message)
    let mut inner_input = Vec::with_capacity(BLOCK_SIZE + message.len());
    for &b in &padded_key {
        inner_input.push(b ^ IPAD);
    }
    inner_input.extend_from_slice(message);
    let inner_hash = sha256(&inner_input);

    // Outer hash: SHA256((key XOR opad) || inner_hash)
    let mut outer_input = Vec::with_capacity(BLOCK_SIZE + 32);
    for &b in &padded_key {
        outer_input.push(b ^ OPAD);
    }
    outer_input.extend_from_slice(&inner_hash);
    sha256(&outer_input)
}
```

### Source: `src/lib.rs`

```rust
pub mod constants;
pub mod hmac;
pub mod ops;
pub mod sha256;
```

### Source: `src/main.rs`

```rust
use sha256_scratch::hmac::hmac_sha256;
use sha256_scratch::sha256::{hex_digest, sha256};

fn main() {
    println!("=== SHA-256 Implementation ===\n");

    let test_vectors = [
        ("", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"),
        ("abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"),
        (
            "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq",
            "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1",
        ),
    ];

    for (input, expected) in &test_vectors {
        let digest = sha256(input.as_bytes());
        let hex = hex_digest(&digest);
        let status = if hex == *expected { "OK" } else { "FAIL" };
        println!("[{status}] sha256(\"{input}\")");
        println!("  expected: {expected}");
        println!("  got:      {hex}");
        println!();
    }

    // Longer input: 1 million 'a' characters
    let million_a = vec![b'a'; 1_000_000];
    let digest = sha256(&million_a);
    let hex = hex_digest(&digest);
    let expected = "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0";
    let status = if hex == expected { "OK" } else { "FAIL" };
    println!("[{status}] sha256(\"a\" * 1_000_000)");
    println!("  expected: {expected}");
    println!("  got:      {hex}");

    println!("\n=== HMAC-SHA256 ===\n");

    // RFC 4231 Test Case 2
    let hmac_key = b"Jefe";
    let hmac_msg = b"what do ya want for nothing?";
    let hmac_result = hmac_sha256(hmac_key, hmac_msg);
    let hmac_hex = hex_digest(&hmac_result);
    let hmac_expected = "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843";
    let status = if hmac_hex == hmac_expected { "OK" } else { "FAIL" };
    println!("[{status}] HMAC-SHA256(\"Jefe\", \"what do ya want for nothing?\")");
    println!("  expected: {hmac_expected}");
    println!("  got:      {hmac_hex}");
}
```

### Tests: `src/tests.rs`

Add `mod tests;` to `lib.rs`:

```rust
pub mod constants;
pub mod hmac;
pub mod ops;
pub mod sha256;
#[cfg(test)]
mod tests;
```

```rust
#[cfg(test)]
mod tests {
    use crate::hmac::hmac_sha256;
    use crate::sha256::{hex_digest, sha256};

    #[test]
    fn nist_vector_empty() {
        let digest = sha256(b"");
        assert_eq!(
            hex_digest(&digest),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
    }

    #[test]
    fn nist_vector_abc() {
        let digest = sha256(b"abc");
        assert_eq!(
            hex_digest(&digest),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
    }

    #[test]
    fn nist_vector_two_blocks() {
        let input = b"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq";
        let digest = sha256(input);
        assert_eq!(
            hex_digest(&digest),
            "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"
        );
    }

    #[test]
    fn nist_vector_million_a() {
        let input = vec![b'a'; 1_000_000];
        let digest = sha256(&input);
        assert_eq!(
            hex_digest(&digest),
            "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"
        );
    }

    #[test]
    fn output_is_always_32_bytes() {
        for len in [0, 1, 55, 56, 64, 100, 1000] {
            assert_eq!(sha256(&vec![0x42u8; len]).len(), 32);
        }
    }

    #[test]
    fn deterministic_and_collision_free() {
        assert_eq!(sha256(b"hello"), sha256(b"hello"));
        assert_ne!(sha256(b"hello"), sha256(b"hellp"));
    }

    #[test]
    fn hmac_rfc4231_test_case_1() {
        // Key = 0x0b repeated 20 times, data = "Hi There"
        let key = vec![0x0bu8; 20];
        let data = b"Hi There";
        let result = hmac_sha256(&key, data);
        assert_eq!(
            hex_digest(&result),
            "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"
        );
    }

    #[test]
    fn hmac_rfc4231_test_case_2() {
        let key = b"Jefe";
        let data = b"what do ya want for nothing?";
        let result = hmac_sha256(key, data);
        assert_eq!(
            hex_digest(&result),
            "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
        );
    }

    #[test]
    fn hmac_rfc4231_test_case_3() {
        let key = vec![0xaau8; 20];
        let data = vec![0xddu8; 50];
        let result = hmac_sha256(&key, &data);
        assert_eq!(
            hex_digest(&result),
            "773ea91e36800e46854db8ebd09181a72959098b3ef8c122d9635514ced565fe"
        );
    }

    #[test]
    fn hmac_long_key_hashed() {
        // Key longer than 64 bytes gets hashed first
        let key = vec![0xaau8; 131];
        let data = b"Test Using Larger Than Block-Size Key - Hash Key First";
        let result = hmac_sha256(&key, data);
        assert_eq!(
            hex_digest(&result),
            "60e431591ee0b67f0d8a26aacbf5b77f8e0bc6213728c5140546040f0ee37f54"
        );
    }
}
```

### Running

```bash
cargo build
cargo test
cargo run
```

### Expected Output

```
=== SHA-256 Implementation ===

[OK] sha256("")
  expected: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
  got:      e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

[OK] sha256("abc")
  expected: ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
  got:      ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad

[OK] sha256("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq")
  expected: 248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1
  got:      248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1

[OK] sha256("a" * 1_000_000)
  expected: cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0
  got:      cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0

=== HMAC-SHA256 ===

[OK] HMAC-SHA256("Jefe", "what do ya want for nothing?")
  expected: 5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843
  got:      5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843
```

## Design Decisions

1. **Separate constants module**: The 64 round constants and 8 initial hash values are long arrays. Keeping them in a separate file prevents the core logic from being buried under tables of hex values.

2. **`rotate_right` instead of manual bit shifting**: Rust provides `u32::rotate_right()` as a primitive. Using it produces clearer code and compiles to a single `ror` instruction on x86. The manual alternative (`(x >> n) | (x << (32 - n))`) works identically but is harder to read.

3. **Padding in a separate pass**: The message is fully padded into a new `Vec<u8>` before processing. This uses O(n) extra memory but simplifies the block iteration. A streaming implementation would process blocks on the fly and pad only the final block, saving memory for large inputs.

4. **HMAC as a separate module**: HMAC is built entirely on top of `sha256()` with no access to internal state. This clean separation means the hash function can be swapped (e.g., SHA-512) without changing the HMAC logic.

## Common Mistakes

1. **Mixing up big and small sigma functions**: `big_sigma0/1` use three rotations. `small_sigma0/1` use two rotations and one shift. If you accidentally use a shift where a rotation belongs (or vice versa), the hash computes without errors but produces wrong output. Compare against the NIST test vectors immediately after implementing these functions.

2. **Little-endian instead of big-endian**: SHA-256 is entirely big-endian: the 16 input words, the 64-bit length in padding, and the final digest bytes. Using `u32::from_le_bytes` instead of `from_be_bytes` anywhere silently corrupts the hash.

3. **Padding length field is bits, not bytes**: The 8-byte suffix in the padding is the original message length in *bits*, not bytes. For a 3-byte message, the value is 24, not 3. Forgetting to multiply by 8 is a classic error.

4. **Off-by-one in padding zeros**: The message must be padded to `56 mod 64` bytes *before* appending the 8-byte length, not `64 mod 64`. This is because the length occupies the last 8 bytes of the final block.

## Performance Notes

| Operation | Time Complexity | Space |
|-----------|----------------|-------|
| Padding | O(n) | O(n) extra |
| Per block | O(1) -- 64 rounds of fixed work | O(1) -- 64 words + 8 state vars |
| Full hash | O(n / 64) blocks | O(n) with eager padding |
| HMAC | 3 x SHA-256 calls | O(n) |

SHA-256 processes about 200-400 MB/s in software on modern hardware (single-threaded, no SIMD). Hardware AES-NI-like extensions (SHA-NI) push this to 2+ GB/s. This implementation will be in the 100-200 MB/s range due to Rust's optimization of the core loop.

## Going Further

- Implement **SHA-512** by changing word size to `u64`, round count to 80, and using different rotation amounts -- the structure is identical to SHA-256
- Add **streaming / incremental hashing**: accept data in chunks via `update(&[u8])` and finalize with `digest() -> [u8; 32]`, processing complete blocks immediately and buffering partial blocks
- Implement **Merkle tree hashing**: compute SHA-256 of each data block as a leaf, then hash pairs of hashes up the tree, producing a single root hash that verifies all blocks
- Benchmark against the `sha2` crate and investigate why the optimized version is faster (SIMD, loop unrolling, avoiding bounds checks)
- Implement **length extension attack**: demonstrate how `SHA256(secret || message)` can be extended without knowing the secret, and verify that HMAC prevents this
