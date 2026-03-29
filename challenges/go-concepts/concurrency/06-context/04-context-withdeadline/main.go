package main

// Expected output (times will vary):
//
// Context WithDeadline
//
// === Basic WithDeadline ===
//   Deadline set to: HH:MM:SS.mmm
//   Current time:    HH:MM:SS.mmm
//   Deadline hit at: HH:MM:SS.mmm
//   Error: context deadline exceeded
//
// === Inspecting Deadline ===
//   WithDeadline context:  deadline=HH:MM:SS.mmm, remaining=~2s
//   Background context:    no deadline
//   WithTimeout(1s) also has deadline: HH:MM:SS.mmm
//
// === WithTimeout Is WithDeadline in Disguise ===
//   WithTimeout deadline:  HH:MM:SS.mmmmmm
//   WithDeadline deadline: HH:MM:SS.mmmmmm
//   Difference: <1ms (they are equivalent)
//
// === Shorter Deadline Always Wins ===
//   Parent deadline:  HH:MM:SS.mmm (200ms from now)
//   Child requested:  5s from now
//   Child actual:     HH:MM:SS.mmm (same as parent!)
//   Child cancelled after ~200ms: context deadline exceeded
//
// === Pipeline with Deadline Budget ===
//   [stage-1] starting (budget remaining: ~500ms)
//   [stage-1] completed in 100ms
//   [stage-2] starting (budget remaining: ~400ms)
//   [stage-2] completed in 100ms
//   [stage-3] starting (budget remaining: ~300ms)
//   [stage-3] completed in 100ms
//   Pipeline result: stage-1 -> stage-2 -> stage-3 -> done
//
// === Verify Knowledge ===
//   Generous deadline (500ms, 3x100ms stages): stage-1 -> stage-2 -> stage-3 -> done
//   Tight deadline (250ms, 3x100ms stages): failed at stage-3: context deadline exceeded

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Example 1: Basic deadline -- operation exceeds its absolute deadline
// ---------------------------------------------------------------------------
// WithDeadline takes an absolute time.Time, not a duration. Use it when the
// deadline is computed externally (e.g., propagated from an upstream caller).
func basicDeadline() {
	fmt.Println("=== Basic WithDeadline ===")

	deadline := time.Now().Add(300 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	fmt.Printf("  Deadline set to: %v\n", deadline.Format("15:04:05.000"))
	fmt.Printf("  Current time:    %v\n", time.Now().Format("15:04:05.000"))

	// Simulate a 500ms operation -- the 300ms deadline fires first.
	select {
	case <-time.After(500 * time.Millisecond):
		fmt.Println("  Operation completed")
	case <-ctx.Done():
		fmt.Printf("  Deadline hit at: %v\n", time.Now().Format("15:04:05.000"))
		fmt.Printf("  Error: %v\n", ctx.Err())
	}

	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 2: Reading back the deadline with ctx.Deadline()
// ---------------------------------------------------------------------------
// ctx.Deadline() returns (deadline time.Time, ok bool). ok is false for
// contexts without a deadline (Background, TODO, plain WithCancel).
func inspectDeadline() {
	fmt.Println("=== Inspecting Deadline ===")

	// WithDeadline context has a deadline.
	deadline := time.Now().Add(2 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	if d, ok := ctx.Deadline(); ok {
		fmt.Printf("  WithDeadline context:  deadline=%v, remaining=~%v\n",
			d.Format("15:04:05.000"),
			time.Until(d).Round(time.Millisecond))
	}

	// Background context has NO deadline.
	bgCtx := context.Background()
	if _, ok := bgCtx.Deadline(); !ok {
		fmt.Println("  Background context:    no deadline")
	}

	// WithTimeout also sets a deadline internally.
	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelTimeout()

	if d, ok := timeoutCtx.Deadline(); ok {
		fmt.Printf("  WithTimeout(1s) also has deadline: %v\n", d.Format("15:04:05.000"))
	}

	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 3: Prove WithTimeout and WithDeadline are equivalent
// ---------------------------------------------------------------------------
// WithTimeout(parent, d) is implemented as WithDeadline(parent, time.Now().Add(d)).
// The deadlines should be nearly identical (sub-millisecond difference from
// the time between the two calls).
func equivalenceDemo() {
	fmt.Println("=== WithTimeout Is WithDeadline in Disguise ===")

	now := time.Now()
	duration := 500 * time.Millisecond

	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), duration)
	defer cancelTimeout()

	ctxDeadline, cancelDeadline := context.WithDeadline(context.Background(), now.Add(duration))
	defer cancelDeadline()

	deadlineFromTimeout, _ := ctxTimeout.Deadline()
	deadlineFromDeadline, _ := ctxDeadline.Deadline()

	diff := deadlineFromTimeout.Sub(deadlineFromDeadline).Abs()

	fmt.Printf("  WithTimeout deadline:  %v\n", deadlineFromTimeout.Format("15:04:05.000000"))
	fmt.Printf("  WithDeadline deadline: %v\n", deadlineFromDeadline.Format("15:04:05.000000"))
	fmt.Printf("  Difference: %v (they are equivalent)\n", diff)
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 4: A child cannot extend its parent's deadline
// ---------------------------------------------------------------------------
// If the parent expires in 200ms, a child requesting 5s still gets 200ms.
// The context tree enforces the minimum deadline at every level.
func shorterDeadlineWins() {
	fmt.Println("=== Shorter Deadline Always Wins ===")

	parent, cancelParent := context.WithDeadline(
		context.Background(),
		time.Now().Add(200*time.Millisecond),
	)
	defer cancelParent()

	// Attempt to set a much longer deadline on the child.
	child, cancelChild := context.WithDeadline(parent, time.Now().Add(5*time.Second))
	defer cancelChild()

	parentDeadline, _ := parent.Deadline()
	childDeadline, _ := child.Deadline()

	fmt.Printf("  Parent deadline:  %v (200ms from now)\n", parentDeadline.Format("15:04:05.000"))
	fmt.Printf("  Child requested:  5s from now\n")
	fmt.Printf("  Child actual:     %v (same as parent!)\n", childDeadline.Format("15:04:05.000"))

	// Wait for the child to be cancelled (will happen at parent's deadline).
	<-child.Done()
	fmt.Printf("  Child cancelled after ~200ms: %v\n", child.Err())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 5: Pipeline stages sharing a deadline budget
// ---------------------------------------------------------------------------
// A single deadline context is passed through multiple stages. Each stage
// consumes some of the budget. Later stages can check how much time remains
// and decide whether to proceed.

func pipelineStage(ctx context.Context, name string, work time.Duration) (string, error) {
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		remaining := time.Until(deadline).Round(time.Millisecond)
		fmt.Printf("  [%s] starting (budget remaining: ~%v)\n", name, remaining)

		// Bail early if there is not enough time for this stage.
		if remaining < work {
			return "", fmt.Errorf("%s: insufficient budget (%v < %v)", name, remaining, work)
		}
	}

	select {
	case <-time.After(work):
		fmt.Printf("  [%s] completed in %v\n", name, work)
		return name, nil
	case <-ctx.Done():
		return "", fmt.Errorf("%s: %w", name, ctx.Err())
	}
}

func pipelineDemo() {
	fmt.Println("=== Pipeline with Deadline Budget ===")

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(500*time.Millisecond))
	defer cancel()

	stages := []struct {
		name string
		work time.Duration
	}{
		{"stage-1", 100 * time.Millisecond},
		{"stage-2", 100 * time.Millisecond},
		{"stage-3", 100 * time.Millisecond},
	}

	result := ""
	for _, s := range stages {
		name, err := pipelineStage(ctx, s.name, s.work)
		if err != nil {
			fmt.Printf("  Pipeline failed: %v\n", err)
			fmt.Println()
			return
		}
		if result != "" {
			result += " -> "
		}
		result += name
	}

	fmt.Printf("  Pipeline result: %s -> done\n", result)
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Verify: Two pipelines with different deadline budgets
// ---------------------------------------------------------------------------
func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")

	runPipeline := func(label string, budget time.Duration) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(budget))
		defer cancel()

		stages := []string{"stage-1", "stage-2", "stage-3"}
		result := ""

		for _, name := range stages {
			select {
			case <-time.After(100 * time.Millisecond):
				if result != "" {
					result += " -> "
				}
				result += name
			case <-ctx.Done():
				fmt.Printf("  %s: failed at %s: %v\n", label, name, ctx.Err())
				return
			}
		}

		fmt.Printf("  %s: %s -> done\n", label, result)
	}

	// 500ms budget for 3x100ms stages = success.
	runPipeline("Generous deadline (500ms, 3x100ms stages)", 500*time.Millisecond)

	// 250ms budget for 3x100ms stages = fails on stage 3.
	runPipeline("Tight deadline (250ms, 3x100ms stages)", 250*time.Millisecond)
}

func main() {
	fmt.Println("Context WithDeadline")
	fmt.Println()

	basicDeadline()
	inspectDeadline()
	equivalenceDemo()
	shorterDeadlineWins()
	pipelineDemo()
	verifyKnowledge()
}
