<!--
difficulty: advanced
concepts: go-ast, go-parser, go-token, syntax-tree, static-analysis
tools: go/ast, go/parser, go/token
estimated_time: 40m
bloom_level: analyzing
prerequisites: go-generate-basics, writing-a-custom-code-generator, interfaces, reflection
-->

# Exercise 29.4: AST Parsing

## Prerequisites

Before starting this exercise, you should be comfortable with:

- `//go:generate` directives (Exercise 29.1)
- Writing custom code generators (Exercise 29.3)
- Interfaces and type assertions
- Basic understanding of tree data structures

## Learning Objectives

By the end of this exercise, you will be able to:

1. Parse Go source files into an Abstract Syntax Tree using `go/parser`
2. Navigate the AST to find type declarations, functions, and constants
3. Use `go/token` to track source positions
4. Extract structural information from Go code programmatically

## Why This Matters

AST parsing is the foundation of all serious Go tooling -- linters, code generators, refactoring tools, and IDE features all parse the AST. Understanding `go/ast` lets you build generators that automatically discover types, struct fields, and constants from source code rather than requiring manual configuration.

---

## Problem

Build a Go program that parses a Go source file and extracts:

1. All exported type declarations (structs and interfaces)
2. All struct fields with their types and tags
3. All constant groups with their values
4. All exported functions with their signatures

### Hints

- `parser.ParseFile` returns an `*ast.File` which is the root of the AST
- Use `ast.Inspect` to walk the tree depth-first
- Type assertions like `node.(*ast.TypeSpec)` let you handle specific node types
- `ast.GenDecl` wraps groups of declarations (const, var, type, import)
- Field tags are in `field.Tag.Value` and include the backtick delimiters

### Step 1: Create the project

```bash
mkdir -p ast-parsing && cd ast-parsing
go mod init ast-parsing
```

### Step 2: Create a sample file to parse

Create `sample/models.go`:

```go
package sample

import "time"

// User represents a registered user.
type User struct {
	ID        int       `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Email     string    `json:"email" db:"email"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	isActive  bool      // unexported field
}

// Role defines permission levels.
type Role int

const (
	RoleViewer Role = iota
	RoleEditor
	RoleAdmin
	RoleOwner
)

// Repository defines data access operations.
type Repository interface {
	FindByID(id int) (*User, error)
	Save(user *User) error
	Delete(id int) error
}

// NewUser creates a User with defaults.
func NewUser(name, email string) *User {
	return &User{
		Name:      name,
		Email:     email,
		CreatedAt: time.Now(),
	}
}

// Validate checks if the user data is valid.
func (u *User) Validate() error {
	return nil
}
```

### Step 3: Write the AST parser

Create `main.go`:

```go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ast-parsing <file.go>")
		os.Exit(1)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Package: %s\n\n", file.Name.Name)

	// Walk all top-level declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			handleGenDecl(fset, d)
		case *ast.FuncDecl:
			handleFuncDecl(fset, d)
		}
	}
}

func handleGenDecl(fset *token.FileSet, decl *ast.GenDecl) {
	switch decl.Tok {
	case token.TYPE:
		for _, spec := range decl.Specs {
			ts := spec.(*ast.TypeSpec)
			pos := fset.Position(ts.Pos())
			switch t := ts.Type.(type) {
			case *ast.StructType:
				fmt.Printf("Struct: %s (line %d)\n", ts.Name.Name, pos.Line)
				for _, field := range t.Fields.List {
					for _, name := range field.Names {
						exported := ast.IsExported(name.Name)
						tag := ""
						if field.Tag != nil {
							tag = field.Tag.Value
						}
						fmt.Printf("  field: %s type=%s exported=%v tag=%s\n",
							name.Name, exprString(field.Type), exported, tag)
					}
				}
				fmt.Println()
			case *ast.InterfaceType:
				fmt.Printf("Interface: %s (line %d)\n", ts.Name.Name, pos.Line)
				for _, method := range t.Methods.List {
					for _, name := range method.Names {
						fmt.Printf("  method: %s\n", name.Name)
					}
				}
				fmt.Println()
			default:
				fmt.Printf("Type: %s = %s (line %d)\n\n", ts.Name.Name, exprString(t), pos.Line)
			}
		}
	case token.CONST:
		fmt.Println("Constants:")
		for _, spec := range decl.Specs {
			vs := spec.(*ast.ValueSpec)
			for _, name := range vs.Names {
				pos := fset.Position(name.Pos())
				fmt.Printf("  %s (line %d)\n", name.Name, pos.Line)
			}
		}
		fmt.Println()
	}
}

func handleFuncDecl(fset *token.FileSet, decl *ast.FuncDecl) {
	pos := fset.Position(decl.Pos())
	receiver := ""
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		receiver = fmt.Sprintf("(%s) ", exprString(decl.Recv.List[0].Type))
	}

	params := fieldListString(decl.Type.Params)
	results := fieldListString(decl.Type.Results)

	fmt.Printf("Func: %s%s(%s)", receiver, decl.Name.Name, params)
	if results != "" {
		fmt.Printf(" (%s)", results)
	}
	fmt.Printf(" (line %d)\n\n", pos.Line)
}

func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	case *ast.ArrayType:
		return "[]" + exprString(e.Elt)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func fieldListString(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	var parts []string
	for _, f := range fl.List {
		typeName := exprString(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, typeName)
		} else {
			for _, name := range f.Names {
				parts = append(parts, name.Name+" "+typeName)
			}
		}
	}
	return strings.Join(parts, ", ")
}
```

### Step 4: Run the parser

```bash
go run . sample/models.go
```

Expected output:

```
Package: sample

Struct: User (line 6)
  field: ID type=int exported=true tag=`json:"id" db:"id"`
  field: Name type=string exported=true tag=`json:"name" db:"name"`
  field: Email type=string exported=true tag=`json:"email" db:"email"`
  field: CreatedAt type=time.Time exported=true tag=`json:"created_at" db:"created_at"`
  field: isActive type=bool exported=false tag=

Type: Role = int (line 15)

Constants:
  RoleViewer (line 18)
  RoleEditor (line 19)
  RoleAdmin (line 20)
  RoleOwner (line 21)

Interface: Repository (line 25)
  method: FindByID
  method: Save
  method: Delete

Func: NewUser(name string, email string) (*User) (line 32)

Func: (*User) Validate() (error) (line 40)
```

---

## Verify

Run the parser on its own source code:

```bash
go run . main.go
```

The parser should successfully analyze itself, listing the `main` function and all helper functions. This demonstrates that the AST parser works on any valid Go source.

---

## What's Next

In the next exercise, you will combine AST parsing with `text/template` to build template-based code generators that automatically discover types from source files.

## Summary

- `go/parser.ParseFile` produces an `*ast.File` from Go source
- `go/token.FileSet` tracks source file positions for error reporting
- `ast.GenDecl` covers `type`, `const`, `var`, and `import` declarations
- `ast.FuncDecl` represents function/method declarations including receivers
- Type assertions on AST nodes (`*ast.StructType`, `*ast.InterfaceType`) enable type-specific handling
- `ast.IsExported` checks if an identifier starts with an uppercase letter

## Reference

- [go/ast package](https://pkg.go.dev/go/ast)
- [go/parser package](https://pkg.go.dev/go/parser)
- [go/token package](https://pkg.go.dev/go/token)
- [Go AST Viewer (online tool)](https://yuroyoro.github.io/goast-viewer/)
