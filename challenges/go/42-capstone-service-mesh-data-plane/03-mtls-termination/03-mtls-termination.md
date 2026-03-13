# 3. mTLS Termination

<!--
difficulty: insane
concepts: [mtls, tls-termination, x509-certificates, certificate-rotation, spiffe, trust-bundles, tls-config, certificate-verification]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [42-capstone-service-mesh-data-plane/02-l7-http-proxy, 33-tcp-udp-and-networking]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-02 (L4/L7 proxy) or equivalent proxy experience
- Understanding of TLS handshake, X.509 certificates, and public key cryptography fundamentals

## Learning Objectives

- **Design** an mTLS termination layer that authenticates both client and server using X.509 certificates
- **Create** a certificate management system supporting dynamic rotation without connection drops
- **Evaluate** trust models including certificate pinning, CA-based trust, and SPIFFE identity verification

## The Challenge

In a service mesh, every service-to-service connection is encrypted and mutually authenticated using mTLS. The data plane sidecar terminates TLS from the downstream client, verifies the client's certificate against a trust bundle, extracts the client's identity (typically a SPIFFE ID from the SAN), and then establishes a new TLS connection to the upstream with its own certificate. This is the security backbone of zero-trust networking.

You will extend your proxy to support mTLS on both the downstream (client-facing) and upstream (backend-facing) sides. The downstream listener must require client certificates, verify them against a configurable CA trust bundle, and extract the peer identity. The upstream dialer must present the proxy's own certificate to the backend. Certificates must be rotatable at runtime without restarting the proxy or dropping active connections -- when new certificates are loaded, they take effect for new connections while existing connections continue using their original certificates.

Building this correctly requires deep understanding of Go's `crypto/tls` package, certificate verification callbacks, and the distinction between the TLS handshake and the certificate verification chain. You must handle certificate expiration, revocation checking, and SAN-based identity extraction.

## Requirements

1. Generate a test CA and leaf certificates (server and client) programmatically using `crypto/x509` and `crypto/ecdsa` for use in tests
2. Configure the downstream TLS listener to require and verify client certificates (`tls.RequireAndVerifyClientCert`)
3. Extract the peer identity from the client certificate's SAN (DNS name or URI, supporting SPIFFE format `spiffe://trust-domain/path`)
4. Make the extracted peer identity available to the routing layer via request context for identity-based routing decisions
5. Configure the upstream TLS dialer to present the proxy's own certificate and verify the upstream's server certificate
6. Implement dynamic certificate rotation: watch certificate files for changes and reload them without restarting the proxy
7. Use `tls.Config.GetCertificate` and `tls.Config.GetClientCertificate` callbacks to serve the latest certificates for new connections
8. Implement certificate expiration checking that logs warnings when certificates are within a configurable window of expiration
9. Support configurable trust bundles per upstream, allowing different CAs for different backends
10. Expose TLS metrics: handshake successes, handshake failures (broken down by reason), certificate expiration timestamps, and active TLS connections

## Hints

- Use `crypto/x509.CreateCertificate` with `x509.Certificate` templates to generate test CAs and leaf certs -- set `IsCA: true` and `KeyUsage: x509.KeyUsageCertSign` for CA certs
- Store the current certificate behind an `atomic.Pointer[tls.Certificate]` and swap it atomically when rotation occurs
- In `GetCertificate`/`GetClientCertificate` callbacks, load the certificate from the atomic pointer to serve the latest version
- For SPIFFE ID extraction, look in `peer.TLSInfo.State.PeerCertificates[0].URIs` for URIs matching the `spiffe://` scheme
- Use `fsnotify` or a polling goroutine to detect certificate file changes on disk
- For per-upstream trust bundles, create separate `tls.Config` instances with different `RootCAs` pools and select the appropriate one based on the upstream being connected to

## Success Criteria

1. The proxy requires and verifies client certificates on the downstream side, rejecting clients with invalid or expired certificates
2. SPIFFE IDs are correctly extracted from client certificate SANs and made available via context
3. The proxy presents its own certificate when connecting to upstream backends
4. Certificate rotation takes effect for new connections without restarting the proxy or dropping existing connections
5. Expired certificates are detected and warned about before they cause handshake failures
6. TLS handshake metrics accurately count successes, failures, and failure reasons
7. Per-upstream trust bundles correctly isolate certificate verification for different backends
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Go crypto/tls package](https://pkg.go.dev/crypto/tls) -- TLS configuration, certificate callbacks, and connection state
- [Go crypto/x509 package](https://pkg.go.dev/crypto/x509) -- certificate generation, parsing, and verification
- [SPIFFE specification](https://spiffe.io/docs/latest/spiffe-about/overview/) -- standard for service identity in distributed systems
- [Envoy SDS (Secret Discovery Service)](https://www.envoyproxy.io/docs/envoy/latest/configuration/security/secret) -- reference design for dynamic certificate management
- [mTLS explained](https://www.cloudflare.com/learning/access-management/what-is-mutual-tls/) -- mutual TLS authentication concepts and flow

## What's Next

Continue to [Load Balancing](../04-load-balancing/04-load-balancing.md) where you will implement multiple load balancing algorithms to distribute traffic across upstream backends.

## Summary

- mTLS ensures both client and server authenticate each other using X.509 certificates
- SPIFFE IDs provide a standardized service identity format extracted from certificate SANs
- Dynamic certificate rotation using atomic swaps and TLS callbacks avoids downtime during certificate renewals
- Per-upstream trust bundles enable different CA hierarchies for different backend services
- Certificate expiration monitoring prevents outages caused by expired certificates
- Go's `crypto/tls` package provides all the primitives needed for production mTLS implementation
