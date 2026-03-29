<!-- difficulty: intermediate-advanced -->
<!-- category: functional-programming -->
<!-- languages: [rust] -->
<!-- concepts: [monads, functors, option-types, result-types, railway-oriented-programming, error-composition] -->
<!-- estimated_time: 6-8 hours -->
<!-- bloom_level: apply, analyze, evaluate -->
<!-- prerequisites: [rust-generics, trait-system, closures, error-handling-basics] -->

# Challenge 80: Functional Option and Result Types

## Languages

Rust (stable, latest edition)

## Prerequisites

- Solid understanding of Rust generics and trait bounds
- Familiarity with closures and `Fn`/`FnOnce` traits
- Basic knowledge of Rust's standard `Option` and `Result` types
- Understanding of trait composition and default method implementations

## Learning Objectives

- **Implement** custom `Option` and `Result` types from scratch without relying on the standard library types
- **Apply** functor and monad patterns through `map`, `flat_map`, and `filter` combinators
- **Design** a railway-oriented programming pipeline where operations chain cleanly through success/failure tracks
- **Analyze** how applicative functors differ from monads when combining multiple fallible values
- **Evaluate** trade-offs between early-return error handling and monadic composition

## The Challenge

Build a comprehensive functional programming toolkit around custom `Option` and `Result` types. You will not use Rust's standard `Option<T>` or `Result<T, E>` -- you will define your own from scratch. This exercise forces you to understand what these types actually do at the algebraic level, not just how to call methods on them.

Start with the core types: `Maybe<T>` (your custom Option) and `Outcome<T, E>` (your custom Result). Implement the full combinator vocabulary: `map`, `flat_map` (monadic bind), `filter`, `unwrap_or`, `and_then`, `or_else`, and `zip`. Then build higher-level patterns on top: railway-oriented pipelines, applicative combination of multiple `Outcome` values, and a `Try` trait that enables the `?`-like early return pattern.

The real challenge is not just implementing these methods -- it is ensuring they compose correctly. A `flat_map` followed by `map` followed by `filter` should work seamlessly. Error contexts should chain. The applicative functor should collect all errors, not just the first one.

## Requirements

1. Define `Maybe<T>` as an enum with `Just(T)` and `Nothing` variants -- do not use `std::option::Option`
2. Define `Outcome<T, E>` as an enum with `Success(T)` and `Failure(E)` variants -- do not use `std::result::Result`
3. Implement on `Maybe<T>`: `map`, `flat_map`, `filter`, `unwrap_or`, `unwrap_or_else`, `is_just`, `is_nothing`, `and_then`, `or_else`, `zip`
4. Implement on `Outcome<T, E>`: `map`, `map_err`, `flat_map`, `unwrap_or`, `and_then`, `or_else`, `zip`
5. Implement a `Functor` trait with `map` and provide implementations for both types
6. Implement a `Monad` trait with `bind` (flat_map) and `unit` (wrap a value), implemented for both types
7. Build `Railway<T, E>` -- a pipeline builder that chains operations, keeping success and failure tracks separate
8. Implement applicative combination: `Outcome::combine2`, `combine3`, `combine4` that take multiple `Outcome` values and a combining function, collecting all errors (not short-circuiting on the first)
9. Define a custom error type `ChainedError` that supports context chaining (wrapping errors with additional messages)
10. Write a practical pipeline example: validate user input (name, email, age) using railway-oriented style, collecting all validation errors

## Hints

<details>
<summary>Hint 1: Core enum definitions</summary>

```rust
#[derive(Debug, Clone, PartialEq)]
pub enum Maybe<T> {
    Just(T),
    Nothing,
}

#[derive(Debug, Clone, PartialEq)]
pub enum Outcome<T, E> {
    Success(T),
    Failure(E),
}
```

The key difference from `std::Option` and `std::Result` is naming. The implementations should mirror the standard library's behavior but under your control.

</details>

<details>
<summary>Hint 2: Railway pipeline structure</summary>

```rust
pub struct Railway<T, E> {
    state: Outcome<T, E>,
}

impl<T, E> Railway<T, E> {
    pub fn of(value: T) -> Self {
        Railway { state: Outcome::Success(value) }
    }

    pub fn then<U>(self, f: impl FnOnce(T) -> Outcome<U, E>) -> Railway<U, E> {
        Railway {
            state: self.state.flat_map(f),
        }
    }
}
```

Each `then` step stays on the success track if the previous step succeeded, or skips straight to the failure track.

</details>

<details>
<summary>Hint 3: Applicative error collection</summary>

For applicative combination, you need a way to accumulate errors. Use `Vec<E>` or a custom collection:

```rust
impl<T, E> Outcome<T, Vec<E>> {
    pub fn combine2<A, B>(
        a: Outcome<A, Vec<E>>,
        b: Outcome<B, Vec<E>>,
        f: impl FnOnce(A, B) -> T,
    ) -> Self {
        match (a, b) {
            (Success(a), Success(b)) => Success(f(a, b)),
            (Failure(mut e1), Failure(e2)) => { e1.extend(e2); Failure(e1) }
            (Failure(e), _) | (_, Failure(e)) => Failure(e),
        }
    }
}
```

</details>

<details>
<summary>Hint 4: Functor and Monad traits</summary>

Rust does not have higher-kinded types, so simulate them with associated types or separate trait impls:

```rust
pub trait Functor {
    type Inner;
    type Mapped<U>: Functor;
    fn fmap<U>(self, f: impl FnOnce(Self::Inner) -> U) -> Self::Mapped<U>;
}

pub trait Monad: Functor {
    fn unit(value: Self::Inner) -> Self;
    fn bind<U>(self, f: impl FnOnce(Self::Inner) -> Self::Mapped<U>) -> Self::Mapped<U>;
}
```

The `Mapped<U>` associated type is the key trick -- it represents "the same container but holding `U` instead."

</details>

## Acceptance Criteria

- [ ] `Maybe<T>` and `Outcome<T, E>` are defined as custom enums, not type aliases of `std::Option` or `std::Result`
- [ ] All combinators (`map`, `flat_map`, `filter`, `unwrap_or`, `zip`, etc.) work correctly on both types
- [ ] `Functor` and `Monad` traits are implemented for both `Maybe` and `Outcome`
- [ ] `Railway<T, E>` chains operations correctly, short-circuiting on failure
- [ ] Applicative `combine2`/`combine3`/`combine4` collect all errors when multiple values fail
- [ ] `ChainedError` supports wrapping errors with context messages
- [ ] The practical validation example demonstrates railway-oriented error collection
- [ ] All tests pass with `cargo test`

## Research Resources

- [Railway Oriented Programming (Scott Wlaschin)](https://fsharpforfunandprofit.com/rop/) -- the definitive introduction to railway-oriented error handling
- [Functors, Applicatives, and Monads in Pictures](https://adit.io/posts/2013-04-17-functors,_applicatives,_and_monads_in_pictures.html) -- visual intuition for these abstractions
- [Rust std::option Module Documentation](https://doc.rust-lang.org/std/option/) -- reference implementation for Option combinators
- [Higher-Kinded Types in Rust (blog)](https://hugopeters.me/posts/14/) -- techniques for simulating HKTs with GATs
- [Error Handling in Rust (Burntsushi)](https://blog.burntsushi.net/rust-error-handling/) -- practical patterns for error composition
- [The `anyhow` and `thiserror` crates](https://docs.rs/anyhow/latest/anyhow/) -- production error handling patterns for reference
