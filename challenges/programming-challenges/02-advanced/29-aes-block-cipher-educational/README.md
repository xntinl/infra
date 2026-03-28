<!-- difficulty: advanced -->
<!-- category: cryptography -->
<!-- languages: [rust] -->
<!-- concepts: [aes, block-cipher, galois-field, s-box, ecb, cbc, key-expansion] -->
<!-- estimated_time: 8-12 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [rust-basics, bitwise-operations, modular-arithmetic, array-manipulation, hex-encoding] -->

# Challenge 29: AES Block Cipher Educational Implementation

## Languages

Rust (stable, latest edition)

## Prerequisites

- Comfortable with bitwise operations: XOR, shift, rotate, and their behavior on `u8` values
- Understanding of hexadecimal representation and conversion between hex strings and byte arrays
- Familiarity with modular arithmetic concepts (not number theory -- just the idea that operations wrap around within a finite set)
- Array indexing and 2D matrix-style operations in Rust (`[[u8; 4]; 4]`)
- Basic understanding of symmetric encryption: same key encrypts and decrypts
- Awareness of block cipher modes (ECB processes blocks independently, CBC chains them)

## Learning Objectives

- **Implement** all four AES-128 round operations (SubBytes, ShiftRows, MixColumns, AddRoundKey) and their inverses from the FIPS 197 specification
- **Analyze** how each operation contributes to confusion and diffusion in the cipher
- **Evaluate** the security implications of ECB mode versus CBC mode using visual patterns
- **Design** a step-by-step state visualization that reveals the transformation at each round
- **Implement** AES key expansion using the Rijndael key schedule

## The Challenge

AES (Advanced Encryption Standard) is the most widely deployed symmetric cipher in the world. Every HTTPS connection, every encrypted disk, every secure messaging app uses it. Yet most developers treat it as a black box. This challenge opens the box.

Implement AES-128 encryption and decryption from scratch. AES-128 uses a 128-bit key and processes data in 128-bit (16-byte) blocks through 10 rounds. Each round applies four operations:

- **SubBytes**: Each byte is replaced by its corresponding entry in the S-box, a fixed 256-byte substitution table derived from the multiplicative inverse in GF(2^8) followed by an affine transformation. This provides confusion -- making the relationship between key and ciphertext complex.
- **ShiftRows**: Each row of the 4x4 state is cyclically shifted left by its row index (row 0 by 0, row 1 by 1, etc.). This provides diffusion across columns.
- **MixColumns**: Each column is treated as a polynomial over GF(2^8) and multiplied by a fixed polynomial. This mixes bytes within columns, providing diffusion across rows.
- **AddRoundKey**: The state is XORed with a 128-bit round key derived from the original key. This is where the secret key enters the computation.

The key expansion algorithm derives 11 round keys from the original 128-bit key using a recursive process involving RotWord, SubWord, and round constants.

This is an educational implementation. It will be correct but not constant-time, not resistant to cache-timing attacks, and not suitable for production use. The goal is understanding, not deployment. Include a visualization mode that prints the 4x4 state matrix after every operation in every round, so you can trace exactly how 16 bytes of plaintext are transformed into 16 bytes of ciphertext across all 10 rounds.

Implement both ECB (Electronic Codebook) and CBC (Cipher Block Chaining) modes. ECB encrypts each block independently, which means identical plaintext blocks produce identical ciphertext blocks -- a devastating weakness for structured data. The famous "ECB penguin" image demonstrates this: encrypting a bitmap in ECB mode preserves the visual structure. CBC chains blocks together using the previous ciphertext as input to the next encryption, eliminating this pattern leakage.

**WARNING**: Do not use this implementation for any real security purpose. Use the `aes` crate or a hardware AES-NI implementation for production. This implementation leaks timing information through data-dependent S-box lookups (Bernstein 2005).

## Requirements

1. Implement the AES S-box as a `[u8; 256]` lookup table and the inverse S-box for decryption. The S-box values come from FIPS 197 Section 5.1.1 -- include the full tables as constants
2. Implement `xtime(a: u8) -> u8`: multiplication by 2 in GF(2^8) with reduction by the irreducible polynomial `0x11B`. This is the foundation for all MixColumns arithmetic
3. Implement `gf_mul(a: u8, b: u8) -> u8`: general multiplication in GF(2^8) using repeated `xtime` and XOR (shift-and-add over the binary field)
4. Implement SubBytes: substitute each byte in the 4x4 state matrix using the S-box
5. Implement ShiftRows: cyclically shift row `i` left by `i` positions (row 0 unchanged, row 1 by 1, row 2 by 2, row 3 by 3)
6. Implement MixColumns: for each column, multiply by the fixed matrix using `gf_mul` with constants 2, 3, 1, 1
7. Implement AddRoundKey: XOR the state with the current 128-bit round key
8. Implement all four inverse operations: InvSubBytes (inverse S-box lookup), InvShiftRows (shift right instead of left), InvMixColumns (multiply by the inverse matrix with constants 14, 11, 13, 9), and AddRoundKey (XOR is its own inverse)
9. Implement key expansion (Rijndael key schedule): generate 11 round keys from the 128-bit input key using RotWord (rotate 4-byte word left by 1), SubWord (apply S-box to each byte), and Rcon (round constants)
10. Implement full block encryption: initial AddRoundKey, then 9 rounds of (SubBytes, ShiftRows, MixColumns, AddRoundKey), then a final round of (SubBytes, ShiftRows, AddRoundKey) without MixColumns
11. Implement full block decryption with the correct inverse operation ordering
12. Implement `bytes_to_state` and `state_to_bytes` conversion functions following the column-major mapping defined in FIPS 197 Section 3.4
13. Implement ECB mode: encrypt/decrypt each 16-byte block independently
14. Implement CBC mode: XOR each plaintext block with the previous ciphertext block (or IV for the first block) before encryption
15. Implement PKCS#7 padding: pad to 16-byte boundary, with a full block of padding when the input is already aligned
16. Implement a verbose/visualization mode that prints the 4x4 state as a hex grid after each operation in each round
17. Validate against the FIPS 197 Appendix B test vectors: key `2b7e151628aed2a6abf7158809cf4f3c`, plaintext `3243f6a8885a308d313198a2e0370734`, expected ciphertext `3925841d02dc09fbdc118597196a0b32`

## Hints

1. The state matrix is 4x4 bytes, stored column-major: `state[row][col]`. The input bytes fill
   columns first: byte 0 goes to `state[0][0]`, byte 1 to `state[1][0]`, byte 2 to
   `state[2][0]`, byte 3 to `state[3][0]`, byte 4 to `state[0][1]`. Getting this mapping wrong
   makes every operation produce wrong results. Test your `bytes_to_state` and `state_to_bytes`
   functions independently before building the round operations on top of them. FIPS 197
   Section 3.4 defines this layout explicitly.

2. Galois field multiplication is the trickiest operation. Start with `xtime(a)`: shift left
   by 1, and if the high bit was set (overflow), XOR with `0x1B`. Then `gf_mul(a, 3)` is
   `xtime(a) ^ a`. For decryption you need multiply by 9, 11, 13, 14. Build a general
   `gf_mul(a, b)` that iterates over bits of `b`: if bit `i` is set, XOR the current value
   of `a` into the result, then apply `xtime` to `a`. This shift-and-XOR approach mirrors
   schoolbook multiplication but in GF(2^8). The irreducible polynomial is
   `x^8 + x^4 + x^3 + x + 1` (0x11B).

3. Key expansion generates 44 words (32-bit values) from the original 4-word key. Words 0-3
   are the key itself. For word `i` where `i % 4 == 0`: apply RotWord (rotate left by 1 byte),
   SubWord (S-box each byte), and XOR with Rcon. The round constants are:
   `[0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x1b, 0x36]`. For other words:
   `w[i] = w[i-4] ^ w[i-1]`. Group words 4 at a time to form round keys.

4. For CBC decryption, you XOR the decrypted block with the **previous ciphertext block**, not
   the previous plaintext. A common mistake is XORing with plaintext, which produces correct
   output for the first block (XORed with IV) but garbage from the second block onward. Draw
   the CBC diagram on paper with arrows showing what gets XORed with what during both
   encryption and decryption.

## Acceptance Criteria

- [ ] Encryption of the FIPS 197 Appendix B test vector produces the exact expected ciphertext:
      key `2b7e1516...`, plaintext `3243f6a8...` -> ciphertext `3925841d...`
- [ ] Decryption of the FIPS 197 test vector ciphertext produces the exact original plaintext
- [ ] `encrypt(decrypt(data)) == data` for arbitrary 16-byte blocks with arbitrary keys
- [ ] Key expansion produces 11 round keys; round key 0 equals the input key
- [ ] `xtime(0x57) == 0xae` and `gf_mul(0x57, 0x83) == 0xc1` (FIPS 197 Section 4.2.1)
- [ ] ShiftRows followed by InvShiftRows is identity for any state
- [ ] MixColumns followed by InvMixColumns is identity for any state
- [ ] `bytes_to_state(state_to_bytes(state)) == state` (column-major round-trip)
- [ ] Two different keys produce different ciphertext for the same plaintext
- [ ] ECB: identical plaintext blocks produce identical ciphertext blocks
- [ ] CBC: identical plaintext blocks produce different ciphertext blocks
- [ ] CBC: different IVs produce different ciphertext for the same plaintext and key
- [ ] PKCS#7: padding and unpadding round-trip for inputs of 1 through 16 bytes
- [ ] PKCS#7: 16-byte aligned input gets a full 16-byte padding block appended
- [ ] PKCS#7: unpadding rejects invalid padding (wrong padding bytes, zero padding value)
- [ ] Visualization mode prints the 4x4 state matrix in hex after every operation in every round
- [ ] No dependencies beyond `std` -- all S-box values, GF arithmetic, and key schedule
      are self-contained
- [ ] All tests pass with `cargo test`

## Research Resources

- [FIPS 197: Advanced Encryption Standard](https://csrc.nist.gov/pubs/fips/197/final) -- the official AES specification with test vectors in Appendix B. Read sections 5.1 through 5.4 for the four round operations and section 5.2 for the key expansion
- [AES -- Wikipedia](https://en.wikipedia.org/wiki/Advanced_Encryption_Standard) -- overview of the algorithm, its history, and the competition that selected Rijndael
- [Rijndael MixColumns -- Wikipedia](https://en.wikipedia.org/wiki/Rijndael_MixColumns) -- detailed explanation of Galois field multiplication in AES, including worked examples
- [A Stick Figure Guide to AES](https://www.moserware.com/2009/09/stick-figure-guide-to-advanced.html) -- visual walkthrough of the full algorithm with intuitive explanations
- [Block Cipher Mode of Operation -- Wikipedia](https://en.wikipedia.org/wiki/Block_cipher_mode_of_operation) -- ECB vs CBC with the famous ECB penguin image demonstrating why ECB leaks structure
- [Galois Field Arithmetic -- Wikipedia](https://en.wikipedia.org/wiki/Finite_field_arithmetic) -- the mathematical foundation for MixColumns (GF(2^8) with the AES irreducible polynomial)
- [PKCS #7 Padding](https://en.wikipedia.org/wiki/Padding_(cryptography)#PKCS#5_and_PKCS#7) -- the padding scheme for block-aligned encryption
- [Cache-timing attacks on AES (Bernstein, 2005)](https://cr.yp.to/antiforgery/cachetiming-20050414.pdf) -- why lookup-table AES is vulnerable to side-channel attacks, demonstrating the gap between correctness and security
- [AES Animation (Rijndael)](https://formaestudio.com/rijndaelinspector/archivos/Rijndael_Animation_v4_eng-html5.html) -- interactive step-by-step animation of each round operation
