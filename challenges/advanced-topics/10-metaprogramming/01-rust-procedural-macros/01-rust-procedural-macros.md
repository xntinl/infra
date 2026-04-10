<!--
type: reference
difficulty: advanced
section: [10-metaprogramming]
concepts: [proc-macros, derive-macros, attribute-macros, function-like-macros, TokenStream, syn, quote, proc_macro2]
languages: [rust]
estimated_reading_time: 75 min
bloom_level: create
prerequisites: [rust-traits, rust-generics, cargo-workspaces, basic-compiler-pipeline]
papers: []
industry_use: [serde, thiserror, async_trait, sqlx, axum, tokio]
language_contrast: high
-->

# Rust Procedural Macros

> Proc macros let you run arbitrary Rust code at compile time to transform one `TokenStream` into another — the mechanism behind `#[derive(Serialize)]`, `#[tokio::main]`, and every Rust framework that feels like magic.

## Mental Model

A procedural macro is a compiler plugin. When `rustc` encounters `#[derive(Builder)]` on a struct, it pauses compilation, calls your macro function with the struct's token stream, and splices the returned token stream into the source as if you had typed it by hand. This happens before type-checking: the tokens your macro produces will be type-checked by the compiler afterward, giving you the full benefit of Rust's type system on generated code.

The key mental model shift is: **your macro receives text (tokens), not a parsed program**. You use the `syn` crate to parse that text into an AST you can inspect, use `quote!` to template the output tokens, and hand a `proc_macro2::TokenStream` back to the compiler. The `proc_macro` crate (from the compiler itself), `proc_macro2` (a stable re-implementation usable in tests), `syn` (parser), and `quote` (code generation template engine) form the canonical four-crate stack.

Proc macros must live in their own crate marked `proc-macro = true`. This is not a limitation — it is a deliberate isolation: the macro code runs in the compiler process, so it must be compiled for the host machine, and it must be a stable ABI boundary. The consequence is that every project using proc macros pays the compile-time cost of building that crate (plus `syn`, which is large) on every clean build. For `serde`, this is the reason `serde_derive` is a separate crate: so users who only need `serde::de::Deserialize` as a bound — but do not derive it — do not pay the proc-macro compile cost.

## Core Concepts

### TokenStream

`TokenStream` is the raw currency of proc macros. It represents a sequence of tokens (identifiers, punctuation, literals, groups) as an opaque type. `proc_macro::TokenStream` is the compiler's type, available only inside `#[proc_macro]` functions. `proc_macro2::TokenStream` is a re-export you can use in tests and in `syn`/`quote` without the compiler ABI restriction. In practice, every proc macro crate does this at its entry points:

```rust
use proc_macro::TokenStream;
#[proc_macro_derive(MyTrait)]
pub fn my_derive(input: TokenStream) -> TokenStream {
    // convert to proc_macro2 immediately
    let input2 = proc_macro2::TokenStream::from(input);
    // ... work with proc_macro2 and syn internally ...
    // convert back at the end
    proc_macro2::TokenStream::from(output).into()
}
```

### The Three Macro Types

**Derive macros** (`#[derive(Trait)]`) are attached to `struct`, `enum`, or `union` definitions. They receive the full item definition and can only *add* new items — they cannot modify the annotated item itself. This is why derive macros generate `impl` blocks separately from the struct.

**Attribute macros** (`#[my_attr]` or `#[my_attr(args)]`) are more powerful: they receive both the attribute arguments and the entire annotated item, and they replace the item with their output. `#[tokio::main]` is an attribute macro that replaces your `async fn main()` with a `fn main()` containing a `Runtime::new().block_on(...)` wrapper.

**Function-like macros** (`my_macro!(...)`) look like `macro_rules!` invocations but run arbitrary Rust code. They are used when neither derive nor attribute fits — for example, `sql!("SELECT ...")` in sqlx, which parses SQL at compile time and emits typed Rust code.

### syn: Parsing the Input

`syn` parses a `TokenStream` into a rich Rust AST. The two most common entry points are:

```rust
let input: syn::DeriveInput = syn::parse(tokens)?;  // for derive macros
let expr: syn::Expr = syn::parse(tokens)?;           // for expressions
```

`DeriveInput` gives you the struct/enum name (`input.ident`), generics (`input.generics`), and data (`input.data` — a `Data::Struct`, `Data::Enum`, or `Data::Union`). For a struct, `input.data` contains the list of fields with their names, types, and attributes.

### quote!: Generating Output

`quote!` is a macro that takes quasi-quoted Rust syntax and produces a `proc_macro2::TokenStream`. Variables from the surrounding scope are interpolated with `#var`:

```rust
let name = &input.ident;
let expanded = quote! {
    impl MyTrait for #name {
        fn hello(&self) -> &str { stringify!(#name) }
    }
};
```

Repetition uses `#(#items)*` syntax, mirroring `macro_rules!` repetition. Generating complex `impl` blocks with generic bounds requires extracting the generics data from `syn` using the helper methods `split_for_impl()`.

## Implementation: Rust

A complete `#[derive(Builder)]` proc macro — generates a builder pattern for any struct.

**Cargo workspace layout:**

```
my_project/
├── Cargo.toml          # workspace
├── builder-derive/
│   ├── Cargo.toml      # [lib] proc-macro = true
│   └── src/lib.rs
└── example/
    ├── Cargo.toml
    └── src/main.rs
```

**builder-derive/Cargo.toml:**
```toml
[package]
name = "builder-derive"
version = "0.1.0"
edition = "2021"

[lib]
proc-macro = true

[dependencies]
syn = { version = "2", features = ["full"] }
quote = "1"
proc_macro2 = "1"
```

**builder-derive/src/lib.rs:**
```rust
use proc_macro::TokenStream;
use proc_macro2::TokenStream as TokenStream2;
use quote::{format_ident, quote};
use syn::{
    parse_macro_input, Data, DeriveInput, Fields, FieldsNamed, Type,
};

/// Derives a builder pattern for a named-field struct.
///
/// Usage:
///   #[derive(Builder)]
///   pub struct Request { url: String, timeout: u64, retries: u32 }
///
/// Generates:
///   pub struct RequestBuilder { url: Option<String>, ... }
///   impl RequestBuilder { pub fn url(mut self, v: String) -> Self { ... } }
///   impl Request { pub fn builder() -> RequestBuilder { ... } }
#[proc_macro_derive(Builder)]
pub fn derive_builder(input: TokenStream) -> TokenStream {
    let input = parse_macro_input!(input as DeriveInput);
    match expand_builder(input) {
        Ok(ts) => ts.into(),
        Err(e) => e.to_compile_error().into(),
    }
}

fn expand_builder(input: DeriveInput) -> syn::Result<TokenStream2> {
    let struct_name = &input.ident;
    let builder_name = format_ident!("{}Builder", struct_name);

    let fields = named_fields(&input)?;

    // Builder struct: each field becomes Option<FieldType>
    let builder_fields = fields.named.iter().map(|f| {
        let name = &f.ident;
        let ty = &f.ty;
        quote! { #name: Option<#ty> }
    });

    // Setter methods: one per field
    let setter_methods = fields.named.iter().map(|f| {
        let name = &f.ident;
        let ty = &f.ty;
        quote! {
            pub fn #name(mut self, value: #ty) -> Self {
                self.#name = Some(value);
                self
            }
        }
    });

    // build() method: returns Result<StructName, String>
    let build_assignments = fields.named.iter().map(|f| {
        let name = &f.ident;
        let field_str = name.as_ref().map(|i| i.to_string()).unwrap_or_default();
        quote! {
            #name: self.#name.ok_or_else(|| {
                format!("field '{}' is required", #field_str)
            })?
        }
    });

    // Default initializer for builder (all None)
    let none_inits = fields.named.iter().map(|f| {
        let name = &f.ident;
        quote! { #name: None }
    });

    let (impl_generics, ty_generics, where_clause) =
        input.generics.split_for_impl();

    let expanded = quote! {
        pub struct #builder_name {
            #(#builder_fields,)*
        }

        impl #builder_name {
            #(#setter_methods)*

            pub fn build(self) -> Result<#struct_name #ty_generics, String> {
                Ok(#struct_name {
                    #(#build_assignments,)*
                })
            }
        }

        impl #impl_generics #struct_name #ty_generics #where_clause {
            pub fn builder() -> #builder_name {
                #builder_name {
                    #(#none_inits,)*
                }
            }
        }
    };

    Ok(expanded)
}

fn named_fields(input: &DeriveInput) -> syn::Result<&FieldsNamed> {
    match &input.data {
        Data::Struct(s) => match &s.fields {
            Fields::Named(f) => Ok(f),
            _ => Err(syn::Error::new_spanned(
                input,
                "Builder only supports structs with named fields",
            )),
        },
        _ => Err(syn::Error::new_spanned(
            input,
            "Builder can only be derived for structs",
        )),
    }
}
```

**example/src/main.rs:**
```rust
use builder_derive::Builder;

#[derive(Builder, Debug)]
pub struct HttpRequest {
    url: String,
    timeout_secs: u64,
    retries: u32,
}

fn main() {
    let req = HttpRequest::builder()
        .url("https://api.example.com/v1/data".to_string())
        .timeout_secs(30)
        .retries(3)
        .build()
        .expect("all required fields were set");

    println!("{:?}", req);

    // Missing field returns an error at runtime (not compile time for optional fields)
    let err = HttpRequest::builder()
        .url("https://api.example.com".to_string())
        // timeout_secs not set
        .retries(1)
        .build();
    println!("Missing field result: {:?}", err);
}
```

**Attribute macro example — `#[log_call]` that wraps a function with entry/exit logging:**

```rust
// In builder-derive/src/lib.rs (add to the same crate)

use syn::{parse_macro_input, ItemFn};

#[proc_macro_attribute]
pub fn log_call(_attr: TokenStream, item: TokenStream) -> TokenStream {
    let input_fn = parse_macro_input!(item as ItemFn);
    let fn_name = &input_fn.sig.ident;
    let fn_name_str = fn_name.to_string();
    let vis = &input_fn.vis;
    let sig = &input_fn.sig;
    let body = &input_fn.block;

    let expanded = quote! {
        #vis #sig {
            eprintln!("[ENTER] {}", #fn_name_str);
            let __result = (|| #body)();
            eprintln!("[EXIT]  {}", #fn_name_str);
            __result
        }
    };

    expanded.into()
}
```

**Function-like macro example — `validated_regex!` that compiles a regex at compile time:**

```rust
use proc_macro::TokenStream;
use proc_macro2::Literal;
use quote::quote;
use syn::{parse_macro_input, LitStr};

/// validated_regex!("^[a-z]+$") — fails to compile if the regex is invalid.
/// At runtime, just calls Regex::new with the same string (guaranteed not to panic).
#[proc_macro]
pub fn validated_regex(input: TokenStream) -> TokenStream {
    let pattern = parse_macro_input!(input as LitStr);
    let value = pattern.value();

    // Validate at macro expansion time (which is compile time)
    if let Err(e) = regex_syntax::Parser::new().parse(&value) {
        return syn::Error::new_spanned(pattern, format!("invalid regex: {}", e))
            .to_compile_error()
            .into();
    }

    quote! { regex::Regex::new(#value).unwrap() }.into()
}
```

### Rust-specific considerations

**Error handling in proc macros**: Return errors via `syn::Error::new_spanned(tokens, message).to_compile_error()`. This attaches the error to the specific tokens that caused it, giving users a meaningful diagnostic pointing at the right line in their source. Panicking in a proc macro produces a cryptic "proc macro panicked" message with no location — always use `syn::Error`.

**Hygiene**: Proc macros in Rust are hygienic at the identifier level when using `quote!`. Identifiers you introduce in generated code (like temporary variables) will not clash with names in the call site. However, paths to types and traits must be fully qualified (e.g., `::std::option::Option` rather than `Option`) if you cannot guarantee the user has the right imports. This is why serde-generated code is full of `_serde::` prefixed paths.

**`cargo expand`**: The `cargo-expand` tool (`cargo install cargo-expand`) prints the macro-expanded source. It is indispensable for debugging proc macros. When a user reports a confusing error in code using your derive macro, the first step is always `cargo expand` to see what code the macro actually produced.

**Compile time**: `syn` with `features = ["full"]` is a large dependency. On a clean build it can add 10-30 seconds depending on hardware. For macros that only need to parse structs (not full Rust expressions), `features = ["derive"]` is significantly smaller. Profile with `cargo build --timings` before shipping a proc macro as a library dependency.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Equivalent mechanism | No compile-time macro system; use `go:generate` | Proc macros (`syn` + `quote`) |
| When it runs | Developer's machine, before `git commit` | Compiler, during every `cargo build` |
| Visibility of generated code | Committed to repo, fully readable | Hidden in-memory; use `cargo expand` to inspect |
| Type safety | Generated code checked at compile time | Generated code checked at compile time |
| Error attribution | Errors in generated file, can be confusing | `syn::Error::new_spanned` gives precise location |
| IDE support | Full (generated code is real Go) | Improving (rust-analyzer understands proc macros) |
| Dependency overhead | None | `syn` = significant compile time |
| Hot path cost | N/A | Zero (all work at compile time) |

## Production War Stories

**serde** is the canonical example of proc macros done right. `#[derive(Serialize, Deserialize)]` generates zero-overhead serialization code with full support for renaming, skipping, flattening, and custom logic — entirely at compile time. The generated code is as fast as hand-written code because it is hand-written code, just authored by a program. The cost: `serde_derive` is consistently in the top-5 compile-time bottlenecks in large Rust projects. The `serde` team tracks this carefully and has invested significantly in optimization.

**async_trait**: Before Rust's native `async fn in traits` (stabilized in Rust 1.75), every async trait method required `#[async_trait]`. This attribute macro rewrites async trait methods to return `Box<dyn Future>`, which is a semantic change (it adds a heap allocation per call). The attribute was a workaround for a language limitation, and its widespread use created a performance footgun many teams did not notice until profiling. This is the canonical example of a macro that compiles successfully but changes runtime behavior in ways users did not expect.

**thiserror**: A derive macro that generates `std::error::Error` implementations. Simple enough that reading its source (`~400 lines`) is an excellent learning exercise — it covers syn parsing, handling optional fields (for `#[from]` and `#[source]` attributes), and producing clean compile errors.

**axum's routing macros**: Axum uses proc macros to generate type-safe routing glue. The macro expansion for a moderately complex router can be thousands of lines. Teams have reported that `cargo expand` output for an axum application is so large it is effectively unreadable — a real debugging burden when something goes wrong at the routing layer.

## Complexity Analysis

| Dimension | Cost |
|-----------|------|
| Compile-time (macro execution) | O(n) in tokens; syn parsing is the dominant cost |
| Compile-time (syn dependency) | ~10-30s per clean build; amortized across incremental builds |
| Runtime | Zero — all work happens before the binary exists |
| Maintenance | High — proc macros are complex Rust code; errors produce non-obvious diagnostics |
| Debugging | High — cargo expand is required; IDE support is incomplete |
| Testing | Requires integration tests (compile-test crates like `trybuild`) |

## Common Pitfalls

**1. Not handling generic structs.** A derive macro that generates `impl MyTrait for Foo` will fail to compile when applied to `Foo<T>`. Always use `input.generics.split_for_impl()` and include `#impl_generics`, `#ty_generics`, and `#where_clause` in your generated `impl` block.

**2. Panicking instead of returning `syn::Error`.** A panic inside a proc macro produces a cryptic compiler error with no source location. Always use `syn::Error::new_spanned(tokens, message)` and return `error.to_compile_error().into()` for graceful, attributed errors.

**3. Not qualifying generated type paths.** If your macro emits `Option<T>`, it works as long as the user has `use std::option::Option` in scope (they do, because it is in the prelude). But if you emit a type from a third-party crate like `serde::Serialize`, you must emit `::serde::Serialize` (absolute path) because the user may not have that in scope. The convention is to re-export your dependencies from the proc-macro's companion crate and use `::my_crate::__private::serde` in generated code.

**4. Mutating the annotated item in a derive macro.** Derive macros can only *add* items; they receive the original item as input but cannot change it. If you need to transform the item (wrap it, add fields, etc.), you need an attribute macro instead.

**5. Missing the test harness.** The standard way to test proc macros is `trybuild` for compile-fail tests ("this input should produce this error message") and `quote!` + your expansion function for unit tests of the generated tokens. Without tests, proc macro bugs surface only when users report them.

## Exercises

**Exercise 1** (30 min): Write a `#[derive(HelloWorld)]` macro that, when applied to any struct, generates an `impl HelloWorld for StructName` with a `hello_world()` method that prints `"Hello, World! I am StructName"`. Use `proc_macro2`, `quote`, and `syn`. Verify with a test binary.

**Exercise 2** (2-4h): Extend the `Builder` macro from this document to support an `#[builder(default = expr)]` field attribute that, instead of returning an error when the field is not set, uses the given expression. For example: `#[builder(default = "https://localhost")]` on a `url: String` field. Parse the attribute with `syn::parse::Parser` and use it in the `build()` method.

**Exercise 3** (4-8h): Implement a `#[derive(TypedBuilder)]` macro that enforces at compile time (using the typestate pattern with phantom type parameters) that required fields have been set. Each setter should change the phantom type state from `Missing<FieldName>` to `Set<FieldName>`, and `build()` should only be callable when all required fields are `Set`. This requires generating different generic parameters for each possible builder state.

**Exercise 4** (8-15h): Implement a `#[derive(OrmModel)]` macro that generates: (1) a `TableName::table_name() -> &'static str` method (snake_case of struct name); (2) `to_row(&self) -> Vec<(&'static str, Box<dyn ToSql>)>` that pairs column names with values; (3) `from_row(row: &Row) -> Result<Self, OrmError>` that constructs the struct from a generic row type. Handle `Option<T>` fields (nullable columns) and `#[column(name = "override")]` attributes. Write `trybuild` tests that verify compile errors for unsupported field types.

## Further Reading

### Foundational Papers

- There are no papers on proc macros specifically. The relevant formalism is in the Rust Reference: [Macros By Example](https://doc.rust-lang.org/reference/macros-by-example.html) and [Procedural Macros](https://doc.rust-lang.org/reference/procedural-macros.html).

### Books

- [The Little Book of Rust Macros](https://veykril.github.io/tlborm/) — the definitive guide to both `macro_rules!` and proc macros. Free online.
- [Rust for Rustaceans (Jon Gjengset)](https://nostarch.com/rust-rustaceans) — Chapter 8 covers macros in production depth.

### Production Code to Read

- [`thiserror` source](https://github.com/dtolnay/thiserror/tree/master/impl/src) — small enough to read fully (~400 lines); covers derive macros with field attribute parsing.
- [`serde_derive` source](https://github.com/serde-rs/serde/tree/master/serde_derive/src) — the reference implementation; study `derive.rs` and `internals/attr.rs` for comprehensive attribute parsing.
- [`derive_builder` crate](https://github.com/colin-kiegel/rust-derive-builder) — a production version of the macro implemented in this document.
- [`async-trait` source](https://github.com/dtolnay/async-trait) — attribute macro that rewrites function signatures; shows how to handle complex syn transformations.

### Talks

- [David Tolnay: "Procedural Macros in Rust" (RustConf 2018)](https://www.youtube.com/watch?v=g4SYTOc8fL0) — the author of `syn` and `quote` explaining the design.
- [Jon Gjengset: "proc macros workshop" (Crust of Rust)](https://www.youtube.com/watch?v=geovSK3wMB8) — live implementation of a derive macro from scratch.
