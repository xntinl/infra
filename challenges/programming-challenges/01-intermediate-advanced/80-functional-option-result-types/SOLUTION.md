# Solution: Functional Option and Result Types

## Architecture Overview

The solution is structured in four layers:

1. **Core types** -- `Maybe<T>` and `Outcome<T, E>` with full combinator vocabularies
2. **Algebraic traits** -- `Functor` and `Monad` traits using GATs to simulate higher-kinded types
3. **Railway pipeline** -- `Railway<T, E>` builder for chaining fallible operations
4. **Applicative combination** -- `combine2`/`combine3`/`combine4` that collect all errors instead of short-circuiting

Each layer builds on the previous. The core types are self-contained enums with methods. The traits abstract over them. The railway and applicative patterns compose them into real-world pipelines.

## Rust Solution

### Project Setup

```bash
cargo new functional-toolkit
cd functional-toolkit
```

```toml
[package]
name = "functional-toolkit"
version = "0.1.0"
edition = "2021"
```

### Source: `src/maybe.rs`

```rust
/// Custom Option type -- not an alias for std::option::Option.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Maybe<T> {
    Just(T),
    Nothing,
}

use Maybe::*;

impl<T> Maybe<T> {
    pub fn is_just(&self) -> bool {
        matches!(self, Just(_))
    }

    pub fn is_nothing(&self) -> bool {
        matches!(self, Nothing)
    }

    pub fn map<U>(self, f: impl FnOnce(T) -> U) -> Maybe<U> {
        match self {
            Just(v) => Just(f(v)),
            Nothing => Nothing,
        }
    }

    pub fn flat_map<U>(self, f: impl FnOnce(T) -> Maybe<U>) -> Maybe<U> {
        match self {
            Just(v) => f(v),
            Nothing => Nothing,
        }
    }

    pub fn and_then<U>(self, f: impl FnOnce(T) -> Maybe<U>) -> Maybe<U> {
        self.flat_map(f)
    }

    pub fn filter(self, predicate: impl FnOnce(&T) -> bool) -> Maybe<T> {
        match self {
            Just(ref v) if predicate(v) => self,
            _ => Nothing,
        }
    }

    pub fn unwrap_or(self, default: T) -> T {
        match self {
            Just(v) => v,
            Nothing => default,
        }
    }

    pub fn unwrap_or_else(self, f: impl FnOnce() -> T) -> T {
        match self {
            Just(v) => v,
            Nothing => f(),
        }
    }

    pub fn or_else(self, f: impl FnOnce() -> Maybe<T>) -> Maybe<T> {
        match self {
            Just(_) => self,
            Nothing => f(),
        }
    }

    pub fn zip<U>(self, other: Maybe<U>) -> Maybe<(T, U)> {
        match (self, other) {
            (Just(a), Just(b)) => Just((a, b)),
            _ => Nothing,
        }
    }

    pub fn to_outcome<E>(self, err: E) -> crate::outcome::Outcome<T, E> {
        match self {
            Just(v) => crate::outcome::Outcome::Success(v),
            Nothing => crate::outcome::Outcome::Failure(err),
        }
    }
}
```

### Source: `src/outcome.rs`

```rust
/// Custom Result type -- not an alias for std::result::Result.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Outcome<T, E> {
    Success(T),
    Failure(E),
}

use Outcome::*;

impl<T, E> Outcome<T, E> {
    pub fn is_success(&self) -> bool {
        matches!(self, Success(_))
    }

    pub fn is_failure(&self) -> bool {
        matches!(self, Failure(_))
    }

    pub fn map<U>(self, f: impl FnOnce(T) -> U) -> Outcome<U, E> {
        match self {
            Success(v) => Success(f(v)),
            Failure(e) => Failure(e),
        }
    }

    pub fn map_err<F>(self, f: impl FnOnce(E) -> F) -> Outcome<T, F> {
        match self {
            Success(v) => Success(v),
            Failure(e) => Failure(f(e)),
        }
    }

    pub fn flat_map<U>(self, f: impl FnOnce(T) -> Outcome<U, E>) -> Outcome<U, E> {
        match self {
            Success(v) => f(v),
            Failure(e) => Failure(e),
        }
    }

    pub fn and_then<U>(self, f: impl FnOnce(T) -> Outcome<U, E>) -> Outcome<U, E> {
        self.flat_map(f)
    }

    pub fn or_else<F>(self, f: impl FnOnce(E) -> Outcome<T, F>) -> Outcome<T, F> {
        match self {
            Success(v) => Success(v),
            Failure(e) => f(e),
        }
    }

    pub fn unwrap_or(self, default: T) -> T {
        match self {
            Success(v) => v,
            Failure(_) => default,
        }
    }

    pub fn unwrap_or_else(self, f: impl FnOnce(E) -> T) -> T {
        match self {
            Success(v) => v,
            Failure(e) => f(e),
        }
    }

    pub fn zip<U>(self, other: Outcome<U, E>) -> Outcome<(T, U), E> {
        match (self, other) {
            (Success(a), Success(b)) => Success((a, b)),
            (Failure(e), _) | (_, Failure(e)) => Failure(e),
        }
    }

    pub fn to_maybe(self) -> crate::maybe::Maybe<T> {
        match self {
            Success(v) => crate::maybe::Maybe::Just(v),
            Failure(_) => crate::maybe::Maybe::Nothing,
        }
    }
}

// --- Applicative combinators: collect ALL errors ---

impl<T, E> Outcome<T, Vec<E>> {
    pub fn combine2<A, B>(
        a: Outcome<A, Vec<E>>,
        b: Outcome<B, Vec<E>>,
        f: impl FnOnce(A, B) -> T,
    ) -> Self {
        match (a, b) {
            (Success(a), Success(b)) => Success(f(a, b)),
            (Failure(mut e1), Failure(e2)) => {
                e1.extend(e2);
                Failure(e1)
            }
            (Failure(e), _) | (_, Failure(e)) => Failure(e),
        }
    }

    pub fn combine3<A, B, C>(
        a: Outcome<A, Vec<E>>,
        b: Outcome<B, Vec<E>>,
        c: Outcome<C, Vec<E>>,
        f: impl FnOnce(A, B, C) -> T,
    ) -> Self {
        let mut errors: Vec<E> = Vec::new();
        let mut vals: (Option<A>, Option<B>, Option<C>) = (None, None, None);

        match a {
            Success(v) => vals.0 = Some(v),
            Failure(e) => errors.extend(e),
        }
        match b {
            Success(v) => vals.1 = Some(v),
            Failure(e) => errors.extend(e),
        }
        match c {
            Success(v) => vals.2 = Some(v),
            Failure(e) => errors.extend(e),
        }

        if errors.is_empty() {
            Success(f(vals.0.unwrap(), vals.1.unwrap(), vals.2.unwrap()))
        } else {
            Failure(errors)
        }
    }

    pub fn combine4<A, B, C, D>(
        a: Outcome<A, Vec<E>>,
        b: Outcome<B, Vec<E>>,
        c: Outcome<C, Vec<E>>,
        d: Outcome<D, Vec<E>>,
        f: impl FnOnce(A, B, C, D) -> T,
    ) -> Self {
        let mut errors: Vec<E> = Vec::new();
        let mut vals: (Option<A>, Option<B>, Option<C>, Option<D>) = (None, None, None, None);

        match a {
            Success(v) => vals.0 = Some(v),
            Failure(e) => errors.extend(e),
        }
        match b {
            Success(v) => vals.1 = Some(v),
            Failure(e) => errors.extend(e),
        }
        match c {
            Success(v) => vals.2 = Some(v),
            Failure(e) => errors.extend(e),
        }
        match d {
            Success(v) => vals.3 = Some(v),
            Failure(e) => errors.extend(e),
        }

        if errors.is_empty() {
            Success(f(
                vals.0.unwrap(),
                vals.1.unwrap(),
                vals.2.unwrap(),
                vals.3.unwrap(),
            ))
        } else {
            Failure(errors)
        }
    }
}
```

### Source: `src/traits.rs`

```rust
use crate::maybe::Maybe;
use crate::outcome::Outcome;

/// Functor: a type that can be mapped over.
/// Uses GATs to simulate higher-kinded types.
pub trait Functor {
    type Inner;
    type Mapped<U>: Functor;

    fn fmap<U>(self, f: impl FnOnce(Self::Inner) -> U) -> Self::Mapped<U>;
}

/// Monad: a type that supports sequencing of computations.
pub trait Monad: Functor {
    fn unit(value: Self::Inner) -> Self;
    fn bind<U>(self, f: impl FnOnce(Self::Inner) -> Self::Mapped<U>) -> Self::Mapped<U>;
}

// --- Functor for Maybe ---

impl<T> Functor for Maybe<T> {
    type Inner = T;
    type Mapped<U> = Maybe<U>;

    fn fmap<U>(self, f: impl FnOnce(T) -> U) -> Maybe<U> {
        self.map(f)
    }
}

// --- Monad for Maybe ---

impl<T> Monad for Maybe<T> {
    fn unit(value: T) -> Self {
        Maybe::Just(value)
    }

    fn bind<U>(self, f: impl FnOnce(T) -> Maybe<U>) -> Maybe<U> {
        self.flat_map(f)
    }
}

// --- Functor for Outcome ---

impl<T, E> Functor for Outcome<T, E> {
    type Inner = T;
    type Mapped<U> = Outcome<U, E>;

    fn fmap<U>(self, f: impl FnOnce(T) -> U) -> Outcome<U, E> {
        self.map(f)
    }
}

// --- Monad for Outcome ---

impl<T, E> Monad for Outcome<T, E> {
    fn unit(value: T) -> Self {
        Outcome::Success(value)
    }

    fn bind<U>(self, f: impl FnOnce(T) -> Outcome<U, E>) -> Outcome<U, E> {
        self.flat_map(f)
    }
}
```

### Source: `src/railway.rs`

```rust
use crate::outcome::Outcome;

/// Railway-oriented programming pipeline.
/// Operations chain along a success track; a failure derails
/// all subsequent steps until the pipeline is consumed.
pub struct Railway<T, E> {
    state: Outcome<T, E>,
}

impl<T, E> Railway<T, E> {
    pub fn of(value: T) -> Self {
        Railway {
            state: Outcome::Success(value),
        }
    }

    pub fn from_outcome(outcome: Outcome<T, E>) -> Self {
        Railway { state: outcome }
    }

    /// Chain a fallible operation. Skips if already on failure track.
    pub fn then<U>(self, f: impl FnOnce(T) -> Outcome<U, E>) -> Railway<U, E> {
        Railway {
            state: self.state.flat_map(f),
        }
    }

    /// Transform the success value. Skips if already on failure track.
    pub fn map<U>(self, f: impl FnOnce(T) -> U) -> Railway<U, E> {
        Railway {
            state: self.state.map(f),
        }
    }

    /// Handle a failure, potentially recovering to the success track.
    pub fn recover(self, f: impl FnOnce(E) -> Outcome<T, E>) -> Railway<T, E> {
        Railway {
            state: self.state.or_else(f),
        }
    }

    /// Tap into the success value for side effects (logging, etc.).
    pub fn inspect(self, f: impl FnOnce(&T)) -> Railway<T, E> {
        if let Outcome::Success(ref v) = self.state {
            f(v);
        }
        self
    }

    /// Tap into the failure value for side effects.
    pub fn inspect_err(self, f: impl FnOnce(&E)) -> Railway<T, E> {
        if let Outcome::Failure(ref e) = self.state {
            f(e);
        }
        self
    }

    /// Consume the railway, returning the underlying Outcome.
    pub fn finish(self) -> Outcome<T, E> {
        self.state
    }
}
```

### Source: `src/chained_error.rs`

```rust
use std::fmt;

/// An error that carries a chain of contextual messages.
/// Each layer adds context about what was happening when the error occurred.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ChainedError {
    messages: Vec<String>,
}

impl ChainedError {
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            messages: vec![message.into()],
        }
    }

    /// Wrap this error with additional context.
    pub fn context(mut self, message: impl Into<String>) -> Self {
        self.messages.push(message.into());
        self
    }

    /// Get the root cause (the first/innermost error message).
    pub fn root_cause(&self) -> &str {
        self.messages.first().map(|s| s.as_str()).unwrap_or("")
    }

    /// Get all messages from innermost to outermost.
    pub fn chain(&self) -> &[String] {
        &self.messages
    }
}

impl fmt::Display for ChainedError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for (i, msg) in self.messages.iter().rev().enumerate() {
            if i > 0 {
                write!(f, ": ")?;
            }
            write!(f, "{msg}")?;
        }
        Ok(())
    }
}

/// Extension trait to add context to any Outcome containing a ChainedError.
pub trait OutcomeContext<T> {
    fn with_context(self, msg: impl Into<String>) -> crate::outcome::Outcome<T, ChainedError>;
}

impl<T> OutcomeContext<T> for crate::outcome::Outcome<T, ChainedError> {
    fn with_context(self, msg: impl Into<String>) -> crate::outcome::Outcome<T, ChainedError> {
        self.map_err(|e| e.context(msg))
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod maybe;
pub mod outcome;
pub mod traits;
pub mod railway;
pub mod chained_error;
```

### Source: `src/main.rs`

```rust
use functional_toolkit::maybe::Maybe;
use functional_toolkit::outcome::Outcome;
use functional_toolkit::railway::Railway;
use functional_toolkit::chained_error::ChainedError;
use functional_toolkit::traits::{Functor, Monad};

fn main() {
    println!("=== Maybe Combinators ===\n");

    let val: Maybe<i32> = Maybe::Just(42);
    let doubled = val.map(|x| x * 2);
    println!("Just(42).map(|x| x * 2) = {:?}", doubled);

    let chained = Maybe::Just(10)
        .flat_map(|x| if x > 5 { Maybe::Just(x + 1) } else { Maybe::Nothing })
        .map(|x| x * 3);
    println!("Just(10).flat_map(>5 -> +1).map(*3) = {:?}", chained);

    let filtered = Maybe::Just(7).filter(|x| *x % 2 == 0);
    println!("Just(7).filter(even) = {:?}", filtered);

    let zipped = Maybe::Just("hello").zip(Maybe::Just(42));
    println!("Just(\"hello\").zip(Just(42)) = {:?}", zipped);

    println!("\n=== Outcome Combinators ===\n");

    let result: Outcome<i32, String> = Outcome::Success(100);
    let mapped = result.map(|x| x / 4);
    println!("Success(100).map(|x| x / 4) = {:?}", mapped);

    let fail: Outcome<i32, String> = Outcome::Failure("division by zero".into());
    let recovered = fail.unwrap_or(0);
    println!("Failure(\"division by zero\").unwrap_or(0) = {}", recovered);

    println!("\n=== Functor and Monad Traits ===\n");

    let via_functor = Maybe::Just(5).fmap(|x| x.to_string());
    println!("Maybe::fmap(5 -> string) = {:?}", via_functor);

    let via_monad = Maybe::unit(10).bind(|x| {
        if x > 0 { Maybe::Just(x * 2) } else { Maybe::Nothing }
    });
    println!("Maybe::unit(10).bind(>0 -> *2) = {:?}", via_monad);

    let outcome_functor: Outcome<_, String> = Outcome::unit(42).fmap(|x| x + 8);
    println!("Outcome::unit(42).fmap(+8) = {:?}", outcome_functor);

    println!("\n=== Railway-Oriented Pipeline ===\n");

    let pipeline_result = Railway::of("  42  ".to_string())
        .map(|s| s.trim().to_string())
        .then(|s| {
            s.parse::<i32>()
                .map(Outcome::Success)
                .unwrap_or_else(|_| Outcome::Failure("not a number".to_string()))
        })
        .then(|n| {
            if n > 0 {
                Outcome::Success(n)
            } else {
                Outcome::Failure("must be positive".to_string())
            }
        })
        .map(|n| n * 2)
        .finish();

    println!("Pipeline(\"  42  \" -> trim -> parse -> positive -> *2) = {:?}", pipeline_result);

    println!("\n=== Applicative Error Collection ===\n");

    let validated = validate_user("", "not-an-email", -5);
    println!("validate_user(\"\", \"not-an-email\", -5) = {:?}", validated);

    let valid = validate_user("Alice", "alice@example.com", 30);
    println!("validate_user(\"Alice\", \"alice@example.com\", 30) = {:?}", valid);

    println!("\n=== Chained Errors ===\n");

    let err = ChainedError::new("file not found")
        .context("failed to read config")
        .context("application startup failed");
    println!("Chained error: {}", err);
    println!("Root cause: {}", err.root_cause());
}

#[derive(Debug)]
struct User {
    name: String,
    email: String,
    age: i32,
}

fn validate_name(name: &str) -> Outcome<String, Vec<String>> {
    if name.is_empty() {
        Outcome::Failure(vec!["name cannot be empty".into()])
    } else {
        Outcome::Success(name.to_string())
    }
}

fn validate_email(email: &str) -> Outcome<String, Vec<String>> {
    if !email.contains('@') {
        Outcome::Failure(vec!["email must contain @".into()])
    } else {
        Outcome::Success(email.to_string())
    }
}

fn validate_age(age: i32) -> Outcome<i32, Vec<String>> {
    if age < 0 || age > 150 {
        Outcome::Failure(vec!["age must be between 0 and 150".into()])
    } else {
        Outcome::Success(age)
    }
}

fn validate_user(name: &str, email: &str, age: i32) -> Outcome<User, Vec<String>> {
    Outcome::combine3(
        validate_name(name),
        validate_email(email),
        validate_age(age),
        |name, email, age| User { name, email, age },
    )
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use functional_toolkit::maybe::Maybe;
    use functional_toolkit::outcome::Outcome;
    use functional_toolkit::railway::Railway;
    use functional_toolkit::chained_error::{ChainedError, OutcomeContext};
    use functional_toolkit::traits::{Functor, Monad};

    // --- Maybe tests ---

    #[test]
    fn maybe_map_just() {
        let result = Maybe::Just(10).map(|x| x * 2);
        assert_eq!(result, Maybe::Just(20));
    }

    #[test]
    fn maybe_map_nothing() {
        let result: Maybe<i32> = Maybe::Nothing.map(|x| x * 2);
        assert_eq!(result, Maybe::Nothing);
    }

    #[test]
    fn maybe_flat_map_chain() {
        let result = Maybe::Just(5)
            .flat_map(|x| Maybe::Just(x + 1))
            .flat_map(|x| Maybe::Just(x * 3));
        assert_eq!(result, Maybe::Just(18));
    }

    #[test]
    fn maybe_flat_map_short_circuit() {
        let result: Maybe<i32> = Maybe::Just(5)
            .flat_map(|_| Maybe::Nothing)
            .flat_map(|x: i32| Maybe::Just(x * 3));
        assert_eq!(result, Maybe::Nothing);
    }

    #[test]
    fn maybe_filter_passes() {
        let result = Maybe::Just(10).filter(|x| *x > 5);
        assert_eq!(result, Maybe::Just(10));
    }

    #[test]
    fn maybe_filter_rejects() {
        let result = Maybe::Just(3).filter(|x| *x > 5);
        assert_eq!(result, Maybe::Nothing);
    }

    #[test]
    fn maybe_unwrap_or() {
        assert_eq!(Maybe::Just(42).unwrap_or(0), 42);
        assert_eq!(Maybe::<i32>::Nothing.unwrap_or(0), 0);
    }

    #[test]
    fn maybe_or_else() {
        let result = Maybe::Nothing.or_else(|| Maybe::Just(99));
        assert_eq!(result, Maybe::Just(99));

        let result = Maybe::Just(1).or_else(|| Maybe::Just(99));
        assert_eq!(result, Maybe::Just(1));
    }

    #[test]
    fn maybe_zip() {
        assert_eq!(
            Maybe::Just(1).zip(Maybe::Just("a")),
            Maybe::Just((1, "a"))
        );
        assert_eq!(
            Maybe::Just(1).zip(Maybe::<&str>::Nothing),
            Maybe::Nothing
        );
    }

    // --- Outcome tests ---

    #[test]
    fn outcome_map_success() {
        let result: Outcome<_, String> = Outcome::Success(10).map(|x| x + 5);
        assert_eq!(result, Outcome::Success(15));
    }

    #[test]
    fn outcome_map_failure() {
        let result: Outcome<i32, _> = Outcome::Failure("err".to_string()).map(|x: i32| x + 5);
        assert_eq!(result, Outcome::Failure("err".to_string()));
    }

    #[test]
    fn outcome_map_err() {
        let result: Outcome<i32, _> = Outcome::Failure("err").map_err(|e| format!("wrapped: {e}"));
        assert_eq!(result, Outcome::Failure("wrapped: err".to_string()));
    }

    #[test]
    fn outcome_flat_map_chain() {
        let result: Outcome<_, String> = Outcome::Success(10)
            .flat_map(|x| Outcome::Success(x * 2))
            .flat_map(|x| Outcome::Success(x + 1));
        assert_eq!(result, Outcome::Success(21));
    }

    #[test]
    fn outcome_flat_map_failure_propagation() {
        let result: Outcome<i32, _> = Outcome::Success(10)
            .flat_map(|_| Outcome::<i32, _>::Failure("boom"))
            .flat_map(|x| Outcome::Success(x + 1));
        assert_eq!(result, Outcome::Failure("boom"));
    }

    #[test]
    fn outcome_or_else_recovery() {
        let result: Outcome<i32, String> =
            Outcome::Failure("err".to_string()).or_else(|_| Outcome::Success(0));
        assert_eq!(result, Outcome::Success(0));
    }

    #[test]
    fn outcome_zip() {
        let result: Outcome<_, String> = Outcome::Success(1).zip(Outcome::Success(2));
        assert_eq!(result, Outcome::Success((1, 2)));
    }

    // --- Functor and Monad trait tests ---

    #[test]
    fn functor_identity_law_maybe() {
        let original = Maybe::Just(42);
        let mapped = original.clone().fmap(|x| x);
        assert_eq!(original, mapped);
    }

    #[test]
    fn functor_composition_law_maybe() {
        let f = |x: i32| x + 1;
        let g = |x: i32| x * 2;
        let a = Maybe::Just(5).fmap(f).fmap(g);
        let b = Maybe::Just(5).fmap(|x| g(f(x)));
        assert_eq!(a, b);
    }

    #[test]
    fn monad_left_identity_maybe() {
        let f = |x: i32| Maybe::Just(x * 2);
        let left = Maybe::unit(10).bind(f);
        let right = f(10);
        assert_eq!(left, right);
    }

    #[test]
    fn monad_right_identity_maybe() {
        let m = Maybe::Just(42);
        let result = m.clone().bind(Maybe::unit);
        assert_eq!(m, result);
    }

    #[test]
    fn functor_identity_law_outcome() {
        let original: Outcome<i32, String> = Outcome::Success(42);
        let mapped = original.clone().fmap(|x| x);
        assert_eq!(original, mapped);
    }

    #[test]
    fn monad_left_identity_outcome() {
        let f = |x: i32| Outcome::<_, String>::Success(x * 2);
        let left = Outcome::<_, String>::unit(10).bind(f);
        let right = f(10);
        assert_eq!(left, right);
    }

    // --- Applicative tests ---

    #[test]
    fn combine2_both_success() {
        let result = Outcome::combine2(
            Outcome::<_, Vec<String>>::Success(1),
            Outcome::Success(2),
            |a, b| a + b,
        );
        assert_eq!(result, Outcome::Success(3));
    }

    #[test]
    fn combine2_collects_all_errors() {
        let result: Outcome<i32, Vec<String>> = Outcome::combine2(
            Outcome::Failure(vec!["err1".into()]),
            Outcome::Failure(vec!["err2".into()]),
            |a: i32, b: i32| a + b,
        );
        assert_eq!(result, Outcome::Failure(vec!["err1".into(), "err2".into()]));
    }

    #[test]
    fn combine3_collects_all_errors() {
        let result: Outcome<String, Vec<String>> = Outcome::combine3(
            Outcome::Failure(vec!["bad name".into()]),
            Outcome::Success("ok@email.com".to_string()),
            Outcome::Failure(vec!["bad age".into()]),
            |name, email, _age: i32| format!("{name} {email}"),
        );
        assert_eq!(
            result,
            Outcome::Failure(vec!["bad name".into(), "bad age".into()])
        );
    }

    #[test]
    fn combine4_all_success() {
        let result = Outcome::combine4(
            Outcome::<_, Vec<String>>::Success(1),
            Outcome::Success(2),
            Outcome::Success(3),
            Outcome::Success(4),
            |a, b, c, d| a + b + c + d,
        );
        assert_eq!(result, Outcome::Success(10));
    }

    #[test]
    fn combine4_all_failure() {
        let result: Outcome<i32, Vec<String>> = Outcome::combine4(
            Outcome::Failure(vec!["e1".into()]),
            Outcome::Failure(vec!["e2".into()]),
            Outcome::Failure(vec!["e3".into()]),
            Outcome::Failure(vec!["e4".into()]),
            |a: i32, b: i32, c: i32, d: i32| a + b + c + d,
        );
        assert_eq!(
            result,
            Outcome::Failure(vec!["e1".into(), "e2".into(), "e3".into(), "e4".into()])
        );
    }

    // --- Railway tests ---

    #[test]
    fn railway_success_pipeline() {
        let result = Railway::of(10)
            .map(|x| x + 5)
            .then(|x| {
                if x > 0 {
                    Outcome::Success(x * 2)
                } else {
                    Outcome::Failure("negative")
                }
            })
            .finish();
        assert_eq!(result, Outcome::Success(30));
    }

    #[test]
    fn railway_short_circuits_on_failure() {
        let result = Railway::of(10)
            .then(|_| Outcome::<i32, &str>::Failure("step 1 failed"))
            .map(|x| x + 100) // should be skipped
            .then(|x| Outcome::Success(x * 2)) // should be skipped
            .finish();
        assert_eq!(result, Outcome::Failure("step 1 failed"));
    }

    #[test]
    fn railway_recover() {
        let result = Railway::of(10)
            .then(|_| Outcome::<i32, &str>::Failure("oops"))
            .recover(|_| Outcome::Success(0))
            .map(|x| x + 1)
            .finish();
        assert_eq!(result, Outcome::Success(1));
    }

    // --- ChainedError tests ---

    #[test]
    fn chained_error_context() {
        let err = ChainedError::new("root cause")
            .context("mid level")
            .context("top level");
        assert_eq!(err.root_cause(), "root cause");
        assert_eq!(err.chain().len(), 3);
        assert_eq!(err.to_string(), "top level: mid level: root cause");
    }

    #[test]
    fn outcome_with_context() {
        let result: Outcome<i32, ChainedError> =
            Outcome::Failure(ChainedError::new("base error"))
                .with_context("during validation");

        match result {
            Outcome::Failure(e) => {
                assert_eq!(e.chain().len(), 2);
                assert_eq!(e.root_cause(), "base error");
            }
            _ => panic!("expected failure"),
        }
    }

    // --- Maybe/Outcome conversion ---

    #[test]
    fn maybe_to_outcome() {
        assert_eq!(
            Maybe::Just(42).to_outcome("missing"),
            Outcome::Success(42)
        );
        assert_eq!(
            Maybe::<i32>::Nothing.to_outcome("missing"),
            Outcome::Failure("missing")
        );
    }

    #[test]
    fn outcome_to_maybe() {
        assert_eq!(
            Outcome::<i32, &str>::Success(42).to_maybe(),
            Maybe::Just(42)
        );
        assert_eq!(
            Outcome::<i32, &str>::Failure("err").to_maybe(),
            Maybe::Nothing
        );
    }
}
```

### Running

```bash
cargo build
cargo test
cargo run
```

### Expected Output

```
=== Maybe Combinators ===

Just(42).map(|x| x * 2) = Just(84)
Just(10).flat_map(>5 -> +1).map(*3) = Just(33)
Just(7).filter(even) = Nothing
Just("hello").zip(Just(42)) = Just(("hello", 42))

=== Outcome Combinators ===

Success(100).map(|x| x / 4) = Success(25)
Failure("division by zero").unwrap_or(0) = 0

=== Functor and Monad Traits ===

Maybe::fmap(5 -> string) = Just("5")
Maybe::unit(10).bind(>0 -> *2) = Just(20)
Outcome::unit(42).fmap(+8) = Success(50)

=== Railway-Oriented Pipeline ===

Pipeline("  42  " -> trim -> parse -> positive -> *2) = Success(84)

=== Applicative Error Collection ===

validate_user("", "not-an-email", -5) = Failure(["name cannot be empty", "email must contain @", "age must be between 0 and 150"])
validate_user("Alice", "alice@example.com", 30) = Success(User { name: "Alice", email: "alice@example.com", age: 30 })

=== Chained Errors ===

Chained error: application startup failed: failed to read config: file not found
Root cause: file not found
```

## Design Decisions

1. **Custom enums over type aliases**: Defining `Maybe<T>` and `Outcome<T, E>` as independent enums rather than aliases of `std::Option`/`std::Result` forces a genuine implementation of every combinator. It also avoids accidental use of standard library methods and makes the learning explicit.

2. **GATs for Functor/Monad**: Rust lacks higher-kinded types, but Generic Associated Types (`Mapped<U>`) allow expressing "the same wrapper around a different inner type." This is the most ergonomic simulation available in stable Rust without macro tricks.

3. **Applicative uses `Vec<E>` for error accumulation**: Monadic `flat_map` short-circuits on the first error -- that is its nature. Applicative combination requires a different strategy: evaluate all branches independently, then merge errors. Using `Vec<E>` is simple and explicit. A production system might use a `NonEmpty<E>` type to guarantee at least one error.

4. **Railway as a thin wrapper**: `Railway<T, E>` is deliberately a thin wrapper around `Outcome<T, E>` with a builder API. It does not add new semantics -- it provides a fluent interface that makes pipelines readable. This separation of convenience from core logic keeps the architecture clean.

5. **ChainedError stores messages in insertion order**: The `messages` vec stores errors inner-to-outer (root cause first). Display reverses this to show the outermost context first, which matches how humans read error messages ("startup failed: config read failed: file not found").

## Common Mistakes

1. **Using `std::Option`/`std::Result` internally**: It is tempting to use `Option` inside `Maybe`'s implementation (e.g., `filter` returning `None`). This defeats the purpose. Every branch must explicitly return `Maybe::Just` or `Maybe::Nothing`.

2. **Confusing monadic bind with applicative combine**: `flat_map` is sequential -- it short-circuits on the first failure. Applicative combination is parallel -- it evaluates all inputs and collects all errors. Using `flat_map` when you want to collect all validation errors means you only ever see the first error.

3. **Breaking the monad laws**: The left identity law (`unit(a).bind(f) == f(a)`) and right identity law (`m.bind(unit) == m`) must hold. A common bug is implementing `bind` with extra logic (like logging) that violates these equalities. The trait implementations must be pure transformations.

4. **Forgetting the `Nothing`/`Failure` branch in `flat_map`**: The closure passed to `flat_map` only runs on the success case. The failure case must propagate unchanged. Forgetting to return `Nothing`/`Failure(e)` in the match arm causes compilation errors or incorrect behavior.

## Performance Notes

| Operation | Complexity | Notes |
|-----------|-----------|-------|
| `map` | O(1) | Single pattern match + closure call |
| `flat_map` | O(1) | Single pattern match + closure call |
| `filter` | O(1) | Pattern match + predicate call |
| `combine2` | O(1) | Two pattern matches |
| `combine3` / `combine4` | O(n) where n = total errors | Extends error vectors |
| `ChainedError::context` | O(1) amortized | Vec push |
| `Railway::then` | O(1) per step | Delegates to `flat_map` |

The custom types have identical performance to `std::Option` and `std::Result` since they compile to the same discriminated union representation. The compiler optimizes the enum layout identically. The applicative combinators' cost scales with the number of accumulated errors, not the number of inputs, since extending a `Vec` is amortized O(1).

## Going Further

- Implement `Iterator` for `Maybe<T>` (yields one or zero elements) and use it in `for` loops
- Add a `Validated<T, E>` type that is inherently applicative (cannot be used monadically), enforcing error collection at the type level
- Implement `FromIterator` for `Outcome` to collect an iterator of `Outcome` values into a single `Outcome<Vec<T>, Vec<E>>`
- Build a `Reader<Env, T>` monad for dependency injection and compose it with `Outcome` for fallible configuration reading
- Explore the `frunk` crate's HList-based approach to applicative validation for comparison with your manual `combine2`/`combine3`/`combine4`
