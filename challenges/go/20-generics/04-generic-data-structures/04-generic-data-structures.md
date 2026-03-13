# 4. Generic Data Structures

<!--
difficulty: intermediate
concepts: [generic-structs, stack, queue, type-safety, methods-on-generic-types]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [type-parameters, generic-functions, slices, methods]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [03 - Comparable and Ordered](../03-comparable-and-ordered/03-comparable-and-ordered.md)
- Familiarity with slices and methods

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a type-safe `Stack[T]` using generics
- **Build** a type-safe `Queue[T]` using generics
- **Define** methods on generic types

## Why Generic Data Structures

Before generics, building a reusable stack or queue meant using `interface{}` everywhere and casting on every operation. A bug where you push an `int` and pop expecting a `string` would only crash at runtime.

Generic data structures catch these mistakes at compile time. A `Stack[int]` will never accept a string, and its `Pop` method returns an `int` directly -- no casting required.

## Step 1 -- Build a Stack

```bash
mkdir -p ~/go-exercises/generic-ds
cd ~/go-exercises/generic-ds
go mod init generic-ds
```

Create `main.go` with a generic stack:

```go
package main

import "fmt"

type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(item T) {
	s.items = append(s.items, item)
}

func (s *Stack[T]) Pop() (T, bool) {
	if len(s.items) == 0 {
		var zero T
		return zero, false
	}
	item := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return item, true
}

func (s *Stack[T]) Peek() (T, bool) {
	if len(s.items) == 0 {
		var zero T
		return zero, false
	}
	return s.items[len(s.items)-1], true
}

func (s *Stack[T]) Len() int {
	return len(s.items)
}

func (s *Stack[T]) IsEmpty() bool {
	return len(s.items) == 0
}

func main() {
	// Integer stack
	var intStack Stack[int]
	intStack.Push(10)
	intStack.Push(20)
	intStack.Push(30)

	fmt.Println("Stack length:", intStack.Len())

	if val, ok := intStack.Peek(); ok {
		fmt.Println("Peek:", val)
	}

	for !intStack.IsEmpty() {
		val, _ := intStack.Pop()
		fmt.Println("Popped:", val)
	}

	// String stack
	var strStack Stack[string]
	strStack.Push("hello")
	strStack.Push("world")
	val, _ := strStack.Pop()
	fmt.Println("String pop:", val)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Stack length: 3
Peek: 30
Popped: 30
Popped: 20
Popped: 10
String pop: world
```

## Step 2 -- Build a Queue

Add a generic queue that follows FIFO order:

```go
type Queue[T any] struct {
	items []T
}

func (q *Queue[T]) Enqueue(item T) {
	q.items = append(q.items, item)
}

func (q *Queue[T]) Dequeue() (T, bool) {
	if len(q.items) == 0 {
		var zero T
		return zero, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	return item, true
}

func (q *Queue[T]) Front() (T, bool) {
	if len(q.items) == 0 {
		var zero T
		return zero, false
	}
	return q.items[0], true
}

func (q *Queue[T]) Len() int {
	return len(q.items)
}

func (q *Queue[T]) IsEmpty() bool {
	return len(q.items) == 0
}
```

Add to `main`:

```go
fmt.Println("\n--- Queue ---")
var q Queue[string]
q.Enqueue("first")
q.Enqueue("second")
q.Enqueue("third")

fmt.Println("Queue length:", q.Len())

if front, ok := q.Front(); ok {
	fmt.Println("Front:", front)
}

for !q.IsEmpty() {
	val, _ := q.Dequeue()
	fmt.Println("Dequeued:", val)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Queue ---
Queue length: 3
Front: first
Dequeued: first
Dequeued: second
Dequeued: third
```

## Step 3 -- Use Stack to Reverse a Slice

Demonstrate the stack by writing a generic reverse function:

```go
func Reverse[T any](items []T) []T {
	var s Stack[T]
	for _, item := range items {
		s.Push(item)
	}
	result := make([]T, 0, len(items))
	for !s.IsEmpty() {
		val, _ := s.Pop()
		result = append(result, val)
	}
	return result
}
```

Add to `main`:

```go
fmt.Println("\n--- Reverse ---")
fmt.Println("Reversed ints:", Reverse([]int{1, 2, 3, 4, 5}))
fmt.Println("Reversed strings:", Reverse([]string{"a", "b", "c"}))
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Reverse ---
Reversed ints: [5 4 3 2 1]
Reversed strings: [c b a]
```

## Step 4 -- Stack with a Constraint

Create a stack that only accepts ordered types and adds a `Min` method:

```go
import "cmp"

type OrderedStack[T cmp.Ordered] struct {
	Stack[T] // embed the generic stack
}

func (s *OrderedStack[T]) Min() (T, bool) {
	if s.IsEmpty() {
		var zero T
		return zero, false
	}
	min := s.items[0]
	for _, v := range s.items[1:] {
		if v < min {
			min = v
		}
	}
	return min, true
}
```

Add to `main`:

```go
fmt.Println("\n--- OrderedStack ---")
var os OrderedStack[int]
os.Push(30)
os.Push(10)
os.Push(20)
if min, ok := os.Min(); ok {
	fmt.Println("Min:", min)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- OrderedStack ---
Min: 10
```

## Common Mistakes

### Value Receivers on Generic Structs That Mutate State

**Wrong:**

```go
func (s Stack[T]) Push(item T) { // value receiver -- modifies a copy
```

**What happens:** The original stack is never modified.

**Fix:** Use pointer receivers: `func (s *Stack[T]) Push(item T)`.

### Forgetting to Handle Empty State

**Wrong:**

```go
func (s *Stack[T]) Pop() T {
	item := s.items[len(s.items)-1] // panics on empty stack
```

**Fix:** Return `(T, bool)` to signal empty state, or check length first.

## Verify What You Learned

```bash
go run main.go
```

Confirm all data structures work correctly with different types.

## What's Next

Continue to [05 - Interface Constraints with Methods](../05-interface-constraints-with-methods/05-interface-constraints-with-methods.md) to learn how to constrain type parameters to types that implement specific methods.

## Summary

- Generic structs declare type parameters: `type Stack[T any] struct { ... }`
- Methods on generic types repeat the type parameter: `func (s *Stack[T]) Push(item T)`
- Return `(T, bool)` for operations that may fail on empty collections
- Use `var zero T` to get the zero value for any type parameter
- Generic types can embed other generic types

## Reference

- [Go spec: Type declarations](https://go.dev/ref/spec#Type_declarations)
- [Go spec: Method declarations](https://go.dev/ref/spec#Method_declarations)
