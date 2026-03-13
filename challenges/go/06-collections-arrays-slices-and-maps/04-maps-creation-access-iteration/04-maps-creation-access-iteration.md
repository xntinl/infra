# 4. Maps: Creation, Access, and Iteration

<!--
difficulty: basic
concepts: [maps, map-literal, make-map, comma-ok-idiom, delete, iteration, zero-value-access]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [variables-and-types, control-flow, slices-creation-append-capacity]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with Go variables, types, and loops
- Understanding of slices (exercises 01-03)
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** maps using literals and `make`
- **Use** the comma-ok idiom to distinguish missing keys from zero values
- **Iterate** over maps with range and explain why order is not guaranteed

## Why Maps

Maps are Go's built-in hash table, providing O(1) average-time lookups, insertions, and deletions by key. They are used everywhere: configuration stores, caches, counting occurrences, grouping data, and building indexes. Go maps have a few important behaviors that differ from other languages: accessing a missing key returns the zero value (no error, no nil pointer), iteration order is randomized, and maps are not safe for concurrent use.

## Step 1 -- Creating Maps

```bash
mkdir -p ~/go-exercises/maps-basics
cd ~/go-exercises/maps-basics
go mod init maps-basics
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Map literal
	ages := map[string]int{
		"Alice":   30,
		"Bob":     25,
		"Charlie": 35,
	}
	fmt.Println("Literal:", ages)

	// make(map[K]V) -- empty map
	scores := make(map[string]float64)
	scores["math"] = 95.5
	scores["science"] = 88.0
	fmt.Println("Make:", scores)

	// make with size hint (does not limit size, just pre-allocates)
	cache := make(map[int]string, 100)
	cache[1] = "one"
	fmt.Println("Cache:", cache, "len:", len(cache))

	// Nil map -- reads return zero value, writes panic
	var nilMap map[string]int
	fmt.Println("Nil map:", nilMap, nilMap == nil)
	fmt.Println("Read from nil:", nilMap["key"]) // 0, no panic
	// nilMap["key"] = 1 // PANIC: assignment to entry in nil map
}
```

Use `make` or a literal to create a map before writing to it. A nil map is safe to read from but panics on write.

### Intermediate Verification

```bash
go run main.go
```

Expected (map print order varies):

```
Literal: map[Alice:30 Bob:25 Charlie:35]
Make: map[math:95.5 science:88]
Cache: map[1:one] len: 1
Nil map: map[] true
Read from nil: 0
```

## Step 2 -- Access, Update, and Delete

```go
package main

import "fmt"

func main() {
	m := map[string]int{
		"apples":  5,
		"bananas": 3,
		"oranges": 8,
	}

	// Access
	fmt.Println("Apples:", m["apples"])

	// Update
	m["apples"] = 10
	fmt.Println("Updated apples:", m["apples"])

	// Delete
	delete(m, "bananas")
	fmt.Println("After delete:", m)

	// Access missing key returns zero value
	fmt.Println("Missing key:", m["grapes"]) // 0

	// Increment pattern (works even for new keys)
	m["grapes"]++
	m["grapes"]++
	fmt.Println("Grapes after ++:", m["grapes"]) // 2
}
```

The `delete` function is a no-op if the key does not exist. The zero-value behavior of maps makes counting and accumulating patterns clean.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Apples: 5
Updated apples: 10
After delete: map[apples:10 oranges:8]
Missing key: 0
Grapes after ++: 2
```

## Step 3 -- The Comma-Ok Idiom

```go
package main

import "fmt"

func main() {
	temps := map[string]float64{
		"Portland": 72.5,
		"Seattle":  68.0,
		"Denver":   0.0, // actually zero degrees!
	}

	// Without comma-ok: can't distinguish "missing" from "zero"
	fmt.Println("Phoenix:", temps["Phoenix"]) // 0 -- missing or zero?

	// With comma-ok: second value tells you if the key exists
	val, ok := temps["Phoenix"]
	fmt.Printf("Phoenix: val=%.1f ok=%v\n", val, ok)

	val, ok = temps["Denver"]
	fmt.Printf("Denver:  val=%.1f ok=%v\n", val, ok)

	// Common pattern: check and branch
	if temp, exists := temps["Seattle"]; exists {
		fmt.Printf("Seattle temp: %.1f\n", temp)
	} else {
		fmt.Println("Seattle not found")
	}
}
```

The comma-ok idiom is essential when the zero value is a valid value. Always use it when the distinction matters.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Phoenix: 0
Phoenix: val=0.0 ok=false
Denver:  val=0.0 ok=true
Seattle temp: 68.0
```

## Step 4 -- Iterating Over Maps

```go
package main

import (
	"fmt"
	"sort"
)

func main() {
	population := map[string]int{
		"Tokyo":     13960000,
		"Delhi":     11030000,
		"Shanghai":  24870000,
		"Sao Paulo": 12330000,
		"Mumbai":    12440000,
	}

	// Range over map -- order is NOT guaranteed
	fmt.Println("Random order:")
	for city, pop := range population {
		fmt.Printf("  %s: %d\n", city, pop)
	}

	// For sorted output, collect keys and sort them
	cities := make([]string, 0, len(population))
	for city := range population {
		cities = append(cities, city)
	}
	sort.Strings(cities)

	fmt.Println("\nSorted by city:")
	for _, city := range cities {
		fmt.Printf("  %s: %d\n", city, population[city])
	}
}
```

Go deliberately randomizes map iteration order to prevent code from depending on it. If you need ordered output, collect the keys and sort them.

### Intermediate Verification

```bash
go run main.go
```

Expected (first block order varies, second block always sorted):

```
Random order:
  Tokyo: 13960000
  Delhi: 11030000
  Shanghai: 24870000
  Sao Paulo: 12330000
  Mumbai: 12440000

Sorted by city:
  Delhi: 11030000
  Mumbai: 12440000
  Sao Paulo: 12330000
  Shanghai: 24870000
  Tokyo: 13960000
```

## Step 5 -- Practical Example: Word Counter

```go
package main

import (
	"fmt"
	"strings"
)

func wordCount(text string) map[string]int {
	counts := make(map[string]int)
	for _, word := range strings.Fields(text) {
		counts[strings.ToLower(word)]++
	}
	return counts
}

func main() {
	text := "the quick brown fox jumps over the lazy dog the fox"
	counts := wordCount(text)

	for word, count := range counts {
		fmt.Printf("  %-10s %d\n", word, count)
	}
}
```

This demonstrates the idiomatic Go pattern of using maps for counting. The zero-value behavior means you never need to check if a key exists before incrementing.

### Intermediate Verification

```bash
go run main.go
```

Expected (order varies):

```
  the        3
  quick      1
  brown      1
  fox        2
  jumps      1
  over       1
  lazy       1
  dog        1
```

## Common Mistakes

### Writing to a Nil Map

**Wrong:**

```go
var m map[string]int
m["key"] = 1 // panic: assignment to entry in nil map
```

**What happens:** A nil map has no backing storage. Write operations panic.

**Fix:** Initialize with `make(map[string]int)` or a map literal before writing.

### Relying on Iteration Order

**Wrong:**

```go
m := map[int]string{1: "a", 2: "b", 3: "c"}
for k, v := range m {
    fmt.Println(k, v) // assuming 1, 2, 3 order
}
```

**What happens:** Map iteration order is randomized. The output changes between runs.

**Fix:** Collect keys, sort them, and iterate over the sorted keys.

### Comparing Maps with ==

**Wrong:**

```go
a := map[string]int{"x": 1}
b := map[string]int{"x": 1}
fmt.Println(a == b) // compile error
```

**What happens:** Maps can only be compared to `nil`. Use `reflect.DeepEqual` or iterate and compare manually.

## Verify What You Learned

1. Create a map from country codes to country names (at least 5 entries) and print them in sorted order by code
2. Write a function that returns the key with the highest value in a `map[string]int`
3. Use the comma-ok idiom to safely look up a key and print different messages for found vs not found

## What's Next

Continue to [05 - Nil Slices vs Empty Slices](../05-nil-slices-vs-empty-slices/05-nil-slices-vs-empty-slices.md) to understand the subtle but important difference between nil and empty slices.

## Summary

- Maps are Go's built-in hash table: `map[KeyType]ValueType`
- Create maps with literals or `make`; nil maps are read-safe but write-panicking
- Access a missing key returns the zero value of the value type
- The comma-ok idiom (`val, ok := m[key]`) distinguishes missing from zero
- `delete(m, key)` removes an entry; no-op if key is absent
- Map iteration order is deliberately randomized
- Maps are not safe for concurrent read/write (use `sync.Mutex` or `sync.Map`)
- The `len` function returns the number of key-value pairs

## Reference

- [Go Spec: Map types](https://go.dev/ref/spec#Map_types)
- [Go Blog: Go maps in action](https://go.dev/blog/maps)
- [Effective Go: Maps](https://go.dev/doc/effective_go#maps)
