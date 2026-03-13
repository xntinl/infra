# 14. Building a Context-Aware Service Framework

<!--
difficulty: insane
concepts: [service-framework, lifecycle-management, dependency-injection, health-checks, graceful-shutdown, context-hierarchy]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [context-withcancel, context-withtimeout-withdeadline, context-withvalue, context-propagation, graceful-shutdown-with-context, select-statement-basics]
-->

## The Challenge

Design and implement a context-aware service framework that manages the lifecycle of multiple components (HTTP servers, background workers, health checks, metrics exporters) using a context hierarchy for coordinated startup and shutdown. The framework must handle component dependencies, health monitoring, and graceful degradation.

## Requirements

1. **Service Interface** -- define a `Service` interface with `Start(ctx context.Context) error` and `Stop(ctx context.Context) error` methods
2. **Application Container** -- an `App` struct that registers services, manages their lifecycle, and provides a shared context hierarchy
3. **Dependency Ordering** -- services declare dependencies; the framework starts them in topological order and stops them in reverse order
4. **Health Monitoring** -- services optionally implement a `Healthcheck` interface; the framework periodically checks health and exposes an aggregate health endpoint
5. **Context Hierarchy** -- the app creates a root context from OS signals; each service receives a derived context that is cancelled independently or when the root is cancelled
6. **Graceful Shutdown** -- on SIGINT/SIGTERM: stop accepting new work, drain in-flight operations (with per-service timeouts), run cleanup hooks, exit with appropriate status code
7. **Error Handling** -- if a critical service fails to start or crashes at runtime, the framework shuts down all other services and exits with an error
8. **Request Context Enrichment** -- HTTP services automatically get middleware that adds request ID, trace ID, and a per-request timeout to the context
9. **Configuration** -- services receive configuration through context values or a config store, not global variables
10. **Metrics** -- the framework tracks service startup time, uptime, health check results, and shutdown duration
11. Must pass `go run -race`

## Hints

<details>
<summary>Hint 1: Core Interfaces</summary>

```go
type Service interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

type DependsOn interface {
	Dependencies() []string
}

type ServiceConfig struct {
	Name            string
	StopTimeout     time.Duration
	Critical        bool   // if true, failure shuts down the app
	HealthInterval  time.Duration
}
```
</details>

<details>
<summary>Hint 2: Application Container</summary>

```go
type App struct {
	mu          sync.Mutex
	services    map[string]serviceEntry
	order       []string           // topological order
	rootCtx     context.Context
	rootCancel  context.CancelFunc
	health      map[string]error   // latest health status per service
	healthMu    sync.RWMutex
	logger      *slog.Logger
	startTime   time.Time
	metrics     *Metrics
}

type serviceEntry struct {
	service    Service
	config     ServiceConfig
	ctx        context.Context
	cancel     context.CancelFunc
	started    bool
	startedAt  time.Time
}

func NewApp(opts ...Option) *App { ... }
func (a *App) Register(svc Service, cfg ServiceConfig) error { ... }
func (a *App) Run() error { ... }
```
</details>

<details>
<summary>Hint 3: Startup Sequence</summary>

```go
func (a *App) Run() error {
	// 1. Create root context from OS signals
	a.rootCtx, a.rootCancel = signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer a.rootCancel()

	// 2. Resolve dependency order (topological sort)
	order, err := a.resolveOrder()
	if err != nil {
		return fmt.Errorf("dependency resolution: %w", err)
	}

	// 3. Start services in order
	for _, name := range order {
		entry := a.services[name]
		svcCtx, svcCancel := context.WithCancel(a.rootCtx)
		entry.ctx = svcCtx
		entry.cancel = svcCancel

		a.logger.Info("starting service", "name", name)
		start := time.Now()

		if err := entry.service.Start(svcCtx); err != nil {
			svcCancel()
			if entry.config.Critical {
				a.shutdown() // stop everything started so far
				return fmt.Errorf("critical service %s failed to start: %w", name, err)
			}
			a.logger.Error("non-critical service failed", "name", name, "error", err)
			continue
		}

		entry.started = true
		entry.startedAt = time.Now()
		a.metrics.RecordStartup(name, time.Since(start))
		a.services[name] = entry
	}

	// 4. Start health checker
	go a.runHealthChecks()

	// 5. Wait for shutdown signal
	<-a.rootCtx.Done()
	return a.shutdown()
}
```
</details>

<details>
<summary>Hint 4: Shutdown Sequence</summary>

```go
func (a *App) shutdown() error {
	a.logger.Info("shutdown initiated")
	var firstErr error

	// Stop in reverse order
	for i := len(a.order) - 1; i >= 0; i-- {
		name := a.order[i]
		entry := a.services[name]
		if !entry.started {
			continue
		}

		timeout := entry.config.StopTimeout
		if timeout == 0 {
			timeout = 10 * time.Second
		}

		stopCtx, stopCancel := context.WithTimeout(context.Background(), timeout)

		a.logger.Info("stopping service", "name", name, "timeout", timeout)
		start := time.Now()

		if err := entry.service.Stop(stopCtx); err != nil {
			a.logger.Error("service stop error", "name", name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}

		entry.cancel() // cancel the service's context
		stopCancel()
		a.metrics.RecordShutdown(name, time.Since(start))
		a.logger.Info("service stopped", "name", name, "duration", time.Since(start))
	}

	return firstErr
}
```
</details>

<details>
<summary>Hint 5: Health Check Loop</summary>

```go
func (a *App) runHealthChecks() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.rootCtx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			for name, entry := range a.services {
				if !entry.started {
					continue
				}
				hc, ok := entry.service.(HealthChecker)
				if !ok {
					continue
				}

				checkCtx, checkCancel := context.WithTimeout(entry.ctx, 3*time.Second)
				err := hc.HealthCheck(checkCtx)
				checkCancel()

				a.healthMu.Lock()
				a.health[name] = err
				a.healthMu.Unlock()

				if err != nil {
					a.logger.Warn("health check failed", "service", name, "error", err)
					if entry.config.Critical {
						a.logger.Error("critical service unhealthy, initiating shutdown")
						a.rootCancel()
					}
				}
			}
			a.mu.Unlock()
		}
	}
}
```
</details>

<details>
<summary>Hint 6: Example Services</summary>

```go
// HTTPService wraps an http.Server as a Service
type HTTPService struct {
	server *http.Server
	name   string
}

func (s *HTTPService) Name() string { return s.name }

func (s *HTTPService) Start(ctx context.Context) error {
	go func() {
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			// Handle error
		}
	}()
	return nil
}

func (s *HTTPService) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *HTTPService) HealthCheck(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost"+s.server.Addr+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("unhealthy: status %d", resp.StatusCode)
	}
	return nil
}

func (s *HTTPService) Dependencies() []string { return nil }

// WorkerService runs a background task
type WorkerService struct {
	name     string
	work     func(ctx context.Context) error
	done     chan struct{}
	deps     []string
}

func (w *WorkerService) Name() string { return w.name }

func (w *WorkerService) Start(ctx context.Context) error {
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		w.work(ctx)
	}()
	return nil
}

func (w *WorkerService) Stop(ctx context.Context) error {
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *WorkerService) Dependencies() []string { return w.deps }
```
</details>

<details>
<summary>Hint 7: Request Context Middleware</summary>

```go
type ctxKey int
const (
	requestIDKey ctxKey = iota
	traceIDKey
)

func requestContextMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Add request ID
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = generateID()
			}
			ctx = context.WithValue(ctx, requestIDKey, reqID)

			// Add trace ID
			traceID := r.Header.Get("X-Trace-ID")
			if traceID == "" {
				traceID = generateID()
			}
			ctx = context.WithValue(ctx, traceIDKey, traceID)

			// Add per-request timeout
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			w.Header().Set("X-Request-ID", reqID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```
</details>

## Success Criteria

1. Services start in dependency order and stop in reverse order
2. A cycle in service dependencies is detected at registration time and returns a clear error
3. If a critical service fails to start, all previously started services are stopped and the app exits with an error
4. If a critical service becomes unhealthy at runtime, the app initiates graceful shutdown
5. Non-critical service failures are logged but do not affect the application
6. SIGINT/SIGTERM triggers graceful shutdown: each service gets its configured stop timeout
7. A second SIGINT/SIGTERM forces immediate exit
8. The health endpoint returns aggregate status of all services as JSON
9. HTTP requests automatically get a request ID, trace ID, and per-request timeout in their context
10. `go run -race main.go` produces no race warnings
11. Startup and shutdown durations are measured and logged per service
12. The framework is testable: services can be started and stopped in unit tests

Demonstrate the framework with an example application that has:
- An HTTP API server (critical)
- A background metrics exporter (non-critical, depends on HTTP server)
- A background queue consumer (critical, no dependencies)
- A health check endpoint that aggregates all service health

Test with:

```bash
go run -race main.go
# In another terminal:
curl http://localhost:8080/health
curl http://localhost:8080/api/data
kill -INT $(pgrep -f "go run")
```

## Research Resources

- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [signal.NotifyContext](https://pkg.go.dev/os/signal#NotifyContext)
- [http.Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)
- [errgroup package](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [slog package](https://pkg.go.dev/log/slog)
- [Uber fx framework](https://github.com/uber-go/fx) (for inspiration, not to be used)
- [Topological Sorting](https://en.wikipedia.org/wiki/Topological_sorting)
- [Twelve-Factor App: Disposability](https://12factor.net/disposability)

## What's Next

Congratulations on completing Section 14. You now have deep understanding of `select` and `context` -- the two constructs that make Go's concurrency model practical for real-world systems.

Continue to [Section 15 - Sync Primitives](../../15-sync-primitives/01-sync-mutex/01-sync-mutex.md) to learn about mutexes, atomic operations, and other low-level synchronization tools.

## Summary

- A service framework manages the lifecycle (start, health, stop) of multiple components
- Context hierarchies propagate cancellation from OS signals through every layer
- Dependency ordering ensures services start in the right sequence and stop in reverse
- Critical services trigger app-wide shutdown on failure; non-critical services degrade gracefully
- Each service gets its own derived context for independent cancellation
- Health checks run on a timer with their own timeout context
- Request context enrichment (IDs, timeouts) is middleware, not application logic
- Thread safety across all shared state is mandatory
