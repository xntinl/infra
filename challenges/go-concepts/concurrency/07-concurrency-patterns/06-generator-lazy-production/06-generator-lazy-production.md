---
difficulty: intermediate
concepts: [generator pattern, lazy evaluation, channel backpressure, producer-consumer, done channel]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, channel direction, select]
---

# 6. Generator: Lazy Production

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** generator functions that return receive-only channels
- **Explain** how channel backpressure drives lazy evaluation
- **Create** a paginated data source that produces pages on demand
- **Apply** the done-channel pattern to prevent goroutine leaks from generators

## Why Generators

A generator is a function that returns a channel and produces values in a background goroutine. The consumer drives the pace: if the consumer stops reading, the generator blocks on its send. This is lazy evaluation through backpressure -- values are produced only as fast as they are consumed.

Consider a real scenario: your application queries a database that returns 10,000 records, but the user only views the first 2 pages (50 records). Loading all 10,000 records upfront wastes database bandwidth, memory, and time. A paginated generator fetches one page at a time, on demand. If the user stops scrolling after page 2, the generator never fetches pages 3-200. This is the same principle behind database cursors and iterator patterns, implemented naturally with Go channels.

The key insight is that an unbuffered channel naturally synchronizes the producer and consumer. The producer only runs when the consumer is ready to receive. This makes generators memory-efficient even for huge datasets -- only one page exists in memory at a time.

```
  Paginated Database Generator

  generator() returns <-chan Page immediately
  Background goroutine fetches pages lazily:

  goroutine:  [fetch page 1] -> [send] -> [block] -> [fetch page 2] -> [send] -> [block]
  consumer:                  <- [recv]              <- [recv]         <- [stop]

  Only fetches what the consumer requests. No wasted work.
```

## Step 1 -- Basic Paginated Generator

Create a generator that simulates fetching database pages on demand.

```go
package main

import (
	"fmt"
	"time"
)

type Record struct {
	ID   int
	Name string
}

type Page struct {
	Number  int
	Records []Record
}

func fetchPages(totalRecords, pageSize int) <-chan Page {
	out := make(chan Page)
	go func() {
		pageNum := 1
		for offset := 0; offset < totalRecords; offset += pageSize {
			end := offset + pageSize
			if end > totalRecords {
				end = totalRecords
			}

			// Simulate database query latency
			time.Sleep(50 * time.Millisecond)

			records := make([]Record, 0, end-offset)
			for i := offset; i < end; i++ {
				records = append(records, Record{
					ID:   i + 1,
					Name: fmt.Sprintf("record_%04d", i+1),
				})
			}

			out <- Page{Number: pageNum, Records: records}
			pageNum++
		}
		close(out)
	}()
	return out
}

func main() {
	fmt.Println("=== Paginated Generator (fetch all) ===")
	start := time.Now()
	pages := fetchPages(50, 10)

	var totalRecords int
	for page := range pages {
		totalRecords += len(page.Records)
		fmt.Printf("  Page %d: %d records (first: %s, last: %s)\n",
			page.Number, len(page.Records),
			page.Records[0].Name,
			page.Records[len(page.Records)-1].Name)
	}
	fmt.Printf("  Total: %d records fetched in %v\n", totalRecords, time.Since(start))
}
```

The function returns immediately with the channel. Pages are fetched lazily as the consumer reads.

### Intermediate Verification
```bash
go run main.go
```
Expected:
```
=== Paginated Generator (fetch all) ===
  Page 1: 10 records (first: record_0001, last: record_0010)
  Page 2: 10 records (first: record_0011, last: record_0020)
  Page 3: 10 records (first: record_0021, last: record_0030)
  Page 4: 10 records (first: record_0031, last: record_0040)
  Page 5: 10 records (first: record_0041, last: record_0050)
  Total: 50 records fetched in 252ms
```

## Step 2 -- Early Cancellation with Done Channel

Show memory efficiency: the consumer only needs 2 pages out of 100. Without cancellation the generator goroutine leaks. Fix it with a done channel.

```go
package main

import (
	"fmt"
	"time"
)

type Record struct {
	ID   int
	Name string
}

type Page struct {
	Number  int
	Records []Record
}

func fetchPagesWithCancel(done <-chan struct{}, totalRecords, pageSize int) <-chan Page {
	out := make(chan Page)
	go func() {
		defer close(out)
		pageNum := 1
		for offset := 0; offset < totalRecords; offset += pageSize {
			end := offset + pageSize
			if end > totalRecords {
				end = totalRecords
			}

			time.Sleep(50 * time.Millisecond)
			fmt.Printf("    [generator] fetched page %d from DB\n", pageNum)

			records := make([]Record, 0, end-offset)
			for i := offset; i < end; i++ {
				records = append(records, Record{
					ID:   i + 1,
					Name: fmt.Sprintf("record_%04d", i+1),
				})
			}

			select {
			case out <- Page{Number: pageNum, Records: records}:
				pageNum++
			case <-done:
				fmt.Printf("    [generator] canceled at page %d, stopping early\n", pageNum)
				return
			}
		}
		fmt.Println("    [generator] all pages sent")
	}()
	return out
}

func main() {
	// Scenario 1: Load everything (wasteful)
	fmt.Println("=== Without Early Stop: Fetch All 100 Pages ===")
	start := time.Now()
	done1 := make(chan struct{})
	pages1 := fetchPagesWithCancel(done1, 1000, 10)
	var count1 int
	for range pages1 {
		count1++
	}
	close(done1)
	fmt.Printf("  Fetched %d pages in %v\n\n", count1, time.Since(start))

	// Scenario 2: Only need first 3 pages (efficient)
	fmt.Println("=== With Early Stop: Only Need 3 Pages ===")
	start = time.Now()
	done2 := make(chan struct{})
	pages2 := fetchPagesWithCancel(done2, 1000, 10)

	var count2 int
	for page := range pages2 {
		count2++
		fmt.Printf("  Consumer got page %d (%d records)\n", page.Number, len(page.Records))
		if count2 >= 3 {
			close(done2) // signal generator to stop
			break
		}
	}
	fmt.Printf("  Fetched only %d pages in %v (saved 97 unnecessary queries)\n",
		count2, time.Since(start))
}
```

The `select` statement lets the goroutine listen for both "consumer wants a page" and "consumer is done". Closing the `done` channel unblocks the `<-done` case and the goroutine exits cleanly.

### Intermediate Verification
```bash
go run main.go
```
Expected: the second scenario is dramatically faster:
```
=== Without Early Stop: Fetch All 100 Pages ===
    [generator] fetched page 1 from DB
    ...
    [generator] fetched page 100 from DB
    [generator] all pages sent
  Fetched 100 pages in 5.1s

=== With Early Stop: Only Need 3 Pages ===
    [generator] fetched page 1 from DB
  Consumer got page 1 (10 records)
    [generator] fetched page 2 from DB
  Consumer got page 2 (10 records)
    [generator] fetched page 3 from DB
  Consumer got page 3 (10 records)
    [generator] canceled at page 4, stopping early
  Fetched only 3 pages in 153ms (saved 97 unnecessary queries)
```

## Step 3 -- Memory Comparison: Lazy vs Eager

Demonstrate the memory difference between loading all data upfront and using a lazy generator.

```go
package main

import (
	"fmt"
	"time"
)

type Record struct {
	ID   int
	Name string
	Data [256]byte // simulate a record with some payload
}

func main() {
	totalRecords := 10000
	pageSize := 100
	pagesNeeded := 3

	// Eager: load everything into memory
	fmt.Println("=== Eager Loading ===")
	start := time.Now()
	allRecords := make([]Record, totalRecords)
	for i := range allRecords {
		allRecords[i] = Record{ID: i + 1, Name: fmt.Sprintf("rec_%d", i+1)}
	}
	// Only use first 3 pages
	used := allRecords[:pagesNeeded*pageSize]
	fmt.Printf("  Allocated %d records (%d KB)\n", len(allRecords), len(allRecords)*280/1024)
	fmt.Printf("  Actually used: %d records\n", len(used))
	fmt.Printf("  Wasted: %d records (%d KB)\n",
		len(allRecords)-len(used),
		(len(allRecords)-len(used))*280/1024)
	fmt.Printf("  Time: %v\n\n", time.Since(start))

	// Lazy: generate pages on demand
	fmt.Println("=== Lazy Generator ===")
	start = time.Now()
	done := make(chan struct{})
	pages := func() <-chan []Record {
		out := make(chan []Record)
		go func() {
			defer close(out)
			for offset := 0; offset < totalRecords; offset += pageSize {
				end := offset + pageSize
				if end > totalRecords {
					end = totalRecords
				}
				page := make([]Record, end-offset)
				for i := range page {
					page[i] = Record{ID: offset + i + 1, Name: fmt.Sprintf("rec_%d", offset+i+1)}
				}
				select {
				case out <- page:
				case <-done:
					return
				}
			}
		}()
		return out
	}()

	var lazyUsed int
	pageCount := 0
	for page := range pages {
		pageCount++
		lazyUsed += len(page)
		if pageCount >= pagesNeeded {
			close(done)
			break
		}
	}
	fmt.Printf("  Allocated only %d records at a time (%d KB per page)\n",
		pageSize, pageSize*280/1024)
	fmt.Printf("  Total records processed: %d\n", lazyUsed)
	fmt.Printf("  Pages fetched: %d out of %d possible\n", pageCount, totalRecords/pageSize)
	fmt.Printf("  Time: %v\n", time.Since(start))
}
```

### Intermediate Verification
```bash
go run main.go
```
```
=== Eager Loading ===
  Allocated 10000 records (2734 KB)
  Actually used: 300 records
  Wasted: 9700 records (2652 KB)
  Time: 5.2ms

=== Lazy Generator ===
  Allocated only 100 records at a time (27 KB per page)
  Total records processed: 300
  Pages fetched: 3 out of 100 possible
  Time: 0.4ms
```

## Step 4 -- Composable Generator with Context

Use `context.Context` instead of a raw done channel for production-ready cancellation.

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type Record struct {
	ID   int
	Name string
}

type Page struct {
	Number  int
	Records []Record
	HasMore bool
}

func queryPages(ctx context.Context, query string, pageSize int) <-chan Page {
	out := make(chan Page)
	go func() {
		defer close(out)
		totalResults := 85 // simulate: query found 85 matching records
		pageNum := 1
		for offset := 0; offset < totalResults; offset += pageSize {
			end := offset + pageSize
			if end > totalResults {
				end = totalResults
			}

			time.Sleep(30 * time.Millisecond) // DB latency

			records := make([]Record, 0, end-offset)
			for i := offset; i < end; i++ {
				records = append(records, Record{
					ID:   i + 1,
					Name: fmt.Sprintf("match_%d_for_%s", i+1, query),
				})
			}

			select {
			case out <- Page{
				Number:  pageNum,
				Records: records,
				HasMore: end < totalResults,
			}:
				pageNum++
			case <-ctx.Done():
				fmt.Printf("    [query] canceled: %v\n", ctx.Err())
				return
			}
		}
	}()
	return out
}

func main() {
	fmt.Println("=== Paginated Query with Context ===\n")

	// Cancel after 2 pages using a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	pages := queryPages(ctx, "laptop", 25)
	for page := range pages {
		fmt.Printf("  Page %d: %d results (hasMore: %v)\n",
			page.Number, len(page.Records), page.HasMore)
	}
	fmt.Println("\n  Generator stopped cleanly when context expired.")
}
```

### Intermediate Verification
```bash
go run main.go
```
```
=== Paginated Query with Context ===

  Page 1: 25 results (hasMore: true)
  Page 2: 25 results (hasMore: true)
    [query] canceled: context deadline exceeded

  Generator stopped cleanly when context expired.
```

## Common Mistakes

### Goroutine Leak from Generators Without Cancellation
**Wrong:**
```go
pages := fetchPages(10000, 10)
firstPage := <-pages
// goroutine inside fetchPages() is stuck on send forever
```
**What happens:** The goroutine never exits. In a long-running service, this accumulates leaked goroutines.

**Fix:** Always use a `done` channel or `context.Context` with generators so you can signal the producer to stop.

### Buffering the Generator Channel
**Wrong:**
```go
out := make(chan Page, 100) // pre-produces 100 pages
```
**What happens:** The generator eagerly produces 100 pages before any consumer reads. This wastes memory and database connections, defeating laziness.

**Fix:** Use unbuffered channels for true lazy evaluation. Only buffer when you have measured a performance need.

### Closing a Channel Twice
**Wrong:**
```go
go func() {
	for i := 0; i < 10; i++ {
		out <- page
	}
	close(out)
	// ... later, done channel triggers
	close(out) // panic: close of closed channel
}()
```
**Fix:** Use `defer close(out)` once, and structure the goroutine to have a single exit path.

## Verify What You Learned

Run `go run main.go` and verify:
- Paginated generator fetches all 5 pages when consumed completely
- Early cancellation stops after 3 pages, avoiding 97 unnecessary database queries
- Lazy loading uses dramatically less memory than eager loading for partial consumption
- Context-based cancellation works with timeouts and explicit cancel

## What's Next
Continue to [07-or-channel-first-to-finish](../07-or-channel-first-to-finish/07-or-channel-first-to-finish.md) to learn how to race multiple goroutines and take the first result.

## Summary
- A generator is a function that returns `<-chan T` and produces values in a background goroutine
- Unbuffered channels provide natural backpressure: pages are fetched lazily on demand
- Generators are memory-efficient even for huge datasets -- only one page exists at a time
- Always provide a cancellation mechanism (`done` channel or context) for generators
- Lazy evaluation avoids wasting database queries, memory, and time when consumers need only a subset
- The pattern maps directly to real database cursors, API pagination, and file streaming

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs) -- generators and multiplexing
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Go Blog: Context](https://go.dev/blog/context)
