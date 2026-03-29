package main

// Expected output:
//
// Context WithValue
//
// === Basic WithValue ===
//   Processing request: req-abc-123
//   Missing key returns nil: <nil>
//
// === Type-Safe Keys Prevent Collisions ===
//   User:  alice
//   Trace: trace-xyz-789
//   Wrong key type returns zero value: ""
//   Each type is its own namespace -- no collision possible
//
// === String Key Collision Problem ===
//   Value for "userID": user-from-package-B
//   Package A's value was silently overwritten!
//   This is why you NEVER use plain strings as context keys.
//
// === Helper Functions Pattern ===
//   Authenticated: Bearer eyJhbG... (prefix: Bearer eyJhbGciO)
//   No token: request rejected -- missing auth token
//
// === Values Are Inherited Down the Tree ===
//   root: requestID=req-001
//   child also sees parent's value: requestID=req-001
//   child added its own: userID=alice
//   root does NOT see child's value: userID=
//
// === Verify Knowledge ===
//   [gateway]    corrID=corr-98765: received request
//   [middleware] corrID=corr-98765: validating auth
//   [handler]    corrID=corr-98765: processing business logic

import (
	"context"
	"fmt"
)

// ---------------------------------------------------------------------------
// Key type definitions
// ---------------------------------------------------------------------------
// Context keys MUST be custom types to avoid cross-package collisions.
// An unexported type ensures no external package can accidentally use the
// same key. The struct{} pattern uses zero bytes of memory.

// contextKey is a named string type for demonstration of typed keys.
type contextKey string

const requestIDKey contextKey = "requestID"

// userKey and traceKey are struct-based keys -- the idiomatic Go approach.
// Being unexported (lowercase), only this package can create values of these types.
type userKey struct{}
type traceKey struct{}

// authTokenKey is used by the helper functions pattern below.
type authTokenKey struct{}

// ---------------------------------------------------------------------------
// Example 1: Store and retrieve a value
// ---------------------------------------------------------------------------
// WithValue returns a NEW context carrying the key-value pair. The original
// context is unchanged (contexts are immutable). Value() walks up the parent
// chain until it finds a matching key or reaches the root (returning nil).
func basicWithValue() {
	fmt.Println("=== Basic WithValue ===")

	ctx := context.Background()
	// WithValue creates a new context -- we must capture the return value.
	ctx = context.WithValue(ctx, requestIDKey, "req-abc-123")

	processRequest(ctx)

	// Looking up a key that was never set returns nil.
	fmt.Printf("  Missing key returns nil: %v\n", ctx.Value(contextKey("nonexistent")))
	fmt.Println()
}

// processRequest reads the request ID from context using a type assertion.
// The comma-ok idiom distinguishes "key not found" from "key found with zero value."
func processRequest(ctx context.Context) {
	reqID, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		fmt.Println("  no request ID found in context")
		return
	}
	fmt.Printf("  Processing request: %s\n", reqID)
}

// ---------------------------------------------------------------------------
// Example 2: Type-safe keys prevent collisions
// ---------------------------------------------------------------------------
// Different struct types are NEVER equal, even if their structure is identical.
// This means userKey{} and traceKey{} will never collide, even though both
// are empty structs.
func typeSafeKeys() {
	fmt.Println("=== Type-Safe Keys Prevent Collisions ===")

	ctx := context.Background()
	ctx = context.WithValue(ctx, userKey{}, "alice")
	ctx = context.WithValue(ctx, traceKey{}, "trace-xyz-789")

	// Each key retrieves only its own value.
	user, _ := ctx.Value(userKey{}).(string)
	trace, _ := ctx.Value(traceKey{}).(string)
	fmt.Printf("  User:  %s\n", user)
	fmt.Printf("  Trace: %s\n", trace)

	// Using the wrong key type returns the zero value after type assertion.
	wrongType, _ := ctx.Value(contextKey("user")).(string)
	fmt.Printf("  Wrong key type returns zero value: %q\n", wrongType)
	fmt.Println("  Each type is its own namespace -- no collision possible")
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 3: Why plain string keys are dangerous
// ---------------------------------------------------------------------------
// Two independent packages both use "userID" as a string key. The second
// silently overwrites the first. This is impossible with typed keys because
// each package defines its own unexported type.
func stringKeyCollision() {
	fmt.Println("=== String Key Collision Problem ===")

	ctx := context.Background()

	// "Package A" stores a value.
	ctx = context.WithValue(ctx, "userID", "user-from-package-A")

	// "Package B" stores a value with the same string key.
	ctx = context.WithValue(ctx, "userID", "user-from-package-B")

	// "Package A" tries to read its value -- gets B's instead.
	value := ctx.Value("userID")
	fmt.Printf("  Value for \"userID\": %s\n", value)
	fmt.Println("  Package A's value was silently overwritten!")
	fmt.Println("  This is why you NEVER use plain strings as context keys.")
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 4: Helper functions pattern (production idiom)
// ---------------------------------------------------------------------------
// The key type is unexported. Two exported functions provide the public API:
// - withAuthToken(ctx, token) -> context.Context  (store)
// - authTokenFrom(ctx) -> (string, bool)          (retrieve)
// This is the standard pattern in Go libraries (e.g., metadata in gRPC).

func withAuthToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, authTokenKey{}, token)
}

func authTokenFrom(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(authTokenKey{}).(string)
	return token, ok
}

func handleRequest(ctx context.Context) {
	token, ok := authTokenFrom(ctx)
	if !ok {
		fmt.Println("  No token: request rejected -- missing auth token")
		return
	}
	// Only show a prefix in logs to avoid leaking the full token.
	prefix := token
	if len(prefix) > 15 {
		prefix = prefix[:15]
	}
	fmt.Printf("  Authenticated: %s... (prefix: %s)\n", token[:15], prefix)
}

func helperFunctionsPattern() {
	fmt.Println("=== Helper Functions Pattern ===")

	ctx := context.Background()
	ctx = withAuthToken(ctx, "Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature")

	handleRequest(ctx)

	// Call without token to see the rejection path.
	handleRequest(context.Background())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 5: Values are inherited down the context tree
// ---------------------------------------------------------------------------
// A child context sees all values from its ancestors. But a parent does NOT
// see values added by its children. Values flow downward, not upward.
func valueInheritance() {
	fmt.Println("=== Values Are Inherited Down the Tree ===")

	type reqIDKey struct{}
	type uidKey struct{}

	root := context.WithValue(context.Background(), reqIDKey{}, "req-001")
	child := context.WithValue(root, uidKey{}, "alice")

	// root's value is visible from root.
	rootReqID, _ := root.Value(reqIDKey{}).(string)
	fmt.Printf("  root: requestID=%s\n", rootReqID)

	// child sees root's value (inherited).
	childReqID, _ := child.Value(reqIDKey{}).(string)
	fmt.Printf("  child also sees parent's value: requestID=%s\n", childReqID)

	// child's own value.
	childUID, _ := child.Value(uidKey{}).(string)
	fmt.Printf("  child added its own: userID=%s\n", childUID)

	// root does NOT see child's value.
	rootUID, _ := root.Value(uidKey{}).(string)
	fmt.Printf("  root does NOT see child's value: userID=%s\n", rootUID)
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Verify: Correlation ID through a 3-function chain
// ---------------------------------------------------------------------------
type correlationIDKey struct{}

func withCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

func correlationIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

func logWithCorrelation(ctx context.Context, layer, message string) {
	corrID := correlationIDFrom(ctx)
	fmt.Printf("  [%-10s] corrID=%s: %s\n", layer, corrID, message)
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")

	// Entry point sets the correlation ID.
	ctx := withCorrelationID(context.Background(), "corr-98765")

	// Each layer reads the correlation ID for structured logging.
	logWithCorrelation(ctx, "gateway", "received request")
	logWithCorrelation(ctx, "middleware", "validating auth")
	logWithCorrelation(ctx, "handler", "processing business logic")
}

func main() {
	fmt.Println("Context WithValue")
	fmt.Println()

	basicWithValue()
	typeSafeKeys()
	stringKeyCollision()
	helperFunctionsPattern()
	valueInheritance()
	verifyKnowledge()
}
