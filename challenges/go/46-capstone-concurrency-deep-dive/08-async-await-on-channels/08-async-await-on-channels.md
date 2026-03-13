<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Async/Await on Channels

## The Challenge

Build a complete async/await programming model on top of Go channels and goroutines, providing a `Future[T]` type that represents a value that will be available in the future and supports composition via `Then`, `Map`, `FlatMap`, `All`, `Race`, `Any`, and error handling with `Recover`. This exercise explores how async/await models (as seen in JavaScript, Rust, C#, and Python) can be expressed using Go's concurrency primitives, and forces you to reason about resource management (goroutine leaks), cancellation propagation, and composable error handling without language-level async support. The resulting library must be practically useful, not just a toy, supporting timeout, cancellation via context, and bounded concurrency for parallel execution of futures.

## Requirements

1. Implement `Future[T]` as a type that wraps a `chan Result[T]` (where `Result[T]` contains either a value or an error) and provides `Await() (T, error)` that blocks until the future resolves, returning the value or error.
2. Implement `Async[T](func(ctx context.Context) (T, error)) Future[T]` that launches a goroutine to execute the function and returns a `Future[T]` that resolves when the function completes.
3. Implement `Then[T, U](Future[T], func(T) (U, error)) Future[U]` that chains a transformation: when the input future resolves successfully, the function is applied to its value, producing a new future. If the input future resolves with an error, the error is propagated without calling the function.
4. Implement `Map[T, U](Future[T], func(T) U) Future[U]` (infallible transformation), `FlatMap[T, U](Future[T], func(T) Future[U]) Future[U]` (monadic bind that flattens nested futures), and `Recover[T](Future[T], func(error) (T, error)) Future[T]` (error recovery).
5. Implement `All[T](futures ...Future[T]) Future[[]T]` that resolves when all input futures resolve, returning a slice of results in order, or returning the first error encountered (cancelling remaining futures via context).
6. Implement `Race[T](futures ...Future[T]) Future[T]` that resolves with the first future to complete (success or error), and `Any[T](futures ...Future[T]) Future[T]` that resolves with the first successful future, ignoring errors unless all fail.
7. Support cancellation: all futures accept a `context.Context`, and cancelling the context causes in-progress futures to receive cancellation and resolve with `context.Canceled`.
8. Implement `WithTimeout[T](Future[T], time.Duration) Future[T]` and `Pool(maxConcurrency int)` that limits the number of concurrently executing async functions, queuing excess submissions.

## Hints

- `Future[T]` should use a buffered channel of size 1 so the producer never blocks; use `sync.Once` to ensure the result is written exactly once.
- For `Then` and `Map`, spawn a new goroutine that awaits the input future, applies the function, and writes to the output future's channel.
- `FlatMap` is trickier: the function returns a `Future[U]`, and you need to await that inner future before resolving the outer one.
- `All` can use `errgroup.Group` internally, collecting results into a pre-allocated slice.
- `Race` uses a `select` on multiple futures' channels; however, since you cannot dynamically select on a variable number of channels, use a merge goroutine pattern.
- For `Pool`, use a semaphore (buffered channel of struct{} with capacity `maxConcurrency`) to gate goroutine launches.
- Watch for goroutine leaks: if a future is created but never awaited, the goroutine should still be able to complete and release resources.
- Make `Await` idempotent: calling it multiple times returns the same result without blocking after the first call (cache the result).

## Success Criteria

1. `Async` launches a goroutine and `Await` returns the correct result.
2. `Then`, `Map`, and `FlatMap` correctly chain computations, with errors propagated through the chain.
3. `All` with 100 futures completes in the time of the slowest future (parallel execution), not the sum (sequential).
4. `Race` returns the result of the fastest future; remaining futures are cancelled via context.
5. Context cancellation propagates to in-flight futures and causes them to resolve with `context.Canceled`.
6. `WithTimeout` resolves with `context.DeadlineExceeded` if the future does not complete in time.
7. `Pool(4)` with 100 submitted tasks runs at most 4 concurrently (verified by an atomic counter tracking active tasks).
8. No goroutine leaks: after all futures are resolved, `runtime.NumGoroutine()` returns to the baseline count.

## Research Resources

- JavaScript Promise/A+ specification -- https://promisesaplus.com/
- Rust futures and async/await -- https://rust-lang.github.io/async-book/
- "Your Mouse is a Database" (Meijer, 2012) -- duality of iterators and observers
- Go `errgroup` package -- https://pkg.go.dev/golang.org/x/sync/errgroup
- Go `context` package documentation -- https://pkg.go.dev/context
- Go `sync` package for `Once`, `WaitGroup` patterns
