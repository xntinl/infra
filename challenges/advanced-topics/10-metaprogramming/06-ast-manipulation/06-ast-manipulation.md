<!--
type: reference
difficulty: advanced
section: [10-metaprogramming]
concepts: [go-ast, go-parser, go-printer, syn-parsing, prettyplease, AST-traversal, code-transformation]
languages: [go, rust]
estimated_reading_time: 80 min
bloom_level: create
prerequisites: [compiler-pipeline-basics, go-reflection-basics, rust-proc-macros, go-code-generation]
papers: []
industry_use: [gopls, staticcheck, golangci-lint, cargo-expand, rustfmt, clippy]
language_contrast: high
-->

# AST Manipulation

> Treating source code as a data structure — parse it to a tree, inspect or transform the tree, print it back to text — is the foundation of every linter, formatter, refactoring tool, and code generator in both ecosystems.

## Mental Model

Every programming language can be described as a grammar, and every syntactically valid program corresponds to a tree according to that grammar: the Abstract Syntax Tree (AST). An AST strips the surface-level tokens (parentheses, semicolons, whitespace) and retains only the meaningful structure: this is a function declaration with these parameters, this is a method call on this expression, this is an if statement with this condition and these branches.

The power of AST manipulation is that it lets you reason about code at the semantic level rather than the textual level. A find-and-replace on source text is brittle — `foo.bar` in a string literal looks the same as `foo.bar` as a method call. An AST traversal knows the difference: it can match only `ast.SelectorExpr` nodes with `X.Name == "foo"` and `Sel.Name == "bar"` that appear in expression position, not in string literals.

Three operations compose into all AST-based tooling:

1. **Parse**: source text → AST (Go: `go/parser.ParseFile`; Rust: `syn::parse`)
2. **Transform/Inspect**: walk the AST, read or modify nodes
3. **Print**: AST → source text (Go: `go/printer.Fprint`; Rust: `quote!` + `prettyplease`)

The round-trip property — parse → print → parse gives you the same tree — is what makes AST manipulation safe for code transformers. A formatter like `gofmt` or `rustfmt` works exactly this way: parse the source, discard all formatting, print the AST with canonical formatting rules.

## Core Concepts

### Go: `go/ast` Package Structure

The Go AST is a hierarchy of `ast.Node` types. The key types:

```
ast.File
├── Name: *ast.Ident               (package name)
├── Imports: []*ast.ImportSpec
└── Decls: []ast.Decl
    ├── *ast.FuncDecl              (function or method)
    │   ├── Name: *ast.Ident
    │   ├── Type: *ast.FuncType    (parameters, results)
    │   └── Body: *ast.BlockStmt
    └── *ast.GenDecl               (var, const, type, import)
        └── Specs: []ast.Spec
            ├── *ast.TypeSpec      (type declaration)
            ├── *ast.ValueSpec     (var or const)
            └── *ast.ImportSpec
```

Traversal uses `ast.Inspect(node, fn)` which calls `fn` for each node in a pre-order depth-first walk. Return `true` to continue into children, `false` to stop.

For type-aware analysis (resolving what type an expression has, which package a name refers to), use `golang.org/x/tools/go/packages` with `NeedTypes | NeedTypesInfo` flags. This loads the full type checker information alongside the AST.

### Go: `go/printer` for Round-Tripping

`go/printer.Fprint(w, fset, node)` prints any AST node back to source text. The `fset` (token.FileSet) is required to map AST positions back to line numbers. The output is `gofmt`-compatible but not identical to `gofmt` output (it does not enforce the canonical spacing rules). Always pipe the output through `go/format.Source()` for canonical formatting.

The `go/format.Node` function combines both steps:
```go
var buf bytes.Buffer
format.Node(&buf, fset, node)
formatted, err := format.Source(buf.Bytes())
```

### Rust: `syn` Crate — The Rust AST

`syn` parses a `TokenStream` into a typed Rust AST. Key types:

```
syn::File
└── items: Vec<syn::Item>
    ├── syn::Item::Fn(ItemFn)
    │   ├── sig: Signature
    │   │   ├── ident: Ident        (function name)
    │   │   ├── inputs: Punctuated<FnArg, Comma>
    │   │   └── output: ReturnType
    │   └── block: Box<Block>
    ├── syn::Item::Struct(ItemStruct)
    │   ├── ident: Ident
    │   └── fields: Fields
    └── syn::Item::Impl(ItemImpl)
        ├── self_ty: Type
        └── items: Vec<ImplItem>
```

To parse a full Rust file (not inside a proc macro):
```rust
use syn::{parse_file, File};
let source = std::fs::read_to_string("src/lib.rs").unwrap();
let ast: File = parse_file(&source).unwrap();
```

For transformations, implement `syn::visit_mut::VisitMut` to walk and modify the AST in place.

### Rust: `prettyplease` for Formatted Output

`prettyplease` is a Rust source code formatter that accepts a `syn::File` and returns a formatted string. It is the `gofmt` equivalent for generated Rust code:

```rust
use prettyplease::unparse;
let formatted = unparse(&ast_file);
```

When generating Rust code with `quote!`, the output is a `TokenStream` — a flat sequence of tokens with no formatting. Pipe it through `prettyplease` for human-readable output.

## Implementation: Go

### Extract Exported Function Signatures from Go Source

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

// FuncSignature holds the extracted metadata for a single exported function.
type FuncSignature struct {
	Name       string
	Receiver   string   // empty for top-level functions
	Parameters []ParamInfo
	Results    []ParamInfo
	Doc        string
}

type ParamInfo struct {
	Names []string // may be empty for anonymous params
	Type  string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: extract-sigs <file.go>")
		os.Exit(1)
	}

	sigs, err := ExtractExportedFunctions(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for _, sig := range sigs {
		printSignature(sig)
	}
}

func ExtractExportedFunctions(filename string) ([]FuncSignature, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	var sigs []FuncSignature

	for _, decl := range f.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		// Skip unexported: exported names start with uppercase
		if !funcDecl.Name.IsExported() {
			continue
		}

		sig := FuncSignature{
			Name: funcDecl.Name.Name,
		}

		// Extract receiver (for methods)
		if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
			sig.Receiver = typeString(funcDecl.Recv.List[0].Type)
		}

		// Extract parameters
		if funcDecl.Type.Params != nil {
			sig.Parameters = extractParams(funcDecl.Type.Params.List)
		}

		// Extract return values
		if funcDecl.Type.Results != nil {
			sig.Results = extractParams(funcDecl.Type.Results.List)
		}

		// Extract doc comment
		if funcDecl.Doc != nil {
			sig.Doc = strings.TrimSpace(funcDecl.Doc.Text())
		}

		sigs = append(sigs, sig)
	}

	return sigs, nil
}

func extractParams(fields []*ast.Field) []ParamInfo {
	var params []ParamInfo
	for _, field := range fields {
		info := ParamInfo{
			Type: typeString(field.Type),
		}
		for _, name := range field.Names {
			info.Names = append(info.Names, name.Name)
		}
		params = append(params, info)
	}
	return params
}

// typeString converts an ast.Expr representing a type to its string form.
// Handles the common cases: Ident, StarExpr (pointer), ArrayType, SelectorExpr.
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeString(t.Elt)
		}
		return fmt.Sprintf("[%s]%s", typeString(t.Len), typeString(t.Elt))
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", typeString(t.Key), typeString(t.Value))
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + typeString(t.Elt)
	case *ast.BasicLit:
		return t.Value
	default:
		return fmt.Sprintf("<%T>", expr)
	}
}

func printSignature(sig FuncSignature) {
	if sig.Doc != "" {
		fmt.Printf("// %s\n", sig.Doc)
	}

	var recv string
	if sig.Receiver != "" {
		recv = fmt.Sprintf("(%s) ", sig.Receiver)
	}

	params := formatParams(sig.Parameters)
	results := formatResults(sig.Results)

	fmt.Printf("func %s%s(%s)%s\n\n", recv, sig.Name, params, results)
}

func formatParams(params []ParamInfo) string {
	var parts []string
	for _, p := range params {
		if len(p.Names) == 0 {
			parts = append(parts, p.Type)
		} else {
			parts = append(parts, strings.Join(p.Names, ", ")+" "+p.Type)
		}
	}
	return strings.Join(parts, ", ")
}

func formatResults(results []ParamInfo) string {
	if len(results) == 0 {
		return ""
	}
	if len(results) == 1 && len(results[0].Names) == 0 {
		return " " + results[0].Type
	}
	return " (" + formatParams(results) + ")"
}
```

**Usage:**
```sh
go run ./main.go ./path/to/mypackage/types.go
# Output:
# // CreateUser creates a new user in the database.
# func CreateUser(ctx context.Context, name string, email string) (*User, error)
#
# func (u *User) Validate() error
```

### Go-specific considerations

**`go/types` for semantic analysis**: `go/ast` gives you syntax; `go/types` gives you semantics. If you need to know the concrete type of an interface value, which method set a type satisfies, or whether two types are identical (not just syntactically equal), load the type checker:

```go
import "golang.org/x/tools/go/packages"
cfg := &packages.Config{Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax}
pkgs, err := packages.Load(cfg, "./...")
```

**`astutil.Apply` for in-place transformations**: For modifying an AST (replacing nodes, inserting nodes), use `golang.org/x/tools/go/ast/astutil.Apply`. It is safer than direct node mutation because it handles the parent-child pointer updates:

```go
astutil.Apply(f, func(c *astutil.Cursor) bool {
    if call, ok := c.Node().(*ast.CallExpr); ok {
        if isDeprecatedFunc(call) {
            c.Replace(newCallExpr(call))
        }
    }
    return true
}, nil)
```

## Implementation: Rust

### Attribute Macro That Injects Logging Using `syn`

```rust
// In a proc-macro crate: src/lib.rs

use proc_macro::TokenStream;
use proc_macro2::TokenStream as TokenStream2;
use quote::quote;
use syn::{
    parse_macro_input, parse_quote,
    visit_mut::{self, VisitMut},
    Block, Expr, ExprReturn, ItemFn, ReturnType, Stmt,
};

/// #[trace_calls] — wraps the function body with entry/exit logging.
///
/// Usage:
///   #[trace_calls]
///   fn process(id: u64, name: &str) -> Result<(), Error> { ... }
///
/// Generates:
///   fn process(id: u64, name: &str) -> Result<(), Error> {
///       eprintln!("[ENTER] process(id={:?}, name={:?})", id, name);
///       let __result = (original body);
///       eprintln!("[EXIT]  process → {:?}", __result);
///       __result
///   }
#[proc_macro_attribute]
pub fn trace_calls(_attr: TokenStream, item: TokenStream) -> TokenStream {
    let mut func = parse_macro_input!(item as ItemFn);
    let fn_name = func.sig.ident.to_string();

    // Collect parameter names for logging
    let param_names: Vec<_> = func.sig.inputs.iter().filter_map(|arg| {
        if let syn::FnArg::Typed(pat_type) = arg {
            if let syn::Pat::Ident(pat_ident) = pat_type.pat.as_ref() {
                return Some(&pat_ident.ident);
            }
        }
        None
    }).collect();

    // Build the format string: "fn(param1={:?}, param2={:?})"
    let param_format = if param_names.is_empty() {
        String::new()
    } else {
        let pairs: Vec<_> = param_names.iter()
            .map(|n| format!("{}={{:?}}", n))
            .collect();
        format!("({})", pairs.join(", "))
    };

    let enter_fmt = format!("[ENTER] {}{}", fn_name, param_format);
    let exit_fmt = format!("[EXIT]  {} → {{:?}}", fn_name);

    let original_block = &func.block;

    let new_block: Block = parse_quote! {
        {
            eprintln!(#enter_fmt, #(#param_names),*);
            let __result = (|| #original_block)();
            eprintln!(#exit_fmt, __result);
            __result
        }
    };

    func.block = Box::new(new_block);
    quote! { #func }.into()
}

/// #[memoize] — adds a HashMap cache to a function that takes a single Clone+Hash+Eq argument
/// and returns a Clone value. This is a simplified example; production memoization
/// (e.g., the `memoize` crate) handles more cases.
#[proc_macro_attribute]
pub fn memoize(_attr: TokenStream, item: TokenStream) -> TokenStream {
    let input = parse_macro_input!(item as ItemFn);
    let fn_name = &input.sig.ident;
    let vis = &input.vis;

    // Extract the single parameter
    let params = &input.sig.inputs;
    if params.len() != 1 {
        return syn::Error::new_spanned(params, "#[memoize] requires exactly one parameter")
            .to_compile_error()
            .into();
    }

    let (param_name, param_type) = match params.first().unwrap() {
        syn::FnArg::Typed(pt) => {
            let name = &pt.pat;
            let ty = &pt.ty;
            (quote! { #name }, quote! { #ty })
        }
        _ => return syn::Error::new_spanned(params, "#[memoize] does not support self parameters")
            .to_compile_error()
            .into(),
    };

    let ret_type = match &input.sig.output {
        ReturnType::Type(_, ty) => quote! { #ty },
        ReturnType::Default => quote! { () },
    };

    let body = &input.block;
    let cache_name = syn::Ident::new(
        &format!("__CACHE_{}", fn_name.to_string().to_uppercase()),
        fn_name.span(),
    );

    let expanded = quote! {
        // Thread-local cache — one per OS thread
        ::std::thread_local! {
            static #cache_name: ::std::cell::RefCell<
                ::std::collections::HashMap<#param_type, #ret_type>
            > = ::std::cell::RefCell::new(::std::collections::HashMap::new());
        }

        #vis fn #fn_name(#param_name: #param_type) -> #ret_type {
            #cache_name.with(|cache| {
                if let Some(cached) = cache.borrow().get(&#param_name) {
                    return cached.clone();
                }
                let result = (|#param_name: #param_type| -> #ret_type { #body })(#param_name.clone());
                cache.borrow_mut().insert(#param_name, result.clone());
                result
            })
        }
    };

    expanded.into()
}
```

**Using these macros:**
```rust
use my_macros::{trace_calls, memoize};

#[trace_calls]
fn greet(name: String, times: u32) -> String {
    format!("{} x{}", name, times)
}

#[memoize]
fn expensive_computation(n: u64) -> u64 {
    // Imagine this is slow
    n * n + n + 41
}

fn main() {
    println!("{}", greet("world".to_string(), 3));
    // [ENTER] greet(name="world", times=3)
    // [EXIT]  greet → "world x3"

    println!("{}", expensive_computation(10)); // computed
    println!("{}", expensive_computation(10)); // cache hit
}
```

### Rust-specific considerations

**`syn` full parse vs. derive parse**: `syn` has two feature levels: `features = ["derive"]` (structs, enums, attributes — for derive macros) and `features = ["full"]` (the complete Rust grammar — for attribute macros that need to parse function bodies, expressions, statements). The `full` feature is significantly larger. If your proc macro only derives from struct definitions, use `"derive"` to save compile time.

**`Visit` vs `VisitMut`**: `syn::visit::Visit` traverses the AST read-only. `syn::visit_mut::VisitMut` traverses it with mutable access so you can transform nodes in place. For analysis (linting, code extraction), use `Visit`. For transformation (macro expansion, injection), use `VisitMut`.

**Span preservation**: Every `syn` AST node carries a `Span` indicating where in the source it came from. When generating code with `quote!`, using `#ident` (where `ident` came from the input span) means compiler errors in the generated code point to the original source. Creating new identifiers with `quote::format_ident!("new_name")` gives them a synthetic span. Choosing which span to use is an art — the goal is that errors point somewhere the user can fix.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| AST library | `go/ast` (stdlib) | `syn` crate |
| Parse entry point | `go/parser.ParseFile` | `syn::parse_file` / `syn::parse` |
| Type info | `golang.org/x/tools/go/packages` | `syn` is syntax only; type info via `cargo check` + proc macros |
| AST walker | `ast.Inspect` callback | `syn::visit::Visit` trait |
| AST transformer | `astutil.Apply` | `syn::visit_mut::VisitMut` trait |
| AST → source | `go/printer.Fprint` + `go/format.Source` | `quote!` + `prettyplease::unparse` |
| Round-trip fidelity | High (gofmt-canonical output) | High (`prettyplease` produces rustfmt-compatible output) |
| Comment preservation | `go/parser.ParseComments` mode | `syn::parse_file` preserves doc comments |
| Production uses | `gopls`, `staticcheck`, `goimports` | `rustfmt`, `clippy`, `cargo-expand` |

## Production War Stories

**`gopls` and the cost of full AST type checking**: `gopls` (the Go language server) loads and type-checks every package in your module to provide IDE features. On a large Go module with hundreds of packages, this takes several seconds on first load and consumes gigabytes of memory. The Go team has invested heavily in incremental parsing and caching within `gopls`. The lesson: AST analysis with full type information is expensive; design tools to be incremental and cache aggressively.

**`rustfmt` and the challenge of comment placement**: `rustfmt` operates on the Rust AST, but AST nodes do not have a natural place to attach comments (comments are between tokens, and tokens are stripped when building the AST). The Rust team built a separate comment attribution system that associates each comment with the nearest AST node by source position. Getting this right for edge cases (comments inside complex expressions, trailing comments on the same line as code) required years of iteration. It is the primary source of `rustfmt` bugs. If you build a tool that round-trips Rust source and needs to preserve comments, study rustfmt's implementation.

**`staticcheck` as a model AST analysis tool**: `staticcheck` is the gold standard for Go static analysis. It uses `golang.org/x/tools/go/analysis` (a framework for composable AST analyzers), which handles the scaffolding (loading packages, running analyzers in dependency order, reporting diagnostics) so each analyzer can focus on the analysis logic. Writing a custom `staticcheck`-compatible analyzer is the right way to encode project-specific linting rules.

## Complexity Analysis

| Dimension | Cost |
|-----------|------|
| Parse (Go, `go/parser`) | O(file size); ~1-5ms for typical files |
| Parse (Rust, `syn`) | O(token count); similar to Go |
| Type-aware analysis (Go) | O(package closure); seconds for large module |
| AST traversal | O(AST nodes); typically fast |
| Round-trip (parse → print) | O(AST nodes); gofmt adds formatting pass |
| `syn::visit_mut` transformation | O(nodes visited) |

## Common Pitfalls

**1. Not handling `ast.CommentMap` in Go transformations.** If you transform an AST and then print it, comments may be lost or misplaced. Use `ast.NewCommentMap` to associate comments with nodes, and carry the comment map through the transformation.

**2. Assuming `ast.Inspect` visits all nodes.** `ast.Inspect` only visits nodes reachable from the root. If you stop the walk early (returning `false`), you miss everything in that subtree. For tools that must visit every node, always return `true` unless you have a deliberate reason to prune.

**3. `syn` parsing succeeding for invalid Rust code.** `syn` parses to the Rust syntax grammar, not to valid Rust semantics. Code that `syn` parses successfully may fail to compile because of type errors or borrow violations. Never assume that a successful `syn::parse` means the code is valid Rust.

**4. Mutating the AST while traversing in Go.** `ast.Inspect` does not support mutation during traversal. Use `astutil.Apply` from `golang.org/x/tools/go/ast/astutil` for in-place transformations — it is designed for mutation.

**5. Ignoring span hygiene in Rust proc macros.** When you create a new `syn::Ident` with a span from the input, errors in generated code point to the input location. When you create identifiers with `Span::call_site()`, errors point to where the macro was invoked. Choosing the wrong span makes generated code errors confusing. The rule: use the input span for things the user controls, use `call_site()` for things the macro generates internally.

## Exercises

**Exercise 1** (30 min): Write a Go tool that reads a `.go` file and counts: (a) total exported functions; (b) total exported methods (functions with receivers); (c) total lines of code (non-empty, non-comment lines inside function bodies). Use `ast.Inspect` and `token.FileSet` for position information.

**Exercise 2** (2-4h): Write a Go AST transformer that reads a `.go` file and replaces all direct `fmt.Println(...)` calls with `log.Println(...)` calls, updating the import section accordingly (remove `fmt` if no longer used, add `log` if not present). Use `astutil.Apply` for transformation and `astutil.AddImport`/`astutil.DeleteImport` for import management. Write the result back with `go/format.Source`.

**Exercise 3** (4-8h): Implement a Rust proc macro `#[instrument]` similar to the `tracing::instrument` macro. It should: (a) parse the `#[instrument(level = "debug")]` attribute arguments; (b) wrap the function body with `tracing::span!` at the specified level; (c) record all function parameters as span fields. Handle both sync and async functions (detect `async fn` from the signature and use `async` in the wrapper accordingly).

**Exercise 4** (8-15h): Build a Go tool that uses `golang.org/x/tools/go/packages` to analyze a codebase and detect all functions that: (a) accept `interface{}` (or `any`) parameters; (b) have more than 5 parameters; (c) have no documentation comment. Output a report grouped by file with the function name, location, and which rules it violates. Package this as a `golang.org/x/tools/go/analysis` analyzer so it can be integrated with `golangci-lint`.

## Further Reading

### Foundational Papers

- Aho, Lam, Sethi, Ullman: "Compilers: Principles, Techniques, and Tools" (Dragon Book) — Chapters 2-4 on parsing and AST construction.
- [Roslyn design documents](https://github.com/dotnet/roslyn/wiki/Roslyn-Overview) — the C# Roslyn compiler was the first mainstream "compiler as a service" API; its design influenced Go's `go/ast` tooling philosophy.

### Books

- [Writing An Interpreter In Go (Thorsten Ball)](https://interpreterbook.com/) — builds a complete AST-based interpreter from scratch; the best hands-on introduction to AST manipulation.
- [Crafting Interpreters (Nystrom)](https://craftinginterpreters.com/) — free online; covers both tree-walking and bytecode approaches.

### Production Code to Read

- [`go/analysis` framework](https://github.com/golang/tools/tree/master/go/analysis) — the scaffolding for `staticcheck` and `go vet` analyzers.
- [`staticcheck` source](https://github.com/dominikh/go-tools) — study `simple/` and `staticcheck/` for idiomatic analysis pass implementations.
- [`syn` source](https://github.com/dtolnay/syn) — `src/item.rs` and `src/expr.rs` for the full Rust AST structure.
- [`rustfmt` source](https://github.com/rust-lang/rustfmt) — `src/visitor.rs` for the comment preservation strategy.
- [`cargo-expand` source](https://github.com/dtolnay/cargo-expand) — shows how macro expansion output is captured and pretty-printed.

### Talks

- [Alan Donovan: "Go Tools" (GopherCon 2014)](https://www.youtube.com/watch?v=3Fn-ePn7mFo) — the design of the Go tooling ecosystem.
- [David Tolnay: "syn and quote" (RustConf 2019)](https://www.youtube.com/watch?v=1KVOkT9DXJk) — the author explaining how syn represents the full Rust grammar.
