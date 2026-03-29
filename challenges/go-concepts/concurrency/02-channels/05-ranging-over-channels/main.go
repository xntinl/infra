package main

import (
	"fmt"
	"strings"
)

// This program demonstrates for-range on channels and the producer-closes pattern.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Basic Range Over Channel ===
//   1
//   2
//   3
//   4
//   5
//   Channel fully drained
//
//   === Example 2: Range Over Buffered Channel ===
//   alpha
//   beta
//   gamma
//   All buffered values consumed after close
//
//   === Example 3: Fibonacci Generator ===
//   0 1 1 2 3 5 8 13 21 34
//
//   === Example 4: Pipeline with Range ===
//   Squared: 1
//   Squared: 4
//   Squared: 9
//   Squared: 16
//   Squared: 25
//
//   === Example 5: Word Frequency Counter ===
//   go: 2
//   channels: 2
//   ...

func main() {
	example1BasicRange()
	example2BufferedRange()
	example3FibonacciGenerator()
	example4PipelineWithRange()
	example5WordFrequencyCounter()
}

// example1BasicRange shows the simplest for-range loop on a channel.
// range receives values one at a time and exits when the channel is closed
// AND all values have been consumed.
func example1BasicRange() {
	fmt.Println("=== Example 1: Basic Range Over Channel ===")

	ch := make(chan int)

	go func() {
		for i := 1; i <= 5; i++ {
			ch <- i
		}
		// close() is REQUIRED. Without it, range blocks forever waiting for more
		// values, and Go's runtime detects a deadlock.
		close(ch)
	}()

	// for val := range ch is equivalent to:
	//   for { val, ok := <-ch; if !ok { break }; ... }
	// It's cleaner and idiomatic.
	for val := range ch {
		fmt.Println(val)
	}
	fmt.Println("Channel fully drained")
	fmt.Println()
}

// example2BufferedRange proves that range on a closed buffered channel drains
// all remaining values before exiting. Close doesn't discard queued data.
func example2BufferedRange() {
	fmt.Println("=== Example 2: Range Over Buffered Channel ===")

	ch := make(chan string, 3)

	// No goroutine needed -- the buffer holds all three values.
	ch <- "alpha"
	ch <- "beta"
	ch <- "gamma"
	close(ch) // close with 3 values still in the buffer

	// range consumes all three buffered values, then exits.
	for val := range ch {
		fmt.Println(val)
	}
	fmt.Println("All buffered values consumed after close")
	fmt.Println()
}

// fibonacci returns a channel that produces the first n Fibonacci numbers.
// The goroutine inside owns the channel lifecycle: it produces, then closes.
// The consumer simply ranges over the channel -- no need to know the count.
func fibonacci(n int) <-chan int {
	ch := make(chan int)
	go func() {
		a, b := 0, 1
		for i := 0; i < n; i++ {
			ch <- a
			a, b = b, a+b
		}
		close(ch)
	}()
	return ch
}

// example3FibonacciGenerator demonstrates the producer-closes, consumer-ranges pattern.
// The producer (fibonacci) decides how many values to produce and closes when done.
// The consumer doesn't need to track count -- it just ranges.
func example3FibonacciGenerator() {
	fmt.Println("=== Example 3: Fibonacci Generator ===")

	for num := range fibonacci(10) {
		fmt.Printf("%d ", num)
	}
	fmt.Println()
	fmt.Println()
}

// square is a pipeline stage that reads ints, squares them, and writes to output.
func square(in <-chan int, out chan<- int) {
	for val := range in {
		out <- val * val
	}
	close(out)
}

// example4PipelineWithRange demonstrates chaining range-based stages in a pipeline.
// Each stage reads from its input until it's closed, processes, and closes its output.
func example4PipelineWithRange() {
	fmt.Println("=== Example 4: Pipeline with Range ===")

	// Stage 1: generate numbers
	nums := make(chan int)
	go func() {
		for i := 1; i <= 5; i++ {
			nums <- i
		}
		close(nums)
	}()

	// Stage 2: square each number
	squared := make(chan int)
	go square(nums, squared)

	// Stage 3: consume (runs in main goroutine)
	for val := range squared {
		fmt.Println("Squared:", val)
	}
	fmt.Println()
}

// --- Word frequency pipeline for Example 5 ---

func generateLines() <-chan string {
	ch := make(chan string)
	go func() {
		lines := []string{
			"go channels are powerful",
			"channels make concurrency safe",
			"go is powerful and safe",
		}
		for _, line := range lines {
			ch <- line
		}
		close(ch)
	}()
	return ch
}

func extractWords(lines <-chan string) <-chan string {
	ch := make(chan string)
	go func() {
		for line := range lines {
			for _, word := range strings.Fields(line) {
				ch <- word
			}
		}
		close(ch)
	}()
	return ch
}

// example5WordFrequencyCounter builds a two-stage pipeline (lines -> words)
// and counts word frequencies in the final stage. This is a realistic example
// of how range-based channel pipelines compose naturally.
func example5WordFrequencyCounter() {
	fmt.Println("=== Example 5: Word Frequency Counter ===")

	words := extractWords(generateLines())

	freq := make(map[string]int)
	for word := range words {
		freq[word]++
	}

	for word, count := range freq {
		fmt.Printf("  %s: %d\n", word, count)
	}
}
