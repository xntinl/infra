# 2. Named Return Values

<!--
difficulty: basic
concepts: [named-returns, naked-return, documentation-via-naming]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [function-declaration]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Explain** what named return values are and how they work
- **Identify** when naked returns are appropriate and when they hurt readability
- **Recall** the zero-value initialization of named returns

## Why Named Return Values

Go lets you give names to the return values in a function signature. These named returns serve as documentation — they tell the caller what each returned value represents. They also create local variables initialized to their zero values, which you can assign during the function body and optionally return with a naked `return` statement.

Named returns are most useful in short functions and in `godoc` output where the parameter names explain the meaning of each returned value. However, naked returns can reduce clarity in longer functions. The Go community has a pragmatic rule: use named returns for documentation, but be cautious with naked returns in functions longer than a few lines.

## Step 1 — Basic Named Returns

```go
package main

import "fmt"

func rectangleDimensions(area, width float64) (length float64, err error) {
    if width == 0 {
        err = fmt.Errorf("width cannot be zero")
        return
    }
    length = area / width
    return
}

func main() {
    l, err := rectangleDimensions(50, 10)
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Printf("Length: %.1f\n", l)
}
```

Notice three things:
- `length` and `err` are declared in the signature
- They start at their zero values (`0.0` and `nil`)
- The bare `return` returns whatever `length` and `err` currently hold

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Length: 5.0
```

## Step 2 — Zero-Value Initialization

Named return values are initialized to their type's zero value. This means you only need to set the values that differ from zero:

```go
package main

import (
    "fmt"
    "strings"
)

func parseTag(tag string) (key, value string, found bool) {
    idx := strings.Index(tag, ":")
    if idx == -1 {
        return // key="", value="", found=false
    }
    key = tag[:idx]
    value = tag[idx+1:]
    found = true
    return
}

func main() {
    k, v, ok := parseTag("env:production")
    fmt.Printf("key=%q value=%q found=%v\n", k, v, ok)

    k, v, ok = parseTag("novalue")
    fmt.Printf("key=%q value=%q found=%v\n", k, v, ok)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
key="env" value="production" found=true
key="" value="" found=false
```

## Step 3 — Named Returns for Documentation

Even if you do not use naked returns, named return values improve documentation. Compare these two signatures:

```go
// Unclear: what does each string mean?
func splitName(full string) (string, string, error)

// Clear: first name, last name, and an error
func splitName(full string) (first, last string, err error)
```

You can use named returns purely for documentation while still using explicit returns:

```go
package main

import (
    "fmt"
    "strings"
)

func splitName(full string) (first, last string, err error) {
    parts := strings.Fields(full)
    if len(parts) < 2 {
        return "", "", fmt.Errorf("expected 'first last', got %q", full)
    }
    return parts[0], parts[1], nil
}

func main() {
    f, l, err := splitName("Rob Pike")
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Printf("First: %s, Last: %s\n", f, l)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
First: Rob, Last: Pike
```

## Step 4 — When to Avoid Naked Returns

Naked returns hurt readability in longer functions because the reader must scan the entire body to find where each named variable was last assigned:

```go
// BAD: naked return in a long function — hard to follow
func processOrder(id int) (total float64, status string, err error) {
    // ... 30 lines of logic ...
    // Where was 'total' last assigned? You have to search.
    return
}

// GOOD: explicit return — immediately clear what is being returned
func processOrder(id int) (total float64, status string, err error) {
    // ... 30 lines of logic ...
    return calculatedTotal, "completed", nil
}
```

**Rule of thumb**: Use naked returns only in functions shorter than ~10 lines.

### Intermediate Verification

Create a file with a short function using named returns and a longer one with explicit returns. Both should compile and run correctly.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Shadowing named returns with `:=` | A new local variable hides the named return; the naked `return` returns the zero value instead |
| Using naked returns in long functions | Compiles fine but confuses readers and reviewers |
| Mixing naked and explicit returns | Legal but inconsistent — pick one style per function |
| Forgetting that named returns are zero-initialized | Can lead to returning `0` or `""` unintentionally |

## Verify What You Learned

1. Write a function `coordinate(input string) (x, y int, err error)` that parses a string like `"3,7"` into two integers. Use named returns for documentation but explicit `return` statements.
2. Write a short function `clamp(val, lo, hi int) (result int)` that uses a naked return. The function should be short enough that the naked return is readable.

## What's Next

Next you will learn about **variadic functions**, which accept a variable number of arguments using the `...` syntax.

## Summary

- Named return values are declared in the function signature: `func f() (name Type)`
- They are initialized to zero values and can be returned with a naked `return`
- Named returns improve `godoc` documentation by labeling what each value means
- Use naked returns only in short functions to keep code readable
- Be careful not to shadow named returns with `:=` inside the function body

## Reference

- [Go spec: Return statements](https://go.dev/ref/spec#Return_statements)
- [Effective Go: Named result parameters](https://go.dev/doc/effective_go#named-results)
- [Go Code Review Comments: Named Result Parameters](https://go.dev/wiki/CodeReviewComments#named-result-parameters)
