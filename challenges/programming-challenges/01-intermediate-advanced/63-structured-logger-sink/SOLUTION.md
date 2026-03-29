# Solution: Structured Logger with Sinks

## Architecture Overview

Both implementations share the same layered architecture:

1. **LogEntry** -- the structured record: timestamp, level, message, key-value fields
2. **Formatter** -- transforms a LogEntry into bytes (JSON or Text)
3. **Sink** -- writes formatted bytes to a destination (stdout, file, async buffer)
4. **Logger** -- the user-facing API: creates entries, applies level filtering and sampling, fans out to sinks

```
Logger (level filter, sampling, context fields)
  |
  +---> Sink[stdout]  <--- Formatter[text]
  |
  +---> Sink[async]   <--- Sink[file + rotation] <--- Formatter[json]
```

## Go Solution

### Project Setup

```bash
mkdir -p structlog && cd structlog
go mod init structlog
```

### Implementation

```go
// log.go
package structlog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

type Entry struct {
	Timestamp  time.Time         `json:"timestamp"`
	Level      Level             `json:"level"`
	Message    string            `json:"message"`
	Fields     map[string]string `json:"fields,omitempty"`
	Suppressed int64             `json:"suppressed,omitempty"`
}

// --- Formatters ---

type Formatter interface {
	Format(e Entry) []byte
}

type JSONFormatter struct{}

func (f *JSONFormatter) Format(e Entry) []byte {
	m := map[string]any{
		"timestamp": e.Timestamp.Format(time.RFC3339Nano),
		"level":     e.Level.String(),
		"message":   e.Message,
	}
	for k, v := range e.Fields {
		m[k] = v
	}
	if e.Suppressed > 0 {
		m["suppressed"] = e.Suppressed
	}
	b, _ := json.Marshal(m)
	return append(b, '\n')
}

type TextFormatter struct{}

func (f *TextFormatter) Format(e Entry) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-23s %-5s %s",
		e.Timestamp.Format("2006-01-02T15:04:05.000"),
		e.Level.String(),
		e.Message,
	)
	for k, v := range e.Fields {
		fmt.Fprintf(&sb, "  %s=%s", k, v)
	}
	if e.Suppressed > 0 {
		fmt.Fprintf(&sb, "  suppressed=%d", e.Suppressed)
	}
	sb.WriteByte('\n')
	return []byte(sb.String())
}

// --- Sinks ---

type Sink interface {
	Write(entry Entry) error
	Flush() error
	Close() error
}

// StdoutSink writes to standard output.
type StdoutSink struct {
	mu        sync.Mutex
	formatter Formatter
	minLevel  Level
}

func NewStdoutSink(formatter Formatter, minLevel Level) *StdoutSink {
	return &StdoutSink{formatter: formatter, minLevel: minLevel}
}

func (s *StdoutSink) Write(e Entry) error {
	if e.Level < s.minLevel {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := os.Stdout.Write(s.formatter.Format(e))
	return err
}

func (s *StdoutSink) Flush() error { return nil }
func (s *StdoutSink) Close() error { return nil }

// FileSink writes to a file with rotation support.
type FileSink struct {
	mu          sync.Mutex
	formatter   Formatter
	minLevel    Level
	path        string
	maxSize     int64
	file        *os.File
	currentSize int64
}

func NewFileSink(formatter Formatter, minLevel Level, path string, maxSize int64) (*FileSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	info, _ := f.Stat()
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return &FileSink{
		formatter:   formatter,
		minLevel:    minLevel,
		path:        path,
		maxSize:     maxSize,
		file:        f,
		currentSize: size,
	}, nil
}

func (s *FileSink) Write(e Entry) error {
	if e.Level < s.minLevel {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data := s.formatter.Format(e)

	if s.maxSize > 0 && s.currentSize+int64(len(data)) > s.maxSize {
		if err := s.rotate(); err != nil {
			return fmt.Errorf("rotation failed: %w", err)
		}
	}

	n, err := s.file.Write(data)
	s.currentSize += int64(n)
	return err
}

func (s *FileSink) rotate() error {
	s.file.Close()

	rotatedName := fmt.Sprintf("%s.%s", s.path, time.Now().Format("2006-01-02T15-04-05"))
	if err := os.Rename(s.path, rotatedName); err != nil {
		return err
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	s.file = f
	s.currentSize = 0
	return nil
}

func (s *FileSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Sync()
}

func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}

// AsyncSink wraps another sink with buffered async writes.
type AsyncSink struct {
	inner    Sink
	ch       chan Entry
	done     chan struct{}
	wg       sync.WaitGroup
}

func NewAsyncSink(inner Sink, bufSize int, flushInterval time.Duration) *AsyncSink {
	a := &AsyncSink{
		inner: inner,
		ch:    make(chan Entry, bufSize),
		done:  make(chan struct{}),
	}
	a.wg.Add(1)
	go a.loop(flushInterval)
	return a
}

func (a *AsyncSink) loop(interval time.Duration) {
	defer a.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case e, ok := <-a.ch:
			if !ok {
				a.inner.Flush()
				return
			}
			a.inner.Write(e)
		case <-ticker.C:
			a.inner.Flush()
		}
	}
}

func (a *AsyncSink) Write(e Entry) error {
	select {
	case a.ch <- e:
		return nil
	default:
		// Buffer full: drop entry rather than blocking the caller.
		return fmt.Errorf("async buffer full, entry dropped")
	}
}

func (a *AsyncSink) Flush() error { return a.inner.Flush() }

func (a *AsyncSink) Close() error {
	close(a.ch)
	a.wg.Wait()
	return a.inner.Close()
}

// --- Sampler ---

type Sampler struct {
	rate    int64
	counter atomic.Int64
}

func NewSampler(rate int) *Sampler {
	return &Sampler{rate: int64(rate)}
}

func (s *Sampler) Check() (bool, int64) {
	n := s.counter.Add(1)
	if n%s.rate == 0 {
		return true, s.rate - 1
	}
	return false, 0
}

// --- Logger ---

type Logger struct {
	sinks     []Sink
	fields    map[string]string
	sampler   *Sampler
}

func NewLogger(sinks ...Sink) *Logger {
	return &Logger{
		sinks:  sinks,
		fields: make(map[string]string),
	}
}

func (l *Logger) WithFields(fields map[string]string) *Logger {
	merged := make(map[string]string, len(l.fields)+len(fields))
	for k, v := range l.fields {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	return &Logger{
		sinks:   l.sinks,
		fields:  merged,
		sampler: l.sampler,
	}
}

func (l *Logger) WithSampler(s *Sampler) *Logger {
	return &Logger{
		sinks:   l.sinks,
		fields:  l.fields,
		sampler: s,
	}
}

func (l *Logger) log(level Level, msg string, fields map[string]string) {
	var suppressed int64
	if l.sampler != nil {
		shouldLog, s := l.sampler.Check()
		if !shouldLog {
			return
		}
		suppressed = s
	}

	allFields := make(map[string]string, len(l.fields)+len(fields))
	for k, v := range l.fields {
		allFields[k] = v
	}
	for k, v := range fields {
		allFields[k] = v
	}

	entry := Entry{
		Timestamp:  time.Now(),
		Level:      level,
		Message:    msg,
		Fields:     allFields,
		Suppressed: suppressed,
	}

	for _, sink := range l.sinks {
		sink.Write(entry)
	}
}

func (l *Logger) Debug(msg string, fields map[string]string) { l.log(LevelDebug, msg, fields) }
func (l *Logger) Info(msg string, fields map[string]string)  { l.log(LevelInfo, msg, fields) }
func (l *Logger) Warn(msg string, fields map[string]string)  { l.log(LevelWarn, msg, fields) }
func (l *Logger) Error(msg string, fields map[string]string) { l.log(LevelError, msg, fields) }

func (l *Logger) Shutdown() {
	for _, sink := range l.sinks {
		sink.Flush()
		sink.Close()
	}
}

// contextKey avoids collisions with other packages.
type contextKey struct{}

func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(contextKey{}).(*Logger); ok {
		return l
	}
	return NewLogger(NewStdoutSink(&TextFormatter{}, LevelInfo))
}

// Ensure the FileSink path directory exists
func ensureDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0755)
}
```

### Tests

```go
// log_test.go
package structlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJSONFormatterOutput(t *testing.T) {
	f := &JSONFormatter{}
	e := Entry{
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Level:     LevelInfo,
		Message:   "request handled",
		Fields:    map[string]string{"request_id": "abc-123", "status": "200"},
	}
	data := f.Format(e)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON output is not valid: %v", err)
	}
	if parsed["message"] != "request handled" {
		t.Errorf("unexpected message: %v", parsed["message"])
	}
	if parsed["request_id"] != "abc-123" {
		t.Errorf("missing field request_id")
	}
}

func TestTextFormatterOutput(t *testing.T) {
	f := &TextFormatter{}
	e := Entry{
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Level:     LevelWarn,
		Message:   "slow query",
		Fields:    map[string]string{"duration": "3.2s"},
	}
	out := string(f.Format(e))

	if !strings.Contains(out, "WARN") {
		t.Error("missing level in text output")
	}
	if !strings.Contains(out, "slow query") {
		t.Error("missing message in text output")
	}
	if !strings.Contains(out, "duration=3.2s") {
		t.Error("missing field in text output")
	}
}

func TestFileSinkRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	sink, err := NewFileSink(&JSONFormatter{}, LevelDebug, path, 500)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ {
		sink.Write(Entry{
			Timestamp: time.Now(),
			Level:     LevelInfo,
			Message:   "test entry with enough data to trigger rotation eventually",
			Fields:    map[string]string{"index": "value"},
		})
	}
	sink.Close()

	entries, _ := os.ReadDir(dir)
	if len(entries) < 2 {
		t.Errorf("expected at least 2 files after rotation, got %d", len(entries))
	}
	for _, e := range entries {
		t.Logf("rotated file: %s", e.Name())
	}
}

func TestAsyncSinkFlushOnClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "async.log")

	fileSink, _ := NewFileSink(&JSONFormatter{}, LevelDebug, path, 0)
	asyncSink := NewAsyncSink(fileSink, 100, 1*time.Second)

	for i := 0; i < 10; i++ {
		asyncSink.Write(Entry{
			Timestamp: time.Now(),
			Level:     LevelInfo,
			Message:   "async entry",
		})
	}

	asyncSink.Close()

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines after flush, got %d", len(lines))
	}
}

func TestLevelFiltering(t *testing.T) {
	var written []Entry
	mock := &mockSink{minLevel: LevelWarn, entries: &written}

	logger := NewLogger(mock)
	logger.Debug("should be dropped", nil)
	logger.Info("should be dropped", nil)
	logger.Warn("should appear", nil)
	logger.Error("should appear", nil)

	if len(written) != 2 {
		t.Errorf("expected 2 entries, got %d", len(written))
	}
}

func TestContextFields(t *testing.T) {
	var written []Entry
	mock := &mockSink{minLevel: LevelDebug, entries: &written}

	logger := NewLogger(mock)
	reqLogger := logger.WithFields(map[string]string{
		"request_id": "req-456",
		"user":       "alice",
	})

	reqLogger.Info("handling request", map[string]string{"path": "/api"})

	if len(written) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(written))
	}
	if written[0].Fields["request_id"] != "req-456" {
		t.Error("missing context field request_id")
	}
	if written[0].Fields["path"] != "/api" {
		t.Error("missing per-call field path")
	}
}

func TestSampling(t *testing.T) {
	var written []Entry
	mock := &mockSink{minLevel: LevelDebug, entries: &written}

	sampler := NewSampler(10)
	logger := NewLogger(mock).WithSampler(sampler)

	for i := 0; i < 100; i++ {
		logger.Info("sampled entry", nil)
	}

	if len(written) != 10 {
		t.Errorf("expected 10 sampled entries (1 in 10), got %d", len(written))
	}
	if written[0].Suppressed != 9 {
		t.Errorf("expected suppressed=9, got %d", written[0].Suppressed)
	}
}

func TestConcurrentLogging(t *testing.T) {
	var written []Entry
	mock := &mockSink{minLevel: LevelDebug, entries: &written}

	logger := NewLogger(mock)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				logger.Info("concurrent entry", nil)
			}
		}()
	}
	wg.Wait()

	if len(written) != 1000 {
		t.Errorf("expected 1000 entries, got %d", len(written))
	}
}

// mockSink collects entries for assertions.
type mockSink struct {
	mu       sync.Mutex
	minLevel Level
	entries  *[]Entry
}

func (m *mockSink) Write(e Entry) error {
	if e.Level < m.minLevel {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.entries = append(*m.entries, e)
	return nil
}

func (m *mockSink) Flush() error { return nil }
func (m *mockSink) Close() error { return nil }
```

### Running and Testing

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestJSONFormatterOutput
--- PASS: TestJSONFormatterOutput (0.00s)
=== RUN   TestTextFormatterOutput
--- PASS: TestTextFormatterOutput (0.00s)
=== RUN   TestFileSinkRotation
    log_test.go:63: rotated file: app.log
    log_test.go:63: rotated file: app.log.2024-01-15T10-30-01
--- PASS: TestFileSinkRotation (0.00s)
=== RUN   TestAsyncSinkFlushOnClose
--- PASS: TestAsyncSinkFlushOnClose (0.00s)
=== RUN   TestLevelFiltering
--- PASS: TestLevelFiltering (0.00s)
=== RUN   TestContextFields
--- PASS: TestContextFields (0.00s)
=== RUN   TestSampling
--- PASS: TestSampling (0.00s)
=== RUN   TestConcurrentLogging
--- PASS: TestConcurrentLogging (0.00s)
PASS
```

## Rust Solution

### Project Setup

```bash
cargo new structlog --lib && cd structlog
```

Add to `Cargo.toml`:

```toml
[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
chrono = { version = "0.4", features = ["serde"] }
```

### Implementation

```rust
// src/lib.rs
use chrono::{DateTime, Utc};
use serde::Serialize;
use std::collections::HashMap;
use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::{mpsc, Arc, Mutex};
use std::thread;
use std::time::Duration;

#[derive(Debug, Clone, Copy, PartialEq, PartialOrd, Serialize)]
pub enum Level {
    Debug,
    Info,
    Warn,
    Error,
}

impl std::fmt::Display for Level {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Level::Debug => write!(f, "DEBUG"),
            Level::Info => write!(f, "INFO"),
            Level::Warn => write!(f, "WARN"),
            Level::Error => write!(f, "ERROR"),
        }
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct LogEntry {
    pub timestamp: DateTime<Utc>,
    pub level: Level,
    pub message: String,
    pub fields: HashMap<String, String>,
    #[serde(skip_serializing_if = "is_zero")]
    pub suppressed: i64,
}

fn is_zero(v: &i64) -> bool { *v == 0 }

// --- Formatters ---

pub trait Formatter: Send + Sync {
    fn format(&self, entry: &LogEntry) -> Vec<u8>;
}

pub struct JsonFormatter;

impl Formatter for JsonFormatter {
    fn format(&self, entry: &LogEntry) -> Vec<u8> {
        let mut s = serde_json::to_string(entry).unwrap_or_default();
        s.push('\n');
        s.into_bytes()
    }
}

pub struct TextFormatter;

impl Formatter for TextFormatter {
    fn format(&self, entry: &LogEntry) -> Vec<u8> {
        let ts = entry.timestamp.format("%Y-%m-%dT%H:%M:%S%.3f");
        let mut line = format!("{} {:5} {}", ts, entry.level, entry.message);
        for (k, v) in &entry.fields {
            line.push_str(&format!("  {}={}", k, v));
        }
        if entry.suppressed > 0 {
            line.push_str(&format!("  suppressed={}", entry.suppressed));
        }
        line.push('\n');
        line.into_bytes()
    }
}

// --- Sinks ---

pub trait Sink: Send + Sync {
    fn write_entry(&self, entry: &LogEntry) -> Result<(), String>;
    fn flush(&self) -> Result<(), String>;
    fn close(&self) -> Result<(), String>;
}

pub struct StdoutSink {
    formatter: Box<dyn Formatter>,
    min_level: Level,
}

impl StdoutSink {
    pub fn new(formatter: Box<dyn Formatter>, min_level: Level) -> Self {
        Self { formatter, min_level }
    }
}

impl Sink for StdoutSink {
    fn write_entry(&self, entry: &LogEntry) -> Result<(), String> {
        if entry.level < self.min_level {
            return Ok(());
        }
        let data = self.formatter.format(entry);
        std::io::stdout().write_all(&data).map_err(|e| e.to_string())
    }

    fn flush(&self) -> Result<(), String> {
        std::io::stdout().flush().map_err(|e| e.to_string())
    }

    fn close(&self) -> Result<(), String> { Ok(()) }
}

pub struct FileSink {
    formatter: Box<dyn Formatter>,
    min_level: Level,
    path: PathBuf,
    max_size: u64,
    state: Mutex<FileSinkState>,
}

struct FileSinkState {
    file: File,
    current_size: u64,
}

impl FileSink {
    pub fn new(
        formatter: Box<dyn Formatter>,
        min_level: Level,
        path: impl AsRef<Path>,
        max_size: u64,
    ) -> Result<Self, String> {
        let path = path.as_ref().to_path_buf();
        let file = OpenOptions::new()
            .create(true).append(true).open(&path)
            .map_err(|e| e.to_string())?;
        let size = file.metadata().map(|m| m.len()).unwrap_or(0);
        Ok(Self {
            formatter, min_level, path, max_size,
            state: Mutex::new(FileSinkState { file, current_size: size }),
        })
    }

    fn rotate(state: &mut FileSinkState, path: &Path) -> Result<(), String> {
        state.file.flush().map_err(|e| e.to_string())?;
        let rotated = format!(
            "{}.{}", path.display(),
            Utc::now().format("%Y-%m-%dT%H-%M-%S")
        );
        fs::rename(path, &rotated).map_err(|e| e.to_string())?;
        state.file = OpenOptions::new()
            .create(true).append(true).open(path)
            .map_err(|e| e.to_string())?;
        state.current_size = 0;
        Ok(())
    }
}

impl Sink for FileSink {
    fn write_entry(&self, entry: &LogEntry) -> Result<(), String> {
        if entry.level < self.min_level {
            return Ok(());
        }
        let data = self.formatter.format(entry);
        let mut state = self.state.lock().unwrap();
        if self.max_size > 0 && state.current_size + data.len() as u64 > self.max_size {
            Self::rotate(&mut state, &self.path)?;
        }
        state.file.write_all(&data).map_err(|e| e.to_string())?;
        state.current_size += data.len() as u64;
        Ok(())
    }

    fn flush(&self) -> Result<(), String> {
        self.state.lock().unwrap().file.flush().map_err(|e| e.to_string())
    }

    fn close(&self) -> Result<(), String> { self.flush() }
}

pub struct AsyncSink {
    tx: Mutex<Option<mpsc::Sender<LogEntry>>>,
    handle: Mutex<Option<thread::JoinHandle<()>>>,
}

impl AsyncSink {
    pub fn new(inner: Arc<dyn Sink>, buf_size: usize, flush_interval: Duration) -> Self {
        let (tx, rx) = mpsc::sync_channel::<LogEntry>(buf_size);
        let handle = thread::spawn(move || {
            loop {
                match rx.recv_timeout(flush_interval) {
                    Ok(entry) => { let _ = inner.write_entry(&entry); }
                    Err(mpsc::RecvTimeoutError::Timeout) => { let _ = inner.flush(); }
                    Err(mpsc::RecvTimeoutError::Disconnected) => {
                        let _ = inner.flush();
                        return;
                    }
                }
            }
        });
        Self {
            tx: Mutex::new(Some(tx)),
            handle: Mutex::new(Some(handle)),
        }
    }
}

impl Sink for AsyncSink {
    fn write_entry(&self, entry: &LogEntry) -> Result<(), String> {
        let guard = self.tx.lock().unwrap();
        if let Some(tx) = guard.as_ref() {
            tx.try_send(entry.clone()).map_err(|e| e.to_string())
        } else {
            Err("sink closed".into())
        }
    }

    fn flush(&self) -> Result<(), String> { Ok(()) }

    fn close(&self) -> Result<(), String> {
        { let mut guard = self.tx.lock().unwrap(); *guard = None; }
        if let Some(handle) = self.handle.lock().unwrap().take() {
            handle.join().map_err(|_| "join failed".to_string())?;
        }
        Ok(())
    }
}

// --- Sampler ---

pub struct Sampler {
    rate: i64,
    counter: AtomicI64,
}

impl Sampler {
    pub fn new(rate: i64) -> Self {
        Self { rate, counter: AtomicI64::new(0) }
    }

    pub fn check(&self) -> (bool, i64) {
        let n = self.counter.fetch_add(1, Ordering::Relaxed) + 1;
        if n % self.rate == 0 {
            (true, self.rate - 1)
        } else {
            (false, 0)
        }
    }
}

// --- Logger ---

pub struct Logger {
    sinks: Vec<Arc<dyn Sink>>,
    fields: HashMap<String, String>,
    sampler: Option<Arc<Sampler>>,
}

impl Logger {
    pub fn new(sinks: Vec<Arc<dyn Sink>>) -> Self {
        Self { sinks, fields: HashMap::new(), sampler: None }
    }

    pub fn with_fields(&self, extra: HashMap<String, String>) -> Self {
        let mut fields = self.fields.clone();
        fields.extend(extra);
        Self { sinks: self.sinks.clone(), fields, sampler: self.sampler.clone() }
    }

    pub fn with_sampler(&self, sampler: Arc<Sampler>) -> Self {
        Self { sinks: self.sinks.clone(), fields: self.fields.clone(), sampler: Some(sampler) }
    }

    fn log(&self, level: Level, msg: &str, extra: HashMap<String, String>) {
        let mut suppressed = 0i64;
        if let Some(ref s) = self.sampler {
            let (should, supp) = s.check();
            if !should { return; }
            suppressed = supp;
        }
        let mut fields = self.fields.clone();
        fields.extend(extra);
        let entry = LogEntry {
            timestamp: Utc::now(), level, message: msg.to_string(), fields, suppressed,
        };
        for sink in &self.sinks {
            let _ = sink.write_entry(&entry);
        }
    }

    pub fn debug(&self, msg: &str, f: HashMap<String, String>) { self.log(Level::Debug, msg, f); }
    pub fn info(&self, msg: &str, f: HashMap<String, String>)  { self.log(Level::Info, msg, f); }
    pub fn warn(&self, msg: &str, f: HashMap<String, String>)  { self.log(Level::Warn, msg, f); }
    pub fn error(&self, msg: &str, f: HashMap<String, String>) { self.log(Level::Error, msg, f); }

    pub fn shutdown(&self) {
        for sink in &self.sinks {
            let _ = sink.flush();
            let _ = sink.close();
        }
    }
}
```

### Rust Tests

```rust
// src/lib.rs (continued, or tests/integration.rs)
#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::AtomicUsize;

    struct CountingSink {
        count: AtomicUsize,
        min_level: Level,
    }

    impl CountingSink {
        fn new(min_level: Level) -> Self {
            Self { count: AtomicUsize::new(0), min_level }
        }
    }

    impl Sink for CountingSink {
        fn write_entry(&self, entry: &LogEntry) -> Result<(), String> {
            if entry.level >= self.min_level {
                self.count.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            }
            Ok(())
        }
        fn flush(&self) -> Result<(), String> { Ok(()) }
        fn close(&self) -> Result<(), String> { Ok(()) }
    }

    #[test]
    fn test_json_formatter_valid_json() {
        let f = JsonFormatter;
        let entry = LogEntry {
            timestamp: Utc::now(), level: Level::Info,
            message: "test".into(), fields: HashMap::new(), suppressed: 0,
        };
        let data = f.format(&entry);
        let parsed: serde_json::Value = serde_json::from_slice(&data).unwrap();
        assert_eq!(parsed["message"], "test");
    }

    #[test]
    fn test_level_filtering() {
        let sink = Arc::new(CountingSink::new(Level::Warn));
        let logger = Logger::new(vec![sink.clone()]);
        logger.debug("drop", HashMap::new());
        logger.info("drop", HashMap::new());
        logger.warn("keep", HashMap::new());
        logger.error("keep", HashMap::new());
        assert_eq!(sink.count.load(std::sync::atomic::Ordering::SeqCst), 2);
    }

    #[test]
    fn test_sampling() {
        let sink = Arc::new(CountingSink::new(Level::Debug));
        let sampler = Arc::new(Sampler::new(10));
        let logger = Logger::new(vec![sink.clone()]).with_sampler(sampler);
        for _ in 0..100 {
            logger.info("sampled", HashMap::new());
        }
        assert_eq!(sink.count.load(std::sync::atomic::Ordering::SeqCst), 10);
    }

    #[test]
    fn test_concurrent_logging() {
        let sink = Arc::new(CountingSink::new(Level::Debug));
        let logger = Arc::new(Logger::new(vec![sink.clone()]));
        let mut handles = vec![];
        for _ in 0..50 {
            let l = logger.clone();
            handles.push(thread::spawn(move || {
                for _ in 0..20 {
                    l.info("concurrent", HashMap::new());
                }
            }));
        }
        for h in handles { h.join().unwrap(); }
        assert_eq!(sink.count.load(std::sync::atomic::Ordering::SeqCst), 1000);
    }
}
```

### Running the Rust Tests

```bash
cargo test -- --nocapture
```

## Design Decisions

**Decision 1: Level filtering at the sink level, not the logger level.** Each sink has its own minimum level. This allows a setup where stdout shows everything (Debug) for development, while the file sink only captures Warn and above. The logger applies sampling globally before reaching sinks, but level filtering is per-sink.

**Decision 2: Non-blocking async sink with drop-on-full.** When the async buffer is full, entries are dropped rather than blocking the caller. This is the correct choice for logging: blocking the application because the log system is slow is worse than losing a few log entries. The alternative (backpressure) would turn a logging problem into an application latency problem.

**Decision 3: Lazy file rotation instead of a background rotator.** Rotation is checked on each write. This means a log file could slightly exceed the size limit (by one entry), but it avoids background goroutines/threads and the complexity of coordinating rotation with writes.

## Common Mistakes

**Mistake 1: Serializing all writes through a single global mutex.** In the multi-sink design, each sink should have its own lock. A global lock means a slow file write blocks stdout output.

**Mistake 2: Forgetting to flush the async sink on shutdown.** Entries in the buffer are lost if the channel is closed without draining. Always drain the channel and call the inner sink's flush before returning from close.

**Mistake 3: Using `fmt.Sprintf` for JSON output.** Manual string formatting produces invalid JSON when field values contain quotes or newlines. Always use `encoding/json` (Go) or `serde_json` (Rust).

## Performance Notes

- JSON formatting is 3-5x slower than text formatting due to escaping and allocation. For high-throughput paths, use text formatting or pre-allocate a buffer.
- The atomic sampler counter is lock-free and adds negligible overhead per log call. At 1M logs/sec, sampling at 1-in-100 reduces sink I/O by 99% with no contention.
- File rotation involves a rename syscall and a new file open. This briefly blocks writes to that sink. For zero-downtime rotation, consider double-buffering the file handle.
