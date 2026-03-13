# 2. Message Passing

**Difficulty**: Avanzado

## Prerequisites
- Completed: 01-threads-and-spawn
- Familiarity with: `thread::spawn`, `move` closures, `Send` trait, `Result`

## Learning Objectives
- Evaluate the trade-offs between channels and shared state for inter-thread communication
- Design pipeline and fan-out/fan-in architectures using channels
- Analyze bounded vs unbounded channel behavior under backpressure
- Apply crossbeam-channel for multi-consumer patterns

## Concepts

### std::sync::mpsc

The standard library provides multi-producer, single-consumer channels. "mpsc" is the key constraint -- multiple threads can send, but only one can receive.

```rust
use std::sync::mpsc;

let (tx, rx) = mpsc::channel(); // unbounded

tx.send(42).unwrap();
let val = rx.recv().unwrap(); // blocks until a value arrives
```

`send()` returns `Err` if the receiver has been dropped. `recv()` returns `Err` if all senders have been dropped. This is how you detect channel closure.

### Bounded vs Unbounded

```rust
// Unbounded: sender never blocks, memory grows without limit
let (tx, rx) = mpsc::channel();

// Bounded (sync_channel): sender blocks when buffer is full
let (tx, rx) = mpsc::sync_channel(16); // buffer of 16 messages
```

Unbounded channels are a memory leak waiting to happen if the consumer is slower than the producer. In production, prefer bounded channels. `sync_channel(0)` is a rendezvous channel -- every send blocks until a matching recv.

### try_recv and Non-Blocking

`recv()` blocks. `try_recv()` returns immediately with `Err(TryRecvError::Empty)` if nothing is available:

```rust
use std::sync::mpsc::TryRecvError;

match rx.try_recv() {
    Ok(val) => println!("Got: {val}"),
    Err(TryRecvError::Empty) => println!("Nothing yet"),
    Err(TryRecvError::Disconnected) => println!("Channel closed"),
}
```

There's also `recv_timeout(Duration)` for bounded waiting.

### Multiple Producers

Clone the sender. Each clone can be moved into a different thread:

```rust
use std::sync::mpsc;
use std::thread;

let (tx, rx) = mpsc::channel();

for i in 0..4 {
    let tx = tx.clone();
    thread::spawn(move || {
        tx.send(i * 10).unwrap();
    });
}
drop(tx); // drop the original -- otherwise rx.iter() never ends

for val in rx {
    println!("{val}");
}
```

That `drop(tx)` is critical. The iterator on `rx` runs until all senders are dropped. If you forget to drop the original, your program hangs.

### crossbeam-channel

The standard `mpsc` has a significant limitation: single consumer. `crossbeam-channel` provides multi-producer, multi-consumer channels with a `select!` macro:

```toml
[dependencies]
crossbeam-channel = "0.5"
```

```rust
use crossbeam_channel::{bounded, select, Receiver, Sender};

let (tx, rx) = bounded::<String>(100);

// Multiple consumers can clone rx
let rx2 = rx.clone();
```

The `select!` macro lets you wait on multiple channels simultaneously:

```rust
use crossbeam_channel::{bounded, select, after};
use std::time::Duration;

let (tx_a, rx_a) = bounded(10);
let (tx_b, rx_b) = bounded(10);

select! {
    recv(rx_a) -> msg => println!("From A: {msg:?}"),
    recv(rx_b) -> msg => println!("From B: {msg:?}"),
    recv(after(Duration::from_secs(5))) -> _ => println!("Timeout"),
}
```

### Communication Patterns

**Pipeline**: Each stage processes and forwards to the next.

```
[Producer] --tx1--> [Stage A] --tx2--> [Stage B] --tx3--> [Collector]
```

**Fan-out**: One producer, multiple consumers pulling from the same channel (requires crossbeam or custom dispatch).

**Fan-in**: Multiple producers, one consumer collecting results (native mpsc does this).

### Channels vs Shared State

| Aspect | Channels | Shared State (Mutex) |
|--------|----------|---------------------|
| Mental model | Message passing | Shared memory |
| Ordering | FIFO guaranteed | No ordering guarantee |
| Backpressure | Bounded channels | N/A |
| Deadlock risk | Low (unidirectional) | Higher (lock ordering) |
| Performance | Allocation per message | Contention on lock |
| Best for | Pipeline architectures | Simple counters, caches |

The Go proverb applies: "Don't communicate by sharing memory; share memory by communicating." But Rust gives you safe access to both approaches, so pick the one that fits.

## Exercises

### Exercise 1: Log Processing Pipeline

**Problem**: Build a three-stage pipeline that processes log lines:

1. **Parser** thread: receives raw log strings, parses them into a struct `LogEntry { level: Level, message: String, timestamp_ms: u64 }`.
2. **Filter** thread: receives parsed entries, drops anything below `Warn` level.
3. **Aggregator** thread: receives filtered entries, counts by level, prints summary when the channel closes.

Feed at least 1000 log lines through the pipeline. Use bounded channels with a buffer of 64. Measure throughput.

**Hints**:
- Define `Level` as an enum: `Debug, Info, Warn, Error`.
- The parser should handle malformed lines gracefully (skip them, don't panic).
- Use `std::time::Instant` to measure wall-clock time from first send to final summary.
- Think about what happens if the filter is slow -- how does backpressure propagate?

**One possible solution**:

```rust
use std::sync::mpsc::{self, SyncSender, Receiver};
use std::thread;
use std::time::Instant;
use std::collections::HashMap;

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
enum Level { Debug, Info, Warn, Error }

#[derive(Debug)]
struct LogEntry {
    level: Level,
    message: String,
    timestamp_ms: u64,
}

fn parse_log(raw: &str) -> Option<LogEntry> {
    let mut parts = raw.splitn(3, '|');
    let ts: u64 = parts.next()?.parse().ok()?;
    let level = match parts.next()?.trim() {
        "DEBUG" => Level::Debug,
        "INFO" => Level::Info,
        "WARN" => Level::Warn,
        "ERROR" => Level::Error,
        _ => return None,
    };
    let message = parts.next()?.to_string();
    Some(LogEntry { level, message, timestamp_ms: ts })
}

fn main() {
    let (raw_tx, raw_rx): (SyncSender<String>, Receiver<String>) = mpsc::sync_channel(64);
    let (parsed_tx, parsed_rx): (SyncSender<LogEntry>, Receiver<LogEntry>) = mpsc::sync_channel(64);
    let (filtered_tx, filtered_rx): (SyncSender<LogEntry>, Receiver<LogEntry>) = mpsc::sync_channel(64);

    // Stage 1: Parser
    let parser = thread::spawn(move || {
        for raw in raw_rx {
            if let Some(entry) = parse_log(&raw) {
                let _ = parsed_tx.send(entry);
            }
        }
    });

    // Stage 2: Filter
    let filter = thread::spawn(move || {
        for entry in parsed_rx {
            match entry.level {
                Level::Warn | Level::Error => { let _ = filtered_tx.send(entry); }
                _ => {}
            }
        }
    });

    // Stage 3: Aggregator
    let aggregator = thread::spawn(move || {
        let mut counts: HashMap<Level, usize> = HashMap::new();
        for entry in filtered_rx {
            *counts.entry(entry.level).or_default() += 1;
        }
        counts
    });

    // Producer
    let start = Instant::now();
    let levels = ["DEBUG", "INFO", "WARN", "ERROR"];
    for i in 0..1000 {
        let level = levels[i % levels.len()];
        let line = format!("{i}|{level}|something happened at step {i}");
        raw_tx.send(line).unwrap();
    }
    drop(raw_tx);

    parser.join().unwrap();
    filter.join().unwrap();
    let counts = aggregator.join().unwrap();

    let elapsed = start.elapsed();
    println!("Counts: {counts:?}");
    println!("Processed 1000 lines in {elapsed:?}");
}
```

### Exercise 2: Fan-Out Work Distribution

**Problem**: Build a system where one producer generates tasks and N workers consume them. Use `crossbeam-channel` so multiple workers can pull from the same receiver. Each worker should identify itself by ID. Collect results through a separate fan-in channel.

**Hints**:
- Clone the `Receiver` for each worker thread.
- Use a second channel for results -- all workers send to it, the main thread receives.
- Try different worker counts and observe how work distributes.
- What happens if you use `crossbeam::bounded(0)` (rendezvous)? How does it change behavior?

**Cargo.toml**:
```toml
[dependencies]
crossbeam-channel = "0.5"
```

**One possible solution**:

```rust
use crossbeam_channel::{bounded, Sender, Receiver};
use std::thread;

struct Task {
    id: usize,
    payload: u64,
}

struct TaskResult {
    task_id: usize,
    worker_id: usize,
    result: u64,
}

fn main() {
    let num_workers = 4;
    let (task_tx, task_rx): (Sender<Task>, Receiver<Task>) = bounded(32);
    let (result_tx, result_rx): (Sender<TaskResult>, Receiver<TaskResult>) = bounded(32);

    // Spawn workers
    let mut handles = Vec::new();
    for worker_id in 0..num_workers {
        let rx = task_rx.clone();
        let tx = result_tx.clone();
        handles.push(thread::spawn(move || {
            for task in rx {
                // Simulate CPU work
                let result = (0..task.payload).sum::<u64>();
                tx.send(TaskResult {
                    task_id: task.id,
                    worker_id,
                    result,
                }).unwrap();
            }
        }));
    }
    drop(result_tx); // drop original so result_rx closes when workers finish

    // Producer
    let producer = thread::spawn(move || {
        for i in 0..100 {
            task_tx.send(Task { id: i, payload: (i as u64 + 1) * 1000 }).unwrap();
        }
    });

    // Collect results
    let mut by_worker = vec![0usize; num_workers];
    for result in result_rx {
        by_worker[result.worker_id] += 1;
    }

    producer.join().unwrap();
    for h in handles { h.join().unwrap(); }

    println!("Work distribution: {by_worker:?}");
}
```

### Exercise 3: Timeout and Graceful Shutdown (Design Challenge)

**Problem**: Design a system with a producer that generates events indefinitely. Workers process events, but the entire system must shut down gracefully within a given timeout. Requirements:

- No events are lost silently (either processed or acknowledged as dropped).
- Workers finish their current job before exiting.
- If shutdown takes longer than the timeout, force-exit with a count of dropped events.

This exercises `select!`, `after()`, and shutdown signaling. Design your approach before looking at any references.

## Design Decisions

**Bounded channel size**: Too small and producers stall frequently (context switches). Too large and you buffer too much memory. Start with a size proportional to your worker count (e.g., `2 * num_workers`) and benchmark from there.

**Channel type selection**: For single-consumer pipelines, `std::sync::mpsc` is fine and avoids a dependency. For multi-consumer work distribution, you need `crossbeam-channel` (or build your own with `Arc<Mutex<VecDeque>>`).

**Typed channels vs `Box<dyn Any>`**: Always use typed channels. The compiler catches message type mismatches at compile time. If you need heterogeneous messages, use an enum.

## Common Mistakes

1. **Forgetting to drop the original sender** -- the receiver's iterator never terminates and your program hangs.
2. **Unbounded channels in production** -- the producer can outpace the consumer indefinitely, consuming unbounded memory.
3. **Sending large values through channels** -- channels clone/move the value. Send `Arc<LargeData>` or indices instead.
4. **Blocking recv in a select loop** -- use `try_recv` or `crossbeam::select!` instead of mixing blocking and non-blocking.

## Verification

Run your pipeline with varying channel sizes (1, 16, 256) and measure throughput. You should observe that very small buffers hurt throughput (too many context switches) and very large buffers waste memory without meaningful gain.

## Summary

- `mpsc::channel()` is unbounded; `sync_channel(n)` is bounded. Prefer bounded.
- Drop all senders to signal channel closure. The receiver's iterator ends naturally.
- `crossbeam-channel` adds multi-consumer and `select!`. Use it when `mpsc` is too limiting.
- Pipeline, fan-out, and fan-in are the core channel patterns. Most concurrent architectures compose from these.
- Channels give you backpressure, ordering, and clean shutdown semantics that shared state does not.

## What's Next

Sometimes message passing is the wrong tool -- shared counters, caches, and flags are better served by shared state. Next exercise covers `Mutex`, `RwLock`, atomics, and the art of not deadlocking.

## Resources

- [std::sync::mpsc](https://doc.rust-lang.org/std/sync/mpsc/)
- [crossbeam-channel docs](https://docs.rs/crossbeam-channel)
- [Crossbeam: Designing Channels](https://docs.rs/crossbeam/latest/crossbeam/channel/index.html)
- [Go Concurrency Patterns](https://go.dev/blog/pipelines) -- the ideas translate directly to Rust channels
