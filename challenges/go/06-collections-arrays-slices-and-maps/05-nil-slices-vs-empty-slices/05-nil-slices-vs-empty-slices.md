# 5. Nil Slices vs Empty Slices

<!--
difficulty: basic
concepts: [nil-slice, empty-slice, json-encoding, reflect-deep-equal, slice-zero-value, api-design]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [slices-creation-append-capacity, maps-creation-access-iteration]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-04 in this section
- Understanding of slice creation and nil
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** between nil slices and empty slices
- **Explain** when the difference matters (JSON encoding, reflect comparison, API contracts)
- **Choose** the appropriate form for function return values

## Why This Matters

A nil slice and an empty slice both have length 0 and capacity 0. They both work with `append`, `len`, `cap`, and `range`. But they are not identical. The difference surfaces in JSON serialization (`null` vs `[]`), reflection-based comparison, and interface nil checks. Understanding this distinction prevents subtle bugs in APIs, serialization, and testing.

## Step 1 -- Creating Nil and Empty Slices

```bash
mkdir -p ~/go-exercises/nil-vs-empty
cd ~/go-exercises/nil-vs-empty
go mod init nil-vs-empty
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Nil slice -- no backing array
	var nilSlice []int
	fmt.Printf("nil slice: %v | nil? %v | len=%d cap=%d\n",
		nilSlice, nilSlice == nil, len(nilSlice), cap(nilSlice))

	// Empty slice -- has a backing array (of size 0)
	emptyLiteral := []int{}
	fmt.Printf("literal:   %v | nil? %v | len=%d cap=%d\n",
		emptyLiteral, emptyLiteral == nil, len(emptyLiteral), cap(emptyLiteral))

	emptyMake := make([]int, 0)
	fmt.Printf("make(0):   %v | nil? %v | len=%d cap=%d\n",
		emptyMake, emptyMake == nil, len(emptyMake), cap(emptyMake))
}
```

Both `fmt.Println` the same way (`[]`) but the nil check differs.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
nil slice: [] | nil? true | len=0 cap=0
literal:   [] | nil? false | len=0 cap=0
make(0):   [] | nil? false | len=0 cap=0
```

## Step 2 -- They Work the Same for Most Operations

```go
package main

import "fmt"

func main() {
	var nilSlice []int
	emptySlice := []int{}

	// append works on both
	nilSlice = append(nilSlice, 1, 2, 3)
	emptySlice = append(emptySlice, 1, 2, 3)
	fmt.Println("nil + append:", nilSlice)
	fmt.Println("empty + append:", emptySlice)

	// range works on both (zero iterations)
	var s []string
	for _, v := range s {
		fmt.Println(v) // never executes
	}
	fmt.Println("range over nil: no panic")

	// len and cap work on both
	fmt.Println("len(nil):", len(s), "cap(nil):", cap(s))
}
```

For everyday code, nil and empty slices are interchangeable. You do not need to initialize a slice before appending.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
nil + append: [1 2 3]
empty + append: [1 2 3]
range over nil: no panic
len(nil): 0 cap(nil): 0
```

## Step 3 -- Where the Difference Matters: JSON Encoding

```go
package main

import (
	"encoding/json"
	"fmt"
)

type Response struct {
	Items []string `json:"items"`
}

func main() {
	// Nil slice encodes as null
	r1 := Response{Items: nil}
	b1, _ := json.Marshal(r1)
	fmt.Println("nil  ->", string(b1))

	// Empty slice encodes as []
	r2 := Response{Items: []string{}}
	b2, _ := json.Marshal(r2)
	fmt.Println("empty ->", string(b2))
}
```

APIs that return JSON often need to distinguish between "no data" (`null`) and "empty list" (`[]`). This matters for API consumers that check for null vs empty arrays.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
nil  -> {"items":null}
empty -> {"items":[]}
```

## Step 4 -- reflect.DeepEqual and Testing

```go
package main

import (
	"fmt"
	"reflect"
)

func main() {
	var nilSlice []int
	emptySlice := []int{}

	fmt.Println("nil == nil:",
		reflect.DeepEqual(nilSlice, nilSlice))     // true
	fmt.Println("empty == empty:",
		reflect.DeepEqual(emptySlice, emptySlice)) // true
	fmt.Println("nil == empty:",
		reflect.DeepEqual(nilSlice, emptySlice))   // false!
}
```

`reflect.DeepEqual` treats nil and empty slices as different. This can cause test failures when comparing expected vs actual results.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
nil == nil: true
empty == empty: true
nil == empty: false
```

## Step 5 -- Guidelines for Choosing

```go
package main

import "fmt"

// Return nil to indicate "no results" or "not applicable"
func findUsers(active bool) []string {
	if !active {
		return nil // no filtering applied
	}
	// Imagine a database query that returns no rows
	return []string{} // explicitly "we looked, found nothing"
}

// Accept nil slices gracefully
func process(items []string) {
	fmt.Printf("Processing %d items (nil? %v)\n", len(items), items == nil)
	for _, item := range items {
		fmt.Println(" ", item)
	}
}

func main() {
	process(findUsers(false))
	process(findUsers(true))
	process([]string{"alice", "bob"})
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Processing 0 items (nil? true)
Processing 0 items (nil? false)
Processing 2 items (nil? false)
```

## Common Mistakes

### Checking len Instead of nil

**Wrong:**

```go
if len(items) == 0 {
    // Could be nil OR empty -- which do you mean?
}
```

**What happens:** If you need to distinguish nil from empty (e.g., for JSON encoding), `len` checks are insufficient.

**Fix:** Check `items == nil` explicitly when the distinction matters. Use `len(items) == 0` when you only care about emptiness.

### Using reflect.DeepEqual in Tests Without Awareness

**Wrong:**

```go
expected := []int{}
actual := functionThatReturnsNil()
assert.True(t, reflect.DeepEqual(expected, actual)) // false!
```

**What happens:** nil and empty are not DeepEqual.

**Fix:** Use `assert.Empty` or compare lengths, or ensure both sides are consistently nil or empty.

## Verify What You Learned

1. Write a function that returns a nil slice and an empty slice, JSON-encode both, and verify the output differs
2. Write a helper function `isNilOrEmpty(s []string) bool` that returns true for both nil and empty slices
3. Explain in a comment when you would return `nil` vs `[]string{}` from a function

## What's Next

Continue to [06 - Copy and Full Slice Expression](../06-copy-and-full-slice-expression/06-copy-and-full-slice-expression.md) to learn how to safely decouple slices from their backing arrays.

## Summary

- `var s []int` is nil; `[]int{}` and `make([]int, 0)` are empty but non-nil
- Both nil and empty slices have `len=0`, `cap=0`
- Both work with `append`, `range`, `len`, `cap` identically
- JSON encoding: nil encodes as `null`, empty encodes as `[]`
- `reflect.DeepEqual` treats nil and empty as different
- Return nil to mean "not applicable"; return empty to mean "no results found"
- Accept nil gracefully in function parameters; check `len == 0` for emptiness

## Reference

- [Go Wiki: Nil slices vs non-nil slices vs empty slices](https://go.dev/wiki/CodeReviewComments#declaring-empty-slices)
- [Go Blog: Go Slices: usage and internals](https://go.dev/blog/slices-intro)
- [encoding/json package](https://pkg.go.dev/encoding/json)
