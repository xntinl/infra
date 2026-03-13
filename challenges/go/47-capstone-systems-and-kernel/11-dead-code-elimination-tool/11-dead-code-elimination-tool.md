<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 20h
-->

# Dead Code Elimination Tool

## The Challenge

Build a static analysis tool in Go that detects and optionally removes dead code -- functions, methods, types, constants, variables, and struct fields that are declared but never used -- across an entire Go project including all its packages. Unlike the compiler's simple "declared and not used" check for local variables, your tool must perform whole-program analysis to find exported functions that are never called, interface methods that are never invoked, struct fields that are never read or written, and types that are never instantiated. The tool must understand Go's reflection, interface satisfaction, init functions, test files, and build tags to avoid false positives, and must produce both a report and optional automatic code rewrites that safely remove dead code.

## Requirements

1. Load the entire Go project using `golang.org/x/tools/go/packages` with `packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedDeps` to obtain the complete AST, type information, and dependency graph for all packages.
2. Build a call graph using `golang.org/x/tools/go/callgraph` with the RTA (Rapid Type Analysis) or VTA (Variable Type Analysis) algorithm that tracks which functions are reachable from the program's entry points (`main`, `init`, and `TestXxx` functions).
3. Identify dead functions: any function (including methods) that is not reachable in the call graph from any entry point is dead; exclude functions that satisfy an interface (they may be called via interface dispatch even if no direct call exists in the source).
4. Identify dead types: types that are never referenced in any live code (not used as a variable type, parameter type, return type, embedded type, or type assertion target) are dead; handle type aliases and named types correctly.
5. Identify dead struct fields: fields that are never read (no selector expression `x.Field`) and never written (no assignment to `x.Field` or composite literal `T{Field: v}`) in any live code; handle embedded structs, JSON/protobuf tags (fields with struct tags are conservatively kept alive), and reflection (`reflect.Value.FieldByName`).
6. Handle false positive avoidance: never report `main`, `init`, `TestXxx`/`BenchmarkXxx`/`ExampleXxx` functions, functions referenced via `reflect`, functions used in `//go:linkname` directives, functions in generated code (files with `// Code generated` header), or exported functions in library packages (non-main packages) as dead.
7. Produce a detailed report listing each dead code entity with its file path, line number, entity type (function/type/field/constant/variable), and the reason it was classified as dead; support JSON and text output formats.
8. Implement automatic rewriting: when invoked with `--fix`, use `go/ast` rewriting to remove dead entities from the source files, then format the result with `go/format` and write it back; handle cascading removals (removing a function may make its helper functions dead too) by iterating until a fixed point.

## Hints

- `golang.org/x/tools/go/callgraph/rta` builds the most accurate call graph for whole-program analysis; it requires an SSA (static single assignment) representation built via `golang.org/x/tools/go/ssa`.
- For struct field analysis, walk all `ast.SelectorExpr` nodes in live functions and record which fields are accessed; any field not in this set is dead.
- Interface satisfaction check: use `types.Implements(typ, iface)` to check if a type satisfies an interface; if it does, all methods required by the interface are implicitly alive if the interface is alive.
- Reflection usage detection: scan for calls to `reflect.TypeOf`, `reflect.ValueOf`, `reflect.Value.MethodByName`, `reflect.Value.FieldByName` -- if any argument's type can be statically determined, mark all methods/fields of that type as alive.
- Struct tags: check if a field has any struct tag using `field.Tag`; fields with tags are conservatively treated as alive (they may be accessed by encoding/json, encoding/xml, etc.).
- `//go:linkname` directives link Go functions to arbitrary symbols; parse comments for this directive and mark referenced functions as alive.
- For cascading removal with `--fix`: after removing dead code, re-analyze the project, find new dead code, remove it, repeat until no new dead code is found.
- Handle build tags by loading the project with the union of all build tag configurations, or accept the active configuration as a parameter.

## Success Criteria

1. On a test project with known dead functions, the tool correctly identifies all dead functions and reports zero false positives.
2. Exported functions in a `main` package that are never called are correctly identified as dead.
3. Methods that satisfy an interface used in live code are correctly kept alive (no false positives on interface methods).
4. Dead struct fields are correctly identified, but fields with struct tags are conservatively kept alive.
5. The `--fix` mode correctly removes dead code and the resulting project compiles and all tests pass.
6. Cascading removal: removing a dead function that was the only caller of a helper correctly identifies the helper as dead in the next iteration.
7. The tool handles a real-world project (e.g., a 50-package codebase) and completes analysis within 30 seconds.
8. JSON output is machine-parseable and includes file paths, line numbers, and entity descriptions for integration with CI pipelines.

## Research Resources

- `golang.org/x/tools/go/packages` -- https://pkg.go.dev/golang.org/x/tools/go/packages
- `golang.org/x/tools/go/ssa` -- static single assignment form for Go
- `golang.org/x/tools/go/callgraph/rta` -- Rapid Type Analysis call graph construction
- `golang.org/x/tools/go/callgraph/vta` -- Variable Type Analysis
- "Practical Whole-Program Analysis of Go Programs" -- https://pkg.go.dev/golang.org/x/tools/go/analysis
- Go `go/ast` package for AST manipulation and rewriting
- Go `go/format` package for formatting rewritten code
- `staticcheck` and `unused` tools -- https://staticcheck.io/ -- inspiration for dead code detection
