# 8. Embedding for Composition

<!--
difficulty: intermediate
concepts: [composition-over-inheritance, method-promotion, embedding-interfaces, delegation-pattern]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [anonymous-structs-and-embedding, methods-value-vs-pointer-receivers, implicit-interface-satisfaction]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 04 (Anonymous Structs and Embedding)
- Understanding of methods and interfaces

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** embedding to compose behavior from multiple types
- **Analyze** how method promotion satisfies interfaces automatically
- **Implement** the delegation pattern using embedding

## Why Embedding for Composition

Go deliberately omits inheritance. Instead, it provides embedding as a composition mechanism. When you embed a type, its methods are promoted to the outer type, which means the outer type automatically satisfies interfaces that the embedded type satisfies. This is powerful -- you can build complex types by composing simpler ones without tight coupling.

This pattern appears throughout the standard library. `bufio.ReadWriter` embeds `*bufio.Reader` and `*bufio.Writer` to satisfy `io.ReadWriter`. Understanding embedding for composition is essential for designing clean, modular Go code.

## Step 1 -- Interface Satisfaction Through Embedding

```bash
mkdir -p ~/go-exercises/composition
cd ~/go-exercises/composition
go mod init composition
```

Create `main.go`:

```go
package main

import "fmt"

type Logger interface {
	Log(msg string)
}

type ConsoleLogger struct {
	Prefix string
}

func (l ConsoleLogger) Log(msg string) {
	fmt.Printf("[%s] %s\n", l.Prefix, msg)
}

type Service struct {
	ConsoleLogger // embedding
	Name string
}

func process(l Logger, action string) {
	l.Log("Processing: " + action)
}

func main() {
	svc := Service{
		ConsoleLogger: ConsoleLogger{Prefix: "SVC"},
		Name:          "OrderService",
	}

	// Service satisfies Logger through embedding
	svc.Log("started")
	process(svc, "create-order")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[SVC] started
[SVC] Processing: create-order
```

`Service` never explicitly declares `Log` -- it is promoted from `ConsoleLogger`.

## Step 2 -- Composing Multiple Behaviors

```go
package main

import "fmt"

type Reader interface {
	Read() string
}

type Writer interface {
	Write(data string)
}

type ReadWriter interface {
	Reader
	Writer
}

type FileReader struct {
	Path string
}

func (r FileReader) Read() string {
	return fmt.Sprintf("data from %s", r.Path)
}

type FileWriter struct {
	Path string
}

func (w *FileWriter) Write(data string) {
	fmt.Printf("Writing to %s: %s\n", w.Path, data)
}

type FileReadWriter struct {
	FileReader
	*FileWriter
}

func transfer(src Reader, dst Writer) {
	data := src.Read()
	dst.Write(data)
}

func main() {
	rw := FileReadWriter{
		FileReader:  FileReader{Path: "/tmp/input.txt"},
		FileWriter:  &FileWriter{Path: "/tmp/output.txt"},
	}

	// Satisfies ReadWriter through composition
	fmt.Println(rw.Read())
	rw.Write("hello")

	fmt.Println("---")
	transfer(&rw, rw) // FileReadWriter satisfies both Reader and Writer
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
data from /tmp/input.txt
Writing to /tmp/output.txt: hello
---
Writing to /tmp/output.txt: data from /tmp/input.txt
```

## Step 3 -- Overriding Promoted Methods

The outer type can define its own method with the same name, shadowing the promoted method:

```go
type Base struct{}

func (b Base) Greet() string {
	return "Hello from Base"
}

type Extended struct {
	Base
}

func (e Extended) Greet() string {
	return "Hello from Extended (base says: " + e.Base.Greet() + ")"
}

func main() {
	e := Extended{}
	fmt.Println(e.Greet())
	fmt.Println(e.Base.Greet())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello from Extended (base says: Hello from Base)
Hello from Base
```

The outer method shadows the promoted method. You can still call the embedded method explicitly.

## Step 4 -- Embedding for Middleware Pattern

A practical example composing behavior layers:

```go
package main

import (
	"fmt"
	"time"
)

type Handler interface {
	Handle(request string) string
}

type BaseHandler struct{}

func (h BaseHandler) Handle(request string) string {
	return "response for: " + request
}

type LoggingHandler struct {
	Handler // embed the interface
	Label   string
}

func (h LoggingHandler) Handle(request string) string {
	fmt.Printf("[%s] handling: %s\n", h.Label, request)
	result := h.Handler.Handle(request)
	fmt.Printf("[%s] done\n", h.Label)
	return result
}

type TimingHandler struct {
	Handler
}

func (h TimingHandler) Handle(request string) string {
	start := time.Now()
	result := h.Handler.Handle(request)
	fmt.Printf("  took: %v\n", time.Since(start))
	return result
}

func main() {
	base := BaseHandler{}
	logged := LoggingHandler{Handler: base, Label: "LOG"}
	timed := TimingHandler{Handler: logged}

	result := timed.Handle("GET /users")
	fmt.Printf("Result: %s\n", result)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (timing will vary):

```
[LOG] handling: GET /users
[LOG] done
  took: ...
Result: response for: GET /users
```

## Common Mistakes

### Forgetting That Embedding Is Not Inheritance

The embedded type does not know about the outer type. There is no `super` or `self` that refers to the outer struct. If a promoted method calls another method on the receiver, it calls the embedded type's version, not the outer type's.

### Nil Pointer Embedded Fields

If you embed a pointer type and forget to initialize it, calling promoted methods panics:

```go
type Wrapper struct {
	*FileWriter // nil if not initialized
}
w := Wrapper{}
w.Write("crash") // nil pointer dereference
```

Always initialize embedded pointer fields.

## Verify What You Learned

1. Create a `Notifier` interface with a `Notify(msg string)` method
2. Create `EmailNotifier` that implements it
3. Create `AuditedNotifier` that embeds a `Notifier` and logs before/after delegating
4. Verify the chain works by passing `AuditedNotifier` where `Notifier` is expected

## What's Next

Continue to [09 - Struct Memory Layout and Padding](../09-struct-memory-layout-and-padding/09-struct-memory-layout-and-padding.md) to understand how Go lays out struct fields in memory.

## Summary

- Embedding promotes methods, automatically satisfying interfaces
- Compose multiple behaviors by embedding multiple types
- The outer type can shadow promoted methods and still call the embedded version explicitly
- Embedding an interface type creates a delegation slot (middleware pattern)
- Embedding is composition, not inheritance -- the embedded type cannot access the outer type
- Always initialize embedded pointer types to avoid nil dereferences

## Reference

- [Effective Go: Embedding](https://go.dev/doc/effective_go#embedding)
- [Go Blog: Composition not inheritance](https://go.dev/talks/2012/splash.article)
- [A Tour of Go: Embedding](https://go.dev/tour/methods/12)
