# 38. Build a TLS 1.3 Handshake

**Difficulty**: Insane

## The Challenge

Transport Layer Security 1.3 is the protocol that protects virtually all internet traffic. Every
HTTPS connection, every API call, every WebSocket upgrade begins with a TLS handshake: a
choreographed exchange of messages where two parties who have never communicated before agree on
a shared secret, authenticate each other, and establish an encrypted channel — all while an
adversary watches every byte on the wire. TLS 1.3 (RFC 8446) was a radical simplification of its
predecessors, removing broken cipher suites, compressing the handshake to a single round-trip,
and encrypting the server's certificate. Your mission is to implement this handshake from raw
bytes — constructing ClientHello and ServerHello messages by hand, performing X25519 key
exchange, deriving keys through HKDF, encrypting with AES-256-GCM, verifying certificate chains,
and optionally supporting 0-RTT early data. The result must successfully complete a handshake
against a real TLS 1.3 server (OpenSSL s_server, rustls, or a public HTTPS endpoint).

This is not a wrapper around an existing TLS library. You will write the record layer parser that
reads 5-byte record headers and extracts content types. You will write the handshake message
serializer that builds ClientHello with supported_versions, key_share, and
signature_algorithms extensions in the exact binary format specified by RFC 8446. You will
implement the transcript hash that feeds every handshake message into a running SHA-256 digest.
You will derive handshake traffic keys using HKDF-Expand-Label — a TLS-specific KDF construction
built on top of HKDF that uses a label encoding unique to TLS 1.3. You will decrypt the server's
EncryptedExtensions, Certificate, and CertificateVerify messages using the handshake traffic
key. You will verify the server's certificate chain against a root CA store. And you will send
your own Finished message to complete the handshake and derive the application traffic keys used
for actual data exchange.

The reason this exercise earns its "Insane" rating is not any single component — X25519 is a
well-documented algorithm, HKDF is straightforward, AES-GCM is available in crates. The
difficulty is in the precise orchestration: the exact byte-level encoding of every message, the
correct sequencing of key derivations (each derived from the previous), the transcript hash that
must include every message in order, the nonce construction for AEAD that increments per-record,
the content type byte appended inside the encrypted record, and the dozens of edge cases where a
single wrong byte means the server sends an alert and closes the connection. Debugging a failed
handshake means staring at hex dumps comparing your bytes against Wireshark captures. This is
protocol engineering at its most exacting, and Rust's strong typing can help you encode the state
machine so that invalid transitions are compile-time errors.

## Acceptance Criteria

### Record Layer
- [ ] Implement a `RecordLayer` parser that reads TLS records from a TCP stream: 1 byte content type, 2 bytes protocol version (`0x0303` for TLS 1.2 compatibility), 2 bytes length, then payload
- [ ] Content types supported: `ChangeCipherSpec` (0x14), `Alert` (0x15), `Handshake` (0x16), `ApplicationData` (0x17)
- [ ] Records are limited to 2^14 bytes (16384) per RFC 8446 Section 5.1; reject oversized records with an alert
- [ ] Implement record serialization: given a content type and payload, produce the 5-byte header followed by payload
- [ ] After key derivation, the record layer encrypts outgoing records and decrypts incoming records using AES-256-GCM with per-record nonce construction
- [ ] Encrypted records use content type `ApplicationData` (0x17) as the outer type; the real content type is the last byte of the plaintext after decryption (inner content type)
- [ ] The per-record nonce is computed as: `iv XOR sequence_number` where `sequence_number` is a 64-bit counter starting at 0, left-padded to the IV length (12 bytes)
- [ ] Sequence numbers are maintained separately for read and write directions and reset to 0 when new traffic keys are installed

### ClientHello Construction
- [ ] Build a ClientHello message with: protocol version `0x0303` (legacy), 32 bytes random, empty session ID (or 32-byte echo for middlebox compatibility), cipher suites `[TLS_AES_256_GCM_SHA384 (0x1302), TLS_AES_128_GCM_SHA256 (0x1301)]`, compression methods `[0x00]`
- [ ] Include the `supported_versions` extension (0x002B) advertising TLS 1.3 (`0x0304`) — this is what makes it a TLS 1.3 handshake, not the legacy version field
- [ ] Include the `key_share` extension (0x0033) with a single X25519 (0x001D) key share: 32 bytes of the client's ephemeral public key
- [ ] Include the `signature_algorithms` extension (0x000D) with at least: `ecdsa_secp256r1_sha256` (0x0403), `rsa_pss_rsae_sha256` (0x0804), `ed25519` (0x0807)
- [ ] Include the `supported_groups` extension (0x000A) with at least: `x25519` (0x001D), `secp256r1` (0x0017)
- [ ] Include the `server_name` extension (SNI, 0x0000) with the target hostname so virtual hosting works
- [ ] The entire ClientHello is correctly length-prefixed at both the handshake message level (4-byte header: type 0x01 + 3-byte length) and the record level
- [ ] All extension lengths and list lengths are correctly encoded as 2-byte big-endian values
- [ ] The ClientHello bytes match what Wireshark would show for a conformant TLS 1.3 client

### ServerHello Parsing
- [ ] Parse the ServerHello message: extract protocol version, server random, session ID echo, selected cipher suite, selected compression method, and extensions
- [ ] Extract the server's `key_share` extension to obtain the server's X25519 public key
- [ ] Extract the `supported_versions` extension to confirm TLS 1.3 was selected (value `0x0304`)
- [ ] Detect HelloRetryRequest by checking for the special server random value defined in RFC 8446 Section 4.1.3; if received, handle the retry flow (regenerate key share for the requested group)
- [ ] Detect and handle a downgrade to TLS 1.2 by checking the last 8 bytes of server random against the sentinel values in RFC 8446 Section 4.1.3 — abort the connection if downgrade is detected
- [ ] Validate that the server selected a cipher suite that was offered in ClientHello
- [ ] Validate that the session ID echo matches the one sent in ClientHello

### X25519 Key Exchange
- [ ] Generate an ephemeral X25519 keypair using `x25519-dalek` (or equivalent): 32-byte secret scalar and 32-byte public key
- [ ] After receiving the server's public key, compute the shared secret via X25519 Diffie-Hellman
- [ ] Verify that the shared secret is not all-zeros (which would indicate a malicious or broken peer)
- [ ] The ephemeral secret key is zeroized after key derivation using the `zeroize` crate — it must not persist in memory
- [ ] Key generation uses `OsRng` (or `getrandom` crate) for cryptographic randomness — never a deterministic seed in production mode

### HKDF Key Derivation
- [ ] Implement HKDF-Extract and HKDF-Expand per RFC 5869 using HMAC-SHA256 (or HMAC-SHA384 for the AES-256 cipher suite)
- [ ] Implement `HKDF-Expand-Label(Secret, Label, Context, Length)` per RFC 8446 Section 7.1: the info input is `length (2 bytes) || "tls13 " || label (length-prefixed) || context (length-prefixed)`
- [ ] Implement the full TLS 1.3 key schedule (RFC 8446 Section 7.1):
  - Early Secret = HKDF-Extract(salt=0, IKM=0) (no PSK)
  - Handshake Secret = HKDF-Extract(salt=Derive-Secret(Early Secret, "derived", ""), IKM=shared_secret)
  - Client Handshake Traffic Secret = Derive-Secret(Handshake Secret, "c hs traffic", transcript_hash_up_to_ServerHello)
  - Server Handshake Traffic Secret = Derive-Secret(Handshake Secret, "s hs traffic", transcript_hash_up_to_ServerHello)
  - Master Secret = HKDF-Extract(salt=Derive-Secret(Handshake Secret, "derived", ""), IKM=0)
  - Client Application Traffic Secret = Derive-Secret(Master Secret, "c ap traffic", transcript_hash_up_to_server_Finished)
  - Server Application Traffic Secret = Derive-Secret(Master Secret, "s ap traffic", transcript_hash_up_to_server_Finished)
- [ ] Derive `key` and `iv` from each traffic secret using HKDF-Expand-Label with labels "key" and "iv"
- [ ] For AES-256-GCM: key is 32 bytes, IV is 12 bytes; for AES-128-GCM: key is 16 bytes, IV is 12 bytes
- [ ] Implement `Derive-Secret(Secret, Label, Messages)` as `HKDF-Expand-Label(Secret, Label, Hash(Messages), Hash.length)`
- [ ] The transcript hash is a running SHA-256 (or SHA-384) digest of all handshake messages in order, without record headers

### Transcript Hash
- [ ] Maintain a running hash that is updated with the raw bytes of each handshake message (type + length + body, but not the record layer header)
- [ ] The transcript includes: ClientHello, ServerHello, EncryptedExtensions, Certificate, CertificateVerify, server Finished
- [ ] After ServerHello, capture the transcript hash at that point for deriving handshake traffic secrets
- [ ] After server Finished, capture the transcript hash for deriving application traffic secrets
- [ ] After client Finished, capture the transcript hash for the resumption master secret (if implementing session tickets)
- [ ] If a HelloRetryRequest is received, the transcript hash is computed with a special synthetic `message_hash` construct per RFC 8446 Section 4.4.1

### AES-256-GCM Encryption
- [ ] Encrypt outgoing records using AES-256-GCM (or AES-128-GCM depending on selected cipher suite)
- [ ] Construct the plaintext as: `handshake_message_bytes || content_type_byte` — the real content type is appended to the plaintext before encryption
- [ ] The AAD (additional authenticated data) for AEAD is the 5-byte record header of the encrypted record: `0x17 || 0x03 0x03 || length`
- [ ] The nonce is 12 bytes: XOR the traffic IV with the 8-byte sequence number (left-padded with zeros)
- [ ] The ciphertext includes a 16-byte authentication tag appended by AES-GCM
- [ ] Decrypt incoming encrypted records by: reading the record, computing the nonce, decrypting with AES-GCM and AAD, stripping trailing zeros, extracting the last non-zero byte as the inner content type
- [ ] Reject records where AEAD decryption fails with a `bad_record_mac` alert

### Server Authentication
- [ ] Parse the server's Certificate message: extract the certificate chain as a sequence of DER-encoded X.509 certificates
- [ ] Parse the CertificateVerify message: extract the signature algorithm and signature bytes
- [ ] Verify the CertificateVerify signature over the content `"                                " (64 spaces) || "TLS 1.3, server CertificateVerify" || 0x00 || transcript_hash` using the server's public key from its leaf certificate
- [ ] Support at least RSA-PSS-RSAE-SHA256 and ECDSA-secp256r1-SHA256 signature verification (use `ring`, `rsa`, or `p256` crates)
- [ ] Verify the certificate chain: each certificate is signed by the next one, the root is in a trusted CA store
- [ ] Verify the leaf certificate's Subject Alternative Name (SAN) matches the hostname connected to
- [ ] Verify certificate validity dates (notBefore <= now <= notAfter)
- [ ] Load the system CA store or a bundled root CA set (using `webpki-roots` or `rustls-native-certs` crate)
- [ ] Parse the server's Finished message: verify its value equals `HMAC(finished_key, transcript_hash)` where `finished_key = HKDF-Expand-Label(server_handshake_traffic_secret, "finished", "", Hash.length)`

### Client Finished
- [ ] Compute the client Finished value: `HMAC(finished_key, transcript_hash)` where `finished_key = HKDF-Expand-Label(client_handshake_traffic_secret, "finished", "", Hash.length)`
- [ ] Send the client Finished as an encrypted handshake record using the client handshake traffic key
- [ ] After sending Finished, install the application traffic keys for both read and write directions
- [ ] Send a `ChangeCipherSpec` record (a single byte `0x01`) before the client Finished for middlebox compatibility per RFC 8446 Appendix D.4

### 0-RTT Early Data (Stretch Goal)
- [ ] Implement session ticket parsing from `NewSessionTicket` messages received after the handshake completes
- [ ] Store the ticket along with the resumption master secret and the cipher suite
- [ ] On reconnection, send early data encrypted with keys derived from the PSK (pre-shared key) from the previous session
- [ ] Include the `early_data` extension (0x002A) in ClientHello and the `pre_shared_key` extension (0x0029)
- [ ] Handle the server's acceptance or rejection of 0-RTT (via the `early_data` extension in EncryptedExtensions)
- [ ] Implement anti-replay protection: track ticket nonces to prevent reuse (or document the limitation)

### Interoperability Testing
- [ ] Successfully complete a full TLS 1.3 handshake against `openssl s_server -tls1_3`
- [ ] Successfully complete a full handshake against a `rustls`-based server (e.g., `hyper` with rustls)
- [ ] Successfully connect to a public HTTPS server (e.g., `https://www.google.com`) and retrieve a response
- [ ] Send an HTTP/1.1 GET request over the established TLS connection and receive a valid HTTP response
- [ ] Capture the handshake with Wireshark/tshark and verify that Wireshark decodes all messages correctly (no "Malformed Packet" warnings)
- [ ] Test with at least two different cipher suites: `TLS_AES_256_GCM_SHA384` and `TLS_AES_128_GCM_SHA256`
- [ ] Test against a server that sends HelloRetryRequest (by initially offering only a less-preferred group, then retrying)

### State Machine and Error Handling
- [ ] Model the handshake as an explicit state machine with states: `Start`, `WaitServerHello`, `WaitEncryptedExtensions`, `WaitCertificate`, `WaitCertificateVerify`, `WaitFinished`, `Connected`, `Error`
- [ ] Use Rust's type system to enforce valid state transitions — invalid transitions should be compile-time errors (use typestate pattern or enum with distinct types per state)
- [ ] Each state transition consumes the previous state and produces the next (move semantics prevent reuse)
- [ ] Implement TLS alert sending for error conditions: `unexpected_message`, `bad_record_mac`, `handshake_failure`, `bad_certificate`, `decode_error`
- [ ] Parse incoming TLS alerts and convert them to descriptive Rust error types
- [ ] Handle the server requesting a HelloRetryRequest by transitioning back to a modified ClientHello state
- [ ] Connection timeout: abort if the handshake does not complete within a configurable deadline (default: 10 seconds)

### Logging and Debugging
- [ ] Implement a `KeyLog` writer compatible with the `SSLKEYLOGFILE` format (per NSS Key Log Format) so Wireshark can decrypt captured traffic
- [ ] Output CLIENT_RANDOM and traffic secrets in the correct format: `CLIENT_HANDSHAKE_TRAFFIC_SECRET`, `SERVER_HANDSHAKE_TRAFFIC_SECRET`, `CLIENT_TRAFFIC_SECRET_0`, `SERVER_TRAFFIC_SECRET_0`
- [ ] Optional verbose mode that logs every handshake message, extension, key derivation step, and record encryption/decryption with hex dumps
- [ ] Log the selected cipher suite, key exchange group, and signature algorithm after a successful handshake

### Testing
- [ ] Unit tests for ClientHello serialization: verify the exact byte sequence matches a known-good capture
- [ ] Unit tests for HKDF-Expand-Label: test against RFC 8448 test vectors (the TLS 1.3 test vectors RFC)
- [ ] Unit tests for the full key schedule: given a known shared secret and transcript hashes, verify all derived keys match RFC 8448
- [ ] Unit tests for AES-GCM encryption/decryption round-trip with known test vectors
- [ ] Unit tests for nonce construction: verify the XOR of IV and sequence number for sequence numbers 0, 1, 255, 256
- [ ] Integration test: complete handshake against localhost OpenSSL or rustls server (spawned in test)
- [ ] Integration test: send and receive application data after handshake completes
- [ ] Test certificate verification failure: connect to a server with an expired or self-signed certificate, verify rejection
- [ ] Test alert handling: intentionally corrupt a message and verify the server sends an appropriate alert
- [ ] Property test: random ClientHello extensions are correctly serialized and can be round-tripped through parse(serialize(x))

## Starting Points

- **RFC 8446 — TLS 1.3**: https://www.rfc-editor.org/rfc/rfc8446 — the definitive specification. Read Sections 2 (Protocol Overview), 4 (Handshake Protocol), 5 (Record Protocol), and 7 (Cryptographic Computations) end-to-end. This is a ~100-page RFC and you will need to reference it constantly. The state machine diagram in Section 2 is your roadmap
- **RFC 8448 — TLS 1.3 Test Vectors**: https://www.rfc-editor.org/rfc/rfc8448 — a complete byte-by-byte trace of a TLS 1.3 handshake with all intermediate values (shared secrets, derived keys, encrypted records). This is your debugging bible. Every key derivation step, every transcript hash, every encrypted record is given in hex. If your implementation disagrees with RFC 8448, your implementation is wrong
- **The Illustrated TLS 1.3 Connection**: https://tls13.xargs.org/ — the single best resource for understanding TLS 1.3. Every byte of every message is annotated with its meaning, with expandable sections showing the key derivation at each step. Start here before reading the RFC
- **rustls source code**: https://github.com/rustls/rustls — a production TLS implementation in Rust. Study `rustls/src/tls13/` for the handshake state machine, `rustls/src/msgs/` for message parsing, and `rustls/src/crypto/` for the key schedule. This is the reference implementation for how to do TLS in Rust correctly
- **tls-parser crate**: https://github.com/rusticata/tls-parser — a nom-based TLS message parser. Study how it decodes record headers, handshake messages, and extensions. This crate parses but does not perform the cryptographic operations — it shows the binary format without the crypto complexity
- **x25519-dalek crate**: https://docs.rs/x25519-dalek — the standard Rust implementation of X25519 key exchange. The API is simple: `EphemeralSecret::random_from_rng(&mut OsRng)` for key generation, `PublicKey::from(&secret)` for the public key, `secret.diffie_hellman(&their_public)` for the shared secret
- **ring crate**: https://docs.rs/ring — provides HKDF, HMAC, AES-GCM, and signature verification. Study `ring::hkdf`, `ring::aead`, and `ring::signature` APIs. Alternatively use `hkdf` + `aes-gcm` from RustCrypto for more granular control
- **RustCrypto crates**: https://github.com/RustCrypto — individual crates for `hkdf`, `hmac`, `sha2`, `aes-gcm`, `p256`. These give you maximum control over each cryptographic operation at the cost of assembling them yourself
- **webpki crate**: https://docs.rs/webpki — X.509 certificate chain verification used by rustls. Study how it validates certificate chains, checks SANs, and enforces path length constraints

## Hints

1. Start with the illustrated TLS connection at tls13.xargs.org and trace through every byte. Before writing any code, understand the complete message flow: ClientHello -> ServerHello -> {EncryptedExtensions, Certificate, CertificateVerify, Finished} -> {Finished}. The curly braces indicate messages encrypted with handshake traffic keys
2. Build the ClientHello first and test it by connecting to a real server. Even before you can parse the response, a correct ClientHello will elicit a ServerHello (not an alert). Compare your bytes against the RFC 8448 test vector byte-for-byte
3. The most common bug is length encoding. TLS uses 2-byte big-endian lengths at multiple nesting levels: record length, handshake message length, extensions list length, individual extension length, key share length. Getting one wrong shifts every subsequent byte, producing garbage. Write a helper that builds length-prefixed byte vectors: `fn length_prefixed_2(data: &[u8]) -> Vec<u8>` that prepends a 2-byte length
4. For HKDF-Expand-Label, the "info" parameter construction is TLS-specific and error-prone. The format is: `u16_be(length) || u8(len("tls13 " + label)) || "tls13 " || label || u8(len(context)) || context`. Note the literal string "tls13 " with a space, not "tls1.3". Write this function, test it against RFC 8448, and do not proceed until it matches
5. The transcript hash must include handshake messages in their raw serialized form (type byte + 3-byte length + body), but NOT the record layer header (5 bytes) and NOT the record layer encryption. After decrypting a record, strip the content type byte and feed the decrypted handshake message(s) into the transcript hash
6. After deriving handshake traffic keys, all subsequent handshake messages from the server arrive encrypted. You must decrypt them with the server handshake traffic key before parsing. The decrypted payload may contain multiple handshake messages concatenated — parse them sequentially
7. The nonce construction catches many people: it is NOT incrementing the IV. It is XOR: `nonce[i] = iv[i] ^ padded_sequence[i]`. The sequence number is a 64-bit integer zero-padded to 12 bytes (the IV length), then XORed byte-by-byte with the IV. Sequence 0 means the nonce equals the IV; sequence 1 flips the last byte; sequence 256 flips the second-to-last byte
8. For certificate verification, do not write your own X.509 parser from scratch unless you want a second Insane-level exercise. Use the `webpki` or `x509-parser` crate to parse certificates and verify signatures. Focus your effort on the TLS-specific parts: CertificateVerify signature computation (the 64-space prefix is important) and Finished HMAC verification
9. Use the typestate pattern for the handshake state machine. Define `struct Handshake<S: State>` with marker types `Start`, `WaitServerHello`, etc. Each transition method consumes `Handshake<CurrentState>` and returns `Result<Handshake<NextState>>`. This prevents calling methods in the wrong order at compile time
10. For debugging, implement SSLKEYLOGFILE output early. Once you have the handshake traffic secrets, write them to a file in the NSS format. Then capture your handshake with tcpdump/Wireshark and load the key log — Wireshark will decrypt the traffic and show you exactly which message is malformed
11. The encrypted record layer has a subtle detail: the plaintext to encrypt is `content || content_type || zeros`. When decrypting, you strip trailing zero bytes and the last non-zero byte is the content type. The zeros are optional padding per RFC 8446 Section 5.4 — most implementations do not add padding, but your decryption must handle it
12. For AES-GCM, the AAD is the 5-byte TLS record header with the encrypted length (ciphertext + tag), not the plaintext length. This is a common source of decryption failures — the AAD must match exactly what was used during encryption, and since TLS encrypts the content type inside the record, the outer header always says content type 0x17
13. Test your key schedule derivations before attempting a real handshake. Use the shared secret and transcript hashes from RFC 8448 to verify every intermediate key. The test vectors give you: Early Secret, Handshake Secret, client/server handshake traffic secrets, Master Secret, and client/server application traffic secrets. If any single derivation is wrong, everything downstream fails
14. The ChangeCipherSpec message sent for middlebox compatibility is NOT included in the transcript hash and does NOT affect the key schedule. It is a legacy compatibility measure: a single record `[0x14, 0x03, 0x03, 0x00, 0x01, 0x01]` sent after ClientHello. Ignore any ChangeCipherSpec records received from the server
15. For 0-RTT, understand the anti-replay implications. Unlike the main handshake, 0-RTT data can be replayed by a network attacker. The server must have application-level idempotency guarantees or implement a replay cache. This is why 0-RTT is a stretch goal — it introduces fundamental security tradeoffs that go beyond correct implementation
16. When connecting to a real server, you may encounter TLS extensions you did not implement (ALPN, post-handshake auth, key update). Your parser should skip unknown extensions gracefully rather than aborting. For the MVP, parse only the extensions you need and log warnings for others
