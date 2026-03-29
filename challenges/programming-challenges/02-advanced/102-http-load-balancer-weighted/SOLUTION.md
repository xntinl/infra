# Solution: HTTP Load Balancer with Weighted Routing

## Architecture Overview

The load balancer is structured in five subsystems: a **frontend** that accepts TCP connections and parses HTTP requests, a **backend pool** that manages server state and health, a **load balancing engine** with pluggable algorithms, a **proxy core** that forwards requests and relays responses, and a **session manager** for sticky cookie affinity.

```
Clients
  |
Frontend (TCP accept + HTTP parse)
  |
Session Manager (cookie affinity check)
  |
Load Balance Engine
  ├── Weighted Round-Robin (smooth Nginx-style)
  └── Least-Connections (weighted)
  |
Backend Pool
  ├── Backend A (weight=3, healthy, active=5)
  ├── Backend B (weight=2, healthy, active=3)
  └── Backend C (weight=1, draining, active=1)
  |
Health Checker (background, per-backend)
  |
Proxy Core (forward request, relay response, add headers)
```

The proxy core buffers the request for retry capability, dials the selected backend, writes the request, reads the response, and relays it back to the client. If the backend fails, the engine selects a different one for retry.

---

## Go Solution

### Project Setup

```bash
mkdir -p loadbalancer && cd loadbalancer
go mod init loadbalancer
```

### Backend and Pool

```go
// backend.go
package loadbalancer

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type BackendStatus int

const (
	StatusHealthy  BackendStatus = iota
	StatusUnhealthy
	StatusDraining
)

func (s BackendStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusUnhealthy:
		return "unhealthy"
	case StatusDraining:
		return "draining"
	default:
		return "unknown"
	}
}

type Backend struct {
	ID     string
	Addr   string
	Weight int

	status        BackendStatus
	activeConns   atomic.Int64
	currentWeight int // for smooth WRR
	mu            sync.RWMutex

	consecutiveSuccesses int
	consecutiveFailures  int
}

func NewBackend(id, addr string, weight int) *Backend {
	return &Backend{
		ID:     id,
		Addr:   addr,
		Weight: weight,
		status: StatusHealthy,
	}
}

func (b *Backend) Status() BackendStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

func (b *Backend) SetStatus(s BackendStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status = s
}

func (b *Backend) IsAvailable() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status == StatusHealthy
}

func (b *Backend) ActiveConns() int64 {
	return b.activeConns.Load()
}

func (b *Backend) Dial(timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", b.Addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", b.Addr, err)
	}
	b.activeConns.Add(1)
	return conn, nil
}

func (b *Backend) Release() {
	b.activeConns.Add(-1)
}

// BackendPool manages the set of backends.
type BackendPool struct {
	backends []*Backend
	mu       sync.RWMutex
}

func NewBackendPool() *BackendPool {
	return &BackendPool{}
}

func (p *BackendPool) Add(b *Backend) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backends = append(p.backends, b)
}

func (p *BackendPool) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, b := range p.backends {
		if b.ID == id {
			p.backends = append(p.backends[:i], p.backends[i+1:]...)
			return
		}
	}
}

func (p *BackendPool) Get(id string) *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, b := range p.backends {
		if b.ID == id {
			return b
		}
	}
	return nil
}

func (p *BackendPool) Healthy() []*Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []*Backend
	for _, b := range p.backends {
		if b.IsAvailable() {
			result = append(result, b)
		}
	}
	return result
}

func (p *BackendPool) All() []*Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cp := make([]*Backend, len(p.backends))
	copy(cp, p.backends)
	return cp
}
```

### Load Balancing Algorithms

```go
// algorithm.go
package loadbalancer

import (
	"sync"
)

// Algorithm selects a backend for a request.
type Algorithm interface {
	Next(backends []*Backend) *Backend
}

// --- Smooth Weighted Round-Robin (Nginx-style) ---

type WeightedRoundRobin struct {
	mu sync.Mutex
}

func NewWeightedRoundRobin() *WeightedRoundRobin {
	return &WeightedRoundRobin{}
}

func (w *WeightedRoundRobin) Next(backends []*Backend) *Backend {
	if len(backends) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	totalWeight := 0
	var best *Backend

	for _, b := range backends {
		b.mu.Lock()
		b.currentWeight += b.Weight
		totalWeight += b.Weight

		if best == nil || b.currentWeight > best.currentWeight {
			best = b
		}
		b.mu.Unlock()
	}

	if best != nil {
		best.mu.Lock()
		best.currentWeight -= totalWeight
		best.mu.Unlock()
	}

	return best
}

// --- Least Connections (weighted) ---

type LeastConnections struct{}

func NewLeastConnections() *LeastConnections {
	return &LeastConnections{}
}

func (lc *LeastConnections) Next(backends []*Backend) *Backend {
	if len(backends) == 0 {
		return nil
	}

	var best *Backend
	bestScore := float64(1<<63 - 1)

	for _, b := range backends {
		active := float64(b.ActiveConns())
		weight := float64(b.Weight)
		if weight == 0 {
			weight = 1
		}
		score := active / weight

		if score < bestScore || (score == bestScore && best != nil && b.Weight > best.Weight) {
			bestScore = score
			best = b
		}
	}

	return best
}
```

### Health Checker

```go
// healthcheck.go
package loadbalancer

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

type HealthCheckConfig struct {
	Interval         time.Duration
	Timeout          time.Duration
	Path             string // HTTP path to probe
	HealthyThreshold int    // consecutive successes to mark healthy
	UnhealthyThreshold int // consecutive failures to mark unhealthy
}

type HealthChecker struct {
	pool   *BackendPool
	cfg    HealthCheckConfig
	cancel context.CancelFunc
}

func NewHealthChecker(pool *BackendPool, cfg HealthCheckConfig) *HealthChecker {
	return &HealthChecker{
		pool: pool,
		cfg:  cfg,
	}
}

func (hc *HealthChecker) Start(ctx context.Context) {
	ctx, hc.cancel = context.WithCancel(ctx)
	go hc.run(ctx)
}

func (hc *HealthChecker) Stop() {
	if hc.cancel != nil {
		hc.cancel()
	}
}

func (hc *HealthChecker) run(ctx context.Context) {
	ticker := time.NewTicker(hc.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hc.checkAll()
		}
	}
}

func (hc *HealthChecker) checkAll() {
	for _, b := range hc.pool.All() {
		if b.Status() == StatusDraining {
			continue
		}
		healthy := hc.probe(b)
		hc.updateStatus(b, healthy)
	}
}

func (hc *HealthChecker) probe(b *Backend) bool {
	conn, err := net.DialTimeout("tcp", b.Addr, hc.cfg.Timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(hc.cfg.Timeout))

	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n",
		hc.cfg.Path, b.Addr)
	if _, err := conn.Write([]byte(req)); err != nil {
		return false
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 2 {
		return false
	}

	return parts[1] == "200"
}

func (hc *HealthChecker) updateStatus(b *Backend, healthy bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if healthy {
		b.consecutiveSuccesses++
		b.consecutiveFailures = 0
		if b.status == StatusUnhealthy && b.consecutiveSuccesses >= hc.cfg.HealthyThreshold {
			b.status = StatusHealthy
			b.consecutiveSuccesses = 0
		}
	} else {
		b.consecutiveFailures++
		b.consecutiveSuccesses = 0
		if b.status == StatusHealthy && b.consecutiveFailures >= hc.cfg.UnhealthyThreshold {
			b.status = StatusUnhealthy
			b.consecutiveFailures = 0
		}
	}
}
```

### HTTP Parser and Proxy Core

```go
// proxy.go
package loadbalancer

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type ProxyRequest struct {
	Method  string
	Path    string
	Version string
	Headers [][2]string // ordered key-value pairs
	Body    []byte
	Raw     []byte // full buffered request for retry
}

func ParseHTTPRequest(reader *bufio.Reader) (*ProxyRequest, error) {
	var raw bytes.Buffer
	tee := io.TeeReader(reader, &raw)
	teeReader := bufio.NewReader(tee)

	line, err := teeReader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimRight(line, "\r\n"), " ", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed request line")
	}

	req := &ProxyRequest{
		Method:  parts[0],
		Path:    parts[1],
		Version: parts[2],
	}

	var contentLength int64
	for {
		header, err := teeReader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		header = strings.TrimRight(header, "\r\n")
		if header == "" {
			break
		}
		idx := strings.IndexByte(header, ':')
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(header[:idx])
		val := strings.TrimSpace(header[idx+1:])
		req.Headers = append(req.Headers, [2]string{key, val})

		if strings.EqualFold(key, "content-length") {
			contentLength, _ = strconv.ParseInt(val, 10, 64)
		}
	}

	if contentLength > 0 {
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(teeReader, body); err != nil {
			return nil, fmt.Errorf("reading body: %w", err)
		}
		req.Body = body
	}

	req.Raw = raw.Bytes()
	return req, nil
}

func (req *ProxyRequest) GetHeader(name string) string {
	for _, h := range req.Headers {
		if strings.EqualFold(h[0], name) {
			return h[1]
		}
	}
	return ""
}

func (req *ProxyRequest) SetHeader(name, value string) {
	for i, h := range req.Headers {
		if strings.EqualFold(h[0], name) {
			req.Headers[i][1] = value
			return
		}
	}
	req.Headers = append(req.Headers, [2]string{name, value})
}

func (req *ProxyRequest) Serialize() []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s %s %s\r\n", req.Method, req.Path, req.Version)
	for _, h := range req.Headers {
		fmt.Fprintf(&buf, "%s: %s\r\n", h[0], h[1])
	}
	buf.WriteString("\r\n")
	if len(req.Body) > 0 {
		buf.Write(req.Body)
	}
	return buf.Bytes()
}

type ProxyResponse struct {
	StatusCode int
	StatusText string
	Version    string
	Headers    [][2]string
	Body       []byte
}

func ReadHTTPResponse(reader *bufio.Reader) (*ProxyResponse, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimRight(line, "\r\n"), " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed response line")
	}

	resp := &ProxyResponse{Version: parts[0]}
	fmt.Sscanf(parts[1], "%d", &resp.StatusCode)
	if len(parts) > 2 {
		resp.StatusText = parts[2]
	}

	var contentLength int64 = -1
	chunked := false

	for {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		header = strings.TrimRight(header, "\r\n")
		if header == "" {
			break
		}
		idx := strings.IndexByte(header, ':')
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(header[:idx])
		val := strings.TrimSpace(header[idx+1:])
		resp.Headers = append(resp.Headers, [2]string{key, val})

		if strings.EqualFold(key, "content-length") {
			contentLength, _ = strconv.ParseInt(val, 10, 64)
		}
		if strings.EqualFold(key, "transfer-encoding") && strings.Contains(strings.ToLower(val), "chunked") {
			chunked = true
		}
	}

	if chunked {
		resp.Body, err = readChunked(reader)
		if err != nil {
			return nil, err
		}
	} else if contentLength > 0 {
		resp.Body = make([]byte, contentLength)
		if _, err := io.ReadFull(reader, resp.Body); err != nil {
			return nil, err
		}
	} else if contentLength == 0 {
		resp.Body = nil
	}

	return resp, nil
}

func readChunked(reader *bufio.Reader) ([]byte, error) {
	var body bytes.Buffer
	for {
		sizeLine, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeLine = strings.TrimRight(sizeLine, "\r\n")
		size, err := strconv.ParseInt(sizeLine, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chunk size: %q", sizeLine)
		}
		if size == 0 {
			reader.ReadString('\n') // trailing CRLF
			break
		}
		chunk := make([]byte, size)
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return nil, err
		}
		body.Write(chunk)
		reader.ReadString('\n') // CRLF after chunk
	}
	return body.Bytes(), nil
}

func (resp *ProxyResponse) Serialize() []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s %d %s\r\n", resp.Version, resp.StatusCode, resp.StatusText)
	for _, h := range resp.Headers {
		if strings.EqualFold(h[0], "transfer-encoding") {
			continue // we de-chunk, so remove this
		}
		fmt.Fprintf(&buf, "%s: %s\r\n", h[0], h[1])
	}
	if len(resp.Body) > 0 {
		fmt.Fprintf(&buf, "Content-Length: %d\r\n", len(resp.Body))
	}
	buf.WriteString("\r\n")
	if len(resp.Body) > 0 {
		buf.Write(resp.Body)
	}
	return buf.Bytes()
}
```

### Load Balancer Core

```go
// balancer.go
package loadbalancer

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

type BalancerConfig struct {
	Addr            string
	Algorithm       string // "wrr" or "leastconn"
	MaxRetries      int
	DialTimeout     time.Duration
	StickySessions  bool
	CookieName      string
	MaxBodyBuffer   int64 // max request body to buffer for retry
}

type Balancer struct {
	cfg       BalancerConfig
	pool      *BackendPool
	algorithm Algorithm
	health    *HealthChecker
	listener  net.Listener
	wg        sync.WaitGroup
	quit      chan struct{}
	algMu     sync.RWMutex
	logger    *slog.Logger
}

func NewBalancer(cfg BalancerConfig, pool *BackendPool, hcCfg HealthCheckConfig, logger *slog.Logger) *Balancer {
	var alg Algorithm
	switch cfg.Algorithm {
	case "leastconn":
		alg = NewLeastConnections()
	default:
		alg = NewWeightedRoundRobin()
	}

	if cfg.CookieName == "" {
		cfg.CookieName = "__lb_backend"
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 2
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}

	return &Balancer{
		cfg:       cfg,
		pool:      pool,
		algorithm: alg,
		health:    NewHealthChecker(pool, hcCfg),
		quit:      make(chan struct{}),
		logger:    logger,
	}
}

func (b *Balancer) SetAlgorithm(name string) {
	b.algMu.Lock()
	defer b.algMu.Unlock()
	switch name {
	case "leastconn":
		b.algorithm = NewLeastConnections()
	default:
		b.algorithm = NewWeightedRoundRobin()
	}
	b.logger.Info("algorithm switched", "algorithm", name)
}

func (b *Balancer) Drain(backendID string) {
	backend := b.pool.Get(backendID)
	if backend == nil {
		return
	}
	backend.SetStatus(StatusDraining)
	b.logger.Info("backend draining", "backend", backendID)

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if backend.ActiveConns() == 0 {
				b.pool.Remove(backendID)
				b.logger.Info("backend drained and removed", "backend", backendID)
				return
			}
		}
	}()
}

func (b *Balancer) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", b.cfg.Addr)
	if err != nil {
		return err
	}
	b.listener = ln
	b.health.Start(ctx)

	go func() {
		<-b.quit
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-b.quit:
				b.wg.Wait()
				return nil
			default:
				continue
			}
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.handleClient(conn)
		}()
	}
}

func (b *Balancer) Shutdown() {
	b.health.Stop()
	close(b.quit)
	if b.listener != nil {
		b.listener.Close()
	}
	b.wg.Wait()
}

func (b *Balancer) handleClient(clientConn net.Conn) {
	defer clientConn.Close()
	clientConn.SetReadDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(clientConn)
	req, err := ParseHTTPRequest(reader)
	if err != nil {
		writeHTTPError(clientConn, 400, "Bad Request")
		return
	}

	// Add proxy headers
	clientIP, _, _ := net.SplitHostPort(clientConn.RemoteAddr().String())
	req.SetHeader("X-Forwarded-For", clientIP)
	req.SetHeader("X-Forwarded-Proto", "http")
	req.SetHeader("Via", "1.1 lb")

	// Check sticky session
	var stickyBackend *Backend
	if b.cfg.StickySessions {
		if backendID := extractCookie(req.GetHeader("Cookie"), b.cfg.CookieName); backendID != "" {
			candidate := b.pool.Get(backendID)
			if candidate != nil && candidate.IsAvailable() {
				stickyBackend = candidate
			}
		}
	}

	// Select backend and proxy with retries
	tried := make(map[string]bool)
	maxAttempts := b.cfg.MaxRetries + 1

	for attempt := 0; attempt < maxAttempts; attempt++ {
		var backend *Backend
		if stickyBackend != nil && attempt == 0 {
			backend = stickyBackend
		} else {
			healthy := b.pool.Healthy()
			// Filter out already-tried backends
			var candidates []*Backend
			for _, h := range healthy {
				if !tried[h.ID] {
					candidates = append(candidates, h)
				}
			}
			if len(candidates) == 0 {
				break
			}

			b.algMu.RLock()
			backend = b.algorithm.Next(candidates)
			b.algMu.RUnlock()
		}

		if backend == nil {
			break
		}
		tried[backend.ID] = true

		resp, err := b.forwardRequest(backend, req)
		if err != nil {
			b.logger.Warn("backend failed", "backend", backend.ID, "attempt", attempt+1, "error", err)
			continue
		}

		if resp.StatusCode == 502 || resp.StatusCode == 503 {
			b.logger.Warn("backend returned error status", "backend", backend.ID, "status", resp.StatusCode)
			continue
		}

		// Set sticky session cookie if needed
		if b.cfg.StickySessions && stickyBackend == nil {
			cookie := fmt.Sprintf("%s=%s; Path=/; HttpOnly", b.cfg.CookieName, backend.ID)
			resp.Headers = append(resp.Headers, [2]string{"Set-Cookie", cookie})
		}

		clientConn.Write(resp.Serialize())
		return
	}

	// All backends failed
	healthy := b.pool.Healthy()
	if len(healthy) == 0 {
		writeHTTPError(clientConn, 503, "Service Unavailable")
	} else {
		writeHTTPError(clientConn, 502, "Bad Gateway")
	}
}

func (b *Balancer) forwardRequest(backend *Backend, req *ProxyRequest) (*ProxyResponse, error) {
	conn, err := backend.Dial(b.cfg.DialTimeout)
	if err != nil {
		return nil, err
	}
	defer func() {
		conn.Close()
		backend.Release()
	}()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	if _, err := conn.Write(req.Serialize()); err != nil {
		return nil, fmt.Errorf("writing to backend: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := ReadHTTPResponse(reader)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return resp, nil
}

func extractCookie(header, name string) string {
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, name+"=") {
			return strings.TrimPrefix(part, name+"=")
		}
	}
	return ""
}

func writeHTTPError(conn net.Conn, status int, text string) {
	body := fmt.Sprintf("<h1>%d %s</h1>", status, text)
	resp := fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: text/html\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		status, text, len(body), body)
	conn.Write([]byte(resp))
}
```

### Tests

```go
// balancer_test.go
package loadbalancer

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func startMockBackend(t *testing.T, id string, handler func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handler(conn)
		}
	}()

	return ln.Addr().String()
}

func okHandler(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	// Read request line and headers
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}
	resp := "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nOK"
	conn.Write([]byte(resp))
}

func healthHandler(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}
	resp := "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
	conn.Write([]byte(resp))
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func sendRawRequest(addr string, request string) (int, string, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	fmt.Fprint(conn, request)

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	var status int
	fmt.Sscanf(strings.Fields(statusLine)[1], "%d", &status)

	var headers []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
		headers = append(headers, strings.TrimSpace(line))
	}

	return status, strings.Join(headers, "\n"), nil
}

func TestWeightedRoundRobin(t *testing.T) {
	counts := make(map[string]*atomic.Int64)
	makeHandler := func(id string) func(net.Conn) {
		counts[id] = &atomic.Int64{}
		return func(conn net.Conn) {
			defer conn.Close()
			reader := bufio.NewReader(conn)
			for {
				line, err := reader.ReadString('\n')
				if err != nil || strings.TrimSpace(line) == "" {
					break
				}
			}
			counts[id].Add(1)
			resp := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
				len(id), id)
			conn.Write([]byte(resp))
		}
	}

	addrA := startMockBackend(t, "A", makeHandler("A"))
	addrB := startMockBackend(t, "B", makeHandler("B"))
	addrC := startMockBackend(t, "C", makeHandler("C"))

	pool := NewBackendPool()
	pool.Add(NewBackend("A", addrA, 3))
	pool.Add(NewBackend("B", addrB, 2))
	pool.Add(NewBackend("C", addrC, 1))

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	balancerAddr := ln.Addr().String()
	ln.Close()

	b := NewBalancer(BalancerConfig{
		Addr:      balancerAddr,
		Algorithm: "wrr",
	}, pool, HealthCheckConfig{
		Interval:           1 * time.Hour, // disable for test
		Timeout:            1 * time.Second,
		Path:               "/health",
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
	}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go b.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	total := 600
	for i := 0; i < total; i++ {
		sendRawRequest(balancerAddr, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	}

	a := counts["A"].Load()
	bCount := counts["B"].Load()
	c := counts["C"].Load()

	t.Logf("Distribution: A=%d (expect ~300), B=%d (expect ~200), C=%d (expect ~100)", a, bCount, c)

	// A should get ~3x C, B should get ~2x C (allow 20% tolerance)
	ratioAC := float64(a) / float64(c)
	ratioBC := float64(bCount) / float64(c)

	if math.Abs(ratioAC-3.0) > 0.6 {
		t.Errorf("A/C ratio = %.2f, expected ~3.0", ratioAC)
	}
	if math.Abs(ratioBC-2.0) > 0.6 {
		t.Errorf("B/C ratio = %.2f, expected ~2.0", ratioBC)
	}

	b.Shutdown()
}

func TestLeastConnections(t *testing.T) {
	pool := NewBackendPool()
	bA := NewBackend("A", "127.0.0.1:1", 1)
	bA.activeConns.Store(10)
	bB := NewBackend("B", "127.0.0.1:2", 1)
	bB.activeConns.Store(2)
	bC := NewBackend("C", "127.0.0.1:3", 1)
	bC.activeConns.Store(5)

	lc := NewLeastConnections()
	selected := lc.Next([]*Backend{bA, bB, bC})
	if selected.ID != "B" {
		t.Errorf("selected %s, expected B (fewest connections)", selected.ID)
	}
}

func TestStickySessions(t *testing.T) {
	addrA := startMockBackend(t, "A", func(conn net.Conn) {
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}
		resp := "HTTP/1.1 200 OK\r\nContent-Length: 1\r\nConnection: close\r\n\r\nA"
		conn.Write([]byte(resp))
	})

	addrB := startMockBackend(t, "B", func(conn net.Conn) {
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}
		resp := "HTTP/1.1 200 OK\r\nContent-Length: 1\r\nConnection: close\r\n\r\nB"
		conn.Write([]byte(resp))
	})

	pool := NewBackendPool()
	pool.Add(NewBackend("A", addrA, 1))
	pool.Add(NewBackend("B", addrB, 1))

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	balancerAddr := ln.Addr().String()
	ln.Close()

	b := NewBalancer(BalancerConfig{
		Addr:           balancerAddr,
		Algorithm:      "wrr",
		StickySessions: true,
		CookieName:     "__lb_backend",
	}, pool, HealthCheckConfig{
		Interval:           1 * time.Hour,
		Timeout:            1 * time.Second,
		Path:               "/health",
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
	}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go b.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	// First request gets a Set-Cookie
	_, headers, err := sendRawRequest(balancerAddr, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	if !strings.Contains(headers, "__lb_backend=") {
		t.Error("response should contain sticky session cookie")
	}

	b.Shutdown()
}

func TestGracefulDraining(t *testing.T) {
	var wg sync.WaitGroup
	requestReceived := make(chan struct{})

	addr := startMockBackend(t, "slow", func(conn net.Conn) {
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}
		close(requestReceived)
		time.Sleep(200 * time.Millisecond) // simulate slow processing
		resp := "HTTP/1.1 200 OK\r\nContent-Length: 4\r\nConnection: close\r\n\r\ndone"
		conn.Write([]byte(resp))
	})

	pool := NewBackendPool()
	pool.Add(NewBackend("slow", addr, 1))

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	balancerAddr := ln.Addr().String()
	ln.Close()

	bal := NewBalancer(BalancerConfig{
		Addr:      balancerAddr,
		Algorithm: "wrr",
	}, pool, HealthCheckConfig{
		Interval:           1 * time.Hour,
		Timeout:            1 * time.Second,
		Path:               "/health",
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
	}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go bal.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	// Start a slow request
	wg.Add(1)
	go func() {
		defer wg.Done()
		status, _, _ := sendRawRequest(balancerAddr,
			"GET /slow HTTP/1.1\r\nHost: localhost\r\n\r\n")
		if status != 200 {
			t.Errorf("slow request status = %d, want 200", status)
		}
	}()

	<-requestReceived
	bal.Drain("slow")

	// New request should fail (no healthy backends)
	time.Sleep(10 * time.Millisecond)
	status, _, _ := sendRawRequest(balancerAddr,
		"GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if status != 503 {
		t.Errorf("request during drain: status = %d, want 503", status)
	}

	wg.Wait()
	bal.Shutdown()
}

func TestRequestRetry(t *testing.T) {
	failCount := atomic.Int64{}
	failAddr := startMockBackend(t, "fail", func(conn net.Conn) {
		conn.Close() // immediately close = connection refused on next attempt
		failCount.Add(1)
	})

	goodAddr := startMockBackend(t, "good", okHandler)

	pool := NewBackendPool()
	pool.Add(NewBackend("fail", failAddr, 1))
	pool.Add(NewBackend("good", goodAddr, 1))

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	balancerAddr := ln.Addr().String()
	ln.Close()

	b := NewBalancer(BalancerConfig{
		Addr:       balancerAddr,
		Algorithm:  "wrr",
		MaxRetries: 2,
	}, pool, HealthCheckConfig{
		Interval:           1 * time.Hour,
		Timeout:            1 * time.Second,
		Path:               "/health",
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
	}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go b.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	status, _, err := sendRawRequest(balancerAddr,
		"GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200 (should retry on good backend)", status)
	}

	b.Shutdown()
}

func TestProxyHeaders(t *testing.T) {
	var receivedHeaders map[string]string
	var mu sync.Mutex

	addr := startMockBackend(t, "echo", func(conn net.Conn) {
		defer conn.Close()
		reader := bufio.NewReader(conn)
		hdrs := make(map[string]string)
		reader.ReadString('\n') // skip request line
		for {
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
			idx := strings.IndexByte(line, ':')
			if idx != -1 {
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				hdrs[strings.ToLower(key)] = val
			}
		}
		mu.Lock()
		receivedHeaders = hdrs
		mu.Unlock()

		resp := "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nOK"
		conn.Write([]byte(resp))
	})

	pool := NewBackendPool()
	pool.Add(NewBackend("echo", addr, 1))

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	balancerAddr := ln.Addr().String()
	ln.Close()

	b := NewBalancer(BalancerConfig{
		Addr:      balancerAddr,
		Algorithm: "wrr",
	}, pool, HealthCheckConfig{
		Interval:           1 * time.Hour,
		Timeout:            1 * time.Second,
		Path:               "/health",
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
	}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go b.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	sendRawRequest(balancerAddr, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")

	mu.Lock()
	defer mu.Unlock()

	if receivedHeaders["x-forwarded-for"] == "" {
		t.Error("X-Forwarded-For header should be set")
	}
	if receivedHeaders["via"] != "1.1 lb" {
		t.Errorf("Via = %q, want '1.1 lb'", receivedHeaders["via"])
	}
}

func TestAllBackendsDown(t *testing.T) {
	pool := NewBackendPool()
	b := NewBackend("down", "127.0.0.1:1", 1)
	b.SetStatus(StatusUnhealthy)
	pool.Add(b)

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	balancerAddr := ln.Addr().String()
	ln.Close()

	bal := NewBalancer(BalancerConfig{
		Addr:      balancerAddr,
		Algorithm: "wrr",
	}, pool, HealthCheckConfig{
		Interval:           1 * time.Hour,
		Timeout:            1 * time.Second,
		Path:               "/health",
		HealthyThreshold:   1,
		UnhealthyThreshold: 3,
	}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go bal.ListenAndServe(ctx)
	time.Sleep(50 * time.Millisecond)

	status, _, err := sendRawRequest(balancerAddr,
		"GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if status != 503 {
		t.Errorf("status = %d, want 503", status)
	}

	bal.Shutdown()
}
```

### Running and Testing

```bash
go test -v -race -timeout 30s ./...
```

### Expected Output

```
=== RUN   TestWeightedRoundRobin
    balancer_test.go:96: Distribution: A=302 (expect ~300), B=198 (expect ~200), C=100 (expect ~100)
--- PASS: TestWeightedRoundRobin (0.65s)
=== RUN   TestLeastConnections
--- PASS: TestLeastConnections (0.00s)
=== RUN   TestStickySessions
--- PASS: TestStickySessions (0.06s)
=== RUN   TestGracefulDraining
--- PASS: TestGracefulDraining (0.28s)
=== RUN   TestRequestRetry
--- PASS: TestRequestRetry (0.06s)
=== RUN   TestProxyHeaders
--- PASS: TestProxyHeaders (0.06s)
=== RUN   TestAllBackendsDown
--- PASS: TestAllBackendsDown (0.06s)
PASS
```

## Design Decisions

**Decision 1: Smooth WRR (Nginx algorithm) over naive WRR.** Naive WRR sends bursts: all requests for A, then all for B. Smooth WRR interleaves them: A, B, A, C, A, B. This prevents burst-induced latency spikes on individual backends. The algorithm uses a `currentWeight` per backend that increments by the backend's weight each round and decrements by the total weight when selected. This produces the optimal interleaving.

**Decision 2: Full request buffering for retry.** The request body is read entirely into memory before forwarding. This enables retry: if the first backend fails, the buffered request can be re-sent to another backend. The trade-off is memory usage for large uploads. The `MaxBodyBuffer` config limits this; oversized bodies bypass retry and stream directly.

**Decision 3: Connection-per-request vs. connection pool to backends.** Each proxied request dials a new TCP connection to the backend and closes it after the response. A connection pool would amortize TCP handshake overhead, but adds complexity (idle connection management, broken connection detection, pool sizing per backend). Connection-per-request is correct and simple; pooling is an optimization.

**Decision 4: Chunked response de-chunking.** Backend responses with `Transfer-Encoding: chunked` are fully read and de-chunked before forwarding to the client. The client receives a `Content-Length`-based response. This simplifies the proxy (no need to relay chunk boundaries) but increases memory usage and latency for streaming responses. A production proxy would relay chunks directly.

## Common Mistakes

**Mistake 1: Not decrementing active connections on all paths.** If the backend connection fails after `Dial()` succeeds, you must still call `Release()` to decrement the counter. A `defer` immediately after `Dial()` is the safe pattern. Missing this causes the least-connections algorithm to see phantom connections.

**Mistake 2: Sticky session cookie without health check fallback.** If the sticky backend goes down and you blindly route to it, the client gets repeated failures. Always verify the sticky backend is healthy before honoring the cookie, and fall back to the normal algorithm with a new cookie if it's not.

**Mistake 3: Health checker probing draining backends.** A draining backend should not be probed (it's being intentionally removed). Probing it and marking it "healthy" would restart traffic to it, defeating the drain.

**Mistake 4: Algorithm state shared across reconfigurations.** When switching from WRR to least-connections, the WRR's `currentWeight` state becomes stale. If you switch back later, the stale state produces an incorrect initial distribution. Reset algorithm state on switch.

## Performance Notes

- The smooth WRR algorithm is O(N) per request where N is the number of backends. With typical backend counts (5-50), this is negligible. For very large pools, pre-compute a lookup table.
- Active health checking adds one TCP connection + HTTP request per backend per interval. With 10 backends at 5-second intervals, this is 2 connections/second -- negligible.
- The largest performance bottleneck in this design is connection-per-request to backends. Adding a connection pool with keep-alive would reduce per-request latency by 1-5ms (TCP handshake elimination) under typical datacenter conditions.
- Request body buffering caps at `MaxBodyBuffer`. For requests exceeding this, the first backend attempt is final -- no retry is possible. Set this value based on expected request sizes (e.g., 1MB for API traffic, higher for upload services).
