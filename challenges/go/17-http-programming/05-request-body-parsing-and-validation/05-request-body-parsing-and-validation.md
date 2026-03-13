# 5. Request Body Parsing and Validation

<!--
difficulty: intermediate
concepts: [json-decode, request-body, input-validation, content-type, http-error]
tools: [go, curl]
estimated_time: 25m
bloom_level: apply
prerequisites: [http-server, json-encoding, structs]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [03 - ServeMux Routing and Patterns](../03-servemux-routing-and-patterns/03-servemux-routing-and-patterns.md)
- Familiarity with JSON encoding/decoding

## Learning Objectives

After completing this exercise, you will be able to:

- **Parse** JSON request bodies using `json.NewDecoder`
- **Validate** input fields and return meaningful error responses
- **Limit** request body size to prevent abuse

## Why Request Body Parsing and Validation

Every web API that accepts data must parse and validate it. Skipping validation leads to crashes, corrupt data, or security vulnerabilities. Go does not have a built-in validation framework, but the pattern of decode-then-validate is straightforward and idiomatic.

## Step 1 -- Decode a JSON Body

```bash
mkdir -p ~/go-exercises/body-parsing
cd ~/go-exercises/body-parsing
go mod init body-parsing
```

Create `main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "user created",
		"user":    req,
	})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /users", createUser)

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

### Intermediate Verification

```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice","email":"alice@example.com","age":30}'
```

Expected: 201 with the user echoed back.

```bash
curl -X POST http://localhost:8080/users -d 'not json'
```

Expected: 400 with `invalid JSON:` error.

## Step 2 -- Add Field Validation

```go
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func validateCreateUser(req CreateUserRequest) []ValidationError {
	var errors []ValidationError

	if req.Name == "" {
		errors = append(errors, ValidationError{Field: "name", Message: "name is required"})
	}
	if req.Email == "" {
		errors = append(errors, ValidationError{Field: "email", Message: "email is required"})
	}
	if req.Age < 0 || req.Age > 150 {
		errors = append(errors, ValidationError{Field: "age", Message: "age must be between 0 and 150"})
	}

	return errors
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if errs := validateCreateUser(req); len(errs) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{"errors": errs})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "user created",
		"user":    req,
	})
}
```

### Intermediate Verification

```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name":"","email":"","age":-1}'
```

Expected: 422 with validation errors for all three fields.

## Step 3 -- Limit Request Body Size

Prevent oversized payloads from consuming memory:

```go
func createUser(w http.ResponseWriter, r *http.Request) {
	// Limit body to 1MB
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if errs := validateCreateUser(req); len(errs) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{"errors": errs})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "user created",
		"user":    req,
	})
}
```

### Intermediate Verification

Test with a valid small body (works), then confirm large bodies are rejected.

## Step 4 -- Reject Unknown Fields

By default, `json.Decoder` silently ignores unknown fields. Enable strict decoding:

```go
func createUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req CreateUserRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if errs := validateCreateUser(req); len(errs) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{"errors": errs})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"message": "user created", "user": req})
}
```

### Intermediate Verification

```bash
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice","email":"alice@example.com","age":30,"extra":"field"}'
```

Expected: 400 with error about unknown field "extra".

## Common Mistakes

### Using json.Unmarshal Instead of json.NewDecoder

**Wrong:**

```go
body, _ := io.ReadAll(r.Body)
json.Unmarshal(body, &req)
```

**What happens:** Reads the entire body into memory before parsing. For large bodies, this is wasteful.

**Fix:** Use `json.NewDecoder(r.Body).Decode(&req)` to stream-parse.

### Not Checking Content-Type

Accepting any content type can lead to confusing errors. Check `r.Header.Get("Content-Type")` if strict parsing is needed.

### Returning 400 for Validation Errors

**Fix:** Use 422 Unprocessable Entity for valid JSON with invalid field values. Reserve 400 for malformed JSON.

## Verify What You Learned

Test all cases:

```bash
curl -X POST http://localhost:8080/users -H "Content-Type: application/json" -d '{"name":"Bob","email":"bob@test.com","age":25}'
curl -X POST http://localhost:8080/users -H "Content-Type: application/json" -d '{"name":"","email":"","age":-5}'
curl -X POST http://localhost:8080/users -d 'not json'
```

Confirm: valid input returns 201, invalid fields return 422, bad JSON returns 400.

## What's Next

Continue to [06 - HTTP Client Timeouts](../06-http-client-timeouts/06-http-client-timeouts.md) to learn how to configure timeouts for HTTP clients.

## Summary

- Use `json.NewDecoder(r.Body).Decode(&req)` to stream-parse JSON bodies
- Validate fields after decoding and return structured error responses
- Use `http.MaxBytesReader` to limit request body size
- Use `dec.DisallowUnknownFields()` for strict JSON parsing
- Return 400 for malformed JSON and 422 for invalid field values

## Reference

- [json.Decoder](https://pkg.go.dev/encoding/json#Decoder)
- [http.MaxBytesReader](https://pkg.go.dev/net/http#MaxBytesReader)
- [HTTP Status Codes](https://developer.mozilla.org/en-US/docs/Web/HTTP/Status)
