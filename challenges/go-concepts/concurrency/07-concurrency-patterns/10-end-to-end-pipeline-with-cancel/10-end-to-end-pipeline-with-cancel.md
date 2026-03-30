---
difficulty: advanced
concepts: [pipeline, context cancellation, error handling, goroutine leak prevention, graceful shutdown]
tools: [go]
estimated_time: 45m
bloom_level: create
prerequisites: [goroutines, channels, select, context, sync.WaitGroup, pipeline, fan-out, fan-in, worker pool]
---

# 10. End-to-End Pipeline with Cancellation

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a complete multi-stage pipeline with context-driven cancellation
- **Handle** errors that propagate through pipeline stages without being dropped
- **Prevent** goroutine leaks by ensuring all goroutines exit on cancellation
- **Verify** goroutine cleanup with runtime.NumGoroutine
- **Combine** multiple concurrency patterns into a production-quality system

## Why End-to-End Pipelines

Real-world concurrent systems are not single patterns -- they are compositions. A production pipeline combines generators, fan-out for parallelism, fan-in for aggregation, error handling, and cancellation. The challenge is not any single pattern but making them all work together correctly.

Consider a real scenario: your team builds a CSV data import system. Users upload large CSV files (100K+ rows) containing customer records. Each record must be validated (format checks, required fields), enriched (lookup external data like company info from an API), and written to the database. The user can cancel the import at any time. When they do, the system must stop reading new records, let in-flight records finish processing, report how many records were imported successfully, and clean up all goroutines.

This exercise is the capstone of the concurrency patterns section. You will combine pipeline + worker pool + context cancellation + error propagation into one system.

```
  CSV Import Pipeline

  readCSV(ctx) --> validate(ctx) --> enrich(ctx, 3 workers) --> writeToDB(ctx) --> report
       |                |                   |                       |
    reads CSV       validates each    3 parallel workers       writes to DB
    line by line    record, marks     enrich with external     (simulated)
    (cancelable)    errors            data, merge outputs

  Context cancellation propagates through ALL stages.
  Errors flow as data (ImportResult.Error), never silently dropped.
  On cancel: stop reading, drain in-flight, report progress.
```

## Step 1 -- Define the Pipeline Data Types

Start with clear types for the data flowing through the pipeline. Every record carries enough context to trace it through all stages.

```go
package main

import (
	"context"
	"fmt"
)

type CSVRecord struct {
	LineNum int
	Fields  map[string]string
}

type ValidatedRecord struct {
	Record CSVRecord
	Valid  bool
	Error  string
}

type EnrichedRecord struct {
	Record      CSVRecord
	CompanyInfo string
	Valid       bool
	Error       string
	WorkerID    int
}

type ImportResult struct {
	LineNum int
	Status  string // "imported", "skipped", "error"
	Detail  string
}

func main() {
	ctx := context.Background()
	_ = ctx
	fmt.Println("Pipeline types defined. Ready to build stages.")
}
```

Each stage transforms one type into the next. Errors are carried as data inside the struct, not as separate error returns. This prevents silent error swallowing and lets the final collector see every outcome.

## Step 2 -- Build the CSV Reader Stage

Create a generator that reads CSV records and respects context cancellation. When the user cancels the import, this stage stops emitting records.

```go
package main

import (
	"context"
	"fmt"
)

type CSVRecord struct {
	LineNum int
	Fields  map[string]string
}

func readCSV(ctx context.Context, data []map[string]string) <-chan CSVRecord {
	out := make(chan CSVRecord)
	go func() {
		defer close(out)
		for i, row := range data {
			record := CSVRecord{LineNum: i + 1, Fields: row}
			select {
			case out <- record:
			case <-ctx.Done():
				fmt.Printf("  [reader] canceled at line %d: %v\n", i+1, ctx.Err())
				return
			}
		}
		fmt.Printf("  [reader] all %d records emitted\n", len(data))
	}()
	return out
}

func main() {
	data := []map[string]string{
		{"name": "Alice Johnson", "email": "alice@acme.com", "company": "Acme Corp"},
		{"name": "Bob Smith", "email": "bob@widgets.io", "company": "Widgets Inc"},
		{"name": "Charlie Brown", "email": "charlie@example.com", "company": "Example LLC"},
	}

	ctx := context.Background()
	records := readCSV(ctx, data)

	fmt.Println("=== CSV Reader Test ===")
	for r := range records {
		fmt.Printf("  Line %d: %s (%s)\n", r.LineNum, r.Fields["name"], r.Fields["email"])
	}
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected:
```
=== CSV Reader Test ===
  Line 1: Alice Johnson (alice@acme.com)
  Line 2: Bob Smith (bob@widgets.io)
  Line 3: Charlie Brown (charlie@example.com)
  [reader] all 3 records emitted
```

## Step 3 -- Build Validation and Enrichment Stages

Create two processing stages: validate checks required fields, enrich simulates looking up company data from an external API.

```go
package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type CSVRecord struct {
	LineNum int
	Fields  map[string]string
}

type ValidatedRecord struct {
	Record CSVRecord
	Valid  bool
	Error  string
}

type EnrichedRecord struct {
	Record      CSVRecord
	CompanyInfo string
	Valid       bool
	Error       string
	WorkerID    int
}

func validate(ctx context.Context, in <-chan CSVRecord) <-chan ValidatedRecord {
	out := make(chan ValidatedRecord)
	go func() {
		defer close(out)
		for record := range in {
			result := ValidatedRecord{Record: record, Valid: true}

			// Check required fields
			name := strings.TrimSpace(record.Fields["name"])
			email := strings.TrimSpace(record.Fields["email"])

			if name == "" {
				result.Valid = false
				result.Error = "missing required field: name"
			} else if email == "" {
				result.Valid = false
				result.Error = "missing required field: email"
			} else if !strings.Contains(email, "@") {
				result.Valid = false
				result.Error = fmt.Sprintf("invalid email format: %s", email)
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

func enrich(ctx context.Context, id int, in <-chan ValidatedRecord) <-chan EnrichedRecord {
	out := make(chan EnrichedRecord)
	go func() {
		defer close(out)
		for vr := range in {
			if !vr.Valid {
				// Forward invalid records without enrichment
				select {
				case out <- EnrichedRecord{
					Record: vr.Record, Valid: false,
					Error: vr.Error, WorkerID: id,
				}:
				case <-ctx.Done():
					return
				}
				continue
			}

			// Simulate external API call for company data
			time.Sleep(30 * time.Millisecond)
			company := vr.Record.Fields["company"]
			companyInfo := fmt.Sprintf("%s (verified, 50 employees)", company)

			select {
			case out <- EnrichedRecord{
				Record: vr.Record, CompanyInfo: companyInfo,
				Valid: true, WorkerID: id,
			}:
			case <-ctx.Done():
				fmt.Printf("  [enricher %d] canceled during enrichment\n", id)
				return
			}
		}
	}()
	return out
}

func main() {
	ctx := context.Background()
	csvData := []CSVRecord{
		{1, map[string]string{"name": "Alice", "email": "alice@acme.com", "company": "Acme"}},
		{2, map[string]string{"name": "", "email": "bob@widgets.io", "company": "Widgets"}},
		{3, map[string]string{"name": "Charlie", "email": "invalid-email", "company": "Ex"}},
		{4, map[string]string{"name": "Diana", "email": "diana@corp.com", "company": "Corp"}},
	}

	in := make(chan CSVRecord)
	go func() {
		for _, r := range csvData {
			in <- r
		}
		close(in)
	}()

	validated := validate(ctx, in)
	enriched := enrich(ctx, 1, validated)

	fmt.Println("=== Validate + Enrich Test ===")
	for er := range enriched {
		if !er.Valid {
			fmt.Printf("  Line %d: INVALID - %s\n", er.Record.LineNum, er.Error)
		} else {
			fmt.Printf("  Line %d: OK - %s [%s]\n",
				er.Record.LineNum, er.Record.Fields["name"], er.CompanyInfo)
		}
	}
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: line 1 and 4 pass, lines 2 and 3 fail validation:
```
=== Validate + Enrich Test ===
  Line 1: OK - Alice [Acme (verified, 50 employees)]
  Line 2: INVALID - missing required field: name
  Line 3: INVALID - invalid email format: invalid-email
  Line 4: OK - Diana [Corp (verified, 50 employees)]
```

## Step 4 -- Fan-Out Enrichment with Worker Pool

Parallelize the enrichment stage (the bottleneck, since it calls an external API) with multiple workers and merge their outputs.

```go
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type CSVRecord struct {
	LineNum int
	Fields  map[string]string
}

type ValidatedRecord struct {
	Record CSVRecord
	Valid  bool
	Error  string
}

type EnrichedRecord struct {
	Record      CSVRecord
	CompanyInfo string
	Valid       bool
	Error       string
	WorkerID    int
}

func validate(ctx context.Context, in <-chan CSVRecord) <-chan ValidatedRecord {
	out := make(chan ValidatedRecord)
	go func() {
		defer close(out)
		for record := range in {
			result := ValidatedRecord{Record: record, Valid: true}
			name := strings.TrimSpace(record.Fields["name"])
			email := strings.TrimSpace(record.Fields["email"])
			if name == "" {
				result.Valid = false
				result.Error = "missing required field: name"
			} else if !strings.Contains(email, "@") {
				result.Valid = false
				result.Error = fmt.Sprintf("invalid email: %s", email)
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

func enrich(ctx context.Context, id int, in <-chan ValidatedRecord) <-chan EnrichedRecord {
	out := make(chan EnrichedRecord)
	go func() {
		defer close(out)
		for vr := range in {
			if !vr.Valid {
				select {
				case out <- EnrichedRecord{Record: vr.Record, Valid: false, Error: vr.Error, WorkerID: id}:
				case <-ctx.Done():
					return
				}
				continue
			}
			time.Sleep(50 * time.Millisecond) // external API call
			select {
			case out <- EnrichedRecord{
				Record: vr.Record, Valid: true, WorkerID: id,
				CompanyInfo: fmt.Sprintf("%s (verified)", vr.Record.Fields["company"]),
			}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func fanOutEnrich(ctx context.Context, in <-chan ValidatedRecord, numWorkers int) <-chan EnrichedRecord {
	workers := make([]<-chan EnrichedRecord, numWorkers)
	for i := 0; i < numWorkers; i++ {
		workers[i] = enrich(ctx, i+1, in)
	}

	out := make(chan EnrichedRecord)
	var wg sync.WaitGroup
	for _, ch := range workers {
		wg.Add(1)
		go func(c <-chan EnrichedRecord) {
			defer wg.Done()
			for er := range c {
				select {
				case out <- er:
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

func main() {
	ctx := context.Background()

	// Generate 15 CSV records
	in := make(chan CSVRecord)
	go func() {
		defer close(in)
		names := []string{"Alice", "Bob", "", "Diana", "Eve", "Frank", "Grace",
			"Henry", "Ivy", "", "Kate", "Leo", "Mia", "Noah", "Olivia"}
		for i, name := range names {
			in <- CSVRecord{
				LineNum: i + 1,
				Fields: map[string]string{
					"name":    name,
					"email":   fmt.Sprintf("%s@company.com", strings.ToLower(name)),
					"company": fmt.Sprintf("Company_%d", i+1),
				},
			}
		}
	}()

	fmt.Println("=== Fan-Out Enrichment (3 workers, 15 records) ===\n")
	start := time.Now()

	validated := validate(ctx, in)
	enriched := fanOutEnrich(ctx, validated, 3)

	var imported, skipped int
	for er := range enriched {
		if !er.Valid {
			skipped++
			fmt.Printf("  Line %2d: SKIP  - %s (worker %d)\n", er.Record.LineNum, er.Error, er.WorkerID)
		} else {
			imported++
			fmt.Printf("  Line %2d: OK    - %s [%s] (worker %d)\n",
				er.Record.LineNum, er.Record.Fields["name"], er.CompanyInfo, er.WorkerID)
		}
	}

	fmt.Printf("\n  Imported: %d, Skipped: %d, Total: %v\n", imported, skipped, time.Since(start))
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: 13 imported, 2 skipped, distributed across 3 workers.

## Step 5 -- Full Pipeline with Cancellation and Progress Reporting

Build the complete system: CSV reader -> validate -> fan-out enrich -> write to DB -> report. Support cancellation via context and verify zero goroutine leaks.

```go
package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

type CSVRecord struct {
	LineNum int
	Fields  map[string]string
}

type ValidatedRecord struct {
	Record CSVRecord
	Valid  bool
	Error  string
}

type EnrichedRecord struct {
	Record      CSVRecord
	CompanyInfo string
	Valid       bool
	Error       string
	WorkerID    int
}

type ImportResult struct {
	LineNum int
	Status  string
	Detail  string
}

func readCSV(ctx context.Context, data []map[string]string) <-chan CSVRecord {
	out := make(chan CSVRecord)
	go func() {
		defer close(out)
		for i, row := range data {
			record := CSVRecord{LineNum: i + 1, Fields: row}
			select {
			case out <- record:
			case <-ctx.Done():
				fmt.Printf("  [reader] stopped at line %d\n", i+1)
				return
			}
		}
	}()
	return out
}

func validate(ctx context.Context, in <-chan CSVRecord) <-chan ValidatedRecord {
	out := make(chan ValidatedRecord)
	go func() {
		defer close(out)
		for record := range in {
			result := ValidatedRecord{Record: record, Valid: true}
			name := strings.TrimSpace(record.Fields["name"])
			email := strings.TrimSpace(record.Fields["email"])
			if name == "" {
				result.Valid = false
				result.Error = "missing name"
			} else if !strings.Contains(email, "@") {
				result.Valid = false
				result.Error = "invalid email"
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

func enrich(ctx context.Context, id int, in <-chan ValidatedRecord) <-chan EnrichedRecord {
	out := make(chan EnrichedRecord)
	go func() {
		defer close(out)
		for vr := range in {
			if !vr.Valid {
				select {
				case out <- EnrichedRecord{Record: vr.Record, Valid: false, Error: vr.Error, WorkerID: id}:
				case <-ctx.Done():
					return
				}
				continue
			}
			select {
			case <-time.After(30 * time.Millisecond): // simulate API call
			case <-ctx.Done():
				return
			}
			select {
			case out <- EnrichedRecord{
				Record: vr.Record, Valid: true, WorkerID: id,
				CompanyInfo: vr.Record.Fields["company"] + " (verified)",
			}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func fanOutEnrich(ctx context.Context, in <-chan ValidatedRecord, n int) <-chan EnrichedRecord {
	workers := make([]<-chan EnrichedRecord, n)
	for i := 0; i < n; i++ {
		workers[i] = enrich(ctx, i+1, in)
	}
	out := make(chan EnrichedRecord)
	var wg sync.WaitGroup
	for _, ch := range workers {
		wg.Add(1)
		go func(c <-chan EnrichedRecord) {
			defer wg.Done()
			for er := range c {
				select {
				case out <- er:
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

func writeToDB(ctx context.Context, in <-chan EnrichedRecord) <-chan ImportResult {
	out := make(chan ImportResult)
	go func() {
		defer close(out)
		for er := range in {
			if !er.Valid {
				select {
				case out <- ImportResult{
					LineNum: er.Record.LineNum,
					Status:  "skipped",
					Detail:  er.Error,
				}:
				case <-ctx.Done():
					return
				}
				continue
			}
			time.Sleep(5 * time.Millisecond) // simulate DB write
			select {
			case out <- ImportResult{
				LineNum: er.Record.LineNum,
				Status:  "imported",
				Detail:  fmt.Sprintf("%s -> %s", er.Record.Fields["name"], er.CompanyInfo),
			}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func main() {
	goroutinesBefore := runtime.NumGoroutine()

	// Generate test CSV data
	csvData := make([]map[string]string, 30)
	names := []string{
		"Alice", "Bob", "", "Diana", "Eve", "Frank", "Grace", "Henry",
		"Ivy", "", "Kate", "Leo", "Mia", "Noah", "Olivia", "Pete",
		"Quinn", "Rose", "", "Sam", "Tina", "Uma", "Vic", "Wendy",
		"Xander", "Yara", "Zoe", "", "Aaron", "Beth",
	}
	for i := 0; i < 30; i++ {
		name := names[i]
		email := ""
		if name != "" {
			email = strings.ToLower(name) + "@company.com"
		}
		csvData[i] = map[string]string{
			"name":    name,
			"email":   email,
			"company": fmt.Sprintf("Corp_%d", i+1),
		}
	}

	// ========== Run 1: Complete import (no cancellation) ==========
	fmt.Println("=== Complete Import (30 records) ===\n")
	ctx1 := context.Background()
	start := time.Now()

	records := readCSV(ctx1, csvData)
	validated := validate(ctx1, records)
	enriched := fanOutEnrich(ctx1, validated, 3)
	results := writeToDB(ctx1, enriched)

	var imported, skipped int
	for r := range results {
		switch r.Status {
		case "imported":
			imported++
		case "skipped":
			skipped++
			fmt.Printf("  Line %2d: SKIP - %s\n", r.LineNum, r.Detail)
		}
	}

	fmt.Printf("\n  Imported: %d, Skipped: %d\n", imported, skipped)
	fmt.Printf("  Completed in %v\n", time.Since(start))

	// Check for goroutine leaks
	time.Sleep(100 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()
	leaked := goroutinesAfter - goroutinesBefore
	if leaked > 0 {
		fmt.Printf("  WARNING: %d goroutine(s) leaked\n", leaked)
	} else {
		fmt.Printf("  Goroutines: OK (before=%d, after=%d)\n\n", goroutinesBefore, goroutinesAfter)
	}

	// ========== Run 2: User cancels import after 100ms ==========
	fmt.Println("=== Canceled Import (user cancels after 100ms) ===\n")
	ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start = time.Now()

	records2 := readCSV(ctx2, csvData)
	validated2 := validate(ctx2, records2)
	enriched2 := fanOutEnrich(ctx2, validated2, 3)
	results2 := writeToDB(ctx2, enriched2)

	var cancelImported, cancelSkipped int
	for r := range results2 {
		switch r.Status {
		case "imported":
			cancelImported++
		case "skipped":
			cancelSkipped++
		}
	}

	fmt.Printf("  Import canceled after %v\n", time.Since(start))
	fmt.Printf("  Partial results: %d imported, %d skipped\n", cancelImported, cancelSkipped)
	fmt.Printf("  (Remaining records were NOT processed -- clean shutdown)\n")

	// Verify cleanup
	time.Sleep(100 * time.Millisecond)
	goroutinesAfter = runtime.NumGoroutine()
	leaked = goroutinesAfter - goroutinesBefore
	if leaked > 0 {
		fmt.Printf("  WARNING: %d goroutine(s) leaked after cancel\n", leaked)
	} else {
		fmt.Printf("  Goroutines: OK after cancel (before=%d, after=%d)\n",
			goroutinesBefore, goroutinesAfter)
	}
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected:
```
=== Complete Import (30 records) ===

  Line  3: SKIP - missing name
  Line 10: SKIP - missing name
  Line 19: SKIP - missing name
  Line 28: SKIP - missing name

  Imported: 26, Skipped: 4
  Completed in 350ms
  Goroutines: OK (before=2, after=2)

=== Canceled Import (user cancels after 100ms) ===

  [reader] stopped at line 12
  Import canceled after 102ms
  Partial results: 8 imported, 1 skipped
  (Remaining records were NOT processed -- clean shutdown)
  Goroutines: OK after cancel (before=2, after=2)
```

## Common Mistakes

### Not Checking ctx.Done() in Every Stage
**Wrong:**
```go
for record := range in {
	out <- processRecord(record) // no cancellation check
}
```
**What happens:** Even after cancel is called, the stage continues processing until the input channel closes, wasting CPU.

**Fix:** Always wrap sends in `select { case out <- result: case <-ctx.Done(): return }`.

### Goroutine Leak from Unclosed Channels
**Wrong:**
```go
func stage(in <-chan CSVRecord) <-chan CSVRecord {
	out := make(chan CSVRecord)
	go func() {
		for r := range in {
			out <- r
		}
		// forgot close(out)
	}()
	return out
}
```
**What happens:** Downstream stages that `range` over this output block forever.

**Fix:** Always `defer close(out)` at the top of the goroutine.

### Not Propagating Errors Through the Pipeline
**Wrong:** Dropping errors silently.
```go
if err != nil {
	continue // error swallowed, consumer never knows
}
```
**Fix:** Wrap errors in the result struct and forward them. Let the final collector decide how to handle errors.

### Using runtime.NumGoroutine Without a Settling Delay
After canceling, goroutines need a moment to respond to ctx.Done() and exit. Check the count after a brief sleep to avoid false positive leak reports.

## Verify What You Learned

Run `go run main.go` and verify:
- Complete import: 26 imported, 4 skipped (records with empty names), zero goroutine leaks
- Canceled import: partial results reported, pipeline stopped cleanly, zero goroutine leaks
- Every stage respects context cancellation
- Errors flow through the pipeline as data, not swallowed silently

## What's Next
Congratulations on completing the concurrency patterns section. You now have the building blocks for any concurrent Go system: pipelines, fan-out/fan-in, worker pools, semaphores, generators, or-channels, tee-channels, rate limiters, and end-to-end composition with cancellation. Continue to [08-errgroup](../../08-errgroup/) to learn the `errgroup` package for structured error handling in concurrent operations.

## Summary
- Production pipelines combine generators, fan-out, fan-in, error handling, and cancellation
- Every goroutine must check `ctx.Done()` on every blocking operation (send, receive)
- Errors should flow through the pipeline as data, not be silently dropped
- Use `runtime.NumGoroutine()` to verify zero goroutine leaks after shutdown
- Always `defer close(out)` in every stage goroutine
- Context cancellation propagates through all stages when any part signals stop
- The pipeline pattern scales: add stages, parallelize bottlenecks, compose patterns freely
- Real-world CSV import demonstrates all patterns working together in one system

## Reference
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Go Blog: Context](https://go.dev/blog/context)
- [Concurrency in Go (Katherine Cox-Buday)](https://www.oreilly.com/library/view/concurrency-in-go/) -- comprehensive pattern reference
- [Advanced Go Concurrency Patterns](https://www.youtube.com/watch?v=QDDwwePbDtw)
