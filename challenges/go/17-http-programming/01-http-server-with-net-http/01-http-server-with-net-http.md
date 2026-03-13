# 1. HTTP Server with net/http

<!--
difficulty: basic
concepts: [http-handlefunc, listenandserve, responsewriter, request, handler-functions]
tools: [go, curl]
estimated_time: 20m
bloom_level: remember
prerequisites: [functions, structs, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- `curl` available for testing

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** how to register a handler with `http.HandleFunc`
- **Use** `http.ListenAndServe` to start an HTTP server
- **Identify** the roles of `http.ResponseWriter` and `*http.Request`

## Why net/http

Go's standard library includes a production-grade HTTP server. Unlike many languages where you need a third-party framework, `net/http` handles routing, request parsing, and connection management out of the box. Many Go web services in production run on nothing more than `net/http`.

The two core pieces are `http.HandleFunc`, which maps a URL pattern to a function, and `http.ListenAndServe`, which starts the server. The handler function receives a `ResponseWriter` to write the response and a `*Request` containing everything about the incoming request.

## Step 1 -- Create a Basic HTTP Server

```bash
mkdir -p ~/go-exercises/http-server
cd ~/go-exercises/http-server
go mod init http-server
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"net/http"
)

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, World!")
}

func main() {
	http.HandleFunc("/hello", helloHandler)
	fmt.Println("Server starting on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Server error:", err)
	}
}
```

`http.HandleFunc` registers `helloHandler` for the `/hello` path. Passing `nil` as the second argument to `ListenAndServe` uses the default `ServeMux`.

### Intermediate Verification

In one terminal:

```bash
go run main.go
```

In another terminal:

```bash
curl http://localhost:8080/hello
```

Expected:

```
Hello, World!
```

Stop the server with Ctrl+C.

## Step 2 -- Read Request Information

Modify `helloHandler` to read query parameters and the HTTP method:

```go
func helloHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "World"
	}
	fmt.Fprintf(w, "Hello, %s!\n", name)
	fmt.Fprintf(w, "Method: %s\n", r.Method)
	fmt.Fprintf(w, "Path: %s\n", r.URL.Path)
}
```

### Intermediate Verification

```bash
curl "http://localhost:8080/hello?name=Gopher"
```

Expected:

```
Hello, Gopher!
Method: GET
Path: /hello
```

## Step 3 -- Add Multiple Routes

Add a second handler and set response headers:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func helloHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "World"
	}
	fmt.Fprintf(w, "Hello, %s!\n", name)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func main() {
	http.HandleFunc("/hello", helloHandler)
	http.HandleFunc("/health", healthHandler)

	fmt.Println("Server starting on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Server error:", err)
	}
}
```

### Intermediate Verification

```bash
curl -i http://localhost:8080/health
```

Expected:

```
HTTP/1.1 200 OK
Content-Type: application/json
...

{"status":"ok"}
```

## Step 4 -- Set Status Codes

Add a handler that returns different status codes:

```go
func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, "page not found: %s\n", r.URL.Path)
}

func main() {
	http.HandleFunc("/hello", helloHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/", notFoundHandler)

	fmt.Println("Server starting on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Server error:", err)
	}
}
```

The `/` pattern matches any path that is not handled by a more specific pattern.

### Intermediate Verification

```bash
curl -i http://localhost:8080/nonexistent
```

Expected:

```
HTTP/1.1 404 Not Found
...

page not found: /nonexistent
```

## Common Mistakes

### Calling WriteHeader After Write

**Wrong:**

```go
fmt.Fprintf(w, "Hello")
w.WriteHeader(http.StatusCreated) // too late
```

**What happens:** The status code defaults to 200 on the first `Write` call. Calling `WriteHeader` afterward has no effect and logs a warning.

**Fix:** Always call `w.WriteHeader()` before writing the body.

### Forgetting to Return After Writing an Error

**Wrong:**

```go
if err != nil {
	http.Error(w, "bad request", http.StatusBadRequest)
	// continues executing below
}
```

**What happens:** The handler keeps running and may write more data to the response.

**Fix:** Add `return` after `http.Error`.

### Using the Default Mux in Production

**What happens:** The default `ServeMux` is a global, so any package that imports your code can register routes on it.

**Fix:** Create your own `http.NewServeMux()` for isolation.

## Verify What You Learned

Run the server and test all three endpoints:

```bash
curl http://localhost:8080/hello?name=Go
curl http://localhost:8080/health
curl -i http://localhost:8080/anything
```

Confirm the hello endpoint greets by name, health returns JSON with status ok, and unknown paths return 404.

## What's Next

Continue to [02 - HTTP Client](../02-http-client/02-http-client.md) to learn how to make HTTP requests from Go.

## Summary

- `http.HandleFunc(pattern, handler)` registers a handler function for a URL pattern
- `http.ListenAndServe(addr, handler)` starts the server; pass `nil` for the default mux
- `http.ResponseWriter` is used to write headers and response body
- `*http.Request` contains the method, URL, headers, and body of the incoming request
- Call `WriteHeader` before writing the body to set the status code

## Reference

- [net/http package](https://pkg.go.dev/net/http)
- [Writing Web Applications](https://go.dev/doc/articles/wiki/)
- [http.HandleFunc](https://pkg.go.dev/net/http#HandleFunc)
