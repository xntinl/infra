---
difficulty: intermediate
concepts: [error channels, result structs, panic recovery, concurrent error collection, partial failures, graceful degradation]
tools: [go]
estimated_time: 30m
bloom_level: apply
---

# 11. Goroutine Error Handling

## Learning Objectives
After completing this exercise, you will be able to:
- **Collect** errors from multiple goroutines using a dedicated error channel
- **Design** result structs that carry both data and error information
- **Recover** from panics inside goroutines using `defer/recover` to prevent process crashes
- **Combine** all three patterns to build resilient concurrent operations

## Why Goroutine Error Handling Matters

In sequential code, error handling is straightforward: call a function, check the error, decide what to do. In concurrent code, multiple goroutines can fail simultaneously, and each failure must be captured without losing the others. A common production mistake is to ignore errors from goroutines, leading to silent data loss or corrupted state.

Go has no exception system, so errors do not propagate across goroutine boundaries automatically. If a goroutine encounters an error, it must explicitly send that error somewhere -- a channel, a shared struct, or a callback. If a goroutine panics and you do not recover it, the entire process crashes, killing every other in-flight goroutine.

In this exercise, you build a parallel file validator that processes multiple configuration files. Each goroutine can encounter different errors: invalid JSON, missing required fields, or permission denied. You will learn three patterns for getting errors out of goroutines, and when to use each one.

## Step 1 -- Error Channel: Collecting Errors From Workers

The simplest pattern: dedicate a channel to errors so the caller can collect every failure from every goroutine.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type ValidationError struct {
	File    string
	Message string
}

func validateConfig(filename string, errCh chan<- ValidationError, wg *sync.WaitGroup) {
	defer wg.Done()

	// Simulate file reading and validation
	time.Sleep(time.Duration(rand.Intn(30)+10) * time.Millisecond)

	switch filename {
	case "database.json":
		errCh <- ValidationError{
			File:    filename,
			Message: "invalid JSON: unexpected token at line 42",
		}
	case "auth.json":
		errCh <- ValidationError{
			File:    filename,
			Message: "missing required field: 'token_expiry'",
		}
	case "storage.json":
		errCh <- ValidationError{
			File:    filename,
			Message: "permission denied: cannot read file",
		}
	}
	// Valid files send nothing to the error channel
}

func main() {
	files := []string{
		"app.json", "database.json", "cache.json",
		"auth.json", "logging.json", "storage.json",
		"metrics.json", "network.json",
	}

	errCh := make(chan ValidationError, len(files))
	var wg sync.WaitGroup

	fmt.Println("=== Parallel Config Validator (Error Channel) ===\n")
	fmt.Printf("  Validating %d config files...\n\n", len(files))

	for _, f := range files {
		wg.Add(1)
		go validateConfig(f, errCh, &wg)
	}

	// Close error channel after all validators finish
	go func() {
		wg.Wait()
		close(errCh)
	}()

	// Collect all errors
	var errors []ValidationError
	for err := range errCh {
		errors = append(errors, err)
	}

	if len(errors) == 0 {
		fmt.Println("  All configs valid.")
	} else {
		fmt.Printf("  Found %d error(s):\n", len(errors))
		for _, e := range errors {
			fmt.Printf("    [FAIL] %-20s %s\n", e.File, e.Message)
		}
	}

	validCount := len(files) - len(errors)
	fmt.Printf("\n  Summary: %d valid, %d invalid out of %d files\n",
		validCount, len(errors), len(files))
}
```

**What's happening here:** Eight config files are validated concurrently. Each goroutine sends errors to a shared error channel. Valid files send nothing. After all goroutines complete, the error channel is closed and the caller collects every error. The WaitGroup ensures the channel is closed only after all work is done.

**Key insight:** The error channel pattern works when you only need to know what failed. It is simple and effective for fire-and-forget validation. The limitation is that you cannot easily correlate errors with successful results -- you know what broke, but you do not have a unified view of all outcomes.

**What would happen if you forgot to close the error channel?** The `for err := range errCh` loop would block forever, waiting for more errors that never come. The program would deadlock.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Parallel Config Validator (Error Channel) ===

  Validating 8 config files...

  Found 3 error(s):
    [FAIL] database.json         invalid JSON: unexpected token at line 42
    [FAIL] auth.json             missing required field: 'token_expiry'
    [FAIL] storage.json          permission denied: cannot read file

  Summary: 5 valid, 3 invalid out of 8 files
```

## Step 2 -- Result Struct: Unified Success and Error Reporting

Carry both the result and the error in a single struct. This gives the caller a complete picture of every goroutine's outcome.

```go
package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

type ConfigResult struct {
	File       string
	Valid      bool
	FieldCount int
	Warnings   []string
	Err        error
}

func validateConfigFull(filename string) ConfigResult {
	time.Sleep(time.Duration(rand.Intn(40)+10) * time.Millisecond)

	switch filename {
	case "database.json":
		return ConfigResult{
			File: filename,
			Err:  fmt.Errorf("invalid JSON: unexpected '}' at line 42"),
		}
	case "auth.json":
		return ConfigResult{
			File: filename,
			Err:  fmt.Errorf("missing required field: 'token_expiry'"),
		}
	case "cache.json":
		return ConfigResult{
			File:       filename,
			Valid:      true,
			FieldCount: 8,
			Warnings:   []string{"'ttl' is set to 0 -- cache disabled"},
		}
	case "storage.json":
		return ConfigResult{
			File: filename,
			Err:  fmt.Errorf("permission denied: file mode 0000"),
		}
	default:
		fields := rand.Intn(15) + 5
		return ConfigResult{
			File:       filename,
			Valid:      true,
			FieldCount: fields,
		}
	}
}

func main() {
	files := []string{
		"app.json", "database.json", "cache.json",
		"auth.json", "logging.json", "storage.json",
		"metrics.json", "network.json",
	}

	results := make(chan ConfigResult, len(files))

	fmt.Println("=== Parallel Config Validator (Result Struct) ===\n")

	for _, f := range files {
		go func(filename string) {
			results <- validateConfigFull(filename)
		}(f)
	}

	var valid, invalid, warned int
	fmt.Printf("  %-20s %-8s %-8s %s\n", "File", "Status", "Fields", "Details")
	fmt.Println("  " + strings.Repeat("-", 65))

	for i := 0; i < len(files); i++ {
		r := <-results

		status := "OK"
		detail := ""

		if r.Err != nil {
			status = "FAIL"
			detail = r.Err.Error()
			invalid++
		} else {
			valid++
			if len(r.Warnings) > 0 {
				status = "WARN"
				detail = r.Warnings[0]
				warned++
			}
		}

		fmt.Printf("  %-20s %-8s %-8d %s\n",
			r.File, status, r.FieldCount, detail)
	}

	fmt.Println("  " + strings.Repeat("-", 65))
	fmt.Printf("  Valid: %d | Warnings: %d | Failed: %d | Total: %d\n",
		valid, warned, invalid, len(files))
}
```

**What's happening here:** Each goroutine returns a `ConfigResult` that carries the filename, validation status, field count, warnings, AND any error. The caller gets a complete view: which files succeeded, which have warnings, and which failed. This is one unified channel for all outcomes.

**Key insight:** The result struct pattern is preferred for most production use cases because it gives a unified view of all outcomes. Unlike the error-only channel, you can report partial success (valid with warnings), compute aggregate statistics, and make decisions based on the full picture. This is the pattern used by `errgroup` and similar libraries internally.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies):
```
=== Parallel Config Validator (Result Struct) ===

  File                 Status   Fields   Details
  -----------------------------------------------------------------
  logging.json         OK       12
  cache.json           WARN     8        'ttl' is set to 0 -- cache disabled
  app.json             OK       9
  database.json        FAIL     0        invalid JSON: unexpected '}' at line 42
  auth.json            FAIL     0        missing required field: 'token_expiry'
  network.json         OK       7
  storage.json         FAIL     0        permission denied: file mode 0000
  metrics.json         OK       11
  -----------------------------------------------------------------
  Valid: 5 | Warnings: 1 | Failed: 3 | Total: 8
```

## Step 3 -- Panic Recovery: Preventing Crashes in Goroutines

Demonstrate that an unrecovered panic in any goroutine crashes the entire process, and show the `defer/recover` pattern to isolate failures.

```go
package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type ParseResult struct {
	File      string
	Parsed    bool
	KeyCount  int
	Err       error
	Panicked  bool
	PanicInfo string
}

func riskyParser(filename string) ParseResult {
	// Simulate different failure modes
	switch filename {
	case "legacy.yaml":
		// BUG: nil map access
		var config map[string]interface{}
		config["key"] = "value" // panic!
	case "broken.toml":
		// BUG: index out of range
		data := []byte{1, 2, 3}
		_ = data[100] // panic!
	case "huge.json":
		// BUG: divide by zero
		size := 0
		_ = 1024 / size // panic!
	}

	// Normal files parse successfully
	time.Sleep(20 * time.Millisecond)
	return ParseResult{
		File:     filename,
		Parsed:   true,
		KeyCount: len(filename) * 3,
	}
}

func safeParser(filename string, results chan<- ParseResult, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			results <- ParseResult{
				File:      filename,
				Panicked:  true,
				PanicInfo: fmt.Sprintf("%v", r),
				Err:       fmt.Errorf("panic: %v", r),
			}
		}
	}()

	results <- riskyParser(filename)
}

func main() {
	files := []string{
		"app.yaml", "database.json", "legacy.yaml",
		"cache.toml", "broken.toml", "auth.json",
		"huge.json", "network.yaml",
	}

	results := make(chan ParseResult, len(files))
	var wg sync.WaitGroup

	fmt.Println("=== Panic Recovery in Concurrent Parsers ===\n")

	for _, f := range files {
		wg.Add(1)
		go safeParser(f, results, &wg)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var parsed, panicked, failed int
	fmt.Printf("  %-20s %-10s %-8s %s\n", "File", "Status", "Keys", "Details")
	fmt.Println("  " + strings.Repeat("-", 65))

	for r := range results {
		status := "OK"
		detail := ""
		keys := fmt.Sprintf("%d", r.KeyCount)

		if r.Panicked {
			status = "PANIC"
			detail = r.PanicInfo
			keys = "-"
			panicked++
		} else if r.Err != nil {
			status = "ERROR"
			detail = r.Err.Error()
			keys = "-"
			failed++
		} else {
			parsed++
		}

		fmt.Printf("  %-20s %-10s %-8s %s\n", r.File, status, keys, detail)
	}

	fmt.Println("  " + strings.Repeat("-", 65))
	fmt.Printf("  Parsed: %d | Errors: %d | Panics recovered: %d | Total: %d\n",
		parsed, failed, panicked, len(files))
	fmt.Println()
	fmt.Println("  Without defer/recover, ANY of those panics would have crashed")
	fmt.Println("  the entire process, killing all 8 parsers mid-flight.")
}
```

**What's happening here:** Eight files are parsed concurrently. Three files trigger panics (nil map, index out of bounds, divide by zero). The `safeParser` wrapper uses `defer/recover` to catch panics and convert them into error results. All other goroutines complete normally.

**Key insight:** `defer/recover` must be in the SAME goroutine as the potential panic. A recover in main cannot catch a panic in a child goroutine. Each goroutine must protect itself. The pattern is: the outermost function in the goroutine has `defer func() { if r := recover(); r != nil { ... } }()` as its first statement. This converts process-fatal panics into handled errors.

**What would happen without the recover?** The first panic (in any goroutine) would crash the entire process. The other 7 goroutines would be killed mid-execution, their results lost, and the error channel would never be closed. In a server, every in-flight request would be dropped.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies):
```
=== Panic Recovery in Concurrent Parsers ===

  File                 Status     Keys     Details
  -----------------------------------------------------------------
  app.yaml             OK         24
  database.json         OK         42
  legacy.yaml          PANIC      -        runtime error: assignment to entry in nil map
  cache.toml           OK         30
  broken.toml          PANIC      -        runtime error: index out of range [100] with length 3
  auth.json            OK         27
  huge.json            PANIC      -        runtime error: integer divide by zero
  network.yaml         OK         36
  -----------------------------------------------------------------
  Parsed: 5 | Errors: 0 | Panics recovered: 3 | Total: 8

  Without defer/recover, ANY of those panics would have crashed
  the entire process, killing all 8 parsers mid-flight.
```

## Step 4 -- Combined Pattern: Production-Grade Concurrent Validator

Combine error channels, result structs, and panic recovery into a single robust validation pipeline.

```go
package main

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type Severity string

const (
	SeverityInfo    Severity = "INFO"
	SeverityWarning Severity = "WARN"
	SeverityError   Severity = "ERROR"
	SeverityFatal   Severity = "FATAL"
)

type Finding struct {
	Severity Severity
	Message  string
}

type FileReport struct {
	File     string
	Valid    bool
	Duration time.Duration
	Findings []Finding
}

func validateFile(filename string) FileReport {
	start := time.Now()

	// Simulate panic-inducing bugs
	if filename == "corrupted.bin" {
		var m map[string]int
		m["x"] = 1 // panic: nil map
	}

	time.Sleep(time.Duration(rand.Intn(30)+10) * time.Millisecond)

	report := FileReport{
		File:     filename,
		Valid:    true,
		Duration: time.Since(start),
	}

	switch filename {
	case "database.json":
		report.Valid = false
		report.Findings = []Finding{
			{SeverityError, "invalid JSON syntax at line 42"},
			{SeverityError, "cannot parse connection string"},
		}
	case "auth.json":
		report.Valid = false
		report.Findings = []Finding{
			{SeverityError, "missing required field 'token_expiry'"},
		}
	case "cache.json":
		report.Findings = []Finding{
			{SeverityWarning, "TTL set to 0; cache effectively disabled"},
			{SeverityInfo, "using default eviction policy: LRU"},
		}
	case "logging.json":
		report.Findings = []Finding{
			{SeverityWarning, "log level 'TRACE' is verbose for production"},
		}
	}

	return report
}

func safeValidate(filename string, results chan<- FileReport, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			results <- FileReport{
				File:  filename,
				Valid: false,
				Findings: []Finding{
					{SeverityFatal, fmt.Sprintf("panic during validation: %v", r)},
				},
			}
		}
	}()

	results <- validateFile(filename)
}

func main() {
	files := []string{
		"app.json", "database.json", "cache.json", "auth.json",
		"logging.json", "storage.json", "metrics.json",
		"network.json", "corrupted.bin",
	}

	results := make(chan FileReport, len(files))
	var wg sync.WaitGroup

	fmt.Println("=== Production Config Validator ===\n")
	start := time.Now()

	for _, f := range files {
		wg.Add(1)
		go safeValidate(f, results, &wg)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect and categorize all reports
	var reports []FileReport
	for r := range results {
		reports = append(reports, r)
	}

	elapsed := time.Since(start)

	// Print detailed report
	validCount, warnCount, errorCount, fatalCount := 0, 0, 0, 0

	for _, r := range reports {
		icon := "+"
		if !r.Valid {
			icon = "X"
		} else if len(r.Findings) > 0 {
			icon = "~"
		}

		fmt.Printf("  [%s] %-20s (%v)\n", icon, r.File, r.Duration.Round(time.Millisecond))

		for _, f := range r.Findings {
			prefix := "   "
			switch f.Severity {
			case SeverityFatal:
				prefix = "!!!"
				fatalCount++
			case SeverityError:
				prefix = " ! "
				errorCount++
			case SeverityWarning:
				prefix = " ~ "
				warnCount++
			case SeverityInfo:
				prefix = " i "
			}
			fmt.Printf("      %s %s\n", prefix, f.Message)
		}
	}

	if errorCount == 0 && fatalCount == 0 {
		validCount = len(files)
	} else {
		for _, r := range reports {
			if r.Valid {
				validCount++
			}
		}
	}

	fmt.Println()
	fmt.Println("  " + strings.Repeat("-", 55))
	fmt.Printf("  Files: %d | Valid: %d | Warnings: %d | Errors: %d | Fatal: %d\n",
		len(files), validCount, warnCount, errorCount, fatalCount)
	fmt.Printf("  Total time: %v (concurrent)\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Sequential estimate: ~%v\n",
		time.Duration(len(files)*25)*time.Millisecond)

	if fatalCount > 0 {
		fmt.Println("\n  FATAL findings indicate panics that were recovered.")
		fmt.Println("  These are BUGS that must be fixed -- recovery is a safety net,")
		fmt.Println("  not a substitute for correct code.")
	}
}
```

**What's happening here:** Nine config files are validated concurrently using all three error handling patterns combined. The `FileReport` struct carries success data, warnings, and errors. `defer/recover` catches panics from corrupted files. The final report categorizes findings by severity: INFO, WARN, ERROR, and FATAL (recovered panics).

**Key insight:** Production error handling in concurrent code requires all three patterns working together. Result structs provide a unified view. Error channels (or result channels) transport outcomes. Panic recovery prevents cascading failures. The severity system lets you distinguish between "fix before deploy" (ERROR) and "fix the code itself" (FATAL).

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies):
```
=== Production Config Validator ===

  [+] app.json              (15ms)
  [X] database.json         (22ms)
       !  invalid JSON syntax at line 42
       !  cannot parse connection string
  [~] cache.json            (18ms)
       ~  TTL set to 0; cache effectively disabled
       i  using default eviction policy: LRU
  [X] auth.json             (12ms)
       !  missing required field 'token_expiry'
  [~] logging.json          (25ms)
       ~  log level 'TRACE' is verbose for production
  [+] storage.json          (20ms)
  [+] metrics.json          (14ms)
  [+] network.json          (19ms)
  [X] corrupted.bin         (0ms)
      !!! panic during validation: runtime error: assignment to entry in nil map

  -------------------------------------------------------
  Files: 9 | Valid: 6 | Warnings: 3 | Errors: 3 | Fatal: 1
  Total time: 28ms (concurrent)
  Sequential estimate: ~225ms

  FATAL findings indicate panics that were recovered.
  These are BUGS that must be fixed -- recovery is a safety net,
  not a substitute for correct code.
```

## Common Mistakes

### Ignoring Errors From Goroutines

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func processFile(name string) error {
	time.Sleep(10 * time.Millisecond)
	if name == "bad.json" {
		return fmt.Errorf("parse error in %s", name)
	}
	return nil
}

func main() {
	files := []string{"a.json", "bad.json", "c.json"}
	var wg sync.WaitGroup

	for _, f := range files {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			processFile(name) // error silently discarded!
		}(f)
	}

	wg.Wait()
	fmt.Println("all done") // claims success, but bad.json failed
}
```

**What happens:** `bad.json` fails, but the error is discarded. The program reports success. In production, this means corrupted data goes unnoticed until a customer reports it.

**Correct -- collect errors through a channel:**
```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func processFile(name string) error {
	time.Sleep(10 * time.Millisecond)
	if name == "bad.json" {
		return fmt.Errorf("parse error in %s", name)
	}
	return nil
}

func main() {
	files := []string{"a.json", "bad.json", "c.json"}
	errCh := make(chan error, len(files))
	var wg sync.WaitGroup

	for _, f := range files {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := processFile(name); err != nil {
				errCh <- err
			}
		}(f)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		fmt.Printf("ERROR: %v\n", err)
	}
}
```

### Putting Recover in the Wrong Goroutine

**Wrong -- complete program:**
```go
package main

import "fmt"

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("recovered: %v\n", r) // NEVER called!
		}
	}()

	go func() {
		panic("crash in child goroutine") // kills the process
	}()

	select {} // wait forever
}
```

**What happens:** `recover()` only works in the goroutine where the panic occurs. The defer in main cannot catch a panic in a child goroutine. The program crashes.

**Fix:** Put the recover inside the goroutine that might panic:
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("recovered in goroutine: %v\n", r)
			}
		}()
		panic("crash in child goroutine")
	}()

	time.Sleep(50 * time.Millisecond)
	fmt.Println("main still running")
}
```

## Verify What You Learned

Build a "parallel data pipeline validator" that:
1. Defines 10 data files with different simulated problems: 3 have parse errors, 2 trigger panics (nil pointer, slice out of bounds), 1 has warnings, and 4 are clean
2. Validates all files concurrently, using a result struct that carries: filename, validity, error, severity, and parse duration
3. Each goroutine is wrapped with `defer/recover` to handle panics
4. Collects all results and prints a report sorted by severity (FATAL first, then ERROR, WARN, OK)
5. Reports the total validation time and the estimated sequential time
6. Exits with a summary: if any FATAL or ERROR findings exist, print "VALIDATION FAILED" in the summary

**Hint:** Use a `[]FileReport` to collect results, then `sort.Slice` to order by severity before printing.

## What's Next
Continue to [12-goroutine-return-values](../12-goroutine-return-values/12-goroutine-return-values.md) to learn three patterns for getting return values out of goroutines -- channels, shared memory with mutex, and callbacks.

## Summary
- Errors do not propagate across goroutine boundaries -- you must explicitly send them via channel or struct
- The error channel pattern is simplest: a dedicated `chan error` for collecting failures
- The result struct pattern is most complete: carry data, error, and metadata in one type
- `defer/recover` must be in the SAME goroutine as the potential panic -- a parent goroutine cannot catch a child's panic
- Recovering panics converts process-fatal crashes into handled errors -- essential for worker goroutines
- Always close result/error channels after all goroutines complete to prevent deadlocks
- In production, use severity levels (INFO/WARN/ERROR/FATAL) to distinguish between actionable issues

## Reference
- [Go Blog: Error handling and Go](https://go.dev/blog/error-handling-and-go)
- [Go Tour: Errors](https://go.dev/tour/methods/19)
- [Effective Go: Recover](https://go.dev/doc/effective_go#recover)
- [Go Spec: Handling panics](https://go.dev/ref/spec#Handling_panics)
