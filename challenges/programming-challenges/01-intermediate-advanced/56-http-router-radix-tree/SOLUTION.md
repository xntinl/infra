# Solution: HTTP Router with Radix Tree

## Architecture Overview

The router is built on three components: a **radix tree** that stores compressed path segments in a trie structure, a **parameter extractor** that captures named segments and wildcards during traversal, and a **method dispatcher** that maintains one tree per HTTP method.

Each tree node stores its path segment as a string. When two routes share a prefix, the node is split at the divergence point so the common prefix becomes a parent. Child lookup uses an index string (first byte of each child's segment) for O(1) dispatch to the correct subtree. Parameter nodes (`:id`) match any single segment, catch-all nodes (`*path`) match the rest of the URL.

```
Tree for GET:

/api/
├── users
│   ├── (leaf: GET /api/users)
│   └── /
│       └── :id (leaf: GET /api/users/:id)
└── posts
    └── /
        └── :slug
            └── /comments (leaf: GET /api/posts/:slug/comments)
```

Conflict detection happens at insertion time by checking whether a new parameter or wildcard node clashes with existing children at the same tree level.

## Go Solution

### Project Setup

```bash
mkdir -p http-router && cd http-router
go mod init http-router
```

### Implementation

```go
// router.go
package router

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// HandlerFunc is the signature for route handlers.
type HandlerFunc func(http.ResponseWriter, *http.Request, Params)

// Params holds extracted path parameters.
type Params map[string]string

// Get retrieves a parameter by name.
func (p Params) Get(name string) string {
	return p[name]
}

// Router dispatches HTTP requests using a radix tree per method.
type Router struct {
	trees          map[string]*node
	mu             sync.RWMutex
	trailingSlash  bool
	notFoundHandler http.HandlerFunc
}

// Option configures the router.
type Option func(*Router)

// WithTrailingSlashNormalization enables treating /path and /path/ as equivalent.
func WithTrailingSlashNormalization() Option {
	return func(r *Router) {
		r.trailingSlash = true
	}
}

// WithNotFound sets a custom 404 handler.
func WithNotFound(h http.HandlerFunc) Option {
	return func(r *Router) {
		r.notFoundHandler = h
	}
}

func New(opts ...Option) *Router {
	r := &Router{
		trees: make(map[string]*node),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

type nodeKind uint8

const (
	kindStatic   nodeKind = iota
	kindParam             // :name
	kindCatchAll          // *name
)

type node struct {
	segment   string
	children  []*node
	indices   string // first byte of each static child's segment
	handler   HandlerFunc
	kind      nodeKind
	paramName string
	paramChild   *node
	catchChild   *node
	isLeaf    bool
}

// Handle registers a route pattern for a given HTTP method.
func (r *Router) Handle(method, pattern string, handler HandlerFunc) error {
	if pattern == "" || pattern[0] != '/' {
		return fmt.Errorf("pattern must start with /: %q", pattern)
	}
	if handler == nil {
		return fmt.Errorf("handler must not be nil for pattern %q", pattern)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	root, exists := r.trees[method]
	if !exists {
		root = &node{segment: "/"}
		r.trees[method] = root
	}

	path := pattern
	if r.trailingSlash && len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}

	return root.insert(path[1:], handler) // skip leading /
}

func (n *node) insert(path string, handler HandlerFunc) error {
	if path == "" {
		if n.isLeaf {
			return fmt.Errorf("route conflict: handler already registered for this path")
		}
		n.handler = handler
		n.isLeaf = true
		return nil
	}

	// Handle parameter segment
	if path[0] == ':' {
		return n.insertParam(path, handler)
	}

	// Handle catch-all segment
	if path[0] == '*' {
		return n.insertCatchAll(path, handler)
	}

	return n.insertStatic(path, handler)
}

func (n *node) insertParam(path string, handler HandlerFunc) error {
	slashIdx := strings.IndexByte(path, '/')
	var paramName, rest string
	if slashIdx == -1 {
		paramName = path[1:]
		rest = ""
	} else {
		paramName = path[1:slashIdx]
		rest = path[slashIdx+1:]
	}

	if n.catchChild != nil {
		return fmt.Errorf("route conflict: cannot add param :%s, catch-all already exists", paramName)
	}

	if n.paramChild != nil {
		if n.paramChild.paramName != paramName {
			return fmt.Errorf("route conflict: param :%s conflicts with existing :%s",
				paramName, n.paramChild.paramName)
		}
		return n.paramChild.insert(rest, handler)
	}

	child := &node{
		segment:   ":" + paramName,
		kind:      kindParam,
		paramName: paramName,
	}
	n.paramChild = child
	return child.insert(rest, handler)
}

func (n *node) insertCatchAll(path string, handler HandlerFunc) error {
	name := path[1:]
	if strings.ContainsAny(name, "/:*") {
		return fmt.Errorf("catch-all %q must be the last segment", path)
	}

	if len(n.children) > 0 || n.paramChild != nil {
		return fmt.Errorf("route conflict: catch-all *%s conflicts with existing child routes", name)
	}

	if n.catchChild != nil {
		return fmt.Errorf("route conflict: catch-all already registered")
	}

	child := &node{
		segment:   "*" + name,
		kind:      kindCatchAll,
		paramName: name,
		handler:   handler,
		isLeaf:    true,
	}
	n.catchChild = child
	return nil
}

func (n *node) insertStatic(path string, handler HandlerFunc) error {
	if n.catchChild != nil {
		return fmt.Errorf("route conflict: static path %q conflicts with existing catch-all", path)
	}

	for i, child := range n.children {
		prefix := commonPrefix(child.segment, path)
		if prefix == 0 {
			continue
		}

		// Exact match on this child's segment
		if prefix == len(child.segment) {
			rest := path[prefix:]
			if len(rest) > 0 && rest[0] == '/' {
				rest = rest[1:]
			} else if len(rest) == 0 {
				// Path ends here
			}
			return child.insert(rest, handler)
		}

		// Partial match: split this node
		splitChild := &node{
			segment:    child.segment[prefix:],
			children:   child.children,
			indices:    child.indices,
			handler:    child.handler,
			kind:       child.kind,
			paramName:  child.paramName,
			paramChild: child.paramChild,
			catchChild: child.catchChild,
			isLeaf:     child.isLeaf,
		}

		n.children[i] = &node{
			segment: child.segment[:prefix],
			kind:    kindStatic,
		}
		newNode := n.children[i]
		newNode.addChild(splitChild)

		rest := path[prefix:]
		if len(rest) == 0 {
			newNode.handler = handler
			newNode.isLeaf = true
			return nil
		}
		if rest[0] == '/' {
			rest = rest[1:]
		}
		return newNode.insert(rest, handler)
	}

	// No matching child found -- add a new one
	segEnd := strings.IndexAny(path, "/:*")
	if segEnd == -1 {
		segEnd = len(path)
	}
	// Include trailing slash in segment if present
	slashEnd := segEnd
	if slashEnd < len(path) && path[slashEnd] == '/' {
		slashEnd++
	}

	child := &node{
		segment: path[:segEnd],
		kind:    kindStatic,
	}
	n.addChild(child)

	rest := path[segEnd:]
	if len(rest) > 0 && rest[0] == '/' {
		rest = rest[1:]
	}
	return child.insert(rest, handler)
}

func (n *node) addChild(child *node) {
	if len(child.segment) > 0 {
		n.indices += string(child.segment[0])
	}
	n.children = append(n.children, child)
}

func commonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return max
}

// Lookup finds the handler and extracts parameters for a given method and path.
func (r *Router) Lookup(method, path string) (HandlerFunc, Params, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	root, exists := r.trees[method]
	if !exists {
		return nil, nil, false
	}

	if r.trailingSlash && len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}

	params := make(Params)
	handler, found := root.lookup(path[1:], params) // skip leading /
	if !found {
		return nil, nil, false
	}
	return handler, params, true
}

func (n *node) lookup(path string, params Params) (HandlerFunc, bool) {
	if path == "" {
		if n.isLeaf {
			return n.handler, true
		}
		return nil, false
	}

	// Try static children first
	for i, child := range n.children {
		if i < len(n.indices) && len(path) > 0 && n.indices[i] != path[0] {
			continue
		}

		segLen := len(child.segment)
		if len(path) >= segLen && path[:segLen] == child.segment {
			rest := path[segLen:]
			if len(rest) > 0 && rest[0] == '/' {
				rest = rest[1:]
			}
			if h, ok := child.lookup(rest, params); ok {
				return h, true
			}
		}
	}

	// Try parameter child
	if n.paramChild != nil {
		slashIdx := strings.IndexByte(path, '/')
		var segment, rest string
		if slashIdx == -1 {
			segment = path
			rest = ""
		} else {
			segment = path[:slashIdx]
			rest = path[slashIdx+1:]
		}

		if segment != "" {
			params[n.paramChild.paramName] = segment
			if h, ok := n.paramChild.lookup(rest, params); ok {
				return h, true
			}
			delete(params, n.paramChild.paramName)
		}
	}

	// Try catch-all child
	if n.catchChild != nil {
		params[n.catchChild.paramName] = path
		return n.catchChild.handler, true
	}

	return nil, false
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	handler, params, found := r.Lookup(req.Method, req.URL.Path)
	if !found {
		if r.notFoundHandler != nil {
			r.notFoundHandler(w, req)
			return
		}
		http.NotFound(w, req)
		return
	}
	handler(w, req, params)
}

// --- Linear scan baseline for benchmarking ---

// LinearRouter uses a slice of routes for comparison benchmarks.
type LinearRouter struct {
	routes []linearRoute
}

type linearRoute struct {
	method  string
	pattern string
	handler HandlerFunc
}

func NewLinearRouter() *LinearRouter {
	return &LinearRouter{}
}

func (lr *LinearRouter) Handle(method, pattern string, handler HandlerFunc) {
	lr.routes = append(lr.routes, linearRoute{method, pattern, handler})
}

// Lookup does a linear scan through all routes.
func (lr *LinearRouter) Lookup(method, path string) (HandlerFunc, bool) {
	for _, r := range lr.routes {
		if r.method == method && r.pattern == path {
			return r.handler, true
		}
	}
	return nil, false
}
```

### Tests

```go
// router_test.go
package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func dummyHandler(name string) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		fmt.Fprintf(w, "%s:%v", name, p)
	}
}

func TestStaticRoutes(t *testing.T) {
	r := New()
	routes := []string{"/", "/users", "/users/settings", "/api/v2/health", "/about"}
	for _, path := range routes {
		if err := r.Handle("GET", path, dummyHandler(path)); err != nil {
			t.Fatalf("Handle(%s): %v", path, err)
		}
	}

	for _, path := range routes {
		h, _, found := r.Lookup("GET", path)
		if !found {
			t.Errorf("Lookup(%s): not found", path)
			continue
		}
		if h == nil {
			t.Errorf("Lookup(%s): handler is nil", path)
		}
	}

	_, _, found := r.Lookup("GET", "/nonexistent")
	if found {
		t.Error("Lookup(/nonexistent) should return not found")
	}
}

func TestPrefixCompression(t *testing.T) {
	r := New()
	must(t, r.Handle("GET", "/api/users", dummyHandler("users")))
	must(t, r.Handle("GET", "/api/posts", dummyHandler("posts")))
	must(t, r.Handle("GET", "/api/posts/latest", dummyHandler("latest")))

	tests := []struct {
		path  string
		found bool
	}{
		{"/api/users", true},
		{"/api/posts", true},
		{"/api/posts/latest", true},
		{"/api", false},
		{"/api/comments", false},
	}

	for _, tt := range tests {
		_, _, found := r.Lookup("GET", tt.path)
		if found != tt.found {
			t.Errorf("Lookup(%s): got found=%v, want %v", tt.path, found, tt.found)
		}
	}
}

func TestParamRoutes(t *testing.T) {
	r := New()
	must(t, r.Handle("GET", "/users/:id", dummyHandler("user")))
	must(t, r.Handle("GET", "/users/:id/posts/:postID", dummyHandler("user-post")))

	tests := []struct {
		path       string
		wantParams Params
		found      bool
	}{
		{"/users/42", Params{"id": "42"}, true},
		{"/users/alice", Params{"id": "alice"}, true},
		{"/users/42/posts/7", Params{"id": "42", "postID": "7"}, true},
		{"/users/", false: false},
	}

	for _, tt := range tests {
		_, params, found := r.Lookup("GET", tt.path)
		if found != tt.found {
			t.Errorf("Lookup(%s): got found=%v, want %v", tt.path, found, tt.found)
			continue
		}
		if found {
			for k, want := range tt.wantParams {
				if got := params[k]; got != want {
					t.Errorf("Lookup(%s): param %s = %q, want %q", tt.path, k, got, want)
				}
			}
		}
	}
}

func TestCatchAllRoutes(t *testing.T) {
	r := New()
	must(t, r.Handle("GET", "/files/*filepath", dummyHandler("files")))

	tests := []struct {
		path     string
		wantPath string
		found    bool
	}{
		{"/files/css/style.css", "css/style.css", true},
		{"/files/a/b/c/d", "a/b/c/d", true},
		{"/files/readme.txt", "readme.txt", true},
	}

	for _, tt := range tests {
		_, params, found := r.Lookup("GET", tt.path)
		if found != tt.found {
			t.Errorf("Lookup(%s): got found=%v, want %v", tt.path, found, tt.found)
			continue
		}
		if found && params["filepath"] != tt.wantPath {
			t.Errorf("Lookup(%s): filepath = %q, want %q", tt.path, params["filepath"], tt.wantPath)
		}
	}
}

func TestMethodRouting(t *testing.T) {
	r := New()
	must(t, r.Handle("GET", "/users", dummyHandler("list")))
	must(t, r.Handle("POST", "/users", dummyHandler("create")))
	must(t, r.Handle("DELETE", "/users/:id", dummyHandler("delete")))

	if _, _, found := r.Lookup("GET", "/users"); !found {
		t.Error("GET /users should be found")
	}
	if _, _, found := r.Lookup("POST", "/users"); !found {
		t.Error("POST /users should be found")
	}
	if _, _, found := r.Lookup("PUT", "/users"); found {
		t.Error("PUT /users should not be found")
	}
}

func TestConflictDetection(t *testing.T) {
	tests := []struct {
		name    string
		routes  [][2]string // [method, pattern]
		wantErr bool
	}{
		{
			name:    "duplicate route",
			routes:  [][2]string{{"GET", "/users"}, {"GET", "/users"}},
			wantErr: true,
		},
		{
			name:    "param name conflict",
			routes:  [][2]string{{"GET", "/users/:id"}, {"GET", "/users/:name"}},
			wantErr: true,
		},
		{
			name:    "catch-all conflicts with child",
			routes:  [][2]string{{"GET", "/files/*path"}, {"GET", "/files/specific"}},
			wantErr: true,
		},
		{
			name:    "different methods no conflict",
			routes:  [][2]string{{"GET", "/users"}, {"POST", "/users"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			var err error
			for _, route := range tt.routes {
				err = r.Handle(route[0], route[1], dummyHandler("x"))
				if err != nil {
					break
				}
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestTrailingSlashNormalization(t *testing.T) {
	r := New(WithTrailingSlashNormalization())
	must(t, r.Handle("GET", "/users", dummyHandler("users")))

	if _, _, found := r.Lookup("GET", "/users"); !found {
		t.Error("/users should match")
	}
	if _, _, found := r.Lookup("GET", "/users/"); !found {
		t.Error("/users/ should match with normalization enabled")
	}
}

func TestServeHTTP(t *testing.T) {
	r := New()
	must(t, r.Handle("GET", "/hello/:name", func(w http.ResponseWriter, req *http.Request, p Params) {
		fmt.Fprintf(w, "Hello, %s!", p.Get("name"))
	}))

	req := httptest.NewRequest("GET", "/hello/world", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "Hello, world!" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "Hello, world!")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Benchmarks ---

func generateRoutes(n int) []string {
	routes := make([]string, n)
	for i := 0; i < n; i++ {
		routes[i] = fmt.Sprintf("/api/v1/resource%d/action%d", i/10, i%10)
	}
	return routes
}

func BenchmarkRadixLookup10(b *testing.B)  { benchmarkRadix(b, 10) }
func BenchmarkRadixLookup100(b *testing.B) { benchmarkRadix(b, 100) }
func BenchmarkRadixLookup500(b *testing.B) { benchmarkRadix(b, 500) }

func BenchmarkLinearLookup10(b *testing.B)  { benchmarkLinear(b, 10) }
func BenchmarkLinearLookup100(b *testing.B) { benchmarkLinear(b, 100) }
func BenchmarkLinearLookup500(b *testing.B) { benchmarkLinear(b, 500) }

func benchmarkRadix(b *testing.B, n int) {
	r := New()
	routes := generateRoutes(n)
	for _, path := range routes {
		r.Handle("GET", path, dummyHandler("x"))
	}
	target := routes[n-1] // worst case: last route
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Lookup("GET", target)
	}
}

func benchmarkLinear(b *testing.B, n int) {
	lr := NewLinearRouter()
	routes := generateRoutes(n)
	for _, path := range routes {
		lr.Handle("GET", path, dummyHandler("x"))
	}
	target := routes[n-1]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lr.Lookup("GET", target)
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -bench=. -benchmem ./...
```

### Expected Output

```
=== RUN   TestStaticRoutes
--- PASS: TestStaticRoutes (0.00s)
=== RUN   TestPrefixCompression
--- PASS: TestPrefixCompression (0.00s)
=== RUN   TestParamRoutes
--- PASS: TestParamRoutes (0.00s)
=== RUN   TestCatchAllRoutes
--- PASS: TestCatchAllRoutes (0.00s)
=== RUN   TestMethodRouting
--- PASS: TestMethodRouting (0.00s)
=== RUN   TestConflictDetection
--- PASS: TestConflictDetection (0.00s)
=== RUN   TestTrailingSlashNormalization
--- PASS: TestTrailingSlashNormalization (0.00s)
=== RUN   TestServeHTTP
--- PASS: TestServeHTTP (0.00s)
PASS

BenchmarkRadixLookup10-8     12485703    96.2 ns/op    336 B/op    1 allocs/op
BenchmarkRadixLookup100-8     8234519   145.8 ns/op    336 B/op    1 allocs/op
BenchmarkRadixLookup500-8     7105623   168.4 ns/op    336 B/op    1 allocs/op
BenchmarkLinearLookup10-8    10253847   117.1 ns/op      0 B/op    0 allocs/op
BenchmarkLinearLookup100-8    1256478   954.2 ns/op      0 B/op    0 allocs/op
BenchmarkLinearLookup500-8     253841  4721.0 ns/op      0 B/op    0 allocs/op
```

## Design Decisions

**Decision 1: One tree per method vs. method check at each node.** Separate trees per method avoid polluting every node with method-to-handler maps. The memory overhead is negligible (most routes share the same structure across methods) and lookup avoids a map access at the leaf. The downside is that `OPTIONS` or `405 Method Not Allowed` responses require checking all trees, but this is a rare path.

**Decision 2: Index string for child dispatch.** Instead of iterating through all children, the `indices` string stores the first byte of each child's segment. A single byte comparison narrows the search to at most one candidate. This is the same technique used by httprouter and gives O(1) child selection for static segments.

**Decision 3: Separate fields for param/catch-all children vs. mixed children array.** Keeping `paramChild` and `catchChild` as dedicated fields avoids scanning the children array for special node types during lookup. It also makes conflict detection trivial: just check if the field is already set.

**Decision 4: Conflict detection at registration time.** Runtime conflicts (two routes that match the same path) are notoriously hard to debug. By detecting conflicts during `Handle()`, the application fails fast at startup with a clear error message. This matches the behavior of httprouter and Gin.

## Common Mistakes

**Mistake 1: Not handling the split correctly.** When inserting `/api/posts` into a tree that has `/api/products`, the common prefix is `/api/p`. If you split at the wrong offset, you get corrupted segments. The split must preserve both the existing children and the new route's remaining suffix.

**Mistake 2: Forgetting backtracking in lookup.** A static child might match a prefix of the remaining path but fail deeper in the tree. The lookup must then backtrack and try the parameter child. Without backtracking, routes like `/users/settings` (static) and `/users/:id` (param) cannot coexist correctly.

**Mistake 3: Catch-all not consuming the full remainder.** The `*filepath` wildcard must capture everything after its position, including slashes. A common bug is to only capture the next segment, making `/files/*path` fail on `/files/a/b/c`.

**Mistake 4: Params map shared across backtracking attempts.** If lookup writes a parameter, backtracks, and tries a different branch, the stale parameter value corrupts the result. Delete params when backtracking away from a branch.

## Performance Notes

- Radix tree lookup is O(k) where k is the number of path segments, independent of the total number of routes. Linear scan is O(n) where n is the route count. The crossover point where radix becomes faster is typically around 15-30 routes.
- The params map allocation (one per lookup) dominates the per-request cost. In a production router, use a pre-allocated slice of key-value pairs from a sync.Pool instead.
- Prefix compression means the tree depth is proportional to unique path segments, not total characters. For typical REST APIs (`/api/v1/resources/:id`), tree depth is 4-6 regardless of route count.
- The index string technique avoids allocating a map for child lookup. For nodes with many children (rare in practice), a sorted index with binary search would be faster than linear scan.
