<!--
difficulty: advanced
concepts: feature-flags, runtime-toggle, percentage-rollout, sync-atomic, hot-reload
tools: sync, sync/atomic, encoding/json, net/http, os/signal
estimated_time: 35m
bloom_level: applying
prerequisites: concurrency-basics, maps, http-server-basics, json
-->

# Exercise 30.3: Feature Flags

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Concurrency primitives (`sync.RWMutex`, `sync/atomic`)
- Maps and JSON encoding/decoding
- HTTP server basics
- File I/O

## Learning Objectives

By the end of this exercise, you will be able to:

1. Build a thread-safe feature flag system with runtime toggling
2. Implement percentage-based rollouts using consistent hashing
3. Reload flag configuration from a file without restarting the service
4. Expose an admin HTTP API for querying and updating flag state

## Why This Matters

Feature flags decouple deployment from release. They let you ship code behind a flag, enable it for a percentage of users, and kill it instantly if something goes wrong -- all without a new deploy. Every mature production system uses feature flags for safe rollouts, A/B testing, and operational kill switches.

---

## Problem

Build a feature flag library and an HTTP service that demonstrates its use. The system must support:

1. Boolean flags (on/off)
2. Percentage rollout flags (enabled for N% of users based on user ID)
3. Thread-safe reads and writes (many goroutines checking flags concurrently)
4. Hot reload from a JSON config file on SIGHUP
5. An admin HTTP API to list, enable, disable, and update flags

### Hints

- Use `sync.RWMutex` to protect the flag map: many concurrent readers, rare writers
- For percentage rollouts, hash the user ID with `crc32.ChecksumIEEE` and check if `hash % 100 < percentage`
- `signal.Notify` with `syscall.SIGHUP` triggers a config reload
- Separate the flag store (library) from the HTTP handlers (application layer)

### Step 1: Create the project

```bash
mkdir -p feature-flags && cd feature-flags
go mod init feature-flags
```

### Step 2: Build the flag store

Create `flags.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"sync"
)

type FlagType string

const (
	FlagBoolean    FlagType = "boolean"
	FlagPercentage FlagType = "percentage"
)

type Flag struct {
	Name       string   `json:"name"`
	Type       FlagType `json:"type"`
	Enabled    bool     `json:"enabled"`
	Percentage int      `json:"percentage,omitempty"` // 0-100
}

type FlagStore struct {
	mu    sync.RWMutex
	flags map[string]*Flag
}

func NewFlagStore() *FlagStore {
	return &FlagStore{flags: make(map[string]*Flag)}
}

func (s *FlagStore) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read flags file: %w", err)
	}
	var flags []*Flag
	if err := json.Unmarshal(data, &flags); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags = make(map[string]*Flag, len(flags))
	for _, f := range flags {
		s.flags[f.Name] = f
	}
	return nil
}

// IsEnabled checks if a boolean flag is on.
func (s *FlagStore) IsEnabled(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flags[name]
	if !ok {
		return false
	}
	return f.Enabled
}

// IsEnabledFor checks if a percentage flag is on for a specific user.
func (s *FlagStore) IsEnabledFor(name, userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flags[name]
	if !ok {
		return false
	}
	if !f.Enabled {
		return false
	}
	if f.Type == FlagBoolean {
		return true
	}
	hash := crc32.ChecksumIEEE([]byte(userID))
	return int(hash%100) < f.Percentage
}

// SetEnabled toggles a boolean flag.
func (s *FlagStore) SetEnabled(name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flags[name]
	if !ok {
		return fmt.Errorf("flag %q not found", name)
	}
	f.Enabled = enabled
	return nil
}

// SetPercentage updates a percentage rollout flag.
func (s *FlagStore) SetPercentage(name string, pct int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flags[name]
	if !ok {
		return fmt.Errorf("flag %q not found", name)
	}
	if pct < 0 || pct > 100 {
		return fmt.Errorf("percentage must be 0-100, got %d", pct)
	}
	f.Percentage = pct
	return nil
}

// All returns a snapshot of all flags.
func (s *FlagStore) All() []*Flag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Flag, 0, len(s.flags))
	for _, f := range s.flags {
		copy := *f
		result = append(result, &copy)
	}
	return result
}
```

### Step 3: Write the HTTP server

Create `main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	store := NewFlagStore()

	flagsFile := "flags.json"
	if v := os.Getenv("FLAGS_FILE"); v != "" {
		flagsFile = v
	}

	if err := store.LoadFromFile(flagsFile); err != nil {
		log.Fatalf("Failed to load flags: %v", err)
	}
	log.Printf("Loaded %d flags from %s", len(store.All()), flagsFile)

	// Reload on SIGHUP
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			log.Println("SIGHUP received, reloading flags...")
			if err := store.LoadFromFile(flagsFile); err != nil {
				log.Printf("Reload failed: %v", err)
			} else {
				log.Printf("Reloaded %d flags", len(store.All()))
			}
		}
	}()

	mux := http.NewServeMux()

	// List all flags
	mux.HandleFunc("GET /flags", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(store.All())
	})

	// Check a flag for a user
	mux.HandleFunc("GET /flags/check", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		userID := r.URL.Query().Get("user_id")
		if name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		var enabled bool
		if userID != "" {
			enabled = store.IsEnabledFor(name, userID)
		} else {
			enabled = store.IsEnabled(name)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"flag":    name,
			"user_id": userID,
			"enabled": enabled,
		})
	})

	// Toggle a flag
	mux.HandleFunc("POST /flags/toggle", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.SetEnabled(req.Name, req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, "flag %q set to %v\n", req.Name, req.Enabled)
	})

	// Demo endpoint that uses flags
	mux.HandleFunc("GET /api/greeting", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user_id")
		greeting := "Hello"
		if store.IsEnabledFor("new-greeting", userID) {
			greeting = "Hey there"
		}
		extra := ""
		if store.IsEnabled("show-banner") {
			extra = " [NEW FEATURE AVAILABLE]"
		}
		fmt.Fprintf(w, "%s, user %s!%s\n", greeting, userID, extra)
	})

	log.Println("Server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

### Step 4: Create the flags config

Create `flags.json`:

```json
[
  {
    "name": "new-greeting",
    "type": "percentage",
    "enabled": true,
    "percentage": 50
  },
  {
    "name": "show-banner",
    "type": "boolean",
    "enabled": false
  },
  {
    "name": "dark-mode",
    "type": "percentage",
    "enabled": true,
    "percentage": 10
  }
]
```

### Step 5: Test

```bash
go run . &
sleep 1

# List all flags
curl -s localhost:8080/flags | jq .

# Check flag for different users
curl -s "localhost:8080/flags/check?name=new-greeting&user_id=alice"
curl -s "localhost:8080/flags/check?name=new-greeting&user_id=bob"

# Toggle a flag
curl -s -X POST localhost:8080/flags/toggle -d '{"name":"show-banner","enabled":true}'

# See it in action
curl -s "localhost:8080/api/greeting?user_id=alice"

kill %1
```

---

## Verify

```bash
go build -o flagserver . && ./flagserver &
sleep 1
curl -s localhost:8080/flags | jq 'length'
curl -s "localhost:8080/flags/check?name=dark-mode&user_id=test123" | jq .enabled
kill %1
```

The flags list should return 3 entries, and the dark-mode check should return a boolean.

---

## What's Next

In the next exercise, you will build health check endpoints that report the readiness and liveness of your service and its dependencies.

## Summary

- Feature flags decouple deployment from release, enabling safe rollouts
- `sync.RWMutex` provides thread-safe concurrent reads with exclusive writes
- Percentage rollouts use consistent hashing on user IDs for deterministic bucket assignment
- SIGHUP-triggered config reload enables flag changes without restarts
- Separate the flag store (library) from HTTP handlers (application) for testability

## Reference

- [sync.RWMutex](https://pkg.go.dev/sync#RWMutex)
- [hash/crc32](https://pkg.go.dev/hash/crc32)
- [signal.Notify](https://pkg.go.dev/os/signal#Notify)
