package main

// End-to-End Pipeline with Cancellation -- Complete Working Example
//
// Capstone exercise combining all patterns: generator, fan-out, fan-in,
// error propagation, context cancellation, rate limiting, and goroutine
// leak prevention. This is the architecture behind real ETL pipelines,
// stream processors, and HTTP request handlers.
//
// Expected output:
//   === Pipeline: 30 records ===
//     Successes: 26, Errors: 4
//     error: record 7 failed validation
//     error: record 14 failed validation
//     error: record 21 failed validation
//     error: record 28 failed validation
//     OK: no goroutine leaks
//
//   === Pipeline: 30 records, cancel after 10 ===
//     [pipeline] canceling after 10 results
//     [generator] canceled at record X
//     OK: no goroutine leaks
//
//   === Rate-Limited Pipeline (20/sec, 50 records) ===
//     Total time: ~2.5s

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Data types: typed records flowing through the pipeline.
// Every ProcessedRecord carries the original record, the processing result,
// which stage produced it, any error, and timing info. This traceability
// is critical for debugging production pipelines.
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// generateRecords: the pipeline source.
// Produces Record values and respects context cancellation on every send.
// Every blocking operation (channel send) must check ctx.Done() -- this
// is how cancellation propagates through the pipeline.
// ---------------------------------------------------------------------------

func generateRecords(ctx context.Context, count int) <-chan Record {
	out := make(chan Record)
	go func() {
		defer close(out)
		for i := 1; i <= count; i++ {
			record := Record{ID: i, Data: fmt.Sprintf("record-%d", i)}
			select {
			case out <- record:
				// Value delivered to next stage.
			case <-ctx.Done():
				fmt.Printf("  [generator] canceled at record %d: %v\n", i, ctx.Err())
				return
			}
		}
	}()
	return out
}

// ---------------------------------------------------------------------------
// validate: first processing stage.
// Reads records, validates them (ID%7==0 is "invalid"), sends
// ProcessedRecord downstream. Errors are wrapped in the record and
// forwarded -- they are NOT dropped. The consumer decides what to do.
// ---------------------------------------------------------------------------

func validate(ctx context.Context, in <-chan Record) <-chan ProcessedRecord {
	out := make(chan ProcessedRecord)
	go func() {
		defer close(out)
		for record := range in {
			start := time.Now()
			var result ProcessedRecord

			if record.ID%7 == 0 {
				// Validation failure: record the error but don't stop the pipeline.
				result = ProcessedRecord{
					Record:   record,
					Stage:    "validate",
					Error:    fmt.Errorf("record %d failed validation", record.ID),
					Duration: time.Since(start),
				}
			} else {
				time.Sleep(10 * time.Millisecond) // Simulate validation work.
				result = ProcessedRecord{
					Record:   record,
					Result:   fmt.Sprintf("valid(%s)", record.Data),
					Stage:    "validate",
					Duration: time.Since(start),
				}
			}

			select {
			case out <- result:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// ---------------------------------------------------------------------------
// transform: second processing stage.
// Reads ProcessedRecords, transforms successful ones, forwards errors
// unchanged. This "pass-through errors" pattern ensures errors propagate
// through the entire pipeline without being silently dropped.
// ---------------------------------------------------------------------------

func transform(ctx context.Context, in <-chan ProcessedRecord) <-chan ProcessedRecord {
	out := make(chan ProcessedRecord)
	go func() {
		defer close(out)
		for pr := range in {
			// Errors pass through unchanged -- never swallow errors.
			if pr.Error != nil {
				select {
				case out <- pr:
				case <-ctx.Done():
					return
				}
				continue
			}

			start := time.Now()
			time.Sleep(20 * time.Millisecond) // Heavier processing.

			result := ProcessedRecord{
				Record:   pr.Record,
				Result:   fmt.Sprintf("transformed(%s)", pr.Result),
				Stage:    "transform",
				Duration: time.Since(start),
			}

			select {
			case out <- result:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// ---------------------------------------------------------------------------
// mergeProcessed: fan-in for ProcessedRecord channels.
// One forwarder per input channel, WaitGroup + closer goroutine.
// Every forwarder checks ctx.Done() to exit on cancellation.
// ---------------------------------------------------------------------------

func mergeProcessed(ctx context.Context, channels ...<-chan ProcessedRecord) <-chan ProcessedRecord {
	out := make(chan ProcessedRecord)
	var wg sync.WaitGroup

	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan ProcessedRecord) {
			defer wg.Done()
			for pr := range c {
				select {
				case out <- pr:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// ---------------------------------------------------------------------------
// fanOutTransform: parallelizes the transform stage.
// Creates numWorkers transform stages sharing the same input.
// Workers compete for input values (fan-out), and their outputs
// are merged (fan-in). This is the scatter-gather pattern.
//
//   validated records
//         |
//   +-----+-----+-----+
//   |     |     |     |
//  xfm0  xfm1  xfm2  (fan-out: share input)
//   |     |     |     |
//   +-----+-----+-----+
//         |
//   merged output        (fan-in: merge outputs)
// ---------------------------------------------------------------------------

func fanOutTransform(ctx context.Context, in <-chan ProcessedRecord, numWorkers int) <-chan ProcessedRecord {
	workers := make([]<-chan ProcessedRecord, numWorkers)
	for i := 0; i < numWorkers; i++ {
		workers[i] = transform(ctx, in)
	}
	return mergeProcessed(ctx, workers...)
}

// ---------------------------------------------------------------------------
// collect: drains the pipeline output and separates successes from errors.
// ---------------------------------------------------------------------------

func collect(in <-chan ProcessedRecord) (successes, errors []ProcessedRecord) {
	for pr := range in {
		if pr.Error != nil {
			errors = append(errors, pr)
		} else {
			successes = append(successes, pr)
		}
	}
	return
}

// ---------------------------------------------------------------------------
// rateLimitStage: applies token-bucket rate limiting to the pipeline.
// Waits for a tick before forwarding each record. Respects cancellation.
// ---------------------------------------------------------------------------

func rateLimitStage(ctx context.Context, in <-chan ProcessedRecord, ratePerSecond int) <-chan ProcessedRecord {
	out := make(chan ProcessedRecord)
	go func() {
		defer close(out)
		interval := time.Second / time.Duration(ratePerSecond)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for pr := range in {
			// Wait for the next tick (rate gate).
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
			// Forward the record.
			select {
			case out <- pr:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// ---------------------------------------------------------------------------
// runPipeline: wires all stages together.
//
//   generateRecords -> validate -> fanOutTransform(3) -> collect/cancel
//
// After completion (or cancellation), checks for goroutine leaks using
// runtime.NumGoroutine(). A leak means some goroutine did not exit --
// usually a missing ctx.Done() check or an unclosed channel.
// ---------------------------------------------------------------------------

func runPipeline(totalRecords int, cancelAfter int) {
	fmt.Printf("=== Pipeline: %d records", totalRecords)
	if cancelAfter > 0 {
		fmt.Printf(", cancel after %d", cancelAfter)
	}
	fmt.Println(" ===")

	goroutinesBefore := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build pipeline: source -> validate -> parallel transform.
	records := generateRecords(ctx, totalRecords)
	validated := validate(ctx, records)
	transformed := fanOutTransform(ctx, validated, 3)

	if cancelAfter > 0 {
		// Consume up to cancelAfter results, then cancel the pipeline.
		count := 0
		for range transformed {
			count++
			if count >= cancelAfter {
				fmt.Printf("  [pipeline] canceling after %d results\n", cancelAfter)
				cancel()
				break
			}
		}
		// Let cancellation propagate through all stages.
		time.Sleep(100 * time.Millisecond)
	} else {
		// Full run: collect all results.
		successes, errors := collect(transformed)
		fmt.Printf("  Successes: %d, Errors: %d\n", len(successes), len(errors))
		for _, e := range errors {
			fmt.Printf("    error: %v\n", e.Error)
		}
	}

	// Goroutine leak check: after cancellation and a settling delay,
	// the goroutine count should return to baseline.
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

// ---------------------------------------------------------------------------
// runRateLimitedPipeline: adds a rate limiter between validate and transform.
// Demonstrates composing patterns: pipeline + fan-out + rate limiting.
// ---------------------------------------------------------------------------

func runRateLimitedPipeline(totalRecords, ratePerSecond int) {
	fmt.Printf("=== Rate-Limited Pipeline (%d/sec, %d records) ===\n", ratePerSecond, totalRecords)

	goroutinesBefore := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pipeline: generate -> validate -> rate-limit -> parallel transform.
	records := generateRecords(ctx, totalRecords)
	validated := validate(ctx, records)
	limited := rateLimitStage(ctx, validated, ratePerSecond)
	transformed := fanOutTransform(ctx, limited, 3)

	successes, errors := collect(transformed)
	fmt.Printf("  Successes: %d, Errors: %d\n", len(successes), len(errors))

	// Leak check
	time.Sleep(100 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()
	leaked := goroutinesAfter - goroutinesBefore
	if leaked > 0 {
		fmt.Printf("  WARNING: %d goroutine(s) may have leaked\n", leaked)
	} else {
		fmt.Printf("  OK: no goroutine leaks\n")
	}
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: End-to-End Pipeline with Cancellation")
	fmt.Println()

	// Full pipeline run: all 30 records processed.
	runPipeline(30, 0)

	// Pipeline with cancellation after 10 results.
	runPipeline(30, 10)

	// Rate-limited pipeline: 50 records at 20/sec.
	start := time.Now()
	runRateLimitedPipeline(50, 20)
	fmt.Printf("  Total time: %v (expected ~2.5s at 20/sec)\n", time.Since(start).Truncate(time.Millisecond))
}
