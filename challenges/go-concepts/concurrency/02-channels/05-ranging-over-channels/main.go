package main

import (
	"fmt"
	"strings"
)

// ============================================================
// Step 1: Basic range over a channel
// ============================================================

func step1() {
	fmt.Println("--- Step 1: Basic Range ---")

	ch := make(chan int)

	go func() {
		for i := 1; i <= 5; i++ {
			ch <- i
		}
		// TODO: Close the channel so range terminates
	}()

	// TODO: Use for-range to print all values from ch
	// for val := range ch { ... }

	fmt.Println("Channel drained")
}

// ============================================================
// Step 2: Deadlock without close
// Uncomment this to see the deadlock, then comment it back.
// ============================================================

func step2Deadlock() {
	fmt.Println("--- Step 2: Deadlock Without Close ---")

	// ch := make(chan int)
	//
	// go func() {
	//     for i := 1; i <= 3; i++ {
	//         ch <- i
	//     }
	//     // No close() — range will block forever!
	// }()
	//
	// for val := range ch {
	//     fmt.Println(val)
	// }

	fmt.Println("(uncomment code to observe deadlock)")
}

// ============================================================
// Step 3: Range with buffered channel
// ============================================================

func step3() {
	fmt.Println("--- Step 3: Buffered Channel Range ---")

	ch := make(chan string, 3)

	// Send values and close — no goroutine needed since buffer has room
	ch <- "alpha"
	ch <- "beta"
	ch <- "gamma"
	// TODO: Close the channel

	// TODO: Range over ch and print each value
}

// ============================================================
// Step 4: Producer-closes, consumer-ranges
// ============================================================

// fibonacci returns a channel that produces the first n Fibonacci numbers.
// The goroutine inside closes the channel when done.
func fibonacci(n int) <-chan int {
	ch := make(chan int)

	go func() {
		// TODO: Generate Fibonacci sequence
		// a, b := 0, 1
		// Send a, then shift: a, b = b, a+b
		// Close channel when done

		close(ch) // placeholder — move after generating values
	}()

	return ch
}

func step4() {
	fmt.Println("--- Step 4: Fibonacci Generator ---")

	// TODO: Range over fibonacci(10) and print each number
	fib := fibonacci(10)
	_ = fib // replace with range loop
}

// ============================================================
// Final Challenge: Word Frequency Counter
//
// 1. generateLines() -> <-chan string (sends lines, closes)
// 2. extractWords(lines) -> <-chan string (splits lines, closes)
// 3. main counts word frequencies and prints them
//
// Lines:
//   "go channels are powerful"
//   "channels make concurrency safe"
//   "go is powerful and safe"
// ============================================================

func generateLines() <-chan string {
	ch := make(chan string)

	go func() {
		lines := []string{
			"go channels are powerful",
			"channels make concurrency safe",
			"go is powerful and safe",
		}
		// TODO: Send each line, then close
		_ = lines // remove when used
	}()

	return ch
}

func extractWords(lines <-chan string) <-chan string {
	ch := make(chan string)

	go func() {
		// TODO: Range over lines
		// For each line, split by space (strings.Fields)
		// Send each word to ch
		// Close ch when done

		_ = strings.Fields // remove when used
	}()

	return ch
}

func finalChallenge() {
	fmt.Println("--- Final: Word Frequency Counter ---")

	// TODO: Wire generateLines -> extractWords -> count in main
	// words := extractWords(generateLines())
	// freq := make(map[string]int)
	// for word := range words { freq[word]++ }
	// Print each word and its count
}

func main() {
	step1()
	fmt.Println()

	step2Deadlock()
	fmt.Println()

	step3()
	fmt.Println()

	step4()
	fmt.Println()

	finalChallenge()
}
