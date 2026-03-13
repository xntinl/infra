# 23. Snapshot/Approval Testing

<!--
difficulty: advanced
concepts: [snapshot-testing, approval-testing, testdata, go-cmp, update-snapshots, structured-diff, serialization-testing]
tools: [go test, go-cmp]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-your-first-test, 07-test-fixtures-and-testdata, 19-golden-file-testing]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with golden file patterns from exercise 19
- Understanding of `testdata/` conventions

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a snapshot testing system that captures structured output and compares against approved baselines
- **Use** `go-cmp` to produce readable diffs when snapshots diverge
- **Apply** snapshot testing to struct serialization, API responses, and configuration output
- **Evaluate** when snapshot testing adds value versus when inline assertions are better

## The Problem

Golden file testing (exercise 19) works well for string output. But many tests need to verify structured data -- Go structs, nested JSON objects, configuration trees. Snapshot/approval testing extends the golden file concept to structured values: serialize the value, store the serialization, and compare on subsequent runs. When the structure intentionally changes, you "approve" the new snapshot.

Build a snapshot testing framework that handles structs, produces readable diffs, and supports an approval workflow.

## Requirements

1. **Create a snapshot helper** using `go-cmp` for structured comparison:

```go
// snapshot.go
package snapshot

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var approve = flag.Bool("approve", false, "approve and update snapshots")

// Match serializes got to JSON and compares against the snapshot file.
// If -approve is set, overwrites the snapshot with the new value.
func Match(t *testing.T, name string, got any) {
	t.Helper()

	gotBytes, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshaling snapshot: %v", err)
	}
	gotBytes = append(gotBytes, '\n')

	snapshotPath := filepath.Join("testdata", "snapshots", name+".snap.json")

	if *approve {
		dir := filepath.Dir(snapshotPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating snapshot dir: %v", err)
		}
		if err := os.WriteFile(snapshotPath, gotBytes, 0o644); err != nil {
			t.Fatalf("writing snapshot: %v", err)
		}
		t.Logf("approved snapshot: %s", snapshotPath)
		return
	}

	wantBytes, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("reading snapshot %s: %v\nRun with -approve to create it.", snapshotPath, err)
	}

	// Unmarshal both to compare structurally (ignoring whitespace differences)
	var gotVal, wantVal any
	if err := json.Unmarshal(gotBytes, &gotVal); err != nil {
		t.Fatalf("unmarshaling got: %v", err)
	}
	if err := json.Unmarshal(wantBytes, &wantVal); err != nil {
		t.Fatalf("unmarshaling snapshot: %v", err)
	}

	if diff := cmp.Diff(wantVal, gotVal); diff != "" {
		t.Errorf("snapshot mismatch for %s (-want +got):\n%s\nRun with -approve to update.",
			name, diff)
	}
}

// MatchString compares a string value against a text snapshot.
func MatchString(t *testing.T, name string, got string) {
	t.Helper()

	snapshotPath := filepath.Join("testdata", "snapshots", name+".snap.txt")

	if *approve {
		dir := filepath.Dir(snapshotPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating snapshot dir: %v", err)
		}
		if err := os.WriteFile(snapshotPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing snapshot: %v", err)
		}
		t.Logf("approved snapshot: %s", snapshotPath)
		return
	}

	wantBytes, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("reading snapshot %s: %v\nRun with -approve to create it.", snapshotPath, err)
	}

	if diff := cmp.Diff(string(wantBytes), got); diff != "" {
		t.Errorf("snapshot mismatch for %s (-want +got):\n%s", name, diff)
	}
}
```

2. **Create domain types** with complex structure:

```go
// config.go
package snapshot

import "time"

type ServiceConfig struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Environment string            `json:"environment"`
	Server      ServerConfig      `json:"server"`
	Database    DatabaseConfig    `json:"database"`
	Features    map[string]bool   `json:"features"`
	Metadata    map[string]string `json:"metadata"`
}

type ServerConfig struct {
	Host         string        `json:"host"`
	Port         int           `json:"port"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	TLS          *TLSConfig    `json:"tls,omitempty"`
}

type TLSConfig struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

type DatabaseConfig struct {
	Driver          string `json:"driver"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Name            string `json:"name"`
	MaxOpenConns    int    `json:"max_open_conns"`
	MaxIdleConns    int    `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
}

// APIResponse represents a paginated API response.
type APIResponse struct {
	Data       []Item     `json:"data"`
	Pagination Pagination `json:"pagination"`
	Links      Links      `json:"links"`
}

type Item struct {
	ID    string            `json:"id"`
	Type  string            `json:"type"`
	Attrs map[string]string `json:"attributes"`
}

type Pagination struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type Links struct {
	Self  string `json:"self"`
	Next  string `json:"next,omitempty"`
	Prev  string `json:"prev,omitempty"`
	First string `json:"first"`
	Last  string `json:"last"`
}
```

3. **Write snapshot tests** for the domain types:

```go
// snapshot_test.go
package snapshot

import (
	"testing"
	"time"
)

func TestServiceConfig_Production(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "order-service",
		Version:     "2.1.0",
		Environment: "production",
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8443,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			TLS: &TLSConfig{
				CertFile: "/etc/ssl/certs/server.crt",
				KeyFile:  "/etc/ssl/private/server.key",
			},
		},
		Database: DatabaseConfig{
			Driver:          "postgres",
			Host:            "db.internal",
			Port:            5432,
			Name:            "orders",
			MaxOpenConns:    25,
			MaxIdleConns:    5,
			ConnMaxLifetime: 5 * time.Minute,
		},
		Features: map[string]bool{
			"new_checkout":   true,
			"beta_dashboard": false,
			"audit_logging":  true,
		},
		Metadata: map[string]string{
			"team":    "platform",
			"oncall":  "platform-oncall@company.com",
			"runbook": "https://wiki.internal/order-service",
		},
	}

	Match(t, "service_config_production", cfg)
}

func TestServiceConfig_Development(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "order-service",
		Version:     "dev",
		Environment: "development",
		Server: ServerConfig{
			Host:         "localhost",
			Port:         8080,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		},
		Database: DatabaseConfig{
			Driver:          "postgres",
			Host:            "localhost",
			Port:            5432,
			Name:            "orders_dev",
			MaxOpenConns:    5,
			MaxIdleConns:    2,
			ConnMaxLifetime: time.Minute,
		},
		Features: map[string]bool{
			"new_checkout":   true,
			"beta_dashboard": true,
			"audit_logging":  false,
		},
	}

	Match(t, "service_config_development", cfg)
}

func TestAPIResponse_FirstPage(t *testing.T) {
	resp := APIResponse{
		Data: []Item{
			{ID: "ord-001", Type: "order", Attrs: map[string]string{"status": "pending", "total": "99.99"}},
			{ID: "ord-002", Type: "order", Attrs: map[string]string{"status": "shipped", "total": "149.50"}},
			{ID: "ord-003", Type: "order", Attrs: map[string]string{"status": "delivered", "total": "29.99"}},
		},
		Pagination: Pagination{
			Page:       1,
			PerPage:    3,
			Total:      12,
			TotalPages: 4,
		},
		Links: Links{
			Self:  "/api/v1/orders?page=1",
			Next:  "/api/v1/orders?page=2",
			First: "/api/v1/orders?page=1",
			Last:  "/api/v1/orders?page=4",
		},
	}

	Match(t, "api_response_first_page", resp)
}

func TestAPIResponse_Empty(t *testing.T) {
	resp := APIResponse{
		Data: []Item{},
		Pagination: Pagination{
			Page:       1,
			PerPage:    10,
			Total:      0,
			TotalPages: 0,
		},
		Links: Links{
			Self:  "/api/v1/orders?page=1",
			First: "/api/v1/orders?page=1",
			Last:  "/api/v1/orders?page=1",
		},
	}

	Match(t, "api_response_empty", resp)
}
```

## Hints

- Install `go-cmp`: `go get github.com/google/go-cmp`. It produces human-readable diffs for any Go value.
- Commit snapshot files to version control. Reviewers can see exactly what changed in the data structure during code review.
- Avoid snapshots that include non-deterministic data (timestamps, UUIDs). Either inject fixed values in tests or use `go-cmp` options like `cmpopts.IgnoreFields` to exclude them.
- Snapshot testing shines when: (a) the output structure is complex enough that inline assertions are unwieldy, (b) you want code review visibility into output changes, (c) the output format evolves over time.
- Snapshot testing is a poor fit when: (a) the output is trivial (use inline assertions), (b) the output changes frequently (snapshot churn), (c) non-deterministic output cannot be stabilized.
- The `-approve` flag is a convention. Some teams use `-update`, `-record`, or `-golden`. Pick one and be consistent.

## Verification

```bash
# Install dependency
go get github.com/google/go-cmp

# Create initial snapshots
go test -approve

# Verify snapshots match
go test -v

# View a snapshot file
cat testdata/snapshots/service_config_production.snap.json

# Intentionally change a struct field and see the diff
go test -v
```

## What's Next

Continue to [24 - Property-Based Testing](../24-property-based-testing/24-property-based-testing.md) to learn how to use the `rapid` library for property-based testing that generates thousands of test cases automatically.

## Summary

- Snapshot testing serializes structured values and compares against stored baselines
- Use `-approve` flag to create or update snapshots when output intentionally changes
- `go-cmp` provides readable diffs showing exactly what changed
- Store snapshots in `testdata/snapshots/` and commit them to version control
- Structural comparison (unmarshal then diff) is more robust than byte-for-byte string matching
- Snapshot testing is ideal for complex, evolving data structures; use inline assertions for simple values

## Reference

- [go-cmp](https://pkg.go.dev/github.com/google/go-cmp/cmp)
- [go-cmp options](https://pkg.go.dev/github.com/google/go-cmp/cmp/cmpopts)
- [Testing in Go: Golden files and snapshot testing](https://www.youtube.com/watch?v=8hQG7QlcLBk)
