<!--
difficulty: advanced
concepts: blue-green-deployment, traffic-shifting, health-checks, zero-downtime, reverse-proxy
tools: net/http, net/http/httputil, sync/atomic, context, os/signal
estimated_time: 35m
bloom_level: applying
prerequisites: http-server-basics, graceful-shutdown, health-endpoints, concurrency-basics
-->

# Exercise 30.14: Blue-Green Deployment Patterns

## Prerequisites

Before starting this exercise, you should be comfortable with:

- HTTP server and reverse proxy basics
- Graceful shutdown (Exercise 30.1)
- Health check endpoints (Exercise 30.4)
- Atomic operations and concurrency

## Learning Objectives

By the end of this exercise, you will be able to:

1. Build a reverse proxy that routes traffic between two backend versions (blue and green)
2. Implement traffic switching with health validation before cutover
3. Support gradual traffic shifting (canary) as a percentage between blue and green
4. Roll back instantly to the previous version on health check failure

## Why This Matters

Blue-green deployment eliminates downtime during releases. You run two identical environments: "blue" (current) and "green" (new). Traffic is switched from blue to green after the new version passes health checks. If something goes wrong, you switch back in seconds. This pattern is the foundation of zero-downtime deployments in production systems.

---

## Problem

Build a deployment proxy that sits in front of two backend servers and manages traffic routing between them. The proxy must support instant switching, gradual canary rollouts, automatic health-based rollback, and a control API for operators.

### Hints

- Use `httputil.NewSingleHostReverseProxy` to proxy requests to backends
- Store the active backend target atomically (`atomic.Pointer` or `sync.RWMutex` protecting a URL)
- For canary routing, use a percentage and a random number: `rand.Intn(100) < canaryPercent` routes to green
- Health check the target before switching -- if the new version is unhealthy, refuse the switch
- Use a background goroutine to continuously health-check the active target and auto-rollback on failure

### Step 1: Create the project

```bash
mkdir -p blue-green && cd blue-green
go mod init blue-green
```

### Step 2: Build the backend servers

Create `backend.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func startBackend(name, addr, version string, healthy *bool) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend", name)
		w.Header().Set("X-Version", version)
		json.NewEncoder(w).Encode(map[string]string{
			"backend": name,
			"version": version,
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if !*healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "unhealthy")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	server := &http.Server{Addr: addr, Handler: mux}
	go server.ListenAndServe()
	return server
}
```

### Step 3: Build the deployment proxy

Create `proxy.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

type Backend struct {
	Name    string
	URL     *url.URL
	Proxy   *httputil.ReverseProxy
	Healthy bool
}

type DeploymentProxy struct {
	mu sync.RWMutex

	blue  *Backend
	green *Backend

	active        string  // "blue" or "green"
	canaryPercent int     // 0-100, percent of traffic to green
	autoRollback  bool
}

func NewDeploymentProxy(blueURL, greenURL string) (*DeploymentProxy, error) {
	bu, err := url.Parse(blueURL)
	if err != nil {
		return nil, fmt.Errorf("parse blue URL: %w", err)
	}
	gu, err := url.Parse(greenURL)
	if err != nil {
		return nil, fmt.Errorf("parse green URL: %w", err)
	}

	return &DeploymentProxy{
		blue: &Backend{
			Name:  "blue",
			URL:   bu,
			Proxy: httputil.NewSingleHostReverseProxy(bu),
		},
		green: &Backend{
			Name:  "green",
			URL:   gu,
			Proxy: httputil.NewSingleHostReverseProxy(gu),
		},
		active:       "blue",
		autoRollback: true,
	}, nil
}

func (dp *DeploymentProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	backend := dp.selectBackend()
	w.Header().Set("X-Routed-To", backend.Name)
	backend.Proxy.ServeHTTP(w, r)
}

func (dp *DeploymentProxy) selectBackend() *Backend {
	dp.mu.RLock()
	defer dp.mu.RUnlock()

	// If canary is active, route a percentage to green
	if dp.canaryPercent > 0 && dp.canaryPercent < 100 {
		if rand.Intn(100) < dp.canaryPercent {
			return dp.green
		}
		return dp.blue
	}

	if dp.active == "green" {
		return dp.green
	}
	return dp.blue
}

func (dp *DeploymentProxy) Switch(target string) error {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	var backend *Backend
	switch target {
	case "blue":
		backend = dp.blue
	case "green":
		backend = dp.green
	default:
		return fmt.Errorf("unknown target: %s", target)
	}

	// Health check before switching
	if !dp.checkHealth(backend) {
		return fmt.Errorf("target %s is unhealthy, refusing to switch", target)
	}

	old := dp.active
	dp.active = target
	dp.canaryPercent = 0
	log.Printf("[DEPLOY] Switched %s -> %s", old, target)
	return nil
}

func (dp *DeploymentProxy) SetCanary(percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("canary percent must be 0-100, got %d", percent)
	}

	dp.mu.Lock()
	defer dp.mu.Unlock()

	if percent > 0 && !dp.checkHealth(dp.green) {
		return fmt.Errorf("green backend is unhealthy, cannot start canary")
	}

	dp.canaryPercent = percent
	log.Printf("[DEPLOY] Canary set to %d%% green", percent)

	if percent == 100 {
		dp.active = "green"
		dp.canaryPercent = 0
		log.Printf("[DEPLOY] Canary promoted: green is now active")
	}

	return nil
}

func (dp *DeploymentProxy) Rollback() {
	dp.mu.Lock()
	defer dp.mu.Unlock()
	old := dp.active
	dp.active = "blue"
	dp.canaryPercent = 0
	log.Printf("[DEPLOY] Rollback: %s -> blue", old)
}

func (dp *DeploymentProxy) Status() map[string]interface{} {
	dp.mu.RLock()
	defer dp.mu.RUnlock()
	return map[string]interface{}{
		"active":         dp.active,
		"canary_percent": dp.canaryPercent,
		"blue_healthy":   dp.checkHealth(dp.blue),
		"green_healthy":  dp.checkHealth(dp.green),
		"auto_rollback":  dp.autoRollback,
	}
}

func (dp *DeploymentProxy) checkHealth(backend *Backend) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(backend.URL.String() + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (dp *DeploymentProxy) StartHealthWatcher(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dp.mu.RLock()
				active := dp.active
				autoRollback := dp.autoRollback
				dp.mu.RUnlock()

				var activeBackend *Backend
				if active == "green" {
					activeBackend = dp.green
				} else {
					activeBackend = dp.blue
				}

				if !dp.checkHealth(activeBackend) {
					log.Printf("[HEALTH] Active backend %s is unhealthy!", active)
					if autoRollback && active == "green" {
						dp.Rollback()
					}
				}
			}
		}
	}()
}
```

### Step 4: Wire it up

Create `main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

func main() {
	// Start backend servers
	blueHealthy := true
	greenHealthy := true

	startBackend("blue", ":9001", "1.0.0", &blueHealthy)
	startBackend("green", ":9002", "2.0.0", &greenHealthy)
	time.Sleep(200 * time.Millisecond)

	// Create deployment proxy
	proxy, err := NewDeploymentProxy("http://localhost:9001", "http://localhost:9002")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	proxy.StartHealthWatcher(ctx, 3*time.Second)

	// Control API
	controlMux := http.NewServeMux()

	controlMux.HandleFunc("GET /deploy/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxy.Status())
	})

	controlMux.HandleFunc("POST /deploy/switch", func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if err := proxy.Switch(target); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "switched to %s\n", target)
	})

	controlMux.HandleFunc("POST /deploy/canary", func(w http.ResponseWriter, r *http.Request) {
		pct, _ := strconv.Atoi(r.URL.Query().Get("percent"))
		if err := proxy.SetCanary(pct); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "canary set to %d%%\n", pct)
	})

	controlMux.HandleFunc("POST /deploy/rollback", func(w http.ResponseWriter, r *http.Request) {
		proxy.Rollback()
		fmt.Fprintln(w, "rolled back to blue")
	})

	// Start control API on separate port
	go func() {
		log.Println("Control API on :8081")
		http.ListenAndServe(":8081", controlMux)
	}()

	// Start proxy on main port
	log.Println("Proxy on :8080 (blue active)")
	http.ListenAndServe(":8080", proxy)
}
```

### Step 5: Test the deployment workflow

```bash
go run . &
sleep 1

# Traffic goes to blue
curl -s localhost:8080/ | jq .backend

# Check deployment status
curl -s localhost:8081/deploy/status | jq .

# Start canary: 20% to green
curl -s -X POST "localhost:8081/deploy/canary?percent=20"

# Send traffic, some goes to green
for i in $(seq 1 10); do curl -s localhost:8080/ | jq -r .backend; done

# Increase canary to 50%
curl -s -X POST "localhost:8081/deploy/canary?percent=50"

# Promote to 100% (full switch)
curl -s -X POST "localhost:8081/deploy/canary?percent=100"
curl -s localhost:8080/ | jq .backend

# Rollback to blue
curl -s -X POST localhost:8081/deploy/rollback
curl -s localhost:8080/ | jq .backend

kill %1
```

---

## Verify

```bash
go build -o proxy . && ./proxy &
sleep 1
BACKEND=$(curl -s localhost:8080/ | jq -r .backend)
echo "Initial backend: $BACKEND"
curl -s -X POST "localhost:8081/deploy/switch?target=green" > /dev/null
BACKEND=$(curl -s localhost:8080/ | jq -r .backend)
echo "After switch: $BACKEND"
kill %1
```

Should print `Initial backend: blue` and `After switch: green`.

---

## Summary

- Blue-green deployment runs two identical environments and switches traffic between them
- A reverse proxy selects the target backend atomically for zero-downtime switching
- Canary routing sends a percentage of traffic to the new version for gradual rollout
- Health check the new version before switching; auto-rollback if it becomes unhealthy
- Separate the data plane (proxy on port 8080) from the control plane (admin API on 8081)

## Reference

- [net/http/httputil.ReverseProxy](https://pkg.go.dev/net/http/httputil#ReverseProxy)
- [Blue-Green Deployments (Martin Fowler)](https://martinfowler.com/bliki/BlueGreenDeployment.html)
- [Canary Releases](https://martinfowler.com/bliki/CanaryRelease.html)
- [Kubernetes deployment strategies](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy)
