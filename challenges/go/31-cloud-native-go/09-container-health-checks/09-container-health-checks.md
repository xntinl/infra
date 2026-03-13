# 9. Container Health Checks

<!--
difficulty: advanced
concepts: [health-check, readiness, liveness, startup-probe, graceful-shutdown, dependency-check]
tools: [go, docker, curl]
estimated_time: 30m
bloom_level: analyze
prerequisites: [http-programming, context, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Docker installed (optional, for container testing)
- Understanding of HTTP servers and context cancellation
- Familiarity with Kubernetes probe concepts (liveness, readiness, startup)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** health check HTTP endpoints for liveness, readiness, and startup probes
- **Analyze** when to report healthy vs unhealthy based on application state
- **Design** dependency checks (database, cache, external service) that feed into readiness
- **Integrate** health checks with graceful shutdown to drain connections before termination

## Why Container Health Checks Matter

Container orchestrators (Kubernetes, ECS, Docker Compose) use health checks to decide whether to route traffic to a container, restart it, or wait for it to start. Without proper health endpoints, your application might receive traffic before it is ready, or continue receiving traffic while it is failing.

The three probe types serve different purposes:
- **Startup**: is the application initialized?
- **Liveness**: is the process healthy (not deadlocked/hung)?
- **Readiness**: can this instance handle traffic right now?

## The Problem

Build a Go HTTP application with comprehensive health check endpoints. The application has two dependencies: a database connection and a cache connection. It must:

1. Report startup completion after initialization
2. Report liveness based on the process being responsive
3. Report readiness based on dependency health
4. Drain in-flight requests during shutdown

## Requirements

1. **Three endpoints** -- `/healthz` (liveness), `/readyz` (readiness), `/startupz` (startup probe)
2. **Liveness** -- always returns 200 if the HTTP server is responding (proves the process is not hung)
3. **Readiness** -- checks database and cache connectivity; returns 200 only if all dependencies are healthy; returns 503 with details if any dependency is down
4. **Startup** -- returns 503 until initialization is complete, then 200
5. **JSON response** -- all health endpoints return `{"status":"ok"}` or `{"status":"error","checks":{"db":"ok","cache":"failed"}}`
6. **Graceful shutdown** -- on SIGTERM, mark readiness as false (stop receiving new traffic), wait for in-flight requests to complete, then exit
7. **Tests** -- test each probe state transition

## Hints

<details>
<summary>Hint 1: Health checker struct</summary>

```go
type HealthChecker struct {
    mu       sync.RWMutex
    started  bool
    ready    bool
    checks   map[string]func(ctx context.Context) error
}

func NewHealthChecker() *HealthChecker {
    return &HealthChecker{
        checks: make(map[string]func(ctx context.Context) error),
    }
}

func (h *HealthChecker) AddCheck(name string, check func(ctx context.Context) error) {
    h.checks[name] = check
}
```

</details>

<details>
<summary>Hint 2: Readiness handler</summary>

```go
func (h *HealthChecker) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
    h.mu.RLock()
    ready := h.ready
    h.mu.RUnlock()

    if !ready {
        w.WriteHeader(http.StatusServiceUnavailable)
        json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
        return
    }

    results := make(map[string]string)
    allOK := true
    for name, check := range h.checks {
        if err := check(r.Context()); err != nil {
            results[name] = err.Error()
            allOK = false
        } else {
            results[name] = "ok"
        }
    }
    // respond based on allOK
}
```

</details>

<details>
<summary>Hint 3: Graceful shutdown</summary>

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
defer stop()

go func() {
    <-ctx.Done()
    health.SetReady(false) // stop receiving new traffic
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    server.Shutdown(shutdownCtx) // drain in-flight requests
}()
```

</details>

## Verification

```bash
go test -v -race ./...

# Manual testing
go run main.go &
curl http://localhost:8080/healthz   # 200 OK
curl http://localhost:8080/readyz    # 200 or 503 depending on deps
curl http://localhost:8080/startupz  # 200 after init
kill -TERM $!                         # graceful shutdown
```

Your tests should confirm:
- Liveness always returns 200 when the server is running
- Startup returns 503 before `SetStarted(true)`, then 200
- Readiness returns 503 when a dependency check fails, with failure details in the response
- After `SetReady(false)`, readiness returns 503

## What's Next

Continue to [10 - Prometheus Metrics Exposition](../10-prometheus-metrics-exposition/10-prometheus-metrics-exposition.md) to expose application metrics for Prometheus scraping.

## Summary

- Liveness probes check if the process is alive; readiness probes check if it can handle traffic
- Startup probes prevent liveness checks from killing slow-starting applications
- Readiness should check dependencies (DB, cache) and return 503 with details when unhealthy
- On shutdown, mark readiness as false first, then drain in-flight requests
- Use `sync.RWMutex` to protect health state accessed by probe handlers and state-changing goroutines

## Reference

- [Kubernetes probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)
- [Graceful shutdown in Go](https://pkg.go.dev/net/http#Server.Shutdown)
- [Container health checks](https://docs.docker.com/reference/dockerfile/#healthcheck)
