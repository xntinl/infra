# Solution: AES Block Cipher Educational Implementation

## Architecture Overview

The implementation follows the FIPS 197 specification directly, organized as:

1. **Constants**: S-box, inverse S-box, Rcon array -- all precomputed lookup tables
2. **GF(2^8) arithmetic**: `xtime()` and `gf_mul()` for Galois field multiplication
3. **Round operations**: SubBytes, ShiftRows, MixColumns, AddRoundKey and their inverses
4. **Key expansion**: Rijndael key schedule producing 11 round keys from a 128-bit key
5. **Cipher core**: 10-round encryption and decryption operating on a single 16-byte block
6. **Modes**: ECB and CBC with PKCS#7 padding
7. **Visualization**: hex dump of the 4x4 state after every operation

The state is represented as `[[u8; 4]; 4]` -- 4 rows by 4 columns, column-major input ordering.

## Rust Solution

```rust
// --- AES S-Box (FIPS 197, Section 5.1.1) ---

const SBOX: [u8; 256] = [
    0x63, 0x7c, 0x77, 0x7b, 0xf2, 0x6b, 0x6f, 0xc5, 0x30, 0x01, 0x67, 0x2b, 0xfe, 0xd7, 0xab, 0x76,
    0xca, 0x82, 0xc9, 0x7d, 0xfa, 0x59, 0x47, 0xf0, 0xad, 0xd4, 0xa2, 0xaf, 0x9c, 0xa4, 0x72, 0xc0,
    0xb7, 0xfd, 0x93, 0x26, 0x36, 0x3f, 0xf7, 0xcc, 0x34, 0xa5, 0xe5, 0xf1, 0x71, 0xd8, 0x31, 0x15,
    0x04, 0xc7, 0x23, 0xc3, 0x18, 0x96, 0x05, 0x9a, 0x07, 0x12, 0x80, 0xe2, 0xeb, 0x27, 0xb2, 0x75,
    0x09, 0x83, 0x2c, 0x1a, 0x1b, 0x6e, 0x5a, 0xa0, 0x52, 0x3b, 0xd6, 0xb3, 0x29, 0xe3, 0x2f, 0x84,
    0x53, 0xd1, 0x00, 0xed, 0x20, 0xfc, 0xb1, 0x5b, 0x6a, 0xcb, 0xbe, 0x39, 0x4a, 0x4c, 0x58, 0xcf,
    0xd0, 0xef, 0xaa, 0xfb, 0x43, 0x4d, 0x33, 0x85, 0x45, 0xf9, 0x02, 0x7f, 0x50, 0x3c, 0x9f, 0xa8,
    0x51, 0xa3, 0x40, 0x8f, 0x92, 0x9d, 0x38, 0xf5, 0xbc, 0xb6, 0xda, 0x21, 0x10, 0xff, 0xf3, 0xd2,
    0xcd, 0x0c, 0x13, 0xec, 0x5f, 0x97, 0x44, 0x17, 0xc4, 0xa7, 0x7e, 0x3d, 0x64, 0x5d, 0x19, 0x73,
    0x60, 0x81, 0x4f, 0xdc, 0x22, 0x2a, 0x90, 0x88, 0x46, 0xee, 0xb8, 0x14, 0xde, 0x5e, 0x0b, 0xdb,
    0xe0, 0x32, 0x3a, 0x0a, 0x49, 0x06, 0x24, 0x5c, 0xc2, 0xd3, 0xac, 0x62, 0x91, 0x95, 0xe4, 0x79,
    0xe7, 0xc8, 0x37, 0x6d, 0x8d, 0xd5, 0x4e, 0xa9, 0x6c, 0x56, 0xf4, 0xea, 0x65, 0x7a, 0xae, 0x08,
    0xba, 0x78, 0x25, 0x2e, 0x1c, 0xa6, 0xb4, 0xc6, 0xe8, 0xdd, 0x74, 0x1f, 0x4b, 0xbd, 0x8b, 0x8a,
    0x70, 0x3e, 0xb5, 0x66, 0x48, 0x03, 0xf6, 0x0e, 0x61, 0x35, 0x57, 0xb9, 0x86, 0xc1, 0x1d, 0x9e,
    0xe1, 0xf8, 0x98, 0x11, 0x69, 0xd9, 0x8e, 0x94, 0x9b, 0x1e, 0x87, 0xe9, 0xce, 0x55, 0x28, 0xdf,
    0x8c, 0xa1, 0x89, 0x0d, 0xbf, 0xe6, 0x42, 0x68, 0x41, 0x99, 0x2d, 0x0f, 0xb0, 0x54, 0xbb, 0x16,
];

const INV_SBOX: [u8; 256] = [
    0x52, 0x09, 0x6a, 0xd5, 0x30, 0x36, 0xa5, 0x38, 0xbf, 0x40, 0xa3, 0x9e, 0x81, 0xf3, 0xd7, 0xfb,
    0x7c, 0xe3, 0x39, 0x82, 0x9b, 0x2f, 0xff, 0x87, 0x34, 0x8e, 0x43, 0x44, 0xc4, 0xde, 0xe9, 0xcb,
    0x54, 0x7b, 0x94, 0x32, 0xa6, 0xc2, 0x23, 0x3d, 0xee, 0x4c, 0x95, 0x0b, 0x42, 0xfa, 0xc3, 0x4e,
    0x08, 0x2e, 0xa1, 0x66, 0x28, 0xd9, 0x24, 0xb2, 0x76, 0x5b, 0xa2, 0x49, 0x6d, 0x8b, 0xd1, 0x25,
    0x72, 0xf8, 0xf6, 0x64, 0x86, 0x68, 0x98, 0x16, 0xd4, 0xa4, 0x5c, 0xcc, 0x5d, 0x65, 0xb6, 0x92,
    0x6c, 0x70, 0x48, 0x50, 0xfd, 0xed, 0xb9, 0xda, 0x5e, 0x15, 0x46, 0x57, 0xa7, 0x8d, 0x9d, 0x84,
    0x90, 0xd8, 0xab, 0x00, 0x8c, 0xbc, 0xd3, 0x0a, 0xf7, 0xe4, 0x58, 0x05, 0xb8, 0xb3, 0x45, 0x06,
    0xd0, 0x2c, 0x1e, 0x8f, 0xca, 0x3f, 0x0f, 0x02, 0xc1, 0xaf, 0xbd, 0x03, 0x01, 0x13, 0x8a, 0x6b,
    0x3a, 0x91, 0x11, 0x41, 0x4f, 0x67, 0xdc, 0xea, 0x97, 0xf2, 0xcf, 0xce, 0xf0, 0xb4, 0xe6, 0x73,
    0x96, 0xac, 0x74, 0x22, 0xe7, 0xad, 0x35, 0x85, 0xe2, 0xf9, 0x37, 0xe8, 0x1c, 0x75, 0xdf, 0x6e,
    0x47, 0xf1, 0x1a, 0x71, 0x1d, 0x29, 0xc5, 0x89, 0x6f, 0xb7, 0x62, 0x0e, 0xaa, 0x18, 0xbe, 0x1b,
    0xfc, 0x56, 0x3e, 0x4b, 0xc6, 0xd2, 0x79, 0x20, 0x9a, 0xdb, 0xc0, 0xfe, 0x78, 0xcd, 0x5a, 0xf4,
    0x1f, 0xdd, 0xa8, 0x33, 0x88, 0x07, 0xc7, 0x31, 0xb1, 0x12, 0x10, 0x59, 0x27, 0x80, 0xec, 0x5f,
    0x60, 0x51, 0x7f, 0xa9, 0x19, 0xb5, 0x4a, 0x0d, 0x2d, 0xe5, 0x7a, 0x9f, 0x93, 0xc9, 0x9c, 0xef,
    0xa0, 0xe0, 0x3b, 0x4d, 0xae, 0x2a, 0xf5, 0xb0, 0xc8, 0xeb, 0xbb, 0x3c, 0x83, 0x53, 0x99, 0x61,
    0x17, 0x2b, 0x04, 0x7e, 0xba, 0x77, 0xd6, 0x26, 0xe1, 0x69, 0x14, 0x63, 0x55, 0x21, 0x0c, 0x7d,
];

const RCON: [u8; 10] = [0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x1b, 0x36];

type State = [[u8; 4]; 4]; // [row][col]
type RoundKey = [u8; 16];

// --- GF(2^8) arithmetic ---

/// Multiply by 2 in GF(2^8) with irreducible polynomial x^8 + x^4 + x^3 + x + 1.
fn xtime(a: u8) -> u8 {
    let shifted = (a as u16) << 1;
    if shifted & 0x100 != 0 {
        (shifted ^ 0x11b) as u8
    } else {
        shifted as u8
    }
}

/// General multiplication in GF(2^8) using repeated xtime.
fn gf_mul(mut a: u8, mut b: u8) -> u8 {
    let mut result: u8 = 0;
    while b > 0 {
        if b & 1 != 0 {
            result ^= a;
        }
        a = xtime(a);
        b >>= 1;
    }
    result
}

// --- State conversion ---

fn bytes_to_state(input: &[u8; 16]) -> State {
    let mut state = [[0u8; 4]; 4];
    for col in 0..4 {
        for row in 0..4 {
            state[row][col] = input[col * 4 + row];
        }
    }
    state
}

fn state_to_bytes(state: &State) -> [u8; 16] {
    let mut output = [0u8; 16];
    for col in 0..4 {
        for row in 0..4 {
            output[col * 4 + row] = state[row][col];
        }
    }
    output
}

// --- Round operations ---

fn sub_bytes(state: &mut State) {
    for row in 0..4 {
        for col in 0..4 {
            state[row][col] = SBOX[state[row][col] as usize];
        }
    }
}

fn inv_sub_bytes(state: &mut State) {
    for row in 0..4 {
        for col in 0..4 {
            state[row][col] = INV_SBOX[state[row][col] as usize];
        }
    }
}

fn shift_rows(state: &mut State) {
    // Row 0: no shift
    // Row 1: shift left by 1
    let tmp = state[1][0];
    state[1][0] = state[1][1];
    state[1][1] = state[1][2];
    state[1][2] = state[1][3];
    state[1][3] = tmp;

    // Row 2: shift left by 2
    let (t0, t1) = (state[2][0], state[2][1]);
    state[2][0] = state[2][2];
    state[2][1] = state[2][3];
    state[2][2] = t0;
    state[2][3] = t1;

    // Row 3: shift left by 3 (= shift right by 1)
    let tmp = state[3][3];
    state[3][3] = state[3][2];
    state[3][2] = state[3][1];
    state[3][1] = state[3][0];
    state[3][0] = tmp;
}

fn inv_shift_rows(state: &mut State) {
    // Row 1: shift right by 1
    let tmp = state[1][3];
    state[1][3] = state[1][2];
    state[1][2] = state[1][1];
    state[1][1] = state[1][0];
    state[1][0] = tmp;

    // Row 2: shift right by 2
    let (t0, t1) = (state[2][0], state[2][1]);
    state[2][0] = state[2][2];
    state[2][1] = state[2][3];
    state[2][2] = t0;
    state[2][3] = t1;

    // Row 3: shift right by 3 (= shift left by 1)
    let tmp = state[3][0];
    state[3][0] = state[3][1];
    state[3][1] = state[3][2];
    state[3][2] = state[3][3];
    state[3][3] = tmp;
}

fn mix_columns(state: &mut State) {
    for col in 0..4 {
        let s0 = state[0][col];
        let s1 = state[1][col];
        let s2 = state[2][col];
        let s3 = state[3][col];

        state[0][col] = gf_mul(2, s0) ^ gf_mul(3, s1) ^ s2 ^ s3;
        state[1][col] = s0 ^ gf_mul(2, s1) ^ gf_mul(3, s2) ^ s3;
        state[2][col] = s0 ^ s1 ^ gf_mul(2, s2) ^ gf_mul(3, s3);
        state[3][col] = gf_mul(3, s0) ^ s1 ^ s2 ^ gf_mul(2, s3);
    }
}

fn inv_mix_columns(state: &mut State) {
    for col in 0..4 {
        let s0 = state[0][col];
        let s1 = state[1][col];
        let s2 = state[2][col];
        let s3 = state[3][col];

        state[0][col] = gf_mul(14, s0) ^ gf_mul(11, s1) ^ gf_mul(13, s2) ^ gf_mul(9, s3);
        state[1][col] = gf_mul(9, s0) ^ gf_mul(14, s1) ^ gf_mul(11, s2) ^ gf_mul(13, s3);
        state[2][col] = gf_mul(13, s0) ^ gf_mul(9, s1) ^ gf_mul(14, s2) ^ gf_mul(11, s3);
        state[3][col] = gf_mul(11, s0) ^ gf_mul(13, s1) ^ gf_mul(9, s2) ^ gf_mul(14, s3);
    }
}

fn add_round_key(state: &mut State, round_key: &RoundKey) {
    for col in 0..4 {
        for row in 0..4 {
            state[row][col] ^= round_key[col * 4 + row];
        }
    }
}

// --- Key expansion ---

pub fn key_expansion(key: &[u8; 16]) -> Vec<RoundKey> {
    let nk = 4; // number of 32-bit words in the key
    let nr = 10; // number of rounds
    let total_words = 4 * (nr + 1); // 44 words

    let mut w: Vec<[u8; 4]> = Vec::with_capacity(total_words);

    // First Nk words are the key itself
    for i in 0..nk {
        w.push([key[4 * i], key[4 * i + 1], key[4 * i + 2], key[4 * i + 3]]);
    }

    for i in nk..total_words {
        let mut temp = w[i - 1];

        if i % nk == 0 {
            // RotWord
            temp = [temp[1], temp[2], temp[3], temp[0]];
            // SubWord
            for byte in &mut temp {
                *byte = SBOX[*byte as usize];
            }
            // XOR with Rcon
            temp[0] ^= RCON[i / nk - 1];
        }

        let prev = w[i - nk];
        w.push([
            prev[0] ^ temp[0],
            prev[1] ^ temp[1],
            prev[2] ^ temp[2],
            prev[3] ^ temp[3],
        ]);
    }

    // Convert to round keys (each round key is 4 words = 16 bytes)
    let mut round_keys = Vec::with_capacity(nr + 1);
    for round in 0..=nr {
        let mut rk = [0u8; 16];
        for word in 0..4 {
            let w_idx = round * 4 + word;
            for byte in 0..4 {
                rk[word * 4 + byte] = w[w_idx][byte];
            }
        }
        round_keys.push(rk);
    }

    round_keys
}

// --- Visualization ---

fn print_state(label: &str, state: &State) {
    println!("  {}", label);
    for row in 0..4 {
        print!("    ");
        for col in 0..4 {
            print!("{:02x} ", state[row][col]);
        }
        println!();
    }
}

fn print_round_key(round: usize, key: &RoundKey) {
    println!("  Round key [{}]:", round);
    let state = bytes_to_state(key);
    for row in 0..4 {
        print!("    ");
        for col in 0..4 {
            print!("{:02x} ", state[row][col]);
        }
        println!();
    }
}

// --- Cipher ---

pub fn aes128_encrypt_block(plaintext: &[u8; 16], key: &[u8; 16], verbose: bool) -> [u8; 16] {
    let round_keys = key_expansion(key);
    let mut state = bytes_to_state(plaintext);

    if verbose {
        println!("ENCRYPTION:");
        print_state("Input:", &state);
    }

    // Initial round key addition
    add_round_key(&mut state, &round_keys[0]);
    if verbose {
        print_round_key(0, &round_keys[0]);
        print_state("After AddRoundKey[0]:", &state);
    }

    // Rounds 1-9
    for round in 1..10 {
        if verbose {
            println!("\n  --- Round {} ---", round);
        }

        sub_bytes(&mut state);
        if verbose { print_state("After SubBytes:", &state); }

        shift_rows(&mut state);
        if verbose { print_state("After ShiftRows:", &state); }

        mix_columns(&mut state);
        if verbose { print_state("After MixColumns:", &state); }

        add_round_key(&mut state, &round_keys[round]);
        if verbose {
            print_round_key(round, &round_keys[round]);
            print_state("After AddRoundKey:", &state);
        }
    }

    // Final round (no MixColumns)
    if verbose {
        println!("\n  --- Round 10 (final) ---");
    }

    sub_bytes(&mut state);
    if verbose { print_state("After SubBytes:", &state); }

    shift_rows(&mut state);
    if verbose { print_state("After ShiftRows:", &state); }

    add_round_key(&mut state, &round_keys[10]);
    if verbose {
        print_round_key(10, &round_keys[10]);
        print_state("After AddRoundKey:", &state);
    }

    state_to_bytes(&state)
}

pub fn aes128_decrypt_block(ciphertext: &[u8; 16], key: &[u8; 16], verbose: bool) -> [u8; 16] {
    let round_keys = key_expansion(key);
    let mut state = bytes_to_state(ciphertext);

    if verbose {
        println!("DECRYPTION:");
        print_state("Input:", &state);
    }

    // Initial round key addition (round 10)
    add_round_key(&mut state, &round_keys[10]);
    if verbose { print_state("After AddRoundKey[10]:", &state); }

    // Rounds 9 down to 1
    for round in (1..10).rev() {
        if verbose {
            println!("\n  --- Round {} ---", round);
        }

        inv_shift_rows(&mut state);
        if verbose { print_state("After InvShiftRows:", &state); }

        inv_sub_bytes(&mut state);
        if verbose { print_state("After InvSubBytes:", &state); }

        add_round_key(&mut state, &round_keys[round]);
        if verbose { print_state("After AddRoundKey:", &state); }

        inv_mix_columns(&mut state);
        if verbose { print_state("After InvMixColumns:", &state); }
    }

    // Final round (no InvMixColumns)
    if verbose {
        println!("\n  --- Round 0 (final) ---");
    }

    inv_shift_rows(&mut state);
    if verbose { print_state("After InvShiftRows:", &state); }

    inv_sub_bytes(&mut state);
    if verbose { print_state("After InvSubBytes:", &state); }

    add_round_key(&mut state, &round_keys[0]);
    if verbose { print_state("After AddRoundKey[0]:", &state); }

    state_to_bytes(&state)
}

// --- PKCS#7 Padding ---

pub fn pkcs7_pad(data: &[u8]) -> Vec<u8> {
    let block_size = 16;
    let padding_len = block_size - (data.len() % block_size);
    let mut padded = data.to_vec();
    padded.extend(std::iter::repeat(padding_len as u8).take(padding_len));
    padded
}

pub fn pkcs7_unpad(data: &[u8]) -> Result<Vec<u8>, &'static str> {
    if data.is_empty() || data.len() % 16 != 0 {
        return Err("invalid padded data length");
    }
    let padding_len = *data.last().unwrap() as usize;
    if padding_len == 0 || padding_len > 16 {
        return Err("invalid padding value");
    }
    if data.len() < padding_len {
        return Err("padding length exceeds data");
    }
    // Verify all padding bytes
    for &byte in &data[data.len() - padding_len..] {
        if byte != padding_len as u8 {
            return Err("inconsistent padding bytes");
        }
    }
    Ok(data[..data.len() - padding_len].to_vec())
}

// --- ECB Mode ---

pub fn aes128_ecb_encrypt(plaintext: &[u8], key: &[u8; 16]) -> Vec<u8> {
    let padded = pkcs7_pad(plaintext);
    let mut ciphertext = Vec::with_capacity(padded.len());

    for chunk in padded.chunks(16) {
        let block: [u8; 16] = chunk.try_into().unwrap();
        let encrypted = aes128_encrypt_block(&block, key, false);
        ciphertext.extend_from_slice(&encrypted);
    }

    ciphertext
}

pub fn aes128_ecb_decrypt(ciphertext: &[u8], key: &[u8; 16]) -> Result<Vec<u8>, &'static str> {
    if ciphertext.is_empty() || ciphertext.len() % 16 != 0 {
        return Err("ciphertext length must be a multiple of 16");
    }

    let mut plaintext = Vec::with_capacity(ciphertext.len());

    for chunk in ciphertext.chunks(16) {
        let block: [u8; 16] = chunk.try_into().unwrap();
        let decrypted = aes128_decrypt_block(&block, key, false);
        plaintext.extend_from_slice(&decrypted);
    }

    pkcs7_unpad(&plaintext)
}

// --- CBC Mode ---

pub fn aes128_cbc_encrypt(plaintext: &[u8], key: &[u8; 16], iv: &[u8; 16]) -> Vec<u8> {
    let padded = pkcs7_pad(plaintext);
    let mut ciphertext = Vec::with_capacity(padded.len());
    let mut prev_block = *iv;

    for chunk in padded.chunks(16) {
        let mut block = [0u8; 16];
        for i in 0..16 {
            block[i] = chunk[i] ^ prev_block[i];
        }
        let encrypted = aes128_encrypt_block(&block, key, false);
        ciphertext.extend_from_slice(&encrypted);
        prev_block = encrypted;
    }

    ciphertext
}

pub fn aes128_cbc_decrypt(
    ciphertext: &[u8],
    key: &[u8; 16],
    iv: &[u8; 16],
) -> Result<Vec<u8>, &'static str> {
    if ciphertext.is_empty() || ciphertext.len() % 16 != 0 {
        return Err("ciphertext length must be a multiple of 16");
    }

    let mut plaintext = Vec::with_capacity(ciphertext.len());
    let mut prev_block = *iv;

    for chunk in ciphertext.chunks(16) {
        let ct_block: [u8; 16] = chunk.try_into().unwrap();
        let decrypted = aes128_decrypt_block(&ct_block, key, false);
        let mut pt_block = [0u8; 16];
        for i in 0..16 {
            pt_block[i] = decrypted[i] ^ prev_block[i];
        }
        plaintext.extend_from_slice(&pt_block);
        prev_block = ct_block;
    }

    pkcs7_unpad(&plaintext)
}

// --- Hex helpers ---

fn hex_str(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{:02x}", b)).collect::<Vec<_>>().join("")
}

fn main() {
    // FIPS 197 Appendix B test vector
    let key: [u8; 16] = [
        0x2b, 0x7e, 0x15, 0x16, 0x28, 0xae, 0xd2, 0xa6,
        0xab, 0xf7, 0x15, 0x88, 0x09, 0xcf, 0x4f, 0x3c,
    ];
    let plaintext: [u8; 16] = [
        0x32, 0x43, 0xf6, 0xa8, 0x88, 0x5a, 0x30, 0x8d,
        0x31, 0x31, 0x98, 0xa2, 0xe0, 0x37, 0x07, 0x34,
    ];
    let expected_ct: [u8; 16] = [
        0x39, 0x25, 0x84, 0x1d, 0x02, 0xdc, 0x09, 0xfb,
        0xdc, 0x11, 0x85, 0x97, 0x19, 0x6a, 0x0b, 0x32,
    ];

    println!("=== FIPS 197 Appendix B Test Vector ===\n");
    println!("Key:       {}", hex_str(&key));
    println!("Plaintext: {}", hex_str(&plaintext));

    let ciphertext = aes128_encrypt_block(&plaintext, &key, true);
    println!("\nCiphertext: {}", hex_str(&ciphertext));
    println!("Expected:   {}", hex_str(&expected_ct));
    println!("Match: {}\n", ciphertext == expected_ct);

    let decrypted = aes128_decrypt_block(&ciphertext, &key, false);
    println!("Decrypted:  {}", hex_str(&decrypted));
    println!("Match: {}\n", decrypted == plaintext);

    // ECB vs CBC demonstration
    println!("=== ECB vs CBC ===\n");
    let msg = b"AAAAAAAAAAAAAAAA AAAAAAAAAAAAAAAA"; // repeated blocks
    let iv = [0u8; 16];

    let ecb = aes128_ecb_encrypt(msg, &key);
    let cbc = aes128_cbc_encrypt(msg, &key, &iv);

    println!("Message: {:?}", String::from_utf8_lossy(msg));
    println!("ECB block 1: {}", hex_str(&ecb[0..16]));
    println!("ECB block 2: {}", hex_str(&ecb[16..32]));
    println!("ECB blocks identical: {}", ecb[0..16] == ecb[16..32]);
    println!("CBC block 1: {}", hex_str(&cbc[0..16]));
    println!("CBC block 2: {}", hex_str(&cbc[16..32]));
    println!("CBC blocks identical: {}", cbc[0..16] == cbc[16..32]);
}

// --- Tests ---

#[cfg(test)]
mod tests {
    use super::*;

    // FIPS 197 Appendix B
    const TEST_KEY: [u8; 16] = [
        0x2b, 0x7e, 0x15, 0x16, 0x28, 0xae, 0xd2, 0xa6,
        0xab, 0xf7, 0x15, 0x88, 0x09, 0xcf, 0x4f, 0x3c,
    ];
    const TEST_PT: [u8; 16] = [
        0x32, 0x43, 0xf6, 0xa8, 0x88, 0x5a, 0x30, 0x8d,
        0x31, 0x31, 0x98, 0xa2, 0xe0, 0x37, 0x07, 0x34,
    ];
    const TEST_CT: [u8; 16] = [
        0x39, 0x25, 0x84, 0x1d, 0x02, 0xdc, 0x09, 0xfb,
        0xdc, 0x11, 0x85, 0x97, 0x19, 0x6a, 0x0b, 0x32,
    ];

    #[test]
    fn test_fips197_encrypt() {
        let ct = aes128_encrypt_block(&TEST_PT, &TEST_KEY, false);
        assert_eq!(ct, TEST_CT);
    }

    #[test]
    fn test_fips197_decrypt() {
        let pt = aes128_decrypt_block(&TEST_CT, &TEST_KEY, false);
        assert_eq!(pt, TEST_PT);
    }

    #[test]
    fn test_roundtrip_block() {
        let key = [0x01u8; 16];
        let pt = [0x42u8; 16];
        let ct = aes128_encrypt_block(&pt, &key, false);
        let recovered = aes128_decrypt_block(&ct, &key, false);
        assert_eq!(recovered, pt);
    }

    #[test]
    fn test_key_expansion_first_round() {
        let rks = key_expansion(&TEST_KEY);
        assert_eq!(rks[0], TEST_KEY);
        assert_eq!(rks.len(), 11);
    }

    #[test]
    fn test_xtime() {
        assert_eq!(xtime(0x57), 0xae);
        assert_eq!(xtime(0xae), 0x47); // overflow: (0x15c) ^ 0x11b = 0x47
    }

    #[test]
    fn test_gf_mul() {
        assert_eq!(gf_mul(0x57, 0x83), 0xc1); // FIPS 197, Section 4.2.1
    }

    #[test]
    fn test_sub_bytes_known() {
        assert_eq!(SBOX[0x00], 0x63);
        assert_eq!(SBOX[0x53], 0xed);
        assert_eq!(INV_SBOX[0x63], 0x00);
        assert_eq!(INV_SBOX[0xed], 0x53);
    }

    #[test]
    fn test_shift_rows_roundtrip() {
        let mut state = [[1, 2, 3, 4], [5, 6, 7, 8], [9, 10, 11, 12], [13, 14, 15, 16]];
        let original = state;
        shift_rows(&mut state);
        inv_shift_rows(&mut state);
        assert_eq!(state, original);
    }

    #[test]
    fn test_mix_columns_roundtrip() {
        let mut state = [[0xdb, 0xf2, 0x01, 0xc6],
                         [0x13, 0x0a, 0x01, 0xc6],
                         [0x53, 0x22, 0x01, 0xc6],
                         [0x45, 0x5c, 0x01, 0xc6]];
        let original = state;
        mix_columns(&mut state);
        inv_mix_columns(&mut state);
        assert_eq!(state, original);
    }

    #[test]
    fn test_pkcs7_padding() {
        let data = b"hello"; // 5 bytes -> 11 bytes padding
        let padded = pkcs7_pad(data);
        assert_eq!(padded.len(), 16);
        assert_eq!(padded[15], 11);

        let unpadded = pkcs7_unpad(&padded).unwrap();
        assert_eq!(unpadded, data);
    }

    #[test]
    fn test_pkcs7_full_block() {
        let data = [0x41u8; 16]; // exactly 16 bytes -> adds 16 bytes of padding
        let padded = pkcs7_pad(&data);
        assert_eq!(padded.len(), 32);
        assert_eq!(padded[31], 16);

        let unpadded = pkcs7_unpad(&padded).unwrap();
        assert_eq!(unpadded, data);
    }

    #[test]
    fn test_ecb_roundtrip() {
        let msg = b"Hello AES-128 ECB mode test!";
        let ct = aes128_ecb_encrypt(msg, &TEST_KEY);
        let pt = aes128_ecb_decrypt(&ct, &TEST_KEY).unwrap();
        assert_eq!(pt, msg);
    }

    #[test]
    fn test_ecb_identical_blocks() {
        let msg = [0x41u8; 32]; // two identical 16-byte blocks
        let ct = aes128_ecb_encrypt(&msg, &TEST_KEY);
        // ECB encrypts identical blocks to identical ciphertext
        assert_eq!(&ct[0..16], &ct[16..32]);
    }

    #[test]
    fn test_cbc_roundtrip() {
        let msg = b"Hello AES-128 CBC mode test!";
        let iv = [0x00u8; 16];
        let ct = aes128_cbc_encrypt(msg, &TEST_KEY, &iv);
        let pt = aes128_cbc_decrypt(&ct, &TEST_KEY, &iv).unwrap();
        assert_eq!(pt, msg);
    }

    #[test]
    fn test_cbc_identical_blocks_differ() {
        let msg = [0x41u8; 32]; // two identical 16-byte blocks
        let iv = [0x00u8; 16];
        let ct = aes128_cbc_encrypt(&msg, &TEST_KEY, &iv);
        // CBC should produce different ciphertext for identical blocks
        assert_ne!(&ct[0..16], &ct[16..32]);
    }

    #[test]
    fn test_cbc_different_iv() {
        let msg = b"same plaintext";
        let iv1 = [0x00u8; 16];
        let iv2 = [0xffu8; 16];
        let ct1 = aes128_cbc_encrypt(msg, &TEST_KEY, &iv1);
        let ct2 = aes128_cbc_encrypt(msg, &TEST_KEY, &iv2);
        assert_ne!(ct1, ct2);
    }

    #[test]
    fn test_different_keys_produce_different_ciphertext() {
        let key2 = [0xffu8; 16];
        let ct1 = aes128_encrypt_block(&TEST_PT, &TEST_KEY, false);
        let ct2 = aes128_encrypt_block(&TEST_PT, &key2, false);
        assert_ne!(ct1, ct2);
    }

    #[test]
    fn test_state_conversion_roundtrip() {
        let input: [u8; 16] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15];
        let state = bytes_to_state(&input);
        let output = state_to_bytes(&state);
        assert_eq!(input, output);
    }
}
```

## Running

```bash
cargo init aes-educational
cd aes-educational

# Copy the solution to src/main.rs
# No external dependencies needed

cargo run
cargo test
cargo test -- --nocapture  # to see visualization output
```

## Expected Output

```
=== FIPS 197 Appendix B Test Vector ===

Key:       2b7e151628aed2a6abf7158809cf4f3c
Plaintext: 3243f6a8885a308d313198a2e0370734
ENCRYPTION:
  Input:
    32 88 31 e0
    43 5a 31 37
    f6 30 98 07
    a8 8d a2 34
  ...
  --- Round 10 (final) ---
  After SubBytes:
    ...
  After ShiftRows:
    ...
  After AddRoundKey:
    39 02 dc 19
    25 dc 11 6a
    84 09 85 0b
    1d fb 97 32

Ciphertext: 3925841d02dc09fbdc118597196a0b32
Expected:   3925841d02dc09fbdc118597196a0b32
Match: true

=== ECB vs CBC ===

ECB blocks identical: true
CBC blocks identical: false
```

## Design Decisions

1. **Lookup-table S-box**: The S-box is precomputed from the GF(2^8) inversion and affine transform specified in FIPS 197. Computing it at runtime is possible but adds complexity without educational value. The full 256-byte table is included for both S-box and inverse S-box.

2. **General `gf_mul` over specialized functions**: Instead of writing separate functions for multiply-by-2, multiply-by-3, etc., a single `gf_mul(a, b)` using the shift-and-XOR algorithm handles all cases. This is slower than specialized versions but clearer for learning.

3. **Column-major state layout**: Following FIPS 197 exactly: `state[row][col]` with column-major input mapping. This is counterintuitive (input byte 1 goes to row 1, column 0, not row 0, column 1) but matching the spec makes verification against test vectors straightforward.

4. **No constant-time operations**: This implementation uses table lookups and conditional branches that leak timing information. A production implementation would use bitsliced S-box computation or hardware AES-NI. The educational value of the lookup table approach outweighs the security concern for a learning exercise.

5. **PKCS#7 padding for aligned input**: When the plaintext is already a multiple of 16 bytes, PKCS#7 adds a full 16-byte padding block. This ensures unpadding is always unambiguous -- the last byte always indicates the padding length.

## Common Mistakes

1. **Row-major vs column-major state**: The most common AES implementation bug. Input bytes fill columns, not rows. Byte 0 is `state[0][0]`, byte 1 is `state[1][0]`, byte 4 is `state[0][1]`. Getting this wrong makes every round produce wrong output.

2. **MixColumns reduction**: Forgetting to reduce by the irreducible polynomial (XOR with 0x1B on overflow) in `xtime()` produces correct results for small values but fails for values >= 0x80.

3. **Decryption operation order**: Decryption is not simply applying inverse operations in forward order. The correct order for each round is: InvShiftRows, InvSubBytes, AddRoundKey, InvMixColumns. Swapping InvShiftRows and InvSubBytes works because they commute, but AddRoundKey and InvMixColumns do not commute.

4. **CBC decryption XOR target**: During CBC decryption, the decrypted block must be XORed with the **previous ciphertext** block, not the previous plaintext. Using plaintext produces garbage from block 2 onward.

5. **Key expansion Rcon indexing**: Rcon is indexed by `i/Nk - 1`, not `i`. Off-by-one here produces wrong round keys, which means correct encryption of round 1 but wrong output from round 2 onward.

## Performance Notes

This is an educational implementation. Performance is not a goal, but for reference:
- Each block encryption/decryption does 10 rounds with ~100 table lookups and ~160 XOR operations
- The lookup-table approach is vulnerable to cache-timing attacks (Bernstein 2005). Each S-box access loads a cache line that depends on secret data
- Production AES implementations use either AES-NI hardware instructions (single-cycle per round) or bitsliced implementations that process multiple blocks in parallel using bitwise operations, eliminating data-dependent memory access
- For multi-block encryption, CBC mode is inherently sequential (each block depends on the previous). CTR mode allows parallel encryption of all blocks

## Going Further

- Implement AES-192 and AES-256 (12 and 14 rounds, different key expansion)
- Implement CTR mode for parallelizable encryption
- Implement GCM (Galois/Counter Mode) for authenticated encryption
- Compute the S-box at runtime from the GF(2^8) multiplicative inverse and affine transform
- Implement a bitsliced version that processes 8 blocks in parallel using bitwise operations
- Compare timing of this implementation against the `aes` crate (hardware AES-NI) to quantify the performance gap
- Create the "ECB penguin" visualization: encrypt a bitmap image in ECB and CBC to visually demonstrate why ECB leaks patterns
