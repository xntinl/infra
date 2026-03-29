package main

// Expected output (timing approximate):
//
// Context Propagation Chain
//
// === Successful Request (1s budget, ~200ms total work) ===
//   [handler]    received request for user-42
//   [service]    looking up user user-42
//   [repository] querying database for user-42
//   [handler]    returning result: profile(data-for-user-42)
//   Result: profile(data-for-user-42)
//
// === Timeout Cancels Deep Layer ===
//   [handler]    received request for user-42
//   [service]    looking up user user-42
//   [repository] querying database for user-42
//   [repository] cancelled: context deadline exceeded
//   Error: handler: service: repository: context deadline exceeded
//
// === Manual Cancel Mid-Chain ===
//   [handler]    received request for user-42
//   [caller] cancelling request
//   [handler]    cancelled: context canceled
//   Error: handler: context canceled
//
// === Context Values Flow Through All Layers ===
//   [handler]    req=req-7f3a: processing request
//   [service]    req=req-7f3a: applying business logic
//   [repository] req=req-7f3a: executing query
//   [handler]    req=req-7f3a: completed successfully
//
// === Verify Knowledge: 4-Layer Chain ===
//   --- 300ms budget (enough for 4x50ms = 200ms) ---
//   [gateway]  req=verify-001: processing
//   [auth]     req=verify-001: processing
//   [compute]  req=verify-001: processing
//   [store]    req=verify-001: processing
//   Success: gateway -> auth -> compute -> store -> done
//   --- 130ms budget (only enough for ~2 layers) ---
//   [gateway]  req=verify-002: processing
//   [auth]     req=verify-002: processing
//   [compute]  req=verify-002: processing
//   Failed at compute: context deadline exceeded

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Request ID context helpers (production pattern from Exercise 05)
// ---------------------------------------------------------------------------
type requestIDKey struct{}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// ---------------------------------------------------------------------------
// Three-layer architecture: handler -> service -> repository
// ---------------------------------------------------------------------------
// Each layer follows the same pattern:
// 1. Accept ctx as the first parameter
// 2. Check ctx.Done() before doing work (fail fast)
// 3. Simulate work with select + time.After
// 4. Call the next layer, passing ctx through
// 5. Wrap errors with the layer name for clear error chains

func handler(ctx context.Context, userID string) (string, error) {
	fmt.Printf("  [handler]    received request for %s\n", userID)

	// Check context before doing any work.
	select {
	case <-ctx.Done():
		fmt.Printf("  [handler]    cancelled: %v\n", ctx.Err())
		return "", fmt.Errorf("handler: %w", ctx.Err())
	case <-time.After(50 * time.Millisecond):
		// Handler processing (validation, routing, etc.)
	}

	result, err := service(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("handler: %w", err)
	}

	fmt.Printf("  [handler]    returning result: %s\n", result)
	return result, nil
}

func service(ctx context.Context, userID string) (string, error) {
	fmt.Printf("  [service]    looking up user %s\n", userID)

	select {
	case <-ctx.Done():
		fmt.Printf("  [service]    cancelled: %v\n", ctx.Err())
		return "", fmt.Errorf("service: %w", ctx.Err())
	case <-time.After(50 * time.Millisecond):
		// Business logic processing.
	}

	data, err := repository(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("service: %w", err)
	}

	return fmt.Sprintf("profile(%s)", data), nil
}

func repository(ctx context.Context, userID string) (string, error) {
	fmt.Printf("  [repository] querying database for %s\n", userID)

	select {
	case <-ctx.Done():
		fmt.Printf("  [repository] cancelled: %v\n", ctx.Err())
		return "", fmt.Errorf("repository: %w", ctx.Err())
	case <-time.After(100 * time.Millisecond):
		// Database query simulation.
	}

	return fmt.Sprintf("data-for-%s", userID), nil
}

// ---------------------------------------------------------------------------
// Example 1: Successful request with generous timeout
// ---------------------------------------------------------------------------
// Total work: handler(50ms) + service(50ms) + repository(100ms) = 200ms.
// Budget: 1s. Everything completes.
func successfulRequest() {
	fmt.Println("=== Successful Request (1s budget, ~200ms total work) ===")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result, err := handler(ctx, "user-42")
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("  Result: %s\n", result)
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 2: Timeout fires at the deepest layer
// ---------------------------------------------------------------------------
// Budget: 120ms. handler(50ms) + service(50ms) = 100ms, leaving only 20ms
// for repository(100ms). The repository is the one that sees DeadlineExceeded.
func timeoutRequest() {
	fmt.Println("=== Timeout Cancels Deep Layer ===")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	result, err := handler(ctx, "user-42")
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("  Result: %s\n", result)
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 3: Manual cancel from the caller
// ---------------------------------------------------------------------------
// A goroutine cancels the context after 80ms, simulating a client disconnect.
// The cancellation hits during the service layer.
func manualCancelRequest() {
	fmt.Println("=== Manual Cancel Mid-Chain ===")

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(80 * time.Millisecond)
		fmt.Println("  [caller] cancelling request")
		cancel()
	}()

	result, err := handler(ctx, "user-42")
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("  Result: %s\n", result)
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 4: Context values flow through the entire chain
// ---------------------------------------------------------------------------
// A request ID attached at the entry point is visible in every layer without
// being passed as an explicit parameter. This is the intended use case for
// context values: metadata that crosses API boundaries.
func requestWithValues() {
	fmt.Println("=== Context Values Flow Through All Layers ===")

	ctx := withRequestID(context.Background(), "req-7f3a")
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	// Simulated chain where each layer logs with the request ID.
	layers := []string{"handler", "service", "repository"}
	for _, layer := range layers {
		reqID := requestIDFrom(ctx)
		action := "processing request"
		if layer == "service" {
			action = "applying business logic"
		} else if layer == "repository" {
			action = "executing query"
		}
		fmt.Printf("  [%-10s] req=%s: %s\n", layer, reqID, action)
	}
	fmt.Printf("  [%-10s] req=%s: completed successfully\n", "handler", requestIDFrom(ctx))
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Verify: 4-layer chain with request ID and variable budgets
// ---------------------------------------------------------------------------
func runChain(ctx context.Context, layers []string, workPerLayer time.Duration) (string, error) {
	result := ""
	for _, name := range layers {
		reqID := requestIDFrom(ctx)
		fmt.Printf("  [%-8s] req=%s: processing\n", name, reqID)

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("failed at %s: %w", name, ctx.Err())
		case <-time.After(workPerLayer):
			if result != "" {
				result += " -> "
			}
			result += name
		}
	}
	return result, nil
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge: 4-Layer Chain ===")

	layers := []string{"gateway", "auth", "compute", "store"}

	// Case 1: 300ms budget for 4x50ms = 200ms work -- success.
	fmt.Println("  --- 300ms budget (enough for 4x50ms = 200ms) ---")
	ctx1 := withRequestID(context.Background(), "verify-001")
	ctx1, cancel1 := context.WithTimeout(ctx1, 300*time.Millisecond)
	defer cancel1()

	result, err := runChain(ctx1, layers, 50*time.Millisecond)
	if err != nil {
		fmt.Printf("  %v\n", err)
	} else {
		fmt.Printf("  Success: %s -> done\n", result)
	}

	// Case 2: 130ms budget for 4x50ms -- fails around layer 3.
	fmt.Println("  --- 130ms budget (only enough for ~2 layers) ---")
	ctx2 := withRequestID(context.Background(), "verify-002")
	ctx2, cancel2 := context.WithTimeout(ctx2, 130*time.Millisecond)
	defer cancel2()

	result, err = runChain(ctx2, layers, 50*time.Millisecond)
	if err != nil {
		fmt.Printf("  %v\n", err)
	} else {
		fmt.Printf("  Success: %s -> done\n", result)
	}
}

func main() {
	fmt.Println("Context Propagation Chain")
	fmt.Println()

	successfulRequest()
	timeoutRequest()
	manualCancelRequest()
	requestWithValues()
	verifyKnowledge()
}
