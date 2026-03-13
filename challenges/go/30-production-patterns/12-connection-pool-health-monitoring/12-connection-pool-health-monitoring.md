<!--
difficulty: advanced
concepts: connection-pool, health-monitoring, database-sql, metrics, stale-connections
tools: database/sql, sync, time, context, net/http
estimated_time: 35m
bloom_level: applying
prerequisites: database-sql-basics, concurrency-basics, health-endpoints, interfaces
-->

# Exercise 30.12: Connection Pool Health Monitoring

## Prerequisites

Before starting this exercise, you should be comfortable with:

- `database/sql` connection pooling
- Concurrency primitives (`sync.Mutex`, goroutines)
- Health check endpoints (Exercise 30.4)
- Context and timeouts

## Learning Objectives

By the end of this exercise, you will be able to:

1. Configure `database/sql` connection pool parameters (max open, max idle, lifetime, idle timeout)
2. Monitor pool health metrics: open connections, in-use, idle, wait count, wait duration
3. Detect degraded pool states (exhaustion, high wait times, stale connections)
4. Expose pool metrics via HTTP for observability and alerting

## Why This Matters

The connection pool is one of the most critical resources in a database-backed service. An exhausted pool blocks all requests. Stale connections cause sporadic errors. A pool that is too large wastes database resources and can hit server-side limits. Monitoring pool health lets you right-size the pool, detect problems before they cause outages, and understand how your service uses its database connections.

---

## Problem

Build a service that monitors its database connection pool health, exposes metrics via HTTP, and raises alerts when the pool enters a degraded state.

### Hints

- `sql.DB.Stats()` returns `sql.DBStats` with all pool metrics
- Key metrics: `OpenConnections`, `InUse`, `Idle`, `WaitCount`, `WaitDuration`, `MaxOpenConnections`
- Set `SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`, `SetConnMaxIdleTime` for production tuning
- A utilization ratio above 80% (`InUse / MaxOpenConnections`) indicates the pool may need scaling
- High `WaitDuration / WaitCount` means requests are waiting too long for connections

### Step 1: Create the project

```bash
mkdir -p pool-health && cd pool-health
go mod init pool-health
go get github.com/mattn/go-sqlite3
```

### Step 2: Build the pool monitor

Create `pool.go`:

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

type PoolStatus string

const (
	PoolHealthy  PoolStatus = "healthy"
	PoolDegraded PoolStatus = "degraded"
	PoolCritical PoolStatus = "critical"
)

type PoolMetrics struct {
	MaxOpen        int           `json:"max_open"`
	Open           int           `json:"open"`
	InUse          int           `json:"in_use"`
	Idle           int           `json:"idle"`
	WaitCount      int64         `json:"wait_count"`
	WaitDuration   time.Duration `json:"wait_duration_ms"`
	Utilization    float64       `json:"utilization_pct"`
	AvgWait        time.Duration `json:"avg_wait_ms"`
	Status         PoolStatus    `json:"status"`
	MaxIdleTimeSec int           `json:"max_idle_time_sec"`
	MaxLifetimeSec int           `json:"max_lifetime_sec"`
}

type PoolMonitor struct {
	db *sql.DB

	mu          sync.RWMutex
	lastMetrics *PoolMetrics
	history     []PoolMetrics
	maxHistory  int

	thresholds PoolThresholds
}

type PoolThresholds struct {
	UtilizationWarn     float64       // warn if utilization exceeds this (0-1)
	UtilizationCritical float64       // critical if exceeds this
	AvgWaitWarn         time.Duration // warn if average wait exceeds this
	AvgWaitCritical     time.Duration
}

func DefaultThresholds() PoolThresholds {
	return PoolThresholds{
		UtilizationWarn:     0.7,
		UtilizationCritical: 0.9,
		AvgWaitWarn:         50 * time.Millisecond,
		AvgWaitCritical:     200 * time.Millisecond,
	}
}

func NewPoolMonitor(db *sql.DB, thresholds PoolThresholds) *PoolMonitor {
	return &PoolMonitor{
		db:         db,
		maxHistory: 60,
		thresholds: thresholds,
	}
}

func (pm *PoolMonitor) Collect() PoolMetrics {
	stats := pm.db.Stats()

	utilization := 0.0
	if stats.MaxOpenConnections > 0 {
		utilization = float64(stats.InUse) / float64(stats.MaxOpenConnections)
	}

	avgWait := time.Duration(0)
	if stats.WaitCount > 0 {
		avgWait = stats.WaitDuration / time.Duration(stats.WaitCount)
	}

	status := PoolHealthy
	if utilization > pm.thresholds.UtilizationCritical || avgWait > pm.thresholds.AvgWaitCritical {
		status = PoolCritical
	} else if utilization > pm.thresholds.UtilizationWarn || avgWait > pm.thresholds.AvgWaitWarn {
		status = PoolDegraded
	}

	metrics := PoolMetrics{
		MaxOpen:      stats.MaxOpenConnections,
		Open:         stats.OpenConnections,
		InUse:        stats.InUse,
		Idle:         stats.Idle,
		WaitCount:    stats.WaitCount,
		WaitDuration: stats.WaitDuration,
		Utilization:  utilization,
		AvgWait:      avgWait,
		Status:       status,
	}

	pm.mu.Lock()
	pm.lastMetrics = &metrics
	pm.history = append(pm.history, metrics)
	if len(pm.history) > pm.maxHistory {
		pm.history = pm.history[1:]
	}
	pm.mu.Unlock()

	return metrics
}

func (pm *PoolMonitor) LastMetrics() *PoolMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.lastMetrics
}

func (pm *PoolMonitor) History() []PoolMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make([]PoolMetrics, len(pm.history))
	copy(result, pm.history)
	return result
}

func (pm *PoolMonitor) StartCollecting(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m := pm.Collect()
				if m.Status != PoolHealthy {
					log.Printf("[POOL ALERT] status=%s utilization=%.1f%% avg_wait=%v open=%d/%d",
						m.Status, m.Utilization*100, m.AvgWait, m.InUse, m.MaxOpen)
				}
			}
		}
	}()
}

func (pm *PoolMonitor) Check(ctx context.Context) error {
	m := pm.Collect()
	if m.Status == PoolCritical {
		return fmt.Errorf("connection pool critical: utilization=%.0f%% avg_wait=%v",
			m.Utilization*100, m.AvgWait)
	}
	return pm.db.PingContext(ctx)
}
```

### Step 3: Build the demo service

Create `main.go`:

```go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Configure the pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	// Create a test table
	db.Exec("CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY, value TEXT)")
	for i := 0; i < 100; i++ {
		db.Exec("INSERT INTO items (value) VALUES (?)", fmt.Sprintf("item-%d", i))
	}

	// Start pool monitoring
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	monitor := NewPoolMonitor(db, DefaultThresholds())
	monitor.StartCollecting(ctx, 2*time.Second)

	mux := http.NewServeMux()

	// Query endpoint (simulates database work)
	mux.HandleFunc("GET /api/query", func(w http.ResponseWriter, r *http.Request) {
		delay := time.Duration(50+rand.Intn(200)) * time.Millisecond

		rows, err := db.QueryContext(r.Context(), "SELECT value FROM items ORDER BY RANDOM() LIMIT 5")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		time.Sleep(delay) // simulate processing

		var items []string
		for rows.Next() {
			var v string
			rows.Scan(&v)
			items = append(items, v)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items":    items,
			"delay_ms": delay.Milliseconds(),
		})
	})

	// Pool metrics endpoint
	mux.HandleFunc("GET /metrics/pool", func(w http.ResponseWriter, r *http.Request) {
		m := monitor.Collect()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m)
	})

	// Pool history endpoint
	mux.HandleFunc("GET /metrics/pool/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(monitor.History())
	})

	// Health check using pool status
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := monitor.Check(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// Load generator endpoint (for testing pool pressure)
	mux.HandleFunc("POST /test/load", func(w http.ResponseWriter, r *http.Request) {
		var wg sync.WaitGroup
		count := 20

		start := time.Now()
		for i := 0; i < count; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				rows, err := db.QueryContext(ctx, "SELECT value FROM items LIMIT 1")
				if err != nil {
					return
				}
				time.Sleep(time.Duration(200+rand.Intn(300)) * time.Millisecond)
				rows.Close()
			}()
		}
		wg.Wait()

		m := monitor.Collect()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"concurrent_queries": count,
			"elapsed_ms":        time.Since(start).Milliseconds(),
			"pool_status":       m.Status,
			"pool_utilization":  fmt.Sprintf("%.1f%%", m.Utilization*100),
		})
	})

	log.Println("Server on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

### Step 4: Test

```bash
go run . &
sleep 1

# Check pool metrics
curl -s localhost:8080/metrics/pool | jq .

# Generate load to see pool pressure
curl -s -X POST localhost:8080/test/load | jq .

# Check metrics after load
curl -s localhost:8080/metrics/pool | jq .

# Check readiness
curl -s localhost:8080/readyz | jq .

kill %1
```

---

## Verify

```bash
go build -o server . && ./server &
sleep 1
curl -s localhost:8080/metrics/pool | jq '.status'
kill %1
```

Should print `"healthy"` for a fresh, unloaded pool.

---

## What's Next

In the next exercise, you will implement panic recovery middleware for production HTTP services.

## Summary

- `sql.DB.Stats()` provides real-time connection pool metrics
- Monitor utilization (`InUse / MaxOpen`), wait count, and average wait duration
- Set thresholds for degraded and critical states to enable alerting
- Collect metrics periodically and maintain a history for trend analysis
- Integrate pool health into readiness probes to stop traffic when the pool is exhausted

## Reference

- [database/sql.DBStats](https://pkg.go.dev/database/sql#DBStats)
- [database/sql connection pool tuning](https://go.dev/doc/database/manage-connections)
- [Connection pool monitoring best practices](https://brandur.org/go-worker-pool)
