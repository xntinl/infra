# Exercise 18: Building a File Watcher

**Difficulty:** Insane | **Estimated Time:** 60 minutes | **Section:** 19 - I/O and Filesystem

## Challenge

Build a production-quality file watcher that monitors directories for changes, debounces rapid events, and executes configurable actions. This is the core of tools like `air` (Go hot reload), `nodemon`, and `watchexec`.

## Requirements

### Core Watcher

Implement a `Watcher` that:

```go
type Watcher struct {
    paths      []string
    patterns   []string          // glob patterns to include (e.g., "*.go")
    ignore     []string          // glob patterns to ignore (e.g., "vendor/*")
    debounce   time.Duration     // coalesce events within this window
    actions    []Action          // callbacks to execute on changes
    recursive  bool              // watch subdirectories
}

type Event struct {
    Path      string
    Op        Op               // Create, Write, Remove, Rename
    Timestamp time.Time
}

type Op int
const (
    Create Op = iota
    Write
    Remove
    Rename
)

type Action func(events []Event) error
```

### Functional Requirements

1. **Directory watching**: Monitor one or more directories for file changes using `github.com/fsnotify/fsnotify` as the underlying OS notification layer.

2. **Recursive watching**: When `recursive` is true, automatically watch all subdirectories. Watch newly created subdirectories. Stop watching deleted subdirectories.

3. **Pattern filtering**: Only fire actions for files matching the include patterns. Skip files matching ignore patterns. Support glob syntax (`*.go`, `internal/**/*.go`, `!*_test.go`).

4. **Event debouncing**: Saving a file in an IDE often generates multiple events (write, chmod, rename+create). Coalesce events within the debounce window and fire a single action with the batch.

5. **Action execution**: Execute registered actions with the batch of events. Actions run sequentially. If an action returns an error, log it but continue with the next action.

6. **Graceful shutdown**: Stop watching on context cancellation or SIGINT/SIGTERM. Drain pending events. Close all watchers.

### API

```go
func New(opts ...Option) *Watcher
func (w *Watcher) Watch(ctx context.Context) error
func (w *Watcher) OnChange(action Action)

// Functional options
func WithPaths(paths ...string) Option
func WithPatterns(patterns ...string) Option
func WithIgnore(patterns ...string) Option
func WithDebounce(d time.Duration) Option
func WithRecursive(b bool) Option
```

### Demo Application

Build a demo that uses your watcher to:

1. Watch a directory of `.go` files
2. On change: run `go vet ./...` and `go build ./...`
3. Print which files changed and the build result
4. Ignore `vendor/`, `.git/`, and `*_test.go`
5. Use 200ms debounce

## Success Criteria

1. **Correctness**: file create, modify, delete, and rename events are all detected
2. **Recursive**: creating a new subdirectory automatically adds it to the watch list; deleting removes it
3. **Filtering**: only files matching patterns trigger actions; ignored patterns are silent
4. **Debouncing**: rapid saves (simulate with 10 writes in 50ms) produce a single action call
5. **Concurrency**: watcher runs in a goroutine, does not block the main thread, no races (pass `go test -race`)
6. **Shutdown**: context cancellation stops the watcher within 1 second, no goroutine leaks
7. **Error resilience**: action errors are logged, not fatal; watcher continues
8. **Event deduplication**: if the same file triggers multiple events in one debounce window, deduplicate by path (keep the latest op)

## Constraints

- Use `github.com/fsnotify/fsnotify` for OS-level notifications (do not poll)
- No other external dependencies for the watcher itself (the demo may use `os/exec`)
- Must work on macOS and Linux
- Debounce must use a timer, not `time.Sleep` (non-blocking)

## Guidance

### Architecture

```
fsnotify events -> filter (patterns/ignore) -> debouncer -> action executor
```

The debouncer is the trickiest part. Use a `time.Timer`:
- On first event: start timer for `debounce` duration, collect events
- On subsequent events within the window: reset timer, add to batch
- When timer fires: execute actions with the collected batch, clear batch

```go
func (w *Watcher) debounceLoop(ctx context.Context, raw <-chan Event) {
    var batch []Event
    timer := time.NewTimer(0)
    if !timer.Stop() { <-timer.C }

    for {
        select {
        case <-ctx.Done():
            return
        case ev := <-raw:
            batch = append(batch, ev)
            timer.Reset(w.debounce)
        case <-timer.C:
            if len(batch) > 0 {
                w.executeActions(deduplicate(batch))
                batch = nil
            }
        }
    }
}
```

### Testing Strategy

1. Create a temp directory
2. Start the watcher
3. Programmatically create/modify/delete files
4. Assert that actions fire with the correct events
5. Use channels to synchronize assertions with async events

Write at least 5 tests:
- Test single file change detection
- Test pattern filtering
- Test ignore filtering
- Test debouncing (multiple rapid changes = one action)
- Test recursive subdirectory watching

## Stretch Goals

- Add a `--run` flag to execute an arbitrary command on changes (like `watchexec`)
- Implement polling fallback for filesystems that do not support inotify/kqueue (NFS, Docker volumes)
- Add file content hashing to avoid false triggers (some editors save without changing content)
- Implement a "run and restart" mode: start a long-running process on startup, kill and restart on changes
- Add WebSocket support: notify browser clients of file changes (live reload)
- Track watch count and warn when approaching OS limits (`/proc/sys/fs/inotify/max_user_watches` on Linux)
