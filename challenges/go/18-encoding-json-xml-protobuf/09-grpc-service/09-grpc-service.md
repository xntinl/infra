# Exercise 09: gRPC Service

**Difficulty:** Advanced | **Estimated Time:** 40 minutes | **Section:** 18 - Encoding

## Overview

gRPC is a high-performance RPC framework built on Protocol Buffers and HTTP/2. It generates both client and server code from `.proto` service definitions, handles serialization automatically, and supports unary calls, server streaming, client streaming, and bidirectional streaming. This exercise builds a complete gRPC service.

## Prerequisites

- Exercise 08 (Protocol Buffers)
- Goroutines and channels (basic)
- Client-server concepts

## Problem

Build a **Task Manager** gRPC service with these RPCs:

1. **CreateTask** (unary) -- takes a task title, description, and priority; returns the created task with an ID
2. **GetTask** (unary) -- takes a task ID; returns the task or an error if not found
3. **ListTasks** (server streaming) -- takes a filter (optional status); streams all matching tasks one by one
4. **UpdateStatus** (unary) -- takes a task ID and new status; returns the updated task

### Proto Definition

```protobuf
syntax = "proto3";
package taskmanager;
option go_package = "gen/taskmanager";

import "google/protobuf/timestamp.proto";

enum Priority {
  LOW = 0;
  MEDIUM = 1;
  HIGH = 2;
}

enum Status {
  TODO = 0;
  IN_PROGRESS = 1;
  DONE = 2;
}

message Task {
  int32 id = 1;
  string title = 2;
  string description = 3;
  Priority priority = 4;
  Status status = 5;
  google.protobuf.Timestamp created_at = 6;
}

message CreateTaskRequest {
  string title = 1;
  string description = 2;
  Priority priority = 3;
}

message GetTaskRequest {
  int32 id = 1;
}

message ListTasksRequest {
  Status filter_status = 1;
  bool has_filter = 2;
}

message UpdateStatusRequest {
  int32 id = 1;
  Status new_status = 2;
}

service TaskManager {
  rpc CreateTask(CreateTaskRequest) returns (Task);
  rpc GetTask(GetTaskRequest) returns (Task);
  rpc ListTasks(ListTasksRequest) returns (stream Task);
  rpc UpdateStatus(UpdateStatusRequest) returns (Task);
}
```

### Implementation Requirements

**Server:**
- Store tasks in-memory (map or slice, protected by a mutex)
- Auto-increment IDs
- Set `created_at` on creation
- Return `codes.NotFound` for missing tasks
- For `ListTasks`, send each task with a small delay (50ms) to demonstrate streaming

**Client:**
- Create 3 tasks with different priorities
- Get a task by ID
- Update a task's status
- List all tasks using the stream
- Print results at each step

## Setup

```bash
# Additional plugin for gRPC
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate both proto and gRPC code
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/taskmanager.proto
```

Dependencies:

```bash
go get google.golang.org/grpc
go get google.golang.org/protobuf
```

## Hints

- The generated code creates an interface (e.g., `TaskManagerServer`) you must implement.
- Use `status.Errorf(codes.NotFound, "task %d not found", id)` for gRPC errors.
- For server streaming, the handler receives a `stream` argument with a `Send(*Task)` method.
- Start the server in a goroutine, then run the client in `main`. Use `net.Listen("tcp", ":0")` to get a random port, or pick a fixed port like `:50051`.
- The client uses `grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))`.
- For the streaming response, call `stream.Recv()` in a loop until `io.EOF`.
- Protect the in-memory store with `sync.Mutex` since gRPC handlers run concurrently.

## Verification Criteria

- Server starts and accepts connections
- Client successfully creates tasks, retrieves them, updates status, and lists via stream
- NotFound error is returned for a non-existent task ID
- Streaming output shows tasks arriving one by one
- Clean shutdown (server stops after client finishes)

## Stretch Goals

- Add a `DeleteTask` RPC
- Add client-side streaming: `ImportTasks(stream CreateTaskRequest) returns (ImportSummary)`
- Add request validation (title must not be empty)
- Add gRPC interceptors for logging

## Key Takeaways

- gRPC generates client and server stubs from `.proto` service definitions
- Four RPC patterns: unary, server streaming, client streaming, bidirectional
- Use `status` and `codes` packages for typed gRPC errors
- The server implements a generated interface; the client uses a generated stub
- gRPC uses HTTP/2 and protobuf, giving it significant performance advantages over REST+JSON
