package main

// Exercise: End-to-End Pipeline with Cancellation
// Instructions: see 10-end-to-end-pipeline-with-cancel.md

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// Step 1: Data types for the pipeline.
type Record struct {
	ID   int
	Data string
}

type ProcessedRecord struct {
	Record   Record
	Result   string
	Stage    string
	Error    error
	Duration time.Duration
}

// Step 2: Implement generateRecords.
// Produces Record values and sends them to a channel.
// Respects context cancellation.
func generateRecords(ctx context.Context, count int) <-chan Record {
	out := make(chan Record)
	go func() {
		defer close(out)
		for i := 1; i <= count; i++ {
			record := Record{ID: i, Data: fmt.Sprintf("record-%d", i)}
			// TODO: select on out <- record and ctx.Done()
			// TODO: on cancellation, print message and return
			_ = record
			_ = ctx
		}
	}()
	return out
}

// Step 3: Implement validate stage.
// Reads Records, validates them (ID%7==0 is invalid), sends ProcessedRecord.
// Respects context cancellation on sends.
func validate(ctx context.Context, in <-chan Record) <-chan ProcessedRecord {
	out := make(chan ProcessedRecord)
	go func() {
		defer close(out)
		for record := range in {
			start := time.Now()
			var result ProcessedRecord

			// TODO: if record.ID%7 == 0, create error result
			// TODO: else, sleep 10ms (simulate work), create success result
			// TODO: select on out <- result and ctx.Done()
			_ = start
			_ = result
			_ = record
		}
	}()
	return out
}

// Step 3 continued: Implement transform stage.
// Reads ProcessedRecords, transforms successful ones, forwards errors unchanged.
// Respects context cancellation.
func transform(ctx context.Context, in <-chan ProcessedRecord) <-chan ProcessedRecord {
	out := make(chan ProcessedRecord)
	go func() {
		defer close(out)
		for pr := range in {
			// TODO: if pr.Error != nil, forward as-is (with select + ctx.Done)
			// TODO: else, sleep 20ms, create transformed result, send with select
			_ = pr
			_ = ctx
		}
	}()
	return out
}

// Step 4: Implement mergeProcessed (fan-in for ProcessedRecord channels).
func mergeProcessed(ctx context.Context, channels ...<-chan ProcessedRecord) <-chan ProcessedRecord {
	out := make(chan ProcessedRecord)
	var wg sync.WaitGroup

	// TODO: for each channel, launch a forwarding goroutine
	// TODO: each forwarder uses select with ctx.Done()
	// TODO: closer goroutine: wg.Wait() then close(out)
	_ = ctx
	_ = wg

	return out
}

// Step 4 continued: Implement fanOutTransform.
// Creates numWorkers transform stages all reading from the same input.
// Merges their outputs.
func fanOutTransform(ctx context.Context, in <-chan ProcessedRecord, numWorkers int) <-chan ProcessedRecord {
	// TODO: create slice of worker output channels
	// TODO: launch numWorkers transform stages sharing `in`
	// TODO: return mergeProcessed(ctx, workers...)
	_ = numWorkers
	return transform(ctx, in) // replace with fan-out implementation
}

// Step 5: Implement collect.
// Drains the pipeline output and separates successes from errors.
func collect(in <-chan ProcessedRecord) (successes []ProcessedRecord, errors []ProcessedRecord) {
	// TODO: range over in, append to successes or errors based on pr.Error
	return
}

// Step 6: Implement runPipeline.
// Wires all stages together, optionally cancels after N results,
// and checks for goroutine leaks.
func runPipeline(totalRecords int, cancelAfter int) {
	fmt.Printf("=== Pipeline: %d records", totalRecords)
	if cancelAfter > 0 {
		fmt.Printf(", cancel after %d", cancelAfter)
	}
	fmt.Println(" ===")

	goroutinesBefore := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// TODO: build pipeline: generateRecords -> validate -> fanOutTransform(3)
	_ = ctx // remove once you use ctx in generateRecords/validate/fanOutTransform

	if cancelAfter > 0 {
		// TODO: consume up to cancelAfter results, then cancel
		// TODO: sleep briefly to let cancellation propagate
	} else {
		// TODO: collect all results
		// TODO: print success count, error count
		// TODO: print each error
	}

	// Goroutine leak check
	time.Sleep(100 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()
	leaked := goroutinesAfter - goroutinesBefore
	if leaked > 0 {
		fmt.Printf("  WARNING: %d goroutine(s) may have leaked (before=%d, after=%d)\n",
			leaked, goroutinesBefore, goroutinesAfter)
	} else {
		fmt.Printf("  OK: no goroutine leaks (before=%d, after=%d)\n",
			goroutinesBefore, goroutinesAfter)
	}
	fmt.Println()
}

// Verify: Add a rate-limiting stage and run with 50 records.
func rateLimitStage(ctx context.Context, in <-chan ProcessedRecord, ratePerSecond int) <-chan ProcessedRecord {
	out := make(chan ProcessedRecord)
	// TODO: create a ticker at the appropriate interval
	// TODO: for each value from in, wait for tick, then forward
	// TODO: respect ctx.Done()
	go func() {
		defer close(out)
		_ = ratePerSecond
		_ = ctx
	}()
	return out
}

func main() {
	fmt.Println("Exercise: End-to-End Pipeline with Cancellation\n")

	// Full pipeline run
	runPipeline(30, 0)

	// Pipeline with cancellation after 10 results
	runPipeline(30, 10)

	// Verify: rate-limited pipeline (uncomment after implementing)
	// fmt.Println("=== Verify: Rate-Limited Pipeline ===")
	// start := time.Now()
	// runRateLimitedPipeline(50, 20) // 50 records at 20/sec
	// fmt.Printf("  Total time: %v (expected ~2.5s at 20/sec)\n", time.Since(start))
}
