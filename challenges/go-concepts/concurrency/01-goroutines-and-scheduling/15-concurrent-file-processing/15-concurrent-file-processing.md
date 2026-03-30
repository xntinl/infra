---
difficulty: intermediate
concepts: [concurrent processing, struct methods, result aggregation, error counting, timing comparison]
tools: [go]
estimated_time: 30m
bloom_level: apply
---


# 15. Concurrent File Processing


## Learning Objectives
After completing this exercise, you will be able to:
- **Design** a struct-based log analyzer that processes files concurrently using goroutines
- **Aggregate** results from multiple goroutines through a typed channel
- **Handle** malformed input gracefully within concurrent workers without crashing
- **Measure** and compare sequential vs concurrent performance with real timing data


## Why Concurrent File Processing

Log analysis is a daily task in operations. When an incident strikes, you need to scan dozens of log files for errors, warnings, and anomalies. A single log file might take 200ms to parse. With 8 files, sequential processing takes 1.6 seconds -- an eternity when production is burning. But each file is independent: the content of `api-server.log` tells you nothing about how to parse `database.log`. This makes log analysis a perfect candidate for fan-out concurrency.

The pattern is straightforward: define a result struct that holds per-file statistics, launch one goroutine per file, collect results through a channel, and merge them into a summary. The challenge is handling malformed lines. Real log files contain truncated entries, binary garbage, and mismatched formats. Your parser must skip bad lines, count them, and keep going -- never crash, never block.

This exercise builds the complete pipeline from a single-file parser to a concurrent multi-file analyzer with a formatted summary table.


## Step 1 -- Single File Analysis

Build the foundation: a `LogFile` struct that holds file content as a string slice, and a `FileAnalysis` result struct. The `AnalyzeFile` method scans every line, classifying it as ERROR, WARN, or INFO.

```go
package main

import (
	"fmt"
	"strings"
	"time"
)

const (
	levelError = "ERROR"
	levelWarn  = "WARN"
	levelInfo  = "INFO"
)

type LogFile struct {
	Name  string
	Lines []string
}

type FileAnalysis struct {
	FileName    string
	TotalLines  int
	ErrorCount  int
	WarnCount   int
	InfoCount   int
	Malformed   int
	Duration    time.Duration
}

func NewLogFile(name string, lines []string) *LogFile {
	return &LogFile{Name: name, Lines: lines}
}

func (lf *LogFile) Analyze() FileAnalysis {
	start := time.Now()
	analysis := FileAnalysis{
		FileName:   lf.Name,
		TotalLines: len(lf.Lines),
	}

	for _, line := range lf.Lines {
		time.Sleep(5 * time.Millisecond) // simulate I/O read latency

		switch classifyLine(line) {
		case levelError:
			analysis.ErrorCount++
		case levelWarn:
			analysis.WarnCount++
		case levelInfo:
			analysis.InfoCount++
		default:
			analysis.Malformed++
		}
	}

	analysis.Duration = time.Since(start)
	return analysis
}

func classifyLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "MALFORMED"
	}
	upper := strings.ToUpper(trimmed)
	switch {
	case strings.Contains(upper, "[ERROR]"):
		return levelError
	case strings.Contains(upper, "[WARN]"):
		return levelWarn
	case strings.Contains(upper, "[INFO]"):
		return levelInfo
	default:
		return "MALFORMED"
	}
}

func printAnalysis(a FileAnalysis) {
	fmt.Printf("  File: %s\n", a.FileName)
	fmt.Printf("    Total lines: %d\n", a.TotalLines)
	fmt.Printf("    Errors:      %d\n", a.ErrorCount)
	fmt.Printf("    Warnings:    %d\n", a.WarnCount)
	fmt.Printf("    Info:        %d\n", a.InfoCount)
	fmt.Printf("    Malformed:   %d\n", a.Malformed)
	fmt.Printf("    Parse time:  %v\n", a.Duration.Round(time.Millisecond))
}

func main() {
	logFile := NewLogFile("api-server.log", []string{
		"2024-01-15 10:00:01 [INFO] Server started on port 8080",
		"2024-01-15 10:00:05 [INFO] Connected to database",
		"2024-01-15 10:01:12 [WARN] Slow query detected: 2.3s",
		"2024-01-15 10:01:45 [ERROR] Connection pool exhausted",
		"2024-01-15 10:02:00 [INFO] Request processed: /api/users",
		"2024-01-15 10:02:33 [ERROR] Timeout calling payment service",
		"2024-01-15 10:03:00 [WARN] Memory usage at 85%",
		"2024-01-15 10:03:15 [INFO] Cache hit ratio: 0.92",
		"",
		"2024-01-15 10:04:00 [INFO] Health check passed",
	})

	fmt.Println("=== Single File Analysis ===\n")
	analysis := logFile.Analyze()
	printAnalysis(analysis)
}
```

**What's happening here:** The `Analyze` method iterates over every line, classifying it by checking for `[ERROR]`, `[WARN]`, or `[INFO]` markers. Lines that match none of these (including empty lines) are counted as malformed. The 5ms sleep per line simulates real disk I/O latency. A 10-line file takes ~50ms.

**Key insight:** The `classifyLine` function is a pure function -- it takes a string and returns a category. This makes it easy to test independently and safe to call from multiple goroutines simultaneously because it has no shared state.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Single File Analysis ===

  File: api-server.log
    Total lines: 10
    Errors:      2
    Warnings:    2
    Info:        5
    Malformed:   1
    Parse time:  50ms
```


## Step 2 -- All Files Concurrently

Now define 8 log files and analyze them all at once. One goroutine per file, results collected through a buffered channel.

```go
package main

import (
	"fmt"
	"strings"
	"time"
)

const (
	levelError = "ERROR"
	levelWarn  = "WARN"
	levelInfo  = "INFO"
)

type LogFile struct {
	Name  string
	Lines []string
}

type FileAnalysis struct {
	FileName   string
	TotalLines int
	ErrorCount int
	WarnCount  int
	InfoCount  int
	Malformed  int
	Duration   time.Duration
}

type LogAnalyzer struct {
	Files []*LogFile
}

func NewLogFile(name string, lines []string) *LogFile {
	return &LogFile{Name: name, Lines: lines}
}

func NewLogAnalyzer(files []*LogFile) *LogAnalyzer {
	return &LogAnalyzer{Files: files}
}

func (lf *LogFile) Analyze() FileAnalysis {
	start := time.Now()
	analysis := FileAnalysis{
		FileName:   lf.Name,
		TotalLines: len(lf.Lines),
	}

	for _, line := range lf.Lines {
		time.Sleep(5 * time.Millisecond)
		switch classifyLine(line) {
		case levelError:
			analysis.ErrorCount++
		case levelWarn:
			analysis.WarnCount++
		case levelInfo:
			analysis.InfoCount++
		default:
			analysis.Malformed++
		}
	}

	analysis.Duration = time.Since(start)
	return analysis
}

func (la *LogAnalyzer) AnalyzeAll() []FileAnalysis {
	results := make(chan FileAnalysis, len(la.Files))

	for _, file := range la.Files {
		go func(f *LogFile) {
			results <- f.Analyze()
		}(file)
	}

	analyses := make([]FileAnalysis, 0, len(la.Files))
	for i := 0; i < len(la.Files); i++ {
		analyses = append(analyses, <-results)
	}
	return analyses
}

func classifyLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "MALFORMED"
	}
	upper := strings.ToUpper(trimmed)
	switch {
	case strings.Contains(upper, "[ERROR]"):
		return levelError
	case strings.Contains(upper, "[WARN]"):
		return levelWarn
	case strings.Contains(upper, "[INFO]"):
		return levelInfo
	default:
		return "MALFORMED"
	}
}

func printSummaryTable(analyses []FileAnalysis, wallClock time.Duration) {
	fmt.Println("  File                     Lines  Errors  Warns  Info  Bad   Time")
	fmt.Println("  ────                     ─────  ──────  ─────  ────  ───   ────")
	for _, a := range analyses {
		fmt.Printf("  %-24s %5d  %6d  %5d  %4d  %3d   %v\n",
			a.FileName, a.TotalLines, a.ErrorCount, a.WarnCount,
			a.InfoCount, a.Malformed, a.Duration.Round(time.Millisecond))
	}
	fmt.Printf("\n  Wall clock: %v\n", wallClock.Round(time.Millisecond))
}

func buildLogFiles() []*LogFile {
	return []*LogFile{
		NewLogFile("api-server.log", []string{
			"2024-01-15 10:00:01 [INFO] Server started",
			"2024-01-15 10:01:12 [WARN] Slow query: 2.3s",
			"2024-01-15 10:01:45 [ERROR] Pool exhausted",
			"2024-01-15 10:02:00 [INFO] Request /api/users",
			"2024-01-15 10:02:33 [ERROR] Payment timeout",
			"2024-01-15 10:03:00 [WARN] Memory 85%",
			"2024-01-15 10:03:15 [INFO] Cache ratio: 0.92",
			"2024-01-15 10:04:00 [INFO] Health OK",
		}),
		NewLogFile("database.log", []string{
			"2024-01-15 10:00:00 [INFO] PostgreSQL ready",
			"2024-01-15 10:01:00 [WARN] Replication lag: 500ms",
			"2024-01-15 10:02:00 [ERROR] Deadlock detected",
			"2024-01-15 10:02:30 [INFO] Deadlock resolved",
			"2024-01-15 10:03:00 [INFO] Vacuum complete",
			"2024-01-15 10:04:00 [WARN] Connection near limit",
		}),
		NewLogFile("auth-service.log", []string{
			"2024-01-15 10:00:00 [INFO] Auth service ready",
			"2024-01-15 10:01:00 [INFO] Token issued: user-42",
			"2024-01-15 10:02:00 [ERROR] Invalid signature",
			"2024-01-15 10:03:00 [WARN] Rate limit near: 90%",
			"2024-01-15 10:04:00 [INFO] Key rotation complete",
		}),
		NewLogFile("load-balancer.log", []string{
			"2024-01-15 10:00:00 [INFO] LB started",
			"2024-01-15 10:01:00 [INFO] Backend registered: api-1",
			"2024-01-15 10:01:30 [INFO] Backend registered: api-2",
			"2024-01-15 10:02:00 [WARN] Backend api-3 unhealthy",
			"2024-01-15 10:02:30 [ERROR] No healthy backends",
			"2024-01-15 10:03:00 [INFO] Backend api-3 recovered",
			"2024-01-15 10:04:00 [INFO] Health check pass",
		}),
		NewLogFile("cache.log", []string{
			"2024-01-15 10:00:00 [INFO] Redis connected",
			"2024-01-15 10:01:00 [WARN] Eviction rate high",
			"2024-01-15 10:02:00 [INFO] Key count: 150k",
			"2024-01-15 10:03:00 [ERROR] OOM: max memory reached",
			"2024-01-15 10:03:30 [WARN] Falling back to disk",
			"2024-01-15 10:04:00 [INFO] Memory freed: 200MB",
			"2024-01-15 10:05:00 [INFO] Cluster rebalanced",
			"2024-01-15 10:06:00 [INFO] Snapshot saved",
			"2024-01-15 10:07:00 [WARN] Slow read: key-999",
		}),
		NewLogFile("scheduler.log", []string{
			"2024-01-15 10:00:00 [INFO] Cron started",
			"2024-01-15 10:01:00 [INFO] Job cleanup: success",
			"2024-01-15 10:02:00 [ERROR] Job report-gen: failed",
			"2024-01-15 10:03:00 [INFO] Job backup: success",
		}),
		NewLogFile("notification.log", []string{
			"2024-01-15 10:00:00 [INFO] Notification service up",
			"2024-01-15 10:01:00 [INFO] Email sent: user-42",
			"2024-01-15 10:02:00 [ERROR] SMTP timeout",
			"2024-01-15 10:02:30 [ERROR] SMTP timeout",
			"2024-01-15 10:03:00 [WARN] Queue depth: 500",
			"2024-01-15 10:04:00 [INFO] Queue drained",
		}),
		NewLogFile("metrics.log", []string{
			"2024-01-15 10:00:00 [INFO] Prometheus scrape OK",
			"2024-01-15 10:01:00 [INFO] Grafana connected",
			"2024-01-15 10:02:00 [WARN] Metric cardinality high",
			"2024-01-15 10:03:00 [INFO] Alert resolved: cpu",
			"2024-01-15 10:04:00 [INFO] Dashboard updated",
		}),
	}
}

func main() {
	files := buildLogFiles()
	analyzer := NewLogAnalyzer(files)

	fmt.Println("=== Concurrent Log Analysis (8 files) ===\n")
	start := time.Now()
	results := analyzer.AnalyzeAll()
	wallClock := time.Since(start)
	printSummaryTable(results, wallClock)
}
```

**What's happening here:** The `LogAnalyzer.AnalyzeAll` method launches 8 goroutines, one per file. Each goroutine calls `Analyze()` on its file and sends the result through a buffered channel. The main goroutine collects exactly 8 results. With 5ms per line and files averaging 6-9 lines, each file takes 30-45ms. Sequential would be ~280ms total; concurrent finishes in ~45ms (the slowest file).

**Key insight:** The `LogAnalyzer` encapsulates the fan-out logic. Callers do not need to know about goroutines or channels -- they call `AnalyzeAll()` and get back a slice of results. This is good API design: concurrency is an implementation detail, not a contract.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies -- results arrive in completion order):
```
=== Concurrent Log Analysis (8 files) ===

  File                     Lines  Errors  Warns  Info  Bad   Time
  ────                     ─────  ──────  ─────  ────  ───   ────
  scheduler.log                4       1      0     3    0   20ms
  auth-service.log             5       1      1     3    0   25ms
  metrics.log                  5       0      1     4    0   25ms
  database.log                 6       1      2     3    0   30ms
  notification.log             6       2      1     3    0   30ms
  load-balancer.log            7       1      1     5    0   35ms
  api-server.log               8       2      2     4    0   40ms
  cache.log                    9       1      3     5    0   45ms

  Wall clock: 45ms
```


## Step 3 -- Malformed Line Handling

Real log files contain garbage. Add lines that do not match any known format: truncated entries, binary data, and empty lines. The parser counts them as malformed without crashing.

```go
package main

import (
	"fmt"
	"strings"
	"time"
)

const (
	levelError = "ERROR"
	levelWarn  = "WARN"
	levelInfo  = "INFO"
)

type LogFile struct {
	Name  string
	Lines []string
}

type FileAnalysis struct {
	FileName      string
	TotalLines    int
	ErrorCount    int
	WarnCount     int
	InfoCount     int
	Malformed     int
	MalformedList []string
	Duration      time.Duration
}

type LogAnalyzer struct {
	Files []*LogFile
}

func NewLogFile(name string, lines []string) *LogFile {
	return &LogFile{Name: name, Lines: lines}
}

func NewLogAnalyzer(files []*LogFile) *LogAnalyzer {
	return &LogAnalyzer{Files: files}
}

func (lf *LogFile) Analyze() FileAnalysis {
	start := time.Now()
	analysis := FileAnalysis{
		FileName:   lf.Name,
		TotalLines: len(lf.Lines),
	}

	for _, line := range lf.Lines {
		time.Sleep(5 * time.Millisecond)

		category := classifyLine(line)
		switch category {
		case levelError:
			analysis.ErrorCount++
		case levelWarn:
			analysis.WarnCount++
		case levelInfo:
			analysis.InfoCount++
		default:
			analysis.Malformed++
			preview := truncate(line, 40)
			analysis.MalformedList = append(analysis.MalformedList, preview)
		}
	}

	analysis.Duration = time.Since(start)
	return analysis
}

func (la *LogAnalyzer) AnalyzeAll() []FileAnalysis {
	results := make(chan FileAnalysis, len(la.Files))

	for _, file := range la.Files {
		go func(f *LogFile) {
			results <- f.Analyze()
		}(file)
	}

	analyses := make([]FileAnalysis, 0, len(la.Files))
	for i := 0; i < len(la.Files); i++ {
		analyses = append(analyses, <-results)
	}
	return analyses
}

func classifyLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "MALFORMED"
	}
	upper := strings.ToUpper(trimmed)
	switch {
	case strings.Contains(upper, "[ERROR]"):
		return levelError
	case strings.Contains(upper, "[WARN]"):
		return levelWarn
	case strings.Contains(upper, "[INFO]"):
		return levelInfo
	default:
		return "MALFORMED"
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func printDetailedReport(analyses []FileAnalysis) {
	totalErrors := 0
	totalWarns := 0
	totalMalformed := 0
	totalLines := 0

	fmt.Println("  File                     Lines  Errors  Warns  Info  Bad")
	fmt.Println("  ────                     ─────  ──────  ─────  ────  ───")
	for _, a := range analyses {
		fmt.Printf("  %-24s %5d  %6d  %5d  %4d  %3d\n",
			a.FileName, a.TotalLines, a.ErrorCount, a.WarnCount,
			a.InfoCount, a.Malformed)
		totalErrors += a.ErrorCount
		totalWarns += a.WarnCount
		totalMalformed += a.Malformed
		totalLines += a.TotalLines
	}

	fmt.Printf("\n  Totals: %d lines | %d errors | %d warnings | %d malformed\n",
		totalLines, totalErrors, totalWarns, totalMalformed)

	if totalMalformed > 0 {
		fmt.Println("\n  --- Malformed Lines ---")
		for _, a := range analyses {
			for _, m := range a.MalformedList {
				fmt.Printf("  [%s] %s\n", a.FileName, m)
			}
		}
	}
}

func buildLogFilesWithGarbage() []*LogFile {
	return []*LogFile{
		NewLogFile("api-server.log", []string{
			"2024-01-15 10:00:01 [INFO] Server started",
			"2024-01-15 10:01:12 [WARN] Slow query",
			"2024-01-15 10:01:45 [ERROR] Pool exhausted",
			"",
			"\x00\x01\x02 binary garbage",
			"2024-01-15 10:03:15 [INFO] Cache OK",
		}),
		NewLogFile("database.log", []string{
			"2024-01-15 10:00:00 [INFO] PostgreSQL ready",
			"2024-01-15 10:01:00 [WARN] Replication lag",
			"TRUNCATED LINE WITHOUT BRACKETS",
			"2024-01-15 10:02:00 [ERROR] Deadlock",
			"2024-01-15 10:03:00 [INFO] Vacuum done",
		}),
		NewLogFile("auth-service.log", []string{
			"2024-01-15 10:00:00 [INFO] Auth ready",
			"just some random text here",
			"2024-01-15 10:02:00 [ERROR] Invalid sig",
			"2024-01-15 10:03:00 [WARN] Rate limit",
			"",
			"2024-01-15 10:04:00 [INFO] Key rotated",
		}),
		NewLogFile("scheduler.log", []string{
			"2024-01-15 10:00:00 [INFO] Cron started",
			"---",
			"2024-01-15 10:02:00 [ERROR] Job failed",
			"2024-01-15 10:03:00 [INFO] Backup OK",
			"partial log entry without level marker at all",
		}),
	}
}

func main() {
	files := buildLogFilesWithGarbage()
	analyzer := NewLogAnalyzer(files)

	fmt.Println("=== Log Analysis with Malformed Lines ===\n")
	start := time.Now()
	results := analyzer.AnalyzeAll()
	wallClock := time.Since(start)
	printDetailedReport(results)
	fmt.Printf("\n  Wall clock: %v\n", wallClock.Round(time.Millisecond))
}
```

**What's happening here:** Each file now contains lines that do not match any log level pattern: empty strings, binary data, truncated entries, and random text. The `classifyLine` function routes them all to "MALFORMED." The `FileAnalysis` captures both the count and a preview list of the offending lines. The report shows per-file stats and then lists all malformed lines across all files.

**Key insight:** The `truncate` helper prevents a single huge garbage line from dominating the report output. In production, you would also want to track the line number for each malformed entry so operators can find and fix the source. The parser never panics on bad input -- it classifies and moves on.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Log Analysis with Malformed Lines ===

  File                     Lines  Errors  Warns  Info  Bad
  ────                     ─────  ──────  ─────  ────  ───
  api-server.log               6       1      1     2    2
  database.log                 5       1      1     2    1
  auth-service.log             6       1      1     2    2
  scheduler.log                5       1      0     2    2

  Totals: 22 lines | 4 errors | 3 warnings | 7 malformed

  --- Malformed Lines ---
  [api-server.log]
  [api-server.log] binary garbage
  [database.log] TRUNCATED LINE WITHOUT BRACKETS
  [auth-service.log] just some random text here
  [auth-service.log]
  [scheduler.log] ---
  [scheduler.log] partial log entry without level marker...

  Wall clock: 30ms
```


## Step 4 -- Sequential vs Concurrent Timing Comparison

Run the same analysis both ways and produce a side-by-side timing table that demonstrates the concrete speedup.

```go
package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	levelError = "ERROR"
	levelWarn  = "WARN"
	levelInfo  = "INFO"
)

type LogFile struct {
	Name  string
	Lines []string
}

type FileAnalysis struct {
	FileName   string
	TotalLines int
	ErrorCount int
	WarnCount  int
	InfoCount  int
	Malformed  int
	Duration   time.Duration
}

type AnalysisSummary struct {
	FileResults    []FileAnalysis
	TotalLines     int
	TotalErrors    int
	TotalWarnings  int
	TotalMalformed int
	WallClock      time.Duration
}

type LogAnalyzer struct {
	Files []*LogFile
}

func NewLogFile(name string, lines []string) *LogFile {
	return &LogFile{Name: name, Lines: lines}
}

func NewLogAnalyzer(files []*LogFile) *LogAnalyzer {
	return &LogAnalyzer{Files: files}
}

func (lf *LogFile) Analyze() FileAnalysis {
	start := time.Now()
	analysis := FileAnalysis{
		FileName:   lf.Name,
		TotalLines: len(lf.Lines),
	}
	for _, line := range lf.Lines {
		time.Sleep(5 * time.Millisecond)
		switch classifyLine(line) {
		case levelError:
			analysis.ErrorCount++
		case levelWarn:
			analysis.WarnCount++
		case levelInfo:
			analysis.InfoCount++
		default:
			analysis.Malformed++
		}
	}
	analysis.Duration = time.Since(start)
	return analysis
}

func (la *LogAnalyzer) AnalyzeSequential() []FileAnalysis {
	results := make([]FileAnalysis, 0, len(la.Files))
	for _, f := range la.Files {
		results = append(results, f.Analyze())
	}
	return results
}

func (la *LogAnalyzer) AnalyzeConcurrent() []FileAnalysis {
	ch := make(chan FileAnalysis, len(la.Files))
	for _, f := range la.Files {
		go func(file *LogFile) {
			ch <- file.Analyze()
		}(f)
	}
	results := make([]FileAnalysis, 0, len(la.Files))
	for i := 0; i < len(la.Files); i++ {
		results = append(results, <-ch)
	}
	return results
}

func classifyLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "MALFORMED"
	}
	upper := strings.ToUpper(trimmed)
	switch {
	case strings.Contains(upper, "[ERROR]"):
		return levelError
	case strings.Contains(upper, "[WARN]"):
		return levelWarn
	case strings.Contains(upper, "[INFO]"):
		return levelInfo
	default:
		return "MALFORMED"
	}
}

func buildSummary(results []FileAnalysis, wallClock time.Duration) AnalysisSummary {
	summary := AnalysisSummary{
		FileResults: results,
		WallClock:   wallClock,
	}
	for _, r := range results {
		summary.TotalLines += r.TotalLines
		summary.TotalErrors += r.ErrorCount
		summary.TotalWarnings += r.WarnCount
		summary.TotalMalformed += r.Malformed
	}
	return summary
}

func printSummary(label string, summary AnalysisSummary) {
	fmt.Printf("  --- %s ---\n", label)

	sorted := make([]FileAnalysis, len(summary.FileResults))
	copy(sorted, summary.FileResults)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].FileName < sorted[j].FileName
	})

	fmt.Println("  File                     Lines  Errors  Warns  Info  Bad   Parse Time")
	fmt.Println("  ────                     ─────  ──────  ─────  ────  ───   ──────────")
	for _, a := range sorted {
		fmt.Printf("  %-24s %5d  %6d  %5d  %4d  %3d   %v\n",
			a.FileName, a.TotalLines, a.ErrorCount, a.WarnCount,
			a.InfoCount, a.Malformed, a.Duration.Round(time.Millisecond))
	}
	fmt.Printf("\n  Totals: %d lines | %d errors | %d warnings | %d malformed\n",
		summary.TotalLines, summary.TotalErrors, summary.TotalWarnings, summary.TotalMalformed)
	fmt.Printf("  Wall clock: %v\n\n", summary.WallClock.Round(time.Millisecond))
}

func buildAllLogFiles() []*LogFile {
	return []*LogFile{
		NewLogFile("api-server.log", []string{
			"2024-01-15 [INFO] Started", "2024-01-15 [WARN] Slow query",
			"2024-01-15 [ERROR] Pool full", "2024-01-15 [INFO] Request OK",
			"2024-01-15 [ERROR] Timeout", "2024-01-15 [WARN] Memory 85%",
			"2024-01-15 [INFO] Cache 92%", "2024-01-15 [INFO] Health OK",
		}),
		NewLogFile("database.log", []string{
			"2024-01-15 [INFO] PG ready", "2024-01-15 [WARN] Replication lag",
			"2024-01-15 [ERROR] Deadlock", "2024-01-15 [INFO] Resolved",
			"2024-01-15 [INFO] Vacuum", "2024-01-15 [WARN] Conn limit",
		}),
		NewLogFile("auth-service.log", []string{
			"2024-01-15 [INFO] Auth ready", "2024-01-15 [INFO] Token user-42",
			"2024-01-15 [ERROR] Bad sig", "2024-01-15 [WARN] Rate limit",
			"2024-01-15 [INFO] Key rotated",
		}),
		NewLogFile("load-balancer.log", []string{
			"2024-01-15 [INFO] LB started", "2024-01-15 [INFO] Backend api-1",
			"2024-01-15 [INFO] Backend api-2", "2024-01-15 [WARN] api-3 sick",
			"2024-01-15 [ERROR] No backends", "2024-01-15 [INFO] api-3 recovered",
			"2024-01-15 [INFO] Health OK",
		}),
		NewLogFile("cache.log", []string{
			"2024-01-15 [INFO] Redis OK", "2024-01-15 [WARN] Eviction high",
			"2024-01-15 [INFO] 150k keys", "2024-01-15 [ERROR] OOM",
			"2024-01-15 [WARN] Disk fallback", "2024-01-15 [INFO] Freed 200MB",
			"2024-01-15 [INFO] Rebalanced", "2024-01-15 [INFO] Snapshot",
			"2024-01-15 [WARN] Slow key-999",
		}),
		NewLogFile("scheduler.log", []string{
			"2024-01-15 [INFO] Cron started", "2024-01-15 [INFO] Cleanup OK",
			"2024-01-15 [ERROR] Report failed", "2024-01-15 [INFO] Backup OK",
		}),
		NewLogFile("notification.log", []string{
			"2024-01-15 [INFO] Notif up", "2024-01-15 [INFO] Email user-42",
			"2024-01-15 [ERROR] SMTP timeout", "2024-01-15 [ERROR] SMTP timeout",
			"2024-01-15 [WARN] Queue 500", "2024-01-15 [INFO] Queue drained",
		}),
		NewLogFile("metrics.log", []string{
			"2024-01-15 [INFO] Prometheus OK", "2024-01-15 [INFO] Grafana OK",
			"2024-01-15 [WARN] Cardinality", "2024-01-15 [INFO] Alert resolved",
			"2024-01-15 [INFO] Dashboard OK",
		}),
	}
}

func main() {
	files := buildAllLogFiles()

	fmt.Println("=== Log Analysis: Sequential vs Concurrent ===\n")

	seqAnalyzer := NewLogAnalyzer(files)
	seqStart := time.Now()
	seqResults := seqAnalyzer.AnalyzeSequential()
	seqWall := time.Since(seqStart)
	seqSummary := buildSummary(seqResults, seqWall)
	printSummary("Sequential", seqSummary)

	concAnalyzer := NewLogAnalyzer(files)
	concStart := time.Now()
	concResults := concAnalyzer.AnalyzeConcurrent()
	concWall := time.Since(concStart)
	concSummary := buildSummary(concResults, concWall)
	printSummary("Concurrent", concSummary)

	fmt.Println("  === Timing Comparison ===")
	speedup := float64(seqWall) / float64(concWall)
	fmt.Printf("  Sequential: %v\n", seqWall.Round(time.Millisecond))
	fmt.Printf("  Concurrent: %v\n", concWall.Round(time.Millisecond))
	fmt.Printf("  Speedup:    %.1fx faster\n", speedup)
	fmt.Printf("  Files:      %d\n", len(files))
}
```

**What's happening here:** The same set of 8 log files is analyzed twice -- first sequentially, then concurrently. Both produce identical data (same errors, same warnings). The only difference is wall-clock time. Sequential sums all file parse times (~250ms); concurrent waits for the slowest file (~45ms). The speedup factor confirms the benefit.

**Key insight:** The speedup is bounded by the number of files and the variance in their sizes. With 8 equally-sized files, you get close to 8x speedup. With 7 tiny files and 1 huge file, the speedup is limited by the huge file. This is Amdahl's Law in practice: the non-parallelizable portion (the slowest file) determines your maximum speedup.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Log Analysis: Sequential vs Concurrent ===

  --- Sequential ---
  File                     Lines  Errors  Warns  Info  Bad   Parse Time
  ────                     ─────  ──────  ─────  ────  ───   ──────────
  api-server.log               8       2      2     4    0   40ms
  auth-service.log             5       1      1     3    0   25ms
  cache.log                    9       1      3     5    0   45ms
  database.log                 6       1      2     3    0   30ms
  load-balancer.log            7       1      1     5    0   35ms
  metrics.log                  5       0      1     4    0   25ms
  notification.log             6       2      1     3    0   30ms
  scheduler.log                4       1      0     3    0   20ms

  Totals: 50 lines | 9 errors | 11 warnings | 0 malformed
  Wall clock: 250ms

  --- Concurrent ---
  File                     Lines  Errors  Warns  Info  Bad   Parse Time
  ────                     ─────  ──────  ─────  ────  ───   ──────────
  api-server.log               8       2      2     4    0   40ms
  auth-service.log             5       1      1     3    0   25ms
  cache.log                    9       1      3     5    0   45ms
  database.log                 6       1      2     3    0   30ms
  load-balancer.log            7       1      1     5    0   35ms
  metrics.log                  5       0      1     4    0   25ms
  notification.log             6       2      1     3    0   30ms
  scheduler.log                4       1      0     3    0   20ms

  Totals: 50 lines | 9 errors | 11 warnings | 0 malformed
  Wall clock: 45ms

  === Timing Comparison ===
  Sequential: 250ms
  Concurrent: 45ms
  Speedup:    5.6x faster
  Files:      8
```


## Common Mistakes

### Collecting Fewer Results Than Goroutines Launched

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func analyze(file string, ch chan<- string) {
	time.Sleep(20 * time.Millisecond)
	ch <- file + ": done"
}

func main() {
	files := []string{"a.log", "b.log", "c.log", "d.log"}
	ch := make(chan string, len(files))
	for _, f := range files {
		go analyze(f, ch)
	}
	// only collecting 2 of 4 results
	for i := 0; i < 2; i++ {
		fmt.Println(<-ch)
	}
	fmt.Println("Finished") // 2 goroutines are abandoned
}
```
**What happens:** Two goroutines successfully send their results, but the other two are abandoned. Their results sit in the buffered channel, and the goroutines have already exited. In a long-running server, this pattern leaks goroutines and memory if the channel is unbuffered.

**Correct -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func analyze(file string, ch chan<- string) {
	time.Sleep(20 * time.Millisecond)
	ch <- file + ": done"
}

func main() {
	files := []string{"a.log", "b.log", "c.log", "d.log"}
	ch := make(chan string, len(files))
	for _, f := range files {
		go analyze(f, ch)
	}
	for i := 0; i < len(files); i++ {
		fmt.Println(<-ch)
	}
	fmt.Println("Finished")
}
```

### Mutating Shared State from Goroutines Without Synchronization

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	totalErrors := 0
	var wg sync.WaitGroup
	files := []int{3, 5, 2, 4} // error counts per file

	for _, count := range files {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			totalErrors += c // DATA RACE: multiple goroutines write to shared int
		}(count)
	}
	wg.Wait()
	fmt.Println("Total errors:", totalErrors) // unpredictable result
}
```
**What happens:** Multiple goroutines increment `totalErrors` simultaneously. Without synchronization, some increments are lost. Run with `go run -race main.go` to see the data race detector flag this.

**Correct -- use a channel to aggregate:**
```go
package main

import "fmt"

func main() {
	counts := []int{3, 5, 2, 4}
	ch := make(chan int, len(counts))

	for _, c := range counts {
		go func(n int) {
			ch <- n
		}(c)
	}

	totalErrors := 0
	for i := 0; i < len(counts); i++ {
		totalErrors += <-ch
	}
	fmt.Println("Total errors:", totalErrors) // always 14
}
```


## Verify What You Learned

Build a "multi-service log auditor" that:
1. Defines 6 services, each with a log file containing 10-20 lines of mixed levels (INFO, WARN, ERROR) plus some malformed lines
2. Analyzes all files concurrently, collecting `FileAnalysis` results through a buffered channel
3. Prints a summary table sorted by error count (most errors first)
4. Identifies the "most problematic service" (highest error count) and the "cleanest service" (lowest error + malformed count)
5. Runs both sequential and concurrent and prints the timing comparison
6. Uses `sort.Slice` to sort results and reports the top 3 most common malformed line patterns

**Hint:** Add a `PatternCount map[string]int` to the summary to track how often each malformed line pattern appears across all files.


## What's Next
Continue to [16-goroutine-supervision](../16-goroutine-supervision/16-goroutine-supervision.md) to build a supervisor that manages long-lived goroutines, automatically restarting them when they panic, with configurable retry limits.


## Summary
- The `LogAnalyzer` encapsulates fan-out concurrency behind a clean API -- callers get a slice of results without knowing about goroutines
- One goroutine per file with a buffered channel is the standard pattern for independent file processing
- Malformed input must be handled gracefully: classify, count, and continue -- never crash the parser
- Sequential vs concurrent timing comparison demonstrates Amdahl's Law: speedup is bounded by the slowest file
- The `classifyLine` function is a pure function, safe for concurrent use because it has no shared mutable state
- Always collect exactly N results from N goroutines to prevent leaks or deadlocks
- Use channels for aggregation instead of shared mutable state to avoid data races


## Reference
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
- [sort.Slice](https://pkg.go.dev/sort#Slice)
- [strings.Contains](https://pkg.go.dev/strings#Contains)
