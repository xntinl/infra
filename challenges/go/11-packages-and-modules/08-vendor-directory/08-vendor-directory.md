# 8. Vendor Directory

<!--
difficulty: advanced
concepts: [go-mod-vendor, vendor-directory, reproducible-builds, offline-builds, vendor-modules-txt]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [go-modules, dependency-management, module-proxies-and-goproxy]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [07 - Module Proxies and GOPROXY](../07-module-proxies-and-goproxy/07-module-proxies-and-goproxy.md)
- Understanding of `go.mod`, `go.sum`, and the module proxy system

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** when and why to vendor dependencies
- **Use** `go mod vendor` to create a vendor directory
- **Build** with `-mod=vendor` for hermetic, offline builds
- **Analyze** the trade-offs between vendoring and proxy-based dependency resolution

## Why Vendor Directory

The module proxy caches dependencies, but you still depend on network access and the proxy's availability. The `vendor/` directory copies all dependencies into your repository, giving you:

1. **Offline builds** -- no network needed at build time
2. **Full auditability** -- every line of dependency code is in your repo
3. **Hermetic CI** -- builds succeed even if a module is deleted upstream
4. **Security review** -- you can inspect and approve dependency changes in PRs

The trade-off is repository size. Vendoring adds potentially megabytes of code. Most open-source projects do not vendor; many enterprise projects do.

## The Problem

Create a project that vendors its dependencies, verify the vendor directory structure, and build both with and without vendoring.

### Requirements

1. Create a Go module with at least two external dependencies.
2. Vendor the dependencies using `go mod vendor`.
3. Examine the `vendor/modules.txt` manifest.
4. Build with `-mod=vendor` and verify it works offline.
5. Compare the vendored build with a non-vendored build.

### Hints

<details>
<summary>Hint 1: Creating the vendor directory</summary>

```bash
go mod vendor
```

This creates a `vendor/` directory containing copies of all dependency packages, plus `vendor/modules.txt` which maps packages to modules and versions.
</details>

<details>
<summary>Hint 2: Building with vendor</summary>

```bash
# Explicitly use vendor directory
go build -mod=vendor ./...

# Or set it as default for the module
go env -w GOFLAGS="-mod=vendor"
```

When `-mod=vendor` is set, Go reads dependencies from `vendor/` instead of the module cache.
</details>

<details>
<summary>Hint 3: Verifying vendor consistency</summary>

```bash
# Re-run vendor and check for changes
go mod vendor
git diff vendor/

# If there are changes, the vendor directory was stale
```

Add this check to CI to ensure the vendor directory stays in sync with `go.mod`.
</details>

<details>
<summary>Hint 4: Vendor directory structure</summary>

```
vendor/
  modules.txt              # manifest: module versions and packages
  github.com/
    google/
      uuid/                # vendored package source
        uuid.go
        ...
  golang.org/
    x/
      text/
        ...
```

Each dependency's source code is copied under `vendor/` following its module path.
</details>

## Verification

```bash
mkdir -p ~/go-exercises/vendoring
cd ~/go-exercises/vendoring
go mod init vendoring
```

Create `main.go`:

```go
package main

import (
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func main() {
	id := uuid.New()
	fmt.Println("ID:", id)

	c := cases.Title(language.English)
	fmt.Println("Title:", c.String("hello world"))
}
```

```bash
# Fetch dependencies
go mod tidy

# Create vendor directory
go mod vendor

# Verify structure
ls vendor/
cat vendor/modules.txt

# Build with vendor
go build -mod=vendor -o app .
./app

# Verify offline build (simulate by clearing cache)
GOFLAGS="-mod=vendor" go build -o app2 .
./app2
```

Expected output:

```
ID: <random uuid>
Title: Hello World
```

Both builds produce the same result.

## What's Next

Continue to [09 - Designing a Public Go Module](../09-designing-a-public-go-module/09-designing-a-public-go-module.md) to learn best practices for creating Go modules that others will import.

## Summary

- `go mod vendor` copies all dependencies into `vendor/`
- `vendor/modules.txt` maps packages to module versions
- Build with `-mod=vendor` to use vendored dependencies
- Vendoring enables offline, hermetic, auditable builds
- Keep `vendor/` in sync: run `go mod vendor` and check for diffs in CI
- Trade-off: repository size vs. build reliability and auditability
- Most open-source projects skip vendoring; many enterprises require it

## Reference

- [Go Modules Reference: Vendoring](https://go.dev/ref/mod#vendoring)
- [go mod vendor](https://pkg.go.dev/cmd/go#hdr-Make_vendored_copy_of_dependencies)
- [Go Blog: Module Mirror and Checksum Database](https://go.dev/blog/module-mirror-launch)
