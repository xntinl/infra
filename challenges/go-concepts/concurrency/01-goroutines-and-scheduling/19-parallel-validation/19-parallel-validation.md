---
difficulty: intermediate
concepts: [concurrent fan-out, structured results, validation pattern, goroutine coordination, report aggregation]
tools: [go]
estimated_time: 30m
bloom_level: apply
---


# 19. Parallel Validation


## Learning Objectives
After completing this exercise, you will be able to:
- **Design** a validation framework where each rule runs as an independent goroutine
- **Collect** structured pass/fail results through a buffered channel
- **Aggregate** validation outcomes into a formatted report with timing data
- **Apply** the fan-out pattern to a realistic Kubernetes pre-flight check scenario


## Why Parallel Validation

Before deploying a Kubernetes manifest, production teams run pre-flight checks: does the namespace exist? Are resource limits set? Is the image tag pinned (not "latest")? Are replicas within range? Each check is independent, often involving a simulated API call or config lookup that takes 50-200ms.

Running 8 checks sequentially means waiting 400-1600ms. Running them concurrently means waiting only as long as the slowest single check. For a CI/CD pipeline that runs these checks on every push, this difference compounds into minutes saved per day.

The pattern is clean: define each validation rule as a function, launch all rules as goroutines, collect `ValidationResult` structs through a channel, and aggregate them into a report. This is the fan-out/fan-in pattern applied to a domain where the rules are truly independent -- the result of one check never affects whether another check runs.


## Step 1 -- Three Sequential Validations

Start with a small set of validations running sequentially. This establishes the data structures and the validation contract.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

type ValidationResult struct {
	RuleName string
	Passed   bool
	Message  string
	Duration time.Duration
}

type ValidationRule struct {
	Name  string
	Check func() ValidationResult
}

func newRule(name string, latency time.Duration, pass bool, msg string) ValidationRule {
	return ValidationRule{
		Name: name,
		Check: func() ValidationResult {
			start := time.Now()
			time.Sleep(latency)
			return ValidationResult{
				RuleName: name,
				Passed:   pass,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

func runSequential(rules []ValidationRule) []ValidationResult {
	results := make([]ValidationResult, 0, len(rules))
	for _, rule := range rules {
		results = append(results, rule.Check())
	}
	return results
}

func printResults(results []ValidationResult, elapsed time.Duration) {
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %-30s %v  %s\n", status, r.RuleName, r.Duration.Round(time.Millisecond), r.Message)
	}
	fmt.Printf("\n  Total wall-clock time: %v\n", elapsed.Round(time.Millisecond))
}

func main() {
	rules := []ValidationRule{
		newRule("namespace-exists", 80*time.Millisecond, true, "namespace 'production' found"),
		newRule("resource-limits-set", 120*time.Millisecond, true, "CPU and memory limits defined"),
		newRule("image-tag-not-latest", 60*time.Millisecond, false, "image uses 'latest' tag -- pin to specific version"),
	}

	fmt.Println("=== Sequential Validation (3 rules) ===")
	fmt.Println()
	start := time.Now()
	results := runSequential(rules)
	elapsed := time.Since(start)
	printResults(results, elapsed)
}
```

**What's happening here:** Each `ValidationRule` wraps a check function that simulates network latency and returns a `ValidationResult` with pass/fail status, a human-readable message, and the duration. Running three checks sequentially takes ~260ms (80 + 120 + 60).

**Key insight:** The `ValidationRule` struct decouples the rule definition from its execution strategy. The same rules will work for both sequential and concurrent execution -- only the runner changes.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Sequential Validation (3 rules) ===

  [PASS] namespace-exists                80ms  namespace 'production' found
  [PASS] resource-limits-set             120ms  CPU and memory limits defined
  [FAIL] image-tag-not-latest            60ms  image uses 'latest' tag -- pin to specific version

  Total wall-clock time: 260ms
```


## Step 2 -- Concurrent Validation with Goroutines

Convert the runner to launch each rule as a goroutine and collect results through a buffered channel.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

type ValidationResult struct {
	RuleName string
	Passed   bool
	Message  string
	Duration time.Duration
}

type ValidationRule struct {
	Name  string
	Check func() ValidationResult
}

type Validator struct {
	Rules []ValidationRule
}

func NewValidator(rules ...ValidationRule) *Validator {
	return &Validator{Rules: rules}
}

func newRule(name string, latency time.Duration, pass bool, msg string) ValidationRule {
	return ValidationRule{
		Name: name,
		Check: func() ValidationResult {
			start := time.Now()
			time.Sleep(latency)
			return ValidationResult{
				RuleName: name,
				Passed:   pass,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

func (v *Validator) RunAll() []ValidationResult {
	results := make(chan ValidationResult, len(v.Rules))

	for _, rule := range v.Rules {
		go func(r ValidationRule) {
			results <- r.Check()
		}(rule)
	}

	collected := make([]ValidationResult, 0, len(v.Rules))
	for i := 0; i < len(v.Rules); i++ {
		collected = append(collected, <-results)
	}
	return collected
}

func runSequential(rules []ValidationRule) []ValidationResult {
	results := make([]ValidationResult, 0, len(rules))
	for _, rule := range rules {
		results = append(results, rule.Check())
	}
	return results
}

func printResults(results []ValidationResult, elapsed time.Duration) {
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %-30s %v  %s\n", status, r.RuleName, r.Duration.Round(time.Millisecond), r.Message)
	}
	fmt.Printf("\n  Total wall-clock time: %v\n", elapsed.Round(time.Millisecond))
}

func main() {
	rules := []ValidationRule{
		newRule("namespace-exists", 80*time.Millisecond, true, "namespace 'production' found"),
		newRule("resource-limits-set", 120*time.Millisecond, true, "CPU and memory limits defined"),
		newRule("image-tag-not-latest", 60*time.Millisecond, false, "image uses 'latest' tag -- pin to specific version"),
	}

	fmt.Println("=== Sequential (3 rules) ===")
	fmt.Println()
	start := time.Now()
	seqResults := runSequential(rules)
	seqElapsed := time.Since(start)
	printResults(seqResults, seqElapsed)

	fmt.Println()
	fmt.Println("=== Concurrent (3 rules) ===")
	fmt.Println()
	validator := NewValidator(rules...)
	start = time.Now()
	concResults := validator.RunAll()
	concElapsed := time.Since(start)
	printResults(concResults, concElapsed)

	fmt.Printf("\n  Speedup: %.1fx\n", float64(seqElapsed)/float64(concElapsed))
}
```

**What's happening here:** `Validator.RunAll` launches one goroutine per rule and collects results from a buffered channel. The three checks now run simultaneously, so the total time drops from ~260ms to ~120ms (the slowest individual check).

**Key insight:** The buffered channel with `cap == len(rules)` ensures no goroutine blocks on send. Each goroutine completes its check and immediately writes the result. The main goroutine reads exactly `len(rules)` results, which guarantees all goroutines finish before proceeding.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Sequential (3 rules) ===

  [PASS] namespace-exists                80ms  namespace 'production' found
  [PASS] resource-limits-set             120ms  CPU and memory limits defined
  [FAIL] image-tag-not-latest            60ms  image uses 'latest' tag -- pin to specific version

  Total wall-clock time: 260ms

=== Concurrent (3 rules) ===

  [FAIL] image-tag-not-latest            60ms  image uses 'latest' tag -- pin to specific version
  [PASS] namespace-exists                80ms  namespace 'production' found
  [PASS] resource-limits-set             120ms  CPU and memory limits defined

  Total wall-clock time: 120ms

  Speedup: 2.2x
```


## Step 3 -- Full Kubernetes Pre-Flight with 8 Checks

Scale to a realistic set of 8 Kubernetes pre-flight validations. Each simulates a different check with varying latency.

```go
package main

import (
	"fmt"
	"time"
)

type ValidationResult struct {
	RuleName string
	Passed   bool
	Message  string
	Duration time.Duration
}

type ValidationRule struct {
	Name  string
	Check func() ValidationResult
}

type Validator struct {
	Rules []ValidationRule
}

func NewValidator(rules ...ValidationRule) *Validator {
	return &Validator{Rules: rules}
}

func (v *Validator) RunAll() []ValidationResult {
	results := make(chan ValidationResult, len(v.Rules))

	for _, rule := range v.Rules {
		go func(r ValidationRule) {
			results <- r.Check()
		}(rule)
	}

	collected := make([]ValidationResult, 0, len(v.Rules))
	for i := 0; i < len(v.Rules); i++ {
		collected = append(collected, <-results)
	}
	return collected
}

func makeCheck(name string, latency time.Duration, pass bool, msg string) ValidationRule {
	return ValidationRule{
		Name: name,
		Check: func() ValidationResult {
			start := time.Now()
			time.Sleep(latency)
			return ValidationResult{
				RuleName: name,
				Passed:   pass,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

func kubernetesPreflightRules() []ValidationRule {
	return []ValidationRule{
		makeCheck("namespace-exists",
			80*time.Millisecond, true,
			"namespace 'production' exists in cluster"),
		makeCheck("resource-limits-set",
			120*time.Millisecond, true,
			"CPU: 500m/1000m, Memory: 256Mi/512Mi"),
		makeCheck("image-tag-pinned",
			60*time.Millisecond, false,
			"image 'api-server:latest' uses mutable tag -- pin to SHA or semver"),
		makeCheck("replicas-in-range",
			90*time.Millisecond, true,
			"replicas=3, within allowed range [2, 10]"),
		makeCheck("required-labels-present",
			70*time.Millisecond, true,
			"labels: app, team, environment all present"),
		makeCheck("pdb-exists",
			150*time.Millisecond, false,
			"no PodDisruptionBudget found for deployment 'api-server'"),
		makeCheck("service-account-exists",
			110*time.Millisecond, true,
			"ServiceAccount 'api-server-sa' found with correct RBAC"),
		makeCheck("network-policy-attached",
			200*time.Millisecond, true,
			"NetworkPolicy 'api-server-netpol' allows ingress on port 8080"),
	}
}

func printResults(results []ValidationResult) {
	for _, r := range results {
		status := "PASS"
		marker := " "
		if !r.Passed {
			status = "FAIL"
			marker = "!"
		}
		fmt.Printf("  %s [%s] %-28s %6v  %s\n",
			marker, status, r.RuleName, r.Duration.Round(time.Millisecond), r.Message)
	}
}

func main() {
	rules := kubernetesPreflightRules()
	validator := NewValidator(rules...)

	fmt.Println("=== Kubernetes Pre-Flight Validation ===")
	fmt.Printf("  Running %d checks concurrently...\n\n", len(rules))

	start := time.Now()
	results := validator.RunAll()
	elapsed := time.Since(start)

	printResults(results)

	passed, failed := 0, 0
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}

	fmt.Printf("\n  Checks: %d passed, %d failed, %d total\n", passed, failed, len(results))
	fmt.Printf("  Wall-clock time: %v\n", elapsed.Round(time.Millisecond))

	var seqEstimate time.Duration
	for _, r := range results {
		seqEstimate += r.Duration
	}
	fmt.Printf("  Sequential estimate: %v\n", seqEstimate.Round(time.Millisecond))
	fmt.Printf("  Speedup: %.1fx\n", float64(seqEstimate)/float64(elapsed))
}
```

**What's happening here:** Eight Kubernetes-style validations run concurrently. Two fail: the image tag check and the PDB check. Results arrive in completion order (fastest first), giving immediate feedback on quick checks while slower ones are still running. The wall-clock time equals the slowest check (~200ms), not the sum (~880ms).

**Key insight:** In a real CI/CD pipeline, this pattern lets you add new validations without increasing total check time (as long as the new check is faster than the current slowest). The validation set is open for extension -- just append another rule to the slice.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies -- results arrive fastest first):
```
=== Kubernetes Pre-Flight Validation ===
  Running 8 checks concurrently...

  ! [FAIL] image-tag-pinned               60ms  image 'api-server:latest' uses mutable tag -- pin to SHA or semver
    [PASS] required-labels-present         70ms  labels: app, team, environment all present
    [PASS] namespace-exists                80ms  namespace 'production' exists in cluster
    [PASS] replicas-in-range               90ms  replicas=3, within allowed range [2, 10]
    [PASS] service-account-exists         110ms  ServiceAccount 'api-server-sa' found with correct RBAC
    [PASS] resource-limits-set            120ms  CPU: 500m/1000m, Memory: 256Mi/512Mi
  ! [FAIL] pdb-exists                     150ms  no PodDisruptionBudget found for deployment 'api-server'
    [PASS] network-policy-attached        200ms  NetworkPolicy 'api-server-netpol' allows ingress on port 8080

  Checks: 6 passed, 2 failed, 8 total
  Wall-clock time: 200ms
  Sequential estimate: 880ms
  Speedup: 4.4x
```


## Step 4 -- Formatted Report with Verdict

Build a `ValidationReport` that aggregates results into a structured, printable report with a final pass/fail verdict.

```go
package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type ValidationResult struct {
	RuleName string
	Passed   bool
	Message  string
	Duration time.Duration
}

type ValidationRule struct {
	Name  string
	Check func() ValidationResult
}

type ValidationReport struct {
	Results      []ValidationResult
	PassedCount  int
	FailedCount  int
	TotalTime    time.Duration
	WallClock    time.Duration
	Verdict      string
}

type Validator struct {
	Rules []ValidationRule
}

func NewValidator(rules ...ValidationRule) *Validator {
	return &Validator{Rules: rules}
}

func (v *Validator) RunAll() ([]ValidationResult, time.Duration) {
	results := make(chan ValidationResult, len(v.Rules))

	start := time.Now()
	for _, rule := range v.Rules {
		go func(r ValidationRule) {
			results <- r.Check()
		}(rule)
	}

	collected := make([]ValidationResult, 0, len(v.Rules))
	for i := 0; i < len(v.Rules); i++ {
		collected = append(collected, <-results)
	}
	wallClock := time.Since(start)

	sort.Slice(collected, func(i, j int) bool {
		return collected[i].Duration < collected[j].Duration
	})

	return collected, wallClock
}

func BuildReport(results []ValidationResult, wallClock time.Duration) ValidationReport {
	report := ValidationReport{
		Results:   results,
		WallClock: wallClock,
	}

	for _, r := range results {
		report.TotalTime += r.Duration
		if r.Passed {
			report.PassedCount++
		} else {
			report.FailedCount++
		}
	}

	if report.FailedCount == 0 {
		report.Verdict = "DEPLOY APPROVED"
	} else {
		report.Verdict = "DEPLOY BLOCKED"
	}

	return report
}

func PrintReport(report ValidationReport) {
	width := 78
	border := strings.Repeat("=", width)

	fmt.Println(border)
	fmt.Println("  KUBERNETES PRE-FLIGHT VALIDATION REPORT")
	fmt.Println(border)
	fmt.Println()

	fmt.Printf("  %-3s %-28s %6s  %s\n", "   ", "CHECK", "TIME", "DETAILS")
	fmt.Printf("  %-3s %-28s %6s  %s\n", "---", "-----", "----", "-------")

	for i, r := range report.Results {
		status := "[OK]"
		if !r.Passed {
			status = "[!!]"
		}
		fmt.Printf("  %s %-28s %5v  %s\n",
			status, r.RuleName, r.Duration.Round(time.Millisecond), r.Message)
		if i == len(report.Results)-1 {
			fmt.Println()
		}
	}

	fmt.Println(strings.Repeat("-", width))
	fmt.Printf("  Passed:     %d/%d\n", report.PassedCount, len(report.Results))
	fmt.Printf("  Failed:     %d/%d\n", report.FailedCount, len(report.Results))
	fmt.Printf("  Wall-clock: %v (concurrent)\n", report.WallClock.Round(time.Millisecond))
	fmt.Printf("  Sum of checks: %v (if sequential)\n", report.TotalTime.Round(time.Millisecond))
	fmt.Printf("  Time saved: %v\n", (report.TotalTime - report.WallClock).Round(time.Millisecond))
	fmt.Println(strings.Repeat("-", width))

	if report.FailedCount > 0 {
		fmt.Println()
		fmt.Println("  FAILED CHECKS:")
		for _, r := range report.Results {
			if !r.Passed {
				fmt.Printf("    - %s: %s\n", r.RuleName, r.Message)
			}
		}
	}

	fmt.Println()
	fmt.Println(border)
	fmt.Printf("  VERDICT: %s\n", report.Verdict)
	fmt.Println(border)
}

func makeCheck(name string, latency time.Duration, pass bool, msg string) ValidationRule {
	return ValidationRule{
		Name: name,
		Check: func() ValidationResult {
			start := time.Now()
			time.Sleep(latency)
			return ValidationResult{
				RuleName: name,
				Passed:   pass,
				Message:  msg,
				Duration: time.Since(start),
			}
		},
	}
}

func main() {
	rules := []ValidationRule{
		makeCheck("namespace-exists",
			80*time.Millisecond, true,
			"namespace 'production' exists"),
		makeCheck("resource-limits-set",
			120*time.Millisecond, true,
			"CPU: 500m/1000m, Memory: 256Mi/512Mi"),
		makeCheck("image-tag-pinned",
			60*time.Millisecond, false,
			"image 'api-server:latest' uses mutable tag"),
		makeCheck("replicas-in-range",
			90*time.Millisecond, true,
			"replicas=3, allowed [2, 10]"),
		makeCheck("required-labels",
			70*time.Millisecond, true,
			"app, team, environment present"),
		makeCheck("pdb-exists",
			150*time.Millisecond, false,
			"no PodDisruptionBudget for 'api-server'"),
		makeCheck("service-account",
			110*time.Millisecond, true,
			"ServiceAccount 'api-server-sa' with RBAC"),
		makeCheck("network-policy",
			200*time.Millisecond, true,
			"ingress allowed on port 8080"),
	}

	validator := NewValidator(rules...)
	results, wallClock := validator.RunAll()
	report := BuildReport(results, wallClock)
	PrintReport(report)
}
```

**What's happening here:** The `ValidationReport` aggregates pass/fail counts, timing data, and the list of failures into a single struct. `PrintReport` renders a structured report with a clear visual hierarchy: sorted results, summary statistics, isolated failure list, and a final verdict. The report shows both concurrent wall-clock time and the sequential estimate, making the speedup tangible.

**Key insight:** The verdict is binary -- any single failure blocks the deploy. This is a critical design decision in validation systems. In some scenarios you might want a "warn" level that logs but doesn't block. The struct-based approach makes it easy to add severity levels later without changing the validation runner.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
==============================================================================
  KUBERNETES PRE-FLIGHT VALIDATION REPORT
==============================================================================

     CHECK                          TIME  DETAILS
  --- -----                         ----  -------
  [!!] image-tag-pinned              60ms  image 'api-server:latest' uses mutable tag
  [OK] required-labels               70ms  app, team, environment present
  [OK] namespace-exists              80ms  namespace 'production' exists
  [OK] replicas-in-range             90ms  replicas=3, allowed [2, 10]
  [OK] service-account              110ms  ServiceAccount 'api-server-sa' with RBAC
  [OK] resource-limits-set          120ms  CPU: 500m/1000m, Memory: 256Mi/512Mi
  [!!] pdb-exists                   150ms  no PodDisruptionBudget for 'api-server'
  [OK] network-policy               200ms  ingress allowed on port 8080

------------------------------------------------------------------------------
  Passed:     6/8
  Failed:     2/8
  Wall-clock: 200ms (concurrent)
  Sum of checks: 880ms (if sequential)
  Time saved: 680ms
------------------------------------------------------------------------------

  FAILED CHECKS:
    - image-tag-pinned: image 'api-server:latest' uses mutable tag
    - pdb-exists: no PodDisruptionBudget for 'api-server'

==============================================================================
  VERDICT: DEPLOY BLOCKED
==============================================================================
```


## Common Mistakes

### Not Collecting All Results Before Deciding

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	results := make(chan bool, 3)

	checks := []struct {
		name    string
		latency time.Duration
		pass    bool
	}{
		{"fast-check", 20 * time.Millisecond, true},
		{"slow-check", 200 * time.Millisecond, false},
		{"medium-check", 80 * time.Millisecond, true},
	}

	for _, c := range checks {
		go func(pass bool, latency time.Duration) {
			time.Sleep(latency)
			results <- pass
		}(c.pass, c.latency)
	}

	// BUG: reads only first result, decides "all passed"
	if <-results {
		fmt.Println("Validation passed!") // wrong -- slow-check will fail
	}
	// Two goroutines are still running, their results are never read
}
```
**What happens:** The program reads only the first result (the fastest check, which passes) and declares success. The slow failing check is never read. In production, this means deploying broken manifests.

**Correct -- collect all results before deciding:**
```go
package main

import (
	"fmt"
	"time"
)

type Result struct {
	Name   string
	Passed bool
}

func main() {
	results := make(chan Result, 3)

	checks := []struct {
		name    string
		latency time.Duration
		pass    bool
	}{
		{"fast-check", 20 * time.Millisecond, true},
		{"slow-check", 200 * time.Millisecond, false},
		{"medium-check", 80 * time.Millisecond, true},
	}

	for _, c := range checks {
		go func(name string, pass bool, latency time.Duration) {
			time.Sleep(latency)
			results <- Result{Name: name, Passed: pass}
		}(c.name, c.pass, c.latency)
	}

	allPassed := true
	for i := 0; i < len(checks); i++ {
		r := <-results
		fmt.Printf("  [%s] %v\n", r.Name, r.Passed)
		if !r.Passed {
			allPassed = false
		}
	}
	fmt.Printf("  Verdict: %v\n", allPassed) // false -- correctly catches slow-check failure
}
```

### Launching Goroutines but Forgetting the Channel

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	allPassed := true // shared variable, no synchronization

	checks := []bool{true, false, true}
	for _, pass := range checks {
		wg.Add(1)
		go func(p bool) {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			if !p {
				allPassed = false // DATA RACE: multiple goroutines write to shared bool
			}
		}(pass)
	}
	wg.Wait()
	fmt.Println("All passed:", allPassed)
}
```
**What happens:** Multiple goroutines write to `allPassed` without synchronization. The race detector would flag this. Even if it happens to work, the code is wrong.

**Correct -- use a channel to collect results:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	results := make(chan bool, 3)

	checks := []bool{true, false, true}
	for _, pass := range checks {
		go func(p bool) {
			time.Sleep(50 * time.Millisecond)
			results <- p
		}(pass)
	}

	allPassed := true
	for i := 0; i < len(checks); i++ {
		if !<-results {
			allPassed = false
		}
	}
	fmt.Println("All passed:", allPassed) // false
}
```


## Verify What You Learned

Build a "CI pipeline validator" that:
1. Defines 6 validation rules: syntax check, unit tests pass, lint score above threshold, no security vulnerabilities, Docker image builds, integration tests pass
2. Each rule has a simulated latency between 100-500ms and a random pass/fail outcome (use `rand.Float32() < 0.8` for 80% pass rate)
3. Runs all 6 rules concurrently using a `Validator` struct
4. Builds a `ValidationReport` with pass/fail counts, wall-clock time, and sequential estimate
5. Prints the report with a "PIPELINE APPROVED" or "PIPELINE BLOCKED" verdict
6. Runs the validation 3 times to show that results vary due to randomness

**Hint:** Use `sort.Slice` on the results by duration to always display the fastest checks first, regardless of completion order.


## What's Next
Continue to [20-goroutine-safe-cache](../20-goroutine-safe-cache/20-goroutine-safe-cache.md) to build a cache with background cleanup goroutines and concurrent request handling.


## Summary
- Independent validations are ideal candidates for concurrent fan-out -- each rule runs in its own goroutine
- A buffered channel sized to the rule count collects results without blocking any goroutine
- Wall-clock time equals the slowest individual check, not the sum of all checks
- Always collect all results before making a pass/fail decision -- reading only the first result is a subtle but dangerous bug
- Structured result types (`ValidationResult`, `ValidationReport`) separate data collection from presentation
- The validation runner is decoupled from the rules themselves, making it easy to add new checks without changing the runner
- Sorting results by duration gives consistent, readable output regardless of goroutine completion order


## Reference
- [Go Spec: Channel types](https://go.dev/ref/spec#Channel_types)
- [Go Blog: Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [Effective Go: Parallelization](https://go.dev/doc/effective_go#parallel)
- [sort.Slice](https://pkg.go.dev/sort#Slice)
