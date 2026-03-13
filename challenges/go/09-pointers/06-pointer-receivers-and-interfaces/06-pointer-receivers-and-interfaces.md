# 6. Pointer Receivers and Interface Satisfaction

<!--
difficulty: intermediate
concepts: [pointer-receivers, value-receivers, method-sets, interface-satisfaction, addressability]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [pointer-basics, pointers-to-structs, implicit-interface-satisfaction]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-05 in this section
- Familiarity with interfaces (Section 08, exercises 01-02)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the method set rules that govern interface satisfaction
- **Explain** why a value type does not satisfy an interface requiring pointer-receiver methods
- **Analyze** real code to determine which receiver type to use

## Why This Matters

Go's interface satisfaction depends on the method set of a type:

- The method set of type `T` includes only methods with value receivers
- The method set of type `*T` includes methods with both value and pointer receivers

This asymmetry means `*T` can satisfy interfaces that `T` cannot. Getting this wrong causes compile errors that confuse newcomers.

## Step 1 -- Value Receiver: Both T and *T Satisfy

Create a new project:

```bash
mkdir -p ~/go-exercises/pointer-receivers-interfaces
cd ~/go-exercises/pointer-receivers-interfaces
go mod init pointer-receivers-interfaces
```

Create `main.go`:

```go
package main

import "fmt"

type Greeter interface {
	Greet() string
}

type English struct{ Name string }

// Value receiver -- both English and *English satisfy Greeter
func (e English) Greet() string {
	return "Hello, " + e.Name
}

func sayHello(g Greeter) {
	fmt.Println(g.Greet())
}

func main() {
	val := English{Name: "Alice"}
	ptr := &English{Name: "Bob"}

	sayHello(val) // English satisfies Greeter ✓
	sayHello(ptr) // *English also satisfies Greeter ✓
}
```

When `Greet` has a value receiver, both `English` (value) and `*English` (pointer) satisfy the `Greeter` interface.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello, Alice
Hello, Bob
```

## Step 2 -- Pointer Receiver: Only *T Satisfies

Replace `main.go` with:

```go
package main

import "fmt"

type Counter interface {
	Increment()
	Count() int
}

type HitCounter struct {
	hits int
}

// Pointer receiver -- only *HitCounter satisfies Counter
func (h *HitCounter) Increment() {
	h.hits++
}

// Value receiver -- included in both T and *T method sets
func (h HitCounter) Count() int {
	return h.hits
}

func runCounter(c Counter) {
	c.Increment()
	c.Increment()
	c.Increment()
	fmt.Println("Count:", c.Count())
}

func main() {
	// This works -- *HitCounter satisfies Counter
	ptr := &HitCounter{}
	runCounter(ptr)

	// This does NOT compile -- HitCounter lacks Increment (pointer receiver)
	// val := HitCounter{}
	// runCounter(val) // COMPILE ERROR
}
```

`Increment` has a pointer receiver. The method set of `HitCounter` (value) does NOT include `Increment`, so a bare `HitCounter` cannot satisfy `Counter`. Only `*HitCounter` can.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Count: 3
```

## Step 3 -- The Compile Error Explained

Create `broken.go` to see the error (do not add a package main -- we will use `go vet`):

```go
package main

import "fmt"

type Sizer interface {
	Size() int
}

type File struct {
	name string
	size int
}

func (f *File) Size() int {
	return f.size
}

func printSize(s Sizer) {
	fmt.Println("Size:", s.Size())
}

func main() {
	f := File{name: "data.txt", size: 1024}
	printSize(f) // will not compile
}
```

### Intermediate Verification

```bash
go build main.go
```

Expected error:

```
cannot use f (variable of type File) as Sizer value in argument to printSize:
    File does not implement Sizer (method Size has pointer receiver)
```

The fix is to pass `&f` instead of `f`, or change `Size` to a value receiver if mutation is not needed.

Now replace `main.go` with the corrected version:

```go
package main

import "fmt"

type Sizer interface {
	Size() int
}

type File struct {
	name string
	size int
}

func (f *File) Size() int {
	return f.size
}

func printSize(s Sizer) {
	fmt.Println("Size:", s.Size())
}

func main() {
	f := File{name: "data.txt", size: 1024}
	printSize(&f) // pass pointer -- compiles
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Size: 1024
```

## Step 4 -- Choosing Value vs Pointer Receiver

Replace `main.go` with a practical example showing both:

```go
package main

import (
	"fmt"
	"strings"
)

type Formatter interface {
	Format() string
}

type Resettable interface {
	Reset()
}

type FormattableResettable interface {
	Formatter
	Resettable
}

type Document struct {
	Title string
	Body  string
}

// Value receiver -- does not mutate, safe for both T and *T
func (d Document) Format() string {
	return fmt.Sprintf("# %s\n\n%s", d.Title, d.Body)
}

// Pointer receiver -- mutates the struct
func (d *Document) Reset() {
	d.Title = ""
	d.Body = ""
}

func display(f Formatter) {
	fmt.Println(f.Format())
	fmt.Println(strings.Repeat("-", 30))
}

func resetAndShow(fr FormattableResettable) {
	fr.Reset()
	fmt.Println("After reset:", fr.Format())
}

func main() {
	doc := &Document{Title: "Go Pointers", Body: "Pointers are addresses."}

	display(doc)        // *Document satisfies Formatter ✓
	resetAndShow(doc)   // *Document satisfies FormattableResettable ✓

	// A value Document satisfies Formatter but NOT FormattableResettable
	val := Document{Title: "Test", Body: "content"}
	display(val)         // Document satisfies Formatter ✓
	// resetAndShow(val) // COMPILE ERROR: Document lacks Reset
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
# Go Pointers

Pointers are addresses.
------------------------------
After reset: #

------------------------------
# Test

content
------------------------------
```

## Guidelines for Choosing Receiver Type

| Criterion | Value Receiver | Pointer Receiver |
|-----------|---------------|-----------------|
| Method mutates the receiver | No | Yes |
| Struct is large (> 64 bytes) | Avoid (copy cost) | Prefer |
| Consistency with other methods | If all are value | If any is pointer |
| Must satisfy interface with pointer methods | Cannot | Can |
| Thread safety (immutable reads) | Safer | Needs synchronization |

**Rule of thumb:** If any method needs a pointer receiver, make all methods on that type use pointer receivers for consistency.

## Common Mistakes

### Mixing Receiver Types Without Reason

**Wrong:**

```go
func (s Server) Start() { ... }  // value
func (s *Server) Stop() { ... }  // pointer
func (s Server) Status() { ... } // value
```

**Fix:** If `Stop` needs a pointer receiver, use pointer receivers for all methods on `Server`.

### Storing Value Types in Interface Slices When Pointer Methods Exist

**Wrong:**

```go
items := []Counter{HitCounter{}, HitCounter{}} // compile error
```

**Fix:**

```go
items := []Counter{&HitCounter{}, &HitCounter{}}
```

## Verify What You Learned

1. Define an interface with one method and implement it with a value receiver -- verify both `T` and `*T` satisfy it
2. Add a second method with a pointer receiver -- verify only `*T` satisfies the composed interface
3. Trigger the "does not implement (method has pointer receiver)" error and fix it
4. Write a guideline for your team on when to use value vs pointer receivers

## What's Next

Continue to [07 - Escape Analysis](../07-escape-analysis/07-escape-analysis.md) to learn how the compiler decides whether to allocate on the stack or the heap.

## Summary

- The method set of `T` includes only value-receiver methods
- The method set of `*T` includes both value-receiver and pointer-receiver methods
- A value `T` cannot satisfy an interface that requires a pointer-receiver method
- A pointer `*T` can satisfy any interface the type's methods cover
- If any method on a type needs a pointer receiver, use pointer receivers for all methods
- The compiler error "does not implement (method has pointer receiver)" means you need to pass a pointer
- Value receivers are appropriate for small, immutable types; pointer receivers for mutable or large types

## Reference

- [Go Spec: Method sets](https://go.dev/ref/spec#Method_sets)
- [Go FAQ: Should I define methods on values or pointers?](https://go.dev/doc/faq#methods_on_values_or_pointers)
- [Effective Go: Pointers vs Values](https://go.dev/doc/effective_go#pointers_vs_values)
- [Go Wiki: CodeReviewComments - Receiver Type](https://go.dev/wiki/CodeReviewComments#receiver-type)
