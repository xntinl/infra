# 12. Multi-Stage Pipeline Cancellation

<!--
difficulty: advanced
concepts: [pipeline-cancellation, stage-context, fan-out-fan-in, errgroup, cascading-shutdown]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [context-withcancel, context-propagation, select-statement-basics, done-channel-pattern]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [11 - Graceful Shutdown with Context](../11-graceful-shutdown-with-context/11-graceful-shutdown-with-context.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how context cancellation flows through multi-stage data pipelines
- **Implement** pipeline stages that clean up and stop when any stage fails
- **Apply** `errgroup` for coordinated cancellation across pipeline goroutines
- **Design** fan-out/fan-in pipelines with proper error propagation

## The Problem

Data processing pipelines chain multiple stages: read from source, transform, filter, aggregate, write to sink. Each stage runs in its own goroutine, connected by channels. When something goes wrong -- a downstream stage encounters bad data, an upstream source disconnects, or the user cancels the operation -- every stage in the pipeline must stop promptly.

Without proper cancellation, failed pipelines leak goroutines. A producer keeps generating data that nobody consumes. A transformer blocks on a send to a channel that nobody reads. These leaks accumulate in long-running services.

Your task: build a multi-stage pipeline where cancellation at any stage propagates to all others, with clean resource cleanup and error reporting.

## Step 1 -- Basic Pipeline with Context

```bash
mkdir -p ~/go-exercises/pipeline-cancel && cd ~/go-exercises/pipeline-cancel
go mod init pipeline-cancel
```

Create `main.go` with a three-stage pipeline:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

// Stage 1: Generate numbers
func generate(ctx context.Context, start, count int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := start; i < start+count; i++ {
			select {
			case <-ctx.Done():
				fmt.Println("  generate: cancelled")
				return
			case out <- i:
				time.Sleep(50 * time.Millisecond)
			}
		}
		fmt.Println("  generate: finished")
	}()
	return out
}

// Stage 2: Double values
func double(ctx context.Context, in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			select {
			case <-ctx.Done():
				fmt.Println("  double: cancelled")
				return
			case out <- v * 2:
			}
		}
		fmt.Println("  double: finished")
	}()
	return out
}

// Stage 3: Consume and print
func consume(ctx context.Context, in <-chan int) int {
	count := 0
	for v := range in {
		select {
		case <-ctx.Done():
			fmt.Println("  consume: cancelled")
			return count
		default:
			fmt.Printf("  consumed: %d\n", v)
			count++
		}
	}
	return count
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	nums := generate(ctx, 1, 20)
	doubled := double(ctx, nums)
	count := consume(ctx, doubled)

	fmt.Printf("processed %d items before timeout\n", count)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (approximately 5-6 items before 300ms timeout):

```
  consumed: 2
  consumed: 4
  consumed: 6
  consumed: 8
  consumed: 10
  generate: cancelled
  double: finished
processed 5 items before timeout
```

## Step 2 -- Error in a Stage Cancels the Pipeline

When a stage encounters an error, it should cancel the entire pipeline:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func generate(ctx context.Context) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := 1; ; i++ {
			select {
			case <-ctx.Done():
				fmt.Println("  generate: stopped")
				return
			case out <- i:
				time.Sleep(30 * time.Millisecond)
			}
		}
	}()
	return out
}

func validate(ctx context.Context, cancel context.CancelFunc, in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			if v == 5 {
				fmt.Println("  validate: error on value 5, cancelling pipeline")
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				fmt.Println("  validate: stopped")
				return
			case out <- v:
			}
		}
	}()
	return out
}

func sink(ctx context.Context, in <-chan int) {
	for v := range in {
		fmt.Printf("  sink: %d\n", v)
	}
	fmt.Println("  sink: channel closed, done")
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nums := generate(ctx)
	valid := validate(ctx, cancel, nums)
	sink(ctx, valid)

	time.Sleep(100 * time.Millisecond) // let goroutines finish
	fmt.Println("pipeline done:", ctx.Err())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
  sink: 1
  sink: 2
  sink: 3
  sink: 4
  validate: error on value 5, cancelling pipeline
  sink: channel closed, done
  generate: stopped
pipeline done: context canceled
```

## Step 3 -- Using errgroup for Pipeline Coordination

`golang.org/x/sync/errgroup` provides coordinated cancellation: if any goroutine returns an error, all others are cancelled:

```bash
go get golang.org/x/sync/errgroup
```

```go
package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	// Channel between stages
	stage1Out := make(chan int, 10)
	stage2Out := make(chan string, 10)

	// Stage 1: Producer
	g.Go(func() error {
		defer close(stage1Out)
		for i := 1; i <= 20; i++ {
			select {
			case <-ctx.Done():
				fmt.Println("producer: cancelled")
				return ctx.Err()
			case stage1Out <- i:
				time.Sleep(50 * time.Millisecond)
			}
		}
		fmt.Println("producer: finished")
		return nil
	})

	// Stage 2: Transformer (fails on value 8)
	g.Go(func() error {
		defer close(stage2Out)
		for v := range stage1Out {
			if v == 8 {
				return fmt.Errorf("transform error: invalid value %d", v)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case stage2Out <- fmt.Sprintf("item-%d", v*10):
			}
		}
		return nil
	})

	// Stage 3: Consumer
	g.Go(func() error {
		for s := range stage2Out {
			fmt.Println(" ", s)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		fmt.Println("pipeline error:", err)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
  item-10
  item-20
  item-30
  item-40
  item-50
  item-60
  item-70
producer: cancelled
pipeline error: transform error: invalid value 8
```

## Step 4 -- Fan-Out Fan-In with Cancellation

Build a pipeline with parallel workers in the middle stage:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func fanOut(ctx context.Context, in <-chan int, workers int) <-chan string {
	out := make(chan string)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for v := range in {
				select {
				case <-ctx.Done():
					return
				default:
					time.Sleep(100 * time.Millisecond) // simulate work
					result := fmt.Sprintf("worker-%d processed %d", id, v)
					select {
					case out <- result:
					case <-ctx.Done():
						return
					}
				}
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	source := make(chan int, 5)

	// Producer
	g.Go(func() error {
		defer close(source)
		for i := 1; i <= 30; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case source <- i:
			}
		}
		return nil
	})

	// Fan-out to 3 workers
	results := fanOut(ctx, source, 3)

	// Consumer with a limit
	g.Go(func() error {
		count := 0
		for r := range results {
			fmt.Println(r)
			count++
			if count >= 10 {
				return fmt.Errorf("consumer: reached limit of 10")
			}
		}
		return nil
	})

	err := g.Wait()
	fmt.Println("result:", err)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: 10 processed items and then cancellation propagates to producer and all workers.

## Step 5 -- Design Your Pipeline

Design a four-stage pipeline for processing log entries:
1. **Reader**: reads log lines from a simulated source
2. **Parser**: parses each line into a structured record (fails on malformed input)
3. **Enricher**: adds metadata (fan-out to 3 workers for parallelism)
4. **Writer**: writes enriched records to a simulated sink

Requirements:
- If the parser encounters a malformed line, cancel the entire pipeline
- If the writer is slow, backpressure propagates upstream naturally via channel blocking
- Context timeout cancels everything after 2 seconds
- All goroutines exit cleanly (no leaks)

<details>
<summary>Hint: Pipeline Structure</summary>

```go
g, ctx := errgroup.WithContext(ctx)

lines := reader(ctx)        // <-chan string
records := parser(ctx, lines) // <-chan Record, returns error on malformed
enriched := enricher(ctx, records, 3) // <-chan Record, fan-out to 3
g.Go(func() error { return writer(ctx, enriched) })

if err := g.Wait(); err != nil { ... }
```

Each stage should be its own function that launches goroutines and returns an output channel. The `errgroup` coordinates cancellation.
</details>

## Common Mistakes

### Not Closing Output Channels

Each stage must `close(out)` when it finishes. Otherwise, the next stage blocks on `range` forever.

### Closing a Channel in the Wrong Goroutine

Only the sending goroutine should close a channel. In fan-out patterns, use a `sync.WaitGroup` to close the output channel after all workers finish.

### Ignoring ctx.Done() in Channel Sends

A stage that blocks on `out <- value` without a `select` on `ctx.Done()` may hang when the consumer stops reading.

## Verify What You Learned

Build the four-stage log processing pipeline from Step 5. Verify:
1. Normal operation processes all log lines
2. A malformed line cancels the entire pipeline
3. A 2-second timeout cancels a slow pipeline
4. `runtime.NumGoroutine()` returns to 1 after the pipeline completes

## What's Next

Continue to [13 - Context Leak Detection](../13-context-leak-detection/13-context-leak-detection.md) to learn how to detect context leaks in your applications.

## Summary

- Every pipeline stage must check `ctx.Done()` in both receives and sends
- `errgroup.WithContext` cancels all goroutines when any one returns an error
- Fan-out workers share the same context; use `sync.WaitGroup` to close the merge channel
- Close output channels with `defer close(out)` at the start of each stage goroutine
- Pipeline backpressure happens naturally through channel blocking
- Always verify zero goroutine leaks after pipeline completion

## Reference

- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [errgroup package](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [Advanced Go Concurrency Patterns](https://go.dev/talks/2013/advconc.slide)
