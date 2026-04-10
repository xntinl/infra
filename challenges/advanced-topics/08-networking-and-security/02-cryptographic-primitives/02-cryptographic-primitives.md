<!--
type: reference
difficulty: advanced
section: [08-networking-and-security]
concepts: [aes-gcm, chacha20-poly1305, blake3, hkdf, aead, nonce-discipline, authenticated-encryption, key-derivation]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [symmetric-cryptography-basics, xor-and-bit-operations, go-interfaces, rust-traits]
papers: [Bernstein 2005 — The Poly1305-AES Message Authentication Code, McGrew & Viega 2004 — The Galois/Counter Mode of Operation, Bellare & Rogaway 2000 — Encode-Then-Encipher Encryption]
industry_use: [openssl, libsodium, signal-protocol, wireguard, age-encryption]
language_contrast: high
-->

# Cryptographic Primitives

> A cryptographic primitive is only as strong as the discipline around its inputs —
> reusing a nonce with AES-GCM destroys both confidentiality and authenticity for every
> message ever encrypted under that key.

## Mental Model

The most important conceptual shift in modern symmetric cryptography is from encryption
to *authenticated encryption with associated data* (AEAD). Encryption alone — AES-CBC,
AES-CTR — guarantees confidentiality but nothing else. An attacker who can modify
ciphertexts and observe whether the server returns an error can mount a padding oracle
attack (Lucky13, POODLE, BEAST). AEAD primitives (AES-GCM, ChaCha20-Poly1305) encrypt
and authenticate simultaneously: if any bit of the ciphertext changes, decryption fails
with an authentication error before any plaintext is exposed.

The second mental shift is understanding what a nonce is and why nonce reuse is
catastrophic. A nonce is a "number used once" — a value that must never be repeated for
a given key. In AES-GCM, the nonce is a 96-bit input to the GCM counter mode. If you
encrypt two messages with the same key and nonce:

1. The keystream is identical, so XOR of the two ciphertexts equals XOR of the two
   plaintexts. An attacker who guesses words in one message can recover the other.
2. Both authentication tags are produced with the same H value (the GCM polynomial hash
   key, derived from the nonce). An attacker can forge authentication tags for arbitrary
   messages — full authenticity break.

This is not a theoretical concern: the "GCM nonce reuse" attack is well-documented, and
several real systems (WPA2 KRACK, certain TLS implementations under connection ID reuse)
have been broken by it. The correct nonce strategy for AES-GCM at scale is a 96-bit
random nonce with a 2^32 message limit per key (birthday bound: 2^48 messages before
collision probability exceeds 1%). Above that limit, re-key.

ChaCha20-Poly1305 has the same nonce requirement but is more forgiving in one sense: it
has no timing side channel on software without AES-NI hardware. On processors without
AES hardware acceleration, AES-GCM in software has cache-timing vulnerabilities; ChaCha20
is designed as a stream cipher with no table lookups, making it constant-time by
construction. WireGuard, Signal, and Let's Encrypt's internal tooling all prefer ChaCha20
for this reason.

## Core Concepts

### AES-GCM (AEAD)

AES-GCM combines AES-CTR (counter mode encryption) with GHASH (a Galois-field polynomial
authenticator). The result is an AEAD construction:

- `Seal(key, nonce, plaintext, additionalData) -> ciphertext || tag`
- `Open(key, nonce, ciphertext || tag, additionalData) -> plaintext | error`

Additional data (AD) is authenticated but not encrypted — useful for headers, sequence
numbers, or routing information that the network needs to read but must not be tampered
with.

The authentication tag is 128 bits. Truncating it is wrong — some protocols truncate to
96 bits "for efficiency," which reduces the forgery resistance from 2^-128 to 2^-96.
Don't do this.

### ChaCha20-Poly1305

ChaCha20-Poly1305 (RFC 8439) uses ChaCha20 as the stream cipher and Poly1305 as the
authenticator. The structure is identical to AES-GCM from the API perspective, but:

- No hardware requirement for constant-time operation — software-only ChaCha20 is
  inherently constant-time.
- 256-bit key only (AES-GCM supports 128 or 256 bits).
- Preferred over AES-GCM on ARM and MIPS processors without hardware AES (mobile,
  embedded, IoT).

XChaCha20-Poly1305 extends the nonce to 192 bits (24 bytes). The longer nonce makes
random nonce generation safe: the collision probability for 2^32 random 192-bit nonces
is negligible. This is useful when you cannot maintain a counter but must generate nonces
randomly. `golang.org/x/crypto/chacha20poly1305` provides `NewX`.

### HKDF (HMAC-based Key Derivation Function)

HKDF (RFC 5869) transforms a high-entropy input key material (IKM) into one or more
cryptographic keys. It has two stages:

1. **Extract**: `PRK = HMAC-Hash(salt, IKM)` — compresses arbitrary-length IKM into a
   fixed-length pseudorandom key.
2. **Expand**: `OKM = HKDF-Expand(PRK, info, length)` — produces output key material
   of any desired length, keyed on a domain-separation label (`info`).

TLS 1.3's key schedule is built entirely on HKDF. The `info` parameter provides domain
separation: deriving a "tls13 c hs traffic" key and a "tls13 s hs traffic" key from the
same PRK gives independent outputs even though the PRK is shared.

**Never use a raw DH shared secret as a symmetric key.** The output of ECDH is a group
element, not a uniformly random key. Always pass it through HKDF-Extract first.

### BLAKE3

BLAKE3 is a cryptographic hash function that is:
- Faster than SHA-256, MD5, and even SHA-1 in software on modern hardware.
- Parallelizable across CPU cores and SIMD lanes.
- Based on the Merkle tree structure, enabling incremental and streaming hashing.

Use BLAKE3 for checksums, content-addressable storage, and password-independent key
derivation (as a faster replacement for SHA-256 in non-adversarial contexts). Do not use
it as a standalone MAC without a key — for MAC, use HMAC-BLAKE3 or the keyed mode.

## Implementation: Go

```go
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// generateKey generates a cryptographically random key of the given length.
// The source is crypto/rand which reads from /dev/urandom on Linux and uses
// the CNG API on Windows. It is safe for cryptographic use.
func generateKey(length int) ([]byte, error) {
	key := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return key, nil
}

// sealAESGCM encrypts plaintext with AES-256-GCM and a random nonce.
// The nonce is prepended to the ciphertext so the receiver can extract it.
// additionalData is authenticated but not encrypted.
//
// SECURE: nonce is random 96-bit, freshly generated per message.
// The nonce is never reused because crypto/rand is used and the probability
// of collision for 2^32 messages under one key is approximately 1 in 2^32.
// Re-key before 2^32 messages.
func sealAESGCM(key, plaintext, additionalData []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes for standard GCM
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends to nonce, producing nonce || ciphertext || tag.
	ciphertext := gcm.Seal(nonce, nonce, plaintext, additionalData)
	return ciphertext, nil
}

// openAESGCM decrypts and authenticates a ciphertext produced by sealAESGCM.
// If the tag is invalid (tampered ciphertext or wrong key), returns an error
// before exposing any plaintext. This is the central guarantee of AEAD.
func openAESGCM(key, ciphertext, additionalData []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		// Do not wrap this error with ciphertext content or position information.
		// Detailed decryption errors can leak information about the plaintext.
		return nil, errors.New("decryption failed: authentication error")
	}
	return plaintext, nil
}

// VULNERABLE: counter nonce reuse — DO NOT DO THIS.
// A shared counter with no synchronization causes duplicate nonces under
// concurrent senders. Even with a mutex protecting the counter, if the same
// key is used across process restarts without persisting the counter, nonces repeat.
var vulnerableNonceCounter uint64 // WRONG

// sealChaCha20Poly1305 encrypts with XChaCha20-Poly1305 and a random 192-bit nonce.
// XChaCha20 uses 24-byte nonces, making random nonce generation safe even at scale:
// the birthday-bound collision probability for 2^32 random 192-bit nonces is 2^-128.
func sealChaCha20Poly1305(key, plaintext, additionalData []byte) ([]byte, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("key must be %d bytes, got %d", chacha20poly1305.KeySize, len(key))
	}

	aead, err := chacha20poly1305.NewX(key) // X = XChaCha20 (24-byte nonce)
	if err != nil {
		return nil, fmt.Errorf("new XChaCha20-Poly1305: %w", err)
	}

	nonce := make([]byte, aead.NonceSize()) // 24 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return aead.Seal(nonce, nonce, plaintext, additionalData), nil
}

func openChaCha20Poly1305(key, ciphertext, additionalData []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("new XChaCha20-Poly1305: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ct, additionalData)
	if err != nil {
		return nil, errors.New("decryption failed: authentication error")
	}
	return plaintext, nil
}

// deriveKeys uses HKDF-SHA256 to derive two independent keys from a single input
// key material. The info labels provide domain separation — the same IKM produces
// different output for different labels.
func deriveKeys(inputKeyMaterial, salt []byte) (encKey, macKey []byte, err error) {
	// Extract: HMAC-SHA256(salt, IKM) -> PRK
	// Expand: HKDF-Expand(PRK, info, length) -> OKM
	// golang.org/x/crypto/hkdf implements both as an io.Reader.

	h := hkdf.New(sha256.New, inputKeyMaterial, salt, []byte("myapp v1 encryption"))
	encKey = make([]byte, 32)
	if _, err := io.ReadFull(h, encKey); err != nil {
		return nil, nil, fmt.Errorf("derive enc key: %w", err)
	}

	h2 := hkdf.New(sha256.New, inputKeyMaterial, salt, []byte("myapp v1 authentication"))
	macKey = make([]byte, 32)
	if _, err := io.ReadFull(h2, macKey); err != nil {
		return nil, nil, fmt.Errorf("derive mac key: %w", err)
	}

	return encKey, macKey, nil
}

// selectPrimitive returns which AEAD to use based on deployment context.
// This encodes the decision logic that is often cargo-culted without justification.
func selectPrimitive(hasAESHardware bool) string {
	if hasAESHardware {
		// AES-NI instructions make AES-GCM faster and constant-time on x86-64.
		// Go's crypto/aes uses AES-NI automatically when available.
		return "AES-256-GCM"
	}
	// Without AES hardware (ARM without Cryptography Extension, MIPS, old x86),
	// AES in software has cache-timing side channels.
	// ChaCha20-Poly1305 is constant-time in software by design.
	return "XChaCha20-Poly1305"
}

func main() {
	key, err := generateKey(32) // 256-bit key
	if err != nil {
		log.Fatalf("generate key: %v", err)
	}

	plaintext := []byte("the quick brown fox jumps over the lazy dog")
	ad := []byte("sender-id:alice")

	// AES-GCM round trip
	ct, err := sealAESGCM(key, plaintext, ad)
	if err != nil {
		log.Fatalf("seal: %v", err)
	}
	fmt.Printf("AES-GCM ciphertext (%d bytes): %s\n", len(ct), hex.EncodeToString(ct[:32]))

	pt, err := openAESGCM(key, ct, ad)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	fmt.Printf("Decrypted: %s\n", pt)

	// Tamper detection
	ct[20] ^= 0xff
	_, err = openAESGCM(key, ct, ad)
	fmt.Printf("Tampered decryption error: %v\n", err)

	// XChaCha20-Poly1305 round trip
	xkey, _ := generateKey(32)
	xct, _ := sealChaCha20Poly1305(xkey, plaintext, ad)
	xpt, _ := openChaCha20Poly1305(xkey, xct, ad)
	fmt.Printf("XChaCha20: %s\n", xpt)

	// HKDF key derivation
	ikm, _ := generateKey(32)
	salt, _ := generateKey(16)
	encKey, macKey, _ := deriveKeys(ikm, salt)
	fmt.Printf("Derived enc key: %s\n", hex.EncodeToString(encKey))
	fmt.Printf("Derived mac key: %s\n", hex.EncodeToString(macKey))
}
```

### Go-specific considerations

**`crypto/cipher` vs `golang.org/x/crypto`.** The standard library provides AES-GCM via
`crypto/cipher.NewGCM`. ChaCha20-Poly1305 and HKDF live in `golang.org/x/crypto` — the
extended crypto library maintained by the Go team. Both are production-quality; `x/crypto`
modules are simply not stable enough for the stdlib's compatibility guarantee.

**AES-NI detection is automatic.** Go's `crypto/aes` checks for AES-NI at startup on
x86-64 and uses hardware-accelerated paths transparently. You do not need to select
implementations manually; `cipher.NewGCM` always uses the fastest available path.

**`crypto/rand.Reader` vs `math/rand`.** `crypto/rand.Reader` is the only acceptable
source of randomness for cryptographic nonces, keys, and salts. `math/rand` is predictable
and deterministic — never use it for security-sensitive values.

**Sealing order matters: nonce prepended before ciphertext.** The pattern
`gcm.Seal(nonce, nonce, plaintext, ad)` appends ciphertext to `nonce` as the dst, so
the output is `nonce || ciphertext || tag`. This is a common Go idiom because `Seal`
appends to its first argument. Document it in your code — readers who are not familiar
with the append semantics will be confused.

## Implementation: Rust

```rust
use ring::aead::{
    Aad, BoundKey, LessSafeKey, Nonce, NonceSequence, OpeningKey, SealingKey,
    UnboundKey, AES_256_GCM, CHACHA20_POLY1305,
};
use ring::error::Unspecified;
use ring::hkdf;
use ring::rand::{SecureRandom, SystemRandom};
use std::convert::TryInto;

/// A nonce sequence that generates random nonces from a secure RNG.
/// ring requires a NonceSequence to manage nonce state, enforcing that callers
/// cannot accidentally reuse nonces.
struct RandomNonce {
    rng: SystemRandom,
}

impl RandomNonce {
    fn new() -> Self {
        Self { rng: SystemRandom::new() }
    }
}

impl NonceSequence for RandomNonce {
    fn advance(&mut self) -> Result<Nonce, Unspecified> {
        let mut nonce_bytes = [0u8; ring::aead::NONCE_LEN];
        self.rng.fill(&mut nonce_bytes)?;
        Ok(Nonce::assume_unique_for_key(nonce_bytes))
    }
}

/// Encrypt with AES-256-GCM. Returns nonce || ciphertext || tag.
///
/// ring's API forces the caller to use a NonceSequence — you cannot pass
/// a raw nonce to SealingKey::seal_in_place. This structural constraint makes
/// nonce reuse harder to introduce accidentally.
fn seal_aes_gcm(key_bytes: &[u8; 32], plaintext: &[u8], ad: &[u8]) -> Result<Vec<u8>, Unspecified> {
    let unbound = UnboundKey::new(&AES_256_GCM, key_bytes)?;
    let mut sealing_key = SealingKey::new(unbound, RandomNonce::new());

    let mut nonce_bytes = [0u8; ring::aead::NONCE_LEN];
    let rng = SystemRandom::new();
    rng.fill(&mut nonce_bytes)?;

    // Prepend nonce to output buffer so recipient can extract it.
    let mut output = Vec::with_capacity(nonce_bytes.len() + plaintext.len() + AES_256_GCM.tag_len());
    output.extend_from_slice(&nonce_bytes);
    output.extend_from_slice(plaintext);

    let nonce = Nonce::assume_unique_for_key(nonce_bytes);
    // seal_in_place_append_tag appends the authentication tag in-place.
    sealing_key.seal_in_place_separate_tag(nonce, Aad::from(ad), &mut output[nonce_bytes.len()..])?;

    Ok(output)
}

/// Encrypt using ring's LessSafeKey which takes an explicit nonce.
/// "LessSafe" because the caller is responsible for nonce uniqueness.
/// Use SealingKey+NonceSequence in production; this is for illustration.
fn seal_aes_gcm_explicit_nonce(
    key_bytes: &[u8; 32],
    nonce_bytes: [u8; 12],
    plaintext: &[u8],
    ad: &[u8],
) -> Result<Vec<u8>, Unspecified> {
    let unbound = UnboundKey::new(&AES_256_GCM, key_bytes)?;
    let key = LessSafeKey::new(unbound);

    let mut in_out = plaintext.to_vec();
    let nonce = Nonce::assume_unique_for_key(nonce_bytes);
    // seal_in_place_append_tag extends in_out with the 16-byte tag.
    key.seal_in_place_append_tag(nonce, Aad::from(ad), &mut in_out)?;

    Ok(in_out)
}

/// Decrypt and authenticate an AES-256-GCM ciphertext (nonce || ciphertext || tag).
/// Returns an error if the tag is invalid — no plaintext is exposed on failure.
fn open_aes_gcm(key_bytes: &[u8; 32], ciphertext: &[u8], ad: &[u8]) -> Result<Vec<u8>, Unspecified> {
    if ciphertext.len() < ring::aead::NONCE_LEN + AES_256_GCM.tag_len() {
        return Err(Unspecified);
    }

    let nonce_bytes: [u8; ring::aead::NONCE_LEN] = ciphertext[..ring::aead::NONCE_LEN]
        .try_into()
        .map_err(|_| Unspecified)?;
    let mut ct = ciphertext[ring::aead::NONCE_LEN..].to_vec();

    let unbound = UnboundKey::new(&AES_256_GCM, key_bytes)?;
    let mut opening_key = OpeningKey::new(unbound, SingleNonce(nonce_bytes));
    let plaintext = opening_key.open_in_place(Aad::from(ad), &mut ct)?;
    Ok(plaintext.to_vec())
}

/// A NonceSequence that returns exactly one nonce (for decryption where the nonce
/// is embedded in the ciphertext and must be used exactly once).
struct SingleNonce([u8; ring::aead::NONCE_LEN]);
impl NonceSequence for SingleNonce {
    fn advance(&mut self) -> Result<Nonce, Unspecified> {
        Ok(Nonce::assume_unique_for_key(self.0))
    }
}

/// Derive a key from input key material using HKDF-SHA256.
/// ring's HKDF API separates Extract from Expand and uses type-level encoding
/// of the algorithm, preventing mixing incompatible algorithms at runtime.
fn derive_key(ikm: &[u8], salt: &[u8], info: &[u8]) -> Result<[u8; 32], Unspecified> {
    let salt = hkdf::Salt::new(hkdf::HKDF_SHA256, salt);
    let prk = salt.extract(ikm);

    let mut okm = [0u8; 32];
    prk.expand(&[info], &hkdf::HKDF_SHA256)
        .and_then(|okm_key| okm_key.fill(&mut okm))
        .map_err(|_| Unspecified)?;
    Ok(okm)
}

fn main() {
    let rng = SystemRandom::new();
    let mut key_bytes = [0u8; 32];
    rng.fill(&mut key_bytes).expect("generate key");

    let plaintext = b"the quick brown fox jumps over the lazy dog";
    let ad = b"sender-id:alice";

    // AES-256-GCM explicit nonce example
    let mut nonce = [0u8; 12];
    rng.fill(&mut nonce).expect("generate nonce");

    let ct = seal_aes_gcm_explicit_nonce(&key_bytes, nonce, plaintext, ad)
        .expect("seal");
    println!("AES-256-GCM ciphertext length: {} bytes", ct.len());

    // Tamper detection
    let mut tampered = ct.clone();
    tampered[10] ^= 0xff;
    let result = open_aes_gcm(&key_bytes, &tampered, ad);
    println!("Tampered decryption: {:?}", result.is_err()); // true

    // HKDF key derivation
    let mut ikm = [0u8; 32];
    rng.fill(&mut ikm).expect("generate IKM");
    let derived = derive_key(&ikm, b"application-salt", b"session-key v1").expect("derive");
    println!("Derived key (first 8 bytes): {:02x?}", &derived[..8]);
}
```

### Rust-specific considerations

**`ring` vs `RustCrypto` crates.** `ring` is a single-purpose, audited library with
minimal dependencies. It has an opinionated API that makes some insecure patterns
structurally impossible (e.g., `NonceSequence`). `RustCrypto` (`aes-gcm`, `chacha20poly1305`
crates) provides more flexible, composable primitives following the `AeadInPlace` trait
pattern. For TLS and QUIC, `ring` is the correct default because `rustls` and `quinn`
require it. For application-level encryption, either works; `ring` has a stronger
security audit history.

**Constant-time guarantees.** Both `ring` and `RustCrypto` implement AES-GCM in
constant time when AES-NI is available. On software-only paths, `ring` uses Montgomery
field arithmetic that avoids secret-dependent table lookups. ChaCha20 is constant-time
by construction in all implementations.

**`LessSafeKey` naming.** `ring`'s `LessSafeKey` is not unsafe in the Rust sense — it
just requires the caller to guarantee nonce uniqueness manually rather than through the
type system. Use `SealingKey` + `NonceSequence` in production code for the type-system
enforcement.

**Error types.** `ring::error::Unspecified` is intentionally opaque — it does not
distinguish "wrong key," "wrong nonce," "tampered ciphertext," or "incorrect additional
data." This prevents oracle attacks where an attacker distinguishes error types to
extract information. Do not add specificity to decryption errors at the application level.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| AES-GCM | `crypto/cipher.NewGCM` (stdlib) | `ring::aead::AES_256_GCM` |
| ChaCha20-Poly1305 | `golang.org/x/crypto/chacha20poly1305` | `ring::aead::CHACHA20_POLY1305` |
| HKDF | `golang.org/x/crypto/hkdf` | `ring::hkdf` |
| BLAKE3 | `github.com/zeebo/blake3` (third-party) | `blake3` crate |
| Nonce management | Manual (caller fills nonce slice) | `NonceSequence` trait (type-enforced) |
| Nonce reuse prevention | Convention only | Type system + `NonceSequence` |
| AES-NI detection | Automatic in stdlib | Automatic in `ring` |
| Constant-time | Yes (AES-NI path) | Yes (AES-NI + ring's software path) |
| Error opacity | Wrapped errors with some context | `Unspecified` (intentionally opaque) |
| FIPS-validated primitives | BoringCrypto integration | `aws-lc-rs` backend for `rustls` |

## Production War Stories

**Nonce reuse in WPA2 (KRACK, 2017).** The WPA2 4-way handshake allowed an attacker to
cause a client to reinstall the session key, resetting the nonce counter. Since AES-CCMP
(used in WPA2) uses a counter nonce, resetting it caused nonce reuse. The attack allowed
decryption and, in some configurations, injection of forged traffic. The root cause: the
key reinstallation was a valid protocol state transition that had a catastrophic side
effect on nonce discipline.

**GCM Nonce Reuse in TLS Reconnect (2016).** Bhargavan and Leurent demonstrated that
if a TLS 1.2 server reused a GCM nonce when resuming a session, the GCM authentication
tag became forgeable. The nonce reuse happened because some implementations initialized
the IV counter to 0 on session resumption rather than generating a fresh random IV.

**Signal Protocol's choice of ChaCha20-Poly1305.** Signal switched from AES-CBC + HMAC
to ChaCha20-Poly1305 in 2016. The reason was not performance but platform consistency:
ChaCha20 is constant-time in pure software, while AES-CBC in software (without AES-NI)
had timing sidechannels on some Android devices. The lesson: "the algorithm is secure"
is not sufficient — the *implementation* must also be secure on the target hardware.

**AWS S3 AEAD downgrade (2020).** AWS S3 client-side encryption SDK v1 used AES-CBC
(not AEAD) for data key encryption. An attacker who could perform chosen-ciphertext
queries to the S3 bucket could recover the data key through a padding oracle. The v2 SDK
mandates AES-GCM. The lesson: CBC encryption without authentication is broken in
interactive settings.

## Security Analysis

**Nonce domain analysis.** Before using an AEAD primitive in a new system, answer: (a)
who generates nonces? (b) are multiple senders sharing a key? (c) can the nonce counter
reset (process restart, failover)? If multiple senders share a key (e.g., distributed
encryption service), use a nonce scheme that partitions the nonce space (e.g., sender ID
prefix + local counter), or use random nonces with XChaCha20-Poly1305.

**Key commitment.** Standard AES-GCM does not provide key commitment: given a ciphertext,
it is possible to find two different keys that both "successfully decrypt" to different
plaintexts (with different tags). This matters in protocols where parties may disagree
about which key was used. The fix is HMAC over the key and ciphertext before returning
plaintext, or use a construction like AES-GCM-SIV which provides key commitment.

**AEAD misuse: encrypting without associated data when you should.** If your protocol
allows an attacker to reorder, duplicate, or substitute ciphertext blocks from different
messages (e.g., packet reordering in UDP), you must include a sequence number in the
associated data. Without it, an attacker can replay block N from session 1 as block N
from session 2. TLS does this correctly; many bespoke protocols do not.

## Common Pitfalls

1. **Reusing nonces with AES-GCM.** The most common mistake in cryptographic code.
   Using a counter nonce that resets on process restart, sharing a key+counter across
   multiple senders, or failing to synchronize the counter under concurrent access all
   cause nonce reuse. Use random nonces with XChaCha20-Poly1305 if you cannot guarantee
   counter uniqueness.

2. **Using raw ECDH output as a symmetric key.** The output of X25519 or P-256 ECDH is a
   curve point, not a uniformly random key. Always run it through HKDF-Extract before
   using it as a key for AES-GCM or ChaCha20.

3. **Truncating authentication tags.** Some protocols truncate GCM tags to 12 or 8 bytes
   "for efficiency." Each byte removed halves the forgery resistance. At 8 bytes, an
   attacker can forge with 2^-64 probability — marginal at scale. Use the full 16-byte tag.

4. **Encrypting the nonce.** Some developers encrypt the nonce along with the plaintext,
   thinking this hides nonce reuse. It does not. GCM's security proof requires the nonce
   to be distinct; it says nothing about whether the nonce is public. Encrypt-the-nonce
   schemes are not standardized and have subtle security issues.

5. **Forgetting to authenticate associated data consistently.** If `additionalData` is
   optional in your API and callers sometimes pass it and sometimes do not, you have a
   framing ambiguity. A ciphertext sealed with `ad="sender:alice"` will fail to open
   with `ad=""`. This is correct behavior, but if callers are inconsistent, they will
   "fix" the issue by disabling associated data — losing its security benefit entirely.

## Exercises

**Exercise 1** (30 min): Write a Go program that demonstrates nonce reuse catastrophe.
Encrypt two different plaintexts with the same key and nonce using AES-GCM. Print the
XOR of the two ciphertexts and show that you can recover partial plaintext knowing one
of the original messages. This exercise should make the nonce reuse rule visceral.

**Exercise 2** (2–4h): Implement a simple file encryption tool in Go that uses
XChaCha20-Poly1305. The tool should: (a) derive an encryption key from a passphrase
using HKDF with a random salt, (b) encrypt the file with a random nonce, (c) prepend
`salt || nonce` to the ciphertext, (d) verify the authentication tag before writing any
decrypted bytes to disk. Handle the case where the output file is the same as the input
(use a temp file + atomic rename).

**Exercise 3** (4–8h): Implement the same file encryption tool in Rust using `ring`.
Use `ring::aead::SealingKey` with a custom `NonceSequence` that maintains a per-file
counter. Benchmark the Go and Rust implementations on a 1GB file. Profile with `perf`
to identify whether the bottleneck is the AEAD cipher, the key derivation, or I/O.

**Exercise 4** (8–15h): Design and implement a multi-sender encrypted message bus. Each
sender has a unique 256-bit key, but all keys are derived from a single master secret
via HKDF with per-sender info labels. Each message includes a sender ID in the associated
data. The receiver maintains a per-sender nonce counter and rejects out-of-order or
replayed messages. Implement in Go with the constraint that the system must continue
working after any sender process restarts (persist nonce state to disk, fsync before
send). Write a test that verifies replay rejection, nonce counter persistence, and
recovery from a simulated crash.

## Further Reading

### Foundational Papers

- Bellare and Namprempre — "Authenticated Encryption: Relations among notions and
  analysis of the generic composition paradigm" (ASIACRYPT 2000). Defines MAC-then-Encrypt
  vs Encrypt-then-MAC and proves why the latter is strictly stronger.
- McGrew and Viega — "The Galois/Counter Mode of Operation" (2004). The original GCM
  specification with the security proof.
- Bernstein — "ChaCha, a variant of Salsa20" (2008). The stream cipher underlying
  ChaCha20-Poly1305, with the design rationale for avoiding table lookups.
- Bellare and Rogaway — "Encode-Then-Encipher Encryption" (2000). Provides the
  theoretical background for authenticated encryption modes.

### Books

- Dan Boneh, Victor Shoup — "A Graduate Course in Applied Cryptography" (freely
  available). Chapter 9 covers authenticated encryption; chapter 6 covers pseudorandom
  functions and block ciphers.
- Jean-Philippe Aumasson — "Serious Cryptography" (No Starch Press, 2017). Chapters 4
  (block ciphers), 5 (stream ciphers), and 7 (authenticated encryption) are directly
  applicable.

### Production Code to Read

- `golang.org/x/crypto/chacha20poly1305` — particularly `chacha20poly1305.go` and the
  assembly in `chacha20_amd64.s`. See how AES-NI detection and dispatch works.
- `ring/src/aead/` — the Rust source for ring's AEAD implementations. Study how the
  `NonceSequence` trait is used to enforce nonce discipline at the type level.
- WireGuard protocol (`wireguard.com/papers/wireguard.pdf`) — uses ChaCha20-Poly1305 for
  all data encryption, with a counter nonce derived from a session state machine that
  explicitly prevents reuse.

### Security Advisories / CVEs to Study

- CVE-2016-0701 (OpenSSL DH small subgroup) — reusing DH keys without HKDF allowed
  subgroup confinement attacks.
- CVE-2019-14869 (BoringSSL GCM nonce) — nonce counter not properly initialized
  in certain TLS session resumption paths.
- KRACK (2017, no CVE assigned to GCM specifically) — the WPA2 nonce reuse attack;
  studied at wpa.live/krack-details.
