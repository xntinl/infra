package main

import (
	"fmt"
	"strings"
)

// ============================================================
// Step 2: Producer with send-only channel
// ============================================================

// produce sends integers 1..5 to the output channel, then closes it.
// TODO: Note the chan<- direction — this function can only send.
func produce(out chan<- int) {
	for i := 1; i <= 5; i++ {
		out <- i
	}
	close(out)

	// Uncomment the next line to see a compile error:
	// val := <-out  // cannot receive from send-only channel
}

// ============================================================
// Step 3: Consumer with receive-only channel
// ============================================================

// consume reads all values from a receive-only channel and prints them.
// TODO: Note the <-chan direction — this function can only receive.
func consume(in <-chan int) {
	for val := range in {
		fmt.Println("Received:", val)
	}

	// Uncomment the next line to see a compile error:
	// in <- 99    // cannot send to receive-only channel
	// close(in)   // cannot close receive-only channel
}

// ============================================================
// Step 4: Transformer in a pipeline
// ============================================================

// double reads values from in, doubles them, and sends to out.
// TODO: Implement this function.
func double(in <-chan int, out chan<- int) {
	// TODO: Range over in, send val*2 to out, then close out
}

func stepPipeline() {
	fmt.Println("--- Pipeline: produce -> double -> consume ---")

	raw := make(chan int)
	doubled := make(chan int)

	go produce(raw)
	go double(raw, doubled)
	consume(doubled)
}

// ============================================================
// Step 5: Generator returning receive-only channel
// ============================================================

// generateNumbers creates a channel, launches a goroutine that sends
// 1..n, closes it, and returns the channel as receive-only.
func generateNumbers(n int) <-chan int {
	// TODO: Create a bidirectional channel
	// TODO: Launch goroutine that sends 1..n and closes channel
	// TODO: Return the channel (auto-converts to <-chan int)

	ch := make(chan int) // placeholder — implement the goroutine
	return ch
}

func stepGenerator() {
	fmt.Println("--- Generator Pattern ---")

	// TODO: Call generateNumbers(5) and range over the result
	nums := generateNumbers(5)
	_ = nums // replace with range loop
}

// ============================================================
// Final Challenge: Three-stage word pipeline
//
// 1. generateWords(words) <-chan string — sends each word, closes
// 2. toUpper(in, out)                  — uppercases, closes out
// 3. addPrefix(in, out)                — prepends "PROCESSED: ", closes out
//
// Wire them and consume in main.
// Words: "go", "channels", "are", "typed"
// Expected:
//   PROCESSED: GO
//   PROCESSED: CHANNELS
//   PROCESSED: ARE
//   PROCESSED: TYPED
// ============================================================

func generateWords(words []string) <-chan string {
	// TODO: Create channel, launch goroutine, return receive-only channel
	ch := make(chan string)
	_ = words // remove when used
	return ch
}

func toUpper(in <-chan string, out chan<- string) {
	// TODO: Range over in, send strings.ToUpper(val) to out, close out
	_ = strings.ToUpper // remove when used
}

func addPrefix(in <-chan string, out chan<- string) {
	// TODO: Range over in, send "PROCESSED: "+val to out, close out
}

func finalChallenge() {
	fmt.Println("--- Final: Word Processing Pipeline ---")

	// TODO: Create the intermediate channels
	// TODO: Wire: generateWords -> toUpper -> addPrefix -> consume in main

	// Hint: the final stage consumption can be a simple range loop:
	// for word := range finalOutput {
	//     fmt.Println(word)
	// }
}

func main() {
	fmt.Println("=== Step 2-3: Produce and Consume ===")
	ch := make(chan int)
	go produce(ch)
	consume(ch)

	fmt.Println()
	fmt.Println("=== Step 4: Pipeline ===")
	stepPipeline()

	fmt.Println()
	fmt.Println("=== Step 5: Generator ===")
	stepGenerator()

	fmt.Println()
	fmt.Println("=== Final Challenge ===")
	finalChallenge()
}
