# Solution: Distributed Tracing Propagation

## Architecture Overview

The tracing library has five components: the Span model and lifecycle, the W3C Trace Context propagator, context integration via `context.Context`, HTTP middleware (server and client), and the span collector with trace reconstruction.

```
Context Layer (context.Context with active span)
    |
    +---> Span Lifecycle (Start, SetAttribute, AddEvent, SetStatus, End)
    |
    +---> Propagator (inject traceparent/tracestate into headers, extract from headers)
    |         |
    |    Server Middleware          Client Middleware
    |    (extract -> span)         (span -> inject)
    |
    +---> Collector (receive ended spans, store, query, reconstruct trees)
    |
    +---> Sampler (AlwaysOn, AlwaysOff, Probability)
```

## Go Solution

### Project Setup

```bash
mkdir -p tracing && cd tracing
go mod init tracing
```

### Span Model

```go
// span.go
package tracing

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type SpanStatus int

const (
	StatusUnset SpanStatus = iota
	StatusOK
	StatusError
)

func (s SpanStatus) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusError:
		return "ERROR"
	default:
		return "UNSET"
	}
}

type SpanEvent struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
}

type SpanData struct {
	TraceID      string            `json:"traceId"`
	SpanID       string            `json:"spanId"`
	ParentSpanID string            `json:"parentSpanId,omitempty"`
	Name         string            `json:"name"`
	ServiceName  string            `json:"serviceName"`
	StartTime    time.Time         `json:"startTime"`
	EndTime      time.Time         `json:"endTime,omitempty"`
	Status       SpanStatus        `json:"status"`
	StatusMsg    string            `json:"statusMessage,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Events       []SpanEvent       `json:"events,omitempty"`
	TraceFlags   byte              `json:"traceFlags"`
}

type Span struct {
	mu       sync.Mutex
	data     SpanData
	ended    bool
	exporter SpanExporter
}

type SpanExporter interface {
	ExportSpan(data SpanData)
}

func newSpan(traceID, spanID, parentSpanID, name, service string, flags byte, exporter SpanExporter) *Span {
	return &Span{
		data: SpanData{
			TraceID:      traceID,
			SpanID:       spanID,
			ParentSpanID: parentSpanID,
			Name:         name,
			ServiceName:  service,
			StartTime:    time.Now(),
			Status:       StatusUnset,
			Attributes:   make(map[string]string),
			TraceFlags:   flags,
		},
		exporter: exporter,
	}
}

func (s *Span) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.data.Attributes[key] = value
}

func (s *Span) AddEvent(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.data.Events = append(s.data.Events, SpanEvent{
		Name:      name,
		Timestamp: time.Now(),
	})
}

func (s *Span) SetStatus(code SpanStatus, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.data.Status = code
	s.data.StatusMsg = message
}

func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.ended = true
	s.data.EndTime = time.Now()

	if s.exporter != nil {
		s.exporter.ExportSpan(s.data)
	}
}

func (s *Span) TraceID() string  { return s.data.TraceID }
func (s *Span) SpanID() string   { return s.data.SpanID }
func (s *Span) Flags() byte      { return s.data.TraceFlags }
func (s *Span) IsRecording() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.ended
}

func generateTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateSpanID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

### W3C Trace Context Propagator

```go
// propagator.go
package tracing

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

const (
	traceparentHeader = "traceparent"
	tracestateHeader  = "tracestate"
	traceFlagSampled  = 0x01
	maxTracestateEntries = 32
)

type TraceContext struct {
	TraceID    string
	ParentID   string
	TraceFlags byte
	TraceState string
}

func ParseTraceparent(header string) (TraceContext, error) {
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return TraceContext{}, fmt.Errorf("invalid traceparent: expected 4 parts, got %d", len(parts))
	}

	version := parts[0]
	if version != "00" {
		return TraceContext{}, fmt.Errorf("unsupported traceparent version: %s", version)
	}

	traceID := parts[1]
	if len(traceID) != 32 {
		return TraceContext{}, fmt.Errorf("invalid trace-id length: %d", len(traceID))
	}
	if _, err := hex.DecodeString(traceID); err != nil {
		return TraceContext{}, fmt.Errorf("invalid trace-id hex: %w", err)
	}
	if traceID == "00000000000000000000000000000000" {
		return TraceContext{}, fmt.Errorf("trace-id must not be all zeros")
	}

	parentID := parts[2]
	if len(parentID) != 16 {
		return TraceContext{}, fmt.Errorf("invalid parent-id length: %d", len(parentID))
	}
	if _, err := hex.DecodeString(parentID); err != nil {
		return TraceContext{}, fmt.Errorf("invalid parent-id hex: %w", err)
	}
	if parentID == "0000000000000000" {
		return TraceContext{}, fmt.Errorf("parent-id must not be all zeros")
	}

	flagStr := parts[3]
	if len(flagStr) != 2 {
		return TraceContext{}, fmt.Errorf("invalid trace-flags length: %d", len(flagStr))
	}
	flagBytes, err := hex.DecodeString(flagStr)
	if err != nil {
		return TraceContext{}, fmt.Errorf("invalid trace-flags hex: %w", err)
	}

	return TraceContext{
		TraceID:    traceID,
		ParentID:   parentID,
		TraceFlags: flagBytes[0],
	}, nil
}

func FormatTraceparent(ctx TraceContext) string {
	return fmt.Sprintf("00-%s-%s-%02x", ctx.TraceID, ctx.ParentID, ctx.TraceFlags)
}

func ParseTracestate(header string) []string {
	if header == "" {
		return nil
	}
	entries := strings.Split(header, ",")
	var result []string
	for _, e := range entries {
		trimmed := strings.TrimSpace(e)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func FormatTracestate(entries []string) string {
	return strings.Join(entries, ",")
}

func PrependTracestate(entries []string, key, value string) []string {
	newEntry := key + "=" + value
	result := []string{newEntry}

	for _, e := range entries {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 && parts[0] == key {
			continue // remove existing entry for this key
		}
		result = append(result, e)
	}

	if len(result) > maxTracestateEntries {
		result = result[:maxTracestateEntries]
	}
	return result
}

// Extract reads trace context from HTTP request headers.
func Extract(r *http.Request) (TraceContext, bool) {
	header := r.Header.Get(traceparentHeader)
	if header == "" {
		return TraceContext{}, false
	}
	ctx, err := ParseTraceparent(header)
	if err != nil {
		return TraceContext{}, false
	}
	ctx.TraceState = r.Header.Get(tracestateHeader)
	return ctx, true
}

// Inject writes trace context into HTTP request headers.
func Inject(r *http.Request, ctx TraceContext) {
	r.Header.Set(traceparentHeader, FormatTraceparent(ctx))
	if ctx.TraceState != "" {
		r.Header.Set(tracestateHeader, ctx.TraceState)
	}
}
```

### Context Integration

```go
// context.go
package tracing

import "context"

type spanContextKey struct{}

func ContextWithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, spanContextKey{}, span)
}

func SpanFromContext(ctx context.Context) *Span {
	if span, ok := ctx.Value(spanContextKey{}).(*Span); ok {
		return span
	}
	return nil
}
```

### Sampler

```go
// sampler.go
package tracing

import (
	"encoding/binary"
	"encoding/hex"
	"hash/fnv"
)

type Sampler interface {
	ShouldSample(traceID string) bool
}

type AlwaysOnSampler struct{}
func (s AlwaysOnSampler) ShouldSample(_ string) bool { return true }

type AlwaysOffSampler struct{}
func (s AlwaysOffSampler) ShouldSample(_ string) bool { return false }

// ProbabilitySampler samples a fraction of traces based on trace ID hash.
// The same trace ID always produces the same decision across services.
type ProbabilitySampler struct {
	threshold uint64
}

func NewProbabilitySampler(probability float64) ProbabilitySampler {
	if probability <= 0 {
		return ProbabilitySampler{threshold: 0}
	}
	if probability >= 1 {
		return ProbabilitySampler{threshold: ^uint64(0)}
	}
	return ProbabilitySampler{
		threshold: uint64(probability * float64(^uint64(0))),
	}
}

func (s ProbabilitySampler) ShouldSample(traceID string) bool {
	b, err := hex.DecodeString(traceID)
	if err != nil || len(b) < 8 {
		h := fnv.New64a()
		h.Write([]byte(traceID))
		return h.Sum64() < s.threshold
	}
	val := binary.BigEndian.Uint64(b[8:16])
	return val < s.threshold
}
```

### Tracer (Span Factory)

```go
// tracer.go
package tracing

import "context"

type Tracer struct {
	serviceName string
	sampler     Sampler
	collector   *Collector
}

func NewTracer(serviceName string, sampler Sampler, collector *Collector) *Tracer {
	return &Tracer{
		serviceName: serviceName,
		sampler:     sampler,
		collector:   collector,
	}
}

func (t *Tracer) StartSpan(ctx context.Context, name string) (*Span, context.Context) {
	parent := SpanFromContext(ctx)

	var traceID, parentSpanID string
	var flags byte

	if parent != nil {
		traceID = parent.TraceID()
		parentSpanID = parent.SpanID()
		flags = parent.Flags()
	} else {
		traceID = generateTraceID()
		if t.sampler.ShouldSample(traceID) {
			flags = traceFlagSampled
		}
	}

	// Respect sampling decision: if not sampled, create a no-op span.
	if flags&traceFlagSampled == 0 {
		return newNonRecordingSpan(traceID, flags), ctx
	}

	spanID := generateSpanID()
	span := newSpan(traceID, spanID, parentSpanID, name, t.serviceName, flags, t.collector)
	return span, ContextWithSpan(ctx, span)
}

func (t *Tracer) StartSpanFromTraceContext(ctx context.Context, name string, tc TraceContext) (*Span, context.Context) {
	if tc.TraceFlags&traceFlagSampled == 0 {
		return newNonRecordingSpan(tc.TraceID, tc.TraceFlags), ctx
	}

	spanID := generateSpanID()
	span := newSpan(tc.TraceID, spanID, tc.ParentID, name, t.serviceName, tc.TraceFlags, t.collector)
	return span, ContextWithSpan(ctx, span)
}

type nonRecordingSpan struct {
	traceID string
	flags   byte
}

func newNonRecordingSpan(traceID string, flags byte) *Span {
	return &Span{
		data: SpanData{
			TraceID:    traceID,
			SpanID:     generateSpanID(),
			TraceFlags: flags,
		},
		ended: true, // will not export
	}
}
```

### HTTP Middleware

```go
// middleware.go
package tracing

import "net/http"

// TracingMiddleware creates a server span for each incoming request.
func TracingMiddleware(tracer *Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, found := Extract(r)

			var span *Span
			var ctx = r.Context()

			if found {
				span, ctx = tracer.StartSpanFromTraceContext(ctx, r.Method+" "+r.URL.Path, tc)
			} else {
				span, ctx = tracer.StartSpan(ctx, r.Method+" "+r.URL.Path)
			}

			span.SetAttribute("http.method", r.Method)
			span.SetAttribute("http.url", r.URL.String())

			recorder := &statusRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(recorder, r.WithContext(ctx))

			span.SetAttribute("http.status_code", itoa(recorder.status))
			if recorder.status >= 400 {
				span.SetStatus(StatusError, "HTTP "+itoa(recorder.status))
			} else {
				span.SetStatus(StatusOK, "")
			}
			span.End()
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// TracingTransport injects trace context into outgoing HTTP requests.
type TracingTransport struct {
	Base http.RoundTripper
}

func (t *TracingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	span := SpanFromContext(r.Context())
	if span != nil && span.TraceID() != "" {
		tc := TraceContext{
			TraceID:    span.TraceID(),
			ParentID:   span.SpanID(),
			TraceFlags: span.Flags(),
		}
		Inject(r, tc)
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	if negative {
		result = "-" + result
	}
	return result
}
```

### Collector with Trace Reconstruction

```go
// collector.go
package tracing

import (
	"sync"
	"time"
)

type Collector struct {
	mu     sync.RWMutex
	spans  map[string][]SpanData // traceID -> spans
	ch     chan SpanData
	done   chan struct{}
	wg     sync.WaitGroup
}

func NewCollector(bufSize int) *Collector {
	c := &Collector{
		spans: make(map[string][]SpanData),
		ch:    make(chan SpanData, bufSize),
		done:  make(chan struct{}),
	}
	c.wg.Add(1)
	go c.loop()
	return c
}

func (c *Collector) ExportSpan(data SpanData) {
	select {
	case c.ch <- data:
	default:
		// Buffer full, drop span.
	}
}

func (c *Collector) loop() {
	defer c.wg.Done()
	for {
		select {
		case data, ok := <-c.ch:
			if !ok {
				return
			}
			c.mu.Lock()
			c.spans[data.TraceID] = append(c.spans[data.TraceID], data)
			c.mu.Unlock()
		case <-c.done:
			// Drain remaining.
			for {
				select {
				case data := <-c.ch:
					c.mu.Lock()
					c.spans[data.TraceID] = append(c.spans[data.TraceID], data)
					c.mu.Unlock()
				default:
					return
				}
			}
		}
	}
}

func (c *Collector) Shutdown() {
	close(c.done)
	close(c.ch)
	c.wg.Wait()
}

func (c *Collector) GetTrace(traceID string) []SpanData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]SpanData, len(c.spans[traceID]))
	copy(result, c.spans[traceID])
	return result
}

// --- Trace Tree Reconstruction and Diagnostics ---

type TraceNode struct {
	Span     SpanData     `json:"span"`
	Children []*TraceNode `json:"children,omitempty"`
}

type TraceDiagnostics struct {
	Complete     bool     `json:"complete"`
	TotalSpans   int      `json:"totalSpans"`
	OrphanSpans  []string `json:"orphanSpans,omitempty"`
	MissingSpans []string `json:"missingSpans,omitempty"`
	ClockSkews   []string `json:"clockSkews,omitempty"`
}

func ReconstructTrace(spans []SpanData) (*TraceNode, TraceDiagnostics) {
	if len(spans) == 0 {
		return nil, TraceDiagnostics{}
	}

	nodes := make(map[string]*TraceNode, len(spans))
	spanIDs := make(map[string]bool, len(spans))
	referencedParents := make(map[string]bool)

	for i := range spans {
		nodes[spans[i].SpanID] = &TraceNode{Span: spans[i]}
		spanIDs[spans[i].SpanID] = true
		if spans[i].ParentSpanID != "" {
			referencedParents[spans[i].ParentSpanID] = true
		}
	}

	var root *TraceNode
	var orphans []string

	for i := range spans {
		node := nodes[spans[i].SpanID]
		if spans[i].ParentSpanID == "" {
			root = node
		} else if parent, ok := nodes[spans[i].ParentSpanID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			orphans = append(orphans, spans[i].SpanID)
		}
	}

	// Detect missing spans (referenced as parents but not present).
	var missing []string
	for parentID := range referencedParents {
		if !spanIDs[parentID] {
			missing = append(missing, parentID)
		}
	}

	// Detect clock skew.
	var skews []string
	startTimes := make(map[string]time.Time, len(spans))
	for _, s := range spans {
		startTimes[s.SpanID] = s.StartTime
	}
	for _, s := range spans {
		if s.ParentSpanID == "" {
			continue
		}
		parentStart, ok := startTimes[s.ParentSpanID]
		if ok && s.StartTime.Before(parentStart) {
			skews = append(skews, s.SpanID+" started before parent "+s.ParentSpanID)
		}
	}

	diag := TraceDiagnostics{
		Complete:     len(orphans) == 0 && len(missing) == 0,
		TotalSpans:   len(spans),
		OrphanSpans:  orphans,
		MissingSpans: missing,
		ClockSkews:   skews,
	}

	return root, diag
}
```

### Tests

```go
// tracing_test.go
package tracing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseTraceparentValid(t *testing.T) {
	header := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tc, err := ParseTraceparent(header)
	if err != nil {
		t.Fatal(err)
	}
	if tc.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("wrong traceID: %s", tc.TraceID)
	}
	if tc.ParentID != "00f067aa0ba902b7" {
		t.Errorf("wrong parentID: %s", tc.ParentID)
	}
	if tc.TraceFlags != 0x01 {
		t.Errorf("wrong flags: %02x", tc.TraceFlags)
	}
}

func TestParseTraceparentInvalid(t *testing.T) {
	cases := []string{
		"",
		"00",
		"00-invalid-00f067aa0ba902b7-01",
		"01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
	}
	for _, c := range cases {
		if _, err := ParseTraceparent(c); err == nil {
			t.Errorf("expected error for: %q", c)
		}
	}
}

func TestTraceparentRoundTrip(t *testing.T) {
	original := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tc, _ := ParseTraceparent(original)
	formatted := FormatTraceparent(tc)
	if formatted != original {
		t.Errorf("round-trip failed: %s != %s", formatted, original)
	}
}

func TestTracestatePreservation(t *testing.T) {
	entries := ParseTracestate("congo=t61rcWkgMzE,rojo=00f067aa0ba902b7")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	entries = PrependTracestate(entries, "myvendor", "abc123")
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after prepend, got %d", len(entries))
	}
	if entries[0] != "myvendor=abc123" {
		t.Errorf("expected myvendor first, got %s", entries[0])
	}
}

func TestTracestateLimitEnforcement(t *testing.T) {
	entries := make([]string, 32)
	for i := range entries {
		entries[i] = "key" + itoa(i) + "=val"
	}
	entries = PrependTracestate(entries, "new", "val")
	if len(entries) > 32 {
		t.Errorf("tracestate exceeds 32 entries: %d", len(entries))
	}
}

func TestProbabilitySamplerConsistency(t *testing.T) {
	sampler := NewProbabilitySampler(0.5)
	traceID := "4bf92f3577b34da6a3ce929d0e0e4736"

	first := sampler.ShouldSample(traceID)
	for i := 0; i < 100; i++ {
		if sampler.ShouldSample(traceID) != first {
			t.Fatal("sampler gave inconsistent result for same traceID")
		}
	}
}

func TestSpanLifecycle(t *testing.T) {
	collector := NewCollector(100)
	defer collector.Shutdown()

	tracer := NewTracer("test-service", AlwaysOnSampler{}, collector)

	span, ctx := tracer.StartSpan(context.Background(), "test-op")
	span.SetAttribute("key", "value")
	span.AddEvent("checkpoint")
	span.SetStatus(StatusOK, "")
	span.End()

	// Modifications after End should be ignored.
	span.SetAttribute("late", "ignored")

	_ = ctx
	time.Sleep(10 * time.Millisecond) // let collector process

	spans := collector.GetTrace(span.TraceID())
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Attributes["key"] != "value" {
		t.Error("missing attribute")
	}
	if _, exists := spans[0].Attributes["late"]; exists {
		t.Error("attribute set after End should be ignored")
	}
}

func TestContextPropagation(t *testing.T) {
	collector := NewCollector(100)
	defer collector.Shutdown()

	tracer := NewTracer("svc", AlwaysOnSampler{}, collector)

	parent, ctx := tracer.StartSpan(context.Background(), "parent")
	child, _ := tracer.StartSpan(ctx, "child")

	if child.data.ParentSpanID != parent.SpanID() {
		t.Errorf("child should link to parent: got parent=%s, expected=%s",
			child.data.ParentSpanID, parent.SpanID())
	}
	if child.TraceID() != parent.TraceID() {
		t.Error("child should share parent's trace ID")
	}

	child.End()
	parent.End()
}

func TestSamplingRespected(t *testing.T) {
	collector := NewCollector(100)
	defer collector.Shutdown()

	tracer := NewTracer("svc", AlwaysOffSampler{}, collector)
	span, _ := tracer.StartSpan(context.Background(), "unsampled")
	span.End()

	time.Sleep(10 * time.Millisecond)
	if len(collector.GetTrace(span.TraceID())) != 0 {
		t.Error("unsampled span should not be exported")
	}
}

func TestServerMiddleware(t *testing.T) {
	collector := NewCollector(100)
	defer collector.Shutdown()
	tracer := NewTracer("api", AlwaysOnSampler{}, collector)

	handler := TracingMiddleware(tracer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := SpanFromContext(r.Context())
		if span == nil {
			t.Error("no span in context")
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(10 * time.Millisecond)
	spans := collector.GetTrace("4bf92f3577b34da6a3ce929d0e0e4736")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].ParentSpanID != "00f067aa0ba902b7" {
		t.Errorf("server span should have incoming parent: %s", spans[0].ParentSpanID)
	}
}

func TestServerMiddlewareNoIncomingContext(t *testing.T) {
	collector := NewCollector(100)
	defer collector.Shutdown()
	tracer := NewTracer("api", AlwaysOnSampler{}, collector)

	handler := TracingMiddleware(tracer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	time.Sleep(10 * time.Millisecond)
	// Should have created a new trace.
	found := false
	collector.mu.RLock()
	for _, spans := range collector.spans {
		if len(spans) > 0 {
			found = true
		}
	}
	collector.mu.RUnlock()
	if !found {
		t.Error("middleware should start a new trace when no traceparent present")
	}
}

func TestClientMiddleware(t *testing.T) {
	collector := NewCollector(100)
	defer collector.Shutdown()
	tracer := NewTracer("client-svc", AlwaysOnSampler{}, collector)

	var capturedHeader string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("traceparent")
		w.WriteHeader(200)
	}))
	defer backend.Close()

	span, ctx := tracer.StartSpan(context.Background(), "client-call")

	client := &http.Client{Transport: &TracingTransport{Base: http.DefaultTransport}}
	req, _ := http.NewRequestWithContext(ctx, "GET", backend.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	span.End()

	if capturedHeader == "" {
		t.Fatal("traceparent header not injected")
	}
	tc, err := ParseTraceparent(capturedHeader)
	if err != nil {
		t.Fatal(err)
	}
	if tc.TraceID != span.TraceID() {
		t.Errorf("trace ID mismatch: %s != %s", tc.TraceID, span.TraceID())
	}
}

func TestMultiServiceChain(t *testing.T) {
	collector := NewCollector(100)
	defer collector.Shutdown()

	tracerA := NewTracer("service-a", AlwaysOnSampler{}, collector)
	tracerB := NewTracer("service-b", AlwaysOnSampler{}, collector)
	tracerC := NewTracer("service-c", AlwaysOnSampler{}, collector)

	// Simulate: A -> B -> C
	spanA, ctxA := tracerA.StartSpan(context.Background(), "handle-request")

	// A calls B: extract context and inject into request.
	tcForB := TraceContext{
		TraceID:    spanA.TraceID(),
		ParentID:   spanA.SpanID(),
		TraceFlags: spanA.Flags(),
	}
	spanB, ctxB := tracerB.StartSpanFromTraceContext(ctxA, "process", tcForB)

	// B calls C.
	tcForC := TraceContext{
		TraceID:    spanB.TraceID(),
		ParentID:   spanB.SpanID(),
		TraceFlags: spanB.Flags(),
	}
	spanC, _ := tracerC.StartSpanFromTraceContext(ctxB, "store", tcForC)

	spanC.End()
	spanB.End()
	spanA.End()

	time.Sleep(20 * time.Millisecond)

	spans := collector.GetTrace(spanA.TraceID())
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans in trace, got %d", len(spans))
	}

	root, diag := ReconstructTrace(spans)
	if !diag.Complete {
		t.Errorf("trace should be complete: orphans=%v, missing=%v", diag.OrphanSpans, diag.MissingSpans)
	}
	if root == nil {
		t.Fatal("expected non-nil root")
	}
}

func TestBrokenTraceDetection(t *testing.T) {
	spans := []SpanData{
		{SpanID: "root", ParentSpanID: "", TraceID: "t1", StartTime: time.Now()},
		{SpanID: "child", ParentSpanID: "missing-parent", TraceID: "t1", StartTime: time.Now()},
	}

	_, diag := ReconstructTrace(spans)
	if diag.Complete {
		t.Error("trace with missing parent should not be complete")
	}
	if len(diag.OrphanSpans) != 1 || diag.OrphanSpans[0] != "child" {
		t.Errorf("expected orphan 'child', got %v", diag.OrphanSpans)
	}
	if len(diag.MissingSpans) != 1 || diag.MissingSpans[0] != "missing-parent" {
		t.Errorf("expected missing 'missing-parent', got %v", diag.MissingSpans)
	}
}

func TestClockSkewDetection(t *testing.T) {
	now := time.Now()
	spans := []SpanData{
		{SpanID: "parent", ParentSpanID: "", TraceID: "t1", StartTime: now.Add(10 * time.Millisecond)},
		{SpanID: "child", ParentSpanID: "parent", TraceID: "t1", StartTime: now}, // before parent
	}

	_, diag := ReconstructTrace(spans)
	if len(diag.ClockSkews) != 1 {
		t.Errorf("expected 1 clock skew, got %d", len(diag.ClockSkews))
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestParseTraceparentValid
--- PASS: TestParseTraceparentValid (0.00s)
=== RUN   TestParseTraceparentInvalid
--- PASS: TestParseTraceparentInvalid (0.00s)
=== RUN   TestTraceparentRoundTrip
--- PASS: TestTraceparentRoundTrip (0.00s)
=== RUN   TestTracestatePreservation
--- PASS: TestTracestatePreservation (0.00s)
=== RUN   TestTracestateLimitEnforcement
--- PASS: TestTracestateLimitEnforcement (0.00s)
=== RUN   TestProbabilitySamplerConsistency
--- PASS: TestProbabilitySamplerConsistency (0.00s)
=== RUN   TestSpanLifecycle
--- PASS: TestSpanLifecycle (0.01s)
=== RUN   TestContextPropagation
--- PASS: TestContextPropagation (0.00s)
=== RUN   TestSamplingRespected
--- PASS: TestSamplingRespected (0.01s)
=== RUN   TestServerMiddleware
--- PASS: TestServerMiddleware (0.01s)
=== RUN   TestServerMiddlewareNoIncomingContext
--- PASS: TestServerMiddlewareNoIncomingContext (0.01s)
=== RUN   TestClientMiddleware
--- PASS: TestClientMiddleware (0.01s)
=== RUN   TestMultiServiceChain
--- PASS: TestMultiServiceChain (0.02s)
=== RUN   TestBrokenTraceDetection
--- PASS: TestBrokenTraceDetection (0.00s)
=== RUN   TestClockSkewDetection
--- PASS: TestClockSkewDetection (0.00s)
PASS
```

## Design Decisions

**Decision 1: Span immutability after End().** Once `End()` is called, all mutations (SetAttribute, AddEvent, SetStatus) are silently ignored. This is consistent with the OpenTelemetry specification and prevents races between the exporter reading span data and late mutations from application code. The alternative (returning errors on post-end mutations) adds noise without value.

**Decision 2: Non-recording spans for unsampled traces.** When the sampling decision is "do not sample", a non-recording span is returned that has the correct trace ID and flags but never exports. This allows context propagation to continue even for unsampled traces, which is essential for the sampling decision to be respected by downstream services.

**Decision 3: Hash-based probability sampler using trace ID bytes.** The sampler reads the lower 8 bytes of the trace ID as a uint64 and compares against a threshold. Since trace IDs are random, this produces uniform sampling. Critically, the same trace ID always produces the same sampling decision, which means every service in the chain agrees on whether to sample without coordination.

**Decision 4: Collector as a separate goroutine with channel input.** Spans are sent to the collector via a buffered channel, decoupling span creation (hot path) from storage (cold path). If the buffer is full, spans are dropped rather than blocking the application. This is the correct trade-off: tracing should never affect application latency.

## Common Mistakes

**Mistake 1: Generating span IDs with `math/rand`.** Math/rand is not cryptographically random and can produce duplicate IDs under high concurrency. Always use `crypto/rand` for trace and span IDs.

**Mistake 2: Failing to propagate trace flags.** If a service creates child spans without copying the parent's trace flags, downstream services lose the sampling decision. Every child span must inherit its parent's trace flags.

**Mistake 3: Propagating context without creating a new span ID.** If the client middleware sets the outgoing `traceparent` with the same span ID as the current span (instead of creating a child), the receiving service will create a sibling rather than a child, breaking the trace tree.

**Mistake 4: Blocking on the collector channel in the middleware hot path.** Using a blocking send on the collector channel means a slow collector (disk I/O, network latency) directly increases HTTP request latency. Always use a non-blocking send with a drop fallback.

## Performance Notes

- The span mutex is per-span, not global. Concurrent spans in different goroutines do not contend with each other. The only shared lock is in the collector's span store.
- `crypto/rand.Read` is backed by the kernel's CSPRNG. On Linux this is fast (100ns per call), but on some systems it may block if entropy is exhausted. For extremely high throughput (millions of spans/sec), consider pre-generating a pool of random IDs.
- The `ParseTraceparent` function allocates minimally: one `strings.Split` and one `hex.DecodeString`. For zero-allocation parsing, use byte-level indexing directly on the header bytes.
- The probability sampler's `hex.DecodeString` call on every sample decision is avoidable if you parse the trace ID bytes once at span creation time and cache the result.
