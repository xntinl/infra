<!--
difficulty: insane
concepts: ast-rewriting, go-ast, go-printer, go-types, source-transformation, refactoring
tools: go/ast, go/parser, go/token, go/printer, go/format, golang.org/x/tools/go/ast/astutil
estimated_time: 90m
bloom_level: creating
prerequisites: ast-parsing, template-based-code-generation, building-a-cli-code-generator, interfaces
-->

# Exercise 29.10: AST Rewriting Tool

## Prerequisites

Before starting this exercise, you should be comfortable with:

- AST parsing with `go/ast` and `go/parser` (Exercise 29.4)
- Building CLI code generators (Exercise 29.9)
- Interfaces and type assertions
- Tree traversal and manipulation

## Learning Objectives

By the end of this exercise, you will be able to:

1. Modify an AST in place by replacing, inserting, and removing nodes
2. Use `go/printer` to render a modified AST back to source code preserving formatting
3. Apply `golang.org/x/tools/go/ast/astutil` for cursor-based AST traversal and rewriting
4. Build a refactoring tool that performs source-to-source transformations

## Why This Matters

AST rewriting is the technique behind `gofmt`, `gorename`, `goimports`, and the refactoring features in `gopls`. Unlike code generation (which creates new files), AST rewriting transforms existing source code in place. This is the most powerful form of Go tooling -- it enables automated refactoring, codemod migrations, and style enforcement across entire codebases.

---

## Challenge

Build a CLI tool called `gomod` (Go modifier) that performs automated source-to-source transformations on Go files. The tool must support multiple rewrite rules, preserve comments and formatting, and operate safely on real codebases.

### Requirements

1. **CLI Interface**:
   - `-input` -- path to the Go source file (or directory for recursive mode)
   - `-rule` -- the rewrite rule to apply (see below)
   - `-dry-run` -- print the rewritten source to stdout instead of modifying the file
   - `-diff` -- show a unified diff of changes instead of writing
   - `-recursive` -- process all `.go` files in the directory tree

2. **Rewrite Rules** (implement all four):

   **a) `wrap-errors`** -- Wrap bare `fmt.Errorf` calls with `%w` verb:
   - Find `fmt.Errorf("...: %v", ..., err)` patterns
   - Rewrite to `fmt.Errorf("...: %w", ..., err)` when the last argument is named `err`
   - Only transform when the format string's last verb is `%v` and the last argument is an `error`-typed identifier

   **b) `add-context`** -- Add context.Context as the first parameter to functions that call other context-accepting functions but do not themselves accept a context:
   - Scan function bodies for calls that pass `context.TODO()` or `context.Background()`
   - Add `ctx context.Context` as the first parameter
   - Replace `context.TODO()` / `context.Background()` with `ctx` in the body
   - Add `"context"` to the import block if not already present

   **c) `rename-field`** -- Rename a struct field and all its references:
   - Accept `-from=OldName` and `-to=NewName` additional flags
   - Find the struct field declaration and rename it
   - Find all selector expressions (`x.OldName`) that reference the field and rename them
   - Update struct literal field names (`OldName: value` becomes `NewName: value`)

   **d) `add-method-doc`** -- Add missing doc comments to exported methods:
   - Find exported methods without a doc comment
   - Generate a comment in the form `// MethodName <verb>s ...` using the receiver type
   - For example: `func (s *Server) Start()` gets `// Start starts the Server.`

3. **Output Quality**:
   - Rewritten source must preserve original comments, blank lines, and formatting
   - Use `go/printer.Fprint` with `printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}`
   - Output must pass `go vet` and `gofmt -d` (no diff)

4. **Safety**:
   - In non-dry-run mode, write to a `.tmp` file first, then rename atomically
   - Never modify files that would not compile after the transformation
   - Report the number of changes made per file

### Success Criteria

- [ ] `gomod -input=example.go -rule=wrap-errors -dry-run` rewrites `%v` to `%w` in error wrapping patterns and prints the result
- [ ] `gomod -input=example.go -rule=add-context -dry-run` inserts `ctx context.Context` parameters and replaces `context.TODO()` calls
- [ ] `gomod -input=example.go -rule=rename-field -from=Name -to=FullName -dry-run` renames the field and all references
- [ ] `gomod -input=example.go -rule=add-method-doc -dry-run` adds doc comments to undocumented exported methods
- [ ] All rewritten files pass `go vet` and compile cleanly
- [ ] Comments, blank lines, and non-target code are preserved exactly
- [ ] `-diff` mode produces a readable unified diff
- [ ] The tool reports how many transformations were applied

### Test Input

Create `example.go` to test all rules against:

```go
package example

import (
	"context"
	"database/sql"
	"fmt"
)

type Server struct {
	Name string
	Port int
	db   *sql.DB
}

func NewServer(name string, port int) (*Server, error) {
	db, err := sql.Open("postgres", "")
	if err != nil {
		return nil, fmt.Errorf("open db: %v", err)
	}
	return &Server{Name: name, Port: port, db: db}, nil
}

func (s *Server) Start() {
	fmt.Printf("Starting %s on :%d\n", s.Name, s.Port)
}

func (s *Server) Ping() error {
	return s.db.PingContext(context.TODO())
}

func (s *Server) Query(q string) (*sql.Rows, error) {
	rows, err := s.db.QueryContext(context.Background(), q)
	if err != nil {
		return nil, fmt.Errorf("query failed: %v", err)
	}
	return rows, nil
}

func copyServer(src *Server) *Server {
	return &Server{
		Name: src.Name,
		Port: src.Port,
	}
}
```

### Suggested Architecture

```
gomod/
  main.go                -- flag parsing, rule dispatch
  internal/
    rewriter/
      rewriter.go        -- shared AST load/save/diff logic
    rules/
      wrap_errors.go     -- wrap-errors rule
      add_context.go     -- add-context rule
      rename_field.go    -- rename-field rule
      add_method_doc.go  -- add-method-doc rule
```

### Key Implementation Notes

- Use `astutil.Apply` from `golang.org/x/tools/go/ast/astutil` for the pre/post cursor pattern -- it is safer than mutating nodes during `ast.Inspect`
- To add imports, use `astutil.AddImport(fset, file, "context")`
- `go/printer` preserves the original token positions stored in the AST; modifying positions incorrectly will corrupt formatting
- For `-diff`, shell out to `diff -u` or implement a simple line-based diff
- String analysis of `fmt.Errorf` format strings requires extracting the string literal from `ast.BasicLit` and parsing the verb sequence

---

## Verify

Test each rule independently:

```bash
go run ./gomod -input=example.go -rule=wrap-errors -dry-run > /dev/null && echo "wrap-errors: OK"
go run ./gomod -input=example.go -rule=add-context -dry-run > /dev/null && echo "add-context: OK"
go run ./gomod -input=example.go -rule=rename-field -from=Name -to=FullName -dry-run > /dev/null && echo "rename-field: OK"
go run ./gomod -input=example.go -rule=add-method-doc -dry-run > /dev/null && echo "add-method-doc: OK"
```

Then verify the output compiles:

```bash
go run ./gomod -input=example.go -rule=wrap-errors -dry-run > /tmp/rewritten.go
cd /tmp && go vet rewritten.go
```

All rules should produce valid, compilable Go source.

---

## Summary

- AST rewriting transforms existing source code in place, preserving comments and formatting
- `go/printer` renders a modified AST back to source; `astutil.Apply` provides safe cursor-based traversal
- `astutil.AddImport` and `astutil.DeleteImport` manage import blocks correctly
- Always validate that rewritten code compiles before overwriting the original file
- Atomic file writes (write to `.tmp`, then rename) prevent data loss on failure

## Reference

- [go/ast package](https://pkg.go.dev/go/ast)
- [go/printer package](https://pkg.go.dev/go/printer)
- [golang.org/x/tools/go/ast/astutil](https://pkg.go.dev/golang.org/x/tools/go/ast/astutil)
- [go/format package](https://pkg.go.dev/go/format)
- [gofmt source code](https://cs.opensource.google/go/go/+/master:src/cmd/gofmt/) -- canonical AST rewriter
- [gopls refactoring](https://github.com/golang/tools/tree/master/gopls) -- production AST rewriting
