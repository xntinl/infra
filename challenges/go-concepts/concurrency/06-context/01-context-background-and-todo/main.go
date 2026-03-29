package main

// Exercise: Context Background and TODO
// Instructions: see 01-context-background-and-todo.md

import (
	"context"
	"fmt"
)

// Step 1: Implement exploreBackground.
// Create a context.Background() and print its type, string representation,
// Err, Done channel, Deadline, and a Value lookup.
func exploreBackground() {
	fmt.Println("=== context.Background() ===")
	// TODO: create background context with context.Background()
	// TODO: print Type using %T format verb
	// TODO: print String using %s format verb
	// TODO: print Err()
	// TODO: print Done() channel
	// TODO: check Deadline() -- print "none" if ok is false
	// TODO: print Value("key")
}

// Step 2: Implement exploreTODO.
// Create a context.TODO() and print the same properties as Step 1.
// Observe the difference (hint: only the string representation changes).
func exploreTODO() {
	fmt.Println("=== context.TODO() ===")
	// TODO: create TODO context with context.TODO()
	// TODO: print the same properties as exploreBackground
}

// Step 3: Implement greet and demonstratePassingContext.
// greet accepts a context.Context as its first parameter (Go convention)
// and a name string. It checks ctx.Err() before proceeding.

// greet prints a greeting using the provided name.
// It checks whether the context is already cancelled before proceeding.
func greet(ctx context.Context, name string) {
	_ = ctx  // TODO: check ctx.Err() and return early if non-nil
	_ = name // TODO: print greeting with context string representation
}

// demonstratePassingContext shows the convention of passing context
// as the first argument to functions.
func demonstratePassingContext() {
	fmt.Println("=== Passing Context ===")
	// TODO: create Background context, call greet with it
	// TODO: create TODO context, call greet with it
}

// Verify: Implement describeContext and verifyKnowledge.
// describeContext accepts a context.Context and prints:
//   - Whether it has a deadline
//   - Whether its Done channel is nil
//   - Its string representation
// verifyKnowledge calls describeContext with both Background and TODO contexts.
func describeContext(ctx context.Context) {
	_ = ctx // TODO: implement
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")
	// TODO: call describeContext with Background and TODO contexts
}

func main() {
	fmt.Println("Exercise: Context Background and TODO\n")

	exploreBackground()
	exploreTODO()
	demonstratePassingContext()
	verifyKnowledge()
}
