# 5. Range Over Collections

<!--
difficulty: basic
concepts: [range, slice-iteration, map-iteration, string-iteration, index-value, channel-range]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [02-for-loops]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [02 - For Loops](../02-for-loops/02-for-loops.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Iterate** over slices, arrays, maps, and strings with `range`
- **Choose** between index-only, value-only, and index-value iteration
- **Explain** how `range` handles strings as UTF-8 rune sequences
- **Recognize** map iteration order as non-deterministic

## Why Range Over Collections

The `range` keyword provides a clean, idiomatic way to iterate over Go's built-in collections. It handles index tracking, bounds checking, and UTF-8 decoding automatically. Using `range` instead of manual indexing prevents off-by-one errors and produces more readable code.

## Step 1 -- Range Over Slices

```bash
mkdir -p ~/go-exercises/range
cd ~/go-exercises/range
go mod init range-demo
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fruits := []string{"apple", "banana", "cherry", "date"}

	// Index and value
	fmt.Println("Index and value:")
	for i, fruit := range fruits {
		fmt.Printf("  [%d] %s\n", i, fruit)
	}

	// Value only (discard index)
	fmt.Println("Value only:")
	for _, fruit := range fruits {
		fmt.Printf("  %s\n", fruit)
	}

	// Index only
	fmt.Println("Index only:")
	for i := range fruits {
		fmt.Printf("  %d\n", i)
	}

	// Range over an array
	numbers := [5]int{10, 20, 30, 40, 50}
	sum := 0
	for _, n := range numbers {
		sum += n
	}
	fmt.Println("Array sum:", sum)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/range && go run main.go
```

Expected:

```
Index and value:
  [0] apple
  [1] banana
  [2] cherry
  [3] date
Value only:
  apple
  banana
  cherry
  date
Index only:
  0
  1
  2
  3
Array sum: 150
```

## Step 2 -- Range Over Maps

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	ages := map[string]int{
		"Alice":   30,
		"Bob":     25,
		"Charlie": 35,
	}

	// Key and value
	fmt.Println("Key-value pairs:")
	for name, age := range ages {
		fmt.Printf("  %s is %d years old\n", name, age)
	}

	// Keys only
	fmt.Println("Keys only:")
	for name := range ages {
		fmt.Printf("  %s\n", name)
	}

	// Note: map iteration order is NOT guaranteed
	// Running multiple times may produce different orders
	fmt.Println("Order may vary between runs!")

	// Counting word frequencies
	words := []string{"go", "is", "go", "fast", "go", "is", "fun"}
	freq := make(map[string]int)
	for _, word := range words {
		freq[word]++
	}
	fmt.Println("\nWord frequencies:")
	for word, count := range freq {
		fmt.Printf("  %s: %d\n", word, count)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/range && go run main.go
```

Expected (order may vary):

```
Key-value pairs:
  Alice is 30 years old
  Bob is 25 years old
  Charlie is 35 years old
Keys only:
  Alice
  Bob
  Charlie
Order may vary between runs!

Word frequencies:
  go: 3
  is: 2
  fast: 1
  fun: 1
```

## Step 3 -- Range Over Strings

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Range over string iterates by rune, not by byte
	s := "Hello, Go!"
	fmt.Println("Rune iteration:")
	for i, r := range s {
		fmt.Printf("  byte[%d] = %c (U+%04X)\n", i, r, r)
	}

	// Multi-byte characters
	emoji := "Go"
	fmt.Println("\nMulti-byte string:")
	fmt.Printf("  bytes: %d, runes: %d\n", len(emoji), len([]rune(emoji)))
	for i, r := range emoji {
		fmt.Printf("  byte[%d] = %c (U+%04X, %d bytes)\n", i, r, r, len(string(r)))
	}

	// Note: byte index jumps for multi-byte runes
	mixed := "aBC"
	fmt.Println("\nByte index jumps:")
	for i, r := range mixed {
		fmt.Printf("  index=%d rune=%c\n", i, r)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/range && go run main.go
```

Expected:

```
Rune iteration:
  byte[0] = H (U+0048)
  byte[1] = e (U+0065)
  byte[2] = l (U+006C)
  byte[3] = l (U+006C)
  byte[4] = o (U+006F)
  byte[5] = , (U+002C)
  byte[6] =   (U+0020)
  byte[7] = G (U+0047)
  byte[8] = o (U+006F)
  byte[9] = ! (U+0021)
```

The multi-byte examples will show byte index gaps where multi-byte runes span multiple bytes.

## Step 4 -- Range Gotchas

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Gotcha 1: range copies values, not references
	nums := []int{1, 2, 3, 4, 5}
	for _, v := range nums {
		v *= 2 // modifies the copy, not the original
	}
	fmt.Println("After range (copy):", nums) // unchanged

	// Fix: use the index to modify
	for i := range nums {
		nums[i] *= 2
	}
	fmt.Println("After index modify:", nums) // doubled

	// Gotcha 2: range over nil is safe (zero iterations)
	var nilSlice []int
	for _, v := range nilSlice {
		fmt.Println("Never printed:", v)
	}
	fmt.Println("Nil slice range: safe (0 iterations)")

	// Gotcha 3: capturing loop variable in goroutine/closure
	// (Less of an issue since Go 1.22 loop variable semantics)
	values := []string{"a", "b", "c"}
	funcs := make([]func(), len(values))
	for i, v := range values {
		funcs[i] = func() { fmt.Printf("  %s", v) }
	}
	fmt.Print("Closures: ")
	for _, f := range funcs {
		f()
	}
	fmt.Println()
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/range && go run main.go
```

Expected:

```
After range (copy): [1 2 3 4 5]
After index modify: [2 4 6 8 10]
Nil slice range: safe (0 iterations)
Closures:   a  b  c
```

## Common Mistakes

### Modifying Slice Elements Through the Value Variable

**Wrong:** `for _, v := range slice { v = newValue }` -- this modifies a copy.

**Fix:** Use `for i := range slice { slice[i] = newValue }`.

### Relying on Map Iteration Order

**Wrong:** Assuming maps iterate in insertion order or alphabetical order.

**What happens:** Go intentionally randomizes map iteration order.

**Fix:** Sort the keys first if you need deterministic order.

### Forgetting That String Range Yields Runes

**Wrong:** Expecting `range` over a string to produce byte indices with consecutive values.

**What happens:** The index jumps by the byte width of each rune. For ASCII this is 1, but for multi-byte UTF-8 it can be 2-4.

**Fix:** Use byte index awareness, or iterate over `[]byte(s)` for raw bytes.

## Verify What You Learned

```bash
cd ~/go-exercises/range && go run main.go
```

Write a program that counts the number of vowels in a string using `range`.

## What's Next

Continue to [06 - Labels, Break, Continue, and Goto](../06-labels-break-continue-goto/06-labels-break-continue-goto.md) to learn about labeled loop control.

## Summary

- `range` iterates over slices, arrays, maps, strings, and channels
- Slice/array: `for i, v := range collection` yields index and value copy
- Map: `for k, v := range m` yields key and value; order is non-deterministic
- String: `for i, r := range s` yields byte index and rune (Unicode code point)
- Use index to modify slice elements; the value variable is a copy
- Range over nil collections is safe -- zero iterations, no panic

## Reference

- [Go Specification: For Range](https://go.dev/ref/spec#For_range)
- [Effective Go: For Range](https://go.dev/doc/effective_go#for)
- [Go Blog: Strings, bytes, runes](https://go.dev/blog/strings)
