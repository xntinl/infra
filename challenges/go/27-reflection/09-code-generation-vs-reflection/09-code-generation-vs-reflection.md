# 9. Code Generation vs Reflection

<!--
difficulty: insane
concepts: [go-generate, ast-parsing, template-codegen, reflection-alternative, compile-time-safety, stringer]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [building-a-simple-orm, reflection-performance-costs, go-ast-package, go-generate]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 1-8 in this section or equivalent reflection experience
- Familiarity with `go generate` and `text/template`
- Basic knowledge of `go/ast` and `go/parser` (reading Go source programmatically)

## Learning Objectives

After completing this challenge, you will be able to:

- **Build** a code generator that reads Go struct definitions and produces type-safe serialization code
- **Compare** reflection-based and code-generated approaches on correctness, performance, and maintainability
- **Use** `go/ast`, `go/parser`, and `text/template` to create a practical `go generate` tool
- **Evaluate** the tradeoffs to decide which approach fits a given problem

## The Challenge

Build two implementations of the same functionality -- a struct-to-JSON marshaler and a JSON-to-struct unmarshaler -- one using reflection at runtime and one using code generation at build time. Then benchmark them head-to-head to quantify the performance difference and analyze the developer-experience tradeoffs.

The reflection version is straightforward: iterate struct fields with `reflect.Type`, read `json` tags, build the JSON output with `reflect.Value`. You built similar code in previous exercises.

The code generation version is harder. You will build a command-line tool that:
1. Parses Go source files using `go/parser` to find structs annotated with a `//go:generate` comment
2. Extracts field names, types, and `json` struct tags from the AST
3. Uses `text/template` to emit a `_json.go` file containing hand-unrolled marshal/unmarshal functions for each struct -- no reflection at runtime

The generated code should look like what a human would write: a `MarshalJSON` method that builds the JSON byte-by-byte using `strconv.AppendInt`, `strconv.AppendQuote`, etc., and an `UnmarshalJSON` method that uses a state machine or `json.Decoder` to populate fields directly.

The crux of the challenge is the AST parsing. You must handle: named types vs built-in types, pointer fields (nullable), embedded structs, slice fields, and nested struct fields. For each Go type you encounter, your template must emit the correct serialization logic.

## Requirements

1. Build a `jsongen` CLI tool that accepts a Go source file path and a list of type names, then generates a `_json_gen.go` file containing `MarshalJSON() ([]byte, error)` and `UnmarshalJSON([]byte) error` methods for each specified type

2. The generator must parse Go source using `go/ast` and `go/parser` -- do not use regular expressions to extract struct definitions

3. Handle these field types in code generation: `string`, `int`/`int64`, `float64`, `bool`, `time.Time`, `*T` (pointers), `[]T` (slices), and nested structs

4. Respect `json` struct tags: custom names (`json:"user_name"`), `omitempty`, and `json:"-"` (skip)

5. Build the equivalent reflection-based `MarshalJSON` and `UnmarshalJSON` as a separate package for comparison

6. Write a comprehensive benchmark suite comparing: encoding/json (stdlib), your reflection version, and your code-generated version -- for structs with 5, 15, and 30 fields

7. The generated code must compile without modification -- run `go vet` and `go build` as part of your test suite to verify

8. Include a `//go:generate` directive in a sample file that invokes your `jsongen` tool, so `go generate ./...` produces the generated file

9. Write a correctness test that marshals with the generated code, unmarshals with `encoding/json` (and vice versa), and verifies round-trip equality for edge cases: empty strings, zero values, nil pointers, empty slices, Unicode, and nested structs

## Hints

<details>
<summary>Hint 1: Parsing Structs from AST</summary>

Use `go/parser.ParseFile` and walk the AST to find type declarations:

```go
fset := token.NewFileSet()
file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)

for _, decl := range file.Decls {
    genDecl, ok := decl.(*ast.GenDecl)
    if !ok || genDecl.Tok != token.TYPE {
        continue
    }
    for _, spec := range genDecl.Specs {
        typeSpec := spec.(*ast.TypeSpec)
        structType, ok := typeSpec.Type.(*ast.StructType)
        if !ok {
            continue
        }
        // typeSpec.Name.Name is the struct name
        // structType.Fields.List contains the fields
    }
}
```
</details>

<details>
<summary>Hint 2: Extracting Field Info from AST</summary>

Each `*ast.Field` has a `Type` (an `ast.Expr`) and optional `Tag` (a `*ast.BasicLit`). Convert the type expression to a string:

```go
func exprToString(expr ast.Expr) string {
    switch t := expr.(type) {
    case *ast.Ident:
        return t.Name // "string", "int", "MyType"
    case *ast.StarExpr:
        return "*" + exprToString(t.X)
    case *ast.ArrayType:
        return "[]" + exprToString(t.Elt)
    case *ast.SelectorExpr:
        return exprToString(t.X) + "." + t.Sel.Name // "time.Time"
    default:
        return fmt.Sprintf("%T", expr)
    }
}
```
</details>

<details>
<summary>Hint 3: Template Structure</summary>

Use `text/template` with a function map for type-specific logic:

```go
var tmpl = template.Must(template.New("json").Funcs(template.FuncMap{
    "marshalField": func(f FieldInfo) string {
        switch f.GoType {
        case "string":
            return fmt.Sprintf("buf = strconv.AppendQuote(buf, s.%s)", f.Name)
        case "int", "int64":
            return fmt.Sprintf("buf = strconv.AppendInt(buf, int64(s.%s), 10)", f.Name)
        // ...
        }
    },
}).Parse(jsonTemplate))
```
</details>

<details>
<summary>Hint 4: Reflection-Based Marshal</summary>

The reflection version iterates fields at runtime:

```go
func ReflectMarshalJSON(v interface{}) ([]byte, error) {
    var buf bytes.Buffer
    buf.WriteByte('{')
    val := reflect.ValueOf(v)
    typ := val.Type()
    first := true
    for i := 0; i < typ.NumField(); i++ {
        field := typ.Field(i)
        tag := field.Tag.Get("json")
        if tag == "-" { continue }
        name, opts := parseTag(tag)
        if name == "" { name = field.Name }
        fv := val.Field(i)
        if strings.Contains(opts, "omitempty") && fv.IsZero() { continue }
        if !first { buf.WriteByte(',') }
        fmt.Fprintf(&buf, "%q:", name)
        writeValue(&buf, fv)
        first = false
    }
    buf.WriteByte('}')
    return buf.Bytes(), nil
}
```
</details>

## Success Criteria

1. `go generate ./...` produces a valid `_json_gen.go` file that compiles and passes `go vet`

2. Generated `MarshalJSON` output is byte-for-byte identical to `encoding/json.Marshal` for the same struct values (excluding whitespace differences if the stdlib is configured to indent)

3. Round-trip correctness: marshal with generated code, unmarshal with `encoding/json`, compare with `reflect.DeepEqual` -- passes for all edge cases including nil pointers and empty slices

4. Benchmarks show the generated code is at least 3-5x faster than `encoding/json` and at least 2x faster than your reflection version for all struct sizes

5. The generated code reports 0 or near-0 `allocs/op` for marshaling (using `strconv.Append*` into a reusable buffer), while the reflection version shows measurable allocations

6. The generator handles pointer fields: `*string` emits code that checks for nil and writes `null`, non-nil writes the dereferenced value

7. Nested struct fields produce recursive marshal calls to the nested type's generated `MarshalJSON` method

## Research Resources

- [go/ast package](https://pkg.go.dev/go/ast) -- the AST types for Go source code
- [go/parser package](https://pkg.go.dev/go/parser) -- parsing Go source into AST
- [text/template package](https://pkg.go.dev/text/template) -- template engine for code generation
- [go generate](https://go.dev/blog/generate) -- official blog post on `go generate`
- [stringer tool source](https://cs.opensource.google/go/x/tools/+/master:cmd/stringer/) -- canonical example of a `go generate` tool
- [easyjson](https://github.com/mailru/easyjson) -- production code-generated JSON library for comparison
- [ffjson](https://github.com/pquerna/ffjson) -- another code-generated JSON library

## What's Next

With both reflection and code generation in your toolkit, the final exercise combines them to build a configuration loader that uses reflection for flexibility with optional code-generated fast paths.

## Summary

Code generation and reflection solve the same problem -- bridging static types with dynamic behavior -- but at different points in time. Reflection operates at runtime with full flexibility but pays per-operation overhead. Code generation operates at build time, producing specialized code that runs with zero reflection overhead but requires a generation step and handles only known types. Parsing Go source with `go/ast` gives the generator access to struct definitions, field types, and tags. The generated code uses `strconv.Append*` and direct field access for zero-allocation marshaling. Choose reflection for library code that must handle arbitrary types; choose code generation for hot-path serialization where performance matters.
