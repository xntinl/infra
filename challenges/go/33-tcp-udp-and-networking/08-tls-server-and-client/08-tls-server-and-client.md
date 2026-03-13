# 8. TLS Server and Client

<!--
difficulty: advanced
concepts: [crypto-tls, tls-config, certificate, key-pair, self-signed, tls-listener]
tools: [go, openssl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [tcp-server-and-client, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Client exercise
- Basic understanding of TLS/SSL concepts (certificates, keys, encryption)
- `openssl` command available for certificate generation

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a TLS server using `crypto/tls` with certificate and key
- **Configure** a TLS client with custom `tls.Config` and CA certificate verification
- **Generate** self-signed certificates for development using Go's `crypto/x509`
- **Analyze** TLS handshake details including cipher suite and protocol version

## Why TLS Matters

TLS encrypts TCP connections to prevent eavesdropping and tampering. Any TCP service that transmits sensitive data must use TLS. Go's `crypto/tls` package provides a complete TLS implementation that wraps `net.Conn` transparently -- your existing TCP code works with minimal changes.

Understanding TLS configuration -- cipher suites, minimum protocol versions, certificate verification -- is essential for building secure network services.

## The Problem

Build a TLS echo server and client. The server uses a self-signed certificate generated programmatically. The client connects and verifies the server's certificate.

## Requirements

1. **Certificate generation** -- generate a self-signed X.509 certificate and RSA key pair using `crypto/x509` and `crypto/rsa` (no external tools)
2. **TLS server** -- use `tls.Listen` or wrap a `net.Listener` with `tls.NewListener`
3. **TLS client** -- use `tls.Dial` with a `tls.Config` that trusts the self-signed CA
4. **Configuration** -- set minimum TLS version to 1.2, specify cipher suites
5. **Handshake inspection** -- log the negotiated TLS version and cipher suite after the handshake
6. **Tests** -- test TLS echo with the generated certificate; verify that an untrusted certificate is rejected

## Hints

<details>
<summary>Hint 1: Generating self-signed certificate in Go</summary>

```go
func generateCert() (tls.Certificate, *x509.CertPool, error) {
    key, _ := rsa.GenerateKey(rand.Reader, 2048)

    template := x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{Organization: []string{"Test"}},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(time.Hour),
        KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
    }

    certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
    certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
    keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

    cert, _ := tls.X509KeyPair(certPEM, keyPEM)
    pool := x509.NewCertPool()
    pool.AppendCertsFromPEM(certPEM)
    return cert, pool, nil
}
```

</details>

<details>
<summary>Hint 2: TLS server</summary>

```go
cert, _, _ := generateCert()
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    MinVersion:   tls.VersionTLS12,
}

listener, _ := tls.Listen("tcp", ":9443", tlsConfig)
defer listener.Close()
```

</details>

<details>
<summary>Hint 3: TLS client with CA</summary>

```go
_, caPool, _ := generateCert() // same cert used for both

conn, err := tls.Dial("tcp", "127.0.0.1:9443", &tls.Config{
    RootCAs:    caPool,
    MinVersion: tls.VersionTLS12,
})
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- TLS handshake completes successfully with the self-signed cert
- Echo works correctly over the encrypted connection
- Connecting without trusting the CA returns a certificate verification error
- The negotiated TLS version is >= 1.2

## What's Next

Continue to [09 - Mutual TLS Authentication](../09-mutual-tls-authentication/09-mutual-tls-authentication.md) to add client certificate verification.

## Summary

- `crypto/tls` wraps TCP connections with TLS encryption transparently
- Generate self-signed certificates with `crypto/x509` and `crypto/rsa` for development
- `tls.Listen` creates a TLS listener; `tls.Dial` creates a TLS client connection
- Set `MinVersion: tls.VersionTLS12` to enforce modern TLS
- The client must trust the server's CA certificate in its `RootCAs` pool
- Inspect `conn.ConnectionState()` for handshake details after connecting

## Reference

- [crypto/tls package](https://pkg.go.dev/crypto/tls)
- [crypto/x509 package](https://pkg.go.dev/crypto/x509)
- [TLS in Go](https://go.dev/blog/tls-cipher-suites)
