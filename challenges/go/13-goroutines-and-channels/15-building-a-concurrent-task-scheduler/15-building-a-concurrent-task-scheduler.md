# 15. Building a Concurrent Task Scheduler

<!--
difficulty: insane
concepts: [dag-scheduler, topological-sort, task-dependencies, concurrent-execution, backpressure]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [goroutines, channels, waitgroup, done-channel-pattern, goroutine-pools, deadlock-detection]
-->

## The Challenge

Design and implement a concurrent task scheduler that executes tasks according to a dependency graph (DAG). Tasks without dependencies run in parallel. A task only starts when all its dependencies have completed successfully. If a task fails, all tasks that depend on it are skipped.

## Requirements

1. **Task Definition** -- each task has a name, a function to execute, and a list of dependency names
2. **DAG Validation** -- detect cycles before execution and return an error
3. **Parallel Execution** -- tasks with no unmet dependencies run concurrently, bounded by a configurable max-concurrency
4. **Error Propagation** -- if a task fails, all downstream dependents are marked as skipped
5. **Result Reporting** -- after execution, report which tasks succeeded, failed, or were skipped, with durations
6. **Cancellation** -- accept a `context.Context` for external cancellation
7. Must pass `go run -race`

## Hints

<details>
<summary>Hint 1: Data Structures</summary>

```go
type TaskStatus int

const (
	StatusPending TaskStatus = iota
	StatusRunning
	StatusDone
	StatusFailed
	StatusSkipped
)

type Task struct {
	Name    string
	Deps    []string
	Fn      func(ctx context.Context) error
}

type TaskResult struct {
	Name     string
	Status   TaskStatus
	Duration time.Duration
	Err      error
}
```
</details>

<details>
<summary>Hint 2: Cycle Detection with DFS</summary>

Use a depth-first search with three colors (white/gray/black) to detect cycles:

```go
func detectCycle(tasks map[string]*Task) error {
	white, gray, black := 0, 1, 2
	color := make(map[string]int)
	for name := range tasks {
		color[name] = white
	}

	var visit func(name string) error
	visit = func(name string) error {
		color[name] = gray
		t := tasks[name]
		for _, dep := range t.Deps {
			switch color[dep] {
			case gray:
				return fmt.Errorf("cycle detected: %s -> %s", name, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[name] = black
		return nil
	}

	for name := range tasks {
		if color[name] == white {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}
```
</details>

<details>
<summary>Hint 3: Ready Queue Approach</summary>

Track each task's remaining dependency count. When a task completes, decrement the count for all its dependents. Tasks whose count reaches zero are placed on a ready queue (channel). A pool of workers reads from the ready queue.

```go
type Scheduler struct {
	tasks       map[string]*Task
	depCount    map[string]int        // remaining unmet deps
	dependents  map[string][]string   // reverse graph: who depends on me
	results     map[string]*TaskResult
	mu          sync.Mutex
	ready       chan string
	maxWorkers  int
}
```

Seed the ready queue with tasks that have zero dependencies.
</details>

<details>
<summary>Hint 4: Skeleton Implementation</summary>

```go
func (s *Scheduler) Run(ctx context.Context) map[string]*TaskResult {
	// 1. Build reverse dependency graph and count deps
	// 2. Seed ready queue with zero-dep tasks
	// 3. Launch worker pool
	// 4. Each worker:
	//    a. Read task name from ready queue
	//    b. Execute task function
	//    c. On success: decrement dependents' dep counts, enqueue newly ready tasks
	//    d. On failure: mark all transitive dependents as skipped
	// 5. Wait for all tasks to be processed
	// 6. Return results
}
```
</details>

<details>
<summary>Hint 5: Complete Example</summary>

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type TaskStatus int

const (
	StatusPending TaskStatus = iota
	StatusRunning
	StatusDone
	StatusFailed
	StatusSkipped
)

func (s TaskStatus) String() string {
	return [...]string{"PENDING", "RUNNING", "DONE", "FAILED", "SKIPPED"}[s]
}

type Task struct {
	Name string
	Deps []string
	Fn   func(ctx context.Context) error
}

type TaskResult struct {
	Name     string
	Status   TaskStatus
	Duration time.Duration
	Err      error
}

type Scheduler struct {
	tasks      map[string]*Task
	depCount   map[string]int
	dependents map[string][]string
	results    map[string]*TaskResult
	mu         sync.Mutex
	maxWorkers int
}

func NewScheduler(maxWorkers int) *Scheduler {
	return &Scheduler{
		tasks:      make(map[string]*Task),
		depCount:   make(map[string]int),
		dependents: make(map[string][]string),
		results:    make(map[string]*TaskResult),
		maxWorkers: maxWorkers,
	}
}

func (s *Scheduler) AddTask(t Task) {
	s.tasks[t.Name] = &t
	s.depCount[t.Name] = len(t.Deps)
	for _, dep := range t.Deps {
		s.dependents[dep] = append(s.dependents[dep], t.Name)
	}
}

func (s *Scheduler) detectCycle() error {
	white, gray, black := 0, 1, 2
	color := make(map[string]int)
	for name := range s.tasks {
		color[name] = white
	}
	var visit func(string) error
	visit = func(name string) error {
		color[name] = gray
		for _, dep := range s.tasks[name].Deps {
			if _, ok := s.tasks[dep]; !ok {
				return fmt.Errorf("unknown dependency: %s -> %s", name, dep)
			}
			switch color[dep] {
			case gray:
				return fmt.Errorf("cycle: %s -> %s", name, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[name] = black
		return nil
	}
	for name := range s.tasks {
		if color[name] == white {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Scheduler) skipDependents(name string) {
	for _, dep := range s.dependents[name] {
		if s.results[dep] == nil {
			s.results[dep] = &TaskResult{Name: dep, Status: StatusSkipped}
			s.skipDependents(dep)
		}
	}
}

func (s *Scheduler) Run(ctx context.Context) (map[string]*TaskResult, error) {
	if err := s.detectCycle(); err != nil {
		return nil, err
	}

	ready := make(chan string, len(s.tasks))
	var remaining sync.WaitGroup

	// Seed with zero-dep tasks
	for name, count := range s.depCount {
		if count == 0 {
			remaining.Add(1)
			ready <- name
		}
	}

	// Workers
	var workerWg sync.WaitGroup
	for i := 0; i < s.maxWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for taskName := range ready {
				s.mu.Lock()
				if s.results[taskName] != nil {
					s.mu.Unlock()
					remaining.Done()
					continue
				}
				s.results[taskName] = &TaskResult{Name: taskName, Status: StatusRunning}
				s.mu.Unlock()

				start := time.Now()
				err := s.tasks[taskName].Fn(ctx)
				dur := time.Since(start)

				s.mu.Lock()
				if err != nil {
					s.results[taskName] = &TaskResult{Name: taskName, Status: StatusFailed, Duration: dur, Err: err}
					s.skipDependents(taskName)
				} else {
					s.results[taskName] = &TaskResult{Name: taskName, Status: StatusDone, Duration: dur}
					for _, dep := range s.dependents[taskName] {
						s.depCount[dep]--
						if s.depCount[dep] == 0 && s.results[dep] == nil {
							remaining.Add(1)
							ready <- dep
						}
					}
				}
				s.mu.Unlock()
				remaining.Done()
			}
		}()
	}

	remaining.Wait()
	close(ready)
	workerWg.Wait()

	return s.results, nil
}

func main() {
	s := NewScheduler(3)

	s.AddTask(Task{Name: "fetch-data", Fn: func(ctx context.Context) error {
		fmt.Println("  fetching data...")
		time.Sleep(100 * time.Millisecond)
		return nil
	}})
	s.AddTask(Task{Name: "parse-data", Deps: []string{"fetch-data"}, Fn: func(ctx context.Context) error {
		fmt.Println("  parsing data...")
		time.Sleep(50 * time.Millisecond)
		return nil
	}})
	s.AddTask(Task{Name: "validate", Deps: []string{"parse-data"}, Fn: func(ctx context.Context) error {
		fmt.Println("  validating...")
		time.Sleep(30 * time.Millisecond)
		return nil
	}})
	s.AddTask(Task{Name: "fetch-config", Fn: func(ctx context.Context) error {
		fmt.Println("  fetching config...")
		time.Sleep(80 * time.Millisecond)
		return nil
	}})
	s.AddTask(Task{Name: "build", Deps: []string{"validate", "fetch-config"}, Fn: func(ctx context.Context) error {
		fmt.Println("  building...")
		time.Sleep(60 * time.Millisecond)
		return nil
	}})
	s.AddTask(Task{Name: "test", Deps: []string{"build"}, Fn: func(ctx context.Context) error {
		fmt.Println("  testing...")
		time.Sleep(40 * time.Millisecond)
		return fmt.Errorf("test failure: 2 tests failed")
	}})
	s.AddTask(Task{Name: "deploy", Deps: []string{"test"}, Fn: func(ctx context.Context) error {
		fmt.Println("  deploying...")
		time.Sleep(50 * time.Millisecond)
		return nil
	}})

	results, err := s.Run(context.Background())
	if err != nil {
		fmt.Printf("scheduler error: %v\n", err)
		return
	}

	fmt.Println("\n=== Results ===")
	for name, r := range results {
		if r.Err != nil {
			fmt.Printf("  %-15s %s (%v) -- %v\n", name, r.Status, r.Duration.Truncate(time.Millisecond), r.Err)
		} else {
			fmt.Printf("  %-15s %s (%v)\n", name, r.Status, r.Duration.Truncate(time.Millisecond))
		}
	}
}
```
</details>

## Success Criteria

1. Tasks with no dependencies execute concurrently
2. A task only starts after all its dependencies have completed successfully
3. Cycle detection prevents infinite loops and returns a clear error message
4. If a task fails, all downstream dependents are marked as `SKIPPED` (not executed)
5. The scheduler respects max-concurrency (at most N tasks run simultaneously)
6. `go run -race main.go` produces no race warnings
7. Context cancellation stops pending tasks promptly

Test with:

```bash
go run -race main.go
```

## Research Resources

- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [errgroup package](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Topological Sorting (Wikipedia)](https://en.wikipedia.org/wiki/Topological_sorting)
- [Kahn's Algorithm](https://en.wikipedia.org/wiki/Topological_sorting#Kahn's_algorithm)
- [context package documentation](https://pkg.go.dev/context)

## What's Next

Continue to [16 - Goroutine Debugging Under Load](../16-goroutine-debugging-under-load/16-goroutine-debugging-under-load.md) to learn how to diagnose goroutine issues in production systems under load.

## Summary

- A DAG scheduler models task dependencies as a directed acyclic graph
- Topological ordering determines execution sequence; cycle detection prevents hangs
- Track unmet dependency counts; enqueue tasks when their count reaches zero
- Worker pools bound concurrency; skipping dependents on failure prevents wasted work
- Always validate the graph before execution and support external cancellation via context
