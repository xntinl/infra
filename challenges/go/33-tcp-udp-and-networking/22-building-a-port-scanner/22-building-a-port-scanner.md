# 22. Building a Port Scanner

<!--
difficulty: insane
concepts: [port-scanning, syn-scan, connect-scan, concurrency-limiter, service-detection, banner-grabbing, rate-limiting]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [tcp-server-and-client, concurrent-tcp-server, connection-timeouts-and-deadlines, dns-resolver-and-custom-dialer]
-->

## Prerequisites

- Go 1.22+ installed
- Completed TCP Server and Connection Timeouts exercises
- Understanding of TCP three-way handshake and port states (open, closed, filtered)
- Experience with goroutine concurrency control (semaphores, worker pools)

## Learning Objectives

- **Create** a concurrent TCP port scanner with configurable parallelism and timeouts
- **Implement** service detection via banner grabbing and well-known port identification
- **Design** a rate-limited scanning engine that balances speed against resource exhaustion

## The Challenge

Port scanners are fundamental network tools that determine which ports on a host are accepting TCP connections. A naive scanner tries one port at a time, but scanning 65,535 ports sequentially with a 1-second timeout would take over 18 hours. Production scanners use concurrent connection attempts with controlled parallelism, short timeouts, and service detection.

Your task is to build a concurrent TCP connect scanner that scans a target host (or CIDR range), identifies open ports, performs basic service detection by reading initial banners, and reports results in a structured format. The scanner must control concurrency to avoid overwhelming the target or exhausting local resources (file descriptors, ephemeral ports).

## Requirements

1. Accept a target specification: single IP, hostname, or CIDR range (e.g., `192.168.1.0/24`)
2. Accept a port specification: single port, range (1-1024), list (22,80,443), or all (1-65535)
3. Implement TCP connect scanning using `net.DialTimeout` with a configurable timeout per port (default 500ms)
4. Limit concurrent connections using a semaphore (buffered channel) with a configurable concurrency level (default 100)
5. Implement banner grabbing: after connecting to an open port, read up to 1024 bytes with a short timeout (1 second) to capture service banners (SSH version, HTTP response, SMTP greeting)
6. Identify well-known services by port number (22=SSH, 80=HTTP, 443=HTTPS, etc.) and by banner content
7. Report results as structured data: host, port, state (open/closed/filtered), service name, banner (if captured), and scan duration
8. Implement rate limiting: maximum connections per second to avoid triggering intrusion detection systems
9. Support scanning multiple hosts concurrently with a separate concurrency limit for hosts vs ports
10. Write tests that start local TCP listeners on known ports and verify the scanner detects them

## Hints

- Use a worker pool pattern: create N worker goroutines that read `(host, port)` tuples from a channel and report results on an output channel
- Distinguish closed (connection refused -- immediate RST) from filtered (timeout -- no response) by checking the error type from `net.DialTimeout`
- For banner grabbing, set a read deadline immediately after connecting: `conn.SetReadDeadline(time.Now().Add(time.Second))`
- Use `net.ParseCIDR` and iterate through the IP range for CIDR scanning
- Rate limiting is best implemented with a `time.Ticker` that gates when new connections can be initiated
- Be careful with file descriptor limits: `ulimit -n` on most systems defaults to 1024

## Success Criteria

1. The scanner correctly identifies open, closed, and filtered ports on a test target
2. Banner grabbing captures SSH, HTTP, and SMTP banners from open ports
3. Concurrency is properly limited -- at no point are more than N connections open simultaneously
4. Scanning 1000 ports on localhost completes in under 5 seconds with 100 concurrent workers
5. CIDR range scanning correctly expands the range and scans all hosts
6. Rate limiting reduces scan speed to the configured connections per second
7. Results are reported in a structured format with accurate timing information
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Nmap Port Scanning Techniques](https://nmap.org/book/man-port-scanning-techniques.html) -- reference for scanning strategies and port states
- [Go net.DialTimeout](https://pkg.go.dev/net#DialTimeout) -- TCP connect with timeout
- [Go net.ParseCIDR](https://pkg.go.dev/net#ParseCIDR) -- CIDR range parsing
- [Well-Known Ports (IANA)](https://www.iana.org/assignments/service-names-port-numbers/) -- port-to-service mapping

## What's Next

Continue to [23 - DNS Recursive Resolver](../23-dns-recursive-resolver/23-dns-recursive-resolver.md) to build a DNS resolver that queries root servers and follows delegation chains.

## Summary

- TCP connect scanning uses `net.DialTimeout` to determine port state: open (connected), closed (RST), or filtered (timeout)
- Concurrency control via semaphores prevents file descriptor exhaustion and target overload
- Banner grabbing reads the first bytes from open connections to identify running services
- CIDR range expansion enables subnet scanning from a single specification
- Rate limiting with `time.Ticker` prevents triggering intrusion detection systems
- Worker pool patterns efficiently distribute scanning work across goroutines
