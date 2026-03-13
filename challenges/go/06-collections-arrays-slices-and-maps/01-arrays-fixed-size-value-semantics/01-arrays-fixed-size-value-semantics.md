# 1. Arrays: Fixed Size and Value Semantics

<!--
difficulty: basic
concepts: [arrays, fixed-size, value-semantics, array-comparison, array-iteration, zero-values]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [variables-and-types, functions, control-flow]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with Go variables and basic types
- Understanding of for loops and range
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** that Go arrays have a fixed size that is part of the type
- **Explain** value semantics: assigning an array copies the entire array
- **Identify** how arrays differ from slices in Go

## Why Arrays Matter

Arrays in Go are the foundation for slices, which you will use constantly. Understanding arrays first is important because their behavior explains why slices exist and how they work under the hood. Go arrays have two properties that surprise developers from other languages: the length is part of the type (`[3]int` and `[4]int` are different types), and arrays have value semantics (assigning one array to another copies all elements). These properties make arrays safe but inflexible, which is why slices are the preferred abstraction in practice.

## Step 1 -- Declaring and Initializing Arrays

Create a new project and explore array fundamentals.

```bash
mkdir -p ~/go-exercises/arrays
cd ~/go-exercises/arrays
go mod init arrays
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Declare an array of 5 integers -- zero-valued
	var counts [5]int
	fmt.Println("Zero-valued:", counts)

	// Initialize with values
	primes := [5]int{2, 3, 5, 7, 11}
	fmt.Println("Primes:", primes)

	// Let the compiler count the elements
	vowels := [...]string{"a", "e", "i", "o", "u"}
	fmt.Println("Vowels:", vowels)
	fmt.Printf("Type: %T, Length: %d\n", vowels, len(vowels))

	// Partial initialization -- unset elements are zero-valued
	sparse := [5]int{0: 10, 3: 40}
	fmt.Println("Sparse:", sparse)
}
```

The `[...]` syntax tells the compiler to count the initializer elements and set the length automatically. The result is still a fixed-size array, not a slice.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Zero-valued: [0 0 0 0 0]
Primes: [2 3 5 7 11]
Vowels: [a e i o u]
Type: [5]string, Length: 5
Sparse: [10 0 0 40 0]
```

## Step 2 -- Value Semantics: Arrays Are Copied on Assignment

```go
package main

import "fmt"

func main() {
	original := [3]int{10, 20, 30}
	copied := original // full copy -- not a reference

	copied[0] = 999

	fmt.Println("Original:", original)
	fmt.Println("Copied:  ", copied)
}
```

Modifying `copied` does not affect `original`. This is value semantics. Every assignment, every function argument pass, every return value creates a full copy of the array.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Original: [10 20 30]
Copied:   [999 20 30]
```

## Step 3 -- Arrays as Function Arguments

```go
package main

import "fmt"

func doubleElements(arr [4]int) [4]int {
	for i := range arr {
		arr[i] *= 2
	}
	return arr
}

func main() {
	data := [4]int{1, 2, 3, 4}
	doubled := doubleElements(data)

	fmt.Println("Original:", data)
	fmt.Println("Doubled: ", doubled)
}
```

The function receives a copy. The original `data` is unchanged. The function must return the modified array for the caller to see the changes.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Original: [1 2 3 4]
Doubled:  [2 4 6 8]
```

## Step 4 -- Array Comparison and Iteration

```go
package main

import "fmt"

func main() {
	a := [3]int{1, 2, 3}
	b := [3]int{1, 2, 3}
	c := [3]int{3, 2, 1}

	// Arrays of the same type can be compared with ==
	fmt.Println("a == b:", a == b)
	fmt.Println("a == c:", a == c)

	// Iterate with range
	temps := [7]float64{72.0, 75.5, 68.3, 80.1, 77.4, 73.2, 69.8}
	sum := 0.0
	for _, t := range temps {
		sum += t
	}
	fmt.Printf("Average temp: %.1f\n", sum/float64(len(temps)))

	// Iterate with index
	for i, t := range temps {
		fmt.Printf("  Day %d: %.1f\n", i+1, t)
	}
}
```

Arrays are comparable with `==` and `!=` as long as the element type is comparable. This is a feature slices do not have.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
a == b: true
a == c: false
Average temp: 73.8
  Day 1: 72.0
  Day 2: 75.5
  Day 3: 68.3
  Day 4: 80.1
  Day 5: 77.4
  Day 6: 73.2
  Day 7: 69.8
```

## Step 5 -- Size Is Part of the Type

```go
package main

import "fmt"

func printThree(arr [3]int) {
	fmt.Println(arr)
}

func main() {
	three := [3]int{1, 2, 3}
	// four := [4]int{1, 2, 3, 4}

	printThree(three)
	// printThree(four) // compile error: cannot use four ([4]int) as [3]int
}
```

Uncomment the `printThree(four)` line to see the compile error. `[3]int` and `[4]int` are completely different types. This rigidity is the primary reason slices are preferred in practice.

## Common Mistakes

### Assuming Arrays Are References

**Wrong:**

```go
data := [3]int{1, 2, 3}
alias := data
alias[0] = 999
fmt.Println(data[0]) // Expecting 999
```

**What happens:** `data[0]` is still `1`. The assignment copies the entire array.

**Fix:** If you need reference semantics, use a slice or pass a pointer to the array (`*[3]int`).

### Mixing Array and Slice Types

**Wrong:**

```go
func process(items []int) { /* ... */ }

data := [5]int{1, 2, 3, 4, 5}
process(data) // compile error
```

**What happens:** `[5]int` is an array; `[]int` is a slice. They are different types.

**Fix:** Use a slice expression: `process(data[:])`.

## Verify What You Learned

1. Declare an array of 12 strings representing the months of the year using `[...]string{...}` syntax
2. Assign that array to a new variable, change "January" to "Jan" in the copy, and print both to confirm the original is unchanged
3. Write a function that takes a `[5]float64` array and returns the maximum value

## What's Next

Continue to [02 - Slices: Creation, Append, and Capacity](../02-slices-creation-append-capacity/02-slices-creation-append-capacity.md) to learn how slices build on arrays with dynamic sizing and reference semantics.

## Summary

- Go arrays have a fixed length that is part of the type: `[3]int` differs from `[4]int`
- Arrays have value semantics: assignment and function calls copy the entire array
- Zero-valued arrays have all elements set to the zero value of the element type
- Arrays can be compared with `==` and `!=` (slices cannot)
- Use `[...]` to let the compiler count initializer elements
- Use index syntax for sparse initialization: `[5]int{0: 10, 3: 40}`
- In practice, slices are preferred over arrays for most use cases

## Reference

- [Go Spec: Array types](https://go.dev/ref/spec#Array_types)
- [Go Tour: Arrays](https://go.dev/tour/moretypes/6)
- [Effective Go: Arrays](https://go.dev/doc/effective_go#arrays)
