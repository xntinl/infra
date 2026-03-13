# 18. Integration Tests with Build Tags

<!--
difficulty: advanced
concepts: [build-tags, go-build-constraint, integration-testing, test-separation, short-flag, testing-short]
tools: [go test]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-your-first-test, 13-build-tags-for-test-separation, 17-testing-with-environment-variables]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of build constraints (`//go:build`)
- Familiarity with `go test -run`, `-short`, and `-v` flags

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a project structure that cleanly separates unit tests from integration tests
- **Apply** build tags to control which tests run in CI versus local development
- **Combine** build tags with `-short` and environment variable checks for layered test control
- **Evaluate** tradeoffs between build tags, `-short` flag, and naming conventions

## The Problem

Your team maintains a REST API client that talks to an external service. Unit tests use mocked HTTP responses and run in milliseconds. Integration tests hit a real test server and take seconds. You need a strategy so that:

1. `go test ./...` runs only unit tests (fast, no external dependencies)
2. `go test -tags=integration ./...` runs both unit and integration tests
3. Developers can run integration tests locally when the test server is available
4. CI runs integration tests only in a dedicated pipeline stage

Build a file-storage client with both unit and integration tests that demonstrates this separation.

## Requirements

1. **Create a `FileStore` client** that wraps HTTP calls to a file storage API:

```go
// store.go
package filestore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type FileStore struct {
	BaseURL    string
	HTTPClient *http.Client
}

type FileMetadata struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

func New(baseURL string) *FileStore {
	return &FileStore{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (fs *FileStore) Upload(name string, data []byte) (*FileMetadata, error) {
	url := fmt.Sprintf("%s/files/%s", fs.BaseURL, name)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := fs.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("uploading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload failed: %s: %s", resp.Status, body)
	}

	var meta FileMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &meta, nil
}

func (fs *FileStore) Download(name string) ([]byte, error) {
	url := fmt.Sprintf("%s/files/%s", fs.BaseURL, name)
	resp, err := fs.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("file not found: %s", name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func (fs *FileStore) Delete(name string) error {
	url := fmt.Sprintf("%s/files/%s", fs.BaseURL, name)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := fs.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("deleting: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete failed: %s", resp.Status)
	}
	return nil
}
```

2. **Create unit tests** (no build tag, always run) using `httptest.NewServer`:

```go
// store_test.go
package filestore

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpload_Unit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		meta := FileMetadata{
			Name:      "test.txt",
			Size:      int64(len(body)),
			CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(meta)
	}))
	defer server.Close()

	store := New(server.URL)
	meta, err := store.Upload("test.txt", []byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "test.txt" {
		t.Errorf("Name = %q, want %q", meta.Name, "test.txt")
	}
	if meta.Size != 5 {
		t.Errorf("Size = %d, want 5", meta.Size)
	}
}

func TestDownload_Unit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("file contents"))
	}))
	defer server.Close()

	store := New(server.URL)
	data, err := store.Download("test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "file contents" {
		t.Errorf("data = %q, want %q", data, "file contents")
	}
}

func TestDownload_NotFound_Unit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	store := New(server.URL)
	_, err := store.Download("missing.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

3. **Create integration tests** (behind a build tag) that hit a real server:

```go
// store_integration_test.go
//go:build integration

package filestore

import (
	"os"
	"testing"
)

func testServerURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("FILESTORE_URL")
	if url == "" {
		t.Skip("FILESTORE_URL not set, skipping integration test")
	}
	return url
}

func TestUploadDownloadDelete_Integration(t *testing.T) {
	baseURL := testServerURL(t)
	store := New(baseURL)

	// Upload
	content := []byte("integration test content")
	meta, err := store.Upload("integration-test.txt", content)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	t.Cleanup(func() {
		store.Delete("integration-test.txt")
	})

	if meta.Name != "integration-test.txt" {
		t.Errorf("Name = %q, want %q", meta.Name, "integration-test.txt")
	}

	// Download
	data, err := store.Download("integration-test.txt")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", data, content)
	}

	// Delete
	if err := store.Delete("integration-test.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	_, err = store.Download("integration-test.txt")
	if err == nil {
		t.Error("expected error downloading deleted file")
	}
}

func TestDownloadNotFound_Integration(t *testing.T) {
	baseURL := testServerURL(t)
	store := New(baseURL)

	_, err := store.Download("definitely-does-not-exist.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
```

4. **Create a hybrid approach** using `-short` for finer control:

```go
// store_slow_test.go
package filestore

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpload_SlowTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	// Simulate a server that responds slowly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"name":"slow.txt","size":0,"created_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	store := New(server.URL)
	store.HTTPClient.Timeout = 5 * time.Second
	_, err := store.Upload("slow.txt", []byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

## Hints

- The `//go:build integration` directive must be the first line of the file (before the package clause). A blank line separates it from the package declaration.
- `go test ./...` does NOT include files with custom build tags unless you pass `-tags=integration`.
- Combine tags with boolean logic: `//go:build integration && !ci` excludes CI environments.
- `t.Skip` with an environment variable check provides a second layer of protection: even if someone passes `-tags=integration`, the test skips if the server is not running.
- Run in CI: `go test -tags=integration -v ./... -count=1` (disable caching with `-count=1` for integration tests).
- The `-short` flag is orthogonal to build tags. Use it for "slow but not external" tests.

## Verification

Run unit tests only (default):

```bash
go test -v ./...
```

Run all tests including integration:

```bash
FILESTORE_URL=http://localhost:8080 go test -tags=integration -v ./...
```

Run quick tests only:

```bash
go test -v -short ./...
```

Confirm integration tests are skipped without the tag:

```bash
go test -v -list 'Integration' ./...
# Should list nothing -- integration test files are excluded by the build tag
```

## What's Next

Continue to [19 - Golden File Testing](../19-golden-file-testing/19-golden-file-testing.md) to learn how to test complex outputs by comparing against known-good reference files.

## Summary

- `//go:build integration` excludes a test file from `go test ./...` unless `-tags=integration` is passed
- Unit tests (no build tag) run by default and use mocks or `httptest`
- Integration tests (behind a tag) hit real external services
- Combine build tags with `t.Skip` and environment variable checks for defense in depth
- `-short` flag is a separate dimension: use it for slow-but-local tests
- In CI, run integration tests in a separate stage: `go test -tags=integration -count=1 ./...`

## Reference

- [Go build constraints](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
- [testing.Short](https://pkg.go.dev/testing#Short)
- [Go blog: Using build constraints](https://go.dev/blog/rebuild)
- [Peter Bourgon: Best practices for Industrial Programming](https://peter.bourgon.org/go-best-practices-2016/)
