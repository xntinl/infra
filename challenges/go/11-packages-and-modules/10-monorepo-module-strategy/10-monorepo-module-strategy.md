# 10. Monorepo Module Strategy

<!--
difficulty: insane
concepts: [monorepo, multi-module, module-boundaries, go-work, release-strategy, api-compatibility]
tools: [go, git]
estimated_time: 60m
bloom_level: create
prerequisites: [go-modules, multi-module-workspaces, internal-packages, designing-a-public-go-module]
-->

## The Challenge

Design and implement a monorepo containing multiple Go modules that represent a microservices platform. The modules must have clean dependency boundaries, independent versioning, and a development workflow that allows cross-module changes without publishing.

## Requirements

1. **Three separate Go modules in one repository:**
   - `platform/core` -- shared types, errors, and interfaces (e.g., `Logger`, `Store`, `Event`)
   - `platform/auth` -- authentication library that depends on `core`
   - `platform/gateway` -- API gateway service that depends on both `core` and `auth`

2. **Each module must:**
   - Have its own `go.mod` with a proper module path (e.g., `github.com/example/platform/core`)
   - Be independently versionable via git tags (e.g., `core/v0.1.0`, `auth/v0.2.0`)
   - Contain at least one exported package with tests

3. **A `go.work` file at the repo root** for local development across modules.

4. **The `core` module must define:**
   - A `Logger` interface with `Info`, `Error`, and `With` methods
   - A `Store` interface with `Get`, `Put`, and `Delete` methods
   - Shared error types (e.g., `ErrNotFound`, `ErrUnauthorized`)
   - A `User` type used by both `auth` and `gateway`

5. **The `auth` module must:**
   - Accept a `core.Store` interface for token persistence
   - Provide `Authenticate(token string) (*core.User, error)` function
   - Use `internal/` for token generation/validation logic

6. **The `gateway` module must:**
   - Accept `core.Logger` and `auth.Authenticator` as dependencies
   - Provide an HTTP handler that validates auth and proxies requests
   - Include integration-style tests using mocks

7. **Dependency direction must be strict:** `core` depends on nothing, `auth` depends on `core`, `gateway` depends on `core` and `auth`. No circular dependencies.

## Hints

<details>
<summary>Hint 1: Repository structure</summary>

```
platform/
  go.work
  core/
    go.mod              # module github.com/example/platform/core
    core.go             # shared types and interfaces
    errors.go           # sentinel errors
    core_test.go
  auth/
    go.mod              # module github.com/example/platform/auth
    auth.go             # public API
    auth_test.go
    internal/
      token/
        token.go        # token generation, unexported
  gateway/
    go.mod              # module github.com/example/platform/gateway
    gateway.go          # HTTP handler
    gateway_test.go
```
</details>

<details>
<summary>Hint 2: Workspace setup</summary>

```bash
cd platform
go work init
go work use ./core ./auth ./gateway
```

The workspace lets `auth` import `core` and `gateway` import both without publishing anything.
</details>

<details>
<summary>Hint 3: Module dependency declarations</summary>

In `auth/go.mod`:
```
module github.com/example/platform/auth

go 1.22

require github.com/example/platform/core v0.0.0
```

The version does not matter during local development because `go.work` overrides it. Before publishing, update to the real tagged version.
</details>

<details>
<summary>Hint 4: Versioning with git tags</summary>

```bash
# Tag core module
git tag core/v0.1.0

# Tag auth module
git tag auth/v0.1.0

# Tag gateway module
git tag gateway/v0.1.0
```

Go uses `<prefix>/v<version>` tags for modules in subdirectories. The prefix matches the subdirectory path relative to the repo root.
</details>

<details>
<summary>Hint 5: Avoiding circular dependencies</summary>

Define interfaces in `core`. Have `auth` implement `core` interfaces. Have `gateway` accept interfaces from `core` and implementations from `auth`. Never import `gateway` from `auth` or `core`.

If you need `auth` to call `gateway`, define an interface in `auth` that `gateway` implements -- not the other way around.
</details>

## Success Criteria

1. `go work sync` succeeds from the repo root
2. `go build ./...` succeeds from the repo root (workspace mode)
3. `go test ./...` succeeds from the repo root (workspace mode)
4. Each module can be built independently: `cd core && go build ./...`
5. `go mod graph` from each module shows correct dependency direction
6. No import cycles exist between modules
7. `core` has zero external dependencies
8. `auth/internal/` packages cannot be imported by `gateway`
9. All exported types and functions have documentation comments

Verify with:

```bash
cd ~/go-exercises/platform
go work sync
go build ./...
go test ./...
go vet ./...

# Verify each module independently
cd core && go build ./... && cd ..
cd auth && go build ./... && cd ..
cd gateway && go build ./... && cd ..

# Verify no import cycles
cd gateway && go mod graph
```

## Research Resources

- [Go Modules Reference](https://go.dev/ref/mod)
- [Go Workspaces](https://go.dev/doc/tutorial/workspaces)
- [Go Blog: Module version numbering](https://go.dev/doc/modules/version-numbers)
- [Organizing a Go module](https://go.dev/doc/modules/layout)
- [Go at Google: Monorepos](https://go.dev/talks/2023/gophercon-israel)
- [Multi-module repositories](https://go.dev/doc/modules/managing-source#multiple-module-source)

## Summary

- Monorepos with multiple Go modules require careful dependency direction
- Use `go.work` for local cross-module development
- Tag releases with `<subdir>/v<version>` format for proper version resolution
- Define shared interfaces and types in a `core` module with zero dependencies
- Use `internal/` within each module for implementation hiding
- Each module should be independently buildable, testable, and versionable
- Workspace files (`go.work`) are local tools -- do not commit them
