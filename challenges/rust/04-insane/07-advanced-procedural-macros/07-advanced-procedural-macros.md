# 7. Advanced Procedural Macros

**Difficulty**: Insane

## The Challenge

Build a procedural macro crate that takes a Rust struct annotated with custom attributes and generates an entire typed REST client module. Given something like:

```rust
#[derive(ApiClient)]
#[api(base_url = "https://api.example.com/v1")]
struct UserService {
    #[endpoint(GET, path = "/users/{id}")]
    get_user: fn(id: u64) -> User,

    #[endpoint(POST, path = "/users", body)]
    create_user: fn(payload: CreateUserRequest) -> User,

    #[endpoint(GET, path = "/users", query)]
    list_users: fn(params: ListParams) -> Vec<User>,

    #[endpoint(DELETE, path = "/users/{id}")]
    delete_user: fn(id: u64) -> (),
}
```

Your macro generates:
- A `UserServiceClient` struct with an inner `reqwest::Client` and base URL.
- An async method for each endpoint with correct HTTP verb, path interpolation, body serialization, query parameter encoding, and typed deserialization.
- A builder for constructing the client with headers, timeouts, and middleware.
- A companion trait so users can mock the client in tests.
- Proper `compile_error!` diagnostics for malformed input (wrong types, missing attributes, conflicting options).

This is how real crates like `sqlx`, `serde`, and `async-trait` work. You are building production-grade codegen.

## Acceptance Criteria

- [ ] Proc macro crate compiles and is usable as a dependency from a separate crate
- [ ] `#[derive(ApiClient)]` parses the struct and all `#[endpoint(...)]` attributes without panicking on valid input
- [ ] Generates async methods with correct signatures: path params extracted from `{param}` syntax, body/query handling based on attribute flags
- [ ] Generated code compiles and runs against a real or mocked HTTP server (use `wiremock` or `mockito`)
- [ ] Supports generic structs: `struct MyService<A: Serialize>` propagates bounds correctly into generated impls
- [ ] Attribute parsing uses `syn::parse::Parse` with custom keywords (not string matching)
- [ ] At least 5 `trybuild` compile-fail tests covering: missing required attributes, type mismatches, duplicate endpoints, invalid path syntax, conflicting body+query on GET
- [ ] At least 3 `trybuild` compile-pass tests for valid usage
- [ ] `cargo expand` output for a sample input is clean and readable
- [ ] Error messages point to the correct span (use `proc-macro2::Span` and `proc-macro-error` or `syn::Error`)

## Starting Points

- Read `serde_derive/src/de.rs` — study how it walks struct fields and generates `Deserialize` impls. Pay attention to how it handles generics via `syn::Generics` and `where` clause construction.
- Read `async-trait/src/lib.rs` — study how it rewrites function signatures, particularly the lifetime elision and `Box<dyn Future>` desugaring.
- Read `sqlx-macros/src/derives/` — study the attribute parsing patterns with custom `Parse` implementations.
- RFC 1566 (proc macros v1.1) and the `proc_macro` rustdoc for the raw token stream API.
- The `quote` crate's interpolation rules: `#var`, `#(#iter)*`, `#(#iter),*` repetition syntax.

## Hints

1. Structure your macro crate as three layers: parsing (custom `syn::parse::Parse` impls that build your own IR), validation (check invariants and emit `syn::Error` with correct spans), and codegen (`quote!` blocks that emit the final tokens). Keep them in separate modules.

2. Generics are the hard part. You need to add trait bounds to the generated impl. Study `syn::Generics::split_for_impl()` and how to merge user-provided bounds with your own requirements (e.g., `T: serde::Serialize`).

3. For path interpolation (`/users/{id}`), parse the path string at macro expansion time, extract parameter names, match them against function arguments, and generate `format!()` calls. This is string processing inside the macro, not at runtime.

4. `proc-macro-error` (or the built-in `Diagnostic` on nightly) lets you attach errors to specific spans. When a user writes `#[endpoint(PATCH, path = "/x")]` and PATCH is unsupported, the error should underline `PATCH`, not the entire attribute.

5. `trybuild` works by comparing compiler output. Your compile-fail tests should assert specific error messages. Write the test cases first — they define your macro's contract.

## Going Further

- Add middleware support: `#[api(middleware = LoggingMiddleware)]` that wraps each generated method call.
- Add retry policies per endpoint: `#[endpoint(GET, path = "/users/{id}", retry = 3)]`.
- Generate an OpenAPI spec from the macro input (inverse direction).
- Add compile-time URL validation (check that path params match function args, reject paths with unbalanced braces).
- Implement a `#[mock]` attribute that generates a mock implementation using `mockall` patterns.

## Resources

- **Source**: `serde_derive` — [github.com/serde-rs/serde/tree/master/serde_derive](https://github.com/serde-rs/serde/tree/master/serde_derive)
- **Source**: `async-trait` — [github.com/dtolnay/async-trait](https://github.com/dtolnay/async-trait)
- **Source**: `sqlx-macros` — [github.com/launchbadge/sqlx/tree/main/sqlx-macros](https://github.com/launchbadge/sqlx/tree/main/sqlx-macros)
- **Crate**: `syn` — [docs.rs/syn](https://docs.rs/syn) — read the `parse` module documentation exhaustively
- **Crate**: `quote` — [docs.rs/quote](https://docs.rs/quote)
- **Crate**: `proc-macro2` — [docs.rs/proc-macro2](https://docs.rs/proc-macro2)
- **Crate**: `trybuild` — [docs.rs/trybuild](https://docs.rs/trybuild)
- **Crate**: `proc-macro-error` — [docs.rs/proc-macro-error](https://docs.rs/proc-macro-error)
- **Tool**: `cargo expand` — [github.com/dtolnay/cargo-expand](https://github.com/dtolnay/cargo-expand)
- **Talk**: David Tolnay — "Procedural Macros vs Sliced Bread" (RustConf 2018)
- **Blog**: Amos (fasterthanlime) — "Procedural Macros: The Ones I Actually Use"
- **Book**: Jon Gjengset — *Rust for Rustaceans*, Chapter 3 (Macros)
