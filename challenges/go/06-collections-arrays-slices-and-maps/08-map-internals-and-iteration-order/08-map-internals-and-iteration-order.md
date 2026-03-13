# 8. Map Internals and Iteration Order

<!--
difficulty: intermediate
concepts: [map-internals, hash-table, buckets, load-factor, iteration-randomization, map-growth, concurrent-map-access]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [maps-creation-access-iteration, slice-internals]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-07 in this section
- Solid understanding of map creation, access, and iteration
- Familiarity with hash tables conceptually

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how Go maps are structured internally (hash, buckets, overflow)
- **Explain** why map iteration order is randomized and not stable across runs
- **Predict** map growth behavior and its impact on performance

## Why Map Internals

Go maps are one of the most optimized data structures in the runtime. Understanding their internal mechanics -- hashing, bucket layout, load factor, and incremental growth -- helps you make better decisions about initial sizing, key type selection, and performance characteristics. It also demystifies behaviors like randomized iteration order (a deliberate security measure) and the "concurrent map read and write" fatal error.

## Step 1 -- Map Header and Reference Semantics

```bash
mkdir -p ~/go-exercises/map-internals
cd ~/go-exercises/map-internals
go mod init map-internals
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	m := map[string]int{"a": 1, "b": 2}

	// A map variable is a pointer to a runtime.hmap struct
	fmt.Printf("Size of map variable: %d bytes\n", unsafe.Sizeof(m))

	// Assignment copies the pointer, not the data
	m2 := m
	m2["c"] = 3
	fmt.Println("m:", m)   // includes "c":3
	fmt.Println("m2:", m2) // same map
}
```

Unlike slices (which are a 3-field struct), a map variable is a single pointer. This means map assignment and function passing always share the same underlying data.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Size of map variable: 8 bytes
m: map[a:1 b:2 c:3]
m2: map[a:1 b:2 c:3]
```

## Step 2 -- Randomized Iteration Order

```go
package main

import "fmt"

func main() {
	m := map[int]string{
		1: "one", 2: "two", 3: "three",
		4: "four", 5: "five", 6: "six",
	}

	// Run multiple iterations to show randomization
	for run := 0; run < 5; run++ {
		keys := make([]int, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		fmt.Printf("Run %d: %v\n", run, keys)
	}
}
```

Go's map iterator starts at a random bucket and a random offset within that bucket. This is intentional: it prevents code from accidentally depending on insertion order, which would break when the runtime implementation changes.

### Intermediate Verification

```bash
go run main.go
```

Expected (order differs each run):

```
Run 0: [3 4 5 6 1 2]
Run 1: [5 6 1 2 3 4]
Run 2: [1 2 3 4 5 6]
Run 3: [6 1 2 3 4 5]
Run 4: [2 3 4 5 6 1]
```

## Step 3 -- Map Growth and Load Factor

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	// Demonstrate the cost of map growth
	var mBefore, mAfter runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&mBefore)

	// Without size hint -- many reallocations
	m1 := make(map[int]int)
	for i := 0; i < 1_000_000; i++ {
		m1[i] = i
	}

	runtime.GC()
	runtime.ReadMemStats(&mAfter)
	fmt.Printf("Without hint: alloc=%d MB, mallocs=%d\n",
		(mAfter.TotalAlloc-mBefore.TotalAlloc)/(1024*1024),
		mAfter.Mallocs-mBefore.Mallocs)

	// Reset
	runtime.GC()
	runtime.ReadMemStats(&mBefore)

	// With size hint -- fewer reallocations
	m2 := make(map[int]int, 1_000_000)
	for i := 0; i < 1_000_000; i++ {
		m2[i] = i
	}

	runtime.GC()
	runtime.ReadMemStats(&mAfter)
	fmt.Printf("With hint:    alloc=%d MB, mallocs=%d\n",
		(mAfter.TotalAlloc-mBefore.TotalAlloc)/(1024*1024),
		mAfter.Mallocs-mBefore.Mallocs)

	_ = m1
	_ = m2
}
```

Maps grow when the load factor (elements / buckets) exceeds ~6.5. Growth doubles the number of buckets and rehashes incrementally. Pre-sizing with `make(map[K]V, n)` reduces the number of growth events.

### Intermediate Verification

```bash
go run main.go
```

The version with the size hint should use fewer allocations and potentially less total memory.

## Step 4 -- Key Hashing and Equality

```go
package main

import "fmt"

type Point struct {
	X, Y int
}

func main() {
	// Structs with all comparable fields can be map keys
	distances := map[Point]float64{
		{0, 0}: 0.0,
		{3, 4}: 5.0,
		{1, 1}: 1.414,
	}
	fmt.Println("Point map:", distances)
	fmt.Println("{3,4}:", distances[Point{3, 4}])

	// Arrays can be map keys (fixed size, comparable)
	grid := map[[2]int]string{
		{0, 0}: "origin",
		{1, 0}: "right",
		{0, 1}: "up",
	}
	fmt.Println("Grid:", grid)

	// These types CANNOT be map keys (compile error):
	// map[[]int]string{}     -- slices are not comparable
	// map[map[int]int]string{} -- maps are not comparable
	// map[func()]string{}    -- functions are not comparable
}
```

Map keys must be comparable (support `==`). This includes booleans, numbers, strings, pointers, channels, interfaces, structs of comparable fields, and arrays of comparable elements.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Point map: map[{0 0}:0 {1 1}:1.414 {3 4}:5]
{3,4}: 5
Grid: map[[0 0]:origin [0 1]:up [1 0]:right]
```

## Step 5 -- Concurrent Map Access Detection

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	m := make(map[int]int)
	var mu sync.Mutex

	// WRONG: concurrent read/write without protection
	// This would cause: fatal error: concurrent map read and map write
	// Uncomment to see the crash:
	//
	// var wg sync.WaitGroup
	// for i := 0; i < 100; i++ {
	//     wg.Add(1)
	//     go func(i int) {
	//         defer wg.Done()
	//         m[i] = i        // concurrent write
	//         _ = m[i-1]      // concurrent read
	//     }(i)
	// }
	// wg.Wait()

	// CORRECT: protect with a mutex
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mu.Lock()
			m[i] = i * i
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	fmt.Println("Entries:", len(m))
	fmt.Println("m[7]:", m[7])
	fmt.Println("m[42]:", m[42])
}
```

Go's runtime has a built-in race detector for maps. Concurrent read and write to the same map causes a fatal crash (not a panic -- it cannot be recovered). Use `sync.Mutex`, `sync.RWMutex`, or `sync.Map` for concurrent access.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Entries: 100
m[7]: 49
m[42]: 1764
```

## Common Mistakes

### Assuming Maps Are Passed by Value

**Wrong:**

```go
func addEntry(m map[string]int) {
    m["new"] = 42
}
// Caller's map IS modified -- maps are reference types
```

**What happens:** This actually works as intended, but developers from other languages sometimes expect maps to be copied.

**Clarification:** Map variables are pointers. Passing to a function shares the same map.

### Taking Address of Map Values

**Wrong:**

```go
m := map[string]int{"a": 1}
p := &m["a"] // compile error: cannot take address of m["a"]
```

**What happens:** Map values may move during growth. Go prevents taking their address.

**Fix:** Copy the value to a local variable first: `v := m["a"]; p := &v`.

## Verify What You Learned

1. Demonstrate that assigning a map to a new variable creates a reference, not a copy (modify through one, read through the other)
2. Write a program that iterates over the same map 10 times and collect the order of keys each time to prove randomization
3. Benchmark `make(map[string]int)` vs `make(map[string]int, 10000)` when inserting 10,000 elements

## What's Next

Continue to [09 - Slices Package](../09-slices-package/09-slices-package.md) to learn the standard library functions for slice manipulation introduced in Go 1.21.

## Summary

- A map variable is a pointer to a `runtime.hmap` struct (8 bytes on 64-bit)
- Maps use hash tables with buckets, each holding up to 8 key-value pairs
- Load factor threshold is ~6.5; exceeding it triggers incremental growth
- Iteration order is deliberately randomized per iteration
- Map keys must be comparable types (no slices, maps, or functions)
- Concurrent read/write causes a fatal error; use `sync.Mutex` or `sync.Map`
- Pre-sizing with `make(map[K]V, n)` reduces growth overhead
- You cannot take the address of a map value

## Reference

- [Go Blog: Go maps in action](https://go.dev/blog/maps)
- [Go Source: runtime/map.go](https://github.com/golang/go/blob/master/src/runtime/map.go)
- [Go Spec: Map types](https://go.dev/ref/spec#Map_types)
- [GopherCon 2016: Keith Randall - Inside the Map Implementation](https://www.youtube.com/watch?v=Tl7mi9QmLns)
