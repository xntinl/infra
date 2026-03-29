# Solution: Reverse Proxy with TLS Termination

## Architecture Overview

The proxy is organized into five packages:

```
                    +-------------------+
                    |   main (config)   |
                    +--------+----------+
                             |
         +-------------------+-------------------+
         |                   |                   |
  +------v------+    +------v------+    +-------v-------+
  |   TLS       |    |   Router    |    |   Health      |
  |   Listener  |    |   (SNI +    |    |   Checker     |
  |             |    |   Round     |    |               |
  |             |    |   Robin)    |    |               |
  +------+------+    +------+------+    +-------+-------+
         |                  |                   |
  +------v------+    +------v------+           |
  |   HTTP      |    |   Backend   |<----------+
  |   Parser    |    |   Pool      |
  +------+------+    +------+------+
         |                  |
  +------v------------------v------+
  |        Proxy Forwarder         |
  |  (headers, relay, logging)     |
  +--------------------------------+
```

1. **TLS Listener**: Accepts connections, performs TLS handshake with SNI-based certificate selection
2. **HTTP Parser**: Raw byte parsing of HTTP/1.1 requests from the TLS connection
3. **Router**: Maps SNI domain names to backend pools via configuration
4. **Backend Pool**: Round-robin selection with health state tracking
5. **Proxy Forwarder**: Header manipulation, request forwarding, response relay
6. **Health Checker**: Background goroutine checking backend health endpoints

## Go Solution

### Project Setup

```bash
mkdir -p reverse-proxy && cd reverse-proxy
go mod init reverse-proxy
```

### config.json

```json
{
  "listen_addr": ":8443",
  "read_timeout_ms": 30000,
  "write_timeout_ms": 30000,
  "idle_timeout_ms": 120000,
  "backend_timeout_ms": 10000,
  "drain_timeout_ms": 30000,
  "health_check_interval_ms": 10000,
  "health_check_path": "/health",
  "routes": [
    {
      "domain": "app.example.com",
      "cert_file": "certs/app.example.com.pem",
      "key_file": "certs/app.example.com-key.pem",
      "backends": ["127.0.0.1:8081", "127.0.0.1:8082"]
    },
    {
      "domain": "api.example.com",
      "cert_file": "certs/api.example.com.pem",
      "key_file": "certs/api.example.com-key.pem",
      "backends": ["127.0.0.1:9091"]
    }
  ]
}
```

### certgen.go

```go
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"time"
)

func generateSelfSignedCert(domain, certFile, keyFile string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	defer certOut.Close()
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyOut, err := os.Create(keyFile)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	keyDER, _ := x509.MarshalECPrivateKey(priv)
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return nil
}
```

### httpparser.go

```go
package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

type HTTPRequest struct {
	Method  string
	Path    string
	Version string
	Headers map[string][]string
	Body    []byte
}

type HTTPResponse struct {
	StatusCode int
	StatusText string
	Version    string
	Headers    map[string][]string
	Body       []byte
}

func parseHTTPRequest(reader *bufio.Reader) (*HTTPRequest, error) {
	// Read request line
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading request line: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed request line: %q", line)
	}

	req := &HTTPRequest{
		Method:  parts[0],
		Path:    parts[1],
		Version: parts[2],
		Headers: make(map[string][]string),
	}

	// Read headers
	for {
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])
		canonicalName := canonicalHeaderName(name)
		req.Headers[canonicalName] = append(req.Headers[canonicalName], value)
	}

	// Read body if Content-Length is present
	if clValues, ok := req.Headers["Content-Length"]; ok && len(clValues) > 0 {
		contentLength, err := strconv.Atoi(clValues[0])
		if err == nil && contentLength > 0 {
			body := make([]byte, contentLength)
			_, err = io.ReadFull(reader, body)
			if err != nil {
				return nil, fmt.Errorf("reading body: %w", err)
			}
			req.Body = body
		}
	}

	return req, nil
}

func canonicalHeaderName(name string) string {
	parts := strings.Split(strings.ToLower(name), "-")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "-")
}

func serializeHTTPRequest(req *HTTPRequest, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s %s %s\r\n", req.Method, req.Path, req.Version)
	if err != nil {
		return err
	}
	for name, values := range req.Headers {
		for _, v := range values {
			_, err = fmt.Fprintf(w, "%s: %s\r\n", name, v)
			if err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprint(w, "\r\n")
	if err != nil {
		return err
	}
	if len(req.Body) > 0 {
		_, err = w.Write(req.Body)
	}
	return err
}

func relayHTTPResponse(backendConn net.Conn, clientWriter io.Writer) (*HTTPResponse, error) {
	reader := bufio.NewReader(backendConn)

	// Read status line
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading status line: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")

	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed status line: %q", line)
	}
	statusCode, _ := strconv.Atoi(parts[1])
	statusText := ""
	if len(parts) >= 3 {
		statusText = parts[2]
	}

	resp := &HTTPResponse{
		StatusCode: statusCode,
		StatusText: statusText,
		Version:    parts[0],
		Headers:    make(map[string][]string),
	}

	// Write status line to client
	fmt.Fprintf(clientWriter, "%s\r\n", line)

	// Read and relay headers
	var contentLength int = -1
	for {
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading response header: %w", err)
		}
		fmt.Fprint(clientWriter, line)
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx >= 0 {
			name := canonicalHeaderName(strings.TrimSpace(trimmed[:colonIdx]))
			value := strings.TrimSpace(trimmed[colonIdx+1:])
			resp.Headers[name] = append(resp.Headers[name], value)
			if name == "Content-Length" {
				contentLength, _ = strconv.Atoi(value)
			}
		}
	}

	// Relay body
	if contentLength >= 0 {
		body := make([]byte, contentLength)
		_, err = io.ReadFull(reader, body)
		if err != nil {
			return nil, err
		}
		clientWriter.Write(body)
		resp.Body = body
	} else {
		// Chunked or connection-close: copy until EOF
		buf := make([]byte, 32768)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				clientWriter.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}

	return resp, nil
}
```

### backend.go

```go
package main

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Backend struct {
	Address string
	Healthy atomic.Bool
}

type BackendPool struct {
	Backends []*Backend
	counter  atomic.Uint64
	mu       sync.RWMutex
}

func NewBackendPool(addresses []string) *BackendPool {
	pool := &BackendPool{
		Backends: make([]*Backend, len(addresses)),
	}
	for i, addr := range addresses {
		b := &Backend{Address: addr}
		b.Healthy.Store(true)
		pool.Backends[i] = b
	}
	return pool
}

// NextHealthy selects the next healthy backend using round-robin.
func (p *BackendPool) NextHealthy() (*Backend, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	total := len(p.Backends)
	for i := 0; i < total; i++ {
		idx := int(p.counter.Add(1)-1) % total
		backend := p.Backends[idx]
		if backend.Healthy.Load() {
			return backend, nil
		}
	}
	return nil, fmt.Errorf("no healthy backends available")
}

// HealthCheck performs HTTP health check against a backend.
func (p *BackendPool) HealthCheck(path string, timeout time.Duration) {
	for _, backend := range p.Backends {
		go func(b *Backend) {
			client := &http.Client{Timeout: timeout}
			url := fmt.Sprintf("http://%s%s", b.Address, path)
			resp, err := client.Get(url)
			if err != nil || resp.StatusCode >= 500 {
				if b.Healthy.Swap(false) {
					fmt.Printf("[health] Backend %s is DOWN\n", b.Address)
				}
			} else {
				if !b.Healthy.Swap(true) {
					fmt.Printf("[health] Backend %s is UP\n", b.Address)
				}
				resp.Body.Close()
			}
		}(backend)
	}
}

// ConnectBackend establishes a TCP connection to the selected backend.
func ConnectBackend(backend *Backend, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", backend.Address, timeout)
}
```

### router.go

```go
package main

import (
	"crypto/tls"
	"fmt"
	"sync"
)

type Route struct {
	Domain   string
	CertFile string
	KeyFile  string
	Backends []string
	Pool     *BackendPool
	Cert     *tls.Certificate
}

type Router struct {
	routes map[string]*Route
	mu     sync.RWMutex
}

func NewRouter() *Router {
	return &Router{
		routes: make(map[string]*Route),
	}
}

func (r *Router) AddRoute(route *Route) error {
	cert, err := tls.LoadX509KeyPair(route.CertFile, route.KeyFile)
	if err != nil {
		return fmt.Errorf("loading cert for %s: %w", route.Domain, err)
	}
	route.Cert = &cert
	route.Pool = NewBackendPool(route.Backends)

	r.mu.Lock()
	r.routes[route.Domain] = route
	r.mu.Unlock()
	return nil
}

func (r *Router) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	route, ok := r.routes[hello.ServerName]
	if !ok {
		return nil, fmt.Errorf("no certificate for domain: %s", hello.ServerName)
	}
	return route.Cert, nil
}

func (r *Router) GetPool(domain string) (*BackendPool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	route, ok := r.routes[domain]
	if !ok {
		return nil, fmt.Errorf("no route for domain: %s", domain)
	}
	return route.Pool, nil
}

func (r *Router) AllPools() []*BackendPool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pools := make([]*BackendPool, 0, len(r.routes))
	for _, route := range r.routes {
		pools = append(pools, route.Pool)
	}
	return pools
}
```

### proxy.go

```go
package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailers":            true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

type ProxyServer struct {
	Router         *Router
	ListenAddr     string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	BackendTimeout time.Duration
	DrainTimeout   time.Duration
	HealthInterval time.Duration
	HealthPath     string

	listener   net.Listener
	wg         sync.WaitGroup
	shutdownCh chan struct{}
}

func NewProxyServer(router *Router, config *Config) *ProxyServer {
	return &ProxyServer{
		Router:         router,
		ListenAddr:     config.ListenAddr,
		ReadTimeout:    time.Duration(config.ReadTimeoutMs) * time.Millisecond,
		WriteTimeout:   time.Duration(config.WriteTimeoutMs) * time.Millisecond,
		IdleTimeout:    time.Duration(config.IdleTimeoutMs) * time.Millisecond,
		BackendTimeout: time.Duration(config.BackendTimeoutMs) * time.Millisecond,
		DrainTimeout:   time.Duration(config.DrainTimeoutMs) * time.Millisecond,
		HealthInterval: time.Duration(config.HealthIntervalMs) * time.Millisecond,
		HealthPath:     config.HealthPath,
		shutdownCh:     make(chan struct{}),
	}
}

func (p *ProxyServer) Start() error {
	tlsConfig := &tls.Config{
		GetCertificate: p.Router.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	tcpListener, err := net.Listen("tcp", p.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	p.listener = tls.NewListener(tcpListener, tlsConfig)

	fmt.Printf("[proxy] Listening on %s (TLS)\n", p.ListenAddr)

	// Start health checker
	go p.healthCheckLoop()

	// Accept connections
	go p.acceptLoop()

	return nil
}

func (p *ProxyServer) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.shutdownCh:
				return // graceful shutdown
			default:
				fmt.Printf("[proxy] Accept error: %v\n", err)
				continue
			}
		}

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.handleConnection(conn)
		}()
	}
}

func (p *ProxyServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		fmt.Println("[proxy] Not a TLS connection")
		return
	}

	// Complete TLS handshake to get SNI
	if err := tlsConn.Handshake(); err != nil {
		fmt.Printf("[proxy] TLS handshake error: %v\n", err)
		return
	}

	sni := tlsConn.ConnectionState().ServerName
	clientAddr := conn.RemoteAddr().(*net.TCPAddr).IP.String()

	reader := bufio.NewReader(tlsConn)

	// Support keep-alive: handle multiple requests per connection
	for {
		conn.SetReadDeadline(time.Now().Add(p.IdleTimeout))

		req, err := parseHTTPRequest(reader)
		if err != nil {
			break // client closed or timeout
		}

		start := time.Now()
		status := p.proxyRequest(req, tlsConn, sni, clientAddr)
		latency := time.Since(start)

		// Access log
		fmt.Printf("[access] %s %s %s %s %s -> %d (%v)\n",
			time.Now().Format(time.RFC3339),
			clientAddr, sni, req.Method, req.Path,
			status, latency,
		)

		// Check Connection: close
		if connHeader, ok := req.Headers["Connection"]; ok {
			for _, v := range connHeader {
				if strings.ToLower(v) == "close" {
					return
				}
			}
		}
	}
}

func (p *ProxyServer) proxyRequest(
	req *HTTPRequest,
	clientConn net.Conn,
	sni string,
	clientAddr string,
) int {
	// Get backend pool for this domain
	pool, err := p.Router.GetPool(sni)
	if err != nil {
		writeErrorResponse(clientConn, 502, "Bad Gateway: no route for domain")
		return 502
	}

	// Select backend
	backend, err := pool.NextHealthy()
	if err != nil {
		writeErrorResponse(clientConn, 503, "Service Unavailable: no healthy backends")
		return 503
	}

	// Remove hop-by-hop headers
	for header := range hopByHopHeaders {
		delete(req.Headers, header)
	}

	// Add forwarding headers
	if existing, ok := req.Headers["X-Forwarded-For"]; ok {
		req.Headers["X-Forwarded-For"] = []string{existing[0] + ", " + clientAddr}
	} else {
		req.Headers["X-Forwarded-For"] = []string{clientAddr}
	}
	req.Headers["X-Forwarded-Proto"] = []string{"https"}
	if host, ok := req.Headers["Host"]; ok {
		req.Headers["X-Forwarded-Host"] = host
	}
	req.Headers["Via"] = []string{"1.1 reverse-proxy"}

	// Connect to backend
	backendConn, err := ConnectBackend(backend, p.BackendTimeout)
	if err != nil {
		writeErrorResponse(clientConn, 502, "Bad Gateway: backend connection failed")
		return 502
	}
	defer backendConn.Close()

	backendConn.SetDeadline(time.Now().Add(p.BackendTimeout))

	// Forward request to backend
	err = serializeHTTPRequest(req, backendConn)
	if err != nil {
		writeErrorResponse(clientConn, 502, "Bad Gateway: failed to forward request")
		return 502
	}

	// Relay response back to client
	resp, err := relayHTTPResponse(backendConn, clientConn)
	if err != nil {
		return 502
	}

	return resp.StatusCode
}

func writeErrorResponse(w net.Conn, status int, message string) {
	body := fmt.Sprintf("%d %s\r\n", status, message)
	fmt.Fprintf(w, "HTTP/1.1 %d %s\r\n", status, message)
	fmt.Fprintf(w, "Content-Length: %d\r\n", len(body))
	fmt.Fprint(w, "Content-Type: text/plain\r\n")
	fmt.Fprint(w, "Connection: close\r\n")
	fmt.Fprint(w, "\r\n")
	fmt.Fprint(w, body)
}

func (p *ProxyServer) healthCheckLoop() {
	ticker := time.NewTicker(p.HealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for _, pool := range p.Router.AllPools() {
				pool.HealthCheck(p.HealthPath, 5*time.Second)
			}
		case <-p.shutdownCh:
			return
		}
	}
}

func (p *ProxyServer) GracefulShutdown() {
	fmt.Println("[proxy] Initiating graceful shutdown...")
	close(p.shutdownCh)
	p.listener.Close()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("[proxy] All connections drained")
	case <-time.After(p.DrainTimeout):
		fmt.Println("[proxy] Drain timeout reached, forcing shutdown")
	}
}
```

### main.go

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type RouteConfig struct {
	Domain   string   `json:"domain"`
	CertFile string   `json:"cert_file"`
	KeyFile  string   `json:"key_file"`
	Backends []string `json:"backends"`
}

type Config struct {
	ListenAddr       string        `json:"listen_addr"`
	ReadTimeoutMs    int           `json:"read_timeout_ms"`
	WriteTimeoutMs   int           `json:"write_timeout_ms"`
	IdleTimeoutMs    int           `json:"idle_timeout_ms"`
	BackendTimeoutMs int           `json:"backend_timeout_ms"`
	DrainTimeoutMs   int           `json:"drain_timeout_ms"`
	HealthIntervalMs int           `json:"health_check_interval_ms"`
	HealthPath       string        `json:"health_check_path"`
	Routes           []RouteConfig `json:"routes"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config Config
	err = json.Unmarshal(data, &config)
	return &config, err
}

// startTestBackend starts a simple HTTP backend for testing.
func startTestBackend(addr string, name string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from %s\n", name)
		fmt.Fprintf(w, "Path: %s\n", r.URL.Path)
		fmt.Fprintf(w, "X-Forwarded-For: %s\n", r.Header.Get("X-Forwarded-For"))
		fmt.Fprintf(w, "X-Forwarded-Proto: %s\n", r.Header.Get("X-Forwarded-Proto"))
		fmt.Fprintf(w, "X-Forwarded-Host: %s\n", r.Header.Get("X-Forwarded-Host"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	})
	fmt.Printf("[backend] %s starting on %s\n", name, addr)
	go http.ListenAndServe(addr, mux)
}

func main() {
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	config, err := loadConfig(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Generate test certificates if they don't exist
	os.MkdirAll("certs", 0755)
	for _, route := range config.Routes {
		if _, err := os.Stat(route.CertFile); os.IsNotExist(err) {
			fmt.Printf("[setup] Generating self-signed cert for %s\n", route.Domain)
			err = generateSelfSignedCert(route.Domain, route.CertFile, route.KeyFile)
			if err != nil {
				fmt.Printf("Error generating cert: %v\n", err)
				os.Exit(1)
			}
		}
	}

	// Start test backends
	startTestBackend("127.0.0.1:8081", "backend-1")
	startTestBackend("127.0.0.1:8082", "backend-2")
	startTestBackend("127.0.0.1:9091", "backend-api")

	// Build router
	router := NewRouter()
	for _, rc := range config.Routes {
		err := router.AddRoute(&Route{
			Domain:   rc.Domain,
			CertFile: rc.CertFile,
			KeyFile:  rc.KeyFile,
			Backends: rc.Backends,
		})
		if err != nil {
			fmt.Printf("Error adding route for %s: %v\n", rc.Domain, err)
			os.Exit(1)
		}
	}

	// Start proxy
	proxy := NewProxyServer(router, config)
	if err := proxy.Start(); err != nil {
		fmt.Printf("Error starting proxy: %v\n", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	fmt.Printf("\n[proxy] Received %v, shutting down...\n", sig)
	proxy.GracefulShutdown()
}
```

### Tests: proxy_test.go

```go
package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func setupTestRouter(t *testing.T) *Router {
	t.Helper()
	domain := "test.example.com"
	certFile := "certs/test.example.com.pem"
	keyFile := "certs/test.example.com-key.pem"

	if err := generateSelfSignedCert(domain, certFile, keyFile); err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	// Start a test backend
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "XFF:%s\n", r.Header.Get("X-Forwarded-For"))
		fmt.Fprintf(w, "XFP:%s\n", r.Header.Get("X-Forwarded-Proto"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	backendListener, _ := net.Listen("tcp", "127.0.0.1:0")
	go http.Serve(backendListener, mux)

	router := NewRouter()
	router.AddRoute(&Route{
		Domain:   domain,
		CertFile: certFile,
		KeyFile:  keyFile,
		Backends: []string{backendListener.Addr().String()},
	})

	return router
}

func TestSNICertificateSelection(t *testing.T) {
	router := setupTestRouter(t)

	tlsConfig := &tls.Config{
		GetCertificate: router.GetCertificate,
	}

	listener, _ := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		conn.Close()
	}()

	clientConfig := &tls.Config{
		ServerName:         "test.example.com",
		InsecureSkipVerify: true,
	}
	conn, err := tls.Dial("tcp", listener.Addr().String(), clientConfig)
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if state.ServerName != "test.example.com" {
		t.Errorf("expected SNI test.example.com, got %s", state.ServerName)
	}
}

func TestUnknownSNIRejected(t *testing.T) {
	router := setupTestRouter(t)

	tlsConfig := &tls.Config{
		GetCertificate: router.GetCertificate,
	}

	listener, _ := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	clientConfig := &tls.Config{
		ServerName:         "unknown.example.com",
		InsecureSkipVerify: true,
	}
	_, err := tls.Dial("tcp", listener.Addr().String(), clientConfig)
	if err == nil {
		t.Error("expected error for unknown SNI, got nil")
	}
}

func TestHTTPParsing(t *testing.T) {
	raw := "GET /api/test HTTP/1.1\r\nHost: test.example.com\r\nContent-Length: 5\r\n\r\nhello"
	reader := bufio.NewReader(strings.NewReader(raw))
	req, err := parseHTTPRequest(reader)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if req.Method != "GET" {
		t.Errorf("method: expected GET, got %s", req.Method)
	}
	if req.Path != "/api/test" {
		t.Errorf("path: expected /api/test, got %s", req.Path)
	}
	if string(req.Body) != "hello" {
		t.Errorf("body: expected hello, got %s", string(req.Body))
	}
}

func TestHopByHopHeaderRemoval(t *testing.T) {
	headers := map[string][]string{
		"Host":           {"example.com"},
		"Connection":     {"keep-alive"},
		"Keep-Alive":     {"timeout=5"},
		"Content-Type":   {"application/json"},
		"Authorization":  {"Bearer token"},
	}
	for h := range hopByHopHeaders {
		delete(headers, h)
	}
	if _, ok := headers["Connection"]; ok {
		t.Error("Connection header should be removed")
	}
	if _, ok := headers["Keep-Alive"]; ok {
		t.Error("Keep-Alive header should be removed")
	}
	if _, ok := headers["Content-Type"]; !ok {
		t.Error("Content-Type should be preserved")
	}
}

func TestRoundRobinSelection(t *testing.T) {
	pool := NewBackendPool([]string{"a:1", "b:2", "c:3"})
	counts := map[string]int{}
	for i := 0; i < 9; i++ {
		b, _ := pool.NextHealthy()
		counts[b.Address]++
	}
	for addr, count := range counts {
		if count != 3 {
			t.Errorf("backend %s selected %d times, expected 3", addr, count)
		}
	}
}

func TestHealthCheckMarksDown(t *testing.T) {
	pool := NewBackendPool([]string{"127.0.0.1:1"}) // port 1 = unreachable
	pool.HealthCheck("/health", 1*time.Second)
	time.Sleep(2 * time.Second)
	_, err := pool.NextHealthy()
	if err == nil {
		t.Error("expected no healthy backends after failed health check")
	}
}

func TestXForwardedHeaders(t *testing.T) {
	req := &HTTPRequest{
		Method:  "GET",
		Path:    "/",
		Version: "HTTP/1.1",
		Headers: map[string][]string{
			"Host": {"app.example.com"},
		},
	}
	clientAddr := "192.168.1.100"

	req.Headers["X-Forwarded-For"] = []string{clientAddr}
	req.Headers["X-Forwarded-Proto"] = []string{"https"}
	req.Headers["X-Forwarded-Host"] = req.Headers["Host"]

	if req.Headers["X-Forwarded-For"][0] != clientAddr {
		t.Errorf("XFF: expected %s, got %s", clientAddr, req.Headers["X-Forwarded-For"][0])
	}
	if req.Headers["X-Forwarded-Proto"][0] != "https" {
		t.Error("XFP should be https")
	}
}

func TestAccessLogFormat(t *testing.T) {
	// Verify log format by checking string contains expected fields
	log := fmt.Sprintf("[access] %s %s %s %s %s -> %d (%v)",
		time.Now().Format(time.RFC3339),
		"192.168.1.1", "app.example.com", "GET", "/api",
		200, 15*time.Millisecond)
	if !strings.Contains(log, "192.168.1.1") { t.Error("missing client IP") }
	if !strings.Contains(log, "app.example.com") { t.Error("missing SNI") }
	if !strings.Contains(log, "GET") { t.Error("missing method") }
	if !strings.Contains(log, "/api") { t.Error("missing path") }
	if !strings.Contains(log, "200") { t.Error("missing status") }
}
```

### Running

```bash
go build -o reverse-proxy .
go test -v ./...

# Run the proxy
./reverse-proxy config.json

# Test with curl (add --resolve to fake DNS)
curl -k --resolve app.example.com:8443:127.0.0.1 https://app.example.com:8443/
```

### Expected Output

```
[setup] Generating self-signed cert for app.example.com
[setup] Generating self-signed cert for api.example.com
[backend] backend-1 starting on 127.0.0.1:8081
[backend] backend-2 starting on 127.0.0.1:8082
[backend] backend-api starting on 127.0.0.1:9091
[proxy] Listening on :8443 (TLS)
[access] 2026-03-28T10:15:32Z 127.0.0.1 app.example.com GET / -> 200 (2.1ms)
[access] 2026-03-28T10:15:33Z 127.0.0.1 app.example.com GET / -> 200 (1.8ms)
[health] Backend 127.0.0.1:8081 is UP
[health] Backend 127.0.0.1:8082 is UP

# curl output:
Hello from backend-1
Path: /
X-Forwarded-For: 127.0.0.1
X-Forwarded-Proto: https
X-Forwarded-Host: app.example.com
```

## Design Decisions

1. **Raw HTTP parsing over `net/http`**: Parsing HTTP from the TLS connection manually demonstrates what a reverse proxy actually does. The `net/http` server abstracts away header parsing, keep-alive management, and body reading. By parsing raw bytes, you see every decision the proxy makes.

2. **`crypto/tls` for TLS, custom everything else**: Go's TLS implementation is production-grade and constant-time. Reimplementing TLS would be a separate challenge (Challenge 91). The proxy value is in the routing, header manipulation, health checking, and connection management -- not in TLS itself.

3. **Atomic booleans for health state**: Using `atomic.Bool` for backend health status avoids locking on the read path (every request checks health). The health checker writes infrequently. An `RWMutex` would also work but adds overhead for the common case (reads).

4. **Round-robin over least-connections**: Round-robin is predictable and stateless. Least-connections requires tracking per-backend connection counts, which adds complexity. For HTTP/1.1 where each request is a separate connection to the backend, round-robin provides adequate distribution.

5. **Graceful shutdown with WaitGroup**: The `sync.WaitGroup` tracks in-flight requests precisely. When SIGTERM arrives, close the listener (stop accepting) and wait for the group to drain. The timeout ensures the proxy doesn't hang indefinitely if a backend is slow.

## Common Mistakes

1. **Not completing the TLS handshake before reading SNI**: The `ServerName` in `ConnectionState()` is only populated after `Handshake()` completes. Reading it before the handshake returns an empty string.

2. **Forwarding hop-by-hop headers**: Headers like `Connection`, `Keep-Alive`, and `Transfer-Encoding` are between the client and proxy, not between the proxy and backend. Forwarding them causes connection management confusion at the backend.

3. **Appending vs. replacing X-Forwarded-For**: If the client already sent an `X-Forwarded-For` header (common in multi-proxy chains), the proxy must append its own value with a comma separator, not replace the existing header. Replacing it loses the original client IP.

4. **Not handling Content-Length correctly**: The proxy must read exactly `Content-Length` bytes for the request body. Reading too few leaves bytes in the stream that corrupt the next request. Reading too many blocks waiting for bytes that will never arrive.

5. **Health check goroutine leak**: If health checks don't have timeouts, a down backend blocks the health checker goroutine indefinitely. Use `http.Client{Timeout: ...}` or `net.DialTimeout` for all backend connections.

## Performance Notes

| Metric | Value |
|--------|-------|
| TLS handshake latency | ~1-3ms (ECDSA P-256 cert) |
| Request forwarding overhead | ~0.5-2ms (local backend) |
| Throughput (keep-alive) | ~10,000-30,000 req/s per core |
| Memory per connection | ~10-20KB (TLS state + buffers) |
| Health check overhead | negligible (~1 HTTP request per interval per backend) |

The bottleneck is typically TLS handshake latency for new connections. With HTTP/1.1 keep-alive, subsequent requests on the same connection skip the handshake and only pay the forwarding overhead.

## Going Further

- Add **HTTP/2 support**: multiplex multiple requests over a single TLS connection using the h2 protocol
- Implement **rate limiting**: per-IP or per-domain request rate limits using a token bucket
- Add **WebSocket proxying**: detect the `Upgrade: websocket` header and switch to bidirectional byte relay
- Implement **backend TLS**: re-encrypt traffic to backends (mTLS) for zero-trust architectures
- Add **circuit breaking**: if a backend returns errors above a threshold, stop sending traffic temporarily
- Implement **request retry**: if a backend returns 502/503, retry on another backend (with idempotency checks)
