# 11. Structured Error Types

<!--
difficulty: advanced
concepts: [structured-errors, json-serialization, error-fields, error-codes, api-errors]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [custom-error-types, json-encoding, errors-as]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [10 - Error Handling Middleware](../10-error-handling-middleware/10-error-handling-middleware.md)
- Familiarity with JSON encoding and custom error types

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** error types with structured fields for machine-readable error responses
- **Serialize** errors to JSON for API responses
- **Implement** error codes alongside human-readable messages

## Why Structured Error Types

Simple error messages like `"user not found"` work for logs, but API consumers need machine-readable error responses. They need error codes to branch on, field-level validation details, and correlation IDs for support tickets.

Structured error types carry this information as fields on the error struct. When serialized to JSON, they produce responses like:

```json
{
  "code": "VALIDATION_FAILED",
  "message": "request validation failed",
  "details": [
    {"field": "email", "message": "invalid format"},
    {"field": "age", "message": "must be positive"}
  ],
  "request_id": "req-abc-123"
}
```

## The Problem

Build a structured error system for a REST API that produces machine-readable JSON error responses.

### Requirements

1. Define an `APIError` struct with fields: `Code string`, `Message string`, `HTTPStatus int`, `Details []FieldError`, `RequestID string`.
2. Define a `FieldError` struct with `Field string` and `Message string`.
3. Implement `Error() string` and `Unwrap() error` on `APIError`.
4. Add a `json.Marshaler` implementation or use JSON struct tags to control serialization (exclude `HTTPStatus` from JSON output, since it goes in the HTTP status code).
5. Create constructor functions: `NewValidationError(requestID string, details []FieldError) *APIError`, `NewNotFoundError(resource, id, requestID string) *APIError`, `NewInternalError(requestID string, cause error) *APIError`.
6. Write a test program that creates each error type, serializes to JSON, and prints the result.

### Hints

<details>
<summary>Hint 1: Struct with JSON tags</summary>

```go
type APIError struct {
    Code       string       `json:"code"`
    Message    string       `json:"message"`
    Details    []FieldError `json:"details,omitempty"`
    RequestID  string       `json:"request_id,omitempty"`
    HTTPStatus int          `json:"-"`
    Cause      error        `json:"-"`
}
```

The `json:"-"` tag excludes fields from serialization.
</details>

<details>
<summary>Hint 2: Constructor for validation error</summary>

```go
func NewValidationError(requestID string, details []FieldError) *APIError {
    return &APIError{
        Code:       "VALIDATION_FAILED",
        Message:    "request validation failed",
        HTTPStatus: 400,
        Details:    details,
        RequestID:  requestID,
    }
}
```
</details>

<details>
<summary>Hint 3: Error method</summary>

```go
func (e *APIError) Error() string {
    if e.Cause != nil {
        return fmt.Sprintf("%s: %s: %s", e.Code, e.Message, e.Cause)
    }
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *APIError) Unwrap() error {
    return e.Cause
}
```
</details>

<details>
<summary>Hint 4: Serializing to JSON</summary>

```go
data, err := json.MarshalIndent(apiErr, "", "  ")
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(data))
```
</details>

## Verification

Your program should produce output similar to:

```
--- Validation Error ---
Error string: VALIDATION_FAILED: request validation failed
HTTP Status: 400
JSON:
{
  "code": "VALIDATION_FAILED",
  "message": "request validation failed",
  "details": [
    {"field": "email", "message": "invalid email format"},
    {"field": "age", "message": "must be between 0 and 150"}
  ],
  "request_id": "req-001"
}

--- Not Found Error ---
Error string: NOT_FOUND: user "42" not found
HTTP Status: 404
JSON:
{
  "code": "NOT_FOUND",
  "message": "user \"42\" not found",
  "request_id": "req-002"
}

--- Internal Error ---
Error string: INTERNAL_ERROR: an unexpected error occurred: connection refused
HTTP Status: 500
JSON:
{
  "code": "INTERNAL_ERROR",
  "message": "an unexpected error occurred",
  "request_id": "req-003"
}
```

Note that the internal error's `Cause` is not serialized to JSON (security: do not leak internal details to clients).

```bash
go run main.go
```

## What's Next

Continue to [12 - Retry Patterns with Backoff](../12-retry-patterns-with-backoff/12-retry-patterns-with-backoff.md) to learn how to implement retry logic with exponential backoff.

## Summary

- Structured error types carry machine-readable fields beyond a simple message
- Use JSON struct tags to control which fields are serialized
- Exclude internal details (`Cause`, `HTTPStatus`) from JSON responses
- Error codes (`VALIDATION_FAILED`, `NOT_FOUND`) let API consumers branch without parsing messages
- Field-level validation details help users fix specific inputs
- Constructor functions enforce consistent error creation

## Reference

- [encoding/json package](https://pkg.go.dev/encoding/json)
- [RFC 7807: Problem Details for HTTP APIs](https://www.rfc-editor.org/rfc/rfc7807)
- [Google API Design Guide: Errors](https://cloud.google.com/apis/design/errors)
