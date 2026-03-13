<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Coroutine Library

## The Challenge

Build a stackful coroutine library in Go that provides cooperative multitasking within a single goroutine, allowing functions to yield execution and be resumed later at the exact point where they yielded. Unlike goroutines which are preemptively scheduled by the Go runtime, your coroutines are cooperatively scheduled by your library, giving the programmer explicit control over context switches. Your library must support creating coroutines from any function, yielding values (generator pattern), bidirectional communication between the caller and coroutine (send and receive), coroutine state management (created, running, suspended, dead), and a round-robin scheduler that can run multiple coroutines on a single OS thread. This exercise explores the duality between coroutines and channels, and teaches the mechanics of cooperative scheduling.

## Requirements

1. Implement `Coroutine[Send, Yield]` that wraps a function `func(co *CoroutineContext[Send, Yield])`, where the function can call `co.Yield(value Yield) Send` to suspend execution and return a value to the caller, and later be resumed with a sent value.
2. Implement `New[S, Y](func(co *CoroutineContext[S, Y])) *Coroutine[S, Y]` that creates a coroutine in the `Created` state without starting execution.
3. Implement `Resume(sendValue S) (Y, bool)` that resumes (or starts) the coroutine, passing `sendValue` to the coroutine (received as the return value of `Yield`), and blocking the caller until the coroutine yields or completes. Returns the yielded value and `true`, or zero value and `false` if the coroutine has finished.
4. Track coroutine state: `Created` (not yet started), `Running` (currently executing), `Suspended` (yielded, waiting to be resumed), `Dead` (function returned or panicked). Attempting to resume a `Dead` coroutine returns `false`.
5. Implement a generator convenience function: `Generator[T](func(yield func(T))) Iterator[T]` that wraps a coroutine into an iterator with `Next() (T, bool)`, enabling `for`-loop-style consumption of yielded values.
6. Implement a `Scheduler` that manages multiple coroutines and runs them in round-robin order on a single goroutine: each coroutine runs until it yields, then the scheduler resumes the next one, cycling through all non-dead coroutines.
7. Handle panics inside coroutines: a panic in a coroutine is caught and stored; the next `Resume` call returns the panic as an error rather than crashing the entire program.
8. Implement coroutine-local storage: each coroutine can store and retrieve key-value pairs via `co.Set(key, value)` and `co.Get(key)`, providing isolated state that persists across yields.

## Hints

- Implement coroutines using a pair of channels: one for the caller to send values to the coroutine (`chan Send`), and one for the coroutine to send values back (`chan Yield`). `Yield` sends on the yield channel and receives on the send channel. `Resume` sends on the send channel and receives on the yield channel.
- Each coroutine runs in its own goroutine (despite the "single goroutine" framing, the goroutine is parked when the coroutine is suspended, so only one is executing at a time).
- Use buffered channels of size 0 (unbuffered) to ensure synchronous handoff between caller and coroutine.
- For the `Generator` wrapper, use `Send = struct{}{}` (unit type) since generators only yield values without receiving input.
- The `Scheduler` can be a loop: `for any coroutine is alive { for each coroutine { resume it; if it yielded, continue to next } }`.
- Panic recovery: use `defer recover()` inside the coroutine goroutine and send the recovered value on a separate error channel.
- Coroutine-local storage: use a `map[any]any` on the `CoroutineContext`.

## Success Criteria

1. A coroutine that yields 1, 2, 3 and then returns produces exactly those three values when resumed three times, and a fourth resume returns `false`.
2. Bidirectional communication works: a coroutine that yields `x` and receives `y = co.Yield(x)` correctly receives the value sent via `Resume(y)`.
3. The generator produces a Fibonacci sequence via `Generator(func(yield func(int)) { a, b := 0, 1; for { yield(a); a, b = b, a+b } })` that can be consumed incrementally.
4. The round-robin scheduler correctly interleaves execution of 4 coroutines, each yielding 5 times, with the output showing round-robin ordering.
5. A panicking coroutine does not crash the program; instead, the panic is returned as an error on the next `Resume`.
6. Coroutine state transitions are correct: `Created -> Running -> Suspended -> Running -> Dead`.
7. No goroutine leaks: after all coroutines are dead, the backing goroutines have exited.

## Research Resources

- Lua coroutines documentation -- https://www.lua.org/pil/9.1.html -- the classic coroutine API
- Python generators (PEP 255) and `send()` protocol (PEP 342)
- Kotlin coroutines -- https://kotlinlang.org/docs/coroutines-overview.html
- "Revisiting Coroutines" (de Moura & Ierusalimschy, 2009)
- Go `iter` package (Go 1.23+) -- range-over-function iterators
- Rob Pike, "Concurrency is not Parallelism" -- understanding cooperative vs preemptive scheduling
