package main

// Exercise: Tee-Channel -- Split Stream
// Instructions: see 08-tee-channel-split-stream.md

import (
	"fmt"
	"sync"
	"time"
)

// Step 1: Implement tee.
// Reads from in, sends each value to BOTH out1 and out2.
// Uses the nil-channel select pattern to ensure both receive each value.
// Respects the done channel for cancellation.
func tee(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int) {
	out1 := make(chan int)
	out2 := make(chan int)

	go func() {
		defer close(out1)
		defer close(out2)

		for val := range in {
			// TODO: use local copies o1, o2 of out1, out2
			// TODO: inner loop (count < 2) with select:
			//   case o1 <- val: set o1 = nil
			//   case o2 <- val: set o2 = nil
			//   case <-done: return
			_ = val
			_ = done
		}
	}()

	return out1, out2
}

// Step 2: backpressureDemo shows how a slow consumer affects the tee.
func backpressureDemo() {
	fmt.Println("=== Backpressure Demo ===")
	done := make(chan struct{})
	defer close(done)

	gen := make(chan int)
	go func() {
		defer close(gen)
		for i := 1; i <= 5; i++ {
			fmt.Printf("  generator: sending %d at %v\n", i, time.Now().Format("04:05.000"))
			gen <- i
		}
	}()

	out1, out2 := tee(done, gen)
	var wg sync.WaitGroup
	wg.Add(2)

	// TODO: launch fast consumer (reads from out1, prints immediately)
	// TODO: launch slow consumer (reads from out2, sleeps 200ms per value)
	// Observe the timestamps to see backpressure in action
	_ = out1
	_ = out2

	wg.Wait()
	fmt.Println()
}

// Step 3: Implement bufferedConsumer.
// Places a buffered channel between a source and a slow consumer
// to absorb speed differences.
func bufferedConsumer(done <-chan struct{}, in <-chan int, bufSize int) <-chan int {
	out := make(chan int, bufSize)
	// TODO: launch goroutine that forwards from in to out, respecting done
	go func() {
		defer close(out)
		_ = done
		_ = in
	}()
	return out
}

// Step 4: logAndProcess splits a stream for logging and processing.
func logAndProcess() {
	fmt.Println("=== Log and Process ===")
	done := make(chan struct{})
	defer close(done)

	events := make(chan int)
	go func() {
		defer close(events)
		for i := 1; i <= 8; i++ {
			events <- i
		}
	}()

	// TODO: tee the events stream
	// TODO: launch logger goroutine (prints all events)
	// TODO: launch processor goroutine (processes even events only)
	// TODO: wait for both

	fmt.Println()
}

// Verify: Implement tee3 that splits one input into three outputs.
func tee3(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int, <-chan int) {
	out1 := make(chan int)
	out2 := make(chan int)
	out3 := make(chan int)

	go func() {
		defer close(out1)
		defer close(out2)
		defer close(out3)

		for val := range in {
			// TODO: same nil-channel pattern but with count < 3
			// TODO: select on o1, o2, o3, done
			_ = val
			_ = done
		}
	}()

	return out1, out2, out3
}

func main() {
	fmt.Println("Exercise: Tee-Channel -- Split Stream\n")

	// Step 1: basic tee
	fmt.Println("=== Basic Tee ===")
	done := make(chan struct{})
	gen := make(chan int)
	go func() {
		defer close(gen)
		for i := 1; i <= 5; i++ {
			gen <- i
		}
	}()

	out1, out2 := tee(done, gen)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for v := range out1 {
			fmt.Printf("  Consumer 1 received: %d\n", v)
		}
	}()
	go func() {
		defer wg.Done()
		for v := range out2 {
			fmt.Printf("  Consumer 2 received: %d\n", v)
		}
	}()
	wg.Wait()
	close(done)
	fmt.Println()

	// Step 2
	backpressureDemo()

	// Step 4
	logAndProcess()

	// Verify: tee3
	fmt.Println("=== Verify: Tee3 ===")
	done3 := make(chan struct{})
	gen3 := make(chan int)
	go func() {
		defer close(gen3)
		for i := 1; i <= 4; i++ {
			gen3 <- i
		}
	}()

	a, b, c := tee3(done3, gen3)
	var wg3 sync.WaitGroup
	wg3.Add(3)
	for name, ch := range map[string]<-chan int{"A": a, "B": b, "C": c} {
		go func(n string, ch <-chan int) {
			defer wg3.Done()
			for v := range ch {
				fmt.Printf("  %s received: %d\n", n, v)
			}
		}(name, ch)
	}
	wg3.Wait()
	close(done3)
}
