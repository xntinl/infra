# 41. Axum Web Framework

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Completed: exercise 13 (serde and serialization)
- Completed: exercise 40 (tower middleware -- understanding `Service` and `Layer` traits)
- Familiarity with HTTP methods, status codes, and REST conventions

## Learning Objectives

- Build a complete REST API with axum's `Router`, handlers, and extractors
- Use `Path`, `Query`, `Json`, `State`, and `Extension` extractors to parse request data
- Share application state safely with `Arc` and implement the `FromRef` pattern
- Handle errors with custom error types and `IntoResponse`
- Build nested routers for modular API organization
- Add WebSocket support for real-time communication
- Test handlers using `axum::body::Body` and `tower::ServiceExt::oneshot`

## Concepts

### Axum's Design Philosophy

Axum is a web framework built on three pillars:

1. **Tower compatibility** -- every handler is a tower `Service`, every middleware is a tower `Layer`
2. **Type-safe extraction** -- request parsing is done via the type system, not string keys
3. **No macros for routing** -- routes are composed with functions, not attribute macros

This means axum code composes naturally: routers are values, handlers are functions, and middleware is stackable. There is no hidden state or magical registration.

### Router and Handlers

A handler is any async function whose arguments are extractors and whose return type implements `IntoResponse`:

```rust
use axum::{
    Router,
    routing::{get, post, put, delete},
    response::IntoResponse,
    http::StatusCode,
};

// Simplest possible handler
async fn health() -> &'static str {
    "ok"
}

// Handler returning a status code
async fn not_found() -> StatusCode {
    StatusCode::NOT_FOUND
}

// Handler returning a tuple (status, body)
async fn created() -> (StatusCode, &'static str) {
    (StatusCode::CREATED, "resource created")
}

// Build the router
fn app() -> Router {
    Router::new()
        .route("/health", get(health))
        .route("/missing", get(not_found))
        .route("/items", post(created))
}
```

Axum handlers can return anything that implements `IntoResponse`. Built-in implementations exist for `String`, `&str`, `StatusCode`, `Json<T>`, `(StatusCode, impl IntoResponse)`, `Html<String>`, `Result<T, E>` where both `T` and `E` are `IntoResponse`, and more.

### Extractors

Extractors are the core of axum's type safety. Each extractor pulls data from a specific part of the request:

```rust
use axum::{
    extract::{Path, Query, Json, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::{Deserialize, Serialize};

// --- Path extractor: /users/:id ---

async fn get_user(Path(id): Path<u64>) -> String {
    format!("User {}", id)
}

// Multiple path parameters: /users/:user_id/posts/:post_id
async fn get_user_post(
    Path((user_id, post_id)): Path<(u64, u64)>,
) -> String {
    format!("User {} Post {}", user_id, post_id)
}

// --- Query extractor: /search?q=rust&page=2 ---

#[derive(Deserialize)]
struct SearchParams {
    q: String,
    page: Option<u32>,
    per_page: Option<u32>,
}

async fn search(Query(params): Query<SearchParams>) -> String {
    format!(
        "Searching for '{}' (page {}, per_page {})",
        params.q,
        params.page.unwrap_or(1),
        params.per_page.unwrap_or(20),
    )
}

// --- Json extractor: parse request body ---

#[derive(Deserialize)]
struct CreateUser {
    name: String,
    email: String,
}

#[derive(Serialize)]
struct User {
    id: u64,
    name: String,
    email: String,
}

async fn create_user(Json(payload): Json<CreateUser>) -> (StatusCode, Json<User>) {
    let user = User {
        id: 1,
        name: payload.name,
        email: payload.email,
    };
    (StatusCode::CREATED, Json(user))
}

// --- Headers extractor ---

use axum::http::HeaderMap;

async fn show_headers(headers: HeaderMap) -> String {
    let user_agent = headers
        .get("user-agent")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("unknown");
    format!("User-Agent: {}", user_agent)
}
```

**Extractor ordering matters.** `Json` and other body-consuming extractors must be the *last* argument because they consume the request body. You can only consume the body once.

```rust
// CORRECT: Path first, Json last
async fn update_user(
    Path(id): Path<u64>,
    State(db): State<AppState>,
    Json(payload): Json<UpdateUser>,
) -> impl IntoResponse { /* ... */ }

// WRONG: Json before State -- will not compile
// async fn bad(Json(p): Json<UpdateUser>, State(s): State<AppState>) { }
// Actually this does compile, but body extractors should be last by convention.
```

### Shared State with Arc

axum provides a `State` extractor for sharing application state across handlers. The state must be `Clone`, so you typically wrap it in `Arc`:

```rust
use axum::{
    Router,
    routing::{get, post, put, delete},
    extract::{Path, Json, State},
    http::StatusCode,
    response::IntoResponse,
};
use std::sync::Arc;
use tokio::sync::RwLock;
use std::collections::HashMap;
use serde::{Deserialize, Serialize};

// --- Application state ---

#[derive(Clone)]
struct AppState {
    db: Arc<RwLock<HashMap<u64, Item>>>,
    next_id: Arc<std::sync::atomic::AtomicU64>,
}

impl AppState {
    fn new() -> Self {
        Self {
            db: Arc::new(RwLock::new(HashMap::new())),
            next_id: Arc::new(std::sync::atomic::AtomicU64::new(1)),
        }
    }

    fn next_id(&self) -> u64 {
        self.next_id
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct Item {
    id: u64,
    name: String,
    description: String,
    completed: bool,
}

#[derive(Deserialize)]
struct CreateItem {
    name: String,
    description: String,
}

#[derive(Deserialize)]
struct UpdateItem {
    name: Option<String>,
    description: Option<String>,
    completed: Option<bool>,
}

// --- Handlers using State ---

async fn list_items(State(state): State<AppState>) -> Json<Vec<Item>> {
    let db = state.db.read().await;
    let items: Vec<Item> = db.values().cloned().collect();
    Json(items)
}

async fn create_item(
    State(state): State<AppState>,
    Json(payload): Json<CreateItem>,
) -> (StatusCode, Json<Item>) {
    let item = Item {
        id: state.next_id(),
        name: payload.name,
        description: payload.description,
        completed: false,
    };

    state.db.write().await.insert(item.id, item.clone());
    (StatusCode::CREATED, Json(item))
}

async fn get_item(
    State(state): State<AppState>,
    Path(id): Path<u64>,
) -> Result<Json<Item>, StatusCode> {
    let db = state.db.read().await;
    db.get(&id)
        .cloned()
        .map(Json)
        .ok_or(StatusCode::NOT_FOUND)
}

async fn update_item(
    State(state): State<AppState>,
    Path(id): Path<u64>,
    Json(payload): Json<UpdateItem>,
) -> Result<Json<Item>, StatusCode> {
    let mut db = state.db.write().await;
    let item = db.get_mut(&id).ok_or(StatusCode::NOT_FOUND)?;

    if let Some(name) = payload.name {
        item.name = name;
    }
    if let Some(description) = payload.description {
        item.description = description;
    }
    if let Some(completed) = payload.completed {
        item.completed = completed;
    }

    Ok(Json(item.clone()))
}

async fn delete_item(
    State(state): State<AppState>,
    Path(id): Path<u64>,
) -> StatusCode {
    let mut db = state.db.write().await;
    if db.remove(&id).is_some() {
        StatusCode::NO_CONTENT
    } else {
        StatusCode::NOT_FOUND
    }
}

fn app() -> Router {
    let state = AppState::new();

    Router::new()
        .route("/items", get(list_items).post(create_item))
        .route("/items/{id}", get(get_item).put(update_item).delete(delete_item))
        .with_state(state)
}
```

### Custom Error Handling

In production, you need structured error responses. Define a custom error type that implements `IntoResponse`:

```rust
use axum::{
    http::StatusCode,
    response::{IntoResponse, Response},
    Json,
};
use serde::Serialize;

// --- Error type ---

#[derive(Debug)]
enum AppError {
    NotFound(String),
    BadRequest(String),
    Conflict(String),
    Internal(String),
}

#[derive(Serialize)]
struct ErrorResponse {
    error: String,
    message: String,
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let (status, error, message) = match self {
            AppError::NotFound(msg) => (StatusCode::NOT_FOUND, "not_found", msg),
            AppError::BadRequest(msg) => (StatusCode::BAD_REQUEST, "bad_request", msg),
            AppError::Conflict(msg) => (StatusCode::CONFLICT, "conflict", msg),
            AppError::Internal(msg) => {
                // Log internal errors but do not expose details to clients
                tracing::error!("Internal error: {}", msg);
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "internal_error",
                    "an internal error occurred".to_string(),
                )
            }
        };

        (
            status,
            Json(ErrorResponse {
                error: error.to_string(),
                message,
            }),
        )
            .into_response()
    }
}

// Convenience conversion from common error types
impl From<tokio::sync::TryLockError> for AppError {
    fn from(_: tokio::sync::TryLockError) -> Self {
        AppError::Internal("lock contention".to_string())
    }
}

// --- Handlers using AppError ---

async fn get_item_v2(
    State(state): State<AppState>,
    Path(id): Path<u64>,
) -> Result<Json<Item>, AppError> {
    let db = state.db.read().await;
    db.get(&id)
        .cloned()
        .map(Json)
        .ok_or_else(|| AppError::NotFound(format!("item {} not found", id)))
}

async fn create_item_v2(
    State(state): State<AppState>,
    Json(payload): Json<CreateItem>,
) -> Result<(StatusCode, Json<Item>), AppError> {
    if payload.name.trim().is_empty() {
        return Err(AppError::BadRequest("name must not be empty".to_string()));
    }

    let item = Item {
        id: state.next_id(),
        name: payload.name,
        description: payload.description,
        completed: false,
    };

    state.db.write().await.insert(item.id, item.clone());
    Ok((StatusCode::CREATED, Json(item)))
}
```

### Custom Extractors

You can define your own extractors by implementing `FromRequestParts` (for data not in the body) or `FromRequest` (for body data):

```rust
use axum::{
    extract::FromRequestParts,
    http::{request::Parts, StatusCode},
    response::{IntoResponse, Response},
};

// Extract an API key from headers
struct ApiKey(String);

impl<S: Send + Sync> FromRequestParts<S> for ApiKey {
    type Rejection = AppError;

    async fn from_request_parts(
        parts: &mut Parts,
        _state: &S,
    ) -> Result<Self, Self::Rejection> {
        let key = parts
            .headers
            .get("x-api-key")
            .and_then(|v| v.to_str().ok())
            .ok_or_else(|| AppError::BadRequest("missing x-api-key header".to_string()))?;

        Ok(ApiKey(key.to_string()))
    }
}

// Use it in a handler
async fn protected_endpoint(ApiKey(key): ApiKey) -> String {
    format!("Authenticated with key: {}...{}", &key[..4], &key[key.len()-4..])
}
```

### Nested Routers

Organize large APIs into modules using `Router::nest`:

```rust
fn items_router() -> Router<AppState> {
    Router::new()
        .route("/", get(list_items).post(create_item_v2))
        .route("/{id}", get(get_item_v2).put(update_item).delete(delete_item))
}

fn admin_router() -> Router<AppState> {
    Router::new()
        .route("/stats", get(admin_stats))
        .route("/reset", post(admin_reset))
}

fn app() -> Router {
    let state = AppState::new();

    Router::new()
        .route("/health", get(health))
        .nest("/api/v1/items", items_router())
        .nest("/admin", admin_router())
        .with_state(state)
}
// Routes: /health, /api/v1/items, /api/v1/items/:id, /admin/stats, /admin/reset
```

### Middleware Integration

Axum supports both tower layers and axum-native middleware:

```rust
use axum::middleware::{self, Next};
use axum::extract::Request;
use axum::response::Response;
use std::time::Instant;

// axum-native middleware (simpler syntax)
async fn timing_middleware(
    request: Request,
    next: Next,
) -> Response {
    let start = Instant::now();
    let method = request.method().clone();
    let uri = request.uri().clone();

    let response = next.run(request).await;

    let elapsed = start.elapsed();
    tracing::info!(
        method = %method,
        uri = %uri,
        status = %response.status(),
        latency_ms = elapsed.as_millis(),
        "request completed",
    );

    response
}

fn app() -> Router {
    let state = AppState::new();

    Router::new()
        .route("/items", get(list_items))
        .layer(middleware::from_fn(timing_middleware))
        // Tower layers also work:
        .layer(tower_http::trace::TraceLayer::new_for_http())
        .with_state(state)
}
```

You can also apply middleware to specific routes:

```rust
fn app() -> Router {
    let state = AppState::new();

    let public_routes = Router::new()
        .route("/health", get(health));

    let protected_routes = Router::new()
        .route("/items", get(list_items).post(create_item_v2))
        .route("/items/{id}", get(get_item_v2))
        .layer(middleware::from_fn(require_auth));

    Router::new()
        .merge(public_routes)
        .merge(protected_routes)
        .with_state(state)
}

async fn require_auth(
    request: Request,
    next: Next,
) -> Result<Response, StatusCode> {
    let auth_header = request
        .headers()
        .get("authorization")
        .and_then(|v| v.to_str().ok());

    match auth_header {
        Some(token) if token.starts_with("Bearer ") => {
            Ok(next.run(request).await)
        }
        _ => Err(StatusCode::UNAUTHORIZED),
    }
}
```

### WebSocket Support

Axum has built-in WebSocket support via the `ws` feature:

```rust
use axum::{
    extract::ws::{Message, WebSocket, WebSocketUpgrade},
    response::IntoResponse,
};

// The handler upgrades the HTTP connection to WebSocket
async fn ws_handler(ws: WebSocketUpgrade) -> impl IntoResponse {
    ws.on_upgrade(handle_socket)
}

// This function runs after the WebSocket handshake
async fn handle_socket(mut socket: WebSocket) {
    // Send a greeting
    if socket
        .send(Message::Text("hello from server".into()))
        .await
        .is_err()
    {
        return; // Client disconnected
    }

    // Echo loop
    while let Some(Ok(msg)) = socket.recv().await {
        match msg {
            Message::Text(text) => {
                let reply = format!("echo: {}", text);
                if socket.send(Message::Text(reply.into())).await.is_err() {
                    break;
                }
            }
            Message::Close(_) => break,
            _ => {} // Ignore binary, ping, pong
        }
    }
}

// With shared state for a chat room
use tokio::sync::broadcast;

#[derive(Clone)]
struct ChatState {
    tx: broadcast::Sender<String>,
}

async fn ws_chat(
    ws: WebSocketUpgrade,
    State(state): State<ChatState>,
) -> impl IntoResponse {
    ws.on_upgrade(move |socket| chat_socket(socket, state))
}

async fn chat_socket(mut socket: WebSocket, state: ChatState) {
    let mut rx = state.tx.subscribe();

    // Split into sender and receiver for concurrent read/write
    let (mut sender, mut receiver) = socket.split();

    // Task: forward broadcast messages to this client
    let mut send_task = tokio::spawn(async move {
        while let Ok(msg) = rx.recv().await {
            use futures_util::SinkExt;
            if sender.send(Message::Text(msg.into())).await.is_err() {
                break;
            }
        }
    });

    // Task: read from this client and broadcast
    let tx = state.tx.clone();
    let mut recv_task = tokio::spawn(async move {
        while let Some(Ok(Message::Text(text))) = receiver.next().await {
            let _ = tx.send(text.to_string());
        }
    });

    // Wait for either task to finish
    tokio::select! {
        _ = &mut send_task => recv_task.abort(),
        _ = &mut recv_task => send_task.abort(),
    }
}
```

### Testing Axum Applications

Axum provides excellent testing support. Since the router is a tower `Service`, you can call it directly without starting a server:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use axum::http::Request;
    use http_body_util::BodyExt;
    use tower::ServiceExt; // for oneshot

    #[tokio::test]
    async fn test_health() {
        let app = app();

        let request = Request::builder()
            .uri("/health")
            .body(Body::empty())
            .unwrap();

        let response = app.oneshot(request).await.unwrap();
        assert_eq!(response.status(), StatusCode::OK);

        let body = response.into_body().collect().await.unwrap().to_bytes();
        assert_eq!(&body[..], b"ok");
    }

    #[tokio::test]
    async fn test_create_and_get_item() {
        let app = app();

        // Create an item
        let create_request = Request::builder()
            .method("POST")
            .uri("/api/v1/items")
            .header("content-type", "application/json")
            .body(Body::from(
                serde_json::to_string(&serde_json::json!({
                    "name": "Test Item",
                    "description": "A test"
                }))
                .unwrap(),
            ))
            .unwrap();

        let response = app.clone().oneshot(create_request).await.unwrap();
        assert_eq!(response.status(), StatusCode::CREATED);

        let body = response.into_body().collect().await.unwrap().to_bytes();
        let item: Item = serde_json::from_slice(&body).unwrap();
        assert_eq!(item.name, "Test Item");
        assert!(!item.completed);

        // Get the item
        let get_request = Request::builder()
            .uri(&format!("/api/v1/items/{}", item.id))
            .body(Body::empty())
            .unwrap();

        let response = app.oneshot(get_request).await.unwrap();
        assert_eq!(response.status(), StatusCode::OK);

        let body = response.into_body().collect().await.unwrap().to_bytes();
        let fetched: Item = serde_json::from_slice(&body).unwrap();
        assert_eq!(fetched.id, item.id);
    }

    #[tokio::test]
    async fn test_not_found() {
        let app = app();

        let request = Request::builder()
            .uri("/api/v1/items/9999")
            .body(Body::empty())
            .unwrap();

        let response = app.oneshot(request).await.unwrap();
        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }

    #[tokio::test]
    async fn test_delete_item() {
        let app = app();

        // Create first
        let create = Request::builder()
            .method("POST")
            .uri("/api/v1/items")
            .header("content-type", "application/json")
            .body(Body::from(r#"{"name":"Delete Me","description":"bye"}"#))
            .unwrap();

        let response = app.clone().oneshot(create).await.unwrap();
        let body = response.into_body().collect().await.unwrap().to_bytes();
        let item: Item = serde_json::from_slice(&body).unwrap();

        // Delete
        let delete = Request::builder()
            .method("DELETE")
            .uri(&format!("/api/v1/items/{}", item.id))
            .body(Body::empty())
            .unwrap();

        let response = app.clone().oneshot(delete).await.unwrap();
        assert_eq!(response.status(), StatusCode::NO_CONTENT);

        // Verify gone
        let get = Request::builder()
            .uri(&format!("/api/v1/items/{}", item.id))
            .body(Body::empty())
            .unwrap();

        let response = app.oneshot(get).await.unwrap();
        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }
}
```

---

## Exercises

### Exercise 1: Full CRUD REST API

Build a complete REST API for a "bookmarks" service with these endpoints:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/bookmarks` | List all bookmarks (with optional `?tag=` filter) |
| `POST` | `/api/bookmarks` | Create a bookmark |
| `GET` | `/api/bookmarks/:id` | Get a single bookmark |
| `PUT` | `/api/bookmarks/:id` | Update a bookmark |
| `DELETE` | `/api/bookmarks/:id` | Delete a bookmark |
| `GET` | `/api/tags` | List all unique tags |
| `GET` | `/health` | Health check |

Requirements:
- Bookmark fields: `id` (u64), `url` (String), `title` (String), `tags` (Vec<String>), `created_at` (i64)
- Use `AppState` with `Arc<RwLock<HashMap<u64, Bookmark>>>` for storage
- Return structured JSON errors with appropriate status codes
- Validate: URL must not be empty, title must not be empty

**Cargo.toml:**

```toml
[package]
name = "axum-bookmarks"
edition = "2021"

[dependencies]
axum = "0.8"
tokio = { version = "1", features = ["full"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
tower = "0.5"
tower-http = { version = "0.6", features = ["trace", "cors"] }
tracing = "0.1"
tracing-subscriber = "0.3"
http-body-util = "0.1"
```

**Hints:**
- Use `Query<ListParams>` with `#[serde(default)]` for optional query parameters
- The `tags` endpoint can scan all bookmarks and collect unique tags into a `HashSet`
- For testing, `app.clone().oneshot(request)` lets you reuse the router across requests
- Use `http_body_util::BodyExt` to collect response bodies in tests

<details>
<summary>Solution</summary>

```rust
use axum::{
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::{get, delete},
    Json, Router,
};
use serde::{Deserialize, Serialize};
use std::collections::{HashMap, HashSet};
use std::sync::Arc;
use tokio::sync::RwLock;

// --- Types ---

#[derive(Debug, Clone, Serialize, Deserialize)]
struct Bookmark {
    id: u64,
    url: String,
    title: String,
    tags: Vec<String>,
    created_at: i64,
}

#[derive(Deserialize)]
struct CreateBookmark {
    url: String,
    title: String,
    #[serde(default)]
    tags: Vec<String>,
}

#[derive(Deserialize)]
struct UpdateBookmark {
    url: Option<String>,
    title: Option<String>,
    tags: Option<Vec<String>>,
}

#[derive(Deserialize)]
struct ListParams {
    tag: Option<String>,
}

// --- Error type ---

#[derive(Debug)]
enum AppError {
    NotFound(String),
    BadRequest(String),
}

#[derive(Serialize)]
struct ErrorBody {
    error: String,
    message: String,
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let (status, error, message) = match self {
            AppError::NotFound(msg) => (StatusCode::NOT_FOUND, "not_found", msg),
            AppError::BadRequest(msg) => (StatusCode::BAD_REQUEST, "bad_request", msg),
        };
        (status, Json(ErrorBody { error: error.into(), message })).into_response()
    }
}

// --- State ---

#[derive(Clone)]
struct AppState {
    bookmarks: Arc<RwLock<HashMap<u64, Bookmark>>>,
    next_id: Arc<std::sync::atomic::AtomicU64>,
}

impl AppState {
    fn new() -> Self {
        Self {
            bookmarks: Arc::new(RwLock::new(HashMap::new())),
            next_id: Arc::new(std::sync::atomic::AtomicU64::new(1)),
        }
    }

    fn next_id(&self) -> u64 {
        self.next_id.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
    }
}

// --- Handlers ---

async fn health() -> &'static str {
    "ok"
}

async fn list_bookmarks(
    State(state): State<AppState>,
    Query(params): Query<ListParams>,
) -> Json<Vec<Bookmark>> {
    let db = state.bookmarks.read().await;
    let bookmarks: Vec<Bookmark> = db
        .values()
        .filter(|b| {
            params.tag.as_ref().map_or(true, |tag| b.tags.contains(tag))
        })
        .cloned()
        .collect();
    Json(bookmarks)
}

async fn create_bookmark(
    State(state): State<AppState>,
    Json(payload): Json<CreateBookmark>,
) -> Result<(StatusCode, Json<Bookmark>), AppError> {
    if payload.url.trim().is_empty() {
        return Err(AppError::BadRequest("url must not be empty".into()));
    }
    if payload.title.trim().is_empty() {
        return Err(AppError::BadRequest("title must not be empty".into()));
    }

    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64;

    let bookmark = Bookmark {
        id: state.next_id(),
        url: payload.url,
        title: payload.title,
        tags: payload.tags,
        created_at: now,
    };

    state.bookmarks.write().await.insert(bookmark.id, bookmark.clone());
    Ok((StatusCode::CREATED, Json(bookmark)))
}

async fn get_bookmark(
    State(state): State<AppState>,
    Path(id): Path<u64>,
) -> Result<Json<Bookmark>, AppError> {
    let db = state.bookmarks.read().await;
    db.get(&id)
        .cloned()
        .map(Json)
        .ok_or_else(|| AppError::NotFound(format!("bookmark {} not found", id)))
}

async fn update_bookmark(
    State(state): State<AppState>,
    Path(id): Path<u64>,
    Json(payload): Json<UpdateBookmark>,
) -> Result<Json<Bookmark>, AppError> {
    let mut db = state.bookmarks.write().await;
    let bookmark = db
        .get_mut(&id)
        .ok_or_else(|| AppError::NotFound(format!("bookmark {} not found", id)))?;

    if let Some(url) = payload.url {
        if url.trim().is_empty() {
            return Err(AppError::BadRequest("url must not be empty".into()));
        }
        bookmark.url = url;
    }
    if let Some(title) = payload.title {
        if title.trim().is_empty() {
            return Err(AppError::BadRequest("title must not be empty".into()));
        }
        bookmark.title = title;
    }
    if let Some(tags) = payload.tags {
        bookmark.tags = tags;
    }

    Ok(Json(bookmark.clone()))
}

async fn delete_bookmark(
    State(state): State<AppState>,
    Path(id): Path<u64>,
) -> StatusCode {
    if state.bookmarks.write().await.remove(&id).is_some() {
        StatusCode::NO_CONTENT
    } else {
        StatusCode::NOT_FOUND
    }
}

async fn list_tags(State(state): State<AppState>) -> Json<Vec<String>> {
    let db = state.bookmarks.read().await;
    let tags: HashSet<String> = db
        .values()
        .flat_map(|b| b.tags.iter().cloned())
        .collect();
    let mut tags: Vec<String> = tags.into_iter().collect();
    tags.sort();
    Json(tags)
}

// --- Router ---

fn app() -> Router {
    let state = AppState::new();

    Router::new()
        .route("/health", get(health))
        .route("/api/bookmarks", get(list_bookmarks).post(create_bookmark))
        .route(
            "/api/bookmarks/{id}",
            get(get_bookmark).put(update_bookmark).delete(delete_bookmark),
        )
        .route("/api/tags", get(list_tags))
        .with_state(state)
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt::init();

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    tracing::info!("listening on http://0.0.0.0:3000");
    axum::serve(listener, app()).await.unwrap();
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use http::Request;
    use http_body_util::BodyExt;
    use tower::ServiceExt;

    async fn body_json<T: serde::de::DeserializeOwned>(response: Response) -> T {
        let body = response.into_body().collect().await.unwrap().to_bytes();
        serde_json::from_slice(&body).unwrap()
    }

    #[tokio::test]
    async fn test_health() {
        let app = app();
        let req = Request::get("/health").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
    }

    #[tokio::test]
    async fn test_crud_lifecycle() {
        let app = app();

        // Create
        let req = Request::post("/api/bookmarks")
            .header("content-type", "application/json")
            .body(Body::from(r#"{"url":"https://rust-lang.org","title":"Rust","tags":["lang"]}"#))
            .unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::CREATED);
        let bookmark: Bookmark = body_json(resp).await;
        assert_eq!(bookmark.title, "Rust");

        // Get
        let req = Request::get(&format!("/api/bookmarks/{}", bookmark.id))
            .body(Body::empty())
            .unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);

        // Update
        let req = Request::put(&format!("/api/bookmarks/{}", bookmark.id))
            .header("content-type", "application/json")
            .body(Body::from(r#"{"title":"Rust Lang"}"#))
            .unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
        let updated: Bookmark = body_json(resp).await;
        assert_eq!(updated.title, "Rust Lang");

        // Delete
        let req = Request::delete(&format!("/api/bookmarks/{}", bookmark.id))
            .body(Body::empty())
            .unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::NO_CONTENT);

        // Verify gone
        let req = Request::get(&format!("/api/bookmarks/{}", bookmark.id))
            .body(Body::empty())
            .unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::NOT_FOUND);
    }

    #[tokio::test]
    async fn test_tag_filter() {
        let app = app();

        // Create two bookmarks with different tags
        for (url, title, tag) in [
            ("https://a.com", "A", "rust"),
            ("https://b.com", "B", "go"),
        ] {
            let body = serde_json::json!({"url": url, "title": title, "tags": [tag]});
            let req = Request::post("/api/bookmarks")
                .header("content-type", "application/json")
                .body(Body::from(serde_json::to_string(&body).unwrap()))
                .unwrap();
            app.clone().oneshot(req).await.unwrap();
        }

        // Filter by tag=rust
        let req = Request::get("/api/bookmarks?tag=rust")
            .body(Body::empty())
            .unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        let bookmarks: Vec<Bookmark> = body_json(resp).await;
        assert_eq!(bookmarks.len(), 1);
        assert_eq!(bookmarks[0].title, "A");
    }

    #[tokio::test]
    async fn test_validation_errors() {
        let app = app();

        let req = Request::post("/api/bookmarks")
            .header("content-type", "application/json")
            .body(Body::from(r#"{"url":"","title":"No URL"}"#))
            .unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::BAD_REQUEST);

        let req = Request::post("/api/bookmarks")
            .header("content-type", "application/json")
            .body(Body::from(r#"{"url":"https://x.com","title":""}"#))
            .unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::BAD_REQUEST);
    }
}
```

**Trade-off analysis:**

| State management | Pros | Cons |
|---|---|---|
| `Arc<RwLock<HashMap>>` | Simple, no external deps | No persistence, all in memory |
| SQLx + SQLite/Postgres | Persistent, queryable | Added complexity, connection pool |
| `dashmap::DashMap` | No lock contention for concurrent reads | External dep, less control |
| Redis via `fred` | Shared across instances | Network latency, operational overhead |

</details>

### Exercise 2: Custom Extractor and Middleware

Build an authentication system using:

1. A custom `AuthUser` extractor that reads a JWT-like token from the `Authorization` header and extracts a user ID
2. An axum middleware that logs all requests with timing
3. Route-specific middleware: apply auth only to `/api/*` routes, leave `/health` unprotected

**Hints:**
- Implement `FromRequestParts<AppState>` for `AuthUser`
- The rejection type should be your `AppError` so it returns JSON error responses
- Use `middleware::from_fn_with_state` if you need access to state in middleware
- `Router::merge()` combines routers; apply `.layer()` to each independently

<details>
<summary>Solution</summary>

```rust
use axum::{
    extract::{FromRequestParts, Request, State},
    http::{request::Parts, StatusCode},
    middleware::{self, Next},
    response::{IntoResponse, Response},
    routing::get,
    Json, Router,
};
use serde::Serialize;
use std::time::Instant;

#[derive(Clone)]
struct AppState {
    valid_tokens: Vec<(String, AuthUser)>,
}

#[derive(Debug, Clone, Serialize)]
struct AuthUser {
    user_id: u64,
    name: String,
    role: String,
}

// --- Custom Extractor ---

impl FromRequestParts<AppState> for AuthUser {
    type Rejection = AppError;

    async fn from_request_parts(
        parts: &mut Parts,
        state: &AppState,
    ) -> Result<Self, Self::Rejection> {
        let header = parts
            .headers
            .get("authorization")
            .and_then(|v| v.to_str().ok())
            .ok_or_else(|| AppError::Unauthorized("missing authorization header".into()))?;

        let token = header
            .strip_prefix("Bearer ")
            .ok_or_else(|| AppError::Unauthorized("malformed authorization header".into()))?;

        state
            .valid_tokens
            .iter()
            .find(|(t, _)| t == token)
            .map(|(_, user)| user.clone())
            .ok_or_else(|| AppError::Unauthorized("invalid token".into()))
    }
}

#[derive(Debug)]
enum AppError {
    Unauthorized(String),
}

#[derive(Serialize)]
struct ErrorBody {
    error: String,
    message: String,
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let (status, error, message) = match self {
            AppError::Unauthorized(msg) => (StatusCode::UNAUTHORIZED, "unauthorized", msg),
        };
        (status, Json(ErrorBody { error: error.into(), message })).into_response()
    }
}

// --- Timing middleware ---

async fn timing(request: Request, next: Next) -> Response {
    let start = Instant::now();
    let method = request.method().clone();
    let uri = request.uri().clone();

    let response = next.run(request).await;

    println!(
        "[{}] {} {} -> {} ({:?})",
        chrono::Utc::now().format("%H:%M:%S"),
        method,
        uri,
        response.status(),
        start.elapsed(),
    );

    response
}

// --- Handlers ---

async fn health() -> &'static str {
    "ok"
}

async fn me(user: AuthUser) -> Json<AuthUser> {
    Json(user)
}

async fn protected_data(user: AuthUser) -> String {
    format!("Hello {}, you have role: {}", user.name, user.role)
}

fn app() -> Router {
    let state = AppState {
        valid_tokens: vec![
            (
                "token-alice".into(),
                AuthUser {
                    user_id: 1,
                    name: "Alice".into(),
                    role: "admin".into(),
                },
            ),
            (
                "token-bob".into(),
                AuthUser {
                    user_id: 2,
                    name: "Bob".into(),
                    role: "viewer".into(),
                },
            ),
        ],
    };

    let public = Router::new().route("/health", get(health));

    let protected = Router::new()
        .route("/api/me", get(me))
        .route("/api/data", get(protected_data));

    Router::new()
        .merge(public)
        .merge(protected)
        .layer(middleware::from_fn(timing))
        .with_state(state)
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use http::Request;
    use http_body_util::BodyExt;
    use tower::ServiceExt;

    #[tokio::test]
    async fn health_is_public() {
        let resp = app()
            .oneshot(Request::get("/health").body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
    }

    #[tokio::test]
    async fn api_requires_auth() {
        let resp = app()
            .oneshot(Request::get("/api/me").body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
    }

    #[tokio::test]
    async fn valid_token_works() {
        let resp = app()
            .oneshot(
                Request::get("/api/me")
                    .header("authorization", "Bearer token-alice")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::OK);

        let body = resp.into_body().collect().await.unwrap().to_bytes();
        let user: AuthUser = serde_json::from_slice(&body).unwrap();
        assert_eq!(user.name, "Alice");
    }

    #[tokio::test]
    async fn invalid_token_rejected() {
        let resp = app()
            .oneshot(
                Request::get("/api/me")
                    .header("authorization", "Bearer bad-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
    }
}
```

</details>

### Exercise 3: WebSocket Echo Server with Broadcast

Build a WebSocket chat application where:

1. Clients connect to `/ws`
2. Each message from a client is broadcast to all connected clients
3. The server tracks connected client count and exposes it at `GET /clients`
4. Handle disconnections gracefully

**Hints:**
- Use `tokio::sync::broadcast` for fan-out messaging
- Use `Arc<AtomicUsize>` to track connected client count
- Split the WebSocket with `.split()` for concurrent send/receive
- Use `tokio::select!` to handle both broadcast receive and socket receive

<details>
<summary>Solution</summary>

```rust
use axum::{
    extract::{
        ws::{Message, WebSocket, WebSocketUpgrade},
        State,
    },
    response::IntoResponse,
    routing::get,
    Json, Router,
};
use futures_util::{SinkExt, StreamExt};
use std::sync::{
    atomic::{AtomicUsize, Ordering},
    Arc,
};
use tokio::sync::broadcast;

#[derive(Clone)]
struct ChatState {
    tx: broadcast::Sender<String>,
    connected: Arc<AtomicUsize>,
}

impl ChatState {
    fn new() -> Self {
        let (tx, _) = broadcast::channel(256);
        Self {
            tx,
            connected: Arc::new(AtomicUsize::new(0)),
        }
    }
}

async fn ws_handler(
    ws: WebSocketUpgrade,
    State(state): State<ChatState>,
) -> impl IntoResponse {
    ws.on_upgrade(move |socket| handle_ws(socket, state))
}

async fn handle_ws(socket: WebSocket, state: ChatState) {
    state.connected.fetch_add(1, Ordering::Relaxed);
    let (mut sender, mut receiver) = socket.split();
    let mut rx = state.tx.subscribe();
    let tx = state.tx.clone();
    let connected = state.connected.clone();

    // Forward broadcasts to this client
    let mut send_task = tokio::spawn(async move {
        while let Ok(msg) = rx.recv().await {
            if sender.send(Message::Text(msg.into())).await.is_err() {
                break;
            }
        }
    });

    // Read from this client and broadcast
    let mut recv_task = tokio::spawn(async move {
        while let Some(Ok(msg)) = receiver.next().await {
            match msg {
                Message::Text(text) => {
                    let _ = tx.send(text.to_string());
                }
                Message::Close(_) => break,
                _ => {}
            }
        }
    });

    tokio::select! {
        _ = &mut send_task => recv_task.abort(),
        _ = &mut recv_task => send_task.abort(),
    }

    connected.fetch_sub(1, Ordering::Relaxed);
}

async fn client_count(State(state): State<ChatState>) -> Json<serde_json::Value> {
    Json(serde_json::json!({
        "connected_clients": state.connected.load(Ordering::Relaxed),
    }))
}

fn app() -> Router {
    let state = ChatState::new();

    Router::new()
        .route("/ws", get(ws_handler))
        .route("/clients", get(client_count))
        .with_state(state)
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    println!("Chat server on http://0.0.0.0:3000");
    println!("  WebSocket: ws://0.0.0.0:3000/ws");
    println!("  Clients:   http://0.0.0.0:3000/clients");
    axum::serve(listener, app()).await.unwrap();
}
```

</details>

## Common Mistakes

1. **Putting a body extractor before non-body extractors.** `Json` consumes the request body. If it appears before `State` or `Path`, those extractors work fine (they do not read the body), but having two body extractors causes a runtime error on the second one. Only one body extractor per handler.

2. **Using `std::sync::Mutex` with axum state.** The state is shared across async tasks. Holding a `std::sync::Mutex` guard across `.await` blocks the tokio worker thread. Use `tokio::sync::RwLock` or `tokio::sync::Mutex`.

3. **Forgetting `.with_state(state)` on the router.** Without this, `State<AppState>` extraction fails at compile time with a confusing trait bound error.

4. **Not implementing `Clone` on state.** Axum requires the state to be `Clone`. Wrap non-Clone inner data in `Arc`.

5. **Testing with `app.oneshot()` but forgetting the app is consumed.** `oneshot` consumes the service. Clone the router before each call: `app.clone().oneshot(req)`.

## Verification

```bash
cargo build
cargo run &

# Test with curl
curl http://localhost:3000/health
curl -X POST http://localhost:3000/api/bookmarks \
  -H 'content-type: application/json' \
  -d '{"url":"https://rust-lang.org","title":"Rust","tags":["lang"]}'
curl http://localhost:3000/api/bookmarks
curl http://localhost:3000/api/bookmarks?tag=lang

# Run tests
cargo test

# Lint
cargo clippy -- -W clippy::all
```

## Summary

Axum provides a type-safe, tower-based web framework where handlers are regular async functions and extractors provide compile-time guarantees about request parsing. State management with `Arc` and `RwLock` gives thread-safe shared data. Custom error types implementing `IntoResponse` produce structured JSON error responses. Nested routers organize large APIs into modules. WebSocket support is built-in via the `ws` extractor. Testing is straightforward because the router is a tower `Service` that can be called directly with `oneshot()` -- no HTTP server required.

## Resources

- [axum documentation](https://docs.rs/axum/0.8)
- [axum GitHub repository](https://github.com/tokio-rs/axum)
- [axum examples](https://github.com/tokio-rs/axum/tree/main/examples)
- [tower-http documentation](https://docs.rs/tower-http)
- [Real-world axum patterns](https://github.com/tokio-rs/axum/blob/main/ECOSYSTEM.md)
- [axum WebSocket example](https://github.com/tokio-rs/axum/tree/main/examples/websockets)
