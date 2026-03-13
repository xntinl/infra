# 1. Package Declaration and Imports

<!--
difficulty: basic
concepts: [package-keyword, import-paths, import-aliases, grouped-imports]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [go-modules, func-main, standard-library]
-->

## Prerequisites

- Go 1.22+ installed
- A working Go module (familiarity with `go mod init`)
- Understanding of `func main` and basic Go syntax

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the role of the `package` declaration at the top of every Go file
- **Use** import paths to bring in standard library and third-party packages
- **Apply** import aliases to resolve naming conflicts

## Why Package Declaration and Imports

Every Go file starts with a `package` declaration. This is not optional -- it tells the compiler which package the file belongs to. Files in the same directory must use the same package name.

Import paths are how you reference other packages. Standard library imports are short (`"fmt"`, `"os"`). Third-party imports use full module paths (`"github.com/user/repo/pkg"`). When two packages share the same name, you use an alias to disambiguate.

Understanding this system is fundamental because Go enforces it strictly: unused imports are compile errors, and the package name determines what is accessible to other code.

## Step 1 -- Multiple Files in One Package

```bash
mkdir -p ~/go-exercises/packages-imports
cd ~/go-exercises/packages-imports
go mod init packages-imports
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("Message:", greet("Go developer"))
	fmt.Println("Version:", version())
}
```

Create `greet.go` in the same directory:

```go
package main

func greet(name string) string {
	return "Hello, " + name + "!"
}
```

Create `version.go` in the same directory:

```go
package main

func version() string {
	return "1.0.0"
}
```

All three files declare `package main`. They can call each other's functions without importing -- they are the same package.

### Intermediate Verification

```bash
go run .
```

Expected:

```
Message: Hello, Go developer!
Version: 1.0.0
```

Note: `go run .` compiles all `.go` files in the current directory.

## Step 2 -- Import Standard Library Packages

Modify `main.go` to use multiple standard library packages:

```go
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

func main() {
	fmt.Println("Message:", greet("Go developer"))
	fmt.Println("Version:", version())

	// os package: environment variables
	home, _ := os.UserHomeDir()
	fmt.Println("Home:", home)

	// runtime package: Go version
	fmt.Println("Go:", runtime.Version())

	// strings package: string manipulation
	words := []string{"packages", "are", "fundamental"}
	fmt.Println("Joined:", strings.Join(words, " "))
}
```

### Intermediate Verification

```bash
go run .
```

Expected (your home directory and Go version will differ):

```
Message: Hello, Go developer!
Version: 1.0.0
Home: /Users/yourname
Go: go1.22.0
Joined: packages are fundamental
```

## Step 3 -- Import Aliases

Sometimes two packages have the same base name. Use aliases to disambiguate:

```go
package main

import (
	"fmt"
	"math/rand"
	crand "crypto/rand"
	"encoding/binary"
)

func main() {
	// math/rand: pseudo-random
	fmt.Println("Pseudo-random:", rand.Intn(100))

	// crypto/rand: cryptographic random (aliased as crand)
	var n int64
	binary.Read(crand.Reader, binary.BigEndian, &n)
	if n < 0 {
		n = -n
	}
	fmt.Println("Crypto-random:", n%100)
}
```

The alias `crand` lets you use both `rand` (math) and `crand` (crypto) in the same file.

### Intermediate Verification

```bash
go run main.go
```

Expected (numbers will vary):

```
Pseudo-random: 42
Crypto-random: 73
```

## Step 4 -- Blank Import for Side Effects

Some packages need to be imported only for their `init()` function (side effects). Use the blank identifier `_`:

```go
import (
	_ "image/png" // registers PNG decoder
)
```

You will see this pattern in database drivers, image format decoders, and tracing libraries. The `_` tells Go "I know I am not calling anything from this package directly."

### Intermediate Verification

This is a concept -- no code to run. Recognize the `_` pattern when you see it in real codebases.

## Common Mistakes

### Mixed Package Names in the Same Directory

**Wrong** -- two files in the same directory:

```go
// file1.go
package main

// file2.go
package utils  // ERROR: different package name
```

**What happens:** Compilation fails. All files in a directory must declare the same package.

**Fix:** Use the same package name, or move the file to a different directory.

### Unused Imports

**Wrong:**

```go
import (
	"fmt"
	"os"  // not used
)
```

**What happens:** Compilation fails. Go does not allow unused imports.

**Fix:** Remove `"os"` or use it. Your editor's Go plugin typically handles this automatically.

### Using the Wrong Import Path

**Wrong:**

```go
import "strings/Builder"  // not a valid import path
```

**Fix:** Import the package, not a type: `import "strings"`. Then use `strings.Builder`.

## Verify What You Learned

Run the multi-file program:

```bash
cd ~/go-exercises/packages-imports
go run .
```

Confirm that functions from different files in the same package are accessible without imports.

## What's Next

Continue to [02 - Exported vs Unexported](../02-exported-vs-unexported/02-exported-vs-unexported.md) to learn how Go controls visibility with capitalization.

## Summary

- Every Go file starts with `package <name>` -- all files in a directory share the same package name
- Imports use the full path: `"fmt"`, `"math/rand"`, `"github.com/user/repo"`
- Group imports in parentheses with blank lines separating standard library from third-party
- Use aliases (`alias "path"`) to resolve naming conflicts
- Use blank imports (`_ "path"`) for side-effect-only imports
- Unused imports are compile errors

## Reference

- [How to Write Go Code: Package paths](https://go.dev/doc/code#PackagePaths)
- [Effective Go: Package names](https://go.dev/doc/effective_go#package-names)
- [Go specification: Import declarations](https://go.dev/ref/spec#Import_declarations)
