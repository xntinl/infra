# 9. Mutual TLS Authentication

<!--
difficulty: advanced
concepts: [mtls, client-certificate, certificate-authority, peer-verification, tls-client-auth]
tools: [go, openssl]
estimated_time: 40m
bloom_level: analyze
prerequisites: [tls-server-and-client, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TLS Server and Client exercise
- Understanding of certificate chains and CA trust
- Knowledge of public/private key pairs

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** mutual TLS (mTLS) where both server and client present certificates
- **Analyze** the difference between server-only TLS and mutual TLS authentication
- **Configure** `tls.Config.ClientAuth` policies: NoClientCert, RequestClientCert, RequireAndVerifyClientCert
- **Extract** client identity from the verified certificate chain

## Why Mutual TLS Matters

Standard TLS only authenticates the server (the client verifies the server's certificate). Mutual TLS additionally requires the client to present a certificate that the server verifies. This provides strong, certificate-based client authentication without passwords or tokens.

mTLS is used for service-to-service communication in zero-trust architectures, Kubernetes API server authentication, and internal microservice communication. Service meshes like Istio and Linkerd use mTLS transparently.

## The Problem

Build a service-to-service communication system where both sides authenticate via certificates:

1. A CA issues certificates for the server and client
2. The server requires and verifies client certificates
3. The client verifies the server certificate
4. The server extracts the client's identity (Common Name) from the certificate

## Requirements

1. **CA setup** -- generate a CA certificate and key programmatically
2. **Server cert** -- generate a server certificate signed by the CA
3. **Client cert** -- generate a client certificate signed by the same CA
4. **Server config** -- `tls.Config` with `ClientAuth: tls.RequireAndVerifyClientCert` and `ClientCAs` pool
5. **Client config** -- `tls.Config` with client certificate in `Certificates` and `RootCAs` pool
6. **Identity extraction** -- server reads `conn.ConnectionState().PeerCertificates[0].Subject.CommonName`
7. **Rejection test** -- verify that a client without a certificate is rejected
8. **Wrong CA test** -- verify that a client with a certificate from a different CA is rejected

## Hints

<details>
<summary>Hint 1: Generating CA and signed certificates</summary>

```go
func generateCA() (*x509.Certificate, *rsa.PrivateKey, error) {
    caKey, _ := rsa.GenerateKey(rand.Reader, 2048)
    caTemplate := &x509.Certificate{
        SerialNumber:          big.NewInt(1),
        Subject:               pkix.Name{CommonName: "Test CA"},
        NotBefore:             time.Now(),
        NotAfter:              time.Now().Add(time.Hour),
        IsCA:                  true,
        BasicConstraintsValid: true,
        KeyUsage:              x509.KeyUsageCertSign,
    }
    caDER, _ := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
    caCert, _ := x509.ParseCertificate(caDER)
    return caCert, caKey, nil
}

func generateSignedCert(ca *x509.Certificate, caKey *rsa.PrivateKey, cn string, isServer bool) (tls.Certificate, error) {
    key, _ := rsa.GenerateKey(rand.Reader, 2048)
    template := &x509.Certificate{
        SerialNumber: big.NewInt(2),
        Subject:      pkix.Name{CommonName: cn},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(time.Hour),
        KeyUsage:     x509.KeyUsageDigitalSignature,
        IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
    }
    if isServer {
        template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
    } else {
        template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
    }
    certDER, _ := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
    // ... PEM encode and create tls.Certificate
}
```

</details>

<details>
<summary>Hint 2: Server mTLS config</summary>

```go
caPool := x509.NewCertPool()
caPool.AddCert(caCert)

serverConfig := &tls.Config{
    Certificates: []tls.Certificate{serverCert},
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    caPool,
    MinVersion:   tls.VersionTLS12,
}
```

</details>

<details>
<summary>Hint 3: Extracting client identity</summary>

```go
tlsConn := conn.(*tls.Conn)
state := tlsConn.ConnectionState()
if len(state.PeerCertificates) > 0 {
    clientCN := state.PeerCertificates[0].Subject.CommonName
    log.Printf("authenticated client: %s", clientCN)
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- mTLS handshake succeeds when both sides present valid certificates from the same CA
- Server extracts the client's Common Name correctly
- Connection is rejected when the client presents no certificate
- Connection is rejected when the client's certificate is signed by a different CA
- Connection is rejected when the server's certificate is not trusted by the client

## What's Next

Continue to [10 - DNS Resolver and Custom Dialer](../10-dns-resolver-and-custom-dialer/10-dns-resolver-and-custom-dialer.md) to implement custom DNS resolution and dialing.

## Summary

- Mutual TLS authenticates both sides: client verifies server, server verifies client
- `tls.RequireAndVerifyClientCert` enforces client certificate presentation
- Both sides trust the same CA; the CA signs server and client certificates
- Extract client identity from `ConnectionState().PeerCertificates[0].Subject`
- mTLS is the standard for service-to-service authentication in zero-trust architectures
- Test both positive (valid certs) and negative (no cert, wrong CA) scenarios

## Reference

- [tls.Config.ClientAuth](https://pkg.go.dev/crypto/tls#ClientAuthType)
- [Mutual TLS explained](https://www.cloudflare.com/learning/access-management/what-is-mutual-tls/)
- [x509.Certificate](https://pkg.go.dev/crypto/x509#Certificate)
