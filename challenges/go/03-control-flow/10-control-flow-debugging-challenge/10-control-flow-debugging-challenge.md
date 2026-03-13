# 10. Control Flow Debugging Challenge

<!--
difficulty: advanced
concepts: [defer-ordering, panic-recover, labeled-break, loop-semantics, range-gotchas, switch-fallthrough, init-statement-scope]
tools: [go]
estimated_time: 30m
bloom_level: evaluate
prerequisites: [01-if-else-and-init-statements, 02-for-loops, 03-switch-statements, 05-range-over-collections, 06-labels-break-continue-goto, 07-defer-semantics-and-ordering, 08-panic-and-recover]
-->

## The Challenge

You have inherited a Go program with seven bugs, each caused by a misunderstanding of Go's control flow semantics. The program compiles and runs but produces incorrect output. Your job is to identify each bug, explain what is going wrong, and fix it so the program produces the expected output.

Each function is independent. Fix them one at a time.

## Requirements

1. Read the buggy program below and create it as `main.go`
2. Run it and observe the incorrect output
3. Fix all seven bugs so the output matches the expected output exactly
4. The program must compile with no errors and no warnings from `go vet`

## The Buggy Program

```bash
mkdir -p ~/go-exercises/cf-debug
cd ~/go-exercises/cf-debug
go mod init cf-debug
```

Create `main.go` with the following code. **Do not change the function signatures or the print format strings** -- only fix the logic bugs.

```go
package main

import (
	"fmt"
	"strings"
)

// Bug 1: defer argument evaluation
func bug1_elapsed() {
	steps := 0
	defer fmt.Printf("bug1: steps = %d\n", steps)
	steps++
	steps++
	steps++
}

// Bug 2: break inside switch inside for
func bug2_firstNegative(numbers []int) int {
	for _, n := range numbers {
		switch {
		case n < 0:
			break
		}
	}
	return 0
}

// Bug 3: range value copy
func bug3_doubleSlice(nums []int) {
	for _, v := range nums {
		v *= 2
	}
}

// Bug 4: defer ordering assumption
func bug4_buildMessage() string {
	var parts []string
	defer func() {
		parts = append(parts, "world")
	}()
	defer func() {
		parts = append(parts, "hello")
	}()
	parts = append(parts, "!")
	return strings.Join(parts, " ")
}

// Bug 5: recover placement
func bug5_safeDiv(a, b int) (int, error) {
	go func() {
		if r := recover(); r != nil {
			fmt.Println("recovered:", r)
		}
	}()
	return a / b, nil
}

// Bug 6: init statement scope
func bug6_lookup(m map[string]int, key string) {
	if v, ok := m[key]; ok {
		fmt.Printf("bug6: found %s = %d\n", key, v)
	}
	fmt.Printf("bug6: final value = %d\n", v)
}

// Bug 7: infinite loop with wrong condition
func bug7_collatz(n int) int {
	steps := 0
	for n == 1 {
		if n%2 == 0 {
			n /= 2
		} else {
			n = 3*n + 1
		}
		steps++
	}
	return steps
}

func main() {
	fmt.Println("=== Control Flow Debugging Challenge ===")
	fmt.Println()

	// Bug 1
	bug1_elapsed()

	// Bug 2
	nums := []int{3, 7, -2, 5, -8, 1}
	result := bug2_firstNegative(nums)
	fmt.Printf("bug2: first negative = %d\n", result)

	// Bug 3
	values := []int{1, 2, 3, 4, 5}
	bug3_doubleSlice(values)
	fmt.Printf("bug3: doubled = %v\n", values)

	// Bug 4
	msg := bug4_buildMessage()
	fmt.Printf("bug4: message = %q\n", msg)

	// Bug 5
	r, err := bug5_safeDiv(10, 0)
	fmt.Printf("bug5: 10/0 = %d, err = %v\n", r, err)

	// Bug 6
	m := map[string]int{"alpha": 1, "beta": 2}
	bug6_lookup(m, "alpha")

	// Bug 7
	steps := bug7_collatz(6)
	fmt.Printf("bug7: collatz(6) = %d steps\n", steps)

	fmt.Println()
	fmt.Println("=== All bugs fixed! ===")
}
```

## Expected Output

When all bugs are fixed, the program should produce:

```
=== Control Flow Debugging Challenge ===

bug1: steps = 3
bug2: first negative = -2
bug3: doubled = [2 4 6 8 10]
bug4: message = "hello world !"
bug5: 10/0 = 0, err = recovered: runtime error: integer divide by zero
bug6: found alpha = 1
bug6: final value = 1
bug7: collatz(6) = 8 steps

=== All bugs fixed! ===
```

## Hints

<details>
<summary>Hint 1: Bug 1 -- defer argument evaluation</summary>

Defer evaluates its arguments when the `defer` statement is executed, not when the deferred function runs. At the time `defer` is called, what is the value of `steps`?

Consider using a closure instead so that `steps` is read at execution time.
</details>

<details>
<summary>Hint 2: Bug 2 -- break inside switch</summary>

An unlabeled `break` inside a `switch` exits the `switch`, not the enclosing `for` loop. The function always falls through to `return 0`.

You need either a labeled break or a direct return.
</details>

<details>
<summary>Hint 3: Bug 3 -- range value copy</summary>

The range variable `v` is a copy of the slice element. Modifying `v` does not modify the original slice.

Use the index to modify the slice in place.
</details>

<details>
<summary>Hint 4: Bug 4 -- defer ordering and return</summary>

Two issues here. First, defers run after the return value is determined. The `return strings.Join(...)` captures `parts` before any defer runs. Second, defers execute in LIFO order.

Use a named return value so the deferred closures can modify the result, and check the ordering.
</details>

<details>
<summary>Hint 5: Bug 5 -- recover placement</summary>

`recover` only works inside a deferred function in the same goroutine. Putting it in a separate goroutine does not catch the panic.

Use `defer func() { ... recover() ... }()` directly in `bug5_safeDiv`.
</details>

<details>
<summary>Hint 6: Bug 6 -- init statement scope</summary>

The variable `v` declared in the `if` init statement is scoped to the `if`/`else` block. It is not accessible outside.

Declare `v` before the `if`, or restructure the code.
</details>

<details>
<summary>Hint 7: Bug 7 -- loop condition</summary>

`for n == 1` means "loop while n equals 1." The Collatz conjecture loop should run while n is NOT 1.

Change the condition to `n != 1`.
</details>

## Success Criteria

1. The program compiles with no errors
2. `go vet ./...` reports no issues
3. The output matches the expected output exactly
4. You can explain each bug: what was wrong, why it was wrong, and what Go semantic caused it

Test with:

```bash
cd ~/go-exercises/cf-debug && go vet ./... && go run main.go
```

## Research Resources

- [Go Specification: Defer Statements](https://go.dev/ref/spec#Defer_statements)
- [Go Specification: Break Statements](https://go.dev/ref/spec#Break_statements)
- [Go Specification: For Range](https://go.dev/ref/spec#For_range)
- [Go Specification: Handling Panics](https://go.dev/ref/spec#Handling_panics)
- [Go Specification: If Statements](https://go.dev/ref/spec#If_statements)
- [Go Blog: Defer, Panic, and Recover](https://go.dev/blog/defer-panic-and-recover)

## What's Next

You have completed Section 03 on Control Flow. Continue to [Section 04 - Functions](../../04-functions/01-function-declaration-and-signatures/01-function-declaration-and-signatures.md) to learn about function declaration, multiple returns, variadic parameters, and closures.

## Summary

This challenge tested your understanding of seven control flow semantics:

1. **Defer argument evaluation** -- arguments are evaluated at the `defer` statement, not at execution time
2. **Break inside switch** -- unlabeled `break` exits the `switch`, not an enclosing `for` loop
3. **Range value copy** -- the value variable is a copy; use the index to modify slice elements
4. **Defer ordering and returns** -- defers run after the return expression is evaluated; use named returns for modification
5. **Recover placement** -- `recover` only works in a deferred function in the same goroutine
6. **Init statement scope** -- variables in `if` init statements are scoped to the `if`/`else` block
7. **Loop condition logic** -- `for condition` means "while condition is true"
