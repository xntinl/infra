# 6. Labels, Break, Continue, and Goto

<!--
difficulty: intermediate
concepts: [labels, labeled-break, labeled-continue, goto, nested-loops, loop-control]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [02-for-loops, 05-range-over-collections]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [02 - For Loops](../02-for-loops/02-for-loops.md) and [05 - Range Over Collections](../05-range-over-collections/05-range-over-collections.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** labeled `break` and `continue` to control nested loops
- **Distinguish** between unlabeled and labeled loop control
- **Explain** why `goto` exists in Go and when its use is appropriate
- **Recognize** the scope rules that restrict `goto` targets

## Why Labels, Break, Continue, and Goto

An unlabeled `break` only exits the innermost loop. When you have nested loops and need to break out of the outer loop from inside the inner one, you need a label. The same applies to `continue` -- without a label it advances the innermost loop, but a labeled `continue` can advance an outer loop.

Go also includes `goto`, which many languages discourage. Go restricts `goto` with scope rules that prevent jumping over variable declarations, making it safer than in C. The primary legitimate use is simplifying error-handling cleanup in functions with multiple resource acquisitions.

## Step 1 -- Unlabeled vs Labeled Break

```bash
mkdir -p ~/go-exercises/labels
cd ~/go-exercises/labels
go mod init labels
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Unlabeled break: only exits the inner loop
	fmt.Println("Unlabeled break:")
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if j == 1 {
				break // exits inner loop only
			}
			fmt.Printf("  i=%d j=%d\n", i, j)
		}
	}

	// Labeled break: exits the outer loop
	fmt.Println("Labeled break:")
Outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i == 1 && j == 1 {
				break Outer // exits both loops
			}
			fmt.Printf("  i=%d j=%d\n", i, j)
		}
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/labels && go run main.go
```

Expected:

```
Unlabeled break:
  i=0 j=0
  i=1 j=0
  i=2 j=0
Labeled break:
  i=0 j=0
  i=0 j=1
  i=0 j=2
  i=1 j=0
```

The unlabeled `break` runs all three outer iterations (only skipping `j >= 1`). The labeled `break` stops everything at `i=1, j=1`.

## Step 2 -- Labeled Continue

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Labeled continue: skip to the next iteration of the outer loop
	fmt.Println("Labeled continue -- skip outer iteration when inner finds a match:")
Rows:
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			if row == col {
				fmt.Printf("  row %d: skipped (diagonal hit at col %d)\n", row, col)
				continue Rows // skip remaining columns for this row
			}
		}
		fmt.Printf("  row %d: processed all columns\n", row)
	}

	// Practical example: search a 2D grid
	fmt.Println("\nSearch for target in 2D grid:")
	grid := [][]int{
		{1, 2, 3},
		{4, 5, 6},
		{7, 8, 9},
	}
	target := 5
	found := false
Search:
	for r, row := range grid {
		for c, val := range row {
			if val == target {
				fmt.Printf("  Found %d at [%d][%d]\n", target, r, c)
				found = true
				break Search
			}
		}
	}
	if !found {
		fmt.Printf("  %d not found\n", target)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/labels && go run main.go
```

Expected:

```
Labeled continue -- skip outer iteration when inner finds a match:
  row 0: skipped (diagonal hit at col 0)
  row 1: skipped (diagonal hit at col 1)
  row 2: skipped (diagonal hit at col 2)
  row 3: skipped (diagonal hit at col 3)

Search for target in 2D grid:
  Found 5 at [1][1]
```

## Step 3 -- Labels with Switch and Select

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// break inside switch inside for: unlabeled break exits the switch, not the loop
	fmt.Println("Break in switch (unlabeled):")
	for i := 0; i < 5; i++ {
		switch i {
		case 3:
			fmt.Println("  hit case 3, breaking switch only")
			break // exits switch, loop continues
		}
		fmt.Printf("  after switch: i=%d\n", i)
	}

	// Labeled break exits the loop from inside a switch
	fmt.Println("\nBreak in switch (labeled):")
Loop:
	for i := 0; i < 5; i++ {
		switch i {
		case 3:
			fmt.Println("  hit case 3, breaking loop")
			break Loop // exits the for loop
		}
		fmt.Printf("  after switch: i=%d\n", i)
	}
	fmt.Println("  loop exited")
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/labels && go run main.go
```

Expected:

```
Break in switch (unlabeled):
  after switch: i=0
  after switch: i=1
  after switch: i=2
  hit case 3, breaking switch only
  after switch: i=3
  after switch: i=4

Break in switch (labeled):
  after switch: i=0
  after switch: i=1
  after switch: i=2
  hit case 3, breaking loop
  loop exited
```

This is a critical distinction. Without the label, `break` inside a `switch` within a `for` only breaks out of the `switch`.

## Step 4 -- Goto

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// goto for cleanup pattern (simplified example)
	fmt.Println("Goto cleanup pattern:")
	if err := step1(); err != nil {
		fmt.Println("  step1 succeeded")
	}

	// goto with scope restrictions
	fmt.Println("\nGoto scope demo:")
	i := 0
top:
	if i >= 3 {
		goto done
	}
	fmt.Printf("  i=%d\n", i)
	i++
	goto top
done:
	fmt.Println("  done")

	// What you CANNOT do with goto:
	// goto skip
	// x := 42     // compile error: goto jumps over declaration of x
	// skip:
	// fmt.Println(x)
}

func step1() error {
	// Simulating a multi-step process with cleanup
	r1, err := acquire("database")
	if err != nil {
		goto cleanup
	}
	fmt.Printf("  acquired: %s\n", r1)

	_, err = acquire("fail-cache")
	if err != nil {
		goto cleanupR1
	}

	return nil

cleanupR1:
	fmt.Printf("  releasing: %s\n", r1)
cleanup:
	fmt.Println("  cleanup complete")
	return err
}

func acquire(name string) (string, error) {
	if name == "fail-cache" {
		return "", fmt.Errorf("cannot acquire %s", name)
	}
	return name, nil
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/labels && go run main.go
```

Expected:

```
Goto cleanup pattern:
  acquired: database
  releasing: database
  cleanup complete
  step1 succeeded

Goto scope demo:
  i=0
  i=1
  i=2
  done
```

Note: `goto` is rare in Go. The `defer` statement (next exercise) is almost always preferred for cleanup. The `goto` cleanup pattern shown here is mainly found in low-level code like the Go standard library itself.

## Common Mistakes

### Forgetting That Break in Switch Does Not Exit a Loop

**Wrong assumption:**

```go
for i := 0; i < 10; i++ {
    switch i {
    case 5:
        break // developer thinks this exits the for loop
    }
}
```

**What happens:** `break` exits the `switch`, not the `for` loop. The loop runs all 10 iterations.

**Fix:** Use a labeled `break` to exit the loop, or restructure the code.

### Trying to Goto Over a Variable Declaration

**Wrong:**

```go
goto end
x := 42 // compile error: goto jumps over declaration
end:
fmt.Println(x)
```

**What happens:** Go does not allow `goto` to jump over variable declarations in the same scope.

**Fix:** Declare the variable before the `goto`, or restructure to avoid `goto`.

### Using Goto When Labeled Break Suffices

**Wrong:**

```go
for i := range items {
    for j := range items[i] {
        if condition {
            goto done
        }
    }
}
done:
```

**Fix:** Use `break Outer` with a label. Reserve `goto` for cleanup patterns that cannot be expressed with loops.

## Verify What You Learned

```bash
cd ~/go-exercises/labels && go run main.go
```

Write a function that searches a 3D slice (slice of slice of slice) for a target value and uses a labeled `break` to exit all three loops when found.

## What's Next

Continue to [07 - Defer Semantics and Ordering](../07-defer-semantics-and-ordering/07-defer-semantics-and-ordering.md) to learn how `defer` provides a cleaner approach to resource cleanup.

## Summary

- Labels are identifiers followed by a colon, placed before `for`, `switch`, or `select`
- `break Label` exits the labeled statement; `continue Label` advances the labeled loop
- Inside a `switch` within a `for`, unlabeled `break` exits the `switch` -- use a label to exit the loop
- `goto` can jump to labels in the same function, but cannot jump over variable declarations
- `goto` is rare in Go; prefer `defer` for cleanup and labeled `break`/`continue` for loop control
- The Go compiler enforces scope rules that make `goto` safer than in C

## Reference

- [Go Specification: Labeled Statements](https://go.dev/ref/spec#Labeled_statements)
- [Go Specification: Break](https://go.dev/ref/spec#Break_statements)
- [Go Specification: Continue](https://go.dev/ref/spec#Continue_statements)
- [Go Specification: Goto](https://go.dev/ref/spec#Goto_statements)
