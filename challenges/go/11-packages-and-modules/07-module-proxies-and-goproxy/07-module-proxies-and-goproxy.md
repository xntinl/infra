# 7. Module Proxies and GOPROXY

<!--
difficulty: advanced
concepts: [goproxy, gonosumcheck, gonosumdb, private-modules, module-proxy, checksum-database]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [go-modules, module-versioning, dependency-management]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - Dependency Management](../06-dependency-management/06-dependency-management.md)
- Understanding of `go.mod`, `go.sum`, and `go get`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how Go fetches modules through proxies and the checksum database
- **Configure** `GOPROXY`, `GONOSUMCHECK`, and `GOPRIVATE` for private modules
- **Apply** proxy configuration for corporate and air-gapped environments

## Why Module Proxies

When you run `go get`, Go does not fetch code directly from GitHub. Instead, it goes through a **module proxy** (by default, `proxy.golang.org`). The proxy caches modules, making downloads faster and ensuring that deleted repositories do not break your builds.

Go also uses a **checksum database** (`sum.golang.org`) to verify that module content has not been tampered with. When you download a module, Go checks its hash against the checksum database.

This system works well for open-source modules. But private modules (hosted on internal Git servers) are not on the public proxy or checksum database. You need to configure Go to fetch them differently.

## The Problem

Configure a Go development environment to handle three scenarios:

1. Public modules fetched through the default proxy
2. Private modules (`company.internal/*`) fetched directly from your Git server
3. An air-gapped environment that uses a self-hosted proxy

### Requirements

1. Inspect the current proxy configuration and understand the default values.
2. Configure `GOPRIVATE` so that `company.internal/*` modules bypass the proxy and checksum database.
3. Configure `GONOSUMCHECK` for modules you trust but cannot publish to the checksum database.
4. Simulate a self-hosted proxy by examining how `GOPROXY` is configured with multiple fallbacks.

### Hints

<details>
<summary>Hint 1: Inspect current configuration</summary>

```bash
go env GOPROXY
go env GONOSUMCHECK
go env GONOSUMDB
go env GOPRIVATE
```

Default `GOPROXY` is: `https://proxy.golang.org,direct`

This means: try the proxy first, fall back to direct VCS access.
</details>

<details>
<summary>Hint 2: Setting GOPRIVATE</summary>

```bash
# Set for the current session
export GOPRIVATE="company.internal/*,github.com/myorg/*"

# Or permanently via go env
go env -w GOPRIVATE="company.internal/*,github.com/myorg/*"
```

`GOPRIVATE` is a comma-separated list of module path patterns. Matching modules:
- Skip the proxy (fetched directly from VCS)
- Skip the checksum database (not verified against `sum.golang.org`)
</details>

<details>
<summary>Hint 3: GOPROXY with multiple proxies</summary>

```bash
# Try company proxy first, then public proxy, then direct
export GOPROXY="https://proxy.company.internal,https://proxy.golang.org,direct"
```

The `|` separator (instead of `,`) means "try next only on HTTP errors, not on 404":
```bash
export GOPROXY="https://proxy.company.internal|https://proxy.golang.org,direct"
```
</details>

<details>
<summary>Hint 4: GONOSUMCHECK vs GONOSUMDB</summary>

- `GONOSUMCHECK`: do not verify checksum at all (dangerous, use sparingly)
- `GONOSUMDB`: do not look up checksums in the public database, but still verify against `go.sum`
- `GOPRIVATE` sets both `GONOSUMCHECK` and `GONOSUMDB` for matching patterns
</details>

## Verification

Run these commands and understand the output:

```bash
# 1. Show current proxy config
go env GOPROXY
# Expected: https://proxy.golang.org,direct

# 2. Set private modules
go env -w GOPRIVATE="example.internal/*"
go env GOPRIVATE
# Expected: example.internal/*

# 3. Verify GONOSUMDB is also set
go env GONOSUMDB
# Expected: example.internal/*

# 4. Reset to defaults
go env -u GOPRIVATE
go env GOPRIVATE
# Expected: (empty)
```

Write a Go program that prints all module-related environment variables:

```go
package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func main() {
	vars := []string{"GOPROXY", "GONOSUMCHECK", "GONOSUMDB", "GOPRIVATE", "GOPATH", "GOMODCACHE"}
	for _, v := range vars {
		cmd := exec.Command("go", "env", v)
		out, _ := cmd.Output()
		fmt.Printf("%-15s = %s", v, strings.TrimSpace(string(out)))
		fmt.Println()
	}
}
```

```bash
go run main.go
```

## What's Next

Continue to [08 - Vendor Directory](../08-vendor-directory/08-vendor-directory.md) to learn how `go mod vendor` creates reproducible builds without network access.

## Summary

- Go fetches modules through `proxy.golang.org` by default
- The checksum database (`sum.golang.org`) verifies module integrity
- `GOPRIVATE` bypasses proxy and checksum for private module patterns
- `GOPROXY` can be configured with multiple proxies and fallback strategies
- Use `,` separator for "try next on any error" and `|` for "try next only on non-404"
- `GONOSUMDB` skips the public checksum database but still checks `go.sum`
- `GONOSUMCHECK` disables checksum verification entirely (use with caution)

## Reference

- [Module proxies](https://go.dev/ref/mod#module-proxy)
- [GOPROXY protocol](https://go.dev/ref/mod#goproxy-protocol)
- [Checksum database](https://go.dev/ref/mod#checksum-database)
- [Private modules](https://go.dev/ref/mod#private-modules)
