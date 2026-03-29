<!-- difficulty: advanced -->
<!-- category: security-cryptography -->
<!-- languages: [rust] -->
<!-- concepts: [tls, handshake, x509, rsa-key-exchange, key-derivation, aes-cbc, pki, state-machine] -->
<!-- estimated_time: 12-18 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [rust-basics, tcp-networking, aes-encryption, rsa-basics, sha256-hashing, hex-encoding, byte-manipulation] -->

# Challenge 91: TLS Handshake Educational Implementation

## Languages

Rust (stable, latest edition)

## Prerequisites

- TCP client/server programming in Rust (`std::net::TcpStream`, `TcpListener`)
- Understanding of AES-CBC encryption and decryption (block cipher with chaining)
- Basic RSA concepts: public/private keys, encryption with public key, signing with private key
- SHA-256 hashing and HMAC construction
- Comfortable with byte-level protocol parsing: reading big-endian lengths, parsing tagged fields, assembling binary messages
- Awareness of the PKI trust model: certificates, certificate authorities, certificate chains

## Learning Objectives

- **Implement** a simplified TLS 1.2 handshake state machine covering ClientHello, ServerHello, Certificate, ServerHelloDone, ClientKeyExchange, ChangeCipherSpec, and Finished messages
- **Analyze** how the handshake establishes shared secrets without transmitting them, and why each message is necessary
- **Evaluate** the security properties provided by each phase of the handshake: authentication, key agreement, forward secrecy implications
- **Design** a key derivation function that expands a pre-master secret into separate encryption and MAC keys for each direction
- **Implement** record-layer encryption using AES-CBC with HMAC-SHA256 for message authentication

## The Challenge

TLS (Transport Layer Security) is the protocol that puts the S in HTTPS. Every secure web connection, every API call over HTTPS, every encrypted email transfer uses TLS. Yet most developers have never seen the handshake that establishes the encrypted channel. They call `connect()` and trust the library. This challenge opens the protocol.

Implement a simplified TLS 1.2 handshake between a client and server, both running in the same process (or as two threads communicating over a local TCP connection). This is an educational implementation that covers the RSA key exchange variant of TLS 1.2 -- not ECDHE, not TLS 1.3, not production-grade. The goal is understanding the protocol mechanics.

The handshake proceeds in phases:

1. **ClientHello**: The client sends its supported protocol version (TLS 1.2 = 0x0303), a 32-byte random value, a session ID, a list of supported cipher suites (you only need `TLS_RSA_WITH_AES_128_CBC_SHA256` = 0x003C), and a compression methods list (null only).

2. **ServerHello**: The server responds with its chosen version, its own 32-byte random, a session ID, the selected cipher suite, and the selected compression method.

3. **Certificate**: The server sends its X.509 certificate chain. For this exercise, parse a self-signed certificate to extract the RSA public key (modulus and exponent). You do not need full X.509/ASN.1 parsing -- extract the key fields from a known certificate format.

4. **ServerHelloDone**: A zero-length message signaling the server is done with its hello phase.

5. **ClientKeyExchange**: The client generates a 48-byte pre-master secret (version + 46 random bytes), encrypts it with the server's RSA public key, and sends the ciphertext.

6. **Key Derivation**: Both sides compute the master secret: `PRF(pre_master_secret, "master secret", client_random + server_random)`. Then derive key material: `PRF(master_secret, "key expansion", server_random + client_random)` to produce `client_write_MAC_key`, `server_write_MAC_key`, `client_write_key`, `server_write_key`, `client_write_IV`, `server_write_IV`.

7. **ChangeCipherSpec**: Both sides send a single-byte message indicating all subsequent messages will be encrypted.

8. **Finished**: Both sides send a verification hash: `PRF(master_secret, "client finished" | "server finished", Hash(all_handshake_messages))`, encrypted under the newly derived keys.

After the handshake, implement record-layer encryption: each message is MACed with HMAC-SHA256, padded with PKCS#7, and encrypted with AES-128-CBC. The IV for each record is sent explicitly (TLS 1.1+ style).

**WARNING**: This implementation is for learning only. It has no timing attack protection, no certificate chain validation, simplified RSA without proper PKCS#1 v1.5 padding, and numerous other shortcuts that make it completely unsuitable for any real security purpose.

## Requirements

1. Implement TLS record framing: 1-byte content type, 2-byte protocol version, 2-byte length, followed by the fragment. Content types: Handshake (22), ChangeCipherSpec (20), ApplicationData (23)
2. Implement ClientHello message construction: version 0x0303, 32-byte client random, empty session ID, cipher suite list containing 0x003C, compression methods containing 0x00
3. Implement ServerHello message construction: version 0x0303, 32-byte server random, 32-byte session ID, selected cipher suite 0x003C, compression 0x00
4. Implement a minimal X.509 certificate parser that extracts the RSA public key (modulus `n` and exponent `e`) from a DER-encoded self-signed certificate. Implement enough ASN.1 tag-length-value parsing to navigate the certificate structure
5. Implement the Certificate handshake message: a certificate list containing one certificate
6. Implement RSA encryption for the ClientKeyExchange: `ciphertext = message^e mod n` using big-integer arithmetic. Implement big-integer modular exponentiation from scratch (or use a minimal bignum implementation)
7. Implement the TLS PRF (Pseudo-Random Function) based on HMAC-SHA256: `P_SHA256(secret, seed) = HMAC(secret, A(1) + seed) + HMAC(secret, A(2) + seed) + ...` where `A(i) = HMAC(secret, A(i-1))` and `A(0) = seed`
8. Implement master secret derivation: `PRF(pre_master_secret, "master secret", client_random + server_random)` producing 48 bytes
9. Implement key expansion: `PRF(master_secret, "key expansion", server_random + client_random)` producing enough bytes for two MAC keys (32 bytes each), two encryption keys (16 bytes each), and two IVs (16 bytes each) = 128 bytes total
10. Implement the ChangeCipherSpec message (single byte 0x01)
11. Implement the Finished message: `PRF(master_secret, label, SHA256(handshake_messages))` where label is `"client finished"` or `"server finished"`, producing 12 bytes of verify data
12. Implement record-layer encryption: compute HMAC-SHA256 over (sequence number + content type + version + length + plaintext), PKCS#7-pad the (plaintext + MAC), generate random explicit IV, AES-128-CBC encrypt, prepend IV to ciphertext
13. Implement record-layer decryption: extract explicit IV, AES-128-CBC decrypt, verify and remove PKCS#7 padding, verify HMAC
14. Implement the handshake state machine: enforce message ordering (ClientHello must come first, ServerHello must follow, etc.), reject out-of-order messages
15. Generate a self-signed RSA key pair and certificate for testing (can use pre-generated test values)
16. Run the full handshake over a local TCP connection and exchange at least one encrypted application data message in each direction
17. Print a trace of every handshake message sent and received, showing the content in hex

## Hints

1. For RSA encryption without external crates, implement big-integer arithmetic using `Vec<u64>`
   to represent large numbers. You need: addition, subtraction, multiplication, division, and
   modular exponentiation (square-and-multiply). The modular exponentiation
   `base^exp mod modulus` is the core operation. Use Montgomery multiplication or simple
   binary exponentiation. For a 2048-bit RSA key, the numbers are ~256 bytes. If this is too
   complex, use a 512-bit key for educational purposes (insecure but easier to implement).

2. The TLS PRF is built on HMAC-SHA256. The iteration `A(i) = HMAC(secret, A(i-1))` generates
   the chain. Each output block is `HMAC(secret, A(i) || seed)`. Concatenate blocks until you
   have enough bytes, then truncate. Getting the seed wrong (client_random and server_random
   order differs between master secret and key expansion) is the most common TLS PRF bug.

3. For X.509 parsing, you only need to traverse the ASN.1 SEQUENCE/SET/BITSTRING structure
   to reach the SubjectPublicKeyInfo field. The path is: Certificate -> TBSCertificate ->
   SubjectPublicKeyInfo -> subjectPublicKey (BIT STRING containing the RSA key). The RSA key
   itself is a SEQUENCE of two INTEGERs (modulus, exponent). Use DER tag-length-value parsing:
   tag (1 byte), length (1 or 2+ bytes, check high bit), value.

4. Record-layer encryption with HMAC-then-encrypt (MAC-then-pad-then-encrypt): compute the MAC
   over the plaintext and metadata, then concatenate plaintext + MAC, then pad with PKCS#7 to
   AES block size, then encrypt with CBC. On decryption, reverse: decrypt, remove padding,
   split plaintext and MAC, recompute MAC and verify. This order (MAC-then-encrypt) is the
   TLS 1.2 standard, though it is vulnerable to padding oracle attacks in theory. TLS 1.3
   switched to AEAD ciphers that avoid this issue.

## Acceptance Criteria

- [ ] ClientHello contains version 0x0303, 32-byte random, cipher suite 0x003C
- [ ] ServerHello correctly selects the offered cipher suite
- [ ] X.509 certificate parser extracts RSA modulus and exponent from a DER-encoded certificate
- [ ] RSA encryption: `decrypt(encrypt(message)) == message` for the test key pair
- [ ] Pre-master secret is 48 bytes: 2-byte version + 46 random bytes
- [ ] PRF produces deterministic output for the same inputs (test against known vectors)
- [ ] Master secret is 48 bytes derived from pre-master secret and both randoms
- [ ] Key expansion produces 128 bytes split into correct key material segments
- [ ] ChangeCipherSpec is exactly 1 byte (0x01) with content type 20
- [ ] Finished verify data is 12 bytes matching `PRF(master_secret, label, Hash(messages))`
- [ ] Record encryption: `decrypt(encrypt(plaintext)) == plaintext` for arbitrary messages
- [ ] HMAC verification rejects tampered ciphertext (flip a byte, verify MAC failure)
- [ ] Handshake state machine rejects out-of-order messages
- [ ] Full handshake completes over TCP: client and server exchange Finished messages successfully
- [ ] Application data is encrypted and decrypted correctly after the handshake
- [ ] Handshake trace output shows each message type, length, and key hex content
- [ ] No external cryptography dependencies -- all crypto (AES, SHA-256, HMAC, RSA) is self-contained or from previous challenges

## Research Resources

- [TLS 1.2 Specification (RFC 5246)](https://datatracker.ietf.org/doc/html/rfc5246) -- the definitive TLS 1.2 reference. Sections 7.4 (handshake protocol) and 6.2.3 (record layer encryption) are the most relevant
- [The Illustrated TLS 1.2 Connection](https://tls12.xargs.org/) -- byte-by-byte walkthrough of a real TLS 1.2 handshake with annotations for every field. The single best resource for understanding the protocol
- [The Illustrated TLS 1.3 Connection](https://tls13.xargs.org/) -- companion resource showing how TLS 1.3 improves on 1.2 (fewer round trips, mandatory AEAD, encrypted extensions)
- [X.509 Certificate Structure (Let's Encrypt)](https://letsencrypt.org/docs/a]/) -- overview of the certificate fields your parser needs to navigate
- [ASN.1 and DER Encoding](https://letsencrypt.org/docs/a]/) -- the binary encoding your X.509 parser must decode
- [PKCS#1: RSA Cryptography (RFC 8017)](https://datatracker.ietf.org/doc/html/rfc8017) -- RSA encryption and signature schemes, including OAEP and PSS padding
- [TLS PRF (RFC 5246, Section 5)](https://datatracker.ietf.org/doc/html/rfc5246#section-5) -- the pseudo-random function specification using HMAC-SHA256
- [Lucky 13 Attack (AlFardan & Paterson, 2013)](https://www.isg.rhul.ac.uk/tls/Lucky13.html) -- demonstrates why MAC-then-encrypt in TLS is problematic, motivating the AEAD switch in TLS 1.3
