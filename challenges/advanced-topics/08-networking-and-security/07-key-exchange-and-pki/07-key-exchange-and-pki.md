<!--
type: reference
difficulty: advanced
section: [08-networking-and-security]
concepts: [x25519-ecdh, ed25519-signatures, x509-chain-validation, ocsp-stapling, certificate-transparency, forward-secrecy, curve25519]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [cryptographic-primitives, tls-internals, elliptic-curve-basics]
papers: [Bernstein 2006 — Curve25519 New Diffie-Hellman Speed Records, Bernstein et al. 2011 — High-Speed High-Security Signatures, Laurie et al. 2013 — Certificate Transparency RFC 6962]
industry_use: [let's-encrypt, signal-protocol, wireguard, age-encryption, ssh-keys, cloudflare-origin-ca]
language_contrast: high
-->

# Key Exchange and PKI

> Curve25519 was designed to resist implementation errors: its cofactor, constant-time
> scalar multiplication, and twist-secure properties mean that nearly all implementation
> mistakes produce a safe result rather than a catastrophic vulnerability.

## Mental Model

Key exchange and PKI solve two orthogonal problems. **Key exchange** (ECDH) establishes a
shared secret between two parties who have never communicated, using only public keys.
Neither party reveals their private key; the shared secret is derived from the interaction
of one party's private key with the other's public key. **PKI** (Public Key Infrastructure)
solves the question of *trust*: how do you know that the public key you received belongs
to the server you intended to contact, not a man-in-the-middle?

The choice of elliptic curve matters enormously. The NIST P-256 and P-384 curves are
widely deployed and standardized, but they have design parameters chosen by NIST with
values ("nothing-up-my-sleeve numbers") whose origin has never been fully explained. By
contrast, Curve25519 was designed by Bernstein with a fully transparent design rationale:
the prime 2^255 - 19 was chosen for efficient 64-bit arithmetic; the cofactor of 8 was
chosen for safety against small-subgroup attacks; the constant-time multiplication formula
was a design goal from the start. Curve25519 is now preferred for new systems (Signal,
WireGuard, SSH, age encryption), while P-256 remains dominant for TLS due to legacy
browser support.

X.509 certificate chain validation is where PKI meets practice. The chain is:
root CA → intermediate CA → leaf certificate. The root is self-signed and trusted by the
OS or browser. The intermediate is signed by the root. The leaf is signed by the
intermediate. A verifier must: (a) verify each signature in the chain, (b) check validity
periods, (c) check key usage extensions (a CA cert must have the CA:TRUE constraint), (d)
check revocation status. Step (d) is where most real-world complexity lives.

OCSP (Online Certificate Status Protocol) is the "is this certificate revoked?" query.
OCSP stapling moves this query from the client to the server: the server fetches a signed
OCSP response from the CA and attaches it to the TLS handshake. The client gets up-to-
date revocation status without making an extra network request. OCSP Must-Staple is an
X.509 extension that tells clients to reject the certificate if no OCSP staple is present.

Certificate Transparency (CT) is a public audit log of all issued certificates. Every
certificate issued by a CT-compliant CA is logged in an append-only Merkle tree. Browsers
require CT proofs (Signed Certificate Timestamps) as evidence the certificate is logged.
This prevents CAs from issuing certificates secretly — any misissuance becomes publicly
auditable.

## Core Concepts

### X25519 Key Exchange

X25519 is ECDH on Curve25519. The "X" denotes the use of the Montgomery ladder — a
constant-time scalar multiplication algorithm that is robust to implementation errors
(any 32-byte scalar produces a valid output, including the zero point, without crashing).

Shared secret derivation:
1. Alice generates private key `a` (32 random bytes), computes public key `A = a * G`.
2. Bob generates private key `b`, computes public key `B = b * G`.
3. Alice computes `shared = a * B = a * b * G`.
4. Bob computes `shared = b * A = b * a * G`.
5. Both arrive at the same group element; use HKDF to derive a symmetric key.

**Never use the raw X25519 output as a key.** The output is a field element (a 255-bit
integer mod 2^255 - 19), not a uniformly random value. Always process with HKDF.

### Ed25519 Signatures

Ed25519 uses a different group encoding (Twisted Edwards curve) but the same underlying
field as X25519. Properties:

- **Deterministic**: signatures are deterministic (no random nonce). This eliminates the
  "static nonce breaks everything" class of bugs (PS3, ECDSA with weak RNG).
- **Batch verification**: 64 signatures can be verified simultaneously using multi-scalar
  multiplication, approximately 2x faster than sequential verification.
- **64-byte signatures**: compact compared to RSA-2048 (256 bytes) or ECDSA-P256 (64
  bytes, same size).

**Why Ed25519 over ECDSA?** ECDSA requires a random `k` per signature. If `k` is
repeated or predictable, the private key is recoverable. Ed25519 derives `k`
deterministically from the message and private key via SHA-512, eliminating RNG dependency.

### X.509 Certificate Chain Validation

RFC 5280 defines the validation algorithm. The key steps beyond signature verification:

1. **Basic constraints**: the CA flag (`cA: TRUE`) must be set in certificates that sign
   other certificates. A leaf certificate with `cA: FALSE` must not be trusted as a CA.
   Failing to check this allows "leaf certificate as intermediate CA" attacks.

2. **Extended Key Usage (EKU)**: leaf certificates should have `id-kp-serverAuth` for TLS
   servers. A certificate issued for code signing should not be usable for TLS server
   authentication. Always enforce EKU.

3. **Subject Alternative Names (SAN)**: hostname matching must use the SAN extension, not
   the Subject CN (deprecated in RFC 2818). Wildcards (`*.example.com`) match exactly one
   label; `*.example.com` does not match `sub.sub.example.com`.

4. **Name constraints**: intermediate CA certificates can include name constraints that
   restrict which domains they may issue certificates for. A constrained CA that issues
   outside its permitted subtree is a policy violation.

## Implementation: Go

```go
package main

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"time"

	"golang.org/x/crypto/hkdf"
)

// x25519KeyExchange demonstrates ECDH with X25519.
// Returns the derived symmetric key after HKDF processing.
func x25519KeyExchange() (aliceKey, bobKey []byte, err error) {
	// Generate Alice's key pair
	alicePriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("alice keygen: %w", err)
	}
	alicePub := alicePriv.PublicKey()

	// Generate Bob's key pair
	bobPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("bob keygen: %w", err)
	}
	bobPub := bobPriv.PublicKey()

	// ECDH: both sides compute the same shared secret
	aliceShared, err := alicePriv.ECDH(bobPub)
	if err != nil {
		return nil, nil, fmt.Errorf("alice ECDH: %w", err)
	}
	bobShared, err := bobPriv.ECDH(alicePub)
	if err != nil {
		return nil, nil, fmt.Errorf("bob ECDH: %w", err)
	}

	// Sanity: raw shared secrets must be equal
	if string(aliceShared) != string(bobShared) {
		return nil, nil, fmt.Errorf("ECDH mismatch — this should be impossible")
	}

	// CRITICAL: never use the raw X25519 output as a symmetric key.
	// Process through HKDF with a context label that includes both public keys
	// to provide binding to the session participants.
	deriveKey := func(shared, localPub, remotePub []byte) ([]byte, error) {
		// Salt = hash of both public keys concatenated (unique per session, public)
		saltData := append(localPub, remotePub...)
		saltHash := sha256.Sum256(saltData)

		h := hkdf.New(sha256.New, shared, saltHash[:], []byte("x25519 session key v1"))
		key := make([]byte, 32)
		if _, err := io.ReadFull(h, key); err != nil {
			return nil, fmt.Errorf("HKDF expand: %w", err)
		}
		return key, nil
	}

	alicePubBytes := alicePub.Bytes()
	bobPubBytes := bobPub.Bytes()

	aliceKey, err = deriveKey(aliceShared, alicePubBytes, bobPubBytes)
	if err != nil {
		return nil, nil, err
	}
	bobKey, err = deriveKey(bobShared, bobPubBytes, alicePubBytes)
	if err != nil {
		return nil, nil, err
	}

	// Note: aliceKey != bobKey because the salt input order differs.
	// In a real protocol, both sides must agree on the input order
	// (e.g., lower public key first, or initiator-first convention).
	// The keys derive the same material if both sides use the same ordering.

	return aliceKey, bobKey, nil
}

// ed25519Example demonstrates Ed25519 key generation, signing, and verification.
func ed25519Example() error {
	// Generate a key pair. Ed25519 public keys are 32 bytes; private keys are 64 bytes
	// (actually: seed || public key in Go's representation).
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	message := []byte("the content to authenticate")

	// Sign: deterministic — same message + same private key = same signature every time.
	// This is a feature, not a bug: eliminates RNG-dependency in signatures.
	sig := ed25519.Sign(priv, message)
	fmt.Printf("Ed25519 signature (%d bytes): %x...\n", len(sig), sig[:8])

	// Verify
	if !ed25519.Verify(pub, message, sig) {
		return fmt.Errorf("signature verification failed")
	}
	fmt.Println("Ed25519 signature valid")

	// Verify that modifying the message invalidates the signature
	modified := append([]byte{}, message...)
	modified[0] ^= 0xFF
	if ed25519.Verify(pub, modified, sig) {
		return fmt.Errorf("modified message verified — this should be impossible")
	}
	fmt.Println("Modified message correctly rejected")

	return nil
}

// generateSelfSignedCert generates a self-signed certificate for testing.
// Production certificates require a CA signature (Let's Encrypt, internal PKI).
func generateSelfSignedCert(hosts []string) (certPEM, keyPEM []byte, err error) {
	// Ed25519 keys for the certificate
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: hosts[0]},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),

		// Set SAN, not just CN. Hostname validation uses SAN.
		DNSNames: hosts,

		// Key usage: digital signature for TLS (not CA usage).
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageTLSClient, x509.ExtKeyUsageServerAuth},

		// BasicConstraints: CA:FALSE — this cert cannot sign other certificates.
		// Failing to set IsCA:false allows leaf-as-CA attacks if chain validation is weak.
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	// Self-signed: signed with its own private key
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("create cert: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

	return certPEM, keyPEM, nil
}

// validateCertChain validates a certificate chain against a trusted root pool.
// This is what `tls.Config.VerifyPeerCertificate` runs under the hood.
func validateCertChain(chain []*x509.Certificate, roots *x509.CertPool, serverName string) error {
	if len(chain) == 0 {
		return fmt.Errorf("empty certificate chain")
	}

	leaf := chain[0]
	intermediates := x509.NewCertPool()
	for _, c := range chain[1:] {
		intermediates.AddCert(c)
	}

	opts := x509.VerifyOptions{
		DNSName:       serverName,
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		// KeyUsages: require TLS server auth
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if _, err := leaf.Verify(opts); err != nil {
		return fmt.Errorf("chain verification failed: %w", err)
	}
	return nil
}

func main() {
	// X25519 key exchange
	aliceKey, bobKey, err := x25519KeyExchange()
	if err != nil {
		panic(err)
	}
	fmt.Printf("X25519 Alice derived key: %x...\n", aliceKey[:8])
	fmt.Printf("X25519 Bob derived key:   %x...\n", bobKey[:8])

	// Ed25519
	if err := ed25519Example(); err != nil {
		panic(err)
	}

	// Self-signed cert
	certPEM, _, err := generateSelfSignedCert([]string{"localhost", "example.local"})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Generated cert PEM (%d bytes)\n", len(certPEM))
}
```

### Go-specific considerations

**`crypto/ecdh` was added in Go 1.20.** Before 1.20, X25519 required `golang.org/x/crypto/
curve25519`. The new `crypto/ecdh` package provides a clean, type-safe API: `ecdh.X25519()`,
`ecdh.P256()`, etc. Use `crypto/ecdh` for all new code.

**`ed25519.Sign` is in `crypto/ed25519` (Go stdlib).** No external dependency needed.
The `PrivateKey` type is 64 bytes (seed + public key) which differs from OpenSSL's
convention (32-byte seed only). Be careful when interoperating with other implementations.

**`crypto/x509` for certificate operations.** Go's `crypto/x509` package handles DER/PEM
encoding, chain construction, and validation. The `x509.VerifyOptions.KeyUsages` field
enforces EKU checking — always set this to `ExtKeyUsageServerAuth` for TLS server
validation.

**Certificate pooling.** `x509.CertPool` is the correct abstraction for trust stores.
`x509.SystemCertPool()` loads the OS trust store. For pinned CAs (internal PKI), create
an empty pool and add only your CA certificates — this prevents "trusted by any public
CA" scenarios.

## Implementation: Rust

```rust
use ring::agreement::{self, EphemeralPrivateKey, UnparsedPublicKey};
use ring::rand::SystemRandom;
use ring::signature::{self, Ed25519KeyPair, KeyPair, Signature, UnparsedPublicKey as SigUnparsedPublicKey};
use ring::hkdf::{self, HKDF_SHA256};

/// X25519 key exchange using ring.
/// ring enforces that private keys are ephemeral (consumed after use), preventing reuse.
fn x25519_exchange() -> Result<Vec<u8>, ring::error::Unspecified> {
    let rng = SystemRandom::new();

    // Alice's ephemeral key pair
    let alice_priv = EphemeralPrivateKey::generate(&agreement::X25519, &rng)?;
    let alice_pub_bytes = alice_priv.compute_public_key()?;

    // Bob's ephemeral key pair
    let bob_priv = EphemeralPrivateKey::generate(&agreement::X25519, &rng)?;
    let bob_pub_bytes = bob_priv.compute_public_key()?;

    // Alice computes the shared secret using Bob's public key.
    // ring's `agree_ephemeral` consumes the private key, preventing reuse.
    let alice_pub_for_bob = UnparsedPublicKey::new(&agreement::X25519, bob_pub_bytes.as_ref());
    let shared_key = agreement::agree_ephemeral(
        alice_priv,
        &alice_pub_for_bob,
        |shared_secret| {
            // Derive a symmetric key from the shared secret using HKDF.
            // The info parameter binds this key to a specific protocol context.
            let prk = hkdf::Salt::new(HKDF_SHA256, b"x25519-exchange-salt")
                .extract(shared_secret);
            let mut key = vec![0u8; 32];
            prk.expand(&[b"session-key v1"], &HKDF_SHA256)
                .and_then(|okm| okm.fill(&mut key))
                .map_err(|_| ring::error::Unspecified)?;
            Ok(key)
        },
    )?;

    // Note: bob_priv is still available here (not yet consumed).
    // In a real protocol, bob would also call agree_ephemeral to get the same shared secret.
    // Here we show Alice's side only for brevity.

    Ok(shared_key)
}

/// Ed25519 sign and verify using ring.
fn ed25519_example() -> Result<(), ring::error::Unspecified> {
    let rng = SystemRandom::new();

    // Generate an Ed25519 key pair from a random seed (PKCS#8 format).
    let pkcs8_bytes = Ed25519KeyPair::generate_pkcs8(&rng)?;
    let key_pair = Ed25519KeyPair::from_pkcs8(pkcs8_bytes.as_ref())?;

    let message = b"the content to authenticate";

    // Sign: deterministic, no RNG required.
    let sig: Signature = key_pair.sign(message);
    println!("Ed25519 signature: {} bytes", sig.as_ref().len());

    // Verify using the public key component.
    let public_key = SigUnparsedPublicKey::new(
        &signature::ED25519,
        key_pair.public_key().as_ref(),
    );
    public_key.verify(message, sig.as_ref())?;
    println!("Ed25519 signature valid");

    // Verify that modifying the message invalidates the signature
    let mut modified = message.to_vec();
    modified[0] ^= 0xFF;
    match public_key.verify(&modified, sig.as_ref()) {
        Ok(_) => panic!("modified message verified — should be impossible"),
        Err(_) => println!("Modified message correctly rejected"),
    }

    Ok(())
}

/// Demonstrate X25519 using the lower-level ring API to show both sides.
fn full_x25519_exchange() -> Result<(), ring::error::Unspecified> {
    let rng = SystemRandom::new();

    let alice_priv = EphemeralPrivateKey::generate(&agreement::X25519, &rng)?;
    let alice_pub = alice_priv.compute_public_key()?;
    let alice_pub_bytes = alice_pub.as_ref().to_vec();

    let bob_priv = EphemeralPrivateKey::generate(&agreement::X25519, &rng)?;
    let bob_pub = bob_priv.compute_public_key()?;
    let bob_pub_bytes = bob_pub.as_ref().to_vec();

    // Alice derives the shared key using Bob's public key
    let alice_key = agreement::agree_ephemeral(
        alice_priv,
        &UnparsedPublicKey::new(&agreement::X25519, &bob_pub_bytes),
        |ss| derive_from_shared(ss, &alice_pub_bytes, &bob_pub_bytes),
    )?;

    // Bob derives the shared key using Alice's public key
    let bob_key = agreement::agree_ephemeral(
        bob_priv,
        &UnparsedPublicKey::new(&agreement::X25519, &alice_pub_bytes),
        |ss| derive_from_shared(ss, &alice_pub_bytes, &bob_pub_bytes),
    )?;

    // Both must derive the same key (same ordering of pub keys in both calls)
    assert_eq!(alice_key, bob_key, "key exchange mismatch");
    println!("X25519 shared key (first 8 bytes): {:02x?}", &alice_key[..8]);

    Ok(())
}

fn derive_from_shared(shared: &[u8], pub_a: &[u8], pub_b: &[u8]) -> Result<Vec<u8>, ring::error::Unspecified> {
    // Deterministic salt: sort the public keys so both sides get the same salt
    // regardless of who is "Alice" and who is "Bob".
    let (first, second) = if pub_a < pub_b { (pub_a, pub_b) } else { (pub_b, pub_a) };
    let mut salt_input = first.to_vec();
    salt_input.extend_from_slice(second);

    let salt_hash = ring::digest::digest(&ring::digest::SHA256, &salt_input);
    let prk = hkdf::Salt::new(HKDF_SHA256, salt_hash.as_ref()).extract(shared);

    let mut key = vec![0u8; 32];
    prk.expand(&[b"x25519 session key v1"], &HKDF_SHA256)
        .and_then(|okm| okm.fill(&mut key))
        .map_err(|_| ring::error::Unspecified)?;
    Ok(key)
}

fn main() {
    // X25519
    full_x25519_exchange().expect("X25519 exchange");

    // Ed25519
    ed25519_example().expect("Ed25519 example");
}
```

### Rust-specific considerations

**`ring::agreement::agree_ephemeral` consumes the private key.** This is a type-system
enforcement of the "ephemeral" property — you cannot accidentally reuse an ephemeral
private key because the API takes it by value. This prevents "static ECDH" mistakes
where a private key is reused across sessions, destroying forward secrecy.

**`ring` vs `x25519-dalek`.** `ring` bundles both X25519 and Ed25519 with a single
audited C/assembly backend. `x25519-dalek` and `ed25519-dalek` are pure-Rust
implementations from the dalek-cryptography team. Both are production-quality. `ring`
is preferred when FIPS matters (aws-lc-rs backend) or when used alongside `rustls`.
`dalek` is preferred for embedded/no_std targets.

**`rustls` + `webpki` for certificate validation.** `rustls` uses the `webpki` crate
(and its successor `rustls-webpki`) for X.509 certificate chain validation. This is a
clean-room implementation of RFC 5280 in Rust, with no C dependencies. It is stricter
than some OpenSSL-based validators: it requires valid UTF-8 in domain names and rejects
many malformed certificates that OpenSSL accepts. This strictness is a feature.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| X25519 | `crypto/ecdh.X25519()` (stdlib, Go 1.20+) | `ring::agreement::X25519` |
| Ed25519 | `crypto/ed25519` (stdlib) | `ring::signature::ED25519` |
| ECDSA P-256 | `crypto/ecdsa` + `crypto/elliptic.P256()` | `ring::signature::ECDSA_P256_SHA256_*` |
| RSA | `crypto/rsa` (stdlib) | `ring::rsa` (limited) or `rsa` crate |
| X.509 validation | `crypto/x509.Verify()` | `rustls-webpki` |
| Certificate parsing | `crypto/x509.ParseCertificate()` | `x509-parser` or `der-parser` crates |
| OCSP | `golang.org/x/crypto/ocsp` | No stdlib — use `pki-types` + custom |
| CSR generation | `crypto/x509.CreateCertificateRequest()` | `rcgen` crate |
| Ephemeral enforcement | Manual convention | Type system (consumed by `agree_ephemeral`) |
| FIPS-validated | BoringCrypto integration | `aws-lc-rs` backend |

## Production War Stories

**Let's Encrypt and the X.509 BasicConstraints bug (2022).** Let's Encrypt discovered
that their OCSP responder was issuing OCSP responses that contained incorrectly populated
certificate extensions. The bug had been present for 6 years and affected ~28 million
active certificates. They revoked all affected certificates within 24 hours (OCSP Must-
Staple was not involved, which is why they could revoke without causing browser warnings).
The lesson: OCSP infrastructure is complex and requires extensive testing of the CA→OCSP
responder integration.

**Equifax breach and certificate expiry (2017).** Equifax's security monitoring tool had
an expired TLS certificate. The certificate renewal was missed because the tool was
silently decrypting traffic to inspect it (TLS inspection proxy), and the proxy's internal
certificate had expired. Monitors showed no traffic, which was misinterpreted as no
attacks. The lesson: expired certificates in infrastructure tools are not just
inconveniences — they can blind your security monitoring.

**DigiNotar CA compromise (2011).** Dutch CA DigiNotar was compromised and issued over
500 fraudulent certificates, including `*.google.com`. Because DigiNotar was in the
trust store of all major browsers and the Windows OS, these certificates were accepted as
valid for months. The Certificate Transparency project was directly motivated by this
incident: CT logs would have made the misissuance visible immediately. DigiNotar went
bankrupt within weeks of public disclosure. The lesson: CA compromise is a systemic risk
for every domain; CT and CAA records are the mitigations.

**SSH host key confusion (various years).** SSH uses host keys for server authentication,
analogous to TLS certificates. Without a PKI to bind hostnames to host keys, SSH relies
on Trust On First Use (TOFU) — the first time you connect, you accept the key. If the
host key changes unexpectedly (server reinstallation, IP reassignment), SSH shows a
warning that most users accept. Organizations running certificate-based SSH (using a CA
to sign host keys) avoid this; they pin trust to the CA's public key in `~/.ssh/known_hosts`.

## Security Analysis

**Forward secrecy.** X25519 with ephemeral keys provides forward secrecy: each session
generates new key material, and compromise of any session key does not affect past or
future sessions. RSA key exchange (used in TLS 1.2 with static RSA) has no forward
secrecy: recording traffic today and obtaining the server's private key later decrypts
all recorded traffic. This is why TLS 1.3 mandated ECDHE-only key exchange.

**Certificate pinning tradeoffs.** Pinning to the exact leaf certificate provides the
strongest protection against misissuance but requires pin updates every time the
certificate rotates. Pinning to the intermediate CA is more flexible but requires trusting
the CA's issuance practices. Pinning to the SPKI (Subject Public Key Info) hash balances
these: pin the public key, not the certificate; the key can be reused in renewed certs.
`crypto/tls.Config.VerifyPeerCertificate` is the Go hook for SPKI pinning.

**OCSP stapling vs CRL.** Certificate Revocation Lists (CRLs) are the historical mechanism
for revocation, but they grew to hundreds of megabytes and required polling. OCSP provides
real-time status but adds a network round-trip per certificate. OCSP stapling is the
correct default for 2024: the server bears the cost of OCSP, and clients get revocation
status without a round-trip. OCSP Must-Staple forces this behavior: clients must reject
certificates that lack a stapled OCSP response.

## Common Pitfalls

1. **Using raw X25519 output as a symmetric key.** The output of ECDH is not a uniformly
   random key — it is a field element. Always process with HKDF before using as an AES-GCM
   or ChaCha20 key.

2. **Not enforcing ephemeral key exchange.** Using a static ECDH private key destroys
   forward secrecy. Always generate a fresh ephemeral key pair per session. `ring`'s
   `agree_ephemeral` enforces this via the type system; Go requires manual discipline.

3. **ECDSA with a weak or repeated nonce.** ECDSA requires a random `k` per signature.
   A repeated `k` exposes the private key (PS3 attack). If you must use ECDSA, use the
   Go stdlib's `ecdsa.Sign` which generates `k` using `crypto/rand`. Prefer Ed25519 which
   is deterministic.

4. **Certificate CN-only hostname validation.** Using `cert.Subject.CommonName` for
   hostname validation is incorrect; use `cert.VerifyHostname(name)` which checks SAN.
   Go's `crypto/tls` does this correctly; custom verification code often does not.

5. **Ignoring certificate expiry in internal PKI.** Internal CAs often issue certificates
   with 10-year validity periods for "convenience." These accumulate, become forgotten,
   and block rotation when the CA is eventually compromised. Issue internal certificates
   with the same 90-day lifetime as Let's Encrypt. Automate renewal.

## Exercises

**Exercise 1** (30 min): Examine a certificate chain in Wireshark. Connect to
`https://example.com` in your browser, export the certificate chain (DevTools → Security →
View Certificate). Use `openssl x509 -text -noout -in cert.pem` to inspect the leaf
certificate. Identify the SPKI, SAN, issuer, EKU, and BasicConstraints fields.

**Exercise 2** (2–4h): Implement X25519 key exchange in Go and print the derived key.
Then implement the same in Rust with `ring`. Verify that the Go and Rust implementations
derive the same key when given the same (fixed) private key bytes. Note: X25519 is
defined over GF(2^255 - 19); the same scalar multiplication must produce the same output
regardless of implementation.

**Exercise 3** (4–8h): Build a simple certificate authority in Go using `crypto/x509`.
Generate a root CA key pair (Ed25519), self-sign the root CA certificate, then issue a
leaf certificate signed by the root CA. Write a TLS server using the leaf certificate
and a client that trusts only your custom root CA (not the system root store). Verify
that the client rejects a certificate issued by a different CA.

**Exercise 4** (8–15h): Implement SPKI pinning for a service-to-service Go client. The
client stores a list of expected SPKI SHA-256 hashes for the server's certificate. On
connection, extract the leaf certificate's SPKI, hash it, and reject the connection if
the hash is not in the allowlist. Implement automatic pin refresh: if the server presents
a certificate signed by a trusted CA but with a new SPKI (key rotation), update the pin
store. Write tests for: (a) valid pin matches, (b) pin mismatch rejection, (c) pin
rotation with CA trust.

## Further Reading

### Foundational Papers

- Bernstein — "Curve25519: New Diffie-Hellman Speed Records" (PKC 2006). The design paper
  for X25519. Read the "Secure implementations" section for the design rationale.
- Bernstein et al. — "High-Speed High-Security Signatures" (CHES 2011). The Ed25519 paper.
  Section 4 (security) is the most important for implementers.
- Laurie, Langley, Kasper — "Certificate Transparency" (RFC 6962, 2013). The CT design.

### Books

- Ivan Ristic — "Bulletproof TLS and PKI" (Feisty Duck, 2nd edition). Chapter 5 covers
  PKI architecture and certificate lifecycle in detail.
- Alfred Menezes et al. — "Handbook of Applied Cryptography" (freely available). Chapter
  11 on digital signatures covers ECDSA and EdDSA.

### Production Code to Read

- `filippo.io/edwards25519` — Go implementation of Twisted Edwards curve operations.
  The constant-time design is explicit in the comments.
- `curve25519-dalek` (`src/edwards.rs`) — Rust. Particularly the `EdwardsPoint::mul`
  implementation which uses the Montgomery ladder.
- Let's Encrypt's Boulder (`letsencrypt/boulder`) — the production CA software. Study
  `ca/ca.go` for how certificates are issued and `va/va.go` for domain validation.

### Security Advisories / CVEs to Study

- CVE-2022-21449 (Java "Psychic Signatures") — Java 15–18 ECDSA verifier accepted `r=0,
  s=0` as a valid signature for any message under any key. Demonstrates the criticality
  of checking that `r` and `s` are in the valid range.
- CVE-2020-0601 (CurveBall) — Windows CryptoAPI validated ECC certificate keys without
  verifying the curve generator parameter. Attackers could forge certificates by using a
  generator that maps to an attacker-controlled point.
- DigiNotar breach (2011) — no CVE, but the post-mortem report is required reading for
  understanding CA trust model failures.
