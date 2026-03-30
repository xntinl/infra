---
difficulty: intermediate
concepts: [context.WithDeadline, absolute deadline, SLA enforcement, time.Now, DeadlineExceeded, remaining budget]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 4. Context WithDeadline

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** a context with an absolute deadline using `context.WithDeadline`
- **Build** an SLA enforcer that ensures a request completes by a specific time
- **Track** remaining time budget as a request passes through processing stages
- **Choose** between `WithDeadline` and `WithTimeout` based on the situation

## Why WithDeadline

While `WithTimeout` specifies "cancel after this duration from now," `WithDeadline` specifies "cancel at this exact point in time." The distinction matters when deadlines are set externally.

Consider a real scenario: an API gateway receives a request at 14:00:00 with an SLA deadline of 14:00:05 (the client expects a response within 5 seconds of sending the request). After network latency and middleware processing, your handler receives it at 14:00:02. You should propagate the original deadline (14:00:05), not create a new 5-second timeout from 14:00:02 -- that would extend the deadline to 14:00:07, violating the SLA contract.

`WithDeadline` is the lower-level primitive. `WithTimeout(parent, d)` is implemented internally as `WithDeadline(parent, time.Now().Add(d))`. Understanding both lets you choose the right tool: `WithTimeout` for relative durations ("timeout after 2 seconds"), `WithDeadline` for absolute points in time ("must complete by 14:00:05").

## Step 1 -- SLA Enforcer: Request Must Complete by Absolute Time

Build an SLA enforcer that sets an absolute deadline for request processing. Multiple stages must complete before the deadline expires:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

const slaBudget = 500 * time.Millisecond

type PipelineStage struct {
	Name     string
	Duration time.Duration
}

type SLAEnforcer struct {
	stages []PipelineStage
}

func NewSLAEnforcer(stages []PipelineStage) *SLAEnforcer {
	return &SLAEnforcer{stages: stages}
}

func (e *SLAEnforcer) processStage(ctx context.Context, stage PipelineStage) error {
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline).Round(time.Millisecond)
	fmt.Printf("[%-12s] starting (budget remaining: %v, needs: %v)\n",
		stage.Name, remaining, stage.Duration)

	if remaining < stage.Duration {
		fmt.Printf("[%-12s] WARNING: insufficient budget, may timeout\n", stage.Name)
	}

	select {
	case <-time.After(stage.Duration):
		fmt.Printf("[%-12s] completed\n", stage.Name)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", stage.Name, ctx.Err())
	}
}

func (e *SLAEnforcer) Execute(ctx context.Context) error {
	for _, stage := range e.stages {
		if err := e.processStage(ctx, stage); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	slaDeadline := time.Now().Add(slaBudget)
	ctx, cancel := context.WithDeadline(context.Background(), slaDeadline)
	defer cancel()

	fmt.Printf("SLA deadline: %v\n", slaDeadline.Format("15:04:05.000"))
	fmt.Printf("Current time: %v\n\n", time.Now().Format("15:04:05.000"))

	enforcer := NewSLAEnforcer([]PipelineStage{
		{Name: "auth", Duration: 80 * time.Millisecond},
		{Name: "validation", Duration: 60 * time.Millisecond},
		{Name: "processing", Duration: 120 * time.Millisecond},
		{Name: "persistence", Duration: 100 * time.Millisecond},
	})

	if err := enforcer.Execute(ctx); err != nil {
		fmt.Printf("\nSLA VIOLATED: %v\n", err)
		fmt.Printf("Deadline was: %v\n", slaDeadline.Format("15:04:05.000"))
		fmt.Printf("Failed at:    %v\n", time.Now().Format("15:04:05.000"))
		return
	}

	fmt.Printf("\nSLA MET: all stages completed before %v\n",
		slaDeadline.Format("15:04:05.000"))
}
```

### Verification
```bash
go run main.go
```
Expected output (times will vary):
```
SLA deadline: 14:30:01.500
Current time: 14:30:01.000

[auth        ] starting (budget remaining: 499ms, needs: 80ms)
[auth        ] completed
[validation  ] starting (budget remaining: 419ms, needs: 60ms)
[validation  ] completed
[processing  ] starting (budget remaining: 358ms, needs: 120ms)
[processing  ] completed
[persistence ] starting (budget remaining: 237ms, needs: 100ms)
[persistence ] completed

SLA MET: all stages completed before 14:30:01.500
```

Each stage reports how much budget remains. You can see the budget shrinking as each stage consumes time. This is how real request pipelines work -- middleware, business logic, and data access all share a single request deadline.

## Step 2 -- SLA Violation: Budget Runs Out Mid-Request

Now increase the processing time so the deadline is exceeded during one of the stages:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

const tightSLABudget = 400 * time.Millisecond

type PipelineStage struct {
	Name     string
	Duration time.Duration
}

type SLAEnforcer struct {
	stages []PipelineStage
}

func NewSLAEnforcer(stages []PipelineStage) *SLAEnforcer {
	return &SLAEnforcer{stages: stages}
}

func (e *SLAEnforcer) processStage(ctx context.Context, stage PipelineStage) error {
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline).Round(time.Millisecond)
	fmt.Printf("[%-12s] starting (remaining: %v)\n", stage.Name, remaining)

	select {
	case <-time.After(stage.Duration):
		fmt.Printf("[%-12s] completed in %v\n", stage.Name, stage.Duration)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s timed out after %v: %w", stage.Name,
			stage.Duration-time.Until(deadline), ctx.Err())
	}
}

func (e *SLAEnforcer) Execute(ctx context.Context) error {
	for _, stage := range e.stages {
		if err := e.processStage(ctx, stage); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	slaDeadline := time.Now().Add(tightSLABudget)
	ctx, cancel := context.WithDeadline(context.Background(), slaDeadline)
	defer cancel()

	fmt.Printf("SLA budget: %v\n", tightSLABudget)
	fmt.Println("Stages: auth(80ms) + validate(60ms) + process(300ms) + persist(100ms) = 540ms")
	fmt.Println("Expected: SLA violation during 'process' stage\n")

	enforcer := NewSLAEnforcer([]PipelineStage{
		{Name: "auth", Duration: 80 * time.Millisecond},
		{Name: "validate", Duration: 60 * time.Millisecond},
		{Name: "process", Duration: 300 * time.Millisecond},
		{Name: "persist", Duration: 100 * time.Millisecond},
	})

	if err := enforcer.Execute(ctx); err != nil {
		fmt.Printf("\nSLA VIOLATED: %v\n", err)
		return
	}
	fmt.Println("SLA MET")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
SLA budget: 400ms
Stages: auth(80ms) + validate(60ms) + process(300ms) + persist(100ms) = 540ms
Expected: SLA violation during 'process' stage

[auth        ] starting (remaining: 400ms)
[auth        ] completed in 80ms
[validate    ] starting (remaining: 319ms)
[validate    ] completed in 60ms
[process     ] starting (remaining: 259ms)

SLA VIOLATED: process timed out after 300ms: context deadline exceeded
```

The "process" stage needed 300ms but only 259ms remained. The context deadline fired automatically, and the error tells you exactly which stage failed and why.

## Step 3 -- Comparing WithDeadline and WithTimeout

Show that `WithTimeout(parent, d)` is exactly `WithDeadline(parent, time.Now().Add(d))`. This matters when you need to choose between them:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

const comparisonDuration = 2 * time.Second

func main() {
	now := time.Now()

	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), comparisonDuration)
	defer cancelTimeout()

	ctxDeadline, cancelDeadline := context.WithDeadline(context.Background(), now.Add(comparisonDuration))
	defer cancelDeadline()

	deadlineFromTimeout, _ := ctxTimeout.Deadline()
	deadlineFromDeadline, _ := ctxDeadline.Deadline()

	diff := deadlineFromTimeout.Sub(deadlineFromDeadline).Abs()
	fmt.Printf("WithTimeout  deadline: %v\n", deadlineFromTimeout.Format("15:04:05.000000"))
	fmt.Printf("WithDeadline deadline: %v\n", deadlineFromDeadline.Format("15:04:05.000000"))
	fmt.Printf("Difference: %v (microseconds apart)\n\n", diff)

	fmt.Println("WHEN TO USE WHICH:")
	fmt.Println("  WithTimeout  -> relative: 'give this 2 seconds from now'")
	fmt.Println("  WithDeadline -> absolute: 'must finish by 14:00:05'")
	fmt.Println()
	fmt.Println("USE WithDeadline when:")
	fmt.Println("  - Propagating an SLA deadline from an upstream caller")
	fmt.Println("  - A gRPC/HTTP header carries an absolute deadline")
	fmt.Println("  - A batch job must finish before a maintenance window")
	fmt.Println()
	fmt.Println("USE WithTimeout when:")
	fmt.Println("  - Setting a per-call timeout on a database query")
	fmt.Println("  - Giving an HTTP request N seconds to complete")
	fmt.Println("  - Any 'max duration' from the current moment")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
WithTimeout  deadline: 14:30:03.000000
WithDeadline deadline: 14:30:03.000000
Difference: 50us (microseconds apart)

WHEN TO USE WHICH:
  WithTimeout  -> relative: 'give this 2 seconds from now'
  WithDeadline -> absolute: 'must finish by 14:00:05'

USE WithDeadline when:
  - Propagating an SLA deadline from an upstream caller
  - A gRPC/HTTP header carries an absolute deadline
  - A batch job must finish before a maintenance window

USE WithTimeout when:
  - Setting a per-call timeout on a database query
  - Giving an HTTP request N seconds to complete
  - Any 'max duration' from the current moment
```

## Step 4 -- Fail Fast: Check Budget Before Starting Expensive Work

In a real system, you should check whether you have enough budget before starting an expensive operation. Starting a 500ms database query with only 100ms of budget left is wasteful:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

const queryBudget = 300 * time.Millisecond

type QueryExecutor struct{}

func NewQueryExecutor() *QueryExecutor {
	return &QueryExecutor{}
}

func (qe *QueryExecutor) Execute(ctx context.Context, query string, estimatedDuration time.Duration) (string, error) {
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		remaining := time.Until(deadline)
		if remaining < estimatedDuration {
			return "", fmt.Errorf(
				"query %q needs ~%v but only %v remains -- skipping to avoid wasted work",
				query, estimatedDuration, remaining.Round(time.Millisecond))
		}
		fmt.Printf("[db] executing %q (needs ~%v, budget: %v)\n",
			query, estimatedDuration, remaining.Round(time.Millisecond))
	}

	select {
	case <-time.After(estimatedDuration):
		return fmt.Sprintf("results for: %s", query), nil
	case <-ctx.Done():
		return "", fmt.Errorf("query %q: %w", query, ctx.Err())
	}
}

func main() {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(queryBudget))
	defer cancel()

	executor := NewQueryExecutor()

	result, err := executor.Execute(ctx, "SELECT * FROM users LIMIT 10", 100*time.Millisecond)
	if err != nil {
		fmt.Printf("[error] %v\n", err)
	} else {
		fmt.Printf("[ok]    %s\n", result)
	}

	result, err = executor.Execute(ctx, "SELECT * FROM orders JOIN ...", 500*time.Millisecond)
	if err != nil {
		fmt.Printf("[error] %v\n", err)
	} else {
		fmt.Printf("[ok]    %s\n", result)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[db] executing "SELECT * FROM users LIMIT 10" (needs ~100ms, budget: 300ms)
[ok]    results for: SELECT * FROM users LIMIT 10
[error] query "SELECT * FROM orders JOIN ..." needs ~500ms but only 199ms remains -- skipping to avoid wasted work
```

The second query detects that it does not have enough budget and fails immediately instead of starting work that is guaranteed to timeout. This saves database connections and CPU.

## Step 5 -- Remaining Budget Decreases Through Layers

Show how a single deadline propagates through multiple service layers, with each layer seeing less remaining budget:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

const (
	requestBudget     = 500 * time.Millisecond
	gatewayDelay      = 50 * time.Millisecond
	serviceDelay      = 80 * time.Millisecond
	repositoryDelay   = 100 * time.Millisecond
)

type RequestPipeline struct{}

func NewRequestPipeline() *RequestPipeline {
	return &RequestPipeline{}
}

func (p *RequestPipeline) logBudget(ctx context.Context, layer string) {
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline).Round(time.Millisecond)
	fmt.Printf("[%-12s] budget remaining: %v\n", layer, remaining)
}

func (p *RequestPipeline) Gateway(ctx context.Context) (string, error) {
	p.logBudget(ctx, "gateway")
	time.Sleep(gatewayDelay)
	return p.Service(ctx)
}

func (p *RequestPipeline) Service(ctx context.Context) (string, error) {
	p.logBudget(ctx, "service")
	time.Sleep(serviceDelay)
	return p.Repository(ctx)
}

func (p *RequestPipeline) Repository(ctx context.Context) (string, error) {
	p.logBudget(ctx, "repository")
	select {
	case <-time.After(repositoryDelay):
		return "data", nil
	case <-ctx.Done():
		return "", fmt.Errorf("repository: %w", ctx.Err())
	}
}

func main() {
	deadline := time.Now().Add(requestBudget)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	fmt.Printf("Absolute deadline: %v\n\n", deadline.Format("15:04:05.000"))

	pipeline := NewRequestPipeline()
	result, err := pipeline.Gateway(ctx)
	if err != nil {
		fmt.Printf("\nFailed: %v\n", err)
	} else {
		fmt.Printf("\nResult: %s\n", result)
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Absolute deadline: 14:30:01.500

[gateway     ] budget remaining: 500ms
[service     ] budget remaining: 449ms
[repository  ] budget remaining: 369ms

Result: data
```

The same absolute deadline is visible at every layer. Each layer sees less remaining time because previous layers consumed part of the budget. This is the natural behavior of deadline propagation -- no layer needs to compute its own timeout.

## Common Mistakes

### Confusing Deadline with Timeout
**Wrong:** Using `WithDeadline` with a duration instead of an absolute time:
```go
ctx, cancel := context.WithDeadline(parent, 5*time.Second) // compile error: wrong type
```
**Fix:** `WithDeadline` takes a `time.Time`, not a `time.Duration`:
```go
ctx, cancel := context.WithDeadline(parent, time.Now().Add(5*time.Second))
// or simply:
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
```

### Assuming Child Can Extend Parent Deadline
A child context always gets the minimum of its own deadline and its parent's. You cannot use `WithDeadline` to grant more time than the parent allows. The parent's SLA is a hard ceiling.

### Not Checking Deadline Before Starting Expensive Work
As shown in Step 4, always check the remaining budget before starting operations with known minimum durations. Starting work that is guaranteed to timeout wastes connections, CPU, and may cause lock contention in the database.

## Verify What You Learned

Build a request pipeline with a 350ms SLA. Three stages need 100ms each (total 300ms). Run it twice: once with enough budget and once with an artificially tight budget to see which stage gets cut off:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type PipelineStage struct {
	Name     string
	Duration time.Duration
}

type RequestPipeline struct {
	stages []PipelineStage
}

func NewRequestPipeline(stages []PipelineStage) *RequestPipeline {
	return &RequestPipeline{stages: stages}
}

func (p *RequestPipeline) Run(label string, budget time.Duration) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(budget))
	defer cancel()

	for _, s := range p.stages {
		deadline, _ := ctx.Deadline()
		remaining := time.Until(deadline).Round(time.Millisecond)
		fmt.Printf("  [%-13s] remaining: %v\n", s.Name, remaining)

		select {
		case <-time.After(s.Duration):
		case <-ctx.Done():
			fmt.Printf("  %s: FAILED at %s: %v\n", label, s.Name, ctx.Err())
			return
		}
	}
	fmt.Printf("  %s: all stages completed\n", label)
}

func main() {
	stages := []PipelineStage{
		{Name: "authenticate", Duration: 100 * time.Millisecond},
		{Name: "authorize", Duration: 100 * time.Millisecond},
		{Name: "execute", Duration: 100 * time.Millisecond},
	}
	pipeline := NewRequestPipeline(stages)

	fmt.Println("=== 350ms budget (300ms needed) ===")
	pipeline.Run("generous", 350*time.Millisecond)

	fmt.Println("\n=== 220ms budget (300ms needed) ===")
	pipeline.Run("tight", 220*time.Millisecond)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== 350ms budget (300ms needed) ===
  [authenticate ] remaining: 350ms
  [authorize    ] remaining: 249ms
  [execute      ] remaining: 149ms
  generous: all stages completed

=== 220ms budget (300ms needed) ===
  [authenticate ] remaining: 220ms
  [authorize    ] remaining: 119ms
  tight: FAILED at authorize: context deadline exceeded
```

## What's Next
Continue to [05-context-withvalue](../05-context-withvalue/05-context-withvalue.md) to learn how to attach request-scoped data -- request IDs, user IDs, and trace IDs -- to contexts for logging and tracing.

## Summary
- `context.WithDeadline(parent, time)` cancels the context at an absolute point in time
- `WithTimeout(parent, d)` is equivalent to `WithDeadline(parent, time.Now().Add(d))`
- Use `WithDeadline` when propagating SLA deadlines from upstream callers
- Use `WithTimeout` when setting relative durations ("give this 2 seconds")
- `ctx.Deadline()` returns the deadline and whether one is set
- A child context inherits the shorter of its own and its parent's deadline
- Check remaining budget with `time.Until(deadline)` before starting expensive work -- fail fast
- Remaining budget naturally decreases as a request flows through layers

## Reference
- [Package context: WithDeadline](https://pkg.go.dev/context#WithDeadline)
- [Package context: WithTimeout](https://pkg.go.dev/context#WithTimeout)
- [time.Until](https://pkg.go.dev/time#Until)
