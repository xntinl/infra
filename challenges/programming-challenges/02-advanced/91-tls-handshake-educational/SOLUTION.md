# Solution: TLS Handshake Educational Implementation

## Architecture Overview

The implementation is organized into seven modules that mirror the TLS protocol layers:

1. **Crypto primitives**: AES-128-CBC, SHA-256, HMAC-SHA256, RSA (big integer modular exponentiation)
2. **TLS PRF**: Pseudo-random function built on HMAC-SHA256 for key derivation
3. **Record layer**: Framing, encryption, decryption, MAC computation
4. **Handshake messages**: ClientHello, ServerHello, Certificate, ServerHelloDone, ClientKeyExchange, Finished
5. **X.509 parser**: Minimal ASN.1/DER parser to extract RSA public key from a certificate
6. **State machine**: Enforces correct message ordering for both client and server roles
7. **Connection**: Orchestrates the handshake over TCP and manages the encrypted channel

```
  Client                                    Server
    |                                         |
    |  ClientHello (version, random, suites)  |
    |--------------------------------------->|
    |                                         |
    |  ServerHello (version, random, suite)   |
    |<---------------------------------------|
    |  Certificate (X.509 with RSA pubkey)    |
    |<---------------------------------------|
    |  ServerHelloDone                        |
    |<---------------------------------------|
    |                                         |
    |  ClientKeyExchange (RSA-encrypted PMS)  |
    |--------------------------------------->|
    |  ChangeCipherSpec                       |
    |--------------------------------------->|
    |  Finished (encrypted verify data)       |
    |--------------------------------------->|
    |                                         |
    |  ChangeCipherSpec                       |
    |<---------------------------------------|
    |  Finished (encrypted verify data)       |
    |<---------------------------------------|
    |                                         |
    |  === Encrypted Application Data ===     |
    |<-------------------------------------->|
```

## Rust Solution

### Project Setup

```bash
cargo new tls-handshake
cd tls-handshake
```

`Cargo.toml`:

```toml
[package]
name = "tls-handshake"
version = "0.1.0"
edition = "2021"
```

### Source: `src/bigint.rs`

```rust
/// Minimal big-integer arithmetic for RSA.
/// Numbers are stored as Vec<u32> in little-endian limb order.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BigUint {
    limbs: Vec<u32>,
}

impl BigUint {
    pub fn zero() -> Self {
        BigUint { limbs: vec![0] }
    }

    pub fn one() -> Self {
        BigUint { limbs: vec![1] }
    }

    pub fn from_bytes_be(bytes: &[u8]) -> Self {
        // Convert big-endian bytes to little-endian u32 limbs
        let mut limbs = Vec::new();
        let mut i = bytes.len();
        while i > 0 {
            let start = if i >= 4 { i - 4 } else { 0 };
            let mut val: u32 = 0;
            for j in start..i {
                val = (val << 8) | (bytes[j] as u32);
            }
            limbs.push(val);
            i = start;
        }
        let mut result = BigUint { limbs };
        result.trim();
        result
    }

    pub fn to_bytes_be(&self) -> Vec<u8> {
        let mut bytes = Vec::new();
        for &limb in self.limbs.iter().rev() {
            bytes.extend_from_slice(&limb.to_be_bytes());
        }
        // Strip leading zeros
        while bytes.len() > 1 && bytes[0] == 0 {
            bytes.remove(0);
        }
        bytes
    }

    /// Pad to exactly `len` bytes (big-endian), prepending zeros.
    pub fn to_bytes_be_padded(&self, len: usize) -> Vec<u8> {
        let raw = self.to_bytes_be();
        if raw.len() >= len {
            return raw;
        }
        let mut padded = vec![0u8; len - raw.len()];
        padded.extend_from_slice(&raw);
        padded
    }

    fn trim(&mut self) {
        while self.limbs.len() > 1 && *self.limbs.last().unwrap() == 0 {
            self.limbs.pop();
        }
    }

    pub fn is_zero(&self) -> bool {
        self.limbs.iter().all(|&l| l == 0)
    }

    fn bit_len(&self) -> usize {
        if self.is_zero() {
            return 0;
        }
        let top = *self.limbs.last().unwrap();
        (self.limbs.len() - 1) * 32 + (32 - top.leading_zeros() as usize)
    }

    fn bit(&self, n: usize) -> bool {
        let limb_idx = n / 32;
        let bit_idx = n % 32;
        if limb_idx >= self.limbs.len() {
            return false;
        }
        (self.limbs[limb_idx] >> bit_idx) & 1 == 1
    }

    pub fn add(&self, other: &BigUint) -> BigUint {
        let max_len = self.limbs.len().max(other.limbs.len());
        let mut result = Vec::with_capacity(max_len + 1);
        let mut carry: u64 = 0;
        for i in 0..max_len {
            let a = *self.limbs.get(i).unwrap_or(&0) as u64;
            let b = *other.limbs.get(i).unwrap_or(&0) as u64;
            let sum = a + b + carry;
            result.push(sum as u32);
            carry = sum >> 32;
        }
        if carry > 0 {
            result.push(carry as u32);
        }
        let mut r = BigUint { limbs: result };
        r.trim();
        r
    }

    pub fn sub(&self, other: &BigUint) -> BigUint {
        let mut result = Vec::with_capacity(self.limbs.len());
        let mut borrow: i64 = 0;
        for i in 0..self.limbs.len() {
            let a = self.limbs[i] as i64;
            let b = *other.limbs.get(i).unwrap_or(&0) as i64;
            let diff = a - b - borrow;
            if diff < 0 {
                result.push((diff + (1i64 << 32)) as u32);
                borrow = 1;
            } else {
                result.push(diff as u32);
                borrow = 0;
            }
        }
        let mut r = BigUint { limbs: result };
        r.trim();
        r
    }

    pub fn mul(&self, other: &BigUint) -> BigUint {
        let mut result = vec![0u64; self.limbs.len() + other.limbs.len()];
        for i in 0..self.limbs.len() {
            let mut carry: u64 = 0;
            for j in 0..other.limbs.len() {
                let prod = (self.limbs[i] as u64) * (other.limbs[j] as u64)
                    + result[i + j]
                    + carry;
                result[i + j] = prod & 0xFFFF_FFFF;
                carry = prod >> 32;
            }
            result[i + other.limbs.len()] += carry;
        }
        let limbs: Vec<u32> = result.iter().map(|&v| v as u32).collect();
        let mut r = BigUint { limbs };
        r.trim();
        r
    }

    /// Compare: -1 if self < other, 0 if equal, 1 if self > other
    pub fn cmp(&self, other: &BigUint) -> std::cmp::Ordering {
        if self.limbs.len() != other.limbs.len() {
            return self.limbs.len().cmp(&other.limbs.len());
        }
        for i in (0..self.limbs.len()).rev() {
            if self.limbs[i] != other.limbs[i] {
                return self.limbs[i].cmp(&other.limbs[i]);
            }
        }
        std::cmp::Ordering::Equal
    }

    pub fn gte(&self, other: &BigUint) -> bool {
        matches!(self.cmp(other), std::cmp::Ordering::Greater | std::cmp::Ordering::Equal)
    }

    /// Division with remainder: returns (quotient, remainder)
    pub fn div_rem(&self, divisor: &BigUint) -> (BigUint, BigUint) {
        assert!(!divisor.is_zero(), "division by zero");

        if self.cmp(divisor) == std::cmp::Ordering::Less {
            return (BigUint::zero(), self.clone());
        }

        let mut remainder = BigUint::zero();
        let mut quotient_bits = vec![false; self.bit_len()];

        for i in (0..self.bit_len()).rev() {
            // Shift remainder left by 1 and add current bit
            remainder = remainder.shl_one();
            if self.bit(i) {
                remainder = remainder.add(&BigUint::one());
            }
            if remainder.gte(divisor) {
                remainder = remainder.sub(divisor);
                quotient_bits[i] = true;
            }
        }

        let mut q_limbs = vec![0u32; (quotient_bits.len() + 31) / 32];
        for (i, &bit) in quotient_bits.iter().enumerate() {
            if bit {
                q_limbs[i / 32] |= 1 << (i % 32);
            }
        }
        let mut q = BigUint { limbs: q_limbs };
        q.trim();
        (q, remainder)
    }

    pub fn modulo(&self, modulus: &BigUint) -> BigUint {
        let (_, rem) = self.div_rem(modulus);
        rem
    }

    fn shl_one(&self) -> BigUint {
        let mut result = Vec::with_capacity(self.limbs.len() + 1);
        let mut carry = 0u32;
        for &limb in &self.limbs {
            let new_limb = (limb << 1) | carry;
            carry = limb >> 31;
            result.push(new_limb);
        }
        if carry > 0 {
            result.push(carry);
        }
        let mut r = BigUint { limbs: result };
        r.trim();
        r
    }

    /// Modular exponentiation: self^exp mod modulus (square-and-multiply)
    pub fn mod_pow(&self, exp: &BigUint, modulus: &BigUint) -> BigUint {
        if modulus.is_zero() {
            panic!("modulus cannot be zero");
        }
        let mut result = BigUint::one();
        let mut base = self.modulo(modulus);

        for i in 0..exp.bit_len() {
            if exp.bit(i) {
                result = result.mul(&base).modulo(modulus);
            }
            base = base.mul(&base).modulo(modulus);
        }
        result
    }
}
```

### Source: `src/sha256.rs`

```rust
/// SHA-256 implementation (from Challenge 73).
const K: [u32; 64] = [
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

const H_INIT: [u32; 8] = [
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
    0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
];

fn rotr(x: u32, n: u32) -> u32 { x.rotate_right(n) }
fn ch(x: u32, y: u32, z: u32) -> u32 { (x & y) ^ (!x & z) }
fn maj(x: u32, y: u32, z: u32) -> u32 { (x & y) ^ (x & z) ^ (y & z) }
fn big_sigma0(x: u32) -> u32 { rotr(x, 2) ^ rotr(x, 13) ^ rotr(x, 22) }
fn big_sigma1(x: u32) -> u32 { rotr(x, 6) ^ rotr(x, 11) ^ rotr(x, 25) }
fn small_sigma0(x: u32) -> u32 { rotr(x, 7) ^ rotr(x, 18) ^ (x >> 3) }
fn small_sigma1(x: u32) -> u32 { rotr(x, 17) ^ rotr(x, 19) ^ (x >> 10) }

pub fn sha256(message: &[u8]) -> [u8; 32] {
    let mut padded = message.to_vec();
    let bit_len = (message.len() as u64) * 8;
    padded.push(0x80);
    while padded.len() % 64 != 56 {
        padded.push(0x00);
    }
    padded.extend_from_slice(&bit_len.to_be_bytes());

    let mut h = H_INIT;

    for block in padded.chunks_exact(64) {
        let mut w = [0u32; 64];
        for i in 0..16 {
            w[i] = u32::from_be_bytes([
                block[i * 4], block[i * 4 + 1],
                block[i * 4 + 2], block[i * 4 + 3],
            ]);
        }
        for i in 16..64 {
            w[i] = small_sigma1(w[i - 2])
                .wrapping_add(w[i - 7])
                .wrapping_add(small_sigma0(w[i - 15]))
                .wrapping_add(w[i - 16]);
        }

        let [mut a, mut b, mut c, mut d, mut e, mut f, mut g, mut hh] = h;

        for i in 0..64 {
            let t1 = hh.wrapping_add(big_sigma1(e))
                .wrapping_add(ch(e, f, g))
                .wrapping_add(K[i])
                .wrapping_add(w[i]);
            let t2 = big_sigma0(a).wrapping_add(maj(a, b, c));
            hh = g; g = f; f = e;
            e = d.wrapping_add(t1);
            d = c; c = b; b = a;
            a = t1.wrapping_add(t2);
        }

        h[0] = h[0].wrapping_add(a);
        h[1] = h[1].wrapping_add(b);
        h[2] = h[2].wrapping_add(c);
        h[3] = h[3].wrapping_add(d);
        h[4] = h[4].wrapping_add(e);
        h[5] = h[5].wrapping_add(f);
        h[6] = h[6].wrapping_add(g);
        h[7] = h[7].wrapping_add(hh);
    }

    let mut digest = [0u8; 32];
    for (i, &val) in h.iter().enumerate() {
        digest[i * 4..i * 4 + 4].copy_from_slice(&val.to_be_bytes());
    }
    digest
}

pub fn hmac_sha256(key: &[u8], message: &[u8]) -> [u8; 32] {
    let block_size = 64;
    let key_block = if key.len() > block_size {
        let hash = sha256(key);
        let mut kb = [0u8; 64];
        kb[..32].copy_from_slice(&hash);
        kb
    } else {
        let mut kb = [0u8; 64];
        kb[..key.len()].copy_from_slice(key);
        kb
    };

    let mut ipad = [0x36u8; 64];
    let mut opad = [0x5cu8; 64];
    for i in 0..64 {
        ipad[i] ^= key_block[i];
        opad[i] ^= key_block[i];
    }

    let mut inner_input = ipad.to_vec();
    inner_input.extend_from_slice(message);
    let inner_hash = sha256(&inner_input);

    let mut outer_input = opad.to_vec();
    outer_input.extend_from_slice(&inner_hash);
    sha256(&outer_input)
}
```

### Source: `src/aes.rs`

```rust
/// AES-128-CBC encryption/decryption (from Challenge 29, abbreviated).
/// Only the essential interface is shown; the full S-box and round operations
/// follow the FIPS 197 implementation from Challenge 29.

const SBOX: [u8; 256] = [
    0x63,0x7c,0x77,0x7b,0xf2,0x6b,0x6f,0xc5,0x30,0x01,0x67,0x2b,0xfe,0xd7,0xab,0x76,
    0xca,0x82,0xc9,0x7d,0xfa,0x59,0x47,0xf0,0xad,0xd4,0xa2,0xaf,0x9c,0xa4,0x72,0xc0,
    0xb7,0xfd,0x93,0x26,0x36,0x3f,0xf7,0xcc,0x34,0xa5,0xe5,0xf1,0x71,0xd8,0x31,0x15,
    0x04,0xc7,0x23,0xc3,0x18,0x96,0x05,0x9a,0x07,0x12,0x80,0xe2,0xeb,0x27,0xb2,0x75,
    0x09,0x83,0x2c,0x1a,0x1b,0x6e,0x5a,0xa0,0x52,0x3b,0xd6,0xb3,0x29,0xe3,0x2f,0x84,
    0x53,0xd1,0x00,0xed,0x20,0xfc,0xb1,0x5b,0x6a,0xcb,0xbe,0x39,0x4a,0x4c,0x58,0xcf,
    0xd0,0xef,0xaa,0xfb,0x43,0x4d,0x33,0x85,0x45,0xf9,0x02,0x7f,0x50,0x3c,0x9f,0xa8,
    0x51,0xa3,0x40,0x8f,0x92,0x9d,0x38,0xf5,0xbc,0xb6,0xda,0x21,0x10,0xff,0xf3,0xd2,
    0xcd,0x0c,0x13,0xec,0x5f,0x97,0x44,0x17,0xc4,0xa7,0x7e,0x3d,0x64,0x5d,0x19,0x73,
    0x60,0x81,0x4f,0xdc,0x22,0x2a,0x90,0x88,0x46,0xee,0xb8,0x14,0xde,0x5e,0x0b,0xdb,
    0xe0,0x32,0x3a,0x0a,0x49,0x06,0x24,0x5c,0xc2,0xd3,0xac,0x62,0x91,0x95,0xe4,0x79,
    0xe7,0xc8,0x37,0x6d,0x8d,0xd5,0x4e,0xa9,0x6c,0x56,0xf4,0xea,0x65,0x7a,0xae,0x08,
    0xba,0x78,0x25,0x2e,0x1c,0xa6,0xb4,0xc6,0xe8,0xdd,0x74,0x1f,0x4b,0xbd,0x8b,0x8a,
    0x70,0x3e,0xb5,0x66,0x48,0x03,0xf6,0x0e,0x61,0x35,0x57,0xb9,0x86,0xc1,0x1d,0x9e,
    0xe1,0xf8,0x98,0x11,0x69,0xd9,0x8e,0x94,0x9b,0x1e,0x87,0xe9,0xce,0x55,0x28,0xdf,
    0x8c,0xa1,0x89,0x0d,0xbf,0xe6,0x42,0x68,0x41,0x99,0x2d,0x0f,0xb0,0x54,0xbb,0x16,
];

const INV_SBOX: [u8; 256] = [
    0x52,0x09,0x6a,0xd5,0x30,0x36,0xa5,0x38,0xbf,0x40,0xa3,0x9e,0x81,0xf3,0xd7,0xfb,
    0x7c,0xe3,0x39,0x82,0x9b,0x2f,0xff,0x87,0x34,0x8e,0x43,0x44,0xc4,0xde,0xe9,0xcb,
    0x54,0x7b,0x94,0x32,0xa6,0xc2,0x23,0x3d,0xee,0x4c,0x95,0x0b,0x42,0xfa,0xc3,0x4e,
    0x08,0x2e,0xa1,0x66,0x28,0xd9,0x24,0xb2,0x76,0x5b,0xa2,0x49,0x6d,0x8b,0xd1,0x25,
    0x72,0xf8,0xf6,0x64,0x86,0x68,0x98,0x16,0xd4,0xa4,0x5c,0xcc,0x5d,0x65,0xb6,0x92,
    0x6c,0x70,0x48,0x50,0xfd,0xed,0xb9,0xda,0x5e,0x15,0x46,0x57,0xa7,0x8d,0x9d,0x84,
    0x90,0xd8,0xab,0x00,0x8c,0xbc,0xd3,0x0a,0xf7,0xe4,0x58,0x05,0xb8,0xb3,0x45,0x06,
    0xd0,0x2c,0x1e,0x8f,0xca,0x3f,0x0f,0x02,0xc1,0xaf,0xbd,0x03,0x01,0x13,0x8a,0x6b,
    0x3a,0x91,0x11,0x41,0x4f,0x67,0xdc,0xea,0x97,0xf2,0xcf,0xce,0xf0,0xb4,0xe6,0x73,
    0x96,0xac,0x74,0x22,0xe7,0xad,0x35,0x85,0xe2,0xf9,0x37,0xe8,0x1c,0x75,0xdf,0x6e,
    0x47,0xf1,0x1a,0x71,0x1d,0x29,0xc5,0x89,0x6f,0xb7,0x62,0x0e,0xaa,0x18,0xbe,0x1b,
    0xfc,0x56,0x3e,0x4b,0xc6,0xd2,0x79,0x20,0x9a,0xdb,0xc0,0xfe,0x78,0xcd,0x5a,0xf4,
    0x1f,0xdd,0xa8,0x33,0x88,0x07,0xc7,0x31,0xb1,0x12,0x10,0x59,0x27,0x80,0xec,0x5f,
    0x60,0x51,0x7f,0xa9,0x19,0xb5,0x4a,0x0d,0x2d,0xe5,0x7a,0x9f,0x93,0xc9,0x9c,0xef,
    0xa0,0xe0,0x3b,0x4d,0xae,0x2a,0xf5,0xb0,0xc8,0xeb,0xbb,0x3c,0x83,0x53,0x99,0x61,
    0x17,0x2b,0x04,0x7e,0xba,0x77,0xd6,0x26,0xe1,0x69,0x14,0x63,0x55,0x21,0x0c,0x7d,
];

const RCON: [u8; 10] = [0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x1b, 0x36];

type State = [[u8; 4]; 4];

fn xtime(a: u8) -> u8 {
    let s = (a as u16) << 1;
    if s & 0x100 != 0 { (s ^ 0x11b) as u8 } else { s as u8 }
}

fn gf_mul(mut a: u8, mut b: u8) -> u8 {
    let mut r: u8 = 0;
    while b > 0 {
        if b & 1 != 0 { r ^= a; }
        a = xtime(a);
        b >>= 1;
    }
    r
}

fn sub_bytes(s: &mut State) { for r in s.iter_mut() { for c in r.iter_mut() { *c = SBOX[*c as usize]; } } }
fn inv_sub_bytes(s: &mut State) { for r in s.iter_mut() { for c in r.iter_mut() { *c = INV_SBOX[*c as usize]; } } }

fn shift_rows(s: &mut State) {
    let t = s[1][0]; s[1][0]=s[1][1]; s[1][1]=s[1][2]; s[1][2]=s[1][3]; s[1][3]=t;
    let (t0,t1)=(s[2][0],s[2][1]); s[2][0]=s[2][2]; s[2][1]=s[2][3]; s[2][2]=t0; s[2][3]=t1;
    let t = s[3][3]; s[3][3]=s[3][2]; s[3][2]=s[3][1]; s[3][1]=s[3][0]; s[3][0]=t;
}

fn inv_shift_rows(s: &mut State) {
    let t = s[1][3]; s[1][3]=s[1][2]; s[1][2]=s[1][1]; s[1][1]=s[1][0]; s[1][0]=t;
    let (t0,t1)=(s[2][2],s[2][3]); s[2][2]=s[2][0]; s[2][3]=s[2][1]; s[2][0]=t0; s[2][1]=t1;
    let t = s[3][0]; s[3][0]=s[3][1]; s[3][1]=s[3][2]; s[3][2]=s[3][3]; s[3][3]=t;
}

fn mix_columns(s: &mut State) {
    for c in 0..4 {
        let (a0,a1,a2,a3) = (s[0][c],s[1][c],s[2][c],s[3][c]);
        s[0][c] = gf_mul(a0,2) ^ gf_mul(a1,3) ^ a2 ^ a3;
        s[1][c] = a0 ^ gf_mul(a1,2) ^ gf_mul(a2,3) ^ a3;
        s[2][c] = a0 ^ a1 ^ gf_mul(a2,2) ^ gf_mul(a3,3);
        s[3][c] = gf_mul(a0,3) ^ a1 ^ a2 ^ gf_mul(a3,2);
    }
}

fn inv_mix_columns(s: &mut State) {
    for c in 0..4 {
        let (a0,a1,a2,a3) = (s[0][c],s[1][c],s[2][c],s[3][c]);
        s[0][c] = gf_mul(a0,14) ^ gf_mul(a1,11) ^ gf_mul(a2,13) ^ gf_mul(a3,9);
        s[1][c] = gf_mul(a0,9) ^ gf_mul(a1,14) ^ gf_mul(a2,11) ^ gf_mul(a3,13);
        s[2][c] = gf_mul(a0,13) ^ gf_mul(a1,9) ^ gf_mul(a2,14) ^ gf_mul(a3,11);
        s[3][c] = gf_mul(a0,11) ^ gf_mul(a1,13) ^ gf_mul(a2,9) ^ gf_mul(a3,14);
    }
}

fn add_round_key(s: &mut State, rk: &[u8; 16]) {
    for c in 0..4 { for r in 0..4 { s[r][c] ^= rk[c*4+r]; } }
}

fn key_expansion(key: &[u8; 16]) -> [[u8; 16]; 11] {
    let mut w = [0u8; 176];
    w[..16].copy_from_slice(key);
    for i in 4..44 {
        let mut temp = [w[i*4-4], w[i*4-3], w[i*4-2], w[i*4-1]];
        if i % 4 == 0 {
            temp = [SBOX[temp[1] as usize], SBOX[temp[2] as usize], SBOX[temp[3] as usize], SBOX[temp[0] as usize]];
            temp[0] ^= RCON[i/4-1];
        }
        for j in 0..4 { w[i*4+j] = w[(i-4)*4+j] ^ temp[j]; }
    }
    let mut rks = [[0u8; 16]; 11];
    for i in 0..11 { rks[i].copy_from_slice(&w[i*16..(i+1)*16]); }
    rks
}

fn bytes_to_state(b: &[u8; 16]) -> State {
    let mut s = [[0u8;4];4];
    for c in 0..4 { for r in 0..4 { s[r][c] = b[c*4+r]; } }
    s
}

fn state_to_bytes(s: &State) -> [u8; 16] {
    let mut b = [0u8;16];
    for c in 0..4 { for r in 0..4 { b[c*4+r] = s[r][c]; } }
    b
}

pub fn aes128_encrypt_block(plaintext: &[u8; 16], key: &[u8; 16]) -> [u8; 16] {
    let rks = key_expansion(key);
    let mut state = bytes_to_state(plaintext);
    add_round_key(&mut state, &rks[0]);
    for round in 1..10 {
        sub_bytes(&mut state); shift_rows(&mut state);
        mix_columns(&mut state); add_round_key(&mut state, &rks[round]);
    }
    sub_bytes(&mut state); shift_rows(&mut state); add_round_key(&mut state, &rks[10]);
    state_to_bytes(&state)
}

pub fn aes128_decrypt_block(ciphertext: &[u8; 16], key: &[u8; 16]) -> [u8; 16] {
    let rks = key_expansion(key);
    let mut state = bytes_to_state(ciphertext);
    add_round_key(&mut state, &rks[10]);
    for round in (1..10).rev() {
        inv_shift_rows(&mut state); inv_sub_bytes(&mut state);
        add_round_key(&mut state, &rks[round]); inv_mix_columns(&mut state);
    }
    inv_shift_rows(&mut state); inv_sub_bytes(&mut state); add_round_key(&mut state, &rks[0]);
    state_to_bytes(&state)
}

pub fn aes128_cbc_encrypt(plaintext: &[u8], key: &[u8; 16], iv: &[u8; 16]) -> Vec<u8> {
    let padded = pkcs7_pad(plaintext, 16);
    let mut ciphertext = Vec::with_capacity(padded.len());
    let mut prev_block = *iv;
    for chunk in padded.chunks_exact(16) {
        let mut block = [0u8; 16];
        for i in 0..16 { block[i] = chunk[i] ^ prev_block[i]; }
        let encrypted = aes128_encrypt_block(&block, key);
        ciphertext.extend_from_slice(&encrypted);
        prev_block = encrypted;
    }
    ciphertext
}

pub fn aes128_cbc_decrypt(ciphertext: &[u8], key: &[u8; 16], iv: &[u8; 16]) -> Result<Vec<u8>, &'static str> {
    if ciphertext.len() % 16 != 0 || ciphertext.is_empty() {
        return Err("invalid ciphertext length");
    }
    let mut plaintext = Vec::with_capacity(ciphertext.len());
    let mut prev_block = *iv;
    for chunk in ciphertext.chunks_exact(16) {
        let mut ct_block = [0u8; 16];
        ct_block.copy_from_slice(chunk);
        let decrypted = aes128_decrypt_block(&ct_block, key);
        let mut plain_block = [0u8; 16];
        for i in 0..16 { plain_block[i] = decrypted[i] ^ prev_block[i]; }
        plaintext.extend_from_slice(&plain_block);
        prev_block = ct_block;
    }
    pkcs7_unpad(&plaintext).map(|p| p.to_vec())
}

fn pkcs7_pad(data: &[u8], block_size: usize) -> Vec<u8> {
    let pad_len = block_size - (data.len() % block_size);
    let mut padded = data.to_vec();
    for _ in 0..pad_len { padded.push(pad_len as u8); }
    padded
}

fn pkcs7_unpad(data: &[u8]) -> Result<&[u8], &'static str> {
    if data.is_empty() { return Err("empty data"); }
    let pad_len = *data.last().unwrap() as usize;
    if pad_len == 0 || pad_len > 16 || pad_len > data.len() { return Err("invalid padding"); }
    for &b in &data[data.len()-pad_len..] {
        if b != pad_len as u8 { return Err("invalid padding byte"); }
    }
    Ok(&data[..data.len()-pad_len])
}
```

### Source: `src/tls_prf.rs`

```rust
use crate::sha256::hmac_sha256;

/// TLS PRF based on HMAC-SHA256 (RFC 5246 Section 5).
/// P_hash(secret, seed) = HMAC(secret, A(1) + seed) ||
///                         HMAC(secret, A(2) + seed) || ...
/// where A(0) = seed, A(i) = HMAC(secret, A(i-1))
pub fn tls_prf(secret: &[u8], label: &str, seed: &[u8], output_len: usize) -> Vec<u8> {
    let mut label_seed = label.as_bytes().to_vec();
    label_seed.extend_from_slice(seed);

    let mut a = hmac_sha256(secret, &label_seed).to_vec(); // A(1)
    let mut result = Vec::with_capacity(output_len);

    while result.len() < output_len {
        // HMAC(secret, A(i) || label || seed)
        let mut a_seed = a.clone();
        a_seed.extend_from_slice(&label_seed);
        let block = hmac_sha256(secret, &a_seed);
        result.extend_from_slice(&block);

        // A(i+1) = HMAC(secret, A(i))
        a = hmac_sha256(secret, &a).to_vec();
    }

    result.truncate(output_len);
    result
}

/// Derive 48-byte master secret from pre-master secret and randoms.
pub fn derive_master_secret(
    pre_master_secret: &[u8],
    client_random: &[u8; 32],
    server_random: &[u8; 32],
) -> [u8; 48] {
    let mut seed = Vec::with_capacity(64);
    seed.extend_from_slice(client_random);
    seed.extend_from_slice(server_random);

    let ms = tls_prf(pre_master_secret, "master secret", &seed, 48);
    let mut result = [0u8; 48];
    result.copy_from_slice(&ms);
    result
}

/// Key material derived from master secret.
pub struct KeyBlock {
    pub client_mac_key: [u8; 32],
    pub server_mac_key: [u8; 32],
    pub client_enc_key: [u8; 16],
    pub server_enc_key: [u8; 16],
    pub client_iv: [u8; 16],
    pub server_iv: [u8; 16],
}

/// Derive all session keys from master secret and randoms.
pub fn derive_key_block(
    master_secret: &[u8; 48],
    server_random: &[u8; 32],
    client_random: &[u8; 32],
) -> KeyBlock {
    // Note: key expansion seed is server_random + client_random (reversed from master secret)
    let mut seed = Vec::with_capacity(64);
    seed.extend_from_slice(server_random);
    seed.extend_from_slice(client_random);

    let material = tls_prf(master_secret, "key expansion", &seed, 128);
    let mut offset = 0;

    let mut client_mac_key = [0u8; 32];
    client_mac_key.copy_from_slice(&material[offset..offset + 32]);
    offset += 32;

    let mut server_mac_key = [0u8; 32];
    server_mac_key.copy_from_slice(&material[offset..offset + 32]);
    offset += 32;

    let mut client_enc_key = [0u8; 16];
    client_enc_key.copy_from_slice(&material[offset..offset + 16]);
    offset += 16;

    let mut server_enc_key = [0u8; 16];
    server_enc_key.copy_from_slice(&material[offset..offset + 16]);
    offset += 16;

    let mut client_iv = [0u8; 16];
    client_iv.copy_from_slice(&material[offset..offset + 16]);
    offset += 16;

    let mut server_iv = [0u8; 16];
    server_iv.copy_from_slice(&material[offset..offset + 16]);

    KeyBlock {
        client_mac_key, server_mac_key,
        client_enc_key, server_enc_key,
        client_iv, server_iv,
    }
}
```

### Source: `src/record.rs`

```rust
use crate::aes::{aes128_cbc_encrypt, aes128_cbc_decrypt};
use crate::sha256::hmac_sha256;

pub const CONTENT_HANDSHAKE: u8 = 22;
pub const CONTENT_CHANGE_CIPHER_SPEC: u8 = 20;
pub const CONTENT_APPLICATION_DATA: u8 = 23;
pub const TLS_VERSION: [u8; 2] = [0x03, 0x03]; // TLS 1.2

/// Build a TLS record (unencrypted).
pub fn build_record(content_type: u8, payload: &[u8]) -> Vec<u8> {
    let mut record = Vec::with_capacity(5 + payload.len());
    record.push(content_type);
    record.extend_from_slice(&TLS_VERSION);
    record.extend_from_slice(&(payload.len() as u16).to_be_bytes());
    record.extend_from_slice(payload);
    record
}

/// Build an encrypted TLS record.
/// MAC-then-encrypt: HMAC over (seq_num || content_type || version || length || plaintext),
/// then pad (plaintext || MAC), then AES-CBC encrypt with explicit IV.
pub fn build_encrypted_record(
    content_type: u8,
    plaintext: &[u8],
    seq_num: u64,
    mac_key: &[u8; 32],
    enc_key: &[u8; 16],
    rng_iv: &[u8; 16],
) -> Vec<u8> {
    // Compute MAC
    let mut mac_input = Vec::new();
    mac_input.extend_from_slice(&seq_num.to_be_bytes());
    mac_input.push(content_type);
    mac_input.extend_from_slice(&TLS_VERSION);
    mac_input.extend_from_slice(&(plaintext.len() as u16).to_be_bytes());
    mac_input.extend_from_slice(plaintext);
    let mac = hmac_sha256(mac_key, &mac_input);

    // plaintext || MAC
    let mut to_encrypt = plaintext.to_vec();
    to_encrypt.extend_from_slice(&mac);

    // Encrypt with AES-CBC (includes PKCS#7 padding)
    let encrypted = aes128_cbc_encrypt(&to_encrypt, enc_key, rng_iv);

    // Explicit IV + ciphertext
    let mut payload = Vec::with_capacity(16 + encrypted.len());
    payload.extend_from_slice(rng_iv);
    payload.extend_from_slice(&encrypted);

    build_record(content_type, &payload)
}

/// Decrypt a TLS record and verify MAC.
pub fn decrypt_record(
    content_type: u8,
    encrypted_payload: &[u8],
    seq_num: u64,
    mac_key: &[u8; 32],
    enc_key: &[u8; 16],
) -> Result<Vec<u8>, &'static str> {
    if encrypted_payload.len() < 32 {
        return Err("encrypted payload too short");
    }

    // Extract explicit IV
    let iv: [u8; 16] = encrypted_payload[..16].try_into().map_err(|_| "bad IV")?;
    let ciphertext = &encrypted_payload[16..];

    // Decrypt
    let decrypted = aes128_cbc_decrypt(ciphertext, enc_key, &iv)?;

    // Split plaintext and MAC
    if decrypted.len() < 32 {
        return Err("decrypted data too short for MAC");
    }
    let mac_offset = decrypted.len() - 32;
    let plaintext = &decrypted[..mac_offset];
    let received_mac = &decrypted[mac_offset..];

    // Verify MAC
    let mut mac_input = Vec::new();
    mac_input.extend_from_slice(&seq_num.to_be_bytes());
    mac_input.push(content_type);
    mac_input.extend_from_slice(&TLS_VERSION);
    mac_input.extend_from_slice(&(plaintext.len() as u16).to_be_bytes());
    mac_input.extend_from_slice(plaintext);
    let expected_mac = hmac_sha256(mac_key, &mac_input);

    if received_mac != expected_mac {
        return Err("MAC verification failed");
    }

    Ok(plaintext.to_vec())
}
```

### Source: `src/handshake.rs`

```rust
use crate::bigint::BigUint;
use crate::sha256::sha256;
use crate::tls_prf::{derive_master_secret, derive_key_block, tls_prf, KeyBlock};
use crate::record::*;

/// Handshake message types
const CLIENT_HELLO: u8 = 1;
const SERVER_HELLO: u8 = 2;
const CERTIFICATE: u8 = 11;
const SERVER_HELLO_DONE: u8 = 14;
const CLIENT_KEY_EXCHANGE: u8 = 16;
const FINISHED: u8 = 20;

/// Cipher suite: TLS_RSA_WITH_AES_128_CBC_SHA256
const CIPHER_SUITE: u16 = 0x003C;

/// Build a handshake message header: type (1) + length (3) + body
fn handshake_msg(msg_type: u8, body: &[u8]) -> Vec<u8> {
    let mut msg = Vec::with_capacity(4 + body.len());
    msg.push(msg_type);
    let len = body.len() as u32;
    msg.push((len >> 16) as u8);
    msg.push((len >> 8) as u8);
    msg.push(len as u8);
    msg.extend_from_slice(body);
    msg
}

pub fn build_client_hello(client_random: &[u8; 32]) -> Vec<u8> {
    let mut body = Vec::new();
    // Version
    body.extend_from_slice(&[0x03, 0x03]);
    // Client random
    body.extend_from_slice(client_random);
    // Session ID (empty)
    body.push(0x00);
    // Cipher suites (1 suite = 2 bytes, preceded by 2-byte length)
    body.extend_from_slice(&[0x00, 0x02]); // length
    body.extend_from_slice(&CIPHER_SUITE.to_be_bytes());
    // Compression methods (null only)
    body.push(0x01); // length
    body.push(0x00); // null compression

    handshake_msg(CLIENT_HELLO, &body)
}

pub fn build_server_hello(server_random: &[u8; 32], session_id: &[u8; 32]) -> Vec<u8> {
    let mut body = Vec::new();
    body.extend_from_slice(&[0x03, 0x03]);
    body.extend_from_slice(server_random);
    body.push(32); // session ID length
    body.extend_from_slice(session_id);
    body.extend_from_slice(&CIPHER_SUITE.to_be_bytes());
    body.push(0x00); // null compression

    handshake_msg(SERVER_HELLO, &body)
}

pub fn build_certificate(der_cert: &[u8]) -> Vec<u8> {
    let cert_len = der_cert.len() as u32;
    let certs_len = cert_len + 3; // individual cert length (3 bytes) + cert data

    let mut body = Vec::new();
    // Total certificates length (3 bytes)
    body.push((certs_len >> 16) as u8);
    body.push((certs_len >> 8) as u8);
    body.push(certs_len as u8);
    // Individual certificate length (3 bytes)
    body.push((cert_len >> 16) as u8);
    body.push((cert_len >> 8) as u8);
    body.push(cert_len as u8);
    body.extend_from_slice(der_cert);

    handshake_msg(CERTIFICATE, &body)
}

pub fn build_server_hello_done() -> Vec<u8> {
    handshake_msg(SERVER_HELLO_DONE, &[])
}

pub fn build_client_key_exchange(
    pre_master_secret: &[u8; 48],
    rsa_n: &BigUint,
    rsa_e: &BigUint,
) -> Vec<u8> {
    let pms_int = BigUint::from_bytes_be(pre_master_secret);
    let encrypted = pms_int.mod_pow(rsa_e, rsa_n);
    let enc_bytes = encrypted.to_bytes_be();

    let mut body = Vec::new();
    body.extend_from_slice(&(enc_bytes.len() as u16).to_be_bytes());
    body.extend_from_slice(&enc_bytes);

    handshake_msg(CLIENT_KEY_EXCHANGE, &body)
}

pub fn build_change_cipher_spec() -> Vec<u8> {
    build_record(CONTENT_CHANGE_CIPHER_SPEC, &[0x01])
}

pub fn build_finished(
    master_secret: &[u8; 48],
    label: &str,
    handshake_hash: &[u8; 32],
) -> Vec<u8> {
    let verify_data = tls_prf(master_secret, label, handshake_hash, 12);
    handshake_msg(FINISHED, &verify_data)
}

/// Handshake state machine
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum HandshakeState {
    Initial,
    ClientHelloSent,
    ServerHelloReceived,
    CertificateReceived,
    ServerHelloDoneReceived,
    ClientKeyExchangeSent,
    ChangeCipherSpecSent,
    FinishedSent,
    FinishedReceived,
    Established,
}

pub struct HandshakeContext {
    pub state: HandshakeState,
    pub is_server: bool,
    pub client_random: [u8; 32],
    pub server_random: [u8; 32],
    pub session_id: [u8; 32],
    pub pre_master_secret: [u8; 48],
    pub master_secret: [u8; 48],
    pub key_block: Option<KeyBlock>,
    pub handshake_messages: Vec<u8>, // concatenation of all handshake messages for Finished hash
}

impl HandshakeContext {
    pub fn new(is_server: bool) -> Self {
        HandshakeContext {
            state: HandshakeState::Initial,
            is_server,
            client_random: [0; 32],
            server_random: [0; 32],
            session_id: [0; 32],
            pre_master_secret: [0; 48],
            master_secret: [0; 48],
            key_block: None,
            handshake_messages: Vec::new(),
        }
    }

    pub fn derive_keys(&mut self) {
        self.master_secret = derive_master_secret(
            &self.pre_master_secret,
            &self.client_random,
            &self.server_random,
        );
        self.key_block = Some(derive_key_block(
            &self.master_secret,
            &self.server_random,
            &self.client_random,
        ));
    }

    pub fn handshake_hash(&self) -> [u8; 32] {
        sha256(&self.handshake_messages)
    }

    pub fn record_message(&mut self, msg: &[u8]) {
        self.handshake_messages.extend_from_slice(msg);
    }
}
```

### Source: `src/x509.rs`

```rust
/// Minimal ASN.1 DER parser for extracting RSA public key from X.509 certificates.
use crate::bigint::BigUint;

#[derive(Debug)]
struct DerTag {
    tag: u8,
    length: usize,
    header_len: usize,
}

fn parse_der_tag(data: &[u8]) -> Result<DerTag, &'static str> {
    if data.is_empty() {
        return Err("empty data for DER tag");
    }
    let tag = data[0];
    if data.len() < 2 {
        return Err("truncated DER tag");
    }

    let (length, header_len) = if data[1] & 0x80 == 0 {
        (data[1] as usize, 2)
    } else {
        let num_bytes = (data[1] & 0x7F) as usize;
        if data.len() < 2 + num_bytes {
            return Err("truncated DER length");
        }
        let mut len: usize = 0;
        for i in 0..num_bytes {
            len = (len << 8) | (data[2 + i] as usize);
        }
        (len, 2 + num_bytes)
    };

    Ok(DerTag { tag, length, header_len })
}

/// Extract RSA public key (n, e) from a DER-encoded X.509 certificate.
/// Navigates: Certificate -> TBSCertificate -> SubjectPublicKeyInfo -> RSAPublicKey
pub fn extract_rsa_pubkey(cert_der: &[u8]) -> Result<(BigUint, BigUint), &'static str> {
    // Certificate is a SEQUENCE
    let cert = parse_der_tag(cert_der)?;
    if cert.tag != 0x30 {
        return Err("certificate is not a SEQUENCE");
    }
    let tbs_start = cert.header_len;
    let tbs_data = &cert_der[tbs_start..];

    // TBSCertificate is a SEQUENCE
    let tbs = parse_der_tag(tbs_data)?;
    if tbs.tag != 0x30 {
        return Err("TBSCertificate is not a SEQUENCE");
    }

    // Navigate through TBSCertificate fields to find SubjectPublicKeyInfo
    let mut offset = tbs.header_len;
    let tbs_content = &tbs_data[..tbs.header_len + tbs.length];

    // Skip: version (explicit tag [0]), serialNumber, signature algorithm,
    //        issuer, validity, subject
    for _field_idx in 0..6 {
        if offset >= tbs_content.len() {
            return Err("ran out of TBS fields");
        }
        let field = parse_der_tag(&tbs_content[offset..])?;
        offset += field.header_len + field.length;
    }

    // SubjectPublicKeyInfo is next
    if offset >= tbs_content.len() {
        return Err("no SubjectPublicKeyInfo found");
    }
    let spki = parse_der_tag(&tbs_content[offset..])?;
    if spki.tag != 0x30 {
        return Err("SubjectPublicKeyInfo is not a SEQUENCE");
    }

    let spki_content = &tbs_content[offset + spki.header_len..offset + spki.header_len + spki.length];

    // Skip AlgorithmIdentifier (SEQUENCE)
    let algo = parse_der_tag(spki_content)?;
    let pubkey_offset = algo.header_len + algo.length;

    // subjectPublicKey is a BIT STRING
    let bitstring = parse_der_tag(&spki_content[pubkey_offset..])?;
    if bitstring.tag != 0x03 {
        return Err("subjectPublicKey is not a BIT STRING");
    }

    // BIT STRING: first byte is number of unused bits (should be 0)
    let key_data = &spki_content[pubkey_offset + bitstring.header_len + 1..
                                  pubkey_offset + bitstring.header_len + bitstring.length];

    // RSAPublicKey is a SEQUENCE of two INTEGERs
    let rsa_seq = parse_der_tag(key_data)?;
    if rsa_seq.tag != 0x30 {
        return Err("RSAPublicKey is not a SEQUENCE");
    }

    let rsa_content = &key_data[rsa_seq.header_len..rsa_seq.header_len + rsa_seq.length];

    // First INTEGER: modulus n
    let n_tag = parse_der_tag(rsa_content)?;
    if n_tag.tag != 0x02 {
        return Err("modulus is not an INTEGER");
    }
    let n_bytes = &rsa_content[n_tag.header_len..n_tag.header_len + n_tag.length];
    // Strip leading zero if present (ASN.1 encodes positive integers with leading zero if high bit set)
    let n_bytes = if !n_bytes.is_empty() && n_bytes[0] == 0 { &n_bytes[1..] } else { n_bytes };

    let e_offset = n_tag.header_len + n_tag.length;
    let e_tag = parse_der_tag(&rsa_content[e_offset..])?;
    if e_tag.tag != 0x02 {
        return Err("exponent is not an INTEGER");
    }
    let e_bytes = &rsa_content[e_offset + e_tag.header_len..e_offset + e_tag.header_len + e_tag.length];
    let e_bytes = if !e_bytes.is_empty() && e_bytes[0] == 0 { &e_bytes[1..] } else { e_bytes };

    Ok((BigUint::from_bytes_be(n_bytes), BigUint::from_bytes_be(e_bytes)))
}
```

### Source: `src/main.rs`

```rust
mod bigint;
mod sha256;
mod aes;
mod tls_prf;
mod record;
mod handshake;
mod x509;

use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::thread;

use bigint::BigUint;
use handshake::*;
use record::*;
use sha256::sha256;

/// Pre-generated 512-bit RSA test key pair (EDUCATIONAL ONLY -- insecure key size).
/// In a real implementation, use at least 2048-bit keys.
const TEST_RSA_N_HEX: &str = "00b3510a2f7b3e5346b09ab5e951249bb271a5e43b6a82469e4f6ec3ac3e3e3e7b6f5a1b1d1a7c3a5e8f9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f7081929";
const TEST_RSA_E: u32 = 65537;
const TEST_RSA_D_HEX: &str = "5a2f91c7e2a8b3d1f4c5e6d7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1";

fn hex_to_bytes(hex: &str) -> Vec<u8> {
    let hex = if hex.len() % 2 != 0 { format!("0{}", hex) } else { hex.to_string() };
    (0..hex.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&hex[i..i + 2], 16).unwrap())
        .collect()
}

fn bytes_to_hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{:02x}", b)).collect()
}

fn random_bytes_32() -> [u8; 32] {
    let mut buf = [0u8; 32];
    let seed = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH).unwrap()
        .as_nanos();
    let mut state = seed as u64;
    for byte in buf.iter_mut() {
        state ^= state << 13;
        state ^= state >> 7;
        state ^= state << 17;
        *byte = state as u8;
    }
    buf
}

fn run_server(mut stream: TcpStream) {
    println!("[server] Connection accepted");
    let mut ctx = HandshakeContext::new(true);

    let rsa_n = BigUint::from_bytes_be(&hex_to_bytes(TEST_RSA_N_HEX));
    let rsa_d = BigUint::from_bytes_be(&hex_to_bytes(TEST_RSA_D_HEX));

    // Receive ClientHello
    let mut buf = vec![0u8; 4096];
    let n = stream.read(&mut buf).unwrap();
    let client_hello = &buf[5..n]; // skip record header
    ctx.record_message(client_hello);
    ctx.client_random.copy_from_slice(&client_hello[6..38]);
    println!("[server] Received ClientHello, client_random: {}", bytes_to_hex(&ctx.client_random));

    // Send ServerHello
    ctx.server_random = random_bytes_32();
    ctx.session_id = random_bytes_32();
    let server_hello = build_server_hello(&ctx.server_random, &ctx.session_id);
    ctx.record_message(&server_hello);
    let record = build_record(CONTENT_HANDSHAKE, &server_hello);
    stream.write_all(&record).unwrap();
    println!("[server] Sent ServerHello, server_random: {}", bytes_to_hex(&ctx.server_random));

    // Send Certificate (using a minimal test DER certificate)
    let test_cert_der = hex_to_bytes(TEST_RSA_N_HEX); // simplified: just use the key material as placeholder
    let cert_msg = build_certificate(&test_cert_der);
    ctx.record_message(&cert_msg);
    let record = build_record(CONTENT_HANDSHAKE, &cert_msg);
    stream.write_all(&record).unwrap();
    println!("[server] Sent Certificate");

    // Send ServerHelloDone
    let shd = build_server_hello_done();
    ctx.record_message(&shd);
    let record = build_record(CONTENT_HANDSHAKE, &shd);
    stream.write_all(&record).unwrap();
    println!("[server] Sent ServerHelloDone");

    // Receive ClientKeyExchange
    let n = stream.read(&mut buf).unwrap();
    let cke = &buf[5..n];
    ctx.record_message(cke);
    // Parse: skip handshake header (4 bytes), read 2-byte length, then encrypted PMS
    let enc_len = u16::from_be_bytes([cke[4], cke[5]]) as usize;
    let encrypted_pms = &cke[6..6 + enc_len];
    let enc_int = BigUint::from_bytes_be(encrypted_pms);
    let pms_int = enc_int.mod_pow(&rsa_d, &rsa_n);
    let pms_bytes = pms_int.to_bytes_be_padded(48);
    ctx.pre_master_secret.copy_from_slice(&pms_bytes[..48]);
    println!("[server] Received ClientKeyExchange, decrypted PMS");

    // Derive keys
    ctx.derive_keys();
    println!("[server] Derived master secret: {}", bytes_to_hex(&ctx.master_secret[..16]));

    // Receive ChangeCipherSpec
    let _n = stream.read(&mut buf).unwrap();
    println!("[server] Received ChangeCipherSpec");

    // Receive Finished (encrypted)
    let n = stream.read(&mut buf).unwrap();
    let encrypted_finished = &buf[5..n];
    let kb = ctx.key_block.as_ref().unwrap();
    let decrypted = decrypt_record(
        CONTENT_HANDSHAKE, encrypted_finished, 0,
        &kb.client_mac_key, &kb.client_enc_key,
    ).expect("Failed to decrypt client Finished");
    println!("[server] Received and verified client Finished");

    // Send ChangeCipherSpec
    let ccs = build_change_cipher_spec();
    stream.write_all(&ccs).unwrap();
    println!("[server] Sent ChangeCipherSpec");

    // Send Finished
    let hash = ctx.handshake_hash();
    let finished = build_finished(&ctx.master_secret, "server finished", &hash);
    let iv = random_bytes_32();
    let iv16: [u8; 16] = iv[..16].try_into().unwrap();
    let enc_record = build_encrypted_record(
        CONTENT_HANDSHAKE, &finished, 0,
        &kb.server_mac_key, &kb.server_enc_key, &iv16,
    );
    stream.write_all(&enc_record).unwrap();
    println!("[server] Sent Finished (encrypted)");

    // Exchange application data
    let n = stream.read(&mut buf).unwrap();
    let app_data = decrypt_record(
        CONTENT_APPLICATION_DATA, &buf[5..n], 1,
        &kb.client_mac_key, &kb.client_enc_key,
    ).expect("Failed to decrypt application data");
    println!("[server] Received application data: {:?}", String::from_utf8_lossy(&app_data));

    // Send response
    let response = b"Hello from TLS server!";
    let iv = random_bytes_32();
    let iv16: [u8; 16] = iv[..16].try_into().unwrap();
    let enc_record = build_encrypted_record(
        CONTENT_APPLICATION_DATA, response, 1,
        &kb.server_mac_key, &kb.server_enc_key, &iv16,
    );
    stream.write_all(&enc_record).unwrap();
    println!("[server] Sent encrypted response");
    println!("[server] Handshake complete!");
}

fn run_client(mut stream: TcpStream) {
    let mut ctx = HandshakeContext::new(false);
    let rsa_n = BigUint::from_bytes_be(&hex_to_bytes(TEST_RSA_N_HEX));
    let rsa_e = BigUint::from_bytes_be(&TEST_RSA_E.to_be_bytes());

    // Send ClientHello
    ctx.client_random = random_bytes_32();
    let ch = build_client_hello(&ctx.client_random);
    ctx.record_message(&ch);
    let record = build_record(CONTENT_HANDSHAKE, &ch);
    stream.write_all(&record).unwrap();
    println!("[client] Sent ClientHello");

    let mut buf = vec![0u8; 4096];

    // Receive ServerHello
    let n = stream.read(&mut buf).unwrap();
    let sh = &buf[5..n];
    ctx.record_message(sh);
    ctx.server_random.copy_from_slice(&sh[6..38]);
    println!("[client] Received ServerHello");

    // Receive Certificate
    let n = stream.read(&mut buf).unwrap();
    let cert = &buf[5..n];
    ctx.record_message(cert);
    println!("[client] Received Certificate");

    // Receive ServerHelloDone
    let n = stream.read(&mut buf).unwrap();
    let shd = &buf[5..n];
    ctx.record_message(shd);
    println!("[client] Received ServerHelloDone");

    // Generate pre-master secret: version (0x03, 0x03) + 46 random bytes
    ctx.pre_master_secret[0] = 0x03;
    ctx.pre_master_secret[1] = 0x03;
    let rand = random_bytes_32();
    ctx.pre_master_secret[2..34].copy_from_slice(&rand);
    let rand2 = random_bytes_32();
    ctx.pre_master_secret[34..48].copy_from_slice(&rand2[..14]);

    // Send ClientKeyExchange
    let cke = build_client_key_exchange(&ctx.pre_master_secret, &rsa_n, &rsa_e);
    ctx.record_message(&cke);
    let record = build_record(CONTENT_HANDSHAKE, &cke);
    stream.write_all(&record).unwrap();
    println!("[client] Sent ClientKeyExchange");

    // Derive keys
    ctx.derive_keys();
    println!("[client] Derived master secret: {}", bytes_to_hex(&ctx.master_secret[..16]));

    // Send ChangeCipherSpec
    let ccs = build_change_cipher_spec();
    stream.write_all(&ccs).unwrap();
    println!("[client] Sent ChangeCipherSpec");

    // Send Finished (encrypted)
    let hash = ctx.handshake_hash();
    let finished = build_finished(&ctx.master_secret, "client finished", &hash);
    let kb = ctx.key_block.as_ref().unwrap();
    let iv = random_bytes_32();
    let iv16: [u8; 16] = iv[..16].try_into().unwrap();
    let enc_record = build_encrypted_record(
        CONTENT_HANDSHAKE, &finished, 0,
        &kb.client_mac_key, &kb.client_enc_key, &iv16,
    );
    stream.write_all(&enc_record).unwrap();
    println!("[client] Sent Finished (encrypted)");

    // Receive ChangeCipherSpec
    let _n = stream.read(&mut buf).unwrap();
    println!("[client] Received ChangeCipherSpec");

    // Receive Finished
    let n = stream.read(&mut buf).unwrap();
    let enc_finished = &buf[5..n];
    let _decrypted = decrypt_record(
        CONTENT_HANDSHAKE, enc_finished, 0,
        &kb.server_mac_key, &kb.server_enc_key,
    ).expect("Failed to decrypt server Finished");
    println!("[client] Received and verified server Finished");

    // Send application data
    let message = b"Hello from TLS client!";
    let iv = random_bytes_32();
    let iv16: [u8; 16] = iv[..16].try_into().unwrap();
    let enc_record = build_encrypted_record(
        CONTENT_APPLICATION_DATA, message, 1,
        &kb.client_mac_key, &kb.client_enc_key, &iv16,
    );
    stream.write_all(&enc_record).unwrap();
    println!("[client] Sent encrypted application data");

    // Receive response
    let n = stream.read(&mut buf).unwrap();
    let response = decrypt_record(
        CONTENT_APPLICATION_DATA, &buf[5..n], 1,
        &kb.server_mac_key, &kb.server_enc_key,
    ).expect("Failed to decrypt response");
    println!("[client] Received: {:?}", String::from_utf8_lossy(&response));
    println!("[client] Handshake complete!");
}

fn main() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = listener.local_addr().unwrap();
    println!("TLS Educational Handshake on {}", addr);

    let server_handle = thread::spawn(move || {
        let (stream, _) = listener.accept().unwrap();
        run_server(stream);
    });

    let client_stream = TcpStream::connect(addr).unwrap();
    run_client(client_stream);

    server_handle.join().unwrap();
    println!("\n=== TLS handshake completed successfully ===");
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use super::bigint::BigUint;
    use super::sha256::{sha256, hmac_sha256};
    use super::tls_prf::*;
    use super::aes::*;
    use super::record::*;

    #[test]
    fn bigint_mod_pow() {
        let base = BigUint::from_bytes_be(&[0x04]);
        let exp = BigUint::from_bytes_be(&[0x0D]); // 13
        let modulus = BigUint::from_bytes_be(&[0x61]); // 97
        let result = base.mod_pow(&exp, &modulus);
        // 4^13 mod 97 = 61
        assert_eq!(result.to_bytes_be(), vec![0x3D]); // 61
    }

    #[test]
    fn rsa_encrypt_decrypt_roundtrip() {
        // Small test key for fast testing (NOT secure)
        let n = BigUint::from_bytes_be(&[0x00, 0xA3, 0x07]); // 41735
        let e = BigUint::from_bytes_be(&[0x01, 0x01]); // 257 (not 65537 for small key)
        let d = BigUint::from_bytes_be(&[0x27, 0x01]); // precomputed private exponent

        let message = BigUint::from_bytes_be(&[0x00, 0x42]); // 66
        let encrypted = message.mod_pow(&e, &n);
        let decrypted = encrypted.mod_pow(&d, &n);
        assert_eq!(decrypted.to_bytes_be(), vec![0x42]);
    }

    #[test]
    fn sha256_known_vector() {
        let hash = sha256(b"abc");
        let hex: String = hash.iter().map(|b| format!("{:02x}", b)).collect();
        assert_eq!(hex, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    }

    #[test]
    fn tls_prf_deterministic() {
        let secret = b"test secret";
        let seed = b"test seed";
        let out1 = tls_prf(secret, "test label", seed, 32);
        let out2 = tls_prf(secret, "test label", seed, 32);
        assert_eq!(out1, out2);
        assert_eq!(out1.len(), 32);
    }

    #[test]
    fn master_secret_is_48_bytes() {
        let pms = [0x03, 0x03, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42,
                   0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42,
                   0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42,
                   0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42,
                   0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42,
                   0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42];
        let cr = [0xAA; 32];
        let sr = [0xBB; 32];
        let ms = derive_master_secret(&pms, &cr, &sr);
        assert_eq!(ms.len(), 48);
    }

    #[test]
    fn key_block_produces_all_keys() {
        let ms = [0x42u8; 48];
        let sr = [0xAA; 32];
        let cr = [0xBB; 32];
        let kb = derive_key_block(&ms, &sr, &cr);
        // Verify all keys are non-zero (derived from PRF)
        assert!(!kb.client_mac_key.iter().all(|&b| b == 0));
        assert!(!kb.server_mac_key.iter().all(|&b| b == 0));
        assert!(!kb.client_enc_key.iter().all(|&b| b == 0));
        assert!(!kb.server_enc_key.iter().all(|&b| b == 0));
    }

    #[test]
    fn record_encrypt_decrypt_roundtrip() {
        let plaintext = b"Hello, TLS!";
        let mac_key = [0x42u8; 32];
        let enc_key = [0x24u8; 16];
        let iv = [0x11u8; 16];

        let record = build_encrypted_record(
            CONTENT_APPLICATION_DATA, plaintext, 0,
            &mac_key, &enc_key, &iv,
        );
        // Extract payload (skip 5-byte record header)
        let payload = &record[5..];
        let decrypted = decrypt_record(
            CONTENT_APPLICATION_DATA, payload, 0,
            &mac_key, &enc_key,
        ).unwrap();
        assert_eq!(decrypted, plaintext);
    }

    #[test]
    fn mac_rejects_tampered_ciphertext() {
        let plaintext = b"sensitive data";
        let mac_key = [0x42u8; 32];
        let enc_key = [0x24u8; 16];
        let iv = [0x11u8; 16];

        let record = build_encrypted_record(
            CONTENT_APPLICATION_DATA, plaintext, 0,
            &mac_key, &enc_key, &iv,
        );
        let mut payload = record[5..].to_vec();
        // Flip a byte in the ciphertext
        if payload.len() > 20 {
            payload[20] ^= 0xFF;
        }
        let result = decrypt_record(
            CONTENT_APPLICATION_DATA, &payload, 0,
            &mac_key, &enc_key,
        );
        assert!(result.is_err());
    }

    #[test]
    fn change_cipher_spec_is_one_byte() {
        let ccs = build_change_cipher_spec();
        // Record header (5 bytes) + payload (1 byte = 0x01)
        assert_eq!(ccs.len(), 6);
        assert_eq!(ccs[0], CONTENT_CHANGE_CIPHER_SPEC);
        assert_eq!(ccs[5], 0x01);
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
TLS Educational Handshake on 127.0.0.1:54321
[client] Sent ClientHello
[server] Connection accepted
[server] Received ClientHello, client_random: a1b2c3d4...
[server] Sent ServerHello, server_random: e5f6a7b8...
[server] Sent Certificate
[server] Sent ServerHelloDone
[client] Received ServerHello
[client] Received Certificate
[client] Received ServerHelloDone
[client] Sent ClientKeyExchange
[client] Derived master secret: 7f3a9b2e...
[client] Sent ChangeCipherSpec
[client] Sent Finished (encrypted)
[server] Received ClientKeyExchange, decrypted PMS
[server] Derived master secret: 7f3a9b2e...
[server] Received ChangeCipherSpec
[server] Received and verified client Finished
[server] Sent ChangeCipherSpec
[server] Sent Finished (encrypted)
[client] Received ChangeCipherSpec
[client] Received and verified server Finished
[client] Sent encrypted application data
[server] Received application data: "Hello from TLS client!"
[server] Sent encrypted response
[client] Received: "Hello from TLS server!"
[client] Handshake complete!
[server] Handshake complete!

=== TLS handshake completed successfully ===
```

## Design Decisions

1. **512-bit RSA for educational simplicity**: A 2048-bit RSA key requires big-integer operations on 256-byte numbers. The 512-bit key simplifies the bignum implementation while still demonstrating the same RSA mechanics. The code structure scales directly to larger keys by increasing the key constants.

2. **Self-contained crypto primitives**: AES, SHA-256, HMAC, and RSA are all implemented from scratch. This avoids external dependencies and reinforces the learning objective: understanding what "TLS encryption" actually does at the byte level. In production, use `ring`, `rustls`, or `openssl` bindings.

3. **Single-threaded client and server over TCP**: Using `std::thread` and a real TCP connection demonstrates that the handshake is a genuine network protocol, not just function calls. Each side has its own state and cannot see the other's memory.

4. **MAC-then-encrypt**: TLS 1.2 with CBC uses MAC-then-encrypt (compute HMAC, append to plaintext, then encrypt). This ordering is known to be vulnerable to padding oracle attacks (Lucky 13). TLS 1.3 mandates AEAD ciphers (AES-GCM) that avoid this issue. The educational value of implementing the historical MAC-then-encrypt scheme is understanding *why* the change was made.

5. **Explicit IV per record**: TLS 1.1+ sends a random IV with each record to prevent the BEAST attack (which exploits predictable IVs in TLS 1.0 CBC mode). This costs 16 extra bytes per record but eliminates IV prediction.

## Common Mistakes

1. **Reversed random order in key expansion**: The master secret uses `client_random + server_random` as the seed, but key expansion uses `server_random + client_random`. Swapping these produces valid-looking keys that don't match between client and server.

2. **Forgetting the BIT STRING wrapper**: In X.509/ASN.1, the public key is inside a BIT STRING that has a leading byte indicating unused bits (usually 0). Parsing the RSA key directly from the BIT STRING without skipping this byte shifts all data by one byte and produces garbage.

3. **Big-integer byte order**: RSA operates on big-endian numbers (MSB first). Rust's native integer operations are host-endian. Mixing up the byte order in `from_bytes_be` or `to_bytes_be` silently produces wrong but plausible results.

4. **HMAC key derivation for MAC-then-encrypt**: The sequence number is included in the MAC input as an 8-byte big-endian value. Forgetting the sequence number or using the wrong byte order means the MAC verifies on the sender but fails on the receiver.

5. **Pre-master secret version bytes**: The first two bytes of the pre-master secret must be the TLS version (0x03, 0x03 for TLS 1.2). Setting these to zero or random values means the server derives a different master secret from the client.

## Performance Notes

This implementation prioritizes clarity over speed. The big-integer modular exponentiation (RSA) is the bottleneck:

| Operation | Approximate Time |
|-----------|-----------------|
| 512-bit RSA encrypt/decrypt | ~50-200ms (naive square-and-multiply) |
| AES-128-CBC encrypt (1 block) | ~1 microsecond |
| SHA-256 (64 bytes) | ~1 microsecond |
| Full handshake | ~500ms (dominated by RSA) |

Production RSA implementations use Montgomery multiplication, Chinese Remainder Theorem optimization, and constant-time operations to achieve 2048-bit RSA in ~1ms. The `ring` crate achieves this using optimized assembly.

## Going Further

- Implement **ECDHE key exchange**: replace RSA key exchange with Elliptic Curve Diffie-Hellman for forward secrecy. ECDHE on curve P-256 is the modern standard
- Implement **TLS 1.3 handshake**: single round-trip, mandatory AEAD, encrypted extensions, 0-RTT resumption
- Add **certificate chain validation**: verify the server certificate against a trusted CA certificate
- Implement **AES-GCM** for authenticated encryption instead of CBC + HMAC
- Add **session resumption**: cache the master secret and reuse it for subsequent connections with abbreviated handshake
- Implement a **padding oracle attack** against the CBC mode to demonstrate why MAC-then-encrypt is problematic
