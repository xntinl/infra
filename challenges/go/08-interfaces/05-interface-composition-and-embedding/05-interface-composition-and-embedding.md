# 5. Interface Composition and Embedding

<!--
difficulty: intermediate
concepts: [interface-embedding, composition, io-ReadWriter, io-ReadCloser, small-interfaces]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [implicit-interface-satisfaction, common-standard-library-interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-04 in this section
- Familiarity with `io.Reader`, `io.Writer`, and `io.Closer`

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** interface embedding to compose larger interfaces from smaller ones
- **Explain** why Go favors many small interfaces over few large ones
- **Identify** standard library interfaces built from composition

## Why Interface Composition and Embedding

Go encourages small, focused interfaces -- often with just one or two methods. When you need a broader capability, you compose interfaces by embedding them inside a new interface. This is how `io.ReadWriter` combines `io.Reader` and `io.Writer`, and how `io.ReadWriteCloser` adds `io.Closer` on top.

Composition avoids the "fat interface" problem found in other languages. A type only needs to implement the exact methods required by the interface it faces, and larger interfaces are assembled incrementally.

## Step 1 -- Embed Two Interfaces into One

Create a new project:

```bash
mkdir -p ~/go-exercises/interface-composition
cd ~/go-exercises/interface-composition
go mod init interface-composition
```

Create `main.go`:

```go
package main

import "fmt"

type Reader interface {
	Read() string
}

type Writer interface {
	Write(data string)
}

// ReadWriter embeds both Reader and Writer
type ReadWriter interface {
	Reader
	Writer
}

type Buffer struct {
	data string
}

func (b *Buffer) Read() string     { return b.data }
func (b *Buffer) Write(data string) { b.data = data }

func process(rw ReadWriter) {
	rw.Write("hello from process")
	fmt.Println("Read back:", rw.Read())
}

func main() {
	buf := &Buffer{}
	process(buf) // Buffer satisfies ReadWriter
}
```

`ReadWriter` requires both `Read` and `Write`. `Buffer` implements both, so it satisfies `ReadWriter` implicitly.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Read back: hello from process
```

## Step 2 -- Standard Library Composition

Explore how the standard library composes interfaces:

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

func readAndClose(rc io.ReadCloser) {
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	fmt.Printf("Read %d bytes: %q\n", len(data), string(data))
}

func readAndWrite(rw io.ReadWriter) {
	rw.Write([]byte("written data"))
	buf := make([]byte, 12)
	n, _ := rw.Read(buf)
	fmt.Printf("Read back %d bytes: %q\n", n, string(buf[:n]))
}

func main() {
	// io.NopCloser turns an io.Reader into an io.ReadCloser
	rc := io.NopCloser(strings.NewReader("closable reader"))
	readAndClose(rc)

	// bytes.Buffer implements both Read and Write
	var buf bytes.Buffer
	readAndWrite(&buf)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Read 15 bytes: "closable reader"
Read back 12 bytes: "written data"
```

## Step 3 -- Build a Three-Level Composition

Create a hierarchy of composed interfaces for a storage system:

```go
package main

import "fmt"

type Getter interface {
	Get(key string) (string, bool)
}

type Setter interface {
	Set(key string, value string)
}

type Deleter interface {
	Delete(key string)
}

// ReadWrite composes Getter and Setter
type ReadWrite interface {
	Getter
	Setter
}

// Store composes all three
type Store interface {
	Getter
	Setter
	Deleter
}

type MemoryStore struct {
	data map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]string)}
}

func (m *MemoryStore) Get(key string) (string, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *MemoryStore) Set(key, value string) {
	m.data[key] = value
}

func (m *MemoryStore) Delete(key string) {
	delete(m.data, key)
}

// Function that only needs read access
func lookup(g Getter, key string) {
	if val, ok := g.Get(key); ok {
		fmt.Printf("Found %s = %s\n", key, val)
	} else {
		fmt.Printf("Key %s not found\n", key)
	}
}

// Function that needs read-write access
func populate(rw ReadWrite) {
	rw.Set("name", "Alice")
	rw.Set("role", "Engineer")
	val, _ := rw.Get("name")
	fmt.Printf("Populated name: %s\n", val)
}

func main() {
	store := NewMemoryStore()

	populate(store) // MemoryStore satisfies ReadWrite
	lookup(store, "name")
	lookup(store, "missing")

	store.Delete("name")
	lookup(store, "name")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Populated name: Alice
Found name = Alice
Key missing not found
Key name not found
```

## Step 4 -- Embedding Adds Method Requirements

When you embed interfaces, the resulting interface requires ALL methods from ALL embedded interfaces:

```go
package main

import "fmt"

type A interface{ MethodA() }
type B interface{ MethodB() }

type AB interface {
	A
	B
}

type OnlyA struct{}
func (OnlyA) MethodA() {}

type BothAB struct{}
func (BothAB) MethodA() {}
func (BothAB) MethodB() {}

func main() {
	var ab AB

	// OnlyA does NOT satisfy AB -- missing MethodB
	// ab = OnlyA{} // COMPILE ERROR

	ab = BothAB{} // BothAB satisfies AB
	_ = ab

	// But OnlyA still satisfies A
	var a A = OnlyA{}
	_ = a

	fmt.Println("Compile-time checks passed")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Compile-time checks passed
```

## Common Mistakes

### Embedding Interfaces with Conflicting Method Signatures

**Wrong:**

```go
type Foo interface { Do() string }
type Bar interface { Do() int }
type FooBar interface {
	Foo
	Bar // COMPILE ERROR: conflicting Do methods
}
```

**Fix:** Ensure embedded interfaces do not have methods with the same name but different signatures.

### Creating Overly Large Composed Interfaces

**Wrong approach:** Composing 10 interfaces into one mega-interface that few types can satisfy.

**Fix:** Keep composed interfaces to 2-4 embedded interfaces. Functions should accept the narrowest interface they need.

## Verify What You Learned

1. Compose `io.Reader` and `io.Writer` manually (without using `io.ReadWriter`) and verify `bytes.Buffer` satisfies it
2. Create a three-level interface hierarchy and a concrete type that satisfies all levels
3. Write a function that accepts only the narrowest interface it needs

## What's Next

Continue to [06 - Interface Segregation](../06-interface-segregation/06-interface-segregation.md) to learn the principle of defining interfaces at the point of consumption.

## Summary

- Interfaces can embed other interfaces to compose broader capabilities
- `io.ReadWriter` = `io.Reader` + `io.Writer` (standard library example)
- A type must implement all methods from all embedded interfaces to satisfy the composed interface
- Favor many small interfaces composed together over one large interface
- Functions should accept the narrowest interface they need
- Embedded interfaces must not have conflicting method signatures

## Reference

- [Go Spec: Interface types](https://go.dev/ref/spec#Interface_types)
- [io.ReadWriter](https://pkg.go.dev/io#ReadWriter)
- [io.ReadWriteCloser](https://pkg.go.dev/io#ReadWriteCloser)
- [Go Proverbs: The bigger the interface, the weaker the abstraction](https://go-proverbs.github.io/)
