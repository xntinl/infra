# 12. Reverse Proxy and Load Balancer

<!--
difficulty: advanced
concepts: [reverse-proxy, load-balancer, httputil, round-robin, health-check, x-forwarded-for]
tools: [go, curl]
estimated_time: 45m
bloom_level: analyze
prerequisites: [http-server, http-client, middleware-chains, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of HTTP handlers and middleware
- Familiarity with goroutines and mutexes

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a reverse proxy using `httputil.ReverseProxy`
- **Implement** round-robin load balancing across multiple backends
- **Add** health checking to remove unhealthy backends
- **Set** forwarding headers (`X-Forwarded-For`, `X-Real-IP`)

## Why Reverse Proxies

A reverse proxy sits in front of backend servers and forwards client requests. It can distribute load, terminate TLS, add authentication, or modify requests/responses. Go's `httputil.ReverseProxy` provides the foundation. Building your own teaches you how production proxies like nginx and Envoy work internally.

## The Problem

Build a reverse proxy that load-balances requests across multiple backend servers using round-robin selection. The proxy should health-check backends periodically and remove unhealthy ones from rotation.

## Requirements

1. A simple backend server that can run on different ports
2. A reverse proxy using `httputil.ReverseProxy` with a custom `Director`
3. Round-robin load balancing across backends
4. Periodic health checks that mark backends up or down
5. Forwarding headers for client IP preservation

## Step 1 -- Create a Backend Server

```bash
mkdir -p ~/go-exercises/reverse-proxy
cd ~/go-exercises/reverse-proxy
go mod init reverse-proxy
```

Create `backend/main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9001"
	}

	name := os.Getenv("NAME")
	if name == "" {
		name = "backend-" + port
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from %s\n", name)
		fmt.Fprintf(w, "X-Forwarded-For: %s\n", r.Header.Get("X-Forwarded-For"))
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	log.Printf("Backend %s starting on :%s", name, port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
```

### Intermediate Verification

```bash
PORT=9001 NAME=backend-1 go run backend/main.go &
PORT=9002 NAME=backend-2 go run backend/main.go &

curl http://localhost:9001/
curl http://localhost:9002/
```

Expected: Each returns its own name.

## Step 2 -- Build a Basic Reverse Proxy

Create `proxy/main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func main() {
	target, _ := url.Parse("http://localhost:9001")

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
	}

	fmt.Println("Reverse proxy on :8080 -> :9001")
	log.Fatal(http.ListenAndServe(":8080", proxy))
}
```

### Intermediate Verification

```bash
go run proxy/main.go &
curl http://localhost:8080/
```

Expected: `Hello from backend-1`.

## Step 3 -- Add Round-Robin Load Balancing

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
)

type Backend struct {
	URL     *url.URL
	Healthy bool
}

type LoadBalancer struct {
	mu       sync.RWMutex
	backends []*Backend
	counter  atomic.Uint64
}

func NewLoadBalancer(urls []string) *LoadBalancer {
	lb := &LoadBalancer{}
	for _, u := range urls {
		parsed, err := url.Parse(u)
		if err != nil {
			log.Fatalf("invalid backend URL %s: %v", u, err)
		}
		lb.backends = append(lb.backends, &Backend{
			URL:     parsed,
			Healthy: true,
		})
	}
	return lb
}

func (lb *LoadBalancer) NextHealthy() *Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	total := len(lb.backends)
	for i := 0; i < total; i++ {
		idx := lb.counter.Add(1) % uint64(total)
		b := lb.backends[idx]
		if b.Healthy {
			return b
		}
	}
	return nil
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	backend := lb.NextHealthy()
	if backend == nil {
		http.Error(w, "no healthy backends", http.StatusServiceUnavailable)
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = backend.URL.Scheme
			req.URL.Host = backend.URL.Host
			req.Host = backend.URL.Host

			// Preserve client IP
			if clientIP := req.RemoteAddr; clientIP != "" {
				prior := req.Header.Get("X-Forwarded-For")
				if prior != "" {
					req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
				} else {
					req.Header.Set("X-Forwarded-For", clientIP)
				}
			}
		},
	}

	log.Printf("Routing to %s", backend.URL)
	proxy.ServeHTTP(w, r)
}

func main() {
	lb := NewLoadBalancer([]string{
		"http://localhost:9001",
		"http://localhost:9002",
	})

	fmt.Println("Load balancer on :8080")
	log.Fatal(http.ListenAndServe(":8080", lb))
}
```

### Intermediate Verification

```bash
curl http://localhost:8080/
curl http://localhost:8080/
curl http://localhost:8080/
curl http://localhost:8080/
```

Expected: Alternating responses from `backend-1` and `backend-2`.

## Step 4 -- Add Health Checking

```go
func (lb *LoadBalancer) HealthCheck(interval time.Duration) {
	ticker := time.NewTicker(interval)
	client := &http.Client{Timeout: 2 * time.Second}

	for range ticker.C {
		lb.mu.Lock()
		for _, b := range lb.backends {
			resp, err := client.Get(b.URL.String() + "/health")
			wasHealthy := b.Healthy
			if err != nil || resp.StatusCode != http.StatusOK {
				b.Healthy = false
				if wasHealthy {
					log.Printf("Backend %s marked DOWN", b.URL)
				}
			} else {
				b.Healthy = true
				if !wasHealthy {
					log.Printf("Backend %s marked UP", b.URL)
				}
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
		lb.mu.Unlock()
	}
}
```

Add to `main`:

```go
func main() {
	lb := NewLoadBalancer([]string{
		"http://localhost:9001",
		"http://localhost:9002",
	})

	go lb.HealthCheck(5 * time.Second)

	fmt.Println("Load balancer on :8080")
	log.Fatal(http.ListenAndServe(":8080", lb))
}
```

Add `"time"` to the import block.

### Intermediate Verification

Kill one backend and wait 5 seconds:

```bash
# Stop backend on port 9002, then:
curl http://localhost:8080/
curl http://localhost:8080/
```

Expected: All requests go to the remaining healthy backend. Server logs show the dead backend marked DOWN.

## Hints

- `httputil.ReverseProxy` handles response copying, hop-by-hop header removal, and error handling
- Creating a new `ReverseProxy` per request is simple but wasteful -- in production, pool or cache them
- The `Director` function must set both `req.URL.Host` and `req.Host` for virtual hosting to work
- Use `ErrorHandler` on `ReverseProxy` to customize error responses
- Atomic counter avoids lock contention on the round-robin index

## Verification

- Requests are distributed across backends in round-robin order
- `X-Forwarded-For` header contains the client IP at the backend
- Stopping a backend causes health checks to mark it DOWN
- All subsequent requests route to healthy backends only
- Restarting the backend causes health checks to mark it UP again

## What's Next

Continue to [13 - Building a REST API](../13-building-a-rest-api/13-building-a-rest-api.md) to build a complete REST API from scratch.

## Summary

`httputil.ReverseProxy` provides a production-quality reverse proxy foundation. Round-robin load balancing distributes requests across backends using an atomic counter. Periodic health checks mark backends up or down. Forwarding headers like `X-Forwarded-For` preserve client identity through the proxy layer. This pattern is the basis for API gateways, service meshes, and edge proxies.

## Reference

- [httputil.ReverseProxy](https://pkg.go.dev/net/http/httputil#ReverseProxy)
- [net/url](https://pkg.go.dev/net/url)
- [Load Balancing Algorithms](https://samwho.dev/load-balancing/)
