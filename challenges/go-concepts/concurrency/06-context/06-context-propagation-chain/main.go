package main

// Exercise: Context Propagation Chain
// Instructions: see 06-context-propagation-chain.md

import (
	"context"
	"fmt"
	"time"
)

// Step 1: Implement the three-layer chain.
// Each function: check ctx.Done(), simulate work, call next layer.

// handler is the entry point for a request.
// It simulates 50ms of work, then calls service.
func handler(ctx context.Context, userID string) (string, error) {
	_ = ctx    // TODO: check ctx.Done() with select
	_ = userID // TODO: pass to service layer
	// TODO: return result from service
	return "", nil
}

// service contains business logic.
// It simulates 50ms of work, then calls repository.
func service(ctx context.Context, userID string) (string, error) {
	_ = ctx    // TODO: check ctx.Done() with select
	_ = userID // TODO: pass to repository layer
	// TODO: return formatted result from repository
	return "", nil
}

// repository handles data access.
// It simulates 100ms of database work.
func repository(ctx context.Context, userID string) (string, error) {
	_ = ctx    // TODO: check ctx.Done() with select
	_ = userID // TODO: return data
	return "", nil
}

// successfulRequest calls the chain with a generous timeout.
// All layers should complete successfully.
func successfulRequest() {
	fmt.Println("=== Successful Request ===")
	// TODO: create context with 1s timeout
	// TODO: call handler("user-42")
	// TODO: print result or error
}

// Step 2: Implement timeoutRequest.
// Use a 120ms timeout so the repository layer times out.
func timeoutRequest() {
	fmt.Println("=== Timeout Cancels All Layers ===")
	// TODO: create context with 120ms timeout
	// TODO: call handler("user-42")
	// TODO: print result or error
}

// Step 3: Implement manualCancelRequest.
// Launch a goroutine that cancels the context after 80ms.
func manualCancelRequest() {
	fmt.Println("=== Manual Cancel from Top ===")
	// TODO: create cancellable context
	// TODO: launch goroutine that sleeps 80ms then calls cancel
	// TODO: call handler("user-42")
	// TODO: print result or error
}

// Step 4: Implement requestWithValues.
// Attach a request ID to the context and access it in each layer.

// TODO: define requestIDKey struct{}

// logWithContext prints a message annotated with the request ID from context.
func logWithContext(ctx context.Context, layer, message string) {
	_ = ctx     // TODO: extract requestIDKey from context
	_ = layer   // TODO: include in log output
	_ = message // TODO: include in log output
}

func requestWithValues() {
	fmt.Println("=== Context Values Through Chain ===")
	// TODO: create context with request ID value
	// TODO: add timeout
	// TODO: call logWithContext from each "layer"
}

// Verify: Implement a 4-layer chain: gateway -> auth -> compute -> store.
// Each layer takes 50ms. Attach a request ID.
// Test with 300ms timeout (success) and 150ms timeout (partial cancellation).
func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")
	// TODO: implement 4-layer chain with request ID propagation
}

func main() {
	fmt.Println("Exercise: Context Propagation Chain\n")

	successfulRequest()
	timeoutRequest()
	manualCancelRequest()
	requestWithValues()
	verifyKnowledge()

	// Final pause for goroutine output
	time.Sleep(100 * time.Millisecond)
}
