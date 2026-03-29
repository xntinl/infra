package main

// Exercise: Pipeline Pattern
// Instructions: see 01-pipeline-pattern.md

import "fmt"

// Step 1: Implement generator.
// It takes a variadic list of ints, returns a receive-only channel,
// and sends each value into the channel from a goroutine.
// Close the channel when all values have been sent.
func generator(nums ...int) <-chan int {
	out := make(chan int)
	// TODO: launch a goroutine that sends each num into out, then closes it
	go func() {
		close(out)
	}()
	return out
}

// Step 2: Implement square.
// It reads ints from an input channel, squares each one,
// and sends the result to an output channel.
func square(in <-chan int) <-chan int {
	out := make(chan int)
	// TODO: launch a goroutine that ranges over in, sends n*n to out, then closes out
	go func() {
		close(out)
	}()
	return out
}

// Step 4: Implement filter.
// It reads ints from an input channel and only forwards
// values that exceed the given threshold.
func filter(in <-chan int, threshold int) <-chan int {
	out := make(chan int)
	// TODO: launch a goroutine that ranges over in, sends values > threshold, then closes out
	go func() {
		_ = threshold
		close(out)
	}()
	return out
}

// Step 3: Chain the pipeline stages together.
func runPipeline() {
	// TODO: create a generator with values 2, 3, 4, 5
	// TODO: pass the generator output through square
	// TODO: range over the squared output and print each result
	fmt.Println("Pipeline output:")
}

// Step 4 continued: Chain with the filter stage.
func runFilteredPipeline() {
	// TODO: generator(2, 3, 4, 5) -> square -> filter(threshold=10) -> print
	fmt.Println("Filtered pipeline output:")
}

// Verify: Build a double stage and a four-stage pipeline.
// double multiplies each value by 2.
// Chain: generator -> square -> double -> filter(threshold=30) -> print
func double(in <-chan int) <-chan int {
	out := make(chan int)
	// TODO: implement double stage
	go func() {
		close(out)
	}()
	return out
}

func runFullPipeline() {
	fmt.Println("Full pipeline output:")
	// TODO: chain generator(2,3,4,5) -> square -> double -> filter(30) -> print
}

func main() {
	fmt.Println("Exercise: Pipeline Pattern\n")

	// Step 1 verification: generator only
	fmt.Print("Generator output: ")
	for n := range generator(2, 3, 4, 5) {
		fmt.Printf("%d ", n)
	}
	fmt.Println()

	// Step 2 verification: generator -> square
	fmt.Print("Squared output: ")
	for n := range square(generator(2, 3, 4, 5)) {
		fmt.Printf("%d ", n)
	}
	fmt.Println()

	// Step 3
	runPipeline()

	// Step 4
	runFilteredPipeline()

	// Verify
	runFullPipeline()
}
