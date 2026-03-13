# 25. Building a Test Suite for a Production Service

<!--
difficulty: insane
concepts: [test-architecture, integration-testing, test-containers, test-isolation, test-helpers, factory-functions, coverage-strategy, ci-optimization, test-layers]
tools: [go test, docker, testcontainers-go]
estimated_time: 90m
bloom_level: create
prerequisites: [all exercises 01-24 in this section]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-24 or equivalent experience with all Go testing facilities
- Docker installed (for integration test containers)
- Understanding of: unit tests, table-driven tests, subtests, httptest, mocking with interfaces, build tags, TestMain, golden files, race detector, coverage analysis

## The Challenge

You have learned every testing tool in Go's ecosystem. Now use them together. Build a comprehensive test suite for a realistic production service: a task management HTTP API backed by a database and a cache, with webhook notifications to external systems. The challenge is not the service implementation (a skeleton is provided) -- it is designing a test architecture that is fast, isolated, maintainable, and gives you confidence to deploy.

Real test suites balance competing concerns. Unit tests are fast but miss integration bugs. Integration tests catch real bugs but are slow and need infrastructure. End-to-end tests verify the full stack but are fragile. Your job is to design the layers, choose the right technique for each concern, wire up CI, and make the whole thing a pleasure to maintain.

## Requirements

1. **Define three test layers** separated by build tags:

```
go test ./...                        # unit only (< 5s, no deps)
go test -tags=integration ./...      # unit + integration (needs DB)
go test -tags=e2e ./...              # unit + integration + e2e (full stack)
```

2. **Implement the service skeleton** with clear boundaries for testing:

```go
// domain.go -- pure business logic, no dependencies
package taskapi

import (
	"fmt"
	"time"
)

type TaskStatus string

const (
	StatusPending    TaskStatus = "pending"
	StatusInProgress TaskStatus = "in_progress"
	StatusDone       TaskStatus = "done"
)

type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      TaskStatus `json:"status"`
	Priority    int        `json:"priority"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateTaskInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Priority    int    `json:"priority"`
}

func (in CreateTaskInput) Validate() error {
	if in.Title == "" {
		return fmt.Errorf("title is required")
	}
	if len(in.Title) > 200 {
		return fmt.Errorf("title exceeds 200 characters")
	}
	if in.Priority < 1 || in.Priority > 5 {
		return fmt.Errorf("priority must be between 1 and 5")
	}
	return nil
}

type UpdateTaskInput struct {
	Title       *string     `json:"title,omitempty"`
	Description *string     `json:"description,omitempty"`
	Status      *TaskStatus `json:"status,omitempty"`
	Priority    *int        `json:"priority,omitempty"`
}

func (in UpdateTaskInput) Validate() error {
	if in.Title != nil && *in.Title == "" {
		return fmt.Errorf("title cannot be empty")
	}
	if in.Title != nil && len(*in.Title) > 200 {
		return fmt.Errorf("title exceeds 200 characters")
	}
	if in.Priority != nil && (*in.Priority < 1 || *in.Priority > 5) {
		return fmt.Errorf("priority must be between 1 and 5")
	}
	if in.Status != nil {
		switch *in.Status {
		case StatusPending, StatusInProgress, StatusDone:
		default:
			return fmt.Errorf("invalid status: %s", *in.Status)
		}
	}
	return nil
}
```

```go
// ports.go -- interfaces for external dependencies
package taskapi

import "context"

type TaskRepository interface {
	Create(ctx context.Context, task *Task) error
	GetByID(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context, limit, offset int) ([]*Task, error)
	Update(ctx context.Context, task *Task) error
	Delete(ctx context.Context, id string) error
}

type TaskCache interface {
	Get(ctx context.Context, id string) (*Task, error)
	Set(ctx context.Context, task *Task) error
	Invalidate(ctx context.Context, id string) error
}

type WebhookNotifier interface {
	Notify(ctx context.Context, event string, task *Task) error
}
```

```go
// service.go -- business logic orchestration
package taskapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo     TaskRepository
	cache    TaskCache
	notifier WebhookNotifier
	clock    func() time.Time
}

func NewService(repo TaskRepository, cache TaskCache, notifier WebhookNotifier) *Service {
	return &Service{
		repo:     repo,
		cache:    cache,
		notifier: notifier,
		clock:    time.Now,
	}
}

func (s *Service) CreateTask(ctx context.Context, input CreateTaskInput) (*Task, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}
	now := s.clock()
	task := &Task{
		ID:          uuid.New().String(),
		Title:       input.Title,
		Description: input.Description,
		Status:      StatusPending,
		Priority:    input.Priority,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}
	_ = s.cache.Set(ctx, task) // best-effort cache
	_ = s.notifier.Notify(ctx, "task.created", task)
	return task, nil
}

func (s *Service) GetTask(ctx context.Context, id string) (*Task, error) {
	// Try cache first
	if task, err := s.cache.Get(ctx, id); err == nil && task != nil {
		return task, nil
	}
	task, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task: %w", err)
	}
	_ = s.cache.Set(ctx, task) // populate cache
	return task, nil
}

func (s *Service) UpdateTask(ctx context.Context, id string, input UpdateTaskInput) (*Task, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}
	task, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task: %w", err)
	}
	if input.Title != nil {
		task.Title = *input.Title
	}
	if input.Description != nil {
		task.Description = *input.Description
	}
	if input.Status != nil {
		task.Status = *input.Status
	}
	if input.Priority != nil {
		task.Priority = *input.Priority
	}
	task.UpdatedAt = s.clock()
	if err := s.repo.Update(ctx, task); err != nil {
		return nil, fmt.Errorf("updating task: %w", err)
	}
	_ = s.cache.Invalidate(ctx, id)
	_ = s.notifier.Notify(ctx, "task.updated", task)
	return task, nil
}

func (s *Service) DeleteTask(ctx context.Context, id string) error {
	task, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("getting task: %w", err)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting task: %w", err)
	}
	_ = s.cache.Invalidate(ctx, id)
	_ = s.notifier.Notify(ctx, "task.deleted", task)
	return nil
}
```

3. **Unit tests** (no build tag, always run) -- test domain logic and service with mocks:

```go
// domain_test.go
package taskapi

import "testing"

func TestCreateTaskInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   CreateTaskInput
		wantErr bool
	}{
		{"valid", CreateTaskInput{Title: "Buy milk", Priority: 3}, false},
		{"empty title", CreateTaskInput{Title: "", Priority: 3}, true},
		{"title too long", CreateTaskInput{Title: string(make([]byte, 201)), Priority: 3}, true},
		{"priority too low", CreateTaskInput{Title: "Test", Priority: 0}, true},
		{"priority too high", CreateTaskInput{Title: "Test", Priority: 6}, true},
		{"min priority", CreateTaskInput{Title: "Test", Priority: 1}, false},
		{"max priority", CreateTaskInput{Title: "Test", Priority: 5}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateTaskInput_Validate(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }
	statusPtr := func(s TaskStatus) *TaskStatus { return &s }

	tests := []struct {
		name    string
		input   UpdateTaskInput
		wantErr bool
	}{
		{"empty update", UpdateTaskInput{}, false},
		{"valid title", UpdateTaskInput{Title: strPtr("New title")}, false},
		{"empty title", UpdateTaskInput{Title: strPtr("")}, true},
		{"valid status", UpdateTaskInput{Status: statusPtr(StatusDone)}, false},
		{"invalid status", UpdateTaskInput{Status: statusPtr("invalid")}, true},
		{"valid priority", UpdateTaskInput{Priority: intPtr(3)}, false},
		{"invalid priority", UpdateTaskInput{Priority: intPtr(0)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

```go
// mock_test.go -- mock implementations of ports
package taskapi

import (
	"context"
	"fmt"
	"sync"
)

type MockRepo struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

func NewMockRepo() *MockRepo {
	return &MockRepo{tasks: make(map[string]*Task)}
}

func (m *MockRepo) Create(_ context.Context, task *Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
	return nil
}

func (m *MockRepo) GetByID(_ context.Context, id string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	return task, nil
}

func (m *MockRepo) List(_ context.Context, limit, offset int) ([]*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tasks []*Task
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	if offset >= len(tasks) {
		return nil, nil
	}
	end := offset + limit
	if end > len(tasks) {
		end = len(tasks)
	}
	return tasks[offset:end], nil
}

func (m *MockRepo) Update(_ context.Context, task *Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[task.ID]; !ok {
		return fmt.Errorf("task not found: %s", task.ID)
	}
	m.tasks[task.ID] = task
	return nil
}

func (m *MockRepo) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[id]; !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	delete(m.tasks, id)
	return nil
}

type MockCache struct {
	mu    sync.RWMutex
	items map[string]*Task
}

func NewMockCache() *MockCache {
	return &MockCache{items: make(map[string]*Task)}
}

func (m *MockCache) Get(_ context.Context, id string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.items[id]
	if !ok {
		return nil, fmt.Errorf("cache miss")
	}
	return task, nil
}

func (m *MockCache) Set(_ context.Context, task *Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[task.ID] = task
	return nil
}

func (m *MockCache) Invalidate(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, id)
	return nil
}

type MockNotifier struct {
	mu     sync.Mutex
	events []NotifyCall
}

type NotifyCall struct {
	Event string
	Task  *Task
}

func NewMockNotifier() *MockNotifier {
	return &MockNotifier{}
}

func (m *MockNotifier) Notify(_ context.Context, event string, task *Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, NotifyCall{Event: event, Task: task})
	return nil
}

func (m *MockNotifier) Calls() []NotifyCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]NotifyCall, len(m.events))
	copy(out, m.events)
	return out
}
```

```go
// service_test.go
package taskapi

import (
	"context"
	"testing"
	"time"
)

func newTestService() (*Service, *MockRepo, *MockCache, *MockNotifier) {
	repo := NewMockRepo()
	cache := NewMockCache()
	notifier := NewMockNotifier()
	svc := NewService(repo, cache, notifier)
	svc.clock = func() time.Time {
		return time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	}
	return svc, repo, cache, notifier
}

func TestService_CreateTask(t *testing.T) {
	svc, _, cache, notifier := newTestService()
	ctx := context.Background()

	task, err := svc.CreateTask(ctx, CreateTaskInput{
		Title:    "Write tests",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Title != "Write tests" {
		t.Errorf("Title = %q, want %q", task.Title, "Write tests")
	}
	if task.Status != StatusPending {
		t.Errorf("Status = %q, want %q", task.Status, StatusPending)
	}
	if task.Priority != 2 {
		t.Errorf("Priority = %d, want 2", task.Priority)
	}

	// Verify cache was populated
	cached, err := cache.Get(ctx, task.ID)
	if err != nil {
		t.Errorf("task should be in cache: %v", err)
	}
	if cached.ID != task.ID {
		t.Errorf("cached task ID mismatch")
	}

	// Verify notification was sent
	calls := notifier.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(calls))
	}
	if calls[0].Event != "task.created" {
		t.Errorf("event = %q, want %q", calls[0].Event, "task.created")
	}
}

func TestService_CreateTask_ValidationError(t *testing.T) {
	svc, _, _, _ := newTestService()
	_, err := svc.CreateTask(context.Background(), CreateTaskInput{
		Title:    "",
		Priority: 3,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestService_GetTask_CacheHit(t *testing.T) {
	svc, _, cache, _ := newTestService()
	ctx := context.Background()

	// Pre-populate cache
	expected := &Task{ID: "cached-id", Title: "From Cache", Status: StatusPending}
	cache.Set(ctx, expected)

	task, err := svc.GetTask(ctx, "cached-id")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Title != "From Cache" {
		t.Errorf("Title = %q, want %q", task.Title, "From Cache")
	}
}

func TestService_GetTask_CacheMiss(t *testing.T) {
	svc, repo, _, _ := newTestService()
	ctx := context.Background()

	// Put in repo only (not cache)
	expected := &Task{ID: "repo-id", Title: "From Repo", Status: StatusDone}
	repo.Create(ctx, expected)

	task, err := svc.GetTask(ctx, "repo-id")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Title != "From Repo" {
		t.Errorf("Title = %q, want %q", task.Title, "From Repo")
	}
}

func TestService_UpdateTask(t *testing.T) {
	svc, _, _, notifier := newTestService()
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, CreateTaskInput{Title: "Original", Priority: 1})
	newTitle := "Updated"
	newStatus := StatusInProgress

	updated, err := svc.UpdateTask(ctx, task.ID, UpdateTaskInput{
		Title:  &newTitle,
		Status: &newStatus,
	})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if updated.Title != "Updated" {
		t.Errorf("Title = %q, want %q", updated.Title, "Updated")
	}
	if updated.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", updated.Status, StatusInProgress)
	}

	calls := notifier.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(calls))
	}
	if calls[1].Event != "task.updated" {
		t.Errorf("event = %q, want %q", calls[1].Event, "task.updated")
	}
}

func TestService_DeleteTask(t *testing.T) {
	svc, repo, _, notifier := newTestService()
	ctx := context.Background()

	task, _ := svc.CreateTask(ctx, CreateTaskInput{Title: "Delete me", Priority: 1})

	if err := svc.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// Verify deleted from repo
	_, err := repo.GetByID(ctx, task.ID)
	if err == nil {
		t.Error("task should be deleted from repo")
	}

	calls := notifier.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 notifications (create+delete), got %d", len(calls))
	}
	if calls[1].Event != "task.deleted" {
		t.Errorf("event = %q, want %q", calls[1].Event, "task.deleted")
	}
}
```

4. **Test helper factories** for creating test data:

```go
// testhelpers_test.go
package taskapi

import "time"

type TaskOption func(*Task)

func WithTitle(title string) TaskOption   { return func(t *Task) { t.Title = title } }
func WithStatus(s TaskStatus) TaskOption  { return func(t *Task) { t.Status = s } }
func WithPriority(p int) TaskOption       { return func(t *Task) { t.Priority = p } }

func NewTestTask(opts ...TaskOption) *Task {
	task := &Task{
		ID:        "test-id",
		Title:     "Test Task",
		Status:    StatusPending,
		Priority:  3,
		CreatedAt: time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
	}
	for _, opt := range opts {
		opt(task)
	}
	return task
}
```

5. **HTTP handler tests** using `httptest`:

```go
// handler_test.go
package taskapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Assume a Handler function exists:
// func NewHandler(svc *Service) http.Handler

func TestHandler_CreateTask_Success(t *testing.T) {
	svc, _, _, _ := newTestService()
	handler := NewHandler(svc) // you implement this

	body := `{"title":"Handler test","priority":2}`
	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	var task Task
	if err := json.NewDecoder(rec.Body).Decode(&task); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if task.Title != "Handler test" {
		t.Errorf("Title = %q, want %q", task.Title, "Handler test")
	}
}

func TestHandler_CreateTask_BadRequest(t *testing.T) {
	svc, _, _, _ := newTestService()
	handler := NewHandler(svc)

	body := `{"title":"","priority":0}`
	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
```

6. **Integration tests** (behind build tag) using TestMain for shared database:

```go
// integration_test.go
//go:build integration

package taskapi

import (
	"context"
	"log"
	"os"
	"testing"
)

var testDB *PostgresRepo // your real DB implementation

func TestMain(m *testing.M) {
	// Start a test database container (testcontainers-go or env var)
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		log.Fatal("TEST_DATABASE_URL is required for integration tests")
	}

	var err error
	testDB, err = NewPostgresRepo(dsn)
	if err != nil {
		log.Fatalf("connecting to test DB: %v", err)
	}

	// Run migrations
	if err := testDB.Migrate(); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	code := m.Run()

	testDB.Close()
	os.Exit(code)
}

func TestIntegration_CRUD(t *testing.T) {
	ctx := context.Background()

	// Use a transaction that rolls back for isolation
	tx, cleanup := testDB.BeginTestTx(t)
	defer cleanup()

	repo := testDB.WithTx(tx)

	task := NewTestTask(WithTitle("Integration task"))
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "Integration task" {
		t.Errorf("Title = %q, want %q", got.Title, "Integration task")
	}

	if err := repo.Delete(ctx, task.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
```

7. **E2E tests** (behind e2e build tag) testing the full stack:

```go
// e2e_test.go
//go:build e2e

package taskapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestE2E_FullLifecycle(t *testing.T) {
	// Wire up the full stack: real DB, real cache, mock webhook
	webhookCalls := make([]string, 0)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		webhookCalls = append(webhookCalls, payload["event"].(string))
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	// Create service with real deps + webhook mock
	// svc := buildFullService(testDB, redisClient, webhookServer.URL)
	// handler := NewHandler(svc)
	// server := httptest.NewServer(handler)
	// defer server.Close()

	// Test: Create -> Get -> Update -> Delete
	// Each step verifies HTTP status, response body, and side effects

	t.Run("create", func(t *testing.T) {
		// POST /tasks with valid body -> 201
	})
	t.Run("get", func(t *testing.T) {
		// GET /tasks/{id} -> 200 with correct body
	})
	t.Run("update", func(t *testing.T) {
		// PATCH /tasks/{id} -> 200 with updated fields
	})
	t.Run("delete", func(t *testing.T) {
		// DELETE /tasks/{id} -> 204
	})
	t.Run("verify webhooks", func(t *testing.T) {
		// Verify webhook received: created, updated, deleted
		if len(webhookCalls) < 3 {
			t.Errorf("expected 3 webhook calls, got %d", len(webhookCalls))
		}
	})

	_ = fmt.Sprintf("placeholder") // remove when implementing
}
```

8. **Coverage strategy** with a Makefile:

```makefile
.PHONY: test test-integration test-e2e test-coverage

test:
	go test -race -count=1 ./...

test-integration:
	go test -race -tags=integration -count=1 ./...

test-e2e:
	go test -race -tags=e2e -count=1 ./...

test-coverage:
	go test -coverprofile=unit.out -covermode=atomic ./...
	go test -tags=integration -coverprofile=integration.out -covermode=atomic ./...
	# Merge profiles (requires gocovmerge)
	gocovmerge unit.out integration.out > combined.out
	go tool cover -func=combined.out
	go tool cover -html=combined.out -o coverage.html
```

## Hints

- The mock implementations in `mock_test.go` live in `_test.go` files so they are only compiled during testing. They implement the same interfaces as real dependencies.
- Factory functions (`NewTestTask(opts...)`) with functional options reduce boilerplate across tests. Define them once in a test helper file.
- Per-test transaction isolation: begin a transaction in test setup, pass it to the repository, roll back in `t.Cleanup`. Each test gets a clean database view.
- `testcontainers-go` can start PostgreSQL and Redis containers in `TestMain`. The first run downloads images; subsequent runs reuse them.
- For CI, run unit tests on every commit (fast), integration tests on PRs (need Docker), and E2E tests on merge to main (full infrastructure).
- Use `-count=1` for integration and E2E tests to disable test caching. Cached integration tests hide real failures.
- Flaky test detection: `go test -count=10 -race ./...` -- if any run fails, you have a flaky test. Fix it before it erodes trust in the suite.
- Test assertion helpers (`AssertStatus`, `AssertJSON`) reduce noise and make test failures more readable.

## Success Criteria

1. `go test ./...` completes in under 5 seconds with zero external dependencies
2. `go test -tags=integration ./...` starts infrastructure, runs tests, tears down -- fully automated
3. `go test -tags=e2e ./...` tests the full request lifecycle including cache population and webhook delivery
4. `go test -race -count=10 ./...` produces no failures and no data races
5. `go tool cover -func` shows 80%+ coverage on business logic (`domain.go`, `service.go`)
6. Adding a new endpoint requires tests in exactly one file per layer (unit, integration, e2e)
7. Mock implementations are verified against the same interface contracts as real implementations
8. Test helpers and factories are reusable across all test files

## Research Resources

- [testcontainers-go](https://golang.testcontainers.org/) -- Docker containers for integration tests
- [Go Test Coverage](https://go.dev/blog/cover) -- coverage profiling and visualization
- [Mitchell Hashimoto: Advanced Testing with Go (GopherCon 2017)](https://www.youtube.com/watch?v=8hQG7QlcLBk) -- test architecture patterns
- [httptest package](https://pkg.go.dev/net/http/httptest) -- testing HTTP servers and clients
- [google/go-cmp](https://pkg.go.dev/github.com/google/go-cmp/cmp) -- rich comparison for test assertions
- [gocovmerge](https://github.com/wadey/gocovmerge) -- merging coverage profiles from multiple test runs
- [Mat Ryer: Testing in Go (blog series)](https://medium.com/@matryer) -- practical testing patterns

## What's Next

Congratulations -- you have completed the testing ecosystem section. You now have the full Go testing toolkit: unit tests, table-driven tests, subtests, test helpers, benchmarks, fuzz testing, golden files, coverage analysis, race detection, property-based testing, and production-grade test architecture. The next section covers **goroutines and channels** -- the foundation of Go's concurrency model.

## Summary

- Production test suites have layers: unit (fast, no deps), integration (real infra), e2e (full stack)
- Separate layers with build tags (`//go:build integration`) so `go test ./...` stays fast
- Use interfaces and mocks for unit tests; real infrastructure for integration tests
- `TestMain` manages expensive shared resources (databases, containers)
- Per-test transaction rollback provides isolation without recreating the database
- Factory functions with functional options reduce test boilerplate
- Coverage merging across layers gives a complete picture
- CI pipeline: unit tests on every commit, integration on PR, e2e on merge
- `-race`, `-count=10`, and `-coverprofile` are your CI flags
