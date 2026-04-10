<!--
type: reference
difficulty: advanced
section: [08-networking-and-security]
concepts: [quic-streams, head-of-line-blocking, connection-migration, 0-rtt-quic, loss-recovery, bbr-congestion, udp-muxing, quic-frames]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [tls-internals, protocol-design, tcp-ip-fundamentals, udp-fundamentals]
papers: [Langley et al. 2017 — QUIC at Google, RFC 9000 — QUIC Transport Protocol, Iyengar & Thomson 2021 — QUIC Loss Detection and Congestion Control]
industry_use: [cloudflare-workers, curl, nginx-quic, google-services, facebook-mvfst, http3]
language_contrast: high
-->

# QUIC Protocol

> QUIC eliminates head-of-line blocking at the transport layer by multiplexing independent
> streams over UDP — a dropped packet stalls only the stream it belongs to, not every
> stream on the connection.

## Mental Model

TCP is a single ordered byte stream. Every byte on a TCP connection is sequenced relative
to every other byte. A dropped segment at position 1000 prevents the delivery of all
subsequent data, even if that data arrived successfully. This is head-of-line (HOL)
blocking. HTTP/2 solved HOL blocking at the application layer by multiplexing streams
over one TCP connection — but the TCP layer still enforces global ordering, so a single
dropped segment stalls all HTTP/2 streams until the retransmission arrives.

QUIC moves the transport to UDP and implements its own reliability, flow control, and
congestion control. Because QUIC manages per-stream delivery, a dropped UDP datagram
stalls only the stream(s) whose data was in that datagram. Other streams continue
delivering data independently. This is the primary reason HTTP/3 is built on QUIC rather
than TCP.

QUIC also solves connection mobility. A TCP connection is a 4-tuple: (src IP, src port,
dst IP, dst port). If either side changes its IP address (mobile handoff, NAT rebinding)
or port (some NATs reassign ports after idle periods), the connection breaks. QUIC uses a
64-bit **Connection ID** (CID) to identify connections, independent of IP addresses.
When a client's IP changes, it sends a PATH_CHALLENGE frame on the new path; the server
validates it and continues the same QUIC session. No reconnect, no re-handshake.

The third key insight is that QUIC integrates TLS 1.3 into the handshake. There is no
separate TLS handshake over a TCP connection — QUIC packets carry TLS records natively.
The Handshake takes exactly 1 RTT (or 0 RTT with session resumption), which is the same
as TLS 1.3 over TCP, but the QUIC implementation can begin exchanging application data
before the handshake completes for 0-RTT connections.

The engineering challenge with QUIC is that it runs over UDP, which many middleboxes
(firewalls, NATs, load balancers) handle incorrectly. Google's original deployment found
that ~2% of connections failed due to middlebox interference. The QUIC spec mandates that
implementations must fall back to TCP if QUIC connections fail within 3 seconds.

## Core Concepts

### QUIC Packet Types and Frame Structure

QUIC has two packet number spaces: **long-header packets** (used during handshake) and
**short-header packets** (used for application data). A QUIC packet contains one or more
frames; frames may be reordered and retransmitted independently.

Key frame types:
- `STREAM`: carries data for a specific stream (stream ID, offset, data, fin flag).
- `ACK`: acknowledges received packets (ranges, not individual bytes like TCP).
- `FLOW_CONTROL`: per-stream and per-connection credit updates.
- `CONNECTION_CLOSE`: terminates the connection with an error code.
- `NEW_CONNECTION_ID`: server offers additional connection IDs for migration.
- `PATH_CHALLENGE` / `PATH_RESPONSE`: validate a new network path.
- `CRYPTO`: carries TLS handshake data.

### Stream Multiplexing

Streams are identified by a 62-bit stream ID. The two low-order bits encode the stream
type: client-initiated or server-initiated, bidirectional or unidirectional. A single QUIC
connection can carry millions of concurrent streams, limited only by flow control credits.

Flow control operates at two levels:
1. **Per-stream**: each stream has its own data limit. The receiver advertises
   `MAX_STREAM_DATA` to increase a stream's credit.
2. **Per-connection**: total bytes across all streams. The receiver advertises
   `MAX_DATA` to increase the connection-level credit.

This two-level credit system prevents a fast stream from consuming all connection credits
and starving slow streams.

### Connection Migration

Migration is handled as follows:
1. Client gets a new IP (e.g., switches from WiFi to cellular).
2. Client starts using new source address, sending PATH_CHALLENGE on the new path.
3. Server receives PATH_CHALLENGE and sends PATH_RESPONSE.
4. Both sides switch to using the new path; old path's state is discarded after timeout.

The server advertises multiple Connection IDs via `NEW_CONNECTION_ID` frames so that
migrating clients can use a fresh CID on the new path (preventing the old CID from being
linkable to the new network address — a privacy consideration).

### 0-RTT Connection Establishment

With a session ticket from a previous connection, a QUIC client can send application data
in the first packet flight (0-RTT). The 0-RTT data is encrypted under the `early_secret`
derived from the session ticket's PSK, which the server already knows.

The replay attack surface is identical to TLS 1.3 0-RTT: the server must implement anti-
replay (nonce cache or single-use tickets). Most production deployments restrict 0-RTT to
idempotent HTTP GET requests.

## Implementation: Go

```go
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/quic-go/quic-go"
)

// quicServerConfig returns a QUIC server configuration with TLS 1.3.
// quic-go requires explicit TLS configuration — there is no insecure mode.
func quicServerConfig(certFile, keyFile string) (*quic.Config, *tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load keypair: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"myapp/1"},
	}

	quicCfg := &quic.Config{
		// MaxIdleTimeout controls how long a connection can be idle before it is closed.
		// Set this to match your application's idle connection semantics.
		MaxIdleTimeout: 30 * time.Second,

		// MaxIncomingStreams limits the number of concurrent peer-initiated streams.
		// Setting this prevents a rogue client from opening millions of streams.
		MaxIncomingStreams:    1000,
		MaxIncomingUniStreams: 100,

		// Allow 0-RTT only if you have implemented server-side anti-replay.
		// The quic-go library does NOT implement anti-replay automatically.
		// Setting this to true without anti-replay is a security vulnerability.
		Allow0RTT: false,

		// EnableDatagrams allows unreliable datagram transmission (RFC 9221).
		// Useful for latency-sensitive data that does not require reliable delivery.
		EnableDatagrams: false,
	}

	return quicCfg, tlsCfg, nil
}

// quicEchoServer listens for QUIC connections and echoes data on each stream.
// Demonstrates: connection accept loop, stream multiplexing, per-stream goroutines.
func quicEchoServer(addr, certFile, keyFile string) error {
	quicCfg, tlsCfg, err := quicServerConfig(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	ln, err := quic.ListenAddr(addr, tlsCfg, quicCfg)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()

	log.Printf("QUIC echo server on %s", addr)
	for {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return fmt.Errorf("accept: %w", err)
		}
		go handleQUICConn(conn)
	}
}

func handleQUICConn(conn quic.Connection) {
	defer conn.CloseWithError(0, "done")

	// Log connection identity and remote address.
	// Connection ID is stable across migrations; RemoteAddr changes on migration.
	log.Printf("new connection from %s, ALPN=%s",
		conn.RemoteAddr(), conn.ConnectionState().TLS.NegotiatedProtocol)

	for {
		// AcceptStream blocks until a new stream is opened by the peer.
		// The server can accept multiple concurrent streams from the same connection.
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			// err is non-nil when the connection is closed or reset.
			log.Printf("accept stream: %v", err)
			return
		}
		go handleQUICStream(stream)
	}
}

func handleQUICStream(stream quic.Stream) {
	defer stream.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			// Echo back exactly what was read.
			if _, werr := stream.Write(buf[:n]); werr != nil {
				log.Printf("stream write: %v", werr)
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("stream read: %v", err)
			}
			return
		}
	}
}

// quicClient connects to a QUIC server, opens multiple streams concurrently,
// and measures per-stream latency. Demonstrates HOL-blocking absence.
func quicClient(addr, caCertFile string) error {
	pool := x509.NewCertPool()
	caPEM, err := os.ReadFile(caCertFile)
	if err != nil {
		return fmt.Errorf("read CA cert: %w", err)
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("no certs in CA file")
	}

	tlsCfg := &tls.Config{
		RootCAs:    pool,
		NextProtos: []string{"myapp/1"},
	}

	quicCfg := &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr, tlsCfg, quicCfg)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseWithError(0, "done")

	// Open 5 concurrent streams and measure that they complete independently.
	// In HTTP/2 over TCP, a dropped TCP segment would stall all 5 "streams."
	// In QUIC, each stream's data is retransmitted independently.
	results := make(chan time.Duration, 5)
	for i := range 5 {
		go func(id int) {
			start := time.Now()

			stream, err := conn.OpenStreamSync(ctx)
			if err != nil {
				log.Printf("stream %d: open: %v", id, err)
				results <- -1
				return
			}
			defer stream.Close()

			msg := fmt.Sprintf("hello from stream %d", id)
			if _, err := stream.Write([]byte(msg)); err != nil {
				log.Printf("stream %d: write: %v", id, err)
				results <- -1
				return
			}
			stream.Close() // half-close: signal end of our send side

			var resp [1024]byte
			n, _ := io.ReadFull(stream, resp[:len(msg)])
			_ = n
			results <- time.Since(start)
		}(i)
	}

	for i := range 5 {
		d := <-results
		if d < 0 {
			log.Printf("stream %d failed", i)
		} else {
			log.Printf("stream %d RTT: %v", i, d)
		}
	}

	return nil
}

func main() {
	fmt.Println("QUIC example — run quicEchoServer and quicClient with valid TLS certs")
	fmt.Println("Generate with: mkcert localhost && mkcert -install")
}
```

### Go-specific considerations

**`quic-go` is the standard QUIC library for Go.** It is maintained by the `quic-go`
organization, used in production by Caddy (HTTP/3), and passes the QUIC interoperability
tests. There is no stdlib QUIC support as of Go 1.22.

**Anti-replay for 0-RTT.** `quic-go` sets `Allow0RTT: false` by default. If you enable
0-RTT, you must implement server-side replay detection. The library does not do this for
you. A minimal implementation: a `sync.Map` from session ticket ID to first-seen timestamp,
with a TTL based on the ticket's expiry. Reject any request if the ticket ID is already in
the map.

**Connection IDs and migration.** `quic-go` handles connection migration transparently —
the `Connection.RemoteAddr()` updates after migration, but the application sees a
continuous stream of data. If you log `RemoteAddr` for audit purposes, be aware it can
change mid-session.

**`context.Context` for all QUIC operations.** Unlike `net.Conn`, QUIC operations
accept a context. Always pass a context with a timeout to `AcceptStream`, `OpenStreamSync`,
and `DialAddr`. Failing to set a timeout means a hung server holds goroutines indefinitely.

## Implementation: Rust

```rust
use quinn::{ClientConfig, Endpoint, ServerConfig, TransportConfig};
use rustls::{Certificate, PrivateKey, RootCertStore};
use std::sync::Arc;
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};

/// Build a quinn server endpoint with TLS 1.3.
fn build_server_endpoint(
    addr: &str,
    cert_chain: Vec<Certificate>,
    key: PrivateKey,
) -> anyhow::Result<Endpoint> {
    let mut transport_config = TransportConfig::default();
    transport_config.max_idle_timeout(Some(Duration::from_secs(30).try_into()?));
    // Limit concurrent streams per connection to prevent resource exhaustion.
    transport_config.max_concurrent_bidi_streams(1000u32.into());
    transport_config.max_concurrent_uni_streams(100u32.into());

    let mut server_crypto = rustls::ServerConfig::builder()
        .with_safe_defaults()
        .with_no_client_auth()
        .with_single_cert(cert_chain, key)?;
    // ALPN negotiation: reject connections that do not speak our protocol.
    server_crypto.alpn_protocols = vec![b"myapp/1".to_vec()];

    let mut server_config = ServerConfig::with_crypto(Arc::new(server_crypto));
    server_config.transport_config(Arc::new(transport_config));

    let endpoint = Endpoint::server(server_config, addr.parse()?)?;
    Ok(endpoint)
}

/// Build a quinn client endpoint.
fn build_client_endpoint(ca_cert: Certificate) -> anyhow::Result<Endpoint> {
    let mut root_store = RootCertStore::empty();
    root_store.add(&ca_cert)?;

    let client_crypto = rustls::ClientConfig::builder()
        .with_safe_defaults()
        .with_root_certificates(root_store)
        .with_no_client_auth();

    let client_config = ClientConfig::new(Arc::new(client_crypto));
    let mut endpoint = Endpoint::client("0.0.0.0:0".parse()?)?;
    endpoint.set_default_client_config(client_config);
    Ok(endpoint)
}

/// Echo server: accept connections, spawn a task per connection.
async fn run_echo_server(endpoint: Endpoint) {
    while let Some(connecting) = endpoint.accept().await {
        tokio::spawn(async move {
            match connecting.await {
                Ok(conn) => handle_quic_connection(conn).await,
                Err(e) => eprintln!("connection failed: {e}"),
            }
        });
    }
}

async fn handle_quic_connection(conn: quinn::Connection) {
    let remote = conn.remote_address();
    eprintln!("new connection from {remote}");

    loop {
        match conn.accept_bi().await {
            Ok((mut send, mut recv)) => {
                // Spawn a task per stream — streams are independent.
                tokio::spawn(async move {
                    let mut buf = vec![0u8; 32 * 1024];
                    loop {
                        match recv.read(&mut buf).await {
                            Ok(Some(n)) => {
                                if let Err(e) = send.write_all(&buf[..n]).await {
                                    eprintln!("stream write: {e}");
                                    break;
                                }
                            }
                            Ok(None) => break, // stream end
                            Err(e) => {
                                eprintln!("stream read: {e}");
                                break;
                            }
                        }
                    }
                    let _ = send.finish().await;
                });
            }
            Err(e) => {
                eprintln!("accept stream from {remote}: {e}");
                return;
            }
        }
    }
}

/// Client: open multiple concurrent streams and measure per-stream latency.
async fn run_client(endpoint: &Endpoint, server_addr: &str) -> anyhow::Result<()> {
    use std::time::Instant;

    let connecting = endpoint.connect(server_addr.parse()?, "localhost")?;
    let conn = connecting.await?;

    let mut handles = Vec::new();
    for i in 0..5usize {
        let conn = conn.clone();
        handles.push(tokio::spawn(async move {
            let start = Instant::now();
            let (mut send, mut recv) = conn.open_bi().await?;
            let msg = format!("hello from stream {i}");
            send.write_all(msg.as_bytes()).await?;
            send.finish().await?;

            let mut response = Vec::new();
            recv.read_to_end(1024, &mut response).await?;
            Ok::<_, anyhow::Error>((i, start.elapsed()))
        }));
    }

    for handle in handles {
        match handle.await? {
            Ok((i, elapsed)) => println!("stream {i} RTT: {elapsed:?}"),
            Err(e) => eprintln!("stream error: {e}"),
        }
    }
    Ok(())
}

#[tokio::main]
async fn main() {
    println!("QUIC example — run with valid TLS certs (use rcgen or mkcert)");
}
```

### Rust-specific considerations

**`quinn` is the standard QUIC library for Rust.** It is used in production by
Cloudflare's `quiche` (their own QUIC implementation) and by the Rust async ecosystem.
`quinn` uses `rustls` for the TLS layer and `tokio` for async I/O.

**Cloneable `Connection` handle.** Unlike Go where `quic.Connection` is a single value,
`quinn::Connection` implements `Clone` cheaply (it is an `Arc` internally). This makes
it easy to share a connection across multiple async tasks for concurrent stream
operations.

**`accept_bi` vs `accept_uni`.** Use `accept_bi` for bidirectional streams (request-
response pattern), `accept_uni` for server-push or notification streams. `open_bi`
opens a bidirectional stream from the client side; the client and server must agree on
who initiates to avoid deadlock (both waiting for the other to initiate).

**Stream finish semantics.** Call `send.finish()` to half-close the send side of a
stream. The peer can still send data on the reverse direction. `conn.close()` closes
the entire connection with a final error code (analogous to TCP RST).

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| QUIC library | `quic-go` | `quinn` |
| TLS integration | `crypto/tls` Config | `rustls` ServerConfig/ClientConfig |
| Connection type | `quic.Connection` (single handle) | `quinn::Connection` (clonable Arc) |
| Stream types | `quic.Stream` (bidi), `quic.ReceiveStream` | `RecvStream` + `SendStream` pairs |
| 0-RTT support | `Allow0RTT: true` (no built-in anti-replay) | `quinn::Incoming::accept_0rtt()` |
| Connection migration | Transparent, `RemoteAddr()` updates | Transparent |
| Datagram support | `EnableDatagrams: true` | `config.datagram_receive_buffer_size` |
| Congestion control | CUBIC (default), BBRv2 (pluggable) | CUBIC (default), BBRv2 via trait |
| Production usage | Caddy HTTP/3, Cloudflare WARP | Cloudflare quiche, Fastly |
| ALPN negotiation | `tls.Config.NextProtos` | `rustls::ServerConfig.alpn_protocols` |

## Production War Stories

**Google's QUIC deployment (2013–2017).** Google deployed QUIC (then called "QUIC" but
with a different design than RFC 9000) to all Google Search, YouTube, and GMail traffic.
At peak, QUIC served ~30% of all Google traffic. The measured latency improvement was
5–15% for cold connections and up to 30% on lossy mobile networks. The 2017 paper
("QUIC at Google") documents the engineering challenges: middlebox ossification (firewalls
blocking UDP 443), CPU cost of user-space congestion control (vs kernel TCP), and the
complexity of per-connection state management without kernel support.

**Cloudflare QUIC and middlebox interference (2022).** Cloudflare measured that ~7% of
QUIC connections failed due to middleboxes blocking UDP 443 or mangling QUIC packets.
Their solution: QUIC connection racing — simultaneously attempt QUIC and TCP, use
whichever connects first, cancel the other. This is now the standard recommendation in
RFC 9312 ("Manageability of the QUIC Transport Protocol").

**Facebook MVFST and 0-RTT at scale (2021).** Facebook's QUIC implementation (MVFST,
used in WhatsApp, Instagram, Messenger) deployed 0-RTT resumption for image and video
downloads. Their anti-replay implementation uses a Bloom filter per session ticket,
accepting a small false-positive rate (Bloom filter false positives cause unnecessary
replay rejection) in exchange for O(1) memory per session. This is a real production
tradeoff: exact replay detection requires storing every session ticket ID, which is
prohibitive at their scale.

## Security Analysis

**Amplification attacks.** QUIC's design limits amplification to 3x before path
validation. A server may send at most 3x the bytes received from a new client until the
client's IP is verified via a handshake round-trip. This prevents QUIC from being used
as a DDoS amplification vector (unlike DNS, which can amplify 50–100x).

**Stateless Reset.** QUIC provides a stateless reset mechanism: a server that has lost
its state (e.g., after a restart) can send a stateless reset packet containing a
pre-computed token that proves the server was the original connection endpoint. This
allows clean connection termination without keeping per-connection state across restarts.
The token is derived from the connection ID and a server-side secret — it must be kept
confidential to prevent spoofed resets.

**Connection ID linkability.** Multiple connection IDs (CIDs) are offered so that clients
can use a different CID after migration, preventing an observer from linking the old and
new network paths to the same session. If you log connection IDs for debugging, be aware
that this creates a side-channel that can re-link migrated connections.

**0-RTT replay.** The same replay attack surface as TLS 1.3 0-RTT. QUIC implementations
that accept 0-RTT data for state-mutating operations are vulnerable to replay attacks.
The QUIC spec requires servers to define which application-level operations are safe to
process in 0-RTT.

## Common Pitfalls

1. **Forgetting to set `MaxIncomingStreams`.** Without a limit, a rogue client can open
   2^62 streams and exhaust server memory. Set `MaxIncomingStreams` to a value appropriate
   for your application's concurrency model.

2. **Not implementing 0-RTT anti-replay.** Enabling `Allow0RTT: true` in `quic-go`
   without implementing server-side replay detection is a security vulnerability. Use
   single-use session tickets or a per-ticket nonce cache.

3. **Stream flow control starvation.** If a client sends data on one stream faster than
   the server reads it, the stream's flow control window fills up. If the application
   never reads from the stream (goroutine/task is blocked), the connection stalls. Always
   consume stream data promptly or cancel the stream with a reset frame.

4. **Not handling `accept_bi` / `accept_uni` exhaustion.** If a peer opens more streams
   than `MaxIncomingStreams`, quinn/quic-go sends a `STREAMS_BLOCKED` frame. If your
   accept loop does not have backpressure (always accepting without bound), you overflow
   the stream limit anyway. Use a semaphore to limit concurrent active streams.

5. **Using QUIC for all traffic without TCP fallback.** Approximately 5–10% of paths
   block UDP 443. Always implement a TCP fallback (Happy Eyeballs variant for QUIC/TCP
   racing). `quic-go` has experimental support for this.

## Exercises

**Exercise 1** (30 min): Use `curl` with HTTP/3 to fetch `https://quic.nginx.org` and
`https://cloudflare-quic.com`. Capture the traffic with `tcpdump -i any -w quic.pcap port 443`.
Open in Wireshark. Filter `quic` in Wireshark and identify the QUIC long-header Initial
packets, the Server Hello, and the application data short-header packets.

**Exercise 2** (2–4h): Build a QUIC file server in Go using `quic-go` that serves files
over bidirectional streams. The client opens a stream, sends a filename, and the server
responds with the file contents. Measure the throughput for a 100MB file transfer.
Compare with a TCP-based implementation (use `net.Listen` and `net.Dial`). For this
comparison, disable lossy network simulation (both will be similar on loopback).

**Exercise 3** (4–8h): Simulate HOL blocking. Use `tc netem` or Toxiproxy to add 1%
packet loss to loopback (`tc qdisc add dev lo root netem loss 1%`). Run a benchmark
that transfers 10 files simultaneously over (a) HTTP/2 over TCP and (b) QUIC / HTTP/3.
Measure per-file latency. Plot the tail latency (p95, p99). The QUIC advantage should be
visible at p99.

**Exercise 4** (8–15h): Implement QUIC connection migration in Rust using `quinn`. Start
a client connected via one IP, then change the outbound IP (e.g., using two network
interfaces or network namespaces). Verify that the QUIC session continues without
reconnection. Measure the migration latency (time between sending on old path and first
data received on new path). Implement a callback that logs migration events with the old
and new `SocketAddr`.

## Further Reading

### Foundational Papers

- Langley et al. — "The QUIC Transport Protocol: Design and Internet-Scale Deployment"
  (SIGCOMM 2017). Google's deployment paper: measurement methodology, latency improvements,
  and middlebox challenges.
- RFC 9000 — "QUIC: A UDP-Based Multiplexed and Secure Transport". The specification.
  Section 9 (connection migration) and section 7 (flow control) are the most important
  for implementers.
- RFC 9002 — "QUIC Loss Detection and Congestion Control". How QUIC implements reliable
  delivery and congestion avoidance on top of UDP.

### Books

- Huitema, "QUIC Implementation and Deployment" (Wiley, 2022) — the only book-length
  treatment of QUIC written by one of the protocol designers.

### Production Code to Read

- `quic-go` source (`quic-go/quic-go`) — particularly `connection.go` for the connection
  state machine and `flow_control.go` for the credit-based flow control implementation.
- Cloudflare `quiche` (`cloudflare/quiche`) — Rust QUIC implementation used in nginx-quic
  and curl. The `src/packet.rs` and `src/stream/` are the best entry points.
- `quinn/src/` — Rust QUIC. `connection.rs` implements the state machine that manages
  stream multiplexing, flow control, and connection migration.

### Security Advisories / CVEs to Study

- CVE-2023-44487 (HTTP/2 Rapid Reset attack) — exploits stream reset to amplify server
  CPU work without corresponding client cost. Not specific to QUIC, but the same stream
  cancellation mechanism exists in QUIC. Mitigated by rate-limiting stream resets.
- QUIC amplification attack (RFC 9000 §8.1) — not a CVE, but a design concern documented
  in the spec. The 3x amplification limit is the mitigation.
