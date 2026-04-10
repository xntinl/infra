<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [GMP-scheduler, goroutine-parking, netpoller, epoll, Tokio-future-model, waker-mechanism, io_uring, async-runtime-internals, cooperative-vs-preemptive, work-stealing-async]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [work-stealing-scheduler, memory-models-and-happens-before, OS-threads, epoll-kqueue-basics]
papers: [Ousterhout 1995 "Why Threads Are A Bad Idea", Adya et al. 2002 "Cooperative Task Management Without Manual Stack Management"]
industry_use: [Go-runtime, Tokio, async-std, Node.js-libuv, Java-virtual-threads, nginx-event-loop]
language_contrast: high
-->

# Async Runtimes Internals

> Understanding your async runtime is the difference between writing code that works and writing code that scales: the scheduler is the hidden performance contract behind every goroutine and future.

## Mental Model

Concurrency without parallelism is the key insight for I/O-bound systems: a single OS thread can serve thousands of concurrent network connections if it parks waiting connections (suspends them in a queue) and runs ready connections (those with data to process). This is the event loop model — the foundation of Node.js, nginx, and every high-concurrency server. The async runtime is the machinery that implements this model: it multiplexes many logical concurrent tasks (goroutines, futures) onto fewer OS threads, parking tasks that are waiting for I/O and waking them when their I/O completes.

Go and Rust take fundamentally different approaches to async scheduling. Go uses **preemptive M:N threading**: N goroutines (user-space threads with their own stacks) multiplexed onto M OS threads, with the scheduler able to preempt any goroutine at safe points. Each goroutine maintains its own stack, enabling natural sequential programming (no `async`/`await` syntax). The runtime manages everything transparently. Rust uses **cooperative polling**: futures are state machines that yield at explicit `.await` points. The scheduler (Tokio, async-std, etc.) only calls a future when it is ready to make progress; the future's `poll` method runs to the next `.await` and either returns a result or suspends. There is no automatic stack — the future's state is encoded in its type.

The performance trade-off: goroutines have a fixed cost (~2KB stack + scheduling overhead ~1µs to spawn) but offer sequential, natural code. Rust futures have near-zero overhead (a future's state machine is typically 100-500 bytes, no heap allocation for simple futures) but require the `async`/`await` syntax and careful attention to blocking operations within async code.

## Core Concepts

### Go's GMP Scheduler in Depth

The three-layer GMP model:

**G (Goroutine)**: A user-space thread with a small, growable stack (starts at 2KB, grows to 1GB by default). Goroutines are cheap to create (~1µs, ~2KB memory). The G stores the goroutine's stack, status (running, runnable, waiting, dead), and a reference to the M it is running on.

**M (Machine / OS thread)**: An OS thread. Each M can run exactly one G at a time. M count is not bounded by GOMAXPROCS — blocked M's (e.g., waiting for a syscall) are allowed, and new M's are created as needed. However, the number of M's running Go code simultaneously is bounded by GOMAXPROCS.

**P (Processor / Logical Processor)**: A run queue + scheduling context. Each P holds a local run queue of runnable goroutines (up to 256, a circular buffer). A P is needed to run a G on an M. There are exactly GOMAXPROCS P's. The key insight: P is the unit of parallelism.

**Scheduling flow**:
1. `go f()` creates a new G and puts it in the current P's local run queue.
2. If the local queue is full (256), half the local queue is moved to the global run queue.
3. The current M runs G's from its P's local queue.
4. When a G makes a blocking syscall, the M detaches from the P and blocks. Another M (or a new M) attaches to the P to continue running other G's. When the syscall returns, the original M tries to re-attach to a P; if none is available, the G goes to the global run queue and the M goes to the thread cache.
5. When a P's local queue is empty, the P first checks the global run queue (once every 61 local schedule ticks), then steals half the run queue from a random other P.

**Goroutine preemption** (Go 1.14+): The Go runtime sends `SIGURG` signals to running goroutines on a 10ms ticker (the `sysmon` goroutine). The signal handler sets a flag that causes the goroutine to be preempted at the next "async preemption point" — essentially any instruction, but the runtime needs to be able to unwind the stack safely (this is why the preemption uses a signal that triggers at a safe point in the compiler-generated output).

### Goroutine Parking and Waking

When a goroutine blocks (on a channel, mutex, or network I/O), it parks:
1. The goroutine's G is moved from "running" to "waiting" state.
2. The current M releases the G and picks up the next G from the P's run queue.
3. The blocked resource (channel, mutex, net conn) stores a reference to the parked G.

When the resource is ready (data arrives on the channel, mutex is released, I/O is ready), the blocked G is woken:
1. The G is moved from "waiting" to "runnable" state.
2. The G is placed in a P's run queue (usually the P that woke it, for cache affinity).
3. The scheduler eventually picks up the G on an M.

This park/wake mechanism is the foundation of all blocking operations in Go. `time.Sleep`, `chan<-`, `<-chan`, `sync.Mutex.Lock()`, `net.Conn.Read()` all ultimately call `gopark` / `goready` in the runtime.

### The Netpoller: epoll Integration

Go's network I/O does not block OS threads. Instead, network operations use a **netpoll** goroutine that integrates with the OS event loop (epoll on Linux, kqueue on macOS, IOCP on Windows):

1. `net.Conn.Read()` returns `ErrWouldBlock` if no data is available.
2. The goroutine registers the connection's file descriptor with epoll and parks itself.
3. The netpoller goroutine calls `epoll_wait` (or equivalent). When a file descriptor is ready, the netpoller wakes the parked goroutine by calling `goready`.
4. The woken goroutine retries the read, which now succeeds.

This design allows Go to handle hundreds of thousands of concurrent network connections with GOMAXPROCS OS threads, because blocked goroutines do not consume OS thread resources.

### Tokio's Future and Waker Model

Rust's async model is based on the `Future` trait:

```rust
pub trait Future {
    type Output;
    fn poll(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Self::Output>;
}
```

`poll` is called by the executor when the future may make progress. It returns `Poll::Ready(value)` if complete, or `Poll::Pending` if waiting. When returning `Poll::Pending`, the future **must** arrange for the executor to be notified when it can make progress, via the `Waker` stored in `cx`.

The `Waker` is a callback registered with the I/O system (epoll, io_uring) that calls `wake()` when the resource is ready. When `wake()` is called, the executor places the task (a future + its executor context) in a ready queue. The worker thread polls the future again.

**Tokio's multi-threaded runtime** uses a work-stealing executor:
- Thread pool of `worker_threads` OS threads (default: CPU count)
- Each thread has a local task queue (Chase-Lev deque)
- Woken tasks are placed in the waking thread's local queue
- Idle threads steal from others

**Tokio's io_uring integration** (tokio-uring crate): io_uring is a Linux kernel interface that enables truly asynchronous I/O without system calls in the hot path. Rather than `epoll_wait` (which wakes on "fd is ready, try I/O"), io_uring submits I/O operations to a ring buffer and receives completions from a separate ring buffer. The tokio-uring runtime integrates io_uring for zero-syscall-per-operation I/O on Linux 5.1+.

## Implementation: Go

```go
package main

import (
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// --- Mini work-scheduler demonstrating goroutine park/wake pattern ---
//
// This simulates the park/wake mechanism used by Go's channel and mutex
// implementations. Real runtime code is in go/src/runtime/proc.go.
//
// Race detector: clean. goready/gopark are implemented with mutexes internally.

type parkQueue struct {
	mu      sync.Mutex
	waiting []chan struct{}
}

// park suspends the calling goroutine until wake is called.
// This mirrors gopark() in the Go runtime.
func (q *parkQueue) park() {
	ch := make(chan struct{}, 1)
	q.mu.Lock()
	q.waiting = append(q.waiting, ch)
	q.mu.Unlock()
	<-ch // suspend goroutine (channel receive blocks)
}

// wake unparks one waiting goroutine.
// This mirrors goready() in the Go runtime.
func (q *parkQueue) wake() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.waiting) == 0 {
		return false
	}
	ch := q.waiting[0]
	q.waiting = q.waiting[1:]
	ch <- struct{}{} // unpark the goroutine (channel send)
	return true
}

// --- Minimal task scheduler with goroutine pools ---
//
// Demonstrates the scheduler structure: submit tasks, run them on goroutines,
// show GMP interaction (goroutines park when no task is ready).
//
// Race detector: clean.

type miniScheduler struct {
	tasks    chan func()
	nWorkers int
	done     chan struct{}
	wg       sync.WaitGroup
}

func newMiniScheduler(nWorkers int) *miniScheduler {
	s := &miniScheduler{
		tasks:    make(chan func(), 1000),
		nWorkers: nWorkers,
		done:     make(chan struct{}),
	}
	for i := 0; i < nWorkers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	return s
}

func (s *miniScheduler) worker() {
	defer s.wg.Done()
	for {
		select {
		case task := <-s.tasks:
			task() // execute task; goroutine is "running"
		case <-s.done:
			return // goroutine exits
		}
		// When s.tasks is empty, goroutine parks at the channel receive.
		// The Go runtime calls gopark() on this goroutine.
		// When a task is submitted (send to s.tasks), the goroutine is woken
		// via goready() by the scheduler.
	}
}

func (s *miniScheduler) submit(task func()) {
	s.tasks <- task
}

func (s *miniScheduler) shutdown() {
	close(s.done)
	s.wg.Wait()
}

// --- Demonstrating goroutine stack growth ---
//
// Go goroutines start with 2KB stacks and grow via copying.
// This function demonstrates recursive stack growth.
// The Go runtime doubles the stack size each time it is exhausted,
// copying the entire stack to a new, larger allocation.

func demonstrateStackGrowth(depth int) int {
	if depth == 0 {
		var buf [1024]byte // 1KB on the stack
		_ = buf
		return 0
	}
	return 1 + demonstrateStackGrowth(depth-1)
}

// --- Netpoller integration demo ---
//
// Shows how Go's network I/O uses the netpoller to avoid blocking OS threads.
// The server goroutine parks when waiting for clients (listening on the socket).
// The client goroutines park when waiting for data.

func netpollerDemo() {
	// Start a simple echo server.
	listener, err := net.Listen("tcp", "127.0.0.1:0") // :0 = random port
	if err != nil {
		fmt.Printf("Listen error: %v\n", err)
		return
	}
	addr := listener.Addr().String()
	defer listener.Close()

	var connections atomic.Int32
	var wg sync.WaitGroup

	// Server: accepts connections and echoes data.
	// The Accept() call parks the goroutine via the netpoller.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // listener closed
			}
			connections.Add(1)
			go func(c net.Conn) {
				defer c.Close()
				defer connections.Add(-1)
				buf := make([]byte, 64)
				n, _ := c.Read(buf) // parks goroutine until data arrives
				c.Write(buf[:n])    // echo
			}(conn)
		}
	}()

	// 10 client goroutines connect, send, and read.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			defer conn.Close()
			msg := fmt.Sprintf("hello from %d", id)
			conn.Write([]byte(msg))
			buf := make([]byte, 64)
			n, _ := conn.Read(buf) // parks until echo arrives
			_ = string(buf[:n])
		}(i)
	}

	// Let connections process.
	time.Sleep(100 * time.Millisecond)
	listener.Close()
	wg.Wait()
	fmt.Printf("Netpoller demo: handled connections without blocking OS threads\n")
}

// --- Scheduler yield and GOMAXPROCS interaction ---

func schedulerYieldDemo() {
	nProcs := runtime.GOMAXPROCS(0)
	fmt.Printf("Running with %d P's (GOMAXPROCS)\n", nProcs)

	var count atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()

	// Spawn 4x GOMAXPROCS goroutines; observe work distribution.
	nGoroutines := nProcs * 4
	for i := 0; i < nGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// CPU-bound work. The scheduler preempts via SIGURG after 10ms.
			for j := 0; j < 100_000; j++ {
				count.Add(1)
				if j%10_000 == 0 {
					runtime.Gosched() // explicit yield; allow other goroutines to run
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("Completed %d operations in %v (%d goroutines, %d procs)\n",
		count.Load(), elapsed, nGoroutines, nProcs)
}

func main() {
	// Mini scheduler
	sched := newMiniScheduler(4)
	var taskWg sync.WaitGroup
	for i := 0; i < 20; i++ {
		taskWg.Add(1)
		id := i
		sched.submit(func() {
			defer taskWg.Done()
			time.Sleep(time.Millisecond)
			_ = id
		})
	}
	taskWg.Wait()
	sched.shutdown()
	fmt.Println("Mini scheduler: 20 tasks completed")

	// Stack growth
	depth := demonstrateStackGrowth(500)
	fmt.Printf("Stack grew through %d recursive calls\n", depth)

	// Netpoller
	netpollerDemo()

	// Scheduler yield
	schedulerYieldDemo()
}
```

### Go-specific considerations

**Blocking syscalls and M multiplication**: When a goroutine makes a blocking syscall (e.g., file I/O, a non-net syscall that cannot go through the netpoller), the M executing that goroutine blocks with it. The Go scheduler detects this: the `sysmon` goroutine polls for M's that have been blocked on a syscall for more than 20µs and hands the P to a new M. This prevents one blocked syscall from starving all other goroutines on that P. The implication: Go applications that do heavy file I/O can have more OS threads than GOMAXPROCS (up to 10,000 by default).

**`runtime.LockOSThread()`**: Some operations require a goroutine to stay on the same OS thread (e.g., thread-local storage, CGo callbacks, GUI operations that must run on the main thread). `runtime.LockOSThread()` binds the goroutine to its current M for the duration of the function. The cost: that M is no longer available for other goroutines while the goroutine is running. Use sparingly.

**`context.Context` and goroutine cancellation**: Go has no built-in mechanism to cancel a goroutine externally. The standard is `context.Context`: pass a context to all goroutines, check `ctx.Done()` at cancellation points, and use `ctx.Err()` to detect whether cancellation was requested. The goroutine must cooperate in its own cancellation — this is the Go equivalent of a coroutine's yield point.

## Implementation: Rust

```rust
use std::future::Future;
use std::pin::Pin;
use std::sync::{Arc, Mutex};
use std::task::{Context, Poll, Wake, Waker};
use std::thread;
use std::time::Duration;
use std::collections::VecDeque;
use std::sync::atomic::{AtomicBool, Ordering};

// --- Minimal single-threaded async executor from scratch ---
//
// This demonstrates the core mechanics of a Tokio-style executor.
// A real executor adds: work-stealing, timer support, I/O registration.
//
// The executor uses the park/unpark mechanism of std::thread.

type BoxFuture<T> = Pin<Box<dyn Future<Output = T> + Send + 'static>>;

// Task wraps a future and the waker infrastructure.
struct Task {
    future: Mutex<BoxFuture<()>>,
    ready_queue: Arc<Mutex<VecDeque<Arc<Task>>>>,
}

impl Task {
    fn spawn(future: impl Future<Output = ()> + Send + 'static, queue: &Arc<Mutex<VecDeque<Arc<Task>>>>) -> Arc<Task> {
        let task = Arc::new(Task {
            future: Mutex::new(Box::pin(future)),
            ready_queue: Arc::clone(queue),
        });
        // Push immediately to the ready queue — task is ready to be polled.
        queue.lock().unwrap().push_back(Arc::clone(&task));
        task
    }

    fn poll(self: Arc<Self>) {
        // Create a Waker from the task itself.
        // When wake() is called, the task re-enqueues itself.
        let waker = Waker::from(Arc::clone(&self));
        let mut cx = Context::from_waker(&waker);

        let mut future = self.future.lock().unwrap();
        match future.as_mut().poll(&mut cx) {
            Poll::Ready(()) => { /* task complete */ }
            Poll::Pending => {
                // Task returned Pending. It has registered a waker.
                // When the waker fires, the task will be re-added to the ready queue.
            }
        }
    }
}

// Implement Wake so that Task can be used as a Waker.
// When wake() is called (by the I/O layer or a timer), the task
// is re-enqueued for polling.
impl Wake for Task {
    fn wake(self: Arc<Self>) {
        self.ready_queue.lock().unwrap().push_back(Arc::clone(&self));
    }
}

// Executor: run all tasks to completion.
struct MiniExecutor {
    ready_queue: Arc<Mutex<VecDeque<Arc<Task>>>>,
}

impl MiniExecutor {
    fn new() -> Self {
        MiniExecutor {
            ready_queue: Arc::new(Mutex::new(VecDeque::new())),
        }
    }

    fn spawn(&self, future: impl Future<Output = ()> + Send + 'static) {
        Task::spawn(future, &self.ready_queue);
    }

    // Run until all tasks are complete.
    fn run(&self) {
        loop {
            let task = {
                let mut queue = self.ready_queue.lock().unwrap();
                queue.pop_front()
            };
            match task {
                Some(task) => task.poll(),
                None => {
                    // No tasks ready. In a real executor, the thread would sleep
                    // on an epoll fd or io_uring completion ring.
                    // Here we check once and exit if no tasks are ever re-enqueued.
                    thread::sleep(Duration::from_micros(100));
                    let queue = self.ready_queue.lock().unwrap();
                    if queue.is_empty() {
                        break;
                    }
                }
            }
        }
    }
}

// --- A simple async timer: demonstrates the waker mechanism ---
//
// The timer's poll() returns Pending on the first call and
// arranges for the waker to fire after a delay. When the waker fires,
// the task is re-polled and returns Ready.

struct AsyncTimer {
    deadline: std::time::Instant,
    waker_registered: AtomicBool,
}

impl AsyncTimer {
    fn new(duration: Duration) -> Self {
        AsyncTimer {
            deadline: std::time::Instant::now() + duration,
            waker_registered: AtomicBool::new(false),
        }
    }
}

impl Future for AsyncTimer {
    type Output = ();

    fn poll(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Self::Output> {
        if std::time::Instant::now() >= self.deadline {
            return Poll::Ready(());
        }

        // The timer is not ready. Register the waker to fire when the deadline passes.
        if !self.waker_registered.swap(true, Ordering::Relaxed) {
            let waker = cx.waker().clone();
            let deadline = self.deadline;
            // Spawn an OS thread to fire the waker after the deadline.
            // In Tokio, this is handled by the timer wheel (hashed timer heap).
            thread::spawn(move || {
                let now = std::time::Instant::now();
                if now < deadline {
                    thread::sleep(deadline - now);
                }
                waker.wake(); // re-enqueue the task
            });
        }

        Poll::Pending
    }
}

// --- Async counter: demonstrates multi-future composition ---

async fn async_task(id: usize, counter: Arc<Mutex<usize>>) {
    // Simulate async work with a timer.
    AsyncTimer::new(Duration::from_millis(id as u64 % 10)).await;
    let mut c = counter.lock().unwrap();
    *c += 1;
}

// --- Tokio multi-threaded runtime: the production version ---
//
// In production, use Tokio. The API:
//
// #[tokio::main]
// async fn main() {
//     let (tx, rx) = tokio::sync::mpsc::channel(100);
//
//     // spawn 1000 async tasks — lightweight futures, not OS threads
//     let mut handles = Vec::new();
//     for i in 0..1000 {
//         let tx = tx.clone();
//         handles.push(tokio::spawn(async move {
//             // non-blocking I/O via tokio::net, tokio::fs
//             tokio::time::sleep(Duration::from_millis(i % 100)).await;
//             tx.send(i).await.unwrap();
//         }));
//     }
//     drop(tx);
//
//     // Collect results
//     let mut results = Vec::new();
//     let mut rx = rx;
//     while let Some(v) = rx.recv().await {
//         results.push(v);
//     }
//     println!("Received {} values", results.len());
// }
//
// Tokio spawns worker_threads (default: CPU count) OS threads.
// Tasks are futures that poll to the next .await point.
// Work-stealing balances load across worker threads.
// The netpoller is integrated: tokio::net operations register with epoll/io_uring.

fn main() {
    let executor = MiniExecutor::new();
    let counter = Arc::new(Mutex::new(0usize));

    for i in 0..10 {
        let c = Arc::clone(&counter);
        executor.spawn(async_task(i, c));
    }

    executor.run();

    let final_count = *counter.lock().unwrap();
    println!("Async executor: completed {final_count} tasks (expect 10)");

    // Demonstrate the single-future blocking executor pattern:
    // futures::executor::block_on is the simplest executor — runs one future
    // synchronously on the current thread. Equivalent to Tokio's block_on.
    // Use it only outside async contexts.
    let timer = AsyncTimer::new(Duration::from_millis(10));
    // Blocking version:
    // futures::executor::block_on(timer);
    // For our mini executor, run through the executor:
    executor.spawn(async {
        AsyncTimer::new(Duration::from_millis(10)).await;
        println!("Timer fired after 10ms");
    });
    executor.run();
}
```

### Rust-specific considerations

**`Pin<&mut Self>` and self-referential futures**: Futures compiled from `async fn` can be self-referential — they store references to values that also live inside the future's state. `Pin` prevents the future from being moved in memory after it is pinned, which would invalidate internal references. This is why `poll` takes `Pin<&mut Self>` rather than `&mut Self`. For futures that are not self-referential (stateless futures, futures that do not hold borrows across `.await`), `impl Unpin` can be derived. Most combinators in `futures` and Tokio handle `Pin` internally; application code rarely deals with it directly.

**`async fn` as state machine compilation**: The Rust compiler transforms each `async fn` into a state machine struct. Each `.await` point is a state transition. The state machine is typically 100-500 bytes (the size of the largest variables held across `.await` points). This is why Rust async tasks are significantly more memory-efficient than goroutines: no stack allocation, just the exact variables needed for resumption.

**Blocking in async context**: Tokio's worker threads are expected to run non-blocking code. A `std::thread::sleep` or a blocking file I/O call inside an async task blocks the Tokio worker thread, preventing it from processing other tasks. The fix: use `tokio::task::spawn_blocking` to run blocking code on a dedicated blocking thread pool (separate from the async worker threads). This is a common footgun when porting synchronous code to async.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust (Tokio) |
|--------|-----|------|
| Concurrency model | M:N green threads (goroutines) with preemptive scheduler | Cooperative future/poll model with work-stealing executor |
| Syntax | None required — all code looks sequential | `async`/`.await` keywords; explicit async boundaries |
| Stack | Per-goroutine growing stack (2KB-1GB) | No stack per future; state machine with exact memory footprint |
| Task spawn cost | ~1µs, ~2KB memory | ~50-100ns, ~100-500 bytes (future state) |
| Blocking I/O | Transparent via netpoller; blocks goroutine but not M | Must use `tokio::net`, `tokio::fs`; blocking code requires `spawn_blocking` |
| Preemption | Preemptive (SIGURG since 1.14); goroutines cannot block the scheduler | Cooperative; a long computation without `.await` blocks the worker thread |
| io_uring | Not standard; third-party crates available | `tokio-uring` crate; experimental native io_uring integration |
| Cancellation | `context.Context`; cooperative; goroutine must check `ctx.Done()` | Drop the `JoinHandle` or cancel the `CancellationToken`; future is dropped at next poll |
| GC interaction | Go GC scans goroutine stacks; scheduling pauses during STW | No GC; futures dropped deterministically via RAII |

## Production War Stories

**Go's sysmon and the scheduler preemption saga**: Before Go 1.14, goroutine preemption was cooperative — a goroutine was preempted only at function call sites, where the compiler inserted preemption checks. A goroutine in a tight loop (no function calls, e.g., a matrix multiply with inlined arithmetic) would monopolize a P indefinitely. This caused observable latency spikes in production services during batch computation. Go 1.14 added asynchronous preemption: `sysmon` sends `SIGURG` to the running goroutine's thread; the signal handler cooperates with the GC to save the goroutine's registers and switch to another goroutine. This was a multi-year project that required changes to the compiler, the GC, and the signal handling infrastructure.

**Tokio task panics and the JoinHandle drop**: A Tokio `spawn`ed task is detached by default — if you drop the `JoinHandle` without awaiting it, the task continues running. This is Go's goroutine behavior. The difference: if a Tokio task panics, the panic is contained to that task (the `JoinHandle::await` returns `Err(JoinError)`). If a Go goroutine panics, it propagates to the top of the goroutine's stack and, if unhandled, crashes the entire process. This is a production footgun in Go: every goroutine needs a `defer recover()` if it might panic and should not crash the service.

**The `async fn` in trait stabilization saga (Rust)**: Async functions in traits were unstable for years because of the `dyn Trait` compatibility issue: `async fn foo()` in a trait expands to `fn foo() -> impl Future`, and `impl Future` is an unnameable type that prevents dynamic dispatch (`dyn Trait`). The `async-trait` proc-macro crate worked around this by boxing the return future. `async fn` in traits was stabilized in Rust 1.75 (2023), but the `dyn Trait` case still requires workarounds. This affected library design for the first 7 years of Rust async, forcing patterns like `BoxFuture<'_, T>` and the `async-trait` crate.

**io_uring and Tokio's architecture tension**: io_uring's async I/O model differs fundamentally from epoll's: epoll is "readiness-based" (notify when I/O can proceed; the application calls `read`/`write`); io_uring is "completion-based" (submit I/O requests; receive completions). Tokio's `AsyncRead`/`AsyncWrite` traits are designed around the readiness model. Adapting them to io_uring requires a different API (`tokio-uring` provides `read_at`/`write_at` instead of `AsyncRead`/`AsyncWrite`). This architectural mismatch is a known limitation and is driving discussion about a more general async I/O trait design in the Rust ecosystem.

## Complexity Analysis

- **Goroutine spawn**: O(1) amortized. Stack allocation from a per-P pool (~1µs, ~2KB). No OS thread creation.
- **Goroutine context switch**: O(1), ~200-400ns on modern hardware. Saves/restores register set, switches stack pointer. Faster than OS thread context switch (~1-10µs) because no kernel transition is required.
- **Tokio task spawn**: O(1), ~50-100ns. Allocates the future state machine on the heap (Box), pushes to a local queue. No stack allocation.
- **Tokio context switch** (`.await` point): O(1), ~10-50ns. Saves the future state (which was already on the heap), pops the next task from the queue. Extremely cheap because there is no stack to save/restore.
- **epoll_wait throughput**: O(1) per event. Can report thousands of ready file descriptors in a single call. The netpoller's `epoll_wait` is called with a 10ms timeout, bounding netpoll overhead to ~0.1% CPU overhead even at 100k idle connections.
- **io_uring submission overhead**: O(1) per operation, but amortized over batch submissions. The ring buffer allows N operations to be submitted with 1 system call (`io_uring_enter`), reducing per-operation syscall overhead to near zero for batched I/O.

## Common Pitfalls

**1. CPU-bound work in Tokio async tasks.** Tokio's worker threads are designed for tasks that spend most time waiting at `.await` points. A long CPU computation without `.await` starves other tasks on the same worker thread (Tokio is cooperative, not preemptive). Fix: use `tokio::task::spawn_blocking` for CPU-intensive work, or `rayon` for CPU parallelism with a separate thread pool. The rule: no more than ~10µs of CPU time per `.await` point in Tokio tasks.

**2. Blocking I/O in Tokio tasks.** `std::fs::File::read()`, `std::net::TcpStream::read()` (synchronous), and any function that calls `sleep()` are blocking calls. They block the Tokio worker thread. Use `tokio::fs::File` and `tokio::net::TcpStream` for I/O, and `tokio::time::sleep()` for timers. `tokio::task::spawn_blocking` is for third-party blocking code that cannot be easily replaced.

**3. Goroutine leaks.** In Go, it is easy to create a goroutine that is never collected: a goroutine blocked on a channel that nobody will ever send to. In large services, goroutine leaks manifest as gradually increasing memory usage and goroutine count. The fix: always provide a cancellation mechanism (`context.Context` timeout, a `done` channel) for goroutines that do blocking work. Profile with `pprof` goroutine stack dumps.

**4. Forgetting that `select {}` in Go is a deadlock, not an idle wait.** `select {}` with no cases blocks forever and is a deadlock (the goroutine is never woken). Use `select { case <-ctx.Done(): return }` for a goroutine that should wait until cancellation.

**5. The Tokio async runtime inside a sync context.** Calling `tokio::runtime::Handle::block_on()` inside another Tokio async context panics: "Cannot start a runtime from within a Tokio runtime." The fix: use `tokio::spawn` to bridge; or use `futures::executor::block_on` for a single future outside any executor; or use `tokio::task::block_in_place` for the rare case of sync code inside async.

## Exercises

**Exercise 1** (30 min): Write a Go program that creates 1,000,000 goroutines, each sleeping for 100ms and incrementing a counter. Observe: memory usage (each goroutine ~2KB = ~2GB for 1M goroutines), creation time (~1µs each = ~1s), and scheduling overhead. Then rewrite using a bounded pool of 1000 goroutines sharing a channel of work items. Compare memory and throughput. This is the "goroutine pooling" question that comes up in every Go performance interview.

**Exercise 2** (2-4h): Implement a single-threaded async runtime in Rust that supports: `spawn(future)` to enqueue a future, `sleep(duration)` as a future that fires after a duration, and `join!(f1, f2)` to run two futures concurrently. The runtime should be single-threaded (no `Arc<Mutex>` needed). Test with 1000 concurrent sleep futures of varying durations. Verify that the total wall time equals the maximum sleep time, not the sum.

**Exercise 3** (4-8h): Implement a minimal HTTP/1.1 server in Go using `net.Listener` and goroutines-per-connection. Profile with `pprof` at 10,000 concurrent connections (use `wrk` or `hey` as the load generator). Identify: the goroutine count, the memory usage per connection, the CPU time in the scheduler. Then rewrite using a goroutine pool with a bounded connection queue. Compare: request throughput, p99 latency, memory usage.

**Exercise 4** (8-15h): Read the Tokio source: `tokio/src/runtime/task/` (task lifecycle), `tokio/src/runtime/scheduler/multi_thread/` (work-stealing executor), `tokio/src/io/driver/` (epoll integration). Write a design document explaining: how a `tokio::spawn`ed task moves from spawned → ready → polling → pending → woken → ready; where the waker is stored; how the work-stealing executor picks up woken tasks; how the epoll integration converts file descriptor readiness to waker calls. Then compare this to Go's goroutine park/wake cycle (`gopark`, `goready` in `go/src/runtime/proc.go`).

## Further Reading

### Foundational Papers

- Adya, A., Howell, J., Theimer, M., Bolosky, W. & Douceur, J. (2002). "Cooperative Task Management Without Manual Stack Management." *USENIX ATC 2002* — The paper underlying cooperative async models (including Rust futures).
- Ousterhout, J. (1995). "Why Threads Are A Bad Idea (for most purposes)." *USENIX Technical Conference* — The case for event-driven (async) programming. Historical context for async/await.

### Books

- Klabnik, S. & Nichols, C. *The Rust Programming Language* (2nd ed., 2022) — Chapter 16: fearless concurrency; Chapter 20 (online): async/await internals.
- Cox-Buday, K. *Concurrency in Go* (O'Reilly, 2017) — Chapter 3: Go's concurrency building blocks; Chapter 6: goroutines and the Go runtime.

### Production Code to Read

- `go/src/runtime/proc.go` — The Go scheduler (5000 lines, extensively commented). Start at `schedule()`, `gopark()`, `goready()`, and `findrunnable()`.
- `tokio/src/runtime/task/mod.rs` — Task lifecycle and waker implementation.
- `tokio/src/runtime/scheduler/multi_thread/worker.rs` — Tokio's work-stealing worker loop.
- Go netpoller: `go/src/runtime/netpoll_epoll.go` (Linux) or `go/src/runtime/netpoll_kqueue.go` (macOS).

### Talks

- "Go: A Look Behind the Scenes" — Dmitry Vyukov (GopherCon 2014) — The GMP scheduler explained by its principal author.
- "How Tokio Works Internally" — Carl Lerche (RustConf 2019) — The future/waker model and io_uring integration.
- "Thinking about Concurrency" — Rob Pike (Golang Talks 2012) — The philosophical case for goroutines over threads.
