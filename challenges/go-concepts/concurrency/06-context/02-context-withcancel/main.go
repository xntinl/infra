package main

// Expected output (timing approximate):
//
// Context WithCancel
//
// === Basic WithCancel ===
//   goroutine: working... iteration 0
//   goroutine: working... iteration 1
//   goroutine: working... iteration 2
//   main: calling cancel()
//   goroutine: stopped (reason: context canceled)
//
// === Cancellation Propagates to Children ===
//   Cancelling parent context...
//   child1: stopped (reason: context canceled)
//   child2: stopped (reason: context canceled)
//
// === Cancel Only a Child ===
//   Cancelling child1 only...
//   parent.Err(): <nil>
//   child1.Err(): context canceled
//   child2.Err(): <nil>
//
// === Cancel Is Idempotent ===
//   First cancel: context canceled
//   Second cancel: context canceled  (no panic, same error)
//   Third cancel: context canceled  (still safe)
//
// === Three-Level Cancellation ===
//   Cancelling middle (parent) context...
//   parent: cancelled (context canceled)
//   child: cancelled (context canceled)
//   grandparent.Err(): <nil>  -- NOT cancelled
//   parent.Err():      context canceled
//   child.Err():       context canceled
//
// === Verify Knowledge ===
//   Before any cancellation:
//     root.Err():   <nil>
//     branch1.Err(): <nil>
//     branch2.Err(): <nil>
//     leaf.Err():   <nil>
//   After cancelling branch1:
//     root.Err():   <nil>
//     branch1.Err(): context canceled
//     branch2.Err(): <nil>
//     leaf.Err():   context canceled

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Example 1: Basic cancel and Done channel
// ---------------------------------------------------------------------------
// WithCancel returns a copy of parent with a new Done channel. When cancel()
// is called, the Done channel is closed, and ctx.Err() returns context.Canceled.
// The goroutine must cooperatively check ctx.Done() -- the context does NOT
// forcibly kill anything.
func basicCancel() {
	fmt.Println("=== Basic WithCancel ===")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // always defer cancel to release resources even if we call it explicitly later

	go func(ctx context.Context) {
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				// Done() channel closed -- time to stop.
				// ctx.Err() tells us WHY: Canceled vs DeadlineExceeded.
				fmt.Printf("  goroutine: stopped (reason: %v)\n", ctx.Err())
				return
			default:
				// No cancellation yet -- keep working.
				fmt.Printf("  goroutine: working... iteration %d\n", i)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}(ctx)

	// Let the goroutine run for ~3 iterations (350ms).
	time.Sleep(350 * time.Millisecond)

	fmt.Println("  main: calling cancel()")
	cancel()

	// Give the goroutine time to receive the signal and print its exit message.
	time.Sleep(50 * time.Millisecond)
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 2: Cancellation propagates from parent to all children
// ---------------------------------------------------------------------------
// When you cancel a parent context, every child derived from it is also
// cancelled. This is the tree structure of contexts in action -- cancellation
// flows downward through the entire subtree.
func cancellationPropagation() {
	fmt.Println("=== Cancellation Propagates to Children ===")

	parent, cancelParent := context.WithCancel(context.Background())
	child1, cancelChild1 := context.WithCancel(parent)
	child2, cancelChild2 := context.WithCancel(parent)
	defer cancelChild1() // safe to call even after parent cancel
	defer cancelChild2()

	// Each worker blocks on <-ctx.Done(), which unblocks when the channel closes.
	worker := func(name string, ctx context.Context) {
		<-ctx.Done()
		fmt.Printf("  %s: stopped (reason: %v)\n", name, ctx.Err())
	}

	go worker("child1", child1)
	go worker("child2", child2)

	fmt.Println("  Cancelling parent context...")
	cancelParent()

	// Both children receive the cancellation signal simultaneously.
	time.Sleep(50 * time.Millisecond)
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 3: Cancelling a child does NOT affect parent or siblings
// ---------------------------------------------------------------------------
// Cancellation flows DOWN only. The parent and sibling contexts remain active.
// This is important: a failing sub-operation should not tear down unrelated
// parts of the system.
func cancelOnlyChild() {
	fmt.Println("=== Cancel Only a Child ===")

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	child1, cancelChild1 := context.WithCancel(parent)
	child2, cancelChild2 := context.WithCancel(parent)
	defer cancelChild2()

	fmt.Println("  Cancelling child1 only...")
	cancelChild1()

	time.Sleep(10 * time.Millisecond)

	fmt.Printf("  parent.Err(): %v\n", parent.Err())
	fmt.Printf("  child1.Err(): %v\n", child1.Err())
	fmt.Printf("  child2.Err(): %v\n", child2.Err())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 4: Cancel is idempotent (safe to call multiple times)
// ---------------------------------------------------------------------------
// The Go docs explicitly state that calling cancel more than once is a no-op.
// This is important because defer cancel() and explicit cancel() calls may
// both execute. No panic, no error.
func cancelIsIdempotent() {
	fmt.Println("=== Cancel Is Idempotent ===")

	ctx, cancel := context.WithCancel(context.Background())

	cancel()
	fmt.Printf("  First cancel: %v\n", ctx.Err())

	cancel() // no panic
	fmt.Printf("  Second cancel: %v  (no panic, same error)\n", ctx.Err())

	cancel() // still no panic
	fmt.Printf("  Third cancel: %v  (still safe)\n", ctx.Err())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 5: Three-level cancellation tree
// ---------------------------------------------------------------------------
// Grandparent -> parent -> child. Cancelling the middle (parent) level
// cancels the child but leaves the grandparent untouched. This demonstrates
// that cancellation propagates to all descendants, not just direct children.
func threeLevelCancellation() {
	fmt.Println("=== Three-Level Cancellation ===")

	grandparent, cancelGrandparent := context.WithCancel(context.Background())
	defer cancelGrandparent()

	parent, cancelParent := context.WithCancel(grandparent)

	child, cancelChild := context.WithCancel(parent)
	defer cancelChild()

	// Workers that print when cancelled.
	go func() {
		<-parent.Done()
		fmt.Printf("  parent: cancelled (%v)\n", parent.Err())
	}()

	go func() {
		<-child.Done()
		fmt.Printf("  child: cancelled (%v)\n", child.Err())
	}()

	fmt.Println("  Cancelling middle (parent) context...")
	cancelParent()

	time.Sleep(50 * time.Millisecond)

	// Grandparent is alive; parent and child are cancelled.
	fmt.Printf("  grandparent.Err(): %v  -- NOT cancelled\n", grandparent.Err())
	fmt.Printf("  parent.Err():      %v\n", parent.Err())
	fmt.Printf("  child.Err():       %v\n", child.Err())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Verify: Tree with branching -- cancel one branch, other branch survives
// ---------------------------------------------------------------------------
func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")

	//         root
	//        /    \
	//   branch1  branch2
	//      |
	//     leaf

	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	branch1, cancelBranch1 := context.WithCancel(root)
	branch2, cancelBranch2 := context.WithCancel(root)
	defer cancelBranch2()

	leaf, cancelLeaf := context.WithCancel(branch1)
	defer cancelLeaf()

	fmt.Println("  Before any cancellation:")
	fmt.Printf("    root.Err():   %v\n", root.Err())
	fmt.Printf("    branch1.Err(): %v\n", branch1.Err())
	fmt.Printf("    branch2.Err(): %v\n", branch2.Err())
	fmt.Printf("    leaf.Err():   %v\n", leaf.Err())

	// Cancel branch1 -- leaf should also cancel, but root and branch2 survive.
	cancelBranch1()
	time.Sleep(10 * time.Millisecond)

	fmt.Println("  After cancelling branch1:")
	fmt.Printf("    root.Err():   %v\n", root.Err())
	fmt.Printf("    branch1.Err(): %v\n", branch1.Err())
	fmt.Printf("    branch2.Err(): %v\n", branch2.Err())
	fmt.Printf("    leaf.Err():   %v\n", leaf.Err())
}

func main() {
	fmt.Println("Context WithCancel")
	fmt.Println()

	basicCancel()
	cancellationPropagation()
	cancelOnlyChild()
	cancelIsIdempotent()
	threeLevelCancellation()
	verifyKnowledge()
}
