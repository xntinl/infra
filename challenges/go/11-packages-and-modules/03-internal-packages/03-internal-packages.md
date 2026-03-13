# 3. Internal Packages

<!--
difficulty: intermediate
concepts: [internal-directory, import-restrictions, package-visibility, api-boundaries]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [package-declaration, exported-vs-unexported, go-modules]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - Exported vs Unexported](../02-exported-vs-unexported/02-exported-vs-unexported.md)
- Understanding of Go module structure

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the `internal/` directory convention to restrict package imports
- **Explain** which packages can and cannot import from an `internal/` directory
- **Design** module layouts that use `internal/` to hide implementation details

## Why Internal Packages

The exported/unexported naming convention controls visibility within a package. But what about visibility between packages? If you export a type in `pkg/auth/token.go`, any package in any module can import it. Sometimes you want to share code between your own packages without making it available to external consumers.

Go solves this with the `internal/` directory. A package inside an `internal/` directory can only be imported by code that is rooted at the parent of `internal/`. This is enforced by the Go toolchain -- not by convention, but by the compiler.

For example:
- `myapp/internal/cache` can be imported by `myapp/server` or `myapp/cli`
- `myapp/internal/cache` cannot be imported by `other-module/anything`
- `myapp/pkg/auth/internal/hash` can be imported by `myapp/pkg/auth` but not by `myapp/server`

## Step 1 -- Create a Module with Internal Packages

```bash
mkdir -p ~/go-exercises/internal-pkgs
cd ~/go-exercises/internal-pkgs
go mod init internal-pkgs
mkdir -p internal/hash
mkdir -p api
```

Create `internal/hash/hash.go`:

```go
package hash

import (
	"crypto/sha256"
	"fmt"
)

// Sum returns the SHA-256 hex digest of the input.
func Sum(data string) string {
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}
```

This package is inside `internal/`, so only packages within `internal-pkgs` can import it.

### Intermediate Verification

```bash
go build ./internal/hash
```

No output means success.

## Step 2 -- Use the Internal Package from Within the Module

Create `api/api.go`:

```go
package api

import "internal-pkgs/internal/hash"

// TokenFor generates an API token for the given user.
func TokenFor(userID string) string {
	return hash.Sum("token:" + userID)
}
```

Create `main.go`:

```go
package main

import (
	"fmt"

	"internal-pkgs/api"
)

func main() {
	token := api.TokenFor("user-42")
	fmt.Println("Token:", token)
}
```

### Intermediate Verification

```bash
go run .
```

Expected:

```
Token: <sha256 hex string>
```

The `api` package imports from `internal/hash` successfully because both are within `internal-pkgs`.

## Step 3 -- Verify External Import Restriction

To confirm that external modules cannot import your internal packages, create a separate module. Write this yourself:

```bash
mkdir -p /tmp/external-test
cd /tmp/external-test
go mod init external-test
```

Create `/tmp/external-test/main.go`:

```go
package main

import "internal-pkgs/internal/hash" // This should fail

func main() {
	println(hash.Sum("test"))
}
```

Try to build it:

```bash
cd /tmp/external-test
go build .
```

You will see an error like:

```
use of internal package internal-pkgs/internal/hash not allowed
```

This proves the `internal/` restriction works.

### Intermediate Verification

```bash
cd /tmp/external-test && go build . 2>&1
```

Expected: error about internal package use.

Clean up:

```bash
rm -rf /tmp/external-test
```

## Step 4 -- Nested Internal Directories

The `internal/` restriction is scoped to its parent. Add a nested structure:

```bash
cd ~/go-exercises/internal-pkgs
mkdir -p api/internal/validate
```

Create `api/internal/validate/validate.go`:

```go
package validate

import "errors"

func UserID(id string) error {
	if len(id) < 3 {
		return errors.New("user ID too short")
	}
	return nil
}
```

Modify `api/api.go` to use it:

```go
package api

import (
	"internal-pkgs/api/internal/validate"
	"internal-pkgs/internal/hash"
)

func TokenFor(userID string) (string, error) {
	if err := validate.UserID(userID); err != nil {
		return "", err
	}
	return hash.Sum("token:" + userID), nil
}
```

Update `main.go` to handle the error:

```go
func main() {
	token, err := api.TokenFor("user-42")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Token:", token)

	_, err = api.TokenFor("ab")
	if err != nil {
		fmt.Println("Error:", err)
	}
}
```

The `api/internal/validate` package is importable by `api` but not by `main` (because `main` is not inside `api/`).

### Intermediate Verification

```bash
cd ~/go-exercises/internal-pkgs
go run .
```

Expected:

```
Token: <sha256 hex string>
Error: user ID too short
```

## Common Mistakes

### Putting Everything in `internal/`

**Wrong:** Making your entire module internal so nobody can use it.

**Fix:** Only put implementation details in `internal/`. Your public API packages should be at the top level or in well-named directories.

### Confusing `internal/` Scope

**Wrong assumption:** "My `pkg/auth/internal/hash` is accessible from `pkg/server`."

**Fact:** It is not. The `internal/` directory scopes to its immediate parent (`pkg/auth` in this case). Only packages rooted at `pkg/auth` can import it.

### Using `internal/` for Test Helpers

This is valid but consider `_test` packages or `testutil` packages instead if the helpers are only used in tests.

## Verify What You Learned

Run the final program with the nested internal package:

```bash
cd ~/go-exercises/internal-pkgs
go run .
```

Confirm both the successful token generation and the validation error.

## What's Next

Continue to [04 - Go Module Versioning](../04-go-module-versioning/04-go-module-versioning.md) to learn how Go modules handle semantic versioning and checksums.

## Summary

- Packages inside `internal/` can only be imported by code within the parent directory
- This is enforced by the compiler, not by convention
- Nested `internal/` directories scope to their immediate parent
- Use `internal/` for implementation details shared between your own packages
- Keep your public API packages outside `internal/`
- The restriction applies to external modules as well as other parts of your own module

## Reference

- [Go specification: Internal packages](https://go.dev/doc/go1.4#internalpackages)
- [Go command: Internal directories](https://pkg.go.dev/cmd/go#hdr-Internal_Directories)
- [Organizing a Go module](https://go.dev/doc/modules/layout)
