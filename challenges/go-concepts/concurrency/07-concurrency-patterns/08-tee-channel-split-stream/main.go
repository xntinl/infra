package main

// Tee-Channel: Split Stream -- Complete Working Example
//
// The tee-channel duplicates one input stream into two output streams.
// Every value from the input appears in BOTH outputs. Uses the nil-channel
// select trick to ensure both consumers receive each value.
//
// Expected output:
//   === Basic Tee ===
//     Consumer 1 received: 1
//     Consumer 2 received: 1
//     Consumer 1 received: 2
//     Consumer 2 received: 2
//     ...
//
//   === Backpressure Demo ===
//     (fast consumer paced by slow consumer)
//
//   === Log and Process ===
//     [LOG] event=1
//     [PROCESS] event=2 -> result=4
//     ...
//
//   === Tee3: Three-Way Split ===
//     A received: 1, B received: 1, C received: 1
//     ...

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// tee: duplicates one input stream into two output streams.
//
// The nil-channel select pattern is the key technique here:
//
//   For each value from input:
//     1. Set o1 = out1, o2 = out2 (both "armed")
//     2. Select: send to whichever consumer is ready first
//     3. Nil out the channel that received (o1 = nil or o2 = nil)
//     4. A nil channel blocks forever in select, so next iteration
//        MUST send to the other channel
//     5. After 2 sends, both consumers have the value
//
// Why not just `out1 <- val; out2 <- val`? Because if out2's consumer
// blocks, the next out1 send also blocks. The select approach lets us
// send to whichever is ready, handling asymmetric consumer speeds.
// More importantly, the done channel can interrupt at any point.
// ---------------------------------------------------------------------------

func tee(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int) {
	out1 := make(chan int)
	out2 := make(chan int)

	go func() {
		defer close(out1)
		defer close(out2)

		for val := range in {
			// Local copies that we can nil out after sending.
			o1, o2 := out1, out2

			// Must send to both before moving to the next value.
			for count := 0; count < 2; count++ {
				select {
				case o1 <- val:
					o1 = nil // Sent to out1. Nil it so next select goes to out2.
				case o2 <- val:
					o2 = nil // Sent to out2. Nil it so next select goes to out1.
				case <-done:
					return // Pipeline canceled -- exit immediately.
				}
			}
		}
	}()

	return out1, out2
}

// ---------------------------------------------------------------------------
// backpressureDemo: shows how the tee runs at the speed of the slowest
// consumer. The fast consumer is paced by the slow consumer because
// the tee cannot advance to the next value until BOTH have received.
// ---------------------------------------------------------------------------

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

	// Fast consumer: reads immediately, no delay.
	go func() {
		defer wg.Done()
		for v := range out1 {
			fmt.Printf("  fast consumer: got %d at %v\n", v, time.Now().Format("04:05.000"))
		}
	}()

	// Slow consumer: 200ms delay per value. The tee and fast consumer
	// are both throttled to this speed.
	go func() {
		defer wg.Done()
		for v := range out2 {
			fmt.Printf("  slow consumer: got %d at %v\n", v, time.Now().Format("04:05.000"))
			time.Sleep(200 * time.Millisecond)
		}
	}()

	wg.Wait()
	fmt.Println()
}

// ---------------------------------------------------------------------------
// bufferedConsumer: places a buffered channel between a source and a slow
// consumer to absorb speed differences. The buffer decouples the tee from
// the slow consumer for up to bufSize values.
// ---------------------------------------------------------------------------

func bufferedConsumer(done <-chan struct{}, in <-chan int, bufSize int) <-chan int {
	out := make(chan int, bufSize)
	go func() {
		defer close(out)
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- v:
				case <-done:
					return
				}
			case <-done:
				return
			}
		}
	}()
	return out
}

// ---------------------------------------------------------------------------
// logAndProcess: real-world tee application.
// Splits an event stream: one branch logs everything, the other processes
// only even events. Both consumers see every event from the source.
// ---------------------------------------------------------------------------

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

	logStream, processStream := tee(done, events)
	var wg sync.WaitGroup
	wg.Add(2)

	// Logger: records every event.
	go func() {
		defer wg.Done()
		for event := range logStream {
			fmt.Printf("  [LOG] event=%d\n", event)
		}
	}()

	// Processor: only processes even events, does "heavy" computation.
	go func() {
		defer wg.Done()
		for event := range processStream {
			if event%2 == 0 {
				fmt.Printf("  [PROCESS] event=%d -> result=%d\n", event, event*event)
			}
		}
	}()

	wg.Wait()
	fmt.Println()
}

// ---------------------------------------------------------------------------
// tee3: splits one input into three outputs.
// Same nil-channel select technique but with count < 3.
//
//              +---> out1
//   input ---> +---> out2
//              +---> out3
// ---------------------------------------------------------------------------

func tee3(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int, <-chan int) {
	out1 := make(chan int)
	out2 := make(chan int)
	out3 := make(chan int)

	go func() {
		defer close(out1)
		defer close(out2)
		defer close(out3)

		for val := range in {
			o1, o2, o3 := out1, out2, out3

			// Send to all three before advancing.
			for count := 0; count < 3; count++ {
				select {
				case o1 <- val:
					o1 = nil
				case o2 <- val:
					o2 = nil
				case o3 <- val:
					o3 = nil
				case <-done:
					return
				}
			}
		}
	}()

	return out1, out2, out3
}

// ---------------------------------------------------------------------------
// teeWithBuffer: variant that uses buffered output channels to partially
// decouple consumers. The buffer absorbs speed differences up to capacity.
// ---------------------------------------------------------------------------

func teeWithBuffer(done <-chan struct{}, in <-chan int, bufSize int) (<-chan int, <-chan int) {
	out1 := make(chan int, bufSize)
	out2 := make(chan int, bufSize)

	go func() {
		defer close(out1)
		defer close(out2)

		for val := range in {
			o1, o2 := out1, out2
			for count := 0; count < 2; count++ {
				select {
				case o1 <- val:
					o1 = nil
				case o2 <- val:
					o2 = nil
				case <-done:
					return
				}
			}
		}
	}()

	return out1, out2
}

func main() {
	fmt.Println("Exercise: Tee-Channel -- Split Stream")
	fmt.Println()

	// Basic tee: both consumers receive every value.
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

	// Backpressure: slow consumer throttles the entire tee.
	backpressureDemo()

	// Log and process: real-world tee application.
	logAndProcess()

	// Tee3: three-way split.
	fmt.Println("=== Tee3: Three-Way Split ===")
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
	go func() {
		defer wg3.Done()
		for v := range a {
			fmt.Printf("  A received: %d\n", v)
		}
	}()
	go func() {
		defer wg3.Done()
		for v := range b {
			fmt.Printf("  B received: %d\n", v)
		}
	}()
	go func() {
		defer wg3.Done()
		for v := range c {
			fmt.Printf("  C received: %d\n", v)
		}
	}()
	wg3.Wait()
	close(done3)
	fmt.Println()

	// Buffered tee: partially decouples consumer speeds.
	fmt.Println("=== Buffered Tee ===")
	done4 := make(chan struct{})
	gen4 := make(chan int)
	go func() {
		defer close(gen4)
		for i := 1; i <= 5; i++ {
			gen4 <- i
		}
	}()

	buf1, buf2 := teeWithBuffer(done4, gen4, 5)
	var wg4 sync.WaitGroup
	wg4.Add(2)
	go func() {
		defer wg4.Done()
		for v := range buf1 {
			fmt.Printf("  Buffered fast: %d\n", v)
		}
	}()
	go func() {
		defer wg4.Done()
		for v := range buf2 {
			fmt.Printf("  Buffered slow: %d\n", v)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	wg4.Wait()
	close(done4)
}
