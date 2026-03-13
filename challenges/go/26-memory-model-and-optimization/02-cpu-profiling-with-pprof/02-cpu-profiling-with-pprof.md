# 2. CPU Profiling with pprof

<!--
difficulty: intermediate
concepts: [cpu-profiling, pprof, flame-graphs, runtime-pprof, go-tool-pprof]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [testing-basics, benchmarking-basics, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Basic understanding of Go benchmarks (`go test -bench`)
- Familiarity with HTTP servers in Go

## Learning Objectives

After completing this exercise, you will be able to:

- **Collect** CPU profiles using `runtime/pprof` and `net/http/pprof`
- **Analyze** profiles with `go tool pprof` in interactive and web mode
- **Interpret** flame graphs to identify hot functions

## Why CPU Profiling

Guessing where your program spends time is almost always wrong. CPU profiling samples the call stack at regular intervals (default 100 Hz) and tells you exactly which functions consume the most CPU. Go's built-in `pprof` toolchain makes profiling a first-class experience with no external dependencies.

You can collect profiles from benchmarks, from running programs via `runtime/pprof`, or from HTTP servers via `net/http/pprof`. The `go tool pprof` command reads profile files and produces interactive reports, flame graphs, and top-N function listings.

## Step 1 -- Profile a Benchmark

Create a project with a function that has an obvious hotspot:

```bash
mkdir -p ~/go-exercises/cpu-profile && cd ~/go-exercises/cpu-profile
go mod init cpu-profile
```

Create `hotspot.go`:

```go
package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// ProcessData performs string manipulation and hashing.
func ProcessData(items []string) []string {
	results := make([]string, 0, len(items))
	for _, item := range items {
		// Deliberate hotspot: repeated string concatenation
		upper := strings.ToUpper(item)
		repeated := ""
		for i := 0; i < 100; i++ {
			repeated += upper // inefficient concatenation
		}
		hash := sha256.Sum256([]byte(repeated))
		results = append(results, fmt.Sprintf("%x", hash))
	}
	return results
}
```

Create `hotspot_test.go`:

```go
package main

import "testing"

func BenchmarkProcessData(b *testing.B) {
	items := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ProcessData(items)
	}
}
```

Generate a CPU profile from the benchmark:

```bash
go test -bench=BenchmarkProcessData -cpuprofile=cpu.prof -benchtime=3s
```

### Intermediate Verification

You should have a `cpu.prof` file and a test binary in the current directory. The benchmark output shows ns/op.

## Step 2 -- Analyze with go tool pprof

Open the profile interactively:

```bash
go tool pprof cpu.prof
```

At the `(pprof)` prompt, run:

```
top10
```

This shows the top 10 functions by CPU time. You should see string concatenation (`runtime.growslice` or `runtime.concatstrings`) and `crypto/sha256` near the top.

Try the `list` command to see annotated source:

```
list ProcessData
```

Type `quit` to exit.

### Intermediate Verification

The `top10` output shows which functions consume the most cumulative CPU time. The `list` command shows per-line CPU time within `ProcessData`.

## Step 3 -- Generate a Flame Graph

Use the web interface to see a flame graph:

```bash
go tool pprof -http=:8080 cpu.prof
```

This opens a browser with several views. Click **Flame Graph** in the VIEW menu. The widest bars represent the most CPU-consuming functions. Look for `ProcessData` and the string concatenation calls beneath it.

Press Ctrl+C to stop the web server.

### Intermediate Verification

The flame graph visually shows `ProcessData` as a wide bar with child frames for string operations and SHA-256 hashing.

## Step 4 -- Profile an HTTP Server

Create `server.go`:

```go
package main

import (
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/ endpoints
)

func main() {
	http.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		items := []string{"alpha", "bravo", "charlie", "delta", "echo"}
		results := ProcessData(items)
		for _, r := range results {
			w.Write([]byte(r + "\n"))
		}
	})
	http.ListenAndServe(":8080", nil)
}
```

In one terminal, run the server:

```bash
go run server.go hotspot.go
```

In another terminal, generate load and capture a profile:

```bash
# generate some load
for i in $(seq 1 100); do curl -s http://localhost:8080/process > /dev/null; done &

# capture 10-second CPU profile
go tool pprof http://localhost:8080/debug/pprof/profile?seconds=10
```

At the `(pprof)` prompt, run `top10` again.

### Intermediate Verification

The profile from the HTTP endpoint shows the same hotspots as the benchmark profile. The `net/http/pprof` import automatically registers endpoints for CPU, memory, goroutine, and other profiles.

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Forgetting `-cpuprofile` flag | No profile file is generated |
| Profiling for too short a duration | Not enough samples to be statistically meaningful |
| Importing `net/http/pprof` in production without access control | Exposes profiling endpoints to the public |
| Confusing `flat` vs `cum` in `top` output | `flat` = time in the function itself; `cum` = time including callees |

## Verify What You Learned

1. What flag generates a CPU profile from a benchmark?
2. What does the flame graph's x-axis represent?
3. How does `net/http/pprof` differ from `runtime/pprof` for profile collection?
4. What is the default sampling frequency for CPU profiles in Go?

## What's Next

CPU profiling tells you where compute time goes. Next, you'll learn memory profiling to find where allocations happen.

## Summary

Go's `pprof` ecosystem provides CPU profiling through benchmarks (`-cpuprofile`), programmatic API (`runtime/pprof`), and HTTP endpoints (`net/http/pprof`). The `go tool pprof` command analyzes profiles interactively with `top`, `list`, and `web` commands, or graphically via flame graphs with the `-http` flag. Profile your code before optimizing -- measure first, then improve.

## Reference

- [Profiling Go Programs](https://go.dev/blog/pprof)
- [runtime/pprof package](https://pkg.go.dev/runtime/pprof)
- [net/http/pprof package](https://pkg.go.dev/net/http/pprof)
