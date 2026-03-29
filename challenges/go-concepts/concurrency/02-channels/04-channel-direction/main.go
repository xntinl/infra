package main

import (
	"fmt"
	"strings"
)

// This program demonstrates directional channel types for compile-time safety.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Send-Only Producer ===
//   1
//   2
//   3
//   4
//   5
//
//   === Example 2: Receive-Only Consumer ===
//   Received: 10
//   Received: 20
//   Received: 30
//
//   === Example 3: Pipeline (produce -> double -> print) ===
//   Doubled: 2
//   Doubled: 4
//   Doubled: 6
//   Doubled: 8
//   Doubled: 10
//
//   === Example 4: Generator Returns Receive-Only ===
//   1
//   2
//   3
//   4
//   5
//
//   === Example 5: Three-Stage Word Pipeline ===
//   PROCESSED: GO
//   PROCESSED: CHANNELS
//   PROCESSED: ARE
//   PROCESSED: TYPED

func main() {
	example1SendOnlyProducer()
	example2ReceiveOnlyConsumer()
	example3Pipeline()
	example4Generator()
	example5WordPipeline()
}

// produce takes a send-only channel: chan<- int.
// The arrow points INTO chan, meaning this function can ONLY send.
// Attempting to receive (val := <-out) would be a compile error:
//
//	"invalid operation: cannot receive from send-only channel"
func produce(out chan<- int, count int) {
	for i := 1; i <= count; i++ {
		out <- i
	}
	// The producer owns the channel lifecycle -- it closes when done.
	close(out)
}

// example1SendOnlyProducer demonstrates a function that only writes to a channel.
func example1SendOnlyProducer() {
	fmt.Println("=== Example 1: Send-Only Producer ===")

	// make creates a bidirectional channel. When passed to produce(),
	// Go automatically narrows it to send-only. This is an implicit conversion.
	ch := make(chan int)
	go produce(ch, 5)

	for val := range ch {
		fmt.Println(val)
	}
	fmt.Println()
}

// consume takes a receive-only channel: <-chan int.
// The arrow points OUT of chan, meaning this function can ONLY receive.
// Attempting to send (in <- 99) or close (close(in)) would be a compile error.
func consume(in <-chan int) {
	for val := range in {
		fmt.Println("Received:", val)
	}
}

// example2ReceiveOnlyConsumer demonstrates a function that only reads from a channel.
func example2ReceiveOnlyConsumer() {
	fmt.Println("=== Example 2: Receive-Only Consumer ===")

	ch := make(chan int)
	go func() {
		for _, v := range []int{10, 20, 30} {
			ch <- v
		}
		close(ch)
	}()

	consume(ch) // ch narrows from chan int to <-chan int automatically
	fmt.Println()
}

// double is a pipeline stage: reads from input (receive-only), writes to output (send-only).
// Each parameter constrains exactly what this function can do with each channel.
// Reading the signature tells you the data flow direction without looking at the body.
func double(in <-chan int, out chan<- int) {
	for val := range in {
		out <- val * 2
	}
	close(out)
}

// example3Pipeline wires three stages: produce -> double -> consume in main.
func example3Pipeline() {
	fmt.Println("=== Example 3: Pipeline (produce -> double -> print) ===")

	raw := make(chan int)
	doubled := make(chan int)

	go produce(raw, 5)       // sends 1..5 to raw
	go double(raw, doubled)  // reads from raw, sends *2 to doubled

	// Final stage runs in main goroutine.
	for val := range doubled {
		fmt.Println("Doubled:", val)
	}
	fmt.Println()
}

// generateNumbers creates a channel internally and returns it as receive-only.
// The caller can only read from the returned channel. The goroutine inside holds
// the only send-capable reference. This enforces the producer-closes contract.
func generateNumbers(n int) <-chan int {
	ch := make(chan int) // bidirectional inside the function
	go func() {
		for i := 1; i <= n; i++ {
			ch <- i
		}
		close(ch)
	}()
	// Returning chan int as <-chan int is an automatic narrowing conversion.
	return ch
}

// example4Generator demonstrates the generator pattern: a function that returns
// a receive-only channel. The internal goroutine is the producer; the caller is
// the consumer. This is a clean ownership model.
func example4Generator() {
	fmt.Println("=== Example 4: Generator Returns Receive-Only ===")

	nums := generateNumbers(5)
	for val := range nums {
		fmt.Println(val)
	}
	fmt.Println()
}

// --- Three-stage word pipeline for Example 5 ---

func generateWords(words []string) <-chan string {
	ch := make(chan string)
	go func() {
		for _, w := range words {
			ch <- w
		}
		close(ch)
	}()
	return ch
}

func toUpper(in <-chan string, out chan<- string) {
	for word := range in {
		out <- strings.ToUpper(word)
	}
	close(out)
}

func addPrefix(in <-chan string, out chan<- string) {
	for word := range in {
		out <- "PROCESSED: " + word
	}
	close(out)
}

// example5WordPipeline wires three string-processing stages together.
// Each stage only has the channel direction it needs, enforced by the compiler.
func example5WordPipeline() {
	fmt.Println("=== Example 5: Three-Stage Word Pipeline ===")

	words := generateWords([]string{"go", "channels", "are", "typed"})

	uppered := make(chan string)
	prefixed := make(chan string)

	go toUpper(words, uppered)
	go addPrefix(uppered, prefixed)

	for result := range prefixed {
		fmt.Println(result)
	}
}
