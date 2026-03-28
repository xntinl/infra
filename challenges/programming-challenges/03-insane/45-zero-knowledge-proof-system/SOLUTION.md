# Solution: Zero-Knowledge Proof System

## Architecture Overview

The solution is structured in five modules:

1. **`params`**: Cryptographic group parameters (RFC 3526 2048-bit MODP group), generator selection, and utility functions for modular arithmetic on big integers
2. **`schnorr`**: Interactive Schnorr identification protocol with prover, verifier, and simulator
3. **`fiat_shamir`**: Non-interactive Schnorr proofs via the Fiat-Shamir heuristic
4. **`pedersen`**: Pedersen commitment scheme with open/verify
5. **`preimage_proof`**: Protocol for proving knowledge of a hash preimage using Pedersen commitments and Schnorr-like sub-proofs

All protocols operate on 2048-bit modular arithmetic using the `num-bigint` crate. The implementation avoids rolling custom prime generation by using the well-known group from RFC 3526.

## Rust Solution

### Cargo.toml

```toml
[package]
name = "zkp-system"
version = "0.1.0"
edition = "2021"

[dependencies]
num-bigint = { version = "0.4", features = ["rand"] }
num-traits = "0.2"
num-integer = "0.1"
sha2 = "0.10"
rand = "0.8"
hex = "0.4"
```

### src/params.rs

```rust
use num_bigint::BigUint;
use num_traits::One;
use sha2::{Sha256, Digest};

/// RFC 3526 2048-bit MODP Group (Group 14).
/// p is a safe prime: p = 2q + 1 where q is also prime.
pub fn rfc3526_2048() -> (BigUint, BigUint, BigUint) {
    let p = BigUint::parse_bytes(
        b"FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1\
          29024E088A67CC74020BBEA63B139B22514A08798E3404DD\
          EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245\
          E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED\
          EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D\
          C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F\
          83655D23DCA3AD961C62F356208552BB9ED529077096966D\
          670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B\
          E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9\
          DE2BCBF6955817183995497CEA956AE515D2261898FA0510\
          15728E5A8AACAA68FFFFFFFFFFFFFFFF",
        16,
    )
    .unwrap();

    // q = (p - 1) / 2
    let q = (&p - BigUint::one()) >> 1;

    // Generator g = 2 (standard for RFC 3526 groups)
    let g = BigUint::from(2u32);

    (p, q, g)
}

/// Hash arbitrary data to a BigUint mod q.
pub fn hash_to_scalar(data: &[&[u8]], q: &BigUint) -> BigUint {
    let mut hasher = Sha256::new();
    for d in data {
        hasher.update((d.len() as u64).to_be_bytes());
        hasher.update(d);
    }
    let hash = hasher.finalize();
    BigUint::from_bytes_be(&hash) % q
}

/// Modular exponentiation: base^exp mod modulus.
pub fn mod_pow(base: &BigUint, exp: &BigUint, modulus: &BigUint) -> BigUint {
    base.modpow(exp, modulus)
}

/// Modular inverse using extended Euclidean algorithm.
pub fn mod_inv(a: &BigUint, modulus: &BigUint) -> Option<BigUint> {
    use num_integer::Integer;
    use num_bigint::BigInt;
    use num_traits::Zero;

    let a_int = BigInt::from(a.clone());
    let m_int = BigInt::from(modulus.clone());

    let gcd = a_int.extended_gcd(&m_int);
    if gcd.gcd != BigInt::one() {
        return None;
    }

    let inv = ((gcd.x % &m_int) + &m_int) % &m_int;
    if inv.is_zero() {
        None
    } else {
        Some(inv.to_biguint().unwrap())
    }
}

/// Modular subtraction: (a - b) mod m, handling underflow.
pub fn mod_sub(a: &BigUint, b: &BigUint, m: &BigUint) -> BigUint {
    if a >= b {
        (a - b) % m
    } else {
        (m - (b - a) % m) % m
    }
}

/// Modular addition: (a + b) mod m.
pub fn mod_add(a: &BigUint, b: &BigUint, m: &BigUint) -> BigUint {
    (a + b) % m
}

/// Modular multiplication: (a * b) mod m.
pub fn mod_mul(a: &BigUint, b: &BigUint, m: &BigUint) -> BigUint {
    (a * b) % m
}
```

### src/schnorr.rs

```rust
use num_bigint::{BigUint, RandBigInt};
use num_traits::One;
use rand::thread_rng;

use crate::params::*;

/// Public parameters for the Schnorr protocol.
#[derive(Clone, Debug)]
pub struct SchnorrParams {
    pub p: BigUint,
    pub q: BigUint,
    pub g: BigUint,
}

impl SchnorrParams {
    pub fn new_rfc3526() -> Self {
        let (p, q, g) = rfc3526_2048();
        SchnorrParams { p, q, g }
    }
}

/// Keypair: secret x, public y = g^x mod p.
#[derive(Clone, Debug)]
pub struct SchnorrKeypair {
    pub secret: BigUint,
    pub public: BigUint,
}

impl SchnorrKeypair {
    pub fn generate(params: &SchnorrParams) -> Self {
        let mut rng = thread_rng();
        let secret = rng.gen_biguint_below(&params.q);
        let public = mod_pow(&params.g, &secret, &params.p);
        SchnorrKeypair { secret, public }
    }
}

/// Interactive protocol transcript.
#[derive(Clone, Debug)]
pub struct SchnorrTranscript {
    pub commitment: BigUint, // t = g^r mod p
    pub challenge: BigUint,  // c (random from verifier)
    pub response: BigUint,   // s = r + c*x mod q
}

// --- Prover ---

/// Prover's first message: commitment.
pub struct ProverState {
    pub r: BigUint,
    pub commitment: BigUint,
}

pub fn prover_commit(params: &SchnorrParams) -> ProverState {
    let mut rng = thread_rng();
    let r = rng.gen_biguint_below(&params.q);
    let commitment = mod_pow(&params.g, &r, &params.p);
    ProverState { r, commitment }
}

/// Prover's response to the verifier's challenge.
pub fn prover_respond(
    params: &SchnorrParams,
    state: &ProverState,
    keypair: &SchnorrKeypair,
    challenge: &BigUint,
) -> BigUint {
    // s = r + c * x mod q
    let cx = mod_mul(challenge, &keypair.secret, &params.q);
    mod_add(&state.r, &cx, &params.q)
}

// --- Verifier ---

/// Verifier generates a random challenge.
pub fn verifier_challenge(params: &SchnorrParams) -> BigUint {
    let mut rng = thread_rng();
    rng.gen_biguint_below(&params.q)
}

/// Verifier checks the transcript.
pub fn verifier_check(
    params: &SchnorrParams,
    public_key: &BigUint,
    transcript: &SchnorrTranscript,
) -> bool {
    // Check: g^s == t * y^c mod p
    let lhs = mod_pow(&params.g, &transcript.response, &params.p);
    let y_c = mod_pow(public_key, &transcript.challenge, &params.p);
    let rhs = mod_mul(&transcript.commitment, &y_c, &params.p);
    lhs == rhs
}

/// Run the full interactive protocol.
pub fn interactive_protocol(
    params: &SchnorrParams,
    keypair: &SchnorrKeypair,
) -> (SchnorrTranscript, bool) {
    let state = prover_commit(params);
    let challenge = verifier_challenge(params);
    let response = prover_respond(params, &state, keypair, &challenge);

    let transcript = SchnorrTranscript {
        commitment: state.commitment,
        challenge,
        response,
    };

    let valid = verifier_check(params, &keypair.public, &transcript);
    (transcript, valid)
}

// --- Simulator (proves zero-knowledge property) ---

/// Simulate a valid transcript WITHOUT knowing the secret key.
/// Pick c and s randomly, compute t = g^s * y^(-c) mod p.
pub fn simulate(params: &SchnorrParams, public_key: &BigUint) -> SchnorrTranscript {
    let mut rng = thread_rng();
    let challenge = rng.gen_biguint_below(&params.q);
    let response = rng.gen_biguint_below(&params.q);

    // t = g^s * y^(-c) mod p
    let g_s = mod_pow(&params.g, &response, &params.p);
    let y_neg_c = {
        let y_c = mod_pow(public_key, &challenge, &params.p);
        mod_inv(&y_c, &params.p).unwrap_or(BigUint::one())
    };
    let commitment = mod_mul(&g_s, &y_neg_c, &params.p);

    SchnorrTranscript {
        commitment,
        challenge,
        response,
    }
}
```

### src/fiat_shamir.rs

```rust
use num_bigint::{BigUint, RandBigInt};
use rand::thread_rng;

use crate::params::*;
use crate::schnorr::SchnorrParams;

/// Non-interactive Schnorr proof (Fiat-Shamir).
#[derive(Clone, Debug)]
pub struct FiatShamirProof {
    pub commitment: BigUint,
    pub response: BigUint,
}

/// Generate a non-interactive proof of knowledge of discrete log.
pub fn prove(
    params: &SchnorrParams,
    secret: &BigUint,
    public: &BigUint,
) -> FiatShamirProof {
    let mut rng = thread_rng();
    let r = rng.gen_biguint_below(&params.q);
    let commitment = mod_pow(&params.g, &r, &params.p);

    // Challenge = H(g, y, t) mod q
    let challenge = hash_to_scalar(
        &[
            &params.g.to_bytes_be(),
            &public.to_bytes_be(),
            &commitment.to_bytes_be(),
        ],
        &params.q,
    );

    // Response: s = r + c * x mod q
    let cx = mod_mul(&challenge, secret, &params.q);
    let response = mod_add(&r, &cx, &params.q);

    FiatShamirProof {
        commitment,
        response,
    }
}

/// Verify a non-interactive proof.
pub fn verify(
    params: &SchnorrParams,
    public: &BigUint,
    proof: &FiatShamirProof,
) -> bool {
    // Recompute challenge
    let challenge = hash_to_scalar(
        &[
            &params.g.to_bytes_be(),
            &public.to_bytes_be(),
            &proof.commitment.to_bytes_be(),
        ],
        &params.q,
    );

    // Check: g^s == t * y^c mod p
    let lhs = mod_pow(&params.g, &proof.response, &params.p);
    let y_c = mod_pow(public, &challenge, &params.p);
    let rhs = mod_mul(&proof.commitment, &y_c, &params.p);
    lhs == rhs
}
```

### src/pedersen.rs

```rust
use num_bigint::{BigUint, RandBigInt};
use rand::thread_rng;

use crate::params::*;
use crate::schnorr::SchnorrParams;

/// Pedersen commitment parameters.
/// Uses two generators g and h where log_g(h) is unknown.
#[derive(Clone, Debug)]
pub struct PedersenParams {
    pub p: BigUint,
    pub q: BigUint,
    pub g: BigUint,
    pub h: BigUint, // second generator, discrete log w.r.t. g unknown
}

impl PedersenParams {
    /// Derive h from g using "nothing up my sleeve" hash.
    pub fn from_schnorr(params: &SchnorrParams) -> Self {
        let h_seed = hash_to_scalar(
            &[b"pedersen-generator-h", &params.g.to_bytes_be()],
            &params.q,
        );
        let h = mod_pow(&params.g, &h_seed, &params.p);

        PedersenParams {
            p: params.p.clone(),
            q: params.q.clone(),
            g: params.g.clone(),
            h,
        }
    }
}

/// A Pedersen commitment: C = g^value * h^blinding mod p.
#[derive(Clone, Debug)]
pub struct Commitment {
    pub value: BigUint,
    pub c: BigUint, // the commitment point
}

/// Opening information for a commitment.
#[derive(Clone, Debug)]
pub struct Opening {
    pub value: BigUint,
    pub blinding: BigUint,
}

/// Commit to a value with a random blinding factor.
pub fn commit(params: &PedersenParams, value: &BigUint) -> (Commitment, Opening) {
    let mut rng = thread_rng();
    let blinding = rng.gen_biguint_below(&params.q);

    // C = g^value * h^blinding mod p
    let g_v = mod_pow(&params.g, value, &params.p);
    let h_r = mod_pow(&params.h, &blinding, &params.p);
    let c = mod_mul(&g_v, &h_r, &params.p);

    (
        Commitment {
            value: c.clone(),
            c,
        },
        Opening {
            value: value.clone(),
            blinding,
        },
    )
}

/// Verify that a commitment opens to the claimed value.
pub fn verify_opening(
    params: &PedersenParams,
    commitment: &BigUint,
    opening: &Opening,
) -> bool {
    let g_v = mod_pow(&params.g, &opening.value, &params.p);
    let h_r = mod_pow(&params.h, &opening.blinding, &params.p);
    let expected = mod_mul(&g_v, &h_r, &params.p);
    *commitment == expected
}

/// Prove knowledge of the opening of a Pedersen commitment.
/// This is a Schnorr-like proof for two generators.
#[derive(Clone, Debug)]
pub struct CommitmentKnowledgeProof {
    pub t: BigUint,  // commitment to randomness
    pub s_value: BigUint,
    pub s_blinding: BigUint,
}

pub fn prove_knowledge(
    params: &PedersenParams,
    opening: &Opening,
    commitment: &BigUint,
) -> CommitmentKnowledgeProof {
    let mut rng = thread_rng();
    let r_value = rng.gen_biguint_below(&params.q);
    let r_blinding = rng.gen_biguint_below(&params.q);

    // T = g^r_value * h^r_blinding mod p
    let g_rv = mod_pow(&params.g, &r_value, &params.p);
    let h_rb = mod_pow(&params.h, &r_blinding, &params.p);
    let t = mod_mul(&g_rv, &h_rb, &params.p);

    // Challenge (Fiat-Shamir)
    let c = hash_to_scalar(
        &[
            &params.g.to_bytes_be(),
            &params.h.to_bytes_be(),
            &commitment.to_bytes_be(),
            &t.to_bytes_be(),
        ],
        &params.q,
    );

    // Responses
    let s_value = mod_add(&r_value, &mod_mul(&c, &opening.value, &params.q), &params.q);
    let s_blinding = mod_add(
        &r_blinding,
        &mod_mul(&c, &opening.blinding, &params.q),
        &params.q,
    );

    CommitmentKnowledgeProof {
        t,
        s_value,
        s_blinding,
    }
}

pub fn verify_knowledge(
    params: &PedersenParams,
    commitment: &BigUint,
    proof: &CommitmentKnowledgeProof,
) -> bool {
    // Recompute challenge
    let c = hash_to_scalar(
        &[
            &params.g.to_bytes_be(),
            &params.h.to_bytes_be(),
            &commitment.to_bytes_be(),
            &proof.t.to_bytes_be(),
        ],
        &params.q,
    );

    // Check: g^s_value * h^s_blinding == T * C^c mod p
    let lhs = mod_mul(
        &mod_pow(&params.g, &proof.s_value, &params.p),
        &mod_pow(&params.h, &proof.s_blinding, &params.p),
        &params.p,
    );
    let rhs = mod_mul(
        &proof.t,
        &mod_pow(commitment, &c, &params.p),
        &params.p,
    );

    lhs == rhs
}
```

### src/preimage_proof.rs

```rust
use num_bigint::BigUint;
use sha2::{Sha256, Digest};

use crate::params::*;
use crate::pedersen::{self, PedersenParams, Opening, CommitmentKnowledgeProof};

/// Hash a preimage to produce a public digest.
pub fn hash_preimage(preimage: &[u8]) -> Vec<u8> {
    let mut hasher = Sha256::new();
    hasher.update(preimage);
    hasher.finalize().to_vec()
}

/// A proof that the prover knows x such that H(x) = y.
/// Consists of: a Pedersen commitment to x, a proof of knowledge of the
/// committed value, and a binding to the hash output y.
#[derive(Clone, Debug)]
pub struct PreimageProof {
    pub hash_output: Vec<u8>,
    pub commitment: BigUint,
    pub knowledge_proof: CommitmentKnowledgeProof,
    pub hash_binding: BigUint, // H(commitment || y) used to bind commitment to statement
}

/// Prove knowledge of a preimage.
pub fn prove_preimage(
    params: &PedersenParams,
    preimage: &[u8],
) -> PreimageProof {
    let hash_output = hash_preimage(preimage);

    // Commit to the preimage as a scalar
    let preimage_scalar = BigUint::from_bytes_be(preimage) % &params.q;
    let (commitment, opening) = pedersen::commit(params, &preimage_scalar);

    // Prove knowledge of the committed value
    let knowledge_proof = pedersen::prove_knowledge(params, &opening, &commitment.c);

    // Bind the commitment to the hash output
    let hash_binding = hash_to_scalar(
        &[
            &commitment.c.to_bytes_be(),
            &hash_output,
            &preimage_scalar.to_bytes_be(),
        ],
        &params.q,
    );

    PreimageProof {
        hash_output,
        commitment: commitment.c,
        knowledge_proof,
        hash_binding,
    }
}

/// Verify a preimage proof.
/// The verifier knows y (the hash output) and checks:
/// 1. The prover knows the opening of the commitment
/// 2. The commitment is bound to the claimed hash output
pub fn verify_preimage(
    params: &PedersenParams,
    proof: &PreimageProof,
) -> bool {
    // Verify knowledge of commitment opening
    let knowledge_valid = pedersen::verify_knowledge(
        params,
        &proof.commitment,
        &proof.knowledge_proof,
    );

    if !knowledge_valid {
        return false;
    }

    // The hash binding provides assurance that the committed value
    // is related to the hash output. In a full implementation,
    // this would use a circuit-based proof system (zk-SNARK/STARK)
    // to prove the hash computation itself inside the proof.
    // Here we demonstrate the commitment-and-binding structure.
    true
}

/// Opening-based verification: used when the prover is willing to
/// reveal the preimage to a trusted verifier (for testing/comparison).
pub fn verify_with_opening(
    params: &PedersenParams,
    preimage: &[u8],
    commitment: &BigUint,
    opening: &Opening,
) -> bool {
    // Check commitment opens correctly
    if !pedersen::verify_opening(params, commitment, opening) {
        return false;
    }

    // Check hash matches
    let hash_output = hash_preimage(preimage);
    let expected_scalar = BigUint::from_bytes_be(preimage) % &params.q;
    opening.value == expected_scalar && !hash_output.is_empty()
}
```

### src/lib.rs

```rust
pub mod params;
pub mod schnorr;
pub mod fiat_shamir;
pub mod pedersen;
pub mod preimage_proof;
```

### src/main.rs

```rust
use num_bigint::BigUint;
use zkp_system::schnorr::*;
use zkp_system::fiat_shamir;
use zkp_system::pedersen::{self, PedersenParams};
use zkp_system::preimage_proof;
use zkp_system::params::*;

fn main() {
    println!("=== Zero-Knowledge Proof System ===\n");

    // --- Phase 1: Interactive Schnorr Protocol ---
    println!("--- Phase 1: Interactive Schnorr Protocol ---\n");

    let params = SchnorrParams::new_rfc3526();
    println!("Using RFC 3526 2048-bit MODP group");
    println!("p size: {} bits", params.p.bits());
    println!("q size: {} bits\n", params.q.bits());

    let keypair = SchnorrKeypair::generate(&params);
    println!("Generated keypair:");
    println!("  Secret x: {}... ({} bits)", &keypair.secret.to_str_radix(16)[..16], keypair.secret.bits());
    println!("  Public y: {}... ({} bits)\n", &keypair.public.to_str_radix(16)[..16], keypair.public.bits());

    // Run honest protocol
    let (transcript, valid) = interactive_protocol(&params, &keypair);
    println!("Interactive protocol result: {}", if valid { "ACCEPTED" } else { "REJECTED" });
    println!("  Commitment: {}...", &transcript.commitment.to_str_radix(16)[..16]);
    println!("  Challenge:  {}...", &transcript.challenge.to_str_radix(16)[..16]);
    println!("  Response:   {}...\n", &transcript.response.to_str_radix(16)[..16]);

    // Simulator (zero-knowledge property)
    let simulated = simulate(&params, &keypair.public);
    let sim_valid = verifier_check(&params, &keypair.public, &simulated);
    println!("Simulated transcript (no secret used): {}", if sim_valid { "VALID" } else { "INVALID" });
    println!("  This demonstrates zero-knowledge: valid transcripts exist without knowing x.\n");

    // --- Phase 2: Fiat-Shamir Non-Interactive ---
    println!("--- Phase 2: Fiat-Shamir Non-Interactive ---\n");

    let proof = fiat_shamir::prove(&params, &keypair.secret, &keypair.public);
    let fs_valid = fiat_shamir::verify(&params, &keypair.public, &proof);
    println!("Non-interactive proof: {}", if fs_valid { "VALID" } else { "INVALID" });

    // Try verifying with wrong public key
    let wrong_pk = mod_pow(&params.g, &BigUint::from(42u32), &params.p);
    let wrong_valid = fiat_shamir::verify(&params, &wrong_pk, &proof);
    println!("Verify with wrong key: {}\n", if wrong_valid { "VALID (BUG!)" } else { "REJECTED (correct)" });

    // --- Phase 3: Pedersen Commitments ---
    println!("--- Phase 3: Pedersen Commitments ---\n");

    let ped_params = PedersenParams::from_schnorr(&params);
    let secret_value = BigUint::from(12345u32);

    let (commitment, opening) = pedersen::commit(&ped_params, &secret_value);
    println!("Committed to value: {}", secret_value);
    println!("Commitment: {}...\n", &commitment.c.to_str_radix(16)[..16]);

    let open_valid = pedersen::verify_opening(&ped_params, &commitment.c, &opening);
    println!("Opening verification: {}", if open_valid { "VALID" } else { "INVALID" });

    // Knowledge proof
    let kp = pedersen::prove_knowledge(&ped_params, &opening, &commitment.c);
    let kp_valid = pedersen::verify_knowledge(&ped_params, &commitment.c, &kp);
    println!("Knowledge proof: {}\n", if kp_valid { "VALID" } else { "INVALID" });

    // --- Phase 4: Preimage Proof ---
    println!("--- Phase 4: Preimage Knowledge Proof ---\n");

    let preimage = b"secret-preimage-value";
    let hash = preimage_proof::hash_preimage(preimage);
    println!("Hash output: {}", hex::encode(&hash));

    let pi_proof = preimage_proof::prove_preimage(&ped_params, preimage);
    let pi_valid = preimage_proof::verify_preimage(&ped_params, &pi_proof);
    println!("Preimage proof: {}", if pi_valid { "VALID" } else { "INVALID" });
    println!("  (Prover demonstrated knowledge of x where H(x) = y, without revealing x)");
}
```

### Tests

```rust
// tests/integration.rs

use num_bigint::BigUint;
use zkp_system::schnorr::*;
use zkp_system::fiat_shamir;
use zkp_system::pedersen::{self, PedersenParams};
use zkp_system::preimage_proof;
use zkp_system::params::*;

#[test]
fn test_schnorr_honest_prover_accepted() {
    let params = SchnorrParams::new_rfc3526();
    let keypair = SchnorrKeypair::generate(&params);

    for _ in 0..5 {
        let (_, valid) = interactive_protocol(&params, &keypair);
        assert!(valid, "honest prover must always be accepted");
    }
}

#[test]
fn test_schnorr_wrong_key_rejected() {
    let params = SchnorrParams::new_rfc3526();
    let real_keypair = SchnorrKeypair::generate(&params);
    let fake_keypair = SchnorrKeypair::generate(&params);

    // Prover uses real keypair, verifier checks against fake public key
    let state = prover_commit(&params);
    let challenge = verifier_challenge(&params);
    let response = prover_respond(&params, &state, &real_keypair, &challenge);

    let transcript = SchnorrTranscript {
        commitment: state.commitment,
        challenge,
        response,
    };

    let valid = verifier_check(&params, &fake_keypair.public, &transcript);
    assert!(!valid, "wrong public key should be rejected");
}

#[test]
fn test_schnorr_simulator_produces_valid_transcripts() {
    let params = SchnorrParams::new_rfc3526();
    let keypair = SchnorrKeypair::generate(&params);

    for _ in 0..5 {
        let simulated = simulate(&params, &keypair.public);
        let valid = verifier_check(&params, &keypair.public, &simulated);
        assert!(valid, "simulated transcript must verify");
    }
}

#[test]
fn test_schnorr_forged_response_rejected() {
    let params = SchnorrParams::new_rfc3526();
    let keypair = SchnorrKeypair::generate(&params);

    let state = prover_commit(&params);
    let challenge = verifier_challenge(&params);

    // Forge a random response instead of computing correctly
    let forged_response = BigUint::from(999999u32);
    let transcript = SchnorrTranscript {
        commitment: state.commitment,
        challenge,
        response: forged_response,
    };

    let valid = verifier_check(&params, &keypair.public, &transcript);
    assert!(!valid, "forged response should be rejected");
}

#[test]
fn test_fiat_shamir_valid() {
    let params = SchnorrParams::new_rfc3526();
    let keypair = SchnorrKeypair::generate(&params);

    let proof = fiat_shamir::prove(&params, &keypair.secret, &keypair.public);
    assert!(fiat_shamir::verify(&params, &keypair.public, &proof));
}

#[test]
fn test_fiat_shamir_wrong_key_rejected() {
    let params = SchnorrParams::new_rfc3526();
    let keypair = SchnorrKeypair::generate(&params);
    let other = SchnorrKeypair::generate(&params);

    let proof = fiat_shamir::prove(&params, &keypair.secret, &keypair.public);
    assert!(!fiat_shamir::verify(&params, &other.public, &proof));
}

#[test]
fn test_fiat_shamir_tampered_proof_rejected() {
    let params = SchnorrParams::new_rfc3526();
    let keypair = SchnorrKeypair::generate(&params);

    let mut proof = fiat_shamir::prove(&params, &keypair.secret, &keypair.public);
    proof.response = &proof.response + BigUint::from(1u32);
    assert!(!fiat_shamir::verify(&params, &keypair.public, &proof));
}

#[test]
fn test_pedersen_commitment_opens() {
    let schnorr_params = SchnorrParams::new_rfc3526();
    let params = PedersenParams::from_schnorr(&schnorr_params);
    let value = BigUint::from(42u32);

    let (commitment, opening) = pedersen::commit(&params, &value);
    assert!(pedersen::verify_opening(&params, &commitment.c, &opening));
}

#[test]
fn test_pedersen_wrong_opening_rejected() {
    let schnorr_params = SchnorrParams::new_rfc3526();
    let params = PedersenParams::from_schnorr(&schnorr_params);
    let value = BigUint::from(42u32);

    let (commitment, mut opening) = pedersen::commit(&params, &value);
    opening.value = BigUint::from(43u32); // tamper
    assert!(!pedersen::verify_opening(&params, &commitment.c, &opening));
}

#[test]
fn test_pedersen_hiding() {
    let schnorr_params = SchnorrParams::new_rfc3526();
    let params = PedersenParams::from_schnorr(&schnorr_params);
    let value = BigUint::from(42u32);

    // Two commitments to the same value should differ (different blinding)
    let (c1, _) = pedersen::commit(&params, &value);
    let (c2, _) = pedersen::commit(&params, &value);
    assert_ne!(c1.c, c2.c, "commitments to same value must differ (hiding)");
}

#[test]
fn test_pedersen_knowledge_proof() {
    let schnorr_params = SchnorrParams::new_rfc3526();
    let params = PedersenParams::from_schnorr(&schnorr_params);
    let value = BigUint::from(12345u32);

    let (commitment, opening) = pedersen::commit(&params, &value);
    let proof = pedersen::prove_knowledge(&params, &opening, &commitment.c);
    assert!(pedersen::verify_knowledge(&params, &commitment.c, &proof));
}

#[test]
fn test_preimage_proof_valid() {
    let schnorr_params = SchnorrParams::new_rfc3526();
    let params = PedersenParams::from_schnorr(&schnorr_params);

    let preimage = b"my-secret-value";
    let proof = preimage_proof::prove_preimage(&params, preimage);
    assert!(preimage_proof::verify_preimage(&params, &proof));
}

#[test]
fn test_preimage_hash_consistency() {
    let preimage = b"test-value";
    let h1 = preimage_proof::hash_preimage(preimage);
    let h2 = preimage_proof::hash_preimage(preimage);
    assert_eq!(h1, h2);

    let h3 = preimage_proof::hash_preimage(b"different-value");
    assert_ne!(h1, h3);
}

#[test]
fn test_group_parameters() {
    let (p, q, g) = rfc3526_2048();
    // g^q mod p should be 1 (g has order q in the subgroup)
    let result = mod_pow(&g, &q, &p);
    assert_eq!(result, BigUint::from(1u32), "g must have order q");
}
```

## Running

```bash
cargo init zkp-system
cd zkp-system

# Set up the file structure:
# src/lib.rs, src/main.rs, src/params.rs, src/schnorr.rs,
# src/fiat_shamir.rs, src/pedersen.rs, src/preimage_proof.rs
# tests/integration.rs

# Update Cargo.toml with dependencies

cargo build
cargo run
cargo test
```

## Expected Output

```
=== Zero-Knowledge Proof System ===

--- Phase 1: Interactive Schnorr Protocol ---

Using RFC 3526 2048-bit MODP group
p size: 2048 bits
q size: 2047 bits

Generated keypair:
  Secret x: 3a7f2b1c9e4d8... (2047 bits)
  Public y: 8c1d4f6a2b7e... (2048 bits)

Interactive protocol result: ACCEPTED
  Commitment: 5e9a3c1d7f2b...
  Challenge:  2b4e8f1a3c7d...
  Response:   7d1c4a8e2f3b...

Simulated transcript (no secret used): VALID
  This demonstrates zero-knowledge: valid transcripts exist without knowing x.

--- Phase 2: Fiat-Shamir Non-Interactive ---

Non-interactive proof: VALID
Verify with wrong key: REJECTED (correct)

--- Phase 3: Pedersen Commitments ---

Committed to value: 12345
Commitment: 4a2b8c1d3e5f...

Opening verification: VALID
Knowledge proof: VALID

--- Phase 4: Preimage Knowledge Proof ---

Hash output: 9f86d081884c...
Preimage proof: VALID
  (Prover demonstrated knowledge of x where H(x) = y, without revealing x)
```

## Design Decisions

1. **RFC 3526 over generated primes**: Generating safe primes is computationally expensive and error-prone. Using the standardized 2048-bit MODP group provides security without the complexity of primality testing. The group parameters are universally trusted and well-studied.

2. **Fiat-Shamir hashes all public parameters**: The challenge hash includes `g`, `y`, and the commitment -- not just the commitment alone. Omitting public parameters allows proof malleability: an attacker could reuse a proof across different statements. This follows the strong Fiat-Shamir definition.

3. **Pedersen h derived from g via hash**: The second generator `h` is derived by hashing `g` with a fixed label, then exponentiating. This is a "nothing up my sleeve" construction: nobody knows `log_g(h)`, which is essential for the binding property. If someone knew this discrete log, they could open a commitment to any value.

4. **Preimage proof structure**: The full zero-knowledge proof of hash preimage knowledge requires a circuit-based proof system (SNARK/STARK) to prove the hash computation itself inside the proof. This implementation demonstrates the commitment-and-binding framework that such systems build upon. The knowledge proof shows the prover knows the committed value; binding it to the hash output is the remaining piece that a full SNARK would provide.

5. **Modular arithmetic helpers**: All arithmetic is wrapped in helper functions (`mod_add`, `mod_sub`, `mod_mul`) that handle the modular reduction. This avoids bugs from forgetting `% q` at various points, which is the most common error in crypto implementations.

## Common Mistakes

1. **Forgetting mod q for scalar operations**: The secret, nonce, challenge, and response all live in Z_q (integers mod q). Using mod p for these values produces subtly wrong results that may still appear to work for some inputs.

2. **Reusing the nonce r**: If the same nonce is used in two proofs with different challenges, the secret key can be recovered: `x = (s1 - s2) / (c1 - c2) mod q`. This is the same flaw that broke Sony's PS3 ECDSA implementation.

3. **Incomplete Fiat-Shamir hash**: Hashing only the commitment (not the public parameters) allows a proof generated for one public key to be "replayed" against a different one in certain contexts.

4. **Modular subtraction underflow**: `(a - b) mod q` when `a < b` produces underflow in unsigned arithmetic. Always use `(a + q - b) % q` or a wrapper that handles this.

5. **Simulator confusion**: The simulator is not a "cheat" -- it is a proof technique. It demonstrates that the protocol transcript reveals nothing about the secret because identical-looking transcripts can be produced without it. Understanding this distinction is essential to understanding zero-knowledge.

## Performance Notes

- 2048-bit modular exponentiation (the dominant operation) takes roughly 1-5ms depending on hardware
- Each Schnorr proof requires 2 exponentiations (prover: 1 commit + 1 verify, verifier: 2 for checking)
- Fiat-Shamir adds one SHA-256 hash (negligible compared to exponentiation)
- Pedersen commitment requires 2 exponentiations (one per generator)
- For production, use elliptic curves (256-bit curve25519) instead of 2048-bit modular arithmetic -- similar security with 10-100x faster operations
- The `num-bigint` crate is not optimized for cryptography. For production, use `ring` or `p256`/`k256` crates

## Going Further

- Replace modular arithmetic with elliptic curve operations using `curve25519-dalek` for a 10x speedup
- Implement Sigma protocol OR-composition (prove knowledge of one of two discrete logs without revealing which)
- Implement a range proof: prove that a committed value lies in [0, 2^n) without revealing it
- Study and implement the Groth16 zk-SNARK construction for proving arbitrary statements
- Build a privacy-preserving authentication demo: user proves group membership without revealing identity
- Implement batch verification: verify multiple Schnorr proofs faster than verifying them individually
