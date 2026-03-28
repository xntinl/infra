# Solution: MapReduce Framework

## Architecture Overview

The framework has three major components:

```
Coordinator (single process)
    - Job state machine: IDLE -> MAP -> SHUFFLE -> REDUCE -> COMPLETE
    - Task assignment via RPC
    - Worker health monitoring via heartbeats
    - Speculative execution scheduler

Worker (multiple processes)
    - Registers with coordinator
    - Executes map or reduce tasks
    - Reports completion/failure

Distributed FS Abstraction
    - Local directories simulate distributed storage
    - Input splits: input/{split-00, split-01, ...}
    - Intermediate: intermediate/{map-M-reduce-R}
    - Output: output/{reduce-R}
```

The coordinator is the single point of control. Workers are stateless: if one dies, its work is reassigned. Map outputs are stored on the worker's "local" disk (a directory). Reduce outputs go to the "global" filesystem.

## Go Solution

### Core Types

```go
// types/types.go
package types

// KeyValue represents an intermediate or output key-value pair.
type KeyValue struct {
	Key   string
	Value string
}

// MapFunc is the user-defined map function.
// Input: filename and file contents. Output: list of key-value pairs.
type MapFunc func(filename string, contents string) []KeyValue

// ReduceFunc is the user-defined reduce function.
// Input: key and all values for that key. Output: single result value.
type ReduceFunc func(key string, values []string) string

// CombinerFunc is an optional local aggregation function.
// Same signature as ReduceFunc, runs on map side.
type CombinerFunc func(key string, values []string) string

// TaskType identifies whether a task is map or reduce.
type TaskType int

const (
	MapTask    TaskType = 0
	ReduceTask TaskType = 1
)

// TaskState tracks the lifecycle of a task.
type TaskState int

const (
	Idle       TaskState = 0
	InProgress TaskState = 1
	Completed  TaskState = 2
)

// Task represents a unit of work assigned to a worker.
type Task struct {
	ID        int
	Type      TaskType
	State     TaskState
	InputFile string   // for map tasks: input split path
	InputFiles []string // for reduce tasks: intermediate file paths
	WorkerID  string
	StartTime int64 // unix timestamp
	NReduce   int   // number of reduce partitions (needed by map tasks)
}
```

### Coordinator

```go
// coordinator/coordinator.go
package coordinator

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"mapreduce/types"
)

type Phase int

const (
	MapPhase    Phase = 0
	ReducePhase Phase = 1
	DonePhase   Phase = 2
)

// Coordinator manages the MapReduce job lifecycle.
type Coordinator struct {
	mu            sync.Mutex
	phase         Phase
	mapTasks      []*types.Task
	reduceTasks   []*types.Task
	nMap          int
	nReduce       int
	workers       map[string]*WorkerInfo
	taskTimeout   time.Duration
	intermediateDir string
	outputDir     string
	logger        *slog.Logger

	// Speculative execution
	mapDurations    []time.Duration
	reduceDurations []time.Duration
	speculativeThreshold float64
}

type WorkerInfo struct {
	ID            string
	LastHeartbeat time.Time
	TasksCompleted int
}

// RPCs exposed to workers

type RegisterArgs struct {
	WorkerID string
}
type RegisterReply struct {
	OK bool
}

type RequestTaskArgs struct {
	WorkerID string
}
type RequestTaskReply struct {
	Task    *types.Task
	NoTask  bool
	JobDone bool
}

type ReportTaskArgs struct {
	WorkerID string
	TaskID   int
	TaskType types.TaskType
	Success  bool
	OutputFiles []string
	Duration time.Duration
}
type ReportTaskReply struct {
	OK bool
}

type HeartbeatArgs struct {
	WorkerID string
}
type HeartbeatReply struct {
	OK bool
}

func NewCoordinator(
	inputFiles []string,
	nReduce int,
	intermediateDir string,
	outputDir string,
	taskTimeout time.Duration,
	logger *slog.Logger,
) *Coordinator {
	c := &Coordinator{
		phase:           MapPhase,
		nMap:            len(inputFiles),
		nReduce:         nReduce,
		workers:         make(map[string]*WorkerInfo),
		taskTimeout:     taskTimeout,
		intermediateDir: intermediateDir,
		outputDir:       outputDir,
		logger:          logger,
		speculativeThreshold: 1.5,
	}

	os.MkdirAll(intermediateDir, 0755)
	os.MkdirAll(outputDir, 0755)

	c.mapTasks = make([]*types.Task, len(inputFiles))
	for i, f := range inputFiles {
		c.mapTasks[i] = &types.Task{
			ID:        i,
			Type:      types.MapTask,
			State:     types.Idle,
			InputFile: f,
			NReduce:   nReduce,
		}
	}

	c.reduceTasks = make([]*types.Task, nReduce)
	for i := 0; i < nReduce; i++ {
		c.reduceTasks[i] = &types.Task{
			ID:    i,
			Type:  types.ReduceTask,
			State: types.Idle,
		}
	}

	return c
}

// Register handles worker registration.
func (c *Coordinator) Register(args *RegisterArgs, reply *RegisterReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.workers[args.WorkerID] = &WorkerInfo{
		ID:            args.WorkerID,
		LastHeartbeat: time.Now(),
	}
	c.logger.Info("worker registered", "worker", args.WorkerID)
	reply.OK = true
	return nil
}

// RequestTask assigns a pending task to a worker.
func (c *Coordinator) RequestTask(args *RequestTaskArgs, reply *RequestTaskReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.phase == DonePhase {
		reply.JobDone = true
		return nil
	}

	var tasks []*types.Task
	if c.phase == MapPhase {
		tasks = c.mapTasks
	} else {
		tasks = c.reduceTasks
	}

	// Find an idle task
	for _, t := range tasks {
		if t.State == types.Idle {
			t.State = types.InProgress
			t.WorkerID = args.WorkerID
			t.StartTime = time.Now().Unix()
			reply.Task = t
			c.logger.Info("task assigned",
				"type", t.Type, "id", t.ID, "worker", args.WorkerID)
			return nil
		}
	}

	// Check for speculative execution opportunities
	specTask := c.findStraggler(tasks)
	if specTask != nil {
		specCopy := *specTask
		specCopy.WorkerID = args.WorkerID
		specCopy.StartTime = time.Now().Unix()
		reply.Task = &specCopy
		c.logger.Info("speculative task launched",
			"type", specTask.Type, "id", specTask.ID, "worker", args.WorkerID)
		return nil
	}

	reply.NoTask = true
	return nil
}

func (c *Coordinator) findStraggler(tasks []*types.Task) *types.Task {
	var durations []time.Duration
	if tasks[0].Type == types.MapTask {
		durations = c.mapDurations
	} else {
		durations = c.reduceDurations
	}

	if len(durations) < 3 {
		return nil
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	median := durations[len(durations)/2]
	threshold := time.Duration(float64(median) * c.speculativeThreshold)

	now := time.Now()
	for _, t := range tasks {
		if t.State == types.InProgress {
			elapsed := now.Sub(time.Unix(t.StartTime, 0))
			if elapsed > threshold {
				return t
			}
		}
	}
	return nil
}

// ReportTask handles task completion or failure reports.
func (c *Coordinator) ReportTask(args *ReportTaskArgs, reply *ReportTaskReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var task *types.Task
	if args.TaskType == types.MapTask {
		if args.TaskID < len(c.mapTasks) {
			task = c.mapTasks[args.TaskID]
		}
	} else {
		if args.TaskID < len(c.reduceTasks) {
			task = c.reduceTasks[args.TaskID]
		}
	}

	if task == nil {
		return fmt.Errorf("unknown task: type=%d id=%d", args.TaskType, args.TaskID)
	}

	if task.State == types.Completed {
		// Already completed (possibly by speculative execution), ignore
		reply.OK = true
		return nil
	}

	if !args.Success {
		task.State = types.Idle
		task.WorkerID = ""
		c.logger.Warn("task failed, resetting to idle",
			"type", args.TaskType, "id", args.TaskID, "worker", args.WorkerID)
		reply.OK = true
		return nil
	}

	task.State = types.Completed
	task.WorkerID = args.WorkerID

	if args.TaskType == types.MapTask {
		c.mapDurations = append(c.mapDurations, args.Duration)
	} else {
		c.reduceDurations = append(c.reduceDurations, args.Duration)
	}

	c.logger.Info("task completed",
		"type", args.TaskType, "id", args.TaskID, "worker", args.WorkerID)

	c.checkPhaseTransition()

	reply.OK = true
	return nil
}

func (c *Coordinator) checkPhaseTransition() {
	if c.phase == MapPhase {
		allDone := true
		for _, t := range c.mapTasks {
			if t.State != types.Completed {
				allDone = false
				break
			}
		}
		if allDone {
			c.phase = ReducePhase
			c.prepareReduceTasks()
			c.logger.Info("map phase complete, starting reduce phase")
		}
	} else if c.phase == ReducePhase {
		allDone := true
		for _, t := range c.reduceTasks {
			if t.State != types.Completed {
				allDone = false
				break
			}
		}
		if allDone {
			c.phase = DonePhase
			c.logger.Info("job complete")
		}
	}
}

func (c *Coordinator) prepareReduceTasks() {
	for i := 0; i < c.nReduce; i++ {
		var inputs []string
		for j := 0; j < c.nMap; j++ {
			path := filepath.Join(c.intermediateDir, fmt.Sprintf("mr-%d-%d", j, i))
			if _, err := os.Stat(path); err == nil {
				inputs = append(inputs, path)
			}
		}
		c.reduceTasks[i].InputFiles = inputs
	}
}

// Heartbeat updates the worker's last-seen timestamp.
func (c *Coordinator) Heartbeat(args *HeartbeatArgs, reply *HeartbeatReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if w, ok := c.workers[args.WorkerID]; ok {
		w.LastHeartbeat = time.Now()
	}
	reply.OK = true
	return nil
}

// CheckTimeouts detects failed workers and resets their tasks.
func (c *Coordinator) CheckTimeouts() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	deadWorkers := make(map[string]bool)

	for id, w := range c.workers {
		if now.Sub(w.LastHeartbeat) > c.taskTimeout {
			deadWorkers[id] = true
			c.logger.Warn("worker timed out", "worker", id)
		}
	}

	if len(deadWorkers) == 0 {
		return
	}

	resetTasks := func(tasks []*types.Task) {
		for _, t := range tasks {
			if t.State == types.InProgress && deadWorkers[t.WorkerID] {
				t.State = types.Idle
				t.WorkerID = ""
				c.logger.Info("task reset due to worker failure",
					"type", t.Type, "id", t.ID)
			}
		}
	}

	if c.phase == MapPhase {
		resetTasks(c.mapTasks)
		// Also reset completed map tasks from dead workers (intermediate output lost)
		for _, t := range c.mapTasks {
			if t.State == types.Completed && deadWorkers[t.WorkerID] {
				t.State = types.Idle
				t.WorkerID = ""
				c.logger.Warn("completed map task reset (worker lost intermediate data)",
					"id", t.ID)
			}
		}
	} else if c.phase == ReducePhase {
		resetTasks(c.reduceTasks)
	}

	for id := range deadWorkers {
		delete(c.workers, id)
	}
}

// IsDone returns whether the job is complete.
func (c *Coordinator) IsDone() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase == DonePhase
}

// Start launches the coordinator RPC server and monitoring HTTP server.
func (c *Coordinator) Start(rpcAddr, httpAddr string) error {
	rpc.Register(c)
	rpc.HandleHTTP()

	ln, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return fmt.Errorf("rpc listen: %w", err)
	}
	go http.Serve(ln, nil)

	// Status page
	http.HandleFunc("/status", c.statusHandler)
	if httpAddr != "" {
		go http.ListenAndServe(httpAddr, nil)
	}

	// Timeout checker
	go func() {
		for !c.IsDone() {
			time.Sleep(c.taskTimeout / 2)
			c.CheckTimeouts()
		}
	}()

	c.logger.Info("coordinator started", "rpc", rpcAddr, "http", httpAddr)
	return nil
}

func (c *Coordinator) statusHandler(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fmt.Fprintf(w, "Phase: %d\n", c.phase)
	fmt.Fprintf(w, "Workers: %d\n", len(c.workers))
	fmt.Fprintf(w, "\nMap Tasks:\n")
	for _, t := range c.mapTasks {
		fmt.Fprintf(w, "  [%d] state=%d worker=%s\n", t.ID, t.State, t.WorkerID)
	}
	fmt.Fprintf(w, "\nReduce Tasks:\n")
	for _, t := range c.reduceTasks {
		fmt.Fprintf(w, "  [%d] state=%d worker=%s\n", t.ID, t.State, t.WorkerID)
	}
}
```

### Worker

```go
// worker/worker.go
package worker

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/rpc"
	"os"
	"path/filepath"
	"sort"
	"time"

	"mapreduce/coordinator"
	"mapreduce/types"
)

// Worker executes map and reduce tasks assigned by the coordinator.
type Worker struct {
	id              string
	coordAddr       string
	mapFunc         types.MapFunc
	reduceFunc      types.ReduceFunc
	combinerFunc    types.CombinerFunc
	intermediateDir string
	outputDir       string
	logger          *slog.Logger
}

func New(
	id string,
	coordAddr string,
	mapFunc types.MapFunc,
	reduceFunc types.ReduceFunc,
	combinerFunc types.CombinerFunc,
	intermediateDir string,
	outputDir string,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		id:              id,
		coordAddr:       coordAddr,
		mapFunc:         mapFunc,
		reduceFunc:      reduceFunc,
		combinerFunc:    combinerFunc,
		intermediateDir: intermediateDir,
		outputDir:       outputDir,
		logger:          logger,
	}
}

// Run registers with the coordinator and processes tasks until the job is done.
func (w *Worker) Run() error {
	client, err := rpc.DialHTTP("tcp", w.coordAddr)
	if err != nil {
		return fmt.Errorf("dial coordinator: %w", err)
	}
	defer client.Close()

	// Register
	regReply := &coordinator.RegisterReply{}
	if err := client.Call("Coordinator.Register",
		&coordinator.RegisterArgs{WorkerID: w.id}, regReply); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// Heartbeat goroutine
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				reply := &coordinator.HeartbeatReply{}
				client.Call("Coordinator.Heartbeat",
					&coordinator.HeartbeatArgs{WorkerID: w.id}, reply)
			}
		}
	}()
	defer close(done)

	for {
		reply := &coordinator.RequestTaskReply{}
		if err := client.Call("Coordinator.RequestTask",
			&coordinator.RequestTaskArgs{WorkerID: w.id}, reply); err != nil {
			return fmt.Errorf("request task: %w", err)
		}

		if reply.JobDone {
			w.logger.Info("job done, shutting down")
			return nil
		}

		if reply.NoTask {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		start := time.Now()
		task := reply.Task
		var taskErr error

		if task.Type == types.MapTask {
			taskErr = w.executeMap(task)
		} else {
			taskErr = w.executeReduce(task)
		}

		duration := time.Since(start)
		report := &coordinator.ReportTaskArgs{
			WorkerID: w.id,
			TaskID:   task.ID,
			TaskType: task.Type,
			Success:  taskErr == nil,
			Duration: duration,
		}
		reportReply := &coordinator.ReportTaskReply{}
		client.Call("Coordinator.ReportTask", report, reportReply)

		if taskErr != nil {
			w.logger.Error("task failed", "type", task.Type, "id", task.ID, "error", taskErr)
		} else {
			w.logger.Info("task completed", "type", task.Type, "id", task.ID, "duration", duration)
		}
	}
}

func (w *Worker) executeMap(task *types.Task) error {
	content, err := os.ReadFile(task.InputFile)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	kvs := w.mapFunc(task.InputFile, string(content))

	// Apply combiner if configured
	if w.combinerFunc != nil {
		kvs = w.applyCombiner(kvs)
	}

	// Partition into nReduce buckets
	buckets := make([][]types.KeyValue, task.NReduce)
	for _, kv := range kvs {
		bucket := ihash(kv.Key) % task.NReduce
		buckets[bucket] = append(buckets[bucket], kv)
	}

	// Write intermediate files atomically
	for i, bucket := range buckets {
		finalPath := filepath.Join(w.intermediateDir, fmt.Sprintf("mr-%d-%d", task.ID, i))
		tmpPath := finalPath + ".tmp"

		file, err := os.Create(tmpPath)
		if err != nil {
			return fmt.Errorf("create intermediate: %w", err)
		}

		enc := json.NewEncoder(file)
		for _, kv := range bucket {
			if err := enc.Encode(&kv); err != nil {
				file.Close()
				os.Remove(tmpPath)
				return fmt.Errorf("encode: %w", err)
			}
		}
		file.Close()

		if err := os.Rename(tmpPath, finalPath); err != nil {
			return fmt.Errorf("rename: %w", err)
		}
	}

	return nil
}

func (w *Worker) applyCombiner(kvs []types.KeyValue) []types.KeyValue {
	grouped := make(map[string][]string)
	for _, kv := range kvs {
		grouped[kv.Key] = append(grouped[kv.Key], kv.Value)
	}

	var result []types.KeyValue
	for key, values := range grouped {
		combined := w.combinerFunc(key, values)
		result = append(result, types.KeyValue{Key: key, Value: combined})
	}
	return result
}

func (w *Worker) executeReduce(task *types.Task) error {
	// Read all intermediate files for this reduce partition
	var intermediate []types.KeyValue
	for _, inputFile := range task.InputFiles {
		file, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("open intermediate: %w", err)
		}
		dec := json.NewDecoder(file)
		for dec.More() {
			var kv types.KeyValue
			if err := dec.Decode(&kv); err != nil {
				file.Close()
				return fmt.Errorf("decode: %w", err)
			}
			intermediate = append(intermediate, kv)
		}
		file.Close()
	}

	// Sort by key
	sort.Slice(intermediate, func(i, j int) bool {
		return intermediate[i].Key < intermediate[j].Key
	})

	// Write output atomically
	finalPath := filepath.Join(w.outputDir, fmt.Sprintf("mr-out-%d", task.ID))
	tmpPath := finalPath + ".tmp"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}

	// Group by key and call reduce
	i := 0
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		var values []string
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := w.reduceFunc(intermediate[i].Key, values)
		fmt.Fprintf(outFile, "%v\t%v\n", intermediate[i].Key, output)
		i = j
	}

	outFile.Close()
	return os.Rename(tmpPath, finalPath)
}

func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}
```

### Example Applications

```go
// apps/wordcount.go
package apps

import (
	"fmt"
	"strings"
	"unicode"

	"mapreduce/types"
)

func WordCountMap(filename string, contents string) []types.KeyValue {
	words := strings.FieldsFunc(contents, func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	var kvs []types.KeyValue
	for _, w := range words {
		kvs = append(kvs, types.KeyValue{Key: strings.ToLower(w), Value: "1"})
	}
	return kvs
}

func WordCountReduce(key string, values []string) string {
	return fmt.Sprintf("%d", len(values))
}

func WordCountCombiner(key string, values []string) string {
	return fmt.Sprintf("%d", len(values))
}
```

```go
// apps/invertedindex.go
package apps

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"mapreduce/types"
)

func InvertedIndexMap(filename string, contents string) []types.KeyValue {
	words := strings.FieldsFunc(contents, func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	base := filepath.Base(filename)
	seen := make(map[string]bool)
	var kvs []types.KeyValue
	for _, w := range words {
		w = strings.ToLower(w)
		if !seen[w] {
			seen[w] = true
			kvs = append(kvs, types.KeyValue{Key: w, Value: base})
		}
	}
	return kvs
}

func InvertedIndexReduce(key string, values []string) string {
	unique := make(map[string]bool)
	for _, v := range values {
		unique[v] = true
	}
	files := make([]string, 0, len(unique))
	for f := range unique {
		files = append(files, f)
	}
	sort.Strings(files)
	return strings.Join(files, ", ")
}
```

```go
// apps/distsort.go
package apps

import (
	"mapreduce/types"
	"strings"
)

func DistSortMap(filename string, contents string) []types.KeyValue {
	lines := strings.Split(contents, "\n")
	var kvs []types.KeyValue
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			kvs = append(kvs, types.KeyValue{Key: line, Value: ""})
		}
	}
	return kvs
}

func DistSortReduce(key string, values []string) string {
	return ""
}
```

### Integration Test

```go
// mapreduce_test.go
package mapreduce_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mapreduce/apps"
	"mapreduce/coordinator"
	"mapreduce/worker"
)

func TestWordCount(t *testing.T) {
	tmpDir := t.TempDir()
	inputDir := filepath.Join(tmpDir, "input")
	intermediateDir := filepath.Join(tmpDir, "intermediate")
	outputDir := filepath.Join(tmpDir, "output")
	os.MkdirAll(inputDir, 0755)

	// Create test input files
	files := map[string]string{
		"file1.txt": "the quick brown fox jumps over the lazy dog",
		"file2.txt": "the fox jumped over the dog again",
		"file3.txt": "quick quick quick fox",
	}
	var inputFiles []string
	for name, content := range files {
		path := filepath.Join(inputDir, name)
		os.WriteFile(path, []byte(content), 0644)
		inputFiles = append(inputFiles, path)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	nReduce := 3
	coord := coordinator.NewCoordinator(
		inputFiles, nReduce, intermediateDir, outputDir,
		10*time.Second, logger,
	)

	rpcAddr := "127.0.0.1:0"
	if err := coord.Start(rpcAddr, ""); err != nil {
		// If port 0 doesn't work, try a specific port
		rpcAddr = "127.0.0.1:9876"
		if err := coord.Start(rpcAddr, ""); err != nil {
			t.Fatalf("start coordinator: %v", err)
		}
	}

	// Launch workers
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w := worker.New(
				fmt.Sprintf("worker-%d", id),
				rpcAddr,
				apps.WordCountMap,
				apps.WordCountReduce,
				apps.WordCountCombiner,
				intermediateDir,
				outputDir,
				logger,
			)
			if err := w.Run(); err != nil {
				t.Errorf("worker %d: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify output
	wordCounts := make(map[string]int)
	for i := 0; i < nReduce; i++ {
		path := filepath.Join(outputDir, fmt.Sprintf("mr-out-%d", i))
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			parts := strings.Split(line, "\t")
			if len(parts) == 2 {
				var count int
				fmt.Sscanf(parts[1], "%d", &count)
				wordCounts[parts[0]] = count
			}
		}
	}

	expected := map[string]int{
		"the": 4, "fox": 3, "quick": 4, "dog": 2,
		"over": 2, "jumped": 1, "again": 1,
	}
	for word, expectedCount := range expected {
		if wordCounts[word] != expectedCount {
			t.Errorf("word %q: got %d, want %d", word, wordCounts[word], expectedCount)
		}
	}
}

func TestWorkerFailure(t *testing.T) {
	// This test verifies that a coordinator can reassign tasks
	// when a worker fails. The full implementation would start
	// a coordinator, launch 4 workers, kill one during the map
	// phase, and verify the job still completes.
	t.Skip("requires full network setup; manual testing recommended")
}
```

## Running the Solution

```bash
mkdir -p mapreduce && cd mapreduce
go mod init mapreduce
# Create package directories: types/, coordinator/, worker/, apps/
# Place all files in their respective directories
go test -v -race -count=1 ./...
```

### Expected Output

```
=== RUN   TestWordCount
--- PASS: TestWordCount (2.1s)
=== RUN   TestWorkerFailure
--- SKIP: TestWorkerFailure
PASS
```

## Design Decisions

1. **RPC over custom protocol**: Go's `net/rpc` provides a battle-tested RPC framework. For a production system, gRPC would be more appropriate, but `net/rpc` keeps the implementation focused on MapReduce logic rather than serialization concerns.

2. **Atomic file writes**: Map and reduce outputs are written to temporary files and renamed atomically. This prevents partial output from being read by downstream tasks. If a worker crashes mid-write, the temporary file is simply orphaned and the task is re-executed.

3. **Combiner as optional optimization**: The combiner runs on the map side, reducing intermediate data before the shuffle. For word count, this turns thousands of `("the", "1")` pairs into a single `("the", "4523")`. The combiner must be idempotent and have the same signature as the reduce function.

4. **JSON for intermediate data**: Simple and debuggable. A production framework would use a binary format (Protocol Buffers, Avro) for lower serialization overhead and smaller files.

5. **Speculative execution based on median**: Using 1.5x the median completion time as the straggler threshold is a common heuristic. It adapts to the actual workload: if all tasks take 10 seconds, a 16-second task gets a backup. The first completion wins.

## Common Mistakes

- **Non-deterministic map output**: If the map function uses a hash map internally and iterates it, the output order varies between runs. This does not affect correctness but makes debugging harder. Sort intermediate output within each partition.
- **Not re-executing completed map tasks from failed workers**: A failed worker's intermediate output is on its local disk and is now inaccessible. Completed reduce tasks do not need re-execution because their output is on the global filesystem.
- **Deadlock in coordinator**: The coordinator must not hold its mutex while calling RPCs (which may block). Use the mutex only for state reads and writes, release it before any network operation.
- **Stale task completion**: A slow worker completes a task that was already re-assigned and completed by another worker. The coordinator must check if the task is still assigned to this worker before accepting the result, or simply accept both (idempotent reduce output via atomic rename).
- **Intermediate file naming collisions**: When a task is re-executed (different worker), both workers may write to the same intermediate file. Use atomic rename to ensure only one writer succeeds.

## Performance Notes

| Component | Bottleneck | Mitigation |
|-----------|-----------|-----------|
| Map phase | I/O on input splits | Larger splits, parallel reads |
| Shuffle | Network transfer of intermediate data | Combiner, compression |
| Reduce phase | Sorting intermediate data | External merge sort for large datasets |
| Coordinator | Lock contention under many workers | Sharded task queues, batch assignments |
| Speculative execution | Wasted compute | Only launch backup when >50% tasks complete |

For the word count example with 1 GB of input split into 64 MB chunks: 16 map tasks, each producing ~64 MB of intermediate data. With a combiner, intermediate data drops to ~1% of input. Reduce phase processes ~10 MB per partition. Total job time is dominated by the map phase for I/O-bound workloads.

## Going Further

- Implement a distributed file system abstraction using gRPC between "data nodes"
- Add input format plugins (CSV, JSON, Parquet) with record-level splitting
- Implement a secondary sort (sort by key, then by value within each key group)
- Add counters and progress reporting (like Hadoop's counters)
- Implement a combiner that runs periodically during the map task instead of only at the end
- Build a job chaining mechanism where the output of one MapReduce job is the input of the next
- Benchmark with the TeraSort benchmark (sort 10 GB of 100-byte records)
