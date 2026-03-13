# 22. Networking with Tokio

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Exercise 21 (concurrency patterns, especially actors and fan-out)
- Understanding of TCP fundamentals (connections, streams, framing)
- Familiarity with HTTP request/response model

## Learning Objectives

- Build a raw TCP server with custom protocol framing using tokio-util codecs
- Implement custom `Encoder` + `Decoder` traits for a binary protocol
- Design an HTTP API with axum: routing, extractors, shared state, middleware
- Compose tower `Service` and `Layer` abstractions to build reusable middleware
- Implement graceful shutdown with signal handling and connection draining
- Configure TLS termination with rustls for production-grade servers

## Concepts

### Part 1: Raw TCP with Framing

TCP is a byte stream, not a message stream. If you send two 100-byte messages, the receiver might get one 200-byte read, or five 40-byte reads, or any other split. Framing solves this by defining message boundaries.

```rust
use tokio::net::{TcpListener, TcpStream};
use tokio_util::codec::{Framed, LinesCodec, LengthDelimitedCodec};
use futures::{SinkExt, StreamExt};

// Approach 1: Line-delimited (text protocols like Redis RESP, SMTP)
async fn line_server(addr: &str) -> anyhow::Result<()> {
    let listener = TcpListener::bind(addr).await?;
    println!("listening on {addr}");

    loop {
        let (stream, peer) = listener.accept().await?;
        tokio::spawn(async move {
            // LinesCodec splits on \n, max line length prevents DoS
            let mut framed = Framed::new(stream, LinesCodec::new_with_max_length(8192));

            while let Some(Ok(line)) = framed.next().await {
                println!("[{peer}] received: {line}");
                let response = format!("echo: {line}");
                if framed.send(response).await.is_err() {
                    break;
                }
            }
            println!("[{peer}] disconnected");
        });
    }
}

// Approach 2: Length-delimited (binary protocols)
async fn binary_server(addr: &str) -> anyhow::Result<()> {
    let listener = TcpListener::bind(addr).await?;

    loop {
        let (stream, peer) = listener.accept().await?;
        tokio::spawn(async move {
            let codec = LengthDelimitedCodec::builder()
                .length_field_offset(0)
                .length_field_length(4)    // 4-byte big-endian length prefix
                .length_adjustment(0)
                .max_frame_length(1024 * 1024) // 1MB max frame
                .new_codec();

            let mut framed = Framed::new(stream, codec);

            while let Some(Ok(frame)) = framed.next().await {
                println!("[{peer}] frame: {} bytes", frame.len());
                // Echo back
                if framed.send(frame.freeze()).await.is_err() {
                    break;
                }
            }
        });
    }
}
```

### Custom Codec

When neither lines nor length-delimited framing fits your protocol, implement `Decoder` and `Encoder` directly. Here is a simple command protocol: 1-byte opcode + 2-byte big-endian payload length + payload:

```rust
use bytes::{Buf, BufMut, BytesMut};
use tokio_util::codec::{Decoder, Encoder};

#[derive(Debug, Clone)]
enum Command {
    Ping,
    Echo(Vec<u8>),
    Quit,
}

struct CommandCodec;

impl Decoder for CommandCodec {
    type Item = Command;
    type Error = std::io::Error;

    fn decode(&mut self, src: &mut BytesMut) -> Result<Option<Self::Item>, Self::Error> {
        if src.is_empty() {
            return Ok(None); // Need more data
        }

        let opcode = src[0];
        match opcode {
            0x01 => {
                // Ping: just 1 byte
                src.advance(1);
                Ok(Some(Command::Ping))
            }
            0x02 => {
                // Echo: 1 byte opcode + 2 byte length + payload
                if src.len() < 3 {
                    return Ok(None); // Need more data
                }
                let len = u16::from_be_bytes([src[1], src[2]]) as usize;
                if src.len() < 3 + len {
                    // Reserve capacity hint so tokio allocates enough
                    src.reserve(3 + len - src.len());
                    return Ok(None);
                }
                src.advance(3);
                let payload = src.split_to(len).to_vec();
                Ok(Some(Command::Echo(payload)))
            }
            0x03 => {
                src.advance(1);
                Ok(Some(Command::Quit))
            }
            _ => Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                format!("unknown opcode: {opcode:#x}"),
            )),
        }
    }
}

impl Encoder<Command> for CommandCodec {
    type Error = std::io::Error;

    fn encode(&mut self, item: Command, dst: &mut BytesMut) -> Result<(), Self::Error> {
        match item {
            Command::Ping => dst.put_u8(0x01),
            Command::Echo(payload) => {
                dst.put_u8(0x02);
                dst.put_u16(payload.len() as u16);
                dst.extend_from_slice(&payload);
            }
            Command::Quit => dst.put_u8(0x03),
        }
        Ok(())
    }
}
```

### Part 2: Axum HTTP Server

Axum is built on top of tokio, hyper, and tower. It uses Rust's type system to extract request data at compile time. There is no runtime reflection.

```rust
use axum::{
    Router,
    routing::{get, post},
    extract::{Path, Query, State, Json},
    http::StatusCode,
    response::IntoResponse,
    middleware,
};
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tokio::sync::RwLock;
use std::collections::HashMap;

// Shared application state
#[derive(Clone)]
struct AppState {
    db: Arc<RwLock<HashMap<String, Item>>>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
struct Item {
    name: String,
    price: f64,
}

#[derive(Deserialize)]
struct ListParams {
    limit: Option<usize>,
    offset: Option<usize>,
}

// Handlers: async functions that take extractors and return impl IntoResponse

async fn health() -> &'static str {
    "ok"
}

async fn list_items(
    State(state): State<AppState>,
    Query(params): Query<ListParams>,
) -> Json<Vec<Item>> {
    let db = state.db.read().await;
    let items: Vec<Item> = db.values()
        .skip(params.offset.unwrap_or(0))
        .take(params.limit.unwrap_or(50))
        .cloned()
        .collect();
    Json(items)
}

async fn get_item(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> Result<Json<Item>, StatusCode> {
    let db = state.db.read().await;
    db.get(&id)
        .cloned()
        .map(Json)
        .ok_or(StatusCode::NOT_FOUND)
}

async fn create_item(
    State(state): State<AppState>,
    Json(item): Json<Item>,
) -> (StatusCode, Json<Item>) {
    let mut db = state.db.write().await;
    db.insert(item.name.clone(), item.clone());
    (StatusCode::CREATED, Json(item))
}

async fn delete_item(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> StatusCode {
    let mut db = state.db.write().await;
    if db.remove(&id).is_some() {
        StatusCode::NO_CONTENT
    } else {
        StatusCode::NOT_FOUND
    }
}

fn app(state: AppState) -> Router {
    Router::new()
        .route("/health", get(health))
        .route("/items", get(list_items).post(create_item))
        .route("/items/{id}", get(get_item).delete(delete_item))
        .with_state(state)
}
```

Extractor order matters. Axum processes extractors left to right. `Json` consumes the body, so it must be the last extractor. `State`, `Path`, and `Query` do not consume the body and can appear in any order before `Json`.

### Part 3: Tower Service and Layer

Every axum handler is ultimately a `tower::Service`. Understanding `Service` and `Layer` lets you write reusable middleware:

```rust
use axum::{
    extract::Request,
    middleware::{self, Next},
    response::Response,
};
use std::time::Instant;

// Function middleware (simplest approach in axum)
async fn timing_middleware(request: Request, next: Next) -> Response {
    let start = Instant::now();
    let method = request.method().clone();
    let uri = request.uri().clone();

    let response = next.run(request).await;

    let elapsed = start.elapsed();
    println!("{method} {uri} -> {} ({elapsed:?})", response.status());
    response
}

// Apply it:
// Router::new()
//     .route("/items", get(list_items))
//     .layer(middleware::from_fn(timing_middleware))

// Tower Layer (reusable across any tower-compatible framework)
use tower::Layer;
use tower::Service;
use std::task::{Context, Poll};
use std::future::Future;
use std::pin::Pin;

#[derive(Clone)]
struct RequestIdLayer;

impl<S> Layer<S> for RequestIdLayer {
    type Service = RequestIdService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        RequestIdService { inner }
    }
}

#[derive(Clone)]
struct RequestIdService<S> {
    inner: S,
}

impl<S> Service<Request> for RequestIdService<S>
where
    S: Service<Request, Response = Response> + Clone + Send + 'static,
    S::Future: Send + 'static,
{
    type Response = Response;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<Response, S::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, mut request: Request) -> Self::Future {
        let id = uuid::Uuid::new_v4().to_string();
        request.headers_mut().insert(
            "x-request-id",
            id.parse().unwrap(),
        );

        let mut inner = self.inner.clone();
        Box::pin(async move {
            let mut response = inner.call(request).await?;
            response.headers_mut().insert(
                "x-request-id",
                id.parse().unwrap(),
            );
            Ok(response)
        })
    }
}
```

The trade-off: `middleware::from_fn` is simpler but axum-specific. A tower `Layer`/`Service` pair works with any tower-based framework (tonic, hyper, etc.) but requires more boilerplate.

### Part 4: Graceful Shutdown

A production server must drain in-flight requests before exiting. Axum's `serve` method supports this directly:

```rust
use tokio::signal;
use tokio::net::TcpListener;

async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install SIGTERM handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => println!("received Ctrl+C"),
        _ = terminate => println!("received SIGTERM"),
    }
}

async fn run_server() {
    let state = AppState {
        db: Arc::new(RwLock::new(HashMap::new())),
    };

    let listener = TcpListener::bind("0.0.0.0:3000").await.unwrap();
    println!("listening on {}", listener.local_addr().unwrap());

    axum::serve(listener, app(state))
        .with_graceful_shutdown(shutdown_signal())
        .await
        .unwrap();

    println!("server shutdown complete");
}
```

### Part 5: TLS with rustls

For production, terminate TLS at the application level with rustls (pure Rust, no OpenSSL dependency):

```rust
use axum_server::tls_rustls::RustlsConfig;

async fn run_tls_server() {
    let tls_config = RustlsConfig::from_pem_file(
        "certs/cert.pem",
        "certs/key.pem",
    ).await.unwrap();

    let app = Router::new().route("/", get(|| async { "hello TLS" }));

    axum_server::bind_rustls("0.0.0.0:3443".parse().unwrap(), tls_config)
        .serve(app.into_make_service())
        .await
        .unwrap();
}
```

Generate self-signed certs for development:

```bash
openssl req -x509 -newkey rsa:4096 -keyout certs/key.pem -out certs/cert.pem \
  -days 365 -nodes -subj '/CN=localhost'
```

In production, use certificates from Let's Encrypt or your PKI. The `RustlsConfig::from_pem_file` watches for file changes, so certificate rotation requires no restart.

## Exercises

### Exercise 1: Chat Server with Custom Protocol

Build a TCP chat server using `CommandCodec` from above. The protocol:
- `0x01` = Join (payload = username)
- `0x02` = Message (payload = UTF-8 text)
- `0x03` = Quit (no payload)
- Server broadcasts messages to all connected clients with the sender's username prepended

**Cargo.toml:**
```toml
[package]
name = "networking-tokio"
edition = "2024"

[dependencies]
tokio = { version = "1", features = ["full"] }
tokio-util = { version = "0.7", features = ["codec"] }
bytes = "1"
futures = "0.3"
axum = "0.8"
axum-server = { version = "0.7", features = ["tls-rustls"] }
tower = { version = "0.5", features = ["full"] }
tower-http = { version = "0.6", features = ["trace", "cors", "compression-gzip"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
uuid = { version = "1", features = ["v4"] }
anyhow = "1"
tracing = "0.1"
tracing-subscriber = "0.3"
```

**Constraints:**
- Use `tokio::sync::broadcast` for message distribution
- Each client connection is a separate task
- Handle disconnections gracefully (remove from user list, notify others)
- Maximum message size: 4096 bytes

<details>
<summary>Solution</summary>

```rust
use bytes::{Buf, BufMut, BytesMut};
use futures::{SinkExt, StreamExt};
use std::collections::HashMap;
use std::sync::Arc;
use tokio::net::TcpListener;
use tokio::sync::{broadcast, RwLock};
use tokio_util::codec::{Decoder, Encoder, Framed};

#[derive(Debug, Clone)]
enum ChatMsg {
    Join(String),
    Message(String),
    Quit,
}

#[derive(Debug, Clone)]
struct Broadcast {
    username: String,
    text: String,
}

struct ChatCodec;

impl Decoder for ChatCodec {
    type Item = ChatMsg;
    type Error = std::io::Error;

    fn decode(&mut self, src: &mut BytesMut) -> Result<Option<Self::Item>, Self::Error> {
        if src.is_empty() {
            return Ok(None);
        }
        let opcode = src[0];
        match opcode {
            0x01 | 0x02 => {
                if src.len() < 3 {
                    return Ok(None);
                }
                let len = u16::from_be_bytes([src[1], src[2]]) as usize;
                if len > 4096 {
                    return Err(std::io::Error::new(
                        std::io::ErrorKind::InvalidData,
                        "message too large",
                    ));
                }
                if src.len() < 3 + len {
                    src.reserve(3 + len - src.len());
                    return Ok(None);
                }
                src.advance(3);
                let payload = src.split_to(len).to_vec();
                let text = String::from_utf8(payload).map_err(|e| {
                    std::io::Error::new(std::io::ErrorKind::InvalidData, e)
                })?;
                Ok(Some(if opcode == 0x01 {
                    ChatMsg::Join(text)
                } else {
                    ChatMsg::Message(text)
                }))
            }
            0x03 => {
                src.advance(1);
                Ok(Some(ChatMsg::Quit))
            }
            _ => Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                format!("unknown opcode: {opcode:#x}"),
            )),
        }
    }
}

impl Encoder<String> for ChatCodec {
    type Error = std::io::Error;

    fn encode(&mut self, item: String, dst: &mut BytesMut) -> Result<(), Self::Error> {
        let bytes = item.as_bytes();
        dst.put_u8(0x02);
        dst.put_u16(bytes.len() as u16);
        dst.extend_from_slice(bytes);
        Ok(())
    }
}

type Users = Arc<RwLock<HashMap<std::net::SocketAddr, String>>>;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let listener = TcpListener::bind("127.0.0.1:9000").await?;
    let (broadcast_tx, _) = broadcast::channel::<Broadcast>(256);
    let users: Users = Arc::new(RwLock::new(HashMap::new()));

    println!("chat server listening on 127.0.0.1:9000");

    loop {
        let (stream, addr) = listener.accept().await?;
        let tx = broadcast_tx.clone();
        let mut rx = broadcast_tx.subscribe();
        let users = users.clone();

        tokio::spawn(async move {
            let mut framed = Framed::new(stream, ChatCodec);
            let mut username = format!("anon-{}", &addr.port());

            // Wait for Join message
            if let Some(Ok(ChatMsg::Join(name))) = framed.next().await {
                username = name;
            }
            users.write().await.insert(addr, username.clone());
            let _ = tx.send(Broadcast {
                username: "server".into(),
                text: format!("{username} joined"),
            });

            loop {
                tokio::select! {
                    msg = framed.next() => {
                        match msg {
                            Some(Ok(ChatMsg::Message(text))) => {
                                let _ = tx.send(Broadcast {
                                    username: username.clone(),
                                    text,
                                });
                            }
                            Some(Ok(ChatMsg::Quit)) | None => break,
                            Some(Ok(ChatMsg::Join(_))) => {} // ignore duplicate joins
                            Some(Err(e)) => {
                                eprintln!("[{addr}] codec error: {e}");
                                break;
                            }
                        }
                    }
                    Ok(broadcast) = rx.recv() => {
                        if broadcast.username != username {
                            let line = format!("[{}] {}", broadcast.username, broadcast.text);
                            if framed.send(line).await.is_err() {
                                break;
                            }
                        }
                    }
                }
            }

            users.write().await.remove(&addr);
            let _ = tx.send(Broadcast {
                username: "server".into(),
                text: format!("{username} left"),
            });
        });
    }
}
```
</details>

### Exercise 2: REST API with Full Middleware Stack

Build a complete CRUD API for a "tasks" resource using axum with:
- Routes: GET /tasks, GET /tasks/{id}, POST /tasks, PUT /tasks/{id}, DELETE /tasks/{id}
- Request timing middleware (log method, path, status, duration)
- Request ID middleware (generate UUID, add to response header)
- CORS middleware (allow all origins for development)
- JSON error responses (not plain text) for 404 and 422

**Constraints:**
- In-memory store with `Arc<RwLock<HashMap>>`
- Task struct: `{ id: Uuid, title: String, done: bool, created_at: DateTime }`
- Return proper HTTP status codes (201 for create, 204 for delete, 404 for missing)
- All middleware must be tower layers, composable

<details>
<summary>Solution</summary>

```rust
use axum::{
    Router,
    routing::{get, put, delete},
    extract::{Path, State, Json, Request},
    http::StatusCode,
    response::{IntoResponse, Response},
    middleware::{self, Next},
};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;
use tokio::sync::RwLock;
use tower_http::cors::CorsLayer;
use uuid::Uuid;

#[derive(Debug, Serialize, Deserialize, Clone)]
struct Task {
    id: Uuid,
    title: String,
    done: bool,
    created_at: String,
}

#[derive(Deserialize)]
struct CreateTask {
    title: String,
}

#[derive(Deserialize)]
struct UpdateTask {
    title: Option<String>,
    done: Option<bool>,
}

#[derive(Serialize)]
struct ErrorResponse {
    error: String,
    status: u16,
}

type Db = Arc<RwLock<HashMap<Uuid, Task>>>;

async fn timing_middleware(request: Request, next: Next) -> Response {
    let start = Instant::now();
    let method = request.method().clone();
    let uri = request.uri().path().to_owned();
    let response = next.run(request).await;
    println!("{method} {uri} -> {} ({:?})", response.status(), start.elapsed());
    response
}

async fn request_id_middleware(mut request: Request, next: Next) -> Response {
    let id = Uuid::new_v4().to_string();
    request.headers_mut().insert("x-request-id", id.parse().unwrap());
    let mut response = next.run(request).await;
    response.headers_mut().insert("x-request-id", id.parse().unwrap());
    response
}

async fn list_tasks(State(db): State<Db>) -> Json<Vec<Task>> {
    let db = db.read().await;
    Json(db.values().cloned().collect())
}

async fn get_task(
    State(db): State<Db>,
    Path(id): Path<Uuid>,
) -> Result<Json<Task>, (StatusCode, Json<ErrorResponse>)> {
    let db = db.read().await;
    db.get(&id)
        .cloned()
        .map(Json)
        .ok_or_else(|| (
            StatusCode::NOT_FOUND,
            Json(ErrorResponse { error: format!("task {id} not found"), status: 404 }),
        ))
}

async fn create_task(
    State(db): State<Db>,
    Json(input): Json<CreateTask>,
) -> (StatusCode, Json<Task>) {
    let task = Task {
        id: Uuid::new_v4(),
        title: input.title,
        done: false,
        created_at: chrono::Utc::now().to_rfc3339(),
    };
    db.write().await.insert(task.id, task.clone());
    (StatusCode::CREATED, Json(task))
}

async fn update_task(
    State(db): State<Db>,
    Path(id): Path<Uuid>,
    Json(input): Json<UpdateTask>,
) -> Result<Json<Task>, (StatusCode, Json<ErrorResponse>)> {
    let mut db = db.write().await;
    let task = db.get_mut(&id).ok_or_else(|| (
        StatusCode::NOT_FOUND,
        Json(ErrorResponse { error: format!("task {id} not found"), status: 404 }),
    ))?;
    if let Some(title) = input.title { task.title = title; }
    if let Some(done) = input.done { task.done = done; }
    Ok(Json(task.clone()))
}

async fn delete_task(
    State(db): State<Db>,
    Path(id): Path<Uuid>,
) -> Result<StatusCode, (StatusCode, Json<ErrorResponse>)> {
    let mut db = db.write().await;
    if db.remove(&id).is_some() {
        Ok(StatusCode::NO_CONTENT)
    } else {
        Err((
            StatusCode::NOT_FOUND,
            Json(ErrorResponse { error: format!("task {id} not found"), status: 404 }),
        ))
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt::init();

    let db: Db = Arc::new(RwLock::new(HashMap::new()));

    let app = Router::new()
        .route("/tasks", get(list_tasks).post(create_task))
        .route("/tasks/{id}", get(get_task).put(update_task).delete(delete_task))
        .layer(CorsLayer::permissive())
        .layer(middleware::from_fn(request_id_middleware))
        .layer(middleware::from_fn(timing_middleware))
        .with_state(db);

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    println!("listening on {}", listener.local_addr().unwrap());

    axum::serve(listener, app)
        .with_graceful_shutdown(async {
            tokio::signal::ctrl_c().await.ok();
            println!("shutting down...");
        })
        .await
        .unwrap();
}
```

Note: add `chrono = { version = "0.4", features = ["serde"] }` to Cargo.toml for the timestamp.
</details>

### Exercise 3: TCP Proxy with Metrics

Build a TCP proxy that accepts connections, connects to an upstream server, and bidirectionally copies data. Track per-connection metrics: bytes sent, bytes received, connection duration.

**Constraints:**
- Use `tokio::io::copy_bidirectional` for the data path
- Accept upstream address as a command-line argument
- Log connection open/close with metrics
- Handle upstream connection failures gracefully (return error to client, do not crash)
- Support at most 100 concurrent connections (reject with a "too busy" response if exceeded)

<details>
<summary>Solution</summary>

```rust
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::Instant;
use tokio::io::{self, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};

static ACTIVE_CONNECTIONS: AtomicUsize = AtomicUsize::new(0);
const MAX_CONNECTIONS: usize = 100;

async fn handle_connection(
    mut client: TcpStream,
    upstream_addr: String,
    peer: std::net::SocketAddr,
) {
    let start = Instant::now();
    ACTIVE_CONNECTIONS.fetch_add(1, Ordering::Relaxed);
    let active = ACTIVE_CONNECTIONS.load(Ordering::Relaxed);
    println!("[{peer}] connected (active: {active})");

    let mut upstream = match TcpStream::connect(&upstream_addr).await {
        Ok(s) => s,
        Err(e) => {
            eprintln!("[{peer}] upstream connect failed: {e}");
            let _ = client.write_all(b"upstream unavailable\n").await;
            ACTIVE_CONNECTIONS.fetch_sub(1, Ordering::Relaxed);
            return;
        }
    };

    match io::copy_bidirectional(&mut client, &mut upstream).await {
        Ok((client_to_upstream, upstream_to_client)) => {
            let elapsed = start.elapsed();
            println!(
                "[{peer}] closed | sent: {client_to_upstream}B | recv: {upstream_to_client}B | duration: {elapsed:?}"
            );
        }
        Err(e) => {
            println!("[{peer}] error: {e} | duration: {:?}", start.elapsed());
        }
    }

    ACTIVE_CONNECTIONS.fetch_sub(1, Ordering::Relaxed);
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let listen_addr = args.get(1).map(|s| s.as_str()).unwrap_or("127.0.0.1:8080");
    let upstream_addr = args.get(2).map(|s| s.to_owned()).unwrap_or_else(|| "127.0.0.1:9000".to_owned());

    let listener = TcpListener::bind(listen_addr).await?;
    println!("proxy listening on {listen_addr} -> {upstream_addr}");

    loop {
        let (client, peer) = listener.accept().await?;

        if ACTIVE_CONNECTIONS.load(Ordering::Relaxed) >= MAX_CONNECTIONS {
            eprintln!("[{peer}] rejected: too many connections");
            let mut client = client;
            let _ = client.write_all(b"too busy\n").await;
            continue;
        }

        let upstream = upstream_addr.clone();
        tokio::spawn(handle_connection(client, upstream, peer));
    }
}
```
</details>

### Exercise 4: Graceful Shutdown Orchestra

Build a server that runs three subsystems concurrently: (1) an HTTP API on port 3000, (2) a TCP echo server on port 3001, (3) a background metrics reporter printing stats every 5 seconds. When SIGTERM or Ctrl+C is received, all three must shut down gracefully: the HTTP server drains requests, the TCP server stops accepting but finishes active connections, and the metrics reporter prints a final summary.

**Constraints:**
- Use `tokio::select!` and `CancellationToken` (from `tokio-util`)
- All three subsystems must respond to the same cancellation signal
- No `unwrap()` on task joins -- handle panics
- Print shutdown order and timing

<details>
<summary>Solution</summary>

```rust
use axum::{Router, routing::get};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;
use tokio::net::TcpListener;
use tokio_util::sync::CancellationToken;

struct Metrics {
    http_requests: AtomicU64,
    tcp_connections: AtomicU64,
    tcp_bytes: AtomicU64,
}

async fn http_subsystem(cancel: CancellationToken, metrics: Arc<Metrics>) {
    let m = metrics.clone();
    let app = Router::new().route("/", get(move || {
        m.http_requests.fetch_add(1, Ordering::Relaxed);
        async { "ok" }
    }));

    let listener = TcpListener::bind("0.0.0.0:3000").await.unwrap();
    println!("[http] listening on :3000");

    axum::serve(listener, app)
        .with_graceful_shutdown(cancel.cancelled_owned())
        .await
        .unwrap();

    println!("[http] shut down");
}

async fn tcp_subsystem(cancel: CancellationToken, metrics: Arc<Metrics>) {
    let listener = TcpListener::bind("0.0.0.0:3001").await.unwrap();
    println!("[tcp] listening on :3001");

    loop {
        tokio::select! {
            _ = cancel.cancelled() => break,
            result = listener.accept() => {
                let (mut stream, _) = result.unwrap();
                metrics.tcp_connections.fetch_add(1, Ordering::Relaxed);
                let cancel = cancel.clone();
                let metrics = metrics.clone();
                tokio::spawn(async move {
                    let mut buf = vec![0u8; 4096];
                    loop {
                        tokio::select! {
                            _ = cancel.cancelled() => break,
                            result = tokio::io::AsyncReadExt::read(&mut stream, &mut buf) => {
                                match result {
                                    Ok(0) | Err(_) => break,
                                    Ok(n) => {
                                        metrics.tcp_bytes.fetch_add(n as u64, Ordering::Relaxed);
                                        if tokio::io::AsyncWriteExt::write_all(&mut stream, &buf[..n]).await.is_err() {
                                            break;
                                        }
                                    }
                                }
                            }
                        }
                    }
                });
            }
        }
    }

    println!("[tcp] shut down");
}

async fn metrics_subsystem(cancel: CancellationToken, metrics: Arc<Metrics>) {
    let mut interval = tokio::time::interval(std::time::Duration::from_secs(5));
    loop {
        tokio::select! {
            _ = cancel.cancelled() => break,
            _ = interval.tick() => {
                println!(
                    "[metrics] http={} tcp_conn={} tcp_bytes={}",
                    metrics.http_requests.load(Ordering::Relaxed),
                    metrics.tcp_connections.load(Ordering::Relaxed),
                    metrics.tcp_bytes.load(Ordering::Relaxed),
                );
            }
        }
    }
    println!(
        "[metrics] final: http={} tcp_conn={} tcp_bytes={}",
        metrics.http_requests.load(Ordering::Relaxed),
        metrics.tcp_connections.load(Ordering::Relaxed),
        metrics.tcp_bytes.load(Ordering::Relaxed),
    );
}

#[tokio::main]
async fn main() {
    let cancel = CancellationToken::new();
    let metrics = Arc::new(Metrics {
        http_requests: AtomicU64::new(0),
        tcp_connections: AtomicU64::new(0),
        tcp_bytes: AtomicU64::new(0),
    });

    let start = Instant::now();

    // Signal handler
    let cancel_signal = cancel.clone();
    tokio::spawn(async move {
        tokio::signal::ctrl_c().await.ok();
        println!("\n[main] received shutdown signal");
        cancel_signal.cancel();
    });

    let mut set = tokio::task::JoinSet::new();
    set.spawn(http_subsystem(cancel.clone(), metrics.clone()));
    set.spawn(tcp_subsystem(cancel.clone(), metrics.clone()));
    set.spawn(metrics_subsystem(cancel.clone(), metrics.clone()));

    while let Some(result) = set.join_next().await {
        match result {
            Ok(()) => {}
            Err(e) => eprintln!("[main] subsystem panicked: {e}"),
        }
    }

    println!("[main] all subsystems shut down in {:?}", start.elapsed());
}
```

Add `tokio-util = { version = "0.7", features = ["codec", "rt"] }` to your dependencies for `CancellationToken`.
</details>

## Common Mistakes

1. **Not setting max frame length.** `LinesCodec::new()` has no limit by default. A malicious client can send gigabytes without a newline, exhausting server memory. Always use `LinesCodec::new_with_max_length()` and `LengthDelimitedCodec::builder().max_frame_length()`.

2. **Consuming the body twice.** Placing `Json<T>` before another body-consuming extractor causes a runtime error. Body-consuming extractors must be last.

3. **Middleware layer ordering.** Layers are applied bottom-up in axum. The last `.layer()` call wraps the outermost layer. If you want timing to wrap request-id, timing must be the last `.layer()` call.

4. **Forgetting `into_make_service()`.** `axum::serve` with `TcpListener` works directly with `Router`. But `axum_server` (for TLS) requires `.into_make_service()`. Mixing these up gives confusing type errors.

5. **Holding locks across await points.** `db.write().await` returns a guard. If you `.await` while holding it, you block all other readers/writers for the entire await duration. Extract data, drop the guard, then await.

## Verification

```bash
cargo build
cargo test
cargo clippy -- -W clippy::pedantic
# Manual testing:
# Terminal 1: cargo run
# Terminal 2: curl http://localhost:3000/health
# Terminal 3: echo "hello" | nc localhost 9000
```

## Summary

Networking in Rust with tokio follows a layered architecture: raw TCP at the bottom, framing codecs above it, HTTP servers above that, and tower middleware composing across all layers. The type system ensures extractors are correct at compile time, codecs handle framing safely, and graceful shutdown is explicit rather than implicit. TLS with rustls eliminates the C dependency chain of OpenSSL while maintaining performance parity.

## What's Next

Exercise 23 applies these networking patterns to database access, building repository patterns with sqlx, diesel, and sea-orm that sit behind axum handlers.

## Resources

- [tokio-util codec module](https://docs.rs/tokio-util/latest/tokio_util/codec/index.html)
- [Axum documentation](https://docs.rs/axum/latest/axum/)
- [Tower Service trait](https://docs.rs/tower/latest/tower/trait.Service.html)
- [Tower building middleware from scratch](https://github.com/tower-rs/tower/blob/master/guides/building-a-middleware-from-scratch.md)
- [rustls ServerConfig](https://docs.rs/rustls/latest/rustls/server/struct.ServerConfig.html)
- [axum-server TLS](https://docs.rs/axum-server/latest/axum_server/tls_rustls/)
