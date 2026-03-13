# 10. DNS Resolver and Custom Dialer

<!--
difficulty: advanced
concepts: [net-resolver, custom-dialer, dns-lookup, dial-context, control-func]
tools: [go, dig]
estimated_time: 40m
bloom_level: analyze
prerequisites: [tcp-server-and-client, context, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of TCP connections and HTTP clients
- Familiarity with DNS concepts (A records, CNAME, resolvers)
- Knowledge of `context.Context`

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a custom `net.Resolver` that uses a specific DNS server
- **Design** a custom `net.Dialer` with resolver overrides, control functions, and local address binding
- **Analyze** DNS resolution behavior in Go (CGO vs pure Go resolver)
- **Integrate** custom dialers into `http.Transport` for HTTP client customization

## Why Custom Resolvers and Dialers Matter

The default DNS resolver and dialer work for most cases, but production systems often need customization. You might need to:

- Use a specific DNS server (corporate resolver, DNS-over-HTTPS)
- Override DNS for testing (point a hostname to localhost)
- Bind to a specific local IP address (multi-homed hosts)
- Apply socket options before connecting (SO_REUSEPORT, TCP_FASTOPEN)
- Log or trace every connection establishment

Go's `net.Dialer` and `net.Resolver` are designed for exactly this kind of customization.

## The Problem

Build a DNS-aware HTTP client that:

1. Uses a custom DNS resolver for host lookups
2. Supports DNS overrides for testing (map hostnames to specific IPs)
3. Logs connection details (resolved IP, local/remote address)
4. Integrates with `http.Transport` for HTTP requests

## Requirements

1. **Custom resolver** -- implement a `net.Resolver` that uses a specific DNS server via `Dial` field
2. **DNS override map** -- create a resolver that returns pre-configured IPs for certain hostnames (useful for testing)
3. **Custom dialer** -- create a `net.Dialer` with the custom resolver, connection timeout, and keep-alive
4. **Control function** -- use `Dialer.Control` to log raw socket file descriptors before connection
5. **HTTP integration** -- plug the custom dialer into `http.Transport.DialContext`
6. **DNS lookup** -- implement direct A record, AAAA record, and CNAME lookups using `net.Resolver`
7. **Tests** -- test DNS override, custom resolver, and HTTP client with custom dialer

## Hints

<details>
<summary>Hint 1: DNS override resolver</summary>

```go
type overrideResolver struct {
    overrides map[string][]string // hostname -> IPs
    fallback  *net.Resolver
}

func (r *overrideResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
    if ips, ok := r.overrides[host]; ok {
        return ips, nil
    }
    return r.fallback.LookupHost(ctx, host)
}
```

</details>

<details>
<summary>Hint 2: Custom dialer with resolver</summary>

```go
resolver := &net.Resolver{
    PreferGo: true,
    Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
        // Use a specific DNS server
        d := net.Dialer{Timeout: 5 * time.Second}
        return d.DialContext(ctx, "udp", "8.8.8.8:53")
    },
}

dialer := &net.Dialer{
    Timeout:   10 * time.Second,
    KeepAlive: 30 * time.Second,
    Resolver:  resolver,
}
```

</details>

<details>
<summary>Hint 3: Plugging into http.Transport</summary>

```go
transport := &http.Transport{
    DialContext: dialer.DialContext,
}
client := &http.Client{Transport: transport}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- DNS override maps a hostname to a specific IP
- Custom resolver queries the specified DNS server
- HTTP client uses the custom dialer for connections
- DNS lookup returns A records for a known domain
- The dialer respects timeout configuration

## What's Next

Continue to [11 - HTTP Keep-Alive Analysis](../11-http-keep-alive-analysis/11-http-keep-alive-analysis.md) to analyze HTTP connection reuse at the transport level.

## Summary

- `net.Resolver` customizes DNS resolution; set `PreferGo: true` and `Dial` to use a custom DNS server
- DNS overrides map hostnames to IPs for testing without modifying `/etc/hosts`
- `net.Dialer` combines resolver, timeout, keep-alive, and control functions into a single dialer
- `Dialer.Control` accesses the raw socket file descriptor before connection completes
- Plug custom dialers into `http.Transport.DialContext` to customize all HTTP connections
- Go uses a pure Go resolver by default; set `GODEBUG=netdns=cgo` to force CGO resolver

## Reference

- [net.Resolver](https://pkg.go.dev/net#Resolver)
- [net.Dialer](https://pkg.go.dev/net#Dialer)
- [Name Resolution in Go](https://pkg.go.dev/net#hdr-Name_Resolution)
