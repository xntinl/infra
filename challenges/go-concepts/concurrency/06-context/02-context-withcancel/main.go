package main

// Exercise: Context WithCancel
// Instructions: see 02-context-withcancel.md

import (
	"context"
	"fmt"
	"time"
)

// Step 1: Implement basicCancel.
// Create a cancellable context with context.WithCancel.
// Launch a goroutine that loops, checking ctx.Done() with select.
// After ~350ms, call cancel() from main and observe the goroutine stopping.
func basicCancel() {
	fmt.Println("=== Basic WithCancel ===")
	// TODO: create cancellable context: ctx, cancel := context.WithCancel(context.Background())
	// TODO: defer cancel()
	// TODO: launch goroutine that loops with select on ctx.Done()
	//       - on ctx.Done(): print stop reason (ctx.Err()) and return
	//       - on default: print iteration number, sleep 100ms
	// TODO: sleep 350ms to let goroutine work
	// TODO: call cancel()
	// TODO: sleep 50ms to let goroutine print its exit message
}

// Step 2: Implement cancellationPropagation.
// Create a parent context with WithCancel.
// Derive two child contexts from the parent.
// Launch a goroutine per child that blocks on <-ctx.Done().
// Cancel the parent and observe both children stopping.
func cancellationPropagation() {
	fmt.Println("=== Cancellation Propagation ===")
	// TODO: create parent: parent, cancelParent := context.WithCancel(context.Background())
	// TODO: create child1 and child2 from parent
	// TODO: defer cancelChild1() and cancelChild2()
	// TODO: launch goroutine per child that waits on <-ctx.Done() and prints
	// TODO: cancel parent
	// TODO: sleep briefly to let goroutines print
}

// Step 3: Implement cancelOnlyChild.
// Create a parent and two children.
// Cancel only child1 and verify parent and child2 are unaffected.
func cancelOnlyChild() {
	fmt.Println("=== Cancel Only Child ===")
	// TODO: create parent with WithCancel
	// TODO: defer cancelParent()
	// TODO: create child1, child2 from parent
	// TODO: defer cancelChild2()
	// TODO: cancel child1 only
	// TODO: print Err() for parent, child1, child2
}

// Verify: Implement verifyKnowledge.
// Create three levels: grandparent -> parent -> child.
// Launch a goroutine on each that prints when cancelled.
// Cancel the middle level (parent) and verify:
//   - grandparent is NOT cancelled
//   - parent IS cancelled
//   - child IS cancelled
func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")
	_ = context.Background // hint: start from Background
	// TODO: implement three-level cancellation test
}

func main() {
	fmt.Println("Exercise: Context WithCancel\n")

	basicCancel()
	cancellationPropagation()
	cancelOnlyChild()
	verifyKnowledge()

	// Final pause for any remaining goroutine output
	time.Sleep(100 * time.Millisecond)
}
