# 56. HTTP Router with Radix Tree

<!--
difficulty: intermediate-advanced
category: web-servers-and-http
languages: [go]
concepts: [radix-tree, patricia-trie, path-parameters, wildcard-routing, method-routing, conflict-detection]
estimated_time: 4-5 hours
bloom_level: analyze
prerequisites: [go-basics, tree-data-structures, string-manipulation, http-methods, benchmarking]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Tree data structures (tries, prefix trees)
- String slicing and byte-level manipulation in Go
- HTTP methods and URL path conventions
- Basic benchmarking with `testing.B`
- Understanding of how web frameworks match URL patterns to handlers

## Learning Objectives

- **Implement** a compressed radix tree (Patricia trie) optimized for URL path segments
- **Analyze** how path compression reduces memory usage and lookup time compared to naive tries
- **Design** a route registration API that detects conflicting patterns at insertion time
- **Apply** parameterized route extraction (`:id`, `*path`) during tree traversal
- **Evaluate** radix tree lookup performance against linear route scanning through benchmarks

## The Challenge

Every HTTP framework needs a router. The simplest approach -- iterating through a list of patterns and checking each one -- works for ten routes but collapses at a thousand. Production routers like those in Gin, Echo, and httprouter use a compressed radix tree (also called a Patricia trie) where common URL prefixes are shared in the tree structure. Looking up `/api/users/42/posts` traverses four nodes instead of scanning hundreds of patterns.

Your task is to build an HTTP router backed by a radix tree. The router stores one tree per HTTP method. Each tree node holds a path segment (compressed -- shared prefixes are split only when needed). Routes can contain named parameters (`:id` matches any single segment) and catch-all wildcards (`*filepath` matches everything remaining). When a request arrives, the router walks the tree, extracts parameters into a map, and returns the matched handler.

Beyond matching, the router must detect conflicts at registration time. Registering `/users/:id` and `/users/:name` is a conflict (same position, different parameter names). Registering `/files/*path` and `/files/specific` is a conflict (catch-all swallows the static route). These must produce clear errors at startup, not silent misbehavior at runtime.

## Requirements

1. Implement a `Router` type with `Handle(method, pattern, handler)` for route registration
2. The internal data structure is a compressed radix tree (one tree per HTTP method)
3. Tree nodes share common prefixes: inserting `/api/users` and `/api/posts` creates a shared `/api/` node with two children
4. Support path parameters: `/users/:id` matches `/users/42` and extracts `{"id": "42"}`
5. Support catch-all wildcards: `/files/*filepath` matches `/files/css/style.css` and extracts `{"filepath": "css/style.css"}`
6. `Lookup(method, path) -> (handler, params, found)` traverses the tree and returns the matched handler with extracted parameters
7. Detect and return errors for conflicting routes at registration time: duplicate paths, parameter name conflicts at the same position, catch-all conflicts with child routes
8. Implement a `ServeHTTP(ResponseWriter, *Request)` method so the router can function as an `http.Handler` (write the dispatch logic, but the router itself must not depend on `net/http` for its core tree operations)
9. Support trailing slash normalization: `/users/` and `/users` match the same route (configurable)
10. Write benchmarks comparing radix tree lookup against a linear scan baseline for 10, 100, and 500 routes

## Hints

<details>
<summary>Hint 1: Node structure for the radix tree</summary>

Each node stores its path segment, children indexed by the first byte of their segment, an optional handler, parameter name (if this is a `:param` node), and whether it's a catch-all. The key insight: when inserting a new route that partially matches an existing node's segment, you split that node at the divergence point.

```go
type node struct {
    segment   string
    children  []*node
    indices   string // first byte of each child's segment for fast lookup
    handler   HandlerFunc
    paramName string
    wildcard  bool
    isLeaf    bool
}
```
</details>

<details>
<summary>Hint 2: Splitting nodes on prefix conflict</summary>

When inserting `/api/posts` into a tree that already has `/api/users`, find the common prefix `/api/`. If the existing node is `/api/users`, split it into `/api/` (parent) and `users` (child), then add `posts` as a sibling child. This split operation is the core of radix tree insertion.
</details>

<details>
<summary>Hint 3: Parameter and wildcard matching during lookup</summary>

During lookup, when you reach a node with a parameter (`:id`), consume the next path segment (everything until the next `/`) and store it in the params map. For a catch-all (`*filepath`), consume the entire remaining path. Backtracking may be needed: if a static child doesn't match, try the parameter child.
</details>

<details>
<summary>Hint 4: Conflict detection strategy</summary>

Conflicts arise when two routes would match the same input. Check at insertion time: if a node already has a parameter child and you're adding another parameter child with a different name, that's a conflict. If a node has a catch-all child and you try to add any other child, that's a conflict. Static segments never conflict with each other (they're different children).
</details>

## Acceptance Criteria

- [ ] Radix tree correctly compresses shared prefixes (verifiable by inspecting tree depth)
- [ ] Static routes match exactly: `/users`, `/users/settings`, `/api/v2/health`
- [ ] Parameter routes extract values: `/users/:id` matches `/users/42` -> `{"id": "42"}`
- [ ] Catch-all routes capture remainder: `/files/*path` matches `/files/a/b/c` -> `{"path": "a/b/c"}`
- [ ] Method-based routing: same path with different methods dispatches to different handlers
- [ ] Conflicting route registration returns a descriptive error
- [ ] Trailing slash normalization works when enabled
- [ ] Lookup returns not-found for unregistered paths without panic
- [ ] Benchmark shows radix tree outperforms linear scan at 100+ routes
- [ ] Router implements `http.Handler` and correctly dispatches incoming requests

## Research Resources

- [httprouter: How does it work?](https://github.com/julienschmidt/httprouter) -- the Go router that popularized radix tree routing; study its `tree.go`
- [Gin Web Framework: route tree internals](https://github.com/gin-gonic/gin/blob/master/tree.go) -- Gin's fork of httprouter with enhancements
- [Morrison: "PATRICIA -- Practical Algorithm To Retrieve Information Coded in Alphanumeric" (1968)](https://dl.acm.org/doi/10.1145/321479.321481) -- the original Patricia trie paper
- [Wikipedia: Radix Tree](https://en.wikipedia.org/wiki/Radix_tree) -- visual walkthrough of insertion and lookup
- [Go `testing` package: Benchmarks](https://pkg.go.dev/testing#hdr-Benchmarks) -- how to write and interpret Go benchmarks
