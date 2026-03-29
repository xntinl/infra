package main

import (
	"fmt"
	"time"
)

// Step 1: Create a channel and exchange a value between goroutines.
// Uncomment and complete the code below.

func step1() {
	// TODO: Create an unbuffered channel of type string
	// messages := ???

	// TODO: Launch a goroutine that sends "hello from goroutine" into the channel

	// TODO: Receive the message and print it
	_ = fmt.Println // remove once you use fmt
}

// Step 2: Prove that sends block until a receiver is ready.
// The goroutine's "send completed" message should only appear
// after main starts receiving.

func step2() {
	messages := make(chan string)

	go func() {
		fmt.Println("goroutine: about to send")
		messages <- "data"
		fmt.Println("goroutine: send completed")
	}()

	// Simulate main being busy for 500ms
	time.Sleep(500 * time.Millisecond)
	fmt.Println("main: about to receive")

	// TODO: Receive from the channel and print the value
}

// Step 3: Send three values through a channel.
// Implement this function to send 10, 20, 30 through the channel.
func sendThreeValues(ch chan int) {
	// TODO: Send 10, 20, 30 through ch
}

func step3() {
	ch := make(chan int)
	go sendThreeValues(ch)

	// TODO: Receive three values and print each as "Received: <value>"
}

// Step 4: Uncomment the code below to observe a deadlock.
// After observing the deadlock error, comment it back out.

func step4() {
	// ch := make(chan int)
	// val := <-ch
	// fmt.Println(val)
}

// Final Challenge: Two goroutines send numbers on separate channels.
// - Goroutine 1 sends even numbers: 2, 4, 6 on evens channel
// - Goroutine 2 sends odd numbers: 1, 3, 5 on odds channel
// Receive all 6 values in main and print their sum (should be 21).

func finalChallenge() {
	// TODO: Create two channels (evens and odds)

	// TODO: Launch goroutine sending 2, 4, 6 on evens

	// TODO: Launch goroutine sending 1, 3, 5 on odds

	// TODO: Receive all 6 values, sum them, and print:
	// fmt.Println("Sum:", sum)
}

func main() {
	fmt.Println("=== Step 1: Basic Send/Receive ===")
	step1()

	fmt.Println("\n=== Step 2: Send Blocks Until Receive ===")
	step2()

	fmt.Println("\n=== Step 3: Multiple Sends ===")
	step3()

	fmt.Println("\n=== Step 4: Deadlock (uncomment to test) ===")
	step4()

	fmt.Println("\n=== Final Challenge ===")
	finalChallenge()
}
