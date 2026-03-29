// Exercise 07 — Heartbeat with Select
//
// Demonstrates time.Ticker heartbeats, health monitoring, stall detection,
// and encapsulation of the heartbeat pattern into a reusable function.
//
// Expected output (approximate):
//
//   === Example 1: Basic heartbeat ===
//   result: 0
//   result: 1
//   heartbeat received
//   result: 2
//   result: 3
//   heartbeat received
//   ...
//   stopping after 1s
//
//   === Example 2: Stall detection ===
//   supervisor: heartbeat OK
//   supervisor: heartbeat OK
//   ...
//   worker: entering stall
//   supervisor: ALERT — worker stalled!
//
//   === Example 3: Reusable heartbeatWorker ===
//   pulse
//   result: 0
//   result: 1
//   pulse
//   ...
//   done
//
//   === Example 4: Multiple monitored workers ===
//   [worker-0] heartbeat
//   [worker-1] heartbeat
//   ...

package main

import (
	"fmt"
	"time"
)

// heartbeatWorker encapsulates the heartbeat pattern into a reusable function.
// It returns read-only channels for the heartbeat signal and work results.
// The caller controls lifetime via the done channel.
func heartbeatWorker(
	done <-chan struct{},
	pulseInterval time.Duration,
	work func(i int) int,
) (<-chan struct{}, <-chan int) {
	// Buffered with 1: if the supervisor hasn't consumed the last heartbeat,
	// the worker drops the new one instead of blocking.
	heartbeat := make(chan struct{}, 1)
	results := make(chan int)

	go func() {
		defer close(results)
		ticker := time.NewTicker(pulseInterval)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Non-blocking send: drop heartbeat if supervisor is slow.
				select {
				case heartbeat <- struct{}{}:
				default:
				}
			case results <- work(i):
				i++
			}
		}
	}()

	return heartbeat, results
}

func main() {
	// ---------------------------------------------------------------
	// Example 1: Basic heartbeat with time.Ticker.
	// A worker sends periodic heartbeats alongside work results.
	// The heartbeat channel is buffered to avoid blocking the worker.
	// ---------------------------------------------------------------
	fmt.Println("=== Example 1: Basic heartbeat ===")

	done := make(chan struct{})
	heartbeat := make(chan struct{}, 1)
	workResults := make(chan int)

	go func() {
		defer close(workResults)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Non-blocking heartbeat send. If the supervisor hasn't
				// consumed the last one, drop it. This prevents the
				// heartbeat mechanism from interfering with actual work.
				select {
				case heartbeat <- struct{}{}:
				default:
				}
			case workResults <- i:
				i++
				time.Sleep(100 * time.Millisecond) // Simulate work
			}
		}
	}()

	// Consume results and heartbeats for 1 second.
	timeout := time.After(1 * time.Second)
consumeLoop:
	for {
		select {
		case val, ok := <-workResults:
			if !ok {
				break consumeLoop
			}
			fmt.Println("result:", val)
		case <-heartbeat:
			fmt.Println("heartbeat received")
		case <-timeout:
			fmt.Println("stopping after 1s")
			close(done)
			break consumeLoop
		}
	}

	// ---------------------------------------------------------------
	// Example 2: Detecting a stalled worker.
	// The supervisor uses a timer that resets on every heartbeat.
	// If no heartbeat arrives within the timeout window, the worker
	// is declared stalled.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 2: Stall detection ===")

	done2 := make(chan struct{})
	heartbeat2 := make(chan struct{}, 1)

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for i := 0; ; i++ {
			// Simulate a stall after 5 healthy iterations.
			if i == 5 {
				fmt.Println("worker: entering stall (blocked operation)")
				time.Sleep(5 * time.Second) // Simulate deadlock or slow external call.
			}

			select {
			case <-done2:
				fmt.Println("worker: shutting down")
				return
			case <-ticker.C:
				select {
				case heartbeat2 <- struct{}{}:
				default:
				}
			default:
				time.Sleep(50 * time.Millisecond) // Simulate normal work.
			}
		}
	}()

	// Supervisor: reset timer on every heartbeat.
	// If no heartbeat for 500ms, declare the worker stalled.
	const heartbeatTimeout = 500 * time.Millisecond
	timer := time.NewTimer(heartbeatTimeout)
	defer timer.Stop()

supervisorLoop:
	for {
		select {
		case <-heartbeat2:
			fmt.Println("supervisor: heartbeat OK")
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(heartbeatTimeout)
		case <-timer.C:
			fmt.Println("supervisor: ALERT — worker stalled!")
			close(done2)
			break supervisorLoop
		}
	}

	// ---------------------------------------------------------------
	// Example 3: Reusable heartbeatWorker function.
	// The function encapsulates the heartbeat machinery. The caller
	// only needs to listen for pulses and results.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 3: Reusable heartbeatWorker ===")

	done3 := make(chan struct{})

	hb, results := heartbeatWorker(done3, 200*time.Millisecond, func(i int) int {
		time.Sleep(80 * time.Millisecond) // Simulate work
		return i * i
	})

	timeout3 := time.After(1 * time.Second)
reusableLoop:
	for {
		select {
		case <-hb:
			fmt.Println("pulse")
		case val, ok := <-results:
			if !ok {
				break reusableLoop
			}
			fmt.Println("result:", val)
		case <-timeout3:
			close(done3)
			// Drain remaining results to let the goroutine exit.
			for range results {
			}
			fmt.Println("done")
			break reusableLoop
		}
	}

	// ---------------------------------------------------------------
	// Example 4: Monitoring multiple workers.
	// Launch 3 heartbeat workers and monitor all of them.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 4: Multiple monitored workers ===")

	done4 := make(chan struct{})

	type workerChannels struct {
		id        int
		heartbeat <-chan struct{}
		results   <-chan int
	}

	workers := make([]workerChannels, 3)
	for i := 0; i < 3; i++ {
		id := i
		hb, res := heartbeatWorker(done4, 150*time.Millisecond, func(j int) int {
			time.Sleep(70 * time.Millisecond)
			return id*100 + j
		})
		workers[i] = workerChannels{id: id, heartbeat: hb, results: res}
	}

	// Monitor all workers for 500ms.
	deadline := time.After(500 * time.Millisecond)
monitorLoop:
	for {
		select {
		case <-workers[0].heartbeat:
			fmt.Println("[worker-0] heartbeat")
		case <-workers[1].heartbeat:
			fmt.Println("[worker-1] heartbeat")
		case <-workers[2].heartbeat:
			fmt.Println("[worker-2] heartbeat")
		case val := <-workers[0].results:
			fmt.Printf("[worker-0] result: %d\n", val)
		case val := <-workers[1].results:
			fmt.Printf("[worker-1] result: %d\n", val)
		case val := <-workers[2].results:
			fmt.Printf("[worker-2] result: %d\n", val)
		case <-deadline:
			fmt.Println("monitoring period ended")
			close(done4)
			// Drain all result channels.
			for _, w := range workers {
				for range w.results {
				}
			}
			break monitorLoop
		}
	}
}
