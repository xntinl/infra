package main

// Expected output:
//
// Context Background and TODO
//
// === context.Background() ===
// Type:     *context.emptyCtx
// String:   context.Background
// Err:      <nil>
// Done:     <nil>
// Deadline: none (no deadline set)
// Value("key"): <nil>
//
// === context.TODO() ===
// Type:     *context.emptyCtx
// String:   context.TODO
// Err:      <nil>
// Done:     <nil>
// Deadline: none (no deadline set)
// Value("key"): <nil>
//
// === Background vs TODO: Identical Behavior ===
// Background == TODO (nil Err)?       true
// Background == TODO (nil Done)?      true
// Background == TODO (no deadline)?   true
// Only difference -> String: "context.Background" vs "context.TODO"
//
// === Passing Context (ctx-first convention) ===
// Hello, Background User! (via context.Background)
// Hello, TODO User! (via context.TODO)
// greet: context already cancelled, skipping
//
// === Context Tree Visualization ===
// root (context.Background):     Err=<nil>  Done=<nil>
// child (context.Background):    Err=<nil>  Done=chan
// grandchild (context.Background): Err=<nil>  Done=chan
// After cancelling child:
// root:       Err=<nil>
// child:      Err=context canceled
// grandchild: Err=context canceled
//
// === Verify Knowledge ===
// --- describe(context.Background) ---
//   Has deadline: false
//   Done is nil:  true
//   String:       context.Background
// --- describe(context.TODO) ---
//   Has deadline: false
//   Done is nil:  true
//   String:       context.TODO

import (
	"context"
	"fmt"
)

// ---------------------------------------------------------------------------
// Example 1: Inspect context.Background()
// ---------------------------------------------------------------------------
// context.Background() returns the root context used in main(), init(), tests,
// and as the top-level parent for incoming requests. It is never cancelled,
// has no deadline, and carries no values.
func exploreBackground() {
	fmt.Println("=== context.Background() ===")

	ctx := context.Background()

	// %T prints the underlying concrete type -- useful to see it is an
	// unexported *context.emptyCtx, meaning the implementation is hidden.
	fmt.Printf("Type:     %T\n", ctx)

	// The Stringer implementation returns a human-readable label.
	fmt.Printf("String:   %s\n", ctx)

	// Err() is nil because a root context is never cancelled.
	fmt.Printf("Err:      %v\n", ctx.Err())

	// Done() returns a nil channel -- a receive on nil blocks forever,
	// which is correct because Background is never cancelled.
	fmt.Printf("Done:     %v\n", ctx.Done())

	// Deadline() returns (time.Time{}, false) -- no deadline set.
	deadline, ok := ctx.Deadline()
	if ok {
		fmt.Printf("Deadline: %v\n", deadline)
	} else {
		fmt.Println("Deadline: none (no deadline set)")
	}

	// Value() returns nil for any key -- no values attached.
	fmt.Printf("Value(\"key\"): %v\n", ctx.Value("key"))
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 2: Inspect context.TODO()
// ---------------------------------------------------------------------------
// context.TODO() is structurally identical to Background(). The only
// difference is the string representation, which signals "I have not
// decided which context to propagate here yet." It is a placeholder
// that should be replaced with a proper context once the design is clear.
func exploreTODO() {
	fmt.Println("=== context.TODO() ===")

	ctx := context.TODO()

	fmt.Printf("Type:     %T\n", ctx)
	fmt.Printf("String:   %s\n", ctx) // prints "context.TODO" instead of "context.Background"
	fmt.Printf("Err:      %v\n", ctx.Err())
	fmt.Printf("Done:     %v\n", ctx.Done())

	deadline, ok := ctx.Deadline()
	if ok {
		fmt.Printf("Deadline: %v\n", deadline)
	} else {
		fmt.Println("Deadline: none (no deadline set)")
	}

	fmt.Printf("Value(\"key\"): %v\n", ctx.Value("key"))
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 3: Prove Background and TODO are behaviorally identical
// ---------------------------------------------------------------------------
// This reinforces that the choice between them is purely about documenting
// intent for human readers and static analysis tools.
func compareBothRoots() {
	fmt.Println("=== Background vs TODO: Identical Behavior ===")

	bg := context.Background()
	todo := context.TODO()

	// Both have nil Err, nil Done channel, no deadline.
	fmt.Printf("Background == TODO (nil Err)?       %v\n", bg.Err() == todo.Err())
	fmt.Printf("Background == TODO (nil Done)?      %v\n", bg.Done() == todo.Done())

	_, bgOk := bg.Deadline()
	_, todoOk := todo.Deadline()
	fmt.Printf("Background == TODO (no deadline)?   %v\n", bgOk == todoOk)

	// The ONLY difference: String() output.
	fmt.Printf("Only difference -> String: %q vs %q\n", bg, todo)
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 4: Passing context as the first parameter
// ---------------------------------------------------------------------------
// Go convention: context.Context is ALWAYS the first parameter, named ctx.
// This is enforced by linters (contextcheck, revive) and expected by the
// entire standard library (database/sql, net/http, gRPC, etc.).

// greet demonstrates the ctx-first convention. It checks ctx.Err() before
// doing any work -- if the context is already cancelled, we skip immediately.
func greet(ctx context.Context, name string) {
	if ctx.Err() != nil {
		fmt.Printf("greet: context already cancelled, skipping\n")
		return
	}
	fmt.Printf("Hello, %s! (via %s)\n", name, ctx)
}

func demonstratePassingContext() {
	fmt.Println("=== Passing Context (ctx-first convention) ===")

	bgCtx := context.Background()
	greet(bgCtx, "Background User")

	todoCtx := context.TODO()
	greet(todoCtx, "TODO User")

	// Show what happens with an already-cancelled context.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	greet(cancelledCtx, "Cancelled User")

	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 5: Context tree structure
// ---------------------------------------------------------------------------
// Every context in Go forms a tree. Root contexts (Background/TODO) sit at
// the top. Derived contexts (WithCancel, WithTimeout, WithValue) are children.
// Cancellation flows DOWN the tree: cancelling a parent cancels all its
// descendants. It never flows UP -- parents are unaffected.
func demonstrateContextTree() {
	fmt.Println("=== Context Tree Visualization ===")

	root := context.Background()
	child, cancelChild := context.WithCancel(root)
	grandchild, cancelGrandchild := context.WithCancel(child)
	defer cancelGrandchild()

	// Before cancellation: all contexts are alive.
	fmt.Printf("root (context.Background):     Err=%v  Done=%v\n", root.Err(), root.Done())
	fmt.Printf("child (context.Background):    Err=%v  Done=%v\n", child.Err(), child.Done())
	fmt.Printf("grandchild (context.Background): Err=%v  Done=%v\n", grandchild.Err(), grandchild.Done())

	// Cancel the child -- grandchild is also cancelled, root is not.
	cancelChild()

	fmt.Println("After cancelling child:")
	fmt.Printf("root:       Err=%v\n", root.Err())
	fmt.Printf("child:      Err=%v\n", child.Err())
	fmt.Printf("grandchild: Err=%v\n", grandchild.Err())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Verify: describeContext inspects any context's properties
// ---------------------------------------------------------------------------
// This function accepts any context.Context and prints its observable
// properties: deadline presence, Done channel state, and string label.
func describeContext(ctx context.Context) {
	_, hasDeadline := ctx.Deadline()
	doneIsNil := ctx.Done() == nil

	fmt.Printf("  Has deadline: %v\n", hasDeadline)
	fmt.Printf("  Done is nil:  %v\n", doneIsNil)
	fmt.Printf("  String:       %s\n", ctx)
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")

	fmt.Println("--- describe(context.Background) ---")
	describeContext(context.Background())

	fmt.Println("--- describe(context.TODO) ---")
	describeContext(context.TODO())
}

func main() {
	fmt.Println("Context Background and TODO")
	fmt.Println()

	exploreBackground()
	exploreTODO()
	compareBothRoots()
	demonstratePassingContext()
	demonstrateContextTree()
	verifyKnowledge()
}
