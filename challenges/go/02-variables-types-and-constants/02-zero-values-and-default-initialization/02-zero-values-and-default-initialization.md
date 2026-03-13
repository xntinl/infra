# 2. Zero Values and Default Initialization

<!--
difficulty: basic
concepts: [zero-values, default-initialization, nil, boolean-zero, numeric-zero, string-zero]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [01-variable-declaration-and-short-assignment]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the zero value for each basic Go type
- **Explain** why Go uses zero values instead of undefined/uninitialized memory
- **Predict** the default value of any variable declared without an initializer

## Why Zero Values

In C, an uninitialized variable contains whatever garbage was in memory. In JavaScript, uninitialized variables are `undefined`. Go takes a different approach: every variable is automatically initialized to its type's zero value. There is no such thing as an uninitialized variable in Go.

This design eliminates an entire class of bugs. You never read garbage memory, and you never need to check if a variable was initialized. A freshly declared `int` is `0`, a `string` is `""`, a pointer is `nil`, and a `bool` is `false`. These are not arbitrary defaults -- they are chosen so that zero values are useful. An empty string, a zero counter, and a false flag are all valid starting states.

Understanding zero values is essential because idiomatic Go code relies on them heavily. Many types are designed so that their zero value is immediately usable without explicit construction.

## Step 1 -- Explore Zero Values for Basic Types

```bash
mkdir -p ~/go-exercises/zero-values
cd ~/go-exercises/zero-values
go mod init zero-values
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	var i int
	var f float64
	var b bool
	var s string
	var by byte
	var r rune

	fmt.Printf("int:     %d (repr: %v)\n", i, i)
	fmt.Printf("float64: %f (repr: %v)\n", f, f)
	fmt.Printf("bool:    %t (repr: %v)\n", b, b)
	fmt.Printf("string:  %q (repr: %v)\n", s, s)
	fmt.Printf("byte:    %d (repr: %v)\n", by, by)
	fmt.Printf("rune:    %d (repr: %v)\n", r, r)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/zero-values && go run main.go
```

Expected:

```
int:     0 (repr: 0)
float64: 0.000000 (repr: 0)
bool:    false (repr: false)
string:  "" (repr: )
byte:    0 (repr: 0)
rune:    0 (repr: 0)
```

## Step 2 -- Zero Values for Composite Types

Add more complex types to see their zero values:

```go
package main

import "fmt"

func main() {
	var p *int             // pointer
	var sl []int           // slice
	var m map[string]int   // map
	var ch chan int         // channel
	var fn func()          // function
	var iface interface{}  // interface

	fmt.Printf("pointer:   %v\n", p)
	fmt.Printf("slice:     %v (nil: %t, len: %d)\n", sl, sl == nil, len(sl))
	fmt.Printf("map:       %v (nil: %t)\n", m, m == nil)
	fmt.Printf("channel:   %v (nil: %t)\n", ch, ch == nil)
	fmt.Printf("function:  %v (nil: %t)\n", fn, fn == nil)
	fmt.Printf("interface: %v (nil: %t)\n", iface, iface == nil)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/zero-values && go run main.go
```

Expected:

```
pointer:   <nil>
slice:     [] (nil: true, len: 0)
map:       map[] (nil: true)
channel:   <nil> (nil: true)
function:  <nil> (nil: true)
interface: <nil> (nil: true)
```

## Step 3 -- Zero Values for Structs

Structs get zero values recursively -- each field is set to its type's zero value:

```go
package main

import "fmt"

type User struct {
	Name   string
	Age    int
	Active bool
	Score  float64
}

func main() {
	var u User

	fmt.Printf("User: %+v\n", u)
	fmt.Printf("Name is empty: %t\n", u.Name == "")
	fmt.Printf("Age is zero: %t\n", u.Age == 0)
	fmt.Printf("Active is false: %t\n", u.Active == false)
	fmt.Printf("Score is zero: %t\n", u.Score == 0.0)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/zero-values && go run main.go
```

Expected:

```
User: {Name: Age:0 Active:false Score:0}
Name is empty: true
Age is zero: true
Active is false: true
Score is zero: true
```

## Step 4 -- Zero Values for Arrays

Arrays, unlike slices, are value types. Their zero value is an array with every element set to its type's zero value:

```go
package main

import "fmt"

func main() {
	var nums [5]int
	var flags [3]bool
	var names [2]string

	fmt.Printf("nums:  %v\n", nums)
	fmt.Printf("flags: %v\n", flags)
	fmt.Printf("names: %q\n", names)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/zero-values && go run main.go
```

Expected:

```
nums:  [0 0 0 0 0]
flags: [false false false]
names: ["" ""]
```

## Step 5 -- Useful Zero Values in Practice

Go types are designed so that zero values are immediately usable. This is a core Go idiom:

```go
package main

import (
	"bytes"
	"fmt"
	"sync"
)

func main() {
	// bytes.Buffer is usable at zero value -- no constructor needed
	var buf bytes.Buffer
	buf.WriteString("Hello, ")
	buf.WriteString("World!")
	fmt.Println(buf.String())

	// sync.Mutex is usable at zero value
	var mu sync.Mutex
	mu.Lock()
	fmt.Println("Lock acquired")
	mu.Unlock()

	// A nil slice can be appended to
	var items []string
	items = append(items, "first")
	items = append(items, "second")
	fmt.Println(items)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/zero-values && go run main.go
```

Expected:

```
Hello, World!
Lock acquired
[first second]
```

## Common Mistakes

### Writing to a Nil Map

**Wrong:**

```go
var m map[string]int
m["key"] = 42 // panic: assignment to entry in nil map
```

**What happens:** A nil map can be read from (returns the zero value) but writing panics. This is one of the most common Go runtime errors.

**Fix:** Initialize the map before writing:

```go
m := make(map[string]int)
m["key"] = 42
```

### Confusing Nil Slice and Empty Slice

**Wrong:** Treating `nil` slices differently from empty slices when it does not matter.

**What happens:** `var s []int` (nil) and `s := []int{}` (empty, non-nil) behave identically for `len`, `cap`, `append`, and `range`. The only difference is `s == nil`.

**Fix:** Use `var s []int` (nil) as the default. Only use `[]int{}` when you need a non-nil empty slice (e.g., for JSON marshaling where you want `[]` instead of `null`).

### Assuming Zero Value Means "Not Set"

**Wrong:** Using `if user.Age == 0` to check if age was provided.

**What happens:** You cannot distinguish between "age was set to 0" and "age was never set." Zero is a valid age for a counter, rating, or score.

**Fix:** Use a pointer `*int` where nil means "not set," or use a boolean flag like `AgeSet bool`.

## Verify What You Learned

Create a program that declares variables of each type category and prints their zero values:

```bash
cd ~/go-exercises/zero-values && go run main.go
```

Confirm that all variables print their expected zero values.

## What's Next

Continue to [03 - Basic Types](../03-basic-types/03-basic-types.md) to explore Go's built-in types in depth.

## Summary

- Every Go variable is initialized to its zero value -- there is no uninitialized memory
- Numeric types: `0`, booleans: `false`, strings: `""`, pointers/slices/maps/channels/functions/interfaces: `nil`
- Struct zero values are recursive: each field gets its type's zero value
- Many standard library types are designed to be usable at their zero value
- A nil slice can be appended to, but a nil map panics on write
- Zero values eliminate uninitialized variable bugs common in other languages

## Reference

- [Go Specification: The Zero Value](https://go.dev/ref/spec#The_zero_value)
- [Effective Go: Allocation with new](https://go.dev/doc/effective_go#allocation_new)
- [Go Blog: Maps in Action](https://go.dev/blog/maps)
