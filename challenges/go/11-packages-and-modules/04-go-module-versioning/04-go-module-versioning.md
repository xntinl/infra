# 4. Go Module Versioning

<!--
difficulty: intermediate
concepts: [semantic-versioning, go-sum, checksums, module-versions, go-get]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [go-modules, package-declaration, imports]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [03 - Internal Packages](../03-internal-packages/03-internal-packages.md)
- Basic familiarity with `go mod init` and `go.mod`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how Go modules use semantic versioning
- **Apply** `go get` to add, upgrade, and downgrade dependencies
- **Interpret** the `go.sum` file and its role in supply chain security

## Why Module Versioning

Go modules use semantic versioning (semver) to manage dependencies. Every module has a version like `v1.2.3`:

- **Major** (v1 → v2): breaking changes. Go treats v2+ as a different module path (`module/v2`).
- **Minor** (v1.2 → v1.3): new features, backwards compatible.
- **Patch** (v1.2.3 → v1.2.4): bug fixes, backwards compatible.

The `go.sum` file contains cryptographic checksums of every dependency. This ensures that when you or your CI system builds the code, you get exactly the same bytes that were there when you ran `go mod tidy`. If someone tampers with a module on the proxy, the checksum will not match and the build fails.

## Step 1 -- Examine `go.mod` and `go.sum`

```bash
mkdir -p ~/go-exercises/mod-versioning
cd ~/go-exercises/mod-versioning
go mod init mod-versioning
```

Create `main.go`:

```go
package main

import (
	"fmt"

	"golang.org/x/text/language"
)

func main() {
	tag, _ := language.Parse("en-US")
	fmt.Println("Language:", tag)
}
```

Add the dependency:

```bash
go mod tidy
```

### Intermediate Verification

```bash
cat go.mod
```

Expected (versions may differ):

```
module mod-versioning

go 1.22

require golang.org/x/text v0.14.0
```

```bash
cat go.sum | head -5
```

Expected: lines with module paths, versions, and base64-encoded hashes:

```
golang.org/x/text v0.14.0 h1:ScX5w1eTa3QqT8oi6+ziP7dTV1S2+ALU0bI+0zXKWiQ=
golang.org/x/text v0.14.0/go.mod h1:18ZOQIKpY8NJVqYksKHtTdi31H5itFRjB5/qKTNYzSU=
```

Each entry has two lines: one for the module content hash (`h1:`), one for the `go.mod` hash.

## Step 2 -- Add a Specific Version

Add a dependency at a specific version:

```bash
go get github.com/google/uuid@v1.5.0
```

Update `main.go`:

```go
package main

import (
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/text/language"
)

func main() {
	tag, _ := language.Parse("en-US")
	fmt.Println("Language:", tag)

	id := uuid.New()
	fmt.Println("UUID:", id)
}
```

### Intermediate Verification

```bash
go run .
```

Expected:

```
Language: en-US
UUID: <random uuid>
```

Check the version in `go.mod`:

```bash
grep uuid go.mod
```

Expected:

```
	github.com/google/uuid v1.5.0
```

## Step 3 -- Upgrade and Downgrade

Upgrade to the latest version:

```bash
go get github.com/google/uuid@latest
grep uuid go.mod
```

Downgrade to an earlier version:

```bash
go get github.com/google/uuid@v1.4.0
grep uuid go.mod
```

### Intermediate Verification

After downgrade:

```bash
grep uuid go.mod
```

Expected:

```
	github.com/google/uuid v1.4.0
```

The program still works because v1.4.0 and v1.5.0 have the same API (minor version change).

## Step 4 -- Understand Major Version Paths

Go treats major versions v2+ as different module paths. This means you can use v1 and v2 of the same module simultaneously:

```go
import (
    v1 "github.com/example/lib"      // v1.x
    v2 "github.com/example/lib/v2"   // v2.x
)
```

This is called the **import compatibility rule**: if the import path is the same, the code must be compatible. Incompatible changes require a new import path.

This is a concept step -- no code to run. But understand that when a module reaches v2, its `go.mod` declares `module github.com/example/lib/v2` and all import paths change.

### Intermediate Verification

Check a real-world example:

```bash
go doc github.com/google/uuid 2>/dev/null | head -3
```

This module is still v1.x, so the import path has no `/v2` suffix.

## Step 5 -- Clean Up Dependencies

Remove unused dependencies:

```bash
go mod tidy
```

Verify nothing changed (since we are using both):

```bash
go mod tidy -v
```

### Intermediate Verification

```bash
go run .
```

Program runs successfully.

## Common Mistakes

### Editing `go.mod` by Hand

**Wrong:** Manually changing version numbers in `go.mod`.

**Fix:** Use `go get pkg@version` to change versions. It updates both `go.mod` and `go.sum` correctly.

### Deleting `go.sum`

**Wrong:** Deleting `go.sum` because it is "generated."

**What happens:** Next build downloads dependencies again and creates a new `go.sum`. But if the module proxy served different content (unlikely but possible), your build could use tampered code.

**Fix:** Commit `go.sum` to version control. It is your supply chain security checkpoint.

### Ignoring the v2+ Path Rule

**Wrong:** Bumping a module to v2.0.0 without changing the module path.

**What happens:** Go will not recognize the new major version correctly. Consumers cannot import v2.

**Fix:** When releasing v2+, update `go.mod` to `module example.com/mylib/v2` and update all internal imports.

## Verify What You Learned

Run the final program:

```bash
go run .
```

Verify that `go.mod` and `go.sum` are consistent:

```bash
go mod verify
```

Expected:

```
all modules verified
```

## What's Next

Continue to [05 - Multi-Module Workspaces](../05-multi-module-workspaces/05-multi-module-workspaces.md) to learn how `go.work` files enable local development with multiple modules.

## Summary

- Go modules use semantic versioning: `vMAJOR.MINOR.PATCH`
- `go get pkg@version` adds, upgrades, or downgrades dependencies
- `go.sum` contains checksums for supply chain security -- commit it to version control
- Major versions v2+ use different import paths (`module/v2`)
- `go mod tidy` removes unused dependencies and adds missing ones
- `go mod verify` checks that dependencies match their checksums

## Reference

- [Go Modules Reference](https://go.dev/ref/mod)
- [Module version numbering](https://go.dev/doc/modules/version-numbers)
- [go.sum file reference](https://go.dev/ref/mod#go-sum-files)
- [Semantic Versioning 2.0.0](https://semver.org/)
