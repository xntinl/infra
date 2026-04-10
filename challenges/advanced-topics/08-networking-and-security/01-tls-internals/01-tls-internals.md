<!--
type: reference
difficulty: advanced
section: [08-networking-and-security]
concepts: [tls-1.3, handshake, hkdf, aead, session-tickets, 0-rtt, mutual-tls, certificate-validation, cipher-suites]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [cryptographic-primitives, key-exchange-and-pki, tcp-ip-fundamentals]
papers: [RFC 8446 — The Transport Layer Security (TLS) Protocol Version 1.3, Bhargavan et al. 2016 — Transcript Collision Attacks: Breaking Authentication in TLS, IKE, and SSH]
industry_use: [nginx, envoy, caddy, rustls, let's-encrypt, cloudflare-workers]
language_contrast: high
-->

# TLS Internals

> The security of TLS 1.3 rests entirely on the secrecy of ephemeral private keys and the
> integrity of the transcript hash — if either is compromised, the protocol provides nothing.

## Mental Model

TLS 1.3 is a key-agreement protocol wrapped around a record encryption layer. The entire
handshake serves one purpose: allow two parties who may have never communicated to derive
a shared secret that no passive observer can compute, authenticated so that an active
attacker cannot substitute their own keys. Every other complexity — certificates, session
resumption, ALPN, SNI — is layered on top of that core.

The most important mental shift from TLS 1.2 to TLS 1.3 is that the handshake was
redesigned around a *transcript hash*. Every message in the handshake is fed into a running
hash; the final keys are derived from that hash combined with the shared secret. This means
that modifying any handshake message — even one sent before authentication — causes the
derived keys to diverge and the connection to fail. There is no longer a separate "Finished"
MAC over a subset of messages; the transcript hash covers everything, and forward secrecy
is mandatory because ephemeral key exchange (ECDHE) is the only allowed mode.

The second critical insight is that TLS 1.3 reduced the cipher suite list to three
AEAD constructions (AES-128-GCM, AES-256-GCM, ChaCha20-Poly1305) and two hash functions
(SHA-256, SHA-384). There are no more negotiable MAC algorithms, no more CBC modes, no
more RSA key exchange. An attacker who can negotiate a weak cipher suite in TLS 1.2
cannot do so in TLS 1.3. Configuration complexity is the primary source of TLS
vulnerabilities; TLS 1.3 eliminates most of the configuration surface.

0-RTT (zero round-trip time resumption) is where TLS 1.3 introduces its one major security
regression. 0-RTT data is encrypted under a key derived from a previous session ticket —
a key the server already knows. This means the server can decrypt the first flight of
application data before completing the handshake. The cost: 0-RTT data has no forward
secrecy relative to the ticket key, and it is vulnerable to replay attacks. An attacker
who captures a 0-RTT packet can replay it against the same server. 0-RTT is appropriate
only for idempotent, replay-safe operations (GET requests), never for state-mutating ones.

## Core Concepts

### TLS 1.3 Handshake Step by Step

```
Client                                        Server
------                                        ------
ClientHello
  + key_share (X25519 public key)
  + supported_versions: TLS 1.3
  + signature_algorithms
  + server_name (SNI)
                          ----------------->
                                              ServerHello
                                                + key_share (server's X25519 public key)
                                              {EncryptedExtensions}
                                              {CertificateRequest}   -- only for mTLS
                                              {Certificate}
                                              {CertificateVerify}
                                              {Finished}
                          <-----------------
{Certificate}             -- only for mTLS
{CertificateVerify}       -- only for mTLS
{Finished}
                          ----------------->
[Application Data]        <--------------->  [Application Data]
```

Items in `{}` are encrypted under the handshake traffic key. Items in `[]` are encrypted
under the application traffic key. The server's first flight is already partially
encrypted — there is no more ServerHelloDone in the clear.

### HKDF Key Schedule

TLS 1.3 derives all session keys using HKDF (HMAC-based Key Derivation Function, RFC 5869).
The key schedule has three stages:

1. **Early Secret**: derived from the pre-shared key (PSK) or zeros if no PSK. Produces
   the early traffic secret used for 0-RTT data.
2. **Handshake Secret**: derived by mixing the ECDHE shared secret into the early secret.
   Produces `client_handshake_traffic_secret` and `server_handshake_traffic_secret`.
3. **Master Secret**: derived from the handshake secret and zeros (no more key material
   is injected). Produces `client_application_traffic_secret_0` and
   `server_application_traffic_secret_0`.

Each traffic secret expands into an actual key and IV via:
```
key = HKDF-Expand-Label(secret, "key", "", key_length)
iv  = HKDF-Expand-Label(secret, "iv",  "", iv_length)
```

The nonce for each AEAD record is the IV XOR'd with the 64-bit sequence number. This
ensures nonces are never reused within a session.

### Certificate Validation Chain

Certificate validation in TLS is chain-of-trust verification:

1. The server presents a certificate (or chain) in the `Certificate` message.
2. The client verifies that the leaf certificate's Subject Alternative Name (SAN) matches
   the hostname it connected to (not the Common Name — CN is deprecated for hostname
   verification since RFC 2818).
3. The client walks up the chain, verifying each certificate's signature against the
   public key in the parent certificate.
4. The root must be in the client's trust store.
5. The client checks validity periods and revocation status (via OCSP or CRL).

The `CertificateVerify` message is separate: it is a signature over the entire transcript
hash made with the server's private key. This proves the server possesses the private key
corresponding to the certificate — otherwise a man-in-the-middle could forward a
legitimate certificate from a different server.

### Session Tickets and Resumption

Session tickets allow a client to resume a session without a full handshake. The server
generates a `NewSessionTicket` message after the handshake completes. The ticket contains
the resumption master secret encrypted under a server-side ticket key (opaque to the
client). On the next connection, the client sends the ticket in the `pre_shared_key`
extension of its ClientHello. If valid, the server can skip certificate authentication
entirely and derive a new session key from the ticket's PSK.

Security implications: ticket keys must rotate frequently (typically every 24 hours or
less). Compromising a ticket key retroactively compromises all sessions resumed with it.
This is why OCSP stapling and short-lived certificates do not help if your ticket key
rotation is broken.

## Implementation: Go

```go
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// buildServerTLSConfig returns a TLS 1.3-only server config.
// Forcing TLS 1.3 eliminates entire CVE classes: BEAST, POODLE, DROWN, ROBOT.
func buildServerTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load keypair: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		// TLS 1.3 cipher suite selection is not user-configurable in Go's crypto/tls.
		// The three suites (AES-128-GCM, AES-256-GCM, ChaCha20-Poly1305) are always
		// available; Go selects AES-GCM when AES-NI is present, ChaCha20 otherwise.
		//
		// WRONG: setting CipherSuites on a TLS 1.3 config has no effect —
		// Go ignores it for TLS 1.3. Do not cargo-cult TLS 1.2 cipher suite lists
		// into TLS 1.3 configs.
	}, nil
}

// buildClientTLSConfig returns a TLS 1.3-only client config with certificate pinning.
// Certificate pinning prevents attacks that rely on rogue CA certificates.
func buildClientTLSConfig(trustedCertFile string) (*tls.Config, error) {
	pool := x509.NewCertPool()
	certPEM, err := os.ReadFile(trustedCertFile)
	if err != nil {
		return nil, fmt.Errorf("read cert: %w", err)
	}
	if !pool.AppendCertsFromPEM(certPEM) {
		return nil, fmt.Errorf("no valid PEM certificate found in %s", trustedCertFile)
	}

	return &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS13,
		// VerifyPeerCertificate runs after the standard chain validation.
		// Use it to enforce additional constraints: EKU, SPKI pinning, CT proofs.
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			// SPKI pinning: verify the leaf cert's public key matches a known fingerprint.
			// This defeats attacks using a legitimate but unexpected CA.
			// In production: load expected pins from configuration, not code.
			_ = rawCerts
			_ = verifiedChains
			return nil
		},
	}, nil
}

// mutualTLSServer starts an mTLS server that requires a valid client certificate.
// mTLS is the correct choice for service-to-service authentication in a zero-trust network.
func mutualTLSServer(serverCert, serverKey, clientCAFile string) error {
	clientCAs := x509.NewCertPool()
	caPEM, err := os.ReadFile(clientCAFile)
	if err != nil {
		return fmt.Errorf("read client CA: %w", err)
	}
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("no valid PEM CA cert in %s", clientCAFile)
	}

	cert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	if err != nil {
		return fmt.Errorf("load server keypair: %w", err)
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    clientCAs,
		// RequireAndVerifyClientCert: both present AND valid against ClientCAs.
		// RequireAnyClientCert would accept self-signed certs — almost always wrong.
		ClientAuth: tls.RequireAndVerifyClientCert,
		MinVersion: tls.VersionTLS13,
	}

	ln, err := tls.Listen("tcp", ":8443", cfg)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()

	log.Println("mTLS server listening on :8443")
	for {
		conn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("accept: %w", err)
		}
		go handleMTLSConn(conn)
	}
}

func handleMTLSConn(conn net.Conn) {
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return
	}

	// Force the handshake so we can inspect the peer certificate.
	// net.Conn.Read would also trigger it lazily, but explicit is better here.
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("handshake failed: %v", err)
		return
	}

	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) > 0 {
		peer := state.PeerCertificates[0]
		log.Printf("client CN=%s, SANs=%v", peer.Subject.CommonName, peer.DNSNames)
	}

	io.Copy(conn, conn) // echo server
}

// inspectHandshake dials a server and logs TLS handshake details.
// Useful for debugging cipher negotiation, session resumption, and certificate chains.
func inspectHandshake(addr string) error {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	s := conn.ConnectionState()
	fmt.Printf("TLS version:     0x%04x (1.3 = 0x0304)\n", s.Version)
	fmt.Printf("Cipher suite:    %s\n", tls.CipherSuiteName(s.CipherSuite))
	fmt.Printf("Server name:     %s\n", s.ServerName)
	fmt.Printf("Session resumed: %v\n", s.DidResume)
	fmt.Printf("OCSP stapled:    %v\n", len(s.OCSPResponse) > 0)

	if len(s.PeerCertificates) > 0 {
		leaf := s.PeerCertificates[0]
		fmt.Printf("Leaf cert:       CN=%s, expires=%s\n",
			leaf.Subject.CommonName, leaf.NotAfter.Format(time.RFC3339))
	}

	return nil
}

// VULNERABLE: demonstrates what NOT to do.
// InsecureTLSConfig disables certificate verification. NEVER use in production.
// This makes TLS useless — you encrypt traffic but authenticate nobody.
func insecureTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true, // VULNERABLE: defeats the entire purpose of TLS
	}
}

// zero0RTTRisks documents the replay attack surface of 0-RTT data.
// In Go's crypto/tls, 0-RTT is not yet exposed in the public API (as of Go 1.22).
// Use this function as documentation of the invariants you must enforce if you
// implement 0-RTT at a lower level (e.g., via quic-go).
//
// Rules for 0-RTT safety:
// 1. Only enable for idempotent operations (GET, HEAD, OPTIONS).
// 2. Never enable for state-mutating operations (POST, PUT, DELETE, financial transactions).
// 3. Implement server-side replay detection using a nonce cache (bounded by the
//    anti-replay window, typically the ticket lifetime).
// 4. Rate-limit 0-RTT acceptance per session ticket to prevent amplification.
func zero0RTTRisks() {
	// This function intentionally has no implementation.
	// It exists as a forcing function for code review: if you add 0-RTT support,
	// you must justify the replay safety of every endpoint that accepts early data.
}

// httpClientWithPinnedCerts returns an http.Client that validates TLS and pins to
// the provided CA certificate. Suitable for service-to-service clients.
func httpClientWithPinnedCerts(caCertFile string) (*http.Client, error) {
	cfg, err := buildClientTLSConfig(caCertFile)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: cfg,
			// Disable HTTP/1.1 keep-alive connection reuse so we can observe
			// per-connection handshake state in tests.
			DisableKeepAlives: false,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		Timeout: 30 * time.Second,
	}, nil
}

func main() {
	if err := inspectHandshake("google.com:443"); err != nil {
		log.Fatalf("inspect: %v", err)
	}
}
```

### Go-specific considerations

**`crypto/tls` is the right default.** Go's standard library TLS is maintained by the Go
security team, reviewed for FIPS 140 compliance, and updated promptly when vulnerabilities
are disclosed. Do not reach for OpenSSL bindings or third-party TLS implementations unless
you have an explicit regulatory requirement.

**FIPS 140-2/3 compliance.** If you need FIPS-validated cryptography, use
`golang.org/x/crypto/internal/boring` (the BoringCrypto integration). This is enabled by
compiling with `GOEXPERIMENT=boringcrypto` on a supported platform. The standard
`crypto/tls` is not FIPS-validated but uses algorithms that happen to be FIPS-approved.
FIPS compliance is about the *implementation* being validated, not the algorithm name.

**TLS 1.3 cipher suites are not configurable.** Setting `CipherSuites` in `tls.Config`
affects only TLS 1.2. For TLS 1.3, Go always offers all three AEAD suites and selects the
best one based on hardware. This is the correct behavior — do not try to override it.

**`VerifyPeerCertificate` vs `VerifyConnection`.** Use `VerifyPeerCertificate` for
additional leaf-certificate checks (SPKI pinning, custom EKU). Use `VerifyConnection` when
you need access to the full `ConnectionState` (cipher suite, negotiated protocol) in your
custom verification logic.

**Session ticket rotation.** Go's `crypto/tls` generates ticket keys automatically at
`tls.Config` creation time. For multi-process deployments, you must share ticket keys
explicitly via `SessionTicketKey` or `SetSessionTicketKeys`. Otherwise each process has
different keys and resumption fails across a load-balanced pool.

## Implementation: Rust

```rust
use rustls::{
    Certificate, ClientConfig, OwnedTrustAnchor, PrivateKey, RootCertStore, ServerConfig,
};
use rustls_pemfile::{certs, pkcs8_private_keys};
use std::fs::File;
use std::io::{self, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::Arc;

/// Load a certificate chain from a PEM file.
fn load_certs(path: &str) -> io::Result<Vec<Certificate>> {
    let f = File::open(path)?;
    let mut reader = BufReader::new(f);
    let raw = certs(&mut reader).map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "bad cert"))?;
    Ok(raw.into_iter().map(Certificate).collect())
}

/// Load a PKCS#8 private key from a PEM file.
fn load_key(path: &str) -> io::Result<PrivateKey> {
    let f = File::open(path)?;
    let mut reader = BufReader::new(f);
    let keys = pkcs8_private_keys(&mut reader)
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "bad key"))?;
    keys.into_iter()
        .next()
        .map(PrivateKey)
        .ok_or_else(|| io::Error::new(io::ErrorKind::NotFound, "no private key"))
}

/// Build a TLS 1.3-only server config.
/// rustls defaults to TLS 1.3 when available; explicitly restricting to 1.3 removes
/// any chance of negotiating to the weaker TLS 1.2 if a client insists.
fn build_server_config(cert_path: &str, key_path: &str) -> Arc<ServerConfig> {
    let certs = load_certs(cert_path).expect("load certs");
    let key = load_key(key_path).expect("load key");

    // rustls::ServerConfig::builder() uses the safe builder pattern:
    // you cannot construct a config without specifying cipher suites and key exchange.
    // This prevents "I'll just use defaults" mistakes.
    let config = ServerConfig::builder()
        .with_safe_defaults()         // TLS 1.2+, safe cipher suites only
        .with_no_client_auth()        // change to with_client_auth_required for mTLS
        .with_single_cert(certs, key)
        .expect("build server config");

    Arc::new(config)
}

/// Build a TLS client config that validates against a custom CA bundle.
fn build_client_config(ca_cert_path: &str) -> Arc<ClientConfig> {
    let mut root_store = RootCertStore::empty();

    let f = File::open(ca_cert_path).expect("open CA cert");
    let mut reader = BufReader::new(f);
    let ca_certs = certs(&mut reader).expect("parse CA certs");
    for cert in ca_certs {
        root_store
            .add(&Certificate(cert))
            .expect("add CA cert");
    }

    // Alternatively, load the system trust store:
    // root_store.add_trust_anchors(webpki_roots::TLS_SERVER_ROOTS.iter().map(|ta| {
    //     OwnedTrustAnchor::from_subject_spki_name_constraints(ta.subject, ta.spki, ta.name_constraints)
    // }));

    let config = ClientConfig::builder()
        .with_safe_defaults()
        .with_root_certificates(root_store)
        .with_no_client_auth();

    Arc::new(config)
}

/// Simple echo server demonstrating rustls ServerConnection usage.
fn run_tls_echo_server(cert_path: &str, key_path: &str) -> io::Result<()> {
    let config = build_server_config(cert_path, key_path);
    let listener = TcpListener::bind("127.0.0.1:8443")?;
    eprintln!("TLS echo server on :8443");

    for stream in listener.incoming() {
        let stream = stream?;
        let config = Arc::clone(&config);
        std::thread::spawn(move || {
            if let Err(e) = handle_tls_conn(stream, config) {
                eprintln!("connection error: {e}");
            }
        });
    }
    Ok(())
}

fn handle_tls_conn(tcp: TcpStream, config: Arc<ServerConfig>) -> io::Result<()> {
    let mut conn = rustls::ServerConnection::new(Arc::clone(&config))
        .map_err(|e| io::Error::new(io::ErrorKind::Other, e))?;

    let mut tls = rustls::Stream::new(&mut conn, &mut tcp.try_clone()?);
    let mut buf = [0u8; 4096];

    // Explicit handshake: rustls is lazy by default.
    // Calling complete_io drives the handshake to completion before reading data.
    tls.conn.complete_io(&mut tls.sock)?;

    loop {
        match tls.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => tls.write_all(&buf[..n])?,
            Err(e) if e.kind() == io::ErrorKind::WouldBlock => break,
            Err(e) => return Err(e),
        }
    }
    Ok(())
}

fn main() {
    // In a real program, pass cert/key paths via args or environment.
    println!("TLS echo server example — requires cert.pem and key.pem");
    println!("Generate test certs with: mkcert localhost");
}
```

### Rust-specific considerations

**rustls is the correct choice for new Rust code.** It is memory-safe (no unsafe in the
core library), passes the TLS-Anvil test suite, and has a clean API that makes insecure
configurations structurally impossible. OpenSSL bindings (`openssl` crate) exist for
compatibility with legacy systems but bring the full surface of OpenSSL's CVE history.

**`ring` vs `aws-lc-rs` as the crypto backend.** rustls supports both. `ring` is
battle-tested and widely deployed. `aws-lc-rs` provides FIPS-validated algorithms via
AWS-LC. For FIPS requirements, use the `rustls-fips` feature flag.

**The builder pattern enforces correct configuration.** `ServerConfig::builder()` requires
you to specify cipher suites and key exchange explicitly — or opt into `with_safe_defaults()`
which gives you the TLS 1.3 suite. You cannot accidentally produce a config with no
cipher suites.

**No session ticket key rotation API.** Unlike Go's `SetSessionTicketKeys`, rustls does
not expose a rotation API in the stable interface. Ticket key rotation requires restarting
the server or using a custom `rustls::server::StoresServerSessions` implementation.

**Memory safety in TLS.** Heartbleed was possible because OpenSSL parsed attacker-controlled
lengths into `memcpy` calls. rustls processes all input via Rust's safe slice operations;
this class of vulnerability is structurally prevented. This is not theoretical — it is the
primary reason Cloudflare chose rustls for Quiche (their QUIC implementation).

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Default TLS library | `crypto/tls` (stdlib) | `rustls` (external crate) |
| FIPS 140 support | BoringCrypto (`GOEXPERIMENT=boringcrypto`) | `rustls-fips` + `aws-lc-rs` |
| TLS 1.3 cipher selection | Automatic (no user config) | `with_safe_defaults()` or explicit |
| mTLS configuration | `tls.Config.ClientAuth` | `with_client_auth_required(...)` |
| Session ticket rotation | `SetSessionTicketKeys()` | Custom `StoresServerSessions` |
| Certificate parsing | `crypto/x509` (stdlib) | `rustls-pemfile` + `webpki` |
| Certificate pinning | `VerifyPeerCertificate` hook | Custom `ServerCertVerifier` trait |
| 0-RTT support | Not in stdlib (quic-go only) | Via `quinn`/`quiche` |
| Memory safety | GC prevents use-after-free | Ownership prevents use-after-free |
| OpenSSL interop | `crypto/tls` only (no OpenSSL) | `openssl` crate available |
| Production adoption | etcd, Kubernetes, Caddy | Cloudflare (quiche), Firefox (NSS→rustls) |

## Production War Stories

**Heartbleed (CVE-2014-0160, OpenSSL 1.0.1–1.0.1f, April 2014).** The TLS heartbeat
extension allowed a client to request up to 64KB of server memory in a response. OpenSSL
trusted the client-supplied length without bounds-checking. An attacker could read private
keys, session tokens, and user passwords from server memory with a 2-byte malformed packet.
The patch was one line: check that the requested length does not exceed the actual payload
length. The lesson: never trust attacker-controlled lengths in memory operations.

**BEAST attack (TLS 1.0, 2011).** CBC mode in TLS 1.0 used the last ciphertext block of
the previous record as the IV for the next record — predictable IVs in a chosen-plaintext
attack allow block boundary guessing. BEAST proved this was practically exploitable in a
browser. The fix was TLS 1.1 (random IVs) and ultimately TLS 1.3 (CBC mode removed
entirely). The lesson: "it's unlikely to be exploited in practice" is not a security
argument.

**Lucky13 (TLS 1.2, 2013).** HMAC-then-pad (the MAC-then-Encrypt scheme used by TLS
1.2's CBC modes) leaked timing information: records with longer padding took slightly more
time to validate. An attacker with a timing oracle could recover plaintexts. The fix was
encrypt-then-MAC (RFC 7366) and constant-time MAC verification. TLS 1.3 addresses this
by using AEAD exclusively. The lesson: timing is a side channel even in memory-safe code.

**Cloudflare TLS 1.3 Rollout (2016).** Cloudflare was the first major CDN to deploy TLS
1.3 at scale, handling millions of TLS handshakes per second. Their engineers wrote the
original `0-RTT` implementation for NGINX and discovered several edge cases: middleboxes
that rejected ClientHello messages containing the `supported_versions` extension (because
they expected TLS 1.2 format), and the anti-replay cache running out of memory under
coordinated 0-RTT replay attacks. The result was the TLS 1.3 anti-replay specification
that became part of RFC 8446. The lesson: deploying a new protocol version at scale always
discovers implementation issues that draft testing misses.

## Security Analysis

**Threat model.** TLS protects against a network attacker who can observe, modify, delay,
or inject traffic between two endpoints. It does not protect against an attacker who has
compromised an endpoint (endpoint security is out of scope) or against a rogue CA that the
client's trust store includes.

**Forward secrecy.** TLS 1.3 mandates ECDHE key exchange. If a server's long-term private
key is compromised in the future, past session traffic remains confidential because the
session keys were derived from ephemeral ECDHE values. TLS 1.2 with RSA key exchange has
no forward secrecy — a future key compromise decrypts all recorded past traffic.

**0-RTT replay surface.** Any endpoint accepting 0-RTT data must treat it as potentially
replayed. Safe pattern: use 0-RTT only for idempotent reads. Unsafe pattern: process a
financial transaction in 0-RTT. The anti-replay mechanism (single-use ticket tokens with
a server-side nonce cache) adds latency and complexity; most deployments disable 0-RTT for
this reason.

**Certificate revocation gap.** OCSP has a fundamental latency problem: by the time a CA
marks a certificate revoked and a client fetches the updated status, an attacker may have
already used the compromised key. OCSP stapling reduces this gap: the server staples a
recent OCSP response (signed by the CA) to the TLS handshake, eliminating the client-side
OCSP fetch latency. Certificate Transparency logs provide a separate audit mechanism but
do not prevent use of revoked certificates.

**SNI leaks the hostname in plaintext.** Even in TLS 1.3, the ClientHello (including SNI)
is sent in cleartext. An observer knows which host you are connecting to even though they
cannot read the content. Encrypted Client Hello (ECH, RFC draft) addresses this by
encrypting the inner ClientHello under the server's public key, obtained from DNS. ECH is
being deployed gradually (Cloudflare, Firefox) as of 2024.

## Common Pitfalls

1. **`InsecureSkipVerify: true` in production code.** This is the most common TLS
   misconfiguration. It makes encryption work but authentication meaningless. Any
   man-in-the-middle can present any certificate and the client will accept it. Use a
   custom CA cert pool for internal services instead.

2. **Sharing a `tls.Config` across goroutines without copying.** `tls.Config` contains
   mutable state (session ticket keys, session cache). If you modify a shared config after
   handing it to a listener, you have a data race. Always `config.Clone()` before modifying.

3. **Not validating SAN (Subject Alternative Names).** Checking only `Subject.CommonName`
   for hostname validation has been incorrect since RFC 2818 (2000) and is rejected by
   modern TLS stacks. `crypto/tls` validates SAN automatically; custom `VerifyConnection`
   code must use `x509.Certificate.VerifyHostname`.

4. **Static session ticket keys in multi-instance deployments.** If you hardcode a session
   ticket key, it never rotates, and a key compromise retroactively decrypts all resumed
   sessions. Use `SetSessionTicketKeys` with a rotating key (fetch from a shared secret
   store, rotate every 24h).

5. **Accepting 0-RTT data for state-mutating endpoints.** This is a replay attack waiting
   to happen. If you implement 0-RTT at the application layer (e.g., via quic-go), you
   must maintain a per-session-ticket nonce cache on the server side and reject duplicate
   nonces within the anti-replay window.

## Exercises

**Exercise 1** (30 min): Write a Go program that connects to `https://tlsv1-2.badssl.com`
and to `https://tls13.1.1.1.1.cloudflare-dns.com`. Use `inspectHandshake` logic to print
the TLS version, cipher suite, and whether session resumption occurred on the second
connection to the same host. Verify you can force a connection failure by setting
`MaxVersion: tls.VersionTLS12` on the 1.1.1.1 client.

**Exercise 2** (2–4h): Implement a mutual TLS server and client in Go. The server requires
client authentication; the client presents a certificate issued by a private CA you create
with `openssl`. Write a test that verifies: (a) a client with a valid cert connects
successfully, (b) a client with a cert from a different CA is rejected, (c) a client with
no cert is rejected. Use `httptest.NewUnstartedServer` to avoid network dependencies.

**Exercise 3** (4–8h): Re-implement exercise 2 in Rust using `rustls`. Add SPKI pinning:
the server extracts the client certificate's SubjectPublicKeyInfo hash and rejects any
client whose SPKI hash is not in a hardcoded allowlist. Write the same three test cases.
Compare the Rust and Go implementations: where does each make the secure choice easy and
where does it require extra work?

**Exercise 4** (8–15h): Implement a TLS session ticket rotation system for a multi-process
Go service. Use Redis to store and distribute the current and previous ticket keys
(retain the previous key to allow in-flight resumption during rotation). Implement a
background goroutine that rotates the key every 24 hours and updates `crypto/tls.Config`
via `SetSessionTicketKeys`. Write a test that starts two server processes with shared
Redis, establishes a connection, rotates the key, and verifies session resumption still
works with the old key. Verify that resumption fails with a forged ticket.

## Further Reading

### Foundational Papers

- RFC 8446 — The Transport Layer Security (TLS) Protocol Version 1.3 (Rescorla, 2018).
  The specification itself. Appendix D (backward compatibility) and Appendix E (security
  analysis) are required reading for anyone deploying TLS 1.3.
- Bhargavan et al. — "Transcript Collision Attacks: Breaking Authentication in TLS, IKE,
  and SSH" (NDSS 2016). Explains why the TLS 1.3 transcript hash must cover all messages.
- Jager, Schwenk, Somorovsky — "On the Security of TLS 1.3 and QUIC Against Weaknesses in
  DTLS 1.2" (CCS 2015). Security analysis that informed the final TLS 1.3 design.

### Books

- Ivan Ristic, "Bulletproof TLS and PKI" (2nd edition, Feisty Duck, 2022) — the most
  complete practical reference on TLS configuration, certificate management, and common
  deployment failures. Updated for TLS 1.3 and Let's Encrypt.
- Dan Boneh, Victor Shoup, "A Graduate Course in Applied Cryptography" (freely available)
  — chapters 10–11 cover authenticated key exchange and TLS.

### Production Code to Read

- `crypto/tls` (Go stdlib, `src/crypto/tls/`) — particularly `handshake_server_tls13.go`
  and `conn.go`. Reading the HKDF key schedule implementation (`key_schedule.go`) is the
  fastest way to make RFC 8446 concrete.
- `rustls/rustls/src/` — `tls13/` directory for the TLS 1.3 handshake state machine.
  The state machine design in Rust is instructive: each handshake state is a distinct type,
  making invalid transitions unrepresentable.
- Cloudflare's `quiche` — `src/tls.rs` uses BoringSSL under the hood but the interface
  design shows how to integrate TLS with a QUIC implementation.

### Security Advisories / CVEs to Study

- CVE-2014-0160 (Heartbleed) — bounds checking failure in TLS heartbeat extension.
- CVE-2016-0800 (DROWN) — SSLv2 cross-protocol attack on RSA key exchange.
- CVE-2019-1559 (0-RTT Padding Oracle) — timing oracle in OpenSSL's 0-RTT processing.
- CVE-2021-3449 (OpenSSL NULL pointer dereference) — malformed CertificateRequest in
  TLS 1.2 renegotiation.
