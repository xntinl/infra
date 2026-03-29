package main

// Exercise: Context WithDeadline
// Instructions: see 04-context-withdeadline.md

import (
	"context"
	"fmt"
	"time"
)

// Step 1: Implement basicDeadline.
// Create a context with a deadline 300ms from now.
// Simulate a 500ms operation -- the deadline should fire first.
func basicDeadline() {
	fmt.Println("=== Basic WithDeadline ===")
	// TODO: deadline := time.Now().Add(300 * time.Millisecond)
	// TODO: ctx, cancel := context.WithDeadline(context.Background(), deadline)
	// TODO: defer cancel()
	// TODO: print the deadline and current time (use Format("15:04:05.000"))
	// TODO: select between time.After(500ms) and ctx.Done()
}

// Step 2: Implement inspectDeadline.
// Show how to read back a deadline from a context using ctx.Deadline().
// Compare WithDeadline, WithTimeout, and Background contexts.
func inspectDeadline() {
	fmt.Println("=== Inspecting Deadline ===")
	// TODO: create a WithDeadline context, print its deadline and time remaining
	// TODO: check Background context -- it has no deadline (ok == false)
	// TODO: create a WithTimeout context, show it also has a deadline
}

// Step 3: Implement equivalenceDemo.
// Demonstrate that WithTimeout(parent, d) and WithDeadline(parent, now.Add(d))
// produce nearly identical deadlines.
func equivalenceDemo() {
	fmt.Println("=== WithTimeout == WithDeadline(now + d) ===")
	// TODO: capture time.Now()
	// TODO: create both WithTimeout(500ms) and WithDeadline(now.Add(500ms))
	// TODO: compare their deadlines -- difference should be < 1ms
}

// Step 4: Implement shorterDeadlineWins.
// Create a parent with a 200ms deadline.
// Create a child attempting a 5s deadline.
// Show that the child inherits the parent's shorter deadline.
func shorterDeadlineWins() {
	fmt.Println("=== Shorter Deadline Always Wins ===")
	// TODO: parent with 200ms deadline
	// TODO: child with 5s deadline (derived from parent)
	// TODO: print both deadlines -- they should match (parent's wins)
	// TODO: wait on child.Done(), print error
}

// Verify: Implement processStage and verifyKnowledge.
// processStage simulates a pipeline stage that takes stageDuration.
func processStage(ctx context.Context, name string, stageDuration time.Duration) error {
	_ = ctx           // TODO: check ctx.Done() with select
	_ = name          // TODO: print stage name and remaining time
	_ = stageDuration // TODO: use with time.After
	return nil
}

func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")
	// TODO: create context with 500ms deadline
	// TODO: pass through 3 stages (each 100ms) -- all should complete
	// TODO: create context with 250ms deadline
	// TODO: pass through 3 stages -- observe which stage gets cut off
}

func main() {
	fmt.Println("Exercise: Context WithDeadline\n")

	basicDeadline()
	inspectDeadline()
	equivalenceDemo()
	shorterDeadlineWins()
	verifyKnowledge()
}
