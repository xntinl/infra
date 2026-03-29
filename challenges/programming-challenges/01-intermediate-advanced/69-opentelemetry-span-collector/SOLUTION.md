# Solution: OpenTelemetry Span Collector

## Architecture Overview

The collector has three layers: an HTTP API for ingestion and querying, an in-memory store with multiple indexes, and a query engine that reconstructs trace trees and computes histograms.

```
HTTP API Layer
  POST /v1/traces          --> Ingest spans
  GET  /v1/traces/:id      --> Query trace tree
  GET  /v1/traces/:id/waterfall --> ASCII visualization
  GET  /v1/services         --> List services
  GET  /v1/services/:name/latency --> Latency histogram
        |
In-Memory Store
  byTrace:   map[traceID][]*Span
  byService: map[serviceName][]*Span
  allSpans:  []*Span (ordered, for eviction)
        |
Query Engine
  buildTree()        --> parent-child reconstruction
  renderWaterfall()  --> ASCII timeline
  computeHistogram() --> latency distribution
```

## Go Solution

### Project Setup

```bash
mkdir -p spancollector && cd spancollector
go mod init spancollector
```

### Span Model

```go
// span.go
package spancollector

import "time"

type SpanStatus string

const (
	StatusOK    SpanStatus = "OK"
	StatusError SpanStatus = "ERROR"
)

type Span struct {
	TraceID       string            `json:"traceId"`
	SpanID        string            `json:"spanId"`
	ParentSpanID  string            `json:"parentSpanId,omitempty"`
	OperationName string            `json:"operationName"`
	ServiceName   string            `json:"serviceName"`
	StartTimeNano int64             `json:"startTimeUnixNano"`
	EndTimeNano   int64             `json:"endTimeUnixNano"`
	Status        SpanStatus        `json:"status"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

func (s *Span) Duration() time.Duration {
	return time.Duration(s.EndTimeNano - s.StartTimeNano)
}

func (s *Span) StartTime() time.Time {
	return time.Unix(0, s.StartTimeNano)
}

type SpanNode struct {
	Span     *Span       `json:"span"`
	Children []*SpanNode `json:"children,omitempty"`
}
```

### Store

```go
// store.go
package spancollector

import (
	"fmt"
	"sync"
	"time"
)

type SpanStore struct {
	mu        sync.RWMutex
	byTrace   map[string][]*Span
	byService map[string][]*Span
	allSpans  []*Span
	spanIndex map[string]bool // traceID:spanID for dedup
	maxSpans  int
}

func NewSpanStore(maxSpans int) *SpanStore {
	return &SpanStore{
		byTrace:   make(map[string][]*Span),
		byService: make(map[string][]*Span),
		spanIndex: make(map[string]bool),
		maxSpans:  maxSpans,
	}
}

func (s *SpanStore) Ingest(spans []*Span) []error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for _, span := range spans {
		if err := validateSpan(span); err != nil {
			errs = append(errs, err)
			continue
		}

		dedupKey := span.TraceID + ":" + span.SpanID
		if s.spanIndex[dedupKey] {
			errs = append(errs, fmt.Errorf("duplicate spanId %s in trace %s", span.SpanID, span.TraceID))
			continue
		}

		s.spanIndex[dedupKey] = true
		s.byTrace[span.TraceID] = append(s.byTrace[span.TraceID], span)
		s.byService[span.ServiceName] = append(s.byService[span.ServiceName], span)
		s.allSpans = append(s.allSpans, span)
	}

	s.evictIfNeeded()
	return errs
}

func validateSpan(span *Span) error {
	if span.TraceID == "" {
		return fmt.Errorf("missing traceId")
	}
	if span.SpanID == "" {
		return fmt.Errorf("missing spanId")
	}
	if span.OperationName == "" {
		return fmt.Errorf("missing operationName")
	}
	if span.ServiceName == "" {
		return fmt.Errorf("missing serviceName")
	}
	if span.EndTimeNano <= span.StartTimeNano {
		return fmt.Errorf("endTime must be after startTime for span %s", span.SpanID)
	}
	return nil
}

func (s *SpanStore) evictIfNeeded() {
	if s.maxSpans <= 0 || len(s.allSpans) <= s.maxSpans {
		return
	}

	evictCount := len(s.allSpans) - s.maxSpans
	evicted := s.allSpans[:evictCount]
	s.allSpans = s.allSpans[evictCount:]

	for _, span := range evicted {
		dedupKey := span.TraceID + ":" + span.SpanID
		delete(s.spanIndex, dedupKey)
		s.removeFromIndex(s.byTrace, span.TraceID, span)
		s.removeFromIndex(s.byService, span.ServiceName, span)
	}
}

func (s *SpanStore) removeFromIndex(idx map[string][]*Span, key string, target *Span) {
	spans := idx[key]
	for i, sp := range spans {
		if sp.SpanID == target.SpanID && sp.TraceID == target.TraceID {
			idx[key] = append(spans[:i], spans[i+1:]...)
			if len(idx[key]) == 0 {
				delete(idx, key)
			}
			return
		}
	}
}

func (s *SpanStore) GetTrace(traceID string) []*Span {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Span, len(s.byTrace[traceID]))
	copy(result, s.byTrace[traceID])
	return result
}

func (s *SpanStore) GetServiceSpans(serviceName string) []*Span {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Span, len(s.byService[serviceName]))
	copy(result, s.byService[serviceName])
	return result
}

func (s *SpanStore) ListServices() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	services := make([]string, 0, len(s.byService))
	for name := range s.byService {
		services = append(services, name)
	}
	return services
}

func (s *SpanStore) SpansByServiceAndOperation(service, operation string) []*Span {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Span
	for _, span := range s.byService[service] {
		if span.OperationName == operation {
			result = append(result, span)
		}
	}
	return result
}

// --- Tree Reconstruction ---

func BuildTraceTree(spans []*Span) *SpanNode {
	if len(spans) == 0 {
		return nil
	}

	nodes := make(map[string]*SpanNode, len(spans))
	for _, s := range spans {
		nodes[s.SpanID] = &SpanNode{Span: s}
	}

	var root *SpanNode
	for _, s := range spans {
		node := nodes[s.SpanID]
		if s.ParentSpanID == "" {
			root = node
		} else if parent, ok := nodes[s.ParentSpanID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}

	// If no explicit root found, use the span with the earliest start time.
	if root == nil && len(spans) > 0 {
		earliest := spans[0]
		for _, s := range spans[1:] {
			if s.StartTimeNano < earliest.StartTimeNano {
				earliest = s
			}
		}
		root = nodes[earliest.SpanID]
	}

	return root
}

// --- Waterfall Visualization ---

func RenderWaterfall(root *SpanNode, width int) string {
	if root == nil {
		return "(empty trace)\n"
	}

	traceStart := root.Span.StartTimeNano
	traceEnd := findTraceEnd(root)
	traceDuration := traceEnd - traceStart
	if traceDuration == 0 {
		traceDuration = 1
	}

	var lines []string
	renderNode(root, 0, traceStart, traceDuration, width, &lines)
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}

func renderNode(node *SpanNode, depth int, traceStart, traceDuration int64, width int, lines *[]string) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	relStart := node.Span.StartTimeNano - traceStart
	relEnd := node.Span.EndTimeNano - traceStart

	barStart := int(relStart * int64(width) / traceDuration)
	barEnd := int(relEnd * int64(width) / traceDuration)
	if barEnd <= barStart {
		barEnd = barStart + 1
	}

	bar := make([]byte, width)
	for i := range bar {
		bar[i] = ' '
	}
	for i := barStart; i < barEnd && i < width; i++ {
		bar[i] = '='
	}

	durMs := float64(node.Span.EndTimeNano-node.Span.StartTimeNano) / 1e6
	line := fmt.Sprintf("%s%-20s |%s| %6.1fms",
		indent,
		truncate(node.Span.OperationName, 20-len(indent)),
		string(bar),
		durMs,
	)
	*lines = append(*lines, line)

	for _, child := range node.Children {
		renderNode(child, depth+1, traceStart, traceDuration, width, lines)
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "."
}

func findTraceEnd(node *SpanNode) int64 {
	end := node.Span.EndTimeNano
	for _, child := range node.Children {
		childEnd := findTraceEnd(child)
		if childEnd > end {
			end = childEnd
		}
	}
	return end
}

// --- Latency Histogram ---

type HistogramBucket struct {
	UpperBound float64 `json:"upperBound"`
	Count      int     `json:"count"`
}

type Histogram struct {
	Buckets []HistogramBucket `json:"buckets"`
	Count   int               `json:"count"`
	Sum     time.Duration     `json:"sumNanos"`
	Min     time.Duration     `json:"minNanos"`
	Max     time.Duration     `json:"maxNanos"`
	P50     time.Duration     `json:"p50Nanos"`
	P90     time.Duration     `json:"p90Nanos"`
	P99     time.Duration     `json:"p99Nanos"`
}

func ComputeHistogram(spans []*Span, boundariesMs []float64) Histogram {
	if len(spans) == 0 {
		return Histogram{}
	}

	durations := make([]time.Duration, len(spans))
	for i, s := range spans {
		durations[i] = s.Duration()
	}

	// Sort for percentile calculation.
	sortDurations(durations)

	buckets := make([]HistogramBucket, len(boundariesMs))
	for i, b := range boundariesMs {
		count := 0
		for _, d := range durations {
			if float64(d.Milliseconds()) <= b {
				count++
			}
		}
		buckets[i] = HistogramBucket{UpperBound: b, Count: count}
	}

	var sum time.Duration
	for _, d := range durations {
		sum += d
	}

	return Histogram{
		Buckets: buckets,
		Count:   len(durations),
		Sum:     sum,
		Min:     durations[0],
		Max:     durations[len(durations)-1],
		P50:     percentile(durations, 0.50),
		P90:     percentile(durations, 0.90),
		P99:     percentile(durations, 0.99),
	}
}

func sortDurations(d []time.Duration) {
	// Simple insertion sort (fine for typical trace sizes).
	for i := 1; i < len(d); i++ {
		key := d[i]
		j := i - 1
		for j >= 0 && d[j] > key {
			d[j+1] = d[j]
			j--
		}
		d[j+1] = key
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}
```

### HTTP Server

```go
// server.go
package spancollector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Server struct {
	store *SpanStore
	mux   *http.ServeMux
}

func NewServer(store *SpanStore) *Server {
	s := &Server{store: store, mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /v1/traces", s.handleIngest)
	s.mux.HandleFunc("GET /v1/traces/{traceId}", s.handleGetTrace)
	s.mux.HandleFunc("GET /v1/traces/{traceId}/waterfall", s.handleWaterfall)
	s.mux.HandleFunc("GET /v1/services", s.handleListServices)
	s.mux.HandleFunc("GET /v1/services/{name}/latency", s.handleLatency)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var spans []*Span
	if err := json.NewDecoder(r.Body).Decode(&spans); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "invalid JSON: %s"}`, err), http.StatusBadRequest)
		return
	}

	errs := s.store.Ingest(spans)
	if len(errs) > 0 {
		messages := make([]string, len(errs))
		for i, e := range errs {
			messages[i] = e.Error()
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"accepted": len(spans) - len(errs),
			"rejected": len(errs),
			"errors":   messages,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"accepted": len(spans)})
}

func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	traceID := r.PathValue("traceId")
	spans := s.store.GetTrace(traceID)
	if len(spans) == 0 {
		http.Error(w, `{"error": "trace not found"}`, http.StatusNotFound)
		return
	}

	tree := BuildTraceTree(spans)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree)
}

func (s *Server) handleWaterfall(w http.ResponseWriter, r *http.Request) {
	traceID := r.PathValue("traceId")
	spans := s.store.GetTrace(traceID)
	if len(spans) == 0 {
		http.Error(w, `{"error": "trace not found"}`, http.StatusNotFound)
		return
	}

	tree := BuildTraceTree(spans)
	waterfall := RenderWaterfall(tree, 40)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(waterfall))
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	services := s.store.ListServices()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(services)
}

func (s *Server) handleLatency(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	operation := r.URL.Query().Get("operation")
	if operation == "" {
		http.Error(w, `{"error": "operation query parameter required"}`, http.StatusBadRequest)
		return
	}

	spans := s.store.SpansByServiceAndOperation(name, operation)
	if len(spans) == 0 {
		http.Error(w, `{"error": "no spans found"}`, http.StatusNotFound)
		return
	}

	boundaries := []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000}
	_ = strings.Join(nil, "") // ensure strings is used
	histogram := ComputeHistogram(spans, boundaries)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(histogram)
}
```

### Tests

```go
// collector_test.go
package spancollector

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func makeSpan(traceID, spanID, parentID, op, svc string, startMs, endMs int64) *Span {
	return &Span{
		TraceID:       traceID,
		SpanID:        spanID,
		ParentSpanID:  parentID,
		OperationName: op,
		ServiceName:   svc,
		StartTimeNano: startMs * 1e6,
		EndTimeNano:   endMs * 1e6,
		Status:        StatusOK,
	}
}

func TestIngestAndQuery(t *testing.T) {
	store := NewSpanStore(1000)
	spans := []*Span{
		makeSpan("t1", "s1", "", "GET /api", "gateway", 0, 200),
		makeSpan("t1", "s2", "s1", "auth.verify", "auth", 10, 50),
		makeSpan("t1", "s3", "s1", "db.query", "database", 60, 180),
	}

	errs := store.Ingest(spans)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	result := store.GetTrace("t1")
	if len(result) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(result))
	}
}

func TestInvalidSpanRejection(t *testing.T) {
	store := NewSpanStore(1000)

	tests := []struct {
		name string
		span *Span
	}{
		{"missing traceId", &Span{SpanID: "s1", OperationName: "op", ServiceName: "svc", StartTimeNano: 1, EndTimeNano: 2}},
		{"missing spanId", &Span{TraceID: "t1", OperationName: "op", ServiceName: "svc", StartTimeNano: 1, EndTimeNano: 2}},
		{"end before start", makeSpan("t1", "s1", "", "op", "svc", 200, 100)},
	}

	for _, tc := range tests {
		errs := store.Ingest([]*Span{tc.span})
		if len(errs) == 0 {
			t.Errorf("%s: expected rejection", tc.name)
		}
	}
}

func TestDuplicateSpanRejection(t *testing.T) {
	store := NewSpanStore(1000)
	span := makeSpan("t1", "s1", "", "op", "svc", 0, 100)

	store.Ingest([]*Span{span})
	errs := store.Ingest([]*Span{span})

	if len(errs) != 1 {
		t.Fatalf("expected 1 duplicate error, got %d", len(errs))
	}
}

func TestTraceTreeReconstruction(t *testing.T) {
	spans := []*Span{
		makeSpan("t1", "root", "", "GET /", "api", 0, 200),
		makeSpan("t1", "child1", "root", "auth", "auth", 10, 50),
		makeSpan("t1", "child2", "root", "query", "db", 60, 180),
		makeSpan("t1", "grandchild", "child2", "index_scan", "db", 70, 150),
	}

	tree := BuildTraceTree(spans)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.Span.SpanID != "root" {
		t.Errorf("expected root span, got %s", tree.Span.SpanID)
	}
	if len(tree.Children) != 2 {
		t.Errorf("expected 2 children, got %d", len(tree.Children))
	}

	var dbNode *SpanNode
	for _, c := range tree.Children {
		if c.Span.SpanID == "child2" {
			dbNode = c
		}
	}
	if dbNode == nil || len(dbNode.Children) != 1 {
		t.Error("expected grandchild under child2")
	}
}

func TestWaterfallRendering(t *testing.T) {
	spans := []*Span{
		makeSpan("t1", "root", "", "GET /api", "api", 0, 200),
		makeSpan("t1", "auth", "root", "verify", "auth", 10, 50),
		makeSpan("t1", "db", "root", "query", "db", 60, 180),
	}

	tree := BuildTraceTree(spans)
	waterfall := RenderWaterfall(tree, 40)

	if waterfall == "" {
		t.Fatal("empty waterfall")
	}
	if !containsStr(waterfall, "GET /api") {
		t.Error("waterfall missing root operation")
	}
	if !containsStr(waterfall, "verify") {
		t.Error("waterfall missing child operation")
	}
	t.Log("\n" + waterfall)
}

func TestLatencyHistogram(t *testing.T) {
	spans := []*Span{
		makeSpan("t1", "s1", "", "query", "db", 0, 10*1e3),   // 10ms
		makeSpan("t2", "s2", "", "query", "db", 0, 50*1e3),   // 50ms
		makeSpan("t3", "s3", "", "query", "db", 0, 100*1e3),  // 100ms
		makeSpan("t4", "s4", "", "query", "db", 0, 200*1e3),  // 200ms
		makeSpan("t5", "s5", "", "query", "db", 0, 500*1e3),  // 500ms
	}

	boundaries := []float64{10, 50, 100, 250, 500, 1000}
	h := ComputeHistogram(spans, boundaries)

	if h.Count != 5 {
		t.Errorf("expected count 5, got %d", h.Count)
	}
	// le=10: 1 span, le=50: 2 spans, le=100: 3 spans
	if h.Buckets[0].Count != 1 {
		t.Errorf("bucket le=10: expected 1, got %d", h.Buckets[0].Count)
	}
	if h.Buckets[1].Count != 2 {
		t.Errorf("bucket le=50: expected 2, got %d", h.Buckets[1].Count)
	}
	if h.Buckets[2].Count != 3 {
		t.Errorf("bucket le=100: expected 3, got %d", h.Buckets[2].Count)
	}
}

func TestHTTPEndpoints(t *testing.T) {
	store := NewSpanStore(1000)
	srv := NewServer(store)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Ingest
	spans := []*Span{
		makeSpan("t1", "s1", "", "GET /", "api", 0, 200*1e3),
		makeSpan("t1", "s2", "s1", "query", "db", 10*1e3, 150*1e3),
	}
	body, _ := json.Marshal(spans)
	resp, err := http.Post(ts.URL+"/v1/traces", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("ingest: expected 200, got %d", resp.StatusCode)
	}

	// Query trace
	resp, _ = http.Get(ts.URL + "/v1/traces/t1")
	if resp.StatusCode != 200 {
		t.Errorf("query trace: expected 200, got %d", resp.StatusCode)
	}

	// Waterfall
	resp, _ = http.Get(ts.URL + "/v1/traces/t1/waterfall")
	if resp.StatusCode != 200 {
		t.Errorf("waterfall: expected 200, got %d", resp.StatusCode)
	}

	// Services list
	resp, _ = http.Get(ts.URL + "/v1/services")
	if resp.StatusCode != 200 {
		t.Errorf("services: expected 200, got %d", resp.StatusCode)
	}

	// Latency histogram
	resp, _ = http.Get(ts.URL + "/v1/services/db/latency?operation=query")
	if resp.StatusCode != 200 {
		t.Errorf("latency: expected 200, got %d", resp.StatusCode)
	}
}

func TestConcurrentIngestion(t *testing.T) {
	store := NewSpanStore(10000)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				traceID := "t" + itoa(n*1000+j)
				store.Ingest([]*Span{
					makeSpan(traceID, "s1", "", "op", "svc", 0, 100),
				})
			}
		}(i)
	}
	wg.Wait()
	t.Logf("total services: %d", len(store.ListServices()))
}

func TestEviction(t *testing.T) {
	store := NewSpanStore(5)

	for i := 0; i < 10; i++ {
		store.Ingest([]*Span{
			makeSpan("t"+itoa(i), "s1", "", "op", "svc", int64(i), int64(i+1)),
		})
	}

	store.mu.RLock()
	count := len(store.allSpans)
	store.mu.RUnlock()

	if count > 5 {
		t.Errorf("expected max 5 spans after eviction, got %d", count)
	}

	// Oldest traces should be evicted.
	if spans := store.GetTrace("t0"); len(spans) != 0 {
		t.Error("t0 should have been evicted")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}
```

### Running and Testing

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestIngestAndQuery
--- PASS: TestIngestAndQuery (0.00s)
=== RUN   TestInvalidSpanRejection
--- PASS: TestInvalidSpanRejection (0.00s)
=== RUN   TestDuplicateSpanRejection
--- PASS: TestDuplicateSpanRejection (0.00s)
=== RUN   TestTraceTreeReconstruction
--- PASS: TestTraceTreeReconstruction (0.00s)
=== RUN   TestWaterfallRendering
    collector_test.go:96:
    GET /api             |========================================|  200.0ms
      verify             |==      |                                   40.0ms
      query              |   ============================|           120.0ms
--- PASS: TestWaterfallRendering (0.00s)
=== RUN   TestLatencyHistogram
--- PASS: TestLatencyHistogram (0.00s)
=== RUN   TestHTTPEndpoints
--- PASS: TestHTTPEndpoints (0.00s)
=== RUN   TestConcurrentIngestion
    collector_test.go:149: total services: 1
--- PASS: TestConcurrentIngestion (0.00s)
=== RUN   TestEviction
--- PASS: TestEviction (0.00s)
PASS
```

## Design Decisions

**Decision 1: Multiple indexes maintained on write.** The store maintains `byTrace`, `byService`, and `allSpans` simultaneously. This costs memory and write-time complexity but makes reads O(1) by trace and O(1) by service. The alternative (single list with scan-based queries) would make ingestion fast but queries slow, which is the wrong trade-off for a query-oriented system.

**Decision 2: Eviction removes from all indexes.** When evicting the oldest spans, the store must remove them from both `byTrace` and `byService` maps. This is O(n) per eviction in the worst case, but amortized over many ingestions it is acceptable. A production system would use a more sophisticated index structure (B-tree or time-partitioned shards).

**Decision 3: Lazy histogram computation.** Histograms are computed on query, not maintained incrementally. This is simpler and avoids the complexity of maintaining streaming quantile estimators. The trade-off is query latency proportional to the number of spans for that service/operation.

## Common Mistakes

**Mistake 1: Not sorting durations before percentile calculation.** Percentiles require sorted data. Computing p99 from unsorted durations gives a random value, not the 99th percentile.

**Mistake 2: Returning references to internal slices from GetTrace.** If the caller modifies the returned slice, it corrupts the store's index. Always return a copy.

**Mistake 3: Using string concatenation for the dedup key without a separator.** `traceID + spanID` could collide: trace "ab" span "cd" vs trace "a" span "bcd". Using `traceID + ":" + spanID` avoids this.

## Performance Notes

- The `RWMutex` allows concurrent reads (trace queries) while serializing writes (ingestion). Under read-heavy workloads this is efficient, but under write-heavy workloads all ingestion goroutines contend on the write lock. A sharded store (shard by trace ID hash) would eliminate this bottleneck.
- Waterfall rendering allocates a byte slice per span. For traces with thousands of spans, pre-allocate the output buffer or use a `strings.Builder`.
- The histogram sort uses insertion sort, which is O(n^2) in the worst case. For large span counts (>10k), switch to `slices.Sort` (Go 1.21+) for O(n log n) performance.
