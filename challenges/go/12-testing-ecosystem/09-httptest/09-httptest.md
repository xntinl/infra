<!-- difficulty: intermediate -->
<!-- concepts: httptest.NewServer, httptest.NewRecorder -->
<!-- tools: go test -->
<!-- estimated_time: 25m -->
<!-- bloom_level: apply -->
<!-- prerequisites: 01-your-first-test, 08-mocking-with-interfaces -->

# httptest

## Prerequisites

Before starting this exercise, you should be comfortable with:
- HTTP handlers (`http.Handler`, `http.HandlerFunc`)
- JSON encoding/decoding
- Interface-based testing

## Learning Objectives

By the end of this exercise, you will be able to:
1. Test HTTP handlers with `httptest.NewRecorder`
2. Test HTTP clients with `httptest.NewServer`
3. Verify response status codes, headers, and bodies
4. Build request objects for handler testing

## Why This Matters

Almost every Go service exposes or consumes HTTP APIs. The `net/http/httptest` package lets you test both sides without starting a real server or making real network calls. `httptest.NewRecorder` captures what your handler writes, and `httptest.NewServer` creates a local test server your client code can hit. Both run entirely in-process, making tests fast and reliable.

## Instructions

You will test an HTTP API server and an HTTP client.

### Scaffold

```bash
mkdir -p userapi && cd userapi
go mod init userapi
```

`userapi.go`:

```go
package userapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// User represents a user in the system.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserHandler handles HTTP requests for users.
type UserHandler struct {
	mu    sync.RWMutex
	users map[string]*User
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler() *UserHandler {
	return &UserHandler{users: make(map[string]*User)}
}

// ServeHTTP routes requests to the appropriate method handler.
func (h *UserHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getUser(w, r)
	case http.MethodPost:
		h.createUser(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *UserHandler) getUser(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/users/")
	if id == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}

	h.mu.RLock()
	user, ok := h.users[id]
	h.mu.RUnlock()

	if !ok {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (h *UserHandler) createUser(w http.ResponseWriter, r *http.Request) {
	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if user.ID == "" || user.Name == "" {
		http.Error(w, "id and name required", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	h.users[user.ID] = &user
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

// UserClient is an HTTP client for the user API.
type UserClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// GetUser fetches a user by ID.
func (c *UserClient) GetUser(id string) (*User, error) {
	resp, err := c.HTTPClient.Get(fmt.Sprintf("%s/users/%s", c.BaseURL, id))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("user %s not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	return &user, nil
}
```

### Your Task

Create `userapi_test.go` with:

**1. Test the handler with `httptest.NewRecorder`**:

```go
package userapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateUser(t *testing.T) {
	handler := NewUserHandler()

	body := `{"id":"1","name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}

	var user User
	if err := json.NewDecoder(rec.Body).Decode(&user); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if user.Name != "Alice" {
		t.Errorf("name = %q, want Alice", user.Name)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	handler := NewUserHandler()

	req := httptest.NewRequest(http.MethodGet, "/users/999", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCreateThenGet(t *testing.T) {
	handler := NewUserHandler()

	// Create
	body := `{"id":"42","name":"Bob","email":"bob@example.com"}`
	createReq := httptest.NewRequest(http.MethodPost, "/users/", strings.NewReader(body))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createRec.Code, http.StatusCreated)
	}

	// Get
	getReq := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getRec.Code, http.StatusOK)
	}

	var user User
	json.NewDecoder(getRec.Body).Decode(&user)
	if user.Name != "Bob" {
		t.Errorf("name = %q, want Bob", user.Name)
	}
}
```

**2. Test the client with `httptest.NewServer`**:

```go
func TestUserClient_GetUser(t *testing.T) {
	handler := NewUserHandler()

	// Pre-populate the handler
	body := `{"id":"1","name":"Charlie","email":"charlie@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Start a test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create a client pointing to the test server
	client := &UserClient{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	user, err := client.GetUser("1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Name != "Charlie" {
		t.Errorf("name = %q, want Charlie", user.Name)
	}
}

func TestUserClient_NotFound(t *testing.T) {
	handler := NewUserHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	client := &UserClient{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	_, err := client.GetUser("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}
```

### Verification

```bash
go test -v
```

All tests should pass without any real HTTP server or network calls.

## Common Mistakes

1. **Forgetting `defer server.Close()`**: Always close the test server to free resources.

2. **Not setting Content-Type on requests**: Handlers that read JSON expect the proper content type header.

3. **Reusing `httptest.ResponseRecorder`**: Create a new recorder for each request. Reusing one will concatenate response bodies.

4. **Hardcoding ports**: Never use real ports. `httptest.NewServer` picks a random available port automatically.

## Verify What You Learned

1. What is the difference between `httptest.NewRecorder` and `httptest.NewServer`?
2. When would you use a recorder vs. a test server?
3. How do you create a request for handler testing?
4. What does `server.Client()` return?

## What's Next

The next exercise covers **testing readers with `iotest`** -- using the standard library to test code that reads from `io.Reader`.

## Summary

- `httptest.NewRecorder()` captures handler output without a network
- `httptest.NewServer(handler)` starts a local server for client testing
- `httptest.NewRequest()` creates requests for handler tests
- Always `defer server.Close()` on test servers
- Use `server.URL` and `server.Client()` for client tests

## Reference

- [net/http/httptest](https://pkg.go.dev/net/http/httptest)
- [httptest.NewRecorder](https://pkg.go.dev/net/http/httptest#NewRecorder)
- [httptest.NewServer](https://pkg.go.dev/net/http/httptest#NewServer)
