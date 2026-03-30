---
difficulty: advanced
concepts: [DAG resolution, topological sort, conditional goroutine launch, dependency tracking, error propagation, critical path]
tools: [go]
estimated_time: 55m
bloom_level: create
prerequisites: [goroutines, channels, sync.WaitGroup, graph concepts]
---


# 30. Goroutine DAG Orchestrator


## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a DAG-based task orchestrator that resolves dependencies and launches goroutines in correct order
- **Implement** topological sorting to validate the DAG and detect cycles
- **Propagate** errors through dependent tasks so that downstream tasks skip execution when upstream tasks fail
- **Identify** the critical path through the DAG and measure actual versus theoretical execution time


## Why a Goroutine DAG Orchestrator

Every build system (Make, Bazel), CI/CD pipeline (GitHub Actions, Jenkins), and workflow engine (Airflow, Temporal) solves the same core problem: execute tasks respecting dependency ordering while maximizing parallelism. Task C depends on A and B -- it cannot start until both complete. Task D depends on C. Tasks A and B have no dependencies on each other, so they run in parallel.

This is a capstone exercise that integrates nearly everything from this section: launching goroutines, coordinating with WaitGroups and channels, managing shared state with mutexes, handling errors, and designing goroutine ownership. The DAG orchestrator is not just an exercise -- it is a pattern you will use directly. Configuration management tools, data pipeline builders, deployment orchestrators, and even UI rendering engines use DAG resolution to determine execution order.

The critical path analysis adds a practical dimension: given the DAG and task durations, the critical path is the longest sequential chain. This determines the minimum possible execution time regardless of how many goroutines you use. Understanding the critical path tells you which tasks to optimize and where adding parallelism provides no benefit.


## Step 1 -- DAG Definition and Validation

Define the task graph, implement topological sort to determine valid execution order, and detect cycles that would cause deadlocks.

```go
package main

import (
	"fmt"
	"time"
)

type TaskStatus int

const (
	TaskPending   TaskStatus = iota
	TaskRunning
	TaskCompleted
	TaskFailed
	TaskSkipped
)

func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskRunning:
		return "running"
	case TaskCompleted:
		return "completed"
	case TaskFailed:
		return "failed"
	case TaskSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

type TaskDef struct {
	Name     string
	Duration time.Duration
	DependsOn []string
	Fn       func() error
}

type DAG struct {
	tasks    map[string]TaskDef
	order    []string
}

func NewDAG() *DAG {
	return &DAG{tasks: make(map[string]TaskDef)}
}

func (d *DAG) AddTask(def TaskDef) {
	d.tasks[def.Name] = def
	d.order = append(d.order, def.Name)
}

func (d *DAG) Validate() error {
	for name, task := range d.tasks {
		for _, dep := range task.DependsOn {
			if _, ok := d.tasks[dep]; !ok {
				return fmt.Errorf("task %q depends on unknown task %q", name, dep)
			}
		}
	}

	_, err := d.TopologicalSort()
	return err
}

func (d *DAG) TopologicalSort() ([]string, error) {
	inDegree := make(map[string]int, len(d.tasks))
	dependents := make(map[string][]string, len(d.tasks))

	for name := range d.tasks {
		inDegree[name] = 0
	}
	for name, task := range d.tasks {
		for _, dep := range task.DependsOn {
			dependents[dep] = append(dependents[dep], name)
			inDegree[name]++
		}
	}

	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		for _, dependent := range dependents[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(sorted) != len(d.tasks) {
		return nil, fmt.Errorf("cycle detected: %d tasks in graph, only %d reachable",
			len(d.tasks), len(sorted))
	}

	return sorted, nil
}

func (d *DAG) ParallelLevels() [][]string {
	inDegree := make(map[string]int, len(d.tasks))
	dependents := make(map[string][]string, len(d.tasks))

	for name := range d.tasks {
		inDegree[name] = 0
	}
	for name, task := range d.tasks {
		for _, dep := range task.DependsOn {
			dependents[dep] = append(dependents[dep], name)
			inDegree[name]++
		}
	}

	var levels [][]string
	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	for len(queue) > 0 {
		levels = append(levels, queue)
		var nextQueue []string
		for _, node := range queue {
			for _, dependent := range dependents[node] {
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					nextQueue = append(nextQueue, dependent)
				}
			}
		}
		queue = nextQueue
	}

	return levels
}

func main() {
	dag := NewDAG()

	dag.AddTask(TaskDef{Name: "checkout", Duration: 100 * time.Millisecond})
	dag.AddTask(TaskDef{Name: "install-deps", Duration: 200 * time.Millisecond, DependsOn: []string{"checkout"}})
	dag.AddTask(TaskDef{Name: "lint", Duration: 150 * time.Millisecond, DependsOn: []string{"install-deps"}})
	dag.AddTask(TaskDef{Name: "unit-tests", Duration: 300 * time.Millisecond, DependsOn: []string{"install-deps"}})
	dag.AddTask(TaskDef{Name: "build", Duration: 250 * time.Millisecond, DependsOn: []string{"lint"}})
	dag.AddTask(TaskDef{Name: "integration-tests", Duration: 400 * time.Millisecond, DependsOn: []string{"build"}})
	dag.AddTask(TaskDef{Name: "security-scan", Duration: 200 * time.Millisecond, DependsOn: []string{"build"}})
	dag.AddTask(TaskDef{Name: "deploy", Duration: 150 * time.Millisecond, DependsOn: []string{"integration-tests", "security-scan", "unit-tests"}})

	if err := dag.Validate(); err != nil {
		fmt.Printf("DAG validation failed: %v\n", err)
		return
	}

	sorted, _ := dag.TopologicalSort()
	fmt.Println("=== Topological Order ===")
	for i, name := range sorted {
		task := dag.tasks[name]
		deps := "none"
		if len(task.DependsOn) > 0 {
			deps = fmt.Sprintf("%v", task.DependsOn)
		}
		fmt.Printf("  %d. %-20s deps=%s\n", i+1, name, deps)
	}

	levels := dag.ParallelLevels()
	fmt.Println("\n=== Parallel Execution Levels ===")
	totalSequential := time.Duration(0)
	totalCriticalPath := time.Duration(0)
	for i, level := range levels {
		var maxDuration time.Duration
		for _, name := range level {
			d := dag.tasks[name].Duration
			totalSequential += d
			if d > maxDuration {
				maxDuration = d
			}
		}
		totalCriticalPath += maxDuration
		fmt.Printf("  Level %d: %v (max duration: %v)\n", i+1, level, maxDuration)
	}

	fmt.Printf("\n=== Time Analysis ===\n")
	fmt.Printf("  Sequential time: %v\n", totalSequential)
	fmt.Printf("  Parallel minimum: %v\n", totalCriticalPath)
	fmt.Printf("  Speedup: %.1fx\n", float64(totalSequential)/float64(totalCriticalPath))
}
```

**What's happening here:** The DAG represents a CI/CD pipeline with 8 tasks. `TopologicalSort` uses Kahn's algorithm: start with tasks that have no dependencies (in-degree zero), process them, reduce the in-degree of their dependents, and repeat. If the sorted output has fewer tasks than the graph, a cycle exists. `ParallelLevels` groups tasks that can execute simultaneously -- all tasks at the same level have their dependencies satisfied by previous levels.

**Key insight:** The parallel levels reveal the minimum execution time. Even with infinite goroutines, you cannot be faster than the sum of the maximum durations at each level. This is the critical path. In the CI pipeline, checkout -> install-deps -> lint -> build -> integration-tests -> deploy is the critical path (1200ms). Optimizing `security-scan` (200ms) provides zero benefit because it runs in parallel with `integration-tests` (400ms).

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Topological Order ===
  1. checkout             deps=none
  2. install-deps         deps=[checkout]
  3. lint                 deps=[install-deps]
  4. unit-tests           deps=[install-deps]
  5. build                deps=[lint]
  6. integration-tests    deps=[build]
  7. security-scan        deps=[build]
  8. deploy               deps=[integration-tests security-scan unit-tests]

=== Parallel Execution Levels ===
  Level 1: [checkout] (max duration: 100ms)
  Level 2: [install-deps] (max duration: 200ms)
  Level 3: [lint unit-tests] (max duration: 300ms)
  Level 4: [build] (max duration: 250ms)
  Level 5: [integration-tests security-scan] (max duration: 400ms)
  Level 6: [deploy] (max duration: 150ms)

=== Time Analysis ===
  Sequential time: 1.75s
  Parallel minimum: 1.4s
  Speedup: 1.2x
```


## Step 2 -- Concurrent Execution with Error Propagation

Build the execution engine that launches goroutines based on dependency completion and propagates errors to skip downstream tasks.

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type TaskStatus int

const (
	TaskPending TaskStatus = iota
	TaskRunning
	TaskCompleted
	TaskFailed
	TaskSkipped
)

func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskRunning:
		return "running"
	case TaskCompleted:
		return "completed"
	case TaskFailed:
		return "FAILED"
	case TaskSkipped:
		return "SKIPPED"
	default:
		return "unknown"
	}
}

type TaskDef struct {
	Name      string
	Duration  time.Duration
	DependsOn []string
	Fn        func() error
}

type TaskResult struct {
	Name     string
	Status   TaskStatus
	Duration time.Duration
	Error    error
}

type Orchestrator struct {
	mu         sync.Mutex
	tasks      map[string]TaskDef
	status     map[string]TaskStatus
	results    map[string]TaskResult
	dependents map[string][]string
	pending    map[string]int
	readyCh    chan string
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		tasks:      make(map[string]TaskDef),
		status:     make(map[string]TaskStatus),
		results:    make(map[string]TaskResult),
		dependents: make(map[string][]string),
		pending:    make(map[string]int),
		readyCh:    make(chan string, 64),
	}
}

func (o *Orchestrator) AddTask(def TaskDef) {
	o.tasks[def.Name] = def
	o.status[def.Name] = TaskPending
	o.pending[def.Name] = len(def.DependsOn)

	for _, dep := range def.DependsOn {
		o.dependents[dep] = append(o.dependents[dep], def.Name)
	}
}

func (o *Orchestrator) shouldSkip(name string) bool {
	task := o.tasks[name]
	for _, dep := range task.DependsOn {
		s := o.status[dep]
		if s == TaskFailed || s == TaskSkipped {
			return true
		}
	}
	return false
}

func (o *Orchestrator) taskDone(name string, result TaskResult) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.status[name] = result.Status
	o.results[name] = result

	for _, dependent := range o.dependents[name] {
		o.pending[dependent]--
		if o.pending[dependent] == 0 {
			o.readyCh <- dependent
		}
	}
}

func (o *Orchestrator) Execute() map[string]TaskResult {
	o.mu.Lock()
	totalTasks := len(o.tasks)
	for name, count := range o.pending {
		if count == 0 {
			o.readyCh <- name
		}
	}
	o.mu.Unlock()

	var wg sync.WaitGroup
	completed := 0

	for completed < totalTasks {
		name := <-o.readyCh

		o.mu.Lock()
		skip := o.shouldSkip(name)
		if skip {
			o.status[name] = TaskSkipped
			o.results[name] = TaskResult{Name: name, Status: TaskSkipped}
			for _, dependent := range o.dependents[name] {
				o.pending[dependent]--
				if o.pending[dependent] == 0 {
					o.readyCh <- dependent
				}
			}
			o.mu.Unlock()
			completed++
			continue
		}
		o.status[name] = TaskRunning
		o.mu.Unlock()

		wg.Add(1)
		go func(taskName string) {
			defer wg.Done()
			task := o.tasks[taskName]

			start := time.Now()
			var err error
			if task.Fn != nil {
				err = task.Fn()
			} else {
				time.Sleep(task.Duration)
			}
			elapsed := time.Since(start)

			result := TaskResult{
				Name:     taskName,
				Duration: elapsed,
			}
			if err != nil {
				result.Status = TaskFailed
				result.Error = err
			} else {
				result.Status = TaskCompleted
			}

			o.taskDone(taskName, result)
		}(name)
		completed++
	}

	wg.Wait()
	return o.results
}

func main() {
	orch := NewOrchestrator()

	orch.AddTask(TaskDef{Name: "checkout", Duration: 100 * time.Millisecond})
	orch.AddTask(TaskDef{Name: "install-deps", Duration: 200 * time.Millisecond, DependsOn: []string{"checkout"}})
	orch.AddTask(TaskDef{Name: "lint", Duration: 150 * time.Millisecond, DependsOn: []string{"install-deps"}})
	orch.AddTask(TaskDef{Name: "unit-tests", Duration: 300 * time.Millisecond, DependsOn: []string{"install-deps"}})
	orch.AddTask(TaskDef{Name: "build", Duration: 250 * time.Millisecond, DependsOn: []string{"lint"}})
	orch.AddTask(TaskDef{Name: "integration-tests", Duration: 400 * time.Millisecond, DependsOn: []string{"build"},
		Fn: func() error {
			time.Sleep(100 * time.Millisecond)
			return errors.New("test assertion failed: expected 200 got 500")
		},
	})
	orch.AddTask(TaskDef{Name: "security-scan", Duration: 200 * time.Millisecond, DependsOn: []string{"build"}})
	orch.AddTask(TaskDef{Name: "deploy", Duration: 150 * time.Millisecond, DependsOn: []string{"integration-tests", "security-scan", "unit-tests"}})

	fmt.Println("=== DAG Orchestrator: CI Pipeline ===")
	start := time.Now()
	results := orch.Execute()
	elapsed := time.Since(start)

	order := []string{"checkout", "install-deps", "lint", "unit-tests", "build",
		"integration-tests", "security-scan", "deploy"}

	fmt.Printf("\n  %-25s %-12s %-12s %s\n", "Task", "Status", "Duration", "Error")
	fmt.Println("  " + "----------------------------------------------------------------------")
	for _, name := range order {
		r := results[name]
		errMsg := ""
		if r.Error != nil {
			errMsg = r.Error.Error()
		}
		fmt.Printf("  %-25s %-12s %-12v %s\n",
			r.Name, r.Status, r.Duration.Round(time.Millisecond), errMsg)
	}

	var completed, failed, skipped int
	for _, r := range results {
		switch r.Status {
		case TaskCompleted:
			completed++
		case TaskFailed:
			failed++
		case TaskSkipped:
			skipped++
		}
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("  Wall time: %v\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Completed: %d | Failed: %d | Skipped: %d\n", completed, failed, skipped)

	if failed > 0 {
		fmt.Println("\n  Pipeline FAILED: error propagation skipped downstream tasks")
	}
}
```

**What's happening here:** The orchestrator uses a `readyCh` channel to signal when a task's dependencies are satisfied. Initially, tasks with no dependencies are enqueued. When a task completes (or fails), `taskDone` decrements the pending count of its dependents. When a dependent's count reaches zero, it is enqueued to `readyCh`. The main loop reads from `readyCh`, checks if the task should be skipped (a dependency failed), and either skips it or launches a goroutine.

**Key insight:** Error propagation cascades through the DAG. When `integration-tests` fails, `deploy` is skipped because it depends on `integration-tests`. But `security-scan` still runs because it does not depend on `integration-tests` -- it depends on `build`, which succeeded. The orchestrator maximizes useful work: it does not abort the entire pipeline on first failure, only the affected downstream path.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== DAG Orchestrator: CI Pipeline ===

  Task                      Status       Duration     Error
  ----------------------------------------------------------------------
  checkout                  completed    100ms
  install-deps              completed    200ms
  lint                      completed    150ms
  unit-tests                completed    300ms
  build                     completed    250ms
  integration-tests         FAILED       100ms        test assertion failed: expected 200 got 500
  security-scan             completed    200ms
  deploy                    SKIPPED      0s

=== Summary ===
  Wall time: 903ms
  Completed: 6 | Failed: 1 | Skipped: 1

  Pipeline FAILED: error propagation skipped downstream tasks
```


## Step 3 -- Critical Path Analysis

Add critical path computation to identify which tasks determine the minimum execution time. Display the actual execution timeline alongside the theoretical critical path.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type TaskStatus int

const (
	TaskPending TaskStatus = iota
	TaskRunning
	TaskCompleted
	TaskFailed
	TaskSkipped
)

func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskRunning:
		return "running"
	case TaskCompleted:
		return "completed"
	case TaskFailed:
		return "FAILED"
	case TaskSkipped:
		return "SKIPPED"
	default:
		return "unknown"
	}
}

type TaskDef struct {
	Name      string
	Duration  time.Duration
	DependsOn []string
	Fn        func() error
}

type TaskResult struct {
	Name      string
	Status    TaskStatus
	Duration  time.Duration
	StartedAt time.Duration
	Error     error
}

type Orchestrator struct {
	mu         sync.Mutex
	tasks      map[string]TaskDef
	status     map[string]TaskStatus
	results    map[string]TaskResult
	dependents map[string][]string
	pending    map[string]int
	readyCh    chan string
	startTime  time.Time
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		tasks:      make(map[string]TaskDef),
		status:     make(map[string]TaskStatus),
		results:    make(map[string]TaskResult),
		dependents: make(map[string][]string),
		pending:    make(map[string]int),
		readyCh:    make(chan string, 64),
	}
}

func (o *Orchestrator) AddTask(def TaskDef) {
	o.tasks[def.Name] = def
	o.status[def.Name] = TaskPending
	o.pending[def.Name] = len(def.DependsOn)
	for _, dep := range def.DependsOn {
		o.dependents[dep] = append(o.dependents[dep], def.Name)
	}
}

func (o *Orchestrator) shouldSkip(name string) bool {
	task := o.tasks[name]
	for _, dep := range task.DependsOn {
		s := o.status[dep]
		if s == TaskFailed || s == TaskSkipped {
			return true
		}
	}
	return false
}

func (o *Orchestrator) taskDone(name string, result TaskResult) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.status[name] = result.Status
	o.results[name] = result
	for _, dependent := range o.dependents[name] {
		o.pending[dependent]--
		if o.pending[dependent] == 0 {
			o.readyCh <- dependent
		}
	}
}

func (o *Orchestrator) Execute() map[string]TaskResult {
	o.startTime = time.Now()

	o.mu.Lock()
	totalTasks := len(o.tasks)
	for name, count := range o.pending {
		if count == 0 {
			o.readyCh <- name
		}
	}
	o.mu.Unlock()

	var wg sync.WaitGroup
	completed := 0

	for completed < totalTasks {
		name := <-o.readyCh

		o.mu.Lock()
		skip := o.shouldSkip(name)
		if skip {
			o.status[name] = TaskSkipped
			o.results[name] = TaskResult{Name: name, Status: TaskSkipped, StartedAt: time.Since(o.startTime)}
			for _, dependent := range o.dependents[name] {
				o.pending[dependent]--
				if o.pending[dependent] == 0 {
					o.readyCh <- dependent
				}
			}
			o.mu.Unlock()
			completed++
			continue
		}
		o.status[name] = TaskRunning
		o.mu.Unlock()

		wg.Add(1)
		go func(taskName string) {
			defer wg.Done()
			task := o.tasks[taskName]
			startedAt := time.Since(o.startTime)
			start := time.Now()

			var err error
			if task.Fn != nil {
				err = task.Fn()
			} else {
				time.Sleep(task.Duration)
			}
			elapsed := time.Since(start)

			result := TaskResult{Name: taskName, Duration: elapsed, StartedAt: startedAt}
			if err != nil {
				result.Status = TaskFailed
				result.Error = err
			} else {
				result.Status = TaskCompleted
			}
			o.taskDone(taskName, result)
		}(name)
		completed++
	}

	wg.Wait()
	return o.results
}

func computeCriticalPath(tasks map[string]TaskDef) ([]string, time.Duration) {
	earliest := make(map[string]time.Duration, len(tasks))
	predecessor := make(map[string]string, len(tasks))

	inDegree := make(map[string]int)
	dependents := make(map[string][]string)
	for name := range tasks {
		inDegree[name] = 0
	}
	for name, task := range tasks {
		for _, dep := range task.DependsOn {
			dependents[dep] = append(dependents[dep], name)
			inDegree[name]++
		}
	}

	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
			earliest[name] = tasks[name].Duration
		}
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		endTime := earliest[node]

		for _, dependent := range dependents[node] {
			candidateStart := endTime + tasks[dependent].Duration
			if candidateStart > earliest[dependent] {
				earliest[dependent] = candidateStart
				predecessor[dependent] = node
			}
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	var criticalEnd string
	var maxTime time.Duration
	for name, t := range earliest {
		if t > maxTime {
			maxTime = t
			criticalEnd = name
		}
	}

	var path []string
	current := criticalEnd
	for current != "" {
		path = append([]string{current}, path...)
		current = predecessor[current]
	}

	return path, maxTime
}

func main() {
	orch := NewOrchestrator()

	orch.AddTask(TaskDef{Name: "checkout", Duration: 100 * time.Millisecond})
	orch.AddTask(TaskDef{Name: "install-deps", Duration: 200 * time.Millisecond, DependsOn: []string{"checkout"}})
	orch.AddTask(TaskDef{Name: "lint", Duration: 150 * time.Millisecond, DependsOn: []string{"install-deps"}})
	orch.AddTask(TaskDef{Name: "unit-tests", Duration: 300 * time.Millisecond, DependsOn: []string{"install-deps"}})
	orch.AddTask(TaskDef{Name: "build", Duration: 250 * time.Millisecond, DependsOn: []string{"lint"}})
	orch.AddTask(TaskDef{Name: "integration-tests", Duration: 400 * time.Millisecond, DependsOn: []string{"build"}})
	orch.AddTask(TaskDef{Name: "security-scan", Duration: 200 * time.Millisecond, DependsOn: []string{"build"}})
	orch.AddTask(TaskDef{Name: "deploy", Duration: 150 * time.Millisecond, DependsOn: []string{"integration-tests", "security-scan", "unit-tests"}})

	criticalPath, criticalTime := computeCriticalPath(orch.tasks)

	fmt.Println("=== DAG Orchestrator with Critical Path ===\n")
	fmt.Printf("Critical path: %v\n", criticalPath)
	fmt.Printf("Critical path time: %v\n\n", criticalTime)

	fmt.Println("--- Executing Pipeline ---")
	start := time.Now()
	results := orch.Execute()
	elapsed := time.Since(start)

	order := []string{"checkout", "install-deps", "lint", "unit-tests", "build",
		"integration-tests", "security-scan", "deploy"}

	fmt.Printf("\n  %-25s %-12s %-12s %-12s\n", "Task", "Status", "Started", "Duration")
	fmt.Println("  " + "--------------------------------------------------------------")
	for _, name := range order {
		r := results[name]
		fmt.Printf("  %-25s %-12s %-12v %-12v\n",
			r.Name, r.Status,
			r.StartedAt.Round(time.Millisecond),
			r.Duration.Round(time.Millisecond))
	}

	fmt.Printf("\n=== Execution Analysis ===\n")
	fmt.Printf("  Critical path time: %v (theoretical minimum)\n", criticalTime)
	fmt.Printf("  Actual wall time:   %v\n", elapsed.Round(time.Millisecond))
	overhead := float64(elapsed-criticalTime) / float64(criticalTime) * 100
	fmt.Printf("  Scheduling overhead: %.1f%%\n", overhead)

	var totalWork time.Duration
	for _, r := range results {
		totalWork += r.Duration
	}
	parallelism := float64(totalWork) / float64(elapsed)
	fmt.Printf("  Effective parallelism: %.1fx\n", parallelism)

	fmt.Printf("\n=== Critical Path Detail ===\n")
	pathTime := time.Duration(0)
	for _, name := range criticalPath {
		task := orch.tasks[name]
		pathTime += task.Duration
		fmt.Printf("  %-25s %v (cumulative: %v)\n", name, task.Duration, pathTime)
	}

	fmt.Println("\n  Tasks NOT on critical path can be optimized only up to the critical path time.")
	fmt.Println("  To reduce wall time, optimize tasks ON the critical path.")
}
```

**What's happening here:** `computeCriticalPath` performs a forward pass through the DAG, computing the earliest completion time for each task. The task with the latest completion time is the end of the critical path. Backtracking through predecessors reconstructs the full path. The execution engine tracks `StartedAt` timestamps to show when each task actually began, revealing how parallelism played out.

**Key insight:** The critical path determines the minimum possible execution time. In this pipeline, the critical path is `checkout -> install-deps -> lint -> build -> integration-tests -> deploy` (1250ms). `unit-tests` (300ms) and `security-scan` (200ms) run in parallel with other tasks and do not affect the critical path. Even if you made `security-scan` ten times faster, the wall time would not change. This is the most important insight for pipeline optimization: find the critical path first, then optimize only those tasks.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== DAG Orchestrator with Critical Path ===

Critical path: [checkout install-deps lint build integration-tests deploy]
Critical path time: 1.25s

--- Executing Pipeline ---

  Task                      Status       Started      Duration
  --------------------------------------------------------------
  checkout                  completed    0s           100ms
  install-deps              completed    100ms        200ms
  lint                      completed    300ms        150ms
  unit-tests                completed    300ms        300ms
  build                     completed    450ms        250ms
  integration-tests         completed    700ms        400ms
  security-scan             completed    700ms        200ms
  deploy                    completed    1.1s         150ms

=== Execution Analysis ===
  Critical path time: 1.25s (theoretical minimum)
  Actual wall time:   1.254s
  Scheduling overhead: 0.3%
  Effective parallelism: 1.4x

=== Critical Path Detail ===
  checkout                  100ms (cumulative: 100ms)
  install-deps              200ms (cumulative: 300ms)
  lint                      150ms (cumulative: 450ms)
  build                     250ms (cumulative: 700ms)
  integration-tests         400ms (cumulative: 1.1s)
  deploy                    150ms (cumulative: 1.25s)

  Tasks NOT on critical path can be optimized only up to the critical path time.
  To reduce wall time, optimize tasks ON the critical path.
```


## Common Mistakes

### Not Detecting Cycles in the DAG

```go
// Wrong: no cycle detection, leads to deadlock
func (o *Orchestrator) Execute() {
	for name := range o.tasks {
		go func(n string) {
			// wait for all dependencies
			for _, dep := range o.tasks[n].DependsOn {
				<-o.doneCh[dep] // if A depends on B and B depends on A, deadlock
			}
			o.run(n)
		}(name)
	}
}
```
**What happens:** If task A depends on B and B depends on A (a cycle), both goroutines wait forever for each other. The orchestrator hangs silently. In a CI system, this manifests as a pipeline that runs indefinitely and must be manually killed.

**Fix:** Validate the DAG with topological sort before execution. If the sort visits fewer nodes than exist in the graph, a cycle exists. Report the cycle and refuse to execute.


### Launching All Goroutines at Once Without Dependency Checks

```go
// Wrong: all tasks start immediately regardless of dependencies
func (o *Orchestrator) ExecuteBroken() {
	var wg sync.WaitGroup
	for name, task := range o.tasks {
		wg.Add(1)
		go func(n string, t TaskDef) {
			defer wg.Done()
			time.Sleep(t.Duration) // runs immediately, no dependency wait
		}(name, task)
	}
	wg.Wait()
}
```
**What happens:** All 8 tasks start at t=0. `deploy` runs before `integration-tests` finishes. `build` runs before `lint` finishes. The dependency ordering is completely ignored. Results are meaningless because tasks consumed input from incomplete predecessors.

**Fix:** Use the ready-channel pattern: only enqueue a task when all its dependencies have completed. The orchestrator tracks pending dependency counts and signals readiness through a channel.


### Not Propagating Errors to Dependent Tasks

```go
// Wrong: downstream tasks run even when upstream fails
func (o *Orchestrator) taskDone(name string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil {
		o.status[name] = TaskFailed
	} else {
		o.status[name] = TaskCompleted
	}
	// unlock dependents unconditionally
	for _, dep := range o.dependents[name] {
		o.pending[dep]--
		if o.pending[dep] == 0 {
			o.readyCh <- dep // deploy runs even though integration-tests failed
		}
	}
}
```
**What happens:** `deploy` runs after `integration-tests` fails, deploying broken code to production. The orchestrator treated failure as completion, satisfying the dependency without checking the outcome. This is the most dangerous bug in a pipeline orchestrator.

**Fix:** Before launching a task, check if any of its dependencies failed or were skipped. If so, mark the task as skipped and propagate the skip to its own dependents. The `shouldSkip` check in the execution loop prevents any task from running with a failed upstream.


## Verify What You Learned

Extend the orchestrator with **retry and conditional execution**:
1. Add a `MaxRetries int` field to `TaskDef`. If a task fails and has retries remaining, re-execute it instead of marking it failed
2. Add an `OnlyIf func() bool` field to `TaskDef`. The task only executes if `OnlyIf` returns true; otherwise it is skipped (but downstream tasks are not affected -- treat it as a successful no-op)
3. Add a `deploy-staging` task that always runs, and a `deploy-production` task with `OnlyIf: func() bool { return isMainBranch }` that only runs on the main branch
4. Print the full execution report showing retries attempted and conditional evaluations

**Hint:** Wrap the task execution in a retry loop. For `OnlyIf`, evaluate the condition before launching the goroutine. If the condition is false, mark the task as `TaskCompleted` (not skipped) so dependents still execute.


## What's Next
Continue to [01-Unbuffered Channel Basics](../../02-channels/01-unbuffered-channel-basics/01-unbuffered-channel-basics.md) in Section 02 -- Channels.


## Summary
- A DAG orchestrator resolves task dependencies, identifies parallelism, and launches goroutines in valid order
- Topological sort (Kahn's algorithm) validates the DAG and detects cycles that would cause deadlocks
- The ready-channel pattern ensures tasks only execute when all dependencies have completed
- Error propagation skips downstream tasks when upstream tasks fail, preventing cascading incorrect execution
- The critical path is the longest dependency chain and determines the minimum possible execution time
- Optimizing tasks NOT on the critical path does not reduce wall time -- always identify the critical path first
- This pattern is the foundation of Make, Bazel, GitHub Actions, Airflow, and every workflow engine


## Reference
- [Topological Sort (Kahn's Algorithm)](https://en.wikipedia.org/wiki/Topological_sorting#Kahn's_algorithm) -- DAG ordering
- [Critical Path Method](https://en.wikipedia.org/wiki/Critical_path_method) -- project scheduling
- [sync.WaitGroup](https://pkg.go.dev/sync#WaitGroup) -- waiting for goroutine completion
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines) -- channel-based orchestration patterns
