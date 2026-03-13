# 13. Building a REST API

<!--
difficulty: insane
concepts: [rest-api, crud, routing, middleware, json, validation, error-handling, repository-pattern, graceful-shutdown]
tools: [go, curl]
estimated_time: 90m
bloom_level: create
prerequisites: [http-server-with-net-http, servemux-routing-and-patterns, middleware-chains, request-body-parsing-and-validation, http-client, json-marshal-unmarshal]
-->

## The Challenge

Build a complete REST API for a task management system using only the standard library (`net/http`, `encoding/json`). The API must support full CRUD operations, proper HTTP semantics (status codes, methods, content negotiation), structured error responses, input validation, middleware (logging, recovery, request ID), and graceful shutdown. No third-party routers or frameworks.

The difficulty lies in building a production-quality API without the conveniences of frameworks like Gin or Echo. You must handle routing with path parameters using Go 1.22+ `ServeMux` patterns, implement proper content-type negotiation, build a consistent error response format, layer middleware correctly, and structure the code with separation of concerns (handler -> service -> repository).

## Requirements

### Data Model

1. A `Task` resource with fields: `ID` (UUID string), `Title`, `Description`, `Status` (pending/in_progress/done), `Priority` (low/medium/high), `CreatedAt`, `UpdatedAt`
2. An in-memory repository (no database required) protected by `sync.RWMutex`

### API Endpoints

3. `POST /api/v1/tasks` -- create a task (return 201 with `Location` header)
4. `GET /api/v1/tasks` -- list all tasks with filtering by status and priority (query params)
5. `GET /api/v1/tasks/{id}` -- get a single task (return 404 if not found)
6. `PUT /api/v1/tasks/{id}` -- full update of a task (return 404 if not found)
7. `PATCH /api/v1/tasks/{id}` -- partial update (only provided fields are changed)
8. `DELETE /api/v1/tasks/{id}` -- delete a task (return 204 on success)

### HTTP Semantics

9. Return appropriate status codes: 200, 201, 204, 400, 404, 405, 415, 500
10. Set `Content-Type: application/json` on all JSON responses
11. Reject requests without `Content-Type: application/json` on POST/PUT/PATCH (return 415)
12. Return `Allow` header on 405 Method Not Allowed responses
13. Support pagination on list endpoint: `?page=1&per_page=20` with pagination metadata in response

### Error Handling

14. All errors return a consistent JSON structure: `{"error": {"code": "NOT_FOUND", "message": "task not found", "details": {}}}`
15. Validation errors include field-level details: `{"error": {"code": "VALIDATION_ERROR", "message": "invalid input", "details": {"title": "required", "priority": "must be low, medium, or high"}}}`
16. Panics in handlers are recovered and return 500 with an error response

### Middleware

17. Request logging middleware: method, path, status code, duration
18. Recovery middleware: catch panics, log stack trace, return 500
19. Request ID middleware: generate a UUID, set `X-Request-ID` header, include in logs
20. Middleware is composable and applied in the correct order

### Graceful Shutdown

21. Listen for SIGINT/SIGTERM and shut down the server with a timeout
22. In-flight requests complete before the server exits

## Hints

<details>
<summary>Hint 1: Go 1.22+ ServeMux with path parameters</summary>

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /api/v1/tasks", listTasks)
mux.HandleFunc("POST /api/v1/tasks", createTask)
mux.HandleFunc("GET /api/v1/tasks/{id}", getTask)
mux.HandleFunc("PUT /api/v1/tasks/{id}", updateTask)
mux.HandleFunc("PATCH /api/v1/tasks/{id}", patchTask)
mux.HandleFunc("DELETE /api/v1/tasks/{id}", deleteTask)
```

Extract the path parameter with `r.PathValue("id")`.
</details>

<details>
<summary>Hint 2: Consistent error response</summary>

```go
type APIError struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]string `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(map[string]APIError{
        "error": {Code: code, Message: message, Details: details},
    })
}
```
</details>

<details>
<summary>Hint 3: Middleware chaining</summary>

```go
type Middleware func(http.Handler) http.Handler

func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
    for i := len(middlewares) - 1; i >= 0; i-- {
        handler = middlewares[i](handler)
    }
    return handler
}

// Usage:
server := Chain(mux, RequestID, Logger, Recovery)
```
</details>

<details>
<summary>Hint 4: Graceful shutdown</summary>

```go
srv := &http.Server{Addr: ":8080", Handler: handler}

go func() {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    srv.Shutdown(ctx)
}()

if err := srv.ListenAndServe(); err != http.ErrServerClosed {
    log.Fatal(err)
}
```
</details>

## Success Criteria

- [ ] All six CRUD endpoints work correctly with proper HTTP methods
- [ ] `POST` returns 201 with a `Location` header pointing to the new resource
- [ ] `DELETE` returns 204 with no body
- [ ] `GET /api/v1/tasks/{id}` returns 404 for non-existent tasks
- [ ] Filtering by `?status=done` and `?priority=high` works on the list endpoint
- [ ] Pagination with `?page=2&per_page=5` returns correct slices and metadata
- [ ] Validation errors return 400 with field-level details
- [ ] Requests without `Content-Type: application/json` on POST/PUT/PATCH return 415
- [ ] Every response includes `X-Request-ID` header
- [ ] Request logs show method, path, status code, and duration
- [ ] A panic in a handler returns 500 with a JSON error (not a crash)
- [ ] SIGINT triggers graceful shutdown (in-flight requests complete)
- [ ] No data races under concurrent requests (`go test -race` or concurrent `curl`)
- [ ] The API is testable with `curl` commands that demonstrate all features

## Research Resources

- [net/http ServeMux patterns (Go 1.22)](https://pkg.go.dev/net/http#ServeMux) -- method and path parameter routing
- [encoding/json](https://pkg.go.dev/encoding/json) -- JSON marshaling/unmarshaling
- [HTTP Status Codes (MDN)](https://developer.mozilla.org/en-US/docs/Web/HTTP/Status) -- when to use each status code
- [REST API Design Best Practices](https://restfulapi.net/) -- resource naming, status codes, error format
- [How I Write HTTP Services in Go (blog)](https://grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/) -- Mat Ryer's patterns for Go HTTP services
- [http.Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown) -- graceful shutdown documentation
