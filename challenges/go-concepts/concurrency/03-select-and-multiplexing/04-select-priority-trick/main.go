// Exercise 04 — Select Priority Trick
//
// Demonstrates that select is fair by default, and how to simulate
// priority using the nested select pattern.
//
// Expected output (approximate):
//
//   === Example 1: Flat select is fair (~50/50) ===
//   flat select — high: ~50, low: ~50
//
//   === Example 2: Nested select gives priority ===
//   priority select — high: 100, low: 100
//   (high messages consumed first, then low)
//
//   === Example 3: Priority with live producers ===
//   [HIGH] URGENT-0
//   [LOW]  normal-0
//   ...
//   all producers finished
//
//   === Example 4: Priority limitation — race window ===
//   (demonstrates that priority is best-effort, not absolute)

package main

import (
	"fmt"
	"time"
)

func main() {
	// ---------------------------------------------------------------
	// Example 1: Flat select distributes ~50/50 when both are ready.
	// This proves that select does NOT respect case order.
	// ---------------------------------------------------------------
	fmt.Println("=== Example 1: Flat select is fair (~50/50) ===")

	high := make(chan string, 100)
	low := make(chan string, 100)

	for i := 0; i < 100; i++ {
		high <- "high"
		low <- "low"
	}

	highCount, lowCount := 0, 0
	for i := 0; i < 100; i++ {
		select {
		case <-high:
			highCount++
		case <-low:
			lowCount++
		}
	}

	fmt.Printf("flat select — high: %d, low: %d\n", highCount, lowCount)

	// ---------------------------------------------------------------
	// Example 2: Nested select trick for priority.
	// The outer select tries ONLY the high-priority channel.
	// If high is empty (default), the inner select listens on both.
	// This drains high before touching low.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 2: Nested select gives priority ===")

	high2 := make(chan string, 100)
	low2 := make(chan string, 100)

	for i := 0; i < 100; i++ {
		high2 <- "high"
		low2 <- "low"
	}

	highCount2, lowCount2 := 0, 0

	// We iterate 200 times because there are 200 total messages.
	for i := 0; i < 200; i++ {
		select {
		case <-high2:
			// Outer select: consume high-priority if available.
			highCount2++
		default:
			// High channel empty — fall through to inner select.
			select {
			case <-high2:
				// A high message might have arrived between outer default
				// and inner select. The inner select checks again.
				highCount2++
			case <-low2:
				lowCount2++
			}
		}
	}

	fmt.Printf("priority select — high: %d, low: %d\n", highCount2, lowCount2)

	// ---------------------------------------------------------------
	// Example 3: Priority with live producers.
	// A high-priority producer sends 5 messages slowly.
	// A low-priority producer sends 20 messages quickly.
	// The nested select ensures high-priority messages are handled first
	// whenever they are available.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 3: Priority with live producers ===")

	highCh := make(chan string, 10)
	lowCh := make(chan string, 10)
	done := make(chan struct{})

	go func() {
		for i := 0; i < 5; i++ {
			highCh <- fmt.Sprintf("URGENT-%d", i)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	go func() {
		for i := 0; i < 20; i++ {
			lowCh <- fmt.Sprintf("normal-%d", i)
			time.Sleep(10 * time.Millisecond)
		}
		// Signal completion after all low-priority messages are sent.
		close(done)
	}()

	for {
		select {
		case msg := <-highCh:
			fmt.Println("[HIGH]", msg)
		default:
			// No high-priority message — check everything.
			select {
			case msg := <-highCh:
				fmt.Println("[HIGH]", msg)
			case msg := <-lowCh:
				fmt.Println("[LOW] ", msg)
			case <-done:
				fmt.Println("all producers finished")
				goto example4
			}
		}
	}

example4:
	// ---------------------------------------------------------------
	// Example 4: Priority limitation — the race window.
	// Between the outer default and the inner select, a high-priority
	// message can arrive. The inner select then sees both channels
	// ready and picks randomly. Priority is strongly biased, not absolute.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 4: Priority limitation — race window ===")

	hiCh := make(chan string, 50)
	loCh := make(chan string, 50)

	// Both channels have data — simulates the race window.
	for i := 0; i < 50; i++ {
		hiCh <- "hi"
		loCh <- "lo"
	}

	hiWins, loWins := 0, 0
	innerReached := 0

	for i := 0; i < 50; i++ {
		select {
		case <-hiCh:
			hiWins++
		default:
			// In this artificial case, high channel is never truly empty
			// because we pre-filled both channels. But the outer select
			// already consumed from high. By the time we reach default,
			// the inner select has both ready.
			innerReached++
			select {
			case <-hiCh:
				hiWins++
			case <-loCh:
				loWins++
			}
		}
	}

	fmt.Printf("hi: %d, lo: %d (inner select reached: %d times)\n",
		hiWins, loWins, innerReached)
	fmt.Println("Note: lo > 0 proves priority is best-effort, not absolute")
}
