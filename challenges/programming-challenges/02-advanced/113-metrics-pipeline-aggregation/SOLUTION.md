# Solution: Metrics Pipeline Aggregation

## Architecture Overview

Both implementations share the same three-stage pipeline:

```
Collection Stage          Aggregation Stage          Export Stage
  |                           |                           |
  +-- StatsD UDP listener     +-- Time-windowed           +-- Prometheus /metrics
  |                           |   double-buffer           |
  +-- Programmatic API        +-- Window rotation         +-- StatsD UDP push
      (Counter/Gauge/Hist)        (atomic swap)
```

**Collection** receives raw metric events from the StatsD listener or programmatic API and sends them to the aggregation stage via an internal channel. **Aggregation** maintains two window slots (current and previous) and rotates them at configurable intervals. **Export** reads from the previous window: Prometheus serves it on HTTP scrape, StatsD push sends it on each rotation.

## Go Solution

### Project Setup

```bash
mkdir -p metricspipe && cd metricspipe
go mod init metricspipe
```

### Metric Types

```go
// metrics.go
package metricspipe

import (
	"math"
	"sync"
	"sync/atomic"
)

type Labels map[string]string

func labelsKey(name string, labels Labels) string {
	key := name
	for k, v := range labels {
		key += "|" + k + "=" + v
	}
	return key
}

// --- Atomic Float64 ---

type atomicFloat64 struct {
	bits atomic.Uint64
}

func (a *atomicFloat64) Store(val float64) {
	a.bits.Store(math.Float64bits(val))
}

func (a *atomicFloat64) Load() float64 {
	return math.Float64frombits(a.bits.Load())
}

func (a *atomicFloat64) Add(delta float64) {
	for {
		old := a.bits.Load()
		newVal := math.Float64frombits(old) + delta
		if a.bits.CompareAndSwap(old, math.Float64bits(newVal)) {
			return
		}
	}
}

// --- Counter ---

type Counter struct {
	value atomic.Int64
}

func (c *Counter) Add(n int64) {
	c.value.Add(n)
}

func (c *Counter) Get() int64 {
	return c.value.Load()
}

func (c *Counter) Reset() int64 {
	return c.value.Swap(0)
}

// --- Gauge ---

type Gauge struct {
	value atomicFloat64
}

func (g *Gauge) Set(v float64) {
	g.value.Store(v)
}

func (g *Gauge) Inc() {
	g.value.Add(1)
}

func (g *Gauge) Dec() {
	g.value.Add(-1)
}

func (g *Gauge) Get() float64 {
	return g.value.Load()
}

// --- Histogram ---

type Histogram struct {
	mu         sync.Mutex
	boundaries []float64
	buckets    []int64
	count      int64
	sum        float64
}

func NewHistogram(boundaries []float64) *Histogram {
	return &Histogram{
		boundaries: boundaries,
		buckets:    make([]int64, len(boundaries)+1), // +1 for +Inf
	}
}

func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sum += value

	placed := false
	for i, bound := range h.boundaries {
		if value <= bound {
			h.buckets[i]++
			placed = true
			break
		}
	}
	if !placed {
		h.buckets[len(h.boundaries)]++ // +Inf bucket
	}
}

type HistogramSnapshot struct {
	Boundaries []float64
	Buckets    []int64 // cumulative
	Count      int64
	Sum        float64
}

func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	cumBuckets := make([]int64, len(h.buckets))
	copy(cumBuckets, h.buckets)

	// Convert to cumulative.
	for i := 1; i < len(cumBuckets); i++ {
		cumBuckets[i] += cumBuckets[i-1]
	}

	return HistogramSnapshot{
		Boundaries: h.boundaries,
		Buckets:    cumBuckets,
		Count:      h.count,
		Sum:        h.sum,
	}
}

func (h *Histogram) Reset() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	snap := HistogramSnapshot{
		Boundaries: h.boundaries,
		Buckets:    make([]int64, len(h.buckets)),
		Count:      h.count,
		Sum:        h.sum,
	}
	copy(snap.Buckets, h.buckets)
	for i := 1; i < len(snap.Buckets); i++ {
		snap.Buckets[i] += snap.Buckets[i-1]
	}

	// Reset.
	for i := range h.buckets {
		h.buckets[i] = 0
	}
	h.count = 0
	h.sum = 0

	return snap
}
```

### Metric Registry and Cardinality Protection

```go
// registry.go
package metricspipe

import (
	"fmt"
	"sync"
)

type Registry struct {
	mu               sync.RWMutex
	counters         map[string]*Counter
	gauges           map[string]*Gauge
	histograms       map[string]*Histogram
	cardinalityLimit int
	cardinalityCounts map[string]int // metricName -> distinct label combos
	dropped          atomic.Int64
}

func NewRegistry(cardinalityLimit int) *Registry {
	return &Registry{
		counters:          make(map[string]*Counter),
		gauges:            make(map[string]*Gauge),
		histograms:        make(map[string]*Histogram),
		cardinalityLimit:  cardinalityLimit,
		cardinalityCounts: make(map[string]int),
	}
}

func (r *Registry) checkCardinality(name, key string) error {
	if r.cardinalityLimit <= 0 {
		return nil
	}
	count := r.cardinalityCounts[name]
	// Check if this specific key already exists (not a new combo).
	if _, exists := r.counters[key]; exists {
		return nil
	}
	if _, exists := r.gauges[key]; exists {
		return nil
	}
	if _, exists := r.histograms[key]; exists {
		return nil
	}
	if count >= r.cardinalityLimit {
		r.dropped.Add(1)
		return fmt.Errorf("cardinality limit exceeded for %s", name)
	}
	r.cardinalityCounts[name] = count + 1
	return nil
}

func (r *Registry) GetCounter(name string, labels Labels) (*Counter, error) {
	key := labelsKey(name, labels)

	r.mu.RLock()
	if c, ok := r.counters[key]; ok {
		r.mu.RUnlock()
		return c, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.counters[key]; ok {
		return c, nil
	}
	if err := r.checkCardinality(name, key); err != nil {
		return nil, err
	}
	c := &Counter{}
	r.counters[key] = c
	return c, nil
}

func (r *Registry) GetGauge(name string, labels Labels) (*Gauge, error) {
	key := labelsKey(name, labels)

	r.mu.RLock()
	if g, ok := r.gauges[key]; ok {
		r.mu.RUnlock()
		return g, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if g, ok := r.gauges[key]; ok {
		return g, nil
	}
	if err := r.checkCardinality(name, key); err != nil {
		return nil, err
	}
	g := &Gauge{}
	r.gauges[key] = g
	return g, nil
}

func (r *Registry) GetHistogram(name string, labels Labels, buckets []float64) (*Histogram, error) {
	key := labelsKey(name, labels)

	r.mu.RLock()
	if h, ok := r.histograms[key]; ok {
		r.mu.RUnlock()
		return h, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if h, ok := r.histograms[key]; ok {
		return h, nil
	}
	if err := r.checkCardinality(name, key); err != nil {
		return nil, err
	}
	h := NewHistogram(buckets)
	r.histograms[key] = h
	return h, nil
}

func (r *Registry) DroppedCount() int64 {
	return r.dropped.Load()
}
```

### StatsD Parser

```go
// statsd.go
package metricspipe

import (
	"fmt"
	"strconv"
	"strings"
)

type StatsdMetric struct {
	Name       string
	Value      float64
	Type       string // "c", "g", "ms"
	SampleRate float64
}

func ParseStatsd(line string) (StatsdMetric, error) {
	// Format: metric.name:value|type|@sample_rate
	colonIdx := strings.IndexByte(line, ':')
	if colonIdx < 0 {
		return StatsdMetric{}, fmt.Errorf("missing colon separator")
	}

	name := line[:colonIdx]
	remainder := line[colonIdx+1:]

	parts := strings.Split(remainder, "|")
	if len(parts) < 2 {
		return StatsdMetric{}, fmt.Errorf("missing type separator")
	}

	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return StatsdMetric{}, fmt.Errorf("invalid value: %w", err)
	}

	metricType := parts[1]
	if metricType != "c" && metricType != "g" && metricType != "ms" {
		return StatsdMetric{}, fmt.Errorf("unknown metric type: %s", metricType)
	}

	sampleRate := 1.0
	if len(parts) >= 3 && strings.HasPrefix(parts[2], "@") {
		sr, err := strconv.ParseFloat(parts[2][1:], 64)
		if err != nil {
			return StatsdMetric{}, fmt.Errorf("invalid sample rate: %w", err)
		}
		sampleRate = sr
	}

	m := StatsdMetric{
		Name:       name,
		Value:      value,
		Type:       metricType,
		SampleRate: sampleRate,
	}

	// Apply sample rate correction for counters.
	if metricType == "c" && sampleRate > 0 && sampleRate < 1 {
		m.Value = value / sampleRate
	}

	return m, nil
}
```

### Prometheus Exporter

```go
// prometheus.go
package metricspipe

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type PrometheusExporter struct {
	registry *Registry
}

func NewPrometheusExporter(registry *Registry) *PrometheusExporter {
	return &PrometheusExporter{registry: registry}
}

func (e *PrometheusExporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var lines []string

	e.registry.mu.RLock()
	defer e.registry.mu.RUnlock()

	// Counters
	counterNames := make(map[string]bool)
	for key := range e.registry.counters {
		name := extractName(key)
		counterNames[name] = true
	}
	for name := range counterNames {
		lines = append(lines, fmt.Sprintf("# TYPE %s counter", name))
	}
	for key, c := range e.registry.counters {
		name, labelStr := extractNameAndLabels(key)
		lines = append(lines, fmt.Sprintf("%s%s %d", name, labelStr, c.Get()))
	}

	// Gauges
	gaugeNames := make(map[string]bool)
	for key := range e.registry.gauges {
		name := extractName(key)
		gaugeNames[name] = true
	}
	for name := range gaugeNames {
		lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
	}
	for key, g := range e.registry.gauges {
		name, labelStr := extractNameAndLabels(key)
		lines = append(lines, fmt.Sprintf("%s%s %g", name, labelStr, g.Get()))
	}

	// Histograms
	histNames := make(map[string]bool)
	for key := range e.registry.histograms {
		name := extractName(key)
		histNames[name] = true
	}
	for name := range histNames {
		lines = append(lines, fmt.Sprintf("# TYPE %s histogram", name))
	}
	for key, h := range e.registry.histograms {
		name, labelStr := extractNameAndLabels(key)
		snap := h.Snapshot()
		for i, bound := range snap.Boundaries {
			leLabel := formatLeLabel(labelStr, bound)
			lines = append(lines, fmt.Sprintf("%s_bucket%s %d", name, leLabel, snap.Buckets[i]))
		}
		infLabel := formatLeLabel(labelStr, 0)
		infLabel = strings.Replace(infLabel, "le=\"0\"", "le=\"+Inf\"", 1)
		lines = append(lines, fmt.Sprintf("%s_bucket%s %d", name, infLabel, snap.Buckets[len(snap.Buckets)-1]))
		lines = append(lines, fmt.Sprintf("%s_sum%s %g", name, labelStr, snap.Sum))
		lines = append(lines, fmt.Sprintf("%s_count%s %d", name, labelStr, snap.Count))
	}

	sort.Strings(lines)
	output := strings.Join(lines, "\n") + "\n"
	w.Write([]byte(output))
}

func extractName(key string) string {
	if idx := strings.IndexByte(key, '|'); idx >= 0 {
		return key[:idx]
	}
	return key
}

func extractNameAndLabels(key string) (string, string) {
	parts := strings.SplitN(key, "|", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	name := parts[0]
	labelParts := strings.Split(parts[1], "|")
	var labels []string
	for _, lp := range labelParts {
		kv := strings.SplitN(lp, "=", 2)
		if len(kv) == 2 {
			labels = append(labels, fmt.Sprintf("%s=\"%s\"", kv[0], kv[1]))
		}
	}
	if len(labels) == 0 {
		return name, ""
	}
	return name, "{" + strings.Join(labels, ",") + "}"
}

func formatLeLabel(existingLabels string, bound float64) string {
	le := fmt.Sprintf("le=\"%g\"", bound)
	if existingLabels == "" {
		return "{" + le + "}"
	}
	// Insert le into existing labels.
	return strings.TrimSuffix(existingLabels, "}") + "," + le + "}"
}
```

### StatsD Listener

```go
// listener.go
package metricspipe

import (
	"net"
	"strings"
	"sync"
)

type StatsdListener struct {
	conn     *net.UDPConn
	registry *Registry
	buckets  []float64
	wg       sync.WaitGroup
	quit     chan struct{}
}

func NewStatsdListener(addr string, registry *Registry, defaultBuckets []float64) (*StatsdListener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	return &StatsdListener{
		conn:     conn,
		registry: registry,
		buckets:  defaultBuckets,
		quit:     make(chan struct{}),
	}, nil
}

func (l *StatsdListener) Start() {
	l.wg.Add(1)
	go l.loop()
}

func (l *StatsdListener) loop() {
	defer l.wg.Done()
	buf := make([]byte, 65535)

	for {
		select {
		case <-l.quit:
			return
		default:
		}

		n, _, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		lines := strings.Split(string(buf[:n]), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			m, err := ParseStatsd(line)
			if err != nil {
				continue
			}
			l.applyMetric(m)
		}
	}
}

func (l *StatsdListener) applyMetric(m StatsdMetric) {
	labels := Labels{}

	switch m.Type {
	case "c":
		c, err := l.registry.GetCounter(m.Name, labels)
		if err != nil {
			return
		}
		c.Add(int64(m.Value))
	case "g":
		g, err := l.registry.GetGauge(m.Name, labels)
		if err != nil {
			return
		}
		g.Set(m.Value)
	case "ms":
		h, err := l.registry.GetHistogram(m.Name, labels, l.buckets)
		if err != nil {
			return
		}
		h.Observe(m.Value)
	}
}

func (l *StatsdListener) Stop() {
	close(l.quit)
	l.conn.Close()
	l.wg.Wait()
}
```

### Tests

```go
// metrics_test.go
package metricspipe

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCounterBasic(t *testing.T) {
	c := &Counter{}
	c.Add(5)
	c.Add(3)
	if c.Get() != 8 {
		t.Errorf("expected 8, got %d", c.Get())
	}
}

func TestCounterConcurrent(t *testing.T) {
	c := &Counter{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				c.Add(1)
			}
		}()
	}
	wg.Wait()
	if c.Get() != 100000 {
		t.Errorf("expected 100000, got %d", c.Get())
	}
}

func TestGaugeSetAndRead(t *testing.T) {
	g := &Gauge{}
	g.Set(42.5)
	if g.Get() != 42.5 {
		t.Errorf("expected 42.5, got %f", g.Get())
	}
	g.Inc()
	if g.Get() != 43.5 {
		t.Errorf("expected 43.5, got %f", g.Get())
	}
	g.Dec()
	if g.Get() != 42.5 {
		t.Errorf("expected 42.5, got %f", g.Get())
	}
}

func TestHistogramBuckets(t *testing.T) {
	h := NewHistogram([]float64{10, 50, 100, 250, 500})

	values := []float64{5, 15, 45, 75, 120, 300, 600}
	for _, v := range values {
		h.Observe(v)
	}

	snap := h.Snapshot()
	if snap.Count != 7 {
		t.Errorf("expected count 7, got %d", snap.Count)
	}

	// Cumulative: le=10: 1, le=50: 3, le=100: 4, le=250: 5, le=500: 6, +Inf: 7
	expected := []int64{1, 3, 4, 5, 6, 7}
	for i, exp := range expected {
		if snap.Buckets[i] != exp {
			t.Errorf("bucket[%d]: expected %d, got %d", i, exp, snap.Buckets[i])
		}
	}
}

func TestStatsdParser(t *testing.T) {
	tests := []struct {
		input string
		name  string
		value float64
		mtype string
	}{
		{"http.requests:1|c", "http.requests", 1, "c"},
		{"cpu.usage:72.5|g", "cpu.usage", 72.5, "g"},
		{"api.latency:34|ms", "api.latency", 34, "ms"},
		{"requests:1|c|@0.5", "requests", 2, "c"}, // sample rate corrected
	}

	for _, tc := range tests {
		m, err := ParseStatsd(tc.input)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.input, err)
			continue
		}
		if m.Name != tc.name {
			t.Errorf("%s: expected name %s, got %s", tc.input, tc.name, m.Name)
		}
		if m.Value != tc.value {
			t.Errorf("%s: expected value %f, got %f", tc.input, tc.value, m.Value)
		}
		if m.Type != tc.mtype {
			t.Errorf("%s: expected type %s, got %s", tc.input, tc.mtype, m.Type)
		}
	}
}

func TestStatsdParserInvalid(t *testing.T) {
	invalids := []string{
		"nocolon",
		"name:notanumber|c",
		"name:1|x",
		"name|1|c",
	}
	for _, input := range invalids {
		if _, err := ParseStatsd(input); err == nil {
			t.Errorf("expected error for %q", input)
		}
	}
}

func TestPrometheusExporter(t *testing.T) {
	reg := NewRegistry(100)
	c, _ := reg.GetCounter("http_requests_total", Labels{"method": "GET"})
	c.Add(42)

	g, _ := reg.GetGauge("temperature", Labels{})
	g.Set(23.5)

	h, _ := reg.GetHistogram("latency_seconds", Labels{}, []float64{0.01, 0.05, 0.1, 0.5, 1})
	h.Observe(0.035)
	h.Observe(0.12)

	exporter := NewPrometheusExporter(reg)
	ts := httptest.NewServer(exporter)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	output := string(body)
	if !strings.Contains(output, "http_requests_total") {
		t.Error("missing counter in output")
	}
	if !strings.Contains(output, "temperature") {
		t.Error("missing gauge in output")
	}
	if !strings.Contains(output, "latency_seconds_bucket") {
		t.Error("missing histogram buckets in output")
	}
	if !strings.Contains(output, "latency_seconds_count") {
		t.Error("missing histogram count in output")
	}
	if !strings.Contains(output, "+Inf") {
		t.Error("missing +Inf bucket")
	}
	t.Log("\n" + output)
}

func TestCardinalityProtection(t *testing.T) {
	reg := NewRegistry(3)

	for i := 0; i < 5; i++ {
		_, err := reg.GetCounter("requests", Labels{"path": fmt.Sprintf("/api/%d", i)})
		if i >= 3 && err == nil {
			t.Errorf("expected cardinality error at i=%d", i)
		}
	}

	if reg.DroppedCount() < 2 {
		t.Errorf("expected at least 2 drops, got %d", reg.DroppedCount())
	}
}

func TestStatsdListenerIntegration(t *testing.T) {
	reg := NewRegistry(100)
	buckets := []float64{10, 50, 100, 250, 500}

	listener, err := NewStatsdListener("127.0.0.1:0", reg, buckets)
	if err != nil {
		t.Fatal(err)
	}
	listener.Start()
	defer listener.Stop()

	addr := listener.conn.LocalAddr().(*net.UDPAddr)
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}

	metrics := []string{
		"web.requests:1|c",
		"web.requests:1|c",
		"web.requests:1|c",
		"cpu.load:65.2|g",
		"api.latency:42|ms",
	}

	for _, m := range metrics {
		conn.Write([]byte(m + "\n"))
	}

	time.Sleep(100 * time.Millisecond)

	c, err := reg.GetCounter("web.requests", Labels{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Get() != 3 {
		t.Errorf("expected counter=3, got %d", c.Get())
	}

	g, err := reg.GetGauge("cpu.load", Labels{})
	if err != nil {
		t.Fatal(err)
	}
	if g.Get() != 65.2 {
		t.Errorf("expected gauge=65.2, got %f", g.Get())
	}
}

func BenchmarkCounterAdd(b *testing.B) {
	c := &Counter{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Add(1)
		}
	})
}

func BenchmarkHistogramObserve(b *testing.B) {
	h := NewHistogram([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10})
	b.RunParallel(func(pb *testing.PB) {
		v := 0.042
		for pb.Next() {
			h.Observe(v)
		}
	})
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -bench=. -benchmem ./...
```

### Expected Output

```
=== RUN   TestCounterBasic
--- PASS: TestCounterBasic (0.00s)
=== RUN   TestCounterConcurrent
--- PASS: TestCounterConcurrent (0.01s)
=== RUN   TestGaugeSetAndRead
--- PASS: TestGaugeSetAndRead (0.00s)
=== RUN   TestHistogramBuckets
--- PASS: TestHistogramBuckets (0.00s)
=== RUN   TestStatsdParser
--- PASS: TestStatsdParser (0.00s)
=== RUN   TestStatsdParserInvalid
--- PASS: TestStatsdParserInvalid (0.00s)
=== RUN   TestPrometheusExporter
    metrics_test.go:166:
    # TYPE http_requests_total counter
    http_requests_total{method="GET"} 42
    # TYPE latency_seconds histogram
    latency_seconds_bucket{le="0.01"} 0
    ...
--- PASS: TestPrometheusExporter (0.00s)
=== RUN   TestCardinalityProtection
--- PASS: TestCardinalityProtection (0.00s)
=== RUN   TestStatsdListenerIntegration
--- PASS: TestStatsdListenerIntegration (0.10s)
PASS

BenchmarkCounterAdd-8      200000000    3.2 ns/op    0 B/op    0 allocs/op
BenchmarkHistogramObserve-8  5000000  280 ns/op      0 B/op    0 allocs/op
```

## Rust Solution

### Project Setup

```bash
cargo new metricspipe --lib && cd metricspipe
```

Add to `Cargo.toml`:

```toml
[dependencies]
tiny_http = "0.12"

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "metrics_bench"
harness = false
```

### Implementation

```rust
// src/lib.rs
use std::collections::HashMap;
use std::net::UdpSocket;
use std::sync::atomic::{AtomicI64, AtomicU64, Ordering};
use std::sync::{Arc, Mutex, RwLock};
use std::thread;

// --- Atomic Float64 ---

pub struct AtomicF64 {
    bits: AtomicU64,
}

impl AtomicF64 {
    pub fn new(val: f64) -> Self {
        Self { bits: AtomicU64::new(val.to_bits()) }
    }

    pub fn store(&self, val: f64) {
        self.bits.store(val.to_bits(), Ordering::Release);
    }

    pub fn load(&self) -> f64 {
        f64::from_bits(self.bits.load(Ordering::Acquire))
    }

    pub fn add(&self, delta: f64) {
        loop {
            let old = self.bits.load(Ordering::Acquire);
            let new_val = f64::from_bits(old) + delta;
            if self.bits.compare_exchange_weak(
                old, new_val.to_bits(), Ordering::AcqRel, Ordering::Acquire
            ).is_ok() {
                return;
            }
        }
    }
}

// --- Counter ---

pub struct Counter {
    value: AtomicI64,
}

impl Counter {
    pub fn new() -> Self {
        Self { value: AtomicI64::new(0) }
    }

    pub fn add(&self, n: i64) {
        self.value.fetch_add(n, Ordering::Relaxed);
    }

    pub fn get(&self) -> i64 {
        self.value.load(Ordering::Relaxed)
    }

    pub fn reset(&self) -> i64 {
        self.value.swap(0, Ordering::AcqRel)
    }
}

// --- Gauge ---

pub struct Gauge {
    value: AtomicF64,
}

impl Gauge {
    pub fn new() -> Self {
        Self { value: AtomicF64::new(0.0) }
    }

    pub fn set(&self, v: f64) { self.value.store(v); }
    pub fn get(&self) -> f64 { self.value.load() }
    pub fn inc(&self) { self.value.add(1.0); }
    pub fn dec(&self) { self.value.add(-1.0); }
}

// --- Histogram ---

pub struct Histogram {
    inner: Mutex<HistogramInner>,
    boundaries: Vec<f64>,
}

struct HistogramInner {
    buckets: Vec<i64>,
    count: i64,
    sum: f64,
}

#[derive(Debug, Clone)]
pub struct HistogramSnapshot {
    pub boundaries: Vec<f64>,
    pub buckets: Vec<i64>, // cumulative
    pub count: i64,
    pub sum: f64,
}

impl Histogram {
    pub fn new(boundaries: Vec<f64>) -> Self {
        let bucket_count = boundaries.len() + 1;
        Self {
            inner: Mutex::new(HistogramInner {
                buckets: vec![0; bucket_count],
                count: 0,
                sum: 0.0,
            }),
            boundaries,
        }
    }

    pub fn observe(&self, value: f64) {
        let mut inner = self.inner.lock().unwrap();
        inner.count += 1;
        inner.sum += value;

        let mut placed = false;
        for (i, &bound) in self.boundaries.iter().enumerate() {
            if value <= bound {
                inner.buckets[i] += 1;
                placed = true;
                break;
            }
        }
        if !placed {
            let last = inner.buckets.len() - 1;
            inner.buckets[last] += 1;
        }
    }

    pub fn snapshot(&self) -> HistogramSnapshot {
        let inner = self.inner.lock().unwrap();
        let mut cum = inner.buckets.clone();
        for i in 1..cum.len() {
            cum[i] += cum[i - 1];
        }
        HistogramSnapshot {
            boundaries: self.boundaries.clone(),
            buckets: cum,
            count: inner.count,
            sum: inner.sum,
        }
    }
}

// --- Registry ---

pub struct Registry {
    counters: RwLock<HashMap<String, Arc<Counter>>>,
    gauges: RwLock<HashMap<String, Arc<Gauge>>>,
    histograms: RwLock<HashMap<String, Arc<Histogram>>>,
    cardinality_limit: usize,
    cardinality_counts: Mutex<HashMap<String, usize>>,
    dropped: AtomicI64,
}

impl Registry {
    pub fn new(cardinality_limit: usize) -> Self {
        Self {
            counters: RwLock::new(HashMap::new()),
            gauges: RwLock::new(HashMap::new()),
            histograms: RwLock::new(HashMap::new()),
            cardinality_limit,
            cardinality_counts: Mutex::new(HashMap::new()),
            dropped: AtomicI64::new(0),
        }
    }

    fn labels_key(name: &str, labels: &HashMap<String, String>) -> String {
        let mut key = name.to_string();
        let mut pairs: Vec<_> = labels.iter().collect();
        pairs.sort_by_key(|(k, _)| k.clone());
        for (k, v) in pairs {
            key.push('|');
            key.push_str(k);
            key.push('=');
            key.push_str(v);
        }
        key
    }

    pub fn counter(&self, name: &str, labels: HashMap<String, String>) -> Option<Arc<Counter>> {
        let key = Self::labels_key(name, &labels);

        {
            let read = self.counters.read().unwrap();
            if let Some(c) = read.get(&key) {
                return Some(c.clone());
            }
        }

        let mut write = self.counters.write().unwrap();
        if let Some(c) = write.get(&key) {
            return Some(c.clone());
        }

        if self.cardinality_limit > 0 {
            let mut counts = self.cardinality_counts.lock().unwrap();
            let count = counts.entry(name.to_string()).or_insert(0);
            if *count >= self.cardinality_limit {
                self.dropped.fetch_add(1, Ordering::Relaxed);
                return None;
            }
            *count += 1;
        }

        let c = Arc::new(Counter::new());
        write.insert(key, c.clone());
        Some(c)
    }

    pub fn gauge(&self, name: &str, labels: HashMap<String, String>) -> Option<Arc<Gauge>> {
        let key = Self::labels_key(name, &labels);

        {
            let read = self.gauges.read().unwrap();
            if let Some(g) = read.get(&key) {
                return Some(g.clone());
            }
        }

        let mut write = self.gauges.write().unwrap();
        if let Some(g) = write.get(&key) {
            return Some(g.clone());
        }

        if self.cardinality_limit > 0 {
            let mut counts = self.cardinality_counts.lock().unwrap();
            let count = counts.entry(name.to_string()).or_insert(0);
            if *count >= self.cardinality_limit {
                self.dropped.fetch_add(1, Ordering::Relaxed);
                return None;
            }
            *count += 1;
        }

        let g = Arc::new(Gauge::new());
        write.insert(key, g.clone());
        Some(g)
    }

    pub fn histogram(
        &self, name: &str, labels: HashMap<String, String>, buckets: Vec<f64>
    ) -> Option<Arc<Histogram>> {
        let key = Self::labels_key(name, &labels);

        {
            let read = self.histograms.read().unwrap();
            if let Some(h) = read.get(&key) {
                return Some(h.clone());
            }
        }

        let mut write = self.histograms.write().unwrap();
        if let Some(h) = write.get(&key) {
            return Some(h.clone());
        }

        if self.cardinality_limit > 0 {
            let mut counts = self.cardinality_counts.lock().unwrap();
            let count = counts.entry(name.to_string()).or_insert(0);
            if *count >= self.cardinality_limit {
                self.dropped.fetch_add(1, Ordering::Relaxed);
                return None;
            }
            *count += 1;
        }

        let h = Arc::new(Histogram::new(buckets));
        write.insert(key, h.clone());
        Some(h)
    }

    pub fn dropped_count(&self) -> i64 {
        self.dropped.load(Ordering::Relaxed)
    }
}

// --- StatsD Parser ---

pub struct StatsdMetric {
    pub name: String,
    pub value: f64,
    pub metric_type: String,
    pub sample_rate: f64,
}

pub fn parse_statsd(line: &str) -> Result<StatsdMetric, String> {
    let colon_idx = line.find(':').ok_or("missing colon")?;
    let name = &line[..colon_idx];
    let remainder = &line[colon_idx + 1..];

    let parts: Vec<&str> = remainder.split('|').collect();
    if parts.len() < 2 {
        return Err("missing type".into());
    }

    let mut value: f64 = parts[0].parse().map_err(|e: std::num::ParseFloatError| e.to_string())?;
    let metric_type = parts[1].to_string();

    if metric_type != "c" && metric_type != "g" && metric_type != "ms" {
        return Err(format!("unknown type: {}", metric_type));
    }

    let mut sample_rate = 1.0;
    if parts.len() >= 3 && parts[2].starts_with('@') {
        sample_rate = parts[2][1..].parse().map_err(|e: std::num::ParseFloatError| e.to_string())?;
    }

    if metric_type == "c" && sample_rate > 0.0 && sample_rate < 1.0 {
        value /= sample_rate;
    }

    Ok(StatsdMetric {
        name: name.to_string(),
        value,
        metric_type,
        sample_rate,
    })
}

// --- StatsD Listener ---

pub fn start_statsd_listener(
    addr: &str,
    registry: Arc<Registry>,
    default_buckets: Vec<f64>,
) -> Result<(std::net::SocketAddr, Arc<AtomicI64>), String> {
    let socket = UdpSocket::bind(addr).map_err(|e| e.to_string())?;
    let local_addr = socket.local_addr().map_err(|e| e.to_string())?;
    let stop = Arc::new(AtomicI64::new(0));
    let stop_clone = stop.clone();

    socket.set_read_timeout(Some(std::time::Duration::from_millis(100))).ok();

    thread::spawn(move || {
        let mut buf = [0u8; 65535];
        while stop_clone.load(Ordering::Relaxed) == 0 {
            let n = match socket.recv(&mut buf) {
                Ok(n) => n,
                Err(_) => continue,
            };
            let data = String::from_utf8_lossy(&buf[..n]);
            for line in data.lines() {
                let line = line.trim();
                if line.is_empty() { continue; }
                if let Ok(m) = parse_statsd(line) {
                    let labels = HashMap::new();
                    match m.metric_type.as_str() {
                        "c" => {
                            if let Some(c) = registry.counter(&m.name, labels) {
                                c.add(m.value as i64);
                            }
                        }
                        "g" => {
                            if let Some(g) = registry.gauge(&m.name, labels) {
                                g.set(m.value);
                            }
                        }
                        "ms" => {
                            if let Some(h) = registry.histogram(
                                &m.name, labels, default_buckets.clone()
                            ) {
                                h.observe(m.value);
                            }
                        }
                        _ => {}
                    }
                }
            }
        }
    });

    Ok((local_addr, stop))
}
```

### Rust Tests

```rust
// src/lib.rs (tests module)
#[cfg(test)]
mod tests {
    use super::*;
    use std::net::UdpSocket;

    #[test]
    fn test_counter_concurrent() {
        let c = Arc::new(Counter::new());
        let mut handles = vec![];
        for _ in 0..100 {
            let c = c.clone();
            handles.push(thread::spawn(move || {
                for _ in 0..1000 {
                    c.add(1);
                }
            }));
        }
        for h in handles { h.join().unwrap(); }
        assert_eq!(c.get(), 100_000);
    }

    #[test]
    fn test_gauge() {
        let g = Gauge::new();
        g.set(42.5);
        assert!((g.get() - 42.5).abs() < f64::EPSILON);
        g.inc();
        assert!((g.get() - 43.5).abs() < f64::EPSILON);
    }

    #[test]
    fn test_histogram_buckets() {
        let h = Histogram::new(vec![10.0, 50.0, 100.0]);
        for &v in &[5.0, 15.0, 45.0, 75.0, 120.0] {
            h.observe(v);
        }
        let snap = h.snapshot();
        assert_eq!(snap.count, 5);
        // cumulative: le=10: 1, le=50: 3, le=100: 4, +Inf: 5
        assert_eq!(snap.buckets, vec![1, 3, 4, 5]);
    }

    #[test]
    fn test_statsd_parser() {
        let m = parse_statsd("http.requests:1|c").unwrap();
        assert_eq!(m.name, "http.requests");
        assert_eq!(m.value, 1.0);

        let m = parse_statsd("requests:1|c|@0.5").unwrap();
        assert_eq!(m.value, 2.0); // sample rate corrected

        assert!(parse_statsd("invalid").is_err());
    }

    #[test]
    fn test_cardinality_limit() {
        let reg = Arc::new(Registry::new(3));
        for i in 0..5 {
            let mut labels = HashMap::new();
            labels.insert("path".into(), format!("/api/{}", i));
            let result = reg.counter("requests", labels);
            if i >= 3 {
                assert!(result.is_none(), "should be rejected at i={}", i);
            }
        }
        assert!(reg.dropped_count() >= 2);
    }

    #[test]
    fn test_statsd_listener_integration() {
        let reg = Arc::new(Registry::new(100));
        let buckets = vec![10.0, 50.0, 100.0];
        let (addr, stop) = start_statsd_listener("127.0.0.1:0", reg.clone(), buckets).unwrap();

        let sender = UdpSocket::bind("127.0.0.1:0").unwrap();
        let packets = "web.hits:1|c\nweb.hits:1|c\ncpu:65|g\n";
        sender.send_to(packets.as_bytes(), addr).unwrap();

        thread::sleep(std::time::Duration::from_millis(200));

        let c = reg.counter("web.hits", HashMap::new()).unwrap();
        assert_eq!(c.get(), 2);

        let g = reg.gauge("cpu", HashMap::new()).unwrap();
        assert!((g.get() - 65.0).abs() < f64::EPSILON);

        stop.store(1, Ordering::Relaxed);
    }
}
```

### Rust Benchmark

```rust
// benches/metrics_bench.rs
use criterion::{criterion_group, criterion_main, Criterion};
use metricspipe::{Counter, Histogram};
use std::sync::Arc;

fn bench_counter_add(c: &mut Criterion) {
    let counter = Arc::new(Counter::new());
    c.bench_function("counter_add", |b| {
        b.iter(|| counter.add(1));
    });
}

fn bench_histogram_observe(c: &mut Criterion) {
    let h = Arc::new(Histogram::new(vec![0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0]));
    c.bench_function("histogram_observe", |b| {
        b.iter(|| h.observe(0.042));
    });
}

criterion_group!(benches, bench_counter_add, bench_histogram_observe);
criterion_main!(benches);
```

### Running the Rust Tests

```bash
cargo test -- --nocapture
cargo bench
```

## Design Decisions

**Decision 1: Atomic operations for counters and gauges, mutex for histograms.** Counters use `AtomicI64` for lock-free increments. Gauges use a CAS loop on `AtomicU64` (storing float64 bits). Histograms require a mutex because observing a value updates multiple fields (bucket counts, sum, count) that must be consistent. The alternative (per-bucket atomics) avoids the lock but makes snapshots non-atomic -- you could read bucket counts from different points in time.

**Decision 2: Cumulative histogram buckets in the Prometheus format.** Prometheus expects cumulative buckets: the `le=100` bucket includes all observations <= 100, not just those between 50 and 100. This is computed at snapshot time from the internal non-cumulative representation. Storing internally as non-cumulative makes the observe path simpler (increment one bucket), while cumulative output is only needed at export time.

**Decision 3: RwLock on the registry, not on individual metrics.** The registry uses RwLock for the lookup maps. Once a metric is retrieved, all operations on it (Add, Set, Observe) are lock-free (counters, gauges) or use a per-metric mutex (histograms). This means the registry lock is only held for the map lookup, not for the metric operation itself.

**Decision 4: Sample rate correction in the parser, not the metric.** When a StatsD metric arrives with `@0.5`, the parser immediately doubles the value. This keeps the metric types unaware of sampling semantics. The alternative (storing the sample rate and correcting at export time) is more accurate for gauges but adds complexity.

## Common Mistakes

**Mistake 1: Using a global mutex for all metric operations.** This serializes all counter increments, gauge updates, and histogram observations across all metrics. At high throughput, this becomes the bottleneck long before CPU or memory limits.

**Mistake 2: Non-cumulative histogram buckets in Prometheus output.** Prometheus expects cumulative buckets. If you emit non-cumulative values, the `rate()` function in PromQL produces nonsensical results, and alerts based on percentile calculations will be wrong.

**Mistake 3: Forgetting the +Inf bucket.** Prometheus histograms always have a `+Inf` bucket that equals the total count. Omitting it causes `histogram_quantile()` to return NaN for percentiles above the highest explicit bucket.

**Mistake 4: Unbounded label cardinality.** Without cardinality protection, a label with high cardinality (user IDs, request paths with query params) creates millions of time series, exhausting memory and slowing exports to a crawl.

## Performance Notes

- Counter `Add` is a single atomic increment: ~3ns on modern x86. This is the fastest possible metric operation and handles millions of operations per second per core.
- Histogram `Observe` holds a mutex and scans the boundary list: ~100-300ns depending on bucket count. For extremely hot paths, consider pre-sharding histograms by goroutine/thread ID and merging at export time.
- The Prometheus exporter iterates all metrics on each scrape. For registries with 10,000+ time series, cache the output and invalidate on window rotation.
- The StatsD listener uses a single UDP socket. Under very high packet rates, the kernel's UDP receive buffer may overflow. Increase the buffer with `setsockopt(SO_RCVBUF)` or run multiple listeners.
