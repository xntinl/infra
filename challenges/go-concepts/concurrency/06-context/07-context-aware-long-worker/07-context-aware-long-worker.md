---
difficulty: advanced
concepts: [cancellation in loops, partial results, cooperative cancellation, chunked processing, progress reporting]
tools: [go]
estimated_time: 35m
bloom_level: analyze
---

# 7. Context-Aware Long Worker

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a report generator that processes large datasets in chunks with cancellation support
- **Check** context between chunks to support graceful mid-operation cancellation
- **Return** partial results when a long-running operation is cancelled
- **Design** a file processing task that respects context deadlines

## Why Context-Aware Workers

Real systems have long-running operations that process thousands or millions of records: generating monthly revenue reports, processing uploaded CSV files, running data migrations, exporting analytics. These operations can take minutes or hours. Without context awareness, they cannot be stopped cleanly -- killing them abruptly risks leaving data in an inconsistent state.

A context-aware worker checks `ctx.Done()` at natural checkpoints: between chunks of records, between files, after each page of results. This gives the worker a chance to finish its current unit of work, save progress, and exit cleanly. When a deployment rolls out (SIGTERM), when an admin cancels a report, or when a deadline approaches, the worker responds promptly without data corruption.

The key design decision: what happens to partial results? A report generator might return the 3,000 rows it processed before cancellation. A file processor might commit the chunks it completed. The mechanism is always the same -- check context between units -- but the partial result strategy depends on your domain.

## Step 1 -- Report Generator with Chunked Processing

Build a report generator that processes sales records in chunks. Between each chunk, it checks whether it should stop:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type ReportResult struct {
	TotalRecords   int
	ProcessedChunks int
	TotalChunks    int
	Revenue        float64
	Complete       bool
	CancelReason   string
}

func generateSalesReport(ctx context.Context, totalRecords int, chunkSize int) ReportResult {
	totalChunks := (totalRecords + chunkSize - 1) / chunkSize
	result := ReportResult{TotalChunks: totalChunks}

	fmt.Printf("[report] starting: %d records in %d chunks of %d\n",
		totalRecords, totalChunks, chunkSize)

	for chunk := 0; chunk < totalChunks; chunk++ {
		// Check context BEFORE processing each chunk.
		select {
		case <-ctx.Done():
			result.CancelReason = ctx.Err().Error()
			fmt.Printf("[report] cancelled at chunk %d/%d: %v\n",
				chunk, totalChunks, ctx.Err())
			return result
		default:
		}

		// Process this chunk.
		recordsInChunk := chunkSize
		remaining := totalRecords - result.TotalRecords
		if remaining < chunkSize {
			recordsInChunk = remaining
		}

		fmt.Printf("[report] processing chunk %d/%d (%d records)...\n",
			chunk+1, totalChunks, recordsInChunk)
		time.Sleep(100 * time.Millisecond) // Simulate processing.

		result.TotalRecords += recordsInChunk
		result.ProcessedChunks++
		result.Revenue += float64(recordsInChunk) * 49.99
	}

	result.Complete = true
	return result
}

func main() {
	// Cancel after 350ms. With 100ms per chunk, only ~3 chunks will complete.
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	result := generateSalesReport(ctx, 5000, 1000) // 5 chunks of 1000

	fmt.Println("\n=== Report Result ===")
	fmt.Printf("Complete:   %v\n", result.Complete)
	fmt.Printf("Processed:  %d/%d chunks\n", result.ProcessedChunks, result.TotalChunks)
	fmt.Printf("Records:    %d/5000\n", result.TotalRecords)
	fmt.Printf("Revenue:    $%.2f\n", result.Revenue)
	if result.CancelReason != "" {
		fmt.Printf("Cancelled:  %s\n", result.CancelReason)
		fmt.Println("Action:     partial report saved, can resume from chunk",
			result.ProcessedChunks+1)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[report] starting: 5000 records in 5 chunks of 1000
[report] processing chunk 1/5 (1000 records)...
[report] processing chunk 2/5 (1000 records)...
[report] processing chunk 3/5 (1000 records)...
[report] cancelled at chunk 3/5: context deadline exceeded

=== Report Result ===
Complete:   false
Processed:  3/5 chunks
Records:    3000/5000
Revenue:    $149970.00
Cancelled:  context deadline exceeded
Action:     partial report saved, can resume from chunk 4
```

The report processed 3 chunks before the deadline. The result contains the partial data (3000 records, partial revenue), and the caller knows exactly where to resume. No data is corrupted because the check happens between complete chunks.

## Step 2 -- File Processor that Respects Deadlines

Build a processor that reads and transforms files from a directory. Each file is a complete unit of work -- cancellation happens between files, not mid-file:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type FileResult struct {
	Name    string
	Size    int
	Status  string
}

func processFile(ctx context.Context, name string, sizeMB int) FileResult {
	processTime := time.Duration(sizeMB*20) * time.Millisecond

	// Check deadline before starting. Skip files that cannot finish in time.
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < processTime {
			return FileResult{
				Name:   name,
				Size:   sizeMB,
				Status: fmt.Sprintf("skipped (needs %v, only %v left)", processTime, remaining.Round(time.Millisecond)),
			}
		}
	}

	fmt.Printf("[processor] processing %s (%dMB, ~%v)\n", name, sizeMB, processTime)

	select {
	case <-time.After(processTime):
		return FileResult{Name: name, Size: sizeMB, Status: "completed"}
	case <-ctx.Done():
		return FileResult{Name: name, Size: sizeMB, Status: fmt.Sprintf("interrupted: %v", ctx.Err())}
	}
}

func processDirectory(ctx context.Context, files []struct{ name string; sizeMB int }) []FileResult {
	var results []FileResult

	for _, f := range files {
		// Check cancellation between files.
		select {
		case <-ctx.Done():
			// Mark remaining files as skipped.
			for _, remaining := range files[len(results):] {
				results = append(results, FileResult{
					Name:   remaining.name,
					Size:   remaining.sizeMB,
					Status: "not started (context cancelled)",
				})
			}
			return results
		default:
		}

		result := processFile(ctx, f.name, f.sizeMB)
		results = append(results, result)
	}

	return results
}

func main() {
	files := []struct{ name string; sizeMB int }{
		{"report-2024-01.csv", 5},   // 100ms
		{"report-2024-02.csv", 3},   // 60ms
		{"report-2024-03.csv", 8},   // 160ms
		{"transactions.csv", 15},     // 300ms -- too large for remaining budget
		{"summary.csv", 2},           // 40ms
	}

	// 350ms budget for all files.
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	fmt.Println("=== File Processing (budget: 350ms) ===\n")
	results := processDirectory(ctx, files)

	fmt.Println("\n=== Results ===")
	for _, r := range results {
		fmt.Printf("  %-25s %3dMB  %s\n", r.Name, r.Size, r.Status)
	}

	completed := 0
	for _, r := range results {
		if r.Status == "completed" {
			completed++
		}
	}
	fmt.Printf("\nProcessed: %d/%d files\n", completed, len(files))
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== File Processing (budget: 350ms) ===

[processor] processing report-2024-01.csv (5MB, ~100ms)
[processor] processing report-2024-02.csv (3MB, ~60ms)
[processor] processing report-2024-03.csv (8MB, ~160ms)

=== Results ===
  report-2024-01.csv        5MB  completed
  report-2024-02.csv        3MB  completed
  report-2024-03.csv        8MB  completed
  transactions.csv          15MB  skipped (needs 300ms, only 29ms left)
  summary.csv                2MB  not started (context cancelled)

Processed: 3/5 files
```

The processor completed 3 files, then detected that the 4th file (15MB, needs 300ms) could not finish within the remaining budget and skipped it proactively. The 5th file was never started because the context expired.

## Step 3 -- Progress Reporting for Long Operations

Build a data export that reports progress, allowing the caller to monitor completion or cancel when they have enough data:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type ExportProgress struct {
	Phase       string
	PagesExported int
	TotalPages    int
	BytesWritten  int
	Done          bool
	Err           error
}

func exportUserData(ctx context.Context, totalPages int, progress chan<- ExportProgress) {
	defer close(progress)

	for page := 1; page <= totalPages; page++ {
		select {
		case <-ctx.Done():
			progress <- ExportProgress{
				Phase:         "cancelled",
				PagesExported: page - 1,
				TotalPages:    totalPages,
				BytesWritten:  (page - 1) * 4096,
				Done:          true,
				Err:           ctx.Err(),
			}
			return
		default:
		}

		progress <- ExportProgress{
			Phase:         "exporting",
			PagesExported: page,
			TotalPages:    totalPages,
			BytesWritten:  page * 4096,
		}

		time.Sleep(80 * time.Millisecond) // Simulate page export.
	}

	progress <- ExportProgress{
		Phase:         "complete",
		PagesExported: totalPages,
		TotalPages:    totalPages,
		BytesWritten:  totalPages * 4096,
		Done:          true,
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	progress := make(chan ExportProgress)
	go exportUserData(ctx, 10, progress)

	fmt.Println("=== Data Export Progress ===\n")
	for p := range progress {
		if p.Done {
			if p.Err != nil {
				fmt.Printf("\nExport cancelled at page %d/%d (%d bytes written)\n",
					p.PagesExported, p.TotalPages, p.BytesWritten)
				fmt.Printf("Reason: %v\n", p.Err)
				fmt.Println("Partial export file is valid and can be downloaded.")
			} else {
				fmt.Printf("\nExport complete: %d pages, %d bytes\n",
					p.PagesExported, p.BytesWritten)
			}
			break
		}
		pct := float64(p.PagesExported) / float64(p.TotalPages) * 100
		fmt.Printf("  [%3.0f%%] page %d/%d exported (%d bytes)\n",
			pct, p.PagesExported, p.TotalPages, p.BytesWritten)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== Data Export Progress ===

  [ 10%] page 1/10 exported (4096 bytes)
  [ 20%] page 2/10 exported (8192 bytes)
  [ 30%] page 3/10 exported (12288 bytes)
  [ 40%] page 4/10 exported (16384 bytes)

Export cancelled at page 4/10 (16384 bytes written)
Reason: context deadline exceeded
Partial export file is valid and can be downloaded.
```

The progress channel lets the caller show a progress bar, log completion status, or decide to cancel early if partial data is sufficient.

## Step 4 -- Atomic Units: Finish Current Record Before Stopping

Sometimes each record has multiple steps that must complete together (like a database transaction). Check cancellation between records, but once a record starts, run all its steps to completion:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type MigrationResult struct {
	Migrated []string
	Pending  []string
	Error    error
}

func migrateRecord(recordID string) {
	fmt.Printf("    validate %s\n", recordID)
	time.Sleep(30 * time.Millisecond)
	fmt.Printf("    transform %s\n", recordID)
	time.Sleep(30 * time.Millisecond)
	fmt.Printf("    write %s\n", recordID)
	time.Sleep(30 * time.Millisecond)
}

func runMigration(ctx context.Context, records []string) MigrationResult {
	result := MigrationResult{}

	for i, record := range records {
		// Check cancellation BETWEEN records.
		select {
		case <-ctx.Done():
			result.Pending = records[i:]
			result.Error = ctx.Err()
			return result
		default:
		}

		// Once started, a record runs to completion (all 3 steps).
		fmt.Printf("[migration] record %d/%d: %s\n", i+1, len(records), record)
		migrateRecord(record)
		result.Migrated = append(result.Migrated, record)
		fmt.Printf("[migration] record %s committed\n\n", record)
	}

	return result
}

func main() {
	records := []string{
		"user-001", "user-002", "user-003",
		"user-004", "user-005",
	}

	// 250ms budget. Each record takes ~90ms (3 steps * 30ms).
	// Should complete 2 records, cancel before 3rd.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	fmt.Println("=== Data Migration (budget: 250ms, ~90ms per record) ===\n")
	result := runMigration(ctx, records)

	fmt.Println("=== Migration Result ===")
	fmt.Printf("Migrated: %v\n", result.Migrated)
	fmt.Printf("Pending:  %v\n", result.Pending)
	if result.Error != nil {
		fmt.Printf("Stopped:  %v\n", result.Error)
		fmt.Println("\nNo data corruption: each migrated record completed all 3 steps.")
		fmt.Printf("Resume migration from record: %s\n", result.Pending[0])
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== Data Migration (budget: 250ms, ~90ms per record) ===

[migration] record 1/5: user-001
    validate user-001
    transform user-001
    write user-001
[migration] record user-001 committed

[migration] record 2/5: user-002
    validate user-002
    transform user-002
    write user-002
[migration] record user-002 committed

=== Migration Result ===
Migrated: [user-001 user-002]
Pending:  [user-003 user-004 user-005]
Stopped:  context deadline exceeded

No data corruption: each migrated record completed all 3 steps.
Resume migration from record: user-003
```

Each migrated record completed all three steps (validate, transform, write) atomically. No record is left half-processed. The caller knows exactly which records were migrated and where to resume.

## Common Mistakes

### Checking ctx.Done() Only at the Start
**Wrong:**
```go
for _, item := range items {
    select {
    case <-ctx.Done():
        return
    default:
    }
    veryLongOperation(item) // runs for minutes -- no cancellation check inside
}
```
**Fix:** Check `ctx.Done()` at multiple points within long operations, or break them into smaller steps. A worker that only checks at the top of each iteration is unresponsive to cancellation during the work phase.

### Blocking on Channel Send After Cancellation
**Wrong:**
```go
case <-ctx.Done():
    results <- partialResult // blocks forever if nobody is reading!
```
**Fix:**
```go
case <-ctx.Done():
    select {
    case results <- partialResult:
    default: // drop if nobody is listening
    }
```

### Not Returning After Cancellation
**Wrong:**
```go
select {
case <-ctx.Done():
    fmt.Println("cancelled")
    // falls through to continue working!
}
doMoreWork() // this still runs
```
**Fix:** Always `return` after handling cancellation. The `select` case does not break out of the surrounding loop or function.

### Using default in a Select with ctx.Done() and a Channel
**Caution:** When you have both `ctx.Done()` and a work channel in a select, adding a `default` case creates a busy loop that wastes CPU:
```go
select {
case <-ctx.Done(): return
case job := <-jobs: process(job)
default: // THIS SPINS THE CPU when both channels are empty!
}
```
Only use `default` in the non-blocking check pattern (the top-of-loop check in Step 1).

## Verify What You Learned

Build a batch processor that transforms 10 records, each taking 60ms. Use a 400ms timeout. Track which records were processed and which were skipped:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type BatchResult struct {
	Processed []string
	Skipped   []string
	Reason    string
}

func batchTransform(ctx context.Context, records []string) BatchResult {
	var processed []string
	for i, record := range records {
		select {
		case <-ctx.Done():
			return BatchResult{
				Processed: processed,
				Skipped:   records[i:],
				Reason:    ctx.Err().Error(),
			}
		default:
		}
		time.Sleep(60 * time.Millisecond)
		processed = append(processed, fmt.Sprintf("%s:transformed", record))
	}
	return BatchResult{Processed: processed, Reason: "completed"}
}

func main() {
	records := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10"}

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	result := batchTransform(ctx, records)
	fmt.Printf("Processed: %v\n", result.Processed)
	fmt.Printf("Skipped:   %v\n", result.Skipped)
	fmt.Printf("Reason:    %s\n", result.Reason)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Processed: [r1:transformed r2:transformed r3:transformed r4:transformed r5:transformed r6:transformed]
Skipped:   [r7 r8 r9 r10]
Reason:    context deadline exceeded
```

## What's Next
Continue to [08-graceful-shutdown-with-context](../08-graceful-shutdown-with-context/08-graceful-shutdown-with-context.md) to build a complete server shutdown sequence that drains in-flight requests, closes connections, and flushes logs.

## Summary
- Check `ctx.Done()` at natural checkpoints: between chunks, between files, between records
- Return partial results with enough information for the caller to resume
- For multi-step records, check cancellation between records but finish steps atomically
- Use the fail-fast pattern: check deadline before starting work that cannot finish in time
- Report progress through a channel so callers can monitor long-running operations
- Always `return` after handling cancellation -- do not fall through to more work
- Avoid `default` in selects with work channels -- it creates busy loops

## Reference
- [Go Blog: Pipelines](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [Package context](https://pkg.go.dev/context)
