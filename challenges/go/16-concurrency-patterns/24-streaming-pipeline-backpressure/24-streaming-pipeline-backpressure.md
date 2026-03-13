# 24. Streaming Pipeline with Backpressure

<!--
difficulty: insane
concepts: [backpressure, streaming-pipeline, bounded-channels, flow-control, producer-consumer, rate-adaptation]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [pipeline-pattern, fan-out-pattern, fan-in-pattern, bounded-parallelism, channels, select, context]
-->

## The Challenge

Build a multi-stage streaming pipeline that processes an unbounded data stream with proper backpressure. When a downstream stage is slow, the pipeline must propagate pressure upstream to slow the producer rather than buffering indefinitely in memory. The system must handle variable processing speeds across stages, provide per-stage throughput metrics, and support graceful shutdown that drains in-flight items.

The fundamental difficulty is that naive channel-based pipelines either block the entire pipeline when one stage is slow (unbuffered channels) or consume unbounded memory trying to keep up (large buffered channels). A real solution requires adaptive flow control: bounded buffers between stages, monitoring of buffer fill levels, and a producer that throttles when downstream pressure builds.

## Requirements

### Pipeline Architecture

1. At least four pipeline stages: `Source -> Transform -> Enrich -> Sink`
2. Each stage runs in its own goroutine (or goroutine pool for parallel stages)
3. Bounded buffered channels between every pair of stages
4. The source generates items faster than the sink can consume them

### Backpressure Mechanics

5. When a stage's output buffer is full, that stage blocks on send, which causes it to stop reading from its input, propagating pressure upstream
6. The source must detect backpressure and report how often it was throttled
7. Implement a "lossy" mode option where the source drops items instead of blocking (with a drop counter)
8. Implement a "batch" mode option where a slow stage accumulates items and processes them in batches to catch up

### Observability

9. Each stage reports: items processed, items in buffer, processing latency (p50, p99), and throughput (items/sec)
10. A monitor goroutine prints pipeline health every second
11. Detect and log when a stage becomes a bottleneck (buffer consistently > 80% full)

### Resilience

12. A panicking stage must not crash the pipeline -- recover and continue
13. Context cancellation triggers graceful shutdown: stop the source, drain all in-flight items, then exit
14. Individual stage errors are logged but do not stop the pipeline (skip the item)

## Hints

<details>
<summary>Hint 1: Stage abstraction</summary>

```go
type Stage[In, Out any] struct {
    Name    string
    Process func(In) (Out, error)
    In      <-chan In
    Out     chan Out
    Metrics *StageMetrics
}
```

Each stage reads from `In`, applies `Process`, and sends to `Out`. The bounded `Out` channel is the backpressure mechanism.
</details>

<details>
<summary>Hint 2: Backpressure detection at the source</summary>

```go
func (s *Source) Emit(item Item) {
    select {
    case s.out <- item:
        // sent immediately
    default:
        // channel full -- backpressure detected
        s.metrics.throttled.Add(1)
        if s.lossy {
            s.metrics.dropped.Add(1)
            return
        }
        s.out <- item // block until space is available
    }
}
```
</details>

<details>
<summary>Hint 3: Per-stage metrics with atomic counters</summary>

```go
type StageMetrics struct {
    processed atomic.Int64
    errors    atomic.Int64
    latencies []time.Duration // protected by mutex for percentile calculation
    mu        sync.Mutex
}

func (m *StageMetrics) RecordLatency(d time.Duration) {
    m.mu.Lock()
    m.latencies = append(m.latencies, d)
    m.mu.Unlock()
}
```
</details>

<details>
<summary>Hint 4: Monitor goroutine</summary>

```go
func monitor(ctx context.Context, stages []*StageMetrics, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            for _, s := range stages {
                bufferUsage := float64(len(s.outChan)) / float64(cap(s.outChan))
                if bufferUsage > 0.8 {
                    fmt.Printf("WARNING: %s buffer at %.0f%%\n", s.Name, bufferUsage*100)
                }
            }
        }
    }
}
```
</details>

## Success Criteria

- [ ] Four or more stages connected by bounded channels
- [ ] A fast producer is visibly throttled by a slow downstream stage (throttle count > 0)
- [ ] Lossy mode drops items under pressure instead of blocking (drop count reported)
- [ ] Per-stage metrics show items processed, buffer depth, and latency percentiles
- [ ] Monitor goroutine reports bottleneck warnings when a buffer exceeds 80% capacity
- [ ] Panicking stages recover without crashing the pipeline
- [ ] Context cancellation drains all in-flight items before exiting
- [ ] No data races (`go run -race`)
- [ ] The program compiles and demonstrates all features with visible output

## Research Resources

- [Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines) -- foundational pipeline patterns
- [Reactive Streams specification](https://www.reactive-streams.org/) -- backpressure semantics from the JVM world
- [Backpressure explained (blog)](https://medium.com/@jayphelps/backpressure-explained-the-flow-of-data-through-software-2350b3e77ce7)
- [Disruptor pattern](https://lmax-exchange.github.io/disruptor/) -- high-performance ring buffer for inter-stage communication
- [Go channel internals](https://go.dev/src/runtime/chan.go) -- how buffered channels implement blocking
