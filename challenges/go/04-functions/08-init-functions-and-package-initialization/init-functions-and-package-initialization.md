# 8. Init Functions and Package Initialization

<!--
difficulty: intermediate
concepts: [init-function, package-initialization, import-side-effects, execution-order]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [function-declaration, packages-basics]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Apply** `init()` functions for package-level setup
- **Analyze** the execution order of `init()` across packages and files
- **Identify** the blank import pattern for side-effect-only imports

## Why Init Functions

Go provides a special function called `init()` that runs automatically when a package is loaded. It requires no arguments, returns nothing, and cannot be called directly. Every package can have multiple `init()` functions — even multiple per file — and they all run before `main()` starts.

The `init()` function is used for setup that must happen before any other code in the package runs: registering drivers, validating configuration, initializing lookup tables, or computing values that are expensive to create at compile time. The standard library uses `init()` extensively — database drivers register themselves in `init()`, image decoders register their formats, and encoding packages set up codec tables.

Understanding `init()` is important because it runs implicitly. If you do not know when and how `init()` fires, import order bugs and surprising startup behavior will confuse you.

## Step 1 — A Basic Init Function

Create a project with the following `main.go`:

```go
package main

import "fmt"

var config string

func init() {
    config = "production"
    fmt.Println("init: config set to", config)
}

func main() {
    fmt.Println("main: config is", config)
}
```

The `init()` function runs after all package-level variable declarations are evaluated but before `main()`.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
init: config set to production
main: config is production
```

## Step 2 — Multiple Init Functions

A single file can contain multiple `init()` functions, and they run in the order they appear:

```go
package main

import "fmt"

func init() {
    fmt.Println("first init")
}

func init() {
    fmt.Println("second init")
}

func init() {
    fmt.Println("third init")
}

func main() {
    fmt.Println("main")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
first init
second init
third init
main
```

## Step 3 — Init Order Across Packages

When package A imports package B, B's `init()` runs before A's `init()`. Create a multi-file example:

Create the directory structure:

```
initdemo/
├── go.mod
├── main.go
└── greeter/
    └── greeter.go
```

`go.mod`:
```
module initdemo

go 1.22
```

`greeter/greeter.go`:
```go
package greeter

import "fmt"

var DefaultGreeting string

func init() {
    DefaultGreeting = "Hello"
    fmt.Println("greeter init: DefaultGreeting set")
}

func Greet(name string) string {
    return DefaultGreeting + ", " + name + "!"
}
```

`main.go`:
```go
package main

import (
    "fmt"
    "initdemo/greeter"
)

func init() {
    fmt.Println("main init")
}

func main() {
    fmt.Println(greeter.Greet("Gopher"))
}
```

The execution order is:
1. `greeter` package variables are initialized
2. `greeter.init()` runs
3. `main` package variables are initialized
4. `main.init()` runs
5. `main()` runs

### Intermediate Verification

```bash
cd initdemo && go run .
```

Expected output:

```
greeter init: DefaultGreeting set
main init
Hello, Gopher!
```

## Step 4 — Blank Imports for Side Effects

Sometimes you import a package only for its `init()` side effects. Use the blank identifier `_`:

```go
package main

import (
    "database/sql"
    "fmt"

    _ "github.com/lib/pq" // registers the postgres driver via init()
)

func main() {
    drivers := sql.Drivers()
    fmt.Println("Registered drivers:", drivers)
}
```

The `_ "github.com/lib/pq"` import causes the package's `init()` to run, which calls `sql.Register("postgres", &Driver{})`. Without this import, the driver would not be available.

Common blank imports in the standard library:

```go
import (
    _ "image/png"  // registers PNG decoder
    _ "image/jpeg" // registers JPEG decoder
    _ "net/http/pprof" // registers pprof HTTP handlers
)
```

### Intermediate Verification

The blank import pattern compiles only if the imported package exists. The concept is what matters here — the driver registration happens invisibly through `init()`.

## Step 5 — Package-Level Variable Initialization Order

Package-level variables are initialized before `init()`, in declaration order (with dependency resolution):

```go
package main

import "fmt"

var a = compute("a")
var b = compute("b")
var c = compute("c")

func compute(name string) string {
    fmt.Printf("initializing %s\n", name)
    return name
}

func init() {
    fmt.Println("init runs after all vars")
}

func main() {
    fmt.Printf("main: a=%s b=%s c=%s\n", a, b, c)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
initializing a
initializing b
initializing c
init runs after all vars
main: a=a b=b c=c
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Trying to call `init()` directly | `init()` cannot be called or referenced |
| Relying on `init()` order across files | Order within a package across files is by filename, but this is an implementation detail — do not depend on it |
| Putting complex logic in `init()` | Hard to test, hard to debug, and runs even when you only need one function from the package |
| Forgetting that `init()` panics crash the entire program | There is no way to recover from an `init()` panic at the caller level |
| Using `init()` for work that could be lazy-loaded | Prefer `sync.Once` for expensive setup that may not always be needed |

## Verify What You Learned

1. Create a package with an `init()` that populates a `map[string]int` of timezone offsets. Import it from `main` and use the map.
2. Add two `init()` functions to a single file. Verify they run in source order.
3. Explain why database drivers use `init()` for registration instead of requiring the user to call a `Register()` function manually.

## What's Next

Next you will explore **closure gotchas — loop variable capture**, a common trap when closures capture loop variables.

## Summary

- `init()` functions run automatically when a package is loaded, before `main()`
- A file can have multiple `init()` functions; they run in source order
- Imported packages' `init()` runs before the importing package's `init()`
- Blank imports (`_ "pkg"`) trigger `init()` for side effects only
- Package-level variables are initialized before `init()` runs
- Avoid putting complex or fallible logic in `init()`; prefer explicit initialization

## Reference

- [Go spec: Package initialization](https://go.dev/ref/spec#Package_initialization)
- [Effective Go: init](https://go.dev/doc/effective_go#init)
