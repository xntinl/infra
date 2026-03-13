# 3. ServeMux Routing and Patterns

<!--
difficulty: intermediate
concepts: [servemux, go-1-22-routing, method-routing, path-parameters, wildcard-patterns]
tools: [go, curl]
estimated_time: 25m
bloom_level: apply
prerequisites: [http-server, http-handlefunc]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [01 - HTTP Server with net/http](../01-http-server-with-net-http/01-http-server-with-net-http.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** Go 1.22's enhanced `ServeMux` patterns with method and path parameters
- **Use** `{name}` wildcards and `{name...}` catch-all patterns
- **Distinguish** between the default mux and a custom `http.NewServeMux()`

## Why Enhanced ServeMux

Before Go 1.22, the default `ServeMux` only supported exact path matching and prefix matching with `/`. You needed third-party routers for method-based routing or path parameters. Go 1.22 added method prefixes (`GET /path`), wildcards (`/users/{id}`), and catch-all patterns (`/files/{path...}`), making third-party routers unnecessary for most applications.

## Step 1 -- Method-Based Routing

Create a server that routes based on HTTP method:

```bash
mkdir -p ~/go-exercises/servemux-routing
cd ~/go-exercises/servemux-routing
go mod init servemux-routing
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /items", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "List all items")
	})

	mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Create a new item")
	})

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

The pattern `"GET /items"` only matches GET requests to `/items`. A POST to the same path goes to the POST handler.

### Intermediate Verification

```bash
curl http://localhost:8080/items
```

Expected: `List all items`

```bash
curl -X POST http://localhost:8080/items
```

Expected: `Create a new item`

```bash
curl -X DELETE http://localhost:8080/items
```

Expected: `405 Method Not Allowed` (automatic when only specific methods are registered).

## Step 2 -- Path Parameters with Wildcards

Use `{name}` to capture path segments:

```go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		fmt.Fprintf(w, "User ID: %s\n", id)
	})

	mux.HandleFunc("GET /users/{id}/posts/{postID}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		postID := r.PathValue("postID")
		fmt.Fprintf(w, "User %s, Post %s\n", id, postID)
	})

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

`r.PathValue("id")` extracts the value matched by `{id}`.

### Intermediate Verification

```bash
curl http://localhost:8080/users/42
```

Expected: `User ID: 42`

```bash
curl http://localhost:8080/users/42/posts/7
```

Expected: `User 42, Post 7`

## Step 3 -- Catch-All Wildcards

Use `{path...}` to match the remainder of the URL:

```go
mux.HandleFunc("GET /files/{path...}", func(w http.ResponseWriter, r *http.Request) {
	filePath := r.PathValue("path")
	fmt.Fprintf(w, "Requested file: %s\n", filePath)
})
```

### Intermediate Verification

```bash
curl http://localhost:8080/files/docs/readme.txt
```

Expected: `Requested file: docs/readme.txt`

## Step 4 -- Combine Into a Full CRUD Router

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var (
	items = make(map[string]Item)
	mu    sync.Mutex
	nextID = 1
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /items", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		result := make([]Item, 0, len(items))
		for _, item := range items {
			result = append(result, item)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
		var item Item
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		mu.Lock()
		item.ID = fmt.Sprintf("%d", nextID)
		nextID++
		items[item.ID] = item
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(item)
	})

	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		item, ok := items[r.PathValue("id")]
		mu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(item)
	})

	mux.HandleFunc("DELETE /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		mu.Lock()
		_, ok := items[id]
		if ok {
			delete(items, id)
		}
		mu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

### Intermediate Verification

```bash
curl -X POST http://localhost:8080/items -d '{"name":"Widget"}' -H "Content-Type: application/json"
curl http://localhost:8080/items
curl http://localhost:8080/items/1
curl -X DELETE http://localhost:8080/items/1
curl http://localhost:8080/items/1
```

Expected: Create returns 201 with the item, list returns the item, get returns it by ID, delete returns 204, and subsequent get returns 404.

## Common Mistakes

### Forgetting the Method Prefix

**Wrong:** `mux.HandleFunc("/items", handler)` -- this matches all methods.

**Fix:** Use `"GET /items"` to restrict to GET only.

### PathValue Returns Empty String for Missing Wildcards

**Wrong:** Calling `r.PathValue("id")` when the pattern does not contain `{id}`.

**What happens:** Returns an empty string, no error.

**Fix:** Ensure the pattern and `PathValue` key match exactly.

### Pattern Precedence Conflicts

When two patterns could match, Go 1.22 uses the most specific one. `GET /items/{id}` is more specific than `GET /items/{id...}`. If neither is more specific, registration panics.

## Verify What You Learned

Test the full CRUD server:

```bash
curl -s -X POST http://localhost:8080/items -d '{"name":"Gadget"}' -H "Content-Type: application/json" | jq .
curl -s http://localhost:8080/items | jq .
```

Confirm items are created, listed, fetched by ID, and deleted.

## What's Next

Continue to [04 - Middleware Chains](../04-middleware-chains/04-middleware-chains.md) to learn how to wrap handlers with cross-cutting concerns.

## Summary

- Go 1.22 `ServeMux` supports method prefixes: `"GET /path"`, `"POST /path"`
- Path parameters use `{name}` syntax, extracted with `r.PathValue("name")`
- Catch-all wildcards use `{name...}` to match the rest of the path
- `http.NewServeMux()` creates an isolated mux instead of using the global default
- Unmatched methods return 405 automatically when specific methods are registered

## Reference

- [Go 1.22 Release Notes - Enhanced Routing](https://go.dev/doc/go1.22#enhanced_routing_patterns)
- [net/http ServeMux](https://pkg.go.dev/net/http#ServeMux)
- [Routing Enhancements Proposal](https://go.dev/blog/routing-enhancements)
