# 8. Procedural Macros

**Difficulty**: Avanzado

## Prerequisites
- Completed: 07-declarative-macros
- Familiarity with: `macro_rules!`, derive traits, attributes, `Result`, AST concepts

## Learning Objectives
- Design and implement custom derive macros using syn and quote
- Analyze the proc-macro compilation model (separate crate, token streams)
- Evaluate the three proc-macro types and choose the right one for a given problem
- Implement compile-time error reporting with useful diagnostics

## Concepts

### Three Types of Procedural Macros

| Type | Syntax | Input | Use Case |
|------|--------|-------|----------|
| **Derive** | `#[derive(MyTrait)]` | The struct/enum definition | Auto-implement traits |
| **Attribute** | `#[my_attr(...)]` | The annotated item | Transform/augment items |
| **Function-like** | `my_macro!(...)` | Arbitrary tokens | DSLs, SQL, HTML templates |

All three follow the same core model: `TokenStream` in, `TokenStream` out.

### Crate Setup

Proc macros must live in a dedicated crate with `proc-macro = true`. This is a hard requirement -- the compiler loads proc-macro crates as plugins.

```
my-project/
  Cargo.toml
  src/
    main.rs          # uses the derive
  my-derive/
    Cargo.toml       # proc-macro crate
    src/
      lib.rs         # derive implementation
```

**my-derive/Cargo.toml**:
```toml
[package]
name = "my-derive"
version = "0.1.0"
edition = "2021"

[lib]
proc-macro = true

[dependencies]
syn = { version = "2", features = ["full"] }
quote = "1"
proc-macro2 = "1"
```

**my-project/Cargo.toml**:
```toml
[dependencies]
my-derive = { path = "./my-derive" }
```

### syn: Parsing Rust Code

`syn` parses a `TokenStream` into a typed AST. The key types:

```rust
use syn::{DeriveInput, Data, Fields, Ident, Type};

// Parse the token stream into a DeriveInput
let input: DeriveInput = syn::parse(tokens).unwrap();

// input.ident -- the name of the struct/enum
// input.generics -- generic parameters
// input.data -- Data::Struct, Data::Enum, or Data::Union
```

For a struct like:
```rust
#[derive(MyDerive)]
struct User {
    name: String,
    age: u32,
}
```

`syn` gives you structured access to the name (`User`), each field's name (`name`, `age`), and each field's type (`String`, `u32`).

### quote: Generating Rust Code

`quote` converts Rust-like syntax back into a `TokenStream`:

```rust
use quote::quote;

let name = &input.ident;

let expanded = quote! {
    impl #name {
        fn hello(&self) {
            println!("Hello from {}", stringify!(#name));
        }
    }
};
```

`#name` interpolates a variable. `#(#fields)*` iterates. This is the inverse of `syn` -- you build code from templates.

### Derive Macro Walkthrough

Let's implement a `Describe` derive that generates a method returning a string description of a struct's fields:

```rust
// What users write:
#[derive(Describe)]
struct Config {
    host: String,
    port: u16,
    debug: bool,
}

// What they get:
// impl Config {
//     fn describe() -> &'static str {
//         "Config { host: String, port: u16, debug: bool }"
//     }
// }
```

**my-derive/src/lib.rs**:
```rust
use proc_macro::TokenStream;
use quote::quote;
use syn::{parse_macro_input, DeriveInput, Data, Fields};

#[proc_macro_derive(Describe)]
pub fn derive_describe(input: TokenStream) -> TokenStream {
    let input = parse_macro_input!(input as DeriveInput);
    let name = &input.ident;

    let field_descriptions = match &input.data {
        Data::Struct(data) => match &data.fields {
            Fields::Named(fields) => {
                let descs: Vec<String> = fields.named.iter().map(|f| {
                    let fname = f.ident.as_ref().unwrap();
                    let ftype = &f.ty;
                    format!("{}: {}", fname, quote!(#ftype))
                }).collect();
                descs.join(", ")
            }
            _ => String::from("(unnamed fields)"),
        },
        Data::Enum(_) => String::from("(enum)"),
        Data::Union(_) => String::from("(union)"),
    };

    let description = format!("{} {{ {} }}", name, field_descriptions);

    let expanded = quote! {
        impl #name {
            pub fn describe() -> &'static str {
                #description
            }
        }
    };

    TokenStream::from(expanded)
}
```

### Handling Generics

If the input struct has generics, your generated impl must include them:

```rust
let (impl_generics, ty_generics, where_clause) = input.generics.split_for_impl();

let expanded = quote! {
    impl #impl_generics MyTrait for #name #ty_generics #where_clause {
        // ...
    }
};
```

`split_for_impl()` is the standard pattern. It handles lifetimes, type parameters, and where clauses correctly.

### Attribute Macros

An attribute macro receives two token streams: the attribute arguments and the annotated item:

```rust
#[proc_macro_attribute]
pub fn my_attribute(attr: TokenStream, item: TokenStream) -> TokenStream {
    // attr = the arguments inside #[my_attribute(...)]
    // item = the function/struct/etc. being annotated
    // Return the modified item
    item // pass-through if no modification
}
```

Example: a `#[log_calls]` attribute that wraps a function with logging:

```rust
#[proc_macro_attribute]
pub fn log_calls(_attr: TokenStream, item: TokenStream) -> TokenStream {
    let input = parse_macro_input!(item as syn::ItemFn);
    let name = &input.sig.ident;
    let block = &input.block;
    let sig = &input.sig;
    let vis = &input.vis;

    let expanded = quote! {
        #vis #sig {
            println!("[CALL] {} entered", stringify!(#name));
            let __result = (|| #block)();
            println!("[CALL] {} exited", stringify!(#name));
            __result
        }
    };

    TokenStream::from(expanded)
}
```

### Error Reporting

Never `panic!` in a proc macro -- it gives a terrible error message. Use `syn::Error` for precise, user-friendly errors:

```rust
use syn::Error;
use syn::spanned::Spanned;

fn validate_fields(fields: &Fields) -> Result<(), syn::Error> {
    match fields {
        Fields::Named(_) => Ok(()),
        other => Err(Error::new(
            other.span(),
            "Describe only supports structs with named fields"
        )),
    }
}

// In the derive function:
#[proc_macro_derive(Describe)]
pub fn derive_describe(input: TokenStream) -> TokenStream {
    let input = parse_macro_input!(input as DeriveInput);

    match impl_describe(&input) {
        Ok(tokens) => tokens.into(),
        Err(err) => err.to_compile_error().into(),
    }
}

fn impl_describe(input: &DeriveInput) -> syn::Result<proc_macro2::TokenStream> {
    // Use ? to propagate errors with good spans
    let fields = match &input.data {
        Data::Struct(data) => &data.fields,
        _ => return Err(Error::new(input.ident.span(), "expected a struct")),
    };
    validate_fields(fields)?;
    // ... generate code ...
    Ok(quote! { /* ... */ })
}
```

The error points to the exact span in the user's code. This is what separates a professional proc macro from a frustrating one.

### Testing Proc Macros

There are multiple approaches:

**Integration tests** (compile-and-run):
```rust
// tests/basic.rs
use my_derive::Describe;

#[derive(Describe)]
struct Point { x: f64, y: f64 }

#[test]
fn test_describe() {
    assert!(Point::describe().contains("x"));
    assert!(Point::describe().contains("f64"));
}
```

**trybuild** (compile-fail tests):
```toml
[dev-dependencies]
trybuild = "1"
```

```rust
#[test]
fn compile_tests() {
    let t = trybuild::TestCases::new();
    t.pass("tests/pass/*.rs");
    t.compile_fail("tests/fail/*.rs");
}
```

Create test files that should fail with specific error messages:
```rust
// tests/fail/enum_not_supported.rs
use my_derive::Describe;

#[derive(Describe)]
enum Color { Red, Green, Blue }

fn main() {}
```

```
// tests/fail/enum_not_supported.stderr
error: expected a struct
 --> tests/fail/enum_not_supported.rs:4:6
  |
4 | enum Color { Red, Green, Blue }
  |      ^^^^^
```

`trybuild` compares actual compiler output against the `.stderr` file.

## Exercises

### Exercise 1: Custom Derive -- EnumVariants

**Problem**: Implement a `#[derive(EnumVariants)]` that generates:
- A `fn variants() -> &'static [&'static str]` returning all variant names.
- A `fn variant_name(&self) -> &'static str` returning the current variant's name.

```rust
#[derive(EnumVariants)]
enum HttpMethod {
    Get,
    Post,
    Put,
    Delete,
    Patch,
}

assert_eq!(HttpMethod::variants(), &["Get", "Post", "Put", "Delete", "Patch"]);
assert_eq!(HttpMethod::Get.variant_name(), "Get");
```

**Hints**:
- Parse as `DeriveInput`, match on `Data::Enum`.
- Iterate `data.variants` to get each `Variant`.
- For `variant_name`, generate a `match self` with one arm per variant.
- Handle unit variants, tuple variants, and struct variants (use `..` pattern to ignore fields).
- Return a proper error for structs and unions.

**One possible solution** for the derive crate:

```rust
use proc_macro::TokenStream;
use quote::quote;
use syn::{parse_macro_input, Data, DeriveInput, Error, Fields};

#[proc_macro_derive(EnumVariants)]
pub fn derive_enum_variants(input: TokenStream) -> TokenStream {
    let input = parse_macro_input!(input as DeriveInput);
    match impl_enum_variants(&input) {
        Ok(ts) => ts.into(),
        Err(e) => e.to_compile_error().into(),
    }
}

fn impl_enum_variants(input: &DeriveInput) -> syn::Result<proc_macro2::TokenStream> {
    let name = &input.ident;
    let (impl_generics, ty_generics, where_clause) = input.generics.split_for_impl();

    let variants = match &input.data {
        Data::Enum(data) => &data.variants,
        _ => return Err(Error::new(name.span(), "EnumVariants only works on enums")),
    };

    let variant_names: Vec<String> = variants.iter()
        .map(|v| v.ident.to_string())
        .collect();

    let match_arms = variants.iter().map(|v| {
        let ident = &v.ident;
        let name_str = ident.to_string();
        let pattern = match &v.fields {
            Fields::Unit => quote! { Self::#ident },
            Fields::Unnamed(_) => quote! { Self::#ident(..) },
            Fields::Named(_) => quote! { Self::#ident { .. } },
        };
        quote! { #pattern => #name_str }
    });

    Ok(quote! {
        impl #impl_generics #name #ty_generics #where_clause {
            pub fn variants() -> &'static [&'static str] {
                &[ #( #variant_names ),* ]
            }

            pub fn variant_name(&self) -> &'static str {
                match self {
                    #( #match_arms, )*
                }
            }
        }
    })
}
```

### Exercise 2: Attribute Macro -- #[timed]

**Problem**: Create an attribute macro `#[timed]` that wraps a function to measure and print its execution time:

```rust
#[timed]
fn compute_heavy(n: u64) -> u64 {
    (0..n).sum()
}
// Prints: "[timed] compute_heavy took 1.23ms"
```

It should work with both sync and async functions. For async, it wraps the entire async body.

**Hints**:
- Parse as `syn::ItemFn`.
- Check `sig.asyncness` to determine if it's async.
- For async: wrap the body in `async move { ... }` with timing around it.
- Preserve the original function signature, visibility, and attributes.

**One possible solution** (sync only, async extension left as challenge):

```rust
use proc_macro::TokenStream;
use quote::quote;
use syn::{parse_macro_input, ItemFn};

#[proc_macro_attribute]
pub fn timed(_attr: TokenStream, item: TokenStream) -> TokenStream {
    let input = parse_macro_input!(item as ItemFn);
    let vis = &input.vis;
    let sig = &input.sig;
    let block = &input.block;
    let name = &sig.ident;
    let name_str = name.to_string();

    let expanded = if sig.asyncness.is_some() {
        quote! {
            #vis #sig {
                let __start = std::time::Instant::now();
                let __result = async move #block .await;
                let __elapsed = __start.elapsed();
                println!("[timed] {} took {:.2?}", #name_str, __elapsed);
                __result
            }
        }
    } else {
        quote! {
            #vis #sig {
                let __start = std::time::Instant::now();
                let __result = (|| #block)();
                let __elapsed = __start.elapsed();
                println!("[timed] {} took {:.2?}", #name_str, __elapsed);
                __result
            }
        }
    };

    TokenStream::from(expanded)
}
```

### Exercise 3: Full Derive with Validation (Design Challenge)

**Problem**: Design and implement a `#[derive(Validate)]` macro for form/API input validation:

```rust
#[derive(Validate)]
struct CreateUser {
    #[validate(min_length = 3, max_length = 50)]
    username: String,
    #[validate(email)]
    email: String,
    #[validate(range(min = 18, max = 150))]
    age: u32,
}

let user = CreateUser { /* ... */ };
user.validate()?; // Returns Result<(), Vec<ValidationError>>
```

This requires:
- A helper attribute `#[validate(...)]` on fields -- use `#[proc_macro_derive(Validate, attributes(validate))]`.
- Parsing custom attribute syntax (syn's `Meta` and `NestedMeta`).
- Generating validation logic per field based on the attribute parameters.
- Good error messages for invalid attribute usage.

Design the attribute syntax, the generated code shape, and the error type. Implement the core. Compare your design against the `validator` crate.

## Design Decisions

**syn "full" vs "derive"**: `syn` with `features = ["full"]` parses all Rust syntax. For derive macros, you often only need `features = ["derive"]` (smaller compile time). Use "full" only if you parse function bodies or complex items.

**proc-macro2 vs proc-macro**: `proc_macro` only works inside proc-macro crates. `proc_macro2` is a wrapper that works in unit tests too. Use `proc_macro2` for internal logic, convert at the boundaries.

**Error quality as a feature**: The difference between a good proc macro and a bad one is entirely in the error messages. Spend time getting spans right. Use `syn::Error::new_spanned` to point errors at the exact token. Users will thank you.

**Testing strategy**: Unit test the parsing and code generation logic using `proc_macro2`. Use `trybuild` for integration and compile-fail tests. This gives you fast iteration (unit tests) and correctness confidence (integration tests).

## Common Mistakes

1. **Forgetting `proc-macro = true`** in Cargo.toml -- the crate compiles but `#[proc_macro_derive]` is not recognized.
2. **Panicking instead of returning errors** -- `unwrap()` in a proc macro produces "proc macro panicked" with no useful context. Always use `syn::Error`.
3. **Not handling generics** -- your derive works for `struct Foo` but crashes on `struct Foo<T>`. Always use `split_for_impl()`.
4. **Generating code that references crate-local items** -- the generated code runs in the user's crate. Use fully qualified paths (`::std::fmt::Display`) or re-export from a helper crate.
5. **Not testing compile-fail cases** -- happy-path tests pass, but users with wrong input get incomprehensible errors. `trybuild` catches this.

## Summary

- Proc macros operate on `TokenStream` and live in dedicated crates with `proc-macro = true`.
- `syn` parses tokens into AST, `quote` generates tokens from templates. Together they handle 95% of proc-macro work.
- Derive macros auto-implement traits; attribute macros transform items; function-like macros are arbitrary DSLs.
- Error reporting via `syn::Error` with correct spans is non-negotiable for usable macros.
- Test with both `trybuild` (compile-fail) and integration tests (behavior).
- Proc macros can do everything `macro_rules!` can't: string manipulation, type inspection, complex logic, attribute parsing.

## What's Next

With concurrency (threads, async, tokio) and metaprogramming (declarative and procedural macros) covered, you have the tools for serious Rust systems programming. Apply these to real projects: build a web service with tokio, create a derive macro for your domain types, or combine both in a production system.

## Resources

- [syn docs](https://docs.rs/syn)
- [quote docs](https://docs.rs/quote)
- [trybuild docs](https://docs.rs/trybuild)
- [Proc Macro Workshop (dtolnay)](https://github.com/dtolnay/proc-macro-workshop) -- hands-on exercises
- [The Reference: Procedural Macros](https://doc.rust-lang.org/reference/procedural-macros.html)
- [Rain's Rust CLI: Derive Macro Tutorial](https://blog.turbo.fish/proc-macro-simple-derive/)
