# 8. Pointers in Slices and Maps

<!--
difficulty: intermediate
concepts: [pointer-slices, pointer-maps, shared-references, collection-mutation, value-vs-pointer-elements]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [pointer-basics, pointers-to-structs, collections-slices-maps]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-05 in this section
- Familiarity with slices and maps (Section 06)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** pointer-based elements in slices and maps to enable shared mutation
- **Explain** the difference between `[]T` and `[]*T` for iteration and modification
- **Analyze** when to store pointers vs values in collections

## Why Pointers in Collections

When you store structs directly in a slice (`[]T`), each element is a value. Iterating with `range` gives you a copy of each element -- modifying the copy does not affect the slice. Storing pointers (`[]*T`) means each element is an address, so modifications through the pointer affect the original struct.

Maps have a different subtlety: you cannot take the address of a map value (`&m[key]` does not compile). This means you must either store pointers in the map or copy-modify-write.

## Step 1 -- Value Slices vs Pointer Slices

Create a new project:

```bash
mkdir -p ~/go-exercises/pointers-collections
cd ~/go-exercises/pointers-collections
go mod init pointers-collections
```

Create `main.go`:

```go
package main

import "fmt"

type Item struct {
	Name  string
	Price float64
}

func main() {
	// Value slice -- range copies each element
	items := []Item{
		{Name: "Book", Price: 15.0},
		{Name: "Pen", Price: 2.0},
	}

	for _, item := range items {
		item.Price *= 1.10 // modifies the COPY, not the slice
	}
	fmt.Println("Value slice after range:", items) // prices unchanged

	// Fix 1: use index
	for i := range items {
		items[i].Price *= 1.10
	}
	fmt.Println("Value slice after index:", items) // prices changed

	// Fix 2: pointer slice
	pItems := []*Item{
		{Name: "Book", Price: 15.0},
		{Name: "Pen", Price: 2.0},
	}

	for _, item := range pItems {
		item.Price *= 1.10 // modifies the original through the pointer
	}
	fmt.Println("Pointer slice after range:")
	for _, item := range pItems {
		fmt.Printf("  %s: $%.2f\n", item.Name, item.Price)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Value slice after range: [{Book 15} {Pen 2}]
Value slice after index: [{Book 16.5} {Pen 2.2}]
Pointer slice after range:
  Book: $16.50
  Pen: $2.20
```

## Step 2 -- Maps with Struct Values: The Copy-Modify-Write Pattern

Replace `main.go` with:

```go
package main

import "fmt"

type User struct {
	Name  string
	Score int
}

func main() {
	users := map[string]User{
		"alice": {Name: "Alice", Score: 100},
		"bob":   {Name: "Bob", Score: 200},
	}

	// This does NOT compile:
	// users["alice"].Score += 10 // cannot assign to struct field in map

	// Pattern: copy, modify, write back
	u := users["alice"]
	u.Score += 10
	users["alice"] = u
	fmt.Println("Alice score:", users["alice"].Score)
}
```

You cannot take the address of a map value or assign to its fields directly. The map may relocate values during growth, which would invalidate any pointer. Go prevents this at compile time.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Alice score: 110
```

## Step 3 -- Maps with Pointer Values

Replace `main.go` with:

```go
package main

import "fmt"

type User struct {
	Name  string
	Score int
}

func main() {
	users := map[string]*User{
		"alice": {Name: "Alice", Score: 100},
		"bob":   {Name: "Bob", Score: 200},
	}

	// Direct modification through the pointer -- this works
	users["alice"].Score += 10
	fmt.Println("Alice score:", users["alice"].Score)

	// Multiple references to the same struct
	leader := users["alice"]
	leader.Score += 5
	fmt.Println("Alice via map:", users["alice"].Score) // also 115
	fmt.Println("Alice via var:", leader.Score)          // 115 -- same struct
}
```

When the map stores `*User`, you get a pointer back. Modifying through that pointer changes the struct that the map also references.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Alice score: 110
Alice via map: 115
Alice via var: 115
```

## Step 4 -- Shared References Across Collections

Replace `main.go` with:

```go
package main

import "fmt"

type Task struct {
	ID       int
	Title    string
	Assignee string
	Done     bool
}

func main() {
	// All tasks
	tasks := []*Task{
		{ID: 1, Title: "Design API", Assignee: "alice"},
		{ID: 2, Title: "Write tests", Assignee: "bob"},
		{ID: 3, Title: "Deploy", Assignee: "alice"},
	}

	// Index by assignee -- same pointers, not copies
	byAssignee := make(map[string][]*Task)
	for _, t := range tasks {
		byAssignee[t.Assignee] = append(byAssignee[t.Assignee], t)
	}

	// Mark task 1 as done through the main slice
	tasks[0].Done = true

	// Verify it is also done in the index
	fmt.Println("Alice's tasks:")
	for _, t := range byAssignee["alice"] {
		fmt.Printf("  [%v] %s\n", t.Done, t.Title)
	}
}
```

Because both the slice and the map hold pointers to the same `Task` structs, a modification through one is visible through the other.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Alice's tasks:
  [true] Design API
  [false] Deploy
```

## Common Mistakes

### Appending Pointer to Loop Variable

**Wrong:**

```go
items := []string{"a", "b", "c"}
ptrs := make([]*string, 0, len(items))
for _, item := range items {
    ptrs = append(ptrs, &item) // all point to the same loop variable!
}
```

In Go versions before 1.22, all pointers point to the same variable (the last value). Go 1.22+ changed loop variable semantics so each iteration gets a new variable, but it is still clearer to use an index:

```go
for i := range items {
    ptrs = append(ptrs, &items[i])
}
```

### Assuming Map Values Are Addressable

**Wrong:**

```go
m := map[string]User{"a": {Name: "Alice"}}
p := &m["a"] // COMPILE ERROR: cannot take address of map element
```

**Fix:** Store pointers in the map: `map[string]*User`.

### Unintended Shared Mutation

When multiple maps or slices hold pointers to the same struct, a modification anywhere is visible everywhere. This is sometimes a feature, sometimes a bug. Document whether your data structures share ownership.

## Verify What You Learned

1. Create a `[]T` slice, attempt to modify elements via `range`, and observe the copy behavior
2. Create a `[]*T` slice and confirm modifications via `range` persist
3. Demonstrate the copy-modify-write pattern for `map[string]T`
4. Build a `map[string]*T` and show direct field modification through the pointer
5. Create two collections referencing the same struct and verify shared mutation

## What's Next

Continue to [09 - Pointer Aliasing and Data Races](../09-pointer-aliasing-and-data-races/09-pointer-aliasing-and-data-races.md) to learn the dangers of multiple goroutines accessing the same pointer concurrently.

## Summary

- `[]T` stores values -- `range` gives copies, use index for in-place modification
- `[]*T` stores pointers -- `range` gives pointers, modifications affect the original
- Map values are not addressable: `&m[key]` does not compile
- `map[K]T` requires copy-modify-write; `map[K]*T` allows direct field modification
- Pointer-based collections enable shared references but require care about unintended mutation
- In Go 1.22+, loop variables are per-iteration, but indexing is still the clearest pattern

## Reference

- [Go Spec: For statements with range clause](https://go.dev/ref/spec#For_range)
- [Go Spec: Index expressions](https://go.dev/ref/spec#Index_expressions)
- [Go Blog: Go 1.22 loop variable change](https://go.dev/blog/loopvar-preview)
- [Go Wiki: Range Clauses](https://go.dev/wiki/Range)
