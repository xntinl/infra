package main

import (
	"fmt"
	"time"
)

func main() {
	// Step 1: Basic event loop with work + quit channels.
	fmt.Println("=== Step 1 ===")

	work := make(chan string)
	quit := make(chan struct{})

	go func() {
		// TODO: for-select loop
		// case task := <-work: print "processing: <task>"
		// case <-quit: print "shutting down", then return
		_ = work
		_ = quit
	}()

	// TODO: send "task-1", "task-2", "task-3" on work channel
	// TODO: close(quit) to signal shutdown

	time.Sleep(100 * time.Millisecond)

	// Step 2: Multiple event sources.
	fmt.Println("\n=== Step 2 ===")

	orders := make(chan string, 5)
	alerts := make(chan string, 5)
	quit2 := make(chan struct{})

	go func() {
		for i := 0; i < 5; i++ {
			orders <- fmt.Sprintf("order-%d", i)
			time.Sleep(30 * time.Millisecond)
		}
	}()

	go func() {
		for i := 0; i < 3; i++ {
			alerts <- fmt.Sprintf("alert-%d", i)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	go func() {
		time.Sleep(300 * time.Millisecond)
		close(quit2)
	}()

	// TODO: for-select loop with three cases:
	// case order := <-orders: print "[ORDER] <order>"
	// case alert := <-alerts: print "[ALERT] <alert>"
	// case <-quit2: print "event loop stopped", return
	_ = orders
	_ = alerts
	_ = quit2

	// Step 3: Detect channel close, use nil channel trick.
	fmt.Println("\n=== Step 3 ===")

	source1 := make(chan int)
	source2 := make(chan int)

	go func() {
		for i := 0; i < 3; i++ {
			source1 <- i
			time.Sleep(50 * time.Millisecond)
		}
		close(source1)
	}()

	go func() {
		for i := 10; i < 14; i++ {
			source2 <- i
			time.Sleep(30 * time.Millisecond)
		}
		close(source2)
	}()

	// TODO: track s1Done, s2Done as booleans
	// TODO: for-select loop
	//   case val, ok := <-source1: if !ok, set source1=nil and s1Done=true, else print
	//   case val, ok := <-source2: if !ok, set source2=nil and s2Done=true, else print
	// After select: if both done, break out of loop
	_ = source1
	_ = source2

	fmt.Println("all exercises complete")
}
