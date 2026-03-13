# 2. For Loops

<!--
difficulty: basic
concepts: [for-loop, three-component-for, while-loop, infinite-loop, break, continue]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [01-if-else-and-init-statements]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [01 - If/Else and Init Statements](../01-if-else-and-init-statements/01-if-else-and-init-statements.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** the three forms of `for` loops in Go
- **Convert** while-loop and do-while patterns to Go's `for`
- **Control** loop execution with `break` and `continue`

## Why For Loops

Go has only one loop keyword: `for`. It replaces `while`, `do-while`, and `for` from other languages. This simplicity means fewer constructs to remember, and the same keyword handles counted loops, condition-based loops, and infinite loops.

## Step 1 -- Three-Component For Loop

```bash
mkdir -p ~/go-exercises/for-loops
cd ~/go-exercises/for-loops
go mod init for-loops
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Classic three-component: init; condition; post
	fmt.Println("Classic for:")
	for i := 0; i < 5; i++ {
		fmt.Printf("  i = %d\n", i)
	}

	// Counting down
	fmt.Println("Countdown:")
	for i := 3; i > 0; i-- {
		fmt.Printf("  %d...\n", i)
	}
	fmt.Println("  Go!")

	// Step by 2
	fmt.Println("Evens:")
	for i := 0; i <= 10; i += 2 {
		fmt.Printf("  %d", i)
	}
	fmt.Println()
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/for-loops && go run main.go
```

Expected:

```
Classic for:
  i = 0
  i = 1
  i = 2
  i = 3
  i = 4
Countdown:
  3...
  2...
  1...
  Go!
Evens:
  0  2  4  6  8  10
```

## Step 2 -- Condition-Only For (While Loop)

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Condition-only for (equivalent to while)
	fmt.Println("While-style:")
	n := 1
	for n < 100 {
		n *= 2
	}
	fmt.Println("  First power of 2 >= 100:", n)

	// Summing until a condition
	total := 0
	i := 1
	for total < 50 {
		total += i
		i++
	}
	fmt.Printf("  Sum exceeded 50 at i=%d, total=%d\n", i-1, total)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/for-loops && go run main.go
```

Expected:

```
While-style:
  First power of 2 >= 100: 128
  Sum exceeded 50 at i=10, total=55
```

## Step 3 -- Infinite Loop with Break

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Infinite loop -- must break explicitly
	fmt.Println("Infinite loop with break:")
	count := 0
	for {
		count++
		if count > 5 {
			break
		}
		fmt.Printf("  count = %d\n", count)
	}
	fmt.Printf("  Exited at count = %d\n", count)

	// Continue skips the rest of the loop body
	fmt.Println("Skip evens with continue:")
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			continue
		}
		fmt.Printf("  %d", i)
	}
	fmt.Println()
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/for-loops && go run main.go
```

Expected:

```
Infinite loop with break:
  count = 1
  count = 2
  count = 3
  count = 4
  count = 5
  Exited at count = 6
Skip evens with continue:
  1  3  5  7  9
```

## Step 4 -- Nested Loops and Practical Patterns

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Nested loops -- multiplication table
	fmt.Println("Multiplication table (1-4):")
	for i := 1; i <= 4; i++ {
		for j := 1; j <= 4; j++ {
			fmt.Printf("%4d", i*j)
		}
		fmt.Println()
	}

	// FizzBuzz
	fmt.Println("\nFizzBuzz (1-20):")
	for i := 1; i <= 20; i++ {
		switch {
		case i%15 == 0:
			fmt.Printf("  FizzBuzz")
		case i%3 == 0:
			fmt.Printf("  Fizz")
		case i%5 == 0:
			fmt.Printf("  Buzz")
		default:
			fmt.Printf("  %d", i)
		}
	}
	fmt.Println()

	// do-while pattern: execute at least once
	fmt.Println("\nDo-while pattern:")
	x := 100
	for {
		fmt.Printf("  x = %d\n", x)
		x /= 2
		if x == 0 {
			break
		}
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/for-loops && go run main.go
```

Expected:

```
Multiplication table (1-4):
   1   2   3   4
   2   4   6   8
   3   6   9  12
   4   8  12  16

FizzBuzz (1-20):
  1  2  Fizz  4  Buzz  Fizz  7  8  Fizz  Buzz  11  Fizz  13  14  FizzBuzz  16  17  Fizz  19  Buzz

Do-while pattern:
  x = 100
  x = 50
  x = 25
  x = 12
  x = 6
  x = 3
  x = 1
```

## Common Mistakes

### Off-by-One Errors

**Wrong:** `for i := 0; i <= len(slice); i++` -- this accesses one element past the end.

**Fix:** Use `<` for zero-based indexing: `for i := 0; i < len(slice); i++`. Better yet, use `range`.

### Modifying the Loop Variable Inside the Body

**Wrong:** Changing `i` inside a three-component loop leads to confusing behavior.

**Fix:** If you need dynamic iteration control, use a condition-only `for` loop instead.

### Forgetting to Update the Condition in a While Loop

**Wrong:**

```go
for n > 0 {
    // forgot to decrement n -> infinite loop
}
```

**Fix:** Ensure the loop body always progresses toward the exit condition.

## Verify What You Learned

```bash
cd ~/go-exercises/for-loops && go run main.go
```

Write a loop that finds the first number greater than 1000 that is divisible by both 7 and 13.

## What's Next

Continue to [03 - Switch Statements](../03-switch-statements/03-switch-statements.md) to learn Go's versatile switch construct.

## Summary

- Go has one loop keyword: `for`
- Three-component: `for init; condition; post { }`
- Condition-only (while): `for condition { }`
- Infinite: `for { }` -- use `break` to exit
- `continue` skips to the next iteration
- `break` exits the innermost loop
- No parentheses around the loop expression; braces are required

## Reference

- [Go Specification: For Statements](https://go.dev/ref/spec#For_statements)
- [Effective Go: For](https://go.dev/doc/effective_go#for)
