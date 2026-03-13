# 6. Function Types and Callbacks

<!--
difficulty: intermediate
concepts: [function-types, type-declarations, callbacks, strategy-pattern, middleware]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [function-declaration, first-class-functions, anonymous-functions]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Apply** named function types to simplify complex function signatures
- **Apply** the callback pattern to decouple logic from control flow
- **Analyze** when to use function types versus interfaces

## Why Function Types and Callbacks

When function signatures appear repeatedly in your code, you can declare a named type for them. This makes the code self-documenting and reduces duplication. Instead of writing `func(string) error` in ten places, you write `type Handler func(string) error` once.

Callbacks are functions passed as arguments to other functions, allowing the caller to inject behavior. This pattern decouples "what to do" from "when to do it." Go's standard library uses callbacks extensively — `http.HandlerFunc`, `sort.Slice`, and `filepath.WalkFunc` are all examples of named function types used as callbacks.

## Step 1 — Declaring a Function Type

A named function type gives a meaningful name to a function signature:

```go
package main

import "fmt"

type Transformer func(string) string

func applyAll(s string, transforms ...Transformer) string {
    for _, t := range transforms {
        s = t(s)
    }
    return s
}

func main() {
    result := applyAll("hello",
        func(s string) string { return s + " world" },
        func(s string) string { return s + "!" },
    )
    fmt.Println(result)
}
```

`Transformer` is now a type. You can use it in function signatures, struct fields, slices, and maps.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
hello world!
```

## Step 2 — Callbacks for Event Handling

Callbacks let you define what happens when an event occurs without hardcoding the behavior:

```go
package main

import "fmt"

type EventHandler func(event string, data map[string]string)

type EventEmitter struct {
    handlers map[string][]EventHandler
}

func NewEventEmitter() *EventEmitter {
    return &EventEmitter{handlers: make(map[string][]EventHandler)}
}

func (e *EventEmitter) On(event string, handler EventHandler) {
    e.handlers[event] = append(e.handlers[event], handler)
}

func (e *EventEmitter) Emit(event string, data map[string]string) {
    for _, handler := range e.handlers[event] {
        handler(event, data)
    }
}

func main() {
    emitter := NewEventEmitter()

    emitter.On("user.login", func(event string, data map[string]string) {
        fmt.Printf("[AUDIT] %s: user=%s\n", event, data["username"])
    })

    emitter.On("user.login", func(event string, data map[string]string) {
        fmt.Printf("[METRICS] login count incremented for %s\n", data["username"])
    })

    emitter.Emit("user.login", map[string]string{"username": "alice"})
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
[AUDIT] user.login: user=alice
[METRICS] login count incremented for alice
```

## Step 3 — The Middleware Pattern

Named function types enable the middleware (decorator) pattern, where you wrap a function with additional behavior:

```go
package main

import (
    "fmt"
    "time"
)

type Operation func(input string) (string, error)

func withLogging(op Operation) Operation {
    return func(input string) (string, error) {
        fmt.Printf("Starting operation with input: %q\n", input)
        result, err := op(input)
        if err != nil {
            fmt.Printf("Operation failed: %v\n", err)
        } else {
            fmt.Printf("Operation succeeded: %q\n", result)
        }
        return result, err
    }
}

func withTiming(op Operation) Operation {
    return func(input string) (string, error) {
        start := time.Now()
        result, err := op(input)
        fmt.Printf("Operation took %v\n", time.Since(start))
        return result, err
    }
}

func processData(input string) (string, error) {
    return "processed:" + input, nil
}

func main() {
    op := withTiming(withLogging(processData))

    result, err := op("hello")
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Println("Final result:", result)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output (timing will vary):

```
Starting operation with input: "hello"
Operation succeeded: "processed:hello"
Operation took 375ns
Final result: processed:hello
```

## Step 4 — Methods on Function Types

Named function types can have methods, which is how `http.HandlerFunc` works:

```go
package main

import "fmt"

type Validator func(value string) error

func (v Validator) Validate(value string) error {
    return v(value)
}

func (v Validator) And(other Validator) Validator {
    return func(value string) error {
        if err := v(value); err != nil {
            return err
        }
        return other(value)
    }
}

func notEmpty(value string) error {
    if value == "" {
        return fmt.Errorf("value must not be empty")
    }
    return nil
}

func minLength(n int) Validator {
    return func(value string) error {
        if len(value) < n {
            return fmt.Errorf("value must be at least %d characters", n)
        }
        return nil
    }
}

func main() {
    validate := Validator(notEmpty).And(minLength(5))

    fmt.Println(validate("hello"))
    fmt.Println(validate("hi"))
    fmt.Println(validate(""))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
<nil>
value must be at least 5 characters
value must not be empty
```

## Step 5 — Function Types vs Interfaces

When should you use a function type versus an interface?

| Use a function type when... | Use an interface when... |
|---|---|
| There is a single behavior to inject | There are multiple related methods |
| The behavior is stateless or has simple state | The implementation needs complex state |
| You want callers to pass inline functions | You want testable, mockable dependencies |

```go
// Function type: one behavior
type Notifier func(message string) error

// Interface: multiple related behaviors
type NotificationService interface {
    Send(message string) error
    SendBatch(messages []string) error
    Status() string
}
```

Both approaches are valid. Function types are lighter weight; interfaces provide more structure.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Overusing function types for complex contracts | Interfaces are clearer when multiple methods are involved |
| Forgetting that function types are comparable only to `nil` | You cannot check equality between two function values |
| Not naming the function type | Repeating `func(string) (string, error)` everywhere is error-prone and hard to read |
| Deeply nesting middleware wrappers | Hard to debug; keep middleware chains shallow |

## Verify What You Learned

1. Declare a `type Predicate func(int) bool` and write `filter(nums []int, pred Predicate) []int`.
2. Write two predicates: `isEven` and `isPositive`. Use them with your `filter` function.
3. Write a `not(p Predicate) Predicate` function that inverts a predicate.
4. Write a `compose(preds ...Predicate) Predicate` that returns a predicate combining all inputs with AND logic.

## What's Next

Next you will explore **recursive functions and stack depth**, learning how recursion works in Go and what happens when you recurse too deeply.

## Summary

- `type MyFunc func(...) ...` creates a named function type
- Callbacks decouple "what to do" from "when to do it"
- Named function types can have methods, enabling patterns like `http.HandlerFunc`
- The middleware pattern wraps functions with additional behavior
- Use function types for single behaviors; use interfaces for multi-method contracts

## Reference

- [Go spec: Type declarations](https://go.dev/ref/spec#Type_declarations)
- [Go standard library: http.HandlerFunc](https://pkg.go.dev/net/http#HandlerFunc)
- [Go blog: First Class Functions in Go](https://go.dev/blog/functions-codewalk)
