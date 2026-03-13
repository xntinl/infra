# 6. Dependency Management

<!--
difficulty: intermediate
concepts: [go-get, go-mod-tidy, replace-directive, dependency-graph, indirect-dependencies]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [go-modules, module-versioning, imports]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Multi-Module Workspaces](../05-multi-module-workspaces/05-multi-module-workspaces.md)
- Understanding of `go.mod` and `go.sum`

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `go get` to add, update, and remove dependencies
- **Apply** `go mod tidy` to synchronize `go.mod` with actual imports
- **Explain** the `replace` directive and when to use it

## Why Dependency Management Matters

As your project grows, you will depend on external libraries. Managing these dependencies correctly ensures reproducible builds, prevents version conflicts, and keeps your project secure. Go's module system automates most of this, but you need to understand the tools.

`go mod tidy` is the workhorse: it scans your source code, adds missing dependencies, removes unused ones, and updates `go.sum`. `go get` is for intentional version changes. The `replace` directive lets you substitute one module for another -- useful for forks, local development, or workarounds.

## Step 1 -- Add Dependencies with `go get`

```bash
mkdir -p ~/go-exercises/dep-management
cd ~/go-exercises/dep-management
go mod init dep-management
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"log"

	"github.com/google/uuid"
	"golang.org/x/exp/slices"
)

func main() {
	id := uuid.New()
	fmt.Println("Generated UUID:", id)

	names := []string{"Charlie", "Alice", "Bob"}
	slices.Sort(names)
	fmt.Println("Sorted:", names)

	idx, found := slices.BinarySearch(names, "Bob")
	if found {
		fmt.Printf("Found Bob at index %d\n", idx)
	}

	log.Println("Done")
}
```

Add the dependencies:

```bash
go mod tidy
```

### Intermediate Verification

```bash
go run .
```

Expected:

```
Generated UUID: <random uuid>
Sorted: [Alice Bob Charlie]
Found Bob at index 1
<timestamp> Done
```

Check `go.mod`:

```bash
cat go.mod
```

You should see both direct and indirect dependencies.

## Step 2 -- Understand Direct vs Indirect Dependencies

```bash
cat go.mod
```

Direct dependencies are the ones you import. Indirect dependencies (marked with `// indirect`) are pulled in by your direct dependencies.

List the full dependency graph:

```bash
go mod graph
```

This shows every module and what it depends on.

### Intermediate Verification

```bash
go mod graph | head -10
```

You will see lines like:

```
dep-management github.com/google/uuid@v1.6.0
dep-management golang.org/x/exp@v0.0.0-...
```

## Step 3 -- Update a Dependency

Check available versions:

```bash
go list -m -versions github.com/google/uuid
```

Upgrade to a specific version:

```bash
go get github.com/google/uuid@v1.5.0
grep uuid go.mod
```

Upgrade to the latest:

```bash
go get github.com/google/uuid@latest
grep uuid go.mod
```

### Intermediate Verification

```bash
go run .
```

Program runs with the updated version.

## Step 4 -- Remove a Dependency

Remove the `slices` import from `main.go`:

```go
package main

import (
	"fmt"
	"log"

	"github.com/google/uuid"
)

func main() {
	id := uuid.New()
	fmt.Println("Generated UUID:", id)
	log.Println("Done")
}
```

Run `go mod tidy` to clean up:

```bash
go mod tidy
cat go.mod
```

### Intermediate Verification

The `golang.org/x/exp` dependency should be gone from `go.mod`:

```bash
grep exp go.mod
```

Expected: no output.

## Step 5 -- Use the `replace` Directive

The `replace` directive substitutes one module for another. Common uses:

1. **Fork a dependency** to fix a bug before the upstream merges your PR
2. **Local development** when workspaces are not suitable
3. **Workaround** a broken version

```bash
# Example: replace a module with a fork
go mod edit -replace github.com/google/uuid=github.com/your-fork/uuid@v1.5.0

# Example: replace with a local path
go mod edit -replace github.com/google/uuid=../my-local-uuid

# Remove the replace
go mod edit -dropreplace github.com/google/uuid
```

### Intermediate Verification

After adding and removing a replace:

```bash
cat go.mod
```

The `replace` directive should be gone after `dropreplace`.

## Common Mistakes

### Running `go get` Instead of `go mod tidy`

**Wrong workflow:** Manually running `go get` for every import you add.

**Better:** Write your code with the imports you need, then run `go mod tidy`. It handles everything.

### Leaving Stale `replace` Directives

**Wrong:** Committing `replace ../local-path` to version control.

**What happens:** CI and other developers cannot build because the local path does not exist.

**Fix:** Remove `replace` directives before committing, or use `go.work` for local development.

### Ignoring `go mod verify`

**Best practice:** Run `go mod verify` in CI to ensure dependencies have not been tampered with.

```bash
go mod verify
```

Expected: `all modules verified`.

## Verify What You Learned

```bash
cd ~/go-exercises/dep-management
go mod tidy
go mod verify
go run .
```

Confirm the program runs and modules are verified.

## What's Next

Continue to [07 - Module Proxies and GOPROXY](../07-module-proxies-and-goproxy/07-module-proxies-and-goproxy.md) to learn how Go fetches modules and how to configure private module access.

## Summary

- `go mod tidy` is the primary tool: adds missing deps, removes unused ones
- `go get pkg@version` changes a specific dependency version
- `go list -m -versions pkg` shows available versions
- `// indirect` marks dependencies pulled in by your direct dependencies
- `replace` substitutes one module for another (forks, local paths)
- Always run `go mod verify` in CI to check dependency integrity
- Do not commit `replace` directives with local paths

## Reference

- [Managing dependencies](https://go.dev/doc/modules/managing-dependencies)
- [go mod tidy](https://pkg.go.dev/cmd/go#hdr-Add_missing_and_remove_unused_modules)
- [go get](https://pkg.go.dev/cmd/go#hdr-Add_dependencies_to_current_module_and_install_them)
- [Go Modules Reference: replace](https://go.dev/ref/mod#go-mod-file-replace)
