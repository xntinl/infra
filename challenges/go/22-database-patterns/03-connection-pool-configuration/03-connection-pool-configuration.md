# 3. Connection Pool Configuration

<!--
difficulty: intermediate
concepts: [connection-pool, max-open-conns, max-idle-conns, conn-max-lifetime, db-stats]
tools: [go, sqlite]
estimated_time: 25m
bloom_level: apply
prerequisites: [database-sql-basics, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [01 - Database/SQL Basics](../01-database-sql-basics/01-database-sql-basics.md)
- Basic understanding of goroutines

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** `MaxOpenConns`, `MaxIdleConns`, and `ConnMaxLifetime` on a connection pool
- **Observe** pool behavior under concurrent load using `db.Stats()`
- **Diagnose** common pool misconfigurations

## Why Connection Pool Configuration

`database/sql` manages a pool of connections behind the scenes. The defaults are generous: unlimited open connections and two idle connections. In production, unlimited connections can overwhelm the database, while too few idle connections force frequent reconnections. Proper pool configuration prevents both connection storms and idle-connection waste.

## The Problem

Build a program that demonstrates connection pool behavior under concurrent load. Observe how pool settings affect throughput, wait times, and connection counts.

## Requirements

1. Configure `SetMaxOpenConns`, `SetMaxIdleConns`, and `SetConnMaxLifetime`
2. Run concurrent queries and observe pool statistics with `db.Stats()`
3. Demonstrate what happens when max connections are exhausted
4. Print pool stats periodically to observe behavior

## Step 1 -- Default Pool Behavior

```bash
mkdir -p ~/go-exercises/pool-config
cd ~/go-exercises/pool-config
go mod init pool-config
go get github.com/mattn/go-sqlite3
```

Create `main.go`:

```go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func printStats(label string, db *sql.DB) {
	stats := db.Stats()
	fmt.Printf("[%s] open=%d inUse=%d idle=%d waitCount=%d waitDuration=%s\n",
		label, stats.OpenConnections, stats.InUse, stats.Idle,
		stats.WaitCount, stats.WaitDuration)
}

func main() {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	for i := 0; i < 100; i++ {
		db.Exec("INSERT INTO test (value) VALUES (?)", fmt.Sprintf("row-%d", i))
	}

	printStats("before", db)

	// Run 20 concurrent queries with default pool settings
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var value string
			err := db.QueryRow("SELECT value FROM test WHERE id = ?", (id%100)+1).Scan(&value)
			if err != nil {
				log.Printf("query %d: %v", id, err)
			}
			time.Sleep(50 * time.Millisecond) // simulate processing
		}(i)
	}

	time.Sleep(10 * time.Millisecond)
	printStats("during", db)

	wg.Wait()
	printStats("after", db)
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Observe the pool stats -- with defaults, all 20 connections may open simultaneously.

## Step 2 -- Limit Open Connections

Add pool configuration before the concurrent queries:

```go
db.SetMaxOpenConns(5)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
db.SetConnMaxIdleTime(1 * time.Minute)
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Now `open` should never exceed 5. `waitCount` shows how many goroutines had to wait for a connection.

## Step 3 -- Observe Pool Under Stress

Add a stress test that prints stats periodically:

```go
func stressTest(db *sql.DB, concurrency int, duration time.Duration) {
	var (
		wg        sync.WaitGroup
		completed int64
		mu        sync.Mutex
	)

	deadline := time.After(duration)
	done := make(chan struct{})

	// Stats printer
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				c := completed
				mu.Unlock()
				stats := db.Stats()
				fmt.Printf("  completed=%d open=%d inUse=%d idle=%d waits=%d\n",
					c, stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount)
			case <-done:
				return
			}
		}
	}()

	// Workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-deadline:
					return
				default:
					var v string
					db.QueryRow("SELECT value FROM test WHERE id = ?", 1).Scan(&v)
					mu.Lock()
					completed++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	close(done)

	mu.Lock()
	fmt.Printf("total queries: %d\n", completed)
	mu.Unlock()
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

## Step 4 -- Compare Configurations

Try different pool settings and compare throughput:

```go
func main() {
	configs := []struct {
		name     string
		maxOpen  int
		maxIdle  int
	}{
		{"tiny (1 conn)", 1, 1},
		{"small (5 conns)", 5, 5},
		{"medium (10 conns)", 10, 10},
	}

	for _, cfg := range configs {
		db, _ := sql.Open("sqlite3", "file::memory:?cache=shared")
		db.Exec("CREATE TABLE IF NOT EXISTS test (id INTEGER PRIMARY KEY, value TEXT)")
		db.Exec("INSERT OR IGNORE INTO test (id, value) VALUES (1, 'hello')")

		db.SetMaxOpenConns(cfg.maxOpen)
		db.SetMaxIdleConns(cfg.maxIdle)

		fmt.Printf("\n--- %s ---\n", cfg.name)
		stressTest(db, 20, 2*time.Second)
		db.Close()
	}
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Compare the total queries completed under each configuration.

## Common Mistakes

### MaxIdleConns Greater Than MaxOpenConns

**Wrong:**

```go
db.SetMaxOpenConns(5)
db.SetMaxIdleConns(10) // idle > open makes no sense
```

**What happens:** Go silently caps idle at open. The extra idle setting is ignored.

**Fix:** Set `MaxIdleConns <= MaxOpenConns`.

### Zero ConnMaxLifetime in Production

**What happens:** Connections stay open forever. If the database restarts or a load balancer rotates connections, stale connections cause errors.

**Fix:** Set `ConnMaxLifetime` to a value shorter than your database or load balancer timeout (e.g., 5 minutes).

## Verification

```bash
CGO_ENABLED=1 go run main.go
```

## What's Next

Continue to [04 - Prepared Statements](../04-prepared-statements/04-prepared-statements.md) to learn when and how to use prepared statements.

## Summary

- `database/sql` manages a connection pool automatically
- `SetMaxOpenConns` limits total connections to the database
- `SetMaxIdleConns` controls how many connections stay open when idle
- `SetConnMaxLifetime` forces connection recycling to prevent stale connections
- `db.Stats()` returns pool metrics: open, in-use, idle, wait count, wait duration
- For production: set all four pool parameters; defaults are rarely optimal

## Reference

- [sql.DB.SetMaxOpenConns](https://pkg.go.dev/database/sql#DB.SetMaxOpenConns)
- [sql.DBStats](https://pkg.go.dev/database/sql#DBStats)
- [Go database/sql: connection pool](https://go.dev/doc/database/manage-connections)
