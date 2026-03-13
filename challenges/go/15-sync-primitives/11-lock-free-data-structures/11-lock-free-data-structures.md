# 11. Lock-Free Data Structures

<!--
difficulty: insane
concepts: [lock-free, cas-loop, atomic-pointer, wait-free, aba-problem]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [atomic-package, compare-and-swap, goroutines]
-->

## The Challenge

Build a lock-free stack using atomic compare-and-swap (CAS) operations. The stack must support concurrent `Push` and `Pop` operations without any mutexes, relying entirely on `atomic.Pointer` and CAS loops.

Lock-free data structures guarantee that at least one thread makes progress in a finite number of steps, even if other threads are delayed or preempted. This is stronger than mutex-based synchronization, where a thread holding a lock can block all others indefinitely.

## Requirements

1. Implement a generic `LockFreeStack[T]` using `atomic.Pointer` (Go 1.19+)
2. `Push(value T)` adds an element to the top of the stack using a CAS loop
3. `Pop() (T, bool)` removes and returns the top element using a CAS loop
4. Must be safe for concurrent use by multiple goroutines
5. Must pass the race detector with zero warnings
6. Benchmark against a mutex-based stack to compare performance under contention

## Hints

<details>
<summary>Hint 1: Node Structure</summary>

```go
type node[T any] struct {
    value T
    next  *node[T]
}

type LockFreeStack[T any] struct {
    head atomic.Pointer[node[T]]
}
```
</details>

<details>
<summary>Hint 2: CAS Loop for Push</summary>

```go
func (s *LockFreeStack[T]) Push(value T) {
    newNode := &node[T]{value: value}
    for {
        oldHead := s.head.Load()
        newNode.next = oldHead
        if s.head.CompareAndSwap(oldHead, newNode) {
            return
        }
        // CAS failed -- another goroutine modified head; retry
    }
}
```
</details>

<details>
<summary>Hint 3: CAS Loop for Pop</summary>

```go
func (s *LockFreeStack[T]) Pop() (T, bool) {
    for {
        oldHead := s.head.Load()
        if oldHead == nil {
            var zero T
            return zero, false
        }
        newHead := oldHead.next
        if s.head.CompareAndSwap(oldHead, newHead) {
            return oldHead.value, true
        }
    }
}
```
</details>

## Success Criteria

1. `go run -race` produces no race warnings
2. All pushed values are accounted for after concurrent push/pop operations
3. The lock-free stack performs comparably or better than a mutex-based stack under high contention
4. Bonus: implement a `Size()` method using `atomic.Int64` to track the count

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type node[T any] struct {
	value T
	next  *node[T]
}

type LockFreeStack[T any] struct {
	head  atomic.Pointer[node[T]]
	count atomic.Int64
}

func (s *LockFreeStack[T]) Push(value T) {
	newNode := &node[T]{value: value}
	for {
		oldHead := s.head.Load()
		newNode.next = oldHead
		if s.head.CompareAndSwap(oldHead, newNode) {
			s.count.Add(1)
			return
		}
	}
}

func (s *LockFreeStack[T]) Pop() (T, bool) {
	for {
		oldHead := s.head.Load()
		if oldHead == nil {
			var zero T
			return zero, false
		}
		if s.head.CompareAndSwap(oldHead, oldHead.next) {
			s.count.Add(-1)
			return oldHead.value, true
		}
	}
}

func (s *LockFreeStack[T]) Size() int64 {
	return s.count.Load()
}

func main() {
	stack := &LockFreeStack[int]{}
	var wg sync.WaitGroup

	// Push 10000 values concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				stack.Push(start*100 + j)
			}
		}(i)
	}
	wg.Wait()
	fmt.Printf("After push: size=%d\n", stack.Size())

	// Pop all values concurrently
	var popped atomic.Int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, ok := stack.Pop()
				if !ok {
					return
				}
				popped.Add(1)
			}
		}()
	}
	wg.Wait()
	fmt.Printf("Popped: %d, remaining: %d\n", popped.Load(), stack.Size())
}
```

## Research Resources

- [Lock-Free Programming (Wikipedia)](https://en.wikipedia.org/wiki/Non-blocking_algorithm)
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
- [ABA Problem](https://en.wikipedia.org/wiki/ABA_problem)
- [The Art of Multiprocessor Programming (book)](https://www.elsevier.com/books/the-art-of-multiprocessor-programming/herlihy/978-0-12-415950-1)

## What's Next

Continue to [12 - sync.OnceValue and OnceFunc](../12-sync-oncevalue-oncefunc/12-sync-oncevalue-oncefunc.md) to learn about Go 1.21+ additions to the sync package.

## Summary

- Lock-free data structures use CAS loops instead of mutexes for thread safety
- A CAS loop reads the current state, computes the new state, and atomically swaps only if no other goroutine modified the state in between
- `atomic.Pointer[T]` (Go 1.19+) provides type-safe atomic pointer operations
- Lock-free structures guarantee progress even if some goroutines are delayed
- The ABA problem is a subtle issue in CAS-based algorithms; Go's garbage collector mitigates it by preventing pointer reuse while references exist
