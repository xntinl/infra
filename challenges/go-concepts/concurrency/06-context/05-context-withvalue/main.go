package main

// Exercise: Context WithValue
// Instructions: see 05-context-withvalue.md

import (
	"context"
	"fmt"
)

// Step 1: Define a typed key and implement basicWithValue.
// Use a custom type (not a raw string) as the context key.

// TODO: define a contextKey type and a requestIDKey constant
// Example: type contextKey string
//          const requestIDKey contextKey = "requestID"

// processRequest reads the request ID from the context.
func processRequest(ctx context.Context) {
	_ = ctx // TODO: extract requestIDKey from context using type assertion
	// TODO: print the request ID, or "no request ID found" if missing
}

func basicWithValue() {
	fmt.Println("=== Basic WithValue ===")
	// TODO: create Background context
	// TODO: add requestIDKey -> "req-abc-123" using context.WithValue
	// TODO: call processRequest with the enriched context
}

// Step 2: Implement typeSafeKeys.
// Define unexported struct types as keys to prevent collisions.

// TODO: define type userKey struct{}
// TODO: define type traceKey struct{}

func typeSafeKeys() {
	fmt.Println("=== Type-Safe Keys ===")
	// TODO: create context with userKey{} -> "alice"
	// TODO: add traceKey{} -> "trace-xyz-789"
	// TODO: retrieve and print each value
	// TODO: show that different key types never collide
}

// Step 3: Implement stringKeyCollision.
// Demonstrate the danger of using plain strings as context keys.
func stringKeyCollision() {
	fmt.Println("=== String Key Collision Problem ===")
	// TODO: store "userID" -> "user-from-package-A"
	// TODO: store "userID" -> "user-from-package-B" (overwrites!)
	// TODO: read "userID" -- Package A's value is lost
}

// Step 4: Implement helper functions pattern.
// Define an unexported key type and exported With/From functions.

// TODO: define type authTokenKey struct{}
// TODO: func withAuthToken(ctx context.Context, token string) context.Context
// TODO: func authTokenFrom(ctx context.Context) (string, bool)

// handleRequest uses the auth token from context.
func handleRequest(ctx context.Context) {
	_ = ctx // TODO: extract auth token using authTokenFrom
	// TODO: print token prefix if found, or rejection message if not
}

func helperFunctionsPattern() {
	fmt.Println("=== Helper Functions Pattern ===")
	// TODO: create context with auth token using withAuthToken
	// TODO: call handleRequest
}

// Verify: Implement correlation ID helpers and a 3-function chain.
// 1. Define correlationIDKey struct{} and helpers
// 2. entry() sets the correlation ID
// 3. middleware(ctx) reads and logs it, forwards ctx
// 4. handler(ctx) reads and uses it
func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")
	// TODO: define correlationIDKey, withCorrelationID, correlationIDFrom
	// TODO: implement entry -> middleware -> handler chain
}

func main() {
	fmt.Println("Exercise: Context WithValue\n")

	basicWithValue()
	typeSafeKeys()
	stringKeyCollision()
	helperFunctionsPattern()
	verifyKnowledge()
}
