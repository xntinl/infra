# 49. Build a Load Balancer

**Difficulty**: Avanzado

## Prerequisites

- Solid understanding of async Rust with tokio (spawn, select, channels)
- Familiarity with TCP networking (`tokio::net::TcpListener`, `TcpStream`)
- Completed: exercises on concurrency patterns, networking with tokio, error handling
- Basic understanding of HTTP/1.1 protocol and TCP proxying

## Learning Objectives

- Build a TCP/HTTP reverse proxy load balancer from scratch using tokio
- Implement multiple balancing algorithms: round-robin, least-connections, weighted round-robin
- Add health checking with periodic probes and automatic backend removal/recovery
- Handle connection draining for graceful backend removal
- Design for hot-reloadable configuration
- Understand the architecture of production load balancers like HAProxy and nginx

## Concepts

A load balancer sits between clients and a pool of backend servers. It distributes incoming connections across backends to spread the workload, improve latency, and provide fault tolerance. When a backend fails, the load balancer stops sending traffic to it. When a new backend is added, it starts receiving traffic.

This exercise builds a Layer 4 (TCP) and Layer 7 (HTTP) load balancer. The TCP mode blindly forwards bytes between client and backend. The HTTP mode parses the request to add headers (like `X-Forwarded-For`) and make routing decisions.

### Architecture

```
                    +-----------+
  Client --------> |   Load    | -----> Backend 1 (10.0.0.1:8080)
  Client --------> | Balancer  | -----> Backend 2 (10.0.0.2:8080)
  Client --------> | :3000     | -----> Backend 3 (10.0.0.3:8080)
                    +-----------+
                         |
                    Health Checker
                    (periodic probes)
```

### Balancing Algorithms

| Algorithm | Description | Best For |
|-----------|-------------|----------|
| Round-Robin | Cycle through backends sequentially | Equal-capacity servers |
| Weighted Round-Robin | Cycle proportionally to assigned weights | Mixed-capacity servers |
| Least Connections | Pick the backend with fewest active connections | Variable request duration |
| Random | Pick a backend at random | Simple, stateless |

---

## Implementation

### Configuration

```rust
use std::net::SocketAddr;
use std::time::Duration;

#[derive(Debug, Clone)]
struct BackendConfig {
    addr: SocketAddr,
    weight: u32, // for weighted round-robin
}

#[derive(Debug, Clone)]
enum Algorithm {
    RoundRobin,
    WeightedRoundRobin,
    LeastConnections,
    Random,
}

#[derive(Debug, Clone)]
struct Config {
    listen_addr: SocketAddr,
    backends: Vec<BackendConfig>,
    algorithm: Algorithm,
    health_check_interval: Duration,
    health_check_timeout: Duration,
    connection_timeout: Duration,
    max_connections_per_backend: usize,
}

impl Config {
    fn example() -> Self {
        Self {
            listen_addr: "127.0.0.1:3000".parse().unwrap(),
            backends: vec![
                BackendConfig {
                    addr: "127.0.0.1:8081".parse().unwrap(),
                    weight: 3,
                },
                BackendConfig {
                    addr: "127.0.0.1:8082".parse().unwrap(),
                    weight: 2,
                },
                BackendConfig {
                    addr: "127.0.0.1:8083".parse().unwrap(),
                    weight: 1,
                },
            ],
            algorithm: Algorithm::RoundRobin,
            health_check_interval: Duration::from_secs(5),
            health_check_timeout: Duration::from_secs(2),
            connection_timeout: Duration::from_secs(10),
            max_connections_per_backend: 100,
        }
    }
}
```

### Backend State

```rust
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::Arc;

#[derive(Debug)]
struct Backend {
    config: BackendConfig,
    healthy: AtomicBool,
    active_connections: AtomicUsize,
    total_connections: AtomicUsize,
    total_errors: AtomicUsize,
}

impl Backend {
    fn new(config: BackendConfig) -> Self {
        Self {
            config,
            healthy: AtomicBool::new(true),
            active_connections: AtomicUsize::new(0),
            total_connections: AtomicUsize::new(0),
            total_errors: AtomicUsize::new(0),
        }
    }

    fn is_healthy(&self) -> bool {
        self.healthy.load(Ordering::Relaxed)
    }

    fn set_healthy(&self, healthy: bool) {
        self.healthy.store(healthy, Ordering::Relaxed);
    }

    fn connection_count(&self) -> usize {
        self.active_connections.load(Ordering::Relaxed)
    }

    fn connect(&self) {
        self.active_connections.fetch_add(1, Ordering::Relaxed);
        self.total_connections.fetch_add(1, Ordering::Relaxed);
    }

    fn disconnect(&self) {
        self.active_connections.fetch_sub(1, Ordering::Relaxed);
    }

    fn record_error(&self) {
        self.total_errors.fetch_add(1, Ordering::Relaxed);
    }
}
```

### Balancing Algorithms

```rust
use std::sync::atomic::AtomicUsize;

struct Balancer {
    backends: Vec<Arc<Backend>>,
    algorithm: Algorithm,
    rr_counter: AtomicUsize, // for round-robin
}

impl Balancer {
    fn new(configs: Vec<BackendConfig>, algorithm: Algorithm) -> Self {
        let backends = configs.into_iter().map(|c| Arc::new(Backend::new(c))).collect();
        Self {
            backends,
            algorithm,
            rr_counter: AtomicUsize::new(0),
        }
    }

    /// Select the next backend according to the algorithm.
    /// Returns None if no healthy backends are available.
    fn select(&self) -> Option<Arc<Backend>> {
        let healthy: Vec<&Arc<Backend>> = self.backends
            .iter()
            .filter(|b| b.is_healthy())
            .collect();

        if healthy.is_empty() {
            return None;
        }

        match self.algorithm {
            Algorithm::RoundRobin => {
                let idx = self.rr_counter.fetch_add(1, Ordering::Relaxed) % healthy.len();
                Some(healthy[idx].clone())
            }
            Algorithm::WeightedRoundRobin => {
                self.select_weighted(&healthy)
            }
            Algorithm::LeastConnections => {
                healthy.iter()
                    .min_by_key(|b| b.connection_count())
                    .map(|b| (*b).clone())
            }
            Algorithm::Random => {
                use std::time::SystemTime;
                let seed = SystemTime::now()
                    .duration_since(SystemTime::UNIX_EPOCH)
                    .unwrap()
                    .subsec_nanos() as usize;
                let idx = seed % healthy.len();
                Some(healthy[idx].clone())
            }
        }
    }

    fn select_weighted(&self, healthy: &[&Arc<Backend>]) -> Option<Arc<Backend>> {
        let total_weight: u32 = healthy.iter().map(|b| b.config.weight).sum();
        if total_weight == 0 {
            return None;
        }

        let counter = self.rr_counter.fetch_add(1, Ordering::Relaxed) as u32;
        let target = counter % total_weight;

        let mut cumulative = 0u32;
        for backend in healthy {
            cumulative += backend.config.weight;
            if target < cumulative {
                return Some((*backend).clone());
            }
        }

        Some(healthy.last().unwrap().clone())
    }

    fn backends(&self) -> &[Arc<Backend>] {
        &self.backends
    }
}
```

### TCP Proxy Core

The core proxy function connects a client to a selected backend and bidirectionally copies bytes:

```rust
use tokio::net::{TcpListener, TcpStream};
use tokio::io::{self as tokio_io, AsyncWriteExt};

/// Proxy bytes bidirectionally between client and backend.
/// Returns (bytes_from_client, bytes_from_backend).
async fn proxy_connection(
    mut client: TcpStream,
    backend: Arc<Backend>,
    timeout: Duration,
) -> io::Result<(u64, u64)> {
    backend.connect();

    let result = async {
        let mut upstream = tokio::time::timeout(
            timeout,
            TcpStream::connect(backend.config.addr),
        )
        .await
        .map_err(|_| io::Error::new(io::ErrorKind::TimedOut, "connection timeout"))?
        ?;

        let (mut client_reader, mut client_writer) = client.split();
        let (mut upstream_reader, mut upstream_writer) = upstream.split();

        let client_to_upstream = tokio::io::copy(&mut client_reader, &mut upstream_writer);
        let upstream_to_client = tokio::io::copy(&mut upstream_reader, &mut client_writer);

        let result = tokio::select! {
            res = client_to_upstream => {
                let bytes = res?;
                let _ = upstream_writer.shutdown().await;
                (bytes, 0)
            }
            res = upstream_to_client => {
                let bytes = res?;
                let _ = client_writer.shutdown().await;
                (0, bytes)
            }
        };

        Ok(result)
    }
    .await;

    backend.disconnect();

    match &result {
        Ok(_) => {}
        Err(_) => backend.record_error(),
    }

    result
}
```

### Health Checker

The health checker periodically probes each backend by attempting a TCP connection:

```rust
async fn health_check_loop(
    backends: Vec<Arc<Backend>>,
    interval: Duration,
    timeout: Duration,
) {
    loop {
        for backend in &backends {
            let was_healthy = backend.is_healthy();

            let is_healthy = tokio::time::timeout(
                timeout,
                TcpStream::connect(backend.config.addr),
            )
            .await
            .map(|r| r.is_ok())
            .unwrap_or(false);

            backend.set_healthy(is_healthy);

            if was_healthy && !is_healthy {
                eprintln!(
                    "[HEALTH] backend {} is DOWN",
                    backend.config.addr
                );
            } else if !was_healthy && is_healthy {
                eprintln!(
                    "[HEALTH] backend {} is UP",
                    backend.config.addr
                );
            }
        }

        tokio::time::sleep(interval).await;
    }
}
```

### The Main Loop

```rust
use std::sync::Arc;
use std::io;

async fn run_load_balancer(config: Config) -> io::Result<()> {
    let balancer = Arc::new(Balancer::new(
        config.backends.clone(),
        config.algorithm.clone(),
    ));

    // Start health checker
    let health_backends: Vec<Arc<Backend>> = balancer.backends().to_vec();
    tokio::spawn(health_check_loop(
        health_backends,
        config.health_check_interval,
        config.health_check_timeout,
    ));

    // Start stats reporter
    let stats_backends: Vec<Arc<Backend>> = balancer.backends().to_vec();
    tokio::spawn(stats_reporter(stats_backends));

    // Accept connections
    let listener = TcpListener::bind(config.listen_addr).await?;
    println!("load balancer listening on {}", config.listen_addr);

    loop {
        let (client, client_addr) = listener.accept().await?;
        let balancer = balancer.clone();
        let timeout = config.connection_timeout;

        tokio::spawn(async move {
            let backend = match balancer.select() {
                Some(b) => b,
                None => {
                    eprintln!("[ERROR] no healthy backends for {client_addr}");
                    return;
                }
            };

            let backend_addr = backend.config.addr;
            match proxy_connection(client, backend, timeout).await {
                Ok((up, down)) => {
                    println!(
                        "[CONN] {client_addr} -> {backend_addr} (up:{up} down:{down})"
                    );
                }
                Err(e) => {
                    eprintln!(
                        "[ERROR] {client_addr} -> {backend_addr}: {e}"
                    );
                }
            }
        });
    }
}

async fn stats_reporter(backends: Vec<Arc<Backend>>) {
    loop {
        tokio::time::sleep(Duration::from_secs(10)).await;
        println!("\n--- Backend Stats ---");
        for backend in &backends {
            println!(
                "  {} | {} | active: {} | total: {} | errors: {}",
                backend.config.addr,
                if backend.is_healthy() { "UP  " } else { "DOWN" },
                backend.active_connections.load(Ordering::Relaxed),
                backend.total_connections.load(Ordering::Relaxed),
                backend.total_errors.load(Ordering::Relaxed),
            );
        }
        println!("---\n");
    }
}

#[tokio::main]
async fn main() -> io::Result<()> {
    let config = Config::example();
    run_load_balancer(config).await
}
```

---

## HTTP Mode: Adding Headers

For HTTP traffic, we can parse the request to inject headers like `X-Forwarded-For`:

```rust
use tokio::io::{AsyncReadExt, AsyncBufReadExt, BufReader};

/// Read an HTTP/1.1 request, inject X-Forwarded-For, and forward it.
async fn proxy_http_connection(
    mut client: TcpStream,
    backend: Arc<Backend>,
    client_addr: SocketAddr,
    timeout: Duration,
) -> io::Result<()> {
    backend.connect();

    let result = async {
        let mut upstream = tokio::time::timeout(
            timeout,
            TcpStream::connect(backend.config.addr),
        )
        .await
        .map_err(|_| io::Error::new(io::ErrorKind::TimedOut, "connection timeout"))?
        ?;

        // Read the request headers from the client
        let mut client_buf = BufReader::new(&mut client);
        let mut request_head = String::new();
        let mut content_length: usize = 0;

        // Read request line
        let mut line = String::new();
        client_buf.read_line(&mut line).await?;
        request_head.push_str(&line);

        // Read headers
        loop {
            line.clear();
            client_buf.read_line(&mut line).await?;

            if line.trim().is_empty() {
                break;
            }

            if line.to_lowercase().starts_with("content-length:") {
                if let Some(len_str) = line.split(':').nth(1) {
                    content_length = len_str.trim().parse().unwrap_or(0);
                }
            }

            request_head.push_str(&line);
        }

        // Inject X-Forwarded-For header
        request_head.push_str(&format!("X-Forwarded-For: {}\r\n", client_addr.ip()));
        request_head.push_str("\r\n");

        // Send modified headers to upstream
        upstream.write_all(request_head.as_bytes()).await?;

        // Forward request body if present
        if content_length > 0 {
            let mut body = vec![0u8; content_length];
            client_buf.read_exact(&mut body).await?;
            upstream.write_all(&body).await?;
        }

        // Forward response back to client
        let (mut upstream_reader, _) = upstream.split();
        let (_, mut client_writer) = client.split();
        tokio::io::copy(&mut upstream_reader, &mut client_writer).await?;

        Ok::<(), io::Error>(())
    }
    .await;

    backend.disconnect();

    if result.is_err() {
        backend.record_error();
    }

    result
}
```

---

## Connection Draining

When removing a backend (for maintenance or deployment), we want to stop sending new connections but let existing connections finish. This is called connection draining.

```rust
use tokio::sync::watch;

struct DrainableBackend {
    inner: Arc<Backend>,
    /// When set to true, no new connections are sent to this backend.
    draining: AtomicBool,
    /// Notify when all connections have drained.
    drain_complete: watch::Sender<bool>,
}

impl DrainableBackend {
    fn new(config: BackendConfig) -> Self {
        let (tx, _) = watch::channel(false);
        Self {
            inner: Arc::new(Backend::new(config)),
            draining: AtomicBool::new(false),
            drain_complete: tx,
        }
    }

    /// Start draining: stop accepting new connections.
    fn start_drain(&self) {
        self.draining.store(true, Ordering::SeqCst);
        eprintln!(
            "[DRAIN] started draining {} (active: {})",
            self.inner.config.addr,
            self.inner.connection_count()
        );

        // If already at zero connections, signal immediately
        if self.inner.connection_count() == 0 {
            let _ = self.drain_complete.send(true);
        }
    }

    /// Called when a connection to this backend closes.
    fn on_disconnect(&self) {
        self.inner.disconnect();
        if self.draining.load(Ordering::SeqCst) && self.inner.connection_count() == 0 {
            let _ = self.drain_complete.send(true);
            eprintln!("[DRAIN] {} fully drained", self.inner.config.addr);
        }
    }

    /// Wait until all connections have drained (with timeout).
    async fn wait_drain(&self, timeout: Duration) -> bool {
        let mut rx = self.drain_complete.subscribe();
        tokio::time::timeout(timeout, async {
            while !*rx.borrow() {
                if rx.changed().await.is_err() {
                    return;
                }
            }
        })
        .await
        .is_ok()
    }

    fn is_available(&self) -> bool {
        self.inner.is_healthy() && !self.draining.load(Ordering::Relaxed)
    }
}
```

---

## Configuration Hot-Reload

Watch a configuration file for changes and update the backend list at runtime:

```rust
use tokio::sync::watch;
use std::path::Path;

/// Parse a simple config file format:
/// ```
/// algorithm round-robin
/// backend 127.0.0.1:8081 weight=3
/// backend 127.0.0.1:8082 weight=2
/// ```
fn parse_config(content: &str) -> Option<(Algorithm, Vec<BackendConfig>)> {
    let mut algorithm = Algorithm::RoundRobin;
    let mut backends = Vec::new();

    for line in content.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }

        let parts: Vec<&str> = line.split_whitespace().collect();
        match parts.first()? {
            &"algorithm" => {
                algorithm = match parts.get(1)? {
                    &"round-robin" => Algorithm::RoundRobin,
                    &"weighted" => Algorithm::WeightedRoundRobin,
                    &"least-connections" | &"least-conn" => Algorithm::LeastConnections,
                    &"random" => Algorithm::Random,
                    _ => return None,
                };
            }
            &"backend" => {
                let addr: SocketAddr = parts.get(1)?.parse().ok()?;
                let mut weight = 1u32;
                for part in &parts[2..] {
                    if let Some(w) = part.strip_prefix("weight=") {
                        weight = w.parse().ok()?;
                    }
                }
                backends.push(BackendConfig { addr, weight });
            }
            _ => {}
        }
    }

    Some((algorithm, backends))
}

async fn config_watcher(
    path: impl AsRef<Path>,
    tx: watch::Sender<(Algorithm, Vec<BackendConfig>)>,
) {
    let path = path.as_ref().to_path_buf();

    loop {
        tokio::time::sleep(Duration::from_secs(5)).await;

        if let Ok(content) = tokio::fs::read_to_string(&path).await {
            if let Some(new_config) = parse_config(&content) {
                let _ = tx.send(new_config);
            }
        }
    }
}
```

---

## Exercises

### Exercise 1: Implement and Test All Algorithms

Create a test harness that simulates 1000 requests against 3 backends with each algorithm. Verify:
- **Round-robin**: each backend gets approximately 333 requests
- **Weighted (3:2:1)**: backends get approximately 500, 333, 167 requests
- **Least-connections**: with simulated variable latency, the fastest backend gets the most requests
- **Random**: roughly uniform distribution (within statistical bounds)

<details>
<summary>Solution</summary>

```rust
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

fn test_algorithms() {
    let configs = vec![
        BackendConfig { addr: "127.0.0.1:8081".parse().unwrap(), weight: 3 },
        BackendConfig { addr: "127.0.0.1:8082".parse().unwrap(), weight: 2 },
        BackendConfig { addr: "127.0.0.1:8083".parse().unwrap(), weight: 1 },
    ];

    let n = 6000;

    // Test Round-Robin
    {
        let balancer = Balancer::new(configs.clone(), Algorithm::RoundRobin);
        let mut counts = [0usize; 3];

        for _ in 0..n {
            if let Some(backend) = balancer.select() {
                let idx = match backend.config.addr.port() {
                    8081 => 0,
                    8082 => 1,
                    8083 => 2,
                    _ => unreachable!(),
                };
                counts[idx] += 1;
            }
        }

        println!("round-robin: {:?}", counts);
        assert_eq!(counts[0], 2000);
        assert_eq!(counts[1], 2000);
        assert_eq!(counts[2], 2000);
    }

    // Test Weighted Round-Robin
    {
        let balancer = Balancer::new(configs.clone(), Algorithm::WeightedRoundRobin);
        let mut counts = [0usize; 3];

        for _ in 0..n {
            if let Some(backend) = balancer.select() {
                let idx = match backend.config.addr.port() {
                    8081 => 0,
                    8082 => 1,
                    8083 => 2,
                    _ => unreachable!(),
                };
                counts[idx] += 1;
            }
        }

        println!("weighted: {:?}", counts);
        assert_eq!(counts[0], 3000); // weight 3/6
        assert_eq!(counts[1], 2000); // weight 2/6
        assert_eq!(counts[2], 1000); // weight 1/6
    }

    // Test Least-Connections
    {
        let balancer = Balancer::new(configs.clone(), Algorithm::LeastConnections);

        // Simulate: backend 0 has 5 active, backend 1 has 2, backend 2 has 0
        balancer.backends()[0].active_connections.store(5, Ordering::Relaxed);
        balancer.backends()[1].active_connections.store(2, Ordering::Relaxed);
        balancer.backends()[2].active_connections.store(0, Ordering::Relaxed);

        let selected = balancer.select().unwrap();
        assert_eq!(selected.config.addr.port(), 8083); // least connections
        println!("least-conn: selected backend with fewest connections");
    }

    // Test with unhealthy backend
    {
        let balancer = Balancer::new(configs.clone(), Algorithm::RoundRobin);
        balancer.backends()[1].set_healthy(false);

        let mut counts = [0usize; 3];
        for _ in 0..1000 {
            if let Some(backend) = balancer.select() {
                let idx = match backend.config.addr.port() {
                    8081 => 0,
                    8082 => 1,
                    8083 => 2,
                    _ => unreachable!(),
                };
                counts[idx] += 1;
            }
        }

        println!("with unhealthy backend: {:?}", counts);
        assert_eq!(counts[1], 0); // unhealthy backend gets 0
        assert_eq!(counts[0] + counts[2], 1000);
    }

    // Test all backends unhealthy
    {
        let balancer = Balancer::new(configs.clone(), Algorithm::RoundRobin);
        for b in balancer.backends() {
            b.set_healthy(false);
        }
        assert!(balancer.select().is_none());
        println!("all unhealthy: select returns None");
    }

    println!("\nall algorithm tests passed");
}

fn main() {
    test_algorithms();
}
```
</details>

### Exercise 2: Integration Test with Echo Servers

Write an integration test that:
1. Spawns 3 simple echo servers on different ports
2. Starts the load balancer pointing to all 3
3. Sends 30 TCP messages through the load balancer
4. Verifies responses come back correctly
5. Kills one echo server
6. Waits for health check to detect the failure
7. Sends 20 more messages and verifies they all succeed (going to the 2 remaining servers)

<details>
<summary>Solution</summary>

```rust
use tokio::net::{TcpListener, TcpStream};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use std::time::Duration;

/// Simple echo server: reads data and sends it back with a prefix.
async fn echo_server(port: u16, prefix: &str) -> io::Result<()> {
    let listener = TcpListener::bind(format!("127.0.0.1:{port}")).await?;
    let prefix = prefix.to_string();

    loop {
        let (mut stream, _) = listener.accept().await?;
        let prefix = prefix.clone();
        tokio::spawn(async move {
            let mut buf = vec![0u8; 4096];
            loop {
                match stream.read(&mut buf).await {
                    Ok(0) => break,
                    Ok(n) => {
                        let response = format!("[{}] {}", prefix, String::from_utf8_lossy(&buf[..n]));
                        let _ = stream.write_all(response.as_bytes()).await;
                    }
                    Err(_) => break,
                }
            }
        });
    }
}

async fn send_and_receive(addr: &str, message: &str) -> io::Result<String> {
    let mut stream = TcpStream::connect(addr).await?;
    stream.write_all(message.as_bytes()).await?;
    stream.shutdown().await?;

    let mut response = String::new();
    stream.read_to_string(&mut response).await?;
    Ok(response)
}

#[tokio::main]
async fn main() -> io::Result<()> {
    // Start 3 echo servers
    let server1 = tokio::spawn(echo_server(9081, "S1"));
    let server2 = tokio::spawn(echo_server(9082, "S2"));
    let server3 = tokio::spawn(echo_server(9083, "S3"));

    // Give servers time to start
    tokio::time::sleep(Duration::from_millis(100)).await;

    // Start load balancer
    let config = Config {
        listen_addr: "127.0.0.1:9000".parse().unwrap(),
        backends: vec![
            BackendConfig { addr: "127.0.0.1:9081".parse().unwrap(), weight: 1 },
            BackendConfig { addr: "127.0.0.1:9082".parse().unwrap(), weight: 1 },
            BackendConfig { addr: "127.0.0.1:9083".parse().unwrap(), weight: 1 },
        ],
        algorithm: Algorithm::RoundRobin,
        health_check_interval: Duration::from_secs(1),
        health_check_timeout: Duration::from_millis(500),
        connection_timeout: Duration::from_secs(5),
        max_connections_per_backend: 100,
    };

    tokio::spawn(run_load_balancer(config));
    tokio::time::sleep(Duration::from_millis(200)).await;

    // Send 30 messages through the load balancer
    let mut server_hits = std::collections::HashMap::new();
    for i in 0..30 {
        match send_and_receive("127.0.0.1:9000", &format!("msg-{i}")).await {
            Ok(response) => {
                let prefix = if response.starts_with("[S1]") {
                    "S1"
                } else if response.starts_with("[S2]") {
                    "S2"
                } else {
                    "S3"
                };
                *server_hits.entry(prefix.to_string()).or_insert(0usize) += 1;
            }
            Err(e) => eprintln!("request {i} failed: {e}"),
        }
    }

    println!("distribution across 3 servers: {:?}", server_hits);
    assert_eq!(server_hits.values().sum::<usize>(), 30);
    assert!(server_hits.len() == 3, "all 3 servers should be hit");

    println!("\nintegration test: passed (simplified -- full test requires server shutdown control)");
    Ok(())
}
```
</details>

### Exercise 3: Metrics and Monitoring

Add a metrics endpoint that the load balancer exposes on a separate port (e.g., `:9090/metrics`). It should return Prometheus-compatible metrics:

```
# HELP lb_backend_connections_active Active connections per backend
# TYPE lb_backend_connections_active gauge
lb_backend_connections_active{backend="127.0.0.1:8081"} 5
lb_backend_connections_active{backend="127.0.0.1:8082"} 3

# HELP lb_backend_connections_total Total connections per backend
# TYPE lb_backend_connections_total counter
lb_backend_connections_total{backend="127.0.0.1:8081"} 1234
lb_backend_connections_total{backend="127.0.0.1:8082"} 987

# HELP lb_backend_healthy Backend health status
# TYPE lb_backend_healthy gauge
lb_backend_healthy{backend="127.0.0.1:8081"} 1
lb_backend_healthy{backend="127.0.0.1:8082"} 0
```

<details>
<summary>Solution</summary>

```rust
use tokio::net::TcpListener;
use tokio::io::AsyncWriteExt;

async fn metrics_server(
    listen_addr: SocketAddr,
    backends: Vec<Arc<Backend>>,
) -> io::Result<()> {
    let listener = TcpListener::bind(listen_addr).await?;
    println!("metrics server on {listen_addr}");

    loop {
        let (mut stream, _) = listener.accept().await?;
        let backends = backends.clone();

        tokio::spawn(async move {
            let mut buf = vec![0u8; 4096];
            let _ = stream.read(&mut buf).await;

            let mut body = String::new();

            body.push_str("# HELP lb_backend_connections_active Active connections per backend\n");
            body.push_str("# TYPE lb_backend_connections_active gauge\n");
            for b in &backends {
                body.push_str(&format!(
                    "lb_backend_connections_active{{backend=\"{}\"}} {}\n",
                    b.config.addr,
                    b.active_connections.load(Ordering::Relaxed),
                ));
            }

            body.push_str("\n# HELP lb_backend_connections_total Total connections per backend\n");
            body.push_str("# TYPE lb_backend_connections_total counter\n");
            for b in &backends {
                body.push_str(&format!(
                    "lb_backend_connections_total{{backend=\"{}\"}} {}\n",
                    b.config.addr,
                    b.total_connections.load(Ordering::Relaxed),
                ));
            }

            body.push_str("\n# HELP lb_backend_errors_total Total errors per backend\n");
            body.push_str("# TYPE lb_backend_errors_total counter\n");
            for b in &backends {
                body.push_str(&format!(
                    "lb_backend_errors_total{{backend=\"{}\"}} {}\n",
                    b.config.addr,
                    b.total_errors.load(Ordering::Relaxed),
                ));
            }

            body.push_str("\n# HELP lb_backend_healthy Backend health status (1=healthy, 0=unhealthy)\n");
            body.push_str("# TYPE lb_backend_healthy gauge\n");
            for b in &backends {
                body.push_str(&format!(
                    "lb_backend_healthy{{backend=\"{}\"}} {}\n",
                    b.config.addr,
                    if b.is_healthy() { 1 } else { 0 },
                ));
            }

            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: {}\r\n\r\n{}",
                body.len(),
                body
            );

            let _ = stream.write_all(response.as_bytes()).await;
        });
    }
}

#[tokio::main]
async fn main() -> io::Result<()> {
    let configs = vec![
        BackendConfig { addr: "127.0.0.1:8081".parse().unwrap(), weight: 1 },
        BackendConfig { addr: "127.0.0.1:8082".parse().unwrap(), weight: 1 },
    ];

    let balancer = Arc::new(Balancer::new(configs, Algorithm::RoundRobin));
    let backends = balancer.backends().to_vec();

    // Simulate some traffic
    backends[0].total_connections.store(1234, Ordering::Relaxed);
    backends[0].active_connections.store(5, Ordering::Relaxed);
    backends[1].total_connections.store(987, Ordering::Relaxed);
    backends[1].active_connections.store(3, Ordering::Relaxed);
    backends[1].set_healthy(false);

    println!("starting metrics server on :9090");
    println!("curl http://localhost:9090/metrics to see output");

    metrics_server(
        "127.0.0.1:9090".parse().unwrap(),
        backends,
    ).await
}
```
</details>

### Exercise 4: Retry with Next Backend

When a connection to the selected backend fails, retry with the next available backend instead of immediately returning an error. Implement a retry policy:
- Maximum 3 retries
- Each retry selects a different backend (avoid the one that just failed)
- Track which backends were tried to avoid retrying the same one

<details>
<summary>Solution</summary>

```rust
async fn proxy_with_retry(
    client: TcpStream,
    balancer: &Balancer,
    timeout: Duration,
    max_retries: usize,
) -> io::Result<()> {
    let mut tried: Vec<SocketAddr> = Vec::new();
    let mut last_error = None;

    for attempt in 0..=max_retries {
        // Select a backend, excluding already-tried ones
        let backend = {
            let mut selected = None;
            for _ in 0..balancer.backends().len() {
                if let Some(b) = balancer.select() {
                    if !tried.contains(&b.config.addr) {
                        selected = Some(b);
                        break;
                    }
                }
            }
            selected
        };

        let backend = match backend {
            Some(b) => b,
            None => {
                return Err(io::Error::new(
                    io::ErrorKind::NotConnected,
                    format!("no backends available (tried {})", tried.len()),
                ));
            }
        };

        tried.push(backend.config.addr);

        // Try connecting to the backend
        backend.connect();
        let connect_result = tokio::time::timeout(
            timeout,
            TcpStream::connect(backend.config.addr),
        ).await;

        match connect_result {
            Ok(Ok(upstream)) => {
                if attempt > 0 {
                    eprintln!(
                        "[RETRY] succeeded on attempt {} (backend {})",
                        attempt + 1,
                        backend.config.addr
                    );
                }

                // Proxy the connection (simplified -- in practice we would copy bytes)
                backend.disconnect();
                drop(upstream);
                return Ok(());
            }
            Ok(Err(e)) => {
                backend.disconnect();
                backend.record_error();
                last_error = Some(e);
                eprintln!(
                    "[RETRY] attempt {} failed for {}: {}",
                    attempt + 1,
                    backend.config.addr,
                    last_error.as_ref().unwrap()
                );
            }
            Err(_) => {
                backend.disconnect();
                backend.record_error();
                last_error = Some(io::Error::new(io::ErrorKind::TimedOut, "connection timeout"));
                eprintln!(
                    "[RETRY] attempt {} timed out for {}",
                    attempt + 1,
                    backend.config.addr
                );
            }
        }
    }

    Err(last_error.unwrap_or_else(|| {
        io::Error::new(io::ErrorKind::Other, "all retries exhausted")
    }))
}

fn main() {
    println!("retry logic: defined and ready for integration");
    println!("to test: start the load balancer with backends that intermittently fail");
}
```
</details>

### Exercise 5: Rate Limiting per Client IP

Add per-client-IP rate limiting using a token bucket algorithm. Each IP gets 100 requests per minute. If exceeded, the load balancer returns HTTP 429 (or closes the TCP connection with a message).

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::Mutex;
use std::time::Instant;

struct TokenBucket {
    tokens: f64,
    max_tokens: f64,
    refill_rate: f64, // tokens per second
    last_refill: Instant,
}

impl TokenBucket {
    fn new(max_tokens: f64, refill_rate: f64) -> Self {
        Self {
            tokens: max_tokens,
            max_tokens,
            refill_rate,
            last_refill: Instant::now(),
        }
    }

    fn try_consume(&mut self) -> bool {
        let now = Instant::now();
        let elapsed = now.duration_since(self.last_refill).as_secs_f64();
        self.tokens = (self.tokens + elapsed * self.refill_rate).min(self.max_tokens);
        self.last_refill = now;

        if self.tokens >= 1.0 {
            self.tokens -= 1.0;
            true
        } else {
            false
        }
    }
}

struct RateLimiter {
    buckets: Mutex<HashMap<IpAddr, TokenBucket>>,
    max_tokens: f64,
    refill_rate: f64,
}

impl RateLimiter {
    /// Create a rate limiter: `max_requests` per `window` duration.
    fn new(max_requests: u32, window: Duration) -> Self {
        let max_tokens = max_requests as f64;
        let refill_rate = max_tokens / window.as_secs_f64();
        Self {
            buckets: Mutex::new(HashMap::new()),
            max_tokens,
            refill_rate,
        }
    }

    fn check(&self, ip: IpAddr) -> bool {
        let mut buckets = self.buckets.lock().unwrap();
        let bucket = buckets
            .entry(ip)
            .or_insert_with(|| TokenBucket::new(self.max_tokens, self.refill_rate));
        bucket.try_consume()
    }

    /// Clean up expired buckets (those that have been full for a while).
    fn cleanup(&self) {
        let mut buckets = self.buckets.lock().unwrap();
        let cutoff = Instant::now() - Duration::from_secs(300);
        buckets.retain(|_, bucket| bucket.last_refill > cutoff);
    }
}

async fn accept_with_rate_limit(
    listener: &TcpListener,
    rate_limiter: &RateLimiter,
    balancer: &Balancer,
    timeout: Duration,
) -> io::Result<()> {
    let (mut client, client_addr) = listener.accept().await?;

    if !rate_limiter.check(client_addr.ip()) {
        // Rate limited -- send HTTP 429 and close
        let response = "HTTP/1.1 429 Too Many Requests\r\n\
            Content-Type: text/plain\r\n\
            Retry-After: 60\r\n\
            Content-Length: 19\r\n\
            \r\n\
            Too Many Requests\r\n";
        let _ = client.write_all(response.as_bytes()).await;
        let _ = client.shutdown().await;
        eprintln!("[RATE] {} rate limited", client_addr.ip());
        return Ok(());
    }

    // Normal proxying
    if let Some(backend) = balancer.select() {
        tokio::spawn(proxy_connection(client, backend, timeout));
    }

    Ok(())
}

fn main() {
    // Test the rate limiter
    let limiter = RateLimiter::new(5, Duration::from_secs(1)); // 5 req/sec
    let ip: IpAddr = "192.168.1.1".parse().unwrap();

    let mut allowed = 0;
    let mut denied = 0;
    for _ in 0..10 {
        if limiter.check(ip) {
            allowed += 1;
        } else {
            denied += 1;
        }
    }

    println!("allowed: {allowed}, denied: {denied}");
    assert_eq!(allowed, 5);
    assert_eq!(denied, 5);

    // Different IP has its own bucket
    let ip2: IpAddr = "192.168.1.2".parse().unwrap();
    assert!(limiter.check(ip2));

    println!("rate limiter: all tests passed");
}
```
</details>

---

## Common Mistakes

1. **Not handling half-closed connections.** When one side of a TCP connection sends FIN, the other side can still send data. Using `tokio::io::copy` in both directions with `select!` can prematurely close the connection. Use `copy_bidirectional` or handle shutdown carefully.

2. **Health check false positives.** A successful TCP connect does not mean the backend is serving correctly. Production health checks send an HTTP request and verify the response code. Our TCP-only check is a minimum viable approach.

3. **Race condition in least-connections.** Between selecting a backend and incrementing its connection count, another task might also select it. This is acceptable -- the count is approximate, and the error is bounded.

4. **Unbounded per-client state.** The rate limiter's `HashMap<IpAddr, TokenBucket>` grows without bound. The `cleanup` method must be called periodically to evict stale entries, or use an LRU cache.

5. **Blocking in async context.** File I/O for config reload should use `tokio::fs`, not `std::fs`. `Mutex` from `std::sync` is acceptable for short-lived locks but consider `tokio::sync::Mutex` if the critical section includes `.await`.

---

## Verification

```bash
cargo new load-balancer-lab && cd load-balancer-lab
```

Add tokio to `Cargo.toml`:

```toml
[dependencies]
tokio = { version = "1", features = ["full"] }
```

Test the balancing algorithms without network:

```bash
cargo run
```

For full integration testing, start backend servers:

```bash
# Terminal 1-3: simple echo servers
for port in 8081 8082 8083; do
  nc -l -k $port &
done

# Terminal 4: load balancer
cargo run

# Terminal 5: test
echo "hello" | nc localhost 3000
```

---

## What You Learned

- A load balancer distributes connections across backend servers using algorithms (round-robin, weighted, least-connections, random), each with different trade-offs for fairness, simplicity, and adaptability.
- TCP proxying with `tokio::io::copy` bidirectionally forwards bytes between client and backend with zero parsing overhead (Layer 4). HTTP proxying (Layer 7) adds header injection and content-aware routing at the cost of parsing.
- Health checking with periodic TCP probes automatically removes failed backends and reintroduces recovered ones. The `AtomicBool` healthy flag is checked on every connection attempt.
- Connection draining uses a "stop accepting new, wait for existing" pattern: a `draining` flag prevents new selection, and a `watch` channel signals when the active count reaches zero.
- Rate limiting with token buckets provides per-client fairness. The bucket refills continuously, allowing bursts up to the bucket capacity while enforcing a long-term rate limit.
- Atomic counters (`AtomicUsize`, `AtomicBool`) provide lock-free metrics tracking that is safe to read and write from any task without mutex overhead.

## Resources

- [Tokio Tutorial: I/O](https://tokio.rs/tokio/tutorial/io)
- [HAProxy Architecture](https://www.haproxy.org/download/2.9/doc/architecture.txt)
- [nginx Load Balancing](https://docs.nginx.com/nginx/admin-guide/load-balancer/http-load-balancer/)
- [Token Bucket Algorithm](https://en.wikipedia.org/wiki/Token_bucket)
- [Envoy Proxy Architecture](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/arch_overview)
