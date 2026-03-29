<!-- difficulty: intermediate-advanced -->
<!-- category: compression-encoding -->
<!-- languages: [rust] -->
<!-- concepts: [sha256, cryptographic-hashing, merkle-damgaard, bitwise-operations, hmac, message-padding] -->
<!-- estimated_time: 5-7 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [bitwise-operations, modular-arithmetic, byte-manipulation, hex-encoding, array-manipulation] -->

# Challenge 73: SHA-256 Hash from Scratch

## Languages

Rust (stable, latest edition)

## Prerequisites

- Comfortable with bitwise operations on `u32`: XOR, AND, NOT, right-rotate, right-shift
- Understanding of big-endian byte ordering and `u32`/byte array conversion
- Familiarity with modular arithmetic (`wrapping_add` on `u32`)
- Byte array manipulation and hexadecimal encoding

## Learning Objectives

- **Implement** the SHA-256 hash algorithm following the FIPS 180-4 specification step by step
- **Analyze** how the Merkle-Damgaard construction processes arbitrary-length inputs through fixed-size blocks
- **Apply** bitwise rotation, shifting, and logical functions to build the compression function
- **Differentiate** between the message schedule expansion and the working variable rounds
- **Design** an HMAC-SHA256 construction using the hash as a building block

## The Challenge

SHA-256 is the workhorse of modern cryptography. Every TLS certificate, every Bitcoin block, every digital signature depends on it. It takes an arbitrary-length message and produces a fixed 256-bit (32-byte) digest that is computationally infeasible to reverse or collide. Yet the algorithm itself is surprisingly mechanical: pad the message, split into 512-bit blocks, expand each block into 64 words, run 64 rounds of bitwise operations, and accumulate the result.

Implement SHA-256 from scratch following FIPS 180-4. The message is padded to a multiple of 512 bits: append a `1` bit, then zeros, then the original message length as a 64-bit big-endian integer. Each 512-bit block is processed by the compression function, which takes eight 32-bit working variables through 64 rounds. Each round applies two sigma functions (bitwise rotations and XORs), two choice/majority functions, and adds a round constant from a precomputed table of 64 values derived from the cube roots of the first 64 primes.

The hash state is initialized with eight constants derived from the square roots of the first eight primes. After processing all blocks, the final state is the 256-bit digest.

As an extension, implement HMAC-SHA256: `HMAC(K, M) = SHA256((K XOR opad) || SHA256((K XOR ipad) || M))`, where `ipad` is `0x36` repeated and `opad` is `0x5c` repeated. HMAC turns a hash function into a message authentication code.

## Requirements

1. Define the 64 round constants `K[0..63]` as a `[u32; 64]` array (first 32 bits of the fractional parts of the cube roots of the first 64 primes)
2. Define the 8 initial hash values `H[0..7]` (first 32 bits of the fractional parts of the square roots of the first 8 primes)
3. Implement message padding: append `0x80`, pad with zeros to 56 mod 64 bytes, append original message length in bits as 8-byte big-endian
4. Implement the six logical functions: `Ch(x,y,z)`, `Maj(x,y,z)`, `Sigma0(x)`, `Sigma1(x)`, `sigma0(x)`, `sigma1(x)` using rotate-right and shift-right
5. Implement message schedule expansion: 16 input words expanded to 64 words using `sigma0` and `sigma1`
6. Implement the compression function: 64 rounds updating eight working variables `a..h`
7. Process all 512-bit blocks sequentially, accumulating results into the hash state (Merkle-Damgaard)
8. Output the final 256-bit digest as a 32-byte array and as a 64-character hex string
9. Validate against NIST test vectors: `sha256("")`, `sha256("abc")`, `sha256("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq")`
10. Implement HMAC-SHA256 following RFC 2104: pad key to block size, XOR with ipad/opad, hash twice
11. Write unit tests for padding correctness, known hash values, and HMAC test vectors

## Hints

<details>
<summary>Hint 1: Rotation and logical functions</summary>

```rust
fn rotr(x: u32, n: u32) -> u32 {
    (x >> n) | (x << (32 - n))
}

fn ch(x: u32, y: u32, z: u32) -> u32 {
    (x & y) ^ (!x & z)
}

fn maj(x: u32, y: u32, z: u32) -> u32 {
    (x & y) ^ (x & z) ^ (y & z)
}

fn big_sigma0(x: u32) -> u32 {
    rotr(x, 2) ^ rotr(x, 13) ^ rotr(x, 22)
}

fn big_sigma1(x: u32) -> u32 {
    rotr(x, 6) ^ rotr(x, 11) ^ rotr(x, 25)
}

fn small_sigma0(x: u32) -> u32 {
    rotr(x, 7) ^ rotr(x, 18) ^ (x >> 3)
}

fn small_sigma1(x: u32) -> u32 {
    rotr(x, 17) ^ rotr(x, 19) ^ (x >> 10)
}
```

Note: `big_sigma` uses only rotations, `small_sigma` uses rotation + shift. Getting these mixed up produces valid-looking but wrong hashes.

</details>

<details>
<summary>Hint 2: Message padding layout</summary>

For a message of `L` bytes:
1. Append byte `0x80` (the `1` bit followed by 7 zero bits)
2. Append zero bytes until total length is `56 mod 64` bytes
3. Append `L * 8` (bit length) as 8-byte big-endian

Example: `"abc"` (3 bytes) becomes 3 + 1 + 52 + 8 = 64 bytes (one block).

</details>

<details>
<summary>Hint 3: Compression round structure</summary>

Each of the 64 rounds:
```rust
let t1 = h.wrapping_add(big_sigma1(e))
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
```

All additions are mod 2^32 (`wrapping_add`).

</details>

<details>
<summary>Hint 4: HMAC construction</summary>

HMAC uses two passes: `SHA256((key XOR opad) || SHA256((key XOR ipad) || message))`. If the key is longer than 64 bytes, hash it first. Pad short keys with zeros to 64 bytes. `ipad` is `0x36` repeated, `opad` is `0x5c` repeated. The outer hash takes the inner hash as input, not the message.

</details>

## Acceptance Criteria

- [ ] `sha256("")` == `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
- [ ] `sha256("abc")` == `ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad`
- [ ] `sha256("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq")` == `248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1`
- [ ] Output is always exactly 32 bytes (256 bits) regardless of input length
- [ ] Padding produces the correct number of blocks (single block for <= 55 bytes, two blocks for 56-64 bytes)
- [ ] Different inputs produce different hashes (no trivial collisions in your test set)
- [ ] The same input always produces the same hash (deterministic)
- [ ] Messages longer than one block (> 64 bytes) hash correctly
- [ ] HMAC-SHA256 matches RFC 4231 test vectors
- [ ] No dependencies beyond `std`
- [ ] All tests pass with `cargo test`

## Research Resources

- [FIPS 180-4: Secure Hash Standard](https://csrc.nist.gov/pubs/fips/180-4/upd1/final) -- the official SHA-256 specification with all constants and pseudocode
- [SHA-2 -- Wikipedia](https://en.wikipedia.org/wiki/SHA-2) -- algorithm overview with pseudocode and worked examples
- [RFC 2104: HMAC](https://datatracker.ietf.org/doc/html/rfc2104) -- the HMAC construction specification
- [RFC 4231: HMAC-SHA Test Vectors](https://datatracker.ietf.org/doc/html/rfc4231) -- test cases for HMAC-SHA256 validation
- [Mining Bitcoin with pencil and paper (Ken Shirriff)](https://www.righto.com/2014/09/mining-bitcoin-with-pencil-and-paper.html) -- hand-computed SHA-256 round, excellent for building intuition
