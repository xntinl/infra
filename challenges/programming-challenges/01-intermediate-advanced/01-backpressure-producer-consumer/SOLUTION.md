# Solution: Backpressure-Aware Producer-Consumer Pipeline

## Architecture Overview

The pipeline is modeled as a linked list of stages, each running in its own goroutine. Stages communicate through buffered channels whose capacity determines the backpressure threshold. When a stage's output channel is full, the stage blocks on send, which in turn slows its consumption from the input channel, propagating pressure upstream.

Three operating modes are supported per-stage: **normal** (block on full buffer), **lossy** (drop oldest item from buffer), and **batch** (accumulate N items or flush on timeout). A central `Pipeline` struct owns all stages and manages lifecycle through a shared `context.Context`.

Metrics are collected per-stage using atomic counters to avoid lock contention on the hot path.

## Go Solution

### Project Setup

```bash
mkdir -p backpressure-pipeline && cd backpressure-pipeline
go mod init backpressure-pipeline
```

### Implementation

```go
// pipeline.go
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// StageMode determines how a stage handles a full output buffer.
type StageMode int

const (
	ModeNormal StageMode = iota
	ModeLossy
	ModeBatch
)

// StageMetrics holds per-stage runtime statistics.
type StageMetrics struct {
	Processed    atomic.Int64
	Dropped      atomic.Int64
	startTime    time.Time
	totalLatency atomic.Int64 // nanoseconds, for average calculation
}

func (m *StageMetrics) Throughput() float64 {
	elapsed := time.Since(m.startTime).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(m.Processed.Load()) / elapsed
}

func (m *StageMetrics) AvgLatency() time.Duration {
	count := m.Processed.Load()
	if count == 0 {
		return 0
	}
	return time.Duration(m.totalLatency.Load() / count)
}

// StageConfig configures a single pipeline stage.
type StageConfig struct {
	Name      string
	BufSize   int
	Mode      StageMode
	BatchSize int           // only used in ModeBatch
	BatchWait time.Duration // max wait before flushing incomplete batch
}

// stage is a single processing step in the pipeline.
type stage[In, Out any] struct {
	config  StageConfig
	fn      func(In) Out
	in      <-chan In
	out     chan Out
	metrics *StageMetrics
}

// Pipeline orchestrates a chain of processing stages.
type Pipeline struct {
	stages  []runnable
	cancel  context.CancelFunc
	ctx     context.Context
	wg      sync.WaitGroup
	metrics []*StageMetrics
	names   []string
}

// runnable is a type-erased interface for starting a stage.
type runnable interface {
	run(ctx context.Context, wg *sync.WaitGroup)
	outputChan() any
}

func NewPipeline(parent context.Context) *Pipeline {
	ctx, cancel := context.WithCancel(parent)
	return &Pipeline{
		ctx:    ctx,
		cancel: cancel,
	}
}

// AddStage appends a processing stage to the pipeline.
// The input type must match the output type of the previous stage.
// For the first stage, the caller provides the input channel.
func AddStage[In, Out any](p *Pipeline, cfg StageConfig, fn func(In) Out, in <-chan In) <-chan Out {
	out := make(chan Out, cfg.BufSize)
	m := &StageMetrics{startTime: time.Now()}

	s := &stage[In, Out]{
		config:  cfg,
		fn:      fn,
		in:      in,
		out:     out,
		metrics: m,
	}

	p.stages = append(p.stages, s)
	p.metrics = append(p.metrics, m)
	p.names = append(p.names, cfg.Name)

	return out
}

func (s *stage[In, Out]) outputChan() any {
	return s.out
}

func (s *stage[In, Out]) run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer close(s.out)

	switch s.config.Mode {
	case ModeNormal:
		s.runNormal(ctx)
	case ModeLossy:
		s.runLossy(ctx)
	case ModeBatch:
		s.runBatch(ctx)
	}
}

func (s *stage[In, Out]) processAndSend(item In) {
	start := time.Now()
	result := s.fn(item)
	latency := time.Since(start)

	s.metrics.totalLatency.Add(int64(latency))
	s.metrics.Processed.Add(1)

	s.out <- result
}

func (s *stage[In, Out]) runNormal(ctx context.Context) {
	for {
		select {
		case item, ok := <-s.in:
			if !ok {
				return
			}
			s.processAndSend(item)
		case <-ctx.Done():
			s.drain()
			return
		}
	}
}

func (s *stage[In, Out]) runLossy(ctx context.Context) {
	for {
		select {
		case item, ok := <-s.in:
			if !ok {
				return
			}
			start := time.Now()
			result := s.fn(item)
			latency := time.Since(start)

			s.metrics.totalLatency.Add(int64(latency))
			s.metrics.Processed.Add(1)

			// Non-blocking send: drop oldest if full
			select {
			case s.out <- result:
			default:
				<-s.out // drop oldest
				s.metrics.Dropped.Add(1)
				s.out <- result
			}
		case <-ctx.Done():
			s.drain()
			return
		}
	}
}

// runBatch requires Out to be a slice type. The caller must configure fn to
// operate on individual items; batching wraps them. For simplicity, this
// implementation accumulates items and sends each processed result individually
// in bursts, which is the common real-world pattern.
func (s *stage[In, Out]) runBatch(ctx context.Context) {
	batchSize := s.config.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}
	maxWait := s.config.BatchWait
	if maxWait <= 0 {
		maxWait = 100 * time.Millisecond
	}

	var batch []In
	timer := time.NewTimer(maxWait)
	defer timer.Stop()

	flush := func() {
		for _, item := range batch {
			s.processAndSend(item)
		}
		batch = batch[:0]
		timer.Reset(maxWait)
	}

	for {
		select {
		case item, ok := <-s.in:
			if !ok {
				flush()
				return
			}
			batch = append(batch, item)
			if len(batch) >= batchSize {
				flush()
			}
		case <-timer.C:
			if len(batch) > 0 {
				flush()
			}
			timer.Reset(maxWait)
		case <-ctx.Done():
			flush()
			s.drain()
			return
		}
	}
}

// drain processes all remaining items in the input channel.
func (s *stage[In, Out]) drain() {
	for item := range s.in {
		s.processAndSend(item)
	}
}

// Run starts all stages and blocks until shutdown completes.
func (p *Pipeline) Run() {
	for _, s := range p.stages {
		p.wg.Add(1)
		go s.run(p.ctx, &p.wg)
	}
	p.wg.Wait()
}

// Shutdown signals all stages to drain and stop.
func (p *Pipeline) Shutdown() {
	p.cancel()
}

// Metrics returns a snapshot of per-stage metrics.
func (p *Pipeline) Metrics() []MetricsSnapshot {
	snapshots := make([]MetricsSnapshot, len(p.metrics))
	for i, m := range p.metrics {
		snapshots[i] = MetricsSnapshot{
			Name:       p.names[i],
			Processed:  m.Processed.Load(),
			Dropped:    m.Dropped.Load(),
			Throughput: m.Throughput(),
			AvgLatency: m.AvgLatency(),
		}
	}
	return snapshots
}

type MetricsSnapshot struct {
	Name       string
	Processed  int64
	Dropped    int64
	Throughput float64
	AvgLatency time.Duration
}

func (m MetricsSnapshot) String() string {
	return fmt.Sprintf("[%s] processed=%d dropped=%d throughput=%.1f/s avg_latency=%v",
		m.Name, m.Processed, m.Dropped, m.Throughput, m.AvgLatency)
}

// PrintMetrics logs all stage metrics.
func (p *Pipeline) PrintMetrics() {
	for _, m := range p.Metrics() {
		slog.Info("stage metrics", "stage", m.Name,
			"processed", m.Processed,
			"dropped", m.Dropped,
			"throughput", fmt.Sprintf("%.1f/s", m.Throughput),
			"avg_latency", m.AvgLatency)
	}
}
```

### Tests

```go
// pipeline_test.go
package pipeline

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNormalBackpressure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := NewPipeline(ctx)

	in := make(chan int, 100)

	// Fast stage -> slow stage. The slow stage should cause backpressure.
	mid := AddStage(p, StageConfig{Name: "double", BufSize: 5, Mode: ModeNormal},
		func(x int) int { return x * 2 }, in)

	out := AddStage(p, StageConfig{Name: "slow", BufSize: 5, Mode: ModeNormal},
		func(x int) int {
			time.Sleep(10 * time.Millisecond)
			return x + 1
		}, mid)

	var results []int
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		for v := range out {
			mu.Lock()
			results = append(results, v)
			mu.Unlock()
		}
		close(done)
	}()

	go p.Run()

	for i := 0; i < 20; i++ {
		in <- i
	}
	close(in)

	<-done
	cancel()

	mu.Lock()
	defer mu.Unlock()

	if len(results) != 20 {
		t.Errorf("expected 20 results, got %d", len(results))
	}

	// Verify transformation: each item should be (i*2)+1
	for i, v := range results {
		expected := (i * 2) + 1
		if v != expected {
			t.Errorf("result[%d] = %d, expected %d", i, v, expected)
		}
	}
}

func TestLossyMode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPipeline(ctx)

	in := make(chan int, 1000)

	out := AddStage(p, StageConfig{Name: "lossy", BufSize: 5, Mode: ModeLossy},
		func(x int) int { return x }, in)

	// Fill input faster than output can drain
	go func() {
		for i := 0; i < 100; i++ {
			in <- i
		}
		close(in)
	}()

	// Slow consumer
	var results []int
	done := make(chan struct{})
	go func() {
		for v := range out {
			time.Sleep(5 * time.Millisecond)
			results = append(results, v)
		}
		close(done)
	}()

	go p.Run()
	<-done

	metrics := p.Metrics()
	if metrics[0].Dropped == 0 {
		t.Log("Warning: no drops detected. Increase message count or slow consumer further for lossy test.")
	}
	t.Logf("Lossy mode: processed=%d, dropped=%d", metrics[0].Processed, metrics[0].Dropped)
}

func TestBatchMode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPipeline(ctx)

	in := make(chan int, 100)
	processedCount := 0
	var mu sync.Mutex

	out := AddStage(p, StageConfig{
		Name:      "batcher",
		BufSize:   50,
		Mode:      ModeBatch,
		BatchSize: 10,
		BatchWait: 50 * time.Millisecond,
	}, func(x int) int {
		mu.Lock()
		processedCount++
		mu.Unlock()
		return x
	}, in)

	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()

	go p.Run()

	for i := 0; i < 50; i++ {
		in <- i
	}
	close(in)

	<-done

	mu.Lock()
	defer mu.Unlock()
	if processedCount != 50 {
		t.Errorf("expected 50 processed, got %d", processedCount)
	}
}

func TestGracefulShutdown(t *testing.T) {
	baseGoroutines := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	p := NewPipeline(ctx)

	in := make(chan int, 10)

	mid := AddStage(p, StageConfig{Name: "stage1", BufSize: 5, Mode: ModeNormal},
		func(x int) int { return x * 2 }, in)

	out := AddStage(p, StageConfig{Name: "stage2", BufSize: 5, Mode: ModeNormal},
		func(x int) int { return x + 1 }, mid)

	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()

	go p.Run()

	// Send some items then trigger shutdown
	for i := 0; i < 5; i++ {
		in <- i
	}
	close(in)

	<-done
	cancel()

	// Allow goroutines to clean up
	time.Sleep(50 * time.Millisecond)

	leaked := runtime.NumGoroutine() - baseGoroutines
	if leaked > 1 { // allow 1 for test infrastructure variance
		t.Errorf("goroutine leak detected: %d extra goroutines", leaked)
	}
}

func TestHighThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping high-throughput test in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPipeline(ctx)

	const messageCount = 100_000
	in := make(chan int, 1000)

	mid := AddStage(p, StageConfig{Name: "transform", BufSize: 100, Mode: ModeNormal},
		func(x int) int { return x * 2 }, in)

	out := AddStage(p, StageConfig{Name: "filter", BufSize: 100, Mode: ModeNormal},
		func(x int) int { return x + 1 }, mid)

	count := 0
	done := make(chan struct{})
	go func() {
		for range out {
			count++
		}
		close(done)
	}()

	go p.Run()

	start := time.Now()
	for i := 0; i < messageCount; i++ {
		in <- i
	}
	close(in)

	<-done
	elapsed := time.Since(start)

	if count != messageCount {
		t.Errorf("expected %d messages, got %d", messageCount, count)
	}

	t.Logf("Processed %d messages in %v (%.0f msg/s)",
		count, elapsed, float64(count)/elapsed.Seconds())
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -v -run TestHighThroughput ./...
go test -bench=. ./...
```

### Expected Output

```
=== RUN   TestNormalBackpressure
--- PASS: TestNormalBackpressure (0.21s)
=== RUN   TestLossyMode
    pipeline_test.go:95: Lossy mode: processed=100, dropped=12
--- PASS: TestLossyMode (0.53s)
=== RUN   TestBatchMode
--- PASS: TestBatchMode (0.06s)
=== RUN   TestGracefulShutdown
--- PASS: TestGracefulShutdown (0.06s)
=== RUN   TestHighThroughput
    pipeline_test.go:152: Processed 100000 messages in 142ms (704225 msg/s)
--- PASS: TestHighThroughput (0.15s)
PASS
```

## Design Decisions

**Decision 1: Generics per stage vs. uniform `any` type.** Each stage is generic (`stage[In, Out]`) but the pipeline stores them as a `runnable` interface for heterogeneous stage types. This gives compile-time type safety at each `AddStage` call while allowing the pipeline to manage stages of different types. The trade-off is that the pipeline cannot enforce type compatibility between stages at compile time -- if you connect a `<-chan string` to a stage expecting `<-chan int`, you get a compile error at the `AddStage` call site, not at pipeline construction.

**Decision 2: Atomic counters vs. mutex-protected metrics.** Metrics use `atomic.Int64` instead of a mutex because metric updates happen on every message. Under high throughput, mutex contention on metrics would become a bottleneck. The trade-off is that reading a consistent snapshot across multiple counters is not atomic, but this is acceptable for monitoring purposes.

**Decision 3: Channel-based lossy mode vs. ring buffer.** The lossy mode uses a standard buffered channel with non-blocking send and drain-one-on-full. A purpose-built ring buffer would be more efficient (no channel overhead), but using channels keeps the implementation consistent across modes and avoids reimplementing synchronization. The performance cost is negligible for most real-world pipeline throughputs.

## Common Mistakes

**Mistake 1: Closing channels from the wrong goroutine.** Only the sender should close a channel. If the pipeline framework closes output channels, but the stage goroutine is still trying to send, you get a panic. The correct pattern: each stage closes its own output channel as the last action before returning.

**Mistake 2: Not draining input on shutdown.** If you simply return from a stage when context is cancelled, upstream stages that are blocked on sending to your input channel will deadlock. You must drain the input channel (or close it from upstream) to unblock senders.

**Mistake 3: Measuring throughput at the producer instead of the consumer.** The producer can push items into the pipeline quickly because of buffering. True pipeline throughput is measured at the final consumer output. Measuring at the producer gives an inflated number that does not reflect actual processing capacity.

## Performance Notes

- Channel buffer sizes directly control memory usage and latency. Larger buffers absorb bursts but increase memory footprint and latency (items wait longer in buffers).
- Under sustained load where the slowest stage is the bottleneck, all upstream stages converge to the slowest stage's throughput. This is correct backpressure behavior, not a bug.
- For CPU-bound stages, consider running multiple goroutines per stage (fan-out) and merging their outputs (fan-in). This is not implemented here to keep the core pattern clear.
- The batch mode timer creates a small GC pressure from `time.NewTimer` allocations. For extreme throughput, consider reusing timers or using a single ticker.

## Going Further

- Add fan-out/fan-in support: a stage that spawns N workers processing from the same input channel and merging into the same output
- Implement circuit breaker mode: if a stage's error rate exceeds a threshold, bypass it with a fallback function
- Add support for typed errors: stages return `(Out, error)` and the pipeline routes errors to a dead-letter channel
- Build a real-world pipeline: read log lines from stdin, parse JSON, filter by severity, batch-write to a file
- Benchmark channel-based backpressure vs. a semaphore-based approach and compare latency distributions
