# 41. Build a Profiler

**Difficulty**: Insane

## The Challenge

Every developer eventually asks "why is my program slow?" — and the answer lives in the
profiler. A profiler is a tool that observes a running program, records where it spends its time,
and presents that information in a way that makes performance bottlenecks visible. Production
profilers like `perf`, `pprof`, and Instruments are sophisticated pieces of systems software
that combine hardware performance counters, signal-based sampling, stack unwinding through
optimized code, symbol resolution from debug info, and visualization — all while adding minimal
overhead to the target program. Your mission is to build a profiler from scratch in Rust that
combines two complementary techniques: **sampling profiling** (periodically interrupting the
program to record its call stack) and **instrumentation profiling** (injecting measurement code
at function entry and exit via a proc-macro). The profiler must collect call stacks from a
running program, resolve them to function names and source locations, and generate a flamegraph
SVG that visualizes where time is being spent.

Sampling profilers work by periodically interrupting the target program — typically via the
`SIGPROF` signal on Unix or the `perf_event_open` system call on Linux — and capturing the
current call stack at each interruption. The call stack is a sequence of return addresses, one
per active function call. These raw addresses must be resolved to human-readable function names
using symbol tables (from the ELF binary) or DWARF debug information. Collecting thousands of
samples and aggregating them produces a statistical profile: functions that appear in many samples
consume proportionally more CPU time. The genius of sampling profilers is that they add
negligible overhead — interrupting a program 100 times per second to capture a stack adds less
than 1% overhead, yet produces a statistically accurate profile. Your sampling profiler will use
either `SIGPROF`/`setitimer` (portable, simpler) or `perf_event_open` (Linux-specific, more
accurate, hardware-counter support) to generate periodic interrupts, walk the stack using frame
pointers or DWARF CFI, and collect samples in a lock-free ring buffer.

Instrumentation profilers take the opposite approach: instead of sampling, they measure every
function call precisely. You will build a `#[profile]` proc-macro attribute that, when applied to
a function, injects timing code at entry and exit — recording the function name, entry timestamp,
exit timestamp, thread ID, and call depth. This gives exact call counts and durations (no
sampling noise) at the cost of higher overhead. The proc-macro rewrites the function body to wrap
it in a `Guard` that records the entry time on creation and the exit time on drop, handling all
exit paths including early returns, panics, and `?` operators. The instrumentation data feeds
into the same aggregation and visualization pipeline as the sampling data, producing flamegraphs
that show both sampled CPU time and exact instrumented durations. Building both approaches in one
tool teaches you the fundamental tradeoff in profiling: low overhead with statistical accuracy
(sampling) versus exact measurement with higher overhead (instrumentation).

## Acceptance Criteria

### Signal-Based Sampling
- [ ] Implement sampling using `setitimer(ITIMER_PROF)` and a `SIGPROF` signal handler, which fires when the process consumes CPU time (not wall-clock time)
- [ ] The sampling rate is configurable (default: 99 Hz — a prime number that avoids aliasing with periodic program behavior)
- [ ] The signal handler captures the current instruction pointer (from `ucontext_t.uc_mcontext`) and walks the call stack
- [ ] The signal handler is async-signal-safe: it does NOT allocate memory, acquire locks, or call non-reentrant functions. All data is written to a pre-allocated lock-free ring buffer
- [ ] The signal handler completes in bounded time — no unbounded loops or recursion within the handler
- [ ] Implement `sigaction` setup with `SA_SIGINFO | SA_RESTART` flags to receive the `ucontext_t` and avoid EINTR on interrupted system calls
- [ ] The profiler can be started and stopped dynamically: `Profiler::start()` arms the timer, `Profiler::stop()` disarms it and flushes collected samples
- [ ] Support profiling the current process (self-profiling) — the signal handler runs within the profiled process

### perf_event_open Sampling (Linux)
- [ ] Implement an alternative sampling backend using the `perf_event_open(2)` system call (Linux-specific, conditionally compiled with `#[cfg(target_os = "linux")]`)
- [ ] Configure a hardware performance counter event: `PERF_TYPE_HARDWARE` with `PERF_COUNT_HW_CPU_CYCLES`, sampling every N cycles (equivalent to ~99 Hz)
- [ ] Enable stack trace collection in the perf event: set `sample_type` to include `PERF_SAMPLE_IP | PERF_SAMPLE_CALLCHAIN | PERF_SAMPLE_TID | PERF_SAMPLE_TIME`
- [ ] Memory-map the perf ring buffer using `mmap` on the perf event file descriptor, and read samples from the ring buffer using the kernel's data_head/data_tail protocol
- [ ] Handle ring buffer wrapping correctly: when `data_head` wraps around the ring buffer, read wrapping entries across the boundary
- [ ] The perf backend provides hardware-accurate timestamps and avoids the signal handler overhead
- [ ] Support configurable event types: CPU cycles, instructions retired, cache misses, branch mispredictions — expose these as an enum in the profiler API

### Stack Unwinding (Frame Pointer)
- [ ] Implement frame-pointer-based stack walking: starting from the current frame pointer (`rbp` on x86_64), read the saved frame pointer and return address at each frame, following the chain until a null frame pointer or an invalid address is encountered
- [ ] The stack walker produces a sequence of return addresses (instruction pointers), one per frame
- [ ] Handle the top-of-stack frame correctly: the current IP comes from the signal context (`ucontext_t`), not from a frame pointer dereference
- [ ] Detect and stop at invalid frame pointers: null, misaligned (not 16-byte aligned on x86_64), pointing outside the stack region, or pointing backward (lower address than the current frame)
- [ ] Limit stack depth to a configurable maximum (default: 128 frames) to bound the work done in the signal handler
- [ ] The frame-pointer walker works correctly only when the target is compiled with `-C force-frame-pointers=yes` — document this requirement
- [ ] Implement a fallback for functions without frame pointers: detect the missing frame pointer (misaligned chain) and stop unwinding gracefully rather than producing garbage frames

### Stack Unwinding (DWARF CFI — Stretch Goal)
- [ ] Implement DWARF Call Frame Information (CFI) based unwinding as an alternative to frame pointers
- [ ] Parse the `.eh_frame` section from the ELF binary to extract CFI entries (CIE and FDE records)
- [ ] For each instruction pointer, find the corresponding FDE and evaluate the DWARF CFI program (a stack-based bytecode) to determine the CFA (Canonical Frame Address) and the location of saved registers
- [ ] Use the computed CFA and saved register locations to unwind each frame
- [ ] DWARF unwinding works even for code compiled without frame pointers (the common default in release mode)
- [ ] Alternatively, use the `gimli` crate's unwinding support to parse `.eh_frame` and evaluate CFI programs, rather than implementing the DWARF state machine from scratch
- [ ] Benchmark DWARF unwinding versus frame-pointer unwinding: DWARF is slower (CFI evaluation per frame) but more general

### Symbol Resolution
- [ ] Given a return address (instruction pointer), resolve it to a function name, source file, and line number
- [ ] Parse the ELF symbol table (`.symtab` and `.dynsym` sections) to map addresses to function names: for each address, find the symbol whose range `[st_value, st_value + st_size)` contains it
- [ ] Use the `addr2line` crate (which wraps `gimli`) to resolve addresses to source file and line number using DWARF debug info (`.debug_info`, `.debug_line` sections)
- [ ] Demangle Rust symbols: the raw symbol name like `_ZN4myapp7process17hf3e8a0b1c2d3e4f5E` should be displayed as `myapp::process` using `rustc_demangle` crate
- [ ] Also demangle C++ symbols (`__cxa_demangle` equivalent) for profiling mixed Rust/C++ programs
- [ ] Handle stripped binaries gracefully: if no debug info is available, display the raw address in hexadecimal with the containing library name (e.g., `libc.so.6+0x12345`)
- [ ] Resolve symbols from shared libraries: parse `/proc/self/maps` (Linux) or use `dladdr` to find which library contains each address, then resolve within that library using its base address offset
- [ ] Cache symbol resolutions: resolving the same address repeatedly should be O(1) after the first lookup. Use a `HashMap<usize, SymbolInfo>` populated lazily

### Lock-Free Sample Collection
- [ ] Implement a lock-free SPSC (single-producer, single-consumer) ring buffer for storing samples from the signal handler (producer) to the processing thread (consumer)
- [ ] The ring buffer is pre-allocated at profiler start and is never resized during profiling
- [ ] The producer (signal handler) writes samples using only atomic stores and memory fences — no locks, no allocation, no syscalls
- [ ] The consumer (processing thread) reads samples using atomic loads and processes them (symbol resolution, aggregation) outside the signal handler context
- [ ] Each sample is a fixed-size struct: `Sample { timestamp: u64, thread_id: u64, stack: [usize; MAX_STACK_DEPTH], depth: u32 }`
- [ ] Handle ring buffer overflow gracefully: if the producer outpaces the consumer, drop the oldest samples (overwrite) and increment an overflow counter rather than blocking
- [ ] Use `AtomicUsize` for head (producer) and tail (consumer) with `Ordering::Release` for stores and `Ordering::Acquire` for loads
- [ ] The ring buffer capacity is a power of two to enable efficient modular indexing with bitwise AND

### Thread-Safe Multi-Threaded Profiling
- [ ] Profile all threads in the process, not just the main thread
- [ ] Use `timer_create` with `SIGEV_THREAD_ID` (Linux) or `pthread_kill` to deliver `SIGPROF` to each thread individually
- [ ] Alternatively, use `setitimer(ITIMER_PROF)` which distributes the signal across threads consuming CPU time (kernel-scheduled, less precise per-thread)
- [ ] Each thread's signal handler writes to a shared lock-free ring buffer (or per-thread ring buffers for zero contention)
- [ ] Aggregate samples across threads: the final profile can show per-thread flamegraphs or a merged view
- [ ] Record the thread ID with each sample for per-thread analysis
- [ ] Handle thread creation and destruction during profiling: new threads are automatically profiled, exiting threads flush their samples

### Instrumentation via Proc-Macro
- [ ] Implement a `#[profile]` attribute proc-macro that transforms a function to record entry/exit timing
- [ ] The macro wraps the function body in a block that creates a `ProfileGuard` on entry: `let _guard = ProfileGuard::new(function_name, module_path);`
- [ ] `ProfileGuard::new` records the entry timestamp (`std::time::Instant::now()` or `rdtsc` for lower overhead)
- [ ] `ProfileGuard::drop` records the exit timestamp and submits a `SpanRecord { name, module, thread_id, enter_time, exit_time, depth }` to the collector
- [ ] The macro preserves the function's original signature, visibility, attributes, generics, and return type exactly
- [ ] The macro handles async functions: for `async fn`, wrap the returned future, not just the synchronous preamble
- [ ] The macro handles functions with early returns (`return`), `?` operator, and panics — the `Drop` guard ensures exit is always recorded
- [ ] Provide a `#[profile_all]` attribute for `impl` blocks that applies `#[profile]` to every method in the block
- [ ] The instrumentation can be compiled out entirely via a feature flag: `#[cfg(feature = "profiling")]` — when the feature is disabled, `#[profile]` is a no-op that does not affect the function body

### Instrumentation Data Collection
- [ ] `SpanRecord` entries are collected in a thread-local `Vec` to avoid synchronization overhead during collection
- [ ] Thread-local buffers are flushed to a global collector when they reach a capacity threshold (default: 4096 entries) or when profiling stops
- [ ] The global collector merges span records from all threads into a unified timeline
- [ ] Support call-depth tracking: each `ProfileGuard::new` increments a thread-local depth counter, each drop decrements it. The depth is stored in the `SpanRecord` for building call trees
- [ ] Total instrumentation overhead per function call should be under 100 nanoseconds (measure with a benchmark)
- [ ] An `InstrumentedProfile` can be built from the collected spans, showing: per-function total time, self time (excluding children), call count, min/max/average duration

### Flamegraph Generation
- [ ] Generate flamegraphs in SVG format from the collected samples or instrumentation spans
- [ ] Each stack trace is a sequence of function names from bottom (main) to top (leaf function). Aggregate identical stack traces and count their frequency
- [ ] The SVG flamegraph layout: each function is a rectangle whose width is proportional to the number of samples in which it appears. Functions are stacked vertically showing the call chain. Rectangles are horizontally sorted alphabetically (not by time) for stable, diff-friendly output
- [ ] Each rectangle includes: function name (truncated if too narrow), fill color (a warm color palette — hue varies by function name hash for visual distinction), and a tooltip showing the function name, sample count, and percentage
- [ ] The SVG is interactive: clicking a frame zooms into that subtree, showing only its callees. A "Reset Zoom" button returns to the full view. This requires embedded JavaScript in the SVG
- [ ] Support both "icicle" (top-down, root at top) and "flame" (bottom-up, root at bottom) orientations
- [ ] The SVG output is self-contained (no external dependencies) and viewable in any web browser
- [ ] For instrumentation data, generate a timeline/span view in addition to the flamegraph: a horizontal timeline where each function call is a rectangle whose width represents its duration, with nesting showing call depth

### Flamegraph Folded Format
- [ ] Output collected profiles in the "folded stacks" text format: each line is `frame1;frame2;frame3;...;leaf_frame count\n` where count is the number of samples with that exact stack
- [ ] This format is compatible with the `inferno` crate and Brendan Gregg's `flamegraph.pl` tools
- [ ] The folded format can be loaded by other profiling tools for visualization or comparison
- [ ] Support reading folded stacks from a file and generating a flamegraph from them (enabling interop with `perf script` output piped through `stackcollapse-perf.pl`)

### Profiler API
- [ ] Provide a high-level API for self-profiling: `let profiler = Profiler::builder().sample_rate(99).build();` `profiler.start();` `// ... code ...` `profiler.stop();` `profiler.flamegraph("output.svg")?;`
- [ ] Provide a RAII guard for scoped profiling: `let _guard = profiler.scope("section_name");` which starts/stops profiling when created/dropped
- [ ] Provide a `report()` method that returns a structured `Profile` containing: total samples, total duration, per-function statistics (total time, self time, call count), and the aggregated stack traces
- [ ] The profiler is `Send + Sync` and can be shared across threads via `Arc<Profiler>`
- [ ] Support merging profiles from multiple runs: `Profile::merge(&[profile1, profile2])` combines samples from different profiling sessions

### Testing
- [ ] Unit test: the lock-free ring buffer correctly handles concurrent produce/consume without data loss or corruption under heavy load (1M samples, verify all received)
- [ ] Unit test: ring buffer overflow correctly drops oldest samples and increments the overflow counter
- [ ] Unit test: symbol resolution correctly maps a known function's address to its name (take the address of a `#[no_mangle]` function and resolve it)
- [ ] Unit test: the `#[profile]` proc-macro preserves function behavior — a profiled function returns the same values as the original
- [ ] Unit test: the proc-macro correctly handles generics, lifetimes, async functions, and functions with `?` operator
- [ ] Unit test: flamegraph SVG output is valid XML and contains the expected function names
- [ ] Integration test: profile a known CPU-intensive workload (e.g., computing Fibonacci recursively) and verify the flamegraph shows the recursive function dominating
- [ ] Integration test: profile a multi-threaded workload and verify samples are collected from all threads
- [ ] Integration test: the folded stacks output is compatible with `inferno::flamegraph::from_reader`
- [ ] Performance test: profiling overhead is less than 5% at 99 Hz sampling rate on a CPU-bound workload (measure wall-clock time with and without profiling)
- [ ] Performance test: `#[profile]` instrumentation adds less than 100ns per function call (benchmark with `criterion`)

### Build and Workspace
- [ ] Workspace with crates: `profiler-core` (sampling, collection, aggregation), `profiler-macro` (the `#[profile]` proc-macro), `profiler` (public API re-exporting both), `profiler-examples` (example programs to profile)
- [ ] The proc-macro crate is `proc-macro = true` in Cargo.toml and depends on `syn`, `quote`, and `proc-macro2`
- [ ] Platform-specific code is behind `#[cfg(target_os = "linux")]` and `#[cfg(target_os = "macos")]` with clear feature gates
- [ ] The profiler compiles and provides at least signal-based sampling on both Linux and macOS (`setitimer`/`SIGPROF` works on both)
- [ ] `perf_event_open` support is Linux-only and behind a `perf` feature flag
- [ ] Example programs demonstrate: self-profiling a CPU workload, profiling a multi-threaded program, using the `#[profile]` macro, generating a flamegraph SVG

## Starting Points

- **pprof-rs**: https://github.com/tikv/pprof-rs — a production Rust sampling profiler used by TiKV. Study its signal handler implementation (`profiler.rs`), stack unwinding (`backtrace.rs`), and sample aggregation. This is the closest existing Rust implementation to what you are building. Pay particular attention to how it handles async-signal-safety in the SIGPROF handler
- **inferno crate**: https://github.com/jonhoo/inferno — a Rust port of Brendan Gregg's flamegraph tools. Study the `flamegraph` module for SVG generation from folded stacks. You can use this crate as a reference for your SVG output, or use it directly for generating the final visualization while you focus on the collection infrastructure
- **Brendan Gregg's Flamegraph tools and blog**: https://www.brendangregg.com/flamegraphs.html — the definitive resource on flamegraph visualization. Gregg invented flamegraphs and his blog explains the design decisions (alphabetical sorting, warm color palettes, interactive zoom) and the folded-stacks format
- **perf_event_open(2) man page**: https://man7.org/linux/man-pages/man2/perf_event_open.2.html — the Linux system call for hardware performance counter access. This is a complex system call with many options; focus on the `PERF_TYPE_HARDWARE` / `PERF_COUNT_HW_CPU_CYCLES` configuration and the ring buffer (mmap) interface for reading samples
- **The `perf` tool source code**: https://github.com/torvalds/linux/tree/master/tools/perf — the Linux kernel's userspace profiling tool. Study `builtin-record.c` for how it configures perf events, `util/evsel.c` for event selection, and `util/session.c` for reading the mmap ring buffer. This is the reference implementation for perf_event_open usage
- **`addr2line` crate**: https://docs.rs/addr2line — Rust library for resolving instruction addresses to file/line using DWARF debug info. Built on top of `gimli` (a pure-Rust DWARF parser). Study its `Context::find_frames()` API for resolving addresses, including inlined function frames
- **`gimli` crate**: https://docs.rs/gimli — a pure-Rust DWARF parser. If you implement DWARF-based unwinding, you will use `gimli::UnwindSection` to parse `.eh_frame` and `gimli::UnwindContext` to evaluate CFI programs. The crate is well-documented with examples for each section type
- **`rustc_demangle` crate**: https://docs.rs/rustc-demangle — demangling for Rust symbol names. `rustc_demangle::demangle("_ZN4myapp7process17hf3e8a0b1c2d3e4f5E")` produces `myapp::process`. Essential for human-readable output
- **`signal-hook` crate**: https://docs.rs/signal-hook — safe Rust abstractions over Unix signals. While you may need to use raw `sigaction` for the SIGPROF handler (signal-hook does not support ucontext access), studying its design shows how to handle signals safely in Rust
- **"Profiling Rust Programs" by Nicholas Nethercote**: https://nnethercote.github.io/perf-book/profiling.html — practical guide to profiling Rust programs with existing tools. Understanding how `perf`, `samply`, and `cargo-flamegraph` work from the user's perspective will inform your profiler's design and output format

## Hints

1. Start with the simplest possible sampling loop: `setitimer(ITIMER_PROF, ...)` at 99 Hz with a SIGPROF handler that increments an `AtomicU64` counter. Verify the counter matches the expected sample count (approximately 99 * elapsed_cpu_seconds). This confirms your signal delivery is working before you add stack walking
2. The SIGPROF signal handler has severe constraints: it runs on the interrupted thread's stack, at an arbitrary point in the program's execution. Any function you call must be async-signal-safe (see `signal-safety(7)` man page). This rules out `malloc`, `free`, `printf`, mutex operations, and most of libc. Your handler must write to pre-allocated memory using only atomic operations and direct system calls
3. For frame-pointer-based stack walking in the signal handler, you need the frame pointer from the interrupted context. On x86_64 Linux, this is `ucontext.uc_mcontext.gregs[REG_RBP]` for the frame pointer and `ucontext.uc_mcontext.gregs[REG_RIP]` for the instruction pointer. On macOS, it is `ucontext.uc_mcontext.__ss.__rbp` and `__rip`. Both require platform-specific code
4. The frame pointer chain is: `rbp` points to the saved previous `rbp`, and `rbp + 8` (on x86_64) points to the return address. Walk: `ip = *(rbp + 8)`, `next_rbp = *rbp`, repeat. Stop when `rbp` is null, misaligned, or outside the stack. This all happens via raw pointer reads in the signal handler — these reads are safe as long as the frame pointer chain is valid (which requires `-C force-frame-pointers=yes`)
5. For the lock-free ring buffer, use a power-of-two sized array with `head` (write index) and `tail` (read index) as `AtomicUsize`. The producer writes at `head % capacity` and increments `head`. The consumer reads at `tail % capacity` and increments `tail`. The buffer is full when `head - tail == capacity`. Use `Release` ordering for updates and `Acquire` for reads to ensure the data is visible
6. Symbol resolution should happen OUTSIDE the signal handler, on the processing thread. The signal handler records raw addresses; a separate thread (or post-processing step) resolves them to symbols. This keeps the signal handler fast and async-signal-safe
7. For the `#[profile]` proc-macro, use `syn` to parse the function, extract its body, and generate a new body: `{ let _guard = ProfileGuard::new(concat!(module_path!(), "::", stringify!(fn_name))); { #original_body } }`. The guard's Drop impl records the exit time. Using `concat!` and `stringify!` means the function name is a compile-time string, not a runtime allocation
8. For async function support in the proc-macro, the challenge is that `async fn foo() { ... }` desugars to `fn foo() -> impl Future`. The guard must live for the duration of the future's execution, not just the synchronous function call. Wrap the entire async block: `async move { let _guard = ProfileGuard::new(...); #original_async_body }` and return this as the function's future
9. For flamegraph generation, the algorithm is: (a) aggregate all stack traces by counting identical traces, (b) sort frames alphabetically at each level, (c) for each frame, compute its width as (number of samples containing this frame at this depth / total samples), (d) render each frame as an SVG rectangle with proportional width. The y-axis is call depth, the x-axis is alphabetical order (NOT time)
10. The interactive zoom in the SVG flamegraph works by embedding a small JavaScript function: when a rectangle is clicked, all rectangles that are not descendants of the clicked frame are hidden, and the clicked frame's width is expanded to 100%. A "Reset Zoom" button restores the original view. Study the `inferno` crate's SVG output or Brendan Gregg's `flamegraph.pl` for the exact JavaScript
11. For `perf_event_open`, the ring buffer protocol is: the kernel writes to a memory-mapped ring buffer and updates `data_head` (an atomic u64 at the start of the mmap). Your consumer reads from `data_tail` to `data_head`, processes the records, then updates `data_tail` (using a memory barrier before the write). Records can wrap around the buffer boundary — handle this by copying the record to a contiguous temporary buffer when it wraps
12. `rdtsc` (read timestamp counter) is the lowest-overhead timer available on x86_64 — it reads the CPU's cycle counter in a single instruction (~25 cycles). For instrumentation profiling, use `rdtsc` instead of `Instant::now()` to minimize overhead. Convert cycles to nanoseconds using the CPU's TSC frequency (read from `/proc/cpuinfo` or `cpuid`). Access `rdtsc` via `core::arch::x86_64::_rdtsc()` (requires `unsafe`)
13. To test the profiler's accuracy, profile a function with a known CPU cost (e.g., a tight loop that runs for exactly 1 second of CPU time). Verify that the profiler reports approximately 1 second of total time for that function, with the sampling-based profile showing the function in approximately `sample_rate * 1` samples
14. For multi-threaded profiling with `setitimer`, be aware that `ITIMER_PROF` delivers `SIGPROF` to whichever thread is running when the timer fires. On a multi-threaded program, this means samples are distributed across threads proportionally to their CPU usage — which is actually the correct behavior for a CPU profiler. If you need per-thread control, use `timer_create` with `SIGEV_THREAD_ID` (Linux-specific)
15. Handle stack traces from signal handlers, PLT stubs, and runtime functions (like the Rust panic machinery or the allocator) gracefully. These show up as "noise" in the profile and can be filtered out in the aggregation step. Provide a `filter` option that removes frames matching specified patterns (e.g., `__GI_*`, `_sigtramp`, `__rust_*`)
16. For macOS support, use `setitimer(ITIMER_PROF)` for signal-based sampling (perf_event_open does not exist on macOS). The `ucontext_t` structure layout differs from Linux — use `#[cfg(target_os = "macos")]` blocks for the platform-specific mcontext access. On Apple Silicon (aarch64), the frame pointer is `x29` and the link register is `x30` (there is no `rbp`/`rip`), requiring architecture-specific unwinding code
