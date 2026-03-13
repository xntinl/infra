# 13. Designing a Plugin System with Interfaces

<!--
difficulty: insane
concepts: [plugin-architecture, interface-discovery, registration-pattern, lifecycle-management, dependency-resolution, hot-reload]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [implicit-interface-satisfaction, interface-composition, dependency-injection, accept-interfaces-return-structs]
-->

## Prerequisites

- Completed exercises 1-12 in this section or equivalent experience
- Strong understanding of interface composition and embedding
- Familiarity with the `plugin` package and its limitations
- Experience with dependency injection patterns

## Learning Objectives

After completing this challenge, you will be able to:

- **Create** a plugin system that discovers, loads, and manages plugins through well-defined interfaces
- **Design** plugin lifecycle interfaces (initialize, start, stop, health check) that handle real-world operational concerns
- **Evaluate** tradeoffs between compile-time plugin registration and runtime discovery

## The Challenge

Build a plugin system for a hypothetical application server. The system must support multiple plugin types — HTTP handlers, middleware, background workers, and event listeners — each defined by its own interface. Plugins register themselves through a central registry, declare their dependencies on other plugins, and follow a managed lifecycle.

The core difficulty is designing the interface hierarchy so that plugins are loosely coupled yet composable. A middleware plugin should be usable by any HTTP handler plugin without either knowing the concrete type of the other. The registry must resolve dependencies in topological order and detect circular dependencies at registration time, not at runtime.

Your plugin system must handle graceful degradation: if a non-critical plugin fails to initialize, the system should log the failure and continue with reduced functionality rather than crashing. Critical plugins (marked via interface) must succeed or the entire system refuses to start.

Consider also versioning: plugins should be able to declare a semantic version and the registry should detect incompatible version constraints between dependent plugins. This does not require full semver resolution — a simple major-version compatibility check is sufficient.

## Requirements

1. Define at least 5 plugin interfaces: `Plugin` (base with Name, Version, Init, Close), `HTTPHandler` (ServeHTTP pattern), `Middleware` (wraps handlers), `Worker` (background goroutine with Start/Stop), `EventListener` (handles typed events), and `HealthChecker` (reports health status)

2. Implement a `Registry` that accepts plugin registration, validates interface satisfaction at registration time, and resolves a dependency-ordered initialization sequence

3. Plugins declare dependencies by returning a list of plugin names from a `DependsOn() []string` method — the registry must detect and reject circular dependencies with a clear error message identifying the cycle

4. Implement a `Lifecycle` manager that initializes plugins in dependency order, starts workers, and shuts down in reverse order with configurable per-plugin timeouts

5. Support optional interfaces: if a plugin implements `HealthChecker`, include it in health aggregation; if it implements `Configurable`, pass it a configuration map during init — plugins should not be required to implement every interface

6. Implement a `CriticalPlugin` marker interface — if a critical plugin fails Init, the system must abort startup and close all already-initialized plugins in reverse order

7. Add version compatibility checking: plugins declare their version as `(major, minor, patch)` and dependencies can specify minimum major version — the registry rejects incompatible combinations

8. Write at least 3 concrete plugin implementations demonstrating different interface combinations: an auth middleware (Middleware + HealthChecker), a metrics worker (Worker + EventListener), and an API handler (HTTPHandler + Configurable)

9. The plugin system must be safe for concurrent use — multiple goroutines should be able to query health status while workers are running

10. Include a demonstration `main` function that registers plugins, starts the system, handles SIGTERM for graceful shutdown, and exercises all plugin types

## Hints

- Use a directed acyclic graph (DAG) for dependency resolution — topological sort with Kahn's algorithm or DFS-based detection gives you both ordering and cycle detection

- The marker interface pattern (`CriticalPlugin` with no methods beyond a tag method like `IsCritical()`) is idiomatic Go for optional behavior classification

- Consider using `sync.Map` or a mutex-protected map for the registry if concurrent registration is needed, but a simpler approach is to require all registration before system start (two-phase: register, then start)

- For the health aggregation, define health as an enum (Healthy, Degraded, Unhealthy) and aggregate across all HealthChecker plugins — the system is as healthy as its least healthy critical component

- `context.Context` should flow through Init, Start, and Stop to support cancellation and deadline propagation

## Success Criteria

1. Registering plugins with circular dependencies returns an error that names the cycle (e.g., "circular dependency: A -> B -> C -> A")

2. Initializing plugins proceeds in correct dependency order — if B depends on A, A.Init() completes before B.Init() begins

3. If a CriticalPlugin fails Init, all previously initialized plugins have their Close methods called in reverse order before the error is returned

4. Health check aggregation correctly reports Degraded when a non-critical plugin is unhealthy and Unhealthy when a critical plugin is unhealthy

5. Graceful shutdown stops workers and closes plugins in reverse initialization order, respecting per-plugin timeout deadlines

6. The auth middleware can wrap the API handler without either knowing the other's concrete type — only interfaces are shared

7. Version compatibility rejects registration when a plugin requires major version 2 of a dependency but only major version 1 is registered

8. All operations are safe under concurrent access — running `go test -race` produces no data race warnings

## Research Resources

- [Go Blog - Interfaces](https://go.dev/blog/interfaces) — the philosophy behind Go's implicit interface satisfaction

- [Effective Go - Interfaces](https://go.dev/doc/effective_go#interfaces) — idiomatic interface design patterns

- [Go plugin package](https://pkg.go.dev/plugin) — the standard library plugin mechanism and its limitations

- [Topological Sort](https://en.wikipedia.org/wiki/Topological_sorting) — algorithms for dependency resolution in DAGs

- [Hashicorp go-plugin](https://github.com/hashicorp/go-plugin) — a production plugin system over RPC for design inspiration
