# 39. gRPC with Tonic

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Completed: exercise 13 (serde and serialization)
- Familiarity with HTTP APIs and the concept of remote procedure calls
- Basic understanding of Protocol Buffers (protobuf) schema language

## Learning Objectives

- Define gRPC services using Protocol Buffer `.proto` files and generate Rust code via `tonic-build`
- Implement unary, server-streaming, client-streaming, and bidirectional-streaming RPCs
- Write interceptors for cross-cutting concerns like authentication and logging
- Handle errors using `tonic::Status` with appropriate gRPC status codes
- Enable server reflection for debugging with tools like `grpcurl`
- Test gRPC services in-process without binding to a network port

## Concepts

### Why gRPC in Rust

gRPC is a high-performance RPC framework built on HTTP/2 and Protocol Buffers. Compared to REST/JSON APIs, gRPC provides:

| Feature | REST/JSON | gRPC/Protobuf |
|---|---|---|
| Schema | OpenAPI (optional, external) | `.proto` file (required, source of truth) |
| Serialization | JSON (text, ~5x larger) | Protobuf (binary, compact) |
| Streaming | WebSockets (separate protocol) | Native (same connection) |
| Code generation | Optional | Required (client + server stubs) |
| HTTP version | HTTP/1.1 typical | HTTP/2 required |
| Browser support | Native | Requires grpc-web proxy |

Tonic is the dominant gRPC framework in Rust. It is built on top of `hyper` and `tower`, which means you can compose tower middleware (timeouts, rate limiting, tracing) with gRPC services the same way you do with axum.

### Protocol Buffer Definition

The `.proto` file is the contract between client and server. It defines messages (data structures) and services (RPC methods).

```protobuf
// proto/tasks.proto
syntax = "proto3";

package taskmanager;

// Data types
message Task {
  string id = 1;
  string title = 2;
  string description = 3;
  TaskStatus status = 4;
  int64 created_at = 5;
  int64 updated_at = 6;
  repeated string tags = 7;
}

enum TaskStatus {
  TASK_STATUS_UNSPECIFIED = 0;
  TASK_STATUS_TODO = 1;
  TASK_STATUS_IN_PROGRESS = 2;
  TASK_STATUS_DONE = 3;
}

message CreateTaskRequest {
  string title = 1;
  string description = 2;
  repeated string tags = 3;
}

message CreateTaskResponse {
  Task task = 1;
}

message GetTaskRequest {
  string id = 1;
}

message GetTaskResponse {
  Task task = 1;
}

message ListTasksRequest {
  TaskStatus status_filter = 1;
  int32 page_size = 2;
}

message UpdateTaskStatusRequest {
  string id = 1;
  TaskStatus new_status = 2;
}

message TaskEvent {
  string task_id = 1;
  string event_type = 2;
  int64 timestamp = 3;
}

message BatchCreateRequest {
  // Used in client-streaming: each message is one task
  string title = 1;
  string description = 2;
}

message BatchCreateResponse {
  int32 created_count = 1;
  repeated string ids = 2;
}

message TaskCommand {
  // Used in bidirectional streaming
  oneof command {
    string subscribe_tag = 1;
    string unsubscribe_tag = 2;
  }
}

// The service definition: four RPC patterns
service TaskManager {
  // Unary: single request, single response
  rpc CreateTask(CreateTaskRequest) returns (CreateTaskResponse);
  rpc GetTask(GetTaskRequest) returns (GetTaskResponse);

  // Server streaming: single request, stream of responses
  rpc ListTasks(ListTasksRequest) returns (stream Task);

  // Client streaming: stream of requests, single response
  rpc BatchCreate(stream BatchCreateRequest) returns (BatchCreateResponse);

  // Bidirectional streaming: stream in both directions
  rpc TaskFeed(stream TaskCommand) returns (stream TaskEvent);
}
```

Key proto3 rules: all fields are optional by default (zero-value if not set), enums must have a zero value, `repeated` fields are vectors, `oneof` is an enum in Rust.

### Code Generation with build.rs

Tonic uses a `build.rs` script to compile `.proto` files into Rust code at build time.

**Project structure:**

```
my-grpc-service/
  proto/
    tasks.proto
  src/
    main.rs
    server.rs
    client.rs
  build.rs
  Cargo.toml
```

**Cargo.toml:**

```toml
[package]
name = "task-grpc"
edition = "2021"

[dependencies]
tonic = "0.12"
tonic-reflection = "0.12"
prost = "0.13"
prost-types = "0.13"
tokio = { version = "1", features = ["full"] }
tokio-stream = "0.1"
uuid = { version = "1", features = ["v4"] }

[build-dependencies]
tonic-build = "0.12"
```

**build.rs:**

```rust
fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Compile proto files into Rust code
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        // Output file descriptor set for reflection
        .file_descriptor_set_path(
            std::path::PathBuf::from(std::env::var("OUT_DIR")?)
                .join("task_descriptor.bin"),
        )
        .compile_protos(&["proto/tasks.proto"], &["proto/"])?;

    Ok(())
}
```

The generated code lives in `OUT_DIR` and is included via:

```rust
pub mod taskmanager {
    tonic::include_proto!("taskmanager");
}
```

This generates:
- Rust structs for every `message` (with `prost` derive macros)
- A `task_manager_server` module with a trait you implement
- A `task_manager_client` module with a ready-to-use client
- Enum types mapping protobuf enums to Rust enums

### Implementing the Server

The generated trait has one method per RPC. You implement it on your service struct:

```rust
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::{Mutex, broadcast};
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Request, Response, Status};

pub mod taskmanager {
    tonic::include_proto!("taskmanager");
}

use taskmanager::task_manager_server::{TaskManager, TaskManagerServer};
use taskmanager::*;

#[derive(Debug)]
pub struct TaskService {
    tasks: Arc<Mutex<HashMap<String, Task>>>,
    event_tx: broadcast::Sender<TaskEvent>,
}

impl TaskService {
    pub fn new() -> Self {
        let (event_tx, _) = broadcast::channel(100);
        Self {
            tasks: Arc::new(Mutex::new(HashMap::new())),
            event_tx,
        }
    }
}

#[tonic::async_trait]
impl TaskManager for TaskService {
    // ---- Unary RPC ----
    async fn create_task(
        &self,
        request: Request<CreateTaskRequest>,
    ) -> Result<Response<CreateTaskResponse>, Status> {
        let req = request.into_inner();

        if req.title.is_empty() {
            return Err(Status::invalid_argument("title must not be empty"));
        }

        let now = chrono::Utc::now().timestamp();
        let task = Task {
            id: uuid::Uuid::new_v4().to_string(),
            title: req.title,
            description: req.description,
            status: TaskStatus::Todo.into(),
            created_at: now,
            updated_at: now,
            tags: req.tags,
        };

        let mut tasks = self.tasks.lock().await;
        tasks.insert(task.id.clone(), task.clone());

        // Broadcast event
        let _ = self.event_tx.send(TaskEvent {
            task_id: task.id.clone(),
            event_type: "created".to_string(),
            timestamp: now,
        });

        Ok(Response::new(CreateTaskResponse {
            task: Some(task),
        }))
    }

    async fn get_task(
        &self,
        request: Request<GetTaskRequest>,
    ) -> Result<Response<GetTaskResponse>, Status> {
        let id = request.into_inner().id;
        let tasks = self.tasks.lock().await;

        match tasks.get(&id) {
            Some(task) => Ok(Response::new(GetTaskResponse {
                task: Some(task.clone()),
            })),
            None => Err(Status::not_found(format!("task {} not found", id))),
        }
    }

    // ---- Server Streaming RPC ----
    type ListTasksStream = ReceiverStream<Result<Task, Status>>;

    async fn list_tasks(
        &self,
        request: Request<ListTasksRequest>,
    ) -> Result<Response<Self::ListTasksStream>, Status> {
        let req = request.into_inner();
        let tasks = self.tasks.lock().await;

        let filtered: Vec<Task> = tasks
            .values()
            .filter(|t| {
                req.status_filter == 0
                    || t.status == req.status_filter
            })
            .cloned()
            .collect();

        let (tx, rx) = tokio::sync::mpsc::channel(4);

        tokio::spawn(async move {
            for task in filtered {
                if tx.send(Ok(task)).await.is_err() {
                    break; // Client disconnected
                }
            }
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }

    // ---- Client Streaming RPC ----
    async fn batch_create(
        &self,
        request: Request<tonic::Streaming<BatchCreateRequest>>,
    ) -> Result<Response<BatchCreateResponse>, Status> {
        let mut stream = request.into_inner();
        let mut ids = Vec::new();
        let mut count = 0i32;

        while let Some(req) = stream.message().await? {
            let now = chrono::Utc::now().timestamp();
            let task = Task {
                id: uuid::Uuid::new_v4().to_string(),
                title: req.title,
                description: req.description,
                status: TaskStatus::Todo.into(),
                created_at: now,
                updated_at: now,
                tags: vec![],
            };

            let mut tasks = self.tasks.lock().await;
            ids.push(task.id.clone());
            tasks.insert(task.id.clone(), task);
            count += 1;
        }

        Ok(Response::new(BatchCreateResponse {
            created_count: count,
            ids,
        }))
    }

    // ---- Bidirectional Streaming RPC ----
    type TaskFeedStream = ReceiverStream<Result<TaskEvent, Status>>;

    async fn task_feed(
        &self,
        request: Request<tonic::Streaming<TaskCommand>>,
    ) -> Result<Response<Self::TaskFeedStream>, Status> {
        let mut command_stream = request.into_inner();
        let mut event_rx = self.event_tx.subscribe();
        let (tx, rx) = tokio::sync::mpsc::channel(32);

        tokio::spawn(async move {
            let mut subscribed_tags: Vec<String> = Vec::new();

            loop {
                tokio::select! {
                    // Process incoming commands
                    cmd = command_stream.message() => {
                        match cmd {
                            Ok(Some(command)) => {
                                if let Some(c) = command.command {
                                    match c {
                                        task_command::Command::SubscribeTag(tag) => {
                                            subscribed_tags.push(tag);
                                        }
                                        task_command::Command::UnsubscribeTag(tag) => {
                                            subscribed_tags.retain(|t| t != &tag);
                                        }
                                    }
                                }
                            }
                            Ok(None) => break, // Client closed stream
                            Err(_) => break,
                        }
                    }
                    // Forward matching events
                    event = event_rx.recv() => {
                        if let Ok(event) = event {
                            // In a real app, filter by subscribed_tags
                            if tx.send(Ok(event)).await.is_err() {
                                break; // Client disconnected
                            }
                        }
                    }
                }
            }
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }
}
```

### Interceptors for Authentication

Interceptors are functions that run before each RPC, similar to middleware. They inspect or modify the request metadata.

```rust
use tonic::{Request, Status};

/// An interceptor that checks for a valid bearer token in metadata.
fn auth_interceptor(req: Request<()>) -> Result<Request<()>, Status> {
    let token = req
        .metadata()
        .get("authorization")
        .and_then(|v| v.to_str().ok());

    match token {
        Some(t) if t.starts_with("Bearer ") => {
            let token_value = &t[7..];
            // In production: validate JWT, check expiry, etc.
            if token_value == "valid-token-123" {
                Ok(req)
            } else {
                Err(Status::unauthenticated("invalid token"))
            }
        }
        _ => Err(Status::unauthenticated("missing or malformed authorization header")),
    }
}

/// A logging interceptor that prints request metadata.
fn logging_interceptor(req: Request<()>) -> Result<Request<()>, Status> {
    println!(
        "[gRPC] method={:?} remote_addr={:?}",
        req.metadata().get("te"),
        req.remote_addr()
    );
    Ok(req)
}
```

Apply interceptors when building the server:

```rust
use tonic::service::interceptor;
use tower::ServiceBuilder;

// Single interceptor
let service = TaskManagerServer::with_interceptor(
    TaskService::new(),
    auth_interceptor,
);

// Or compose multiple via tower layers
let layer = ServiceBuilder::new()
    .layer(interceptor::InterceptorLayer::new(logging_interceptor))
    .layer(interceptor::InterceptorLayer::new(auth_interceptor))
    .into_inner();

let service = TaskManagerServer::new(TaskService::new());

tonic::transport::Server::builder()
    .layer(layer)
    .add_service(service)
    .serve("[::1]:50051".parse().unwrap())
    .await?;
```

### Error Handling with tonic::Status

`tonic::Status` maps to gRPC status codes. Use the right code for the right situation:

```rust
use tonic::Status;

// Validation errors
Status::invalid_argument("title must not be empty");

// Not found
Status::not_found(format!("task {} does not exist", id));

// Permission denied (authenticated but not authorized)
Status::permission_denied("user lacks admin role");

// Unauthenticated (no valid credentials)
Status::unauthenticated("token expired");

// Internal error (unexpected server failure)
Status::internal("database connection failed");

// Already exists (conflict)
Status::already_exists(format!("task with slug '{}' already exists", slug));

// Resource exhausted (rate limiting, quota)
Status::resource_exhausted("rate limit exceeded, retry after 10s");

// Deadline exceeded
Status::deadline_exceeded("operation did not complete within 30s");

// Unimplemented
Status::unimplemented("TaskFeed is not available in this build");
```

You can also attach metadata to error responses for machine-parseable error details:

```rust
fn rich_error(field: &str, message: &str) -> Status {
    let mut status = Status::invalid_argument(message);
    // Attach details as trailing metadata
    status.metadata_mut().insert(
        "x-error-field",
        field.parse().unwrap(),
    );
    status
}
```

### Server Reflection

Reflection lets tools like `grpcurl` discover services without `.proto` files:

```rust
use tonic_reflection::server::Builder as ReflectionBuilder;

// In main(), load the file descriptor set generated by build.rs
let reflection_service = ReflectionBuilder::configure()
    .register_encoded_file_descriptor_set(
        tonic::include_file_descriptor_set!("task_descriptor"),
    )
    .build_v1()?;

tonic::transport::Server::builder()
    .add_service(TaskManagerServer::new(TaskService::new()))
    .add_service(reflection_service)
    .serve(addr)
    .await?;
```

Now you can use `grpcurl`:

```bash
# List services
grpcurl -plaintext localhost:50051 list

# Describe a service
grpcurl -plaintext localhost:50051 describe taskmanager.TaskManager

# Call a unary RPC
grpcurl -plaintext -d '{"title": "My Task", "description": "Do something"}' \
  localhost:50051 taskmanager.TaskManager/CreateTask
```

### Implementing the Client

The generated client provides async methods matching each RPC:

```rust
use taskmanager::task_manager_client::TaskManagerClient;
use taskmanager::*;
use tonic::Request;

async fn run_client() -> Result<(), Box<dyn std::error::Error>> {
    let mut client = TaskManagerClient::connect("http://[::1]:50051").await?;

    // --- Unary call ---
    let response = client
        .create_task(Request::new(CreateTaskRequest {
            title: "Learn gRPC".to_string(),
            description: "Build a task service with Tonic".to_string(),
            tags: vec!["rust".to_string(), "grpc".to_string()],
        }))
        .await?;

    let task = response.into_inner().task.unwrap();
    println!("Created task: {} (id={})", task.title, task.id);

    // --- Server streaming ---
    let mut stream = client
        .list_tasks(Request::new(ListTasksRequest {
            status_filter: 0, // all
            page_size: 10,
        }))
        .await?
        .into_inner();

    while let Some(task) = stream.message().await? {
        println!("Listed task: {} [status={}]", task.title, task.status);
    }

    // --- Client streaming ---
    let requests = vec![
        BatchCreateRequest {
            title: "Task A".into(),
            description: "First batch task".into(),
        },
        BatchCreateRequest {
            title: "Task B".into(),
            description: "Second batch task".into(),
        },
    ];

    let request_stream = tokio_stream::iter(requests);
    let response = client.batch_create(request_stream).await?;
    println!("Batch created: {} tasks", response.into_inner().created_count);

    // --- Attach metadata (for auth) ---
    let mut request = Request::new(GetTaskRequest {
        id: task.id.clone(),
    });
    request
        .metadata_mut()
        .insert("authorization", "Bearer valid-token-123".parse().unwrap());

    let response = client.get_task(request).await?;
    println!("Got task: {:?}", response.into_inner().task);

    Ok(())
}
```

### Client Configuration

```rust
use tonic::transport::{Channel, Endpoint};
use std::time::Duration;

let channel = Endpoint::from_static("http://[::1]:50051")
    .timeout(Duration::from_secs(5))
    .connect_timeout(Duration::from_secs(3))
    .concurrency_limit(256)
    .tcp_keepalive(Some(Duration::from_secs(60)))
    .http2_keep_alive_interval(Duration::from_secs(30))
    .keep_alive_timeout(Duration::from_secs(20))
    .connect()
    .await?;

let client = TaskManagerClient::new(channel);
```

### Performance Characteristics

| Aspect | Detail |
|---|---|
| Serialization | Protobuf is ~2-5x faster than serde_json for similar payloads |
| HTTP/2 multiplexing | Multiple RPCs share one TCP connection |
| Streaming | No per-message connection overhead |
| Code generation | Compile-time cost, but zero runtime reflection |
| Binary size | Protobuf adds ~200KB; tonic + hyper + h2 add ~1-2MB |

---

## Exercises

### Exercise 1: Build the Complete Task Management Service

Implement the full task management gRPC service described above. Your service must support:

1. `CreateTask` -- unary RPC that validates title is non-empty
2. `GetTask` -- unary RPC that returns `NOT_FOUND` for unknown IDs
3. `ListTasks` -- server-streaming RPC that filters by status
4. `BatchCreate` -- client-streaming RPC that accepts a stream of tasks
5. An auth interceptor that requires a `Bearer` token in metadata

**Cargo.toml:**

```toml
[package]
name = "task-grpc"
edition = "2021"

[dependencies]
tonic = "0.12"
tonic-reflection = "0.12"
prost = "0.13"
tokio = { version = "1", features = ["full"] }
tokio-stream = "0.1"
uuid = { version = "1", features = ["v4"] }

[build-dependencies]
tonic-build = "0.12"
```

**Hints:**
- Use `tonic::include_proto!("taskmanager")` to include generated code
- The server-streaming return type must be a `ReceiverStream<Result<Task, Status>>`
- Use `tokio::sync::Mutex` (not `std::sync::Mutex`) for state in async code
- The `TaskStatus` enum maps to `i32` in the generated code; use `.into()` for conversion
- Test with `grpcurl` or write a Rust client binary

<details>
<summary>Solution</summary>

```rust
// build.rs
fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .file_descriptor_set_path(
            std::path::PathBuf::from(std::env::var("OUT_DIR")?)
                .join("task_descriptor.bin"),
        )
        .compile_protos(&["proto/tasks.proto"], &["proto/"])?;
    Ok(())
}

// src/main.rs
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Request, Response, Status};

pub mod taskmanager {
    tonic::include_proto!("taskmanager");
}

use taskmanager::task_manager_server::{TaskManager, TaskManagerServer};
use taskmanager::*;

#[derive(Debug)]
struct TaskService {
    tasks: Arc<Mutex<HashMap<String, Task>>>,
}

impl TaskService {
    fn new() -> Self {
        Self {
            tasks: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    fn make_task(title: String, description: String, tags: Vec<String>) -> Task {
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;

        Task {
            id: uuid::Uuid::new_v4().to_string(),
            title,
            description,
            status: TaskStatus::Todo.into(),
            created_at: now,
            updated_at: now,
            tags,
        }
    }
}

#[tonic::async_trait]
impl TaskManager for TaskService {
    async fn create_task(
        &self,
        request: Request<CreateTaskRequest>,
    ) -> Result<Response<CreateTaskResponse>, Status> {
        let req = request.into_inner();

        if req.title.trim().is_empty() {
            return Err(Status::invalid_argument("title must not be empty"));
        }

        let task = Self::make_task(req.title, req.description, req.tags);
        self.tasks.lock().await.insert(task.id.clone(), task.clone());

        Ok(Response::new(CreateTaskResponse { task: Some(task) }))
    }

    async fn get_task(
        &self,
        request: Request<GetTaskRequest>,
    ) -> Result<Response<GetTaskResponse>, Status> {
        let id = request.into_inner().id;
        let tasks = self.tasks.lock().await;

        tasks
            .get(&id)
            .cloned()
            .map(|task| Response::new(GetTaskResponse { task: Some(task) }))
            .ok_or_else(|| Status::not_found(format!("task '{}' not found", id)))
    }

    type ListTasksStream = ReceiverStream<Result<Task, Status>>;

    async fn list_tasks(
        &self,
        request: Request<ListTasksRequest>,
    ) -> Result<Response<Self::ListTasksStream>, Status> {
        let req = request.into_inner();
        let tasks = self.tasks.lock().await;

        let filtered: Vec<Task> = tasks
            .values()
            .filter(|t| req.status_filter == 0 || t.status == req.status_filter)
            .take(if req.page_size > 0 { req.page_size as usize } else { usize::MAX })
            .cloned()
            .collect();

        let (tx, rx) = tokio::sync::mpsc::channel(4);

        tokio::spawn(async move {
            for task in filtered {
                if tx.send(Ok(task)).await.is_err() {
                    break;
                }
            }
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }

    async fn batch_create(
        &self,
        request: Request<tonic::Streaming<BatchCreateRequest>>,
    ) -> Result<Response<BatchCreateResponse>, Status> {
        let mut stream = request.into_inner();
        let mut ids = Vec::new();

        while let Some(req) = stream.message().await? {
            let task = Self::make_task(req.title, req.description, vec![]);
            ids.push(task.id.clone());
            self.tasks.lock().await.insert(task.id.clone(), task);
        }

        let count = ids.len() as i32;
        Ok(Response::new(BatchCreateResponse {
            created_count: count,
            ids,
        }))
    }

    type TaskFeedStream = ReceiverStream<Result<TaskEvent, Status>>;

    async fn task_feed(
        &self,
        _request: Request<tonic::Streaming<TaskCommand>>,
    ) -> Result<Response<Self::TaskFeedStream>, Status> {
        Err(Status::unimplemented("TaskFeed not yet implemented"))
    }
}

fn auth_interceptor(req: Request<()>) -> Result<Request<()>, Status> {
    match req.metadata().get("authorization") {
        Some(value) => {
            let token = value
                .to_str()
                .map_err(|_| Status::unauthenticated("invalid metadata encoding"))?;
            if token.starts_with("Bearer ") && token.len() > 7 {
                Ok(req)
            } else {
                Err(Status::unauthenticated("malformed bearer token"))
            }
        }
        None => Err(Status::unauthenticated("missing authorization metadata")),
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr = "[::1]:50051".parse()?;
    let service = TaskService::new();

    let reflection_service =
        tonic_reflection::server::Builder::configure()
            .register_encoded_file_descriptor_set(
                tonic::include_file_descriptor_set!("task_descriptor"),
            )
            .build_v1()?;

    println!("TaskManager gRPC server listening on {}", addr);

    tonic::transport::Server::builder()
        .add_service(TaskManagerServer::with_interceptor(service, auth_interceptor))
        .add_service(reflection_service)
        .serve(addr)
        .await?;

    Ok(())
}
```

</details>

### Exercise 2: Bidirectional Streaming Task Feed

Extend the service to implement the `TaskFeed` bidirectional streaming RPC:

1. The client sends `TaskCommand` messages to subscribe/unsubscribe from tag-based filters
2. The server pushes `TaskEvent` messages whenever a task matching the subscribed tags is created or updated
3. Use `tokio::sync::broadcast` to distribute events from write operations to all active feeds

**Hints:**
- Store a `broadcast::Sender<TaskEvent>` in `TaskService`
- Each `TaskFeed` call subscribes to the broadcast channel
- Use `tokio::select!` to simultaneously read commands and forward events
- Include tag information in `TaskEvent` so the feed loop can filter
- Handle the case where the broadcast channel lags (receiver falls behind)

<details>
<summary>Solution</summary>

```rust
use tokio::sync::broadcast;

#[derive(Debug)]
struct TaskService {
    tasks: Arc<Mutex<HashMap<String, Task>>>,
    event_tx: broadcast::Sender<(TaskEvent, Vec<String>)>,
}

impl TaskService {
    fn new() -> Self {
        let (event_tx, _) = broadcast::channel(256);
        Self {
            tasks: Arc::new(Mutex::new(HashMap::new())),
            event_tx,
        }
    }
}

// In create_task, after inserting:
let _ = self.event_tx.send((
    TaskEvent {
        task_id: task.id.clone(),
        event_type: "created".to_string(),
        timestamp: now,
    },
    task.tags.clone(),
));

// The TaskFeed implementation:
type TaskFeedStream = ReceiverStream<Result<TaskEvent, Status>>;

async fn task_feed(
    &self,
    request: Request<tonic::Streaming<TaskCommand>>,
) -> Result<Response<Self::TaskFeedStream>, Status> {
    let mut command_stream = request.into_inner();
    let mut event_rx = self.event_tx.subscribe();
    let (tx, rx) = tokio::sync::mpsc::channel(32);

    tokio::spawn(async move {
        let mut subscribed_tags: std::collections::HashSet<String> =
            std::collections::HashSet::new();

        loop {
            tokio::select! {
                cmd = command_stream.message() => {
                    match cmd {
                        Ok(Some(command)) => {
                            if let Some(c) = command.command {
                                match c {
                                    task_command::Command::SubscribeTag(tag) => {
                                        subscribed_tags.insert(tag);
                                    }
                                    task_command::Command::UnsubscribeTag(tag) => {
                                        subscribed_tags.remove(&tag);
                                    }
                                }
                            }
                        }
                        _ => break,
                    }
                }
                event = event_rx.recv() => {
                    match event {
                        Ok((task_event, tags)) => {
                            // Send if any subscribed tag matches, or if no filter is active
                            let matches = subscribed_tags.is_empty()
                                || tags.iter().any(|t| subscribed_tags.contains(t));

                            if matches {
                                if tx.send(Ok(task_event)).await.is_err() {
                                    break;
                                }
                            }
                        }
                        Err(broadcast::error::RecvError::Lagged(n)) => {
                            eprintln!("Feed lagged, skipped {} events", n);
                            // Continue processing; some events were lost
                        }
                        Err(broadcast::error::RecvError::Closed) => break,
                    }
                }
            }
        }
    });

    Ok(Response::new(ReceiverStream::new(rx)))
}
```

</details>

### Exercise 3: In-Process Testing Without Network

Write integration tests that start the server in-process and connect to it without binding to a real TCP port. Use `tokio::net::UnixListener` or tonic's built-in testing support.

**Hints:**
- Use `tonic::transport::Server::builder()` with a `tokio::net::TcpListener` bound to `127.0.0.1:0` (random port)
- Extract the assigned port with `listener.local_addr()`
- Alternatively, use `tower::ServiceExt` to call the service directly as a tower `Service` without any network
- For the tower approach, wrap the service in `TaskManagerServer::new(service)` and call `tower::ServiceExt::ready(&mut svc).await?.call(request).await?`

<details>
<summary>Solution</summary>

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use taskmanager::task_manager_client::TaskManagerClient;
    use tokio::net::TcpListener;
    use tonic::transport::Server;

    async fn start_server() -> String {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let url = format!("http://{}", addr);

        let service = TaskService::new();

        tokio::spawn(async move {
            let incoming =
                tokio_stream::wrappers::TcpListenerStream::new(listener);

            Server::builder()
                .add_service(TaskManagerServer::new(service))
                .serve_with_incoming(incoming)
                .await
                .unwrap();
        });

        // Give the server a moment to start
        tokio::time::sleep(std::time::Duration::from_millis(50)).await;
        url
    }

    #[tokio::test]
    async fn test_create_and_get_task() {
        let url = start_server().await;
        let mut client = TaskManagerClient::connect(url).await.unwrap();

        // Create
        let response = client
            .create_task(Request::new(CreateTaskRequest {
                title: "Test task".into(),
                description: "A test".into(),
                tags: vec!["test".into()],
            }))
            .await
            .unwrap();

        let task = response.into_inner().task.unwrap();
        assert_eq!(task.title, "Test task");
        assert!(!task.id.is_empty());

        // Get
        let response = client
            .get_task(Request::new(GetTaskRequest {
                id: task.id.clone(),
            }))
            .await
            .unwrap();

        let fetched = response.into_inner().task.unwrap();
        assert_eq!(fetched.id, task.id);
        assert_eq!(fetched.title, "Test task");
    }

    #[tokio::test]
    async fn test_get_nonexistent_returns_not_found() {
        let url = start_server().await;
        let mut client = TaskManagerClient::connect(url).await.unwrap();

        let result = client
            .get_task(Request::new(GetTaskRequest {
                id: "nonexistent".into(),
            }))
            .await;

        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::NotFound);
    }

    #[tokio::test]
    async fn test_create_empty_title_returns_invalid_argument() {
        let url = start_server().await;
        let mut client = TaskManagerClient::connect(url).await.unwrap();

        let result = client
            .create_task(Request::new(CreateTaskRequest {
                title: "".into(),
                description: "no title".into(),
                tags: vec![],
            }))
            .await;

        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::InvalidArgument);
    }

    #[tokio::test]
    async fn test_list_tasks_streaming() {
        let url = start_server().await;
        let mut client = TaskManagerClient::connect(url).await.unwrap();

        // Create 3 tasks
        for i in 0..3 {
            client
                .create_task(Request::new(CreateTaskRequest {
                    title: format!("Task {}", i),
                    description: String::new(),
                    tags: vec![],
                }))
                .await
                .unwrap();
        }

        // List all
        let mut stream = client
            .list_tasks(Request::new(ListTasksRequest {
                status_filter: 0,
                page_size: 0,
            }))
            .await
            .unwrap()
            .into_inner();

        let mut count = 0;
        while let Some(Ok(_task)) = stream.message().await.transpose() {
            count += 1;
        }

        assert_eq!(count, 3);
    }

    #[tokio::test]
    async fn test_batch_create() {
        let url = start_server().await;
        let mut client = TaskManagerClient::connect(url).await.unwrap();

        let requests = vec![
            BatchCreateRequest {
                title: "Batch 1".into(),
                description: "First".into(),
            },
            BatchCreateRequest {
                title: "Batch 2".into(),
                description: "Second".into(),
            },
        ];

        let response = client
            .batch_create(tokio_stream::iter(requests))
            .await
            .unwrap();

        let result = response.into_inner();
        assert_eq!(result.created_count, 2);
        assert_eq!(result.ids.len(), 2);
    }
}
```

**Trade-off analysis:**

| Testing approach | Pros | Cons |
|---|---|---|
| Real TCP (random port) | Tests full stack including HTTP/2 | Slower, port allocation can conflict |
| In-process tower `Service` | Fastest, no network overhead | Skips HTTP/2 transport layer |
| `grpcurl` / external tool | Tests real wire protocol | Manual, hard to automate |
| Mock client | Isolates logic from transport | May miss serialization bugs |

</details>

## Common Mistakes

1. **Using `std::sync::Mutex` in async code.** Holding a `std::sync::Mutex` guard across an `.await` point will block the entire tokio worker thread. Use `tokio::sync::Mutex` or restructure to hold the lock briefly.

2. **Forgetting the `#[tonic::async_trait]` attribute.** The `TaskManager` trait implementation requires this attribute macro because Rust traits do not yet natively support async methods in all configurations tonic needs.

3. **Not handling stream termination.** When a streaming client disconnects, `tx.send()` returns `Err`. Always check the send result and break out of the loop.

4. **Ignoring broadcast lag.** `broadcast::Receiver::recv()` returns `RecvError::Lagged(n)` when the receiver falls behind. Log it and continue; do not treat it as a fatal error.

5. **Hardcoding ports in tests.** Use `:0` to let the OS assign a random available port. Hardcoded ports cause flaky tests in CI.

## Verification

```bash
# Generate code and build
cargo build

# Run the server
cargo run --bin server &

# Test with grpcurl (requires reflection)
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext -d '{"title":"Test","description":"Hello"}' \
  -H 'authorization: Bearer valid-token-123' \
  localhost:50051 taskmanager.TaskManager/CreateTask

# Run tests
cargo test

# Lint
cargo clippy -- -W clippy::all
```

## Summary

Tonic brings gRPC to Rust with full support for all four RPC patterns (unary, server streaming, client streaming, bidirectional streaming). The `build.rs` code generation step produces type-safe client and server stubs from `.proto` definitions. Interceptors handle cross-cutting concerns like authentication. Error handling maps naturally to gRPC status codes via `tonic::Status`. Because tonic is built on tower, you can compose the same middleware you use with axum or hyper. Server reflection enables discovery with `grpcurl`, and in-process testing avoids network overhead in CI.

## Resources

- [tonic documentation](https://docs.rs/tonic/0.12)
- [tonic GitHub repository](https://github.com/hyperium/tonic)
- [Protocol Buffers language guide](https://protobuf.dev/programming-guides/proto3/)
- [prost documentation](https://docs.rs/prost/0.13)
- [gRPC core concepts](https://grpc.io/docs/what-is-grpc/core-concepts/)
- [tonic examples](https://github.com/hyperium/tonic/tree/master/examples)
