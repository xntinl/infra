# 5. Generator Pattern

<!--
difficulty: intermediate
concepts: [generator, channel-returning-function, lazy-evaluation, infinite-sequences]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, closures]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with goroutines and channels
- Understanding of closures and function returns

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the generator pattern and its relationship to lazy evaluation
- **Apply** generators using channel-returning functions
- **Identify** when generators are useful vs when iterators or slices suffice

## Why Generators

A generator is a function that returns a channel and produces values on demand in a background goroutine. Consumers receive values as they become available, without needing to know how they are produced.

Generators are useful for:
- Infinite sequences (Fibonacci, primes, IDs)
- Reading from external sources (files, network, databases)
- Decoupling production from consumption

The pattern encapsulates the goroutine lifecycle inside the function, making the API clean for consumers.

## Step 1 -- Integer Sequence Generator

```bash
mkdir -p ~/go-exercises/generator
cd ~/go-exercises/generator
go mod init generator
```

Create `main.go`:

```go
package main

import "fmt"

func integers(done <-chan struct{}) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		n := 0
		for {
			select {
			case out <- n:
				n++
			case <-done:
				return
			}
		}
	}()
	return out
}

func main() {
	done := make(chan struct{})
	ints := integers(done)

	// Take 10 values
	for i := 0; i < 10; i++ {
		fmt.Println(<-ints)
	}

	close(done) // Stop the generator
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
0
1
2
3
4
5
6
7
8
9
```

## Step 2 -- Fibonacci Generator

```go
package main

import "fmt"

func fibonacci(done <-chan struct{}) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		a, b := 0, 1
		for {
			select {
			case out <- a:
				a, b = b, a+b
			case <-done:
				return
			}
		}
	}()
	return out
}

func take(done <-chan struct{}, in <-chan int, n int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := 0; i < n; i++ {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				out <- v
			case <-done:
				return
			}
		}
	}()
	return out
}

func main() {
	done := make(chan struct{})
	defer close(done)

	fibs := fibonacci(done)
	first15 := take(done, fibs, 15)

	for v := range first15 {
		fmt.Printf("%d ", v)
	}
	fmt.Println()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
0 1 1 2 3 5 8 13 21 34 55 89 144 233 377
```

## Step 3 -- Composable Generators

Build generators that compose like Unix pipes:

```go
package main

import "fmt"

func repeat(done <-chan struct{}, values ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for {
			for _, v := range values {
				select {
				case out <- v:
				case <-done:
					return
				}
			}
		}
	}()
	return out
}

func take(done <-chan struct{}, in <-chan int, n int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := 0; i < n; i++ {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				out <- v
			case <-done:
				return
			}
		}
	}()
	return out
}

func filter(done <-chan struct{}, in <-chan int, fn func(int) bool) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for {
			select {
			case v, ok := <-in:
				if !ok {
					return
				}
				if fn(v) {
					select {
					case out <- v:
					case <-done:
						return
					}
				}
			case <-done:
				return
			}
		}
	}()
	return out
}

func main() {
	done := make(chan struct{})
	defer close(done)

	// Repeat 1,2,3,4,5 -> filter odds -> take 8
	source := repeat(done, 1, 2, 3, 4, 5)
	odds := filter(done, source, func(n int) bool { return n%2 != 0 })
	result := take(done, odds, 8)

	for v := range result {
		fmt.Printf("%d ", v)
	}
	fmt.Println()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
1 3 5 1 3 5 1 3
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Not providing a done channel for infinite generators | Goroutine leaks when the consumer stops early |
| Closing the done channel multiple times | Panics; use `defer close(done)` once in the owner |
| Buffering the output channel unnecessarily | Hides backpressure issues; unbuffered is fine for most generators |

## Verify What You Learned

1. Write a prime number generator using the Sieve of Eratosthenes with channels
2. Compose `repeat`, `filter`, and `take` to produce the first 5 multiples of 3 from a repeating 1-10 sequence

## What's Next

Continue to [06 - errgroup Basic Usage](../06-errgroup-basic-usage/06-errgroup-basic-usage.md) to learn structured error handling in concurrent code.

## Summary

- A generator is a function that returns a receive-only channel and produces values in a goroutine
- Always accept a `done` channel (or `context.Context`) for cancellation
- Generators enable lazy evaluation of infinite sequences
- They compose naturally: generators can consume other generators' channels
- Close the done channel from the consumer side to stop generators

## Reference

- [Go Concurrency Patterns (talk)](https://go.dev/talks/2012/concurrency.slide)
- [Advanced Go Concurrency Patterns (talk)](https://go.dev/talks/2013/advconc.slide)
