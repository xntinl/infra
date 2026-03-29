package main

// Pipeline Pattern -- Complete Working Example
//
// A pipeline is a series of stages connected by channels, where each stage
// is a goroutine that receives values from upstream, transforms them, and
// sends them downstream. Closing the output channel signals completion.
//
// Expected output:
//   === Generator Only ===
//   Generator output: 2 3 4 5
//
//   === Generator -> Square ===
//   Squared output: 4 9 16 25
//
//   === Three-Stage Pipeline ===
//   Pipeline output:
//     4
//     9
//     16
//     25
//
//   === Filtered Pipeline (threshold=10) ===
//   Filtered pipeline output:
//     16
//     25
//
//   === Four-Stage Pipeline: gen -> square -> double -> filter(30) ===
//   Full pipeline output:
//     32
//     50
//
//   === String Pipeline ===
//   String pipeline output:
//     [HELLO]
//     [WORLD]
//     [GOPHER]

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Stage 1: Generator -- the head of every pipeline.
// Takes raw input values and converts them into a channel stream.
// The returned channel is receive-only so the compiler enforces direction.
// ---------------------------------------------------------------------------

func generator(nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		// Send each value into the channel. The goroutine blocks on each
		// send until the downstream stage reads, providing backpressure.
		for _, n := range nums {
			out <- n
		}
		// Closing signals downstream that no more values will arrive.
		// Without this, any `range` loop on the channel blocks forever.
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// Stage 2: Square -- a transform stage.
// Every pipeline stage follows the same shape:
//   1. Accept an input channel
//   2. Return an output channel
//   3. Process in a goroutine
//   4. Close output when done
// This uniformity makes stages composable like LEGO bricks.
// ---------------------------------------------------------------------------

func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			out <- n * n
		}
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// Stage 3: Filter -- conditionally forwards values.
// Demonstrates that not every value needs to pass through. The output
// channel may produce fewer values than the input channel.
// ---------------------------------------------------------------------------

func filter(in <-chan int, threshold int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			if n > threshold {
				out <- n
			}
		}
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// Stage 4: Double -- multiplies each value by 2.
// Another transform stage showing pipeline extensibility.
// ---------------------------------------------------------------------------

func double(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			out <- n * 2
		}
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// Generic pipeline stage: mapStage applies any func(int)->int.
// This is the production pattern -- instead of writing a new function for
// each transform, parameterize the stage with a function.
// ---------------------------------------------------------------------------

func mapStage(in <-chan int, fn func(int) int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			out <- fn(n)
		}
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// String pipeline stages -- demonstrates pipelines are not limited to int.
// Any type can flow through a pipeline.
// ---------------------------------------------------------------------------

func stringGenerator(words ...string) <-chan string {
	out := make(chan string)
	go func() {
		for _, w := range words {
			out <- w
		}
		close(out)
	}()
	return out
}

func toUpper(in <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		for s := range in {
			out <- strings.ToUpper(s)
		}
		close(out)
	}()
	return out
}

func bracket(in <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		for s := range in {
			out <- "[" + s + "]"
		}
		close(out)
	}()
	return out
}

func filterByLength(in <-chan string, minLen int) <-chan string {
	out := make(chan string)
	go func() {
		for s := range in {
			if len(s) >= minLen {
				out <- s
			}
		}
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// Pipeline compositions
// ---------------------------------------------------------------------------

func runPipeline() {
	fmt.Println("=== Three-Stage Pipeline ===")
	// Composition reads left-to-right: generate -> square -> consume
	nums := generator(2, 3, 4, 5)
	squared := square(nums)

	fmt.Println("Pipeline output:")
	for result := range squared {
		fmt.Printf("  %d\n", result)
	}
}

func runFilteredPipeline() {
	fmt.Println("\n=== Filtered Pipeline (threshold=10) ===")
	// Adding a stage is just inserting a function call in the chain.
	// Existing stages remain untouched -- open/closed principle.
	nums := generator(2, 3, 4, 5)
	squared := square(nums)
	filtered := filter(squared, 10)

	fmt.Println("Filtered pipeline output:")
	for result := range filtered {
		fmt.Printf("  %d\n", result)
	}
}

func runFullPipeline() {
	fmt.Println("\n=== Four-Stage Pipeline: gen -> square -> double -> filter(30) ===")
	// gen(2,3,4,5) -> square(4,9,16,25) -> double(8,18,32,50) -> filter>30(32,50)
	nums := generator(2, 3, 4, 5)
	squared := square(nums)
	doubled := double(squared)
	filtered := filter(doubled, 30)

	fmt.Println("Full pipeline output:")
	for result := range filtered {
		fmt.Printf("  %d\n", result)
	}
}

func runStringPipeline() {
	fmt.Println("\n=== String Pipeline ===")
	// Demonstrates pipelines work with any type, not just integers.
	// gen -> filterByLength(>=4) -> toUpper -> bracket
	words := stringGenerator("hi", "hello", "go", "world", "gopher")
	long := filterByLength(words, 4)
	upper := toUpper(long)
	bracketed := bracket(upper)

	fmt.Println("String pipeline output:")
	for s := range bracketed {
		fmt.Printf("  %s\n", s)
	}
}

func main() {
	fmt.Println("Exercise: Pipeline Pattern")
	fmt.Println()

	// Stage verification: generator alone
	fmt.Println("=== Generator Only ===")
	fmt.Print("Generator output: ")
	for n := range generator(2, 3, 4, 5) {
		fmt.Printf("%d ", n)
	}
	fmt.Println()

	// Stage verification: generator -> square
	fmt.Println("\n=== Generator -> Square ===")
	fmt.Print("Squared output: ")
	for n := range square(generator(2, 3, 4, 5)) {
		fmt.Printf("%d ", n)
	}
	fmt.Println()

	// Composed pipelines
	fmt.Println()
	runPipeline()
	runFilteredPipeline()
	runFullPipeline()
	runStringPipeline()
}
