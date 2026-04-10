<!--
type: reference
difficulty: advanced
section: [08-networking-and-security]
concepts: [schnorr-protocol, fiat-shamir, pedersen-commitments, sigma-protocols, zero-knowledge, completeness, soundness, non-interactive-proofs]
languages: [go, rust]
estimated_reading_time: 75-90 min
bloom_level: analyze
prerequisites: [modular-arithmetic, group-theory-basics, hash-functions, discrete-logarithm-problem]
papers: [Schnorr 1991 — Efficient Signature Generation by Smart Cards, Fiat & Shamir 1986 — How To Prove Yourself, Pedersen 1991 — Non-Interactive and Information-Theoretic Secure Verifiable Secret Sharing, Thaler 2022 — Proofs Arguments and Zero-Knowledge]
industry_use: [zcash, signal-zkp, idemix, semaphore, aztec-network, polygon-id]
language_contrast: medium
-->

# Zero-Knowledge Proofs

> A zero-knowledge proof lets you convince someone that you know a secret without
> revealing the secret — the transcript of a successful proof is indistinguishable from
> a transcript a simulator could produce without knowing anything.

## Mental Model

A zero-knowledge proof system has three security properties that must all hold simultaneously:

**Completeness**: an honest prover who actually knows the secret will always convince an
honest verifier. This is the "it works when it should work" property.

**Soundness**: a dishonest prover who does not know the secret cannot convince the verifier,
except with negligible probability. This is the "it fails when it should fail" property.
The "negligible probability" is usually 2^-128 after multiple rounds.

**Zero-knowledge**: the verifier learns nothing beyond the fact that the statement is true.
Formally, there exists a *simulator* that can produce valid-looking transcripts without
knowing the prover's secret. If the verifier cannot distinguish real transcripts from
simulated ones, the protocol is zero-knowledge. This is the "proof leaks nothing" property.

The Schnorr identification protocol is the canonical sigma protocol: it has exactly three
moves (commitment → challenge → response), it satisfies all three properties for the
discrete logarithm relation, and it is the foundation for most practical ZKP systems in
use today. Understanding Schnorr deeply is a prerequisite for Bulletproofs, STARKs, and
zk-SNARKs.

The Fiat-Shamir heuristic converts any public-coin interactive protocol (one where the
verifier's challenge is random) into a non-interactive protocol by replacing the verifier's
random challenge with a hash of the prover's commitment. This produces a signature scheme
(Schnorr signatures are exactly Fiat-Shamir applied to the Schnorr identification protocol).
The security reduction requires the hash function to behave as a random oracle — this is
why the info/context must include all public parameters, not just the commitment.

The central engineering challenge with ZKP is parameter discipline. Real systems use
standardized groups (RFC 3526 MODP groups) to avoid generating parameters incorrectly,
and they use constant-time arithmetic to prevent timing attacks during proof generation.
The security of the discrete logarithm assumption depends on the group parameters being
properly generated — using weak groups is the fastest way to invalidate all proofs
produced under them.

## Core Concepts

### Schnorr Identification Protocol (Interactive)

**Setup**: public prime `p`, prime-order subgroup generator `g`, subgroup order `q`.
Prover knows secret `x`, public key `y = g^x mod p`.

**Protocol** (three moves):
1. Prover picks random `r ← Zq`, computes commitment `t = g^r mod p`, sends `t`.
2. Verifier picks random challenge `c ← Zq`, sends `c`.
3. Prover computes response `s = (r + c*x) mod q`, sends `s`.
4. Verifier checks: `g^s ≡ t * y^c (mod p)`.

Correctness: `g^s = g^(r + cx) = g^r * (g^x)^c = t * y^c mod p`. ✓

**Simulator**: pick `c` and `s` at random, compute `t = g^s * y^(-c) mod p`.
The triple `(t, c, s)` satisfies the verification equation without knowing `x`.
This is what makes the protocol zero-knowledge.

### Fiat-Shamir Transform (Non-Interactive)

Replace the verifier's random challenge with `c = H(g || y || t)` where `H` is a hash
function modeled as a random oracle. The prover computes `c` themselves and produces
the response `s = (r + c*x) mod q`.

The proof is the pair `(t, s)`. Verification: compute `c = H(g || y || t)`, check
`g^s ≡ t * y^c (mod p)`.

**Critical**: the hash must include all public parameters, not just `t`. If you hash
only `t`, an attacker can fix `c` and `s` first, then compute `t = g^s * y^(-c) mod p`,
producing a valid proof without knowing `x`. Including `y` in the hash binds the proof
to a specific public key.

### Pedersen Commitments

A Pedersen commitment lets you commit to a value `x` with blinding factor `r`:
`C = g^x * h^r mod p` where `g` and `h` are independent generators (nobody knows
`log_g(h)`).

Properties:
- **Hiding**: the commitment reveals nothing about `x` (statistically — even with infinite
  compute, you cannot determine `x` from `C`).
- **Binding**: you cannot find two different values `(x, r)` and `(x', r')` that produce
  the same commitment — unless you can solve discrete log.

To open the commitment, reveal `(x, r)`. The verifier checks `g^x * h^r ≡ C (mod p)`.

## Implementation: Go

```go
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
)

// RFC 3526 MODP Group 14 parameters (2048-bit).
// Using standardized parameters avoids the risk of generating unsafe primes.
// These parameters are public knowledge — security comes from the discrete log hardness.
var (
	// p: the 2048-bit safe prime
	pHex = "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1" +
		"29024E088A67CC74020BBEA63B139B22514A08798E3404DD" +
		"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245" +
		"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
		"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D" +
		"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F" +
		"83655D23DCA3AD961C62F356208552BB9ED529077096966D" +
		"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B" +
		"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9" +
		"DE2BCBF6955817183995497CEA956AE515D2261898FA0510" +
		"15728E5A8AACAA68FFFFFFFFFFFFFFFF"

	p, _ = new(big.Int).SetString(pHex, 16)
	g    = big.NewInt(2) // generator for the MODP group

	// q = (p-1)/2 (safe prime subgroup order)
	q = new(big.Int).Rsh(new(big.Int).Sub(p, big.NewInt(1)), 1)
)

// SchnorrParams holds the public parameters for a Schnorr proof.
type SchnorrParams struct {
	P *big.Int // prime modulus
	Q *big.Int // subgroup order
	G *big.Int // generator
}

// KeyPair holds a Schnorr key pair.
type KeyPair struct {
	X *big.Int // secret scalar (private key)
	Y *big.Int // g^X mod P (public key)
}

// GenerateKeyPair generates a Schnorr key pair.
func GenerateKeyPair(params SchnorrParams) (*KeyPair, error) {
	x, err := rand.Int(rand.Reader, params.Q)
	if err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	y := new(big.Int).Exp(params.G, x, params.P)
	return &KeyPair{X: x, Y: y}, nil
}

// InteractiveProof represents a Schnorr transcript: commitment, challenge, response.
type InteractiveProof struct {
	T *big.Int // commitment: g^r mod p
	C *big.Int // verifier's challenge
	S *big.Int // response: r + c*x mod q
}

// SchnorrProve executes the prover side of the interactive protocol.
// r is the random nonce chosen by the prover.
func SchnorrProve(params SchnorrParams, kp *KeyPair, challenge *big.Int) (*InteractiveProof, error) {
	// Step 1: generate random commitment r, compute t = g^r mod p
	r, err := rand.Int(rand.Reader, params.Q)
	if err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	t := new(big.Int).Exp(params.G, r, params.P)

	// Step 3: s = r + c*x mod q
	cx := new(big.Int).Mul(challenge, kp.X)
	cx.Mod(cx, params.Q)
	s := new(big.Int).Add(r, cx)
	s.Mod(s, params.Q)

	return &InteractiveProof{T: t, C: challenge, S: s}, nil
}

// SchnorrVerify verifies a Schnorr proof transcript.
// Checks: g^s ≡ t * y^c (mod p)
func SchnorrVerify(params SchnorrParams, publicKey *big.Int, proof *InteractiveProof) bool {
	// LHS = g^s mod p
	lhs := new(big.Int).Exp(params.G, proof.S, params.P)

	// RHS = t * y^c mod p
	yc := new(big.Int).Exp(publicKey, proof.C, params.P)
	rhs := new(big.Int).Mul(proof.T, yc)
	rhs.Mod(rhs, params.P)

	return lhs.Cmp(rhs) == 0
}

// NonInteractiveProof is a Fiat-Shamir Schnorr proof.
// The challenge is derived from the hash of public parameters + commitment.
type NonInteractiveProof struct {
	T *big.Int // commitment
	S *big.Int // response
}

// FiatShamirProve generates a non-interactive Schnorr proof.
// The challenge c = SHA256(p || g || y || t) binds the proof to the public key y.
func FiatShamirProve(params SchnorrParams, kp *KeyPair) (*NonInteractiveProof, error) {
	r, err := rand.Int(rand.Reader, params.Q)
	if err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	t := new(big.Int).Exp(params.G, r, params.P)

	// Hash all public inputs: p, g, y, t.
	// Omitting any of these allows an attacker to forge proofs for different keys.
	c := fiatShamirChallenge(params.P, params.G, kp.Y, t)

	cx := new(big.Int).Mul(c, kp.X)
	cx.Mod(cx, params.Q)
	s := new(big.Int).Add(r, cx)
	s.Mod(s, params.Q)

	return &NonInteractiveProof{T: t, S: s}, nil
}

// fiatShamirChallenge computes c = SHA256(p || g || y || t) as a big.Int mod q.
func fiatShamirChallenge(p, g, y, t *big.Int) *big.Int {
	h := sha256.New()
	appendBigInt := func(n *big.Int) {
		b := n.Bytes()
		// Length-prefix each field to prevent concatenation ambiguity.
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(b)))
		h.Write(length[:])
		h.Write(b)
	}
	appendBigInt(p)
	appendBigInt(g)
	appendBigInt(y)
	appendBigInt(t)

	digest := h.Sum(nil)
	c := new(big.Int).SetBytes(digest)
	// Reduce mod q so c is in the correct range.
	c.Mod(c, q)
	return c
}

// FiatShamirVerify verifies a non-interactive Schnorr proof.
func FiatShamirVerify(params SchnorrParams, publicKey *big.Int, proof *NonInteractiveProof) bool {
	c := fiatShamirChallenge(params.P, params.G, publicKey, proof.T)

	lhs := new(big.Int).Exp(params.G, proof.S, params.P)
	yc := new(big.Int).Exp(publicKey, c, params.P)
	rhs := new(big.Int).Mul(proof.T, yc)
	rhs.Mod(rhs, params.P)

	return lhs.Cmp(rhs) == 0
}

// PedersenCommitment represents a commitment to a value with a blinding factor.
type PedersenCommitment struct {
	C *big.Int // g^value * h^blinding mod p
}

// pedersenH is a second generator for Pedersen commitments.
// In practice, h = hash-to-group(g) — no known discrete log relationship to g.
// For demonstration, we use g^3 mod p (in a real system, this would be insecure
// if the DL of h base g is known — use hash-to-group or trusted setup).
var pedersenH = new(big.Int).Exp(g, big.NewInt(3), p)

// PedersenCommit creates a commitment to value using random blinding factor.
func PedersenCommit(value *big.Int) (*PedersenCommitment, *big.Int, error) {
	blinding, err := rand.Int(rand.Reader, q)
	if err != nil {
		return nil, nil, fmt.Errorf("generate blinding: %w", err)
	}

	gv := new(big.Int).Exp(g, value, p)
	hr := new(big.Int).Exp(pedersenH, blinding, p)
	c := new(big.Int).Mul(gv, hr)
	c.Mod(c, p)

	return &PedersenCommitment{C: c}, blinding, nil
}

// PedersenVerify verifies that a commitment opens to the claimed value.
func PedersenVerify(comm *PedersenCommitment, value, blinding *big.Int) bool {
	gv := new(big.Int).Exp(g, value, p)
	hr := new(big.Int).Exp(pedersenH, blinding, p)
	expected := new(big.Int).Mul(gv, hr)
	expected.Mod(expected, p)
	return comm.C.Cmp(expected) == 0
}

func main() {
	params := SchnorrParams{P: p, Q: q, G: g}

	// Interactive Schnorr
	kp, err := GenerateKeyPair(params)
	if err != nil {
		panic(err)
	}

	// Simulate verifier challenge
	challenge, _ := rand.Int(rand.Reader, q)
	proof, err := SchnorrProve(params, kp, challenge)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Interactive Schnorr proof valid: %v\n", SchnorrVerify(params, kp.Y, proof))

	// Non-interactive (Fiat-Shamir)
	niProof, err := FiatShamirProve(params, kp)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Fiat-Shamir proof valid: %v\n", FiatShamirVerify(params, kp.Y, niProof))

	// Verify with wrong public key — must fail
	wrongKP, _ := GenerateKeyPair(params)
	fmt.Printf("Fiat-Shamir with wrong key: %v (expect false)\n",
		FiatShamirVerify(params, wrongKP.Y, niProof))

	// Pedersen commitment
	value := big.NewInt(42)
	comm, blinding, err := PedersenCommit(value)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Pedersen opens correctly: %v\n", PedersenVerify(comm, value, blinding))
	fmt.Printf("Pedersen tampered value:  %v (expect false)\n",
		PedersenVerify(comm, big.NewInt(43), blinding))
}
```

### Go-specific considerations

**`math/big` is not constant-time.** The `big.Int` operations in Go use variable-time
algorithms. For production ZKP code where the prover's secret `x` is sensitive, all
computations involving `x` (especially `Exp` for the response `s = r + c*x`) should use
constant-time implementations. The `golang.org/x/crypto/internal/nistec` package provides
constant-time elliptic curve operations; for DLP-based protocols on modular groups, use
the `filippo.io/nistec` package or switch to elliptic curve-based Schnorr (curve25519-dalek
equivalent is not yet in Go stdlib).

**Prefer elliptic curve groups.** The 2048-bit modular arithmetic above requires 2048-bit
big integer multiplications. Using curve25519 gives 128-bit security with 256-bit
integers, making operations ~16x faster. The `filippo.io/edwards25519` package provides
the correct group operations for curve25519-based Schnorr.

**`num-bigint` in Rust is analogous to `math/big` in Go.** Both are general-purpose
arbitrary-precision libraries with variable-time operations.

## Implementation: Rust

```rust
use num_bigint::{BigUint, RandBigInt};
use num_traits::{One, Zero};
use rand::thread_rng;
use sha2::{Digest, Sha256};

// RFC 3526 MODP Group 14 — 2048-bit prime.
const P_HEX: &str = "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1\
29024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245\
E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D\
C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F83655D23DCA3AD961C62F356208552BB9ED529077096966D\
670C354E4ABC9804F1746C08CA18217C32905E462E36CE3BE39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9\
DE2BCBF6955817183995497CEA956AE515D2261898FA051015728E5A8AACAA68FFFFFFFFFFFFFFFF";

#[derive(Debug, Clone)]
struct SchnorrParams {
    p: BigUint,
    q: BigUint, // (p-1)/2
    g: BigUint,
}

impl SchnorrParams {
    fn from_rfc3526() -> Self {
        let p = BigUint::parse_bytes(P_HEX.replace('\n', "").as_bytes(), 16)
            .expect("parse p");
        let q = (&p - BigUint::one()) >> 1usize;
        let g = BigUint::from(2u32);
        Self { p, q, g }
    }
}

#[derive(Debug, Clone)]
struct KeyPair {
    x: BigUint, // private key
    y: BigUint, // g^x mod p (public key)
}

fn generate_keypair(params: &SchnorrParams) -> KeyPair {
    let mut rng = thread_rng();
    let x = rng.gen_biguint_below(&params.q);
    let y = params.g.modpow(&x, &params.p);
    KeyPair { x, y }
}

#[derive(Debug, Clone)]
struct NonInteractiveProof {
    t: BigUint,
    s: BigUint,
}

/// Compute Fiat-Shamir challenge: SHA256(p || g || y || t) mod q.
/// Length-prefixing each field prevents concatenation ambiguity attacks.
fn fiat_shamir_challenge(p: &BigUint, g: &BigUint, y: &BigUint, t: &BigUint) -> BigUint {
    let mut hasher = Sha256::new();
    for n in [p, g, y, t] {
        let bytes = n.to_bytes_be();
        let len = (bytes.len() as u32).to_be_bytes();
        hasher.update(len);
        hasher.update(&bytes);
    }
    let digest = hasher.finalize();
    BigUint::from_bytes_be(&digest)
}

/// Generate a non-interactive Schnorr proof (Fiat-Shamir transform).
fn fiat_shamir_prove(params: &SchnorrParams, kp: &KeyPair) -> NonInteractiveProof {
    let mut rng = thread_rng();
    let r = rng.gen_biguint_below(&params.q);
    let t = params.g.modpow(&r, &params.p);

    let c = fiat_shamir_challenge(&params.p, &params.g, &kp.y, &t);
    let c_mod_q = &c % &params.q;

    // s = (r + c * x) mod q
    let cx = (&c_mod_q * &kp.x) % &params.q;
    let s = (&r + &cx) % &params.q;

    NonInteractiveProof { t, s }
}

/// Verify a non-interactive Schnorr proof.
fn fiat_shamir_verify(params: &SchnorrParams, public_key: &BigUint, proof: &NonInteractiveProof) -> bool {
    let c = fiat_shamir_challenge(&params.p, &params.g, public_key, &proof.t);
    let c_mod_q = &c % &params.q;

    // Check: g^s ≡ t * y^c (mod p)
    let lhs = params.g.modpow(&proof.s, &params.p);
    let yc = public_key.modpow(&c_mod_q, &params.p);
    let rhs = (&proof.t * &yc) % &params.p;

    lhs == rhs
}

/// Pedersen commitment: C = g^value * h^blinding mod p.
/// h is a second generator with no known discrete log relationship to g.
fn second_generator(params: &SchnorrParams) -> BigUint {
    // In production: derive h via hash-to-group from a publicly verifiable seed.
    // Here: h = g^7 mod p (for demonstration — in real code, use a nothing-up-my-sleeve value).
    params.g.modpow(&BigUint::from(7u32), &params.p)
}

fn pedersen_commit(params: &SchnorrParams, value: &BigUint) -> (BigUint, BigUint) {
    let h = second_generator(params);
    let mut rng = thread_rng();
    let blinding = rng.gen_biguint_below(&params.q);

    let gv = params.g.modpow(value, &params.p);
    let hr = h.modpow(&blinding, &params.p);
    let commitment = (&gv * &hr) % &params.p;

    (commitment, blinding)
}

fn pedersen_verify(params: &SchnorrParams, commitment: &BigUint, value: &BigUint, blinding: &BigUint) -> bool {
    let h = second_generator(params);
    let gv = params.g.modpow(value, &params.p);
    let hr = h.modpow(blinding, &params.p);
    let expected = (&gv * &hr) % &params.p;
    &expected == commitment
}

fn main() {
    let params = SchnorrParams::from_rfc3526();
    let kp = generate_keypair(&params);

    // Non-interactive proof
    let proof = fiat_shamir_prove(&params, &kp);
    println!("Fiat-Shamir valid: {}", fiat_shamir_verify(&params, &kp.y, &proof));

    // Wrong public key must fail
    let wrong_kp = generate_keypair(&params);
    println!("Wrong public key: {} (expect false)", fiat_shamir_verify(&params, &wrong_kp.y, &proof));

    // Pedersen commitment
    let value = BigUint::from(42u32);
    let (commitment, blinding) = pedersen_commit(&params, &value);
    println!("Pedersen opens: {}", pedersen_verify(&params, &commitment, &value, &blinding));
    println!("Tampered value: {} (expect false)",
        pedersen_verify(&params, &commitment, &BigUint::from(43u32), &blinding));
}
```

### Rust-specific considerations

**`num-bigint` is variable-time.** The same warning as for Go's `math/big` applies:
`BigUint::modpow` is not constant-time. For production-grade ZKP where timing leaks
the prover's secret, use the `curve25519-dalek` crate which provides constant-time
scalar multiplication on Curve25519. The challenge in the ZKP system (`45-zero-knowledge-
proof-system`) explicitly recommends `curve25519-dalek` as a stretch goal for this reason.

**`bellman` for zk-SNARKs.** For Groth16 proofs and more advanced ZKP constructions
(as used in Zcash), the `bellman` crate provides the circuit definition and proving system.
`arkworks` is the other major ecosystem. Both require understanding R1CS constraint systems
and are significantly more complex than Schnorr.

**`curve25519-dalek` for production-grade sigma protocols.** The crate provides constant-
time `RistrettoPoint` operations, which are ideal for Schnorr-style proofs on an elliptic
curve. The group order is ~2^252 (much smaller arithmetic than 2048-bit modular groups),
and the API explicitly marks which operations are constant-time.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Big integer arithmetic | `math/big` (stdlib) | `num-bigint` (crate) |
| Constant-time scalar ops | `filippo.io/edwards25519` | `curve25519-dalek` |
| zk-SNARK support | Limited (no `bellman` equivalent) | `bellman`, `arkworks`, `plonky2` |
| Sigma protocol libraries | None — implement from scratch | `merlin` (transcript), `bulletproofs` |
| Hash-to-group | Manual | `curve25519-dalek::RistrettoPoint::hash_from_bytes` |
| Type-safety for group elements | No (bigints are raw bytes) | `RistrettoPoint` is a distinct type |
| Constant-time guarantees | Weak (needs external crates) | `subtle` crate + `dalek` ecosystem |
| Production adoption | Limited | Zcash (Sapling), Solana (SP1) |

## Production War Stories

**Zcash Sprout inflation bug (2018).** A subtle soundness bug in the original Zcash zk-SNARK
circuit allowed an attacker to generate valid proofs for invalid transactions — creating ZEC
from nothing. The bug existed for ~2 years before discovery. No attacker exploited it.
The root cause was a constraint system bug, not an issue with the underlying ZKP math.
The lesson: circuit implementations require independent audits; soundness bugs in ZKP
circuits are extremely difficult to detect without formal verification.

**Tornado Cash Merkle tree nullifier reuse (2019 audit finding).** A review found that
the nullifier hash construction in early Tornado Cash contracts could be vulnerable if
the hash function's collision resistance degraded. The fix was to use Poseidon (a
ZKP-friendly hash) instead of SHA256, which simplified the circuit and reduced proof size
by 40%.

**Signal's Private Set Intersection (2021).** Signal deployed Private Set Intersection
to check phone contacts without revealing which contacts are not on Signal. The protocol
uses elliptic curve DH combined with a Bloom filter — not a ZKP, but a related
cryptographic protocol. The deployment involved tens of millions of users and showed that
ZKP-adjacent privacy protocols are deployable at production scale.

## Security Analysis

**Soundness error.** A single-round Schnorr proof has soundness error 1/q (negligible
for 256-bit q). An attacker who guesses the challenge correctly can forge a proof. For
non-interactive proofs (Fiat-Shamir), the soundness error is 1/|H| where |H| is the
hash output space. Using SHA-256 gives soundness error 2^-256.

**Rewinding attacks and simulation soundness.** The Fiat-Shamir transform is only provably
secure in the random oracle model. In the standard model (where H is a real hash function,
not a random oracle), the proof system may not be simulation-sound — a malicious prover
who can rewind the random oracle can potentially extract witnesses. For production systems,
this means: (a) use standardized hash constructions, (b) do not implement custom Fiat-
Shamir variants without a security proof.

**Trusted setup for zk-SNARKs.** Groth16 proofs (used in Zcash) require a trusted setup
ceremony — a multi-party computation that generates the proving and verification keys.
If any single participant in the ceremony retains their randomness, they can forge proofs.
Zcash ran ceremonies with hundreds of participants to minimize this risk. STARKs and
Bulletproofs are transparent (no trusted setup), which is why they are preferred for new
deployments despite being larger.

**Pedersen commitment second generator.** The security of Pedersen commitments requires
that the discrete log of `h` base `g` is unknown to anyone. Using `h = g^k` for any
known `k` breaks the binding property. In production, generate `h` using a
nothing-up-my-sleeve construction: `h = hash_to_group(canonical_context_string)` where
the derivation is publicly auditable.

## Common Pitfalls

1. **Not including all public parameters in Fiat-Shamir challenge.** If you hash only the
   commitment `t` without including `g`, `y`, and the group parameters, an attacker can
   fix `c` and `s` freely and compute `t = g^s * y^(-c) mod p` — a valid proof for any
   public key. The challenge must bind the proof to the specific statement being proven.

2. **Using variable-time `modpow` during proof generation when `x` is secret.** If the
   prover's secret `x` is high-value (e.g., a signing key), variable-time exponentiation
   leaks bits of `x` through timing. Use constant-time scalar multiplication.

3. **Reusing the commitment nonce `r` across two proofs.** If you generate two proofs
   with the same `r` (and the same key `x`) but different challenges, an attacker can
   recover `x` from the two responses: `s1 - s2 = (c1 - c2) * x mod q`. This is the
   "lattice attack on Schnorr" — the same technique used to extract Sony's PlayStation 3
   signing key (which used a static `r`).

4. **Using a Pedersen generator `h = g^k` for known `k`.** If `log_g(h) = k` is known,
   the binding property is broken: you can create two openings `(x, r)` and `(x', r')`
   for the same commitment by solving `x' - x ≡ k*(r - r') mod q`.

5. **Treating proof of knowledge and proof of membership as equivalent.** A Schnorr proof
   proves knowledge of the discrete log, not that the discrete log is a particular value.
   If you want to prove "I know the preimage of this hash," you need a proof system for
   the hash function's circuit (R1CS/PLONK), not Schnorr.

## Exercises

**Exercise 1** (30 min): Implement the Schnorr simulator in Go. Given parameters and a
public key `y` (but no secret `x`), produce transcripts `(t, c, s)` that pass the
verification equation. Verify that 10 simulated transcripts all pass. This makes the
zero-knowledge property concrete.

**Exercise 2** (2–4h): Implement the reuse attack. Generate two Schnorr proofs for the
same key `kp` using the same commitment nonce `r` but different challenges `c1` and `c2`.
Show that you can recover `x = (s1 - s2) * inverse(c1 - c2) mod q`. Verify the recovered
`x` satisfies `g^x ≡ y (mod p)`.

**Exercise 3** (4–8h): Implement the complete challenge from
`programming-challenges/03-insane/45-zero-knowledge-proof-system/` in Rust. Follow the
three phases: interactive Schnorr, Fiat-Shamir transform, and Pedersen commitment with
proof of knowledge. Use the RFC 3526 group 14 parameters. Write the 12 test cases from
the acceptance criteria.

**Exercise 4** (8–15h): Port the Schnorr implementation to use Curve25519 via `curve25519-
dalek`. Replace `BigUint::modpow` with `RistrettoPoint::mul_base` and `Scalar`
operations. Benchmark the two implementations: 2048-bit modular Schnorr vs Curve25519
Schnorr. Profile with `criterion`. Demonstrate that the Curve25519 version is constant-
time by measuring proof generation time for 10,000 different secrets and confirming the
standard deviation is below 1 microsecond.

## Further Reading

### Foundational Papers

- Schnorr, "Efficient Signature Generation by Smart Cards" (J. Cryptology, 1991). The
  original Schnorr identification protocol paper.
- Fiat and Shamir, "How To Prove Yourself: Practical Solutions to Identification and
  Signature Problems" (CRYPTO 1986). The transform from interactive to non-interactive.
- Pedersen, "Non-Interactive and Information-Theoretic Secure Verifiable Secret Sharing"
  (CRYPTO 1991). The commitment scheme.
- Thaler, "Proofs, Arguments, and Zero-Knowledge" (2022, freely available at
  people.cs.georgetown.edu/jthaler/ProofsArgsAndZK.pdf). The best modern textbook;
  chapters 12–13 cover sigma protocols specifically.

### Books

- Dan Boneh, Victor Shoup — "A Graduate Course in Applied Cryptography" — chapters 19–20
  on sigma protocols and zero-knowledge.
- Boneh and Shoup — specific treatment of Fiat-Shamir in the random oracle model
  (chapter 19.2).

### Production Code to Read

- `curve25519-dalek/src/ristretto.rs` — constant-time Ristretto group operations.
  Study how scalar multiplication is implemented without branching on secret bits.
- `bulletproofs` crate source (dalek-cryptography) — implements Bulletproofs range proofs
  built on Pedersen commitments and a Fiat-Shamir transcript via `merlin`.
- Zcash Sapling circuit source (`zcash/librustzcash`) — a full R1CS circuit for a
  production ZKP application.

### Security Advisories / CVEs to Study

- Zcash Sprout inflation bug (disclosed 2019, Zcash blog) — soundness failure in a ZKP
  circuit. Demonstrates how circuit bugs differ from protocol bugs.
- PS3 signing key extraction (2010) — Sony's ECDSA implementation used a static nonce
  `r`, allowing private key recovery from two signatures with the same `r`.
- CVE-2019-14318 (mcl library timing attack) — variable-time scalar multiplication in
  a ZKP support library leaked the prover's secret through timing.
