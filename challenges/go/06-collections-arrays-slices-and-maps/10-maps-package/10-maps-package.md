# 10. Maps Package

<!--
difficulty: intermediate
concepts: [maps-package, clone, copy, equal, delete-func, keys, values, collect, generic-map-functions]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [maps-creation-access-iteration, map-internals-and-iteration-order, slices-package]
note: Go 1.21+
-->

## Prerequisites

- Go 1.21+ installed (the `maps` package was added in Go 1.21)
- Completed exercises 01-09 in this section
- Understanding of map creation, access, deletion, and iteration
- Basic familiarity with generics syntax

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** standard library `maps` functions for cloning, copying, and comparing maps
- **Use** `maps.Keys` and `maps.Values` to extract map data into slices
- **Combine** the `maps` and `slices` packages for sorted map iteration

## Why the Maps Package

Before Go 1.21, operations like cloning a map, comparing two maps for equality, or extracting sorted keys required manual iteration every time. The `maps` package provides generic, type-safe functions that eliminate this boilerplate. Combined with the `slices` package, it gives you a complete toolkit for working with maps efficiently and correctly.

## Step 1 -- Keys and Values

```bash
mkdir -p ~/go-exercises/maps-pkg
cd ~/go-exercises/maps-pkg
go mod init maps-pkg
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"maps"
	"slices"
)

func main() {
	population := map[string]int{
		"Tokyo":     13960000,
		"Delhi":     11030000,
		"Shanghai":  24870000,
		"Sao Paulo": 12330000,
		"Mumbai":    12440000,
	}

	// Extract keys (order is not guaranteed)
	keys := slices.Collect(maps.Keys(population))
	fmt.Println("Keys (unsorted):", keys)

	// Sort keys for deterministic output
	slices.Sort(keys)
	fmt.Println("Keys (sorted):", keys)

	// Extract values
	values := slices.Collect(maps.Values(population))
	slices.Sort(values)
	fmt.Println("Values (sorted):", values)

	// Sorted iteration pattern
	fmt.Println("\nSorted by city:")
	for _, city := range keys {
		fmt.Printf("  %-12s %d\n", city, population[city])
	}
}
```

`maps.Keys` and `maps.Values` return iterators (Go 1.23+) or slices (Go 1.21-1.22). Use `slices.Collect` to materialize iterators into slices.

### Intermediate Verification

```bash
go run main.go
```

Expected (sorted sections are deterministic):

```
Keys (unsorted): [Tokyo Delhi Shanghai Sao Paulo Mumbai]
Keys (sorted): [Delhi Mumbai Sao Paulo Shanghai Tokyo]
Values (sorted): [11030000 12330000 12440000 13960000 24870000]

Sorted by city:
  Delhi        11030000
  Mumbai       12440000
  Sao Paulo    12330000
  Shanghai     24870000
  Tokyo        13960000
```

## Step 2 -- Clone and Copy

```go
package main

import (
	"fmt"
	"maps"
)

func main() {
	original := map[string]int{
		"a": 1, "b": 2, "c": 3,
	}

	// Clone creates a shallow copy
	cloned := maps.Clone(original)
	cloned["a"] = 999
	fmt.Println("Original:", original) // a:1 unchanged
	fmt.Println("Cloned:", cloned)     // a:999

	// Copy merges src into dst (overwrites existing keys)
	dst := map[string]int{"x": 10, "a": 100}
	maps.Copy(dst, original)
	fmt.Println("After Copy:", dst) // x:10, a:1, b:2, c:3
}
```

`maps.Clone` allocates a new map. `maps.Copy` writes all key-value pairs from the source into an existing destination, overwriting on key collision.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Original: map[a:1 b:2 c:3]
Cloned: map[a:999 b:2 c:3]
After Copy: map[a:1 b:2 c:3 x:10]
```

## Step 3 -- Equal and EqualFunc

```go
package main

import (
	"fmt"
	"maps"
	"strings"
)

func main() {
	a := map[string]int{"x": 1, "y": 2, "z": 3}
	b := map[string]int{"x": 1, "y": 2, "z": 3}
	c := map[string]int{"x": 1, "y": 2}

	fmt.Println("a == b:", maps.Equal(a, b))
	fmt.Println("a == c:", maps.Equal(a, c))

	// EqualFunc with custom comparison
	upper := map[string]string{"greeting": "HELLO", "farewell": "BYE"}
	lower := map[string]string{"greeting": "hello", "farewell": "bye"}

	caseInsensitiveEqual := maps.EqualFunc(upper, lower, func(a, b string) bool {
		return strings.EqualFold(a, b)
	})
	fmt.Println("Case-insensitive equal:", caseInsensitiveEqual)
}
```

`maps.Equal` compares both keys and values using `==`. `maps.EqualFunc` lets you define custom value comparison.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
a == b: true
a == c: false
Case-insensitive equal: true
```

## Step 4 -- DeleteFunc

```go
package main

import (
	"fmt"
	"maps"
)

func main() {
	scores := map[string]int{
		"Alice":   95,
		"Bob":     42,
		"Charlie": 78,
		"Diana":   31,
		"Eve":     88,
	}

	fmt.Println("Before:", scores)

	// Delete all entries where score < 50
	maps.DeleteFunc(scores, func(name string, score int) bool {
		return score < 50
	})

	fmt.Println("After filtering:", scores)
}
```

`maps.DeleteFunc` removes entries in place based on a predicate. It is the map equivalent of filtering.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Before: map[Alice:95 Bob:42 Charlie:78 Diana:31 Eve:88]
After filtering: map[Alice:95 Charlie:78 Eve:88]
```

## Step 5 -- Combining maps and slices for Real-World Patterns

```go
package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
)

func main() {
	// Word frequency counter
	text := []string{"go", "is", "fun", "go", "is", "fast", "go", "is", "simple"}
	freq := make(map[string]int)
	for _, word := range text {
		freq[word]++
	}

	// Sort by frequency (descending), then alphabetically
	type entry struct {
		word  string
		count int
	}

	entries := make([]entry, 0, len(freq))
	for word, count := range freq {
		entries = append(entries, entry{word, count})
	}

	slices.SortFunc(entries, func(a, b entry) int {
		if c := cmp.Compare(b.count, a.count); c != 0 {
			return c // descending by count
		}
		return cmp.Compare(a.word, b.word) // ascending by word
	})

	fmt.Println("Word frequencies:")
	for _, e := range entries {
		fmt.Printf("  %-10s %d\n", e.word, e.count)
	}

	// Invert a map (swap keys and values)
	colors := map[string]string{
		"red": "#FF0000", "green": "#00FF00", "blue": "#0000FF",
	}
	inverted := make(map[string]string, len(colors))
	for k, v := range colors {
		inverted[v] = k
	}
	fmt.Println("\nInverted colors:", inverted)
	_ = maps.Clone(inverted) // just to show maps import is used
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Word frequencies:
  go         3
  is         3
  fast       1
  fun        1
  simple     1

Inverted colors: map[#0000FF:blue #00FF00:green #FF0000:red]
```

## Common Mistakes

### Assuming maps.Clone is a Deep Copy

**Wrong:**

```go
original := map[string][]int{"a": {1, 2, 3}}
cloned := maps.Clone(original)
cloned["a"][0] = 999
fmt.Println(original["a"][0]) // 999 -- shared slice!
```

**What happens:** `maps.Clone` creates a shallow copy. Slice values are shared, not cloned.

**Fix:** Deep-clone manually: iterate and clone each slice value.

### Forgetting maps.Keys Returns an Iterator in Go 1.23+

**Wrong (Go 1.23+):**

```go
keys := maps.Keys(m)       // returns iter.Seq[K], not []K
fmt.Println(keys)           // prints function address, not keys
```

**Fix:** Use `slices.Collect(maps.Keys(m))` to materialize into a slice.

## Verify What You Learned

1. Clone a map, modify the clone, and verify the original is unchanged
2. Write a function that merges two maps, with the second map's values taking priority on key conflicts
3. Use `maps.DeleteFunc` to remove all entries from a `map[string]string` where the value is empty

## What's Next

Continue to [11 - Slice Memory Leaks](../11-slice-memory-leaks/11-slice-memory-leaks.md) to learn about common memory leak patterns with slices and how to avoid them.

## Summary

- `maps.Keys` and `maps.Values` extract data into iterators (use `slices.Collect` to get slices)
- `maps.Clone` creates a shallow copy of a map
- `maps.Copy` merges source entries into a destination map
- `maps.Equal` and `maps.EqualFunc` compare maps for equality
- `maps.DeleteFunc` removes entries matching a predicate
- Combine `maps.Keys` + `slices.Sort` for deterministic ordered iteration
- `maps.Clone` is shallow: slice/map/pointer values are shared, not deep-copied

## Reference

- [maps package](https://pkg.go.dev/maps)
- [slices package](https://pkg.go.dev/slices)
- [Go 1.21 Release Notes](https://go.dev/doc/go1.21)
